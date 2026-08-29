---
status: proposed
date: 2026-08-28
decision-makers: Project Owner
consulted: none
informed: none
associated-plan: 0121-PLAN-achieve-iphone-functional-parity.md
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Achieve iPhone functional parity

## Context and Problem Statement

Magic CLI Remote has an iOS runner and a substantial shared Flutter product,
but it does not yet have a releasable, daily-driver iPhone application with the
same user outcomes as Android. The distinction matters:

* **shared feature code exists** for nearly all session and agent operations;
* **iOS lifecycle, notification, hardware, signing, release, and review work is
  incomplete**;
* the app currently calls itself both an Android-only product and an iOS
  "software-complete dev target," neither of which describes the actual state;
* MADR 0067 deliberately deferred background attention, and that deferral is
  now the largest functional parity gap.

This record answers two questions:

1. What is the present iPhone support level, including bugs and incomplete
   wiring that are not visible in shared Dart tests?
2. What architecture and verification standard should define "full parity"
   with Android without fighting iOS lifecycle and distribution rules?

The decision is **outcome parity, not implementation symmetry**. Android may
keep a foreground service and install signed APK updates; iPhone may use APNs
and TestFlight/App Store updates. Both must let a person pair, operate every
supported agent feature, leave the phone in a pocket, receive timely attention,
act safely, resume without corruption, and install a supported release.

### Assessment scope and method

This record was reassessed read-only at local `HEAD` and `origin/master`
`294aee6` on 2026-08-28. That commit renames this record from its original 0119
draft to 0121 after the branch incorporated the real 0119 and 0120 records. The
working tree was clean before and after diagnostics. The intervening commits
add test portability work, analyzer exclusions, lockfile movement, and MADR
bookkeeping, but do not close an iOS finding.

The reassessment ran existing non-mutating diagnostics and inspected committed
source, tests, generated state, git history, resolved plugin source already in
the local package cache, current Apple/Flutter documentation, the installed
Apple toolchain, available simulators, attached devices, and local signing
identities. It deliberately did not run an iOS build: that creates derived
artifacts and belongs to the approved implementation plan.

The review covered:

* `apps/mobile/lib`, all 115 top-level Flutter test files, and platform gates;
* `apps/mobile/ios`, the Xcode project, entitlements, property lists, CocoaPods,
  generated Swift-package state, native tests, and signing runbook;
* Android native code, manifest, release job, and alert behavior as the parity
  baseline;
* the relay and relay-host trust boundary relevant to background attention;
* MADRs 0067, 0068, 0070, 0071, 0084, 0101, and later mobile feature history;
* current Apple, Flutter, Home Assistant, and Happy project documentation.

### Baseline: what is already strong

The iPhone work is not a greenfield rewrite. The app is intentionally a shared
Flutter application, and platform gating is narrow. The current shared product
already implements:

| Surface | iPhone code state | Evidence / qualification |
| --- | --- | --- |
| Pair by code, QR, or token | Implemented in shared Dart | QR/camera and Local Network prompt remain device-gated |
| Direct mesh/LAN and opaque relay transport | Implemented | iOS park/resume and relay lifecycle have unit coverage; hardware F1/F2/F6 remain open |
| TLS pinning, client identity, secure credential storage | Implemented | Keychain uses `first_unlock_this_device`; uninstall/reinstall inversion remains device-gated |
| Session list/create/end/delete, models, modes, thinking levels | Shared | No deliberate iOS feature gate found |
| Streaming chat, tools, permissions, questions, cancellation | Shared | Foreground path exists; background attention does not |
| Provider fleet/auth, receipts, handoff, sharing, Codex surfaces | Shared | Most landed after the iOS native port and lack iPhone integration evidence |
| Workspace inspection, diagnostics, skill authoring, shell command UI | Shared | Same qualification as above |
| Image, audio, camera, speech input | Shared/plugin-backed | HEIC, long speech, file picker, microphone, and memory pressure need device validation |
| On-device diagnostics and transcript cache | Shared | iOS file/data-protection behavior has no native integration test |
| Adaptive navigation transitions and privacy switcher shield | Implemented | Security behavior is not equivalent to Android `FLAG_SECURE` |

The shared Flutter suite is broad: 115 top-level test files cover protocol,
transport, session state, screens, receipts, attachments, provider operations,
and rendering. On the reassessment host, `flutter test` passed 1,310 tests with
3 intentional skips, `flutter analyze` reported no issues, and
`dart format --output=none --set-exit-if-changed .` checked 193 files without
changing one. Only six test files explicitly select `TargetPlatform.iOS`, and
there is still no `integration_test/` directory. The Keychain posture is
particularly thoughtful:
`SettingsStore` refuses plaintext fallback on iOS and uses
`first_unlock_this_device` (`apps/mobile/lib/data/local/settings_store.dart:42-69`),
then detects the Keychain-survives-reinstall inversion
(`settings_store.dart:350-371`). The lifecycle code also parks instead of
pretending iOS can preserve a WebSocket (`apps/mobile/lib/app_lifecycle.dart:173-204`).

### Parity findings

Severity in this record:

* **S1** — blocks a reliable iPhone product or can produce a user-visible wrong
  result on a primary path;
* **S2** — incomplete native/release wiring or material compliance risk;
* **S3** — hardening, idiom, polish, or documentation debt.

#### F1 — background attention is absent (S1, headline parity blocker)

Android keeps the process and WebSocket eligible in the background with a
`remoteMessaging` foreground service
(`apps/mobile/android/app/src/main/AndroidManifest.xml:17-34,112-133`). iOS
correctly parks its socket because the process suspends
(`apps/mobile/lib/app_lifecycle.dart:173-204`). There is no APNs entitlement,
token registration, push provider, or alert relay in `apps/mobile/ios`. There
is also no `remote-notification` background mode; a visible alert-only design
does not require that mode, while any future silent/background notification
handler would have to justify and test it.

