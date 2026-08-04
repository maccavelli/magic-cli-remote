# MADR 0067 — Implementation plan: iOS port of the mobile companion app

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: In progress. P0 partially pre-satisfied by commit `14bdbd0`
  (iOS shell scaffolded, simulator build verified 2026-08-03) and the
  target/Podfile work in `eb8c990`; pods for all 9 plugins installed
  2026-08-03. **P1 implemented 2026-08-03** (U1/U2 green: `flutter analyze`
  clean, 691 tests passing). P0 remainder (entitlements, signing doc),
  P2–P5 outstanding.
- **Date**: 2026-08-03
- **Scope**: `apps/mobile` only (Dart, `ios/` runner, tests), plus
  `docs/standards/mobile/` and `docs/ops-hardware-validation.md` Part F.
  No Go changes.
- **Source**: [0067-MADR-ios-port.md](0067-MADR-ios-port.md)

---

## 0. Grounding — where every change lands

Seams that already exist (the plan adds iOS branches inside them, never
new cross-cutting paths):

| Seam | Location | Current consumer |
| --- | --- | --- |
| Foreground-service gate | `lib/data/notifications/foreground_service.dart:29` | already no-ops on iOS — stays |
| Backgrounding policy | `lib/app_lifecycle.dart:135-160` (`_onBackground`) | early-returns when notifications enabled — gains iOS branch (D2) |
| Background maintenance timer | `lib/data/notifications/notification_coordinator.dart:291-328` | armed when `!appForegrounded` — gated off on iOS (D2) |
| Notification init | `lib/data/notifications/notification_service.dart:56-58` | Android-only settings — gains Darwin settings + categories (D4) |
| Notification presentation | `notification_service.dart:170,204,235,291` | Android-only details — gains per-kind Darwin details (D4) |
| Permission plumbing | `notification_service.dart:314-338` → `notification_coordinator.dart:459-463` | Android plugin only — gains iOS resolution (D4) |
| Secure storage construction | `lib/data/local/settings_store.dart:58-62` | `aOptions` only — gains `iOptions` (D5) |
| Credentials-expected probe | `settings_store.dart:120` + 0066 probe path | detects loss — gains reinstall-inversion detection (D5) |
| Plaintext guard | `lib/features/connect/connect_screen.dart:1103-1117` | `Platform.isAndroid` — extends to iOS (D7) |
| Theme platform pin | `lib/theme/celestial.dart:251,415` | forces android — becomes adaptive (D7) |
| Native shell | `ios/Runner/SceneDelegate.swift` (empty) | gains privacy overlay (D7) |

Notable non-facts (checked, not assumed):

- No MethodChannels anywhere; no `path_provider`/file I/O; no mDNS in app
  or daemon — none of the usual porting surfaces exist here.
- ATS never engages (all networking is dart:io) — there is no
  `NSAppTransportSecurity` work in any phase.
- `connectivity_plus` VPN-as-`other` on Apple is already handled
  (`lib/app_lifecycle.dart:105-108`).

## P0 — Toolchain: make the iOS shell real

1. `flutter pub get && pod install` so `Podfile.lock` covers all 9 plugin
   pods (today: only `Flutter` + `flutter_foreground_task`).
2. ~~Uncomment and set the Podfile platform to match the deployment
   target.~~ **Done 2026-08-03**: target decided as iOS 16.0 (MADR D1/F2);
   `platform :ios, '16.0'` set in `ios/Podfile` and
   `IPHONEOS_DEPLOYMENT_TARGET = 16.0` in all three `project.pbxproj`
   configurations.
3. Create `ios/Runner/Runner.entitlements` (Debug + Release) with
   `keychain-access-groups` (needed by P3 before any secure-storage read
   works in release builds). No `UIBackgroundModes` — deliberately absent
   per MADR D2/Rejected.
4. Set `DEVELOPMENT_TEAM` locally (not committed if it embeds the team id
   in a way the owner prefers private — mirror how
   `ops-android-signing.md` handles keystore secrets; start
   `docs/ops-ios-signing.md` recording the paid-program/provisioning
   decision, D8).
