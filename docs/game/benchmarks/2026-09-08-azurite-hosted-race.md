# Azurite bot specification and benchmark

> Hosted run: 2026-09-08. Implementation review: 2026-09-08 at repository
> commit `534b325`; the shipped build is recorded separately below.
> This document explains the heads-up `27td-fl` bot, its offline training,
> runtime decisions, implementation steps, and recorded benchmark evidence.
> Hosted rates retain the original report's `BB/100` labels. The local
> exploitability tool uses big blinds per 100 hands; do not assume that a
> platform big-bet rate has the same scale. Read with
> [`../measurement.md`](../measurement.md).
>
> **Verdict up front: azurite's recorded mean is within 2 BB/100 of both solver bots, and
> it is the first generation selected by a local exploitability number
> rather than by a hosted match.** Against the two rivals h3 was measured
> on, the gap closes from `−9.75` and `−12.19` to `−1.65` and `−1.72`.
> The result carries one unresolved anomaly, recorded in full below: an
> earlier, less-trained checkpoint scores *better* on exploitability than
> the one that shipped.

## What azurite is

An MCCFR blueprint supplies the betting and draw action probabilities.
Successful blueprint lookups bypass heuristic betting rules. Hand-written
structure still defines the available draw candidates, and Onyx handles
missing lookups, unusable purified mixtures, and event-tracking failures.
There is no online CFR training in the Azurite runtime.

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
| fallback | Onyx lines, including snows and mixes; `0.1%` is the recorded evaluator untrained-read rate, not a hosted runtime measurement |

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

Pooled half-widths were reported as the mean per-match half-width over
`√3`. These are approximate summaries, not a recomputation from raw paired
observations; they also do not establish uncertainty across all fixed-card
conditions. Both reported loss intervals exclude zero under that method.
The win over `swit-27td-1.0` is the first
positive result any of our generations has posted against a rival bot.

`paul-sauron100-lite-1bit` failed three times with `engine-failed` at hand
0, no hands played and no decision requested from our seat. The same
digest `e568bc040771` engine-failed at 2 seats on 2026-08-30 and again on
2026-09-01. A fourth attempt was not made.
These failures do not measure Azurite's strength; the original report attributed
them to the opponent, but hand-zero failures alone do not isolate the cause.

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
blueprint plays a 160-bucket-per-street abstraction. Further training may favor the coarse strategy space while worsening its
score against a finer-resolution response. The observed split is
`70.8 → 85.5` for the button and `52.3 → 56.6` for the big blind.
That split alone does not establish the mechanism or rule out an
implementation issue.

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
  implemented and tested (`fixed_layout_test.go`) but were not enabled
  in this build. Their hosted benefit has not been measured here.
- **Convergence.** Exploitability was still moving at 349M iterations.
- **Six-handed.** Declared at 2 seats only.

## How the bot works

### Game and runtime contract

Azurite targets two-seat deuce-to-seven triple draw fixed limit. Each player
holds five cards, with three drawing rounds and four betting rounds. Aces
are high; straights and flushes count against a low hand. The best hand is
7-5-4-3-2 without a flush. The public tree uses blinds 50/100, bets of 100
before and after the first draw, bets of 200 after the second and third
draws, and the engine's four-wager cap.

The arena starts one static Linux amd64 executable. It exchanges compact
JSON Lines through stdin and stdout, without sockets or sidecar strategy
files. A `hello` receives `join`; `hand-start` resets state; `event` updates
the table and strategy tracker; `act` receives an action. Every response
flushes immediately. Diagnostics go to stderr, and `match-end` or EOF
ends the process. Unknown message types are ignored.

### Offline training to runtime action

```text
Game tree + hand abstraction + draw candidates
                    |
         MCCFR self-play training
                    |
   Average strategy -> byte probabilities -> gzip
                    |
       Go embed overlay -> static executable
                    |
Arena event -> table and public-tree tracker -> lookup
                    |
         Purify -> sample -> legalize -> reply
                    |
     Missing or unusable lookup -> Onyx fallback
```

On startup, `lapis.New` reconstructs the tree and abstraction, decodes the
embedded blueprint, and checks its table lengths. `cmd/bot/player.go`
selects the `azurite` profile, creates Onyx as the fallback, and forwards
events to both bots so the fallback has current state. Azurite bypasses
the modeled betting overlays and the `learned-bayes` river override.

For each decision, the tracker locates the public node and constructs a
view from the sorted private hand, seat, draw history, and last aggressor.
A betting lookup returns fold, pass, or aggression; pass becomes check or
call, and aggression becomes the legal bet or raise. A draw lookup returns
a keep mask over the sorted five cards; its complement becomes discards.
`wire.Legalize` clamps successful proposals to the engine's offered actions.
Onyx legalizes its own fallback decisions. An unexpected event can mark
the tracker lost for the rest of the hand, so fallback monitoring must
distinguish missing training from tracking errors.

