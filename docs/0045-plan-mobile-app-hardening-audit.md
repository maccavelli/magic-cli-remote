# MADR 0045 — Implementation plan: mobile app hardening audit

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0045](./0045-MADR-mobile-app-hardening-audit.md). Read that
first: it carries the verified findings, decisions, and severity rationale.
This document is the build order — thorough, phase-sequenced, and keyed to
current source locations as of 2026-07-28 (`ff4b2c1`).

- **Status**: Revalidated and implementation-ready
- **Date**: 2026-07-28
- **Scope**: `apps/mobile` (41 production Dart files, 33 tests,
  Flutter/Android/Linux scaffolding) + daemon work for H5 title persistence and
  H8 authoritative pending-ask reconciliation + protocol docs
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

Close every verified remediation finding in MADR 0045 (H1–H10, noting H3 is
medium after revalidation; T1–T4; P1–P4; C1–C4; S1–S3; N1; W1; and the
non-refuted low table) with independent, testable changes. W2/W3 are recorded
product follow-ups and are not closure criteria.

### Non-goals

- R8 / resource shrinking, ProGuard keep rules, or release-pipeline work.
- Replacing the WS transport, rewriting Riverpod graph, or redesigning chat
  layout.
- New agent providers, audio recording/attachment UX, or a disabled-command
  browser.
- New event types. H8 adds a request/response RPC carrying existing
  permission/question event shapes, not a broadcast event type.
- Live-tagged provider tests unless a phase explicitly needs them (H5 daemon
  unit tests suffice).

### Ground rules

1. **One phase → one commit** is a suggested review shape, not a correctness
   requirement. Cross-layer phases (A8/A11) may use a tightly related pair. Do
   not push unless the user asks.
2. **Go gate before `git add`**: `make pre-add-check` / `scripts/go-precheck.sh`
   for any touched `.go` files (`gofmt`, `golint`, `govulncheck`).
3. **Dart gate**: `dart format` on every touched `.dart` file; `flutter analyze`
   and `flutter test` for the packages you touch; `make preflight` before calling
   a multi-phase block done.
4. **Commit without `-m`**: the prepare-commit-msg hook generates the message.
5. **Line numbers drift**. Prefer symbol names + surrounding comments from this
   plan over exact line numbers when applying patches.
6. **L-tool (NUL) is Phase 0**, not deferred polish: a source file grepped as
   binary has already produced false positives in this audit.
7. **No phase is complete at code compile alone.** Run its named regression
   tests, then the phase-local analyzer/test gate; mark the phase complete only
   after its acceptance assertions pass.

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
| H3: plaintext contract has two failures | daemon emits `mode=off`; app `TlsMode` cannot parse it; legacy `ws://` then loses its scheme in `_applyPair`; Android base policy forbids cleartext |
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
| P1: byte-only staged-image FIFO cannot safely be applied to history | `_sentImages` stores only bytes; `_applyLive` alone calls `_zipStagedImages`; history may contain older image messages |
| W1: `timed_out` is permission-only and the open sheet holds the request event | `event.Event.TimedOut`; `_showPermissionSheet` receives the original request, so parse-only cannot select dismissal copy |
| W2/W3 are not defects | audio is provider-specific and needs new UX/dependencies; protocol-v1 permits clients to omit unavailable commands |

---

## 1. Dependency graph

```text
Phase 0  L-tool (NUL) ──────────────────────────────────────────────┐
                                                                     │
Phase A1 H4 size budget ─────────────────────────────────────────────┤
Phase A2 H3 mode/platform policy ────────────────────────────────────┤
Phase A3 H10 key + S1 deep-link stash ───────────────────────────────┤
Phase A4 H9 question modal loop ─────────────────────────────────────┤
Phase A5 H1 pin identity + T3 token wipe ──┐                         │
Phase A6 T4 single-peer bridge ────────────┼─ security cluster       │
                                           │                         │
Phase A7 H2 seq-gap resync ─────────────────┤                         │
Phase A8 H5 session_title ──────────────────┤                         │
Phase A9 H6 question notifs ──→ A11 H8 ask reconcile                 │
Phase A10 H7 FGS lifecycle ───→ B7 N1 maintenance retry              │
                                           │                         │
Phase B1 T1/T2 epoch completeness ─────────┘                         │
Phase B2 P1 staged images (after A7 preferred)                       │
Phase B3 P2 + P4 streaming markdown                                  │
Phase B4 P3 keystore latch                                           │
Phase B5 C1–C4 chat UI                                               │
Phase B6 S2/S3 connect safety                                        │
Phase B7 N1 background park retry                                    │
Phase B8 W1 timeout resolution                                       │
                                                                     │
Phase C  remaining lows (error typing, polish, protocol doc L-w*) ───┘
                                                                     │
Phase V  full verification (analyze, test, race, preflight) ─────────┘
```

**Parallelism (safe once Phase 0 is in):**

- A1, A2, A3, A4 are independent of each other.
- A5 and A6 are independent; both touch connection security.
- A7 is independent of A5/A6/A11; do it before B2.
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

1. Make the local `catalogFor` helper generic in its key type:
   `Future<PickerCatalog> catalogFor<K>(Map<K, PickerCatalog> cache, K key, …)`.
2. Change `modelCatalogs` to
   `<(String provider, String modelProvider), PickerCatalog>{}` and use
   `(provider: p, modelProvider: modelProvider)` as the key. Keep
   `providerCatalogs` string-keyed.
3. Do not substitute another delimiter: provider/model ids are externally
   supplied and no current validation proves that `|` (or any printable
   delimiter) is impossible.
4. Confirm with:

   ```bash
   # must print 0
   python3 -c "print(open('apps/mobile/lib/features/sessions/sessions_screen.dart','rb').read().count(b'\\x00'))"
   # must no longer say "binary file matches"
   rg -n "session\.create|sessionLabels" apps/mobile/lib/features/sessions/sessions_screen.dart | head
   ```

### Tests

- Existing `sessions_screen_test.dart` / `model_provider_step_test.dart` must
  still pass.
- Add/extend the cache-key test with values that would collide under delimiter
  concatenation (for example `('a|b', 'c')` vs `('a', 'b|c')`) and assert two
  fetches/two cache entries.

### Acceptance

- File is text to ripgrep; zero NUL bytes; create-session / model cache still
  works.

### Rollback

Local cache-key type only; no persisted data migration.

---

## Phase A1 — H4: encoded attachment budget (stop socket kills)

**Closes:** H4

**Files:**

- `apps/mobile/lib/features/chat/chat_screen.dart` (pick path ~538)
- `apps/mobile/lib/data/ws/mcremote_client.dart`
- new `apps/mobile/lib/data/protocol/frame_budget.dart`
- `docs/protocol-v1.md`
- mobile request/prompt and chat tests

### Problem recap

Composer accepts **4 MiB raw** images, then `base64Encode`s (~+33%) into one
`session.prompt` frame (`mcremote_client.dart` `prompt` → `request` →
`ch.sink.add`). Daemon `conn.SetReadLimit(1 << 20)` closes with
`StatusMessageTooBig`. A normal multi-megapixel photo kills the socket and
loses the prompt. Text-only prompts are also currently unbounded.

