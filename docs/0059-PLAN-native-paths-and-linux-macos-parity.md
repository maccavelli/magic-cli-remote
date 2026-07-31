# MADR 0059 — Implementation plan: native paths and Linux/macOS parity

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to
[MADR 0059](0059-MADR-native-paths-and-linux-macos-parity.md). This is the
review and build plan: it re-checks the MADR against the current tree, corrects
underspecified decisions, names concrete APIs and files, orders changes so no
installation is silently forked, and defines native acceptance gates.

- **Status:** Proposed — implementation must not begin past Phase 1 until the
  decision amendments in §1.3 are accepted.
- **Date:** 2026-07-31
- **Scope:** `mcremote`, `mcrelay`, shared path/filesystem packages, config
  loading, service rendering, migration, admin IPC, engine supervision,
  Makefile, CI, Darwin release packaging, and operator documentation.
- **Non-goals:** privileged LaunchDaemon installation, App Store packaging,
  `SMAppService`, direct unprivileged binding to ports 80/443, or pretending a
  LaunchAgent survives logout.
- **Standards:** project `AGENTS.md`; Go pre-add gate (`gofmt`, `golint`,
  `govulncheck`); `make test` and `make race` before a commit.

---

## 1. Reassessment of MADR 0059

### 1.1 Overall judgment

The MADR's central finding is correct: Darwin compilation succeeds, but the
tree does not yet have functional parity. The native directory decision,
explicit migration requirement, native resolver requirement, Darwin engine
recovery, native CI, and signed/notarized release requirements should stand.

The implementation must refine six points before the MADR is accepted. These
are correctness changes, not optional polish.

### 1.2 Verified codebase facts

