# 2-7-bot — agent context

Harness for **MixedSolver Arena** (https://arena.sorawit.dev), the hosted poker
bot competition platform. Target game: **`27td-fl`** (2-7 triple draw, fixed
limit).

**Current state: harness plus one bot.** `cmd/arena` drives the platform;
`cmd/bot` plays `27td-fl`. The bot is **`nutt-27td-fl-hu-h1`** — heads-up,
heuristic, generation 1 — and it is a deliberate floor rather than an attempt
at the frontier: a real evaluator, a real draw rule, no bluffing and no mixing.
See "The bot" below before changing it.

## The contract is already written down — read it, don't rediscover it

| Question | File |
|---|---|
| How does a bot talk to the engine? | `docs/protocol/WIRE_PROTOCOL.md` |
| Open Face Chinese (different protocol) | `docs/protocol/WIRE_PROTOCOL_OFC.md` |
| What may I upload, and how is it validated? | `docs/arena/hosted-bot-interface.md` |
| What are the HTTP endpoints and payloads? | `docs/arena/http-api.md` |
| Which games and table sizes exist? | `docs/arena/games.md` |
| Where did these documents come from? | `docs/SOURCES.md` |
| What do we call our bots, and when is it a new one? | `docs/naming.md` |
| What are the rules of `27td-fl`? | `docs/game/rules.md` |
| How do I read a match report without fooling myself? | `docs/game/measurement.md` |
| What has the bot been benchmarked against, and when? | `docs/game/benchmarks/` |

A working reference bot is vendored at `docs/protocol/examples/bot.py`.

## Hard rules

**Artifacts.** One static Linux x86-64 ELF (or a ZIP holding exactly one),
≤300 MiB. Build with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. Dynamic
binaries, Mach-O, scripts, shell wrappers and sidecar data files are all
rejected — strategy data must be compiled into the executable.

**Runtime.** stdio only. Never open a socket: the hosted platform spawns a
subprocess and passes no address. Compact JSON Lines, flush stdout after every
reply, diagnostics to stderr only, exit on `match-end` or EOF. Ignore unknown
fields and unknown message/event tags rather than erroring.

**Uploads.** Never retry a failed chunk inside a session — the server discards
interrupted streams, so resuming yields a corrupt artifact. Cancel with
`DELETE` and start over. One in-flight upload per account.

**Numbers.** Every `…Milli` (÷1000), `…Ppm` (÷1,000,000) and `…Micros` (µs)
field is a scaled integer. Always convert via `internal/arena/units.go`;
printing one raw is wrong by orders of magnitude.

**Vendored docs.** `docs/protocol/` mirrors upstream byte-for-byte. Never
hand-edit it. To update, in this order and in one commit: bump `ENGINE_SHA` in
the `Makefile`, run `./scripts/sync-docs.sh --update`, then refresh the digest
table in `docs/SOURCES.md`. The script reads the pin out of the `Makefile`, so
bumping it second makes the update a silent no-op.

**Secrets.** The arena API key lives in `.env` (gitignored) or `$API_KEY`.
Never commit it, never log it, never put it in a doc or test fixture.

## Workflow

```sh
make test          # go test ./...
make docs-check    # vendored docs still match the pinned upstream commit
make fmt vet       # go fmt / go vet
make arena         # the harness CLI, into bin/arena
make bot           # host build, into bin/bot
make bot-release   # the static linux x86-64 artifact, named for the bot
make spar          # run BOT (default ./bin/bot) against builtin:random
make engine        # bootstrap third_party/poker-arena at the pinned SHA
```

Two switches worth knowing. `--fault-policy forfeit` on a local
`poker-arena run` turns any illegal action into an immediate loss, which is a
far sharper legality gate than reading `faults` afterwards. And
`BOT_SPAR_LOG=<a --log file> go test ./internal/deuce/` replays every showdown
in that file against the evaluator — the engine publishes its own `HandValue`
for each shown hand, so any match log is a free correctness oracle.

## The bot

```
cmd/bot/          the stdio loop; main.go, ~50 lines of dispatch in run()
internal/cards/   Card, and the hand helpers everything else shares
internal/deuce/   the 2-7 evaluator, in the engine's frozen encoding
internal/wire/    the JSON-Lines protocol, plus Legalize
internal/table/   hand state rebuilt from the event stream
internal/policy/  the strategy: policy.go is the entry point, then
                  chart.go (ranges), draw.go (discards), bet.go (wagers)
```

- **The policy only proposes; `wire.Legalize` decides.** Every action passes
  through it, clamped against the arena's own `decision`. That is why
  `faults: 0` is a real gate: a strategy bug can make the bot play badly but
  not illegally, and the two stay distinguishable in a match report.
- **`internal/deuce` must match the engine bit for bit**, not merely agree on
  ordering. That is what makes `showdown-show.hi` an oracle. `make test` proves
  it exhaustively over all 2,598,960 five-card hands. It also matched 122,204
  showdowns in a local 300,000-hand spar log on 2026-08-22 — that log was never
  committed, so that half is a dated observation, not a check.
- **h1 does not bluff and does not mix.** Both omissions are deliberate and
  both are documented at the point they would go — snowing in `draw.go`,
  determinism in `chart.go`. Being deterministic is what makes h1 fully
  exploitable by anything that models it; that is the price of being legible.
- Retuning any threshold means a **new name**, not a new version
  (`docs/naming.md`).

Before any upload: `make spar` and confirm `faults: 0`, then
`arena upload --dry-run` to check the artifact and the chunk plan. Hosted
validation runs a smoke match per declared game × table size, and the version
is unusable until all pass — so a wasted upload costs real time.

## Decisions already made

- **Go**, for iteration speed with enough throughput for later self-play work.
- **Harness before bot** — the platform contract lands in the repo first.
- **Versioning is the platform's.** Re-uploading under an existing bot name
  appends an immutable, digest-addressed version. Do not build any local
  version-tracking machinery.
- **The generation lives in the name**, because an upload accepts no version
  label and a match report names seats but not versions. `docs/naming.md` has the
  grammar; `internal/arena/name.go` enforces it. A raceable build takes a new
  name — appending a version is for replacing a broken one and needs `--append`.
- Table sizes are declared per upload and are **exact** — 4 does not imply 3.

## Gotchas already hit

- Go's `flag` package stops parsing at the first positional argument, which
  silently ignores later flags. Use `parseFlags` in `cmd/arena/flags.go`.
- `GET /api/bots/{id}` returns **405**; version history is at
  `/api/bots/{id}/versions`.
- The API returns **two** error envelopes — `{code,message}` and `{error}`.
  Both are handled in `decodeAPIError`.
- `/api/progress` answers **204** when nothing changed. That is success.
- Hosted hand logs use the platform's own event vocabulary, **not** the
  upstream wire `Event` union. Do not share a decoder between them.