Consequently, a permission ask, question, turn completion, or agent error that
occurs after iOS suspends the app does not reach the phone. Settings admits this
at `apps/mobile/lib/features/settings/settings_screen.dart:1057-1065`. This is
not a small notification bug; it removes the product's core "walk away and be
called back when needed" outcome.

MADR 0067 D3 and later assessments named this as a follow-up. The follow-up was
never decided. Foreground local notifications are not a substitute.

#### F2 — notification authorization is requested automatically at launch (S1)

`NotificationService.init()` suppresses the plugin's initialize-time request,
then immediately calls `ios.requestPermissions(...)`
(`apps/mobile/lib/data/notifications/notification_service.dart:65-111`). The
root `ConnectionLifecycleScope` always starts the coordinator after reading
preferences (`apps/mobile/lib/app_lifecycle.dart:74-116`), and the shipped
preference defaults to enabled
(`apps/mobile/lib/data/local/settings_store.dart:504-509`). This means a fresh
install can receive Apple's one-shot notification prompt before pairing and
before the person has seen why alerts matter.

The Settings toggle does contain the right user-initiated request seam
(`settings_screen.dart:664-684`), but launch initialization has already spent
the prompt. Apple explicitly recommends requesting authorization in context,
not automatically on first launch. Initialization and authorization need to be
separate operations, with an unknown/disabled default until the person opts in
after pairing or sees a clear pre-permission explanation.

The initializer also lacks an in-flight future. Because the coordinator starts
listening to events before awaiting `init()`
(`notification_coordinator.dart:98-113`), a fast incoming event can enter
`_ensureReady()` and race a second plugin initialization. Initialization should
be single-flight even though the OS normally coalesces its own permission UI.

#### F3 — foreground notification actions race reconnect and cold launch (S1)

iOS Allow and Deny actions are deliberately marked `foreground`
(`notification_service.dart:35-40,75-94`). The response handler immediately
calls `McremoteClient.respondPermission`
(`notification_coordinator.dart:215-261`). On an iOS resume, however, reconnect
is independently debounced by 350 ms (`app_lifecycle.dart:207-271`). On a cold
launch, auto-connect, coordinator initialization, launch-response replay, and
the response call are likewise not ordered behind a connected-state barrier.

Therefore the action can arrive while the socket is still parked or connecting.
The catch opens the session, but the selected Allow/Deny result is not queued or
retried. Part F3 of `docs/ops-hardware-validation.md:334-346` expects this path
to resolve correctly, yet every iOS row is still parked for lack of a device.

Apple's notification API does not require forcing these actions into the
foreground: the system can launch an app in the background and call its
notification delegate. The durable design is to persist an action intent,
authenticate it, reconcile the live pending ask and expiry, deliver it through
a background-capable attention channel, and retire it idempotently. "Try the
current WebSocket immediately" is not a sufficient state machine.

Any future background Allow/Deny action must also require device authentication
and preserve the host's fail-safe timeout. A notification must never turn a
stale or replayed action into agent power.

#### F4 — an Android-only foreground plugin still executes native iOS lifecycle code (S1/S2)

The Dart wrapper correctly no-ops outside Android
(`apps/mobile/lib/data/notifications/foreground_service.dart:24-36,74-75,107-108,118-123`).
The dependency is nevertheless an iOS CocoaPod
(`apps/mobile/pubspec.yaml:38-40`; `apps/mobile/ios/Podfile.lock:1-13`) and its
native plugin auto-registers.

The resolved `flutter_foreground_task` 10.0.0 iOS source was inspected at the
version pinned by `pubspec.lock`. Registration installs both application and
scene delegates. Launch unconditionally calls
`setMinimumBackgroundFetchInterval` and registers a `BGAppRefreshTask`; both
application and scene background callbacks submit that task. The plugin also
implements `UNUserNotificationCenter` delegate methods. Its own README says
iOS execution requires `BGTaskSchedulerPermittedIdentifiers`, but this app
deliberately declares neither that key nor a background mode.

So the app-level Dart no-op does **not** produce a native no-op. Every iOS
background transition can touch a background-task schedule the product did not
choose, while an unrelated notification plugin also owns notification
callbacks. This contradicts MADR 0067's stated foreground-only/no-background
posture and creates needless battery, logging, review, and callback-conflict
risk.

The plugin also uses `UserDefaults` throughout its iOS implementation and ships
no `PrivacyInfo.xcprivacy` in version 10.0.0. Apple requires required-reason API
use to be declared by the executable or SDK bundle that uses it; a third-party
SDK cannot rely on another SDK's manifest. This is a concrete App Store
submission risk, not a request to add bogus background entitlements.

The right fix is to keep this Android service implementation out of the iOS
binary/registrant through a platform-specific package seam or a narrowly
maintained wrapper/fork. Enabling background fetch merely to make an unused
plugin quiet would be the wrong architecture.

#### F5 — no iOS build, native test, archive, signing, or distribution lane exists (S1/S2)

The Flutter CI job runs on Ubuntu and performs only format, analyze, and Dart
tests (`.github/workflows/ci.yml:357-390`). Tagged releases build a signed
Android APK, but there is no macOS runner, iOS simulator build, `xcodebuild`
test, archive, IPA, signing, App Store Connect upload, or TestFlight job.

`apps/mobile/ios/RunnerTests/RunnerTests.swift:5-9` is the untouched template.
There is no `integration_test/` directory. The Dart suite mocks platform
channels, and only six test files explicitly exercise `TargetPlatform.iOS`.
That suite is valuable for shared logic, but it cannot discover a missing
entitlement, plugin delegate side effect, privacy manifest, signing failure,
camera/speech behavior, APNs callback, Keychain reinstall behavior, or an Xcode
archive failure.

The signing runbook records simulator-only status, no enrolled device, no paid
team, no APNs, and no TestFlight (`docs/ops-ios-signing.md:7-47`). The Xcode
project commits no development team and targets iOS 16, which is reasonable for
source development, but there is no reproducible release path.

#### F6 — real-device confidence is still zero for the iOS acceptance matrix (S1)

