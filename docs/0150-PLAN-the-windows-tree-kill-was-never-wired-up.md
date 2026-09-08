---
status: completed
date: 2026-09-07
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0150 — Wire the Windows tree-kill guarantee

Implements [0150-MADR-the-windows-tree-kill-was-never-wired-up.md](0150-MADR-the-windows-tree-kill-was-never-wired-up.md)
decisions D1–D7, closing findings F1–F9.

## Goal

1. `procutil.SuperviseStarted` compiles and is callable on every target, as a
   no-op off Windows.
2. All six long-lived spawn sites supervise their process, and hold the
   returned `release` for that process's lifetime.
3. No site defers `release` at the spawn point.
4. A test proves a *provider's* engine is supervised — not only that job
   objects work in isolation.
5. MADR 0116 D8 describes what shipped, and D9's first survivability bullet is
   true rather than aspirational.
6. Unix behaviour is byte-for-byte unchanged.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `internal/procutil/procutil_unix.go` | P1 | no-op declaration (D1) |
| `internal/procutil/procutil_other.go` | P1 | no-op declaration (D1) |
| `internal/procutil/procutil_windows.go` | P1 | correct the doc comment (F3) |
| `internal/provider/acphttp/provider.go` | P2 | engine spawn (D2) |
| `internal/provider/httpagent/provider.go` | P2 | engine spawn (D2) |
| `internal/provider/acpagent/acpagent.go` | P2 | engine spawn (D2) |
| `internal/provider/codex/provider.go` | P3 | engine spawn (D2) |
| `internal/provider/acpagent/terminal.go` | P3 | terminal spawn (D2) |
| `internal/providerauth/cli.go` | P3 | auth CLI spawn (D2) |
| `internal/procutil/supervise_wiring_test.go` (new) | P4 | wiring evidence (D7) |
| `docs/0116-MADR-windows-and-linux-arm64-build-targets.md` | P5 | amend D8/D9 (D6) |

### Out of scope

* **`SetProcessGroup`.** D4 keeps job creation out of it — `os/exec` has no hook
  between `CreateProcess` and the caller regaining control, which is the whole
  reason `SuperviseStarted` exists.
* **The six `cmd.Run` sites** (`codex/adapter.go`, `codex/auth.go` ×2,
  `codex/logout.go` ×2, `codex/store_reality.go`). D5 defers them; they wait for
  the child, so the window is bounded by the call.
* **Any Unix code path.** C1 makes this a contract, not an aspiration.
* **The other three findings from the 2026-09-07 Windows pass.** Separate
  subjects; separate records if they are taken up.

## Stability rule

Every phase ends with:

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
go build ./... && go test ./... && go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')
```

and, before any push, from this Windows host:

```bash
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

The three-target cross-build is not ceremony here: F2 is a compile error that
only appears off Windows, and P1 exists to fix exactly that class.

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — Unix behaviour does not change.** The no-op must return
`func() {}, nil` and do nothing else. A Unix `go test ./...` before and after
P1 must be identical.

**C2 — `release` is never deferred at the spawn site.** It is stored and called
from teardown.

**C3 — no provider gains a second teardown path.** `release` is called beside
the existing `TerminateProcessGroup` / `KillProcessGroup` / engine-close call,
not from a new goroutine, timer or finaliser.

**C4 — a failure to supervise is an error, not a silent skip.** If
`SuperviseStarted` returns an error the spawn must fail loudly, the same way a
failed `cmd.Start` does. Starting an engine that is known to be unsupervised is
the state this record exists to end.

**The contract most at risk is C2**, and it is the reason this plan exists in
the shape it does. `defer release()` immediately after `cmd.Start()` is the
natural Go reflex, it compiles, it reads correctly in review, and it passes any
test that only asserts the engine started — because the engine is killed when
the *starting function* returns, not when the test's assertion runs. It would
present as "the engine dies instantly on Windows, sometimes", which is a much
worse day than the bug being fixed.

## Dependency and delivery order

P1 first — nothing else compiles without it (F2). P2 before P3: the three
engine providers are where the grandchildren actually are, and getting the
lifetime shape right on them sets the pattern for the rest. P4 after P2 so the
test has a wired site to assert against. P5 last, so the amendment describes
what landed rather than what was intended — which is the failure mode of D8
itself.

## Implementation Steps

### P1 — make the API cross-platform (D1; closes F2, F3)

Add to `procutil_unix.go` and `procutil_other.go`:

```go
// SuperviseStarted is a no-op off Windows: a process group started with
// Setpgid is already signalled as a unit, so there is nothing to attach.
func SuperviseStarted(p *os.Process) (release func(), err error) {
	return func() {}, nil
}
```

