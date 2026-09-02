---
status: proposed
date: 2026-09-02
decision-makers: maccavelli
consulted: —
informed: —
---

# A superseded socket's close event must not act on the live one

## Context and Problem Statement

The app can end up **permanently offline while its notification says
"Connected to host"**. Measured 2026-09-02 on AVD `mcremote_test` / API 36 with
APK `0.15.3.13`, daemon `macos-laptop`, device `emu-0129b`:

```text
23:09:06  mesh path black-holed (iptables DROP, both directions, emulator only)
23:09:31  mcremote: Mesh died under a live session; reconnecting over Relay
23:09:32  daemon: device authenticated                    <- relay leg is up
23:09:32  daemon: ws client disconnected reason=replaced  <- daemon closes the mesh leg (4001)
23:09:37  daemon: ws client disconnected reason=peer_closed
          ...
23:13     iptables rules removed; the network is healthy again
23:20:29  daemon has logged nothing since 23:09:37.
          Live connections for this device: 0, for 11 minutes.
          App process alive. Foreground notification still reads
          "Connected to host — Listening for approvals — app closed".
```

The app never recovered on its own. Only relaunching it restored the
connection. Seven of those eleven minutes were on a perfectly healthy network,
so this is not a link problem — the client had stopped trying and did not know
it.

This is the failure [0129](0129-MADR-background-alert-delivery-survives-task-removal.md)
exists to prevent, one layer down. 0129 made the notification track *the
client's belief*; this record is about the client's belief being wrong and
never being corrected.

### The detection path itself is fine

`_startPing` (`mcremote_client.dart:2419`) sends an application `ping` every
`kAppPingPeriod` (10 s, `link_health.dart:52`) with `kAppPingTimeout` (6 s),
and tears the socket down on the **second** consecutive miss. Two misses, not
one, is deliberate — bouncing a live connection on a single lost packet is the
flap MADR 0046 L-1 fixed.

That predicts detection in ~20–26 s, and the measurement agrees: the
black-hole went on at 23:09:06 and the client declared the transport dead at
23:09:31, **25 seconds**. The ping does its job. Nothing below proposes
changing its cadence.

### What actually breaks

A transport failover briefly holds **two** sockets: the new leg authenticates
before the old one is reaped. The daemon resolves that by closing the older
connection with close code 4001 (`kCloseReplaced`, 0068 D3) — visible above as
`reason=replaced` at 23:09:32.

The old socket's close is delivered to `_onSocketDone`
(`mcremote_client.dart:2541`), which is bound as a bare callback:

```dart
_sub = _channel!.stream.listen(
  _onMessage,
  onError: _onSocketError,
  onDone: _onSocketDone,
  cancelOnError: false,
);
```

Nothing in that binding records *which* socket it belongs to. So when the old
leg's `done` fires, the handler runs against whatever is current:

* it reads `_channel?.closeCode` — the **new** channel's, not the closed one's;
* on the 4001 branch it calls `_setState(McConnectionState.disconnected)` and
  `_teardownSocket(suppressReconnect: true)`, and `_teardownSocketImpl`
  (`:2785`) cancels `_pingTimer`, nulls `_sub`, `_channel`, `_httpClient` and
  `_relayTransport` — i.e. it dismantles the **live** relay leg;
* and then it `return`s **without scheduling a reconnect**, by design.

The `return` is correct for the case the branch was written for: 4001 normally
means *another device login replaced us*, so redialling would fight the newer
login. It is wrong when the replacement is our own failover — there we are the
newer login, and tearing ourselves down is self-inflicted.

The state then settles wherever the in-flight `_connectLeg` leaves it. With the
socket gone, the ping cancelled and no reconnect armed, `_scheduleReconnect`
cannot help either: its first guard is
`if (_state == McConnectionState.connected) return;`.

Hence the observed end state — `connected`, no socket, no ping, no timer,
indefinitely.

### The codebase already has the guard, and this path skips it

