---
status: accepted
date: 2026-09-24
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Make `make preflight` true: hermetic install tests, a degraded-manager fix, pinned staticcheck, and CI parity

This record began on 2026-09-24 as an amendment to
[MADR 0169](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md)
(commit `d7f02e5`). The owner moved it here the same day, so that each record covers one
subject. Its identifiers are renumbered from 1; the table under More Information maps the 0169
numbers to these.

## Context and Problem Statement

MADR 0169's P9 verification ran this repository's `make preflight` on the Linux server. Two
failures surfaced there: PLAN 0169 Deviations 6 and 7. The owner decided to fix both. This
record captures what investigating them established. All of it comes from the code, from
driving the scripts and binaries on WSL, the Linux server and the macOS laptop, and from git
history. Nothing was changed to obtain it.

### What was measured, not assumed

- **The unapproved restart.** On the Linux server, running `scripts/install-binary_test.sh`
  stopped and restarted the live `mcremote` user service at 2026-09-24 02:03:03 UTC.
  - The service journal shows "Stopping mcremote.service", then providers re-initializing.
  - The test printed `FAIL the service was never stopped`, with the output
    `Stopping mcremote.service for install… Starting mcremote.service…`.
  - The journal shows no session or prompt activity in the 33 minutes before, so no live
    session is known to have been cut.
- **systemd exit codes, measured with no shell in between** (Python `subprocess`; an earlier
  measurement through `wsl -- bash -c` was discarded, because the outer shell expands `$?`):

  | Host | `systemctl --user is-system-running` | `is-active mcremote` | effect of `HOME=<fake>` |
  | --- | --- | --- | --- |
  | WSL, systemd 255 (one failed user unit) | `degraded`, **exit 1** | `inactive`, exit 4 | none |
  | Linux server, systemd 259 | `running`, exit 0 | `active`, exit 0 | none |

- **Driving `install-binary.sh` on a degraded manager.** WSL, with a throwaway transient
  unit (`systemd-run --user --unit=mc-installprobe sleep 600`):
  - the script exited 0 and printed `Installed …`;
  - the unit's MainPID was **240055 before and after**: it was never stopped or restarted;
  - nothing warned.
- **Driving `install-binary_test.sh` on three hosts:**
  - macOS: exit 0.
  - WSL: exit 0, but only because the manager is `degraded`, so the script fell to the
    `launchctl` stub.
  - Linux server: FAIL, after restarting the live service (above).
- **Driving `scripts/install_test.sh`.** Its header claims "identical results on a macOS
  workstation and a Linux CI runner".
  - macOS and the Linux server: 139 passed.
  - WSL: 136 passed, 3 failed (cases 22c, 24 and 25).
  - Those three cases do not set the `MC_TEST_OSRELEASE` seam (`install.sh:106`), so on WSL
    they read the real `/proc/sys/kernel/osrelease` ("microsoft"). They then hit the WSL
    suppression of the advisory they assert (`install.sh:858`), which is MADR 0099 F6 working
    as designed.
- **staticcheck 0.8.1** (the host Go-tool standard) over `master` at `0022a46`, for GOOS
  linux, darwin and windows, gives **43 unique findings**, 4 of them in `_test.go` files:

  | Check | Count | Where |
  | --- | --- | --- |
  | ST1005 (error string capitalized) | 33 | `internal/provider/codex/*` (32) and `internal/ws/codex_handlers.go:158`. Every one begins "Codex …" |
  | U1000 (unused) | 5 | `codex/session.go:2128` `serverDied`; `codex/threads.go:675` `nativeThreads`; `codex/store_reality.go:147` `describeReality`; `providerauth/store.go:174` `removeGeneration`; `launch/launch.go:83` `maxCommandLineBatch` (linux and darwin only) |
  | SA1019 (deprecated) | 5 | `appdirs/security_windows.go:37` `windows.OpenCurrentProcessToken`; `acpagent/rewind_test.go:484` `parser.ParseDir`; `receipt/jws_test.go:45,46,59` `ecdsa` `X`, `Y` and `D` |

  On the Linux server's older checkout (`1a7702e`) the count was 42 both under mise's Go and
  under `~/sdk`'s Go. So the findings do not come from 0169's toolchain change.
- **git blame** dates the findings' lines from 2026-07-26 (19fe240) to 2026-09-19 (5500d62).
  34 come from the four Codex commits of 2026-08-25. staticcheck joined `make preflight` in
  627eb4b on 2026-07-27. The two SA1019 deprecations are themselves new ("since Go 1.25",
  "since Go 1.26"), so some older code began failing as the toolchain moved.

### Findings

