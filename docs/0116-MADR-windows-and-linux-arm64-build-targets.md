---
status: proposed
date: 2026-08-27
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Ship mcremote and mcrelay on windows/amd64 and linux/arm64 behind a platform layer with native OS pathing

## Context and Problem Statement

The owner asked for four things, in one pass:

1. cross-compile support for `windows/amd64` and `windows/arm64`;
2. `linux/arm64` support;
3. **native OS pathing** that is **idempotent** and follows platform standards;
4. the codebase **optimized for the new Windows build targets**, grounded in
   Go 1.26.5 Windows idiom.

Today the two Go daemons are Linux/macOS products with a Windows-shaped hole.
The toolchain is Go 1.26.5 (`go.mod:3`), the module already depends directly on
`golang.org/x/sys v0.47.0` (`go.mod:18`), and `go tool dist list` on this
toolchain offers `windows/amd64` and `windows/arm64` but no `windows/arm` — the
32-bit ARM Windows port was **removed in Go 1.26**, as announced in the Go 1.25
notes.

This record decides **what "Windows support" means for these two daemons, which
platform behaviours are abstracted versus refused, and what the pathing and
idempotency contract is on every OS.**

**Scope note (2026-08-27).** The owner narrowed the ask after the findings
below were gathered: **`windows/arm64` is out of scope for this record.** The
shipped target set is `windows/amd64` plus the three existing POSIX targets and
`linux/arm64` — **five targets**. The arm64 Windows evidence is deliberately
*kept* rather than deleted, because it is what makes the deferral a considered
choice rather than an omission, and because D19 shows re-entry costs a matrix
line and no new code.

### What was measured, not assumed

All build probes below were run on this tree at `997896d` with Go 1.26.5, into a
scratch copy so the working tree stayed clean.

**`linux/arm64` already compiles, unchanged:**

```text
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/mcremote  → exit 0
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/mcrelay   → exit 0
```

It is also **already in the release matrix** (`.github/workflows/ci.yml:162`).
So "add linux/arm64" is not a compile problem — it is a *verification and
packaging* gap (see F2).

**`windows/amd64` fails on exactly three symbol groups:**

```text
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
# internal/admin
internal/admin/admin.go:81:20: undefined: appdirs.CheckSocketPathLength
internal/admin/admin.go:85:20: undefined: appdirs.EnsurePrivateDir
internal/admin/admin.go:88:20: undefined: appdirs.ValidateRuntimeDir
internal/admin/admin.go:98:32: undefined: syscall.Stat_t
internal/admin/admin.go:123:35: undefined: syscall.Stat_t
internal/admin/admin.go:139:36: undefined: syscall.Stat_t
# internal/procutil
internal/procutil/registry.go:78:20: undefined: appdirs.EnsurePrivateDir
# internal/relay
internal/relay/fileconfig.go:803:17: undefined: appdirs.EnsurePrivateDir
```

With three throwaway shims (`appdirs` private-dir/socket helpers, an
`fsutil.WithLock` stub, and a `syscall.Stat_t` replacement in `admin`), the
**entire tree — every package and every test file — compiles and vets clean for
both Windows architectures**:

```text
GOOS=windows GOARCH=amd64 go build ./...   → exit 0
GOOS=windows GOARCH=arm64 go build ./...   → exit 0
GOOS=windows GOARCH=amd64 go vet   ./...   → exit 0, no output
```

The `arm64` line is retained as evidence even though that target is now out of
scope (D19): it establishes that **nothing blocking Windows is
architecture-specific.** Every gap this record finds is a `GOOS` gap, so the
work below is `//go:build windows` throughout and inherently arch-agnostic.

That is the single most important fact in this record: **the port is not a
compile problem. It is a semantics problem.** Ten lines of shims buy a green
build and a daemon that would silently mis-behave on every security-relevant
path. The work is in the runtime behaviour behind those symbols.

### Findings

Each finding is evidence-led. Where a claim is an inference that a Windows host
must confirm, it is labelled **[unverified]**.

---

**F1 — The compile surface is three symbol groups, all in `appdirs`, `fsutil`
and `admin`.**

`internal/appdirs` has `ensure_unix.go` and `runtime_unix.go`, both
`//go:build unix`, with **no non-Unix counterpart**. They export
`EnsurePrivateDir`, `ValidateRuntimeDir`, `MaxUnixSocketPathLen` and
`CheckSocketPathLength`. Seven call sites depend on them:
`admin/admin.go:81,85,88`, `daemon/daemon.go:86`, `daemon/certs.go:29`,
`procutil/registry.go:78`, `relay/fileconfig.go:803`.

`internal/fsutil/lock_unix.go` is `//go:build unix` with no counterpart and
exports `WithLock`; four `internal/providerauth` call sites use it
(`reconcile.go:293`, `transaction.go:138,436,548`).

`internal/admin/admin.go` reads `fi.Sys().(*syscall.Stat_t)` at lines 98, 123
and 139 for uid and inode checks. `syscall.Stat_t` does not exist on Windows.

**F2 — `linux/arm64` is built but never verified, installed or checked.**

It compiles and is in the CI matrix, but:

* `scripts/verify-build-metadata.sh:19–21` builds and asserts tags only for
  `linux/amd64`, `darwin/arm64`, `darwin/amd64`. The `netgo,osusergo` policy is
  therefore **unenforced on `linux/arm64`**.
* The tag job's post-build smoke only runs `dist/mcremote-linux-amd64` and
  `dist/mcrelay-linux-amd64` (`ci.yml`, "Smoke the host-native build"), so an
  arm64 artifact is content-checked by SHA only.
* `scripts/install.sh:83–91` maps `aarch64|arm64 → arm64`, so the installer
  *can* fetch it; nothing proves the fetched binary runs.

**F3 — The Makefile applies `netgo,osusergo` to Windows, and on Windows
`netgo` is actively harmful.**

`Makefile:89–93` — the `else` branch that catches `GOOS=windows` — sets
`GO_TAGS := netgo,osusergo`. Verified against the Go 1.26.5 tree:

* `osusergo` on `windows/amd64` is a **no-op**. `go list -f '{{.GoFiles}}'
  os/user` returns the identical file set with and without the tag
  (`[lookup.go lookup_windows.go user.go]`) — `os/user` on Windows never used
  cgo.
* `netgo` on Windows is **not** a no-op. It swaps `net/netgo_off.go` for
  `netgo_on.go`, setting `netGoBuildTag = true`; `net/conf.go:87` then sets
  `confVal.netGo = true`, and `mustUseGoResolver` returns true. This **forces
  the pure-Go resolver on Windows**, overriding `goosPrefersCgo()`, which
  explicitly names `"windows"` as a platform that prefers the system resolver.
* The pure-Go resolver's Windows config source is
  `net/dnsconfig_windows.go`. It harvests DNS servers from
  `GetAdaptersAddresses` and **skips any adapter that is not
  `IfOperStatusUp` or has no `FirstGatewayAddress`**, and it populates **no
  search list, no NRPT policy, no per-suffix routing** — it sets only
  `servers`, `ndots: 1`, `timeout: 5s`, `attempts: 2`.

Consequence: a Windows host resolving the relay hostname through a VPN,
corporate split-DNS, or a gateway-less virtual adapter (the shape a Tailscale
or WireGuard interface takes) would have that adapter's resolver **silently
dropped**. **[unverified on hardware]** — the mechanism is read from the Go
source; the failure has not been reproduced on a Windows host. Note that
`internal/tailnet/tailnet.go` shells out to `tailscale ip -4` rather than
resolving a MagicDNS name, so this package itself is not exposed; the exposure
is the relay hostname on the `relayhost` dial path.

**F4 — `appdirs` is XDG-only and degenerates on Windows rather than failing.**

`internal/appdirs/roots.go` builds every root from `os.UserHomeDir()` plus XDG
env vars. On Windows `os.UserHomeDir()` returns `%USERPROFILE%`
(`os/file.go:608–609`), so `SystemRoots` *succeeds* and yields:

| Root | Windows value it would produce | Platform-correct value |
| --- | --- | --- |
| `ConfigHome` | `C:\Users\x\.config` | `%AppData%` (`FOLDERID_RoamingAppData`) |
| `DataHome` | `C:\Users\x\.local\share` | `%LocalAppData%` (`FOLDERID_LocalAppData`) |
| `StateHome` | `C:\Users\x\.local\state` | `%LocalAppData%\<product>\State` |
| `CacheHome` | `C:\Users\x\.cache` | `%LocalAppData%\<product>\Cache` |
| `Logs` | `C:\Users\x\Library\Logs` | `%LocalAppData%\<product>\Logs` |
| `RuntimeHome` | `%TEMP%\mcremote-runtime--1` | `%LocalAppData%\<product>\Runtime` |

Two specifics:

* `roots.go:66` sets `Logs: filepath.Join(home, "Library", "Logs")`
  **unconditionally**. That path is macOS-only and is nonsense on Windows.
* `roots.go:89` and `roots.go:96` call `os.Getuid()`. Go returns **`-1`** on
  Windows (`syscall/syscall_windows.go:1352`), so the temp runtime leaf becomes
  `mcremote-runtime--1` — identical for **every user on the machine**, in the
  shared `%TEMP%`. On a multi-user Windows box that is a collision *and* a
  cross-user data-exposure surface.

