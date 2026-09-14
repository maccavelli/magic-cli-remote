---
status: proposed
date: 2026-08-15
decision-makers: [Project Owner]
consulted: [Implementer]
informed: [Operators of 8 GiB mesh hosts, Android phone clients]
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Keep long-running agent work alive across screen lock, transport blips, and host pressure

## Context and Problem Statement

Overnight on 2026-08-15 the Android phone left a **kilo** session running a
long host-side job (release-build / `flutter test` class work, implemented
as kilo `background_process` children). In the morning the operator found:

1. the phone had **failed over to mcrelay even though the Tailscale mesh
   was still up**;
2. the **kilo session was closed** on the phone;
3. the **kilo task was gone**.

This is not a kilo-only defect. The same pipes carry grok, goose,
opencode, and codex: one Android WebSocket, one DialEpisode failover
policy, one systemd user cgroup, one in-memory live-session map that
`CloseAll` empties on every daemon death.

The architectural question is: **what must stay alive when the phone
screen locks and a long agent turn is in progress — the socket, the
session object, the engine, the user's job — and what is allowed to
move (mesh → relay, live → durable, engine restart)?**

Scope: Android keep-alive (`apps/mobile` FGS / lifecycle / transport
selection), daemon session restore (`internal/session`, `internal/daemon`),
systemd unit and engine cgroup isolation (`internal/cli/service`),
provider-neutral work-item survival (`internal/provider/httpagent` and
ACP children), and the existing per-provider `prewarm` knob (config +
phone Settings). Does **not** reopen 0062's user transport menu, 0019's
"one engine per daemon" ownership of `* serve`, or 0084 D7's
`remoteMessaging` FGS type. Does **not** rename the YAML key to
`pre-warm` (hyphen): the shipped key is `prewarm`.

This revision (same day) grounds process-memory choices in official
Go / Dart / Flutter documentation (§Research) and locks **D5/D7**:
every agent defaults `prewarm: false`, and the phone can flip that
per agent from Settings → Providers → that agent.

Related: [0062](0062-MADR-phone-transport-selection.md) (DialEpisode
failover), [0063](0063-MADR-connection-liveness-truth.md) (app-ping /
read deadline), [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md)
(resume window), [0019](0019-MADR-opencode-process-management-plan.md)
(KillMode=control-group), [0084](0084-MADR-android-app-hardening-and-performance.md)
(wake lock removed), [0072](0072-MADR-phone-reconnect-and-provider-timeout-incident.md)
(prior reconnect incident), [0075](0075-MADR-kilo-cli-provider.md) /
[0088](0088-MADR-kilo-7.4.22-surface-parity.md) (kilo dialect; `background_process`
is an engine tool, not a mcremote object).

## Decision Drivers

* A phone with a **live, working session** must keep the host link across
  screen lock / Doze without the user unlocking the device.
* Failover to relay is a **recovery hop**, not a new sticky home, when
  the mesh is still reachable.
* A long agent job (release build, test suite, `bash` that runs for
  minutes) must **not die because the daemon or the phone blinked**.
* Fixes must apply to **every supported agent**, with kilo as the
  incident example — not a kilo-only special case.
* 8 GiB / no-swap mesh hosts are in the supported set (this host;
  `AGENTS.md` already records 4 CPUs / 7.8 GiB).
* 0019 still holds: a leaked `kilo serve` / `opencode serve` after
  daemon death is worse than a clean stop. What we isolate is the
  **user's job**, not the engine.
* Battery cost of a wake lock is acceptable **while a session is
  running**; it is not acceptable for a parked idle socket.
* An 8 GiB host must not boot kilo + opencode + grok engines before
  anyone opens a session. Idle RSS last night was already
  hundreds of MiB per Bun engine.
* The operator must be able to flip that from the phone, without SSH
  and without rewriting the whole `config.yaml`.
* Official Go docs: `GOMEMLIMIT` / `debug.SetMemoryLimit` bound **only
  Go-managed memory in that process**. They do not see Bun / Node /
  Dart / C children. They must not be the primary control for this
  incident.

## Considered Options

