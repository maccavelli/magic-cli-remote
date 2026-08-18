---
status: proposed
date: 2026-08-18
decision-makers: Project Owner (scope and acceptance)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Make Android agent alerts survive their own lifecycle, and make their absence diagnosable

## Context and Problem Statement

Settings carries four agent-alert toggles (`settings_screen.dart:991-1041`):
**Agent alerts** (master), **Permission requests**, **Turn complete**, and
**Errors**. The report under investigation: with everything enabled, only
turn-complete notifications ever appear on the phone — permission requests and
errors never fire.

The investigation that grounds this record ran the full pipeline twice: once
statically (daemon emission → WebSocket broadcast → Dart parse → coordinator →
plugin → Android), and once live, on the API 36 emulator against this host's
real daemon (v0.13.7.1) and a live agent session, with the current app build.
(The reproduction vehicle was kilo; the choice is arbitrary — everything above
the provider layer is provider-uniform, and per-provider differences are called
out explicitly below as a parity matrix, never as a "default provider".)

### What the live reproduction established

Every alert kind **works** in a clean environment, current code:

| Scenario | Result |
|---|---|
| Turn completes while backgrounded | ✅ notification on `agent_done` (dumpsys: id 1320963967) |
| Permission ask arrives while backgrounded | ✅ notification on `approval_needed`, importance 4, Allow/Deny actions attached (id 297567813) |
| Ask arrives while watching → app backgrounded with it pending | ✅ suppressed in-app (correct), then raised by the catch-up path the moment the app backgrounds |
| Allow tapped from the shade | ✅ `respondPermission` sent, notification retired |
| Engine killed mid-turn while backgrounded | ✅ notification on `agent_error` |

So the defect is **not** a broken code path from event to notification. The
reported symptom is produced by the system's own lifecycle rules — which the
live run also caught, twice, without looking for them:

### F1 — a permission notification lives at most two minutes, then vanishes without a trace

`permission_timeout_seconds` defaults to **120** for kilo, grok, goose,
opencode (`config.go:749-810`, `defaults_mcremote.yaml:62-131`; codex gets
900). When it expires, the daemon answers the permission itself
(`httpagent/session.go:1167-1190` `expirePermission`: `RespondPermission(...,
cancelled)`) and emits `permission_resolved` — and the phone's coordinator
**cancels the notification** on any resolution
(`notification_coordinator.dart:152-156`).

This happened live: the first ask's notification was posted at 12:52:13 and
was gone before an Allow tapped ~2½ minutes later could land; the transcript
showed *"Permission request timed out after 2m0s — the agent stopped
waiting"*, the tool call showed `write FAILED`, and the turn then completed —
leaving exactly one persistent notification: **"Agent finished"**.

That is the reported symptom, generated faithfully: walk away, agent asks,
notification appears, two minutes pass, notification is deleted, turn ends,
"Agent finished" persists. Check the phone any time later than two minutes
after the ask and the observable history is *"only end turn fires."* The
cancel-on-resolution rule was built so a stale Allow/Deny can't be tapped
(MADR 0046 M-4) — correct — but for the *timeout* resolution it also deletes
the only evidence the agent ever asked.

### F2 — most tool runs never produce a permission event at all

In the same live session, `Run the shell command 'uname -a'` executed to
COMPLETED with **no permission_request emitted** — the engine's own approval
config auto-ran it; only the out-of-workspace write (`external_directory
/tmp/*`) produced a blocking ask. Every engine carries its own approval
config, so how often a blocking ask actually occurs varies per provider and
per host configuration. A user "testing the permission toggle" by asking the
agent to run a command may generate nothing to notify about. (Auto-approved
requests are deliberately surfaced as the in-transcript approval card instead
— MADR 0051.)

### F3 — an error notification's body drops the error

`showError` is called with `detail: ev.text`
(`notification_coordinator.dart:174-181`), but the daemon puts the message of
a `TypeError` event in the `error` field, not `text` (`event.go:446`; kilo
`lifecycle.go:148`, codex `session.go:1915`). The Dart model parses both;
the coordinator reads the wrong one, so the notification body degrades to the
bare session label. Display still happens (verified live) — the *content* is
lost. Errors are also emitted only alongside a failed turn's `turn_complete`
(invariant documented at codex `session.go:1869`), so when both fire the user
sees "Agent finished" and an "Agent error" that names no error.

### F4 — a per-channel OS block is invisible to the app

