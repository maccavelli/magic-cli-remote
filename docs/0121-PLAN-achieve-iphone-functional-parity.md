---
status: proposed
date: 2026-08-28
associated-madr: 0121-MADR-achieve-iphone-functional-parity.md
owners: Project Owner
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0121 — Achieve iPhone functional parity

This is the implementation plan for
[MADR 0121](0121-MADR-achieve-iphone-functional-parity.md). It implements the
proposed content-opaque APNs attention plane and the iPhone build, device,
distribution, privacy, accessibility, and review gates around it. It is not a
second architecture decision.

## Authorization and execution contract

The MADR and this plan are `proposed`. Writing these two documents is the
repository's documentation bootstrap exception; no source, test, generated
file, dependency, configuration, build artifact, signing state, live service,
or external Apple resource may be changed until the Project Owner explicitly
accepts MADR 0121 and authorizes this plan or a named phase.

Execution is sequential. A later phase does not begin merely because an earlier
phase passed. Before each commit:

1. update the phase evidence in the repository documents named by that phase;
2. run every phase-specific check plus the common checks below;
3. run `make pre-add-check FILES="<changed Go files>"` before staging any Go
   file;
4. run `dart format` on changed Dart files before staging them;
5. inspect `git diff --check` and `git diff --cached`;
6. commit with `git commit --no-edit`, allowing the repository hook to generate
   the message;
7. do not push, tag, publish an image, upload a build, create an App Store
   Connect object, or submit for review unless the owner explicitly authorizes
   that external action in the same turn.

Any discovery outside the exact phase scope stops execution. Amend this MADR
and plan, obtain approval, then continue. In particular, phase 3 records
physical-device defects but does not authorize an open-ended repair sweep.

## Goal and release definition

Deliver one shared Flutter product whose iPhone support claim is backed by:

* the complete shared product surface on physical iPhones over direct and relay
  transports;
* visible, generic APNs attention for permissions, questions, turn completion,
  and agent errors while the app is foregrounded, suspended, or terminated;
* authenticated, durable, idempotent Allow/Deny actions that cannot grant agent
  power after the host ask expires;
* foreground-only operation for users who decline publisher-operated push;
* an iPhone-only, privacy-reviewed, signed archive installed and upgraded
  through TestFlight;
* reproducible simulator/native CI, a physical-device matrix, accessibility
  evidence, truthful product documentation, and an exercised App Review path.

The release claim remains “iOS development target; not a supported iPhone
release” until every mandatory acceptance criterion in this plan is satisfied.

## Grounded starting point

The plan is based on repository and runtime facts at `294aee6` on 2026-08-28:

| Area | Verified baseline | Consequence for execution |
| --- | --- | --- |
| Working state | `HEAD` and `origin/master` are `294aee6`; tree was clean before diagnostics | Rebase/reassess if execution starts from another commit |
| Shared tests | `flutter test`: 1,310 passed, 3 skipped; `flutter analyze`: no issues; Dart format: 193 files unchanged | These become no-regression gates, not iPhone proof |
| iOS-specific tests | 6 of 115 test files explicitly select iOS; no `integration_test/`; native Runner test is a template | Add native and end-to-end coverage before support claims |
| Toolchain | macOS 26.6.2, Flutter 3.44.8, Dart 3.12.2, Xcode 26.6, CocoaPods 1.17.0; `flutter doctor -v` green | Pin the same Flutter and Xcode line in CI first |
| Runtime targets | iOS 26.5 simulators exist; no physical device is attached; Flutter lists macOS/Chrome only | Simulator work can start after approval; device phase is a hard gate |
| Signing | `security find-identity -v -p codesigning` reports 0 valid identities | Paid team/certificates/profiles are external prerequisites |
| Lifecycle | iOS parks its WebSocket; Android uses `flutter_foreground_task` | Keep park/resume; replace the Android dependency before iOS work |
| Native composition | `flutter_foreground_task` 10.0.0 is in `Podfile.lock` and registers iOS app/scene delegates | Runtime Dart guards are insufficient; remove it from the dependency graph |
| Notifications | initialization requests authorization; no in-flight init future; action response can race reconnect | Separate setup/consent and persist action intent before network use |
| APNs | no APS entitlement, registration callback, gateway, or distribution credentials | Build the narrow attention plane described below |
| Distribution | no iOS CI, archive, IPA, signing, TestFlight, or App Store metadata lane | Add structural CI before signed delivery |
| Hardware evidence | all existing iPhone F1-F6 rows are parked | A simulator cannot close the release matrix |
| Host test qualification | `umask 0022 && make test` passes; two unrelated file-mode tests fail under host default `0077` | Use `umask 0022` for this plan and do not absorb that separate defect |

If any baseline command differs at execution start, record the new output and
classify it before editing. A pre-existing unrelated failure is documented and
kept out of phase scope; a regression caused by this plan is fixed before the
phase can pass.

## Fixed architecture contracts

These contracts resolve the implementation choices left open by the MADR. A
change to any contract requires an approved MADR/plan amendment.

### Product and lifecycle boundary

* The full protocol-v2 WebSocket remains the only session transport. iOS parks
  it in the background and reconnects/resynchronizes in the foreground.
* APNs is an opt-in attention plane, not a second session protocol. It carries
  four coarse kinds only: `permission`, `question`, `turn_complete`, and
  `agent_error`.
* V1 uses visible alert pushes. It does not use silent push, background fetch,
  `BGTaskScheduler`, a notification service extension, or
  `UIBackgroundModes=remote-notification`.
* Questions, completion, and errors open the relevant app surface after normal
  reconnect. Only permission notifications expose background Allow/Deny.
* Android retains its foreground-service outcome, implemented by app-owned
  Kotlin rather than a cross-platform dependency that links native iOS code.

### Service boundary and configuration

Create a third Go binary, `mcpush`, separate from `mcremote` and `mcrelay`.
`mcpush` terminates HTTPS behind the publisher's reverse proxy, stores delivery
routes and short-lived action envelopes in PostgreSQL 17, and talks directly to
APNs over HTTP/2. It never terminates or forwards protocol-v2 session traffic.

The iOS binary contains one compile-time HTTPS gateway URL supplied with
`--dart-define=MC_ATTENTION_GATEWAY_URL=...`. The executing owner must supply
the final production URL before phase 6; no example hostname is committed as a
shipping default. The daemon accepts a route only when its URL exactly matches
one of `attention.allowed_gateway_urls`. Production defaults to attention off
and an empty allowlist. Tests may allow an explicit loopback HTTP server.

New daemon configuration is:

```yaml
attention:
  enabled: false
  allowed_gateway_urls: []
  request_timeout_seconds: 10
  outbox_max_events: 1024
```

Environment bindings are `MCREMOTE_ATTENTION_ENABLED`,
`MCREMOTE_ATTENTION_ALLOWED_GATEWAY_URLS`,
`MCREMOTE_ATTENTION_REQUEST_TIMEOUT_SECONDS`, and
`MCREMOTE_ATTENTION_OUTBOX_MAX_EVENTS`. No capability or APNs token is accepted
from YAML or a CLI flag.

### Route capabilities and storage

After pairing and explicit consent, the iPhone creates a route with `mcpush`.
The request contains the APNs environment, bundle topic, current APNs token,
and SHA-256 fingerprint of the already-enrolled P-256 device public key.
`mcpush` returns:

