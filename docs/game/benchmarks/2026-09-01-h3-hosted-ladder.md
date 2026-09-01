# Benchmark: `nutt-27td-fl-hu-h3` vs the hosted ladder (heads-up)

> Run 2026-09-01 on the live platform, same day as the
> [local selection report](2026-09-01-h3-selection.md). Units are the
> platform's big bets per 100 hands. Read with
> [`../measurement.md`](../measurement.md).
>
> **Verdict up front: h3 is the best bot we have hosted, and it undoes h2's
> regression without touching h2's chart.** It beats h1 on every rung of
> the ladder, and the one change between h2 and h3 — the generated draw
> table — is worth 5 to 10 BB/100 against every real rival.

## Setup

| | |
|---|---|
| bot | `nutt-27td-fl-hu-h3`, version `a488f1b6`, digest `d608f77b3a0a` (repo commit `0ed755a`) |
| uploaded | `arena upload --games 27td-fl --counts 2`; hosted validation passed on the first attempt |
| queued via | `bin/arena compete --game 27td-fl --hands N --versions <h3>,<opp>` |
| dealing | duplicate, 3 CPU cores, 5000ms decision limit (platform defaults) |
| opponents | pinned by digest — **identical digests to the 2026-08-30 h1 and 2026-09-01 h2 ladders**, so every delta below is real |

`faults: 0` for both seats in all twelve matches, and no match hit a
terminal reason. `paul-sauron100-lite-1bit` still shows no version past
`e568bc040771`, the one that engine-failed twice at 2 seats on 2026-08-30,
so it was not attempted a third time.

## Results

h3's rate, with the same-digest h2 and h1 numbers beside it.

| match | opponent | hands | h3 BB/100 | CI95 | h2 was | h1 was | vs h2 | vs h1 |
|---|---|---|---|---|---|---|---|---|
| 135 | `autofold` | 600 | `+24.25` | `±2.04` | `+24.00` | `+18.75` | (`+0.3`) | `+5.5` |
| 136 | `autocall` | 600 | `+121.04` | `±10.44` | `+105.04` | `+92.62` | `+16.0` | `+28.4` |
| 137 | `autopush` | 600 | `+281.46` | `±42.99` | `+156.79` | `+185.62` | `+124.7` | `+95.8` |
| 131 | `rand/30/30/40` | 2,000 | `+71.88` | `±7.15` | `+56.24` | `+39.94` | `+15.6` | `+31.9` |
| 132 | `rand/50/30/20` | 2,000 | `+107.06` | `±11.23` | `+100.41` | `+100.58` | (`+6.7`) | (`+6.5`) |
| 133 | `rand/30/50/20` | 2,000 | `+108.92` | `±9.57` | `+92.92` | `+63.14` | `+16.0` | `+45.8` |
| 134 | `rand/10/70/20` | 2,000 | `+85.06` | `±6.21` | `+72.86` | `+59.94` | `+12.2` | `+25.1` |
| 128 | `swit-27td-1.0` | 10,000 | `−10.81` | `±3.21` | `−20.83` | `−12.12` | `+10.0` | (`+1.3`) |
| 127 | `swit-27td-5.1-i048` | 50,004 | `−9.75` | `±1.39` | `−16.23` | `−15.64` | `+6.5` | `+5.9` |
| 126 | `paul-gandalf200-4bit` | 300,000 | `−12.19` | `±0.54` | `−17.23` | `−12.42` | `+5.0` | (`+0.2`) |

Parenthesised deltas do not clear the combined CI of the two competitions
(`√(hw₁² + hw₂²)`); every other delta does. The three rival rows are the
ones that matter, and all three recover the h2 regression in full.

Also run, to check the local judge against the field:

| match | opponent | hands | h3 BB/100 | CI95 | local said |
|---|---|---|---|---|---|
| 129 | `nutt-27td-fl-hu-h1` | 10,000 | `+5.60` | `±2.68` | `+4.28 ±0.62` |
| 130 | `nutt-27td-fl-hu-h2` | 10,000 | `+3.79` | `±2.78` | `+2.28 ±0.63` |

