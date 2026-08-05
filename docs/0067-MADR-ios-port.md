# MADR 0067: iOS port of the mobile companion app

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: **Implemented 2026-08-04** (software; P0–P5 of the plan, one
  commit per phase, suite green at 709 tests + simulator builds throughout).
  Hardware validation is **parked — no iPhone hardware exists**: Part F rows
  in [ops-hardware-validation.md](ops-hardware-validation.md) are authored
  and marked `⏸ no device`; Q1–Q3 below stay open until they run. D3's
  follow-up MADR (background attention) remains undecided by design. The
  iOS shell was scaffolded ahead of this decision (commit `14bdbd0`,
  2026-08-03); decisions D1–D8 locked and implemented 2026-08-03/04.
  **Amendment A1 (2026-08-04)**: transport protocol parity assessment —
  wire parity confirmed; behavioural parity under iOS's reconnect-heavy
  pattern needs the T1–T11 work list (see A1), proposed as its own
  follow-up MADR (0068 candidate) since it touches daemon and relay.
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

Hardware rows are authored as **Part F** in
[ops-hardware-validation.md](ops-hardware-validation.md), parked
`⏸ no device` until an iPhone exists.

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

---

## Amendment A1 — transport protocol parity assessment (2026-08-04)

Requested after P0–P5 landed: a deep audit of the Android app's core
transport engine and mcrelay support against the iOS effort, to establish
what "protocol parity" still requires. Method: two full audits against the
tree — the Dart engine (`lib/data/ws/*`, both dial paths, teardown/resume
machinery, test inventory) and the Go contract (`internal/ws/server.go`,
`internal/relay/*`, `internal/relayhost/client.go`, `internal/auth`,
`docs/protocol-v1.md`). Everything below carries file:line anchors.

### The headline, stated precisely

**Wire-format and mechanism parity already hold.** Grep-verified: the
transport layer contains zero `Platform.`/`defaultTargetPlatform` checks,
zero socket options (no keepalive, no nodelay — anywhere), and no Android
system dependency; the loopback relay bridge is IPv4-loopback on both ends
(`relay_transport.dart:154`, `mcremote_client.dart:808`), which iOS
provides and exempts from local-network privacy. The Android
`network_security_config.xml` was never enforced for this traffic anyway —
dart:io bypasses Android's `NetworkSecurityPolicy` exactly as it bypasses
ATS, so the two platforms were already at (undocumented) parity there.
Auth-per-reconnect is cheap on the daemon (one flock + `devices.json`
read, no KDF — `internal/auth/store.go:403-448`), TLS session tickets are
on by default (`internal/certs/certs.go:533-538`), and the idempotency
ledger works across sockets (`internal/ws/idempotency.go:37-47`), which is
what makes retry-after-foreground safe.

**Behavioural parity under the iOS connection pattern does not yet hold.**
Android as shipped is one long-lived socket held by a foreground service;
iOS as decided (D2) is a disconnect on every pocket and a cold dial on
every glance. That pattern shift, not the port itself, is where the gaps
are. They fall into four groups.

### F10 — The liveness contract is informal and entirely client-driven

The daemon never sends WS pings; transport-level ping/pong does **not**
reset its 60 s read deadline — only an app-level `{"type":"ping"}` data
frame does (`internal/ws/server.go:528-537`, `:588-590`; stated in 0063
but absent from `protocol-v1.md`, which has no connection-lifecycle
section at all). The deadline is the *only* half-open reaper and has no
config key. Nothing advertises the cadence to clients (`auth_ok` carries
only device id/name/home dir, `:816-820`). The engine honours it today
(10 s app ping, `link_health.dart:26`), but the contract exists as
convention, and iOS's suspend timing makes the failure mode (healthy
socket reaped at 60 s) routine rather than exotic.

### F11 — Half-open sockets + flat connection pools = self-eviction

There is **no per-device connection replacement**: N authed sockets per
device coexist (`internal/ws/server.go:951-960`), capacity is a flat
`MaxWSClients = 8` (`internal/config/config.go:666`), and only
*unauthenticated* sockets are evictable at capacity (`:1269-1279`,
asserted by `eviction_test.go:22-28`). A suspended-without-FIN socket
holds its slot up to 60 s. Eight reconnect cycles inside one reap window
fill the pool with the device's own zombies and the ninth — the live
dial — is refused `too many clients`. The relay has the same shape:
`MaxPhonesPerHost = 8` held from `beginJoin` to splice end
(`internal/relay/hub.go:188-219`), reaped in practice by the daemon's
60 s deadline (else 5 min splice-idle), plus join rate limits a flapping
client can reach (30 joins/min/IP, charged to the phone's *and* the
host's IP per reconnect — `internal/relay/config.go:54-57`,
`internal/relay/server.go:339-343`). The relay also reads its first
envelope with **no deadline** (`server.go:367`, `:481`, `:601`): an
upgrade-then-suspend leaves an unreaped goroutine.