* a 128-bit random opaque `route_id`;
* a 256-bit `publish_cap`, transferred by the phone to its paired daemon;
* a 256-bit `consume_cap`, transferred by the phone to its paired daemon;
* a 256-bit `manage_cap`, retained only by the phone.

All identifiers/capabilities use unpadded base64url. Capability values are
shown once and never logged. PostgreSQL stores only
`SHA-256(capability || per-row salt)`; constant-time comparison follows an
indexed route lookup. The daemon stores its route/capabilities in an atomic,
owner-only file beneath `<data_dir>/attention/`; the phone stores its route and
manage capability in Keychain-backed `SettingsStore`. APNs tokens exist only on
the phone transiently and encrypted in the gateway database.

`mcpush` encrypts each APNs token with AES-256-GCM. The 32-byte master key and a
versioned key identifier come from the deployment secret manager, never the
repository or database. Associated data binds ciphertext to route ID, topic,
environment, and key version. Key rotation rewrites ciphertext online and
keeps the prior key read-only until every row is migrated.

Route lifecycle is explicit:

* APNs registration is requested on every authorized app launch; a changed
  token updates the route using `manage_cap`.
* alerts off, background-attention off, sign-out, device revoke, or app identity
  reset deletes both gateway and daemon route state;
* APNs `410 Unregistered` disables and deletes the token immediately;
* a route with no successful authenticated activity for 90 days is deleted;
* a device may own at most one active route for a given gateway/topic/environment.

### Attention event and APNs payload

The daemon creates a random 128-bit `event_id`, coarse kind, UTC creation time,
authoritative expiry, and a random 256-bit `action_id` only for actionable
permissions. It stores the sensitive mapping from `action_id` to device ID,
session ID, permission ID, allowed options, and host expiry locally. The
gateway never receives that mapping.

The JSON published to `mcpush` is bounded to 2 KiB and contains only:

```json
{
  "v": 1,
  "event_id": "opaque-base64url",
  "kind": "permission",
  "expires_at": "RFC3339 UTC",
  "action_id": "opaque-base64url"
}
```

`action_id` is omitted for non-actionable events. The APNs alert is constructed
by the gateway from the coarse kind. Titles/bodies are fixed strings such as
“Agent needs attention” and never accept caller-supplied text. The payload uses
`apns-push-type: alert`, `apns-priority: 10`, the host expiry as
`apns-expiration`, the event ID as `apns-collapse-id`, and category
`MC_PERMISSION_V1` only for permission. It contains no session title, host
name, path, prompt, tool, arguments, transcript, output, credential, error
detail, model, provider, or device name.

The daemon owns retry state. The gateway submits immediately to APNs and keeps
only an idempotency digest/status, not the payload. Transient APNs/gateway
failure leaves the event in the daemon outbox for exponential retry bounded by
expiry. Permanent APNs errors disable the route and surface a coarse status to
the phone on reconnect.

### Background permission action

Both `MC_ALLOW_V1` and `MC_DENY_V1` are `authenticationRequired` notification
actions without the `foreground` option. The background Dart entrypoint performs
only bounded work:

1. write an action intent to an atomic owner-only queue before networking;
2. load the existing P-256 device identity from Keychain;
3. create a compact ES256 JWS over canonical JSON containing version, route ID,
   action ID, choice, device public-key fingerprint, issued-at, and host expiry;
4. POST the JWS to `mcpush` using `manage_cap`;
5. retain and retry the intent until the gateway accepts it, the host confirms
   it, or authoritative expiry passes.

The JWS payload contains only the coarse choice and opaque identifiers. The
gateway checks bounds and route/fingerprint binding but cannot create a valid
signature. It durably queues the compact JWS until the daemon long-polls with
`consume_cap` or expiry passes. The daemon verifies ES256 against the public key
in `internal/auth.Store`, validates device/route/action binding, checks the
current pending permission and its host-authored expiry, and invokes the same
session-manager permission response path as WebSocket `permission.respond`.

The daemon action ledger is authoritative and idempotent. First valid delivery
records `pending`, then the terminal host result. Replays return the recorded
result. Unknown, tampered, wrong-device, wrong-route, invalid-choice, or expired
actions never call the provider. A crash after provider response but before
ledger completion is reconciled against the manager's pending-ask state and
fails closed; it cannot convert a deny/timeout into allow.

### Gateway retention, abuse, and observability

PostgreSQL retention is fixed:

| Data | Retention |
| --- | --- |
| Active route and encrypted APNs token | Until delete, APNs 410, or 90 days inactive |
| Publish idempotency digest and APNs status | 24 hours |
| Pending signed action envelope | Until host expiry, capped at 15 minutes |
| Action terminal digest/status | 24 hours |
| Aggregated delivery metrics | 30 days |
| Structured request logs | 7 days, with tokens/capabilities/JWS/payloads excluded |

The gateway applies per-IP route-creation limits, per-route publish/action
limits, a global body limit, database timeouts, and maximum outstanding actions.
It exposes `/health/live`, `/health/ready`, and authenticated/loopback-only
Prometheus metrics. Metrics identify result classes, APNs environment, and
coarse kind only. They never label route, device, token, capability, action,
host, session, or IP. Operations include alerts for sustained 5xx responses,
APNs authentication failure, queue age near expiry, database saturation, and
unexpected route growth.

## External inputs and hard stop gates

These inputs cannot be inferred or safely created by source changes. Phase 0
does not pass until the owner supplies and records them in the named private
operator systems, not this repository:

1. active paid Apple Developer Program team and App Store Connect access;
2. final bundle ID and display name, with `com.maccavelli.magicCliRemote` and
   “Magic CLI Remote” used unless the owner explicitly changes the decision;
3. development and distribution signing identities and matching profiles;
4. APNs Auth Key ID, Team ID, `.p8` secret, production gateway HTTPS URL, and
   DNS/TLS ownership;
5. App Store Connect app record, privacy-policy public URL, support URL, and
   reviewer contact;
6. PostgreSQL 17 service, secret manager, reverse proxy/load balancer, backup
   location, metrics sink, and an on-call owner for `mcpush`;
7. at least one smallest-supported physical iPhone and one current
   iPhone/current-iOS device. One device may satisfy both only if it is also the
   smallest supported class; otherwise two devices are mandatory;
8. a bounded review daemon/relay environment containing no personal data and
   credentials for App Review;
9. explicit owner decisions for privacy policy, App Privacy answers, export
   compliance, TestFlight groups, and whether public App Store submission is in
   the authorized release scope.

Secrets, certificates, provisioning profiles, App Store Connect API keys, APNs
tokens, capability values, database dumps, and reviewer credentials must never
be committed or printed in CI logs.

## Common verification commands

Run from repository root unless a phase says otherwise. The controlled umask
is part of the reproducible baseline, not a workaround for a plan regression.

```bash
git diff --check
umask 0022 && make test
go test -race ./...
go vet ./...
make verify-build-metadata
cd apps/mobile && dart format --output=none --set-exit-if-changed .
cd apps/mobile && flutter analyze
cd apps/mobile && flutter test
```

When Go files change, also run `make pre-add-check FILES="..."` with every
changed `.go` file. When Dart files change, format only those files first, then
run the repository-wide no-change command above. When iOS project/native files
change, run the simulator build and native test script created in phase 2.
When Android native/service files change, run the Android debug build and its
unit tests. When docs change, run the repository's pinned Markdown linter over
the changed documents.

## Phase 0 — Accept the decision and lock external inputs

### Work

