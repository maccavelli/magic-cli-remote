# MADR 0046 — Implementation plan: post-remediation mobile debug pass

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0046](./0046-MADR-mobile-debug-pass.md). Read that first:
it carries the verified findings, decisions, and severity rationale. This
document is the build order — phase-sequenced and keyed to current source
locations as of 2026-07-28 (`5322e63`).

- **Status**: **Complete** (2026-07-29). All phases A1–C5 landed one commit
  each, in the order below, plus Phase V. Deviations are recorded in MADR 0046
  §7.
- **Date**: 2026-07-28
- **Scope**: `apps/mobile` Dart sources and tests; small Go changes for
  L-13/I-2 and a protocol-doc change for I-1. No daemon protocol changes.
- **Standards**: `/home/mac/standards/mobile` v3.12.2-v3 — `networking.md`,
  `architecture.md`, `dart.md`, `flutter.md`, `android.md`
- **Related**: [MADR 0045](./0045-MADR-mobile-app-hardening-audit.md) /
  [plan](./0045-PLAN-mobile-app-hardening-audit.md),
  [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md)

---

## 0. Goal, non-goals, and ground rules

### Goal

Close every finding in MADR 0046 — H-A…H-D, M-1…M-9, L-1…L-13, I-1/I-2 —
with independent, testable changes, and land the regression test that each
finding proved missing. All four highs are regressions from the 0045
remediation batch; the bar for "done" is therefore *stronger than green*:
each phase must add the test that would have caught its finding.

### Non-goals

- No new protocol event types, RPCs, or daemon behavior changes (M-4 is
  app-side by decision; L-13/I-2 are Go hygiene, not wire changes).
- No transport replacement, no `web_socket_channel` upgrade in this plan
  (H-A is fixed by call-order, not by taking a new dependency version; an
  upgrade may follow separately).
- No queued-prompt persistence across screen exit (H-D decision records the
  State-scoped queue as intended behavior).
