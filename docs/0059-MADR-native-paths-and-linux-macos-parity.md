# MADR 0059: XDG paths and Linux/macOS functional parity

- **Status**: Accepted (A1–A6 locked 2026-07-31; implementation in progress)
- **Date**: 2026-07-31
- **Deciders**: Project Owner
- **Scope**: `mcremote` and `mcrelay` path resolution, runtime files,
  subprocess lifecycle, DNS behavior, service environments, build validation,
  and release packaging on Linux and macOS.
- **Implementation plan**:
  [0059-PLAN-native-paths-and-linux-macos-parity.md](0059-PLAN-native-paths-and-linux-macos-parity.md)
  — actionable build order, APIs, and acceptance gates. Plan amendments
  **A1–A6** refine this MADR for implementation.
- **Related**:
  [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md),
  [0058-PLAN-macos-launchd-service-implementation.md](0058-PLAN-macos-launchd-service-implementation.md),
  [0012-MADR-mcremote-daemon-assessment-action-plan.md](0012-MADR-mcremote-daemon-assessment-action-plan.md),
  [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md).
- **Owner constraints**:
  - **Greenfield on macOS** — no path-layout migration, dual-tree detection,
    layout markers, or `migrate-paths` command.
  - Prefer a **single standards-adherent path contract** over inventing a
    second OS-specific product layout.
- **Relationship to 0058**: Confirms and hardens XDG-shaped product paths on
  macOS (0058 service environment direction). Does **not** replace 0058's
  LaunchAgent design, no-sudo boundary, or modern `launchctl` decisions.

## Decision summary

Adopt one typed application-directory resolver for both binaries that
implements the **XDG Base Directory Specification** on Linux **and** Darwin.
It distinguishes configuration, durable data, state, cache, runtime IPC, and
temporary work; keeps resolution free of filesystem side effects; and rejects
invalid relative `$XDG_*` roots with a diagnostic and the specification
fallback.

Product path leaves use the short name (`mcremote`, `mcrelay`) under XDG roots
on every Unix target. Reverse-DNS identifiers remain LaunchAgent labels and
stdio log directory names only. Do **not** use Darwin
`os.UserConfigDir` / `os.UserCacheDir` for product ConfigDir/DataDir/CacheDir —
those APIs follow Cocoa Application Support / Caches conventions, which this
CLI/daemon product deliberately does not adopt as defaults.

`$XDG_*` variables participate on **both** OSes when absolute and non-empty.
LaunchAgents export the daemon's **resolved absolute** XDG roots so service,
shell, and child engines share one contract. Cross-platform explicit overrides
remain product-specific flags and environment variables (`--config`,
`MCREMOTE_CONFIG`, `MCREMOTE_DATA_DIR`, `MCRELAY_CONFIG`, `MCRELAY_DATA_DIR`).
Relative base overrides are rejected. Relative paths inside YAML resolve
against the directory containing the loaded config file, not the process CWD.
There is **no** `${HOME}` / tilde interpolation.

Because installs are treated as **greenfield**, defaults do not move and no
migration command ships. Multi-instance safety uses a DataDir-derived instance
key for runtime sockets and engine records, not a second filesystem layout.

Functional parity also requires fixes outside path defaults:

1. Omit both `netgo` and `osusergo` on Darwin so the native resolver and
   Directory Services-backed user lookup are available under `CGO_ENABLED=0`.
2. Replace Linux-only `/proc` engine discovery with a cross-platform runtime
   registry whose process identity is verified using an OS-specific start
   token.
3. Run native macOS CI, publish both Darwin architectures, and sign and
   notarize release artifacts.
4. Define parity as equal supported outcomes, with two explicit platform
   exceptions: a user LaunchAgent cannot survive logout, and a non-privileged
   service cannot directly bind privileged ports.

This assessment found that Darwin binaries cross-compile, but the repository
does **not** yet provide functional parity. Cross-compilation is necessary
evidence, not a macOS runtime test.

## Context

The repository already has a substantial macOS LaunchAgent implementation and
XDG-shaped config/data defaults on every OS. The remaining portability problem
is deeper than flipping directory trees: incomplete XDG classes, unsafe
filesystem primitives, Linux-only engine recovery, forced pure-Go DNS on
Darwin, and Linux-only CI/release.