All six iPhone hardware rows remain parked
(`docs/ops-hardware-validation.md:329-346`):

| Gate | Unverified behavior |
| --- | --- |
| F1 | Local Network prompt, first-dial failure/retry, denied-permission copy |
| F2 | ten-minute suspension, reconnect, full resync, no timer burst |
| F3 | local notification action, permission flow, cold-launch replay |
| F4 | delete/reinstall Keychain inversion and re-pair |
| F5 | QR, speech cap, HEIC MIME, Tailscale Local Network behavior |
| F6 | ten park/resume cycles on direct and relay transport |

The only later iOS record is a 2026-08-25 iPhone 17e **simulator** preview of
composer/diagnostics layout with stubbed providers and no daemon pairing
(`docs/0112-PLAN-opencode-1.18.21-surface-parity.md:1878-1899`). Since the last
native iOS commit on 2026-08-04, `apps/mobile` changed by 30,695 insertions and
1,918 deletions across 218 files; `apps/mobile/lib` plus `apps/mobile/test`
account for 30,089 insertions and 1,893 deletions across 139 files. That includes provider
auth, receipts, settings, notification hardening, Codex surfaces, audio,
workspace inspection, diagnostics, skill authoring, sharing, and shell commands.

Shared code makes these likely to work, but "likely" is not a support tier. A
real iPhone and a paid Apple Developer Program membership are prerequisites for
closing this record's product claim.

#### F7 — privacy and App Store release artifacts are incomplete (S2)

There is no app-target `PrivacyInfo.xcprivacy`, no committed privacy-report
verification, no in-app privacy-policy link, and no App Store metadata source.
Settings' About section contains only version and the non-iOS update tile
(`apps/mobile/lib/features/settings/settings_screen.dart:1447-1458`). Apple's
current guideline 5.1.1 requires an easily accessible privacy-policy link both
in App Store Connect metadata and inside the app.

Most current iOS plugins do bundle their own manifests, including the
Apple-listed `connectivity_plus`; the exception that matters here is the unused
iOS half of `flutter_foreground_task` described in F4. The release process must
generate and review Xcode's aggregate privacy report rather than infer
compliance from individual packages.

The app uses TLS, P-256 client identities, JWS receipts, and certificate
pinning. App Store Connect therefore also needs an explicit export-compliance
determination. That is an administrative determination, not a reason to remove
encryption.

#### F8 — App Review positioning is unresolved (S2 product risk)

Apple's current guideline 4.2.7 places strict conditions on clients that mirror
specific host software, including a local-LAN restriction and host-initiated
account management. Magic CLI Remote is arguably **not** a remote desktop: it
does not stream or mirror the host screen, it renders a native protocol client,
and the agent software executes on a user-owned computer. Even so, its name,
purpose, relay/off-mesh path, and phone-initiated provider device flows make the
guideline a credible review question.

The repository's earlier assertion that the app fits a LAN/user-owned-host
model predates both the off-mesh relay and later provider-auth surfaces. The
submission must explain, accurately:

* the app pairs only with a daemon the person controls;
* the relay is an opaque transport and does not execute agents or expose a
  software store;
* provider software and credentials live on the person's host;
* phone-side provider flows control already-installed host software rather than
  create a primary Magic CLI Remote cloud account;
* no digital goods are sold in the app.

Apple also requires reviewers to receive full access, including a demo account,
fully featured demo mode, sample QR code, hardware, or other resources as
applicable. A reviewer cannot be expected to install six CLIs and configure a
private daemon. A bundled read-only demonstration mode plus a live, bounded
review host/sample QR is the most defensible review path.

This is a release risk to validate early through TestFlight external review and
clear review notes. It is not evidence that the technical product should be
artificially restricted to LAN before Apple evaluates the actual native-client
architecture.

#### F9 — APNs content and trust boundaries need an explicit privacy design (S2)

Apple says push notifications should not carry sensitive personal or
confidential information. Agent prompts, tool arguments, working directories,
session titles, and command output can all be confidential. Copying today's
local notification title/body into a plaintext APNs payload would violate the
product's existing opaque-relay posture and create lock-screen leakage.

APNs also changes the self-hosting boundary. An App Store binary can only
receive pushes sent with credentials controlled by the app publisher's Apple
team. Those credentials cannot be distributed to every `mcremote` or
self-hosted `mcrelay`. Full background parity therefore requires an operated
APNs gateway, even when the session relay remains self-hosted. The gateway can
be zero-knowledge for content, but it will necessarily observe device-token,
host-routing, size, timing, and delivery metadata. That trade-off needs an
opt-in, a documented retention policy, rate limits, deletion/unregister paths,
and an honest privacy policy.

Happy demonstrates the relevant product pattern: mobile coding-agent alerts
through an end-to-end-encrypted relay whose server cannot read message content.
Home Assistant demonstrates the platform split: WebSocket local push is useful
under iOS's restricted-network Local Push conditions, while general reachability
uses push; notification actions default to background execution rather than
forcing the app visible. These are architectural precedents, not code to copy.

#### F10 — security shielding is good but not Android-equivalent (S3 hardening)

Android sets `FLAG_SECURE` before the first frame, preventing screenshots,
recording, and recents thumbnails
(`apps/mobile/android/app/src/main/kotlin/com/maccavelli/magic_cli_remote/MainActivity.kt:9-20`).
iOS has no equivalent general screenshot-prevention flag. The current
`SceneDelegate` adds an opaque view on `sceneWillResignActive`, which follows
Apple's app-switcher snapshot guidance
(`apps/mobile/ios/Runner/SceneDelegate.swift:4-31`). That is correct for
snapshots but does not react to active screen recording, mirroring, or remote
control.

Current UIKit offers scene capture-state observation. The iPhone app should
selectively redact token reveal, credential fields, permission details, and
other high-risk content while capture is active, without blanking harmless
screens. A screenshot has already happened by the time its notification
arrives, so documentation must not promise impossible screenshot prevention.

#### F11 — the Xcode target accidentally claims native iPad support (S3)

