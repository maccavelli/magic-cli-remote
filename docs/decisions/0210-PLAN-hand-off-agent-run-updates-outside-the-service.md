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
* `docs/guides/ops-linux-install.md`, added by H3 Deviation 1;
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

**Files:** this plan (the execution record), and the MADR (status and a note). Also the 0054
MADR, added by L1 Deviation 1. The binary is a build of H3's commit.

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
   * ~~0054's PLAN gets a dated note~~ The 0054 MADR gets a dated note, where the item is (L1
     Deviation 1): the handoff item of "What stays deliberately unadopted" is adopted by 0210,
     except on Windows and the script supervisors.
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

H1 was committed by the owner as `6f944fbd`, who approved H2 ("committed, proceed").

### H2, 2026-10-23: the script backends fail safe

**What changed.**

* **`internal/cli/service/scriptbackend.go`:**
  * `supervisedPID` is the service's own PID, or 0 when it is not running. For runit and s6 it is
    the existing sampler's `pid` when the service is up.
  * For OpenRC it reads `<name>.pid` from the directory that `"${XDG_RUNTIME_DIR:-/tmp}"`
    (`supervisor.go:138`) names in the environment `Start` gives the service. It tries
    `$XDG_RUNTIME_DIR`, then `userRuntimeDir()` (the value `Start` falls back to), then `/tmp`.
  * `Inside(ctx)` walks this process's ancestors through the seam `parentOf`, up to PID 1, and is
    true when the walk meets the supervised PID. The walk is bounded at 4096 steps.
  * An error reading a parent is returned, never read as "outside". With no supervised PID, or no
    parent reader, it is false.
  * `Stop` calls `Inside` first. Inside, it returns `<name>: <ErrInsideService>`; on an error it
    returns `<name>: cannot tell whether this process runs inside it: <err>`. Both happen before
    `sv`, `s6-svc` or `rc-service` runs.
* **`scriptbackend_inside_linux.go`:** `procParent` reads `/proc/<pid>/stat` after the command
  name's last `)`, and returns the parent, field 4.
* **`scriptbackend_inside_other.go`** (`!linux`): `procParent` is nil, so `Inside` is false, as
  step 2 says.
  * A first draft returned `errors.ErrUnsupported` instead, and so made `Inside` an error off Linux.
    The existing `TestUpdateRestartsARunitService` (`internal/updateclient`), which drives a fake
    runit service on macOS, failed with `cannot tell whether this process runs inside it: read the
    parent of process <pid>: unsupported operation`.
  * The code was brought back to the plan's text, so this is not a deviation. No file outside
    H2's list changed.
* **`scriptbackend_unix_test.go`:**
  * Helpers: `withAncestry` (a fake `parentOf`), `supervise` (fake `sv` or `s6-svstat` output, or
    an OpenRC pidfile under a `t.Setenv` `XDG_RUNTIME_DIR`), and `stopCalls`.
  * New tests: `TestScriptBackendStopRefusesFromInside` (runit, s6, OpenRC),
    `…StopsFromOutside`, `…StopWhenNotRunning`, `…StopWhenAncestryIsUnreadable`, and
    `TestProcParentReadsTheRealParent`. The last runs on Linux only. It checks this process and a
    `sleep` child it starts. A child is needed because under `go test` a process's parent and its
    process group can be the same number, so a check of the process alone cannot tell field 4
    from field 5.
  * The existing `TestScriptBackendStop` gains `withAncestry`. That makes it independent of the
    real process tree, whose ancestry its fake PID could otherwise meet.

**Seen failing first.** The plants were run on a scratch copy of the tree on macOS
(`h2_plants.py`), and on a scratch directory on the Linux test host (`h2_linux.py`). Messages are
quoted without their temporary paths.

