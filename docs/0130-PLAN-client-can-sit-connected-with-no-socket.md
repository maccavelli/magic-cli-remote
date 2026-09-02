---
status: "proposed"
date: 2026-09-02
associated-madr: "0130-MADR-client-can-sit-connected-with-no-socket.md"
---
# Investigate: the client can sit `connected` with no socket

Associated MADR: [0130-MADR-client-can-sit-connected-with-no-socket.md](0130-MADR-client-can-sit-connected-with-no-socket.md)

## Goal

Answer one question with an observation rather than a reading of the code:
**which of the four sequences in the MADR's Confirmation block actually
occurs** during the eleven-minute window. No fix is designed until that is
known, and no transport-core change lands without a test that has been seen to
fail (MADR D1).

## Scope

### In scope

* `apps/mobile/lib/data/ws/mcremote_client.dart` — a debug-only trace of the
  four values that disagreed: connection state, `_channel` presence, whether
  the ping timer is armed, and `_connectEpoch`.
* One emulator session reproducing the outage with the same black-hole.
* Amending the MADR with the answer.

### Out of scope

* **Any fix.** Including the two changes already written and held at
  `scratchpad/0130-hardening-unproven.patch`. They stay out of the tree until
  the trace says what to aim at (MADR D1). They are not deleted — the patch is
  the record of what was tried and why it was not trusted.
* **`kAppPingPeriod` / `kAppPingTimeout` / the two-miss rule** (MADR D4).
* The daemon, which behaved correctly throughout.
* The foreground-service notification, which reported the client's state
  faithfully — 0129 owns that surface.

## Stability rule

The trace is additive and debug-gated: it changes no control flow, only what is
printed. If reading it requires changing behaviour to make an event observable,
that is no longer instrumentation and needs its own decision.

## Implementation Steps

### I1 — A trace of the quartet that disagreed

Add a single private helper in `McremoteClient` that prints state, socket
presence, ping-armed and epoch together, and call it from the points where any
of the four changes:

* `_setState` (every transition);
* `_adoptOpenedSocket` and `_teardownSocketImpl` (socket appears / disappears);
* `_startPing` and wherever the ping timer is cancelled;
* `_scheduleReconnect` — including its early returns, which is the one place
  that would show the client declining to reconnect *because* it believes it is
  connected.

Guard the whole thing behind `kDebugMode` so release builds are unchanged.
Format it so a `grep` reconstructs the sequence in order, e.g.

```text
mcremote/trace 23:09:31.284 state=reconnecting socket=no ping=off epoch=7 at=_setState
```

**The early returns matter more than the transitions.** The current logs are
silent for the whole eleven minutes; the interesting event is very likely
something *declining* to act, and nothing today prints that.

**Verification:** `flutter analyze` clean, suite unchanged, and a local run
shows the trace ordering a normal connect → ping → disconnect cycle.

### I2 — Reproduce, with the same instrument as the original

Build a debug APK, install, pair, and put the service isolate in charge (task
removal, as in 0129 P6). Then:

```bash
adb root
adb shell "iptables -I OUTPUT 1 -d <daemon-tailnet-ip> -p tcp --dport 7531 -j DROP; \
           iptables -I INPUT  1 -s <daemon-tailnet-ip> -p tcp --sport 7531 -j DROP"
# ... observe until the client has failed over and gone quiet ...
adb shell "iptables -D OUTPUT -d <daemon-tailnet-ip> -p tcp --dport 7531 -j DROP; \
           iptables -D INPUT  -s <daemon-tailnet-ip> -p tcp --sport 7531 -j DROP"
adb shell "iptables -L -n | grep -c 7531"   # must print 0
```

Remove the rules in the same session that added them and verify the count is
zero. They touch only the disposable AVD.

Liveness is the daemon log's running count (`device authenticated` +1,
`ws client disconnected` −1). **Not `lsof`** — it reported a stale fd and port
as `ESTABLISHED` across an hour of connects during the 0129 session, straight
through a window the daemon logged as disconnected.

**On-device observation, and this is the gate:** the trace covering the window
between the last daemon event and the manual relaunch, showing which of the
four sequences occurred.

**If it does not reproduce**, that is a result too, and it is recorded rather
than retried indefinitely: the original run had a transport dying every ~40 s
of its own accord, which may be a necessary ingredient. Say so, and note what
was different.

### I3 — Amend the MADR with the answer

Record the observed sequence verbatim, name which of the four it was, and only
then decide the fix — as an amendment here or as 0131, depending on whether the
decision in the MADR still stands.

Re-examine the held patch at that point: if the trace vindicates part of it,
say which part and why; if it does not, say that too. Either way it stops being
an open question in the scratchpad.

## Verification (whole plan)

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && flutter analyze && flutter test
```

The suite is a regression guard only — it cannot show anything about the goal,
which is an observation on a device.

### Acceptance criteria

1. A debug build prints the state/socket/ping/epoch quartet at every change and
   at `_scheduleReconnect`'s early returns; release builds are unchanged. (I1)
2. The black-hole is applied and removed in one session, with
   `iptables -L -n | grep -c 7531` printing 0 afterwards. (I2)
3. Either the outage reproduces and the trace names which of the MADR's four
   sequences occurred, or it does not reproduce and the differences from the
   original run are recorded. (I2)
4. The MADR carries the answer, and the held patch is either adopted with a
   test that has been seen to fail, or dropped with a reason. (I3)

## Rollout and Rollback

Nothing ships. The trace is debug-only and is removed in the change that fixes
the cause. Rollback is deleting the helper and its call sites.

## Deferred

* **The hardening patch** (`scratchpad/0130-hardening-unproven.patch`): epoch
  guards on `_onSocketDone` / `_onSocketError`, and the invariant that
  `connected` implies a live socket. Both are defensible; neither is
  demonstrated. Trigger: I3.
* **Cancelling the previous subscription in `_adoptOpenedSocket`.** Noticed
  while investigating: the callers overwrite `_sub` without cancelling it. On
  every path examined a teardown has already cancelled it first, so this is
  currently harmless — but it means the invariant "`_sub` belongs to
  `_channel`" is held by convention across three call sites rather than by
  construction. Worth closing on its own merits, not as part of a fix.
* **Detection latency for a silently dead link (~25 s).** Out of scope by
  MADR D4 and not a defect on this evidence.
