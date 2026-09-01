# Benchmark: selecting `nutt-27td-fl-hu-h2`'s chart (local, heads-up)

> Run 2026-09-01 on the local engine, pinned `ENGINE_SHA`
> `80c7eeb758b05fd957063330747c4f234f77a0f8`. Read with
> [`../measurement.md`](../measurement.md). Raw engine reports sit next to
> this file (`2026-09-01-h2-*.json`).

h2 is h1 with one change: the predraw Open/Defend columns come from an
equity-rollout chart (`cmd/chartgen`) instead of hand-tuned thresholds. The
shapes, keeps, draw rule and betting rule are h1's, unchanged — so every
number below measures the chart and nothing else. This addresses the
dominant leak the [2026-08-30 hosted ladder](2026-08-30-h1-hosted-ladder.md)
measured: h1's over-folding, collected by every rival at ~12 BB/100.

## Setup

| | |
|---|---|
| bots | h2 candidates from repo state at this commit; h1 built from `747c1da` policy (identical behavior — the draw.go change is a pure refactor) |
| game | `27td-fl`, heads-up, sb 50 / bb 100, duplicate dealing |
| volume | 100,000 decks = 200,000 hands per match, `--seed 1` |
| generator | `chartgen -samples 4000 -iter 6000 -rounds 8 -seed 1`, sweeping realization `r` |

## Candidate sweep

The generator's one free parameter is the realization factor `r`. Three
candidates, each played against h1:

| r | button open | BB defend | vs h1 BB/100 | CI95 |
|---|---|---|---|---|
| 0.55 | 100% | 64% | `+2.77` | `±0.42` |
| 0.70 | 100% | 67% | `+2.95` | `±0.44` |
| **0.85** | **70.4%** | **100%** | **`+3.37`** | `±0.43` |

Cross-check, since h1's tight opens never test a wide defend:
`r=0.85` vs `r=0.55` (the 100%-opener) came out **`+5.01 ±0.43`** for
r=0.85. The tight-open/defend-everything shape wins both boards, so
**r=0.85 is h2's chart**.

Two findings worth keeping:

- **Pot odds alone defend everything.** Under check-down showdown equity,
  even the bottom of the deck clears the big blind's 3:1 price; the whole
  cost of a bad defend lives in later streets. The winning chart embraces
  this — defend 100% — and relies on the postdraw rules to cap the damage.
  The property tests in `policy_test.go` document the measured choice.
- **The open EV model is fold-equity dominated.** At low r the model opens
  100% of buttons; those candidates measurably lose to the 70% chart.
  What actually bled was opening the bottom ~30%, not defending it.

## Regression: the builtin ladder at seed 1

Same protocol as [2026-08-30-h1-vs-builtins.md](2026-08-30-h1-vs-builtins.md).
`faults: 0` in every match, here and in the sweep.

| opponent | h2 BB/100 | CI95 | h1 was | delta |
|---|---|---|---|---|
| `builtin:folder` | `+25.98` | `±0.11` | `+18.58` | `+7.40` |
| `builtin:random:1` | `+68.75` | `±0.84` | `+52.25` | `+16.50` |
| `builtin:caller` | `+89.06` | `±0.60` | `+75.78` | `+13.28` |
| `builtin:shover` | `+155.79` | `±2.36` | `+128.19` | `+27.60` |

Every rung improves; nothing regresses. The folder gain is the reclaimed
steal EV the 2026-08-30 report predicted (h1 left ~half of it unclaimed).

## What is not yet measured

The number that matters is hosted: **`swit-27td-1.0`
(digest `bc589c4441f5`) at 10,000 hands**, where h1 scored
`-12.12 ±3.19`. Everything above is against h1 and the saturated builtins —
a chart that beats the over-folder locally can still lose to a rival that
attacks the 100% defend. Upload and run rung 2 before believing anything.
