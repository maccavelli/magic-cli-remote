---
status: accepted
date: 2026-10-23
decision-makers: Project Owner
consulted: "go-selfupdate-lib v1.17.2 (036289ec), go doc -all of selfupdate/cli, selfupdate/service and its systemd, launchd and taskscheduler backends, and its 0011 and 0021 records; acp-go-sdk2 v1.20.0 agentproc (tree_windows.go); grok-build at 2bdd1d6a (its terminal backend selection); Microsoft Learn's Nested Jobs, Job Objects, Process Creation Flags and JOBOBJECT_BASIC_LIMIT_INFORMATION pages, read 2026-10-22; this repository at b946cf2d"
informed: anyone who changes the update command, the service backends, or how agents' processes are contained
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Hand an agent-run update off to the service manager, and make every backend fail safe

## Context and Problem Statement

`mcremote` runs as a user service: a systemd user unit, a launchd agent, a per-user Task Scheduler
task, or a runit, s6 or OpenRC user service. Through it, the phone's user drives agent CLIs that
run shell commands for them. One of those commands can be `mcremote update`. That run is then a
descendant of the service it is about to stop, swap and restart.

[0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
left the library's handoff unadopted, under "What stays deliberately unadopted":

> **The `service.Detacher` handoff:** updates run from an operator shell, never inside the daemon,
> so nothing would call it. If the phone ever triggers updates, this is the seam to adopt.

That premise no longer holds: the phone triggers updates through an agent.
[0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)
F1 named the gap, and its D3 proposed adopting the handoff "after reproducing it". F1 was inferred
from code. [0207-REPORT-go-selfupdate-lib-deeper-adoption-assessment.md](../reports/0207-REPORT-go-selfupdate-lib-deeper-adoption-assessment.md)
A1 then corrected it in part. This record re-reads all of it against the tree at `b946cf2d`, and
decides D3 in full.

What happens to an agent-run `mcremote update` today, and what has to change for it to update the
daemon, on every service manager Magic supports?

### What happens today [from code; nothing here is reproduced yet]

**The update command sets no handoff.** `internal/updateclient/client.go:250` calls
`cli.Run(ctx, u, req, o)` with `o` from `cli.StdioOptions()`, and `Options.HandOff` is its zero
value, which "never hands off" (`selfupdate/cli`, `Options.HandOff`). The managed installer stops
the service before it applies anything (`selfupdate/managed.go:192-196`, 0207-REPORT A1).

| Service manager | Backend | Is the agent's update "inside"? | What its `Stop` does from inside | Outcome today |
| :--- | :--- | :--- | :--- | :--- |
| systemd (user) | the library's `systemd.Unit` | yes: `Inside` compares this process's cgroup with the unit's `ControlGroup`. The unit is `KillMode=control-group` (`mcremote.user.service.tmpl:44`), and `Setpgid` (`procutil_unix.go:29`) does not leave the cgroup. | refuses with `service.ErrInsideService` before changing anything (`systemd/lifecycle.go:72-83`) | **fails safe:** `update failed: … this process runs inside the service it would stop`, exit 1, daemon still up, binary unchanged (`managed.go:278-300`, `recoverStop`) |
| launchd | the library's `launchd.Job` | yes: the job's process "is one of this process's ancestors" (`launchd.Job.Inside`) | refuses with `ErrInsideService` (`launchd/lifecycle.go:106`) | **fails safe**, as above |
| Task Scheduler | the library's `taskscheduler.Task` | yes, while the parent chain reaches a running instance: `Inside` "walks up the recorded parents" (`taskscheduler/detach.go:15-49`) | refuses with `ErrInsideService` (`taskscheduler/lifecycle.go:100-101`) | **fails safe**, except for the edge in the next paragraph |
| runit, s6, OpenRC (user) | Magic's own `scriptBackend` (`internal/cli/service/backend.go:59-60`) | there is no check at all | `Stop` runs `sv down`, `s6-svc -d` or `rc-service --user … stop` at once (`scriptbackend.go:80-104`) | **not safe:** see F2 |

**F1 — On the library's three backends an agent-run update fails safe, and cannot succeed.** This
is 0207-REPORT A1's correction of 0207 F1, which said the service would be left stopped. It is
confirmed here against the same library code at `v1.17.2`, which is now the pin. The phone's user
cannot update Magic through an agent at all. The operator must open a shell outside the service.

**F2 — On runit, s6 and OpenRC an agent-run update can leave the service stopped.** This is new,
and not in 0207-REPORT, whose A1 covered the library's backends only.

* `scriptBackend.Stop` has no inside guard. The stop takes the daemon down, and the daemon's
  teardown ends the agent's process group (`acpagent/terminal.go:39`,
  `codex/provider.go:760`, `procutil.KillProcessGroup`), which holds the update. The update dies
  before the swap and the start, and the supervisor has been told to keep the service down.
* That ordering is read from the code. Whether the update is killed before `Install` gets past
  the stop is to be reproduced (Confirmation).