* Obtain explicit acceptance of MADR option E and execution authorization.
* Confirm every external input above and record only non-secret identifiers in
  the operator runbook: Apple team identifier, bundle ID, gateway URL, APNs key
  ID, PostgreSQL environment name, privacy/support URLs, device models/OS, and
  accountable owners.
* Change MADR status to `accepted` and this plan status to `accepted`. Do not
  mark either implemented.
* Capture a fresh `git status`, commit, toolchain, simulator, physical-device,
  codesigning-identity, Flutter-test, analyzer, and Go-test baseline in
  `docs/ops-ios-signing.md` and `docs/ops-hardware-validation.md`.

### Files

* `docs/0121-MADR-achieve-iphone-functional-parity.md`
* `docs/0121-PLAN-achieve-iphone-functional-parity.md`
* `docs/ops-ios-signing.md`
* `docs/ops-hardware-validation.md`

### Commands

```bash
git status --short
git rev-parse HEAD
sw_vers
flutter --version
dart --version
xcodebuild -version
pod --version
flutter doctor -v
xcrun simctl list devices available
xcrun devicectl list devices
security find-identity -v -p codesigning
umask 0022 && make test
cd apps/mobile && flutter analyze
cd apps/mobile && flutter test
```

### Exit criteria and commit

All nine external-input gates are resolved, no secret is in the diff, current
baselines are recorded, and the owner has authorized execution. Commit the
documentation-only status/evidence change. Stop if membership, devices,
gateway ownership, privacy ownership, or operated-service responsibility is
unresolved.

## Phase 1 — Remove the cross-platform foreground-task dependency

### Work

Replace `flutter_foreground_task` with an app-owned Android service and a narrow
method channel. Preserve the existing Android contract: `remoteMessaging`
foreground-service type, notification channel/ID, start-on-connect,
stop-on-disconnect, notification text updates, boot/restart non-behavior, and
`stopWithTask` semantics. Dart remains platform-neutral and receives a typed
controller whose iOS implementation is a true no-op because no related native
plugin exists.

The Kotlin service must call `startForeground` promptly, handle duplicate
start/stop/update operations idempotently, reject malformed method arguments,
and expose `isRunning`. It must not persist session content or add receivers,
boot permissions, wake locks, background-fetch identifiers, or iOS code.

Add an archive/composition assertion that fails if the iOS plugin registrant,
CocoaPods lock, built frameworks, symbols, or generated dependency metadata
contains `flutter_foreground_task`, `BGTaskSchedulerPermittedIdentifiers`, or
the plugin's task identifier.

### Files

* `apps/mobile/android/app/src/main/kotlin/com/maccavelli/magic_cli_remote/HostConnectionService.kt` (new)
* `apps/mobile/android/app/src/main/kotlin/com/maccavelli/magic_cli_remote/MainActivity.kt`
* `apps/mobile/android/app/src/main/AndroidManifest.xml`
* `apps/mobile/lib/data/notifications/foreground_service.dart`
* `apps/mobile/test/foreground_service_test.dart` (new)
* `apps/mobile/pubspec.yaml`
* `apps/mobile/pubspec.lock`
* `apps/mobile/ios/Podfile.lock`
* `apps/mobile/ios/Runner/GeneratedPluginRegistrant.m` if regenerated and tracked
* `scripts/assert-ios-composition.sh` (new)
* `Makefile`

### Verification

```bash
cd apps/mobile && flutter pub get
cd apps/mobile && flutter test test/foreground_service_test.dart test/notifications_test.dart
cd apps/mobile && flutter build apk --debug
cd apps/mobile && flutter build ios --simulator --no-codesign
./scripts/assert-ios-composition.sh apps/mobile/build/ios/iphonesimulator/Runner.app
rg -n "flutter_foreground_task|BGTaskScheduler|com.pravera.flutter_foreground_task" apps/mobile/ios apps/mobile/build/ios/iphonesimulator/Runner.app
```

The final `rg` must return no match; the assertion script is the automated
version of that requirement. Run the common gates and pre-add checks.

### Exit criteria and commit

Android starts/stops/updates the app-owned service through tests and a debug
build; its manifest still declares only the intended foreground-service
surface; the iOS simulator app builds with no foreground-task code or BGTask
registration. Commit phase 1 alone. A failure to preserve Android behavior or
remove the iOS binary surface blocks phase 2.

## Phase 2 — Establish iPhone-native build, consent, and privacy foundations

### Work

1. Make `NotificationService.init()` single-flight and category-only. Add a
   separately invoked `requestAuthorization()` and a typed authorization-state
   query. Subscribe/replay only after the initialization future resolves.
2. Replace the Boolean preference ambiguity with an explicit delivery mode:
   `off`, `foregroundOnly`, or `backgroundAttention`. A fresh iOS install starts
   at `off`; Android migration preserves its current enabled behavior. Existing
   iOS installs migrate to `foregroundOnly` without triggering an OS prompt.
3. Present explanatory copy after successful pairing or from Settings. Only a
   direct user action requests iOS permission. Denial leaves pairing and all
   foreground protocol behavior available and supplies a Settings deep link.
4. Normalize the display name to “Magic CLI Remote,” describe iPhone support
   truthfully, and change every Runner configuration to iPhone-only
   `TARGETED_DEVICE_FAMILY=1`.
5. Add an app-owned privacy manifest with tracking false and only declarations
   actually attributable to app-owned native code. Do not duplicate plugin
   declarations or invent required-reason categories.
6. Extend the scene shield with a testable capture-state adapter. Redact only
   designated high-risk Flutter surfaces; retain the app-switcher shield. Do
   not claim screenshot prevention.
7. Replace the Runner test template with tests for plugin registration absence,
   privacy-shield lifecycle, capture notifications, display name, device family,
   and the absence of unrelated background modes.
8. Add an iOS CI job on `macos-26`, pin Flutter 3.44.8, select Xcode 26.6,
   resolve CocoaPods, build the simulator app, run Runner tests, run the shared
   suite, and run the composition assertion. A script selects an available
   iPhone simulator by UDID rather than pinning a runner-specific device name.

### Files

* `apps/mobile/lib/data/notifications/notification_service.dart`
* `apps/mobile/lib/data/notifications/notification_coordinator.dart`
* `apps/mobile/lib/data/notifications/agent_notifications.dart`
* `apps/mobile/lib/data/local/settings_store.dart`
* `apps/mobile/lib/app_lifecycle.dart`
* `apps/mobile/lib/features/settings/settings_screen.dart`
* `apps/mobile/lib/features/chat/chat_screen.dart`
* `apps/mobile/lib/state/app_providers.dart`
* `apps/mobile/test/notifications_test.dart`
* `apps/mobile/test/settings_store_test.dart`
* `apps/mobile/test/settings_screen_test.dart`
* `apps/mobile/test/resume_flow_test.dart`
* `apps/mobile/test/privacy_redaction_test.dart` (new)
* `apps/mobile/ios/Runner/Info.plist`
* `apps/mobile/ios/Runner/PrivacyInfo.xcprivacy` (new)
* `apps/mobile/ios/Runner/SceneDelegate.swift`
* `apps/mobile/ios/Runner.xcodeproj/project.pbxproj`
* `apps/mobile/ios/RunnerTests/RunnerTests.swift`
* `scripts/run-ios-simulator-tests.sh` (new)
* `.github/workflows/ci.yml`
* `README.md`
* `apps/mobile/README.md`
* `apps/mobile/pubspec.yaml`
* `docs/ops-ios-signing.md`

### Verification