* Option 1: Layered stability contract (connection ≠ session ≠ job)
* Option 2: Always-on Android wake lock only
* Option 3: Host memory / cgroup limits only
* Option 4: Park the socket on Android screen lock (iOS 0067 D2)
* Option 5: `GOMEMLIMIT` / `debug.SetMemoryLimit` on `mcremote` only

## Decision Outcome

Chosen option: **"Layered stability contract (connection ≠ session ≠ job)"**,
because last night's failure was three independent layers collapsing into
each other: Doze froze the app-ping so a healthy mesh looked dead; the
phone then **preferred relay** and **stuck to it**; the host then
OOM-killed the **entire mcremote cgroup**, which SIGTERM'd kilo *and*
the release-build children; on restart the live session map was empty
and the phone saw `session_not_live` until a manual resume.

* Companion Implementation Plan: [0089-PLAN-long-running-session-stability.md](./0089-PLAN-long-running-session-stability.md)

Locked decisions:

| ID | Decision |
| --- | --- |
| **D1** | **Conditional keep-alive on Android.** While the socket is `connected` *or* any session status is `running`, the foreground service holds a partial wake lock (and may hold a wifi lock). When the socket is parked / idle-disconnected, `allowWakeLock` stays **false** (0084 D3's intent, narrowed). Re-declare `WAKE_LOCK` in the manifest. This **amends 0084 D3**. |
| **D2** | **Doze / read-deadline is not mesh death.** A host `reason=read_deadline` or two app-ping misses after the app has been `hidden`/`paused` is classified as a **client liveness stall**. The next DialEpisode **retries the same transport first**. Failover to the other transport is allowed only if a mesh reachability probe fails, or the same-transport retry fails with a hop-worthy code. A stall-recovery hop **must not** update sticky transport. This **amends 0062 D6 / 0063 D6** for the screen-lock case only. |
| **D3** | **Daemon boot rehydrates durable sessions that were live.** `CloseAll` on shutdown still marks them `disconnected` (0019 teardown stays). On the next `serve`, the manager **resumes** every durable row whose last status was `running` or `idle` (not purged, not `disconnected` for longer than a grace window), attaching the persisted `agent_session_id`. The phone must not have to tap Resume to see a session that was working when the host died. A `daemon_restarted` / `turn_interrupted` notice is emitted on the transcript. |
| **D4** | **User jobs leave the daemon's kill/OOM blast radius.** Engines remain daemon-owned (0019). `background_process` / long `bash` children are started in a **sibling systemd scope** (or equivalent process-group + `MemoryAccounting` slice) so `KillMode=control-group` and a unit `oom-kill` do **not** SIGTERM the gradle/flutter/javac tree. Ship `MemoryAccounting=yes`, `MemoryHigh` (soft), and a documented `MemoryMax` default that can be raised. Apply to kilo, opencode, grok, goose, and codex children — not a kilo special case. |
| **D5** | **`prewarm` exists, defaults `false` on every agent, YAML key stays `prewarm`.** Do not invent `pre-warm`. Code defaults today: grok `true` (Phase 4.2), opencode `true` (0019), kilo `true` (0075), goose `false`, codex `false`. Flip grok / opencode / kilo to **`false`**. `config.Defaults()`, Viper `SetDefault`, `defaults_mcremote.yaml`, `configs/config*.yaml`, `docs/config.md`, README, and the tests that assert "should default true" all change together. An **existing** host file that already writes `prewarm: true` (this host's `~/.config/mcremote/config.yaml`) is **not** rewritten by `serve` or `setup-service`. Changing the live value is D7. Document `default_cwd`: a kilo/opencode session at `$HOME` indexes the entire home tree. **Amends** 0012 §4.2, 0019 §4.2, 0075 (kilo prewarm-on). |
| **D6** | **Surface interruption honestly, for every provider.** `session_not_live` during an in-flight chat is recovered by an automatic resume-from-durable (same id + `agent_session_id`) when the host is reachable; the toast becomes "Host restarted — session reattached; in-flight turn was interrupted", not "start again from Sessions." Engine `background_process` (and ACP long tools) are published as work-items so the phone can show a running job without decoding a raw tool card. |
| **D7** | **Phone toggle on each agent's Settings page writes `providers.<id>.prewarm` and applies it live.** Surface: Settings → Providers → `<agent>` → **Pre-warm engine** switch (same screen as Default mode / credentials — `ProviderDetailScreen`). Wire: additive `prewarm: bool` on `providers.list_result` entries; new request `providers.set_prewarm` `{provider_id, prewarm}` → `ok` / `error`. Persist by a **surgical YAML node write** of that one key (preserve comments and sibling keys; never `viper.WriteConfig` the whole file — that would dump the relay secret and strip comments). Apply: `true` → `EnsureServer()` now; `false` → `Shutdown()` **only if that provider has zero live sessions**, otherwise set a "stop when idle" latch and do not kill a running turn. Push `provider.auth_status`-shaped or a dedicated `providers.prewarm` event so every connected phone refreshes the switch. Old phones ignore the new field; old daemons reject the new request with `unsupported`. |

## Consequences

* Good, because a screen lock no longer converts a healthy mesh into a
  relay-sticky session.
* Good, because an OOM or `systemctl restart` no longer silently
  evaporates the live session *and* the user's compile.
* Good, because every agent inherits the same keep-alive, restore, and
  job-isolation contract.
* Bad, because a wake lock while a turn is running costs battery; the
  FGS notification must stay accurate so Android 15 does not treat it
  as a stale service.
* Bad, because sibling scopes for jobs add process-management surface
  (must still be reaped on session close / user cancel).
* Bad, because boot-time resume of every durable idle session can
  start engines even with D5's default-off — D3 must start an engine
  only for a rehydrated row of **that** provider, never call
  `EnsureServer` for the others.
* Bad, because first session after a cold start pays the Bun / ACP
  cold-start (documented 3–5 s for opencode; ~500 ms for codex).
  That is the intended trade for not holding 400–650 MiB idle.
* Neutral, because 0019's "no leaked engine" invariant is unchanged;
  only job children move. `prewarm: false` still starts the engine
  on first `session.create`.

## Pros and Cons of the Options

### Option 1: Layered stability contract (Chosen Option)

Treat the phone socket, the mcremote session object, the agent engine,
and the user's OS job as four lifetimes with explicit coupling rules
(D1–D7).

* Good, because it matches the incident: each layer failed for a
  different reason and the current code couples them accidentally.
* Good, because kilo is the example, not the only beneficiary — grok
  ACP children and opencode `bash` hit the same cgroup.
* Good, because it preserves 0019 (engines die with the daemon) and
  0062 (one automatic hop) while correcting their over-broad readings.
* Bad, because it is several coordinated changes, not one flag.
* Neutral, because operators can still force-relay or disable
  notifications.

### Option 2: Always-on Android wake lock only

Re-enable `allowWakeLock: true` unconditionally on the FGS.

* Good, because it is a ten-line change and would have stopped the
  04:44 `read_deadline` on many devices.
* Bad, because it does not explain or fix the **closed session** or
  **killed task** — those were host OOM + `CloseAll`.
* Bad, because it burns battery on a parked idle connection, which is
  exactly why 0084 D3 removed the permission.
* Bad, because Samsung App Sleep can still freeze a process that has a
  wake lock but no battery-optimization exemption.

### Option 3: Host memory / cgroup limits only

Add swap, `MemoryMax`, and stop prewarming kilo+opencode+grok.

* Good, because it attacks the actual kill (OOM of a 6+ GiB cgroup on
  a 7.8 GiB / 0-swap host).
* Bad, because the phone would still fail over to relay on Doze
  `read_deadline` and stick there with the mesh up.
* Bad, because a `systemctl restart` (four of them last night before
  the OOM) still `CloseAll`s live sessions and SIGTERMs job children
  under `KillMode=control-group`.

### Option 4: Park the socket on Android screen lock (iOS 0067 D2)

On `onPause`/`onHide`, `disconnect(manual: false)` like iOS.

* Good, because it is already written (`shouldParkOnBackground`) and
  avoids a frozen Dart timer lying about liveness.
* Bad, because it repeals the product's Android headline: "walk away
  and get pinged." Approvals during a long turn would wait until
  unlock.
* Bad, because the host job still dies on OOM/restart; parking the
  phone does not protect it.

### Option 5: `GOMEMLIMIT` / `debug.SetMemoryLimit` on `mcremote` only

Set a soft Go-heap cap on the daemon process.

* Good, because it is one environment variable, officially supported
  since Go 1.19 ([`runtime/debug.SetMemoryLimit`](https://pkg.go.dev/runtime/debug#SetMemoryLimit),
  [GC guide — Memory limit](https://go.dev/doc/gc-guide#Memory_limit)).
* Bad, because the official definition of the limit is
  `MemStats.Sys - HeapReleased` **in this Go process**. It "does not
  account for … memory managed by non-Go code inside the same process"
  and does not account for **child processes at all**. Last night's
  RSS was kilo 652 MiB + opencode 418 MiB + flutter frontend 624 MiB +
  grok ×2; mcremote itself was **39 MiB**. A GOMEMLIMIT on `mcremote`
  would have been a no-op.
* Bad, because the same official guide's **Don't** list matches this
  host: "Don't use the memory limit when … the Go program might share
  some of its limited memory with other programs" and "Don't set a
  memory limit to avoid out-of-memory conditions when a program is
  already close to its environment's memory limits" — that replaces
  OOM with GC thrashing (50% CPU cap, then it still exceeds).
* Neutral, because a future optional `GOMEMLIMIT` on a **Go** child
  (grok) is allowed as a secondary knob; it is not this MADR's
  primary control.

## Confirmation

The decision is accepted when all of the following are true:

1. **Screen-lock soak (Android).** With a live kilo (or grok/opencode)
   turn running and the screen locked for ≥ 10 minutes, the host log
   shows **no** `reason=read_deadline` for that device, the FGS
   notification still reads "Connected", and `activeTransport` remains
   mesh if the mesh probe still passes. Automated stand-in:
   `lifecycle_policy_test` + a new DialEpisode test that a
   `read_deadline` after `hidden` does **not** flip `_diedOnTransport`.
2. **Mesh-up / relay-not-sticky.** After a forced stall-recovery hop
   to relay, the next successful mesh dial restores mesh and does
   **not** persist relay as sticky. Test in `dial_episode_test.dart`.
3. **Host restart rehydrate.** Kill `-TERM` the user unit during a
   live session; after `Restart=always`, `sessions.list` shows that
   id `live=true` **without** a phone Resume tap, and the transcript
   contains a `turn_interrupted` (or equivalent notice). Test in
   `manager_durable_test.go` / a new boot-restore test.
4. **Job survival.** A kilo (or opencode) `background_process` /
   long `bash` whose cgroup is a sibling scope is still running after
   `systemctl --user restart mcremote`. The rehydrated session
   reattaches or at least reports the still-running pid. A unit
   `oom-kill` of `mcremote.service` does not list the job in
   `systemctl status` as a killed member.
5. **Memory budget.** On this class of host, `memory.peak` of
   `mcremote.service` stays under `MemoryHigh` during a `flutter test`
   spawned by an agent; `MemoryMax` if set rejects a runaway engine
   without taking down the user's job scope. `setup-service` writes
   the accounting keys.
6. **Cross-provider.** The same D1–D4 paths are covered by unit tests
   that do not name kilo: httpagent (kilo+opencode) and at least one
   ACP provider (grok or goose) for job-scope + boot-restore.
7. **Honesty.** Chat no longer maps `session_not_live` during an
   in-flight turn to "start again from Sessions" when auto-resume
   succeeds (`mc_exception_test.dart` + chat recovery test).
8. **Prewarm defaults.** `config.Defaults()` reports `Prewarm==false`
   for grok, goose, opencode, codex, and kilo. `TestDefaultsGrokPrewarm`
   and the opencode/kilo "should default true" tests are inverted.
   A config file that **omits** the key boots **no** engine (asserted
   by a daemon unit test that spies `EnsureServer`).
9. **Phone toggle.** Provider detail shows a switch labelled
   "Pre-warm engine" for every ready agent. Toggling it persists
   `providers.<id>.prewarm` (surgical YAML write; sibling keys and
   comments survive) and either starts or latches-stop the engine.
   `providers.list_result` includes `prewarm`. An old daemon without
   the RPC shows the switch disabled with "Host does not support
   pre-warm control".
10. **No live-session kill.** `providers.set_prewarm` `{prewarm:false}`
    while that provider has a live session returns `ok` with
    `engine: "stopping_when_idle"` and leaves the engine up until the
    last session of that provider closes.

Ops (not gated in CI): add a userspace swap file on 8 GiB / 0-swap
hosts; disable Samsung "Put unused apps to sleep" for Magic CLI
Remote; optional battery-optimization exemption prompt behind a
Settings row.

## More Information

### Research — official Go / Dart / Flutter process and memory docs

Read 2026-08-15. These are the facts D4/D5/D7 rest on. Informal blogs
are not cited.

| Source | What it actually says | What we may / must not do |
| --- | --- | --- |
| [`runtime/debug.SetMemoryLimit`](https://pkg.go.dev/runtime/debug#SetMemoryLimit) (Go 1.19+) | Soft limit on `MemStats.Sys - HeapReleased` **in this process**. Excludes the Go binary, OS kernel memory on behalf of the process, C/`syscall.Mmap`, and **any other process**. `GOMEMLIMIT` is the env form. A limit at or below current use makes GC run nearly continuously; the app may still progress. | May set on `mcremote` itself (today ~39 MiB RSS — pointless for this incident). Must **not** treat it as a cap on kilo/opencode/flutter/grok children. |
| [Go GC guide — Memory limit](https://go.dev/doc/gc-guide#Memory_limit) | **Do** use the limit when the Go program is the *only* consumer of a reservation, and leave 5–10% headroom. **Don't** use it when the program shares limited memory with other programs. **Don't** use it to dodge an already-tight OOM — that swaps OOM for thrashing (runtime caps GC at ~50% CPU, then still exceeds). | Matches this host: five agent engines + their jobs share 7.8 GiB. Primary control is **not starting them** (D5) and **OS cgroups** (D4). |
| [Go GC guide — GOGC](https://go.dev/doc/gc-guide#GOGC) | Doubling GOGC roughly doubles heap overhead and halves GC CPU. `GOGC=off` is legal only if a memory limit still applies. | Leave GOGC at default for `mcremote`. Do not `GOGC=off`. |
| [`os/exec.Cmd`](https://pkg.go.dev/os/exec#Cmd) | After `Start`, `Wait` **must** be called or resources leak. `CommandContext` default `Cancel` is `Process.Kill` (SIGKILL). `WaitDelay` then kills a child that ignores cancel or leaves pipes open. `Wait` also waits for pipe-copy goroutines; orphaned grandchildren that inherit the pipes keep `Wait` blocked until they close them. | Keep 0019's custom SIGTERM-then-SIGKILL (`procutil.TerminateProcessGroup`). Do **not** switch engines to the default `Kill`. Set a `WaitDelay` on job scopes so a wedged grandchild cannot pin `Wait`. Drain stdout/stderr. |
| [`os/exec` package overview](https://pkg.go.dev/os/exec) | Does not invoke a shell; does not expand globs. `SysProcAttr` is the supported hook for OS-specific start (process group, `Pdeathsig`). | Continue `Setpgid` for trees we spawn. Sibling systemd scopes (D4) are *in addition to*, not instead of, this. |
| [`dart:io` Process](https://api.dart.dev/stable/dart-io/Process-class.html) | `start` returns a `Process`; `run` does not. stdout/stderr are **pipes with limited capacity** — if the child writes past the buffer and the parent does not read, the child **blocks forever**. `kill` defaults to SIGTERM. `Process.killPid` exists. `ProcessStartMode.detached` orphans the child. | Flutter app must keep draining any process it starts (not applicable to host engines). Do **not** detach kilo's `background_process` via Dart; isolation is a host/systemd job. |
| [Flutter isolates](https://docs.flutter.dev/perf/isolates) | Isolates have **their own heap** and talk only by message copy (or transfer on `Isolate.exit`). They cannot share the main isolate's memory. They cannot manage a host OS process. UI / `rootBundle` / unsolicited platform-channel pushes stay on the main isolate. `compute` / `Isolate.run` are for CPU work that would miss a frame. | Isolates do **not** keep the WebSocket alive and do **not** constrain host RSS. Android keep-alive stays FGS + wake lock (D1). Do not spawn a worker isolate "to hold the socket." |
| [Dart concurrency](https://dart.dev/language/concurrency) | Same actor model: no shared mutable state. | Confirms isolates are the wrong tool for "keep a socket + timers firing under Doze." |

Implication, one sentence: **the only official levers that see kilo / flutter / opencode RSS are (1) not starting those processes (D5/D7) and (2) the OS process/cgroup APIs `os/exec` and systemd already wrap (D4).**

### Incident timeline (host `wonder`, 2026-08-15 UTC)

Source: `journalctl --user -u mcremote`, kilo engine logs under
`~/.local/share/kilo/log/`, session
`18464783-7ccd-4534-bec6-d5941f1daa8f` (kilo, cwd=`/home/mac`,
`agent_session_id=ses_ffca69fbaffeZCC2mGKG8MMiMq`, owner phone
`a4d0b5ac-4081-4adc-a7e3-4ab0f83f68c3`).

| Time (UTC) | What happened |
| --- | --- |
| 03:24–03:30 | Four operator `systemctl` stop/start cycles. Each: `shutting down reason="terminated signal received"` → `session closed purge=false` → `engine stopped … kilo … graceful=true`. Memory peaks recorded: **3.6 GiB** (01:08), **4.8 GiB** (03:24), **1.9 GiB in 1m49s** (03:26). |
| 03:30:36 | kilo `prompt_async` / `session.turn.open` after the last bounce. |
| 03:45–04:27 | Hundreds of kilo `background_process.updated` events (the long job). |
| 04:25:28 | Phone **peer_closed** the mesh socket in the same second a **relay tunnel was bridged** (`local=100.64.0.4:7531` — mesh address of the host). DialEpisode failover while the tailnet was up. |
| 04:27:17 | **`mcremote.service: The kernel OOM killer killed some processes in this unit.`** `Failed with result 'oom-kill'`. kilo log last line is `background_process.updated`. Session closed `purge=false`. kilo engine `graceful=true`. |
| 04:27:20 | systemd `Restart=always`. Engines prewarm (opencode + kilo + grok ACP). **No session rehydrate.** |
| 04:29:53–04:30:00 | Phone, still holding the chat, sends seven RPCs → `ws error frame code=session_not_live`. Toast path: *"That session is no longer running on the host — start again from Sessions."* (`mc_exception.dart`) |
| 04:30:29 | Manual / UI `resumeSession` → `http session resumed` + `session created` (same ids). **The in-flight turn and OS children are already dead.** |
| 04:36:00 | New `kilo prompt_async` (operator or agent retry). |
| 04:44:27 | `ws client disconnected reason=read_deadline` (120 s, host config `limits.ws_read_deadline_seconds`). App-ping period is 10 s (`kAppPingPeriod`) — the Dart timer did not fire for two minutes. |
| 04:44:30 | Relay tunnel bridged again. `_noteTransportDeath()` + 0063 D6: **prefer the other transport**. Mesh still up. |
| 04:47:35 | Relay registration ping/pong `context deadline exceeded` (host under memory pressure). |
| 04:48:56 | kilo SSE resync `context deadline exceeded`. |
| 04:49:34 | **Second `oom-kill`.** Session closed. kilo stopped. |
| 04:49:40 | Restart. Phone reconnects **via relay** immediately (`tunnel bridged` at 04:49:43) because sticky/death state still says mesh died. |

Host at assessment time (same failure mode still live):

| Signal | Value |
| --- | --- |
| RAM / swap | 7.8 GiB / **0 B** |
| `mcremote.service` `MemoryCurrent` / `memory.peak` | **5.8 GiB / 6.4 GiB** |
| `MemoryHigh` / `MemoryMax` | infinity / infinity |
| Top RSS in the unit | kilo serve 652 MiB, flutter frontend_server 624 MiB (kilo grandchild), opencode 418 MiB, flutter_tester 278 MiB, grok ×2, goose serve |
| kilo session meta | still `status: running`, cwd `/home/mac` |
| `background_process` in mcremote | **zero references** in Go or Dart |

### Why each symptom happened

**"Failed over to mcrelay even with the mesh up."**

0063 D6 / `McremoteClient._noteTransportDeath` records the *active*
transport as dead and the next DialEpisode **returns the other path
without probing** (`mcremote_client.dart` `_resolvePrimaryTransport`).
A `read_deadline` after Doze is not evidence the tailnet is down — it
is evidence the **phone stopped writing**. Success on relay is then
persisted as sticky (`_recordTransportSuccess`), so later reconnects
stay on relay. 04:25:28 and 04:44:27–30 are that path. The tunnel
`local=100.64.0.4:7531` is the host's Tailscale address: mesh was up
on the host the entire time.

**"The kilo session was closed."**

Live sessions exist only in `Manager.sessions`. Every shutdown —
SIGTERM from systemd, including the SIGTERM systemd sends after
`oom-kill` — runs `gracefulDrain` → `mgr.CloseAll` (`daemon.go`).
`CloseAll` calls provider `Close` and persists `status=disconnected`.
Nothing on the next `serve` re-creates those rows. The phone's
in-memory chat keeps sending; the host answers `session_not_live`.
Resume is a **manual** `createSession` with the old id (`resumeSession`
in `mcremote_client.dart`), and it only reattaches the **conversation**,
not the turn.

**"The kilo task was killed."**

Kilo implements long work as `background_process` children of
`kilo serve`. Those processes (last night: the release-class job;
this afternoon: `flutter test` under `/data/gitrepos/magic-git`) sit
in `user@1000.service/app.slice/mcremote.service` because
`KillMode=control-group` (0019, `mcremote.user.service.tmpl`). An
OOM in that unit, or any `systemctl restart`, SIGTERMs the whole
tree. mcremote has no type for that job, so the phone cannot show
"build still running" or "build killed by host OOM."

**Why Doze beat the foreground service.**

The FGS exists "only to keep the app process — and therefore the
main-isolate WebSocket — alive" (`foreground_service.dart`). It
sets `allowWakeLock: false` / `allowWifiLock: false` on purpose.
0084 D3 then **removed** the `WAKE_LOCK` permission as unused.
`remoteMessaging` keeps the process eligible; it does **not** keep
Dart `Timer.periodic` (app-ping, 10 s) or `WebSocket.pingInterval`
(20 s) firing under Samsung Doze. The host's only liveness signal
is inbound data (`internal/ws/liveness.go`). After 120 s of silence
it closes with `reason=read_deadline`. That is working as designed
— the design assumed the FGS would keep timers honest.

### Findings that apply to every agent

| Layer | kilo (incident) | grok / goose / codex (ACP) | opencode (HTTP) |
| --- | --- | --- | --- |
| Phone Doze / FGS / DialEpisode | same socket | same | same |
| `CloseAll` on daemon death | session dropped | session dropped | session dropped |
| Engine in mcremote cgroup | `kilo serve` | `grok agent` / `goose serve` / `codex` | `opencode serve` |
| Long OS job in same cgroup | `background_process` | spawned tools / sub-agents | `bash` / child sessions |
| Prewarm on this host | `true` | grok `true` | `true` |
| First-class job object in mcremote | none | none | none |

### Amendments to prior records

* **0084 D3** — wake lock is no longer "unused permission to delete."
  It is required while a live turn needs the radio. Parked/idle
  remains no-lock.
* **0062 D6 / 0063 D6** — "the transport that just died is the worst
  primary" is true for a blackholed mesh. It is false for a
  screen-lock liveness stall. Classification is the amendment.
* **0019 KillMode=control-group** — still required for the engine.
  Refined: job children must not share that cgroup's OOM/kill
  domain. `prewarm` default **true → false** (0019 §4.2).
* **0012 §4.2 / grok Phase 4.2** — grok `prewarm` default **true → false**.
* **0075** — kilo `prewarm` default **true → false**.

### What this record is not

* Not a request to raise `ws_read_deadline_seconds` past 120. A
  frozen client should not hold a half-open socket forever; it
  should **not freeze**.
* Not a request to adopt iOS parking on Android.
* Not a kilo Cloud / `kilo remote` change. The engine's own
  `background_process` API stays; mcremote must supervise and
  isolate it.
* Not a swap-file implementation in-tree. Ops on 8 GiB hosts
  should add swap; the unit must still bound itself.
