# Benchmark: `nutt-27td-fl-hu-h1` vs the builtin ladder (heads-up)

> Run 2026-08-30 on the local engine. Read with
> [`../measurement.md`](../measurement.md) in hand — especially "Correctness
> before strength" and the note that builtins are rung 0–1 of the ladder.

## Setup

| | |
|---|---|
| bot | `nutt-27td-fl-hu-h1`, built from repo commit `44b7b0b` (clean tree) |
| engine | `poker-arena` at pinned `ENGINE_SHA` `80c7eeb758b05fd957063330747c4f234f77a0f8` |
| game | `27td-fl`, heads-up, sb 50 / bb 100, stack 100 bb, raise cap 4 |
| dealing | `duplicate` (default) — each deck played from both seats |
| volume | 100,000 decks = **200,000 hands** per matchup (100,000 paired observations) |
| seed | `--seed 1` for every match; `builtin:random` additionally seeded `random:1` |
| fault policy | `substitute` (default); `faults` confirmed 0 before reading any rate |

Reproduce any row with:

```sh
make engine bot
third_party/poker-arena/target/release/poker-arena run \
  --game 27td-fl --hands 100000 --seed 1 \
  --bot 'h1@cmd:./bin/bot' --bot 'OPP@builtin:OPP' \
  --timeout-ms 1000 --output json
```

Raw engine reports are recorded next to this file
(`2026-08-30-h1-vs-*.json`).

## Results

All rates are h1's, in BB/100 with CI95 half-width, on paired duplicate
observations. `faults: 0` and `forfeited_by: null` in every match.

| opponent | h1 BB/100 | CI95 | h1 vpip | h1 fold | h1 wtsd | h1 wsd |
|---|---|---|---|---|---|---|
| `builtin:folder` | `+37.15` | `±0.23` | 0.248 | 0.252 | — | — |
| `builtin:random:1` | `+104.49` | `±1.52` | 0.453 | 0.425 | 0.153 | 0.833 |
| `builtin:caller` | `+151.55` | `±1.13` | 0.556 | 0.252 | 0.748 | 0.769 |
| `builtin:shover` | `+256.38` | `±4.18` | 0.556 | 0.740 | 0.260 | 0.993 |

Decision latency (h1):

| opponent | decisions | mean | p99 | max |
|---|---|---|---|---|
| `builtin:folder` | 100,000 | 84µs | 225µs | 2.3ms |
| `builtin:random:1` | 629,538 | 38µs | 95µs | 6.3ms |
| `builtin:caller` | 1,097,222 | 36µs | 80µs | 6.0ms |
| `builtin:shover` | 1,144,566 | 35µs | 80µs | 40.4ms |

## Reading the result

- **The legality gate holds.** Zero faults over 800,000 hands and ~3M
  decisions, across four very different pressure profiles. `wire.Legalize`
  is doing its job.
- **The evaluator and draw discipline show through.** wsd 0.83 vs random and
  0.99 vs shover: when h1 reaches showdown it almost always has the best
  hand. Against shover it folds 74% of the time and only continues with
  goods — exactly what a value-only strategy should do against constant
  aggression.
- **The button open range is measurably tight.** Against folder, h1 opens
  only ~50% of its buttons (vpip 0.248 across all hands, all of it raises)
  and open-folds the rest. The always-open ceiling against folder is
  75 BB/100; h1 collects 37.15. Roughly half the available steal EV is
  unclaimed. This is the fixed chart, not a bug — h1 has no opponent model
  and no notion that the blinds are dead money against this opponent.
- **Latency is comfortable but not frontier.** Mean 35–84µs against the
  frontier's 25µs; the real budget is ~380µs/decision (10-minute wall clock
  over a 300k-hand match), so throughput is fine. The 40ms max outlier vs
  shover is worth a glance eventually (likely GC or process warm-up) but is
  nowhere near threatening the wall clock: 1.58M decisions × 38µs ≈ 60s.