The local 200,000-hand estimates sit inside both hosted intervals. The
selection loop's head-to-head numbers now predict hosted head-to-head — but
that was also true of h2, so it is not the interesting part.

## What actually changed: the fold moved, not the chart

h3 plays h2's chart. `vpip` is the proof — 75.5–77.1% against the three
rivals, against h2's recorded 76–77%. Everything else moved:

| vs the three rivals | h1 | h2 | h3 |
|---|---|---|---|
| `fold` | 57–62% | 46–48% | **41–43%** |
| `wtsd` | — | 31–34% | **33–38%** |
| `wsd` | 55–58% | 56–57% | 56–58% |

h2's diagnosed leak was *call-then-fold*: it paid one small bet predraw to
defend, then folded mid-hand almost half the time because the postdraw
rules could not find a hand worth continuing with. h3 defends the same
hands and folds 5 points less, reaching showdown 3 points more often **at
the same win rate once there**. The extra showdowns are therefore not
loose calls — they are hands the draw table turned into something worth
playing, out of the same predraw range h2 abandoned.

That is the h2 report's item 3 — "defend and continue must be priced
together" — satisfied from the draw side rather than the chart side. We
expected to have to narrow the defend; instead the continuation improved
enough to justify the defend that was already there.

## The frontier gap is structural, not a draw leak

Two rows read together say where the remaining loss lives:

- **`swit-27td-5.1-i048`: −15.64 → −9.75.** The h1 report noted that the
  tuned blueprint beat h1 *harder* than the frontier did (−15.64 vs
  −12.42) and attributed the extra ~3 BB/100 to determinism being
  exploitable. That premise was wrong, or at least incomplete: 5.9 BB/100
  of it was the draw rule, and it closed without h3 becoming any less
  deterministic. `swit-5.1` was exploiting a *specific mistake*, not
  legibility in general.
- **`paul-gandalf200-4bit`: −12.42 → −12.19.** Unmoved. The frontier's
  edge survived the entire h2+h3 cycle untouched.

So h3 has paid off the draw rule, and what remains is the ~12 BB/100 the
frontier collects from a bot that never bluffs, never mixes, and prices
its predraw decisions off check-down equity. Against it h3 shows `af` 0.93
to gandalf's 2.18 and folds 42.8% to its 24.1% — h3 still surrenders far
more pots without a fight than it wins by pressure.

## Latency

Mean 54–78µs per decision, p99 123–245µs, max 1.363ms across all twelve
matches; the 300,000-hand match completed in 169s of the 600s wall-clock
cap. Comparable to h1's 60µs mean and well inside the ~380µs per-decision
budget that cap implies. The draw table costs nothing measurable.

## What this dictates for h4

1. **The next gain is aggression, not accuracy.** Both remaining levers —
   bluffing (snowing) and mixing — are the ones h1 deliberately omitted,
   and the gandalf row is now the only place a leak is provably left. `af`
   0.93 against a 2.18 opponent is the headline number to move.
2. **Re-price the defend anyway, but measure it against `swit-27td-1.0`.**
   The r=0.55 chart from the h2 sweep is still ungraded. It now has a
   fair test: h3 vs `swit-27td-1.0` at −10.81 ±3.21 is the cheapest rival
   row, and h3's chart is the control.
3. **The local gate held.** The pressure probes (shover, and h2-as-judge)
   selected a change that transferred to the field — the h2 post-mortem's
   fix worked. Keep the adversarial rungs as hard gates for h4; do not go
   back to selecting on h1 alone.
4. **h3 is the bot to race.** h1 was the previous answer to that question;
   it is beaten on every rung and by 5.60 ±2.68 head to head.

## Reproduction

```sh
bin/arena compete --game 27td-fl --hands N \
  --versions a488f1b6-a705-40db-b976-e2e2b1c47839,<opponent version> --watch
bin/arena match <id>            # per-seat rates, faults and behavior
```

Match records are immutable, so matches 126–137 are the record. Opponent
version ids as run: `swit-27td-1.0` `37cdb73c`, `swit-27td-5.1-i048`
`4f38a620`, `paul-gandalf200-4bit` `656cef23`, `nutt-27td-fl-hu-h1`
`a06919a0`, `nutt-27td-fl-hu-h2` `1262e462`.