`TARGETED_DEVICE_FAMILY = "1,2"` in all Runner configurations
(`apps/mobile/ios/Runner.xcodeproj/project.pbxproj:472-476,602-606,653-659`),
and iPad orientations/assets are present. MADR 0067 explicitly did not commit
to iPad layouts, and no iPad acceptance matrix exists. The product request here
is iPhone support.

The release target should be iPhone-only (`1`) until iPad navigation, adaptive
layout, keyboard/pointer behavior, Stage Manager, screenshots, and review
metadata are deliberately tested. Letting an iPhone app run in compatibility
mode on iPad is different from claiming native iPad support.

#### F12 — product truth, naming, and versioning have drifted (S3)

`apps/mobile/README.md:1-4,112-118` still says Android-only and "iOS / Web not
enabled." The root README says iOS is software-complete but simulator-only
(`README.md:1360-1369`). `pubspec.yaml:2-4` calls the app an Android companion
and pins `0.1.0+1`; iOS has no build-number allocator or distribution override.
`Info.plist:9-10` displays "Magic Cli Remote" rather than the product's "Magic
CLI Remote" spelling.

These are small individually, but they are release blockers collectively:
support claims, app identity, build numbering, store metadata, screenshots,
privacy copy, and distribution instructions need one iPhone source of truth.

#### F13 — iOS idiom and accessibility are not an acceptance dimension yet (S3)

The code has good adaptive beginnings: Cupertino page transitions, safe-area
padding fixes, system pickers, and no hand-built photo browser. It still lacks
a recorded iPhone matrix for VoiceOver, Dynamic Type, Bold Text, Reduce Motion,
light/dark/high-contrast appearance, keyboard/speech permission denial,
landscape, smallest supported width, offline relaunch, memory warnings, and
interruption during camera/speech/file selection.

Functional parity does not require replacing Material widgets with a separate
Cupertino application. It does require that every control have a useful
semantic label, tap targets remain usable, text reflows instead of clipping,
native back gestures work, system permissions are requested at the moment of
use, and destructive/credential actions respect platform authentication and
privacy expectations.

### Root-cause summary

The codebase's main problem is not missing duplicate iOS screen code. It is that
the shared Flutter surface grew quickly while iOS remained a simulator-only
port with no native build gate, no device owner, and an intentionally deferred
background architecture. The result is a wide shared feature surface resting
on an unverified platform foundation.

The priority order follows from that root cause:

1. remove incorrect native behavior and make iOS buildable/reviewable on every
   change;
2. fix notification consent and durable action state;
3. validate the existing shared product on a real iPhone;
4. add the APNs attention plane that closes the core parity gap;
5. distribute through TestFlight and resolve App Review positioning;
6. polish platform idiom only after correctness and delivery are measurable.

### Reassessment outcome at 294aee6

The original option analysis and chosen architecture remain sound. Current
runtime evidence improves confidence in the shared Dart layer and local Apple
toolchain, but it does not alter the product conclusion:

* the host is macOS 26.6.2 with Flutter 3.44.8, Dart 3.12.2, Xcode 26.6,
  CocoaPods 1.17.0, and a clean `flutter doctor -v` result;
* iOS 26.5 iPhone simulators are installed, but `xcrun devicectl list devices`
  reports no physical device and Flutter sees only macOS and Chrome targets;
* `security find-identity -v -p codesigning` reports zero valid identities;
* no source change since the original draft adds APNs, removes the iOS
  foreground-task plugin, supplies iOS CI/archive/distribution, or records a
  real-device pass;
* `make test` passes under the conventional `0022` umask. Under this host's
  unusual default `0077` umask, two existing file-mode assertions fail because
  requested group-readable modes are masked to owner-only. That independent
  baseline issue is not an iPhone defect and is outside this record.

Therefore findings F1-F13 remain open, option E remains the recommended
decision, and the record remains `proposed` until the Project Owner accepts the
operated-gateway and Apple-program obligations.

## Decision Drivers

* A pocketed or terminated iPhone must receive permission, question,
  completion, and error attention without an always-running socket.
* The relay and push operator must not receive prompt, transcript, tool,
  credential, or command plaintext.
* The iOS design must use platform-supported lifecycle mechanisms and avoid
  declaring background modes solely to keep a generic WebSocket alive.
* Existing direct/relay TLS, device identity, permission timeout, owner
  isolation, and signed-receipt invariants must remain authoritative.
* Shared Flutter behavior should stay shared; platform-specific code belongs at
  lifecycle, permission, security, distribution, and integration seams.
* Parity must be proved on a physical iPhone and in an iOS build lane, not
  inferred from Android, Linux, or widget tests.
* Notification permission must be contextual, optional, reversible, and useful
  even when declined.
* App Store review, privacy manifests, privacy labels, export compliance, and
  reviewer access are product requirements, not launch-week paperwork.
* Users who reject the operated push service must retain a fully functional
  foreground client with honest limitations.
* Android behavior must not regress while the shared notification model is
  refactored.

## Considered Options

* **A — Keep iOS foreground-only and call the shared screens parity.** Finish
  device testing and distribution but accept no pocketed-phone alerts.
* **B — Keep the WebSocket alive with background fetch/tasks or an unrelated
  background mode.** Add capabilities around the current connection.
* **C — Use Local Push Connectivity as the primary alert channel.** Add an
  `NEAppPushProvider` and maintain a direct local connection.
* **D — Buy a general push vendor and send current notification payloads.** Use
  a hosted SDK/service such as Firebase or OneSignal for speed.
* **E — Add an opt-in, publisher-operated, content-opaque APNs attention plane
  and retain foreground direct/relay transport for full sessions.**

## Decision Outcome

Chosen option: **"E — Add an opt-in, publisher-operated, content-opaque APNs
attention plane and retain foreground direct/relay transport for full
sessions"**, because it is the only option that closes the defining parity gap
across LAN, tailnet, cellular, suspension, and termination without misusing iOS
background execution or exposing agent content to a push vendor.

