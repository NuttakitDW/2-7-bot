# Benchmark: `nutt-27td-fl-hu-h1` vs the hosted ladder (heads-up)

> Run 2026-08-30 on the live platform, same day as the
> [local builtin benchmark](2026-08-30-h1-vs-builtins.md). Units here are
> the platform's: **big bets per 100 hands** (1 BB = 2 big blinds — see
> `../../arena/games.md`). Read with [`../measurement.md`](../measurement.md).

## Setup

| | |
|---|---|
| bot | `nutt-27td-fl-hu-h1`, version `a06919a0`, digest `6110e23e3fd5` (the 2026-08-22 upload) |
| queued via | `bin/arena compete -game 27td-fl -hands N -versions <h1>,<opp> -watch` |
| dealing | duplicate, 3 CPU cores, 5000ms decision limit (platform defaults) |
| volumes | per the ladder in `measurement.md`: 600 / 2,000 / 10,000 / 50,004 / 300,000 |

Match records are immutable on the platform; the match ids below are the
record. Opponent versions are pinned by digest, so these rows are comparable
to any future run against the same digest.

## Results

h1's rate, BB(big bets)/100, CI95 half-width. `faults: 0` in every
completed match.

| match | opponent | digest | hands | h1 BB/100 | CI95 |
|---|---|---|---|---|---|
| 104 | `autofold` | `cad2b7e33185` | 600 | `+18.75` | `±2.13` |
| 105 | `autocall` | `2c3c80daa39a` | 600 | `+92.62` | `±10.43` |
| 106 | `autopush` | `e7305255a31a` | 600 | `+185.62` | `±37.84` |
| 107 | `rand/30/30/40` | `5a9444bc267d` | 2,000 | `+39.94` | `±5.95` |
| 108 | `rand/50/30/20` | `e6538915f21d` | 2,000 | `+100.58` | `±11.22` |
| 109 | `rand/30/50/20` | `35d6aabe36e6` | 2,000 | `+63.14` | `±8.09` |
| 110 | `rand/10/70/20` | `3018934b7cf5` | 2,000 | `+59.94` | `±5.89` |
| 111 | `swit-27td-1.0` | `bc589c4441f5` | 10,000 | `-12.12` | `±3.19` |
| 113 | `swit-27td-5.1-i048` | `d8b54676bcad` | 50,004 | `-15.64` | `±1.22` |
| 114 | `paul-gandalf200-4bit` | `92df5c730cbb` | 300,000 | `-12.42` | `±0.61` |
| 112, 115 | `paul-sauron100-lite-1bit` | `e568bc040771` | 0 | failed | — |

`paul-sauron100-lite-1bit` failed twice with `engine-failed` at hand 0 —
a platform-side launch failure of that artifact at 2 seats, before any
decision was made (h1 was charged no faults and no forfeit). Not retried a
third time; try again after that bot uploads a new version.

## Reading the result

- **h1 sits exactly between rung 1 and rung 2 of the ladder.** It beats
  every baseline by a distance no CI can question, and loses to every real
  rival. That is the designed floor doing what a floor does.
- **The gap to competent play is ~12 BB/100.** `swit-27td-1.0` — 1.3 MiB,
  the smallest rival on the platform — beats h1 by about the same margin as
  the 79.6 MiB frontier bot does. Whatever h1's leaks are, they are basic
  enough that rung 2 already collects them in full.
- **`swit-27td-5.1-i048` beats h1 *harder* than the frontier does**
  (−15.64 vs −12.42, CIs well apart). A near-equilibrium bot collects only
  what h1 gives up unprompted; a tuned blueprint appears to be actively
  exploiting a pattern. h1 is deterministic — that is the price of
  legibility, now with a number on it: roughly 3 BB/100 of extra
  exploitation.
- **The leak profile is visible in the behavior stats.** Against all three
  rivals h1 folds 57–62% of hands while they vpip 63–66%. They open wide,
  h1 surrenders its blinds, and its tight range means the pots it does
  contest are too transparent (wsd 55–58% — barely above break-even at
  showdown despite entering with the stronger range). Same diagnosis as the
  local folder matchup: the chart is too tight for heads-up, and h1 never
  fights back without the goods.
- **Cross-check with the local benchmark holds.** Local `builtin:folder`
  +18.58 BB/100 vs hosted `autofold` +18.75 ±2.13 — same number. The local
  and hosted ladders measure the same bot the same way, so local spars
  remain a valid cheap proxy for the baseline rungs.
- **Latency on the platform is comfortable**: h1 mean 60µs, max 0.57ms over
  300k hands; the rivals decide in 20–30µs. No wall-clock risk.

## What this changes about the next steps

The ideas list in the [builtins report](2026-08-30-h1-vs-builtins.md)
stands, but the hosted numbers reorder it:

1. **Chart width first** — the over-folding leak is the one every rival
   collects on. Ideas 1 (wider buttons) and 3 (use opponent draw counts)
   attack it directly and are the cheapest of the list.
2. **Mixing moves up** — the 3 BB/100 gap between swit-5.1 and gandalf is a
   measured cost of determinism, not a theoretical one.
3. **The target for h2 is concrete:** beat `swit-27td-1.0` (digest
   `bc589c4441f5`) at 10,000 hands. That is the next rung, it costs one
   competition to measure, and −12.12 ±3.19 is the number to move.