* *Reproduced 2026-10-23* (the PLAN's R1) on runit, s6 and OpenRC 0.62.6. An agent-run update
  left each service down, and the binary unchanged, with no result written. s6 read `down (exitcode
  0) … normally up`, and OpenRC `* status: stopping`.

**F3 — On Windows, Magic's own job, not Task Scheduler's, is what would kill a handed-off run.**

* Task Scheduler's job has **no** limit flags: no `JOB_OBJECT_LIMIT_BREAKAWAY_OK` and no
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. A process left in it survives `Stop`. The library measured
  this as its 0021-MADR Amendment A1, probe 2. It is why `taskscheduler.Task.Detach` hops: the hop
  "starts the real run detached and exits", and the run "survives it, even inside the task's job,
  which refuses breakaway".
* Magic puts the processes it starts in jobs of its own, with `KILL_ON_JOB_CLOSE`:
  * `procutil.SuperviseStarted` (`procutil_windows.go:261-271`, `LimitFlags:
    JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` only) holds codex's engine (`codex/provider.go:625`),
    opencode's and kilo's (`httpagent/provider.go:554`), provider-auth CLIs
    (`providerauth/cli.go:129`), and every **terminal Magic runs for an ACP agent**
    (`acpagent/terminal.go:111`).
  * The ACP agent process itself starts under `agentproc`. Its job has
    `KILL_ON_JOB_CLOSE | BREAKAWAY_OK` (`acp-go-sdk2@v1.20.0/agentproc/tree_windows.go:73`).
* **grok's shell commands go through Magic's terminal, not agentproc's job** [from grok's source,
  not yet observed on the wire]. grok picks the client's terminal when the client advertises one
  and a gateway exists (`xai-grok-shell/src/session/acp_session_impl/spawn.rs:3022`,
  `TerminalBackendKind::AcpClient`). Magic advertises `Terminal: true` to every ACP spec
  (`acpagent/acpagent.go:1102`). So 0207's amendment note, "agentproc's job allows breakaway, so
  D3's Windows measurement no longer has Magic's agent job in the way", does not hold for an
  update grok runs: that update is in `SuperviseStarted`'s job, which refuses breakaway.
* When the daemon stops, it closes its job handles, and Windows "terminates all processes
  associated with the job and its child jobs in the hierarchy"
  ([Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)). The
  library's `DetachProcess` asks for `CREATE_BREAKAWAY_FROM_JOB`, and a refusal "is then retried
  inside the job" (0207-REPORT A1, `detach_windows.go`). So without breakaway, a handed-off run is
  killed mid-update, after the daemon has been stopped. That is the outage 0207 F1 feared.
