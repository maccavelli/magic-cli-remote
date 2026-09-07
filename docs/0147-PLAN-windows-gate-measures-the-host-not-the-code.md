---
status: proposed
date: 2026-09-07
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0147 — Make the Windows gate measure the code, not the shell, the PATH, or the checkout

Implements [0147-MADR-windows-gate-measures-the-host-not-the-code.md](0147-MADR-windows-gate-measures-the-host-not-the-code.md)
decisions D1–D8, closing findings F1–F12.

## Goal

Observable states, all on a Windows host at `cc2e467` or later:

1. `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1`
   reports every check passing, A2 included.
2. `go test ./...` passes from **both** Git Bash and PowerShell, with the same
   set of skips in each.
3. `TestDiscoveryPropagatesDialectErrors` fails if the error it receives is not
   the dialect's error — proven by running it against the pre-P4 tree, where it
   must go red under PowerShell.
4. `TestEveryAsyncDispatchedMethodIsInTheTable` and the `internal/event` scan
   pass against CRLF input, proven by a runtime-synthesised CRLF copy.
5. No file's line endings changed, no production `.go` file changed, and no test
   skipped that was not already skipped.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `scripts/ci-windows-local.ps1` | P1, P5 | A2 probe (D1); bash test step (D2) |
| `internal/ws/op_timeout_test.go` | P2 | LF-only scan (D5) |
| `internal/event/retention_test.go` | P2 | same idiom (D5) |
| `internal/provider/httpagent/provider_test.go` | P3, P4 | vacuous assertion (D4); `Bin` (D3) |
| `internal/provider/httpagent/connected_test.go` | P4 | `Bin` call sites (D3) |
| `internal/provider/httpagent/currentmodel_test.go` | P4 | `Bin` call sites (D3) |
| `internal/provider/httpagent/modelcatalog_test.go` | P4 | `Bin` call sites (D3) |

### Out of scope

* **`internal/provider/httpagent/provider.go`** — and every other non-test `.go`
  file. D3 chose `os.Executable()` precisely so no production seam is added.
  Touching this file is the single clearest signal the plan has gone wrong.
* **`.gitattributes` and the 1668 CRLF files** — D6 keeps the renormalisation
  deferred to the MADR 0118 successor.
* **`.github/workflows/ci.yml`** — CI is already correct (it uses `shell: bash`);
  the local gate is what diverges. AGENTS.md also forbids workflow edits without
  explicit permission.
* **`internal/testexec/testexec.go`** — `SkipIfNoSymlink` is right as written
  (MADR 0147 F2); only the PowerShell probe that shadows it is wrong.
* **`TestACPConnectionSurvivesAStalledPump`** — the flake in the 0143 ledger
  (run 34139293426) is a separate matter with its own process.

## Stability rule

Every phase ends with, from Git Bash:

```bash
go build ./...
go test ./...
go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')   # changed files only, never the tree
```

and, from PowerShell, for any phase after P4:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

`gofmt -l` is scoped to changed files deliberately: a tree-wide run reports
roughly every Go file because of the CRLF state (MADR 0147 F11), and that noise
has previously been mistaken for drift. Never run `make fmt` to "fix" it.

One commit per phase, each naming its phase and the decisions it implements. The
MADR and this PLAN are committed alone, ahead of P1, under the AGENTS.md
bootstrap exception. **`git push` and tags need an explicit instruction in the
same turn** — this plan does not authorise either.

## Cross-cutting contracts

**C1 — no production code changes.** Every edit lands in a `_test.go` file or in
`scripts/ci-windows-local.ps1`.

**C2 — no test is skipped, weakened, or given a platform exemption.** Assertion
counts may only go up. The one pre-existing skip that stays is
`TestStartServerBailsWhenEngineExitsImmediately` (`provider_test.go:33`), which
genuinely executes `false` and depends on its non-zero exit.

**C3 — no file's line endings change.** Not one. D5 makes the *readers* tolerant;
it does not touch the files read.