Android lets the user silence or block a single notification **channel**
(long-press on a notification → turn off that category) while the app's
overall permission stays granted. The three kinds live on three channels —
`approval_needed`, `agent_done`, `agent_error`
(`notification_service.dart:24-26`) — so a per-channel block reproduces the
reported symptom exactly and permanently on that device. Settings today
surfaces only the app-level state (`osBlocked` →
`areNotificationsEnabled()`, `settings_screen.dart:1043-1058`); a blocked
channel shows a healthy toggle. The plugin exposes what's needed
(`getNotificationChannels()`, plugin `platform_flutter_local_notifications.dart:584`);
nothing calls it.

### F5 — the timeout resolution is unmarked on three of five providers

Whether `permission_resolved` carries `timed_out: true` on expiry depends on
the transport a provider is built on:

| Provider | Transport | `timed_out` on expiry |
|---|---|---|
| grok | `acpagent` (`grok.go:105`) | ❌ — helper `acpagent/session.go:1306-1316` has no TimedOut parameter; timeout arm `:1949-1966` |
| opencode | `httpagent` (`http.go:110`) | ❌ — `httpagent/session.go:1185-1189` emits plain `cancelled` |
| kilo | `httpagent` (`kilo.go:62`) | ❌ — same path |
| goose | `acphttp` (`goose.go:72`) | ✅ `acphttp/session.go:1396` |
| codex | own session | ✅ `codex/session.go:1719` |

Any client behavior keyed on "this ask expired unattended" is blind on grok,
opencode, and kilo alike — a parity hole across three of the five providers,
not a single-provider quirk. It is also a conformance defect, not a missing
nicety: `docs/protocol-v1.md:1104` already specifies `timed_out` as *"true
when the request was auto-cancelled because the client did not answer within
`permission_timeout_seconds`"* — the three ❌ rows violate the documented
contract today. (`question_resolved` documents no such flag (`:926`), so the
question half of part B is an additive extension, to be documented alongside.)
The Dart model already parses the flag (`models.dart:1648`); the coordinator
currently ignores it.

### Findings that are noted, not fixed here

* **Android 15/16 notification cooldown** marks rapid successive
  notifications `SILENT` (observed in dumpsys on every test notification) —
  it lowers alerting, never suppresses display. Not a cause.
* **`CATEGORY_CALL` on the permission notification** displayed fine on API 36;
  restrictions around that category concern full-screen intents and ranking,
  not shade display.
* **Plugin action `requestCode` overflow**: the plugin computes action request
  codes as `notificationDetails.id * 16` (plugin
  `FlutterLocalNotificationsPlugin.java:322`); this app's FNV-derived 31-bit
  ids overflow that multiply. Java wraps silently and `PendingIntent` accepts
  any int; a cross-notification collision could route an action tap to the
  wrong payload but cannot suppress display. Upstream concern; recorded so a
  future misrouted-tap report starts here.

## Decision Drivers

* **D1 — The user's mental model is the spec.** "All four toggles work when
  enabled" must mean: when the agent asks or fails while you are away, your
  phone retains evidence of it until you've seen it — not merely "a
  notification object existed for some window."
* **D2 — No silent states.** A blocked channel, a failed init, or an expired
  ask must be *visible* — the settings screen already does this for app-level
  blocks and init failures; the gaps are per-channel state and timeout expiry.
* **D3 — Diagnosable on the user's device, not the emulator.** The failing
  environment is a real phone this investigation could not touch. The fix must
  include a way to validate the display path *there* in seconds.
* **D4 — Keep the security posture.** Cancel-on-resolution exists so a stale
  Allow/Deny can't fire (M-4). Nothing here may reintroduce an actionable
  notification for a dead ask.
* **D5 — Provider parity is a first-class requirement.** No provider is "the
  primary"; all five are to be equally featured and hardened. A client
  behavior keyed on `timed_out` must therefore work identically on every
  provider — F5's three-of-five gap (grok, opencode, kilo) is a parity
  violation, not a single-provider quirk.

## Considered Options

* **Evidence-preserving alerts + per-channel diagnostics + on-device test
  path** (a composite of the five targeted fixes below)
* **Lengthen the permission timeout instead** — raise
  `permission_timeout_seconds` so asks outlive the user's absence
* **Documentation only** — describe the two-minute window and per-channel
  blocks in the README/settings copy
* **Rework alerting on a push transport** (FCM / UnifiedPush) so asks reach a
  device whose socket died

## Decision Outcome

Chosen option: **"Evidence-preserving alerts + per-channel diagnostics +
on-device test path"**, because the live reproduction shows the pipeline is
sound and the user-visible failure comes from three specific, individually
small holes: expiry deletes the evidence (F1), content is dropped (F3), and
OS-level per-channel state is invisible (F4) — with F5 as the wire-contract
prerequisite for fixing F1 uniformly. The other options each fail a driver:
a longer timeout leaves the agent blocked for the whole window and still
deletes the evidence at whatever the new deadline is (D1); documentation
changes nothing on the phone (D2); a push transport is a different
architecture with its own MADR-scale privacy trade-offs, solving a problem
(dead socket) this report does not exhibit — turn-complete arrives, so the
socket is alive.

