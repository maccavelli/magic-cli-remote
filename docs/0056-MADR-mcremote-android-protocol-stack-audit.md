# MADR 0056: mcremote ↔ Android protocol-stack audit

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Date:** 2026-07-30
- **Status:** **Accepted (partial)** — phases 0–7 of the companion plan
  implemented; H-5b service-owned socket (phase 9) deferred
- **Baseline:** `fa21393` (`master`)
- **Re-verified:** 2026-07-30 (independent deep-dive pass; findings below still
  present in tree; markdown session-window pass added same day)
- **Scope:** mcremote WebSocket control plane, session manager and durable store,
  outbound relay host; Android/Flutter WebSocket client, relay bridge,
  transcript reconciliation, notifications, foreground service, lifecycle, and
  session-chat markdown rendering
- **Related:** [protocol v1](protocol-v1.md),
  [implementation plan 0056](0056-PLAN-mcremote-android-protocol-stack-remediation.md),
  [MADR 0046](0046-MADR-mobile-debug-pass.md),
  [MADR 0018](0018-MADR-mobile-chat-performance-action-plan.md),
  [MADR 0027](0027-MADR-opencode-streaming-rendering.md),
  [MADR 0045](0045-MADR-mobile-app-hardening-audit.md) (P2/P4 status),
  [server remediation plan 0055](0055-PLAN-mcremote-server-remediation.md)
- **Repository standards:** [Go networking](standards/go/network.md),
  [Go sessions](standards/go/session.md),
  [mobile networking](standards/mobile/networking.md),
  [Android](standards/mobile/android.md)

> **Note on numbering:** there is no MADR 0058. This document is the
> cross-stack protocol audit. Plans 0054/0055 cover earlier server hardening;
> this MADR is the current backlog for mcremote ↔ Android communication.

## 1. Decision

Accept the findings in this record as the current cross-stack hardening backlog.
Remediation is sequenced in
[0056-PLAN-mcremote-android-protocol-stack-remediation.md](0056-PLAN-mcremote-android-protocol-stack-remediation.md)
(phases 0–9, locked decisions D1–D11, red-then-green tests). No critical/P0
defect was found. Six high-severity correctness, security, data integrity, or
product-reliability defects should be fixed before expanding the protocol:

1. Move reconnect reconciliation out of mounted chat routes and into a
   connection-scoped session synchronizer.
2. Give mutating requests bounded server lifetimes and idempotent retry
   semantics.
3. Make the relay bridge lossless or fail closed; never discard bytes from an
   opaque TCP/TLS stream.
4. Make ownership persistence a fail-closed part of create and legacy claim.
5. Make Android background connection ownership match the foreground-service
   promise, or explicitly narrow that promise.
6. Do not let an incomplete or malformed `session.list` destructively evict the
   phone's last transcript copy.

Medium findings tighten frame limits, protocol validation, crash durability,
relay shutdown, multi-page history integrity, outer-stream fail-closed
behavior, gap-clear semantics, and message-level feature parity. The final
section defines an implementation order and acceptance gates.

## 2. Executive assessment

The stacks are substantially stronger than the historical audits imply. TLS
identity, client-key enrolment, origin filtering, inbound request sizing,
per-client output queues, event ordering, session ownership filters, durable
history, reconnect backoff, and Android manifest declarations are all present
and generally coherent.

The remaining risks are mostly **cross-boundary failures**. Each side is locally
reasonable, but their combined semantics are unsafe:

- the daemon exposes history and sequence numbers, but only a mounted chat asks
  for reconciliation;
- the phone times out requests, but the daemon detaches their work and has no
  idempotency ledger;
- the relay carries a byte stream, but the phone bounds it by deleting old
  frames;
- the daemon calls `session.list` authoritative while silently omitting corrupt
  rows, and the phone reacts by deleting its cache;
- Android declares a foreground messaging service, but the service owns no
  messaging connection or restart state.

These defects are not detected by the currently visible tests.

## 3. System traced

```text
Android UI / notification action
        |
        v
McremoteClient -- direct WSS --------------------------+
        |                                              |
        +-- mcrelay WSS -> loopback TCP -> inner WSS --+
                                                       |
                                                       v
                                             internal/ws.Server
                                                       |
                                  auth + owner filter + request dispatch
                                                       |
                                                       v
                                            session.Manager / Store
                                                       |
                                                       v
                                          provider process / HTTP / ACP
```

For every finding below, the audit followed the request, response, event,
disconnect, persistence, and recovery paths on both sides rather than reviewing
files in isolation.

## 4. Severity model and index

| Severity | Meaning |
|---|---|
| **High** | Data/security boundary failure, permanent state divergence, core background promise failure, or user-visible correctness loss with no automatic repair |
| **Medium** | Real robustness, compatibility, durability, or incomplete-functionality gap with a bounded trigger or recovery |
| **Low** | Documentation drift, diagnostic weakness, or low-impact cleanup |