The decision is proposed because it introduces an operated service and Apple
Developer Program obligations that require owner approval. Until accepted and
implemented, the truthful product status remains "iOS development target,
simulator-validated in part; not a supported iPhone release."

### D1 — Define parity by outcome and publish a capability matrix

An iPhone release reaches parity when every shared agent/session operation works
on iPhone and the platform-specific equivalents provide the same user outcome:

| Android mechanism | iPhone equivalent | Required outcome |
| --- | --- | --- |
| Foreground service + live WebSocket | APNs attention + reconnect/resync | Alert while pocketed, then current state |
| Shade Allow/Deny over live socket | Authenticated, queued, idempotent action | One deliberate tap resolves a live ask once |
| Notification channels | In-app per-kind settings + iOS app permission/status | Person controls alert kinds and sees blockers |
| `FLAG_SECURE` | App-switcher shield + selective scene-capture redaction | High-risk content is not casually exposed |
| Signed APK updater | TestFlight/App Store release delivery | Supported upgrades preserve pairing/data |
| Android camera/speech/file plugins | iOS system pickers and permissions | Same attachment/input result, native consent |

No Android-only APK installer is to be ported to iOS. No iOS-only OS concept is
to be fabricated on Android. The maintained matrix records **supported**,
**implemented but device-unverified**, **platform-equivalent**, and
**intentionally unavailable**, with evidence links for each status.

### D2 — Keep one Flutter product and explicit platform adapters

Screens, protocol models, state reduction, transport selection, session
operations, notification intent models, and feature tests remain shared.
Platform adapters own:

* background eligibility and lifecycle;
* notification registration, authorization, delivery, and actions;
* secure storage accessibility needed by those actions;
* screen-capture/app-switcher protection;
* distribution and update affordances;
* native integration tests.

`defaultTargetPlatform` checks remain suitable for small rendering decisions.
Behavior with native lifecycle or entitlement consequences must sit behind a
typed interface whose Android and iOS implementations are separately built and
tested. This is how `flutter_foreground_task` is removed from the iOS binary
rather than merely avoided at runtime.

### D3 — Preserve park-on-background and protocol-v2 resume

iOS continues to park the full session WebSocket when backgrounded. Foreground
resume uses the existing protocol-v2 reconnect, pending-ask reconciliation,
history-gap handling, and transport fallback. Background tasks are not used as
a periodic socket keeper.

Apple's Local Push Connectivity may be evaluated later as an optional LAN-only
optimization for restricted networks if the project can obtain the special
`app-push-provider` entitlement. It is not the primary channel: it is scoped to
matching Wi-Fi SSIDs and environments without APNs, and does not solve general
tailnet/cellular reachability.

### D4 — Add a narrow attention protocol, not a second session protocol

The daemon emits only four attention classes: permission, question, turn
complete, and error. The attention plane carries a versioned, bounded,
expiry-aware opaque envelope. It does not carry history, prompts, tool
arguments, provider credentials, workspace content, or a general protocol-v2
tunnel.

The publisher-operated gateway:

* authenticates registered app/device delivery capabilities;
* maps a delivery registration to APNs environment, topic, and device token;
* validates size, expiry, rate, and replay bounds;
* submits a generic APNs notification;
* stores no decryptable agent content;
* exposes unregister/deletion and bounded diagnostics;
* records and documents the metadata it necessarily observes;
* cannot mint `permission.respond` authority.

The existing `mcrelay` trust model and code can inform this service, but session
splicing and push delivery remain separate roles. A self-hosted session relay
does not receive the publisher's APNs private key. A source builder who signs a
different app under their own Apple team may configure their own compatible
gateway.

### D5 — Push content is generic; actionable authority is opaque and bounded

The APNs-visible title/body contains no session name, path, prompt, tool
argument, output, error detail, or credential. Example display copy is
"Agent needs attention," "Agent finished," or "Agent reported an error."
The notification category may identify the coarse class needed for buttons;
all request identifiers and routing capabilities are encrypted to the paired
device or represented by random, one-use, short-lived capabilities.

Rich lock-screen previews are not a v1 goal. If later offered, they require a
separate privacy decision and on-device notification-service-extension
decryption with a generic fallback.

An Allow/Deny action:

1. requires device authentication;
2. is durably recorded before network work;
3. is bound to device, host, request, allowed choice, and expiry;
4. is delivered through a background-capable HTTPS/relay action endpoint or on
   the next foreground connection;
5. is verified by the host against the current pending ask;
6. is idempotent and safe to retry;
7. becomes an expired/tombstone notification when the host has stopped waiting.

The gateway must not be able to change Deny into Allow or replay a result. The
implementation plan must select and threat-model the exact signature or
one-time-capability construction before source changes begin; existing device
P-256 identity and signed-receipt primitives are the preferred starting point.

### D6 — Separate notification setup, consent, and delivery state

Plugin/category initialization must not request authorization. The user flow is:

1. pair successfully or enter an explicit alerts onboarding step;
2. explain what each alert class does and that an opt-in operated APNs gateway
   observes delivery metadata but not content;
3. let the person choose foreground-only or background attention;
4. request iOS authorization in that context;
5. register with APNs/gateway only after consent;
6. show denied/provisional/authorized state and a Settings deep link;
7. unregister and delete gateway state when disabled, signed out, revoked, or
   reinstalled as appropriate.

Declining notifications never blocks pairing or foreground operation. Local
notification initialization is single-flight. Settings' test notifications
remain local tests; gateway health gets a separate end-to-end diagnostic so a
local banner cannot falsely prove background delivery.

### D7 — Remove iOS foreground-task code and complete native privacy posture

Before enabling any iOS background capability:

* exclude `flutter_foreground_task` from the iOS plugin graph and archive;
* verify no unused BGTask registration or foreground-service notification
  delegate remains;
* add an app privacy manifest for app-owned required-reason APIs/data practices;
* verify every linked third-party SDK manifest in the archive;
* generate and review Xcode's aggregate privacy report;
* keep `UIBackgroundModes` absent unless the accepted plan has a narrowly
  justified background-notification use case;
