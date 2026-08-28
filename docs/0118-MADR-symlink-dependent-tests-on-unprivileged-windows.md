---
status: accepted
date: 2026-08-27
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Symlink-dependent tests probe for the privilege rather than assuming the platform

## Context and Problem Statement

`go test ./...` does not pass on an ordinary Windows developer machine. Three
tests fail, all for the same reason:

```text
--- FAIL: TestWriteFileAtomicRejectsSymlinkTarget (0.00s)
    atomic_test.go:62: symlink …\real …\link: A required privilege is not held by the client.
--- FAIL: TestPathWithinRootsResolvesSymlinkEscape (0.00s)
    session_test.go:709: symlink …\002 …\001\escape: A required privilege is not held by the client.
--- FAIL: TestProjectValidationCanonicalRootsNamesAndImportThreads (0.00s)
    projects_p6_test.go:74: symlink …\real …\link: A required privilege is not held by the client.
```

Creating a symbolic link on Windows requires `SeCreateSymbolicLinkPrivilege`,
held by an elevated process or granted process-wide by Developer Mode. A
standard non-elevated developer shell has neither.

MADR 0116 brought Windows in as a declared support tier and P11 of its plan
began closing test-suite portability gaps. This record covers a class that
phase did not reach, and decides **how a test that depends on an OS capability
the platform gates behind a privilege should behave.**

### What was measured, not assumed

Probed on `windows/amd64`, Go 1.26.5, ordinary non-elevated shell, at tree
`b92c4e7`:

```text
file symlink err: symlink …\t …\l: A required privilege is not held by the client.
unwraps to Errno: true (value 1314)
dir  symlink err: symlink …\realdir …\ldir: A required privilege is not held by the client.
```

Three facts follow, and they shape the whole decision:

1. The failure is **errno 1314** — `ERROR_PRIVILEGE_NOT_HELD`.
2. It **unwraps through `errors.As` to `syscall.Errno`**, so it can be matched
   precisely rather than by string.
3. **File and directory symlinks fail identically**, so one probe covers both
   kinds. Two of the three tests link a directory; one links a file.

### Findings

**F1 — Three call sites, one shape.**

| Test | File | Link kind |
| --- | --- | --- |
| `TestWriteFileAtomicRejectsSymlinkTarget` | `internal/fsutil/atomic_test.go:61` | file |
| `TestPathWithinRootsResolvesSymlinkEscape` | `internal/provider/acpagent/session_test.go:708` | directory |
| `TestProjectValidationCanonicalRootsNamesAndImportThreads` | `internal/provider/codex/projects_p6_test.go:73` | directory |

Each is `os.Symlink(...)` followed immediately by `t.Fatal(err)`.

**F2 — All three assert security invariants, so weakening them is not
available.**

They cover, respectively: atomic write refusing a symlinked target; a symlink
escaping a permitted root not counting as inside it; and project-root
canonicalisation through a symlink. These are the checks that stop a symlink
from being used to write or read outside an intended boundary. Rewriting them
to avoid symlinks would delete the property under test, not port it.

**F3 — `GOOS` is the wrong predicate; the privilege is not a platform
constant.**

The existing skip helpers in `internal/testexec` are `GOOS`-based, and
correctly so: `SkipIfNoXDG` (`:98`) skips because Windows resolves Known
Folders instead of XDG, which is a fixed platform fact.

Symlink creation is not fixed. The same `windows/amd64` binary succeeds under
Developer Mode, under an elevated shell, or as an administrator, and fails
otherwise. A `runtime.GOOS == "windows"` skip would therefore discard coverage
on every Windows machine that *can* run these tests — including, most likely,
CI (F4).

**F4 — CI probably does not see this failure. [unverified]**

`ci.yml:267` runs a `windows-latest` test lane, and the tree is green. GitHub's
Windows runners execute as an administrator account, which holds the
privilege — so the most likely explanation is that CI has it and a developer's
box does not. This could not be confirmed from the development machine and is
marked accordingly; the decisions below are deliberately written so that they
hold whether or not it is true.

If it *is* true, the practical impact is sharper than "three tests fail": the
Windows test suite is green in CI and red for a Windows developer, which is the
configuration most likely to erode trust in running tests locally at all.

**F4 — verified true, 2026-08-27 (plan P3).** CI run [33101607297][ci-b92c4e7]
on `b92c4e7` — the exact tree these three tests fail on locally — reports job
`Go (windows/amd64)` (id `98621854770`) as **success**. At that commit the three
tests are unguarded and unconditional, so an unprivileged runner would have
failed them and taken the lane red. It did not, therefore `windows-latest`
holds `SeCreateSymbolicLinkPrivilege`. The `[unverified]` marker above is
retained as written, and this paragraph is what settles it.

