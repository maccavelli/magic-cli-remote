---
status: proposed
date: 2026-10-31
decision-makers: Project Owner
consulted: none
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Harden the httpagent SSE pump: stream liveness, a WHATWG parser, typed health, and gap-triggered resync

## Context and Problem Statement

`internal/provider/httpagent` is the shared HTTP+SSE transport for the kilo
and OpenCode dialects. One long-lived `serve` process, one multiplexed
`GET /global/event` stream, one health poll at `GET /global/health`, and
[0014-MADR-sse-reconnect-resync-decision.md](0014-MADR-sse-reconnect-resync-decision.md)
to heal a turn when frames are missed. The engine works. Session semantics
(turn gates, stall watchdog, coalescing, catalog invalidation) are already
stronger than the engines' own TypeScript clients. This record does **not**
replace that engine with those clients.

The gap is stream plumbing. A 2026-10-31 review of `httpagent` against the
sibling checkouts `/Users/<user>/gitrepos/opencode` (HEAD `237a847056`) and
`/Users/<user>/gitrepos/kilocode` (HEAD `0a9faeff69`), and against the
WHATWG [Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html)
spec, found four defects that the engines' own clients already close, and
that mcremote still leaves open:

1. **A `200` is treated as success.** Health accepts any 200 body;
   `OnHealthy` logs non-JSON at Debug and returns nil
   (`internal/provider/opencode/http.go:659-667`,
   `internal/provider/kilo/version.go:27-36`). SSE accepts any 200 and
   never inspects `Content-Type` (`provider.go:759-772`). That is how
   OpenCode v2's HTML SPA fallback at `/global/health` and `/global/event`
   counted as a healthy, connected engine in
   [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
   (D12). The same mask would hide a future kilo break.
2. **The SSE reader is a line scanner, not an event-block parser.**
   `streamOnce` dispatches every `data: ` line independently
   (`provider.go:792-821`) and ignores every other field. WHATWG events
   are blank-line delimited; `data:` lines concatenate; the space after
   the colon is optional; `: comment` lines are keepalives. OpenCode v2
   already emits `: heartbeat` comments every 15 seconds
   (`packages/server/src/handlers/event.ts:23-25`).
3. **A hung or half-open stream is invisible.** `http.Client{Timeout: 0}`
   with the default `Transport` (`provider.go:150-153`) has no
   `ResponseHeaderTimeout` and no body read deadline. `streamOnce` builds
   the GET with `http.NewRequest` — no context — so a generation bump
   cannot cancel it (`provider.go:759-765`). `pumpEvents` only notices
   the generation at loop-top and after a frame (`provider.go:731-756`,
   `812-816`). A half-open TCP read blocks until the OS times it out;
   a live TCP that has gone silent is never abandoned.
4. **Kilo already tells us a gap happened, and we only log it.**
   `noteSyncSeq` (`internal/provider/kilo/sync.go:42-66`) Warns on a
   forward jump of more than one and claims "the per-session resync
   fills them". Nothing calls `session.resync()`.
   [0137-PLAN-prompt-to-first-token-latency-regression.md](0137-PLAN-prompt-to-first-token-latency-regression.md)
   Phase 7.1 declined `POST /sync/history` (28 MB, unbounded) and said
   the existing 0014 resync would fill a detected gap. The detect half
   landed; the fill half did not. A mid-stream drop (oversized line,
   parser skip) currently waits for the stall watchdog.

The engines already publish keepalives, so an idle timeout is safe. Kilo
current source emits `server.heartbeat` **data** events every 10 seconds
(`packages/opencode/src/server/routes/instance/httpapi/handlers/global.ts:44-47`
and `handlers/event.ts:46-49`). OpenCode v2 emits `: heartbeat` **comments**
every 15 seconds. Live fixtures prove the 1.x line does the same data
heartbeat: kilo 7.4.20 `testdata/kilo-spike-7.4.20/sse-or.raw` lines 133-139
(four consecutive `server.heartbeat` frames); kilo 7.5.6
`testdata/wire/7.5.6/frames.jsonl` lines 22 and 56; OpenCode 1.18.26
`testdata/wire/1.18.26/frames.jsonl` lines 84-85.

This record picks **one most beneficial item** and **three highest
immediate value-add items**, as one architectural choice: keep the
homegrown engine, and bring its pump up to the liveness and typing bar
the engines' own clients already have.

## Decision Drivers

* Keep the homegrown engine. Session semantics stay in `httpagent`.
* Fail loud on a wrong content type or a body that is not the health
  document. The 0214 HTML-200 outage must not be able to hide again,
  on either dialect, without waiting for 0214's still-proposed ACP rewrite.
* Detect a dead or half-open SSE stream in seconds, and reconnect under
  the existing 0014 resync, rather than waiting for an OS TCP timeout
  or the stall watchdog.
* Parse the stream the way the spec and the engines' generated clients
  parse it, so a comment keepalive, a multi-line `data:` event, or a
  `data:` line without a space still works.
* When kilo reports a seq gap, run the resync 0137 already named, under
  the 0014 gates that already exist.
* Cheap, local, unit-testable changes first. Live-tagged tests pin
  behaviour that depends on a real engine.
* Do not take on replay, EventSource, polling, per-session SSE, or
  `POST /sync/history`. Those were measured and declined.

## Considered Options

* A: Adopt the engines' generated TypeScript HTTP client (or a Go
  rewrite of it) as the SSE pump
