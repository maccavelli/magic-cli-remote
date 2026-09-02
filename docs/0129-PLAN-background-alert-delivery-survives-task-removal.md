---
status: in-progress
date: 2026-09-01
associated-madr: "0129-MADR-background-alert-delivery-survives-task-removal.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0129 — Stop lying first, then move the connection into the service isolate

Implements [0129-MADR-background-alert-delivery-survives-task-removal.md](0129-MADR-background-alert-delivery-survives-task-removal.md)
decisions D1–D5.

## Goal

Swipe the app off recents, and an approval alert still arrives — and until that
is true, the app says plainly that it will not.

Finish line:

* **Phase C** (P1–P2): after a task removal the notification stops claiming a
  live connection and offers a one-tap route back;
* **Phase A** (P3–P6): the socket, the ping, the reconnect and alert delivery
  live in the service isolate, and P7's rows 1–3 from 0126 pass on hardware or
  emulator.

## Scope

### In scope

Phase C:

* `apps/mobile/lib/data/notifications/foreground_service.dart` — a real
  `TaskHandler` in place of the no-op
* `apps/mobile/android/app/src/main/AndroidManifest.xml` — only if a
  notification action needs declaring
* tests for the handler's notification-state logic

Phase A, additionally:

* `apps/mobile/lib/data/ws/mcremote_client.dart` — construction from a second
  isolate; explicit ownership handover
* `apps/mobile/lib/data/local/settings_store.dart`,
  `lib/data/ws/client_identity.dart` — second-isolate access to secure storage
* `apps/mobile/lib/data/notifications/notification_coordinator.dart` — running
  service-side, and answering asks
* `apps/mobile/lib/app_lifecycle.dart` — handover on foreground/background
* `apps/mobile/lib/state/transcripts_notifier.dart` — reconciliation after a
  period with no UI isolate

### Out of scope

* **iOS.** MADR 0067 D2 parks the socket there by design; D1 is Android-only.
  Do not "harmonise" the platforms in this record.
* **Replacing `flutter_foreground_task`.** The coupling is a named cost in the
  MADR, not a task here.
* **Battery-optimisation prompting** and `autoRunOnBoot`. The MADR rules both
  out as wrong-layer.
* **Anything in 0126 P1–P6.** Complete and unaffected; P1's `stopWithTask`
  removal is a precondition for this work.
* **Row 3 of 0126 P7.** Passed; unrelated.

## Stability rule

Every phase ends with:

```bash
cd apps/mobile && dart format . && flutter analyze && flutter test
```

then **one commit** (`git commit --no-edit`; never `-m`).

**Widget tests cannot see any of this.** The defect this record exists for was
invisible to 1367 passing tests, because it lives in isolate lifecycle. A green
suite is necessary and proves nothing about the goal; every phase names the
on-device observation that does.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — One socket at a time.** D2. Before either isolate dials, the other must
have released. Two live sockets make the daemon close one with 4001 and the app
fights itself — the parked-zombie state 0126 F2 had to repair. Any phase that
can produce two connections is wrong even if it demoes correctly.

**C2 — The notification never outlives its owner's knowledge.** D3. If no
isolate is maintaining the connection, the title says so. This holds from P2
onward, not only at the end.

**C3 — An actionable notification's actions must work.** D4. Do not ship a
service-isolate alert carrying Allow/Deny until those buttons resolve the
permission host-side with no UI isolate running. A dead button is worse than a
passive notice — 0126 F2's reasoning about stale asks, one layer up.

**C4 — Every phase states its on-device observation.** See the stability rule.

**C1 and C3 are the ones at risk.** C1 because handover is invisible in a
single-isolate debug run; C3 because the notification will *look* right long
before the buttons work.

## Dependency and delivery order

```text
P1 (TaskHandler skeleton + data channel)
  -> P2 (honest notification)          <- Phase C ships here, independently useful
    -> P3 (second-isolate storage + identity)
      -> P4 (client in the service isolate, handover)
        -> P5 (alert delivery + Allow/Deny service-side)
          -> P6 (on-device verification)
```

