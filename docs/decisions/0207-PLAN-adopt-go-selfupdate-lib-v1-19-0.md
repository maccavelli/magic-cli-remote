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

### U3b — Replace before the stop, with a post-install probe (amendment D, D33 to D36) — proposed

**Files:**

* `internal/updateclient/managed.go`, `client.go`, `managed_test.go`, and the test file that holds
  `New`'s tests (measured in U3);
* new: `internal/updateclient/replacefirst_guard_test.go` (G1);
* new: `internal/cli/service/definitions_once_test.go` (G2);
* new: `internal/updateclient/live_replacefirst_test.go` (live-tagged);
* `Makefile`, for its `live-replacefirst` target;
* `docs/guides/ops-linux-install.md` and `ops-windows-install.md`, one sentence each on the order;
* this plan.

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