* B: Leave the pump as-is; take typed health only as part of 0214
* C: Ship only the three cheap items (typed health, event-block parser,
  gap-triggered resync) and defer stream liveness
* D: Harden the homegrown pump in place: stream liveness, typed health,
  a WHATWG event-block parser, and gap-triggered resync
* E: Add Last-Event-ID replay, `EventSource`, per-session SSE, or
  `types=` filtering

## Decision Outcome

Chosen option: "D: Harden the homegrown pump in place: stream liveness,
typed health, a WHATWG event-block parser, and gap-triggered resync",
because the four items are one coherent "make the pump as strict and
live as the engines' own client" decision; because D12 in 0214 is
already specified and kilo/1.x can have it now; and because stream
liveness is the only item that closes a class of hang that 0014 cannot
see (no reconnect means no resync).

The ranking inside D:

| Rank | Item | Why this rank |
| --- | --- | --- |
| **Most beneficial** | Stream liveness: connect/header timeout, idle timeout that treats comments as activity, generation-cancelled GET, dedicated Transport knobs | Closes half-open hangs and silent HTML-200 SSE loops. 0014 only runs after a reconnect; this is what *causes* the reconnect. |
| **Immediate 1** | Typed health + content-type (0214 D12) | Smallest change; turns the next engine-shape break into one log line, for both dialects, without waiting for 0214. |
| **Immediate 2** | WHATWG event-block parser | Unblocks idle-timeout (comments must count as activity). Matches the generated client. Keeps the skippable oversized cap. |
| **Immediate 3** | Kilo seq-gap and oversized-skip must call 0014 resync | The detect half already landed in 0137 Phase 7.1; wiring the fill half is a few lines plus tests. |

Suggested PLAN order is Immediate 1, then 2, then 3, then stream
liveness. Liveness depends on the parser treating comments as
activity. The three cheap items are each independently shippable.

### Most beneficial — stream liveness

Four cooperating pieces, all in `httpagent`. Dialects unchanged.

**1. Connect / header timeout (2 seconds).**

OpenCode's Solid connection manager aborts a connect after 2 seconds
(`packages/client/src/solid/connection.ts:32`, `connectTimeout = 2_000`).
httpagent's health poll already uses a 2-second per-probe context
(`provider.go:617`). The SSE GET currently waits forever for headers.

Pin `ResponseHeaderTimeout: 2 * time.Second` on a **cloned**
`http.DefaultTransport`. Do not mutate the global DefaultTransport.
Header-wait of 2 seconds is safe for REST as well: `apiAt` already
carries a caller context, and catalog bodies of several megabytes are
read *after* headers arrive.

**2. Idle timeout (45 seconds), reset on any received bytes including comments.**

OpenCode's client default is 45 seconds
(`connection.ts:35`, `defaultIdleTimeout = 45_000`), with the comment
"The server writes a keepalive comment every 15 seconds, so a quiet
but healthy stream never trips this." Its tests pin the contract:
`event.subscribe ignores server heartbeat comments` and
`event.subscribe reports heartbeat comments as stream activity`
(`packages/client/test/promise.test.ts:867-896`). Activity is reported
from `reader.read()`, before parsing (`httpapi-codegen` `sse()`:
`if (!next.done) requestOptions?.onActivity?.()`).

