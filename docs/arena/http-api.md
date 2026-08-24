# MixedSolver Arena HTTP API

> Observed from the live platform at <https://arena.sorawit.dev> on 2026-08-22 —
> its SPA bundle, its `/protocol/bots` guide, and authenticated read-only calls.
> The platform publishes no machine-readable specification, so this document is
> an observation and can go stale silently. See [`../SOURCES.md`](../SOURCES.md).

Base URL: `https://arena.sorawit.dev`. All responses are JSON.

## Authentication

Either the browser session cookie or, for API clients:

```
Authorization: Bearer <API_KEY>
Accept: application/json
```

API keys are minted at `POST /api/api-keys` and carry **the same bot and
competition permissions as the account**. This repo reads the key from the
`API_KEY` environment variable (or a local `.env`, which is gitignored).

## Error envelopes

The API returns **two different shapes** depending on the endpoint. A client
must handle both — the platform's own SPA reads `error ?? message`:

```json
401 {"code":"authentication_required","message":"valid login credentials or an API key are required"}
400 {"error":"unknown hand collection"}
```

A `204` is a meaningful success for `/api/progress` (nothing new), not an error.

## Unit scaling — read this before rendering any number

Money, rates and frequencies are transported as **scaled integers**. Printing
them raw is wrong by three or six orders of magnitude.

| Suffix | Divide by | Fields |
|---|---|---|
| `…Milli` | 1000 | `ratePer100Milli`, `confidence95Milli`, `potMilli`, `netMilli`, `awardsMilli`, `amountMilli`, `aggressionMilli` |
| `…Ppm` | 1 000 000 | `vpipPpm`, `pfrPpm`, `wentToShowdownPpm`, `wonAtShowdownPpm`, `foldRatePpm` |
| `…Micros` | 1000 → ms | `meanMicros`, `p50Micros`, `p90Micros`, `p99Micros`, `maxMicros` |

`rateUnit` on a match names the display unit for `ratePer100Milli` — e.g.
`"BB/100"` for betting games, `"pts/100"` for OFC.

## Read endpoints

### `GET /api/health`
No auth required.
```json
{"data":"sqlite","liveIntervalMs":5000,"liveTransport":"poll","liveUpdates":true,"mode":"hosted","status":"ok"}
```

### `GET /api/auth/me`
```json
{"id":"…","username":"nuttakit","role":"member","passwordChangeRequired":false,"createdAtMs":1785560974360}
```

### `GET /api/bots` · `GET /api/account/bots`
All visible bots (including `system` baselines) versus only the ones you own.
Each carries its `latestVersion`:
```json
{"id":"…","name":"nutt-27td-m1","system":false,"state":"ready","createdAtMs":1786419111349,
 "latestVersion":{"id":"…","botId":"…",
   "artifactDigest":"3b26576e…","artifactSize":2959672,
   "target":"linux-x86_64-static","supportedGames":["27td-fl"],
   "supportedPlayerCounts":[2],"createdAtMs":1786419111349}}
```
`state` gates competition entry — a version is selectable only once smoke
validation has passed.

### `GET /api/bots/{id}/versions`
The immutable version history, newest first. **`GET /api/bots/{id}` is `405`** —
there is no bot-detail endpoint; that path only accepts `PATCH` (rename) and
`DELETE`.

### `GET /api/competitions` · `GET /api/competitions/{id}`
```json
{"id":"…","config":{"game":"27td-fl","players":["<versionId>", "…"],"hands":100002,
  "duplicate":true,"cpuCores":3,"decisionTimeoutMs":5000},
 "state":"completed","players":[{"player":0,"botId":"…","versionId":"…","name":"…","artifactDigest":"…"}],
 "createdAtMs":…,"updatedAtMs":…,"matchId":86,"failureCode":null,"peakMemoryBytes":54662267}
```
`state` is one of `queued` / `provisioning` / `running` / `completed` /
`failed` / `cancelled`. `failureCode` of `match-timeout` means the 10-minute
wall clock ended it. `matchId` links to the match record once one exists.

### `GET /api/matches?limit=N` · `GET /api/matches/{id}`
The list returns match summaries; the detail wraps the same summary as
`matchInfo` and adds per-player `stats`:
```json
{"matchInfo":{"id":86,"game":"badugi-fl","family":"betting","dealMode":"duplicate",
  "status":"completed","configuredHands":10000,"cpuCores":3,"decisionTimeoutMs":5000,
  "currentHand":10000,"completedHands":10000,
  "players":["nutt-badugi-fl","rand/30/30/40"],
  "ratePer100Milli":[119635,-119635],"confidence95Milli":[3831,3831],
  "confidenceSamples":[5000,5000],"rateUnit":"BB/100",
  "startedAtMs":…,"updatedAtMs":…,"terminalReason":null},
 "stats":[{"player":0,"name":"nutt-badugi-fl","hands":10000,"faults":0,
   "decisions":{"count":34962,"meanMicros":1106,"p50Micros":825,"p90Micros":1961,
                "p99Micros":2139,"maxMicros":2395},
   "vpipPpm":494600,"pfrPpm":195300,"aggressionMilli":2294,
   "wentToShowdownPpm":124800,"wonAtShowdownPpm":741987,"foldRatePpm":165200,"ofc":null}],
 "eventCursor":653328415,"sampleHandCount":100,"biggestHandCount":100}
```
**`faults` is the number that matters for correctness.** Any value above zero
means the bot emitted an illegal or malformed action, or missed a deadline.