- **F1 — `install-binary_test.sh` is not hermetic.**
  - It replaces `HOME` and puts a stub `launchctl` first on PATH, but leaves the real
    `systemctl` on PATH.
  - `systemctl --user` talks to the real user manager through `XDG_RUNTIME_DIR` and D-Bus,
    and `HOME` has no effect on it (measured on both Linux hosts).
  - On any host whose user manager is `running`, the script under test takes its Linux branch
    against the live unit. On the Linux server that restarted the running daemon.
- **F2 — That test's verdict depends on the host, not the code.**
  - It passes on macOS and on a degraded Linux manager, and fails on a healthy one.
  - The Linux branch of `install-binary.sh` (`is-active`, `is-enabled`, `stop`, `start`) is
    exercised by no test at all.
- **F3 — `make install` does not restart the service when the user manager is `degraded`.**
  - `detect_service` (`install-binary.sh:63`) requires `is-system-running` to exit 0, and
    `degraded` exits 1. Any single failed user unit therefore turns off service handling: the
    binary is swapped, the old process keeps running the replaced inode, and the install
    exits 0 without a warning.
  - The repository's two other detectors already get this right. `install.sh:149-152` falls
    back to `show-environment`. The Go `preflightLinux` (`internal/cli/service/setup.go:715-721`)
    fails only when the bus cannot be reached.
- **F4 — Three gates have no reliable runner.**
  - CI's `go` job runs gofmt, tidy, vet, the race and cgo-free tests, `verify-units`,
    `verify-build-metadata` and the build (`.github/workflows/ci.yml:84-159`). It runs
    neither staticcheck nor either shell suite.
  - `make preflight` runs staticcheck and `install-binary_test.sh`. Its comment says it
    "mirrors the `go` and `flutter` jobs gate-for-gate … so a green preflight means a green
    CI" (`Makefile:360-362`). That is false in both directions.
  - `install_test.sh` is referenced by nothing but its own usage line.
  - This is how F1–F3, F5 and F8 went unseen: the only runner of each was a local target that
    had been red since 2026-07-27. That last point is inferred from the dates, not measured at
    each commit.
- **F5 — `install_test.sh` is host-dependent in 3 of its 139 cases** (22c, 24, 25). They omit
  the `MC_TEST_OSRELEASE` seam that its other environment cases set. The installer's behaviour
  is correct.
- **F6 — The ST1005 findings are one convention broken in one package.** All 33 strings begin
  "Codex". No Go test and no Dart code matches their text (searched; the only other occurrences
  are copies of the same literal). "Codex provider unavailable" is one message written out 14
  times, and "Codex engine is not running" 3 times. Every other package passes ST1005.
- **F7 — Four of the five U1000 findings are dead code left by a replacement.**
  - `serverDied` was replaced by `engineLost` plus the reconnect loop (b56b00f,
    `provider.go:1002-1064`). Retained sessions are reconciled on the next `ensureEngine`, so
    nothing needs it. Checked: it is not a missing call.
  - `nativeThreads` is a context-less duplicate of `nativeThreadsFor(ctx)`, which is used at
    all 16 native-thread call sites in `threads.go` (f2b8e48).
  - `removeGeneration` was superseded by `pruneGenerations` (518f1df).
  - `maxCommandLineBatch` is declared in the all-platform `launch.go`, but only
    `launch_windows.go` uses it.
- **F8 — The fifth U1000 finding is an unimplemented promise, not dead code.**
  - MADR 0074's amendment (3066d31) says an `external` credential store "is a reason to tell
    the operator the truth".
  - `describeReality` is that explanation. Its only caller is a `//go:build live_codex` test.
  - Driving `mcremote doctor` (0.20.0) on the Linux server and on Windows prints, for codex,
    only `~/.codex/auth.json`. The operator is never told.
- **F9 — The SA1019 findings have direct replacements.**
  - `OpenCurrentProcessToken` becomes `OpenProcessToken(CurrentProcess(), TOKEN_QUERY)`,
    which is the same access, made explicit.
  - `ParseDir` becomes a `ParseFile` walk; ParseDir also ignored build tags.
  - The raw `ecdsa` field use in the JWS test becomes `ParseRawPrivateKey` and
    `ParseUncompressedPublicKey`. That code is test-only.
- **F10 — staticcheck's version is whatever the host has.** The Makefile called `staticcheck`
  from PATH (`Makefile:387,533`). There is no `go.mod` tool directive, no configuration and no
  CI step. What decided preflight was the host's Go-tool standard, not the repository.
- **F11 — The shell-quoting trap applies to this project's own WSL test lane.**
  `wsl -- bash -c '…'` re-parses the command through WSL's default shell, so `$?` and other
  expansions happen outside. Exit codes gathered that way are wrong. The first measurement in
  this investigation made exactly that mistake. `wsl -e` or a script file avoids it.