| Fact | Concrete evidence | Consequence |
|---|---|---|
| Config/data defaults are Linux-shaped on every OS | `internal/xdg/dirs.go` joins `.config` and `.local/share`; `internal/config/load.go` and `internal/relay/fileconfig.go` consume it | macOS never reaches Go/Apple-native defaults |
| Relative XDG roots are accepted | `ConfigHomeFor` / `DataHomeFor` join any non-empty value | Violates XDG's absolute-path requirement |
| Directory logic is duplicated | `internal/xdg/dirs.go`; `xdgConfigHome`, `xdgDataHome`, `xdgCacheHome`, `defaultConfigPath`, and config-dir creation in `internal/cli/service/setup.go` | Foreground and service behavior can drift |
| Service paths are normalized differently | `service.normalize` converts config/data to absolute; foreground loaders do not finalize all filesystem fields | The same YAML can resolve differently by working directory |
| Darwin plist exports Linux roots | `internal/cli/service/plist_render.go` injects `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, and `XDG_CACHE_HOME` | mcremote and child CLIs can differ between shell and LaunchAgent |
| Admin IPC is data-relative | `admin.SocketPath(dataDir)` and every `Serve`/`Call`/`NotifyDisconnect` caller | Moving to one product socket would collide for custom data dirs or multiple instances |
| Stale socket cleanup follows and removes by path | `admin.Serve` uses `Stat`, failed ping, then `Remove`; shutdown also removes by name | Unexpected file types, replacement races, and symlink redirection are not rejected |
| Auth/session writes use predictable temporary names | `auth.writeFileAtomic` and `session.Store.atomicWrite` use `path + ".tmp"` | Concurrent writers and stale/symlinked staging entries are avoidable risks |
| Certificate staging is intentionally recoverable | `certs.writePairAtomic`, `promoteNewPair`, and `cleanupOrphanNew` use stable `.new` files under a certificate lock | It must not be blindly replaced by a single-file helper |
| Engine discovery is Linux-only | `internal/cli/engines.go` rejects non-Linux; `owner_other.go` cannot enumerate environments or verify PID reuse | `engines` and startup orphan reaping are nonfunctional on macOS |
| Three engine launch sites require tracking | `httpagent.startServer`, `acphttp.startServer`, and `codex.startEngine` stamp environment markers after configuring process groups | Registry integration must reach OpenCode, Goose, and Codex wrappers |
| Build tags force non-native Darwin behavior | Makefile sets `netgo,osusergo` for all targets | Darwin bypasses native DNS and native Directory Services-backed user lookup |
| Release/CI are Linux-only | Go job uses Ubuntu; release `PLATFORMS` enables only `linux/amd64` | Cross-build success is the only present Darwin evidence |
| No `doctor` command exists | mcremote root has serve/version/pair/setup-service/engines; mcrelay has serve/setup-service/version | MADR diagnostics cannot depend on a nonexistent command |

### 1.3 Required MADR amendments for approval

| ID | MADR text to refine | Approved implementation decision |
|---|---|---|
| **A1** | Darwin ConfigDir is the application root | Use `~/Library/Application Support/<bundle-id>/config`; keep sibling `data` and `state` directories. This prevents config files from colliding with internal directory names and maps legacy config as one tree. |
| **A2** | Runtime is `<product>/admin.sock` | Namespace runtime and engine state by a stable instance key derived from the resolved absolute DataDir. Default and custom instances cannot steal each other's socket or records. |
| **A3** | Omit `netgo` on Darwin | Omit **both** `netgo` and `osusergo`. Go 1.26 provides Darwin syscall-backed native DNS and `os/user` implementations even with `CGO_ENABLED=0`. Audit builds for arm64/amd64 already succeed with no tags. |
| **A4** | Accept documented `${HOME}` interpolation | Do not add interpolation. Config-relative and absolute paths are sufficient; literal `$`/`${HOME}` expansion introduces another parser and source-dependent behavior. `~` also remains literal. |
| **A5** | One helper replaces every temporary-file transaction | Use one helper for single-file replacement. Preserve the certificate pair's stable recovery journal, but make its private staging writes unique before publishing `.new`. |
| **A6** | `doctor` reports constraints | Update help/docs and make `migrate-paths --dry-run --json` expose path diagnostics. A general `doctor` command is separate scope. |

### 1.4 Additional constraint: test hermeticity

The focused baseline suite passes with a sanitized `PATH`, but currently fails
in the ambient development environment at `TestRenderPlistLogsNotTmp`. That
test searches the entire plist for `/tmp/`; the inherited PATH contains a valid
Codex temporary component even though both log fields are under `Library/Logs`.
Replace substring tests with parsed-field assertions and inject roots/env.

Verified during this reassessment:

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

| Product | Short name | Darwin bundle/launchd identifier |
|---|---|---|
| mcremote | `mcremote` | `com.magiccliremote.mcremote` |
| mcrelay | `mcrelay` | `com.magiccliremote.mcrelay` |

| Class | Linux | macOS native |
|---|---|---|
| ConfigDir | `$XDG_CONFIG_HOME/<product>` or `~/.config/<product>` | `~/Library/Application Support/<id>/config` |
| ConfigFile | `<ConfigDir>/config.yaml` | same |
| DataDir | `$XDG_DATA_HOME/<product>` or `~/.local/share/<product>` | `~/Library/Application Support/<id>/data` |
| StateDir | `$XDG_STATE_HOME/<product>` or `~/.local/state/<product>` | `~/Library/Application Support/<id>/state` |
| CacheDir | `$XDG_CACHE_HOME/<product>` or `~/.cache/<product>` | `~/Library/Caches/<id>` |
| LogDir | journald; explicit file fallback under StateDir | `~/Library/Logs/<id>` |
| RuntimeBase | valid `$XDG_RUNTIME_DIR/<product>`; else valid `/run/user/$UID/<product>`; foreground-only secure temp fallback | `$TMPDIR/<product>-<uid>` (or `/tmp` fallback from `os.TempDir`) |
| RuntimeDir | `<RuntimeBase>/<instance-key>` | same |
| Admin socket | `<RuntimeDir>/admin.sock` | same |
| Engine records | `<StateDir>/instances/<instance-key>/engines` | same |
| Operation temp | `MkdirTemp` or same-directory `CreateTemp` | same |

`tls.letsencrypt.cache_dir` remains a legacy field name for durable CertMagic
account/certificate storage. Its default remains `<DataDir>/acme`; it never
uses CacheDir.

### 2.2 Instance identity

Derive `InstanceKey` from the final absolute, cleaned DataDir:

```text
instance-key = first 16 lowercase hex chars of SHA-256(clean-absolute-data-dir)
```

The hash contains no secret. DataDir—not config path or service label—is the
identity boundary because devices, pair codes, sessions, managed TLS, admin
operations, and daemon ownership all converge there. Daemon and CLI must call
the same function after all overrides. Never use PID in a durable namespace.

### 2.3 Path-source rules

| Source | Relative value behavior |
|---|---|
| CLI `--config`, `--data-dir`, TLS path flags | Resolve once against invocation CWD and store absolute |
| Product config/data environment variables | Reject if relative |
| YAML filesystem values | Resolve against the directory containing the loaded config file |
| Default | Resolver returns absolute |
| Provider `bin` | Do not path-normalize; a basename intentionally uses PATH |

Filesystem YAML fields are DataDir, TLS cert/key, ACME storage, and every
provider `default_cwd`. `directory_url`, Route53 profile, and provider binaries
are not filesystem paths.

---

## 3. Target architecture and APIs

### 3.1 `internal/appdirs`

Create:

```text
internal/appdirs/
  product.go          Product definitions and validation
  roots.go            production root discovery and injectable test roots
  paths.go            pure layout construction and InstanceKey
  detect.go           read-only legacy/native/marker selection
  ensure_unix.go      private leaf/type/owner/mode validation
  runtime_unix.go     runtime validation and Unix socket length
  paths_test.go
  detect_test.go
  ensure_unix_test.go
