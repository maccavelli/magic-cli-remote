---
status: proposed
date: 2026-09-18
decision-makers: Project Owner
consulted: none
informed: none
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The Windows service path is half-wired: it installs a daemon it cannot refresh, cannot log, and mis-describes

## Context and Problem Statement

MADR 0116 made `windows/amd64` a shipped, CI-tested target and MADR 0145 gave
it a local gate. Both succeeded at the level they aimed at: the tree compiles,
`go vet` is clean, the unit suite passes, and the acceptance script runs green
on a real Windows host. Measured again here — `GOOS=windows GOARCH=amd64
CGO_ENABLED=0 go vet ./...` exits 0, and `go test` over `internal/cli/service`,
`internal/admin`, `internal/appdirs`, `internal/procutil` and `internal/fsutil`
passes on Windows 11 build 26200 with go1.26.6.

What those gates do not reach is the *service lifecycle*. `scripts/acceptance-windows.ps1`
asserts `version`, `paths --json`, `pair create`, `pair list` and `doctor`; it
never installs the service, never starts the daemon, and never runs an update
(`scripts/acceptance-windows.ps1:74-176`, with `setup-service` and `serve`
relegated to two unassertable "Manual steps" at `:181-186`). Everything past
that line is unexamined, and this pass found it is where the Windows port stops
being finished.

The problem is not that Windows support is absent. It is that the install path
was completed and the paths that *depend* on a successful install were not:
refresh, logging, restart, environment, principal resolution, status probing,
and the post-install summary all still assume systemd or launchd. A Windows
operator can install the daemon, and then cannot update it, cannot read its
logs, is told to run `systemctl` and `journalctl`, and gets a console window at
every logon.

This record states what was measured, names the decisions needed to close it,
and does not implement them.

### What was measured, not assumed

All measurements were taken on this host: **Windows 11 Home, build 26200
(10.0.26200), go1.26.6 windows/amd64**, as `MAC420\macsm`, in an
unprivileged shell.

Three throwaway probe programs were run from `%TEMP%\kilo` (outside the
repository; the tree was not written). Their outputs are quoted verbatim where
a finding rests on them.

**Probe 1 — what `os.Lstat` reports for a live AF_UNIX socket file on Windows.**
A Go `net.Listen("unix", …)` socket, then `os.Lstat` on its path:

```text
Mode()=Srw-rw-rw-
IsDir=false IsRegular=false
ModeSocket bit set?   true
ModeIrregular bit set? false
ModeSymlink bit set?   false
admin.go:96 'exists and is not a socket' fires? false
fi.Sys() concrete type = *syscall.Win32FileAttributeData
asserts to *windows.ByHandleFileInformation? false
asserts to *windows.Win32FileAttributeData? false
os.Stat ok, mode=Srw-rw-rw-
```

**Probe 2 — where a real file identity comes from, and what a stale socket does.**

```text
Win32FileAttributeData: attrs=0x420 ctime={376844663 31278177} size=0
  -> any file-index field? NO (struct has no FileIndex)
ByHandleFileInformation: idxHi=983040 idxLo=979006 -> identity=4222124651638846
dial stale socket ERR (expected): dial unix …\admin.sock: connect: No connection
  could be made because the target machine actively refused it.
os.Remove stale socket ERR: remove …\admin.sock: The system cannot find the file specified.
after remove, Lstat IsNotExist=true
second bind while live: err=listen unix …\admin.sock: bind: Only one usage of each
  socket address (protocol/network address/port) is normally permitted.
```

The `os.Remove` error is the interesting line: after a *graceful* `ln.Close()`
Windows had already deleted the socket file, so there was nothing to remove.

**Probe 3 — what survives a hard kill**, i.e. exactly what `schtasks /end` does
(`internal/cli/service/control_schtasks.go:52-57` documents `/end` as a
`TerminateProcess`). A child process listened on the socket and was killed with
`Process.Kill()`:

```text
child pid=17868 ready="CHILD_READY"
PRE-KILL socket file exists: true
POST-KILL socket file SURVIVES: mode=Srw-rw-rw-
=> stale socket file DOES survive a hard kill
   dial stale: ERR (expected) … target machine actively refused it.
   os.Remove stale: OK
   rebind after remove: err=<nil>
```

**Test-suite measurements.** `go test -v` over the five Windows-relevant
packages, on this host:

```text
--- SKIP: TestSocketIdentityStable (0.00s)
--- SKIP: TestWriteFileAtomic (0.00s)
--- SKIP: TestWriteFileAtomicInjectedFailures (0.00s)
--- SKIP: TestSystemRootsDispatches (0.00s)
--- SKIP: TestSystemPaths (0.00s)
--- SKIP: TestDefaultConfigFile (0.00s)
ok  internal/cli/service  ok  internal/admin  ok  internal/appdirs
ok  internal/procutil     ok  internal/fsutil
```

`TestSystemPaths` and `TestDefaultConfigFile` skip with a legitimate reason
("Known Folder lookup is covered in roots_windows_test.go"). The other four do
not, and are findings.

**Static measurements.** Repo-wide greps, each reproduced in the Evidence index:

* `windowsgui|ShowWindow|GetConsoleWindow|SW_HIDE|CREATE_NO_WINDOW|DETACHED_PROCESS`
  → **zero hits** in any `.go`, `Makefile`, `.mk`, `.ps1`, `.sh` or `.yml`.
* `MC_WINDOWS_SIGN|signtool|Authenticode` → hits **only** in `docs/`.
* `ExtraEnviron|Environ` in `schtasks.go` and `setup_schtasks.go` → **zero hits**.
* `SuperviseStarted` → six production call sites (`acpagent.go:470`,
  `acpagent/terminal.go:119`, `acphttp/provider.go:361`, `codex/provider.go:591`,
  `httpagent/provider.go:535`, `providerauth/cli.go:129`), so MADR 0150's
  tree-kill guarantee is genuinely wired. This is not a finding; it is recorded
  because it was checked.

### Findings

**F1 — `mcremote update` and `mcrelay update` always roll back on Windows once
the service is installed.** The chain is complete and unconditional:

1. `internal/cli/service/refresh.go:117-124` — `RefreshUnit` dispatches on
   `linux` and `darwin` only; every other OS returns
   `"setup-service --refresh is only supported on Linux and macOS (running on %s)"`.
2. `internal/cli/setup_service.go:103-108` — the `--refresh` branch returns that
   error, so the child process exits non-zero (`cmd/mcremote/main.go:34`).
3. `internal/cli/service/exec_refresher.go:61-69` — a non-zero child becomes an
   error from `ExecRefresher.RefreshUnit`.
4. `internal/updateclient/lifecycle.go:146-156` — `Reconciler.Reconcile` returns
   it, and its own comment states "a reconcile failure is fatal and enters the
   shared rollback path".
5. `mcplib@v1.4.1/selfupdate/managed.go:86-88` — `if recErr != nil { return
   InstallResult{}, s.recover(ctx, product, applied, receipt, true, recErr) }`.

The managed path is taken because `service.IsInstalled` →
`isInstalledWindows` (`control_schtasks.go:35-40`) reports `true` for a
registered task. So on Windows: install the service, then every subsequent
`update` downloads, verifies, swaps the binary, fails reconciliation, and rolls
the swap back. Self-update is unavailable to every Windows operator who follows
the documented install path. `docs/ops-windows-install.md` does not mention
`update` at all.

**F2 — `setup-service` prints systemd instructions on Windows.**
`internal/cli/service/result_print.go:48-57` and `:75-135` switch on `res.Scope`
with exactly two arms: `case "launchd-agent"` and `default`. The Windows path
sets `res.Scope = "windows-task"` (`setup_schtasks.go:20`, `setup.go:314`),
which falls into `default`. A Windows operator who has just successfully
installed a Task Scheduler task is told:

```text
Unit file:         Task Scheduler\mcremote
Unit name:         mcremote.service
Scope:             systemd-user
Enabled:           yes (systemctl --user enable)
Started:           yes (systemctl --user restart/start)
Linger:            not enabled (run: loginctl enable-linger $USER)
Note: setup-service does not install the binary.
      Install/update it with: make install
Status:  systemctl --user status mcremote
Logs:    journalctl --user -u mcremote -f
Stop:    systemctl --user stop mcremote
Disable: systemctl --user disable --now mcremote
```

Every command in that block is absent on Windows, the scope label is wrong, the
unit name gains a `.service` suffix the task does not have, and `make install`
contradicts the documented `install.ps1`. `internal/relay/cli.go:492-501` proves
the scope value was known to the authors — mcrelay grew a correct
`case "windows-task"` addendum about public ports — but it calls the shared
printer first at `:480`, so mcrelay on Windows emits the wrong systemd block
*and* the right Windows note, one after the other.

**F3 — The Windows daemon has no log destination, while `mcremote paths`
advertises one.** `internal/appdirs/roots_windows.go:64` sets
`Logs: filepath.Join(base, "Logs")`; `internal/appdirs/paths.go:63-71` turns any
non-empty `roots.Logs` into `p.LogDir`; `internal/cli/paths.go:91-92` prints
`log_dir:`. So `mcremote paths` on Windows reports
`%LocalAppData%\mcremote\Logs`, and `docs/ops-windows-install.md` lists it in
the "Where things live" table as though it were populated.

Nothing writes there. `internal/logging/slog.go:20-24` defaults `Out` to
`os.Stderr` and `internal/cli/serve.go:80-83` passes no `Out`. A Task Scheduler
`Exec` action cannot redirect stdout or stderr, and `taskDefinition`
(`schtasks.go:37-87`) has no field for it. The only code that creates a log
directory is `setupLaunchdAgent` (`setup.go:431-435`), and the only code that
names log files is `plist_render.go:177-178` (`StandardOutPath` /
`StandardErrorPath`); systemd gets journald
(`mcremote.user.service.tmpl:55-57`). There is no `--log-file` flag anywhere in
the repository. Consequence: **every log line the Windows daemon emits is
discarded**, and the directory operators are pointed at is never created.
MADR 0157 (daemon-owned rolling logs) is the designed fix and is still
`status: proposed`.

**F4 — `socketIdentity` always fails on Windows, so the admin socket is never
cleaned up and its safety guard is dead code.**
`internal/admin/owner_windows.go:41-47` asserts
`fi.Sys().(*windows.ByHandleFileInformation)`. Probe 1 measured the concrete
type as `*syscall.Win32FileAttributeData`; the assertion is `false` on every
call. Probe 2 confirms why: `Win32FileAttributeData` carries
`FileAttributes`/`CreationTime`/`FileSize` and **no file index at all** — a real
index requires `CreateFile` + `GetFileInformationByHandle`, which returned
`identity=4222124651638846` for the same file.

