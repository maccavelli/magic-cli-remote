---
status: proposed
date: 2026-10-23
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the `go-selfupdate-lib v1.19.0` pin and the fullest use of the library (0207 amendment B)

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its "Amendment — 2026-10-23 (B)": D15 to D22, and D4 as amended. The owner's answers of 2026-10-23
to its questions 8 to 11 apply.

Sibling plans of the same MADR:

* [0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md):
  D25 to D27.
* [0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md),
  complete, which this plan's U1 follows.

D23, the Windows handoff, is not here. It is 0210's amendment B, at the owner's answer to question
8.

## Goal

* `go.mod` requires `go-selfupdate-lib v1.19.0`, and `ci.yml`'s two pins name `a6af9dc7…`.
* `make ci-windows-smoke` can pass on Windows: its build stamps the identity `buildinfo` reads.
* The update refuses a staged binary that cannot run here, and an image for another platform,
  before anything is stopped. It uses the library's HTTP client.
* Magic's Windows service path runs no PowerShell, and no `schtasks.exe` except `/change /enable`
  and `/disable`, until the library exports those.
* `ProbeStatus` reads launchd and Task Scheduler state in one library call each.
* `setup-service` stops and starts through the backends where their semantics match, and confirms
  every start the same way, with the product's probe.
* The script backend honours its context and wraps the library's sentinel errors.
* mcrelay's unit is `Type=notify`, and `systemctl --user start` returns after its `READY=1`.

## Scope

* **In scope:** the MADR's D15 to D22 and D4, in the phases below.
* **Not in scope:**
  * D23 (0210 amendment B), and 0210's A2, A3 and L2, which run between this plan's U2 and U3
    (the order below).
  * Any `acp-go-sdk2` change: the sibling plan.
  * Removing `schtasks /change`, which waits on the issue U0 files.
  * runit, s6 and OpenRC backends in the library, which wait on go-selfupdate-lib#2.

## Order across plans

At the owner's answer to question 8:

1. This plan's U0, U1 and U2.
2. [0210-PLAN-hand-off-agent-run-updates-outside-the-service.md](0210-PLAN-hand-off-agent-run-updates-outside-the-service.md),
   amendment A's A2 and A3.
3. 0210's amendment B (the Windows handoff).
4. 0210's L2, which then checks the Windows handoff instead of the refusal.
5. This plan's U3 to U8.

The `acp-go-sdk2` plan touches no file this one does, and runs after step 4.

## Facts the steps rely on [read 2026-10-23, at `88d37d1c`]

| Fact | Where |
| :--- | :--- |
| The pin and its two workflow pins | `go.mod:12`; `.github/workflows/ci.yml:712`, `:733` |
| The update's HTTP client is `&http.Client{Timeout: OperationTimeout}` | `internal/updateclient/client.go:91-94` |
| The spec's `identity_args` are `["version"]`; `Spec.Unpacker()` is nil for `binary` packaging | `selfupdate-release.json`; library `releasespec/spec.go:329-330` |
| `binaryWarning` compares paths as strings | `internal/cli/service/refresh.go:589` |
| The Windows script stamps `main.*` | `scripts/ci-windows-local.ps1:132` |
| `taskState` runs PowerShell; its callers | `control_schtasks.go:25-44`; `:48`, `:93`; `refresh_schtasks.go:31`; `uninstall.go:219` |
| `schtasks /query /xml`, `/create`, `/delete` | `setup_schtasks.go:26`, `:61`, `:86`, `:91`; `refresh_schtasks.go:38`, `:158`; `uninstall.go:223` |
| The field comparison | `setup_schtasks.go:134-318` |
| launchd's own `launchctl print` reads | `control.go:153-157`; `launchd_wait.go:54-57`, `:103-112`; `settle.go:189`, `:256` |
| `stopAndWaitDarwin`, `launchdJobRunning` callers | `setup.go:576`, `:597`, `:600`, `:820` |
| Start confirmation | `setup.go:430`; `installerhook.go:184`; `settle.go:82-178` |
| mcrelay's unit is `Type=simple`, and a test requires it | `mcrelay.user.service.tmpl:18`; `notify_unit_test.go:44-45` |
| `Task.Start` re-enables only what its own `Stop` disabled | library `taskscheduler/lifecycle.go:272-299` |

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Every new test is seen failing first,** on a scratch copy, never in the working tree. The
  execution record quotes the failure.
