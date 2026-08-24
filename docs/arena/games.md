# Game registry

> The hosted registry, read from the platform's SPA on 2026-08-22 and
> cross-checked against the upstream engine README at the pinned commit.
> See [`../SOURCES.md`](../SOURCES.md).

Twenty betting variants plus four Open Face Chinese variants, all expressed as
data over one rules engine. A bot declares which of these it supports at upload
time, and a competition may only use a game every seated version declares.

`maxPlayers` is the **platform's** registry value, not the engine's. It bounds
what a hosted competition can request, and it is **not** the same as a version's
declared `supportedPlayerCounts` — each version must separately declare the exact
counts it handles.

It can be narrower than the engine allows. The engine has no field called
`maxPlayers` at all; the equivalent is `GameSpec.seats`, a range. Every row below
matches that range except the three drawmaha games, which the platform caps at 4
while the engine declares `seats: 2..=6`
(`crates/poker-core/src/game/spec.rs:387`). Both readings were re-confirmed
2026-08-24 — the 4 is a deliberate platform restriction, not a transcription
error. Trust the platform value for what a competition will accept.

## Betting games — `WIRE_PROTOCOL.md`

| id | name | family | maxPlayers | unit |
|---|---|---|---|---|
| `holdem-nl` | No-Limit Hold'em | community | 9 | bb |
| `holdem-fl` | Fixed-Limit Hold'em | community | 9 | BB |
| `omaha-pl` | Pot-Limit Omaha | community | 9 | bb |
| `omaha8-pl` | Pot-Limit Omaha Hi-Lo | community | 9 | bb |
| `omaha8-fl` | Fixed-Limit Omaha Hi-Lo | community | 9 | BB |
| `bigo-pl` | Pot-Limit Big O | community | 9 | bb |
| `stud-fl` | Seven Card Stud | stud | 7 | BB |
| `stud8-fl` | Stud Hi-Lo | stud | 7 | BB |
| `razz-fl` | Razz | stud | 7 | BB |
| **`27td-fl`** | **2–7 Triple Draw** | **draw** | **6** | **BB** |
| `a5td-fl` | A–5 Triple Draw | draw | 6 | BB |
| `badugi-fl` | Badugi | draw | 6 | BB |
| `5cd-nl` | No-Limit Five Card Draw | draw | 6 | bb |
| `27sd-nl` | No-Limit 2–7 Single Draw | draw | 6 | bb |
| `badacey-fl` | Badacey | draw | 6 | BB |
| `badeucy-fl` | Badeucy | draw | 6 | BB |
| `archie-fl` | Archie | draw | 6 | BB |
| `drawmaha-fl` | Drawmaha | draw | 4 | BB |
| `drawmaha-27-fl` | Drawmaha 2–7 | draw | 4 | BB |
| `drawmaha-dugi-fl` | Drawmaha Badugi | draw | 4 | BB |

Fixed-limit games display in **big bets** (`BB`), pot/no-limit games in **big
blinds** (`bb`); both divide `ratePer100Milli` by 1000. Stud games set
`preservePrivateCardOrder`, since a stud hand's card order is meaningful.

## Open Face Chinese — `WIRE_PROTOCOL_OFC.md`

A separate, independently versioned protocol: no chips, no betting, placement
decisions scored in points. **A bot speaks one protocol or the other, never
both.**

| id | name | maxPlayers | unit |
|---|---|---|---|
| `ofc` | Open Face Chinese | 4 | pts |
| `ofc-pineapple` | Pineapple OFC | 3 | pts |
| `ofc-progressive` | Progressive Pineapple OFC | 3 | pts |
| `ofc-27` | 2–7 Pineapple OFC | 3 | pts |

OFC competitions cannot use duplicate dealing.

## What is actually contested

This repo targets **`27td-fl`**. The full roster on that game, read from
`GET /api/bots` on 2026-08-22 — nineteen versions, twelve of them real bots:

| bot | seats | artifact |
|---|---|---|
| `paul-sauron200-27td-1bit` | 6 | 228.2 MB |
| `swit-27td-ring3` | 6 | 131.4 MB |
| `swit-27td-ring2` | 6 | 123.0 MB |
| `paul-gandalf200-4bit` | 2 | 83.4 MB |
| `paul-sauron100-lite-1bit` | 2,3,4,5,6 | 33.2 MB |
| `paul-sauron100-exploit-1bit` | 6 | 32.7 MB |
| `paul-sauron100-neutral-1bit` | 6 | 32.7 MB |
| `swit-27td-ring1` | 6 | 30.6 MB |
| `swit-27td-5.1-i048` | 2 | 22.7 MB |
| `nutt-27td-m1` (ours) | 2 | 3.0 MB |
| `nutt-27td-fl` (ours) | 2,3,4,5,6 | 2.3 MB |
| `swit-27td-1.0` | 2 | 1.4 MB |

Plus seven `system` baselines, all declaring 2,3,4,5,6: `rand/30/30/40`,
`rand/50/30/20`, `rand/30/50/20`, `rand/10/70/20`, `autocall`, `autofold` and
`autopush`. The upstream engine offers the same family-aware `builtin:random`
locally, which is what hosted smoke validation plays against.

Both heads-up and 6-handed are actively contested; no 3/4/5-handed competitions
exist. **Heads-up is contested by `swit` and `paul` too** — `swit-27td-5.1-i048`,
`swit-27td-1.0` and `paul-gandalf200-4bit` all declare `[2]`.

Artifact naming leaks how rivals are built, or at least reads that way. `1bit` and
`4bit` look like quantisation widths for a compiled-in strategy table; `i048` reads
as an iteration counter and `ring1`/`ring2`/`ring3` as successive 6-handed
generations. Sizes cluster well under the 300 MiB cap. All of that is inference from
names and file sizes, not anything the platform states. See
[`../game/measurement.md`](../game/measurement.md) for how to read the standings
these bots produce.