```bash
cd apps/mobile && flutter test test/notifications_test.dart test/settings_store_test.dart test/settings_screen_test.dart test/resume_flow_test.dart test/privacy_redaction_test.dart
./scripts/run-ios-simulator-tests.sh
./scripts/assert-ios-composition.sh apps/mobile/build/ios/iphonesimulator/Runner.app
plutil -lint apps/mobile/ios/Runner/Info.plist apps/mobile/ios/Runner/PrivacyInfo.xcprivacy apps/mobile/ios/Runner/Runner.entitlements
grep -n 'TARGETED_DEVICE_FAMILY = 1;' apps/mobile/ios/Runner.xcodeproj/project.pbxproj
test "$(grep -c 'TARGETED_DEVICE_FAMILY = 1;' apps/mobile/ios/Runner.xcodeproj/project.pbxproj)" -eq 3
```

Run the common gates. Open a pull request only if separately authorized, then
require the new macOS job to pass on that exact commit before closing the phase.

### Exit criteria and commit

No launch path requests notification permission; concurrent initialization is
one native call; migration tests cover Android and iOS; iPhone-only targeting,
name, privacy manifest, native tests, simulator build, and CI are green; product
copy states that background attention is not yet enabled. Commit phase 2 alone.

## Phase 3 — Prove the existing foreground product on physical iPhones

### Work

Run the existing hardware F1-F6 matrix before adding APNs. Add rows for every
shared surface introduced after MADR 0067: providers and auth, receipts,
handoff, sharing, Codex threads/execution/terminals, workspace inspection,
diagnostics, skill authoring, shell commands, images/HEIC, bounded audio,
camera, QR, long speech, files, transcript relaunch, capture redaction,
accessibility permission denial, and sign-out/revoke.

Each row records commit, semantic version/build, device model, iOS version,
transport, network, daemon/provider versions, steps, expected/actual result,
logs or screenshot/video artifact location, and pass/fail. Run direct LAN,
tailnet where supported, and opaque relay. Exercise ten park/resume cycles,
ten-minute suspension, terminate/relaunch, network loss/restore, first Local
Network denial then enablement, delete/reinstall Keychain inversion, and
memory/attachment bounds.

This phase may repair only defects in the exact phase-1/2 files already listed
and only when the behavior is already specified by MADR 0067/0121. Any defect
requiring protocol, provider, data-model, or unrelated screen changes is
recorded and stops the phase for an approved amendment.

### Files

* `docs/ops-hardware-validation.md`
* `docs/ops-ios-signing.md`
* Phase-1/2 files only when a covered, specified defect is reproduced

### Verification

```bash
xcrun devicectl list devices
security find-identity -v -p codesigning
cd apps/mobile && flutter devices
cd apps/mobile && flutter run --release -d <recorded-physical-device-udid>
```

The UDID is selected from and recorded with `devicectl`; it is never committed.
Run the full common gates after the manual matrix. Attach logs only after
redacting tokens, hostnames, paths, prompts, and personal content.

### Exit criteria and commit

Every foreground/shared matrix row passes on the required physical device set
over direct and relay paths, or an approved amendment explicitly reclassifies
the row. Commit the evidence and any strictly scoped corrections. No simulator,
code inspection, or “shared by Flutter” assertion substitutes for a device pass.

## Phase 4 — Add the daemon attention protocol, durable state, and revocation

### Work

1. Add protocol-v2 capability `attention.v1` and authenticated owner-device
   messages `attention.register`, `attention.unregister`, and
   `attention.status`. Older clients/daemons ignore the additive capability;
   the UI hides background attention when absent.
2. Validate exact gateway allowlist membership, HTTPS outside tests, route and
   capability lengths, kinds, device-key fingerprint, and maximum frame sizes.
   Never echo capability values in responses, events, errors, or logs.
3. Implement owner-only atomic route, outbox, action-ledger, and tombstone
   stores under `<data_dir>/attention/`. Use existing appdirs/private-file and
   atomic-write conventions. Startup rejects group/world-accessible state rather
   than weakening permissions.
4. Add an `attention.Manager` that creates coarse events from owner-visible
   session events, persists before send, retries with bounded exponential
   backoff, expires deterministically, and reports coarse health.
5. Extend `eventHub` so WebSocket broadcast remains synchronous in semantics
   while attention enqueue is non-blocking and bounded. Backpressure drops no
   actionable permission event silently: outbox exhaustion reports a structured
   daemon error and metric while session processing continues.
6. Add attention cleanup to every revoke path. Extend the local admin socket so
   `pair revoke`/`prune` tells the live daemon to delete routes/actions as well
   as disconnect sockets. A stopped daemon discovers externally removed device
   records at startup and removes orphaned attention state.
7. Add and validate the new configuration/default/env/docs surface. Attention
   remains disabled unless both `enabled` and a non-empty allowlist are present.

### Files

* `internal/protocol/messages.go`
* `internal/protocol/messages_test.go`
* `internal/protocol/capabilities.go` and its tests, if capability constants are split
* `internal/attention/model.go` (new)
* `internal/attention/store.go` (new)
* `internal/attention/manager.go` (new)
* `internal/attention/client.go` (new)
* `internal/attention/*_test.go` (new)
* `internal/ws/server.go`
* `internal/ws/server_test.go`
* `internal/ws/server_session_handlers_test.go`
* `internal/daemon/daemon.go`
* `internal/daemon/daemon_test.go`
* `internal/auth/store.go`
* `internal/auth/store_test.go`
* `internal/admin/admin.go`
* `internal/admin/admin_test.go`
* `internal/cli/pair.go`
* `internal/cli/pair_test.go`
* `internal/config/config.go`
* `internal/config/load.go`
* `internal/config/config_test.go`
* `configs/config.example.yaml`
* `configs/config.prod.example.yaml`
* `docs/config.md`
* `docs/protocol.md`

### Verification

```bash
go test ./internal/protocol ./internal/attention ./internal/ws ./internal/daemon ./internal/auth ./internal/admin ./internal/cli ./internal/config
go test -race ./internal/attention ./internal/ws ./internal/daemon ./internal/admin
umask 0077 && go test ./internal/attention ./internal/auth ./internal/admin
rg -n "publish_cap|consume_cap|manage_cap" internal | rg "slog|Printf|Errorf|String"
```

The secret-log search must produce no logging/formatting path containing a
capability. Add fuzz tests for message decoding, URL validation, corrupt state,
unknown versions, overlong fields, and duplicate/replayed registration. Run the
common gates and pre-add check.

### Exit criteria and commit

Protocol negotiation is backward compatible; state survives restart and fails
closed on insecure/corrupt files; registration is owner/device-bound; every
revoke/prune path removes authority; outbox/action expiry is deterministic
under a fake clock; secrets are absent from logs. Commit phase 4 alone with
attention still disabled by default.

## Phase 5 — Build and operationalize `mcpush`

### Work

1. Add `cmd/mcpush` and `internal/pushgateway` with explicit configuration,
   validation, graceful shutdown, request IDs, body/time limits, and structured
   redacted logs.
2. Add PostgreSQL migrations for routes, salted capability hashes, encrypted
   APNs tokens, publish idempotency, pending actions, terminal digests, and
   retention timestamps. Migrations are monotonic and transactional; the binary
   refuses to serve against an unknown schema version.
3. Implement route create/update/delete, generic publish, action submit,
   action long-poll/acknowledge, and coarse route status endpoints. Mutations
   require the appropriate bearer capability. Route creation is IP-rate-limited
   and returns capabilities only once.
