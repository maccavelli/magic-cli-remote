---
status: accepted
date: 2026-10-19
decision-makers: Project Owner
consulted: none
informed: operators running mcremote update / mcrelay update; install-script consumers; anyone consuming the GitHub release assets
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Standardize self-update on `go-selfupdate-lib`: command, releases, services, installers

> Record number note (2026-10-10): `0054` is the number
> `scripts/check_records.py --next` returns — the one gap in the
> repository-wide sequence. Gap-fill numbers are established practice:
> [0180-MADR-check-records-is-a-preflight-and-ci-gate.md](0180-MADR-check-records-is-a-preflight-and-ci-gate.md)
> records 0181 as one.
>
> History of this record (2026-10-10): first drafted as
> "Rehome self-update from the mcplib module to go-selfupdate-lib" with a
> rehome-plus-gap-closing recommendation. Before any review, the owner
> asked for the deep-adoption option to be the proposal, so this draft
> supersedes that one; it was never committed. The rehome remains the
> first phase of what is now proposed.

## Context and Problem Statement

`mcremote update` and `mcrelay update` are implemented once in
`github.com/maccavelli/mcplib/selfupdate`, pinned at `mcplib v1.4.1`
(`go.mod:12`), and bound to this repository by `internal/updateclient`
([0065-MADR-update-automation.md](0065-MADR-update-automation.md),
[0100-MADR-update-unit-refresh-and-daemon-reload.md](0100-MADR-update-unit-refresh-and-daemon-reload.md),
[0103-MADR-update-tracks-release-build-and-active-service.md](0103-MADR-update-tracks-release-build-and-active-service.md),
each superseded as to *where the code lives* by MADR 0005 in mcplib). The
release pipeline publishes through mcplib's reusable workflow, pinned by
commit (`.github/workflows/ci.yml:1042`). This record proposes moving to
`github.com/maccavelli/go-selfupdate-lib` and adopting the library's
whole self-update stack — the canonical command, the build stamps, the
embedded release spec, the build workflow, the reference service
lifecycles, and the generated installers — and records every operator
contract that changes.

`mcplib` is the owner's general fleet library; `selfupdate` has since been
extracted from it into `go-selfupdate-lib`, a standalone module whose only
dependencies are the standard library and `golang.org/x/mod`, `x/sys`,
`x/term`. The package and its release workflow are mcplib `v1.6.0`'s,
**with the same Go API** (go-selfupdate-lib@v1.13.0/docs/decisions/
0002-MADR-rehome-selfupdate-from-mcplib.md), plus the fixes of three
debugging passes — everything after that extraction exists only in the
new module. Its guides name this repository three times: the migration
guide for `magic-cli-remote`'s `codesignTransformer` and for a
`bridge-release` input that exists only for this repository's spent
`v0.16.0` bridge, and the reference `ExecReconciler`'s receipt is
described as "version 1 of magic-cli-remote's refresh result"
(go-selfupdate-lib@v1.13.0/selfupdate/service/execreconciler.go:32-33) —
this library was built with this repository's requirements as a first
class input, not around them. The guides are explicit that the migration
runs under each product's own records; this record is that authorization,
under review.

### What this repository consumes today (evidence)

Everything below was verified in the tree and in the module cache.

* `mcplib` is imported **only** for `mcplib/selfupdate`. The seam is
  `internal/updateclient`:
  * `client.go` composes the updater with every component explicit, then
    builds the shared cobra command (`update`; flags `--check`, `--yes`,
    `--force`, `--version`; exit codes 0/10/1; `GH_TOKEN`/`GITHUB_TOKEN`;
    `MC_CODESIGN_IDENTITY`).
  * `legacy.go` normalizes this binary's own stamped identity,
    `BASE.N` → `vMAJOR.MINOR.PATCH`. Release builds since v0.16 stamp
    `X.Y.Z` without the `v` (`Makefile:173`, ci.yml build job:
    `ver="${GITHUB_REF_NAME#v}"`), so the normalize path is live on
    every release binary, not only legacy ones.
  * `lifecycle.go` adapts `internal/cli/service` to the library's
    `Lifecycle` and `Reconciler` (ExecRefresher runs the **new** binary's
    `setup-service --refresh --json` and parses its JSON receipt —
    `verdict`, `path`, `backup`, `changed`, `reloaded`, `reason`,
    `warnings`, `internal/cli/service/refresh.go:43-49`).
  * `codesign_darwin.go` / `codesign_other.go`: a `Transformer` that
    re-signs the staged binary on macOS so TCC grants survive an update
    ([0069-MADR-macos-permissions-and-sandbox-parity.md](0069-MADR-macos-permissions-and-sandbox-parity.md)).
  * `legacy_test.go` carries the **frozen v0.15.3 selector fixture** —
    a verbatim copy, not production code — proving an already-installed
    pre-0.16 client can still resolve the `v0.16.0` bridge file set.
* The release pipeline builds, stamps, renames, smoke-checks, cgo-checks,
  sums and stages **in this repository** (ci.yml build/release jobs:
  `build_matrix`, the `awk '{print $2}'` version smoke, the
  `go version -m` cgo assert of
  [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md)
  D21, the SHA256SUMS block, the APK staging, the spent v0.16.0 bridge
  copies), then hands a bare artifact to mcplib's publish workflow.
* The shipped installers are hand-written here: `scripts/install.sh`,
  `scripts/install.ps1`, with hermetic suites (`scripts/install_test.sh`,
  `scripts/install_ps1_test.ps1`, `install_ps1_unit_test.ps1`,
  [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md))
  and decided behaviors: install both binaries by default, run
  `mcremote setup-service` by default, keep the relay service opt-in via
  `--with-relay-service`
  ([0097-MADR-linux-curl-installer.md](0097-MADR-linux-curl-installer.md),
  [0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md),
  [0104-MADR-installer-linux-and-macos.md](0104-MADR-installer-linux-and-macos.md),
  [0156-MADR-only-the-posix-installer-learned-canonical-names.md](0156-MADR-only-the-posix-installer-learned-canonical-names.md)).
* `go-selfupdate-lib` is published (module cache resolves `v1.5.0`…
  `v1.13.0`; `git ls-remote` shows `v1.13.0` → peeled commit
  `5e199c831b5691ea687943e3c3fd495d50c739ed`). It requires `go 1.27.1`;
  this module is `go 1.27.2`
  ([0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md)),
  and its three `x/` module requirements are ones this module already
  meets or exceeds, so `go.mod` swaps one line for another
  ([0174-MADR-keep-dependencies-free-of-known-advisories.md](0174-MADR-keep-dependencies-free-of-known-advisories.md)).

### API compatibility, verified symbol by symbol

Every exported identifier this repository touches exists unchanged in
`go-selfupdate-lib@v1.13.0/selfupdate`; `*` marks additive items the
current code builds without knowing:

| This repository uses | Same in v1.13.0? |
|---|---|
| `Repository`, `Platform`, `DefaultLimits`, `Limits` | Yes |
| `NewGitHubSource`, `GitHubOptions` (incl. `APIBaseURL`, which the seam's tests require) | Yes |
| `NewExactAssetSelector`, the `Installer`/`ReleaseSource` seams | Yes (`*`: releasespec composes them) |
| `NewStandaloneInstaller`, `NewManagedInstaller` (`*`: `With`/`For` variants, `ReplaceBeforeStop`) | Yes |
| `NewStrictVersionPolicy`, `Request{Product, CurrentVersion, CurrentBuild, TargetVersion, CheckOnly, Force, Yes}` (`*`: `Platform`, `DryRun`, `Channel`) | Yes |
| `BuildKind` (`ReleaseBuild`/`LocalBuild`/`BuildUnknown`) | Yes |
| `Result`, `ExitCode`, `ErrUpdateAvailable` (`*`: `Warnings`, `RolledBack`, `ReplacedBeforeStop`) | Yes |
| `Lifecycle`, `Reconciler`, `ReconcileResult{Changed, Detail, State}` (`*`: `Warnings`) | Yes — **plus `EnabledLifecycle`**, which gates one behavior below |
| `Transformer`, `TransformRequest`, `NewTextReporter`, `NewTerminalConfirmer` | Yes — a nil `Transformer` stays a legal no-op, as `codesign_other.go` relies on |

### What `go-selfupdate-lib`'s stack is, piece by piece

All verified in the library source and guides, not assumed.

* **`buildinfo`** owns the build stamps
  (`buildinfo.version`, `buildinfo.kind`, link-time only;
  `LDFlags(tag)` renders the one `-ldflags` string). `Identity()` reports
  a release **only** when the stamp is exactly `release` and the version
  is a strict `vMAJOR.MINOR.PATCH` tag; anything else is a local build.
  `Current()`/`String()` give the identity command's output.
* **`selfupdate/cli`** is the canonical command: one flag set
  (`--check`, `--yes`/`-y`, `--force`, `--dry-run`, `--json`,
  `--version`, `--channel`), one stream rule (stdout is protocol output
  and empty without `--json`; everything else — events, prompts, the
  outcome line — goes to stderr), the 0/10/1 exit codes, signal
  cancellation and a run timeout. A cobra product binds `cli.Flags` on
  its pflag set and calls `Flags.Request`, `Run`, `Exit`
  (go-selfupdate-lib@v1.13.0/selfupdate/cli/{doc.go,command.go,flags.go,
  run.go}).
* **`releasespec`** is one embedded `selfupdate-release.json` (products
  with `package` and `identity_args`, platforms, packaging, extras with
  optional `{tag}` names, prerelease channels, installer settings) that
  the program parses for its asset selector and the workflows read for
  the build and the publish. It replaces the platform matrix copies this
  repository keeps in Makefile, CI, tests, and installers.
* **`build-selfupdate-release.yml`** builds every product on every
  platform with one fixed recipe (one linux runner, cross-compile,
  `CGO_ENABLED=0`, `-trimpath -buildvcs=true`, `buildinfo` stamps, no
  per-platform flags), checks each binary from its own build information
  (platform, tags, cgo, trimpath, commit, clean tree, toolchain, module),
  runs each product's identity command on every GitHub-hosted platform
  (linux/amd64, linux/arm64, darwin/arm64, windows/amd64 and
  windows/arm64), writes `SHA256SUMS`, takes extras from artifacts of
  the same run, and uploads the staged set. Off a tag it **rehearses**:
  the same recipe and checks on every branch push and PR.
* **`install.sh` / `install.ps1` templates** render from the same spec
  when `"installer"` is present: download by tag over HTTPS, strict
  `SHA256SUMS` verification before anything is installed, platform
  pick, install into `~/.local/bin` (never `sudo`) /
  `%LOCALAPPDATA%\Programs\<name>`, `.prev` retention, identity check
  per product with rollback, PATH handling, `--product`/`--dir`/
  `--version`/`--no-hooks`/`--dry-run`/`--uninstall` options,
  `before_install`/`after_install` hooks into product subcommands, exit
  codes 0/1/2/3, and `SELFUPDATE_INSTALL_BASE_URL` for local testing.
  The one-liner URLs keep the asset names
  (`install.sh`, `install.ps1`). Hook and `identity_args` arguments keep
  to `[A-Za-z0-9._:=/,+@%-]` because the installers embed them.
* **`selfupdate/service` backends** — `systemd`, `launchd` (and `scm`
  for Windows **services**) — each a `Lifecycle` and `EnabledLifecycle`
  and `Reconciler` and `service.Detacher`:
  `systemd.New(Options{Unit, Scope: User, …})`,
  `launchd.New(Options{Label, Domain, Plist, …})`. Stops wait out the
  service manager's own kill bound; `WaitHealthy` requires a **new
  instance** (new invocation ID / PID) that stays up for a settle
  window, then an optional application probe; `Enabled` answers
  `is-enabled` / `RunAtLoad <true/>` / the SCM start type.
  `service.NewExecReconciler(ExecOptions{Reconcile, RestoreFunc})` asks
  the product's own binary to reconcile its definition and print a
  receipt — "magic-cli-remote's refresh result" — schema fields
  `schema_version`, `verdict`, `path`, `backup`, `changed`, `reloaded`,
  `reason`, `warnings` — the same field names
  `internal/cli/service/refresh.go:43-49` already ships as a
  cross-version contract, and `RestoreFunc` covers this repository's
  in-process restore (amendment A1 of the library's 0011-MADR names it
  as "where magic-cli-remote restores").
* **`codesign.NewSigner`** re-signs the staged binary under a fixed
  `Identifier` with `codesign` resolved to an absolute path, fixing the
  two defects the migration guide names in this repository's own
  `codesignTransformer`: an identifier taken from the staging file's
  name, and a `codesign` found on `PATH`.

### What changes for this repository's contracts, `v1.4.1` → the stack

Each row was verified in the library source, not assumed.

| Change | Effect here |
|---|---|
| `ManagedInstaller` starts a **stopped** service only when the `Lifecycle` implements `EnabledLifecycle` (`selfupdate/managed.go:136-143`) | **Ends HealStart as-is.** `mcplib@v1.4.1`'s chain (`managed.go:64-133`, inventoried in [0159-MADR-the-windows-service-path-is-half-wired.md](0159-MADR-the-windows-service-path-is-half-wired.md)) calls `Start` after `Reconcile` unconditionally when installed. The reference backends implement `Enabled`, so the gate comes alive with them; the Windows adapter below must implement it too. |
| Command surface: stdout empty without `--json`, progress and outcome on stderr; `--dry-run`, `--json`, `--channel` appear; signals and a run timeout are installed | An **amendment of [0065](0065-MADR-update-automation.md)/[0103](0103-MADR-update-tracks-release-build-and-active-service.md)'s documented UX** (progress on stdout). `--check`, `--yes`, `--force`, `--version` and the 0/10/1/exit-1 codes stay. `docs/guides/config.md` is rewritten for the stream rule and the new flags; `--channel` is refused ("not offered by the version policy") until channels are configured — none proposed. |
| Identity: stamps move to `buildinfo`; `version` prints `buildinfo.Identity()` first ("`vX.Y.Z (release) 0123456789ab`"); `main.version/commit/date/main.buildKind` and the identity closures die; `NormalizeInstalled` retires (never-BASE.N stamps; strict-tag by construction) | The `version` output shape changes (`mcremote X.Y.Z (commit) date` today, `internal/cli/root.go:47`). The CI `awk '{print $2}'` smokes and the identity_args check replace it. The **frozen v0.15.3 fixture stays**; only live identity normalization goes — a strict-tag stamp cannot be `BASE.N`. |
| `WaitHealthy` requires a new instance up for a settle window (default 10 s), then an optional probe | Stricter than today's poll-`IsActive` (a crash loop flapping under the window now fails health and rolls back); the daemon health check `doctor` already runs can later back `launchd.Options.Probe`/`systemd.Options.Probe` — wiring it is deliberately not in this migration (see "not adopted"). |
| `Reconciler` runs the same child command but the receipt, warnings and in-process restore are library-owned (`ExecReconciler`) | A reconcile failure stays **fatal** with rollback, as decided (mcplib MADR 0005); the reconcile child and its JSON are unchanged — the product keeps owning the definition through `setup-service --refresh`. The half-wired Windows refresh is [0159](0159-MADR-the-windows-service-path-is-half-wired.md) D1's in-progress work, independent of this migration: `setup-service --refresh` learns the schtasks path in `internal/cli/service`, and `ExecReconciler` runs whatever the product ships. |
| EOF at the `[y/N]` prompt is a decline (exit 0); non-TTY without `--yes` still fails `ErrConfirmationRequired` (`selfupdate/confirmer.go:38-58`) | The documented "non-interactive apply without `--yes` fails" is preserved; `docs/guides/config.md` gains one sentence for the EOF edge. |
| A post-`complete` error is a warning (exit 0); `.<base>.selfupdate.pending` journal and `selfupdate-kept` backups appear beside the binary | Softer exit-code edge and new housekeeping files — both need a documentation line; `--json` carries them as `warnings`. |
| Only the selected binary and `SHA256SUMS` are checked; unrelated assets (the ~60 MB APK) no longer block updates; Windows busy-image replace is retried; `--version` is pinned to `ErrIntegrity` | Fixes this repository's shipped shapes directly; strengthens the documented "install that exact release". |
| Release pipeline: the build workflow replaces this repository's build/stamp/rename/smoke/cgo-assert/sum/stage steps; publish moves to the new module's workflow and `bridge-release` is deleted (GitHub rejects unknown inputs, and the `v0.16.0` bridge is spent) | The fixed recipe's build-information checks supersede the CI-side cgo assert ([0116](0116-MADR-windows-and-linux-arm64-build-targets.md) D21) and the release-lane side of `make verify-build-metadata` (`netgo`/`osusergo` are inert under `CGO_ENABLED=0`; the Darwin-omits-`netgo` invariant of [0059](0059-MADR-native-paths-and-linux-macos-parity.md) D9 holds with no tags added anywhere). The local dev-build gate stays. Rehearsals run on every push and PR instead of the release recipe being first tried on the tag. |
| Installers: `scripts/install.sh`, `scripts/install.ps1` and their suites are deleted; `install.sh`/`install.ps1` become template-rendered extras (same URLs) | Operator-visible amendments below; the Android APK becomes a spec `extra` from the build run's artifact (`extras-artifact-name`), so the android-apk job stays and feeds the build workflow. |
| Publish workflow support (`prerelease_channels`, `--verify-attestation`, `ghattest`) | Not proposed: stable-only stays decided; provenance verification needs a logged-in `gh`, which a service host has no reason to own. Available later without another migration. |

## Decision Drivers

* One self-update implementation, the one that is maintained: the fleet's fixes land in `go-selfupdate-lib`; `mcplib/selfupdate` is the snapshot it was extracted from.
* Fleet consistency: `mcremote`/`mcrelay` get the same command surface, stamps, spec, release recipe, and installer behavior as every other fleet product, instead of keeping a private variant of each next to the shared library.
* Delete duplicated maintenance, not just a dependency: the hand-rolled command wrapper (flags, prompts, exit mapping, reporter, confirmer), the platform matrix copies, the release build/stage recipe, the codesign transformer, and the shipped installers are all things the library now ships and this repository pays to maintain anyway.
* Preserve this repository's decided operator contracts, and amend each one visibly in this record rather than silently: exit codes 0/10/1; HealStart of a crashed unit; `--force` as "replace a local build"; the release contract — exact asset names, `SHA256SUMS` format, the published `v0.16.0` bridge an installed pre-0.16 client needs; install both binaries by default.
* The library's own guides are the path, and they cite this repository by name in them; inventing a private subset re-opens the divergence problem the extraction was meant to close.
* Dependency governance: advisory-clean pin, toolchain floor met, `x/` requirements already present
  ([0169](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md), [0174](0174-MADR-keep-dependencies-free-of-known-advisories.md)).
* Sequencing risk: a tag is still the first time the publish path runs end to end; rehearsals retire most of that risk before the tag, and the first tagged release after the merge is the release-contract acceptance.

## Considered Options

* **Option A — Deep standardization (chosen):** move to `go-selfupdate-lib v1.13.0` and adopt the stack: buildinfo stamps, the `cli` command surface bound through cobra, the embedded `releasespec`, the build workflow, the publish workflow move with `bridge-release` deleted, reference `systemd`/`launchd` lifecycles with `NewExecReconciler`, `codesign.NewSigner`, and the generated installers replacing `scripts/install.sh` and `scripts/install.ps1`.
* **Option B — Deep standardization, but keep the hand-written installers:** adopt everything except `"installer"`; keep `scripts/install.sh`, `scripts/install.ps1` and their suites as extras.
* **Option C — Rehome plus targeted gap-closing:** swap the import and the publish pin, add `Enabled`, replace the codesign transformer, change nothing else.
* **Option D — Stay on `mcplib`:** bump to its final `selfupdate` (`v1.6.0`) or remain at `v1.4.1`.

## Decision Outcome

Chosen option: "**Option A — Deep standardization**: move to `go-selfupdate-lib v1.13.0` and adopt the stack: buildinfo stamps, the `cli` command surface bound through cobra, the embedded `releasespec`, the build workflow, the publish workflow move with `bridge-release` deleted, reference `systemd`/`launchd` lifecycles with `NewExecReconciler`, `codesign.NewSigner`, and the generated installers replacing `scripts/install.sh` and `scripts/install.ps1`", because every piece replaces something this repository maintains privately today with the fleet-owned equivalent that already encodes this repository's own decided behaviors (the ExecReconciler receipt is this repository's refresh contract; the migration guide names its codesign transformer and its bridge input; the build workflow's checks subsume the repository's own CI asserts); because the operator-visible changes are enumerated below and each is an amendment of a documented contract that this record makes explicit rather than silent; and because deferring any piece (Option C) or carving out the installers (Option B) keeps the most maintenance-heavy surface hand-rolled while paying the full dependency cost of the library.

