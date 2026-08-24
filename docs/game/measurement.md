# Measuring a result on this platform

> Written 2026-08-22, trimmed 2026-08-24. Mechanics cite
> [`../arena/http-api.md`](../arena/http-api.md) and the vendored
> [`../protocol/WIRE_PROTOCOL.md`](../protocol/WIRE_PROTOCOL.md). The sample-size
> figures are derived from **one** observed match on the live platform; the
> derivation is shown below so it can be rechecked or replaced.

How to read a match report without fooling yourself. The frontier bots on this
platform sit inside `±0.44 BB/100` of each other, which is close to the
**measurement floor of the platform itself** — an edge smaller than that cannot be
established by a single competition, no matter how the bot is built.

## Duplicate dealing is already doing the variance reduction

`27td-fl` competitions run `duplicate: true`. Each deck is played `seat_count` times
with the bots rotated through the seats, so both sides play the same cards from both
positions and the paired difference cancels most of the deal luck.

Two consequences that are easy to get wrong:

- **`observations` counts rotation-sets, not hands.** A 300,000-hand heads-up match
  reports `confidenceSamples: 150000`. Confidence intervals are computed on paired
  differences, so the reduction is already baked into the reported CI. Do not apply
  your own variance correction on top.
- **`hands % players.length == 0` is enforced.** That is why real competitions use
  values like `100002` (divisible by 2, 3 and 6) rather than round numbers.

Seat rotation between hands happens even outside duplicate mode — the button is
always seat 0 and the *bots* move (`WIRE_PROTOCOL.md:145-150`).

## How many hands you actually need

**This whole section rests on a single observation.** From match 14
(`paul-gandalf200-4bit` vs `swit-27td-5.1-i048`, read 2026-08-22): 150,000
observations, CI95 half-width `0.443 BB/100`. Inverting `hw = 1.96σ/√n` gives
**σ ≈ 87.54 BB/100 per observation** for a frontier-versus-frontier matchup. Both
tables below are that one number pushed through `n = (1.96σ/e)²`, so the 300,000-hand
row reproduces its input rather than predicting anything. Re-derive σ from a fresh
match before leaning on the small-`e` rows.

| true edge to resolve | observations | hands |
|---|---|---|
| 10 BB/100 | 295 | 590 |
| 5 BB/100 | 1,178 | 2,356 |
| 2 BB/100 | 7,360 | 14,720 |
| 1 BB/100 | 29,438 | 58,876 |
| 0.5 BB/100 | 117,750 | 235,500 |
| 0.43 BB/100 | 159,207 | **318,414 — exceeds the 300,000 cap** |

Read the other way, for the competition sizes actually offered:

| hands | observations | CI95 |
|---|---|---|
| 2,000 | 1,000 | `± 5.43` |
| 10,000 | 5,000 | `± 2.43` |
| 50,004 | 25,002 | `± 1.09` |
| 100,002 | 50,001 | `± 0.77` |
| 300,000 | 150,000 | `± 0.44` |

**An edge below roughly 0.43 BB/100 cannot be established in a single competition**,
because `hands` is capped at 300,000. `paul-gandalf200-4bit`'s `+0.43 ± 0.44` sits
exactly at that floor: the frontier is tied *at the limit of what the platform can
measure*. Separating bots down there needs repeated competitions, not a bigger one.

**σ is matchup-dependent.** Against the retired, loose `nutt-27td-fl`, match 74
implied σ ≈ 220 BB/100 — two and a half times noisier, because a wild opponent
creates big pots. Tight-versus-tight is the low-variance regime. Use 87.54 for
frontier comparisons and expect worse against anything loose. (Both σ figures come
from live match reports read 2026-08-22 and are not reproduced by anything in this
repo.)

## The opponent ladder

Roster and artifact sizes observed 2026-08-22; rivals upload new versions, so
re-read `arena bots --all --game 27td-fl` before relying on a rung. Ordered by what
each one actually tests. Sizes are the hands needed to resolve a meaningful
difference at that rung, per the table above.

| rung | opponent | tests | hands |
|---|---|---|---|
| 0 | `autofold`, `autocall`, `autopush` | legality, gross blunders | 600 |
| 1 | `rand/30/30/40` and siblings | basic hand ranking | 2,000 |
| 2 | `swit-27td-1.0` (1.4 MB) | competent play | 10,000 |
| 3 | `swit-27td-5.1-i048` (22.7 MB) | a tuned blueprint | 50,000 |
| 4 | `paul-gandalf200-4bit` (83 MB) | the heads-up frontier | 300,000 |

`builtin:random` is family-aware — it makes legal draw decisions, not just legal
wagers — and is what hosted smoke validation plays against. Note that **validation
does not measure strategy quality**: a legal bot that plays terribly still validates.

Locally, `make spar` runs against `builtin:random` on the real engine. Use
`builtin:random:1` style seeds for reproducibility, and `--log` to capture the
unredacted event stream.

## Correctness before strength

`faults` is the gate, and it is binary. A fault is an illegal action, a malformed
message, a timeout, or a disconnect; the default `substitute` policy silently
replaces the decision with the minimal legal one — **a stand pat at a draw** — logs
it, and continues. So a faulting bot does not crash, it just quietly plays worse.
`faults: 0` must be confirmed before any result is interpreted at all.

Also worth watching: the `decisions` histogram (`meanMicros`, `p99Micros`,
`maxMicros`). Match 14 telemetry for `paul-gandalf200-4bit`, read 2026-08-22:
`count 1,582,032`, `meanMicros 25`, `p50 22`, `p90 40`, `p99 56`, `maxMicros 298`.
So the frontier decides in **25µs mean / 298µs max**. Drift there is an early warning
that a design has stopped being a table lookup — which matters because the nominal
`decisionTimeoutMs` of 5000 is not the real budget: a competition also ends at
10 minutes of wall clock, and a 300,000-hand heads-up match is ~1.58M decisions
inside 600s, or about 380µs each.

## Method notes

- Compare against a **fixed** opponent set. Rivals upload new versions, and
  competitions pin an immutable digest, so a result is only comparable to another
  result against the same digest.
- Re-uploading under an existing bot name appends a version rather than replacing it,
  so version history is the platform's job — do not build local tracking
  ([`../../CLAUDE.md`](../../CLAUDE.md)).
- A competition ends at its hand count **or 10 minutes, whichever comes first**. A
  slow bot yields a partial match with `failureCode: "match-timeout"` — check for it
  before reading a rate.