So in `internal/admin/admin.go:129-134` `sockInode` stays `0`, and the shutdown
removal at `:146-150` is gated on `ok && sockInode != 0 && id == sockInode` —
three conditions that can never all hold. The socket file is never removed on a
graceful Windows shutdown, and the inode-identity check that exists to stop this
daemon deleting a *different* daemon's socket does not run. The comment at
`owner_windows.go:39-40` ("Any failure reports (0, false), which the caller's
`sockInode != 0` guard already treats as 'do not remove' — the conservative
branch") anticipated a failure mode and accepted it, without noticing it is the
only mode.

`internal/admin/owner_windows_test.go:57-59` is
`if !okA || !okB { t.Skip("file index unavailable on this filesystem") }`, so
the test that would have caught this **can never fail on Windows**. Measured:
`--- SKIP: TestSocketIdentityStable`.

**F5 — A hard kill does leave the socket file behind, and recovery works only
because the ping fails.** Probe 3 measured that after `TerminateProcess` the
socket file survives with mode `Srw-rw-rw-`; dialling it returns "target machine
actively refused it", `os.Remove` succeeds, and rebinding succeeds. So the
`docs/ops-windows-install.md` claim that "a stale admin socket is detected and
cleared on the next start" holds — via `admin.go:109-114`, which pings and then
removes. It holds *because the dial fails*, not because anything verified the
file's identity; given F4 there is no identity check to fall back on. Probe 2
also measured the asymmetry: after a graceful `Close()` Windows deletes the file
itself, so the same `os.Remove` fails with "The system cannot find the file
specified." Two shutdown paths, two different filesystem outcomes, one guard
that never runs.

**F6 — `os.ModeSocket` *is* reported for a Windows AF_UNIX socket file
(hypothesis closed).** `admin.go:96` refuses a path whose mode lacks
`ModeSocket`, and `internal/appdirs/security_windows.go:66` refuses any
`ModeIrregular` directory as "a reparse point". A Windows AF_UNIX socket file
*is* a reparse point — probe 2 measured `attrs=0x420`
(`FILE_ATTRIBUTE_REPARSE_POINT|FILE_ATTRIBUTE_ARCHIVE`) — and Go maps
unrecognised reparse tags to `ModeIrregular`, so both guards looked likely to
misfire and wedge the daemon permanently. Probe 1 measured the opposite: Go
reports `Srw-rw-rw-` with `ModeSocket` set and `ModeIrregular` clear, because it
special-cases `IO_REPARSE_TAG_AF_UNIX`. Neither guard fires. **This hypothesis
is closed**; it is recorded so the next reader does not spend a probe on it.

**F7 — Restart parity: Windows restarts only on failure, at most three times.**
`schtasks.go:121-124` sets `RestartOnFailure` `Interval=PT1M`, `Count=3`. The
Unix paths are unconditional: `mcremote.user.service.tmpl:28-31` sets
`Restart=always` with `RestartSec=5` and a generous `StartLimitIntervalSec=300`
/ `StartLimitBurst=30`, and `plist_render.go:169-171` sets `RunAtLoad` plus
`KeepAlive` true with `ThrottleInterval` 2. Task Scheduler's `RestartOnFailure`
fires only when the action returns non-zero, so a Windows daemon that exits 0 —
a clean shutdown, a config-validation exit, an `os.Exit(0)` — is **never**
restarted, while systemd and launchd both bring it back. After three failures
Windows gives up for good; the Unix paths keep retrying. The code comment calls
this "the closest analogue to the systemd unit's Restart=", which is true of the
element and false of the behaviour.

**F8 — There is no graceful stop on Windows, and no telemetry that would show
it.** `control_schtasks.go:52-57` documents `schtasks /end` as a
`TerminateProcess` with no drain, and `internal/cli/signals_windows.go:15-17`
and `internal/relay/signals_windows.go:15-17` return only `os.Interrupt` because
Windows never delivers SIGTERM. That much is a known, documented decision. What
is not documented is that it is *unobservable*: the Unix paths get
`TimeoutStopSec=45` / `ExitTimeOut 45` and a journald or file record of the
drain, while Windows gets neither a drain nor — per F3 — any record that the
daemon existed. A hub torn down mid-flight on Windows leaves no trace to debug
from.

**F9 — The task XML carries no environment block, the PATH helper is
POSIX-only, and `--env` is silently dropped.** `taskDefinition`
(`schtasks.go:37-87`) has no environment element and `renderTaskXML:95-143`
sets none, against `mcremote.user.service.tmpl:41-52` (HOME, USER, LOGNAME,
PATH, five XDG vars) and `plist_render.go:89-125` (the same dict).
`servicePathEnv` / `servicePathExtras` (`setup.go:869-887`) return
`~/.local/bin`, `~/.grok/bin`, `~/.opencode/bin`, `~/.cache/kilo/bin`,
`~/go/bin`, `~/.local/go/bin`, `~/.local/flutter/bin`, `/opt/homebrew/bin`,
`/usr/local/bin`, `/usr/bin`, `/bin` and join with `":"` — no Windows branch and
no `;` separator — and are never called from the schtasks path at all.

`Options.ExtraEnviron` is a documented, bound, repeatable flag
(`setup_service.go:92` `--env`, plumbed at `:48`; `relay/cli.go:430`), consumed
by the plist renderer (`plist_render.go:119`) and the systemd template
(`setup.go:808`, `:978-979`). A grep for `ExtraEnviron|Environ` across
`schtasks.go` and `setup_schtasks.go` returns **nothing**: on Windows `--env` is
accepted, validated, and discarded without a warning. Separately, a Windows task
inherits the environment Task Scheduler builds from the registry rather than the
interactive shell's, so a provider CLI made available by a profile script (nvm,
fnm, volta, scoop shims) is not on the daemon's PATH and there is no way to add
it.

**F10 — A console window appears at every logon.** No build path passes
`-H windowsgui`: `Makefile:43` is `GO_LDFLAGS := -s -w`,
`scripts/ci-windows-local.ps1:132` is `-s -w -X main.version=…`, and the
repo-wide grep for `windowsgui|ShowWindow|GetConsoleWindow|SW_HIDE` returns zero
hits. The task runs `LogonType=InteractiveToken` (`schtasks.go:107`) in the
user's interactive session, so a console-subsystem binary is given a visible
console window that persists for the daemon's lifetime. `Settings.Hidden`
(`schtasks.go:71`, set false at `:117`) does not help: it hides the *task* in the
Task Scheduler UI, not the window. The documented install therefore leaves a
stray console on every operator's desktop at every logon.

**F11 — The task principal is derived from two environment variables with no
fallback.** `currentTaskUser` (`schtasks.go:203-210`) returns
`USERDOMAIN\USERNAME`, or bare `USERNAME` if `USERDOMAIN` is empty, or `""` if
both are. That value is written into `Triggers.LogonTrigger.UserID` (`:103`) and
`Principals.Principal.UserID` (`:106`). An empty or unresolvable UserId makes
`schtasks /create` fail, surfacing as the raw schtasks message
(`setup_schtasks.go:46-48`). There is no `LookupAccountName`, no token-derived
SID, and no `whoami` fallback — even though `appdirs.currentUserSID()`
(`security_windows.go:36-47`) already does the correct thing in the same module
and is exported for exactly this kind of caller. For a Microsoft-account or
Azure AD principal, `USERNAME` is frequently not the form Task Scheduler wants.