### Information sets and draw choices

An information set is the group of situations that shares an action
distribution. Predraw betting uses the exact rank-multiset and flushness
class. Postdraw betting uses equity buckets: the requested `160,160,160`
counts apply to the three betting streets after draws, not to predraw.
Actual counts can differ slightly because of group allocation and ties.

Equity here is a ranking against a uniformly dealt, static opponent.
It rolls future draws backward through candidate transitions; it is not
an opponent-specific showdown probability. Before cutting quantile buckets,
the builder separates hands by the clipped draw count of their best keep.
It balances class count and deal weight to preserve resolution for rare
strong hands.

The default `history` layout keys bets by public betting node, draw-count
context, and hand bucket. Counts of three or more share one category.
Draw lookups use street, seat, last-aggressor relationship, draw context,
and distinct-rank set plus flushness. They retain less public history than
betting lookups. This information loss matters when interpreting convergence.

`policy.DrawCandidates` generates at most six keeps in deterministic order:
stand pat, the structural keep, and selected shorter or low-card alternatives.
Drawing five is included when the structural keep is empty. The blueprint
learns among those candidates, not all 32 possible discard masks. Stand pat
is available even with a weak hand, allowing a snow to emerge from training.
Changing candidate order requires regenerating compatible strategy data.

### Training and blueprint format

External-sampling Monte Carlo counterfactual regret minimization (MCCFR)
deals one hand per iteration and walks it for each traverser seat. It
explores every available action at the traverser's decisions and samples
the opponent and chance events. Regret-matching+ clips updated cumulative
regrets at zero; the trainer also accumulates a linearly weighted average
strategy. With no opponent model and weight zero, training is self-play.

Workers share tables under short information-set locks. Parallel scheduling
makes a 14-worker run non-bit-reproducible even with seed 7. Use one worker
for a serial reproducibility experiment. At the recorded slot counts, the
two float64 arrays and one uint32 array per slot require about 3.20 GiB
for primary training tables alone. Checkpoints, extraction, the game tree,
and evaluator allocations require additional memory.

Extraction retains sets meeting the visit floor and normalizes each action
vector into bytes summing to 255. All-zero vectors represent missing sets.
The gzip payload begins with `27bp`, then two little-endian uint64 lengths,
then the betting and draw byte arrays. The recorded arrays contain
171,632,175 bytes before compression and header overhead; this is not
the compressed ELF size or total process memory.

The decoder checks magic and array lengths, but the file does not carry a
complete semantic fingerprint of bucket assignments or candidate order.
Matching dimensions alone cannot prove compatibility. Preserve the source
revision, layout, abstraction flags, draw generator, and checkpoint identity.

### Purification and fallback behavior

The shipped selection is sampled with `AZURITE_GREEDY=false` and
`AZURITE_PURIFY=0.30`. The implementation computes a byte threshold
`uint8(0.30 * 255) = 76`, keeps weights at least 76, and samples from their
remaining sum. Thus the effective cutoff is 76/255, about 29.8%.
For example, weights `[26, 89, 140]` become probabilities `[0, 89/229,
140/229]`. If no weight survives, the lookup falls back instead of choosing
the largest action automatically. Greedy selection is a different policy.

Purification changes the trained average strategy. Its observed local
benefit is an empirical result, not an equilibrium guarantee. The evaluator
command below uses h3 fallback; the deployed runtime uses Onyx. The reported
0.1% untrained-read figure does not establish an identical hosted fallback
frequency or an exact evaluation of the deployed composite policy.

## How to implement and reproduce

### Prepare the source and toolchain

Use Go compatible with `go.mod` (`1.24.4`), Make, and Python 3 for the
Makefile's embed-overlay helper. Rust and Git are also needed to build the
pinned local engine through `make engine`. The shipped record names commit
`b42b72b`; this implementation guide was checked at `534b325`. A fresh
training run at the latter revision is a new experiment, not the old digest.

Keep all generated checkpoints, executables, and exports under ignored
`bin/`. The repository ignores `internal/lapis/blueprint.bin.gz` too:
ordinary Go tests/builds importing `lapis` need that embedded file to exist.
The Azurite build targets substitute a supplied blueprint with a Go overlay.

### Build and train with explicit settings

Run from the repository root. These commands describe a new training run;
they were not executed to regenerate the historical benchmark.