### Steps

#### A1.1 Authoritative transport gate

1. Define `kMaxClientFrameBytes = 1 << 20` in `frame_budget.dart`, explicitly
   mirroring `internal/ws/server.go:maxWSMessageBytes`.
2. Add `encodeRequestEnvelope({required id, required type, payload})` and
   `encodedRequestBytes(...)` in that file. In `request`, construct the actual
   envelope (including the real 36-character request id), call the encoder
   once, then measure `utf8.encode(encoded).length`.
   If it exceeds the limit:
   - remove the newly inserted completer from `_pending`;
   - do not call `sink.add`;
   - throw `McException('Request is too large …',
     code: 'payload_too_large', permanent: false)`.
   Send the already encoded string when it fits; do not encode twice.
3. Keep `payload_too_large` a **client-local** code. Do not register it as a
   daemon error code: `coder/websocket` rejects an over-limit frame in
   `conn.Read` before `handleMessage` has a request id and therefore cannot
   return a correlated error.
4. Unit-test exact UTF-8 bytes, including non-ASCII text and JSON-escaped
   characters; assert an oversize request never reaches a fake sink and leaves
   no pending completer.

#### A1.2 Composer preflight and preservation

1. Add `sessionPromptFrameBytes` to `frame_budget.dart`. It builds the same
   payload as `McremoteClient.prompt` and calls `encodedRequestBytes` with a
   fixed 36-character id. Both paths therefore share the envelope encoder; its
   result must equal the actual serialized frame for an equivalent prompt.
2. At image-pick time, preflight the candidate together with every already
   staged attachment and the current text. This is cumulative: two individually
   acceptable images may exceed the frame together.
3. At send time, preflight again because the user may type more text after
   staging. Perform this check **before** clearing the composer, staging local
   thumbnail correlation, or clearing `_pendingImages`.
4. Replace the `4 * 1024 * 1024` message with a measured message such as
   "This prompt is 1.24 MB; the connection limit is 1 MB. Remove an attachment
   or shorten the message." Do not advertise a single raw-image limit: MIME,
   JSON escaping, text, and other attachments affect the remaining budget.
5. If the transport gate still fires (defense in depth), restore both text and
   attachment state; no prompt content may disappear.

#### A1.3 Protocol documentation

In `docs/protocol-v1.md`:

- state that each client→daemon WebSocket message is limited to **1 MiB**;
- state attachments are base64 within the JSON frame and count after encoding;
- state that exceeding the transport read limit closes the socket before an
  application error can be returned;
- require conforming clients to preflight exact serialized UTF-8 bytes.

Keep the server limit unchanged; raising it needs a separate DoS/memory review.

### Tests

- Unit: exact frame at/below the limit is sent; one byte over is rejected
  locally; multibyte text is counted in bytes, not Dart code units.
- Widget/unit: cumulative picker and send-time paths surface the friendly
  message, preserve composer/attachments, and do not call `prompt`.
- Extend any existing staged-image test (`staged_images_test.dart`) if it
  assumes 4 MiB.

### Acceptance

- No mobile request—not only image prompts—can write a frame above the daemon
  read limit; a ~2 MB photo is rejected without changing socket state or
  composer state; existing small images still send.
- Protocol doc mentions the 1 MiB frame budget.

### Rollback

Revert cap + doc; behaviour returns to today's breakage for large photos.

---

## Phase A2 — H3: represent plaintext honestly and enforce Android TLS policy

**Closes:** H3

**Files:** `pair_uri.dart`, `connect_screen.dart`, `apps/mobile/README.md`,
`docs/protocol-v1.md`,
`pair_uri_test.dart`, `connect_screen_test.dart`

### Problem recap

The actual daemon QR uses `host=ws://…&mode=off`. The app rejects it because
`TlsMode.off` does not exist. A legacy `ws://` QR without `mode` parses, but
`_applyPair` drops the scheme. Android then forbids cleartext app-wide by
design, despite the README claiming debug builds allow it.

### Steps

1. Add `off('off')` to `TlsMode`. Keep
   `TlsMode.fallback == TlsMode.selfsigned` for existing manual/stored TLS
   inputs, but make `PairPayload.tryParse` use the daemon's documented legacy
   inference when `mode` is absent:
   fingerprint present → `selfsigned`; fingerprint absent → `off`. An
   unrecognized non-empty mode still rejects.
2. Validate mode/transport combinations in `PairPayload.tryParse` after that
   inference:
   - `off` requires no fingerprint and must not carry an explicitly secure
     scheme. If the legacy host is bare, materialize `ws://` in
     `PairPayload.host` so modern secure-by-default parsing cannot reinterpret
     it;
   - `selfsigned` requires secure transport and a fingerprint;
   - `letsencrypt` requires secure transport and the fingerprint the daemon
     contract says is emitted for both TLS modes;
   - any contradictory combination rejects.
3. `_applyPair` sets the visible host from
   `SettingsStore.stripFingerprint(payload.host)`, preserving `ws://`/`wss://`.
   Continue carrying fingerprint/mode separately.
4. Before `_claimCode`, `_connect`, or health probing, if
   `Platform.isAndroid` and `parseEndpoint(host).secure == false`, stop with a
   targeted TLS-required status. Do not attempt a socket and do not mutate
   stored credentials/relay state.
5. Linux development retains explicit `ws://`; Android debug and release share
   the TLS-mandatory policy. Do **not** add a global cleartext exception.
6. Correct `apps/mobile/README.md` and `protocol-v1.md`:
   - remove the false "debug builds allow ws://" claim;
   - say Android rejects `mode=off` by policy;
   - say clients must preserve or explicitly reject transport intent, never
     reinterpret it.

### Tests

- Parser accepts a real daemon-shaped `mode=off` QR and rejects contradictory
  mode/scheme/fingerprint combinations.
- Legacy no-`mode`, no-fingerprint bare host is inferred as `off` per the
  daemon contract and materialized as `ws://`; legacy fingerprint-bearing
  input remains `selfsigned`.
- Connect-screen test on an Android target override: plaintext QR shows the
  TLS-required error; fake client records zero claim/connect/health calls and
  fake store records zero writes.
- Non-Android test preserves `ws://` through the exact connect input.
- Existing selfsigned/letsencrypt QR tests remain green.
- Regression: secure QR still dials `wss`.

### Acceptance

- A valid plaintext QR is represented faithfully. Android rejects it clearly
  before mutation; supported development platforms pass `ws://` unchanged.
  TLS QRs remain pairable.

### Rollback

One screen function.

---

## Phase A3 — H10 + S1: per-session ChatScreen identity and cold-start deep link

**Closes:** H10, S1

**Files:** `apps/mobile/lib/app.dart`, `app_providers.dart`, connect success
paths in `connect_screen.dart`, router/connect widget tests

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
2. Add a dedicated in-memory `PendingNavigationController` provider with:
   `remember(Uri)`, `String? take()`, and `clear()`. Do not put navigation
   state on `McremoteClient`, and do not persist it—the launch notification can
   be replayed again on a later process start, while a stale persisted route
   could surprise a later pairing.
