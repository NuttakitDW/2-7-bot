# Hosted bot interface (MixedSolver Arena)

> Derived from the platform's own "Hosted bot interface" guide at
> <https://arena.sorawit.dev/protocol/bots>, observed 2026-08-22. The *gameplay*
> contract is not here — it is upstream, in
> [`../protocol/WIRE_PROTOCOL.md`](../protocol/WIRE_PROTOCOL.md) (betting games)
> and [`../protocol/WIRE_PROTOCOL_OFC.md`](../protocol/WIRE_PROTOCOL_OFC.md)
> (Open Face Chinese). See [`../SOURCES.md`](../SOURCES.md).

Arena bots are standalone Linux executables launched by the upstream
`poker-arena` engine as subprocesses. **They receive no address argument and
open no network connection.**

## Runtime contract

- Read arena messages from **stdin**; write bot messages to **stdout**.
- Compact JSON Lines: one JSON object, then `\n`.
- **Flush stdout after every response.**
- Diagnostics go to **stderr only**.
- Reply before the per-action deadline.
- Exit after `match-end` or a clean EOF.
- Ignore unknown JSON fields, message tags, and event tags.
- Reply to `hello` with the bare `{"t":"join"}`. **Do not send a name** — Arena
  assigns competition identity and reports it back in `joined`.

The engine never trusts identity supplied by an executable: it assigns distinct
match-local seat names, which is why one immutable version can fill several
seats in a single competition without colliding with itself.

## Artifact contract

Upload **one** of:

- a static Linux **x86-64 ELF** executable, or
- a **ZIP** archive containing exactly one such executable.

| Rule | Value |
|---|---|
| Maximum upload size | 300 MiB (`300 * 1024 * 1024`) |
| Recorded target | `linux-x86_64-static` |
| Sandbox | Linux gVisor, artifact mounted **read-only** |

ZIP parsing rejects encrypted entries, multi-disk archives, directories, links,
multiple files, unsafe paths, and decompressed data beyond the executable limit.

**Explicitly unsupported:** dynamically linked executables, macOS Mach-O files,
scripts, shell bundles, model directories, and archives with auxiliary files.
*All required strategy data must be compiled into the executable.* There is no
sidecar file, no weights directory, no runtime download.

For a Go bot this means `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` — which produces
a static ELF by default and needs no special linker flags.

## Registration and validation

At upload the artifact declares:

- every **game** it claims to support, and
- every **exact table size** it handles: 2 (heads-up), 3, 4, 5, and/or 6.

The two are independent. **Support for four players does not imply support for
three.**

Arena then creates an immutable version identified by its SHA-256 digest and
runs a short **smoke match for every applicable declared game × table size**.
The candidate fills one seat; upstream family-aware random strategies fill the
rest. A declared count above a game's validated seat cap is simply inapplicable
to that game, but each declared game must still overlap at least one declared
table size.

**A version becomes selectable only after all smoke matches succeed.**
Validation checks that the process starts, completes the handshake, produces
parseable legal actions, stays within limits, and exits normally. **It does not
measure strategy quality** — a legal bot that plays terribly still validates.

On failure, the owner and administrators can expand a bounded diagnostic in the
bot inventory: a stable failure code plus a sanitized tail. Raw host paths and
unbounded process output are never exposed.

## Identity and versioning

The display name registered on the website is the public identity. Re-uploading
under an existing bot name **appends a new version** rather than replacing the
old one; `GET /api/bots/{id}/versions` lists the history, each entry pinned by
digest. Competitions retain their immutable version and a snapshot of the name,
so renaming a bot never rewrites history.

This is why the repo carries no versioning machinery of its own — the platform
already provides immutable, digest-addressed versions.

## Testing against the upstream engine

The engine and CLI live in <https://github.com/mixedsolver/poker-arena> and run
outside the hosted platform. Production uses the same engine contract inside an
isolated container, so a clean local match is real evidence.

```sh
poker-arena run \
  --game 27td-fl \
  --hands 100 \
  --bot 'my-bot@cmd:./my-bot' \
  --bot 'baseline@builtin:random:1' \
  --output json
```

`cmd:"COMMAND"` spawns the bot and speaks over its stdio; `tcp:PORT` listens on
127.0.0.1 instead. `NAME@spec` assigns the competition name.

**Note the asymmetry:** the upstream engine supports a TCP transport, but the
*hosted* platform does not — it only ever spawns a subprocess. A bot written for
the hosted arena needs stdio and nothing else.
