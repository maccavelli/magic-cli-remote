---
status: proposed
date: 2026-09-07
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Make the Windows gate measure the code, not the shell, the PATH, or the checkout

## Context and Problem Statement

`make ci-windows` (MADR/PLAN 0145) exists so a Windows contributor learns about a
go-native failure before GitHub does. Its `.SYNOPSIS` states the contract
plainly: it "Mirrors CI go-native windows/amd64 unit contract by default."

On the dev laptop it does not mirror CI. After enabling Developer Mode and
rebooting on 2026-09-07, the gate still reports two failed checks and two failed
tests — and **not one of the three underlying defects is a fact about the code
under test.** One is the gate probing a capability with the wrong mechanism; one
is a test resolving a POSIX binary from `PATH`, which differs between the shell
CI uses and the shell the gate uses; one is a test scanning Go source for
LF-only delimiters in a working tree that is partly CRLF.

The cost is not only the noise. The same PATH difference that makes one test
fail loudly makes a second test **pass for the wrong reason**, which is the more
expensive half: a red line gets investigated, a green line does not.

### What was measured, not assumed

All measurements are on `cc2e467`, Windows 11 Home 10.0.26200, Go 1.26.6,
`CGO_ENABLED=0`, after Developer Mode was enabled and the host rebooted
(boot `2026-09-07 11:14:23`).

**The gate's three runs, before and after the privilege change.** Identical
invocation, `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1`:

| Run | Developer Mode | Failed checks | Failed tests |
| --- | --- | --- | --- |
| 1, 2 (pre-reboot) | off, then on-but-unrebooted | A2, A6 | 6 |
| 3 (post-reboot) | on | A2, A6 | 2 |

The four tests that cleared are `TestWriteFileAtomicRejectsSymlinkTarget`
(`internal/fsutil`), `TestPathWithinRootsResolvesSymlinkEscape`
(`internal/provider/acpagent`), `TestProjectValidationCanonicalRootsNamesAndImportThreads`
(`internal/provider/codex`) and `TestSkipIfNoSymlinkProbeNeverFails`
(`internal/testexec`). They now pass **with `MC_REQUIRE_SYMLINK=1` set**, which
is the assertion MADR 0118 D4 wanted: the capability is present, not merely
skipped around.

**A2 disagrees with the suite it guards.** Two probes, same shell, same minute:

```text
PowerShell 5.1  New-Item -ItemType SymbolicLink  -> Administrator privilege required for this operation.
Go              os.Symlink                       -> go os.Symlink OK
```

`scripts/ci-windows-local.ps1:165` uses the first. `internal/testexec/testexec.go:149`
— the helper every symlink test calls — uses the second. Windows PowerShell 5.1
never requests `SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE`, so it needs the
privilege that Developer Mode deliberately does not grant; Go passes the flag and
succeeds. A2 therefore reports "cannot create symlinks" about a host on which
every symlink test passes.

**The `false` failure is a shell difference, not a platform one.** Same commit,
same machine, same test, two shells:

```text
$ go test ./internal/provider/httpagent/ -run TestDiscoveryForwardsToDialectWhenEngineIsUp   # Git Bash
ok      github.com/maccavelli/magic-cli-remote/internal/provider/httpagent  0.186s

PS> ...\ci-windows-local.ps1                                                                  # PowerShell
--- FAIL: TestDiscoveryForwardsToDialectWhenEngineIsUp (0.00s)
    provider_test.go:199: ListAgentSessions: false binary not found
```

`exec.LookPath("false")` from each shell explains it exactly:

```text
Git Bash    false -> "C:\Program Files\Git\usr\bin\false.exe"  err=<nil>
PowerShell  false -> ""                                        err=exec: "false": executable file not found in %PATH%
```

`C:\Program Files\Git\usr\bin\false.exe` exists on this box (33123 bytes,
dated Feb 12 2026). Git Bash puts that directory on `PATH`; PowerShell does not.