Idle at 45 seconds is three OpenCode-v2 keepalive intervals and four
kilo intervals (10 s). Both dialects already emit keepalives on the
1.x line as `server.heartbeat` data events (fixtures cited above).

Reset the idle timer on every successful `Read` that returns at least
one byte, **before** parsing. Comment-only blocks (`: heartbeat\n\n`)
must keep the stream up. Data-only `server.heartbeat` frames already
would, once they parse.

Idle timeout is stream-level. The stall watchdog (`session.go:1140-1183`)
stays turn-level: a quiet *turn* still nags, while keepalives keep the
*stream* up. `server.heartbeat` has an empty session id, so it already
takes the engine-global path and does not touch `lastActivity`.

On idle expiry: cancel the GET, return from `streamOnce`, let
`pumpEvents` reconnect with the existing 1–10 s backoff (reset after
30 s uptime, `provider.go:746-755`). Reconnect runs `resyncSessions`
exactly as today.

**3. Generation-cancelled GET.**

Build the SSE request with `http.NewRequestWithContext`. Cancel that
context when `p.generation != gen` or `p.closed`. Today the generation
check sits at loop-top and after a dispatched frame; a blocked read
on a dead generation is invisible. After a respawn the old GET can
linger until the OS TCP timeout while the new `pumpEvents` already
holds a second stream.

A short ticker (100 ms is plenty) watching generation/closed, or a
channel closed by the death monitor / `Shutdown`, both work. The PLAN
picks. The invariant: once generation moves, the previous GET's
context is cancelled and its body is closed.

**4. Dedicated Transport knobs; SSE `Accept-Encoding: identity`.**

Clone `http.DefaultTransport` and set `ResponseHeaderTimeout` as
above. Keep `Client.Timeout = 0` (SSE bodies last for the life of the
engine). Do **not** set `DisableCompression` on the Transport: REST
catalogs of 4+ MB currently benefit from gzip
([0043-MADR-model-selection.md](0043-MADR-model-selection.md) D4).

On the SSE GET only, set `Accept-Encoding: identity`. Kilo already
excludes `/global/event` and `/event` from compression, and excludes
`text/event-stream` by content-type regex
(`packages/opencode/src/server/routes/instance/httpapi/middleware/compression.ts:6-12,
47-48`). OpenCode v2 sets `Cache-Control: no-cache, no-transform` on
the event stream (`packages/server/src/handlers/event.ts:29`). Go's
default Transport still advertises `gzip`. Identity on the SSE request
is the precise defence against a future proxy buffering an infinite
gzip stream.

Keep `Accept: text/event-stream` (already set, `provider.go:764`).

Constants, pinned to the engines' own client:

| Knob | Value | Source |
| --- | --- | --- |
| Connect / header timeout | 2 s | OpenCode `connectTimeout`; httpagent health probe |
| Idle timeout | 45 s | OpenCode `defaultIdleTimeout` |
| Idle reset | any read, including comments | OpenCode `onActivity` |
| Client.Timeout | 0 | already correct |
| SSE Accept-Encoding | identity | kilo compression exclude + no-transform |
| Backoff | 1 s … 10 s, reset after 30 s up | already correct; leave it |

### Immediate 1 — typed health and content-type (0214 D12)

Implement D12 in `httpagent` so both dialects get it without waiting
for 0214.

**Health (`GET` + `HealthPath`), inside the existing poll
(`provider.go:617-633`). A 200 is healthy only when all of:**

* Media type (RFC 9110) is `application/json`. Compare
  `strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))`.
  Parameters (`charset=utf-8`) are ignored. This is the generated
  client's `isContentType`
  (`opencode/packages/httpapi-codegen/src/index.ts`, function
  `isContentType`: `split(";", 1)[0].trim().toLowerCase() === expected`).
* Body unmarshals as JSON into `{healthy bool, version string}`.
* `strings.TrimSpace(version)` is non-empty.
* If `healthy` is present and `false`, fail.

Otherwise the probe is a miss: log the status, media type, and a
clipped body, and keep polling until `serverStartTimeout`. A persistent
miss still fails spawn, as today.