### `GET /api/matches/{id}/hands?collection=&page=`
Collections are **`samples`** (the default, and what an omitted parameter means)
and **`biggest`**. Anything else is `400 {"error":"unknown hand collection"}`.
```json
{"collection":"biggest","page":0,"pageCount":1,"totalHands":100,
 "hands":[{"number":2411,"roles":["bigblind","smallblind"],"status":"completed",
   "potMilli":23000,"awardsMilli":[23000,null],"netMilli":[11500,-11500],
   "winner":0,"finalCards":[["Jc","6d","Ts","Ah"],["8d","9d","8h","4c"]],
   "showdown":true,"ofcBoards":null}]}
```

### `GET /api/matches/{id}/hands/{n}`
One hand plus its event list.

> **These events are not wire events.** The hosted log uses the platform's own
> vocabulary — `{id, handNumber, kind, player, street, action, cards, amountMilli, detail, placements}`
> with cards as a packed string (`"JcKh4hJd"`) — and is *not* the upstream
> `Event` union from `WIRE_PROTOCOL.md`. Two separate vocabularies; do not write
> one decoder for both.

### `GET /api/progress?matchId=&since=`
Live polling. Both parameters are optional; `since` takes the previous
`revision`. **Returns `204` with no body when nothing has changed.**
```json
{"revision":35,"matches":[{"id":86,"status":"completed","currentHand":10000,
  "completedHands":10000,"updatedAtMs":…,"eventCursor":653328415}]}
```
`/api/health` advertises the cadence: `liveTransport: "poll"`,
`liveIntervalMs: 5000`.

## Write endpoints

### Bot upload — a four-step flow

Identical for the website and API clients. The website sends 64 MiB requests so
the flow survives proxies with a 100 MB per-request ceiling; **the selected file
is never transformed**, so the bytes on the wire are the bytes of the artifact.

**1. Open the session**
```
POST /api/bot-uploads
{"name":"my-bot","games":["27td-fl"],"playerCounts":[2],"size":2959672}
   → {"uploadId":"…","chunkBytes":67108864,"totalChunks":1}
```
`size` is the exact byte count and is enforced.

**2. Send every chunk, in ascending zero-based order**
```
PUT /api/bot-uploads/{uploadId}/chunks/{index}
Content-Type: application/octet-stream
<raw bytes: file[index*chunkBytes : min(size, (index+1)*chunkBytes)]>
   → {"receivedBytes":…,"nextChunk":…}
```
The website verifies `receivedBytes == end` and `nextChunk == index + 1` after
every chunk and aborts otherwise; a client should do the same.

**3. Finalize** — `POST /api/bot-uploads/{uploadId}/complete`

**4. Cancel** — `DELETE /api/bot-uploads/{uploadId}`

### Upload session rules

- **One in-flight artifact per account.**
- Sessions belong to the authenticated account and **expire after one hour**.
- A session is discarded if a chunk is missing, the declared byte count differs,
  or the user cancels.
- **Do not retry a failed chunk inside the same session.** The server discards
  malformed or interrupted streams — the only correct recovery is `DELETE` and a
  fresh upload from chunk 0.

Validation then runs asynchronously; poll `/api/account/bots` until the new
version's `state` settles. See
[`hosted-bot-interface.md`](hosted-bot-interface.md) for what validation checks.

### `POST /api/competitions`

```json
{"game":"27td-fl","players":["<versionId>","<versionId>"],
 "hands":10000,"duplicate":true,"cpuCores":3,"decisionTimeoutMs":5000}
   → {"id":"<competitionId>", …}
```

`players` holds **version ids, not bot ids**, one per seat, in seat order. The
same version id may appear more than once to seat a bot against itself.

Constraints, as enforced client-side by the platform's own UI — worth checking
before spending a request:

| Field | Rule |
|---|---|
| table size | 2–6 seats (`players.length`) |
| `hands` | integer, `1 … 300000` |
| `cpuCores` | integer, `1 … 8` |
| `decisionTimeoutMs` | integer, `1 … 5000` (UI default 5000) |
| `duplicate` | requires `hands % players.length == 0` |
| `duplicate` + OFC | not allowed |
| every version | must declare this `game` **and** this exact seat count |

Every competition ends at its hand count **or 10 minutes, whichever comes
first** — so a large `hands` value on a slow bot yields a partial match with
`failureCode: "match-timeout"`, not a failure to launch.

## Endpoint summary

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | service status, live-update cadence |
| GET | `/api/auth/me` | current account |
| GET | `/api/bots` | all bots incl. system baselines |
| GET | `/api/account/bots` | bots owned by this account |
| GET | `/api/bots/{id}/versions` | immutable version history |
| PATCH | `/api/bots/{id}` | rename |
| DELETE | `/api/bots/{id}` | delete bot |
| POST | `/api/bot-uploads` | open upload session |
| PUT | `/api/bot-uploads/{id}/chunks/{i}` | send one chunk |
| POST | `/api/bot-uploads/{id}/complete` | finalize |
| DELETE | `/api/bot-uploads/{id}` | cancel |
| GET | `/api/competitions` · `/{id}` | queued/running/finished competitions |
| POST | `/api/competitions` | queue a match |
| GET | `/api/matches?limit=` · `/{id}` | match summaries and statistics |
| GET | `/api/matches/{id}/hands` | hand collections (`samples`, `biggest`) |
| GET | `/api/matches/{id}/hands/{n}` | one hand plus platform events |
| GET | `/api/progress` | live polling cursor |
| GET/POST/DELETE | `/api/api-keys` | key management |

Admin-only paths (`/api/admin/*`) exist but are not usable by a `member`
account.
