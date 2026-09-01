# Benchmark: `nutt-27td-fl-hu-h2` vs the hosted ladder (heads-up)

> Run 2026-09-01 on the live platform, same day as the
> [local selection report](2026-09-01-h2-selection.md). Units are the
> platform's big bets per 100 hands. Read with
> [`../measurement.md`](../measurement.md).
>
> **Verdict up front: h2 is a regression against real opposition.** It
> improves on h1 against every baseline and loses *harder* to every rival.
> The details below say why, and what that falsifies.

## Setup

| | |
|---|---|
| bot | `nutt-27td-fl-hu-h2`, version `1262e462`, digest `fe01c416f61e` (repo commit `7f03d95`) |
| queued via | `bin/arena compete --game 27td-fl --hands N --versions <h2>,<opp> --watch` |
| dealing | duplicate, 3 CPU cores, 5000ms decision limit (platform defaults) |
| opponents | pinned by digest — **identical digests to the 2026-08-30 h1 ladder**, so every delta below is real |

`faults: 0` in every match. `paul-sauron100-lite-1bit` was not retried
(engine-failed twice at 2 seats on 2026-08-30; no new version since).

## Results

h2's rate, with h1's 2026-08-30 number against the same digest beside it.

| match | opponent | hands | h2 BB/100 | CI95 | h1 was | delta |
|---|---|---|---|---|---|---|
| 117 | `autofold` | 600 | `+24.00` | `±2.05` | `+18.75` | `+5.3` |
| 118 | `autocall` | 600 | `+105.04` | `±10.29` | `+92.62` | `+12.4` |
| 119 | `autopush` | 600 | `+156.79` | `±39.76` | `+185.62` | `−28.8` (CIs overlap) |
| 120 | `rand/30/30/40` | 2,000 | `+56.24` | `±6.91` | `+39.94` | `+16.3` |
| 121 | `rand/50/30/20` | 2,000 | `+100.41` | `±11.80` | `+100.58` | `0.0` |
| 122 | `rand/30/50/20` | 2,000 | `+92.92` | `±8.79` | `+63.14` | `+29.8` |
| 123 | `rand/10/70/20` | 2,000 | `+72.86` | `±6.16` | `+59.94` | `+12.9` |
| 116 | `swit-27td-1.0` | 10,000 | **`−20.83`** | `±3.63` | `−12.12` | **`−8.7`** |
| 124 | `swit-27td-5.1-i048` | 50,004 | `−16.23` | `±1.61` | `−15.64` | `−0.6` (CIs overlap) |
| 125 | `paul-gandalf200-4bit` | 300,000 | **`−17.23`** | `±0.66` | `−12.42` | **`−4.8`** |

The h2 target set before the change — beat `swit-27td-1.0` at 10,000
hands — was **missed in the wrong direction**: the gap widened from 12 to
21 BB/100, CIs well apart.

## Why: the leak moved, it did not close

The per-seat stats tell one consistent story across all three rival
matches (h2: vpip 0.76–0.77, fold 0.46–0.48, wtsd 0.31–0.34, wsd
0.56–0.57):

- **h2 wins the showdowns and loses the money.** wsd 55–57% against every
  rival — better than the rivals' own 44–50% — yet the rates are deeply
  negative. The losses live in pots that never reach showdown.
- **h1's cheap folds became h2's expensive folds.** h1 folded 57–62% of
  hands, mostly predraw for free or for the blind. h2 defends 100% of big
  blinds (per its chart), then runs into h1's unchanged postdraw
  continuing rules against opponents who bet relentlessly
  (`swit-27td-1.0`: pfr 53%, fold 21%) — so it pays one small bet
  predraw and folds mid-hand 46–48% of the time. Call-then-fold is
  strictly worse than fold.
- **The local judge was the flaw.** The [selection sweep](2026-09-01-h2-selection.md)
  picked defend-100% because it beat h1 by the most — but h1 bets only
  with value, so a trash defend against h1 gets a free showdown. Against
  an aggressor the same defend faces bets on every street and h2's tight
  continuing rules surrender. **Optimizing against a passive sparring
  partner selected exactly the trait a real opponent punishes.**

What is *not* falsified: the equity-ranking machinery itself. Every
baseline rung improved (the rand rungs by +13 to +30), meaning the
generated ranking orders hands well; and vs `autofold` the wider opens
collected the predicted extra steal EV. The failure is confined to the
**cut selection** — specifically pricing the big-blind defend off
check-down equity with a judge that never applies pressure.

## What this dictates for h3

1. **Split the realization factor.** One r for opens (position, initiative)
   and a much harsher one for defends (out of position, no initiative,
   facing three streets of pressure). The defend-64% candidate (r=0.55)
   is already generated and likely closer to right on that side.
2. **Fix the judge before re-tuning.** Candidate selection must include an
   aggressive opponent. Locally that means h2-vs-candidate and a
   pressure probe (a scripted relentless bettor), not h1 alone — h1 is
   now known to reward loose defends.
3. **Defend and continue must be priced together.** A predraw defend is
   only worth its price if the postdraw rules can continue often enough
   behind it. Either widen `continues()` for defended hands with any low
   draw, or narrow the defend to hands the postdraw rules will fight for.
4. h1 remains the better hosted bot. **Do not race h2**; it exists as a
   measured data point. Per `docs/naming.md` the next attempt is a new
   name.

The one-line lesson, worth the upload it cost: **local head-to-head
against your own previous bot is not a proxy for the field — it selects
for exploiting that bot's specific passivity.** The next selection loop
needs an adversarial judge before it needs a better model.