**C4 — every assertion that runs on Linux runs on Windows**, under both shells.

**C5 — the entry point is preserved.** `make ci-windows` and
`powershell -File scripts/ci-windows-local.ps1` behave as documented in
`AGENTS.md` and `docs/ops-windows-install.md`.

**C6 — no meta-test and no new CI lane** (D8).

**The contract most at risk is C1**, in P4. If `os.Executable()` interacts
awkwardly with `launch.Resolve` for any call site, adding a two-line resolver
hook to `provider.go` is the obvious fix, would look harmless in review, and is
exactly what D3 rejected. The mitigation is that the mechanism was measured
before this plan was written: on Windows,
`exec.LookPath(os.Executable())` returns the path unchanged and without error.
If a call site nonetheless resists, **stop and amend the MADR** rather than
opening the seam.

## Dependency and delivery order

P1 and P2 are independent of everything and of each other. P3 must precede P4,
and **P5 must come last.**

The ordering is not cosmetic. Running the suite under bash makes `false` resolve
(MADR F4), so landing P5 early would turn F4 and F7 green without fixing either
— the gate would agree with CI while both tests still measured `PATH`. Putting
P5 last means P3 and P4 must be verified under PowerShell, where the defects are
actually visible.

P3 before P4 has the same shape: on the pre-P4 tree, P3's tightened assertion
must **fail** under PowerShell. That failure is the proof that F7's silent false
pass was real, and it is the only chance to observe it.

## Implementation Steps

### P1 — A2 probes with the mechanism the suite uses (D1; closes F1, F3)

Replace the `New-Item -ItemType SymbolicLink` probe at
`scripts/ci-windows-local.ps1:160-172` with one that exercises Go's `os.Symlink`
— the same call `testexec.SkipIfNoSymlink` makes. A small `go run` of a
throwaway program in `$env:TEMP`, or a `go test -run` of the existing
`TestSkipIfNoSymlinkProbeNeverFails` in `internal/testexec`, both satisfy D1;
prefer the latter, since it reuses the real helper rather than a second copy of
its logic.

Keep `MC_REQUIRE_SYMLINK=1` set unconditionally at line 89. It is correct and
intentional (MADR 0147 F2, MADR 0118 D4).

**Verification.** On this host, with Developer Mode on and rebooted, A2 passes.
To prove it is a real probe and not a stub, temporarily point it at a directory
where symlink creation is impossible and confirm it fails.

### P2 — source scans stop depending on line endings (D5; closes F8, F9, F10)

In `internal/ws/op_timeout_test.go`, normalise on read: strip `\r` from the
bytes of `server.go`, `codex_handlers.go` and `../protocol/messages.go` before
any `strings.Index` or regexp runs. That fixes both the `"\n}\n"` delimiter
(`:124`) and the latent `^\tcase (.+):$` anchor (`:66`) in one move. Apply the
same to `internal/event/retention_test.go:95`.

Add the CRLF guard chosen in D5/Q3: a subtest that copies the real source into
`t.TempDir()` with `\n` rewritten to `\r\n`, runs the same scan against the copy,
and asserts an identical result. Nothing is committed in CRLF form, so
`.gitattributes` cannot normalise the guard away.

**Verification.** `go test ./internal/ws/ ./internal/event/` passes from both
shells. Revert the normalisation locally and confirm the new subtest fails —
a guard that cannot fail is not a guard.

### P3 — the discovery error test names the error it expects (D4; closes F7)

`TestDiscoveryPropagatesDialectErrors` (`provider_test.go:227-245`) asserts only
`err != nil`. Assert on the dialect's actual message — `session listing blew up`
and `project listing blew up` — so a second error path reaching the same line
cannot satisfy it.

**Verification, and it must be done in this order:**

```powershell
go test ./internal/provider/httpagent/ -run TestDiscoveryPropagatesDialectErrors -count=1
```

From **PowerShell, before P4 lands, this must FAIL** with the `false binary not
found` error rather than the dialect error. That red line is the evidence for
F7. From Git Bash the same command passes. Record both outcomes in the commit
message; they are the only direct observation of the false pass.

