---
status: "proposed"
date: 2026-09-02
associated-madr: "0130-MADR-a-superseded-socket-must-not-close-the-live-one.md"
---
# Implement: a superseded socket's close event must not act on the live one

Associated MADR: [0130-MADR-a-superseded-socket-must-not-close-the-live-one.md](0130-MADR-a-superseded-socket-must-not-close-the-live-one.md)

## Goal

The app can no longer reach a state where it holds no socket, runs no ping,
has no reconnect scheduled, and reports `connected`. Concretely: re-running the
measurement in the MADR must end with a live connection, not eleven minutes of
silence behind a notification that claims otherwise.

## Scope

### In scope

* `apps/mobile/lib/data/ws/mcremote_client.dart` — the socket callback binding
  (`:1977`, `:2211`), `_onSocketDone` (`:2541`), `_onSocketError` (`:2529`),
  and the periodic ping body (`:2419`) where the D3 invariant is checked.
* `apps/mobile/test/` — new tests for the two-socket sequence, the 4001 park on
  a current epoch, and the D3 invariant.

### Out of scope

* **`kAppPingPeriod` / `kAppPingTimeout` / the two-miss rule.** MADR D4: the
  measurement shows detection already takes 25 s and works. Nothing here tunes
  it, and a change to those constants is a different decision.
* The daemon. It behaved correctly throughout — closing the older duplicate
  with 4001 is 0068 D3 working as designed.
* The foreground-service notification. It reported the client's state
  faithfully; 0129 already covers that surface.
* `_teardownSocketImpl`'s internals. It already only closes the bundle it
  observed on entry; the defect is in who calls it.

## Stability rule

`mcremote_client.dart` is the transport core for every platform and every
session. Each phase below is independently revertable, and P1 (the fix) is
useful without P2 (the invariant). Land and verify them separately, so a
regression can be bisected to one of them rather than to "the 0130 change".

## Implementation Steps

### P1 — A close or error belongs to the socket that raised it (D1, D2)

Bind both callbacks with the epoch current at `listen` time, at both call sites
(`:1977` pair leg, `:2211` connect leg):

```dart
final boundEpoch = _connectEpoch;
_sub = _channel!.stream.listen(
  _onMessage,
  onError: (Object e) => _onSocketError(e, boundEpoch),
  onDone: () => _onSocketDone(boundEpoch),
  cancelOnError: false,
);
```

`_onSocketDone(int epoch)` and `_onSocketError(Object e, int epoch)` return
immediately when `epoch != _connectEpoch`, before reading `_channel`, before
touching state, and before any teardown. Log the drop once at `debugPrint` —
a stale close is expected during failover, but a *burst* of them is a signal
worth being able to see.

Keep the 4001 branch exactly as it is otherwise, including its comment: under
the guard it is reached only when the close arrives on the live epoch, which is
the second-device case it was written for (D2).

**Note for the implementer:** `_onMessage` is deliberately left unbound. A
message can only arrive on a stream that is still subscribed, and messages from
a superseded socket are already handled by the epoch checks inside `request`
and the pending map. Widening the change there is out of scope.

**Verification:** the tests in P3, plus `flutter analyze` clean.

### P2 — `connected` implies a live socket and an armed ping (D3)

In the periodic ping body, before the existing
`if (_state == McConnectionState.connected)` send, assert the invariant and
repair it rather than sitting in it:

```dart
if (_state == McConnectionState.connected &&
    (_channel == null || !(_pingTimer?.isActive ?? false))) {
  // connected with nothing behind it: whatever produced this, sitting in it
  // means silent, indefinite offline (MADR 0130 D3).
  debugPrint('mcremote: connected with no live socket — repairing');
  _setState(McConnectionState.error);
  _scheduleReconnect();
  return;
}
```

**Deliberate subtlety, do not "simplify" it away:** the ping timer cannot
observe its own cancellation — if `_pingTimer` was cancelled, this body never
runs. So the check as written catches the null-channel half on the timer, and
the armed-ping half has to be checked from somewhere that still runs. Put the
second half where the state is *entered* (`_setState`, on transition into
`connected`) and where the app resumes, not only on the ping. Whichever
placement is chosen, P3's third test must fail without it — if the test cannot
be made to fail, the check is in the wrong place and the phase is not done.

**Verification:** P3's invariant test, and the on-device re-run in P4.

