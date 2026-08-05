# Hardware validation checklist (outstanding)

<!-- markdownlint-disable MD013 MD024 MD060 -->

The checks that **cannot** be automated, in one place, with the exact commands
for macOS (launchd) and Linux (systemd user units).

**Progress: 2026-08-02** — A1–A4 pass (keepalive detection, all three
transports, unattended recovery). B4, B7, B10 pass (off-mesh QR, mesh-death
failover, forced reconnect from Settings). See "Results" at the end for what
that does and does not establish.

**Software status (0070 P2, 2026-08-05):** 0063 link-liveness *code* is
**Implemented** (`link_health.dart` + suite); Part A rows below are
**hardware verification only**, not open software. Same pattern for 0062 G7
and 0067/0068 F rows.

| Gate | Software | Hardware |
|------|----------|----------|
| **0062 G7** | done | Part B remainder (B12 deferred; blank rows) |
| **0063 Part A** | done | A* verification on device |
| **0067/0068 F1–F6** | done | ⏸ no iPhone |
| **0069 G1 / U8** | done | FDA walkthrough + codesigning identity |
| **0066 E1/E2** | done | ✔ 2026-08-03 |

What is missing is evidence from a real phone against a real host — not
missing phase code.

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
| A1 | `kill -STOP`, mesh, app foregrounded | Leaves "Connected" ≤ 40 s; amber then red/reconnecting | ✅ 2026-08-02 |
| A2 | `kill -STOP`, mesh, app backgrounded 2–3 min first | Same bound. **This is where the keepalive earns its place** — app timers are throttled here | ✅ 2026-08-02 |
| A3 | `kill -STOP` while on **relay** (Settings → Route → Relay → Reconnect now first) | Same bound; exercises the outer-hop keepalive and mcrelay's tolerance of ping frames | ✅ 2026-08-02 |
| A4 | `kill -CONT` after each of the above | Reconnects unattended, no tap required | ✅ 2026-08-02 |
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

> **Changed by 0064 (D6):** the scan-pauses-for-a-choice behaviour these rows
> describe is now **Select mode only**, and the default is **Auto** (scan →
> claim immediately over mesh, relay fallback pre-claim). Before running
> **B1, B2, B3 and B5**, set Settings → **Connect mode → Select**. Token QRs
> never pause in either mode. Part C validates the 0064 behaviours themselves.

| # | Scenario | Expected | Pass |
|---|----------|----------|------|
| B1 | On-mesh, both probes pass → scan QR | Transport menu appears; **nothing is dialled** until Connect | |
| B2 | Same, choose **Mesh** → Connect | Connects over mesh; Settings → Route confirms | |
| B3 | Same, choose **Relay** → Connect | Connects over relay | |
| B4 | Off-mesh (Tailscale off on phone), scan QR with relay | Auto-connects over relay, no menu, **no ~8 s stall** | ✅ 2026-08-02 |
| B5 | Dual-available QR carrying a **pair code** | Button reads "Claim & connect"; code is claimed once, on the chosen transport | |
| B6 | Mesh probe passes but the dial fails (block :7531 on the host) | One automatic hop to relay; status narrates "Mesh failed — trying relay…" | |
| B7 | Kill mesh mid-session (Tailscale off) | Reconnects over relay, not over the dead mesh | ✅ 2026-08-02 |
| B8 | Airplane-mode flap ×5 | No mesh↔relay thrash loop | |
| B9 | Bad/expired token | No transport hop; re-pair guidance shown | |
| B10 | Settings → Reconnect now with Relay forced while sticky is Mesh | Moves onto relay without re-pairing | ✅ 2026-08-02 |
| B11 | Mesh-only pairing (QR with no `relay=`) | Relay is never opened; no transport menu offered | |
| B12 | **Claim over mesh, kill the link *after* the code is sent** | **No relay retry.** Copy says the code may have been used. A fresh code then pairs cleanly | ⏸ deferred 2026-08-02 — see below |
| B13 | Sticky relay + relay route cleared | Hops to mesh rather than stranding | |
| B14 | Mesh flap ×5 | Transport switches stay bounded; session remains usable | |

**B12 is deferred, deliberately.** The latch logic *is* covered:
`dial_episode_test.dart` asserts that a post-claim `host_offline` produces
**zero** relay connections, and that test fails without the latch (verified by
reverting it). What the hardware row would add is the on-screen copy and the
fresh-code recovery — not the safety property itself.

