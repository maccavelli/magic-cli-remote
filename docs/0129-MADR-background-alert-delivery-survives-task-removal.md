---
status: proposed
date: 2026-09-01
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Move the host connection into the foreground service's own isolate, so alerts survive a swipe

## Context and Problem Statement

The app's stated P0 is "walk away and get pinged".
[0126](0126-MADR-android-client-debugging-pass-findings.md) F1 found that
`android:stopWithTask="true"` had disabled every path that could restart the
keep-alive service, and P1 removed it. 0126 P7 then tested the result on a real
Android 16 image against a live daemon.

**The service now survives. The feature still does not.**

### Measured, 2026-09-01, AVD `mcremote_test` / API 36, APK 0.15.3.5

Swipe the app from recents — confirmed a real task removal
(`RecentsView: onTaskRemoved: 25`; `dumpsys activity recents` then matches the
app zero times):

```text
process            pid unchanged
service            isForeground=true  foregroundId=42  types=0x200
                   stopIfKilled=false  oom_score_adj=200
FGS notification   still "Connected to host"
socket to daemon   GONE — three samples over 9 s, never returned
```

P1's fix is doing its job: with the flag present the plugin's `onTaskRemoved`
would have called `stopSelf()`, and there were zero
`ForegroundServiceStartNotAllowed` refusals. The same holds under a real
`SIGKILL` — `START_STICKY` recreated the service with a new pid.

**What dies is the Dart isolate.** Established by control rather than inference:
thread names were captured after the swipe, the app relaunched, and the sets
diffed.

```text
present when LIVE, absent after the swipe:
  + 3.io
  + 3.raster
  + DartWorker
```

`DartWorker` is the Dart VM's worker thread. After a swipe there is none, no
`vm-service` listener, and no Flutter log line of any kind.

### Why the service being alive does not help

Every part of the feature lives in the **main isolate**, which is bound to the
Activity's `FlutterEngine` and dies with it:

* the WebSocket to the daemon (`McremoteClient`),
* the 10 s app-ping that holds the host's read deadline open,
* the reconnect ladder and the transport-fallback logic,
* `NotificationCoordinator`, which turns `permission_request` into a
  notification and routes Allow/Deny back to the daemon.

The isolate that *does* survive is `_KeepAliveTaskHandler`
(`foreground_service.dart:13-22`) and it is a deliberate no-op — empty
`onStart`, empty `onRepeatEvent`. It was written to keep the **process** alive
on the assumption that the process is what holds the connection. It is not.

So the architecture keeps alive the one thing that does not matter and lets the
Activity take the rest with it.

### The scope of the harm, stated precisely

This is **not** broken in the ordinary background case. Press Home and the
Activity is stopped but not destroyed: engine alive, socket alive, alerts
arrive. What breaks it is anything that destroys the Activity —

* the user swiping the app off recents, which many people do habitually;
* Android reclaiming the process under memory pressure.

Both are silent. The user is told nothing, and the evidence they *can* see says
the opposite.

### A second defect, found in the same run

The foreground-service notification read **"Connected to host"** for minutes with
no socket at all. MADR 0056 H-5a exists so that this title always reflects real
socket state — `NotificationCoordinator._onConn` updates it on every transition.
After a swipe nothing can update it, because no Dart code runs to notice. The
one surface that is supposed to tell the truth about the connection is the one
guaranteed to be stale exactly when the connection is gone.

### What is already ruled out

* **Battery-optimisation exemption prompting** (0126's deferred item, gated on
  this row). It is aimed at restarting the *service* — which is already alive
  and already restarts. Wrong layer.
* **`autoRunOnBoot` / `autoRunOnMyPackageReplaced`.** Same: they start the
  service, not the isolate that matters.

### What the plugin makes possible

`flutter_foreground_task` 11.0.1 runs a genuine Dart isolate in the service,
started from a `callbackHandle`, with a real contract
(`task_handler.dart`):

```dart
Future<void> onStart(DateTime, TaskStarter);
void onRepeatEvent(DateTime);
Future<void> onDestroy(DateTime, bool isTimeout);
void onReceiveData(Object data);
void onNotificationButtonPressed(String id);
void onNotificationPressed();
```

plus `sendDataToMain` / `sendDataToTask` / `addTaskDataCallback` for
bidirectional traffic with the UI isolate. `onNotificationButtonPressed` matters
particularly: Allow/Deny could be answered from the service isolate, which is
where they would have to be answered if the UI isolate is gone.

## Decision Drivers

* The feature is the reason the app exists, and it fails silently.
* It fails in two ordinary situations, one of which (an OS reclaim) the user
  does not even initiate.
* Whatever is chosen, the notification must stop claiming a connection that is
  not there — that part is not architectural.
* A second isolate holding a second socket collides with the daemon's
  replace-elders behaviour (close code 4001), so "just connect from both" is not
  available.
