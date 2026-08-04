---
title: "iOS Platform and Security Standards"
version: "3.12.2-v1"
last_updated: "2026-08-04"
component: "ios"
deployment_target: "16.0"
---

# iOS Platform and Security Standards

Decisions and rationale live in
[MADR 0067](../../0067-MADR-ios-port.md); this page is the working
reference. Where Android and iOS deliberately diverge, both sides are
stated so neither is "the default".

## Build configuration

- Deployment target is **iOS 16.0**, set identically in
  `ios/Runner.xcodeproj` (all three configurations) and `ios/Podfile`
  (`platform :ios, '16.0'`). Raising it needs a decision; lowering it is a
  regression (0067 D1).
- Open `ios/Runner.xcworkspace` in Xcode — never `Runner.xcodeproj` alone;
  CocoaPods targets only exist in the workspace.
- `Runner/Runner.entitlements` carries exactly one entitlement:
  `keychain-access-groups` (required by `flutter_secure_storage`).
  **No `UIBackgroundModes`, no push, no time-sensitive entitlements** —
  deliberately absent per 0067 D2; the background story is the follow-up
  MADR (0067 D3/Q4). Adding a background mode "to keep the socket alive"
  is both ineffective and an App Review 2.5.4 rejection risk.
- Signing: no `DEVELOPMENT_TEAM` is committed. Simulator builds need none.
  Device provisioning is documented in
  [ops-ios-signing.md](../../ops-ios-signing.md).

## Lifecycle: foreground-first, honestly

iOS has no foreground-service analogue; a suspended process loses its
sockets and its timers never fire (0067 F3, TN2277). The app therefore:

- Always parks the socket on backgrounding (`shouldParkOnBackground` in
  `lib/data/ws/lifecycle_policy.dart` — unconditional on iOS, unchanged on
  Android) and reconnects on resume.
- Never arms the background maintenance retry timer on iOS
  (`notification_coordinator.dart`).
- Says so in Settings ("Alerts arrive while the app is open") rather than
  pretending otherwise (0063: no simulated liveness).

Do not add code that assumes background execution on iOS. If a feature
needs it, it belongs in the 0067 D3 follow-up decision, not in a
`Timer`.

## Secrets and application security

- `FlutterSecureStorage` is constructed with
  `IOSOptions(accessibility: KeychainAccessibility.first_unlock_this_device)`
  — device-bound (no backup/restore migration; the Keychain mirror of
  Android's `allowBackup="false"`) while still readable shortly after a
  reboot. Do not weaken to a migrating accessibility class.
- The **reinstall inversion** is handled in
  `SettingsStore.probeSecretStore()`: iOS Keychain items outlive app
  deletion while the `expect_credentials` prefs marker does not, so
  secrets-without-marker on iOS means a previous install's credentials —
  they are cleared and the connect screen is the re-pair flow (0067 D5,
  closing 0066 Q2). The restored-from-backup shape (marker without
  secrets) is 0066's existing `credentialsLost` path.
- Secure-storage failures fail closed on iOS exactly as on Android; the
  cleartext prefs fallback stays desktop-only.
- Plaintext `ws://` is refused before dialling on both phone platforms
  (`_plaintextBlocked` in the connect screen). ATS never applies to the
  app's networking (all `dart:io`), so TLS enforcement is entirely ours —
  keep it in Dart.
- The screenshot/app-switcher analogue of Android's `FLAG_SECURE` is the
  privacy shield in `ios/Runner/SceneDelegate.swift` (opaque cover on
  `sceneWillResignActive`). Keep it an opaque view, not a blur.

## Local network privacy (iOS 14+)

The first LAN dial triggers the one-time permission prompt and **that dial
fails**; there is no API to query the permission state, and a backgrounded
app with undetermined permission is denied silently (TN3179). The connect
screen therefore gives the first network-shaped failure one automatic
retry (`_withLocalNetRetry` — never after a spent pair code) and keeps the
failure copy suggestive, pointing at Settings → Privacy & Security →
Local Network. There is no discovery/mDNS in the product, so
`NSBonjourServices` and the multicast entitlement stay absent.

## Notifications

- Darwin init registers the `approval_actions` category; Allow/Deny are
  `foreground` actions because a suspended process has no WebSocket to
  answer on (the iOS mirror of Android's `showsUserInterface: true`).
- Permission is requested explicitly in `NotificationService.init()` (all
  `DarwinInitializationSettings` request booleans stay false), keeping the
  consent flow symmetric with Android.
- Interruption level stays at `active`; `timeSensitive` needs its
  entitlement and is a deliberate fast-follow (0067 D4).

## Known device-gated items

QR camera behaviour, speech (60 s SFSpeechRecognizer session cap), HEIC
mime labelling, local-network prompt behaviour on Tailscale addresses, and
cold-launch notification tap replay are validated only on hardware — Part
F of [ops-hardware-validation.md](../../ops-hardware-validation.md).