It is deferred because the window is impractical to hit by hand: the claim
frame goes on the wire roughly 500 ms after the tap and the reply lands ~80 ms
later, measured from this host's own logs (`tunnel bridged` → `device paired
via short code` = 237 ms over the relay). Hitting that reliably needs either
scripted input over adb — which is not currently connected — or latency
injection on the return path (`dnctl`/`pfctl` on macOS, `tc netem` on Linux).
Either is a fair amount of setup for a property a passing test already proves.

**Status: logic covered by test; on-screen copy and recovery unverified.**
Revisit if the claim path changes, or when adb/latency injection is available.

Original rationale for why the row matters: Pair codes are one-shot: the
daemon removes the code when the claim arrives and only restores it if the
device-create then fails. If the client retried a claim on another transport it
would meet a permanent `invalid_code`, stranding a user whose token exists on
the host and was never delivered. Everything else here degrades gracefully;
this one loses a pairing.

To produce it: start the claim, then kill the phone's connectivity in the
window after the code goes on the wire (airplane mode the instant you tap
Connect, or `kill -STOP` the daemon at that moment).

---

## Part C — 0064 connect screen (Connect mode, Settings token, burn trail)

Same setup as Part B (both transports up) unless a row says otherwise. The
commands are identical on macOS and Linux — `mcremote` speaks the same CLI on
both; only the service manager differs, and none of these rows touch it.

| # | Scenario | Expected | Pass |
|---|----------|----------|------|
| C1 (V11, Auto) | Connect mode **Auto** (the default), on-mesh, scan a dual-available **code** QR | Claims immediately — status says "claiming over mesh…", no pause, no menu wait; Settings → Route shows mesh | |
| C2 (V11, Select) | Connect mode **Select**, same QR | Pauses with the menu; pick a transport; **Claim & connect** completes the pairing (= B1+B5) | |
| C3 (V11, Auto, off-mesh) | Auto, Tailscale off on the phone, scan the same QR | Claims over the relay with no ~8 s mesh stall (sole-available path, unchanged by mode) | |
| C4 (V10) | On the host: `mcremote pair create --name phone-token`. On the phone: Settings → **Long-lived token**, paste, Save, back, **Connect** | Pairs with the token; no code involved. The Host field must already name the host | |
| C5 (V15) | Burn a code (the B12 window: kill connectivity after the claim is sent), then on the host: `mcremote pair list` | The burn's device row is listed — a registration no phone holds a token for. `mcremote pair revoke <id>` or `mcremote pair prune` clears it | |

Notes:

- **C5 rides the B12 window** and inherits its deferral caveat: hitting the
  gap by hand is impractical (see Part B). When B12 is staged — adb scripting
  or `dnctl`/`tc netem` latency injection — run C5 in the same session; the
  orphan check is one `pair list` after the failure. What C5 adds over B12 is
  the **host-side** fingerprint of the burn: the phone shows the top
  notification ("That pair code has been used…" with the one-tap *Enter code*
  action — 0064 D7), the client log carries
  `pair code spent without token (transport=…, code=…)`, and the host shows
  the orphan.
- **C4's Save is the commit point**: the ✕ in the field only empties it, and
  Cancel abandons the edit. Saving an empty field removes the stored token —
  verify the subtitle flips to "absent".
- C1 deliberately spends a pair code on an unattended claim. If it fails
  mid-claim you have produced C5's condition organically — check `pair list`
  before generating the next code.

---

## Part E — 0066 secure-storage upgrade resilience

These rows close MADR 0066's U5/U6 and gate 0065's phone update stages. E1
needs the first release built after 0066 landed; E2 needs the one after that.

| # | Scenario | Expected | Pass |
|---|----------|----------|------|
| E1 (U5) | Phone paired and working on the current release. Install the first post-0066 release APK **over the top** (no uninstall) | App opens still paired; sessions list loads; no banner, no re-pair | ✔ 2026-08-03 (see note) |
| E2 (U6) | Same, one release later — the exact shape of the 0066 incident | Still paired. **Acceptable failure**: if the platform kills the store again, the app shows the single "Stored credentials were reset" banner; Enter code re-pairs; pinned paths and preferences intact; host shows the orphan row (`pair list`, prune it) | ✔ 2026-08-03 |
| E3 (negative) | Settings → Long-lived token → clear it (Save empty). Kill and relaunch the app | **No** "Stored credentials were reset" banner — a deliberate clear must not read as a platform reset | |

Notes:

- **E1/E2 passed 2026-08-03** on the v0.6.9 → v0.7.0 in-place upgrade:
  app force-closed, upgraded over the top, reopened **still paired with
  the active sessions intact** — the exact scenario that destroyed the
  store on v0.6.6 → v0.6.7. E1's literal pre-0066 → 0066 transition
  (v0.6.7 → v0.6.9) is recorded as subsumed: it ran on a store already
  damaged by recurrence #2 plus the RAM-only-identity bug (0066 incident
  #3), so it exercised the *recovery* path rather than the happy path;
  v0.6.9 → v0.7.0 demonstrated both preserved pairing and preserved
  sessions cleanly. **E2 passing opens 0065's phone-stage gate.**
- The silent-wipe *detection* path (marker outliving the token) cannot be
  simulated on unrooted hardware — there is no way to wipe the app's secure
  store from outside without wiping all app data, which also removes the
  marker. That state machine is covered by the `settings_store_test.dart`
  probe tests and the connect-screen banner widget tests; E-rows validate
  what only hardware can: real upgrades over real Keystore.
- After any re-pair, compare Settings → Client identity against
  `mcremote pair list`'s KEY column — the prefixes must match. A rejected
  key now also leaves `client key rejected` Warn lines in the daemon log
  (0066 D8), which is the first thing to quote if E2 goes wrong.

---

## Part F — 0067 iOS port

These rows close MADR 0067's F1g–F5g. **All parked (`⏸ no device`) until an
iPhone exists** — every P0–P4 behaviour they exercise is implemented and
unit/widget-tested; what is missing is real iOS: the permission prompts,
true suspension, the Keychain surviving an actual delete/reinstall, and the
camera/speech hardware. Requires a paid-or-free developer team on the Mac
([ops-ios-signing.md](ops-ios-signing.md)) and the daemon reachable from
the phone's network.

| # | Scenario | Expected | Pass |
|---|----------|----------|------|
| F1 (F1g) | Fresh install, foreground, first `wss://` dial to the daemon's LAN/mesh address | Local Network prompt appears; the triggering dial fails; the automatic retry (or a second tap) connects. Deny instead: failure copy suggests Settings → Privacy & Security → Local Network | ⏸ no device |
| F2 (F2g) | Connected, background the app 10 min, reopen | Clean reconnect + full session resync in < 3 s on LAN; no stale-timer burst, no phantom "connected" state before the probe verifies (0063) | ⏸ no device |
| F3 (F3g) | Alerts on, app foregrounded on the sessions list, agent in another session asks for permission | Banner with Allow/Deny; either action opens the app (foreground action) and resolves the ask; notification permission flow and cold-launch tap replay also verified here | ⏸ no device |
| F4 (F4g) | Pair, verify, delete the app, reinstall the same build | App starts **unpaired** (stale Keychain credentials detected and cleared — the 0067 D5 inversion); pairing again works; host shows the orphan device row (`pair list`, prune it) | ⏸ no device |
| F5 (F5g) | QR pair via camera; voice input past 60 s; attach a photo (HEIC); repeat F1 with the Tailscale (100.64/10) address | QR scans; speech session ends gracefully at the SFSpeechRecognizer cap (answer 0067 Q3); image arrives with a correct mime (answer Q2); record whether CGNAT dials trigger the Local Network prompt (answer Q1) | ⏸ no device |
| F6 (F6g) | Ten rapid background/foreground cycles against a live **v2** daemon+client pair (0068 G1; both transports; on relay, watch `mcrelay` logs) | No `too many clients` from the daemon (P2 replacement absorbs same-device churn), no `limit`/`rate_limited` from the relay (and none needing the P6 sweep to self-correct — watch for `phone slot divergence corrected`); within-window resumes take the P4 fast path (no history walk, `resumed` in auth logs); session intact after the tenth resume; answers 0067 A1-Q5 (how long a suspended socket lingers server-side — expect ≤60 s via P1 liveness) | ⏸ no device |

