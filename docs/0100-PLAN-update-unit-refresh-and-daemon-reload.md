---
status: accepted
date: 2026-08-18
madr: "0100-MADR-update-unit-refresh-and-daemon-reload.md"
owner: Project Owner
target: v0.13.5
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Reconcile the service definition during `update`, and reload the manager before the restart

Associated MADR:
[0100-MADR-update-unit-refresh-and-daemon-reload.md](0100-MADR-update-unit-refresh-and-daemon-reload.md)

## Objective and Scope

Make `mcremote update` / `mcrelay update` deliver a service-definition fix the
way they already deliver a binary fix, and make them tell the service manager
about it.

**Done means:** on a systemd host, an `update` that lands a release whose unit
template changed leaves the installed unit matching the new template, with
`daemon-reload` run before the restart; a unit carrying baked flags, `--env`
secrets, or hand-added directives is never silently rewritten; a failed update
restores the old unit alongside the old binary; and `update` no longer fails on
hosts that have no service installed.

**In scope**

| Area | Files |
|---|---|
| Refresh implementation | `internal/cli/service/refresh.go` (new), `internal/cli/service/refresh_test.go` (new) |
| Command surface | `internal/cli/setup_service.go`, `internal/relay/cli.go`, their `_test.go` |
| Update sequence | `internal/update/service.go`, `internal/update/swap.go`, `internal/update/run.go`, `internal/update/swap_test.go`, `internal/update/run_test.go` |
| Wiring | `internal/cli/update.go`, `internal/relay/update.go` |
| Installed-ness probe | `internal/cli/service/control.go`, `internal/cli/service/control_test.go` |
| Installer | `scripts/install.sh`, `scripts/install_test.sh` |
| Docs | `docs/ops-linux-install.md`, `docs/ops-mcrelay.md`, `README.md`, `docs/0065-MADR-update-automation.md` (pointer only) |

**Out of scope, deliberately**

* **`doctor`'s `ProbeStatus`.** Its Linux `PlistPresent` is really "enabled"
  (`control.go:158-159`). Part C adds a separate `IsInstalled` using `LoadState`
  rather than changing `doctor`'s reporting inside a bug fix. Recorded in the
  MADR.
* **Custom `--unit-name` installs.** `update` has never managed them
  (`control.go:39`, `:61`, `:104`) and this plan does not change that.
* **`runit` / `s6` / `openrc` restarts from `update`.** Part C stops `update`
  failing there; teaching it to cycle those backends is separate work.
* **The mobile/APK update path.** Untouched.

## Prerequisites and Dependencies

### Defect locations, verified in the working tree 2026-08-18

| Ref | File | Line(s) | Current content |
|---|---|---|---|
| F1 | `internal/update/swap.go` | 37-137 | `SwapAndRestart` — swaps and cycles; never writes a definition |
| F1 | `internal/update/service.go` | 5-10 | `ServiceControl` = `IsActive` / `Stop` / `Start` only |
| F2 | `internal/cli/service/control.go` | 104 | `return runSystemctl("--user", "start", product+".service")` |
| F2 | `internal/cli/service/setup.go` | 348, 584 | the two places that *do* reload, for contrast |
| F3 | `internal/update/run.go` | 139 | `HealStart: true,` (unconditional) |
| F3 | `internal/update/swap.go` | 69, 118-121 | `wantUp := opts.WasActive \|\| opts.HealStart`; `Start` error is fatal |
| F3 | `internal/update/swap.go` | 93-108 | deferred restore that undoes the good swap |
| D | `scripts/install.sh` | 417-439 | upgrade branch: keeps the unit, advises `setup-service --force` at `:434` |

### Reference points the new code must match

| Concern | Existing implementation |
|---|---|
| Unit path | `filepath.Join(xdgConfigHome(), "systemd", "user", UnitName+".service")` — `setup.go:313-320` |
| Plist path | `$HOME/Library/LaunchAgents/<label>.plist` — `setup.go:405`, label from `LaunchdLabel` (`setup.go:164`) |
| Atomic write | `writeUnitAtomic(dir, path, body, mode)` — `setup.go:1034` |
| Mode rule | `0644`, or `0600` when `ExtraEnviron` is non-empty — `setup.go:325-328`, `setup.go:408-411` |
| Value quoting | `systemdQuote` — `setup.go:1011` (`%`→`%%`; escape and wrap when the value has space/tab/quote/backslash) |
| Managed env keys | `HOME USER LOGNAME PATH XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME XDG_CACHE_HOME XDG_RUNTIME_DIR` — template lines 42-50 |
| Marker line | ``# Managed by `{{.Product}} setup-service` / `{{.Product}} --setup-service`.`` — template line 1 |
| Test seams | `OverrideInstallOS`, `OverrideRunSystemctl`, `OverrideRunLaunchctl` (`setup.go:198-227`), `Options.UnitDir` |

