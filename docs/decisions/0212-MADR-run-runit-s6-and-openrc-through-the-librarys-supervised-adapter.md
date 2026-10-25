---
status: proposed
date: 2026-10-24
decision-makers: Project Owner
consulted: "issue #27 of this repository and go-selfupdate-lib issue #2 with its comments, read 2026-10-24; go-selfupdate-lib's 0035-MADR-serve-supervisors-through-a-supervised-adapter.md and 0035-REPORT-library-backends-or-consumer-api-for-runit-s6-and-openrc.md at 9a10708 (committed, not pushed), its selfupdate/service code at that commit, and its published tags to v1.21.0; OpenRC's NEWS.md, user-guide.md and etc/rc.conf on master, read 2026-10-24; this repository at 793b8f5b, with its 0054, 0207 and 0210 records. Amendment A: go-selfupdate-lib at fff0940 (its 0011, 0015, 0020, 0023, 0025, 0033 and 0035 records, its service package, systemd backend, internal/testproc and extending guide); OpenRC's service-script and supervise-daemon guides, openrc-run(8) and start-stop-daemon(8); s6's servicedir page; runit's sv(8) and runsv(8); all read 2026-10-24. Amendment B: go-selfupdate-lib at v1.22.0 (8480556), its CHANGELOG, migration guide §22, service package and 0035 records; library issue #2 and #27, read 2026-10-24"
informed: anyone who changes the service backends, setup-service, the update command, or Magic's go-selfupdate-lib pin
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Run runit, s6 and OpenRC through go-selfupdate-lib's supervised adapter, with Magic keeping each supervisor's state and commands

## Context and Problem Statement

Magic's own `scriptBackend` runs managed updates on runit, s6 and OpenRC user services. It misses
parts of the backend contract that the library's systemd, launchd and Task Scheduler backends keep:

* the caller's context;
* the typed errors;
* a bounded stop that puts things back when it fails;
* the application probe;
* a handoff for an update an agent starts from inside the service.

Today an agent-run update on these supervisors fails safe and changes nothing (0210 D4 and D8).
It cannot succeed.

go-selfupdate-lib issue #2 asked for three full library backends. The library's owner chose a
different shape on 2026-10-24 (its 0035-MADR, D0-C):

* one supervisor-neutral adapter, `service.NewSupervised`, which owns the contract;
* the program supplies its supervisor's state and commands.