Go's own convention is available and unambiguous: `os.UserConfigDir()` returns
`%AppData%` and `os.UserCacheDir()` returns `%LocalAppData%`
(`os/file.go:504–512`, `557–565`), and `x/sys/windows` exposes
`KnownFolderPath` with `FOLDERID_RoamingAppData` / `FOLDERID_LocalAppData` /
`FOLDERID_ProgramData` (`syscall_windows.go:1577–1590`,
`zknownfolderids_windows.go:35–40`).

**F5 — Durable writes fail outright on Windows. This is the highest-severity
finding.**

`fsutil.WriteFileAtomic` honours `AtomicOptions.SyncDir` by opening the parent
directory and calling `File.Sync()` (`atomic.go:118`, `125–131`). On Windows
`File.Sync()` is `syscall.Fsync` → `FlushFileBuffers`
(`internal/poll/fd_fsync_windows.go`), with **no directory special case** in
`os/file_posix.go:163–171`. `FlushFileBuffers` requires `GENERIC_WRITE` on the
handle; `os.Open` gives read access. This is tracked upstream as
[golang/go#75541](https://github.com/golang/go/issues/75541) — *"sync dir on
windows Access is denied"*.

`SyncDir: true` is set on **six production write paths**:

* `internal/auth/store.go:602–609` — the **device token store**. Pairing writes
  through this.
* `internal/session/store.go:72–78` — session metadata and history.
* `internal/providerauth/store.go:153`, `manifest.go:268`, `reconcile.go:295`,
  `transaction.go:453` — the provider-auth transaction log.

Because `WriteFileAtomic` **returns** a `SyncDir` failure by deliberate design
(`atomic.go:46–49`, MADR 0074 D25 — *"a caller that asked for durability must
not be told the write succeeded"*), every one of these writes would return an
error on Windows *after the rename had already landed*. **`mcremote pair` would
report failure on a write that actually succeeded.** That design choice is
correct on POSIX and is exactly what makes Windows fail loudly here.

**F6 — The admin control plane compiles on Windows but its authentication does
not.**

AF_UNIX works: Go's `net` package includes `unixsock_posix.go` on Windows
(`go list -f '{{.GoFiles}}' net`), and `os` sets `ModeSocket` for the
`IO_REPARSE_TAG_AF_UNIX` reparse tag (`os/types_windows.go:209–211`, `257–258`).
So `net.Listen("unix", …)` at `admin.go:112` and the `Lstat`/`ModeSocket` check
at `admin.go:94–97` are viable.

What is not viable is everything around them:

* `admin.go:98,123,139` — `syscall.Stat_t` uid/inode checks do not exist.
* `admin.go:114` — `os.Chmod(socketPath, 0o600)` on Windows only toggles the
  read-only attribute. **It grants no access control.** The package doc's
  premise — *"Auth is filesystem permissions only: the socket is created mode
  0600"* (`admin.go:1–3`) — is false on Windows.
* `appdirs.CheckSocketPathLength` derives its limit from
  `unix.RawSockaddrUnix.Path`; Windows `sockaddr_un` has a different
  (~108-byte) `sun_path` that must be derived separately, not inherited.

Without a replacement, a Windows daemon would expose an **unauthenticated
local control plane** that can disconnect any paired device.

**F7 — Process supervision degrades to "kill immediately, and lie about
liveness".**

* `procutil/procutil_other.go` (`//go:build !unix`) makes `SetProcessGroup` a
  no-op and `TerminateProcessGroup` kill-only, honestly documented as *"no
  portable 'ask nicely' signal"*. On Windows the correct primitive exists:
  **Job Objects** with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, available in
  `x/sys/windows` (`syscall_windows.go:350–351`,
  `types_windows.go:2551,2571`). Without it, killing a provider CLI leaves its
  grandchildren (node, python, git) orphaned.
* `procutil/owner_other.go:29–37` — `OwnerAlive` calls
  `p.Signal(syscall.Signal(0))`. On Windows `os.FindProcess` **always
  succeeds** and `Signal` returns "not supported by windows", so
  **`OwnerAlive` returns `false` for every live process**. The doc already
  warns callers must not authorise destructive action on it off Linux; on
  Windows it is not merely weak, it is inverted.
* `procutil/starttoken_other.go` returns `("", false)`, so engine records carry
  no start token and pid recycling is undetectable.

**F8 — Shutdown never runs on Windows.**

`internal/cli/serve.go:86` and `internal/relay/cli.go:248` both use
`signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. `syscall.SIGTERM`
is *defined* on Windows so this compiles, but Windows never delivers it. A
console `Ctrl+C` maps to `os.Interrupt` and works; a service stop, a
`taskkill`, or a session logoff does not. Under the SCM, stop is
`SERVICE_CONTROL_STOP` delivered through `x/sys/windows/svc`, which the code
does not handle. **Every graceful-drain path — hub teardown, socket removal,
provider termination — would be skipped on a service stop.**

**F9 — Self-update is unsafe on Windows in three distinct ways.**

`internal/update/swap.go`:

* Line 71, `_ = os.Remove(prev)` — deleting a `.prev` that a previous run left
  behind fails on Windows if any process still has it open, and the error is
  discarded. Line 123's `os.Rename(dest, prev)` then fails because `prev`
  exists and cannot be replaced. **[unverified on hardware]**
* Line 130, `os.Chmod(dest, 0o755)` is a no-op for the execute bit on Windows.
* Renaming a *running* `.exe` is permitted on Windows (the classic
  rename-then-replace update), so the ordering at lines 123–127 is
  fundamentally sound — but only if `prev` is guaranteed removable first.

`internal/update/github.go:85–106` — `AssetFor` derives `VER` as
`TrimPrefix(name, "mcremote-windows-amd64-")`. The release job names Windows
assets `mcremote-windows-amd64-<VER>.exe` (`ci.yml`, "Rename to include the
version"), so `publishedVER` would be `"0.14.10.1.exe"`. That value flows into
`NewerPublished(publishedVER, local)` (`run.go:80`) and into
`SumsAsset(publishedVER)` (`run.go:113`). `SumsAsset` happens to survive via its
`HasPrefix("SHA256SUMS")` fallback (`github.go:117–121`), but the version
comparison does not. **Windows self-update would mis-compare versions.**

**F10 — Provider CLIs are npm shims on Windows, and `os/exec` will not run
them.**

Six provider packages resolve an engine binary by name via `exec.LookPath`
(`codex/provider.go:213,1003`, `acpagent/acpagent.go:399`,
`acphttp/provider.go:185`, `httpagent/provider.go:154`). On Windows a
globally-installed npm CLI lands as three files: an extensionless shell script,
`foo.ps1`, and `foo.cmd`.

Go's `lookPath` honours `PATHEXT` (`os/exec/lp_windows.go:113–130`) and would
resolve `foo.cmd` correctly. But `syscall.StartProcess` **always** passes
`argv0` as `lpApplicationName` (`syscall/exec_windows.go:339–341`), and
Microsoft's `CreateProcessW` documentation states plainly:

> To run a batch file, you must start the command interpreter; set
> *lpApplicationName* to cmd.exe and set *lpCommandLine* to the following
> arguments: /c plus the name of the batch file.

So a resolved `.cmd`/`.bat` must be launched through `cmd.exe /c`, and doing so
re-introduces `cmd.exe` metacharacter interpretation over arguments the daemon
builds from session state — the class of injection Go's own toolchain guards
against. The same doc fixes the command line ceiling at **32,767 characters**,
which matters because provider argv can carry prompts and file lists.

**F11 — There is no Windows service story, and the code says so cleanly.**

`internal/cli/service/control.go` returns `service control unsupported on %s`
at lines 47, 64, 107 and 147. `internal/cli/service/setup.go:279–286` refuses
install off Linux/macOS but still previews a systemd unit under `--print-only`.
`deploy/` contains only `systemd/` and `launchd/`. This degradation is honest
and safe; it simply means `setup-service`, `update --restart`, and the
enabled-but-stopped healing in `SwapAndRestart` are inert on Windows.

**F12 — Cross-process locking silently disappears on Windows.**

`internal/auth/filelock_other.go` and `internal/certs/filelock_other.go`
(`//go:build !unix`) are documented no-ops. `internal/fsutil/lock_unix.go` has
no counterpart at all. So on Windows the auth device store, the cert directory,
and the provider-auth transaction log lose **all** cross-process serialisation,
while the CLI and the daemon are explicitly designed to share those files. The
primitive exists — `windows.LockFileEx` / `UnlockFileEx` with
`LOCKFILE_EXCLUSIVE_LOCK` (`x/sys/windows/zsyscall_windows.go:2923,3524`,
`syscall_windows.go:68`).

**F13 — Packaging and CI have no Windows lane and no arm64 assurance.**

* `.github/workflows/ci.yml:165` — `# PLATFORMS="$PLATFORMS windows/amd64"`,
  commented out. `windows/arm64` was never present, and stays absent (D19).
* `scripts/install.sh:73–81` — *"this installer supports Linux and macOS only …
  Windows is not a supported host; use WSL2."*
* `scripts/verify-build-metadata.sh:19–21` — no `linux/arm64`, no `windows/*`.
* No `windows-latest` runner anywhere in CI, so no Windows test has ever run.
* The Makefile already handles `BIN_EXT := .exe` (`Makefile:122–126`) and
  detects MINGW/MSYS/CYGWIN hosts (`Makefile:63–75`), so the scaffolding is
  half-built.
* macOS codesigning is wired (`Makefile:186–194`, MADR 0069 D6) with a
  documented rationale about identity churn. **Windows has the same problem
  with a different mechanism**: SmartScreen and Smart App Control weigh
  publisher reputation, unsigned files must rebuild reputation on every
  release, and only Authenticode certificates from a Windows Root Certificate
  Program CA accumulate it. There is no `signtool` equivalent in the build.

**F14 — Test suite portability is better than expected.**

Of 407 test files, 15 assert POSIX permission bits, 6 use `os.Symlink`, and 5
shell out to `sh`/`bash`. Everything else compiles and vets under
`GOOS=windows`. A Windows CI lane is affordable; it is not a rewrite.

**F15 — Relay-specific Windows notes.**

`mcrelay` prints `sudo setcap 'cap_net_bind_service=+ep'` guidance for ports
below 1024 (`relay/cli.go:465–471`). Windows has **no privileged-port
restriction** — any user may bind 443 — so that copy is wrong there. The real
Windows obstacles are different: `netsh int ipv4 show excludedportrange`
reservations (Hyper-V/WinNAT/Docker Desktop routinely claim wide dynamic
ranges), and the Windows Defender Firewall inbound prompt on first bind.

**F17 — The release job cannot produce an unversioned alias for a Windows
asset. This is a latent bug already in the tree.**

`ci.yml:589–600` renames build outputs to versioned asset names and
**explicitly anticipates Windows**: *"Keep any extension LAST so a Windows
asset stays `mcremote-windows-amd64-<ver>.exe`, not `...exe-<ver>`."* It works
— `mcremote-windows-amd64.exe` becomes `mcremote-windows-amd64-<VER>.exe`.

Thirty lines later the alias loop that MADR 0097 A2 depends on was never
updated to match (`ci.yml:622`):

```bash
for f in mcremote-*-"${VER}" mcrelay-*-"${VER}"; do
  alias_name="${f%-${VER}}"
```

The glob requires the filename to **end** in `${VER}`. A Windows asset ends in
`.exe`, so it never matches, no `mcremote-windows-amd64.exe` alias is
published, and any installer that builds a URL from platform alone gets a 404.

The same `.exe` contamination reaches the installer's version resolution.
`scripts/install.sh:176–190` derives the resolved version by stripping the
platform prefix from the matched `SHA256SUMS` line, which for
`mcremote-windows-amd64-0.14.10.1.exe` yields `"0.14.10.1.exe"` — the identical
defect F9 records in `update/github.go`'s `AssetFor`. **One naming convention is
being parsed by three independent implementations** (the release job, the
installer, and the self-updater), and only the release job's rename step
handles the extension.

**F16 — Some Windows awareness already exists and should be the model.**

`internal/config/config.go:1238`, `internal/provider/codex/config.go:94,116`
and `internal/provider/codex/launch.go:46` already refuse the `unix_ws` and
`managed_daemon_proxy` Codex transports on Windows with clear errors. That is
the right pattern: **refuse explicitly at config-validation time rather than
fail obscurely at runtime.**

**F18 — GitHub now hosts native arm64 runners, free for public repositories.
Emulation is no longer the answer for `linux/arm64`.**

`maccavelli/magic-cli-remote` is **public** (`gh repo view --json visibility`
→ `PUBLIC`), which matters because every arm64 runner below is free there and
billable elsewhere.

| Label | Status | Public-repo spec | Image maintainer |
| --- | --- | --- | --- |
| `ubuntu-24.04-arm` | GA (Aug 2025) | 4 vCPU / 16 GB | Arm Limited (partner image) |
| `ubuntu-22.04-arm` | GA | 4 vCPU / 16 GB | Arm Limited |
| `windows-11-arm` | GA | 4 vCPU / 16 GB | Arm variant of the GitHub image |
| `windows-latest` (Server 2025) | GA | 4 vCPU / 16 GB | GitHub |

arm64 runners reached GA for public repositories in **August 2025** and became
available for private repositories in **January 2026**. This directly retires
the plan's original QEMU step and answers the MADR's first open question: the
`linux/arm64` binary can be **executed natively** in CI rather than emulated.
The `windows-11-arm` row is recorded but **unused** under D19; it is what makes
that deferral reversible on demand.

**F19 — Race-detector support is not uniform across the candidate targets.**

Read from the toolchain, not from documentation —
`internal/platform/supported.go:23–34` in Go 1.26.5:

```go
case "linux":
    return goarch == "amd64" || goarch == "arm64" || ...
case "darwin":
    return goarch == "amd64" || goarch == "arm64"
case "freebsd", "netbsd", "windows":
    return goarch == "amd64"
```

So the *port* supports `-race` on `linux/arm64` and `windows/amd64`, and not
on `windows/arm64`.

**But port support is not the binding constraint — cgo is.** The gate is
`cmd/go/internal/work/init.go:194–204`:

```go
// Note: On macOS, -race does not require cgo. -asan and -msan still do.
if !cfg.BuildContext.CgoEnabled && (cfg.Goos != "darwin" || cfg.BuildASan || cfg.BuildMSan) {
    fmt.Fprintf(os.Stderr, "go: %s requires cgo; enable cgo by setting CGO_ENABLED=1\n", modeFlag)
```

Reproduced on this tree with Go 1.26.5:

```text
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -race ./cmd/mcremote → go: -race requires cgo
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -race ./cmd/mcremote → go: -race requires cgo
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -race ./cmd/mcremote → exit 0
```

**Darwin is the sole exemption**: the race runtime ships prebuilt there, so
`-race` needs no cgo and no external C compiler. Everywhere else `-race`
*forces* `CGO_ENABLED=1`, which D20 refuses. The runner images do carry C
compilers (`windows-latest` GCC 15.2.0 and LLVM 20.1.8; `ubuntu-24.04-arm`
GCC 12/13/14) — that is why enabling cgo there *would* work, and precisely why
the decision to refuse it has to be explicit rather than incidental.

Also from `internal/platform/zosarch.go:112–113`: `windows/amd64` is a
**first-class** port; `windows/arm64` is `CgoSupported` but **not**
first-class. That asymmetry is part of the evidence D19 rests on.

**F20 — A self-hosted runner is not an option for this repository.**

The owner has a Windows laptop available. Registering it as a self-hosted
runner would be a security error, not a shortcut: GitHub's own secure-use
reference states self-hosted runners should almost never be used with public
repositories, because anyone who can fork and open a pull request can execute
code on the runner, and runners are non-ephemeral by default so a compromise
persists between jobs. The repository is public and already accepts fork PRs
(`ci.yml`'s Android signing job reasons explicitly about fork builds).

The laptop is therefore a **manual acceptance host**, never CI infrastructure.

**F21 — The Arm partner images are deliberately narrowed, and one gap bites.**

`ubuntu-24.04-arm` and `windows-11-arm` are not the x64 images recompiled;
they ship a reduced tool set. Nothing this repository's Go jobs need is
missing from the Ubuntu Arm image (gcc and the build tooling are present;
what is absent — Android SDK, Homebrew, CodeQL, browsers — this workflow's Go
job never touches).

`windows-11-arm` is unused under D19, but its divergence is part of why:
**MSYS2 is not available on it**, along with Docker, the Android SDK and
several others. Steps on that runner should therefore be written
`shell: pwsh` rather than `shell: bash`. Its preinstalled Go is 1.24.11
(cached 1.25.5), older than this module's 1.26.5, so `actions/setup-go` with
`go-version-file: go.mod` — which `ci.yml` already uses — is load-bearing
there rather than merely tidy.

**F22 — Nothing guarantees the shipped binaries are cgo-free. The policy is a
default, and defaults are overridable.**

`Makefile:38` sets `CGO_ENABLED ?= 0`, and `Makefile:37` documents the escape
hatch explicitly: *"Override for a cgo build: make build CGO_ENABLED=1."*
`?=` in Make assigns only when the variable is **not already defined**, and an
environment variable counts as defined. Demonstrated:

```text
$ make show                      → make sees CGO_ENABLED=0
$ CGO_ENABLED=1 make show        → make sees CGO_ENABLED=1
```

So a stray `CGO_ENABLED=1` exported anywhere in a shell, a CI runner image, or
a future workflow step silently produces cgo-linked release binaries. The
release job never sets the variable, so today it gets `0` — by luck of a clean
environment, not by construction.

`scripts/verify-build-metadata.sh` does not close this. It sets
`CGO_ENABLED=0` on **its own** throwaway builds (line 15) and then asserts
build *tags*; it never inspects a binary for cgo, and it never inspects the
binaries the release actually uploads. It proves the policy is *achievable*,
not that any shipped artifact *has* it.

The fix is available and cheap, because the toolchain records the answer in
the binary:

```text
$ go version -m mcrelay | grep CGO_ENABLED
	build	CGO_ENABLED=0
```

and `debug/buildinfo` — what `go version -m` uses — parses **ELF, PE, Mach-O
and XCOFF** (`debug/buildinfo/buildinfo.go:126–150`). Verified in practice: a
`linux/arm64` ELF cross-built on this darwin/arm64 host reports
`build CGO_ENABLED=0`, `build -tags=netgo,osusergo`, `GOOS=linux`,
`GOARCH=arm64` when inspected from the Mac. **A Linux release job can
therefore assert cgo-freeness on every artifact it publishes, including the
Windows `.exe` and both Darwin binaries, without executing any of them.**

### Idempotency: what the word has to mean here

The owner asked for pathing that "is idempotent". The current
`EnsurePrivateDir` (`ensure_unix.go:16–30`) is idempotent *and converging*: it
`MkdirAll`s, then validates ownership and mode, and **repairs** a too-permissive
mode via `chmod` before re-checking. Repeated calls converge on the same state
and the second call is a no-op.

There is no equivalent contract expressed anywhere as a rule, and `Resolve`
(`paths.go:29–31`) is documented as *"Pure: no filesystem side effects"* — the
resolution half is already idempotent by construction. The port needs the
*ensure* half to hold that same contract on Windows, where "mode 0700" is not a
thing and the equivalent is an explicit DACL granting the owner SID and
`SYSTEM`, with inheritance disabled.

## Decision Drivers

* **Refusing loudly beats degrading silently.** F6, F7 and F12 all produce a
  daemon that looks healthy while its security invariants are gone. That is
  worse than not shipping a Windows binary.
* **The daemons are not symmetric.** `mcrelay` is a stateless public edge:
  listen, TLS, hub, splice. `mcremote` is a stateful host agent: it spawns
  provider CLIs, holds device tokens, manages services, and self-updates.
  Nearly every hard finding (F5, F7, F9, F10, F11) is `mcremote`'s.
* **Go 1.26.5 supplies the primitives.** `x/sys v0.47.0` is already a direct
  dependency and carries `windows`, `windows/svc`, `windows/svc/mgr`,
  `windows/registry`, Known Folders, `LockFileEx`, SDDL and Job Objects. No new
  dependency is needed.
* **The compile surface is small; the semantic surface is not.** Three shims
  buy a green build. That temptation must be recorded and rejected explicitly.
* **Path layout is a one-way door.** Once a Windows user has state under
  `C:\Users\x\.config\mcremote`, moving it later needs a migration. Get it right
  before the first release.
* **`linux/arm64` costs almost nothing and is already half-shipped.** Closing it
  is cheap and removes a published-but-unverified artifact.
* **Verification requires a real Windows host.** Several findings are marked
  `[unverified]`; a CI runner is the minimum bar for accepting them.

## Considered Options

* **A — Uncomment the matrix.** Add the three shims, publish
  `windows/amd64`, change nothing else.
* **B — Refuse Windows; document WSL2.** Keep `install.sh`'s existing stance,
  add only `linux/arm64` hardening.
* **C — Platform layer with native OS pathing, phased, Windows as a declared
  support tier.** Introduce `_windows.go` implementations behind the existing
  interfaces, give `appdirs` a native-roots provider, fix the durability and
  locking primitives, refuse what cannot be made safe, and gate the release on
  a Windows CI lane.
* **D — Full parity in one pass**, including a Windows Service via the SCM,
  Authenticode signing, an MSI/winget package, and `cmd.exe` shim launching for
  every provider.
* **E — Ship `mcrelay` on Windows only; keep `mcremote` POSIX-only.**

## Decision Outcome

Chosen option: **"C — Platform layer with native OS pathing, phased, Windows as
a declared support tier"**, because it is the only option that satisfies all
four parts of the request without shipping a daemon whose security properties
are quietly absent. A is refuted directly by F5, F6, F7 and F12 — the build
would be green and the product broken. D is the same destination but front-loads
Authenticode procurement, MSI packaging and the `cmd.exe` argument-safety
problem (F10), none of which block a first Windows binary. E is refuted by the
measurement: `mcremote` and `mcrelay` share `appdirs`, `fsutil`, `procutil` and
`admin`, so splitting them saves no work while halving the value.

### The decisions

**D1 — Target set: five targets.** Both daemons build for `linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64` and `windows/amd64`.

`windows/arm64` is **deferred, not rejected** — see D19 for the rationale and
the re-entry cost. The 32-bit `windows/arm` port does not exist to confuse the
matrix either way: Go 1.26 removed it, as announced in the Go 1.25 notes.

**D2 — Build flags become explicitly per-GOOS; Windows gets no tags.**
`Makefile:83–93` gains a `windows` arm setting `GO_TAGS :=` and
`GO_BUILDFLAGS := -trimpath`, matching the Darwin arm. Rationale is F3:
`osusergo` is inert and `netgo` would override the platform resolver Go itself
prefers on Windows. The `else` fallback keeps `netgo,osusergo` for genuinely
static-friendly targets. `scripts/verify-build-metadata.sh` is extended to
build `linux/arm64` and `windows/amd64` and to assert **absence** of
`netgo`/`osusergo` on the Windows target, the same way it already asserts
absence on Darwin.

**D3 — `appdirs` gains a native-roots provider; `Paths` keeps one shape.**
`Resolve` stays pure and OS-agnostic. `SystemRoots` splits into
`roots_unix.go` (today's XDG logic, unchanged) and `roots_windows.go` built on
Known Folders:

| Field | Windows source |
| --- | --- |
| `Home` | `os.UserHomeDir()` (`%USERPROFILE%`) |
| `ConfigHome` | `FOLDERID_RoamingAppData` (`%AppData%`) |
| `DataHome` | `FOLDERID_LocalAppData` |
| `StateHome` | `FOLDERID_LocalAppData` + `\State` |
| `CacheHome` | `FOLDERID_LocalAppData` + `\Cache` |
| `RuntimeHome` | `FOLDERID_LocalAppData` + `\Runtime` |
| `Logs` | `FOLDERID_LocalAppData` + `\Logs` |
| `Temp` | `os.TempDir()` |

`Roots.Logs` stops being populated unconditionally (F4); it becomes
per-platform, empty on Linux. `os.Getuid()` leaves the shared code path
entirely — the Windows runtime root is per-user by construction because
`FOLDERID_LocalAppData` already is, which removes the `-1` collision without
needing a uid at all. The `InstanceKey` SHA-256-of-data-dir scheme
(`paths.go:124`) is unchanged and remains correct on Windows, but hashing input
is lower-cased first on Windows because NTFS is case-insensitive — otherwise
`C:\Users\X` and `c:\users\x` key two different instances of the same
directory.

**Machine scope (owner decision 2026-08-27, resolving open question 3).** The
table above is the interactive-user layout, which is the only one this record
ships, because D12 selects a per-user service. Should a machine-wide service
ever be added, its roots are **`%ProgramData%\<product>`**
(`FOLDERID_ProgramData`) with the D4 owner-only DACL — **not** the service
account's `%LocalAppData%`, which resolves under
`C:\Windows\System32\config\systemprofile\AppData\Local`: opaque, lost to
profile resets, and the last place an operator would look for the pairing
database. Recorded now so the answer is not re-derived under pressure later.

**Path length (owner decision 2026-08-27, resolving open question 4).**
`appdirs` **emits a `Diagnostic` and proceeds** when a resolved Windows layout
is long enough to risk `MAX_PATH`; it does **not** refuse. The mechanism
already exists — `SystemRoots` returns `[]Diagnostic` for
`xdg_relative_ignored` and `xdg_runtime_fallback` — so this is a new code, not
a new channel. Refusing was considered and rejected: Go's `fixLongPath`
(`os/path_windows.go:100–105`) transparently applies the `\\?\` prefix below
the 248-byte threshold, so most `os` calls succeed regardless, and a hard
refusal would block layouts that work. The real exposure is a **provider CLI**
that cannot open a deep path, and a named diagnostic points at that directly.
Note the deliberate asymmetry with `CheckSocketPathLength`, which *does*
refuse: an over-long `sun_path` cannot work at all, whereas an over-long
directory usually can.

**D4 — One idempotency contract, three implementations.** `EnsurePrivateDir`
is specified — in the package doc, not just in prose here — as:

> Idempotent and converging. Creates the directory and any parents if absent;
> if present, verifies owner and access and repairs access to the private
> state; returns an error rather than repairing when the leaf is a symlink,
> reparse point, or not owned by the current principal. A second call on a
> converged directory performs no writes.

On Unix that is today's behaviour, unchanged. On Windows "private" means an
explicit DACL granting the current user SID and `SYSTEM` full control with
inheritance disabled, built with `x/sys/windows` SDDL helpers
(`SecurityDescriptorFromString`, `security_windows.go:1452`), and the leaf is
rejected if `FILE_ATTRIBUTE_REPARSE_POINT` is set — the Windows analogue of
today's symlink refusal. The same contract text is applied to
`ValidateRuntimeDir`.

**D5 — `SyncDir` becomes a platform capability, and Windows durability is
stated, not faked.** `fsutil` grows `syncDir` per-platform: the current
implementation on Unix; on Windows a documented no-op returning `nil`, because
`FlushFileBuffers` on a directory handle is not supported
([golang/go#75541](https://github.com/golang/go/issues/75541)). The choice is
between three bad options and one acceptable one:

* returning the error preserves MADR 0074 D25's honesty but **breaks pairing**
  (F5) — rejected;
* swallowing it inside the caller re-introduces the exact silent-success 0074
  D25 forbade — rejected;
* making `SyncDir` a caller-visible no-op with an explicit
  `docs/`-level statement that **on NTFS the rename is ordered but the
  directory entry is not separately flushed** is accurate, keeps the POSIX
  guarantee intact, and does not lie. Chosen.

This is a genuine reduction in crash-durability on Windows relative to Linux,
and it is recorded as such rather than papered over. `SyncFile` is unaffected —
`FlushFileBuffers` on a *file* handle works normally.

**D6 — Real cross-process locks on Windows.** `fsutil.WithLock` gains
`lock_windows.go` using `LockFileEx` with
`LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY` and the same
poll-until-deadline loop as `lock_unix.go:36–50`. `auth/filelock_other.go` and
`certs/filelock_other.go` are replaced by `_windows.go` implementations
delegating to it; the `!unix` no-ops survive only for genuinely unsupported
platforms and their doc comments are narrowed to say so.

**D7 — The admin control plane is authenticated on Windows or it does not
listen.** AF_UNIX is retained (F6: `net` supports it, `os` reports
`ModeSocket`), and the POSIX uid/inode checks are replaced by a
`admin/owner_windows.go` that reads the socket file's owner SID via
`GetNamedSecurityInfo` and compares it to the process token's user SID; the
`0600` chmod is replaced by creating the *runtime directory* with the D4 DACL,
so the socket inherits an owner-only ACL. If that DACL cannot be established,
**`Serve` returns an error and the daemon starts without an admin socket** —
the same fail-closed posture the package already takes for a
wrong-owner socket (`admin.go:100`). A named pipe with an owner-only SDDL was
considered and deferred: it would fork the transport, and AF_UNIX plus a
correct DACL reaches the same security property with one code path.

**D8 — Job Objects for process supervision.** `procutil` gains
`procutil_windows.go`: `SetProcessGroup` creates a Job Object with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` and assigns the child;
`TerminateProcessGroup` closes the job (killing the whole tree) after a
graceful attempt via `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT)` for children
started with `CREATE_NEW_PROCESS_GROUP`; `KillProcessGroup` terminates the job.
`owner_windows.go` replaces the inverted `Signal(0)` liveness check (F7) with
`OpenProcess` + `GetExitCodeProcess` != `STILL_ACTIVE`, and
`starttoken_windows.go` implements `ProcessStartToken` via
`GetProcessTimes`' creation time, restoring pid-recycle detection to Linux
parity.

**D9 — Shutdown on Windows is `os.Interrupt`, and nothing else pretends
otherwise.** The two `signal.NotifyContext` call sites keep `os.Interrupt`
(console `Ctrl+C`, `CTRL_BREAK_EVENT` and `CTRL_CLOSE_EVENT` map to it) and
drop the never-delivered `syscall.SIGTERM` on Windows via a small per-platform
`shutdownSignals()` helper. The POSIX list is unchanged.

**There is no second trigger.** An earlier draft added `svc.Run` to answer
`SERVICE_CONTROL_STOP`; D12's per-user Task Scheduler model removes the SCM
from the picture entirely, so that path is dropped rather than built untested.
The consequence is explicit: **ending the scheduled task terminates the process
without a graceful drain.** That is survivable by construction, and the reasons
are worth recording so nobody "fixes" it by inventing an admin `stop` op:

* orphaned provider processes are already prevented by D8's Job Object with
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` — the tree dies with the parent handle;
* a left-behind admin socket is already handled by the stale-socket path
  (`admin.go:92–105`), which pings before binding and refuses to steal a live
  control plane;
* dropped WebSocket connections are what the phone's reconnect logic exists
  for.

What is genuinely lost is an orderly hub teardown. Accepted, and stated rather
than papered over.

**D10 — Update swap is made Windows-correct.** `swap.go:71`'s discarded
`os.Remove(prev)` becomes a checked removal that retries briefly and **fails
the swap** if `prev` cannot be cleared, rather than deferring the failure to
line 123. `os.Chmod(dest, 0o755)` at line 130 is skipped on Windows. And
`update/github.go` `AssetFor` strips a trailing `.exe` before returning `VER`,
with a regression test pinning `mcremote-windows-amd64-0.14.10.1.exe →
"0.14.10.1"` (F9).

**D11 — Provider launch handles PATHEXT shims explicitly, or refuses.** A
single `internal/provider/launch` helper resolves an engine binary and
classifies the result: a PE image is executed directly; a `.cmd`/`.bat`
resolution is executed through `cmd.exe /c` **only when every argument passes a
conservative metacharacter allowlist**, and otherwise returns a typed error
telling the operator to point `providers.<name>.bin` at the real executable.
This is the F16 pattern — refuse at the boundary with an actionable message —
applied to the risk Microsoft's own `CreateProcessW` documentation creates.
The 32,767-character command-line ceiling becomes a pre-flight check with a
clear error rather than an opaque `CreateProcess` failure.

**D12 — Windows background execution is a per-user Task Scheduler entry, not a
Windows Service** (owner decision 2026-08-27, resolving open question 2).

`internal/cli/service` gains a `windows` arm that registers a **Task Scheduler
at-logon task running as the interactive user**, requiring no elevation.
`Setup`, `Remove`, `IsActive`, `IsInstalled`, `Start` and `Stop` are
implemented against it, replacing the four
`service control unsupported on windows` returns (F11) for `windows` only.

This keeps Windows consistent with the product rather than with Windows
convention, and product consistency is what matters here:
`internal/cli/service` has **only ever installed user-scope definitions** — a
`systemd --user` unit on Linux, a LaunchAgent in the user domain on macOS,
described in `relay/cli.go` as *"user domain only — no sudo"*. A `LocalSystem`
service would have been the first elevated install path in the project.

Three consequences, all accepted deliberately:

* **No start before logon**, and no run under an unattended account. For
  `mcremote` — a host agent that drives the developer's own CLIs against their
  own files — that is arguably correct rather than merely tolerable. For
  `mcrelay` on a Windows server it is a real limitation, and D15 records it.
* **D3's path table stays exactly as written.** `FOLDERID_LocalAppData`
  resolves to the real user, not a service profile, so open question 3 does not
  bind. This also avoids the one-way door: no user acquires state under a
  system profile that a later change would have to migrate.
* **No SCM integration is built** (D9). `sc.exe create` or NSSM against these
  binaries is unsupported (D15).

**D13 — Packaging, and one asset-naming convention parsed in one place.**
F17 shows the `.exe` suffix is handled by the release job's rename step and by
nothing else. The convention — `<product>-<goos>-<goarch>[-<VER>][.exe]`, with
any extension last — is written down once, the alias loop is corrected to match
it, and the version-extraction logic in `install.sh` and
`update/github.go` is fixed to strip a trailing extension before comparing.
CI gains `windows/amd64` to
`PLATFORMS` (`ci.yml:160–165`), a `windows-latest` job running
`go build ./...` and `go test ./...` for `windows/amd64`, and
`verify-build-metadata` coverage per D2. `scripts/install.ps1` replaces the
current flat refusal (F13) for native Windows and installs to
**`%LocalAppData%\Programs\<product>\`** (owner decision 2026-08-27,
resolving open question 5) — the modern per-user Windows install convention,
needing no elevation, and the direct analogue of the `~/.local/bin` default
`service/setup.go:52` already carries. `service/setup.go`'s default binary
discovery gains the matching Windows branch, so `setup-service` finds the
installed binary rather than only `os.Executable()`. `%ProgramFiles%` was
rejected for the same reason `LocalSystem` was (D12): it would require
elevation to install *and* to update, breaking the no-sudo posture the Linux
and macOS installers deliberately keep; `install.sh`'s WSL2 message
stays for POSIX shells. The `linux/arm64` smoke gap (F2) is closed by running
the arm64 binary **natively on `ubuntu-24.04-arm`** — see D17, which supersedes
this record's earlier QEMU proposal.

**D14 — Authenticode signing is designed in now and procured later.** The
build gains an `MC_WINDOWS_SIGN_*` hook mirroring `MC_CODESIGN_IDENTITY`
(`Makefile:186–194`), unset by default. The release notes state that unsigned
Windows binaries trigger SmartScreen and may be blocked by Smart App Control,
and that reputation accrues only to a certificate from a Windows Root
Certificate Program CA — the same honest posture MADR 0069 D6 takes for macOS
TCC.

**D15 — What is explicitly refused on Windows.** Recorded so a future
maintainer does not read these as oversights: Codex `unix_ws` and
`managed_daemon_proxy` transports remain Unix-only (already refused, F16);
`grok` device auth's `sandbox-exec` path stays Darwin-only
(`grok/device_auth.go:87`); `procutil.SetDeathSignal`'s `PR_SET_PDEATHSIG` has
no Windows equivalent and Job Objects supersede it; `internal/tcc` stays
Darwin-only; **running either binary under the Windows Service Control Manager
(`sc.exe create`, NSSM, or similar) is unsupported** — they do not call
`StartServiceCtrlDispatcher`, so the SCM would kill them at start-up timeout
(D9/D12); **`mcrelay` cannot start before user logon on Windows**, which rules
out the unattended-server deployment its Linux system unit supports; `relay/cli.go:465–471`'s `setcap` guidance is replaced on Windows
with text about excluded port ranges and the Defender Firewall prompt (F15).

**D16 — Support tier.** `windows/amd64` is a **Tier 2** target: built and
unit-tested in CI on every push and tag, but not exercised by the live provider
suites, and shipped with a documented list of unsupported surfaces (D15). Linux
and macOS remain Tier 1. `windows/arm64` is **unsupported** — not built, not
published (D19). This is stated in `README.md` so users learn it before
installing, not after.

**D17 — The CI verification matrix: build everywhere, test natively, never
emulate.** Cross-compilation proves a target *links*; only execution proves it
*works*. Every target this project publishes gets native execution in CI,
which F18 now makes possible at zero cost:

| Job | Runner | What it does | `-race`? |
| --- | --- | --- | --- |
| `go` | `ubuntu-latest` | gofmt, tidy, vet, **two full-suite passes** (D20a), cross-build all five targets, release matrix on tag | yes, on the cgo pass only |
| `go-linux-arm64` | `ubuntu-24.04-arm` | `go build` + `go vet` + full suite, `CGO_ENABLED=0` | **no** — would force cgo (D20) |
| `go-windows` | `windows-latest` | `go build` + `go vet` + full suite, `CGO_ENABLED=0` | **no** — would force cgo (D20) |

Race coverage is not lost, it is **relocated to the one platform that can have
it for free**: darwin, where `-race` runs under `CGO_ENABLED=0` (F19). That is
the owner's development machine and the stability rule's per-phase gate, so
every phase is still race-checked — on the same platform-independent
concurrency code the other targets compile.

Rules that follow from this table:

* **`strategy.fail-fast: false`** wherever a matrix is used, so one platform's
  failure does not hide another's — the whole point of the matrix is to learn
  about all of them in one run.
* The Windows and arm64 jobs run on **pull requests as well as tags**. A build
  target verified only at tag time is verified too late; F17 is precisely the
  bug that shape produces.
* `actions/setup-go` with `go-version-file: go.mod` on every job (F21).
* Every action stays **SHA-pinned**, as `ci.yml` already does throughout —
  the `tj-actions/changed-files` tag-hijack of March 2025 is the standing
  argument, and it applies to any new action this work adds.
* The `release` job's `needs:` grows to include the new jobs, so a tag cannot
  publish an artifact for a platform whose tests did not run.

**D18 — Local verification: native arm64 on the Mac, the Windows laptop by
hand, and no self-hosted runner.**

* **`linux/arm64` on the owner's Apple Silicon Mac** runs *natively*, not
  emulated: Virtualization.framework hosts a real aarch64 Linux VM, so a
  `linux/arm64` container is full-speed. The path is `colima` (free, Lima-based
  — `lima` is already installed on the host, `docker` CLI is present, and no
  daemon is currently running) started as `colima start --arch aarch64
  --vm-type vz`, then the suite inside `golang:1.26.5`. QEMU is not involved
  and should not be introduced.
* **`windows/amd64` on the owner's Windows laptop** runs the suite natively.
  It is a **manual acceptance host only** — F20 forbids registering it as a
  self-hosted runner while this repository is public, and that is a hard rule,
  not a preference.
* **`windows/arm64` is not built or tested anywhere** under this record (D19).
  Its absence of a local host was one of the inputs to that decision, not a
  gap left open by it.
* `act` and similar local CI emulators run Linux containers only and cannot
  reproduce the Windows jobs; local verification is the commands themselves,
  not a simulated workflow.

**D19 — `windows/arm64` is deferred, and the deferral is cheap by
construction.** The owner narrowed scope on 2026-08-27. Recorded here with its
reasons, so a future maintainer reads a decision rather than an oversight.

Why deferring costs almost nothing:

* **No code in this record is architecture-specific.** Every file the work adds
  is `//go:build windows`, which says nothing about `GOARCH`. The measured
  evidence above shows the tree already cross-compiles clean for
  `windows/arm64` once the `GOOS` gaps are closed. Re-entry is therefore a
  `PLATFORMS` line, a `verify-build-metadata` case, and a CI matrix leg —
  **no new source files and no new decisions.**
* The five-target set still covers every machine the project is actually asked
  to run on today.

Why deferring is the right call now rather than later:

* **`windows/arm64` is not a first-class Go port.** `zosarch.go:112–113` marks
  `windows/amd64` `FirstClass: true` and `windows/arm64` `CgoSupported` only.
  Shipping a Tier-2 platform on a non-first-class port doubles the unknowns.
  (An earlier draft also cited its lack of race-detector support. D20 removes
  that argument — under a `CGO_ENABLED=0` policy **no** non-darwin leg runs
  `-race`, so `windows/arm64` is no worse than the others on that axis. The
  three reasons below stand on their own.)
* **There is no local host for it** (D18). `windows/amd64` has the owner's
  laptop; `windows/arm64` would be verifiable only by pushing.
* **Its runner image is the most divergent** (F21): no MSYS2, no Docker, an
  older preinstalled Go. That is a second set of CI-authoring quirks to absorb
  in the same pass that introduces Windows at all.

What re-entry looks like, when it is wanted: add `windows/arm64` to
`PLATFORMS`, add a `build_one windows arm64 ""` case to
`verify-build-metadata.sh`, and add one `windows-11-arm` matrix leg with
`shell: pwsh` and no `-race`. F18, F19 and F21 already record everything that
leg needs. That is a follow-up MADR only if something surprising surfaces;
otherwise it is a plan amendment.

**D20 — `CGO_ENABLED=0` everywhere, for builds *and* tests. No exceptions
introduced by this record.** The owner's constraint, and it is the right one
for reasons beyond preference.

The rule:

* Every build this record adds or changes sets `CGO_ENABLED=0`. That is
  already the Makefile's default (`Makefile:38`) and this record does not
  touch it.
* Every **test** invocation this record adds sets `CGO_ENABLED=0` too. This is
  the part that is new, and it is the part that has a cost: `-race` forces
  cgo off darwin (F19), so the `ubuntu-24.04-arm` and `windows-latest` legs
  run plain `go test`.
* `-race` **is** used where it costs nothing: on darwin, under
  `CGO_ENABLED=0`, which is the per-phase gate in the plan's stability rule
  and the owner's local machine.

Why this is more than a preference. Shipped binaries are `CGO_ENABLED=0`
with `netgo,osusergo` on Linux (`Makefile:86–88`). Enabling cgo changes which
implementation the test binary links, measurably:

```text
GOOS=linux CGO_ENABLED=1 go list -f '{{.GoFiles}}' os/user
  → [cgo_listgroups_unix.go cgo_lookup_unix.go lookup.go user.go]  (+ cgo files)
GOOS=linux CGO_ENABLED=0 go list -f '{{.GoFiles}}' os/user
  → [listgroups_unix.go lookup.go lookup_stubs.go lookup_unix.go user.go]
```

So `os/user` under cgo resolves through **libc**, while the shipped binary
parses `/etc/passwd` in pure Go. For DNS the effect is narrower than it first
appears and worth stating precisely: `net/conf.go`'s `goosPrefersCgo()` names
only windows, plan9, darwin, ios and android — **not linux** — so on Linux a
cgo-enabled binary does not automatically prefer the system resolver. What it
does gain is the *option*: `cgoAvailable` becomes true, so `/etc/nsswitch.conf`
and `LOCALDOMAIN`/`RES_OPTIONS` can route lookups to libc, which the
`netgo`-tagged artifact can never do.

The net of it: a cgo-enabled test run links a different `os/user` than
production and a `net` whose resolver selection has a branch production
does not. Testing the paths the product actually ships is worth more than race
coverage on a third and fourth platform, particularly when the concurrency
under test is platform-independent Go that darwin already races.

What this costs, stated plainly: a data race that manifests only under
`linux/arm64` or `windows/amd64` scheduling, and never under darwin, would not
be caught automatically. That is a real gap, not a theoretical one. It is
accepted because closing it requires shipping a test binary built against a
different resolver stack than production, which trades a known blind spot for
an unknown one.

**D20a — the pre-existing `ubuntu-latest` race job is kept *and* joined by a
cgo-free pass (owner decision 2026-08-27, options A + B).** This resolves what
was open question 0.

`ci.yml:108` runs `go test -race ./...` on `ubuntu-latest` and predates this
record. It implies `CGO_ENABLED=1` for that test binary. The decision is to
keep it **and** add a second pass in the same job:

```yaml
- name: Test (race, all packages)          # existing, unchanged
  run: go test -race ./...                 # implies CGO_ENABLED=1
- name: Test (cgo-free, all packages)      # new
  run: CGO_ENABLED=0 go test ./...         # the stack users actually ship
```

Why both rather than either:

* **Keeping the race pass** preserves the project's only automated race
  coverage on Linux. It costs nothing in artifact terms — a test binary is
  never published, and D21 asserts cgo-freeness on the artifacts themselves,
  so no shipped file is affected by how tests are linked.
* **Adding the cgo-free pass** closes the parity gap D20 identifies: without
  it, `linux/amd64` — the most-used target — would be the one platform whose
  tests never exercise the pure-Go `os/user` and `netgo` paths its own release
  binary is built with.
* Neither alone is sufficient: (A) alone leaves `linux/amd64` tested only
  against libc; (B) alone (converting the job) would delete race coverage
  outright.

Note the new `go-native` matrix already gives Linux a cgo-free pass on
`ubuntu-24.04-arm`, so this addition is specifically about **amd64**, where
scheduling and code paths can differ from arm64.

The cost is one extra full-suite run on the busiest job. That is the price of
testing both the implementation users receive and the one the race detector
requires, and it is paid on a free public runner.

**D21 — Every published binary is cgo-free, and that is asserted on the
artifact, not assumed from the recipe.** The owner's requirement. F22 shows
today's `CGO_ENABLED ?= 0` is a default a stray environment variable defeats,
and that nothing anywhere inspects a shipped binary for it.

Three layers, outermost first, because the outermost is the only one that
cannot be bypassed:

1. **Artifact assertion (the guarantee).** Immediately before upload, the
   release job runs `go version -m` over every file in `dist/` and **fails the
   release** unless each reports `build CGO_ENABLED=0`. This holds no matter
   how the binary was produced — make, a direct `go build`, a future workflow
   rewrite, or a hand-built artifact someone dropped in. It is the only check
   that survives a change to the build system.
2. **Policy verification.** `scripts/verify-build-metadata.sh` gains the same
   `CGO_ENABLED=0` assertion alongside its existing tag assertions, for all
   five targets, so the rule is enforced locally and in `make preflight` —
   not only at tag time.
3. **Build-recipe guard (defence in depth).** The release-producing targets
   refuse to run with a non-zero `CGO_ENABLED` — a **hard error naming the
   variable**, not a silent `override`. A silently-ignored override is worse
   than the hole it patches: the operator believes they built something they
   did not.

The `make build CGO_ENABLED=1` escape hatch at `Makefile:37` is therefore
withdrawn for the binaries this project ships. `make debug` keeps its
flexibility — those binaries are explicitly never shipped
(`Makefile:165–174`) — and any genuine future need for a cgo build becomes a
deliberate, separate target rather than an environment variable that happens
to leak.

Note this is strictly stronger than D20 and independent of it: D20 governs how
**tests** are run, D21 governs what **ships**. D20 could be revisited without
touching D21, and D21 holds even if `-race` (and therefore cgo) is ever
reinstated in a test job, because a test binary is not an artifact.

### Consequences

* Good, because every finding that would produce a *silently* insecure daemon
  (F5, F6, F7, F12) is closed by a real implementation or a fail-closed
  refusal, never by a no-op.
* Good, because pathing follows Known Folders on Windows and XDG on Unix while
  `Resolve` stays a single pure function — the layout is native without
  forking the model.
* Good, because the idempotency contract (D4) is written into the package doc
  and applies identically on all three platforms, which is what makes repeated
  `serve`/`pair`/`setup-service` runs safe everywhere.
* Good, because `linux/arm64` stops being a published-but-unverified artifact.
* Good, because F17 — a latent release-job bug that would have broken the
  first Windows release regardless of this work — is found and fixed before it
  can ship.
* Good, because no new module dependency is required: `x/sys v0.47.0` already
  carries Known Folders, SDDL, `LockFileEx`, Job Objects and `svc`.
* Bad, because crash durability on Windows is genuinely weaker than on POSIX
  (D5) and no amount of code fixes that — it is an upstream platform gap.
* Bad, because the OS-specific file count grows materially: `appdirs`,
  `fsutil`, `admin`, `procutil`, `auth`, `certs` and `cli/service` each gain a
  `_windows.go`, and every one needs a Windows CI lane to stay honest.
* Good, because D12 keeps Windows on the same no-elevation, user-scope install
  posture as Linux and macOS — no part of this work introduces the project's
  first `sudo`-equivalent.
* Bad, because D12 means `mcrelay` on Windows cannot start before user logon,
  which is exactly the unattended-server deployment its Linux system unit
  supports. Windows is not a server platform for `mcrelay` under this record.
* Bad, because D9's dropped SCM path means ending the scheduled task kills the
  daemon without a hub drain; D8's Job Object and the stale-socket logic make
  that survivable, but it is not orderly.
* Bad, because D11's `cmd.exe /c` allowlist will reject some legitimate
  arguments, pushing operators to configure an explicit `bin` path.
* Neutral, because Windows has no privileged-port restriction, so `mcrelay`
  binding 443 is *easier* there — but excluded port ranges and the firewall
  prompt replace that friction rather than remove it.
* Good, because deferring `windows/arm64` (D19) drops a non-first-class port
  with no local acceptance host, without deferring any code — every file added
  here is `//go:build windows` and arch-agnostic.
* Good, because D20a leaves `linux/amd64` tested twice over — once with the
  race detector, once against the exact pure-Go `os/user` and `netgo` stack
  its release binary ships — so neither coverage is traded for the other.
* Bad, because D20a doubles the full-suite runtime of the busiest CI job.
* Good, because D21 turns "our binaries are cgo-free" from a Makefile default
  that a stray environment variable defeats (F22) into an assertion on the
  published artifact that no build-system change can bypass.
* Good, because D20 keeps every test on the same pure-Go `net` and `os/user`
  implementations the shipped binaries use, so CI cannot go green against a
  resolver stack users never run.
* Bad, because D21 withdraws the documented `make build CGO_ENABLED=1` escape
  hatch for shipped binaries; a future cgo need becomes a new target and a new
  decision rather than an environment variable.
* Bad, because D20 gives up automatic race detection on `linux/arm64` and
  `windows/amd64`: a race that only manifests under those schedulers, and not
  under darwin, is not caught by any job.
* Good, because D17 executes every published target natively in CI at no
  cost, so no platform ships on cross-compilation alone — and D18 keeps the
  owner's Windows laptop out of the CI trust boundary (F20).
* Neutral, because several findings (F3's resolver impact, F9's `.prev`
  removal) are read from source and documentation and are marked
  `[unverified]`; the Windows CI lane in D13 is what converts them to facts.

### Confirmation

The decision is confirmed when all of the following hold:

1. `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...` and
   `GOOS=linux GOARCH=arm64 go build ./...` — both exit 0 with **no shims in
   the tree**.
2. `GOOS=windows GOARCH=amd64 go vet ./...` exits 0 (it already does with
   shims; it must without them).
3. Three CI jobs are green per D17: `ubuntu-latest`, `ubuntu-24.04-arm` and
   `windows-latest`. The two new legs run `CGO_ENABLED=0 go test ./...` —
   **no `-race`**, because it would force cgo (D20/F19) — or every skipped test
   carries an explicit `t.Skip` naming the platform reason.
   `CGO_ENABLED=0 go test -race ./...` passes on darwin, where the exemption
   makes race coverage free.
3a. The `ubuntu-latest` `go` job runs **both** passes per D20a — `go test
   -race ./...` and `CGO_ENABLED=0 go test ./...` — and both are green.
4. The `linux/arm64` and `windows/amd64` binaries each report the expected
   version when **executed on their native runner**, closing F2's smoke gap
   with no emulation anywhere in the workflow.
5. `make verify-build-metadata` builds all five targets and asserts:
   `linux/*` has `netgo` **and** `osusergo`; `darwin/*` and `windows/amd64`
   have **neither**. It must **not** build `windows/arm64` (D19).
6. `appdirs` tests assert the Known Folders layout on Windows and the XDG
   layout on Unix from the same `Resolve` call, and an `EnsurePrivateDir`
   idempotency test proves a second call performs no writes and converges a
   deliberately over-permissive directory on both platforms.
7. `mcremote pair create` succeeds on Windows — the direct regression test for
   F5.
8. `admin.Serve` on Windows either binds a socket whose owner SID matches the
   process token, or returns an error; a test proves the fail-closed branch.
9. A `procutil` test proves a spawned grandchild is killed when the job closes,
   and that `OwnerAlive` returns true for a live process (F7's inversion).
10. `AssetFor` returns `"0.14.10.1"` for
   `mcremote-windows-amd64-0.14.10.1.exe` (F9).
11. A tag build publishes `mcremote`/`mcrelay` for all five targets with
    matching `SHA256SUMS` entries and working unversioned aliases.
12. `mcremote setup-service` on Windows registers a Task Scheduler at-logon
    task **without elevation**, a second run is a no-op (the C2 idempotency
    contract at the service layer), and `IsActive`/`Stop`/`Start` drive it
    (D12). No code path calls `StartServiceCtrlDispatcher`.
13. **Every file the release uploads reports `build CGO_ENABLED=0` under
    `go version -m`**, asserted in the release job before upload, and the same
    assertion passes in `make verify-build-metadata` for all five targets
    (D21). A deliberately cgo-linked binary placed in `dist/` must fail the
    release.

## Pros and Cons of the Options

### A — Uncomment the matrix

* Good, because it is roughly a day's work and produces downloadable Windows
  binaries immediately.
* Good, because it needs no new tests and no Windows runner.
* Bad, because F5 makes `mcremote pair` return an error on every device
  registration.
* Bad, because F6 ships an **unauthenticated** local control plane that can
  disconnect paired devices — `os.Chmod(0o600)` grants nothing on Windows.
* Bad, because F12 removes cross-process locking between the CLI and the
  daemon on files they are designed to share.
* Bad, because F7 leaves orphaned provider grandchildren and an `OwnerAlive`
  that is false for every live process.
* Bad, because F3 forces a resolver that discards gateway-less adapters.
* Bad, because F4 writes state to `%USERPROFILE%\.config` and a
  `-1`-suffixed shared-temp runtime directory, creating a migration debt and a
  multi-user collision.

### B — Refuse Windows; document WSL2

* Good, because it is honest, costs nothing, and matches
  `scripts/install.sh:78–80` today.
* Good, because WSL2 genuinely works — the Linux binaries are already built.
* Bad, because it does not answer the request.
* Bad, because WSL2 cannot supervise Windows-native provider CLIs, so a user
  whose `codex`/`grok`/`kilo` install is a Windows npm shim gains nothing.
* Bad, because it leaves F2's `linux/arm64` verification gap open regardless.

### C — Platform layer with native OS pathing, phased (chosen)

* Good, because it closes every silent-failure finding with either an
  implementation or an explicit refusal.
* Good, because it reuses the abstraction seams the codebase already has:
  build-tagged files, `installOS`, `Roots` injection, the `fileOps` seam in
  `fsutil`, and the `ServiceControl` interface.
* Good, because it uses only primitives already vendored in `x/sys v0.47.0`.
* Good, because F14 shows the test suite already compiles and vets under
  `GOOS=windows`, so the CI lane is affordable.
* Neutral, because Windows lands as Tier 2 — built and unit-tested, not
  live-tested against provider CLIs.
* Bad, because it is several phases of work, not one.
* Bad, because it permanently increases the per-platform file count and the CI
  matrix.

### D — Full parity in one pass

* Good, because it would deliver the best Windows experience: a real service,
  a signed binary, a winget package.
* Bad, because Authenticode procurement is an external dependency with a
  purchase and identity-validation lead time, and D14 shows signing alone does
  not grant SmartScreen trust — reputation accrues over releases.
* Bad, because F10's `cmd.exe` argument-safety problem is genuinely hard and
  would block the whole port behind it.
* Bad, because it delays closing F5 and F6, which are the actual correctness
  bugs.

### E — `mcrelay` on Windows only

* Good, because `mcrelay` is the simpler daemon: no provider spawning (F10),
  no self-update service restart (F9/F11), no device token store (F5's worst
  case).
* Good, because `mcrelay` is the piece most likely to run on a Windows server.
* Bad, because both daemons share `appdirs`, `fsutil` and `admin`, so F1, F4,
  F5, F6 and F12 must be solved anyway — the saving is illusory.
* Bad, because `mcrelay`'s value is serving `mcremote` hosts, and the natural
  Windows user wants the *host* agent.
* Bad, because a split target list contradicts `ci.yml:157`'s existing
  invariant — *"the SAME set for both daemons"*.

## More Information

### Evidence index

| Ref | Claim | Where |
| --- | --- | --- |
| F1 | 3 missing symbol groups block the Windows build | `appdirs/ensure_unix.go`, `appdirs/runtime_unix.go`, `fsutil/lock_unix.go`, `admin/admin.go:98,123,139` |
| F2 | `linux/arm64` built but unverified | `ci.yml:162`, `scripts/verify-build-metadata.sh:19–21` |
| F3 | `netgo` forces the pure-Go resolver on Windows | `Makefile:89–93`; Go `net/conf.go:87,142–145,165–176`, `net/dnsconfig_windows.go` |
| F4 | XDG roots degenerate on Windows | `appdirs/roots.go:60–99`; Go `os/file.go:504–512,557–565,608–609`, `syscall/syscall_windows.go:1352` |
| F5 | `SyncDir` fails on Windows | `fsutil/atomic.go:118,125–131`; `auth/store.go:602–609`; `session/store.go:72–78`; `providerauth/{store.go:153,manifest.go:268,reconcile.go:295,transaction.go:453}`; [golang/go#75541](https://github.com/golang/go/issues/75541) |
| F6 | Admin socket auth is POSIX-only | `admin/admin.go:1–3,81–139,112–114`; Go `os/types_windows.go:209–211,257–258` |
| F7 | Process supervision degrades and inverts | `procutil/procutil_other.go`, `procutil/owner_other.go:29–37`, `procutil/starttoken_other.go` |
| F8 | `SIGTERM` never delivered on Windows | `cli/serve.go:86`, `relay/cli.go:248` |
| F9 | Update swap and VER parsing are Windows-unsafe | `update/swap.go:71,123,127,130`, `update/github.go:85–106`, `update/run.go:67,80,113` |
| F10 | npm shims need `cmd.exe`; Go passes `lpApplicationName` | Go `os/exec/lp_windows.go:113–130`, `syscall/exec_windows.go:339–341`; [CreateProcessW docs](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw) |
| F11 | No Windows service path | `cli/service/control.go:47,64,107,147`, `cli/service/setup.go:279–286`, `deploy/` |
| F12 | Locking silently disabled | `auth/filelock_other.go`, `certs/filelock_other.go`, `fsutil/lock_unix.go` |
| F13 | Packaging has no Windows lane | `ci.yml:165`, `scripts/install.sh:73–81`, `Makefile:122–126,186–194` |
| F14 | 15/407 test files assert POSIX perms; tree vets under `GOOS=windows` | measured |
| F15 | `setcap` guidance is wrong on Windows | `relay/cli.go:465–471` |
| F18 | Native arm64 GitHub runners are GA and free for public repos | `gh repo view` → PUBLIC; [arm64 GA changelog](https://github.blog/changelog/2025-08-07-arm64-hosted-runners-for-public-repositories-are-now-generally-available/); [runner reference](https://docs.github.com/en/actions/reference/github-hosted-runners-reference) |
| F19 | `-race` forces `CGO_ENABLED=1` off darwin; darwin exempt; `windows/arm64` is not a race port | Go `cmd/go/internal/work/init.go:194–204` (reproduced), `internal/platform/supported.go:23–34`, `zosarch.go:112–113` |
| F20 | Self-hosted runners must not serve a public repo | [GitHub secure-use reference](https://docs.github.com/en/actions/reference/security/secure-use) |
| F21 | Arm partner images are narrowed; `windows-11-arm` has no MSYS2 (a D19 input) | [partner-runner-images](https://github.com/actions/partner-runner-images) image READMEs |
| F22 | cgo=0 is an overridable default; no artifact is ever checked for it | `Makefile:37–38` (env-override reproduced), `scripts/verify-build-metadata.sh:15`, `debug/buildinfo/buildinfo.go:126–150` |
| F17 | Windows assets get no unversioned alias; `.exe` leaks into resolved versions | `ci.yml:589–600,622–630`, `scripts/install.sh:176–190`, `update/github.go:85–106` |
| F16 | Existing Windows refusals are the model | `config/config.go:1238`, `codex/config.go:94,116`, `codex/launch.go:46` |

### Related records

* [0059](./0059-MADR-native-paths-and-linux-macos-parity.md) — established the XDG layout this record
  extends rather than replaces.
* [0060](./0060-MADR-local-unsigned-build-and-install.md) — `-trimpath`, commit-timestamp
  `DATE`, and the `check-host-target` install guard (`Makefile:245–250`), all
  of which already work for cross-compilation.
* [0065](./0065-MADR-update-automation.md) — `SwapAndRestart` and `AssetFor`, both
  amended in fact by F9.
* [0069](./0069-MADR-macos-permissions-and-sandbox-parity.md) — the macOS codesigning posture D14
  mirrors for Authenticode.
* [0074](./0074-MADR-remote-provider-auth-from-phone.md) — D25's rule that a
  requested durability failure must be reported, which is what makes F5 a hard
  failure rather than a silent one.
* [0097](./0097-MADR-linux-curl-installer.md) / [0104](./0104-MADR-installer-linux-and-macos.md)
  — the unversioned-alias asset scheme `install.ps1` must reuse.
* [0115](./0115-MADR-mcrelay-go126-audit-and-hardening.md) — the most recent
  Go 1.26 audit of `mcrelay`; this record extends its scope to platform
  portability.

### External references

* [Go 1.26 Release Notes](https://go.dev/doc/go1.26) — `windows/arm64` internal
  linking of cgo programs (context for D19's eventual re-entry); removal of the 32-bit `windows/arm` port; `os.OpenFile`
  accepting Windows-specific file flags; `os.Process.WithHandle`.
* [golang/go#75541 — sync dir on windows Access is denied](https://github.com/golang/go/issues/75541)
* [CreateProcessW — Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw)
  — batch files require `cmd.exe /c`; 32,767-character command-line limit.
* [`golang.org/x/sys/windows/svc`](https://pkg.go.dev/golang.org/x/sys/windows/svc)
  and [`svc/mgr`](https://pkg.go.dev/golang.org/x/sys/windows/svc/mgr) — SCM
  integration for D9 and D12.
* [SmartScreen reputation for Windows app developers — Microsoft Learn](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)
  — the basis for D14.

### Open questions for the plan

0. ~~**What happens to the pre-existing `go test -race ./...` on
   `ubuntu-latest`?**~~ **Resolved 2026-08-27 by the owner: options A + B.**
   The race pass is kept and a `CGO_ENABLED=0 go test ./...` pass is added
   beside it. Recorded as **D20a**; it expands this work's scope to include
   `ci.yml`'s existing `go` job, which the plan had previously fenced off.

0a. **When does `windows/arm64` come back?** D19 defers it and records the
   re-entry recipe. The trigger is a user asking for it or Windows on Arm
   becoming a first-class Go port — not a date.

1. ~~**`linux/arm64` runtime verification** — QEMU, a self-hosted arm64
   runner, or SHA-only?~~ **Resolved 2026-08-26 by F18/D17:** none of the
   three. `ubuntu-24.04-arm` is GA and free for public repositories, so the
   arm64 binary is executed natively in CI. QEMU is not introduced and no
   self-hosted runner is registered (F20).
2. ~~**Windows service scope**~~ **Resolved 2026-08-27: per-user Task
   Scheduler at-logon, no elevation.** Recorded as **D12**, which replaces the
   `LocalSystem` service the earlier draft assumed. Knock-on: **D9** drops
   `svc.Run` and states the ungraceful-stop consequence; **D15** records SCM
   execution and pre-logon `mcrelay` start as unsupported.
3. ~~**`%ProgramData%` for a machine-wide `mcrelay`**~~ **Resolved 2026-08-27:
   `%ProgramData%\<product>` with the D4 DACL, if a machine-wide service is
   ever built.** Does not bind today — D12's per-user model keeps D3's table
   as written. Recorded in **D3** so it is not re-derived later.
4. ~~**Path-length policy**~~ **Resolved 2026-08-27: emit a `Diagnostic`,
   do not refuse.** Recorded in **D3**, reusing the `[]Diagnostic` channel
   `SystemRoots` already returns.
5. ~~**Where does an installed Windows binary live?**~~ **Resolved
   2026-08-27: `%LocalAppData%\Programs\<product>\`.** Recorded in **D13**;
   `service/setup.go`'s default binary discovery gains the matching Windows
   branch.

**All open questions are now resolved.** What remains outstanding is not a
decision but a verification: the `[unverified]` findings (F3's resolver
impact, F9's `.prev` removal) become facts on the first Windows CI run.