### F12 — Park→resume races in the engine, concentrated in the relay path

The iOS-normal cycle exercises teardown paths Android rarely ran:

- `_teardownSocket` nulls `_relayTransport` synchronously then awaits its
  close (`mcremote_client.dart:2151-2189`); the resume dial 350 ms later
  can open a **second `join` for the same `host_id`** while the first
  outer WSS is still closing. Nothing asserts a single-outstanding-join
  invariant, and no test covers teardown racing a dial.
- `RelayTransport` has three half-alive states: `_replacePeer` lacks a
  `_closed` check (accept racing close installs a peer nobody closes,
  `relay_transport.dart:266-281`); outer-stream `onDone` closes only the
  peer, leaving `_server`/`_outerHttp` alive with `failure == null`
  (`:175-178`); and the outer `sink.close()` is unbounded (`:380`) inside
  iOS's ~5 s background grace window (the inner close is bounded 2 s).
- `_onBackground` early-returns on `!mounted` **before** the park runs
  (`app_lifecycle.dart:143`), leaving socket + timers to suspend live.
- A dial in flight at background time is epoch-checked only *after* its
  awaits (`mcremote_client.dart:1713`); `RelayTransport.open` is
  epoch-unaware, so a half-built bridge survives suspension.
- The relay leg's serial worst case (15 s join + 8 s loopback + 20 s
  inner ready = 43 s) exceeds the 35 s episode budget
  (`kDialEpisodeBudget`, `:485`) — a stalled relay-first dial silently
  forfeits its mesh fallback. Reached rarely on Android; every foreground
  is a cold dial on iOS.

### F13 — Resume semantics quietly repeal three shipped decisions on iOS

- `reconnect()` unconditionally zeroes `_handshakeFailures` and the
  backoff rung (`mcremote_client.dart:2072-2073`) and every resume routes
  through it — so the 6-failure park is unreachable on iOS and a wedged
  daemon is re-dialled at full rate on every app switch.
- `bumpNetworkGeneration()` fires per resume (`app_lifecycle.dart:214`),
  refreshing the D11 (0062) per-generation fallback budget constantly —
  its anti-thrash property is materially weakened under iOS cadence.
- The in-memory `_stickyTransport` bypasses the authority scoping the
  store enforces (`mcremote_client.dart:1113-1121` vs
  `settings_store.dart:262-273`) and is never invalidated by
  park/disconnect — consulted an order of magnitude more often on iOS.
- The urgent-probe VPN heuristic is inverted on Apple platforms:
  `connectivity_plus` reports `other`, never `vpn` (the code comment says
  so), so `!results.contains(vpn)` is **always true** on iOS — every
  connectivity blip on a mesh session triggers the 2 s urgent probe
  (`app_lifecycle.dart:110-113`, `mcremote_client.dart:1919-1932`),
  which can tear down a healthy session on a slow cellular leg. This is
  the single highest-risk engine finding.

### F14 — Resync is correct but not gap-aware, in either direction

`SessionSynchronizer` runs the full reconcile (double `session.list` +
up to 32 pages of `session.history` per session) on **every** connected
edge (`session_synchronizer.dart:32-91`) — a 3-second app switch costs
the same as an 8-hour gap, on cellular, per foreground. In the other
direction the daemon's 800-event ring returns whatever survives
`since_seq` with **no `first_seq`/gap marker**
(`internal/session/manager.go:816-821`), so a long-absent phone cannot
distinguish "missed nothing" from "ring already truncated"; `seq` can
also restart lower after an unclean daemon exit (5 s persistence
debounce, `:205-208`), silently filtering real events. Async ops
in flight at backgrounding are cancelled with the socket
(`internal/ws/server.go:127-134`) — the idempotency ledger makes the
retry safe but covers six types with 256 process-wide entries.

### A1-D1 — Decision: treat "reconnect-heavy client" as a protocol profile