**CI is green for the same reason Git Bash is.** `.github/workflows/ci.yml:259`
pins the Windows unit leg to `{ runner: windows-latest, label: windows/amd64, shell: bash }`,
and the test step at line 327 sets `shell: bash` again inside the retry action.
The workflow even says why, at line 494: "`shell: bash` is explicit because
windows-latest defaults to PowerShell." So CI has never exercised this test under
PowerShell, and the local gate — which does — is not mirroring it.

**The CRLF failure is neither shell- nor privilege-dependent.** It reproduces
identically under Git Bash and PowerShell:

```text
--- FAIL: TestEveryAsyncDispatchedMethodIsInTheTable (0.00s)
    op_timeout_test.go:185: codexPhoneOperations not delimited
```

`internal/ws/op_timeout_test.go:118-124` locates the `codexPhoneOperations`
table and then searches for the literal `"\n}\n"` to find its end. Of the four
files the test reads, exactly one is CRLF:

| File read by the test | Endings |
| --- | --- |
| `internal/ws/server.go` | LF |
| `internal/ws/codex_handlers.go` | **CRLF** |
| `internal/protocol/messages.go` | LF |
| `internal/protocol/op_timeouts.json` | LF |

**The working tree is mixed, and git thinks it is clean.** 1668 tracked text
files carry CRLF. `.gitattributes:1` says `* text=auto eol=lf`, but that binds at
checkout: files rewritten by recent pulls are LF, files checked out earlier under
`core.autocrlf=true` (still set locally) are CRLF and were never renormalized.
`git status` reports a clean tree because the index stat cache matches what was
written at checkout time. CI never sees any of it, because a fresh clone honours
`.gitattributes`.

### Findings

**F1 — A2 probes a capability the suite does not use, and reports a false
negative.** `scripts/ci-windows-local.ps1:165` calls `New-Item -ItemType SymbolicLink`;
`internal/testexec/testexec.go:149` calls `os.Symlink`. Measured on 2026-09-07:
the first fails, the second succeeds, on the same host in the same minute. A2 is
now the only thing keeping the symlink checklist red, and it is wrong.

**F2 — the symlink capability is genuinely present, and `MC_REQUIRE_SYMLINK=1`
now asserts it correctly.** All four previously-failing symlink tests pass with
the variable set. Setting it unconditionally at `scripts/ci-windows-local.ps1:89`
is *correct* and intentional per MADR 0118 D4 — an earlier reading during this
investigation called the unconditional set a defect, and that reading was wrong.
F1 is the whole of the remaining symlink problem.

**F3 — the only valid symlink probe is a functional one.** Developer Mode does
not add `SeCreateSymbolicLinkPrivilege` to the token: `whoami /priv` listed no
such entry either side of the change, before or after the reboot. It enables an
unprivileged-create path instead. A privilege-name check would therefore report
"absent" on a working host, and Go's `SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE`
did **not** remove the need to reboot — measured, not assumed.

**F4 — `TestDiscoveryForwardsToDialectWhenEngineIsUp` measures `PATH`, not the
transport.** It passes under Git Bash and fails under PowerShell on the same
commit and machine, because `exec.LookPath("false")` differs between them.

**F5 — the gate does not mirror CI's shell, which is the contract 0145 claims.**
CI runs the Windows unit lane under `shell: bash` (`ci.yml:259`, `:327`);
`scripts/ci-windows-local.ps1` runs `go test` from PowerShell. Every PATH-derived
divergence between the two is invisible to CI by construction.

**F6 — the binary in question is never executed.** `Config{Bin: "false"}` exists
only so `Provider.Ready()` (`internal/provider/httpagent/provider.go:160-163`,
via `launch.Resolve`) returns true; the test injects a live `httptest` server
through `withFakeEngine`. There are 24 `Bin: "false"` call sites across five
files in that package, but only the ones reaching a `Ready()`-gated method
(`ListAgentSessions`, `ListProjects` — `provider.go:309`, `:326`) can fail.

