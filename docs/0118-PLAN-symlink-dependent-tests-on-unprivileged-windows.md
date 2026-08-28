---
status: in-progress
date: 2026-08-27
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0118 — Symlink-dependent tests probe for the privilege

Implements [0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md](0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md)
decisions D1–D6, closing findings F1–F5.

## Goal

`go test ./...` passes in an ordinary non-elevated Windows shell, with three
tests skipping for a stated reason, and continues to *run* those tests
everywhere the symlink privilege exists — including CI, where its absence
becomes a build failure rather than a silent skip.

Finish line:

* unprivileged Windows: `go test ./...` green, exactly three skips, each naming
  the privilege;
* `MC_REQUIRE_SYMLINK=1`: those three fail instead of skipping;
* Developer Mode / elevated / Linux / macOS: no skips, tests run as today;
* a probe failure that is *not* `ERROR_PRIVILEGE_NOT_HELD` still fails.

## Scope

### In scope (the only files any phase may touch)

* `internal/testexec/` — the new `SkipIfNoSymlink` helper (existing file
  alongside the other `SkipIfNo*` helpers)
* `internal/fsutil/atomic_test.go`
* `internal/provider/acpagent/session_test.go`
* `internal/provider/codex/projects_p6_test.go`
* `.github/workflows/ci.yml` — one env line on the Windows test lane
* `AGENTS.md` or `docs/ops-windows-install.md` — one line on the developer
  prerequisite, only if P3 finds no existing home for it

### Out of scope

* Any change to `WriteFileAtomic`, `pathWithinRoots`, or codex project
  validation. This plan changes **no production code**. If a phase finds itself
  editing a non-`_test.go` file outside `internal/testexec`, it has left scope.
* Softening, rewriting, or symlink-free re-expression of the three assertions
  (MADR F2, D5).
* Auditing the tree for other tests that could use the helper (D6).
* The tree-wide CRLF normalisation found in the same session. Unrelated,
  larger, and not covered by this record.

## Stability rule

Every phase ends with:

```bash
make pre-add-check
gofmt -l cmd internal                  # must print nothing
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build ./...
go test -count=1 ./internal/testexec/ ./internal/fsutil/ \
        ./internal/provider/acpagent/ ./internal/provider/codex/
```

then **one commit** (`git commit --no-edit`; never `-m`). **No `git push` and
no tags in this plan** — with the exception that P4 cannot be verified without
one, which is why P4 is explicitly gated on the owner (see below).

## Cross-cutting contracts

**C1 — No production code.** The entire change surface is tests, one test
helper, and one CI line.

**C2 — Skip is never the fallback for an unknown error.** Only
`ERROR_PRIVILEGE_NOT_HELD` skips. Everything else fails (MADR D2, F5).

**C3 — No `GOOS` check in the helper.** The predicate is the machine's
capability, not its operating system (MADR F3). A phase that reaches for
`runtime.GOOS` here has taken option A, which was rejected.

**C4 — Assertions are untouched.** The diff to each of the three test files is
one added `SkipIfNoSymlink` call, plus an import line in
`acpagent/session_test.go` (and possibly `codex/projects_p6_test.go`), which
already lack it. No existing line in any of the three is modified.

## Implementation Steps

### P1 — The helper (D1, D2, D3)

Add to `internal/testexec`, beside the existing `SkipIfNo*` helpers:

```go
// ERROR_PRIVILEGE_NOT_HELD. Declared as a plain Errno so this file needs no
// build tag: syscall.Errno exists on every supported platform and this value
// simply never occurs on Unix.
const errPrivilegeNotHeld = syscall.Errno(1314)

// SkipIfNoSymlink skips t where the process cannot create a symbolic link.
//
// The predicate is the machine, not the OS (MADR 0118 F3): the same Windows
// binary succeeds under Developer Mode or an elevated shell and fails
// otherwise, so a GOOS check would discard coverage on every machine that can
// actually run the test. Any probe failure other than the missing privilege is
// fatal — a blanket skip would convert a broken filesystem into silent
// non-coverage (MADR 0118 D2).
//
// Set MC_REQUIRE_SYMLINK to turn the skip into a failure, as CI does.
func SkipIfNoSymlink(t *testing.T) { /* body per MADR D1 */ }
```

Imports needed: `errors`, `os`, `path/filepath`, `syscall`, `testing`.

**Self-test.** Add `internal/testexec/symlink_test.go` asserting the two
branches that can be exercised without changing machine privilege:

* the helper does not panic and leaves no probe artefacts behind (`t.TempDir`
  handles cleanup, so assert only that it returns or skips);
