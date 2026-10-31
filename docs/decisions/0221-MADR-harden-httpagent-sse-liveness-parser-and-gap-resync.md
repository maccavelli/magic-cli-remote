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
`GET` on `EventsPath` (`/global/event`), one health poll on `HealthPath`
(`/global/health`), and
[0014-MADR-sse-reconnect-resync-decision.md](0014-MADR-sse-reconnect-resync-decision.md)
to heal a turn when frames are missed. Session semantics (turn gates, stall
watchdog, coalescing, catalog invalidation) already live here. This record
does not replace that engine with the engines' TypeScript clients.

The gap is stream plumbing. A 2026-10-31 review read `httpagent`, both
dialects, the live wire fixtures in-tree, and the sibling checkouts
`/Users/<user>/gitrepos/opencode` (HEAD `237a847056`) and
`/Users/<user>/gitrepos/kilocode` (HEAD `0a9faeff69`), plus Go 1.27.3
`net/http`. Four defects are closed in those clients and still open here.

This revision of the record (same calendar day as the first draft) replaces
unmeasured claims with the counts, contracts, and call sites below. The
chosen option is unchanged.

### What the code does today (read, not inferred)

1. **Health: any HTTP 200 ends the poll.**
   `startServer` (`internal/provider/httpagent/provider.go:617-633`) builds
   `GET` with a 2-second context, reads at most 64 KiB
   (`io.LimitReader(res.Body, 64<<10)`), and on `StatusOK` optionally calls
   `HealthyHook.OnHealthy`. It does not read `Content-Type`. Non-200 and
   transport errors retry every 50 ms until `serverStartTimeout` (60 s,
   `provider.go:32`).
2. **`OnHealthy` never fails boot on a bad body.**
   OpenCode (`internal/provider/opencode/http.go:659-667`) and kilo
   (`internal/provider/kilo/version.go:27-36`) JSON-unmarshal
   `{healthy, version}`; on error they Debug-log and `return nil`. Empty
   version records `""`. `VersionGate.CheckMinVersion` must return nil on
   empty (`httpagent.go:397-400`; OpenCode `http.go:716-718`). Unit tests
   pin this: kilo `TestOnHealthyRecordsVersion` (`dialect_test.go:166-169`)
   and OpenCode `version_test.go:229-240` ("malformed body is tolerated").
3. **SSE: any HTTP 200 is a connected stream.**
   `streamOnce` (`provider.go:759-772`) sets `Accept: text/event-stream`,
   uses `http.NewRequest` (no context), and returns only on non-200
   (`fmt.Errorf("sse status %d", …)`). `Content-Type` is unread.
4. **The reader is a line scanner.**
   `provider.go:787-821` calls `readSSELine` and dispatches only lines with
   prefix `data: ` (space required). Comments, `event:`, `id:`, `retry:`,
   `data:` without a space, and blank lines are skipped. Each matching line
   is one `DecodeFrame`. Cap `maxSSELine = 10 << 20` (10 MiB), skippable
   (`provider.go:831-859`; `TestReadSSELineSkipsOversized`).
5. **The HTTP client has no deadlines except those the caller passes.**
   `NewWithLogger` (`provider.go:150-153`): `&http.Client{Timeout: 0}` and
   no `Transport`, so `http.DefaultTransport`. Health already passes a
   2-second context. SSE does not. `http.NewRequest` wraps
   `context.Background` (Go 1.27.3 `net/http/request.go:871-873`).
   `Client.Timeout` of zero means no timeout (`client.go:92-98`).
6. **Generation is checked where a read is not blocked.**
   `pumpEvents` (`provider.go:731-756`) tests
   `generation == gen && eng != nil && eng.url == url && !closed` at
   loop-top. `streamOnce` tests `generation != gen` after a frame with a
   session id (`provider.go:811-816`). A blocked body read sees neither.
   `generation++` happens in `startServer` (`provider.go:670`) and
   `RestartForCredentialChange` (`httpagent.go:356`). `Shutdown`
   (`provider.go:422-427`) sets `closed` and nils `eng`; the death monitor
   (`provider.go:700-706`) nils `eng`. Neither increments generation.
7. **Kilo seq-gap is a Warn.**
   `DecodeFrame` (`kilo/dialect.go:191-193`) calls `noteSyncSeq`.
   `noteSyncSeq` (`kilo/sync.go:42-66`) stores `syncSeq[aggregateID]` and
   on a forward jump of more than one logs
   `"kilo sync stream gap"` with `missed=seq-last-1` and the hint
   `"the per-session resync fills them"`. Nothing in kilo or httpagent
   calls `session.resync` from that path. Production `DecodeFrame` has one
   caller: `streamOnce`. Tests that pin the log and the empty session id:
   `kilo/sync_test.go` `TestSyncGapIsReported`,
   `TestFirstSyncSightingIsNotAGap`,
   `TestSyncSequenceGoingBackwardsIsNotAGap`,
   `TestSyncFramesAreNotRoutedToASession`,
   `TestFixtureSyncFramesAdvanceContiguously`.
