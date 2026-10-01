---
status: proposed
date: 2026-08-19
decision-makers: Project Owner
consulted: none
informed: operators running mcremote update / mcrelay update / curl|sh install.sh
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Compare `update` against the published `BASE.N` release, and recycle a product's service only when that product has a unit file

## Context and Problem Statement

After a host is installed from the published release (the curl one-liner or
a previous `update --force`), `mcremote update` and `mcrelay update` do not
behave like they are talking to that same release:

1. `update` refuses to apply a newer GitHub release unless `--force` is
   passed, printing that the installed version "looks like a dev suffix" —
   even when the running binary is the exact `BASE.N` that the previous
   release shipped, not a local `make` build.
2. `mcrelay update` on a stock one-liner host (binary installed, **no**
   mcrelay unit) has historically tried to start `mcrelay.service`, failed,
   and rolled the swap back. A host may run only mcremote, only mcrelay, or
   both; each product's `update` must look at **that** product's unit file
   and ignore the other.

This record grounds both failures in the current tree and in the live
GitHub release, and proposes the version rule and the per-product
service-cycle rule. No implementation is included.

* Implementation Plan: [0103-PLAN-update-tracks-release-build-and-active-service.md](./0103-PLAN-update-tracks-release-build-and-active-service.md)

Review corrections (2026-08-19, Project Owner):

* A crashed daemon that **has** a unit file must still be updated and
  restarted. "No service file" is the only case that is binary-only.
  That is 0100 F3 HealStart, kept, not withdrawn. The first draft of
  this record treated installed-but-down as binary-only; that draft is
  superseded in place.
* A process with no unit file is not a supported state. The installer
  must never start a daemon unless that product already has (or this
  invocation just wrote) a unit/plist. `update` does not grow an
  "orphan process" path; it just refuses to `Stop`/`Start` when
  `IsInstalled` is false.

Related:
[0065](0065-MADR-update-automation.md) (the original version rule and
`--force` gates),
[0072](0072-MADR-phone-reconnect-and-provider-timeout-incident.md) D5
(HealStart for a unit that is installed but down),
[0097](0097-MADR-linux-curl-installer.md) (one-liner installs both
binaries; mcrelay service is opt-in),
[0100](0100-MADR-update-unit-refresh-and-daemon-reload.md) F3
(`HealStart` gated on `IsInstalled`).

### How a published release is actually versioned

Three different strings exist for one ship. They are not interchangeable.

| String | Example (live `releases/latest`, 2026-08-19) | Who produces it |
|---|---|---|
| GitHub release tag | `v0.13.9` | `git tag vX.Y.Z`; CI rejects any tag that is not exactly `v[0-9]+.[0-9]+.[0-9]+` (`.github/workflows/ci.yml:152-155`) |
| Build serial / asset suffix | `0.13.9.1` | `scripts/next-build-version.sh` on the tag run; stamped into both binaries via `-X main.version=` (`Makefile:154`, `ci.yml:172-177`); asset names `mcremote-linux-amd64-0.13.9.1`, `SHA256SUMS-0.13.9.1` |
| Local `make` build | `0.13.9.1.g<shortsha>` | same allocator, **offline / no-push path**: `ver="${ver}.${uniq}"` with `uniq=g$(git rev-parse --short HEAD)` (`next-build-version.sh:201-210`) |

The comment at the top of `next-build-version.sh` still says "Release tags
remain `v0.2.1` (three-part); build tags never use the `v`-prefix." That is
true of the **git** ledger (`v0.13.9` vs `build/0.13.9.1`). It is not true
of the **binary the operator is running**. `mcremote version` after
`curl …/install.sh | sh` prints `0.13.9.1`. That is the published release,
not a developer checkout.

[0065 D2](0065-MADR-update-automation.md) defined the opposite rule:
compare only the three-part BASE, "never chase `N`", and treat any
dev-suffixed local version as a machine the updater must not replace
unless `--force`. The only suffix D2 named was `.g<hash>`. The
implementation generalised that to **any fourth numeric component**.