The consequence is the one the plan hoped for: D4 is inert on landing and a
tripwire afterwards, so it costs nothing today and does not turn a green lane
red on first push. The sharper impact described above is confirmed to be the
real one — green in CI, red on a developer's box.

[ci-b92c4e7]: https://github.com/maccavelli/magic-cli-remote/actions/runs/33101607297

**F5 — A blanket skip would hide a real failure.**

`os.Symlink` in a `t.TempDir()` can fail for reasons other than privilege — an
exotic filesystem, a sandbox, a full disk. A helper that skips on *any* error
converts those into silent non-coverage. F-2 of MADR 0116's posture applies:
refusing loudly beats degrading silently.

## Decision Drivers

* **The invariants under test are security properties.** Losing them quietly
  on one platform is the outcome to design against.
* **Coverage should be kept wherever it can run.** The privilege is
  machine-configurable, so the predicate must be the machine, not the OS.
* **A developer must be able to run `go test ./...` and get a meaningful
  result** without elevating a shell.
* **CI must not be allowed to skip silently.** If the lane that is supposed to
  verify Windows stops verifying these invariants, that must be a failure, not
  a quiet `SKIP` line in a log nobody reads.

## Considered Options

* **A — Skip on `runtime.GOOS == "windows"`**, matching the existing helpers.
* **B — Probe for the capability and skip when it is absent**, with the probe
  distinguishing "no privilege" from every other error.
* **C — Probe as in B, and additionally let CI demand the capability**, so a
  skip in the Windows lane fails the build.
* **D — Require Developer Mode**, document it as a prerequisite, and leave the
  tests failing until it is enabled.

## Decision Outcome

Chosen option: **"C — Probe, skip locally, demand in CI"**.

A is refuted by F3: it would discard the coverage on precisely the machines
that can provide it, CI included. B is most of the answer but leaves F4's
failure mode open — if the CI runner ever loses the privilege, coverage
evaporates with no signal. D is the most rigorous reading of F2, but it makes a
clean checkout fail `go test ./...` on a supported platform tier for a reason
unrelated to the code under test, which is a poor first experience and does not
survive contact with a contributor who cannot change machine policy.

C keeps the strictness where it is enforceable and the pragmatism where it is
needed.

### The decisions

**D1 — Add `testexec.SkipIfNoSymlink(t)`, probe-based.**

```go
// ERROR_PRIVILEGE_NOT_HELD. Declared as a plain Errno so this file needs no
// build tag: the value simply never occurs on Unix.
const errPrivilegeNotHeld = syscall.Errno(1314)

func SkipIfNoSymlink(t *testing.T) {
    t.Helper()
    dir := t.TempDir()
    target := filepath.Join(dir, "symlink-probe-target")
    if err := os.WriteFile(target, nil, 0o600); err != nil {
        t.Fatal(err)
    }
    err := os.Symlink(target, filepath.Join(dir, "symlink-probe-link"))
    if err == nil {
        return
    }
    var errno syscall.Errno
    if errors.As(err, &errno) && errno == errPrivilegeNotHeld {
        if os.Getenv("MC_REQUIRE_SYMLINK") != "" {
            t.Fatalf("MC_REQUIRE_SYMLINK is set but the symlink privilege is "+
                "not held: %v", err)
        }
        t.Skip("symlink creation needs SeCreateSymbolicLinkPrivilege " +
            "(Developer Mode or an elevated shell); MADR 0118")
    }
    t.Fatalf("symlink probe failed for an unexpected reason: %v", err)
}
```

`syscall.Errno` exists on every platform Go supports, so this compiles without
a build tag and needs no per-OS file. Verified by probe: the error unwraps to
`syscall.Errno(1314)`.

**D2 — Any probe failure that is not `ERROR_PRIVILEGE_NOT_HELD` fails the
test.** This is F5's requirement, and it is the line that separates this
helper from a blanket skip.

**D3 — One probe covers both link kinds.** Measured: file and directory
symlinks fail with the same errno. The probe creates a file link because it is
the cheaper of the two; no test needs a separate directory probe.

**D4 — `MC_REQUIRE_SYMLINK=1` in the Windows CI lane turns the skip into a
failure.** CI asserts the capability rather than hoping for it. If GitHub ever
changes runner privileges, the build breaks loudly instead of quietly ceasing
to test three security invariants.

**D5 — The three tests are otherwise unmodified.** They gain one
`testexec.SkipIfNoSymlink(t)` line each, before the first `os.Symlink`. No
assertion is softened, no symlink is replaced with a stand-in (F2).

**D6 — The helper is not applied preemptively.** Only the three tests in F1
get it. A future test that needs a symlink should call it, but this record does
not go looking for candidates.

