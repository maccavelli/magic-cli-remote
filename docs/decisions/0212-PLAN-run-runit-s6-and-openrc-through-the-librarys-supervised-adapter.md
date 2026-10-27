---
status: complete
date: 2026-10-27
associated-madr: "0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement runit, s6 and OpenRC through go-selfupdate-lib's supervised adapter

Associated MADR: [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md),
with its amendments A to D. Amendment C records P4's deviations. Amendment D (2026-10-27) is
~~proposed~~ decided ("approved to proceed", 2026-10-27); it reshapes P5 and orders 0207's U6 after
it.

**Decided by the owner, 2026-10-24:**

* the direction: the library's `NewSupervised`;
* D1, two stages, now consecutive phases of this plan (D16);
* D3-B, the cgroup check;
* D5-B, the other backends' bounds;
* D8, the pin moves under 0207;
* D9-A;
* D10-A, with D10-B as the fallback the probes decide;
* D11-A as corrected by D17;
* D13-A;
* D16.

**Proposed, and accepted by approving this plan:** D2-A, D4-A, D6-A, D7-A, D12-A, D14-A, D15-A and
D18. The owner asked for this plan, verbatim: "U1b, then write 0212 PLAN".

## Goal

* An update on runit, s6 or OpenRC runs through `libservice.NewSupervised`. It honours its context,
  returns the library's sentinel errors, and puts the supervisor's want back when a stop fails. It
  runs the product's application probe, and hands off when it runs inside the service.
* An agent-run update on each of the three supervisors hands off and completes. 0210's R1
  reproduction then ends with the update applied and the service restarted.
* No supervisor tool is found on `PATH`, and no tool's output reaches the update's stdout.
* runit, s6 and OpenRC definitions log, and a daemon that hangs on stop is killed at 45 s. OpenRC
  restarts a crashed daemon, when the probes allow D10-A.
* The live runs of the three supervisors live in the tree: `make live-supervisors`, with CI for
  runit and s6.

## Scope

* **In scope:** the MADR's D1 to D18, in the phases below.
* **Not in scope:**
  * setup-service's own start confirmation for these kinds, which stays on `settle` (D6-A).
    0207's U6 builds on this plan's state functions.
  * OpenRC *system* services, which Magic does not install.
  * A `notification-fd` for s6 (D18).
  * Any change to the systemd, launchd or Task Scheduler backends.

## Order across plans

1. [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md)'s
   U1b, the `v1.22.0` pin. Phase P2 needs `libservice.SupervisedState` (D16).
2. This plan's P1 to P7.
3. 0207's U3, U3b and U4 to U8 may run between this plan's phases, since they touch other files,
   with two exceptions:
   * **G2.** 0207 U3b's definition guard renders "the runit, s6 and OpenRC definitions, in every
     form 0212 renders". Whichever of U3b and this plan's P4 lands second adds the other's forms to
     it.
   * **U6.** 0207 U6 builds its script-kind start confirmation on P3's state functions (D6-A), so
     it follows P3.
4. 0207's U7 is retired into this plan (MADR R2). It is not run.

## Facts the steps rely on [read 2026-10-24, at `793b8f5b`]

| Fact | Where |
| :--- | :--- |
| The script kinds get `newScriptBackend` | `internal/cli/service/backend.go:59-60` |
| `scriptBackend`: `Installed`, `Enabled`, `Running`, `supervisedPID`, `Inside`, `Stop`, `Start`, `WaitHealthy` | `scriptbackend.go:44-247` |
| `Stop` polls to `stopTimeout`, 15 s, with `time.Sleep`, and ignores `ctx` | `scriptbackend.go:41`, `:181-191` |
| `Start` starts a session supervisor when none watches | `scriptbackend.go:198-230` |
| `procParent` reads `/proc` | `scriptbackend_inside_linux.go:13` |
| The samplers: `sv status`, `s6-svstat`, `rc-service --user … status` | `settle.go:198-241` |
| The status expressions | `settle.go:180-192` |
| `runCmd` and `runCmdEnv` stream stdout to `os.Stdout`, with `cmdTimeout` and no caller context | `setup.go:1369-1407` |
| `runCmdOutput` captures | `setup.go:1409-1425` |
| `detectLinux` finds tools with `exec.LookPath` | `detect.go:74-77`, `:99-122` |
| `renderScript`: runit and s6 `run`; OpenRC with `start-stop-daemon` and a `pidfile` that falls back to `/tmp` | `supervisor.go:116-187`, `:138`, `:167-184` |
| `recoverScript` | `supervisor.go:245` |
| `readServeArgs` reads only the systemd unit on Linux | `health.go:72-100` |
| `IsInstalled` builds a backend | `control.go:23`, `:45` |
| Both `main`s complete the handoff hop first | `cmd/mcremote/main.go:24`, `cmd/mcrelay/main.go:25` |
| The adapter refuses `KillAfter` without `Kill` | library `selfupdate/service/supervised.go:170-177` at `v1.22.0` |

*Re-read 2026-10-26, at `95ca8b91`, before approval.* 0207's U3b, U4, U5 and U8 have landed since
`793b8f5b`. Every fact above still holds. Only these locations moved:

* `IsInstalled` is at `control.go:44`, and builds the script backend at `:66` (U5 added the launchd
  status reader above it).
* `readServeArgs` is at `health.go:71`. On Linux it still reads only the systemd unit.
* The samplers are at `settle.go:203-217`, and the status expressions at `:182-191`.
* P5's help text is the update command's `Long`, at `internal/updateclient/client.go:355-356`
  ("Under runit, s6 and OpenRC it refuses before downloading …"). `:290-294` is now a comment on
  `insiderFor`.
* G2, 0207 U3b's definition guard, landed in `internal/cli/service/definitions_once_test.go`. It
  covers today's runit, s6 and OpenRC forms (`:88-94`), so P4 adds the new forms there, as "Order
  across plans" provides.

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Every new test is seen failing first,** on a scratch copy, never in the working tree. The
  execution record quotes the failure.
* **Before staging:** `make pre-add-check FILES="…"` on every staged `.go` file; `make test`;
  `make race`; `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64; `go mod tidy -diff`;
  `make check-records`; `make markdownlint-docs`; and the guard's redact preview from the repository
  root.
  * `go test -race ./...` on the Linux test host, in a scratch copy of the working tree, as 0207's
    rules set.
* **Host and container runs** need the owner's word for that phase. They use a scratch service,
  never a host's own `mcremote`. Every container and copy they make is removed afterwards.
* **A phase ends with its execution record** here, staged for the owner, who commits it.

### P1 — Magic's probes (D14-A; Confirmation)

**Files:** this plan only. The probes are Python scripts in the session scratchpad. They run in the
0210 kit's three containers (Void runit; Alpine 3.22 s6; Alpine 3.22 OpenRC 0.62.6) and on the
Linux test host.

1. **Tool paths (D2).** `command -v` for `sv`, `runsvdir`, `svlogd`, `s6-svc`, `s6-svstat`,
   `s6-svscan`, `s6-svscanctl`, `s6-log`, `rc-service`, `rc-update`, `openrc`,
   `start-stop-daemon` and `supervise-daemon`. The fixed list is the union, in a fixed order.
   * Expected: `/usr/bin`, `/bin`, `/usr/sbin`, `/sbin`.
   * **Decision point:** a tool outside every expected directory adds that directory to the list,
     recorded here.
2. **Stdout (M9).** What each start, stop and status command prints on stdout and stderr.
3. **Exit codes (D12).** For each tool: success; not installed; no supervisor; permission denied
   where it can be produced. These become `CommandError`'s classification table.
4. **State fixtures (D7, D16).** Captured output, saved under the scratchpad for P3's `testdata`:
   * `sv status` and `s6-svstat -o up,wantedup,normallyup,ready,pid,exitcode,updownfor`, each for
     up, down, "down, want up", down-file and no supervisor;
   * `rc-service --user <name> status` for started, stopped and crashed.
5. **D10-A, in the OpenRC container, against Magic's built daemon:**
   * `supervisor=supervise-daemon` under `openrc --user`;
   * `notify=socket:ready`: `rc-service start` returns only after `READY=1`;
   * `supervise_daemon_args` with `--env`;
   * which PID the `pidfile` holds: the daemon's, or `supervise-daemon`'s;
   * a crash restarted after `respawn_delay`;
   * a daemon that ignores TERM killed by `retry="TERM/45/KILL/5"`, and how long
     `rc-service stop` blocks (D17).

   **Decision point:** if any of these fails, D10-B applies, recorded here before P4.
6. **D9-A:** in the s6 container, a daemon that ignores TERM is killed 45 s after `s6-svc -d`, with
   `timeout-kill` 45000. In the runit container, `sv kill` ends it (the library's P3 says so; this
   repeats it against Magic's daemon).
7. **D3:** the daemon's and a detached child's `/proc/<pid>/cgroup` under `openrc --user` in the
   OpenRC container.

### P2 — The command layer (stage 1; D2-A, D12-A)

**Files:** `internal/cli/service/`: a new `supervisortool.go` (the fixed list, the resolver and
the runner), its test, `detect.go`, `setup.go` (`runCmd`, `runCmdEnv`, `runCmdOutput` callers on
script paths), `supervisor.go`, `supervisor_unix.go`, `scriptbackend.go`, `settle.go` (the
samplers' calls only); `testdata/` as needed; this plan. *P2 Deviation 1, 2026-10-26:*
`detect_unix_test.go`, `supervisor_unix_test.go`, `scriptbackend_unix_test.go` and
`internal/updateclient/managed_script_unix_test.go` join. *Deviations 2 and 3:* the built environment
also carries `XDG_CONFIG_HOME` when set, and `--` goes to `rc-service` only (execution record below).

1. **Tests first:**
   * The resolver returns an absolute path from the fixed list, and an error naming every
     directory searched when the tool is in none. It never consults `PATH`. Plant: a resolver that
     falls back to `exec.LookPath`, with a tool only on a planted `PATH`.
   * The runner passes a built environment: `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, `LC_ALL=C`,
     `XDG_RUNTIME_DIR`, `HOME` and `USER`. Plant: an inherited `PATH`.
   * The runner honours `ctx`: a cancelled context kills the tool. Plant: `context.Background()`.
   * A non-zero exit becomes a `*libservice.CommandError` with `Backend` set to the kind, wrapped
     with `ErrPermission`, `ErrNotInstalled` or `ErrNoSupervisor` by P1's table. Plant: an
     unclassified error.
   * No tool output reaches the process's stdout. Plant: `cmd.Stdout = os.Stdout`.
   * `detectLinux` and the update resolve through the same resolver.
2. **Code:**
   * The runner is a `libservice.Runner`, by default `libservice.ExecRunner`, at the resolved
     absolute path, with `--` before names where P1 shows the tool accepts it.
   * Every `sv`, `s6-svc`, `s6-svstat`, `s6-svscanctl`, `rc-service`, `rc-update` and `openrc` call
     on the script paths goes through it, setup-service's included. setup-service prints its own
     messages, not the tools'.
   * `startSessionSupervisor` keeps its detached start, at the resolved path.
   * `lookPath` in `detectLinux` becomes the resolver.
3. Gates.

### P3 — State, in the library's type (stage 1; D7-A, D16, D18, D12-A, D4-A)