**F7 — the same root cause makes a second test pass for the wrong reason.**
`TestDiscoveryPropagatesDialectErrors` (`provider_test.go:227-245`) asserts only
`err != nil`. Under PowerShell it receives `"false binary not found"` instead of
the dialect's `"session listing blew up"`, so it is green while testing nothing.
This is the more dangerous defect of the two, because nothing draws attention
to it.

**F8 — the ws source scan is LF-only, against a mixed tree.**
`op_timeout_test.go:124` searches for `"\n}\n"` in `codex_handlers.go`, which is
CRLF. The sibling delimiter at `:58` (`"\n// asyncHandler is a slow WS op"`)
survives CRLF by accident, because the newline it anchors on is immediately
followed by the text it matches.

**F9 — a second LF assumption in the same file is latent, not benign.** The
`caseLabel` regex `^\tcase (.+):$` (`op_timeout_test.go:66`) is applied to
`strings.Split(sw, "\n")`, so on a CRLF file every line retains a trailing `\r`
and the `$` anchor fails. It works today only because `server.go` happens to be
LF. Should a future pull rewrite that file the other way, this test fails
differently and for the same underlying reason.

**F10 — the idiom has already spread.** `internal/event/retention_test.go:95`
uses the identical `"\n}\n"` delimiter against `retention.go`, which is LF today.
It is one checkout away from the same failure.

**F11 — the tree-wide CRLF state is real but out of proportion to this record.**
1668 tracked files, `git status` clean, root cause a pre-`.gitattributes`
checkout under `core.autocrlf=true`. MADR 0118 defers the tree-wide issue and it
still has no record of its own. Renormalising is a whole-tree diff and a separate
decision.

**F12 — every one of these is invisible to CI.** Fresh checkout (LF everywhere),
bash shell (`false` on `PATH`), runners that hold the privilege. CI cannot
regress any of them, so the local gate is the only place they can be caught —
which is precisely why the gate must be trustworthy.

## Decision Drivers

* **A gate that is red for host reasons trains its reader to ignore it.** Three
  runs in one session produced the same red lines for three unrelated
  environmental reasons; the next real regression arrives on the same lines.
* **A false pass costs more than a false failure.** F7 is green today and tests
  nothing.
* **0145's stated contract is "mirrors CI".** Either the gate matches CI's
  execution contract or the claim should be withdrawn; a partial mirror that
  diverges silently on `PATH` is the worst of the three.
* **Do not weaken tests to make a host happy.** MADR 0118 D2 already established
  the principle: a blanket skip converts a broken environment into silent
  non-coverage.
* **Prefer fixing the measurement over fixing the world.** Renormalising 1668
  files to satisfy two `strings.Index` calls is the tail wagging the dog.

## Considered Options

* **A — Fix each defect at its own root: probe parity, shell parity,
  line-ending-agnostic scans, and tighten the vacuous assertion.** (chosen)
* **B — Renormalise the working tree to LF and leave the scans as they are.**
* **C — Skip the two failing tests on Windows.**
* **D — Change only the gate to run under bash, and leave the tests alone.**

## Decision Outcome

Chosen: **Option A**, because each of the three defects has a different root and
only A addresses all three without weakening a test or moving 1668 files. B and
D each fix exactly one finding and leave the false pass (F7) standing.

### The decisions

**D1 — A2 must probe with the mechanism the suite uses.** Replace the
PowerShell `New-Item -ItemType SymbolicLink` probe with one that exercises Go's
`os.Symlink`, so the gate's capability check and `testexec.SkipIfNoSymlink`
cannot disagree. Closes F1, F3.