| ID | Sev | Area | Finding |
|---|---|---|---|
| H-1 | High | Reconnect/history | Inactive sessions can miss events permanently because reconciliation is route-local |
| H-2 | High | Request/session lifecycle | Client timeouts, detached server work, and non-idempotent mutation retries create ambiguous and duplicate outcomes |
| H-3 | High | Android relay | Relay buffering deletes bytes from an opaque TLS stream and has no byte budget |
| H-4 | High | Ownership/security | Session create and legacy ownership claims acknowledge success when owner persistence fails |
| H-5 | High | Android background | The foreground service owns no socket or recovery state, so process/service restart breaks the advertised background behavior |
| H-6 | High | Session list/cache | Partial or malformed authoritative lists can erase the phone's last transcript snapshot |
| M-1 | Medium | Framing/history | The 512 KiB history cap is an estimate that ignores most event payloads; outbound frames have no exact cap |
| M-2 | Medium | Compatibility | Protocol version, WebSocket message kind, and response type are not consistently validated |
| M-3 | Medium | Durability/performance | Sliding history debounce has an unbounded crash-loss window and rewrites the full ring |
| M-4 | Medium | Feature parity | Provider message/part IDs never reach Android, leaving message-level fork/diff/revert unwireable |
| M-5 | Medium | Relay lifecycle | Host tunnels are detached from daemon/control cancellation |
| M-6 | Medium | Android relay | Outer stream errors are swallowed; the tunnel can hang instead of fail closed |
| M-7 | Medium | History paging | A truncated multi-page history fetch can return a partial ring as if complete |
| M-8 | Medium | Reconnect/history | Suspected gaps never clear when history events lack `seq` |
| M-9 | Medium | Chat markdown | Streaming isolate renderer is a GFM subset; tables/strike/images/task boxes/list numbers collapse or look raw until finalize |
| M-10 | Medium | Chat markdown | Stream closer only seals `**`, `` ` ``, and fences — open `*`, `~~`, `[links](` flash literally |
| M-11 | Medium | Chat markdown | Past 4000 chars, streaming intentionally switches to monospace plain text (raw markers) until the turn ends |
| M-12 | Medium | Chat markdown | Up to three different render engines per turn cause layout snap and “processing” jank in session chat |
| L-1 | Low | UX/spec drift | Android still tells users host history is live-only although history is durable |
| L-2 | Low | Doc drift | `sessionHistory` dartdoc claims empty-on-error but the implementation throws |
| L-3 | Low | Chat markdown | Scroll-suppressed stream updates are not rescheduled on scroll end without a new chunk |
| L-4 | Low | Chat markdown | Closed strikethrough is stripped of decoration on the isolate path |

## 5. High findings

### H-1 — Inactive sessions can remain permanently incomplete

#### Evidence

- On every transition back to connected,
  `apps/mobile/lib/state/transcripts_notifier.dart:200-209` marks every known
  session's `_seqGapSuspected` flag.
- That notifier does not fetch history. The actual list/history RPCs live in
  `ChatScreen._resyncAfterReconnect`
  (`apps/mobile/lib/features/chat/chat_screen.dart:251-277`) and are triggered
  only by the connection listener of an already-mounted chat
  (`:1957-1968`).
- The sessions screen refreshes metadata and calls `syncFromMeta`, but never
  fetches transcript history
  (`apps/mobile/lib/features/sessions/sessions_screen.dart:135-200,1196-1200`).
- Opening that chat later does not repair it:
  `_maybeReplayHistory` returns immediately when the local transcript already
  has any items (`chat_screen.dart:329-342`). It does not consult the suspected
  gap.

#### Failure sequence

1. Session A has a populated phone transcript but is not the mounted chat.
2. The socket drops; A emits events while the phone is offline.
3. Reconnect marks A's gap flag, but only the currently mounted route (if any)
   fetches history.
4. The user later opens A. Because it is populated, history replay is skipped.
5. No later action consumes A's gap flag. Missing assistant chunks, tool
   terminal states, permissions, or `turn_complete` remain absent until another
   reconnect happens while A is mounted.

This can leave the transcript incomplete and the composer/status state stale
despite the daemon holding the repair data.

#### Decision

Connection-scoped state, not a screen, owns reconciliation. On reconnect:

- fetch one authoritative session snapshot;
- enqueue history resync for every locally known session with a last sequence
  or suspected gap, with bounded concurrency;
- reconcile pending asks once;
- let screens observe the result.

As an immediate safety fix, chat open must fetch/reconcile history whenever the
notifier reports a suspected gap, even when the transcript is populated.

#### Acceptance

- A test disconnects while A is inactive, emits events, reconnects, then opens
  A without another connection transition. A contains every retained event and
  the gap flag clears.
- Reconciliation deduplicates live events that arrive while history is paging.
- One bad/slow session does not prevent the rest from reconciling.

### H-2 — Mutation completion is ambiguous and retries are not idempotent

#### Evidence

- Slow handlers run through `dispatchAsync`; it detaches them with
  `context.WithoutCancel(ctx)` and adds no generic deadline
  (`internal/ws/server.go:640-690`).
- The comment says handlers bind their own work, but this is not a protocol
  invariant and the manager/provider call receives the detached context.
- Each connection has eight async slots. A handler that never returns keeps a
  slot forever; the code explicitly logs that this can permanently produce
  `rate_limited` (`server.go:647-665`).
- Android's `request` timeout only removes its completer
  (`apps/mobile/lib/data/ws/mcremote_client.dart:1582-1620`). It sends no
  cancellation and cannot learn whether the daemon completed later.
- Normal Android `session.create` calls omit `session_id`
  (`mcremote_client.dart:1888-1922`), so the manager generates a fresh UUID
  (`internal/session/manager.go:280-292`).
- Supplying a stable `session_id` alone is still insufficient: duplicate create
  is defined as close-and-replace (`manager.go:294-349`), not replay of the
  original result.

#### Failure sequence

1. A create/prompt/rename reaches the provider and takes longer than 30 seconds.
2. Android reports a timeout and forgets the request.
3. The daemon continues after disconnect because cancellation was stripped.
4. A retry can create a second session, submit a second prompt, or replace an
   already-successful session.
5. On a live socket, repeated hung work can consume every async slot. After the
   phone has gone, the detached goroutines/provider work can still remain.

This is an at-least-once transport exposed as if it were request/response, with
no operation identity or reconciliation contract.

#### Decision

- Derive all work from a daemon-lifecycle context and add operation-specific
  deadlines. Disconnect grace may be explicit for create, but must still be
  bounded and cancelled on daemon shutdown.
- Add a bounded idempotency ledger keyed by `(device_id, request_id)` for
  mutating requests. Store in-progress and terminal results long enough to
  cover reconnect/retry; a duplicate receives the original result and never
  re-executes.
- Allow Android to preserve a request ID across a retry. A new cancellation
  message can be added for operations whose providers support safe
  cancellation.
- Reconcile timed-out creates through the idempotency result or a stable
  client-operation ID; do not infer failure from timeout.

This directly enforces the repository networking standard: bounded contexts
must derive from request or server lifecycle, and network goroutines must not
be detached from cancellation.

#### Acceptance

- Drop the response after provider `Start` succeeds, retry the same request, and
  assert exactly one provider process/session exists.
- Timeout and disconnect a deliberately blocked handler; it releases its async
  slot by the operation deadline.
- Daemon shutdown cancels every async handler and relay tunnel within a bounded
  drain deadline.
- A duplicate prompt never reaches the provider twice.

### H-3 — Android relay overflow corrupts the TLS stream

#### Evidence

- `RelayTransport` buffers outer binary frames until the loopback peer accepts
  (`apps/mobile/lib/data/ws/relay_transport.dart:162-185`).
- At 64 frames it calls `_outerBuf.removeFirst()` and then appends the new frame
  (`:180-184`).
- Those frames are explicitly an opaque TCP/TLS byte pipe (`:17-24`). Removing
  any byte from TCP/TLS is unrecoverable stream corruption, not a lossy event
  policy.
- The limit counts frames, not bytes. mcrelay's default frame limit is 1 MiB
  (`internal/relay/config.go:52`), so this queue can retain roughly 64 MiB plus
  copies before a peer attaches.
- Unexpected text after `join_ok` is logged and dropped rather than terminating
  the corrupted splice (`relay_transport.dart:162-168`).
- Relay tests cover URL/join-plane parsing only; none exercises byte order,
  overflow, peer-attach races, or teardown.

#### Impact

The current overflow policy guarantees a later TLS parse/MAC failure or timeout
while hiding the actual cause. It also exposes a significant pre-accept memory
spike on a mobile device.

#### Decision

- Preserve every byte in order or close the transport immediately with a typed
  `relay_buffer_overflow` error.
- Bound both frames and exact bytes; choose a byte cap below the relay's maximum
  plus a small TLS/HTTP handshake allowance.
- Treat any post-join text/control-plane payload as a protocol violation and
  close both legs.
- Add back-pressure where the WebSocket API permits it; otherwise fail fast
  rather than pretending the tunnel remains valid.

#### Acceptance

- A deterministic test fragments a TLS-like byte sequence into arbitrary relay
  frames, delays peer accept, and asserts exact byte equality after flush.
- Overflow produces one typed failure and zero bytes are silently discarded.
- A maximum-size frame cannot make retained memory exceed the configured byte
  cap.

### H-4 — Ownership persistence fails open

#### Evidence

- A disk-only legacy record is first-touch claimed in
  `Manager.Authorize`. If `Store.Save` fails, the manager logs a warning but
  still returns success (`internal/session/manager.go:704-730`).
- A live legacy claim calls `persistNow`, whose `writePersist` also logs and
  swallows `Store.Save` failure (`manager.go:1420-1520`).
- New session creation inserts a live owner, starts the event pump, calls
  `persistNow`, and returns success regardless of persistence failure
  (`manager.go:423-445`).

#### Failure sequence

1. Device A claims or creates a session while the disk is full, read-only, or
   otherwise failing.
2. The mutating operation succeeds and the in-memory owner protects the live
   process.
3. The daemon restarts. The record is still unowned, missing, or stale.
4. Device B can first-touch claim the legacy record, or A's durable session
   disappears.

Ownership is a security boundary, but its durable transition currently has
best-effort status semantics.

#### Decision

Split security-critical persistence from advisory status persistence:

- create and first ownership claim must synchronously persist the owner;
- failure returns a typed error and rolls back or refuses the mutation;
- status/history updates may remain best-effort but must expose degraded
  persistence in health/diagnostics.

#### Acceptance

- Inject `Store.Save` failure for live claim, disk-only claim, and create.
- No caller receives success; no provider mutation occurs after a failed claim;
  restart cannot broaden ownership.
- A successful response proves the owner record is durable.

### H-5 — Foreground service does not own background communication

#### What is correct

The manifest declares `FOREGROUND_SERVICE`,
`FOREGROUND_SERVICE_REMOTE_MESSAGING`, a non-exported service, and
`android:foregroundServiceType="remoteMessaging"`
(`apps/mobile/android/app/src/main/AndroidManifest.xml:15-18,96-108`).
Android defines this type for transferring text messages between devices and
requires no type-specific runtime prerequisites. The declaration therefore
matches the product use case.

#### Gap

- The foreground callback says it exists only to keep the process and
  **main-isolate** WebSocket alive
  (`apps/mobile/lib/data/notifications/foreground_service.dart:4-7`).
- Its task handler does no start, repeat, or destroy work (`:13-22`).
- Credentials, `McremoteClient`, event subscriptions, pending asks, and
  reconnect policy all remain in the UI/main isolate.
- The manifest enables the plugin restart receiver, but a restarted callback
  cannot reconstruct the protocol client. The foreground notification may say
  “Connected to host / Listening” even though no socket or subscription was
  recreated.
- Android 12+ generally forbids starting a foreground service while the app is
  already backgrounded. Yet connection and connectivity callbacks may call
  `_service.start()` from the background
  (`notification_coordinator.dart:210-243`;
  `app_lifecycle.dart:89-104`). The exception is caught and reduced to a debug
  line, leaving no scheduling fallback.
- Startup invokes `coord.start()` and `coord.setEnabled()` concurrently
  (`app_lifecycle.dart:49-78`). The latter may start the service before
  notification initialization/permission handling finishes.

Android's official guidance says apps targeting API 31+ cannot normally start
an FGS from the background and receive
`ForegroundServiceStartNotAllowedException` outside enumerated exemptions.
The code's “best effort” catch prevents a crash but does not preserve the core
“walk away and get pinged” promise.

#### Decision

Choose and document one architecture:

1. **Service-owned connection:** the service isolate owns durable credentials,
   socket, reconnect, pending-ask reconciliation, and notification generation;
   UI communicates through an isolate/service bridge.
2. **Main-process best effort:** explicitly state that notifications require the
   existing process/socket, start the FGS only from a visible user action, and
   remove claims that the restart receiver restores communication.

For the current local/mesh-only product, service-owned connection is the only
option that meets the stated background contract without cloud push.

#### Acceptance

- On-device tests cover screen lock, Doze, network/VPN change, process kill
  while the service is running, service restart, and Android 12+ background
  start denial.
- The foreground notification is derived from actual authenticated socket
  state, never a requested state.
- After process/service recreation, a pending permission produces one
  actionable notification and no duplicate response.
- Swiping away remains intentionally terminal because `stopWithTask=true`.

### H-6 — An incomplete session list can erase the phone's last copy

#### Evidence

- `Store.List` silently skips any session whose `meta.json` cannot be read or
  decoded (`internal/session/store.go:153-179`).
- `Manager.listFiltered` suppresses a top-level store error too, returning only
  live in-memory rows (`internal/session/manager.go:883-929`).
- `session.list_result` carries no `complete`/degraded marker
  (`internal/ws/server.go:960-966`).
- Android `listSessions` treats any non-error response without a list as a
  valid empty list (`apps/mobile/lib/data/ws/mcremote_client.dart:1623-1633`).
- `syncFromMeta` treats that snapshot as authoritative and immediately prunes
  in-memory transcript state, sequence bookkeeping, staged images, and the
  serialized cache (`transcripts_notifier.dart:828-855`;
  `data/chat/transcript_cache.dart:163-190`).

#### Failure sequence

1. One host metadata record becomes transiently unreadable/corrupt, or a
   version-drifted response omits `sessions`.
2. The daemon silently omits the row, or Android interprets the payload as an
   empty successful list.
3. Android deletes the corresponding transcript/cache. A malformed payload can
   prune all sessions.
4. The host's `LoadHistory` also maps corrupt and missing history to the same
   empty result (`store.go:236-256`), so neither side preserves a diagnosable
   recovery path.

#### Decision

- A snapshot is destructive-authoritative only when its response type and
  payload validate and the daemon marks it complete.
- Store enumeration must report/quarantine per-row corruption and return a
  degraded/partial result or fail the request; it must never silently present a
  partial set as complete.
- Prefer explicit delete tombstones/events for cache eviction. At minimum, do
  not prune on incomplete snapshots, and retain recoverable phone copies for a
  grace period.
- Distinguish unknown, missing, corrupt, and empty history in internal APIs and
  diagnostics even if v1 preserves an empty wire result for unknown sessions.

#### Acceptance

- Corrupt one `meta.json`; `session.list` is visibly degraded and Android keeps
  every existing transcript.
- Feed `session.list` a wrong response type or missing `sessions`; Android
  raises a protocol error and prunes nothing.
- An explicit successful `session.delete` still removes host and phone data.

## 6. Medium and low findings

### M-1 — History and outbound frame limits are not exact

The protocol promises a soft ~512 KiB history page. `HistoryPage` enforces it
with `approxEventBytes` (`internal/session/manager.go:772-844`), but the
estimate counts only text/error/tool/status/permission/agent-session strings.
It ignores title, questions/options, command catalogs, plan entries, approvals,
subagents, modes, config options, remote commands, attachments, JSON escaping,
and envelope/payload growth (`internal/event/event.go:398-510`).

The loop also always permits one event even when that event alone exceeds the
budget. `writeJSON` marshals and enqueues outbound frames without any maximum
(`internal/ws/server.go:1576-1603`). The daemon and Android client may therefore
allocate, queue, receive, and decode a very large JSON message. The relay's
outer 1 MiB limit is not an inner-message safeguard: the inner WSS frame is an
opaque TLS/TCP stream re-chunked into many outer binary frames.

**Decision:** budget the actual marshalled `session.history_result`, enforce an
exact outbound maximum for every response/event, and cap provider-derived
nested collections/strings at normalization boundaries. Define a typed
oversize error or paged representation for a single oversize event.

**Tests:** nested question/config/command payloads, escapable Unicode/control
text, and one event larger than the page/relay limit.

### M-2 — Version, message kind, and response type validation drift

- The spec requires `"v": 1` on every message, but the daemon accepts omitted
  or zero versions (`internal/ws/server.go:538-540`).
- Android defaults a missing version to 1 and never rejects another version
  (`apps/mobile/lib/data/protocol/models.dart:21-35`;
  `mcremote_client.dart:1539-1579`).
- The daemon discards the WebSocket message kind returned by `Read` and accepts
  binary JSON although the application contract says JSON text
  (`server.go:507-538`). Android casts incoming data to `String` and drops
  binary.
- Pending requests correlate only by ID. Most operation methods check only
  `type == error`; a wrong response type can become `[]`, an empty catalog, an
  empty diff, or false success (`mcremote_client.dart:1623-1800,1947-2067`).
  Even a ping counts any matching response as liveness.

RFC 6455 defines text and binary as distinct data types and provides
`Sec-WebSocket-Protocol` for selecting application protocols. The WebSocket
library handles the RFC framing, but mcremote's application-level contract is
not enforced consistently.

**Decision:** require v1 in v1 envelopes (or explicitly document the legacy
zero rule), reject unsupported inbound versions on Android, require text
messages on the daemon control plane, and give `request` an expected response
type/set. Consider `Sec-WebSocket-Protocol: mcremote.v1` for a negotiated future
version, with a compatibility rollout rather than a unilateral switch.

### M-3 — Durable history has an unbounded crash-loss interval

Every event resets a one-second debounce
(`internal/session/manager.go:1523-1540`). A continuously streaming provider can
therefore postpone persistence indefinitely until a one-second quiet period or
clean close. The timer is shared across all dirty sessions, so one noisy session
also postpones quieter sessions already waiting to flush. A process/host crash
during that interval loses the entire dirty tail, despite the feature being
described as durable.

When it does flush, it copies and rewrites the complete retained ring through a
single store mutex (`manager.go:1548-1588`;
`internal/session/store.go:207-233`). Bursty sessions separated by one-second
pauses repeatedly marshal, fsync, rename, and directory-fsync up to 800 events.

**Decision:** use a debounce plus maximum-latency deadline, or preferably an
append/segmented journal compacted to the 800-event snapshot. Define the
maximum acknowledged-history loss on crash.

### M-4 — Message-level provider operations cannot be wired on Android

The daemon protocol exposes `message_id`/`part_id` for fork, revert, and diff,
but `event.Event` carries neither identifier. Android explicitly omits
`session.revert`/`session.unrevert` because no received chat item can construct
the request (`apps/mobile/lib/data/ws/mcremote_client.dart:1820-1834`).
Fork/diff are wired only at whole-session scope; `/undo` and `/redo` may still
work as provider/daemon slash commands where advertised.

**Decision:** either label message-level operations server/other-client-only,
or carry optional stable provider message and part IDs through the event,
history, cache, and `ChatItem` models. Render controls only when the provider
advertises the capability and an ID exists.

### M-5 — Relay host tunnels outlive their cancellation owner

On a relay `dial`, mcremote launches
`openTunnel(context.WithoutCancel(ctx), ...)`
(`internal/relayhost/client.go:157-180`). Authentication is bounded by 30
seconds, but the established bridge uses the detached parent indefinitely
(`:180-239`). This intentionally survives a control-channel reconnect, but it
also severs daemon-shutdown cancellation and violates the repository network
standard.

**Decision:** give `relayhost.Client` a root daemon-lifecycle context and a
separate registration-attempt context. Tunnels may survive one control
connection, but not client/daemon shutdown. Track and drain active tunnels.

### M-6 — Relay outer-stream errors are swallowed

After `join_ok`, the outer data subscription uses `onError: (_) {}`
(`apps/mobile/lib/data/ws/relay_transport.dart:126-132`). A post-join outer
WebSocket error therefore neither closes the loopback peer nor surfaces a typed
failure. Combined with H-3's silent frame drop and the empty error handler on
peer write failures (`:175-177`), the phone can sit on a half-dead tunnel while
the UI still believes the daemon path is up.

**Decision:** any outer stream error, unexpected post-join text (H-3), or peer
write failure must close both legs with a typed `relay_*` error and force the
control-plane reconnect path. Never ignore the error callback on an opaque
byte pipe.

**Tests:** inject an outer `onError` after `join_ok` and assert both the
loopback socket and the outer channel close, and that the control client
observes a reconnectable failure rather than an indefinite hang.

### M-7 — Multi-page history can return a partial ring as complete

`sessionHistory` auto-pages while `truncated` is true
(`apps/mobile/lib/data/ws/mcremote_client.dart:1665-1703`). On each page:

```dart
if (list is! List || list.isEmpty) return out;
```

If page *N* was truncated and page *N+1* returns a missing/empty `events`
array (malformed payload, wrong response type that is not `error`, or a
transient empty page), the client returns the partial `out` without throwing.
`resyncHistory` then treats that prefix as the authoritative ring and can
rebuild a populated transcript **downward**, discarding newer local content
that was already correct.

This is the history counterpart of H-6: incomplete snapshots must not be
destructive-authoritative.

**Decision:**

- require `type == session.history_result` on every page (M-2);
- if the previous page was truncated, a missing/empty/malformed next page is a
  hard error for the whole fetch — return nothing usable for rebuild;
- `resyncHistory` must refuse to rebuild when the fetch is incomplete or when
  the fetched max seq is below the local `_lastSeq` without a confirmed ring
  reset.

**Tests:** truncated page 1 + empty page 2 must throw / fail the whole fetch;
a populated local transcript must remain unchanged.

### M-8 — Suspected gaps never clear when history has no `seq`

`resyncHistory` computes `maxSeq` over fetched events and returns early when
`maxSeq == 0` (`transcripts_notifier.dart:789-796`), even if
`_seqGapSuspected[sessionId]` is true. Unstamped or pre-seq history therefore
cannot heal a gap flag set on reconnect (H-1). The gap remains forever; later
opens skip replay because the transcript is non-empty and no path clears the
flag without a successful rebuild.

**Decision:** when a gap is suspected and history is unstamped, either:

1. rebuild from the full ring by arrival order (document that seq-less history
   is last-resort and may duplicate until fold rules apply), or
2. keep the local transcript, clear the gap only after an explicit "history
   unavailable / unstamped" degraded state is recorded, and surface it in
   diagnostics.

Prefer (2) for safety: do not destroy a populated transcript for unstamped
data.

**Tests:** seed a non-empty transcript + gap flag, feed unstamped history,
assert the transcript is unchanged and the gap either clears with a degraded
marker or remains actionable without silent permanent failure.

### L-1 — Android history copy is stale

`ChatScreen` still says host history exists only while a session is live and
that close/restart clears it (`chat_screen.dart:186-191,263-270,329-338`).
Protocol v1 and the manager now persist the 800-event tail across close/restart.
The stale note can tell a user data was expected to disappear when the actual
cause is empty/corrupt/unknown history.

**Decision:** update the text and comments to describe bounded durable history,
and surface a distinct degraded/unavailable reason when the daemon can provide
one.

### L-2 — `sessionHistory` dartdoc claims empty-on-error

The method comment says it "Returns an empty list on error or when the session
has no history" (`mcremote_client.dart:1655-1660`), but mid-page and terminal
errors throw `McException`. Callers that catch and treat failure as empty
(chat open) are fine; callers that trust the comment for silent degradation
are not. Align the dartdoc with the throw contract; keep the fail-closed
behavior.

### M-9 — Streaming isolate renderer is not full GFM

Steady-state streaming (after the first isolate parse) builds widgets from
`ParsedResult` via `_buildFromParsed` (`chat_bubble.dart:725-839`), not from
`MarkdownBody`. The AST walker (`markdown_parser.dart`) only emits
paragraph/heading/code/blockquote/list/hr blocks and bold/italic/code/link
spans. Live probes at `fa21393`:

- GFM table `| a | b | …` → one paragraph whose text is fused (`ab12`)
- Mid-stream table `| a | b |\n|---|\n| 1` → paragraph `ab1`
- Task list `- [ ] todo` → bullet text `todo` (checkbox state dropped)
- Ordered list `1. one / 2. two / 3. three` → three items all prefixed `1. `
  in the RichText builder (`chat_bubble.dart:759-762`)
- Image `![alt](url)` → empty paragraph
- Closed `~~str~~` → text without strike decoration (see L-4)

Finalized replies use full `MarkdownBody` (`chat_bubble.dart:678-686`), which
supports tables, richer GFM, and selectable text. Users therefore watch
agent markdown look raw, fused, or mis-numbered mid-turn, then snap into a
different layout at `turn_complete`. Nested lists were fixed (0045 P4 /
`markdown_parser_test.dart`); the broader feature cliff remains.

**Decision:** one renderer for stream and finalize. Prefer isolate parse →
stable block model that includes tables, ordered indices, strike, and images,
or always use `MarkdownBody` on buffered text with the existing frame throttle.
Do not ship a subset renderer that only the stream path uses.

### M-10 — Stream closer covers only three constructs

`bufferStreamingMarkdown` (`streaming_markdown.dart:18-74`) auto-closes only
fenced code, inline code, and `**bold**`. Probes:

- `hello **bol` → buffered to closed bold (good)
- `hello *ita` → unchanged, asterisks remain
- `hello ~~str` → unchanged
- `see [text](http://x` → unchanged

So during the first-frame `MarkdownBody` path and any path that relies on
buffering alone, incomplete italic, strike, and links render as literal
syntax — the main “raw markdown in the chat window” symptom for short replies.
MADR 0027 already named this; it is still true at this baseline. Expanding the
heuristic closer is fragile; pairing M-9’s single full parser is the durable
fix.

### M-11 — Long-stream path shows raw markdown on purpose

When `streaming && text.length > kMaxStreamingMarkdownChars` (4000)
(`chat_models.dart:51`, `chat_bubble.dart:645-658`), the UI switches to
monospace `SelectableText` with **no** markdown parse and **no** stream
closer. Markers (`**`, fences, headings, tables) appear literally until the
turn finalizes and flips back to `MarkdownBody`. Agent replies routinely
exceed 4k. Tests lock this behavior in as performance policy
(`streaming_markdown_test.dart` long plain path), so it is intentional — but
it is also why long session-chat turns look “broken” until complete.

**Decision:** raise or remove the cliff only with a non-`MarkdownBody`-per-frame
path (incremental/block cache). Until then, document the cliff in-product or
keep a lightweight last-N-lines styled preview so the live end is not raw
source.

### M-12 — Multi-engine swap causes layout snap / jank

Per assistant turn the bubble can pass through:

1. first stream frame(s): `MarkdownBody` + `bufferStreamingMarkdown`
2. steady stream (&lt;4k): isolate `RichText` blocks (M-9 subset)
3. steady stream (&gt;4k): plain monospace (M-11)
4. finalize: full `MarkdownBody` again

Widget identity, font metrics, table layout, and list numbering all change.
Combined with reverse-list extent growth (`ListView reverse: true`), each swap
reads as jitter or “markdown still processing.” Frame-aligned throttle and
widget cache (0018 / Proposal F,C) reduce parse cost but do not remove the
engine swap. Scroll-time update suppression (L-3) freezes mid-engine state.

**Decision:** same as M-9 — one engine. Keep frame throttle and scroll pause
as performance tools, not as partial-render mode switches.

### L-3 — Scroll suppress without end-of-scroll reschedule

`_AssistantMarkdown.didUpdateWidget` returns early while
`ChatScrollActivity.isScrolling` is true (`chat_bubble.dart:861-862`) and does
not queue a follow-up when scroll ends. Fresh chunks after scroll end update
normally; if the model pauses while the user is scrolling, the bubble can stay
on a stale stream snapshot until the next chunk or finalize. Low severity;
fix is to listen for scroll-end and call `_updateStreamingRender` if dirty.

### L-4 — Closed strikethrough loses decoration on isolate path

`parseMarkdownOffMain('hello ~~str~~ world')` yields a single paragraph of
plain text `hello str world` — no italic/bold/code flags and no strike style
in `ParsedSpan`. The walker has no `del`/`s` case (`markdown_parser.dart:186-209`
default only recurses). Streaming isolate path therefore never shows
strikethrough even when the markers are closed; finalize `MarkdownBody` does.

## 7. Compatibility and standards assessment

| Area | Assessment | Notes |
|---|---|---|
| RFC 6455 framing | **Mostly conformant** | `coder/websocket` / `web_socket_channel` handle masking, fragmentation, ping/close, and text writes. Application text-vs-binary enforcement is asymmetric (M-2). |
| WebSocket subprotocol negotiation | **Gap** | No `Sec-WebSocket-Protocol` selection; application version exists only inside JSON. Not required for RFC conformance, but useful for safe future protocol negotiation. |
| Protocol v1 envelopes | **Partial** | Type/ID vocabulary is documented and error codes are closed/tested. Mandatory version and response type are not enforced. |
| Inbound 1 MiB limit | **Good** | Android preflights exact serialized UTF-8 bytes; daemon sets a 1 MiB read limit. |
| Outbound sizing | **Gap** | No exact global cap; the history estimate is incomplete, and relay re-chunking does not bound the inner WSS message (M-1). |
| TLS/server identity | **Good** | Self-signed pinning and Let's Encrypt chain-or-fallback-pin behavior are deliberately separated; plaintext is explicit opt-out and Android rejects it by policy. |
| Client identity/auth | **Good with H-4 caveat** | Client key/token enrolment, revocation, and owner-filtered events are present. Durable owner transitions fail open on storage error. |
| Browser Origin | **Good** | Native clients may omit Origin; browser origins use the explicit allowlist. |
| Event ordering/back-pressure | **Good** | One writer per connection, bounded queue, write deadline, slow-client disconnect, and per-session sequence numbers. Recovery ownership is the missing layer (H-1). |
| Android FGS declaration | **Conformant** | `remoteMessaging` type and permissions match Android guidance. Runtime architecture does not deliver restart semantics (H-5). |
| Android FGS standards text | **Drift** | `standards/mobile/android.md` states the FGS keeps the user-visible remote-messaging connection alive. Implementation only keeps the *process* (and main-isolate socket) alive; service isolate owns no connection (H-5). |
| Android notification permission | **Mostly conformant** | Permission is declared and requested; concurrent notification/service startup can invert the intended order. Denial is surfaced in Settings. |
| Durable session/history | **Partial** | Atomic file replacement, fsync, ownership filters, ring bounds, and restart seeding are good. Security writes, corruption reporting, and crash-loss bounds need work. |
| Go network cancellation | **Partial** | Request/server lifecycle contexts are used in many paths. `dispatchAsync` and `relayhost.openTunnel` still use `context.WithoutCancel` without operation deadlines (H-2, M-5), which the Go networking standard forbids for network goroutines. |
| Go session ownership | **Partial** | Owner filters on list/broadcast/ops are present and tested. Durable owner transitions fail open on store errors (H-4), which weakens the security boundary the session standard requires. |
| Pending-ask reconciliation | **Good** | Reconnect re-fetches `session.pending_asks`, cancels stale notifications, and does not treat fetch failure as empty (correct opposite of H-6). |
| Question notifications | **Intentional open-only** | Multi-item questions open the chat; only permissions expose Allow/Deny from the shade. Not a defect unless product wants shade answers for questions. |
| GFM streaming fidelity | **Partial** | Final `MarkdownBody` is GFM-capable via `flutter_markdown_plus` + `markdown` GFM extension set. Stream isolate path and stream closer are strict subsets (M-9, M-10). Long-stream path is intentionally non-markdown (M-11). |
| Prior markdown fixes | **Partial** | 0045 P2 (stream parse without buffer) is **fixed** (`_updateStreamingRender` buffers before parse). 0045 P4 (nested list fuse) is **fixed** (`_walkList` + test). 0027 raw-marker and long-stream cliffs remain. |

Primary external references:

- [RFC 6455 — The WebSocket Protocol](https://www.rfc-editor.org/rfc/rfc6455)
- [Android foreground service types — remote messaging](https://developer.android.com/develop/background-work/services/fgs/service-types#remote-messaging)
- [Android restrictions on starting a foreground service from the background](https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start)
- [Android notification runtime permission](https://developer.android.com/develop/ui/compose/notifications/notification-permission)

## 8. Controls verified as present

These were examined and are not findings:

- exact Android outbound request-size preflight and daemon inbound 1 MiB read
  limit;
- authenticated owner-filtered list, operations, pending asks, and event fanout;
- bounded client count, unauthenticated eviction, authentication deadline,
  per-client async cap, bounded output queue, and write deadline;
- one WebSocket writer per daemon connection and ordered shared event buffer;
- sequence-stamped bounded history, durable restart seeding, and paged Android
  fetch;
- client connection epochs, bounded close handshake, reconnect backoff/parking,
  ping probes, and typed certificate failures;
- self-signed fingerprint verification, Let's Encrypt fallback-pin semantics,
  secure Android credential storage, backup disabled, cleartext disabled, and
  non-exported foreground service;
- notification pending-ask reconciliation and main-isolate notification actions
  that remain visible until a response succeeds.

## 9. Test gaps exposed by the audit

Add cross-stack tests, not only local unit tests:

1. reconnect while a populated session is inactive, then open it later;
2. response-loss and retry for every mutating operation, especially create and
   prompt;
3. async handler deadline/disconnect/shutdown and slot release;
4. relay pre-peer byte equality, byte-budget overflow, unexpected text,
   outer-stream errors, and peer-attach/close races;
5. create/claim with injected durable-store failure;
6. corrupt/partial `session.list` without client cache eviction;
7. exact marshalled history/event size with nested collections;
8. missing/zero/future envelope versions, binary control messages, and wrong
   response types;
9. long continuous stream plus crash to establish the durability-loss bound;
10. on-device FGS/process-kill/Doze/background-start-denial matrix;
11. truncated history page 1 + empty/malformed page 2 must not rebuild;
12. gap flag + unstamped history must not permanently strand a session;
13. stream vs finalize markdown parity for tables, strike, ordered lists, images;
14. open `*`, `~~`, `[link](` must not flash raw under the stream closer;
15. crossing 4000 chars must not flash full source-view unless product accepts it.

## 10. Recommended remediation order

| Phase | Findings | Reason |
|---|---|---|
| 0 | Regression tests for H-1, H-3, H-4, H-6, M-7 | Protects data/integrity behavior before refactors |
| 1 | H-3, M-6, H-4, H-6, M-7 | Smallest high-impact fail-closed fixes (relay + ownership + snapshots) |
| 2 | H-1, M-8 | Central cross-session synchronizer, reconnect ownership, gap clear rules |
| 3 | H-2, M-5 | Request ledger, deadlines, cancellation, lifecycle-owned tunnels |
| 4 | M-1, M-2 | Exact framing and strict compatibility validation |
| 5 | M-3 | Journal/max-latency durability design and performance measurement |
| 6 | H-5 | Android service-owned connection, requiring on-device acceptance |
| 7 | M-9, M-10, M-11, M-12 | Single markdown engine for session chat; kill raw flash and engine swaps |
| 8 | M-4, L-1…L-4 | Capability/UX completion and documentation |

Each phase must leave exactly one owner for connection/session state. Avoid
adding retries until the operation being retried is idempotent.

## 11. Session-management hardening opportunities (beyond defects)

These are not defects in the current contract, but they are the highest-leverage
stability and performance improvements once H/M items land:

| Opportunity | Why |
|---|---|
| Connection-scoped **session synchronizer** (H-1 owner) | Single place for list + history + pending-asks + gap flags; screens become observers |
| **Idempotency ledger** keyed by `(device_id, request_id)` (H-2) | Safe client retries after timeout without duplicate create/prompt |
| **Append-only history journal** with periodic compact to 800-event snapshot (M-3) | Bounds crash loss and removes full-ring rewrite cost under chatty agents |
| **Per-session history flush deadline** independent of other sessions' noise | Noisy session must not postpone quiet sessions' durability |
| Exact **marshalled outbound frame cap** with typed oversize / paging (M-1) | Aligns daemon, Android, and relay outer limits |
| **Sec-WebSocket-Protocol: mcremote.v1** negotiation (M-2) | Safe future protocol bumps without JSON-only version ambiguity |
| Service-isolate **owned socket** with credential store (H-5 option 1) | True background approvals without depending on a live UI isolate |
| Explicit **delete tombstones** for session cache eviction (H-6) | Destructive prune only on proven host delete, never on partial list |
| Bounded concurrency when resyncing N sessions after reconnect | Protects phone radio/CPU; one slow session must not block the rest |
| Surface **degraded persistence** in `/healthz` or hello diagnostics | Operators see disk-full ownership/history failures before multi-device incidents |
| **Single markdown pipeline** for stream + finalize (M-9…M-12) | Ends raw flash, table fusion, and layout snap in session chat windows |
| Incremental/block-cached parse instead of full re-parse of the growing string | Performance without the 4k mono cliff |

## 12. Verification performed for this assessment

| Check | Result |
|---|---|
| Repository state | `master` at `fa21393`; working tree clean except this untracked MADR |
| Static re-verification of H-1…H-6, M-1…M-5, L-1 | **Confirmed present** at cited call sites |
| New findings M-6, M-7, M-8, L-2 | **Confirmed** in `relay_transport.dart`, `mcremote_client.dart`, `transcripts_notifier.dart` |
| Markdown findings M-9…M-12, L-3, L-4 | **Confirmed** by code inspection + live `parseMarkdownOffMain` / `bufferStreamingMarkdown` probes |
| `flutter test` markdown suite | **Pass** — `markdown_parser_test`, `streaming_markdown_test`, `chat_render_test` (59 tests) |
| `dart format --output=none --set-exit-if-changed .` | **Pass** (prior pass; not re-run this edit) |
| `dart --suppress-analytics analyze` | **Pass** (prior pass) |
| `go test -race ./internal/session` | **Pass** (prior pass) |
| `go test -race ./internal/ws ./internal/relayhost` | May require non-sandbox env (loopback bind) |
| Android device/Doze/process-kill tests | **Not run** — no device attached |

The blocked checks are not recorded as code failures. They must be run in the
normal development/CI environment before accepting remediation.

## 13. Independent re-verification summary (2026-07-30)

A second deep-dive pass walked request, response, event, disconnect,
persistence, and recovery paths on both stacks without treating the first draft
as authoritative. Outcome:

- **All six High findings remain valid** with the same root causes and line
  evidence (minor line drift only; behavior unchanged at `fa21393`).
- **M-1…M-5 and L-1 remain valid.**
- **Added M-6, M-7, M-8, L-2** from gaps the first draft did not name: outer
  error swallowing, partial multi-page history treated as complete, unstamped
  history leaving permanent gap flags, and dartdoc/throw mismatch.
- **No P0 / critical** security RCE or unauthenticated control-plane bypass was
  found. TLS pinning, client-key enrolment, origin allowlist, inbound 1 MiB
  limit, owner-filtered fanout, and bounded output queues remain solid.
- Highest product risk remains **cross-boundary**: route-local resync, fail-open
  ownership persistence, destructive incomplete snapshots, and the FGS promise
  vs architecture mismatch.

## 14. Session-chat markdown deep-dive (why it looks raw)

### Pipeline

```text
provider tokens
  → daemon chunkbuf + session event
  → WS event → transcripts_notifier (32ms batch)
  → transcript_reducer (append/fold assistant text)
  → _TranscriptPane / _ChatBubble (ValueKey seq)
  → _AssistantMarkdown
        ├ stream <4k, no cache yet: MarkdownBody(bufferStreamingMarkdown)
        ├ stream <4k, isolate ready: RichText from ParsedResult (subset GFM)
        ├ stream >4k: SelectableText monospace (no parse)
        └ finalize: MarkdownBody(full text)
```

### Why users see raw / “still processing” markdown

1. **Incomplete closer (M-10).** Only `**`, `` ` ``, and ``` are sealed while
   streaming. Open `*italic`, `~~strike`, and `[link](url` show as source.
2. **Subset isolate renderer (M-9, L-4).** After the first background parse,
   tables fuse to a single run of cell text, ordered lists all show `1.`,
   images vanish, task checkboxes vanish, strikethrough is plain text.
3. **Intentional long-stream plain path (M-11).** Over 4000 characters the
   bubble is monospaced source until the turn ends — long agent answers
   (the common case) look unprocessed for most of the turn.
4. **Engine swaps (M-12).** Each transition changes layout metrics; reverse
   list scroll correction makes that read as jank.
5. **Scroll freeze (L-3).** Mid-stream paint can stall while the user scrolls
   history, then jump when the next chunk or finalize arrives.

### What is already fixed (do not re-litigate)

- 0045 **P2**: stream path now parses `bufferStreamingMarkdown(text)` in
  `_updateStreamingRender` (`chat_bubble.dart:880`).
- 0045 **P4**: nested lists keep depth/order (`markdown_parser.dart` `_walkList`,
  `markdown_parser_test.dart`).
- 0018: frame-aligned throttle, widget cache, isolate offload, reverse list,
  follow-vs-read, show-more clamp for huge *finalized* replies.

### Probe evidence (executed 2026-07-30)

| Input | bufferStreamingMarkdown | parseMarkdownOffMain / stream builder |
|---|---|---|
| `hello **bol` | closes bold | bold `bol` |
| `hello *ita` | unchanged | literal `*ita` |
| `hello ~~str` | unchanged | literal `~~str` |
| `~~str~~` closed | n/a | plain `str` (no strike) |
| GFM table | unchanged | paragraph `ab12` |
| `1. one` … `3. three` | unchanged | three blocks, UI prefix always `1.` |
| open fence | appends `\n```` | code block streams |
| `![alt](url)` | n/a | empty paragraph |

### Relationship to MADR 0027

0027 correctly diagnosed raw-marker leaks and the 4k mono cliff. Parts of its
performance plan shipped (frame throttle, isolate parse). The **fidelity**
recommendations (full closer or single full renderer; eliminate mono cliff
without a subset engine) are still open and are captured here as M-9…M-12 so
the cross-stack audit has one backlog.
