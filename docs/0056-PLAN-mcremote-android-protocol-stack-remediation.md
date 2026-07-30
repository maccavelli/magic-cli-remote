# MADR 0056 — Implementation plan: mcremote ↔ Android protocol-stack remediation

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0056](./0056-MADR-mcremote-android-protocol-stack-audit.md).
Read that first: it carries the verified findings, severity rationale, and
acceptance criteria. This document is the **build order** — phase-sequenced,
file-specific, and grounded in the tree at baseline `fa21393` (`master`).

- **Status:** **Phases 0–8 complete** (2026-07-30); phase 9 (H-5b) deferred;
  M-4 phone message IDs deferred (document server/other-client only);
  Phase 5b journal optional
- **Date:** 2026-07-30
- **Baseline:** `fa21393`
- **Scope:** Go daemon (`internal/ws`, `internal/session`, `internal/relayhost`,
  `internal/protocol`, `internal/event` as needed) and Android/Flutter client
  (`apps/mobile/lib/data/ws`, `state`, `features/chat`, `features/sessions`,
  `data/chat`, `data/notifications`, `app_lifecycle`) plus matching tests and
  protocol docs
- **Standards:** [Go networking](standards/go/network.md),
  [Go sessions](standards/go/session.md),
  [mobile networking](standards/mobile/networking.md),
  [Android](standards/mobile/android.md)
- **Related plans:** [0055 server remediation](0055-PLAN-mcremote-server-remediation.md)
  (done), [0046 mobile debug](0046-PLAN-mobile-debug-pass.md) (done),
  [0027 streaming rendering](0027-MADR-opencode-streaming-rendering.md) (partial)

---

## 0. MADR assessment (grounded)

### 0.1 Overall judgment

MADR 0056 is **accurate and actionable**. Independent re-check against
`fa21393` confirms every High and Medium finding’s root cause at the cited
symbols. Severity ranking is correct: no P0/RCE, highest risk is
**cross-boundary** state (resync ownership, ownership durability, destructive
list snapshots, opaque relay byte loss, FGS promise mismatch).

Markdown findings M-9…M-12 are **product-visible and user-reported** (raw /
“still processing” text in session chat). They are Medium, not High: they do
not lose durable host data, but they dominate day-to-day perceived quality.

### 0.2 Finding validity matrix