### The shape, per surface

* `internal/updateclient` stays the one seam and keeps what is
  product-specific: the embedded `selfupdate-release.json`, the updater
  factory (source, policy, per-GOOS lifecycle choice, codesign identity),
  the cobra binding of `cli.Flags`, and the Windows lifecycle adapter.
  It loses the hand-rolled command body, the identity normalization, the
  lifecycle/reconcile adapters for Linux/macOS, and the codesign
  transformer.
* Composition on Linux/macOS:
  `NewManagedInstaller(standalone, systemd.New(...)|launchd.New(...), service.NewExecReconciler(...))`
  — the backend supplies `Lifecycle` and `Enabled` (HealStart gate live
  and honest), `NewExecReconciler` supplies the reconcile child this
  repository already ships, with the in-process `RestoreFunc`.
* Windows keeps the Task Scheduler model decided by
  [0159-MADR-the-windows-service-path-is-half-wired.md](0159-MADR-the-windows-service-path-is-half-wired.md)
  (in progress): the repo's adapter stays as the Windows `Lifecycle`,
  grows an `Enabled` probe (the task's at-logon presence), and its
  reconcile remains the product's `setup-service --refresh` once 0159 D1
  ships it. The library's `scm` backend is for Windows *services* and is
  not adopted.
* The spec, roughly:

```json
{
  "schema": 1,
  "products": [
    {"name": "mcremote", "package": "./cmd/mcremote", "identity_args": ["version"]},
    {"name": "mcrelay",  "package": "./cmd/mcrelay",  "identity_args": ["version"]}
  ],
  "platforms": [
    {"os": "linux", "arch": "amd64"},
    {"os": "linux", "arch": "arm64"},
    {"os": "darwin", "arch": "arm64"},
    {"os": "windows", "arch": "amd64"}
  ],
  "packaging": "binary",
  "extras": [
    {"name": "magic-cli-remote-{tag}-arm64.apk"}
  ],
  "installer": {"name": "magic-cli-remote", "env_prefix": "MAGIC_CLI_REMOTE"}
}
```

  The list of platforms encodes [0116](0116-MADR-windows-and-linux-arm64-build-targets.md) D19 (`windows/arm64` absent) and [0120-MADR-retire-the-darwin-amd64-target.md](0120-MADR-retire-the-darwin-amd64-target.md) (`darwin/amd64` absent) exactly once, read by client, build and publish. A unit test calls `spec.Product` and `spec.AssetSelector` per product so a spec the program cannot use fails in tests, not in a release.
* CI: jobs build (rehearsing) → release
  (`needs.build.outputs.*` → publish), `apk` feeds the build via
  `extras-artifact-name`; the repository's own build/stamp/rename/
  smoke/cgo/sum/stage steps and the `bridge-release` input and bridge
  staging go. Test, lint, unit, doctor and mobile jobs stay and can be
  `need`ed by the build.
* Installers: hooks keep the decided defaults — one `after_install` hook
  runs `mcremote setup-service` (the stock install still ends with a
  unit), nothing runs for `mcrelay` (the relay service stays opt-in, now
  by running `mcrelay setup-service` afterwards instead of an installer
  flag). `--no-hooks` replaces `--no-service`.

### The amendments, stated

1. **HealStart becomes "enabled ⇒ heal".**
   [0103](0103-MADR-update-tracks-release-build-and-active-service.md)
   F2's matrix keeps its cases; the "unit file means the daemon is
   supposed to run" rule becomes "**enabled** unit file means it". This
   product's supplied state always answers enabled (systemd
   `WantedBy=default.target` plus the `enable` `setup-service` runs,
   `internal/cli/service/mcremote.user.service.tmpl:78`,
   `internal/cli/setup_service.go:85`; launchd `RunAtLoad` + `KeepAlive`
   always true, `internal/cli/service/plist_render.go:169-170`; the
   Windows task runs at logon), so the change bites only an operator who
   deliberately disabled the unit — the honest case. A deliberately
   disabled unit is no longer healed.
2. **`update` output moves to stderr; stdout becomes protocol-only.**
   `--dry-run` and `--json` appear; `--channel` is refused until
   channels are configured. Scripts that captured progress on stdout
   must switch (none known in this repository; the docs are the
   contract).
3. **`version` output changes shape** to `buildinfo.Identity()` —
   `vX.Y.Z (release) 0123456789ab` — because the build workflow's
   identity check and the installers both require it. The CI smokes that
   parsed `$2` change with it.
4. **The installer's relay opt-in flag retires.** `--with-relay-service`
   and `--no-service` (and their tests) go; the equivalents are
   `mcrelay setup-service` after install and `--no-hooks`. The condition
   of adopting the hook: `setup-service` on a host with no supported
   service manager must not fail the install (today's installer detects
   and hints); the PLAN verifies what `setup-service` exits there and,
   if needed, that behavior ships as part of this work — otherwise the
   `after_install` hook would turn a working install into exit 3 on
   unsupported hosts.