8. **The fill path already exists.**
   `session.resync` (`session.go:1195-1208`) gates on
   `!closed && turnActive && !promptInFlight`, then
   `ds.Resync(ctx, turnStartedAt)` with a 15-second context.
   `resyncSessions` (`provider.go:865-878`) is started from a successful
   SSE connect (`provider.go:780`) and from the stall watchdog
   (`session.go:1167-1169`). Kilo's `Resync` is `kilo/resync.go` (tree,
   permissions, questions, todos, parent message-log) and is covered by
   `kilo/resync_test.go`.

### What the engines emit (measured)

Four in-tree captures were counted on 2026-10-31. Raw files are full SSE
(including `data: ` prefixes and blank lines). JSONL files are decoded
payloads only.

| Capture | Bytes | Events | `data: ` (space) | `data:` no space | comments | `event:`/`id:`/`retry:` | multi-line `data:` pairs | heartbeats | sync | max line |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| kilo 7.4.20 `testdata/kilo-spike-7.4.20/sse-or.raw` | 97,738 | 70 | 70 | 0 | 0 | 0 | 0 | 4 | 24 | 21,043 |
| kilo 7.4.20 `sse-global.raw` | 9,288 | 22 | 22 | 0 | 0 | 0 | 0 | 2 | 5 | 1,138 |
| kilo 7.5.6 `testdata/wire/7.5.6/frames.jsonl` | 25,432 | 56 | n/a (JSONL) | n/a | n/a | n/a | n/a | 2 | 18 | 865 |
| OpenCode 1.18.26 `testdata/wire/1.18.26/frames.jsonl` | 15,901 | 85 | n/a | n/a | n/a | n/a | n/a | 2 | 0 | 587 |

Further counts from those files:

* kilo 7.5.6: 18 `sync` frames, one aggregate, seq 0..17 contiguous
  (`TestFixtureSyncFramesAdvanceContiguously` already asserts this).
  18/56 = 32% of the turn.
* kilo 7.4.20 `sse-or.raw`: 24 `sync` / 70 events; 4 consecutive
  `server.heartbeat` at lines 133-139.
* OpenCode 1.18.26: 45 `plugin.added`, 2 `catalog.updated`, 2
  `server.heartbeat` at JSONL lines 84-85. No `sync`.
* Every `server.heartbeat` in these captures has empty `properties`.
  kilo `TestDecodeFrameHeartbeatHasNoSession` (`dialect_test.go:98-107`)
  asserts the transport therefore drops it (empty session id).
* Largest observed SSE line is 21,043 bytes, 0.2% of `maxSSELine`.

Current engine **source** (sibling checkouts), which the 1.x JSONL does
not yet reflect:

* Kilo HEAD `0a9faeff69` `packages/opencode/src/server/routes/instance/httpapi/handlers/global.ts:44-47`
  and `handlers/event.ts:46-49`: `Stream.tick("10 seconds")` +
  `Stream.drop(1)` emits a `server.heartbeat` **data** event every 10 s
  after the first interval. `eventData` sets `event: "message"`
  (`global.ts:18-24`) and the stream is `Sse.encode()` (`global.ts:53`).
  A kilo unit test already documents that encoding as
  `` `event: message\ndata: ${payload}\n\n` ``
  (`packages/opencode/test/tool/websearch.test.ts:84`).
* OpenCode HEAD `237a847056` `packages/server/src/handlers/event.ts:23-25`:
  `Stream.tick("15 seconds")` maps to the raw string `": heartbeat\n\n"`,
  merged with the encoded event stream. That is a WHATWG comment, not a
  `data:` line. `Cache-Control: no-cache, no-transform` (`event.ts:29`).

Inference, labelled: 1.x captures are compatible with today's line
scanner (`data: ` + blank line, no comments, no `event:` field). The
parser item is for (a) OpenCode v2 comment keepalives, (b) current kilo
`event: message` lines (harmless today because they are ignored), (c)
spec `data:` without a space and multi-line `data:`, neither of which
appears in the captures.

### The 0214 outage this pump still permits