P2 is a release boundary. If P3 onward stalls, the app is still honest.

## Implementation Steps

### P1 — A real TaskHandler and a data channel (D5)

Replace `_KeepAliveTaskHandler`'s empty methods with a handler that starts, logs
its lifecycle, and exchanges messages with the UI isolate over
`sendDataToMain` / `sendDataToTask` / `addTaskDataCallback`.

No connection logic yet. The point is to establish that a second isolate runs at
all, survives task removal, and can talk to the UI isolate when there is one.

**On-device observation:** after a swipe, the service isolate logs a heartbeat
and `DartWorker` is present in the surviving process — the exact thread whose
absence is the finding in 0126 P7.

### P2 — The notification stops lying (D3, C2) — Phase C ships

The handler tracks whether a UI isolate is alive (last message received, plus
`onDestroy`). When it is not, the handler updates the notification itself:

```text
title  Alerts paused
text   Tap to reconnect to your host
```

and `onNotificationPressed` launches the app.

Pick the wording deliberately: it must not imply an error, and must not claim a
connection. "Alerts paused" is a state the user can act on; "Disconnected" reads
as a fault they must diagnose.

**On-device observation:** swipe the app away; within one handler tick the
notification changes from "Connected to host"; tapping it restores the app and
the connection. This is the check that failed in 0126 P7 and is the acceptance
criterion for Phase C on its own.

### P3 — Secure storage and identity from a second isolate (D1)

`SettingsStore` and `ClientIdentity` back onto `flutter_secure_storage` and
`SharedPreferences`, both of which are platform-channel plugins. Establish
whether they work from a service isolate at all — background isolates need their
own `BackgroundIsolateBinaryMessenger` registration, and this is the phase most
likely to discover that something does not work rather than that it does.

**If a plugin cannot serve the service isolate, stop and report.** The fallback
shapes — passing credentials across the data channel, or having the UI isolate
prime a cache — have real security consequences (a device token crossing an
isolate boundary, or living longer in memory) and are not a decision to take
mid-phase.

**On-device observation:** the service isolate reads the pinned fingerprint and
the client identity, and logs their presence, with no Activity running.

### P4 — The client moves, with explicit handover (D1, D2, C1)

`McremoteClient` is constructed in the service isolate and owns the socket. The
UI isolate asks for the connection when it comes to the foreground, and yields
it when it goes away.

Handover, both directions, is a message and an acknowledgement — never a
timeout, never optimistic. The releasing side confirms its socket is closed
before the acquiring side dials.

Two things this phase must not do: let both isolates dial (C1), and let the
handover drop the resume token — `_resumeToken` is memory-only by design
(0068 D4), so a handover that loses it turns every app switch into a full
reconcile.

**On-device observation:** foreground/background the app repeatedly while
watching `lsof -iTCP:7531` daemon-side. Exactly one connection at all times, and
never zero while alerts are enabled.

### P5 — Alerts and answers from the service isolate (D4, C3)

`NotificationCoordinator` runs service-side against the service-side client.
`onNotificationButtonPressed` maps Allow/Deny to `respondPermission`.

`showsUserInterface: true` on the Android actions exists precisely because the
plugin's default routed action taps to a background isolate that could not reach
the WebSocket (`notification_service.dart:187-192`). That reasoning inverts once
the socket lives in the service isolate — revisit it deliberately and record why
whichever way it lands.

**On-device observation, and this is C3's gate:** with the app swiped away,
raise a `permission_request`, tap **Allow** on the notification, and confirm the
permission resolves host-side with no Activity ever started.

### P6 — Re-run 0126 P7 (D1)

Rows 1 and 2 of 0126 P7, which currently fail, plus the notification check:

```text
1  swipe from recents, raise permission_request  -> alert arrives; DartWorker present
2  SIGKILL the process                           -> service AND isolate return;
                                                    socket reappears daemon-side
3  Allow from the notification, no UI isolate    -> resolves host-side
4  at no point does the title claim "Connected"  -> while no socket exists
```

Use a real `SIGKILL` for row 2. `adb shell am kill` — what 0126 P7 originally
specified — cannot kill a foreground-service process and tests nothing; that is
recorded in 0126's execution record.