3. Store only validated in-app locations matching
   `/sessions/<non-empty-id>`; preserve query parameters but reject schemes,
   authorities, and unrelated routes.
4. Route every successful ConnectScreen path through one helper:
   `final target = pending.take() ?? '/sessions'; context.go(target)`. This
   includes cold auto-connect, `_claimCode`, `_connect`, and the
   already-paired branch in `_load`; the current plan's two-path wording would
   still lose the target on auto-connect.
5. Clear the controller on explicit disconnect/credential clear and after
   consumption.
6. Composes with H10: after auth, `go('/sessions/$id')` builds a **fresh**
   `ChatScreen` for that id.

### Tests

- Widget: build two routes with different ids; assert State is not reused
  (e.g. distinct `Key`s, or a test-only `debug` counter on `initState`).
- Redirect: unpaired app with pending `/sessions/abc?name=Example` ends on that
  chat after each success path, including cold auto-connect.
- Sign-out clears pending so the next pair does not jump to a stale id.
- Invalid/external pending locations are not retained.

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
3. Keep the existing unified pending banner (`hasPending`, ~1930) as the retry
   path: its Review callback already clears both presented-id sets and calls
   `_maybeShowQuestion`. Do not add a second banner or another implicit retry.
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
- new `apps/mobile/test/mcremote_client_test.dart`

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
   - Compute requested authority and the authority of `await getHost()`.
     Resolve `effectiveId` as the explicit/current-process `deviceId`, or the
     persisted id only when those two authorities match.
   - An `effectiveId` may select `pins['id:$id']`; a persisted id is therefore
     never applied to a hand-edited/new authority before authentication.
   - An authority record may be returned only for that exact authority and
     compatible owner.

3. In `setFingerprint`:
   - Use the same `effectiveId` rule. Before authentication, a fresh QR
     fingerprint for a changed authority writes `host:<new-authority>` rather
     than falling back to the old saved device id.
   - After auth/pair returns the new device id, migrate that exact authority
     record to `id:<new-device-id>` and remove only the matching host record.
   - Never overwrite `id:A` with a record for B.

   Required lookup/write behavior:

   ```text
   resolve(A, explicit current id A)          -> id:A
   resolve(A, persisted id A + saved host A) -> id:A
   resolve(B, persisted id A + saved host A) -> host:B only; never id:A
   set(B, fresh QR, no current id)            -> host:B; id:A unchanged
   set(B, after auth returns id B)            -> id:B; remove host:B
   ```

4. Replace the existing "pin survives the host moving to a new tailnet IP"
   expectation. Before TLS/auth there is no trustworthy evidence that the new
   authority is the same daemon. Required tests:
   - A→B without a fresh QR rejects as unpinned and preserves A;
   - A address change without a QR rejects;
   - A address change with a fresh matching QR succeeds and rekeys after auth;
   - same-authority cold reconnect still retrieves `id:A`.

### T3 — Steps

1. Capture whether `claimPairCode` changes authority before `_noteHost`.
2. If it does, clear `_lastToken`, `_paired`, and the persisted token before
   opening B's socket. Keep the stored host/relay tuple at A until B succeeds.
   Credentials are host-scoped; no B failure or process restart may combine A's
   token with B's `_lastHostInput`.
3. On same-authority re-pair, keep the old token only until a new pair token is
   issued if product behavior requires fallback; make that branch explicit and
   tested. On changed authority, no fallback is permitted.
4. Coordinate with S2: relay route and stored host/token remain committed only
   after successful pairing.

### Tests

- Pin: all four authority/QR cases above, including persisted device id.
- Client: simulate failed pair to B after paired A; assert
  `hasCredentials == false`, `SettingsStore.getToken()` is null, stored host
  remains A, and no reconnect attempt sends A's token to B.

### Acceptance

- Warm host switch never dials B with A's pin; a moved/self-signed daemon
  requires fresh QR evidence and then succeeds.
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
2. Close the listening `ServerSocket` immediately after the first accepted peer
   is installed. Do not retain `_replacePeer` behavior for later accepts.
3. Keep outer WSS + inner TLS semantics unchanged.
4. Document in a short comment: co-resident apps can no longer splice the
   bridge; DoS-by-eviction is closed.

### Tests

- Open transport; connect one loopback peer; a second `Socket.connect` is
  refused, while the first peer continues to exchange bytes.
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

3. Add a second notifier-owned subscription to `client.connectionStates`.
   Track the prior state; on a transition from any non-connected state to
   `connected`, mark every id in `_lastSeq.keys` suspected. Cancel this
   subscription in `ref.onDispose`. This avoids relying on a mounted
   `ChatScreen` to mark the evidence; the existing chat reconnect listener
   remains responsible for fetching its session's history.
4. In `resyncHistory`:

   ```dart
   final gap = _seqGapSuspected[sessionId] == true;
   if (!missedNewer && !missedOlder && !gap) return;
   // ... rebuild ...
   _seqGapSuspected[sessionId] = false; // on successful commit only
   ```

5. Make `_applyChunked` report whether its owning generation reached the final
   commit. Clear the flag only on `true`; a superseded/failed apply must retain
   the repair signal.
6. Clear gap flags in `clear`, `clearAll`, and `syncFromMeta` eviction paths
   alongside `_lastSeq`.
7. Update the block comment on `resyncHistory` to describe gap detection and
   the daemon ring's bounded-authority trade-off.

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
4. **Superseded apply retains flag** and a later resync still rebuilds.
5. **Reconnect marker without observed jump:** apply contiguous 1–10, emit
   reconnect transition, fetch 1–10; assert one authoritative rebuild then flag
   clear.

### Acceptance

- Missed mid-window events during disconnect are never permanently invisible.

### Rollback

Remove gap flag; restore pure bounds gate.

---

## Phase A8 — H5: session_title end-to-end

**Closes:** H5

**Files:**

- App: `models.dart`, `sessions_screen.dart`, `chat_screen.dart` (app bar),
  `notification_coordinator.dart`
- Daemon: `internal/session/manager.go` pump
- Tests: `session_meta_test.dart`, `sessions_screen_test.dart`,
  `notifications_test.dart`, `internal/session/manager_durable_test.go`

### App steps

1. **`SessionEvent`**: add `final String? title;` to the class, constructor,
   `withText`, and `fromJson` (`json['title'] as String?`).
2. **`sessions_screen.dart` `_onSessionEvent`**: on `ev.type == 'session_title'`
   with non-empty `ev.title`, update matching `SessionMeta` via
   `copyWith(name: ev.title)` and refresh
   `notificationCoordinator.sessionLabels[id]`.
3. **Chat app bar**: initialize `_title` from `widget.sessionName`, add a
   `StreamSubscription<SessionEvent> _titleSub` in `initState`, and update
   `_title` on a non-empty `session_title` for `widget.sessionId`. Cancel it in
   `dispose`. H10's `ValueKey` guarantees the subscription cannot carry across
   ids. The app bar reads `_title`, with the current id fallback.
4. Reducer may continue to `default: break` for `session_title` (not a
   transcript item) unless you want a system line — **do not** add transcript
   noise by default.