* add the APNs entitlement only when the gateway path exists and is exercised;
* add `remote-notification` background mode only if the accepted design handles
  silent/background notifications and proves that the mode is necessary.

Privacy-policy and App Privacy label data must match runtime behavior,
including APNs token and gateway metadata retention. No tracking or advertising
SDK is introduced.

### D8 — Make iPhone a build and device support tier

The repository gains two complementary gates after an implementation plan is
approved:

* **per-change macOS CI:** resolve dependencies, validate formatting/analysis,
  build the iOS simulator target, run shared tests, run native Runner tests, and
  execute a focused simulator integration flow against a fake daemon;
* **device acceptance:** run the full matrix on at least one smallest-supported
  iPhone-class device and one current iPhone/iOS combination, over direct and
  relay paths, with notification, network, suspend, terminate, camera, speech,
  picker, Keychain, capture, and upgrade cases.

An unsigned simulator build is the early structural gate. Signed archive,
privacy report, and TestFlight upload are release gates. A simulator is not
evidence for camera, APNs, suspension, Keychain uninstall, thermal/memory, or
tailnet behavior.

### D9 — Distribute to TestFlight before claiming App Store support

The project enrolls in the paid Apple Developer Program, creates explicit
development/distribution provisioning, establishes monotonically increasing
iOS build numbers, and produces a reproducible `flutter build ipa`/Xcode
archive. Internal TestFlight is the first daily-driver channel; external
TestFlight is the first review rehearsal.

TestFlight/App Store is the iOS update mechanism. Settings shows the installed
version and, if useful, an App Store/TestFlight management link, but does not
download or install IPA files. Export compliance is answered and recorded for
each distribution configuration.

### D10 — Treat App Review as a tested product path

Before an App Store submission:

* ship an easily accessible in-app privacy-policy link;
* maintain accurate App Privacy answers and data-retention/deletion behavior;
* provide a bounded, non-destructive, read-only demo mode that exercises the
  major screens without external CLIs;
* provide App Review a live review host/sample QR for pairing and full protocol
  behavior, with stable availability and no personal data;
* attach a physical-device pairing video if helpful;
* explain the native protocol-client architecture, user-owned host, opaque
  relay, provider credential location, no store, and no app primary account;
* explicitly address why guideline 4.2.7 does or does not apply rather than
  hoping the reviewer infers it;
* keep backend/gateway services live for the review window.

App Store acceptance is a release criterion, not a prerequisite for internal
TestFlight parity. A rejection that requires changing relay reachability,
account flow, or provider surfaces triggers an amendment to this MADR and a new
owner decision rather than an opportunistic code change.

### D11 — Scope this release to iPhone and finish platform idiom deliberately

The native target is iPhone-only until a separate iPad support decision. The
iPhone acceptance pass covers:

* VoiceOver labels, focus order, and announcement of streamed/status changes;
* Dynamic Type at accessibility sizes with no clipped controls;
* Bold Text, Reduce Motion, light/dark/high-contrast appearance;
* safe areas, landscape, keyboard, predictive/Cupertino back gestures;
* system photo/file pickers and just-in-time camera/microphone/speech access;
* selective scene-capture redaction plus the existing app-switcher shield;
* memory pressure for long transcripts and maximum bounded attachments;
* interruptions and permission denial/revocation.

The display name is normalized to "Magic CLI Remote," and documentation stops
describing iOS as both disabled and complete.

### Required implementation sequence

This is sequencing guidance for the associated implementation plan, not
authorization to mutate source:

1. **Foundation:** platform plugin split; permission single-flight/context;
   iPhone-only target; privacy manifest/report; truthful docs; macOS simulator
   build/native smoke gate.
2. **Device baseline:** paid team and real iPhone; close existing F1-F6 plus all
   post-0067 shared surfaces; fix only defects covered by the approved plan.
3. **Attention protocol and gateway:** threat model, registration/token
   lifecycle, opaque event delivery, queued authenticated actions, revocation,
   rate/abuse controls, failure diagnostics.
4. **APNs app integration:** entitlements, registration, categories, terminated
   delivery, action processing, generic content, settings/consent.
5. **Distribution:** signed archive, build-numbering, internal then external
   TestFlight, upgrade/reinstall validation, crash and delivery diagnostics.
6. **App Store readiness:** privacy policy/labels, demo/review host, metadata,
   accessibility/idiom pass, review submission and recorded outcome.

A complete `0121-PLAN-achieve-iphone-functional-parity.md` must enumerate exact
files, commands, acceptance criteria, phase commits, gateway operations, and
rollback before any implementation begins.

## Consequences

### Positive

* Good, because iPhone parity becomes a measurable support claim rather than an
  inference from shared Dart code.
* Good, because APNs solves the actual suspended/terminated attention problem
  without pretending iOS can run Android's foreground-service design.
* Good, because generic opaque pushes preserve the existing relay's
  content-confidentiality goal and reduce lock-screen exposure.
* Good, because queued idempotent actions fix both current resume/cold-launch
  races and future push-action retries.
* Good, because removing the unused foreground-task plugin shrinks native
  lifecycle, notification, privacy-manifest, and review surface.
* Good, because physical-device and macOS CI gates stop later shared features
  from silently regressing iOS.
* Good, because TestFlight supplies a sustainable daily-driver update path and
  feedback channel before public App Store claims.
* Good, because foreground-only/no-gateway remains an honest privacy-preserving
  mode for people who reject operated push.

### Negative

* Bad, because full background parity creates a publisher-operated service and
  ongoing APNs availability, security, abuse, privacy, and cost obligations.
* Bad, because the gateway necessarily observes delivery metadata even when it
  cannot read content.
* Bad, because a paid Apple Developer Program membership, signing assets,
  App Store Connect administration, and macOS CI capacity become permanent
  project dependencies.
* Bad, because reliable background actions add a new protocol and cryptographic
  state machine that must coexist with permission timeout and revocation.