`_connectEpoch` (`:575`) is incremented on every dial (`:1884`, `:2114`) and on
teardown (`:2750`), and `_staleAttempt(epoch)` (`:577`) is the standard test.
Every comparable asynchronous continuation uses it:

| site | guard |
|---|---|
| periodic ping (`:2446`) | `final pingEpoch = _connectEpoch;` … `if (pingEpoch != _connectEpoch) return;` |
| ping continuation (`:2470`) | `if (pingEpoch != _connectEpoch \|\| _state != connected) return;` |
| health probe (`:2507`) | `final probeEpoch = _connectEpoch;` … `if (probeEpoch != _connectEpoch) return;` |
| pair/connect legs (`:1970`) | `if (_staleAttempt(epoch)) { … }` |
| **`_onSocketDone` / `_onSocketError`** | **none** |

`_teardownSocketImpl` is itself careful about exactly this hazard, and says so:
"A newer attempt may adopt a socket while this close handshake is pending; this
teardown must only ever close the bundle it observed on entry." The care stops
at the function boundary — its *caller* has no idea which socket it is
speaking for.

### Why it went unnoticed

The window only opens when two sockets exist at once, which happens on
transport failover and on a genuine second-device login. Both are rare in
normal use and neither appears in the unit suite: `+1388` tests, none of which
drive a close event for a superseded channel. It survived 0126's debugging pass
and all of 0129 because every earlier on-device run either had one socket or
was restarted by hand before the symptom could be seen.

## Decision Drivers

* The app silently stops delivering the alerts it exists to deliver, and says
  the opposite while doing it.
* It is unrecoverable without a manual relaunch — no timer, no lifecycle event
  and no network change brings it back.
* The failing path is the *recovery* path, so it fires precisely when the link
  is already unreliable.
* The guard needed already exists and is used by every neighbouring call site;
  this is an omission, not a missing capability.
* Detection latency is **not** implicated and must not be traded away to make
  the symptom rarer.

## Considered Options

* **A — Bind socket callbacks to their channel, and make `connected` a
  self-checking state**
* **B — Bind socket callbacks to their channel only**
* **C — Add a supervisor timer that redials whenever the daemon has no
  connection**
* **D — Shorten the ping cadence so the stuck state is re-entered sooner**

## Decision Outcome

Chosen option: **"A — Bind socket callbacks to their channel, and make
`connected` a self-checking state"**, because it fixes the defect at its cause
and adds one cheap invariant that would have surfaced it in seconds rather than
minutes.

The two parts are deliberately separate:

* **D1 — A close or error belongs to the socket that raised it.**
  `_onSocketDone` and `_onSocketError` take the epoch (and the channel) they
  were bound with, and return immediately when it is no longer current. A
  superseded socket may not read the live channel's `closeCode`, may not set
  connection state, and may not call `_teardownSocket`.

* **D2 — 4001 parks only when the replacement is not ours.** The
  deliberate no-reconnect branch stays for its real case (another device
  logged in). Under D1 it stops firing for our own failover, because that close
  arrives on a stale epoch. The branch keeps its comment and its behaviour; it
  simply stops being reached by the wrong event.

* **D3 — `connected` implies a live socket and an armed ping.** The pair is an
  invariant, and the client checks it on the timer it already runs rather than
  on a new one. If the state says `connected` while `_channel` is null or the
  ping timer is not armed, that is a bug wherever it came from: the client
  drops to `error` and schedules a reconnect instead of sitting in it. This is
  the part that makes the class of failure self-limiting — any future variant
  costs one ping period, not a relaunch.

