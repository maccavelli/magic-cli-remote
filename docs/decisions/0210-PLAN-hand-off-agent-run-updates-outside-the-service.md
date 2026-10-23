---
status: in-progress
date: 2026-10-23
associated-madr: "0210-MADR-hand-off-agent-run-updates-outside-the-service.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement handing an agent-run update off to the service manager, and making every backend fail safe

Associated MADR: [0210-MADR-hand-off-agent-run-updates-outside-the-service.md](0210-MADR-hand-off-agent-run-updates-outside-the-service.md),
with the owner's answers of 2026-10-23: Windows waits on
[go-selfupdate-lib#6](https://github.com/maccavelli/go-selfupdate-lib/issues/6) (D3); the script
backends get the guard now, and the handoff via
[go-selfupdate-lib#2](https://github.com/maccavelli/go-selfupdate-lib/issues/2) (D4); the Linux
reproduction puts a shell into the unit's cgroup.

## Goal

* On systemd and launchd, an `mcremote update --yes` started inside the service hands off. The
  daemon restarts on the new version, and `doctor` reports the outcome (D1, D2, D5, D6).
* On runit, s6 and OpenRC, the same command fails safe: `ErrInsideService`, exit 1, the service
  still up (D4).
* On Windows, nothing changes: no handoff, and the library's backend keeps failing safe (D3).
* Each behaviour is shown on its host before the change, and confirmed after it (D7).

## Scope

* **In scope:** the MADR's D1, D2, D4, D5, D6 and D7, in the phases below.
* **Not in scope:**
  * D3's Windows handoff and its grok `terminal/create` measurement. They wait for #6, and an
    amendment of the MADR adopts them.
  * A handoff on runit, s6 or OpenRC, which waits for #2.
  * `setup-service`'s own stops and starts, which are 0207 D4.
  * Any provider quota: no phase runs a real agent CLI.
* **Out of the tree:** the probe kit (R0) lives in the scratchpad, as earlier plans' probes did.
  Nothing in it is committed.

## Facts the steps rely on [read 2026-10-23, at `b946cf2d`]

| Fact | Where |
| :--- | :--- |
| The update command calls `cli.Run(ctx, u, req, o)` with no `HandOff` | `internal/updateclient/client.go:250` |
| It is shared by both products | `internal/relay/update.go:13`, `updateclient.NewCommand` |
| Its test seams are `commandIdentity` and `newCommandUpdater` | `client.go:166-180`; `command_test.go:76-81` |
| The backend for this OS comes from `service.NewBackendWith(product, unitName, h)` | `internal/cli/service/backend.go:39-95` |
| The library's three backends implement `service.Detacher`; Magic's `scriptBackend` does not | the library's `service` package doc; `scriptbackend.go` |
| `scriptBackend.Stop` has no inside check | `scriptbackend.go:80-104` |
| runit's and s6's samplers record the supervised PID; OpenRC's records none | `settle.go:198-218`, `sample.pid` at `:62` |
| OpenRC's script writes the daemon's PID to `"${XDG_RUNTIME_DIR:-/tmp}/<unit>.pid"` | `supervisor.go:138`, `:167` (`--make-pidfile`) |
| Both `main`s call `cli.Execute` or `relay.Execute` first, and nothing before | `cmd/mcremote/main.go:13-27`, `cmd/mcrelay/main.go:13-27` |
| `doctor` composes its sections in `newDoctorCmd` | `internal/cli/doctor.go:26-57` |
| The daemon starts in `daemon.Run` | `internal/daemon/daemon.go:211` |
| The daemon's shutdown closes sessions, killing each terminal's process group | `daemon.go:701` (`mgr.CloseAll`); `acpagent/terminal.go:31-47` |
| A provider's binary is configurable, as `providers.grok.bin` | `internal/config/load.go:313` |
| `scripts/smoke-protocol` creates a session with `-provider` and sends `-prompt` | `scripts/smoke-protocol/main.go:40-52` |
| The guides that document `mcremote update` | `docs/guides/ops-linux-install.md:263-280`, `ops-windows-install.md:231-232`, `README.md:111` |
| The latest published release | `v0.22.2` (`git tag`) |
| `--force` "replaces a local build", and `--version` "selects an exact release tag" | the library's `cli.Flags` |

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Every new test is seen failing first,** on a scratch copy, never in the working tree. The
  execution record quotes the failure.
* **Before staging:** `make pre-add-check FILES="…"` on every staged `.go` file, then `make test`,
  `make race`, and `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64. Also the
  guard's redact preview from the repository root, and `make check-records`.
* **Host runs** (R1, L1) run on the owner's word for that phase. Each uses a scratch service:
  * unit, label or task `mcremote-d3probe`;
  * its own `XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_DATA_HOME` under a temporary directory;
  * listen port `17631`, so the host's real `mcremote` is never stopped, restarted or rewritten
    (the test-host notes).

  Each run ends with the scratch service removed (`setup-service --remove --unit-name
  mcremote-d3probe`) and its directories deleted.
* **A phase ends with its execution record** here, staged for the owner, who commits it.

### R0 — The probe kit (scratchpad only)

**Files:** none in the tree. Under the scratchpad's `m54/d3/kit/`:

1. **`probeagent/`**, a Go ACP agent built from `acp-go-sdk2@v1.20.0/example/agent`, in a module of
   its own that requires `acp-go-sdk2 v1.20.0`.
   * It answers `initialize` with no auth methods, and `session/new` with a session.
   * On `session/prompt` whose text begins with the word `RUN` and a space, it calls the client's `terminal/create` with
     `sh -c "<rest>"` (`cmd.exe /c` on Windows), then `terminal/wait_for_exit`. It returns the
     exit status as an agent message, then ends the turn.
2. **`d3config.yaml.tmpl`**, a scratch daemon config:
   * listen `127.0.0.1:17631`, TLS off;
   * `auth.require_client_key: false`, so `smoke-protocol` can connect;
   * `providers.grok.bin` set to the probe agent's absolute path;
   * every other provider disabled;
   * the scratch data directory.
3. **`d3_run.py`**, the driver. Its phases are `install`, `inside-cgroup`, `inside-agent`,
   `observe` and `remove`. It prints, for each run:
   * the update's exit status and last stderr line;
   * the daemon's PID, before and after, from the backend's own status command;
   * its `/health` version, before and after;
   * `doctor`'s handoff section (L1 only).

   It runs on each host over SSH, as the S6 runner did, with `-o BatchMode=yes -o
   ConnectTimeout=5`, and in the Podman containers locally.
4. **Containers** (local Podman, the test-host notes):
   * Void Linux with runit;
   * Alpine with s6;
   * Alpine booted with `/sbin/init` for OpenRC.

   Each gets a Linux build of the binary and the probe agent.
5. **Kit self-check:** on the macOS host, `inside-agent` against a scratch daemon prints the
   probe's `RUN echo ok` output through `smoke-protocol`. Any failure to reach `terminal/create`
   stops R1, as a deviation.

### R1 — Reproduce today's behaviour (D7, before any change)

**Files:** this plan (the execution record). The binary is a build of `HEAD`, the commit before H1.

1. **Linux, systemd** (the Linux test host). `install`, then `inside-cgroup`:
   * the driver writes a shell's PID to the scratch unit's `cgroup.procs`, at the path that
     `systemctl --user show -p ControlGroup --value mcremote-d3probe.service` names, under
     `/sys/fs/cgroup`;
   * from that shell it runs `mcremote update --yes --force --version v0.22.2`.

   Expected: exit 1, with `update failed: … this process runs inside the service it would stop`,
   and the daemon's PID and version unchanged. A refused write to `cgroup.procs` is a deviation.
   Also recorded: `systemctl --version` is 236 or later, which `systemd.Unit.Detach` needs.
2. **macOS, launchd** (this host). `install`, then `inside-agent`, with the prompt `RUN mcremote
   update --yes --force --version v0.22.2`. Expected: as in step 1.
3. **Windows, Task Scheduler** (the Windows test host). As in step 2, with `cmd.exe /c`. Expected:
   as in step 1, with `ErrInsideService`. Any other outcome is recorded as measured, since F3's
   edge is from reading.
4. **runit, s6, OpenRC** (one container each). `install`, then `inside-agent`. Recorded as it
   happens:
   * the update's exit;
   * whether the service is up afterwards (`sv status`, `s6-svstat`, `rc-service --user …
     status`);
   * whether the binary changed.

   F2 predicts a dead update and a service left down.
5. **Records.** The outcomes go in R1's execution record. Any outcome that contradicts the MADR's
   F1 to F3 is a deviation: stop, prompt, and amend the MADR before H1.

### H1 — The handoff in the update command (D1, D2, D6)

**Files:**

* `internal/updateclient/client.go`, `command_test.go`;
* `cmd/mcremote/main.go`, `cmd/mcrelay/main.go`;
* `docs/guides/ops-linux-install.md`, `docs/guides/ops-windows-install.md`, `README.md`;
* this plan.

1. **Tests first,** in `command_test.go`, with a new seam, `newCommandDetacher func(product
   string, health service.HealthOptions) (libservice.Detacher, bool)`:
   * a `Detacher` that reports inside, with `--yes`, makes the command print `update handed off:`
     and exit 0. The fake records a `HandOff` whose `Args` are the command's;
   * the same with `--json` writes a result object with `handed_off`;
   * inside, without `--yes`: `ErrConfirmationRequired`, exit 1;
   * `newCommandDetacher` returning false, the script backends and Windows: no handoff, and the
     run is as today;
   * `--check` and `--dry-run` never call `Detach`.

   Seen failing on today's command.
2. **`client.go`:**
   * `newCommandDetacher`'s production value builds `service.NewBackendWith(product, "", health)`
     and asserts `libservice.Detacher`. It returns false on `runtime.GOOS == "windows"` (D3), for a
     backend that is not a `Detacher` (D4), and on a backend error, where the managed installer's
     own path decides.
   * In `RunE`, after the updater is built: when it returns true, set `o.HandOff =
     cli.HandOff{Detach: libservice.HandOffFunc(d, libservice.HandOff{Args: os.Args[1:]}),
     Report: libservice.ReportFunc()}`.
   * The `Long` help gains one paragraph, D6: "Run from inside the service, such as by an agent in
     a phone session, the update hands itself off to the service manager and needs `--yes`, since
     the detached run cannot prompt. On Windows, and under runit, s6 and OpenRC, it refuses
     instead and leaves the service running."
3. **Both `main`s** call `libservice.LoadHandOffEnvContext(ctx)` first, with a 15 s
   `context.WithTimeout`. On an error they print `error: handoff environment: <err>` and exit 1.
   A test in each `main` package is not possible without a subprocess. The behaviour is covered by
   the library's own tests and by L1.
4. **Guides.** Each update section gains the D6 paragraph, worded for its OS:
   * `ops-linux-install.md:263` covers systemd and the script supervisors;
   * `ops-windows-install.md:231` covers Windows;
   * `README.md:111` gets one line.
5. **Gates,** as in the rules. Also `make markdownlint-docs`.

### H2 — The script backends fail safe (D4)

**Files:** `internal/cli/service/scriptbackend.go`, a new `scriptbackend_inside_linux.go` with
`scriptbackend_inside_other.go`, `scriptbackend_unix_test.go`, and this plan.

1. **Tests first**, with a seam for the parent lookup, `parentOf func(pid int) (int, error)`, and
   one for the supervised PID:
   * this process's ancestry reaching the supervised PID makes `Stop` return an error that is
     `libservice.ErrInsideService`, and runs no stop command (the fake runner records none);
   * ancestry not reaching it, or a supervised PID of 0 (not running), stops as today;
   * an ancestry read error is returned, never read as "outside", as `HandOffIfInside` treats
     `Inside`'s errors.

   Seen failing on today's `Stop`.
2. **`Inside(ctx) (bool, error)`** on `scriptBackend`:
   * for runit and s6, the supervised PID is `sample.pid` from the existing sampler;
   * for OpenRC, it is the integer in `"${XDG_RUNTIME_DIR:-/tmp}/<unit>.pid"`, the path
     `supervisor.go:138` renders;
   * it walks `os.Getpid()`'s ancestors by `/proc/<pid>/stat`, field 4, to PID 1, and is true
     when the walk meets the supervised PID;
   * the non-Linux file returns false, since the script kinds are Linux-only (`backend.go:51-60`).
3. **`Stop`** calls `Inside` first. When it is true, `Stop` returns
   `fmt.Errorf("%s: %w", b.name, libservice.ErrInsideService)` before running `sv`, `s6-svc` or
   `rc-service`. The managed installer's `recoverStop` then leaves the service running, as on
   the library's backends.
4. **Gates,** as in the rules. Also `GOOS=linux go test ./internal/cli/service/` on the Linux
   test host, since the `/proc` walk is Linux-only.

### H3 — Reporting (D5)

**Files:**

* `internal/cli/doctor.go`, `doctor_test.go`;
* `internal/daemon/daemon.go`, and a new `daemon_handoff.go` with its test;
* this plan.

1. **Tests first:**
   * `renderHandOffDoctor(w, res, err)` prints:
     * "self-update handoff: none" for `fs.ErrNotExist`;
     * the ID, start and finish times, exit code and error for a result;
     * the read error otherwise.
   * The daemon's `cleanupHandOffs(ctx, goos, newBackend)` calls the backend's
     `CleanupHandOffs` once on `darwin`, through an interface assertion. It calls nothing
     elsewhere, and logs, never returns, a failure.

   Each is seen failing first.
2. **`doctor.go`:** after the silent-commands section, `exe, _ := os.Executable()`, then
   `libservice.ReadHandOffResult(libservice.DefaultResultPath(exe))` into `renderHandOffDoctor`.
   The `Long` text names the new section.
3. **`daemon.go`:** in `Run`, after start-up, `cleanupHandOffs` runs once in a goroutine with a
   10 s timeout. It uses `service.NewBackend("mcremote", "")`, so start-up never waits on
   `launchctl`.
4. **Gates,** as in the rules.

### L1 — Confirm on each host (D7, after H1 to H3)

**Files:** this plan (the execution record), and the MADR (status and a note). The binary is a
build of H3's commit.

1. **Linux, systemd.** `install`, then `inside-cgroup`. Expected:
   * `update handed off: …`, exit 0;
   * a transient unit `mcremote-d3probe-selfupdate-<id>.service` appears, in `systemctl --user
     list-units`;
   * within the health timeout, the scratch daemon has a new PID, and `/health` reports `v0.22.2`;
   * `doctor` run from the swapped binary's directory shows the handoff with exit code 0.

     The published `v0.22.2` has no handoff section, so the result file is read directly
     instead, with `cat` of `.mcremote.selfupdate.handoff` beside the binary.
2. **macOS, launchd.** `install`, then `inside-agent`. Expected: as step 1, with a one-shot job
   `<label>.selfupdate.<id>` instead of a transient unit. After a daemon restart from this
   change's build, `launchctl print` shows no finished handoff job, which is
   `CleanupHandOffs`'s effect.
3. **Windows.** `inside-agent`. Expected: exit 1 with `ErrInsideService`, the service up, the
   binary unchanged. This is R1's outcome, now with `HandOff` unset by design.
4. **runit, s6, OpenRC.** `inside-agent`. Expected:
   * exit 1, with `ErrInsideService`;
   * the service up, with the same PID;
   * the binary unchanged.

   This is the fail-safe that R1 lacked, if R1 recorded F2.
5. **Records.**
   * L1's execution record.
   * The MADR becomes `accepted` for D1, D2 and D4 to D7, with a dated note. D3 stays open on #6.
   * 0054's PLAN gets a dated note: the handoff item of "What stays deliberately unadopted" is
     adopted by 0210, except on Windows and the script supervisors.
   * The plan becomes `complete`. A later amendment, when #6 or #2 ships, opens a new phase for
     the Windows or script-supervisor handoff.

## Verification

* R1's outcomes are recorded for systemd, launchd, Task Scheduler, runit, s6 and OpenRC, before
  any change.
* H1 to H3: every new test seen failing first, then the gates clean.
* L1: the handoff completes on systemd and launchd, with a new PID, the new version and a
  recorded result. Windows and the script supervisors fail safe with the service up.
* No host's real `mcremote` service changed during R1 or L1. The real service's PID is recorded
  before and after each run.

## Rollout and Rollback

* **Rollout.** The change reaches users at the next tag, after `make ci-windows-smoke` on the
  Windows host (`AGENTS.md`).
  * A handoff needs the running binary to have it. The first update that hands off is the one
    after the tag that ships this change.
  * Operator-run updates from a shell are unchanged: `Inside` is false there, so the run is as
    today.
* **Rollback.** Revert H1 to H3's commits. The update command then sets no handoff again, and the
  script backends lose their guard, which brings back F2. The library's three backends keep
  failing safe either way.

## Execution record

The owner approved this plan with "Proceed to r0", 2026-10-23.

### R0, 2026-10-23: the probe kit

Everything is in the scratchpad's `m54/d3/kit/`. Nothing in the tree changed but this record.

**What was built.**

* **The binaries** (`build_kit.py`). `mcremote` and `smoke-protocol` come from `git archive HEAD`
  at `b946cf2d`, so no uncommitted change enters them. The probe agent comes from the kit's
  `probeagent/` module, which requires `acp-go-sdk2 v1.20.0`. All are built with
  `CGO_ENABLED=0 -trimpath` for darwin/arm64, linux/amd64, linux/arm64 and windows/amd64.
  `mcremote version` prints `dev (local)`, so L1's `update` needs `--force`, as the plan says.
* **The probe agent** (`make_probeagent.py`). It is generated from the SDK's
  `example/agent/main.go`, which keeps every interface method. Its `Prompt` handles `RUN
  <command>` by calling `terminal/create` with `/bin/sh -c`, or `cmd.exe /c`, then
  `terminal/wait_for_exit`, `terminal/output` and `terminal/release`. It reports
  `probeagent: exit <n>` and the output as an agent message.
* **The driver** (`d3_run.py`). Its phases are `selfcheck`, `install`, `inside-cgroup`,
  `inside-agent`, `observe` and `remove`, under the unit, label or task `mcremote-d3probe` on port
  17631. It sets a scratch `XDG_STATE_HOME` and `XDG_DATA_HOME`, by `--env` for the service, and
  passes `--service-config`.
* **The containers** (`containers/`). `d3probe-runit` is `ghcr.io/void-linux/void-musl` with
  `runit`, `shadow`, `python3` and `curl`. `d3probe-s6` is Alpine 3.22 with `s6`.
  `d3probe-openrc` is Alpine 3.22 with OpenRC 0.62.6, booted with `/sbin/init`. Each has a
  non-root `tester` user, as 0054's container runs did. The Podman machine was started for the
  run and stopped after it.

**Seen working.**

* **The self-check** (step 5), on macOS. A foreground scratch daemon used the probe as its grok
  provider. Prompted through `smoke-protocol` with `RUN echo ok`, it returned:

  ```text
  selfcheck: daemon up, /health version up, binary dev (local)
  selfcheck: smoke-protocol exit 0; probe output seen
     event type=assistant_message_chunk      status=             session=6d2b0c5f text="probeagent: exit 0\nok\n" err=""
  ```

  So an agent's command reaches Magic's `terminal.go` and runs under the daemon, which is the
  path R1 relies on.
* **The containers' detection** (`containers.py check`), each as `tester`:

  ```text
  runit: sv=yes runsvdir=yes; print-only exit 0: definition starts '#!/bin/sh'; 15 lines
  s6: s6-svc=yes s6-svscan=yes; print-only exit 0: definition starts '#!/bin/sh'; 15 lines
  openrc: rc-service=yes openrc=yes; print-only exit 0: definition starts '#!/sbin/openrc-run'; 18 lines
  ```

**Found on the way, and fixed in the kit** (none of it changes the plan):

1. **`/health` does not exist.** The daemon serves `/healthz`, which answers `{"ok":true}` with no
   version. The update's own readiness check is the admin-socket ping (`internal/cli/health.go`,
   `adminPingProbe`). So the driver reads readiness from `/healthz`, and the version from the
   binary on disk with `version`, which after a swap is the new one. Where the plan says
   `/health`, read `/healthz` plus `version`.
2. **The example agent starts its arguments as a client.** Magic starts the grok provider's binary
   with grok's arguments. The first self-check failed with `acp initialize: … "input ended before
   a response"`, because the example tried to run those arguments and exited. The probe now
   always speaks ACP on its stdio.
3. **Void's base image has no `useradd`.** `shadow` is added.
4. **OpenRC's `--user` mode needs `XDG_RUNTIME_DIR`.** Without it, `rc-service --user --help`
   exits 1, Magic detects no OpenRC user services, and `--print-only` renders the systemd unit.
   The container check now gives `tester` `/run/user/1000`, mode 0700, as a login would. R1's
   OpenRC run passes the same.

**Not done in R0.** R1, which needs the owner's word.

The owner committed the records as `ecffce5c` and approved R1 ("committed, proceed").

### R1 Deviation 1, 2026-10-23 — the scratch service lives under `$HOME`. The owner decided: "Scratch dir under $HOME"

**Found** by R1's first macOS run, before any update reached a stop:

```text
update failed: selfupdate: mcremote: selfupdate: target …/scratchpad/m54/d3/kit/scratch-d3probe/mcremote is outside allowed roots
```

The library's `TargetPolicy.AllowedRoots` are "additive canonical directories besides the user's
home directory", and the update command sets none (`internal/updateclient/client.go:98-104`,
`TargetPath` unset). So a binary outside `$HOME` cannot be updated at all. The rules' "scratch
directories under a temporary directory" put it in the session's temporary scratchpad. This is
not a Magic defect: the policy is the library's, on purpose.

**Decision.** ~~"its own `XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_DATA_HOME` under a temporary
directory"~~. The scratch service now lives under `~/mcr-d3probe` on each host and in each
container: its binary copy, config, data, state and work directories. It keeps its own name, port
and XDG directories, and is deleted at each run's end. The kit's driver is the only change.

**Also recorded, accepted by the owner ("Accept, and record it"):** on macOS the scratch plist
logs to the same `~/Library/Logs/mcremote/mcremote.err.log` as the real service, because
`setup-service` has no log-path flag. Its lines interleave with the real service's while a run
lasts. They are identifiable by port 17631 and the scratch data directory.

**Also found on the way, fixed in the kit:**

* The probe agent advertised no auth method. Magic's grok spec then refuses `session/new` with
  `acp: authentication required (advertised [])` (`acpagent/authselect.go:47`), unless a key is
  present in the daemon's environment. The R0 self-check passed only because its foreground daemon
  inherited this shell's environment.
* The probe now advertises `cached_token`, and its `Authenticate` accepts it, so no credential
  enters a scratch service's environment.
* The kit was rebuilt from `ecffce5c`, whose code is `b946cf2d`'s; the commit between them is
  docs only.

The first macOS scratch service was removed (`setup-service --remove` exit 0; `launchctl print`
of its label: "Could not find service"). The real service's PID was unchanged before and after.

### R1 Deviation 2, 2026-10-23 — Linux reaches "inside" through the probe agent. The owner decided: "Use the probe agent on Linux too"

**Found** by R1.1's first run on the Linux test host (systemd 259):

```text
inside-cgroup: ControlGroup /user.slice/user-1000.slice/user@1000.service/app.slice/mcremote-d3probe.service
inside-cgroup: update exit 97 (cgroup.procs write refused)
   /bin/sh: 1: echo: echo: I/O error
```

The plan named a refused write as a deviation. The cause:

* An SSH shell starts in `0::/user.slice/user-1000.slice/session-37.scope`, outside the user
  manager's delegated subtree.
* Moving it into the scratch unit's cgroup needs a write at their common ancestor,
  `user-1000.slice`, which belongs to root.
* A shell started with `systemd-run --user --scope` starts in
  `…/user@1000.service/app.slice/run-p…scope`, inside the subtree. That was offered, and declined.

**Decision.** ~~R1.1's and L1.1's `inside-cgroup`~~. Linux reaches "inside" as the other hosts do:
the scratch daemon's probe agent runs the update through Magic's terminal (`inside-agent`). It
needs no provider quota. The MADR's answer to question 3 is amended to match. Nothing changed on
the host:

* the scratch unit was installed and removed (`setup-service --remove` exit 0);
* `~/mcr-d3kit` was deleted;
* the real service's `MainPID` was 1356 before and after.

### R1 Deviation 3, 2026-10-23 — the run removed the Windows host's real task, which was restored. Windows cannot host a scratch service. The owner decided: "Yes, restore it now" and "Probe the real task, carefully"

**What happened.**

* `setup-service --unit-name mcremote-d3probe` on the Windows test host failed with "error:
  --unit-name is not supported on Windows: the task is always named mcremote (MADR 0159 D13)".
* The kit's runner then ran its cleanup on the failed install, `setup-service --remove --unit-name
  mcremote-d3probe`. On Windows that removes the one task there is, the real one: "Removed: Task
  Scheduler\mcremote. The daemon was stopped and the service disabled."
* The real `mcremote` (PID 10000, task `Running`) was stopped, and its task deleted.
* Not touched: the installed binary, the config, the data directory (devices, sessions, TLS key)
  and the saved definition `State/mcremote-task.prev.xml`.
* The plan's rules assumed a scratch name on every OS. Windows does not have one. The automatic
  removal was the kit's design error, not Magic's.

**Restored**, on the owner's word:

* The command was the installed binary's own `setup-service --config <the real config>`, the
  binary and config the saved definition names.
* It exited 0: "Task: Task Scheduler\mcremote … Enabled: yes … Started: yes".
* Afterwards the task was `Running`, with `mcremote` at a new PID. Its action, arguments
  (`serve --config …\mcremote\config.yaml --detach-console`) and working directory matched the
  saved definition. It has the logon and time triggers, and is enabled.

**The kit is fixed so this cannot recur:**

* the runner never removes after a failed install;
* the driver refuses `install` and `remove` on Windows, where the name always means the real task.

**Decision for Windows.** ~~a scratch task `mcremote-d3probe`~~. Windows is probed through its
real task, carefully. The installed binary was found to be `mcremote 0.20.1 (a078372b)`, older
than this month's library work, so a probe of it would show old code. Given that, the owner chose
"Upgrade the host first, then probe". The procedure, for R1.3 and L1.3:

1. **W0, the upgrade.** An operator `mcremote update --yes`, run by the installed binary from an
   SSH shell, outside the service. That is the supported upgrade path. It takes the host to the
   latest release, `v0.22.2`. The version, binary hash and task state are recorded before and
   after.
2. **W1, before the probe.** Record the task's state and PID, the binary's and config's SHA-256,
   and `pair list`. Copy the config to `config.yaml.d3bak`.
3. **W2, the temporary changes.** A script on the host edits only two keys in the real config:
   * `providers.grok.bin` becomes the probe agent's path;
   * `auth.require_client_key` becomes `false`, so the probe client can connect. Listen
     (`tailscale:7531`, tailnet only) and TLS are unchanged.

   Then the task is restarted (`Stop-ScheduledTask`, `Start-ScheduledTask`), and `pair create
   d3probe` mints a token.
4. **W3, the probe.** `smoke-protocol` dials the host's tailnet address on port 7531. It prompts
   `RUN <installed mcremote.exe> --config <real config> update --yes --force --version v0.22.2`,
   the installed version. So even an update that wrongly proceeds reinstalls the same release.
   Recorded: the transcript's result line, the task's PID, and the binary's hash.
5. **W4, the restore,** which runs whatever W2 or W3 did:
   * `pair revoke` of the `d3probe` device's ID;
   * the config copied back from `config.yaml.d3bak`, and its SHA-256 compared with W1's;
   * the task restarted, and confirmed `Running`;
   * `pair list` compared with W1's.

   The backup is deleted only after the comparisons pass.

### R1, 2026-10-23: today's behaviour, reproduced on every target

Every update below was `mcremote update --yes --force --version v0.22.2`. It ran through the
scratch daemon's terminal, at a probe agent's `RUN` prompt (Deviation 2). The binaries are the R0
kit's, from `ecffce5c` (Deviation 1), except on Windows, where the installed release ran.

| Target | The update's result (from the daemon's transcript) | Daemon afterwards | Binary afterwards |
| :--- | :--- | :--- | :--- |
| macOS, launchd | `update failed: … stop service: selfupdate: managed install failed selfupdate: service: this process runs inside the service it would stop: gui/<uid>/com.magiccliremote.mcremote-d3probe` | same PID, up | unchanged (`dev (local)`) |
| Linux, systemd 259 | `update failed: … this process runs inside the service it would stop: mcremote-d3probe.service` | same PID, up | unchanged |
| Windows, Task Scheduler, the real task at `v0.22.2` | `update failed: … this process runs inside the service it would stop: \mcremote` | same PID, `Running` | same SHA-256 |
| runit (Void) | none was written: the update died with the daemon | **no serve process; `/healthz` unreachable** | unchanged |
| s6 (Alpine 3.22) | none was written | **`down (exitcode 0) 129 seconds, normally up`** | unchanged |
| OpenRC 0.62.6 (Alpine 3.22) | none was written | **`* status: stopping`, no serve process** | unchanged |

**What this confirms.**

* **The MADR's F1 holds.** On the library's three backends, an agent-run update fails safe with
  `ErrInsideService`, the daemon stays up, and nothing is replaced.
* **F2 is reproduced, no longer only read from code.** On all three script supervisors, the stop
  took the daemon down. The daemon's teardown took the update with it, before any swap or start.
  The supervisor was left holding the service down. For runit, `sv status` printed nothing (the
  run's lookup of the service directory found none), so its state is read from the process table
  and `/healthz`.
* **F3's edge did not occur.** On Windows the update was judged inside and refused, and the real
  daemon kept its PID.

**The runs, in order, with what each left behind.**

1. **macOS.**
   * The first run failed on the target policy (Deviation 1).
   * The second ran under `~/mcr-d3probe` and gave the result above.
   * Both scratch services were removed (`setup-service --remove` exit 0, then `launchctl print`
     of the label: "Could not find service").
   * The real service's PID was the same before, between and after the runs.
2. **Linux.**
   * The first run's `cgroup.procs` write was refused (Deviation 2).
   * The second used the probe agent.
   * Both scratch units were removed. No `mcremote-d3probe` file remains under the user's systemd
     directory, and the kit and scratch directories are gone.
   * The real service's `MainPID` was the same before and after.
3. **Windows.**
   * The first attempt removed the real task, which was restored (Deviation 3).
   * W0 upgraded the host from `0.20.1` to `v0.22.2` (`update exit 0`). The task restarted on the
     new release.
   * W1 to W4 gave the row above. W4 restored the config byte for byte (SHA-256 `348495ea…`
     before and after).
   * W4's device check failed: the kit's `pair create d3probe` had passed its name as a
     positional argument, which `pair create` ignores, so the device was called `device` and the
     revoke found nothing.
   * The `device` row (created during the run, never used, keyless) was revoked by its ID by
     hand: "Revoked device device (…)". `pair list` then showed only the device paired before.
   * The backup and the kit were deleted. The task is `Running`.
   * The kit now passes `--name`.
4. **Containers.**
   * The first runit run showed the outage, without the supervisor and binary checks.
   * A second runit run failed on a release download (`update failed: … Get …`), before any stop,
     so it shows nothing.
   * A third gave the row above, as did one run each of s6 and OpenRC.
   * Each container was removed after its run, and the Podman machine was stopped.
5. **A stray directory.** `~/mcr-d3probe` on macOS, which the kit's guard check created through
   `install`'s preparation step, was found and deleted. It held only the scratch config, binary
   copy and empty directories.

**The kit's guards, seen working:**

* `check_windows_guard.py`: on a faked Windows, `install` and `remove` return 2 before any
  `setup-service` call. On the control, macOS, both reach it.
* The runner no longer removes anything after a failed install.

**Not done in R1.** H1, which needs the owner's word.

R1 was committed by the owner as `145ff6e0`, who approved H1 ("committed, proceed").

### H1, 2026-10-23: the handoff in the update command

**What changed.**

* **`internal/updateclient/client.go`:**
  * `newCommandDetacher` builds the managed installer's own `newBackend(product, "", health)`, and
    returns it through `detacherFor(runtime.GOOS, b)`.
  * `detacherFor` returns false on Windows (D3), and for a backend that is not a
    `libservice.Detacher` (D4).
  * `commandArgs` is `os.Args[1:]`, so a detached run repeats the whole command line, global flags
    included.
  * In `RunE`, after the updater is built, a backend that is a `Detacher` sets
    `o.HandOff = cli.HandOff{Detach: libservice.HandOffFunc(d, libservice.HandOff{Args:
    commandArgs()}), Report: libservice.ReportFunc()}`.
  * The `Long` help gains D6's paragraph, and its exit-status line counts "handed off" under 0.
* **`cmd/mcremote/main.go`, `cmd/mcrelay/main.go`** call `libservice.LoadHandOffEnvContext`
  first, with a 15 s context. On an error they print `error: handoff environment: <err>` and exit 1.
* **`internal/updateclient/command_test.go`:**
  * `runCommandContext` now sets `newCommandDetacher` to `commandTestDetacher`, or to none. No
    test reaches the host's real service manager.
  * Six tests are new: `TestUpdateHandsOffFromInsideTheService`, `…HandOffUnderJSON`,
    `…HandOffNeedsYes`, `…OutsideTheServiceRunsItself`, `…ChecksAndDryRunsNeverHandOff` and
    `TestDetacherForGatesWindowsAndScriptBackends`. They use a fake `Detacher` that records each
    handoff.
  * The tests were first written in a new `handoff_test.go`, which is not in H1's files. They were
    moved into `command_test.go`, and that file was deleted before anything was staged.
* **Guides:** `ops-linux-install.md` and `ops-windows-install.md` each gain an "Updating from a
  phone session" paragraph for their OS, and `README.md` one sentence.
  * The Linux paragraph first said that `mcremote doctor` shows a handoff's outcome. That is H3's
    work, so the clause was removed, to keep H1's commit true.
  * H3 adds it back, which adds `ops-linux-install.md` to H3's files.

**Seen failing first**, on a scratch copy holding H1's tests and seams (`h1_plants.py`):

| Plant | Failed |
| :--- | :--- |
| the handoff not wired (today's command: `… ok && false`) | `…HandsOffFromInsideTheService`, `…HandOffUnderJSON`, `…HandOffNeedsYes`, `…OutsideTheServiceRunsItself` |
| Windows not gated | `TestDetacherForGatesWindowsAndScriptBackends` |
| the command's arguments dropped | `TestUpdateHandsOffFromInsideTheService` |

The restored copy passed. `TestUpdateChecksAndDryRunsNeverHandOff` guards the opposite direction,
a handoff on a run that installs nothing. That decision is the library's (`cli.HandOff.Detach`
"runs before an apply that would install"), so no plant of Magic's code can break it.

**The start-up step, run.** A scratch build:

* `mcremote version` printed the version as before.
* With `SELFUPDATE_HANDOFF_ENV=/nonexistent/handoff.env` it exited 1: `error: handoff environment:
  lstat /nonexistent/handoff.env: no such file or directory`.

So the call runs first, and refuses a bad environment.

**Gates.**

| Gate | Result |
| :--- | :--- |
| the six new tests, `-v` | each `--- PASS` |
| `TestWorkflowPinsMatchGoMod`, `TestReleaseJobsReadTheSpec` | ok |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 43 packages ok, 0 FAIL lines |
| `make vulncheck` | clear; GO-2026-5932 allowlisted, as before |
| `go mod tidy -diff` | no output |
| `make pre-add-check` on the four Go files | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `make markdownlint-docs`, `make check-records` | 0 issues in 55 files; ok |

**Not done in H1.** H2 and H3. The handoff itself is confirmed live in L1, after H3.