### Tests that will need updating in the same commit

Run these first, so nothing is discovered late:

```bash
grep -rn "HealStart" internal/update/
grep -rn "FuncService{" internal/ --include=*.go
grep -rn "setupServiceFlags{" internal/ --include=*.go
```

Known now:

* `internal/update/swap_test.go:107` `TestSwapAndRestart_HealEnabledDown` — still
  valid; `HealStart` keeps its meaning, only its caller changes.
* `internal/cli/setup_service_test.go:42` `TestSetupServiceFlagsToOptionsCarriesAllFields`
  — extend when the flag struct grows.
* `scripts/install_test.sh:249` `unit was NOT rewritten` — must stay green. Its
  fixture unit is `# existing unit with different content`, which carries no
  managed marker, so the provenance rule keeps it: verdict `kept`.

### Environment

* Go toolchain per `go.mod`; `make pre-add-check` before every `git add`.
* A real Linux systemd-user host for Phase 0 and Phase 7 (the 0098/0099 EC2
  pattern). Everything else is provable on macOS through the override seams.

### Blocking dependencies

Phase 0 must complete before Phase 1 is written. It converts three code
readings into observations; if any comes back different, the design changes
before code does.

## Technical Design

### 1. Recovering `Options` from an installed unit

New in `internal/cli/service/refresh.go`:

```go
// recoverOptions rebuilds the Options that rendered unitBody, so a refresh can
// re-render with the current template without losing anything the operator
// baked in at setup time. Returns ok=false with a reason when the unit contains
// anything this package did not write.
func recoverOptions(product, unitName, unitBody string) (Options, string, bool)
```

Grammar, derived line by line from `mcremote.user.service.tmpl` /
`mcrelay.user.service.tmpl`:

| Unit line | Recovered field |
|---|---|
| `Description=<raw>` | `Description` (rendered unquoted, so read raw) |
| `WorkingDirectory=<q>` | `WorkingDirectory` |
| `ExecStart=<q-binary> serve [--config <q>] [--data-dir <q>] [--listen-host <q>] [--listen-port <n>] [--log-level <q>] [--log-format <q>]` | `Binary`, `ConfigPath`, `DataDir`, `ListenHost`, `ListenPort`, `LogLevel`, `LogFormat` |
| `Environment=<KEY>=<q>` where `KEY` is not a managed key | appended to `ExtraEnviron` as `KEY=<unquoted>` |
| `Environment=<KEY>=<q>` where `KEY` **is** a managed key | pinned into `renderEnv` (see §1a); never recomputed |
| anything else | ignored for recovery; screened by the provenance rule |

`<q>` is the inverse of `systemdQuote`: if the token starts and ends with `"`,
strip the quotes and replace `\\`→`\` then `\"`→`"`; finally replace `%%`→`%`
in every value, quoted or not.

`ExecStart` tokenisation walks the line character by character, tracking an
`inQuote` flag and honouring `\` escapes inside quotes, so a path containing a
space round-trips.  `UnitName` comes from the file's base name, `Product` from
the caller.

`ok=false` (with a human reason) when: the second token is not `serve`; a token
starts with `--` and is not one of the six known flags; a known flag has no
value; `--listen-port` is not an integer; or there is more than one `ExecStart=`
line.

### 1a. Pinning the environment block (0100 F4)

`render` reads `HOME`, `USER`, `PATH` and the `XDG_*` roots from the process
doing the rendering, not from `Options` (`setup.go:843-852`), and for `mcremote`
`servicePathEnv` (`setup.go:804-833`) derives PATH from `os.Getenv("PATH")`.
Phase 0 measured the consequence on wonder: the same binary renders a different
`Environment=PATH=` under a different caller, and drops
`Environment=XDG_RUNTIME_DIR=` when the caller has none. Recomputing would make
every refresh report a change and would rewrite the daemon's PATH to the
updating process's environment.

```go
// renderEnv pins the environment-derived values a unit was rendered with, so a
// refresh reproduces them instead of recomputing them from whoever runs it
// (MADR 0100 F4). nil means "compute", which is what Setup does.
type renderEnv struct {
    Home, User, Path                                       string
    XDGConfigHome, XDGDataHome, XDGStateHome, XDGCacheHome string
    XDGRuntimeDir string // empty means the line is omitted, as the template does
}

