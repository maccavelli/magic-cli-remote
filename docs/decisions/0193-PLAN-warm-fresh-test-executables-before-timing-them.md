---
status: complete
date: 2026-10-15
associated-madr: "0193-MADR-warm-fresh-test-executables-before-timing-them.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement warming a test's fresh executables before timing them

Associated MADR: [0193-MADR-warm-fresh-test-executables-before-timing-them.md](0193-MADR-warm-fresh-test-executables-before-timing-them.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-15 the owner accepted the
recommendations and approved the PLAN ("accept recommendations and proceed"). That covers W1–W7, and
writing the 0137 F1 lock bug up as a proposed 0137 amendment (W7 step 3); fixing it needs its own
approval. This PLAN implements the MADR's chosen option, "Run every fixture once before the test
times it, in the shared helpers, and route every run fixture through them" (D1–D7).

* A phase starts only after the owner approves it by name.
* Each phase ends with `make pre-add-check` on its staged Go files and `go test -race` on the
  packages it touched. W6 runs the full gates.
* The owner commits (`git commit --no-edit`); an agent stages only.
* No file under `cmd/` or `internal/` that is not a `_test.go` file changes, except
  `internal/testexec/testexec.go`. `testexec` is imported only by tests.

## Goal

A test that runs an executable it has just written times only the run it means to, on every
platform. Its negative checks see what they watch. The reproduction that fails today passes, and
0137's breakages are still caught on macOS under load.

## Scope

* `internal/testexec/testexec.go` and a new `testexec_test.go`: the warm-up in `WriteShellStub` (D1),
  `WarmExecutable` (D2), and their unit tests.
* The twelve test files that write run fixtures themselves (D3):

  | File | Helper or test | Mode today |
  | --- | --- | --- |
  | `internal/provider/codex/auth_test.go` | `fakeCodex` (`:45`) | `0o700` |
  | `internal/provider/codex/device_auth_test.go` | `fakeDeviceCodex` (`:29`) | `0o700` |
  | `internal/provider/codex/sandbox_health_test.go` | `TestProbeSandboxHealth_FakeBinOK` (`:111`), `TestProbeSandboxHealth_FakeBinDenied` (`:131`) | `0o755` |
  | `internal/provider/codex/apikey_test.go` | `fakeCodexAPIKey` (`:22`) | `0o700` |
  | `internal/provider/codex/logout_test.go` | `fakeCodexLogout` (`:22`) | `0o700` |
  | `internal/provider/codex/status_truth_test.go` | `TestStatusDoesNotSpawnAProcessPerCall` (`:79`) | `0o700` |
  | `internal/provider/codex/store_reality_test.go` | `fakeDoctorBin` (`:28`), `fakeGarbageDoctorBin` (`:57`) | `0o700` |
  | `internal/provider/codex/owned_device_auth_test.go` | `fakeCodexLogin` (`:23`) | `0o700` |
  | `internal/provider/grok/owned_device_auth_test.go` | `fakeGrokLogin` (`:22`) | `0o700` |
  | `internal/providerauth/cli_test.go` | `fakeCLI` (`:23`) | `0o700` |
  | `internal/providerauth/ownedflow_test.go` | `fakeLoginScript` (`:22`) | `0o700` |
  | `internal/cli/service/installed_test.go` | `stubBinary` (`:105`) | `0o755` |

* `internal/ws/receipt_e2e_test.go`: warm the built `mcremote` before its 15 s run (D2).
* `internal/provider/codex/status_truth_test.go`: wait for background probes before counting (D4).
* A new guard test in `internal/testexec` (D5).
* `internal/procutil/spawnsites_test.go`: `testexec.go` in the A17 allowlist (added by Deviation 3).
* `scripts/gatekeeper_load.py` (D7).
* Cross-links in the 0137, 0189 and 0192 PLANs.
* **Out of scope:**
  * any bound in a test or in Magic (D6);
  * the 0137 F1 lock bug in `store_reality.go` (MADR More Information). It needs its own approval, as
    an amendment to 0137;
  * the `cli/service` fixtures that are never run;
  * the re-execs of the test binary itself (`os.Args[0]`, `os.Executable()`), which `go test` has
    already run once.

## Implementation Steps

### W1. `testexec`: the warm-up (D1, D2)

1. Write `internal/testexec/testexec_test.go` first. Show each test failing against today's
   `WriteShellStub`:
   * `TestWriteShellStubWarmsWithoutRunningTheBody`: a body that appends to a marker. After
     `WriteShellStub` returns, the marker does not exist, and the file named by the warm-up variable
     was created. (Today: no warm-up file.)
   * `TestWriteShellStubLeavesTheStubRunnable`: running the returned stub with arguments, and without
     the variable, reaches the body with those arguments and exits with the body's status.
   * `TestWarmExecutableRunsTheBinary`: `WarmExecutable` on a stub returns, and on a non-executable
     file fails the test. Use a fake `testing.TB` that records `Fatalf`, so the failure is observed
     rather than fatal.
   * `TestWarmUpVariableDoesNotLeak`: after `WriteShellStub`, `os.Getenv("MCREMOTE_STUB_WARM")` is
     empty.
2. Implement D1 in `WriteShellStub`:
   * Write `"#!/bin/sh\n" + guard + body + "\n"` at `0o700`, with the guard
     `[ -n "$MCREMOTE_STUB_WARM" ] && { : > "$MCREMOTE_STUB_WARM"; exit 0; }`.
   * Run the stub with `exec.Command(path)` (Deviations 2 and 3: `testexec.go` is allowlisted in the
     A17 guard) and `cmd.Env = append(os.Environ(), "MCREMOTE_STUB_WARM="+marker)`, where `marker`
     is under `t.TempDir()`.
   * `t.Fatalf` with the combined output if the run fails or the marker is missing.
   * The doc comment says why: macOS holds a new file's first run for a Gatekeeper scan (MADR 0193).
3. Implement D2: `WarmExecutable(t testing.TB, path string, args ...string)` runs the binary once and
   `t.Fatalf`s with the output on failure.
4. **Planted breaks, each on a scratch copy.** Each must fail W1's tests:
   * the guard removed;
   * the warm-up run removed;
   * the body placed before the guard;
   * the variable set with `os.Setenv` instead of on the command.

### W2. Route the run fixtures through the helper (D3)

1. For each row of the Scope table, replace the local `os.WriteFile` of a `#!/bin/sh` script with
   `testexec.WriteShellStub(t, path, body)`. The body loses its own `#!/bin/sh` line.
   * Each helper keeps its signature, return value and path.
   * Heredocs (`fakeCLI`, `stubBinary`) and argument checks (`fakeGarbageDoctorBin`'s `$1`) move into
     the body unchanged.
2. **Modes.** Three sites write `0o755` today. Before changing them, read each test for an assertion
   on the mode, or code that checks more than the owner's execute bit.
   * If none, `0o700` is fine.
   * If one exists, stop: record a deviation, and offer a mode parameter on the helper.
3. A helper that skips on Windows today keeps that skip, now through `WriteShellStub`.
4. After each package, `go test -race` on it, and compare its test count with the count before the
   change.

### W3. The freshly built binary (D2)

1. In `internal/ws/receipt_e2e_test.go`, call `testexec.WarmExecutable(t, binPath, "version")` after
   the build, and before `runCtx` is created.
2. Confirm `mcremote version` exits 0 without a data directory, on macOS, Linux and Windows. If it
   does not on one of them, stop and record a deviation.

### W4. Negative checks (D4)

1. **The prewarm window.** No code change. W6 proves that breakage O is caught 10 of 10 under load.
2. **`TestStatusDoesNotSpawnAProcessPerCall`.**
   * After the five `AuthStatus` calls, wait until no background refresh is in flight, then count.
     Poll `realityCache.refreshing` under `realityCache.mu`; the test is in package `codex`. Bound
     the wait at 30 s, failing with "a background probe never finished".
   * ~~Plant a probe per call (bypass the `refreshing` check) on a scratch copy, run under the W6
     load, and show it caught 10 of 10.~~ (Deviation 1: that break does not exercise this test.)
   * ~~With today's immediate read, run the same plant under load and record how often it passes, as
     the before figure.~~
   * Plant, on scratch copies, under the W6 load, 10 runs each, against today's test and the new one:
     a forced background probe on every call; and a spawn on every call that bypasses the cache,
     both with and without a simulated 0137 F1 fix. The new test must catch all of them 10 of 10.

### W5. The guard test (D5)

1. Add `TestRunFixturesAreWarmed` in `internal/testexec` (a `_test.go` file). It walks `../../internal`
   and `../../cmd` for `_test.go` files that contain `#!/bin/sh` outside `internal/testexec`.
2. Its allowlist names the never-run `cli/service` fixtures, with the reason (`Setup` checks the
   executable bit and renders the path; it never runs the binary):
   * `launchd_sequence_test.go`;
   * `setup_launchd_test.go`;
   * `refresh_test.go`;
   * `setup_test.go`.
3. A failure names the file and line, and says to use `testexec.WriteShellStub`.
4. On a scratch copy, plant a direct `os.WriteFile` of a `#!/bin/sh` script in a non-allowlisted
   test file, and show the guard failing.

### W6. The load generator and the verification (D7)

1. Add `scripts/gatekeeper_load.py` (stdlib only):
   * `--workers N --seconds S`: write and run new `#!/bin/sh` scripts in a loop, from N threads, for
     S seconds, under one temporary directory it removes at the end.
   * Print the number of runs and the rate.
   * A module docstring says what it is for and that it only loads macOS (MADR 0193 E9).
   * The repository's Python ignore rules already cover `scripts/`. Confirm that with
     `git check-ignore`.
2. **The reproduction on the macOS host.** Keep the raw `-json` output of every run under the
   scratch directory. Use `-timeout=30m`, because `go test`'s 10-minute package limit is what ended
   E11's runs.
   * Without load:
     `go test -race -count=3 -timeout=30m ./internal/testexec/ ./internal/provider/acpagent/ ./internal/providerauth/ ./internal/provider/codex/ ./internal/provider/grok/ ./internal/cli/service/ ./internal/daemon/ ./internal/ws/`.
     Want: all pass.
   * The same under `--workers 12`, then under `--workers 40`. Want: no test fails.
   * Before W2–W4, the 12-worker run fails as E7 records. That is the before figure, kept with the
     after.
3. **Breakages, on scratch copies, under `--workers 12`, 10 runs each:**
   * 0137 M (re-arm removed) and N (re-arm on the happy path only): want 10 of 10 caught.
   * 0137 O (arm at turn start): want 10 of 10 caught with the ordering message.
   * 0137 S (the non-blocking probe made synchronous) and S2 (the coalescing flag removed): want 10
     of 10 caught.
   * The same O run on the Linux test host. Want 10 of 10 caught.
4. **Full suites.** `go test -race ./...` twice under `--workers 12`, with raw output kept. Want: no
   failure in the exposed tests. Record any other failure with its output, as a finding.
5. **The gates.**
   * `make pre-add-check` on every changed Go file; `make lint`.
   * `make preflight`.
   * `make ci-windows` on the Windows test host. `testexec`'s warm-up skips there, and
     `WarmExecutable` runs `mcremote.exe version`.
   * `python3 scripts/check_records.py --check-all`; `make markdownlint-docs`.

### W7. Records

1. Add a dated line to the 0189 PLAN's and the 0192 PLAN's flake notes, pointing at 0193.
2. Add a dated note to the 0137 PLAN's breakage list: under load on macOS, breakage O passed 8 of 10
   until 0193.
3. Present the 0137 F1 lock bug to the owner as a proposed 0137 amendment, with the scratch proof
   from 0193. Do not fix it here.

## Verification

* Every new test is seen failing first. Every planted break in W1, W4, W5 and W6 is shown failing on
  a scratch copy, never in the tree.
* The W6 reproduction passes under 12 and 40 workers, and its before figures are kept beside the
  after figures.
* 0137's breakages M, N, O, S and S2 are caught 10 of 10 under load on macOS. O is also caught on the
  Linux test host.
* `go test -race ./...`, `make pre-add-check`, `make lint`, `make preflight`, `make ci-windows`,
  `check_records.py --check-all` and `make markdownlint-docs` pass.
* The PLAN's execution record keeps the numbers, not a summary of them.

## Rollout and Rollback

* Test-only. Magic's binaries do not change: `testexec` is imported only from `_test.go` files.
* On Linux and Windows CI, each stub runs once more, which costs milliseconds. Windows skips the
  shell stubs as before.
* Rollback is reverting the commits. The flakes and E8's missed breakage return with them.

## Execution record

### Deviation 1, 2026-10-15: what the status test's wait protects against

* **Found** in W4, on scratch copies under 12-worker load, 10 runs each. The table is in MADR
  Amendment 1.
  * W4's planted break (the `refreshing` check bypassed) passed both today's test and the new one.
    With a warm stub the first probe finishes before the second `AuthStatus` call, so the cache
    answers the rest. That break belongs to `TestNonBlockingProbeCoalescesConcurrentCallers`
    (0137 S2).
  * Today's test catches a probe per call, and a cache-bypassing spawn per call, 10 of 10. That is
    because the 0137 F1 lock makes each call wait behind the running probe. The MADR's table row said
    otherwise.
  * With a simulated F1 fix (the lock held only around the cache), today's test passed the
    cache-bypassing spawn 10 of 10, falsely. The new test caught it 10 of 10.
* **Decision.** The owner chose "keep and amend": keep the wait, amend the MADR (Amendment 1, D4,
  the table row), and replace W4's planted break.
* **Scope.** No file joins it.

### Deviation 2, 2026-10-15: `testexec` spawns through `procutil.Command`

* **Found** by `make ci-windows`, which failed (`ci-windows-exit=2`) in
  `TestNoBareSpawnOutsideProcutil` (`internal/procutil/spawnsites_test.go:110`, acceptance criterion
  A17, MADR 0159): `2 site(s) start a child process without procutil.Command:
  internal/testexec/testexec.go:87 and :103`.
  * The guard applies to every non-test file, and `testexec.go` is one, so it fails on every
    platform. W1's own runs did not include `internal/procutil`.
  * W1 step 2 as written said `exec.Command(path)`.
* ~~**Decision.** The owner chose `procutil.Command(context.Background(), …)` over adding
  `testexec.go` to the guard's allowlist, whose comment rejects exemptions of that kind. There is no
  import cycle: every `procutil` test is in the external `procutil_test` package, and none imports
  `testexec`.~~ Superseded by Deviation 3: that check was incomplete, and there is a cycle.
* ~~**Scope.** No file joins it.~~
* ~~W1 step 2: `exec.Command(path)`.~~ ~~W1 step 2 and step 3 spawn through `procutil.Command`.~~

### Deviation 3, 2026-10-15: `testexec` cannot import `procutil`; it is allowlisted instead

* **Found** by `make ci-windows` after Deviation 2's change. `go vet ./...` (A5) and
  `go test ./...` (A6) failed: `internal/fsutil imports internal/testexec from atomic_test.go
  imports internal/procutil from testexec.go imports internal/fsutil from registry.go: import cycle
  not allowed in test`.
  * `procutil` depends on `appdirs` and `fsutil` (`go list -deps ./internal/procutil`).
    `fsutil/atomic_test.go` is an in-package test that uses `testexec`'s skip helpers. The cycle is
    on every platform.
  * Deviation 2's "no import cycle" check looked only at `procutil`'s own tests, and the local check
    vetted two packages rather than `./...`.
* **Decision.** Of three resolutions, the owner chose to revert to `exec.Command` and allowlist
  `internal/testexec/testexec.go` in the A17 guard. The alternatives were a new package for the
  spawning helpers, and converting `fsutil`'s test to an external package.
  * The guard's own text admits a site that "truly cannot use it", with the reason. The reason:
    `testexec` is test-only, imported only from `_test.go` files and never linked into the daemon,
    and it cannot import `procutil` without the cycle.
  * That parallels the existing entry for `codesign_darwin.go`.
* **Scope.** `internal/procutil/spawnsites_test.go` joins it, for the allowlist entry.
* W1 step 2 is as first written: `exec.Command(path)`. The guard's earlier failure on the two sites
  (Deviation 2) is the evidence that the entry is needed.
* **Re-verified:** `go vet ./...` clean; `testexec`, `procutil`, `fsutil` and `appdirs` pass with
  `-race`; W1's four planted breaks fail as before.

### W1–W4, 2026-10-15: complete (staged, not committed)

**Approval:** the owner on 2026-10-15, "accept recommendations and proceed".

**W1, `testexec`.**

* `testexec_test.go` has six tests. Against today's helper they did not build. Against the bare
  structure (`writeAndWarm` writing only, `WarmExecutable` empty), three **failed**:
  * `writeAndWarm failure = "", want it to mention "did not create"`;
  * `… "exit status 3"`;
  * `WarmExecutable accepted a file it cannot run`.

  The other three passed trivially, because with no warm-up nothing can reach the body or leak.
  The planted breaks below give them their meaning.
* `WriteShellStub` now writes the guard and calls `writeAndWarm`. That runs the stub with
  `MCREMOTE_STUB_WARM` on the command only, and fails the test unless the run succeeds and creates
  the file. `WarmExecutable` runs a binary once.
* **Planted breaks, each on a scratch copy.** All four failed:

  | Plant | Failure |
  | --- | --- |
  | the guard removed | "the run did not create … so it did not reach the warm-up guard", in four tests |
  | the warm-up run removed | `writeAndWarm failure = ""` in both refusal tests |
  | the body placed before the guard | "the warm-up reached the stub's body", and three others |
  | the variable set with `os.Setenv` | "MCREMOTE_STUB_WARM=… leaked into the test process", and the stub skipping its body |

* `make pre-add-check` on both files: clean. `go test -race` on `testexec`, `acpagent`, `codex` and
  `daemon`: passed.

**W2, routing.**

* The twelve files in the Scope table now call `testexec.WriteShellStub`. Their scripts lost their
  own `#!/bin/sh` line, and two unused `os` imports went.
* **Modes:** no test, and no code it calls, depends on `0o755` at the three sites that used it.
  `installed_test.go` is `//go:build unix`.
* **One change on Windows:** `TestStatusDoesNotSpawnAProcessPerCall` had no Windows skip. Its fixture
  cannot run there, so it passed without testing anything. It now skips, like every other fixture
  (MADR 0116 D16).
* **Test counts,** before and after, identical: `cli/service` 151 pass; `codex` 509 pass, 1 skip;
  `grok` 71 pass; `providerauth` 177 pass, 1 skip.

**W3, the built binary.**

* `mcremote version` exits 0 in an empty home and writes nothing there.
* `receipt_e2e_test.go` calls `testexec.WarmExecutable(t, binPath, "version")` before its 15 s run.
  `TestReceiptEndToEndOverRealWS` passed in 3.24 s.

**W4, negative checks.**

* `TestStatusDoesNotSpawnAProcessPerCall` counts through `settledCallCount`. That waits until no
  refresh is flagged and the count has held for 300 ms, bounded at 30 s. It passed 3 of 3 with
  `-race`.
* Planted breaks: see Deviation 1 and MADR Amendment 1. Unplanted, under load, it passed 10 of 10.

### W5–W7 and verification, 2026-10-15: complete (staged, not committed)

**W5, the guard.**

* `TestRunFixturesAreWarmed` (`internal/testexec/runfixtures_test.go`) passes on the tree: 541
  test files scanned, 4 allowlisted. It also fails on an allowlist entry whose file no longer writes
  a script, so the list cannot rot.
* **Planted breaks, each on a scratch copy.** All three failed:

  | Plant | Failure |
  | --- | --- |
  | a hand-written `#!/bin/sh` write in `codex/auth_test.go` | "internal/provider/codex/auth_test.go:210 writes a #!/bin/sh script by hand" |
  | `refresh_test.go` no longer writes a script | "internal/cli/service/refresh_test.go is in neverRunFixtures but writes no #!/bin/sh script" |
  | the guard against the test files as they were before W2 | 14 violations in 12 files: exactly the twelve in the Scope table |

**W6, the load generator.**

* `scripts/gatekeeper_load.py`: `--workers`, `--seconds` (0 runs until interrupted), stops on
  SIGINT or SIGTERM, cleans up its directory, and rejects `--workers 0`. `__pycache__/` is already
  ignored (`.gitignore:37`).

**W6, verification on the macOS host.** Raw `-json` output of every run was kept in the session
scratch directory. A second session's `go test` runs were active on the host throughout, adding
uncontrolled load (recorded at start and end).

| Run | Result |
| --- | --- |
| eight packages, `-count=3 -timeout=30m`, no load | 3,144 passed, 0 failed (107 s) |
| the same, 12 workers | 3,144 passed, 0 failed (289 s; again after Deviation 3: 317 s) |
| the same, 40 workers | 3,144 passed, 0 failed (752 s) |
| full `go test -race ./...`, 12 workers, twice (after Deviation 3) | both passed (147 s, 110 s) |
| full `go test -race ./...`, no load (after Deviation 3) | passed (58 s) |

Before 0193, the same 12-worker run failed both prewarm tests 2 of 3 (MADR E7). The 40-worker run
failed the two `codex` probe tests 5 of 5 (MADR E11).

**0137's breakages, on scratch copies, 12 workers, 10 runs each.** Each fails exactly the tests 0137
recorded for it, and no others:

| Breakage | Result |
| --- | --- |
| M, re-arm removed | both prewarm tests failed 10/10 (`wanted 1 "prewarm failed", got 0`) |
| N, re-arm on the happy path only | `TestAFailedTurnStillRearmsTheSpare` failed 10/10; the other passed |
| O, arm at turn start | `TestSpareIsRearmedAtTurnEndNotTurnStart` failed 10/10, with "the spare was armed 1 time(s) while the turn was still running" |
| S, the non-blocking probe made synchronous | `TestNonBlockingProbeRunsNoBinaryOnAColdCache` failed 10/10 ("the binary ran before the call returned") |
| S2, the coalescing flag removed | `TestNonBlockingProbeCoalescesConcurrentCallers` failed 10/10 ("20 concurrent callers started 3–10 probes") |

Before 0193, O passed 8 of 10 on this host (MADR E8).

**The Linux test host,** 12 workers, 10 runs, against the final code: O was caught 10 of 10, and the
tests passed 10 of 10 without it. The remote copy was removed.

**Gates.**

* `make pre-add-check` on the 17 changed Go files: clean. `go vet ./...`: clean. `make lint`: clean.
* `make preflight`: markdownlint 0 issues; `dart format` (`Formatted 219 files (0 changed)`);
  `flutter analyze` (`No issues found!`); `flutter test` (`+1473 ~3: All tests passed!`).
* `make ci-windows` on the Windows test host:
  * Run 1: `ci-windows-exit=2`, Deviation 2.
  * Run 2: `ci-windows-exit=2`, the import cycle, Deviation 3.
  * Run 3: `ci-windows-exit=0`, with A5 `go vet ./...` and A6 `go test ./...` passing, and
    `testexec`, `procutil`, `fsutil` and the routed packages all `ok`.
* `check_records.py --check-all` and `make markdownlint-docs`: clean.

**W7, records.**

* The 0189 and 0192 PLANs' flake notes point at 0193.
* The 0137 PLAN's breakage list notes that O passed 8 of 10 on macOS under load until 0193.
* The 0137 F1 lock bug is written up in the 0137 MADR as "Amendment, 2026-10-15 (proposed)": the
  defect, the scratch proof (1.73 s, 3 of 3), a proposed fix and its confirmation. It is not
  implemented. It waits for the owner's approval.

**Not done, by scope.**

* The 0137 F1 fix (above).
* The never-run `cli/service` fixtures, which are allowlisted in W5.
* `providerauth`'s flake was never reproduced on demand (MADR More Information). Its fixtures are
  warmed like the rest, and the 12- and 40-worker runs above passed with them.
