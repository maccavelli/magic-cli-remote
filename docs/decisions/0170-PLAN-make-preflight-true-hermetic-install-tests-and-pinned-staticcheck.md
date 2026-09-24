---
status: in-progress
date: 2026-09-24
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0170 — Make `make preflight` true: hermetic install tests, a degraded-manager fix, pinned staticcheck, and CI parity

Implements [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md)
decisions D1–D6, closing findings F1–F12.

This plan was written on 2026-09-24 as phase P10 of PLAN 0169 and executed under that name. The
owner moved it here the same day. The commits cite it as P10 of 0169; the numbering map is in
the MADR.

## Goal

- `bash scripts/install-binary_test.sh` and `sh scripts/install_test.sh` pass on macOS, WSL
  and the Linux server. They cannot reach a real service manager, and they cover
  `install-binary.sh`'s Linux branch, a degraded manager included.
- `make install` stops and restarts the service on a `degraded` user manager.
- `make staticcheck` runs the pinned v0.8.1 for three GOOS and reports 0 findings, with nothing
  suppressed.
- `mcremote doctor` names the codex store reality, and explains one mcremote cannot protect.
- CI's `go` job runs the same three gates, and `make preflight` is green on the Linux server.

## Scope

### In scope (the only files any phase may touch)

- `scripts/install-binary.sh`, `scripts/install-binary_test.sh`, `scripts/install_test.sh`
- `Makefile`: `STATICCHECK_VERSION`, the tool rule, the `staticcheck` target and preflight's
  staticcheck line
- `.github/workflows/ci.yml`: three steps in the `go` job
- `internal/provider/codex/{execution,managed_daemon,projects,runtime,session,threads,transport,ws_auth,store_reality}.go`,
  and `reality_host_test.go` (the renamed function's only caller)
- `internal/ws/codex_handlers.go`, `internal/providerauth/store.go`,
  `internal/provider/launch/{launch,launch_windows}.go`, `internal/appdirs/security_windows.go`
- `internal/provider/acpagent/rewind_test.go`, `internal/receipt/jws_test.go`
- `internal/cli/doctor.go`, `internal/cli/doctor_test.go`
- the code comments in the files above that cite "MADR 0169" for these decisions, repointed
  to 0170 when the record moved
- this pair, and the pointer left in the 0169 pair
- on the Linux server, a fast-forward of its `magic-cli-remote` checkout, so preflight runs the
  fixed tree

### Out of scope

- **`install.sh`'s container detection**, which reads host files with no test seam (see
  Deferred).
- **Publishing.** Pushing to GitHub is blocked by the disclosure guard, until MADR/PLAN 0171
  redacts the tree.

## Stability rule

Every phase ends with gofmt clean; build and vet for windows, linux and darwin;
`go test -race ./...` and `CGO_ENABLED=0 go test ./...`; and `scripts/go-precheck.sh` over the
changed Go files. Commit with `git commit --no-edit`. Push and workflow dispatch only on an
explicit ask.

## Cross-cutting contracts

- **C1 — No test acts on a real service.** The install suites run with stub-only PATHs, and
  the live unit's start time is checked across every run on the Linux server.
- **C2 — Nothing is suppressed.** No `staticcheck.conf`, no `//lint:ignore`, and no check
  removed from preflight or CI.
- **C3 — Every new check is seen failing before it is trusted,** against a copy or an overlay,
  never by dirtying the tree.

C1 is the one most at risk. A test that "just calls `systemctl`" to verify something is an
easy edit to make later; the guard exists to refuse it.

## Dependency and delivery order

P1 → P2 → P3 → P4 → P5. P3 was committed before P2's findings commit (see its execution note).

## Implementation Steps

### P1 — Hermetic install tests and the degraded-manager fix (D1, D2; closes F1, F2, F3, F5, F12)

1. `install-binary_test.sh`: stub-only PATH per run; stub `systemctl` (manager state and unit
   state from files) and `launchctl`; fake `XDG_RUNTIME_DIR`; the hermetic guard; Linux cases
   L1–L4 next to the darwin cases D1–D2.
2. `install_test.sh`: a non-WSL `MC_TEST_OSRELEASE` for every case that does not name its own.
3. Seen to fail, then fix D1 in `install-binary.sh`.

**Verification:** the suites on WSL, the Linux server and macOS. The guard, with the real
service manager put on the run PATH in a scratch copy, must exit 2. The transient-unit probe on
the degraded WSL manager must show the unit stopped.

**Executed 2026-09-24, commit `919204b`.**
- **Seen to fail before the fix** (WSL, degraded):
  - L2 failed with F3's signature: one `is-system-running` call, then "Installed", no stop, no
    start.
  - The guard exited 2 with `/usr/bin/systemctl` on the run PATH, and on macOS with
    `/bin/launchctl`.
  - `install_test.sh` had failed 3/139 on WSL.