| ID | Still present at `fa21393`? | Primary evidence |
|---|---|---|
| H-1 | **Yes** | `TranscriptsNotifier` only sets `_seqGapSuspected` on reconnect; `ChatScreen._resyncAfterReconnect` is route-local; `_maybeReplayHistory` returns if `items.isNotEmpty` without consulting the gap flag |
| H-2 | **Yes** | `dispatchAsync` uses `context.WithoutCancel(ctx)` with no operation deadline; Android `request` drops completer on 30s timeout and mints a new UUID every call |
| H-3 | **Yes** | `RelayTransport._onOuterData` `removeFirst()` at `kRelayOuterBufferMax` (64 frames) |
| H-4 | **Yes** | `Authorize` disk claim logs Save failure and returns nil; `Create` returns after `persistNow` regardless of `writePersist` error |
| H-5 | **Yes** | `mcRemoteForegroundCallback` / `_KeepAliveTaskHandler` empty; credentials and `McremoteClient` live on main isolate |
| H-6 | **Yes** | `Store.List` skips bad rows; `listFiltered` swallows store.List error; Android `listSessions` treats non-list as `[]`; `syncFromMeta` prunes |
| M-1 | **Yes** | `approxEventBytes` counts a fixed 128 + a few strings; `writeJSON` has no max |
| M-2 | **Yes** | Daemon accepts `env.V == 0`; Android defaults missing `v` to 1; no expected-response-type helper |
| M-3 | **Yes** | `historyPersistDebounce = 1s` reset on every event; shared timer; full-ring `SaveHistory` |
| M-4 | **Yes** | `event.Event` has no message/part id; client comments document deliberate omission of revert |
| M-5 | **Yes** | `openTunnel(context.WithoutCancel(ctx), …)` |
| M-6 | **Yes** | outer dataSub `onError: (_) {}` |
| M-7 | **Yes** | `sessionHistory` `if (list is! List \|\| list.isEmpty) return out` mid-paging |
| M-8 | **Yes** | `resyncHistory` returns when `maxSeq == 0` even if gap suspected |
| M-9 | **Yes** | live probe: table → paragraph `ab12`; ordered UI always `1.` |
| M-10 | **Yes** | closer only `**` / `` ` `` / fence |
| M-11 | **Yes** | `kMaxStreamingMarkdownChars = 4000` → mono plain path |
| M-12 | **Yes** | MarkdownBody ↔ isolate RichText ↔ mono ↔ MarkdownBody |
| L-1 | **Yes** | chat history copy still “live-only” |
| L-2 | **Yes** | dartdoc vs throw |
| L-3 | **Yes** | scroll suppress without reschedule |
| L-4 | **Yes** | closed `~~` loses decoration on isolate path |

### 0.3 Already fixed (do not re-open)

| Prior ID | Status | Evidence |
|---|---|---|
| 0045 P2 stream parse without buffer | **Fixed** | `_updateStreamingRender` calls `parseMarkdownOffMain(bufferStreamingMarkdown(text))` |
| 0045 P4 nested list fuse | **Fixed** | `_walkList` + `markdown_parser_test.dart` |
| 0018 reverse list / cache / frame throttle | **Shipped** | `_AssistantMarkdown` post-frame throttle, `ValueKey(seq)` |

### 0.4 Gaps in the MADR (plan fills them)

1. **H-5 is two products.** Full service-isolate socket ownership is multi-week.
   This plan splits **H-5a** (honest promise + safe FGS start) from **H-5b**
   (service-owned connection).
2. **H-2 is three layers.** Deadlines, then server idempotency ledger, then
   client retry with stable request ids — do not enable client retries before
   the ledger.
3. **M-3 full journal** is a design sub-project. Phase 5 ships **max-latency
   flush** first; journal is optional follow-on with its own spike.
4. **M-4** needs an explicit product decision before protocol extension; plan
   keeps it as a gated late phase.

### 0.5 What remains solid (do not regress)

- TLS pin modes + client-key enrolment
- Origin allowlist for browsers
- Inbound 1 MiB read limit + Android outbound preflight
- Per-client `asyncInFlight` cap (8), bounded `out` queue, write deadline,
  slow-client disconnect
- Owner-filtered list / broadcast / mutating ops
- Pending-ask reconcile that **does not** treat fetch failure as empty
- Atomic session meta/history writes under store mutex

---

## 1. Locked decisions for this plan

| # | Question | **Chosen** | Implication |
|---|---|---|---|
| **D1** | Who owns reconnect resync? | **Connection-scoped `SessionSynchronizer`** on the phone (Riverpod, keepAlive), not ChatScreen | Screens observe; synchronizer list+history+gaps |
| **D2** | Destructive list prune | **Only when `session.list_result` validates and `complete: true`** | Partial/degraded never calls `retainOnly` |
| **D3** | Ownership persist failure | **Fail closed** on create and first claim | Typed error; roll back in-memory claim; no success without durable owner |
| **D4** | Relay overflow | **Fail closed** with `relay_buffer_overflow`; never drop bytes | Bound frames **and** bytes |
| **D5** | Async WS work | **Derive from daemon lifecycle + per-op deadline**; no naked `WithoutCancel` | Disconnect grace optional only for create Start, still bounded |
| **D6** | Mutation retries | **Server idempotency ledger** `(device_id, request_id)` before client auto-retry | Duplicate returns stored result |
| **D7** | FGS architecture | **H-5a first** (honest main-isolate keep-alive + foreground-only start); **H-5b** service-owned socket is a separate milestone | Product copy must match reality until H-5b |
| **D8** | Markdown engines | **Single engine for stream and finalize**: frame-throttled `MarkdownBody` on buffered text; retire stream-only isolate subset as primary path | Isolate optional later for offline bulk only if profiling requires |
| **D9** | History durability interim | **Debounce + max latency** (e.g. 5s) before full journal | Bounds crash-loss window |
| **D10** | Protocol growth | Prefer **additive optional fields** (`complete`, `degraded`, `request_id` echo already present) | Document in `protocol-v1.md` same PR as wire change |
| **D11** | Message IDs (M-4) | **Gate:** implement only after product confirms message-level fork/revert on phone | Default: document server/other-client-only |

---

## 2. Ground rules

1. **One phase → one commit** as the review shape unless the phase table says
   otherwise. Do not push unless the user asks.
2. **Go pre-add gate** for any touched `.go`:
   `make pre-add-check` / `scripts/go-precheck.sh` (`gofmt`, `golint`,
   `govulncheck`).
3. **Dart gate** per mobile phase: `dart format` on touched files;
   `cd apps/mobile && dart analyze` and the phase’s `flutter test` list.
4. **Commit without `-m`** (prepare-commit-msg hook).
5. **Line numbers drift.** Prefer symbol names over line numbers in patches.
6. **Red then green:** for each High finding, land a regression test that
   fails on current main (Phase 0), then turn it green in the fix phase.
7. **No client auto-retry of mutations** until Phase 3 ledger is green.
8. **Exactly one owner** of connection/session reconciliation state after
   Phase 2 — ChatScreen must not re-own list/history resync.
9. **Protocol doc + smoke:** any wire field change updates `docs/protocol-v1.md`
   and, where applicable, `scripts/smoke-protocol` / existing protocol tests.

### Verification commands (repeatable)

```bash
# Go (module root)
make pre-add-check
go test -race ./internal/ws/... ./internal/session/... ./internal/relayhost/... ./internal/protocol/...

# Mobile
cd apps/mobile
dart format --output=none --set-exit-if-changed .
dart analyze
flutter test test/relay_transport_test.dart test/history_replay_test.dart \
  test/session_history_paging_test.dart test/streaming_markdown_test.dart \
  test/markdown_parser_test.dart test/chat_render_test.dart