* **D4 — Detection cadence is unchanged.** `kAppPingPeriod` stays 10 s,
  `kAppPingTimeout` stays 6 s, and the two-miss rule stays. The measurement
  says detection already works in 25 s; the eleven minutes were the stuck
  state, not the clock. Changing timings here would trade battery and reconnect
  churn on real networks (0062 D11's fallback budget) for nothing.

### Consequences

* Good, because the app stops being able to reach a state where it is offline,
  silent, and claiming otherwise, with no way back but a relaunch.
* Good, because D3 bounds every future instance of the same class to one ping
  period, including ones nobody has thought of.
* Good, because it needs no new timer, no new state and no new platform call —
  D1 is the guard the neighbouring call sites already use.
* Neutral, because the 4001 park behaviour is unchanged for the case it was
  written for; a genuine second-device login still parks quietly.
* Neutral, because detection latency stays at ~25 s for a silently dead link.
  That is a separate question, and this record deliberately does not answer it.
* Bad, because D3 can mask a future D1-style omission: a stuck state now
  self-heals instead of being reported. Mitigated by recording it — the repair
  path logs, so the invariant firing is visible rather than silent.
* Bad, because the fix lives in the transport core, which every platform and
  every session depends on, so a regression here is broad. Mitigated by the
  tests in the plan, which drive the exact two-socket sequence that no existing
  test covers.

### Confirmation

1. A unit test drives the measured sequence — connect, adopt a second socket,
   deliver a 4001 close for the **first** — and asserts the live channel, its
   subscription and its ping timer all survive. `debugPingArmed` and
   `debugSocketResourcesHeld` (`:1002`) already exist as seams for exactly this.
2. A unit test asserts the 4001 park still works when the close arrives on the
   **current** epoch: state parks, no reconnect is scheduled.
3. A unit test asserts D3: forcing `connected` with no armed ping results in
   `error` plus a scheduled reconnect within one ping period.
4. Each of the three is mutation-tested against a scratchpad copy before being
   trusted, per the "a check is not trusted until it has been seen to fail"
   rule.
5. On device, the measured scenario is re-run: black-hole the mesh path, and
   confirm the client fails over, keeps a live socket, and — when the block is
   removed — is still connected. The 11-minute silence must not reproduce.

## Pros and Cons of the Options

### A — Bind socket callbacks to their channel, and make `connected` self-checking

* Good, because it removes the cause: a stale event can no longer act on live
  state.
* Good, because the invariant catches the whole class, not just this instance.
* Good, because it reuses `_connectEpoch`, already the file's idiom.
* Bad, because it is two changes rather than one, and the second is a
  behaviour change in a hot path.

### B — Bind socket callbacks to their channel only

* Good, because it is the minimal correct fix for the measured defect.
* Good, because it carries no risk of masking anything.
* Bad, because the next omission of the same kind is again unbounded — the app
  would sit stuck until a relaunch, and the only reason we know this one exists
  is an eleven-minute black-hole experiment nobody runs routinely.

### C — Supervisor timer that redials when the daemon has no connection

* Good, because it recovers from any stuck state regardless of cause.
* Bad, because the client cannot see "the daemon has no connection" — that is
  precisely what it is wrong about. It would have to redial on a schedule,
  which is a reconnect storm dressed as a watchdog.
* Bad, because it treats the symptom and leaves a stale socket teardown able to
  dismantle a live leg.

### D — Shorten the ping cadence

* Good, because it needs no structural change.
* Bad, because it does not fix anything: the stuck state has **no armed ping**,
  so a shorter period never fires. It would have changed the eleven minutes
  into eleven minutes.
* Bad, because it spends battery and reconnect budget (0062 D11) against a
  defect it cannot reach.

## More Information

* [0129](0129-MADR-background-alert-delivery-survives-task-removal.md) P6 row 4
  is where this was found; its "OPEN — connection liveness detection latency"
  entry is superseded by this record, and the measurement it asked for is the
  timeline above.
* [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md) D3 defines the
  4001 close this turns on.
* [0063](0063-MADR-connection-liveness-truth.md) D6 is the mesh↔relay failover that
  opens the two-socket window.
* [0062](0062-MADR-phone-transport-selection.md) D11's per-generation
  fallback budget is why D4 refuses to tune timings speculatively.
* [0046](0046-MADR-mobile-debug-pass.md) L-1 is why the ping needs two
  misses, and L-2 is the same stale-teardown hazard caught inside
  `_teardownSocketImpl` — the fix there is the precedent for the fix here.