**Files:** a new `internal/cli/service/supervisorstate.go` and its test; `testdata/supervisor/`
(P1's fixtures, named by supervisor, version and state); `scriptbackend.go`; `settle.go`;
`health.go`; this plan. *P3 Deviations 1 and 2, 2026-10-26:* `supervisor_unix_test.go`,
`scriptbackend_unix_test.go`, `settle_test.go`, `setup.go` and `supervisor.go` join (execution
record below).

1. **Tests first,** replaying P1's fixtures:
   * one `state<Kind>(ctx) (libservice.SupervisedState, error)` per kind:
     * **runit:** `sv status`. `Restarting` from "down … want up". `NormallyUp` is the absence of
       `<dir>/down`. Exit 1, "unable to open supervise/ok", is `ErrNoSupervisor`.
     * **s6:** `s6-svstat -o` (D7-A). Exit 1 is `ErrNoSupervisor`. `ready` is read and not mapped:
       `Ready` is nil (D18). Plant: `Ready` set from the field.
     * **OpenRC:** the `status` exit code, with 32, "crashed", as `Failed`. The PID from the
       `pidfile` P1 identified. `NormallyUp` from the `default` runlevel link. No user session is
       `ErrNoSupervisor`.
     * `Installed` is the definition's existence, as today.
   * The samplers and `Inside` read this state, and their behaviour is unchanged. The existing
     settle tests pass unedited.
   * `settle` compares PID and `procutil.ProcessStartToken` (D12-A). Plant: a reused PID with a new
     start time reads as the same instance.
   * The OpenRC `pidfile` has no `/tmp` fallback (D12-A). Plant: `${XDG_RUNTIME_DIR:-/tmp}`.
   * `readServeArgs` reads the script definitions through `recoverScript` (D4-A).
2. **Code** as tested.
3. Gates.

### P4 — The definitions (D9-A, D10, D15-A)

**Files:** `supervisor.go` (`renderScript`, `recoverScript`, `setupScript`, `removeScript`), their
tests, `testdata/` goldens; `docs/guides/ops-linux-install.md`; the 0207 U3b definition guard, if
U3b has landed; this plan. *Re-read 2026-10-26:* U3b has landed, and its guard is
`definitions_once_test.go`. *P4 Deviation 1, 2026-10-26:* `scriptbackend_unix_test.go` joins
(execution record below). *P4 Deviation 3, 2026-10-26:* `scriptbackend.go` joins. *P4 Deviation
4:* `supervisorstate.go`, its test, and runit fixtures join.

1. **Tests first:**
   * **runit and s6:** a `log/run` service directory, `svlogd` for runit and `s6-log` for s6, into
     `$XDG_STATE_HOME/<product>/log`. **s6:** a `timeout-kill` file holding `45000`.
   * **OpenRC, D10-A** (or D10-B, by P1's decision point): `command`, `command_args`,
     `directory`; `supervisor=supervise-daemon`; `respawn_delay=5`, `respawn_max=30`,
     `respawn_period=300`; `retry="TERM/45/KILL/5"`; `output_log` and `error_log`; ~~on OpenRC 0.62
     or later, `notify=socket:ready`~~ *(dropped 2026-10-26, P4 Deviation 2)*; the environment
     through `supervise_daemon_args`.
   * `recoverScript` reads both the current and the new form of each definition (D15-A). Plant: a
     reader of the new form only, against a current-form fixture.
   * `removeScript` removes the `log` directory too.
   * Golden files for each rendered form.
2. **Code** as tested.
3. **Containers, on the owner's word:** a fresh install and a `--refresh` from the current form,
   in each container. Logs are written; s6 and OpenRC kill a TERM-ignoring daemon at 45 s; OpenRC
   restarts a crashed one under D10-A.
   * *Added 2026-10-26, P2 Deviation 2:* in the OpenRC container, an install and a start with
     `XDG_CONFIG_HOME` set to a non-default directory, where `rc-service --user` finds the script
     Magic wrote.
4. Gates.

### P4b — A log directory per unit (owner decision, 2026-10-27; ~~proposed~~ approved and done 2026-10-27)

*Added 2026-10-27 at the owner's word: "Commit P4, then a P4b phase". Runs after the owner commits
P4, on approval of this phase.*

**Why.** runit's and s6's `log/run` write into `$XDG_STATE_HOME/<product>/log`, one directory per
product (`scriptLogDir`). Two services of one product (two `--unit-name`s) would run two loggers
there, and the second could not take its lock. This was found by reading the code, and step 1
reproduces it before any fix. OpenRC writes `<unit>.log`, one file per unit, and is unaffected.

**Files:** `supervisor.go` (`scriptLogDir`, `scriptAuxFor`, `recoverScript` if the path is read
back, `removeScript`'s result), the CLI's removal output (`internal/cli/setup_service.go` or the
result printer it calls, named at the phase's start), their tests, `testdata/definitions/`
goldens, `docs/guides/ops-linux-install.md`, this plan.

1. **Reproduce, in the runit and s6 containers, on the owner's word:** two services of one
   product, both running at once, under P4. Record what each logger does.
2. **Tests first:**
   * runit and s6 log into `$XDG_STATE_HOME/<product>/log/<unit>/`. OpenRC's `<unit>.log` stays.
   * A refresh moves an install's `log/run` from P4's path onto the new one, and `Unchanged`
     holds after.
   * `--remove` keeps the log files and names the kept directory in its output (owner decision
     2026-10-27, "Keep them").
3. **Code** as tested.
4. **Containers, on the owner's word:** two units of one product running at once, each logging to
   its own directory; a refresh from P4's path, adopted by the next start.
5. Gates.

### P5 — The adapter (stage 2; D3-B, D5-B, D17, D6-A)

*Held 2026-10-27 by the owner, until an assessment of moving the service stack out of Magic and into
go-selfupdate-lib, and the decision it leads to. See the owner decisions of 2026-10-27 in the
execution record.*

*Amended 2026-10-27 by the MADR's amendment D. Approved the same day: "approved to proceed". The
hold is lifted.*

* **Step 0 (D20):** move the pin to go-selfupdate-lib `v1.22.1`. Before the bump, read the tag's
  diff against `v1.22.0`, and confirm it changes only `Supervised.Stop`'s restore and its
  records.
* **`Up` (D21):** in this order:
  1. start the session supervisor when none watches the scan directory, as today's `Start`;
  2. adopt the log service with `adoptLogService` (P4 Deviations 3 and 4), only when `State`
     reports the service down;
  3. run `sv up`, `s6-svc -u`, or `rc-service --user -- <name> start`.
* **Tests added to step 1:**
  * `Up` adopts a log service on a down service, and does not on one that is up: the restore after
    a failed stop;
  * a second `Up` after an adoption runs no adoption command.
* **Files:** `supervisor.go` joins, for `adoptLogService`'s caller. `go.mod` and `go.sum` join, for
  step 0.
* **Nothing moves into the library (D19).** No library issue is filed for this phase.
* **0207's U6 follows P5 (D22),** and confirms the script kinds' starts by this phase's
  `WaitHealthy`.

**Files:** `backend.go`, a new `supervisedbackend.go` and its test, `control.go`, `scriptbackend.go`
and `scriptbackend_inside_linux.go` (deleted), their tests; `internal/updateclient/client.go`
(the help text at ~~`:290-294`~~ `:355-356`, re-read 2026-10-26); `docs/guides/ops-linux-install.md`;
this plan.

1. **Tests first:**
   * Each kind's `SupervisedOptions` are accepted by `NewSupervised`. Plant: `KillAfter` set on s6
     (refused, V3 of amendment B).
   * `Poll` is `Timeout` 75 s and `Settle` 10 s. `MCREMOTE_SVC_WAIT_SECS`, when set, becomes
     `Poll.Timeout`, and is refused when not above 45 s on runit, or not above the settle window.
   * **runit:** `Kill` runs `sv kill <dir>`, and `KillAfter` is 45 s. **s6 and OpenRC:** no
     `Kill`.
   * `Probe` is `HealthOptions.backendProbe`.
   * **OpenRC's `DetachCheck` (D3-B)** refuses when this process and the daemon share a cgroup
     whose last element starts with `openrc.`, read from `/proc/<pid>/cgroup`, and allows
     otherwise. Plant: a check that always allows, against a fixture of a shared `openrc.` cgroup.
   * `IsInstalled` reads the state's `Installed`.
   * The update's stdout carries only `--json` output on these kinds.
2. **Code:**
   * `NewBackendWith` builds one `*libservice.Supervised` per script kind: `State` from P3, `Up`
     from today's `Start` (the session supervisor start kept), `Down` from today's stop command
     with no wait, and the options above.
   * Delete `scriptBackend`, `Inside`, `parentOf`, `procParent`, `maxAncestry`, `stopTimeout` and
     the script `WaitHealthy`. `settle` stays (D6-A). `refuseInside` stays for any backend that is
     not a `Detacher`.
   * The help text and `ops-linux-install.md` say the update hands off on these supervisors, and
     name OpenRC's refusal under a shared service cgroup.
3. Gates.

### P6 — The live tests in the tree (D13-A; Confirmation)

**Files:** `test/supervisors/Containerfile.runit`, `Containerfile.s6`, `Containerfile.openrc`
(images pinned by digest, Void included); a new `internal/cli/service/live_supervisors_linux_test.go`
behind `MCREMOTE_REQUIRE_RUNIT`, `MCREMOTE_REQUIRE_S6` and `MCREMOTE_REQUIRE_OPENRC`; `Makefile`
(`live-supervisors`); `.github/workflows/ci.yml` (a runit and s6 step on the Ubuntu runner);
`docs/guides/ops-linux-install.md`; this plan.

1. Each live test installs, stops, starts, updates from a shell, updates from inside the service
   (the handoff), and removes. It ends with the handed-off build running, read through
   `/proc/<pid>/exe`.
2. **Seen failing, live, on a scratch copy:** a `Down` that does nothing (the stop times out and
   the want is restored); `Detach` with the hop dropped from the spec; OpenRC's `DetachCheck` made
   to allow.
3. **On the owner's word:** `make live-supervisors` in the three containers; 0210's R1 reproduction
   re-run, now ending with the update applied and the service restarted on each.
   * *Added 2026-10-25, from 0207 PLAN U3b Deviation 2:* the agent-run update, run through the
     0210 kit's probe agent, must hand off on each of the three. Its result must have
     `replaced_before_stop: true`, `service_started: true` and `rolled_back: false`, with a new PID
     and `/healthz` up. That is 0207-MADR D36's success run, which U3b could not run there while
     those supervisors had no handoff.
4. The CI step runs runit and s6, as the library's M6 runs s6. `actionlint` is clean. OpenRC runs
   through the target only, since it needs a container booted with its init.
5. Gates.

### P7 — The records

**Files:** the records named in the MADR's "What this record changes in others"; this plan; the
MADR's status.

1. Dated notes in 0054-MADR S5-3, 0207-MADR D20 and 0207-PLAN U7 (retired), 0207-MADR D4 and
   0207-PLAN U6, and 0210-MADR D4 and its question 2.
2. **On the owner's ask:** a comment on #27 with the MADR's R6 corrections and this plan's outcome.
   Closing #27 is the owner's.
3. The MADR becomes `accepted`, and this plan `complete`, when every Verification item holds.

## Verification

* P1's probe results, quoted per supervisor, with each decision point's outcome.
* Each phase's fail-first tests, quoted in its record.
* `git grep -n -e 'exec.LookPath' -e 'os.Stdout' -- internal/cli/service` finds no script-kind
  tool call.
* `git grep -n -e scriptBackend -e stopTimeout -e procParent -- internal` finds nothing outside
  history comments.
* `make live-supervisors` passes in all three containers. CI's runit and s6 step passes.
* An agent-run update hands off and completes on runit, s6 and OpenRC.

## Rollout and Rollback

* **Rollout.** At the next tag after P6. An existing install moves to the new definitions on the
  update's reconcile, through `setup-service --refresh` (D15-A).
* **Rollback.** By phase, in reverse.
  * P5 alone restores `scriptBackend` and its fail-safe refusal from inside.
  * P4 alone restores the current definitions; `recoverScript` still reads both forms until P4's
    revert removes the new one, so a refresh moves an install back.
  * P2 and P3 revert only after P5, which builds on them.
  * Nothing on a host needs undoing beyond a refresh.

## Execution record

Not started. The plan waits for the owner's approval, after 0207's U1b.

*2026-10-26:* 0207's U1b, U3, U3b, U4, U5 and U8 are done, and U7 is retired here. The facts were
re-read at `95ca8b91` (the note under the facts table). The plan still waits for the owner's
approval. P1 needs the owner's word for its container and host runs, and the 0210 kit's three
containers on the Linux test host, which were last used in 0207 U3b step 5.

*Approved 2026-10-26.* The owner committed the re-read as `d2eca29a` and answered "committed,
approve 0212, proceed". That approves this plan, and with it D2-A, D4-A, D6-A, D7-A, D12-A, D14-A,
D15-A and D18, as "Proposed, and accepted by approving this plan" provides. P1 is a probe phase, all
container and host runs, so "proceed" is taken as the word for P1's runs. Each later phase's runs
still need their own.

* **Correction to the note above.** 0207 U3b step 5 ran the 0210 kit's three images in local
  Podman on this Mac (linux/arm64), not on the Linux test host. P1 runs them there, and its tool-path
  probe also on the Linux test host (linux/amd64), as P1 says.

### P1, 2026-10-26: Magic's probes

**How it ran** (`m212/p1_drive.py` and `m212/p1_in.py`, session scratchpad):

* The local Podman machine was started for the run and stopped again afterwards, as it was before.
* Each supervisor got a fresh container from the 0210 kit's images, as the non-root `tester` user.
  Root made only `/run/user/1000`, and a root-owned `0700` directory for the permission probes.
* The scratch services lived under `~/p1`. For OpenRC's D10-A probe, Magic's daemon was built for
  linux/arm64 from `d2eca29a`, with its own loopback config.
* Each container was removed; `podman ps -a` lists none. The Linux test host ran the tool-path probe
  in a scratch directory, deleted afterwards.

| Image | Versions |
| :--- | :--- |
| `d3probe-runit` | Void Linux (musl), `runit-2.3.1_2` |
| `d3probe-s6` | Alpine 3.22.6, `s6` 2.13.2.0, `execline` 2.9.7.0 |
| `d3probe-openrc` | Alpine 3.22.6, `openrc` 0.62.6, `busybox` 1.37.0 |
| the Linux test host | Ubuntu 26.04.1 LTS, systemd; of the list, only `start-stop-daemon` |

**Two mistakes in the first run, fixed:**

* **OpenRC's probes never ran.** The script did not create `~/.config/rc/runlevels/{boot,default,shutdown}`,
  which `startOpenRC` creates first (`supervisor.go:584`). So `openrc --user boot` failed with
  "boot: not a valid runlevel". A `start` tried before the session existed then left each service
  "already starting" for the rest of the run.
* **s6's version line was wrong.** `apk info` read a repository cache the image does not have.
* **The fix.** The script creates the runlevels as Magic does, drops the start before the session,
  and reads versions from `/lib/apk/db/installed`.
* **What counts.** s6 and OpenRC ran again in full, and their rows below are from that second run.
  runit's first run had neither problem, and counts. Both runs' logs are kept in the scratchpad.

**1. Tool paths (D2).**

| Tool | runit (Void) | s6 (Alpine) | OpenRC (Alpine) | Linux test host |
| :--- | :--- | :--- | :--- | :--- |
| `sv`, `runsvdir`, `svlogd` | `/usr/bin` (`/bin`, `/sbin`, `/usr/sbin` link to it) | absent | absent | absent |
| `s6-svc`, `s6-svstat`, `s6-svscan`, `s6-svscanctl`, `s6-log` | absent | `/usr/bin` | absent | absent |
| `rc-service`, `rc-update`, `openrc`, `start-stop-daemon`, `supervise-daemon` | absent | absent | `/sbin` | `start-stop-daemon` in `/usr/sbin` (`/sbin` links to it) |

* **Decision point: no change.** Every tool found is in `/usr/bin`, `/bin`, `/usr/sbin` or `/sbin`.
  The fixed list stays those four directories, which P2's built `PATH`,
  `/usr/sbin:/usr/bin:/sbin:/bin`, also covers.
* Nothing was found in `/usr/local/*`, and `tester`'s `PATH` on Void is only
  `/usr/local/sbin:/usr/local/bin:/usr/bin`. So a resolver that read `PATH` would differ by host,
  and the fixed list does not.

**2. What the tools print (M9).**

* **runit** prints its failures and warnings on **stdout**: "fail: …", "warning: …", "timeout: …".
  Only its usage goes to stderr.
* `sv up`, `sv down`, `sv kill` and `sv exit` print nothing on success. `sv restart` prints
  "ok: run: <dir>: (pid 30) 0s".
* `sv restart` waits. Into a run that keeps failing, it returned after **7.6 s** with exit 1 and
  "timeout: down: <dir>: 1s, normally up, want up". That is sv's default 7 s wait. Magic's
  `startScript` calls `sv restart` (`supervisor.go:412`).
* **s6** prints nothing on success, and its errors go to stderr.
* **OpenRC** prints progress on stdout ("* Starting p1ssd ... [ ok ]", "* Caching service
  dependencies ... [ ok ]"), and its errors and warnings on stderr. `status` goes to stdout, except
  "crashed", which goes to stderr.
* So no tool's output can be streamed to the update's stdout. P2 captures all of it.

**3. Exit codes (D12), the classification table for P2:**

| Case | runit `sv` | s6 | OpenRC `rc-service --user` |
| :--- | :--- | :--- | :--- |
| success | 0 | 0 | 0 |
| no definition | 1, stdout "fail: <dir>: unable to change to service directory: file does not exist" | `s6-svstat` 1, "s6-supervise not running", the same as no supervisor | 1, stderr "service \`p1absent' does not exist" |
| no supervisor | 1, stdout "warning: <dir>: unable to open supervise/ok" (status), or "supervise/control" (`up`, `down`) | `s6-svstat` 1, "s6-supervise not running"; `s6-svc` and `s6-svscanctl` **100**, "supervisor not listening" | no session: 1, "You are attempting to run an openrc service on a system which openrc did not boot"; no `XDG_RUNTIME_DIR`: 1, "XDG_RUNTIME_DIR unset." |
| permission denied | 1, stdout "fail: <dir>: unable to change to service directory: access denied" | **111**, "unable to check <dir>: Permission denied" (`s6-svc`: "unable to control") | not produced (below) |
| usage | 100 | — | — |
| a wait timed out | 1, stdout "timeout: …" (`sv restart`) | — | — |

* **runit exits 1 for every failure,** so its class comes from the message, not the code.
* **s6 tells a missing definition from a missing supervisor by neither code nor message.**
  `Installed` comes from the definition's existence, as P3 already provides.
* **OpenRC's permission case was not produced.** As `tester`, a system service only reported
  "WARNING: local is already starting", because the container's system OpenRC was never booted. User
  services run as their own user, so this is the case least likely to arise. P2 classifies it by
  message if it does.
* **`--` before a name** (P2): `s6-svstat` and `rc-service --user` accept it. **`sv` does not:**
  `sv status -- <dir>` exits 1 with "fail: --: unable to change to service directory". The runner
  passes no `--` to `sv`.

**4. State fixtures (D7, D16),** saved in the scratchpad for P3's `testdata/supervisor/`, each command's
stdout as it was, with its exit code and stderr beside it:

* **runit:** up ("run: <dir>: (pid 20) 2s"), down ("down: <dir>: 0s, normally up"), down and wanted up
  ("…, normally up, want up"), up with a down file ("…, normally down"), down with a down file, up
  and wanted down ("run: … 4s, want down, got TERM"), no supervisor, no definition, permission
  denied.
* **s6:** both the default and the `-o up,wantedup,normallyup,ready,pid,exitcode,updownfor` form of
  up, down, down and wanted up, up and down with a down file, and up and wanted down; no supervisor;
  no definition; permission denied.
  * `-o` prints the fields in order: up, `true true true false 18 -1 2`; down,
    `false false true false -1 0 0`; down and wanted up, `false true true false -1 0 0`; up with a
    down file, `true true false false 39 -1 1`; up and wanted down, `true false true false 46 -1 4`.
  * `ready` is `false` even when up, because there is no `notification-fd`. That is D18: `Ready`
    stays nil.
* **OpenRC:** no `XDG_RUNTIME_DIR`, no session, no definition; the start-stop-daemon form stopped
  (**3**, "status: stopped"), started (**0**) and crashed (**32**, "status: crashed"); the
  supervise-daemon form started, stopped, and started again after a crash.

**5. D10-A, in the OpenRC container, against Magic's daemon:**

| Check | Result |
| :--- | :--- |
| `supervisor=supervise-daemon` under `openrc --user` | runs: "status: started" |
| `notify=socket:ready` | with Magic's daemon, `rc-service start` returned in 0.047 s and `/healthz` answered at once. With a stand-in that sends `READY=1` after 3 s, it returned in **3.027 s**. The daemon's `NOTIFY_SOCKET` was `/run/user/1000/openrc/supervise-p1sd.sock` |
| `supervise_daemon_args` with `--env` | `P1_ENV`, `XDG_STATE_HOME` and `XDG_DATA_HOME` reached the daemon's environment |
| which PID the `pidfile` holds | **`supervise-daemon`'s** (306), not the daemon's (307), whose parent is 306 |
| a crash, with `respawn_delay=5` | `SIGKILL` to the daemon; a new daemon after **5.3 s**; "status: started" |
| a TERM-ignoring daemon, `retry="TERM/45/KILL/5"` | `rc-service stop` blocked **48.1 s**, then "status: stopped", with none left |
| `output_log`, `error_log` | both written; the daemon's log is in `error_log` |

* **Decision point: D10-A stands.** Every check held, on OpenRC 0.62.6.
* **For P3:** under D10-A the `pidfile` names the supervisor. The daemon's PID is its child.
* **For P5:** the 48 s stop is within D17's 75 s `Poll.Timeout`.

**6. D9-A:**

* **s6**, with `timeout-kill` holding `45000`: a daemon that ignores TERM was down **45.3 s** after
  `s6-svc -d`, then "down (signal SIGKILL)". Both runs gave 45.3 s.
* **runit:** after `sv down` the daemon stayed "run: …, want down, got TERM". `sv kill` ended it in
  0.21 s.

**7. D3:** under `openrc --user`, the daemon, its detached child, `supervise-daemon`, this process
and PID 1 were all in `0::/`. `rc_cgroup_mode` is unset in `/etc/rc.conf`, and `/sys/fs/cgroup` is
not writable to the container. That matches amendment B's V6 on OpenRC 0.63.2: a user service has no
service cgroup there, so D3-B's check allows (E3). A system service, and a real OpenRC host, stay
unmeasured, as E3 says.

**What P1 changes for the later phases.** No decision changes, and no file outside P1's list was
touched. The phases take these facts, all within their steps as written:

* **P2:**
  * runit failures are classified by message;
  * no `--` for `sv`;
  * `sv restart` can block for 7 s and exit 1 on a service that will not stay up, so a start
    through it is bounded by the runner's context.
* **P3:**
  * the fixtures above;
  * OpenRC's daemon PID is the child of the PID in the `pidfile` under D10-A, and the PID in it
    under today's start-stop-daemon form;
  * s6's missing-definition and missing-supervisor states are told apart by the definition, not the
    tool.
* **P4:** D10-A, with `notify=socket:ready` available from 0.62. *Corrected 2026-10-26, P4
  Deviation 2: only a service's first start works with it, so it is dropped.*
* **P5:** OpenRC's stop of a hung daemon takes about 48 s.

P1 is complete once the owner commits this record. P2 is next, on the owner's word.

### P2 Deviations 1 to 3, 2026-10-26 — the tests' fakes, the built environment, and `--`. The owner decided all three as recommended

**Approval.** The owner committed P1's record and answered "committed, proceed".

**Found by measuring before any edit:**

* **1. The tests fake the tools on `PATH`.**
  * `fakeHost` (`detect_unix_test.go:52-65`) symlinks one stub under each tool's name into a
    directory, and makes that directory the whole `PATH`. Four test files use it:
    `detect_unix_test.go`, `supervisor_unix_test.go`, `scriptbackend_unix_test.go` and
    `uninstall_unix_test.go`.
  * `internal/updateclient/managed_script_unix_test.go:59-62` puts its own fake `sv` on `PATH`.
  * A resolver that never reads `PATH`, which is D2-A's point, finds none of them. On a host with
    the real tools it would find those instead.
  * None of these test files is on P2's list.
* **2. The built environment would split OpenRC from Magic.** Magic writes OpenRC user scripts under
  `$XDG_CONFIG_HOME/rc/init.d` (`supervisor.go:63`). OpenRC's user mode reads its init.d from
  `XDG_CONFIG_HOME`, by its documentation; P1 did not probe a non-default value. The tools inherit
  the variable today. Step 1's list (`PATH`, `LC_ALL`, `XDG_RUNTIME_DIR`, `HOME`, `USER`) drops it.
* **3. Where `--` guards anything.**
  * Unit names can begin with `-` (`unitNameRe`, `setup.go:172`).
  * runit and s6 tools are always passed absolute directories, which cannot.
  * P1 showed `rc-service --user` and `s6-svstat` accept `--`, and `sv` does not. `rc-update` was not
    probed.

**The resolutions put to the owner, and chosen:**

1. **The test files join P2:**
   * `detect_unix_test.go`: `fakeHost` also points the resolver's directories at its fake
     directory, which covers its four users;
   * `internal/updateclient/managed_script_unix_test.go`: the same, through an exported
     `OverrideSupervisorToolDirs`;
   * `supervisor_unix_test.go` and `scriptbackend_unix_test.go`: only the expected calls and exit
     keys that gain `--`, by 3.

   No assertion is loosened. The alternatives put were a test-only fallback to `PATH`, a workaround
   that tests a lookup production never uses, and stopping P2.
2. **The runner also passes `XDG_CONFIG_HOME` when it is set.** A test pins it. P4's container run
   checks it with a non-default value.
3. **`--` goes before the service name for `rc-service` only.** The s6 and runit tools' arguments are
   absolute paths, and `rc-update`'s acceptance is unprobed.

**Files added to P2:** `internal/cli/service/detect_unix_test.go`, `supervisor_unix_test.go`,
`scriptbackend_unix_test.go`, and `internal/updateclient/managed_script_unix_test.go`.

### P2, 2026-10-26: the command layer

**The new code** (`internal/cli/service/supervisortool.go`):

* `supervisorTools` is the thirteen tools P1 probed. `supervisorToolDirs` is `/usr/sbin`, `/usr/bin`,
  `/sbin`, `/bin`, in `toolPath`'s order.
* `resolveTool(name)` returns the first executable file of that name in those directories. It never
  consults `PATH`, refuses a name not on the list, and its error names every directory searched.
* `toolEnv` is the built environment: `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, `LC_ALL=C`, the runtime
  directory (as `withUserRuntimeEnv` derives it), `HOME`, `USER`, `XDG_CONFIG_HOME` when set
  (Deviation 2), then a call's extra entries.
* `toolCall.run(ctx)` runs the resolved tool through `toolRunner`, by default the library's
  `ExecRunner`, so a cancelled context kills it and both streams are captured.
  * A non-zero exit is a `*libservice.CommandError`: `Backend` the kind, `Step` the tool and its
    arguments, `Name` the service directory or name, `ExitCode`, and `Message` from stderr, or
    stdout when that is empty, cut at 4 KiB.
  * `classifyTool` wraps it with `ErrNotInstalled`, `ErrNoSupervisor`, `ErrPermission` or
    `ErrTimeout` by P1's table, or with nothing.
* `runBounded` gives callers with no context, setup-service and the samplers, `cmdTimeout`, as
  `runCmd` did. `OverrideSupervisorToolDirs` lets tests outside the package fake the tools.

**The change, by site:**

| Site | Before | After |
| :--- | :--- | :--- |
| `detectLinux` | `lookPath` (`exec.LookPath`) for runit, s6 and OpenRC; `runCmdOutput("rc-service", "--user", "--help")` | `haveTools` (the resolver); the same probe through the runner |
| `startScript`, `removeScript`, `startOpenRC` (`supervisor.go`) | `runCmd`, `runCmdEnv`: output streamed to the terminal, inherited environment, no context | `runBounded`, captured; `rc-service --user -- <name>` (Deviation 3) |
| `scriptBackend.Stop`, `Start` | `runCmd`, `runCmdEnv`, the context ignored | `toolCall.run(ctx)` |
| the runit, s6 and OpenRC samplers (`settle.go`) | `runCmdOutput`, stdout and stderr combined | `runBounded`; runit and s6 parse stdout; OpenRC reads "status: crashed" from both streams, since P1 found it on stderr |
| `startSessionSupervisor` | the scanner found on `PATH` | at the resolved path; still detached, with its environment as before |
| `runCmdEnv` (`setup.go`) | called only on these paths | deleted |

* **What stays.** `lookPath` still finds `launchctl` and `systemctl`. `runCmd` still streams for
  `systemctl`, `launchctl` and `loginctl`, which are not script kinds.
* **The Verification check holds:** `git grep -n -e 'exec.LookPath' -e 'os.Stdout' -- internal/cli/service`,
  outside tests, finds only `lookPath`, `plutil`, `systemctl`, `launchctl` and `runCmd`'s stream.
  No supervisor tool goes through `runCmd`, `runCmdOutput` or `runCmdEnv` any more.
* **What an operator sees change.** setup-service no longer echoes the tools' own lines, such as
  OpenRC's "* Starting mcremote ... [ ok ]". It prints its own result as before, and a failure's
  error now carries the `CommandError`'s kind, step, exit code and message.

**The tests:**

| Test | Pins |
| :--- | :--- |
| `TestResolveToolNeverReadsPATH` | the directories in order, a non-executable file skipped; a tool only on `PATH` not found, the error naming every directory; a program not on the list refused |
| `TestToolRunnerBuildsItsEnvironment` | the absolute path, and exactly the built environment, with and without `XDG_CONFIG_HOME`; nothing else of the caller's |
| `TestToolRunHonoursItsContext` | a tool still running when the context ends is killed, and the run returns `context.Canceled` |
| `TestToolOutputNeverReachesTheTerminal` | a tool's stdout and stderr are captured and returned, and neither reaches this process's |
| `TestToolFailuresAreClassified` | thirteen rows of P1's table, verbatim: the `CommandError`'s fields, and each class or none |
| `TestDetectionAndTheUpdateResolveAlike` | with runit in the fixed directories and s6 on `PATH`, detection says runit, and the update runs runit's `sv` from there |

The existing tests pass, adapted only by Deviation 1:

* the fakes find their state beside themselves, from `${0%/*}`;
* `fakeHost` and `runitHost` point the resolver at their fakes;
* four expected `rc-service` calls and one exit key gain `--`.

**Seen failing first** (`m212/p2_plants.py`, session scratchpad). Each plant ran on its own scratch
copy of the working tree. All nine fail on their named tests:

| Plant | The failure |
| :--- | :--- |
| the resolver falls back to `exec.LookPath` | "resolveTool(s6-svc) = "…/s6-svc"; want an error: it is only on PATH" |
| the caller's `PATH` | "missing ["PATH=/usr/sbin:/usr/bin:/sbin:/bin"]; unexpected (names only) ["PATH"]" |
| the caller's whole environment | "unexpected (names only) [" and the caller's variable names |
| `XDG_CONFIG_HOME` dropped | `…/XDG_CONFIG_HOME_set`: "missing ["XDG_CONFIG_HOME=/home/u/cfg"]" |
| `context.Background()` for the run | "the run took 30.056887958s after its context ended" |
| the output copied to `os.Stdout` | "the tool's output reached the terminal: stdout " * Starting mcremote ... [ ok ]\n"" |
| `classifyTool` classifies nothing | "errors.Is(err, selfupdate: service: not installed) = false", and each class after it |
| detection back on `lookPath` | "detectLinux = s6; want runit, whose tools are where the update resolves them" |
| the OpenRC sampler without `--` | `TestOpenRCCrashedIsFailed`: "sample {up:true … failed:true …}, want failed and not up" |

**The plants found a hazard in a test, now closed.** In the whole-environment plant's first run,
`TestToolRunnerBuildsItsEnvironment` printed the leaked environment's values, and with them a session
token from the agent's own environment. A regression in CI would print the runner's secrets into the
CI log the same way.

* **The fix.** The test now reports a difference through `envDiff`: the expected entries it misses,
  which the test set itself, and only the *names* of the entries it did not expect.
* **The re-run.** The three environment plants were run again, and fail with names only, as above.
* **Clean-up.** The plant's log, and two older scratch logs that held the same value, were deleted,
  and no file in the scratchpad or the tree holds it.

**Gates** (`m212/p2_gates.py`):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the twelve changed and added Go files | "12 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 44 packages ok |
| P2's tests and the script-kind setup, backend, settle and update tests on the Linux test host, verbose | 66 `--- PASS` lines, tests and subtests; no failure or skip. The copy was deleted |

**The first gate run failed on the tree's own guard, now fixed.** `make test` failed in
`internal/testexec`'s `TestRunFixturesAreWarmed`:
"internal/cli/service/supervisortool_test.go:58 writes a #!/bin/sh script by hand" (MADR 0193 D5).

* **Cause.** It was the resolver test's decoy: a file that is never run, written non-executable so
  the resolver skips it. The guard is about the `#!/bin/sh` literal, whether or not the script runs.
* **The fix.** The decoy now holds "not executable" and no shebang. It was never meant to run, so
  nothing it tests changed.
* **The re-run.** The guard and the resolver test pass, and every gate above is from the second run.

P2 is complete once the owner commits it. P3 is next, on the owner's word.

### The library, assessed 2026-10-26, at the owner's ask

The owner committed P2 and asked to "assess the current state of the go-selfupdate-lib sdk sibling
repo to ensure it aligns with this work". The sibling checkout was read only.

* **The pin is current.** go-selfupdate-lib's newest tag is still `v1.22.0`. Its `main`, at
  `96115f8`, is nine commits past it. Those commits touch only docs, CI, the records tooling and
  three test files, and no production code. `supervised.go`, `errors.go` and `runner.go` are as
  `v1.22.0` has them.
* **A fix to the adapter is in progress there, uncommitted.** The library's 0035-MADR Amendment A3
  and its PLAN's phase M11 carry it, decided by its owner as "fix the bug now":
  * In `v1.22.0`, `Supervised.Stop` runs its restoring `Up`, after a stop that failed with the
    process still running, on the stop's own context. The wait has always spent that context, so
    the `Up` never starts its command, and the service is left down and not wanted up. The wait's
    timeout error also loses its last observation.
  * The fix gives `Up` a context of its own, so a failed `Stop` can take twice `Poll.Timeout`. It
    also counts `Poll.Timeout` from before `Down`, so a `Down` that blocks spends part of it.
    `v1.22.1` releases it, on its owner's word.
* **What it means here:**
  * **Stage 1, P2 to P4, is unaffected.** It uses `SupervisedState`, `CommandError` and the
    sentinels, which are unchanged, and nothing calls `Supervised.Stop` until P5.
  * **P5 should not adopt the adapter at `v1.22.0`.** A hung OpenRC or runit daemon whose stop
    fails would be left down. With the bound from before `Down`, OpenRC's `Down`, which P1 measured
    blocking 48 s, leaves 27 s of D17's 75 s for the wait. That still covers it.
  * **Proposed, for the owner at P5:** an amendment moving the pin to `v1.22.1` before P5, as 0207's
    U1b moved it to `v1.22.0`. It is not part of P3.
* **P2 and P3 agree with the library's guide** (`extending-selfupdate.md`, the supervised section):
  * a `State` that finds no supervisor returns an error wrapping `ErrNoSupervisor`, with
    `Installed` and `NormallyUp` still set, which P2's classification and P3 provide;
  * `Ready` stays nil for s6 without a `notification-fd` (D18).
  * The adapter takes any other `State` error as a failure, so P3 reports a missing definition as
    `Installed: false`, not as an error.

### P3 Deviations 1 and 2, 2026-10-26 — the s6 fakes, and the replaced process's start token. The owner decided both as recommended

**Found by measuring before any edit:**

* **1. Three tests feed s6's human form.** D7-A reads s6 with
  `s6-svstat -o up,wantedup,normallyup,ready,pid,exitcode,updownfor`. These tests print the human
  form ("up (pid 81 pgid 81) 40 seconds") through their fakes:
  * `supervisor_unix_test.go:775` (`TestSetupNotesTheProcessARestartReplaces`);
  * `scriptbackend_unix_test.go:232` (`supervise`);
  * `settle_test.go`'s `TestSamplersReadTheirManagers`, whose s6 rows test the human-form regular
    expression.

  Step 1 says the existing settle tests "pass unedited", and the first two files are not on P3's
  list.
* **2. The replaced process is known by PID alone.** D12-A has `settle` compare PID and start token.
  `settle.go` can do that for a restart while watched. Its other PID check, which skips the process
  a restart replaces, reads `res.replacedPid`. That is recorded at `supervisor.go:439` and declared
  at `setup.go:142`, neither on P3's list.

**The resolutions put to the owner, and chosen:**

1. **The three test files join P3.**
   * Their s6 fakes print the `-o` form P1 captured.
   * `TestSamplersReadTheirManagers`' s6 rows parse `-o` lines into the same expected samples.
   * The human-form s6 expression is deleted, since nothing reads it. No expected value changes.

   The alternatives put were two s6 parsers, a workaround, and stopping P3.
2. **Both checks compare the start token.**
   * `Result` gains `replacedToken` beside `replacedPid` (`setup.go`), and `startScript` records it
     with the PID (`supervisor.go`).
   * `settle` skips the replaced process only when PID and token both match.

   The alternative put was the watched restart alone.

**Files added to P3:** `internal/cli/service/supervisor_unix_test.go`, `scriptbackend_unix_test.go`,
`settle_test.go`, `setup.go` (the `Result` field) and `supervisor.go` (recording it).

### P3, 2026-10-26: state, in the library's type

*Note, 2026-10-26: P4 Deviation 4a corrects P3's runit reader for a service with a log service. Its
fixtures had none.*

**The new code** (`internal/cli/service/supervisorstate.go`):

* **`observe(ctx, kind, def, name)`** returns an `observation`: a `libservice.SupervisedState`,
  plus the uptime `settle` needs, which the library's type does not carry. P5's `State` is
  `observe`'s state, wrapped where P5 first calls it.
* **What every kind shares:**
  * `Installed` is the definition's existence.
  * A missing definition is `Installed: false` and no error, as the library's adapter needs (the
    assessment above).
  * No supervisor is `ErrNoSupervisor`, with `Installed` and `NormallyUp` still set.
* **runit:** `sv status <dir>`, parsed by `parseRunitStatus`.
  * "run: … (pid N) Ns" is up, with that PID and uptime, and wanted up unless ", want down".
  * "down: … want up" is `WantUp` and `Restarting`.
  * `NormallyUp` is the absence of `<dir>/down`.
* **s6:** `s6-svstat -o up,wantedup,normallyup,ready,pid,exitcode,updownfor <dir>` (D7-A), parsed
  by `parseS6Status`.
  * Up, wanted up and the PID come from their fields, and the uptime from `updownfor`.
  * `Restarting` is down and wanted up. `ready` is read into `Detail` and not mapped, so `Ready`
    stays nil (D18).
  * `NormallyUp` is the down file's absence, which also answers when no supervisor does.
  * Seven fields or an error.
* **OpenRC:** `rc-service --user -- <name> status`.
  * Exit 0 is up and wanted up, 3 is stopped, and 32 is `Failed`.
  * No session, and no runtime directory, are `ErrNoSupervisor`, by P2's classification.
  * `NormallyUp` is the `default` runlevel link.
  * **The PID** (`openRCPID`) is the pidfile's, in the runtime directory only, with no `/tmp`
    fallback (D12-A). When the process it names is `supervise-daemon`, the D10-A form P1 measured,
    the PID is that process's child, read from a scratchable `/proc` root (`stateProcRoot`,
    `statParent`).
* **`startToken`** wraps `procutil.ProcessStartToken`, as a variable for tests.

**The change, by site:**

| Site | Before | After |
| :--- | :--- | :--- |
| the runit, s6 and OpenRC samplers (`settle.go`) | each tool's output, parsed per kind; OpenRC sampled with no PID | `observe`, through `sampleOf`: up, PID, uptime, `exited` from `Restarting`, `failed` from `Failed`, and the start token |
| `settle`'s check for a restart while watched | the PID changed | the PID changed, or stayed the same with another start token |
| `settle`'s skip of the replaced process | the PID matched | the PID and, where both were read, the token matched |
| `startScript` (`supervisor.go`), `Result` (`setup.go`) | `replacedPid` | `replacedPid` and `replacedToken` |
| `scriptBackend.Running`, `supervisedPID` (`scriptbackend.go`) | the sampler; OpenRC's pidfile tried in `XDG_RUNTIME_DIR`, the user's runtime directory, then `/tmp` | `observe`, with the caller's context; no `/tmp` |
| `readServeArgs` (`health.go`) | the systemd unit only, on Linux | a script kind's definition through `recoverScript` (D4-A) |
| `runitStatusRe`, `s6StatusRe`, `downWantUpRe`, `parseStatus` | `settle.go` | `runitStatusRe` moved to `supervisorstate.go`; the rest deleted, since nothing reads them |

* **What a sample now says that it did not.** An OpenRC sample carries the daemon's PID. So
  `settle` now sees an OpenRC restart while it watches, which it could not before. No sample that was
  up is now down, or the other way.
* **One D12-A item stays for P4.** The rendered OpenRC script still reads
  `pidfile="${XDG_RUNTIME_DIR:-/tmp}"…`. The reader no longer falls back, and `openrc --user` refuses
  to run without `XDG_RUNTIME_DIR` (P1), so `/tmp` is never where the file is. The rendered line is
  the definition, which P4 rewrites for D10-A, so it changes there, in one refresh rather than two.

**The tests:**

| Test | Pins |
| :--- | :--- |
| `TestStateReplaysTheProbes` | 24 of P1's fixtures, from `testdata/supervisor/`, through each kind's state: runit's up, down, wanted up, down file both ways, wanted down, no supervisor, missing definition and permission; s6's `-o` forms of the same; OpenRC's stopped, crashed, no session, no runtime directory and missing definition. It checks the whole state, the uptime and a nil `Ready` |
| `TestOpenRCStartedReadsThePIDFile` | started: the pidfile's PID; under supervise-daemon, started and restarted: the supervisor's child |
| `TestOpenRCPIDFileHasNoTmpFallback` | a pidfile of the service's name in `/tmp`, with none in the runtime directory, gives PID 0 |
| `TestSettleTellsAReusedPIDByItsStartToken` | the same PID with a new token while watched is a restart; a new process with the replaced PID counts; the replaced process itself is still skipped |
| `TestSamplerCarriesTheStartToken` | the sampler's sample is the state's, with the up process's token |
| `TestReadServeArgsReadsScriptDefinitions` | runit, s6 and OpenRC definitions, rendered by setup's template, give the baked serve arguments |

* **The fixtures.** `testdata/supervisor/runit-2.3.1/`, `s6-2.13.2.0/` and `openrc-0.62.6/` hold 27
  of P1's captures: each command's stdout byte for byte in `<state>.stdout`, and its command, exit
  code and stderr in `<state>.json`. The containers' test account is written as `/home/<user>`, as the
  tree's guard requires (`internal/homepaths_test.go`); nothing parses those paths.
* **Deviation 1's test changes, as chosen:**
  * the s6 fakes print `-o` lines;
  * `TestSamplersReadTheirManagers`' s6 rows are `-o` lines with the same expected samples, and its
    helpers call `parseRunitStatus`, `parseS6Status` and `sampleOf`;
  * `TestSetupS6RescansAndRestarts` expects the `s6-svstat -o …` call it now makes;
  * `TestSetupNotesTheProcessARestartReplaces` also checks `replacedToken`.

**Seen failing first** (`m212/p3_plants.py`, session scratchpad). Each plant ran on its own scratch
copy of the working tree. All fourteen fail on their named tests:

| Plant | The failure |
| :--- | :--- |
| s6's `ready` mapped to `Ready` | `…/s6-2.13.2.0/o-up`: "Ready = false; D18 leaves it nil" |
| runit's `Restarting` not set | `…/runit-2.3.1/down-wantup`: `Restarting:false` |
| `NormallyUp` ignores the down file | `…/up-normallydown`, `…/down-downfile`, and s6's: `NormallyUp:true` |
| a missing definition is an error | `…/runit-2.3.1/absent`: "observe: selfupdate: runit: sv status …: exit 1: fail: …" |
| exit 32 not `Failed` | `…/openrc-0.62.6/crashed`: `Failed:false` |
| no supervisor loses the state | `…/s6-2.13.2.0/o-nosupervisor`: `Installed:false … NormallyUp:false` |
| supervise-daemon's PID taken for the daemon's | `TestOpenRCStartedReadsThePIDFile/sd-started`: "PID:306 …; want up, pid 307" |
| the pidfile read from `/tmp` too | "PID = 4242, read from /tmp/p3probe….pid; want 0: there is no /tmp fallback" |
| a restart while watched ignores the token | "settle = running, ""; want a restart while watched" |
| the replaced process told by PID alone | "settle = failed, "it was not seen running in 5s"; want running: PID 81 is a new process" |
| the replaced process never skipped | "settle took the process the restart replaces for the new one" |
| the sampler drops the token | "sample {… token:}, want {… token:token-of-20}" |
| setup records no token | "replacedToken = "", want process 81's" |
| `readServeArgs` ignores the script kinds | each kind: "read the mcrelay unit: open …" |

One plant was miswritten on its first run, and rebuilt. Removing the watched-restart case left
`lastToken` unread, so the copy did not compile, and a failure to build proves nothing. The plant
now keeps the comparison and never takes it, and it fails as above.

**The first gate run failed three ways, each fixed:**

* **`make staticcheck`:** "internal/cli/service/supervisorstate.go:51:6: func stateOf is unused
  (U1000)". The tree's staticcheck does not count a test as a caller. `stateOf` is gone, and P5 wraps
  `observe` where it first calls it.
* **`make pre-add-check`:** `supervisorstate_test.go` was not gofmt'd. It is now.
* **`go test -race ./...` on the Linux test host:** `TestTrackedFilesUseOnlyPlaceholderHomes`, once
  per fixture: the home path's account "is not a placeholder: write <user>".
  * The fixtures held the containers' test-account home paths.
  * The Linux copy tracks every file, so the guard saw them there. Here they were untracked, and it
    did not.
  * The fixtures are now written with `/home/<user>` by the copy script, and no fixture names the
    test account.
  * The guard passes here with P3's new files marked as intended (`git add -N`).
* **The plants were run again** on the final code, and all fourteen fail as above.

**Gates** (`m212/p3_gates.py`, the second run):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the ten changed and added Go files | "10 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, every file tracked | exit 0; 44 packages ok, `TestTrackedFilesUseOnlyPlaceholderHomes` included |
| P3's and P2's tests, and the script-kind setup, backend, settle and update tests, on the Linux test host, verbose | 89 `--- PASS` lines, tests and subtests; no failure or skip. The copy was deleted |

**Found at staging, after the gates: the fixtures' stdout files were ignored.** They were first
named `<state>.out`, and `.gitignore:14` ignores `*.out`. Every gate passed only because the files
were on disk. The Linux copy's `git add -A` honours `.gitignore` too, so a commit would have left the
replay test without its input.

* **The fix.** They are `<state>.stdout`, which no rule ignores (`git check-ignore` exits 1), and the
  test reads that.
* **The check.** The staged index, exported with `git checkout-index -a` into a scratch directory,
  passes `internal/cli/service`, `internal/updateclient` and `internal/testexec`, fixtures included.

P3 is complete once the owner commits it. P4, the definitions, is next, on the owner's word. Its
container run needs the owner's word too.

### P4 Deviation 1, 2026-10-26 — how the renderer learns OpenRC's version. The owner decided: "Probe the version (Recommended)"

*Superseded 2026-10-26 by P4 Deviation 2: `notify` is dropped, and the probe with it.*

**Approval.** The owner committed P3 and answered "committed, proceed". Steps 1, 2 and 4 run now.
Step 3, the containers, waits for the owner's word, as the rules provide.

**Found while writing step 2.**

* D10-A sets `notify=socket:ready` "on OpenRC 0.62 or later", so the renderer must know the host's
  version. The plan does not say how, and P1 did not probe `openrc --version`.
* A probe run at render time adds an `openrc --version` call. Tests that pin OpenRC's exact calls see
  it, and one of them, `scriptbackend_unix_test.go`, is not on P4's list.

**The resolutions put to the owner:**

1. **Chosen:** render `notify` only when `openrc --version` reads 0.62 or later, and leave it out when
   the output cannot be read.
   * A script without `notify` starts on any OpenRC; one with it starts on none older.
   * Step 3 checks that the real output parses.
   * `scriptbackend_unix_test.go` joins P4, to stub the probe, as `supervisor_unix_test.go`'s
     tests do.
2. Always render `notify`. An OpenRC 0.60 or 0.61 host would get a script whose start fails.
3. Never render it. `rc-service start` would return before the daemon is ready, on every host.

**File added to P4:** `internal/cli/service/scriptbackend_unix_test.go`.

### P4, 2026-10-26: the definitions, steps 1, 2 and 4

**runit and s6 (D9-A).** The run script is unchanged. Beside it, setup now writes:

* `log/run`, the log service: `svlogd -tt` for runit, `s6-log T` for s6, into
  `$XDG_STATE_HOME/<product>/log`. It sets `PATH` to the supervisor tools' fixed directories (D2-A),
  and makes the log directory with `mkdir -p -m 0700` at each start.
* for s6, `timeout-kill` holding `45000`.

**OpenRC (D10-A).** The script uses OpenRC's default functions:

* `supervisor=supervise-daemon`, `command`, `command_args`, `directory`;
* the environment through `supervise_daemon_args`, as `--env` pairs;
* `pidfile="${XDG_RUNTIME_DIR}"'/<name>.pid'`, with no `/tmp` fallback, which closes the D12-A item
  P3 left here;
* `output_log` and `error_log`, both `$XDG_STATE_HOME/<product>/log/<name>.log`, as `2>&1` does;
* `respawn_delay=5`, `respawn_max=30`, `respawn_period=300` (the unit's `StartLimitBurst=30` in 300 s)
  and `retry="TERM/45/KILL/5"`;
* `notify="socket:ready"` when `openrc --version` reads 0.62 or later (Deviation 1);
* a `start_pre` that runs `checkpath -d -m 0700` on the log directory.

**Two design points the plan did not settle, and how P4 settles them:**

* **`supervise-daemon.sh` passes `command`, `command_args`, `directory`, the logs and
  `supervise_daemon_args` through `eval`.** So each value is shell words, quoted once more for the
  assignment. A word the shell reads as itself stays bare, so a plain path stays readable.
  * Sourced as `openrc-run` sources it, and `eval`'d as `supervise-daemon.sh` does, every word comes
    back byte for byte. That includes the parity options' `/opt/my apps/it's/mcremote`, a
    `TOKEN=a 'quoted' $value` and an `EMPTY=` (`m212/p4_eval_check.py`, session scratchpad), under
    macOS's `/bin/sh` and the Linux test host's (gates).
  * That OpenRC's own `supervise-daemon.sh` evals exactly so is step 3's to confirm.
* **The definitions make their log directory, not setup.** A setup that made it would write into
  `$XDG_STATE_HOME` outside the definition. The package's tests do not isolate `XDG_STATE_HOME`, so
  every setup test would have made one in the home of whoever runs them. A log directory removed
  later would also stop the logger. `log/run`'s `mkdir` and OpenRC's `start_pre` make it at each
  start instead.

**Setup, refresh and remove:**

* **Setup** writes the files beside the run script with it, and `Unchanged` means they are unchanged
  too. A `log/run` without setup's managed marker is refused without `--force`, and replaced with it.
* **Refresh (D15-A).**
  * It reads both OpenRC forms: today's start-stop-daemon form, and the supervise-daemon form.
  * A pre-P4 install moves on. For runit and s6, the missing files are added, with no backup, since
    the run script is unchanged. For OpenRC, the script is rewritten, and the old one kept as the
    backup.
  * A foreign `log/run` keeps the definition: `VerdictKept`, naming it.
* **Remove** also stops s6's log service and its supervisor (`s6-svc -d`, `-x` on `<dir>/log`). The
  service directory goes as before, the log service in it.
  * *For the owner to confirm:* the log files under `$XDG_STATE_HOME/<product>/log` stay, as a
    removed systemd unit's journal does. The plan's "`removeScript` removes the `log` directory too"
    is read as the log service's directory.

**The tests** (step 1; `scriptdefs_unix_test.go`, and the guard in `definitions_once_test.go`):

| Test | Pins |
| :--- | :--- |
| `TestDefinitionsMatchTheirGoldens` | seven forms byte for byte, in `testdata/definitions/`: runit's and s6's run scripts, their `log/run`, s6's `timeout-kill`, and the OpenRC script with and without `notify` |
| `TestOpenRCDefinitionIsD10A` | D10-A's lines by name, `notify` only when OpenRC takes it, and none of the start-stop-daemon form |
| `TestRecoverScriptReadsBothForms` | each kind's form from before P4 (`*.pre-0212.golden`, rendered by `HEAD` before P4) and its new form recover to the same options and environment, and the old one re-renders to the new |
| `TestSetupWritesTheFilesBesideTheScript` | runit and s6: the files written; a second setup `Unchanged`; a foreign `log/run` refused without `--force`, replaced with it |
| `TestRefreshBringsAnInstallFromBefore0212On` | each kind's pre-P4 install refreshed onto the new form, OpenRC's backup the old script, and a second refresh unchanged |
| `TestRemoveStopsS6sLogService` | s6's removal stops both services, then the directory goes |
| `TestOpenRCNotifyFollowsTheVersion` | `notify` for 0.62.6, 0.63 and 1.0.1; none for 0.61.3, 0.60, unreadable output, a failed run or no `openrc` |
| `TestFilesBesideTheScriptsNeverRunTheBinary` | G2: no file beside a run script names the binary |

* **The existing tests,** adapted for the new forms:
  * the G2 guard reads OpenRC's start command from `command=`;
  * `scriptEnvAndArgv` reads the supervise-daemon form;
  * the refresh test's "older script" drops `XDG_CACHE_HOME` from `supervise_daemon_args`;
  * `withOpenRCNotify` pins the version probe in `supervisorHost`, `scriptBackendHost` and the guard.
* **Two of my mistakes, caught before staging:**
  * **Goldens carrying the account name.** `TestTrackedFilesUseOnlyPlaceholderHomes` failed on four
    goldens: the home path's account "is not a placeholder".
    * The runit and s6 scripts' comment names the service directory from this process's XDG
      directories, not the pinned environment. So a render on this Mac named this Mac's home.
    * The goldens, both new and pre-P4, are now rendered with `HOME` and the XDG variables at the
      placeholder home (`placeholderHome`). No golden names a real home.
    * The comment's source is unchanged; it is the renderer's, from before 0212.
  * **A shebang in a test.** A test's foreign `log/run` was written with a `#!/bin/sh` literal, which
    MADR 0193 D5's guard refuses. It is never run, and now has none.

**Seen failing first** (`m212/p4_plants.py`, session scratchpad). Each plant ran on its own scratch
copy of the working tree. All fourteen fail on their named tests, and were run again on the final
code:

| Plant | The failure |
| :--- | :--- |
| recovery reads the new OpenRC form only | "the form from before P4 does not recover: carries a line setup-service never writes: start-stop-daemon --start …" |
| runit has no log service | "forms [… runit s6 s6.log-run s6.timeout-kill], want [… runit runit.log-run …]" |
| s6's `timeout-kill` at 30000 | `…/s6.timeout-kill` differs from its golden |
| the log directory not made at start | `…/runit.log-run` and `…/s6.log-run` differ from their goldens |
| no `supervisor=supervise-daemon` | "no line "supervisor=supervise-daemon"", with and without `notify` |
| `retry` at 30 s | "no line "retry=\"TERM/45/KILL/5\""" |
| `notify` always | "notify line present = true, want false" |
| no `start_pre` | "no line "start_pre() {"" |
| `notify` from 0.61 | `…/0.61.3`: "openRCSupportsNotify = true, want false" |
| `command` quoted once, not for `eval` | `TestScriptMatchesTheSystemdUnit/openrc-user`: the binary lost from the argv |
| setup replaces a foreign `log/run` | "setup over an operator's log/run: <nil>; want a refusal naming --force" |
| refresh ignores the files beside the script | runit and s6: "Verdict:unchanged"; want refreshed |
| remove leaves s6's log service | the calls without the two on `<dir>/log` |
| a `log/run` naming the binary | "… log/run names the binary", each product and kind |

**Gates** (`m212/p4_gates.py`):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the five changed and added Go files | "5 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, every file tracked | exit 0; 44 packages ok, the home-path and fixture guards included |
| P4's tests, and the script-kind setup, refresh, remove, backend and update tests, on the Linux test host, verbose | 89 `--- PASS` lines, tests and subtests; no failure or skip |
| the `eval` check under the Linux test host's `/bin/sh` | "every word survives the eval, byte for byte". The copy was deleted |

**Not done in P4 yet.** Step 3, the containers, on the owner's word. A fresh install and a
`--refresh` from the current form, in each container, and with them:

* `openrc --version`'s real output read as 0.62, so `notify` is rendered;
* OpenRC's own `supervise-daemon.sh` evals the values as the shell check does, with a binary path
  holding a space;
* `checkpath` makes the log directory as the user;
* the log services write `current` under runit and s6;
* s6 and OpenRC kill a TERM-ignoring daemon at 45 s, and OpenRC restarts a crashed one;
* a refresh over a running start-stop-daemon-started daemon, then a restart under the
  supervise-daemon form;
* the P2 Deviation 2 check: `XDG_CONFIG_HOME` at a non-default directory, where `rc-service --user`
  finds the script setup wrote.

### P4 step 3, 2026-10-26: the containers, first run

**Approval.** The owner committed P4's steps 1, 2 and 4 and answered "committed, proceed", the word
for step 3. `m212/p4_drive.py` and `m212/p4_in.py` (session scratchpad) ran it in the 0210 kit's three
images on local Podman, linux/arm64. Two builds: P4 (`d07bca78`) and the commit before it
(`caa96e43`). Raw output: `m212/p4-<kind>.jsonl`. Every container was removed after, and the Podman
machine stopped again.

| Check | runit | s6 | OpenRC |
| :--- | :--- | :--- | :--- |
| A, then B: an install by the build before P4, refreshed by P4 | `log/run` added, no backup; the daemon from A still ran, same PID | `log/run` and `timeout-kill` added, no backup; the daemon from A still ran | the script rewritten, the old one kept as the backup; the daemon from A still ran |
| C: the update's restart after the refresh | the daemon restarted; **the log service did not start**: "unable to open supervise/ok" (Deviation 3) | the daemon restarted; **the log service did not start**: "s6-supervise not running" (Deviation 3) | the start-stop-daemon form's daemon stopped in 0.179 s; the new form started in 0.014 s, the daemon's parent `supervise-daemon`; `checkpath` made the log directory, 0700, and the log was written |
| D: `--remove` | every file gone: `run`, `log/run`, the backup | every file gone, `timeout-kill` too | the script and its backup gone |
| D: then a fresh install | 6.05 s; the log service up; `current` written; the directory 0700 | 1.01 s; the log service up; `current` written; the directory 0700 | **failed at once**: "mcremote-p4: bind: Address in use" (Deviation 2) |
| E: OpenRC's start, and a crash restarted | — | — | the start failed the same way; the crash restart was **not reached** |
| F: a daemon that ignores TERM | after `sv down`, "want down, got TERM"; `sv kill` ended it | down **45.27 s** after `s6-svc -d`, "down (signal SIGKILL)" | setup's restart blocked to its 35 s bound; the stop after it ran past the probe's 120 s (Deviation 2). The 45 s kill was **not measured** |
| G: `XDG_CONFIG_HOME` elsewhere | — | — | the script written at the other location, the start found it, and the service ran |

* `openrc --version` printed "openrc (OpenRC [PODMAN]) 0.62.6", which the parser read as 0.62.
  Deviation 2 removes the parser.
* C's daemon ran from `/home/<user>/p4/my apps/mcremote`. The path with a space survived OpenRC's own
  `supervise-daemon.sh` `eval`.
* G's `rc-service status` read "started" under either `XDG_CONFIG_HOME`. That tells the locations
  apart from nothing: OpenRC finds a started service through `$RC_SVCDIR/started/<name>` before
  its script directories (`src/librc/librc.c:748`, 0.62.6). The start, which had no such link,
  found the script at the other location.
* **A mistake of mine, caught before recording.** The driver's digest printed "present" for a file
  the raw output records as `false`, and so first read the removal as leaving files. The raw lines
  show every file gone.

### P4 Deviations 2 and 3, 2026-10-26 — OpenRC's `notify`, and adopting a log service. The owner decided both as recommended

**Deviation 2: OpenRC's `notify=socket:ready` cannot be used.** Found in step 3, and explained from
OpenRC 0.62.6's source. Its master branch, read 2026-10-26, is unchanged.

* `supervise-daemon` binds `$RC_SVCDIR/supervise-<service>.sock` before it forks the daemon
  (`src/shared/misc.c:557-566`, called with the service's name at
  `src/supervise-daemon/supervise-daemon.c:1088`).
* After `READY=1` it unlinks `supervise-<program>.sock`, built from its own program name
  (`supervise-daemon.c:1253`, `misc.c:612`). The service's socket stays. `start-stop-daemon` has the
  same mismatch (`src/start-stop-daemon/start-stop-daemon.c:636` and `:1213`).
* So a service's first start works, and every later one fails at once. That is step 3's D and E,
  after C's start under the new form.
* The wait for `READY=1` has no timeout (`misc.c:587-619`). A daemon that never sends it blocks the
  start for good, as F's stand-in did.
* P1's probes used a new service name for each, so none started twice.

The resolutions put to the owner:

1. **Chosen:** drop `notify`.
   * Readiness comes from setup's check after the start and the update's health wait, as on runit
     and s6.
   * `openRCSupportsNotify`, its version probe and their tests go, and Deviation 1 is superseded.
   * Recovery still reads a script with `notify`, so a refresh moves a P4 script off it.
2. Drop it, and keep the version gate, set past every release, for an upstream fix. No shipped
   OpenRC could reach that code, so nothing could test it.

Removing the leftover socket in `start_pre` was not offered. It rests on a path internal to OpenRC,
and leaves the start unbounded.

**Deviation 3: runit and s6 do not start a log service added to an existing install.** Step 3's C:
after the refresh, the update's restart left the log service unsupervised. runsv and s6-svscan read
a service's `log` directory only when they first take the service up.

* **Asked twice.** The owner first chose "Refresh adopts it": refresh restarts the service's
  supervision.
  * Before any code, I found that the update runs refresh as its reconcile step, between stopping
    the old daemon and starting the new one (`internal/updateclient/managed.go:34`). The installer
    hook also runs it before its own stop and start (`installerhook.go:149`).
  * A restart inside refresh would start the daemon mid-update, ahead of the update's own start
    and its checks. I put the choice to the owner again.
* **Chosen:** "At Magic's next start".
  * When `log/run` exists and its log service is not supervised, setup's start (`startScript`) and
    the script backend's `Start` restart the service's supervision: runit `sv exit`, then a wait
    for runsvdir's new runsv; s6 `s6-svc -x`, then `s6-svscanctl -an`.
  * On the update path the daemon is already down there, so no restart is added. Refresh stays
    write-only, as its contract says.
  * P5 carries it into the adapter's start command.
* The commands are probed in the containers before any code. The owner's answer approves that run,
  and the re-run of step 3.

**File added to P4:** `internal/cli/service/scriptbackend.go`. Its test joined with Deviation 1.

**What these change elsewhere.** P1's `notify` row held for one start per service. Its line "P4:
D10-A, with `notify=socket:ready` available from 0.62" no longer holds. The MADR's
[amendment C](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md#amendment--2026-10-26-c-openrcs-notify-and-adopting-a-log-service)
records both.

**Still to do in P4:** the adoption probe, the code and tests of both deviations, the gates, and
step 3 again in all three containers. OpenRC's crash restart and 45 s kill are measured in that run.

### P4 Deviation 3's probe, 2026-10-26: which commands adopt a log service

`m212/p4b_drive.py` and `m212/p4b_in.py` (session scratchpad) ran in the runit and s6 images. Each
probe installs with the build before P4, refreshes with P4, and then adopts. It runs once with the
daemon stopped first, as the update's cycle does (U), and once with it running, as setup's start
does (S). Raw output: `m212/p4b-<kind>.jsonl`, and `p4b-s6-rescan-first.jsonl`.

| Kind, order | U: the log service supervised | S | The daemon's output in `current` |
| :--- | :--- | :--- | :--- |
| runit, `sv exit` | 3.8 s, by a new runsv | 5.1 s | yes, both |
| s6, `s6-svc -x`, then a rescan | 60.4 s | 60.5 s | **no**, both: `current` empty |
| s6, a rescan, then `s6-svc -x` (`-dx` for S) | 0.003 s | 0.25 s | yes, both, within 0.5 s |

* In the first s6 order, s6-svscan started the service's s6-supervise again at once, before the log
  service existed. The daemon came back with no pipe to the logger. The rescan, 60 s later, started
  the logger alone.
* In both kinds the new supervisor started the daemon itself, since there was no `down` file. The
  `sv up` or `s6-svc -u` after it found the daemon up.

### P4 Deviations 2 and 3, 2026-10-26: what was built

* **Deviation 2.** The `notify` line, `openRCSupportsNotify`, its version probe and its test are
  gone. The golden `openrc-user.no-notify.golden` is now `openrc-user.golden`. `openRCFixedLines`
  still reads the line, so a refresh moves a P4 script off it (`TestRefreshMovesAScriptOffNotify`).
* **Deviation 3.** `adoptLogService` (`supervisor.go`) takes a log service up, in the order the
  probe found.
  * It runs only when `log/run` exists and the log service is not supervised.
  * runit: `sv exit`, then a wait until the log service answers. s6: a rescan, a wait for the log
    service, `s6-svc -dx`, then a wait for a new s6-supervise that answers. Each wait is bounded by
    `adoptTimeout`, 60 s.
  * `startScript` calls it over a supervised service; when it adopts, setup brings the daemon up
    with `sv up` or `s6-svc -u` rather than restarting it again. `scriptBackend.Start` calls it
    before its up command.
  * The guide names the commands for a person who restarts the service by hand.
* **The plants** (`m212/p4d_plants.py`): eleven, each on its own scratch copy. All failed on their
  named tests, after one fix of mine below.

| Plant | The failure |
| :--- | :--- |
| `notify` rendered again | "the supervise-daemon form has "notify"" |
| recovery drops the `notify` line | the refresh of a script with it: `Verdict:kept` |
| the backend's start skips adoption | calls `[sv up …]`, want `[sv exit …` |
| setup's start skips adoption | calls `[… sv restart …]` |
| setup restarts again after adopting | calls `[… sv exit … sv restart …]` |
| s6: `-x` before the rescan | "s6-svscan did not start its log service"; calls `[s6-svc -dx …]`, want `s6-svscanctl -an` first |
| runit does not wait | "adoptLogService: <nil>; want a timeout naming the log service" |
| s6 does not wait for a new s6-supervise | "adoptLogService: <nil>; want a timeout naming s6-supervise" |
| "not supervised" read as an error | "logServiceSupervised = false, selfupdate: runit: sv status …/log: exit 1" |
| s6's PID matched by a prefix of the directory | "s6SupervisorPID = 10 with only the log's and another process; want 0" |
| adoption with no `log/run` | "adoptLogService = true, … runsvdir did not start its log service" |

* **A mistake of mine, caught by the plants.** The three timeout tests first bounded the adoption
  at 100 ms. That bound covers the fake tools' own runs too, which take longer on macOS. So the s6
  test timed out on the fake `s6-svstat`, not on the missing new s6-supervise, and passed with the
  wait planted out. The bound is now 2 s (`adoptTestTimeout`), and the plant fails.

### P4 step 3, 2026-10-26: the containers, second run

Step 3 again, with the working tree's build. C now runs the update's cycle through
`setup-service --from-installer`: the refresh, the script backend's `Stop` and `Start`, then the
verified start. The first run used the raw supervisor commands. H is new: setup with P4 over a
refreshed install from before P4. Raw output: `m212/p4r-<kind>.jsonl`.

| Check | runit | s6 | OpenRC |
| :--- | :--- | :--- | :--- |
| C: the update's cycle after the refresh | **failed**: "read whether the mcremote service is running: … nothing supervises the service" (Deviation 4a) | **failed** after 1.0 s: the log service's status right after the rescan, "No such file or directory", exit 111 (Deviation 4b) | worked; the daemon's parent `supervise-daemon`; the log written |
| D: `--remove`, then a fresh install | every file gone; the log service up, `current` written | the same | the script and backup gone; the fresh install started |
| E: repeated starts, and a crash | — | — | starts 2, 3 and 4 of one service all worked; a `SIGKILL`ed daemon back in **5.14 s** |
| F: a daemon that ignores TERM | `sv kill` ended it | down **45.04 s** after `s6-svc -d`, by `SIGKILL` | `rc-service stop` took **50.1 s**, the daemon gone |
| G: `XDG_CONFIG_HOME` elsewhere | — | — | as in the first run |
| H: setup with P4 over a refreshed install | adopted; the daemon replaced; `current` has its output | the log service up, but **`current` empty** (Deviation 4b) | — |

### P4 Deviation 4, 2026-10-26 — runit's state with a log service, and s6's adoption test. The owner decided both as recommended

**4a: P3's runit state reader misreads a service that has a log service.**

* With the log service unsupervised, `sv status <dir>` exits 1. It prints the service's status,
  then "; warning: <dir>/log: unable to open supervise/ok: file does not exist". The classifier
  reads the whole output as `ErrNoSupervisor`, so the update's `Running` failed (C).
* With it supervised, `sv status` exits 0 with
  "run: <dir>: (pid 55) 0s; run: log: (pid 54) 0s". `runitStatusRe`'s greedy `.*` takes the log's
  PID, 54, as the daemon's. That reaches `Inside` and the replaced-process check, and the
  adapter in P5.
* P3's fixtures were captured with no log service, so they had neither line.
* **Chosen:** "Fix it in P4".
  * Read only the service's own segment, the text before "; ".
  * Classify a failure only when that segment is not a status line.
  * Add fixtures of the two outputs, from this run, with the home path redacted.
  * The other choice put to the owner was the same fix recorded as a P3 follow-up.

**4b: on s6, a supervised log service does not mean the daemon writes to it.**

* Setup's own `s6-svscanctl -a`, run before the adoption check, took the log service up while the
  daemon's s6-supervise kept its old output (H). Any other rescan does the same. And the first
  status read after a rescan can fail while the log's supervisor starts (C).
* **Chosen:** "The pipe itself".
  * Adopt unless the daemon's s6-supervise writes into the pipe the log's s6-supervise reads:
    `/proc/<pid>/fd/1` against `/proc/<pid>/fd/0`, each process found by its working directory.
  * The waits treat a supervisor still starting as not ready yet.
* The other choice put to the owner was to decide before setup's rescan. Any other rescan would
  then still leave the daemon unlogged, with no error.

**Files added to P4:** `internal/cli/service/supervisorstate.go`, its test, and fixtures under
`testdata/supervisor/runit-2.3.1/`. The owner's answer approves the third run of step 3.

### P4 Deviation 4, 2026-10-26: what was built

* **4a.** `observeRunit` reads `runitOwnStatus`: `sv status`'s output up to "; run: log: ",
  "; down: log: ", "; finish: log: " or "; warning: ".
  * A failure counts only when that segment is not a status line ("run: ", "down: ", "finish: ").
  * Three fixtures join `testdata/supervisor/runit-2.3.1/`, each a real output from these runs with
    the home path redacted: `up-log` (the probe's U), `up-lognosupervisor` (the second run's C,
    exit 1) and `run-wantdown-log` (the second run's F). `TestStateReplaysTheProbes` reads them.
* **4b.** `s6LoggerConnected` compares `/proc/<pid>/fd/1` of the service's s6-supervise with
  `/proc/<pid>/fd/0` of the log service's: the same `pipe:[…]`. `s6SupervisorPID` finds each by its
  working directory.
  * On s6, adoption runs unless the two are connected.
  * The wait after the rescan looks for the log service's s6-supervise as a process, not its
    status.
  * The wait after `s6-svc -dx` looks for the connection, then a status that answers. A PID
    compare was dropped: the old s6-supervise was not on the pipe, and only one started after the
    rescan is, so the connection already proves a new one.
  * `logServiceSupervised` stays for runit, where the one runsv runs both.
* **The tests:** `TestS6LoggerConnectedComparesThePipes`; a subtest where s6's log service runs but
  not for the daemon; `s6Logged` for the setup tests over a supervised s6 service. All are in
  `scriptdefs_unix_test.go` and `supervisor_unix_test.go`.
* **The plants** (`m212/p4d_plants.py`): fifteen, each on its own scratch copy. All failed on their
  named tests. Deviation 3's "s6 does not wait for a new s6-supervise" became "s6 does not wait for
  the pipe", and four are new:

| Plant | The failure |
| :--- | :--- |
| s6 does not wait for the pipe | "adoptLogService: <nil>; want a timeout naming s6-supervise" |
| s6 adopts only an unsupervised log service | `…/s6's_log_service_runs,_but_not_for_the_daemon`: not adopted |
| s6's pipes compared by kind only | `TestS6LoggerConnectedComparesThePipes/another_pipe` |
| runit reads the whole status line | `…/runit-2.3.1/up-log`: the log's PID, 54, for the daemon's 55 |
| runit takes the log's warning for its own | `…/runit-2.3.1/up-lognosupervisor`: `ErrNoSupervisor` |

* **A mistake of mine, caught by the plants.** The runit "does not wait" plant failed to compile once
  4b moved `logUp` into runit's case. It is now written to compile, and fails its test.

### P4 step 3, 2026-10-26: the containers, third run

The same driver as the second run, with the working tree's build after Deviation 4. Raw output:
`m212/p4t-<kind>.jsonl`.

| Check | runit | s6 | OpenRC |
| :--- | :--- | :--- | :--- |
| C: the update's cycle after the refresh | 5.03 s; the log service adopted; `current` has the daemon's output | 4.05 s; the same | 1.24 s; the daemon under `supervise-daemon`; logged |
| D: `--remove`, then a fresh install | every file gone; logged | every file gone; logged | the script and backup gone; logged |
| E: repeated starts, and a crash | — | — | starts 2, 3 and 4 worked; a crash back in **5.40 s** |
| F: a daemon that ignores TERM | `sv kill` ended it | down **45.05 s** after `s6-svc -d`, by `SIGKILL` | `rc-service stop` **50.14 s**, the daemon gone |
| G: `XDG_CONFIG_HOME` elsewhere | — | — | the start found the script there; the service ran |
| H: setup with P4 over a refreshed install | adopted; the daemon replaced; logged | adopted; the daemon replaced; logged | — |

Every container was removed, and the Podman machine stopped again.

### P4 Deviations 2 to 4, 2026-10-26: the gates

`m212/p4_gates.py`, over the eight changed Go files and three records, on the final code:

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the eight Go files | "8 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, every file tracked | exit 0; 44 packages ok, the home-path and fixture guards included |
| P4's tests, the new ones included, and the script-kind setup, refresh, remove, backend and update tests, on the Linux test host, verbose | 112 `--- PASS` lines, tests and subtests; no failure or skip |
| the `eval` check under the Linux test host's `/bin/sh` | exit 0. The copy was deleted |

**P4 is complete** once the owner commits it.

### Owner decisions, 2026-10-27, on the questions P4 left open

* **`--remove` and the log files: "Keep them".** Removal keeps `$XDG_STATE_HOME/<product>/log`, as
  a removed systemd unit keeps its journal. Its output is to name the kept directory, and the guide
  how to delete it. That lands in P4b.
* **The library pin: "Pin v1.22.1 first".** P5 starts by moving the pin to go-selfupdate-lib
  `v1.22.1` once its owner tags it, after the tag's contents are checked. `v1.22.0`'s
  `Supervised.Stop` restores the service on a spent context and leaves it down (the library's 0035
  amendment A3). The MADR's D8 is to be amended with P5.
* **The log directory: a per-unit directory, in a P4b phase after P4's commit.** See P4b above.
* **How P5 keeps the log-service adoption on the update path: held.**
  * First answer: "Ask the library for a hook".
  * Then I found that `SupervisedOptions.Up` is already a function Magic supplies. The adapter's
    `Start` calls it, as does its restore after a failed stop (go-selfupdate-lib
    `selfupdate/service/supervised_lifecycle.go:112` and `:174`, read-only). I put that to the
    owner.
  * The owner's answer, verbatim: "we need to look at this from the perspective of moving the
    service stack out of magic and into the selfupdate library."
  * So P5 is held until that assessment, and the decision it leads to, which may change this
    record's direction. P4 and P4b stand either way: they change what Magic writes today.

### The assessment, 2026-10-27

The owner asked, verbatim: "assess the current state of the go-selfupdate-lib functionality
regarding services. evaluate what is already built in magic-cli-remote. i want to keep the library
as lean and idempotent as possible. find the best way to ensure magic-cli-remote will leverage
go-selfupdate-lib without building too many functions and features into the library."

* It was read-only. The library was surveyed at `v1.22.1` and `20e7cc7`, leaving its uncommitted
  work untouched. This repository was read at `f5082636`.
* The facts and decisions are the MADR's amendment D:
  * D19: nothing moves into the library;
  * D20: pin `v1.22.1`;
  * D21: adopt the log service in Magic's `Up`;
  * D22: 0207's U6 after P5.
* The owner then said: "committed, write the 0212 amendment and 0207 U6 note". Amendment D, the P5
  note above, and the U6 note in
  [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md) are
  that writing. They wait for the owner's approval.
* The owner approved them, verbatim: "approved to proceed". P5 runs as amended.

### P5 step 0, 2026-10-27: the tag, read before the bump

* **The tag:** `v1.22.1` is annotated tag `56f5f20e`, whose commit is
  `4d9ae417f61c4cdd0a7bccd1d342d773d9cc34cd`. Read with `git ls-remote origin` from the library's
  clone.
* **What changed against `v1.22.0`** (`git diff --stat v1.22.0 v1.22.1`): 29 files.
  * One non-test Go file, `selfupdate/service/supervised_lifecycle.go`: 27 lines in, 20 out. The
    restore after a failed `Stop` calls `Up` on its own `Poll.Timeout`, and the stop's bound
    counts from before `Down`. "A failed Stop can take twice" `Poll.Timeout`.
  * The rest are tests, records (its 0035 amendment A3, and a docs-gates record numbered 0036),
    guides, CI and scripts.
* **Library workflows:** `build-selfupdate-release.yml` and `publish-selfupdate-release.yml` are
  byte-identical between the tags.
* **For P5:** a failed stop can now take up to 150 s, twice D17's 75 s. `adoptLogService`'s 60 s
  bound fits inside `Up`'s own 75 s.

### P5 Deviation 1, 2026-10-27 — the pin moves `ci.yml` too. The owner decided: "Add ci.yml to P5 (Recommended)"

* **Found at step 0.** `TestWorkflowPinsMatchGoMod` (`internal/updateclient/spec_test.go:132-138`)
  requires `.github/workflows/ci.yml`'s two library workflow pins (`:711`, `:732`) to name the
  release `go.mod` requires, as MADR 0054 D1 does. `ci.yml` is not on P5's list.
* **The resolutions put to the owner:**
  1. **Chosen:** `ci.yml` joins P5 for those two lines only. The SHA moves from `84805563…` to
     `4d9ae417…`, and the comment from `v1.22.0` to `v1.22.1`.
  2. A separate 0207 pin step, a U1c, committed before P5's code.
* **File added to P5:** `.github/workflows/ci.yml`, those two lines.

### P5 Deviation 2, 2026-10-27 — the runit update test runs real processes, on Linux. The owner decided: "Real processes, Linux only (Recommended)"

* **Found** once the adapter was in, run on this Mac and on the Linux test host. Only
  `TestUpdateRestartsARunitService` (`internal/updateclient/managed_script_unix_test.go`, not on
  P5's list) failed, on both:
  * "a mcremote service is installed, but its service manager cannot be reached:
    MCREMOTE_SVC_WAIT_SECS=3 is not above the 10s settle window" (`:121`, `:133`, `:146`). Its bound,
    `:59`, is below D17's.
  * Its fake `sv` invents PIDs no process has (`:31`, `:41`, `:83`). The adapter takes an instance
    only from a live `/proc/<pid>` (the library's `supervised.go:202-211`). Its health wait ignores
    an empty instance once a previous one is set (`poll.go:39-41`). So with any valid bound it
    would wait out 75 s.
  * Its `unix` build tag runs it on macOS, where the library builds no adapter
    (`supervised.go:153-156`).
* **The resolutions put to the owner:**
  1. **Chosen:** rename the file `managed_script_linux_test.go`, and give it real processes.
     * The fake `sv up` starts `/bin/sleep` and records its PID. `sv down` kills it, and cleanup
       kills any left.
     * `MCREMOTE_SVC_WAIT_SECS=15`. *Corrected while writing it: 50. On runit D17 also requires a
       bound above the 45 s `KillAfter`, so 15 is refused.*
     * The four cases stay.
  2. Give the managed update a fake `Lifecycle` in place of the adapter, and keep the test portable.
* **Deleting the test** was not offered. It is the only test of the update's whole cycle on runit.
* **File added to P5:** that test file, renamed.

### P5, 2026-10-27: the adapter

**Step 0, the pin.** `go.mod` and `go.sum` move to go-selfupdate-lib `v1.22.1`, and `ci.yml`'s two
library workflow pins move with them (Deviation 1). `TestWorkflowPinsMatchGoMod` passes.

**The backend** (`supervisedbackend.go`, new). For runit, s6 and OpenRC, `NewBackendWith` now
returns the library's `*libservice.Supervised`, built by `newSupervisedBackend`:

| Option | What Magic hands the adapter |
| :--- | :--- |
| `State` | P3's `observe`, its `SupervisedState` as it is |
| `Up` | `supervisedUp`, in D21's order: a session supervisor when none watches; then, on runit and s6, `adoptLogService` when `State` reports the service down; then `sv up`, `s6-svc -u`, or `rc-service --user -- <name> start` (OpenRC starting the user session first, as before) |
| `Down` | `supervisedDown`: `sv down`, `s6-svc -d`, `rc-service --user -- <name> stop`, with no wait |
| `Kill`, `KillAfter` | runit only: `sv kill <dir>`, at 45 s (D17) |
| `Poll` | `supervisedPoll`: `Timeout` 75 s and `Settle` 10 s. `MCREMOTE_SVC_WAIT_SECS`, when set, is the timeout, refused when not above 10 s, or on runit not above 45 s |
| `Probe` | `HealthOptions.backendProbe`, the product's check, as on every backend |
| `DetachCheck` | OpenRC only: `openRCDetachCheck` (D3-B). It refuses when this process and the daemon share a cgroup, read per hierarchy from `/proc/<pid>/cgroup`, whose last element starts with `openrc.` |

* **Deleted:** `scriptbackend.go`, `scriptbackend_inside_linux.go`, `scriptbackend_inside_other.go`
  and `scriptbackend_unix_test.go`. With them go `scriptBackend`, its `Inside`, `parentOf`,
  `procParent`, `maxAncestry`, `stopTimeout` and the script `WaitHealthy`.
  * The library's adapter now does the inside check, the stop's wait and kill, the health wait and
    the handoff.
  * `settle` stays, for setup's start (D6-A).
* **`IsInstalled`** reads the definition through `scriptInstalled`, the check the deleted
  `scriptBackend.Installed` made.
* **The update hands off on these supervisors.** The adapter is a `Detacher`, so `update` takes the
  handoff path there, as on systemd. `refuseInside` stays for any backend that is not one.
  * The help text says so, and names OpenRC's refusal under a shared OpenRC cgroup.
  * `client.go`'s comments on the backends that cannot hand off are brought up to date.
  * `ops-linux-install.md` says the same, with the bounds and the kill.
  * `command_test.go`'s comments still name the script backend as its example of one that cannot
    hand off. That file is not on P5's list, and its tests pass unchanged.

**The tests:**

| Test | File | Pins |
| :--- | :--- | :--- |
| `TestSupervisedPollIsD17` | `supervisedbackend_unix_test.go` | 75 s and 10 s; the variable's bounds per kind; a value that is not a number refused |
| `TestSupervisedOptionsPerKind` | the same | `Kill` and 45 s on runit alone, running `sv kill <dir>`; `DetachCheck` on OpenRC alone; the product's probe, and none without one |
| `TestSupervisedDownStopsAndDoesNotWait` | the same | each kind's stop command, alone |
| `TestSupervisedUp` | the same | `sv up` through a watching runsvdir; a session s6-svscan; OpenRC's session, then `rc-service start` |
| `TestSupervisedUpAdoptsALogService` | the same | runit and s6: adopted on a service that is down, then up; a second `Up` adopts nothing; on a service that is up, the restore, nothing is adopted |
| `TestSupervisedCommandsPrintNothing` | the same | `Up`, `Down`, `State` and `Kill` write nothing to stdout, the update's `--json` stream |
| `TestOpenRCDetachCheckIsD3B` | the same | refused in a shared `openrc.` cgroup, cgroup v2 and a v1 hybrid; allowed in `0::/` as P1 measured, in another service's OpenRC cgroup, in a shared cgroup that is not OpenRC's, and with the daemon down |
| `TestIsInstalledReadsTheScriptDefinition` | the same | each kind's definition found, and not once it is gone |
| `TestSupervisedOptionsAreAccepted` | `supervisedbackend_linux_test.go` | `NewSupervised` accepts each kind's options, and refuses s6's with a `KillAfter` |
| `TestNewBackendFollowsTheRecord` | the same | the adapter, a `Detacher`, for each recorded script kind; the systemd backend otherwise; a refused bound fails the build |
| `TestProbeStatusOnRunit` | the same | doctor through the adapter: installed, enabled, not running, and the `sv up` hint |
| `TestUpdateRestartsARunitService` | `internal/updateclient/managed_script_linux_test.go` (Deviation 2) | the update's whole cycle on runit, over real processes: restarted; started when enabled; left stopped when disabled; rolled back on a crash loop |

* **The setup tests** are unchanged. `setup-service` still starts through `startScript` and
  confirms through `settle` (D6-A).
* **On the Linux test host,** every test above passes, verbose. The runit update test takes 41.6 s:
  two settle windows, and the rollback's.

**Seen failing first** (`m212/p5_plants.py`, session scratchpad). Fourteen plants, each on its own
scratch copy of the working tree. Two ran on the Linux test host, where the adapter is built. All
failed on their named tests:

| Plant | Where | The failure |
| :--- | :--- | :--- |
| s6 given a `KillAfter` | this Mac | `TestSupervisedOptionsPerKind/s6`: "KillAfter = 45s, want none" |
| the same, against the library | Linux | `TestSupervisedOptionsAreAccepted/s6`: "SupervisedOptions.KillAfter is set, but Kill is not" |
| runit with no `Kill` | this Mac | "Kill set = false; want it on runit only" |
| no application check | this Mac | "options {… Probe:<nil> …}: want the name, State, Up, Down and the probe" |
| OpenRC with no `DetachCheck` | this Mac | "DetachCheck set = false; want it on OpenRC only" |
| the check always allows | this Mac | `…/a_shared_OpenRC_cgroup`: "openRCDetachCheck = <nil>; want refused true" |
| the check refuses any shared cgroup | this Mac | `…/the_root_cgroup,_as_P1_measured`: refused in `/` |
| runit's bound not above 45 s | this Mac | "runit, MCREMOTE_SVC_WAIT_SECS=45: … <nil>; want a refusal" |
| a bound not above the settle window | this Mac | "s6, MCREMOTE_SVC_WAIT_SECS=10: … <nil>; want a refusal" |
| `Up` adopts on a service that is up | this Mac | `…/up:_the_restore_adopts_nothing`: an adoption attempted, then timed out |
| `Up` never adopts | this Mac | `…/down:_adopted,_then_up`: "calls [sv status … sv up …]" |
| `Up` prints | this Mac | `TestSupervisedCommandsPrintNothing/runit`: "stdout got "up\n"" |
| a missing definition read as installed | this Mac | "IsInstalled after removal = true, <nil>; want false" |
| runit's `Down` does nothing | Linux | `TestUpdateRestartsARunitService/running_is_restarted`: "managed install failed" |

* **Two mistakes of mine, caught before they counted:**
  * `TestSupervisedOptionsPerKind` would have called a nil `Kill` and panicked under the "no `Kill`"
    plant. It now guards the call, and the plant fails it cleanly.
  * One plant did not compile ("declared and not used: fi"). It was rewritten to the one
    replacement it needs, and fails its test.

**Gates** (`m212/p5_gates.py`, over the seven changed and added Go files and five records):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the seven Go files | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, every file tracked | exit 0; 44 packages ok |
| `internal/cli/service` and `internal/updateclient`, verbose, on the Linux test host | 639 `--- PASS` lines; no failure; one skip, `TestLiveReplaceFirstProbeFailure`, a live test that runs only when its variables are set |

**P5 is complete** once the owner commits it. Next by this record: P4b and P6, each on the owner's
word; then 0207's U6, after P5 (D22).

### P4b, 2026-10-27: start, and step 1

**Approval.** The owner committed P5 (`5c499ad0`) and answered "committed, proceed". P4b is next by
the record, and that answer is the word for step 1's reproduction.

**Files named at the phase's start** (P4b's list leaves them to be named): the removal output is
`PrintRemoveResult` in `internal/cli/service/result_print.go`. The kept directory it is to print
needs a field on `Result`, in `internal/cli/service/setup.go`.

**Step 1, the reproduction** (`m212/p4c_drive.py`, `m212/p4c_in.py`, session scratchpad).
* Run in the runit and s6 images, local Podman, with the build of `5c499ad0`.
* Two services of one product were set up, `mcremote-a` on port 17661 and `mcremote-b` on 17662,
  each with its own config and data directory. Both log services render
  `exec svlogd -tt '<home>/.local/state/mcremote/log'` (`s6-log T` on s6). Raw output:
  `m212/p4c-<kind>.jsonl`.

| After 8 s | runit | s6 |
| :--- | :--- | :--- |
| `mcremote-a`'s log service | "run: …/mcremote-a/log: (pid 19) 16s" | "up (pid 17 pgid 17) 10 seconds" |
| `mcremote-b`'s log service | "down: …/mcremote-b/log: 1s, normally up, want up" | "down (exitcode 111) 0 seconds, normally up, want up" |
| `svlogd` or `s6-log` processes | one, in `mcremote-a/log` | one, in `mcremote-a/log` |
| the shared `current` | `mcremote-a`'s lines only (17661), none of `mcremote-b`'s | the same |

* The reading is confirmed: the second logger cannot take the directory, and its supervisor
  restarts it without end. The second daemon's output reaches no file.
* The loggers' own messages were not captured: `podman logs` was empty in both.
* **Not measured:** the second daemon writes into a pipe its supervisor holds open with no reader.
  Once that pipe's buffer fills, about 64 KiB of output, the daemon may block on its own logging.

### P4b Deviation 1, 2026-10-27 — how an existing install moves to its own directory. The owner decided: "Next start restarts the logger (Recommended)"

* **Found while designing step 2.** An install from P4 or P5 runs a logger writing to
  `<product>/log`. A refresh rewrites its `log/run` to `<product>/log/<unit>`. But a running logger
  keeps the directory it started with.
  * Neither of Magic's starts restarts it: they restart the daemon, and adopt a log service only
    when none runs (P4 Deviation 3).
  * The adapter's `Up` is in `supervisedbackend.go`, which postdates P4b's file list.
* **The resolutions put to the owner:**
  1. **Chosen:** setup's start and the adapter's `Up` restart the log service alone, with
     `sv restart <dir>/log` or `s6-svc -r <dir>/log`.
     * This happens only when `log/run` is newer than that service's start, read from the uptime
       the state readers already parse.
     * The daemon is untouched. The supervisor holds the pipe, so no output is lost.
     * After one restart the times agree, so it does not run again.
  2. The refresh restarts the logger when it rewrites `log/run`. Refresh would then no longer be
     write-only, which amendment D's C3 relies on.
  3. New installs only. Existing installs keep the shared directory until a
     `setup-service --force`.
* **Files added to P4b:** `internal/cli/service/supervisedbackend.go` and its tests.
* **The log files under the old `<product>/log`** stay where they are, as removal keeps logs (owner
  decision, 2026-10-27). The guide says where they are.

### P4b, 2026-10-27: steps 2 to 4

**What changed** (`supervisor.go` unless named):

* **`unitLogDir`:** runit's and s6's `log/run` log into `$XDG_STATE_HOME/<product>/log/<unit>`.
  * The unit's name is the service directory's.
  * `mkdir -p -m 0700` names both the product's log directory and the unit's, so both are private,
    as the single directory was.
  * OpenRC's `<unit>.log` in the product's directory is unchanged.
  * The goldens `runit.log-run.golden` and `s6.log-run.golden` changed in those two lines only.
    P4's forms are kept as `*.log-run.p4.golden`, for the refresh test.
* **`restartStaleLogService`** (Deviation 1) restarts the log service alone: `sv restart <dir>/log`
  or `s6-svc -r <dir>/log`.
  * It runs only when the logger is up and `log/run` is newer than its start: now, less the uptime
    the readers parse.
  * Setup's start calls it when it adopted nothing, and so does the adapter's `Up` on a service
    that is down (`supervisedbackend.go`).
* **`--remove` names the kept logs.**
  * `keptLogs` finds the unit's directory, the product's (an install from before P4b), or OpenRC's
    `<unit>.log`. It reads them from the definition's own pinned environment, before the
    definition goes.
  * `Result.KeptLogs` (`setup.go`) carries it, and `PrintRemoveResult` (`result_print.go`) prints
    "Logs kept: <path> (delete them yourself when no longer needed)".
* **The guide** gives the per-unit directory, why each unit needs its own, how an older install
  moves, and the new `tail -f` paths.

**The tests** (step 2):

| Test | File | Pins |
| :--- | :--- | :--- |
| `TestEachUnitLogsIntoItsOwnDirectory` | `scriptdefs_unix_test.go` | two units, two directories, both made private; OpenRC's `<unit>.log` unchanged |
| `TestRefreshMovesLogRunToTheUnitsDirectory` | the same | P4's `log/run` rewritten to the unit's directory; a second refresh unchanged |
| `TestRestartStaleLogService` | the same | restarted when newer; not when older, when the logger is down, or with no log service; never the daemon's service |
| `TestSetupRestartsAStaleLogService` | the same | setup's path: the log service restarted, then the daemon, as before |
| `TestRemoveNamesTheKeptLogs` | the same | each kind: none named with no logs; the kept path named, and still there, with them |
| `TestSupervisedUpRestartsAStaleLogService` | `supervisedbackend_unix_test.go` | `Up` restarts it on a service that is down, and not on one that is up |

* `restartStaleLogService` is a variable for that last test. The fakes print one output per tool,
  so they cannot show the daemon down and its logger up at once.
* `TestSetupRunitRestartsThroughTheUsersSupervisor` and `TestSetupS6RescansAndRestarts` now see
  the check's status read. Their logger is not up, so nothing is restarted.

**Seen failing first** (`m212/p4bx_plants.py`). Ten plants, each on its own scratch copy, run on
this Mac. All failed on their named tests:

| Plant | The failure |
| :--- | :--- |
| one directory for every unit | "runit mcremote-a: log/run does not make and log into …/log/mcremote-a"; "both units' log/run are the same" |
| the product's directory not made private | the same test: the `mkdir` line names only the unit's |
| refresh leaves `log/run` | `TestRefreshMovesLogRunToTheUnitsDirectory/runit`: "log/run after the refresh" differs |
| restart when older | "restartStaleLogService = false, <nil>; want true" and "= true … want false" |
| restart a logger that is down | `…/runit_the_logger_not_up`: "= true, <nil>; want false" |
| restart the daemon, not the logger | the calls show `sv restart <dir>`, not `<dir>/log` |
| setup does not restart the logger | `TestSetupRestartsAStaleLogService`: the calls lack `sv restart …/log` |
| `Up` does not restart the logger | "log service restarts [], want […/mcremote]" |
| remove names no logs | "Remove: KeptLogs "", <nil>; want …/log/mcremote" |
| the output omits them | "output … does not name …" |

* **Three mistakes of mine, caught before they counted:**
  * The first `TestRemoveNamesTheKeptLogs` wrote a file where the log directory goes.
  * It also installed with `writeDefinition`, which pins a placeholder home. The fix installs
    through `Setup` in a scratch home, as a real install does.
  * `TestRestartStaleLogService`'s "logger not up" case would have passed with the up check
    planted out: a down logger's start reads as now. Its `log/run` is now dated in the future.

**Step 4, the containers** (`m212/p4d_drive.py`, `m212/p4d_in.py`). Builds: old, P5's `5c499ad0`;
new, the working tree. Raw output: `m212/p4d-<kind>.jsonl`.

| Check | runit | s6 |
| :--- | :--- | :--- |
| A: two services of one product at once | both loggers up (pids 19 and 43); `log/mcremote-a/current` has 17661 only, `log/mcremote-b/current` 17662 only; both directories and `log` 0700 | the same (pids 17 and 41) |
| B: `--remove` of each | "Logs kept: …/log/mcremote-a" and "…/mcremote-b"; both directories still there | the same |
| C: an install by P5's build, refreshed, then the update's cycle (`--from-installer`) | `log/run` rewritten to `log/mcremote`; the logger restarted (pid 86, then 126); the daemon replaced (87, then 129); `log/mcremote/current` has its lines; the old `log/current` kept | the same (95, then 144; daemon 94, then 134) |
| H: the same, then `setup-service` with the new build | the logger restarted (160, then 189); `log/mcremote/current` has the daemon's lines; the old `log/current` kept | the same (176, then 209) |

**Step 5, the gates** (`m212/p5_gates.py`, over the seven changed Go files and three records):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the seven Go files | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, every file tracked | exit 0; 44 packages ok |
| `internal/cli/service` and `internal/updateclient`, verbose, on the Linux test host | 662 `--- PASS` lines; no failure; one skip, the opt-in live `TestLiveReplaceFirstProbeFailure` |

**P4b is complete** once the owner commits it. Next by this record: P6, the live tests in the tree,
on the owner's word; then 0207's U6 (D22).

### P6 Deviation 1, 2026-10-27 — where the update comes from, the probe agent, and the runner. The owner decided all three as recommended

**Approval.** The owner committed P4b (`5cacbd01`) and answered "committed, proceed", the word for P6
and its runs.

**Found at the phase's start.** The 0210 kit, in the session scratchpad's `m54/d3/kit/`, does the
update from inside in three steps:

1. It pairs a client through `scripts/smoke-protocol`, which is in the tree.
2. That client prompts a probe agent, a stand-in ACP agent the daemon spawns.
3. The agent runs `mcremote update --yes --force --version <v>`, which downloads a published
   release.

The probe agent is the kit's own Go module. P6's list names neither it nor how a Go test runs
inside a container. The latest release is `v0.23.0`, from before 0212. Its own refresh leaves the
new definitions alone: on OpenRC it reports "kept".

**The decisions:**

1. **The release: "A pinned real release".** `update --yes --force --version v0.23.0`, from GitHub,
   as the kit did. CI passes `GITHUB_TOKEN` against rate limits.
   * The test ends with `/proc/<pid>/exe` no longer the test build, and with the result's
     `replaced_before_stop`, `service_started` and `rolled_back` as 0207-MADR D36 asks.
   * The other choice put to the owner was a fake release server. It would need a hidden
     release-source override in `update`, a product change.
2. **The probe agent: "In the main module".** `test/supervisors/probeagent/main.go`, a `package
   main` here. go.mod already requires acp-go-sdk2, so there is no new dependency. The other
   choice was its own module, with pins and gates of its own.
3. **The runner: "Built on the host, run inside".**
   * `make live-supervisors` runs `scripts/live_supervisors.py`. The script builds the images, then
     cross-compiles `mcremote`, `smoke-protocol`, the probe agent and the live test binary
     (`go test -c`) for Linux.
   * It copies them into each container, and runs the test as the container's `user` with
     `MCREMOTE_REQUIRE_<KIND>=1`. Podman runs it locally, Docker on the CI runner.
   * The other choice was a Go toolchain in each image.

**Files added to P6:** `test/supervisors/probeagent/main.go` and `scripts/live_supervisors.py`.

### P6 Deviation 2, 2026-10-27 — OpenRC's log directory on a fresh home. The owner decided: "mkdir the parent, then checkpath (Recommended)"

* **Found** by the first OpenRC live run. `TestLiveOpenRC/install` failed: "mcremote-live is not
  running after setup: it was not seen running in 15s".
* **The cause.** In a debug container, `/sbin/openrc` was wrapped to log each call setup made. It
  showed `openrc --user default` printing
  `checkpath: /home/<user>/.local/state/mcremote/log: could not open .local: No such file or directory`,
  then "ERROR: mcremote-live failed to start".
  * P4's `start_pre` runs `checkpath -d -m 0700 <logdir>`, which makes no missing parents.
  * On a home with no `~/.local/state`, the first start fails. Setup writes its own records under
    `~/.local` only after that start.
  * P4's container runs always had `~/.local` already, from an earlier step. runit's and s6's
    `log/run` use `mkdir -p` and are unaffected.
* **The resolutions put to the owner:**
  1. **Chosen:** `start_pre` runs `mkdir -p '<XDG_STATE_HOME>/<product>'`, then
     `checkpath -d -m 0700 '<logdir>'` as now. The parents get the user's defaults, the log
     directory is private, and checkpath still corrects its mode.
  2. `mkdir -p -m 0700 '<logdir>'` alone, as runit and s6 do. A log directory that already exists
     with a wider mode would no longer be corrected.
* **Files added to P6:** `internal/cli/service/supervisor.go`, `testdata/definitions/openrc-user.golden`
  and `scriptdefs_unix_test.go`.

### P6 Deviation 3, 2026-10-27 — OpenRC's state has reported the supervisor's PID. The owner decided: "Match the kernel's comm (Recommended)"

* **Found** by the OpenRC live run after Deviation 2. `TestLiveOpenRC/install` failed on its
  `/proc/<pid>/exe` check: "the daemon runs 9ee1e74e…; want mcremote, 03140eb7…".
* **The cause.** P3's `openRCPID` (`supervisorstate.go:260`) takes the pidfile's process for
  `supervise-daemon`, and then its child, only when `/proc/<pid>/comm` reads `"supervise-daemon"`.
  * The kernel keeps 15 characters (`TASK_COMM_LEN`), so it reads `"supervise-daemo"`, as P4 step 3
    recorded ("the daemon's parent: supervise-daemo").
  * So the comparison never held, and since P3 OpenRC's `State` has reported the supervisor's PID.
  * P3's fixture (`supervisorstate_test.go:232`) used the untruncated name, which no `/proc`
    shows.
  * **The effect:** the adapter's instance is the supervisor's, so a daemon `supervise-daemon`
    keeps restarting could look like one stable instance to the health wait. The inside check is
    unaffected, since it reaches the daemon through the supervisor either way.
* **The resolutions put to the owner:**
  1. **Chosen:** compare with the name as `/proc/<pid>/comm` gives it, its first 15 characters.
     The fixture takes the real value.
  2. Identify the supervisor by `/proc/<pid>/exe`'s base name. That is a different read than P3
     measured.
* **Files added to P6:** `internal/cli/service/supervisorstate.go` and `supervisorstate_test.go`.

### P6 Deviation 4, 2026-10-27 — the live plants. The owner decided: "Two live plants, one by unit test (Recommended)"

* **Found** before step 2. Of the plan's three live plants, only "a `Down` that does nothing" can
  work as written.
  * **"`Detach` with the hop dropped from the spec"** is the library's code: Magic builds no hop.
    Without it, the detached run is still started in a session of its own, which on runit and s6
    may well succeed. So it may never fail, and prove nothing.
  * **"OpenRC's `DetachCheck` made to allow"** cannot fail in a container. The daemon and the
    update are both in `0::/` (P1, and the library's V6), so the real check allows too.
* **The resolutions put to the owner:**
  1. **Chosen:** two live plants on scratch copies.
     * `Down` does nothing, on runit.
     * The update sets no handoff: `detacherFor` reports false, Magic's own wiring, which the
       "update from inside" step must catch.
     * OpenRC's `DetachCheck` stays proven by `TestOpenRCDetachCheckIsD3B` and P5's plants on it.
       It is recorded as not provable live in a container.
  2. The same, plus a scratch copy of the library through a `go.mod` replace, with the hop dropped.
     If it passed, that would prove nothing either way.

### P6, 2026-10-27: the live tests in the tree

**What was added:**

* **`test/supervisors/Containerfile.runit`, `.s6`, `.openrc`.** The kit's images, each pinned to
  its multi-arch index (linux/amd64 and linux/arm64), read with `podman manifest inspect`:
  * `ghcr.io/void-linux/void-musl@sha256:cd50e0a9…6ed59`;
  * `docker.io/library/alpine@sha256:2e0b81d6…eb55f`, Alpine 3.22.7.

  The packages are the distributions' current ones at build time: neither keeps older ones to
  pin. OpenRC read 0.62.6. The kit's `python3` is gone, since the test is a compiled Go binary.
* **`test/supervisors/probeagent/main.go`** (Deviation 1). The kit's agent, reduced to the methods
  `acp.Agent` requires. A prompt "RUN <command>" runs the command through the client's terminal.
* **`internal/cli/service/live_supervisors_linux_test.go`.** `TestLiveRunit`, `TestLiveS6` and
  `TestLiveOpenRC`, each off unless `MCREMOTE_REQUIRE_<KIND>=1`. In six steps:
  1. install with `setup-service`;
  2. stop and start through the update's own backend, the adapter, a new PID answering;
  3. update from a shell, `update --yes --force --version v0.23.0 --json`;
  4. put the build under test back, through the backend;
  5. update from inside: the probe agent runs the update, which must print "update handed off";
  6. `--remove`.

  Steps 3 and 5 require the result to be applied, replaced before the stop, started and not
  rolled back. A new PID must answer `/healthz`, and `/proc/<pid>/exe` must be the binary on disk
  and not the build under test.
* **`scripts/live_supervisors.py`** (Deviation 1) and **`make live-supervisors`**.
  * Per kind it builds the image. Then it cross-compiles four binaries once for the image's
    architecture: `mcremote`, `smoke-protocol`, the probe agent and `go test -c` of
    `internal/cli/service`.
  * It runs the test as `user` in a fresh container, which it removes afterwards.
  * *The account was `tester`, the kit's. The first gates' home-path guard,
    `TestTrackedFilesUseOnlyPlaceholderHomes`, failed on it on the Linux test host, where every
    file is tracked: "scripts/live_supervisors.py:33: home path account "tester" is not a
    placeholder". The same failure appeared in `Containerfile.runit` and `Containerfile.s6`. The
    container's account is now `user`, a placeholder the guard accepts, in P6's own files. The
    guard was not changed.*
  * `CONTAINER_ENGINE` picks Podman or Docker. `GH_TOKEN` is passed by name only.
* **`.github/workflows/ci.yml`:** a `live-supervisors` job runs runit and s6 on `ubuntu-latest`
  with Docker and the job's token. `actionlint` is clean. The job is not among the release build's
  `needs`. It has not run on GitHub yet: it runs on the push.
* **`docs/guides/ops-linux-install.md`:** a paragraph under Testing.

**The live runs** (step 3, local Podman, linux/arm64):

| Run | Kind | Result | Steps (s) | The handoff |
| :--- | :--- | :--- | :--- | :--- |
| first | runit | PASS, 51.9 s | 3.1, 10.6, 12.2, 10.5, 15.4, 0.0 | "update handed off: eed092d0… (a detached run, started …" |
| first | s6 | PASS, 48.5 s | 1.1, 10.4, 11.6, 10.5, 15.0, 0.0 | handed off |
| first | OpenRC | FAIL at install (Deviation 2) | — | — |
| second | OpenRC | FAIL at install's `/proc/<pid>/exe` check (Deviation 3) | — | — |
| third | OpenRC | PASS, 50.6 s | 1.1, 10.3, 12.1, 11.1, 15.3, 0.6 | "update handed off: 27d738f6…" |
| **final**, all three, on the final tree | runit | PASS, 50.3 s | 2.1, 10.4, 11.8, 10.5, 15.6, 0.0 | "update handed off: 4d209d76…" |
| | s6 | PASS, 49.1 s | 1.1, 10.4, 12.1, 10.7, 14.8, 0.0 | "update handed off: 2ad2f33b…" |
| | OpenRC | PASS, 50.5 s | 1.1, 10.3, 12.2, 11.0, 15.3, 0.6 | "update handed off: 5498220a…" |

This is 0210's R1 reproduction, now in the tree. It ends with the update applied and the service
restarted on each kind. It is also 0207-MADR D36's success run, which U3b could not run under
these supervisors while they had no handoff.

**Seen failing, live** (step 2, Deviation 4; `m212/p6_plants.py`). Each plant is on a scratch copy
whose own driver built and ran runit:

| Plant | The failure |
| :--- | :--- |
| `Down` does nothing on runit | `…/stop_and_start_through_the_update's_backend`: "Stop: … timed out: mcremote-live still runs 1m15s after it was stopped (last: up=true pid=222 … 30s)". The adapter's `sv kill` fired at 45 s, and runsv started the daemon again, still wanted up |
| the update sets no handoff | `…/update_from_inside,_handed_off`: "the agent's update did not hand off". The agent's output was "update failed: this update runs inside the mcremote service …", the refusal |

* OpenRC's `DetachCheck` is not provable live in a container (Deviation 4). It stays proven by
  `TestOpenRCDetachCheckIsD3B` and P5's plants on it.
* **Deviations 2 and 3's fixes, seen failing:** live, as the first and second OpenRC runs. As
  unit tests (`m212/p6u_plants.py`), each plant on its own scratch copy:

| Plant | The failure |
| :--- | :--- |
| `start_pre` without the parent's `mkdir` | `TestOpenRCDefinitionIsD10A`: "no line "\tmkdir -p '/home/u/.local/state/mcremote'"" |
| the supervisor matched by its untruncated name | `TestOpenRCStartedReadsThePIDFile/sd-started`: "state {… PID:306 …}; want up, pid 307", the supervisor's PID for the daemon's |
* **Two mistakes of mine, in the debug scripts only:** a config and a wrapper written through
  `printf '%s'` with a JSON-quoted string got literal `\n`s. The first wrapper therefore never ran,
  and the first debug daemon failed on its config. Both were rewritten as files copied in.

**Gates** (`m212/p5_gates.py`, over the six changed and added Go files and two records, run twice):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the six Go files | "6 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `actionlint .github/workflows/ci.yml` | exit 0 |
| `go test -race ./...` on the Linux test host, every file tracked | first run: the home-path guard failed on `tester` (above); second run: `internal/ws` `TestReceiptEndToEndOverRealWS` failed (below); third run, `go test -race -count=1 ./...`: exit 0, 44 packages ok |
| `internal/cli/service` and `internal/updateclient`, verbose, on the Linux test host | exit 0, both runs |

* **A flaky test, not P6's.** `TestReceiptEndToEndOverRealWS` failed once:
  * The message: "want ok for permission.receipt, got event payload={"event":{"type":"permission_resolved"…".
    The test reads the next frame, and a `permission_resolved` event can arrive before the reply.
  * P6 changes nothing in `internal/ws`. On the committed tree before P6 (`5cacbd01`), the same
    test failed once in 100 race runs on the Linux test host, with the same message
    (`go test -race -count=100 -run '^TestReceiptEndToEndOverRealWS$' ./internal/ws/`).
  * It is left for a record of its own. P6 does not touch it.

**P6 is complete** once the owner commits it. The CI job runs on the push. Next by this record: P7,
the records; then 0207's U6 (D22).

### P7, 2026-10-27: the records

The owner committed P6 as `dcd7b31f` and answered "committed, proceed". P7 has no host or container
runs.

**Step 1, the dated notes** (each points here, and none rewrites history):

| Record | Where | The note |
| :--- | :--- | :--- |
| [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md) | S5-3 | the update runs these supervisors through the library's `NewSupervised` since P5; `scriptBackend` is deleted, so D5 holds unextended |
| [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md) | D20 | retired into this record (R2), and met by the library's adapter |
| the same | D4 | P5 and P6 have landed; nothing U6 waits on remains |
| [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md) | U7 | retired, not run; P6 runs the three containers |
| the same | U6 | the wait is over; U6 still needs the owner's approval |
| [0210-MADR-hand-off-agent-run-updates-outside-the-service.md](0210-MADR-hand-off-agent-run-updates-outside-the-service.md) | D4 | these supervisors hand off; OpenRC refuses in a shared OpenRC cgroup (D3-B); P6 runs R1 in the tree |
| the same | question 2 | go-selfupdate-lib#2 closed on 2026-10-25 with the adapter; Magic writes no `Detacher` of its own |

* The amendment-D note already on 0207-MADR D4 and 0207-PLAN U6 (D22) stays. The new notes record
  only that P5 and P6 have landed.
* This plan's opening line called amendment D "proposed". It is now struck through and marked
  decided.

**Step 2, the #27 comment: not done.** The step runs on the owner's ask, which has not been given.
Issue #27 is open. The comment would carry the MADR's R6 corrections and this plan's outcome.

**Step 3, the statuses: not yet.** Every Verification item was checked against `dcd7b31f`:

| Item | Result |
| :--- | :--- |
| P1's probe results, per supervisor | held: "P1, 2026-10-26" above |
| each phase's fail-first tests, quoted | held: in each phase's record |
| `git grep -n -e 'exec.LookPath' -e 'os.Stdout' -- internal/cli/service` | held. It finds `detect.go:77` (`lookPath`, for `launchctl` and `systemctl` only), `setup.go:681`, `:865` and `:891` (`plutil`, `systemctl`, `launchctl`), `setup.go:1380` (`runCmd`, whose only callers run `launchctl`, `systemctl` and `loginctl`), and three test files. No script-kind tool call |
| `git grep -n -e scriptBackend -e stopTimeout -e procParent -- internal` | held: no output |
| `make live-supervisors` in all three containers | held: P6's final run |
| CI's runit and s6 job | **open.** `master` is 10 commits ahead of `origin/master`, so the job has never run on GitHub |
| an agent-run update hands off and completes on all three | held: P6's step 5, on each kind |

So the MADR stays `proposed` and this plan `in-progress`. Both change when the CI job passes, after
the owner pushes. Pushing is the owner's ask.

**Gates** (no Go file changed, so the Go gates do not apply):

| Gate | Result |
| :--- | :--- |
| `make check-records` | exit 0 |
| `make markdownlint-docs` | exit 0, "0 issues". It lints guides and reports only, not `docs/decisions/` |
| `markdownlint-cli2` over the five changed records, at `HEAD` and in the tree (`m212/p7_lint_compare.py`) | 129 findings in each, none only in the tree. Every one predates P7: list style and one unspaced list in 0054, a doubled blank line at 0210's end, and four in this plan's earlier execution records |
| the guard's redact preview, from the repository root | "would edit 0 file(s)" |

* **The lint comparison, seen failing first.** My first draft of step 2 began a line with "#27",
  which renders as a heading. The direct lint caught it (MD018), and the line now begins "Issue
  #27". `m212/p7_lint_plant.py` put the old line back in a scratch copy, and the comparison then
  exited 1: "only in the tree: 1 … MD018: #27 is open."

**P7 is done except step 3,** which waits for CI's first `live-supervisors` run. Next, on the
owner's word: 0207's U6 (D22).

### P7 step 3, 2026-10-27: the statuses

The owner committed P7 as `a2106744` and pushed it ("committed and pushed"). CI ran on that
commit: [run 40548773233](https://github.com/maccavelli/magic-cli-remote/actions/runs/40548773233),
conclusion `success`.

**The `live-supervisors` job, "Live supervisors (runit, s6)":** `success`, 17:15:06Z to 17:18:09Z.
Its log, read in full for the test lines, shows the tests ran rather than skipped:

| Kind | Result | Steps (s) | The handoff |
| :--- | :--- | :--- | :--- |
| runit | `--- PASS: TestLiveRunit (53.39s)` | 2.06, 10.56, 13.35, 10.79, 16.47, 0.14 | "update handed off: afaba9af6c402a05 (a detached run, started …" |
| s6 | `--- PASS: TestLiveS6 (49.40s)` | 1.10, 10.32, 11.88, 10.51, 15.42, 0.15 | "update handed off: cfc5735b17c9d29c (a detached run, started …" |

* It built for linux/amd64 ("built mcremote, smoke-protocol, probeagent and live.test for
  linux/amd64") and ran under Docker. Every local run in P6 was Podman on linux/arm64, so this is
  the first run on the other architecture and engine.
* The driver's last line: "live supervisors: runit, s6; failed: none".
* The run's six other test and build jobs passed. The Android APK and the two self-update release
  jobs were skipped, as on any push without a tag.

**Every Verification item now holds** (P7's table above, with CI's job closed here). So, as step 3
provides:

* the MADR is `accepted`, with a dated note under its Decision Outcome;
* this plan is `complete`.

**Not done, by this plan's terms:** the comment on issue #27 (step 2), which waits for the owner's
ask. Next by these records, on the owner's word: 0207's U6 (D22).
