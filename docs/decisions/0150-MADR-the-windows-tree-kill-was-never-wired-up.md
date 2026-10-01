---
status: proposed
date: 2026-09-07
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The Windows tree-kill guarantee was designed, tested, documented, and never wired up

## Context and Problem Statement

On Unix a provider's descendants die because the daemon signals a process
*group*: `syscall.Kill(-p.Pid, SIGKILL)` reaches every process in it, and
`Pdeathsig` kills the engine if the daemon dies un-gracefully. Windows has
neither. MADR 0116 D8 chose the Job Object as the replacement, and
`procutil.SuperviseStarted` implements it correctly:
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, with a test that proves a grandchild dies
when the job handle closes.

**Nothing calls it.** Every reference to it outside its own file and test is a
doc comment describing a caller that does not exist. On Windows the daemon
therefore has no tree-kill at all: teardown terminates the direct child and
leaves its descendants running.

This is worse than an unused function, because two other records rest on it.
0116 D8 describes an implementation that did not ship, and 0116 D9 lists the
Job Object as the first of three reasons why terminating the scheduled task
without a graceful drain is survivable. That reason is not true today.

Found during a Windows debugging pass over `mcrelay` and `mcremote`
(2026-09-07); the same pass is the source of three unrelated findings that are
deliberately not in this record.

### What was measured, not assumed

All on `513d0c4`, Windows 11, Go 1.26.6.

**`SuperviseStarted` has no production caller.** Repo-wide, including tests:

```text
procutil/owner_windows.go:17          // ... see [SuperviseStarted], which kills ...
procutil/procutil_windows.go:25       // ... which the caller invokes right after Start.
procutil/procutil_windows.go:93       // SuperviseStarted attaches an already-started ...
procutil/procutil_windows.go:101      func SuperviseStarted(...)          <- the definition
procutil/procutil_windows_test.go:58  release, err := SuperviseStarted(...)

production call sites: 0        (procutil.SetProcessGroup, by contrast: 12)
```

**It cannot be called from shared code, because it does not exist there.** Its
doc comment says "It is a no-op on Unix (MADR 0116 D8)", but no such
declaration exists — `procutil_unix.go` and `procutil_other.go` do not declare
it. A cross-platform caller fails to compile:

```console
$ GOOS=windows go build ./internal/procutil/   # (with a probe caller added)
builds OK
$ GOOS=linux go build ./internal/procutil/
internal/procutil/zz_probe.go:7:18: undefined: SuperviseStarted
$ GOOS=darwin go build ./internal/procutil/
internal/procutil/zz_probe.go:7:18: undefined: SuperviseStarted
```

**What Windows teardown does instead.** `KillProcessGroup` carries the Unix
name but not the Unix reach:

```go
// unix
syscall.Kill(-p.Pid, syscall.SIGKILL)   // the whole process group

// windows
windows.TerminateProcess(h, 1)          // one process
```

`TerminateProcessGroup` sends `CTRL_BREAK_EVENT` to the group, waits, and then
escalates to that single-process terminate.

**The graceful phase is unlikely to fire in deployment.**
`GenerateConsoleCtrlEvent` requires the caller to have a console. 0116 D12 runs
the daemon as a per-user Task Scheduler entry, which has none, so that call
fails and `TerminateProcessGroup` escalates immediately — to the terminate that
reaches one process. **[Inferred from the API contract and D12, not observed on
a running scheduled task.]**

**0116 D8 describes something that was not built.** Its text:

> `SetProcessGroup` creates a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`
> and assigns the child; `TerminateProcessGroup` closes the job (killing the
> whole tree) …; `KillProcessGroup` terminates the job.

The shipped `SetProcessGroup` says the opposite, and explains why:

> This does NOT create the job object: os/exec exposes no hook between
> CreateProcess and the caller regaining control, so the tree-kill guarantee is
> [SuperviseStarted]'s.
> — `procutil_windows.go:23`

The deviation is correct — `AssignProcessToJobObject` needs a process that
exists — but the compensating step was never taken.

**The spawn sites, and which kind they are.** Twelve `SetProcessGroup` call
sites, split by how the command is run:

| Kind | Sites |
| --- | --- |
| Long-lived (`cmd.Start`) | `acpagent/acpagent.go:426`, `acpagent/terminal.go:90`, `acphttp/provider.go:327`, `codex/provider.go:523`, `httpagent/provider.go:494`, `providerauth/cli.go:85` |
| Short-lived (`cmd.Run`) | `codex/adapter.go:184`, `codex/auth.go:49`, `codex/auth.go:73`, `codex/logout.go:147`, `codex/logout.go:221`, `codex/store_reality.go:139` |

**`release()` kills the tree.** It is not a cleanup defer. The existing test is
explicit — `release() // closing the job kills the tree` — because
`KILL_ON_JOB_CLOSE` fires when the last handle closes.