5. The notification coordinator also consumes `session_title` directly and
   updates `sessionLabels`, because the sessions screen may not be mounted when
   a background title arrives. This label update runs even when notifications
   are disabled; it is state maintenance, not notification dispatch.

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
  `Get`/`List` meta name updates, `FlushPersist` writes the new name, and a new
  Manager loading the store returns it. Also assert an empty title does not
  erase a user name.
- Dart: coordinator receives a title while SessionsScreen is absent, then a
  turn-complete notification uses the new label.

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

1. Extend `NotifKind` with `question`.
2. Extend `NotifPayload` to carry `questionId` (parallel to `permissionId`).
3. Replace `notificationIdFor` with one target-key helper:
   `notificationIdFor(kind, sessionId, requestId?)`. Hash the UTF-8 bytes of
   `v2:<kind>:<sessionId>:<requestId-or-empty>` with a locally implemented
   stable FNV-1a 32-bit function, mask to a non-zero positive 31-bit id, and use
   it for permissions and turn-complete too. Do not use Dart `String.hashCode`;
   Android ids must be reproducible after process restart. This also closes
   L-n5: the current permission helper omits `sessionId`.
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
   - A11 will replace direct show/cancel bookkeeping with a unified known-ask
     reconciler; keep this phase's case shaped so that refactor is mechanical.
7. Ensure Dart 3 switch exhaustiveness / fall-through: use separate cases with
   breaks/returns as the existing permission case does (note: current code
   uses switch without break between cases relying on no fall-through in Dart
   3 — keep the same style).

### Tests

- `shouldNotify` already allows `question_request` — add explicit expect.
- Payload round-trip with `questionId`.
- Coordinator unit test with fake service: `question_request` → show once;
  `question_resolved` → cancel.
- Same request id in two sessions produces different notification ids; show
  and cancel compute exactly the same id for each target.

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
   - **Stop** only when notifications are disabled or on `disconnected`
     (manual logout / deliberate tear-down), not on `error`. A parked retryable
     error needs the service for B7, while a permanent error can update the
     service text but will eventually stop on sign-out.
   - Practical rule:

     ```dart
     if (!enabled) { stop; return; }
     if (state == connected || state == reconnecting) start;
     else if (state == disconnected) stop;
     else if (state == error) updateOrKeepRunning;
     ```

2. **Serialize start/stop** in `ForegroundServiceController`:
   - Chain operations on a single `Future _chain = Future.value();`
   - `start`/`stop`/`update` append bodies with
     `_chain = _chain.then((_) => body()).catchError(log)`.
   - Guarantees a stop cannot overtake a start when both are `unawaited` from
     consecutive state events.
3. Close L-n3 here: update service text for connected, reconnecting, and
   connection-unavailable states through a concrete `update({title, text})`
   method queued on the same chain, with `onlyAlertOnce: true`. `_onConn`
   uses “Connected to host”, “Reconnecting to host”, and
   “Connection unavailable — retrying periodically” respectively.

### Tests

- Unit: simulate `error` then `reconnecting` → service mock must not end
  stopped.
- Unit: `disconnected` → stop.
- Unit: ordered start/stop/update serialization with delayed fake bodies.
- Unit: disabling notifications from any state ends stopped after the queue
  drains.

### Acceptance

- Background reconnect no longer kills the keep-alive service on the
  error→reconnecting edge; API 31+ restart ban avoided.

### Rollback

Restore stop-on-error.

---

## Phase A11 — H8: reconcile pending asks after reconnect

**Closes:** H8, L-n2

**Depends on:** A9 (question notification API)

**Files:**

- `internal/protocol/messages.go`, `messages_test.go`, `doc_coverage_test.go`
- `internal/session/manager.go` + manager tests
- `internal/ws/server.go` + handler tests
- `docs/protocol-v1.md`
- `mcremote_client.dart`
- `notification_coordinator.dart`

### Steps

#### A11.1 Daemon authority

1. Add pending maps to each live manager entry:
   `map[permissionID]event.Event` and `map[questionID]event.Event`.
2. In `Manager.pump`, while holding `m.mu` and only for the owning session,
   call `appendHistoryLocked(&ev)` first so the copied request has its assigned
   sequence, then:
   - lazily initialize the relevant map and insert non-empty ids on
     `permission_request` / `question_request`;
   - remove on the matching resolved event;
   - clear both maps on `turn_complete` or `error`, matching
     `transcript_reducer.dart`, because an errored/completed turn cannot answer
     its outstanding asks.
   Store the original request event shape; do not synthesize fields later.
   Close and replacement already remove the entry from `m.sessions`, so its
   maps automatically leave the authoritative snapshot.
3. Add
   `func (m *Manager) PendingAsks(deviceID string) []event.Event`, filtering by
   `Meta.OwnerDeviceID` and sorting copies by session id then sequence (with
   request id as a final tie-break). Copy under lock; never expose internal map
   references.
4. Add protocol request/response types:
   - `TypeSessionPendingAsks = "session.pending_asks"` with `{}`;
   - response `session.pending_asks_result` with
     `{ "events": [<existing request event shapes>] }`.
   This is a read-only snapshot and does not append history or broadcast.
5. Add a dispatch case beside `session.history`; the handler calls
   `Manager.PendingAsks(deviceID)` and returns a correlated
   `TypeSessionPendingAsksResult` envelope. No new error code is needed because
   the in-memory snapshot does not fail. Add message round-trip/doc coverage,
   ownership tests, request→resolve/turn-complete removal tests,
   close/replacement exclusion tests, handler authentication tests, and
   protocol docs.
   History is deliberately not the source: its 800-event retention cannot
   guarantee that an old still-pending request remains present.

#### A11.2 Mobile reconciliation

1. Add `McremoteClient.pendingAsks()` returning parsed `SessionEvent`s and
   throwing typed errors (do not map failure to an empty authoritative
   snapshot).
2. Coordinator tracks a set of shown targets
   `(kind, sessionId, requestId)` and a map of known pending request events.
   Live request events enter the known map even when notification policy
   suppresses display; live resolutions remove and cancel them.
3. On every non-connected→connected transition, serialize one
   `_reconcilePendingAsks` run:
   - fetch the snapshot;
   - build the replacement known map;
   - cancel every tracked permission/question target absent from the snapshot;
   - replace known state only after all comparisons are computed, then call
     `_refreshAskNotifications`.
4. If the RPC fails, retain both known and shown state and retry on the next
   connected transition/maintenance reconnect. Never interpret failure as
   "nothing pending."
5. `_refreshAskNotifications` applies enabled + `_watching(sessionId)` to every
   known ask: show eligible unshown targets and cancel shown targets that are
   now being watched. Call it after live request/resolution, successful
   snapshot, `setEnabled`, `setAppForegrounded`, `claimSession`, and
   `releaseSession`.
6. `enabled=false` cancels all shown ask notifications but retains known asks,
   so re-enabling can surface requests that are still pending. Explicit
   coordinator disposal clears both maps after cancelling; RPC failure does
   not.

### Tests

- Go: two sessions owned by device A and one by B; A snapshot returns only A's
  unresolved events in deterministic order.
