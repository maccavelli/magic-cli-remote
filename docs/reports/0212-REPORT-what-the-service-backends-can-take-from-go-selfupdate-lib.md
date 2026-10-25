<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# What Magic's service backends can take from `go-selfupdate-lib`

Observed 2026-10-24, at `master` `793b8f5b` of this repository, with the uncommitted 0207
amendment C and 0212 records. The library was read at `main` `fff0940`. That is four commits ahead
of its `origin/main`, and its newest tag is `v1.21.0`.

**Scope.** The owner asked for a deep dive into the library's design, decisions, documentation and
code. The aim is to find what can improve the stability, robustness and standards adherence of the
service backends that stay in Magic, using the testing already done. In the owner's words: "deep
dive into the design/decisions/documentation/developed code in the go-selfupdate-lib codebase and
see if we can use any of it here to improve stability, enhance robustness and standards/best
practices adherence, and leverage all of that testing to overhaul and improve the backends here in
magic-cli-remote."

The owner confirmed that the library's adapter still ships ("Adapter still ships"). So Magic keeps,
per supervisor:

* its definitions;
* its state reads;
* its start and stop commands.

The library's `service.NewSupervised` owns the contract. This report decides nothing and changes no
source.

Associated records:

* [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](../decisions/0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
  (proposed). This report feeds its amendment A.
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](../decisions/0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
  whose PLAN's U4 to U6 move setup-service's systemd, launchd and Task Scheduler paths onto the
  library's backends.
* [0210-MADR-hand-off-agent-run-updates-outside-the-service.md](../decisions/0210-MADR-hand-off-agent-run-updates-outside-the-service.md),
  whose PLAN's R1, L1 and L2 ran the supervisors live.
* The library's own records, by file name, in the `go-selfupdate-lib` repository:
  * `0011-MADR-reference-service-lifecycles.md`, the backend contract;
  * `0015-MADR-remediate-third-debugging-pass-findings.md` B3, the stop that outlives the caller's
    context;
  * `0020-MADR-precheck-gofmt-errors-and-replace-before-stop.md`;
  * `0023-MADR-robust-test-fixtures-and-process-waits.md`;
  * `0025-MADR-align-with-go-sdk-design-practices.md` D2, the typed command errors;
  * `0033-MADR-scrub-service-manager-environment-from-handoffs.md`;
  * `0035-MADR-serve-supervisors-through-a-supervised-adapter.md` (accepted 2026-10-24),
    `0035-PLAN-serve-supervisors-through-a-supervised-adapter.md` (proposed), and
    `0035-REPORT-library-backends-or-consumer-api-for-runit-s6-and-openrc.md`.

## Executive assessment

**What "the testing" is, measured.**

* **The library has run no probe of runit, s6 or OpenRC.**
  * Its 0035 records rest on the supervisors' manuals (its F12 to F25) and on a read of Magic's
    code.
  * Its 0035 PLAN designs the probes (phase M1, P1 to P4) and a live s6 test (M6). It is still
    `proposed`, with no execution record.
* **What the library has tested is its four backends.** systemd, launchd, SCM and Task Scheduler
  each have:
  * fakes;
  * captured real output as fixtures;
  * an env-gated live test against the real service manager, in CI.
* **Magic's own live testing of these supervisors is the 0210 PLAN's runs:**
  * R1 reproduced the fault, and L1 and L2 confirmed the fix.
  * They ran in three Podman containers: Void with runit, Alpine 3.22 with s6, and Alpine 3.22 with
    OpenRC 0.62.6.
  * They used a scratch kit: three Containerfiles, the drivers, and a probe agent.
  * **That kit exists only in a session scratchpad.** Nothing in the tree can repeat those runs.

**The findings, most important first.**

1. **The script definitions fall short of the systemd unit and the launchd job (B1).**
   * The unit and the job restart the daemon, give it 45 s to stop before a kill, and keep its
     logs.
   * Of the three script supervisors, none keeps the logs and none kills a daemon that hangs on
     stop. OpenRC does not restart a daemon that crashes either.
   * Each gap has a documented remedy in that supervisor's own manual.
2. **OpenRC can do most of what the unit does, but Magic's script opts out (B2).**
   * The script writes its own `start()` and `stop()`. That disables OpenRC's recommended
     `supervise-daemon`.
   * It also leaves the stop at `start-stop-daemon`'s default, `SIGTERM/5`, which is TERM, a 5 s
     wait, and then no kill.
   * OpenRC 0.62 and later accept `READY=1` on a notify socket, which Magic's daemon already sends.
3. **The stop bound is shorter than the daemon's own drain (B4).** Magic gives the daemon 45 s to
   drain on every other service manager. Its script backend waits 15 s, and the adapter's defaults
   would wait 60 s, with a kill at 30 s.
4. **The supervisor commands do not meet the library's command rules (B5).** They run by bare name
   on `PATH`, with no caller context and no fixed locale. They print to the update's stdout, and
   their errors are plain strings. 0212's stage 1 fixes the first three of those. The locale, the
   typed errors and the English-text matches are not in 0212 yet.
5. **Setup finds tools through `PATH`, but the update will look in fixed directories (B6).** So
   setup could succeed on a host whose update then cannot find the tools.
6. **The testing is not repeatable (B8, B9).**
   * The kit belongs in the tree, as env-gated live tests in the library's pattern.
   * The library's planned probes collect exactly the facts 0212 must otherwise probe for itself.
7. **0212 predates the library's accepted 0035 (B10).** That record now has `Kill` and
   `KillAfter`, `internal/procfs` and a release number.

## How this was done

* **Read in the library:**
  * the package documentation, and `errors.go`, `runner.go`, `poll.go`, `detach.go` and
    `managerenv.go`;
  * the systemd backend (`unit.go`, `lifecycle.go`, `captured_test.go`);
  * `internal/testproc`;
  * the "Run as a service" and "Test a program that self-updates" sections of
    `docs/guides/extending-selfupdate.md`;
  * the records named above, whose decision sections were read in full.
* **Read in Magic:**
  * `internal/cli/service`. A read-only subagent inventoried it: every tool call, wait, error,
    parse, seam and PID comparison. Its load-bearing claims were re-checked here, each cited below
    with file and line.
  * The units, the plist renderer, `internal/procutil`, `internal/certs/acme.go:30`, and the 0210
    PLAN's run records.
* **The supervisors' documentation, fetched 2026-10-24:**
  * OpenRC's `NEWS.md`, `user-guide.md`, `service-script-guide.md`, `supervise-daemon-guide.md`,
    `man/openrc-run.8`, `man/start-stop-daemon.8` and `etc/rc.conf`, on `master`;
  * s6's `servicedir.html`;
  * runit's `sv.8.html` and `runsv.8.html`.
* **Evidence tags:**
  * **[read]:** code or records at the commits above;
  * **[docs]:** a supervisor's documentation, as fetched;
  * **[derived]:** a conclusion from tagged facts;
  * **[unverified]:** not measured. A PLAN probes it first.

## Findings

### B1 — The script definitions fall short of the unit and the job

What each service definition asks of its supervisor:

| Concern | systemd unit [read] | launchd job [read] | runit `run` [read] | s6 `run` [read] | OpenRC script [read] |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Restart after a crash | `Restart=always`, `RestartSec=5` (`mcremote.user.service.tmpl:36-37`) | `KeepAlive`, `ThrottleInterval` 2 (`plist_render.go:170-171`) | runsv restarts `./run` (runsv(8)) [docs] | s6-supervise restarts it [docs] | **none.** `start-stop-daemon` "doesn't provide any process supervision" (openrc-run(8)) [docs]; the 0210 runs saw "crashed" stay crashed |
| Crash-loop throttle | `StartLimitIntervalSec=300`, `StartLimitBurst=30` (`:17-18`) | `ThrottleInterval` | 1 s between quick restarts (runsv(8)) [docs] | 1 s minimum (library 0035 F19) | n/a |
| Stop, then kill | `TimeoutStopSec=45`, then SIGKILL (`:38`) | `ExitTimeOut` 45 (`plist_render.go:172`) | **`sv down` sends TERM, never KILL** (sv(8)) [docs] | **never** without a `timeout-kill` file: "the service is never forcibly killed" (servicedir) [docs] | **`SIGTERM/5`**: "If this option is not given, the default is SIGTERM/5" (start-stop-daemon(8)) [docs]. Magic's `stop()` passes no `--retry` (`supervisor.go:182`) |
| Logs | journal (`:62-64`) | `StandardOutPath`, `StandardErrorPath` (`plist_render.go:177-178`) | **none.** `exec … 2>&1` goes to runsvdir's stdout, which setup-service starts with output "nowhere" (`supervisor_unix.go:61-64`) | **none,** the same | **none.** `start-stop-daemon --background` with no `output_log` |
| Ready means serving | `Type=notify`, `READY=1` (`:23`) | n/a | n/a | possible: `notification-fd` [docs] | possible: `notify=socket:ready` from 0.62 [docs] |
| Open files | `LimitNOFILE=65536` (`:82`) | `NumberOfFiles` 65536 | inherited | inherited | inherited |

[derived]:

* On these supervisors a daemon that hangs on stop is never killed.
* Its output is lost.
* On OpenRC, a crash leaves Magic down until someone restarts it.

These are remedies each supervisor documents for itself:

* runit's and s6's `log/` service directories, with `svlogd` and `s6-log`;
* s6's `timeout-kill` file;
* OpenRC's `supervise-daemon`, `retry`, `output_log` and `error_log`, and `notify`.

### B2 — OpenRC: the custom `start()` and `stop()` opt out of what OpenRC offers

* **[docs]** The `supervise-daemon` guide: "If you write your own start, stop and status functions
  in your service script, none of this will work. You must allow OpenRC to use the default
  functions."
  * openrc-run(8) calls `supervise-daemon` "the recommended option".
  * It has existed since 0.21 (NEWS), with `respawn_delay`, `respawn_max` (default 10 for the
    service's lifetime) and `respawn_period`.
* **[docs]** The default functions read these variables:
  * `command`, `command_args`, `pidfile` and `directory` (the working directory);
  * `output_log` and `error_log`, or `output_logger` and `error_logger`;
  * `retry`, `notify`, `umask` and `supervise_daemon_args`.

  (openrc-run(8), "Default start/stop" table.)
* **[docs]** From 0.62, `notify=socket:ready` "will export $NOTIFY_SOCKET and listen for
  notifications. At the moment supporting READY=1" (openrc-run(8); NEWS 0.62). 0.60 and 0.61 had
  the fd form as `ready`.
* **[read]** Magic's daemon already sends `READY=1` to the socket it was started with
  (`internal/daemon/daemon_notify.go`, 0210 D10). So on 0.62 and later, an OpenRC start could
  return only once the daemon serves, as under `Type=notify` [derived].
* **[read]** Why Magic wrote its own functions (`supervisor.go:162-166`): an `export` in the script
  replaced the `PATH` that `openrc-run`'s helpers need, which the 2026-10-19 probe showed. So the
  environment goes to `start-stop-daemon --env`. Under the default functions the same flags go
  through `start_stop_daemon_args` or `supervise_daemon_args` [docs]. Whether that works for user
  services is [unverified].
* **[unverified]**
  * Whether `supervise-daemon` runs under `openrc --user`.
  * Which PID its `pidfile` holds: the supervisor's or the daemon's. Magic's `Inside` and the
    adapter's `State.PID` read that PID.
  * Whether `notify=socket:ready` works for a user service.

### B3 — No logs on runit, s6 or OpenRC

* **[read]** The run script ends `exec … 2>&1` (`supervisor.go:160`). The script supervisors'
  scanners are started with stdout and stderr unset, so the output is discarded
  (`supervisor_unix.go:61-77`). The daemon writes no log file of its own (no match for a log-file
  open under `internal/daemon`, `internal/config` or `cmd/mcremote`).
* **[docs]** runsv(8): "If the directory service/log exists, runsv creates a pipe, redirects
  service/run's and service/finish's standard output to the pipe … and starts ./run". s6's
  servicedir describes the same with `log/`, and its run script is told to send stderr there with
  `exec 2>&1`, as Magic's already does.
* **[derived]** A `log/run` of `exec svlogd -tt <dir>` (runit) or `exec s6-log … <dir>` (s6) gives
  rotated logs. Under `$XDG_STATE_HOME/<product>/log`, they sit beside what the launchd job writes.
  The two loggers' exact arguments are to be checked against their manuals in the PLAN.

### B4 — The stop bound is shorter than the daemon's drain

* **[read]** The daemon gets 45 s to drain:
  * `TimeoutStopSec=45` on systemd, `ExitTimeOut` 45 on launchd;
  * on SIGTERM it closes its sessions and their agents' process groups.
* **[read]** `scriptBackend.Stop` waits 15 s (`stopTimeout`, `scriptbackend.go:41`).
* **[reported]** The adapter's `Poll.Timeout` defaults to 60 s. Its `KillAfter` defaults to half of
  it, 30 s (library 0035-MADR D4-C).
* **[read]** The library's systemd backend bounds its stop by the longer of `Poll.Timeout` and the
  unit's `TimeoutStopUSec` plus 30 s (`systemd/lifecycle.go`, `stopBound`; 0015 B3). For Magic's
  unit that is 75 s, not 60.
* **[derived]**
  * 0212 D5-B ("the same bounds as systemd and launchd") was decided as 60 s. The stop systemd
    actually gets is 75 s.
  * With `KillAfter` at 30 s, the adapter would kill a daemon still inside the 45 s drain it is
    allowed everywhere else.
  * Consistent values: `Poll.Timeout` 75 s, and `KillAfter` 45 s, where the supervisor does not
    kill by itself. The adapter shares `Poll.Timeout` between the stop and the health wait, so the
    health wait's ceiling rises too. Its settle window does not.
* **[read]** `internal/certs/acme.go:30`: a first certificate issuance may take 3 minutes before the
  listener opens. The unit allows 240 s to start (`:27`). With no readiness from the supervisor,
  the health wait would roll back such a start [derived]. Whether an update's restart ever issues a
  certificate, rather than finding one cached, is [unverified]. Readiness (B2, s6's
  `notification-fd`) removes the risk where available.

### B5 — The command layer, against the library's rules

The library's rules (its 0011-MADR §2; `systemd/unit.go`) [read]:

* tools at absolute paths in fixed directories, never `PATH`;
* a built environment: a fixed `PATH`, `LC_ALL=C`, no pager or colours, and only the variables the
  tool needs, such as `XDG_RUNTIME_DIR`;
* names validated, and preceded by `--`;
* every call bound by the caller's context;
* a failed command is a `*service.CommandError` (backend, step, name, exit code, message),
  classified with `ErrPermission` or `ErrNotInstalled` where the message says so (its 0025 D2).

Magic [read, subagent inventory, re-checked]:

* **Helpers.** `runCmd`, `runCmdEnv` and `runCmdOutput` find every tool by bare name, through
  `procutil.Command` (`setup.go:1369-1424`).
  * They bound it by their own 30 s, not the caller's context.
  * They inherit the environment plus `XDG_RUNTIME_DIR`.
  * `runCmd` and `runCmdEnv` stream stdout to the process's stdout.
* **Errors.** Only one go-selfupdate-lib sentinel is used anywhere in `internal/cli/service`:
  `ErrInsideService` (`scriptbackend.go:168`). Every other error is a plain string.
* **English-text matches with no fixed locale:**
  * `setup.go:874`, "connect to bus" in `systemctl --user is-system-running`'s output;
  * `settle.go:218`, "status: crashed" in `rc-service status`'s output;
  * `setup.go:647-671`, `launchctl` text.

  Whether any of these is localised is [unverified]. `LC_ALL=C` removes the question at no cost
  [derived].

0212's stage 1 already takes the runner, the absolute paths, the context and the captured output.
It does not yet take the built environment, `--`, or `CommandError` and its classification.

### B6 — Setup detects by `PATH`; the update will not

* **[read]** `detectLinux` chooses runit, s6 or OpenRC by `exec.LookPath` (`detect.go:99-121`).
  setup-service then writes that kind's definition.
* **[derived]** Under 0212 D2-A (a fixed list of directories), the update would not find a tool
  setup found elsewhere on `PATH`, such as in a Nix profile. The service would be set up, and its
  update would fail.
* **[derived]** Detection that uses the same resolver as the update keeps the two in agreement.
  setup-service then refuses, with a message naming the directories, what the update could not
  drive.

### B7 — Process identity is the bare PID

* **[read]** Every PID comparison in `internal/cli/service` is a bare PID:
  * `settle`'s `replacedPid` and `lastPid` (`settle.go:91`, `:110-111`);
  * `Inside`'s walk (`scriptbackend.go:142`);
  * the OpenRC PID file, read with no check of liveness or owner (`scriptbackend.go:105-120`).
* **[read]** Magic already reads a start token on Linux, macOS and Windows:
  `procutil.ProcessStartToken` (`internal/procutil/starttoken_linux.go`, `_darwin.go`,
  `_windows.go`). Its owner registry uses it.
* **[derived]**
  * The adapter will make the update's instances `PID@start-time` (library 0035 D3-A).
  * setup-service's `settle` stays (0212 D6-A), and can use `ProcessStartToken` now at almost no
    cost.
  * The PID file's `/tmp` fallback (`supervisor.go:138`) is in a directory every user can write.
    OpenRC user services require `XDG_RUNTIME_DIR` (user guide) [docs]. So refusing to start with
    it unset, as setup already does (`supervisor.go:579`), would remove the fallback.

### B8 — The testing is not repeatable

* **[read]** The 0210 runs depend on a scratch kit:
  * `containers/Containerfile.runit`, `.s6` and `.openrc`;
  * `containers.py`, `run_r1.py` and `openrc_kind.py`;
  * `d3_run.py`, `build_kit.py`, and the probe agent (`probeagent/main.go`, 310 lines).

  Each Containerfile adds a non-root `tester` user.
  * The Void image is unpinned (`void-musl:latest`). The Alpine images are pinned to `3.22`.
* **[read]** The library's pattern for the same need:
  * an env-gated live test per backend (`SELFUPDATE_REQUIRE_SYSTEMD`, `…_LAUNCHD`, `…_SCM`,
    `…_TASKSCHED`), run in CI;
  * captured real output under `testdata/`, named by version (`show-active-255.txt`,
    `show-active-259.txt`), replayed through the parser (`systemd/captured_test.go`);
  * helper processes waited for with `testproc.Start`, which fails at once when the helper exits
    first, and is bounded by the test's deadline (its 0023 D2-A).
* **[read]** The supervisors' real output already appears in Magic's records. The 0210 PLAN's R1
  and L1 tables quote:
  * `up (pid 17 pgid 17) 10 seconds`;
  * `down (exitcode 0) 129 seconds, normally up`;
  * `* status: started` and `* status: stopping`.

  None is a fixture.
* **[read]** Magic has its own answers to the library's 0023 problems: 0193 warms fresh test
  executables, and 0209 waits for shell traps in `procutil` tests. So `testproc` itself adds little
  [derived].
* **[derived]** Move the kit into the tree:
  * Containerfiles under a test directory;
  * a live-tagged Go test or a `make live-supervisors` target, gated like the library's switches.

  This turns three manual runs into a check anyone can repeat. It also holds the fixtures B5's
  parsers need.

### B9 — The library's planned probes are the facts 0212 needs

* **[read]** The library's 0035 PLAN M1:
  * P2 records `s6-svstat -o` for up, down, a down file, an exiting service and no supervisor.
  * P3 records runit's `sv status`, `sv down`, `sv up` and `sv kill`, including "the exit code and
    text for a directory with no runsv". The PLAN says this "settles F14's [unverified] for the
    consumer, and for runit's fixtures in magic-cli-remote#27".
  * P4 records, for an OpenRC user service:
    * its cgroup;
    * whether a detached process survives `rc_cgroup_cleanup=YES`;
    * the `status` exit codes.
  * M9 sends these results to issue #27.
* **[derived]** 0212's own Confirmation plans most of the same probes. Taking the library's results
  avoids running them twice. Magic then probes only:
  * its own definitions (B1, B2);
  * its tool paths;
  * its stdout question (0212 M9).
* **[read]** The library probes on a WSL Ubuntu 24.04 and an Alpine container. Magic's runs used
  Void and Alpine 3.22, with OpenRC 0.62.6. Runit on Void and on Ubuntu may print the same thing;
  that is [unverified].

### B10 — 0212 predates the library's accepted 0035

* **[read]** The library's 0035 MADR, accepted 2026-10-24, chose:
  * **D4-C:** an optional `Kill`, and `KillAfter`, defaulting to half of `Poll.Timeout`;
  * **D13-C:** a new `internal/procfs`.

  Its PLAN ships `v1.22.0`. Its "What magic-cli-remote does" step 6 says: "Set `Kill` where the
  supervisor does not escalate itself: on runit, `sv kill <dir>`".
* **[read]** 0212 says the library record is proposed, lists no `Kill`, and does not name the
  release. Its facts L1 and L4 need a dated correction, and its stage 2 needs `Kill` and
  `KillAfter` (B4).

### B11 — Already covered by other records

| Finding [read] | Where it is fixed |
| :--- | :--- |
| `launchdJobRunning` counts `state = waiting` as running (`launchd_wait.go:111`, read 2026-10-24); `settle`'s launchd sampler deliberately does not (`settle.go:256-265`) | 0207 U5 and U6 delete `launchdJobRunning`, and move launchd onto `Job.Status` and the backend's `Stop` and `WaitHealthy` |
| PowerShell and `schtasks.exe` on Windows | 0207 U4 and U6, with amendment C's D30 and D31 |
| `UpgradeFromInstaller` builds a 5-minute context, but calls `verifyStart` with none, and does not restart a service whose stop failed (`installerhook.go:141`, `:158-186`) | 0207 U6, which confirms starts through `WaitHealthy`. The script kinds' share becomes the adapter's |
| setup-service's exported functions return plain errors | 0207 U6, where the setup paths move onto the backends |

### B12 — Considered, not recommended

* **Replace before the stop** (library 0020; `ManagedOptions.ReplaceBeforeStop`).
  * The daemon never runs its own executable while serving: no `os.Executable()` outside the setup
    and doctor commands [read].
  * But the replace already takes tens of milliseconds, and the version probe of 0207 U3 runs
    before the stop either way. The gain is small.
  * An unplanned restart in the gap would start the new binary unchecked [docs, library guide].
  * *Note, 2026-10-24.* The owner had it scoped and specified with a post-install probe, which
    supplies the gain this finding found missing: 0207-MADR amendment D, and the 0207 PLAN's step
    U3b.
* **The library's `internal/testproc`.** It is internal to the library, and Magic has its own
  answers (B8).
* **A `umask` in the script definitions.** The mcremote unit omits `UMask` on purpose, and a test
  holds that (`setup_test.go:61-62`).

## Recommendations, mapped to records

| # | Recommendation | From | Record |
| :--- | :--- | :--- | :--- |
| R1 | Bring the script definitions level with the unit: logs on all three, a kill on stop on all three, restart on OpenRC | B1, B3 | 0212 amendment A |
| R2 | OpenRC: default functions with `supervisor=supervise-daemon`, `retry`, `output_log`/`error_log`, and `notify=socket:ready` on 0.62+, after probes | B2 | 0212 amendment A |
| R3 | The adapter's bounds: `Poll.Timeout` 75 s and `KillAfter` 45 s, plus `Kill` for runit (`sv kill`). For s6, `timeout-kill` in the service directory, or `Kill` (`s6-svc -k`) | B4, B10 | 0212 amendment A, refining D5 |
| R4 | The command layer: a built environment with `LC_ALL=C` and a fixed `PATH`, `--` where a tool takes it, and `*service.CommandError` with classification | B5 | 0212 stage 1 |
| R5 | One tool resolver for detection and for the update | B6 | 0212 stage 1 |
| R6 | `settle` compares `PID@ProcessStartToken`; the OpenRC PID file has no `/tmp` fallback | B7 | 0212 stage 1 |
| R7 | The kit in the tree, as env-gated live tests with CI where a runner allows it; captured output as fixtures, named by version; the Void image pinned | B8 | 0212 amendment A |
| R8 | Take the library's M1 probe results, rather than repeating them | B9 | 0212 Confirmation |
| R9 | Dated corrections to 0212's L1 and L4: the library record accepted, `Kill`, `v1.22.0` | B10 | 0212 amendment A |
| R10 | `refreshScript`'s recovery reads both the current and the new definition forms, so an existing install refreshes onto R1 and R2 | B1, B2 [derived: `recoverScript` parses the `exec` and `start-stop-daemon` lines, `supervisor.go:245-340`] | 0212 amendment A |

## Not verified

* Whether `supervise-daemon`, `notify=socket:ready` and `supervise_daemon_args --env` work for an
  OpenRC user service on 0.62.6. Which PID `supervise-daemon`'s `pidfile` holds (B2).
* `svlogd`'s and `s6-log`'s arguments for a user's log directory (B3).
* Whether an update's restart can trigger a first certificate issuance (B4).
* Whether any matched tool text is localised (B5).
* Whether runit prints the same `sv status` on Void and on Ubuntu (B9).

## Sources

* The library, `main` at `fff0940`:
  * `selfupdate/service/doc.go`, `errors.go`, `runner.go`, `poll.go`, `detach.go`, `detach_unix.go`
    and `managerenv.go`;
  * `selfupdate/service/systemd/unit.go`, `lifecycle.go` and `captured_test.go`;
  * `internal/testproc/testproc.go`;
  * `docs/guides/extending-selfupdate.md`, "Run as a service" and "Test a program that
    self-updates".
* OpenRC on `master`, fetched 2026-10-24:
  * <https://github.com/OpenRC/openrc/blob/master/NEWS.md>;
  * <https://github.com/OpenRC/openrc/blob/master/user-guide.md>;
  * <https://github.com/OpenRC/openrc/blob/master/supervise-daemon-guide.md>;
  * <https://github.com/OpenRC/openrc/blob/master/man/openrc-run.8>;
  * <https://github.com/OpenRC/openrc/blob/master/man/start-stop-daemon.8>;
  * <https://github.com/OpenRC/openrc/blob/master/etc/rc.conf>.
* s6: <https://skarnet.org/software/s6/servicedir.html>.
* runit: <http://smarden.org/runit/sv.8.html> and <http://smarden.org/runit/runsv.8.html>.
* This repository at `793b8f5b`:
  * `internal/cli/service`;
  * `internal/procutil/starttoken_*.go` and `owner_linux.go`;
  * `internal/daemon/daemon_notify.go`;
  * `internal/certs/acme.go`;
  * the 0210 PLAN's R1 and L1 records.