Then call `OnHealthy(body)` when the dialect implements `HealthyHook`.
The hook remains the place dialects record version and warn on drift.
It no longer has to be the place they reject a non-JSON body — the
transport already did. Dialects may keep returning nil on a body they
cannot parse, for unit tests that call `OnHealthy` directly; the
transport test is the one that refuses HTML.

**SSE (`streamOnce`, after `Do`, before reading the body).** A 200 is
connected only when the media type is `text/event-stream` (same
parameter-ignoring compare). Otherwise return an error naming the
content type, so `pumpEvents` reconnects and the log is one line, not
a silent empty-body loop.

`apiAt` REST responses are out of this item. D12 named health and the
event stream. A later record can type REST.

**Tests that must change.**

* `internal/provider/httpagent/supervise_wiring_test.go:114-117` writes
  `{}` to `/health` with no `Content-Type` and no version. After this
  item that helper must send `Content-Type: application/json` and a
  body `{"healthy":true,"version":"test"}`. The fake dialect's
  `HealthPath` is `/health`.
* New httpagent tests, against **both** a fake OpenCode-shaped dialect
  and a fake kilo-shaped dialect (or one fake dialect used twice):
  * health 200 `text/html` with an HTML body → spawn fails (or the
    poll misses until timeout, if tested through `ensureServer`).
  * health 200 `application/json` with `{"healthy":true}` and no
    version → miss.
  * health 200 `application/json; charset=utf-8` with
    `{"healthy":true,"version":"1.2.3"}` → pass.
  * SSE 200 `text/html` → `streamOnce` errors, names the type.
  * SSE 200 `text/event-stream; charset=utf-8` → accepted.
* Dialect unit tests that currently assert `OnHealthy([]byte("ok"))`
  returns nil (`kilo/dialect_test.go:166-169`,
  `opencode/version_test.go:229-240`) stay. They test the hook, not
  the transport.

Each new gate is first seen to fail on a planted HTML 200, on a
scratch copy, then the assertion is inverted.

### Immediate 2 — WHATWG event-block parser

Replace the per-line `data: ` prefix check in `streamOnce` with a
block parser. `readSSELine` stays as the line reader (bounded,
skippable, CR/LF trimmed). A new assembler sits on top.

WHATWG rules we honour
([HTML Living Standard, Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html)):

* An event is the block of lines ended by a blank line.
* A line whose first character is `:` is a comment. It is activity
  (Immediate-most-beneficial idle timer) and is otherwise ignored.
* Field name is the slice before the first `:`; value is the slice
  after, with a single leading U+0020 stripped if present.
* `data` fields concatenate with U+000A between them. Dispatch the
  concatenated payload as today's `DecodeFrame` input.
* `id`, `event`, `retry` are ignored. Engines do not replay, so
  `Last-Event-ID` is unused. Event types on the wire are inside the
  JSON, not the SSE `event:` field. Hono/Effect emit `event: message`
  or omit it; we ignore both.
* An unterminated block at EOF is dispatched if it contains any
  `data` (generated client: `if (next.done && buffer !== "") buffer += "\n\n"`).

Prefix matching today requires a space (`bytes.HasPrefix(line, []byte("data: "))`).
The spec and the generated client accept `data:` with or without the
space (`line.startsWith("data:") ? line.slice(5).trimStart()`). Honour
that.

**Oversized cap.** Keep skippable, never fatal. A `bufio.Scanner`
`ErrTooLong` would abort the shared stream for every session — that is
why `readSSELine` exists (`provider.go:782-785`,
`TestReadSSELineSkipsOversized`). The generated client throws
`SseEventTooLarge` at 16 MiB assembled and aborts the iterator
(`httpapi-codegen` `const maxSseEventBytes = 16 * 1024 * 1024`). We
diverge: skip the event, log, keep the stream. Raise the assembled
event cap to **16 MiB** to match the client; keep consuming the rest
of the oversized event so the stream stays aligned. An oversized
*line* still uses `readSSELine`'s skippable path; the assembler treats
that as "this event is junk" and waits for the next blank line.

Skipping an event is a gap. Immediate 3 fires resync on that skip.

**Tests.**

* Comment-only block `: heartbeat\n\n` produces no `DecodeFrame` and
  (once liveness lands) resets idle.
* `data: {"type":"x"}\n\n` (space) and `data:{"type":"x"}\n\n` (no
  space) both dispatch.