### Findings

**F1 — the tree-kill guarantee is absent on Windows.** `SuperviseStarted` has
zero production callers. Nothing creates a job object, so no provider's
descendants are bound to anything.

**F2 — no cross-platform caller can compile.** `SuperviseStarted` is declared
only in `procutil_windows.go`. Measured: `GOOS=linux` and `GOOS=darwin` builds
fail with `undefined: SuperviseStarted`. This is the mechanical reason the
wiring was never done, and it means the fix begins with a no-op declaration
rather than a call.

**F3 — the function's own documentation asserts an API that does not exist.**
"It is a no-op on Unix" describes the declaration F2 shows is missing. The
comment is not merely stale; it is the reason a reader would believe the wiring
was possible.

**F4 — Windows teardown reaches one process.** `KillProcessGroup` is
`TerminateProcess` on a single handle against Unix's `Kill(-pid)` on a group,
and `TerminateProcessGroup` escalates to it. A provider engine's own children —
`node`, `python`, `git` spawned by an agent CLI — survive.

**F5 — the graceful phase probably never runs in deployment.**
`GenerateConsoleCtrlEvent` needs a console; the scheduled task (0116 D12) has
none. **[Inferred, not observed.]**

**F6 — 0116 D8's description does not match what shipped**, and the divergence
is deliberate and correct on its own terms (`os/exec` has no hook). What is
missing is the step that was supposed to replace it.

**F7 — 0116 D9's survivability argument rests on F1.** Its first bullet —
"orphaned provider processes are already prevented by D8's Job Object … the
tree dies with the parent handle" — is false today. The other two bullets
(stale-socket handling, phone reconnect) are unaffected.

**F8 — `release` is a teardown handle, not a defer.** Closing the job kills the
tree, so `defer release()` at a spawn site would kill the engine the moment the
enclosing function returns.

**F9 — the long-lived sites are where the exposure is.** Six sites start a
process that outlives the call; six run a CLI to completion. The `cmd.Run` sites
bound their own window by waiting.

## Decision Drivers

* **A guarantee that exists only in comments is worse than one that is absent**,
  because the comments are load-bearing: D9 reasons from it.
* **The exposure is the interesting one.** Provider CLIs spawn language runtimes
  and toolchains; those grandchildren hold ports, file handles and engine state.
* **Windows is the active development host.** Whatever this costs in review, it
  is being paid daily by the machine most likely to hit it.
* **The mechanism already exists and is proven.** This is a wiring problem, not
  a design problem — the risky part of the work was done in `ef52386`.

## Considered Options

* **A — Declare a cross-platform no-op, then wire the long-lived sites.** (chosen)
* **B — Build D8 as originally written: `SetProcessGroup` creates the job.**
* **C — One daemon-wide job object that every spawned process joins.**
* **D — Leave it; rely on `CTRL_BREAK_EVENT` and per-process terminate.**

## Decision Outcome

Chosen: **Option A.** It restores the guarantee with the mechanism that already
exists and is tested, and it is the only option that does not either fight
`os/exec` (B) or give up per-provider teardown (C).

### The decisions

**D1 — declare `SuperviseStarted` on every platform.** A no-op returning
`func() {}, nil` for `unix` and `!unix && !windows`, matching the shape its doc
comment already promises. Nothing can be wired until this exists (F2, F3).

**D2 — wire the six long-lived spawn sites.** Call it immediately after
`cmd.Start()` and keep `release` for the lifetime of the process, on the struct
that already owns the engine handle.

**D3 — `release` is invoked from the existing teardown path, never deferred at
the spawn site.** This is the one way to get D2 wrong that still compiles and
looks right (F8). Where a site already calls `TerminateProcessGroup` or
`KillProcessGroup`, `release` belongs beside that call.

**D4 — do not move job creation into `SetProcessGroup`.** `os/exec` exposes no
hook between `CreateProcess` and the caller regaining control, which is why the
shipped code deviates from D8 and why that deviation stands (F6).

**D5 — leave the six `cmd.Run` sites alone for now.** They wait for the child,
so the unsupervised window is bounded by the call rather than by the daemon's
lifetime. Supervising them means restructuring `Run` into `Start`/`Wait`, which
is a larger change with a smaller payoff. Named in the plan as deferred, not
overlooked.

**D6 — amend MADR 0116.** D8's description must say what shipped, and D9's
first survivability bullet must either become true (after this lands) or be
withdrawn. Leaving a record asserting a guarantee the code does not provide is
the failure this record is about, repeated one level up.

