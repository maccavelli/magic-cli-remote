# MADR 0045 — Implementation plan: mobile app hardening audit

Companion to [MADR 0045](./0045-MADR-mobile-app-hardening-audit.md). Read that
first: it carries the verified findings, decisions, and severity rationale.
This document is the build order — thorough, phase-sequenced, and keyed to
current source locations as of 2026-07-28 (`master` @ merge of
`feat/auto-approve-modes`).

- **Status**: Proposed
- **Date**: 2026-07-28
- **Scope**: `apps/mobile` (Dart/Flutter/Android) + minimal daemon touch for
  H4 (typed oversize error), H5 (`session_title` → `Meta.Name`), and protocol
  docs
- **Standards**: `/home/mac/standards/mobile` v3.12.2-v3 — `networking.md`,
  `architecture.md`, `dart.md`, `flutter.md`, `android.md`
- **Related**: [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md),
  [MADR 0015](./0015-MADR-mcrelay-transport-security.md),
  [MADR 0042](./0042-MADR-android-app-remediation.md) /
  [plan](./0042-plan-android-app-remediation.md),
  [MADR 0044](./0044-MADR-auto-approve-modes.md)

---

## 0. Goal, non-goals, and ground rules

### Goal

Close every verified finding in MADR 0045 (H1–H10, T1–T4, P1–P4, C1–C4,
S1–S3, N1, W1–W3, and the low table in §5) with independent, testable
changes. Prefer the MADR's decisions; do not re-litigate severity.

### Non-goals

- R8 / resource shrinking, ProGuard keep rules, or release-pipeline work.
- Replacing the WS transport, rewriting Riverpod graph, or redesigning chat
  layout.
- New agent providers or new event types (except documenting / consuming
  fields the daemon already emits: `title`, `timed_out`).
- A full daemon "re-emit pending asks on attach" protocol change in this
  plan — H8 ships client-side reconciliation first; the daemon re-emit is
  recorded as a **protocol follow-up** (see Phase A11).
- Live-tagged provider tests unless a phase explicitly needs them (H5 daemon
  unit tests suffice).

### Ground rules

1. **One phase → one commit** (or a tightly related pair when a single finding
   spans app+daemon). Do not push unless the user asks.
2. **Go gate before `git add`**: `make pre-add-check` / `scripts/go-precheck.sh`
   for any touched `.go` files (`gofmt`, `golint`, `govulncheck`).
3. **Dart gate**: `dart format` on every touched `.dart` file; `flutter analyze`
   + `flutter test` for the packages you touch; `make preflight` before calling
   a multi-phase block done.
4. **Commit without `-m`**: the prepare-commit-msg hook generates the message.
5. **Line numbers drift**. Prefer symbol names + surrounding comments from this
   plan over exact line numbers when applying patches.
6. **L-tool (NUL) is Phase 0**, not deferred polish: a source file grepped as
   binary has already produced false positives in this audit.

### File map (canonical paths)

| Area | Path |
|---|---|
| WS client | `apps/mobile/lib/data/ws/mcremote_client.dart` |
| Relay bridge | `apps/mobile/lib/data/ws/relay_transport.dart` |
| Protocol models | `apps/mobile/lib/data/protocol/models.dart` |
| Pair URI | `apps/mobile/lib/data/protocol/pair_uri.dart` |
| Settings / pins | `apps/mobile/lib/data/local/settings_store.dart` |
| Transcripts | `apps/mobile/lib/state/transcripts_notifier.dart` |
| Reducer | `apps/mobile/lib/data/chat/transcript_reducer.dart` |
| Markdown | `apps/mobile/lib/data/chat/markdown_parser.dart` |
| Chat UI | `apps/mobile/lib/features/chat/chat_screen.dart` |
| Bubbles | `apps/mobile/lib/features/chat/chat_bubble.dart` |
| Connect | `apps/mobile/lib/features/connect/connect_screen.dart` |
| Sessions | `apps/mobile/lib/features/sessions/sessions_screen.dart` |
| Settings UI | `apps/mobile/lib/features/settings/settings_screen.dart` |
| Router | `apps/mobile/lib/app.dart` |
| Notif coordinator | `apps/mobile/lib/data/notifications/notification_coordinator.dart` |
| Notif service | `apps/mobile/lib/data/notifications/notification_service.dart` |
| Notif policy | `apps/mobile/lib/data/notifications/agent_notifications.dart` |
| FGS | `apps/mobile/lib/data/notifications/foreground_service.dart` |
| Daemon events | `internal/event/event.go` |
| Session manager | `internal/session/manager.go` |
| WS server | `internal/ws/server.go` |
| Protocol doc | `docs/protocol-v1.md` |

### Verified baseline facts (do not rediscover)

These were re-checked against source while writing this plan:

| Claim | Evidence |
|---|---|
| H1: `connect()` sets host via `_setLastHostInput` then `_resolvePin` before `_noteHost` | `mcremote_client.dart` `connect()` (~654–656) vs `_connectInternal` / `_noteHost` (~808, 942) |
| H1: by-id pin lookup ignores stored authority | `settings_store.dart` `getPinnedCert` returns `pins['id:$id']` without authority check (~289–291) |
| H2: resync gates on window extremes only | `transcripts_notifier.dart` `resyncHistory` `missedNewer`/`missedOlder` (~701–705) |
| H3: pair applies `hostAuthority` only | `connect_screen.dart` `_applyPair` (~257); `PairPayload.host` / `secure` exist and are unused there |
| H4: 4 MiB raw cap vs 1 MiB WS read limit | `chat_screen.dart` ~538; `internal/ws/server.go` `maxWSMessageBytes = 1 << 20` |
| H5: no `title` on `SessionEvent`; no app `session_title` handling | `models.dart` `SessionEvent` / `fromJson`; sessions `_onSessionEvent` only `session_status` / `turn_complete` |
| H5: daemon pump never folds title into `Meta.Name` | `manager.go` pump (~463–494) handles status/commands/mode/usage only; rename at ~987 is user-initiated |
| H6: policy allows `question_request`; switch has no case | `agent_notifications.dart` `shouldNotify`; `notification_coordinator.dart` `_onEvent` switch |
| H7: `error` stops FGS; `reconnecting` starts it | `_onConn` (~168–179) |
| H9: question failure removes id from `_presentedQuestionIds` | `chat_screen.dart` ~1572 |
| H10: `ChatScreen` built without key | `app.dart` ~33 |
| T1: epoch claimed in `claimPairCode` but post-`pair.claim` mutations unguarded | ~676 vs ~723+ |
| T2: `_relayTransport = transport` inside `_openSocketViaRelay` before caller epoch check | ~465 |
| L-tool: one literal NUL in `sessions_screen.dart` | `'$p\x00$modelProvider'` (binary to grep) |

---

## 1. Dependency graph

