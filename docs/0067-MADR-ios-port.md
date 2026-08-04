# MADR 0067: iOS port of the mobile companion app

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — for review. Not implemented. The iOS Flutter shell
  was scaffolded ahead of this decision (commit `14bdbd0`, 2026-08-03) and
  builds clean on the simulator; no decision-bearing iOS behaviour exists yet.
- **Date**: 2026-08-03
- **Deciders**: Project Owner
- **Implementation plan**: [0067-PLAN-ios-port.md](0067-PLAN-ios-port.md)
- **Scope**: `apps/mobile` (Dart + `ios/` runner), docs under
  `docs/standards/mobile/`. **No protocol changes, no daemon changes, no
  relay changes.** The pairing contract (`internal/pairuri/pairuri.go`),
  auth model (0005), and transport policy (0062/0063) are consumed as-is.
- **Related**:
  [MADR-client-identity-decision.md](MADR-client-identity-decision.md)
  (client key must remain PEM-loadable by Dart `SecurityContext` — D-key
  storage constraint carries over),
  [MADR-certificate-management-decision.md](MADR-certificate-management-decision.md)
  (pin-first trust model),
  [0063-MADR-connection-liveness-truth.md](0063-MADR-connection-liveness-truth.md)
  (verified liveness; iOS suspension makes this the norm, not the edge),
  [0066-MADR-secure-storage-upgrade-resilience.md](0066-MADR-secure-storage-upgrade-resilience.md)
  (F14 there records the iOS Keychain failure mode as "dormant" — this MADR
  wakes it),
  [0064-MADR-connect-screen-simplification.md](0064-MADR-connect-screen-simplification.md)
  (connect flow gains an iOS local-network first-run step),
  [0065-MADR-update-automation.md](0065-MADR-update-automation.md)
  (APK install channel has no iOS analogue — named as a non-port below).