* Bad, because App Review may still interpret the product under guideline 4.2.7
  or adjacent chatbot/plugin rules, forcing a later product decision.
* Bad, because iPhone-only scope defers native iPad support despite the current
  Xcode target accidentally advertising it.

### Neutral

* Neutral, because Android keeps its existing foreground-service delivery; the
  implementation differs while outcomes converge.
* Neutral, because local notifications remain useful while foregrounded, but
  they no longer define background parity.
* Neutral, because TestFlight builds expire and are not a permanent public
  distribution channel; App Store work follows separately.
* Neutral, because this decision extends MADR 0067 D3 and the later parity
  backlog without rewriting the historical simulator-port decision.

## Confirmation

Parity is confirmed only when every applicable row below has an evidence link,
date, device/OS, transport, build number, and pass result. Unit-only or
simulator-only evidence cannot close a row marked **device**.

### Build, archive, and native composition

| Criterion | Evidence required |
| --- | --- |
| Every PR builds the iOS simulator target | Green macOS CI receipt |
| Runner native tests are meaningful and green | `xcodebuild test` receipt |
| Release archive contains no iOS `flutter_foreground_task` code | Archive/link inspection |
| Only chosen background/APNs capabilities exist | Entitlement and `Info.plist` inspection |
| Privacy manifests aggregate cleanly | Xcode privacy report reviewed and stored |
| Signed IPA has expected bundle id, version, build, and team | Archive/export inspection |
| TestFlight install and upgrade preserve pairing and local state | Device evidence |

### Existing product surface on iPhone

The device matrix must exercise code/QR/token pairing; Local Network allow and
deny; self-signed and public-chain TLS; direct mesh/LAN and relay; reconnect and
ten park/resume cycles; all provider/session/model/mode/thinking operations;
streaming chat; cancel; permissions; questions; provider auth; receipts and
handoff; sharing; workspace inspection; diagnostics; skill authoring; shell
commands; image/HEIC and bounded audio attachments; QR; long speech; transcript
cache/relaunch; sign-out/revoke; and delete/reinstall Keychain behavior.

Every shared feature added after 0067 is either passed on iPhone or explicitly
classified as a platform-equivalent/non-goal with owner approval. "No iOS gate
in Dart" is not evidence.

### Background attention and action correctness

| Scenario | Required result |
| --- | --- |
| Foreground, background, suspended, terminated | Each enabled attention class arrives once within its declared service objective |
| Direct host unreachable but gateway reachable | Generic alert still arrives; app later reconciles current host state |
| Phone offline then online before expiry | Delivery/action retries without duplication |
| Ask expires before action | No agent power; actionable alert becomes passive tombstone |
| Allow/Deny from locked notification | Device authentication; exactly one host result |
| Cold action with no session socket | Intent persists, reconnects/delivers or reports expiry; choice is not lost |
| Duplicate/replayed/tampered action | Host rejects or idempotently returns prior result |
| Device revoke, sign-out, alerts off, APNs-token rotation | Old delivery registration cannot alert or act |
| Gateway unavailable | Foreground app remains fully usable; UI reports background-attention degradation honestly |
| Relay/gateway/APNs observation | Captured requests contain no prompt, path, tool, transcript, output, credential, or session-title plaintext |

### Privacy, security, accessibility, and review

* In-app notification consent precedes the system prompt; decline preserves
  foreground use; settings show actual authorization and gateway state.
* Privacy policy, App Privacy answers, gateway retention/deletion, and observed
  traffic agree.
* Token-reveal and other designated high-risk views redact during active scene
  capture; the app-switcher snapshot remains opaque.
* VoiceOver and accessibility Dynamic Type complete the primary pairing,
  session, chat, permission, settings, and recovery flows.
* iPhone orientations and smallest supported width produce no overflow, hidden
  action, unreachable dialog, or keyboard obstruction.
* App Review can use the demo and review host without developer intervention;
  review notes accurately describe relay, execution, accounts, and provider
  auth.
* External TestFlight review and, when public distribution is claimed, App
  Store review pass with the same binary behavior.

## Pros and Cons of the Options

### A — Keep iOS foreground-only

* Good, because it preserves a fully self-hosted, no-operated-service posture.
* Good, because it is the smallest release and privacy surface.
* Bad, because permission asks and completions do not reach a pocketed phone,
  which fails the central mobile use case and Android outcome parity.
* Bad, because labeling this "full parity" would make the support claim false.

### B — Background fetch/tasks or an unrelated background mode

* Good, because it appears to reuse the current WebSocket and avoids a new
  service.
* Bad, because background refresh is discretionary and periodic, not a timely
  inbound message channel.
* Bad, because declaring audio, location, VoIP, or another unrelated mode would
  violate platform intent and create battery/App Review risk.
* Bad, because the current foreground-task plugin already demonstrates the
  hidden lifecycle surface this option would expand.

### C — Local Push Connectivity primary

* Good, because content can stay directly between a local host and device.
* Good, because it is an Apple-supported persistent local alert mechanism for
  its intended restricted-network use case.
* Bad, because it requires a specially requested Network Extension entitlement
  and configured matching Wi-Fi SSIDs.
* Bad, because it does not solve cellular, ordinary off-mesh relay, or general
  tailnet use; Apple itself describes APNs as the usual fallback.

### D — General push vendor with current payloads

* Good, because SDKs, dashboards, token lifecycle, and delivery diagnostics can
  shorten a prototype.
* Bad, because it adds another party and SDK while APNs and an authenticated
  provider still exist underneath.
* Bad, because sending current notification bodies exposes confidential agent
  context and conflicts with Apple's push-content guidance.
* Bad, because it weakens the project's existing self-hosted/opaque-relay trust
  story without being required for the core mechanism.

### E — Content-opaque APNs attention plane (chosen)

* Good, because it works in the lifecycle states and networks that define the
  parity gap.
* Good, because the full session protocol remains end-to-end on the existing
  transport; push is a narrow attention/capability plane.
* Good, because generic content and end-to-end opaque routing minimize both
  gateway and lock-screen disclosure.