# Phase-specific extras listed per phase
```

---

## 3. File map (canonical)

### Go

| Area | Path |
|---|---|
| WS server | `internal/ws/server.go` |
| WS tests | `internal/ws/server_test.go`, `server_session_handlers_test.go`, new `*_test.go` as named |
| Session manager | `internal/session/manager.go` |
| Session store | `internal/session/store.go` |
| Session tests | `internal/session/manager_*.go`, `store_test.go` |
| Relay host | `internal/relayhost/client.go` |
| Protocol types | `internal/protocol/messages.go`, `errors.go` |
| Events | `internal/event/event.go` |
| Protocol doc | `docs/protocol-v1.md` |

### Android / Flutter

| Area | Path |
|---|---|
| WS client | `apps/mobile/lib/data/ws/mcremote_client.dart` |
| Relay | `apps/mobile/lib/data/ws/relay_transport.dart` |
| Lifecycle | `apps/mobile/lib/app_lifecycle.dart` |
| Transcripts | `apps/mobile/lib/state/transcripts_notifier.dart` |
| Providers | `apps/mobile/lib/state/app_providers.dart` |
| Chat UI | `apps/mobile/lib/features/chat/chat_screen.dart` |
| Sessions UI | `apps/mobile/lib/features/sessions/sessions_screen.dart` |
| Markdown | `apps/mobile/lib/data/chat/{streaming_markdown,markdown_parser,chat_models}.dart` |
| Bubble | `apps/mobile/lib/features/chat/chat_bubble.dart` |
| FGS | `apps/mobile/lib/data/notifications/foreground_service.dart` |
| Notif coord | `apps/mobile/lib/data/notifications/notification_coordinator.dart` |
| Protocol models | `apps/mobile/lib/data/protocol/models.dart` |
| Manifest | `apps/mobile/android/app/src/main/AndroidManifest.xml` |

### New files expected

| File | Phase |
|---|---|
| `apps/mobile/lib/state/session_synchronizer.dart` (or under `data/session/`) | 2 |
| `apps/mobile/test/session_synchronizer_test.dart` | 0 / 2 |
| `apps/mobile/test/relay_byte_pipe_test.dart` | 0 / 1 |
| `internal/session/owner_persist_test.go` (or extend manager_owner_test) | 0 / 1 |
| `internal/ws/async_deadline_test.go` | 0 / 3 |
| `internal/ws/idempotency.go` + tests | 3 |
| `internal/session/list_result.go` helpers if needed | 1 |

---

## 4. Phase overview

| Phase | Theme | Findings | Est. | Depends |
|---|---|---|---|---|
| **0** | Regression guardrails (tests red on main) | H-1, H-3, H-4, H-6, M-7 | S–M | none |
| **1** | Fail-closed integrity | H-3, M-6, H-4, H-6, M-7, M-8 (gap clear rules partial) | M | 0 |
| **2** | Connection-scoped synchronizer | H-1, M-8 | M | 1 (H-6/M-7 safe list) |
| **3** | Request lifecycle + relay host | H-2, M-5 | L | 0–1 |
| **4** | Framing + protocol validation | M-1, M-2 | M | 1 optional |
| **5** | History durability bounds | M-3 | M | none (parallel after 1) |
| **6** | FGS honesty (H-5a) | H-5 partial | M | none |
| **7** | Single markdown engine | M-9…M-12, L-3, L-4 | M | none (parallel) |
| **8** | Docs / low polish / gated M-4 | L-1, L-2, M-4 (gated), standards drift | S | 2, 6, 7 preferred |
| **9** | FGS service-owned socket (H-5b) | H-5 full | L | 6, 2, 3 |

**Recommended serial spine:** 0 → 1 → 2 → 3.  
**Parallel after 1:** 4, 5, 6, 7.  
**Late:** 8, 9.

```text
0 ──► 1 ──► 2 ──► 3 ──► 9
       │      │
       ├──► 4 │
       ├──► 5 │
       ├──► 6 ─┘
       └──► 7 ──► 8
```

---

## Phase 0 — Regression guardrails

**Goal.** Commit tests that **fail on current main** for each High data-integrity
defect and for M-7. No production fixes in this phase except test helpers.

### 0.1 H-1 inactive session gap never healed

| | |
|---|---|
| **File** | `apps/mobile/test/session_synchronizer_test.dart` (new) and/or extend `history_replay_test.dart` |
| **Drive** | Fake `McremoteClient`: session A populated with items + `_lastSeq`; mark reconnect (simulate connection state transition); do **not** mount ChatScreen; open A later via notifier-only API or a thin harness that only calls today’s open path (`hydrate` / empty-only replay) |
| **Assert (today, fail)** | After reconnect without mounted chat, opening a non-empty transcript does **not** fetch history / clear gap; `debugGapSuspected(A)` stays true and missing events never applied |
| **Assert (after Phase 2)** | History fetched for A; gap clears; all retained events present |

### 0.2 H-3 relay frame drop corrupts pipe

| | |
|---|---|
| **File** | `apps/mobile/test/relay_byte_pipe_test.dart` (new); extend `relay_transport_test.dart` if preferred |
| **Drive** | Inject binary frames into `_onOuterData` path before peer attach; exceed `kRelayOuterBufferMax`; then attach peer and collect bytes |
| **Assert (today, fail)** | Either (a) dropped prefix bytes (current), or (b) after Phase 1: transport closes with `relay_buffer_overflow` and **zero** successful peer flush of a truncated stream |
| **Also assert after Phase 1** | Ordered flush equals concatenated input when under budget |

### 0.3 H-4 ownership persist fail-open

| | |
|---|---|
| **File** | `internal/session/manager_owner_test.go` or `owner_persist_test.go` |
| **Drive** | Store stub / wrapper: `Save` returns error on claim and create paths; inject full disk simulation |
| **Assert (today, fail)** | `Authorize(..., claim=true)` returns nil and/or `Create` returns Meta despite Save error |
| **Assert (after Phase 1)** | Error returned; no successful claim; create does not report success; restart cannot widen ownership |

### 0.4 H-6 / M-7 destructive incomplete snapshots

| | |
|---|---|
| **Files** | `apps/mobile/test/history_replay_test.dart` (extend syncFromMeta cases); `session_history_paging_test.dart`; optional Go `store_test.go` for corrupt meta |
| **Drive A** | Populate notifier + cache for sessions S1,S2; call `syncFromMeta([])` or wrong-typed list path via `listSessions` mock returning success with missing `sessions` |
| **Assert (today)** | Transcripts/cache pruned (document current behavior) |
| **Assert (after Phase 1)** | No prune on incomplete/error; prune only on complete empty or explicit delete |
| **Drive B** | `sessionHistory` mock: page1 truncated + events, page2 empty list |
| **Assert (after Phase 1)** | Whole fetch fails; `resyncHistory` does not rebuild downward |

### 0.5 Phase 0 gate

```bash
# Tests exist and fail for the right reason on main (or xfail documented).
# After later phases, same tests pass.
cd apps/mobile && flutter test test/history_replay_test.dart \
  test/session_history_paging_test.dart test/relay_transport_test.dart
