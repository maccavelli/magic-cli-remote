# Windows install and operations

`mcremote` and `mcrelay` run natively on **Windows 10 1809 or later,
`windows/amd64`**. This page covers what differs from Linux and macOS.

Decisions behind this page: [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md).

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
.\install.ps1
```

Binaries land in `%LOCALAPPDATA%\Programs\mcremote\` and
`%LOCALAPPDATA%\Programs\mcrelay\` — **per-user, no elevation**, the analogue
of `~/.local/bin` on Unix.

### SmartScreen

The published binaries are **not Authenticode-signed yet**. Windows SmartScreen
may warn on first run, and Smart App Control may block them outright. This is
expected. Signing is designed into the build (`MC_WINDOWS_SIGN_*`) but the
certificate has not been procured; note that signing alone does not grant
SmartScreen trust — reputation accrues over releases.

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
three consequences worth knowing before you rely on it:

- **It starts at logon, not at boot.** There is no unattended operation, and
  `mcrelay` in particular cannot serve a headless Windows server this way.
- **`schtasks /end` terminates the process** rather than asking it to drain.
  Provider process trees still die with it — they are held in a Job Object —
  and a stale admin socket is detected and cleared on the next start. What is
  lost is an orderly hub teardown.
- **`sc.exe create` and NSSM will not work.** These binaries do not call
  `StartServiceCtrlDispatcher`, so the Service Control Manager kills them at
  the start-up timeout. Running them under the SCM is unsupported.

Useful commands:

```powershell
schtasks /query /tn mcremote /fo LIST /v    # status, and who it runs as
schtasks /run   /tn mcremote                # start now
schtasks /end   /tn mcremote                # stop
mcremote setup-service --remove             # deregister
```

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
script, `foo.ps1`, and `foo.cmd`. Only the `.cmd` is launchable, and Windows
requires it to go through `cmd.exe /c`.

`mcremote` handles this, but it **refuses** to pass an argument containing a
character `cmd.exe` would reinterpret — `& | < > ^ % " ( ) !` — because quoting
is not a reliable defence against delayed expansion. If you hit that error, set
the provider's `bin` to the real executable rather than the shim:

```yaml
providers:
  codex:
    bin: C:\Users\dev\AppData\Roaming\npm\node_modules\@openai\codex\bin\codex.exe
```

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