* **The rule that makes breakaway enough**
  ([Nested Jobs](https://learn.microsoft.com/en-us/windows/win32/procthread/nested-jobs)):
  > "If the immediate job object allows breakaway, the child process breaks away from the
  > immediate job object and from each job in the parent job chain, moving up the hierarchy until
  > it reaches a job that does not permit breakaway."

  With `BREAKAWAY_OK` on Magic's job, the immediate one, the hop leaves it and stops at Task
  Scheduler's job, where it survives (probe 2). The page does not say this outright for a parent
  that refuses; it is read from the sentence above, and the Confirmation measures it.
* **The existing edge** (0207-REPORT A1): if an intermediate process in the agent's chain has
  exited, `Task.Inside`'s walk "ends at a parent that has exited". The update is then judged
  outside, and its `Stop` drains the daemon while the update is still in Magic's job, so it dies.
  This is from reading only.

**F4 — The rest of the library's wiring is small, and has two constraints.** From `go doc -all` at
`v1.17.2`:

* `cli.Options.HandOff{Detach: service.HandOffFunc(d, spec), Report: service.ReportFunc()}`.
  Detach "runs before an apply that would install, never a check, a dry run, or a run with nothing
  to install". When it hands off, "the run ends at once: `update handed off: <detail>` on Stderr,
  or under JSON a result object with `handed_off`, and exit 0".
* "A program calls `ReportFunc`, or `LoadHandOffEnv`, first in main." That applies the launchd
  handoff's private environment file, and finishes a Windows hop, which "starts the real run and
  exits".
* `systemd.Unit.Detach` starts "a transient service, `<unit>-selfupdate-<id>.service`, whose
  parent is the service manager", with "the environment in a 0600 file, never on the command
  line", and "needs systemd 236 or later". `launchd.Job.Detach` loads "a one-shot job of its own,
  `<label>.selfupdate.<id>`". `taskscheduler.Task.Detach` hops (F3).
* **Constraint 1 — no prompt.** "The detached run cannot ask for confirmation, so inside the
  service a request without Yes is refused with `selfupdate.ErrConfirmationRequired`." An agent
  must run `mcremote update --yes`.
* **Constraint 2 — the result is read later.** The detached run writes a `HandOffResult` to
  `DefaultResultPath`, `.<base>.selfupdate.handoff` beside the executable. Nothing in Magic reads
  it.
* `launchd.Job.CleanupHandOffs` "boots out the job's finished one-shot handoff jobs … a program may
  call it at start-up".

## Decision Drivers

* An update started from the phone must either complete or fail safe. The daemon is never left
  down by an update it started itself.
* The library owns the handoff (0054 O1, "every concern the library owns moves to it"). Magic
  wires it and does not write its own.
* Every service manager Magic installs to gets the same guarantee, and where the library has no
  backend, Magic's own backend must at least fail safe.
* Containment of agents' processes (0116 D8) is loosened only as far as the handoff needs, and the
  loosening is stated.
* Every claim from code is reproduced before the change and confirmed after, on the matching host.
* An agent reads the outcome as text, and so does the operator later.

## Considered Options

* **A** — Adopt the library's handoff on systemd, launchd and Task Scheduler. On Windows, allow
  breakaway from Magic's own job. Give the script backends an inside guard that fails safe. Report
  the last handoff in `doctor`.
* **B** — As A, but keep the handoff off Windows. An agent-run update there keeps failing safe.
* **C** — As A, but on Windows ask the library for a Task Scheduler-native detach, a one-shot task
  as systemd and launchd already do, instead of breakaway.
* **D** — As A, and also hand off on runit, s6 and OpenRC through `service.ProcessDetacher` with
  Magic's own inside check.
* **E** — Change nothing; document that updates run from an operator shell.

## Decision Outcome

Proposed: "A" on systemd and launchd, with "C" on Windows (the owner, 2026-10-23). This lets the
phone's user update Magic through an agent wherever the library can detach without loosening
Magic's containment, and it makes every other backend fail safe. Windows joins when the library
ships a one-shot-task detach, go-selfupdate-lib#6, and 0116 D8 stays as it is.

*Accepted 2026-10-23, after the PLAN's L1 confirmed it on every target:* D1, D2 and D4 to D7, as
the plan approved by the owner set out. On systemd and launchd an agent-run `mcremote update
--yes` handed off and the daemon restarted on the new release. On Windows, runit, s6 and OpenRC it
failed safe with the service up. D3 is decided (option C) and stays open on go-selfupdate-lib#6:
when #6 ships, an amendment here turns the Windows handoff on.

* **D1 — The shared update command hands off.**
  * `updateclient.NewCommand` sets `o.HandOff` to `service.HandOffFunc(d, service.HandOff{Args:
    os.Args[1:]})` and `service.ReportFunc()`. Here `d` is the same backend the managed installer
    drives (`service.NewBackendWith`), used as a `service.Detacher`.
  * Both products get it, because the command is shared (`internal/relay/update.go:13`). mcrelay
    runs no agents, so its `Inside` is false and it never hands off.
  * A backend that is not a `Detacher` sets no handoff: the script backends (D4). Neither does
    Windows until go-selfupdate-lib#6 ships (D3).
* **D2 — Both `main`s load the handoff environment first.** `cmd/mcremote/main.go` and
  `cmd/mcrelay/main.go` call `service.LoadHandOffEnvContext` before `cli.Execute`, with a context
  of 15 s, above the hop wait's own 10 s. A failure is printed and exits 1, as other start-up
  errors do. In a Windows hop, that call starts the real run and exits.
* ~~**D3 — On Windows, Magic's own job allows breakaway.**~~ *Replaced 2026-10-23 by the owner's
  answer to question 1.* The original text follows, for the record:
  `procutil.SuperviseStarted` sets `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
  JOB_OBJECT_LIMIT_BREAKAWAY_OK`, as `agentproc` does, amending 0116 D8.
* **D3 — On Windows, the handoff waits for a library detach that needs no breakaway** (option C).
  * Filed as [go-selfupdate-lib#6](https://github.com/maccavelli/go-selfupdate-lib/issues/6): a
    one-shot task, whose parent is Task Scheduler, as `systemd.Unit.Detach` and
    `launchd.Job.Detach` already do.
  * Until a release has it, Magic sets no handoff on Windows. `updateclient` sets `o.HandOff`
    only when the backend is a `Detacher` and the OS is not Windows.
  * An agent-run update there keeps failing safe, `ErrInsideService` (F1), except in F3's edge. If
    an exited intermediate parent misleads `Task.Inside`, the update drains the daemon and dies in
    Magic's job. That edge stays open until #6 ships, and is measured in the reproduction.
  * 0116 D8 is unchanged: `SuperviseStarted` keeps `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` alone.
  * When #6 ships, an amendment here moves the pin and turns the Windows handoff on.
* **D4 — The script backends fail safe.**
  * `scriptBackend.Stop` refuses with `service.ErrInsideService` when this process descends from
    the service's process. It walks this process's ancestors, by `/proc/<pid>/stat`, to the
    supervised process the backend already samples.
  * The managed installer then leaves the service running and fails the update, as on the
    library's backends (`recoverStop`).
  * No handoff there; option D is declined below. A handoff waits for the library's backends,
    `go-selfupdate-lib` issue #2 (the owner, 2026-10-23).
  * **The supervisors' documented stop semantics** (read 2026-10-23), which bound what the guard
    protects:
    * runit, `sv down`: "send it the TERM signal, and the CONT signal … After it stops, do not
      restart service" ([sv(8)](http://smarden.org/runit/sv.8.html));
    * s6, `s6-svc -d`: "send it a SIGTERM (by default) then a SIGCONT … and do not restart it",
      to "the supervised process" ([s6-svc](https://skarnet.org/software/s6/s6-svc.html)). Only
      `-P`, `-C` and `-K` reach its process group;
    * OpenRC: Magic's script stops with `start-stop-daemon --stop --pidfile`
      (`supervisor.go:180-184`). `rc_cgroup_cleanup` is `NO` unless set
      ([rc.conf](https://github.com/OpenRC/openrc/blob/master/etc/rc.conf)). No page says whether
      it applies to user services.

    So what kills an inside update is Magic's own teardown, `mgr.CloseAll`, which ends the
    agents' and terminals' process groups (`daemon.go:701`; `terminal.go:31-47`). The guard is
    what prevents it.
* **D5 — The outcome is reported.**
  * Under JSON, the agent sees `update handed off: <detail>` or `handed_off`, with exit 0, from
    the library.
  * `doctor` reads `DefaultResultPath` beside the running binary, with `service.ReadHandOffResult`.
    It reports the last handoff's ID, start and finish times, exit code and error, or "none".
  * On macOS, the daemon calls `launchd.Job.CleanupHandOffs` once at start, as the library
    suggests.
* **D6 — An agent runs `mcremote update --yes`.** The command's help and the operator guide say
  so, and why: the detached run cannot prompt (F4, constraint 1).
* **D7 — Reproduce first, confirm after**, on the matching host (the Confirmation below).

### Consequences

* Good, because the phone's user can update Magic through an agent on systemd and launchd. The
  daemon restarts on the new version, as it does for an operator's update.
* Good, because 0116 D8's containment on Windows is unchanged (D3).
* Bad, because on Windows an agent-run update keeps failing safe until go-selfupdate-lib#6
  ships. F3's edge, an exited intermediate parent, stays open until then.
* Good, because runit, s6 and OpenRC stop leaving the service down (F2). They fail safe, like the
  library's backends.
* Good, because the mechanism is the library's, tested there live on every OS, and Magic only
  wires it.
* Neutral, because an agent must pass `--yes`, and a handed-off update reports its final result
  only in the result file and `doctor`, not on the agent's terminal.
* Neutral, because the daemon restarts in the middle of the agent's session. The session ends, as
  it would for any update, and the phone reconnects.
* Bad, because the change is verified mainly by live runs on the hosts. Each one restarts a real
  user service.

### Confirmation

Before any change (D7). Each run uses a scratch service under its own name, with scratch state,
config and data directories, so no host's real service is touched (the test-host notes).

1. **Linux (systemd).** ~~A shell is moved into the scratch unit's cgroup (`cgroup.procs`).~~
   *Amended 2026-10-23:* the probe agent runs the update, as in item 2 (question 3, amended). An
   `mcremote update --yes` run from inside ends with
   `update failed: … this process runs inside the service it would stop`, exit 1. The daemon's
   PID and version are unchanged.
2. **macOS (launchd), Windows (Task Scheduler), and Linux runit, s6 and OpenRC in throwaway
   containers.** "Inside" is reached the way an agent reaches it, without spending provider quota.
   * A scratch ACP agent, built from `acp-go-sdk2`'s example agent, is set as the scratch
     daemon's `providers.grok.bin`.
   * Prompted through `scripts/smoke-protocol`, it asks Magic's terminal (`terminal/create`) to
     run `mcremote update --yes`.

   On the library's backends, the expected outcome is the fail-safe message above. On the script
   backends, F2's outcome is recorded as it happens: stopped and left down, or failed safe.
3. ~~Also measured on Windows: whether grok's `bash` reaches Magic's `terminal/create`.~~ *Moved
   2026-10-23 to the amendment that adopts go-selfupdate-lib#6.* It bears only on the Windows
   handoff, which waits for #6 (D3).

After the change:

1. **On systemd and launchd**, an agent-run or inside-the-scope `mcremote update --yes`:
   * prints `update handed off`, and exits 0;
   * the daemon restarts on the new version, with a new PID;
   * `doctor` reports the handoff with exit code 0.

   The running binary is a local build of this change. The "new" one is the latest published
   release, installed with `--force --version <tag>` ("Force replaces a local build", the
   library's `cli.Flags`), so no tag is needed.
2. **On Windows**, the update command sets no handoff, and an inside update still fails safe with
   `ErrInsideService`, as before. A unit test pins that `HandOff` is unset on Windows.
3. **On the script backends**, the inside update fails with `ErrInsideService`, and the service
   stays up.
4. Unit tests:
   * `HandOff` is set from the backend, and absent for a non-`Detacher`;
   * `scriptBackend.Stop` refuses inside, against a faked ancestry;
   * `doctor`'s report is right with no result, a success and a failure.

   Each is seen failing first.

## Pros and Cons of the Options

### A — The library's handoff, breakaway on Windows, script backends fail safe

* Good, because it covers every backend the library has, and makes the rest safe.
* Good, because D3 matches `agentproc`'s own choice for ACP agents. One rule, not two.
* Neutral, because D3 is one flag, and the Confirmation tests both its sides.
* Bad, because D3 loosens 0116 D8 for any process that asks to break away.

### B — No handoff on Windows

* Good, because 0116 D8 stays as it is.
* Bad, because Windows users cannot update through an agent, which is the gap this record exists
  to close.
* Bad, because F3's edge stays fatal: an exited intermediate parent still lets `Stop` drain the
  daemon while the update dies in Magic's job.

### C — A Task Scheduler-native detach from the library

* Good, because the run would not descend from the daemon at all, with no breakaway, as with
  `systemd-run` and launchd's one-shot job.
* Bad, because it does not exist. It needs a library release, and Magic would wait for it.
* Bad, because registering a task per update leaves definitions to clean up, as launchd's one-shot
  jobs need `CleanupHandOffs`.
* Neutral, because it can follow A later. Magic's wiring would not change, only the library's
  `Detach`.

### D — `ProcessDetacher` on runit, s6 and OpenRC

* Good, because those users could update through an agent too.
* Bad, because the library warns that `DetachProcess` "is no defence against a cgroup kill". An
  OpenRC system may run user services in cgroups, so a detach that is safe under runit may not be
  under OpenRC.
* Bad, because it is Magic writing a backend's handoff, which upstream issue #2 asks the library
  to own.

### E — Change nothing

* Good, because nothing changes.
* Bad, because F2 leaves script-backed services stopped.
* Bad, because the phone's user still cannot update through an agent, which 0054 named as the
  trigger to adopt this seam.

## More Information

### What this record changes in others

* **0054** "What stays deliberately unadopted": the handoff item is superseded by this record,
  as 0207 D3 said it would be. The 0054 MADR carries a dated note at that item, and at its
  original summary, since 2026-10-23 (the PLAN's L1 Deviation 1).
* **0207** D3 is decided in full here, and F1 is corrected (F1 above, with 0207-REPORT A1). Its
  amendment note on D3, "agentproc's job allows breakaway", is corrected by F3: grok's shell
  commands run in Magic's terminal job. 0207 gets a dated note pointing here.
* **0116** D8 is unchanged (D3, as decided 2026-10-23).

### Questions for the owner

1. **Windows breakaway (D3).** Accept `BREAKAWAY_OK` on Magic's job, the recommendation? Or keep
   the handoff off Windows (B), or wait for a library detach (C)?

   *Answered 2026-10-23: "Open as an issue in the library repo, then wait on it."* That is
   option C. It was filed as
   [go-selfupdate-lib#6](https://github.com/maccavelli/go-selfupdate-lib/issues/6),
   "service/taskscheduler: detach a handoff as a one-shot task, without job breakaway". D3 is
   rewritten to wait on it.
2. **Script backends (D4).** Fail safe only, the recommendation? Or also hand off there (D)?

   *Answered 2026-10-23: "Guard now, handoff via #2".* D4's inside guard lands now. The
   handoff on runit, s6 and OpenRC waits for
   [go-selfupdate-lib#2](https://github.com/maccavelli/go-selfupdate-lib/issues/2)'s backends,
   whose proposal already includes a `Detacher`. Magic writes none (option D declined).
   * The supervisors' stop semantics were added to #2 as a comment, to inform its inside check
     and its `Detacher`. runit's `sv down` and s6's `s6-svc -d` signal only the supervised
     process. OpenRC's `rc_cgroup_cleanup` is off by default.
   * The comment proposes descent from the supervised process as the inside check, the same check
     D4 implements.
3. **The Linux reproduction.** Move a shell into the unit's cgroup (no quota, the recommendation),
   or run a real grok session (quota)?

   *Answered 2026-10-23: "Add the shell to the group."* The Linux runs put a shell's PID into
   the unit's `cgroup.procs`. No agent and no provider quota are needed.

   *Amended 2026-10-23: "Use the probe agent on Linux too".* The move was refused, because an
   SSH shell starts outside the user manager's delegated subtree (the PLAN's R1 Deviation 2). Linux
   now reaches "inside" through the scratch daemon's probe agent, as the other hosts do, still
   with no provider quota.
4. **Where the work lives.** A PLAN for this record, `0210-PLAN-…` (the recommendation). It would
   have phases for reproduction, D1+D2+D5+D6, D3, D4 and the live confirmation.

   *Answered 2026-10-23:* write the PLAN.
   [0210-PLAN-hand-off-agent-run-updates-outside-the-service.md](0210-PLAN-hand-off-agent-run-updates-outside-the-service.md)
   is that plan. D3 has no implementation phase now; its one change waits on #6.

### Evidence index

* Library, `v1.17.2`: `go doc -all` of `selfupdate/cli` (`Options.HandOff`, `HandOff`),
  `selfupdate/service` (the handoff section, `HandOffFunc`, `ReportFunc`, `LoadHandOffEnv`,
  `HandOffHop`, `DetachProcess`, `ProcessDetacher`, `DefaultResultPath`), `service/systemd`,
  `service/launchd` and `service/taskscheduler` (`Inside`, `Detach`, `CleanupHandOffs`).
  `docs/decisions/0021-MADR-task-scheduler-lifecycle-and-windowless-children.md`, Amendment A1,
  probe 2.
* `acp-go-sdk2@v1.20.0/agentproc/tree_windows.go:73`.
* grok-build `2bdd1d6a`: `xai-grok-shell/src/session/acp_session_impl/spawn.rs:758-764`,
  `:3012-3027`, `:3176-3185`; `xai-grok-shell/src/agent/mvp_agent/agent_ops.rs:4113-4120`.
* Microsoft Learn, read 2026-10-22: Nested Jobs; Job Objects; Process Creation Flags
  (`CREATE_BREAKAWAY_FROM_JOB`); `JOBOBJECT_BASIC_LIMIT_INFORMATION`.
* This repository at `b946cf2d`: `internal/updateclient/client.go:250`;
  `internal/cli/service/backend.go:39-95`, `scriptbackend.go:80-104`,
  `mcremote.user.service.tmpl:44`; `internal/procutil/procutil_windows.go:243-300`; the
  `SuperviseStarted` callers listed in F3; `internal/provider/acpagent/acpagent.go:1100-1102`;
  `cmd/mcremote/main.go`, `cmd/mcrelay/main.go`.

## Amendment — 2026-10-23 (A): three findings from L1

~~**Status of this amendment: proposed.**~~ **Decided by the owner on 2026-10-23:** "5C: both",
"6A: every backend", "7A: refuse early in Magic" and "Skip R2" (the questions below). The
decisions above stay accepted. This amendment adds
D8 to D10, from what the PLAN's L1 observed. The owner asked to "address the 3 discovered items"
on 2026-10-23. Read against `907c3c29` and go-selfupdate-lib `v1.17.2`.

### What L1 found

* **F5 — The service manager's notify socket reaches the detached run.**
  * **Seen.** On the Linux test host, the handoff's journal has
    `mcremote-d3probe-selfupdate-1467c607….service: Got notification message from PID 1866372,
    but reception is disabled`, at 15:05:44.600.
  * **The window.** The detached run was PID 1866346. The notifier was another process in the
    transient unit, between the old daemon's stop (44.160) and the new one's start (44.629).
    That is the reconcile window. The library's `ExecReconciler` runs the new binary as
    `setup-service --refresh --json` (`selfupdate/service/execreconciler.go:115-116`), and that
    refresh runs `systemctl --user daemon-reload` (`internal/cli/service/refresh.go:248`).
  * **Who sent it is not known.** Magic sends sd_notify only from `daemon.Run`
    (`internal/daemon/daemon.go:690`, `:709`, `:735`, `:751`), through the library's
    `systemd.Notify`, which reads `NOTIFY_SOCKET` on each call (`service/systemd/notify.go:22`).
    So the notifier was one of the two children, most likely `systemctl`. The journal has no entry
    under that PID.
  * **How the socket gets there.** The daemon's unit is `Type=notify`
    (`mcremote.user.service.tmpl:23`), so systemd gives the daemon `NOTIFY_SOCKET`. Magic's
    children inherit the daemon's environment: `acpagent/terminal.go:81`, `codex/provider.go:569`,
    `httpagent/provider.go:534`, and the SDK's agent processes. An agent's `mcremote update`
    therefore has it. Both detaching backends write the caller's whole environment into the
    handoff's environment file: `service/systemd/detach.go:209` and `service/launchd/detach.go:100`
    (`os.Environ()`).
  * **Effect today.** One discarded message and a journal line. In the daemon's own unit, a child's
    message is dropped, since `NotifyAccess=` defaults to `main` for `Type=notify`
    (systemd.service(5)) and the template sets none. In a transient unit, reception is off.
  * **Why it matters still.** The variable names one unit's socket, and it travels into processes
    that belong to another unit, and into every command an agent runs. Any process there that
    speaks sd_notify talks to a unit that is not its own.
* **F6 — Removing a service leaves its definition's backup.**
  * **Seen.** On Linux, `~/.config/systemd/user/mcremote-d3probe.service.prev` remained after
    `setup-service --remove`, which exited 0. It was mode 0600.
  * **Where it comes from.** A refresh that rewrites a definition first keeps the old one, and
    prints `service definition refreshed: <path> (previous kept at <backup>)`
    (`result_print.go:26`). The backups are:
    * systemd: `<unit>.service.prev` (`refresh.go:239`);
    * launchd: `<label>.plist.prev` (`refresh.go:306`);
    * runit and s6: `run.prev` inside the service directory (`supervisor.go:61`);
    * OpenRC: `<name>.prev` beside `init.d` (`supervisor.go:69`);
    * Windows: `<name>-task.prev.xml` in the state directory (`refresh_schtasks.go:164-184`).
  * **What remove deletes.**
    * `removeSystemd` deletes the unit and its enable link (`setup.go:773-779`).
    * `removeLaunchdAgent` deletes the plist (`setup.go:823`).
    * `removeScript` deletes the runit or s6 service directory, which takes `run.prev` with it
      (`supervisor.go:486`), or the OpenRC script alone (`supervisor.go:496`).
    * `removeSchtasks` deletes the task (`setup_schtasks.go:91`).
    * `--uninstall` calls the same four (`uninstall.go:269-275`).

    So the backup outlives the service on systemd, launchd, OpenRC and Windows.
  * **Why it matters.** A backup is mode 0600 when its definition carries extra environment
    (`refresh.go:325-329`), and that is where `--env` values live, such as a relay secret. An
    uninstall that leaves it leaves the secret. A later `setup-service` renders fresh and never
    reads it. The update's own rollback reads the backup only during the update, through the
    reconcile receipt (`execreconciler.go:147-173`).
  * This predates 0210. It is decided here because L1 found it (the same-number rule in
    `AGENTS.md`).
* **F7 — Where nothing hands off, an inside update downloads before it refuses.**
  * **Seen.** In each of L1's runit, s6 and OpenRC transcripts, `downloading-manifest` and
    `installing … target=v0.22.2` come before `this process runs inside the service it would
    stop`.
  * **The cause.** The refusal comes from `Stop`, inside the managed install, after the download
    and staging.
  * **Windows.** It takes the same path, since `HandOff` is unset there (D3). That is read from
    code; L1's Windows transcript was filtered to the result line.
  * **What the library offers.** It runs `HandOff.Detach` before `u.RunWith`, after only a
    release check (`selfupdate/cli/run.go:140-149`). A `Detach` error fails the run, and nothing
    is downloaded.
  * Both backends that refuse already answer `Inside`: `scriptBackend.Inside` (H2), and
    `taskscheduler.Task.Inside` (`service/taskscheduler/detach.go:15`).

### Considered options

* **For F5:**
  * **5A** — The daemon takes `NOTIFY_SOCKET` out of its own environment when it starts, keeps
    the value, and sends its notifications to the kept path. No child inherits it, on any spawn
    path.
  * **5B** — A go-selfupdate-lib issue: `Detach`'s environment file leaves out the service
    manager's per-unit variables (`NOTIFY_SOCKET`, `WATCHDOG_PID`, `WATCHDOG_USEC`, `LISTEN_PID`,
    `LISTEN_FDS`, `LISTEN_FDNAMES`, `MAINPID`, `MANAGERPID`, `INVOCATION_ID`, `JOURNAL_STREAM`,
    `SYSTEMD_EXEC_PID`). Magic waits for the release.
  * **5C** — Both.
  * **5D** — Record it, and change nothing.
* **For F6:**
  * **6A** — Each remover deletes the backup of the definition it removes, on every backend, and
    its result names it.
  * **6B** — As 6A, except Windows.
  * **6C** — Keep the backups, and have remove print where each was left.
  * **6D** — Change nothing.
* **For F7:**
  * **7A** — Where the update sets no handoff and the backend answers `Inside`, set
    `HandOff.Detach` to a check that refuses with `ErrInsideService` before the download.
  * **7B** — A library issue for such a check in `service`, and wait.
  * **7C** — Change nothing.

### Proposed decisions

* **D8 — An update that cannot hand off refuses before it downloads (7A).**
  * In `updateclient`, when `newCommandDetacher` reports no `Detacher` but the backend has
    `Inside(ctx) (bool, error)`, `o.HandOff.Detach` is a refusal. Inside, it returns
    `fmt.Errorf("…: %w", libservice.ErrInsideService)`; outside, `false, "", nil`; an `Inside`
    error is returned, never read as "outside".
  * This covers Windows (D3) and runit, s6 and OpenRC (D4). `Report` stays unset there, since no
    run is detached.
  * `Stop`'s guard stays, as the last line of defence.
  * The message names the remedy, updating from a shell.
* **D9 — Removing a service removes its definition's backup (6A).**
  * `removeSystemd`, `removeLaunchdAgent`, `removeScript` (OpenRC) and `removeSchtasks` each
    delete the backup the matching refresh writes. A missing backup is not an error. The result
    gains the path, and `--remove`'s and `--uninstall`'s output lists it.
  * Windows looks the path up without creating the state directory, which `taskBackupPath` does
    today (`refresh_schtasks.go:180`).
  * *Recommended because* a backup can hold `--env` secrets, and nothing reads it once its service
    is gone.
* **D10 — The daemon keeps its notify socket to itself (5C).**
  * In `daemon.Run`, before anything is spawned, Magic reads `NOTIFY_SOCKET`, unsets it, and
    sends `READY=1` and `STOPPING=1` to the kept address. That is `sd_notify`'s own
    `unset_environment` practice (sd_notify(3)).
  * The send is the library's datagram protocol, a few lines in a new `daemon_notify.go`:
    a path, or an abstract `@` name, and `SOCK_DGRAM`. `notifySystemd` keeps its signature, and
    is passed that sender.
  * A go-selfupdate-lib issue asks `Detach` to leave the per-unit variables out (5B), so that every
    product is safe. Filing it is outward-facing and needs the owner's word.
  * *Recommended because* 5A closes the leak at its source for every child Magic spawns, including
    the SDK's, and 5B closes it for other products and other variables.

### Confirmation of this amendment

* **Unit tests, each seen failing first:**
  * D8: an inside update on a backend with `Inside` and no handoff fails with `ErrInsideService`,
    and the fake updater records no run. Outside, it runs as today, and `--check` and `--dry-run`
    never call it.
  * D9: per remover, a definition with a backup leaves neither behind, and the result names the
    backup. A missing backup is no error. On Windows, no state directory is created.
  * D10: after start-up, the daemon's environment has no `NOTIFY_SOCKET`, `READY=1` still reaches
    the test's socket, and a child spawned after start-up sees no `NOTIFY_SOCKET`. The existing
    `TestRunNotifiesSystemd` keeps passing.
* **Live** (the PLAN's L2, on the owner's word):
  * Linux, systemd: L1.1's handoff completes, and the transient unit's journal has no
    `reception is disabled` line.
  * The same run's `--remove` leaves no `.service.prev`.
  * In the three containers, the inside update is refused with no `downloading` event.
  * On Windows, by the real-task procedure (R1 Deviation 3), the same.

### Questions for the owner (amendment A)

1. **F5.** Approve 5C, with the issue filed in go-selfupdate-lib (recommended)? Or 5A alone, 5B
   alone, or 5D?
2. **F6.** Approve 6A, on every backend, Windows included (recommended)? Or 6B, 6C or 6D?
3. **F7.** Approve 7A (recommended)? Or 7B or 7C?
4. **Who sent the message.** Should the PLAN identify it on the Linux test host (its R2), so the
   issue can name it? That run points `NOTIFY_SOCKET` at a listener and runs `systemctl --user
   daemon-reload` and the refresh against the test host's user manager. The fix does not depend
   on the answer.

*Answered 2026-10-23:*

1. "5C: both". D10 stands as written. The issue is filed in A0.
2. "6A: every backend". D9 stands as written, Windows included.
3. "7A: refuse early in Magic". D8 stands as written.
4. "Skip R2". The sender stays "one of the two children, most likely `systemctl`". The issue says
   so.

## Amendment — 2026-10-23 (B): the Windows handoff, now that go-selfupdate-lib#6 has shipped

**Status of this amendment: proposed.** The owner chose its place and order on 2026-10-23, in
answer to 0207's amendment B question 8: "0210 amend B, before L2 (Recommended)". It becomes
decided when the owner approves the PLAN's amendment B.

### What changed

* **#6 shipped in `go-selfupdate-lib v1.19.0`** (2026-10-23; its CHANGELOG; migration guide §19).
  `taskscheduler.Task.Detach` now registers and runs a one-shot task, `<Name>.selfupdate.<id>`,
  which Task Scheduler starts outside the program's jobs. `Task.CleanupHandOffs` removes finished
  ones. `Options.HopDetach` keeps the old hop.
* **D3 waited on exactly this** ("Windows joins when the library ships a one-shot-task detach").
  0207's amendment B D15 pins `v1.19.0`, and this amendment follows it.
* **What else `v1.19.0` changes for a handoff:**
  * Only a task with an `InteractiveToken` principal hands off by default; another logon type
    gets an error naming `HopDetach`. Magic's task registers with `InteractiveToken`
    (`internal/cli/service/schtasks.go`, `renderTaskXML`; 0116 D12).
  * The one-shot run's first argument names its environment file, and
    `service.LoadHandOffEnvContext` applies and removes it. Both `main`s call it first already.
  * `WriteHandOffEnv` gives its directory a protected access list on Windows: the user, SYSTEM
    and Administrators.
  * A one-shot task shows in Task Scheduler while it runs and until the next cleanup, and a
    console window shows for about 50 ms while its hop starts the update.

### Proposed decisions

* **D11 — The Windows handoff is on (D3, option C, carried out).**
  * `detacherFor` no longer refuses Windows: a backend that is a `Detacher` hands off on every
    OS. 0116 D8's kill-on-close jobs stay as they are, since the one-shot task is outside them.
  * The daemon's start-up `CleanupHandOffs` runs on Windows as well as macOS
    (`internal/daemon/daemon_handoff.go`). `taskscheduler.Task.CleanupHandOffs(ctx) error` fits
    its interface unchanged.
  * D5's reporting is unchanged: `doctor` reads the result file the library writes.
  * **D8 stops applying on Windows by itself.** `insiderFor` returns false for a `Detacher`, so
    Windows gets the handoff, not the refusal. D8 stays for runit, s6 and OpenRC.
  * The `update` command's help, `ops-windows-install.md`'s "Updating from a phone session", and
    the README's sentence say Windows hands off.
* **L2's Windows step changes.** It expects a handoff. A handoff really stops, replaces and
  restarts the task, and a detached run replaces the binary that started it. So the Windows
  procedure is rewritten before L2.3 runs, and approved by the owner (the PLAN's B2).

### Confirmation of this amendment

* **Unit tests, each seen failing first:** `detacherFor("windows", <a Detacher>)` is true, and
  `insiderFor` false for it; the daemon's cleanup runs on `windows` and not on `linux`.
* **Live** (L2, on the owner's word): on the Windows test host, an agent-run `update --yes` prints
  `update handed off: …`; the task restarts on the target release; `doctor` shows the handoff's
  result; the one-shot task is gone after the daemon's next start.

### Consequences of this amendment

* Good, because an agent-run update now works on Windows, as on systemd and launchd.
* Good, because Magic's containment is unchanged: no breakaway was needed.
* Neutral, because D8's early refusal now covers only the script supervisors.
* Bad, because the Windows live check really updates the host's task, which needs a procedure of
  its own (B2).