go test -race ./internal/session/ -run 'Owner|Persist|Claim'
```

**Commit shape:** test-only.

---

## Phase 1 — Fail-closed integrity (H-3, M-6, H-4, H-6, M-7)

**Goal.** Smallest high-impact correctness fixes. No big architecture moves.

### 1.1 H-3 + M-6 — Relay lossless or die

**Files:** `apps/mobile/lib/data/ws/relay_transport.dart`, tests.

**Implementation:**

1. Replace drop-oldest policy:
   - Track `_outerBufBytes` sum of frame lengths.
   - Caps: e.g. `kRelayOuterBufferMaxFrames = 64` and
     `kRelayOuterBufferMaxBytes = 256 * 1024` (tune: must stay under typical
     phone budget; mcrelay max frame is 1 MiB so never retain more than one
     max frame + handshake slack without peer).
2. On overflow: call `close()` with `McException(code: 'relay_buffer_overflow',
   permanent: false)` surfaced to the control client so reconnect can fire.
3. On unexpected post-join **text** frame: same fail-closed (protocol violation).
4. On outer stream `onError`: close both legs (remove `onError: (_) {}`).
5. On peer write failure during flush: close transport; do not continue with
   partial order.
6. Keep one-peer accept + server.close() (already correct).

**Acceptance:**

- Under-budget multi-frame sequence: exact byte equality after peer attach.
- Over-budget: typed error, no silent drop, no half-open splice.
- Unexpected text after join_ok: close.
- Outer error: close.

### 1.2 H-4 — Ownership persist fail-closed

**Files:** `internal/session/manager.go` (`Authorize`, `Create`, `writePersist`),
tests, optional `protocol` error code `persist_failed`.

**Implementation:**

1. Split persistence policy:
   - **Security-critical** (create insert, first-touch claim live + disk):
     `writePersist` / `Store.Save` error **returns** to caller.
   - **Advisory** (status-only debounced meta): keep log+continue but optional
     health flag later.
2. `Authorize` disk claim:
   ```go
   if err := m.store.Save(rec); err != nil {
     return fmt.Errorf("persist claim: %w", err)
   }
   ```
   Do **not** return nil after failed Save. Do **not** leave in-memory owner
   stamped if disk claim failed (live path: revert OwnerDeviceID or never set
   until Save succeeds — prefer set-under-lock only after Save, or set then
   rollback on failure).
3. `Create`: after map insert + pump, if `persistNow` fails:
   - Close and remove the live session (or never insert until Save succeeds —
     prefer **Save before advertising success**; if process already started,
     Close it and return error).
4. Map manager error to WS `persist_failed` / existing session error helper.
5. Tests: inject Save failures for create, live claim, disk claim.

**Acceptance:** success response ⇒ owner durable across restart; failure ⇒ no
broadened ownership.

### 1.3 H-6 — Non-destructive list snapshots

**Server files:** `internal/session/store.go`, `manager.go`,
`internal/protocol/messages.go`, `internal/ws/server.go`, `docs/protocol-v1.md`.

**Implementation (server):**

1. `Store.List` returns `([]Record, listStats, error)` or
   `ListResult{Records, SkippedCorrupt, RootError}`.
   - Per-row corrupt/unreadable: count + log; do **not** pretend full success
     without signaling.
2. `listFiltered` / `ListFor`: if store root error, return error to WS (not
   live-only silent).
3. `SessionListResultPayload` additive fields:
   ```go
   type SessionListResultPayload struct {
     Sessions []session.Meta `json:"sessions"`
     Complete bool           `json:"complete"`           // true only if full enum succeeded
     Degraded bool           `json:"degraded,omitempty"` // true if rows skipped
     Skipped  int            `json:"skipped,omitempty"`
   }
   ```
4. `handleSessionList`: set `Complete: true` only when store enum had no root
   error; `Degraded` when skipped > 0. Prefer `Complete: false` when degraded
   so clients refuse prune (stricter). **Decision for this plan:**  
   `complete = (rootErr == nil && skipped == 0)`.

**Client files:** `mcremote_client.dart`, `models.dart`,
`transcripts_notifier.dart`, `sessions_screen.dart`, `chat_screen.dart`.

**Implementation (client):**

1. `listSessions` requires `type == 'session.list_result'`; missing/invalid
   `sessions` → throw protocol error (not `[]`).
2. Parse `complete` (default **false** if absent for new clients talking to
   old daemons? Compatibility: old daemon omits field → treat as complete only
   when sessions is a List **and** we are mid-rollout).  
   **Rollout rule:**  
   - If payload has key `complete`: honor it.  
   - If key absent (old daemon): treat as complete=true only when `sessions is List` (preserve today’s behavior for old hosts) **but** never treat non-List as empty success.
3. `syncFromMeta(metas, {required bool authoritative})` or
   `syncFromMeta(SessionListSnapshot snap)`:
   - `retainOnly` / cache prune **only** when `authoritative && complete`.
   - Status adoption may still run for ids present in the snapshot when
     degraded.
4. Sessions/chat call sites pass the snapshot object, not bare lists.

**Acceptance:** corrupt meta.json → degraded list → phone keeps transcripts;
wrong response type → throw, no prune; explicit delete still removes.

### 1.4 M-7 — Multi-page history fail-closed

**Files:** `mcremote_client.dart` `sessionHistory`, `transcripts_notifier.dart`
`resyncHistory`, tests.

**Implementation:**

1. Every page: require `type == 'session.history_result'`.
2. If previous page had `truncated == true` and next page has missing/empty
   `events` while still truncated expectation: **throw** (do not return partial
   `out`).
3. Optional: if page empty and `truncated == false` and `out` non-empty, OK
   (finished).
4. `resyncHistory`: refuse rebuild if `maxSeq < local _lastSeq` unless gap
   policy explicitly allows ring reset (document); never rebuild from partial
   fetch (fetch already throws).
5. Fix L-2 dartdoc in same PR: document throw-on-error.

**Acceptance:** Phase 0.4 Drive B green.

### 1.5 M-8 partial — gap clear without unstamped rebuild

**Files:** `transcripts_notifier.dart`.

**Implementation (minimal, completes with Phase 2):**

1. If gap suspected and history `maxSeq == 0`: **do not** rebuild; set a
   per-session `historyDegraded` / clear gap only after logging degraded, or
   keep gap and expose `debugGapSuspected` until Phase 2 synchronizer retries
   later. Prefer: keep gap, set `historyUnstamped = true`, never destroy local
   transcript.
2. Chat open path (Phase 2 will own this): if gap or unstamped, still attempt
   resync when synchronizer runs.

**Acceptance:** unstamped history does not wipe local items.

### 1.6 Phase 1 gate

```bash
make pre-add-check
go test -race ./internal/session/... ./internal/ws/...
cd apps/mobile && dart format <touched> && dart analyze && \
  flutter test test/relay_transport_test.dart test/relay_byte_pipe_test.dart \
  test/history_replay_test.dart test/session_history_paging_test.dart