* `errors.As(err, &errno) && errno == errPrivilegeNotHeld` matches a
  synthesised `&os.LinkError{Err: errPrivilegeNotHeld}` — this pins the
  unwrapping behaviour the probe measured, on every platform, without needing
  Windows to run it.

The second is the one that matters: it is the part of D1 that would break
silently if a future Go release changed the error wrapping.

**Verification:** `go test ./internal/testexec/` on the host;
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go vet ./internal/testexec/` to confirm
the no-build-tag claim holds on a Unix target.

### P2 — Apply to the three call sites (D5, C4)

One line each, immediately before the first `os.Symlink`:

| File | Line | Note |
| --- | --- | --- |
| `internal/fsutil/atomic_test.go` | before `:61` | file symlink; already imports `testexec` |
| `internal/provider/acpagent/session_test.go` | before `:708` | directory symlink; **needs the import added** |
| `internal/provider/codex/projects_p6_test.go` | before `:73` | directory symlink |

Import status, checked rather than assumed:

* `internal/fsutil/atomic_test.go` — **already imports** `testexec`; no import
  edit needed.
* `internal/provider/codex/` — the package already imports `testexec` widely
  (`apikey_test.go`, `auth_test.go`, and others), but `projects_p6_test.go`
  itself must be checked and may need the import line added.
* `internal/provider/acpagent/session_test.go` — **does not import**
  `testexec`; this is the one file that gains an import as well as the call.

`testexec` imports only stdlib (`internal/testexec/testexec.go`), so no cycle
is possible in any direction.

Per MADR D3 one probe covers both link kinds (measured: identical errno), so
all three get the same helper.

**Verification (this is the phase that proves the goal):**

```text
go test ./internal/fsutil/ ./internal/provider/acpagent/ ./internal/provider/codex/
  → ok; exactly 3 skips naming the privilege

MC_REQUIRE_SYMLINK=1 go test ./internal/fsutil/
  → FAIL, message names MC_REQUIRE_SYMLINK

go test ./...
  → green on this unprivileged Windows machine, which it is not today
```

### P3 — CI demands the capability (D4)

Add `MC_REQUIRE_SYMLINK: "1"` to the Windows test lane (`.github/workflows/ci.yml:267`
matrix leg) so a skip there is a failure.

**Answer MADR open question 1 first.** Before setting it, determine whether the
Windows runner currently holds the privilege — the MADR marks this
**[unverified]** and it decides whether this phase is inert or turns the lane
red:

* if CI holds it: setting the var is a no-op today and a tripwire later —
  land it;
* if CI does *not* hold it: these three tests are currently **failing or
  skipping in CI already**, and D4 would make that loud. That is the intended
  behaviour, but it is a red lane on first push and needs the owner's
  agreement, not a plan's assumption.

The cheapest way to find out is reading the existing Windows lane's log for
those three test names. Do that before editing the workflow.

**Also decide MADR open question 2** — whether the Linux/macOS lanes get the
var too. Recommendation: yes, inert but explicit, so a sandbox regression on
those runners surfaces as a failure rather than a skip. One line each; state
the choice rather than leaving it implicit.

**Verification:** `.github/workflows/ci.yml` parses (`gh workflow view` or a
YAML lint); no local test change.

### P4 — Confirm on CI (gated)

This is the only phase requiring a push, which the stability rule otherwise
forbids. **Do not execute without an explicit instruction in the same turn.**

Push the branch, read the Windows lane, and confirm: three tests run (not
skip), the lane is green, and `MC_REQUIRE_SYMLINK` did not fire.

If P3's investigation showed CI lacks the privilege, this phase instead
confirms the lane fails *for the stated reason* and the owner decides between
enabling Developer Mode on the runner and relaxing D4.

## Verification (whole plan)

```bash
# unprivileged Windows shell (the machine this was found on)
go test ./...                                  # green, 3 skips
MC_REQUIRE_SYMLINK=1 go test ./internal/fsutil/ # FAIL, names the var

