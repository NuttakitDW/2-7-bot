# Sources and provenance

Everything under `docs/` is the contract this repo builds against. It has two
halves with different rules.

## `docs/protocol/` — vendored, do not edit

Byte-for-byte mirrors of the upstream engine's canonical specifications. The
arena links to these documents and explicitly states it "does not maintain
copies", so upstream is the only source of truth; these files exist here so the
contract is readable offline and so drift is detectable.

**Never hand-edit these files.** To update them, in this order and in one commit:
bump `ENGINE_SHA` in the `Makefile`, run `./scripts/sync-docs.sh --update`, then
refresh the digest table below. The order matters — the script reads the pin out of
the `Makefile` to build its fetch URL, so running it first re-fetches the commit you
already have and silently does nothing.

| Field | Value |
|---|---|
| Repository | https://github.com/mixedsolver/poker-arena |
| Pinned commit | `80c7eeb758b05fd957063330747c4f234f77a0f8` |
| Commit date | 2026-08-02 |
| Fetched | 2026-08-22 |
| License | See upstream `LICENSE` |

| File | SHA-256 |
|---|---|
| `protocol/WIRE_PROTOCOL.md` | `50386431b5959ecefc514ab3b44f2458af92211fbf3eba998891b4a6a6bc52e5` |
| `protocol/WIRE_PROTOCOL_OFC.md` | `8c49b909b773deda48295891e7c09fdfb213532fa76899105e55c050dfed97c2` |
| `protocol/examples/bot.py` | `03be37e50d9b84e049605cdaa1f522cc48f767194267d530ec0ce9e554ea3b86` |
| `protocol/examples/ofc_bot.py` | `cf1999f1076e5b32ef1eda881390278ac9c8e639f2658c6f99f6f35e67a5842a` |

`make docs-check` re-fetches at the pinned SHA and fails on any mismatch. It
also reports when upstream `master` has moved past the pin, which is a prompt to
review — not an automatic upgrade.

## `docs/arena/` — ours, maintained by hand

The hosted platform at https://arena.sorawit.dev publishes no machine-readable
specification of its own HTTP API, so these documents were derived from the
platform itself on **2026-08-22**:

- its `/protocol/bots` guide ("Hosted bot interface"),
- its single-page-app bundle, which carries the request shapes and the
  client-side validation limits,
- authenticated read-only calls against the live API with the account's key.

They describe the *hosting* layer — upload, validation, competitions, matches.
The *gameplay* contract is upstream's, in `docs/protocol/`. When the two
overlap, upstream wins.

Because these are observations rather than a published spec, they can go stale
without any signal. Re-verify against the live API before trusting a detail that
a change would silently break.

## `docs/game/` — ours, maintained by hand

Game knowledge, written **2026-08-22**, trimmed to two files on **2026-08-24**. The
wire protocol deliberately carries no rules — `WIRE_PROTOCOL.md:79-86` records the
decision that "the bot is expected to know the game" — so nothing in `docs/protocol/`
or `docs/arena/` states the deck size, the number of draws, or the hand ranking. This
directory is that gap filled in, and nothing more.

Two files, with different reliability:

- [`game/rules.md`](game/rules.md) is derived from the upstream engine source at the
  pinned `ENGINE_SHA`, checked out locally by `make engine` at
  `third_party/poker-arena/`. Every claim cites a `file:line` under
  `crates/poker-core/src/`. **The engine outranks any poker reference** — casino 2-7
  may differ from this implementation. Re-verify these citations after any
  `ENGINE_SHA` bump; `make docs-check` does not check them.
- [`game/measurement.md`](game/measurement.md) is how to read a match report. Its
  mechanics cite [`arena/http-api.md`](arena/http-api.md) and the vendored wire
  protocol; its *numbers* — σ, the opponent ladder, the timing histogram — come from
  authenticated read-only calls to `/api/bots`, `/api/competitions` and
  `/api/matches` on 2026-08-22, and go stale as rivals upload new versions. The file
  says so at each point of use.

**What is deliberately absent.** Strategy advice, solver roadmaps, abstraction
designs and a research bibliography lived here until 2026-08-24 and were removed:
none of it was checkable from this repo, most of it prescribed a bot nobody had
built, and the shipped bot cited it as authority while quietly disagreeing with it.
This is a harness for building bots, not a place to write down how a bot should
play. Recover it from history if a later generation needs it — `git log --diff-filter=D
-- docs/theory/`.

`scripts/sync-docs.sh` writes only the four explicit paths under `docs/protocol/`,
so this directory is never touched by a docs sync.