### F1 — `ParseBase` classifies a published `BASE.N` as a local/dev build

`internal/update/version.go:14-49`:

```go
if len(parts) > 3 {
    dev = true
}
if strings.Contains(v, ".g") || strings.Count(v, ".") > 2 {
    dev = true
}
```

`0.13.9.1` has four dotted parts and no `.g`. It is flagged `dev=true`.
`0.13.9.1.gdeadbee` is also `dev=true`. The two are not the same thing.
`TestParseBase` (`version_test.go:12-17`) never feeds a four-part
**numeric** version; the only four-part case is `0.6.7.4.gf7fe252`.
`TestRun_DevRequiresForce` (`run_test.go:65-84`) uses `0.1.0.1.gdead` —
the real local-build shape — so the published-release shape has no test
that would have failed when it was lumped in.

`update.Run` (`run.go:70-90`) then does:

1. `NewerBase(rel.Base, local)` — three-part only. `rel.Base` is
   `BaseString(tag)` (`github.go:70`), so for tag `v0.13.9` it is
   `0.13.9`. Local `0.13.9.1` parses to the same triple. Equal BASE →
   "already up to date" and return, **if the latest tag is still this
   base**.
2. If GitHub latest is a **new** tag (`v0.13.10`), `NewerBase` is true,
   then `if localDev && !opts.Force { return fmt.Errorf("local build %q
   looks like a dev suffix; pass --force …") }`.

So a host that was installed from `v0.13.9` / asset `0.13.9.1` cannot
take `v0.13.10` / asset `0.13.10.1` without `--force`. That is the
reported failure: the installed version *is* the previous published
release, and the validator still calls it a "dev suffix". `--force` was
defined as "overwrite a local compile" (0065 D2, `docs/config.md:608`);
it is now required for every ordinary hop between published releases.

The other half of D2 is still wrong even when `--force` is not required.
`NewerBase` ignores `N`. If the same GitHub tag is rebuilt and the
latest asset becomes `0.13.9.2` (CI allocates a new `build/0.13.9.2` on
every tag run — `ci.yml:167-172`), a host on `0.13.9.1` is told "already
up to date" and never downloads. The thing that identifies a ship is the
asset suffix `VER`, which `AssetFor` already extracts
(`github.go:81-105`). `Run` never compares against it.

`--check` inherits both mistakes: it reports the three-part BASE, and
on a newer BASE plus a four-part local it errors instead of exiting 10.

The curl one-liner is **not** this gate. `scripts/install.sh` does not
call `ParseBase`. It downloads `…/latest/download/mcremote-linux-$ARCH`
and records `RESOLVED_VER` from the SHA256SUMS filename
(`install.sh:172-173`). Re-running the one-liner always fetches latest.
The broken path after bootstrap is `mcremote update` / `mcrelay update`.

### F2 — the service cycle must be per-product and keyed off the unit file

Default `scripts/install.sh` always installs **both** binaries
(`PRODUCTS="mcremote mcrelay"`, line 24). It only runs
`mcrelay setup-service` when `--with-relay-service` is set (default
`WITH_RELAY_SERVICE=0`, lines 471, 704). After a stock one-liner,
`mcremote` has a user unit; `mcrelay` is a binary on `$PATH` with no
unit and no launchd plist. The inverse is a supported host too: someone
can run `mcrelay setup-service` and never supervise mcremote. Or both.

The operator rule, confirmed on review:

* **This product has a service definition** (systemd user unit or
  LaunchAgent plist — `service.IsInstalled(product)`,
  `control.go:111-145`): `update` swaps the binary, refreshes the
  definition (0100), and **starts** it. If the process was already
  running, stop first. If it was down (crash, bootout, failed start),
  start it anyway — that is 0072 D5 / 0100 F3 HealStart. A daemon with
  a unit file is supposed to be running; leaving a crashed one down
  after a binary swap is a failed update.