- Go: request→resolve, turn-complete, and error remove entries; closed and
  replaced entries do not appear.
- Dart: missed ask in snapshot after reconnect shows once; repeated identical
  snapshot is idempotent.
- Dart: absent target cancels stale notification; failed snapshot cancels
  nothing.
- Watching foreground session still suppresses display; background/different
  session shows it.
- Ask received while its chat is foregrounded is remembered without a
  notification; backgrounding the app shows it, returning to that chat cancels
  it, and re-enabling alerts shows it again if still unresolved.

### Acceptance

- Every unresolved ask known to the live daemon surfaces after reconnect,
  independent of bounded history and mounted screens; resolved asks cannot
  leave actionable stale notifications.

### Rollback

Protocol addition is backward-compatible. A client receiving `unknown_type`
may log/skip reconciliation, but this release's daemon and mobile changes ship
together.

---

## Phase B1 — T1 + T2: attempt-owned pair and relay resources

**Closes:** T1, T2, L-t3

**Files:** `mcremote_client.dart`

### T1 — `claimPairCode`

1. Introduce a private attempt-owned result, for example:

   ```dart
   typedef _OpenedSocket = ({
     WebSocketChannel channel,
     HttpClient httpClient,
     RelayTransport? relay,
   });
   ```

   `_openSocket`, `_openSocketDirect`, and `_openSocketViaRelay` return this
   bundle. Add one `_closeOpenedSocket(result)` helper that closes only those
   local resources.
2. After every asynchronous boundary that can overlap a newer attempt
   (`_resolvePin`, `_openSocket`, `request('pair.claim')`, and each credential
   persistence call), check `_staleAttempt(epoch)` before mutating shared
   connection, credential, paired, or reconnect state.
3. Until adoption, all failure/stale paths close the local bundle and throw a
   typed `pair_failed` / `pairing superseded`; they must not call
   `_failHandshake` or `_teardownSocket` against a newer attempt.
4. After `pair_ok`, check the epoch before assigning `_lastToken`, `_paired`,
   persisting the token/pin, or emitting `connected`. If persistence contains
   multiple awaits, check between them; a stale successful token is discarded.
5. Keep `_connectInternal`'s existing epoch guards, but route its socket open
   through the same ownership bundle so pair and normal connect cannot diverge.
6. This closes L-t3 too: `_closeOpenedSocket` always closes
   `channel.sink` before force-closing its `HttpClient`, including
   `channel.ready` timeout paths in direct and relay opens.

### T2 — relay transport ownership

1. `_openSocketViaRelay` keeps its new `RelayTransport` local and returns it in
   `_OpenedSocket`; it never assigns `_relayTransport` and its catch block
   closes `transport` directly, not `_closeRelayTransport()`.
2. The winning caller adopts the bundle atomically after its epoch check:
   close the previously adopted relay, then assign `_channel`, `_httpClient`,
   and `_relayTransport` from the result.
3. A stale caller invokes `_closeOpenedSocket(result)`. It must never call a
   helper that reads and clears shared `_relayTransport`, because that field
   may belong to the newer attempt.
4. `_openSocket` must not begin by closing shared relay state. Teardown of the
   previously adopted connection stays in the epoch setup path; socket opening
   itself is side-effect free with respect to connection fields.

### Tests

- Add injectable socket/relay factories under `@visibleForTesting`; avoid a
  timing-only live-network test.
- Pause attempt A after open, start/adopt B, then release A. Assert A closes
  only its channel/client/relay, B remains installed, and A cannot change
  `_autoReconnect`, `_paired`, token, error, or connection state.
- Pause A after `pair_ok` and before persistence, supersede with B, then
  release A; assert A's token/pin are neither stored nor installed.
- Relay open failure closes its local transport exactly once and leaves an
  already-adopted transport untouched.

### Acceptance

- Stale pair attempt cannot disable the newer connection's auto-reconnect or
  overwrite its token.
- Interleaved relay connects cannot orphan or clobber the live transport.

---

## Phase B2 — P1: fail safe at staged-image replay boundaries

**Closes:** P1

**Files:** `transcripts_notifier.dart`, `staged_images_test.dart`

### Steps

1. Keep `_sentImages` as transient, live-path-only FIFO state. The wire
   `AttachmentInfo` exposes only kind/MIME, so do not claim that text plus
   descriptors uniquely identifies a local send.
2. At the start of every `replayHistory`, `resyncHistory` rebuild, and other
   `_applyChunked` hydration generation, remove `_sentImages[sessionId]`
   **before** setting `_hydrating` or accepting deferred events.
3. `_drainDeferred` and `_applyChunked` never call `_zipStagedImages`. A
   user-message applied there renders descriptor-only placeholders, which is
   truthful. Once a replay boundary clears the FIFO, no later live echo can
   consume stale bytes.
4. Keep normal `_applyLive` FIFO zipping and current send-failure `removeLast`
   behavior; direct sends are serialized by `_sending`, and queued prompts
   flush one at a time. Preserve existing session clear/eviction cleanup.
5. Add a comment at `_sentImages` documenting the deliberate degradation:
   lossless restoration across reconnect needs a daemon-echoed unique
   `client_prompt_id`; content/descriptor matching is not a safe substitute.

### Tests

- Normal live echo still receives local bytes.
- Stage images, begin resync with an old image-bearing history event, then
  drain a new deferred `user_message`: both render placeholders and the next
  live prompt cannot receive the stale bytes.
- A superseded/failed `_applyChunked` generation still leaves the FIFO cleared;
  safety does not depend on final commit.
- Failed normal send removes its staged batch as before.

### Acceptance

- No wrong-image-on-next-bubble after resync/chunked apply. A rare reconnect
  race may lose only the local thumbnail enhancement, never wire content.

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

1. Add `_walkList(md.Element list, blocks, {required int depth})`. For each
   direct `li`, build one item block from only its non-list children, storing
   `depth` in `ParsedBlock.level`; then recurse each direct child `ul`/`ol` with
   `depth + 1`. Route top-level `ul`/`ol` cases in `_walkElement` through it.
2. Do not pass nested list nodes to `_extractSpans`. Splitting them into their
   own blocks prevents `_mergeAdjacent` from fusing `item1` + `nested` without
   inventing newline text inside either item.
3. In `chat_bubble.dart`, calculate list padding as
   `16.0 * (block.level + 1)` logical pixels. `ParsedBlock.level` currently
   affects headings only, so parser metadata alone is not a visible fix.
   Preserve ordered versus unordered marker selection at every level.
4. Re-run the reproduction: `parseMarkdownOffMain('- item1\n  - nested')`
   must not produce a single fused span `"item1nested"`.

### Tests

- Streaming: unclosed `**bold` buffers; finalized equals non-streaming parse
  for common fixtures.
- Nested unordered, ordered, and mixed-list parser tests plus a widget/golden
  assertion that the nested row is indented and text is not fused.

### Acceptance

- No literal marker flash in steady state; nested lists readable mid-stream.

---

## Phase B4 — P3: keystore transient errors

**Closes:** P3

**Files:** `settings_store.dart`, `settings_store_test.dart`

### Steps