Then close 0126: its P7 rows 1–2 pass, and its GATED battery-optimisation entry
resolves as "not needed" if rows 1–2 pass without it.

## Verification (whole plan)

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && flutter analyze && flutter test
make apk && make manifest-surface
```

plus the six on-device observations above. The suite is a regression guard here,
not evidence for the goal.

### Acceptance criteria

1. Phase C alone: after a swipe the notification stops claiming a connection,
   and tapping it restores the app. (P2)
2. Swipe from recents, then a host-side `permission_request` produces a
   notification. (P6 row 1)
3. `SIGKILL` → the service and the isolate both return and the socket
   reappears daemon-side. (P6 row 2)
4. Allow, tapped with no UI isolate running, resolves the permission host-side.
   (P6 row 3, C3)
5. Exactly one socket at any moment across a hundred foreground/background
   cycles. (C1)
6. `flutter test` at or above 1367, and `make manifest-surface` still passes —
   a new notification action or receiver must be an allowlist decision, not a
   surprise.

Criterion 5 is the one most likely to be skipped and most likely to bite:
0126 F2 exists because a second connection was allowed to strand the first.

## Rollout and Rollback

Phase C is additive and revertible on its own — the handler changes what the
notification says and nothing else.

Phase A changes where the connection lives, which is not a small revert. It
should land behind whatever release cadence allows a real soak, and the fallback
is Phase C's honesty, which stays correct even if A is reverted.

Nothing here changes the daemon or the protocol, so a phone on an older build
keeps working against the same host.

## Deferred

* **iOS parity.** Out of scope by 0067 D2; the platforms diverge further and
  that is a known, accepted cost.
* **Reducing the `flutter_foreground_task` coupling.** Named as a cost in the
  MADR. Relevant to 0127's F-KGP watch, not resolvable here.
* **Whether the app-ping cadence should differ service-side.** 0126 D5 pinned it
  to `kLinkFreshFor` for UI freshness. With no UI, that constraint may not
  apply — but changing it belongs with the measurement in 0128 P4, not here.

## Execution record — 2026-09-01

### P1 + P2 — Phase C complete (D3, D5)

`_KeepAliveTaskHandler` — empty `onStart`, empty `onRepeatEvent` — is now
`McRemoteTaskHandler`. `eventAction` moved from `.nothing()` to a 30 s repeat;
the comment it replaced ("the service only keeps the process alive") *was* the
mistaken assumption, and the code now says so.

The two isolates share no memory, so the whole vocabulary between them is a
heartbeat carrying the title and text the UI isolate wants shown. Sent on a
timer **and** on every connection transition — the timer alone would leave up to
one interval of staleness after a state change.

**The design call worth recording:** a handler that has *never* seen a heartbeat
reads as paused. That is the `START_STICKY` case — the system recreated the
service after a process death, so the main isolate is gone and never checked in.
Assuming "alive until proven otherwise" would hold a stale "Connected to host"
for the entire grace window, which is the bug itself. Proven to discriminate:

```text
with `return false` for the never-seen case:
  no heartbeat ever seen reads as paused [E]  Expected: true  Actual: <false>