4. Implement APNs provider JWT signing and HTTP/2 delivery with production and
   sandbox endpoints, strict topic/environment binding, APNs reason mapping,
   token rotation, 410 cleanup, transient retry classification, and a fake
   transport. Never retry a permanent APNs error.
5. Implement AES-GCM token encryption/key rotation, constant-time capability
   verification after indexed lookup, secure randomness, clock injection,
   deterministic expiry, log redaction, and maximum-body enforcement.
6. Add retention workers, route/action quotas, health/readiness/metrics, database
   backup/restore and key-rotation runbooks, incident/revocation procedures, an
   OCI image, a hardened systemd unit, and a CI lane that builds/tests/scans the
   image without publishing it.
7. Add a threat-model test suite for stolen publish/consume/manage capability,
   database-only compromise, replay, tamper, route swapping, cross-environment
   tokens, oversized input, slow clients, APNs outage, database restart, and
   clock skew.

### HTTP contract

All endpoints are `/v1`, JSON, HTTPS, and capped at 4 KiB request bodies except
the empty long-poll request. Error bodies contain stable codes and no secrets.

| Method/path | Authority | Result |
| --- | --- | --- |
| `POST /v1/routes` | rate-limited bootstrap | Create route; return capabilities once |
| `PUT /v1/routes/{route_id}` | manage | Rotate APNs token or enabled kinds |
| `DELETE /v1/routes/{route_id}` | manage or publish | Delete route and queued actions |
| `GET /v1/routes/{route_id}/status` | manage | Coarse active/degraded/disabled state |
| `POST /v1/routes/{route_id}/events` | publish | Idempotently submit bounded generic event |
| `POST /v1/routes/{route_id}/actions` | manage | Queue device-signed compact JWS |
| `GET /v1/routes/{route_id}/actions:next` | consume | Long-poll at most 25 seconds |
| `POST /v1/routes/{route_id}/actions/{digest}:ack` | consume | Record terminal host result |

The OpenAPI document is generated from/checked against handler tests and ships
with the operator docs. It is not used to generate runtime security logic.

### Files

* `cmd/mcpush/main.go` (new)
* `cmd/mcpush/main_test.go` (new)
* `internal/pushgateway/config.go` (new)
* `internal/pushgateway/server.go` (new)
* `internal/pushgateway/routes.go` (new)
* `internal/pushgateway/events.go` (new)
* `internal/pushgateway/actions.go` (new)
* `internal/pushgateway/apns.go` (new)
* `internal/pushgateway/crypto.go` (new)
* `internal/pushgateway/store.go` (new)
* `internal/pushgateway/retention.go` (new)
* `internal/pushgateway/metrics.go` (new)
* `internal/pushgateway/*_test.go` and `testdata/` (new)
* `deploy/mcpush/migrations/0001_initial.sql` (new)
* `deploy/mcpush/openapi.yaml` (new)
* `deploy/mcpush/Dockerfile` (new)
* `deploy/systemd/mcpush.service` (new)
* `configs/mcpush.example.yaml` (new)
* `docs/config-mcpush.md` (new)
* `docs/ops-mcpush.md` (new)
* `go.mod`
* `go.sum`
* `Makefile`
* `.github/workflows/ci.yml`

### Verification

Use an ephemeral local PostgreSQL 17 instance whose data directory is outside
the repository and delete it after the run. The test APNs server must assert
headers and bodies without contacting Apple.

```bash
go test ./cmd/mcpush ./internal/pushgateway
go test -race ./internal/pushgateway
go test -fuzz=Fuzz -fuzztime=30s ./internal/pushgateway
go vet ./cmd/mcpush ./internal/pushgateway
make build-push
make verify-units
docker build --pull=false -f deploy/mcpush/Dockerfile -t mcpush:plan-0121 .
```

Run migration up, backup, restore to a fresh database, key rotation, retention,
and service restart drills. Capture counts/digests, not live secrets. Run the
common gates and pre-add check.

### Exit criteria and commit

The fake-APNs end-to-end flow survives service/database restart; route tokens
are encrypted, caps are hashed, actions are durable, retention deletes on
schedule, logs/metrics contain no forbidden fields, migrations restore from
backup, and the image runs unprivileged/read-only except for required temp
paths. Commit phase 5 without deploying or publishing.

## Phase 6 — Add APNs registration, consent, and route lifecycle to iPhone

### Work

1. Add a small app-owned Swift plugin that requests remote-notification
   registration only after explicit `backgroundAttention` consent, emits token
   changes/failures over a method channel, and unregisters when disabled. Call
   `registerForRemoteNotifications` on every authorized launch.
2. Add the APS environment entitlement through build settings appropriate to
   development/release profiles. Keep `UIBackgroundModes` absent. Assert both
   conditions in CI/archive inspection.
3. Add a typed Dart `AttentionClient`, route model/store, APNs adapter, and
   lifecycle coordinator. Create/update/delete gateway routes and transfer only
   publish/consume capabilities to the authenticated paired daemon through the
   phase-4 messages.
4. Bind the gateway route to the enrolled device-key fingerprint. Reject route
   setup until pairing and client-key enrollment are complete. Identity reset,
   sign-out, unpair, alert disable, gateway change, and APNs token rotation all
   have explicit tested transitions.
5. Show foreground-only/background choice, Apple authorization state, APNs
   registration state, gateway state, last coarse delivery result, deletion
   control, metadata/retention disclosure, privacy-policy link, and separate
   local-vs-gateway diagnostics in Settings.
6. Compile the exact gateway URL into signed builds from protected CI input.
   Debug builds may use an explicitly passed loopback URL. Empty URL hides
   background setup and states “publisher gateway not configured.”

### Files

* `apps/mobile/ios/Runner/AppDelegate.swift`
* `apps/mobile/ios/Runner/PushRegistrationPlugin.swift` (new)
* `apps/mobile/ios/Runner/Runner.entitlements`
* `apps/mobile/ios/Runner/Info.plist`
* `apps/mobile/ios/Runner.xcodeproj/project.pbxproj`
* `apps/mobile/ios/RunnerTests/RunnerTests.swift`
* `apps/mobile/lib/data/attention/attention_models.dart` (new)
* `apps/mobile/lib/data/attention/attention_client.dart` (new)
* `apps/mobile/lib/data/attention/push_registration.dart` (new)
* `apps/mobile/lib/data/attention/attention_coordinator.dart` (new)
* `apps/mobile/lib/data/local/settings_store.dart`
* `apps/mobile/lib/data/ws/mcremote_client.dart`
* `apps/mobile/lib/data/protocol/models.dart`
* `apps/mobile/lib/app_lifecycle.dart`
* `apps/mobile/lib/state/app_providers.dart`
* `apps/mobile/lib/features/settings/settings_screen.dart`
* `apps/mobile/test/attention_client_test.dart` (new)
* `apps/mobile/test/attention_coordinator_test.dart` (new)
* `apps/mobile/test/settings_screen_test.dart`
* `apps/mobile/test/client_identity_test.dart`
* `scripts/assert-ios-entitlements.sh` (new)
* `.github/workflows/ci.yml`
* `apps/mobile/store/privacy-policy.md` (new)

### Verification

```bash
cd apps/mobile && flutter test test/attention_client_test.dart test/attention_coordinator_test.dart test/settings_screen_test.dart test/client_identity_test.dart
./scripts/run-ios-simulator-tests.sh
./scripts/assert-ios-entitlements.sh apps/mobile/build/ios/iphonesimulator/Runner.app development
rg -n "UIBackgroundModes|BGTaskSchedulerPermittedIdentifiers" apps/mobile/ios
```

