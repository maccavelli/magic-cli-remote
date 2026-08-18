---
status: draft
date: 2026-08-18
madr: "0101-MADR-android-agent-alert-delivery.md"
owner: Project Owner
target: next app + daemon release
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Make Android agent alerts survive their own lifecycle, and make their absence diagnosable

Associated MADR:
[0101-MADR-android-agent-alert-delivery.md](0101-MADR-android-agent-alert-delivery.md)

## Objective and Scope

Implement the five parts of MADR 0101: uniform `timed_out` on the wire (B),
tombstones for asks that expire unattended (A), per-channel OS state in
Settings (C), an on-device test path per alert kind (D), and error bodies that
carry the error (E).

**Done means:** an ask that expires while its notification sits unanswered in
the shade leaves a passive, tappable "timed out" notification instead of
vanishing; all five providers mark expiry resolutions identically (the MADR F5
matrix reads ✅ in every row); a per-channel OS block is reported next to the
specific toggle it silences; each per-kind row can fire a real test
notification on the user's own device; and an error notification's body shows
the classified error message. All confirmed per MADR §Confirmation C1–C7,
including a live emulator pass.

**In scope**

| Area | Files |
|---|---|
| Daemon: expiry marking (B) | `internal/provider/httpagent/session.go`, `internal/provider/acpagent/session.go`, `internal/provider/acpagent/extensions.go`, `internal/provider/httpagent/expiry_test.go`, `internal/provider/acpagent/session_test.go`, `internal/provider/acpagent/extensions_test.go` |
| Protocol docs (B) | `docs/protocol-v1.md` (question `timed_out`, additive) |
| Coordinator: tombstones + error body + test guard (A, D, E) | `apps/mobile/lib/data/notifications/notification_coordinator.dart` |
| Notification service: tombstone + channel probe + test sends (A, C, D) | `apps/mobile/lib/data/notifications/notification_service.dart`, `apps/mobile/lib/data/notifications/agent_notifications.dart` |
| Settings UI (C, D) | `apps/mobile/lib/features/settings/settings_screen.dart` |
| Mobile tests | `apps/mobile/test/notifications_test.dart`, `apps/mobile/test/settings_screen_test.dart` |

**Out of scope, deliberately**

* **`permission_timeout_seconds` defaults.** Widening the actionable window is
  host configuration; the MADR records the trade-off. A `docs/config.md`
  sentence may ride along with Phase 5, nothing more.
* **iOS.** No channels, no background socket (MADR 0067 D2). Part C returns
  null / renders nothing on iOS; parts A, D, E apply unchanged where iOS
  notifications already work (foreground-adjacent lifecycles).
* **F2 behavior.** Engine-side auto-approval is working-as-designed; part C's
  UI work carries one line of settings copy, no behavior change.
* **The plugin `requestCode` overflow** (MADR §noted findings) — upstream.
* **The FGS/`host_connection` channel.** Blocking it is survivable (the
  service still runs); folding it into part C's warning is a follow-up if a
  report ever implicates it.

## Prerequisites and Dependencies

### Facts verified in the working tree, 2026-08-18