Paths are API and data contracts. Foreground and service execution must resolve
the same files. A second layout or a silent default change can create empty
identity stores, lose pairing state, or request replacement certificates —
exactly the failure mode migration was invented to prevent. Under greenfield,
the correct response is **not** to introduce Library Application Support
defaults and a migration; it is to **keep one XDG contract**, complete it, and
prove service/foreground equivalence.

Desired properties:

- one path meaning per directory class on both OSes;
- XDG-compliant root validation (absolute `$XDG_*` only);
- deterministic, side-effect-free resolution;
- creation that is safe to repeat;
- foreground and service execution resolving the same files;
- no dual-layout or migrate-paths machinery;
- equal mcremote/mcrelay features on Linux and macOS, except where the selected
  operating-system service model makes that impossible.

## Research baseline

### Go standard library

[`os.UserHomeDir`](https://pkg.go.dev/os#UserHomeDir) uses `$HOME` on Unix,
including macOS. [`os.UserConfigDir`](https://pkg.go.dev/os#UserConfigDir)
returns `$XDG_CONFIG_HOME` or `$HOME/.config` on Unix **except Darwin**, where
it returns `$HOME/Library/Application Support` and does **not** honor
`$XDG_CONFIG_HOME`. [`os.UserCacheDir`](https://pkg.go.dev/os#UserCacheDir)
similarly returns XDG cache on Linux and `$HOME/Library/Caches` on Darwin.
Both reject a relative XDG base on platforms where they consult `$XDG_*`.

[`os.TempDir`](https://pkg.go.dev/os#TempDir),
[`os.CreateTemp`](https://pkg.go.dev/os#CreateTemp), and
[`os.MkdirTemp`](https://pkg.go.dev/os#MkdirTemp) provide temporary locations
and unique names. The caller remains responsible for cleanup and for validating
runtime security properties.

Go has no `UserDataDir`, `UserStateDir`, or `UserRuntimeDir`; the application
must define those classes from platform standards. For this product that
standard is XDG on both Linux and Darwin, implemented explicitly rather than
via Darwin `UserConfigDir`.

### Linux / XDG

The [XDG Base Directory Specification
0.8](https://specifications.freedesktop.org/basedir-spec/0.8/) separates
config, data, state, cache, and runtime files. All `$XDG_*_HOME` values must be
absolute; relative values are invalid and must be ignored. `$XDG_RUNTIME_DIR`
has stronger requirements: owned by the user, mode `0700`, local, scoped to the
login lifetime. If it is absent, an application may use a replacement only with
an appropriate warning and equivalent security.

### macOS

Apple's [File System Programming Guide: The Library
Directory](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/MacOSXDirectories/MacOSXDirectories.html)
places application support, caches, and logs under `Library` for **application**
data. That guidance is authoritative for Cocoa/sandboxed apps. It is **not**
adopted here as the product config/data/state contract for a Unix-style CLI
daemon that must match Linux operators, docs, and XDG-aware child CLIs.

Apple paths **are** used where they are the service-manager convention:

- LaunchAgent definitions under `~/Library/LaunchAgents/`;
- agent stdout/stderr under `~/Library/Logs/<label>/` per
  [`launchd.plist(5)`](https://manp.gs/mac/5/launchd.plist).

Apple's [Secure Coding Guide on race
conditions](https://developer.apple.com/library/archive/documentation/Security/Conceptual/SecureCodingGuide/Articles/RaceConditions.html)
still applies: private temporary directories, unique temporary files, symlink
and TOCTOU resistance for config, tokens, certificates, and the admin socket.

A per-user LaunchAgent is tied to its user session; it is not a boot-persistent
daemon.

### Strategic conclusion (path policy)

| Criterion | XDG on Linux and Darwin | Apple Library product defaults |
|---|---|---|
| Spec for CLI/daemon config classes | XDG Base Dir Spec | Apple Library guide (app-oriented) |
| Greenfield cost | Harden existing shape; no migration | Second layout forever, or a migration (forbidden) |
| Operator and docs surface | One matrix | Dual forever |
| Child engines | Often honor `XDG_*` when set | Library roots invisible to XDG-only tools |
| Go Darwin `UserConfigDir` | Deliberate non-use for product paths | Matches stdlib, wrong product class |

**Decision:** XDG-everywhere is the standards-adherent, robust greenfield
outcome. “Native macOS” work remains in DNS, process identity, launchd,
CI, and signing — not in inventing a second product directory tree.

## Current implementation trace

### Directory resolution

| Concern | Current code | Current behavior | Assessment |
|---|---|---|---|
| Config | [`internal/xdg/dirs.go`](../internal/xdg/dirs.go) | `$XDG_CONFIG_HOME/<app>`, else `$HOME/.config/<app>` on every OS | Correct product policy shape; must reject relative `$XDG_*` |
| Data | same | `$XDG_DATA_HOME/<app>`, else `$HOME/.local/share/<app>` | Same |
| Cache | [`internal/cli/service/setup.go`](../internal/cli/service/setup.go) | Duplicated helper for service environment | Needs shared contract in `appdirs` |
| State | none | Mixed into data | Incomplete XDG; add StateDir |
| Runtime | none | Admin socket is `<data_dir>/admin.sock` | Ephemeral IPC must leave durable data |
| Temp | call-site-specific | Predictable `file.tmp` in several stores; `CreateTemp` in some service writes | Inconsistent collision/symlink/crash behavior |
| Logs | service setup | LaunchAgent under `~/Library/Logs/<product>` | Acceptable for agent stdio; use launchd label consistently |
| User binary | service setup | `~/.local/bin` | Acceptable CLI convention on both platforms |

`internal/xdg` exposes only config and data despite its name. It accepts
relative XDG variables, contrary to the specification and to Go's Linux
`UserConfigDir` behavior. Completing XDG compliance — not switching to
Application Support — is the path work.

`EnsureDir` calls `MkdirAll`, follows the final path with `Stat`, and chmods
the final directory to `0700`. That is not a safe generic primitive: an
explicit path may be shared, and a symlink can redirect the chmod. Service
default-config creation is a second implementation that can ignore chmod
errors. Multiple implementations are already drifting.

### Config and service equivalence

Both config loaders honor an explicit config path first and otherwise call the
current XDG helper. The service setup code makes selected paths absolute, while
foreground loading does not consistently do so. Relative certificate, key,
ACME storage, data, and provider working-directory values can resolve against
different CWDs. A LaunchAgent sets working directory to `$HOME`; a foreground
command inherits the invocation directory.

[`internal/cli/service/plist_render.go`](../internal/cli/service/plist_render.go)
injects `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, and `XDG_CACHE_HOME`. Under
XDG-everywhere that **direction is correct**, but values must be the daemon's
**resolved absolute** roots (including state/runtime when used), not
hand-rolled strings that can drift from flag/env overrides.

### Data classification and filesystem safety

The admin socket is runtime IPC, not durable data. Keeping it under DataDir
exposes it to backup tools, leaves stale entries in a persistent tree, and can
produce long `sun_path` values. A short per-user runtime directory with an
instance-key leaf avoids that.

Stale-socket logic must use `Lstat`, refuse unexpected type/owner/parent, and
remove only a verified stale socket. Runtime directory ownership and mode must
be validated before listening.

Durable stores must not use predictable `<target>.tmp` names. Use random
`CreateTemp` in the **target directory**, set mode, write, sync where needed,
close, rename, and best-effort sync the parent.

The ACME field is named `cache_dir` but holds durable account/certificate
state. It must never move to CacheDir.

### Process lifecycle parity

Process groups and file locking have Darwin implementations. Engine discovery
and crash recovery do not:

- [`internal/cli/engines.go`](../internal/cli/engines.go) refuses non-Linux
  because it depends on `/proc`.
- [`internal/procutil/owner_other.go`](../internal/procutil/owner_other.go)
  cannot enumerate environments or verify PID reuse on Darwin.
- Startup orphan reaping therefore finds nothing on macOS.

This is a user-visible feature gap and a resource-leak risk.

### DNS and Go build policy

The Makefile applies `CGO_ENABLED=0` and `-tags netgo,osusergo` to every target.
The [`net` package](https://pkg.go.dev/net#hdr-Name_Resolution) and
[`net/conf.go`](https://go.dev/src/net/conf.go) prefer the native resolver on
Darwin; `netgo` disables it. Forcing `netgo` is a high risk for split DNS,
VPN/tailnet DNS, and scoped resolvers. Darwin builds must omit `netgo` and
`osusergo`. Linux may retain pure-Go static policy.

Verified: `CGO_ENABLED=0` Darwin arm64/amd64 builds of both products succeed
with **no** net/user tags under the repository Go toolchain (1.26.x).

### Build, CI, and release

CI runs Go tests on Ubuntu. Release enables only `linux/amd64`. There is no
native validation of LaunchAgents, path permissions, Unix socket length,
resolver selection, Darwin process identity, or service lifecycle. Unsigned
binaries face Gatekeeper friction. The release must state the minimum supported
macOS version aligned with the Go toolchain floor.

## Gap and risk assessment

| Priority | Gap | User impact | Required outcome |
|---|---|---|---|
| **P0** | Darwin build forces `netgo`/`osusergo` | Names/users resolve differently from macOS expectations | Platform-specific tags + native smoke |
| **P0** | Engine inventory/orphan recovery Linux-only | Feature failure and leaked providers on macOS | Cross-platform verified registry |
| **P0** | No native macOS CI or Darwin release targets | Compile success conceals runtime breakage | Darwin jobs + arm64/amd64 signed release |
| **P1** | Incomplete XDG (no state/runtime; relative roots accepted) | Wrong roots, multi-instance and reboot hygiene gaps | Full XDG classes + absolute-root validation |
| **P1** | Duplicated path helpers; service/foreground CWD drift | Same config, different files | Single `appdirs` + stable path-base rules |
| **P1** | Runtime socket in durable data | Long path, stale socket, unsafe unlink | Instance-keyed RuntimeDir |
| **P1** | Predictable `.tmp` files | Collision, symlink, partial-write risk | Same-directory `CreateTemp` helper |
| **P2** | LaunchAgent XDG strings may drift from overrides | Service/provider vs shell divergence | Export resolved absolute XDG from Paths |
| **P2** | No path introspection command | Operators cannot dump roots | `paths` / `paths --json` |
| **P2** | Docs show incomplete contracts | Operator mistakes | XDG table + override rules for both OSes |
| **P2** | No explicit LaunchAgent umask | Modes rely on every call site | `Umask=63` + explicit 0600/0700 |
| **P2** | No signing/notarization pipeline | Gatekeeper warnings | Developer ID + notarization |

There is **no** gap for “macOS still uses XDG defaults.” That is the accepted
product policy under greenfield.

## Decisions

### D1 — One typed resolver for both products

Replace the partial `internal/xdg` abstraction and duplicated service helpers
with a package such as `internal/appdirs`:

```go
type Paths struct {
    Home, ConfigDir, ConfigFile, DataDir, StateDir, CacheDir string
    RuntimeBase, RuntimeDir, AdminSocket, EngineRegistryDir string
    TempBase, LogDir, InstanceKey string
}

func Resolve(...) (Paths, error) // pure; no filesystem side effects
func Ensure(...) error           // create/validate only what the operation needs
```

Tests inject roots; production discovery branches only where platforms truly
differ (runtime fallback, LaunchAgent log base).

| Product | Short name (XDG leaf) | launchd label |
|---|---|---|
| mcremote | `mcremote` | `com.magiccliremote.mcremote` |
| mcrelay | `mcrelay` | `com.magiccliremote.mcrelay` |

### D2 — XDG directory contract on Linux and Darwin

| Semantic class | Default (both OSes) | Rules |
|---|---|---|
| User home | `os.UserHomeDir()` | Never infer from username or CWD |
| ConfigDir | `$XDG_CONFIG_HOME/<product>` or `~/.config/<product>` | Contains `config.yaml` |
| DataDir | `$XDG_DATA_HOME/<product>` or `~/.local/share/<product>` | Identity, pairing, sessions, auth, ACME/TLS |
| StateDir | `$XDG_STATE_HOME/<product>` or `~/.local/state/<product>` | Reconstructable ops state, engine registry |
| CacheDir | `$XDG_CACHE_HOME/<product>` or `~/.cache/<product>` | Disposable; correctness must not depend on it |
| RuntimeBase | Valid `$XDG_RUNTIME_DIR/<product>`; else validated `/run/user/$UID/<product>` on Linux; else secure per-uid temp leaf with diagnostic | User-owned, directory, `0700`, not a symlink |
| RuntimeDir | `<RuntimeBase>/<instance-key>` | Per DataDir instance |
| Admin socket | `<RuntimeDir>/admin.sock` | Never under DataDir |
| Engine records | `<StateDir>/instances/<instance-key>/engines` | Same instance key |
| Operation temp | `MkdirTemp` / same-directory `CreateTemp` | Unique; cleaned up |
| Service definition | systemd user unit / `~/Library/LaunchAgents/<label>.plist` | Service manager locations |
| LaunchAgent stdio | n/a (journald) / `~/Library/Logs/<label>/` | Agent stdout/stderr only |
| User executable | `~/.local/bin` | Cross-platform CLI convention |

`$XDG_*` absolute and non-empty → use on **both** OSes. Relative → ignore with
diagnostic and use fallback. Do not call Darwin `os.UserConfigDir` for product
paths.

`instance-key` = first 16 lowercase hex chars of
`SHA-256(clean-absolute-data-dir)`.

`tls.letsencrypt.cache_dir` remains a legacy field name for durable CertMagic
storage under DataDir (default `<DataDir>/acme`); never CacheDir.

### D3 — Override precedence is explicit and platform-neutral

Highest first:

1. command-line flag;
2. product-specific environment variable;
3. XDG-derived or built-in default from the resolver.

No layout marker step (greenfield; no migration).

All directory/base overrides must be absolute after flag CWD canonicalization.
`~` and `${HOME}` are **not** expanded in config values. Relative YAML
filesystem fields resolve against the directory containing the loaded config
file. Provider `bin` basenames may use `PATH`.

### D4 — Export validated XDG roots from LaunchAgent

The macOS plist sets `HOME`, `USER`, `LOGNAME`, a deliberate `PATH`, `Umask`
decimal `63` (octal `077`), and the daemon's **resolved absolute**
`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, and
`XDG_RUNTIME_DIR` when the runtime base is a real XDG runtime root. Values must
match `appdirs` after overrides — not independently hand-rolled home-relative
strings.

Extra `--env` pass-through remains; path-like extras must be absolute.
Sensitive call sites still use explicit `0600` / `0700`; umask is defense in
depth.

### D5 — Runtime IPC leaves the data directory

Move `admin.sock` to instance-keyed `RuntimeDir`. At startup: validate parent
without trusting a final symlink; `Lstat`; accept only a current-user socket;
probe; remove only verified stale sockets inside the validated parent; bind;
chmod `0600`; verify. Clients use the same resolver. No DataDir socket fallback
(dual probes are ambiguous).

### D6 — Atomic persistence uses unique same-directory staging

Shared helper for single-file replacement of tokens, sessions, configuration,
units, plists, and engine records:

1. validate target parent;
2. `CreateTemp(parent, ".<base>-*")`;
3. exact permission;
4. write and, for durable state, `Sync`;
5. close;
6. rename;
7. best-effort parent sync where required;
8. remove temp on every error.

No predictable `<target>.tmp`. Certificate pair promotion retains its stable
`.new` recovery journal; private staging before publish to `.new` must still be
unique.

### D7 — Greenfield: no path-layout migration

There is no `migrate-paths` command, no dual legacy/native selection, and no
layout marker. Defaults already match the accepted XDG contract; work is
compliance and parity, not relocation.

If a future owner decision introduces an installed base that must move, that
requires a new MADR. Implementing migration under the current constraint is
rejected as pure cost.

### D8 — Replace `/proc` discovery with a verified engine registry

When mcremote starts an engine, atomically write a `0600` record under the
instance engine directory containing at least:

```text
engine ID, provider, PID, process-group ID, owner token,
OS process-start token, creation time, daemon instance ID
```

Delete after normal reap. Enumerate, verify PID + OS start token + owner before
any signal. Quarantine corrupt/unverifiable records; never kill on PID alone.

Linux start token from `/proc`; Darwin from `golang.org/x/sys/unix`
(`sysctl` / `KinfoProc`). Keep Linux Pdeathsig as defense-in-depth only.

### D9 — Platform-specific network and user build policy

| Target | Policy |
|---|---|
| Linux | `CGO_ENABLED=0`; `netgo,osusergo` may remain for static deployment |
| Darwin | Omit **both** `netgo` and `osusergo`; `CGO_ENABLED=0` after native verification |

Native macOS smoke logs resolver selection with `GODEBUG=netdns=1`. Split-DNS /
tailnet cases are documented manual acceptance where public runners are flaky.
Build metadata records Go version and tags.

### D10 — Native CI and supported Darwin releases are release gates

Native GitHub-hosted macOS runners run tests/race as supported, directory
contract tests, foreground/LaunchAgent equivalence, `plutil -lint`, launchctl
lifecycle smoke, socket length, engine registry, and resolver/user smoke.

Release both `darwin/arm64` and `darwin/amd64`. Sign with Developer ID
Application, hardened runtime, timestamp; notarize with `notarytool`; verify
with `codesign` and `spctl`. State the minimum macOS version (Go toolchain
floor is a floor, not optional).

### D11 — Parity is outcome-based with explicit scope exceptions

Both OSes support the same commands, configuration meanings, durable state,
pairing/auth, transports, providers, engine inventory/recovery, and
diagnostics (`paths` / `paths --json`). Mechanisms may differ (`systemd --user`
versus LaunchAgent). Path *defaults* do **not** differ by product policy: both
use XDG.

Two differences cannot be eliminated inside the accepted user-only scope:

1. A per-user LaunchAgent ends at logout; Linux user services may linger.
2. A user process cannot normally bind ports 80/443; use DNS-01, redirection,
   or a reverse proxy.

Documentation and `paths`/help text must state these. They are deployment
constraints, not silent feature failures.

## Acceptance matrix

Parity is achieved only when every non-excepted row is green on native Linux
and native macOS jobs:

| Capability | Linux evidence | macOS evidence |
|---|---|---|
| Resolve paths | XDG set/unset/invalid-relative matrix | **Same algorithm** + native filesystem smoke |
| Repeat setup | Second run produces no unintended changes | Same, including plist and directory modes |
| Foreground/service equivalence | systemd snapshot matches resolved Paths | launchd XDG env matches resolved Paths |
| Layout migration | **none** (greenfield) | **none** (greenfield) |
| Admin IPC | validated runtime dir and stale-socket tests | same plus socket-length test |
| Atomic stores | crash/failure and symlink tests | same; case-sensitive APFS when available |
| DNS / user lookup | pure-Go/static policy test | no forced tags; native resolver/user smoke |
| Engine lifecycle | normal, crash, PID-reuse tests | same via Darwin process identity |
| Service lifecycle | install/start/status/restart/remove | bootstrap/kickstart/print/bootout/remove |
| Diagnostics | `paths --json` matches serve resolution | identical |
| Release | checksum and smoke | arm64 + amd64 codesign/notary/Gatekeeper smoke |
| Logout persistence | documented systemd linger | **Exception: LaunchAgent ends at logout** |
| Privileged ports | capability/proxy or DNS-01 | proxy/redirection or DNS-01 |

Cross-compiling from Linux does not satisfy a macOS evidence cell.

## Consequences

### Positive

- One path contract and one operator story on Linux and macOS.
- No migration surface area or dual-tree failure modes.
- XDG compliance (absolute roots, state/runtime classes) matches the CLI/daemon
  ecosystem and child-engine expectations.
- Service and foreground commands operate on the same installation when XDG
  exports match resolved Paths.
- Runtime IPC is shorter-lived, safer, and less likely to exceed Unix socket
  limits.
- macOS uses its actual DNS routing and user-lookup facilities.
- Engine management becomes a real macOS feature.
- Darwin artifacts gain test, provenance, and Gatekeeper evidence.

### Costs and tradeoffs

- macOS operators who expect Application Support for *all* apps must learn that
  this product uses XDG by policy (`paths` and docs make that explicit).
- A typed path package, instance-key runtime, and engine registry add code and
  tests.
- Native macOS CI and notarization require runner minutes and Apple credentials.
- “Parity” cannot honestly promise survive-logout behavior while installation
  remains unprivileged.
- Go's Darwin `UserConfigDir` is intentionally unused for product paths;
  contributors must not “fix” that by switching to Application Support.

## Rejected alternatives

### Apple Library product defaults on macOS

Rejected under greenfield and CLI/daemon standards. Creates a permanent second
layout, docs dualism, and either a migration or a silent fork risk. Apple
Library remains correct for LaunchAgent plists and agent stdio logs only.

### Path-layout migration (`migrate-paths`)

Rejected. No installed-base requirement; migration would be pure cost and a
second source of path bugs. A future installed-base move needs a new MADR.

### Silent default changes

Rejected in principle. Greenfield avoids the need; any future default change
without an explicit owner decision and migration design remains forbidden.

### Use cache for durable-looking files

Rejected. Credentials, ACME state, and ownership records are not safe to purge.
Only performance-only data belongs in CacheDir.

### Keep `admin.sock` in DataDir for discovery simplicity

Rejected. A shared resolver makes runtime discovery equally simple; DataDir has
inferior lifetime, backup, security, and length properties.

### Parse Darwin process environments to imitate `/proc`

Rejected as the primary contract. More fragile than an application-owned
registry plus OS start-token verification.

### Treat a successful Darwin cross-build as macOS support

Rejected. It exercises the compiler, not launchd, APFS, the system resolver,
socket limits, Gatekeeper, or Darwin process APIs.

### `${HOME}` / tilde interpolation in YAML

Rejected. Absolute overrides and config-relative relative paths are sufficient;
interpolation adds a second parser and source-dependent behavior.

## Implementation order

Aligned with the companion plan:

1. Decision lock: this MADR + plan A1–A6; hermetic baseline tests.
2. Side-effect-free `appdirs` + `fsutil` atomic helper (no caller cutover).
3. Config/relay finalization; remove `internal/xdg`; add `paths` / `paths --json`.
4. Service convergence: export resolved absolute XDG; LaunchAgent `Umask=63`.
5. Runtime admin IPC + single-file atomic adoption; preserve cert `.new` journal.
6. Verified engine registry and Darwin process-start adapter.
7. Split Linux/Darwin build tags; metadata checks; native DNS/user smoke.
8. Native macOS CI; activate Darwin release targets; signing/notarization.
9. Docs and acceptance matrix evidence; status flip only with evidence.

No dual-layout detection or migrate-paths step appears in this order.

## Plan amendments A1–A6 (normative for implementation)

**Status: Accepted** by Project Owner 2026-07-31.

| ID | Decision | Status |
|---|---|---|
| **A1** | XDG layout on Linux and Darwin; no Application Support product defaults | Accepted |
| **A2** | DataDir-derived instance key for runtime and engine state | Accepted |
| **A3** | Omit both `netgo` and `osusergo` on Darwin | Accepted |
| **A4** | No `${HOME}` / tilde interpolation | Accepted |
| **A5** | Shared single-file atomic helper; certificate pair recovery remains specialized | Accepted |
| **A6** | `paths` / `paths --json` diagnostics; general `doctor` out of scope | Accepted |

## Sources consulted

| Source | Use in this decision |
|---|---|
| [Go `os` package](https://pkg.go.dev/os) | Home, temp APIs; Darwin vs Unix `UserConfigDir` behavior |
| [Go `net` package](https://pkg.go.dev/net) | Resolver modes and `netgo` behavior |
| [Go `net/conf.go`](https://go.dev/src/net/conf.go) | Darwin preference for native resolution |
| [XDG Base Directory Specification 0.8](https://specifications.freedesktop.org/basedir-spec/0.8/) | Config/data/state/cache/runtime semantics and absolute-root validation |
| [Apple File System Programming Guide — Library Directory](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/MacOSXDirectories/MacOSXDirectories.html) | Informative for app placement; **not** product path defaults |
| [Apple Secure Coding Guide — Race Conditions](https://developer.apple.com/library/archive/documentation/Security/Conceptual/SecureCodingGuide/Articles/RaceConditions.html) | Private temp, symlink/TOCTOU |
| [Apple launchd guide](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) and [`launchd.plist(5)`](https://manp.gs/mac/5/launchd.plist) | LaunchAgent lifetime, environment, stdio paths, umask |
| [Apple code signing](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac) and [notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) | External distribution |
| [Go minimum requirements](https://go.dev/wiki/MinimumRequirements) and [Go 1.26 notes](https://go.dev/doc/go1.26) | macOS deployment-floor policy |
| [GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) | Native macOS CI |
| [`adrg/xdg`](https://github.com/adrg/xdg) | Cross-platform CLI ecosystem precedent; informative |
| [POSIX `sys_un.h`](https://www.man7.org/linux/man-pages/man0/sys_un.h.0p.html) | Unix socket path portability |
| Companion [0059 plan](0059-PLAN-native-paths-and-linux-macos-parity.md) | Implementation refinements A1–A6, phases, gates |