```

**Commit shape:** may split 1.1 | 1.2 | 1.3–1.5 if review size demands; prefer
1.1 alone then 1.2 then client+server list/history together.

---

## Phase 2 — Connection-scoped session synchronizer (H-1, M-8)

**Goal.** Exactly one owner of post-reconnect reconciliation.

### 2.1 Design

```text
McremoteClient.connectionStates
        │
        v
SessionSynchronizer (Notifier, keepAlive)
        │  on connected (edge)
        ├── listSessions() → snapshot (Phase 1 complete rules)
        ├── syncFromMeta(snapshot)
        ├── for each local session with lastSeq>0 or gapSuspected
        │     (bounded concurrency, e.g. 2–3)
        │       sessionHistory → resyncHistory
        ├── pendingAsks already in NotificationCoordinator (leave there)
        └── clear gaps / mark degraded
ChatScreen / SessionsScreen
        └── observe transcripts; no private resync ownership
```

### 2.2 Files

| Action | Path |
|---|---|
| **Add** | `apps/mobile/lib/state/session_synchronizer.dart` |
| **Wire** | `app_providers.dart`, `app_lifecycle.dart` or `app.dart` (ensure built once) |
| **Strip** | `ChatScreen._resyncAfterReconnect` history/list ownership → call synchronizer or delete |
| **Fix open** | `_maybeReplayHistory`: if `debugGapSuspected` / synchronizer reports gap, force history even when items non-empty |
| **Sessions** | keep metadata refresh; do not reimplement history |
| **Tests** | `session_synchronizer_test.dart` (Phase 0.1 green) |

### 2.3 Implementation steps

1. `SessionSynchronizer` dependencies: `McremoteClient`, `TranscriptsNotifier`.
2. Listen to connection edge `→ connected` (same pattern as transcripts gap
   marking; **move** gap marking here or keep in notifier but **only**
   synchronizer clears via resync).
3. Concurrency: `Pool`/`Future` wait with max 2 in flight; one failure must not
   cancel others (collect errors, log).
4. Dedup: generation counter so a second reconnect supersedes in-flight work.
5. Live events during history page: existing `_deferred` / resync rebuild
   paths must remain correct (do not regress MADR 0018).
6. ChatScreen connection listener: remove duplicate list+history; optional
   `await synchronizer.waitUntilIdle` for composer status only if needed.
7. Immediate safety if synchronizer delayed: `_maybeReplayHistory` consults
   gap flag (MADR H-1 “immediate safety fix”).

### 2.4 Acceptance (H-1)

1. Disconnect while A inactive; emit host events for A; reconnect; **never**
   open chat during outage; open A once → full retained transcript; gap false.
2. Bad/slow session B does not block A.
3. Live chunks during resync do not double-append (seq dedupe).
4. Sessions list refresh does not prune incomplete snapshots (Phase 1).

### 2.5 Phase 2 gate

```bash
cd apps/mobile && flutter test test/session_synchronizer_test.dart \
  test/history_replay_test.dart test/chat_render_test.dart
