---
status: proposed
date: 2026-10-14
associated-madr: "0193-MADR-warm-fresh-test-executables-before-timing-them.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement warming a test's fresh executables before timing them

Associated MADR: [0193-MADR-warm-fresh-test-executables-before-timing-them.md](0193-MADR-warm-fresh-test-executables-before-timing-them.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"Run every fixture once before the test times it, in the shared helpers, and route every run fixture
through them" (D1–D7).

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
   * Run the stub with `exec.Command(path)` and `cmd.Env = append(os.Environ(),
     "MCREMOTE_STUB_WARM="+marker)`, where `marker` is under `t.TempDir()`.
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
   * Plant a probe per call (bypass the `refreshing` check) on a scratch copy, run under the W6 load,
     and show it caught 10 of 10.
   * With today's immediate read, run the same plant under load and record how often it passes, as
     the before figure.

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
