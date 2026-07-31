# MADR 0059 — Implementation plan: XDG paths and Linux/macOS parity

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to
[MADR 0059](0059-MADR-native-paths-and-linux-macos-parity.md). This is the
review and build plan: it re-checks the MADR against the current tree, corrects
underspecified decisions under an owner constraint of **greenfield macOS
(no path migration)**, names concrete APIs and files, and defines acceptance
gates.

- **Status:** Accepted — A1–A6 locked 2026-07-31; implementation in progress.
  MADR 0059 is aligned (XDG-everywhere, greenfield, A1–A6).
- **Date:** 2026-07-31
- **Scope:** `mcremote`, `mcrelay`, shared path/filesystem packages, config
  loading, service rendering, admin IPC, engine supervision, Makefile, CI,
  Darwin release packaging, and operator documentation.
- **Owner constraints (this revision):**
  - **Greenfield on macOS** — no installed-base migration, no dual-layout
    detection, no `migrate-paths` command, no layout markers.
  - Prefer **standards-adherent, single-contract** path behavior over
    inventing a second OS-specific layout.
- **Non-goals:** privileged LaunchDaemon installation, App Store packaging,
  `SMAppService`, direct unprivileged binding to ports 80/443, pretending a
  LaunchAgent survives logout, **or any XDG→Library (or reverse) migration**.
- **Standards:** project `AGENTS.md`; Go pre-add gate (`gofmt`, `golint`,
  `govulncheck`); `make test` and `make race` before a commit.

---

## 0. Research and strategic assessment

### 0.1 Why the MADR proposed leaving XDG on macOS

MADR 0059 argued for Apple `~/Library/...` defaults because:

1. Go's `os.UserConfigDir` / `os.UserCacheDir` return
   `~/Library/Application Support` and `~/Library/Caches` on Darwin, not XDG.
2. Apple's File System Programming Guide documents Application Support, Caches,
   and Logs for application data.
3. Mixing Linux-shaped defaults into a LaunchAgent can surprise macOS-only
   operators who expect Library trees.

Those points are real for **Cocoa / sandboxed GUI apps**. They are weaker for
**Unix-style CLI daemons** that already speak XDG on Linux and must stay
identical under `systemd --user` and LaunchAgent.

### 0.2 Why XDG on both platforms is the better greenfield outcome

