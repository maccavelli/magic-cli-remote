---
status: accepted
date: 2026-09-19
decision-makers: Project Owner
consulted: none
informed: none
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The Windows service path is half-wired: it installs a daemon it cannot refresh, cannot log, and mis-describes

## Context and Problem Statement

MADR 0116 made `windows/amd64` a shipped, CI-tested target and MADR 0145 gave
it a local gate. Both succeeded at the level they aimed at: the tree compiles,
`go vet` is clean, and the unit suite passes. (The 2026-09-18 draft also said
the acceptance script "runs green on a real Windows host". It does not: it has
not parsed since it was added — see F16.) Measured again here — `GOOS=windows GOARCH=amd64
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

**Revision 2026-09-19 (still `proposed`).** A second pass re-checked every
citation against `38078c5` and ran the probes the draft's open questions asked
for. The details are in "Re-measured 2026-09-19". Most citations held. These
did not:

* **F7 was wrong.** Task Scheduler's `RestartOnFailure` does not restart a task
  whose program exits non-zero: measured 0 restarts in 4 min 42 s. On Windows,
  a crashed daemon is never restarted.
* **D6 as written is impossible.** `schtasks /create` rejects an environment
  element ("The task XML contains an unexpected node").
* **D7 as written breaks the CLI.** A `-H windowsgui` binary loses its exit
  code and its redirected output in PowerShell, the operator's shell.
* **F11's Microsoft-account premise is contradicted.** This host's account is a
  Microsoft account, and the environment-derived name works. The real defect is
  that the name comes from overridable environment variables.
* **F3's path is wrong.** The advertised `log_dir` is `…\Logs\mcremote`, not
  `…\Logs`.
* **The acceptance script has never parsed** (F16).
* **`--unit-name` is a second flag silently dropped on Windows** (F17).

D3 now defers to MADR 0157, which already plans the Windows log sink.
D5–D11 are rewritten on measured ground. D13 is new. The open questions are
answered.

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

`TestSystemPaths`, `TestDefaultConfigFile` and `TestSystemRootsDispatches` skip
with a legitimate reason (the Known Folder lookup is covered by
`roots_windows_test.go`'s seam). The other three do not, and are findings (F4,
F15).

**Static measurements.** Repo-wide greps, each reproduced in the Evidence index:

* `windowsgui|ShowWindow|GetConsoleWindow|SW_HIDE|CREATE_NO_WINDOW|DETACHED_PROCESS`
  → **zero hits** in any `.go`, `Makefile`, `.mk`, `.ps1`, `.sh` or `.yml`.
* `MC_WINDOWS_SIGN|signtool` → hits **only** in `docs/`. (Adding
  `Authenticode` also matches prose in `README.md:154` and
  `scripts/install.ps1:286`: "not Authenticode-signed yet".)
* `ExtraEnviron|Environ` in `schtasks.go` and `setup_schtasks.go` → **zero hits**.
* `SuperviseStarted` → six production call sites at `b3d3355`, **five** at
  `38078c5` after MADR 0160 deleted `acphttp` (`acpagent.go:470`,
  `acpagent/terminal.go:119`, `codex/provider.go:590`,
  `httpagent/provider.go:535`, `providerauth/cli.go:129`), so MADR 0150's
  tree-kill guarantee is genuinely wired. This is not a finding; it is recorded
  because it was checked.

### Re-measured 2026-09-19

All on this host at `38078c5` (Windows 11 Home build 26200, go1.26.6,
`MAC420\macsm`, unprivileged). The probes ran as throwaway per-user scheduled
tasks named `mcr-probe-*`. Their XML reused the live `mcremote` task's
principal (`InteractiveToken`, the user's SID) and settings. All of them, and
every process they started, were deleted afterwards; the live `mcremote` task
stayed `Running` throughout.

**Citations.** Every `file:line` in this record was re-checked at `38078c5`.
All held except these:

* the mcplib rollback call is at `managed.go:89-90`, not `:86-88`;
* `normalizeTaskXML` is at `setup_schtasks.go:93-120`;
* `servicePathExtras` spans `setup.go:887-910`;
* `configs/config.example.yaml:225`, not `:266`, after MADR 0160;
* `SuperviseStarted` now has **five** production call sites, because MADR 0160
  deleted `acphttp`.

**Probe 4 — F1 on the real binaries.** `mcremote setup-service --refresh` exits
**1** with "setup-service --refresh is only supported on Linux and macOS
(running on windows)". This holds for the installed v0.17.4 (`788ab6b`) and for
a build of `38078c5`, with and without `--print-only`. mcplib `v1.4.1`
`managed.go:64-133` confirms the rest of the chain. `Install` runs stop, apply,
`Reconcile`, `Start`, `WaitHealthy`, then commit. Any error after apply calls
`recover`, which runs `Restore` (only when the receipt says `Changed`), rolls
back the binary, and restarts.

**Probe 5 — the live task.** `Get-ScheduledTask mcremote` reports `State`
`Running` (enum value **4**), `Principal.UserId macsm`, `LogonType
Interactive`, and `RestartInterval PT1M`, `RestartCount 3`. The registered XML
(`schtasks /query /tn mcremote /xml ONE`) stores the principal as the SID
`S-1-5-21-…-1001`, and carries `Description` "mcremote background service
(magic-cli-remote)".

**Probe 6 — what "a console window at logon" is on this host.** The daemon (pid
15768, parent `svchost` 4328) has `MainWindowHandle 0`. It owns a zero-size
`PseudoConsoleWindow` and a `conhost` child with no window of its own. Windows
Terminal (pid 8636) started at 19:16:41, **one second** after the daemon
(19:16:40). A console-subsystem probe run as a task reproduced this: a new
`OpenConsole` host appeared within 111 ms, started by the same `svchost` (pid
2580) that hosts the daemon's, and the probe's console handle was non-zero. So
on Windows 11 the console is handed to Windows Terminal. Whether a tab is still
visible on the operator's screen is **[unverified]**, because it can only be
seen, not measured, from here.

**Probe 7 — ways to hide the console.**

| Variant | Console host created? | Exit code reaches Task Scheduler? | `schtasks /end` kills the program? | CLI in PowerShell |
| --- | --- | --- | --- | --- |
| Console binary, as today | yes, `OpenConsole` at about 110 ms | yes | yes | works |
| `-H windowsgui` binary | no; `GetConsoleWindow`=0 | yes, 1 gives 0x1 | yes | **broken**: `> file` is empty, a failing command reports exit **0** (the shell does not wait) |
| `conhost.exe --headless <exe>` | no | **no**: exit 1 gives 0x0 | **no**: conhost dies, the program is **orphaned** | n/a |
| Console binary calling `FreeConsole()` first | none seen, sampled every 50 ms for 3 s | yes | yes | works; the CLI never calls it |

Git Bash does wait for a GUI-subsystem binary (exit 1 preserved). PowerShell 7
is the operator's shell on this host, and `install.ps1` runs in PowerShell.

**Probe 8 — restart semantics.** A task with `RestartOnFailure` `PT1M`×3 whose
program exits **1** ran **once** in 4 min 42 s. One exiting 0 ran once. One
ended with `/end` (result `0x41306`) ran once. A GUI-subsystem program exiting 1
ran once in 1 min 43 s.

A repeating trigger does work. A `TimeTrigger` with `Repetition` `PT1M` and
`MultipleInstancesPolicy IgnoreNew`:

* relaunched a program that exits at once, at 07:52:14, 07:53:15 and 07:54:15;
* **did not** start a second instance of one still running (1 run and 1 process
  across 4½ minutes);
* stopped relaunching while the task was disabled, with 0 new runs in 150 s.

`schtasks /change /disable` and `/enable` both succeed **unelevated** on your
own task. Task Scheduler accepts a `LogonTrigger` and a repeating `TimeTrigger`
together on one task.

**Probe 9 — environment.** A task created with an `<Environment>` element
inside `<Exec>` is rejected: "The task XML contains an unexpected node.
(5,239):Environment". A task-launched program gets 54 variables, including the
full registry **Machine+User `Path`** (npm `…\Roaming\npm`, `~\.grok\bin`, the
Codex bin, `%LOCALAPPDATA%\Programs\…`), `USERPROFILE`, `APPDATA`,
`LOCALAPPDATA`, `USERDOMAIN` and `USERNAME`. There is no `HOME`. On this host
every provider CLI's directory is already on that `Path`. No nvm, fnm, volta or
scoop is installed here, so the draft's profile-script concern is
**[unverified]**.

**Probe 10 — dropped flags and the principal.**

* `setup-service --print-only` renders byte-identical XML with and without
  `--unit-name mcr-accept --env MCREMOTE_LOG_LEVEL=debug`.
* With `USERNAME=bogus USERDOMAIN=` it renders `<UserId>bogus</UserId>`. With
  both empty it renders `<UserId></UserId>`.
* The token and the SID resolve to `MAC420\macsm`, identical to the environment
  form. This account's `PrincipalSource` is **MicrosoftAccount**.

**Probe 11 — a status probe that does not depend on the UI language.**
`powershell -NoProfile -NonInteractive -Command "(Get-ScheduledTask -TaskName
mcremote -ErrorAction SilentlyContinue)"`:

