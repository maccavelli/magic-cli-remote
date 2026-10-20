# Windows install and operations

`mcremote` and `mcrelay` run natively on **Windows 10 1809 or later,
`windows/amd64`**. This page covers what differs from Linux and macOS.

Decisions behind this page: [0116-MADR-windows-and-linux-arm64-build-targets.md](../decisions/0116-MADR-windows-and-linux-arm64-build-targets.md).

## Support tier

Windows is **Tier 2**: built, unit-tested and smoke-tested in CI on every push
and tag, but not exercised by the live provider suites. Linux and macOS are
Tier 1.

`windows/arm64` is **not supported** — not built, not published. This is a
deliberate decision (MADR 0116 D19), not a gap waiting to be filled: it is not
a first-class Go port, it has no local acceptance host, and its runner image is
the most divergent. Windows on Arm can run the amd64 build under emulation, but
the installer will not select it for you.

## Install

```powershell
irm https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.ps1 | iex
```

Or download and inspect first, which is the better habit:

```powershell
irm https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.ps1 -OutFile install.ps1
notepad install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

Each release publishes its own `install.ps1`, generated from the release by
go-selfupdate-lib
([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)).
It downloads both binaries over HTTPS, checks each against the release's
`SHA256SUMS`, and installs them into one folder,
`%LOCALAPPDATA%\Programs\magic-cli-remote\` — **per-user, no elevation**, the
analogue of `~/.local/bin` on Unix. Each new binary must report the release's
version, or the previous ones are put back. The previous binary is kept as
`<name>.exe.prev`.

It then runs `mcremote setup-service --from-installer`, which registers the
background task on a first install (see
[Running in the background](#running-in-the-background)). On a later install
it refreshes the task and restarts it on the new binary, the way `update`
does. A task that runs another folder's binary is reported and left alone.
`mcrelay setup-service --from-installer` runs too, and does nothing unless a
relay task exists.

Windows has no per-user program folder that is on `PATH` by default, so the
installer adds its folder to your **User** `Path`, as winget and Scoop do with
theirs (MADR 0159 D16). It adds the folder only if it is missing, keeps every
existing entry as written, including `%VAR%` references, and tells running
programs about the change. Open a new terminal to use `mcremote` by name.

### Options

| Parameter | Environment variable | Default |
| --- | --- | --- |
| `-Version TAG` | `MCREMOTE_VERSION` | the installer's own release |
| `-InstallDir DIR` | `MCREMOTE_INSTALL_DIR` | `%LOCALAPPDATA%\Programs\magic-cli-remote` |
| `-Product NAME` | — | both `mcremote` and `mcrelay` |
| `-NoHooks` | `MCREMOTE_NO_HOOKS=1` | unset: run `setup-service --from-installer` |
| `-NoPathUpdate` | `MCREMOTE_NO_PATH_UPDATE=1` | unset: add the folder to the User `Path` |
| `-VerifyAttestation` | — | unset; also verifies each download's attestation with `gh` |
| `-DryRun`, `-Uninstall` | — | — |

`irm | iex` cannot take parameters. Either set the environment variable first,
or run the download as a script block:

```powershell
$env:MCREMOTE_NO_PATH_UPDATE = '1'
irm https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.ps1 | iex

& ([scriptblock]::Create((irm https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.ps1))) -NoPathUpdate
```

**Renamed from the earlier hand-written installer:** `MCREMOTE_INSTALL_NO_PATH_UPDATE`
is now `MCREMOTE_NO_PATH_UPDATE`. `MCREMOTE_VERSION`, `MCREMOTE_INSTALL_DIR` and
`MCREMOTE_NO_HOOKS` are new on Windows. The earlier installer did not register
the task; it printed `Next: mcremote setup-service`.

**Installing an older release.** `-Version` refuses a release published before
this installer, because its binaries report their version in an older form: it
puts back what was there and fails with exit code `2`. Use that release's own
`install.ps1`, from
`https://github.com/maccavelli/magic-cli-remote/releases/download/<tag>/install.ps1`.

Exit codes, when run with `-File`: `0` installed · `1` usage or environment
error · `2` download, checksum or version check failed (nothing changed, or the
previous binaries restored) · `3` installed, but `setup-service` failed. Under
`iex` a failure is an error, `install failed (exit N)`, and the session stays
open.