```sh
mkdir -p bin
export AZURITE_BUCKETS=160,160,160
export AZURITE_FIXED=none
export AZURITE_PURIFY=0.30
export AZURITE_GREEDY=false
export AZURITE_BLUEPRINT=bin/azurite.bin.gz

make cfrgen-azurite
bin/cfrgen-azurite train -iters 400000000 -workers 14 \
  -seed 7 -regret plus -minvisits 1 \
  -state bin/azurite.state -out "$AZURITE_BLUEPRINT" -every 30m
bin/cfrgen-azurite stats -bp "$AZURITE_BLUEPRINT"
bin/cfrgen-azurite exploit -bp "$AZURITE_BLUEPRINT" \
  -purify 0.30 -fallback h3
```

The 400M target is a training budget, not the shipped 348,984,983-iteration
checkpoint. Periodic saves preserve both raw training state and an extracted
blueprint. Resume with `train -resume bin/azurite.state` plus the same
settings; `-iters` means additional iterations on resume. Keep the same
visit floor when extracting checkpoints for comparison.

**Default mismatch:** `AZURITE_PURIFY` defaults to `0.05`, and
`make exploit-azurite` currently hard-codes `-purify 0.05` even when the
variable is overridden. Use the explicit evaluator invocation above for
the shipped selection. These docs do not change Makefile behavior.

### Build and validate the bot

```sh
make bot-azurite
make bot-azurite-release
make engine
make spar BOT=./bin/bot-azurite HANDS=10000
go run ./cmd/arena upload --games 27td-fl --counts 2 \
  --file bin/2-7-azurite-1 --dry-run
```

The exported settings above must remain active for both bot builds. The
host build is for local sparring; the release target emits a stripped,
static Linux amd64 executable using `CGO_ENABLED=0` and an embed overlay.
Require zero protocol faults and inspect decision latency. For the stricter
engine check, add `--fault-policy forfeit` to the direct engine invocation
shown by `make -n spar BOT=./bin/bot-azurite HANDS=10000`.

Run existing tests for hand ranking, draw-candidate stability, layout and
blueprint compatibility, tracker behavior, legality, and exploitability.
Use the matching abstraction flags and blueprint overlay when testing a
new Azurite checkpoint; a plain default-profile test does not prove an
equity-profile checkpoint is compatible.

Before an actual upload, select a fresh bot name for any changed raceable
build (`AZURITE_BOT_NAME`), rebuild, and repeat the dry run with that filename.
The historical `2-7-azurite-1` identity is already taken. Hosted validation
checks legality, not strength. Strength testing should retain opponent
version IDs, checkpoint/digest, seeds, match IDs, fault counts, latency,
raw reports, and rate units. Use repeated duplicate-dealt matches against
both previous bots and pressure opponents; compare 25M and 349M checkpoints
under identical selection settings before claiming the anomaly is resolved.

### Implementation map

| Source | Responsibility |
|---|---|
| `cmd/bot/main.go`, `cmd/bot/player.go` | JSONL dispatch and Azurite profile wiring |
| `internal/lapis/lapis.go` | Embedded blueprint, event tracker, lookup and fallback |
| `internal/cfr/tree.go`, `layout.go` | Public action tree and information-set addressing |
| `internal/cfr/abstract.go`, `equity.go` | Hand classes and postdraw equity buckets |
| `internal/policy/drawcand.go` | Shared deterministic candidate keeps |
| `internal/cfr/mccfr.go`, `checkpoint.go` | Training, averaging and resumable state |
| `internal/cfr/blueprint.go` | Extraction, format, purification and sampling |
| `internal/cfr/exploit.go`, `transition.go` | Approximate best response and draw transitions |
| `internal/onyx/`, `internal/wire/` | Fallback policy and legal action handling |
| `cmd/cfrgen/`, `Makefile` | Trainer/evaluator commands and release settings |

## Evidence limits and next experiment

The hosted and local figures above are preserved from the dated benchmark;
this documentation update does not add new matches or training measurements.
The exploitability walker uses 7,462 live classes, independent seat deals
without cross-seat or discard card removal, restricted candidate draws,
and clipped draw counts by default. `-exact` retains the exploited seat's
exact counts at additional cost. Its score is an approximate-game best
response metric, not a certified exploitability bound for full poker.

The CLI reports mean responder chips per hand. With a 100-chip big blind,
that number equals big blinds per 100 hands; in 200-chip big bets per 100
hands it would be half as large. Preserve raw platform `rateUnit` metadata
before comparing that metric to hosted rates. The table values here are
not rescaled in the absence of their original raw reports.

The original statement that more training should never score worse is not
a general acceptance criterion for this abstracted, sampled, purified
strategy. Determinism rules out repeated-evaluation sampling noise, not
training variation or model bias. The proposed abstraction explanation
remains untested. The next useful experiment is a controlled checkpoint
comparison with equal flags, matched opponents and repeated hosted runs.