1. Replace permanent `_secureDisabled = true` with a two-second cooldown
   (`_secureRetryAfter`) shared by reads/writes/deletes. Inject a clock for
   tests. During cooldown, use the existing preferences fallback; after
   expiry, the next operation probes secure storage again.
2. Catch `Exception`, not bare `Error`; programming/runtime `Error`s must
   remain visible. Log the operation and exception without token contents.
3. On successful secure-storage access, clear the cooldown and reconcile the
   fallback using the existing migration rules. Repeated failures extend only
   to `now + 2 seconds`, never the process lifetime.
4. Keep `AndroidOptions(resetOnError: true)`: the pinned
   `flutter_secure_storage` implementation already uses this option to
   delete/retry corrupt entries and the mobile standard mandates it. This
   finding is about the app's permanent secondary latch, not changing plugin
   recovery policy.

### Tests

- Fake clock + storage failing once then succeeding: fallback is used during
  cooldown and secure reads recover after expiry without process restart.
- Secure write/delete failure follows the same cooldown and later recovery.
- An injected `Error` is not swallowed; logs never contain token values.

### Acceptance

- One Keystore hiccup does not present the app as unpaired for the process
  lifetime.

---

## Phase B5 — C1–C4: chat UI correctness

**Closes:** C1, C2, C3, C4

**Files:** `chat_screen.dart`, `permission_loop_test.dart`,
`model_command_test.dart`, `staged_images_test.dart`

### C1 — external dismiss vs "Allow always?" dialog

1. Capture the exact sheet `ModalRoute` and track whether the
   "Allow always?" confirmation route is open.
2. On external resolution, dismiss an open confirmation dialog with its
   expected `false` result, then remove/pop the captured sheet route with
   `__external__`. Never blindly pop the navigator top with a `String`, which
   may be a `Future<bool?>` dialog.
3. Make disposal/idempotence explicit: if either route is already absent, the
   handler is a no-op and clears the matching open flag.
4. Test: open permission sheet + confirm dialog; fire external resolution;
   neither route stuck; no TypeError.

### C2 — config sheet revert

1. Guard `setSheet(() => opts[i] = prev)` with `sheetCtx.mounted` (same as the
   notification).

### C3 — `/model` reentrancy

1. Add a dedicated `_interceptingModel` flag around
   `_maybeInterceptModelCommand` before its first await; clear in `finally`.
   Do not reuse `_sending`: a selected `/model <id>` recursively calls the
   ordinary send path and must not be queued as though the agent were busy.
2. Reject a second interception while the flag is set and disable only the
   relevant composer/send interaction during the picker.

### C4 — busy queue + images

1. Extend `_QueuedPrompt` to own the text and immutable copies of both
   `PromptAttachment`s and local image bytes.
2. In the busy branch, atomically move `_pendingImages` into the queued prompt
   and clear the composer thumbnails. Attachment-only prompts are valid;
   `(text empty, attachments empty)` remains the only early return.
3. `_maybeFlushQueue` passes the queued attachments/bytes to the same direct
   send helper used by immediate sends. A transient send failure reinserts the
   complete prompt at the front; it must not reconstruct it from current
   composer state.
4. Queue chips label attachment-only entries (for example “1 image”) and
   removal drops the owned bytes as well as descriptors.

### Tests

- C3: double-tap `/model` → single picker / single intercept.
- C4: busy + staged images → queue owns them and composer clears; flush sends
  them; failed flush requeues them; removing the chip cannot leak them into the
  next prompt. Empty IME action with no attachments queues nothing, while
  attachment-only input queues once.

---

## Phase B6 — S2 + S3: connect-screen safety

**Closes:** S2, S3, L-t6

**Files:** `connect_screen.dart`, `settings_screen.dart` (dialog reuse)

### S2 — relay writes only on success

1. `_applyPair` currently writes `setRelayRoute` + `store.setRelayUrl/HostId`
   before claim/connect succeeds, and `setRelayUrl(null)` can wipe a good
   route.
2. Make relay hints attempt parameters instead of mutating client-global
   routing before the attempt:
   - add optional `relayUrl` / `relayHostId` parameters to `connectWithToken`
     and `claimPairCode`;
   - thread them into `_openSocket` / `_shouldUseRelay`;
   - keep them in the attempt-owned bundle from B1 until the epoch wins.
3. Only after authenticated connect/pair success, adopt the route in
   `McremoteClient` and persist relay URL/host ID beside host/token. An explicit
   empty relay in a successful new pairing clears the old route; failure or
   supersession changes neither the adopted nor persisted relay tuple. (A5
   separately clears an old cross-authority token before pairing for safety.)
4. Remove `_pendingRelay` / `_pendingHostId` if they are no longer needed.
   Pending UI state must not be a second source of routing truth.
5. This also closes L-t6: store the adopted route's normalized authority beside
   it and make `_shouldUseRelay(hostInput)` refuse hints whose authority does
   not match. Successful adoption replaces all three fields; a failed attempt
   leaves the previous authority/route tuple intact but ineligible for B.

### S3 — confirm clear credentials

1. Reuse Settings' `AlertDialog` ("Clear saved credentials?") before
   `clearAll()` on the connect overflow menu.
2. Identical copy and destructive emphasis.

### Tests

- Failed claim after QR with empty relay does not clear stored relay of prior
  pairing and does not change the client's adopted route.
- Successful claim with new relay adopts and persists both fields; successful
  direct pairing clears both.
- Clear credentials shows dialog; cancel leaves store intact.

---

## Phase B7 — N1: parked error still retries in background

**Closes:** N1

**Depends on:** A10

**Files:** `mcremote_client.dart`, `notification_coordinator.dart`,
`app_lifecycle.dart`, corresponding tests

### Steps

1. Keep fast reconnect/backoff ownership in `McremoteClient`, but expose a
   read-only signal such as `willAutoReconnect` / `reconnectParked` so the
   coordinator can distinguish a parked retryable error from permanent or
   manual disconnection.
2. Replace public writes to
   `NotificationCoordinator.appForegrounded` with
   `setAppForegrounded(bool)`. The coordinator owns one maintenance timer and
   schedules it only when all are true:
   - notifications are enabled;
   - app is backgrounded;
   - connection is `error` and the client reports parked/retryable;
   - paired credentials still exist.
3. Use a documented slow interval (start at 5 minutes, cap at 30 minutes with
   bounded exponential backoff). Timer fire rechecks every condition before
   calling `client.reconnect()`; it never overlaps an in-flight reconnect.
4. Cancel/reset the maintenance timer on foreground, connected,
   reconnecting, disconnected/manual logout, credentials cleared, or
   notifications disabled. A failed maintenance attempt schedules the next
   capped interval; success resets the interval.
5. Fold L-n4 here: do not acquire a permanent Wi-Fi lock while parked. The
   foreground service keeps the process eligible. Set both `allowWakeLock` and
   `allowWifiLock` false, document the battery trade-off, and add the
   screen-off device smoke below; this plugin's lock options are service-wide,
   so they cannot be toggled only around an individual reconnect.
6. Optional one-shot “Connection lost — alerts paused” notification is not
   required to close N1; if added, key/cancel it deterministically and avoid
   repeating it each timer.