* returns the numeric state **4** for the live task, and `$null` (printed
  `absent`) for a missing one;
* takes 1261–2375 ms per call (3 samples), against 53 ms for `schtasks /query
  /v`.

`schtasks` output under a non-English UI **could not be measured**: this host
has only the English UI. The localisation claim in F12 therefore stays
**[unverified]**.

**Probe 12 — the acceptance script.**

* `scripts/acceptance-windows.ps1` fails to parse under both PowerShell 5.1 and
  7, at line 65: `throw "$Label: got …"`, where `$Label:` reads as a
  scope-qualified variable. The line has been there since `ca436bb`
  (2026-09-06, MADR 0145, the script's introduction), so the script has never
  run.
* With only that line fixed (`${Label}`), in a scratch worktree, it runs to
  completion with **2 failures**:
  * `paths --json (C1)`: "log_dir: got '…\Local\mcremote\Logs\mcremote' want
    '…\Local\mcremote\Logs'";
  * its `go test ./...`: `TestProviderInitializesBeforeTimingOut`,
    `TestHandleTunnelRejections` and `TestBridgeFrameLimitFollowsConfig`, all
    context deadline exceeded. Each of those passed 3/3 run alone, and the full
    suite passed twice in the main tree that day. They are load-sensitive
    timing, observed once and unexplained, and not a finding here.
* `mcrelay setup-service --print-only` renders a valid task, and no `mcrelay`
  task is registered on this host.

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
5. `mcplib@v1.4.1/selfupdate/managed.go:89-90` — `if recErr != nil { return
   InstallResult{}, s.recover(ctx, product, applied, receipt, true, recErr) }`.
   Measured: the refresh child exits 1 on the installed v0.17.4 and on HEAD
   (probe 4).

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
non-empty `roots.Logs` into `p.LogDir`, **appending the product name**
(`paths.go:70`); `internal/cli/paths.go:91-92` prints `log_dir:`. So
`mcremote paths` on Windows reports `%LocalAppData%\mcremote\Logs\mcremote`.
`docs/ops-windows-install.md:58` lists `%LocalAppData%\mcremote\Logs` in the
"Where things live" table, as though it were populated, and
`scripts/acceptance-windows.ps1:142-143` asserts that same path. The code, the
document and the gate all disagree (probe 12; MADR 0157 F4 records the same
mismatch).

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

**F7 — A crashed Windows daemon is never restarted.** `schtasks.go:121-124`
sets `RestartOnFailure` `Interval=PT1M`, `Count=3`, under the comment "the
closest analogue to the systemd unit's Restart=". The Unix paths are
unconditional: `mcremote.user.service.tmpl:28-31` sets `Restart=always` with
`RestartSec=5` and a generous `StartLimitIntervalSec=300` /
`StartLimitBurst=30`, and `plist_render.go:169-171` sets `RunAtLoad` plus
`KeepAlive` true with `ThrottleInterval` 2.

The 2026-09-18 draft said `RestartOnFailure` fires when the action returns
non-zero. **Measured, it does not** (probe 8). A program exiting 1 under exactly
this setting ran once in 4 min 42 s, and one exiting 0 or killed by `/end` also
ran once. (That fits `RestartOnFailure` covering a task that fails to launch
**[inferred; launch failure was not probed]**.) So a Windows daemon that
crashes, exits on a config error, or exits cleanly stays down until the next
logon.

What does work, measured: a repeating `TimeTrigger` with `IgnoreNew`
relaunches within one interval (1 minute minimum), never duplicates a running
instance, and is silenced by an unelevated `/change /disable`.

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

**F9 — `--env` is silently dropped on Windows, and the task format cannot
carry environment at all.** `taskDefinition` (`schtasks.go:37-87`) has no
environment element and `renderTaskXML:95-143` sets none, against
`mcremote.user.service.tmpl:41-52` (HOME, USER, LOGNAME, PATH, the XDG vars)
and `plist_render.go:89-125` (the same dict). This is not an omission that one
element would fix: Task Scheduler **rejects** an `<Environment>` node in an
`Exec` action (probe 9).

`servicePathEnv` / `servicePathExtras` (`setup.go:869-881`, `887-910`) are
POSIX-only, `:`-joined, and never called from the schtasks path. For mcremote
they return `~/.local/bin`, `~/.grok/bin`, `~/.opencode/bin`,
`~/.cache/kilo/bin`, `~/go/bin`, `~/.local/go/bin`, `~/.local/flutter/bin`,
`/opt/homebrew/bin` and `/usr/local/bin`; `/usr/bin` and `/bin` belong to the
mcrelay branch. **No Windows equivalent is needed on this evidence.** A task
inherits the registry Machine+User `Path`, which here already contains every
provider CLI's directory (probe 9).

`Options.ExtraEnviron` is a documented, bound, repeatable flag
(`setup_service.go:92` `--env`, plumbed at `:48`; `relay/cli.go:430`), consumed
by the plist renderer (`plist_render.go:119`) and the systemd template
(`setup.go:808`, `:978-979`). A grep for `ExtraEnviron|Environ` across
`schtasks.go` and `setup_schtasks.go` returns **nothing**: on Windows `--env` is
accepted, validated, and discarded without a warning (measured, probe 10).
Separately, a task inherits the environment Task Scheduler builds from the
registry, not the interactive shell's (measured, probe 9). A provider CLI that
only a profile script puts on `PATH` would therefore be invisible to the
daemon. None exists on this host, so that case is **[unverified]**.

**F10 — A console appears at every logon.** No build path passes
`-H windowsgui`: `Makefile:43` is `GO_LDFLAGS := -s -w`,
`scripts/ci-windows-local.ps1:132` is `-s -w -X main.version=…`, and the
repo-wide grep for `windowsgui|ShowWindow|GetConsoleWindow|SW_HIDE` returns zero
hits. The task runs `LogonType=InteractiveToken` (`schtasks.go:107`) in the
user's interactive session, so a console-subsystem binary is given a visible
console window that persists for the daemon's lifetime. `Settings.Hidden`
(`schtasks.go:71`, set false at `:117`) does not help: it hides the *task* in the
Task Scheduler UI, not the window. **Measured mechanism on Windows 11 (probe
6):** the console is handed to Windows Terminal. Windows Terminal started one
second after the daemon at logon, and a console-subsystem task probe created a
new `OpenConsole` host within about 110 ms. So the documented install opens a
terminal window or tab hosting `mcremote.exe` at every logon; whether it is
still visible at a given moment is **[unverified]**. The fix cannot be
`-H windowsgui` on the CLI binary (probe 7, D7).

**F11 — The task principal is derived from two environment variables that any
shell can override.** `currentTaskUser` (`schtasks.go:203-210`) returns
`USERDOMAIN\USERNAME`, or bare `USERNAME` if `USERDOMAIN` is empty, or `""` if
both are. That value is written into `Triggers.LogonTrigger.UserID` (`:103`) and
`Principals.Principal.UserID` (`:106`). An empty or unresolvable UserId makes
`schtasks /create` fail, surfacing as the raw schtasks message
(`setup_schtasks.go:46-48`). There is no `LookupAccountName`, no token-derived
SID, and no `whoami` fallback — even though `appdirs.currentUserSID()`
(`security_windows.go:36-47`) already does the correct thing in the same module
(exported, per its own comment, for `internal/admin`'s owner check; it serves
this caller equally well). Measured (probe 10): `USERNAME=bogus USERDOMAIN=`
renders `<UserId>bogus</UserId>`, and both empty render `<UserId></UserId>`.
The draft's claim that a Microsoft-account principal is "frequently not the form
Task Scheduler wants" is **contradicted on this host**: its account is a
Microsoft account, the environment-derived `MAC420\macsm` equals the
token-derived name, and registration succeeds. Task Scheduler stores the
resolved SID (probe 5).

**F12 — `isActiveWindows` swallows every error, and status parsing is
English-only.** `control_schtasks.go:9-15` returns `(false, nil)` for *any*
`schtasks /query` failure, so "not registered", "schtasks not on PATH" and "the
task store is broken" are indistinguishable. `taskStatusRunning` (`:23-32`)
matches the literal key `Status` and value `Running`; on a non-English Windows
both are localised and it returns false. The comment accepts that as
conservative and `TestTaskStatusRunning/localised` pins it. That a
non-English Windows localises these fields is **[unverified]** here (English-only
host, probe 11), but it is the premise the code's own comment states. The
consequence is downstream: `Lifecycle.WaitHealthy` (`updateclient/lifecycle.go:93-128`)
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
untested gap. And the script has never run at all (F16). This is why a port that passes every automated gate on Windows can
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

**F16 — The Windows acceptance script has never run.**
`scripts/acceptance-windows.ps1:65` is `throw "$Label: got '$g' want '$w'"`.
`$Label:` parses as a scope-qualified variable, so the whole file fails to parse
under both PowerShell 5.1 and 7 (probe 12), and no check in it has ever
executed. The line dates from `ca436bb` (2026-09-06), the commit that created
the script for MADR 0145. With that one line fixed, the script runs, and its
`paths --json` check fails on the `log_dir` mismatch described in F3. So the
gate this record's context took as green has been silently absent, and it
would be red on its first real run.

**F17 — `--unit-name` is silently dropped on Windows.** `setup-service
--print-only --unit-name mcr-accept` renders the same task as without it (probe
10). `taskNameFor` (`taskname.go:5`) returns the bare product, and every
lifecycle probe (`control_schtasks.go`) keys on the product too. This is the
same class of defect as `--env` in F9: an accepted, documented flag with no
effect. It cannot simply be honoured on Windows, because `update`'s lifecycle
would then look for a task under the product name and not find it.

**F18 — Rolling back a refreshed definition runs the OLD binary's restore
code.** `Reconciler.Restore` (`internal/updateclient/lifecycle.go:169-184`)
calls `service.ExecRefresher.RestoreUnit`, which is `RestoreUnitBackup`
(`internal/cli/service/refresh.go:129-146`) in the process performing the
update, and that process is the old binary. The code is `os.Rename(backup,
path)` plus a `systemctl` reload on Linux; it knows nothing of Task Scheduler.

So once D1 lets a new binary refresh a Windows task, the first update from any
pre-D1 binary has this failure mode. If `Start` or `WaitHealthy` fails after a
successful refresh, `Restore` fails, the binary is rolled back, and the
refreshed task definition stays behind. A refreshed definition that carries a
`serve` flag the old binary does not know (D6, D7) would then leave the
rolled-back daemon unable to start. **[Inferred from the code; not
exercised.]**

**F19 — Setup's idempotency check can never match a real registered task.**
`sameTaskDefinition` (`setup_schtasks.go:87-89`) compares normalised text.
Task Scheduler rewrites a definition on registration. It drops default-valued
elements (`RunLevel`, `AllowHardTerminate`, `Enabled`, `Hidden`,
`RunOnlyIfNetworkAvailable`, the trigger's `Enabled`), stores the principal as
a SID, and adds `URI`, `IdleSettings` and `UseUnifiedSchedulingEngine`
(measured: an element-level diff of `setup-service --print-only` against
`schtasks /query /xml ONE` for the live task). So the two texts are never
equal. Measured with the installed v0.17.4, the very binary that registered the
task: `mcremote setup-service` exits 1 with 'scheduled task "mcremote" exists
with different content (pass --force to overwrite)', and the live definition
is byte-identical before and after. The unit tests pass because their fake
`schtasks` returns the rendered text. MADR 0116 C2 therefore does not hold on
Windows, and a refresh built on the same comparison would report `refreshed`
on every update.

**Not findings, recorded because they were checked and are sound.**
`fsutil`'s Windows work is evidence-based and correct: `rename_windows.go:32-38`
matches `ERROR_ACCESS_DENIED` *and* `ERROR_SHARING_VIOLATION` on measured
behaviour (MADR 0153 F2), `rename_other.go:5-19` makes the POSIX retry a
compile-time no-op, and `syncdir_windows.go` documents its durability gap with
an upstream issue (golang/go#75541) that `docs/ops-windows-install.md` repeats.
`procutil.SuperviseStarted` has five real call sites (six before MADR 0160) and
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

Each decision names the measurement it rests on. Where a behaviour could not be
measured on this host, the decision says so and the PLAN verifies it in the
phase that depends on it.

* **D1 — Implement `setup-service --refresh` for the schtasks path, and let
  Windows restore a refreshed task.**
  * `RefreshUnit` (`refresh.go:117-124`) gains a `windows` arm calling
    `refreshSchtasks`.
  * `refreshSchtasks` reads the registered definition with the existing
    `/query /xml ONE` path (`setup_schtasks.go:23`). It treats the definition
    as managed only if the `Description` is setup's own (probe 5) and there is
    exactly one `Exec` action running `serve`.
  * It recovers `Options` from the `Exec` element. It compares
    **semantically** (D14), and re-registers only when the definitions differ.
    Before re-registering, it writes the exported XML to a backup file.
  * `RestoreUnitBackup` gains a Task Scheduler branch: re-register from the
    backup through `encodeTaskXML`.
  * Because of F18, this capability ships in a release **before** any change
    to the task's `serve` arguments (D6, D7). Closes **F1**; bounds **F18**.
* **D2 — Teach `PrintSetupResult` the `windows-task` scope.**
  * Print `schtasks /query /tn <name>`, `schtasks /run /tn <name>`, stop as
    `schtasks /change /tn <name> /disable` then `/end` (D5), and removal as
    `setup-service --remove`.
  * Name the binary installer as `install.ps1`.
  * The log line comes from MADR 0157 D10 once it lands. Until then, state
    that the Windows daemon writes no log file.
  * Never print `systemctl`, `journalctl`, `loginctl`, `.service` or
    `make install` for this scope. mcrelay's Windows note
    (`relay/cli.go:492-501`) stays and follows the corrected block.
  * Closes **F2**.
* **D3 — The Windows log destination is MADR 0157's.** 0157 already decides
  and plans it:
  * P1 unifies the resolved directory and fixes the documentation and
    acceptance disagreement in F3;
  * P4 wires the file sink;
  * P7 is the Windows acceptance.

  This record does not re-plan it. D7 depends on 0157 P4 being complete.
  Closes **F3** by reference and makes **F8** observable.
* **D4 — Fix `socketIdentity` on Windows and make its test assert.**
  * Derive the identity by opening the path with `windows.CreateFile`, with no
    access rights, share read, write and delete, `OPEN_EXISTING`, and
    `FILE_FLAG_OPEN_REPARSE_POINT|FILE_FLAG_BACKUP_SEMANTICS`, then calling
    `GetFileInformationByHandle`. Probe 2 measured a stable non-zero index this
    way; the exact flags were not recorded, and the now-asserting test decides
    them.
  * `owner_windows_test.go:57-59` fails instead of skipping.
  * Closes **F4**; restores the guard **F5** lacks.
* **D5 — Restart through a repeating trigger, and make stop mean
  disable.**
  * The task gains a `TimeTrigger` repeating every `PT1M` with no end,
    alongside the `LogonTrigger`. `MultipleInstancesPolicy` stays `IgnoreNew`
    (probe 8: relaunch within one interval, no duplicate instance, both
    triggers accepted together).
  * `stopWindows` becomes `schtasks /change /disable` then `/end`, and
    `startWindows` becomes `/change /enable` then `/run` (probe 8: both are
    unelevated, and a disabled task is not relaunched). Otherwise the trigger
    would restart a daemon that `update` stopped to replace its binary.
  * `RestartOnFailure` stays, for launch failures, and its comment stops
    claiming parity.
  * Documented residuals:
    * a restart takes up to about a minute on Windows, against 5 s with
      systemd;
    * a bare `schtasks /end` is undone within a minute, so the operator stops
      the daemon with the D2 commands.
  * Closes **F7**.
* **D6 — Deliver `--env` through a private file, not the task XML.** The task
  XML cannot carry environment (probe 9).
  * On Windows, `setup-service --env K=V` writes the validated entries to an
    owner-only `service.env` beside the service config.
  * The task's arguments gain `--env-file "<path>"`. `serve --env-file` applies
    the entries before `config.Load`, refusing a file that is not owner-only,
    the same posture as MADR 0155.
  * `--refresh` recovers them from the file, and `--remove` deletes it.
  * No Windows `servicePathExtras` is added: a task already inherits the
    registry `Path` (probe 9).
  * Ships after D1's release (F18). Closes **F9**.
* **D7 — Hide the console by detaching it in the daemon, not by changing the
  binary's subsystem.**
  * `serve` gains `--detach-console`. On Windows it calls `FreeConsole()` as
    the first action of `RunE`; elsewhere it does nothing. Only the task's
    arguments pass it.
  * Probe 7 measured no console host at 50 ms resolution with this, while the
    exit code and `/end` semantics stayed correct and the CLI was unaffected.
  * Rejected, on measurement: `-H windowsgui` (the CLI loses exit codes and
    output in PowerShell) and `conhost --headless` (`/end` orphans the daemon,
    and the exit code is lost).
  * After `FreeConsole` stderr goes nowhere, so this ships only after MADR 0157
    P4's file sink, and after D1's release (F18).
  * Closes **F10**.
* **D8 — Derive the task principal from the process token.**
  * The principal's `UserId` becomes the SID from `appdirs.CurrentUserSID()`,
    which is the form Task Scheduler stores (probe 5). The `LogonTrigger`
    `UserId` and `Author` become the account name from `SID.LookupAccount`.
  * `USERNAME` and `USERDOMAIN` are no longer read. A lookup failure is an
    error before `/create`, never an empty `UserId` (probe 10).
  * Closes **F11**.
* **D9 — Probe task state without parsing localised text, and let errors
  surface.**
  * `isActiveWindows` and `isInstalledWindows` query
    `(Get-ScheduledTask -TaskName <name> -ErrorAction SilentlyContinue)`
    through `powershell -NoProfile -NonInteractive`, reading the numeric
    `State` (4 = Running) and `$null` for a missing task (probe 11). A failure
    to run the probe itself is returned as an error, not as "not running".
  * The per-call cost of about 1.3 s fits `WaitHealthy`'s 30 s window.
  * Closes **F12**, without depending on the unmeasured localisation claim.
* **D10 — Correct the documents; defer the signing hook until a certificate
  exists.**
  * `docs/ops-windows-install.md:40-43` stops claiming the hook is wired.
  * MADR 0116 gets an additive amendment: D14's hook waits for a certificate,
    because a hook nothing can call cannot be verified.
  * Closes **F13**.
* **D11 — Make the acceptance script run, then give the service lifecycle a
  gate.**
  * Fix `acceptance-windows.ps1:65` so it parses. Its `log_dir` check stays
    red until MADR 0157 P1, which owns that path.
  * Add `scripts/acceptance-windows-service.ps1`. It exercises the full
    lifecycle through **mcrelay**, whose task is not installed on this host
    (probe 12), so the operator's live `mcremote` task is never touched. It
    covers:
    * unelevated `setup-service --force`;
    * the summary text;
    * `Running` via D9's probe;
    * `--refresh --json` reporting `unchanged`;
    * a killed daemon relaunched within 120 s (D5);
    * `--remove` leaving no task and no process.

    It cleans up in a `finally` block.
  * A real `update` of the live mcremote task is a manual, owner-approved step
    in the PLAN's rollout, never automated.
  * Closes **F14** and **F16**.
* **D12 — Assert the Windows ACL contract of atomic writes.** Where
  `SkipIfNoPOSIXModes` skips, assert `appdirs.FileIsOwnerOnly` of a file
  written by `WriteFileAtomic` into an `EnsurePrivateDir` directory. That this
  holds is **[unverified]**. If the new assertion fails, that is a new finding
  for its own record, and the test is not weakened. Closes **F15**.
* **D13 — Refuse `--unit-name` on Windows unless it equals the product.** The
  lifecycle cannot follow a renamed task (F17), so accepting the flag and
  ignoring it is replaced by a clear error. Closes **F17**.
* **D14 — Compare task definitions semantically.** Replace
  `sameTaskDefinition`'s text comparison with a comparison of the fields setup
  controls:
  * triggers, principal SID, logon type, settings, and the `Exec` command,
    arguments and working directory;
  * default values applied to both sides;
  * the principal compared by SID.

  Setup's `Unchanged` and D1's `unchanged` verdict then mean what they say on a
  real task store. The unit fixtures gain a real `schtasks` export (probe 5),
  with the SID replaced. Closes **F19**.

### Consequences

* Windows operators gain:
  * a working `update`;
  * a daemon that comes back after a crash;
  * correct post-install guidance;
  * no terminal at logon;
  * a `--env` that does something;
  * a setup that reports `unchanged` when nothing changed.
* The work ships in **two releases** (F18):
  1. The first carries D1, D2, D4, D5, D8, D9, D11–D14, all of which leave the
     task's `serve` arguments as older binaries expect.
  2. The second carries D6 and D7, which add `serve` arguments. D7 also waits
     for MADR 0157 P4.
* A host that jumps from a pre-D1 binary straight to the second release is
  exposed to F18 if its update fails after the refresh. The recovery is
  `setup-service --force` with the binary left in place. The PLAN's rollout
  names it.
* D5 does not reach systemd parity:
  * restart latency is about a minute, not 5 s;
  * stopping requires disabling.
  Both are documented, not hidden.
* D9 costs about 1.3 s per status probe instead of about 50 ms.
* D11's new script registers and removes a real task. It must never run
  elevated, and must clean up on failure.
* None of this promotes Windows to Tier 1, adds `windows/arm64`, or introduces
  SCM support.

### Confirmation

```powershell
# 1. Still compiles and vets for Windows.
$env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'; go vet ./...   # exit 0

# 2. D4 / D12 - no longer skipped.
go test ./internal/admin/ -run TestSocketIdentityStable -v     # --- PASS
go test ./internal/fsutil/ -run 'TestWriteFileAtomic$' -v      # --- PASS

# 3. D14 - setup is idempotent against the real task store.
mcremote setup-service; mcremote setup-service                 # second: exit 0, reports unchanged

# 4. D1 - refresh supported, and unchanged on a current task.
mcremote setup-service --refresh --json                        # {"verdict":"unchanged",...}

# 5. D2 - no Unix commands in the Windows summary (from the D11 script).

# 6. D5 / D9 / D11 - the service lifecycle gate.
pwsh -File scripts\acceptance-windows-service.ps1              # 0 checks failed

# 7. D7 - no console host at logon (release 2): the probe-7 method run against the real task.

# 8. D8 / D13 - principal and flags.
$env:USERNAME='bogus'; mcremote setup-service --print-only     # UserId is the SID, not "bogus"
mcremote setup-service --print-only --unit-name other          # error, not silence

# 9. D10 - no document claims an unwired hook.
git grep -n 'MC_WINDOWS_SIGN' -- docs/ops-windows-install.md   # only in the "not yet" wording
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
* Bad, because Task Scheduler cannot express `Restart=always` or restart a
  program that exits (F7, measured). D5 works around that with a repeating
  trigger and about a minute of latency. It also cannot redirect stdio (F3, so
  logging moves into the daemon through MADR 0157), and cannot stop a task
  gracefully (F8). Option A closes these by working around the engine or by
  documenting the residual; it never fully reaches Unix parity.
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
| Reconcile error triggers rollback | `mcplib@v1.4.1/selfupdate/managed.go:89-90`; the full sequence at `:64-133` |
| Windows task counts as installed | `internal/cli/service/control_schtasks.go:34-40` |
| `PrintSetupResult` has no windows arm | `internal/cli/service/result_print.go:48-57`, `:75-135` |
| Windows scope value | `internal/cli/service/setup_schtasks.go:20`; `internal/cli/service/setup.go:314` |
| mcrelay knows the scope but calls the shared printer first | `internal/relay/cli.go:480`, `:492-501` |
| Windows `Logs` root is set | `internal/appdirs/roots_windows.go:64` |
| `LogDir` derived from it (product appended) and printed | `internal/appdirs/paths.go:63-71` (`:70`); `internal/cli/paths.go:47`, `:68`, `:91-92`; measured `log_dir` `…\Local\mcremote\Logs\mcremote` |
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
| `RestartOnFailure` does not restart a program that exits (0, 1 or `/end`) | measured, probe 8: 1 run each in 4 min 42 s; GUI exit-1 variant 1 run in 1 min 43 s |
| Repeating trigger relaunches, never duplicates, is silenced by disable | measured, probe 8: runs at 07:52:14, 07:53:15, 07:54:15; long-running 1 run and 1 process over 4½ min; 0 runs in 150 s disabled; `/change /disable` and `/enable` unelevated |
| Logon and repeating triggers coexist | measured, probe 8: `mcr-probe-both` registered with both |
| systemd restart policy | `internal/cli/service/mcremote.user.service.tmpl:17-18`, `:28-32` |
| launchd restart policy | `internal/cli/service/plist_render.go:169-172` |
| systemd/launchd environment blocks | `mcremote.user.service.tmpl:41-52`; `plist_render.go:89-125` |
| `servicePathExtras` is POSIX-only and `:`-joined | `internal/cli/service/setup.go:869-881`, `:887-910` |
| Task XML rejects an environment element | measured, probe 9: "The task XML contains an unexpected node. (5,239):Environment" |
| A task inherits the registry Machine+User `Path` and no `HOME` | measured, probe 9 (54 variables) |
| `--env` is bound and plumbed | `internal/cli/setup_service.go:29`, `:48`, `:92`; `internal/relay/cli.go:430` |
| `--env` is consumed by plist and systemd | `plist_render.go:119`; `setup.go:808`, `:978-979` |
| `--env` is dropped by schtasks | measured: grep `ExtraEnviron\|Environ` in `schtasks.go` + `setup_schtasks.go` → zero hits |
| No `-H windowsgui` anywhere | measured: repo-wide grep `windowsgui\|ShowWindow\|GetConsoleWindow\|SW_HIDE\|CREATE_NO_WINDOW\|DETACHED_PROCESS` → zero hits |
| Release/local link flags | `Makefile:43`; `scripts/ci-windows-local.ps1:132` |
| Task runs in the interactive session | `internal/cli/service/schtasks.go:105-108` |
| The at-logon console is handed to Windows Terminal | measured, probe 6: Windows Terminal started 1 s after the daemon; the probe task created `OpenConsole` within about 110 ms |
| `-H windowsgui` breaks the CLI in PowerShell | measured, probe 7: `> file` empty; exit 1 reported as 0 |
| `conhost --headless` orphans on `/end` and loses the exit code | measured, probe 7 |
| `FreeConsole()` at start creates no console host | measured, probe 7 (50 ms sampling for 3 s) |
| `Hidden` is false, and hides the task not the window | `internal/cli/service/schtasks.go:71`, `:117` |
| Principal comes from two env vars | `internal/cli/service/schtasks.go:202-210`, used at `:103`, `:106` |
| Overridden env gives a wrong or empty principal | measured, probe 10: `<UserId>bogus</UserId>`; `<UserId></UserId>` |
| A Microsoft account works with the env form; Task Scheduler stores the SID | measured, probes 5 and 10: `PrincipalSource MicrosoftAccount`; token = env = `MAC420\macsm`; export `UserId` is the SID |
| A correct token-based SID helper already exists | `internal/appdirs/security_windows.go:36-47`, exported at `:328` |
| `isActiveWindows` swallows errors | `internal/cli/service/control_schtasks.go:9-15` |
| Status parsing matches English literals | `internal/cli/service/control_schtasks.go:23-32` |
| A locale-free state probe exists | measured, probe 11: `Get-ScheduledTask` State 4 or absent, 1261–2375 ms |
| `WaitHealthy` polls `Running` for 30s | `internal/updateclient/lifecycle.go:93-128`, `:15-20` |
| D14 promised `MC_WINDOWS_SIGN_*` | `docs/spec/0116-MADR-windows-and-linux-arm64-build-targets.md:917-918` |
| The hook does not exist | measured: repo-wide grep `MC_WINDOWS_SIGN\|signtool` → hits only in `docs/` |
| `codesign-maybe` is darwin-gated | `Makefile:204-210` |
| Ops doc claims signing is wired | `docs/ops-windows-install.md:40-43` |
| Acceptance script coverage | `scripts/acceptance-windows.ps1:74-176`, manual steps `:181-186` |
| Acceptance script never parsed | measured, probe 12: PowerShell 5.1 and 7 `ParseFile` 1 error at line 65; `git log -L65,65` gives `ca436bb` |
| Once parsed, 2 checks fail | measured, probe 12: `log_dir` mismatch; `go test ./...` timing failures (3 tests, each 3/3 alone) |
| `--unit-name` and `--env` are dropped on Windows | measured, probe 10: byte-identical `--print-only` |
| Rollback restore runs in the old binary | `internal/updateclient/lifecycle.go:169-184`; `internal/cli/service/exec_refresher.go:86-87`; `internal/cli/service/refresh.go:129-146` |
| Setup idempotency never matches a real task | measured: element diff of render against export; installed v0.17.4 `setup-service` exits 1 "exists with different content" |
| mcrelay is a safe acceptance subject here | measured, probe 12: `mcrelay setup-service --print-only` renders; `schtasks /query /tn mcrelay` finds nothing |
| Atomic-write tests skip on Windows | `internal/fsutil/atomic_test.go:13`, `:129`; `internal/testexec/testexec.go:67-74`, `:118-124`; measured `--- SKIP` ×2 |
| `SuperviseStarted` is genuinely wired (not a finding) | measured: six call sites at `b3d3355`, five at `38078c5`; `--- PASS: TestSuperviseStartedKillsTree` |
| Codex default transport is stdio (not a finding) | `internal/provider/codex/config.go:80`; `configs/config.example.yaml:225` |

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
* **MADR 0157** — `daemon-owned-rolling-logs`, `status: proposed`. **D3 is
  delivered by it, and D7 waits for its P4.** Its context states that the
  Windows task "has no console and discards" the stream. Probe 6 contradicts
  the first half: the task gets a console, handed to Windows Terminal, and the
  output goes nowhere readable. 0157 should be corrected when it is next
  revised. Its own text already contains a "Rotation under a Windows tail
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
* **MADR 0162** — isolated config-test roots. It made `make ci-windows` fully
  green on this host on 2026-09-19, which is the gate this record's PLAN
  relies on.
* **MADR 0118** — symlink privilege as a machine property, and D2's rule that a
  blanket skip converts a broken environment into silent non-coverage. That rule
  is what makes **F4**'s `t.Skip` and **F15** defects rather than pragmatism.
* `docs/ops-windows-install.md` — the operator-facing page. Already documents
  the syncDir gap, `/end` semantics, at-logon-not-boot, the npm `.cmd` shim, and
  the "not supported" list. **F3** (the Logs table row) and **F13** (the signing
  claim) are statements in it that the code does not support.

### Open questions for the plan

The draft's seven questions, answered on 2026-09-19:

1. **Does D3 wait for MADR 0157?** D3 *is* 0157. This record no longer plans
   logging. D7 waits for 0157 P4, and D11's `log_dir` check waits for 0157 P1.
2. **What does `-H windowsgui` break?** The CLI in PowerShell: redirected
   output is empty and exit codes are lost (probe 7). D7 uses `FreeConsole()`
   in the task-launched daemon instead.
3. **Is there a machine-readable task state?** Yes: `Get-ScheduledTask`'s
   numeric `State` (probe 11). A non-English host was not available, so D9
   avoids depending on the answer rather than asserting it.
4. **Which install roots belong in a Windows `servicePathExtras`?** None, on
   this evidence: the task inherits the registry `Path` (probe 9). Profile-only
   CLIs remain **[unverified]**; none is installed here.
5. **Raise `Count`, or a watchdog?** Raising `Count` does nothing: exits are
   never restarted (probe 8). A repeating trigger is the watchdog, and stop
   becomes disable (D5).
6. **Where does the stateful acceptance run live?** In a separate
   `scripts/acceptance-windows-service.ps1`, driven through mcrelay so the live
   mcremote task is untouched (D11).
7. **Is F13 a code fix or a documentation fix?** Documentation now; the hook
   waits for a certificate (D10).

None remain open. The PLAN verifies the three **[unverified]** items in the
phases that depend on them: the `FreeConsole` behaviour at a real logon (D7),
the ACL of an atomic write (D12), and a repeating trigger with a fixed past
`StartBoundary` (D5).