**D7 — prove the wiring, not just the mechanism.**
`TestSuperviseStartedKillsTree` already proves the job object works. What is
missing is evidence that a provider's engine is actually supervised, which is
what regressed silently.

### Consequences

* Good: a killed daemon takes provider trees with it, which is the property
  0116 D9 already claims and currently does not have.
* Good: the fix is small and the risky part — the job object itself — is
  already written and tested.
* Neutral: `release` becomes state on six structs. Each already holds a process
  handle, so this adds a field beside one that exists.
* Bad: D3's trap is real and quiet. A `defer release()` compiles, passes a unit
  test that only checks startup, and kills the engine in production the instant
  the starting function returns.
* Bad: this changes teardown behaviour on the platform with the least coverage.
  A provider that today leaks a grandchild will, after this, have it killed —
  which is the point, but it is a behaviour change under a process that has
  never exercised it.
* Bad: the `cmd.Run` sites stay unsupervised (D5), so the guarantee is
  "engines and long-lived helpers", not "everything this daemon spawns".

### Confirmation

```bash
# 1. The API exists everywhere, so a caller compiles on every target.
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...

# 2. Every long-lived spawn site supervises. Six sites, six calls:
grep -rn 'SuperviseStarted' --include='*.go' internal/ | grep -v _test | grep -v procutil/

# 3. No site defers it at the spawn point (D3):
grep -rn 'defer release\|defer .*SuperviseStarted' --include='*.go' internal/

# 4. The mechanism still holds, and the wiring is now covered:
go test ./internal/procutil/ -run TestSupervise -count=1 -v
go test ./... && go test -race ./...
```

## Pros and Cons of the Options

### A — Cross-platform no-op, then wire the long-lived sites (chosen)

* Good, because it uses the mechanism that already exists and is proven by test.
* Good, because per-process jobs give both properties at once: individual
  teardown kills that provider's tree, and a killed daemon closes every job
  handle it holds, so all trees die — which is what D9 needs.
* Good, because the no-op declaration makes the API honest about what its own
  comment already claims.
* Neutral, because six structs gain a field.
* Bad, because D3's trap survives review easily.

### B — Build D8 as written: `SetProcessGroup` creates the job

* Good, because it needs no second call and no lifetime bookkeeping, so D3's
  trap disappears entirely.
* **The strongest argument for it:** a guarantee that depends on every future
  spawn site remembering a second call is a guarantee that will regress again —
  which is precisely what this record is about. Folding it into the one function
  everybody already calls is structurally safer than adding a rule.
* Bad, because it cannot be done as described. `AssignProcessToJobObject`
  requires a started process; `SetProcessGroup` runs before `cmd.Start()`.
  `os/exec` has no hook in between — the shipped code says so at
  `procutil_windows.go:23`.
* Bad, because the nearest workable variant (`SysProcAttr` tricks, or wrapping
  every spawn in a helper that starts and assigns) either reaches into
  platform-specific internals or replaces every call site anyway.

### C — One daemon-wide job that every spawned process joins

* Good, because it is a single handle with no per-site bookkeeping, and it
  delivers D9's property directly: the daemon dies, the job closes, everything
  dies.
* Good, because a new spawn site joining the daemon job is a smaller thing to
  remember than a new site managing a release handle.
* Bad, because it gives up per-provider teardown: closing the shared job kills
  every engine, so individual `Close`/`Purge` still needs the per-process path,
  and the daemon ends up with both mechanisms rather than one.
* Bad, because a job a process is already assigned to constrains nesting rules;
  provider CLIs that create their own jobs would need Windows 8+ nested-job
  behaviour, which is another platform assumption to carry.

### D — Leave it; rely on `CTRL_BREAK_EVENT` and per-process terminate

* Good, because it is free, and no user has reported an orphaned process.
* Bad, because "no report" is expected: an orphaned `node` holding a port
  surfaces as a *later* failure to start, not as a visible leak.
* Bad, because it leaves 0116 D9 resting on a false premise, and the honest
  version of this option is to amend D9 to say the daemon leaks trees when the
  scheduled task ends — which is a worse thing to write down than to fix.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Zero production callers | `grep -rn SuperviseStarted --include='*.go' .` |