### Uninstall

Two commands, in this order:

```powershell
mcremote setup-service --uninstall
& ([scriptblock]::Create((irm https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.ps1))) -Uninstall
```

The first stops and removes the tasks that run a binary from this folder. The
second deletes the binaries and their `.prev` copies, and takes the folder off
the User `Path` once neither is left. Configuration and data stay. The
installer will do both once go-selfupdate-lib has a `before_uninstall` hook
([issue 1](https://github.com/maccavelli/go-selfupdate-lib/issues/1)).

### SmartScreen

The published binaries are **not Authenticode-signed yet**. Windows SmartScreen
may warn on first run, and Smart App Control may block them outright. This is
expected. No signing step exists in the build yet: a signing hook waits for a
code-signing certificate, because a hook nothing can call cannot be verified
(MADR 0159 D10). Note that signing alone does not grant SmartScreen trust —
reputation accrues over releases.

## Where things live

Windows uses Known Folders, not XDG (MADR 0116 D3). `mcremote paths` prints the
resolved layout; the shape is:

| What | Path |
| --- | --- |
| Config | `%AppData%\mcremote\config.yaml` |
| Data (device tokens, certs) | `%LocalAppData%\mcremote` |
| State | `%LocalAppData%\mcremote\State` |
| Cache | `%LocalAppData%\mcremote\Cache` |
| Runtime (admin socket) | `%LocalAppData%\mcremote\Runtime` |
| Logs | `%LocalAppData%\mcremote\Logs` |

Config is under **roaming** `%AppData%` because it is the one thing worth
following you to another machine. Everything else is machine-specific and
deliberately does not roam.

Each of these directories is created with an owner-only DACL — the equivalent
of mode `0700`. If a directory exists but is owned by another user, the daemon
refuses to use it rather than repairing it.

## Running in the background

```powershell
mcremote setup-service          # no elevation required
```

This registers a **Task Scheduler at-logon task** running as you, at
`LeastPrivilege` (MADR 0116 D12). It is not a Windows Service, and that has
consequences worth knowing before you rely on it:

- **It starts at logon, not at boot.** There is no unattended operation, and
  `mcrelay` in particular cannot serve a headless Windows server this way.
- **It runs without a window, and so do the agent CLIs it starts.** The task
  passes `serve --detach-console`, so the daemon lets go of the console Windows
  gives it as it starts (MADR 0159 D7). Until the daemon writes its own log file
  (MADR 0157), its log output is not kept anywhere. To watch it, stop the task
  and run `mcremote serve` in a terminal.

  In **v0.18.1 only**, that fix moved the window rather than removing it: a
  process with no console is given a *new* one when it starts a child, so every
  agent session opened a terminal window, and closing that window killed the
  session. From v0.19.0 children are started with `CREATE_NO_WINDOW` whenever the
  daemon has no console, so no window appears anywhere in the tree (MADR 0159
  F22, F23, D17).
- **A stopped or crashed daemon is restarted within about a minute.** The task
  carries a trigger that fires every minute and starts the daemon if it is not
  running (it never starts a second copy). Task Scheduler's own restart-on-
  failure setting does not restart a program that exits, so this trigger is
  what does (MADR 0159 D5). Systemd restarts within 5 s; Windows cannot go
  below one minute.
- **`schtasks /end` terminates the process** rather than asking it to drain.
  Provider process trees still die with it — they are held in a Job Object —
  and a stale admin socket is detected and cleared on the next start. What is
  lost is an orderly hub teardown. `mcremote update` does not lose it: it asks
  the daemon to shut down over its admin socket first, and ends the task only
  if the daemon is still running 30 seconds later
  ([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
  A2).
- **Stopping one session asks the agent CLI to finish first.** The daemon sends
  `CTRL_BREAK` and escalates to a hard kill only if the tree is still there when
  the timeout expires. That signal needs a console shared with the child, which a
  windowless daemon does not have, so from v0.19.0 it briefly attaches to the
  child's own console to deliver it (MADR 0159 D22). In **v0.18.1 and v0.18.0**
  this phase failed silently and every provider tree was hard-killed — correct,
  because the Job Object still collected the tree, but with nothing given a chance
  to flush (MADR 0159 F24).
- **`sc.exe create` and NSSM will not work.** These binaries do not call
  `StartServiceCtrlDispatcher`, so the Service Control Manager kills them at
  the start-up timeout. Running them under the SCM is unsupported.

Useful commands (the same ones `setup-service` prints):

```powershell
schtasks /query  /tn mcremote /fo LIST /v                          # status, and who it runs as
schtasks /change /tn mcremote /enable;  schtasks /run /tn mcremote # start now
schtasks /change /tn mcremote /disable; schtasks /end /tn mcremote # stop, and keep it stopped
mcremote setup-service --remove                                    # deregister
```

Stop by disabling first. A bare `schtasks /end` is undone within a minute by
the restart trigger; a disabled task stays stopped until you enable it. `setup-service` itself is safe to re-run: it compares what it would register
with what Task Scheduler holds and reports the task unchanged when they match
(MADR 0159 D14).

Two `setup-service` flags are refused on Windows rather than silently ignored
(MADR 0159 F9, F17). `--unit-name` is refused because the task is always named
after the product, and `update` looks it up by that name. `--env` is refused
because Task Scheduler cannot carry environment variables in the task
definition; a Windows delivery path for it is planned (MADR 0159 D6).

## Updating

```powershell
mcremote update           # check, download, verify, replace, restart
mcremote update --check   # only report whether an update is available
```

When the background task is installed, `update` stops it: it disables the task,
asks the daemon to shut down over its admin socket, waits up to 30 seconds for
it to exit, and ends the task only if it has not. It then replaces the binary,
and asks the **new** binary to refresh the task
definition. The refresh keeps every option baked into the task and re-registers
it only if this release's template differs. `update` then starts the task
again (enable, then run), waits for it to report running, and then for the
daemon's admin socket to answer a ping, at the socket of the data directory
the task runs with. The `mcremote` that runs `update` does all this, so an
update from a release before this one ends the task as before. If any step after
the swap fails, it rolls back: the previous binary is restored, and so is the
previous task definition, which the refresh saved under
`%LocalAppData%\mcremote\State` (MADR 0159 D1).

**Updating from v0.17.x or earlier.** Those releases cannot restore a task
definition. If an update from one of them fails after the refresh, the binary is
rolled back but the refreshed definition stays (MADR 0159 F18). It still runs
the old binary, because the refresh keeps the same arguments. If the task does
not come back to running, re-register it with the binary now in place:

```powershell
mcremote setup-service --force
```

Before MADR 0159, `update` on Windows always rolled back, because the task
definition could not be refreshed (F1).

**Going back to v0.18.0 or earlier by hand.** A task written by v0.18.1 or
later passes `--detach-console`, which older binaries reject, so the task
cannot start them. After installing an older binary yourself, re-register the
task with it:

```powershell
mcremote setup-service --force
```

### The folder move

Both binaries now live in one folder, `%LOCALAPPDATA%\Programs\magic-cli-remote\`,
which is where go-selfupdate-lib's installer template puts them
([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
D9; PLAN S5g). An install in the per-product folders is moved by the task
refresh. That is the refresh `update` runs after the swap, or
`mcremote setup-service --refresh` by hand. With no task, or one the refresh
cannot reproduce (verdict `kept`), nothing moves. Otherwise:

1. Each of `mcremote.exe` and `mcrelay.exe` that is present is copied into the
   new folder. Each copy's `version` identity is compared with its source's. On
   any mismatch nothing is switched, and the refresh says why.
2. The task is re-registered to run the new folder's copy.
3. The User `Path` loses the two per-product entries and gains the new folder
   once. Every other entry stays as written, and so does the value's registry
   type. Running programs are told of the change, but terminals that were
   already open need to be reopened.
4. Each old copy that no task runs is deleted, with its `.prev`, and with the
   files the updater kept beside it (`.<name>.exe.selfupdate…`). Then the folder
   goes, if nothing else is in it. A copy in use stays, and a later refresh
   deletes it. Files you put there are not touched.

`--refresh --json` names the move in `migrated`. `--refresh --print-only`
makes no move.

Re-running `install.ps1` makes the same move. It installs into the new folder,
and its `setup-service --from-installer` refreshes a task that still runs a
per-product folder's copy, so the task moves as above. A binary the installer
has just put in the new folder is newer than the old copy, and is kept.

## Durability caveat

`WriteFileAtomic` writes to a temp file, fsyncs it, and renames it into place.
On Unix the parent directory is then fsynced too, so the rename itself is
durable. **On Windows that final step is a no-op.**

This is an upstream platform gap, not a shortcut: syncing a directory handle on
Windows returns "Access is denied" ([golang/go#75541][gh75541]), and there is
no API that flushes a directory entry independently of its files.

Practically: on NTFS the rename is ordered, but the directory entry is not
separately flushed, so a power loss in the window right after a write returns
can lose that write. The file contents themselves are still fsynced. If this
matters for your deployment, Linux and macOS do not have the gap.

[gh75541]: https://github.com/golang/go/issues/75541

## Provider CLIs installed by npm

npm installs a global CLI on Windows as three files: an extensionless shell
script, `foo.ps1`, and `foo.cmd`. `exec.LookPath` resolves the bare name to the
`.cmd`.

**`cmd.exe` is involved whether or not anyone asks for it.** `CreateProcess`
starts a batch file by spawning the command interpreter implicitly, so the shim's
arguments are parsed by `cmd.exe` rules — which are not the rules Go's `os/exec`
escapes for, and Go does not reconcile the two ([golang/go#68313][go68313],
[#69939][go69939]). This is the [BatBadBut][batbadbut] class, CVE-2024-24576.
Until v0.19.0 `mcremote` had a guard for it that was never reached, so an
argument of `a&calc` arrived at a shim as two commands (MADR 0159 F29, F30).

From v0.19.0 `mcremote` runs a shim as `cmd.exe /d /s /v:off /c` with a command
line it builds and quotes itself: `/d` skips the `AutoRun` registry hook, `/s`
makes quote handling deterministic, and `/v:off` disables delayed expansion. With
each argument quoted, `&`, `|`, `<`, `>`, `^`, `!`, `(`, `)` and spaces are passed
through **literally** — including paths like `C:\Program Files (x86)\tool\x`,
which the previous rule would have refused.

Four things still cannot be represented, and are **refused** with an error
naming which one it was: a `%`, because `%VAR%` expands even inside quotes; a
`"`, because it ends the quoted run and `cmd.exe` honours no escape for it; a
trailing `\`, because it meets the closing quote and the shim's own consumer
unescapes the pair; and a line break, because a command line cannot contain one.
If you hit any of these, set the provider's `bin` to the real executable rather
than the shim:

```yaml
providers:
  codex:
    bin: C:\Users\dev\AppData\Roaming\npm\node_modules\@openai\codex\bin\codex.exe
```

A native executable — `grok.exe`, or a `bin` pointed at one as above — never goes
near `cmd.exe` and none of these rules apply to it.

[batbadbut]: https://flatt.tech/research/posts/batbadbut-you-cant-securely-execute-commands-on-windows/
[go68313]: https://github.com/golang/go/issues/68313
[go69939]: https://github.com/golang/go/issues/69939

## Public ports (mcrelay)

Windows has **no privileged-port restriction**, so binding 443 or 80 needs no
elevation. Two other things do bite:

- **Excluded port ranges.** Hyper-V, WinNAT and Docker Desktop reserve wide
  dynamic ranges. Check before you debug a mysterious bind failure:

  ```powershell
  netsh int ipv4 show excludedportrange protocol=tcp
  ```

- **Windows Defender Firewall** prompts on the first inbound bind. Allow the
  private and/or public profile as appropriate for your deployment.

## What is not supported on Windows

Recorded so these read as decisions rather than oversights (MADR 0116 D15):

- Codex `unix_ws` and `managed_daemon_proxy` transports — Unix-only, and
  refused at config validation with a clear message.
- `grok` device auth's `sandbox-exec` path — Darwin-only.
- macOS TCC / Full Disk Access guidance — Darwin-only.
- Reading another process's environment (`procutil.ProcessEnv`) — needs
  `NtQueryInformationProcess` and a cross-bitness PEB walk.
- Running under the Service Control Manager, and pre-logon start.
- `windows/arm64`.

## Verifying a build yourself

```powershell
$env:CGO_ENABLED = '0'
go build ./... ; go vet ./... ; go test ./...
.\scripts\acceptance-windows.ps1
```

Every shipped binary is pure-Go, and that is asserted on the artifact rather
than assumed from the build recipe:

```powershell
go version -m .\mcremote.exe | Select-String CGO_ENABLED   # must be CGO_ENABLED=0
```

## Local CI-style gates on the Windows dev host (MADR/PLAN 0145)

These targets are **Windows-host-only**. On macOS/Linux they print
`Windows-only; skipping on <os>` and exit 0 (not a silent PASS). Unix hosts
keep using `make preflight`. Do not register an auto-hook that runs
`ci-windows` off-Windows. Never register the Windows dev host as a GHA self-hosted runner
(MADR 0116 F20).

| When | Command |
| --- | --- |
| Before push (unit / go-native mirror) | `make ci-windows` |
| Before tag (light release-shaped smoke) | also `make ci-windows-smoke` |
| Functional F5 / paths / doctor | `scripts/acceptance-windows.ps1` |

Script: `scripts/ci-windows-local.ps1`. Decisions:
[0145-MADR-local-windows-ci-style-tests.md](../decisions/0145-MADR-local-windows-ci-style-tests.md),
[0145-PLAN-local-windows-ci-style-tests.md](../decisions/0145-PLAN-local-windows-ci-style-tests.md).

### Drift vs CI (local intentionally omits)

| CI behaviour | Local default |
| --- | --- |
| go-native build/vet/test, CGO=0, `MC_REQUIRE_SYMLINK=1` | mirrored on Windows |
| go-native retry-once (0143) | omitted (optional `-RetryOnce`) |
| `-race` / flutter `preflight` | omitted |
| smoke-native GH artifact download | omitted; light local smoke builds `dist/*-windows-amd64.exe` |
| Run on macOS/Linux | skip + exit 0 |

## Codex's Windows sandbox, and the ACLs it leaves behind

`mcremote` does not provision Codex's Windows sandbox and never asks it to — a
test forbids referencing `windowsSandbox/setupStart` outside test files. Codex
provisions it on its own, and the consequences land on the host, so they are
recorded here.

**What provisioning does.** It creates the local accounts `CodexSandboxOffline`
and `CodexSandboxOnline` in a `CodexSandboxUsers` group, hides them from the sign-in
screen, adds Windows Firewall rules, and applies **deny-read ACLs** to paths the
active permission profile marks unreadable.

**There is no repair or uninstall command.** The only caller of Codex's own
cleanup is an MSIX uninstall event. The only record of which denies were applied
is `%USERPROFILE%\.codex\.sandbox\deny_read_acl_state.json`, and **if that file
is deleted or empty, every ACE it recorded is orphaned permanently** — nothing
scans for stale denies. Recovery is manual:

```powershell
takeown /f "%USERPROFILE%\.codex\config.toml"
icacls "%USERPROFILE%\.codex\config.toml" /grant "%USERNAME%":(F)
```

`icacls` cannot remove an ACE whose SID no longer resolves — it reports
`Successfully processed 0 files` and exits 52. Use the .NET ACL API for those
(`Get-Acl` / `RemoveAccessRule` / `Set-Acl`), which works on the raw SID.

**Two places a deny can come from**, and nothing else: the active permission
profile's `[permissions.<name>.filesystem]` Deny globs, and `[filesystem]
deny_read` in `%ProgramData%\OpenAI\Codex\requirements.toml`. No built-in rule
ever names `config.toml`, `auth.json` or `CODEX_HOME`, so a deny on one of those
came from a glob or from that managed file.

Worth checking on a shared machine: `C:\ProgramData` is writable by standard
users by default, so a non-administrator can pre-create
`C:\ProgramData\OpenAI\Codex` and plant a `requirements.toml`. Codex measures
that shape and reports it as telemetry; it does not prevent it.

**A downgrade hazard.** Codex 0.149.1 and earlier derived the principal to grant
from `%USERNAME%`, falling back to the literal string `"Administrators"` when the
variable was absent — which is what a service or relay-spawned app-server looks
like. Those releases therefore ACL'd `.sandbox`, `.sandbox-secrets` and
`.sandbox-bin` for an account the real user is not, and a non-administrator could
then neither read nor re-ACL them. 0.155.1 reads the OS token instead. `mcremote`
passes an explicit `USERNAME` to the engine regardless, as defence in depth.

Background: MADR 0163 F28–F30.
