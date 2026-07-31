# MADR 0059: Native paths and Linux/macOS functional parity

- **Status**: Proposed
- **Date**: 2026-07-31
- **Deciders**: Project Owner
- **Scope**: `mcremote` and `mcrelay` path resolution, runtime files,
  subprocess lifecycle, DNS behavior, service environments, build validation,
  release packaging, and migration on Linux and macOS.
- **Related**:
  [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md),
  [0058-PLAN-macos-launchd-service-implementation.md](0058-PLAN-macos-launchd-service-implementation.md),
  [0012-MADR-mcremote-daemon-assessment-action-plan.md](0012-MADR-mcremote-daemon-assessment-action-plan.md),
  [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md).
- **Supersedes if accepted**: The XDG-on-macOS portions of 0058 decisions D7
  and D8. The LaunchAgent design, no-sudo boundary, and modern `launchctl`
  decisions in 0058 remain unchanged.

## Decision summary

Adopt one typed application-directory resolver for both binaries. It will use
Go's OS-native user directories, distinguish configuration, durable data,
state, cache, runtime IPC, temporary work, and logs, and keep resolution free
of filesystem side effects. Linux follows the XDG Base Directory
Specification. macOS follows `~/Library` conventions through Go's
`os.UserConfigDir` and `os.UserCacheDir`, with application identifiers beneath
those roots.

`XDG_*` variables remain Linux inputs. They will not be synthesized inside a
macOS LaunchAgent. Cross-platform explicit overrides remain the product-specific
flags and environment variables (`--config`, `MCREMOTE_CONFIG`,
`MCREMOTE_DATA_DIR`, `MCRELAY_CONFIG`, and `MCRELAY_DATA_DIR`). Relative base
directory variables and service paths will be rejected; relative paths inside
configuration will be resolved against a documented, stable base rather than
the caller's current working directory.

An idempotent, conflict-detecting migration command will move existing macOS
XDG-layout installations to native paths. It will never silently merge two
populated installations or treat ACME material as disposable cache.

Functional parity also requires fixes outside path resolution:

1. Do not force Go's pure resolver with the `netgo` build tag on Darwin.
2. Replace Linux-only `/proc` engine discovery with a cross-platform runtime
   registry whose process identity is verified using an OS-specific start
   token.
3. Run native macOS CI, publish both Darwin architectures, and sign and
   notarize release artifacts.
4. Define parity as equal supported outcomes, with two explicit platform
   exceptions: a user LaunchAgent cannot survive logout, and a non-privileged
   service cannot directly bind privileged ports. Literal parity for either
   would require expanding the accepted no-root scope.

This assessment found that all four Darwin binaries cross-compile, but the
repository does **not** yet provide 100% functional parity. Cross-compilation
is necessary evidence, not a macOS runtime test.

## Context

The repository already has a substantial macOS LaunchAgent implementation.
The remaining portability problem is deeper than `runtime.GOOS` dispatch:
paths become API and data contracts. A default changed after installation can
make a service start with an empty configuration, create a second identity
store, lose pairing state, request replacement certificates, or launch provider
processes with a different configuration than the foreground command.

The desired properties are:

- native defaults on each operating system;
- exactly one meaning for every directory class;
- deterministic and side-effect-free resolution;
- creation that is safe to repeat;
- explicit migration of old layouts;
- foreground and service execution resolving the same files;
- equal mcremote/mcrelay features on Linux and macOS, except where the selected
  operating-system service model makes that impossible.

## Research baseline

The following authorities establish the target behavior.

### Go standard library