Notes:

- F4 is the one Android cannot have: the Keystore dies with the app there.
  If F4 shows a *paired* app after reinstall, the D5 probe regressed.
- F5's Q1 answer feeds back into the MADR's Open questions — record it
  there, not just here.

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

## Results

### Passed 2026-08-02

**A1–A4** — the keepalive detects a silent peer on mesh foregrounded,
mesh backgrounded, and over the relay, and recovers unattended. A2 and A3 are
the load-bearing ones: in the foreground the 10 s application ping usually
notices first, so those two are what actually demonstrate the protocol
keepalive doing work the old build could not do. A3 additionally confirms
mcrelay tolerates ping frames on the phone plane — verified in
`coder/websocket`'s source beforehand, now observed against the real relay.

**B4, B7, B10** — off-mesh QR connects without the ~8 s mesh stall (the
regression the probe-forward exists to prevent); a mesh death reconnects over
the relay rather than retrying the dead path (0063 D6); and Settings can move a
live session onto the relay without re-pairing (0062 D6).

### What this does *not* yet establish

Detection is proven. **Restraint is not.** A6/A7 — the idle negative controls —
are still outstanding, and they are what rule out the opposite failure:
keepalive that closes *healthy* sessions would be worse than the bug it fixes,
and nothing observed so far would have caught it. Until those run, the evidence
says the mechanism fires, not that it only fires when it should.

**A5** is also outstanding, and it is the regression test for plan amendment
B1: a reply longer than the daemon's 60 s read deadline must survive, because
that deadline is reset only by the application ping and a streaming session
sends nothing upstream.

### Remaining

| Gate | Outstanding |
|------|-------------|
| 0063 | A5 (long stream), A6 (idle 15 min foreground), A7 (idle 30 min backgrounded), A8 (out of Wi-Fi range) |
| 0062 **G7** | B1, B2, B3, B5, B6, B8, B9, B11, **B12**, B13, B14 — B1/B2/B3/B5 now need Connect mode = Select first (0064) |
| 0064 | C1–C4; C5 deferred with B12 |
| 0066 | E3 (deliberate-clear negative) only — E1/E2 passed 2026-08-03, 0065's phone-stage gate is **open** |

**B12 remains the highest-value untested row** — a claim killed after the code
is on the wire must not be retried on another transport, or the user loses a
pairing whose token exists on the host.

When Part A is complete, 0063's gate closes; when Part B is complete, 0062
**G7** closes. Update the Status line in each MADR accordingly.