```

---

## Phase 3 — Request lifecycle + relay host (H-2, M-5)

### 3.1 H-2a — Bounded async handlers (no ledger yet)

**Files:** `internal/ws/server.go` `dispatchAsync`, daemon root context if
needed, tests `async_deadline_test.go`.

**Implementation:**

1. Pass a **server-lifecycle** context into `Server` (already partially true
   via request ctx — ensure daemon cancel closes in-flight work).
2. Replace naked `WithoutCancel` with:
   ```go
   opCtx, cancel := context.WithTimeout(context.WithoutCancel(lifecycleCtx), opTimeout(env.Type))
   defer cancel()
   ```
   where `lifecycleCtx` is cancelled on client close **and** server shutdown,
   and `WithoutCancel` is only used to detach from the **read-loop deadline**,
   not from lifecycle.
3. Per-type timeouts (starting point; tune under test):

   | Type | Timeout |
   |---|---|
   | `session.create` | 120s |
   | `session.prompt` | 60s (submit only; turn continues on events) |
   | `session.history` | 30s |
   | catalog lists | 60s |
   | default async | 30s |

4. On timeout: write error frame `deadline_exceeded` if still connected; free
   async slot (defer already decrements).
5. Client disconnect: cancel lifecycle-derived op ctx so providers that honor
   ctx stop; document providers that ignore ctx as residual risk.

**Acceptance:** blocked handler frees slot by deadline; shutdown drains.

### 3.2 H-2b — Idempotency ledger

**Files:** new `internal/ws/idempotency.go`, wire in `dispatchAsync` / mutating
handlers, `protocol-v1.md`.

**Implementation:**

1. Key: `(deviceID, requestID)` from envelope `id` (client already sends UUID).
2. States: `in_progress` | `done` with stored response envelope bytes or
   structured result.
3. TTL: e.g. 10 minutes; bound map size per device (e.g. 256 entries LRU).
4. Mutating types: `session.create`, `session.prompt`, `session.close`,
   `session.delete`, `session.rename`, `session.fork`, permission/question
   respond if not already sync.
5. In-progress duplicate: wait on condition or return `in_progress` error —
   prefer **wait with timeout** then return same result.
6. Android: optional later — preserve request id across single manual retry;
   **do not** auto-retry create/prompt until this ships.

**Acceptance:**

- Start succeeds, drop response, retry same id → one provider session.
- Duplicate prompt → one provider call.

### 3.3 M-5 — Relay host tunnel lifecycle

**Files:** `internal/relayhost/client.go`, tests.

**Implementation:**

1. `Client` holds `rootCtx` from `Run` (daemon lifecycle).
2. Control session uses `rootCtx`; registration attempts may use shorter
   child ctx.
3. `openTunnel(rootCtx, …)` — **not** `WithoutCancel(controlConnCtx)` that
   outlives `Run`.
4. Track `activeTunnels sync.WaitGroup` or map; on `Run` exit cancel root and
   wait with timeout for bridge goroutines.
5. Tunnels may outlive a **single** control reconnect only if `rootCtx` still
   live (optional: re-parent); must not outlive process/daemon stop.

**Acceptance:** daemon cancel closes tunnels within drain deadline; no
goroutine leak test.

### 3.4 Phase 3 gate

```bash
make pre-add-check
go test -race ./internal/ws/... ./internal/relayhost/...
```

---

## Phase 4 — Framing and protocol validation (M-1, M-2)

### 4.1 M-1 — Exact history and outbound caps

**Files:** `manager.go` HistoryPage, `event` sizing helper, `ws/server.go`
`writeJSON`/`writeBytes`.

**Implementation:**

1. Replace `approxEventBytes` with marshal-size of the **page payload** (or
   cumulative `json.Marshal` of events with early stop). Prefer building the
   page then measuring `len(json.Marshal(SessionHistoryResultPayload{...}))`.
2. Keep soft 512 KiB; always allow at least one event; if one event exceeds
   budget, return that event alone with `truncated` if more remain, or typed
   `event_too_large` if a single event exceeds hard max.
3. Hard outbound cap on every `writeJSON` (e.g. 1 MiB matching inbound): if
   exceeded, log + send error envelope or disconnect with reason — do not
   enqueue multi-megabyte frames.
4. Tests: nested questions/config, unicode, single oversize event.

### 4.2 M-2 — Version, kind, response type

**Server:**

1. Require `env.V == protocol.Version` (1). Reject missing/zero with
   `bad_version` **or** document one release of accepting 0 with deprecation
   log — **this plan: reject non-1 after one minor compatibility window flag
   `protocol.strict_version` default true in new builds**.
2. Inspect WebSocket message type from `conn.Read`; reject binary on control
   plane with protocol error / close.

**Client:**

1. `Envelope.fromJson`: if `v` present and `!= 1`, throw permanent protocol
   error; if absent, accept as 1 only during compatibility (mirror server).
2. `request` gains `expectedTypes: Set<String>` or required `expectedType`;
   methods pass e.g. `session.list_result`. Wrong type → throw, never empty
   success.
3. `_onMessage`: if data is not String, fail connection or ignore with metric —
   do not `as String` cast crash (cast is caught today; make typed).

**Optional follow-on (not blocking):** `Sec-WebSocket-Protocol: mcremote.v1`
negotiation with rollout — document only in this phase unless low-risk.

### 4.3 Phase 4 gate

```bash
go test -race ./internal/ws/... ./internal/session/... ./internal/protocol/...
cd apps/mobile && flutter test test/session_history_paging_test.dart # + new protocol tests
```

---

## Phase 5 — History durability bounds (M-3)

**Files:** `internal/session/manager.go` history timer paths, tests in
`manager_durable_test.go`.

**Implementation (D9 interim):**

1. Track `historyDirtySince map[string]time.Time` on first mark.
2. On schedule: if `now - dirtySince >= historyMaxLatency` (recommend **5s**),
   flush that id immediately even if debounce keeps resetting.
3. Prefer per-session timers **or** on shared tick scan dirtySince.
4. Document crash-loss bound: “at most historyMaxLatency of tail under
   continuous stream.”
5. Optional Phase 5b (separate commit/spike): append-only `history.jsonl` +
   compact to ring snapshot — only if 5a insufficient under load.

**Acceptance:** continuous event every 100ms for 10s still flushes by 5s+ε;
quiet session not starved forever by noisy peer (per-id dirtySince).

---

## Phase 6 — FGS honesty (H-5a)

**Goal.** Match product claims to architecture **without** full isolate move.

**Files:** `foreground_service.dart`, `notification_coordinator.dart`,
`app_lifecycle.dart`, Settings strings, `standards/mobile/android.md`,
user-visible notification text.

**Implementation:**

1. **Start FGS only from foreground user-visible paths** or when already in
   foreground (`appForegrounded == true`). On
   `ForegroundServiceStartNotAllowedException`, do not pretend connected;
   surface Settings guidance.
2. Notification title/text must reflect **actual** `McConnectionState`, not
   aspirational “Listening” after failed start.
3. Document in Settings: background alerts require the process to remain alive;
   swiping away stops service (`stopWithTask=true` remains intentional).
4. Fix concurrent `coord.start()` / `setEnabled()` race: sequence
   `await notifs.init()` → permission → then service start.
5. Restart receiver: either disable false promise or show “Open app to
   reconnect” if socket not restored.
6. Update `docs/standards/mobile/android.md` to match.

**Out of scope for 6:** moving `McremoteClient` to service isolate (Phase 9).

**Acceptance:** no false “Connected/Listening” without live authenticated
socket; BG start denial does not crash; copy matches behavior.

---

## Phase 7 — Single markdown engine (M-9…M-12, L-3, L-4)

**Goal.** Session chat never shows raw GFM source for ordinary agent replies
during stream, and does not snap engines at finalize.

### 7.1 Chosen approach (D8)

**Primary path for stream and finalize:**

- Frame-throttled rebuild (keep Proposal F).
- `data: bufferStreamingMarkdown(text)` while `streaming == true`.
- Always `MarkdownBody` (or one shared builder widget) with the same
  `MarkdownStyleSheet` and `pre` builder.
- On finalize: `data: text` without buffer closers (unchanged contract).

**Retire as default:** `_buildFromParsed` isolate RichText stream path and the
`kMaxStreamingMarkdownChars` mono plain path.

**Performance safeguard:**

1. Keep post-frame throttle (1 render/frame max).
2. Keep scroll suppress (L-3 fix: dirty flag + reschedule on scroll end).
3. Optional: raise work threshold — if profiling shows jank on huge streams,
   clamp **visible** stream window (e.g. last N chars of markdown) with a
   “scroll for full” affordance rather than mono source dump.
4. Isolate parse may remain for **finalized** huge messages or for a future
   block cache — not a second incompatible engine mid-stream.

### 7.2 Stream closer (M-10)

Either:

- **A (preferred with MarkdownBody):** expand closer carefully for `*` /
  `~~` / links with same precedence rules as today, **or**
- **B:** accept that incomplete tokens may briefly style wrong but must not
  show raw if GFM parser treats open markers as text — verify visually.

Ship tests for open italic/strike/link so regressions fail.

### 7.3 L-3 scroll reschedule

When `ChatScrollActivity` goes true→false and `_dirtyPending`, call
`_updateStreamingRender`. Wire via listening to the same `ValueNotifier` used
by `ChatScrollActivitySensor`.

### 7.4 L-4

If isolate path remains for any mode, add `del`/`s` → strike `TextStyle`. If
retired, covered by MarkdownBody.

### 7.5 Tests

| Test | Expect |
|---|---|
| Stream table | MarkdownBody present; not fused `ab12` plain |
| Stream ordered list | visual numbers not all `1.` (MarkdownBody) |
| Stream `*open` | no permanent raw after buffer/parser policy |
| >4k stream | still markdown-styled (or documented window clamp), not mono source of full text |
| Finalize | no engine-identity flash test (same State widget type) |
| Existing 0018 parse-count tests | rewrite counts for single-engine policy |

### 7.6 Phase 7 gate

```bash
cd apps/mobile && flutter test test/streaming_markdown_test.dart \
  test/markdown_parser_test.dart test/chat_render_test.dart