5. **Install exit codes become 0/1/2/3** (the templates'), and the
   Windows PATH write is the template's, not ours.
6. **A post-`complete` failure is exit 0 plus a warning; EOF at the
   prompt declines; journal/kept-backup files appear.** All documented;
   `--json` surfaces them machine-readably.

### Consequences

* Good, because `mcplib` and its `go.mod`/`go.sum` entries leave, one dependency-focused module covers command, stamps, spec, release, services, codesign and installers, and three debugging passes of fixes arrive — two of which address this repository's real shapes (the always-uploading APK extra; the Windows busy-image replace).
* Good, because the release recipe is rehearsed on every push and PR instead of being tried first on the tag, and each binary is checked from its own build information and run (identity command) on every GitHub-hosted platform — strictly more than today's host-native smoke.
* Good, because maintenance this repository duplicates retires: the hand-rolled command wrapper, the platform-matrix copies (Makefile, CI, tests), the release build/stage recipe and its asserts, the codesign transformer, the legacy identity normalizer, and the shipped installers with their hermetic suites.
* Good, because reconciliation, the receipt and in-process restore become library-owned while the product keeps owning the definition: the child command and its JSON are unchanged, a reconcile failure stays fatal with rollback, and `ExecReconciler`'s receipt is the contract `refresh.go` already ships.
* Good, because HealStart survives with an honest gate, `WaitHealthy` stops accepting a flapping restart as healthy, and the pre-0.16 published bridge is untouched — the frozen fixture keeps proving it, with only its import path changed.
* Neutral, because `internal/updateclient` shrinks rather than disappears: the spec, the per-GOOS composition, the Windows adapter and the codesign identity stay product code.
* Neutral, because local dev builds keep the Makefile, its stamps and `make verify-build-metadata`; only the release lane's build moves into the workflow.
* Bad, because operators see behavior changes that no mechanical migration could avoid: the stream flip, new flags, a different `version` string, installer option changes, the relay opt-in flag becoming a manual command, the softer exit-0 warning edge, and new housekeeping files. Each is enumerated above; the docs must say all of it.
* Bad, because the hand-written installer suites ([0097](0097-MADR-linux-curl-installer.md), [0099](0099-MADR-installer-service-state-verification.md), [0104](0104-MADR-installer-linux-and-macos.md), [0156](0156-MADR-only-the-posix-installer-learned-canonical-names.md), [0170](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md)) are deleted with the scripts, and their decided behaviors rest on the templates honoring them ([0156](0156-MADR-only-the-posix-installer-learned-canonical-names.md)'s canonical-name lesson included — the templates' names and URLs are the same, so its bridge concern carries over intact).
* Bad, because the Windows adapter is still this repository's to finish ([0159](0159-MADR-the-windows-service-path-is-half-wired.md) in progress): the deep adoption adds the `Enabled` probe to it but neither fixes nor worsens the half-wired refresh; it must land with or after 0159 D1 for Windows managed updates to work at all.
* Bad, because the first tagged release after the change is the end-to-end acceptance: an imperfect release cannot be unpublished (immutable), and although rehearsals cover the build, the publish job itself first runs at the tag.

### Confirmation

* `grep -rn 'mcplib/selfupdate' --include='*.go' .` finds nothing; `grep -rn 'maccavelli/mcplib/.github/workflows' .github` finds nothing; the publish `with:` block has no `bridge-release`.
* `go build ./...`, `go vet ./...`, the full suite, `make preflight`, `make pre-add-check` ([0174](0174-MADR-keep-dependencies-free-of-known-advisories.md)) and Windows gates ([0145-MADR-local-windows-ci-style-tests.md](0145-MADR-local-windows-ci-style-tests.md)) pass; `scripts/acceptance-windows.ps1` and `scripts/acceptance-windows-service.ps1` run against the generated installers, and the live-installer tradition of [0098-MADR-ephemeral-cloud-install-verification.md](0098-MADR-ephemeral-cloud-install-verification.md) is rerun on the generated installers before the shipped scripts are deleted.
* Unit tests pin the seams: `spec.Product`/`spec.AssetSelector` per product; `Enabled=true` + installed + down ⇒ started (and `false` ⇒ not) on each OS adapter; the `ExecReconciler` receipt round-trip against `refresh.go`'s emitter; the codesign signer's fixed identifier and resolved binary; the identity command's first line.
* CI observables: a rehearsal on a branch push produces a staged, checked, identity-run artifact for all four platforms and both products with the APK extra, `SHA256SUMS` naming canonical names only; a publish produces an immutable, attested release whose asset set the frozen v0.15.3 fixture still resolves; `update --check` from an older installed binary exits 10 against it.
* Docs: `docs/guides/config.md`'s update section rewritten (streams, flags, warnings, journal, EOF); README and operator guides' installer options corrected.

## Pros and Cons of the Options

### Option A — Deep standardization (chosen)

* Good, because it finishes what the 0005-extraction started: one shared implementation, and this repository stops maintaining a private variant of flags, stamps, matrix, build, signing and installers beside it.
* Good, because the pieces interlock by design — the spec feeds client, build and publish; the stamps feed the identity checks; the receipt feeds reconciliation — and adopting them together is less work than adopting any two halves.
* Good, because the operator-facing changes arrive once, in one release, with one documented delta, instead of dribbling across future migrations.
* Neutral, because `internal/updateclient` remains, smaller, and the cobra binding is retained as a thin `cli.Flags` binding rather than `cli.Command` directly.
* Bad, because the blast radius of one migration covers command UX, release infrastructure, services and installers at once; the phases in the PLAN must land in dependency order (stamps before build workflow; spec before both workflows; identity command before the identity run works).
* Bad, because the installer and stream amendments are user-visible and the installer suite deletion gives up bespoke behavior coverage this repository built carefully ([0099](0099-MADR-installer-service-state-verification.md)'s state machine in particular).

### Option B — Defer the generated installers

* Good, because it keeps the two most operator-coupled artifacts (installers) in this repository's proven hands for one more cycle, and the spec's `installer` block can be added later without another migration.
* Neutral, because `install.sh`/`install.ps1` continue as spec `extras`, and their test suites keep running.
* Bad, because the installers are the maintenance heaviest piece this decision exists to shed — hand-maintained bootstrap scripts with per-OS branching — and keeping them re-opens the fork ([0156](0156-MADR-only-the-posix-installer-learned-canonical-names.md) is the cautionary record).
* Bad, because the hook-listing amenity experiments glimpsed in the templates (`--product`, `--uninstall`, attestation) would each need duplication to offer at all.

### Option C — Rehome plus targeted gap-closing

* Good, because the churn is smallest: imports, one probe meeting, one transformer swap, one workflow pin, one input deleted.
* Bad, because it keeps this repository paying for the whole duplicated surface the library now ships, and the fleet divergence the extraction closed re-opens at the command, spec and pipeline levels anyway.
* Bad, because every deferred piece arrives later anyway, with a second doc pass, a second test pass and a second release.

### Option D — Stay on `mcplib`

* Good, because nothing moves today.
* Neutral, because bumping to `mcplib v1.6.0` delivers the first-pass fixes (APK, Windows replace) through a module whose other packages (the MCP go-sdk stack, `llmprovider`, `wizard`) this repository does not use.
* Bad, because that is where the mcplib path dead-ends: nothing after the extraction (the enabled gate, the canonical command, the spec, the build workflow, the templates, the packaged codesign signer) exists in mcplib at all.
* Bad, because the successor publish workflow no longer accepts `bridge-release`, so the publish side must move regardless — staying buys nothing there.

## More Information

* Library evidence (cited relative to the module): `docs/guides/migrating-from-mcplib-selfupdate.md` (§2 behavior deltas, §3 release workflow, §4 check greps, §5 canonical command with the cobra recipe, §6–§13 version deltas); `docs/guides/building-releases.md` (§1 spec, §2 embed, §3 identity command, §4 workflow wiring and immutability, §5 rehearsals, §6 extras-from-artifacts — the APK pattern, §7 archives, §8 build recipe and checks, §11 moving from a Makefile, §12 installers, hooks, options, exit codes, moving from a hand-written installer); `docs/guides/extending-selfupdate.md` ("Run as a service": backend selection, stop/health/`Enabled` semantics, `NewExecReconciler` for a program that owns its definition); `selfupdate/service/execreconciler.go` (Receipt, schema 1 of mcremote's refresh contract, `RestoreFunc` for in-process restore); `selfupdate/service/systemd/unit.go`, `selfupdate/service/launchd/job.go` (Options); `selfupdate/managed.go:136-143` (the `Enabled` gate); `selfupdate/confirmer.go:38-58` (TTY/EOF/nil-`In`); `selfupdate/codesign/codesign.go:151` (`NewSigner`); `buildinfo/buildinfo.go` (stamps, `Identity`, `LDFlags`); `selfupdate/cli/{doc.go,command.go,flags.go,run.go}` (command surface, streams, signals, exit codes); `scripts/verify-selfupdate-release.sh` (the verifier whose contract
  [0132-MADR-verify-the-apk-by-github-asset-digest.md](0132-MADR-verify-the-apk-by-github-asset-digest.md)
  records, moved with the package).
* Current pin evidence: `go.mod:12`; `.github/workflows/ci.yml:1042` (`…mcplib/.github/workflows/publish-selfupdate-release.yml@8d4d89ff34ff79c1ed65f223cc3d83b3ec0550aa # mcplib v1.4.1`); `internal/updateclient/{client,legacy,lifecycle,codesign_darwin,codesign_other}.go` and its four `_test.go` files; `cmd/mcremote/main.go:29`, `cmd/mcrelay/main.go:25` (`ExitCode`); `internal/cli/root.go:46-48` (`VersionString`), `internal/cli/update.go`, `internal/relay/update.go`; `internal/cli/service/{control.go,refresh.go:43-49,exec_refresher.go,plist_render.go:169-170}`; `Makefile:173-190,222-239` (release and dev stamps); `scripts/{install.sh,install.ps1,install_test.sh,install_ps1_test.ps1,install_ps1_unit_test.ps1,verify-build-metadata.sh}`; `docs/guides/config.md:637-717`; `apps/mobile/lib/data/update/app_update.dart:112-113` (comments citing the publisher/verifier).
* Workflow pins, both resolved to the peeled commit of the annotated `v1.13.0` tag with `git ls-remote https://github.com/maccavelli/go-selfupdate-lib 'refs/tags/v1.13.0^{}'` (2026-10-10; tag ref `0a121fbc2001f7013cac58f65399ab72fef6f47d`):
  `build-selfupdate-release.yml@5e199c831b5691ea687943e3c3fd495d50c739ed` and `publish-selfupdate-release.yml@5e199c831b5691ea687943e3c3fd495d50c739ed # go-selfupdate-lib v1.13.0`, moved together with the `go.mod` requirement, per the build workflow's own version check.
* Relationships this record amends or touches, to update when it is accepted: [0103](0103-MADR-update-tracks-release-build-and-active-service.md) (HealStart matrix — enabled gate; stdout UX), [0065](0065-MADR-update-automation.md) (command surface), [0059](0059-MADR-native-paths-and-linux-macos-parity.md) D9 (per-OS build tags — release recipe flattens under cgo-off, local gates stay), [0116](0116-MADR-windows-and-linux-arm64-build-targets.md) D19/D21 (platform set — encoded in the spec; cgo assert — subsumed), [0097](0097-MADR-linux-curl-installer.md)/[0099](0099-MADR-installer-service-state-verification.md)/[0104](0104-MADR-installer-linux-and-macos.md)/[0156](0156-MADR-only-the-posix-installer-learned-canonical-names.md)/[0170](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) (installer behavior moves to the templates), [0159](0159-MADR-the-windows-service-path-is-half-wired.md) (Windows adapter stays schtasks and grows `Enabled`; refresh interplay stated above), [0132](0132-MADR-verify-the-apk-by-github-asset-digest.md) (`SHA256SUMS`/canonical-name contract preserved). Historical records that cite `mcplib v1.4.1` file:line evidence stay as written — snapshots — with this record as the pointer forward.
* Deliberately **not** adopted (each available later without another migration): prerelease channels (`spec.prerelease_channels` + `--channel` live), provenance verification (`ghattest`, installer `--verify-attestation` — needs a logged-in `gh` on the updating host), archive packaging, `ReplaceBeforeStop` (its conditions — no self-spawn, no stop hooks — do not all hold here), the service `Detacher`/handoff (updates here run from an operator shell, not inside the daemon), `systemd.Ready()`/`Type=notify` (a unit-template change of its own), the `Probe` readied health checks (wire the existing probes into `Options.Probe` after the backend lands).
* Implementation Plan: none yet. Per
  [0105-MADR-mutating-work-requires-madr-and-plan.md](0105-MADR-mutating-work-requires-madr-and-plan.md),
  a PLAN under this number precedes any code change, phased in dependency
  order (rehome → stamps/identity → spec → command surface → services/
  reconcile/codesign → build+publish workflows → installers), with this
  record's Confirmation section and the migration guide §4 greps as its
  verification bar, and the first tagged release after the merge as the
  release-contract acceptance.

## Amendment — 2026-10-13: fullest adoption, `v1.15.2`, and the owner's installer decisions

The owner's direction (2026-10-13): *"I want magic to offload as much as possible to the
libraries, and provide the fullest adoption possible."* This amendment applies that direction to
Option A. It records the facts that changed since the draft, as found by
[0054-REPORT-go-selfupdate-lib-full-adoption-assessment.md](../reports/0054-REPORT-go-selfupdate-lib-full-adoption-assessment.md)
and by a scratch-copy probe on 2026-10-13. It also records four owner decisions, and the decisions
that follow from them. Where a decision below contradicts the draft above, this amendment governs.
The draft's text is kept as the record of what was first proposed. The direction does not extend
to `go-llmprovider-sdk`. Migrating Magic's model catalogs to it is out of scope by
[0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md).

### Facts that changed

* **The target is `v1.15.2`, not `v1.13.0`.** `v1.14.0` (2026-10-10), `v1.15.0`, `v1.15.1`
  (2026-10-11) and `v1.15.2` (2026-10-12) have been published. The library's migration guide
  states that each one only adds to the API or changes none, as reported by its `make apicheck`
  (`docs/guides/migrating-from-mcplib-selfupdate.md` §2, §14, §15). `git ls-remote` on
  2026-10-13 resolves `refs/tags/v1.15.2^{}` to `436a7f5976a6815491c7384527fef90869574a1b`. That
  commit replaces both workflow pins under "More Information". The library still requires only
  `golang.org/x/mod v0.40.0`, `x/sys v0.47.0` and `x/term v0.43.0`, which this module already
  exceeds.
* **The Windows backend exists.** `v1.14.0` added `selfupdate/service/taskscheduler`, a
  `Lifecycle`, `EnabledLifecycle`, `Reconciler` and `service.Detacher` for a per-user task, built
  from this repository's 0159 measurements. `v1.15.0` makes it refuse to stop a disabled task it
  could not start again (`ErrTaskDisabled`). `v1.15.2` bounds its stop grace and re-enables a
  task whose stop failed. The draft's "Windows keeps … the repo's adapter" no longer holds.
* **0159 D1 has shipped** (`internal/cli/service/refresh_schtasks.go`). The draft's condition that
  Windows "must land with or after 0159 D1" is met.
* **The Go API matches symbol for symbol.** A script over `git show v1.15.2:selfupdate/*.go` finds
  every one of the 26 `selfupdate` identifiers this repository uses. The library exports 219
  names, mcplib `v1.4.1` exports 95, and none of the 95 is missing.
* **A rehome on its own changes behavior, and no test notices.** Observed on a scratch copy on
  2026-10-13: with only the import path changed and `go mod tidy` run, `go build ./...`, `go vet`
  (darwin/arm64, windows/amd64, and linux/amd64 with `CGO_ENABLED=0`) and `go test` of
  `internal/updateclient`, `internal/cli`, `internal/cli/service`, `internal/relay` and
  `internal` all pass. But mcplib `v1.4.1` starts any installed service after an update
  (`selfupdate/managed.go:92`). `v1.15.2` starts a stopped one only when the `Lifecycle` reports
  it enabled (`selfupdate/managed.go:135-145`). `internal/updateclient/lifecycle.go` implements no
  `Enabled`, so after a bare rehome a stopped daemon is no longer restarted by an update, and the
  suite stays green. The report's Phase 1 statement, "Behavior does not change, because the API is
  unchanged", is wrong in this one respect.
* **`go mod tidy` adds two indirect lines.** In the same probe, `github.com/davecgh/go-spew` and
  `github.com/pmezard/go-difflib` became explicit `// indirect` requirements. mcplib had supplied
  them. The MCP go-sdk, `invopop/jsonschema`, `x/oauth2` and `yaml/v4` leave the module graph.
* **The installers have no uninstall hook.** `releasespec.Installer.Hooks` accepts only
  `before_install` and `after_install` (`selfupdate/releasespec/installer.go`, `HookBeforeInstall`
  and `HookAfterInstall`). A template `--uninstall` removes binaries and `.prev` files only, so the
  shipped installer's removal of service definitions (gap G4 in the report) has no library seam.
* **`installer.name` names the Windows folder and the default environment prefix.**
  `%LOCALAPPDATA%\Programs\<Name>`, with the prefix derived from `Name` unless `env_prefix` is set
  (`releasespec.Installer`). The draft's `"env_prefix": "MAGIC_CLI_REMOTE"` would rename every
  variable operators use today (`MCREMOTE_VERSION`, `MCREMOTE_INSTALL_DIR`, `MCREMOTE_NO_SERVICE`,
  `MCREMOTE_INSTALL_NO_PATH_UPDATE`).
* **The release staging is already compatible with the library's publish workflow.** It copies
  regular files only (`cp -f`, `.github/workflows/ci.yml:980-988`), and the three extra names
  match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. The only incompatible inputs are `bridge-release` and
  the `v0.16.0` bridge step (`ci.yml:990-1014`, `:1048`).

### Owner decisions (2026-10-13)

* **O1 — Fullest adoption.** Every concern the library owns moves to it. Product code remains only
  where the library deliberately does not own the concern: the service definition and its refresh
  child, the release spec's content, the signing identity, the Android APK build, and the
  developer `make install` path.
* **O2 — The supervisors move into `setup-service`.** runit, s6, OpenRC (user and system), and the
  broken-systemd fallback, now handled only by `scripts/install.sh`, become Go code in
  `mcremote setup-service`, with tests. The post-setup state verification of
  [0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md)
  moves with them. No operator loses supervision when the scripts are deleted.
* **O3 — Windows adopts the library's install folder.** Binaries install into
  `%LOCALAPPDATA%\Programs\<installer.name>`, replacing
  [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md)
  D13's `%LOCALAPPDATA%\Programs`. Existing installs are migrated, not abandoned.
* **O4 — ACP is decided separately.** The `acp-go-sdk2` adoption is not part of this record. It is
  [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md).

### Decisions that follow

* **D1 — Pin `go-selfupdate-lib v1.15.2`,** or the latest tag at implementation time if the
  migration guide records it as API-compatible. `go.mod` and both workflow pins move together, the
  pins to the tag's peeled commit.
* **D2 — No rehome without the backends.** The import swap lands in the same change as
  `systemd.New`, `launchd.New` and `taskscheduler.New`, each of which implements
  `EnabledLifecycle`, so "enabled ⇒ heal" (amendment 1 of the draft) is true from the first build
  that drops mcplib. No interim release ships `lifecycle.go` against the new library.
* **D3 — Windows uses `taskscheduler`.** `taskscheduler.New(Options{Name: <the task name
  setup-service registers>})` with `service.NewExecReconciler` replaces the product's Windows
  adapter, `control_schtasks.go`'s update-path functions, and the draft's planned `Enabled` probe.
  The library's `scm` backend stays unadopted: the product runs a per-user task, not a Windows
  service. The draft's Consequences bullet "the Windows adapter is still this repository's to
  finish" is withdrawn.
* **D4 — Reconciliation is `service.NewExecReconciler`.** It runs
  `ExecOptions{Reconcile: []string{"setup-service", "--refresh", "--json"}}`, with `RestoreFunc`
  wrapping the product's in-process restore (`RestoreUnitBackup`, `internal/cli/service/refresh.go`).
  `exec_refresher.go` and the product `Reconciler` are deleted.
* **D5 — The backends own health.** Each backend's `WaitHealthy` (a new instance that stays up for
  a settle window) replaces the product's `IsActive` poll. Fullest adoption also wires what the
  draft deferred under "not adopted":
  * the daemon's existing health check as each backend's `Options.Probe`;
  * a graceful shutdown request as `taskscheduler.Options.RequestStop`, where the daemon offers
    one;
  * `systemd.Notify` from the daemon, with `Type=notify` in the user unit template.
* **D6 — `doctor` and `update` read one source.** `service.ProbeStatus` is rebased on the
  backends' `Installed`, `Enabled` and running probes, so the two commands cannot disagree.
  `Stop`, `Start` and `control_schtasks.go` are deleted once nothing calls them.
* **D7 — Tests use `selfupdatetest`.** The library's `FakeSource` and `GitHubServer` replace the
  product's hand-built release fixtures wherever they cover the same case. The frozen v0.15.3
  selector fixture stays, with only its import path changed: it is a contract fixture.
* **D8 — The environment prefix is `MCREMOTE`.** The spec sets
  `"installer": {"name": "magic-cli-remote", "env_prefix": "MCREMOTE"}`. Two variables are renamed,
  and both renames are documented: `MCREMOTE_NO_SERVICE` becomes `MCREMOTE_NO_HOOKS`, and
  `MCREMOTE_INSTALL_NO_PATH_UPDATE` becomes `MCREMOTE_NO_PATH_UPDATE`. Tests use
  `SELFUPDATE_INSTALL_BASE_URL` in place of `MC_TEST_BASE_URL`.
* **D9 — The Windows folder is `%LOCALAPPDATA%\Programs\magic-cli-remote`** (O3, D8). It holds
  both products, so it carries the release's name rather than one product's. The owner's question
  on 2026-10-13 illustrated the folder as `…\Programs\mcremote`; this record proposes the library's
  default, the repository name, and the owner confirms the name when accepting this record. The
  migration: `setup-service --refresh`, which every managed update already runs, finds a task whose
  `Exec` path is under the old folder, re-registers the task at the new path, and moves the user
  `PATH` entry. The first managed update after the change performs the move. A fresh install never
  sees the old folder.
* **D10 — `setup-service` grows to close the installer gaps** (O2). It gains:
  * the runit, s6 and OpenRC backends and the broken-systemd fallback (G1);
  * a verified "running, enabled at boot" result with crash-loop detection, so the
    `after_install` hook's exit status is honest (G2);
  * environment-variable equivalents of the per-invocation installer options `--no-linger`,
    `--force-service` and `--with-relay-service`, because hook arguments are fixed in the spec
    (G3);
  * `setup-service --uninstall`, which stops the service and removes every definition it can own
    (G4);
  * an exit status of 0, with a printed hint, on a host with no supported service manager, which
    meets the draft's amendment 4.
* **D11 — Ask the library for a `before_uninstall` hook.** Without one, a template `--uninstall`
  leaves a service definition pointing at a deleted binary. The request is made under
  `go-selfupdate-lib`'s own records, and this product adopts the hook when it ships. Until then,
  the documented uninstall is `mcremote setup-service --uninstall`, then the installer's
  `--uninstall`, and the installer's uninstall output is checked to say so. This is the one gap
  where the library, not this repository, has to move.
* **D12 — BusyBox `wget` is refused.** The templates refuse a `wget` without `--https-only` (G5).
  The stricter policy is accepted, not worked around: Alpine-style hosts install `curl`, and the
  operator guide says so.
* **D13 — `/health` reports `buildinfo` as it is.** A release's version becomes `v0.X.Y`, where it
  is `0.X.Y` today (`internal/ws/server.go:531`). No code in `apps/mobile/lib` was found reading
  the field. The change is documented for operators who poll `/health`.
* **D14 — The publish workflow moves in the same change as the rehome.** That change switches the
  `uses:` line to `maccavelli/go-selfupdate-lib/.github/workflows/publish-selfupdate-release.yml`
  at the D1 pin, and deletes `bridge-release` and the bridge staging step. After it, no file
  references mcplib except the historical records. The build workflow (`build-selfupdate-release.yml`)
  follows in a later phase, because it depends on `buildinfo` and the embedded spec.

### What stays deliberately unadopted

Each item is available later without another migration, and each has a reason other than effort:

* **Prerelease channels:** no channel is a product decision yet, and stable-only remains decided.
* **Client-side provenance verification (`ghattest`):** it needs a logged-in `gh` on the updating
  host, which a service host has no reason to own. The templates' `--verify-attestation`
  installer option is adopted with the templates.
* **`ReplaceBeforeStop`:** its conditions, no self-spawn and no stop hooks, do not all hold here.
* **The `service.Detacher` handoff:** updates run from an operator shell, never inside the daemon,
  so nothing would call it. If the phone ever triggers updates, this is the seam to adopt.

### Consequences of this amendment

* Good, because mcplib leaves in one change with no window in which a stopped daemon is silently
  left down, and that change also deletes the product's Linux, macOS and Windows lifecycle
  adapters, its refresher wrapper and its codesign transformer.
* Good, because Windows managed updates gain the library's measured Task Scheduler behavior. That
  includes the `v1.15.x` fixes: refusing a disabled task, a bounded stop grace, and re-enabling
  the task after a failed stop.
* Good, because no operator loses supervision or uninstall behavior when the hand-written
  installers go (O2, D10, D11). The installer itself becomes the library's, with the
  library's tests.
* Neutral, because `setup-service` grows: the supervisors, verification and uninstall move into
  it rather than disappearing. That is product code the library deliberately leaves to the
  product, now in Go with tests instead of in shell.
* Bad, because D9 migrates every existing Windows install once, and a migration bug there would
  point the task at a missing binary. Its tests must cover the old-folder, new-folder and
  already-migrated cases, and `scripts/acceptance-windows-service.ps1` must run it before the
  first tag.
* Bad, because D11 depends on a change in another repository. Until it ships, an uninstall is two
  commands, and an operator who runs only the installer's leaves a dangling definition.
* Bad, because two installer variables are renamed (D8) and `/health`'s version gains a `v` (D13).
  Both are documented, and neither can be avoided under the library's conventions.

### Confirmation added by this amendment

* On a scratch copy, the rehome with `lifecycle.go` still in place is shown to leave an installed,
  enabled, stopped service stopped. With the backends wired, the same test shows it started. The
  test is seen failing first.
* Each backend: installed, enabled and stopped means started; disabled and stopped means not
  started; running means restarted. On Windows, a disabled task that is running gives
  `ErrTaskDisabled` and nothing changes.
* `setup-service --refresh` migrates a task and a `PATH` entry from `%LOCALAPPDATA%\Programs` to
  `%LOCALAPPDATA%\Programs\magic-cli-remote`, and is a no-op on an already-migrated install.
* `setup-service --uninstall` leaves no definition for any backend it supports, on a scratch user
  or a VM, never on the operator's own account.
* After the rehome change, a file scan finds `mcplib` only under `docs/decisions/` and
  `docs/reports/`.

## Amendment — 2026-10-19: re-verified against `da95b793` and the current library releases

~~**Status: proposed.**~~ **Accepted 2026-10-19.** The owner accepted this record, the
2026-10-13 amendment and this one, with the answers A1–A4 below. That meets step 2 of
[0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)
D3. Each S phase still starts only when the owner approves it by name.

The owner asked on 2026-10-19 for this record to be checked against the codebase, the current
releases of its dependencies, and the project's other records, with every assertion proved. This
amendment records what that check found. It changes no decision except D13's wording, and it
corrects some premises and citations. Where it differs from the text above, this amendment
governs, and the text above stays as written.

The method:

- **The tree:** `master` at `da95b793`, clean. Only reads, plus `go list -m all` and
  `scripts/vulncheck.sh`, which write nothing to the tree.
- **The libraries:** scratch clones of `go-selfupdate-lib`, `mcplib` and `acp-go-sdk2`, read at
  their tags with `git show` and `git grep`. `git ls-remote --tags` supplied the tag commits, and
  the GitHub security-advisories API the advisories.
- **One probe:** a scratch copy of the tree. The working tree was not touched.

### The releases on 2026-10-19

| Module | Latest tag (date) | Peeled commit | After the tag | This repository |
| :--- | :--- | :--- | :--- | :--- |
| `go-selfupdate-lib` | `v1.15.2` (2026-10-12) | `436a7f5976a6815491c7384527fef90869574a1b` | `main` is 2 commits ahead (`f774c5c`, `5c0ab0d`); they change only `README.md` and `docs/` | not required yet |
| `mcplib` | `v1.6.0` (2026-09-28) | `4e1f9a53e265808bbfa740e3e3b09a51ed7f56ce` | 3 commits; they add 2 decision records about `go-llmprovider-sdk` | `v1.4.1` (`go.mod:12`) |
| `acp-go-sdk2` | `v1.15.2` (2026-10-18) | `f7defb4c596feb2b90727522028e5b7e810ce15a` | none | `v1.15.0` (`go.mod:11`), under 0184 |

### Facts confirmed

- **D1's target is still the latest release.** No `go-selfupdate-lib` release has followed
  `v1.15.2`, and its peeled commit is unchanged since the 2026-10-13 amendment.
  - Its `go.mod` requires `go 1.27.1`, `golang.org/x/mod v0.40.0`, `x/sys v0.47.0` and
    `x/term v0.43.0`.
  - This module has `go 1.27.2`, `x/mod v0.41.0`, `x/sys v0.49.0` and `x/term v0.47.0`.
  - `go1.27.2` is the newest stable Go release that `https://go.dev/dl/?mode=json` lists.
  - Neither `go-selfupdate-lib` nor `mcplib` has a published GitHub security advisory.
- **A rehome on its own still changes behavior, and still no test notices.** The probe was rerun
  on a scratch copy of `da95b793`:
  - **The swap.** The import path was changed in the 11 files that import `mcplib/selfupdate`
    (both `cmd/*/main.go` files and nine in `internal/updateclient`, four of them tests). Then
    `go get github.com/maccavelli/go-selfupdate-lib@v1.15.2` and `go mod tidy` ran.
  - **`go.mod`** loses `mcplib v1.4.1`. It gains `go-selfupdate-lib v1.15.2`, plus
    `davecgh/go-spew` and `pmezard/go-difflib` as `// indirect`. Nothing else changes.
  - **`go list -m all`** before the swap lists `mcplib v1.4.1`, `modelcontextprotocol/go-sdk
    v1.6.1`, `invopop/jsonschema v0.14.0`, `golang.org/x/oauth2 v0.36.0` and `go.yaml.in/yaml/v4
    v4.0.0-rc.2`. After it, it lists none of the five.
  - **The gates all exit 0:**
    - `go build ./...`;
    - `go vet ./...` for darwin/arm64, windows/amd64, and linux/amd64 with `CGO_ENABLED=0`;
    - `go test -count=1` of `internal/updateclient`, `internal/cli`, `internal/cli/service` and
      `internal/relay`.
  - **The behavior change.** `internal/updateclient/lifecycle.go` still has no `Enabled` method.
    `v1.15.2` starts a stopped service only through `EnabledLifecycle`
    (`selfupdate/managed.go:135-145`). `mcplib v1.4.1` starts it unconditionally
    (`selfupdate/managed.go:92`). So D2 is still required.
  - **Advisories.** `govulncheck` on the copy reports nothing called and nothing imported. It
    reports one required-module finding, `GO-2026-5932` in `golang.org/x/crypto v0.58.0`, which
    has no fixed version. The untouched tree reports the same ID, and `scripts/vulncheck-allow.txt`
    allowlists it. The swap adds no finding.
- **The API, counted with a stated method.**
  - The 11 files use 31 distinct `selfupdate.` identifiers, 29 of them outside tests.
  - Top-level declarations, as `go doc -all` lists them: `mcplib v1.4.1`'s `selfupdate` exports
    77, and `v1.15.2`'s exports 170. None of the 77 is missing.
  - These figures replace the 2026-10-13 amendment's 26, 219 and 95, whose counting method that
    amendment did not record. The probe's clean compile is the stronger proof.
- **The receipt contract still matches, field for field.**
  - `service.Receipt` has the JSON fields `schema_version`, `verdict`, `path`, `backup`,
    `changed`, `reloaded`, `reason` and `warnings`.
  - Its verdicts are `none`, `unchanged`, `refreshed` and `kept`.
  - `RefreshResult` has every field except `schema_version` (`internal/cli/service/refresh.go:42-49`).
    It uses the same four verdicts (`refresh.go:19-28`).
  - A receipt with no `schema_version` is read as version 1
    (`selfupdate/service/execreconciler.go:35-37`), so `refresh.go` needs no new field.
  - The receipt's `warnings` become `ReconcileResult.Warnings` (`execreconciler.go:131`).
  - Every backend D3 names has a constructor, `New(Options) (…, error)`, in `systemd`,
    `launchd` and `taskscheduler`.
- **The library names this repository in fewer places than the draft says.** At `v1.15.2`,
  `selfupdate/service/execreconciler.go` no longer says "magic-cli-remote's refresh result". The
  migration guide still names this repository's `codesignTransformer` (`:581`) and its
  `v0.16.0`-only `bridge-release` (`:136`).
- **D11's request to the library has not been made.** At `go-selfupdate-lib` `main` (`5c0ab0d`),
  no file mentions an uninstall hook. `releasespec` still accepts only `before_install` and
  `after_install` (`selfupdate/releasespec/installer.go:24`, `:27`, `:84-85`).
  *(2026-10-19, PLAN S6: the request is now made, as `maccavelli/go-selfupdate-lib` issue #1.
  `v1.16.0` still has no uninstall hook.)*
- **D9's folder name is decided.** The owner answered
  [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)'s
  Q1 on 2026-10-13: the folder is `%LOCALAPPDATA%\Programs\magic-cli-remote`. D9's "the owner
  confirms the name when accepting this record" is met.
- **`acp-go-sdk2` does not touch this migration.** Its `go.mod` at `v1.15.0` and at `v1.15.2` has
  no `require` block, so it brings neither `mcplib` nor `go-selfupdate-lib` into the module graph.
  - The ACP migration has landed (`db717ed3`), and 0184 is accepted.
  - The pin is `v1.15.0`, while `v1.15.2` is tagged, and moving it is 0184's decision.
  - Its one advisory, GHSA-579q-9j9v-7w8w, affects `v0.15.0` only.

### Facts corrected

- **D13 named the wrong endpoint.** No `/health` route exists. The daemon's version is the
  `version` field of `GET /v1/hello`, which requires authorization (`internal/ws/server.go:295`,
  `:527-546`, the field at `:543`). The unauthenticated `GET /healthz` returns only
  `{"ok":true}` (`:294`, `:482-487`). This was already so at `64e3b219`, with the field at `:531`.
  - The same version also reaches ACP agents as `clientInfo.version`
    (`internal/daemon/daemon.go:327`, `internal/provider/acpagent/acpagent.go:1203`), as
    [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
    D4 expects.
  - The phone app never requests `/v1/hello`. `apps/mobile/lib` names it only in a comment
    (`lib/data/local/settings_store.dart:1271`).
  - **D13 now reads:** `/v1/hello`'s `version` and ACP's `clientInfo.version` report `buildinfo`
    as it is, so a release reports `v0.X.Y` where it reports `0.X.Y` today. The decision is
    otherwise unchanged.
- **D5's two premises do not hold today.**
  - `doctor` runs no daemon health check. It renders `service.ProbeStatus`, the service manager's
    view (`internal/cli/doctor.go:37`).
  - The daemon has two health checks: the admin socket's `ping` (`internal/admin/admin.go:37`,
    `:192`, used by `internal/cli/pair.go:36`) and `GET /healthz`.
  - The admin socket has no shutdown request. Its operations are `ping`, `disconnect_device` and
    `disconnect_devices` (`admin.go:36-40`). So under D5's own condition, "where the daemon offers
    one", `taskscheduler.Options.RequestStop` is not wired unless a shutdown operation is added.
  - Q1 and Q2 below ask the owner to decide these two points.
- **Amendment 1's premise, "this product's supplied state always answers enabled", is too
  broad.** It is corrected below; its conclusion holds.
  - **What the library treats as enabled:**
    - systemd: `UnitFileState` `enabled` or `enabled-runtime` (`systemd/lifecycle.go:50`).
    - launchd: `RunAtLoad`, or `KeepAlive` true, and the label not disabled in launchd's override
      database (`launchd/lifecycle.go:44`).
    - Task Scheduler: the task enabled, with an enabled trigger that starts it unattended
      (`taskscheduler/lifecycle.go:64`).
  - **Magic leaves a service not enabled in three ways:**
    - `setup-service --no-enable` (`internal/cli/setup_service.go:85`) skips
      `systemctl --user enable` and `launchctl enable` (`internal/cli/service/setup.go:392-397`,
      `:517-522`).
    - On macOS, that also leaves any earlier `launchctl disable` in force, such as the one
      `--remove` runs (`setup.go:697`).
    - On Windows, stopping disables the task
      (`internal/cli/service/control_schtasks.go:92-108`), and since `da95b793`, `--no-start`
      keeps a stopped task disabled (0201 D22, as amended).
  - **The conclusion.** Each of these is an operator's deliberate choice, so only a service the
    operator chose not to run goes unhealed.
- **The workflow does not enforce the pin rule.** The More Information bullet on pins says they
  move with the `go.mod` requirement "per the build workflow's own version check". The build
  workflow's check is a floor only. Its `checkLibraryFloor` requires the module to need a library
  release that can read every field the spec uses (`internal/cmd/selfupdate-release/floor.go`).
  D1's rule, that `go.mod` and both pins move together, is this record's own.
- **D8 maps the installer variables imprecisely.**
  - The shipped `install.sh` reads `MCREMOTE_VERSION`, `MCREMOTE_INSTALL_DIR`,
    `MCREMOTE_NO_SERVICE`, `MCREMOTE_SVC_WAIT_SECS` and `MC_TEST_BASE_URL` (`scripts/install.sh:30`,
    `:1024-1027`). It prints PATH advice and edits no file (`:287`).
  - The shipped `install.ps1` reads one variable, `MCREMOTE_INSTALL_NO_PATH_UPDATE`
    (`scripts/install.ps1:37`).
  - The `install.sh` template reads `<PREFIX>_VERSION`, `<PREFIX>_INSTALL_DIR` and
    `<PREFIX>_NO_HOOKS`, and only prints PATH advice (`installer/install.sh:375-380`, `:462-464`).
  - The `install.ps1` template also reads `<PREFIX>_NO_PATH_UPDATE`
    (`installer/install.ps1:475-476`).
  - So, of D8's renames, `MCREMOTE_INSTALL_NO_PATH_UPDATE` → `MCREMOTE_NO_PATH_UPDATE` is
    Windows-only. `MCREMOTE_VERSION` and `MCREMOTE_INSTALL_DIR` are new on Windows.
  - `MCREMOTE_SVC_WAIT_SECS` has no template equivalent, and D8 and D10 do not mention it (Q3).
- **"Implementation Plan: none yet" is out of date.** The PLAN,
  [0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md](0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md),
  is `in-progress`:
  - R1 was executed on 2026-10-11.
  - S1–S8 were added on 2026-10-13.
  - No S phase has started: `go.mod` does not require `go-selfupdate-lib`.
- **Citations that moved, or were wrong.** Each was read at `da95b793`.

| Cited above | Now |
| :--- | :--- |
| ci.yml build job, `ver="${GITHUB_REF_NAME#v}"` | `BASE="${GITHUB_REF_NAME#v}"` at `.github/workflows/ci.yml:198`, then `ver="$BASE"` at `:220` |
| `Makefile:173` as the release stamp | `:173` is the local `build` target. The release lane runs `build-relay` (`:222`) and `build-remote` (`:239`) with `VERSION="$ver" BUILD_KIND=release` (`ci.yml:230`) |
| `mcremote.user.service.tmpl:78` (`WantedBy`) | `:79`; `:78` is `[Install]` |
| `internal/cli/setup_service.go:85` as "the `enable` setup-service runs" | `:85` is the `--no-enable` flag. The enables are `internal/cli/service/setup.go:393` (systemd) and `:518` (launchd) |
| `internal/ws/server.go:531` (`/health`) | `GET /v1/hello`, field at `:543` (see above) |
| `ci.yml:990-1014` (bridge step) | `:990-1016` |
| `docs/guides/config.md:637-717` (update section) | `:637-664`; `:665` starts the next section |
| `apps/mobile/lib/data/update/app_update.dart:112-113` (comments citing mcplib) | `:118-120` |
| `go-selfupdate-lib@v1.13.0/selfupdate/service/execreconciler.go:32-33` | true at `v1.13.0`; removed by `v1.15.2` (see above) |

The other file and line citations above were checked and still hold:

- `go.mod:12`;
- `ci.yml:980-988`, `:1042` and `:1048`;
- `refresh.go:43-49`;
- `plist_render.go:169-170`;
- `internal/cli/root.go:46-48`;
- `cmd/mcremote/main.go:29` and `cmd/mcrelay/main.go:25`;
- in the library at `v1.15.2`: `managed.go:135-145`, `confirmer.go:38-58` and `codesign.go:151`.

### Records that now depend on this one

- [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)
  is `accepted`. It makes this record's PLAN its self-update track and holds that S1 removes
  `mcplib` entirely.
- [0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md)
  is `accepted`. Its PLAN P9 waits for S1 and S3, and carries three items:
  - **D26 (§6.6)** depends on the receipt's `warnings` reaching `Result.Warnings`, confirmed
    above.
  - **D30 (§6.10(d))** puts a recorded unit name into the backends' `Options`:
    `systemd.Options.Unit`, `launchd.Options.Label` and `taskscheduler.Options.Name`
    (`systemd/unit.go:41`, `launchd/job.go:82`, `taskscheduler/task.go:49`).
  - **The §6.1 check** confirms that an update on a host with no installed service still
    succeeds after the move.
- Since the 2026-10-13 amendment, 0201 has changed code this migration keeps: refresh's handling
  of `%t` (D27), and `setup-service --no-start` (D22, amended). Neither change affects a decision
  here. D4 keeps the refresh child, and D10 extends `setup-service` rather than replacing it.

### Questions for the owner

Each question applies to a later phase, and none blocks S1. The owner answered all three on
2026-10-19; the answers follow the questions.

- **Q1 (D5, S8): what backs each backend's `Options.Probe`?**
  - **The admin socket's `ping` (recommended).** It is local, and does not depend on the
    listener's TLS mode or port. `pair` already uses it.
  - **Or `GET /healthz`.** That needs the configured scheme and port, and it tests the
    listener.
- **Q2 (D5, S8): add a shutdown operation to the admin socket, so that
  `taskscheduler.Options.RequestStop` can let the daemon drain on Windows?**
  - **Add it (recommended under O1).** The cost is one more admin operation, on a socket only its
    owner can open.
  - **Or leave `RequestStop` unset.** Windows then keeps the no-drain stop that
    [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md)
    D9 accepts.
- **Q3 (D8 and D10, S5): what happens to `MCREMOTE_SVC_WAIT_SECS`?**
  - **Keep the name (recommended).** `setup-service`'s verified start (D10, G2) reads it, so an
    operator's existing setting keeps working.
  - **Or retire it,** and document the removal with the other installer changes.

### The owner's answers (2026-10-19)

The owner chose the recommended option for each question, and for A4, which followed from them.
The answers refine D5 and D10 as follows:

- **A1 — the probe is the admin socket's `ping`.** S8 wires an `Options.Probe` for each backend
  (`systemd`, `launchd` and `taskscheduler`). The probe sends `ping` to the daemon's admin socket,
  and passes only on a successful reply. `GET /healthz` is not used as the probe.
- **A2 — the admin socket gains a shutdown operation.** S8 adds the operation and its tests:
  - On receiving it, the daemon begins the same graceful shutdown as its shutdown signals.
    `serve` cancels its context on them (`internal/cli/serve.go:46`): `SIGINT` and `SIGTERM` on
    Unix (`signals_unix.go:12`), and `os.Interrupt` only on Windows (`signals_windows.go:16`).
  - `taskscheduler.Options.RequestStop` sends it, and `Options.StopGrace` is set shorter than
    the poll timeout, as `taskscheduler.New` requires (`taskscheduler/task.go:71-76`).
  - The socket stays owner-only (`internal/admin/owner_unix.go`, `owner_windows.go`).
  - D5's condition, "where the daemon offers one", is met by this work.
  - [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md)
    D9's no-drain stop on Windows applies only when the request fails or the grace runs out.
- **A4 — `mcrelay` uses `/healthz`, and stops without a drain.** Writing the PLAN amendment found
  that A1 and A2 can reach `mcremote` only. `mcrelay` serves no admin socket: nothing under
  `internal/relay` or `cmd/mcrelay` uses `internal/admin`, and only `mcremote`'s daemon calls
  `admin.Serve` (`internal/daemon/daemon.go:490`). `mcrelay` serves `GET /healthz`, which returns
  `{"ok":true}` (`internal/relay/server.go:114`, `:418-422`). The owner chose, on 2026-10-19:
  - `mcrelay`'s `Options.Probe` sends `GET /healthz` to the relay's own listener: the port in
    `listen.port`, using whichever transport the relay's resolved configuration serves. That is
    plaintext, or TLS from certificate files or ACME (`internal/relay/fileconfig.go:43-46`,
    `:72-77`, `:580-620`; `internal/relay/config.go:157-166`). It passes only on status 200 with
    the body `{"ok":true}`. The probe verifies TLS normally; it never skips verification.
  - `mcrelay` sets no `RequestStop`. On Windows it keeps the no-drain stop of 0116 D9.
  - `mcrelay` gains no admin socket.
- **A3 — `MCREMOTE_SVC_WAIT_SECS` is kept.** S5's verified start in `setup-service` (D10, G2)
  waits up to that many seconds. The default stays 15, as in `scripts/install.sh:30`. So the
  `after_install` hook honours an operator's existing setting, and the operator guide documents
  the variable with `setup-service`.

### Consequences of this amendment

- Good, because the record now matches the tree and the released libraries. Its one false
  endpoint, its one false premise, and the stale citations are corrected, each with its evidence.
- Good, because D1, D2 and D9 are re-confirmed with today's evidence: the target release, the
  behavior trap that requires the backends, and the owner's folder name.
- Neutral, because no decision changes except D13's wording, and the owner's answers A1–A4, which
  refine D5 and D10 within their existing phases, S8 and S5.
- Bad, because A2 adds product work to S8: a new admin operation, a new path into the daemon's
  graceful shutdown, and the tests for both.
- Bad, because the PLAN still repeats the corrected facts. Its `/health` steps and checks, the
  `app_update.dart` and `ci.yml:990-1014` citations, and the `RequestStop` step must be amended
  to match before the phases that use them start.

## Amendment — 2026-10-19 (S4): rehearsals run on demand, not on every push

Starting S4 showed that the decision's "rehearsed on every push and PR" would put the Android APK
build on every push and PR. Branch pushes and PRs have never built the APK. The owner chose to
keep it that way.

### What S4 found

- **The build workflow cannot stage without the APK.**
  - The spec's APK extra has no `path`, so the APK must come from an extras artifact.
  - At the D1 pin, `436a7f59`, the stage tool returns `extra … has no path, and no extras
    directory was given` when that artifact is absent (`internal/cmd/selfupdate-release/stage.go:262-266`).
  - The workflow's verify step then checks the staged set against every extra
    (`build-selfupdate-release.yml`, "Verify the staged release").
  - Nothing exempts a rehearsal. Off a tag, the APK is named `magic-cli-remote-rehearsal-<sha12>-arm64.apk`.
- **What the APK costs.** In run 36022338143 the APK job took 8 min 26 s, after waiting for the
  `go` job. A push run takes about 6 minutes without it and about 14 with it.
- **What it would break.** On the canonical repository the APK job fails when no signing secrets
  are present (`ci.yml:701-707`, MADR 0065 B1). GitHub gives no Actions secrets to Dependabot PRs
  (`.github/dependabot.yml`) or to fork PRs. Every such PR would turn red. The only way to avoid
  that would be to loosen the signing rule.

### The owner's decision (2026-10-19)

- **The build workflow runs only where the APK already runs:** on a tag, and on a manual
  `workflow_dispatch` (`ci.yml:635`).
- **Branch pushes and PRs keep their CI unchanged.** They run the test, lint and mobile jobs and
  build no release set.
- **A dispatched rehearsal is required before every tag.**
  - Before tagging, run `workflow_dispatch` on the commit to be tagged.
  - That run is the rehearsal this record asks for. It builds, checks, identity-runs and stages
    the whole set, the APK included, stamped `rehearsal-<sha12> (local)`, and publishes nothing.

### What this changes in the record

These statements are superseded. They stay as written above.

- **"Rehearsals run on every push and PR"** (the release-pipeline row of "What changes for this
  repository's contracts"). Read it as "rehearsals run on a dispatch, and one is required before
  every tag".
- **"the release recipe is rehearsed on every push and PR instead of being tried first on the
  tag"** (Decision Outcome, Consequences). The recipe is still rehearsed before the tag, but on
  demand rather than on every change.
- **"a rehearsal on a branch push produces …"** (Confirmation). Read it as "a dispatched
  rehearsal produces …". The rest of that bullet stands.

### The frozen fixture, and the `v0.16.0` bridge that is no longer published

S4's verification found that two statements of this record are false today:

- "the pre-0.16 published bridge is untouched — the frozen fixture keeps proving it"
  (Decision Outcome, Consequences);
- "a publish produces an immutable, attested release whose asset set the frozen v0.15.3 fixture
  still resolves" (Confirmation). S8 step 3 of the PLAN repeats it.

**The evidence, 2026-10-19:**

- **What the frozen selector matches.** In `internal/updateclient/legacy_test.go` it matches only
  `<product>-<os>-<arch>-<ver>` compatibility names. Only the `v0.16.0` bridge carried them.
- **Run on S4's rehearsed set,** it returns `no asset matching <product>-<os>-<arch>-*` for all
  eight product and platform pairs. The same holds for every published release, all of which
  carry canonical names only. `v0.20.1`'s assets are the eight canonical binaries, `SHA256SUMS`,
  `install.sh`, `install.ps1` and the APK.
- **The bridge release is gone.**
  - The `v0.16.0` tag exists, and its CI run 33701454306 succeeded on 2026-09-03.
  - `GET /repos/maccavelli/magic-cli-remote/releases/tags/v0.16.0` returns 404.
  - `gh release list` shows nine releases, `v0.17.1` to `v0.20.1`. `v0.17.0`'s run also succeeded,
    and it has no release either.
  - When and why the earlier releases were removed is not recorded here.

**The owner's decision (2026-10-19): correct the record, and keep the fixture.**

- **No published release serves an installed pre-0.16 client.** Its selector finds no asset in
  any release, so it cannot update itself. It must be reinstalled with the installer, which
  resolves canonical names.
- **The frozen fixture stays, as D7 decided, with its role restated.** It records the `v0.16.0`
  file set and how a pre-0.16 client read it. It is not evidence about any release that is
  published now or later.
- **The confirmation becomes:**
  - the set a release run stages, rehearsed or tagged, equals the last published release's asset
    set, apart from the tag in the APK's name;
  - this program's selector, built from the embedded spec, resolves every product and platform
    in it.

  S4 has shown both for its rehearsal (the PLAN's S4 record). S8 checks them for the first tag.

### Consequences of this amendment

- Good, because push and PR CI keep their cost and their result. No Dependabot or fork PR turns
  red, and the APK signing rule stays as it is.
- Good, because the whole release recipe still runs before the tag: the dispatched rehearsal runs
  the same jobs as the tag.
- Neutral, because the library's workflow can rehearse on every change. This repository uses it
  more narrowly than it allows.
- Bad, because a change that breaks the release recipe is caught only when someone dispatches a
  rehearsal, not on the push that broke it. The required rehearsal before each tag bounds the
  damage to "found before the tag".
- Bad, because the APK job must name its file the way the library names the `{tag}` extra:
  - the tag on a tag;
  - otherwise `rehearsal-` plus the commit's first 12 hex digits (`plan.go:95-100` at the pin).

  That rule is now in two places. A mismatch is loud: the stage step refuses an extras directory
  holding a file the spec does not list, and the verify step refuses a set missing one.

## Amendment — 2026-10-19 (S5): the supervisors, the relay, updates and the Windows folder

Reading S5 against the code before starting it found four things the record did not settle. The
owner decided each on 2026-10-19, choosing the recommended option every time.

### What S5's reading found

- **The record contradicts itself on the relay.**
  - Amendment 4 retires the installer's `--with-relay-service`. In its place you run
    `mcrelay setup-service` after the install (the "Installers" bullet of "The shape, per surface",
    and amendment 4 under "The amendments, stated").
  - D10, written later (2026-10-13), gives `--with-relay-service` an environment-variable
    equivalent read by `setup-service`. PLAN S5 step 3 names it `MCREMOTE_WITH_RELAY_SERVICE`.
- **The shell installer's other supervisors are thinner than its systemd path.**
  - For runit and s6, `install.sh` writes a run script that is a bare
    `exec "<dir>/mcremote" serve 2>&1`. For OpenRC it writes an init script with
    `command_args="serve"` (`scripts/install.sh:776-831`).
  - Each covers `mcremote` only. None passes `--config`. None bakes the environment the systemd
    unit sets.
  - None is checked for a start. `svc_confirm` runs only on the systemd and launchd paths
    (`:686`, `:704`, `:741`, `:759`).
- **`update` has never restarted those supervisors.**
  - On Linux, `NewBackend` always returns the systemd backend (`internal/cli/service/backend.go:36-41`).
  - On a runit, s6 or OpenRC host, a managed update finds no systemd unit and installs the binary
    as a standalone update.
  - The supervisor keeps the old process running, on the replaced file. That was already true
    under `mcplib`. Only `install.sh` stopped and restarted those services (`svc_note_active`,
    `:597-609`).
- **D9's migration would point the task at a missing binary.**
  - Today each product has its own folder: `%LOCALAPPDATA%\Programs\<product>\<product>.exe`
    (`scripts/install.ps1:324-326`; `internal/cli/service/setup.go:1364-1371`).
  - Each of those folders has its own User `PATH` entry (`install.ps1:357-369`).
  - The library's template installs flat: `%LOCALAPPDATA%\Programs\<installer.name>\<product>.exe`
    (template `install.ps1:468`, `:489`).
  - A managed update replaces the binary where it already is, so after an update it is still in
    the old folder.
  - D9 says only that the refresh "re-registers the task at the new path, and moves the user
    `PATH` entry". The binaries are never moved, and there are two entries, not one.

### The owner's decisions (2026-10-19)

- **S5-1: D10 holds for the relay.**
  - With `MCREMOTE_WITH_RELAY_SERVICE=1`, `mcremote setup-service` also sets up `mcrelay`, from
    the `mcrelay` binary in the same folder as its own `--binary`, and checks that it starts.
  - `mcrelay setup-service` keeps working on its own.
  - Amendment 4 still retires the installer's flag. The variable replaces the flag, and running
    `mcrelay setup-service` yourself is the other way to opt in.
- **S5-2: the runit, s6 and OpenRC definitions match the systemd unit.**
  - They run the same `serve` command, with the same arguments and the same environment the
    unit bakes in, for both products.
  - They live in the user directories `install.sh` already uses: `$XDG_DATA_HOME/runit/service`,
    `$XDG_DATA_HOME/s6/service` and `$XDG_CONFIG_HOME/rc/init.d` (`install.sh:299-303`). Each is
    named by the recorded unit name.
  - They follow the unit's rules for `--force`, `--refresh` and `--remove`.
- **S5-3: `update` restarts them.**
  - Each of the three supervisors gets a product backend that implements `selfupdate.Lifecycle`
    and `selfupdate.EnabledLifecycle`, built on the same control code `setup-service` uses.
  - `setup-service` records which backend it set up. `NewBackend` and `doctor` read that record,
    so a managed update stops, starts and health-checks those services, and rolls back on them,
    as it does on systemd.
  - This extends D5 ("the backends own health") to backends this product writes, because the
    library has none for these supervisors.
- **S5-4: D9's migration copies, switches, then cleans up.** When `setup-service --refresh` on
  Windows finds the task running a binary from `%LOCALAPPDATA%\Programs\<product>\`:
  - It copies each product's `.exe` that is present into `%LOCALAPPDATA%\Programs\magic-cli-remote\`.
  - It checks that each copy runs and reports the same identity as its source.
  - It re-registers the task at the new path.
  - It replaces both per-product User `PATH` entries with the one new folder, keeping the value's
    registry type.
  - It deletes the old copies when nothing runs from them. A copy in use is deleted by a later
    refresh.
  - A task that already runs from the new folder means no move, only that cleanup.
  - If a copy fails its check, nothing is switched.

### Proposed with the PLAN, for the owner's review

These settle the points D10 leaves open. The S5 amendment of the PLAN implements them, and
approving the PLAN accepts them.

- **A host where `setup-service` cannot supervise exits 0, with a hint.**
  - This covers three cases: systemd present but its user manager unreachable (the
    "broken-systemd fallback"); a system-only OpenRC; and no supported supervisor.
  - Nothing is written. The advice `install.sh` prints is ported: its `advisories`, `:854-893`.
  - This meets amendment 4, and matches `install.sh`, which never falls through from a broken
    systemd to another supervisor (`detect_init`, `:148-158`).
  - **System OpenRC is narrowed from O2's "OpenRC (user and system)".** A system service needs
    root to write `/etc/init.d`, and `setup-service` runs as the user. So the hint gives the root
    steps rather than performing them.
- **`setup-service --uninstall` removes both products' definitions,** for every backend it
  supports.
  - It removes only definitions whose program sits in the same folder as the binary running it,
    which is `install.sh`'s ownership rule (`svc_targets_install_dir`, `:387-392`).
  - So D11's two documented commands leave no definition pointing at a binary the installer then
    deletes. Definitions that run another folder's binary are reported and left alone.

### Consequences of this amendment

- Good, because hosts on runit, s6 and OpenRC keep supervision with the same configuration as
  systemd hosts, and `update` keeps them current. Neither was true before.
- Good, because the Windows migration can no longer leave the task without a binary. The binaries
  move before the task does, and nothing switches unless the copies are checked.
- Neutral, because the relay has two ways to opt in, the variable and running
  `mcrelay setup-service`. Both use the same code.
- Bad, because S5 grows. It now also covers three product backends for `update`, a backend
  record, and the binary copy in the migration. The PLAN splits S5 into sub-phases, each committed
  on its own.
- Bad, because nothing in CI or on the test hosts exercises runit, s6 or OpenRC today. The live
  checks need throwaway containers on this Mac's Podman machine. If a supervisor cannot be run
  there, its live check is recorded as not done.

## Amendment — 2026-10-19 (S7): the installer's hook must upgrade, not only install

~~**Status of this amendment: proposed.** The record stays `accepted`. The decisions below wait for
the owner, and PLAN S7 does not start until they are decided.~~
**Decided by the owner on 2026-10-19: H1, M1 and L1, the recommended option each time.** The record
stays `accepted`.

Reading S7 against the code before starting it found that its one hook cannot do what the shipped
installer does on a host that already has the product.

### What S7's reading found

- **S7 step 1's hook is a first-install command.** The PLAN gives the generated installer one hook,
  `{"when": "after_install", "product": "mcremote", "args": ["setup-service"]}`. The arguments are
  fixed in the spec, so every run of the installer runs that same command.
- **The shipped installer branches on an existing definition** (`scripts/install.sh:652-719`, and
  `:721-772` for launchd):
  - **A definition that runs another folder's binary** is left alone. The installer reports it
    and still exits 0 (`:666-670`).
  - **A definition this folder owns** is refreshed with `setup-service --refresh`, which keeps
    every option baked into it (`svc_refresh_units`, `:639-650`). Then the services that were
    running before the install are restarted and checked (`:672-697`).
  - **Only `--force-service`** re-runs a full `setup-service --force` (`:672`).
  - **Only a host with no definition** gets a first-install `setup-service` (`:699-718`).
- **Plain `setup-service` refuses a definition that differs, on every backend:** systemd
  (`internal/cli/service/setup.go:462`), launchd (`:545`), the Task Scheduler
  (`setup_schtasks.go:43`), and runit, s6 and OpenRC (`supervisor.go:375`). Any option baked in at
  setup time makes the unit differ from a default render, and so does any template change in a new
  release. So, under S7 as written, re-running the installer on an existing host would:
  - install the new binaries;
  - fail the hook with "pass --force to overwrite", and exit 3;
  - leave the daemon running the old binary.

  `--force`, through `MCREMOTE_FORCE_SERVICE=1`, would instead reset every baked option.
- **On Windows the template installs where the old task does not run.** It installs to
  `%LOCALAPPDATA%\Programs\magic-cli-remote\` (template `install.ps1:468`). A host installed by
  the shipped `install.ps1` has a task that runs `…\Programs\<product>\<product>.exe`.
  - Plain `setup-service` refuses that task as different, as above.
  - `setup-service --refresh` takes D9's migration path. Its copy step copies each old binary into
    the new folder, overwriting what is there (`migrate.go:163-177`). Run straight after the
    template, it would replace the release just installed with the old binaries, check the copies
    against those old sources, and point the task at them. The result is a silent downgrade.
- **Neither installer stops the service first.** The shipped one stops the running services before
  the swap and restarts them after (`svc_note_active`, `svc_stop_if_running`, `:597-615`). The
  template has no step like that.
  - On Unix, a rename leaves the running process on the old file.
  - On Windows, the template renames the running `.exe` to `.prev` (template `install.ps1:566-572`).

  Either way, the new binary runs only once the hook restarts the service.
- **The library releases do not change this.** The installer templates are byte-identical at
  `v1.15.2` and `v1.16.0` (`git diff --stat v1.15.2 v1.16.0 --
  internal/cmd/selfupdate-release/installer/` is empty), so S7 can render at the D1 pin.

### Options for the hook

- **H1 — a `setup-service` mode for the installer (recommended).** One argument, proposed as
  `--from-installer`, makes `setup-service` do what the shipped installer's branches do, for the
  product it runs as:
  - **No definition:** a first-install setup. For `mcremote` this includes
    `MCREMOTE_WITH_RELAY_SERVICE`. For `mcrelay` it does nothing, because the relay's first
    service comes from that variable or from `mcrelay setup-service`.
  - **A definition this binary's folder owns:** a refresh, which keeps the baked options. The
    service is then cycled by `update`'s rule (S5f): a running service is restarted, and a stopped
    one is started only when it is enabled. Each start is confirmed by the verified start (S5c).
  - **A definition that runs another folder's binary:** it is reported, left alone, and the
    command exits 0. This is the shipped installer's "foreign" case. On Windows, the per-product
    folders of D9 are the migration case below, not foreign.
  - **`MCREMOTE_FORCE_SERVICE=1`:** a full `setup-service --force`, as `--force-service` was.
  - **The spec has two hooks,** one per product:
    - `{"when": "after_install", "product": "mcremote", "args": ["setup-service", "--from-installer"]}`;
    - the same for `mcrelay`.

    So an install of only one product (`--product`) still cycles that product's service.
  - **No `before_install` hook.** The restart after the swap is what puts the new binary in
    service, as above.
- **H2 — two hooks, `setup-service --refresh` then `setup-service`.** The second still refuses a
  definition the refresh kept with its baked options, and a refresh restarts nothing. So H2 fails
  in the same cases S7 does.
- **H3 — make plain `setup-service` upgrade a definition it owns, instead of refusing it.** This
  changes a command operators run by hand. Today a definition that differs is an error unless they
  pass `--force`. This option would silently refresh it instead.

### Also proposed: the migration keeps a newer binary

- **M1 — the migration copies a binary only when the new folder does not have it (recommended).**
  `copyToNewFolder` skips a product whose `.exe` is already in
  `%LOCALAPPDATA%\Programs\magic-cli-remote\`. That copy is the one the template just installed,
  or one an earlier move made.
  - The task is still switched, and the `PATH` and cleanup steps still run.
  - With H1, an old-folder host upgraded by the generated `install.ps1` ends on the new release,
    in the new folder, with the old folders removed.
  - It also removes the S5h finding: a re-run of the old `install.ps1` can no longer be undone by a
    later refresh, because that installer is deleted.
- **M2 — or compare versions,** and copy only when the old folder's binary is newer. This needs a
  version order inside the migration. M1 needs none, because a binary in the new folder can only
  have been put there by the template or by an earlier move.

### Questions for the live installs

S7 step 3 reruns the live installs of
[0098-MADR-ephemeral-cloud-install-verification.md](0098-MADR-ephemeral-cloud-install-verification.md).
That record's fleet is paid AWS and DigitalOcean hosts.

- **L1 — the hosts S5 used (recommended):**
  - throwaway Podman containers on this Mac: Void for runit, Alpine with s6, and Alpine booted
    with OpenRC;
  - the Linux test host, under a scratch unit name and a scratch `XDG_STATE_HOME`;
  - macOS on this Mac;
  - the Windows test host, including one upgrade of an install from the old folders.

  This costs nothing. It does not cover the rows only 0098's fleet reaches: RHEL with SELinux
  enforcing, arm64 on cloud hardware, and WSL.
- **L2 — L1, plus the 0098 fleet for the rows L1 cannot reach.** The spend is bounded by 0098's
  own ceiling and teardown, and the owner approves it before launch.

### Consequences of this amendment

- Good, because re-running the installer stays a safe upgrade: options baked into the service
  survive, and a definition from another install is not taken over. That is the shipped
  installer's behaviour, now in Go with tests.
- Good, because an old Windows install upgraded by the generated installer is moved, not
  downgraded.
- Neutral, because `setup-service` gains one more mode. It is built from parts S5 already ships:
  the refresh, the ownership rule of `--uninstall`, `update`'s cycling rule, and the verified
  start.
- Bad, because S7 grows by a Go sub-phase before its release-lane work. The PLAN splits S7 into
  sub-phases, each committed on its own.

### H1 and M1 as built (PLAN S7a, Deviations 1 and 2, accepted 2026-10-19)

- **H1:**
  - **A definition another folder's binary runs is left, even with `MCREMOTE_FORCE_SERVICE=1`.**
    That is install.sh's order (`scripts/install.sh:666`, before `:672`).
  - **On Windows, a task still in install.ps1's per-product folder is moved, even with that
    variable.** A plain `setup --force` would leave both old folders, and their older binaries,
    on the User `PATH`.
- **M1:**
  - **A binary kept in the new folder must report its identity,** or the move is not made.
  - **A copy that fails its check is removed,** so a later move cannot keep it.

## Amendment — 2026-10-19 (S7d): the shipped installer's advisories

~~**Status of this amendment: proposed.**~~ **Decided by the owner on 2026-10-19:** port the
advisories rather than drop them, and then "Approve; document A3", which approves A1, A2 and A3
as proposed. The record stays `accepted`.

### What S7d's reading found

Three behaviours of the shipped `install.sh` are not in the generated installers, and the
report's G1–G8 table does not list them:

- **The AppArmor notice.** It runs when `/proc/sys/kernel/apparmor_restrict_unprivileged_userns`
  is `1` (`scripts/install.sh:895-902`). `mcremote` itself is unaffected, but the agent CLIs'
  bubblewrap sandbox fails.
- **The macOS Full Disk Access note** (`:904-913`). It says how to grant the access and that an
  unsigned upgrade drops the grant.
- **Clearing `com.apple.quarantine` on macOS** (`clear_quarantine`, `:273-281`). It was run from
  the shell, with `xattr -d`, on each installed binary.

The shipped script printed the first two on every install, whatever the service result
(`advisories`, called at `:1113`).

### Proposed: port the two notices; document the quarantine

- **A1 — The AppArmor notice moves into `setup-service --from-installer`, for `mcremote`.**
  - It is printed on Linux whenever that file reads `1`, after whatever the hook did.
  - The text is the shipped one. The remedy names
    [0048-MADR-codex-sandbox-namespace.md](0048-MADR-codex-sandbox-namespace.md) and the Linux guide, because
    an installer run through `curl` has no `scripts/` folder to run `bwrap-apparmor-fix.sh` from.
- **A2 — The Full Disk Access note moves into `setup-service --from-installer`, for `mcremote`.**
  - It is printed on macOS on every run.
  - It names the binary in the hook's own folder and the LaunchAgent's label.
- **A3 — The quarantine clearing is not ported. It is documented instead.**
  - **Why.** The generated `install.sh` runs the installed `mcremote` before any hook: its identity
    check runs `mcremote version`. macOS Gatekeeper blocks a quarantined, unsigned binary when it
    is run. So a binary that still had the attribute would fail the identity check, and the hook
    that could clear it would never run.
  - **What setup-service could still do.** Only remove the attribute from a binary that is already
    running, where it no longer matters.
  - **Why it rarely arises.** `curl`, which the documented install uses, does not set the attribute,
    as `install.sh:273` notes.
  - **What is documented.** The macOS guide gives `xattr -d com.apple.quarantine` for a binary
    fetched by a browser.
  - **The alternative.** Port it anyway, knowing it can act only on a binary that is already
    running.
- **Only `mcremote`'s hook prints them.** The installer runs both products' hooks, so printing in
  `mcrelay`'s too would show each notice twice. Both notices are about `mcremote`: the agent CLIs
  are its children, and the grant is its own.

### Consequences of this amendment

- Good, because an Ubuntu 24.04+ host and a Mac still get the warnings the shipped installer gave
  them, on every install.
- Neutral, because `setup-service --from-installer` prints two notices that plain `setup-service`
  does not. The notices belong to an install, not to a setup by hand.
- Bad, because the quarantine clearing is lost. On the documented `curl` path it removed nothing,
  and the documented `xattr` command covers a browser download.

## Amendment — 2026-10-20 (S8): where the probe looks, and how long a notify start may take

**Decided by the owner on 2026-10-20,** when S8 began: "From the definition", "TimeoutStartSec=240"
and "Four sub-phases", each the recommended option. The record stays `accepted`. This refines A1,
A4 and D5; it reverses nothing.

### What S8's reading found

- **The admin socket's path depends on the data directory.**
  - The socket is `<runtime dir>/<instance key>/admin.sock`, where the instance key is a hash of
    the data directory (`internal/appdirs/paths.go:94-100`).
  - A service can carry its own `--config` and `--data-dir`, and `mcrelay`'s its own
    `--listen-port`. Setup bakes them into the definition, and the refresh reads them back from all
    three definition formats (`internal/cli/service/refresh.go:461-464`, `:731-733`,
    `refresh_schtasks.go:235-237`).
  - `mcremote update` loads no configuration of its own. A probe that resolved the socket as `pair`
    does (`internal/cli/pair.go:36`), from the default configuration, would ping a socket no daemon
    serves on such a host. Health would never pass, and every update would roll back.
  - `pair`'s own ping has the same blind spot, but a missed ping there costs nothing.
- **A first start can wait three minutes for Let's Encrypt before it listens.**
  - The daemon obtains its certificate before it opens the listener, and the first issuance is
    bounded by `ACMEStartupTimeout`, 3 minutes (`internal/certs/acme.go:30`).
  - Under `Type=notify`, `systemctl start` returns only on `READY=1`. The unit template sets no
    `TimeoutStartSec` (`internal/cli/service/mcremote.user.service.tmpl:21-32`), so systemd's
    default, 90 seconds, applies.
  - A slow DNS challenge would therefore fail the start and loop on `Restart=always`, where today's
    `Type=simple` cannot.

### The owner's decisions (2026-10-20)

- **S1 — The probe finds its target from the installed definition.**
  - Each probe reads the product's installed definition, with the refresh's own parser. It loads
    the configuration that definition names, with its baked `--data-dir`, and for `mcrelay` its
    baked `--listen-port`.
  - `mcremote`'s probe pings that configuration's admin socket (A1). `mcrelay`'s requests that
    configuration's `/healthz` (A4).
  - A definition that names none of these gets the defaults, which are also what the service runs
    with.
  - The alternative, the default configuration as `pair` resolves it, is not taken: it fails every
    update of a service with a baked `--config` or `--data-dir`.
- **S2 — `Type=notify` gets `TimeoutStartSec=240`.**
  - The daemon sends `READY=1` once its listener is open, and `STOPPING=1` when shutdown begins.
  - The template gains `TimeoutStartSec=240`, longer than the 3-minute issuance bound with time for
    the rest of start-up.
  - The alternatives, not taken: `EXTEND_TIMEOUT_USEC` heartbeats from the ACME wait, which
    thread systemd into `internal/certs`; and `READY=1` when the admin socket opens, before TLS
    and the listener, which would no longer mean the daemon serves phones.
- **S3 — S8 runs as four sub-phases,** each committed on its own: the probes and the shutdown
  request; `Type=notify`; the pointer amendments; and the first tag, on the owner's explicit ask.

### Consequences of this amendment

- Good, because a service run with its own configuration or data directory still passes health,
  and its updates still complete.
- Good, because a first start that waits on Let's Encrypt is not killed by systemd.
- Neutral, because each probe reads the definition and the configuration it names. Both are small
  local files.
- Bad, because `systemctl start` now returns only at `READY=1`. A start that hangs before it is
  reported after up to four minutes, and `setup-service` and `update` wait that long; a daemon that
  exits before `READY=1` still fails the start at once.

## Amendment — 2026-10-20 (S9): `v1.17.1` and the `before_uninstall` hook

~~**Status of this amendment: proposed.**~~ **Decided by the owner on 2026-10-20:** "U1
product-scoped" for the hook, and "Approve; start S9a", which approves S9-1 to S9-5 as proposed.
The record stays `accepted`. The amendment changes no decision above: D11 said this product
"adopts the hook when it ships", and the hook has shipped.

### What changed in the library

- **D11's request was met.** `maccavelli/go-selfupdate-lib` issue 1, "releasespec: add a
  `before_uninstall` installer hook", was closed as completed on 2026-10-20. The closing comment
  names `v1.17.0`.
- **The releases since this record's pin, `v1.15.2`** (the library's `CHANGELOG.md` and migration
  guide §16–§17, read at `v1.17.1`):
  - **`v1.16.0`** adds API and changes no message.
    - Of what this product uses: the Task Scheduler backend loses a data race when two tasks are
      driven at once.
    - On Windows, the cleanup receipt's retries take the session's `LockTimeout`.
    - `fmt` and `slog` print a `Credential` as `<redacted>`.
    - Release signing is new and opt-in (`sign` on the publish workflow).
  - **`v1.17.0`** adds `releasespec.HookBeforeUninstall`:
    - The hook runs the installed copy of each selected product before `--uninstall` /
      `-Uninstall` removes it.
    - A failed hook removes nothing, and the installer exits 1. A new `--force` / `-Force` removes
      anyway and exits 3.
    - `--uninstall --dry-run` lists the hooks it would run.
    - The build workflow refuses a spec with the hook in a module that requires less than
      `v1.17.0`.
  - **`v1.17.1`** changes no API, but fixes two things this product depends on:
    - `install.ps1`'s uninstall reported a program it could not remove, such as a running
      `mcremote.exe`, as removed, and exited 0. Both installers now say so, remove the rest and
      exit 3.
    - A managed install's session now serializes its methods. `update` uses that session.
- **The workflows' contract holds.**
  - At `v1.17.1` the build workflow has the same five inputs and seven outputs as at `v1.15.2`.
  - The publish workflow adds only optional inputs, `sign` and `note-verifier-key`, and an
    optional secret. So the five inputs this repository passes are unchanged.
- **The pin.** `git ls-remote https://github.com/maccavelli/go-selfupdate-lib 'refs/tags/v1.17.1^{}'`
  gives `e8d4904a09194d99761042a8996983e8d5b957eb` (2026-10-20).

### What the hook has to do here

- **`setup-service --uninstall` removes both products' definitions.** It removes every definition
  of `mcremote` and of `mcrelay` that runs a program from the folder of the binary running it
  (`internal/cli/service/uninstall.go`, `Uninstall`). Both products' `setup-service --uninstall`
  call it.
- **The installer runs one hook per selected product.** By default it selects both. With
  `--product NAME` it selects one.
- **`v0.21.0`, the one release whose binaries have `setup-service --uninstall`,** refuses that
  flag with `--from-installer`. Earlier releases have no `--uninstall` at all.
  - The hook runs the installed copy, so any new argument fails on such an install.
  - The installer then exits 1 and removes nothing, and the operator uses `--force`, or the two
    commands D11 documents.

### Options for the hook

- **U1 — Both products, a product-scoped uninstall (recommended).**
  - `setup-service --uninstall --from-installer` removes only the running product's own
    definitions in its folder, and reports the rest as plain `--uninstall` does.
  - The spec has one `before_uninstall` hook per product, with those arguments.
  - Good, because `--product mcrelay --uninstall` removes only the relay's service, and a default
    uninstall removes both.
  - Bad, because a `v0.21.0` install, or an older one, cannot run it. Its uninstall fails safe,
    with exit 1 and nothing removed, and needs the two commands once.
- **U2 — Both products, plain `setup-service --uninstall`.**
  - Good, because `v0.21.0` installs uninstall in one command, and no code changes.
  - Bad, because `--product mcrelay --uninstall` also removes `mcremote`'s service, while the
    `mcremote` binary stays.
- **U3 — `mcremote` only, plain `setup-service --uninstall`.**
  - Good, because a default uninstall removes both services in one hook.
  - Bad, because `--product mcrelay --uninstall` runs no hook, and leaves the relay's service
    pointing at the binary it deletes. That is the gap D11 exists to close.

### Proposed decisions

- **S9-1 — Move to `v1.17.1`,** `go.mod` and both workflow pins together, as D1 requires. Not
  `v1.17.0`: without `v1.17.1`'s fix, an uninstall on Windows with the daemon running would say it
  removed `mcremote.exe` and exit 0.
- **S9-2 — The hook is U1** (the owner's choice).
- **S9-3 — The uninstall becomes one command.**
  - The documentation says so, with `--force` and the uninstall exit codes: 1 when a hook failed
    and nothing was removed, and 3 when forced or only partly removed.
  - It also says that an install of `v0.21.0` or earlier uninstalls with the two commands.
  - `setup-service --uninstall`'s closing line, which names the installer's `--uninstall` as the
    next step, is reworded for both paths.
- **S9-4 — Release signing is not adopted here.** It is opt-in and needs a key, a verifier in
  `Config.ManifestVerifiers` and a first signed tag. That is a decision of its own.
  - Decided 2026-10-20 in
    [0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md](0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md):
    no signing; `v*` tags are limited to the owner by a tag ruleset.
- **S9-5 — The release that ships this is the second S8 release.** `v0.21.0` → that release is the
  first update run by a binary with S8a's code. So it is when S8a Deviation 1's Windows check and
  the live probe checks are run.

### Consequences of this amendment

- Good, because an uninstall removes the services and then the binaries, in one command, on every
  OS.
- Good, because on Windows the hook stops the task before the installer deletes its binary, which
  a running daemon would otherwise hold.
- Good, because the Task Scheduler race and the session serialization fixes arrive with it.
- Neutral, because the publish workflow's new inputs are optional and left unset.
- Bad, under U1, because an install made by `v0.21.0` or earlier needs the two commands once, or
  `--force`.

## Amendment — 2026-10-20 (S10, S11): the Task Scheduler refresh, and the macOS run

~~**Status of this amendment: proposed.**~~ **Decided by the owner on 2026-10-20:** "Approve S10a"
for S10-1 and S10-2, and "M1 real, backed up" for S11, each the recommended option. The record stays
`accepted`. S10 corrects a defect PLAN S9d found in this product's own code; it reverses no decision
above. S11 reverses S7c Deviation 3 ("Skip macOS").

### What S9d's warning comes from

PLAN S9d's Windows update printed "Task Scheduler\mcremote was rewritten and the service manager
was not reloaded; the next start may run the previous definition", though the task then ran
`v0.22.0`. Read on 2026-10-20, at `v1.17.1` and this repository's `HEAD`, `8385b133`:

- **F1 — The update disables the task before it refreshes it.**
  - The managed session runs `Stop`, then `Reconcile`, then `Start` (library
    `selfupdate/managed.go:193`, `:208`, `:213`).
  - The Task Scheduler backend's `Stop` disables an enabled task and records that it did, and
    `Start` enables it again (library `selfupdate/service/taskscheduler/lifecycle.go:107`, `:267`).
    The disable keeps the watchdog trigger from relaunching the old binary mid-update.
- **F2 — So every managed update on Windows rewrites the task.**
  - The refresh always renders `Enabled` true (`internal/cli/service/schtasks.go:144`), and the
    comparison counts the task's `Enabled` (`setup_schtasks.go:179`, `:291`).
  - During an update it compares against the task `Stop` just disabled. They differ, so it backs
    the task up, re-registers it and reports `changed` (`refresh_schtasks.go:84-108`).
  - S9d shows both sides. The install hook, which runs no `Stop`, reported "service definition
    unchanged: Task Scheduler\mcremote". The update then, with the same template in `v0.21.0` and
    `v0.22.0`, reported a rewrite. `git diff v0.21.0 v0.22.0` changes no file the task is rendered
    from.
- **F3 — And every rewrite warns.**
  - `refreshSchtasks` never sets `reloaded` (`refresh_schtasks.go:27-109`). The library's exec
    reconciler adds the warning whenever a receipt is `changed` and not `reloaded` (library
    `selfupdate/service/execreconciler.go:133-138`).
  - Task Scheduler has no reload step: a re-registered definition is what its next run uses. So on
    Windows the warning is never true.
- **F4 — Inferred from the code, not yet observed through an update: an update can undo a stop.**
  - The documented way to stop the service is to disable the task, then end it, "and keep it
    stopped" (`docs/guides/ops-windows-install.md:213-217`). The backend's `Stop` leaves a
    disabled, idle task alone, and the session then starts nothing (library
    `taskscheduler/lifecycle_test.go:297`, `TestStopDisabledIdle`).
  - But the refresh re-registers the task enabled, and its watchdog trigger runs every minute
    (`schtasks.go:121-131`). So the daemon would start within a minute of an update.
  - The install hook would do the same, and then report the service as "started on the new binary
    (it was stopped and is enabled)" (`installerhook.go:263`).
  - The same mechanism was measured once already, through `setup-service --force --no-start`:
    "Disabled, then Ready/Enabled at once, then Running 75 s later" (MADR 0201 D22, amended
    2026-10-18). That fix covered `setupSchtasks` only, not the refresh.
- **F5 — launchd has the same gap in the receipt, with a different effect.**
  - `refreshLaunchd` never sets `reloaded` either (`refresh.go:300-319`).
  - Under an update, the launchd backend's `Stop` boots the job out and its `Start` bootstraps it,
    which reads the new plist. So a rewrite there is followed by the new definition, and the
    warning would be wrong too.
  - A job that is loaded but not running is not booted out by `Stop` (library
    `launchd/reconcile_unix_test.go:50-56`). There the warning would be right.
  - No plist template has changed since `v0.21.0`, so no update has hit this. It is unobserved.

### Proposed decisions (S10)

- **S10-1 — The refresh keeps the task's enabled state.** `refreshSchtasks` renders the task as
  enabled or disabled as it is registered, with `withTaskDisabled` (`setup_schtasks.go:109`), which
  `setupSchtasks` already uses for `--no-start` (`:32-38`).
  - An update's disabled task then compares unchanged. Nothing is re-registered, backed up or warned.
  - A task the operator disabled stays stopped through an update or a reinstall, as the guide
    promises.
  - A task that does need a new definition is re-registered as `Stop` left it, and `Start` enables
    it again, as now.
  - Not taken: ignoring `Enabled` in the comparison. That would still re-enable a stopped task
    whenever a template changes.
- **S10-2 — A Task Scheduler re-registration reports `reloaded: true`.** That is what the receipt
  field means for a manager with no separate reload step. The fix is in this product's receipt, not
  in the library's warning, which is correct for systemd.
- **S10-3 — launchd is not changed here.** F5 is unobserved; S11 looks for it.
- **S10-4 — It ships as `v0.22.1`,** on the owner's ask. The receipt is written by the *new* binary,
  so an update from `v0.22.0` to `v0.22.1` already shows the fix.

### The macOS run (S11)

S7c Deviation 3 skipped macOS because a scratch `HOME` does not isolate launchd: the hook would
bootstrap a job into the real login session. Since then, PLAN S9c and S9d ran on the Windows test
host's real install, backed up first and restored after, at the owner's choice. This Mac has a real
install: `com.magiccliremote.mcremote.plist`, running `~/.local/bin/mcremote` `0.20.1` (read on
2026-10-20).

- **M1 — This Mac's real install, backed up and restored, as on Windows (recommended).**
  - Good, because it is the only run on a real operator's Mac, Full Disk Access and all.
  - Bad, because it replaces the binary that holds the Full Disk Access grant. Restoring the same
    bytes should restore the grant, since the grant follows the code signature, but that is not yet
    observed.
- **M2 — A GitHub-hosted macOS runner, dispatched by hand.**
  - Good, because it is disposable and touches no operator account.
  - Bad, because it needs a new workflow, and whether a hosted runner's `gui/<uid>` domain accepts a
    bootstrap is unconfirmed. It also has no Full Disk Access to observe.
- **M3 — Keep macOS unrun.** The launchd path stays covered by tests alone, and F5 stays unobserved.

### Consequences of this amendment

- Good, because a Windows update stops warning about a reload that does not exist, and stops
  rewriting a task that has not changed.
- Good, because a task the operator stopped stays stopped through an update and a reinstall, as
  `ops-windows-install.md` already says it does.
- Neutral, because a task that does need rewriting is still backed up and restorable, as now.
- Bad, because the refresh now carries one piece of state from the registered task into what it
  renders. A test has to keep the comparison and the render agreeing on it, or the task would be
  rewritten on every update again.