```

Public internal API:

```go
type Product struct { Name, Identifier string }
type Layout uint8 // Legacy, Native
type Roots struct { Home, Config, Data, State, Cache, Runtime, Temp, Logs string }
type Paths struct {
    Product Product; Layout Layout; Home, AppRoot string
    ConfigDir, ConfigFile, DataDir, StateDir, CacheDir string
    LogDir, RuntimeBase, RuntimeDir, AdminSocket string
    EngineRegistryDir, TempBase, InstanceKey string
}
type Diagnostic struct { Code, Message string }

func SystemRoots(product Product) (Roots, []Diagnostic, error)
func Candidates(product Product, roots Roots) Candidates
func Detect(c Candidates) (Selection, error)            // read-only
func Resolve(c Candidates, selection Selection) Paths    // pure
func WithDataDir(Paths, string) (Paths, error)            // recompute instance paths
func EnsurePrivateDir(string) error
func ValidateRuntimeDir(string) error
```

Do not make `runtime.GOOS` the only test seam. Tests inject `Roots` and layout;
production discovery alone branches by GOOS.

### 3.2 Filesystem primitives

Create `internal/fsutil/atomic.go`, `sync.go`, and Unix lock helpers:

```go
type AtomicOptions struct { Perm os.FileMode; SyncFile, SyncDir bool }
func WriteFileAtomic(path string, data []byte, opts AtomicOptions) error
func WithLock(path string, timeout time.Duration, fn func() error) error
```

`WriteFileAtomic` uses `CreateTemp(filepath.Dir(path), ".<base>-*")`, exact
chmod, optional file sync, close, rename, optional best-effort directory sync,
and cleanup on every failure. It rejects a symlink final target where the call
site requires a regular owned file.

Adopt it in `auth/store.go`, `session/store.go`, service config/unit/plist
writes, migration journals, and engine records. Extracting auth/cert locks into
the shared helper is allowed only with existing timeout/error semantics intact.

For `certs.writePairAtomic`, retain `.new` as a recovery protocol. Write each
payload to a unique file, then atomically publish that file to stable `.new`;
keep `promoteNewPair` and `cleanupOrphanNew` tests.

### 3.3 Effective config metadata

Add non-Viper fields to both `config.Config` and `relay.FileConfig`:

```go
ConfigFile  string        `mapstructure:"-"`
Paths       appdirs.Paths `mapstructure:"-"`
Diagnostics []appdirs.Diagnostic `mapstructure:"-"`
```

Loading order is fixed:

1. resolve/canonicalize explicit config flag or env;
2. detect layout when config is not explicitly decisive;
3. read YAML;
4. bind flags/env and unmarshal;
5. determine origin for each filesystem field (`Flag.Changed`, env present,
   `v.InConfig`, default);
6. normalize according to §2.3;
7. recompute `Paths` from final DataDir;
8. validate config once;
9. normalize TLS once.

Remove the post-Load filesystem overrides in `internal/cli/serve.go`,
`internal/cli/pair.go`, and `internal/relay/cli.go`; otherwise `Config.Paths`
can become stale. Retain only genuinely special non-path transformations.

### 3.4 Layout selection

Darwin selection precedence:

1. explicit config/data overrides;
2. a valid completed native layout marker;
3. native-only populated evidence;
4. legacy-only populated evidence (continue legacy and emit one warning);
5. neither populated (new install uses native);
6. both populated: compare mapped manifests; equivalent selects native,
   different fails closed with migration guidance.

Legacy evidence is `~/.config/<product>/config.yaml` or meaningful content in
`~/.local/share/<product>`. Ignore stale `admin.sock`, lock files, and known
temporary staging entries. A directory created only for a migration marker is
not native config/data evidence.

Linux never performs XDG-to-native migration. Invalid relative XDG roots are
ignored with a diagnostic and specification fallback. Runtime root additionally
requires current UID ownership, directory type, and mode `0700`.

---

## 4. Migration design

### 4.1 CLI contract

Add the same command to both binaries:

```text
<product> migrate-paths                 # dry-run by default
<product> migrate-paths --dry-run --json
<product> migrate-paths --apply
<product> migrate-paths --apply --source legacy|native  # conflict only
<product> migrate-paths --apply --service-label <label>
<product> migrate-paths --apply --no-service
```

No command deletes the legacy tree. Cleanup is a later, separately confirmed
operation after the native installation has run successfully.

### 4.2 Shared package

Create `internal/pathmigration/` with detector, manifest, journal, copier,
service coordination interface, and tests. A versioned journal lives at
`<StateDir>/migration-v1.json`; the lock is `<StateDir>/.migration.lock`.

Journal phases are `planned`, `service_stopped`, `config_published`,
`data_published`, `service_rewritten`, `marker_written`, `service_restarted`,
and `complete`. Each phase is atomically written. Re-running validates
manifests and resumes; it never repeats a destructive step by assumption.

### 4.3 Apply algorithm

1. Resolve candidates without creating config/data.
2. Acquire the migration lock.
3. Re-detect after lock; reject symlinks, special files, wrong owner, and
   source changes.
4. Inspect the installed default/custom LaunchAgent when service coordination
   is enabled. Record loaded/enabled/running state.
5. Decode only plists produced by this project. Reconstruct `service.Options`.
   Unknown/manual keys cause a pre-mutation refusal, not silent loss.
6. Boot out a running agent without disabling it; verify it is gone.
7. Copy legacy config to a unique staging directory inside native AppRoot,
   accepting only regular files/directories. Hash size+content manifests.
8. Publish the config directory by same-filesystem rename.
9. Repeat for DataDir, excluding `admin.sock`, locks, `.tmp`, and incomplete
   certificate staging. Preserve ACME, devices, pair codes, sessions, and TLS.
10. Rewrite only exact legacy default config/data arguments in reconstructed
    service options; render, `plutil -lint`, and atomically replace the plist.
11. Write a marker containing schema, product, source/destination, manifests,
    timestamp, and binary version.
12. Restart only if it was running; retain prior enabled state.
13. Verify config load, data manifest, admin ping for mcremote when running, and
    `launchctl print` state. Mark complete.

If config contains a custom DataDir, copy no external data and report it as an
intentional override. If it explicitly names the legacy default, leave the
override unchanged unless the user selects an explicit rewrite; never rewrite
arbitrary YAML text. Legacy logs remain as historical files and are reported,
not merged.

### 4.4 Failure and rollback rules

- Failure before marker: legacy remains authoritative; staged/native partials
  are journaled and a rerun resumes or verifies them.
- Failure after marker: native remains authoritative; never automatically fall
  back to stale legacy data.
- Service restart failure does not roll data back; print exact `launchctl`
  recovery commands and paths.
- Both populated and different requires `--source`; default is refusal.
- Byte equivalence compares mapped ConfigDir and DataDir trees, not StateDir,
  CacheDir, RuntimeDir, or logs.

---

## 5. Phase-by-phase implementation

```text
Phase 0 → Phase 1 appdirs/fsutil → Phase 2 loaders → Phase 3 migration
                                      │                 │
                                      ├→ Phase 5 IPC    └→ Phase 4 default/service switch
                                      └→ Phase 6 engine registry