Five parts:

**A — An expired ask leaves a tombstone, not a hole (F1).** When a shown ask
resolves as *unattended expiry*, the coordinator replaces the actionable
notification with a passive one on the same channel and id — "Permission
timed out — the agent stopped waiting (tap to reopen the session)" — instead
of cancelling outright. No Allow/Deny actions (D4); tapping opens the
session. Resolutions that carry a `device_id` (a human answered somewhere),
an auto-approval sweep, session close (`dropSessionAsks`), and sign-out keep
today's cancel behavior — the tombstone is only for the case where nobody saw
the ask.

**B — Uniform `timed_out` on the wire (F5).** `httpagent.expirePermission`
(covering opencode and kilo) and `acpagent`'s timeout arm (covering grok) set
`TimedOut: true` on their `permission_resolved` (and the question-timeout
paths likewise), matching goose (acphttp) and codex. After part B every
provider reports expiry identically; part A keys on exactly this flag.

**C — Per-channel state in Settings (F4).** Settings reads
`getNotificationChannels()` and, per toggle, warns when that kind's channel
is blocked or silenced at the OS level, with the same visual treatment as the
existing app-level block row. The master-switch probe stays as-is.

**D — A test button per kind (D3).** Each per-kind row gains a "send test
notification" affordance that fires that kind's real `show...()` path with
sample content. This validates channel, icon, importance, and actions on the
actual device — turning any future "X doesn't fire" report into a
sixty-second diagnosis.

**E — Error notifications carry the error (F3).** `showError` receives
`ev.error ?? ev.text`, clipped to notification-body length.

### Consequences

* Good, because the exact reported experience — walk away, come back, find
  only "Agent finished" — becomes impossible for an asked-and-expired
  permission: either the actionable notification is still live, or its
  tombstone says what happened and when.
* Good, because a per-channel block stops masquerading as an app bug: the
  toggle that cannot deliver says so, next to the switch the user is staring
  at (D2).
* Good, because part D gives every future report a reproducible first step on
  the reporting device — including the original reporter's phone, which this
  investigation could not examine.
* Good, because no security posture changes: tombstones are non-actionable,
  and every resolution a human made still cancels (D4).
* Neutral, because part B changes no contract for permissions — it brings
  three providers into conformance with what `protocol-v1.md:1104` already
  promises — and is a documented additive extension for questions. Old
  clients ignore the field either way.
* Neutral, because a tombstone occupies the shade until dismissed; that is
  the point, but users who never answer asks will see more retained
  notifications than today.
* Bad, because none of this can *rule out* an OS/OEM cause on the reporter's
  device — parts C and D are how that gets confirmed or eliminated, not a
  guarantee of delivery.

### Confirmation

| # | Claim | How confirmed |
|---|---|---|
| C1 | An ask that expires unattended leaves a tombstone with no actions | ✅ tests: `an expired shown ask leaves a tombstone, not a cancel`, `question expiry gets the same tombstone` (notifications_test); live check pending Phase 6 |
| C2 | A human-answered ask still cancels outright | ✅ test: `a human-resolved ask cancels as before`; plus `session close still cancels, never tombstones` |
| C3 | every provider marks timeout resolutions | ✅ Go: `TestPermissionExpiryRejectsAndNotifies` / `TestQuestionExpiryRejectsAndNotifies` (httpagent), `TestPermissionTimeoutCancels` / `TestRespondPermissionTimeoutLeavesDeviceEmpty` / `TestAskUserQuestionTimeoutMarksTimedOut` (acpagent); acphttp/codex pre-existing. F5 matrix ✅ ×5. Live expiry pending Phase 6 |
| C4 | Settings warns on a blocked channel | ✅ widget tests: `a blocked channel warns next to its own toggle only`, `no channel info … shows no rows`, `the app-level block suppresses per-channel rows`; live check pending Phase 6 |
| C5 | Test buttons deliver on-device | ✅ tests: `sendTestNotification drives the real show paths`, `taps on a test notification neither navigate nor respond`, widget `test buttons fire their kind and gate with the master`; live check pending Phase 6 |
| C6 | Error body carries the message | ✅ test: `error notifications carry the error field, text as fallback`; live check pending Phase 6 |
| C7 | No regression in the verified-good paths | live re-run pending Phase 6 (the five scenarios in the evidence table) |

## Pros and Cons of the Options

### Evidence-preserving alerts + diagnostics + test path (chosen)

* Good, because it fixes the three holes the evidence actually shows, and
  nothing else.