* 0126 is otherwise complete; this should not hold it open.

## Considered Options

* **A — Move the connection into the service isolate.** The `TaskHandler` owns
  the socket, the ping, the reconnect and alert delivery. The UI isolate becomes
  a consumer over the data channel.
* **B — Accept the limit and make the app honest.** No architectural change;
  document it, and fix the stale notification.
* **C — Hybrid.** The `TaskHandler` does not own the connection, but it detects
  that the main isolate is gone, corrects the notification, and offers a
  one-tap route back.
* **D — Do nothing.**

## Decision Outcome

Chosen option: **"A — move the connection into the service isolate", with C
shipped first as an interim**, because A is the only option that actually
delivers the P0 and C is the only one that stops the app lying while A is built.

A is a real piece of work and should not be rushed under the pressure of a
feature that is already shipped-and-broken; C is small, independently valuable,
and does not have to be reverted when A lands — the `TaskHandler` that C
introduces is the same one A grows into.

B is rejected as a destination but is honest about the cost, and its
documentation half is subsumed by C. D is rejected: the silence is the worst
property of the current behaviour.

### Decisions

* **D1 — The host connection belongs to the foreground service's isolate**, not
  to the Activity's. The service isolate owns the socket, the app-ping, the
  reconnect ladder and alert delivery, for as long as alerts are enabled.
* **D2 — Exactly one isolate holds a socket at a time.** The daemon closes an
  older connection with 4001 when a newer one authenticates
  ([0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md) D3), so two
  isolates connecting would make the app fight itself and produce exactly the
  parked-zombie state 0126 F2 had to fix. Handover is explicit in both
  directions, over `sendDataToTask` / `sendDataToMain`.
* **D3 — The notification never claims a state nothing is maintaining.** Whoever
  owns the connection owns the title. When no owner is running, the title says
  so and offers a way back. This holds under C alone and is not deferred to A.
* **D4 — Allow/Deny must be answerable while the UI isolate is dead**, via
  `onNotificationButtonPressed` in the service isolate. An actionable
  notification whose actions cannot work is worse than no notification —
  0126 F2's reasoning about stale asks applies with more force here.
* **D5 — C ships first and is not throwaway.** The `TaskHandler` C introduces is
  the vehicle A extends; C is a subset of A's structure, not a parallel one.

### Consequences

* Good, because A makes "walk away and get pinged" true for the two cases where
  it is currently false, including the one the user never chose.
* Good, because C removes the lie within days rather than whenever A lands, and
  because a user who has been swiped-out gets told rather than left believing
  they are covered.
* Good, because D2 names the failure mode ahead of time; the obvious naive
  implementation of A produces a self-inflicted 4001 loop.
* Bad, and this is A's real cost: the service isolate shares no memory with the
  UI isolate, so `McremoteClient`, `SettingsStore`, the secure-storage-backed
  client identity and the transcript layer must all be reachable from a second
  isolate, and their state reconciled at every handover. That is the bulk of the
  work and the bulk of the risk.
* Bad, because it deepens coupling to `flutter_foreground_task`. The connection
  would then depend on a third-party plugin's isolate lifecycle — and 0127's
  F-KGP already records that two of this app's plugins are on a deprecation path.
* Bad, because iOS gets nothing from this. MADR 0067 D2 parks the socket there
  by design, and D1 is Android-only by construction; the platforms diverge
  further.
* Neutral, because nothing in 0126 P1–P6 is undone. P1's `stopWithTask` removal
  is a precondition for A, not a mistake — without it the service would not be
  alive to host the isolate.

### Confirmation

The same P7 rows, re-run, with the outcomes that currently fail:

1. Swipe from recents, then raise a `permission_request` host-side → **the alert
   arrives**, and `DartWorker` is present in the surviving process.
2. `SIGKILL` the process → `START_STICKY` recreates the service **and** the
   isolate reconnects, proven by a socket reappearing daemon-side.
3. Tapping Allow on that notification resolves the permission on the host, with
   the UI isolate never started.
4. At no point does the foreground-service notification claim "Connected to
   host" while no socket exists (D3) — the check that failed this time.

C alone is confirmed by 4 plus: after a swipe, the notification says the
connection is gone and tapping it restores it.

## More Information

* Evidence: [0126-PLAN](0126-PLAN-android-client-debugging-pass-findings.md)
  execution record, 2026-09-01 P7 entry — thread-name diff, socket samples,
  `dumpsys` output.
* [0126](0126-MADR-android-client-debugging-pass-findings.md) F1 is the
  necessary-but-not-sufficient fix this record builds on.
* [0056](0056-MADR-mcremote-android-protocol-stack-audit.md) H-5a is the invariant D3
  restores.
* [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md) D3 is the 4001
  behaviour D2 is designed around.
* [0067](0067-MADR-ios-port.md) D2 explains why this is Android-only.
* Row 3 of P7 passed and is unrelated to this record.