* **Before staging:** `make pre-add-check FILES="…"` on every staged `.go` file, `make test`,
  `make race`, `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64, `go mod tidy -diff`,
  `make check-records`, `make markdownlint-docs`, and the guard's redact preview from the
  repository root.
* **Host runs** need the owner's word for that phase. On Linux and macOS they use a scratch
  service, `mcremote-d3probe`, as 0210's rules set. On Windows they use the real task, backed up
  and restored, as 0210 R1 Deviation 3 set out (W1 to W4).
* **A phase ends with its execution record** here, staged for the owner, who commits it.

### U0 — The library issue for enabling a task (D18)

**Files:** this plan. One issue in go-selfupdate-lib:

* **Title:** "taskscheduler: export Enable and Disable for a task".
* **Body:** `Start` re-enables only a task its own `Stop` disabled; a setup command that runs in
  another process, after an earlier stop, must enable first. Magic's `startWindows` and
  `stopWindows` (`schtasks /change`) are the consumer. No host names.
* The record carries the issue's URL.

### U1 — Pin `v1.19.0` (D15)

**Files:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`; this plan.

1. `go get github.com/maccavelli/go-selfupdate-lib@v1.19.0`, then `go mod tidy`.
2. Both `ci.yml` pins: `@a6af9dc760a5392134dc62b00e6491b112d0ada5 # go-selfupdate-lib v1.19.0`.
3. `git ls-remote https://github.com/maccavelli/go-selfupdate-lib 'refs/tags/v1.19.0^{}'` still
   gives `a6af9dc7…`, and no newer tag exists; otherwise stop and prompt.
4. Gates. `TestWorkflowPinsMatchGoMod` and `TestReleaseJobsReadTheSpec` pass. The diff is
   `go.mod`'s one line, `go.sum`'s two, and the two pins.

### U2 — The Windows CI script stamps `buildinfo` (D21)

**Files:** `scripts/ci-windows-local.ps1`; this plan.

1. **Seen failing first,** on the Windows host, on the owner's word: `make ci-windows-smoke` on
   the current script, expecting check B14 to fail (REPORT A7).
2. Line 132 takes the `Makefile`'s pair:
   `-X github.com/maccavelli/go-selfupdate-lib/buildinfo.version=$ver` and `….kind=local`.
3. On the Windows host: `make ci-windows-smoke` passes, B14 included.

### U3 — The update's guards (D16)

