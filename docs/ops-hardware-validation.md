# Hardware validation checklist (outstanding)

<!-- markdownlint-disable MD013 MD024 MD060 -->

The checks that **cannot** be automated, in one place, with the exact commands
for macOS (launchd) and Linux (systemd user units).

Two gates are open:

| Gate | Source | What it covers |
|------|--------|----------------|
| **0062 G7** | [0062-PLAN §6.4](0062-PLAN-phone-transport-selection.md) | Transport selection: menu, failover, pair-code safety |
| **0063 hardware** | [0063-PLAN §4.4](0063-PLAN-connection-liveness-implementation.md) | Link liveness: does the status light tell the truth |

Both code changes are implemented and unit-tested. What is missing is evidence
from a real phone against a real host.

## Why these cannot be automated

Every fake in the suite closes its socket politely. The failures that matter
here do not:

- A **blackholed path** drops packets without RST or FIN. Writes still succeed
  into the local send buffer, so nothing below the heartbeat notices.
- A **half-up tailnet** answers a TCP handshake while the daemon is
  unreachable, so a probe passes and the dial still fails.
- A **one-shot pair code** can be consumed host-side by a claim the phone never
  sees the answer to.

The test suite covers the logic around all three. Only hardware covers the
three themselves.

---

## Setup

### 1. Build and install the phone app

```bash
make apk
# → apps/mobile/build/app/outputs/flutter-apk/app-release.apk
adb install -r apps/mobile/build/app/outputs/flutter-apk/app-release.apk
```

Or install the APK from the latest GitHub release. **Check the version**: the
relay path only works from **v0.6.2**, and QR pairing with both transports up
only works from **v0.6.3**.

### 2. Confirm the daemon is up and the relay is registered

```bash
# macOS
tail -20 ~/Library/Logs/mcremote/mcremote.err.log | grep -E "listening|registered with mcrelay"

# Linux
journalctl --user -u mcremote -n 50 --no-pager | grep -E "listening|registered with mcrelay"
```

You want both lines. `listening` without `registered with mcrelay` means the
relay half will fail every test in Part B.

### 3. Know which transport is live

**Settings → Route** names it: *"Connected over Mesh"* / *"Connected over
Relay"*. This is read from the live socket, not from configuration, so it is
the authoritative answer. The sessions banner says the same thing.

---

## Platform reference

|  | macOS (launchd) | Linux (systemd --user) |
|--|-----------------|------------------------|
| Main PID | `pgrep -f "mcremote serve"` | `systemctl --user show -p MainPID --value mcremote` |
| Logs (live) | `tail -f ~/Library/Logs/mcremote/mcremote.err.log` | `journalctl --user -u mcremote -f` |
| Logs (search) | `grep … ~/Library/Logs/mcremote/mcremote.err.log` | `journalctl --user -u mcremote --no-pager \| grep …` |
| Stop the service | `launchctl bootout gui/$(id -u)/com.magiccliremote.mcremote` | `systemctl --user stop mcremote` |
| Start the service | `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` | `systemctl --user start mcremote` |
| Restart policy | `KeepAlive=true` | `Restart=always` |
| Config | `~/.config/mcremote/config.yaml` | `~/.config/mcremote/config.yaml` |

### The pitfall that silently invalidates the liveness tests

Part A needs **TCP alive, application silent**. How you stop the daemon decides
whether you are testing that or something else entirely:

| Method | What actually happens | Valid? |
|--------|----------------------|--------|
| `kill -STOP <pid>` | Process suspended. Kernel keeps ACKing; nothing ever pongs. | ✅ **This is the test** |
| `kill -9` / `kill` | Socket closes cleanly (FIN/RST), and the service manager restarts it | ❌ Tests the ordinary error path |
| `launchctl bootout` / `systemctl --user stop` | Clean close **and** the service stays down | ❌ Same, plus nothing to reconnect to |

Neither `KeepAlive` nor `Restart=always` reacts to `SIGSTOP` — both watch for
the process *exiting*, and a stopped process has not exited. Verified on macOS:
the PID is unchanged and the process sits in state `T` until `kill -CONT`.

On Linux, signal the **main PID directly** rather than
`systemctl --user kill -s STOP`, which signals the whole control group
(`KillMode=control-group`) and would suspend the agent engines too.

---

## Part A — 0063 link liveness

The claim under test: **green means verified recently**, and a peer that stops
answering gets its socket closed rather than leaving the client on a connection
that no longer exists.

### The core test

```bash
# macOS
PID=$(pgrep -f "mcremote serve")
# Linux
PID=$(systemctl --user show -p MainPID --value mcremote)

kill -STOP $PID     # start the stopwatch
# …watch the phone…
kill -CONT $PID     # revive
```

`kill -STOP` is not a poor substitute for a network blackhole — from the
client's side it produces exactly "writes succeed, nothing comes back", which
is the condition the keepalive targets, and it is deterministic, needs no
sudo, and disturbs nothing else on the network.