* Two `data:` lines in one block concatenate with a newline.
* `event: message` / `id: …` / `retry: 1000` around a `data:` line do
  not change the payload.
* An oversized event is skipped; the next well-formed event still
  parses (`TestReadSSELineSkipsOversized` remains, plus an assembler
  test).
* `TestStreamConnectTriggersResync` already sets
  `Content-Type: text/event-stream` (`session_test.go:356`); it stays
  valid.

### Immediate 3 — seq-gap and oversized-skip call 0014 resync

**Kilo seq gap.** `DecodeFrame` already calls `noteSyncSeq`
(`internal/provider/kilo/dialect.go:191-193`) and does not route the
sync frame to a session (routing both the plain frame and its twin
would double-deliver; 0137 Phase 7.1). On a forward jump of more than
one, besides the existing Warn, the dialect must tell httpagent to
resync **that** `aggregateID`.

Shape (PLAN picks the exact hook; the invariant is the point):

* `pumpEvents` / `streamOnce` is the only `DecodeFrame` caller, and it
  is single-threaded per generation. A consume-once optional interface
  is enough, for example `TakeStreamGap() (sid string, ok bool)` on the
  dialect, checked after every `DecodeFrame`.
* Look the session up with the existing `lookupSessionLocked`.
* Call `s.resync()`. The 0014 gates already inside `resync`
  (`session.go:1195-1200`) skip idle, closed, and `promptInFlight`.
* Do not dispatch the sync frame as `HandleEvent`.

A first sighting still only sets the baseline (`sync.go:51-57`). A
backward seq still means a new aggregate reusing an id, not a loss.

**Oversized skip, both dialects.** When the assembler drops an event
as too large, call `resyncSessions(gen)` — the same sweep a reconnect
already runs (`provider.go:862-878`). 0014 already names "a dropped
oversized SSE line that happened to carry `session.idle`" as a gap
the stall watchdog covers. This item makes that heal immediate
instead of waiting for `TurnStallNotice`.

**Still declined:** `POST /sync/history`. The 0137 Phase 7.1 live
probe stands: 28 MB, 13410 events, 153 aggregates, no allow-list,
experimental upstream. This item fills a detected gap with the
targeted per-session REST resync 0014 already specified.

**Tests.**

* Kilo: two `sync` frames for the same `aggregateID` with `seq` 3 then
  7, against a registered turn-active session, must call `Resync` once
  and must not `HandleEvent` the sync frames. An idle session must
  not resync (0014 gate).
* Kilo: consecutive seq (`3` then `4`) must not resync.
* httpagent: an oversized event on a stream that then continues must
  call `resync` on a turn-active session.
* The existing Warn log stays; add an assertion it still fires.

### Out of scope (declined here, and not to be smuggled into the PLAN)

* Adopting `@opencode/client`, Effect `HttpApi`, Hono `streamSSE`, or
  a Go rewrite of those. Ideas come from them; the pump stays ours.
* `Last-Event-ID` / SSE replay. Neither engine replays. 0014 resync
  is the recovery.
* `EventSource`. We need custom demux, generation cancel, and
  skippable oversized events.
* Polling as a fallback when SSE is down.
* Ending a turn on reconnect. 0014's gates exist because that is
  wrong.
* Per-session SSE. One multiplexed `/global/event` is the engine
  contract.
* `types=` subscription filters. Engine-global frames
  (`catalog.updated`, diagnostics) must keep arriving
  ([0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md)
  F8).
* `POST /sync/history` resume.
* Typing `apiAt` REST content-type. D12 named health and SSE.
* Changing coalescing ([0024-MADR-stream-coalescing.md](0024-MADR-stream-coalescing.md)),
  the stall watchdog's notice text, or dialect event vocabularies.
* OpenCode v2 path/auth/ACP work. That remains 0214. This record
  makes a v2 engine on the current 1.x dialect **fail fast at health**
  with a named content-type/JSON error, which is the D12 outcome.

### Consequences

* Good, because a half-open or silent SSE stream reconnects in 45
  seconds at worst, and a hung handshake dies in 2 seconds, instead of
  waiting for an OS TCP timeout or a stall notice about a stream that
  is already dead.
* Good, because the next HTML-200 engine-shape break, on kilo or
  OpenCode, is one log line at spawn rather than a healthy-looking
  daemon with a reconnect loop.