| Criterion | XDG on Linux **and** macOS | Apple Library layout on macOS |
|---|---|---|
| Spec authority for *this* product class | [XDG Base Directory Spec 0.8](https://specifications.freedesktop.org/basedir-spec/0.8/) is the de facto standard for CLI config/data/state/cache/runtime | Apple Library guide targets application bundles and GUI apps |
| Code and docs surface | One layout matrix, one test suite, one operator story | Two layouts forever; dual docs and dual fixtures |
| Greenfield cost | Zero migration; harden the existing XDG helpers | Either ship empty Library paths from day one *or* migrate — owner forbids migration |
| Override model | Absolute `$XDG_*` + product flags/env (already planned) | Same flags, but defaults and LaunchAgent env diverge from Linux |
| Child CLIs / engines | Many honor `XDG_*` when set; LaunchAgent can export the same roots the daemon uses | Invented Library roots are invisible to tools that only know XDG |
| Go stdlib | On Linux, `UserConfigDir`/`UserCacheDir` implement XDG (and reject relative `$XDG_*`) | On Darwin, stdlib deliberately **ignores** `$XDG_CONFIG_HOME` and returns Application Support — correct for GUI, not mandatory for CLI |
| Backup / cache semantics | Operator controls via XDG or product flags; CacheDir remains disposable by contract | Library/Caches has nicer default exclusion on some backup tools — minor for this product |
| Service manager logs | journald (Linux); LaunchAgent `StandardOutPath` may still use `~/Library/Logs` without putting *product* state there | Same LaunchAgent log paths either way |

**Conclusion:** For mcremote/mcrelay, **deviating from XDG on macOS is not
required for robustness or standards adherence.** The robust greenfield choice
is:

1. Implement the **XDG Base Directory Specification fully and correctly** on
   every Unix target (Linux and Darwin).
2. Use Apple paths only where XDG is silent or where the **service manager**
   expects them (LaunchAgent stdout/stderr under `~/Library/Logs`).
3. Do **not** call Darwin `os.UserConfigDir` for product ConfigDir/DataDir —
   implement the XDG algorithm ourselves (as the tree already roughly does),
   and bring it into full compliance (absolute roots, state/runtime classes,
   rejection of relative `$XDG_*`).

This is a deliberate, documented choice *against* Go's Darwin `UserConfigDir`
default and *for* XDG as the product path contract. It does **not** mean
ignoring macOS entirely: DNS/user build tags, engine process identity,
LaunchAgent lifecycle, CI, and signing remain Darwin-native work.

### 0.3 Standards mapping used by this plan

| Class | Authority | Product rule |
|---|---|---|
| Config / data / state / cache | XDG Base Directory Spec 0.8 | Same algorithm on Linux and Darwin |
| `$XDG_*` absolute requirement | XDG §2; also Go's Linux `UserConfigDir` | Relative or empty-invalid → ignore with diagnostic, use fallback |
| Runtime | XDG `$XDG_RUNTIME_DIR` security properties | Validate ownership, type, mode `0700`, locality; never invent an insecure shared temp as a silent default |
| Atomic replace | POSIX rename-over; Apple Secure Coding Guide TOCTOU notes | Unique same-directory staging (`CreateTemp`), not predictable `*.tmp` |
| LaunchAgent logs | `launchd.plist(5)` StandardOutPath/ErrorPath | `~/Library/Logs/<launchd-label>/` only for agent stdio, not product DataDir |
| DNS / user DB on Darwin | Platform resolver / Directory Services | Omit `netgo` and `osusergo` on Darwin; keep static pure-Go policy on Linux |

### 0.4 What this revision changes relative to the prior plan draft

| Prior plan/MADR direction | This revision |
|---|---|
| macOS defaults under `~/Library/Application Support/...` | **XDG defaults on macOS** (`~/.config`, `~/.local/share`, …) |
| Dual legacy/native layout + `migrate-paths` | **Greenfield only** — no migration package or commands |
| Remove XDG exports from LaunchAgent | **Keep** exporting *validated absolute* XDG roots so agent == shell |
| Config under `.../<id>/config` sibling of data/state | Single product leaf under each XDG root (standard XDG layout) |
| Diagnostics via `migrate-paths --dry-run --json` | Diagnostics via `paths --json` (and setup-service dry-run) |

---

## 1. Reassessment of MADR 0059

### 1.1 Overall judgment

The MADR's central *finding* still stands: Darwin compilation succeeds, but
the tree does **not** yet provide functional parity (engine discovery, build
tags, CI/release, runtime IPC hygiene, atomic writes, XDG compliance gaps).

The MADR's central *directory strategy* (Apple-native defaults + migration)
does **not** stand under the greenfield constraint and the assessment in §0.
Replace it with **XDG-everywhere + Darwin-native non-path parity work**.

What should still ship:

- Full XDG class coverage (config, data, state, cache, runtime) with correct
  absolute-root validation.
- Instance-keyed runtime and engine namespaces (multi-instance safety).
- Runtime IPC out of DataDir.
- Unique-staging atomic writes (with certificate-pair recovery preserved).
- Cross-platform engine registry (replace Linux-only `/proc` discovery).
- Darwin without forced `netgo`/`osusergo`.
- Native macOS CI and signed/notarized Darwin releases.
- Explicit parity exceptions (LaunchAgent logout; no unprivileged 80/443).

What must not ship:

- Library Application Support product trees as defaults.
- Layout markers, dual-tree detection, or any migrate-paths flow.

### 1.2 Verified codebase facts

| Fact | Concrete evidence | Consequence |
|---|---|---|
| Config/data defaults are already XDG-shaped on every OS | `internal/xdg/dirs.go` joins `.config` and `.local/share` | Greenfield can **keep** this shape; harden compliance instead of flipping defaults |
| Relative XDG roots are accepted | `ConfigHomeFor` / `DataHomeFor` join any non-empty value | Violates XDG absolute-path requirement and Go Linux `UserConfigDir` behavior |
| No State/Runtime classes | Socket under DataDir; state mixed into data | Incomplete XDG; multi-instance and reboot hygiene suffer |
| Directory logic is duplicated | `internal/xdg/dirs.go` vs helpers in `internal/cli/service/setup.go` | Foreground and service behavior can drift |
| Service paths are normalized differently | `service.normalize` absolutizes; foreground loaders do not finalize all fields | Same YAML can resolve differently by CWD |
| Darwin plist exports XDG roots | `plist_render.go` injects `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME` | Correct *direction* for XDG-everywhere; must export **validated absolute** values matching the daemon's resolved roots (including state when used) |
| Admin IPC is data-relative | `admin.SocketPath(dataDir)` | Custom data dirs / multi-instance collide if socket is a single fixed path under product data |
| Stale socket cleanup is path-naive | `Stat`, failed ping, `Remove` | Symlink/type/owner races |
| Auth/session writes use predictable `*.tmp` | `auth.writeFileAtomic`, `session.Store.atomicWrite` | Collision and symlink risk |
| Certificate staging is a recovery journal | `certs.writePairAtomic` / `.new` promote | Must not be collapsed into a blind single-file helper |
| Engine discovery is Linux-only | `engines.go`, `owner_other.go` | macOS engines/orphan reaping nonfunctional |
| Build tags force pure-Go net/user everywhere | Makefile `netgo,osusergo` | Darwin bypasses native DNS and DS-backed user lookup |
| Release/CI are Linux-only | Ubuntu job; `PLATFORMS` only `linux/amd64` | Cross-build is not a runtime proof |
| No path diagnostic command | No `doctor` / `paths` | Operators cannot dump resolved roots without reading code |

### 1.3 Required MADR amendments for approval (A1–A6)

These supersede the previous plan's A1–A6 text and match MADR 0059 D1–D11 /
A1–A6. Accept these before coding past Phase 1.

| ID | Previous / MADR direction | **Approved greenfield decision** | Rationale |
|---|---|---|---|
| **A1** | Darwin ConfigDir under Application Support (`…/<id>` or `…/<id>/config`) | **XDG layout on Linux and Darwin.** ConfigDir = `$XDG_CONFIG_HOME/<product>` or `~/.config/<product>`; DataDir / StateDir / CacheDir follow the same product leaf under their XDG roots. Do not use `os.UserConfigDir` on Darwin for product paths. | Spec-complete, single contract, greenfield-friendly; Apple Library is not the CLI standard |
| **A2** | Runtime = single `<product>/admin.sock` | **Instance-key namespace:** `RuntimeDir = <RuntimeBase>/<instance-key>`; engines under `StateDir/instances/<instance-key>/engines`. `instance-key` = first 16 hex chars of SHA-256(clean absolute DataDir). | Multi-instance and custom `--data-dir` safety without migrating anything |
| **A3** | Omit `netgo` on Darwin | Omit **both** `netgo` and `osusergo` on Darwin. Linux may keep pure-Go static policy. | Native Darwin DNS/user with `CGO_ENABLED=0` (verified arm64/amd64 no-tag builds) |
| **A4** | Documented `${HOME}` interpolation | **No interpolation.** Absolute overrides only; relative YAML filesystem fields resolve against the directory containing the loaded config file. Literal `$`, `${HOME}`, and `~` stay literal. | One parser-free rule; no shell/YAML semantic split |
| **A5** | One helper for every temp transaction | Shared `WriteFileAtomic` for single-file replace; **certificate pair keeps** its stable `.new` recovery protocol with unique private staging before publish. | Predictable `*.tmp` is unsafe; cert recovery is a multi-file journal |
| **A6** | `doctor` / migration diagnostics | Add **`paths`** (both products): print resolved roots; `--json` for machines. Document LaunchAgent logout and privileged-port constraints in help/docs. General multi-check `doctor` remains out of scope. | Greenfield needs introspection without inventing migration or a kitchen-sink doctor |

### 1.4 Additional constraint: test hermeticity

The focused baseline suite passes with a sanitized `PATH`, but fails in the
ambient development environment at `TestRenderPlistLogsNotTmp`. That test
searches the entire plist for `/tmp/`; the inherited PATH can contain a
legitimate Codex temporary component even when log fields are under
`Library/Logs`. Replace substring tests with parsed-field assertions and
inject roots/env.

Verified during reassessment:

```text
go test ./internal/xdg ./internal/config ./internal/relay ./internal/admin \
  ./internal/procutil ./internal/cli/service ./internal/cli
  # passes with a controlled PATH; ambient PATH exposes the brittle plist test

for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -o /dev/null ./cmd/mcremote
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -o /dev/null ./cmd/mcrelay
done
# all four succeed with neither netgo nor osusergo
```

---

## 2. Target path contract

### 2.1 Products and layouts

| Product | Short name | launchd label / reverse-DNS id (service only) |
|---|---|---|
| mcremote | `mcremote` | `com.magiccliremote.mcremote` |
| mcrelay | `mcrelay` | `com.magiccliremote.mcrelay` |

Product directory leaves under XDG roots use the **short name**, not the
reverse-DNS id. The reverse-DNS id remains the LaunchAgent label and log
directory name only.

| Class | Default (Linux **and** Darwin) | Rules |
|---|---|---|
| ConfigDir | `$XDG_CONFIG_HOME/<product>` or `~/.config/<product>` | Contains `config.yaml`; durable, operator-authored |
| ConfigFile | `<ConfigDir>/config.yaml` | Single file |
| DataDir | `$XDG_DATA_HOME/<product>` or `~/.local/share/<product>` | Identity, pairing, sessions, auth, ACME/TLS material |
| StateDir | `$XDG_STATE_HOME/<product>` or `~/.local/state/<product>` | Reconstructable operational state, engine registry |
| CacheDir | `$XDG_CACHE_HOME/<product>` or `~/.cache/<product>` | Fully disposable; correctness must not depend on it |
| RuntimeBase | Valid `$XDG_RUNTIME_DIR/<product>`; else validated `/run/user/$UID/<product>` on Linux; else secure per-uid temp leaf with diagnostic | Owned by user, directory, mode `0700`, not a symlink |
| RuntimeDir | `<RuntimeBase>/<instance-key>` | Per DataDir instance |
| Admin socket | `<RuntimeDir>/admin.sock` | Never under DataDir |
| Engine records | `<StateDir>/instances/<instance-key>/engines` | Same instance key as runtime |
| Operation temp | `MkdirTemp` / same-directory `CreateTemp` | Unique; cleaned on success and failure |
| Service unit / plist | `~/.config/systemd/user/*.service` (Linux); `~/Library/LaunchAgents/<label>.plist` (Darwin) | Service manager locations, not XDG product data |
| LaunchAgent stdio | n/a on Linux (journald); `~/Library/Logs/<label>/` on Darwin | **Only** agent stdout/stderr; not product LogDir for application state |
| User executable install | `~/.local/bin` | Deliberate cross-platform CLI convention |

`$XDG_*` values participate on **both** OSes when absolute and non-empty.
Relative `$XDG_*` values are **invalid**: ignore with a diagnostic and use the
specification fallback. Never synthesize relative roots.

`tls.letsencrypt.cache_dir` remains a legacy *field name* for durable CertMagic
storage. Default remains `<DataDir>/acme`; it **never** uses CacheDir.

### 2.2 Instance identity

```text
instance-key = first 16 lowercase hex chars of SHA-256(clean-absolute-data-dir)
```

DataDir is the identity boundary (devices, pair codes, sessions, managed TLS,
admin ownership). Daemon and CLI must call the same function after all
overrides. Never use PID in a durable namespace.

### 2.3 Path-source rules

Precedence, highest first:

1. command-line flag;
2. product-specific environment variable (`MCREMOTE_CONFIG`,
   `MCREMOTE_DATA_DIR`, `MCRELAY_CONFIG`, `MCRELAY_DATA_DIR`);
3. XDG-derived or built-in default from the resolver.

| Source | Relative value behavior |
|---|---|
| CLI `--config`, `--data-dir`, TLS path flags | Resolve once against invocation CWD; store absolute |
| Product config/data environment variables | Reject if relative |
| `$XDG_*` roots | Reject relative (diagnostic + fallback); never join |
| YAML filesystem values | Resolve against the directory containing the loaded config file |
| Default | Resolver returns absolute paths only |
| Provider `bin` | Basename may use `PATH`; do not force-absolute |

Filesystem YAML fields: DataDir, TLS cert/key, ACME storage, provider
`default_cwd`. Non-paths: `directory_url`, Route53 profile, provider binary
names.

### 2.4 LaunchAgent environment (XDG-aligned)

The Darwin plist **does** export the daemon's resolved absolute XDG roots
(`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, and
`XDG_RUNTIME_DIR` when the runtime base is a real XDG runtime root). Values
must match `appdirs` resolution after overrides — not hand-rolled
`HOME/.config` strings that can drift.

Also set `HOME`, `USER`, `LOGNAME`, deliberate `PATH`, and `Umask` decimal
`63` (octal `077`). Extra `--env` pass-through remains; absolute only for path
keys.

Rationale: under XDG-everywhere, exporting XDG is how LaunchAgent and
interactive shells stay equivalent and how child engines see the same contract.

---

## 3. Target architecture and APIs

### 3.1 `internal/appdirs`

Create:

```text
internal/appdirs/
  product.go          Product definitions and validation
  roots.go            XDG root discovery (both GOOS) + injectable test roots
  paths.go            pure layout construction and InstanceKey
  ensure_unix.go      private leaf/type/owner/mode validation
  runtime_unix.go     runtime validation and Unix socket length
  paths_test.go
  ensure_unix_test.go
```

**No** `detect.go`, layout enum, or legacy/native selection.

Public internal API:

```go
type Product struct{ Name, LaunchLabel string }
type Roots struct {
    Home, ConfigHome, DataHome, StateHome, CacheHome string
    RuntimeHome, Temp, Logs string // Logs = LaunchAgent stdio base on Darwin only
}
type Paths struct {
    Product Product
    Home, ConfigDir, ConfigFile, DataDir, StateDir, CacheDir string
    LogDir, RuntimeBase, RuntimeDir, AdminSocket string
    EngineRegistryDir, TempBase, InstanceKey string
}
type Diagnostic struct{ Code, Message string }

func SystemRoots(product Product) (Roots, []Diagnostic, error)
func Resolve(product Product, roots Roots, dataDirOverride string) (Paths, error) // pure
func WithDataDir(Paths, string) (Paths, error) // recompute instance paths
func EnsurePrivateDir(string) error
func ValidateRuntimeDir(string) error
```

XDG home resolution (both Linux and Darwin):

```text
xdgOr(env, fallbackUnderHome):
  if $env is non-empty and absolute → use it
  if $env is non-empty and relative → diagnostic, use fallback
  else → fallback
```

Do not make `runtime.GOOS` the only test seam. Tests inject `Roots`; production
discovery branches only where platforms truly differ (runtime fallback roots,
LaunchAgent log base).

### 3.2 Filesystem primitives

Create `internal/fsutil/atomic.go`, `sync.go`, and Unix lock helpers:

```go
type AtomicOptions struct {
    Perm os.FileMode
    SyncFile, SyncDir bool
}
func WriteFileAtomic(path string, data []byte, opts AtomicOptions) error
func WithLock(path string, timeout time.Duration, fn func() error) error
```

`WriteFileAtomic` uses `CreateTemp(filepath.Dir(path), ".<base>-*")`, exact
chmod, optional file sync, close, rename, optional best-effort directory sync,
and cleanup on every failure. Reject a symlink final target where the call
site requires a regular owned file.

Adopt it in `auth/store.go`, `session/store.go`, service config/unit/plist
writes, and engine records.

For `certs.writePairAtomic`, retain `.new` as a recovery protocol. Write each
payload to a unique file, then atomically publish to stable `.new`; keep
`promoteNewPair` and `cleanupOrphanNew` tests.

### 3.3 Effective config metadata

Add non-Viper fields to both `config.Config` and `relay.FileConfig`:

```go
ConfigFile  string              `mapstructure:"-"`
Paths       appdirs.Paths       `mapstructure:"-"`
Diagnostics []appdirs.Diagnostic `mapstructure:"-"`
```

Loading order:

1. resolve/canonicalize explicit config flag or env;
2. read YAML (missing optional default config is allowed where today allows it);
3. bind flags/env and unmarshal;
4. determine origin for each filesystem field;
5. normalize according to §2.3;
6. recompute `Paths` from final DataDir;
7. validate config once;
8. normalize TLS once.

Remove post-Load filesystem overrides in `internal/cli/serve.go`,
`internal/cli/pair.go`, and `internal/relay/cli.go` that can stale
`Config.Paths`.

### 3.4 Path diagnostics command

```text
mcremote paths
mcremote paths --json
mcrelay  paths
mcrelay  paths --json
```

Print resolved ConfigDir, ConfigFile, DataDir, StateDir, CacheDir, RuntimeDir,
AdminSocket, InstanceKey, and any diagnostics (e.g. ignored relative XDG).
Honor the same flag/env overrides as `serve`. No filesystem mutation.

---

## 4. Intentionally absent: migration

There is **no** §4 migration design in this plan.

- No `internal/pathmigration`.
- No `migrate-paths` command.
- No layout markers under StateDir.
- No dual-tree conflict resolution.

If a future owner decision introduces an installed base that must move, that
requires a new MADR. Under the current greenfield constraint, implementing
migration would be pure cost and a second source of path bugs.

---

## 5. Phase-by-phase implementation

```text
Phase 0 → Phase 1 appdirs/fsutil → Phase 2 loaders + paths cmd
                                      │
                                      ├→ Phase 3 service XDG convergence
                                      ├→ Phase 4 runtime IPC + atomic single-file
                                      └→ Phase 5 engine registry
Phase 6 build policy → Phase 7 native CI/release → Phase 8 docs/acceptance
```

### Phase 0 — Decision lock and hermetic baseline

- Accept A1–A6 (this revision). MADR 0059 already matches: XDG-everywhere,
  greenfield (no D7 migration), D4 exports validated XDG, diagnostics → `paths`.
- Fix `TestRenderPlistLogsNotTmp` by decoding fields; inject PATH/home.
- Record clean `make test`, `make race`, and four Darwin no-tag cross-builds.

**Gate:** behavior unchanged; baseline green independent of ambient PATH.
**Commit:** docs/test-only.

### Phase 1 — `appdirs` and `fsutil` without caller cutover

- Implement products, XDG roots (both GOOS), pure resolve, instance key,
  diagnostics, private-dir validation, runtime validation, atomic writes,
  locks.
- Keep production callers on current `internal/xdg` until Phase 2.
- Tests: full XDG set/partial/unset/relative matrices on both GOOS via injected
  roots; UID/mode/symlink errors; temp fallback; socket-length bound;
  deterministic instance keys; atomic cleanup/failure/concurrency.

**Gate:** new package tests and race pass; no operator-visible path change.

### Phase 2 — One effective-path pipeline + `paths`

- Refactor `internal/config/load.go` and `internal/relay/fileconfig.go` to
  attach ConfigFile/Paths/Diagnostics and finalize once.
- Delete `internal/relay.AbsPath` after replacement coverage.
- Remove redundant CLI post-load path writes.
- Replace `internal/xdg` callers; delete package after cutover.
- Add `paths` / `paths --json` for both products.

Tests: source precedence, missing optional default config, explicit missing
config failure, YAML-relative TLS/CWD, env-relative rejection, CLI relative
canonicalization, provider bin untouched, identical foreground resolution.

**Gate:** config/CLI tests + new contract tests pass.

### Phase 3 — Service convergence (still XDG)

- Service setup consumes `appdirs.Paths` for config/data/state/cache/runtime.
- Darwin plist exports **validated absolute** XDG roots from resolved Paths
  (config/data/state/cache; runtime when applicable).
- Add plist integer `Umask=63`.
- Keep LaunchAgent stdio under `~/Library/Logs/<label>/`.
- Linux systemd continues to export the same validated XDG roots.
- Update default YAML comments, CLI hints, deploy plists, helper scripts.

**Gate:** parsed unit/plist assertions; setup twice is byte-idempotent;
foreground and service share ConfigFile/DataDir/RuntimeDir.

### Phase 4 — Runtime admin IPC and single-file persistence

- Admin APIs take explicit socket path (or `appdirs.Paths`), not DataDir.
- Validate runtime parent and `sun_path` capacity.
- `Lstat`; reject non-socket/wrong-owner; shutdown removal only if inode still
  matches this listener.
- Adopt `fsutil.WriteFileAtomic` in auth/session/service single-file stores.
- Preserve certificate pair recovery per A5.

Tests: two custom DataDirs simultaneously, stale/live sockets, symlink
refusal, overlength path, client/daemon resolution, atomic failure.

**Gate:** admin operations work in foreground and service smoke tests; no
socket under DataDir.

### Phase 5 — Cross-platform engine registry

```go
type EngineRecord struct {
    Schema int
    ID, Provider, InstanceKey string
    PID, PGID int
    Owner, StartToken, DaemonID string
    CreatedAt time.Time
}
```

- Linux start token: `/proc/<pid>/stat` field 22.
- Darwin start token: `SysctlKinfoProc` / `P_starttime` via `x/sys/unix`.
- Register immediately after `cmd.Start`; publication failure kills and reaps
  the group. Remove only matching ID/start token after `Wait`.
- Wire `httpagent`, `acphttp`, `codex` (+ OpenCode/Goose wrappers).
- Rewrite orphan reaping and `mcremote engines` to verify PID+token+owner.
- Retain env markers and Linux Pdeathsig as defense-in-depth only.

**Gate:** lifecycle, SIGKILL, PID reuse, malformed record, concurrent
instances covered on Linux and Darwin test builds.

### Phase 6 — Darwin-native build policy

- Tags: Linux `netgo,osusergo`; Darwin **no** resolver/user tags.
- Fix Makefile “fully static” Darwin wording.
- `make verify-build-metadata` asserts tags via `go version -m`.
- Build both products for Darwin arm64 and amd64 on relevant changes.
- Native CI logs resolver selection with `GODEBUG=netdns=1`; split-DNS remains
  documented manual acceptance, not a flaky public-runner gate.

**Gate:** no-tag Darwin builds identify the current user on native macOS;
resolver not forced pure-Go.

### Phase 7 — Native CI, signing, notarization, release

- `macos-15` (arm64) and `macos-15-intel` jobs with explicit labels.
- gofmt, tidy, vet, tests/race as supported, appdirs/admin/process tests,
  `plutil -lint`, uniquely labeled LaunchAgent smoke.
- Activate `darwin/arm64` and `darwin/amd64` release artifacts.
- Sign with Developer ID Application, hardened runtime, timestamp;
  `notarytool submit --wait`; document ZIP vs staple (PKG/DMG only if a later
  packaging decision requires offline tickets).
- `spctl` + checksums on final names. Tag releases fail closed without secrets;
  PRs run unsigned native tests.

**Gate:** release downloads and runs native artifacts on both architectures;
Linux + macOS (+ mobile if required by existing pipeline) all green.

### Phase 8 — Documentation and final parity acceptance

Update `README.md`, `docs/config.md`, `docs/config-mcrelay.md`,
`docs/ops-mcrelay.md`, examples, deploy assets. Historical MADRs stay
historical; add 0059 pointers rather than rewriting history.

Document:

- XDG table for both OSes (including that Darwin uses XDG, not Application
  Support, by product policy);
- override/source rules and no tilde/`${HOME}` expansion;
- `paths --json`;
- runtime socket discovery and instance keys;
- ACME durability under DataDir;
- LaunchAgent session lifetime and privileged-port constraints;
- DNS/build-tag policy;
- minimum macOS version and signing verification.

**Gate:** acceptance matrix in §7; MADR/plan status flips only with evidence.

---

## 6. PR and commit breakdown

| PR | Scope | Must not include |
|---|---|---|
| 1 | A1–A6 docs lock (MADR+plan already aligned) + hermetic plist test | Runtime behavior change |
| 2 | appdirs/fsutil and exhaustive XDG tests | Caller cutover |
| 3 | config/relay finalization, xdg removal, `paths` command | Service plist semantics beyond needed tests |
| 4 | service XDG export convergence, Umask, deploy assets | Admin/engine changes |
| 5 | runtime admin socket + atomic single-file adoption | Engine registry |
| 6 | engine registry and Darwin identity | Build/release workflow |
| 7 | target-specific build policy and metadata checks | Signing secrets |
| 8 | native macOS CI and LaunchAgent smoke | Publishing changes |
| 9 | signed/notarized Darwin release archives | App bundle/SMAppService |
| 10 | docs, live acceptance evidence, status updates | Unrelated cleanup |

Each Go PR runs `make pre-add-check FILES="..."` before staging, then
`make test` and `make race`. Do not use `git commit -m`; the repository hook
generates the message.

---

## 7. Final acceptance matrix

| Area | Linux required evidence | macOS required evidence |
|---|---|---|
| Path defaults | XDG set/unset/invalid-relative matrix | **Same algorithm** with injected homes + native filesystem smoke |
| Overrides | flag/env/YAML precedence → absolute | identical |
| New install | XDG config/data/state created as needed | **same paths** (`~/.config`, `~/.local/share`, …) |
| Migration | none | **none** (greenfield) |
| Service equivalence | unit env XDG roots match resolved Paths | plist XDG roots match resolved Paths |
| Idempotence | second setup no unintended writes | identical |
| Runtime IPC | private instance socket; two instances | same + socket length / replacement race |
| Persistence | atomic failure/concurrency/recovery | same; case-sensitive APFS when available |
| Engines | lifecycle/PID reuse/orphan | same using KinfoProc start token |
| DNS/user lookup | pinned Linux pure-Go policy | no forced tags; native resolver/user smoke |
| Service lifecycle | install/restart/status/remove + linger | bootstrap/kickstart/print/bootout/remove |
| Diagnostics | `paths --json` matches serve resolution | identical |
| Release | executable/checksum/version | arm64+amd64 signature/notary/spctl/checksum/version |
| Logout | documented linger behavior | explicit accepted LaunchAgent exception |
| Privileged ports | capability/proxy/DNS-01 guidance | proxy/redirection/DNS-01 guidance |

### Release-blocking definition of parity

Parity is complete when every non-excepted row passes on **native** runners.
Cross-compilation cannot close a macOS cell. Accepted mechanism
constraints—LaunchAgent logout lifetime and unprivileged ports—must stay
visible in CLI/docs and must not be described as implemented parity.

---

## 8. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Operators expect Application Support on macOS | Document product policy in README/config; `paths` shows actual roots; reverse-DNS remains only for launchd label/logs |
| Relative `$XDG_*` silently mis-rooted today | Reject relative values with diagnostic; test matrix |
| Multiple daemon collision | DataDir-derived instance namespace for runtime + engines |
| PID reuse kills unrelated process | Record ID + PID + OS start token + owner verification |
| Service regeneration drops manual plist keys | Decode known schema; refuse unknown keys before mutation |
| PrivateTmp hides Linux fallback socket | Prefer/validate XDG runtime or `/run/user/$UID`; fail rather than bind an inaccessible private temp |
| Darwin socket path too long | Short runtime base, hashed instance, platform `sun_path` check |
| Certificate recovery regression | Preserve stable `.new` journal + failure-injection suite |
| Child CLI ignores Library but needs shared roots | Export validated XDG from LaunchAgent (A1/XDG-everywhere) |
| Native CI network flakes | Deterministic build-tag checks; split-DNS as controlled acceptance |
| Notarization secret exposure | Protected tag env, ephemeral keychain, no secret in logs/artifacts |

---

## 9. Review checklist

- [x] Accept **A1**: XDG layout on Linux **and** Darwin; no Application Support
      product defaults; no `os.UserConfigDir` for product paths on Darwin.
- [x] Accept **A2**: DataDir-derived instance key for runtime and engine state.
- [x] Accept **A3**: no `netgo` **or** `osusergo` on Darwin.
- [x] Accept **A4**: no `${HOME}`/tilde interpolation.
- [x] Accept **A5**: shared single-file atomic helper; certificate pair remains
      a specialized recovery transaction.
- [x] Accept **A6**: `paths` / `paths --json` diagnostics; no general `doctor`
      in this scope.
- [x] Confirm **no migration** package, command, layout marker, or dual-tree
      detection (greenfield).
- [x] Confirm LaunchAgent **exports validated absolute XDG** roots matching
      resolved Paths.
- [ ] Confirm generated plist with unknown/manual keys fails before mutation.
- [ ] Confirm signed/notarized ZIP is sufficient, or separately authorize
      PKG/DMG for stapled offline tickets.
- [x] Confirm LaunchAgent/logout and privileged-port exceptions remain accepted.
- [x] Confirm MADR 0059 and this plan stay in lockstep if further amendments land.
- [x] Approve phased PR order and release-blocking acceptance matrix.
