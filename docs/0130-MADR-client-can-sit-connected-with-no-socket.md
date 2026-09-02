---
status: proposed
date: 2026-09-02
decision-makers: maccavelli
consulted: —
informed: —
---

# The client can sit `connected` with no socket, and the cause is not yet known

> **Amended 2026-09-02, before any code shipped.** The first draft of this
> record named a cause — a superseded socket's close event dismantling the live
> leg — and proposed a fix for it. **That mechanism is falsified**; the section
> "The mechanism this record first proposed, and why it is wrong" below records
> it in full rather than deleting it, because the way it was ruled out is the
> most useful thing here for whoever picks this up. The decision is now to
> *find* the cause before changing the transport core.

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
connection. Seven of those eleven minutes were on a healthy network, so this is
not a link problem — the client had stopped trying and did not know it.

Liveness above is a running count over the daemon log (`device authenticated`
+1, `ws client disconnected` −1), **not** `lsof`, which in the same session
reported the same fd and source port as `ESTABLISHED` across an hour of
connects and straight through a window the daemon logged as disconnected.

## What is established

* **The end state was reached.** State `connected`, no connection daemon-side,
  for eleven minutes, ending only in a manual relaunch. Measured, above.
* **That state cannot recover itself.** `_scheduleReconnect`
  (`mcremote_client.dart:2597`) begins
  `if (_state == McConnectionState.connected) return;`, so no backoff rung ever
  runs while the client believes it is connected. Read in code.
* **Detection of a dead link is not implicated.** `_startPing` (`:2419`) sends
  an application ping every `kAppPingPeriod` (10 s) with `kAppPingTimeout`
  (6 s) and tears down on the second consecutive miss — two, not one,
  deliberately (0046 L-1). That predicts ~20–26 s, and the black-hole measured
  **25 s**. The ping works; nothing here proposes changing its cadence.
* **`_onSocketDone` (`:2541`) and `_onSocketError` (`:2529`) carry no epoch
  guard**, while every comparable asynchronous continuation in the file does —
  the periodic ping (`:2446`), its continuation (`:2470`), the health probe
  (`:2507`) and the dial legs via `_staleAttempt` (`:577`). Read in code. This
  is a real inconsistency. It is **not** established that it causes anything.

## The mechanism this record first proposed, and why it is wrong

**Proposed:** a transport failover briefly holds two sockets; the daemon closes
the older with 4001; that close is delivered to the unguarded `_onSocketDone`,
which reads the *live* channel's `closeCode`, tears the live bundle down, and
returns without scheduling a reconnect.

**Why it cannot happen.** The story needs a superseded socket whose
subscription is still live. There is none:

* `_connectInternal` calls `_teardownSocket(suppressReconnect: true)` before it
  dials;
* `_connectLeg` calls it again at the top of **every** leg — its comment says
  so outright: *"A second leg inherits the first leg's wreckage; start from a
  clean slate."*;
* `_teardownSocketImpl` (`:2785`) does `await sub?.cancel()`.

A cancelled subscription delivers no `done`. So on both the second-dial path
and the intra-episode leg-fallback path, the old socket is unsubscribed before
the new one is adopted, and its close never reaches a handler at all.

**How it was ruled out.** Two candidate fixes were written and then
mutation-tested against a scratchpad copy: the epoch guard removed from
`_onSocketDone`, and both invariant guards disabled. **Every test still
passed** in both mutations — including the two written specifically to catch
them. Tests that cannot fail prove nothing, so rather than ship them, the
premise was re-read, and it did not survive. The work is preserved at
`scratchpad/0130-hardening-unproven.patch`; it is defensible hardening and
carries no demonstrated behaviour change.

The lesson worth keeping: the mutation test is what caught this. Both changes
looked right, the suite was green at `+1390`, and the reasoning in the first
draft read plausibly. Only deliberately breaking the code revealed that nothing
was checking it.

## Decision Drivers

* The failure is severe and user-invisible: no alerts arrive, and the one
  surface that would say so says the opposite.
* It is unrecoverable without a relaunch, so a user has no route back that they
  would think to take.
* The transport core is shared by every platform and every session; a
  speculative change there is a broad risk taken for an unproven benefit.
* One plausible mechanism has already been wrong. Confidence from reading the
  code is now demonstrably not enough.
* The scenario is reproducible on demand (the black-hole), so evidence is
  cheap to obtain — there is no reason to guess.

## Considered Options

