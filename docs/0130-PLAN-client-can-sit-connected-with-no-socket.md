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

### I4 — the degraded state: repeated rapid failovers on a release build

Added 2026-09-02 after I2 came back negative in all four configurations. I2
eliminated the steady-state cases; this phase tests the two differences the
execution record names between tonight's clean runs and the outage night.

**The trace gate changes, and this is a deviation from I1 as written.** I1 says
"guard the whole thing behind `kDebugMode` so release builds are unchanged",
and that is exactly what makes the release build untestable. The gate becomes:

```dart
const _kForceTrace = bool.fromEnvironment('mc.trace');
...
if (!kDebugMode && !_kForceTrace) return;
```

A default release build is still byte-identical in behaviour — `mc.trace`
defaults to false and the branch is const-folded away. Only a build made with
`--dart-define=mc.trace=true` traces. That keeps I1's promise for anything that
ships while making the hypothesis testable.

**The protocol.** Service isolate owning the connection (task removal), then
flap the mesh path rather than cutting it once: block ~30 s, unblock ~15 s,
repeat. Each cycle forces a detect → failover → recover round trip, which is
the shape the outage night's log shows over and over before it went silent.

**What would confirm it:** the trace stops while the daemon shows no
connection, and the notification keeps claiming one. That is the outage, and
the last lines before the silence name the sequence.

**What would refute it:** the client keeps cycling through an arbitrary number
of flaps. Then the degraded-transport hypothesis is wrong too, and the record
says so rather than reaching for a fifth configuration.

Rules removed in the same session; `iptables -L -n | grep -cE '7531|8443'`
must print 0 at the end.

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

### I4 — degraded state: **hypothesis not confirmed**, but the signature was seen once

Release builds `0.15.3.16` and `.17`, both with `--dart-define=mc.trace=true`.
The gate works as promised: a normal `make apk` build launched clean produced
**0 trace lines** while the app connected and ran, so nothing that ships is
affected.

#### The instrument had a hole, and the first run fell straight into it

I1 traced `startPing` but not the ping *ticks*. So when the first run went
silent there was no way to tell whether pings were succeeding, failing, or not
firing at all — the three possibilities that distinguish the MADR's sequences.
Fixed by tracing `ping:tick` / `ping:ok` / `ping:miss(n)`. Recorded because the
gap only became visible when the instrument was pointed at the real thing:

```text
06:07:01.269  state=connected socket=yes ping=off epoch=2 at=startPing
              ... nothing at all for four minutes ...
```

#### The outage signature reproduced once, before the ping trace existed

Run 1 (`0.15.3.16`, service isolate owning, single-path mesh flap):

```text
06:07:00.788  daemon: device authenticated     (relay leg)     live=2
06:07:00.789  daemon: disconnected replaced    (mesh leg)      live=1
06:07:05.477  daemon: disconnected peer_closed (relay leg)     live=0
06:07:01.269  app:    last trace — connected, socket=yes
              ... four minutes, live=0, device Awake, not dozing ...
              notification: "Connected to host — Listening for approvals"
              emulator socket to relay 52.2.52.22:8443 still ESTABLISHED
```

`mWakefulness=Awake`, `deviceidle mState=ACTIVE` — **sleep is not the
explanation**, which was the first plausible theory and is now ruled out.

The distinguishing feature is a **half-open relay tunnel**: the phone's TCP to
the relay stayed up while the daemon-side leg was gone. The client held a
socket it believed live, and the daemon had nothing.

#### With the ping traced, eight alternating flaps did not reproduce it

Run 2 (`0.15.3.17`, service isolate owning). The first flap protocol was
**wrong and is recorded as such**: cutting one path only moves the client to the
other, after which further cuts to the dead path are a no-op — the "silence" it
produced was an idle, genuinely-connected client. Replaced with an alternating
flap that cuts whichever path is live, forcing a failover every cycle.

Result across the run: **16 epochs, 20 ping misses, 12 recoveries, never
stuck.** Detection is tight and repeatable:

```text
06:28:02.842  ping:tick        state=connected socket=yes ping=on epoch=13
06:28:08.848  ping:miss(1)
06:28:18.910  ping:miss(2)  -> teardown -> error -> reconnecting
06:28:20.900  adoptSocket   -> recovered
```

**16 seconds** from the last good tick to teardown, ~18 s to a new socket. That
is a better measurement than 0129's ~25 s, which was taken from the moment the
link was cut rather than from the ping tick.

#### What I4 establishes

* **The degraded-transport hypothesis is not confirmed.** Repeated rapid
  failovers do not wedge the client; it survived every one of eight.
* **Sleep is ruled out** as the cause of the silent window.
* **The ping detector works**, on a release build, in the service isolate,
  under sustained flapping — `miss(1)`, `miss(2)`, teardown, reconnect.
* **The signature is real and was reproduced once**, with one distinguishing
  feature not present in any healthy run: a half-open relay tunnel, phone-side
  TCP alive, daemon-side leg gone.

#### Still open

Why, in that state, the ping did not miss. Every healthy run shows a miss
within 6 s of a broken link; the stuck run showed none for four minutes. With
`ping:tick` now traced, one more capture of that state would settle it — the
next attempt should target the half-open tunnel directly (drop the daemon's
relay leg while leaving the phone's TCP up) rather than cutting the phone's
path, which is what every reproduction so far actually did.

The held patch stays out of the tree. Nothing here vindicates it.