**Files:** `internal/updateclient/client.go`, `client_test.go` or `command_test.go` (whichever
holds `New`'s tests, measured first), `internal/cli/service/refresh.go`, its test; this plan.

1. **Tests first:**
   * A staged binary whose `version` fails, or prints another tag, is refused, and the fake
     service records no stop.
   * A download that is not an image for this platform is refused before any stop.
   * `New` with no `HTTPClient` uses a client with no `Client.Timeout`.
   * `binaryWarning` reports no warning for two spellings of one path (case on Windows; a symlink
     on Unix).
2. **`client.go`:**
   * `client = selfupdate.DefaultHTTPClient()` where none is given.
   * `Config.Probes`: `selfupdate.NewVersionProber(spec identity args, nil, timeout)`; with
     `MC_CODESIGN_IDENTITY` on macOS, `codesign.NewChecker` as well.
   * `Config.Verifiers`: `selfupdate.NewImageVerifier(selfupdate.Platform{})`.
   * The tests' fake binaries need a runnable fixture, or the prober's `Run` seam. Which one is
     measured in step 1.
3. **`refresh.go`:** `binaryWarning` calls `service.SameExecutable`.
4. Gates.

### U4 — Task Scheduler through the COM API (D17)

**Files:** `internal/cli/service/control_schtasks.go`, `setup_schtasks.go`, `refresh_schtasks.go`,
`schtasks.go`, `uninstall.go`, `control.go`, and their tests (`schtasks_test.go` and those the
measurement finds); this plan.

1. **Measure each call site** in the facts table, and name the test files, before any edit. A
   file outside this list is a deviation.
2. **Tests first,** against a fake of the library calls:
   * install of an absent task registers once; an unchanged one registers nothing;
   * a changed one without `--force` fails naming the differing path from `DiffDefinitions`;
   * `--remove` deletes through `Delete`, and an absent task is no error;
   * `ProbeStatus` on Windows makes one `Status` call;
   * the uninstall's ownership check reads the program from `Definition`.
3. **Code:** a seam for the `*taskscheduler.Task` calls; `taskState` → `Status`; register, read
   and delete → `Register`, `Definition`, `Delete`; `sameTaskDefinition` → `SameDefinition`.
   `encodeTaskXML`, the temporary file and the three `schtasks` verbs go. `taskFieldsFromXML`
   stays only for the fields Magic reads.
4. **On the Windows host,** on the owner's word: `make ci-windows`; then, against the real task by
   W1 to W4: `setup-service` (unchanged), `--force` (re-registers), `--remove` and a fresh install.
   The task's definition after W4 compares the same as W1's.
5. Gates.

### U5 — launchd status through the library (D19)

**Files:** `internal/cli/service/control.go`, `launchd_wait.go`, their tests; this plan.

1. **Tests first:** `ProbeStatus` on darwin makes one `Status` call and reports `Loaded` from it;
   `launchdLoaded` returns an error, not false, for a probe that fails other than "not loaded".
2. **Code:** a seam for `launchd.Job`'s `Status` and `Loaded`; `ProbeStatus` and `launchdLoaded`
   use them.
3. **On macOS:** `mcremote doctor` against the real service shows the same service section as
   before, read-only.
4. Gates.

### U6 — Stop, start and start confirmation on the backends (D4)

**Files:** `internal/cli/service/setup.go`, `setup_schtasks.go`, `control_schtasks.go`,
`launchd_wait.go`, `settle.go`, `installerhook.go`, and their tests; this plan.

1. **Measure each call site** first (the facts table), and record where a backend's semantics
   differ from setup's: a first install with nothing loaded; persistent enabling; a task disabled
   by an earlier process (D18).
2. **Tests first:**
   * `--remove` and `--force` call the backend's `Stop`;
   * a Windows start enables, then calls `Task.Start`;
   * a start is confirmed by `WaitHealthy` for systemd, launchd and Task Scheduler, and by
     `PollHealthy` over the samplers for runit, s6 and OpenRC; `ErrTimeout` maps to starting,
     `ErrUnhealthy` to failed.
3. **Code** as tested. `stopAndWaitDarwin`, `launchdJobRunning`, `startWindows`, `stopWindows`
   and the settle loops go when nothing calls them.
4. **Host runs,** on the owner's word: install, `--force` and `--remove` of the scratch service
   on Linux and macOS; on Windows, against the real task by W1 to W4. `--remove` on Windows ends
   the daemon with exit code 0, a drain, as in 0054 S9d.
5. Gates.

### U7 — The script backend's contract (D20)

**Files:** `internal/cli/service/scriptbackend.go`, its tests; this plan.

1. **Tests first:** a cancelled context ends `Stop`, `Start` and `WaitHealthy` with `ctx.Err()`;
   their timeout and failure match `service.ErrTimeout` and `ErrUnhealthy` with `errors.Is`.
2. **Code** as tested.
3. **The three containers,** on the owner's word: install, `--force` and `--remove`, as 0210 R1.
4. Gates.

### U8 — mcrelay tells systemd it is ready (D22)

**Files:** `internal/cli/service/mcrelay.user.service.tmpl`, `notify_unit_test.go`, the relay's
start and shutdown code in `internal/relay` (measured first), the kept-socket sender 0210 A3
builds, moved where both daemons can import it; this plan.

1. **Measure** where mcrelay is ready (listening) and where it begins to shut down, and where
   0210 A3 left the sender.
2. **Tests first:** `notify_unit_test.go` requires `Type=notify` for mcrelay, replacing its
   "must stay `Type=simple`" assertion; a relay run against a `unixgram` listener receives
   `READY=1`, then `STOPPING=1`, and its environment has no `NOTIFY_SOCKET` after start.
3. **Code** as tested. An mcrelay unit written by an older release is refreshed by the existing
   refresh path, as mcremote's was.
4. **On the Linux host,** on the owner's word: a scratch mcrelay unit, `systemctl --user start`
   returns after `READY=1`, and `systemctl --user stop` sees `STOPPING=1` in the journal.
5. Gates, with the Linux host running `go test -race ./internal/relay/`.

## Verification

* U1: `go list -m github.com/maccavelli/go-selfupdate-lib` gives `v1.19.0`; the pin tests pass.
* U2: `make ci-windows-smoke` passes on the Windows host.
* U3 to U8: each phase's fail-first tests, quoted in its record; the gates.
* After U4 and U6: `git grep -n -e powershell -e '"/create"' -e '"/query"' -e '"/delete"' --
  internal/cli/service` finds nothing outside tests and hints.
* Each host run's outcome, in its phase's record.

## Rollout and Rollback

* **Rollout.** At the next tag, after `make ci-windows-smoke` passes (U2).
* **Rollback.** Revert by phase. U1 alone reverts to `v1.17.2`; U3 to U8 do not depend on each
  other's code, except U8 on 0210 A3's sender. A revert of U4 brings back the PowerShell probe and
  `schtasks.exe`; a revert of U6, the hard kill on Windows `--remove`. Nothing is migrated on a
  host, so nothing has to be undone there.

## Execution record

Nothing has run. The plan waits for the owner's approval.