- **F12 — The degraded state is ordinary.** On WSL one failed desktop-portal unit
  (`xdg-desktop-portal-gtk.service`) is enough. Such units fail routinely on headless hosts, so
  F3 is not a corner case.

## Decision Drivers

- A gate that only a local target runs, and that has been red for weeks, gates nothing. Every
  check `make preflight` runs must also run in CI.
- A test must never act on the developer's live services, whatever the host's state.
- A real `make install` failure (F3) and a documented promise (F8) outrank the convenience of
  suppressing the findings that exposed them.
- The staticcheck verdict must be the repository's, pinned, not the host's.

## Considered Options

- **A — Fix the defects, make the tests hermetic, run the same gates in CI.**
- **B — Suppress and narrow.** Add `staticcheck.conf` disabling ST1005, drop staticcheck and
  `install-binary_test.sh` from preflight, and correct the comment.
- **C — Fix only the install test and F3**, and leave staticcheck red.
- **D — Status quo.**

## Decision Outcome

Chosen option: **A**, because it is the only option that leaves every defect found fixed, and
each fix covered by a gate that actually runs.

### The decisions

- **D1 — One systemd detection rule across the repository.** `install-binary.sh` treats the
  user manager as usable when `XDG_RUNTIME_DIR` exists and either `is-system-running` or
  `show-environment` succeeds. That is `install.sh`'s rule, so a `degraded` manager still gets
  its service stopped and restarted.
- **D2 — The install tests are hermetic and cover both branches.**
  - `install-binary_test.sh` gives each run a PATH made only of a stub directory, with stub
    `systemctl` or `launchctl`, a fake `XDG_RUNTIME_DIR` and no D-Bus address.
  - A guard refuses to run, with exit 2, if either service manager resolves outside its stub
    directory on the PATH a run will use.
  - It gains Linux cases: a running unit is stopped, swapped and started; a `degraded` manager
    still restarts (F3); an enabled-but-stopped unit is healed; a disabled unit is left alone.
  - `install_test.sh` gives every case that does not name its own a non-WSL `MC_TEST_OSRELEASE`.
- **D3 — CI runs every gate `make preflight` runs.** The `go` job gains `make staticcheck`,
  `install-binary_test.sh` and `install_test.sh`. The Makefile's "gate-for-gate" comment then
  becomes true, rather than being deleted.
- **D4 — staticcheck is pinned by the repository.** A Makefile variable,
  `STATICCHECK_VERSION = v0.8.1`, drives the `staticcheck` target, which preflight and CI call.
  - The target installs that version for the host into the gitignored `bin/tools/`, then runs
    it once for each of GOOS linux, darwin and windows.
  - The first design, `go run …@version`, could not do the per-GOOS pass, because `GOOS` would
    cross-compile the tool itself.
  - A `go.mod` tool directive was not chosen: it would add staticcheck's dependencies to this
    module's graph and to govulncheck's scope.
- **D5 — The 43 findings are fixed, not suppressed.**
  - **ST1005:** one sentinel per repeated message (`errCodexUnavailable`,
    `errCodexEngineNotRunning`), and the remaining strings in lower case. The phone shows the
    lower-case text; no client logic depends on it (F6).
  - **Dead code:** `serverDied`, `nativeThreads` and `removeGeneration` are deleted.
    `maxCommandLineBatch` moves to `launch_windows.go`.
  - **SA1019:** replaced as in F9.
  - No `staticcheck.conf`, no `//lint:ignore`, and no check removed from preflight.
- **D6 — The store-reality explanation is wired, not deleted (F8).** `describeReality` becomes
  the exported `DescribeReality`. `mcremote doctor`'s codex entry prints the observed store and,
  for a store mcremote cannot protect, the explanation. It reads no values and names no
  account. A unit test pins the output for every reality.

### Consequences

- Good, because `make install` now restarts the service on a degraded manager, and says so when
  a restart fails, instead of exiting 0 silently.
- Good, because the install tests can no longer reach a real service manager, on any host.
- Good, because a green preflight now means a green CI and the other way round, with the
  staticcheck version fixed by the repository.
- Good, because an operator whose codex credential is outside mcremote's reach is told.
- Bad, because `mcremote doctor` now spawns `codex doctor --json` once (bounded by
  `providerauth.ProbeTimeout`; 6.0 s end to end on the Windows host).
- Neutral, because the phone's Codex error text changes case.

### Confirmation

```sh
bash scripts/install-binary_test.sh        # passes on macOS, WSL (degraded) and the Linux server; the live unit's start time unchanged
sh scripts/install_test.sh                 # 139 passed on all three
make staticcheck                           # 0 findings, pinned v0.8.1, GOOS linux/darwin/windows
make preflight                             # green on the Linux server
mcremote doctor                            # the codex entry names the store reality
# CI: the go job runs staticcheck and both shell suites (a dispatched run shows them)
```