restored: +5 All tests passed
```

Wording is deliberate: "Alerts paused" is a state the user can act on;
"Disconnected" reads as a fault to diagnose, and nothing is faulty — nothing is
maintaining it.

#### On-device observation (AVD `mcremote_test`, API 36, APK 0.15.3.6)

Paired to the live daemon, then swiped from recents:

```text
BASELINE   title "Connected to host"; DartWorker present; socket up
           engines 1.* and 2.*  (2.* is the new service isolate — P1's check)

SWIPE      recents entries        0
           process                alive (pid 2345)
           FGS record             1
           DartWorker             gone      <- main isolate dead, as in 0126 P7
           socket                 gone

RESULT     title "Alerts paused"
           text  "Tap to reconnect to your host"      <- the check that failed in 0126 P7

TAP        top activity  magic_cli_remote/.MainActivity
           socket        restored
           title         "Connected to host"          <- handler stood down
```

Phase C's acceptance criterion is met: the notification no longer claims a
connection nobody is maintaining, and one tap restores it.

**What this does NOT do**, restated so the record cannot be misread: alerts
still do not arrive after a swipe. That is Phase A (P3–P6). Phase C makes the
app honest about it, nothing more.

#### Two measurement errors, both mine

* The title-monitoring loop used `grep -B4 -A16 "channel=host_connection"` and
  reported "no notification" ten times running while the notification was
  present and correct the whole time — the pattern assumed a line ordering
  `dumpsys` does not guarantee. A plain `grep -oE 'android\.title=...'` showed
  the truth immediately. The flip therefore happened somewhere inside a 150 s
  window rather than at a captured moment; the *outcome* is verified, the
  latency is not.
* A `DartWorker` count read 0 on a connected app, contradicting the evidence
  0126 P7 rests on. It was a nested command substitution racing the app
  restart; a clean re-read showed `DartWorker` present. The 0126 finding stands
  — verified there by a controlled before/after diff, not by a single sample.

Seventh and eighth verification-of-a-verification failures in this body of work.
Both were caught because the result contradicted something already established.

#### Scope addition — `docs/ops-android-emulator.md`

Not in this plan's file list. Pairing failed three times with **"code already
used"** before the cause was found: **the emulator loads the virtual-scene
poster at boot and caches it.** Every poster swap after boot was invisible, so
the camera kept showing the first QR, whose one-shot code had been consumed an
hour earlier. The ops doc documented overwriting `poster.png` but not that the
swap must precede boot. Added, with `mcremote pair list` named as the check — a
code that was never claimed does not appear there at all.

P6 will need pairing again, so leaving this undocumented would have cost the
same hour twice.

### P3 — Secure storage and identity from the service isolate: **both work** (D1)

This phase was written as the gate — *"the phase most likely to discover that
something does not work rather than that it does"* — with instructions to stop
and report if a plugin could not serve the service isolate, because the
fallbacks put a device token across an isolate boundary.

**No fallback is needed. Both plugins answer.**

#### Why, before the measurement

`flutter_foreground_task` does not spawn a bare isolate. It creates a **full
`FlutterEngine`** for the task
(`ForegroundTask.kt:49`, `flutterEngine = FlutterEngine(context)`), takes that
engine's own `binaryMessenger`, and runs the entry point through
`DartExecutor.executeDartCallback`. That engine registers plugins, so the task
isolate is not a "background isolate" needing
`BackgroundIsolateBinaryMessenger.ensureInitialized` — it is a second engine
with a working channel of its own.

#### Measured, on device, twice

A temporary probe in the handler read `SharedPreferences` and
`flutter_secure_storage`, logging presence and length only — never a value,
since this lands in a device log.

With the main isolate **alive**:

```text
mcremote/fgs: task isolate started (system)
mcremote/fgs/probe[onStart]: SharedPreferences OK, 10 keys, host=true
mcremote/fgs/probe[onStart]: secure storage OK, device_token present=true len=68
```

With the main isolate **dead** — the case D1 actually needs — fired when the
handler concluded the UI isolate was gone:

```text
mcremote/fgs/probe[uiIsolateGone]: SharedPreferences OK, 10 keys, host=true
mcremote/fgs/probe[uiIsolateGone]: secure storage OK, device_token present=true len=68

corroboration at the same instant:
  DartWorker  0            <- main isolate gone
  socket      0            <- nothing connected
  title       "Alerts paused"
```

So the service isolate can read the host, the device id, the pins and the device
token with no Activity in existence. **D1 is achievable as designed**, and the
security question the phase flagged does not arise: no credential has to cross
an isolate boundary, because the isolate that needs it can fetch it.

#### Incidental: the grace window, measured

The `uiIsolateGone` probe fired at **~t+90 s** after the swipe, against a 95 s
window and a 30 s check interval. That is the flip latency P2 could not capture
because its monitoring grep was broken — recorded here since the probe happened
to timestamp it.

#### The probe is removed

It answered its question, and a permanent version would read the device token on
every pause transition — unnecessary work against a secret, for no ongoing
benefit. P4 will read storage as part of doing its job, not as diagnostics.
Gate after removal: `dart format` clean, `flutter analyze` clean,
`flutter test` `+1372 ~3`.

### P4 — The connection moved into the service isolate (D1, D2, C1)

**The rule implemented**, narrower than "ownership changes on every lifecycle
transition" and deliberately so: the service isolate takes the connection **only
when the UI isolate is confirmed gone**, and hands it straight back when the UI
isolate reappears. While the UI isolate lives — foreground or backgrounded — it
keeps the socket exactly as before, so nothing about today's working behaviour
changes.

That choice also makes C1 nearly free in one direction. When the handler
concludes the UI isolate is gone, that isolate is not holding a socket either —
0126 P7 measured zero connections daemon-side in exactly that state — so there
is nothing to collide with. Only the hand-back needs an acknowledgement.

* `_takeOwnership()` builds an `McremoteClient`, calls
  `reconnectFromStore(SettingsStore())` — reachable from here, proven in P3 —
  and drives the notification title from the real connection state, so MADR
  0056 H-5a's invariant holds no matter which isolate owns the socket.
* `_releaseOwnership()` disconnects with `manual: false` (a handover, not a
  sign-out: pairing and stored credentials are untouched), then sends
  `mc.released`.
* `ForegroundServiceController.claimOwnership()` sends a heartbeat and waits for
  that acknowledgement. A timeout does **not** mean "dial anyway" — it means the
  service never answered, which is almost always because no service is running,
  and the caller is told which happened.
* `app_lifecycle.dart` gates its `reconnectFromStore` behind that claim.

#### On-device: the service takes over (APK 0.15.3.8)

Swiped from recents, then watched:

```text
t+015s..t+075s   sockets=0   "Connected to host"    <- stale title, pre-takeover
t+090s           sockets=1   "Connected to host"

at t+090s, corroborated:
  recents entries   0                      <- app swiped away
  top activity      nexuslauncher          <- not foreground
  engines           1.raster only          <- the service's engine, alone
  notification      "Connected to host"
                    "Listening for approvals — app closed"
  handler log       took ownership, state=McConnectionState.connected
  daemon            emu-0129b LAST_USED 2026-09-02T01:42:12Z
```

**A live, authenticated connection to the daemon, with the app swiped away and
no Activity in existence.** That is D1, and it is the thing 0126 P7 row 1 proved
impossible before.

#### On-device: C1 across the hand-back

The relaunch sampled every 500 ms for 30 s, spanning the handover:

```text
samples 60   max concurrent sockets 1   distribution: 60x 1
handler log  released ownership
title        "Connected to host"
```

Never two, and never zero. The acknowledgement does its job.

#### Correction: `DartWorker` is not a liveness indicator

Recorded because this session leaned on it repeatedly. After the relaunch the
main isolate is unambiguously alive — `topResumedActivity` is
`magic_cli_remote/.MainActivity`, the socket is up, and a second engine
(`3.raster`) exists — and `DartWorker` is **absent**. It is a Dart VM worker
thread created on demand, not a per-isolate fixture.

0126 P7's conclusion still stands: it rested on a controlled before/after diff in
which `3.io`, `3.raster` **and** `DartWorker` all disappeared together, and was
independently corroborated by zero sockets daemon-side and no Flutter log line
of any kind. But single-sample "DartWorker: 0" was weaker evidence than it was
presented as, in this record and in earlier summaries. **The reliable indicators
are the numbered engine thread groups (`N.raster` / `N.io`) and the socket.**

#### Not verified

* Acceptance criterion 5 asks for a hundred foreground/background cycles; this
  was one hand-back, sampled densely. One clean handover is not a hundred.
* P5's work — alert delivery and Allow/Deny from the service isolate — is
  untouched. The service now holds a connection, but `NotificationCoordinator`
  still runs only in the UI isolate, so a `permission_request` arriving while
  the app is swiped away is received and dropped.

Gate: `dart format` clean, `flutter analyze` clean, `flutter test` `+1372 ~3`.