func renderWith(opts Options, env *renderEnv) (string, error)
```

* `render(opts)` becomes `renderWith(opts, nil)`; `Setup` is untouched.
* `renderPlistWith` is the launchd twin — its `EnvironmentVariables` dict is
  built from the same sources (`plist_render.go:78-86`), so it has the same
  defect and the same fix.
* Recovery fills `renderEnv` from the installed definition. A managed key that
  is **absent** there (an older template) falls back to the computed value and
  adds a warning naming the key.
* If the computed PATH contains an entry the pinned PATH lacks — a tool prefix
  added by a later release — the refresh emits
  `"PATH entries added in this release are not applied (…); re-derive with
  setup-service --force"` and leaves the value alone. Stability over freshness,
  stated in the MADR consequences.

### 2. The provenance rule

```go
// unitIsManaged reports whether unitBody is a file this package wrote and can
// reproduce. A refresh rewrites only a managed unit; anything else is reported
// and left alone.
func unitIsManaged(product, unitBody string) (bool, string)
```

* Line 1 equals the rendered marker for this product (template line 1).
* No `ExecStartPre=`, `ExecStartPost=`, `ExecStop=`, `ExecReload=`,
  `EnvironmentFile=` (leading-whitespace tolerant; `=` may be `=-`).
* No `[` section header other than `[Unit]`, `[Service]`, `[Install]`.
* Exactly one `ExecStart=`.

For launchd there is no comment line to carry a marker, so the plist equivalent
checks structure: `Label` equals `com.magiccliremote.<product>` and
`ProgramArguments[1]` equals `serve`. Recovery there reads `ProgramArguments`
(the same flag list, unquoted — `plist_render.go:52-68`, emitted at `:113`),
`WorkingDirectory`, and every `EnvironmentVariables` key not in the managed set
(`plist_render.go:121`).

### 3. `RefreshUnit`

```go
// RefreshOptions tunes a refresh. Zero value is the update-path behaviour.
type RefreshOptions struct {
    PrintOnly bool // render and report; write nothing
    NoReload  bool // skip daemon-reload (tests)
}

// RefreshResult is the machine-readable outcome. The JSON field names are a
// cross-version contract: `update` in release N parses what `setup-service
// --refresh --json` prints in release N+1. Add fields; never rename one.
type RefreshResult struct {
    Verdict    string   `json:"verdict"`    // none|unchanged|refreshed|kept
    Path       string   `json:"path"`
    BackupPath string   `json:"backup,omitempty"`
    Changed    bool     `json:"changed"`
    Reloaded   bool     `json:"reloaded"`
    Reason     string   `json:"reason,omitempty"`
    Warnings   []string `json:"warnings,omitempty"`
}

func RefreshUnit(opts Options, ro RefreshOptions) (RefreshResult, error)
```

Algorithm (`installOS` selects the path, exactly as `Setup` does at
`setup.go:294-303`):

1. Normalise product and unit name only — **not** the binary defaulting in
   `normalize` (`setup.go:703-719`), which would resolve a binary the unit may
   not use. Read the definition; missing → `{Verdict: "none"}`, return.
2. `unitIsManaged` → false: `{Verdict: "kept", Reason: …}`; still reload (step 6).
3. `recoverOptions` → not ok: `{Verdict: "kept", Reason: …}`; still reload.
4. Re-render with `renderWith(recovered, pinnedEnv)` / `renderPlistWith` and
   compare bytes. Equal → `{Verdict: "unchanged"}`.
5. Different → `PrintOnly` prints the new body and returns
   `{Verdict: "refreshed", Changed: true}` without writing. Otherwise copy the
   existing file to `<path>.prev` (same mode), `writeUnitAtomic` the new body
   with the mode rule from `setup.go:325-328`, `plutil -lint` on darwin
   (`setup.go:430`), and return
   `{Verdict: "refreshed", Changed: true, BackupPath: …}`.
6. Linux only: `runSystemctl("--user", "daemon-reload")` unless `ro.NoReload`;
   set `Reloaded`. A reload error is returned as an error only when a write also
   happened; otherwise it becomes a warning. macOS performs no reload —
   `Stop`/`Start` bootout/bootstrap re-reads the plist (`control.go:83-99`).
7. Warning, always evaluated: if the recovered `Binary` differs from
   `os.Executable()` (resolved through symlinks), append
   `"ExecStart points at X; the updated binary is Y"`. That is the case where an
   update swaps a binary the service does not run.

`RestoreUnitBackup(path, backup string) error` — rename `backup` back over
`path` and, on Linux, `daemon-reload`. Template-free, so the *old* binary can
call it during rollback.

### 4. Command surface

Both products' `setupServiceFlags` gain:

```go
refresh bool // --refresh
jsonOut bool // --json (only meaningful with --refresh)
```

```
--refresh   re-render the installed unit/plist from this binary's template,
            preserving the options baked into it; rewrite only if this binary
            wrote it. Reloads the systemd user manager.