| Ref | File:lines | Fact |
|---|---|---|
| B1 | `httpagent/session.go:1167-1190` | `expirePermission` emits `permission_resolved` with `Status: cancelled`, **no** `TimedOut` |
| B2 | `httpagent/session.go:1217-1239` | `expireQuestion` emits `question_resolved`, **no** `TimedOut` |
| B3 | `acpagent/session.go:1306-1316` | `permissionResolved(permID, status, deviceID, optionID)` helper — no TimedOut parameter |
| B4 | `acpagent/session.go:1949-1966` | permission timeout arm (the `case <-timeout:` branch); its emit at `:1964` calls the helper with `cancelled, "", ""`. Two other cancel-path callers (`:1889`, `:1947`) are NOT timeouts and stay unmarked |
| B5 | `acpagent/extensions.go:325-338` | question timeout arm emits via `questionResolvedEvent(qID, cancelled)` — no TimedOut |
| B6 | `acphttp/session.go:1396`, `codex/session.go:1719` | the two conformant emitters, for signature reference |
| B7 | `docs/protocol-v1.md:1104-1108` | `timed_out` on `permission_resolved` is already contract; `:926` documents `question_resolved` without it |
| A1 | `notification_coordinator.dart:152-156` | any resolution → `_knownAsks.remove` + cancel; `ev.timedOut` ignored |
| A2 | `models.dart:1395,1488,1648` | `SessionEvent.timedOut` parsed from `timed_out` — no model change needed |
| A3 | `models.dart` `SessionEvent` | has **no** `deviceId` field; keying part A on `timedOut` alone is equivalent per B7's contract (timeout ⇔ `timed_out: true`; human answers never set it) and avoids a model change |
| A4 | `agent_notifications.dart:129-146` | `notificationIdFor(kind, sessionId, requestId)` — reusing the id makes the tombstone replace the actionable notification atomically |
| C1 | `notification_service.dart:24-26` | channel ids `approval_needed` / `agent_done` / `agent_error` are private constants |
| C2 | plugin `platform_flutter_local_notifications.dart:584` | `getNotificationChannels()` exists on the Android implementation; returns `AndroidNotificationChannel` with `.importance` |
| C3 | `settings_screen.dart:1043-1058` | the app-level `_osBlocked` warning row — the visual pattern part C mirrors per-channel |
| D1 | `app.dart:237` | `onOpenSession → router.go('/sessions/$id')` — a test tap must not navigate to a fake session |
| D2 | `notification_coordinator.dart:186-218` | `_onResponse` allow/deny path calls `respondPermission` — a test payload must not reach the daemon |
| E1 | `notification_coordinator.dart:174-181` | `showError(..., detail: ev.text)`; daemon `TypeError` carries the message in `error` (`event.go:446`) |
| T1 | `httpagent/expiry_test.go:75,104` | `TestPermissionExpiryRejectsAndNotifies` / `TestQuestionExpiryRejectsAndNotifies` — the natural home for B assertions |
| T2 | `test/notifications_test.dart` (647 lines) | coordinator harness with fake service already exists (`pending asks` group) |
| T3 | `test/settings_screen_test.dart` | full widget harness with `_FakeStore` and provider overrides |

### Environment

* Go toolchain per `go.mod`; `make pre-add-check` before every `git add`.
* Flutter/Dart: `flutter analyze` + `dart format` gate (AGENTS.md — CI fails
  on one unformatted file); `flutter test` for `apps/mobile`.
* Emulator verification (Phase 6): AVD `mcremote_test` (API 36), this host's
  live daemon at the tailnet address. Build gotchas measured this session:
  Gradle must run on JDK 21 (`org.gradle.java.home` is now pinned in
  `~/.gradle/gradle.properties`); if a JDK-26 daemon ever poisons the
  `androidJdkImage` transform again, kill the daemons and delete
  `~/.gradle/caches/9.1.0/transforms/3c6040*`. The emulator app retains
  pairing credentials — no camera step needed.

### Blocking dependencies

Part A's tombstone keys on `ev.timedOut`, which three of five providers do not
send until part B lands. **Phase 1 (B) must merge before Phase 2 (A) is
verifiable end-to-end**; unit tests for A can land in the same commit stack
regardless (the fake feeds `timedOut: true` directly).

## Technical Design

### 1. Part B — uniform `timed_out` on expiry (daemon)

Four emission sites change; no signatures on the wire, one field added.

**httpagent** (covers opencode + kilo):

* `expirePermission` (B1): add `TimedOut: true` to the `TypePermissionResolved`
  event.
* `expireQuestion` (B2): add `TimedOut: true` to the `TypeQuestionResolved`
  event.

**acpagent** (covers grok):

* Extend the helper (B3) rather than inlining, so every future caller makes an
  explicit choice:

  ```go
  func (s *session) permissionResolved(permID, status, deviceID, optionID string) event.Event
  func (s *session) permissionExpired(permID string) event.Event   // new: cancelled + TimedOut
  ```

  The timeout arm (B4) switches to `permissionExpired`. The non-timeout
  `cancelled` callers (agent-side abandonment, close) are untouched — the
  contract (B7) is explicit that only the timer expiry sets the flag.
* `questionResolvedEvent` gains the same treatment: a `questionExpiredEvent`
  used only by the timeout arm (B5).