Issue [#27](https://github.com/maccavelli/magic-cli-remote/issues/27) tracks Magic's side of that.

The owner's direction for this record (2026-10-24): "we will be keeping backend functionality in
magic-cli-remote and building an adapter on the library". This record reads that as the library's
D0-C. Magic keeps:

* its scripts and its PID-file layout;
* its session supervisors;
* the way it reads and drives each supervisor.

The library's adapter owns the contract. Building the adapter inside Magic instead is weighed as
D1-C, so that reading can be corrected at review.

This record decides Magic's side:

* when the work lands;
* how supervisor tools are found and run;
* where state is read;
* the application probe;
* OpenRC's handoff check;
* what happens to `settle`;
* which library pin the work starts from.

It does not decide the adapter's API. That is the library's 0035-MADR, D1 to D14, still proposed.

### Evidence tags

* **[read]:** code or text read for this record, at the commit given.
* **[docs]:** a supervisor's own documentation, fetched 2026-10-24. The PLAN re-reads each page it
  relies on.
* **[reported]:** an issue, or a record of this repository or the library.
* **[derived]:** a conclusion drawn from tagged facts.
* **[unverified]:** not yet measured. The PLAN probes it before any code depends on it.

### Facts

#### The library [read, go-selfupdate-lib `main` at 9a10708 and its published tags]

* **L1. The adapter does not exist yet.**
  * `service.NewSupervised` is proposed in 0035-MADR (`status: proposed`, 2026-10-24).
  * Its D0 is decided (D0-C). D1 to D14, the API, are not. There is no 0035 PLAN.
  * The MADR and REPORT are two commits ahead of `origin/main`, not pushed.
  * Issue #2 is open. Its 2026-10-24 comment says the names "may change in the plan" [reported].
* **L2. Releases.**
  * The newest tag is `v1.21.0`, which adds Task Scheduler `Enable` and `Disable` (#8, closed
    2026-10-24).
  * `v1.20.0` stops passing the service manager's per-unit variables to a handoff (#7).
  * Magic pins `v1.19.0` (`go.mod:12`). So any bump to the adapter's release also takes in
    `v1.20.0` and `v1.21.0`.
* **L3. What the library already ships.**
  * `ProcessDetacher(inside)` (`selfupdate/service/handoff.go:293`).
  * `DetachProcess`, which on Unix starts the run in a new session, in `/`
    (`detach.go:22-34`, `detach_unix.go:10-22`).
  * `PollHealthy`, whose defaults are a 250 ms interval, a 60 s timeout and a 10 s settle window.
    `Validate` refuses a settle window that is not shorter than the timeout (`poll.go:29-76`).
  * `Runner` and `ExecRunner`, which:
    * require an absolute path;
    * keep at most 1 MiB of each output stream;
    * kill the tool when the context ends;
    * treat a non-zero exit as data, not as an error (`runner.go`).
  * The systemd backend looks for its tools only in `/usr/bin` and `/bin`, never on `PATH`
    (`systemd/unit.go:95-96`).
* **L4. The adapter as proposed (0035-MADR D2 to D11) [reported].**
  * `SupervisedOptions` takes `State`, `Up` and `Down` functions, plus optional `Name`, `Poll`,
    `Probe` and `DetachCheck`.
  * `SupervisedState` has `Installed`, `Supervised`, `Up`, `PID`, `WantUp`, `NormallyUp`,
    `Restarting`, `Failed`, `Ready` (a pointer) and `Detail`.
  * `*Supervised` is a `Lifecycle`, an `EnabledLifecycle`, a `Reconciler` and a `Detacher`, and
    has a `Status` method. It runs on Linux only.
  * **Stop:**
    * refuses from inside;
    * records the instance as PID plus start time;
    * calls `Down`;
    * waits to `Poll.Timeout` even after the caller's context ends;
    * calls `Up` again on failure.
  * **WaitHealthy** fails at once on `Failed`, on a restart during the wait, or on a crash loop.
    Then it runs `Probe`.
  * **Detach** runs `DetachCheck`, then `DetachProcess`.
  * **New sentinel:** `ErrNoSupervisor`.

#### Magic today [read, at 793b8f5b]

The code is unchanged since `be41ef9`, the commit issue #27 and the library's records cite. Only
two 0210 records changed since then.

* **M1. Where the backend is built.** `NewBackendWith` returns `newScriptBackend` for the script
  kinds (`internal/cli/service/backend.go:59-60`). `IsInstalled` builds one too
  (`control.go:45`). `Backend` is `Lifecycle` plus `EnabledLifecycle` (`backend.go:20-23`).
* **M2. Its inside check.**
  * `Inside` walks `/proc/<pid>/stat` parents, at most 4096 steps, to the supervised PID
    (`scriptbackend.go:83-155`; `scriptbackend_inside_linux.go`).
  * The PID comes from the sampler for runit and s6, and from the PID file candidates for OpenRC
    (`:91-122`).
  * The PID's start time is not compared, so a reused PID could match [derived].
* **M3. Its stop.**
  * `Stop` refuses from inside, then runs the stop command.
  * It then polls with `time.Sleep(settleTick)` up to its own 15 s (`stopTimeout`). `ctx` is
    unused, and a timeout is a plain error (`:162-192`).
  * Nothing restarts the service when the stop fails.
* **M4. Its start.** `Start` either starts a session `runsvdir` or `s6-svscan`, or runs `sv up`
  or `s6-svc -u`. On OpenRC, it runs `openrc --user boot` and `default` when the user's runlevel
  is unset, then `rc-service --user <name> start`. `ctx` is unused (`:194-230`).
* **M5. Its health wait.** `WaitHealthy` runs `settle` for `MCREMOTE_SVC_WAIT_SECS` seconds,
  default 15. Its errors are plain, and it runs no application probe (`:232-247`;
  `backend.go:34-38`).
* **M6. `settle` is shared.** `verifyStart` calls it for every kind (`settle.go:305-325`). That is
  setup-service's verified start. So `settle` cannot be deleted for the script kinds while
  setup-service still uses it there.
* **M7. The samplers** (`settle.go:180-241`):
  * runit's `sv status` and s6's `s6-svstat` human form are read with regular expressions.
  * OpenRC is read from `rc-service --user <name> status`: its exit code, and the text
    "status: crashed".
* **M8. How commands run.**
  * `runCmd` and `runCmdEnv` find the tool by name on `PATH` (`procutil.Command`).
  * They bound it by their own 30 s (`setup.go:42`), not the caller's context.
  * They set `cmd.Stdout = os.Stdout` (`setup.go:1369-1407`).
  * `runCmdOutput` captures the output instead.
  * `startSessionSupervisor` also finds `runsvdir` or `s6-svscan` on `PATH`
    (`supervisor_unix.go:65-81`).
  * `runCmdOutput`'s doc comment sits above `runCmdEnv` (`setup.go:1387-1391`).
* **M9. The update's stdout.**
  * The command promises: "Stdout is empty unless --json is given" (`internal/updateclient/client.go:287-288`).
  * During an update, `Stop` and `Start` run supervisor tools through `runCmd`, which connects
    their stdout to the process's stdout [read].
  * Which of `sv`, `s6-svc`, `rc-service` and `openrc --user` actually print there is
    [unverified].
  * The reconcile child, `setup-service --refresh --json`, has its stdout parsed by the library as
    the receipt. For the script kinds it runs no supervisor tool (`refreshScript`,
    `supervisor.go:516-557`), so the receipt is not at risk [read].
* **M10. The application probe cannot read a script definition.**
  * `HealthOptions.backendProbe` reads the serve arguments with `readServeArgs`
    (`health.go:53`, `:143-155`).
  * On Linux, `readServeArgs` reads only `<systemd user dir>/<unit>.service` (`health.go:89-100`).
  * Under runit, s6 or OpenRC that file does not exist, so the probe would fail on every call
    [derived].
  * `recoverScript` already recovers a script definition's options (`supervisor.go:245`).
  * So passing the probe as is (issue #27, item 6) would fail every update on these supervisors
    with a timeout and a rollback [derived].
* **M11. The handoff wiring.**
  * The update sets a handoff for any backend that is a `libservice.Detacher` (`detacherFor`).
  * Otherwise it refuses an inside run before it downloads (`refuseInside`)
    (`client.go:187-252`, `:326-333`).
  * A `*Supervised` returned from `NewBackendWith` would get the handoff with no change there.
    `refuseInside` would then not apply to these kinds [derived].
  * The command's help still says that under runit, s6 and OpenRC "it refuses before
    downloading" (`client.go:290-294`). So does `docs/guides/ops-linux-install.md:297`.
* **M12. The reconciler.**
  * Every backend gets Magic's own `ExecReconciler` (`internal/updateclient/managed.go:62-90`).
  * So the adapter's verify-only `Reconcile` would not be used [derived].
* **M13. Tests and hosts.**
  * `scriptbackend_unix_test.go` has 11 tests, all over fakes.
  * No CI job or live-tagged test drives a real runit, s6 or OpenRC.
  * The 0210 runs used Podman containers, through a scratch kit [reported, 0210-PLAN]:
    * Void Linux with runit;
    * Alpine 3.22 with s6;
    * Alpine 3.22 with OpenRC 0.62.6.

#### The supervisors [docs, read 2026-10-24]

* **S1. Where an OpenRC user service's settings come from.**
  * OpenRC 0.64's NEWS: "user services no longer load the system-wide /etc/rc.conf". For
    compatibility, it suggests a symlink "/etc/user/rc.conf > /etc/rc.conf".
  * The user guide:
    * `~/.config/rc/rc.conf` overrides `/etc/rc.conf`;
    * `~/.config/rc/conf.d` overrides `/etc/user/conf.d`.
* **S2. `rc_cgroup_cleanup`.**
  * `etc/rc.conf` shows it commented out as `"NO"`.
  * It kills "all of the processes in a service's cgroup … when the service is stopped".
  * It can be set "for the individual services in /etc/conf.d/<service>".
  * It uses cgroup2's `cgroup.kill` where the kernel has it.
  * So its effective value for Magic's user service can come from any of several shell-sourced
    files, and which ones depends on the version [derived]:
    * `/etc/rc.conf` before 0.64;
    * `/etc/user/rc.conf` from 0.64, where present;
    * `~/.config/rc/rc.conf`;
    * the service's own `conf.d` files.
* **S3. OpenRC cgroups.**
  * From 0.53, OpenRC's service cgroups are named with an `openrc.` prefix.
  * From 0.51, the default cgroup mode is `unified`.
  * Whether *user* services get a cgroup at all is [unverified]. The library's 0035-MADR F24 says
    the same.
* **S4. s6's machine form.** `s6-svstat -o` prints documented, "programmatically parsable" fields.
  It exits 1 when no `s6-supervise` runs on the directory [reported, library 0035-MADR F17].
* **S5. runit's status text.**
  * What `sv status` prints is undocumented beyond one FAQ example [reported, library 0035-MADR
    F14].
  * Magic's parse has worked live. 0210's L1.4 refused from inside on Void, which needs the PID
    that parse returns [reported, derived].

#### The earlier records, assessed [read]

* **R1. 0054-MADR S5-3** gave these supervisors "a product backend", "because the library has
  none for these supervisors". That is still true. It stops being true when the adapter is
  adopted.
* **R2. 0207-MADR amendment B, D20, and 0207-PLAN U7.**
  * They make `scriptBackend` honour `ctx` and wrap the sentinels. Neither is started.
  * Every line U7 would change is deleted or replaced by the adapter: the stop poll and the
    script `WaitHealthy` [derived].
  * So doing U7 first would be thrown away.
* **R3. 0207-MADR D4 (as amended) and 0207-PLAN U6.**
  * They confirm setup-service's start "by `service.PollHealthy` over the existing samplers for
    runit, s6 and OpenRC". They remove the settle loops "when nothing calls them".
  * That overlaps this record's state work, so their order matters.
* **R4. 0210-MADR D4 and question 2.**
  * The answer "Guard now, handoff via #2" expected #2's "backends, whose proposal already
    includes a `Detacher`". #2's direction is now the adapter (its 2026-10-24 comment).
  * 0210's table "What happens today" cites `scriptbackend.go:80-104` for `Stop`. Those lines are
    now `:162-192`, because D4 added `Inside` above them. The table is dated history from before
    the change, so it stays as written. This record cites today's lines.
* **R5. 0210 option D**, Magic's own `ProcessDetacher`, was declined for two reasons:
  * `DetachProcess` "is no defence against a cgroup kill";
  * #2 asked the library to own the handoff.

  The adapter answers the second. D3 below answers the first, for OpenRC.
* **R6. Issue #27 against the code.** Its citations hold at 793b8f5b. Four points are incomplete:
  * **(a)** Item 6 misses M10.
  * **(b)** Item 7's "read the user's rc.conf" misses two sources: the service's own `conf.d`, and
    `/etc/rc.conf` before 0.64 (S1, S2).
  * **(c)** Item 8's "unless setup-service still uses it" is settled: setup-service does use it
    (M6).
  * **(d)** The acceptance line "no supervisor tool is looked up on PATH" also covers
    `startSessionSupervisor` and `openrc --user`, which `Up` reuses (M8).

## Decision Drivers

* **The owner's direction.** Magic keeps its supervisor knowledge, and the library owns the
  contract (library 0035-MADR D0-C).
* **No regression.** On all three supervisors, the agent-run update keeps failing safe until the
  handoff works, as 0210's L1 and L2 confirmed.
* **Nothing thrown away.** No code is written now that the adapter's arrival deletes.
* **The update's promises:** its stdout carries only `--json` output, and cancelling it cancels
  its commands.
* **Facts before code.** Every [unverified] item is measured before code depends on it.
* **One change per pin.** A regression traces to one bump.

## Considered Options

### D1: when Magic's side lands

* **D1-A.** Everything at once, after the library releases the adapter. This is issue #27 as
  written.
* **D1-B.** Two stages.
  * **Stage 1, now,** on the current pin. Only work that survives the adapter:
    * supervisor tools run through a `libservice.Runner`, with absolute paths, the caller's
      context, and captured output;
    * s6's state is read from `s6-svstat -o`;
    * each kind's state is read into the adapter's proposed fields, pinned by captured fixtures;
    * `readServeArgs` learns the script definitions.
  * **Stage 2, after the release:** the bump, the `*Supervised` per kind, and the deletions.
  * 0207 U7 is retired.
* **D1-C.** Magic builds the adapter itself now, on `ProcessDetacher`, `PollHealthy` and
  `Runner`, and swaps to the library's later.

### D2: where supervisor tools are found

* **D2-A.** A fixed list of directories, as the library's systemd backend does (L3). The list is
  measured on each target.
* **D2-B.** setup-service resolves each tool through `PATH` once, and records the absolute paths
  beside the backend record. The update reads them.
* **D2-C.** Keep `PATH`.

### D3: OpenRC's `DetachCheck`

* **D3-A.** Read `rc_cgroup_cleanup` from the user's `rc.conf` and refuse when it is `YES`. This is
  issue #27, item 7.
* **D3-B.** Refuse when this process shares the supervised process's cgroup and that cgroup is
  OpenRC's, named with the `openrc.` prefix, whatever the setting. Both are read from
  `/proc/<pid>/cgroup`.
* **D3-C.** As D3-B, but refuse only when the setting, read as in D3-A, is also `YES`.
* **D3-D.** No check.

### D4: the application probe on these supervisors

* **D4-A.** `readServeArgs` reads script definitions through `recoverScript`, and the probe is
  passed as `Probe`.
* **D4-B.** No probe, as today.

### D5: the health wait's bounds

* **D5-A.** Keep `settle`'s numbers: `Poll.Settle` 2 s, and `Poll.Timeout` from
  `MCREMOTE_SVC_WAIT_SECS`, default 15 s.
* **D5-B.** The same bounds as the systemd and launchd backends, which Magic leaves at the
  library's defaults: 10 s settle and 60 s timeout. `MCREMOTE_SVC_WAIT_SECS`, when set, becomes
  `Poll.Timeout`. A value not above the settle window is refused, as `Validate` does.

### D6: `settle` and setup-service

* **D6-A.** `settle` stays for setup-service's verified start. This record removes only the update
  backend's use of it. 0207 U6, when it lands, builds its script-kind probes on stage 1's state
  functions rather than on the samplers.
* **D6-B.** This record also moves setup-service's script-kind start onto
  `Supervised.WaitHealthy`.

### D7: s6's state

* **D7-A.** `s6-svstat -o up,wantedup,normallyup,ready,pid,exitcode,updownfor`, the documented
  form (S4).
* **D7-B.** Keep the human-form regular expression (M7).

### D8: the pin the adapter's bump starts from

* **D8-A.** Move to `v1.21.0` first, under 0207 as its own amendment and step, so this record's
  bump carries only the adapter.
* **D8-B.** Jump from `v1.19.0` to the adapter's release here.

## Decision Outcome

The owner answered the five questions on 2026-10-24 (see "Questions for the owner"):

* The direction, D1, D3, D5 and D8 are decided as recommended.
* D2, D4, D6 and D7 were not asked, and stay proposed.

So the record stays `proposed` until the PLAN is presented with them.

* **D1: "D1-B", two stages.** *Decided 2026-10-24.*
  * Stage 1 fixes M8 and M9 and closes M10 now, and none of it is lost when the adapter arrives:
    `State`, `Up` and `Down` are built from exactly this code.
  * It also retires 0207 U7 (R2) instead of writing code the adapter deletes.
  * D1-A waits on four things for fixes that do not need the adapter (L1): the library's D1 to
    D14, its PLAN, its release, and a push.
  * D1-C repeats what the owner has just placed in the library, and is the "Magic writes the
    handoff" route 0210 declined (R5).
* **D2: "D2-A", a fixed list.**
  * It is the rule the library's backends keep (L3), and it needs no new record format.
  * The list is measured with `command -v` in each container and on the Linux test host. It is
    expected to include `/usr/bin`, `/bin`, `/usr/sbin`, `/sbin`, and `/command` for s6 [unverified].
  * A tool found in none of them is an error that names the directories searched. It is not a
    `PATH` fallback.
  * D2-B would serve user-level installs, such as a Nix profile, at the cost of a new record
    format. Nothing here shows such an install, and it can be added later.
* **D3: "D3-B", the cgroup check.** *Decided 2026-10-24.*
  * A cleanup kill reaches only the service's cgroup (S2). `DetachProcess` does not move the run
    out of it (L3).
  * So the run dies only when it shares an OpenRC service cgroup with the daemon and the effective
    setting is `YES`.
  * D3-B reads the first condition from the kernel and refuses whatever the second says. The
    refusal is today's fail-safe, never a stopped service.
  * D3-A and D3-C would need to source several shell files whose set depends on the version
    (S1, S2), and they could be wrong in the unsafe direction.
  * D3-B's cost is that a host with OpenRC cgroups but cleanup off keeps the refusal. If the
    probe shows user services get no cgroup (S3), the check never refuses, which is also correct.
* **D4: "D4-A".** Without it, every update on these supervisors would time out (M10). With it,
  they get the application check the other backends run. `readServeArgs` has one other caller,
  the Windows stop request (`health.go:168`), which these supervisors never reach. So nothing
  else changes.
* **D5: "D5-B".** *Decided 2026-10-24.*
  * The update should hold every backend to one bar.
  * A 10 s window catches a daemon that dies a few seconds after it starts, which a 2 s window
    passes.
  * The 60 s timeout is the bound the update already accepts on systemd and launchd.
  * setup-service's own wait is unchanged (D6).
* **D6: "D6-A".** It keeps this record to the update's backend, and leaves setup-service's start
  to 0207 U6, where it is already planned (R3). D6-B would widen a change that already spans
  three supervisors.
* **D7: "D7-A".** It is the only documented form among the three supervisors (S4). It also gives
  `Ready` and `wantedup` directly, where the regular expression reads them from prose.
* **D8: "D8-A".** *Decided 2026-10-24.*
  * `v1.20.0` changes what a handoff inherits, and `v1.21.0` unblocks 0207 D18.
  * Each belongs to 0207's records and checks, not to this one.

### What Magic does, by stage

**Stage 1 (now, on the pin of the day):**

1. **A runner for the script kinds.**
   * Every `sv`, `s6-svc`, `s6-svstat`, `s6-svscanctl`, `rc-service`, `rc-update` and `openrc`
     call goes through a `libservice.Runner`, by default `ExecRunner`:
     * at an absolute path (D2);
     * with the caller's context;
     * with output captured.
   * The error carries the tool's stderr, as `runCmd` does today.
   * `startSessionSupervisor` keeps its detached start, at an absolute path.
   * setup-service's script paths use the same runner. Its terminal output is then its own
     printing, not the tools'.
2. **State, per kind,** into one Magic struct with the adapter's proposed fields (L4), read from
   captured fixtures:
   * **runit:** `sv status` as today. `Restarting` comes from "down … want up". `NormallyUp` is
     the absence of `<dir>/down`.
   * **s6:** `s6-svstat -o` (D7). Exit 1 is "no supervisor".
   * **OpenRC:**
     * the `status` exit code;
     * "crashed" is `Failed`;
     * the PID comes from today's PID file candidates;
     * `NormallyUp` is the `default` runlevel link;
     * no user session is "no supervisor".

   The samplers and `Inside` read this struct. Their behaviour is unchanged.
3. **`readServeArgs` reads script definitions** (D4) through `recoverScript`. Nothing calls it
   for these supervisors until stage 2 passes the probe.

**Stage 2 (after the library release that ships the adapter, and after D8):**

1. The bump.
2. `NewBackendWith` builds one `*libservice.Supervised` per script kind:
   * `State` from stage 1;
   * `Up` from today's `Start`, with the session supervisor start kept;
   * `Down` from today's stop command, with no wait;
   * `Probe` from `backendProbe`;
   * `Poll` (D5);
   * for OpenRC, `DetachCheck` (D3).
3. `IsInstalled` (`control.go:45`) reads the state's `Installed`.
4. **Delete:**
   * `scriptBackend`;
   * `Inside`, `parentOf`, `procParent` and `maxAncestry`;
   * `stopTimeout`;
   * the script `WaitHealthy`.

   `settle` stays (D6). `refuseInside` stays for any backend that is not a `Detacher` (M11).
5. The command's help text (`client.go:290-294`) and `ops-linux-install.md` say that the update
   hands off on these supervisors, with OpenRC's exception.
6. The records: see "What this record changes in others".

### Consequences

* Good, because the update's stdout and cancellation promises hold on these supervisors from stage
  1 onward (M8, M9).
* Good, because stage 1 alone closes the probe's gap (M10) and moves s6 to its documented form.
  Neither waits on the library.
* Good, because stage 2 brings the handoff, the sentinels, a bounded stop that puts the
  supervisor's want back, PID-reuse-safe instances, and restart detection, all in one release
  (L4).
* Good, because no work is thrown away: U7 is retired, and stage 1's code becomes `State`, `Up` and
  `Down`.
* Neutral, because Magic keeps its runit and OpenRC parsing, and the responsibility for it (S5).
* Bad, because OpenRC hosts with service cgroups keep the refusal even with cleanup off (D3).
* Bad, because stage 2 depends on a library decision, plan and release that do not exist yet
  (L1).
* Bad, because a supervisor installed outside the measured directories stops working with the
  update until the list grows (D2).

### Confirmation

* **Probes first,** in the three containers and on the Linux test host. Their output goes in the
  PLAN:
  * each tool's absolute path (D2);
  * what each stop, start and status command prints on stdout (M9);
  * `s6-svstat -o` output for up, down, and "down, want up";
  * for an OpenRC user service: the daemon's and a detached child's `/proc/<pid>/cgroup`, and
    whether that child survives a stop with `rc_cgroup_cleanup="YES"` (S3).

  Where the library's 0035 PLAN measures the same thing first, its result is cited instead.
* **Tests, each seen failing first on a planted break in a scratch copy:**
  * **Context:** a cancelled context ends every runner call.
  * **Tool lookup:** no tool is looked up on `PATH`. A planted bare name fails.
  * **stdout:** `update --json`, with a fake tool that prints to stdout, writes only the JSON
    document. This fails against today's `runCmd`.
  * **State fixtures:** each kind's state is read from captured output for up, down, crashed,
    restarting and no supervisor.
  * **The probe:** `readServeArgs` recovers each script definition's listen address.
  * **Stage 2:** fakes for `State`, `Up` and `Down` through `*Supervised`. `errors.Is` matches the
    library's sentinels. OpenRC's `DetachCheck` refuses in a shared `openrc.` cgroup and allows
    otherwise.
* **Gates:** `make pre-add-check`, `make test`, `make check-records`, and Linux
  `go test -race ./...` on the Linux test host.
* **Live, on the owner's word:** 0210's R1 reproduction re-run in the three containers.
  * After stage 1: each still fails safe, with the service up.
  * After stage 2: each hands off, updates, restarts and writes a result file. On OpenRC with a
    shared service cgroup it refuses before downloading.
  * The host's real service is not touched.

## Pros and Cons of the Options

### D1-A. All at once, after the release

* Good, because it is one change, as the issue describes.
* Bad, because the stdout risk, the `PATH` lookup and the probe gap wait on four library steps
  (L1).
* Bad, because U7 would either wait as well, or be written and then deleted.

### D1-B. Two stages

* Good, because every stage 1 line survives into stage 2.
* Neutral, because it is two PLAN phases rather than one.

### D1-C. Magic builds the adapter now

* Good, because the handoff could arrive without waiting on the library.
* Bad, because it duplicates what the owner placed in the library, and is the route 0210 declined
  (R5).
* Bad, because it would be deleted when the library ships.

### D2-A. A fixed list

* Good, because it matches the library's rule, and needs no new state.
* Bad, because an unusual install location needs the list extended.

### D2-B. Resolve once at setup

* Good, because it follows wherever the user installed the tools.
* Bad, because it changes the backend record's format, and stale paths outlive a reinstall.

### D2-C. `PATH`

* Bad, because an update run from cron or a bare shell resolves a different tool, or none.

### D3-A. Read the setting

* Bad, because it misses the per-service `conf.d` files and the pre-0.64 file (S1, S2), so it
  can allow a handoff that the stop then kills.

### D3-B. Shared OpenRC cgroup

* Good, because it reads one fact from the kernel and errs toward the fail-safe.
* Bad, because hosts with cgroups but cleanup off keep the refusal.

### D3-C. Shared cgroup and the setting

* Good, because it refuses least often.
* Bad, because it takes on D3-A's parsing risk.

### D3-D. No check

* Bad, because under cleanup the update dies after the stop, and the service is left down.

### D4-A. Script definitions in `readServeArgs`

* Good, because it gives every backend the same application check.

### D4-B. No probe

* Bad, because the adapter's wait would then rest on supervisor state alone, unlike the other
  backends.

### D5-A. `settle`'s numbers

* Good, because it matches setup-service.
* Bad, because a 2 s window passes a daemon that dies a few seconds after it starts.

### D5-B. The other backends' bounds

* Good, because there is one bar for every backend's update.
* Neutral, because an update on these supervisors can take up to 10 s longer.

### D6-A. `settle` stays for setup-service

* Good, because the scope stays the update's backend.
* Neutral, because `settle` lives on until 0207 U6.

### D6-B. setup-service on the adapter too

* Bad, because it widens the change, and pre-empts U6.

### D7-A. `s6-svstat -o`

* Good, because it is documented, and gives the readiness and want fields directly.

### D7-B. The regular expression

* Bad, because it rests on prose s6 does not promise.

### D8-A. `v1.21.0` first, under 0207

* Good, because each bump carries one release's change.

### D8-B. One jump

* Bad, because three releases' changes land in one step.

## More Information

### What this record changes in others, once accepted

Each change is a dated note. No history is rewritten.

* **0054-MADR S5-3:** the script backends move onto the library's adapter here.
* **0207-MADR D20 and 0207-PLAN U7:** retired into this record (R2).
* **0207-MADR D4 and 0207-PLAN U6:** the script-kind start confirmation is built on this record's
  state functions (D6).
* **0210-MADR D4 and question 2:** the handoff on these supervisors comes through the adapter,
  with OpenRC's check (D3).
* **Issue #27:** a comment with R6's four corrections.

### Questions for the owner

1. **The direction.** Is "an adapter on the library" the library's own `NewSupervised` (this
   record's reading), or one built in Magic (D1-C)?

   *Answered 2026-10-24: "Library's NewSupervised".* The library ships the adapter. Magic
   supplies `State`, `Up` and `Down`, and keeps its scripts, PID files and session supervisors.
2. **D1.** Stage 1 now, on the current pin, and U7 retired?

   *Answered 2026-10-24: "Two stages, retire U7".*
3. **D3.** The cgroup check, rather than reading `rc_cgroup_cleanup`?

   *Answered 2026-10-24: "Shared cgroup check".*
4. **D5.** The other backends' bounds, rather than `settle`'s?

   *Answered 2026-10-24: "Same as systemd/launchd".* That is a 10 s settle window and a 60 s
   timeout. `MCREMOTE_SVC_WAIT_SECS`, when set, becomes the timeout.
5. **D8.** `v1.21.0` first, under 0207?

   *Answered 2026-10-24: "v1.21.0 first, under 0207".* A 0207 amendment and its own step move
   the pin. This record's stage 2 bump then carries only the adapter. Written as
   [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)'s
   amendment C (D29) and the plan's step U1b.

### Evidence index

* Issue: <https://github.com/maccavelli/magic-cli-remote/issues/27>.
* Library: <https://github.com/maccavelli/go-selfupdate-lib/issues/2>. Its 0035-MADR and
  0035-REPORT, cited by file name. The library is another repository, and the records are not
  yet pushed.
* OpenRC: <https://github.com/OpenRC/openrc/blob/master/NEWS.md>,
  <https://github.com/OpenRC/openrc/blob/master/user-guide.md>,
  <https://github.com/OpenRC/openrc/blob/master/etc/rc.conf>.
* This repository at 793b8f5b:
  * `internal/cli/service/`: `scriptbackend.go`, `scriptbackend_inside_linux.go`, `backend.go`,
    `control.go`, `settle.go`, `setup.go`, `supervisor.go`, `supervisor_unix.go`, `health.go`;
  * `internal/updateclient/`: `client.go`, `managed.go`.
* Records:
  * [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md);
  * [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md);
  * [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md);
  * [0210-MADR-hand-off-agent-run-updates-outside-the-service.md](0210-MADR-hand-off-agent-run-updates-outside-the-service.md);
  * [0210-PLAN-hand-off-agent-run-updates-outside-the-service.md](0210-PLAN-hand-off-agent-run-updates-outside-the-service.md).
* The PLAN follows once the owner answers the questions above.

## Amendment — 2026-10-24 (A): what the library's design and testing add

**Status of this amendment: proposed.**

The owner asked, verbatim: "deep dive into the design/decisions/documentation/developed code in the
go-selfupdate-lib codebase and see if we can use any of it here to improve stability, enhance
robustness and standards/best practices adherence, and leverage all of that testing to overhaul and
improve the backends here in magic-cli-remote." Asked whether the library's adapter still ships, the
owner answered "Adapter still ships". So D0 to D8 above stand.

The evidence is
[0212-REPORT-what-the-service-backends-can-take-from-go-selfupdate-lib.md](../reports/0212-REPORT-what-the-service-backends-can-take-from-go-selfupdate-lib.md).
Its findings are cited here as B1 to B12, and its recommendations as R1 to R10.

### Corrections to the facts above, dated 2026-10-24

* **L1** said the library's 0035-MADR was proposed and had no PLAN. It is now **accepted**, with D1
  to D14 chosen. Its `0035-PLAN-serve-supervisors-through-a-supervised-adapter.md` is proposed, and
  ships the adapter as **`v1.22.0`**. The library's `main` is four commits ahead of `origin/main`,
  still unpushed (B10).
* **L4** lacks two of the owner's choices there:
  * **D4-C:** `SupervisedOptions.Kill` and `KillAfter`. `KillAfter` defaults to half of
    `Poll.Timeout`, and is refused unless it is shorter.
  * **D13-C:** the `/proc` reads move to a new `internal/procfs`.

  Its step 6 for this repository adds: "Set `Kill` where the supervisor does not escalate itself:
  on runit, `sv kill <dir>`" (B10).
* **D5,** as answered ("Same as systemd/launchd"), was written up as a 60 s timeout. The library's
  systemd backend bounds its stop by the longer of `Poll.Timeout` and the unit's `TimeoutStopUSec`
  plus 30 s. For Magic's unit, `TimeoutStopSec=45`, that is **75 s** (B4). D11 below asks the owner
  to confirm that reading.

### What the deep dive found, in brief

* **The library has run no probe of these supervisors.** Its 0035 PLAN designs them (M1, P1 to P4)
  and a live s6 test (M6). The live testing of runit, s6 and OpenRC is Magic's own, from the 0210
  PLAN, run with a kit that exists only in a session scratchpad (B8, B9).
* **Magic's script definitions fall short of its own systemd unit and launchd job (B1):**
  * no logs, on all three;
  * no kill when the daemon hangs on stop, on all three. OpenRC's stop is `SIGTERM/5` and then
    gives up;
  * no restart after a crash, on OpenRC.
* **OpenRC is held back by Magic's own `start()` and `stop()`, which turn off
  `supervise-daemon`** (B2). From 0.62, `notify=socket:ready` accepts the `READY=1` Magic's daemon
  already sends.
* **The library's command rules** (its 0011 §2 and 0025 D2) add three things to stage 1 (B5):
  * a built environment with `LC_ALL=C` and a fixed `PATH`;
  * `--` before names;
  * `*service.CommandError` with its classification.
* **Setup finds the tools on `PATH`**, where the update will look in fixed directories (B6).
* **Setup's start check compares bare PIDs**, though Magic already has `procutil.ProcessStartToken`
  (B7).

### Considered options

#### D9: the runit and s6 definitions

* **D9-A.** Parity with the unit:
  * a `log/run` service directory, `svlogd` for runit and `s6-log` for s6, writing to
    `$XDG_STATE_HOME/<product>/log`;
  * for s6, a `timeout-kill` file of 45000 ms, so s6 itself kills a daemon still alive 45 s after
    `s6-svc -d`;
  * for runit, which has no such file, `Kill` (`sv kill <dir>`) passed to the adapter in stage 2.
* **D9-B.** Logs only.
* **D9-C.** No change.

#### D10: the OpenRC script

* **D10-A.** OpenRC's default functions:
  * `command`, `command_args` and `directory`;
  * `supervisor=supervise-daemon`, with `respawn_delay=5`, and `respawn_max` and `respawn_period`
    set to the unit's `StartLimitBurst=30` in 300 s;
  * `retry="TERM/45/KILL/5"`;
  * `output_log` and `error_log`;
  * on OpenRC 0.62 or later, `notify=socket:ready`.

  The environment goes through `supervise_daemon_args`. Probes first (B2): whether
  `supervise-daemon`, `notify` and `--env` work under `openrc --user` on 0.62.6, and which PID the
  `pidfile` then holds. **If a probe fails, D10-B applies.**
* **D10-B.** Keep `start-stop-daemon` and the custom functions. Add `--retry TERM/45/KILL/5` to the
  stop, and the logs, through the flags `start-stop-daemon` documents for them. These are checked
  in the PLAN. OpenRC still restarts nothing.
* **D10-C.** No change.

#### D11: the adapter's bounds

* **D11-A.**
  * `Poll.Timeout` 75 s, the stop bound the library's systemd backend gives Magic's unit (B4).
  * `KillAfter` 45 s, the drain the daemon gets everywhere else.
  * `MCREMOTE_SVC_WAIT_SECS`, when set, still becomes `Poll.Timeout`. A value not above
    `KillAfter`, or not above the settle window, is refused.
* **D11-B.** The library's defaults: a 60 s timeout, and a kill at 30 s.

#### D12: the command layer and process identity (stage 1)

* **D12-A.** Stage 1 also takes the following:
  * **A built environment:** `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, `LC_ALL=C`, and
    `XDG_RUNTIME_DIR`. `HOME` and `USER` too, where the tool reads them.
  * **`--` before names,** where a tool accepts it.
  * **Typed failures.** A failed tool is a `*libservice.CommandError` (`Backend` is the kind).
    It is wrapped with `ErrPermission`, `ErrNotInstalled` or `ErrNoSupervisor` where the
    supervisor's exit code or message says so. The PLAN measures each mapping.
  * **One tool resolver** for `detectLinux` and for the update, so setup refuses what the update
    could not drive (B6).
  * **`settle` compares PID and start token,** through `procutil.ProcessStartToken` (B7).
  * **The OpenRC PID file** has no `/tmp` fallback: no `XDG_RUNTIME_DIR` is an error, as
    `startOpenRC` already makes it (B7).
* **D12-B.** Stage 1 as D1-B defined it, without these.

#### D13: the tests

* **D13-A.** The 0210 kit moves into the tree, as these:
  * Containerfiles with pinned images, Void included;
  * env-gated live tests, `MCREMOTE_REQUIRE_RUNIT`, `MCREMOTE_REQUIRE_S6` and
    `MCREMOTE_REQUIRE_OPENRC`, in the library's pattern. Each installs, stops, starts, updates from
    inside, and removes;
  * a `make live-supervisors` target that runs them in the containers;
  * captured tool output as fixtures, named by supervisor, version and state, replayed through the
    `State` functions.

  CI runs runit and s6 on its Ubuntu runner, as the library's M6 does for s6. OpenRC runs locally
  through the target, since it needs a container booted with its init.
* **D13-B.** As D13-A, with no CI step.
* **D13-C.** The kit stays in a scratchpad.

#### D14: the probes

* **D14-A.** Take the library's 0035 PLAN M1 results (P2 to P4) for the supervisors' own
  behaviour. Magic probes only:
  * its definitions (D9, D10);
  * its tool paths;
  * its stdout question (M9).
* **D14-B.** Magic repeats every probe itself.

#### D15: existing installs

* **D15-A.** `recoverScript` reads both the current and the new forms of each definition
  (`supervisor.go:245-340`).
  * `setup-service --refresh` then moves an existing install onto D9 and D10. The update's
    reconcile child runs that refresh, so an update does it unattended.
  * A definition it cannot read is kept, as today (`VerdictKept`).
* **D15-B.** New installs only. An existing install needs `--force`.

### Proposed decisions

* **D9: "D9-A".** Every gap it closes is closed by the supervisor's own documented mechanism (B1,
  B3). Logs alone (D9-B) leave a hung stop unkilled.
* **D10: "D10-A", with D10-B as the fallback the probes decide.** D10-A gives OpenRC the restart,
  the kill, the logs and the readiness the unit has (B2). openrc-run(8) calls `supervise-daemon`
  the recommended option.
* **D11: "D11-A".** It is what "the same as systemd" measures to (B4). With D11-B the adapter would
  kill a daemon inside the 45 s drain it is allowed on systemd and launchd.
* **D12: "D12-A".** Each item is the library's rule (B5) or uses code Magic already has (B7), at
  little cost. Each removes a way the update could misread a tool, or setup and the update could
  disagree.
* **D13: "D13-A".** The live runs are the most valuable testing this work has. They should outlive
  a scratchpad, and catch a regression in CI where a runner allows (B8).
* **D14: "D14-A".** The probes are the same, and the library plans them and reports them to issue
  #27 (B9).
* **D15: "D15-A".** Without it, D9 and D10 reach no existing install unless a person passes
  `--force`.

### Where each lands

* **Stage 1, which does not wait on the library:** D9's logs and s6 `timeout-kill`; D10; D12;
  D13; D15.
* **Stage 2:** D9's runit `Kill`, and D11's `KillAfter` and `Poll`.
* **Confirmation:** D14 replaces the overlapping probes in this record's Confirmation.

### Consequences of the amendment

* Good, because the three supervisors restart, stop and log as the unit and the job do. Where
  OpenRC allows it, they also start only once the daemon serves.
* Good, because the live runs become a repeatable check, and the parsers get real fixtures.
* Good, because setup and the update agree on which tools they can drive.
* Neutral, because stage 1 grows, but none of it is thrown away in stage 2.
* Bad, because D10-A rests on three facts about `supervise-daemon` under `openrc --user` that
  nothing has measured. D10-B is the fallback.
* Bad, because D13-A adds a CI step, and two container images to keep current.
* Bad, because D15-A widens `recoverScript`, which must read two forms of each definition until the
  old form is retired.

### Confirmation of the amendment

* **The probes of D10, first, in the OpenRC container:**
  * `supervise-daemon` under `openrc --user`;
  * `notify=socket:ready` with Magic's daemon: `rc-service start` returns after `READY=1`;
  * `supervise_daemon_args --env`;
  * which PID the `pidfile` holds;
  * a crash restarted after `respawn_delay`;
  * a hung stop killed at 45 s.
* **runit and s6:** logs written under `$XDG_STATE_HOME/<product>/log`; a daemon that ignores TERM
  killed at 45 s, by `timeout-kill` on s6 and by the adapter's `Kill` on runit.
* **Each new test seen failing first:**
  * the built environment, with a planted inherited `PATH`;
  * the resolver, with a planted bare name;
  * `CommandError`'s classification;
  * `settle` against a reused PID;
  * the two-form recovery.
* **The live tests** pass in CI for runit and s6, and locally for OpenRC. An agent-run update hands
  off and completes on each, after stage 2.

### Questions this amendment adds

* **6. D9 and D10.** Bring the definitions to the unit's level, with OpenRC on `supervise-daemon`
  if the probes pass, and on `start-stop-daemon` with `--retry` if not?

  *Answered 2026-10-24: "Full parity, probe-gated (Recommended)".* D9-A, and D10-A with D10-B as
  the fallback the probes decide.
* **7. D11.** Read "the same as systemd" as a 75 s bound with the kill at 45 s?

  *Answered 2026-10-24, as restated in amendment B: "75 s, kill at 45 s (Recommended)".* D11-A as
  corrected by amendment B's D17.
* **8. D13.** The kit in the tree, with CI for runit and s6?

  *Answered 2026-10-24: "In tree, CI for runit+s6 (Recommended)".* D13-A.

D2, D4, D6, D7, D12, D14 and D15 have no question. They are proposed, and go to the owner with the
PLAN.

## Amendment — 2026-10-24 (B): the adapter is released as `v1.22.0`

**Status of this amendment: proposed.**

The owner asked, verbatim: "assess the latest releases of go-selfupdate-lib, acp-go-sdk2,
go-llmprovider-sdk, and ensure we are pinning latest. assess the impact of the new releases on our
planning." The pins are
[0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)'s,
amendment E. This amendment covers what the release changes here.

### Facts [read, go-selfupdate-lib at `v1.22.0`, `8480556`]

* **V1. The release.**
  * `v1.22.0` is tagged on `84805563ed252df384f59ced45a3daf529702ab4` (tag object `b7c2181…`),
    2026-10-24. It is the newest tag.
  * The library's 0035-PLAN records M0 to M8 as done, with the tag's CI run passing all 18 jobs.
    That includes `TestLiveSupervisedS6` on `ubuntu-24.04`. Its M9, the comments on library issue #2
    and on #27, waits on its owner. Both issues are open.
* **V2. The API is the one L4 and amendment A describe,** with one more field:
  `SupervisedState.Instance`, `<pid>@<start time>`, which the adapter fills and `State` leaves
  empty. No file of the adapter has a build constraint, so its types compile on every OS
  (`supervised.go`, `supervised_lifecycle.go`).
* **V3. `KillAfter` needs `Kill`.** `NewSupervised` refuses a `KillAfter` set without a `Kill`, or
  one not shorter than `Poll.Timeout`. A `Kill` with no `KillAfter` gets half of `Poll.Timeout`
  (`supervised.go:170-177`). One `Poll` bounds both the stop's wait and the health wait
  (`SupervisedOptions.Poll`).
* **V4. The handoff goes through the hop.** The library's 0035-PLAN deviation D2 found that a
  direct `DetachProcess` left the run inside the service, so the adapter's `Stop` refused it.
  `Detach` now sets `SELFUPDATE_HANDOFF_HOP=1`, as the SCM handoff does. The program must complete
  the hop first in `main`. Both of Magic's `main`s already do, through
  `libservice.LoadHandOffEnvContext` (`cmd/mcremote/main.go:24`, `cmd/mcrelay/main.go:25`), which
  calls `HandOffHopContext` (library `handoffenv.go:107`).
* **V5. The health wait's restart rule** (the library's deviation D3). `WaitHealthy` fails on
  `Restarting` only:
  * after a new instance was seen up;
  * or when it lasts, unbroken, longer than the settle window, with no new instance ever up.

  One restart before the first up is tolerated. That is the tolerance `settle` has today.
* **V6. The library's probes are in** (its 0035-MADR amendment A1, P1 to P4):
  * **s6** (`s6` 2.12.0.3): `s6-svstat -o up,wantedup,normallyup,ready,pid,exitcode,updownfor`
    prints `false` for `ready` on a service with no `notification-fd`. No supervisor: `s6-svstat`
    exits 1, and `s6-svc` exits 100.
  * **runit** (2.1.2): no supervisor is exit 1, "unable to open supervise/ok". `sv kill` ended a
    program that ignores TERM.
  * **OpenRC** (0.63.2, Alpine 3.24.3, in a privileged Docker container): `status` exits 0, 3 and
    32 for started, stopped and crashed. A *user* service's daemon and its detached run were both in
    `0::/`, so a user service had no service cgroup. With `rc_cgroup_cleanup=YES`, the detached run
    survived `rc-service stop`. A real OpenRC host is unmeasured.
  * A detached run survived each supervisor's stop.
* **V7. What else moved.** `git diff --stat v1.21.0 v1.22.0`, outside the documents, touches only
  the adapter's files, `internal/procfs`, `errors.go` (`ErrNoSupervisor`), `doc.go`, a
  `//nolint:gosec` comment in `sameexec.go`, `ci.yml` and `.golangci.yml`. The files this record and
  0207's amendment D cite are unchanged since `fff0940`: `managed.go`, `probe.go`, `types.go`,
  `handoff.go`, `detach.go`, `detach_unix.go`, `poll.go` and `runner.go`.

### What the release changes here

* **E1. Stage 2 no longer waits.** The Consequences' "stage 2 depends on a library decision, plan
  and release that do not exist yet" (L1) no longer holds. D1's two stages stand. Stage 2 can
  follow stage 1 directly.
* **E2. The bump moves to 0207.** 0207's amendment E moves U1b's pin to `v1.22.0`, by the owner's
  ask above. So:
  * stage 2's step 1, "The bump", has nothing left to do;
  * D8's answer, "v1.21.0 first, under 0207", is kept in spirit: the pin moves under 0207, in its
    own step. Its consequence, "this record's stage 2 bump then carries only the adapter", lapses;
  * "One change per pin" still holds for Magic's code: `v1.22.0` adds only code Magic does not call
    until stage 2 (V7).
* **E3. D3 is confirmed where it was measured.** In the OpenRC probe, a user service had no service
  cgroup (V6), so D3-B's check would never refuse there. The outcome of D3 foresaw this and called
  it correct. A system service, and a real host, stay unmeasured. D3-B's refusal still protects them.
* **E4. D11-A, as written, would be refused** (V3). It sets `KillAfter` 45 s for every kind, but
  only runit gets a `Kill` (D9-A). D17 below corrects it.
* **E5. s6's readiness** (V6). D9-A gives the s6 service no `notification-fd`, so `s6-svstat`'s
  `ready` would read `false` for a healthy daemon. D18 below.
* **E6. D14-A's premise holds.** The library's P2 to P4 ran and reported, as D14-A expected. D10-A's
  own probes, `supervise-daemon` under `openrc --user`, were not among them, and stay Magic's.
* **E7. No change from the hop** (V4) **or the restart rule** (V5).

### The decisions this amendment proposes

* **D16 — Stage 1 uses the library's types.**
  * Stage 1's per-kind state is a `libservice.SupervisedState`, not a Magic struct with the same
    fields. Stage 2 then passes the stage 1 functions to `NewSupervised` unchanged.
  * This needs 0207's U1b, at `v1.22.0`, to land before stage 1. Stage 1 and stage 2 then become
    two consecutive phases of this record's PLAN.
  * The other route, a Magic copy of the struct, would be deleted in stage 2. That is the "nothing
    thrown away" driver.
* **D17 — D11-A, corrected for the released API.**
  * `Poll.Timeout` is 75 s for all three kinds, as D11-A says. It also bounds the health wait, so
    these supervisors wait up to 75 s for health, where systemd and launchd wait 60 s.
  * **runit:** `Kill` is `sv kill <dir>`, and `KillAfter` is 45 s.
  * **s6:** no `Kill`. s6 kills the daemon itself, by D9-A's `timeout-kill` of 45000 ms.
  * **OpenRC:** no `Kill`. The stop's own `retry` kills it: `TERM/45/KILL/5`, by D10-A or D10-B.
  * `MCREMOTE_SVC_WAIT_SECS`, when set, still becomes `Poll.Timeout`. On runit it must be above
    45 s, as V3 requires. On every kind it must be above the settle window.
  * The PLAN measures how long OpenRC's `Down` blocks, since `rc-service stop` runs the retry
    schedule before it returns.
* **D18 — `Ready` is nil on all three kinds.**
  * s6's `State` reads `ready` but does not map it, since the service has no `notification-fd`
    (E5).
  * runit and OpenRC report no readiness.
  * If a later record gives the s6 service a `notification-fd`, `Ready` follows it there.

### Consequences of the amendment

* Good, because stage 2 can follow stage 1 without waiting on the library.
* Good, because stage 1's state is written once, in the library's type (D16).
* Good, because D11's bounds are now ones `NewSupervised` accepts (D17).
* Neutral, because the health wait on these supervisors is 75 s, not 60 s (D17).
* Bad, because stage 1 now waits on 0207's U1b (D16), which in turn waits on the owner's approval.

### Confirmation of the amendment

* A test builds each kind's `SupervisedOptions` as the backend does and calls `NewSupervised`. It
  succeeds for all three. Seen failing on a scratch copy with `KillAfter` set on s6.
* A fixture test: s6's `State`, over a captured `s6-svstat -o` line with `ready` `false`, returns
  `Ready` nil.
* The PLAN records how long OpenRC's `rc-service stop` takes against a daemon that ignores TERM.

### Questions this amendment adds

* **9. D16.** Stage 1 on `libservice.SupervisedState`, after U1b, with both stages as consecutive
  phases of one PLAN?

  *Answered 2026-10-24: "SupervisedState, one plan (Recommended)".*
* **7, restated with D17.** A 75 s bound everywhere, with the kill at 45 s: by the adapter on runit,
  and by the supervisor itself on s6 and OpenRC?

  *Answered 2026-10-24: "75 s, kill at 45 s (Recommended)".*

D18 had no question. It goes to the owner with the PLAN, with D2, D4, D6, D7, D12, D14 and D15.

### Owner answers recorded 2026-10-24

* **Decided:** D9 (D9-A), D10 (D10-A, falling back to D10-B on a failed probe), D11 as D17, D13
  (D13-A) and D16.
* **Still proposed:** D2, D4, D6, D7, D12, D14, D15 and D18. They go to the owner with the PLAN.
* **The owner's word on execution, verbatim:** "U1b, then write 0212 PLAN". The PLAN is
  [0212-PLAN-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-PLAN-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md),
  `proposed`.

### Evidence index of the amendment

* go-selfupdate-lib at `v1.22.0`: `CHANGELOG.md`; migration guide §22;
  `selfupdate/service/supervised.go`, `supervised_lifecycle.go`, `errors.go` and `handoffenv.go`;
  its 0035-MADR amendment A1, and its 0035-PLAN execution record (M1 to M8, deviations D2 and D3).
  `git diff --stat v1.21.0 v1.22.0` and `git diff --stat fff0940 v1.22.0` over the files V7 names.
* `git ls-remote --tags` of the library. `gh issue view` of library issue #2 and of #27, both open.
* This repository at `793b8f5b`: `cmd/mcremote/main.go` and `cmd/mcrelay/main.go`.