### P3 — Tests that drive the sequence no existing test covers

Three cases, in `apps/mobile/test/` (new file
`mcremote_socket_epoch_test.dart` unless an existing client test file is the
better home):

1. **A stale close must not touch the live leg.** Connect, adopt a second
   socket (the failover), then deliver a `done` with `closeCode`
   `kCloseReplaced` for the **first**. Assert: `debugSocketResourcesHeld` still
   reports the live bundle, `debugPingArmed` is true, and state is still
   `connected`. Both seams already exist (`:1000-1002`).
2. **A current-epoch 4001 still parks.** Connect, deliver the 4001 close on the
   live epoch. Assert: state parks, no reconnect timer is armed, and resources
   are released. This is the regression guard for D2 — the fix must not turn
   the park into a reconnect loop that fights another device.
3. **The invariant repairs a stuck state.** Force `connected` with no live
   socket, and assert the client leaves `connected` and schedules a reconnect
   within one ping period.

**Each of the three is mutation-tested before it is trusted**, against a
scratchpad copy of the tree and never the working tree (the session that found
this bug also produced a "0 violations" pass from a sampler that had already
exited, and a `grep` that reported a missing manifest entry that was present —
an unverified instrument is worth nothing here). Record in the execution record
what each mutation was and what the failure looked like.

### P4 — Re-run the measurement on device

The same experiment as the MADR, so the before/after is comparable:

```bash
adb root
adb shell "iptables -I OUTPUT 1 -d <daemon-tailnet-ip> -p tcp --dport 7531 -j DROP; \
           iptables -I INPUT  1 -s <daemon-tailnet-ip> -p tcp --sport 7531 -j DROP"
# ... observe ...
adb shell "iptables -D OUTPUT -d <daemon-tailnet-ip> -p tcp --dport 7531 -j DROP; \
           iptables -D INPUT  -s <daemon-tailnet-ip> -p tcp --sport 7531 -j DROP"
```

**Remove the rules in the same session that added them**, and verify with
`iptables -L -n | grep -c 7531` returning 0. They are confined to the
disposable AVD and touch nothing on the host.

**On-device observation, and this is the gate:** with the mesh path blocked,
the client fails over to relay and **keeps a live connection**; when the block
is removed it is still connected. Liveness is read from the daemon log as a
running count (`device authenticated` +1, `ws client disconnected` −1) — **not**
from `lsof`, which in the 0129 session reported the same stale fd and port as
ESTABLISHED across an hour of connects and straight through a window the daemon
logged as disconnected.

The failure signature to watch for is its absence: no window in which the
running count is 0 while the notification reads "Connected to host".

## Verification (whole plan)

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && flutter analyze && flutter test
```

plus P4's on-device run. The suite is a regression guard here, not evidence for
the goal — no unit test can show that the app stays online across a real
transport failover.

### Acceptance criteria

1. A 4001 close for a superseded socket leaves the live socket, its
   subscription and its ping timer intact. (P3 case 1)
2. A 4001 close on the live epoch still parks without reconnecting. (P3 case 2)
3. `connected` with no live socket resolves itself within one ping period
   rather than persisting. (P3 case 3)
4. On device, blocking the mesh path produces a failover that ends **connected**,
   with no window where the daemon's running count is 0 while the notification
   claims a connection. (P4)
5. `dart format`, `flutter analyze` and `flutter test` clean, with the suite
   count up by the three new cases.

## Rollout and Rollback

Ships in the next APK; no migration, no persisted state, no protocol change —
the daemon is untouched, so a phone on the old build and one on the new build
behave identically from its side.

Rollback is reverting P1 and P2 independently. Reverting P2 alone restores the
old "sit in it" behaviour while keeping the actual fix; reverting P1 alone
restores the defect, so if only one can be kept, keep P1.

## Deferred

* **Detection latency for a silently dead link (~25 s).** Out of scope by D4,
  and not a defect on this evidence: two missed pings at 10 s with a 6 s
  timeout is the documented trade from 0046 L-1. Revisit only with a
  measurement showing 25 s is too slow for a real user-visible case, and treat
  it against 0063, which owns liveness truth.
* **A test that the app recovers across a *real* failover.** P3 simulates the
  two-socket sequence at the client boundary; nothing in the suite exercises an
  actual mesh→relay switch. That needs the harness 0129 P6 did by hand and is
  worth its own decision.
