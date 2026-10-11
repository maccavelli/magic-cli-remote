# `go-selfupdate-lib` full-adoption assessment for `magic-cli-remote`

Observed 2026-10-11. This report checks proposed MADR 0054 against the current library and this
tree, and states exactly what full adoption of `go-selfupdate-lib` requires. Full adoption covers
the update command, build stamps, release specification, build and publish workflows, service
lifecycles, macOS re-signing, and installers. The report decides nothing and changes no source.

Associated records:

- [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
  (proposed)
- [0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md)
  (phase R1 produced this report)
- [0159-MADR-the-windows-service-path-is-half-wired.md](../decisions/0159-MADR-the-windows-service-path-is-half-wired.md)
- [0065-MADR-update-automation.md](../decisions/0065-MADR-update-automation.md),
  [0103-MADR-update-tracks-release-build-and-active-service.md](../decisions/0103-MADR-update-tracks-release-build-and-active-service.md)
- Installer records: [0097-MADR-linux-curl-installer.md](../decisions/0097-MADR-linux-curl-installer.md),
  [0099-MADR-installer-service-state-verification.md](../decisions/0099-MADR-installer-service-state-verification.md),
  [0104-MADR-installer-linux-and-macos.md](../decisions/0104-MADR-installer-linux-and-macos.md),
  [0116-MADR-windows-and-linux-arm64-build-targets.md](../decisions/0116-MADR-windows-and-linux-arm64-build-targets.md),
  [0156-MADR-only-the-posix-installer-learned-canonical-names.md](../decisions/0156-MADR-only-the-posix-installer-learned-canonical-names.md),
  [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](../decisions/0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md)

## Executive assessment

Full adoption is feasible. MADR 0054's Option A is still the right shape, with one change: the
library can now take over more than the MADR assumed.

- **The library has caught up with Windows.** `go-selfupdate-lib v1.14.0` was published after the
  MADR was drafted. It adds `selfupdate/service/taskscheduler`, a per-user Task Scheduler
  lifecycle built from this repository's own 0159 measurements. This repository's task already
  has the shape that backend requires. The Windows lifecycle adapter, which the MADR keeps as
  product code, can therefore move to the library too. **Recommendation:** amend MADR 0054 to adopt
  `taskscheduler` and drop the "Windows adapter is still this repository's to finish" consequence.
- **The 0159 refresh dependency is resolved.** The MADR requires the Windows `setup-service
  --refresh` from 0159 D1 before Windows managed updates can work. That has shipped
  (`internal/cli/service/refresh_schtasks.go`). Only 0159 P12, environment delivery, remains, and it
  is independent of this migration.
- **Most of the update machinery can be deleted.** The library replaces:
  - the hand-rolled update command body;
  - the legacy identity normalizer;
  - the Linux, macOS and Windows lifecycle adapters;
  - the macOS re-signing transformer;
  - the release build, stamp, smoke, cgo-check, checksum and staging recipe;
  - the publish pin and the spent `v0.16.0` bridge.
- **Some work cannot leave the product.** The library never creates, renders or deletes a service
  definition. `setup-service`, which renders the systemd unit, the launchd plist and the task XML,
  stays here, and so does its `--refresh --json` child. So do the content of the release spec, the
  choice of signing identity, the Android APK build, and the developer `make install` path.
- **The installers are the hard part.** The hand-written `scripts/install.sh` does things the
  library's installer templates do not, and the MADR does not inventory them:
  - it supervises `mcremote` under runit, s6 and OpenRC;
  - it falls back when the systemd user manager is broken;
  - it verifies the service's state after setup (0099);
  - its uninstall removes service definitions;
  - it accepts BusyBox `wget`.

  The Windows installer installs into a different folder from the template's. The environment
  prefix the MADR proposes does not match the variables operators use today. Each of these must
  be moved into `setup-service`, accepted as a loss, or migrated before the scripts are deleted.

**Recommended direction:** adopt in dependency order, as the MADR proposes, with two changes. Make
Windows a library-backend phase rather than an adapter phase. Make the installers the last phase,
gated on closing the gaps listed under
[Installer replacement: gaps that block deleting the scripts](#installer-replacement-gaps-that-block-deleting-the-scripts).
Ship the command, stamps, spec, lifecycles and release workflows before that. They do not depend
on the installers.

## Scope and evidence

### Baselines

| Repository | Baseline | Go | Self-update dependency state |
| :--- | :--- | :--- | :--- |
| `magic-cli-remote` | `9778cbc17b39`, plus owner work not yet committed (toolchain records, `go.mod` floor) | `go 1.27.2` in the working tree | `github.com/maccavelli/mcplib v1.4.1`; publish workflow pinned to mcplib commit `8d4d89ff34ff…` |
| `go-selfupdate-lib` | tag `v1.14.0`, peeled commit `81dae60060274260e6f500f383aa1b79cee460d1`; `main` is `cf0664cd224c`, two documentation commits ahead | `go 1.27.1`, toolchain `go1.27.2` | requires only `golang.org/x/mod v0.40.0`, `x/sys v0.47.0`, `x/term v0.43.0` |

This module already requires `x/mod v0.41.0`, `x/sys v0.48.0` and `x/term v0.46.0`, so the
library's requirements are met. Swapping `mcplib` for `go-selfupdate-lib` removes a module and adds
none. `git ls-remote` on the library's origin lists `v1.12.1`, `v1.13.0` and `v1.14.0`. The peeled
commit above was resolved from the local tag. The implementing PLAN must confirm it again with
`git ls-remote … 'refs/tags/v1.14.0^{}'` before pinning the workflows, as the MADR did for
`v1.13.0`.

### Method

Read-only. No build, test, probe or installer run was performed for this report. Claims cite the
file and line read. Library behavior is cited from the library's source, its guides
(`docs/guides/migrating-from-mcplib-selfupdate.md` §14, `docs/guides/extending-selfupdate.md`
"Task Scheduler", `docs/guides/building-releases.md` §12), and its
`0021-MADR-task-scheduler-lifecycle-and-windowless-children.md`.

## What has changed since MADR 0054 was drafted

| # | The MADR says | Observed now | Recommended amendment |
| :--- | :--- | :--- | :--- |
| C1 | Adopt `v1.13.0`; pin both workflows to `5e199c83…` | `v1.14.0` is published. According to the library's §14, the only exported change is a new package, and `make apicheck` reports it compatible with `v1.13.0` | Target `v1.14.0` (or the latest tag at implementation time), and move `go.mod` and both workflow pins together |
| C2 | "Windows keeps … the repo's adapter … grows an `Enabled` probe … The library's `scm` backend is for Windows *services* and is not adopted" | `selfupdate/service/taskscheduler` implements `Lifecycle`, `EnabledLifecycle`, `Reconciler` and `service.Detacher` for a per-user task. It disables the task before stopping it, so the watchdog trigger cannot relaunch the old binary mid-swap. It works through the COM API, never `schtasks.exe`, and its `Restore` is library code, which closes 0159 F18's class | Adopt `taskscheduler.New(Options{Name: "mcremote", RewritePath: true})` on Windows, and delete the product adapter (see the disposition map) |
| C3 | Windows "must land with or after 0159 D1" | 0159 D1 shipped (`refresh_schtasks.go`, commits `be5a575e` and `43bd535b`). The 0159 PLAN is `in-progress` with only P12 left | Record the condition as met |
| C4 | Spec `"installer": {"name": "magic-cli-remote", "env_prefix": "MAGIC_CLI_REMOTE"}` | Today's installers read `MCREMOTE_VERSION`, `MCREMOTE_INSTALL_DIR`, `MCREMOTE_NO_SERVICE` (`install.sh`) and `MCREMOTE_INSTALL_NO_PATH_UPDATE` (`install.ps1:37,279`). The library's guide says to keep the script's names | Use `"env_prefix": "MCREMOTE"`. Two names still change: `MCREMOTE_NO_PATH_UPDATE` and `MCREMOTE_NO_HOOKS` |
| C5 | Not discussed | The template installs on Windows into `%LOCALAPPDATA%\Programs\<name>`; `install.ps1:270` installs into `%LOCALAPPDATA%\Programs` itself (MADR 0116 D13) | A decision: move the folder, which needs migration of the PATH entry and the task's `Exec` path, or set a name that preserves the existing layout if the template allows it (unverified) |
| C6 | Amendment 4: "`setup-service` on a host with no supported service manager must not fail the install" | Concretely: `setup-service` handles only systemd-user on Linux, launchd on macOS and Task Scheduler on Windows (`internal/cli/service/setup.go:301-323`). `install.sh`'s `detect_init` also supports runit, s6, OpenRC (user and system) and a broken-systemd fallback, writing supervisor run scripts itself (`svc_runit`, `svc_s6`, `svc_openrc_user`) | Decide whether those supervisors move into Go `setup-service` or are dropped. Either way, it is not just a matter of exit codes |

The MADR's symbol-by-symbol API table, its contract-change table and its six stated amendments
otherwise still match the library source. The table's references move from `v1.13.0` to
`v1.14.0` unchanged, because `v1.14.0` is additive.

## Current self-update surface, and its disposition under full adoption

"Delete" means the code has a library equivalent and no other caller. "Shrink" means a thin product
binding remains. "Keep" means the library deliberately does not own it.

### Go code

| Path | Size (lines) | What it does today | Disposition |
| :--- | ---: | :--- | :--- |
| `cmd/mcremote/main.go`, `cmd/mcrelay/main.go` | small | `main.version/commit/date/buildKind` link-time stamps; exit through `selfupdate.ExitCode` | **Shrink**: stamps move to `buildinfo` (`buildinfo.LDFlags`), and exit goes through `cli.Exit` |
| `internal/cli/root.go:12-48`, `internal/relay/cli.go:49` | small | `SetVersionInfo`, `SetBuildKind`, `VersionString()` → `mcremote X.Y.Z (commit) date` | **Shrink**: `version` prints `buildinfo.Identity()` first (`vX.Y.Z (release) <commit>`), which the build workflow's identity run and the installers both require |
| `internal/cli/update.go`, `internal/relay/update.go` | 15 each | build the `update` command from `updateclient.NewCommand` | **Keep, thin**: bind `cli.Flags` on the cobra flag set and call `Flags.Request`, `cli.Run`, `cli.Exit` |
| `internal/updateclient/client.go` | 233 | updater factory, plus a hand-rolled command (flags at `:217-220`, prompt, reporter, exit mapping) | **Shrink** to the updater factory: the embedded spec, source, policy, per-GOOS lifecycle and signer |
| `internal/updateclient/legacy.go` | 89 | `BASE.N` → `vX.Y.Z` normalization of the binary's own stamp | **Delete**: a `buildinfo` release stamp is a strict tag by construction |
| `internal/updateclient/legacy_test.go` | 226 | frozen v0.15.3 selector fixture proving the published `v0.16.0` bridge still resolves | **Keep**, import path change only. It is a contract fixture, not production code |
| `internal/updateclient/lifecycle.go` (+ test) | 185 | adapts `service.IsInstalled/IsActive/Stop/Start` and `service.ExecRefresher` to `Lifecycle` and `Reconciler`; polls `IsActive` for health | **Delete**: `systemd.New`, `launchd.New`, `taskscheduler.New`, plus `service.NewExecReconciler` with `RestoreFunc` |
| `internal/updateclient/codesign_darwin.go`, `codesign_other.go` (+ test) | 89 | re-signs the staged binary by running `codesign` found on `PATH` (`codesign_darwin.go:46`) with no fixed identifier | **Delete**: `codesign.NewSigner` fixes both defects the library's migration guide names |
| `internal/cli/service/exec_refresher.go` | 97 | runs the new binary's `setup-service --refresh --json` and parses the receipt | **Delete**: `service.NewExecReconciler` runs the same child. Its `Receipt` reads `refresh.go`'s fields and treats a missing `schema_version` as 1 (`execreconciler.go:182-183`) |
| `internal/cli/service/control.go` | 248 | `IsActive`, `Stop`, `Start`, `IsInstalled` per OS; `ProbeStatus` for `doctor` (`internal/cli/doctor.go:28`) | **Shrink**: `Stop` and `Start` have no caller left once `lifecycle.go` goes. `ProbeStatus` stays, or is rebased on the backends' `Installed`/`Enabled` and running probes so `doctor` and `update` agree |
| `internal/cli/service/control_schtasks.go` | 108 | the Windows `schtasks` lifecycle | **Delete** once `ProbeStatus` no longer needs it |
| `internal/cli/service/launchd_wait.go` | 111 | waits for launchd teardown | **Keep**: `setup.go:489,505,688` uses it outside updates |
| `internal/cli/service/refresh.go`, `refresh_schtasks.go` | 825, 255 | the product's definition refresh and its JSON receipt, the cross-version contract | **Keep**: the program owns its definition (library 0021 D-d, D9) |
| `internal/cli/service/setup.go`, `setup_schtasks.go`, `schtasks.go`, `plist_render.go`, unit templates | 1,319 and more | render, install, enable and start the service definitions | **Keep**, and possibly **grow**, if the installer's supervisor fallbacks move here (C6) |

The update path loses roughly 750 lines of production Go. `setup-service` keeps its size, or
grows.

### Build, release and installers

| Path | What it does today | Disposition |
| :--- | :--- | :--- |
| `Makefile` release stamps (`:165-180`) and `debug` stamps | `-X main.version=… -X main.buildKind=…` | **Shrink**: render `buildinfo.LDFlags`. Developer builds stay in the Makefile, as the library's guide §11 expects |
| `scripts/verify-build-metadata.sh` (83) | the build-tag and cgo policy gate | **Keep** for local builds. The release-lane half is superseded by the build workflow's per-binary build-information checks |
| `ci.yml` `go` job, "Build mcremote & mcrelay" (`build_matrix`) | cross-builds release binaries with stamps | **Delete** from the release lane: replaced by `build-selfupdate-release.yml`, which rehearses on every push and PR |
| `ci.yml` `release` job (`:874-1027`) | APK rename and assert; `sha256sum -c`; cgo-free assert per binary; linux-amd64 `version` smoke via `awk '{print $2}'`; staging of binaries, `SHA256SUMS`, `install.sh`, `install.ps1` and APK; `v0.16.0` bridge copies | **Delete**, except the APK build and `assert-flutter-release-apk.sh`, which feed the build workflow as an `extras-artifact-name` artifact |
| `ci.yml` `publish` job (`:1034-1048`) | mcplib `publish-selfupdate-release.yml@8d4d89ff…` with `bridge-release` | **Replace** with the library's publish workflow. `bridge-release` goes: the successor workflow rejects it, and the bridge is spent |
| `scripts/install.sh` (1,118), `install.ps1` (371) | the shipped installers | **Replace** with template-rendered extras, after the gaps below close |
| `scripts/install_test.sh` (980), `install_ps1_test.ps1` (445), `install_ps1_unit_test.ps1` (337); CI "install.sh tests" step | the hermetic installer suites (0170) | **Delete** with the scripts. Coverage moves to the library's installer tests, plus a repository acceptance run of the rendered installers |
| `scripts/install-binary.sh` (+ test), `make install` | swaps a locally built binary under a running service | **Keep**: a developer path the library has no equivalent for |
| `docs/guides/config.md:637-717`, README install section, operator guides | document today's command and installer | **Rewrite** for streams, flags, warnings, journal files, installer options and exit codes |

The release lane and the installers together remove about 3,250 lines of shell and PowerShell and
about 200 lines of workflow.

## Installer replacement: gaps that block deleting the scripts

The library's templates (`building-releases.md` §12) download by tag over HTTPS and verify strictly
against `SHA256SUMS`. They install into `~/.local/bin` or `%LOCALAPPDATA%\Programs\<name>`, run an
identity check with rollback, and offer `--version`, `--dir`, `--product`, `--no-hooks`,
`--dry-run`, `--uninstall`, `--allow-root` and `--verify-attestation`. They allow at most eight
fixed-argument hooks and use exit codes 0, 1, 2 and 3. Compared with what this repository decided
and ships:

| # | Hand-written behavior (evidence) | Template behavior | What closes the gap |
| :--- | :--- | :--- | :--- |
| G1 | runit, s6 and OpenRC supervision; a broken-systemd fallback (`install.sh` `detect_init`, `svc_runit`, `svc_s6`, `svc_openrc_user`) | none: product steps belong in a hook into a product subcommand | Move the supervisors into `setup-service` (Go, tested), or decide to drop them and document the loss. Without one of these, the `after_install` hook exits 3 on those hosts |
| G2 | post-setup service-state verification: settle wait, `NRestarts` crash-loop detection, a "running, enabled at boot" report (0099; `install.sh` around `:453-472`, `:919`) | the hook's exit status only | `setup-service` must verify and report the state it produced, so the hook's exit code is honest |
| G3 | per-invocation options `--no-linger`, `--force-service`, `--with-relay-service`, `--verbose` (`install.sh:1034-1043`) | hook arguments are fixed in the spec | Defaults move into `setup-service`. Options become environment variables `setup-service` reads, or follow-up commands (`mcrelay setup-service`, as the MADR proposes for the relay) |
| G4 | `--uninstall` stops the service and removes the unit, plist, runit, s6 or OpenRC definitions it targets (`do_uninstall`, `:958-994`) | removes binaries and `.prev` files only; no uninstall hook | A `mcremote setup-service --uninstall` (or equivalent) that operators run first, documented. Otherwise an uninstall leaves a unit pointing at a deleted binary |
| G5 | BusyBox `wget` is accepted (Alpine without `curl`) | BusyBox `wget` is refused for lacking `--https-only` | Accept it as a stricter policy and document "install curl", or ask the library for a change |
| G6 | Windows installs into `%LOCALAPPDATA%\Programs` (`install.ps1:270`, 0116 D13) and **does not** run `setup-service`. It prints "Next: mcremote setup-service" (`install.ps1:363`) | installs into `%LOCALAPPDATA%\Programs\<name>`; an `after_install` hook runs on both installers | Decide the folder (C5). Decide whether a Windows install should start registering the task automatically; that is a new behavior there |
| G7 | variables `MCREMOTE_*`; `MC_TEST_BASE_URL` for tests | `<PREFIX>_*`; `SELFUPDATE_INSTALL_BASE_URL` | `env_prefix: "MCREMOTE"` (C4); tests move to the library variable |
| G8 | exit 3 = "binaries installed, service setup failed" | exit 3 = "an `after_install` hook failed" | Equivalent once the hook is `setup-service` |

G1, G2 and G4 are product work in `setup-service`. They are the main cost of Option A's installer
phase. MADR 0054's Option B, which keeps the hand-written installers, avoids that cost for one more
cycle at the price the MADR states. The recommended order below lets the owner choose between A and
B at the last phase with everything else already shipped.

## Operator- and phone-visible changes

The MADR's "The amendments, stated" list is accurate. Add these:

- **`/health`'s `version` field.** The daemon passes its stamp through `daemon.Options.Version` to
  `internal/ws/server.go:531`. Under `buildinfo` a release stamp is `v0.X.Y`, where it is `0.X.Y`
  today. No Dart code in `apps/mobile/lib` was found reading that field, but operators polling
  `/health` will see the change. Decide whether `/health` reports `buildinfo` as is or strips the
  `v`.
- **Windows install folder** (C5/G6) and **automatic task registration on Windows** (G6).
- **Renamed variables:** `MCREMOTE_NO_SERVICE` becomes `MCREMOTE_NO_HOOKS`, and
  `MCREMOTE_INSTALL_NO_PATH_UPDATE` becomes `MCREMOTE_NO_PATH_UPDATE` (C4).
- **Uninstall no longer removes services** unless G4 is closed.
- **Unsupported supervisors** (runit, s6, OpenRC) are lost unless G1 is closed.

## Recommended implementation sequence

These are recommendations for a later amendment of the 0054 PLAN. None is approved by this report.
Every phase keeps `make preflight`, `make pre-add-check`, the race suite and the Windows gates
(0145) green.

### Phase 0: amend the MADR

Record C1–C6: target `v1.14.0`, adopt `taskscheduler`, mark the 0159 condition met, the
environment prefix, the Windows folder decision, and the supervisor decision. Choose between A and
B for installers explicitly, or defer that choice to Phase 6 as recommended here.

### Phase 1: rehome

Replace `mcplib` with `go-selfupdate-lib v1.14.0` and change the imports. Run the library's guide
§4 greps. Behavior does not change, because the API is unchanged. Keep the mcplib publish pin for
now, so releases keep working while later phases land.

### Phase 2: stamps and identity

Introduce `buildinfo`. Change `version` (and `--version`) to print `buildinfo.Identity()` first.
Delete `legacy.go`. Update the Makefile stamps and the CI `awk` smoke together. Decide the
`/health` `version` field.

### Phase 3: spec and command

Embed `selfupdate-release.json`. Derive the asset selector from it, with a unit test per product
calling `spec.Product` and `spec.AssetSelector`. Replace the hand-rolled command with a `cli.Flags`
binding. Rewrite `docs/guides/config.md`'s update section for the stream rule and the new flags.

### Phase 4: lifecycles, reconcile and signing

On Linux and macOS, use `systemd.New` and `launchd.New` with `service.NewExecReconciler` and
a `RestoreFunc` that wraps `RestoreUnitBackup` (`refresh.go:133`). On Windows, use `taskscheduler.New` with
`NewExecReconciler`. Use `codesign.NewSigner` on macOS. Delete `lifecycle.go`, `exec_refresher.go`,
the codesign transformer and, after rebasing `ProbeStatus`, `control_schtasks.go`. Optionally wire
the daemon's admin-socket shutdown as `taskscheduler.Options.RequestStop` and the existing health
check as `Options.Probe`. The MADR lists both as later work.

### Phase 5: release workflows

Move to `build-selfupdate-release.yml` and the library's publish workflow, pinned by peeled commit.
Feed the APK via `extras-artifact-name`. Delete the release job's build, stamp, smoke, cgo, sum,
staging and bridge steps. Keep shipping `scripts/install.sh` and `install.ps1` as spec `extras`
until Phase 6. Confirm a branch rehearsal before the first tag.

### Phase 6: installers, gated on G1–G7

Close G1, G2 and G4 in `setup-service`, with tests seen to fail first. Decide G5 and G6. Add the
`"installer"` block with one `after_install` hook running `mcremote setup-service`. Rerun the
0098-style live install verification and `scripts/acceptance-windows*.ps1` against the rendered
installers. Only then delete the scripts and their suites.

Phases 1–5 can ship across releases without operator disruption beyond the documented command and
`version` changes. Phase 6 carries the operator-visible installer changes and should ship in one
release with its documentation.

## Acceptance gates

### Source and dependency

- `grep -rn 'mcplib' --include='*.go' .` finds nothing, and `go.mod` has no `mcplib`.
- `grep -rn 'maccavelli/mcplib/.github/workflows' .github` finds nothing; the publish `with:` has
  no `bridge-release`.
- `go list -m github.com/maccavelli/go-selfupdate-lib` reports the pinned tag, and both workflow
  pins name that tag's peeled commit.
- `internal/updateclient` holds no `Lifecycle`, `Reconciler` or `Transformer` implementation.

### Behavior

- `Enabled=true`, installed and down ⇒ started, and `Enabled=false` ⇒ not started, on each OS
  backend. Each case is seen to fail on a scratch copy with the gate removed.
- The `ExecReconciler` receipt round-trips against `refresh.go`'s emitter, including a receipt with
  no `schema_version`.
- The signer uses a fixed identifier and an absolute `codesign`.
- The `version` first line equals `<tag> (release)` for a release build.
- `update --check` from the last mcplib-built release exits 10 against the first library-built
  release, and the frozen v0.15.3 fixture still resolves the published asset set.

### Installers (Phase 6 only)

- Each of G1–G7 is either closed with a test or recorded as an accepted loss in the amended MADR.
- The rendered installers pass a live install on Linux (systemd user), macOS and Windows. Uninstall
  leaves no definition pointing at a removed binary.

## Risks

| Risk | Consequence | Mitigation |
| :--- | :--- | :--- |
| Installers deleted before G1, G2 and G4 close | `install.sh` hosts on runit, s6 or OpenRC lose their service; installs report exit 3; uninstall leaves dangling units | Phase 6 gate; Option B as the fallback |
| Windows folder moves silently | PATH and the task point at the old binary; `update` rewrites a path operators no longer use | Decide C5 explicitly; migrate or keep the layout |
| `version` and `/health` shape change | scripts parsing `$2`, or monitoring parsing `version`, break | Change them in the same phase; document both |
| First tag is the first publish run | an immutable imperfect release | branch rehearsals (Phase 5), then the first tag as the release-contract acceptance |
| Stricter health (`WaitHealthy` settle window) | a crash-looping daemon now rolls an update back | intended; document it |

## Conclusions

The library now covers every self-update concern this repository maintains privately, Windows
included. The unavoidable product code is the service definition and its refresh child, the spec,
and the signing identity. MADR 0054's facts need six corrections (C1–C6) before acceptance. The
largest is that the Windows adapter no longer has to stay here. The installer replacement is the
only phase whose cost is not mostly deletion: the hand-written installer encodes supervisor support,
state verification and uninstall behavior that must first move into `setup-service`. Sequencing the
installers last captures the rest of the cleanup without waiting on that work.

No source, module, workflow or installer change was made by this report.