### P4 — httpagent stops resolving a POSIX binary from PATH (D3; closes F4, F6)

Add one unexported helper to `provider_test.go`:

```go
// testBin returns a path that always resolves through launch.Resolve on every
// platform: the running test binary. Nothing executes it — withFakeEngine
// injects the engine — it exists only so Provider.Ready() is true. `false` was
// used for this and resolved from PATH, which differs between Git Bash and
// PowerShell (MADR 0147 F4).
func testBin(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}
```

**The rule, applied uniformly:** every `Config{Bin: "false"}` in the package
becomes `Config{Bin: testBin(t)}` — all 24 call sites across
`provider_test.go`, `connected_test.go`, `currentmodel_test.go` and
`modelcatalog_test.go` — with exactly one exception:
`TestStartServerBailsWhenEngineExitsImmediately` (`provider_test.go:36`), which
genuinely runs `false` for its non-zero exit and keeps both `Bin: "false"` and
its `exec.LookPath` skip guard at line 33. Leave a comment there saying why it
is the exception, so the surviving reference is not read as a missed edit.

Only the `Ready()`-gated sites can fail today (F6), but a package where 23 sites
say one thing and one says another is a trap; uniformity is the point.

**Verification.** From PowerShell: `go test ./internal/provider/httpagent/ -count=1`
passes, including the P3 assertion that was red a commit ago. From Git Bash: the
same, with `TestStartServerBailsWhenEngineExitsImmediately` running rather than
skipping.

### P5 — the gate runs `go test` under bash (D2; closes F5)

In `scripts/ci-windows-local.ps1`, run the A6 test step through bash rather than
invoking `go test` from PowerShell, matching `ci.yml:259` and `:327`. Keep every
host check (A2–A5) in PowerShell, and keep the script itself as the entry point
so `make ci-windows` is untouched (C5).

Fail loudly and specifically if bash is absent: a Windows host without Git Bash
should be told that, not shown a confusing test failure.

**Verification.** The full gate passes. Then, as the real test of D2, confirm
that P4 did the work and not P5: `git stash` the P5 commit, run the suite from
PowerShell, and see it still pass.

### P6 — the last PATH-resolved binary goes (D9; closes F13)

Added by the 2026-09-07 amendment, after executing P4 showed A5 could not be
met while `TestStartServerBailsWhenEngineExitsImmediately` still resolved
`false` from `PATH`.

Replace `Config{Bin: "false"}` in that test with the running test binary
re-invoked as a helper process — Go's standard idiom:

```go
// TestHelperProcessExitsNonZero is not a test. It is the immediately-exiting
// engine TestStartServerBailsWhenEngineExitsImmediately needs, and it is inert
// unless the guard variable is set.
func TestHelperProcessExitsNonZero(t *testing.T) {
	if os.Getenv("MC_HELPER_EXIT_NONZERO") != "1" {
		return
	}
	os.Exit(1)
}
```

The provider is then pointed at `os.Executable()` with
`Args: []string{"-test.run=^TestHelperProcessExitsNonZero$"}` and the guard set
in its environment. **The plan must confirm how `Config` passes extra args and
env to the spawned engine before assuming this shape** — if it cannot, say so
and stop rather than adding a production seam (C1).

Remove the `exec.LookPath("false")` skip: with nothing to look up, the skip has
nothing to guard, and leaving it would re-create the gap in a new place.

**Verification.** From **both** shells:

```bash
go test ./internal/provider/httpagent/ -count=1 -v   # census: skips must match
```

`TestStartServerBailsWhenEngineExitsImmediately` must now *run* in both, not
skip in one. Then re-check A5 and A11, which is the whole point of the phase.
The test's own assertion — prompt failure well under `serverStartTimeout` — must
still hold, so confirm it fails when the helper is made to linger rather than
exit.

**Ordering.** P6 may land before or after P5. It cannot be masked by P5: A5 is
verified by running `go test` directly in each shell, not through the gate, so
the gate's own shell is irrelevant to it.

