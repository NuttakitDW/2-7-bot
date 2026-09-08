# Benchmark: `2-7-azurite-1` vs swit and paul (heads-up)

> Run 2026-09-08 on the live platform. A rendered spec-and-build summary
> of this generation is published at
> <https://claude.ai/code/artifact/315b9fe2-b106-41fb-87c8-6534d103d61f>. Units are the platform's big bets
> per 100 hands. Read with [`../measurement.md`](../measurement.md).
>
> **Verdict up front: azurite is within 2 BB/100 of both solver bots, and
> it is the first generation selected by a local exploitability number
> rather than by a hosted match.** Against the two rivals h3 was measured
> on, the gap closes from `−9.75` and `−12.19` to `−1.65` and `−1.72`.
> The result carries one unresolved anomaly, recorded in full below: an
> earlier, less-trained checkpoint scores *better* on exploitability than
> the one that shipped.

## What azurite is

An MCCFR blueprint is the bot. There is no chart and no hand-written
betting logic in the decision path; the heuristic survives only as the
fallback for sets the blueprint never visited.

| | |
|---|---|
| bot | `2-7-azurite-1`, version `648933a4-1ffd-49df-ad88-ca2cc2bf5c5d`, digest `805cd4ecd25f` |
| repo | commit `b42b72b`, branch `azurite/2-7-azurite` |
| artifact | 111.4 MiB static ELF (`116,797,624` bytes), `linux-x86_64-static` |
| uploaded | 2026-09-08 05:14; hosted validation passed on the first attempt |
| abstraction | equity buckets, `160,160,160` (`AZURITE_BUCKETS`) |
| tables | `165,319,791` bet slots, `6,312,384` draw slots |
| training | external-sampling MCCFR, regret-matching+, `348,984,983` iterations over 8h45m on 14 workers, seed 7 |
| extraction | visit floor 1, chosen as the lowest floor fitting the size budget |
| selection | purification floor `0.30`, sampled (not greedy), fixed-card slices **off** |
| fallback | onyx lines, including snows and mixes, on `0.1%` of reads |

Purification matters more than it looks. An average strategy that has not
converged carries residual probability on actions it has all but
abandoned; playing those costs real chips. Dropping every action under
`0.30` and renormalising is what turned an early azurite build that lost
to onyx into one that beats it by 15 BB/100.

## Hosted results

Queued with `bin/arena compete --game 27td-fl --hands 30000 --cores 8`.
Duplicate dealing, 5000ms decision limit. **`faults: 0` for both seats in
all nine completed matches**, no terminal reasons, our worst decision in
any match was 11.05ms against a 5000ms limit.

Three runs per opponent, because the arena's constant big-blind card
([`arena-fixed BB card`](2026-09-01-h3-hosted-ladder.md)) differs per
match and one match reads one card.

| opponent | 379–382 | 383–386 | 387–390 | pooled (90,000 hands) | h3 was |
|---|---|---|---|---|---|
| `swit-27td-5.1-i048` | `−1.91` | `−1.39` | `−1.66` | **`−1.65 ±0.97`** | `−9.75` |
| `paul-gandalf200-4bit` | `−1.01` | `−1.13` | `−3.02` | **`−1.72 ±0.95`** | `−12.19` |
| `swit-27td-1.0` | `+2.48` | `+3.97` | `+3.39` | **`+3.28 ±1.12`** | `−10.81` |

Pooled half-widths are the mean per-match half-width over `√3`. Both
losses clear their pooled interval, so they are real and small; azurite is
near parity, not at parity. The win over `swit-27td-1.0` is the first
positive result any of our generations has posted against a rival bot.

`paul-sauron100-lite-1bit` failed three times with `engine-failed` at hand
0, no hands played and no decision requested from our seat. The same
digest `e568bc040771` engine-failed at 2 seats on 2026-08-30 and again on
2026-09-01. It is an opponent-side fault and was not attempted a fourth
time.

Style, against the two solver bots, is unremarkable and symmetric —
`vpip` 74–83%, `pfr` 48–60%, `wtsd` 37–48%, `w$sd` 48.6–50.1%. Azurite is
consistently the more aggressive seat (`af` 1.52–1.55 against 1.23–1.26)
and holds showdown equity, which is the shape a blueprint should have.

## Local numbers, same build

`spar.sh`, 5 seeds × 50,000 duplicate hands each, `faults: 0` throughout.