- **These rungs are saturated.** Per the ladder in `measurement.md`, the
  builtins are rung 0–1: they test legality and gross blunders, and h1 now
  clears them with CIs a hundred times smaller than the edges. **Nothing
  further can be learned locally from builtins.** The next measurable rung
  is hosted: `swit-27td-1.0` (competent play, 10k hands), then the tuned
  blueprints. These numbers are the baseline to hold while changing the
  policy — any regression against this table at seed 1 is a real
  regression.

## What to try next (ideas only — nothing here is implemented)

Ordered roughly by expected value per unit of effort. Per
[`../../naming.md`](../../naming.md), any retuned threshold or new strategy
is a **new bot name** (h2, or a different scheme), not a re-upload of h1.

1. **Widen the button chart.** The folder matchup shows ~50% of buttons
   open-folded. Heads-up 27TD equilibrium opens far wider than 50% —
   position plus two blinds of dead money carry weak one-card draws.
   Cheapest first step: re-derive the open threshold in `chart.go` from
   equity rollouts rather than hand-rank intuition. Likely worth several
   BB/100 against everything, not just folder.

2. **Equity rollouts instead of a static chart.** The decision budget is
   ~380µs and h1 currently spends 38µs. There is room for a few thousand
   Monte Carlo rollouts per decision using the existing `internal/deuce`
   evaluator: sample opponent hands consistent with their public draw
   counts, roll the remaining draws, and bet/fold on equity vs pot odds.
   This replaces every hand-tuned threshold with one principled number and
   scales to all streets.

3. **Use the opponent's public draw counts.** Draw counts are broadcast in
   the event stream and `internal/table` already rebuilds state. A pat
   opponent versus a 3-card-drawing opponent should not face the same
   bet/call thresholds. This is the cheapest form of hand reading — the
   information is already parsed and currently ignored.

4. **A minimal opponent model.** Track per-match fold-to-raise, vpip, and
   aggression from the event stream, and shift thresholds along one axis
   (tighter vs stations, wider steals vs nits). The caller/folder results
   show a single fixed strategy cannot maximize against both ends; even a
   two-mode adjustment claws back real EV. Start with steal frequency vs
   fold-to-open — the folder gap quantifies exactly that.

5. **Snowing (the documented omission in `draw.go`).** Standing pat on a
   brick hand and betting through is the classic 27TD bluff, and its
   absence is what makes h1 fully value-weighted (wsd 0.99 vs shover reads
   great here but means h1 never wins a pot it didn't earn at showdown).
   Snowing only pays against opponents who fold, so it belongs with (4) —
   gate it on observed fold rates.

6. **Mixing (the documented omission in `chart.go`).** Determinism is what
   makes h1 exploitable by any opponent that models it. Seeded mixed
   strategies at the marginal decisions (borderline opens, borderline
   calls) blunt that without giving up much EV. Keep the seed derived from
   the hand id so play stays reproducible in spar logs.

7. **A blueprint via self-play / CFR.** The real jump: fixed-limit 27TD
   heads-up has a small betting tree per street; the size is in the draws.
   With an abstraction over hand classes (made-low rank, cards-to-a-smooth-
   low, blockers held) the game is within reach of external-sampling MCCFR.
   The rung-3 hosted opponent is a 22.7 MB "tuned blueprint" — tables that
   size compile comfortably into the ≤300 MiB static artifact. This is
   weeks, not days, and wants (2) as its evaluator substrate first.

8. **Harness support, so the above is measurable.** A `make bench` target
   that runs this exact ladder (same seeds, same volumes) and appends a
   dated report here; and a best-response-lite probe (an opponent scripted
   to attack one known leak, e.g. relentless button raises against the
   tight chart) to measure exploitability locally. The builtins can no
   longer distinguish good from better — local measurement has to improve
   before the policy does, or every change is flying blind between
   uploads.

The honest summary: h1's floor is solid — legal everywhere, value-sound,
fast. Its two designed-in gaps (no bluffs, no adaptation) are now visible
as numbers: ~38 BB/100 left against folder, a pure-value showdown profile.
The next generation should pick one gap and close it under a new name.