- No renaming of the transcript-cache key namespace (H-C decision: exclude,
  don't migrate).

### Ground rules

1. **One phase → one commit** as the review shape; A2 may use a tightly
   related pair (store contract + client call sites). Do not push unless the
   user asks.
2. **Go gate before `git add`** for any touched `.go` file:
   `make pre-add-check` / `scripts/go-precheck.sh` (`gofmt`, `golint`,
   `govulncheck`).
3. **Dart gate**: `dart format` on every touched `.dart` file;
   `flutter analyze` and `flutter test` in `apps/mobile` per phase;
   `make preflight` before calling a multi-phase block done.
4. **Commit without `-m`**: the prepare-commit-msg hook generates the message.
5. **Line numbers drift.** Prefer symbol names + the quoted surrounding
   comments over exact line numbers when applying patches.
6. **No phase is complete at code compile alone.** Run its named regression
   tests first red (where feasible), then green, then the phase gate.
7. **Order highs first.** A1–A4 before any B phase; the B order below is
   value-ranked but B phases are mutually independent unless noted.

### File map (canonical paths)

| Area | Path |
|---|---|
| WS client | `apps/mobile/lib/data/ws/mcremote_client.dart` |
| Typed errors | `apps/mobile/lib/data/ws/mc_exception.dart` |
| Relay bridge | `apps/mobile/lib/data/ws/relay_transport.dart` |
| Settings / pins | `apps/mobile/lib/data/local/settings_store.dart` |
| Transcript cache | `apps/mobile/lib/data/chat/transcript_cache.dart` |
| Transcripts | `apps/mobile/lib/state/transcripts_notifier.dart` |
| Reducer | `apps/mobile/lib/data/chat/transcript_reducer.dart` |
| Chat models | `apps/mobile/lib/data/chat/chat_models.dart` |
| Chat UI | `apps/mobile/lib/features/chat/chat_screen.dart` |
| Sessions UI | `apps/mobile/lib/features/sessions/sessions_screen.dart` |
| Connect | `apps/mobile/lib/features/connect/connect_screen.dart` |
| Toasts | `apps/mobile/lib/theme/top_notification.dart` |
| Notif coordinator | `apps/mobile/lib/data/notifications/notification_coordinator.dart` |
| Notif service | `apps/mobile/lib/data/notifications/notification_service.dart` |
| Provider meta (Go) | `internal/provider/provider.go` |
| Events (Go) | `internal/event/event.go` |
| WS server (Go) | `internal/ws/server.go` |
| Protocol doc | `docs/protocol-v1.md` |

### Verified baseline facts (do not rediscover)

Re-checked against source and packages while writing this plan:

| Claim | Evidence |
|---|---|
| H-A: failed-dial `sink.close()` never completes in `web_socket_channel 3.0.3` | Reproduced with the app's locked package config: refused-port dial, `ready` failed at 44 ms, `sink.close()` still pending at 10 s |
| H-A: safe close order already exists in-repo | `_closeOpenedSocket` bounds the close with `.timeout(2 s)` (`mcremote_client.dart:559-570`) |
| H-B: exact deleted guard | `git show 6fe2e0d -- …settings_store.dart`: `persistedMatchesAuthority` computed from `getHost()` authority, removed from both `getPinnedCert` and `setFingerprint` |
| H-B: churn motivation | `settings_store_test.dart` "survives tailnet IP churn when the device identity is known" writes and reads with **implicit** persisted id |
| H-B: claim-migration exists | test "a pin taken before pairing is claimed by the identity that follows" |
| H-B: client already nulls identity on authority change | `_noteHost` doc comment (`mcremote_client.dart:1085-1098`): "a stale [deviceId] would otherwise key the next host's certificate pin under the previous host's identity" |
| H-C: index key is inside the entry namespace | `_indexKey = 'tx_cache_v1_index'`, `_entryPrefix = 'tx_cache_v1_'` (`transcript_cache.dart:19-20`) |
| H-C: `retainOnly` runs constantly | `syncFromMeta` → `_cache.retainOnly` on every sessions refresh/reconnect (`transcripts_notifier.dart:790`) |
| H-D: riverpod throw is unconditional | `flutter_riverpod-3.3.2 …/consumer.dart` `_assertNotDisposed`: literal `throw StateError(…)`, not an assert |
| H-D: guard placement | `chat_screen.dart:906-914` — `ref.read` at `:910` precedes the `mounted` check at `:914` |
| M-1: `healthz` skips `_noteHost` | `mcremote_client.dart:697-728` vs. `claimPairCode`/`connect`/`_connectInternal`, which all call `_noteHost` before `_resolvePin` (`:752/:796/:948`) |
| M-3: `_clearSecret` honours the cooldown | `settings_store.dart:548-559`; `_shouldTrySecure` (`:605-608`); cooldown armed by any failure (`:615-617`) |
| M-5: gap flagged inside `_noteSeq`; fold notes heads early | `transcripts_notifier.dart:147-156` (flag), `:398/:412` (fold-time notes), `:416-419` (neutral pass-through) |
| M-6: text-literal dedup | `transcript_reducer.dart:221-228` scans all items for `'Permission request timed out'` |
| M-9: `force` exists, never passed | `_scrollToEnd({bool force = false})` guard `if (_listScrolling.value && !force) return;` (`chat_screen.dart:449-468`); no call site passes it (`grep -n 'force: true'` → none) |
| L-3: blanket-permanent mapping | `mc_exception.dart:52-70` — both branches and the fallback construct `permanent: true` |
| L-13: `omitempty` inert on struct | `internal/provider/provider.go:312`; `omitzero` precedent at `internal/event/event.go:373` |
| Baseline green | `flutter analyze`: no issues; `flutter test`: 408/408 at `5322e63` |

---

## 1. Dependency graph and order

```text
Phase A1  H-A dial-failure hang ────────────── transport reliability
Phase A2  H-B pin identity contract + M-1 ──── pin security cluster
Phase A3  H-C cache index ──────────────────── storage integrity
Phase A4  H-D + L-8 lifecycle reads ────────── chat send safety
Phase B1  M-2 pending pair hints
Phase B2  M-3 sign-out keystore bypass
Phase B3  M-4 ask lifecycle ──→ C2 (both touch the coordinator)
Phase B4  M-5 seq-gap detection
Phase B5  M-6 timeout-notice dedup
Phase B6  M-7/M-8 controller ownership
Phase B7  M-9 FAB force
Phase C1  transport lows L-1 L-2 L-3 L-4
Phase C2  notification lows L-5 L-12          (after B3)
Phase C3  pipeline lows L-6 L-7
Phase C4  UI lows L-9 L-10 L-11
Phase C5  Go/protocol L-13 I-1 I-2            (independent of all Dart phases)
Phase V   full verification
```

- A1–A4 are independent of each other; land in ID order for review clarity.
- All B phases are independent of each other and of A, **except** B1 is more
  meaningful after A2 (both touch pairing trust) and C2 must follow B3.
- C5 can land at any point (Go-only).
- Suggested sequential order:
  `A1 → A2 → A3 → A4 → B1 → B2 → B3 → B4 → B5 → B6 → B7 → C1 → C2 → C3 → C4 → C5 → V`.

---

## Phase A1 — H-A: failed dial must fail fast (transport)

**Closes:** H-A

**Files:** `apps/mobile/lib/data/ws/mcremote_client.dart`
(`_openSocketDirect`, `_openSocketViaRelay`), new test
`apps/mobile/test/socket_dial_failure_test.dart`.

### Problem recap

Both dial catch blocks `await channel?.sink.close();` before
`httpClient.close(force: true)`. When `ready` failed, that close future
never completes (web_socket_channel 3.0.3 wires the internal controller's
listener only on connect success), so the catch never reaches
`pinner.translate` and the caller never returns: connect spinner latched,
`_reconnectInFlight` stuck true, auto-reconnect dead, `HttpClient` leaked.

### Steps

1. In `_openSocketDirect`'s catch (currently `:486-492`): reorder to

   ```dart
   } catch (e) {
     httpClient.close(force: true); // aborts the in-flight connect; resolves
                                    // the adapter's futures
     final c = channel;
     if (c != null) {
       unawaited(
         c.sink.close().timeout(const Duration(seconds: 2)).catchError((_) {}),
       );
     }
     throw pinner.translate(e, url);
   }
   ```

   `unawaited` comes from `dart:async`. The 2 s bound mirrors
   `_closeOpenedSocket`.
2. Same reorder in `_openSocketViaRelay`'s catch (currently `:547-556`):
   `httpClient.close(force: true)` first, then unawaited bounded sink close,
   then `await transport.close()` (the relay transport close is safe and must
   still be awaited so the loopback server is released before rethrow), then
   `throw pinner.translate(e, url)`.
3. Audit the two callers (`_connectInternal`, and the reconnect path that
   sets `_reconnectInFlight`) for any other `await` on a possibly-dead
   channel sink in failure paths; none is expected, but the grep is cheap:
   `grep -n "sink.close" lib/data/ws/mcremote_client.dart` — every hit must
   be either post-`ready`-success or timeout-bounded.

### Tests (`socket_dial_failure_test.dart`)

Real sockets, no fakes — this class of bug is invisible to fakes:

1. **Refused port**: bind a `ServerSocket`, note the port, close it, then
   `McremoteClient.connect` (or a test seam invoking `_openSocketDirect` via
   the public connect with a `127.0.0.1:<port>` host). Assert the future
   completes with an `McException` **within 5 s** (generous vs. the 8 s ready
   timeout path — refused is immediate).
2. **Black hole**: a `ServerSocket` that accepts and never speaks TLS/WS →
   assert typed failure within the ready timeout + margin (≤ 12 s), not a
   hang. Mark `timeout: Timeout(Duration(seconds: 30))`.
3. **Reconnect machinery revives**: after a failed dial, assert a subsequent
   `connect()` attempt is not blocked (state returns to `error`, a second
   call reaches the dial again — observable via a counting connection
   attempt hook or by the second typed failure arriving promptly).
4. **Pin mismatch surfaces typed**: dial a local `SecureServerSocket` with a
   self-signed cert while pinned to a different fingerprint; assert the
   error is the typed `cert_mismatch` (this is the path H-A was swallowing).

### Acceptance

- All four tests green; running test 1 against the **pre-fix** tree hangs
  (verify once to confirm the test bites, then apply the fix).
- `flutter analyze` clean; full `flutter test` green.

### Rollback

Call-order change confined to two catch blocks; no persisted state.

---

## Phase A2 — H-B + M-1: pin identity contract (store + client)

**Closes:** H-B, M-1

**Files:** `settings_store.dart`, `mcremote_client.dart`,
`test/settings_store_test.dart`, `test/mcremote_client_test.dart` (or new
`test/pin_identity_test.dart`).

### Problem recap

`6fe2e0d` replaced the authority-guarded persisted-id fallback with an
unconditional one in both `getPinnedCert` and `setFingerprint` to survive
tailnet IP churn. That lets an unclaimed pairing attempt against daemon B
overwrite daemon A's `id:` pin (write side) and lets any host lookup vouch
with the paired daemon's pin (read side). `healthz` additionally probes
without `_noteHost`, under the stale identity and stale `_tlsMode`.

### Steps

#### A2.1 Store: writes never adopt the persisted identity

In `setFingerprint` (currently `:350-…`), drop the persisted-id fallback:

```dart
final explicitId = _idOrNull(deviceId);
final effectiveId = explicitId; // null → authority-keyed pending pin
```

With `effectiveId == null` the existing `_pinKey(null, authority)` path
writes the `host:authority` record with `device_id: null` — the
pre-`6fe2e0d` pending-pin shape that the claim-migration ("claimed by the
identity that follows") already promotes on pairing success. Keep the
"identity known ⇒ supersede `host:` record" branch as is.

#### A2.2 Store: reads fall back only on opt-in

`getPinnedCert` (and the `getFingerprint` convenience) gain
`bool fallbackToPersistedIdentity = false`:

```dart
final persistedId = (explicitId == null && fallbackToPersistedIdentity)
    ? await getDeviceId()
    : null;
final effectiveId = explicitId ?? persistedId;
```

The rest of the lookup (id-keyed primary, authority-keyed secondary with the
owner rules) is unchanged. Update the doc comment: the persisted identity
vouches only when the caller asserts it is dialling the paired daemon
(token-bearing connect), because the daemon accepting the stored token is
the continuity proof.

#### A2.3 Client: pass intent

- `_resolvePin` gains `{required bool assumePaired}` and forwards it as
  `fallbackToPersistedIdentity` on the `getPinnedCert` call (`:395-398`).
  Note `deviceId:` stays as today (in-memory id, nulled by `_noteHost` on
  authority change — that invariant is now honoured by the store).
- Call sites: `connect` / `_connectInternal` reconnect paths (`:798`,
  `:951`) pass `assumePaired: true` **only when a stored token will be
  presented** (both already sit on token-bearing paths; assert with a
  comment). `claimPairCode` (`:754`) passes `false`.
- `setFingerprint` inside `_resolvePin` (`:374-379`) needs no change: with
  A2.1, a null in-memory `deviceId` now yields a pending `host:` record
  instead of adopting the persisted identity.

#### A2.4 Client: side-effect-free `healthz` (M-1)

In `healthz` (`:697-728`):

- Resolve the probe pin without touching client pin state: do **not** call
  `_resolvePin`. Inline: explicit `fingerprint` param or
  `SettingsStore.fingerprintFrom(hostInput)` if present (normalized), else
  `await _settings.getPinnedCert(hostInput, deviceId: null)` (no fallback).
- Build the probe's `CertPinner` from the **resolved** mode:
  explicit `mode` param → `#tls=` from the host string → stored pin's mode →
  `TlsMode.fallback`; never from the `_tlsMode` field.
- If a scanned pending fingerprint should still be persisted at probe time,
  call `_settings.setFingerprint(hostInput, fp, deviceId: null, mode: …)`
  directly — with A2.1 that is a safe authority-keyed pending pin.

#### A2.5 Tests

1. Rewrite "survives tailnet IP churn…" to the real flow: pin written with
   an **explicit** id (as pairing does), read back from the new address with
   `fallbackToPersistedIdentity: true` after `setDeviceId` — still returns
   the pin.
2. New: **second-daemon write** — persisted id dev-A with `id:dev-A` pin;
   `setFingerprint(hostB, fpB)` (no id) must NOT touch `id:dev-A`; the pin
   lands under `host:<authorityB>`; a subsequent claim as dev-B promotes it.
3. New: **second-daemon read** — `getPinnedCert(hostB)` without opt-in
   returns null; with opt-in returns the persisted-identity pin (documented
   caller responsibility).
4. Client-level: fake settings store recording calls — pair-claim flow never
   passes the fallback; reconnect flow does; `healthz` against host B with a
   paired dev-A never reads or writes an `id:dev-A` record and leaves
   `_pinnedFor`/`_tlsMode` untouched (expose via existing test seams or
   `@visibleForTesting` getters).

### Acceptance

- All A2.5 tests green; pre-fix, tests 2–4 must fail (verify test 2 bites
  before fixing).
- Existing pin tests (`legacy migration`, `different identity never
  inherits`, `claimed by the identity that follows`) still green.

### Rollback

Store change is behavioral, not schema: records written by the buggy build
(`id:` overwrites) are not repaired retroactively — the MADR accepts rescans
for already-contaminated installs. No migration to revert.

---

## Phase A3 — H-C: `retainOnly` must not eat the index

**Closes:** H-C

**Files:** `transcript_cache.dart`, `test/transcript_cache_test.dart`.

### Problem recap

`_retainOnly` (`:169-183`) prefix-sweeps every key starting with
`tx_cache_v1_` — which includes `tx_cache_v1_index` itself (`id = 'index'`
never in `liveIds`) — then reads the now-deleted index and persists `[]`.
Runs on every sessions refresh/reconnect via `syncFromMeta`; eviction and
`clear()` are permanently defeated; blobs accumulate unbounded.

### Steps

1. Add a private predicate and use it in **every** prefix sweep in the file
   (`_retainOnly`, and the orphan sweeps in `_clear`/save paths if they
   share the pattern):

   ```dart
   bool _isEntryKey(String key) =>
       key.startsWith(_entryPrefix) && key != _indexKey;
   ```

2. Reconcile in `_retainOnly` (repairs damage shipped by the buggy build):

   ```dart
   final surviving = p.getKeys().where(_isEntryKey)
       .map((k) => k.substring(_entryPrefix.length))
       .where(liveIds.contains)
       .toSet();
   final kept = (p.getStringList(_indexKey) ?? const <String>[])
       .where(surviving.contains)
       .toList();
   kept.addAll(surviving.difference(kept.toSet())); // recovered orphans
   await p.setStringList(_indexKey, kept);
   ```

   Ordering note: recovered orphans append at the MRU end is *wrong* — they
   are of unknown age; append at the **front** (LRU end) so they evict
   first. Keep the existing index order for known entries.
3. Do not rename `_indexKey` (no data migration).

### Tests

1. `retainOnly` with all cached sessions live preserves the index verbatim
   and every blob (fails on the pre-fix tree — verify once).
2. Orphan repair: write a blob with no index entry (simulating the shipped
   bug), call `retainOnly` with it live → it appears in the index (at the
   LRU end) and a subsequent `clear()` removes it.
3. Eviction still works after `retainOnly`: fill past
   `kTranscriptCacheMaxSessions`, `retainOnly(all)`, add one more → oldest
   evicted, index length capped.

### Acceptance

- New tests green, existing cache tests (incl. `:250`'s invariant) green.

### Rollback

Pure logic; the reconcile is idempotent and safe on healthy stores.

---

## Phase A4 — H-D + L-8: lifecycle-safe provider reads in chat

**Closes:** H-D, L-8

**Files:** `chat_screen.dart`, `test/chat_send_failure_test.dart` (new) or
extend `staged_images_test.dart`.

### Problem recap

`_sendText`'s catch calls `ref.read(transcriptsProvider.notifier)` before
the `mounted` guard; riverpod 3.3.2 throws `StateError` on unmounted `ref`
use in all build modes. Backing out of the chat with a prompt in flight that
then fails → unhandled `StateError` + `_sentImages` FIFO leak → thumbnails
mis-attach to the next image echo. `_ModeSelector.onSelected` (`:2591`) has
the same class of defect after its confirm-dialog await.

### Steps

1. In `_sendText` (`:893-…`), before the first `await`:

   ```dart
   final client = ref.read(mcremoteClientProvider);
   final transcripts = ref.read(transcriptsProvider.notifier);
   ```

   Use `transcripts.unstageSentImages(widget.sessionId)` in the catch,
   **unconditionally** (per the standards' capture-before-await pattern,
   flutter.md § Async & Lifecycle Safety — notifiers outlive the element;
   the dangerous part was `ref`, not the notifier call). `mounted` keeps
   guarding only composer restore, `_pendingImages`/`_queuedPrompts`
   mutations, and the toast.
2. In `_ModeSelector.onSelected` (`:2585-…`), read the client into a local
   before `await _confirmDangerousMode(…)`; keep the existing post-await
   `mounted` check for UI.
3. Sweep the file for the pattern — any `ref.read` that is (a) after an
   `await` and (b) not guarded by `mounted`:
   `grep -n "await" -A3 lib/features/chat/chat_screen.dart | grep -n "ref.read"`
   is noisy; do it by review of the ~12 `ref.read` sites instead. Fix any
   further hits the same way (expected: none beyond the two named).

### Tests

1. Widget test: pump a chat screen with a fake client whose `prompt()`
   completes with an error **after** the screen is popped; stage an image,
   send, pop, complete the error. Assert: no unhandled exception (use
   `FlutterError.onError`/`tester.takeException`), and the fake transcripts
   notifier observed `unstageSentImages` for the session.
2. Regression guard for the mode selector: select a dangerous mode, tear the
   screen down while the confirm dialog is open, confirm → no exception,
   `setMode` still reaches the client (captured pre-await).

### Acceptance

- Test 1 fails pre-fix with the `StateError` surfaced by `takeException`;
  green post-fix. Full suite green.

### Rollback

Local variable captures only.

---

## Phase B1 — M-2: pending pair hints are one attempt's state

**Closes:** M-2

**Files:** `connect_screen.dart`, `test/connect_screen_test.dart`.

### Steps

1. Introduce `_clearPendingPairHints()` clearing `_pendingFingerprint`,
   `_pendingTlsMode`, `_pendingFor`, `_attemptRelayUrl`, `_attemptRelayHostId`,
   `_attemptRelaySpecified`. Replace the body of the fingerprint-clearing in
   `_onHostEdited` (`:74-81`) with a call to it.
2. Call it from every pair-claim failure path (the `catch`/error branches of
   the claim flow around `_applyPair`/`_connect`) — the comment at
   `:244-245` ("Relay hints belong to this pairing attempt") becomes true.
   Success path: hints are consumed then cleared as today.
3. Verify `_shouldUseRelay` consumers receive null relay params for a manual
   connect after a failed QR attempt (`mcremote_client.dart:603-609` then
   falls back to direct/stored-route logic).

### Tests

Extend `connect_screen_test.dart`: apply a QR pair for host A (with relay
tuple), fail the claim, edit the host field to B, connect → the fake client
records `relayUrl == null && relayHostId == null`; and `setRelayRoute` is
never called with A's url under B's authority.

### Acceptance / Rollback

Test red pre-fix, green post; UI-local state only.

---

## Phase B2 — M-3: sign-out always clears the keystore

**Closes:** M-3

**Files:** `settings_store.dart`, `test/settings_store_test.dart`.

### Steps

1. `_clearSecret` (`:548-559`): remove the `_shouldTrySecure` gate — deletes
   always attempt. On delete failure: still remove the prefs fallback, then
   **throw** `SecureStorageUnavailable` (record the failure first via
   `_recordSecureFailure` so the cooldown still dampens subsequent
   reads/writes).
2. `clearAll` (`:585-603`): let the exception propagate after attempting
   *all* clears (collect-then-rethrow: try token, fingerprint, identity;
   if any threw, rethrow the first — a partial clear must not abort the
   rest).
3. Callers audit: connect screen "Clear host & all data" and the Settings
   sign-out must surface a failure ("Couldn't clear saved credentials — try
   again") instead of the success copy. Grep call sites of `clearAll` /
   `clearToken` and wrap with the existing error-toast pattern.

### Tests

1. Cooldown-armed store (inject the test clock, as the existing cooldown
   tests do): `clearToken()` during cooldown still calls the secure
   backend's `delete` (fails pre-fix, where the skip is codified at
   `:645-651` of the test file — update that test's expectation to the new
   contract).
2. Failing delete → `SecureStorageUnavailable` propagates from `clearAll`,
   and the remaining clears were still attempted (spy storage records all
   three delete calls).

### Acceptance

- Updated + new tests green; the old skip-codifying test is rewritten, not
  deleted (it now asserts delete-attempted-despite-cooldown).

### Rollback

Contract change is caller-visible (new throw); both callers updated in the
same commit.

---

## Phase B3 — M-4: asks die with their session

**Closes:** M-4

**Files:** `notification_coordinator.dart`, `chat_screen.dart`,
`sessions_screen.dart`, sign-out call path (settings/connect),
`test/notifications_test.dart`.

### Steps

1. Coordinator: add `dropSessionAsks(String sessionId)` — cancel any shown
   ask notifications for that session (the stable kind-scoped FNV ids make
   them addressable), remove its pending-ask records, and forget presented
   ids. Add `dropAllAsks()` iterating the same.
2. Call `dropSessionAsks` after a **successful** `deleteSession`/`closeSession`
   in both delete paths (`sessions_screen.dart` end-session flow ~`:1059`,
   `chat_screen.dart` `_endSession` ~`:1150`) — after the RPC, not before
   (a failed delete leaves the session and its asks live).
3. Call `dropAllAsks()` on sign-out (`clearAll` call sites) and on
   `disconnect()`-for-signout, not on transient disconnects (the reconcile
   handles those).
4. Leave `_reconcilePendingAsks` untouched — it remains the authority for
   daemon-side closes during an outage.

### Tests

Extend `notifications_test.dart`: pending ask shown → `dropSessionAsks` →
notification cancelled + a later `permission_resolved` for it is a no-op;
sign-out drops all; a *failed* delete drops nothing.

### Acceptance / Rollback

Coordinator-local additions; no protocol change.

---

## Phase B4 — M-5: gap detection over the raw batch

**Closes:** M-5

**Files:** `transcripts_notifier.dart`,
`test/transcript_ingest_test.dart`, `test/history_replay_test.dart`.

### Steps

1. Split `_noteSeq` (`:147-156`): keep the bounds update as
   `_noteSeqBounds(ev)`; move the `_seqGapSuspected` computation into a new
   `_detectGaps(String sessionId, List<SessionEvent> batch)` that walks the
   **raw batch in arrival order** comparing against `_lastSeq` *before* any
   folding, flagging once per real discontinuity.
2. Call `_detectGaps` at batch ingestion (the entry point that currently
   leads into `_foldChunks`), then fold; fold-time calls at `:398`/`:412`
   and the apply-time call become `_noteSeqBounds`.
3. Add `@visibleForTesting bool debugGapSuspected(String sessionId)` beside
   `debugLastSeq`/`debugFirstSeq` (`:138-145`).
4. Re-check the cancel-resync trigger (`chat_screen.dart:990-993`) and the
   `resyncHistory` consumer (`:750-752`) — no changes needed there once the
   flag is truthful.

### Tests

1. `transcript_ingest_test.dart`: the existing OpenCode interleave
   (`[chunk s2, usage s3, chunk s4, usage s5, chunk s6]`, cf. `:92`) must
   leave `debugGapSuspected == false` (red pre-fix).
2. A genuinely gapped batch (`[s2, s5]`) still flags.
3. `history_replay_test.dart`: extend the "resync is a no-op when nothing
   was missed" case (`:376`) to ingest via the chunked/batch path.

### Acceptance / Rollback

Behavioral flag fix; no persisted state.

---

## Phase B5 — M-6: timeout notice per resolution

**Closes:** M-6

**Files:** `transcript_reducer.dart`, `chat_models.dart`,
`test/history_replay_test.dart` or new `test/permission_timeout_test.dart`.

### Steps

1. `ChatItem.system` gains an optional `String? dedupeKey` (persisted with
   the item through the cache codec — check `transcript_cache` codec fields
   and bump/extend tolerantly: absent key decodes to null).
2. `_onPermissionResolved` (`:214-228`): build the notice with
   `dedupeKey: 'perm-timeout:$id'` and dedup by scanning for that key
   instead of the literal text.
3. Confirm the cache codec round-trips the new field
   (`transcript_cache_test.dart` codec case).

### Tests

Two permissions time out in sequence → two notices; replaying both
resolutions (replay flag set) adds no duplicates; codec round-trip
preserves `dedupeKey`.

### Acceptance / Rollback

Additive model field; old cached items decode with null key and simply
never match (worst case: one duplicate notice on first post-upgrade replay
— acceptable).

---

## Phase B6 — M-7/M-8: routes own their text controllers

**Closes:** M-7, M-8

**Files:** `sessions_screen.dart`, `chat_screen.dart`,
`test/sessions_screen_test.dart` (or new), question-sheet test.

### Steps

1. Rename dialog (`sessions_screen.dart:222-261`): extract a private
   `_PromptTextDialog` `StatefulWidget` (initial text, label, validator)
   that owns and disposes its `TextEditingController` in its own `dispose()`
   — the pattern `_createSessionFlow` documents at `:308-312`. The caller
   awaits `showDialog<String>` and receives the submitted text; it no longer
   holds a controller.
2. Question sheet (`chat_screen.dart:1610-1656`): move `customControllers`
   into the sheet's stateful body widget; dispose there. The sheet's result
   carries the answer strings, so the caller needs no controller access
   after pop.
3. Sweep both files for any remaining `controller.dispose()` immediately
   after an awaited route: `grep -n "dispose()" …` and review — expected
   remaining: none of this pattern.

### Tests

Widget tests that submit with an **active IME composition**:
`tester.enterText` then set
`tester.testTextInput.updateEditingValue(… composing: TextRange(…))`, tap
Save/Submit, `await tester.pumpAndSettle()` — pre-fix this trips the
disposed-notifier `FlutterError` via `takeException`; post-fix clean, value
delivered.

### Acceptance / Rollback

Widget-local refactors; no behavior change on the happy path.

---

## Phase B7 — M-9: wire the FAB's force jump

**Closes:** M-9

**Files:** `chat_screen.dart`, widget test.

### Steps

1. FAB handler (`:2155-2160`): call `_scrollToEnd(force: true)` and move the
   `_userNearBottom.value = true` / `_unreadWhileScrolledUp.value = 0`
   mutations to after the jump is queued (same synchronous block is fine —
   the point is they no longer precede an aborted jump).
2. Grep for other intended-force call sites (`chat-open`? — no: chat-open
   has no gesture to fight; leave unforced).

### Tests

Widget test: start a fling (`tester.fling`), tap the FAB mid-activity,
settle → scroll offset 0, unread indicator cleared, FAB hidden. Pre-fix the
offset stays non-zero while unread is already wiped.

### Acceptance / Rollback

One call site + ordering.

---

## Phase C1 — transport lows (L-1, L-2, L-3, L-4)

**Files:** `mcremote_client.dart`, `mc_exception.dart`,
`relay_transport.dart`, `test/mcremote_client_test.dart`,
`test/relay_path_test.dart`.

### Steps

1. **L-1**: capture `final epoch = _connectEpoch;` at ping-send and at
   `probeLiveness` entry; in the failure/`catchError` handlers compare
   before touching state or scheduling (`:1141-1156`, `:1171-1185`). Stop
   incrementing `_missedPings` for failures whose `code == 'connection_lost'`
   raised by intentional teardown (`_failAllPending` calls) — key off the
   epoch mismatch, which now covers it.
2. **L-2**: in `_teardownSocket` (`:1352-1385`), move the pending-map
   snapshot into the synchronous detach section:
   `final pending = Map.of(_pending); _pending.clear();` alongside the
   `_sub/_channel/_httpClient/_relayTransport` detaches; after the awaits,
   fail the **snapshot** only.
3. **L-3** (`mc_exception.dart:43-70`):
   - `pair_error`: `permanent: code != 'rate_limited'` at minimum; align the
     full set with `docs/protocol-v1.md:739` (only `client_key_*` and the
     doc's explicit permanent codes stay permanent).
   - `auth_error`: `permanent` only for `invalid_token`, `bad_version`,
     `unauthorized`, `client_key_*`; generic `auth_failed` → transient.
   - `error`-typed replies: extract `payload['message']`/`['code']` when
     present (daemon's over-limit auth reply is this shape,
     `internal/ws/server.go:699`), map transient. `_failHandshake` already
     counts non-permanent failures toward the parked cap (`:1106`), so
     repeat offenders still park.
4. **L-4** (`relay_transport.dart`): in `_replacePeer`/peer wiring, attach
   `onDone`/`onError` on the peer subscription that close the transport
   (owner semantics: peer gone ⇒ tunnel down); handle `_peer!.done`
   rejections by `unawaited(_peer.done.catchError(...))` or by guarding
   writes with a `_peerClosed` flag; delete the dead `prev` local.

### Tests

- L-2: deterministic unit — start teardown against a paused fake await,
  adopt a new socket with a pending completer, resume teardown → completer
  untouched.
- L-3: table test over `(type, code) → permanent` incl. the `error`-typed
  message extraction; client-level: a `pair_error rate_limited` leaves
  `_autoReconnect` true.
- L-1/L-4: unit-level where the seams allow; otherwise assert via the
  existing fake-transport tests (no state flap on connect-over-connected).

## Phase C2 — notification lows (L-5, L-12) — after B3

**Files:** `notification_service.dart`, `notification_coordinator.dart`,
`chat_screen.dart` or `sessions_screen.dart` (nav guard),
`test/notifications_test.dart`.

### Steps

1. **L-5**: make `init()` retryable — on failure store the error, leave
   `_ready` false, and have `show()`/`schedule()` re-attempt `init()` once
   per call window (e.g. not more than every 30 s) before giving up
   silently; expose `lastInitError` for the Settings screen row ("Alerts
   unavailable: …").
2. **L-12**: `_watchStack` becomes a `List<String>` allowing duplicates
   (`:57-68`); `claimSession` pushes unconditionally; `releaseSession`
   removes **one** occurrence (`lastIndexOf` + `removeAt`). "Watched" =
   list contains id. Additionally guard the sessions-row double-push
   (`sessions_screen.dart:1415-1422`): ignore taps while a push for the
   same id is in flight (`_navBusy` flag cleared on return).

### Tests

Coordinator: claim twice, release once → still watched (suppression holds);
release twice → unwatched. Service: failing init, then a later `show()`
after the backend recovers → notification delivered.

## Phase C3 — pipeline lows (L-6, L-7)

**Files:** `transcripts_notifier.dart`, `chat_models.dart`,
`test/history_replay_test.dart`.

### Steps

1. **L-6**: in `hydrateFromCache`'s keep-live merge (`:261-281`), carry
   `usage: live.usage ?? seeded.usage` exactly as `modes`/`currentModeId`
   are carried (precedent test `history_replay_test.dart:262`).
2. **L-7**: in the `sessionTranscriptProvider` family (`:818-821`), return a
   canonical shared empty instance for absent ids
   (`static final SessionTranscript empty = SessionTranscript(…)` or a
   per-notifier cached map of empties), so identical absence is identical
   (`identical()` short-circuits the provider's equality). Do **not**
   hand-write `==` over the whole transcript (hot path).

### Tests

- Hydrate race: seed cache, deliver a `usage_update` mid-load → indicator
  survives (red pre-fix, mirrors the modes test).
- Provider identity: two reads for an absent id across a commit of another
  session are `identical`; a listening widget rebuild count stays flat.

## Phase C4 — UI lows (L-9, L-10, L-11)

**Files:** `chat_screen.dart`, `sessions_screen.dart`,
`top_notification.dart`, `test/top_notification_test.dart`.

### Steps

1. **L-9**: add `bool _endingSession = false;` guard around chat-screen
   `_endSession` (`:1109-1164`) — set before the confirm dialog resolves the
   action, clear in `finally`; menu item disabled/ignored while true
   (mirror `_endingIdBusy`).
2. **L-10**: in sessions `_refresh` (`:155-177`), merge: snapshot rows whose
   id received an event after the RPC started keep the event status. The
   listener (`:84-111`) already tracks per-id updates — record
   `_statusOverrides[id] = (status, at)` on event, and when applying a
   snapshot, prefer overrides newer than the refresh start time; clear
   overrides the snapshot confirms.
3. **L-11**: `Dismissible.onDismissed` (`top_notification.dart:174-177`)
   removes the overlay entry immediately (call the removal path directly
   instead of `_dismiss()`'s `reverse()`); ensure the queue advances without
   the extra ~550 ms.

### Tests

- L-11: extend `top_notification_test.dart` — after a swipe-dismiss, the
  next queued toast appears within one pump of the collapse (no 550 ms
  wait), and no `FlutterError` when a rebuild happens post-dismiss (inset
  change simulation).
- L-9/L-10: widget tests with a slow fake client — double end-session fires
  one delete; a `turn_complete` during `_refresh` wins over the stale
  snapshot.

## Phase C5 — Go/protocol hygiene (L-13, I-1, I-2)

**Files:** `internal/provider/provider.go`, `internal/event/event.go`,
`internal/ws/server.go`, `docs/protocol-v1.md`, Go tests.

### Steps

1. **L-13**: ``UpdatedAt time.Time `json:"updated_at,omitzero"` ``
   (`provider.go:312`). Sweep for siblings:
   ``grep -rn 'time.Time `json:"[^"]*,omitempty"' internal/`` (excluding
   `_test` files) — fix every wire-facing hit the same way. Unit test:
   marshal a zero `UpdatedAt` → key absent.
2. **I-1**: correct the `event.go:387-389` comment (a clear **omits**
   `entries`); add a "plan event" note to `docs/protocol-v1.md`: absent
   `entries` ⇒ replace-with-empty (clear), explicitly contrasted with
   `session_mode`'s absent⇒keep-current merge.
3. **I-2**: in `handleSessionPendingAsks` (`server.go:588`), read
   `c.deviceID` under `s.mu` (match `handlePermissionRespond`, `:922`).
4. Go gate: `make pre-add-check` on all touched files.

### Acceptance

`go test ./internal/...` green; marshal test red pre-fix for L-13; the Dart
side needs no change (its `updated_at` parse already tolerates absence —
`models.dart:122-129`).

---

## Phase V — full verification

1. `cd apps/mobile && dart format --set-exit-if-changed lib test` (touched
   files must already be formatted per-phase; this is the belt-and-braces
   pass).
2. `flutter analyze` — zero issues.
3. `flutter test` — full suite green, including every new regression test
   from A1…C4. Expected count: 408 + ~25 new.
4. `go build ./... && go test ./internal/...` and the Go pre-add gate for
   C5's files.
5. `make preflight`.
6. Checklist against MADR 0046 §1: every ID maps to a landed phase and a
   named test; any deliberate deferral is recorded in the MADR (edit its
   Status line), not silently dropped.
7. Manual smoke (device or emulator, per `docs/mobile-profiling.md` setup):
   - pair, kill daemon, watch reconnect backoff continue past the first
     failure (H-A);
   - scan a second daemon's QR, cancel, reconnect to the first (H-B);
   - long session list churn does not grow SharedPreferences (H-C — inspect
     via `adb shell run-as … cat shared_prefs`).

Done means: all findings closed or explicitly deferred in the MADR, suite
green, gates clean.