| opponent | seeds | mean |
|---|---|---|
| `2-7-onyx-156` | `+13.04 +16.73 +18.46 +13.77 +18.89` | **`+15.53`** |
| onyx-model (swit mirror) | `+4.03 +5.44 +6.66 +3.52 +4.39` | **`+4.81`** |

Every seed is positive against both. The mirror is the harder gate and the
one that was previously negative at 25M iterations.

## The exploitability ladder

`cfrgen exploit` computes a best response over the abstract game and
reports the responder's value per hand, which at 50/100 blinds is
numerically BB/100. Lower is better. All figures at purify `0.30`,
`-fallback h3`, same evaluator binary.

| strategy | exploitability | button | big blind |
|---|---|---|---|
| always fold | `100.00` | — | — |
| uniform random | `~490` | `625` | `355` |
| shipped lapis blueprint | `673` | — | — |
| `2-7-cobalt-1` (heuristic) | `196` | — | — |
| `nutt-27td-fl-hu-h3` (heuristic) | `119` | — | — |
| legacy buckets, 5M iterations | `93.2` | — | — |
| equity 160, 5M | `90.4` | — | — |
| equity 160, 10M | `80.3` | — | — |
| equity 160, 25M | **`61.6`** | `70.8` | `52.3` |
| equity 160, 349M — **shipped** | `71.0` | `85.5` | `56.6` |

The lapis row is why this evaluator was built. That bot was trained as a
targeted exploit of cobalt with 24% of its reads untrained, and it scores
worse than playing uniformly at random. No hosted match ever said so.

## The anomaly: more training scored worse

The 25M checkpoint scores `61.6` and the 349M checkpoint that shipped
scores `71.0`, under identical flags and the same evaluator binary. One of
the acceptance tests for the evaluator was that longer training never
scores worse, and at this scale it did.

What has been ruled out:

- **Not the fallback.** Re-scored both checkpoints with `-fallback h3` and
  with no fallback at all. The four numbers are `61.57 / 61.57` and
  `71.04 / 71.04` — identical to four significant figures, because
  untrained reads are 0.6% and 0.1% respectively and too rare to move it.
- **Not the extraction floor.** Both were extracted at visit floor 1.
- **Not measurement noise.** The evaluator is deterministic; repeated runs
  return the same number bit for bit.

The leading hypothesis is abstraction pathology. The best response is
computed at full hand-class resolution — 7,462 live classes — while the
blueprint plays a 160-bucket-per-street abstraction. Converging harder
toward the abstract game's equilibrium can increase exploitability
measured at a finer resolution than the abstraction can express; this is a
known effect in abstracted CFR, not a bug in the walk. The split supports
it: the button best response moved `70.8 → 85.5` while the big blind moved
only `52.3 → 56.6`, and the button is the seat with more strategic
resolution to lose.

**This is a hypothesis, not a finding.** It is not tested. The decisive
experiment is to race the 25M build hosted against the same three
opponents and see whether the exploitability ordering predicts the hosted
ordering. Until that is run, the exploitability number is validated as a
*coarse* filter — it correctly separated 655 from 90 from 71 — and not yet
as a *fine* one.

The shipped build is the 349M one. It was chosen before the anomaly was
noticed, and the hosted results are the ones above, so nothing here is
retracted; but the 25M checkpoint may be the better bot and has not been
raced.

## What is not in this build

- **The fixed-card button slices.** `AZURITE_FIXED=none`. The slices are
  implemented and tested (`fixed_layout_test.go`) but were not enabled,
  so the constant big-blind-card edge is trained and unused. That edge is
  orthogonal to everything measured here and has never been raced.
- **Convergence.** Exploitability was still moving at 349M iterations.
- **Six-handed.** Declared at 2 seats only.

## Reproducing

```sh
make cfrgen-azurite                                    # trainer + evaluator
bin/cfrgen-azurite train -iters 400000000 -workers 14 \
  -seed 7 -minvisits 1 -state az.state -out az.bin.gz -every 30m
bin/cfrgen-azurite exploit -bp az.bin.gz -purify 0.30 -fallback h3
make bot-azurite-release AZURITE_BLUEPRINT=az.bin.gz AZURITE_PURIFY=0.30
```

The build flags must match between trainer, evaluator and bot: all three
read `handProfile`, `equityProfile` and `fixedProfile` through
`-ldflags -X`, and a mismatch decodes a blueprint against the wrong
layout. The Makefile's `AZURITE_*` variables are the single source.