**D2 — the gate runs `go test` under the same shell contract as CI.** CI pins
`shell: bash`; the gate must do the same for the test invocation. Where the gate
keeps PowerShell for host checks, that is fine — the *test* invocation is the
part that must match. **Resolved 2026-09-07 (Q2): the PowerShell script shells
out to bash for the test step only.** `powershell -File scripts/ci-windows-local.ps1`
stays the entry point, `make ci-windows` is unchanged, and the host checks stay
in PowerShell. The rejected alternative was prepending Git's `usr\bin` to `PATH`
inside PowerShell: it fixes today's symptom but mirrors one artefact of CI's
environment rather than its execution contract, so the next PATH divergence
would again be invisible. Closes F5.

**D3 — no test may depend on a POSIX binary resolved from `PATH`.**
`TestDiscoveryForwardsToDialectWhenEngineIsUp` needs `Ready()` to be true and
never runs the binary (F6); it must obtain that without `false`. **Resolved
2026-09-07 (Q1): use `os.Executable()` — the running test binary.** It exists on
every platform by construction, satisfies `launch.Resolve` under both the POSIX
executable-bit and Windows PATHEXT rules (MADR 0116 D24), and is never executed
because `withFakeEngine` injects the engine. Decisive property: `Config.Bin` is
*already* the seam, so no production code changes for a test's benefit. The
rejected alternative was an injectable resolver in `provider.go`; it is more
explicit but adds production surface that exists only for tests. Substituting a
different always-present binary name (`cmd`, `go`) was rejected outright — that
is the same bug with a longer fuse. Closes F4, F6.

**D4 — `TestDiscoveryPropagatesDialectErrors` must assert the specific error.**
`err != nil` is not an assertion when a second error path can reach the same
line. Closes F7.

**D5 — source-scanning tests normalise line endings before scanning.** Strip
`\r` on read, in every test that treats Go source as text: `internal/ws/op_timeout_test.go`
and `internal/event/retention_test.go`. The tests exist to measure the *source*,
and the checkout's line endings are not part of the source. **Resolved 2026-09-07
(Q3): the CRLF case is synthesised at runtime** — the test copies the real source
into `t.TempDir()` with `\n` rewritten to `\r\n` and scans that, so the guard
cannot be normalised away by `.gitattributes` on a fresh checkout. The rejected
alternative was a committed fixture pinned with `-text`: durable, but it adds a
file whose only purpose is to violate the repository's own eol policy. Closes
F8, F9, F10.

**D8 — no meta-test polices the class.** **Resolved 2026-09-07 (Q4).** F4 and
F10 are the only known instances and both are fixed here. A denylist test
grepping `_test.go` for POSIX-only binary names, and a second CI lane running the
Windows unit tests under PowerShell, were both considered and rejected: the first
rots into an unmaintained list, the second duplicates a 30-minute job to catch a
class with two known members. If a third instance appears, that is the evidence
to revisit this — and it belongs in a new record, not an amendment here.

**D6 — do not renormalise the tree under this record.** It is a whole-tree diff
belonging to the CRLF work MADR 0118 defers. D5 makes the tests correct
regardless of how that is eventually settled. Closes F11.

**D7 — no test is skipped, weakened, or given a Windows-only exemption.** Every
assertion that runs on Linux today still runs on Windows after this change.

### Consequences

* Good: the gate's failure set becomes a statement about the code. A red line
  means something again.
* Good: F7's silent false pass becomes a real assertion, on every platform —
  this is the only change here that adds coverage rather than restoring it.
* Good: D5 removes a class of failure that depends on how a file was checked
  out, which is untestable in CI and therefore permanently invisible there.
* Neutral: the gate keeps PowerShell for host checks and gains a bash dependency
  for the test step. Git Bash is already required by CI's own Windows lane, so
  this adds no new tool to the contributor's box.
* Bad: D2 makes the gate depend on a bash on `PATH`. A Windows host without Git
  Bash gets a clear failure where it previously got a misleading one — better,
  but still a new prerequisite to document.