### Consequences

* Good: `go test ./...` gives a Windows developer a meaningful green run
  without an elevated shell.
* Good: coverage is retained on every machine that can provide it, rather than
  discarded by OS (F3).
* Good: CI's verification of three security invariants becomes an assertion
  (D4) rather than an accident of runner configuration (F4).
* Bad: on an unprivileged machine, three security invariants genuinely are not
  verified. The skip message says so, and D4 confines that state to developer
  machines.
* Bad: one more environment variable in the CI matrix.
* Neutral: no production code changes. This record touches test files, one new
  helper, and one workflow line.

### Confirmation

```text
# unprivileged Windows shell
go test ./internal/fsutil/ ./internal/provider/acpagent/ ./internal/provider/codex/
  → ok, with three SKIP lines naming the privilege

# same shell, CI's setting
MC_REQUIRE_SYMLINK=1 go test ./internal/fsutil/
  → FAIL, naming MC_REQUIRE_SYMLINK

# Developer Mode on, or elevated
go test ./...                        → ok, no skips, tests actually run

# Linux / macOS
go test ./...                        → ok, no skips, unchanged
```

## Pros and Cons of the Options

### A — `GOOS`-based skip

* Good: one line, matches the existing helper style.
* Bad: discards coverage on every Windows machine that can run the tests,
  including CI (F3, F4).
* Bad: makes the Windows lane permanently blind to three security invariants.

### B — Probe and skip

* Good: correct predicate; keeps coverage wherever it exists.
* Bad: no signal if CI silently loses the capability.

### C — Probe, skip locally, demand in CI (chosen)

* Good: B's coverage, plus a loud failure if the CI lane stops verifying.
* Good: distinguishes "cannot" from "broken" (D2).
* Bad: an extra env var and one more thing for the matrix to get right.

### D — Require Developer Mode

* Good: strictest reading of F2 — the invariants are always verified.
* Bad: a clean checkout fails `go test ./...` on a supported tier for reasons
  unrelated to the code.
* Bad: unavailable to a contributor on a managed machine that forbids
  Developer Mode.

## More Information

### Evidence index

| Claim | Evidence |
| --- | --- |
| Three failing tests, one cause | `go test ./...` on `b92c4e7`, windows/amd64 |
| Failure is errno 1314 | probe: `errors.As` → `syscall.Errno(1314)` |
| File and dir symlinks fail alike | probe: both return the same errno |
| Call sites | `internal/fsutil/atomic_test.go:61`; `internal/provider/acpagent/session_test.go:708`; `internal/provider/codex/projects_p6_test.go:73` |
| Existing helpers are `GOOS`-based | `internal/testexec/*.go:30,60,72,86,98,111` |
| `SkipIfNoXDG` is legitimately platform-based | `internal/testexec/*.go:98–104` |
| Windows CI lane exists | `.github/workflows/ci.yml:267` |
| CI runner holds the privilege | **verified 2026-08-27** — run 33101607297, job `Go (windows/amd64)` green on `b92c4e7`, where the three tests are unguarded |

### Related records

* **MADR 0116** — declared Windows a support tier; its P11 began test-suite
  portability work. This record continues that line for a class P11 did not
  cover, and reuses its "refuse loudly rather than degrade silently" posture.
* **Commit `b92c4e7`** — "fix file permission tests for Windows compatibility",
  the immediately preceding instance of this same class of problem.

### Open questions for the plan

Both were settled during execution on 2026-08-27; the questions are kept as
asked, with their answers beneath.

1. Is F4 true? A single CI run with `-v` on the Windows lane settles whether
   these tests currently run or are about to start skipping. The plan should
   check rather than assume, because if CI *does* lack the privilege then D4
   turns a green lane red on first push and that must be an expected outcome,
   not a surprise.

   **Answered: yes.** Settled without a new run — the existing green
   `Go (windows/amd64)` job on `b92c4e7` is sufficient evidence, because the
   tests were unguarded at that commit. See the F4 amendment above. D4 lands
   inert.

2. Should `MC_REQUIRE_SYMLINK` be set on the Linux and macOS lanes too? They
   always hold the capability, so it would be inert — but inert-and-explicit
   may be preferable to absent, and it would catch a sandbox regression there.

   **Answered: yes for Linux; macOS does not arise.** The variable is set at
   job level on both `go` (ubuntu-latest) and `go-native`, the latter covering
   the `linux/arm64` and `windows/amd64` matrix legs in one declaration. There
   is no macOS job to set it on — native macOS CI is deliberately off
   (`ci.yml`, above `smoke-native`), with Darwin validated locally. If a macOS
   lane is ever restored it should carry the variable, for the same reason
   Linux does.