```

---

## Phase 8 — Docs and low polish (L-1, L-2, gated M-4)

### 8.1 L-1

Update `ChatScreen` honesty copy/comments: host keeps last **800** events
durably; empty may mean never recorded / corrupt / not live, not “always
cleared on close.”

### 8.2 L-2

Already fixed if Phase 1.4 touched dartdoc; verify.

### 8.3 M-4 (gated) — **deferred (product)**

**Decision (2026-07-30):** do **not** extend the wire for phone message-level
fork/diff/revert yet. Daemon RPC and other clients may still use
`message_id`/`part_id` where the provider supplies them. Android continues to
omit `session.revert` / `session.unrevert` construction (see
`mcremote_client.dart` comments). Revisit only with an explicit product ask.

### 8.4 Standards drift

Align `standards/mobile/android.md` FGS text with Phase 6 reality (and Phase 9
when done).

---

## Phase 9 — Service-owned connection (H-5b)

**Goal.** Deliver the background contract for mesh/local without cloud push.

### 9.1 Architecture

```text
UI isolate                          Service / background isolate
────────                            ────────────────────────────
credentials read  ◄────────────►  secure store access
pending asks UI   ◄── commands ──►  McremoteClient socket
transcript mirror ◄── events ────  reconnect policy
                                  notification post