```
Phase 0  L-tool (NUL) ──────────────────────────────────────────────┐
                                                                     │
Phase A1 H4 size budget ─────────────────────────────────────────────┤
Phase A2 H3 scheme ──────────────────────────────────────────────────┤
Phase A3 H10 key + S1 deep-link stash ───────────────────────────────┤
Phase A4 H9 question modal loop ─────────────────────────────────────┤
Phase A5 H1 pin identity + T3 token wipe ──┐                         │
Phase A6 T4 single-peer bridge ────────────┼─ security cluster       │
                                           │                         │
Phase A7 H2 seq-gap resync ────┐           │                         │
Phase A8 H5 session_title ─────┤           │                         │
Phase A9 H6 question notifs ───┼─ notify / │                         │
Phase A10 H7 FGS lifecycle ────┤  reconnect│                         │
Phase A11 H8 ask reconcile ────┘  (A7+A9   │                         │
                                  feed A11)│                         │
                                           │                         │
Phase B1 T1/T2 epoch completeness ─────────┘                         │
Phase B2 P1 staged images (after A7 preferred)                       │
Phase B3 P2 + P4 streaming markdown                                  │
Phase B4 P3 keystore latch                                           │
Phase B5 C1–C4 chat UI                                               │
Phase B6 S2/S3 connect safety                                        │
Phase B7 N1 background park retry                                    │
Phase B8 W1–W3 protocol UI                                           │
                                                                     │
Phase C  remaining lows (error typing, polish, protocol doc L-w*) ───┘
                                                                     │
Phase V  full verification (analyze, test, race, preflight) ─────────┘
```

**Parallelism (safe once Phase 0 is in):**

- A1, A2, A3, A4 are independent of each other.
- A5 and A6 are independent; both touch connection security.
- A7 is independent of A5/A6; do it before A11 and ideally before B2.
- A8 is independent of A7; do before or after A9.
- A9 before A11; A10 can land with either.
- B-phases after their A dependencies; B3–B8 largely independent of each other.

**Suggested ship order for a single sequential implementer** (value × risk):

`0 → A1 → A2 → A3 → A4 → A5 → A6 → A7 → A8 → A9 → A10 → A11 → B1 → B2 → B3 → B4 → B5 → B6 → B7 → B8 → C → V`

---

## Phase 0 — L-tool: remove the NUL byte (prerequisite)

**Closes:** L-tool  
**Files:** `apps/mobile/lib/features/sessions/sessions_screen.dart`  
**Why first:** the literal `\x00` makes the entire ~1.5k-line file binary to
`grep`/`git grep`, which already caused a false-positive audit finding (N5)
and briefly hid `session.create` callers. Every later phase that greps this
file is unreliable until this lands.

### Steps

1. Locate the composite map key built as `'$p\x00$modelProvider'` (create-
   session / model-provider step; ~line 403 region).
2. Replace the separator with a printable delimiter that cannot appear in
   either side under existing validation — prefer `'|'` (provider ids and model
   ids in this app do not contain `|`). Result: `'$p|$modelProvider'`.
3. Update any matching parse/split of that key in the same file (search for
   the same string construction and any `split`/`substring` that assumed NUL).
4. Confirm with:

   ```bash
   # must print 0
   python3 -c "print(open('apps/mobile/lib/features/sessions/sessions_screen.dart','rb').read().count(b'\\x00'))"
   # must no longer say "binary file matches"
   rg -n "session\.create|sessionLabels" apps/mobile/lib/features/sessions/sessions_screen.dart | head
   ```

### Tests

- Existing `sessions_screen_test.dart` / `model_provider_step_test.dart` must
  still pass (key is internal to a map; behaviour unchanged if all writers and
  readers agree).
- If a unit test constructs the same key, update it.

### Acceptance

- File is text to ripgrep; zero NUL bytes; create-session / model cache still
  works.

### Rollback

Single string change.

---

## Phase A1 — H4: encoded attachment budget (stop socket kills)

**Closes:** H4 (and preconditions W2)  
**Files:**
- `apps/mobile/lib/features/chat/chat_screen.dart` (pick path ~538)
- optionally a small shared helper under `apps/mobile/lib/data/protocol/` or
  next to prompt types
- `docs/protocol-v1.md`
- `internal/ws/server.go` (typed oversize path)
- daemon WS tests if present

### Problem recap

Composer accepts **4 MiB raw** images, then `base64Encode`s (~+33%) into one
`session.prompt` frame (`mcremote_client.dart` `prompt` → `request` →
`ch.sink.add`). Daemon `conn.SetReadLimit(1 << 20)` closes with
`StatusMessageTooBig`. A normal multi-megapixel photo kills the socket and
loses the prompt.

### Steps

#### A1.1 Client: cap on encoded size

1. Define a single constant, e.g. `kMaxPromptFrameBytes = 1 << 20` (mirror
   daemon) and `kPromptEnvelopeOverhead` (conservative fixed budget for JSON
   envelope + `session_id` + `text` field — **≥ 4 KiB**, document the
   constant).
2. Effective max raw payload for attachments:

   ```
   maxBase64Payload = kMaxPromptFrameBytes - kPromptEnvelopeOverhead - utf8(text).length
   maxRawBytes ≈ floor(maxBase64Payload * 3 / 4)
   ```

   Refuse **before** staging when `base64 length of candidate + other staged
   attachments + envelope ≥ budget`. Practical raw ceiling for a single image
   with empty text is ~700–750 KiB; do not hardcode 700 without computing from
   the constants.