gofmt -l cmd internal                          # empty
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build ./...
```

**Not verifiable on this host:** the privileged path (tests actually running on
Windows) needs Developer Mode or an elevated shell, and `-race` needs a
non-Windows host per 0116 P8 C7. Both are stated rather than skipped; P4 covers
the first via CI.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Unprivileged Windows: `go test ./...` green | D1, D5 |
| A2 | Exactly three skips, each naming the privilege | D1 |
| A3 | `MC_REQUIRE_SYMLINK=1` converts skip to failure | D4 |
| A4 | Non-privilege probe errors still fail | D2 |
| A5 | Linux/macOS unchanged, no skips | D1 |
| A6 | Windows CI runs the three tests, does not skip them | D4, F4 |
| A7 | No production file changed | C1 |
| A8 | No existing line in the three test files is modified; additions are the helper call and, where absent, its import | C4 |

A4 is the one to guard: it is the difference between this helper and the
blanket skip that would have hidden a real failure.

## Rollout and Rollback

Test-only. No binary behaviour changes, so there is nothing to roll out and
nothing a user can observe. `git revert` of P2 restores today's failures;
reverting P1 as well removes the helper. P3 reverts independently of both.

## Execution record (2026-08-27)

**P1, P2, P3 executed. P4 not run** — it needs a push, which the stability rule
forbids without an instruction in the same turn, and none was given. The plan
stays `in-progress` until it happens.

**Result.** `go test ./...` is green on the unprivileged Windows machine this
was found on (A1), which it was not at `b92c4e7`. The staged diff is 111
insertions and **zero deletions** across five files, so C1 and C4 hold
mechanically rather than by inspection. `MC_REQUIRE_SYMLINK=1` converts the
skip to a failure naming the variable (A3).

**Four things the plan did not predict correctly. Recorded because each is
evidence about how the plan was written, not only about the code.**

1. **P3's investigation cost nothing, and the plan budgeted for a CI run.**
   The plan proposed reading the Windows lane's log for the three test names.
   That was unnecessary: the lane being *green* at `b92c4e7`, where the tests
   are unguarded, already proves the privilege is held. A passing test is
   stronger evidence than a log line, and it existed before the plan was
   written. MADR F4 is now verified, not inferred.

2. **`gofmt -l cmd internal` cannot print nothing on this machine.** The
   stability rule requires it to. It lists ~600 files, including
   `acpagent/session_test.go` — all of them CRLF, which is the very issue this
   plan defers in its last bullet. The rule was written without noticing it
   depends on the deferred item. Substituted: `gofmt -l` over the five touched
   files, which is empty. Normalising `session_test.go` was rejected because it
   rewrites all 748 lines and would breach C4; `.gitattributes` (0116 D27)
   normalises it on staging anyway, so the *committed* content is LF and the
   recorded diff is the one added line.

3. **`make pre-add-check` could not run: `golint` was absent.** Exit 2 is
   "required tool missing", not a finding. Installed via the line
   `scripts/go-precheck.sh` prints for exactly this case. `govulncheck` cannot
   reach its database from here; the script already treats that as a warning
   rather than a finding, and the gate was run with
   `GO_PRECHECK_SKIP_VULN=1`. All five files pass gofmt and golint.

4. **A2 counts four skips naming the privilege, not three.** The fourth is P1's
   own self-test: the `probe` subtest of `TestSkipIfNoSymlinkProbeNeverFails`,
   which exercises the helper and therefore skips exactly as the call sites do.
   The plan asked for that self-test ("assert only that it returns or skips")
   without noticing it would report a skip of its own. The three **call sites**
   are exactly three, which is what A2 is about. Nesting the probe in a subtest
   is what keeps the self-test itself passing rather than skipping; the
   alternative — dropping the assertion to make a number match — would trade
   real coverage for a tidier count.

**One unrelated failure, investigated and dismissed.**
`TestDiscoveryForwardsToDialectWhenEngineIsUp` (`internal/provider/httpagent`)
fails in a bare PowerShell and passes in Git Bash: it needs the POSIX `false`
binary on `PATH`. Not a code defect, not caused by this change, and not
0118's — it is the 0116 P11 class (an extensionless POSIX executable), and it
is why the whole-suite runs above were done in Git Bash, which is the shell the
MADR's own baseline was measured in.

**Also learned, outside this plan.** CI run 33101607297 on `b92c4e7` is green
across `Go (test; build on tag)`, `Go (windows/amd64)` and `Go (linux/arm64)`.
That is the third Windows run [0116-PLAN](0116-PLAN-windows-and-linux-arm64-build-targets.md)
records as "not yet verified — that needs a push"; it has happened, and it
passed. Updating 0116's status belongs to 0116, not here.

## Deferred (named, so they are not mistaken for oversights)

* **Auditing the tree for other symlink-dependent tests** — MADR D6 scopes this
  record to the three known sites. A sweep is cheap but belongs to whoever adds
  the fourth.
* **Enabling Developer Mode on developer machines** — option D, rejected in the
  MADR. Nothing here prevents a developer from enabling it; with it on, all
  three tests simply run.
* **The tree-wide CRLF issue** — 1282 files carry CRLF despite
  `.gitattributes` specifying `eol=lf`, because the checkout predates the rule
  and git normalises on read so `git status` stays clean. Found in the same
  session, unrelated to symlinks, and needs its own record: the fix
  (`git rm --cached -r . && git reset --hard`) rewrites the entire working
  tree and wants a deliberate decision, not a footnote in this one.