```

Use `flutter_foreground_task` data ports or a dedicated background entrypoint
that owns:

- token + pin + client cert load
- `McremoteClient.connect` / reconnect
- event → notification
- permission respond from notification actions

UI becomes a subscriber. SessionSynchronizer either lives in service isolate
or consumes mirrored state.

### 9.2 Acceptance (on-device matrix)

| Scenario | Expect |
|---|---|
| Screen lock | socket stays; asks notify |
| Doze | eventual delivery within OS limits |
| Process kill with FGS | restart restores socket **or** honest “open app” |
| API 31+ BG start | no silent failure loop |
| Swipe away | stop (stopWithTask) terminal by design |
| Duplicate permission respond | one effect |

### 9.3 Gate

On-device manual + instrumented tests; unit tests for isolate message protocol.

**Estimate:** large; do not start until Phases 1–3 and 6 are stable.

---

## 5. Cross-cutting protocol changelog (accumulate in protocol-v1.md)

| Field / rule | Phase | Notes |
|---|---|---|
| `session.list_result.complete` / `degraded` / `skipped` | 1 | Additive |
| Fail closed missing `sessions` | 1 | Client |
| Async `deadline_exceeded` | 3 | Error code |
| Idempotent mutating requests by `id` | 3 | Document semantics |
| Strict `v: 1` | 4 | Compatibility flag |
| Text-only control frames | 4 | |
| Exact history budget | 4 | Soft 512 KiB measured |
| Optional `message_id` on events | 8 gated | |

---

## 6. Test inventory (must exist when plan complete)

### Go

- Owner persist fail create/claim
- List degraded/complete flags
- Async deadline releases slot
- Idempotent create/prompt
- Relayhost tunnel cancel on root ctx
- History page exact size / oversize single event
- Version reject / binary reject

### Flutter

- Relay byte equality + overflow fail-closed + outer error close
- listSessions incomplete no prune
- sessionHistory truncated+empty throws
- SessionSynchronizer inactive session heal
- Gap + unstamped no wipe
- Markdown single-engine stream table/list
- Scroll-end reschedule
- FGS start policy unit tests where possible

---

## 7. Risk register

| Risk | Mitigation |
|---|---|
| Strict `complete` breaks old app against new daemon | Additive fields; old apps ignore; new apps default carefully (Phase 1 rollout rule) |
| Strict version breaks old clients | Flag + short dual-accept window |
| Idempotency memory growth | LRU + TTL per device |
| Markdown always-MarkdownBody jank | Frame throttle + optional visible window; profile on mid phone |
| H-5b scope explosion | H-5a first; H-5b separate milestone |
| Create rollback after Start leaves orphan process | Close provider on persist failure path; test with fake provider |

---

## 8. Definition of done (whole plan)

- [x] All Phase 0 tests green
- [x] H-1…H-6 acceptance (H-5 via H-5a honesty; H-5b deferred)
- [x] M-1…M-12 and L-1…L-4 closed or deferred (M-4 deferred; markdown via 0056/0057)
- [x] `protocol-v1.md` matches wire (idempotency + deadline notes)
- [x] Go precheck + race on touched packages; flutter analyze/tests for mobile
- [x] MADR 0056 status → **Accepted** with H-5b deferred
- [x] Product copy matches H-5a (FGS keeps process, not service-owned socket)

---

## 9. Suggested first week execution

| Day | Work |
|---|---|
| 1 | Phase 0 tests (H-1 harness, relay overflow, owner persist, list/history) |
| 2 | Phase 1.1 relay fail-closed |
| 3 | Phase 1.2 owner fail-closed |
| 4–5 | Phase 1.3–1.5 list/history + client prune rules |
| 6–8 | Phase 2 synchronizer |
| 9+ | Phase 3 deadlines; parallel Phase 7 markdown if UI pain is top priority |

If product prioritizes “chat looks raw” over multi-device ownership edge cases,
swap **Phase 7 earlier** after Phase 1.1 only — but **do not** skip Phase 1.3
before Phase 2 (prune safety).

---

## 10. Plan maintenance

- When a phase lands, tick its section and add commit SHA under the phase
  heading.
- If implementation diverges, update **this plan** and the MADR decision table
  in the same PR.
- Do not open MADR 0057+ for the same backlog until this plan is Accepted or
  explicitly superseded.
