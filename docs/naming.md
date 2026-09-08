# Bot naming and versioning

> Hand-maintained, like [`arena/`](arena/) and [`game/`](game/) and unlike
> [`protocol/`](protocol/) — `scripts/sync-docs.sh` never touches this file. This
> is our convention, not the platform's; the platform imposes no naming rule at
> all. `internal/arena/name.go` enforces everything below, and
> `arena upload` refuses a name that breaks it.

## Why the name carries the version

An upload declares four fields and no more —
`{"name","games","playerCounts","size"}` ([`arena/http-api.md`](arena/http-api.md)).
There is no version label, no notes, no tag. The platform assigns the version
itself: immutable, digest-addressed, newest first. That is deliberate and we do not
work around it.

But match results identify a seat by **name only**. `stats[].name` and
`players[]` carry the bot name snapshot, never the version id, so two versions of
one bot in the same competition are indistinguishable in `arena match <id>`.

So the generation has to live in the name. Every account on the roster reached the
same conclusion: `swit-27td-5.1-i048`, `swit-27td-ring1/2/3`,
`paul-gandalf200-4bit`, `paul-sauron100-{exploit,neutral,lite}-1bit`.

## The grammar

```
nutt-<gameId>-<seats>-<lineage><gen>[-<qualifier>]...
```

| segment | values | meaning |
|---|---|---|
| owner | `nutt` | fixed |
| gameId | a registry id — `27td-fl`, `badugi-fl` | one game per name; contains hyphens of its own |
| seats | `hu` `6max` `hu6` `all` | exactly what `--counts` declares |
| lineage | `h` `b` `x` | heuristic / blueprint (solved) / experiment |
| gen | `1`, `2`, … | no leading zero |
| qualifier | zero or more `[a-z0-9]+` | build knobs, in order: iterations, quantisation, role |

Whole name: lowercase `[a-z0-9-]`, no dots, no slashes, at most 40 characters. The
longest name on the roster is `paul-sauron100-exploit-1bit`, at 27.

Seat tokens stand for exact sets, because declared counts are exact — declaring 4
does not imply 3 ([`arena/hosted-bot-interface.md`](arena/hosted-bot-interface.md)):

| token | `--counts` | |
|---|---|---|
| `hu` | `2` | heads-up |
| `6max` | `6` | six-handed |
| `hu6` | `2,6` | the two tracks actually contested |
| `all` | `2,3,4,5,6` | every seat count the arena offers |

Qualifiers borrow the roster's vocabulary, so our names read alongside theirs:

- `i050`, `i200` — CFR iterations in millions, zero-padded to three (cf. `swit`'s `i048`)
- `1bit`, `4bit`, `8bit` — quantisation width of the compiled-in table (cf. `paul`)
- `exploit`, `neutral`, `lite` — role variants of one blueprint (cf. `paul-sauron100-*`)

```
nutt-27td-fl-hu-h1             first heuristic, heads-up
nutt-27td-fl-hu-h2             retuned heuristic — raceable against h1
nutt-27td-fl-hu-b1-i050        blueprint gen 1, 50M iterations
nutt-27td-fl-hu-b1-i200        same abstraction, more iterations
nutt-27td-fl-6max-b2-1bit      gen 2 abstraction, 1-bit, six-handed
nutt-27td-fl-hu-b2-i200-4bit   gen 2, 200M iterations, 4-bit
nutt-27td-fl-all-h1            declares 2,3,4,5,6
nutt-badugi-fl-hu-x1           an experiment on another game
```

`gen` versus qualifier is a readability split, not a policy one. Bump `gen` when two
builds are no longer comparable except by playing them; change a qualifier when the
same design was built with different knobs. Either way it is a new name.

## The codename grammar

Since 2026-09-05 a second, shorter grammar exists for the one game we
actually contest:

```
2-7-<codename>-<gen>
```

| segment | values | meaning |
|---|---|---|
| `2-7` | fixed | `27td-fl`, heads-up — the parser fills in `hu` and `27td-fl` |
| codename | one lowercase word | a strategy family, chosen fresh when the design changes |
| gen | `1`, `2`, … | raceable builds within the family |

```
2-7-cobalt-1     h3's chart and draws with added aggression
2-7-lapis-1      MCCFR blueprint, first build
```

Two rules. A codename names a *design*: retuning within it bumps `gen`, a
new architecture takes a new word. And a bot is never named after the agent
or branch that built it — `fable` is reserved by the parser, and the branch
`fable/2-7-fable` produced `2-7-lapis-1`, not `2-7-fable-1`.

## New name, or new version?

**A new raceable build always gets a new name.**

| situation | action |
|---|---|
| architecture, abstraction, quantisation, iteration count, seat set or tuning changed | new name |
| identical strategy, broken build — faulting artifact, wrong declared counts, bad build flags | `--append` a version to the same name |

The test is: *would I ever want to race the old build against the new one?* If yes it
needs its own name, because the match report only prints names. If the old build is
simply wrong and should stop existing, append over it.

`arena upload` refuses an existing name and suggests the next generation. `--append`
is the deliberate opt-in for the second row, and nothing else.

The platform's version history stays exactly as it is — immutable, digest-addressed,
and the only version axis. We keep no local version store.

## The artifact filename is the bot name

Build to `bin/<botname>`, with no extension:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o bin/nutt-27td-fl-hu-b1-i050 ./cmd/bot
arena upload --games 27td-fl --counts 2 --file bin/nutt-27td-fl-hu-b1-i050
```

`--name` defaults to the filename, so the binary that was built and the name it is
uploaded under cannot drift apart. Passing `--name` explicitly still works and is
cross-checked against `--games` and `--counts` either way.

## Retired names

`nutt-27td-m1` and `nutt-27td-fl` predate this convention. They keep their standings
and their snapshots in past competitions, and they are never uploaded to again —
`ParseBotName` rejects both by name. Reaching them would mean the platform's web UI
or editing the constant, which is the intended amount of friction.