**F12 — `isActiveWindows` swallows every error, and status parsing is
English-only.** `control_schtasks.go:9-15` returns `(false, nil)` for *any*
`schtasks /query` failure, so "not registered", "schtasks not on PATH" and "the
task store is broken" are indistinguishable. `taskStatusRunning` (`:23-32`)
matches the literal key `Status` and value `Running`; on a non-English Windows
both are localised and it returns false. The comment accepts that as
conservative and `TestTaskStatusRunning/localised` pins it — but the consequence
is downstream: `Lifecycle.WaitHealthy` (`updateclient/lifecycle.go:93-128`)
polls `Running` for 30s, never sees healthy, and reports
`"%s did not become healthy within %s"`. On a localised Windows an update
therefore fails at the health gate as well as at reconciliation (F1), and a
probe failure is reported as "not running" rather than as the error it is.

**F13 — Authenticode signing was decided and never wired, and the ops doc
claims otherwise.** MADR 0116 D14
(`docs/spec/0116-MADR-windows-and-linux-arm64-build-targets.md:917-918`) states
"Authenticode signing is designed in now and procured later. The build gains an
`MC_WINDOWS_SIGN_*` hook mirroring `MC_CODESIGN_IDENTITY`." Repo-wide,
`MC_WINDOWS_SIGN` appears **only in docs**. `Makefile:204-210` `codesign-maybe`
is gated `[ "$(GOOS)" = "darwin" ]`, and there is no `signtool` invocation
anywhere. Yet `docs/ops-windows-install.md:42` says, in the present tense,
"Signing is designed into the build (`MC_WINDOWS_SIGN_*`)". An operator reading
that will set a variable that does nothing. The hook was the deliverable D14
claimed; the certificate was the part explicitly deferred.

**F14 — The Windows acceptance gate stops before the service path.**
`scripts/acceptance-windows.ps1:74-176` asserts build, vet, test, cgo-free
binaries, `version`, `paths --json`, `pair create`, `pair list` and `doctor`
exit 0. `setup-service` and `serve` appear only as two "Manual steps this script
cannot assert" (`:181-186`), one of which merely asks whether an elevation
prompt appeared. F1, F2, F3, F7, F9, F10, F11 and F12 all live in that
untested gap. This is why a port that passes every automated gate on Windows can
still be unable to update itself.

**F15 — The core atomic-write tests do not run on Windows.**
`internal/fsutil/atomic_test.go:13` calls `testexec.SkipIfNoPOSIXModes(t)` and
`:129` calls `testexec.SkipIfNoUnlinkOpenFile(t)`; both gates
(`internal/testexec/testexec.go:67-74`, `:118-124`) skip unconditionally on
Windows. Measured: `--- SKIP: TestWriteFileAtomic`,
`--- SKIP: TestWriteFileAtomicInjectedFailures`. The gates are correctly
reasoned — POSIX mode bits and unlink-of-an-open-file genuinely do not exist
here — but the effect is that `WriteFileAtomic`'s contract is asserted on
Windows only by `TestWriteFileAtomicSurvivesARealHeldHandle` and the
rename-retry tests. The property that matters on Windows (the *ACL*, per
`appdirs.FileIsOwnerOnly`) is never asserted of an atomically-written file.

