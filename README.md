# 2-7-bot

Harness for [MixedSolver Arena](https://arena.sorawit.dev) — the hosted poker
bot competition platform.

It holds three things: the contract the platform runs on, the tooling to drive
it — inspect bots, upload artifacts, queue competitions, read results, spar
locally against the real engine — and a bot that plays `27td-fl`.

## Where the contract lives

Everything a bot must obey is written down in `docs/`, so it never has to be
rediscovered:

| Path | What it is |
|---|---|
| `docs/protocol/WIRE_PROTOCOL.md` | The betting-game JSON-Lines protocol. **Vendored upstream — do not edit.** |
| `docs/protocol/WIRE_PROTOCOL_OFC.md` | The separate Open Face Chinese protocol. Vendored. |
| `docs/protocol/examples/` | Upstream's dependency-free reference bots. |
| `docs/arena/hosted-bot-interface.md` | Runtime contract, artifact rules, validation. |
| `docs/arena/http-api.md` | The platform's HTTP API, endpoint by endpoint. |
| `docs/arena/games.md` | Game registry and what is actually contested. |
| `docs/SOURCES.md` | Provenance, the pinned upstream commit, and update rules. |
| `docs/naming.md` | How we name and version our own bots. Ours, not the platform's. |
| `docs/game/rules.md` | The `27td-fl` rules, cited to the engine at the pinned SHA. |
| `docs/game/measurement.md` | How to read a match report without fooling yourself. |

`make docs-check` re-fetches the vendored files at the pinned commit and fails
on any drift. Only `docs/protocol/` is vendored; everything else is ours, and the
`docs/arena/` files in particular were derived by observing the live platform on
2026-08-22, so they can rot without any signal. `docs/SOURCES.md` records the
provenance of each.

## The short version of the contract

A bot is **one static Linux x86-64 ELF** (≤300 MiB), launched as a subprocess.
It reads JSON Lines from stdin, writes them to stdout, flushes after every
reply, logs only to stderr, and exits on `match-end` or EOF. No sockets, no sidecar
files, no scripts — strategy data is compiled in.

For Go that means `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build`.

At upload the artifact declares which games and which **exact** table sizes it
supports; the arena smoke-tests every declared combination before the version
becomes selectable. Validation checks legality, not strength.

## Setup

```sh
echo 'API_KEY=<your arena api key>' > .env   # gitignored
make arena                                   # the CLI, into bin/arena
```

The key comes from the arena's account page and is read from `API_KEY` in the
environment or from `.env`. Set `ARENA_BASE_URL` to target another host.

## Using the harness

```sh
go run ./cmd/arena help

go run ./cmd/arena health                    # service status + whoami
go run ./cmd/arena bots                      # your bots and their capabilities
go run ./cmd/arena bots --all --game 27td-fl # everyone's, filtered
go run ./cmd/arena versions <botId>          # immutable version history

go run ./cmd/arena matches --limit 10
go run ./cmd/arena match 86                  # per-seat stats; watch the FAULTS column
go run ./cmd/arena hands 86 --collection biggest
go run ./cmd/arena hand 86 1                 # one hand, event by event

go run ./cmd/arena competitions --game 27td-fl
go run ./cmd/arena compete --game 27td-fl --versions <id>,<id> --hands 1000 --watch
go run ./cmd/arena watch <competitionId>
```

Add `--json` for the raw document. It is available on the eight read commands
above — `health`, `bots`, `versions`, `matches`, `match`, `hands`, `hand`,
`competitions` — but not on `compete` or `watch`, which take no flags.

### Uploading

```sh
go run ./cmd/arena upload \
  --games 27td-fl --counts 2 \
  --file bin/nutt-27td-fl-hu-h1 --dry-run
```

`upload` inspects the artifact locally before sending anything — ELF or ZIP,
x86-64, statically linked, within the size cap — because a rejected artifact
costs a round trip and a validation slot. `--dry-run` stops after the plan;
`--force` overrides the pre-flight.

Bot names follow [`docs/naming.md`](docs/naming.md):
`nutt-<game>-<seats>-<lineage><gen>[-<qualifier>]`. `--name` defaults to the
artifact filename, and whichever way it arrives it is cross-checked against
`--games` and `--counts` before anything is sent.

**A new raceable build takes a new name.** Uploading under an existing name
appends a version instead of replacing one, which is right only when the old
build was broken — a match report identifies seats by name, so two versions of
one name cannot be told apart in results. That path needs `--append`. Version
history itself is the platform's, not ours: immutable, digest-addressed, and
snapshotted into every competition that used it.

## Sparring locally

```sh
make engine                                   # clone + build upstream at the pinned SHA
make spar                                     # bin/bot vs builtin:random
make spar HANDS=10000
make spar BOT='python3 docs/protocol/examples/bot.py'
```

`make spar` runs any executable against `builtin:random` through the upstream
CLI — the same engine contract production uses, with no hosted quota. Check
`faults: 0` in the JSON report before uploading anything.

For a sharper legality check, run the engine directly with
`--fault-policy forfeit`: instead of substituting a legal action and counting
the fault, the match ends and the offender loses.

## The bot

`nutt-27td-fl-hu-h1` — heads-up, heuristic, generation 1. It exists to be a
correct floor, not a contender: an actual hand ranking and an actual draw
rule, which is what both retired bots were missing. It does not bluff and does
not mix, deliberately.

```sh
make bot                                      # host build, for sparring
make bot-release                              # bin/nutt-27td-fl-hu-h1, static linux x86-64
```

Measured locally on 2026-08-22 over 10,000 duplicate-dealt decks per opponent,
`faults: 0` throughout under `--fault-policy forfeit`. Nothing in the repo
regenerates this table — rerun the spars to refresh it:

| opponent | BB/100 |
|---|---|
| `builtin:shover` | `+135.28 ± 6.60` |
| `docs/protocol/examples/bot.py` | `+104.79 ± 5.41` |
| `builtin:caller` | `+80.07 ± 1.76` |
| `builtin:random` | `+52.22 ± 2.38` |
| `builtin:folder` | `+18.98 ± 0.37` |

## Layout

```
cmd/arena/        the harness CLI
cmd/bot/          the bot: a stdio loop over the packages below
internal/arena/   REST client: bots, versions, uploads, competitions, matches
internal/cards/   the wire's card vocabulary
internal/deuce/   the 2-7 evaluator, in the engine's frozen encoding
internal/wire/    the JSON-Lines protocol, and the legality guard
internal/table/   hand state rebuilt from the event stream
internal/policy/  the strategy
docs/             the contract (see above)
scripts/          docs drift check, upstream engine bootstrap
third_party/      upstream engine checkout (gitignored)
```

## Development

```sh
make test         # go test ./...
make fmt vet
make docs-check
make arena bot    # bin/arena, bin/bot
```

`docs/game/` is the knowledge the wire protocol deliberately omits — the rules,
cited to the engine, and how to read a result. It is not a strategy guide: this
repo is a harness for building bots, and how a bot should play is the bot's
business, argued in code where it can be tested.

---

Copyright © 2026 Nuttakit Kundum.