Correct the Windows doc comment, which currently claims a Unix no-op that does
not exist (F3), and its "the caller invokes right after Start" line, which
describes a caller that does not yet exist.

**Verification.** The three-target cross-build passes with a temporary
cross-platform caller present, and `go test ./internal/procutil/` is unchanged
on Unix (C1).

### P2 — supervise the three engine providers (D2, D3; closes F1 for engines)

`acphttp/provider.go:327`, `httpagent/provider.go:494`,
`acpagent/acpagent.go:426`. Each already owns the process:

| Site | Where `release` belongs |
| --- | --- |
| `acphttp` | `engine struct { cmd, url, port, dead }` — add `release func()` |
| `httpagent` | `engine struct { cmd, url, port, id, dead }` — add `release func()` |
| `acpagent` | the struct that holds the ACP connection and `cmd` |

Call `SuperviseStarted(cmd.Process)` immediately after `cmd.Start()`, store the
result, and invoke it from the existing teardown — beside the call that already
terminates the process (C3). On error, fail the spawn (C4).

**Verification.** Grep shows three calls and zero `defer release`. On Windows,
start an engine and confirm the daemon still serves it for longer than the
starting function's lifetime — that is the C2 trap, and the only way to see it
is to let time pass after startup returns.

### P3 — supervise the remaining long-lived sites (D2, D3; closes F1)

`codex/provider.go:523` (holds a `procutil.RegisterEngine` lease — `release`
belongs with it), `acpagent/terminal.go:90` (`terminalProc`), and
`providerauth/cli.go:85`.

`providerauth/cli.go` is long-lived despite appearances: it calls `cmd.Start()`
and then waits asynchronously (`go func() { f.err = cmd.Wait() }()`), returning
a handle. It belongs in this group, not with the `cmd.Run` sites — which
answers the MADR's open question 2.

**Verification.** Grep shows six calls total. The auth flow still completes on
Windows; a cancelled auth leaves no surviving child.

### P4 — prove a provider is supervised, not just the mechanism (D7)

`TestSuperviseStartedKillsTree` proves job objects work. What regressed was the
*wiring*, so the test must assert on a wired path.

Use the helper-process idiom this repo already established (MADR 0147 D9/D10):
re-exec the test binary as a stand-in engine that spawns a grandchild, run it
through a provider's real start path, then take the provider's teardown and
assert the grandchild is gone.

Where routing a fake binary through a provider is too costly, the cheaper
proxy — assert that the struct's stored `release` is non-nil after start and is
invoked by teardown — still catches the regression that happened, which was a
call that was never made. Prefer the real path; take the proxy only with a
comment saying which guarantee it does and does not cover.

**Verification.** The test fails against the pre-P2 tree. That is the only
evidence that it tests the wiring rather than the job object.

### P5 — amend MADR 0116 (D6; closes F6, F7)

Two corrections, additive, not edits to the original rationale:

* **D8** describes `SetProcessGroup` creating and `TerminateProcessGroup`
  closing the job. Neither is true; record what shipped and why
  (`os/exec` has no hook), and that `SuperviseStarted` is the compensating step
  — now wired by this record.
* **D9's** first survivability bullet asserts orphan prevention "already"
  exists. Note that it was not true from `ef52386` until this plan landed, and
  is true now.

**Verification.** 0116 carries an amendment naming 0150; its original D8/D9 text
is unedited.

## Verification (whole plan)

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
go test ./... && go test -race ./...
grep -rn 'SuperviseStarted' --include='*.go' internal/ | grep -v _test | grep -v procutil/
grep -rn 'defer release' --include='*.go' internal/
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | All three targets build | D1, F2 |
| A2 | Six production `SuperviseStarted` calls, at the six long-lived sites | D2, Confirmation 2 |
| A3 | Zero `defer release` at any spawn site | D3, C2 |
| A4 | A supervision error fails the spawn | C4 |
| A5 | The P4 test fails against the pre-P2 tree | D7 |
| A6 | Unix `go test ./...` output unchanged by P1 | C1 |
| A7 | 0116 amended; its original D8/D9 text unedited | D6 |
| A8 | Windows gate green; `go test -race` green | Confirmation 4 |

**A5 is the criterion most likely to be skipped**, because it requires
deliberately running a new test against old code to watch it fail — the same
shape as 0149's A7, and the same reason it gets dropped: once the fix is in, the
opportunity is gone without a revert. Without it, P4 might be asserting that job
objects work, which was never in doubt.

**A3 is the one most likely to be satisfied accidentally and then regressed.**
It should be checked at the end of every phase, not once.