* Good, because a kilo seq gap or an oversized skipped event heals
  the turn through the resync 0014 already specified, without taking
  on an unbounded history fetch.
* Good, because the parser matches the spec and the generated client,
  so OpenCode v2 comment keepalives and a future `data:` without a
  space keep working, and idle-timeout can treat comments as activity.
* Good, because D12 lands for kilo and OpenCode 1.x now, while 0214
  is still proposed.
* Neutral, because a quiet-but-healthy engine that stopped emitting
  keepalives would reconnect every 45 seconds. Both current engines
  emit them; a regression in the engine would show up as reconnect
  logs, which is the point.
* Neutral, because `OnHealthy` dialect unit tests that feed non-JSON
  stay green; the new refusal lives in the transport.
* Bad, because an OpenCode v2 binary pointed at the current 1.x
  dialect will fail spawn (HTML 200) instead of appearing connected.
  That binary is already unusable on this dialect; the error becomes
  honest. Restoring v2 is 0214.
* Bad, because fake-engine tests that serve `{}` without a JSON
  content type or a version will fail until they send a real health
  document.
* Bad, because a 45-second idle reconnect during a long engine pause
  *with keepalives disabled* would resync turn-active sessions. Cost
  is the existing 0014 REST; idle sessions stay free.

### Confirmation

* Unit tests named under each item above. Each new gate is first
  seen to fail on planted bad input (HTML 200, silent connection,
  `seq` jump, oversized event, comment-only block), on a scratch
  copy or a temp file, then the production assertion is written.
  The handoff of the implementing PLAN records what the failure
  looked like.
* `TestReadSSELineSkipsOversized` and `TestStreamConnectTriggersResync`
  still pass. `TestResyncGatesOnTurnState` still enforces the 0014
  gates on gap-triggered resync.