| Plant | Failed, with |
| :--- | :--- |
| no guard (today's `Stop`: `if inside && false`) | `…StopRefusesFromInside`, all three kinds: `Stop = mcremote is still running 15s after it was stopped, want ErrInsideService` |
| an ancestry error read as outside (`_ = err`) | `…StopWhenAncestryIsUnreadable`: `a stop that could not tell inside from outside still ran [sv down …]` |
| the walk skipped (`_, p = parent, 1`) | `…StopRefusesFromInside`, all three kinds, as the first row |
| OpenRC's pidfile not read (`if true {` in `supervisedPID`) | `…StopRefusesFromInside/openrc-user` only |
| Linux: field 5, the process group, read as the parent | `TestProcParentReadsTheRealParent`: `procParent(child) = <go's pid>, <nil>; want <test's pid>` |
| Linux: field 3, the state, read as the parent | `TestProcParentReadsTheRealParent`: `strconv.Atoi: parsing "R": invalid syntax` |

Each restored copy passed.

* The first process-group plant passed, because it was checked on this process alone. The test
  gained the child, and the plant was run again.
* Two earlier plant runs failed only in the harness: one on a `grep` that read `[2]` as a
  character class, and one on a plant that did not compile. Each was rerun after the harness was
  fixed, and the verdicts above require the test's own `--- FAIL` line.

**On the Linux test host** (Linux x86_64, go1.27.3 linux/amd64), the working tree's package, in a
scratch directory that was deleted afterwards: `go vet`, `go test -count=1 -v` and
`go test -race` on `./internal/cli/service/`, each exit 0. Nothing on the host's real service was
touched.

**Gates.**

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the four Go files | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 0 FAIL lines |
| `go mod tidy -diff` | no output |
| `make check-records` | ok |

**Not done in H2.** H3, which needs the owner's word. The guard is confirmed live in L1, in the
runit, s6 and OpenRC containers.

H2 was committed by the owner as `1bfab31b`, who approved H3 ("committed, proceed").

### H3 Deviation 1, 2026-10-23 — the Linux guide joins H3's files. The owner decided: "committed, proceed"

* **Found.** H1 removed a clause from `ops-linux-install.md`'s "Updating from a phone session"
  paragraph, which said that `mcremote doctor` shows a handoff's outcome, because that was H3's
  work (H1's record). H3's files did not name the guide, so putting the clause back needed it.
* **Decided.** H2's handoff said the guide would be added to H3's files as a deviation, and the
  owner approved H3 with "committed, proceed". The guide is added to H3's files, for that one
  sentence. No other guide changes: Windows and the script supervisors do not hand off, and
  macOS has no install guide. Its update is covered by `README.md`'s sentence from H1.
* **MADR.** Unchanged: D5 already says that `doctor` reports the last handoff.

### H3, 2026-10-23: reporting

**What changed.**

* **`internal/cli/doctor.go`:**
  * `renderHandOffDoctor(w, res, err)` prints `self-update handoff: none` for `fs.ErrNotExist`. For
    a read error it prints `could not be read: <err>`. For a result it prints the ID, the start and
    finish times (RFC 3339), the exit code, and the error when there is one.
  * `doctor` calls it after the silent-commands section, with
    `libservice.ReadHandOffResult(libservice.DefaultResultPath(exe))`, where `exe` is
    `os.Executable()`.
  * The `Long` text names the section.
* **`internal/daemon/daemon_handoff.go`** (new):
  * `cleanupHandOffs(ctx, goos, newBackend, log)` returns at once off `darwin`, without building
    a backend. On macOS it builds one, asserts the local interface `handOffCleaner`
    (`CleanupHandOffs(ctx) error`), and calls it once. It logs a failure, to build the backend or
    to clean up, at WARN, and returns nothing.
  * `newHandOffBackend` is `service.NewBackend("mcremote", "")`.
  * Against step 1's sketch, the function takes the daemon's logger, since it logs, and
    `newBackend` returns `any`. That lets the test's fakes stand in without implementing the whole
    `service.Backend`.
* **`internal/daemon/daemon.go`:** after `READY=1`, a goroutine runs `cleanupHandOffs` with a 10 s
  timeout on the daemon's context. It recovers a panic, as the daemon's other goroutines do.
  Start-up never waits on `launchctl`.
  * The library keeps handoff jobs under `$HOME` (`launchd/detach.go:70-82`), and the daemon
    package's `TestMain` already isolates `HOME`. So the tests that call the real `Run` find no
    job, and never reach `launchctl`.
* **Tests:**
  * `TestRenderHandOffDoctor` covers none, unreadable, a success (no error line), and a failure.
  * In `daemon_handoff_test.go`, `TestCleanupHandOffsOnMacOS`, `…OnlyOnMacOS`, `…LogsAFailure` and
    `…SkipsAnotherBackend` use a fake cleaner.
  * `TestHandOffBackendCleansUpOnMacOS` (darwin only) asserts that the backend the daemon really
    builds is a `handOffCleaner`. A renamed library method would otherwise turn the cleanup into
    a silent no-op.
* **`docs/guides/ops-linux-install.md`** (Deviation 1): the "Updating from a phone session"
  paragraph says again that `mcremote doctor` shows the handoff's outcome.

**Seen failing first**, on a scratch copy of the tree (`h3_plants.py`):

| Plant | Failed, with |
| :--- | :--- |
| `renderHandOffDoctor` renders nothing | `TestRenderHandOffDoctor`: `no result: ""` |
| no result read as an error | `TestRenderHandOffDoctor`: `no result: "self-update handoff (…)\n  could not be read: file does not exist\n"` |
| a result's error not shown | `TestRenderHandOffDoctor`: `failure missing "error:     health check failed"` |
| the cleanup never called | `TestCleanupHandOffsOnMacOS`: `built 1, cleanup calls 0, logged ""; want 1, 1, nothing`; `…LogsAFailure` |
| no macOS gate | `TestCleanupHandOffsOnlyOnMacOS`: `linux: built 1, cleanup calls 1; want none` |
| a cleanup failure dropped | `TestCleanupHandOffsLogsAFailure`: `cleanup failure logged ""` |
| the interface no longer matches launchd's (an extra method) | `TestHandOffBackendCleansUpOnMacOS`: `the macOS backend *launchd.Job has no CleanupHandOffs`; `…OnMacOS`; `…LogsAFailure` |

Each restored copy passed.

**`doctor`, run.** A scratch build of the working tree, with its result file beside it:

* no file: `self-update handoff: none`;
* a planted failed result: the ID, both times, `exit code: 1` and `error:     planted: health
  check failed`;
* a malformed file: `could not be read: selfupdate: service: malformed handoff result: invalid
  character 'n' …`.

`doctor` exited 0 each time. The file was removed afterwards.

**Gates.**

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the five Go files | "5 file(s) clean (gofmt, golint, govulncheck)" |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 0 FAIL lines |
| `go mod tidy -diff` | no output |
| `make markdownlint-docs`, `make check-records` | 0 issues in 55 files; ok |

**Not done in H3.** L1, which needs the owner's word for each host run. It is the first time the
handoff runs end to end.

H3 was committed by the owner as `07bc7661`, who approved L1 and its host runs ("committed,
proceed. approved for host testing").

### L1 Deviation 1, 2026-10-23 — the note on 0054 goes in its MADR. The owner decided: "0054 MADR"

* **Found.** Step 5 said "0054's PLAN gets a dated note" on the handoff item of "What stays
  deliberately unadopted". That item is in the 0054 MADR: in its 2026-10-13 amendment's list, and
  in the original "More Information" summary. The 0054 PLAN does not carry it. L1's files named
  neither 0054 record.
* **Decided.** The note goes beside the item, in the 0054 MADR, at both places. The 0054 MADR
  joins L1's files. Step 5's bullet is struck and corrected above.
* **MADR.** 0210's "What this record changes in others" says the note is there.

### L1, 2026-10-23: confirmed on every target

The kit was rebuilt from `07bc7661` (`build_kit.py`). Every update was `mcremote update --yes
--force --version v0.22.2`, run through a scratch daemon's terminal at the probe agent's `RUN`
prompt (R1 Deviation 2). On Windows the daemon was the real task's, with the procedure of R1
Deviation 3 (W1 to W4).

| Target | The agent saw | Daemon | Binary |
| :--- | :--- | :--- | :--- |
| macOS, launchd | `probeagent: exit 0`, `update handed off: 2aaf2495… (launchd job com.magiccliremote.mcremote-d3probe.selfupdate.2aaf2495…)` | PID 68693 → 68789, up | `dev (local)` → `v0.22.2 (release)` |
| Linux, systemd 259 | `probeagent: exit 0`, `update handed off: 1467c607… (transient unit mcremote-d3probe-selfupdate-1467c607….service)` | PID 1866277 → 1866380, up | `dev (local)` → `v0.22.2 (release)` |
| Windows, the real task | `probeagent: exit 1`, `update failed: … this process runs inside the service it would stop: \mcremote` | PID 43212 throughout the update, `Running` | the installed binary and the update binary each unchanged |
| runit (Void) | `probeagent: exit 1`, `update failed: … managed install failed mcremote-d3probe: … this process runs inside the service it would stop` | PID 19 before and after, `/healthz` up | unchanged (`6f6d4dc2…`, the kit build) |
| s6 (Alpine 3.22) | as runit | PID 17; `up (pid 17 pgid 17) 10 seconds` | unchanged |
| OpenRC 0.62.6 (Alpine 3.22) | as runit | PID 150; `* status: started` | unchanged |

**Against the plan's expectations.**

* **L1.1 and L1.2, the handoff.** Each printed `update handed off`, and the agent's command exited
  0. The result file, read with `cat` since the swapped `v0.22.2` has no `doctor` section (step
  1), held `"exit_code":0`, `"applied":true`, `"service_was_running":true`,
  `"service_started":true` and `"rolled_back":false`. The installed digest equalled the release
  digest. The detached run took 14 s on macOS and 11 s on Linux.
* **L1.1, the transient unit.** It had finished, and was no longer listed, when the result
  appeared. Its journal shows the run: `Started mcremote-d3probe-selfupdate-1467c607….service -
  selfupdate handoff for mcremote-d3probe.service`, the release's resolve, download and
  verification, `mcremote: updated dev -> v0.22.2`, and `Consumed 1.046s CPU time over 11.423s
  wall clock time`. `systemctl --version` is 259, above `Detach`'s 236.
* **L1.2, the cleanup.** After the handoff, the one-shot job was still loaded, `state = not
  running`, and its plist was in `~/Library/Application Support/selfupdate/handoff`. The scratch
  binary was set back to this change's build, and the scratch job restarted (`launchctl
  kickstart -k`, exit 0). Then `launchctl print` of the handoff job said `Could not find service
  … in domain for user gui: <uid>`, and its files were gone. That is `CleanupHandOffs`, run by
  H3's start-up step.
* **L1.3, Windows.** The update binary was the kit's build of H3's commit, run against the real
  config, as L1's binary rule says. The installed release, `v0.22.2`, predates H1. With
  `HandOff` unset on Windows, the update failed safe, as in R1. W4 then:
  * revoked the temporary device;
  * restored the config byte for byte (`348495ea…`);
  * found the paired devices as in W1;
  * restarted the task (`Running`, a new PID).
* **L1.4, the script supervisors.** Each failed safe with the service up and the same PID. That
  is the fail-safe R1 lacked: R1 left all three down. runit's `sv status` printed nothing, as in
  R1, so its state is read from the process table and `/healthz`.
* **Not run: H3's `doctor` against a real handoff's result.** Step 1 allowed reading the file
  directly. H3's `doctor` was run on the same file format with planted results, in H3's record.

**Observations, not failures:**

* **`NOTIFY_SOCKET` reaches the detached run.** The Linux journal has
  `mcremote-d3probe-selfupdate-1467c607….service: Got notification message from PID 1866372, but
  reception is disabled`. The library's environment file copies the inside process's whole
  environment (`systemd/detach.go:209`, `os.Environ()`). That includes the daemon's
  `NOTIFY_SOCKET` from its `Type=notify` unit, so a process in the transient unit sent an
  `sd_notify` message, which systemd dropped. Which process it was is not known. It did no harm
  here, and is a candidate for a library issue.
* **`setup-service --remove` leaves the refresh's backup.** On Linux,
  `~/.config/systemd/user/mcremote-d3probe.service.prev` remained after the remove. It was written
  at 15:05, during the handoff, by the post-update definition refresh (`refresh.go:239`). It was
  deleted by hand. macOS left none. This is behaviour from before 0210, recorded for a later
  record.
* **The script supervisors' update downloads before it refuses.** Each transcript shows
  `installing … target=v0.22.2` before the stop was refused. The managed installer stages the new
  binary first, and nothing was swapped.

**What each run left behind.** Nothing:

* the scratch services were removed (`setup-service --remove` exit 0 on macOS and Linux); the
  scratch label and unit were gone, and `~/mcr-d3probe` and `~/mcr-d3kit` were deleted on each
  host;
* the library's empty `~/Library/Application Support/selfupdate/handoff` stays, as a real handoff
  would leave it;
* the Windows backup `config.yaml.d3bak` was deleted after W4's checks passed;
* each container was removed, and the Podman machine was stopped.

The real services were unchanged:

* macOS PID 67479 before and after;
* Linux `MainPID` 1356 before and after;
* the Windows task `Running` throughout, with its restarts from W2 and W4 only.

**The kit's changes for L1** (scratchpad only):

* after an inside update that hands off, `d3_run.py` waits for the result file and the restarted
  daemon, and on systemd prints the transient unit's state and journal;
* a `cleanup-check` phase runs L1.2's cleanup;
* `d3_windows.py` runs the kit's update binary and records both binaries' hashes.

**The plan is complete.** Every acceptance criterion under Verification is met. The MADR is
accepted for D1, D2 and D4 to D7, and D3 stays open on go-selfupdate-lib#6. A later amendment,
when #6 or #2 ships, opens a new phase for the Windows or the script-supervisor handoff.

L1 was committed as `907c3c29`, at the owner's word ("Commit to master").

## Amendment — 2026-10-23 (A): L1's three findings

~~**Status: proposed.**~~ **Approved by the owner on 2026-10-23 ("proceed"); in progress.** This
implements the MADR's "Amendment — 2026-10-23 (A)": D8 (F7), D9 (F6)
and D10 (F5). The phases above stay complete. The front matter's `status` became `in-progress`
on that approval. With the owner's answers of 2026-10-23 ("5C: both",
"Skip R2"), the order is A0, A1, A2, A3, then L2, and ~~R2~~ does not run. The rules for every phase, above, apply
unchanged: files named at the start, every new test seen failing first on a scratch copy, the
gates, the guard's redact preview, and each phase staged for the owner.

### ~~R2 — Who sent the notification (only on question 4)~~ Not run: the owner chose "Skip R2"

**Files:** this plan. On the Linux test host, in a scratch directory:

1. A Python listener binds a `SOCK_DGRAM` socket at a scratch path, and prints each datagram
   with the sender's PID (`SO_PASSCRED`).
2. With `NOTIFY_SOCKET` set to that path, it runs:
   * `systemctl --user daemon-reload`;
   * `systemctl --user show-environment`, a read-only control;
   * the kit's `mcremote setup-service --refresh --json`, against a scratch unit that L2's
     install leaves in place.
3. The record names the sender, or says that none sent anything.

### A0 — The library issue (only on question 1: 5B or 5C)

**Files:** this plan. One issue in go-selfupdate-lib:

* **Title:** "Detach copies the caller's per-unit service-manager variables into the detached
  run".
* **Body:** F5's evidence, without host names. Which variables to leave out (the MADR's 5B list).
  `systemd/detach.go:209` and `launchd/detach.go:100`.
* The record carries the issue's URL. Nothing waits on it.

### A1 — Refuse before the download (D8)

**Files:** `internal/updateclient/client.go`, `command_test.go`; this plan.

1. **Tests first**, in `command_test.go`:
   * A backend that is no `Detacher` but answers `Inside` true: the update fails with
     `ErrInsideService`, and the fake updater records no run.
   * The same, answering false: the update runs as today.
   * An `Inside` error is returned.
   * `--check` and `--dry-run` never call `Inside`.
   * On a faked Windows, the refusal is set even though the backend is a `Detacher`.
   * The existing `TestDetacherForGatesWindowsAndScriptBackends` and H1's "no handoff" test are
     updated, since those backends now get the refusal.
2. **`client.go`:**
   * A seam beside `newCommandDetacher`, `newCommandInsider func(product string, health
     service.HealthOptions) (insider, bool)`, where `insider` is `interface{ Inside(ctx) (bool,
     error) }`. It returns the backend when it answers `Inside` and `detacherFor` reports false.
   * In `RunE`, when no handoff is set and that seam reports true, `o.HandOff.Detach` is
     `refuseInside(b)`. That function returns `fmt.Errorf("this update runs inside the %s service
     and cannot hand off here; run it from a shell: %w", product, libservice.ErrInsideService)`
     when inside, and the error itself when `Inside` fails.
   * The `Long` help's sentence about refusing becomes "it refuses before downloading".
3. **Gates.**

### A2 — Remove deletes the backup (D9)

**Files:**

* `internal/cli/service/setup.go`, `supervisor.go`, `setup_schtasks.go`, `refresh_schtasks.go`,
  `uninstall.go`;
* `internal/cli/setup_service.go`;
* the tests `setup_test.go`, `setup_launchd_test.go`, `supervisor_unix_test.go`,
  `schtasks_test.go`, `uninstall_unix_test.go`;
* `docs/guides/ops-linux-install.md`, `docs/guides/ops-windows-install.md`;
* this plan.

1. **Tests first:**
   * For systemd, launchd, OpenRC and Windows: a definition and its backup in place; `Remove`
     leaves neither, and `Result.RemovedBackup` names the backup.
   * Without a backup, no error, and no name.
   * For runit and s6, as today: the directory and its `run.prev` go.
   * On Windows, the remove creates no state directory.
   * `--uninstall`'s result lists the backup.
2. **Code:**
   * `Result.RemovedBackup string`.
   * Each remover deletes its backup with `os.Remove`, after the definition, and ignores
     `fs.ErrNotExist`.
   * `refresh_schtasks.go` splits `taskBackupPath`, so that a lookup does not create the state
     directory.
   * `setup_service.go` prints `Removed backup:     <path>`, and `uninstall.go` adds it to the
     removed items.
3. **Guides:** `ops-linux-install.md`, beside the refresh table's `<unit>.prev` row (line 306), and
   `ops-windows-install.md`'s uninstall section (line 110). Each says that `--remove` and
   `--uninstall` delete the kept definition too.
4. **Gates.**

### A3 — The daemon keeps its notify socket (D10)

**Files:** `internal/daemon/daemon.go`, a new `internal/daemon/daemon_notify.go`,
`notify_unix_test.go`; this plan.

1. **Tests first**, in `notify_unix_test.go`, with a `unixgram` listener:
   * After `Run` is ready, `os.Getenv("NOTIFY_SOCKET")` is empty, and `READY=1` arrived.
   * A child that the test's fake provider spawns after start-up prints its environment, and it
     has no `NOTIFY_SOCKET`.
   * On shutdown, `STOPPING=1` arrives.
   * An abstract `@` address is accepted.
   * `TestRunNotifiesSystemd` passes unchanged.
2. **`daemon_notify.go`:**
   * `takeNotifySocket()` reads the variable, unsets it, and returns a sender. With no variable,
     the sender reports `false, nil`, as the library does.
   * The sender writes one datagram, by the library's rules (`service/systemd/notify.go`): a path,
     or `@` for the abstract namespace.
3. **`daemon.go`:**
   * `Run` calls `takeNotifySocket` before any provider or terminal is started.
   * The `READY=1` and `STOPPING=1` call sites pass its sender to `notifySystemd`. That keeps
     `notifySystemd`'s signature, and drops the import of the library's `systemd` package if
     nothing else uses it.
4. **Gates,** with the Linux test host running `go test -race ./internal/daemon/`, since the
   socket tests are Unix-only.

### L2 — Confirm on each host (on the owner's word)

**Files:** this plan, and the MADR (the amendment's status and a note). The binary is a build of
A3's commit. The kit is R0's, under the rules R1's deviations set.

1. **Linux, systemd.** `install`, then `inside-agent`. Expected:
   * the handoff completes, as in L1.1;
   * the transient unit's journal has no `reception is disabled` line;
   * the scratch daemon became active, so `READY=1` still arrives.

   Then `remove`. Expected: no `mcremote-d3probe.service.prev` remains, and the output names it.
2. **The three containers.** `install`, then `inside-agent`. Expected:
   * refused with the D8 message;
   * no `downloading-manifest` or `installing` event in the transcript;
   * the PID unchanged.
3. ~~**Windows.** W1 to W4 with the kit's update binary, as L1.3. Expected: the D8 refusal with no
   download event, and W4's restore.~~ *Replaced by amendment B's B2 (proposed 2026-10-23): with
   the Windows handoff on, L2.3 expects a handoff, by a procedure B2 writes and the owner
   approves.*
4. **macOS.** `install`, `inside-agent`, `cleanup-check`, then `remove`, as L1.2. Expected: as in
   L1.2, unchanged by A1 to A3.
5. **Records.**
   * L2's execution record.
   * The MADR amendment becomes `decided`, with a dated note.
   * This plan becomes `complete` again.

### Rollout and rollback for amendment A

* **Rollout.** At the next tag, with `make ci-windows-smoke` first.
* **Rollback.** Revert A1 to A3's commits. Then:
  * F7 comes back: a refused update downloads first, and still leaves the service running.
  * F6 comes back: backups stay after a remove.
  * F5 comes back: a harmless journal line.

  Nothing is migrated, so nothing has to be undone on a host.

### Execution record for amendment A

#### A0, 2026-10-23: the library issue

Filed as [go-selfupdate-lib#7](https://github.com/maccavelli/go-selfupdate-lib/issues/7), label
`enhancement`, on the owner's "5C: both" and "proceed".

* **Title:** "service: Detach copies the caller's per-unit service-manager variables into the
  detached run". The `service:` prefix follows the library's issue titles, such as #6; the
  wording is otherwise A0's.
* **Body:** the request and the 5B list; F5's journal line with the unit, ID and PIDs as
  placeholders; the sender as "most likely `systemctl --user daemon-reload`", not traced further
  ("Skip R2"); and the cited lines, checked against the module cache at `v1.17.2`.
* **Added while checking the citations.** Two more places pass `os.Environ()` on, and the body
  names them: `selfupdate/service/execreconciler.go:176`, the reconcile command, and
  `selfupdate/service/detach.go:30`, `DetachProcess`. The reconcile command's line is how the
  refresh, and its children, inherit the variable inside the transient unit. Nothing in Magic's
  phases changes because of them.
* **Before filing,** the disclosure guard's redact preview ran over the body in a scratch
  repository: "would edit 0 file(s)".
* Nothing waits on it.

#### A1, 2026-10-23: refuse before the download

**What changed.**

* **`internal/updateclient/client.go`:**
  * `insider` is `interface{ Inside(ctx context.Context) (bool, error) }`.
  * `insiderFor(goos, backend)` returns the backend as an `insider` only where `detacherFor`
    reports false: on Windows, and for a backend that is not a `Detacher`.
  * The seam `newCommandInsider(product, health) (insider, bool)` builds `newBackend(product, "",
    health)` and returns it through `insiderFor(runtime.GOOS, b)`.
  * `refuseInside(product, b)` is a `cli.HandOff.Detach`. Inside, it returns `this update runs
    inside the <product> service and cannot hand off here; run it from a shell: %w` around
    `libservice.ErrInsideService`. Outside, it returns `false, "", nil`. An `Inside` error is
    returned as it is.
  * In `RunE`, where `newCommandDetacher` reports false and `newCommandInsider` reports true,
    `o.HandOff.Detach` is `refuseInside(product, b)`. `Report` stays unset there.
  * The `Long` help now says that on Windows, runit, s6 and OpenRC the update "refuses before
    downloading and leaves the service running; run it from a shell there".
* **`internal/updateclient/command_test.go`:**
  * `runCommandContext` sets `newCommandInsider` to `commandTestInsider`, or to none, as it does
    for the detacher. It also counts `downloads`, the fake GitHub's requests to the asset host.
  * Four tests are new: `TestUpdateRefusesInsideBeforeDownloading`,
    `TestUpdateOutsideRunsWhereNothingHandsOff`, `TestUpdateFailsWhenInsideCannotBeTold` and
    `TestUpdateChecksAndDryRunsNeverAskInside`. They use a `fakeInsider` that counts its calls.
  * `TestDetacherForGatesWindowsAndScriptBackends` gains a second column, the refusal. A Detacher
    on Windows now gets the refusal. H1's "no handoff" case was that table's last row, "a backend
    that is not a Detacher". It is now two rows: "a script backend", which gets the refusal, and
    "a backend that answers neither", which gets nothing.
* **Guides, unchanged.** They still read true. `ops-linux-install.md` says the update "refuses
  from inside the service and leaves it running, so update from a shell instead".
  `ops-windows-install.md` quotes "this process runs inside the service it would stop", which
  still ends the new message, since that message wraps the library's error.

**Seen failing first**, on a scratch copy of the tree (`a1_plants.py`). The restored copy passed.

| Plant in `client.go` | Failed, with |
| :--- | :--- |
| never refuses (`if false && in`) | `…RefusesInsideBeforeDownloading`: "exit status 0, want 1" |
| an `Inside` error read as outside | `…FailsWhenInsideCannotBeTold`: "exit status 0", "the failed update downloaded 2 asset(s)", "changed the executable" |
| the refusal not wired (`_ = b`) | `…RefusesInsideBeforeDownloading`, `…OutsideRunsWhereNothingHandsOff` ("Inside was never asked"), `…FailsWhenInsideCannotBeTold` |
| `Inside` asked before the library decides | `…ChecksAndDryRunsNeverAskInside`: "[--check]: Inside calls 1", "[--dry-run --yes]: Inside calls 1" |
| a Detacher refused too | `TestDetacherForGatesWindowsAndScriptBackends`: "a Detacher on Linux: insiderFor = true, want false", and on macOS |
| the remedy dropped from the message | `…RefusesInsideBeforeDownloading`: the last line without "; run it from a shell" |
| not `ErrInsideService` (`%v`, "inside") | `…RefusesInsideBeforeDownloading`: the last line ending "run it from a shell: inside" |

The "downloaded 2 asset(s)" line shows that the download counter can fail: without the refusal,
the run made two requests to the asset host.

**Gates.**

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the two Go files | "2 file(s) clean (gofmt, golint, govulncheck)" |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 43 packages ok, 0 FAIL lines |
| `go mod tidy -diff` | no output |
| `make markdownlint-docs`, `make check-records` | 0 issues in 55 files; exit 0 |

The docs gates ran over a tree that also holds another session's unstaged changes:
`.markdownlint-cli2.jsonc`, `docs/README.md` and the untracked 0211 pair. A1 does not touch or
stage them.

**Not done in A1.** A2 and A3. The refusal is confirmed live in L2.

A1 was committed by the owner as `250b59bb`.

#### A2 Deviation 1, 2026-10-23 — mcrelay prints its own `--remove` result. The owner decided: "Shared printer (Recommended)"

* **Found** before any edit, by `git grep` for the printers of a removal result. A2 names
  `internal/cli/setup_service.go` (`:164`). mcrelay prints the same lines from its own copy,
  `internal/relay/cli.go:488-489`, which A2 did not name. The two differ only in "The daemon" and
  "The relay". The copy predates 0210.
* **Why it matters.** mcrelay's backups are the likeliest to hold a secret given with `--env`,
  and D9 says the output names the backup it deleted.
* **Options put to the owner:** one shared printer that both commands call; one more line in
  mcrelay's copy; or mcremote only.
* **Decided:** one `service.PrintRemoveResult(w, res, what)`, in `result_print.go`, which prints
  `Removed:` and, when set, `Removed backup:`, then "The <what> was stopped and the service
  disabled." Both commands call it.
* **Files added to A2:** `internal/relay/cli.go`, `internal/cli/service/result_print.go`,
  `internal/cli/service/result_print_test.go`.

#### A2 Deviation 2, 2026-10-23 — tests would delete the Windows host's real backup. The owner decided: "TestMain, package-wide (Recommended)"

* **Found** while writing the Windows remover, before any test ran. `removeSchtasks` now deletes
  `<state>\<task>-task.prev.xml`, found through `taskBackupFile`. Two existing tests reach it
  without a stub: `TestRemoveSchtasksIdempotent` (`schtasks_test.go:182`) and
  `TestUninstallOnWindows` (`uninstall_test.go:36`). On the Windows test host the lookup is the
  account's real LocalAppData, so `make ci-windows` would delete the real task's refresh backup.
  On macOS and Linux it would try `~/.local/state`.
* **Options put to the owner:** point the lookup at a scratch directory for the whole test binary,
  in the package's `TestMain`; or stub it in each test that reaches it.
* **Decided:** `TestMain` (`unitrecord_test.go`), which already points the unit-name record at a
  scratch directory so that "no test may write it into the developer's real state directory",
  also points `taskBackupFile` there. The real lookup is kept as `realTaskBackupFile`, for the one
  test that checks it creates nothing.
* **File added to A2:** `internal/cli/service/unitrecord_test.go`.

#### A2, 2026-10-23: removing a service removes its backup

**What changed.**

* **`internal/cli/service/setup.go`:**
  * `Result.RemovedBackup`, the backup a remove deleted, or empty.
  * `removeBackup(path)`: `os.Remove`; a missing file is `""` and no error; any other failure is
    an error, "remove the kept definition: …".
  * `removeSystemd` deletes `<unit>.service.prev` after the unit and its enable link, before
    `daemon-reload`. `removeLaunchdAgent` deletes `<label>.plist.prev` after the plist.
* **`supervisor.go`:** `removeScript` deletes OpenRC's `<name>.prev`, kept beside `init.d`. runit
  and s6 are unchanged: their `run.prev` goes with the service directory, and is not named.
* **`refresh_schtasks.go`:** `taskBackupPath` is split. `taskBackupFile` resolves
  `<state>\<task>-task.prev.xml` and creates nothing; `taskBackupPath` calls it, then makes the
  state directory, as before, for a refresh.
* **`setup_schtasks.go`:** `removeSchtasks` deletes the backup through `taskBackupFile`. It does
  so even when the task was already gone, which is a step beyond A2's text: a backup outlives a
  task deleted by hand, and D9's reason, an `--env` secret, holds for it too.
* **`uninstall.go`:** `UninstallItem.Backup`; `removeDefinition` returns the backup;
  `PrintUninstallResult` adds `removed: <product> <kind> backup <path>`.
* **`result_print.go`:** `PrintRemoveResult(out, res, what)`, per Deviation 1, called by
  `internal/cli/setup_service.go` ("daemon") and `internal/relay/cli.go` ("relay"). It prints
  `Removed backup:    <path>`, aligned with `Removed:`, where A2's text had one more space.
* **`unitrecord_test.go`:** per Deviation 2.
* **Guides:**
  * `ops-linux-install.md`, after the refresh table: the kept `<unit>.prev` can hold `--env`
    values, and `--remove` and `--uninstall` delete it and print its path.
  * `ops-windows-install.md`, the uninstall section: `setup-service --uninstall` also removes the
    task definition a refresh kept under `%LocalAppData%\<product>\State`, as `--remove` does.
* **Tests.**
  * New: `TestRemoveDeletesTheUnitBackup` (pinned to systemd);
    `TestRemoveSchtasksDeletesTheBackup`, `…WithNoBackup`, `…AbsentTaskDropsItsBackup`;
    `TestTaskBackupFileCreatesNothing` (Unix; on Windows the seam test covers it);
    `TestPrintRemoveResult`; `TestUninstallRemovesTheDefinitionsBackup`.
  * Extended: `TestRemove` (no backup, none named), `TestDarwinRemoveAgent` and `TestRemoveScript`
    (a backup in place, gone after).

**Seen failing first,** on a scratch copy of the tree (`a2_plants.py`). The restored copy passed.

| Plant | Failed, with |
| :--- | :--- |
| systemd keeps its backup | `TestRemoveDeletesTheUnitBackup`: "…/units/mcremote-rm.service.prev survived Remove"; `TestUninstallRemovesTheDefinitionsBackup`: "want one item naming the backup" |
| launchd keeps its backup | `TestDarwinRemoveAgent`: "…/com.magiccliremote.mcremote.plist.prev survived Remove" |
| OpenRC keeps its backup | `TestRemoveScript/openrc-user`: "…/rc/mcremote.prev survived Remove" |
| Windows keeps its backup | `TestRemoveSchtasksDeletesTheBackup`: "…/mcremote-task.prev.xml survived the remove"; `…AbsentTaskDropsItsBackup` |
| Windows looks up through `taskBackupPath` | three `TestRemoveSchtasks…` tests: "the remove looked the backup up through taskBackupPath, which creates the state directory" |
| the lookup creates the state directory | `TestTaskBackupFileCreatesNothing`: "the lookup created …/state" |
| a missing backup is an error | `TestRemove`, `TestRemoveSchtasksIdempotent`, `…WithNoBackup`: "remove the kept definition: … no such file or directory" |
| a missing backup is named | `TestRemove`: "RemovedBackup = "…/com.magiccliremote.mcremote-rm.plist.prev" with no backup to remove"; `…WithNoBackup` |
| the printer drops the backup | `TestPrintRemoveResult`: "with a backup:" |
| the uninstall printout drops it | `TestUninstallRemovesTheDefinitionsBackup`: "output lacks "removed: mcremote systemd-user backup …"" |
| the uninstall forgets it | `TestUninstallRemovesTheDefinitionsBackup`: "want one item naming the backup" |

* **The last plant first failed to compile,** since `return "", err` left `res` unused. It was
  rewritten as `return res.RemovedBackup[:0], err`, and then failed as shown.
* **The new systemd test first failed on this Mac for the wrong reason.** `Remove` picks its
  backend by the host's OS, so on macOS it ran the launchd remover. It now pins Linux
  (`OverrideInstallOS("linux")`, `OverrideRunSystemctl`), as `unitrecord_setup_test.go` does.
* **Not fixed, recorded:** the older `TestRemove` has the same pinning gap. On macOS it removes a
  launchd plist and then checks for a `.service` file that was never written, so there it proves
  less than its name says. It is outside A2.
* **The plant log shows Deviation 2 working:** the Windows remover's lookup resolved to
  `…/service-unit-record-…/state/mcremote-task.prev.xml`, the scratch directory, not real state.

**Gates.**

* **First run:** `make test` and `make race` each failed one test outside the service package,
  `TestRunFixturesAreWarmed` (0193's guard): "internal/cli/service/supervisor_unix_test.go:616
  writes a #!/bin/sh script by hand. Use testexec.WriteShellStub …". It was A2's own fixture: the
  OpenRC backup's content was `#!/bin/sh`. A backup is never run, so its content is now plain text,
  and the guard passes. The OpenRC plant tests the file's existence, not its content, so it holds.
* **Second run:**

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the fifteen Go files | "15 file(s) clean (gofmt, golint, govulncheck)" |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 43 packages ok, 0 FAIL lines |
| `go mod tidy -diff` | no output |
| `make markdownlint-docs`, `make check-records` | exit 0 |

**Not done in A2.** A3. No host run belongs to A2; its outcome on the Linux test host is L2.1's
"no `.prev` remains". `make ci-windows` was not run on the Windows host for A2; Deviation 2's
isolation is what keeps that run off the real backup, and it is first exercised there by L2.

## Amendment — 2026-10-23 (B): the Windows handoff

**Status: proposed.** This implements the MADR's "Amendment — 2026-10-23 (B)", D11. The owner set
its order on 2026-10-23: after
[0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md)'s
U1 (the `v1.19.0` pin) and amendment A's A2 and A3, and before L2. The rules for every phase,
above, apply unchanged.

### B1 — Turn the Windows handoff on (D11)

**Files:** `internal/updateclient/client.go`, `command_test.go`, `internal/daemon/daemon_handoff.go`,
`daemon_handoff_test.go`, `docs/guides/ops-windows-install.md`, `README.md`; this plan.

1. **Tests first:**
   * `TestDetacherForGatesWindowsAndScriptBackends`: a Detacher on Windows now hands off and gets
     no refusal.
   * The daemon's cleanup runs for `goos == "windows"` with a backend that has
     `CleanupHandOffs`, and still not for `linux`.
2. **Code:**
   * `detacherFor` drops its Windows branch, and its comment says why (#6, `v1.19.0`).
   * `cleanupHandOffs` runs on `darwin` and `windows`.
   * The `update` command's `Long` help: Windows hands off; runit, s6 and OpenRC refuse before
     downloading.
3. **Guides:** `ops-windows-install.md`, "Updating from a phone session" (line 249): the update
   hands off to a one-shot Task Scheduler task, and `doctor` shows its outcome. `README.md:117-119`:
   systemd, launchd and Task Scheduler hand off; runit, s6 and OpenRC refuse.
4. **Gates.**

### B2 — The Windows live procedure for L2.3

**Files:** this plan; a new script in the scratch kit (not committed).

1. **Why a new procedure.** L1.3 ran the kit's update binary against the real task, and it was
   refused. A handoff instead stops the task, replaces the binary the detached run started from,
   and runs that binary's `setup-service --refresh`, which renders the task for that binary's path.
   Run from the kit's directory, it would point the real task at the kit's binary.
2. **The procedure** is written so the handed-off update runs as the installed binary, at its
   installed path, on a release that has B1. It is drafted, presented with its restore steps (W1
   to W4's backup of the config, the task definition and the binary), and run only on the owner's
   word. If no published release has B1 yet, the options are put to the owner rather than chosen
   here.
3. **Expected:** `update handed off: …`; the task back up on the target release; `doctor` shows
   the handoff's result; after the daemon's next start, no `mcremote.selfupdate.*` task remains;
   W4 restores the config byte for byte.