| # | Scenario | Expected | Pass |
|---|----------|----------|------|
| A1 | `kill -STOP`, mesh, app foregrounded | Leaves "Connected" ≤ 40 s; amber then red/reconnecting | |
| A2 | `kill -STOP`, mesh, app backgrounded 2–3 min first | Same bound. **This is where the keepalive earns its place** — app timers are throttled here | |
| A3 | `kill -STOP` while on **relay** (Settings → Route → Relay → Reconnect now first) | Same bound; exercises the outer-hop keepalive and mcrelay's tolerance of ping frames | |
| A4 | `kill -CONT` after each of the above | Reconnects unattended, no tap required | |
| A5 | Agent reply longer than 60 s, no user input | Session survives to the end | |
| A6 | Idle 15 min, foregrounded | Zero disconnects | |
| A7 | Idle 30 min, backgrounded | Zero disconnects | |
| A8 | Walk out of Wi-Fi range (cellular off) | Leaves "Connected"; recovers on return | |

**A5 is the regression test for plan amendment B1.** The daemon's read deadline
is only reset by the application `ping`, never by protocol keepalive frames, and
a streaming session sends nothing upstream. If the ping were ever made
conditional on the link "looking healthy", a long reply would be dropped
mid-answer — presenting as the agent hanging.

**A6/A7 are the negative control and matter as much as A1–A3.** Keepalive that
kills healthy sessions would be worse than the bug it fixes. Count disconnects
before and after:

```bash
# macOS
grep -c "ws client disconnected" ~/Library/Logs/mcremote/mcremote.err.log
# Linux
journalctl --user -u mcremote --no-pager | grep -c "ws client disconnected"
```

Any increase during an idle period is a regression.

### Reading A1–A3 correctly

Both detectors are in this build. In the **foreground** the 10 s application
ping usually notices first, so a fast foreground result does not prove the
protocol keepalive fired. **A2 and A3 are where it is doing the work.**

---

## Part B — 0062 transport selection (G7)

Requires both transports usable: on the tailnet, with a relay paired for this
host. Confirm in Settings → Route that both probe chips read "up".

| # | Scenario | Expected | Pass |
|---|----------|----------|------|
| B1 | On-mesh, both probes pass → scan QR | Transport menu appears; **nothing is dialled** until Connect | |
| B2 | Same, choose **Mesh** → Connect | Connects over mesh; Settings → Route confirms | |
| B3 | Same, choose **Relay** → Connect | Connects over relay | |
| B4 | Off-mesh (Tailscale off on phone), scan QR with relay | Auto-connects over relay, no menu, **no ~8 s stall** | |
| B5 | Dual-available QR carrying a **pair code** | Button reads "Claim & connect"; code is claimed once, on the chosen transport | |
| B6 | Mesh probe passes but the dial fails (block :7531 on the host) | One automatic hop to relay; status narrates "Mesh failed — trying relay…" | |
| B7 | Kill mesh mid-session (Tailscale off) | Reconnects over relay, not over the dead mesh | |
| B8 | Airplane-mode flap ×5 | No mesh↔relay thrash loop | |
| B9 | Bad/expired token | No transport hop; re-pair guidance shown | |
| B10 | Settings → Reconnect now with Relay forced while sticky is Mesh | Moves onto relay without re-pairing | |
| B11 | Mesh-only pairing (QR with no `relay=`) | Relay is never opened; no transport menu offered | |
| B12 | **Claim over mesh, kill the link *after* the code is sent** | **No relay retry.** Copy says the code may have been used. A fresh code then pairs cleanly | |
| B13 | Sticky relay + relay route cleared | Hops to mesh rather than stranding | |
| B14 | Mesh flap ×5 | Transport switches stay bounded; session remains usable | |

**B12 is the highest-value row in this document.** Pair codes are one-shot: the
daemon removes the code when the claim arrives and only restores it if the
device-create then fails. If the client retried a claim on another transport it
would meet a permanent `invalid_code`, stranding a user whose token exists on
the host and was never delivered. Everything else here degrades gracefully;
this one loses a pairing.

To produce it: start the claim, then kill the phone's connectivity in the
window after the code goes on the wire (airplane mode the instant you tap
Connect, or `kill -STOP` the daemon at that moment).

---

## Triage when something fails

| Symptom | Look at |
|---------|---------|
| Relay never connects | `registered with mcrelay` in the daemon log; `curl -s https://<relay>/healthz` from the phone's network |
| "host not available" | mcrelay is up but the daemon is not registered — check for `relay registration ended` churn in the daemon log |
| Both transports dead after an install | The install may have left the service down. `launchctl print gui/$(id -u)/com.magiccliremote.mcremote` / `systemctl --user status mcremote` |
| Status light disagrees with reality | Capture the daemon log around the moment, plus which transport Settings → Route named |

Daemon log lines worth knowing:

- `listening addr=… tls=…` — the mesh listener is up
- `registered with mcrelay host_id=…` — the relay path is usable
- `tunnel bridged to local listener` — a phone joined through the relay
- `ws client disconnected reason=read_deadline` — the daemon gave up on a
  silent client at 60 s
- `device authenticated` — a session actually completed the handshake

## Recording results

Fill in the Pass columns and note the observed timing for A1–A3. When Part A is
complete, 0063's gate closes; when Part B is complete, 0062 **G7** closes.
Update the Status line in each MADR accordingly.