Each new check must first be seen failing:
- the guard, with the real service manager on the run PATH;
- the degraded case, against the old `detect_service`;
- the pinned staticcheck, on the old tree (43 findings);
- the doctor test, against the old doctor.

## Pros and Cons of the Options

### A — Fix the defects, make the tests hermetic, run the same gates in CI (chosen)

- Good, because every defect found gets a fix, and each fix a test or gate that runs in CI.
- Bad, because it touches the codex provider in about eight files, plus doctor, the two shell
  tests, the Makefile and CI.
- Neutral, because the phone's error text changes case.

### B — Suppress and narrow

- Good, because it is the smallest diff.
- Bad, because F3 (a real `make install` failure) and F8 (a documented promise) stay broken,
  and the tests that could catch them stay switched off.

### C — Fix only the install test and F3

- Good, because it removes the risk of a live-service restart quickly.
- Bad, because preflight stays red, so it keeps failing as a gate, and F8 stays open.

### D — Status quo

- Bad, because the next person to run `make preflight` on a Linux host restarts their own
  daemon.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| The live restart at 02:03:03 UTC | the Linux server's `journalctl --user -u mcremote`, 2026-09-24 |
| Exit codes of `is-system-running`, `is-active`, and the effect of a fake HOME | Python `subprocess` probe on WSL (systemd 255) and the Linux server (systemd 259) |
| `install-binary.sh` does not restart on a degraded manager | transient-unit probe on WSL: MainPID 240055 before and after |
| `install_test.sh` fails 3/139 on WSL only | runs on WSL, the Linux server and macOS, 2026-09-24 |
| 43 staticcheck findings, by check, file and GOOS | staticcheck 0.8.1 `-f json` for linux, darwin and windows, merged |
| Finding dates and commits | `git blame --porcelain` on each finding's line |
| Nothing matches the ST1005 texts | whole-repository search of Go and Dart sources for each string |
| `serverDied` is superseded, not missing | `internal/provider/codex/provider.go:1002-1064`; b56b00f |
| `describeReality` had one caller, behind `live_codex` | `internal/provider/codex/reality_host_test.go:1` |
| `mcremote doctor` printed only the codex path | `mcremote doctor` 0.20.0 on the Linux server and Windows |
| CI's `go` job gates | `.github/workflows/ci.yml:84-159` at `0022a46` |

### Numbering carried over from MADR 0169

| 0169 | 0170 |
| --- | --- |
| F11–F22 | F1–F12 |
| D16–D21 | D1–D6 |
| PLAN P10, steps 1–7 | PLAN P1–P5 |
| PLAN A15–A18 | PLAN A1–A4 |

### Related records

- [0169-MADR](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md):
  the toolchain work whose verification surfaced this.
- [0074-MADR](../spec/0074-MADR-remote-provider-auth-from-phone.md): the "tell the operator the
  truth" amendment that D6 implements.
- [0099-MADR](../spec/0099-MADR-installer-service-state-verification.md): the installer's WSL
  advisory routing that F5's cases exercise.

## Amendment — 2026-09-24: `BASE_VERSION` is empty when make runs from PowerShell

**F13 — `Makefile:9` sorts tags with `sort -V`, and from PowerShell that is Windows' `sort`.**
Once Git's `usr\bin` was back on the Windows user `Path`, so that make gets `sh.exe` and
`make ci-windows` stops taking the skip branch, `make -n ci-windows` from a fresh PowerShell
printed `-VThe system cannot find the file specified.`. Windows always puts the machine `Path`,
which holds `C:\Windows\System32`, ahead of the user `Path`, so `sort` resolves to
`System32\sort.exe`, which reads `-V` as a file name. Measured with
`make --eval 'pv: ; @echo $(BASE_VERSION)' pv`: PowerShell prints an empty value and the error
twice; Git Bash prints `0.20.0`. `LOCAL_VERSION` (`Makefile:49`) is built from it, so a local
build from PowerShell is stamped `.g<commit>` instead of `0.20.0.g<commit>`. The defect predates
the `Path` change: without `sh`, make ran `$(shell)` through `cmd.exe`, which found the same
`sort`. No other command in that pipeline has a System32 namesake.

**D7 — Let git order the tags.** Replace `sort -V` with `git tag --sort=v:refname`, git's own
version sort, so the pipeline no longer depends on which `sort` is first on `PATH`. The
`grep -E` filter, `tail -1`, `sed` and the `|| echo 0.0.0` fallback are unchanged, so the
selected tag is the same on every host where it was already right.
