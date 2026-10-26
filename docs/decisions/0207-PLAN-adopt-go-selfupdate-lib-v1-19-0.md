---
status: in-progress
date: 2026-10-25
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
   W1 to W4: `setup-service` (unchanged), `--force` (re-registers), `--remove` and a fresh install.
   The task's definition after W4 compares the same as W1's.
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

*Amendment C, proposed (D30):*

* The Windows start is `Task.Enable`, then `Task.Start`. The stop is `Task.Stop`, which leaves
  the task disabled.
* Step 2 adds a test: after a stop in another process, a start enables first and then starts. It
  is seen failing against a start that only calls `Task.Start`, which returns `ErrTaskDisabled`.
* After U6, `git grep -n '"/change"' -- internal/cli/service` finds nothing outside tests.
  `result_print.go:174`'s printed hint stays.

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
    classifies by the stable HRESULTs `0x80041318` and `0x8004131A`, with a truncated definition as
    its negative control.
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

**Not done:** the CI run on the owner's push, which confirms `Go (test)` is green again.