### Tests

- Fake-clock coordinator test: after the client's Nth failure parks it in the
  background, exactly one timer is armed; firing it attempts one reconnect.
- Foregrounding, disabling alerts, manual disconnect, credential clear, and a
  normal connected transition each cancel the timer.
- Permanent/non-retryable errors never arm it; repeated parked failures back
  off and cap as documented; no overlap with an in-flight attempt.
- Foreground-service initialization sets both lock flags false; manual
  screen-off smoke keeps the socket/reconnect functional without them.

### Acceptance

- Daemon downtime >1 minute does not permanently kill background alerts until
  next process start.

---

## Phase B8 — W1: expose request timeout resolution

**Closes:** W1

**Files:** `models.dart`, `chat_models.dart`, `transcript_reducer.dart`,
`transcripts_notifier.dart`, `chat_screen.dart`,
`transcript_reducer_test.dart`, `permission_loop_test.dart`

### W1 — `timed_out`

1. Parse `timed_out` on `SessionEvent` (`json['timed_out'] == true`).
2. Add `permissionResolutions` to `SessionTranscript`, keyed by permission id.
   In `_onPermissionResolved`, insert the resolved event before removing the
   pending request, cap the insertion-ordered map at 32 entries, and ignore an
   exact duplicate. Add
   `TranscriptsNotifier.consumePermissionResolution(sessionId, permissionId)`
   and clear the map on session eviction.
3. Split the sheet dismissal callbacks by request kind. The transcript listener
   looks up the open permission id in `next.permissionResolutions` and closes
   that exact sheet with `__external_timeout__` when `timedOut`, otherwise
   `__external__`. After the route returns, consume that resolution record.
4. Where permission external resolution currently says “resolved elsewhere”,
   branch on the route result: timeout shows
   “Request timed out — the agent moved on”; otherwise retain existing copy.
5. `_onPermissionResolved` appends exactly one system transcript item for a
   newly observed timed-out id. The modal only shows transient top copy; it
   never appends, so live/replayed duplicates cannot create two lines.

### Tests

- Parse and retain `timed_out`; the permission sheet displays timeout copy
  after the reducer has consumed the pending request.
- Live plus replayed duplicate resolution produces one system line; bounded
  resolution metadata clears after consumption and session eviction.

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
| L-t3 | folded into B1 | Attempt-owned socket cleanup closes late upgrades |
| L-t4 | `mcremote_client.dart` | Remove broad catch in `sessionHistory`; propagate typed request/transport errors and reserve `[]` for an authoritative empty result |
| L-t5 | `mcremote_client.dart` | `_failAllPending` → `McException(..., code: 'connection_lost')` |
| L-t6 | folded into B6 | Relay hints carry and validate their authority |
| L-t7 | `relay_transport.dart` | Pass through `Uint8List` without `List<int>.from` |

### C2 — transcript / provider polish

| ID | File | Work |
|---|---|---|
| L-p1 | `transcripts_notifier.dart` | `TranscriptCache.retainOnly(liveIds)` on `syncFromMeta` |
| L-p2 | `transcripts_notifier.dart` | Snapshot `t.items.isNotEmpty` once per replay batch |
| L-p3 | `transcripts_notifier.dart`, `transcript_cache.dart` | Route every unawaited save through `_saveCacheBestEffort` with catch+`debugPrint`; construct the plain payload map then JSON-encode it via `compute` using a top-level callback |
| L-p4 | `app_providers.dart` | Set `_userChanged=true` in `set`; guarded `_load` assigns only when false, including when the user explicitly chose system |
| L-p5 | `transcripts_notifier.dart` | Convert `sessionTranscriptProvider` to `Provider.autoDispose.family`; remount derives the same value from global `transcriptsProvider` |

### C3 — UI / settings / notifications / docs

| ID | File | Work |
|---|---|---|
| L-c1 | `chat_bubble.dart` | `textScaler: MediaQuery.textScalerOf(context)` on streaming `RichText` |
| L-c2 | `chat_screen.dart` | `context.mounted` before `setMode` after await |
| L-c3 | `chat_screen.dart` | `_scrollToEnd(force:)` skips `_listScrolling` when forced |
| L-c4 | `scroll_activity.dart` | ignore nested scrollables (`depth != 0`) |
| L-c5 | `top_notification.dart` | `Dismissible`; longer duration when action present |
| L-s1 | `sessions_screen.dart` | Spinner only when `_sessions.isEmpty` |
| L-s2 | `sessions_screen.dart` | On end-session failure call authoritative `_refresh()`; never restore the pre-await list snapshot |
| L-s3 | `settings_screen.dart` | try/catch prefs reads in `_load` |
| L-s4 | `sessions_screen.dart` | `friendlyOpError` for banners |
| L-s5 | `sessions_screen.dart` | Disable Rename Save while the trimmed input is empty and show the existing name as initial text |
| L-s6 | `settings_store.dart` | `clearAll` removes `_kLastCwd`, `_kRecentCwds`, and every key with either preferred-model prefix |
| L-n1 | `notification_coordinator.dart` | Subscribe to streams before `await init` |
| L-n2 | folded into A11 | — |
| L-n3 | folded into A10 | State-appropriate service text |
| L-n4 | folded into B7 | Maintenance backoff and parked Wi-Fi-lock policy |
| L-n5 | folded into A9 | Notification identity includes kind + session + request |
| L-w1 | `chat_screen.dart` | Usage chip with count when `size <= 0 && used > 0` |
| L-w2 | `docs/protocol-v1.md` | `session_config` merge semantics, not full replacement |
| L-w4 | `docs/protocol-v1.md` | Add `switch_mode` to tool_kind vocabulary |
| L-w5 | `apps/mobile/README.md` | Replace “transcripts are in-memory only” with daemon-ring replay + best-effort bounded phone-cache behavior |

### C-wave tests

- Prefer extending existing tests (`settings_store_test`, `top_notification_test`,
  `streaming_markdown_test`, `mc_exception_test`).
- C1: inject request/transport errors and assert `sessionHistory` throws the
  typed failure while a real empty result returns `[]`; exercise stale ping
  epochs and failed identity retry.
- C2: evict a deleted session and assert memory plus cache removal; verify a
  late theme load cannot overwrite light, dark, or an explicit system
  selection. Make cache persistence throw and assert the best-effort wrapper
  captures/logs it; exercise the top-level encoder with a maximum-tail payload.
  Listen to an auto-disposed session provider, close the listener, then remount
  and assert it re-derives the same global transcript without retaining the
  old family element.
- C3: add focused widget/controller coverage for each behavioral row. For
  `clearAll`, seed last/recent cwd plus two providers' preferred model and
  model-provider keys and assert all are removed; for L-n1, emit an event
  synchronously during notification init and assert it is observed.
- Doc-only items (L-w2, L-w4, L-w5): no code test; ensure
  `TestEventTypesAreDocumented` / protocol drift tests still pass if any.

---

## Phase V — Verification matrix

Run before declaring the audit closed.

### Mobile

```bash
make preflight
```

