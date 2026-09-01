# Benchmark: selecting `nutt-27td-fl-hu-h3`'s draw table (local, heads-up)

> Run 2026-09-01 on the local engine, pinned `ENGINE_SHA`
> `80c7eeb758b05fd957063330747c4f234f77a0f8`. Read with
> [`../measurement.md`](../measurement.md). Raw engine reports sit next to
> this file (`2026-09-01-h3-vs-*.json`).

h3 is h2 with one change: the draw rule comes from a generated table
(`cmd/drawgen`) instead of h1's three-row `standPatFloor`. The chart,
shapes, keeps and betting rules are h2's, unchanged — `continues()` still
reasons from the structural `DrawingKeep`, pinned by
`TestContinuesIgnoresTheDrawTable` — so every number below measures the
draw rule and nothing else.

## What changed in the draw

The old rule read one fact (the opponent's most recent draw count) and
answered one question (stand or break) with h1's untuned thresholds. The
table reads three — street, count, and the read's staleness, the value
`OpponentDraw` always returned and draw.go used to throw away — and
chooses among up to six candidate keeps per hand (`DrawCandidates`),
including the untrimmed straight-risk keeps the structural rule refused.
Cells the generator could not decide fall back to the structural rule, and
a Broken-category hand is never allowed to stand pat: h3 still does not
snow, for the reasons draw.go documents.

Generation: `drawgen -deals 20000000 -rounds 3 -seed 1` — branched
rollouts (`equity.DrawSamples`) scoring every candidate against the same
villain, fixed-pointed with both seats on the previous round's table.
37,744 of 156,975 cells decided, covering 95.5% of sampled decisions; by
sample weight the decisions split pat 22.7% / structural keep 60.2% /
alternate keeps 17.1%.

Two reversals of h1's believed rule survived both independent 20M-deal
generations and are pinned in `policy_test.go` / `draw_test.go`:

- **A nine no longer breaks into a one-card read on draw2/draw3.** h1's
  eight-floor broke it; measured, standing wins (0.698 vs 0.503 at draw3).
  The last draw is the one you cannot come back from.
- **A rough ten no longer stands on a three-card read** while a draw
  remains — its break keep is one card from beating every ten.

## The gate

The h2 hosted regression traced to a passive local judge, so the gate here
is a ladder, and the pressure probes are hard gates: a candidate that beat
h1 but bled to the shover would have been rejected. Same protocol as the
h2 selection: 100,000 decks = 200,000 hands per match, duplicate dealing,
`--seed 1`, `faults: 0` everywhere (plus a forfeit-policy smoke match).

| opponent | h3 BB/100 | CI95 | h2 was | delta |
|---|---|---|---|---|
| h2 (repo head) | `+2.28` | `±0.63` | — | — |
| h1 (`bin/bot-h1`) | `+4.28` | `±0.62` | `+3.37` | `+0.91` |
| `builtin:shover` | `+186.05` | `±2.23` | `+155.79` | `+30.26` |
| `builtin:caller` | `+91.77` | `±0.56` | `+89.06` | `+2.71` |
| `builtin:folder` | `+25.98` | `±0.11` | `+25.98` | `0.00` |
| `builtin:random:1` | `+76.68` | `±0.84` | `+68.75` | `+7.93` |

No regression on any rung, and the largest gain is against the most
aggressive opponent — the opposite signature to h2's failure. The folder
delta is exactly zero because the folder never reaches a draw.

Timing (solo run, vs folder — the slowest path): mean 89µs, p99 267µs,
inside the ~380µs hosted budget. The table adds one classify and one array
index per draw decision.

## The chart question, left open

The h2 post-mortem suggested the r=0.55 chart is "likely closer to right"
hosted. The local judge cannot settle that — rewarding defend-100% locally
is exactly the bias that produced h2 — and the plan's fallback trigger
(the local gate showing the defend-heavy chart dominating the result) did
not fire: h3 improves most under maximum pressure. So h3 ships on the h2
chart, and the hosted ladder gets to say whether the defend leak is now
priced in. If it is not, re-pricing defend and continue together against
the draw table is h4, as the h2 report already concluded.

## Reproduction

```sh
poker-arena run --game 27td-fl --hands 100000 --seed 1 --output json \
  --bot 'h3@cmd:./bin/bot' --bot 'OPP'
```

with `OPP` one of `h2@cmd:<h2 build of 4574e8a>`, `h1@cmd:./bin/bot-h1`,
or the builtins above. The h3 binary is the repo head at this commit;
regenerate the table with `go run ./cmd/drawgen -deals 20000000 -rounds 3
-seed 1` (near-tie cells flip between generations — the pinned tests only
hold cells that were stable across independent runs).