* Bad: this record fixes the measurement and explicitly leaves 1668 CRLF files
  in place (D6). Anyone reading `gofmt -l` locally still sees a wall of noise.

### Confirmation

```bash
# 1. The gate passes every check, A2 included. Run from a Windows host.
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1

# 2. The two named tests pass from BOTH shells, proving shell independence.
go test ./internal/provider/httpagent/ -run 'TestDiscovery' -count=1
go test ./internal/ws/ -run TestEveryAsyncDispatchedMethodIsInTheTable -count=1
#    then the identical two commands from PowerShell; both must be `ok`.

# 3. The vacuous assertion is gone: the specific dialect error is required.
grep -n 'session listing blew up' internal/provider/httpagent/provider_test.go
#    expect: an assertion comparing against this string, not just `err == nil`

# 4. No test reaches a Ready()-gated method via a PATH-resolved POSIX binary.
grep -rn 'Bin: *"false"' --include='*_test.go' internal/provider/httpagent/

# 5. The source scans survive CRLF. Prove it, do not assume it:
#    the test synthesises a CRLF copy of its input at runtime and scans that.
```

## Pros and Cons of the Options

### A — Fix each defect at its own root (chosen)

* Good, because it is the only option that closes F7, the silent false pass.
* Good, because after it the gate's red lines carry information.
* Good, because D5 makes the scans correct under either resolution of the
  tree-wide CRLF question, so it cannot be invalidated by MADR 0118's successor.
* Neutral, because it touches four files across three concerns; the phases are
  independent and can land separately.
* Bad, because it is more work than any single-root option, and three small
  fixes are easier to half-finish than one big one.

### B — Renormalise the tree to LF, leave the scans LF-only

* Good, because it fixes the *cause* of F8 rather than the symptom, and would
  also silence the `gofmt -l` noise that has been live since MADR 0118.
* Good, because the repo already declares the intent: `.gitattributes` says
  `* text=auto eol=lf`, so the tree is simply out of compliance with its own
  policy.
* **The strongest argument for it:** D5 makes the tests tolerate a state the
  repository has already declared invalid. Tolerating a policy violation is how
  the violation becomes permanent, and a reader five years out will find scans
  defending against an ending the project supposedly forbids.
* Bad, because it is a 1668-file diff that touches nearly every review, and it
  fixes neither F1 nor F4 nor F7 — the gate stays red and the false pass stays
  green.
* Bad, because it is the decision MADR 0118 explicitly deferred, and taking it
  here as a side effect of a test fix would bury a large decision in a small
  record.

### C — Skip the two tests on Windows

* Good, because it is a two-line change and the gate goes green immediately.
* Bad, because it is exactly the failure mode MADR 0118 D2 was written to
  prevent: a skip converts an environment problem into silent non-coverage.
* Bad, because F7 stays green-and-empty, and the skip would make F4's real
  content — that discovery forwards additive metadata untouched — untested on
  the platform where the transport is least exercised.

### D — Change only the gate to run under bash

* Good, because it is one line and closes F4 and F5 outright.
* Good, because it moves the gate toward its stated "mirrors CI" contract with
  minimal risk.
* Bad, because it leaves A2 (F1) red, the CRLF scan (F8) failing, and the false
  pass (F7) unexamined — the gate would still be a thing you have to explain
  rather than read.
* Bad, because making the gate match CI *hides* F4 rather than fixing it: the
  test would still measure `PATH`, and would still fail for anyone who runs
  `go test ./...` from PowerShell by hand.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Gate contract is "mirrors CI go-native windows/amd64 unit contract" | `scripts/ci-windows-local.ps1` `.SYNOPSIS`/`.DESCRIPTION` |