5. Smoke: `flutter build ios --simulator` and `flutter run` on simulator —
   app reaches the connect screen.

**Tests:** none new — this phase is build plumbing; the gate is the smoke
run.

## P1 — Lifecycle: honest iOS suspend/resume (MADR D2)

1. Introduce a platform-aware branch in `_onBackground()`
   (`lib/app_lifecycle.dart:135-160`): on iOS always take the
   `disconnect(manual: false)` path; keep the Android FGS early-return
   untouched. Express the platform check injectably (mirroring how
   `lifecycle_policy.dart` is already isolated) so U1 tests both branches
   on any host.
2. Gate the maintenance timer arm-site
   (`notification_coordinator.dart:291-328`) off on iOS (U2).
3. Resume path: verify `_retryNow` (350 ms debounce,
   `app_lifecycle.dart:174`) + `probeLiveness()` covers the
   socket-reclaimed (EBADF-on-resume) case — add a test that a dead
   channel at resume produces exactly one reconnect, no timer burst.

**Tests:** U1, U2 unit; resume-reconnect unit against a fake channel.

## P2 — Notifications: Darwin wiring (MADR D4)

1. `notification_service.dart:56-58`: add `DarwinInitializationSettings`
   (all request booleans false) and register `UNNotificationCategory`
   equivalents for the three kinds; Allow/Deny actions get the foreground
   option on iOS.
2. Add `DarwinNotificationDetails` at all four `show()` sites (`:170`,
   `:204`, `:235`, `:291`) — approval kind maps high-importance semantics;
   time-sensitive interruption is out of scope (fast-follow with
   entitlement).
3. Permission plumbing: resolve
   `IOSFlutterLocalNotificationsPlugin` in `requestNotificationsPermission`
   / `areNotificationsEnabled` paths (`:314-338`) so
   `osBlocked()` (`notification_coordinator.dart:459-463`) answers
   truthfully on iOS (U4).
4. Cold-launch tap replay (`:343-361`) — confirm it fires on iOS once
   Darwin init exists; covered by hardware F3g.
5. Copy: `settings_screen.dart:859` and the D3 honesty text — Settings
   explains that on iOS alerts arrive only while the app is open (until
   the follow-up MADR lands).

**Tests:** U3, U4 unit; widget test for the platform-aware Settings copy.

## P3 — Secrets: Keychain posture + reinstall inversion (MADR D5)

1. `settings_store.dart:58-62`: add
   `IOSOptions(accessibility: KeychainAccessibility.first_unlock_this_device)`.
2. Extend the 0066 probe: on iOS, secrets present while
   `expect_credentials` marker absent ⇒ stale foreign credentials — clear
   secure keys, surface the existing re-pair flow (answers 0066 Q2 for
   iOS). Keep Android behaviour byte-identical.
3. Confirm fail-closed paths (`:863-868`, `:892-896`, `:919-921`) surface
   `SecureStorageUnavailable` identically on iOS (they already branch on
   `Platform.isAndroid || Platform.isIOS` — no change expected, pin with a
   test).

**Tests:** U6 unit (probe matrix: marker×secret present/absent on iOS).

## P4 — Parity guards and platform polish (MADR D6, D7)

1. Plaintext guard: extend `_androidPlaintextBlocked()`
   (`connect_screen.dart:1103-1117`) to iOS with platform-neutral copy
   (U5).
2. Local-network first-run (D6): first dial treated as
   permission-triggering — one automatic retry after first failure; after
   repeated failure, connect-screen error copy gains the iOS branch
   pointing to Settings → Privacy & Security → Local Network. Fits inside
   the 0064 connect-screen state machine (no new screens).
3. Theme: `celestial.dart:251` platform-adaptive (iOS gets native
   transitions/physics; Android keeps predictive back at `:415`) (U7).
4. `SceneDelegate.swift`: resign-active privacy overlay (FLAG_SECURE
   analogue) — lean Swift, no plugin.
5. Chat polish behind Q2/Q3: HEIC mime verification and
   `mobile_scanner` error-code mapping — resolve the MADR open questions
   on device, patch `_mimeForImage` / `_errorTitle` only if the answers
   demand it.