* **This product has no service definition**: swap the binary only. Do
  not `Stop`, do not `Start`, do not invent a unit. The installer is
  not allowed to create this situation: it never launches `serve`
  except through `setup-service` (systemd/launchd) or by writing a
  supervisor service directory first (runit/s6). `--no-service`
  installs binaries and starts nothing (`install.sh:546-548`). The
  `nohup … serve` line in `summary` (`:644`) is operator advice, not
  something the script execs.

Each command is independent. `mcremote update` probes only
`mcremote.service` / `com.magiccliremote.mcremote.plist`.
`mcrelay update` probes only `mcrelay.service` /
`com.magiccliremote.mcrelay.plist`. Running one must not inspect or
cycle the other.

What the code does today (`run.go:133-151`, `swap.go:76-94`):

* Both CLIs always pass a `Service` into `Run`, and `RestartService` is
  `opts.Service != nil` — always true. That boolean does not mean "this
  product has a unit".
* `HealStart: installed` (0100 F3) is the gate that actually prevents
  `Start` on a no-unit host. `TestRunSkipsHealStartWhenNotInstalled`
  covers it. `TestRunHealStartsWhenInstalledButDown` covers the crash
  case. **The desired service matrix is already what 0100 F3 specified.**
* The remaining defect is honesty and coupling: `RestartService` is
  still the wrong flag, so a reader (or a future call site) cannot see
  that the cycle is supposed to be `IsInstalled(this product)`. A
  pre-0100 binary on a host still does unconditional HealStart and is
  the original "mcrelay update rolled back because Unit not found"
  report ([0100-findings](0100-findings-update-refresh.md) §F3).

Confirmed matrix, **desired and (for the Start/no-Start split) current
after 0100 F3**, applied independently to whichever product the operator
invoked:

| This product's unit file | Process | What `update` does |
|---|---|---|
| Absent (stock `mcrelay` after curl\|sh; mcremote-never-supervised) | irrelevant | Swap only. No Stop, no Start, no new unit. |
| Present | running | Stop, swap, `--refresh`, start, wait-until-active. |
| Present | down (crash / bootout / failed) | Swap, `--refresh`, **Start** (HealStart), wait-until-active or rollback. |

Masked units already count as not installed (`control.go:139-140`) and
stay in the first row.

## Decision Drivers

* A binary stamped `BASE.N` with no `.g<hash>` is a published release.
  `--force` is only for overwriting a locally compiled/installed
  binary (`dev`, `debug`, or a `.g<hash>` / extra uniqueness suffix).
* `update` without `--force` must apply a newer published `BASE.N`,
  including a newer `N` on the same GitHub tag.
* Equal published `BASE.N` is "already up to date" (exit 0). `--check`
  reports that rule and never invents an update.
* A unit file for **this** product means the daemon is supposed to be
  running. Update + restart, including when it is currently down.
* No unit file for **this** product means binary-only. Do not start a
  service the operator never created, and do not roll back a good swap
  over `Unit not found`.
* An unsupervised process (`serve` with no unit) is not a supported
  host state. The installer must not produce one: it starts a daemon
  only after that product has a unit/plist, or after this invocation
  has just written one. `--no-service` installs binaries and starts
  nothing.
* The rule is the same function for mcremote and mcrelay, keyed only
  on the product named by the command. A host may run one, the other,
  or both.
* Do not change the GitHub tag scheme. CI, `install.sh` unversioned
  aliases, and `next-build-version.sh` all assume `vX.Y.Z`.
* Keep 0100's unit `--refresh`. Keep the swap/verify/rollback
  machinery.

## Considered Options

* Option 1: compare the published asset `VER` (`BASE.N`) to the running
  binary; treat only `.g` / `dev` / `debug` as local; recycle this
  product's service iff `IsInstalled(product)` (HealStart when the
  unit exists but the process is down)
* Option 2: keep 0065's three-part BASE compare, but stop classifying
  `BASE.N` as `dev`
* Option 3: retag GitHub releases as `v0.13.9.1` and compare tags
  literally