| Gate result: 2 checks / 6 tests before, 2 checks / 2 tests after | three runs of `scripts/ci-windows-local.ps1`, 2026-09-07 |
| A2 uses `New-Item -ItemType SymbolicLink` | `scripts/ci-windows-local.ps1:165` |
| Suite probes with `os.Symlink` | `internal/testexec/testexec.go:149` |
| PowerShell probe fails while Go probe succeeds, same host and minute | direct probe, 2026-09-07 (`Administrator privilege required` vs `go os.Symlink OK`) |
| `MC_REQUIRE_SYMLINK=1` set unconditionally, by design | `scripts/ci-windows-local.ps1:89`; MADR 0118 D4; `internal/testexec/testexec.go:139-141` |
| Developer Mode grants no token privilege | `whoami /priv` before and after reboot: no `SeCreateSymbolicLinkPrivilege` row |
| Reboot was required | boot `2026-09-07 11:14:23`; `os.Symlink` failed before it, succeeded after |
| httpagent test passes under Git Bash, fails under PowerShell | `go test ./internal/provider/httpagent/ -run TestDiscoveryForwardsToDialectWhenEngineIsUp` in each shell |
| `LookPath("false")` differs by shell | `exec.LookPath` probe: `C:\Program Files\Git\usr\bin\false.exe` vs not found |
| `false.exe` exists on this host | `ls -l "/c/Program Files/Git/usr/bin/false.exe"` — 33123 bytes, Feb 12 2026 |
| CI Windows unit lane uses bash | `.github/workflows/ci.yml:259`, `:327`; rationale at `:494` |
| Binary is never executed; only `Ready()` needs it | `internal/provider/httpagent/provider.go:160-163`, `:309`, `:326`; `withFakeEngine` at `provider_test.go:172-180` |
| 24 `Bin: "false"` call sites in the package | `grep -rn 'Bin: *"false"' --include='*.go'` |
| `TestDiscoveryPropagatesDialectErrors` asserts only `err != nil` | `internal/provider/httpagent/provider_test.go:227-245` |
| ws scan delimiter is LF-only | `internal/ws/op_timeout_test.go:118-124` |
| `caseLabel` regex is CRLF-fragile | `internal/ws/op_timeout_test.go:66` applied to `strings.Split(sw, "\n")` |
| Of the four files the ws test reads, only `codex_handlers.go` is CRLF | per-file endings check, 2026-09-07 |
| Same `"\n}\n"` idiom in the event package | `internal/event/retention_test.go:95` |
| 1668 tracked CRLF files; policy says LF | binary-safe scan over `git ls-files`; `.gitattributes:1` |
| ws test fails identically under both shells | run under Git Bash and PowerShell, same output |

### Related records

* **MADR 0118** — *Symlink-dependent tests probe for the privilege rather than
  assuming the platform.* Origin of `SkipIfNoSymlink` and `MC_REQUIRE_SYMLINK`
  (D2, D4). This record does not change that design; it fixes a probe that fails
  to implement it. 0118 also defers the tree-wide CRLF question, which D6 keeps
  deferred.
* **MADR 0145** — *Local CI-style Windows tests catch go-native failures before
  GitHub.* This record repairs the "mirrors CI" claim that 0145 makes and F5
  falsifies.
* **MADR 0143** — *CI retries a failed job once and records every retry.* Not a
  cause here, but the reason the local gate matters: CI's retry can mask a
  genuine flake once, and the ledger's first and only row so far
  (`TestACPConnectionSurvivesAStalledPump`, run 34139293426) came from the same
  Windows lane.
* **MADR 0116** — *windows/amd64 and linux/arm64 build targets.* Source of the
  PATHEXT/executable-bit platform layer that `launch.Resolve` sits on (D24).

### Open questions for the plan

All four were put to the decision-maker on 2026-09-07 and answered before the
plan was written; the resolutions are folded into D2, D3, D5 and D8 above rather
than left here, so the decisions section is the single place a reader must look.