3. Replace the `4 * 1024 * 1024` check in the image picker path with the shared
   helper. User-visible message must state the real limit (e.g. "Image too large
   for the connection (max ~N KB after encoding)") — not "4 MB".
4. Apply the same helper when adding a second/third image so cumulative staged
   attachments stay under budget.
5. Keep the check client-side even if the daemon later returns a typed error:
   preventing the kill is the primary fix.

#### A1.2 Protocol doc

In `docs/protocol-v1.md`:

- Document that each client→daemon WebSocket text frame is capped at **1 MiB**
  (`maxWSMessageBytes`).
- Document that `session.prompt` attachments are base64 in-frame; clients must
  size-check **encoded** content.
- Document error code `payload_too_large` (see A1.3).

#### A1.3 Daemon: typed error instead of transport death (defense in depth)

Today the library closes the connection before application code runs. Options
ranked:

1. **Preferred if feasible without forking `coder/websocket` behaviour:** keep
   `SetReadLimit(1 MiB)` (DoS protection) **and** document that older clients
   still die on breach. New clients never hit it (A1.1). Optionally raise the
   limit only after a negotiated capability — **out of scope** unless trivial.
2. **Application-level guard for messages that do parse under the limit but
   are rejected for policy:** not applicable when the frame itself exceeds the
   limit.
3. **Practical daemon deliverable for this phase:** add `payload_too_large` to
   the documented error code table and, if there is any post-read size check on
   decoded prompt attachments (or if read limit is raised later), return:

   ```json
   { "type": "error", "payload": { "code": "payload_too_large", "message": "..." } }
   ```

   without tearing down the session. If the transport still kills on >1 MiB,
   note that explicitly in the doc so operators know A1.1 is the real fix.

Do **not** silently raise the WS read limit without a DoS review.

### Tests

- Unit: helper rejects a buffer whose base64 length exceeds the budget; accepts
  one just under.
- Widget/unit: picker path surfaces the friendly message and does not call
  `prompt` (mock client).
- Extend any existing staged-image test (`staged_images_test.dart`) if it
  assumes 4 MiB.

### Acceptance

- A ~2 MB photo never opens a socket-killing frame; user sees a clear error;
  existing small images still send.
- Protocol doc mentions the 1 MiB frame budget.

### Rollback

Revert cap + doc; behaviour returns to today's breakage for large photos.

---

## Phase A2 — H3: preserve plaintext pair scheme

**Closes:** H3  
**Files:** `apps/mobile/lib/features/connect/connect_screen.dart`,
`apps/mobile/test/connect_screen_test.dart`, `apps/mobile/test/pair_uri_test.dart`

### Problem recap

`_applyPair` sets `_hostCtrl.text = payload.hostAuthority` and drops
`payload.secure` / `payload.host`. `parseEndpoint` defaults bare authority to
**secure**, so `ws://100.64.0.1:7531` becomes `wss://` and fails against a
plaintext daemon. `pair_uri_test.dart` already pins that `payload.host` keeps
the insecure scheme — the screen throws it away.

### Steps

1. When applying a pair payload, populate the host field from a scheme-aware
   value:
   - If `!payload.secure`, set `_hostCtrl.text` to `payload.host` with any
     `#fp=…` fragment stripped **or** explicitly `'ws://${payload.hostAuthority}'`.
   - If `payload.secure`, bare authority remains fine (default TLS).
2. Prefer threading `payload.host` (already "connection input ready for
   `parseEndpoint`") after stripping only the fingerprint fragment if the
   fingerprint is carried separately via `_pendingFingerprint` (today's design).
   Do **not** leave `#fp=` in the visible host field.
3. Keep `_pendingFingerprint` / `_pendingTlsMode` as today.
4. Ensure `_claimCode` / `_connect` pass the host field through
   `claimPairCode`/`connect` unchanged so `normalizeWsUrl` / `parseEndpoint`
   see `ws://`.

### Tests

- Connect-screen (or pair-apply) test: payload with `secure: false` /
  `host: 'ws://100.64.0.1:7531'` results in host field / connect input that
  `parseEndpoint` reports `secure: false`.
- Existing pair_uri tests remain green.
- Regression: secure QR still dials `wss`.

### Acceptance

- Plaintext daemon QR is pairable end-to-end without silent TLS upgrade.

### Rollback

One screen function.

---

## Phase A3 — H10 + S1: per-session ChatScreen identity and cold-start deep link

**Closes:** H10, S1  
**Files:** `apps/mobile/lib/app.dart`, connect success paths in
`connect_screen.dart`, possibly a tiny holder on `McremoteClient` or a
provider for "pending post-auth location"

### H10 — Steps

1. In the `/sessions/:id` builder (`app.dart` ~29–34):

   ```dart
   return ChatScreen(
     key: ValueKey(id),
     sessionId: id,
     sessionName: name,
   );
   ```

2. Do **not** implement a large `didUpdateWidget` reset unless a follow-up
   proves go_router still reuses State (it should not with a changing
   `ValueKey`).

### S1 — Steps

1. In `GoRouter.redirect`, when unpaired and the attempted location is
   `/sessions/<id>…`, stash that full location (path + query) before returning
   `'/'`.
2. Stash storage options (pick one; prefer simplest that tests cleanly):
   - a field on `McremoteClient` / small `PendingNav` provider cleared on
     sign-out and after successful consume; or
   - `SettingsStore` only if it must survive process death (nice-to-have; not
     required for the warm redirect race).
3. ConnectScreen success navigation currently goes to `/sessions` (list).
   Change success paths (`_claimCode` / `_connect` success) to:
   `context.go(pending ?? '/sessions')` then clear the stash.
4. Clear stash on explicit credential clear / logout.
5. Composes with H10: after auth, `go('/sessions/$id')` builds a **fresh**
   `ChatScreen` for that id.

### Tests

- Widget: build two routes with different ids; assert State is not reused
  (e.g. distinct `Key`s, or a test-only `debug` counter on `initState`).
- Redirect: unpaired app with pending `/sessions/abc` ends on that chat after
  simulated pair success (mock client `shouldStayInApp`).
- Sign-out clears pending so the next pair does not jump to a stale id.

### Acceptance

- `/sessions/A` → `/sessions/B` never flushes A's queued prompts into B.
- Notification / deep link to a session after cold start + pair opens that
  session, not the list.

### Rollback

Key line + stash helpers.

---

## Phase A4 — H9: question sheet must not loop

**Closes:** H9  
**Files:** `apps/mobile/lib/features/chat/chat_screen.dart`,
`apps/mobile/test/permission_loop_test.dart` (extend)

### Problem recap

Permission path keeps failed ids in `_presentedPermissionIds` and retries only
via the banner Review button (`permission_loop_test.dart`, commit `00d15b7`).
Question path on failure does `_presentedQuestionIds.remove(questionId)`
(~1572) while the id remains in `pendingQuestions` → post-frame
`_maybeShowQuestion` reopens forever.

### Steps

1. In the question sheet's catch path, **do not** remove `questionId` from
   `_presentedQuestionIds`.
2. Surface `friendlyOpError(e)` the same way the permission path does.
3. Ensure banner / Review affordance for questions already exists or mirror the
   permission banner entry point so deliberate retry still works (add id
   removal only on explicit Review, if that is how permissions work).
4. On successful resolve or `question_resolved` / external dismiss, clear the
   presented id as permissions do today.

### Tests

Extend `permission_loop_test.dart` (or sibling `question_loop_test.dart`):

- Mock client whose `respondQuestion` always throws.
- Seed transcript with `pendingQuestions`.
- Pump chat; submit or cancel; assert sheet does **not** reappear in a loop
  (e.g. respond attempts == 1 after settle, sheet finder empty after dismiss
  attempt without Review).
- Review path can re-present once.

### Acceptance

- Permanent question RPC failure is escapable without system back; no tight
  modal loop.

### Rollback

Restore remove-on-failure (not recommended).

---

## Phase A5 — H1 + T3: pin identity and failed re-pair token hygiene

**Closes:** H1, T3  
**Files:**
- `apps/mobile/lib/data/ws/mcremote_client.dart`
- `apps/mobile/lib/data/local/settings_store.dart`
- `apps/mobile/test/cert_pinning_test.dart`
- client connection tests if any

### H1 — Steps

1. In `connect()`, replace:

   ```dart
   _setLastHostInput(hostInput);
   _lastToken = token;
   await _resolvePin(hostInput, fingerprint, mode);
   ```

   with host-identity-safe ordering:

   ```dart
   _noteHost(hostInput); // clears deviceId when authority changes
   _lastToken = token;
   await _resolvePin(hostInput, fingerprint, mode);
   ```

   so `_noteHost`'s authority-change guard still sees the **previous** host
   when comparing `prev` vs `next`.

2. In `getPinnedCert`:
   - When returning the by-id record (`pins['id:$id']`), **require**
     `rec['authority'] == authority` for the dialled host.
   - If the by-id record exists but authority mismatches, fall through to the
     authority scan loop (do not return the foreign pin).

3. In `setFingerprint`:
   - When writing `id:$id`, never overwrite a record whose stored authority is
     a **different** host without first ensuring the keying model is
     host-scoped. Today the key is `id:$deviceId` only — that is the root
     clobber. Prefer:
     - key pins as `id:$deviceId` **plus** authority in the value and refuse
       to return mismatches (step 2), **and**
     - on set, if existing `id:$id` has a different authority, either migrate
       to a composite key `id:$deviceId:$authority` or keep one slot but only
       after explicit host switch (clearing old). **Minimum fix matching the
       MADR:** authority check on read + on write, if `id:$id` points at
       another authority, do not destroy it by blind overwrite — use
       `host:$authority` secondary and/or composite keys.

   Concrete minimal algorithm (implement this unless a cleaner composite lands):

   ```
   getPinnedCert(host):
     id = deviceId ?? stored
     auth = authority(host)
     if id != null:
       rec = pins[id:id]
       if rec != null && rec.authority == auth: return rec
     // existing authority scan loop...

   setFingerprint(host, fp):
     id = ...
     auth = authority(host)
     if id != null:
       rec = pins[id:id]
       if rec != null && rec.authority != auth:
         // preserve old host pin under host:oldAuth if missing
         pins.putIfAbsent(host:rec.authority, rec)
     write pins[id:id] = {fp, authority: auth, ...}  // current host wins id slot
   ```

   Document the trade-off: one active id-keyed pin (current daemon); previous
   daemon retained under `host:` key for reconnect-by-authority.

4. Extend `cert_pinning_test.dart` (~324 region): store a pin under device id
   for host A; `getPinnedCert(B)` must not return A's pin; `setFingerprint(B)`
   must not erase A's recoverable record.

### T3 — Steps

1. On any `claimPairCode` failure path that does not issue a new token
   (`_failHandshake`, connect errors after `_noteHost(B)`):
   - clear `_lastToken` and set `_paired = false` when the attempt was a
     **re-pair to a different host** or when no token was obtained; at minimum
     clear `_lastToken` so `hasCredentials` is false until success.
2. Align with `_failHandshake`'s permanent/`invalid_token` logic: failed pair
   to host B must not leave `_lastHostInput=B` + `_lastToken=A` with
   `_paired==true`.
3. Prefer: on entering `claimPairCode`, if authority changes via `_noteHost`,
   clear `_lastToken` **before** the await (credentials are for the previous
   host). On success, set the new token.

### Tests

- Pin: A then B as above.
- Client: simulate failed pair to B after paired A; assert
  `reconnectFromStore` / `hasCredentials` does not attempt B with A's token
  (inspect `_lastToken` via test seams or public getters if available; add
  `@visibleForTesting` only if necessary).

### Acceptance

- Warm host switch never dials B with A's pin; false `cert_mismatch` gone.
- Failed re-pair never auths B with A's bearer token.

### Rollback

Revert connect ordering + pin checks + token clears.

---

## Phase A6 — T4: single-peer loopback bridge

**Closes:** T4  
**Files:** `apps/mobile/lib/data/ws/relay_transport.dart`,
`apps/mobile/test/relay_transport_test.dart`

### Steps

1. In the `server.listen` accept handler (~136 region), after the first
   successful peer attach:
   - call `server.close()` so no further accepts occur, **or**
   - reject additional sockets while `_peer != null` and healthy (close the
     new socket immediately).
2. Prefer **close after first accept** — simplest and matches "exactly one
   connection per transport".
3. Keep outer WSS + inner TLS semantics unchanged.
4. Document in a short comment: co-resident apps can no longer splice the
   bridge; DoS-by-eviction is closed.

### Tests

- Open transport; connect one loopback peer; second dial fails or is ignored
  without replacing the first peer's pipe.
- Existing relay path tests stay green (`relay_path_test.dart`,
  `relay_transport_test.dart`).

### Acceptance

- Only one peer ever owns the tunnel for a given `RelayTransport` lifetime.

### Rollback

Restore unlimited accept + `_replacePeer`.

---

## Phase A7 — H2: seq-gap aware resync

**Closes:** H2  
**Files:** `apps/mobile/lib/state/transcripts_notifier.dart`,
`apps/mobile/test/history_replay_test.dart`

### Problem recap

`resyncHistory` only checks `maxSeq > last` and `minSeq < first`. Live events
after reconnect advance `_lastSeq` before history returns; both gates false →
permanent hole for mid-window sequences missed during the outage. Common when
the daemon drops slow consumers (`internal/ws/server.go`).

### Steps

1. Add per-session gap state, e.g. `final Map<String, bool> _seqGapSuspected = {}`.
2. In `_noteSeq` (or immediately after updating `_lastSeq`):

   ```dart
   final last = _lastSeq[id] ?? 0;
   if (ev.seq > 0 && last > 0 && ev.seq > last + 1) {
     _seqGapSuspected[id] = true;
   }
   if (ev.seq > last) _lastSeq[id] = ev.seq;
   ```

   Treat `seq == 0` as "unstamped" (do not gap-detect).

3. On connection transition to `connected` / resync trigger, set
   `_seqGapSuspected[id] = true` for sessions you care about **or** for all
   known session ids in the notifier (MADR: "set it for all sessions on a
   reconnect transition"). Prefer the reconnect path in the client listener
   that already calls resync/history.
4. In `resyncHistory`:

   ```dart
   final gap = _seqGapSuspected[sessionId] == true;
   if (!missedNewer && !missedOlder && !gap) return;
   // ... rebuild ...
   _seqGapSuspected[sessionId] = false; // on successful commit only
   ```

5. Clear gap flags in `clear` / session dispose paths alongside `_lastSeq`.
6. Update the block comment on `resyncHistory` to describe gap detection.

### Tests (`history_replay_test.dart` group `seq-based resync`)

Existing tests at ~353 / ~373 cover non-racing variants only. Add:

1. **Interior gap:** apply seq 1–10 live; set `_lastSeq` to 21 via a live event
   with seq 21 (simulating post-reconnect live); call `resyncHistory` with
   events 1–21 including 11–20; assert 11–20 appear (rebuild happens because
   gap flag or because you set the flag when applying 21).
2. **No-op still works:** contiguous 1–10, resync 1–10, no rebuild side
   effects (existing test remains).
3. **Flag clears** after successful resync so a second identical resync is a
   no-op without a new gap.

### Acceptance

- Missed mid-window events during disconnect are never permanently invisible.

### Rollback

Remove gap flag; restore pure bounds gate.

---

## Phase A8 — H5: session_title end-to-end

**Closes:** H5  
**Files:**
- App: `models.dart`, `sessions_screen.dart`, `chat_screen.dart` (app bar),
  `notification_coordinator.dart` labels optional
- Daemon: `internal/session/manager.go` pump
- Tests: `session_meta_test.dart` / new event parse test; `internal/session`
  manager test; `acphttp` already emits titles

### App steps

1. **`SessionEvent`**: add `final String? title;` to the class, constructor,
   `withText`, and `fromJson` (`json['title'] as String?`).
2. **`sessions_screen.dart` `_onSessionEvent`**: on `ev.type == 'session_title'`
   with non-empty `ev.title`, update matching `SessionMeta` via
   `copyWith(name: ev.title)` and refresh
   `notificationCoordinator.sessionLabels[id]`.
3. **Chat app bar**: prefer live name from session list / local state over the
   constructor `sessionName` when a title event arrives. Minimal approach:
   - listen to client events in chat for `session_title` on this session id
     and `setState` a `_title` field; or
   - read updated name from a sessions provider if one exists.
   Constructor `sessionName` remains the initial value.
4. Reducer may continue to `default: break` for `session_title` (not a
   transcript item) unless you want a system line — **do not** add transcript
   noise by default.

### Daemon steps (belt and suspenders)

In `Manager.pump`, inside the `mine` block, after status/mode handling and
before `appendHistoryLocked`:

```go
if ev.Type == event.TypeSessionTitle {
    title := strings.TrimSpace(ev.Title)
    if title != "" && title != e.meta.Name {
        e.meta.Name = title
        meta := e.meta
        persistMeta = &meta
    }
}
```

Reuse existing `persist` / `persistNow` path after unlock (same as status).
Mirror the user `Rename` semantics for `Meta.Name` only — do **not** call
provider `RenameSession` (agent title is already authoritative from the
agent).

Empty title: ignore (do not clear a user rename).

### Tests

- Dart: `SessionEvent.fromJson` parses `title` on `session_title`.
- Dart: sessions list updates name on synthetic event (widget or notifier).
- Go: manager test with a fake session that emits `TypeSessionTitle`; assert
  `Get`/`List` meta name updates and persist is scheduled (follow
  `TestRenameIsOwnerAuthorized…` patterns in `manager_history_test.go`).

### Acceptance

- Agent-set titles show in the session list and chat chrome without a manual
  rename; `session.list` after reconnect still carries the name (daemon
  persist).

### Rollback

Revert parse + UI branch + pump branch independently.

---

## Phase A9 — H6: question notifications

**Closes:** H6 (feeds A11 / L-n2)  
**Files:**
- `agent_notifications.dart` (`NotifKind`, payload, `notificationIdFor`)
- `notification_service.dart`
- `notification_coordinator.dart`
- `apps/mobile/test/notifications_test.dart`

### Steps

1. Extend `NotifKind` with `question` (or reuse a generic open-only kind).
2. Extend `NotifPayload` to carry `questionId` (parallel to `permissionId`).
3. `notificationIdFor`: stable id from `sessionId + questionId` (distinct
   channel from permissions so both can coexist).
4. `NotificationService.showQuestion({sessionId, questionId, sessionLabel,
   detail?})`:
   - open-only notification (no Allow/Deny actions — questions need the form
     UI);
   - tap → `NotifAction.open` → existing `onOpenSession`.
5. `cancelQuestion(sessionId, questionId)` parallel to `cancelPermission`.
6. Coordinator `_onEvent`:
   - early cancel on `question_resolved` (like `permission_resolved`);
   - `case 'question_request':` → `showQuestion` when `shouldNotify` passes
     (already whitelisted).
7. Ensure Dart 3 switch exhaustiveness / fall-through: use separate cases with
   breaks/returns as the existing permission case does (note: current code
   uses switch without break between cases relying on no fall-through in Dart
   3 — keep the same style).

### Tests

- `shouldNotify` already allows `question_request` — add explicit expect.
- Payload round-trip with `questionId`.
- Coordinator unit test with fake service: `question_request` → show once;
  `question_resolved` → cancel.

### Acceptance

- Backgrounded user gets a tap-to-open notification when the agent asks a
  question; resolution clears it.

### Rollback

Remove kind + cases.

---

## Phase A10 — H7: FGS lifecycle under reconnect

**Closes:** H7 (+ sets up L-n3)  
**Files:**
- `notification_coordinator.dart` `_onConn`
- `foreground_service.dart` (serialize start/stop)

### Steps

1. **Policy change in `_onConn`:**
   - Start service when `enabled && (connected || reconnecting)` (unchanged).
   - **Stop** only on `disconnected` (manual logout / deliberate tear-down),
     **not** on bare `error` while auto-reconnect remains possible.
   - Practical rule matching the MADR:

     ```dart
     if (!enabled) { stop; return; }
     if (state == connected || state == reconnecting) start;
     else if (state == disconnected) stop;
     else if (state == error) {
       // Keep service up if the client will retry; stop only when parked
       // permanent. Expose a getter on the client if needed, e.g.
       // client.autoReconnectEnabled / hasCredentials.
       if (!client.willAutoReconnect) stop;
     }
     ```

   Add a narrow public getter on `McremoteClient` if `_autoReconnect` is
   private (preferred over duplicating heuristics).

2. **Serialize start/stop** in `ForegroundServiceController`:
   - Chain operations on a single `Future _chain = Future.value();`
   - `start`/`stop` become `_chain = _chain.then((_) => _startBody())` with
     error swallowing per existing best-effort policy.
   - Guarantees a stop cannot overtake a start when both are `unawaited` from
     consecutive state events.

3. Optional in this phase (else L-n3): `updateService` text for reconnecting
   vs connected (`onlyAlertOnce: true`).

### Tests

- Unit: simulate `error` then `reconnecting` with auto-reconnect on → service
  mock must not end stopped.
- Unit: `disconnected` → stop.
- Unit: ordered start/stop serialization (fake delays).

### Acceptance

- Background reconnect no longer kills the keep-alive service on the
  error→reconnecting edge; API 31+ restart ban avoided.

### Rollback

Restore stop-on-error.

---

## Phase A11 — H8: reconcile pending asks after reconnect

**Closes:** H8, L-n2  
**Depends on:** A7 (history/resync trustworthy), A9 (question notif API)  
**Files:**
- `notification_coordinator.dart`
- `transcripts_notifier.dart` / chat already holds `pendingPermissions` /
  `pendingQuestions` after history apply
- possibly `mcremote_client.dart` for a post-connect hook

### Steps

1. After each transition to `McConnectionState.connected`, run
   `_reconcilePendingAsks()`:
   - For each live session id (from last `listSessions` or transcript map
     keys), ensure history/resync has been applied (existing chat/sessions
     refresh paths) **or** fetch a short history tail.
   - Read pending permission/question maps from
     `TranscriptsNotifier` / `sessionTranscriptProvider` state if accessible
     to the coordinator; if the coordinator must stay free of Riverpod,
     expose a callback/`PendingAskSource` interface registered by the app
     layer (cleaner).
2. For each pending permission not already notified, `showPermission`.
3. For each pending question not already notified, `showQuestion`.
4. Cancel notifications whose ids are **no longer** pending (stale Allow/Deny
   after another device answered during the outage) — closes L-n2.
5. Dedup: track "currently notified" ids so reconcile is idempotent.
6. **Protocol follow-up (document only in this phase):** open a short note in
   the MADR or protocol-v1 "Future" section: daemon should re-emit unresolved
   permission/question control events on attach so clients need not scrape
   history. Do not implement daemon re-emit unless trivial.

### Tests

- Seed transcript pending maps; fire `connected`; assert show* called.
- Clear pending; reconcile; assert cancel* called for stale notifs.
- Watching foreground session still suppresses per `shouldNotify`.

### Acceptance

- Asks raised during a background outage surface as notifications after
  reconnect without user foregrounding the app first.

### Rollback

Remove reconcile hook; live-only behaviour returns.

---

## Phase B1 — T1 + T2: epoch guards on pair and relay assign

**Closes:** T1, T2  
**Files:** `mcremote_client.dart`

### T1 — `claimPairCode`

1. After every await (`_openSocket`, `request('pair.claim')`, pin persist),
   if `_staleAttempt(epoch)`: tear down any socket you still own, **do not**
   call `_failHandshake` in a way that sets `_autoReconnect=false` on the
   newer attempt, **do not** write `_lastToken` / `_paired` / `connected`.
2. Pattern to mirror: `_connectInternal` guards at ~875/905/918/927.
3. On stale after `pair.claim` success payload: discard token; close channel;
   throw `pairing superseded` (already used earlier in the function).

### T2 — relay transport assignment

1. Change `_openSocketViaRelay` to **return** `RelayTransport` (or a record
   with channel/http/transport) **without** assigning `_relayTransport`.
2. Caller assigns only after `_staleAttempt(epoch)` passes.
3. On stale path: `await transport.close()`.
4. Ensure error paths still close the local transport (today
   `_closeRelayTransport` assumes field assignment — adjust).

### Tests

- Hard without real sockets: extract epoch-guarded apply function or use a
  fake channel. At minimum, unit-test that a superseded epoch does not flip
  `_autoReconnect` (visibleForTesting getter).
- Relay: two interleaved opens leave only one live transport (mock
  `RelayTransport.open` if needed).

### Acceptance

- Stale pair attempt cannot disable the newer connection's auto-reconnect or
  overwrite its token.
- Interleaved relay connects cannot orphan or clobber the live transport.

---

## Phase B2 — P1: staged images on deferred/chunked apply

**Closes:** P1  
**Files:** `transcripts_notifier.dart`, `staged_images_test.dart`

### Steps

1. Call `_zipStagedImages` from `_drainDeferred` / `_applyChunked` paths when
   applying a `user_message`, not only `_applyLive` (~430).
2. When a `user_message` is skipped by the seq guard, still pop the matching
   FIFO head if it was staged for that prompt (avoid sticky wrong image on the
   next bubble).
3. On resync rebuild, do not leave thumbnails pointing at wrong items —
   re-zip or clear staged queue for that session when rebuilding from
   authority (prefer clear-on-rebuild if echo attachments are in history).

### Tests

- Stage images; apply user_message via deferred path; assert bubble has bytes.
- Stage; skip duplicate seq; next user_message does not get previous images.

### Acceptance

- No wrong-image-on-next-bubble after resync/chunked apply.

---

## Phase B3 — P2 + P4: streaming markdown fidelity

**Closes:** P2, P4  
**Files:** `chat_bubble.dart`, `markdown_parser.dart`,
`streaming_markdown_test.dart`, parser tests

### P2

1. In `_updateStreamingRender`, parse
   `bufferStreamingMarkdown(text)` while keeping `_parsedText` keyed on **raw**
   `text` for staleness.
2. Preserve the module contract: raw `**` / backticks never flash in
   steady-state streaming for replies under the markdown branch cap.

### P4

1. In `_extractSpans` (or block builder), recurse nested `ul`/`ol` as indented
   blocks; carry depth into `ParsedBlock.level` (or equivalent).
2. Insert a break span when descending into a nested list so
   `_mergeAdjacent` cannot fuse `item1` + `nested` into `item1nested`.
3. Re-run the reproduction: `parseMarkdownOffMain('- item1\n  - nested')`
   must not produce a single fused span `"item1nested"`.

### Tests

- Streaming: unclosed `**bold` buffers; finalized equals non-streaming parse
  for common fixtures.
- Nested list unit test (executed, not only read).

### Acceptance

- No literal marker flash in steady state; nested lists readable mid-stream.

---

## Phase B4 — P3: keystore transient errors

**Closes:** P3  
**Files:** `settings_store.dart`, `settings_store_test.dart`

### Steps

1. Replace permanent `_secureDisabled = true` latch with:
   - short cooldown / retry count (e.g. 3 attempts, 500 ms–2 s backoff), or
   - time-boxed disable that auto-clears.
2. Classify errors where possible: permanent corruption vs transient
   (Android Keystore unlock race). Only permanent paths may force a user-
   visible "secure storage unavailable" state.
3. Do not catch bare `Error`.
4. Re-evaluate `AndroidOptions(resetOnError: true)` interaction — a transient
   decrypt must not wipe the only copy of the token without a second chance.

### Tests

- Mock failing secure storage once then succeeding → reads recover.
- Repeated permanent failure still fails closed.

### Acceptance

- One Keystore hiccup does not present the app as unpaired for the process
  lifetime.

---

## Phase B5 — C1–C4: chat UI correctness

**Closes:** C1, C2, C3, C4  
**Files:** `chat_screen.dart` (+ tests as available)

### C1 — external dismiss vs "Allow always?" dialog

1. Capture the sheet's `ModalRoute` in the permission sheet builder.
2. `_dismissSheet` should `navigator.removeRoute(sheetRoute)` (or pop until
   that route) instead of blind `Navigator.pop(ctx, '__external__')` when a
   dialog may be above it.
3. Alternatively: if confirm dialog open, pop it with `false` first, then pop
   the sheet with `__external__`.
4. Test: open permission sheet + confirm dialog; fire external resolution;
   neither route stuck; no TypeError.

### C2 — config sheet revert

1. Guard `setSheet(() => opts[i] = prev)` with `sheetCtx.mounted` (same as the
   notification).

### C3 — `/model` reentrancy

1. Set `_sending` or a dedicated `_interceptingModel` flag around
   `_maybeInterceptModelCommand` before the first await; clear in `finally`.
2. Disable send button while intercepting (existing `_sending` UI if reused).

### C4 — busy queue + images

1. In the busy branch (~780), queue text **and** attachments together
   (`_QueuedPrompt` gains attachment fields / staged bytes), **or** refuse
   with a SnackBar/top notification if product prefers no queueing of images.
2. MADR preferred: queue together and clear pending images when queued.
3. If `text.isEmpty && attachments.isEmpty` return early **before** queue
   (already true at top); if `text.isEmpty && attachments.isNotEmpty` while
   busy, either queue attachments-only or refuse — do **not** queue
   `_QueuedPrompt('')` alone while leaving thumbnails for the next send.
4. Flush path must pass attachments into `_sendText`.

### Tests

- C3: double-tap `/model` → single picker / single intercept.
- C4: busy + staged images → queue retains them; empty IME action does not
  enqueue blank prompt.

---

## Phase B6 — S2 + S3: connect-screen safety

**Closes:** S2, S3  
**Files:** `connect_screen.dart`, `settings_screen.dart` (dialog reuse)

### S2 — relay writes only on success

1. `_applyPair` currently writes `setRelayRoute` + `store.setRelayUrl/HostId`
   before claim/connect succeeds, and `setRelayUrl(null)` can wipe a good
   route.
2. Move persistence of relay fields to the same success paths as
   `setHost`/`setToken`.
3. Keep in-memory `client.setRelayRoute` for the **current attempt** only, or
   also defer it; if claim fails, restore previous in-memory route from store.
4. Pending fields: `_pendingRelay` / `_pendingHostId` parallel to fingerprint.

### S3 — confirm clear credentials

1. Reuse Settings' `AlertDialog` ("Clear saved credentials?") before
   `clearAll()` on the connect overflow menu.
2. Identical copy and destructive emphasis.

### Tests

- Failed claim after QR with empty relay does not clear stored relay of prior
  pairing (settings store mock).
- Clear credentials shows dialog; cancel leaves store intact.

---

## Phase B7 — N1: parked error still retries in background

**Closes:** N1  
**Files:** `mcremote_client.dart`, `notification_coordinator.dart` (optional
user-visible notice)

### Steps

1. When handshake failures hit the park threshold (~6) and state is `error`
   with alerts enabled / paired:
   - schedule a **slow** retry timer (e.g. 5 minutes) that calls `reconnect()`
     while credentials exist; reset on success.
2. Keep FGS up per A10 so the timer process is less killable.
3. Optional: one-shot local notification "Connection lost — alerts paused"
   when entering park (and cancel on reconnect).

### Tests

- After N failures, a timer is armed; on fire, reconnect attempted.
- Manual disconnect cancels the slow timer.

### Acceptance

- Daemon downtime >1 minute does not permanently kill background alerts until
  next process start.

---

## Phase B8 — W1–W3: protocol UI parity

**Closes:** W1, W2, W3  
**Files:** `models.dart`, `chat_screen.dart`, reducer/transcript as needed

### W1 — `timed_out`

1. Parse `timed_out` on `SessionEvent` (`json['timed_out'] == true`).
2. Where permission/question external resolution copy says "resolved
   elsewhere", branch: if `timed_out` → "Request timed out — the agent moved
   on".
3. Append a system transcript line on timeout dismiss (MADR).

### W2 — audio attach (larger; may split)

1. Gate a mic / file-audio affordance on `capabilities.audio`.
2. Stage `PromptAttachment(kind: 'audio', …)` under **A1 size budget**.
3. Reuse recording plugin already in tree if present; otherwise file-picker
   audio as MVP.
4. If scope balloons, ship file-picker MVP in B8 and track full recording UX
   as follow-on — but do not leave the capability dead with zero affordance.

### W3 — unavailable commands

1. Stop filtering `if (c.available)` out of autocomplete exclusively.
2. Show unavailable rows disabled with `reason` as subtitle.
3. Still allow documented "send anyway" behaviour if product requires; default
   is explain-don't-hide per protocol-v1 / MADR 0023.

### Tests

- W1 parse + copy branch unit test.
- W3 autocomplete lists disabled row with reason.
- W2: capability false hides control; true shows; oversize rejected by A1
  helper.

---

## Phase C — Low findings (complete table)

Land in three waves. Each wave is one commit unless noted.

### C0 — already done in Phase 0

| ID | Notes |
|---|---|
| L-tool | Phase 0 |

### C1 — error typing and connection hygiene

| ID | File | Work |
|---|---|---|
| L-t1 | `mcremote_client.dart` | Null `_ensureIdentity` cached future on error before rethrow |
| L-t2 | `mcremote_client.dart` | Missed-ping teardown: capture epoch; bail unless still connected + current |
| L-t3 | `mcremote_client.dart` | `channel.ready` timeout paths: `unawaited(channel.sink.close()…)` |
| L-t4 | `mcremote_client.dart` | `sessionHistory` catch narrow to transport exceptions; sentinel for fail vs empty |
| L-t5 | `mcremote_client.dart` | `_failAllPending` → `McException(..., code: 'connection_lost')` |
| L-t6 | `mcremote_client.dart` | Scope relay hints per authority; clear in `_noteHost` on change |
| L-t7 | `relay_transport.dart` | Pass through `Uint8List` without `List<int>.from` |
| P3 residual | if not fully closed in B4 | classification only |

### C2 — transcript / provider polish

| ID | File | Work |
|---|---|---|
| L-p1 | `transcripts_notifier.dart` | `TranscriptCache.retainOnly(liveIds)` on `syncFromMeta` |
| L-p2 | `transcripts_notifier.dart` | Snapshot `t.items.isNotEmpty` once per replay batch |
| L-p3 | `transcripts_notifier.dart` | catch+`debugPrint` on cache save; consider `compute` for large tails |
| L-p4 | `app_providers.dart` | Theme load only if still system sentinel; try/catch |
| L-p5 | `transcripts_notifier.dart` | `sessionTranscriptProvider` → `autoDispose.family` (check listeners) |

### C3 — UI / settings / notifications / docs

| ID | File | Work |
|---|---|---|
| L-c1 | `chat_bubble.dart` | `textScaler: MediaQuery.textScalerOf(context)` on streaming `RichText` |
| L-c2 | `chat_screen.dart` | `context.mounted` before `setMode` after await |
| L-c3 | `chat_screen.dart` | `_scrollToEnd(force:)` skips `_listScrolling` when forced |
| L-c4 | `scroll_activity.dart` | ignore nested scrollables (`depth != 0`) |
| L-c5 | `top_notification.dart` | `Dismissible`; longer duration when action present |
| L-s1 | `sessions_screen.dart` | Spinner only when `_sessions.isEmpty` |
| L-s2 | `sessions_screen.dart` | End-session error restore only removed row or `_refresh()` |
| L-s3 | `settings_screen.dart` | try/catch prefs reads in `_load` |
| L-s4 | `sessions_screen.dart` | `friendlyOpError` for banners |
| L-s5 | `sessions_screen.dart` | Rename Save disabled on empty (or clear with feedback) |
| L-s6 | `settings_store.dart` | `clearAll` removes host-scoped cwd/model keys |
| L-n1 | `notification_coordinator.dart` | Subscribe to streams before `await init` |
| L-n2 | folded into A11 | — |
| L-n3 | `foreground_service.dart` | State-appropriate service text |
| L-n4 | `foreground_service.dart` | Escalate background backoff; re-evaluate wifi lock |
| L-w1 | `chat_screen.dart` | Usage chip with count when `size <= 0 && used > 0` |
| L-w2 | `docs/protocol-v1.md` | `session_config` merge semantics, not full replacement |
| L-w3 | `models.dart` / sessions | Latch `agent_session_id` into cached meta or document reliance |
| L-w4 | `docs/protocol-v1.md` | Add `switch_mode` to tool_kind vocabulary |

### C-wave tests

- Prefer extending existing tests (`settings_store_test`, `top_notification_test`,
  `streaming_markdown_test`, `mc_exception_test`).
- Doc-only items (L-w2, L-w4): no code test; ensure
  `TestEventTypesAreDocumented` / protocol drift tests still pass if any.

---

## Phase V — Verification matrix

Run before declaring the audit closed.

### Mobile

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

Targeted clusters after each phase group:

| After | Tests |
|---|---|
| 0 | `sessions_screen_test`, `model_provider_step_test` |
| A1 | staged images / chat send helpers |
| A2 | `pair_uri_test`, `connect_screen_test` |
| A3 | router/widget tests for ChatScreen key + pending nav |
| A4 | `permission_loop_test` (+ question variant) |
| A5 | `cert_pinning_test` |
| A6 | `relay_transport_test`, `relay_path_test` |
| A7 | `history_replay_test` |
| A8 | model parse + session manager Go tests |
| A9–A11 | `notifications_test` |
| B3 | `streaming_markdown_test` |
| full | `make preflight` (if available at repo root) |

### Daemon (when Go changed)

```bash
make pre-add-check   # or scripts/go-precheck.sh on touched packages
go test ./internal/session/ ./internal/event/ ./internal/ws/
go test -race ./internal/session/
```

### Manual smoke (device or emulator)

1. Pair plaintext daemon (H3) and TLS daemon (regression).
2. Switch hosts A→B with pins (H1).
3. Send large photo — rejected cleanly (H4).
4. Kill network mid-turn; restore — no permanent transcript hole (H2); pending
   permission notifies (H8).
5. Navigate session A→B quickly with a queued prompt (H10).
6. Fail a question RPC — no modal loop (H9).
7. Background app; trigger question — notification (H6); toggle airplane mode
   — FGS survives reconnect (H7).

---

## 2. Finding → phase index

| ID | Sev | Phase |
|---|---|---|
| L-tool | Low | 0 |
| H4 | High | A1 |
| H3 | High | A2 |
| H10 | High | A3 |
| S1 | Med | A3 |
| H9 | High | A4 |
| H1 | High | A5 |
| T3 | Med | A5 |
| T4 | Med | A6 |
| H2 | High | A7 |
| H5 | High | A8 |
| H6 | High | A9 |
| H7 | High | A10 |
| H8 | High | A11 |
| L-n2 | Low | A11 |
| T1 | Med | B1 |
| T2 | Med | B1 |
| P1 | Med | B2 |
| P2 | Med | B3 |
| P4 | Med | B3 |
| P3 | Med | B4 |
| C1–C4 | Med | B5 |
| S2–S3 | Med | B6 |
| N1 | Med | B7 |
| W1–W3 | Med | B8 |
| remaining L-* | Low | C |

All ten highs appear in Phase A (plus Phase 0 hygiene). No verified finding is
left without a phase.

---

## 3. Risk notes

1. **H1 pin key design** is the highest-risk data migration: bad pin writes
   can brick reconnect. Write tests first; exercise A→B→A host switching.
2. **H10 `ValueKey`** is one line but will reset scroll/composer when the key
   changes — desired. Confirm go_router version still keys pages by pattern
   (17.3.0 noted in MADR).
3. **A11 reconcile** can spam notifications if history re-lists resolved asks
   — always intersect with pending maps from the reducer, not raw history
   alone.
4. **W2 audio** may pull in permissions (mic) and Play policy — keep MVP
   minimal.
5. **L-p5 autoDispose** can surprise if something assumes eternal transcript
   providers — grep listeners before flipping.
6. **Daemon H5 persist** should not call provider Rename (side effects /
   ACLs); only `Meta.Name`.

---

## 4. Out-of-scope follow-ups (record, do not implement here)

- Daemon re-emit of unresolved permission/question on WebSocket attach (true
  fix for H8 without history scraping).
- Negotiated larger WS frames / chunked prompt upload for big attachments.
- Composite pin keys multi-daemon simultaneous identity (beyond A5 minimum).
- Full in-chat audio recording UX polish beyond W2 MVP.

---

## 5. Implementation checklist (copy into PR / commit series)

- [ ] Phase 0 — L-tool NUL
- [ ] Phase A1 — H4 size budget + protocol note
- [ ] Phase A2 — H3 scheme
- [ ] Phase A3 — H10 key + S1 stash
- [ ] Phase A4 — H9 question loop
- [ ] Phase A5 — H1 pin + T3 token
- [ ] Phase A6 — T4 single peer
- [ ] Phase A7 — H2 seq gap
- [ ] Phase A8 — H5 title app+daemon
- [ ] Phase A9 — H6 question notifs
- [ ] Phase A10 — H7 FGS
- [ ] Phase A11 — H8 reconcile
- [ ] Phase B1 — T1/T2 epoch
- [ ] Phase B2 — P1 images
- [ ] Phase B3 — P2/P4 markdown
- [ ] Phase B4 — P3 keystore
- [ ] Phase B5 — C1–C4
- [ ] Phase B6 — S2/S3
- [ ] Phase B7 — N1
- [ ] Phase B8 — W1–W3
- [ ] Phase C — lows C1–C3 waves
- [ ] Phase V — full verification

---

## 6. References

- [MADR 0045 — findings](./0045-MADR-mobile-app-hardening-audit.md)
- [protocol-v1.md](./protocol-v1.md) — frame limits, `session_title`, `timed_out`
- [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md) — resync precedent
- [MADR 0015](./0015-MADR-mcrelay-transport-security.md) — relay threat model
- Commit `00d15b7` — permission modal loop fix (mirror for H9)