* Good, because an explicit opt-in preserves a foreground-only privacy mode.
* Bad, because the publisher must operate and secure a highly available gateway
  and accurately disclose its metadata.
* Bad, because the action protocol and App Review path are substantive new
  work, not a Flutter configuration switch.

## More Information

### Evidence index

| Claim | Repository evidence |
| --- | --- |
| iOS is intentionally park/resume | `apps/mobile/lib/app_lifecycle.dart:173-204`; MADR 0067 D2 |
| Android background alerts use FGS | `apps/mobile/android/app/src/main/AndroidManifest.xml:17-34,112-133`; `foreground_service.dart:24-140` |
| iOS alert permission is automatic | `notification_service.dart:65-111`; `app_lifecycle.dart:74-116`; `settings_store.dart:504-509` |
| Action can precede reconnect | `notification_coordinator.dart:215-261`; `app_lifecycle.dart:207-271` |
| Android plugin is linked on iOS | `pubspec.yaml:38-40`; `ios/Podfile.lock:1-13`; resolved plugin 10.0.0 source |
| No push/background configuration | `ios/Runner/Info.plist`; `ios/Runner/Runner.entitlements:1-14` |
| No iOS CI or release artifact | `.github/workflows/ci.yml:357-405`; `README.md:1400-1412` |
| Native test is a stub | `ios/RunnerTests/RunnerTests.swift:5-9` |
| All physical iPhone gates are parked | `docs/ops-hardware-validation.md:329-346` |
| Signing is simulator/free-team only | `docs/ops-ios-signing.md:7-47` |
| Only later iOS smoke is a stubbed simulator UI | `docs/0112-PLAN-opencode-1.18.21-surface-parity.md:1878-1899` |
| iPad is unintentionally targeted | `ios/Runner.xcodeproj/project.pbxproj:472-476,602-606,653-659` |
| Product docs disagree | `apps/mobile/README.md:1-4,112-118`; `README.md:1360-1369`; `pubspec.yaml:2-4` |

### Official platform references

* [Apple: Configuring background execution modes](https://developer.apple.com/documentation/xcode/configuring-background-execution-modes) — background apps are normally suspended; use limited modes sparingly and for their intended service.
* [Apple: Handling notifications and notification-related actions](https://developer.apple.com/documentation/usernotifications/handling-notifications-and-notification-related-actions) — actions can launch the app in the background without foregrounding it.
* [Apple: Asking permission to use notifications](https://developer.apple.com/documentation/usernotifications/asking-permission-to-use-notifications) — request authorization in explanatory context, not automatically on first launch.
* [Apple: Local Push Connectivity](https://developer.apple.com/documentation/networkextension/local-push-connectivity) — restricted matching-SSID use, special entitlement, and APNs fallback model.
* [Apple: Privacy manifest files](https://developer.apple.com/documentation/bundleresources/privacy-manifest-files) and [required-reason APIs](https://developer.apple.com/documentation/bundleresources/describing-use-of-required-reason-api) — app/SDK responsibility and bundle-local declarations.
* [Apple: Third-party SDK requirements](https://developer.apple.com/support/third-party-SDK-requirements/) — developers remain responsible for linked SDK code; `connectivity_plus` is on Apple's named list.
* [Apple: Preparing UI for the background](https://developer.apple.com/documentation/uikit/preparing-your-ui-to-run-in-the-background) and [protecting sensitive content during screen sharing](https://developer.apple.com/documentation/swiftui/protecting-sensitive-content-when-screen-sharing) — snapshot shielding and current scene-capture response.
* [Apple: App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/) — 2.5.4 background modes, 4.2.7 remote clients, 4.5.4 confidential push content, 5.1.1 privacy policy/data minimization, and full reviewer access.
* [Apple: TestFlight overview](https://developer.apple.com/help/app-store-connect/test-a-beta-version/testflight-overview) and [export compliance](https://developer.apple.com/help/app-store-connect/manage-app-information/overview-of-export-compliance).
* [Flutter: Continuous delivery](https://docs.flutter.dev/deployment/cd) — validate `flutter build ipa` and use fastlane or Xcode Cloud for iOS delivery.

### Comparable product references

* [Happy mobile client](https://github.com/slopus/happy) and [Happy Server](https://github.com/slopus/happy/tree/main/packages/happy-server) — a directly comparable coding-agent mobile client advertising iOS/Android push through end-to-end-encrypted synchronization where the relay cannot read content.
* [Home Assistant local push](https://companion.home-assistant.io/docs/notifications/notification-local/) — WebSocket local push as a constrained network-specific path rather than a universal iOS background socket.
* [Home Assistant actionable notifications](https://companion.home-assistant.io/docs/notifications/actionable-notifications/) — background action handling, unique action identity, timeout awareness, and multi-server routing.
* [`flutter_foreground_task` 10.0.0](https://pub.dev/packages/flutter_foreground_task/versions/10.0.0) — the exact resolved plugin version whose local iOS source and configuration requirements were inspected.

### Related project records

* [0067-MADR-ios-port.md](0067-MADR-ios-port.md) — simulator-first port,
  foreground lifecycle, Keychain, Local Network prompt, deferred background
  attention, and the still-open hardware matrix.
* [0068-MADR-protocol-v2-reconnect-resilient-transport.md](0068-MADR-protocol-v2-reconnect-resilient-transport.md) — reconnect/resume foundation this decision preserves.
* [0070-MADR-deep-dive-debugging-pass.md](0070-MADR-deep-dive-debugging-pass.md) and [0071-MADR-codebase-assessment.md](0071-MADR-codebase-assessment.md) — prior records that identify APNs and iPhone hardware as residual product work.
* [0101-MADR-android-agent-alert-delivery.md](0101-MADR-android-agent-alert-delivery.md) — the Android alert outcome and timeout/tombstone behavior iPhone must match.
* [ops-hardware-validation.md](ops-hardware-validation.md) and [ops-ios-signing.md](ops-ios-signing.md) — current device and provisioning source of truth.