**Docs:** `protocol-v1.md` — extend the `question_resolved` row (`:926`) and
the `timed_out` bullet (`:1104`) to state the flag appears on both resolution
types, same semantics. Additive; old clients ignore it.

**Tests** (T1 + acpagent):

| Test | Asserts |
|---|---|
| `TestPermissionExpiryRejectsAndNotifies` (extend) | the resolved event has `TimedOut == true` |
| `TestQuestionExpiryRejectsAndNotifies` (extend) | same for questions |
| `TestPermissionAnsweredBeforeExpiryIsNotRejected` (extend) | the answered path's resolution has `TimedOut == false` |
| acpagent: new `TestPermissionTimeoutMarksTimedOut` | drive the timeout arm; event carries `TimedOut: true`, empty `DeviceID` |
| acpagent: new `TestQuestionTimeoutMarksTimedOut` | same for the question arm |
| acpagent: existing cancel-path tests | unchanged and still asserting no `TimedOut` (add the assertion where absent) |

### 2. Part A — tombstones for unattended expiry (coordinator + service)

**Rule.** In `_onEvent`'s resolution branch (A1), the behavior forks on two
conditions captured *before* mutation: `hadNotification =
_shownAsks.remove(key)` and `expired = ev.timedOut`:

| hadNotification | expired | Behavior |
|---|---|---|
| true | true | **tombstone**: replace the notification in place |
| true | false | cancel (today's behavior — a human or the agent resolved it) |
| false | any | nothing to retire (today's behavior) |

`_knownAsks.remove` happens in all cases. `dropSessionAsks`, `dropAllAsks`,
`dispose`, and the reconcile-stale path keep today's cancel — those are
session-close and sign-out flows where MADR D4 wants removal, and the pending
snapshot carries no resolutions to fork on anyway.

**Service.** One new method, mirroring the show/cancel family:

```dart
/// Replace an ask's actionable notification with a passive expiry notice on
/// the same channel and id (MADR 0101 A). No actions: the ask is dead and a
/// stale Allow/Deny must not exist (0046 M-4). Tap opens the session.
Future<void> showAskExpired({
  required NotifKind kind,          // permission | question
  required String sessionId,
  required String requestId,
  required String sessionLabel,
}) async
```

* Same id via `notificationIdFor(kind, sessionId, requestId)` (A4) — the
  replacement is atomic, no flicker, no second slot.
* Channel `approval_needed` (same as the ask it replaces), `Importance.high`
  on the channel but the *notification* posts with
  `Priority.defaultPriority` — the moment has passed; it should sit in the
  shade, not peek.
* Title `Permission timed out` / `Question timed out`; body
  `'$sessionLabel · the agent stopped waiting'`.
* Payload: `NotifPayload(kind: kind, sessionId: sessionId)` — **no**
  `permissionId`/`questionId`, so every tap decodes to the plain open path
  (D2 branch `pid == null → onOpenSession`).
* `autoCancel` true; no `timeoutAfter`.

**Coordinator** calls it with `_labelFor(key.$2)`; the switch in `_showAsk` /
`_cancelAsk` stays exhaustive over `NotifKind` (compile-time guarantee the
turnComplete/error arms remain no-ops).

**Tests** (T2 harness):

| Test | Asserts |
|---|---|
| `TestExpiredShownAskLeavesTombstone`* | request → shown; resolution with `timedOut: true` → fake service records `showAskExpired` on the same id, no `cancel` |
| `TestHumanResolvedAskCancels` | resolution with `timedOut: false` → cancel, no tombstone (C2) |
| `TestUnshownExpiryIsSilent` | ask suppressed by watching, resolution with `timedOut: true` while still watching → neither cancel nor tombstone |
| `TestQuestionExpiryTombstone` | same fork for `question_resolved` |
| `TestSessionCloseStillCancels` | `dropSessionAsks` unchanged: cancel, never tombstone |
| `TestTombstonePayloadOpensSession` | decoded payload has null request ids → open path |

\* Dart test names are `test('...')` strings; the table names describe them.

### 3. Part C — per-channel OS state in Settings

**Constants.** The three channel ids move to public constants on
`NotificationService` (`kPermissionChannelId` etc.) — they are Android
concepts, so they stay in the service, not `agent_notifications.dart`.

**Service probe**, next to `areNotificationsEnabled()`:

```dart
/// Channel ids the OS currently refuses to display (importance == none).
/// Null when the platform has no channels (iOS, tests, desktop).
Future<Set<String>?> blockedChannelIds() async
```

Implementation: Android impl's `getNotificationChannels()` (C2), collect ids
whose `importance == Importance.none`. Wrapped in the same best-effort
try/catch as every plugin call; null on failure. Lowered-but-nonzero
importance is *not* reported — a silenced channel still displays, and
conflating "quiet" with "blocked" would make the warning cry wolf.

**Coordinator** re-exports it (`osBlockedChannels()`), keeping Settings
plugin-free like the existing `osBlocked()`.

**Settings UI.** In `_load` (and after `_setNotifications(true)`), fetch the
set alongside `_osBlocked`. Under each per-kind `SwitchListTile` whose channel
is in the set, render the C3 warning pattern:

> ⚠ *Android is blocking this category* — Allow "Approval needed" for Magic
> CLI Remote in system notification settings.

Channel↔toggle map: asks → `approval_needed`, turn complete → `agent_done`,
errors → `agent_error`. The app-level `_osBlocked` row is unchanged and takes
precedence (when the whole app is blocked, per-channel rows are suppressed —
one cause, one message). One settings-copy line lands on the asks row's
subtitle: *"Asks fire only for actions the agent's own config doesn't
auto-approve."* (MADR F2 note.)

**Tests** (T3 harness): fake coordinator/service returns
`{'agent_error'}` → the Errors row shows the warning, the other rows do not;
returns null → no rows (iOS); app-level blocked → per-channel rows suppressed.

### 4. Part D — a test notification per kind

**Sentinel.** `agent_notifications.dart` gains
`const kTestNotificationSessionId = '_notif-test'`. `NotifPayload.decode`
already requires a non-empty sessionId; the sentinel is a legal payload.

**Coordinator.**

```dart
/// Fire a real notification through the full platform path with sample
/// content, so the user can validate channel/icon/actions on their own
/// device (MADR 0101 D). Bypasses `shouldNotify` on purpose — the point is
/// the display path, not the routing rules.
Future<void> sendTestNotification(NotifKind kind) async
```

* `NotifKind.permission` → `showPermission(sessionId: sentinel, permissionId:
  'test', toolName: 'test tool', detail: 'This is a test — Allow and Deny '
  'only dismiss it.', allowOptionId: null)`. `allowOptionId: null` means the
  Allow tap takes the existing no-option fallback (open) rather than sending
  a respond (D2 guard below).
* `turnComplete` / `error` → their show methods with a "Test" session label
  and, for error, a sample detail.
* `question` is exercised by the permission test (same channel); no fourth
  button.

**The guard (D1/D2).** First lines of `_onResponse`:

```dart
if (p.sessionId == kTestNotificationSessionId) {
  // A test notification proves the display path; its taps must neither
  // navigate nor reach the daemon.
  unawaited(_notifs.cancelPermission(p.sessionId, p.permissionId ?? 'test'));
  return;
}
```

This intercepts open, allow, and deny alike, and also covers a cold-start
launch replay (`takeLaunchResponse`) carrying the sentinel.

**Settings UI.** Each per-kind `SwitchListTile` gets
`secondary: IconButton(icon: notification-test glyph, tooltip: 'Send test
notification', onPressed: master && kind enabled ? ... : null)` calling
through the coordinator. Disabled exactly when the switch is (same
`_notifications` gate), so the button always tests what the current settings
would actually deliver.

**Tests:** coordinator unit tests — sentinel taps never call
`respondPermission` or `onOpenSession` (a recording fake for both);
`sendTestNotification(permission)` reaches the fake service with the sentinel
session id. Settings widget test — buttons disabled when master off; tap
calls the coordinator (fake records it).

### 5. Part E — error bodies carry the error

`notification_coordinator.dart` (E1): `detail: ev.error ?? ev.text`. The
daemon clips classified messages to 400 chars (`agenterr.Present` /
`clip(msg, 400)`), and Android truncates the body line; no client-side clip.
Test: coordinator fake receives the `error`-field text when both fields are
present, and `text` when only it is (notice-shaped events never reach
`showError`, but the fallback keeps the old behavior for any provider that
populated `text`).

## Execution Phases

Each phase is one commit, `make pre-add-check` (Go) and
`dart format` + `flutter analyze` + `flutter test` (Dart) green before it.

---

### Phase 1 — Part B: daemon conformance

1. httpagent: `TimedOut: true` at B1 and B2.
2. acpagent: `permissionExpired` / `questionExpiredEvent` helpers; timeout
   arms switched (B4, B5); non-timeout callers untouched.
3. Extend/add the six tests from §1.
4. `docs/protocol-v1.md`: question `timed_out` documented (additive).
5. `go test ./internal/provider/...` green.

**Exit criterion:** the MADR F5 matrix reads ✅ in all five rows, proven by
tests naming each transport.

---

### Phase 2 — Parts A + E: coordinator behavior

1. `showAskExpired` in the service; tombstone fork in `_onEvent` per §2.
2. `detail: ev.error ?? ev.text` (§5).
3. The seven coordinator tests (§2 table + E test) against the existing fake
   harness (T2).
4. All existing notification tests unchanged and green — the fork must be
   invisible to every non-timeout path.

**Exit criterion:** `flutter test test/notifications_test.dart` green,
including the pre-existing groups.

---

### Phase 3 — Part C: channel diagnostics

1. Public channel-id constants; `blockedChannelIds()` probe; coordinator
   re-export.
2. Settings: per-toggle warning rows, precedence rule, F2 copy line.
3. Widget tests per §3.

**Exit criterion:** on the emulator, `adb shell` blocking the `agent_error`
channel (Settings → Apps → notifications, or `cmd notification` where the
image supports it) makes the Errors row warn; unblocking clears it on next
screen load.

---

### Phase 4 — Part D: test path

1. Sentinel constant; `sendTestNotification`; `_onResponse` guard.
2. Settings `secondary` buttons wired and gated.
3. Coordinator + widget tests per §4.

**Exit criterion:** on the emulator, all three buttons produce shade
notifications on their channels; tapping the test permission's Allow neither
navigates nor errors (logcat clean).

---

### Phase 5 — Docs

1. `docs/config.md` (or the settings copy already landed in Phase 3): one
   sentence that `permission_timeout_seconds` bounds the actionable window of
   an ask notification.
2. MADR 0101 §Confirmation rows updated with the test names that pin them.

**Exit criterion:** no doc claims alerts persist longer than the host's
configured window.

---

### Phase 6 — Live verification (emulator)

Re-run the five verified-good scenarios from the MADR evidence table
(regression, C7) plus the new behaviors:

1. Ask arrives backgrounded → let it expire (120s) → **tombstone** replaces
   the actionable notification on the same id; tap opens the session (C1).
2. Ask answered from the shade within the window → cancel, no tombstone (C2).
3. Ask expires on a kilo session *and* a grok session — flag now uniform (C3
   live half; goose/opencode by transport-sharing argument, codex already ✅).
4. Block `agent_error` at the OS → Settings warns (C4); unblock → clears.
5. Three test buttons deliver (C5).
6. Engine-kill error repro → body shows the classified message (C6).
7. Record results in the MADR §Confirmation table; flip MADR status to
   `accepted`.

**Exit criterion:** C1–C7 all observed; screenshots/dumpsys receipts captured
in the session (not committed — 0099 precedent of not committing raw logs).

## Verification

### Commands

```bash
make pre-add-check                                  # staged Go files
go test ./internal/provider/httpagent/... ./internal/provider/acpagent/... \
        ./internal/provider/kilo/... ./internal/provider/opencode/... \
        ./internal/provider/grok/... ./internal/provider/acphttp/...
cd apps/mobile
dart format --output=none --set-exit-if-changed .   # CI's first failure mode
flutter analyze
flutter test
```

### Acceptance

| # | Criterion (MADR ref) | Proof |
|---|---|---|
| V1 | Expiry marked on every provider (C3, F5) | Phase 1 tests; Phase 6.3 live |
| V2 | Tombstone on unattended expiry, same id, no actions (C1) | Phase 2 tests; Phase 6.1 dumpsys `actions=0` |
| V3 | Human-resolved asks cancel as today (C2, D4) | Phase 2 tests; Phase 6.2 |
| V4 | Session close / sign-out never tombstone (D4) | Phase 2 `TestSessionCloseStillCancels` |
| V5 | Blocked channel warned per-toggle; iOS/null silent (C4) | Phase 3 widget tests; Phase 6.4 |
| V6 | Test buttons deliver and their taps are inert (C5) | Phase 4 tests; Phase 6.5 |
| V7 | Error body carries the message (C6) | Phase 2 test; Phase 6.6 |
| V8 | Verified-good paths unregressed (C7) | Phase 6 re-run of the MADR evidence table |

### Cross-version behavior (stated, not assumed)

* **New app + old daemon:** the three unmarked providers still omit
  `timed_out` → the tombstone fork never triggers → exactly today's cancel
  behavior. No error, no misbehavior — just the old UX until the daemon
  updates.
* **Old app + new daemon:** the extra field is parsed and ignored
  (`models.dart:1648` predates this plan). No change.
* Parts C/D/E are app-only and version-independent.

## Rollout and Rollback

**Rollout.** Phase 1 (daemon) can ship in any daemon release independently —
it is a conformance fix with no behavior visible until the app updates.
Phases 2–4 ship together in the next app build (they share the settings
screen and coordinator). No migration, no persisted-format change; the only
new persisted surface is nothing — test/tombstone state lives in the
notification shade.

**Rollback.**

* Phase 1: revert restores the (non-conformant) prior wire shape; the new app
  degrades gracefully per §Cross-version.
* Phase 2: revert restores cancel-on-any-resolution; tombstone tests revert
  with it.
* Phases 3–4: pure UI additions; revert removes the rows/buttons.
* No part changes data at rest; per-host rollback is "install the previous
  APK" (in-app updates, 0065 P4).

## Task Checklist

**Phase 1 — daemon conformance (B)**

- [ ] `httpagent.expirePermission` sets `TimedOut: true`
- [ ] `httpagent.expireQuestion` sets `TimedOut: true`
- [ ] `acpagent.permissionExpired` helper; timeout arm switched
- [ ] `acpagent.questionExpiredEvent`; timeout arm switched
- [ ] six tests (§1 table) green; non-timeout paths assert `TimedOut == false`
- [ ] `protocol-v1.md` question `timed_out` documented
- [ ] `make pre-add-check` clean

**Phase 2 — coordinator (A, E)**

- [ ] `showAskExpired` (same id, same channel, no actions, open-only payload)
- [ ] `_onEvent` fork per the §2 table; drop/close/sign-out paths untouched
- [ ] `detail: ev.error ?? ev.text`
- [ ] seven new tests; all pre-existing notification tests unchanged
- [ ] `dart format` / `flutter analyze` / `flutter test` clean

**Phase 3 — channel diagnostics (C)**

- [ ] public channel-id constants
- [ ] `blockedChannelIds()` (importance == none only; null off-Android)
- [ ] per-toggle warning rows + app-level precedence
- [ ] F2 copy line on the asks row
- [ ] widget tests (blocked / null / precedence)

**Phase 4 — test path (D)**

- [ ] `kTestNotificationSessionId` sentinel
- [ ] `sendTestNotification(kind)`; permission test carries no allowOptionId
- [ ] `_onResponse` sentinel guard (open/allow/deny + launch replay)
- [ ] Settings `secondary` buttons, gated with the switches
- [ ] coordinator + widget tests

**Phase 5 — docs**

- [ ] timeout-window sentence in config/settings copy
- [ ] MADR Confirmation rows name their tests

**Phase 6 — live verification**

- [ ] C1 tombstone observed on-device (dumpsys: same id, `actions=0`)
- [ ] C2 answered-in-time cancels
- [ ] C3 expiry marked live on two transports (kilo + grok)
- [ ] C4 blocked-channel warning appears and clears
- [ ] C5 three test buttons deliver
- [ ] C6 error body carries message
- [ ] C7 MADR evidence table re-run green
- [ ] MADR status → `accepted`