* Option 4: recycle the service only when the process is currently
  `IsActive` (binary-only when installed-but-down)

## Decision Outcome

Chosen option: "Option 1: compare the published asset `VER` (`BASE.N`)
to the running binary; treat only `.g` / `dev` / `debug` as local;
recycle this product's service iff `IsInstalled(product)`", because
that is what a release actually is (CI stamps `0.13.9.1`, not
`v0.13.9`), because `--force` then means what operators already think
it means, and because a unit file is the operator's statement that this
daemon should be running — a crash is a reason to start after the swap,
not a reason to leave it down.

This **amends 0065 D2** (three-part-only compare; "never chase `N`";
dev = any extra dotted field). It **reaffirms 0072 D5 / 0100 F3
HealStart** and requires the cycle flag to be `IsInstalled(this
product)`, not `Service != nil`. 0100's `--refresh` stays.

Option 4 was the first draft of this record's service half. It is
rejected: a crashed daemon with a unit file must come back.

### Shape of the chosen option

**Version identity**

* Parse a version into `(major, minor, patch, N, local)`.
  * `0.13.9` → N=0, local=false (legacy three-part).
  * `0.13.9.1` → N=1, local=false (**published release**).
  * `0.13.9.1.gdeadbee` / `0.13.9.1.ci<runid>` / `dev` / `debug` /
    empty → local=true.
  * A fourth component that is not a bare integer is local, even if it
    is not `.g…`.
* `NewerPublished(remoteVER, localVER)` compares major, then minor,
  then patch, then N. The remote side is the **asset suffix**
  `AssetFor` already returns (`0.13.9.1`), not `rel.Base`.
* `Run` order:
  1. `Latest` + `AssetFor` → published `VER`.
  2. If local is local-build and not `--force` → refuse (same
     message, but only for real local builds).
  3. If published `VER` is not strictly newer → "already up to date"
     (unless `--force`, which still re-seeds). `--check` stops here
     and does not honour `--force`.
  4. If `--check` and newer → `ErrUpdateAvailable` (exit 10), printing
     `local VER → published VER`, not `→ base`.
  5. Else download that asset, verify, swap.

**Service cycle (per product, idempotent)**

* `mcremote update` and `mcrelay update` call the same `update.Run`
  with `Product` set to the command's own name. The probe is
  `IsInstalled(opts.Product)` / `IsActive(opts.Product)` only. There
  is no cross-product list, no "also restart the other daemon", and no
  implicit `setup-service` that would create a unit the host did not
  already have.
* `RestartService` and `HealStart` are both `installed` for that
  product (not `opts.Service != nil`).
  * `installed == false`: swap only. Log the existing "binary installed
    at … (restart the service yourself if needed)" line
    (`swap.go:166`). `--refresh` is a no-op (`VerdictNone`, already
    tested in `TestRefreshNoUnitInstalled`).
  * `installed == true` and `IsActive`: stop (soft-fail), swap,
    `--refresh`, start, wait-until-active or rollback.
  * `installed == true` and not `IsActive`: swap, `--refresh`,
    **Start** (HealStart), wait-until-active or rollback. This is the
    crashed-daemon path.
* `--refresh` still runs when a unit exists, so a template fix lands
  before the start (0100). Failures there stay non-fatal.

### Consequences

* Good, because a host on `0.13.9.1` takes `0.13.10.1` (or `0.13.9.2`)
  with a plain `mcremote update` / `mcrelay update`, no `--force`.
* Good, because `--force` again means "replace my local compile",
  matching `docs/config.md` and 0065's original intent.
* Good, because a stock mcrelay binary install (no unit) cannot fail or
  roll back a swap over `Unit not found`.
* Good, because a crashed mcremote or mcrelay that **has** a unit is
  brought back after the swap — same rule on a mcrelay-only host, a
  mcremote-only host, or both.
* Neutral, because GitHub tags stay `vX.Y.Z`. Operators who think the
  release *tag* is `v0.13.9.1` are looking at the asset / `mcremote
  version` string; we document that pairing rather than retagging.