- **After the fix:** D1–D2 and L1–L4 pass on all three hosts, and `install_test.sh` gives
  139/139 on all three.
  - The server's live unit kept `ActiveEnterTimestamp` 02:03:03 across the run.
  - The transient probe now stops the unit and attempts a start. The start fails only because a
    transient unit is gone once stopped, and the script now warns about it where before it said
    nothing.

### P2 — Pinned staticcheck and the 43 findings (D4, D5; closes F6, F7, F9, F10)

1. `STATICCHECK_VERSION`, a rule that installs the tool for the host into `bin/tools/`, and a
   `staticcheck` target that runs it for three GOOS. Preflight calls the target.
2. Seen to fail on the old tree, then fix the ST1005, U1000 (dead code) and SA1019 findings.

**Verification:** `make staticcheck` exits 0 with 0 findings for every GOOS.

**Executed 2026-09-24, commit `8944b53`.**
- **Seen to fail:** exit 2, with 42 findings per GOOS (43 unique).
- **Fixed:**
  - ST1005: two sentinels replace 17 copies, and the rest are lower-cased.
  - Three dead functions deleted, and `maxCommandLineBatch` moved.
  - Five SA1019 uses replaced. The JWS test now also asserts that the RFC's `d` derives its
    published `(x, y)`.
- **After:** exit 0, with 0 findings for linux, darwin and windows (WSL). gofmt is clean,
  build and vet pass for three GOOS, and the race and cgo-free suites pass on Windows (42
  packages each). `go-precheck.sh` is clean on 19 files.
- **Differed from the plan as written:** P10 step 3 in 0169 said `go run …@version`. That
  cannot do a per-GOOS pass, so the target installs the tool first (MADR D4).

### P3 — Doctor tells the truth (D6; closes F8)

1. A doctor test for every `StoreReality`, with the observation stubbed, so no test spawns
   codex.
2. Seen to fail, then export `DescribeReality` and wire the observation into doctor.

**Verification:** the test passes, fails under mutation, and a freshly built binary prints the
store.

**Executed 2026-09-24, commit `bd03cab`.**
- The test was seen failing under two `go test -overlay` mutations, with the tree untouched:
  one drops the warning line, one drops the store line.
- A fresh `mcremote doctor` on Windows prints `store: file_protected` for codex, in 6.0 s
  including the bounded probe.
- **Ordering change:** this commit came before P2's, so P2's commit could show 0 findings
  (`describeReality` was unused until doctor called it).

### P4 — CI parity (D3; closes F4)

The `go` job gains `make staticcheck`, `bash scripts/install-binary_test.sh` and
`sh scripts/install_test.sh`.

**Verification:** a dispatched `ci.yml` run shows all three steps green.

**Executed 2026-09-24, commit `1c45fe5`.** The dispatched run is **pending**: the push is
blocked by the disclosure guard until PLAN 0171 has run.

### P5 — Verification on the Linux server (all decisions)

`make preflight` in the server's checkout, fast-forwarded to the fixed commits.

**Executed 2026-09-24.** The commits reached the server by a `git bundle` over ssh (nothing was
published), fast-forwarding its clean checkout from `fac3db6` to `ff3b604`.
- `make preflight` exits 0, and every step ran: gofmt, tidy, vet, staticcheck v0.8.1 for linux,
  darwin and windows, `go test -race`, the install tests, the systemd units, the release
  build, the Flutter pin, `dart format`, `flutter analyze`, and `flutter test` (1416 passed).
- The live `mcremote` unit's `ActiveEnterTimestamp` stayed at 02:03:03 UTC across the run.
- A4's local half is met. Its dispatched CI run waits for the push (PLAN 0171).

## Verification (whole plan)

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Both install suites pass on macOS, WSL and the Linux server, touch no real service manager (the live unit's start time is unchanged), and cover the Linux branch including a degraded manager; each new case was seen failing | D1, D2 |
| A2 | `make staticcheck` (pinned v0.8.1, three GOOS) reports 0 findings, was seen reporting 43, and nothing is suppressed | D4, D5 |
| A3 | `mcremote doctor` names the codex store reality with its explanation; its test was seen failing first | D6 |
| A4 | CI's `go` job runs staticcheck and both shell suites, green on a dispatched run; `make preflight` green on the Linux server | D3 |

The criterion most likely to be dropped quietly is **A4's dispatched run**. Every local check
already passes, and the push that CI needs is blocked by an unrelated guard. That makes it
tempting to call the plan done without CI ever having run the new steps.

## Rollout and Rollback

Revert the phase's commit. P1's `install-binary.sh` change is one function. The old detection
can be restored alone, but that brings F3 back.

## Deferred (named, so they are not mistaken for oversights)

- **A container seam for `install.sh`.** Its container detection reads `/.dockerenv` and
  `/proc/1/cgroup` directly, so `install_test.sh`'s "native" cases would still flip inside a
  container. No host or CI runner is a container, so nothing fails today. It belongs in an
  installer record, when a container lane exists.
- **The GitHub push**, pending MADR/PLAN 0171.