**Not findings, recorded because they were checked and are sound.**
`fsutil`'s Windows work is evidence-based and correct: `rename_windows.go:32-38`
matches `ERROR_ACCESS_DENIED` *and* `ERROR_SHARING_VIOLATION` on measured
behaviour (MADR 0153 F2), `rename_other.go:5-19` makes the POSIX retry a
compile-time no-op, and `syncdir_windows.go` documents its durability gap with
an upstream issue (golang/go#75541) that `docs/ops-windows-install.md` repeats.
`procutil.SuperviseStarted` has six real call sites and
`TestSuperviseStartedKillsTree` passes on this host. `launch_windows.go` routes
`.cmd` shims through `cmd.exe /c`, rejects cmd.exe metacharacters rather than
quoting them, and fixes the `Mode()&0o111` trap that broke `setup-service`
(MADR 0116 F23c). `appdirs/security_windows.go` compares ACE *sets* with alias
resolution rather than descriptor strings (MADR 0116 D23, 0155). Codex's default
transport is `stdio` (`internal/provider/codex/config.go:80`), so the Unix-only
`unix_ws` / `managed_daemon_proxy` refusals do not break a default Windows
config.

## Decision Drivers

* **D-a — An install path that cannot be updated is a liability, not a
  feature.** F1 means every Windows operator who follows the docs is pinned to
  the version they installed, and learns it only when an update rolls itself
  back. This is the single most severe finding.
* **D-b — Silence is worse than a missing feature.** F3 and F8 together mean a
  Windows daemon that misbehaves produces no evidence at all. Every other fix on
  this list is harder to verify without logs.
* **D-c — The product must not tell the operator to run commands that do not
  exist.** F2 is not cosmetic: `systemctl --user status mcremote` on Windows is
  a dead end that costs the operator the time the summary was supposed to save.
* **D-d — A test that cannot fail is not coverage.** F4's `t.Skip` and F15's
  unconditional gates both convert "unverified" into "green". MADR 0118 D2
  already established the house rule for exactly this: a blanket skip turns a
  broken environment into silent non-coverage.
* **D-e — Parity gaps must be either closed or named.** F7, F9 and F13 are all
  cases where a decision was recorded and the implementation stopped short.
  `docs/ops-windows-install.md` already models the right response — its "What is
  not supported on Windows" section exists so gaps "read as decisions rather
  than oversights". These three are not in it.
* **D-f — Windows stays Tier 2.** MADR 0116 set that tier deliberately and
  nothing here argues for changing it. The fixes should make Tier 2 honest, not
  promote it.
* **D-g — No elevation.** MADR 0116 D12's whole point is that install and run
  need no administrator. Any fix that reaches for `sc.exe`, a Windows Service,
  or an elevated `signtool` contradicts it.

## Considered Options

* **A — Finish the Task Scheduler path in place.** Implement `refreshSchtasks`,
  give the daemon its own log file, teach the printer the `windows-task` scope,
  repair `socketIdentity`, add an environment block, build with
  `-H windowsgui`, and extend the acceptance script over the service lifecycle.
* **B — Move Windows to a real Windows Service under the SCM.** Implement
  `StartServiceCtrlDispatcher`, gain `sc.exe`-managed restart-on-failure and
  boot-start, and get proper service logging through the Event Log.
* **C — Withdraw the Windows service path; document foreground-only operation.**
  Delete `setupSchtasks`, make `setup-service` refuse on Windows with a pointer
  to `mcremote serve`, and drop the claims the service path cannot currently
  support.

## Decision Outcome

Option A. The Task Scheduler path is already built, tested at the unit level,
unelevated, and documented; what is missing is the second half of its lifecycle,
and every item in it is local. B contradicts D-g and throws away working code to
solve problems A also solves. C is honest but regresses a shipped capability and
leaves Windows operators with no background operation at all.

### The decisions

* **D1 — Implement `setup-service --refresh` for the schtasks path.** Add
  `refreshSchtasks` beside `refreshSystemd` and `refreshLaunchd`, dispatch it
  from `RefreshUnit` (`refresh.go:117-124`), and make it recover options from
  the registered XML the way `recoverOptions` does from a unit. Until it exists,
  `update` on Windows must not silently roll back. Closes **F1**.
* **D2 — Teach `PrintSetupResult` the `windows-task` scope.** Add the arm to
  `result_print.go` with `schtasks /query|/run|/end`, `setup-service --remove`,
  the real log destination from D3, and `install.ps1` instead of `make install`.
  Never print `systemctl`, `journalctl`, `loginctl` or `.service` on Windows.
  Closes **F2**.
* **D3 — Give the Windows daemon a real log destination, and stop advertising
  one it does not write.** Implement MADR 0157's daemon-owned rolling log for
  the Windows service path, writing under the `LogDir` that `paths` already
  reports. If 0157 is not executed first, then `roots_windows.go:64` must stop
  setting `Logs` until something writes to it, and the
  `docs/ops-windows-install.md` table row must go with it. Closes **F3**, and
  makes **F8** observable.
* **D4 — Fix `socketIdentity` on Windows and make its test assert.** Derive the
  identity from `CreateFile` + `GetFileInformationByHandle` (probe 2 measured
  this works and yields a stable non-zero index) rather than from `fi.Sys()`,
  and change `owner_windows_test.go:57-59` so a not-ok result **fails** instead
  of skipping. Closes **F4**; restores the guard **F5** currently lacks.
* **D5 — Close the restart gap as far as Task Scheduler allows, and name the
  residual.** Raise `RestartOnFailure` `Count` to match the Unix retry posture,
  and record in `docs/ops-windows-install.md` that a clean exit (code 0) is not
  restarted on Windows because the task engine offers no `Restart=always`. If
  that residual is unacceptable, the alternative is a watchdog action in the
  task itself — decide it explicitly rather than leaving the comment at
  `schtasks.go:121` to imply parity. Closes **F7**.
* **D6 — Put an environment block in the task XML, with a Windows
  `servicePathExtras`.** Add the element to `taskDefinition`, emit
  `ExtraEnviron` so `--env` stops being silently dropped, and give
  `servicePathExtras` a Windows branch returning the real per-user install roots
  (`%LOCALAPPDATA%\Programs\…`, npm global, scoop shims) joined with `;`.
  Closes **F9**.
* **D7 — Build the Windows binaries with `-H windowsgui`.** Add it to
  `GO_LDFLAGS` for `GOOS=windows` in `Makefile` and to the release and
  `ci-windows-local.ps1` link flags, so the at-logon task produces no console
  window. Verify the CLI still behaves when run interactively — a GUI-subsystem
  binary attached to a console needs its stdio handles checked. Closes **F10**.
* **D8 — Derive the task principal from the process token.** Replace
  `currentTaskUser`'s `USERDOMAIN\USERNAME` read with a token-derived account
  name, reusing `appdirs.CurrentUserSID()` and `SID.LookupAccount`, keeping the
  env read only as a fallback. Closes **F11**.
* **D9 — Make Windows status probing fail loudly and parse locale-independently.**
  `isActiveWindows` must distinguish "not registered" from "the probe failed",
  and `taskStatusRunning` must stop depending on English text — query a
  machine-readable form (`schtasks /query /xml`, or the task's state field)
  rather than `/fo LIST /v`. Closes **F12**.
* **D10 — Either wire `MC_WINDOWS_SIGN_*` or correct the documents that claim
  it.** MADR 0116 D14 promised the hook; if it is still wanted, add it beside
  `codesign-maybe`. If it is not, amend `docs/ops-windows-install.md:42` and
  record the reversal against 0116 rather than leaving a present-tense claim
  about absent code. Closes **F13**.
* **D11 — Extend `scripts/acceptance-windows.ps1` over the service lifecycle.**
  Assert `setup-service --force` from a non-administrator shell, `--refresh`,
  `--print-only` scope text, a daemon start that produces a non-empty log file,
  an admin-socket round trip, a stop that leaves no socket file, and a second
  start that recovers from one deliberately left behind. Closes **F14**, and is
  what keeps D1–D9 from regressing.
* **D12 — Replace the POSIX-mode assertions in the atomic-write tests with
  platform-correct ones.** Where `SkipIfNoPOSIXModes` currently skips the whole
  test, assert the Windows equivalent (`appdirs.FileIsOwnerOnly`) so
  `WriteFileAtomic`'s contract is verified here too. Closes **F15**.

### Consequences

* Windows operators gain a working `update`, readable logs, correct post-install
  guidance, no stray console window, and a `--env` flag that does something.
* `refreshSchtasks` (D1) is the largest single item: it needs an XML recovery
  path analogous to `recoverOptions`/`recoverPlistOptions`, and `normalizeTaskXML`
  (`setup_schtasks.go:87-119`) already exists to compare definitions.
* D7 is the only decision with a plausible regression surface. A
  GUI-subsystem binary gets no console by default; if any interactive Windows
  path depends on console attachment (a TTY prompt, the `picker`, the
  `providerauth` flow) it must be checked, not assumed. This is the decision
  most likely to be reverted under pressure, because the fix is one ldflag and
  the breakage would be subtle.
* D3 depends on MADR 0157, which is `proposed` and unexecuted. If 0157 slips,
  D3's second arm (stop advertising `LogDir`) must still ship — an advertised
  log directory that is never written is worse than none, because it sends the
  operator looking in the right place for nothing.
* D5 cannot reach full parity. Task Scheduler has no `Restart=always`; the
  residual must be documented rather than papered over, or it will be
  rediscovered as a bug report.
* D11 lengthens the Windows acceptance run and makes it stateful — it registers
  and removes a real task. It must clean up after itself and must not run
  elevated, or it stops testing the documented path.
* None of these decisions promote Windows to Tier 1, add `windows/arm64`, or
  introduce SCM support. Those remain 0116's decisions.

### Confirmation

```powershell
# 1. The tree still compiles and vets for Windows.
$env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go vet ./...                                    # expect: exit 0, no output

# 2. D4 — socketIdentity no longer skips, and now asserts.
go test ./internal/admin/ -run TestSocketIdentityStable -v
#   expect: --- PASS (not --- SKIP)

# 3. D12 — the atomic-write contract is verified on Windows.
go test ./internal/fsutil/ -run 'TestWriteFileAtomic' -v
#   expect: --- PASS for TestWriteFileAtomic (not --- SKIP)

# 4. D1 — refresh is supported on Windows.
go run ./cmd/mcremote setup-service --refresh --print-only
#   expect: a verdict line, NOT "only supported on Linux and macOS"

# 5. D2 — the post-install summary names no Unix command.
#   from a non-administrator shell, after `setup-service --force`:
#   expect: no occurrence of systemctl, journalctl, loginctl, make install,
#           or a ".service" unit name; schtasks commands present.

# 6. D3 — the daemon writes where `paths` says it does.
mcremote paths --json | Select-String log_dir
#   then start the task and expect that directory to exist and grow.

# 7. D7 — no console window at logon.
#   register the task, log off and on: expect no mcremote console window.
go version -m .\dist\mcremote-windows-amd64.exe | Select-String 'windowsgui'
#   expect: -H windowsgui present in the ldflags

# 8. D11 — the extended acceptance gate.
.\scripts\acceptance-windows.ps1
#   expect: 0 CHECK(S) FAILED, with service-lifecycle checks now listed

# 9. D10 — no document claims an unwired hook.
Select-String -Path docs\*.md,docs\spec\*.md -Pattern 'MC_WINDOWS_SIGN'
#   expect: hits only where the hook exists, or an explicit amendment
```

## Pros and Cons of the Options

### A — Finish the Task Scheduler path in place (chosen)

* Good, because every finding is local to code that already exists and already
  has a Windows branch: `refresh.go` needs a third arm, `result_print.go` a
  third case, `schtasks.go` an environment element, `owner_windows.go` a
  different API call. No new subsystem, no new dependency.
* Good, because it preserves MADR 0116 D12's central property — install and run
  with no elevation. A per-user at-logon task needs no administrator, and
  `scripts/acceptance-windows.ps1:184-186` already tests for exactly that.
* Good, because the unit-level scaffolding is real and passing:
  `TestSetupSchtasksIdempotent`, `TestSetupSchtasksRegistersWhenChanged`,
  `TestRemoveSchtasksIdempotent`, `TestEncodeTaskXMLIsUTF16LEWithBOM` and
  `TestTaskXMLParsesInRealSchtasks` (which drives the actual `schtasks.exe` on
  this host) all pass. The XML is correct; the lifecycle around it is not.
* Good, because `schtasks.go` is deliberately not build-tagged (`:3-8`), so the
  whole Windows branch stays testable from a Unix host via `OverrideInstallOS`.
  D1 inherits that property.
* Bad, because Task Scheduler cannot express `Restart=always` (F7/D5), cannot
  redirect stdio (F3, so D3 must move logging into the daemon), and cannot stop
  a task gracefully (F8). Option A closes these by working around the engine or
  by documenting the residual — it never fully reaches Unix parity.
* Bad, because the task starts at logon, not at boot, so a headless Windows
  host still cannot run `mcrelay` unattended. That is documented in
  `ops-windows-install.md` and is out of scope here.

### B — Move Windows to a real Windows Service under the SCM

* Good, because it would genuinely fix F7 and F8: the SCM restarts on failure
  *and* on success, and `SERVICE_CONTROL_STOP` is a real graceful stop with a
  configurable timeout — the true analogue of SIGTERM plus `TimeoutStopSec`.
* Good, because it starts at boot, which would make headless `mcrelay` on
  Windows viable for the first time.
* Good, because the Event Log is a first-class log sink, which would address F3
  without the daemon owning rotation.
* Bad, because it contradicts D-g and MADR 0116 D12 outright: `sc.exe create`
  requires elevation, and `docs/ops-windows-install.md` already states these
  binaries "do not call `StartServiceCtrlDispatcher`, so the Service Control
  Manager kills them at the start-up timeout. Running them under the SCM is
  unsupported." Reversing that is a new decision, not a bug fix.
* Bad, because it discards working, tested code — the whole schtasks renderer,
  its UTF-16 encoding fix (MADR 0116 F24), and six passing tests — to solve
  problems option A also solves.
* Bad, because a Windows Service runs in session 0 with no interactive desktop,
  which breaks every provider CLI that needs a browser for device auth. That is
  a larger regression than any finding on this list.

### C — Withdraw the Windows service path; document foreground-only operation

* Good, because it is immediately honest. F1, F2, F7, F9, F10, F11 and F12 all
  disappear at once, and no operator is misled by a `systemctl` instruction or
  an update that rolls itself back.
* Good, because it is cheap: one refusal in `Setup` and a documentation pass.
* Good, because it would be the right answer if the remaining gaps were
  unbounded. They are not — every one has a named, local fix.
* Bad, because it regresses a shipped, documented, CI-tested capability.
  `docs/ops-windows-install.md` has a whole "Running in the background" section,
  and `install.ps1` installs binaries whose purpose includes background
  operation.
* Bad, because background operation is the product. A remote-control daemon an
  operator must keep a terminal open for does not survive a logoff, which is the
  entire reason MADR 0116 D12 chose an at-logon task.
* Bad, because it would leave F3, F4 and F15 standing anyway: the log
  destination, the socket identity and the skipped tests are not properties of
  the service path.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Tree compiles and vets for windows/amd64 | measured: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...` → `VET_EXIT=0` |
| Host identity | measured: Windows 11 Home, build 26200 (10.0.26200), go1.26.6 windows/amd64, `MAC420\macsm`, unprivileged shell |
| `RefreshUnit` supports only linux/darwin | `internal/cli/service/refresh.go:117-124` |
| `--refresh` returns the error to the child's exit code | `internal/cli/setup_service.go:103-108`; `cmd/mcremote/main.go:29-34` |
| Non-zero refresh child becomes an error | `internal/cli/service/exec_refresher.go:61-69` |
| Reconcile error is fatal | `internal/updateclient/lifecycle.go:143-156` (comment + code) |
| Reconcile error triggers rollback | `mcplib@v1.4.1/selfupdate/managed.go:86-88` |
| Windows task counts as installed | `internal/cli/service/control_schtasks.go:34-40` |
| `PrintSetupResult` has no windows arm | `internal/cli/service/result_print.go:48-57`, `:75-135` |
| Windows scope value | `internal/cli/service/setup_schtasks.go:20`; `internal/cli/service/setup.go:314` |
| mcrelay knows the scope but calls the shared printer first | `internal/relay/cli.go:480`, `:492-501` |
| Windows `Logs` root is set | `internal/appdirs/roots_windows.go:64` |
| `LogDir` derived from it and printed | `internal/appdirs/paths.go:63-71`; `internal/cli/paths.go:47`, `:68`, `:91-92` |
| Logger defaults to stderr; serve passes no `Out` | `internal/logging/slog.go:20-24`; `internal/cli/serve.go:80-83` |
| Task XML has no log or env element | `internal/cli/service/schtasks.go:37-87` |
| Only launchd creates a log dir / names log files | `internal/cli/service/setup.go:431-435`; `internal/cli/service/plist_render.go:177-178` |
| systemd logs to journald | `internal/cli/service/mcremote.user.service.tmpl:55-57` |
| No `--log-file` flag exists | measured: repo-wide grep `log-file\|logFile\|LogFile\|log_file` in `internal/cli` → zero hits |
| MADR 0157 is unexecuted | `docs/spec/0157-MADR-daemon-owned-rolling-logs.md` frontmatter `status: proposed` |
| `socketIdentity` asserts the wrong type | `internal/admin/owner_windows.go:41-47` |
| `fi.Sys()` is `*syscall.Win32FileAttributeData` | measured, probe 1 |
| `Win32FileAttributeData` has no file index | measured, probe 2 (`attrs=0x420`, no FileIndex field) |
| A real index needs `CreateFile`+`GetFileInformationByHandle` | measured, probe 2 (`identity=4222124651638846`) |
| Shutdown removal is gated on the dead identity | `internal/admin/admin.go:129-134`, `:146-150` |
| The Windows identity test skips instead of failing | `internal/admin/owner_windows_test.go:57-59`; measured `--- SKIP: TestSocketIdentityStable` |
| `ModeSocket` *is* set on Windows (hypothesis closed) | measured, probe 1 (`Mode()=Srw-rw-rw-`, ModeSocket true, ModeIrregular false) |
| The socket file is a reparse point | measured, probe 2 (`attrs=0x420`) |
| A hard kill leaves the socket file behind | measured, probe 3 (`POST-KILL socket file SURVIVES: mode=Srw-rw-rw-`) |
| Stale-socket recovery works via the failed ping | measured, probe 3 (dial refused → `os.Remove` OK → rebind `err=<nil>`); `internal/admin/admin.go:109-114` |
| A graceful close deletes the file, so `os.Remove` then fails | measured, probe 2 (`The system cannot find the file specified.`) |
| `schtasks /end` is a TerminateProcess | `internal/cli/service/control_schtasks.go:51-57` |
| Windows shutdown signals are `os.Interrupt` only | `internal/cli/signals_windows.go:15-17`; `internal/relay/signals_windows.go:15-17` |
| Windows restart policy | `internal/cli/service/schtasks.go:118-124` |
| systemd restart policy | `internal/cli/service/mcremote.user.service.tmpl:17-18`, `:28-32` |
| launchd restart policy | `internal/cli/service/plist_render.go:169-172` |
| systemd/launchd environment blocks | `mcremote.user.service.tmpl:41-52`; `plist_render.go:89-125` |
| `servicePathExtras` is POSIX-only and `:`-joined | `internal/cli/service/setup.go:869-887` |
| `--env` is bound and plumbed | `internal/cli/setup_service.go:29`, `:48`, `:92`; `internal/relay/cli.go:430` |
| `--env` is consumed by plist and systemd | `plist_render.go:119`; `setup.go:808`, `:978-979` |
| `--env` is dropped by schtasks | measured: grep `ExtraEnviron\|Environ` in `schtasks.go` + `setup_schtasks.go` → zero hits |
| No `-H windowsgui` anywhere | measured: repo-wide grep `windowsgui\|ShowWindow\|GetConsoleWindow\|SW_HIDE\|CREATE_NO_WINDOW\|DETACHED_PROCESS` → zero hits |
| Release/local link flags | `Makefile:43`; `scripts/ci-windows-local.ps1:132` |
| Task runs in the interactive session | `internal/cli/service/schtasks.go:105-108` |
| `Hidden` is false, and hides the task not the window | `internal/cli/service/schtasks.go:71`, `:117` |
| Principal comes from two env vars | `internal/cli/service/schtasks.go:202-210`, used at `:103`, `:106` |
| A correct token-based SID helper already exists | `internal/appdirs/security_windows.go:36-47`, exported at `:328` |
| `isActiveWindows` swallows errors | `internal/cli/service/control_schtasks.go:9-15` |
| Status parsing matches English literals | `internal/cli/service/control_schtasks.go:23-32` |
| `WaitHealthy` polls `Running` for 30s | `internal/updateclient/lifecycle.go:93-128`, `:15-20` |
| D14 promised `MC_WINDOWS_SIGN_*` | `docs/spec/0116-MADR-windows-and-linux-arm64-build-targets.md:917-918` |
| The hook does not exist | measured: repo-wide grep `MC_WINDOWS_SIGN\|signtool` → hits only in `docs/` |
| `codesign-maybe` is darwin-gated | `Makefile:204-210` |
| Ops doc claims signing is wired | `docs/ops-windows-install.md:40-43` |
| Acceptance script coverage | `scripts/acceptance-windows.ps1:74-176`, manual steps `:181-186` |
| Atomic-write tests skip on Windows | `internal/fsutil/atomic_test.go:13`, `:129`; `internal/testexec/testexec.go:67-74`, `:118-124`; measured `--- SKIP` ×2 |
| `SuperviseStarted` is genuinely wired (not a finding) | measured: six call sites; `--- PASS: TestSuperviseStartedKillsTree` |
| Codex default transport is stdio (not a finding) | `internal/provider/codex/config.go:80`; `configs/config.example.yaml:266` |

### Related records

* **MADR 0116** — `windows-and-linux-arm64-build-targets`. The port itself.
  D3 (Known Folders), D4 (private DACL), D5 (syncDir no-op), D6 (LockFileEx),
  D7 (admin socket), D8 (job object), D9/D12 (Task Scheduler, `/end` is a
  TerminateProcess), D14 (signing hook — **F13 shows it was never wired**),
  D15 (the "not supported" list), D16 (testexec gates), D22/D23 (ACL
  predicates). Most of what is *right* about the Windows port is here.
* **MADR 0145** — `local-windows-ci-style-tests`. Created
  `scripts/acceptance-windows.ps1` and `make ci-windows`. **F14** is about what
  that script does not cover.
* **MADR 0150** — job-object supervision. Its D1/D3/D4 and F1/F2/F8 are why
  `SuperviseStarted` has callers today. Verified present; not a finding.
* **MADR 0153** — atomic rename retry on Windows. The evidence-based
  `ERROR_ACCESS_DENIED` predicate. Sound; cited as the model F4 should follow.
* **MADR 0155** — owner-only config guards, `foreignTrustees`, the `LA` alias
  fix. Sound.
* **MADR 0157** — `daemon-owned-rolling-logs`, `status: proposed`. **D3 depends
  on it.** Its own text already contains a "Rotation under a Windows tail
  (measured 3/3 on this host)" section, so the Windows behaviour was probed
  while designing it.
* **MADR 0160** — `remove-goose-cli-support`, `proposed` (cross-reference added
  2026-09-18; no finding or decision here changes). It deletes
  `internal/provider/acphttp`, so of the six `SuperviseStarted` call sites
  recorded above, `acphttp/provider.go:361` goes and five remain; nothing in
  this record depends on that site. 0160 also removes the `goose:` block from
  `defaults_mcremote.yaml` and treats a leftover block as a warning, not a load
  failure: a refusal would have made the managed `update` path this record's F1
  describes fail on every platform, not only Windows (0160 F16).
* **MADR 0118** — symlink privilege as a machine property, and D2's rule that a
  blanket skip converts a broken environment into silent non-coverage. That rule
  is what makes **F4**'s `t.Skip` and **F15** defects rather than pragmatism.
* `docs/ops-windows-install.md` — the operator-facing page. Already documents
  the syncDir gap, `/end` semantics, at-logon-not-boot, the npm `.cmd` shim, and
  the "not supported" list. **F3** (the Logs table row) and **F13** (the signing
  claim) are statements in it that the code does not support.

### Open questions for the plan

1. **Does D3 wait for MADR 0157, or ship the smaller half first?** Stopping
   `roots_windows.go:64` from advertising a `Logs` root is a two-line change
   that removes a false claim today; implementing the rolling log is 0157's
   whole scope. The plan should probably sequence the correction first and the
   implementation second, but that is a phasing decision.
2. **What exactly does `-H windowsgui` break?** D7's risk is that a
   GUI-subsystem binary gets no console, and some interactive Windows flows (the
   `picker`, `providerauth` device-code prompts, `pair`) may depend on console
   attachment or on a TTY check. This needs a probe on a real host before the
   flag is added, not after.
3. **Is there a machine-readable task state that survives localisation?** D9
   assumes `schtasks /query /xml` or an equivalent exposes state without English
   text. That must be measured on a non-English Windows, or on this host with a
   forced UI culture, before the parser is rewritten.
4. **Which per-user install roots belong in a Windows `servicePathExtras`?**
   D6 names `%LOCALAPPDATA%\Programs`, npm global and scoop shims, but the real
   list should come from where the provider CLIs actually land on a Windows
   install — including `fnm`/`volta`/`nvm-windows`, which relocate `node` per
   shell.
5. **Should `RestartOnFailure` `Count` be raised, or is a watchdog action the
   honest answer?** D5 leaves this open. A higher count still never restarts a
   clean exit; a second task action that relaunches on exit would, at the cost of
   complexity in the XML.
6. **Does D11's stateful acceptance run belong in `acceptance-windows.ps1` or in
   a separate script?** Registering and removing a real scheduled task is
   side-effecting in a way the current script is not, and it must clean up after
   a failure.
7. **Is F13 a code fix or a documentation fix?** D10 allows either. If the
   certificate is still not procured, wiring a hook that nothing can call has
   its own cost; amending 0116 and the ops page may be the better half.