On a physical development-signed iPhone, verify consent before the Apple prompt,
authorized/denied transitions, Settings deep link, APNs token arrival, rotation
simulation, route create/update/delete, identity reset, uninstall/reinstall,
daemon disabled/allowlist mismatch, gateway outage, and foreground-only use.
Use sandbox APNs credentials only. Run common gates.

### Exit criteria and commit

No APNs/gateway contact occurs before consent; the system prompt is requested
only from a user action; token and route lifecycle survive launch/reinstall
rules; disabling or revoking deletes authority; APS is the only new entitlement;
no background mode appears; foreground-only remains complete. Commit phase 6.

## Phase 7 — Deliver generic attention end to end

### Work

1. Map authoritative session events to the four coarse kinds after owner
   visibility is resolved. Coalesce repeated non-actionable events; never
   coalesce different live permission asks.
2. For each event, persist the daemon outbox record before HTTP, publish with
   `publish_cap`, apply bounded retry/jitter until expiry, and record only
   coarse status. A gateway timeout never blocks session event broadcast.
3. Have `mcpush` construct the fixed APNs body and headers. Assert payload
   equality from fixtures and reject any unexpected JSON field or caller title/body.
4. Register iOS notification categories and route notification taps through
   normal reconnect/state reconciliation. A tap never trusts notification state
   as current session state.
5. Add an end-to-end harness: fake provider -> session manager -> attention
   outbox -> fake gateway/APNs -> captured generic notification. Include direct
   and relay-connected phones, daemon restart, gateway restart, offline/online,
   expiry, duplicate, APNs 410, and route deletion.
6. Define the operational objective: once the daemon receives an event and both
   network and APNs accept traffic, `mcpush` submits it to APNs within 5 seconds
   at p95. In a 20-event physical-device run on each required device/network,
   at least 19 arrive within 30 seconds and none appears twice. Record APNs as
   best-effort; do not claim an impossible guaranteed device-delivery SLA.

### Files

* `internal/attention/manager.go`
* `internal/attention/client.go`
* `internal/attention/store.go`
* `internal/attention/*_test.go`
* `internal/daemon/daemon.go`
* `internal/daemon/daemon_test.go`
* `internal/session/manager.go`
* `internal/session/manager_test.go`
* `internal/pushgateway/events.go`
* `internal/pushgateway/apns.go`
* `internal/pushgateway/*_test.go`
* `internal/ws/receipt_e2e_test.go` or a new `internal/ws/attention_e2e_test.go`
* `apps/mobile/lib/data/notifications/notification_service.dart`
* `apps/mobile/lib/data/notifications/notification_coordinator.dart`
* `apps/mobile/lib/data/attention/attention_coordinator.dart`
* `apps/mobile/test/notifications_test.dart`
* `apps/mobile/test/attention_coordinator_test.dart`
* `apps/mobile/integration_test/attention_delivery_test.dart` (new)
* `apps/mobile/test/support/fake_attention_gateway.dart` (new)
* `scripts/run-ios-simulator-tests.sh`
* `docs/ops-hardware-validation.md`
* `docs/ops-mcpush.md`

### Verification

```bash
go test ./internal/attention ./internal/pushgateway ./internal/session ./internal/ws ./internal/daemon
go test -race ./internal/attention ./internal/pushgateway ./internal/session ./internal/ws
cd apps/mobile && flutter test test/notifications_test.dart test/attention_coordinator_test.dart
./scripts/run-ios-simulator-tests.sh integration_test/attention_delivery_test.dart
```

Capture gateway requests in the e2e harness and mechanically search decoded
headers/body/logs for seeded sensitive canaries. The test fails if any prompt,
path, tool, transcript, output, credential, session title, host name, or device
name is present. Run the physical 20-event matrix and common gates.

### Exit criteria and commit

All four kinds deliver generic content in foreground, suspended, and terminated
states; retry/expiry/410/delete behavior is correct; session traffic remains on
the original transport; seeded confidential content is absent from gateway/APNs
observation; delivery objectives pass on both device/network classes. Commit
phase 7.

## Phase 8 — Complete durable authenticated Allow/Deny actions

### Work

1. Generate a random action ID and local sensitive mapping before publishing a
   permission alert. Bind it to device, route, session, permission, exact
   provider options, creation, and authoritative host expiry.
2. Register background, authentication-required Allow/Deny actions. Implement
   a top-level background Dart callback that atomically records intent before
   loading Keychain or making HTTPS requests. Keep initialization bounded and
   free of widget/provider assumptions.
3. Reuse/refactor the existing P-256/JWS primitives to sign canonical action
   JSON. Add fixed test vectors shared between Dart and Go; reject non-canonical,
   high-S/non-normalized, wrong-algorithm, wrong-key, and malformed JWS inputs.
4. Queue and long-poll signed actions through `mcpush`. The gateway validates
   manage/consume authority and bounds only; the daemon performs the definitive
   signature, binding, pending-ask, allowed-choice, and expiry checks.
5. Route valid actions into a single session-manager method shared with
   WebSocket `permission.respond`. Persist terminal ledger state and ack the
   gateway. Replays return the same coarse result without a provider call.
6. On foreground reconnect, drain local intents, reconcile host status, update
   or retire the notification, and show explicit accepted/denied/expired/
   unavailable state. Never silently discard a selected choice.
7. Preserve fail-safe timeout: locked/unavailable authentication, expired ask,
   terminated daemon, revoked device, stale route, network failure, gateway
   failure, tamper, and crash all result in no new agent power.

### Files

* `internal/attention/action.go` (new)
* `internal/attention/action_test.go` (new)
* `internal/attention/store.go`
* `internal/attention/manager.go`
* `internal/session/manager.go`
* `internal/session/manager_permission_test.go`
* `internal/ws/server.go`
* `internal/ws/server_session_handlers_test.go`
* `internal/auth/store.go`
* `internal/pushgateway/actions.go`
* `internal/pushgateway/actions_test.go`
* `internal/receipt/jws.go` only if extracting a shared strict ES256 verifier does not change receipt semantics
* `apps/mobile/lib/data/attention/action_intent.dart` (new)
* `apps/mobile/lib/data/attention/action_queue.dart` (new)
* `apps/mobile/lib/data/attention/action_signer.dart` (new)
* `apps/mobile/lib/data/attention/attention_client.dart`
* `apps/mobile/lib/data/notifications/notification_service.dart`
* `apps/mobile/lib/data/notifications/notification_coordinator.dart`
* `apps/mobile/lib/data/ws/jws.dart`
* `apps/mobile/lib/data/ws/client_identity.dart`
* `apps/mobile/lib/data/local/settings_store.dart`
* `apps/mobile/test/action_queue_test.dart` (new)
* `apps/mobile/test/action_signer_test.dart` (new)
* `apps/mobile/test/notifications_test.dart`
* `apps/mobile/integration_test/attention_action_test.dart` (new)
* `internal/attention/testdata/action_vectors.json` (new shared vectors)
* `docs/ops-hardware-validation.md`

### Verification

```bash
go test ./internal/attention ./internal/session ./internal/ws ./internal/auth ./internal/pushgateway
go test -race ./internal/attention ./internal/session ./internal/ws ./internal/pushgateway
cd apps/mobile && flutter test test/action_queue_test.dart test/action_signer_test.dart test/notifications_test.dart
./scripts/run-ios-simulator-tests.sh integration_test/attention_action_test.dart
```