**Tests:** U5, U7; unit for the first-dial retry state transition.

## P5 — Docs, standards, and hardware gate

1. `docs/standards/mobile/ios.md` mirroring `android.md` (Keychain
   posture, no-UIBackgroundModes rationale, privacy overlay, signing
   pointer); amend the `standards/mobile/README.md` sentence reserving iOS.
2. Finish `docs/ops-ios-signing.md` (provisioning, team, renewal cadence —
   the `ops-android-signing.md` sibling).
3. Add **Part F** to `docs/ops-hardware-validation.md` with rows
   F1g–F5g from the MADR verification table, plus the Gate row in the top
   table.
4. Flip Status bullets in MADR + this plan as phases land.

**Tests:** none — docs; the gate is Part F rows passing on hardware.

## File checklist

| File | Phases |
| --- | --- |
| `ios/Podfile`, `ios/Podfile.lock` | P0 |
| `ios/Runner/Runner.entitlements` (new) | P0 |
| `ios/Runner.xcodeproj/project.pbxproj` | P0 |
| `docs/ops-ios-signing.md` (new) | P0, P5 |
| `lib/app_lifecycle.dart` | P1 |
| `lib/data/ws/lifecycle_policy.dart` | P1 |
| `lib/data/notifications/notification_coordinator.dart` | P1, P2 |
| `lib/data/notifications/notification_service.dart` | P2 |
| `lib/features/settings/settings_screen.dart` | P2 |
| `lib/data/local/settings_store.dart` | P3 |
| `lib/features/connect/connect_screen.dart` | P4 |
| `lib/theme/celestial.dart` | P4 |
| `ios/Runner/SceneDelegate.swift` | P4 |
| `lib/features/chat/chat_screen.dart` (only if Q2/Q3 demand) | P4 |
| `lib/features/connect/qr_scan_screen.dart` (only if Q3 demands) | P4 |
| `docs/standards/mobile/ios.md` (new), `docs/standards/mobile/README.md` | P5 |
| `docs/ops-hardware-validation.md` | P5 |

## Verification map (MADR → plan)

| MADR ID | Plan phase | Kind |
| --- | --- | --- |
| U1, U2 | P1 | unit |
| U3, U4 | P2 | unit |
| U6 | P3 | unit |
| U5, U7 | P4 | unit / widget |
| F1g, F2g, F3g, F4g, F5g | P5 (Part F) | hardware |

## Edge cases held by design (for review)

- **Resume after hours, relay transport**: the loopback `ServerSocket`
  bridge (`relay_transport.dart:154`) dies with suspension; P1's
  always-disconnect makes this deterministic instead of half-alive.
- **Notification permission granted but local network denied**: two
  independent iOS permissions with opposite failure visibility; the P4
  connect-flow copy must not blame the wrong one (no API to query local
  network — copy stays suggestive, never assertive).
- **First launch on iPhone restored from an Android user's… n/a** — but
  first launch on an iPhone **restored from another iPhone's backup**:
  D5's `first_unlock_this_device` means secrets deliberately do not
  migrate; the expect-marker (prefs) *does* migrate → this is the
  marker-present/secret-absent case, which is 0066's existing
  `credentialsLost` path. Confirmed symmetric with the reinstall
  inversion. Both land in re-pair, never a crash loop.
- **Force-quit from the app switcher**: iOS delivers no lifecycle
  callback; next launch is cold. Cold-launch tap replay (P2.4) and
  transcript cache cover the UX.

## Sequencing and commits

House rules: one commit per phase (`git commit --no-edit`), full suites
green before each commit (`flutter analyze && flutter test` + simulator
smoke for P0/P4), no pushes. P0 → P1 → P2 → P3 → P4 → P5 strictly — P1
before P2 so notification testing never observes the stale-timer burst;
P5 last so docs describe shipped behaviour. Hardware Part F runs after P5;
**no iPhone hardware exists as of 2026-08-03**, so Part F rows are
authored but parked (`⏸ no device`) until one does — P0–P4 are
simulator-complete by design. Gate closure, when it happens, is recorded
in `ops-hardware-validation.md` and mirrored into both Status bullets per
house convention.