## Rollout and Rollback

Test and platform-glue changes only; no protocol, no wire format, no
user-visible surface. Each phase reverts independently.

The behaviour change is confined to Windows teardown: descendants that
previously survived will now be killed. That is the intent, but it is a real
change on the platform with the least coverage — so P2 and P3 are separate
commits, and reverting P3 alone leaves the three engine providers supervised.

Nothing in CI exercises this: the Windows lane runs unit tests, and no test
spawns a real provider engine. The evidence for this record is local, which is
itself worth knowing.

## Deferred (named, so they are not mistaken for oversights)

* **The six `cmd.Run` sites** (D5). They wait for the child, bounding the
  unsupervised window to the call. Supervising them means restructuring `Run`
  into `Start`/`Wait` at each site — a larger change for a smaller exposure.
  Revisit if a codex CLI is ever seen leaving a child behind.
* **The daemon-wide job object** (MADR option C). It delivers D9's property with
  one handle and no per-site bookkeeping, and it remains the better answer if the
  per-site rule regresses again. Rejected here only because it gives up
  per-provider teardown; if a future site is found unwired, that is the evidence
  to revisit rather than to add another rule.
* **Confirming F5 on a running scheduled task.** Whether
  `GenerateConsoleCtrlEvent` actually fails without a console is inferred from
  the API contract and 0116 D12. It changes nothing about the fix — the
  escalation path is what matters — but it does change how honestly the graceful
  phase can be described.
* **A guard against a future unwired spawn site.** The natural shape is a test
  asserting every `SetProcessGroup` site is followed by supervision, which is a
  source-scan meta-test. MADR 0149 D11 shows how to write one that does not rot,
  but that record also set the bar: a class guard is justified by a third
  instance, and this is the first.

## Amendment — 2026-09-07: P4 lands in the provider package, not procutil

The scope table names `internal/procutil/supervise_wiring_test.go` for P4. That
file cannot hold this test: the assertions D7 asks for read `engine.release`
and call `Provider.startServer`, both unexported in
`internal/provider/httpagent`. A test in `procutil` can only reach `procutil`,
which is exactly the mechanism-not-wiring test that already exists.

**Corrected in-scope file for P4:**
`internal/provider/httpagent/supervise_wiring_test.go` (new). No production
file is touched by P4; the scope table's other rows are unchanged.

`httpagent` was chosen over the other two supervised providers because its
start path is reachable with a fake engine at the lowest cost: `Config.Bin` is
already the seam, `ServeArgs` hands the helper the port, and the health poll
goes green as soon as the helper serves one route. The test exercises the real
`startServer` and the real `Shutdown`, not a constructed `engine` value.

**A5 is met, twice.** The plan warned this criterion is the one most likely to
be skipped. Both sabotages were run against the P4 test in the current tree and
then reverted:

| Sabotage at the publish site | Result |
| --- | --- |
| `release: nil` (the pre-P2 shape) | FAIL — "engine has no release" |
| `release: func() {}` (stored, inert) | FAIL — "grandchild ... survived teardown" (30.1s) |

The second sabotage is the one that mattered. On its first form the test
**passed** it, because the grandchild was inheriting the engine's console
process group and `CTRL_BREAK_EVENT` was killing it without the job object's
help. That is MADR 0150 F10, added by amendment, and the test now places the
grandchild in its own process group on Windows so the assertion has something
only supervision can satisfy.

**Verification run.** `gofmt` clean; `CGO_ENABLED=0` builds for
windows/linux/darwin; the new test green at `-count=3`; `go test ./...` and
`go test -race ./...` green.

## Amendment — 2026-09-07 (second): P3 needs an idempotent release

P3's three sites do not each have one teardown path the way P2's did, and the
scope table did not anticipate what that costs.

**`internal/procutil/procutil_windows.go` is in scope for P3 as well as P1.**
The release `SuperviseStarted` returns is now wrapped in a `sync.Once`. The
reason is not tidiness: a second `CloseHandle` on the same value is not a
wasted call, because the handle number can have been reused by then and the
second close would take an unrelated handle with it. Codex alone reaches its
engine from three teardown paths — `Shutdown`, `reapAttempt`, and the death
monitor — and `Shutdown` racing the death monitor is a real interleaving, not a
hypothetical one: `handleUnexpectedEngineExit` can be past its `p.closed` check
when `Shutdown` takes the engine. Without idempotence every such site needs its
own guard, and the site that forgot would be a Windows-only handle bug of
exactly the class this record exists to remove.

**Where `release` lives, per site** (the MADR's open question 1, answered for
the last three):