Parity is redefined: not "the bytes match" (they do) but "the shipped
Android contract remains correct when the client reconnects an order of
magnitude more often". That requires named work on all three components.
The remediation is deliberately **not** folded into this MADR's plan —
it touches the daemon and relay (out of 0067's locked scope) and warrants
its own MADR/PLAN (0068 candidate: *transport hardening for
reconnect-heavy clients*). The work list, prioritised:

| # | Where | Work | Driver | Priority | 0068 disposition |
| --- | --- | --- | --- | --- | --- |
| T1 | phone | Serialize park→resume: awaited, epoch-guarded teardown before any new dial; single-outstanding-join invariant; fix `RelayTransport` half-alive states (accept/close race, `onDone` full teardown, bounded outer close, truly awaitable idempotent `close()`) | F12 | **P1** | ✅ 0068 P5 |
| T2 | phone | Platform-correct urgent-probe gating: on Apple, absence of a `vpn` signal is not evidence of mesh death — use the lenient probe | F13 | **P1** | ✅ 0068 P5 (`vpnSignalMeaningful`) |
| T3 | phone | Preserve backoff/park state across lifecycle resumes (distinguish user-initiated from resume-driven reconnects before zeroing `_handshakeFailures`); revisit per-resume generation bumps vs D11 | F13 | P2 | ✅ 0068 P5 |
| T4 | daemon | Per-device connection replacement: a successful `auth` closes the device's older authed socket(s), so zombies cannot exhaust `MaxWSClients` | F11 | **P1** | ✅ 0068 P2 (close 4001) |
| T5 | daemon+docs | Specify the liveness/lifecycle contract in `protocol-v1.md` (ping cadence, read deadline — advertised in `auth_ok`; client SHOULD-close-before-reconnect; reconnect semantics) | F10 | P2 | ✅ 0068 P0/P1 (`caps`, protocol-v2.md) |
| T6 | daemon | Gap signalling: `session.history` returns `first_seq` (or explicit truncation marker) so clients detect ring loss and refetch; define the daemon-restart `seq` case | F14 | P2 | ✅ 0068 P3 (seq bounds + epoch) |
| T7 | phone | Gap-scaled resync: cheap short-gap path (seq check before the full paged walk), resumable after interrupted resync | F14 | P2 | ✅ 0068 P3, improved by P4 resume fast path |
| T8 | phone | Map local-network-denial and NAT64 failure shapes to distinct copy; verify relay fallback engages on IPv6-only carriers (App Store review tests NAT64; mesh dials literal IPv4) | F12/F15 | P2 | ✅ 0068 P5 (copy); carrier verification stays hardware (F5g/F6g) |
| T9 | relay | First-envelope read deadline; slot-accounting reconciliation sweep; `Retry-After` on rate-limit responses | F11 | P3 | ✅ 0068 P1 (deadline) + P6 (sweep, retry_after) |
| T10 | phone | Make the relay leg's serial timeouts fit inside `kDialEpisodeBudget` (or extend the budget knowingly) | F12 | P3 | ✅ 0068 P5 (`relayLegTimeouts`) |
| T11 | tests | The audit's enumerated gaps: teardown-races-dial, park→resume rebuilds-from-scratch (exactly one join), `close()` idempotency/concurrency, urgent-probe path, overdue-timer burst on resume, transport tests under `TargetPlatform.iOS` | all | **P1** (alongside T1/T2) | ◑ 0068 P5 (unit level); the client-level park→resume single-join integration test is deferred — needs a fake-relay + TLS-daemon harness |

P1 = before the first hardware run (Part F would otherwise measure the
races, not the product); P2 = before daily-driver use; P3 = opportunistic.
**Disposition column added at 0068 close-out (2026-08-05).** Q5/Q7 remain
hardware questions (F6g/F5g); Q6 was answered and rejected in 0068 P2 —
joins carry no device identity by design, so there is no relay-side
replacement to build.

### A1 verification additions

| # | Check | How |
| --- | --- | --- |
| U8 | Exactly one relay `join` outstanding across a park→resume cycle; teardown completes (or is safely superseded) before the next dial adopts a socket | unit (fake relay) |
| U9 | `RelayTransport.close()` idempotent and awaitable under concurrent callers; accept-after-close leaves no open socket; outer `onDone` fully closes | unit |
| U10 | On `TargetPlatform.iOS`, a connectivity event without a `vpn` signal uses the lenient probe, not the urgent one | unit |
| U11 | Resume-driven reconnect does not reset the handshake-failure park counter; user-initiated does | unit |
| F6g | Ten rapid background/foreground cycles against a live daemon: no `too many clients`, no relay `limit`, session intact | hardware (Part F) |

### A1 open questions

- Q5: How long does a suspended iOS socket actually stay half-open
  server-side (does backgrounding reliably FIN before suspension)?
  Determines how much of F11 T4 buys in practice. Hardware.
- Q6: Does mcrelay tolerate a second `join` for the same `host_id`
  while the first splice is closing, or does T1 also need relay-side
  join-replacement? Answerable with a live-tagged Go test.
- Q7: What does `connectivity_plus` actually report on iOS with
  Tailscale active (`other` is asserted from docs, unverified)?
  Gates T2's exact predicate. Hardware.