* `make test` and `make race` pass.
* `make live-kilo` and `make live-opencode` (owner's word on quota)
  confirm a real engine: health JSON + version, SSE
  `text/event-stream`, keepalives arriving inside the idle window.
  No prompt is required for those three facts.
* This record stays `proposed` until the owner accepts it. A PLAN is
  written only when asked. Implementation waits on explicit approval
  of the pair.

## Pros and Cons of the Options

### A: Adopt the engines' generated TypeScript HTTP client (or a Go rewrite of it) as the SSE pump

The OpenCode v2 client is generated from OpenAPI (`packages/httpapi-codegen`,
emitted into `packages/client/src/promise/generated/client.ts`). Kilo's
current server is Effect `HttpApiBuilder` plus `effect/encoding/Sse`.
The 1.x line used Hono `streamSSE`. None of these is a Go SDK.

* Good, because the generated `sse()` already does content-type
  checks, event-block assembly, a 16 MiB cap, and `onActivity`.
* Neutral, because those four behaviours are small and can be copied
  as ideas without taking the client.
* Bad, because the client has no 0014 resync, no turn gates, no
  stall watchdog, no coalescing, no catalog invalidation, no
  generation-scoped pump, and no skippable oversized event (it
  aborts). Replacing `httpagent` would throw away the part that
  already works very well.
* Bad, because the generated client is TypeScript and per-engine.
  A Go rewrite would have to be done twice (kilo's envelope vs
  OpenCode's) and then kept in lockstep with codegen.

### B: Leave the pump as-is; take typed health only as part of 0214

* Good, because 0214 D12 already specifies the content-type and
  version checks, and 0214 is the OpenCode v2 record.
* Bad, because 0214 is still `proposed`, is an ACP+lease rewrite,
  and its D12 is the one piece kilo needs too. Waiting couples a
  two-line health check to a transport replacement.
* Bad, because stream liveness, the parser, and the gap-resync wire
  are not in 0214 at all. The HTML-200 class of bug would remain
  on kilo for as long as 0214 stays open.

### C: Ship only the three cheap items and defer stream liveness

* Good, because Immediate 1–3 are each a few dozen lines and are
  independently testable.
* Good, because the parser is a prerequisite for idle-timeout
  treating comments as activity, so doing it first is the right
  order anyway.
* Bad, because the hang class (half-open TCP, silent live TCP,
  uncancelled generation GET) stays open. 0014 cannot heal a
  stream that never reconnects.
* Neutral, because C is a valid *phase split* inside D. Choosing
  D does not forbid shipping 1–3 before liveness; it forbids
  treating liveness as optional.

### D: Harden the homegrown pump in place (chosen)

* Good, because it copies the four behaviours the engines' clients
  got right (typed health, event-block parse, idle/connect
  timeouts, activity-including-comments) and keeps the session
  semantics they lack.
* Good, because every change lives in `httpagent` plus a few kilo
  dialect lines for the gap signal. OpenCode dialect code is
  untouched beyond tests that feed `OnHealthy`.
* Good, because constants are pinned to values the engines already
  emit (10 s / 15 s keepalives) and already consume (2 s connect,
  45 s idle, 16 MiB event).
* Neutral, because a PLAN can still land Immediate 1–3 before
  liveness.
* Bad, because fake-engine fixtures must grow a JSON content type
  and a version, and because a v2 OpenCode binary on the 1.x
  dialect will fail spawn honestly.

### E: Add Last-Event-ID replay, EventSource, per-session SSE, or `types=` filtering

* Good, because Last-Event-ID is in the spec and EventSource would
  be less code.
* Bad, because neither engine replays missed events. Last-Event-ID
  would be sent and ignored. 0014 exists for that reason.
* Bad, because `EventSource` cannot skip an oversized event, cannot
  cancel on generation, and cannot expose comment activity as a
  first-class reset.
* Bad, because per-session SSE and `types=` would drop
  engine-global frames (`catalog.updated`, diagnostics) that F8
  already made load-bearing, and would multiply connections to a
  server that already multiplexes.

## More Information

### Evidence, by item

**Typed health / content-type**

* httpagent health poll: `internal/provider/httpagent/provider.go:617-633`
  — any 200, then optional `OnHealthy`.
* SSE connect: `provider.go:759-772` — any 200, `Accept:
  text/event-stream` already set, response type unread.
* OpenCode `OnHealthy`: `internal/provider/opencode/http.go:659-705`
  — non-JSON Debug-logs and returns nil; empty version records
  nothing; `CheckMinVersion` skips empty
  (`http.go:716-718`, `httpagent.go:397-400`).
* Kilo `OnHealthy`: `internal/provider/kilo/version.go:27-56` — same
  non-JSON tolerance.
* 0214 D12: JSON must be `application/json`, SSE must be
  `text/event-stream`, health requires a decodable version; tests
  against HTML 200 for both dialects
  (`0214-MADR-…` "Decision Outcome" D12 and "Confirmation").
* 0214 probe table row 3–4: v2 `GET /global/health` and
  `/global/event` answer `200 text/html`.
* Kilo health schema: `healthy: Schema.Literal(true)`, `version:
  Schema.String`
  (`kilocode/packages/opencode/src/server/routes/instance/httpapi/groups/global.ts:14-17`).
* Kilo health handler returns `{healthy: true, version: InstallationVersion}`
  (`handlers/global.ts:80-82`).
* Generated client: `isContentType` for both JSON and SSE;
  `sse()` throws `UnsupportedContentType` when the media type is
  wrong (`opencode/packages/client/src/promise/generated/client.ts:405-410`
  and the `json()` helper).
* Fake health helper that will fail: `supervise_wiring_test.go:114-117`.

**Event-block parser**

* Current dispatch: `provider.go:787-821`, prefix `data: ` with a
  required space; comments, `id`, `event`, `retry`, and multi-line
  `data:` are dropped or mis-handled.
* Line cap 10 MiB, skippable: `provider.go:831-859`,
  `TestReadSSELineSkipsOversized`.
* Generated client assembler: blank-line delimited, `data:` with
  optional space, comments yield no event, 16 MiB assembled cap,
  abort on oversize (`httpapi-codegen/src/index.ts` `sse()`).
* OpenCode v2 keepalives are comments: `: heartbeat\n\n` every 15 s
  (`packages/server/src/handlers/event.ts:23-25`).
* WHATWG: event blocks, optional space after colon, comments,
  `Last-Event-ID` (unused here).

**Stream liveness**

* Client construction: `provider.go:150-153`, `Timeout: 0`, no
  Transport, so `http.DefaultTransport` (`ResponseHeaderTimeout`
  0, `DisableCompression` false).
* SSE GET has no context: `provider.go:760`.
* Generation check after a frame only: `provider.go:812-816`.
* OpenCode client: 2 s connect, 45 s idle, abort+reconnect, comment
  activity (`packages/client/src/solid/connection.ts:32-38, 84-91,
  97`).
* Kilo keepalives: 10 s `server.heartbeat` data events, `Stream.drop(1)`
  so the first fires after 10 s (`handlers/global.ts:44-47`,
  `handlers/event.ts:46-49`).
* Kilo compression excludes streaming paths and `text/event-stream`
  (`middleware/compression.ts`).
* 0014 resync on successful (re)connect: `provider.go:774-780`.

**Gap-triggered resync**

* `noteSyncSeq`: `internal/provider/kilo/sync.go:27-66` — Warn only.
* Call site: `dialect.go:191-193` inside `DecodeFrame`.
* 0137 F7: `0137-MADR-prompt-to-first-token-latency-regression.md`
  "F7 — kilo 7.5.6 ships an event-sourced `sync` stream that
  mcremote ignores".
* 0137 Phase 7.1: detect landed; `POST /sync/history` declined
  (28,351,089 bytes / 13410 events / 153 aggregates);
  "filled by the existing resync" was the written recovery, and
  reconnect-resync is the only path that actually calls it.
* 0014 gates: turn-active, not `promptInFlight`, stale-evidence,
  finished-turns-only (`0014-MADR-sse-reconnect-resync-decision.md`,
  `session.go:1195-1208`).
* Stall watchdog as delayed cover for oversized-line gaps:
  0014 "Triggers" item 2; `session.go:1161-1169`.

### Files a PLAN would touch

* `internal/provider/httpagent/provider.go` — health poll, `streamOnce`,
  `pumpEvents`, client/Transport, parser assembler, oversized →
  `resyncSessions`, generation cancel.
* `internal/provider/httpagent/httpagent.go` — optional gap hook
  interface, if that is the shape.
* `internal/provider/httpagent/session.go` — no gate changes;
  `resync` already correct.
* `internal/provider/kilo/sync.go` / `dialect.go` — gap signal out
  of `noteSyncSeq`.
* Tests: `httpagent/session_test.go`, `httpagent/supervise_wiring_test.go`,
  new `httpagent/sse_parse_test.go` / `health_type_test.go` /
  `stream_liveness_test.go`; `kilo` gap-resync test next to
  `dialect_test.go`.
* Dialects' `OnHealthy` bodies stay; their "non-JSON is tolerated"
  unit tests stay.

### Relationship to other records

* **0014** remains the resync protocol. This record adds two
  triggers (seq gap, oversized skip) and makes reconnect more
  likely to happen (liveness). Gates are unchanged.
* **0024** coalescing is untouched. The parser still hands one
  JSON payload at a time to `DecodeFrame`.
* **[0075-MADR-kilo-cli-provider.md](0075-MADR-kilo-cli-provider.md)**
  kilo-on-httpagent, un-gated loopback, no Authorization: unchanged.
* **0137 F7/F8**: F8 catalog invalidation already landed. F7's
  fill half lands here. `POST /sync/history` stays declined.
* **0214 D12**: specified there, implemented here, so kilo and
  OpenCode 1.x do not wait on an ACP rewrite. When 0214 is
  accepted, D12 is already done; 0214 should point here rather
  than re-specify the checks.
* **0214** otherwise (ACP, lease, v2 `/api`, password): out of
  scope. The honest spawn failure on a v2 binary is the intended
  D12 side effect.

### Implementation notes for a future PLAN

Order: Immediate 1 (typed health), Immediate 2 (parser), Immediate 3
(gap → resync), then stream liveness. Parser before liveness so
comment keepalives reset the idle timer on the first liveness
commit rather than causing a 45-second false reconnect against
OpenCode v2.

Do not put `ResponseHeaderTimeout` on `http.DefaultTransport`; clone
it. Do not set `DisableCompression` on the shared Transport; set
`Accept-Encoding: identity` on the SSE GET only.

Idle timeout and stall watchdog measure different things. A test
that feeds only `: heartbeat` comments must keep the stream up and
must still let a turn-active session with no `data:` frames hit
the stall watchdog.

Host identifiers never appear in the implementing diff. Fixture
bodies that already carry redacted paths stay redacted.