| # | Question | Resolution |
| --- | --- | --- |
| Q1 | What replaces `false`? | `os.Executable()`; `Config.Bin` is already the seam (D3) |
| Q2 | How does the gate match CI's shell? | bash for the test step only; PowerShell keeps the host checks (D2) |
| Q3 | How is CRLF tolerance proven? | synthesised at runtime into `t.TempDir()` (D5) |
| Q4 | Guard the class with a meta-test? | No — fix the two instances (D8) |

Remaining for the plan to settle, being mechanical rather than directional:

1. **Which of the 24 `Bin: "false"` call sites change?** Only those reaching a
   `Ready()`-gated method can fail (F6), but leaving the rest inconsistent is its
   own trap for the next reader. The plan must state the rule it applies and
   apply it uniformly.
2. **Does `TestStartServerBailsWhenEngineExitsImmediately` keep its skip-guard?**
   It is the one test that genuinely *executes* `false` and depends on its
   non-zero exit, so D3 does not reach it and its `exec.LookPath` skip at
   `provider_test.go:33` stays correct. The plan should say so explicitly, so the
   surviving `false` reference is not mistaken for a missed edit.

   **Answered wrongly. See the 2026-09-07 amendment below** — the answer was
   self-consistent but contradicted acceptance criterion A5, and executing P4
   is what exposed it.

## Amendment — 2026-09-07: D3 extends to the last PATH-resolved binary

Executing P4 produced the skip census A5 asks for, and it does not match:

| Shell | Skips in `internal/provider/httpagent` |
| --- | --- |
| Git Bash | `TestNormalizeInstanceKey` |
| PowerShell | `TestNormalizeInstanceKey`, `TestStartServerBailsWhenEngineExitsImmediately` |