## Verification (whole plan)

```bash
# From Git Bash
go build ./... && go test ./... && go test -race ./...
```

```powershell
# From PowerShell
go test ./... -count=1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

Both shells must produce the same pass/skip set. A test that runs in one and
skips in the other means a `PATH`- or shell-dependency survived.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | The gate reports every check passing, A2 included | Confirmation 1 |
| A2 | A2 fails when pointed at a location where symlink creation is impossible | D1 |
| A3 | `go test ./...` passes from Git Bash | Confirmation 2 |
| A4 | `go test ./...` passes from PowerShell | Confirmation 2 |
| A5 | The pass/skip set is identical in both shells | C4 |
| A6 | `TestDiscoveryPropagatesDialectErrors` asserts the dialect's message | Confirmation 3 |
| A7 | That test was observed failing under PowerShell before P4 | D4, F7 |
| A8 | No `Ready()`-gated test resolves a binary from `PATH` | Confirmation 4 |
| A9 | The CRLF guard fails when the normalisation is reverted | Confirmation 5 |
| A10 | `git diff --stat` shows no production `.go` file and no line-ending churn | C1, C3 |
| A11 | No `_test.go` in `internal/provider/httpagent` resolves `false` from `PATH` | D9 (amendment) |

**A5 was unmet as originally written, and the fix is P6, not a reworded
criterion.** Running the census A5 demands is what exposed the contradiction
between it and the D3/C2 exemption (MADR F13). A5 stands unchanged; P6 makes it
achievable.

**A7 is the criterion most likely to be quietly dropped.** It requires running a
test *expecting it to fail*, in a specific shell, at a specific commit, before
the fix lands — and once P4 is in, the opportunity is gone and cannot be
recovered without a revert. Everything else can be checked at the end; this one
cannot. If P3 and P4 are combined into a single commit for convenience, A7 is
unobservable and F7 goes back to being an assertion rather than a measurement.

**A5 is the one most likely to be checked carelessly.** After P5 the gate runs
bash, so the tempting habit is to verify there only — which is precisely the
blindness F5 describes.

## Rollout and Rollback

Five independent commits, in the stated order. Nothing here ships to a user:
every change is a test or the local gate, so there is no runtime blast radius
and no release coupling.

Rollback is per-phase `git revert`. P5 is the only phase that changes how the
gate executes, so a host that turns out to lack bash reverts P5 alone and keeps
the four test fixes. P4 is the only phase touching many call sites; it is
mechanical and reverts cleanly.

No phase depends on CI, and CI's behaviour does not change — it is already green
on all of this (F12), which is why none of it can regress there.

## Deferred (named, so they are not mistaken for oversights)

* **Tree-wide CRLF renormalisation (1668 files).** D6 keeps it out. It is a
  whole-tree diff, it is the decision MADR 0118 explicitly deferred, and D5
  makes these tests correct under either outcome. It needs its own record — and
  the strongest argument for doing it, that D5 teaches the tests to tolerate a
  state `.gitattributes` already forbids, is written up in the MADR's Option B
  rather than buried here.
* **The local `gofmt -l` noise.** Same root cause, same deferral. Until it is
  settled, CI's Gofmt step is the authority on formatting, not the local run.
* **A meta-test or second CI lane policing shell-dependent tests.** D8 rejected
  both for now. Revisit when a third instance appears — that is the evidence
  that would change the answer, and it belongs in a new record.
* **`TestACPConnectionSurvivesAStalledPump`.** The first and so far only row in
  the 0143 flake ledger (run 34139293426, first attempt fail, retry pass). It is
  a timing test on a Windows runner, unrelated to the three defects here. The
  ledger is the right place to watch whether it becomes a pattern.
* **Withdrawing or rewording 0145's "mirrors CI" claim.** After P5 the claim is
  true, so no edit is needed. If a future divergence makes it false again, the
  honest fix is to narrow the claim in 0145 rather than to widen this record.
