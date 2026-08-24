# `27td-fl` — the rules the protocol refuses to state

> Derived 2026-08-22 from the upstream engine at the pinned
> `ENGINE_SHA = 80c7eeb758b05fd957063330747c4f234f77a0f8`, checked out at
> `third_party/poker-arena/` by `make engine`. **The engine is ground truth, not
> poker websites** — casino 2-7 may differ from this implementation in exactly the
> details that decide hands. Re-verify after any `ENGINE_SHA` bump.
>
> Every claim below cites a `file:line` relative to
> `third_party/poker-arena/crates/poker-core/src/`, so `game/spec.rs:567` is
> `third_party/poker-arena/crates/poker-core/src/game/spec.rs` line 567. Citations
> to `WIRE_PROTOCOL.md` are to the vendored copy in `../protocol/`.

`WIRE_PROTOCOL.md:79-86` states that `hello` carries no rules because "the bot is
expected to know the game". This document is where that knowledge lives.

## The spec in one place

`27td-fl` is `triple_draw_base(stakes, hand_size = 5)` with the deuce-to-seven
evaluator substituted in (`game/spec.rs:567-573`).

| property | value | source |
|---|---|---|
| Deck | standard 52 | `deck.rs:20` |
| Hand size | 5, dealt private | `game/spec.rs:651`, `:676` |
| Seats | `2..=6` | `game/spec.rs:665` |
| Betting | fixed limit, `raise_cap: Some(4)` | `game/spec.rs:672` |
| Forced bets | blinds, ante optional (`0` in practice) | `game/spec.rs:671` |
| Evaluator | `EvalKind::DeuceToSevenLow` | `game/spec.rs:571` |
| Showdown | single side; **`lo` is always `None`** | `game/spec.rs:688-696` |
| Hole usage | `HoleUsage::AllOwn` — all five of your own cards | `game/spec.rs:692` |
| Rate unit | `BB/100`, divisor = big **bet** | `game/spec.rs:701-706` |

`raise_cap` counts wagers, and **the opening bet or blind counts as the first**
(`WIRE_PROTOCOL.md:112-117`), so a capped street allows bet + three raises.

## Street structure and bet tiers

Four streets, each a draw phase (where applicable) followed by a betting round
(`game/spec.rs:673-683`):

| # | label | deal | tier | first to act |
|---|---|---|---|---|
| 0 | `predraw` | 5 private cards | **Small** | after blinds |
| 1 | `draw1` | draw, `max: 5` | **Small** | left of button |
| 2 | `draw2` | draw, `max: 5` | **Big** | left of button |
| 3 | `draw3` | draw, `max: 5` | **Big** | left of button |

This settles two things the wire docs left open.

**`max_discards` is 5, not 3.** `DealSpec::Draw { max: hand_size }` with
`hand_size = 5` (`game/spec.rs:659`). The `3` in the protocol's transcript belongs
to an unnamed game and does not apply here. You may discard your entire hand.

**The small-bet/big-bet boundary is between `draw1` and `draw2`.** `predraw` and
`draw1` bet in small bets; `draw2` and `draw3` bet in big bets
(`game/spec.rs:674-682`). Confirm at runtime from `min_to`/`max_to` rather than
hardcoding, but this is the shape.

At the documented stakes (SB 50 / BB 100, ante 0), `tiers()` returns
`(100, 200)` — **small bet = 1 big blind, big bet = 2 big blinds**
(`game/spec.rs:756-758`). So one `BB` in a `BB/100` figure is 200 chips.
`starting_stack` is 10000 and is **reset every hand** (`WIRE_PROTOCOL.md:96`) —
this is chip-EV per hand, not a running tournament stack.

## Hand ranking

The evaluator is one line (`eval/low.rs:16-24`): take every 5-card subset, rank it
with the ordinary **high**-hand classifier with the ace-low wheel disabled, take the
minimum, and subtract from `0x00FF_FFFF`.

The engine's own encoding note (`eval/mod.rs:28-30`) says the same thing in
prose: the 2-7 ordering is the exact inverse of the high ordering, straights and
flushes count against you, the ace is always high, and the returned value is
`0x00FF_FFFF` minus the high encoding.

Consequences, each backed by an engine test in `eval/low.rs`:

- **`7-5-4-3-2` is the nut hand** (`:176`).
- **The ace is always high and never completes a wheel.** `A-5-4-3-2` is not a
  straight; it is an ace-high no-pair hand, among the worst holdings — though it
  still beats any pair (`:199-206`).
- **Straights and flushes count against you.** `2-3-4-5-7` beats `2-3-4-5-6`
  (`:185`), and any nine-low beats both a six-high straight and a king-high flush
  (`:190-195`).
- Suits are irrelevant except for the flush check itself (`:180`).

`HandValue` is a thin `u32` and the encodings are **frozen** (`eval/mod.rs:9`), so
`showdown-show.hi` is directly comparable and directly decodable. The high encoding
underneath it is `class` in bits `[20..24)` and five 4-bit tiebreak ranks in bits
`[0..20)`, most significant first, with `Two = 0 … Ace = 12` (`eval/mod.rs:11-20`).
Values are only comparable within one `EvalKind`.

## Draw mechanics — three details that matter

From the draw-street contract at `game/state.rs:135-157`.

**Everyone draws before anyone bets, in seat order starting left of the button**
(`game/state.rs:136-139`, `game/state.rs:734-745`). All-in seats draw too. Heads-up this means the big
blind draws first and the button draws last — the button acts last in both phases,
so it sees every opponent's draw count before choosing its own.

**Replacements are dealt immediately, before the next seat draws** (`game/state.rs:142-143`).
The deck is consumed in draw order, so an earlier drawer's cards are gone before a
later drawer sees the deck.

**The deck can run out, and the muck is reshuffled back in** (`game/state.rs:145-152`).
Six seats drawing five cards three times would need far more than 52 cards. When a
replacement request exceeds the deck, the engine deals what remains, then reshuffles
the muck — every card discarded earlier this hand plus every folded hand,
*excluding the drawing seat's own just-discarded cards* — and continues. Only in a
pathological case may the seat's own discards be recycled. **There is no event for
this**; determinism comes from the seeded RNG.

That last point breaks naive card removal: a card you saw discarded is not
permanently dead, and after a reshuffle a card you discarded yourself on an earlier
street can come back to you. Any card-counting logic must model the muck, and must
not assume a card seen once is gone for the hand.

**Run-out situations still run every draw phase** while skipping the betting rounds
(`game/state.rs:153-156`), so an all-in seat draws to a complete final hand.

## Positional facts from the wire

The button is always seat 0 and the arena rotates *bots* through seats between
hands rather than moving the button (`WIRE_PROTOCOL.md:145-150`); seat number is
therefore position. Heads-up, seat 0 posts the small blind and seat 1 the big blind.
Expect your `seat` to change every hand.

## Verified empirically

Confirmed 2026-08-22 from a 40-hand local spar
(`poker-arena run --game 27td-fl --bot a@builtin:random:1 --bot b@builtin:random:2 --log …`),
reading the unredacted event log rather than trusting the source read alone:

- **`max_discards` is 5.** `draw-result.count` values of 0, 1, 2, 3, 4 **and 5** all
  occur. A seat may discard its entire hand.
- **Bet tiers are as the spec says.** Wager increments by street: `predraw` 100,
  `draw1` 100, `draw2` 200, `draw3` 200 — small bet 100 (= 1 big blind), big bet 200
  (= 2 big blinds), with the tier changing between `draw1` and `draw2`.
- **Street labels** are exactly `predraw`, `draw1`, `draw2`, `draw3`.

**Decoder gotcha found while doing this.** The `street-start` event for `predraw`
fires **after** the `post` events for the blinds, not before. A decoder that resets
its per-street commitment tracking on `street-start` will therefore wipe the blinds
and mis-measure the first predraw raise as a full 200 rather than an increment of
100. Seed street commitments from `post`, or skip the reset on street 0.

## Resolved and still open

Resolved here, against the wire docs' own list of gaps: `max_discards` (5), the
small-bet/big-bet boundary (after `draw1`), the `HandValue` encoding (frozen,
`0x00FF_FFFF - high_no_wheel`), and `lo` (always `None`, so `pot-awarded.side` is
always `"whole"` for this game).

Still unverified, because it is a platform behaviour rather than an engine one:
what the three numbers in the `rand/30/30/40` baseline name mean.