[`os.UserHomeDir`](https://pkg.go.dev/os#UserHomeDir) uses `$HOME` on Unix,
including macOS. [`os.UserConfigDir`](https://pkg.go.dev/os#UserConfigDir)
returns `$XDG_CONFIG_HOME` or `$HOME/.config` on Unix, but
`$HOME/Library/Application Support` on Darwin. It rejects a relative XDG base.
[`os.UserCacheDir`](https://pkg.go.dev/os#UserCacheDir) similarly returns the
XDG cache root on Linux and `$HOME/Library/Caches` on Darwin.

[`os.TempDir`](https://pkg.go.dev/os#TempDir) returns `$TMPDIR` on Unix, falling
back to `/tmp`, but does not guarantee the directory exists or is accessible.
[`os.CreateTemp`](https://pkg.go.dev/os#CreateTemp) and
[`os.MkdirTemp`](https://pkg.go.dev/os#MkdirTemp) provide unique names with safe
initial modes. The caller remains responsible for cleanup.

Go has no `UserDataDir`, `UserStateDir`, or `UserRuntimeDir`; the application
must define those classes from platform standards.

### Linux/XDG

The [XDG Base Directory Specification
0.8](https://specifications.freedesktop.org/basedir/0.8/) separates config,
data, state, cache, and runtime files. All `$XDG_*_HOME` values must be absolute;
relative values are invalid and must be ignored. `$XDG_RUNTIME_DIR` has stronger
requirements than the other roots: it must be owned by the user, mode `0700`,
local, and scoped to the login lifetime. If it is absent, an application may
use a replacement only with an appropriate warning and equivalent security.

### macOS

Apple's [File System Programming Guide: The Library
Directory](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/MacOSXDirectories/MacOSXDirectories.html)
places application support files in `Library/Application Support`, disposable
cache in `Library/Caches`, and logs in `Library/Logs`. Apple's [Locating Items
in the Standard
Directories](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/AccessingFilesandDirectories/AccessingFilesandDirectories.html)
also directs applications to the appropriate domain rather than assembling a
home-relative path by convention.

Apple's [Secure Coding Guide on race
conditions](https://developer.apple.com/library/archive/documentation/Security/Conceptual/SecureCodingGuide/Articles/RaceConditions.html)
recommends private temporary directories and unique temporary files and warns
about symlink, time-of-check/time-of-use, and case-sensitivity assumptions.
These concerns apply to config, tokens, certificates, and the admin socket.

For services, the [launchd programming
guide](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html)
and [`launchd.plist(5)`](https://manp.gs/mac/5/launchd.plist) define working
directory, environment, output paths, and `Umask`. A per-user LaunchAgent is
tied to its user session; it is not a boot-persistent daemon.

## Current implementation trace

### Directory resolution

| Concern | Current code | Current behavior | Assessment |
|---|---|---|---|
| Config | [`internal/xdg/dirs.go`](../internal/xdg/dirs.go) | `$XDG_CONFIG_HOME/<app>`, else `$HOME/.config/<app>` on every OS | Correct Linux default; non-native macOS default; accepts invalid relative XDG values |
| Data | [`internal/xdg/dirs.go`](../internal/xdg/dirs.go) | `$XDG_DATA_HOME/<app>`, else `$HOME/.local/share/<app>` on every OS | Correct Linux default; non-native macOS default |
| Cache | [`internal/cli/service/setup.go`](../internal/cli/service/setup.go) | Duplicated helper used for service environment | No shared cache contract; non-native macOS value |
| State | none | State is mixed into data | No semantic contract |
| Runtime | none | Admin socket is `<data_dir>/admin.sock` | Ephemeral IPC is mixed into durable data |
| Temp | call-site-specific | Fixed `file.tmp` names in several persistence paths; safe `CreateTemp` in some service writes | Inconsistent collision, symlink, and crash behavior |
| Logs | [`internal/cli/service/setup.go`](../internal/cli/service/setup.go) | macOS LaunchAgent logs under `~/Library/Logs/<product>` | Native and acceptable, but identity naming is inconsistent with other proposed paths |
| User binary | service setup | `~/.local/bin` | Acceptable CLI convention on both platforms; Apple defines no canonical per-user CLI bin |

`internal/xdg` exposes only config and data despite its name. It reimplements
home lookup rather than using the richer standard-library directory functions.
It also accepts relative XDG variables, contrary to XDG and unlike
`os.UserConfigDir`/`os.UserCacheDir`.

`EnsureDir` calls `MkdirAll`, follows the final path with `Stat`, and changes
the final directory to `0700`. That is repeatable for a directory the program
owns, but it is not a safe generic primitive: an explicit path may be shared,
and a symlink can redirect the chmod. The service default-config path contains
a second directory creation implementation and ignores a chmod error. Multiple
implementations are already drifting.

### Config and service equivalence

Both config loaders honor an explicit config path first and otherwise call the
current XDG helper. Data directory defaults are derived separately. The service
setup code makes selected paths absolute, while normal foreground loading does
not consistently do so. Relative certificate, key, ACME storage, data, and
provider working-directory values can consequently resolve against different
current working directories. A LaunchAgent currently sets its working directory
to the user's home, whereas a foreground command inherits the invocation
directory.

[`internal/cli/service/plist_render.go`](../internal/cli/service/plist_render.go)
injects `XDG_CONFIG_HOME=$HOME/.config`, `XDG_DATA_HOME=$HOME/.local/share`, and
`XDG_CACHE_HOME=$HOME/.cache` into every generated LaunchAgent. This freezes the
legacy layout and exports Linux conventions to provider CLIs launched by
mcremote. The same user can therefore see different provider configuration,
credentials, or cache behavior between an interactive shell and the service.

The CLI help and the main configuration documents describe Linux paths as the
universal defaults. They need to derive examples from the resolver or present a
platform table.

### Data classification and filesystem safety

The admin socket is a runtime endpoint, not durable application data. Keeping
it under Application Support would expose it to backup/synchronization, leaves
stale entries in a persistent tree, and creates long Unix socket paths. Portable
software must not assume an arbitrary `sockaddr_un.sun_path` size; macOS paths
are particularly constrained. A short per-user runtime directory avoids that
failure mode.

The stale-socket logic uses `Stat`, probes, and then removes the path. It should
use `Lstat` and refuse an unexpected type, owner, or parent rather than unlinking
an arbitrary entry after a race. Runtime directory ownership and mode must be
validated before listening.

Several durable stores write to the predictable name `<target>.tmp` and
truncate it. Use a random `CreateTemp` file in the **target directory**, apply
the intended mode, write, sync where durability matters, close, rename, and
sync the parent directory. Keeping the staging file beside the target preserves
same-filesystem atomic rename semantics.

The ACME field is named `cache_dir`, but it holds account and certificate state
required for stable operation. It is durable data and must never move to
`UserCacheDir`, where the OS or user may delete it at any time.

### Process lifecycle parity

Process groups and file locking have Darwin implementations and cross-compile.
Engine discovery and crash recovery do not:

- [`internal/cli/engines.go`](../internal/cli/engines.go) explicitly refuses
  non-Linux systems because it depends on `/proc`.
- [`internal/procutil/owner_other.go`](../internal/procutil/owner_other.go) makes
  parent-death handling a no-op, cannot read a process environment, and returns
  no results from `FindByEnv`.
- Startup orphan reaping therefore silently finds nothing on macOS.

This is a user-visible feature gap and a resource-leak risk. A LaunchAgent can
reap a process group during normal service management, but it does not solve a
foreground daemon killed with `SIGKILL`, a process that escapes, or the
`mcremote engines` command.

### DNS and Go build policy

The Makefile applies `CGO_ENABLED=0` and `-tags netgo,osusergo` to every target.
The [`net` package resolver
documentation](https://pkg.go.dev/net#hdr-Name_Resolution) explains that the
pure Go resolver sends DNS queries based on files such as `/etc/resolv.conf`,
while the native resolver uses operating-system facilities. The current Go
[`net/conf.go` source](https://go.dev/src/net/conf.go) explicitly prefers the
native resolver on Darwin and identifies `netgo` as disabling it.

Forcing `netgo` therefore bypasses macOS system resolver behavior. That is a
high compatibility risk for split DNS, VPN/tailnet DNS, multicast resolution,
and dynamically scoped resolvers—the exact environments in which these tools
operate. Darwin can use its system resolver without enabling ordinary cgo in
current Go releases, so the platform build should omit `netgo` and verify the
selected resolver natively with `GODEBUG=netdns=1` tests. Linux can retain the
static pure-Go policy if desired.

### Build, CI, and release

The following audit builds succeeded on 2026-07-31 using the repository's Go
toolchain and tags:

```text
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -tags netgo,osusergo ./cmd/mcremote -> Mach-O arm64
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -tags netgo,osusergo ./cmd/mcremote -> Mach-O x86_64
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -tags netgo,osusergo ./cmd/mcrelay  -> Mach-O arm64
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -tags netgo,osusergo ./cmd/mcrelay  -> Mach-O x86_64
```

This establishes source/build-tag portability only. CI runs Go tests on Ubuntu.
The release workflow enables only `linux/amd64`; `darwin/arm64` is commented
out and `darwin/amd64` is absent. There is no native validation of LaunchAgents,
path permissions, Unix socket length, resolver selection, Darwin process
identity, or service lifecycle.

The repository currently targets Go 1.26.5. Go's [minimum requirements
wiki](https://go.dev/wiki/MinimumRequirements) and [Go 1.26 release
notes](https://go.dev/doc/go1.26) make macOS version support a toolchain policy,
not just a project preference. The release must state its minimum supported
macOS version and update that statement when Go raises its deployment floor.

Unsigned, unnotarized standalone binaries can also encounter Gatekeeper
friction. Apple's [distribution signing
guidance](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac)
and [notarization
guidance](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)
are the release baseline for binaries distributed outside the Mac App Store.

## Gap and risk assessment

| Priority | Gap | User impact | Required outcome |
|---|---|---|---|
| **P0** | Darwin build forces `netgo` | Names may resolve differently from macOS and tailnet/VPN expectations | Platform-specific resolver policy plus native tests |
| **P0** | Engine inventory and orphan recovery are Linux-only | Feature failure and leaked provider processes on macOS | Cross-platform verified runtime registry |
| **P0** | No native macOS CI or active release targets | Compile success can conceal runtime breakage; no supported artifact | Darwin test job and arm64/amd64 signed release |
| **P0** | Changing defaults without migration would fork installations | Apparent data/config loss, duplicate identity, ACME churn | Conflict-detecting, resumable migration |
| **P1** | XDG layout is hard-coded on macOS | Non-native paths and divergence from Go/Apple conventions | OS-native resolver |
| **P1** | LaunchAgent synthesizes XDG variables | Service and foreground/provider behavior drift | Omit synthetic XDG on Darwin |
| **P1** | Runtime socket lives in durable data | Long path, stale socket, unsafe unlink, backup pollution | Private short runtime directory |
| **P1** | Relative paths depend on current directory | Same config means different files in shell and service | Stable path-base contract |
| **P1** | Directory creation follows/chmods a final symlink | Unexpected mutation or redirection | Owned-leaf validation and narrow creation APIs |
| **P1** | Predictable `.tmp` files | Collision, symlink, partial-write risk | Same-directory `CreateTemp` transaction |
| **P2** | Docs/help show Linux-only defaults | Operator mistakes | Generated/platform-specific documentation |
| **P2** | LaunchAgent has no explicit restrictive umask | New-file modes rely on every call site | `Umask=63` (decimal for octal `077`) plus explicit modes |
| **P2** | No signing/notarization pipeline | Gatekeeper warnings and weak provenance | Developer ID signing, hardened runtime, notarization |

## Decisions

### D1 — One typed resolver for both products

Replace the partial `internal/xdg` abstraction and duplicated service helpers
with a package such as `internal/appdirs`:

```go
type Paths struct {
    Home       string
    ConfigDir  string
    ConfigFile string
    DataDir    string
    StateDir   string
    CacheDir   string
    RuntimeDir string
    TempBase   string
    LogDir     string
}

func Resolve(product Product, overrides Overrides) (Paths, error)
func Ensure(paths Paths, needed Set) error
```

`Resolve` is deterministic and has no filesystem side effects. `Ensure`
creates only the directories needed for the selected operation and validates
ownership, type, and mode. This separation makes repeated help, dry-run,
render, and migration calls idempotent.

The product identity is explicit:

| Product | Short name | macOS identifier / launchd label |
|---|---|---|
| mcremote | `mcremote` | `com.magiccliremote.mcremote` |
| mcrelay | `mcrelay` | `com.magiccliremote.mcrelay` |

### D2 — Native directory contract

| Semantic class | Linux default | macOS default | Rules |
|---|---|---|---|
| User home | `os.UserHomeDir()` | `os.UserHomeDir()` | Never infer from a username or current directory |
| Config root | `os.UserConfigDir()/mcremote` or `/mcrelay` | `os.UserConfigDir()/com.magiccliremote.<product>` | Contains `config.yaml`; operator-authored, durable |
| Data | `$XDG_DATA_HOME/<product>`, else `~/.local/share/<product>` | `~/Library/Application Support/com.magiccliremote.<product>/data` | Identity, pairing, sessions, auth, certificate/account material |
| State | `$XDG_STATE_HOME/<product>`, else `~/.local/state/<product>` | `~/Library/Application Support/com.magiccliremote.<product>/state` | Reconstructable operational state, engine registry, migration markers |
| Cache | `os.UserCacheDir()/<product>` | `os.UserCacheDir()/com.magiccliremote.<product>` | Fully disposable; correctness cannot depend on it |
| Runtime | Valid `$XDG_RUNTIME_DIR/<product>`; otherwise validated per-user temp fallback with warning | `os.TempDir()/<product>-<uid>` | Short, private `0700`, local, not backed up; contains `admin.sock` |
| Operation temp | `os.MkdirTemp("", "<product>-*")` or same-directory `CreateTemp` | same | Unique; always cleaned up |
| Logs | journald; file fallback under state when explicitly selected | `~/Library/Logs/com.magiccliremote.<product>` | LaunchAgent stdout/stderr only; bounded by an operations policy |
| Service definition | `os.UserConfigDir()/systemd/user/<unit>.service` | `~/Library/LaunchAgents/<label>.plist` | Native service manager location |
| User executable | `~/.local/bin` | `~/.local/bin` | Deliberate cross-platform CLI convention, not application data |

On Linux, invalid relative XDG values are ignored with a diagnostic and the
specified fallback is used. `$XDG_RUNTIME_DIR` receives additional owner,
directory-type, permission, and locality validation. On macOS, XDG variables do
not alter application defaults.

The fallback runtime leaf includes the numeric UID and is created/validated as
`0700`. If an existing leaf is a symlink, has the wrong owner, or is accessible
by group/other, startup fails instead of repairing an untrusted object. The
admin socket path is length-checked before bind.

### D3 — Override precedence is explicit and platform-neutral

Precedence, highest first:

1. command-line flag;
2. product-specific environment variable;
3. an installation layout marker selected by migration;
4. the OS-native default.

XDG variables participate only in the Linux default calculation. Generic
`XDG_*` values are not a substitute for product-specific overrides on macOS.
All directory/base overrides must be absolute after expansion. `~` is not
silently expanded in config values because shells do not process YAML; accept
an absolute path or a documented `${HOME}` interpolation performed before
validation.

Paths stored in YAML follow one stable rule:

- absolute paths remain absolute;
- relative content paths resolve against the directory containing the loaded
  config file;
- a provider's explicit `default_cwd` resolves against that same base;
- runtime-generated paths come only from `Paths`, never the process working
  directory.

This makes foreground, systemd, and launchd executions equivalent.

### D4 — Do not export invented XDG roots from launchd

The macOS plist contains `HOME`, `USER`, `LOGNAME`, and a deliberate `PATH`, but
does not generate `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, or `XDG_CACHE_HOME`.
An operator can still pass an explicit XDG variable through the existing
extra-environment mechanism for a third-party provider; it must be absolute
and is passed through, not interpreted as mcremote/mcrelay's own macOS default.

The LaunchAgent sets `Umask` to decimal `63` (octal `077`). Sensitive call sites
still use explicit `0600` files and `0700` directories; umask is defense in
depth, not an access-control API.

### D5 — Runtime IPC leaves the data directory

Move `admin.sock` to `RuntimeDir`. At startup:

1. validate the parent without following an untrusted final symlink;
2. `Lstat` an existing socket path;
3. accept only a socket owned by the current user;
4. probe it;
5. remove it only when it is a verified stale socket inside the validated
   runtime parent;
6. bind, chmod `0600`, then verify the result.

Clients resolve exactly the same `RuntimeDir`; there is no data-dir fallback
after migration because probing two sockets creates ambiguity about which
daemon receives an administrative command.

### D6 — Atomic persistence uses unique same-directory staging

Every replacement of tokens, sessions, certificates, configuration, units,
plists, registries, and migration markers uses one shared atomic-write helper:

1. validate the target parent;
2. `CreateTemp(parent, ".<base>-*")`;
3. set the exact permission;
4. write and, for durable state, `Sync`;
5. close;
6. rename to the final path;
7. sync the parent where the platform supports the required durability;
8. remove the temporary file on every error.

No code opens a predictable `<target>.tmp` path. Temporary directories are
unique and cleaned on success and failure. Path handling assumes
case-sensitive semantics so tests also pass on case-sensitive APFS volumes.

### D7 — macOS layout migration is explicit and idempotent

Changing the default without migration is rejected. Add a command such as:

```text
mcremote migrate-paths --dry-run
mcremote migrate-paths --apply
mcrelay  migrate-paths --dry-run
mcrelay  migrate-paths --apply
```

Detection compares the legacy macOS paths (`~/.config/<product>` and
`~/.local/share/<product>`) with the new native paths:

| State | Behavior |
|---|---|
| Neither populated | Use native defaults |
| Legacy only | Continue using legacy with one actionable warning until migration; `setup-service` uses the same selected layout |
| Native only | Use native |
| Both populated and byte-equivalent | Record native selection; retain legacy as an operator-removable backup |
| Both populated and different | Fail closed with both paths and require an explicit source choice; never merge automatically |

`--apply` stops the service or refuses while it is active, takes a per-product
migration lock, copies into a private staging directory on the destination
filesystem, preserves/normalizes modes, verifies critical files, atomically
publishes the destination, writes a versioned layout marker, and restarts the
previously running service. The source remains as a backup until a separate
confirmed cleanup. Re-running any completed or interrupted phase converges on
the same result.

The migration includes configuration, data, state that was previously mixed
into data, logs if applicable, and service references. It does **not** copy a
stale `admin.sock`. ACME accounts and certificates are treated as durable data.

### D8 — Replace `/proc` discovery with a verified engine registry

When mcremote starts an engine, atomically write a `0600` record beneath
`StateDir/engines` containing at least:

```text
engine ID, provider, PID, process-group ID, owner token,
OS process-start token, creation time, daemon instance ID
```

Delete it after normal reap. At startup and for `mcremote engines`, enumerate
records, compare PID plus the OS start token to defeat PID reuse, verify owner
state, and act only on the verified process group. Corrupt or unverifiable
records are quarantined/reported, never used as authority to kill a process.

Linux can obtain the start token from `/proc`. Darwin can obtain process start
information through `golang.org/x/sys/unix` (`sysctl`/`KinfoProc`). Keep the
current parent-death signal as a Linux optimization; the registry is the
cross-platform recovery contract. Tests must cover daemon `SIGKILL`, stale PID
reuse, malformed records, graceful reap, and foreground versus service launch.

### D9 — Platform-specific network build policy

Build policy becomes:

| Target | Resolver policy |
|---|---|
| Linux | `CGO_ENABLED=0`, `netgo,osusergo` may remain for static deployment |
| Darwin | Omit `netgo`; use the native Darwin resolver; retain `CGO_ENABLED=0` only after native verification with the supported Go version |

Native macOS tests must exercise ordinary DNS plus a scoped/split-DNS setup
representative of Tailscale or a VPN. Tests log resolver selection with
`GODEBUG=netdns=1` and fail if a release binary unexpectedly selects the forced
pure-Go path. Build metadata records the Go version and tags.

### D10 — Native CI and supported Darwin releases are release gates

Add a maintained GitHub-hosted macOS runner (see [GitHub-hosted runner
reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners))
that performs:

- `go test ./...` and the race suite where supported;
- native directory-contract tests with isolated homes;
- config equivalence between foreground and rendered LaunchAgent;
- `plutil -lint` and `launchctl bootstrap`/`bootout` lifecycle tests;
- Unix socket creation at realistic long home paths;
- engine registry/orphan recovery tests;
- Darwin native resolver selection and DNS acceptance;
- permission, symlink, relative-path, migration replay, and collision tests.

Release both `darwin/arm64` and `darwin/amd64` (or also provide a verified
Universal 2 artifact). Sign with Developer ID Application identity, enable the
hardened runtime as required by the notarization workflow, timestamp, submit
with `notarytool`, staple where applicable, and verify with `codesign` and
`spctl`. The checksum manifest covers the final distributed artifact.

The supported macOS floor is explicit in release notes and CI. With Go 1.26 it
cannot be lower than the toolchain's supported floor; a toolchain upgrade that
raises that floor is a reviewed compatibility change.

### D11 — Parity is outcome-based and has explicit scope exceptions

The parity contract means both operating systems support the same commands,
configuration meanings, durable state, pairing/auth behavior, transports,
providers, engine inventory/recovery, upgrades, and diagnostics. Mechanisms may
differ (`systemd --user` versus LaunchAgent, XDG versus `~/Library`).

Two differences cannot be eliminated inside the accepted user-only scope:

1. A per-user LaunchAgent ends at logout. Linux user services can persist with
   linger. Surviving macOS logout requires a privileged LaunchDaemon or a
   bundled ServiceManagement design and therefore a new owner decision.
2. A user process cannot normally bind ports 80/443 directly. Use DNS-01,
   redirection, or a reverse proxy. A privileged helper would again expand
   scope.

Documentation and `doctor` output must state these differences. They are
supported deployment constraints, not silent feature failures. If “100%
parity” is defined to include boot-before-login, survive-logout operation, or
direct privileged-port binding, this MADR must remain Proposed until the
no-root boundary in 0058 is revisited.

## Acceptance matrix

Parity is achieved only when every non-excepted row is green on native Linux
and native macOS jobs:

| Capability | Linux evidence | macOS evidence |
|---|---|---|
| Resolve paths | XDG absolute/fallback matrix | Application Support/Caches/Logs/temp matrix |
| Repeat setup | Second run produces no unintended changes | Same, including plist and directory modes |
| Foreground/service equivalence | systemd path/config snapshot | launchd path/config snapshot |
| Existing install upgrade | replayable legacy migration | replayable XDG-to-native migration and conflict case |
| Admin IPC | validated runtime dir and stale-socket test | same plus socket-length test |
| Atomic stores | crash/failure and symlink tests | same on default and case-sensitive APFS |
| DNS | pure-Go/static policy test | native resolver plus split-DNS test |
| Engine lifecycle | normal, crash, PID-reuse tests | same outcomes through Darwin process identity |
| Service lifecycle | install/start/status/restart/remove | bootstrap/kickstart/print/bootout/remove |
| Release | checksum and smoke test | arm64 + amd64, codesign/notary/Gatekeeper smoke test |
| Logout persistence | supported through documented systemd linger policy | **Scoped exception: user LaunchAgent ends at logout** |
| Privileged ports | requires capability/proxy or DNS-01 | requires proxy/redirection or DNS-01 |

Cross-compiling from Linux does not satisfy a macOS evidence cell.

## Consequences

### Positive

- Paths match user expectations and OS cleanup/backup semantics.
- Service and foreground commands operate on the same installation.
- Repeated setup and migration are deterministic and recoverable.
- Runtime IPC is shorter-lived, safer, and less likely to exceed Unix socket
  limits.
- macOS uses its actual DNS routing policy.
- mcremote engine management becomes a real macOS feature rather than a
  compile-only stub.
- Darwin artifacts have test, provenance, and Gatekeeper evidence.

### Costs and tradeoffs

- Existing macOS users need a visible migration instead of a transparent
  default change.
- A typed path package and migration transaction add code and tests.
- Native macOS CI and notarization require runner minutes and Apple credentials.
- The engine registry adds persistent operational state and OS-specific process
  start-token adapters.
- Some third-party CLIs may have relied on the LaunchAgent's invented XDG
  environment. They must receive explicit provider environment overrides.
- “Parity” cannot honestly promise survive-logout behavior while installation
  remains unprivileged.

## Rejected alternatives

### Keep XDG paths everywhere

This avoids migration, but conflicts with Go and Apple native defaults, keeps
service/provider drift, and fails the stated requirement for OS standards.
XDG remains correct on Linux, not a universal macOS application-directory API.

### Change macOS defaults silently

Rejected because it can create an empty second installation and appear to lose
identity, pairings, sessions, or certificates.

### Use cache for all reconstructable-looking files

Rejected. Credentials, ACME state, ownership records, and migration markers are
not safe to purge. Only data whose deletion affects performance—not identity,
availability, or correctness—belongs in cache.

### Keep `admin.sock` in DataDir for discovery simplicity

Rejected because a shared resolver makes runtime discovery equally simple,
while a persistent Application Support path has inferior lifetime, backup,
security, and length properties.

### Parse Darwin process environments to imitate `/proc`

Rejected as the primary contract. It is more fragile and less auditable than an
application-owned registry. The registry records intent; the OS start token
verifies identity before action.

### Treat a successful Darwin cross-build as macOS support

Rejected. It exercises the compiler and build constraints, not launchd, APFS,
the system resolver, socket limits, Gatekeeper, or Darwin process APIs.

## Implementation order

1. Add platform contract tests and the side-effect-free `appdirs` resolver.
2. Make all config, service, help, and doctor paths consume it.
3. Add migration detection, dry-run, transaction, and conflict tests before
   switching the default.
4. Move runtime IPC and adopt the shared atomic-write primitive.
5. Stop synthesizing XDG roots in LaunchAgents and add restrictive umask.
6. Implement the verified engine registry and Darwin process-start adapter.
7. Split Linux/Darwin resolver build policy and add native DNS tests.
8. Add native macOS CI and make both Darwin release targets active.
9. Add signing, notarization, provenance checks, and update platform docs.

No step that changes a default ships before its migration and rollback tests.

## Sources consulted

| Source | Use in this decision |
|---|---|
| [Go `os` package](https://pkg.go.dev/os) | Home, config, cache, temporary directory and safe temp APIs |
| [Go `net` package](https://pkg.go.dev/net) | Resolver modes and `netgo` behavior |
| [Go `net/conf.go`](https://go.dev/src/net/conf.go) | Darwin preference for native resolution |
| [XDG Base Directory Specification 0.8](https://specifications.freedesktop.org/basedir/0.8/) | Linux config/data/state/cache/runtime semantics and validation |
| [Apple File System Programming Guide — Library Directory](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/MacOSXDirectories/MacOSXDirectories.html) | Application Support, Caches, and Logs placement |
| [Apple File System Programming Guide — Locating Items](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/AccessingFilesandDirectories/AccessingFilesandDirectories.html) | Native directory discovery and temporary files |
| [Apple Secure Coding Guide — Race Conditions](https://developer.apple.com/library/archive/documentation/Security/Conceptual/SecureCodingGuide/Articles/RaceConditions.html) | Private temp paths, symlink/TOCTOU, case behavior |
| [Apple launchd guide](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) and [`launchd.plist(5)`](https://manp.gs/mac/5/launchd.plist) | LaunchAgent lifetime, environment, paths, umask |
| [Apple code signing](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac) and [notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) | External distribution requirements |
| [Go minimum requirements](https://go.dev/wiki/MinimumRequirements) and [Go 1.26 notes](https://go.dev/doc/go1.26) | macOS deployment-floor policy |
| [GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) | Native macOS CI availability |
| [`adrg/xdg`](https://github.com/adrg/xdg) | Cross-platform ecosystem precedent; informative, not normative |
| [POSIX `sys_un.h`](https://www.man7.org/linux/man-pages/man0/sys_un.h.0p.html) | Unix socket path portability |
| [Go `os.Root` security design](https://go.dev/blog/osroot) | Traversal-resistant path handling precedent |