* Good, because every part is independently small and independently testable.
* Good, because it converts the unfalsifiable part of the report ("my phone
  doesn't show them") into something the user can check themselves (C, D).
* Bad, because it will not satisfy a user whose real complaint is the
  two-minute agent-side timeout itself — that trade-off (agent blocked vs.
  user latitude) is a host config decision (`permission_timeout_seconds`)
  and stays one.

### Lengthen the permission timeout instead

* Good, because it directly widens the window in which the Allow/Deny buttons
  are usable — arguably the most user-valuable two lines of config.
* Good, because codex already ships 900s, so precedent exists.
* Bad, because the agent sits blocked the whole time; 0044 D4.1 chose the
  fail-safe expiry precisely so a missed notification can't hang a turn
  forever.
* Bad, because at any finite timeout the evidence still vanishes at expiry —
  it shrinks F1, never closes it. Compatible with the chosen option as a
  config-guidance footnote, not a substitute.

### Documentation only

* Good, because zero code risk, and F2 (auto-approve means fewer asks than
  users expect) genuinely is a documentation matter.
* Bad, because D2 fails outright: the phone still silently deletes evidence
  and still can't see channel blocks.

### Push-transport rework

* Good, because it would cover the one class this record leaves open: a
  backgrounded process whose socket (and FGS) died entirely.
* Bad, because it contradicts the app's no-cloud, self-hosted model
  (manifest: "All local + mesh only — no cloud push") — a deliberate product
  stance, not an omission.
* Bad, because the evidence shows the socket alive in the failing scenario
  (turn-complete arrives); this solves a different problem.

## More Information

### Reproduction evidence (emulator, API 36, 2026-08-18)

Debug build of the working tree at `45f2eaa`; daemon v0.13.7.1 on this host
over the tailnet; live agent sessions (kilo as the arbitrary repro vehicle);
notifications inspected via
`dumpsys notification` and host-side screen capture. Key receipts: the
permission record with both actions
(`id=297567813 … channel=approval_needed … actions=2 … category=call
importance=4`); the shade rendering Allow/Deny; the timeout transcript line
after the raced Allow; the `agent_error` record appearing within seconds of
`kill -9` on the kilo engine mid-turn. The `SILENT` flag on successive test
notifications is Android's notification cooldown and did not affect display.

### What the reporter can do today (before any fix lands)

* Check per-channel state: long-press any Magic CLI Remote notification →
  the per-category toggles — or Settings → Apps → Magic CLI Remote →
  Notifications: `Approval needed`, `Agent finished`, `Agent error` must all
  be on. A blocked `Approval needed`/`Agent error` with `Agent finished` on
  reproduces the report exactly (F4).
* Expect permission alerts to live `permission_timeout_seconds` (120s
  default) — an ask answered later than that has already been withdrawn (F1).
* Raising `providers.<name>.permission_timeout_seconds` on the host widens
  the actionable window at the cost of the agent staying blocked that long.

### Out of scope, recorded

* **F2** is working-as-designed (each engine's own approval config; 0051
  approval cards) — a settings-copy line ("asks fire only for actions the
  agent's own config doesn't auto-approve") could land with part C's UI work.
* The plugin `requestCode` overflow (upstream), the iOS background story
  (0067 D2 — no background socket by design), and any OEM-specific
  suppression a real device might add.
* `mcremote_client.dart` reconnect/park behavior — verified sound and not
  implicated (turn-complete arrives in the failing scenario).

### Related records

* MADR 0052 B3 — introduced the per-kind toggles and channels.
* MADR 0046 L-5/L-12/M-4 — init-failure surfacing, watch-stack semantics, and
  the cancel-on-close rule part A refines.
* MADR 0044 D4.1 — the permission fail-safe expiry that motivates a tombstone
  rather than a longer wait.
* MADR 0077 §1 — `device_id` on resolutions, the field part A uses to tell
  "a human answered" from "nobody did."
* MADR 0084 A3/D1 — the on-device error diary; part C/D failures record there.

### External references

* [flutter_local_notifications #2254](https://github.com/MaikuB/flutter_local_notifications/issues/2254) — action buttons missing in background (adjacent symptom, not this defect; display was unaffected here)
* [Android notification cooldown (Android 15/16)](https://www.androidpolice.com/android-15-notification-cooldown-vibration/) — alerting reduction, not display suppression; explains the observed `SILENT` flags ([Android Central](https://www.androidcentral.com/apps-software/android-15-notification-cooldown), [Android Authority](https://www.androidauthority.com/android-15-notification-cooldown-great-3537037/))
* [Control notifications on Android](https://support.google.com/android/answer/9079661?hl=en) — per-channel ("category") blocking, the F4 mechanism