**F13 — the record contradicted itself, and only execution revealed it.** D3
and C2 deliberately exempted `TestStartServerBailsWhenEngineExitsImmediately`,
while A5 requires an identical pass/skip set in both shells and C4 requires
every assertion that runs on Linux to run on Windows. Both cannot hold. The
exemption was not a regression — the `exec.LookPath` skip predates this record
— but it means the record shipped a criterion it had already decided to
violate, and open question 2 above answered the narrow question ("is the
surviving `false` an oversight?") without noticing the wider one.

This is worth stating plainly because it is a failure of the record, not of the
code: the contradiction was present when the MADR was written and survived
review, and it took running the census to see it.

**D9 — the last `false` goes too.** `TestStartServerBailsWhenEngineExitsImmediately`
must supply its own immediately-exiting process rather than resolve one from
`PATH`, using Go's standard helper-process idiom: re-exec `os.Args[0]` with
`-test.run` pointed at a helper test that exits non-zero, gated by an
environment variable so it is inert in a normal run. Its `exec.LookPath` skip
is removed with it — there is nothing left to look up. Closes F13, and makes
A5 true as written rather than true-with-an-asterisk.

The alternative considered and rejected was narrowing A5 to permit the
documented exception. It is cheaper and would have been defensible, but this
record's entire subject is tests that measure their environment instead of
their subject; stopping at 23 of 24 and writing the exception into the
acceptance criteria would leave the one remaining instance blessed by the
document that exists to remove them. The rejected option's strongest argument
stands, and is recorded here rather than dismissed: the helper-process idiom is
more machinery than a one-line skip, and machinery in a test is itself a place
bugs hide.

**Consequence.** The package gains a small exported-to-nobody helper test that
does nothing outside its env guard, and the assertion it protects — that a
dying engine fails startup promptly rather than spinning `serverStartTimeout` —
now runs on every platform and every shell, which it previously did not.

## Amendment — 2026-09-07 (second): D8 is reversed, on its own terms

**F14 — the third instance appeared, from the census D9 required.** Running the
whole-suite skip comparison A5 asks for, after every phase had landed:

| Shell | Skips across `go test ./...` |
| --- | --- |
| Git Bash | 119 |
| PowerShell | 120 |

The difference is `TestHandleWSErrorKillsEngine`
(`internal/provider/acphttp/provider_test.go:59`), which calls
`exec.Command("sleep", "60")`. `sleep` is a POSIX binary resolved from `PATH`:
present under Git Bash, absent under PowerShell, where `cmd.Start()` fails and
the test takes its `t.Skipf`. It has therefore never failed anywhere — CI runs
bash and sees it pass, the gate ran PowerShell and saw it skip — while quietly
not testing that `handleWSError` kills the engine. Same class as F4, same
silent shape as F7.

D8 rejected a class guard and named its own falsification: *"If a third
instance appears, that is the evidence to revisit this."* It appeared within
the same session, produced by the very verification the amendment added. A rule
that states a falsifiable condition and is then ignored when the condition
fires is worse than no rule, so D8 is reversed.

**What changed the answer is not only the count.** D8's objection was specific
and correct: a denylist of POSIX binary names *does* rot. The inventory taken
for this amendment shows the guard does not need to be a denylist. Every
`exec.Command`/`exec.LookPath` on a bare string literal across all `_test.go`
files falls into four groups:

| Group | Example | Legitimate? |
| --- | --- | --- |
| Live-tagged CLI tests (`live_*_test.go`) | `exec.LookPath("codex")` | Yes — resolving the real CLI is their purpose |
| Platform-specific files (`*_windows_test.go`) | `exec.Command("cmd.exe")` | Yes — the file only builds there |
| The Go toolchain | `exec.LookPath("go")` | Yes — `go test` cannot run without it |
| Everything else | `exec.Command("sleep", "60")` | **No** — this is the class |

The first two exemptions are *structural* — a build tag and a filename suffix,
both of which a new file carries automatically — and the third is a single
justified name. That is a rule about the shape of a call in a context, not a
list of programs somebody must remember to extend.

**D10 — fix the third instance.** `TestHandleWSErrorKillsEngine` must spawn a
process that lives without resolving one from `PATH`, by the same
helper-process idiom as D9. Closes F14.

**D11 — add the class guard, structurally scoped.** A test asserts that no
`_test.go` file which builds on every platform in the default build calls
`exec.Command`, `exec.CommandContext` or `exec.LookPath` with a bare string
literal, excepting `go`. Files carrying a `live_*` build constraint and files
with a platform filename suffix are out of its scope by construction, not by
enumeration. This supersedes D8.

**What would falsify D11 in turn.** If the guard ever has to grow a third
hand-maintained name, D8's original objection has won and the guard should be
deleted rather than extended — record that, so the next person has the same
falsifiable condition D8 gave and this amendment honoured.

## Amendment — 2026-09-07 (third): Option B's cost was wrong

Option B above ("Renormalise the tree to LF, leave the scans LF-only") is
rejected against a stated cost of "a 1668-file diff that touches nearly every
review". **That cost is not real, and the sentence should not be trusted.**

Measured under MADR 0148: `git add --renormalize .` stages **zero** changes.
Every blob in `HEAD` is already LF; the CRLF exists only in the working tree,
which git treats as clean. There is no diff, no commit, and no review burden —
the refresh is a local operation with an empty `git status` on both sides of it.
It was executed on 2026-09-07 and took the census from 1069 files to 0 without
producing a commit.

**The rejection still stands, for the reasons that were true.** Option B fixes
neither F1 (the A2 probe), nor F4 (the shell-dependent `false`), nor F7 (the
silent false pass), and D6's judgement — that the tree-wide question belongs in
its own record rather than buried in a test fix — was right; that record is now
0148. Only the cost estimate was wrong.

The original argument is left unedited above, because what a record believed at
decision time is the thing it exists to preserve. This note is how a reader who
reaches that sentence learns not to rely on it.

**D5 of this record is unaffected.** 0148 D3 keeps the read-time normalisation
deliberately: it protects any contributor whose checkout is stale the way this
one was, and a locally-clean tree is not a reason to remove it. See 0148 C2,
which names that removal as the contract most at risk.