* Neutral, because the Start/no-Start split is already 0100 F3; this
  record makes `RestartService` match that split and forbids
  cross-product coupling.
* Bad, because starting an installed-but-unconfigured mcrelay (unit
  file present, `MCRELAY_HOSTS` missing) can still fail the wait-until-
  active check and roll back. That is inherent to "a unit file means
  it should be running". The remedy is to fix or remove the unit, not
  to skip Start.
* Bad, because every test that encoded "four dotted parts ⇒ dev" or
  "equal BASE ⇒ up to date regardless of N" must move with the version
  rule. `TestRun_DevRequiresForce` stays valid only for `.g` / `dev`.
  `TestRunHealStartsWhenInstalledButDown` and
  `TestRunSkipsHealStartWhenNotInstalled` stay as-is.

### Confirmation

* `Parse` / `NewerPublished` table: `0.13.9.1` is not local; `0.13.9.1`
  < `0.13.9.2` < `0.13.10.1`; `0.13.9` (N=0) < `0.13.9.1`;
  `0.13.9.1.gdead` is local.
* `Run` without `--force`: local `0.13.9.1` + latest asset `0.13.10.1`
  attempts the download (does not error "dev suffix").
* `Run` without `--force`: local `0.13.9.1` + latest asset `0.13.9.1`
  prints "already up to date".
* `Run` without `--force`: local `0.13.9.1` + latest asset `0.13.9.2`
  (same GitHub tag, newer N) attempts the download.
* `Run` without `--force`: local `0.13.9.1.gdead` + newer published
  VER errors with `--force`. `--check` on that pair also refuses
  rather than exiting 10 (local compile is not a "release behind").
* `--check` on a clean `0.13.9.1` vs `0.13.10.1` exits via
  `ErrUpdateAvailable` and names both full VERs.
* `Run` with `IsInstalled=false`: swap succeeds, no `Stop`, no
  `Start`. Existing `TestRunSkipsHealStartWhenNotInstalled` remains
  required. Do **not** add an "orphan process" fixture — that state is
  not supported.
* Installer invariant: `scripts/install.sh` never execs `serve` or
  `nohup` except via `setup-service` / a supervisor service directory
  written first. `--no-service` starts nothing. A grep-level check in
  `scripts/install_test.sh` (or a comment-anchored assertion next to
  `setup_service`) pins this.
* `Run` with `IsInstalled=true`, `IsActive=false`: `Start` called once
  (HealStart). Existing `TestRunHealStartsWhenInstalledButDown` remains
  required, for **both** product names as the `Product` field.
* `Run` with `IsInstalled=true`, `IsActive=true`: stop → refresh →
  start, wait-until-active still applies.
* A test or explicit assertion that `mcremote` `Run` never calls
  `IsInstalled`/`Start`/`Stop` with `"mcrelay"` and vice versa.
* Manual: on a curl\|sh host at `0.13.9.1` (mcremote unit, no mcrelay
  unit), `mcremote update` against a newer VER completes without
  `--force` and leaves the mcremote unit running; `mcrelay update`
  replaces the binary and does not create or start a unit. On a
  mcrelay-only host (mcrelay unit, no mcremote unit), the inverse.
  Kill the supervised daemon, run `update`, the unit comes back.

## Pros and Cons of the Options

### Option 1: compare published `BASE.N`; `--force` only for local builds; cycle iff this product has a unit file

* Good, because it matches the strings CI actually stamps and GitHub
  actually serves (tag `v0.13.9`, asset/binary `0.13.9.1`).
* Good, because a newer `N` on the same tag is a real ship and is no
  longer invisible.
* Good, because stock mcrelay (no unit) is binary-only, stock
  mcremote (unit) is recycled, a mcrelay-only host is the mirror
  image, and a crashed daemon with a unit is restarted. One function,
  one probe, both products.