Phase 7 build policy → Phase 8 native CI/release → Phase 9 docs/acceptance
```

### Phase 0 — Decision lock and hermetic baseline

- Accept A1–A6 and patch MADR 0059 accordingly.
- Fix `TestRenderPlistLogsNotTmp` by decoding fields; inject PATH/home.
- Add a `make test-go-focused` only if useful; do not weaken existing gates.
- Record clean `make test`, `make race`, and four Darwin no-tag cross-builds.

**Gate:** current behavior unchanged; baseline is green independent of ambient
PATH. **Commit:** docs/test-only.

### Phase 1 — `appdirs` and `fsutil` without switching defaults

- Implement products, injected roots, candidate paths, pure resolve, instance
  key, diagnostics, private-directory validation, runtime validation, atomic
  writes, and locks.
- Keep callers on legacy behavior until Phase 3 is available.
- Test Linux all-XDG/partial/unset/relative matrices; Darwin native/legacy
  candidates; UID/mode/symlink errors; temp fallback; socket-length bound;
  deterministic instance keys; atomic cleanup/failure/concurrency.

**Gate:** new package tests and race pass; no operator-visible path change.

### Phase 2 — One effective-path pipeline

- Refactor `internal/config/load.go` and `internal/relay/fileconfig.go` to attach
  ConfigFile/Paths/Diagnostics and finalize once.
- Delete `internal/relay.AbsPath` after its replacement is covered.
- Remove redundant CLI post-load path writes.
- Replace `internal/xdg` callers; retain a temporary compatibility facade only
  if needed for one commit, then delete package/tests.
- Refactor service default config, systemd unit paths, docs hints, and log paths
  to consume injected `appdirs.Paths`.

Tests cover every source precedence, missing optional default config, explicit
missing config failure, YAML-relative TLS/CWD, env-relative rejection, CLI
relative canonicalization, provider bin untouched, and identical foreground /
service effective paths.

**Gate:** all current config and CLI tests plus new contract tests pass.

### Phase 3 — Migration before default switch

- Implement `internal/pathmigration` and both Cobra commands.
- Implement generated-plist decode/reconstruction under
  `internal/cli/service/installed_plist.go` with unknown-key refusal.
- Add fixtures for empty, legacy-only, native-only, equivalent dual, conflicting
  dual, stale socket, custom data, interrupted phases, service stopped/running,
  invalid symlink, and modified source during copy.
- Dry-run output lists every source, destination, exclusion, service action,
  warning, and conflict; JSON has a versioned schema.

**Gate:** migration replay is idempotent under injected failures at every journal
phase. No source deletion test exists because source deletion is out of scope.

### Phase 4 — Native default and service convergence

- Enable Darwin selection rules; new installations select native, legacy-only
  remains legacy with warning until migrated.
- Remove synthesized XDG keys from Darwin plist unless supplied explicitly via
  `--env`; validate explicit XDG values as absolute pass-through values.
- Add plist integer `Umask=63`.
- Change native log path to bundle identifier.
- Linux systemd continues to export validated XDG roots from appdirs.
- Update default YAML comments, CLI hints/examples, deploy plists, and
  `scripts/start-mcremote-grok.sh`.

**Gate:** parsed plist/unit assertions for both products; setup twice is
byte-idempotent; foreground and service load the same ConfigFile/DataDir.

### Phase 5 — Runtime admin IPC and single-file persistence

- Change admin APIs to accept an explicit socket path (or `appdirs.Paths`), not
  DataDir; update daemon and pair callers.
- Validate runtime parent and `sun_path` capacity using the platform's
  `RawSockaddrUnix.Path` length.
- Use `Lstat`; reject non-socket/wrong-owner entries. Before shutdown removal,
  verify the path still names the socket inode created by this listener.
- Adopt `fsutil.WriteFileAtomic` in auth/session/service single-file stores.
- Preserve and harden certificate pair recovery per A5.

Tests cover two custom DataDirs simultaneously, stale/live sockets, regular
file and symlink refusal, path replacement before shutdown, overlength path,
mode/owner, client/daemon resolution, cancellation cleanup, and atomic failure.

**Gate:** admin revoke/prune works in foreground and LaunchAgent smoke tests on
both OSes; no socket appears under DataDir.

### Phase 6 — Cross-platform engine registry

Create in `internal/procutil`:

```go
type EngineRecord struct { Schema int; ID, Provider, InstanceKey string; PID, PGID int; Owner, StartToken, DaemonID string; CreatedAt time.Time }
type EngineTracker interface { Register(EngineRecord) (Lease, error); Remove(Lease) error }
type Registry struct { /* records are stored below the instance engine directory */ }
```

- Add `process_identity_linux.go` using `/proc/<pid>/stat` field 22.
- Add `process_identity_darwin.go` using x/sys/unix
  `SysctlKinfoProc("kern.proc.pid", pid)` and `P_starttime`.
- Register immediately after `cmd.Start`; if record publication fails, kill and
  reap the group and fail startup. Remove only the matching ID/start token after
  `Wait`.
- Inject a tracker through `httpagent`, `acphttp`, `codex`, and their
  OpenCode/Goose wrappers; tests use an in-memory fake.
- Rewrite `ReapOrphanEngines` and `mcremote engines` to enumerate all instance
  directories, verify PID+start token+owner before signaling, quarantine corrupt
  records, and refuse unverifiable records.
- Retain environment markers and Linux Pdeathsig as defense-in-depth, not kill
  authorization.

**Gate:** Linux and native Darwin tests cover graceful exit, daemon SIGKILL,
owner live/dead, PID reuse, malformed record, concurrent instances, register
failure, and `--reap`. No destructive action is based on PID alone.

### Phase 7 — Darwin-native build policy

- Make tags target-specific: Linux `netgo,osusergo`; Darwin no resolver/user
  tags; other targets explicit rather than inheriting Linux policy.
- Correct Makefile's “fully static” Darwin wording.
- Add `make verify-build-metadata` to assert Darwin binaries do not report
  `netgo`/`osusergo` via `go version -m` and Linux artifacts retain policy.
- Build both products for Darwin arm64 and amd64 on every relevant change.
- Native CI logs resolver selection with `GODEBUG=netdns=1`; localhost is a
  stable smoke test. Real tailnet/VPN split-DNS remains a documented
  self-hosted/manual release acceptance test, not a flaky public-runner test.

**Gate:** no-tag Darwin builds run and identify the current user on native macOS;
resolver is not forced pure-Go.

### Phase 8 — Native CI, signing, notarization, release

- Add arm64 job on `macos-15` and Intel job on `macos-15-intel` per the current
  [GitHub-hosted runner table](https://docs.github.com/en/actions/reference/runners/github-hosted-runners);
  keep labels explicit rather than `macos-latest`.
- Run gofmt, tidy check, vet, tests/race as supported, appdirs/migration/admin/
  process tests, `plutil -lint`, and a uniquely labeled live LaunchAgent smoke.
- Activate `darwin/arm64` and `darwin/amd64` for both products.
- Build/sign on macOS using an ephemeral keychain and Developer ID Application
  certificate. Use `codesign --force --options runtime --timestamp` and verify
  each binary with `codesign --verify --strict --verbose=2`.
- Package signed mcremote+mcrelay by architecture as ZIP, submit with
  `xcrun notarytool submit --wait`, and verify accepted status. Apple's
  [notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)
  notes that ZIP cannot be stapled directly; if offline-ticket stapling is
  required, add a signed PKG or DMG in a separately reviewed packaging phase.
- Run `spctl -a -vv -t exec` on extracted binaries and regenerate checksums over
  final published names. Tag releases fail closed when signing/notary secrets
  are absent; PRs run unsigned native tests.

**Gate:** release job downloads and executes native artifact on each
architecture, verifies version/signature/notary result/checksum, and publishes
only after Linux, macOS, Flutter, and Android gates succeed.

### Phase 9 — Documentation and final parity acceptance

Update `README.md`, `docs/config.md`, `docs/config-mcrelay.md`,
`docs/ops-mcrelay.md`, `docs/protocol-v1.md`, active examples in prior MADRs,
embedded defaults, CLI examples/help, and deploy assets. Historical MADRs stay
historical unless their status/current guidance explicitly conflicts; add a
0059 pointer rather than rewriting history indiscriminately.

Document native tables, override/source rules, migration/backup recovery,
runtime socket discovery, ACME durability, service/session lifetime, privileged
ports, DNS policy, minimum macOS version, signing verification, and legacy
cleanup as a separate confirmation.

**Gate:** execute the acceptance matrix in §7 and change MADR/plan status only
after every non-excepted row is evidenced.

---

## 6. PR and commit breakdown

| PR | Scope | Must not include |
|---|---|---|
| 1 | A1–A6 docs lock + hermetic plist test | Runtime behavior change |
| 2 | appdirs/fsutil and exhaustive tests | Caller migration/default switch |
| 3 | config/relay finalization and CLI override cleanup | Native default switch |
| 4 | migration dry-run/detector/manifests | `--apply` |
| 5 | migration apply/journal/service coordination | Source deletion |
| 6 | native default, plist/systemd convergence, Umask | Admin/engine changes |
| 7 | runtime admin socket + atomic single-file adoption | Engine registry |
| 8 | engine registry and Darwin identity | Build/release workflow |
| 9 | target-specific build policy and metadata checks | Signing secrets |
| 10 | native macOS CI and LaunchAgent smoke | Publishing changes |
| 11 | signed/notarized Darwin release archives | App bundle/SMAppService |
| 12 | docs, live acceptance evidence, status updates | Unrelated cleanup |

Each Go PR runs `make pre-add-check FILES="..."` before staging, then
`make test` and `make race`. Do not use `git commit -m`; the repository hook
generates the message.

---

## 7. Final acceptance matrix

| Area | Linux required evidence | macOS required evidence |
|---|---|---|
| Path defaults | XDG set/unset/invalid matrix | injected contract + native filesystem smoke |
| Overrides | flag/env/YAML precedence and absolute result | identical |
| New install | XDG config/data created once | App Support config/data created once |
| Existing install | no Linux migration/regression | legacy continue, dry-run, apply, replay, conflict |
| Service equivalence | rendered unit and foreground snapshot match | rendered plist and foreground snapshot match |
| Idempotence | second setup/migration no unintended writes | identical |
| Runtime IPC | private instance socket; two instances | same plus socket length and replacement race |
| Persistence | atomic failure/concurrency/recovery tests | same; case-sensitive APFS run |
| Engines | lifecycle/PID reuse/orphan tests | same using KinfoProc start token |
| DNS/user lookup | pinned Linux pure-Go policy | no forced tags; native resolver/user smoke |
| Service lifecycle | install/restart/status/remove + linger | bootstrap/kickstart/print/bootout/remove |
| Release | executable/checksum/version | arm64+amd64 signature/notary/spctl/checksum/version |
| Logout | documented linger behavior | explicit accepted LaunchAgent exception |
| Privileged ports | capability/proxy/DNS-01 guidance | proxy/redirection/DNS-01 guidance |

### Release-blocking definition of parity

Parity is complete only when every non-excepted row passes on native runners
and the migration has an exercised legacy fixture. Cross-compilation cannot
close a macOS cell. The two accepted mechanism constraints—LaunchAgent logout
lifetime and unprivileged ports—must remain visible in CLI/docs and cannot be
described as implemented parity.

---

## 8. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Silent second installation | Legacy detection and migration ship before default switch |
| Multiple daemon collision | DataDir-derived instance namespace |
| PID reuse kills unrelated process | Record ID + PID + OS start token + owner verification |
| Service regeneration drops manual plist keys | Decode known schema; refuse unknown keys before mutation |
| Interrupted two-tree migration | Lock, journaled phases, manifests, source retained |
| PrivateTmp hides Linux fallback socket | Prefer/validate XDG runtime or `/run/user/$UID`; service setup fails rather than using an inaccessible private temp fallback |
| Darwin socket path too long | Short runtime base, hashed instance, platform-derived `sun_path` check |
| Certificate recovery regression | Preserve stable `.new` journal and its failure-injection suite |
| Native CI becomes network-flaky | Deterministic build-tag checks; split-DNS as controlled acceptance |
| Notarization secret exposure | Protected tag environment, ephemeral keychain, no secret output/artifacts |
| Old service continues legacy config | Migration reconstructs/re-renders known generated plist before marker/restart |

---

## 9. Review checklist

- [ ] Accept A1: Darwin `.../<id>/config` subdirectory.
- [ ] Accept A2: DataDir-derived instance key for runtime and engine state.
- [ ] Accept A3: no `netgo` **or** `osusergo` on Darwin.
- [ ] Accept A4: no `${HOME}`/tilde interpolation.
- [ ] Accept A5: shared single-file atomic helper; certificate pair remains a
      specialized recovery transaction.
- [ ] Accept A6: migration diagnostics now; general doctor command out of scope.
- [ ] Confirm migration never deletes legacy source in this project.
- [ ] Confirm generated plist with unknown/manual keys fails before mutation.
- [ ] Confirm signed/notarized ZIP is sufficient, or separately authorize PKG/DMG
      for stapled offline tickets.
- [ ] Confirm LaunchAgent/logout and privileged-port exceptions remain accepted.
- [ ] Approve phased PR order and release-blocking native acceptance matrix.