On each physical device run a fault-injection matrix for foreground,
background, suspended, terminated, locked, biometric/passcode cancellation,
offline-before-tap, offline-after-tap, daemon restart, gateway restart, duplicate
tap, replay, modified choice, modified expiry, wrong key/device/route, provider
timeout, ask expiry one second before tap, and device revoke. Inspect the
provider fake's call counter: exactly one valid live action calls it; all other
cases call it zero times. Run common gates.

### Exit criteria and commit

The chosen action is durably recorded before network work, survives process and
service restart, authenticates with the enrolled device key, resolves a live ask
at most once, reports terminal state, and never extends host expiry or grants on
failure. The gateway cannot mint or alter a valid action. Commit phase 8.

## Phase 9 — Build a reproducible signed archive and TestFlight lane

### Work

1. Add fastlane configuration for archive validation and App Store Connect API
   authentication. Keep all keys/certificates/profiles in protected CI secrets
   and an ephemeral keychain; delete the keychain after the job.
2. Use the Git tag without `v` as `CFBundleShortVersionString`. Serialize the
   TestFlight upload job with a concurrency group, query the latest App Store
   Connect build for that marketing version, and allocate exactly `latest + 1`
   immediately before archive. Abort rather than reuse/decrement a number.
3. Build with `flutter build ipa`, export with the explicit App Store method,
   and inspect the archive/IPA for bundle ID, team, minimum OS, iPhone-only
   family, version/build, APS entitlement, absence of background modes,
   forbidden plugin symbols, embedded provisioning, SDK privacy manifests, and
   aggregate privacy report.
4. Run signed-development install first, then internal TestFlight. Verify clean
   install, upgrade from the last internal build, paired state, Keychain
   inversion, direct/relay reconnect, foreground-only, APNs token rotation, all
   attention kinds/actions, revoke, sign-out, and crash-free launch.
5. Add external TestFlight only after internal evidence passes and the owner
   authorizes upload/review. Record beta-review result and remediate only within
   approved product scope.
6. Update Settings to show the installed version/build and an iOS store-management
   link. Never download or sideload IPA updates from the app.

### Files

* `apps/mobile/Gemfile` (new)
* `apps/mobile/Gemfile.lock` (new)
* `apps/mobile/ios/fastlane/Appfile` (new)
* `apps/mobile/ios/fastlane/Fastfile` (new)
* `apps/mobile/ios/ExportOptions.plist` (new, no profile secrets)
* `apps/mobile/ios/Runner.xcodeproj/project.pbxproj`
* `apps/mobile/pubspec.yaml`
* `apps/mobile/lib/features/settings/settings_screen.dart`
* `apps/mobile/lib/data/update/app_update.dart`
* `apps/mobile/test/app_update_tile_test.dart`
* `scripts/assert-ios-archive.sh` (new)
* `.github/workflows/ios-testflight.yml` (new)
* `docs/ops-ios-signing.md`
* `docs/ops-hardware-validation.md`
* `apps/mobile/store/export-compliance.md` (new)

### Verification

```bash
cd apps/mobile && bundle exec fastlane ios test_archive
./scripts/assert-ios-archive.sh apps/mobile/build/ios/archive/Runner.xcarchive
codesign -d --entitlements :- apps/mobile/build/ios/archive/Runner.xcarchive/Products/Applications/Runner.app
security cms -D -i apps/mobile/build/ios/archive/Runner.xcarchive/Products/Applications/Runner.app/embedded.mobileprovision
```

The CI workflow first supports a no-upload validation mode. The upload action
requires protected environment approval and an explicit owner request. Verify
the downloaded TestFlight binary, not only the locally exported IPA. Run common
gates before the phase commit.

### Exit criteria and commit

The same commit deterministically creates a validated signed IPA; build numbers
increase; credentials do not persist or log; privacy/entitlement/composition
checks pass; internal TestFlight clean-install and upgrade matrices pass on both
devices; external beta review passes if authorized. Commit phase 9; upload is a
separately authorized external action.

## Phase 10 — Finish iPhone idiom, privacy, demo, and review readiness

### Work

1. Audit primary flows with VoiceOver, accessibility Dynamic Type, Bold Text,
   Reduce Motion, light/dark/increased-contrast, smallest supported width,
   landscape, software/hardware keyboard, predictive back/Cupertino gesture,
   interrupted pickers, and memory pressure. Fix only the audited iPhone/shared
   presentation surfaces listed below; protocol/provider changes require an
   amendment.
2. Mark token reveal, credential entry, permission detail, and other explicitly
   inventoried high-risk views for scene-capture redaction. Verify harmless
   screens remain usable and app-switcher snapshots stay opaque.
3. Add a bounded, read-only, deterministic demo repository behind a launch-time
   “Explore demo” choice. Fixtures exercise sessions, streaming-complete chat,
   tool/permission/question presentation, providers, receipts, diagnostics,
   workspace, attachments, and error states without executing commands,
   accepting credentials, contacting a gateway, or mutating a real host.
4. Operate a separate bounded review daemon/relay with synthetic data, least
   privilege, expiry, monitoring, and a sample QR. Do not embed its long-lived
   credentials in the binary or repository. Review access can be revoked
   independently without affecting users.
5. Finalize and publish the privacy policy; make in-app and App Store metadata
   links identical. Record App Privacy answers, retention/deletion behavior,
   export-compliance answer, support URL, age rating, category, screenshots,
   review contact, and reviewer instructions as versioned store source.
6. Review notes explain the native protocol client, user-owned execution host,
   opaque session relay, separate metadata-only attention gateway, provider
   credential location, no digital-goods store, no primary cloud account, demo,
   and sample QR. Explicitly address guideline 4.2.7 and current relay behavior.

### Files

* `apps/mobile/lib/app.dart`
* `apps/mobile/lib/state/app_providers.dart`
* `apps/mobile/lib/features/connect/connect_screen.dart`
* `apps/mobile/lib/features/sessions/sessions_screen.dart`
* `apps/mobile/lib/features/chat/chat_screen.dart`
* `apps/mobile/lib/features/chat/transcript_pane.dart`
* `apps/mobile/lib/features/chat/question_sheet.dart`
* `apps/mobile/lib/features/settings/settings_screen.dart`
* `apps/mobile/lib/features/settings/providers_screen.dart`
* `apps/mobile/lib/data/demo/demo_repository.dart` (new)
* `apps/mobile/lib/data/demo/demo_fixtures.dart` (new)
* `apps/mobile/test/demo_mode_test.dart` (new)
* `apps/mobile/test/accessibility_test.dart` (new)
* `apps/mobile/test/privacy_redaction_test.dart`
* `apps/mobile/store/privacy-policy.md`
* `apps/mobile/store/app-privacy.md` (new)
* `apps/mobile/store/review-notes.md` (new)
* `apps/mobile/store/review-host.md` (new, no credentials)
* `apps/mobile/ios/fastlane/metadata/en-US/description.txt` (new)
* `apps/mobile/ios/fastlane/metadata/en-US/keywords.txt` (new)
* `apps/mobile/ios/fastlane/metadata/en-US/privacy_url.txt` (new)
* `apps/mobile/ios/fastlane/metadata/en-US/support_url.txt` (new)
* `apps/mobile/ios/fastlane/metadata/en-US/review_information/notes.txt` (new)
* `apps/mobile/ios/fastlane/screenshots/README.md` (new; generated screenshots remain release artifacts unless policy says otherwise)
* `docs/ops-hardware-validation.md`

