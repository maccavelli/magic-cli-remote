# `go-selfupdate-lib` deeper-adoption assessment for `magic-cli-remote`

Observed 2026-10-21, at `master` `3e6d15b5` of this repository and at `go-selfupdate-lib` `v1.17.2`
(`036289ecc701d84c7749f4950a57a154c1af2cf4`). The owner asked how far more of this repository's
update and service code can move onto the library, so that Magic keeps only what the library
deliberately leaves to a product. This report also assesses the library's current release. It
decides nothing and changes no source.

Associated records:

- [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](../decisions/0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)
  (proposed). This report feeds its review, and corrects its F1 (see [A1](#a1--an-agent-run-update-fails-safe-today-0207-f1-overstates-the-harm)).
- [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
  (accepted; PLAN complete), and its owner decision O1, "every concern the library owns moves to
  it".
- [0054-REPORT-go-selfupdate-lib-full-adoption-assessment.md](0054-REPORT-go-selfupdate-lib-full-adoption-assessment.md),
  the assessment this one follows, at `v1.14.0`.
- [0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md](../decisions/0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md)
  (no release signing).

## Executive assessment

**The current release is `v1.17.2`, and there is no newer one.** It was tagged 2026-10-21 and
changes only the toolchain (Go 1.27.3, with a 1.26.10 floor, for GO-2026-6809). Its API matches
`v1.17.1`'s exactly. The library publishes tags only, no GitHub releases. Its default branch is three
`docs(…)` commits ahead of the tag. Magic pins `v1.17.1` in `go.mod` and in both reusable workflow
pins in `ci.yml`. The move is 0207's D1. A scratch copy with only `go.mod` moved builds, vets, and
passes every test except the guard that requires the workflow pins to move with it.

**Magic's update path is already the library's.** The `update` command, the release pipeline, the
managed install, the reconcile receipt, the codesign transform and the build stamps are canonical.
Nothing in `internal/updateclient` duplicates the library. What remains is in three places:

1. **The handoff is unadopted,** and 0207 F1 describes its absence wrongly. An update an agent
   starts inside the daemon does not stop the daemon under itself. Every library backend refuses
   that stop with `service.ErrInsideService`, and the managed installer then leaves the service
   running and the binary unreplaced. The update fails safe, with exit 1, on all three operating
   systems (A1). On Windows, adopting the handoff has a hard prerequisite in Magic's own process
   containment (A1).
2. **`setup-service` still controls services with its own code** where the backends would do it,
   and its copies are weaker than the library's in measurable ways (A2, A3):
   - a launchd teardown bound of 15 s against the plist's own 45 s exit timeout;
   - no transient-error retry on bootstrap;
   - a hard kill on Windows where the backend drains;
   - two contradictory readings of launchd's `state = waiting`;
   - no inside-the-service guard on any stop;
   - three different ways to confirm a start: the update's, the installer hook's and `setup`'s.
3. **The update's composition** skips library parts that cost little and guard real failures:
   - a staged-binary probe;
   - an image check;
   - the library's recommended HTTP client shape (A5).

There is also one stale build-stamp site, proven by experiment (A7).

**Recommended direction:** move the pin first (0207 D1). Then:

1. take the low-risk composition items (A5, A6, A7);
2. move `setup-service`'s stop, start and start verification onto the backends and
   `service.PollHealthy` in one release, with the live checks run once (A2, A3, A4);
3. adopt the handoff last, after the owner decides the Windows breakaway trade-off (A1).

Four asks for the library would remove the rest of the duplication, which Magic cannot remove
alone (A10). They are filed as `go-selfupdate-lib` issues #2 to #5.

## Scope and evidence

### Baselines

| Repository | Baseline | Self-update dependency |
| :--- | :--- | :--- |
| `magic-cli-remote` | `master` `3e6d15b5`, clean tree | `go-selfupdate-lib v1.17.1` in `go.mod`; `build-selfupdate-release.yml` and `publish-selfupdate-release.yml` pinned to `e8d4904a09194d99761042a8996983e8d5b957eb` (`v1.17.1`), `ci.yml:712` and `:733` |
| `go-selfupdate-lib` | tag `v1.17.2`, peeled `036289ecc701d84c7749f4950a57a154c1af2cf4`; a shallow clone at the tag | — |

### Method

Everything was read-only for this tree. The scripts and outputs sat in a session scratch directory
and are not committed.

- **Tags and releases:** `git ls-remote --tags` on the library, and
  `gh api repos/maccavelli/go-selfupdate-lib/releases`, which returned none.
- **Commits after the tag:** `gh api …/compare/v1.17.2...HEAD`.
- **The API:** `go doc -all` on each library package, run in a clone at the tag.
- **Magic's usage:** a script over every tracked `.go` file. It reads each import of the library,
  takes the import's local name, and records every `name.Symbol` reference.
- **Magic's code:** read in full where a finding rests on it. Citations are `file:line` at
  `3e6d15b5`; library citations are paths inside the module at `v1.17.2`.
- **One experiment, on a scratch output path (A7):** a `go build` of `./cmd/mcremote` with the
  Windows local-CI script's `-ldflags`, then the binary's `version` command.
- **Not run:** nothing touched a service manager, and no live host was used. Claims about runtime
  behaviour are marked "from code" where they rest on reading alone.

## The current release

| Tag | Peeled commit | What it changed |
| :--- | :--- | :--- |
| **`v1.17.2`** | `036289ecc701d84c7749f4950a57a154c1af2cf4` | Toolchain only: Go 1.27.3, floor 1.26.10, for GO-2026-6809. API identical to `v1.17.1` (0207, "go-selfupdate-lib, `v1.17.1` → `v1.17.2`"). |
| `v1.17.1` | `e8d4904a09194d99761042a8996983e8d5b957eb` | Installers report a program they could not remove, and exit 3. **Magic's pin.** |
| `v1.17.0` | `adc2c757d7e2fa9ed7e5eb5ff33a076eba34f4bd` | The `before_uninstall` hook, which Magic adopted in 0054 S9. |
| `v1.16.0` | `86f6e700f663bc7fee93ea4b58f30f8c281c7013` | `NewGitHubUpdater`, typed errors, handoff contexts, `NewVersionProberWith`, `NewSlogReporter`, opt-in `signednote`. |
| `v1.15.2` | `436a7f5976a6815491c7384527fef90869574a1b` | Task Scheduler stop grace bounded; a task whose stop failed is enabled again. |

- **No GitHub releases.** The releases endpoint returns an empty list, as 0207 also found. A
  consumer reads tags.
- **After the tag:** the default branch is three commits ahead, all `docs(…)` by their subjects:
  - `b0be3e18`, "update references for v1.17.2";
  - `18849cde`, "complete Go 1.27.3 migration plan";
  - `f07d3fdf`, "record rejected proposal to publish GitHub releases on tag".

  Their subjects name no code change; their diffs were not read for this report.
- **The move for Magic** is 0207 D1: `go.mod` and both `ci.yml` pins together. 0207's scratch copy
  moved `go.mod` alone. There, `go build`, `go vet` for three platforms and `go test -race` passed
  in 41 packages. The one failure, `TestWorkflowPinsMatchGoMod`, is the guard that requires the
  workflow pins to move with `go.mod` (0207, "The pin moves, proven on a scratch copy"). A run with
  all three moved has not been made.

## What Magic uses today

Magic imports 10 of the library's 14 public packages. Its production code references 51 distinct
library identifiers, and its tests 16 more.

| Package | Magic's production files | What Magic uses | Status |
| :--- | :--- | :--- | :--- |
| `buildinfo` | `internal/cli/root.go`, `internal/relay/cli.go`, `internal/updateclient/client.go` | `Identity`, `Info` | Canonical. |
| `selfupdate` | `internal/updateclient/client.go`, `managed.go`, `internal/cli/service/backend.go` | `New`, `Config`, `NewGitHubSource`, `GitHubOptions`, `NewStandaloneInstaller`, `NewManagedInstaller`, `NewStrictVersionPolicy`, `DefaultLimits`, `UserAgent`, `Lifecycle`, `EnabledLifecycle`, … (23 names in production code) | Canonical. `Probes` and `Verifiers` are unset (A5). |
| `selfupdate/cli` | `internal/updateclient/client.go` | `Flags`, `Options`, `StdioOptions`, `Run`, `Exit` | `Options.HandOff` unset (A1). |
| `selfupdate/codesign` | `internal/updateclient/managed.go` | `NewSigner`, `SignOptions` | `NewChecker` unused (A5). |
| `selfupdate/releasespec` | `internal/updateclient/spec.go` | `Parse`, `Spec` | Canonical; the spec drives the build, publish and installers. |
| `selfupdate/service` | `internal/updateclient/managed.go`, `internal/cli/service/health.go`, `internal/cli/health.go`, `internal/relay/health.go` | `NewExecReconciler`, `ExecReconciler`, `ExecOptions`, `Receipt`, `Runner`, `Health`, `HealthProbe` | `HandOffFunc`, `ReportFunc`, `PollHealthy` and `SameExecutable` unused (A1, A3, A6). |
| `selfupdate/service/systemd` | `internal/cli/service/backend.go`, `internal/daemon/daemon.go` | `New`, `Options`, `User`, `Ready`, `Stopping` | The backend is used by `update` and the installer hook only (A2). |
| `selfupdate/service/launchd` | `internal/cli/service/backend.go` | `New`, `Options`, `GUI` | As systemd (A2). |
| `selfupdate/service/taskscheduler` | `internal/cli/service/backend.go` | `New`, `Options` | As systemd (A2). |
| `selfupdate/selfupdatetest` | tests only | `NewGitHubServer`, `NewRelease` | Canonical. |
| `selfupdate/archive` | — | — | Not needed: the spec's packaging is `binary`. |
| `selfupdate/service/scm` | — | — | Not needed: Magic runs a per-user task, not a Windows service (0054 D3). |
| `selfupdate/verify/signednote` | — | — | Not adopted by decision (0205). |
| `selfupdate/verify/ghattest` | — | — | Not adopted; its premise still holds (A9). |

## Findings

### A1 — An agent-run update fails safe today; 0207 F1 overstates the harm

0207 F1 says that an `mcremote update` started by an agent inside the daemon would kill itself and
leave the service stopped on Linux and Windows, and that macOS is unaffected. The library's code
says otherwise for all three.

- **Every backend's `Stop` refuses from inside its service, before it changes anything:**
  - **systemd:** `Unit.Stop` checks `insideUnit` first, and returns `service.ErrInsideService`
    (`selfupdate/service/systemd/lifecycle.go:72-83`). `Inside` compares this process's cgroup with
    the unit's `ControlGroup`. Magic's agents start with `Setpgid` only
    (`internal/procutil/procutil_unix.go:29`), which does not leave the unit's cgroup. So an agent's
    update is inside.
  - **launchd:** `Job.Stop` returns `ErrInsideService` (`selfupdate/service/launchd/lifecycle.go:106`).
    `Job.Inside` is true when the job's process shares this process's group, *or is one of its
    ancestors* (`launchd/detach.go:25-51`). The daemon is the agent's ancestor, so the update is
    inside, whatever `Setpgid` did. 0207 F1's "On macOS … the update would survive" therefore misses
    the point: it never gets to the bootout.
  - **Task Scheduler:** `Task.Stop` refuses first (`taskscheduler/lifecycle.go:100-101`, `:143`).
    `Inside` walks up the recorded parents to a running instance (`taskscheduler/detach.go:15-49`,
    `internal/winproc/tree.go`).
  - Each refusal has unit and live tests in the library: `systemd_test.go`, `live_linux_test.go`,
    `launchd_test.go`, `live_darwin_test.go`, `lifecycle_test.go` and `live_windows_test.go`.
- **The managed installer turns that refusal into a clean failure.** `Install` stops a running
  service before it applies anything (`selfupdate/managed.go:192-196`). On a `Stop` error,
  `recoverStop` asks `Running`. A service that still runs is left running, and the error is
  returned. Its comment reads "a stop refused from inside the service leaves it running"
  (`managed.go:278-300`). No binary was replaced. `cli.Exit` prints `update failed: …` and exits 1.
- **So the cost of the missing handoff is a failed update, not an outage.** From a phone session,
  an agent asked to update Magic cannot. The operator must run `mcremote update` from a shell
  outside the service.
- **One Windows edge remains.** Magic's real kill scope on Windows is not the parent chain. It is
  the job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` that the daemon holds
  (`internal/procutil/procutil_windows.go:253-271`). The library's walk "ends at a parent that has
  exited" (`internal/winproc/tree.go`, `Descends`). If an agent's process chain has an
  intermediate that exited, `Inside` is false while the update is still in Magic's job. `Stop` then
  drains the daemon, the job closes, and the update dies with it, leaving the task stopped and
  disabled, as 0207 feared. This rests on reading only; it is not reproduced.

**Adopting the handoff, and what it needs.** The library's wiring is small:

- `cli.Options.HandOff` from `service.HandOffFunc(backend, spec)` and `service.ReportFunc()`;
- `service.ReportFunc` (or `LoadHandOffEnv`) called first in both products' `main`, so that a
  detached run, a Windows hop and the launchd environment file work;
- `launchd.Job.CleanupHandOffs` once at daemon start, which the library suggests.

Four things are Magic's to settle first:

1. **Windows needs `JOB_OBJECT_LIMIT_BREAKAWAY_OK` on Magic's agent job, or the handoff kills
   itself.** `DetachProcess` asks for `CREATE_BREAKAWAY_FROM_JOB`, and when the job refuses with
   `ERROR_ACCESS_DENIED` it "is then retried inside the job" (`selfupdate/service/detach_windows.go`,
   `startDetached`). Magic's job sets `KILL_ON_JOB_CLOSE` only. A detached run left inside that job
   stops the daemon, the daemon's exit closes the job, and the run is killed mid-update. 0207 F1
   said this was "to be measured, not assumed". The code settles it: the prerequisite is required.
   It has a cost. With `BREAKAWAY_OK`, any agent descendant that asks to break away escapes the
   kill-on-close containment of MADR 0116 D8. That is an owner decision (Q1).
2. **A detached run cannot prompt.** Inside the service, `HandOffFunc` refuses a request without
   `--yes` with `ErrConfirmationRequired` (library `service` package doc, `HandOffFunc`). An agent
   must run `mcremote update --yes`.
3. **runit, s6 and OpenRC have no `Detacher`.** Magic's `scriptBackend` implements none. It can
   use `service.ProcessDetacher` with its own inside check, but `DetachProcess` "is no defence
   against a cgroup kill" (library doc). The handoff is exact only for systemd, launchd and Task
   Scheduler.
4. **The result is read later.** The detached run writes a `HandOffResult` beside the executable
   (`DefaultResultPath`). Nothing in Magic reads it yet. `doctor` could report the last one.

### A2 — `setup-service` controls services with its own code where the backends would do it

0054 O1 moved "every concern the library owns". The update path and the installer hook's
upgrade (`internal/cli/service/installerhook.go:147-197`) use the backends' `Stop` and `Start`.
`setup-service`'s own install, `--force` and `--remove` paths do not. 0207 F2 named part of this.
The full list, with the ways each copy is weaker:

| Magic's code | Library equivalent | What Magic's copy lacks or gets wrong |
| :--- | :--- | :--- |
| `stopAndWaitDarwin`, `launchdLoaded` (`launchd_wait.go:54-93`), called from `setup.go:600` and `:820` | `launchd.Job.Stop` | **Its bound is 15 s** (`launchd_wait.go:26`). The plist Magic renders sets `ExitTimeOut` to 45 s (`plist_render.go:172`). `Job.Stop` waits for the longer of the poll timeout and `ExitTimeOut` + 30 s, here 75 s. The daemon's drain bounds its HTTP shutdown at 10 s and its admin drain at 2 s (`internal/daemon/daemon.go:697-706`), so 15 s is a thin margin, not an observed failure. The comment's reference, "the 15s active-wait already in update/swap.go" (`:22`), names a file that no longer exists. |
| `launchdJobRunning` (`launchd_wait.go:103-112`), from `setup.go:576`, `:597` | `launchd.Job.Running` | It counts `state = waiting` as running (`:111`). `parseLaunchctlPrint` in the same package says "waiting" is "a KeepAlive job held back after exiting, which is a crash loop, not a running daemon" (`settle.go:256-259`). The package holds both readings. |
| The enable, bootstrap, kickstart sequence (`setup.go:604-626`) | `launchd.Job.Start` | No retry. `Job.Start` "enables the label (bootstrap of a disabled label fails with 5), bootstraps the plist when the job is not loaded, retrying launchd's transient 5 and 37 within the deadline, then kickstarts it". Magic's falls back to one `kickstart -k` on any bootstrap error. That error 5 is the one S11's restore met (0054 PLAN S11, step 5). |
| `startWindows`, `stopWindows` (`control_schtasks.go:55-104`), from `setup_schtasks.go:72`, `:90` | `taskscheduler.Task.Start`, `Task.Stop` | **`stopWindows` hard-kills.** Its comment says `schtasks /end` "is a TerminateProcess, NOT a graceful signal: the daemon gets no drain" (`:83-84`). `Task.Stop` with Magic's `RequestStop` drains, as 0054 PLAN S9d and S10b measured (exit 0, against 1067). So `setup-service --remove` kills the daemon on Windows, where `update` drains it. (`--force` re-registers and runs the task without stopping it.) |
| `taskState`, `isInstalledWindows` (`control_schtasks.go:19-53`), also from `refresh_schtasks.go:31` and `uninstall.go:219` | `taskscheduler.Task.Installed`, `Running` | Each call starts PowerShell, measured at about 1.3 s (0159 probe 11). The backend reads the same state through the Task Scheduler COM API. |
| `systemctl --user restart` (`setup.go:487`) and `stop` (`:770`) | `systemd.Unit.Stop`, `Unit.Start` | No inside guard. `Unit.Start` also records the invocation, so `WaitHealthy` can require a new instance. The `daemon-reload`, `enable` and `loginctl enable-linger` around it are definition management, and stay Magic's. |
| `runCmd("systemctl" …)`, `runCmd("launchctl" …)` (`setup.go:1338-1345`, `control.go:199-205`) | The backends' absolute tool paths | Magic finds the tools on `PATH`. The library never does: "a runner never looks a tool up on PATH" (`service.Command`). |

Two consequences follow from the table:

- **None of `setup-service`'s stops guard against running inside the service.** What an agent
  that runs `mcremote setup-service --force` from a phone session causes differs by OS (from code):
  - **Linux:** the restart is a systemd job, so it completes. But with `KillMode=control-group`
    (`mcremote.user.service.tmpl:44`), the `setup-service` process dies with the unit's cgroup. The
    steps after the restart then never run: linger, the unit-name and backend records, and the
    verified start (`setup.go:487-497`, `:423-431`).
  - **macOS:** the bootout reaps the job's process group, and the agent's group is its own, so
    `setup-service` survives and bootstraps the job again.
  - **Windows:** `--force` re-registers and runs the task, and stops nothing; only `--remove`
    stops it, as intended.

  A library backend's `Stop` would refuse with `ErrInsideService` in each case.
- **What can move, and what cannot.** The backends do not write, enable or remove definitions.
  The library still "never creates, renders or deletes a service definition", as the 0054 report
  found. So the move is:
  - `setup`'s stop and start, and `--remove`'s stop, call the backend;
  - rendering, writing, `daemon-reload`, `systemctl enable`, `launchctl disable` before a removal,
    and `schtasks /create` and `/delete` stay.

  One semantic needs care. `setupLaunchdAgent` honours `--no-enable` (`setup.go:604`), and
  `Job.Start` always enables the label first. With `--no-enable`, `setup` keeps its own start.

### A3 — Three ways to confirm a start

| Path | How it confirms a start | Runs the product's `/health` probe? |
| :--- | :--- | :--- |
| `mcremote update` | the backend's `WaitHealthy`: a new instance, stable for the settle window, then `Options.Probe` | yes (0054 D5) |
| The installer hook's upgrade | the backend's `Stop` and `Start`, then Magic's `verifyStart` (`installerhook.go:184`) | no |
| `setup-service` | Magic's `verifyStart` (`setup.go:430`) | no |

- **Magic's `settle` (`settle.go:82-128`, `settleSystemd` `:137-178`)** samples `systemctl show`,
  `launchctl print` or the backend's `Running` once a second for `MCREMOTE_SVC_WAIT_SECS`
  (default 15). It reports running, starting or failed.
- **The library has both pieces it needs.** `WaitHealthy` covers systemd, launchd and Task
  Scheduler, with the probe. `service.PollHealthy(ctx, probe, PollOptions)` is the generic loop:
  - Ready, as one instance, for the settle window;
  - an instance change restarts the window;
  - `Failed` ends it at once;
  - `ErrTimeout` when it runs out (library `service` doc).
- **Proposed shape:**
  - for the library kinds, `verifyStart` calls the backend's `WaitHealthy`, with
    `PollOptions.Timeout` from `MCREMOTE_SVC_WAIT_SECS`;
  - for runit, s6 and OpenRC, the per-second loop becomes `PollHealthy` over a `HealthProbe` built
    from the existing parsers. The process ID is the `Instance`, and "down, want up" twice is
    `Failed`;
  - `ErrTimeout` maps to `HealthStarting`, `ErrUnhealthy` to `HealthFailed`, and nil to
    `HealthRunning`.

  The three outcomes, the warning text and `diagnose` stay.
- **What it gains:** one definition of "started" across `update`, the hook and `setup`, with the
  `/health` probe on all three. It also removes the two loops (`settle.go:82-178`, about 95 lines)
  and the systemd and launchd samplers. The settle window must be shorter than the timeout
  (`PollOptions.Validate`). With the 15 s default and launchd's 10 s settle, that holds.

### A4 — The script backend does not follow the backend contract

`scriptBackend` (`internal/cli/service/scriptbackend.go`) is Magic's `Lifecycle` for runit, s6 and
OpenRC. The library has no backend for them. Against the library's contract:

- **It ignores its context.** `Stop`, `Start` and `WaitHealthy` take `context.Context` and do not
  use it (`:80`, `:110`, `:146`). Their commands run without one, and `Stop` sleeps to its own
  deadline. The library's backends return `ctx`'s error when it ends, and the managed installer
  bounds recovery by context (`managed.go`, `recoveryTimeout`).
- **Its errors carry no library sentinel.** A timeout or a failed start is a plain `fmt.Errorf`
  (`:100`, `:156`, `:158`). The backends' errors match `service.ErrTimeout`, `ErrUnhealthy`,
  `ErrPermission` and `ErrNotInstalled` with `errors.Is` ("The errors every backend returns",
  library `service` doc).
- **It has no inside check,** so it cannot refuse a stop from inside, and implements no
  `Detacher` (A1).

These are fixes inside Magic. A10 has the alternative: the library adopts these backends.

### A5 — The update's composition skips cheap library guards

`updateclient.New` (`internal/updateclient/client.go:68-133`) composes every part explicitly, as
0054 intended. Three parts the library offers are left out:

- **No staged-binary probe.** `Config.Probes` run "the verified, runnable staging file … after any
  transform and before anything is replaced" (library `Config`).
  - `selfupdate.NewVersionProber([]string{"version"}, nil, timeout)` runs the staged `mcremote
    version`, and requires exit 0 and the tag in its output. The spec already declares those
    arguments: `identity_args: ["version"]` (`selfupdate-release.json`; library `Product.IdentityArgs`).
  - Today a binary that cannot run on this host is found only after the service was stopped and
    the binary swapped: when the reconcile child (the new binary's `setup-service --refresh`) or
    `WaitHealthy` fails, and the installer rolls back. The probe finds it before the stop, with no
    downtime.
  - On macOS with `MC_CODESIGN_IDENTITY` set, `codesign.NewChecker` is the matching probe for the
    re-signed file.
  - **Not probed here:** the existing `updateclient` tests serve fake binaries from
    `selfupdatetest`. A probe would need a runnable fixture, or a `NewVersionProberWith` `Run`
    seam in tests.
- **No image check.** `NewImageVerifier(Platform{})` requires the download to be an ELF, Mach-O or
  PE image for the platform. `New` refuses it beside an `Unpacker`, and Magic's spec gives none for
  `binary` packaging (`releasespec/spec.go:325-333`). It is defence in depth behind `SHA256SUMS`.
- **The HTTP client is the shape the library advises against.** Magic sets
  `&http.Client{Timeout: OperationTimeout}`, 15 minutes over each request including the download
  (`client.go:91`), with no response-header timeout.
  - `GitHubOptions.Client`'s documentation: "prefer a context deadline, or a Transport with
    ResponseHeaderTimeout, so a slow large download is not cut off (0025-MADR D14)".
  - `cli.Run` already bounds the whole run with the same 15 minutes (`client.go:232`).
  - So a GitHub request that stalls before its headers waits up to 15 minutes, where the library's
    default waits 30 seconds.
  - The library's default client is unexported (`defaultHTTPClient`, `githubupdater.go`). Magic can
    build the same shape (clone `http.DefaultTransport`, set `ResponseHeaderTimeout`, no
    `Client.Timeout`), or ask for the constructor to be exported (A10).

### A6 — `binaryWarning` reimplements `service.SameExecutable` weakly

0207 F6 and D8 cover this. `binaryWarning` compares `exe == unitBinary` as strings after
`EvalSymlinks` (`internal/cli/service/refresh.go:589-605`). `service.SameExecutable` compares cleaned
paths, case-insensitively on Windows, then by `os.SameFile`, which matches 8.3 short names and
junctions. This report confirms the finding and adds nothing to it. `samePath` in `migrate.go:53`
stays, for the reason 0207 gives.

### A7 — The Windows local CI script stamps symbols that no longer exist

`scripts/ci-windows-local.ps1:132` builds the smoke binaries with
`-X main.version=… -X main.commit=… -X main.date=… -X main.buildKind=local`. No `main` package
declares those variables any more. Identity comes from `buildinfo`, which the `Makefile` stamps as
`-X $(BUILDINFO).version=… -X $(BUILDINFO).kind=…` (`Makefile:23`, `:172`). That is "exactly the
-X pair buildinfo.LDFlags renders" (`Makefile:6`).

**Proven on this host.** A build with the script's flags, to a scratch output, and its `version`:

```text
$ go build -trimpath -ldflags "-s -w -X main.version=9.9.9.gabc1234 -X main.commit=abc1234 -X main.buildKind=local" -o <scratch>/mcremote-ps1stamp ./cmd/mcremote
$ <scratch>/mcremote-ps1stamp version
v0.22.3-0.20261021214436-3e6d15b57921 (local) 3e6d15b57921
```

- **The linker drops the stamps without a word.** The binary reports the toolchain's VCS version.
- **Check B14 of `make ci-windows-smoke`** requires the output to contain the stamped `$ver`
  (`ci-windows-local.ps1:154-160`). So it cannot pass as written. That is inferred from this
  experiment, not run on Windows. AGENTS.md makes that target a before-tag gate on Windows hosts.
- **The fix is the `Makefile`'s pair:** `-X github.com/maccavelli/go-selfupdate-lib/buildinfo.version=$ver`
  and `….kind=local`.

### A8 — Smaller items

- **mcrelay does not tell systemd it is ready.** mcremote's unit is `Type=notify`, and its daemon
  sends `READY=1` and `STOPPING=1` through `systemd.Ready` and `systemd.Stopping`
  (`internal/daemon/daemon.go:689`, `:695`). mcrelay's unit is `Type=simple`
  (`mcrelay.user.service.tmpl:18`), and nothing in the relay sends a state.
  - Under `Type=simple`, `systemctl start` returns once the process forks, so readiness is judged
    by sampling alone.
  - Adopting it would take the relay's two notify calls, at its ready and shutdown points, and
    `Type=notify` in its template; the size was not measured. It changes nothing for the update
    path, whose `WaitHealthy` already probes.
- **Detection could use `systemd.Available()`.** `detectLinux` takes systemd when `systemctl` is on
  `PATH` (`detect.go:99-105`), and never falls through from a broken systemd, by design (0099).
  - `systemd.Available()` is the `sd_booted(3)` test: `/run/systemd/system` exists.
  - On a host that has `systemctl` installed but did not boot systemd, Magic reports
    `KindSystemdBroken` where runit, s6 or OpenRC may be the real supervisor.
  - Whether that host class matters is a product question. It is noted, not recommended.
- **macOS's receipt never reports a reload (0054 F5, open).** `refreshLaunchd` never sets
  `Reloaded` (`refresh.go:256-323`). The library's `launchd.Job.Reconcile` reloads a job that is
  loaded but not running, and `Restore` does the same. Magic's refresh child cannot call those,
  because it re-renders the whole plist rather than rewriting one path. The fix stays in Magic's
  receipt, as S10-2 did for Windows. This report adds no new evidence to F5.

### A9 — Library parts that stay unadopted, checked against `v1.17.2`

| Part | Reason, re-checked |
| :--- | :--- |
| `verify/ghattest` | Its premise still holds at `v1.17.2`: "it runs gh … and gh must be logged in even for a public repository … It suits a developer's machine … more than an unattended service" (package doc). An opt-in for developer machines is possible, not needed. |
| `verify/signednote` | 0205: no release signing while releases stay immutable. |
| `archive` | The spec's packaging is `binary`. |
| `service/scm` | Magic runs a per-user Task Scheduler task (0054 D3). |
| `Checker`, `CheckStore`, `NewFileCheckStore` | A cached "is there an update?" for a notice in `doctor`, `/health` or the phone. Magic has no such feature, so nothing is duplicated. Adopting it would be new scope. |
| Prerelease channels, `ReplaceBeforeStop` | As 0054's "What stays deliberately unadopted". |
| `systemd.Watchdog`, `Status`, `Reloading` | The unit sets no `WatchdogSec`, and nothing in Magic sends a watchdog, status or reload state. |
| `ManagedInstaller.KeptBackups` | `doctor` could list previous binaries an update left. Not a duplicate. |
| SemVer helpers | `NewSemverPolicy` is a `VersionPolicy` for strict `vX.Y.Z` tags. It does not fit provider versions such as `1.18.27`. 0207 D5's `x/mod/semver` stands. |

### A10 — What Magic cannot move without a library change

These are the remaining duplications that the library would have to grow to remove. At the owner's
request (Q2), each was filed on `go-selfupdate-lib` on 2026-10-21, citing the library at `v1.17.2`
and this repository at `6db2792`.

1. **runit, s6 and OpenRC backends**
   ([go-selfupdate-lib#2](https://github.com/maccavelli/go-selfupdate-lib/issues/2)).
   - Magic has `scriptBackend` (159 lines) and their sampling. 0054 O2 put the supervisors in
     `setup-service` because no library backend existed.
   - Upstream, they would gain the backend contract (A4) and a `Detacher`.
   - Rendering their run scripts would stay Magic's, as for every backend.
2. **Task Scheduler definitions**
   ([go-selfupdate-lib#3](https://github.com/maccavelli/go-selfupdate-lib/issues/3)).
   - Magic registers, exports and deletes its task by `schtasks /create /xml`, `/query /xml` and
     `/delete` (`setup_schtasks.go`, `refresh_schtasks.go:145-162`).
   - It compares definitions semantically, because Task Scheduler rewrites what it stores
     (`setup_schtasks.go:134-318`).
   - The library already talks COM to Task Scheduler. A `Register` and `Definition` pair, and a
     definition comparison, would remove `schtasks.exe` and PowerShell from Magic's Windows path.
3. **A launchd "loaded" probe**
   ([go-selfupdate-lib#4](https://github.com/maccavelli/go-selfupdate-lib/issues/4)).
   - `ProbeStatus` still runs `launchctl print` itself, because no backend method says "loaded"
     (`control.go:150-156`; 0054 PLAN S1, Deviation 2).
   - `launchd.Job` has `Installed`, `Enabled` and `Running`, but not loaded.
4. **The default HTTP client** (A5;
   [go-selfupdate-lib#5](https://github.com/maccavelli/go-selfupdate-lib/issues/5)): exported as a
   constructor, so a consumer that composes `New` explicitly gets the same timeouts
   `NewGitHubUpdater` uses.

`internal/winproc`, the library's Windows process tree and liveness code, is internal. Magic's
`procutil` job objects do a different job (containment, not detection), so there is no duplicate
to ask about.

## What stays Magic's, by the library's design

These are the boundaries the library draws, re-checked at `v1.17.2`:

- **Service definitions:**
  - the systemd unit, the launchd plist, the task XML and the supervisor scripts, all rendered
    from Magic's templates;
  - the `setup-service --refresh --json` child that re-renders them, and its receipt;
  - the in-process restore (`RestoreUnitBackup`), which `ExecReconciler` calls.

  The backends' own `Reconcile` only checks or rewrites the binary path, so it cannot replace a
  re-render.
- **The Windows folder migration** (`migrate.go`), the installer hooks' arguments, and
  `setup-service --uninstall`'s removal of definitions.
- **The health probe's content** (`health.go`): how to reach each product's daemon.
- **The release spec's content,** the codesign identity, the APK build, and the developer
  `make install` path.

## Recommended sequence, for the owner's review

This decides nothing. It maps the findings onto 0207, whose open question 1 asks whether the work
is one PLAN or amendments.

| Step | Findings | 0207 today | Risk | Verification |
| :--- | :--- | :--- | :--- | :--- |
| 1 | Pin `v1.17.2` | D1 | none (toolchain only) | `go.mod` and both pins; `TestWorkflowPinsMatchGoMod` |
| 2 | A6, A7, A5's HTTP client, A5's image check | D8 has A6; the rest is new | low | unit tests; A7's B14 on the Windows host |
| 3 | A5's version probe and codesign check | new | low; the probe runs before any stop | a runnable fixture, or the `Run` seam |
| 4 | A2, A3, A4: `setup-service` on the backends; one start confirmation | extends D4 | medium: changes service behaviour on three operating systems | live `setup-service` install, `--force` and `--remove` on each test host, the real install backed up and restored |
| 5 | A1: the handoff | D3, whose premise this report corrects | medium to high on Windows (Q1) | an agent-run `update --yes` from a phone session on each host; on Windows with an intermediate parent that has exited |
| 6 | A8's mcrelay notify | new | low | `systemctl --user start` returns after `READY=1` |

0207's D3 says "reproduce it on the Linux test host first". After A1, the reproduction to expect is
`update failed: … this process runs inside the service it would stop`, with the daemon still up and
unchanged. It is not a dead service.

## Questions for the owner

1. **Windows breakaway (A1).** The handoff on Windows needs `JOB_OBJECT_LIMIT_BREAKAWAY_OK` on
   Magic's agent job. That lets an agent descendant leave the kill-on-close containment of 0116 D8.
   The options:
   - accept it;
   - keep the handoff off Windows, where an agent-run update keeps failing safe;
   - or ask the library for a different detach.
2. **Library asks (A10).** File any of the four as `go-selfupdate-lib` issues? *Answered
   2026-10-21:* all four filed, as
   [#2](https://github.com/maccavelli/go-selfupdate-lib/issues/2),
   [#3](https://github.com/maccavelli/go-selfupdate-lib/issues/3),
   [#4](https://github.com/maccavelli/go-selfupdate-lib/issues/4) and
   [#5](https://github.com/maccavelli/go-selfupdate-lib/issues/5).
3. **Where the work lives.** As amendments to 0207 (D3 corrected, D4 extended, new decisions for
   A5, A7 and A8), or as a new MADR for `setup-service`'s move onto the backends?
4. **mcrelay readiness (A8).** In scope with the rest, or left as is?

## Evidence notes

- `git ls-remote --tags https://github.com/maccavelli/go-selfupdate-lib`: the five tags in the
  release table, newest `v1.17.2` at `036289ec…`.
- `gh api repos/maccavelli/go-selfupdate-lib/releases?per_page=5`: an empty list.
- `gh api repos/maccavelli/go-selfupdate-lib/compare/v1.17.2...HEAD`: `ahead_by` 3, the three
  documentation commits named above.
- `gh issue list -R maccavelli/go-selfupdate-lib --state all`, before filing: one issue, #1
  (closed), so none of A10's four was a duplicate. After filing: #2 to #5 open, each labelled
  `enhancement`.
- Library documentation quoted above is `go doc -all` output at the tag, for `selfupdate`,
  `selfupdate/cli`, `selfupdate/service`, `service/systemd`, `service/launchd`, `codesign`,
  `releasespec` and `verify/ghattest`.
- Magic's import map: 10 packages; 51 production identifiers; 16 test-only ones:
  - `buildinfo.KindRelease`;
  - `selfupdate.Platform`, `ReleaseBuild`, `Request` and `TransformRequest`;
  - `selfupdatetest.NewGitHubServer` and `NewRelease`;
  - `service.Command`, `ErrTimeout`, `Output`, `PollHealthy`, `PollOptions` and `RunnerFunc`;
  - `launchd.Job`, `systemd.Unit` and `taskscheduler.Task`.