| Site | Field | Invoked from |
| --- | --- | --- |
| `codex/provider.go` | `engineAttempt.release`, copied to `engine.release` | `reapAttempt`, `Shutdown`, `handleUnexpectedEngineExit` |
| `acpagent/terminal.go` | `terminalProc.release` | `killIfRunning`, which all three teardown paths already funnel through |
| `providerauth/cli.go` | `CLIFlow.release` | `Kill`, inside the existing `once` |

Two decisions inside those rows are worth recording rather than leaving in the
diff:

* **`killIfRunning` now releases even when the child is already reaped.** It
  returned early on a closed `done` channel, because signalling a recycled PID
  is worse than doing nothing. That reasoning does not extend to the job: a
  terminal whose own process has exited can still have descendants, and on
  Windows nothing has signalled them. The kill keeps its guard; the release
  sits outside it.
* **`CLIFlow` releases only from `Kill`.** A flow that ends on its own never
  calls `Kill`, so its job handle stays open until the daemon exits — where
  `KILL_ON_JOB_CLOSE` takes anything still running, which is the property 0116
  D9 wants. Releasing at child-reap was considered and rejected: for a
  shim-based CLI the surviving grandchild may still *be* the flow, and killing
  it there would break sign-in on Windows only, with no way to test it from
  this host. One held handle per device flow is the cheaper mistake.

**Coverage, stated plainly.** Of the three sites, only `providerauth` is
exercised by existing tests — ten `StartCLIDeviceFlow` calls including the
`Kill` paths, green on this Windows host. `acpagent/terminal.go` and codex's
`launchEngineProcess` have no test that spawns through them, so for those two
this phase rests on compilation, the shared shape, and review. P4's test covers
`httpagent` only. That is a real gap and is recorded here rather than implied
away by a green suite.

## Execution record — 2026-09-07

All five phases landed, one commit each, on `master`.

| Phase | Commit | What it did |
| --- | --- | --- |
| P1 | `7303077` | `SuperviseStarted` declared on every platform, no-op off Windows |
| P2 | `c6db124` | the three engine providers supervise |
| P4 | `aa30bbf` | the wiring test, and MADR F10 with it |
| P3 | `f4aabbb` | codex, agent terminals, the auth CLI; idempotent release |
| P5 | this commit | MADR 0116 amended |

P4 ran before P3, against the order this plan set. The reason was worth the
deviation: P4 is what turns P2 from "compiles and reasons correctly" into
"demonstrated", and writing it while only three call sites existed kept the
test's subject small. It also paid for itself immediately — the first version
of the test passed a sabotage it should have failed, which is how F10 was
found, and finding that before P3 rather than after meant P3's three sites were
wired with the correct model of what the job object is actually for.

### Acceptance criteria

| # | Criterion | Result |
| --- | --- | --- |
| A1 | All three targets build | met — `CGO_ENABLED=0` for windows/linux/darwin at every phase |
| A2 | Six production `SuperviseStarted` calls at the six long-lived sites | met — six, and twelve `SetProcessGroup` sites total, so the six `cmd.Run` sites account for the difference exactly |
| A3 | Zero `defer release` at any spawn site | met — the only match repo-wide is the warning comment in `procutil_windows.go` |
| A4 | A supervision error fails the spawn | met by construction at all six sites; **no test exercises the error path**, because forcing `CreateJobObject` to fail needs a seam none of the six has |
| A5 | The P4 test fails against the pre-P2 tree | met twice, both sabotages reverted; see the P4 amendment |
| A6 | Unix `go test ./...` unchanged by P1 | **not verified locally** — this host is Windows and has no Unix runner. The Linux CI lane was green on `c6db124`, and the no-op is `return func() {}, nil`, but the byte-for-byte comparison C1 describes was never run |
| A7 | 0116 amended; original D8/D9 unedited | met — the diff is 57 insertions, 0 deletions |
| A8 | Windows gate green; `go test -race` green | met at every phase |

### What this record did not do

* **The six `cmd.Run` sites** stay unsupervised (D5). Their exposure is bounded
  by the call, and supervising them means restructuring each into
  `Start`/`Wait`.
* **Two of P3's three sites have no test that spawns through them** —
  `acpagent/terminal.go` and codex's `launchEngineProcess`. P4 covers
  `httpagent`; `providerauth` is covered by its existing suite. The gap is
  named in the P3 amendment and is the most likely place for this fix to be
  silently undone.
* **F5 is still inferred.** Whether `GenerateConsoleCtrlEvent` fails under a
  running scheduled task was not tested, and F10 makes that question sharper
  rather than softer: the graceful phase turns out to do more of the work than
  this record assumed, so knowing when it is unavailable matters more.
