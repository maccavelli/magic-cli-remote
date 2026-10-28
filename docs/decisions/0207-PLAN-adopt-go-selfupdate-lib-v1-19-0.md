---
status: complete
date: 2026-10-27
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the `go-selfupdate-lib v1.19.0` pin and the fullest use of the library (0207 amendment B)

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its "Amendment — 2026-10-23 (B)": D15 to D22, and D4 as amended. The owner's answers of 2026-10-23
to its questions 8 to 11 apply.

*Added 2026-10-24, proposed:* its "Amendment — 2026-10-24 (C)", D29 to D32, taken here as step U1b
and as notes on U4 and U6. D32 changes no code.

*Added 2026-10-24, proposed:* its "Amendment — 2026-10-24 (D)", D33 to D36, replace-before-stop
with a post-install probe, taken here as step U3b.

*Added 2026-10-24, proposed:* its "Amendment — 2026-10-24 (E)", D37 to D39. D37 moves U1b's pin
to `v1.22.0`. D38 and D39 change nothing here.

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
  * *Amendment C, proposed:* `v1.21.0` exports them (D30), so the goal is no `schtasks.exe` at
    all, after U4 and U6.
* *Amendment C, proposed:* `go.mod` requires `go-selfupdate-lib v1.21.0`, and `ci.yml`'s two pins
  name `29337d00…` (D29, U1b).
  * *Amendment E, proposed:* `v1.22.0` instead, with the pins naming `84805563…` (D37, U1b).
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
    *Amendment C, proposed:* the issue shipped in `v1.21.0`, so this moves into U6 (D30).
  * runit, s6 and OpenRC backends in the library, which wait on go-selfupdate-lib#2.
    *Amendment E:* `v1.22.0` ships the library's answer, the supervised adapter. Adopting it is
    0212's work, not this plan's.

## Order across plans

At the owner's answer to question 8:

1. This plan's U0, U1 and U2.
2. [0210-PLAN-hand-off-agent-run-updates-outside-the-service.md](0210-PLAN-hand-off-agent-run-updates-outside-the-service.md),
   amendment A's A2 and A3.
3. 0210's amendment B (the Windows handoff).
4. 0210's L2, which then checks the Windows handoff instead of the refusal.
5. This plan's U3 to U8.

The `acp-go-sdk2` plan touches no file this one does, and runs after step 4.

*Amendment D, proposed:* U3b runs right after U3, whose prober it reuses.

*Amendment C, proposed:* steps 1 to 4 are done. U1b runs next, before U3, so U3 to U8 build on
`v1.21.0`. 0212's stage 1 does not depend on the pin, and its stage 2 follows U1b
([0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
D8).

*Amendment E, proposed:* U1b pins `v1.22.0`, so U3 to U8 build on it. 0212's stage 2 then has no
pin move of its own, and its stage 1 can use the adapter's `SupervisedState` (0212 amendment B,
E2 and D16).

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
  * *Added 2026-10-24, the owner's answer "Yes, every phase":* `go test -race ./...` on the Linux
    test host too, in a scratch copy of the working tree. A Linux-only test failure reached CI
    through macOS gates
    ([0210-PLAN-hand-off-agent-run-updates-outside-the-service.md](0210-PLAN-hand-off-agent-run-updates-outside-the-service.md),
    L2 Deviation 1). The sibling `acp-go-sdk2` plan takes these rules, so it gets the same gate.
  * *Added 2026-10-25, U4 Deviation 2, the owner's answer "option 1":* `make staticcheck`, the
    pinned staticcheck for GOOS linux, darwin and windows, which CI's `Go (test)` job runs. U4
    shipped a finding it reports.
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

### U1b — Pin ~~`v1.21.0`~~ `v1.22.0` (amendment C, D29; amendment E, D37) — done

*Amended 2026-10-24 (E), before it ran:* `v1.22.0` was tagged the same day, and the owner asked
for the newest pins. The struck values are D29's.

**Files:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`; this plan.

1. `git ls-remote https://github.com/maccavelli/go-selfupdate-lib 'refs/tags/v1.22.0^{}'` gives
   `84805563ed252df384f59ced45a3daf529702ab4`, and no tag above `v1.22.0` exists. Otherwise stop
   and prompt. ~~`v1.21.0^{}` gives `29337d004689272c3f347db4c444ed81e0bec21c`.~~
2. `go get github.com/maccavelli/go-selfupdate-lib@v1.22.0`, then `go mod tidy`.
   ~~`@v1.21.0`~~
3. Both `ci.yml` pins:
   `@84805563ed252df384f59ced45a3daf529702ab4 # go-selfupdate-lib v1.22.0`.
   ~~`@29337d004689272c3f347db4c444ed81e0bec21c # go-selfupdate-lib v1.21.0`~~
4. **The diff** is `go.mod`'s one line, `go.sum`'s two (the module's `h1:` line changes to
   `h1:AHBwXsYs08WGTteAU7e/1NAa/CqbnTbZx+Xj6LSjKmk=` and its `/go.mod` hash does not), and the two
   pins. No Go source changes. `git grep a6af9dc7` finds nothing in `.github`, `go.mod` or
   `go.sum`.
5. **Gates:**
   * the rules above, including `go test -race ./...` on the Linux test host;
   * `TestWorkflowPinsMatchGoMod` and `TestReleaseJobsReadTheSpec` pass;
   * `make vulncheck`;
   * on the Windows host, on the owner's word, `make ci-windows`, since the pin changes what
     Windows builds. No task is touched.

   U1b stages no Go file, so `make vulncheck` and the pin tests stand in for the per-file pre-add
   check, as in U1.

### U1c — Pin `v1.22.2` (amendment G, D44) — ~~proposed~~ approved 2026-10-27, folded into U6

**Files:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`; this plan and the MADR's amendment G.

1. `git ls-remote https://github.com/maccavelli/go-selfupdate-lib 'refs/tags/v1.22.2^{}'` gives
   `50deb3f1317bcd9142f2c91be2b5609957c677c9`, and no tag above `v1.22.2` exists. Otherwise stop
   and prompt.
2. `go get github.com/maccavelli/go-selfupdate-lib@v1.22.2`, then `go mod tidy`.
3. Both `ci.yml` pins: `@50deb3f1317bcd9142f2c91be2b5609957c677c9 # go-selfupdate-lib v1.22.2`.
4. **The diff** is `go.mod`'s one line, `go.sum`'s module lines, and the two pins. No Go source
   changes. `git grep 4d9ae417` then finds nothing in `.github`, `go.mod` or `go.sum`.
5. **Gates:** U1b's, on the commit as it will land: the rules above, `go test -race ./...` on the
   Linux test host, `TestWorkflowPinsMatchGoMod` and `TestReleaseJobsReadTheSpec`, and
   `make vulncheck`. `make ci-windows` on the Windows host, on the owner's word.

**U6 is in the working tree, uncommitted** (its execution record, "U6 Deviation 1"), and its tests
do not compile yet. So the working tree cannot pass these gates, and U1c's commit must not carry
U6's code. How U1c lands beside it is the owner's to decide when approving U1c.

### U3 — The update's guards (D16) — done