### Verification

```bash
cd apps/mobile && flutter test test/demo_mode_test.dart test/accessibility_test.dart test/privacy_redaction_test.dart
cd apps/mobile && flutter test --update-goldens=false
./scripts/run-ios-simulator-tests.sh
```

Run the manual accessibility/appearance/device matrix and a review rehearsal by
a person who did not configure the environment. The reviewer must reach every
major demo surface and pair to the review host using only committed review
instructions plus separately supplied review credentials. Inspect network logs:
demo mode makes no daemon, relay, gateway, provider, analytics, or external
asset request. Validate every metadata/privacy statement against captured
runtime behavior and the gateway schema/retention job.

### Exit criteria and commit

Primary flows pass the accessibility/idiom matrix; high-risk views redact under
capture; demo is full enough for comprehension, read-only, deterministic, and
offline; the review host is monitored and isolated; policy, labels, metadata,
and runtime agree; an uninvolved tester completes the review script. Commit
phase 10. Metadata upload/submission still requires explicit authorization.

## Phase 11 — Final parity acceptance, rollout, and record closure

### Work

1. Freeze a release candidate commit. Run every automated command from phases
   1-10 and every physical-device row without source edits between archive and
   evidence capture.
2. Repeat the 20-event delivery sample for every attention kind/network/device,
   the complete action fault matrix, direct/relay session matrix, clean install,
   previous-build upgrade, terminate/relaunch, ten park/resume cycles, Keychain
   reinstall, token rotation, revoke, gateway outage, and daemon outage.
3. Conduct gateway restore, APNs key rotation, database key rotation, route
   deletion, retention, rollback, incident, and on-call drills in the production
   staging environment. Confirm dashboards/alerts and bounded redacted logs.
4. Promote to internal TestFlight, then external TestFlight only with explicit
   owner authorization. Hold a minimum seven-day daily-driver soak with no S1/S2
   defect, no unexplained action, no lost valid selected action, and no secret
   in gateway/APNs telemetry.
5. If public App Store support is authorized, submit the same behavior reviewed
   in TestFlight and record the decision. A rejection requiring product-scope
   change stops for a MADR amendment.
6. Publish the final support matrix, update all product docs, set MADR status to
   `accepted` or `superseded` as appropriate, and set this plan to `completed`
   only after all mandatory evidence and authorized release outcomes pass.

### Files

* `docs/0121-MADR-achieve-iphone-functional-parity.md`
* `docs/0121-PLAN-achieve-iphone-functional-parity.md`
* `docs/ops-hardware-validation.md`
* `docs/ops-ios-signing.md`
* `docs/ops-mcpush.md`
* `README.md`
* `apps/mobile/README.md`
* `apps/mobile/store/review-notes.md`
* App/source files only if a failing criterion is already within an approved
  earlier phase; otherwise amend before editing

### Final acceptance criteria

Every item is mandatory unless the owner explicitly marks App Store public
submission out of scope while retaining TestFlight-only support:

* iOS simulator/native CI, shared tests, Go tests/race/vet, Android build, signed
  archive, privacy report, and composition assertions pass on the frozen commit.
* The archive is iPhone-only, correctly signed/versioned, contains APS and no
  unrelated background modes or `flutter_foreground_task` code.
* Every shared product and MADR 0067 hardware row passes on the required device
  set over direct and relay transports.
* Consent precedes the Apple prompt; denial and gateway outage preserve complete
  foreground operation; Settings reports actual authorization/route health.
* Four generic attention kinds meet the stated sample objective in foreground,
  suspended, and terminated states without duplicate display.
* Exactly one valid live Allow/Deny action reaches the provider; replay, tamper,
  wrong binding, expiry, revoke, lock/auth failure, or outage grants no power.
* Gateway/APNs captures contain no seeded prompt, path, tool, transcript,
  output, credential, error detail, session title, host name, or device name.
* Route deletion, APNs 410, sign-out, identity reset, revoke, and 90-day retention
  remove delivery/action authority as specified.
* TestFlight clean install and upgrade preserve intended state; the seven-day
  soak has no unresolved S1/S2 issue.
* Accessibility, capture redaction, demo, review host, privacy policy/labels,
  export compliance, metadata, support copy, and runtime behavior agree.
* External TestFlight beta review passes. App Store review passes before the
  project claims public App Store availability.

### Exit criteria and commit

Commit only the final evidence/status/support-truth update. Do not push, tag,
publish, promote, or submit without explicit same-turn authorization. If any
criterion fails, leave the plan incomplete, record the fact, and return to the
owning phase or amend the decision; do not weaken the criterion in place.

## Rollout and rollback

### Rollout order

1. Keep daemon `attention.enabled=false` and ship phases 1-3 as foreground-only
   iPhone foundations.
2. Deploy `mcpush` to staging with sandbox APNs and synthetic routes; exercise
   operations before any production token exists.
3. Enable one owner-operated daemon and one internal device allowlist entry.
4. Expand to internal TestFlight devices, then external TestFlight only after
   privacy/delivery/action evidence passes.
5. Enable production APNs route creation only in the signed production build.
6. Submit to the App Store only after the soak and review rehearsal.

### Software rollback

* Mobile: disable background attention remotely only through route deletion and
  documented user-visible degradation; the app falls back to foreground-only.
  A prior TestFlight/App Store build may be restored only if its protocol and
  data formats remain compatible.
* Daemon: set `attention.enabled=false`, restart, and retain tombstones until
  their normal expiry. Session WebSocket behavior continues unchanged.
* Gateway: reject new route/event/action traffic with a stable maintenance
  response while preserving delete and status endpoints. Do not drop pending
  actions as “successful.”
* Database migrations are forward-compatible and not rolled back destructively.
  Restore a tested backup to a new database, validate schema/key versions, then
  switch traffic.

### Security or privacy shutdown

If an APNs key, database token key, capability, signing asset, or gateway is
suspected compromised:

1. disable new route/event/action traffic;
2. rotate/revoke the affected external key in Apple/secret manager;
3. invalidate all capabilities in the affected scope and delete queued actions;
4. force route re-registration from authenticated paired devices;
5. verify old routes cannot publish, consume, manage, alert, or act;
6. notify affected users and regulators according to the approved privacy/
   incident policy;
7. preserve redacted audit evidence and amend the MADR if the trust boundary
   changes.

Rollback success means foreground direct/relay operation remains intact, no
stale action grants power, users see background-attention degradation, and old
capabilities/APNs credentials are unusable.

## Explicitly deferred or excluded

* native iPad support, Mac Catalyst, Apple Watch, widgets, Live Activities, and
  notification content extensions;
* silent push, background WebSocket keepalive, background fetch/tasks, VoIP,
  location/audio background modes, and Local Push Connectivity;
* rich or encrypted prompt previews on the lock screen;
* a general protocol-v2 tunnel through `mcpush` or merging `mcpush` with
  `mcrelay`;
* third-party push/analytics/advertising SDKs;
* App Attest/device attestation, multi-region active-active gateway, and
  user-operated APNs credentials in the publisher-signed App Store binary;
* repair of the unrelated host-umask test assumption;
* any App Review-requested change to relay, accounts, provider auth, digital
  goods, or host ownership without an approved MADR amendment.

These exclusions are not implementation shortcuts. They define the smallest
platform-supported architecture that closes the iPhone outcome gap decided by
MADR 0121.