--json      with --refresh, print the result as one JSON object
```

`runSetupService` branches on `f.refresh` before the `f.remove` branch and
prints either the JSON object or one human line per verdict:

```
service definition unchanged: /home/u/.config/systemd/user/mcrelay.service
service definition refreshed: … (previous kept at ….prev)
service definition kept: … — <reason>; refresh it with: mcrelay setup-service --force
no service definition installed for mcrelay — nothing to refresh
```

Exit 0 for all four; non-zero only on error. `--refresh` with `--remove` is a
usage error; with `--print-only` it is a dry run.

### 5. `internal/update` changes

`service.go`:

```go
// UnitRefresh is what a post-swap definition reconciliation did.
type UnitRefresh struct {
    Changed    bool
    Path       string
    BackupPath string
    Output     string // one human line, already formatted
}

// UnitRefresher reconciles the installed service definition with the one the
// freshly swapped binary carries. Implemented outside this package so update
// stays free of template knowledge (0065 P1).
type UnitRefresher interface {
    RefreshUnit(product, binary string) (UnitRefresh, error)
    RestoreUnit(product string, r UnitRefresh) error
}
```

`ServiceControl` gains `IsInstalled(product string) (bool, error)`;
`FuncService` gains `IsInstalledFn`, each nil-safe in the existing style
(`service.go:18-40`) so no current construction site breaks.

`swap.go` — `SwapOpts` gains `Refresher UnitRefresher` (nil ⇒ skipped, which
keeps all four existing tests unchanged). After the successful
`os.Rename(staged, dest)` and `Chmod`, and **before** the `Start` block at
`:118`:

```go
var refreshed UnitRefresh
if opts.Refresher != nil {
    r, rerr := opts.Refresher.RefreshUnit(opts.Product, dest)
    if rerr != nil {
        log("service definition refresh failed: " + rerr.Error() + " (continuing)")
    } else {
        refreshed = r
        if r.Output != "" {
            log(r.Output)
        }
    }
}
```

The deferred rollback (`swap.go:93-108`) restores the definition before it
restarts the service:

```go
if refreshed.Changed && opts.Refresher != nil {
    if rerr := opts.Refresher.RestoreUnit(opts.Product, refreshed); rerr != nil {
        log("restore previous service definition failed: " + rerr.Error())
    } else {
        log("restored previous service definition from " + refreshed.BackupPath)
    }
}
```

`run.go:126-141` — replace `HealStart: true` with a probe:

```go
active, installed := false, false
if opts.Service != nil {
    active, _ = opts.Service.IsActive(opts.Product)
    installed, _ = opts.Service.IsInstalled(opts.Product)
}
…
HealStart: installed,
Refresher: opts.Refresher,
```

`RunOpts` gains `Refresher UnitRefresher`, passed through from both CLIs.

### 6. `service.IsInstalled` and the exec adapter

`control.go`:

```go
// IsInstalled reports whether a service definition exists for product,
// regardless of whether it is enabled or running. Linux uses LoadState (not
// is-enabled, which exits non-zero for an installed-but-disabled unit);
// darwin checks for the LaunchAgent file.
func IsInstalled(product string) (bool, error)
```

* linux: `systemctl --user show -p LoadState --value <product>.service`;
  installed when the trimmed output is neither empty nor `not-found`. If
  `systemctl` is absent or errors, fall back to `os.Stat` on the unit path, so a
  systemd-less host answers `false` rather than erroring.
* darwin: `os.Stat($HOME/Library/LaunchAgents/com.magiccliremote.<product>.plist)`.

`ExecRefresher` (same package) implements `update.UnitRefresher` structurally:

```go
// ExecRefresher runs `<binary> setup-service --refresh --json` so the refresh
// uses the NEW binary's embedded template. The process performing an update is
// the OLD binary and cannot render the new definition itself (MADR 0100).
type ExecRefresher struct{ Timeout time.Duration } // default 60s
```

`RefreshUnit` runs the child with `withUserRuntimeEnv(os.Environ())`
(`setup.go:1106`), decodes one `RefreshResult`, and maps it to `UnitRefresh`.
A non-zero exit, a decode failure, or a timeout is returned as an error — which
`swap.go` logs and steps over. That is also the downgrade path: an older child
binary has no `--refresh` flag, exits non-zero, and the update proceeds exactly
as it does today. `RestoreUnit` calls `RestoreUnitBackup` in-process.

Wiring, both products (`internal/cli/update.go:45-50`,
`internal/relay/update.go:34-39`): add `IsInstalledFn: service.IsInstalled` to
`FuncService` and `Refresher: service.ExecRefresher{}` to `RunOpts`.

### 7. Installer (Part D)

In `svc_systemd`'s upgrade branch (`scripts/install.sh:417`), before the restart
loop:

```sh
        # Refresh managed definitions before restarting, so a unit fix in this
        # release actually lands (MADR 0100). Never fatal: a refusal here just
        # means the operator's unit is kept, which is the pre-0100 behaviour.
        for _p in mcremote mcrelay; do
            [ -x "$INSTALL_DIR/$_p" ] || continue
            [ -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$_p.service" ] || continue
            "$INSTALL_DIR/$_p" setup-service --refresh 2>&1 | sed 's/^/  /' || true
        done
```

`install_binaries` (`:751`) runs before `setup_service` (`:755`), so the binary
invoked is the newly installed one. The blanket advisory at `:434` is dropped in
favour of what the refresh printed per product.

## Execution Phases

Phase 0 gates everything. Phases 1-2 land together (the command is useless
untested and the update path needs it). Phases 3-6 each keep the tree shippable
on their own.

---

### Phase 0 — Confirm the readings on a host — ✅ done 2026-08-18

**Deliverable:** an observation for each code reading the design rests on.
Executed on **wonder** (Ubuntu 26.04, systemd 259); full evidence in
[0100-findings-update-refresh.md](0100-findings-update-refresh.md).

| Item | Result |
|---|---|
| **F2** — restart without `daemon-reload` runs the cached definition | ✅ scratch unit: on-disk `v2`, loaded `v1`, restart still reported success |
| **F3** — `update` rolls back a good swap where no unit exists | ✅ isolated `mcrelay` copy: `Unit mcrelay.service not found.` → `restored previous binary from .prev` → exit 1, binary byte-identical |
| **P0.3** — `<unit>.service.prev` is inert in the unit dir | ✅ `daemon-reload` exit 0, zero unit-file matches, zero journal complaints |
| **F4** *(new)* — a render is not a pure function of the unit | ⚠️ found here; §1a added to this plan and a third constraint added to the MADR before any code was written |

No live service was mutated; the host was verified unchanged afterwards.

**Exit criterion:** met.

---

### Phase 1 — `RefreshUnit` and its tests — ✅ done 2026-08-18

**Deliverable:** the refresh logic, provable without a systemd host.

1. `internal/cli/service/refresh.go`: `recoverOptions`, `unitIsManaged`,
   `unquoteSystemd`, `splitExecStart`, `RefreshUnit`, `RestoreUnitBackup`, and
   the plist equivalents.
2. `internal/cli/service/setup.go` / `plist_render.go`: extract `renderWith` and
   `renderPlistWith` per §1a; `render` / `renderPlist` become thin wrappers
   passing `nil`, so `Setup` behaviour is byte-identical.
3. `internal/cli/service/refresh_test.go`:

   | Test | Asserts |
   |---|---|
   | `TestRefreshRoundTripsRenderedUnit` | `render(opts)` → `recoverOptions` → equal `Options`, over a table incl. spaces in paths, `%` in values, `--env`, custom `--unit-name`, empty optionals |
   | `TestRefreshUnchangedOnFreshSetup` | `Setup` then `RefreshUnit` → `unchanged`, file bytes untouched, exactly one `daemon-reload` recorded |
   | `TestRefreshRewritesStaleUnit` | `Setup`, then inject `PrivateDevices=true` after `[Service]` → `refreshed`, file equals the canonical render, `.prev` holds the injected version |
   | `TestRefreshPreservesBakedOptions` | `Setup` with `ListenPort: 9099`, `ExtraEnviron: ["K=V"]`, then stale-ify → refreshed unit still carries `--listen-port 9099` and `Environment=K=V`, mode still `0600` |
   | `TestRefreshKeepsHandEditedUnit` | `ExecStartPre=/bin/true` injected → `kept`, bytes identical, reason names the directive |
   | `TestRefreshKeepsForeignUnit` | file without the marker → `kept` |
   | `TestRefreshKeepsUnparseableExecStart` | `--unknown-flag` in `ExecStart` → `kept` |
   | `TestRefreshNoUnitInstalled` | empty `UnitDir` → `none`, zero systemctl calls |
   | `TestRefreshDarwinRewritesPlist` | `OverrideInstallOS("darwin")` → plist rewritten, zero systemctl calls |
   | `TestRefreshWarnsOnForeignExecStartBinary` | `Binary` elsewhere → warning present, verdict unaffected |
   | `TestRefreshPinsRenderedEnvironment` | render with `PATH=A`, refresh with `PATH=B` → `Environment=PATH=` unchanged; caller without `XDG_RUNTIME_DIR` does not drop the line; a pinned PATH missing a computed entry produces the warning |
   | `TestRestoreUnitBackup` | rename back + one reload |

4. `make pre-add-check FILES="internal/cli/service/refresh.go internal/cli/service/refresh_test.go internal/cli/service/setup.go internal/cli/service/plist_render.go"`.

**Exit criterion:** `go test ./internal/cli/service/...` green, including the
round-trip table.

---

### Phase 2 — Command surface — ✅ done 2026-08-18

**Deliverable:** `<product> setup-service --refresh` on both binaries.

1. `--refresh` / `--json` in `internal/cli/setup_service.go` and
   `internal/relay/cli.go` (separate flag structs — change both).
2. `runSetupService` branch, four human lines, JSON object, `--remove`
   conflict, `--print-only` dry run.
3. Extend `internal/cli/setup_service_test.go`:
   `TestRunSetupServiceRefreshPrintsVerdict`,
   `TestRunSetupServiceRefreshJSONShape`,
   `TestSetupServiceRefreshConflictsWithRemove`.
4. Help text in both `Long` blocks (`setup_service.go:129-145`,
   `relay/cli.go:411-417`).

**Exit criterion:** `mcremote setup-service --refresh --json` on a host with no
unit prints `{"verdict":"none",…}` and exits 0.

---

### Phase 3 — Update sequence — ✅ done 2026-08-18

**Deliverable:** `update` refreshes before it starts, and rolls the definition
back when the start fails.

1. `internal/update/service.go`: `UnitRefresh`, `UnitRefresher`,
   `ServiceControl.IsInstalled`, `FuncService.IsInstalledFn` (nil ⇒ `false, nil`).
2. `internal/update/swap.go`: `SwapOpts.Refresher`, the refresh call before
   `:118`, the restore inside the deferred block.
3. `internal/update/run.go`: `RunOpts.Refresher`; `HealStart: installed`.
4. Tests in `internal/update/swap_test.go`:

   | Test | Asserts |
   |---|---|
   | `TestSwapRefreshesBeforeStart` | fake refresher and service append to one ordered log: `stop, refresh, start` |
   | `TestSwapContinuesWhenRefreshFails` | refresher errors → swap still succeeds, `Start` still called, message logged |
   | `TestSwapRestoresUnitOnStartFailure` | refresher reports `Changed` → `Start` fails → `RestoreUnit` called once, binary is `old` again |
   | `TestSwapSkipsRefreshWhenNil` | existing behaviour byte-for-byte |

5. In `internal/update/run_test.go`: `TestRunSkipsHealStartWhenNotInstalled` —
   `IsInstalledFn` false, `Start` never called, `Run` returns nil.

**Exit criterion:** `go test ./internal/update/...` green; the four pre-existing
`TestSwapAndRestart_*` tests pass unmodified.

---

### Phase 4 — `IsInstalled`, `ExecRefresher`, wiring — ✅ done 2026-08-18

**Deliverable:** the real binaries do what Phase 3 tested with fakes.

1. `service.IsInstalled` per §6, with the `LoadState` probe and the `os.Stat`
   fallback.
2. `service.ExecRefresher` per §6: 60s timeout, `withUserRuntimeEnv`.
3. Wire both products (`internal/cli/update.go`, `internal/relay/update.go`).
4. `internal/cli/service/control_test.go`:
   `TestIsInstalledLinuxLoadState` (loaded / not-found / systemctl error),
   `TestIsInstalledDarwinPlistPresent`.
5. `TestExecRefresherParsesJSON` using a stub script written to `t.TempDir()`
   that prints a fixed `RefreshResult`; and `TestExecRefresherNonZeroIsError`
   (which is also the old-binary downgrade case).

**Exit criterion:** `go build ./... && go test ./internal/...` green;
`go vet ./...` clean.

---

### Phase 5 — Installer — ✅ done 2026-08-18

**Deliverable:** `curl … | sh` repairs a managed unit on an existing host.

1. The refresh loop in `svc_systemd` per §7; rework the `:434` advisory.
2. `shellcheck -s sh scripts/install.sh` clean.
3. `scripts/install_test.sh`: new case — a stub `mcremote` that appends its
   arguments to `$MC_ARGS_LOG`, plus an existing unit file; assert the log
   contains `setup-service --refresh` and that it precedes the restart; assert
   the existing `unit was NOT rewritten` case at `:249` still passes.

**Exit criterion:** `sh scripts/install_test.sh` fully green.

---

### Phase 6 — Documentation — ✅ done 2026-08-18

**Two deviations from this plan, recorded rather than silently absorbed:**

* `ops-mcrelay.md:194` was **not** changed as written. That `daemon-reload` is
  part of a *drop-in* recipe, where it is exactly right — `--refresh` does not
  touch drop-ins. The `--refresh` documentation went into §2's "an existing unit
  does not pick up 0091 hardening" paragraph instead, which is the advice
  `--refresh` actually supersedes.
* There is no `CHANGELOG` in this repository, so the release-note text lives in
  `ops-linux-install.md` §Updating ("Two limitations worth knowing") instead of a
  separate file.

**Deliverable:** the new behaviour and its one real limitation are written down.

1. `docs/ops-linux-install.md` — an "Updating" section: what `update` now does
   in order, what `--refresh` reports, and that a host on a pre-0100 binary
   needs one more update (or `curl | sh`) before the refresh applies.
2. `docs/ops-mcrelay.md` §2 — replace the "re-run `setup-service --force` to
   pick up 0091 hardening" advice with `--refresh`, which does the same thing
   without resetting baked options. (The `daemon-reload` at `:194` belongs to a
   drop-in recipe and stays.)
3. `README.md:85` — one line that `update` also reconciles the service
   definition.
4. `docs/0065-MADR-update-automation.md` — an "Extended by 0100" pointer in
   §More Information. No historical rationale rewritten.
5. Release notes for the target tag: the pre-0100 limitation, verbatim from
   MADR §Consequences.

**Exit criterion:** no doc still presents a hand-run `daemon-reload` after an
update as the only option.

---

### Phase 7 — Host verification — ✅ done 2026-08-18

**Deliverable:** C1-C9 observed on a real host, not inferred. Executed on
wonder against linux/amd64 binaries built from the implementation branch; full
evidence in [0100-findings-update-refresh.md](0100-findings-update-refresh.md)
§"Phase 7". Host restored to its exact pre-Phase-7 state afterward.

1. Ephemeral Linux host (0098 pattern). Install the current release via
   `curl | sh`.
2. Hand-install an `mcrelay.service` carrying `PrivateDevices=true` and
   `RestrictNamespaces=true` (the 0099 F4a shape) and confirm the crash loop.
3. Install the Phase 1-4 binary; run `mcrelay update --force --yes`; assert the
   unit no longer carries either directive, `.prev` does, `journalctl --user`
   shows `daemon-reload` before the start, and the unit stays active for 10s.
4. Repeat with `--listen-port 9099 --env K=V` baked in — both survive.
5. Repeat with `ExecStartPre=/bin/true` added — unit untouched, verdict `kept`
   printed, update still succeeds.
6. On a host with no unit: `mcremote update --force --yes` exits 0 and does not
   roll back (F3).
7. Run the update over ssh with a deliberately minimal `PATH` and with
   `XDG_RUNTIME_DIR` unset: the unit's `Environment=PATH=` and
   `Environment=XDG_RUNTIME_DIR=` must be unchanged afterwards (F4 regression on
   a real host, not just in a test).
8. Record everything in `docs/0100-findings-update-refresh.md`, appending to the
   Phase 0 results.

**Exit criterion:** every row of MADR §Confirmation marked observed, with the
command and output that proves it.

## Verification

### Commands

```bash
make pre-add-check                      # gofmt + golint + govulncheck, per staged file
go build ./...
go test ./internal/cli/service/... ./internal/update/... ./internal/cli/... ./internal/relay/...
go test -race ./internal/cli/service/... ./internal/update/...
go vet ./...
shellcheck -s sh scripts/install.sh
sh scripts/install_test.sh
```

### Acceptance

| # | Criterion | Proof |
|---|---|---|
| A1 | A stale managed unit is rewritten to the current template | `TestRefreshRewritesStaleUnit`; Phase 7.3 |
| A2 | Baked options and the `0600` mode survive | `TestRefreshPreservesBakedOptions`; Phase 7.4 |
| A2b | The environment block is pinned, not recomputed | `TestRefreshPinsRenderedEnvironment`; Phase 7.8 |
| A3 | A hand-edited or foreign unit is never rewritten | `TestRefreshKeepsHandEditedUnit`, `…KeepsForeignUnit`, `…KeepsUnparseableExecStart`; Phase 7.5; `install_test.sh:249` |
| A4 | `daemon-reload` runs before the restart on every Linux update with a unit | `TestRefreshUnchangedOnFreshSetup`, `TestSwapRefreshesBeforeStart`; Phase 7.3 journal |
| A5 | A refresh failure never fails an update | `TestSwapContinuesWhenRefreshFails` |
| A6 | A failed start restores binary **and** definition | `TestSwapRestoresUnitOnStartFailure` |
| A7 | `update` no longer fails where no service is installed | `TestRunSkipsHealStartWhenNotInstalled`; Phase 7.6 |
| A8 | The installer refreshes on upgrade | new `install_test.sh` case; Phase 7 re-run of `curl \| sh` |
| A9 | No existing behaviour regressed | the four `TestSwapAndRestart_*` tests unmodified and green; `install_test.sh` fully green |

### The regression guard

`TestRefreshRewritesStaleUnit` fails against today's code (the function does not
exist) and against any future change that drops the rewrite. Pair it with
`TestRefreshRoundTripsRenderedUnit`, which fails the moment a template gains a
rendered field the recovery parser cannot read — the specific way this design
can rot silently.

## Rollout and Rollback

**Rollout.** Land Phases 1-4 in one release; they are inert without each other.
Phase 5 should land in the same tag — it is the only part that helps hosts whose
installed binary predates this work, so shipping it later delays the actual
repair. Tag, publish, then re-run the 0098 sweep's systemd rows.

**Rollback.** Each phase is a separate commit:

* Phase 5 alone: revert the `install.sh` hunk; the upgrade branch returns to
  keeping units and advising `--force`.
* Phases 3-4: revert the `Refresher` wiring in `internal/cli/update.go` and
  `internal/relay/update.go`. `SwapOpts.Refresher` is nil-tolerant, so `update`
  falls back to today's behaviour with no other change.
* Phases 1-2: `setup-service --refresh` is additive; leaving it in place while
  reverting Phases 3-5 gives operators a manual repair with no automatic
  rewrite.

**Per-host rollback.** Every rewrite leaves `<unit>.prev` next to the unit:

```bash
mv ~/.config/systemd/user/mcrelay.service.prev ~/.config/systemd/user/mcrelay.service
systemctl --user daemon-reload && systemctl --user restart mcrelay
```

**Blast-radius note.** The only destructive operation introduced is the rewrite
of a unit that passes the provenance rule, and it is always preceded by a
backup. Drop-ins under `<unit>.d/` are never read, written, or removed.

## Task Checklist

**Phase 0 — confirm on a host — done**

- [x] F2 reproduced: stale definition served, warning captured
- [x] F3 reproduced: update fails and rolls back with no unit installed
- [x] `<unit>.prev` confirmed inert in the unit directory
- [x] F4 discovered; MADR §"third constraint" and plan §1a added
- [x] `docs/0100-findings-update-refresh.md` written; wonder left unchanged

**Phase 1 — `RefreshUnit`**

- [x] `renderWith` / `renderPlistWith` extracted; `Setup` output byte-identical
- [x] `recoverOptions` + `splitExecStart` + `unquoteSystemd`
- [x] `renderEnv` pinned from the installed definition, with the missing-key and
      new-PATH-entry warnings
- [x] `unitIsManaged` (marker, directive denylist, section allowlist, single `ExecStart`)
- [x] `RefreshUnit` incl. `.prev` backup, mode rule, `plutil -lint` on darwin
- [x] `RestoreUnitBackup`
- [x] plist recovery + structural provenance check
- [x] twelve tests from the Phase 1 table
- [x] `make pre-add-check` on both files

**Phase 2 — command surface**

- [x] `--refresh` / `--json` on `mcremote` and `mcrelay`
- [x] four human verdict lines + JSON object
- [x] `--remove` conflict, `--print-only` dry run
- [x] three command tests
- [x] help text in both `Long` blocks

**Phase 3 — update sequence**

- [x] `UnitRefresh`, `UnitRefresher`, `IsInstalled` on the interface
- [x] `FuncService` nil-safe additions
- [x] refresh call placed after the swap, before `Start`
- [x] definition restored in the deferred rollback
- [x] `HealStart: installed` in `run.go`
- [x] five tests; the four existing `TestSwapAndRestart_*` unmodified

**Phase 4 — real implementations**

- [x] `service.IsInstalled` via `LoadState`, with `os.Stat` fallback
- [x] `service.ExecRefresher` with timeout and `withUserRuntimeEnv`
- [x] wired in `internal/cli/update.go` and `internal/relay/update.go`
- [x] four tests
- [x] `go vet ./...` clean

**Phase 5 — installer**

- [x] refresh loop in `svc_systemd` upgrade branch, non-fatal
- [x] `:434` advisory reworked
- [x] `shellcheck -s sh` clean
- [x] new `install_test.sh` case; `:249` case still green

**Phase 6 — docs**

- [x] `ops-linux-install.md` "Updating" section incl. the pre-0100 limitation
- [x] `ops-mcrelay.md:194` recipe replaced
- [x] `README.md:85` line
- [x] `0065` "Extended by 0100" pointer
- [x] release notes drafted

**Phase 7 — host verification**

- [x] 0099 F4a unit reproduced in a crash loop, cleared by `--refresh`
- [x] baked `--listen-port` / `--env` survive (verified via `--refresh`
      directly; `mcrelay update --force` was exercised separately for F3 —
      see below)
- [x] hand-edited unit kept, refresh still succeeds
- [x] no-unit host: `update --force --yes` exits 0, no rollback (F3, on the
      exact reproduction from Phase 0, now fixed)
- [x] update under a minimal `PATH` / no `XDG_RUNTIME_DIR` leaves the unit's
      environment block untouched (C10)
- [x] findings file completed; MADR status → `accepted`