`make preflight` is the repository authority for the full mobile trio
(`dart format --output=none --set-exit-if-changed .`, `flutter analyze`, and
`flutter test`). During implementation, run the smallest relevant tests after
each change, then the full preflight before staging.

Targeted clusters after each phase group (exact filenames may be extended when
a phase extracts a new testable helper):

| After | Tests |
|---|---|
| 0 | `sessions_screen_test`, `model_provider_step_test` |
| A1 | `chat_screen_test.dart`, request-size helper/client tests |
| A2 | `pair_uri_test.dart`, `connect_screen_test.dart`, Android policy test/check |
| A3 | `app_router_test.dart`, `connect_screen_test.dart`, `chat_screen_test.dart` |
| A4 | `permission_loop_test.dart` (+ question variant) |
| A5 | `cert_pinning_test` |
| A6 | `relay_transport_test`, `relay_path_test` |
| A7 | `history_replay_test` |
| A8 | model parse + session manager Go tests |
| A9–A11 | `notifications_test.dart` plus WS/session Go handler tests |
| B1 | `mcremote_client_test.dart` / extracted socket ownership tests |
| B2 | `staged_images_test.dart`, `history_replay_test.dart` |
| B3 | `streaming_markdown_test.dart`, markdown parser tests |
| B4 | `settings_store_test.dart` |
| B5 | `chat_screen_test.dart`, `permission_loop_test.dart` |
| B6 | `connect_screen_test.dart`, settings tests |
| B7 | notification coordinator / foreground service tests |
| B8 | model parse, transcript reducer, permission widget tests |
| full | `make preflight` from repository root |

### Daemon (when Go changed)

```bash
make pre-add-check FILES="<every touched .go file>"
go test ./internal/session ./internal/event ./internal/protocol ./internal/ws
go test -race ./internal/session ./internal/ws
```

Run `make pre-add-check` before any `git add` of Go files, as required by the
repository gate; the final pre-commit path still runs the full race suite.

### Manual smoke (device or emulator)

1. On Linux/dev, pair a real `mode=off` QR over `ws://`; on Android, assert
   the same QR is rejected before host/token/pin mutation with actionable copy.
   Pair self-signed and Let's Encrypt TLS daemons as regressions.
2. Switch hosts A→B with pins (H1).
3. Send large photo — rejected cleanly (H4).
4. Kill network mid-turn; restore — no permanent transcript hole (H2); pending
   permission notifies (H8).
5. Navigate session A→B quickly with a queued prompt (H10).
6. Fail a question RPC — no modal loop (H9).
7. Background app; trigger question — notification (H6); toggle airplane mode
   — FGS survives reconnect (H7). Lock the screen for at least 10 minutes with
   wake/Wi-Fi locks disabled, restore the daemon/network, and confirm the
   maintenance retry reconnects and a new ask notifies (N1/L-n4).

---

## 2. Finding → phase index

| ID | Sev | Phase |
|---|---|---|
| L-tool | Low | 0 |
| H4 | High | A1 |
| H3 | Med | A2 |
| H10 | High | A3 |
| S1 | Med | A3 |
| H9 | High | A4 |
| H1 | High | A5 |
| T3 | Med | A5 |
| T4 | Med | A6 |
| H2 | High | A7 |
| H5 | High | A8 |
| H6 | High | A9 |
| L-n5 | Low | A9 |
| H7 | High | A10 |
| L-n3 | Low | A10 |
| H8 | High | A11 |
| L-n2 | Low | A11 |
| T1 | Med | B1 |
| T2 | Med | B1 |
| L-t3 | Low | B1 |
| P1 | Med | B2 |
| P2 | Med | B3 |
| P4 | Med | B3 |
| P3 | Med | B4 |
| C1–C4 | Med | B5 |
| S2–S3 | Med | B6 |
| L-t6 | Low | B6 |
| N1 | Med | B7 |
| L-n4 | Low | B7 |
| W1 | Med | B8 |
| W2 | Follow-up | Out of scope |
| W3 | Follow-up | Out of scope |
| Remaining non-folded L-* | Low | C1–C3 |

All nine high-severity findings appear in Phase A (plus Phase 0 hygiene).
Every verified in-scope finding has a phase; W2 and W3 are explicitly
reclassified product follow-ups rather than correctness gaps.

---

## 3. Risk notes

1. **H1 trust migration** is the highest-risk change. A persisted device ID
   cannot authenticate a new authority before TLS/auth, so host movement must
   require fresh QR fingerprint evidence. Write tests first; exercise A→B→A,
   same-daemon new-authority, and different-daemon cases without ever
   overwriting A's ID pin from B.
2. **H10 `ValueKey`** is one line but will reset scroll/composer when the key
   changes — desired. Confirm go_router version still keys pages by pattern
   (17.3.0 noted in MADR).
3. **A11 reconcile** changes the wire protocol and daemon memory state. The
   daemon snapshot, not bounded history or a mobile reducer, is authoritative;
   ship daemon and client together and handle `unknown_type` without clearing
   notifications.
4. **B1/B6 attempt ownership** touches connection setup broadly. Centralize
   the resource bundle and relay arguments once; duplicated “restore old
   state” paths are race-prone.
5. **L-p5 autoDispose** can surprise if something assumes eternal transcript
   providers — grep listeners before flipping.
6. **Daemon H5 persist** should not call provider Rename (side effects /
   ACLs); only `Meta.Name`.

---

## 4. Out-of-scope follow-ups (record, do not implement here)

- Full audio-prompt attachment UX. Providers do not uniformly support audio,
  the app has speech-to-text but no recording/file-picker dependency, and this
  needs a separate capability/product decision rather than being implied by
  `capabilities.audio` parsing.
- Showing unavailable slash commands with their `reason`. The protocol says
  clients should offer available commands; the current filter conforms.
  Exposing unavailable commands is an optional discoverability enhancement.
- Negotiated larger WS frames / chunked prompt upload for big attachments.
- A daemon-echoed `client_prompt_id` for lossless restoration of local
  thumbnail bytes across history rebuilds. Current attachment descriptors are
  intentionally metadata-only and cannot support safe correlation.
- Composite pin keys multi-daemon simultaneous identity (beyond A5 minimum).

---

## 5. Implementation checklist (copy into PR / commit series)

- [ ] Phase 0 — L-tool NUL
- [ ] Phase A1 — H4 size budget + protocol note
- [ ] Phase A2 — H3 `mode=off` representation + platform policy
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
- [x] Phase B2 — P1 images
- [x] Phase B3 — P2/P4 markdown
- [x] Phase B4 — P3 keystore
- [x] Phase B5 — C1–C4
- [x] Phase B6 — S2/S3
- [x] Phase B7 — N1
- [x] Phase B8 — W1 timeout resolution
- [ ] Phase C — lows C1–C3 waves
- [ ] Phase V — full verification

---

## 6. References

- [MADR 0045 — findings](./0045-MADR-mobile-app-hardening-audit.md)
- [protocol-v1.md](./protocol-v1.md) — frame limits, `session_title`, `timed_out`
- [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md) — resync precedent
- [MADR 0015](./0015-MADR-mcrelay-transport-security.md) — relay threat model
- Commit `00d15b7` — permission modal loop fix (mirror for H9)