* Neutral, because the Start/no-Start split is already 0100 F3; the
  new work on this half is making `RestartService` say `installed` and
  pinning the per-product isolation in tests.
* Bad, because an installed-but-unconfigured unit can still fail
  wait-until-active and roll back — accepted, and inherent to "the
  file means it should run".

### Option 2: keep three-part BASE compare; only fix the `dev` bit

* Good, because it is a one-predicate change in `ParseBase` and unblocks
  `0.13.9.1` → `0.13.10.1` without `--force`.
* Bad, because `0.13.9.1` → `0.13.9.2` on the same GitHub tag stays
  "already up to date". The operator-visible version is `N`, and D2's
  "never chase N" is what made the validator ungrounded.
* Bad, because it does not pin the per-product service cycle.

### Option 3: retag releases as `v0.13.9.1`

* Good, because tag, asset suffix, and `mcremote version` would finally
  be one string.
* Bad, because CI explicitly rejects non-`vX.Y.Z` tags
  (`ci.yml:152-155`), `next-build-version.sh` resolves BASE from
  three-part tags only, and `install.sh --version` / unversioned
  `…/latest/download` aliases all assume the current scheme. A retag
  is a release-pipeline rewrite, not an updater fix.
* Bad, because a local `.g` build would still need a separate rule, so
  the parser work does not disappear.

### Option 4: recycle only when `IsActive` (leave crashed units down)

* Good, because it would never `Start` a leftover unconfigured unit.
* Bad, because a crashed daemon with a unit file stays down after
  `update`, which is the opposite of the requested heal. Rejected on
  review (2026-08-19).

## More Information

* Live `GET /repos/maccavelli/magic-cli-remote/releases/latest`
  (2026-08-19): `tag_name=v0.13.9`; assets include
  `mcremote-linux-amd64-0.13.9.1`, `mcrelay-linux-amd64-0.13.9.1`,
  `SHA256SUMS-0.13.9.1`, plus the 0097 unversioned aliases
  `mcremote-linux-amd64` / `mcrelay-linux-amd64`.
* Touch-point inventory (confirmed against current code):
  * `internal/update/version.go` (`ParseBase`, `NewerBase`,
    `BaseString`), `internal/update/version_test.go`
  * `internal/update/run.go` (gates at `:70-90`, `RestartService` /
    `HealStart` at `:133-151`), `internal/update/run_test.go`
  * `internal/update/github.go` (`Release.Base`, `AssetFor` VER)
  * `internal/update/swap.go` (`wantUp`, `HealStart`, start-or-rollback)
  * `internal/cli/update.go`, `internal/relay/update.go`
  * `internal/cli/service/control.go` (`IsActive`, `IsInstalled`)
  * `scripts/next-build-version.sh`, `.github/workflows/ci.yml`
    tag-run stamping, `scripts/install.sh` (`PRODUCTS`,
    `--with-relay-service`, `--no-service`, `setup_service`,
    `summary` nohup-as-advice), `scripts/install_test.sh`
  * `docs/config.md` `--force` row, 0065 D2, 0100 F3
* Out of scope:
  * Changing how GitHub tags or `build/<BASE>.<N>` ledger tags are cut.
  * Making the one-liner enable mcrelay by default.
  * Phone "Restart to update" (0065 §2.5) — different artifact, already
    versioned as an APK.
  * Re-running `curl|sh` as an upgrade tool. 0097 already says it is
    bootstrap; this record does not reopen that.
* Review questions resolved (2026-08-19, Project Owner):
  1. Crashed daemon with a unit file: update + restart (HealStart
     stays). No unit file: binary-only.
  2. Same function for both binaries; a host may run mcrelay only,
     mcremote only, or both. No cross-product cycle.
  3. An unsupervised / orphan process is not a supported state. The
     installer must not start a daemon unless that product has a unit
     (or this invocation just created one).
* Pre-approval status: this record proposes. An implementation plan
  (`0103-PLAN-update-tracks-release-build-and-active-service.md`) is
  written and approved before any code changes begin.
