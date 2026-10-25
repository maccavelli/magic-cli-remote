---
status: proposed
date: 2026-10-25
associated-madr: "0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement runit, s6 and OpenRC through go-selfupdate-lib's supervised adapter

Associated MADR: [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md),
with its amendments A and B.

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
samplers' calls only); `testdata/` as needed; this plan.

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
`health.go`; this plan.

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
U3b has landed; this plan.

1. **Tests first:**
   * **runit and s6:** a `log/run` service directory, `svlogd` for runit and `s6-log` for s6, into
     `$XDG_STATE_HOME/<product>/log`. **s6:** a `timeout-kill` file holding `45000`.
   * **OpenRC, D10-A** (or D10-B, by P1's decision point): `command`, `command_args`,
     `directory`; `supervisor=supervise-daemon`; `respawn_delay=5`, `respawn_max=30`,
     `respawn_period=300`; `retry="TERM/45/KILL/5"`; `output_log` and `error_log`; on OpenRC 0.62
     or later, `notify=socket:ready`; the environment through `supervise_daemon_args`.
   * `recoverScript` reads both the current and the new form of each definition (D15-A). Plant: a
     reader of the new form only, against a current-form fixture.
   * `removeScript` removes the `log` directory too.
   * Golden files for each rendered form.
2. **Code** as tested.
3. **Containers, on the owner's word:** a fresh install and a `--refresh` from the current form,
   in each container. Logs are written; s6 and OpenRC kill a TERM-ignoring daemon at 45 s; OpenRC
   restarts a crashed one under D10-A.
4. Gates.

### P5 — The adapter (stage 2; D3-B, D5-B, D17, D6-A)

**Files:** `backend.go`, a new `supervisedbackend.go` and its test, `control.go`, `scriptbackend.go`
and `scriptbackend_inside_linux.go` (deleted), their tests; `internal/updateclient/client.go`
(the help text at `:290-294`); `docs/guides/ops-linux-install.md`; this plan.

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