| 12 `SetProcessGroup` sites | same grep for `procutil.SetProcessGroup` |
| Undefined on linux/darwin | `GOOS=linux/darwin go build` with a probe caller |
| Unix kills the group; Windows one process | `procutil_unix.go:29` vs `procutil_windows.go:46` |
| Escalation path | `procutil_windows.go:58-91` (`TerminateProcessGroup`) |
| `SetProcessGroup` does not create the job, and why | `procutil_windows.go:16-31` |
| D8 says it does | `docs/0116-MADR-…md:809-815` |
| D9 rests on the job object | `docs/0116-MADR-…md:834-836` |
| Daemon runs as a scheduled task | MADR 0116 D12 |
| `release()` kills the tree | `procutil_windows_test.go:70` |
| Spawn-site classification | per-site inspection of the 30 lines after each `SetProcessGroup` |

### Related records

* **MADR 0116** — D8 (the Job Object decision this record completes), D9 (whose
  survivability argument depends on it), D12 (the scheduled task that makes the
  graceful phase unreachable).
* **MADR 0137** — F4 is the inline/async split that shapes which teardown paths
  exist at all.
* **MADR 0147/0149** — the recent Windows-correctness work whose debugging pass
  surfaced this. Their pattern applies here too: the defect is silent, and the
  documentation asserts the guarantee that is missing.

### Open questions for the plan

1. **Where does `release` live at each of the six sites?** Each already owns a
   process handle somewhere; the plan must name the field and the teardown
   function per site rather than leaving it to the implementer.
2. **Is `providerauth/cli.go:85` long-lived enough to need it?** It starts an
   interactive auth CLI. If that process is waited on within the same call, it
   belongs with the `cmd.Run` group instead. **[unverified]**
3. **What does the wiring test look like?** D7 asks for evidence that a provider
   is supervised, not that job objects work. A per-provider test needs a real
   engine binary; a cheaper proxy may be a unit assertion that the teardown path
   invokes the release it stored.
4. **Does F5 hold in practice?** Whether `GenerateConsoleCtrlEvent` actually
   fails under the scheduled task is inferred. It changes nothing about the fix,
   but it does change how the graceful phase should be described.

## Amendment — 2026-09-07: CTRL_BREAK already reached part of the tree

Executing P4 produced a measurement that narrows F1. It does not withdraw it,
and it does not change any decision above, but leaving it out would let this
record keep a claim it can no longer make in full.

**F10 — on Windows, `TerminateProcessGroup` already killed in-group
descendants.** Measured on this host, Windows 11, Go 1.26.6, by sabotaging the
P4 test's subject and watching the result:

| Spawn stores | Grandchild's process group | Grandchild after teardown |
| --- | --- | --- |
| `release` from `SuperviseStarted` | engine's | dead |
| `func() {}` (job never closed) | engine's | **dead** |
| `func() {}` (job never closed) | its own (`CREATE_NEW_PROCESS_GROUP`) | **alive after 30s** |

The second row is the finding. With supervision present but inert — the job
handle held open, so nothing the job does can be credited — a grandchild in the
engine's console process group still died. `GenerateConsoleCtrlEvent` delivers
`CTRL_BREAK_EVENT` to every process in the target group, and a child spawned
without `CREATE_NEW_PROCESS_GROUP` inherits its parent's, so the graceful phase
was already reaching one process deeper than "the direct child".

**What F1 still says, precisely.** The tree-kill guarantee was absent, and the
job object remains the only thing that supplies it, for the descendants the
console control event cannot reach:

* one in another process group — the `node`/`python`/`git`-under-an-agent-CLI
  case 0116 D8 was written for, and the third row above;
* one that ignores `CTRL_BREAK_EVENT`, which a process may simply do;
* every descendant on the escalation path, where the graceful phase fails or
  times out and `TerminateProcess` takes the direct child alone. **[reasoned
  from the API contract, not measured]**

So the pre-0150 exposure was narrower than "no tree-kill at all" and wider than
nothing: descendants that behaved like well-mannered console children were
already being killed, and precisely the ones the job object was chosen for were
not. The correction is worth having on the record because it is the difference
between a guarantee and a coincidence — the old behaviour depended on a
property of the child, and the new behaviour does not.

**D8 — the wiring test asserts against a descendant the console cannot
reach.** Its helper puts the grandchild in its own process group on Windows, so
the assertion fails when supervision is removed; off Windows it deliberately
leaves the grandchild in the engine's group, because a `setpgid` escape there
is the residual gap the no-op release cannot close and never claimed to. The
test therefore proves the wiring on every platform (the release is stored at
spawn, and teardown invokes it) and proves the tree-kill only on Windows, which
is the only place it exists.

The evidence for D7 is the same table: sabotaging the spawn to store no release
fails the test on the stored-release assertion, and sabotaging it to store an
inert one fails the test on the surviving grandchild. Without the second, the
test would have passed against unsupervised code — it did, at first, and that
is how F10 was found.
