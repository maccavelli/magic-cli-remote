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

## Execution record — 2026-09-02

### I1 — the trace, and what it caught before leaving the desk

Ten `_trace` call sites behind `kDebugMode`, covering every state transition,
socket adopt/detach, ping arm, and `_scheduleReconnect`'s early returns.
Format:

```text
mcremote/trace 2026-09-02T05:40:33.951657 state=connected socket=yes ping=off epoch=2 at=setState
```

**The existing unit suite immediately printed the incoherent pair** — sequence
(2) from the MADR's Confirmation block, without any device involved:

```text
mcremote/trace ... state=connected socket=no ping=off epoch=2 at=teardown:detached
```

So `connected` with no socket is genuinely reachable: `_teardownSocketImpl`
detaches the bundle while the state still says `connected`. In every observed
case the caller corrected it within a millisecond (`state=disconnected` at the
next `setState`), so the window is real but ordinarily closes at once. That is
the window a fix would have to make unreachable — and it is the first direct
evidence for any part of this record.

`flutter analyze` clean, suite unchanged at `+1388 ~3`.

### I2 — the reproduction: **did not reproduce, in four configurations**

Debug APK (`--debug`, versionCode 1, installed with `-r -d`; local release
builds are debug-signed because `android/key.properties` is absent, so the
pairing survived the swap). AVD `mcremote_test`, daemon `macos-laptop`, device
`emu-0129b`.

| # | owner | blocked | result |
|---|---|---|---|
| 1 | UI isolate | mesh | detected in 28 s, failed over to relay, **connected** |
| 2 | UI isolate | mesh + relay | healthy retry ladder, epochs 4→6, never stuck; **recovered by itself** when unblocked |
| 3 | service isolate | mesh | detected in 21 s, failed over to relay, **connected** |
| 4 | service isolate | mesh + relay | healthy retry ladder, epochs 6→7; notification read **"Reconnecting to host / Retrying connection"**, and it **recovered by itself** when unblocked |

Row 4 is the closest configuration to the outage — same isolate owning the
socket, both paths dead — and it is precisely the case that was expected to go
silent. It did not. The client kept cycling
`error → connecting → error → reconnecting`, the trace never stopped, the
notification told the truth throughout, and the connection came back
unattended once the rules were removed.

Rules applied and removed in the same session;
`iptables -L -n | grep -cE '7531|8443'` printed 0 afterwards. Relay endpoint
was `52.2.52.22:8443`, found from the emulator's own socket table.

### What this rules out, and what is left

**Ruled out** — none of these is the cause on its own:

* the transport core's reconnect ladder (rows 2 and 4 exercise it hard);
* the mesh→relay failover (rows 1 and 3);
* the service isolate's ownership of the connection (rows 3 and 4);
* the notification path (row 4 reported "Reconnecting" correctly for minutes).

**Left standing:** the outage happened on a build and in a machine state that
this session did not recreate. The one difference already on record is that the
original evening's transport was **dying spontaneously every ~40 s** before the
outage — 21:40, 21:43, 22:22, 22:43 all show unforced drops — whereas tonight's
link was stable and every drop was one this session caused. Repeated rapid
failovers, or the daemon/relay in the state that produced them, is the most
obvious untested ingredient.

The second difference is the build: the outage was release `0.15.3.13`,
tonight was a debug build. Debug changes timing and enables assertions, so a
race is exactly the kind of defect it could hide.

### Deviation — I3 cannot be completed as written

The plan's I3 says "amend the MADR with the answer". There is no answer yet:
the MADR's Confirmation block asks which of four sequences occurs, and the
reproduction produced none of them. Sequence (2) was observed *in the unit
suite* as a sub-millisecond window that always closed, which is evidence about
the mechanism but not about the outage.

Recorded rather than papered over: this phase ends with the question open, a
working instrument, and four configurations eliminated. What it does **not**
end with is a fix, and nothing here justifies landing one.