**Files:** `internal/updateclient/client.go`, `client_test.go` or `command_test.go` (whichever
holds `New`'s tests, measured first), `internal/cli/service/refresh.go`, its test; this plan.
*U3 Deviation 1, 2026-10-25:* `internal/updateclient/managed_test.go` joins, for the runnable
fixture (execution record below). *U3 Deviation 2:* so do `internal/updateclient/testdata/fixture/main.go`
and its `go.mod`, the fixture program. *U3 Deviation 3:* so does
`internal/cli/service/refresh_windows_test.go`, for the case on Windows.

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

### U3b — Replace before the stop, with a post-install probe (amendment D, D33 to D36) — ~~proposed~~ approved 2026-10-25, done

**Files:**

* `internal/updateclient/managed.go`, `client.go`, `managed_test.go`, and the test file that holds
  `New`'s tests (measured in U3);
* new: `internal/updateclient/replacefirst_guard_test.go` (G1);
* new: `internal/cli/service/definitions_once_test.go` (G2);
* new: `internal/updateclient/live_replacefirst_test.go` (live-tagged);
* `Makefile`, for its `live-replacefirst` target;
* `docs/guides/ops-linux-install.md` and `ops-windows-install.md`, one sentence each on the order;
* this plan.
* *U3b Deviation 1, 2026-10-25:* `internal/updateclient/testdata/fixture/main.go`, U3's fixture,
  joins for a switch that makes it fail only once installed (execution record below).

1. **Measure:**
   * that U3 has landed, and where its prober is built;
   * the fixture binary U3 chose (a runnable fixture, or the prober's `Run` seam), which U3b
     reuses;
   * the real `mcremote version` run time on each OS, which fixes D34's timeout.
2. **Tests first,** each seen failing on a scratch copy:
   * **The order.** Over the fake GitHub server and a fake lifecycle (`managed_test.go`'s pattern),
     the target holds the new release's bytes when `Stop` is called. This fails under today's
     `NewManagedInstaller`.
   * **The probe.** A release whose binary fails `version` ends the update with an error. The
     fake lifecycle records no `Stop`, and the target's bytes are unchanged. Without `PostInstall`
     this sees a `Stop`.
   * **The result.** The `--json` result has `replaced_before_stop: true`.
   * **G1** (D35): AST scan of the non-test files under `cmd` and `internal`.
     * Its allowlist, with reasons: `internal/cli/doctor.go`, and `internal/cli/service/setup.go`,
       `refresh.go` and `uninstall.go`.
     * Plants: an `os.Executable()` call added to an `internal/daemon` file, and a stale allowlist
       entry. Each must fail.
   * **G2** (D35): renders both units, the plist, the task XML, and each script kind's definition.
     The binary's path appears exactly once, and no `ExecStop`, `ExecStopPost` or `ExecReload`
     appears. Plant: an `ExecStop=` line in a copy of the mcremote template, which must fail.
3. **Code:**
   * `managedInstaller` builds `NewManagedInstallerWith(…, selfupdate.ManagedOptions{ReplaceBeforeStop:
     true})`.
   * `New` sets `install.PostInstall` to U3's version prober. It is built once, and `Config.Probes`
     uses it too.
4. **The live test** `TestLiveReplaceFirstProbeFailure`, behind
   `MCREMOTE_REQUIRE_REPLACEFIRST=1` and a `make live-replacefirst` target.
   * It builds the updater as `New` does, against the fake GitHub server, serving a release whose
     binary fails `version`.
   * It uses the real backend of the scratch service.
   * It asserts the error, the target's bytes unchanged, and the daemon's PID and start token
     unchanged.
5. **Host runs, on the owner's word** (0210's kit and rules; Windows by W1 to W4):
   * **L-a, success:** an update from a shell, and an agent-run update that hands off, on:
     * Linux (systemd, scratch service) and macOS (launchd, scratch service);
     * runit, s6 and OpenRC, in the containers;
     * Windows, on the real task.

     Each result has `replaced_before_stop: true`, `service_started: true` and
     `rolled_back: false`, with a new PID and `/healthz` up.
   * **L-b, failure:** `make live-replacefirst` in each of the same places. The daemon's PID and
     start token are unchanged, and so are the binary's bytes.
6. **Gates,** with the Linux race gate, and the guide sentences.

### U4 — Task Scheduler through the COM API (D17)

**Files:** `internal/cli/service/control_schtasks.go`, `setup_schtasks.go`, `refresh_schtasks.go`,
`schtasks.go`, `uninstall.go`, `control.go`, and their tests (`schtasks_test.go` and those the
measurement finds); this plan. *U4 Deviation 1, 2026-10-25:* `health.go` and `health_test.go` join
(execution record below).

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
   W1 to W4: `setup-service` (unchanged), `--force` ~~(re-registers)~~, `--remove` and a fresh
   install. The task's definition after W4 compares the same as W1's.
   *U4 Deviation 4, 2026-10-26:* over an unchanged definition `--force` registers nothing (D17).
   The overwrite path is exercised by a changed option, refused, then forced, then restored.
5. Gates.

*Amendment C, proposed (D31):*

* **Tests first:**
  * a stopped task with an unchanged definition registers nothing, and asks for no `--force`;
  * `--no-start` over a stopped task with a changed definition registers it with
    `<Enabled>false</Enabled>`;
  * "stopped" is read from `Status.Disabled`.
* **On the Windows host:** step 4 adds `setup-service --no-start` over the stopped real task. It
  stays disabled, and reads as unchanged.

### U5 — launchd status through the library (D19)

**Files:** `internal/cli/service/control.go`, `launchd_wait.go`, their tests; this plan.
*U5 Deviation 1, 2026-10-26:* `launchd_sequence_test.go`, `setup_launchd_test.go` and
`uninstall_unix_test.go` join (execution record below).

1. **Tests first:** `ProbeStatus` on darwin makes one `Status` call and reports `Loaded` from it;
   `launchdLoaded` returns an error, not false, for a probe that fails other than "not loaded".
2. **Code:** a seam for `launchd.Job`'s `Status` and `Loaded`; `ProbeStatus` and `launchdLoaded`
   use them.
3. **On macOS:** `mcremote doctor` against the real service shows the same service section as
   before, read-only.
4. Gates.

### U6 — Stop, start and start confirmation on the backends (D4) — done 2026-10-27, with Deviations 1 to 3

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

*Amendment C, proposed (D30):*

* The Windows start is `Task.Enable`, then `Task.Start`. The stop is `Task.Stop`, which leaves
  the task disabled.
* Step 2 adds a test: after a stop in another process, a start enables first and then starts. It
  is seen failing against a start that only calls `Task.Start`, which returns `ErrTaskDisabled`.
* After U6, `git grep -n '"/change"' -- internal/cli/service` finds nothing outside tests.
  `result_print.go:174`'s printed hint stays.

*Note, 2026-10-27, from
[0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)'s
amendment D (D22), decided by the owner the same day ("approved to proceed"):*

* **U6 waits for 0212's P5,** not only its P3. P5 gives runit, s6 and OpenRC a library backend,
  `*libservice.Supervised`, so U6 then works on one path for all six kinds.
* **Step 2's start confirmation, amended.**
  * ~~by `PollHealthy` over the samplers for runit, s6 and OpenRC~~ For runit, s6 and OpenRC, by
    the 0212 P5 backend's `WaitHealthy`, which runs `PollHealthy` over `State` itself.
  * `ErrTimeout` still maps to starting, and `ErrUnhealthy` to failed.
* **A test added to step 2:** setup does not call a backend's `Start` on a systemd unit that is
  already active, then wait on it. systemd's `Start` records the unit's current invocation first,
  so its `WaitHealthy` would wait for a new one that never comes (go-selfupdate-lib
  `selfupdate/service/systemd/lifecycle.go:138-166`, at `v1.22.1`). It is seen failing against a
  setup that does.
* **Files:** ~~unchanged~~ widened by Deviation 1 (2026-10-27, below). `scriptbackend.go` is gone
  by then, deleted in 0212's P5.
* **Nothing is asked of the library** (0212 amendment D, D19). U6 uses only what `v1.22.1` has.

*Deviation 1, 2026-10-27, decided by the owner:* U6 also wires the product's health into setup,
builds a backend by kind, starts the script kinds through the adapter, takes the backends' wait
limits, and drops macOS's in-place `kickstart -k`. The files and the reasons are in the execution
record, "U6 Deviation 1".

*Note, 2026-10-27:* 0212's P5 landed as `5c499ad0`, and its P6 live tests (`dcd7b31f`) passed on
runit, s6 and OpenRC. U6's wait is over. It still needs the owner's approval to start.

### U6b — setup-service's result says what U6 does (D4) — ~~proposed~~ approved and done 2026-10-27

The owner asked, after U6 was pushed as `59bcadad`: "scope in fixing result print output". U6's
record had left `result_print.go` stale, since it was outside U6's files.

**Files:** `internal/cli/service/result_print.go`, `setup.go` (`Result`, `stopForRemoval`),
`setup_schtasks.go` and `supervisor.go` (their removals record a tolerated stop failure), and
their tests; this plan.

1. **The "Started:" line names what setup did** (`result_print.go:119-127`):
   * systemd: "yes (stopped, then started)" in place of "(systemctl --user restart/start)";
   * launchd: "yes (booted out, then bootstrapped and kickstarted)" in place of "(bootstrap +
     kickstart)";
   * Windows: "yes (enabled, then started; --force stops it first)" in place of "(schtasks /run)".
2. **`--remove` reports a stop that failed.** `stopForRemoval` tolerates any stop failure but the
   inside refusal, and the printer still said "The daemon was stopped". So:
   * it returns the tolerated error, and each removal records it on a new `Result.StopFailure`;
   * `PrintRemoveResult` prints "The daemon could not be stopped: <why>. Its definition is removed;
     end the process yourself if it still runs." in that case. Otherwise it prints, as today, that
     the daemon was stopped and the service disabled.
3. **Unchanged:** the operator commands the result prints (`Status:`, `Start:`, `Stop:`), which
   are still how a person does each by hand. D30 kept the Windows `Start:` hint.
4. **Tests first:** each "Started:" line per scope, and `--remove` with a tolerated stop failure on
   systemd, launchd, Windows and a script kind, each seen failing on a scratch copy.
5. Gates, as the rules above. No host run: the wording is unit-tested, and U6's host runs
   exercised the paths.

### U7 — The script backend's contract (D20)

**Files:** `internal/cli/service/scriptbackend.go`, its tests; this plan.

1. **Tests first:** a cancelled context ends `Stop`, `Start` and `WaitHealthy` with `ctx.Err()`;
   their timeout and failure match `service.ErrTimeout` and `ErrUnhealthy` with `errors.Is`.
2. **Code** as tested.
3. **The three containers,** on the owner's word: install, `--force` and `--remove`, as 0210 R1.
4. Gates.

*Retired, noted 2026-10-27:* U7 is retired into
[0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
(its R2), and is not run. 0212's P5 (`5c499ad0`) deleted `scriptbackend.go`. The library's
`service.NewSupervised` now gives runit, s6 and OpenRC the contract D20 asks for. 0212's P6
(`dcd7b31f`) runs the three containers in the tree, as `make live-supervisors`.

### U8 — mcrelay tells systemd it is ready (D22)

**Files:** `internal/cli/service/mcrelay.user.service.tmpl`, `notify_unit_test.go`, the relay's
start and shutdown code in `internal/relay` (measured first), the kept-socket sender 0210 A3
builds, moved where both daemons can import it; this plan. *U8 Deviation 1, 2026-10-26:*
`internal/daemon/daemon.go` and `internal/daemon/notify_unix_test.go` join, and the unit also gets
`TimeoutStartSec=240` (execution record below).

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
* U1b (amendment C): the same, giving ~~`v1.21.0`~~ `v1.22.0` (amendment E). `go list -m
  github.com/maccavelli/acp-go-sdk2` still gives `v1.20.0`, and `go list -m all` names no
  `go-llmprovider-sdk` (D38, D39).
* U3b (amendment D): its tests, G1 and G2, each seen failing first; L-a and L-b recorded per host.
* U2: `make ci-windows-smoke` passes on the Windows host.
* U3 to U8: each phase's fail-first tests, quoted in its record; the gates.
* After U4 and U6: `git grep -n -e powershell -e '"/create"' -e '"/query"' -e '"/delete"' --
  internal/cli/service` finds nothing outside tests and hints. With amendment C, `'"/change"'` is
  added to that search.
* Each host run's outcome, in its phase's record.

## Rollout and Rollback

* **Rollout.** At the next tag, after `make ci-windows-smoke` passes (U2).
* **Rollback.** Revert by phase. U1 alone reverts to `v1.17.2`; U3 to U8 do not depend on each
  other's code, except U8 on 0210 A3's sender. A revert of U4 brings back the PowerShell probe and
  `schtasks.exe`; a revert of U6, the hard kill on Windows `--remove`. Nothing is migrated on a
  host, so nothing has to be undone there.
* *Amendment D:* U3b reverts alone. Dropping `ReplaceBeforeStop` restores the default order. A
  dropped `PostInstall` leaves U3's staged probe in place. G1 and G2 may stay, since they hold
  facts the default order does not need, but which do no harm.
* *Amendment C:* U1b alone reverts to `v1.19.0`. Once U4 or U6 has landed on `Task.Enable` or
  `Status.Disabled`, it cannot revert alone: those phases revert first. *Amendment E:* the same
  holds at `v1.22.0`, and once 0212's stage 1 uses `SupervisedState`, that stage reverts first too.

## Execution record

The owner committed the records as `d8e261c7` and approved U0 to U2 on 2026-10-23: "committed,
proceed with U0–U2". U3 to U8 wait for their own approval, after 0210's steps (the order above).

### U0, 2026-10-23: the library issue

Filed as [go-selfupdate-lib#8](https://github.com/maccavelli/go-selfupdate-lib/issues/8),
"taskscheduler: export Enable and Disable for a task", label `enhancement`.

* **Body:** `Start`'s documented rule, quoted; `Stop`'s step 3; the list of `Task`'s exported
  methods, none of which enables or disables alone; Magic's `startWindows` and `stopWindows` as the
  consumer; and a proposal for `Enable` and `Disable` with the library's error sentinels.
* **Checked against the source before filing:** `Stop` disables through `r.setEnabled(false)`
  (`lifecycle.go:215-226`), `Start` enables through `r.setEnabled(true)`, and on Windows
  `setEnabled` is `IRegisteredTask::put_Enabled` (`com_windows.go:78`). So the operation exists,
  unexported.
* **Before filing,** the disclosure guard's redact preview ran over the body in a scratch
  repository: "would edit 0 file(s)".

### U1, 2026-10-23: the `v1.19.0` pin

* **Step 3 first:** `git ls-remote --tags` gives `refs/tags/v1.19.0^{}` at
  `a6af9dc760a5392134dc62b00e6491b112d0ada5`, and no tag above `v1.19.0`.
* `go get github.com/maccavelli/go-selfupdate-lib@v1.19.0`: "upgraded … v1.17.2 => v1.19.0".
  `go mod tidy`: no output.
* Both `ci.yml` pins now read `@a6af9dc760a5392134dc62b00e6491b112d0ada5 # go-selfupdate-lib
  v1.19.0` (`:712`, `:733`). `git grep` finds no `036289ec` left in `.github`, `go.mod` or `go.sum`.
* **The diff** is the planned shape: `ci.yml` 2 lines, `go.mod` 1, `go.sum` 2 (the module's `h1:`
  and `/go.mod` lines). No Go source changed.

**Gates.** U1 stages no Go file, so `make vulncheck` and the pin tests stood in for the per-file
pre-add check.

| Gate | Result |
| :--- | :--- |
| `make vulncheck` | "clear; allowlisted required-module findings: GO-2026-5932", as before |
| `TestWorkflowPinsMatchGoMod`, `TestReleaseJobsReadTheSpec` | ok (`internal/updateclient`) |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 43 packages ok, 0 FAIL lines |
| `go mod tidy -diff` | no output |
| `make markdownlint-docs`, `make check-records` | exit 0 |

The pin tests' ability to fail was shown by the 0207 REPORT: a copy with only `go.mod` moved
failed them.

### U2, 2026-10-23: the Windows CI script stamps `buildinfo`

**Seen failing first,** on the Windows test host, in a scratch clone of `master` at `d8e261c7`
made from a git bundle (the hash checked equal to the local `HEAD`), not the host's install:

```text
PASS  B12 build dist/*-windows-amd64.exe
PASS  B13 go version -m CGO_ENABLED=0
FAIL  B14 version identity -- C:\Users\<user>\mcr-u2\dist\mcremote-windows-amd64.exe version output did not contain stamped VERSION=0.0.0.gd8e261c7
v0.0.0-20261024002022-d8e261c79aae (local) d8e261c79aae
ci-windows-local: 1 CHECK(S) FAILED
make ci-windows-smoke: exit 2
```

The binary reported the toolchain's VCS version, so the `main.*` stamps were dropped, as the
REPORT's A7 inferred. The base is `0.0.0` because a bundle of `master` carries no tags.

**The change,** `scripts/ci-windows-local.ps1:129-133`: `$ld` is now `-s -w -X
$buildinfo.version=$ver -X $buildinfo.kind=local`, with `$buildinfo` the library's `buildinfo`
package, the `Makefile`'s pair (`Makefile:23`, `:173`). The `$commit` and `$date` lines that fed
only the dropped stamps are gone; `$ver`'s own commit lookup stays.

**After,** the same clone with the working tree's `ci-windows-local.ps1`, `go.mod` and `go.sum`
copied in (so with U1's pin):

```text
PASS  B12 build dist/*-windows-amd64.exe
PASS  B13 go version -m CGO_ENABLED=0
PASS  B14 version identity
ci-windows-local: ALL SELECTED CHECKS PASSED
make ci-windows-smoke: exit 0
```

Both binaries print `0.0.0.gd8e261c7 (local) d8e261c79aae-dirty`: the stamped version, verbatim,
as a local build.

**`make ci-windows` too, beyond U2's steps,** since U1's pin changes what Windows builds:

* **First run:** A2 to A5 passed, and A6 failed one test. The clone had the new `go.mod` but the
  old `ci.yml`, because only three files had been copied in: "`build-selfupdate-release.yml@036289ec…`
  is commented "# go-selfupdate-lib v1.17.2"; want "# go-selfupdate-lib v1.19.0", the release
  go.mod requires" (`spec_test.go:186`). That was the clone's mistake, not the tree's. It also
  shows `TestWorkflowPinsMatchGoMod` failing on a real half-moved pin.
* **With `ci.yml` copied in as well:** "PASS A6 go test ./… (Git Bash, as CI; no -race, no
  live_*)", "ALL SELECTED CHECKS PASSED", `make ci-windows`: exit 0. The smoke passed again.
* The remote clone and its bundle were deleted afterwards.

**Gates.** `make check-records` and `make markdownlint-docs` exit 0. The script is PowerShell, so
no Go gate applies to it.

**A process slip, recorded.** Two remote commands broke the owner's rule of one plain command per
call. `u2_windows.py` first sent `&&`-chained command strings to the host, and one direct call
chained `make ci-windows` with `echo exit=$?`. Both ran only in the scratch clone. The script now
sends single commands (`make -C mcr-u2 …`), and the log was fetched with a plain `scp`.

### U1b, 2026-10-24: the `v1.22.0` pin

**Approval.** The owner answered "U1b, then write 0212 PLAN" on 2026-10-24, with amendment E's
target, `v1.22.0`. The Windows host run of step 5 waits for the owner's word, as the step says.

* **Step 1:** `git ls-remote --tags` gives `refs/tags/v1.22.0^{}` at
  `84805563ed252df384f59ced45a3daf529702ab4` (tag object `b7c2181b…`), and no tag above
  `v1.22.0`.
* **Step 2:** `go get github.com/maccavelli/go-selfupdate-lib@v1.22.0`: "upgraded … v1.19.0 =>
  v1.22.0". `go mod tidy`: no output.
* **Step 3:** both `ci.yml` pins now read `@84805563ed252df384f59ced45a3daf529702ab4 #
  go-selfupdate-lib v1.22.0` (`:712`, `:733`). A script asserted two matches of the old pin before
  replacing, and two of the new after (`m207e/move_ci_pins.py`, session scratchpad).
* **Step 4, the diff,** as planned: `ci.yml` 2 lines; `go.mod` 1; `go.sum` 2, where the module's
  `h1:` line is `h1:AHBwXsYs08WGTteAU7e/1NAa/CqbnTbZx+Xj6LSjKmk=` and its `/go.mod` hash is
  unchanged. No Go source changed. `git grep a6af9dc7 -- .github go.mod go.sum` finds nothing.

**Step 5, the gates** (`m207e/u1b_gates.py`, logs beside it). U1b stages no Go file, so
`make vulncheck` and the pin tests stand in for the per-file pre-add check, as in U1.

| Gate | Result |
| :--- | :--- |
| `go list -m` | `go-selfupdate-lib v1.22.0`, `acp-go-sdk2 v1.20.0` |
| `go mod tidy -diff` | exit 0, no output |
| `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `TestWorkflowPinsMatchGoMod`, `TestReleaseJobsReadTheSpec` (`-v`) | `--- PASS`, each |
| `make test`, `make race` | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make vulncheck` | "clear; allowlisted required-module findings: GO-2026-5932", as before (0174) |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, go1.27.3 linux/amd64, in a copy of the working tree with its own git index | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line. The copy was deleted |
| `make ci-windows` on the Windows host, 2026-10-25, on the owner's word | exit 0: A2 to A6 `PASS`, "ALL SELECTED CHECKS PASSED". See below |

**The Windows run** (`m207e/u1b_windows.py`, log beside it) used a scratch clone of `master` made
from a git bundle, not the host's install, and touched no task. The clone's `HEAD` checked equal to
the local `HEAD`, `93ff4964`. That is the pin commit `f27b743f` plus one commit that adds only the
0213 records, so the Go tree it built is U1b's. The clone and both bundles were deleted afterwards.

```text
PASS  A3 go.mod toolchain
PASS  A2 symlink probe (MC_REQUIRE_SYMLINK=1)
PASS  A4 go build ./...
PASS  A5 go vet ./...
PASS  A6 go test ./... (Git Bash, as CI; no -race, no live_*)
ci-windows-local: ALL SELECTED CHECKS PASSED
make ci-windows: exit 0
```

The pin tests' ability to fail stands from U2's record above, where a half-moved pin failed
`TestWorkflowPinsMatchGoMod`. No new test was added, so there is no new plant.

**Not done in U0 to U2.** U3 to U8 wait for 0210's A2, A3, amendment B and L2, and for their own
approval.

### U3 Deviation 1, 2026-10-25 — the release fixture becomes a runnable image. The owner decided: "option 1, research this. make it as idiomatic, hardened, and robust as possible."

**Approval.** 0210 is complete, so U3's wait is over. The owner answered "pushed. proceed with u3"
on 2026-10-25.

**Found while measuring step 1, before any file was written.** The update tests serve releases
whose binary for every platform is a line of text: `releaseBody` returns `"mcremote <tag>
<os>/<arch>"` (`internal/updateclient/managed_test.go:90-92`). Both of U3's guards refuse that:

* `NewImageVerifier` requires an ELF, Mach-O or PE image for the platform, and has no test seam;
* `NewVersionProber` runs the staged file with the spec's identity arguments, and text does not
  run.

So every existing test that installs a release would fail under U3: those in `managed_test.go`,
`command_test.go` and `managed_script_unix_test.go`. Step 2 foresaw a fixture change ("a runnable
fixture, or the prober's `Run` seam"), but the files list did not name `managed_test.go`, where
the fixture lives. The prober's `Run` seam alone would not do, since the image verifier has none.

**The resolutions put to the owner:**

1. **Chosen:** `managed_test.go` joins U3. `releaseBody` serves the package's own test binary for
   the host platform, and `TestMain` answers its identity arguments with the release tag.
2. A separate fixture program under `testdata/`, built once in `TestMain`. It needs the toolchain
   at test time and the same `managed_test.go` change.
3. Not offered: a seam that replaces the guards in tests. The existing install tests would then
   never pass the real image check, which is a guard turned off.

**The design, from go-selfupdate-lib's own tests.** The library's `selfupdate/probe_test.go`
serves its test binary as the new release, and its `TestMain` prints a version when
`SELFUPDATE_TEST_PRINT_VERSION` is set (`replace_native_test.go:17-34`). U3 follows that pattern,
with these additions:

* **The fixture answers only when it is run exactly as the prober runs it:** a marker variable
  that `TestMain` sets before `m.Run`, and arguments equal to the spec's identity arguments. A
  child started any other way runs the tests as usual, and a probe can never start the whole
  suite again.
* **It prints what a release prints:** `buildinfo.Info{Version: tag, Kind: KindRelease}.String()`,
  with the tag `v0.2.0` by default. A test sets another tag or a non-zero exit with `t.Setenv`.
* **The image is read once, in `TestMain`,** which exits 2 when it cannot read it, so no test
  fails later with a confusing error. Only the host platform's asset is an image. The others stay
  text, since only the host's asset is downloaded.
* **`requireReplaced` reports a size and a SHA-256 when it fails,** not the bytes.
* **`New` fails closed:** a product whose spec has no `identity_args` is refused, and the prober is
  never silently left out.
* **The macOS checker takes a requirement:** `identifier "<the signer's identifier>"`, so the
  staged binary must carry the signature `newTransformer` gave it. It runs before the version
  prober, so a binary whose signature fails is never executed.

The test binary is 19 MB, as measured with `go test -c` on darwin/arm64. That is well under the
library's 512 MiB `Limits.Executable`.

### U3 Deviation 2, 2026-10-25 — the fixture becomes a small program, built once. The owner decided: "D1: option 2"

**Found after Deviation 1 was built, with every gate passing.** Serving the test binary cost time
the plan did not foresee. The binary is 19 MB, and 24 MB under `-race`. A test that installs a
release took about 1.9 s under `-race`, against about 0.15 s for one that stops before the
download. The updateclient package's times, taken from U1b's gate logs (before) and Deviation 1's
gate run (after):

| Run | Before | After Deviation 1 |
| :--- | :--- | :--- |
| `make test`, darwin/arm64 | 11.5 s | 20.6 s |
| `make race`, darwin/arm64 | 13.9 s | 50.7 s |
| `go test -race ./...`, the Linux test host | 5.5 s | 84.5 s |

On the Linux test host it became the slowest package, beyond `internal`'s 69.3 s, so the whole
race run grew.

**The resolutions put to the owner:** keep the test binary and accept the cost, or Deviation 1's
second resolution, a small fixture program built once (recommended, chosen).

**The design, from go-selfupdate-lib's `archive/main_test.go` and `internal/testproc.Build`:**

* **The program is `testdata/fixture/main.go`, a module of its own** (`go.mod`, `go 1.27.3` as
  the main module). It imports only the standard library. The go command ignores `testdata`, so
  `go build ./...`, `go vet ./...` and `go mod tidy` never see it.
* **`TestMain` builds it once,** into a temporary directory that it removes after the tests:
  `go build -trimpath -buildvcs=false -ldflags="-s -w"`, with `GOOS` and `GOARCH` the host's,
  `CGO_ENABLED=0`, `GOWORK=off` and `GOFLAGS=` cleared. The build is therefore the same under
  `-race`, `-cover` or any `GOFLAGS`, and writes nothing in the tree. A build that fails, or a
  missing go command, exits 2 with the go command's output.
* **It answers only `version`.** Run with any other arguments, it fails with exit 2. `TestMain`
  refuses a spec whose `mcremote` identity arguments are not exactly `["version"]`, so the two
  cannot drift apart unnoticed.
* **What it prints and how it exits:** `<tag> (release)`, which is `buildinfo.Info.String()` for a
  release with no revision. The tag is `v0.2.0` unless `MCREMOTE_UPDATECLIENT_FIXTURE_VERSION` is
  set, and the exit status is `MCREMOTE_UPDATECLIENT_FIXTURE_EXIT`'s, where a value that is not a
  number exits 2.
* **Deviation 1's marker variable goes.** It kept the test binary from starting its own suite
  again, and a separate program cannot do that.
* **The rest of Deviation 1 stands:** `releaseBody`, `describe`, `foreignImage`, `New` failing
  closed, and the checker's requirement.

### U3 Deviation 3, 2026-10-25 — the Windows case gets its own test file. The owner decided: "D2: add a windows case"

**Found while writing step 1's fourth test.** `refresh_test.go` is built for Unix only
(`//go:build unix`), so it can hold the symlink case but not the case on Windows that step 1 names.
`internal/cli/service/refresh_windows_test.go` joins U3: `binaryWarning` reports no warning for this
test binary's path in another case, and still warns for another file. The Windows run of
`make ci-windows` is what runs it, and it also runs the fixture and the PE patch on Windows. That run
needs the owner's word.

### U3, 2026-10-25: the update's guards

**The change, as built after Deviations 1 to 3:**

* **`client.go`:**
  * `httpClient` returns the given client, or `selfupdate.DefaultHTTPClient()`, which has no
    `Client.Timeout`.
  * `Config.Verifiers` is `NewImageVerifier(selfupdate.Platform{})`, the runtime platform.
  * `Config.Probes` comes from `newProbes`:
    * on macOS with `MC_CODESIGN_IDENTITY`, first `codesign.NewChecker` with the requirement
      `identifier "com.magiccliremote.<product>"`;
    * then `NewVersionProber(<the product's identity_args>, nil, 30 s)`;
    * a product with no `identity_args` is an error.
* **`refresh.go`:** `binaryWarning` compares with `libservice.SameExecutable`.
* **The tests:**

  | Test | What it pins |
  | :--- | :--- |
  | `TestUpdateRefusesAReleaseThatIsNotForThisPlatform` | text, and a host image patched to another architecture: `ErrIntegrity`, no stop, start or refresh, the executable untouched |
  | `TestUpdateRefusesAStagedBinaryThatCannotRunHere` | a fixture that exits 3, and one that reports `v0.1.9`: "staged binary failed a probe", no stop, the executable untouched |
  | `TestNewUsesTheLibraryHTTPClient` | no `Client.Timeout`, its own transport, a given client kept |
  | `TestNewProbesTheStagedBinary` | one probe for each product, two on macOS with an identity, an error without `identity_args` |
  | `TestFixtureAnswersOnlyItsIdentityArguments` | the fixture's contract, which the refusal tests rely on |
  | `TestBinaryWarningKnowsTwoSpellingsOfOneBinary` (Unix) | a symlink to the test binary gives no warning; another file warns |
  | `TestBinaryWarningIgnoresCaseOnWindows` (Windows) | the test binary's path in another case gives no warning; another file warns |

* **The fixture** is 1.6 MB on darwin/arm64, against the test binary's 19 MB. Building it in
  `TestMain` writes nothing in the tree (`git status` shows only U3's files).

**Seen failing first.** `m207u3/plants.py` (session scratchpad) ran each plant on its own scratch
copy: `git archive HEAD` of `go.mod`, `go.sum`, `cmd` and `internal`, with U3's files laid over it
and one plant applied by an asserted replacement. A plant counts only when `go test` exits non-zero
on the named test's `--- FAIL`, or on `TestMain`'s message. A build failure does not count, and
the script's first run refused three such plants until they were rewritten to compile.

| Plant | The failure |
| :--- | :--- |
| U3's `client.go` over the committed tests (Deviation 1's evidence) | `TestUpdateStartsAStoppedServiceOnlyWhenEnabled`: "selfupdate: mcremote: selfupdate: integrity check failed", each subtest |
| no image verifier | `TestUpdateRefusesAReleaseThatIsNotForThisPlatform`: the probe still refused both binaries ("staged binary failed a probe: … fork/exec …"), but the test requires `ErrIntegrity`, so it failed |
| no probes | `TestUpdateRefusesAStagedBinaryThatCannotRunHere`: "managed install failed", past the stop |
| the client with `Timeout: OperationTimeout` | `TestNewUsesTheLibraryHTTPClient`: "Client.Timeout = 15m0s, want none" |
| no probe and no error without `identity_args` | `TestNewProbesTheStagedBinary`: "a product with no identity_args was given no probe and no error" |
| `binaryWarning` comparing strings | `TestBinaryWarningKnowsTwoSpellingsOfOneBinary`: "binaryWarning(a symlink to this binary) = ["the definition runs …"]" |
| `foreignImage` patching nothing | `TestUpdateRefusesAReleaseThatIsNotForThisPlatform`: "the patched fixture is not a darwin/amd64 image" |
| `TestMain` expecting other identity arguments | the run stops before any test: "updateclient: release fixture: … testdata/fixture answers only [version]" |
| a fixture that does not compile | the run stops: "go build in testdata/fixture: exit status 1", with the compiler's message |
| a fixture that answers any arguments | `TestFixtureAnswersOnlyItsIdentityArguments`: "stdout "v0.2.0 (release)\n", exit 0; want "", exit 2", for no, other and extra arguments |

The Windows test was not planted: this host cannot run it. It runs in `make ci-windows`.

**Gates** (`m207u3/u3_gates.py`, logs beside it):

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the 7 Go files | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| The new tests (`-count=1 -v`) | `--- PASS`, each, subtests included |
| `make test`, `make race` | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs`, markdownlint on this plan | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, go1.27.3 linux/amd64, in a copy of the working tree with its own git index | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line. The copy was deleted |

**The updateclient package's time**, which Deviation 2 was about:

| Run | Before U3 | Deviation 1 | Built |
| :--- | :--- | :--- | :--- |
| `make test`, darwin/arm64 | 11.5 s | 20.6 s | 14.5 s |
| `make race`, darwin/arm64 | 13.9 s | 50.7 s | 23.0 s |
| `go test -race ./...`, the Linux test host | 5.5 s | 84.5 s | 12.3 s |

The difference that remains is the fixture's build, and the probe each install test now runs.

**The Windows run, 2026-10-25, on the owner's word ("proceed")** (`m207u3/u3_windows.py`, log
beside it). It used a scratch clone of `master` made from a git bundle, not the host's install, and
touched no task. The clone's `HEAD` checked equal to the local `HEAD`, `6c664d9b`, and U3's eight
files were copied in: five modified, three new, as `git status` in the clone showed.

```text
PASS  A3 go.mod toolchain
PASS  A2 symlink probe (MC_REQUIRE_SYMLINK=1)
PASS  A4 go build ./...
PASS  A5 go vet ./...
PASS  A6 go test ./... (Git Bash, as CI; no -race, no live_*)
ci-windows-local: ALL SELECTED CHECKS PASSED
make ci-windows: exit 0
```

The new tests then ran verbosely in the same clone, so that each is seen by name on Windows: the
fixture built as a `.exe`, the PE patch, and the case test.

```text
--- PASS: TestFixtureAnswersOnlyItsIdentityArguments (0.06s)
--- PASS: TestNewUsesTheLibraryHTTPClient (0.00s)
--- PASS: TestNewProbesTheStagedBinary (0.00s)
--- PASS: TestUpdateRefusesAReleaseThatIsNotForThisPlatform (0.03s)
    --- PASS: …/not_an_executable (0.01s)
    --- PASS: …/an_image_for_another_architecture (0.02s)
--- PASS: TestUpdateRefusesAStagedBinaryThatCannotRunHere (0.09s)
--- PASS: TestUpdateStartsAStoppedServiceOnlyWhenEnabled (0.13s)
ok      github.com/maccavelli/magic-cli-remote/internal/updateclient  0.473s
--- PASS: TestBinaryWarningIgnoresCaseOnWindows (0.00s)
ok      github.com/maccavelli/magic-cli-remote/internal/cli/service  0.021s
```

The fixture's seven subtests and the refusals' subtests all passed (the log keeps each line). The
clone and both bundles were deleted afterwards.

**U3 is complete.** Every step and every gate above passed. It is staged for the owner.

**Not done in U3.** U3b, U4, U5, U6 and U8, each of which waits for its own approval. U6 also waits
for 0212's P3.

### U3b Deviation 1, 2026-10-25 — the fixture fails only once installed. The owner decided: "option 1, proceed"

**Approval.** The owner committed U3 as `17ccfd21` and answered "committed, proceed" on 2026-10-25,
which approves U3b, and with it the MADR's amendment D. Step 5's host runs still wait for the
owner's word, per host.

**Found while writing step 2's probe test.** U3 runs the same version prober on the staged file,
before anything is installed. A release whose binary fails `version` is therefore refused there,
with or without `PostInstall`. So the probe test as step 2 words it passes either way, and the
confirmation "without `PostInstall` this sees a `Stop`" cannot be met. To test D34 on its own, the
binary must pass while staged and fail once installed. The library stages it as
`.<product>.selfupdate-*` beside the target (`session.go:98` at `v1.22.0`) and installs it under the
product's name, so the fixture can tell the two runs apart by its own file name.

**A process slip, recorded.** The switch was written into `testdata/fixture/main.go` before the
files list was checked, and the file is not on it. Nothing else depended on the change when the
owner was asked.

**The resolutions put to the owner:**

1. **Chosen:** the fixture joins U3b. With `MCREMOTE_UPDATECLIENT_FIXTURE_FAIL_NAMED` set, it exits
   1 when its own file has that name. The probe test sets it to the installed name, and the
   fixture's contract test gains the case.
2. Test `PostInstall` below `New`, with a managed installer built in the test. This leaves `New`'s
   wiring untested.
3. Drop the separate test, and amend the MADR's confirmation. `PostInstall` would then never be
   seen to work.

### U3b, 2026-10-25: replace before the stop, steps 1 to 4 and 6

**Step 1, measured.**

* U3 landed as `17ccfd21`. Its prober is built in `newProbes` (`client.go`), and its fixture is
  `testdata/fixture`.
* **The `version` run** (`m207u3b/measure_version.py`, session scratchpad): both products built as
  a release is, each copied to a fresh path and run 10 times on each OS. Run 1 is a newly written
  file's first run, which is what the post-install probe meets.

  | Binary | First run | Warm median (max) |
  | :--- | :--- | :--- |
  | mcremote, darwin/arm64 (this Mac) | 773 ms | 30 ms (36 ms) |
  | mcremote, linux/amd64 (the Linux test host) | 13 ms | 15 ms (24 ms) |
  | mcremote, windows/amd64 (the Windows test host) | 265 ms | 39 ms (41 ms) |
  | mcrelay, darwin/arm64 | 711 ms | 26 ms (27 ms) |
  | mcrelay, linux/amd64 | 23 ms | 22 ms (30 ms) |
  | mcrelay, windows/amd64 | 236 ms | 44 ms (45 ms) |

  The worst case is under a second, so D34's 30 s stands, with a margin of about 40 times. The
  remote scratch directories were deleted.

**Step 3, the code.**

* `managed.go`: `managedInstaller` builds `NewManagedInstallerWith(…,
  selfupdate.ManagedOptions{ReplaceBeforeStop: true})`, with a comment naming the premise and the
  two guards that hold it.
* `client.go`: `newProbes` also returns the version prober, and `New` sets
  `InstallOptions.PostInstall` to it. One prober serves both `Config.Probes` and `PostInstall`.

**Step 2, the tests:**

| Test | What it pins |
| :--- | :--- |
| `TestUpdateReplacesTheBinaryBeforeTheStop` | at `Stop` the target already holds the new release's bytes; one stop, one start |
| `TestUpdateRollsBackAReleaseThatFailsOnceInstalled` | a release that passes the staged probe and fails once installed (Deviation 1): "installed binary failed its probe", no stop, start or refresh, the previous bytes back |
| `TestUpdateReportsTheReplacementBeforeTheStop` | the `--json` result has `"replaced_before_stop":true` |
| `TestNothingRunsItsOwnExecutableWhileServing` (G1) | no non-test file under `cmd` or `internal` refers to `os.Executable` or `os.Args[0]`, or calls `procutil.Command` with `"mcremote"` or `"mcrelay"`, outside the allowlist of four files and their reasons. Imports are resolved per file, a dot import of `os` or `procutil` is itself a site, and a stale entry fails |
| `TestDefinitionsRunTheBinaryOnlyToStart` (G2) | for both products: the systemd unit, the plist, the task XML, and the runit, s6 and OpenRC scripts. The binary's path appears once, on the start command's line, and no unit has `ExecStop`, `ExecStopPost` or `ExecReload`. It is built on every OS |
| `TestNoSupervisorFinishScript` (G2) | no non-test file of `internal/cli/service` names a `finish` script |
| `TestFixtureAnswersOnlyItsIdentityArguments` | gains the cases for the fixture's new switch |

**Step 4, the live test.** `live_replacefirst_test.go`, `TestLiveReplaceFirstProbeFailure`, and
`make live-replacefirst`.

* It runs only with `MCREMOTE_REQUIRE_REPLACEFIRST=1`, and then it requires
  `MCREMOTE_REPLACEFIRST_UNIT` and an absolute `MCREMOTE_REPLACEFIRST_BINARY`.
* It builds the updater with `New`. `newBackend` is the real backend of the named unit, wrapped to
  count `Stop` and `Start`.
* The release is the fixture, set to fail once installed. Because the failure comes only at the
  installed path, the swap happens under the running daemon, and it is rolled back.
* **The daemon's PID comes from:**
  * launchd's `Status.PID` on macOS;
  * Task Scheduler's `Status.PIDs` on Windows;
  * on Linux, under any supervisor, the one process whose `/proc/<pid>/exe` is the target.
* **It asserts:**
  * the post-install refusal, with `ReplacedBeforeStop` true;
  * no `Stop` or `Start`;
  * the original bytes;
  * the same PID and `procutil.ProcessStartToken`;
  * the service still running.
* A cleanup restores the original bytes if any other bytes are left.
* Without its variables it skips, as every gate run shows. It has not run against a service yet:
  that is step 5.

**The guides.** Both described the old order in their own words. The Linux guide's numbered list
said "stop … swap", and the Windows guide said "stops it … It then replaces the binary". So the order
changed in each list, rather than a sentence being added beside a list that would contradict it.
These are the two files the step names.

**Seen failing first** (`m207u3b/plants.py`). Each plant ran on its own scratch copy: `git archive
HEAD` with U3b's changed and new files laid over it, and one plant applied by an asserted
replacement or a new file. 13 plants, 13 failures as expected:

| Plant | The failure |
| :--- | :--- |
| `ReplaceBeforeStop: false` | `TestUpdateReplacesTheBinaryBeforeTheStop`: "at Stop the target is 15 bytes, … want the new release, 1587874 bytes" |
| the same | `TestUpdateReportsTheReplacementBeforeTheStop`: "kind "result", applied true, replaced_before_stop false" |
| no `PostInstall` | `TestUpdateRollsBackAReleaseThatFailsOnceInstalled`: "managed install failed", past the stop, which is the MADR's "without `PostInstall` … sees a `Stop`" |
| `os.Executable()` in a new `internal/daemon` file (the MADR's G1 plant) | G1: "`zz_g1plant.go:6:12: os.Executable`" |
| a stale allowlist entry (the MADR's G1 plant) | G1: "`ownExecutableAllowlist["internal/daemon/daemon.go"] no longer finds …`" |
| `os` imported as `xos`, used as a function value | G1: "`zz_g1plant.go:5:15: os.Executable`" |
| `os.Args[0]` | G1: "`zz_g1plant.go:5:32: os.Args[0]`" |
| `procutil` imported as `pu`, `pu.Command(ctx, "mcrelay", "version")` | G1: "`procutil.Command(…, "mcrelay", …)`" |
| a dot import of `os` | G1: "`zz_g1plant.go:3:8: dot import of os`" |
| `ExecStop={{.Binary}} stop` in the mcremote unit template (the MADR's G2 plant) | G2, `mcremote/systemd_unit`: "the binary appears 2 times, want once" |
| OpenRC's `stop()` given `--exec <binary>` | G2, `openrc-user_script` for both products: "the binary appears 2 times" |
| a `"finish"` literal in a new `internal/cli/service` file | `TestNoSupervisorFinishScript`: "`zz_g2plant.go:3:17 names a supervisor finish script`" |
| the fixture's switch disabled | `TestFixtureAnswersOnlyItsIdentityArguments`, "run under the name it must fail as": "exit 0; want "", exit 1" |

**Gates** (`m207u3b/u3b_gates.py`, logs beside it):

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the 8 Go files | "8 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| The new tests (`-count=1 -v`) | `--- PASS`, each; the live test `--- SKIP` without its variables |
| `make -n live-replacefirst` | `MCREMOTE_REQUIRE_REPLACEFIRST=1 go test ./internal/updateclient/ -run '^TestLiveReplaceFirstProbeFailure$' -count=1 -timeout 300s -v` |
| `make test`, `make race` | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` (the guides), markdownlint on this plan and the MADR | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, go1.27.3 linux/amd64, in a copy of the working tree with its own git index | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line; updateclient 14.1 s, cli/service 6.8 s. The copy was deleted |

**The Windows run, 2026-10-25, on the owner's word ("run make ci-windows")**
(`m207u3b/u3b_windows.py`, log beside it). It used a scratch clone of `master` made from a git
bundle, not the host's install, and touched no task. The clone's `HEAD` checked equal to the local
`HEAD`, `17ccfd21`, and the 13 files U3b changed or added were copied in.

```text
PASS  A3 go.mod toolchain
PASS  A2 symlink probe (MC_REQUIRE_SYMLINK=1)
PASS  A4 go build ./...
PASS  A5 go vet ./...
PASS  A6 go test ./... (Git Bash, as CI; no -race, no live_*)
ci-windows-local: ALL SELECTED CHECKS PASSED
make ci-windows: exit 0
```

U3b's tests then ran verbosely in the same clone:

```text
--- PASS: TestFixtureAnswersOnlyItsIdentityArguments (0.07s)
--- PASS: TestNewProbesTheStagedBinary (0.00s)
--- SKIP: TestLiveReplaceFirstProbeFailure (0.00s)
--- PASS: TestUpdateReplacesTheBinaryBeforeTheStop (0.09s)
--- PASS: TestUpdateRollsBackAReleaseThatFailsOnceInstalled (0.08s)
--- PASS: TestUpdateReportsTheReplacementBeforeTheStop (0.09s)
--- PASS: TestNothingRunsItsOwnExecutableWhileServing (0.05s)
ok      github.com/maccavelli/magic-cli-remote/internal/updateclient  0.532s
--- PASS: TestDefinitionsRunTheBinaryOnlyToStart (0.01s)
--- PASS: TestNoSupervisorFinishScript (0.01s)
ok      github.com/maccavelli/magic-cli-remote/internal/cli/service  0.034s
```

The fixture's nine subtests, including both new cases for its switch, passed. So did G2's twelve
definitions (the log keeps each line). On Windows, the post-install test swapped a real `.exe`
under the target's name, refused it, and restored the previous file. The clone and both bundles
were deleted afterwards.

**Steps 1 to 4 and 6 are complete,** and staged for the owner.

**Not done in U3b yet:**

* **Step 5's host runs, each on the owner's word:** L-a, the success runs, and L-b,
  `make live-replacefirst`, on:
  * Linux and macOS, on the 0210 kit's scratch services;
  * runit, s6 and OpenRC, in the containers;
  * Windows, on the real task by W1 to W4.

  The plan stays `in-progress` until they are recorded.

### U3b step 5, 2026-10-25: macOS and Linux, on the scratch services

**Approval.** The owner committed U3b's steps 1 to 4 and 6 as `1f7ed945`, and answered "committed
proced", which approves step 5's host runs. Run first on the two scratch-service hosts, as the 0210
kit sets them up.

**The setup:**

* **The kit:** the 0210 kit (R0), rebuilt from `HEAD` `1f7ed945`. The scratch service is
  `mcremote-d3probe`, on port 17631, under `~/mcr-d3probe`. The host's real service is never touched.
* **The new phases:** `u3b_run.py`, beside the kit in the session scratchpad:
  * `shell-update <tag>` runs `update --yes --force --version <tag> --json` from a shell, against
    the scratch service;
  * `handoff-result` reads the result after the kit's `inside-agent` phase.
* **The release:** the update installs the latest published release, `v0.23.0`. So the code that
  updates is U3b's, and the binary that is probed and swapped in is a real release's.

**macOS (launchd):**

| Run | Outcome |
| :--- | :--- |
| L-b, `make live-replacefirst` | `--- PASS`: "installed probe of mcremote failed: exit status 1", "the replacement was rolled back"; `replaced_before_stop=true`, no `Stop` or `Start`, the same PID and start token, the same bytes |
| L-b with `ReplaceBeforeStop: false`, on a scratch copy of `HEAD` (`plant_live.py`) | `--- FAIL`, as it should: "the binary was not replaced before the stop", "the update called Stop 1 and Start 1 times, want neither", "PID 34297's start token is "" … the daemon was replaced" |
| L-a from a shell, to `v0.23.0` | `applied=True, replaced_before_stop=True, service_started=True, rolled_back=False`; PID 34816 → 35135; `/healthz` up, binary `v0.23.0 (release)` |
| L-a by the agent | `update handed off: 001e398dcb47dd34 (launchd job …)`; the result has `replaced_before_stop: true`, `service_started: true`, `rolled_back: false`; PID 35437 → 35522; `/healthz` up on `v0.23.0` |

* **Cleanup.** The scratch job was removed (`setup-service --remove` exit 0; `launchctl print` exit
  113, not found).
* **The handoff's own job stayed loaded.** The one-shot job had finished with exit 0, but it was
  still loaded with its plist, because no daemon of this build restarted after it to clear it. That
  is the case 0210's L1.2 recorded. It was unloaded with `launchctl bootout`, and its plist deleted.

**Linux, systemd** (the Linux test host; `u3b_linux.py`, then `u3b_linux_lb.py`):

| Run | Outcome |
| :--- | :--- |
| L-b, `make live-replacefirst` | `--- PASS`, as on macOS |
| L-a from a shell, to `v0.23.0` | `applied=True, replaced_before_stop=True, service_started=True, rolled_back=False`; PID 3015050 → 3015618; `/healthz` up on `v0.23.0` |
| L-a by the agent | `update handed off: c597c2f93bd14b1d (transient unit mcremote-d3probe-selfupdate-c597c2f93bd14b1d.service)`. The transient unit's journal runs through to "mcremote: updated dev -> v0.23.0", with no "reception is disabled" line. The result has `replaced_before_stop: true`, `service_started: true`, `rolled_back: false`; the scratch unit is `active`/`running` on PID 3015821 |

**A finding, fixed within U3b's files.** The first Linux run's plant did not fail on the stop and the
daemon. It failed before the update ran: "0 processes run /home/<user>/mcr-d3probe/mcremote, want
exactly one daemon".

* **The cause.** The live test found the Linux daemon by `/proc/<pid>/exe`. The passing L-b just
  before it had swapped the binary under the running daemon and rolled it back, and after that the
  daemon's `exe` link no longer named the target.
* **The driver was too loose.** It counted any `--- FAIL` as the expected failure, so this plant
  proved nothing.
* **The fix,** in `live_replacefirst_test.go`, which is one of U3b's files. `pidByCommandLine` finds
  the one process whose first argument is the binary, by its named or resolved path, and which has
  `serve` among its arguments. That is what the supervisor started, and a rename under the process
  does not change it. The 0210 kit finds its daemon the same way.
* **The plant check is now strict.** It must fail with "want neither" and "the daemon was replaced",
  and without "want exactly one daemon".

**Linux, again, with the fixed test** (`u3b_linux_lb.py`):

| Run | Outcome |
| :--- | :--- |
| L-b 1 | `--- PASS` |
| L-b with `ReplaceBeforeStop: false` | `--- FAIL`, as it should: "the update called Stop 1 and Start 1 times, want neither", "PID 3015975's start token is "" … the daemon was replaced" |
| L-b 2, after the plant's swap and rollback under the running daemon | `--- PASS`: the case the first lookup could not handle |

* **Cleanup.** The scratch unit and its `.prev` backup were removed, and `list-unit-files` no
  longer lists the unit. The kit, the two source trees and `~/mcr-d3probe` were deleted.
* **macOS again with the fixed file.** L-b passed (`--- PASS`), and the scratch job was removed
  again. The macOS path reads launchd's PID, so the fix does not change what it checks.
* **Gates on the fix:**
  * `make pre-add-check` on the file: "1 file(s) clean (gofmt, golint, govulncheck)";
  * `go vet` for windows/amd64 and linux/amd64;
  * `go test ./internal/updateclient` passes.

~~**Not done in step 5 yet,** each on the owner's word:~~ Both ran on 2026-10-25, below.

### U3b step 5, 2026-10-25: Windows on the real task, and runit, s6 and OpenRC in the containers

**Approval.** The owner committed the macOS and Linux record and answered "committed, run both step 5
tests".

**Windows, the real task, by W1 to W4** (`u3b_win.py` on this Mac, `u3b_win_host.py` on the host,
beside the kit).

* **How the run was arranged.** W1 backed up the installed binary and the config beside
  themselves. Every later step ran inside a `try`, with W4 in its `finally`.
* **This change's build** (the kit's `mcremote-windows-amd64.exe`, built from `1f7ed945`, the same
  product code as `HEAD`) was put at the installed path with the task stopped, and the task started.
* **L-b** ran `make live-replacefirst` in a tree of `HEAD`, with `MCREMOTE_REPLACEFIRST_UNIT=mcremote`
  (the task name is fixed, MADR 0159 D13) and the installed path.

| Step | Outcome |
| :--- | :--- |
| W1 | `v0.23.0 (release)`; the task `Running`; one paired device; no one-shot task |
| L-b 1 | `--- PASS`: `replaced_before_stop=true`, "installed probe of mcremote failed: exit status 1", rolled back; no `Stop` or `Start`; the same PID and start token |
| L-b with `ReplaceBeforeStop: false` | `--- FAIL`, as it should: "the update called Stop 1 and Start 1 times, want neither", "PID 17076's start token is "" … the daemon was replaced" |
| L-b 2, after a swap and a rollback | `--- PASS` |
| L-a from a shell, to `v0.23.0` | `applied=True, replaced_before_stop=True, service_started=True, rolled_back=False`; PID 57136 → 39380; `/healthz` answers |
| L-a by the agent (W2, W3), from this change's build again | `update handed off: c51a8ce7d857c413 (task \mcremote.selfupdate.c51a8ce7d857c413)`. The result has exit code 0, `replaced_before_stop=True, service_started=True, rolled_back=False`; `/healthz` answers on `v0.23.0`; no one-shot task left |
| W4 | the device revoked; "binary restored byte for byte (v0.23.0 (release) …); config restored byte for byte; devices as before; task Running"; no one-shot task after that start's cleanup; the backups deleted |

W3's transcript scan reads the newest session files, so it also printed two lines from 0210's own
Windows runs: the refusal of its R1 and L1, and the handoff `68b00105…` of its L2.3. This run's
handoff is `c51a8ce7…`, the ID in the result file.

**runit, s6 and OpenRC,** in the 0210 kit's containers (local Podman, linux/arm64; `u3b_containers.py`).

* **The setup.** A fresh container per supervisor, as the non-root `tester` user. The scratch
  service was installed from this change's build.
* **How L-b runs there.** The images have no Go toolchain, so the driver brings the test in:
  * the updateclient test binary, cross-compiled with `go test -c`, clean and planted;
  * `testdata/fixture`;
  * go1.27.3 linux/arm64, from go.dev and checked against the SHA-256 that go.dev publishes, for
    `TestMain` to build the fixture.

  It runs `TestLiveReplaceFirstProbeFailure` with the make target's variables. The container has no
  `make`, so this is the target's command, run directly.

**The first container run counts only in part.** Two mistakes in the driver, neither in U3b:

* **runit's L-b never ran:** "the go command is required to build the fixture: exec: "go":
  executable file not found". The Void image has no `tar` or `gzip`, so the toolchain was never
  unpacked, and the driver did not check.
* **s6's L-b never ran:** "mcremote-d3probe must be running before the test: running=false". The
  kit installs the scratch service with its own `XDG_STATE_HOME` and `XDG_DATA_HOME`. runit and s6
  find a service's directory under `XDG_DATA_HOME`, and the driver ran the test without those two
  variables. OpenRC's definition is under `XDG_CONFIG_HOME`, which the kit leaves alone.
* **The fix and the rerun.** The driver now unpacks the toolchain on this Mac and copies the
  directory in. It checks `go version` before anything else, and passes the scratch service's two
  variables, as the kit's own `update` has them. runit and s6 then ran again in full.
* **OpenRC's first run counts.** It had no such problem, and its rows below are from that run.

| Supervisor | L-b 1 | L-b planted | L-b 2 | L-a from a shell, to `v0.23.0` | The agent-run update |
| :--- | :--- | :--- | :--- | :--- | :--- |
| runit (Void) | `--- PASS` | `--- FAIL`, as it should: "Stop 1 and Start 1 times, want neither", "PID 29's start token is "" … replaced" | `--- PASS` | `replaced_before_stop=True, service_started=True, rolled_back=False`; PID 718 → 826; `/healthz` up | refused: "this update runs inside the mcremote service and cannot hand off here"; no download event; PID 856 throughout |
| s6 (Alpine) | `--- PASS` | `--- FAIL`, as it should: "… PID 26's start token is "" … replaced" | `--- PASS` | the same fields; PID 730 → 840 | refused, the same; PID 877 throughout |
| OpenRC (Alpine) | `--- PASS` | `--- FAIL`, as it should: `* Stopping mcremote-d3probe … [ ok ]`, `* Starting mcremote-d3probe … [ ok ]`, "… PID 189's start token is "" … replaced" | `--- PASS` | the same fields; PID 1100 → 1400 | refused, the same; PID 1531 throughout |

Each container's scratch service was removed (`setup-service --remove` exit 0, and OpenRC's `.prev`
backup removed too), and each container was deleted. `podman ps -a` lists none. The Podman machine
was stopped again, as it was before.

**A difference from the plan, waiting for the owner.** Step 5 and the MADR's D36 expect an agent-run
update that hands off under runit, s6 and OpenRC, with `replaced_before_stop: true`. That cannot
happen yet. Under those supervisors an update from inside the service refuses before it downloads,
because no handoff exists for them (0210-MADR D8). The library's supervised adapter brings one, but
adopting it is 0212's work. What ran is the refusal, with the service untouched. The replacement
before the stop is shown there by the update from a shell and by L-b.

### U3b Deviation 2, 2026-10-25 — the agent-run success under runit, s6 and OpenRC moves to 0212. The owner decided: "committed. proceed"

**Found in step 5's container runs** (above). Step 5 and MADR D36 expected the agent-run update to
hand off under runit, s6 and OpenRC. Under 0210-MADR D8 it refuses there before any download,
because those supervisors have no handoff yet. The refusal was what ran, with no download event and
the PID unchanged.

**The resolutions put to the owner:**

1. **Chosen:** close U3b as it stands. Record the refusal as D8's, by design. The agent-run success
   run for those three supervisors moves to 0212's P6, whose supervised adapter brings the handoff.
2. Keep U3b open until 0212 lands, then run the container agent case under U3b.

The owner answered "committed. proceed" after the two were put, with the first recommended. That is
taken as the first. The note sits on MADR D36, and 0212's PLAN P6 now names the check.

**U3b is complete.**

* Steps 1 to 4 and 6 were committed as `1f7ed945`, and the live test's lookup fix as the next
  commit.
* **Step 5** passed on macOS, Linux (systemd), Windows (the real task, restored byte for byte),
  runit, s6 and OpenRC:
  * L-b and its plant, everywhere;
  * the update from a shell, everywhere;
  * the agent-run handoff where a handoff exists: launchd, systemd and Task Scheduler.

**Not done in this plan.** U4, U5, U6 and U8, each on its own approval. U6 also waits for 0212's P3.
The plan stays `in-progress`.

### U4 Deviation 1, 2026-10-25 — `health.go` reads the task too. The owner decided: "option 1."

**Approval.** The owner committed U3b's closing records and answered "committed, proceed" to U4
being next.

**Found by step 1's measurement, before any edit.** The facts table names three readers of the
stored definition (`setup_schtasks.go:26`, `refresh_schtasks.go:38`, `uninstall.go:223`). There is a
fourth: `health.go:119` runs `schtasks /query /tn <task> /xml ONE` to recover the serve options.
`backendProbe` calls it on every health check of an update. `health.go` is not on U4's files list.

**The resolutions put to the owner:**

1. **Chosen:** `health.go` and `health_test.go` join U4, and the export becomes `Task.Definition`
   through the same seam.
2. Leave it to an amendment of its own. U4 would then ship with one `/query /xml` call, and the
   plan's check after U4 and U6, no `"/query"` outside tests and hints, could not pass.

**What step 1 measured:**

| Site | Today | U4 |
| :--- | :--- | :--- |
| `control_schtasks.go:25-44` `taskState` | PowerShell `Get-ScheduledTask` | `Task.Status` |
| `control_schtasks.go:48`, `:93`; `refresh_schtasks.go:31`; `uninstall.go:224` | `taskState`'s callers | `Status` |
| `setup_schtasks.go:26`; `refresh_schtasks.go:38`; `uninstall.go:228`; `health.go:119` | `schtasks /query /xml` | `Task.Definition` |
| `setup_schtasks.go:61`; `refresh_schtasks.go:158` | `schtasks /create /xml`, through a UTF-16 temporary file | `Task.Register` |
| `setup_schtasks.go:86`, `:89` | `/query`, then `/delete` | `Task.Delete` |
| `setup_schtasks.go:142` `sameTaskDefinition`; `refresh_schtasks.go:84` | Magic's field comparison | `SameDefinition`, and `DiffDefinitions` for the message |
| `setup_schtasks.go:32`; `refresh_schtasks.go:78` `taskDisabled` | read from the stored XML | `Status.Disabled` (D31) |
| `control.go` `ProbeStatus` | three backend calls | one `Status` |
| `control_schtasks.go:62`, `:65`, `:90`, `:100` | `/change`, `/run`, `/end` | kept: U6's, by D30 |

**The test files it finds:**

* `schtasks_test.go`, `task_compare_test.go`, `refresh_schtasks_test.go`, `nostart_schtasks_test.go`
  and `uninstall_test.go`;
* `installerhook_test.go`, which uses `taskDisabled` and `withTaskDisabled`;
* `control_schtasks_windows_test.go`;
* `schtasks_windows_test.go`, which tests `encodeTaskXML` against the real `schtasks.exe` and goes
  with it;
* `health_test.go`, by this deviation.
* *Corrected while the tests were written:* `migrate_test.go`, whose `migrationHost` stubbed both
  old seams, and `control_test.go`, for `ProbeStatus`. Both are test files the measurement finds,
  as step 1 provides. The list above left them out.

### U4, 2026-10-25: Task Scheduler through the library, steps 1 to 3 and 5

**The seam** (`schtasks.go`):

* `taskClient` is the four calls U4 uses: `Status`, `Definition`, `Register` and `Delete`.
* `newTaskClient` returns `taskscheduler.New(Options{Name: name})`. `openTask` checks the name
  first, as `taskNameRe` did for the PowerShell probe.
* `taskStatus`, `taskDefinitionOf`, `registerTaskXML` and `taskDiffPaths` are the helpers the
  callers share.

**The change, by site:**

* `taskState` and its PowerShell probe are gone. `isInstalledWindows` and `stopWindows` read
  `Status`.
* **Setup** reads `Status`, then `Definition`, and compares with `DiffDefinitions`.
  * A changed task without `--force` is refused, and the error names each path that differs.
  * A comparison error without `--force` is refused, never taken as unchanged.
  * It registers with `Register`.
  * `--no-start` keeps a task disabled when `Status.Disabled` says so (D31).
* **Remove** reads `Status`, stops, then calls `Delete`.
* **Refresh** reads `Status` and `Definition`, renders disabled when `Status.Disabled`, compares with
  `DiffDefinitions`, and registers with `Register`.
* **The rest:**
  * the uninstall's ownership check and `health.go` read `Definition`;
  * `ProbeStatus` takes one `Status` from a backend that has it;
  * `encodeTaskXML`, the temporary files, `sameTaskDefinition`, `sameTaskAccount` and
    `taskDisabled` are gone;
  * `taskFieldsFromXML` stays: the uninstall reads the program with it, and the tests read what a
    render holds.
* **What remains of `schtasks.exe`:** `/change … /enable`, `/run`, `/change … /disable` and
  `/end`, U6's by D30. `git grep -i -e powershell -e '"/create"' -e '"/query"' -e '"/delete"'` in
  the package's non-test files finds only two comments.

**The tests.** Every Windows test now runs on `fakeTaskScheduler`.

* **How the fake behaves:**
  * tasks are kept as their stored definitions;
  * a definition's `Settings/Enabled` is the switch, and `Register` sets it;
  * `switchOff` sets the switch apart from the definition, so a test can tell which one the code
    reads;
  * the fake also records `schtasks.exe`.
* **Step 2's tests and amendment C's:**

  | Test | Pins |
  | :--- | :--- |
  | `TestSetupSchtasksRegistersAnAbsentTask` | an absent task registers once, with exactly the render; no `Definition` read |
  | `TestSetupSchtasksIdempotent` | an unchanged task registers nothing |
  | `TestSetupSchtasksNamesWhatDiffers` | without `--force`, the refusal names each path `DiffDefinitions` reports; nothing registered, nothing started |
  | `TestSetupSchtasksPropagatesAFailedRead` | a failed read is the error, nothing registered or started |
  | `TestRemoveSchtasksDeletesThroughTheLibrary`, `TestRemoveSchtasksIdempotent` | one `Delete`, no `schtasks /delete` or `/query`; an absent task deletes nothing |
  | `TestProbeStatusOnWindowsReadsOnce` | one `Status` read, and no per-state call |
  | `TestUninstallOnWindows`, `TestUninstallLeavesAnotherFoldersTask` | the program comes from `Definition`; another folder's task is left |
  | `TestAStoppedUnchangedTaskNeedsNoForce` | D31: a stopped, current task reads unchanged, with and without `--no-start` |
  | `TestNoStartKeepsAStoppedWindowsTaskStopped`, `TestNoStartReadsTheSwitchFromTaskScheduler` | D31: `--no-start` keeps a task disabled by `Status.Disabled`, whatever the stored text says |
  | `TestRefreshSchtasksReadsTheSwitchFromTaskScheduler` | the same for refresh |
  | `TestTaskStatusDrivesInstalled`, `TestOpenTaskRejectsAnUnsafeName` | one read; an error is no "not installed"; an unsafe name reaches nothing |

* **`task_compare_test.go`** now holds the library's comparison to Magic's own definitions:
  * a real export (the v0.17.4 fixture) compares the same as its render;
  * every field setup controls is detected;
  * the switch is not compared (D31);
  * unreadable input never compares unchanged.
* **The Windows-only tests:**
  * `schtasks_windows_test.go` keeps 0116's guard against a definition Task Scheduler cannot parse
    (F25). It now hands a poisoned render to the real `Register`, never registering (C8). It
    classifies by the stable HRESULTs `0x80041318` and `0x8004131A`, with ~~a truncated definition~~
    as its negative control. *U4 Deviation 3, 2026-10-26: the library refuses a truncated definition
    before Task Scheduler sees it; the control is now a repeated attribute.*
  * `control_schtasks_windows_test.go` reads an absent task through the real `Status`.
  * Both run in `make ci-windows`.

**A library finding** (`m207u4/diffprobe`, session scratchpad, against `v1.22.0`):

* `DiffDefinitions` returns one difference and no error for `""`, `"not xml"` and `"<Other/>"`, and
  an error for malformed XML (`"<Task><Triggers>"`). Its documentation says that "malformed XML on
  either side is an error".
* `SameDefinition` returns `true` alongside that error. A caller that checks only the boolean takes
  malformed XML as unchanged.
* Magic calls `DiffDefinitions` and checks the error first, and `TestUnreadableNeverComparesUnchanged`
  holds that. Filing the finding in the library is the owner's call.

**Seen failing first** (`m207u4/plants.py`). Each plant ran on its own scratch copy with U4's files
laid over `HEAD`. All 11 fail on their named tests:

| Plant | The failure |
| :--- | :--- |
| every comparison "the same" | `TestSetupSchtasksRegistersWhenChanged`: "Unchanged = true for a different definition"; `TestSetupSchtasksNamesWhatDiffers`: "a changed task was overwritten without --force" |
| setup reads the switch from the XML | "a task Task Scheduler reports disabled was registered enabled" |
| refresh ignores the switch | `TestRefreshSchtasksKeepsADisabledTaskDisabled` and `…ReadsTheSwitchFromTaskScheduler`: registered enabled |
| remove never deletes | "calls ["status mcremote" "status mcremote"] … want one delete" |
| `ProbeStatus` asks three times | "ProbeStatus asked Installed separately", "Status was read 0 times" |
| a read error swallowed in `taskStatus` | "isInstalledWindows = false, <nil>; want false, err=true" |
| a read error swallowed in setup | "err = <nil>, want the read's permission error" |
| unreadable taken as unchanged | "setup over "<Task><Triggers>": err <nil> … schtasks ["/change /tn mcremote /enable" "/run /tn mcremote"]" |
| the uninstall reads no program | `TestUninstallOnWindows`: nothing removed |
| health reads another task | "read scheduled task "mcrelayx": … not installed" |
| no name check | "taskStatus("a b") accepted an unsafe name", and the names reached Task Scheduler |

**The first run of the plants found a gap in the tests, now closed.** Plant 8 did not fail. Several
new tests did not stub `schtasks.exe`. Under a plant, setup went on to start the task through the
real one, which is absent on macOS, and that error read as the refusal the test wanted. On the
Windows host the same regression would have run `schtasks /change /enable` and `/run` against the
real `mcremote` task.

* `withTaskScheduler` now records `schtasks.exe` too, so no test using the fake reaches it.
* The refusal tests assert the refusal's message, and that nothing was started.
* All 11 plants were run again and failed as above.

**Gates** (`m207u4/u4_gates.py`):

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` on the 18 changed Go files | "18 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| The Windows-path tests (`-count=1 -v`) | 108 `--- PASS` lines, tests and subtests |
| `make test`, `make race` | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs`, markdownlint on this plan and the MADR | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host, go1.27.3 linux/amd64 | exit 0; 43 packages ok. The copy was deleted |

**Not done in U4 yet.** Step 4, on the owner's word: `make ci-windows`, which runs the two
Windows-only tests, then the real task by W1 to W4, with amendment C's `--no-start` over the
stopped task.

### U4 Deviation 2, 2026-10-25 — an unused test field reddened CI. The owner decided: "option 1"

**Found** in CI run `40233848601`, the first run on U4's `9c0c5e2b`, pushed with
[0215-PLAN-test-darwin-arm64-natively-in-ci.md](0215-PLAN-test-darwin-arm64-natively-in-ci.md)'s
`40c151e5`. `Go (test)` failed at `Staticcheck (pinned; GOOS linux, darwin, windows)`:

```text
internal/cli/service/refresh_schtasks_test.go:19:2: field creates is unused (U1000)
```

**Cause.** U4 moved `refreshHarness` onto `fakeTaskScheduler`. `registered()` reads
`h.sched.registered`, so nothing writes or reads `creates` any more. U4's gates, this plan's
"Before staging" list, did not include `make staticcheck`, so the finding first ran in CI.
`make staticcheck` on HEAD reproduces it for all three GOOS, and it is the only finding.

**The resolutions put to the owner:**

1. **Chosen:** fix it here, as a U4 deviation: delete the field, show `make staticcheck` failing
   on HEAD and passing with the fix, run U4's gates, and add `make staticcheck` to this plan's
   "Before staging" list so U5 to U8 run it.
2. Delete the field without this record or the gate change, leaving the same gap for U5 to U8.

**Files:** `internal/cli/service/refresh_schtasks_test.go`, already on U4's list, and this plan.

**The fix.** `refreshHarness` loses its `creates` field. Nothing else changes, and no test changes
what it asserts.

**Seen failing first** (`m207u4/u4d2_gates.py`, session scratchpad): `make staticcheck` in a
`git archive HEAD` copy exited 2 with
`internal/cli/service/refresh_schtasks_test.go:19:2: field creates is unused (U1000)`, once each for
GOOS linux, darwin and windows. The copy was deleted.

**Gates:**

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0; staticcheck v0.8.2 for linux, darwin and windows, no findings |
| `make pre-add-check` on the changed Go file | "1 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| The U4 and refresh tests (`-count=1 -v`) | exit 0 |
| `make test`, `make race` | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs`, markdownlint on this plan | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 43 packages ok. The copy was deleted |

**CI.** The owner committed this as `dfa65b1f` and pushed it. CI run `40328847048` on `dfa65b1f`
concluded `success`, `Go (test)`'s staticcheck step included.

### U4 Deviation 3, 2026-10-26 — the Windows negative control cannot run. The owner decided: "fix these"

**Found** in step 4's `make ci-windows` run on the Windows host, at `9c0c5e2b`.
`TestRenderedTaskParsesInRealTaskScheduler`'s negative control skipped:

```text
unclassified rejection of the truncated definition: selfupdate: taskscheduler: register
\mc-0207-u4-probe-1d4b13e4: the definition is not readable XML: XML syntax error on line 27: unexpected EOF
```

**Cause.** go-selfupdate-lib `v1.22.0`'s `Register` parses the definition in Go before it calls
Task Scheduler (`registrationLogon`, `taskscheduler/register.go:143-146`). A definition Go cannot
parse never reaches Task Scheduler, so the truncated control can never produce `0x8004131A`. The
main subtest's "could not parse" branch has never been seen to fire on a real host. U4's record, "a
truncated definition as its negative control", describes a control that does not run.

**The resolutions put to the owner:**

1. **Chosen:** replace the truncated input with one Go parses and Task Scheduler rejects as
   malformed. A probe on the Windows host finds it, in a scratch clone, registering nothing. Then
   both subtests are seen failing on planted input there.
2. Make the control assert the library's own rejection instead. That no longer proves Task
   Scheduler's result can be classified, so it is a workaround.
3. Leave it skipping.

**Files:** `internal/cli/service/schtasks_windows_test.go`, already on U4's list, and this plan.

**The probe** (`m207u4/d3/zz_probe_windows_test.go`, session scratchpad). This ran in a scratch clone of
`dfa65b1f` on the Windows host, through the library's `Register`, on throwaway names. Each name was deleted
and then confirmed absent. Every input that kept a `Task` root also carried the invalid `ExecutionTimeLimit`:

| Input | Result |
| :--- | :--- |
| the poisoned render (control) | `HRESULT 0x80041318` |
| root `<Other/>`; root renamed; an unknown element in `Settings` | `HRESULT 0x80041316` (unexpected node) |
| a wrong namespace | `HRESULT 0x80041317` |
| duplicate `Settings`; no `Actions`; `version="9.9"`; text inside `Task` | `HRESULT 0x80041318` |
| **a repeated attribute** (`version="1.4" version="1.4"`) | **`HRESULT 0x8004131A`** |
| an undeclared namespace prefix; a `DOCTYPE` with an internal subset; an XML declaration mid-document | `HRESULT 0x8004131A` |
| an attribute holding `&#0;` | refused in Go: "the definition is not readable XML … illegal character code U+0000" |

Nothing registered. Go's decoder accepts four inputs that Task Scheduler's parser rejects as malformed.

**The change.** The negative control, now "a definition only Task Scheduler cannot parse", repeats the rendered
definition's `version` attribute. The test refuses to build the control when that attribute is not there exactly
once, and its classification is unchanged. A comment cites this deviation.

**Seen failing first** (`m207u4/d3/plants_drive.py`, on the Windows host's clone):

| Variant | Result |
| :--- | :--- |
| as fixed | both subtests `--- PASS` |
| the control fed the merely invalid definition | `--- FAIL`: "a malformed definition read as a value error, so this test no longer discriminates: … HRESULT 0x80041318" |
| the main subtest fed the repeated attribute | `--- FAIL`: "Task Scheduler could not parse the rendered definition: … HRESULT 0x8004131A" |

The second plant is the first time the main subtest's "could not parse" branch has fired on a real host. The clone
was deleted.

### U4 Deviation 4, 2026-10-26 — `--force` over an unchanged task registers nothing. The owner decided: "fix these"

**Found** in step 4's real-task run: `setup-service --force` over the unchanged `mcremote` task left
the stored task file's time unchanged, and so did the `--force --no-start` witness.

**Cause.** Step 4's "`--force` (re-registers)" is wrong about the code before and after U4. An
identical definition reads unchanged, with or without `--force` (D17). The run never exercised
overwriting an **existing** task through `Register`: the fresh install after `--remove` registered
an absent one.

**The resolutions put to the owner:**

1. **Chosen:** correct the step's wording, and add one real-task step that exercises the overwrite
   path:
   * `setup-service` with one option changed, `--log-level info`, which `serveArgs` writes into the
     task's arguments: refused without `--force`, naming the differing path;
   * the same with `--force`: registered, and the stored time moves;
   * the original options with `--force`: registered again, and the export is W1's again;
   * the `--no-start` witness, and the same backup and final restore as step 4.
2. Correct only the wording, and leave the overwrite path unproven on a real host.

**Files:** this plan. The run uses scratch scripts only.

**The run** (`m207u4/u4d4_win.py`, session scratchpad), on the Windows host's real `mcremote` task. HEAD's build,
`dfa65b1f` with this deviation's test file, ran only `setup-service`. Its options were recovered from the stored
task, as in step 4, and it backed up and restored as step 4 did:

| Step | Result |
| :--- | :--- |
| W1 | installed v0.23.0 (release), task Running; export and the one refresh backup saved |
| `setup-service … --log-level info` | exit 1: "scheduled task "mcremote" exists with different content: Actions/Exec[1]/Arguments (pass --force to overwrite)"; stored time and export unchanged |
| the same with `--force` | exit 0; the stored time moved; the export carries `--log-level info`; the daemon's PID unchanged |
| the original options with `--force` | exit 0; the stored time moved again; the export is W1's |
| the `--no-start` witness | the stored time unchanged: nothing registered |
| final | binary, config, export and refresh backup as W1; task Running; `/healthz` answers; the kit was deleted |

`Register` overwrote an existing task on a real host, and setup refused the change without `--force`, naming the
path `DiffDefinitions` reported.

### U4 Deviations 2 to 4: gates, 2026-10-26

`m207u4/u4d34_gates.py`, on the working tree with Deviation 3's test file:

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the changed Go file | "1 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 43 packages ok |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 43 packages ok. The copy was deleted |

With these, U4's step 4 is done. U4 is complete once the owner commits it, and the next CI run confirms it.

### U5 Deviation 1, 2026-10-26 — three more test files steer `launchdLoaded`. The owner decided: "option 1, proceed"

**Approval.** The owner committed U4's Deviations 2 to 4 as `775f5a56` and answered "committed,
proceed" to U5 being next.

**Found before any edit.** `launchdLoaded` reads `launchctl print` through the `runLaunchctlCapture`
seam (`launchd_wait.go:54-57`). Test files outside U5's list stub that seam to steer it through
setup, `--remove` and uninstall. Once it reads `Job.Loaded`, those stubs no longer reach it:

* off macOS, `launchd.New` refuses with `ErrUnsupported`, so the teardown would see an error;
* on a Mac, the tests would query the real launchd, the leak `setup_launchd_test.go`'s comment
  says it closed (MADR 0125 P5).

**The resolutions put to the owner:**

1. **Chosen:** those files join U5. `launchdLoaded` gets its own seam, defaulting to the library's
   `Job.Loaded` and overridable through an exported `OverrideLaunchdLoaded`. Each test sets it
   beside its existing stub and answers "loaded" or "not loaded" explicitly. No assertion is
   loosened.
2. Move only `ProbeStatus` now, and leave `launchdLoaded` on `launchctl print` until U6, with a MADR
   amendment moving D19's second half there.
3. Route the library's job through an adapter over `runLaunchctlCapture`, so the old stubs still
   work. A workaround: those stubs return no exit code, so the adapter cannot tell 113 from a
   failure, which is what D19 is for.

**The measurement, after the decision.** A grep found four files. A trace over `go test ./...` on a
scratch copy (`m207u5/trace_loaded.py`, session scratchpad) logged each `launchdLoaded` call and
each `launchctl print`, with the caller:

| Test file | Tests that reach `launchdLoaded` |
| :--- | :--- |
| `launchd_wait_test.go` (U5's own) | the five `TestStopAndWait…` |
| `launchd_sequence_test.go` | `TestDarwinSetupWaitsForTeardownBeforeBootstrap`, `…RestartsInPlaceWhenPlistUnchanged`, `…ReportsAStuckTeardown`, `TestDarwinRemoveWaitsForTeardownWhenLoaded` |
| `setup_launchd_test.go` | `TestDarwinSetupAgentOrder`, `TestDarwinSetupNeverTouchesSystemDomain`, `TestDarwinRemoveAgent` |
| `uninstall_unix_test.go` | `TestUninstallOnMacOS` |
| `setup_test.go` | `TestRemove`, unstubbed (below) |

* `nostart_launchd_test.go` never reaches `launchdLoaded`. `--no-start` skips the teardown, and its
  stub answers only `launchdJobRunning`. It does not join.
* In `launchd_sequence_test.go`, `launchdLoaded` consumed every `print` answer in the queue, except
  the in-place restart's second `Setup`, where `launchdJobRunning` read it. One queue answering both
  seams in call order keeps each sequence.
* `TestRemove` runs the host's own service path. On macOS it reads the real launchd for
  `com.magiccliremote.mcremote-rm`, a label nothing installs, and only reads; elsewhere it never
  reaches launchd. It stays as it is. `TestSetupSecretEnvTightensMode` and
  `TestSetupWritesUnitWithoutCopyingBinary` read the real `launchctl print` the same way through
  `launchdJobRunning`, which U6 removes.

**Files added to U5:** `internal/cli/service/launchd_sequence_test.go`, `setup_launchd_test.go` and
`uninstall_unix_test.go`. `OverrideLaunchdLoaded` lives in `launchd_wait.go`, beside its seam,
because `setup.go`, where the other overrides live, is not on the list.

### U5, 2026-10-26: launchd status through the library, steps 1, 2 and 4

**The seams:**

* `ProbeStatus` reads a backend that is a `launchdStatusReader` with one `Status` call, as it reads
  a `taskStatusReader` on Windows. `*launchd.Job` is one, by a compile-time assertion beside the
  Task Scheduler one.
* `launchdLoaded(svc)` returns `(bool, error)`. It calls `launchdLoadedProbe`, by default
  `jobLoaded`: the library's `Job.Loaded` for the job `svc` names, `gui/<uid>/<label>`, run through
  `launchdRunner` (nil is the library's own runner). A target in another form is an error.
  `OverrideLaunchdLoaded` replaces the probe for tests.

**The change, by site:**

* **`ProbeStatus`** on macOS takes `Installed`, `Loaded`, `Enabled` and `Running` from one probe.
  It no longer runs `launchctl print` itself. A failed probe is "service probe failed: …", where
  `print`'s failure read as "bootout left down". `Enabled` and `Active` still need the plist, as
  before, and `Loaded` does not, also as before.
* **`stopAndWaitDarwin`** returns the probe's error when the probe fails before the bootout, and
  boots nothing out. A probe that fails during the wait is returned too, carrying the bootout's
  error when there was one. It is not `ErrLaunchdStillLoaded`. Its callers are unchanged:
  * setup reports "the existing job could not be torn down: read whether … is loaded: …";
  * `--remove` and the uninstall discard the error, as they discarded every teardown error before.
* **What still reads `launchctl print`:** `launchdJobRunning`, `launchdLastExitNote` and
  `settle.go`. They are U6's.

**The tests** (step 1, and Deviation 1's files):

| Test | Pins |
| :--- | :--- |
| `TestProbeStatusOnMacOSReadsOnce` | one `Status` read, no per-state call, no `launchctl` and no `launchdLoaded`; seven states, among them "bootout left down", a job loaded without its plist and a probe error |
| `TestStopAndWaitRefusesToGuessOnAFailedProbe` | a probe that fails before the bootout is the error, and nothing is booted out |
| `TestStopAndWaitReportsAProbeThatFailsMidWait` | a probe that fails after the bootout is the error, not `ErrLaunchdStillLoaded`, and carries a failed bootout's error |
| `TestLaunchdLoadedTellsNotLoadedFromAFailedProbe` | on macOS, the real `Job.Loaded` over a scripted runner: exit 0 is loaded, 113 is not loaded, and exit 5, exit 1 and a runner that could not run are errors; the job reads the label `svc` names. Elsewhere, `ErrUnsupported`, never "not loaded" |
| `TestLaunchdLoadedRejectsAnotherTarget` | a `user/…` or `system/…` target, a non-numeric uid or a target without a label is an error, and no `launchctl` runs |

* `TestProbeStatusReadsTheBackend` loses its two darwin cases, which moved to
  `TestProbeStatusOnMacOSReadsOnce`, and the `loaded` column only they read.
* `launchd_wait_test.go`'s `scriptedLaunchctl` answers through `OverrideLaunchdLoaded` and can fail a
  given probe. Its five existing tests are otherwise unchanged.
* `launchd_sequence_test.go`'s `scriptedPrint` became `scriptedLaunchd`: one queue answering
  `launchdLoaded` and `launchctl print` in call order, as the trace in Deviation 1 measured.
* `setup_launchd_test.go`'s three tests and `TestUninstallOnMacOS` answer "not loaded" through
  `OverrideLaunchdLoaded` beside their existing stubs.

**Seen failing first** (`m207u5/plants.py`, session scratchpad). Each plant ran on its own scratch
copy of the working tree. All ten fail on their named tests:

| Plant | The failure |
| :--- | :--- |
| `ProbeStatus` runs `launchctl print` for "loaded" again | "ProbeStatus ran launchctl [print gui/<uid>/com.magiccliremote.mcremote] itself"; a running job read as "bootout left down" |
| `ProbeStatus` never consults the backend's `Status` | "ProbeStatus asked Installed separately", "Status was read 0 times, want once" |
| `ProbeStatus` takes "loaded" from the plist | "bootout left down": "loaded but not running — run: launchctl kickstart -k …", want "bootout left down" |
| a failed probe before the bootout returns nil | "want the probe's error, got <nil>" |
| a failed probe during the wait returns nil | `…FailsMidWait/bootout_succeeded`: "want the probe's error, got <nil>" |
| the wait's error drops the bootout's | `…FailsMidWait/bootout_failed`: "want the bootout error carried through" |
| `jobLoaded` swallows `Job.Loaded`'s error | exit 5, exit 1 and the runner error: "launchdLoaded = false, <nil>; want false, error true" |
| the job reads another label | "launchctl ran ["managername" "list com.example.other"]; want it to read com.magiccliremote.mcremote" |
| a `user/…` target is accepted | "launchctl [print …] ran for a target launchdLoaded must refuse"; "= true, <nil>; want an error" |
| `scriptedLaunchd` ignores the queue for `launchdLoaded` | `TestDarwinSetupWaitsForTeardownBeforeBootstrap`: "a loaded job must be booted out"; the stuck-teardown and remove tests fail too |

**The plants found a gap in the tests, now closed.** The `user/…` plant did not fail on its first
run. Every target the test refused also failed the length or the uid check, so the domain check was
never what refused it. The test now has a `user/501/…` target, and a runner that fails the test if
`launchctl` runs. Both plants were run again and failed as above. A second plant was miswritten and
did not compile; it was rebuilt as above.

**The trace again**, on the plants' copy with every call to the real `Job.Loaded` logged: only
`TestRemove` reaches it, for `com.magiccliremote.mcremote-rm`, as Deviation 1 recorded.

**Gates** (`m207u5/u5_gates.py`):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the seven changed Go files | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 43 packages ok; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 43 packages ok |
| U5's tests on the Linux test host | `TestLaunchdLoadedTellsNotLoadedFromAFailedProbe` passes off macOS |
| that test, with `jobLoaded` planted to return "not loaded" when the job cannot be built | `--- FAIL`: "launchdLoaded off macOS = false, <nil>; want false and ErrUnsupported". The copy was deleted |

### U5 step 3, 2026-10-26: `mcremote doctor` on macOS. The owner said: "run step 3"

`m207u5/u5_doctor.py`, session scratchpad. It ran `mcremote doctor`, which only reads, against the
real `mcremote` LaunchAgent, with three binaries. None of them wrote to the repository or the
service. The builds were written to the scratchpad.

| Binary | Version | The service section |
| :--- | :--- | :--- |
| the installed release | 0.20.1 | present, loaded and active: yes; "OK: service is loaded and running"; no `enabled:` line |
| a build of `git archive HEAD`, `775f5a56`, without U5 | dev (local) | present, loaded, enabled and active: yes; "OK: service is loaded and running" |
| this tree's build, with U5 | `775f5a56acfe-dirty` | the same as HEAD's |

* The plist path is `/Users/<user>/Library/LaunchAgents/com.magiccliremote.mcremote.plist` in all
  three.
* **"Before" is HEAD's build, not the installed release.** The release predates the `enabled:` line
  and doctor's last three sections, so it cannot show what U5 changed.
* **HEAD and U5 print the same doctor output.** All six sections, service, TCC, credentials,
  engines, silent commands and the update handoff, match byte for byte. U5's service section comes
  from the library's one `Status` probe, where HEAD's ran `launchctl print` for "loaded".

With this, U5's four steps are done. U5 is complete once the owner commits it, and the next CI
run, with its darwin/arm64 leg, confirms it.

**CI, 2026-10-26.** The owner committed U5 as `dc97fb01` and pushed it with U4's `775f5a56`. CI run
`40420371644` on `dc97fb01` concluded `success`. Every job passed, `Go (test)` with its staticcheck
step and `Go (darwin/arm64)` included. U4 and U5 are complete.

### U8 step 1 and Deviation 1, 2026-10-26 — the sender's caller moves with it, and mcrelay needs a start timeout. The owner decided: "option 1, fix the mcrelay gap"

**Approval.** The owner committed and pushed U5 as `dc97fb01` and said "committed and pushed.
proceed". U7 is retired into 0212 (its R2), and U6 waits for 0212's P3, so U8 is next.

**Step 1's measurement:**

| What | Where |
| :--- | :--- |
| mcrelay is ready: the listener is open, with its TLS wrapper | `relay/server.go:239-269`, then `Serve` |
| mcrelay begins to shut down | `server.go:289`, the context ends and the drain begins; `:297`, serving failed |
| What runs before the listener opens | `relay/cli.go:270` `ApplyTLS`: Let's Encrypt issuance, synchronous, bounded by `certs.ACMEStartupTimeout`, 3 minutes (`certs/acme.go:30`, applied at `acme.go:149` and `acme_http.go:106`) |
| 0210 A3's kept-socket sender | `daemon/daemon_notify.go`, unexported |
| Its users | `daemon.go:218`, `:691`, `:711`, `:737`, `:753`; its tests, `daemon/notify_unix_test.go:209-270` |
| The daemon's logging of a send | `notifySystemd`, `daemon.go:41-53` |
| An older mcrelay unit is refreshed | `relay/cli.go:469` `RefreshUnit`, which the managed update runs as `setup-service --refresh` (`updateclient/managed.go:34`) |
| A rollback restores the unit | `managed.go:84-94`: the definition's `.prev`, with the old binary |

**Found 1: the move changes two files outside U8's list.** `daemon.go` calls the sender, and
`internal/daemon/notify_unix_test.go` tests it. Neither is on the list.

The resolutions put to the owner:

1. **Chosen:** both join U8. The sender moves to a shared package, `daemon.go`'s five calls move
   to it, and the sender's own tests move with it. The daemon's end-to-end notify tests stay.
2. Give mcrelay its own copy of the sender: two copies of one protocol, against the step's "moved
   where both daemons can import it".
3. Leave a forwarding wrapper in `daemon_notify.go`, so `daemon.go` does not change. A workaround:
   the wrapper would exist only to stay inside the list.

**Found 2: `Type=notify` alone would leave mcrelay unstartable on a first issuance.** Under
`Type=notify`, systemd waits for `READY=1` for `TimeoutStartSec`, which defaults to the manager's
`DefaultTimeoutStartSec`, 90 s. mcrelay can spend up to 3 minutes in `ApplyTLS` before it listens.
A slow issuance at boot would be killed and restarted in a loop, where `Type=simple` never waits.
mcremote's unit has `TimeoutStartSec=240` for the same bound (MADR 0054 amendment S8). The owner
decided to fix it here: mcrelay's unit gets `TimeoutStartSec=240`, and the unit test ties both
units' timeouts to `certs.ACMEStartupTimeout`.

**Evaluated at the owner's ask,** whether option 1 and that fix are the most robust, idiomatic and
platform-compatible fixes. They are, with three refinements:

* **The library first.** v1.22.0 exports `systemd.Notify`, `Ready` and `Stopping`, but `Notify`
  reads `NOTIFY_SOCKET` on every call and never unsets it (`systemd/notify.go:21-22`). Once the
  variable is unset, as 0210 D10 decided by `sd_notify(3)`'s own `unset_environment` practice, it
  cannot send. No variant takes a kept address. So the sender stays Magic's, in one package both
  daemons import.
  * A library export taking the kept address would shrink that package to its take-and-unset.
    Filing it is outward-facing and the owner's call; it is not filed.
* **Platforms.** The package needs no build tags. `net.DialUnix` compiles everywhere, and only
  systemd sets `NOTIFY_SOCKET`. Under launchd, Task Scheduler, runit, s6 and OpenRC, and in a
  terminal, a send is "not sent" with no error. Go does not support `unixgram` on Windows, and the
  sender never dials there. An abstract `@` name is Linux's, and Go's `syscall` maps it.
* **Moved as it is.** The sender ran live in 0210's L2. It moves unchanged, and the daemon's logging
  helper moves too, so both daemons log a failed send the same way. *Refinement 1.*
* **Where mcrelay sends.**
  * `READY=1` goes in `Serve`, once the listener is open and its serve loop started. That covers
    `ListenAndServe` and a caller's own listener. A connection the kernel queues before the loop
    accepts it is served, so "listening" is the right moment, as in the daemon.
  * `STOPPING=1` goes when shutdown begins, before the drain, on both exit paths, as in the
    daemon's `gracefulDrain`.
  * mcrelay is the unit's main process. `Type=notify` takes messages from the main process by
    default (`NotifyAccess=main`), so nothing else is needed.
  * `serve` takes the socket before anything else runs. mcrelay spawns no child today, so this
    guards the next one. *Refinement 2.*
* **The timeout.**
  * A fixed `TimeoutStartSec` works on every systemd.
  * `EXTEND_TIMEOUT_USEC` (systemd 236 and later) would keep the 90 s default outside ACME. But it
    needs a ticker inside `internal/certs`, outside U8's list, and would differ from mcremote's unit.
  * 240 s is the 3-minute bound plus a minute, as mcremote's is.
  * The test asserts each unit's timeout exceeds `certs.ACMEStartupTimeout`, besides the literal, so
    raising the bound without the units fails a test. *Refinement 3.*
* **Rollback and downgrade.** A rollback restores the unit's `.prev`, `Type=simple`, with the old
  binary (`managed.go:84-94`), so the update never runs an old mcrelay under the notify unit. A
  binary copied back by hand, with no refresh, would time out at start. mcremote has had the same
  exposure since S8.
* **Not changed:** the static examples in `deploy/systemd/` stay `Type=simple`, as mcremote's did at
  S8. `Type=simple` works with every release, and `make verify-units` checks them as they are.

**Files added to U8:** `internal/daemon/daemon.go` and `internal/daemon/notify_unix_test.go`. The
shared package is `internal/sdnotify`: `sdnotify.go`, where `daemon_notify.go`'s sender moves, and
`sdnotify_unix_test.go`, where its tests move. The relay files the measurement found are
`internal/relay/server.go`, `cli.go`, and a new `notify_unix_test.go`. *Corrected while the code was
written:* `internal/relay/config.go` too, which holds the server's `Config`, the start code's input.
It is relay start code the measurement finds, as U8's list provides, and the list above left it out.

### U8, 2026-10-26: mcrelay tells systemd it is ready, steps 2, 3 and 5

**The shared sender** (`internal/sdnotify`):

* `Take()` reads and unsets `NOTIFY_SOCKET`. `Socket.Send(state)` writes one datagram. Both are
  `daemon_notify.go`'s `takeNotifySocket` and `send`, unchanged.
* `Socket.Notify(log, state)` sends and logs, and is the daemon's `notifySystemd`, unchanged.
* `daemon_notify.go` is gone. `daemon.go` takes the socket with `sdnotify.Take()` and sends its
  `READY=1` and three `STOPPING=1` with `notify.Notify`. What it sends, and when, is unchanged.

**mcrelay:**

* `serve` takes the socket first, before the console detach, `Load` and `ApplyTLS`, and hands it to
  the server as `Config.Notify`.
* `Serve` sends `READY=1` once the listener is open and its serve loop started.
* It sends `STOPPING=1` when shutdown begins: on the context's end before the drain, and on a
  serving error before that path's drain.
* An empty socket sends nothing, so tests, terminals and every other service manager are
  unaffected.

**The unit.** `mcrelay.user.service.tmpl` is `Type=notify` with `TimeoutStartSec=240`, each with a
comment naming its reason.

**The tests:**

| Test | Pins |
| :--- | :--- |
| `TestServeNotifiesSystemd` (relay) | the real `serve` command against a unixgram listener: `READY=1` arrives, and the listener then accepts a connection; `NOTIFY_SOCKET` is unset after start-up; a cancelled context brings `STOPPING=1`, and `serve` returns nil |
| `TestServeNotifiesStoppingWhenServingFails` (relay) | a listener closed under `Serve` brings `STOPPING=1` after `READY=1`, and `Serve` returns the listener's error |
| `TestUnitIsTypeNotify` | both units: `Type=notify`, `TimeoutStartSec=240`, no `Type=simple`, and a timeout longer than `certs.ACMEStartupTimeout`. It replaces "mcrelay must stay `Type=simple`" |
| `TestRefreshMovesASimpleUnitToNotify` | for mcremote and mcrelay, an earlier release's `Type=simple` unit with no start timeout is refreshed to the current template (step 3) |
| `TestSocketSends` (sdnotify) | the daemon's `TestNotifySocketSends`, moved with the sender: a path, an abstract name on Linux, and no socket |
| `TestNotifyLogsAFailedSend` (sdnotify) | a failed send is a warning naming the state, and no socket logs nothing |

The daemon's `TestRunNotifiesSystemd` and `TestRunTakesItsNotifySocket` stay where they are,
unchanged, and pass.

**Seen failing first** (`m207u8/plants.py`, session scratchpad). Each plant ran on its own scratch
copy of the working tree. All eleven fail on their named tests:

| Plant | The failure |
| :--- | :--- |
| `Serve` sends no `READY=1` | both relay tests: "waiting for READY=1: … i/o timeout" |
| no `STOPPING=1` when the context ends | `TestServeNotifiesSystemd`: "waiting for STOPPING=1: … i/o timeout" |
| no `STOPPING=1` when serving fails | `TestServeNotifiesStoppingWhenServingFails`: the same |
| `serve` reads the variable and leaves it set | "NOTIFY_SOCKET = "…/notify" after start-up, want it unset" |
| `serve` never hands the socket to the server | "waiting for READY=1: … i/o timeout" |
| the mcrelay unit stays `Type=simple` | `TestUnitIsTypeNotify/mcrelay`: "mcrelay unit lacks "Type=notify"", "still says Type=simple" |
| the mcrelay unit has no start timeout | "mcrelay unit lacks "TimeoutStartSec=240"", "the unit has no TimeoutStartSec" |
| `certs.ACMEStartupTimeout` raised to 5 minutes | "mcremote unit: TimeoutStartSec 4m0s is not longer than certs.ACMEStartupTimeout 5m0s", and the same for mcrelay |
| `Take` leaves the variable set | `TestSocketSends/path`: "Take left NOTIFY_SOCKET set" |
| `Notify` drops a failure | "a failed send logged ""; want a warning naming READY=1" |
| the daemon sends no `READY=1` | `TestRunNotifiesSystemd`: "waiting for READY=1: … i/o timeout" |

* **No plant for "READY=1 before the listener".** `Serve` is handed an open listener, so `READY=1`
  cannot come before it. A plant moving the send earlier, into `serve`, would race a listen that
  takes microseconds, and could pass by luck. The dial after `READY=1` stays as the daemon's test
  has it.
* **One test mistake, fixed before the plants.** The first run of `TestNotifyLogsAFailedSend`
  expected `state=READY=1`, but slog's text handler quotes a value holding `=`, as
  `state="READY=1"`. The code was right, and the assertion now matches.

**Gates** (`m207u8/u8_gates.py`):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` on the nine changed and added Go files | "9 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok, `internal/sdnotify` the new one; no `FAIL`, panic or `DATA RACE` line |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 44 packages ok |
| U8's tests on the Linux test host, verbose | all pass; `TestSocketSends/abstract` runs there, where macOS skips it. The copy was deleted |

### U8 step 4, 2026-10-26: a scratch mcrelay unit on the Linux test host. The owner said: "you may run the linux host check"

`m207u8/u8_linux.py`, which drives `u8_host.py` on the host, session scratchpad.

* **Binaries.** It built two linux/amd64 mcrelay binaries:
  * U8's, from the owner's commit of U8, `2d6efd62`;
  * a control from `dc97fb01`, the commit before U8, which sends no `READY=1`.
* **The unit.** U8's own `mcrelay setup-service --print-only` rendered a user unit named
  `mcrelay-u8probe`, a name nothing else uses. It runs on loopback with TLS off, at `--log-level
  debug` so each send is logged, and with its config and data in a scratch kit.
* **Cleanup.** It ran whatever happened, and the kit was deleted after the unit was confirmed gone.

| Step | Result |
| :--- | :--- |
| the rendered unit | has `Type=notify`, `TimeoutStartSec=240`, `--log-level debug` and its port |
| `systemctl --user show`, once it started | `Type=notify`, `NotifyAccess=main`, `TimeoutStartUSec=4min`, `active`/`running` |
| `systemctl --user start` | exit 0 after 0.106 s. The journal logged "sd_notify sent … state="READY=1"" 0.102 s after the start began, and the start returned 0.004 s after that |
| the listener, as the start returned | accepted a connection |
| `systemctl --user stop` | exit 0 after 0.027 s, `inactive`, `Result=success`. "sd_notify sent … state="STOPPING=1"" was logged 0.011 s after the stop began |
| control: the `dc97fb01` binary in the same unit, with a 5 s `TimeoutStartSec` drop-in | exit 1 after 5.145 s, `Result=timeout`, still `activating`: "Job for mcrelay-u8probe.service failed because a timeout was exceeded." |
| clean | the unit and the drop-in removed, the manager reloaded and reset; `systemctl --user cat` no longer knows the unit |

* **The control is what makes the first rows mean something.** A unit that ignored `READY=1` would
  report `active` as early. Without `READY=1`, this unit's start waited out its timeout and failed.
* **A mistake in the first run, fixed.** The first run built the control from `HEAD`, which was
  already the owner's commit of U8. That control sent `READY=1`, went `active`, and the control step
  failed. The driver now names `dc97fb01`, and the run above is the second, complete one. The first
  run also cleaned up, and its notify rows matched the second's.

With this, U8's five steps are done. U8 is complete once the next CI run on `2d6efd62` passes. Of
this plan's phases, only U6 remains, which waits for 0212's P3. The plan stays `in-progress`.

### U6 start, 2026-10-27

0212 is complete (`12be09c2`, `f5d9af5a`), so U6's wait (amendment D, D22) is over. The owner
answered "committed proceed" when U6 was named next. That approves U6, and is taken as the word for
its step 4 host runs, as for earlier phases.

### U6 step 1, 2026-10-27: each call site, measured [read at `f5d9af5a`; go-selfupdate-lib at `v1.22.1`]

| Call site | What setup does today | What the backend does | Where they differ |
| :--- | :--- | :--- | :--- |
| systemd start, `setup.go:496-501` | `systemctl --user restart`, on a stopped or a running unit | `Unit.Stop`: refuses inside, then stops and waits for inactive (`systemd/lifecycle.go:72-105`). `Unit.Start`: records `InvocationID`, then `systemctl start` (`:138-166`). `WaitHealthy`: a new invocation, the settle window, then the product's probe (`:176-214`) | A running unit needs `Stop` before `Start`, or `WaitHealthy` waits for an invocation that never comes (amendment D's F4). The library has no enable for systemd, so `setup.go:489-494` keeps `systemctl enable` |
| systemd remove, `setup.go:780-781` | `stop` and `disable`, errors discarded | `Unit.Stop`, bounded, with the inside refusal | The stop is bounded and reported; `disable` stays setup's |
| launchd, unchanged plist and running job, `setup.go:586-596` | `kickstart -k`, restarting in place (0125-MADR D4) | `Job.Start` runs `enable`, `bootstrap` only when not loaded, then `kickstart` without `-k` (`launchd/lifecycle.go:248-279`). A loaded running job is not restarted | No library call restarts in place (Deviation 1d) |
| launchd, other starts, `setup.go:606-636` | `stopAndWaitDarwin`, `enable` unless `--no-enable`, `bootstrap`, `kickstart -k` | `Job.Stop`: refuses inside; returns nil when not loaded; else `bootout`, then waits for the job and its PID to go (`:150-235`). `Job.Start` as above, retrying `bootstrap`'s 5, 37 and in-progress within its bound (`:288-320`). `WaitHealthy`: a new PID for at least `ThrottleInterval`, then the probe (`:329-363`) | `Job.Start` always runs `enable`, so `--no-enable` no longer skips it there. The plist's `RunAtLoad` does not read `NoEnable` (`git grep NoEnable` finds no plist use) |
| launchd remove, `setup.go:847-848` | `stopAndWaitDarwin`, then `disable` | `Job.Stop` | Same wait; `disable` stays setup's |
| Windows start, `setup_schtasks.go:81`, `control_schtasks.go:29-38` | `/change /enable`, `/run` | `Task.Enable` (`taskscheduler/lifecycle.go:333`), then `Task.Start`, which leaves a running instance alone and returns `ErrTaskDisabled` for a task another process disabled (`:284-323`) | Today `--force` re-registers and runs `/run`, which leaves a running instance on the old definition. Step 2 has `--force` call `Stop` first |
| Windows remove, `setup_schtasks.go:107`, `control_schtasks.go:57-73` | `/change /disable`, then `/end`, a hard kill (0116-MADR D9) | `Task.Stop`: refuses inside, disables, sends `RequestStop` and waits `StopGrace`, then ends and waits (`:138-211`) | A drain, when the product's `RequestStop` is passed (Deviation 1a) |
| script kinds' start, `supervisor.go:850-931` (`startScript`) | the down file or `rc-update add`; a session supervisor when none watches; an s6 rescan; log adoption; `sv restart`, `s6-svc -r` or `rc-service restart` | `Supervised.Start` runs `Up`, which starts a session supervisor, adopts a log service and runs the up command (`supervisedbackend.go`, 0212 D21-A). It leaves alone a running instance other than the one `Stop` recorded (`supervised_lifecycle.go:163-178`). `WaitHealthy` counts only an instance other than the one `Stop` recorded (`:199-206`) | Only `Stop` records the old instance, so a restart confirmed by the adapter must be stopped and started through it (Deviation 1b) |
| script kinds' remove, `supervisor.go:962-1018` | the down file, `sv down` then `sv exit` (`s6-svc -d`, `-x`), and `rc-service stop`, errors discarded | `Supervised.Stop`: `Down`, the wait, and on runit `sv kill` at 45 s | The adapter's stop is bounded; the supervisor's exit stays setup's |
| the installer hook, `installerhook.go:154`, `:184` | `NewBackend`, with no health wiring; `Stop` and `Start` on it; then `verifyStart` | `WaitHealthy` on the same backend | The hook's backend has no product probe and no drain (Deviation 1a) |
| the start check, `setup.go:439-441`, `settle.go:303-319` | `verifyStart`: `settle`, up to `MCREMOTE_SVC_WAIT_SECS`, default 15 (`settle.go:22-40`); any positive value | `WaitHealthy`, by `Poll`: a 10 s settle window and a 60 s timeout by default (`service/poll.go:46-48`). The adapter refuses a timeout not above the settle window (0212 D17) | A healthy start takes at least 10 s. A value of 10 or less cannot work (Deviation 1c) |
| the backend's kind, `backend.go:54` | `Setup` detects the kind itself (`setup.go:336`), and records it only after the start (`:433-437`) | `NewBackendWith` on Linux takes the recorded kind, and detects one only when there is no record (`backendrecord.go:51-57`). The probe finds the definition the same way (`health.go:91`) | A record that an earlier setup left on another supervisor names the wrong kind for both (Deviation 1a) |

### U6 Deviation 1, 2026-10-27 — four gaps step 1 found. The owner decided all four as recommended

U6 named six files. Step 1 shows that D4 as amended cannot be met inside them. The owner was asked
four questions, each with three options, and chose the recommended one each time:

* **(a) "Pass health in, build by kind".**
  * `Options` gains `Health HealthOptions`. `internal/cli/setup_service.go` and
    `internal/relay/cli.go` set it from each product's health wiring, as the update does
    (`internal/cli/health.go:24-30`, `internal/relay/health.go:29`). The installer hook builds its
    backend with it.
  * `backend.go` gains a constructor that takes the kind. `NewBackendWith` calls it after
    `ResolveBackend`, and `Setup` calls it with the kind it detected. The probe takes that kind
    too, so it reads the definition setup just wrote.
  * So every start check runs the product's probe, and Windows `--remove` drains.
  * *Corrected the same day, before any code:* the question put to the owner said a first install
    has no recorded backend, so `NewBackendWith` could not build one. With no record it detects the
    kind, as `Setup` does, so a first install gets the right backend. The gap is a record left by an
    earlier setup on another supervisor. The answer stands, since building by kind closes that gap
    as well.
* **(b) "Through the adapter".** `startScript` keeps its enable steps: the down file, `rc-update
  add`, OpenRC's runlevels and s6's rescan of a new directory. A start then goes through the
  adapter: `Stop`, then `Start`, then its `WaitHealthy`. `removeScript` stops through the adapter
  first, then makes the supervisor exit as before. `settle` goes.
* **(c) "The backends' limits".** A 10 s settle window and a 60 s timeout. `MCREMOTE_SVC_WAIT_SECS`,
  when set, is the timeout, and must be above 10 (above 45 on runit), as the update already
  requires. `ErrTimeout` reads `starting`; `ErrUnhealthy` reads `failed`. The guides' default of 15
  becomes 60.
* **(d) "Job.Stop then Job.Start".** One path on macOS. The in-place `kickstart -k` goes, with a
  dated note on 0125-MADR D4. `--no-enable` no longer skips `launchctl enable` when setup starts
  the job.

**The rule each kind keeps.** A start restarts a running service, as setup does today on systemd,
launchd and the script kinds. On Windows, only `--force` stops a running task first. Without it,
an unchanged task that runs is left running, as `/run` leaves it today.

**Files added to U6:** `internal/cli/service/backend.go`, `supervisor.go`, `health.go` if the
constructor needs it, `internal/cli/setup_service.go`, `internal/relay/cli.go`, and their tests;
`docs/guides/config.md`, `config-mcrelay.md` and `ops-linux-install.md`; and the records:
[0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)
(amendment F) and
[0125-MADR-launchd-bootout-is-asynchronous.md](0125-MADR-launchd-bootout-is-asynchronous.md) (a
note on D4). U6's note above said "Files: unchanged". That was before step 1.

### U6 paused, 2026-10-27

The owner said "stop" while U6's tests were being written. U6's source edits and the records above
are in the working tree, unstaged. The service package's tests do not compile against them yet
(`go vet ./internal/...`: `settle_test.go`, `health_test.go`). Nothing of U6 is staged.

### U1c, 2026-10-27: the `v1.22.2` pin, folded into U6

**Approval.** The owner asked "update the pin for go-selfupdate-lib to current", and was asked two
questions:

* "Approve U1c?": "Approve, proceed (Recommended)". That includes `make ci-windows` on the Windows
  host.
* "How should the pin land?": "Fold into U6". The pin moves in the working tree now, and lands with
  U6 once U6 is finished and gated. Nothing is staged until then.

**Steps 1 to 4:**

* `git ls-remote … 'refs/tags/v1.22.2^{}'` gave `50deb3f1317bcd9142f2c91be2b5609957c677c9`, and
  `git ls-remote --tags --refs` listed no tag above `v1.22.2`.
* `go get github.com/maccavelli/go-selfupdate-lib@v1.22.2`: "upgraded … v1.22.1 => v1.22.2". Then
  `go mod tidy`.
* `go.mod`'s one line. `go.sum`'s two: the module's `h1:` changes to
  `h1:05J2BTJUQm4gdCqBor+eA/LXwDVbi+RG1Z5Y7q9zm/w=`, and its `/go.mod` hash does not.
* Both `ci.yml` pins name `@50deb3f1317bcd9142f2c91be2b5609957c677c9 # go-selfupdate-lib v1.22.2`.
  `git grep 4d9ae417 -- .github go.mod go.sum` finds nothing. No Go source changed for the pin.

**Step 5, what could run with U6 in the tree:**

| Gate | Result |
| :--- | :--- |
| `go build ./...` | exit 0 |
| `go mod tidy -diff` | exit 0, no output |
| `TestWorkflowPinsMatchGoMod`, `TestReleaseJobsReadTheSpec` | both pass |
| `make vulncheck` | "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `actionlint .github/workflows/ci.yml` | exit 0 |

**Waiting for U6:** `make test`, `make race`, `make staticcheck`, `go vet` for the three targets,
`go test -race ./...` on the Linux test host, and `make ci-windows` on the Windows host. They run
on the tree that holds both, at U6's end. *Ran 2026-10-27, all passing: "U6, 2026-10-27: the
gates" below.*

### U6 resumed, 2026-10-27, and Deviation 2 — the files beyond Deviation 1, and the relay's probe. The owner decided: "Widen to these (Recommended)", "Pass a relay probe in"

The owner answered "resume U6". Two questions were put before any further edit, each with three
options:

* **The file scope.** Deviation 1's list did not hold what the change reaches:
  * `supervisedbackend.go`, one line, already made: the probe takes the kind (D40);
  * `supervisorstate.go`: `scriptDefOf`, `observeBounded` and `startToken`, which only `settle`
    used, go with it;
  * test files of sources outside the list: `TestMain` in `unitrecord_test.go`,
    `supervisorstate_test.go`, `uninstall_unix_test.go`, `installerenv_unix_test.go`, and any other
    test in `internal/cli/service` that the build or a run shows U6 breaks.

  Answer: "Widen to these (Recommended)". Each file is named below with why.
* **The relay set up beside mcremote** (`MCREMOTE_WITH_RELAY_SERVICE=1`) got the service manager's
  check but no `/healthz`, since `SetupRelayBeside` builds its options in the service package.
  Answer: "Pass a relay probe in".
  * The option said the probe would move somewhere both commands could import. That proved
    unnecessary: `internal/cli` already links `internal/relay`, through `internal/relayhost`, and
    the relay imports nothing of `internal/cli`.
  * So `internal/relay/health.go` exports `SetupHealth`, `installerenv.go`'s `SetupRelayBeside`
    takes the relay's `HealthOptions`, and `internal/cli/setup_service.go` passes it. No new
    dependency enters mcremote's binary.

**One more check, made within D42 without a question.** On Windows, mcremote's 30 s drain must fit
inside the timeout, or the library refuses to build the task backend. Without a check, `setup`
would fail late, and `--remove` would skip its stop and delete a running task. So:

* `Setup` refuses a `MCREMOTE_SVC_WAIT_SECS` that is not above `Health.StopGrace` on Windows,
  before anything is written;
* `stopForRemoval` refuses the removal when the backend cannot be built, instead of tolerating it.

### U6 Deviation 3, 2026-10-27 — `schtasks.go`. The owner decided: "Delete both (Recommended)"

The first gate run's `make staticcheck` failed: "internal/cli/service/schtasks.go:121:6: func
taskName is unused (U1000)". `startWindows` and `stopWindows` were its only callers, and
`runSchtasks` had no production caller left either, only the tests' recorder. `schtasks.go` was
outside the widened scope. The owner chose to delete both, so that no path can run `schtasks.exe`
(D30). The tests' `withSchtasks` recorder and their "no schtasks ran" checks went with it, in
`schtasks_test.go`, `health_test.go`, `migrate_test.go`, `uninstall_test.go`,
`nostart_schtasks_test.go` and `task_compare_test.go`. The two `--no-start` tests now check that
setup asks the task backend for nothing instead.

### U6, 2026-10-27: what was built

**Source** (D40 to D43, and Deviations 2 and 3):

| File | Change |
| :--- | :--- |
| `backend.go` | `newBackendFor(kind, …, poll)`, which `NewBackendWith` calls after `ResolveBackend`; `setupBackend`, setup's constructor with its bounds, and `OverrideSetupBackend` for external tests |
| `health.go` | `readServeArgs` and `backendProbe` take the kind; empty means the record's |
| `settle.go` | `settle` and its samplers are gone. `verifyStart` waits on the backend that started the service: `nil` is running, `ErrTimeout` is starting, anything else failed. `setupPoll` is D42 |
| `setup.go` | `Options.Health`; `startThrough`, `watchThrough` and `stopForRemoval`; systemd starts with `Stop` and `Start`; launchd's in-place `kickstart -k`, teardown and bootstrap give way to `Job.Stop` and `Job.Start`; the D42 checks, before anything is written |
| `setup_schtasks.go`, `control_schtasks.go` | Windows starts with `Task.Enable` and `Task.Start`, `--force` stops first, and `--remove` stops through `Task.Stop`; `startWindows` and `stopWindows` are deleted |
| `launchd_wait.go` | `stopAndWaitDarwin`, `launchdJobRunning` and their timings are deleted; `launchdLoaded` stays, for `--no-start`'s note |
| `supervisor.go` | `startScript` and `startOpenRC` keep the enable steps, restart a supervised service through the adapter, and only watch one a new scanner or session brings up; `removeScript` stops through the adapter first |
| `supervisedbackend.go` | the probe takes the kind |
| `supervisorstate.go` | `scriptDefOf`, `observeBounded` and `startToken` are deleted |
| `installerhook.go` | the hook builds through `setupBackend` with the plan's health, and the start check waits on that backend |
| `installerenv.go`, `internal/relay/health.go`, `internal/cli/setup_service.go`, `internal/relay/cli.go` | each product's health reaches setup, the relay set up beside mcremote included |
| `schtasks.go` | `runSchtasks` and `taskName` are deleted |

**Guides:** `config.md`, `config-mcrelay.md` and `ops-linux-install.md` describe the start check
as the update's, with its new limits.

**Tests.**

* **`TestMain`** makes every setup build a fake backend that does nothing. Without that, setup's
  tests would run the real `systemctl`, `launchctl` or supervisor on the host.
* **`setupFake`**, in `settle_test.go`, models the two things setup must get right: a running
  service that `Start` leaves alone, whose `WaitHealthy` then times out; and a task another process
  disabled, which `Start` refuses until `Enable`.
* **New:** `setup_backend_unix_test.go`, and new tests in `settle_test.go`, `schtasks_test.go`,
  `installerenv_unix_test.go`, `installerhook_unix_test.go`, `supervisor_unix_test.go`,
  `supervisorstate_test.go` and `launchd_sequence_test.go`.
* **Rewritten** over the fake: setup's restart tests in `supervisor_unix_test.go`,
  `settle_unix_test.go`, `launchd_sequence_test.go` and `setup_launchd_test.go`, and the two
  log-service tests in `scriptdefs_unix_test.go`, folded into
  `TestSetupLeavesTheLogServiceToTheAdaptersUp`.
* **Removed, with the code they tested:**
  * `settle`'s tests: `TestSettleSystemd`, `TestSettleOtherKinds`, `TestSamplersReadTheirManagers`,
    `TestSettleIgnoresTheReplacedProcess`;
  * the two start-token tests in `supervisorstate_test.go`; the adapter keys an instance by PID and
    start time itself;
  * `stopAndWaitDarwin`'s seven tests in `launchd_wait_test.go`; that wait is `Job.Stop`'s now;
  * `TestStopDisablesThenEnds` and `TestStartEnablesThenRuns`.
* **Fixed in other files:** `health_test.go` and `nostart_launchd_test.go`.

**Seen failing first** (`m207u6/u6_plants.py`). Each plant was a single replacement, on a scratch
copy of the tree, made once and restored after its run. The unplanted copy passed every test named:

| Plant | Its tests | The failure |
| :--- | :--- | :--- |
| systemd starts without stopping | `TestSetupVerifiesTheStart` | `calls ["start" "wait"], want ["stop" "start" "wait"]` |
| Windows starts without enabling | `TestWindowsStartEnablesATaskAnotherProcessDisabled` | `… the service could not be started: selfupdate: taskscheduler: the task is disabled` |
| `--force` does not stop the task | `TestWindowsStartsThroughTheTask` | `calls ["enable" "start"], want ["stop" "enable" "start"]` |
| remove ignores the inside refusal | `TestRemoveStopsThroughTheBackend`, `TestRemoveSchtasksStopsThroughTheTask`, `TestDarwinRemoveFromInsideTheJobRemovesNothing` | all three; first `err = <nil>, want the inside refusal` |
| the probe reads the recorded kind | `TestReadServeArgsReadsScriptDefinitions` | `readServeArgs(runit) under a systemd record = {…}, "", read the mcrelay unit: open …/mcrelay.service` |
| setup drops the product's health | `TestSetupBuildsItsBackendByKindWithTheProductsHealth` | `built {Kind:systemd-user … Health:{Probe:<nil> RequestStop:<nil> StopGrace:0s}}` |
| a script restart does not stop first | `TestSetupConfirmsTheRestartOnTheAdapterThatMadeIt`, `TestSetupRunitRestartsThroughTheUsersSupervisor` | `adapter calls ["start"] …; want a stop and a start on one runit adapter` |
| macOS restarts an unchanged plist in place | `TestDarwinSetupRestartsAnUnchangedPlistThroughTheJob` | `calls ["launchctl kickstart -k gui/…"], want ["job stop" "job start"]` |
| the wait is not checked against the settle window | `TestSetupPollIsD42`, `TestSetupVerifiesTheStart` | `10, systemd-user: <nil>; want a refusal naming the settle window` |
| the wait is not checked against the drain | `TestSetupRefusesAWaitShorterThanTheDrain` | `a 30 s wait with a 30 s drain: <nil>, want a refusal naming the drain` |
| a timed-out check reads failed | `TestVerifyStart` | `health "failed", err … timed out` |
| the relay beside gets mcremote's health | `TestSetupRelayBesideConfirmsWithTheRelaysProbe` | `mcremote's probe was given to the relay` |
| the hook drops the plan's health | `TestFromInstallerConfirmsOnTheBackendItCycled` | `builds [{Kind:runit … StopGrace:0s}], want one runit backend with the plan's health` |
| setup restarts the log service itself | `TestSetupLeavesTheLogServiceToTheAdaptersUp` | `calls [sv status …/log sv restart …]` |
| Windows `--no-start` starts the task | `TestNoStartDoesNotStartTheWindowsTask`, `TestNoStartKeepsAStoppedWindowsTaskStopped` | both |
| macOS `--no-start` stops and starts the job | `TestNoStartLeavesARunningLaunchdJobAlone` | `--no-start built a backend to stop or start the job` |
| a failed stop gets the bootstrap hint | `TestDarwinSetupReportsAFailedStop` | `a failed stop got the bootstrap hint` |

* **Two plants corrected, before the final run.**
  * The first "timed-out" plant left `errors` unused, and the copy did not build. It proved
    nothing, and was re-planted as `errors.Is(err, nil)`.
  * The first "drain" plant failed on a missing task principal further on, not on the missing
    refusal. The test now sets a principal first, so the plant reaches registration.

**Two test faults the Linux test host found, both mine:**

* `TestRemoveStopsThroughTheBackend/systemd-user` put only the fake tools on `PATH`, so setup's
  preflight found no `systemctl` there. macOS skips that check. The systemd case now keeps the
  host's `PATH`.
* `TestNoStartLeavesARunningLaunchdJobAlone` did not stub `launchdLoaded`, which `--no-start`'s
  note now reads. On this Mac it read the real launchd, where a job by that label is loaded, and
  passed by accident. It now stubs it.

### U6, 2026-10-27: the gates

`m207u6/u6_gates.py`, second run, over the 36 changed and added Go files:

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows. The first run failed on `taskName` (Deviation 3), and on `fastSettle` and `withRealVerifyStart`, which were in an untagged test file but used only by unix ones; they moved to `settle_unix_test.go` |
| `make pre-add-check` | "36 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok |
| `make check-records`, `make markdownlint-docs`, `actionlint` | exit 0 |
| `make vulncheck` | "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `TestWorkflowPinsMatchGoMod`, `TestReleaseJobsReadTheSpec` | pass |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 44 packages ok (the first run failed on the two test faults above) |
| `make ci-windows` on the Windows test host, on a scratch copy | "ci-windows-local: ALL SELECTED CHECKS PASSED"; the Windows-path tests: 119 passed, none skipped |

* `git grep -n -e stopAndWaitDarwin -e launchdJobRunning -e startWindows -e stopWindows -e
  scriptBackend -e runSchtasks -- internal/cli/service`, outside tests: nothing (amendment D's
  confirmation).
* The same search for `powershell`, `"/create"`, `"/query"`, `"/delete"`, `"/change"`, `"/run"`
  and `"/end"`: nothing (this plan's Verification, with D30).

### U6 step 4, 2026-10-27: the host runs

**macOS and the Linux test host** (`m207u6/u6_hosts.py`, which runs `u6_run.py`). They used the
scratch service `mcremote-d3probe`, port 17631, under `~/mcr-u6probe`, deleted afterwards:

| Host | Install | `--force`, definition unchanged | `--remove` |
| :--- | :--- | :--- | :--- |
| macOS (launchd) | exit 0, 11.1 s, "confirmed after the start", `/healthz` ok | 10.7 s, confirmed, pid 42384 → 42637, plist unchanged | 0.4 s, pid gone, `/healthz` gone |
| Linux (systemd) | exit 0, 10.9 s, confirmed, `/healthz` ok | 11.0 s, confirmed, pid 4001610 → 4001691, unit unchanged | 0.9 s, pid gone, unit and its enable link removed |

* The macOS `--force` is amendment F's case, an unchanged plist over a running job. The job was
  stopped and started, on a new PID, as D43 decided.
* **A mistake of mine, first run.** `u6_run.py` read `/health`; the daemon serves `/healthz`. Every
  setup step behaved the same as in the second run, but the script's own health check reported
  "NOT ok". It was corrected, and the second run is the one above.

**runit, s6 and OpenRC** (`make live-supervisors`, local Podman, on the owner's word). The machine
was started for the run and stopped again:

| Kind | Result | Install | The handoff |
| :--- | :--- | :--- | :--- |
| runit | PASS, 60.2 s | 11.3 s | "update handed off: 7f1019fe…" |
| s6 | PASS, 57.9 s | 10.3 s | "update handed off: fa6cf791…" |
| OpenRC | PASS, 60.5 s | 10.4 s | "update handed off: 2f08d1af…" |

The install step took 1.1 to 3.1 s in 0212's P6, and now takes about 10. That is setup's start
check, waiting out the adapter's settle window (D41, D42).

**Windows, the real task** (`m207u6/u6_win.py`, which runs `u6_win_host.py`, on the owner's word).
The run follows W1 to W4, as U4 did:

* W1 recorded the installed `v0.23.0 (release)`, the binary, config and task export, and one
  refresh backup. The tree's build only ran `setup-service`, with the flags recovered from the task.
* The daemon's exit code was read from a handle opened before each stop.

| Phase | Result |
| :--- | :--- |
| plain setup over the running task | exit 0, 10.3 s, "confirmed after the start"; pid 43944 unchanged; export identical |
| `--force` | exit 0, 10.7 s, confirmed; the old daemon 43944 exited with code 0, the drain; pid → 9680; `/healthz` answers; export identical |
| `--remove` | exit 0, 0.3 s; daemon 9680 exited with code 0; the task absent, and its refresh backup removed |
| fresh install | exit 0, 10.2 s, confirmed; pid 56236; export identical to W1's |
| final | the refresh backup put back; binary, config, export and backup as W1; the task Running; `/healthz` answers; the kit deleted |

`--remove` on Windows now ends the daemon with exit code 0, a drain, as 0054 S9d did by hand.
Before U6 it was `schtasks /end`, a kill.

**Not changed, and noted for its own record.** `result_print.go` still prints "Started: yes
(schtasks /run)" for a Windows setup, and "The daemon was stopped …" after any `--remove`, even
when a tolerated stop failed. The first is no longer how the task starts. `result_print.go` is
outside U6's files.

**U6 and U1c are complete once the owner commits them.** The plan becomes `complete` when CI on
that push passes.

### U6 and U1c, 2026-10-27: CI on the push

The owner committed U6 and U1c as `59bcadad` and pushed them. CI on that commit,
[run 40570546247](https://github.com/maccavelli/magic-cli-remote/actions/runs/40570546247),
concluded `success`:

* `Go (test)`, `Go (darwin/arm64)`, `Go (linux/arm64)` and `Go (windows/amd64)`;
* `Live supervisors (runit, s6)`, its second run on GitHub, now on `v1.22.2` and U6's setup;
* both Flutter jobs.

The release and APK jobs were skipped, as on any push without a tag.

### U6b, 2026-10-27: approval

The owner asked "scope in fixing result print output, mark 0207 complete", and chose "Do U6b, then
complete (Recommended)": U6b as written above, with the plan marked complete in its record.

### U6b, 2026-10-27: what was built

| File | Change |
| :--- | :--- |
| `result_print.go` | "Started:" names what setup did: "(stopped, then started)", "(booted out, then bootstrapped and kickstarted)", "(enabled, then started; --force stops it first)". `PrintRemoveResult` says "The daemon could not be stopped: <why>. Its definition is removed; end the process yourself if it still runs." when the stop failed |
| `setup.go` | `Result.StopFailure`; `stopForRemoval` returns the failure it tolerates. `ErrNotInstalled` is no failure: there was nothing to stop |
| `setup.go`, `setup_schtasks.go`, `supervisor.go` | each removal records it |
| `testdata/result_print_systemd.golden`, `result_print_launchd.golden` | the one "Started:" line in each, recaptured with `-update`; `git diff` shows that line and nothing else |

**Tests:**

* `TestPrintSetupResultNamesWhatTheStartDid`, new;
* `TestPrintRemoveResult`, with a failed stop;
* `TestRemoveRecordsAFailedStop`, new, for systemd, runit and a service not installed;
* a "failed stop, recorded" case in `TestRemoveSchtasksStopsThroughTheTask`;
* `TestDarwinRemoveRecordsAFailedStop`, new.

**Seen failing first** (`m207u6/u6b_plants.py`, the same driver as U6's). The unplanted copy
passed every test named:

| Plant | Its tests | The failure |
| :--- | :--- | :--- |
| the old Windows "Started:" line | `TestPrintSetupResultNamesWhatTheStartDid` | `windows-task: no "Started:           yes (enabled, then started; --force stops it first)"` |
| the old systemd "Started:" line | it, and `TestPrintSetupResultUnixUnchanged` | both; `systemd summary changed` |
| `--remove` says stopped after a failed stop | `TestPrintRemoveResult` | `with a failed stop:` and the old line |
| a tolerated failure dropped | `TestRemoveRecordsAFailedStop`, `TestRemoveSchtasksStopsThroughTheTask`, `TestDarwinRemoveRecordsAFailedStop` | all three; `StopFailure:` empty |
| a service not installed reported as a failure | `TestRemoveRecordsAFailedStop` | `Remove of a service not installed: … want no failure` |

**Gates** (`m207u6/u6_gates.py`, over U6b's eight Go files, with D27's records in the tree):

| Gate | Result |
| :--- | :--- |
| `make staticcheck` | exit 0, linux, darwin and windows |
| `make pre-add-check` | "8 file(s) clean (gofmt, golint, govulncheck)" |
| `go mod tidy -diff`; `go build ./...`; `go vet ./...` darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok |
| `make check-records`, `actionlint`, `make vulncheck`, the pin tests | exit 0; vulncheck "clear" |
| `make markdownlint-docs` | the first run failed: "0167-REPORT…:640:3 MD004/ul-style Unordered list style [Expected: dash; Actual: asterisk]", in D27's note, mine. Fixed; exit 0, "0 issues" |
| `go test -race -count=1 ./...` on the Linux test host | exit 0; 44 packages ok |

No host run: the wording is unit-tested, and U6's host runs exercised the paths, as U6b's step 5
said.

**The plan is complete** once the owner commits U6b. Every phase has run (U7 was retired into 0212,
and U1c folded into U6), and this plan's Verification holds. CI ran on `59bcadad` for U6 and U1c;
U6b's own CI follows its push.

### U6b, 2026-10-27: CI on the push

The owner committed U6b as `5d1e7e2c` and pushed it. CI on that commit,
[run 40585331665](https://github.com/maccavelli/magic-cli-remote/actions/runs/40585331665),
concluded `success`:

* `Go (test)`, `Go (darwin/arm64)`, `Go (linux/arm64)` and `Go (windows/amd64)`;
* `Live supervisors (runit, s6)`;
* both Flutter jobs.

The release and APK jobs were skipped, as on any push without a tag. The Docs workflow on the same
commit, [run 40585330893](https://github.com/maccavelli/magic-cli-remote/actions/runs/40585330893),
also concluded `success`. Every phase of this plan has now passed CI.