* **A — Instrument, reproduce, and read the actual sequence before changing
  anything**
* **B — Ship the unproven hardening now and investigate afterwards**
* **C — Record the finding and stop**

## Decision Outcome

Chosen option: **"A — Instrument, reproduce, and read the actual sequence
before changing anything"**, because the one mechanism confidently derived from
reading this code turned out to be impossible, and the scenario can be
reproduced on demand for the cost of two `iptables` rules.

* **D1 — No transport-core change lands without a failing test or an observed
  sequence behind it.** The hardening in
  `scratchpad/0130-hardening-unproven.patch` stays out of the tree until it can
  be tied to something real. It is not discarded — it may well be right — but
  "it looks like it should help" is what produced the falsified draft.
* **D2 — The instrument is a debug-only trace of the four values that
  disagreed.** State, `_channel` presence, ping-armed and `_connectEpoch`, each
  transition timestamped. The eleven minutes are silent in the current logs:
  `mcremote/fgs` and the daemon both say plenty, and the client says nothing
  between 23:09:33 and the relaunch. Whatever happened, no existing log line
  covers it.
* **D3 — Reproduce with the same instrument, not a similar one.** The same
  black-hole against the same AVD, with liveness read from the daemon's running
  count. Anything else risks measuring a different failure.
* **D4 — Detection cadence stays untouched.** `kAppPingPeriod`,
  `kAppPingTimeout` and the two-miss rule are unchanged. 25 s is the documented
  trade (0046 L-1) and the measurement matches it; the eleven minutes were not
  the clock.

### Consequences

* Good, because the next change to the transport core will be aimed at a
  sequence someone has actually seen, rather than at the most plausible story.
* Good, because the trace is useful beyond this bug: the state/socket/ping/epoch
  quartet is exactly what every future connection-liveness question needs.
* Neutral, because the failure remains possible until the cause is found. It
  has been possible for some time and is rare enough that no user has reported
  it.
* Bad, because it costs another device session before anything is fixed, and
  the reproduction depends on an emulator whose transport was already flaky
  that evening.
* Bad, because a debug-only trace risks becoming permanent noise. Bounded by
  keeping it behind `kDebugMode` and removing it in the same change that fixes
  the cause.

### Confirmation

The investigation is confirmed when the log shows, for the eleven-minute
window, **which** of these happened:

1. `connected` was entered while `_channel` was null — the state machine lied
   at a transition; or
2. `_channel` went null while the state stayed `connected` — a teardown left an
   incoherent pair; or
3. the ping timer was cancelled while the state stayed `connected` — the
   detector was disarmed; or
4. none of these, and the client held a socket it believed live while the
   daemon had already dropped it — a half-open TCP the ping should have caught,
   which would point at the ping path rather than the state machine.

Each implies a different fix, and the four are distinguishable from the trace
alone. The record is amended with the answer before any fix is designed.

## Pros and Cons of the Options

### A — Instrument, reproduce, read

* Good, because it replaces a falsified guess with an observation.
* Good, because the reproduction is deterministic and cheap.
* Bad, because it defers the fix by one session.

### B — Ship the unproven hardening now, investigate afterwards

* Good, because both changes are small and defensible in isolation, and one of
  them (`connected` implies a live socket) plausibly bounds the symptom
  whatever causes it.
* Bad, because neither has a test that can fail, so if it fixes the outage
  nothing proves it, and if it does not, the record implies it was handled.
* Bad, because the teardown-coerces-state change sits in a path every session
  crosses constantly, for a benefit no evidence supports.

### C — Record and stop

* Good, because it overstates nothing and costs nothing.
* Bad, because a reproducible, unrecoverable, silent outage stays open with no
  owner, and the reproduction recipe goes cold.

## More Information

* [0129](0129-MADR-background-alert-delivery-survives-task-removal.md) P6 row 4
  is where this was found; its "OPEN — connection liveness detection latency"
  entry is superseded by this record.
* [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md) D3 defines the
  4001 close.
* [0063](0063-MADR-connection-liveness-truth.md) owns liveness truth and is
  where a ping or deadline change would belong.
* [0062](0062-MADR-phone-transport-selection.md) D11's per-generation fallback
  budget is why D4 refuses to tune timings speculatively.
* [0046](0046-MADR-mobile-debug-pass.md) L-1 is why the ping needs two misses;
  L-2 is the stale-teardown hazard already handled inside
  `_teardownSocketImpl`, and reading it is what made the falsified mechanism
  look plausible.