- **Non-goals**: App Store distribution; background push ("walk away and
  get pinged") — deliberately split out as a follow-up decision (see D3);
  mDNS/Bonjour discovery (the product has none — QR/paste/manual only);
  hardware-bound client keys (0005 already ruled this out for the Dart
  `SecurityContext` PEM path); iPad/visionOS layouts.

---

## Problem

The companion app is Android-only. We want the same app on iPhone. Flutter
was chosen in `PLAN-flutter-android-client-assessment.md` partly because it
"leaves a path to iOS without rewriting domain code" — this MADR decides
how to walk that path.

The port is **not** uniformly hard. The transport/security core is pure
`dart:io` and moves unchanged (F1). The hard part is that the app's
headline background story — an Android foreground service holds the
process and its WebSocket alive so approval requests arrive as
notifications while the phone is pocketed — has **no iOS equivalent**
(F3). A port that ignores this ships an app that looks done and silently
never notifies. The decisions below draw the line between "ports
unchanged", "ports with an iOS branch", and "does not port — decide
separately".

## Grounding facts (verified against the tree, 2026-08-03)

### F1 — The transport/security stack is platform-neutral and ports unchanged

All networking is `dart:io` (`HttpClient`, `SecureSocket`,
`IOWebSocketChannel` with `customClient`) — `lib/data/ws/mcremote_client.dart:721-742`,
relay bridge `lib/data/ws/relay_transport.dart:154`. Dart bundles BoringSSL
on both platforms: `SecurityContext` PEM loading (client cert + EC key,
`lib/data/ws/mcremote_client.dart:164-165`) and `badCertificateCallback`
SHA-256 pinning (`:127-137`) behave identically on iOS
(<https://api.dart.dev/dart-io/SecurityContext-class.html>). Apple's ATS
applies only to URLSession, never to raw sockets
(<https://developer.apple.com/documentation/bundleresources/information-property-list/nsapptransportsecurity>),
so the self-signed pinned `wss://` to a LAN IP needs **no ATS exemption**.
Client identity (0005: `basic_utils` P-256 self-signed cert,
`lib/data/ws/client_identity.dart:44-60`) is pure Dart. There are **zero**
MethodChannels in the app and zero direct file I/O (prefs + secure storage
only), so there is no platform-channel or filesystem porting surface.

### F2 — The iOS shell exists but is inert

Commit `14bdbd0` added `apps/mobile/ios/` (verified simulator build,
Xcode 26.6 / Flutter 3.44.8). `Info.plist` already carries the five usage
strings (camera, mic, speech, photos, local network). But:
`ios/Podfile.lock` lists only `Flutter` + `flutter_foreground_task` — the
other 8 plugin pods are not installed; no `.entitlements` file exists; no
`UIBackgroundModes`; no `DEVELOPMENT_TEAM` configured. The scaffold's
deployment target was the template default 13.0; raised to 16.0 per D1
(decision 2026-08-03: the install base is the owner's modern iPhone, the
strictest plugin floor is 13.0, and the roadmap floors are iOS 15 for
time-sensitive notifications and 16.1 for Live Activities — 16.0 clears
plugin floors and keeps roadmap features one `@available(iOS 16.1)` check
away instead of three). Bundle id is `com.maccavelli.magicCliRemote`
vs Android `com.maccavelli.magic_cli_remote` (iOS forbids underscores —
intentional, note for signing).

### F3 — iOS has no foreground-service analogue; suspended apps lose sockets

Apple (DTS, "iOS Background Execution Limits"): there is no mechanism for
"running code continuously in the background… resuming in the background
in response to a network or IPC request"
(<https://developer.apple.com/forums/thread/685525>). Suspension follows
seconds after backgrounding; TN2277: the system "may choose to reclaim
resources out from underneath a network socket", surfacing as EBADF on
resume; recommended pattern is close-on-background / reopen-on-foreground
(<https://developer.apple.com/library/archive/technotes/tn2277/_index.html>).
`flutter_foreground_task` on iOS is BGTaskScheduler + notifications —
"approximately 30 seconds every 15 minutes", opportunistic, killed by
force-close (<https://pub.dev/packages/flutter_foreground_task>) — and the
app already hard-gates it off:
`lib/data/notifications/foreground_service.dart:29`. Background URLSession
supports only download/file-upload tasks, not WebSockets. VoIP push
requires CallKit call UI or the app is killed. Declaring `voip`/`audio` to
hold a socket is an App Review 2.5.4 rejection.

### F4 — The Dart lifecycle layer assumes the foreground service exists

`lib/app_lifecycle.dart:135-160`: `_onBackground()` early-returns (leaves
the socket up, reconnect timers armed) when notifications are enabled —
the comment says the foreground service is the point. On iOS those timers
land in a suspended process and never fire; resume then replays a burst of
stale state. The 5→30 min background maintenance `Timer`
(`lib/data/notifications/notification_coordinator.dart:291-328`) likewise
cannot fire while suspended. The reconnect machinery itself
(`lib/data/ws/mcremote_client.dart:1983-2032`, backoff 1→30 s;
`probeLiveness()` `:1917-1946`) is exactly what an iOS resume path needs —
it just must be *driven* by foregrounding, not by wall-clock timers.

### F5 — Notifications are currently a silent no-op on iOS

`lib/data/notifications/notification_service.dart:56-58` initializes with
`InitializationSettings(android: …)` only — no
`DarwinInitializationSettings`, so no permission request and no category
registration ever happens on iOS. All four `show()` sites (`:170`, `:204`,
`:235`, `:291`) pass `NotificationDetails(android: …)` only — nothing is
presented. The Allow/Deny actions (`:146-159`) rely on
`showsUserInterface: true` routing taps into the live main isolate; on iOS
a background action fires into a **suspended process with no WebSocket**
(`:372-377` is a deliberate no-op stub), so actions must be foreground
(`.foreground` option) on iOS. Permission plumbing
(`:314-338`) resolves only the Android plugin — `osBlocked()` returns
`null` on iOS and the Settings warning never shows. Settings copy is
hardcoded "blocked by Android"
(`lib/features/settings/settings_screen.dart:859`).

### F6 — Keychain semantics invert two deliberate Android postures

Android sets `allowBackup=false` + backup/extraction exclusion rules and
`flutter_secure_storage` is constructed with only
`aOptions: AndroidOptions(resetOnError: true)`
(`lib/data/local/settings_store.dart:58-62`) — **no `iOptions`**. On iOS,
default accessibility means secrets migrate to a new phone via encrypted
backup/restore, and Keychain items **survive app deletion** — so a
reinstall finds a stale `client_key`/`device_token` while the
`expect_credentials` marker (prefs, `:120`) was wiped: the exact inverse
of the 0066 `credentialsLost` signal (0066 F14 called this "the
mirror-image failure mode, dormant"; Q2 there is still open). Fix shape:
`IOSOptions(accessibility: KeychainAccessibility.first_unlock_this_device)`
plus a reinstall-consistency probe. Requires the Keychain access-groups
entitlement in Debug and Release
(<https://pub.dev/packages/flutter_secure_storage>). Note also
`shared_preferences` → `NSUserDefaults` **is** backed up on iOS, so the
transcript cache (`lib/data/chat/transcript_cache.dart:20-21`) and `host`
gain backup exposure Android excluded.

### F7 — iOS 14+ local-network privacy will hit the first connect

TN3179 (<https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy>):
the first LAN packet triggers the one-time user prompt; **that first
connection attempt fails**; there is **no API to query** the permission
state (denial is indistinguishable from daemon-down at the socket level);
and a *backgrounded* app with undetermined permission is denied silently
with no prompt. `NSLocalNetworkUsageDescription` is already present (F2).
No `NSBonjourServices` is needed — the product has no discovery: the QR's
`host=` comes from the daemon shelling out to `tailscale ip -4`
(`internal/tailnet/tailnet.go`); grep for mdns/bonjour across `internal/`
and `cmd/` is empty. Whether the local-network prompt fires for Tailscale
CGNAT addresses (100.64.0.0/10) as well as RFC1918 must be confirmed on
hardware.

### F8 — Small divergences, enumerated

- Plaintext guard is Android-only:
  `lib/features/connect/connect_screen.dart:1103-1117` refuses `ws://`
  with Android-specific copy; iOS needs the same guard and copy.
- `MainActivity.kt` sets `FLAG_SECURE` (token kept out of screenshots and
  the app switcher); iOS has no equivalent flag —
  `ios/Runner/SceneDelegate.swift` is an empty subclass; needs a
  resign-active privacy overlay or we accept the leak.
- Theme pins `platform: TargetPlatform.android` and predictive-back
  transitions (`lib/theme/celestial.dart:251`, `:415`) — on iOS this kills
  edge-swipe-back and native scroll physics.
- `speech_to_text` on iOS: SFSpeechRecognizer hard ~60 s session cap,
  per-device/per-app throttling of server recognition, and different
  status-string timing — the string match at
  `lib/features/chat/chat_screen.dart:496` is the fragile point.
- `image_picker` on iOS defaults to HEIC; `_mimeForImage`
  (`lib/features/chat/chat_screen.dart:627-634`) would mislabel `.heic` as
  JPEG (mitigated by `imageQuality` forcing JPEG conversion — verify).
- `connectivity_plus` on Apple reports VPN as `other` — already
  anticipated at `lib/app_lifecycle.dart:105-108`; degrades gracefully. ✅
- Debug host prefill already branches correctly for iOS Simulator
  (`lib/features/connect/connect_screen.dart:25`). ✅

### F9 — Distribution facts

Free personal team: provisioning expires every **7 days**, 10 App ID cap
(<https://developer.apple.com/support/compare-memberships/>). Paid program
($99/yr): 1-year development profiles; TestFlight internal testing (no
review, 90-day builds). App Review 4.2.7 (remote desktop clients) fits
this product's LAN/user-owned-host model but is written for
screen-mirroring and applied unevenly; 2.5.4 forbids background-mode
misuse. The 0065 "Restart to update" APK channel has **no iOS analogue**
under any distribution route.

## Prior art survey (external, 2026-08-03)

The two open-source products in this exact niche — phone as remote for a
coding agent on your dev machine — converged on the same architecture:

- **Happy** (<https://github.com/slopus/happy>, MIT): no reliance on a
  live socket in background; a self-hostable **zero-knowledge relay**
  holds only ciphertext and triggers APNs when the agent needs attention;
  QR pairing carries an ephemeral Curve25519 public key, phone encrypts
  its secret to it.
- **Omnara** (<https://github.com/omnara-ai/omnara>, Apache-2.0): phone
  talks only to a (self-hostable) API server; APNs push fired **only**
  when input is required.
- **Home Assistant iOS** (<https://github.com/home-assistant/iOS>): the
  only sanctioned cloud-free LAN push — `NEAppPushProvider` network
  extension — works solely on allow-listed Wi-Fi SSIDs, needs a native
  extension target, is documented-finicky, and HA still keeps an APNs
  fallback.
- **Blink Shell / Termius / Mosh**: accept the disconnect; make resync
  cheap (Mosh SSP), stretch time with Live Activities, put session
  durability server-side.
- **KDE Connect iOS / Möbius Sync**: the cautionary tales — daemon-style
  apps that document their own background unreliability.

Transferable consensus: **foreground = direct pinned LAN socket (our
existing model, unchanged); background = treat the socket as disposable
and resync cheaply; pocketed-phone alerts require an APNs path (relay) or
don't exist.** Our daemon already buffers sessions server-side and the
protocol resyncs on attach, which is the hard prerequisite others had to
build.

## Decision drivers

- Preserve the self-hosted, no-cloud, no-telemetry model (0045/0046
  posture; the notification layer is explicitly cloud-free today).
- One codebase; platform divergence only behind seams that already exist
  (`foreground_service.dart`, `notification_service.dart`,
  `app_lifecycle.dart` — F4/F5 show the seams are real).
- Honest state over simulated state (0063): never pretend background
  liveness iOS does not grant.
- Fail-closed secrets (0046/0066) must survive translation to Keychain
  semantics, including the reinstall inversion (F6).
- Ship a usable foreground app soon; do not gate the whole port on the
  background-push product question.

## Decision outcome

### D1 — Single codebase; iOS is a first-class target of `apps/mobile`

No fork, no rewrite. The `dart:io` transport/security core (F1) ships to
iOS byte-for-byte. iOS-divergent behaviour is expressed inside the
existing seams, never scattered through features. `docs/standards/mobile/`
gains `ios.md` (mirror of `android.md`) and its README sentence "do not
describe iOS-specific configuration as implemented behavior" is amended
when phases land. Deployment target is **iOS 16.0** (owner-decided
2026-08-03; rationale in F2), set identically in `project.pbxproj` and
the Podfile.

### D2 — iOS connection model: foreground-first, disposable socket

On iOS the WebSocket is a foreground resource. Backgrounding always takes
the clean-suspend path (`disconnect(manual: false)`) — the
`_onBackground()` early-return that exists for the Android foreground
service (F4) is bypassed on iOS. Resume drives the existing fast path
(`_retryNow` + `probeLiveness`). Background maintenance timers are not
armed on iOS. This is TN2277's recommended pattern and what every surveyed
iOS app converged on. Consequence stated honestly: **on iOS v1, nothing
happens while the phone is pocketed; state resyncs on open.**

### D3 — Background attention ("walk away and get pinged") is split into its own MADR

The credible options are (a) a self-hostable zero-knowledge APNs relay
(Happy/Omnara pattern — fits our relay codebase, contradicts strict
no-cloud only if Apple's APNs counts as cloud, which it does), (b)
`NEAppPushProvider` LAN push (cloud-free, SSID-gated, native extension,
finicky), (c) stay foreground-only. This is a product decision with
infrastructure cost, not a porting task; deciding it here would either
stall the port or bake in an unexamined cloud dependency. 0067 ships (c)
and documents it in-app (Settings copy explains the iOS difference).
Follow-up MADR owns (a) vs (b).

### D4 — Notifications: full Darwin wiring, foreground-presentation scope

`DarwinInitializationSettings` with all permission booleans false at init;
explicit permission request through the existing coordinator consent flow;
`UNNotificationCategory`/actions registered at init with Allow/Deny marked
**foreground** on iOS (a suspended process has no socket to answer on —
F5); per-kind `DarwinNotificationDetails` alongside the Android details at
all four call sites; iOS permission/`osBlocked` plumbing; platform-neutral
Settings copy. Time-sensitive interruption level (needs entitlement) is
noted as a fast-follow, not v1.

### D5 — Keychain posture mirrors the Android no-backup decision

`IOSOptions(accessibility: KeychainAccessibility.first_unlock_this_device)`
so secrets are device-bound (no migration via restore) and available
shortly after reboot; Keychain access-groups entitlement added to Debug
and Release. The reinstall inversion (F6) is closed by extending the 0066
probe: secrets present + `expect_credentials` absent ⇒ treat as stale
foreign credentials, clear and re-pair. This also answers 0066 Q2 for iOS.

### D6 — Local-network first-run is an explicit connect-flow state

First connection attempt only ever from foreground (F7 background-denial
rule). The connect flow treats the first LAN dial as
"permission-triggering": expect one failure, retry after the prompt, and
on repeated failure show copy directing to Settings → Privacy & Security →
Local Network (no API exists to query — F7). No `NSBonjourServices`, no
multicast entitlement — there is no discovery to port.

### D7 — Parity guards and platform polish

The `ws://` plaintext refusal extends to iOS with iOS copy (F8);
`SceneDelegate` gains a resign-active privacy overlay as the
`FLAG_SECURE` analogue (F8); theme `platform:` becomes adaptive so iOS
gets edge-swipe-back and native physics while Android keeps predictive
back (F8); "blocked by Android" copy becomes platform-aware.

### D8 — Distribution: paid developer program, development provisioning

The 7-day free-team cycle (F9) is untenable for a daily-driver. Paid
program, 1-year development profile on the owner's device now; TestFlight
internal later if wanted. App Store submission is out of scope (Non-goals)
— revisit alongside D3's follow-up MADR since review guideline 4.2.7/2.5.4
constraints interact with the background answer.

### Rejected

- **`voip`/`audio`/`location` background modes to hold the socket.**
  2.5.4 rejection risk, PushKit now requires CallKit call UI, and runtime
  kills — the "loopholes" are closed (F3).
- **`flutter_foreground_task` as an iOS keepalive.** It is BGTaskScheduler
  underneath — ~30 s per ~15 min, opportunistic (F3). The existing
  hard-gate stays; we do not add `UIBackgroundModes: fetch` for it in v1.
- **Background URLSession for the WebSocket.** Download/file-upload tasks
  only (F3).
- **Deciding the APNs relay inside this MADR.** See D3.
- **mDNS/Bonjour discovery.** No discovery exists to port; raw multicast
  needs an Apple-approval-gated entitlement (F7).
- **Hardware-bound client key via platform channel TLS.** Re-affirmed out
  of scope per 0005 (Dart `SecurityContext` needs exportable PEM).

## Consequences

### Positive

- The security-critical core (pinning, client identity, relay bridge)
  ships to iOS with zero diff — no new attack surface from the port (F1).
- Honest iOS lifecycle: no armed-but-dead timers, no fake liveness;
  0063's "verified, not assumed" principle extends naturally.
- Keychain decision closes a known dormant 0066 failure mode rather than
  shipping it.
- The background product question gets its own decision with real options
  costed, instead of a rushed default.

### Negative / trade-offs

- iOS v1 has **no pocketed-phone alerts** — a real feature gap vs Android,
  visible to the user. Mitigated by in-app copy and by D3's follow-up.
- Foreground Allow/Deny actions open the app instead of acting inline —
  slower than Android's one-tap-from-shade.
- Ongoing $99/yr and cert/profile maintenance; 0065's update automation
  does not extend to the iOS binary.
- Platform-adaptive theme means the two platforms no longer render
  pixel-identically; screenshots/tests that assumed Android transitions
  need care.

### Neutral

- ATS never engages (dart:io), so no `NSAppTransportSecurity` keys exist
  to audit — the trust story remains 100 % in Dart, same as Android.
- `NSUserDefaults` backup exposure of non-secret prefs (F6) is accepted;
  secrets are Keychain-bound and excluded by D5.

## Alternatives considered

- **Native Swift rewrite.** Maximum platform fidelity, double maintenance,
  re-implements 23 k lines of audited Dart including the 0005 identity
  crypto. Rejected — the Flutter bet already paid for this port (F1).
- **Browser-based access instead of an app** (VibeTunnel pattern —
  xterm.js over a tunnel). Zero iOS code, but abandons the native UX,
  QR pairing, secure storage, and notification model entirely. Noted as a
  debug/fallback surface, not the product.
- **`NEAppPushProvider` in v1.** Cloud-free background push is seductive,
  but: native extension target, SSID allow-listing UX, documented
  flakiness, and Home Assistant still ships an APNs fallback. Deferred to
  D3's MADR with eyes open.

## Verification

| # | Check | How |
| --- | --- | --- |
| U1 | Lifecycle policy on iOS always takes the suspend path when backgrounded (no early-return with notifications enabled) | unit test (platform-injected policy) |
| U2 | Background maintenance timer is never armed on iOS | unit test |
| U3 | Notification init registers Darwin settings + categories; all four kinds carry `iOS:` details | unit test on service construction |
| U4 | `osBlocked()` resolves the iOS plugin and returns a real answer | unit test (mocked plugin) |
| U5 | Plaintext `ws://` refused on iOS with iOS copy | widget test |
| U6 | Secure storage constructed with `first_unlock_this_device`; reinstall inversion (secret present, marker absent) resolves to clear-and-re-pair | unit test |
| U7 | Theme resolves platform adaptively (iOS ≠ forced android) | widget test |
| F1g | First-connect local-network prompt: one failed dial, prompt, retry succeeds; denial path shows Settings guidance | hardware (Part F) |
| F2g | Background 10 min → resume: reconnect + full resync < 3 s on LAN; no stale-timer burst | hardware (Part F) |
| F3g | Notification permission flow, banner presentation, Allow/Deny foreground round-trip against live daemon | hardware (Part F) |
| F4g | App delete + reinstall: stale Keychain credentials detected and cleared, clean re-pair | hardware (Part F) |
| F5g | QR pair, speech input (>60 s session behaviour), HEIC image attach, mesh (Tailscale) vs RFC1918 prompt behaviour | hardware (Part F) |

Hardware rows land as **Part F** in
[ops-hardware-validation.md](ops-hardware-validation.md) when the plan is
accepted.

## Open questions

- Q1: Does the iOS local-network prompt fire for Tailscale CGNAT
  (100.64.0.0/10) destinations, or only RFC1918/link-local? Determines
  whether mesh-first users ever see the F1g flow. Hardware answer.
- Q2: Does `image_picker` with `imageQuality: 85` always transcode HEIC →
  JPEG on iOS, making the `_mimeForImage` fallback safe? Verify on device;
  otherwise map `.heic`/`.heif` explicitly.
- Q3: Do `mobile_scanner` iOS error codes match the Android
  `permissionDenied` mapping the scan screen branches on
  (`lib/features/connect/qr_scan_screen.dart:132`)?
- Q4: For D3's follow-up: can `mcrelay` grow the zero-knowledge APNs leg
  (Happy pattern) without violating the no-telemetry posture, or is
  `NEAppPushProvider` worth the extension cost?