[0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
probed OpenCode v2.1.4 with this provider's own `ServeArgs`. Rows 3–4:

| # | mcremote | v2.1.4 | Effect |
| --- | --- | --- | --- |
| 3 | `GET /global/health` | **200 `text/html`** (SPA fallback) | health poll passes; `OnHealthy` Debug-logs; empty version; `CheckMinVersion` skips |
| 4 | `GET /global/event` | **200 `text/html`** | no `data:` lines; body ends; `streamOnce` reconnects with backoff, silently |

0214 D12 specified the checks this record implements, for both dialects.
0214 is still `proposed` and is an ACP+lease rewrite. Waiting on it leaves
kilo and OpenCode 1.x unprotected.

### Stall vs stream

Default `TurnStallNoticeSeconds` is 120 for both OpenCode and kilo
(`internal/config/config.go:818, 852`; copied in
`internal/daemon/daemon.go:68-69, 161-162`). kilo `lifecycle.go:36`
records a field symptom of that delay: the phone sat on "running" until
the stall watchdog resynced "~2 minutes later". Stream liveness at 45 s
heals a dead *stream* 75 s before that nag. They measure different
things: `lastActivity` is stored on prompt submit (`session.go:1075`),
on stall-notice emit (`session.go:1186`), and on `dispatch`
(`session.go:1381`). Engine-global frames, including `server.heartbeat`,
never call `dispatch`.

## Decision Drivers

* Keep the homegrown engine. Session semantics stay in `httpagent`.
* A 200 that is the wrong media type, or a health body that is not the
  health document, must fail in one named error. HTML 200 persists
  forever (0214 row 3), so it is a hard reject, not a poll miss.
* A stream that is open and silent, or whose generation has moved, must
  reconnect under 0014 rather than wait for TCP keepalive or the 120 s
  stall watchdog.
* Parse event blocks the way WHATWG and the generated client parse them,
  so v2 comment keepalives are activity and a future `data:` without a
  space still dispatches.
* When kilo reports a seq gap, call the `Resync` 0137 already named,
  under the 0014 gates that already exist. `POST /sync/history` stays
  declined on the 28,351,089-byte probe.
* Constants are pinned to values the engines already emit and their
  own client already consumes.
* Each new gate is first seen to fail on planted input.

## Considered Options

* A: Adopt the engines' generated TypeScript HTTP client (or a Go rewrite of it) as the SSE pump
* B: Leave the pump as-is; take typed health only as part of 0214
* C: Ship only the three cheap items (typed health, event-block parser, gap-triggered resync) and defer stream liveness
* D: Harden the homegrown pump in place: stream liveness, typed health, a WHATWG event-block parser, and gap-triggered resync
* E: Add Last-Event-ID replay, EventSource, per-session SSE, or `types=` filtering

## Decision Outcome

Chosen option: "D: Harden the homegrown pump in place: stream liveness,
typed health, a WHATWG event-block parser, and gap-triggered resync",
because the four items are one "make the pump as strict and live as the
engines' own client" decision; because 0214 D12 is already specified and
kilo/1.x can have it now; and because stream liveness is the only item
that closes a hang 0014 cannot see (no reconnect means no resync).

Ranking inside D:

| Rank | Item | Why this rank |
| --- | --- | --- |
| **Most beneficial** | Stream liveness | 0014 runs after reconnect; this causes the reconnect. Stall default is 120 s; idle at 45 s is 75 s faster on a dead stream. |
| **Immediate 1** | Typed health + content-type (0214 D12) | Smallest change; HTML 200 becomes one error for both dialects. |
| **Immediate 2** | WHATWG event-block parser | Prerequisite for idle-timeout treating comments as activity. Matches generated `sse()`. |
| **Immediate 3** | Seq-gap and oversized-skip call 0014 resync | Detect half landed in 0137 Phase 7.1 (`TestSyncGapIsReported`); fill half is `session.resync`, already implemented. |

PLAN order: Immediate 1, 2, 3, then liveness. Parser before liveness so
the first liveness commit does not false-reconnect on v2 comments.

### Pinned shapes (PLAN does not invent these)

**Media-type compare** (both health and SSE). Copy the generated client
(`opencode/packages/client/src/promise/generated/client.ts:2711-2713`):

```text
strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])) == want
```

Empty or missing `Content-Type` fails. `httptest`/`http.Server` Write of
`{}` without a header sniffs as `text/plain; charset=utf-8` (Go 1.27.3
`http.DetectContentType`, measured 2026-10-31: `"{}"` and
`"{\"healthy\":true}"` → `text/plain; charset=utf-8`;
`"<!doctype html>"` → `text/html; charset=utf-8`).

**Health, inside the existing poll, on `StatusOK`.** Hard-fail (same
branch as `OnHealthy` error: `killTree`, return
`fmt.Errorf("%s health check rejected: %w", p.cfg.Bin, err)`) when any
of:

* media type ≠ `application/json`
* body does not unmarshal into `{Healthy bool, Version string}`
* `strings.TrimSpace(Version) == ""`
* `Healthy == false`

Connection errors and non-200 still poll. A 200 HTML SPA would otherwise
poll for the full 60 s; 0214 D12 asked for one log line.

Then call `OnHealthy(body)` if the dialect implements `HealthyHook`.
Dialect unit tests that feed non-JSON directly to `OnHealthy` stay.

Helper, exported for tests in the `httpagent` package (unexported is
enough): `func healthResponseOK(ct string, body []byte) error`.

**SSE, after `Do`, before reading the body.** If media type ≠
`text/event-stream`, return `fmt.Errorf("sse content-type %q, want text/event-stream", ct)`.
`pumpEvents` reconnects as it does for `sse status %d`.

**Event-block assembler** on top of `readSSELine` (keep that function and
`TestReadSSELineSkipsOversized`). WHATWG
[Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html):

* blank line ends an event
* line starting with `:` is a comment: counts as activity, no dispatch
* field name before first `:`; strip one leading U+0020 from the value
* `data` fields concatenate with U+000A; that payload is `DecodeFrame`'s
  input (today's `line[len("data: "):]`)
* `id` / `event` / `retry` ignored. Zero `Last-Event-ID` references in
  `internal/provider/**/*.go`.
* unterminated block at EOF dispatched if it has any `data` (generated
  client `client.ts:430`)
* assembled event cap **16 MiB** (`client.ts:336`
  `const maxSseEventBytes = 16 * 1024 * 1024`). Oversize is skippable,
  never fatal: consume to the next blank line, log, keep the stream.
  Generated client throws `SseEventTooLarge` and aborts; we diverge
  because this stream is shared across every session.

**StreamGapDialect**, optional, same shape as `CatalogEventDialect`
(`httpagent.go:540-542`, used at `provider.go:805-808`):

```go
type StreamGapDialect interface {
    TakeStreamGap() (sid string, ok bool)
}
```

`noteSyncSeq`, on a forward jump, besides the existing Warn, stores the
`aggregateID` in a consume-once field protected by `httpDialect.mu`
(the same mutex as `syncSeq`). After every `DecodeFrame` in `streamOnce`,
type-assert and if `ok` look up the session and call `s.resync()`.
OpenCode does not implement it. Do not route `typ == "sync"` to
`HandleEvent` (already true: `TestSyncFramesAreNotRoutedToASession`).

On assembler oversize skip, call `resyncSessions(gen)` (both dialects).

**Stream liveness**, all in `streamOnce` / client construction:

* Clone `http.DefaultTransport`; set `ResponseHeaderTimeout: 2 * time.Second`.
  Do not mutate the global. Do not set `DisableCompression` on the clone
  (REST catalogs are 4.3 MB, [0043-MADR-model-selection.md](0043-MADR-model-selection.md) D4).
* Keep `Client.Timeout = 0`.
* SSE GET: `http.NewRequestWithContext`, `Accept: text/event-stream`
  (already set), `Accept-Encoding: identity`.
* Context cancelled when `generation != gen || eng == nil || eng.url != url || closed`.
  A 100 ms ticker goroutine reading those fields under `p.mu` is enough;
  `defer cancel()` on `streamOnce` return.
* Idle: wrap `res.Body` so any `Read` that returns `n > 0` resets a
  45 s `time.AfterFunc` that `cancel()`s the same context. Comments
  therefore keep the stream up. On idle expiry `streamOnce` returns;
  `pumpEvents` reconnects with the existing 1–10 s backoff, reset after
  30 s up (`provider.go:746-755`), and `resyncSessions` runs on the new
  connect.

Constants:

| Knob | Value | Proof |
| --- | --- | --- |
| Header timeout | 2 s | OpenCode `packages/client/src/solid/connection.ts:32` `connectTimeout = 2_000`; httpagent health already 2 s (`provider.go:617`) |
| Idle timeout | 45 s | `connection.ts:35` `defaultIdleTimeout = 45_000`; comment "keepalive every 15 seconds" |
| Idle reset | any `Read` n>0 | `client.ts:423` `if (!next.done) requestOptions?.onActivity?.()`; tests `promise.test.ts:867-896` |
| Kilo keepalive | 10 s data | `handlers/global.ts:44-47` `Stream.tick("10 seconds")` + `drop(1)` |
| OpenCode v2 keepalive | 15 s comment | `event.ts:23-25` |
| Stall default | 120 s | `config.go:818, 852` |
| SSE Accept-Encoding | identity | kilo `middleware/compression.ts:6-12, 11-12, 47-48` excludes `text/event-stream` and `/global/event`; Go DefaultTransport otherwise sends `Accept-Encoding: gzip` (`transport.go:199-207, 3022-3025`) |
| Assembled cap | 16 MiB | `client.ts:336` |
| Line cap | 10 MiB | `maxSSELine`, keep |
| Backoff | 1–10 s, reset after 30 s | `provider.go:746-755`, keep |
| Health body cap | 64 KiB | `provider.go:622`, keep |
| `serverStartTimeout` | 60 s | `provider.go:32`, keep |

### Out of scope

* Adopting `@opencode/client`, Effect `HttpApi`, Hono `streamSSE`, or a
  Go rewrite of those.
* `Last-Event-ID` / SSE replay. No references in `internal/provider`.
  0014 is the recovery. Engines do not replay (0014; `streamOnce`
  comment `provider.go:774-776`).
* `EventSource`.
* Polling when SSE is down.
* Ending a turn on reconnect (0014 gates).
* Per-session SSE. Contract is one `/global/event`.
* `types=` filters. Engine-global `catalog.updated` is load-bearing
  ([0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md) F8;
  OpenCode `EngineEventInvalidatesCatalog` at `http.go:205-207`).
* `POST /sync/history`. Probe: 28,351,089 bytes, 13,410 events, 153
  aggregates; listing one aggregate removed 1 event
  ([0137-PLAN-prompt-to-first-token-latency-regression.md](0137-PLAN-prompt-to-first-token-latency-regression.md)
  Phase 7.1).
* Typing `apiAt` REST content-type. D12 named health and SSE.
* Coalescing ([0024-MADR-stream-coalescing.md](0024-MADR-stream-coalescing.md)),
  stall-notice text, dialect event vocabularies.
* OpenCode v2 path/auth/ACP (0214). This record makes a v2 binary on
  the 1.x dialect fail spawn with a named health error.

### Consequences

* Good, because a silent open stream reconnects in 45 s and a hung
  handshake dies in 2 s, 75 s before the 120 s stall nag on a dead
  stream.
* Good, because the next HTML-200 engine-shape break, kilo or OpenCode,
  is `health check rejected: …` at spawn.
* Good, because a kilo seq gap or an oversized skipped event calls the
  `Resync` that already has tests, without a 28 MB history fetch.
* Good, because v2 comment keepalives become idle-activity, and
  `data:` without a space still dispatches.
* Good, because D12 lands for kilo and OpenCode 1.x while 0214 is
  still proposed.
* Neutral, because an engine that stopped emitting keepalives
  reconnects every 45 s. Both current engines emit them; a regression
  shows up as reconnect logs.
* Neutral, because `OnHealthy` dialect unit tests that feed non-JSON
  stay green.
* Bad, because an OpenCode v2 binary on the 1.x dialect fails spawn.
  That binary is already unusable on this dialect (0214 rows 3–8).
* Bad, because `TestEngineTeardownSupervisesTheDescendantTree` boots
  through `startServer` against a helper that writes `{}` with no
  `Content-Type` (`supervise_wiring_test.go:114-117`). After Immediate 1
  that helper must send `Content-Type: application/json` and
  `{"healthy":true,"version":"test"}`. Sniff of `{}` is
  `text/plain; charset=utf-8`, which fails the media-type check even
  with a JSON body and no header.
* Bad, because a 45 s idle reconnect during a long pause with
  keepalives disabled would resync turn-active sessions. Cost is the
  existing 0014 REST; idle sessions stay free (`TestResyncGatesOnTurnState`).

### Confirmation

* Unit tests in the matrix in More Information. Each new gate is first
  seen to fail on planted input (HTML 200, silent connection, `seq`
  jump, oversized event, comment-only block), on a scratch copy, then
  the production assertion is written. The implementing PLAN records
  what that failure looked like.
* `TestReadSSELineSkipsOversized`, `TestStreamConnectTriggersResync`,
  `TestResyncGatesOnTurnState`, `TestSyncGapIsReported`,
  `TestSyncFramesAreNotRoutedToASession`,
  `TestFixtureSyncFramesAdvanceContiguously`,
  `TestDecodeFrameHeartbeatHasNoSession` still pass.
* `make test` and `make race` pass.
* `make live-kilo` and `make live-opencode` (owner's word on quota)
  confirm, with no prompt: health JSON + non-empty version + media type
  `application/json`; SSE media type `text/event-stream`; a keepalive
  inside 45 s. OpenCode live `waitForHealth`
  (`live_http_test.go:1184-1217`) currently accepts any 200 and does
  not check `Content-Type`; after this item it still passes against
  1.x because the engine already returns JSON health
  (`live_http_test.go:1124-1137` unmarshals `{healthy, version}`).
* This record stays `proposed` until the owner accepts it. A PLAN is
  written only when asked. Implementation waits on explicit approval
  of the pair.

## Pros and Cons of the Options

### A: Adopt the engines' generated TypeScript HTTP client (or a Go rewrite of it) as the SSE pump

The OpenCode v2 client is generated (`packages/httpapi-codegen`, emitted
at `packages/client/src/promise/generated/client.ts`). Kilo's current
server is Effect `HttpApiBuilder` plus `effect/unstable/encoding/Sse`.
The 1.x captures were produced by Hono-era `streamSSE` (no `event:`
fields in those files). None of these is a Go SDK.

* Good, because generated `sse()` (`client.ts:401-450`) already does
  content-type checks, event-block assembly, a 16 MiB cap, and
  `onActivity`.
* Neutral, because those four behaviours are small and are copied as
  ideas in D.
* Bad, because that client has no 0014 resync, no turn gates, no stall
  watchdog, no coalescing, no catalog invalidation, no
  generation-scoped pump, and aborts on oversize. `httpagent` already
  has those.
* Bad, because the generated client is TypeScript and per-engine
  envelope (kilo wraps `payload`, OpenCode uses `payload` or bare).

### B: Leave the pump as-is; take typed health only as part of 0214

* Good, because 0214 D12 already specifies the checks.
* Bad, because 0214 is still `proposed` and is an ACP+lease rewrite.
  D12 is the piece kilo needs too.
* Bad, because stream liveness, the parser, and the gap-resync wire
  are not in 0214.

### C: Ship only the three cheap items and defer stream liveness

* Good, because Immediate 1–3 are independently testable.
* Good, because the parser is a prerequisite for comment activity.
* Bad, because the hang class stays open. 0014 cannot heal a stream
  that never reconnects.
* Neutral, because C is a valid phase split inside D. Choosing D
  forbids treating liveness as optional; it does not forbid shipping
  1–3 first.

### D: Harden the homegrown pump in place (chosen)

* Good, because it copies the four behaviours the engines' clients got
  right and keeps the session semantics they lack.
* Good, because production edits are `httpagent/provider.go`,
  `httpagent/httpagent.go` (one new optional interface),
  `kilo/sync.go` / `kilo/dialect.go` (gap slot). OpenCode production
  code is untouched.
* Good, because constants are pinned to measured emit intervals and
  to the client the engines already ship.
* Neutral, because a PLAN can still land Immediate 1–3 before
  liveness.
* Bad, because the supervise-wiring helper and any other fake engine
  that serves `{}` must grow a JSON content type and a version.

### E: Add Last-Event-ID replay, EventSource, per-session SSE, or `types=` filtering

* Good, because Last-Event-ID is in the spec.
* Bad, because neither engine replays. 0014 exists for that reason.
  Zero `Last-Event-ID` references under `internal/provider`.
* Bad, because `EventSource` cannot skip an oversized event, cannot
  cancel on generation, and cannot expose comment activity.
* Bad, because `types=` would drop `catalog.updated` (F8;
  2 frames in the 1.18.26 capture, load-bearing).

## More Information

### Fact, inference, decision

| Kind | Claim |
| --- | --- |
| Fact | Health poll accepts any 200; `OnHealthy` returns nil on non-JSON. |
| Fact | SSE accepts any 200; prefix `data: ` with required space. |
| Fact | `Client{Timeout:0}`, no Transport, SSE `NewRequest` → `context.Background`. |
| Fact | Go 1.27.3 DefaultTransport: Dialer Timeout/KeepAlive 30 s, IdleConnTimeout 90 s (pool), ResponseHeaderTimeout 0, DisableCompression false (sends gzip). |
| Fact | 1.x fixtures: 0 comments, 0 `data:` without space, 0 multi-line `data:`, 0 `event:`/`id:`/`retry:`. |
| Fact | kilo 7.5.6: 18/56 frames are `sync`, seq 0..17 contiguous, 0 gaps. |
| Fact | kilo source emits 10 s data heartbeats with `event: message`; OpenCode v2 source emits 15 s `: heartbeat` comments. |
| Fact | `noteSyncSeq` only logs; kilo `Resync` is implemented and tested. |
| Fact | Stall default 120 s; OpenCode client idle 45 s, connect 2 s. |
| Fact | 0214 v2.1.4 probe: `/global/health` and `/global/event` are 200 `text/html`. |
| Fact | `POST /sync/history` 28,351,089 bytes / 13,410 events / 153 aggregates. |
| Fact | `DetectContentType("{}")` is `text/plain; charset=utf-8`. |
| Inference | TCP keepalive (30 s) can notice a vanished peer; it cannot notice a peer that ACKs and sends no SSE bytes. Idle timeout is for that case. |
| Inference | `IdleConnTimeout` 90 s does not bound an active SSE body read. |
| Decision | HTML 200 health is a hard reject, not a 60 s poll miss. |
| Decision | Oversize is skippable (shared stream); generated client abort is the rejected alternative. |
| Decision | Gap hook is `StreamGapDialect.TakeStreamGap`, copied from `CatalogEventDialect`. |

### Failure-mode matrix

| Failure | Today | After |
| --- | --- | --- |
| Engine serves 200 `text/html` health (0214 row 3) | Poll succeeds; empty version; Start proceeds | `health check rejected` names the content type; process killed |
| Engine serves 200 `text/html` SSE (0214 row 4) | Reconnect loop, no frames, Debug `sse stream ended` | `sse content-type "text/html", want text/event-stream`; reconnect still, log is one line |
| Engine hung, TCP open, no bytes | Stall at 120 s may resync the turn; stream stays | Idle cancel at 45 s; reconnect; 0014 resync on connect |
| Peer vanished, no RST | Dialer KeepAlive 30 s may eventually fail the read | Same, plus 45 s idle as a ceiling |
| `RestartForCredentialChange` / respawn | New `pumpEvents`; old GET uses `context.Background` until TCP dies | Old GET context cancelled on generation/eng/closed |
| kilo `seq` jumps 2 → 5 on a live stream | Warn `missed=2`; stall at 120 s is the heal | Warn + `s.resync()` immediately (0014 gates) |
| Oversized SSE line dropped (`provider.go:789-790`) | Warn; stall at 120 s is the heal (0014 trigger 2) | Warn; `resyncSessions(gen)` |
| OpenCode v2 `: heartbeat\n\n` | Ignored (no `data: ` prefix); idle timeout would false-fire if landed first | Comment is activity; no `DecodeFrame` |
| `data:{"type":"x"}` (no space) | Dropped | Dispatched |
| Two `data:` lines, one block | Two `DecodeFrame`s (broken JSON each) | One concatenated payload |

### Call-site map (production)

| Symbol | File | Role after this record |
| --- | --- | --- |
| `NewWithLogger` | `provider.go:134-157` | Clone Transport; `ResponseHeaderTimeout=2s`; `Timeout=0` |
| `startServer` health loop | `provider.go:603-650` | Call `healthResponseOK` before `OnHealthy`; hard-fail on typed error |
| `pumpEvents` | `provider.go:731-757` | Unchanged control flow |
| `streamOnce` | `provider.go:759-829` | Context, content-type, assembler, gap hook, oversize → `resyncSessions` |
| `readSSELine` | `provider.go:840-859` | Unchanged; assembler uses it |
| `resyncSessions` | `provider.go:865-878` | Also called on oversize skip |
| `session.resync` | `session.go:1195-1208` | Unchanged gates |
| `session.dispatch` | `session.go:1378-1392` | Unchanged; still the only `lastActivity` touch from frames |
| `session.watchStall` | `session.go:1142-1188` | Unchanged; 120 s default |
| `HealthyHook` | `httpagent.go:387-392` | Unchanged; transport now pre-filters |
| `CatalogEventDialect` | `httpagent.go:540-542` | Pattern to copy |
| `StreamGapDialect` | `httpagent.go` (new) | After `DecodeFrame` |
| `noteSyncSeq` | `kilo/sync.go:42-66` | On gap, set pending sid |
| `DecodeFrame` (kilo) | `kilo/dialect.go:171-198` | Still the only `noteSyncSeq` caller |
| `DecodeFrame` (opencode) | `opencode/http.go:842-859` | Unchanged |
| `OnHealthy` (both) | `opencode/http.go:659-705`, `kilo/version.go:27-56` | Unchanged |
| kilo `Resync` | `kilo/resync.go` | Unchanged; now reachable from a live-stream gap |

`DecodeFrame` production callers: one (`streamOnce`). Test callers:
kilo `dialect_test.go`, `sync_test.go`, `session_test.go`; OpenCode
`fixture_test.go`.

`session.resync` callers today: `resyncSessions` (connect),
`watchStall`, and tests in `session_test.go`. After: plus gap hook and
oversize skip.

### httpagent inventory (2026-10-31)

Production: `provider.go` 1,275 lines, `session.go` 1,923, `httpagent.go`
738, plus `authcatalog.go`, `connected.go`, `deviceauth.go`, `runtime.go`.
SSE pump, health poll, client, and parser all sit in `provider.go`.
Tests that must change or grow are `session_test.go` (458 lines) and
`supervise_wiring_test.go` (253). Other `*_test.go` files in the package
do not drive `streamOnce` / health poll.

### Test matrix

**Stay green, no edits expected**

* `TestReadSSELineSkipsOversized` — line reader still skippable
* `TestResyncGatesOnTurnState` — gap-triggered resync uses the same `resync`
* `TestStallResyncEndsGhostTurnWithoutNotice`, `TestStallNoticeStillFiresWhenTurnIsLive`
* kilo `TestSyncGapIsReported` (log still fires), `TestFirstSyncSightingIsNotAGap`, `TestSyncSequenceGoingBackwardsIsNotAGap`, `TestSyncFramesAreNotRoutedToASession`, `TestFixtureSyncFramesAdvanceContiguously`
* kilo `TestDecodeFrameHeartbeatHasNoSession`
* kilo `TestOnHealthyRecordsVersion` non-JSON branch; OpenCode `version_test.go` "malformed body is tolerated"
* kilo `resync_test.go` (Resync itself)

**Will fail until the helper is updated**

* `TestEngineTeardownSupervisesTheDescendantTree` via
  `TestHelperProcessServesHealth` (`supervise_wiring_test.go:114-117`):
  writes `{}`, no `Content-Type`, path `/health` (fake `HealthPath`).
  Must set `Content-Type: application/json` and body
  `{"healthy":true,"version":"test"}`. SSE helper already sets
  `text/event-stream` (line 122).

**Already sets SSE content-type (stays valid; will also satisfy Immediate 1 SSE check)**

* `TestStreamConnectTriggersResync` (`session_test.go:356`)

**Add, httpagent package**

* `healthResponseOK`: 200 `text/html` + HTML body → error names type;
  `application/json` + `{"healthy":true}` → error empty version;
  `application/json` + `{"healthy":false,"version":"1"}` → error;
  `application/json; charset=utf-8` + `{"healthy":true,"version":"1.2.3"}` → nil;
  missing header + `{"healthy":true,"version":"1"}` → error (sniff is not used on the client; missing header fails the compare).
* `streamOnce` 200 `text/html` → error `sse content-type`.
* `streamOnce` 200 `text/event-stream; charset=utf-8` → accepted.
* Assembler: `: heartbeat\n\n` → no `DecodeFrame`; `data: {…}\n\n` and
  `data:{…}\n\n` both dispatch; two `data:` lines concatenate with `\n`;
  `event: message\nid: x\nretry: 1000\ndata: {…}\n\n` payload unchanged;
  oversized event skipped, next event parses, `resync` called if
  turn-active.
* Liveness: a handler that writes headers and no body is cancelled
  inside ~45 s (test with a 200 ms fake idle); a handler that writes
  `: heartbeat\n\n` every 50 ms is not cancelled; bumping `generation`
  cancels an in-flight GET (httptest handler blocked on `<-ctx.Done()`).

**Add, kilo**

* Two `sync` frames seq 3 then 7, registered turn-active session →
  `Resync` once, `HandleEvent` not called with `sync`. Idle session →
  no `Resync`. Seq 3 then 4 → no `Resync`. Warn still contains
  `missed=3`.

**Planted-fail protocol**

Each new assertion is first run against a scratch copy of the test with
the production check omitted or inverted, so the failure is seen
(HTML 200 currently passes spawn; seq jump currently does not call
`Resync`; comment-only currently would trip idle if idle landed before
the parser). Record the failure text in the PLAN execution log.

### Error strings (for test matching)

* `%s health check rejected: health content-type %q, want application/json`
* `%s health check rejected: health body is not JSON: %v`
* `%s health check rejected: health version is empty`
* `%s health check rejected: health reports healthy=false`
* `sse content-type %q, want text/event-stream`
* existing `sse status %d` stays for non-200
* existing `dropping oversized SSE line` stays; assembler adds a sibling
  `dropping oversized SSE event` with `limit_bytes=16777216`

### Wire examples (from fixtures; paths redacted)

kilo 7.5.6 heartbeat (JSONL payload, empty properties):

```json
{"payload":{"id":"evt_06852ba6b0015eDKDZ44TAtGIj","type":"server.heartbeat","properties":{}}}
```

OpenCode 1.18.26 heartbeat (JSONL):

```json
{"id":"evt_068535a2c001C3j4zNQH9z2vGs","type":"server.heartbeat","properties":{}}
```

kilo 7.4.20 raw SSE shape (every event in `sse-or.raw` / `sse-global.raw`):

```text
data: {"payload":{…}}\n
\n
```

OpenCode v2 source keepalive (not in 1.x fixtures):

```text
: heartbeat\n
\n
```

kilo current source event (not in 7.4.20 raw captures):

```text
event: message\n
data: {…}\n
\n
```

kilo sync twin (7.5.6 JSONL; 18 of 56 frames). `DecodeFrame` records seq
and returns `typ="sync"`, `sid=""`:

```json
{"payload":{"type":"sync","syncEvent":{"id":"evt_…","type":"message.updated.1","seq":0,"aggregateID":"ses_…"}}}
```

### Relationship to other records

* [0014-MADR-sse-reconnect-resync-decision.md](0014-MADR-sse-reconnect-resync-decision.md)
  remains the resync protocol. This record adds two triggers (seq gap,
  oversized skip) and makes reconnect more likely (liveness). Gates
  unchanged.
* [0024-MADR-stream-coalescing.md](0024-MADR-stream-coalescing.md)
  untouched. Assembler still hands one JSON payload to `DecodeFrame`.
* [0043-MADR-model-selection.md](0043-MADR-model-selection.md) D4: 4.3 MB
  `/provider` fetch; reason the shared Transport keeps gzip for REST.
* [0075-MADR-kilo-cli-provider.md](0075-MADR-kilo-cli-provider.md)
  kilo-on-httpagent, un-gated loopback, no Authorization: unchanged.
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md)
  F7 detect landed; F7 fill lands here; F8 catalog invalidation already
  landed; `POST /sync/history` stays declined.
* [0137-PLAN-prompt-to-first-token-latency-regression.md](0137-PLAN-prompt-to-first-token-latency-regression.md)
  Phase 7.1 is the 28 MB probe and the "filled by the existing resync"
  sentence this record makes true on a live stream.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
  D12 specified here, implemented here. When 0214 is accepted it should
  point here rather than re-specify the checks. ACP/lease/v2 `/api`
  remain 0214.

### Implementation order for a future PLAN

1. Immediate 1 — `healthResponseOK` + SSE content-type; fix
   `supervise_wiring_test.go` helper; HTML-200 tests.
2. Immediate 2 — assembler on `readSSELine`; parser tests; keep
   skippable cap.
3. Immediate 3 — `StreamGapDialect` + kilo pending sid; oversize →
   `resyncSessions`; kilo gap-resync test.
4. Stream liveness — cloned Transport, request context, idle body
   wrapper, generation ticker; liveness tests.

Do not land 4 before 2: v2 `: heartbeat` would look idle.

Do not mutate `http.DefaultTransport`. Clone it. The in-repo precedent
is `internal/relayhost/client.go:360-377`.

Host identifiers never appear in the implementing diff. Fixture bodies
that already carry redacted paths stay redacted.

### Peer source pins (reference only)

* OpenCode checkout `/Users/<user>/gitrepos/opencode` HEAD `237a847056`.
  Event handler last touched by `1434464f7e`.
* Kilo checkout `/Users/<user>/gitrepos/kilocode` HEAD `0a9faeff69`.
  `handlers/global.ts` last touched by `305588ecea`.
* Go 1.27.3 GOROOT `net/http/transport.go:47-58` DefaultTransport;
  `transport.go:199-207` DisableCompression; `transport.go:231-235`
  ResponseHeaderTimeout; `client.go:92-98` Timeout zero; `request.go:871-873`
  NewRequest uses Background.
