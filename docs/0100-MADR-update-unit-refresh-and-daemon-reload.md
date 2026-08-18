---
status: proposed
date: 2026-08-18
decision-makers: Project Owner (scope, severity, and release gating)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Reconcile the service definition during `update`, and reload the manager before the restart

## Context and Problem Statement

`mcremote update` and `mcrelay update` ([MADR 0065](0065-MADR-update-automation.md))
download a release, verify it against `SHA256SUMS`, swap the binary, and cycle
the service. The whole mutation surface is `SwapAndRestart`
(`internal/update/swap.go:37`), driven by a three-method interface —
`IsActive`, `Stop`, `Start` (`internal/update/service.go:5-10`) — wired to
`service.IsActive/Stop/Start` in both products
(`internal/cli/update.go:47-49`, `internal/relay/update.go:36-38`). The Linux
arms of those three are exactly `systemctl --user is-active|stop|start
<product>.service` (`internal/cli/service/control.go:39-42`, `:61`, `:104`).

**Nothing on that path writes a service definition, and nothing reloads the
service manager.** The only writer of `~/.config/systemd/user/*.service` in the
tree is `service.Setup` (`internal/cli/service/setup.go:342`), reachable only
from `setup-service` (`internal/cli/setup_service.go:104`,
`internal/relay/cli.go:467`) — and it *does* run `daemon-reload`
(`setup.go:348`). `Remove` reloads too (`setup.go:584`). `update` is the one
mutation path in the codebase that does neither.

Three defects follow. F1 and F2 are the reported regression; F3 was found while
confirming them and lives in the same twelve lines.

### F1 — a fix that lives in the template cannot be delivered by `update`

Commit `14ea974` removed `PrivateDevices=true` and `RestrictNamespaces=true`
from `internal/cli/service/mcrelay.user.service.tmpl` after
[0099](0099-MADR-installer-service-state-verification.md) F4a proved that both
directives make the unit **unstartable in user scope on every host** —
`Failed at step CAPABILITIES … status=218/CAPABILITIES`, in a permanent
`Restart=always` loop. That fix exists *only* in the template.

A host that takes that release with `mcrelay update` gets the new binary and
keeps the old unit. The daemon stays in exactly the crash loop the release was
cut to end. `install.sh` has the same hole, but deliberately and out loud: its
upgrade branch keeps the existing unit (`scripts/install.sh:417`) and then
prints `refresh the unit itself with: mcremote setup-service --force` (`:434`).
`update` prints nothing, so the operator has no way to know the release did not
land.

### F2 — the manager is never told the definition changed

`Start` calls `systemctl --user start` directly. If the unit on disk differs
from what the user manager last parsed — a drop-in added, an earlier
`install.sh --force-service`, a hand edit, or F1's own rewrite once we add it —
systemd restarts the **cached** definition and emits its
`… changed on disk … run 'systemctl --user daemon-reload'` warning. The restart
appears to succeed while running the old definition. `setup-service` (`:348`)
and `Remove` (`:584`) both reload; `update` does not.

### F3 — `update` fails, and rolls back a good binary, on hosts with no service

`HealStart: true` is passed unconditionally (`internal/update/run.go:139`), so
`wantUp` is always true (`swap.go:69`) and `Start` always runs
(`swap.go:118-121`). On a host that has no unit or plist installed — a plain
binary install, any `runit`/`s6`/`openrc` install (`control.go` knows only
systemd and launchd), or macOS without the LaunchAgent — `systemctl --user
start mcremote.service` fails, `SwapAndRestart` returns an error, and the
deferred restore (`swap.go:93-108`) renames `.prev` back over the new binary.
The update reports failure and undoes a swap that had already succeeded.

Confirmed on a host: an isolated `mcrelay` copy on wonder downloaded the
release, swapped, failed to start (`Unit mcrelay.service not found.`), printed
`restored previous binary from .prev`, and exited 1 with the binary
byte-identical to before ([findings](0100-findings-update-refresh.md) §F3).

### The constraint that shapes the decision

**The process running `update` is the old binary.** The unit templates are
compiled into it (`setup.go:26-29`), so re-rendering in-process writes the very
content the update is supposed to replace. Any correct fix must obtain the
definition from the **new** binary.

### The second constraint: baked options

A unit is rendered from `Options` (`setup.go:41-83`) that only the operator's
original `setup-service` invocation knew: `--listen-port`, `--service-config`,
`--data-dir`, `--env`, `--binary`, `--working-directory`, `--unit-name`. After
that invocation those values survive **only inside the unit file**. A refresh
that re-renders from defaults silently un-bakes them — turning a fix into a
different outage. A refresh must therefore recover its inputs from the file it
is about to replace.

### The third constraint: a render is not a pure function of the unit

Confirmed on a host during Phase 0
([findings](0100-findings-update-refresh.md) §F4). `render` takes `HOME`,
`USER`, `PATH` and the `XDG_*` roots from the **process doing the rendering**,
not from `Options`. For `mcremote`, `servicePathEnv` (`setup.go:804-833`)
prepends tool prefixes to `os.Getenv("PATH")`, so the same binary renders a
different unit depending on who runs it:

    caller PATH = ambient      -> Environment=PATH=…/.local/share/mise/shims:…
    caller PATH = /usr/bin:/bin -> Environment=PATH=/usr/local/bin:…

and `Environment=XDG_RUNTIME_DIR=` disappears entirely when the caller has no
`XDG_RUNTIME_DIR`. (`mcrelay` is immune: its PATH is a fixed closed set, 0091 D1.)

A refresh that recomputed these would report a change on every host, and would
rewrite the daemon's `PATH` to whatever environment the update ran in — ssh,
cron, or a timer. So the recovery step must pin them from the installed file too,
and the re-render must reuse them verbatim. With that, a refresh changes exactly
what the *template* changed and nothing else.

### What makes a rewrite defensible at all

The template already directs operators away from editing the file:

* `mcremote.user.service.tmpl:1-2` — *"Managed by `mcremote setup-service` …
  Do not hand-edit if you re-run setup (use `--force` to overwrite)."*
* `:59-60` — *"Hardening (user-unit safe). On by default; set any of these to
  false **in a drop-in** to disable."*

Drop-ins live in `<unit>.d/` and survive a rewrite of the unit untouched. So the
supported customisation channel is unaffected by a refresh, and the file itself
is already declared ours.

## Decision Drivers

* **D1 — A release that fixes a unit must be able to apply it.** 0099 F4a is
  the existence proof: without this, the fix for an unstartable daemon cannot
  reach the daemons it was written for.
* **D2 — Never silently discard operator configuration.** Baked serve flags,
  `--env` secrets (which force mode `0600`, `setup.go:325-327`), drop-ins, and
  hand-added directives must all survive, or the refresh must decline.
* **D3 — A definition refresh must never fail an update.** The binary swap is
  `update`'s contract. Reconciliation is best-effort on top of it; a refresh
  error is a warning, never a rollback.
* **D4 — Deterministic, testable off a systemd host.** The package's existing
  seams (`OverrideRunSystemctl`, `OverrideRunLaunchctl`, `OverrideInstallOS`,
  `Options.UnitDir`) must carry the new tests, so CI on macOS still proves the
  Linux path.
* **D5 — No new persistent state.** No provenance database, no sidecar hash
  file, nothing to migrate or to go stale.
* **D6 — Honest about reach.** This change cannot repair a host whose currently
  installed binary predates it. Say that plainly rather than implying a fleet-wide
  fix.
* **D7 — Keep `internal/update` decoupled from `internal/cli/service`.**
  0065 P1 chose an injected interface so update logic is testable with fakes.
  Extend that interface; do not import the service package into it.

## Considered Options

1. **Post-swap reconciliation executed by the new binary** — after the swap,
   run `<new binary> setup-service --refresh`, which recovers the installed
   unit's options, re-renders from its own (new) template, rewrites only a
   provenance-clean unit, and reloads the manager. Then start.
2. **`daemon-reload` only** — add a reload before `Start`; never touch the
   definition.
3. **In-process re-render** — have `update` call `service.Setup(Force: true)`
   before starting.
4. **Re-render from defaults in the child** — same as 1, but the child runs
   plain `setup-service --force` instead of recovering baked options.
5. **Detect and warn** — compare, print a diff and the exact `setup-service
   --force` command, change nothing.
6. **Reconcile at `serve` startup** — the daemon repairs its own unit when it
   boots.
7. **Defer** — document the gap, keep pointing operators at `setup-service
   --force`.

## Decision Outcome

Chosen option: **"Post-swap reconciliation executed by the new binary"**, in
four parts, because it is the only option that satisfies **D1** without
violating **D2**. Options 2, 5 and 7 leave F1 unfixed by construction. Option 3
cannot work at all — the old binary carries the old template. Option 4 fixes F1
by silently discarding every baked flag, which trades one outage for another.
Option 6 cannot reach the case that motivates the whole record: a unit that
cannot start never runs the code that would repair it.

### Part A — `setup-service --refresh` (both products)

A new mode on the existing command, implemented as `service.RefreshUnit`, that
takes no options from the caller and derives everything from the installed
definition:

1. Locate the definition (`~/.config/systemd/user/<product>.service`, or
   `~/Library/LaunchAgents/com.magiccliremote.<product>.plist`). Absent →
   verdict `none`, exit 0, nothing done.
2. **Recover** `Options` from it: `ExecStart=` yields `Binary` and every baked
   serve flag; `WorkingDirectory=` and `Description=` are read directly; every
   `Environment=` line whose key is not one the template itself emits becomes
   `ExtraEnviron`; and every line whose key *is* one the template emits
   (`HOME`, `USER`, `LOGNAME`, `PATH`, `XDG_*`) is pinned as a render input, per
   the third constraint above.
3. **Re-render** with the binary's own embedded template.
4. Byte-identical → verdict `unchanged`.
5. Different **and** provenance-clean → back the file up to `<path>.prev`,
   write atomically via the existing `writeUnitAtomic` (`setup.go:1034`), verdict
   `refreshed`.
6. Different and **not** provenance-clean → write nothing, verdict `kept`, and
   name the escape hatch: `<product> setup-service --force`.
7. On Linux, finish with `systemctl --user daemon-reload` whenever a definition
   is installed — unconditionally, not gated on having written anything. That is
   what closes **F2**, and it costs one idempotent call.

Every verdict exits 0. Only an I/O or `systemctl` failure is an error.

**Provenance-clean** means all of:

| Rule | Rationale |
|---|---|
| Line 1 is the template's own `# Managed by …` marker for this product | The file says it is ours; the header also forbids hand edits |
| Exactly one `ExecStart=` line, and it parses as `<binary> serve [known flag pairs]…` | An unrecognised token means intent we cannot reproduce |
| No `ExecStartPre=`, `ExecStartPost=`, `ExecStop=`, `ExecReload=`, `EnvironmentFile=` | Directives the template never emits and that carry operator intent a rewrite would destroy |
| No section other than `[Unit]`, `[Service]`, `[Install]` | Same reason |

A directive the template *used to* emit and no longer does (`PrivateDevices=`,
`RestrictNamespaces=`) is deliberately **not** a disqualifier — removing it is
the whole point of the release that motivates this record.

### Part B — the update sequence

`SwapAndRestart` gains an optional `Refresher` (nil in existing tests, so they
keep passing) and becomes:

```
IsActive → Stop → swap binary → Refresh (child: new binary) → Start → wait active
                                    │
                                    └─ on Start failure: restore binary .prev
                                       AND unit .prev, reload, Start again
```

Refresh sits **after** the swap (the new template only exists once the new
binary is in place) and **before** `Start` (so the new definition is what
starts). Its failures are logged and stepped over (**D3**). Rollback restores
the definition as well as the binary, so a failed update leaves the host exactly
as it was found.

On macOS no reload step is needed: `Stop` is `launchctl bootout`, which unloads
the job, and `Start` then `bootstrap`s from the plist on disk
(`control.go:83-99`) — the plist is re-read from disk in the same cycle. macOS
needs Part A's *write*, not Part A's *reload*. This is the mechanism
[MADR 0058](0058-MADR-macos-launchd-service-hardening.md) §"Plist on disk vs
loaded job" describes.

### Part C — the heal-start guard (F3)

`ServiceControl` gains `IsInstalled`. `run.go` sets `HealStart` from it instead
of hardcoding `true`. A host with no definition installed gets its binary
swapped and nothing else — which is what `update` means there.

### Part D — the installer refreshes on upgrade

`svc_systemd`'s upgrade branch (`scripts/install.sh:417-439`) runs
`<installed binary> setup-service --refresh` for each product whose unit exists,
before restarting them, and reports the verdict. `install.sh` is re-fetched by
every `curl | sh`, and `install_binaries` runs before `setup_service`
(`:751`, `:755`), so this part — and only this part — repairs hosts whose
currently installed binary predates this record.

| Part | F1 | F2 | F3 | Repairs today's fleet |
|---|---|---|---|---|
| A — `--refresh` | ✅ | ✅ | — | only via D |
| B — sequence | ✅ | ✅ | — | no |
| C — heal guard | — | — | ✅ | no |
| D — installer | ✅ | ✅ | — | ✅ |

### Consequences

* Good, because a template fix reaches every host that runs `update` — the
  property 0099 F4a needed and did not have.
* Good, because baked options, `--env` secrets, drop-ins, and the `0600` mode
  rule survive a refresh untouched; a unit we cannot reproduce is never
  rewritten, only reported.
* Good, because a failed update now restores the definition as well as the
  binary, so the "stop → swap → start fails" path is fully reversible.
* Good, because `update` stops failing on hosts that never had a service (F3),
  which includes every `runit`/`s6`/`openrc` host the installer supports.
* Bad, because **the fix does not apply to the update that installs it.** The
  orchestrating parent is the old binary; hosts on today's releases get the new
  behaviour only from their *next* `update`, or immediately from `curl | sh`
  (Part D), or from `setup-service --force`. This is inherent to a self-updating
  binary and is stated in the release notes rather than papered over.
* Bad, because `update` now executes a subprocess it downloaded moments ago.
  The binary is already SHA256-verified against the release's `SHA256SUMS`
  (`internal/update/download.go`) and is about to be run as a daemon anyway, so
  this adds no new trust, but it does add a new failure mode — bounded by a
  timeout and treated as non-fatal.
* Neutral, because pinning the environment block means a refresh will not pick
  up a *new* entry added to `servicePathEnv` by a later release. That is the
  deliberate trade — stability over freshness — and the refresh reports the
  difference as a warning so `setup-service --force` remains the way to
  re-derive.
* Neutral, because a refresh writes `<unit>.prev` into the unit directory.
  systemd ignores files without a unit suffix — confirmed on a host in Phase 0.
* Neutral, because the recovery parser is a second consumer of the template's
  shape. `TestRefreshRoundTripsRenderedUnit` pins the two together: any future
  template change that the parser cannot read fails the build.

### Confirmation

| # | Claim | How it is confirmed |
|---|---|---|
| C1 | A stale unit is rewritten by `update` | Unit rendered with an obsolete directive injected → `--refresh` → file matches the current render, `.prev` holds the old one |
| C2 | Baked options survive | `setup-service --listen-port 9099 --env K=V` → `--refresh` → re-rendered unit still carries both, mode still `0600` |
| C3 | A hand-edited unit is never rewritten | `ExecStartPre=` injected → verdict `kept`, file byte-identical, message names `setup-service --force` |
| C4 | `daemon-reload` runs on every Linux update with a unit installed | Recorded `runSystemctl` calls in `TestRefresh…`; on a host, `journalctl --user` shows the reload before the start |
| C5 | Refresh failure never fails an update | Fake refresher returning an error → swap still succeeds, `Start` still called |
| C6 | Rollback restores the definition | Fake refresher reports a change, `Start` fails → binary **and** unit restored, reload re-run |
| C7 | F3 | `IsInstalled` false → `Start` never called, `update` exits 0 |
| C8 | Installer repairs an existing managed unit | `scripts/install_test.sh`: stub binary logs `setup-service --refresh`; the existing "unit was NOT rewritten" assertion (foreign unit) still passes |
| C9 | End to end | On a Linux host: install a unit with the 0099 F4a directives, `mcrelay update --force`, unit no longer carries them and the service is active |
| C10 | A refresh does not rewrite the environment block | `TestRefreshPinsRenderedEnvironment`: render with one `PATH`, refresh under another, `Environment=PATH=` unchanged; `XDG_RUNTIME_DIR` unset in the caller does not drop the line |

**Already observed** ([0100 findings](0100-findings-update-refresh.md), wonder,
Ubuntu 26.04 / systemd 259, 2026-08-18): F2 — a restart without `daemon-reload`
runs the cached definition while reporting success; F3 — `update` on a host with
no unit rolls back a byte-identical binary and exits 1; the `<unit>.prev` backup
location is inert; and F4, which changed this record before any code was
written.

## Pros and Cons of the Options

### 1. Post-swap reconciliation executed by the new binary (chosen)

* Good, because it is the only option where the definition that lands is the
  one the release ships.
* Good, because recovery-then-render preserves operator input without any
  stored state (**D2**, **D5**).
* Good, because the provenance rule is a small, enumerable list of conditions
  that a test can drive exhaustively (**D4**).
* Bad, because it introduces a parser for a file we also generate — two
  representations of one shape, kept honest only by a round-trip test.
* Bad, because it needs a subprocess, a timeout, and a documented stable JSON
  contract between two versions of the same program.
* Neutral, because the new `--refresh` mode is useful on its own, independent of
  `update`, and gives `install.sh` something better to call than the advisory at
  `:434`.

### 2. `daemon-reload` only

* Good, because it is a three-line change with no new concepts.
* Good, because it closes F2 completely.
* Bad, because it leaves F1 untouched — the reported regression's most damaging
  half. A reload of an unchanged, broken unit re-reads the same broken unit.

### 3. In-process re-render

* Good, because no subprocess, no parser, no new contract.
* Bad, because it **cannot work**: the running process is the old binary and its
  embedded template (`setup.go:26-29`) is the old template. It would write the
  content the update exists to replace, and would report success doing it.

### 4. Re-render from defaults in the child

* Good, because it removes the parser entirely — the child just runs
  `setup-service --force`.
* Bad, because it silently discards `--listen-port`, `--service-config`,
  `--data-dir`, `--env` and `--binary` on every update. On a relay host that is
  a new outage on the next restart, caused by the tool meant to prevent one.
  Straight violation of **D2**.

### 5. Detect and warn

* Good, because it cannot break anything, and an accurate warning is strictly
  better than today's silence.
* Good, because it needs no subprocess — a *diff* against the old template is
  wrong in the same way option 3 is, but "your unit differs from the shipped
  default" can be stated without the new template.
* Bad, because the operator who most needs it is the one whose daemon is in a
  crash loop and who is not watching `update`'s stdout.
* Bad, because it makes the fix conditional on human follow-through, which is
  the same bet 0099 D2 already lost once.

### 6. Reconcile at `serve` startup

* Good, because it would repair the existing fleet without a second update.
* Bad, because the motivating failure is a unit that **never starts** — the
  repair code never runs.
* Bad, because a daemon that rewrites its own unit and reloads the manager mid
  boot needs a restart to apply it, i.e. a self-restart loop with no natural
  fixed point.

### 7. Defer

* Good, because `setup-service --force` and `curl | sh` already exist as manual
  remedies.
* Bad, because it accepts that "the release fixes the unit" and "the update
  applies the fix" are permanently unrelated statements — and 0099 shows that a
  gap left to discipline is a gap that recurs.

## More Information

### Finding-to-remedy map

| Finding | Evidence | Remedy |
|---|---|---|
| F1 unit never refreshed | Only writer is `setup.go:342`, reachable only from `setup-service`; `14ea974` is a template-only fix | Parts A + B, and D for existing hosts |
| F2 no `daemon-reload` | `control.go:104` starts directly; `setup.go:348` and `:584` reload, `update` does not | Part A step 7 |
| F3 unconditional heal-start | `run.go:139` → `swap.go:69` → `swap.go:118-121` → restore at `swap.go:93-108` | Part C |

### What this record explicitly does not fix

* **Hosts already running a broken unit whose binary predates this change.**
  They need `curl … | sh` (Part D), `setup-service --force`, or one more
  `update` after this one ships. Release notes must say so.
* **A unit installed under a custom `--unit-name`.** `control.go` derives the
  unit from the product name alone (`:39`, `:61`, `:104`), so `update` has never
  managed such installs and still will not. Recorded here so the limitation is
  not rediscovered as a new bug.
* **Non-systemd Linux backends.** `install.sh` can install `runit`, `s6` and
  `openrc` services; `internal/cli/service` knows only systemd and launchd, so
  `update` cannot cycle them. Part C stops it from *failing* on those hosts; it
  does not teach it to restart them.

### Deliberately out of scope

* **`ProbeStatus`'s `PlistPresent` on Linux** is computed from `systemctl --user
  is-enabled` (`control.go:158-159`), which exits non-zero for a unit that is
  installed but disabled — so `doctor` under-reports there. Real, adjacent, and
  not on the update path: Part C uses `LoadState` instead rather than changing
  `doctor`'s output in a bug-fix record.
* **A system-scope (`--system`) relay unit.** 0065 §"Open questions" already
  parks this; nothing here changes it.
* **Plist provenance.** The launchd path gets the same write-and-recover
  treatment, but a plist has no comment line to carry the managed-by marker;
  the marker check is therefore replaced by a structural one (Label matches
  `com.magiccliremote.<product>` and `ProgramArguments[1] == "serve"`), stated
  in the plan.

### Related records

* [0100-PLAN-update-unit-refresh-and-daemon-reload.md](0100-PLAN-update-unit-refresh-and-daemon-reload.md)
  — the implementation plan for this decision.
* [0100-findings-update-refresh.md](0100-findings-update-refresh.md) — Phase 0
  host confirmation of F2, F3 and the backup location, and the discovery of F4.
* [0065-MADR-update-automation.md](0065-MADR-update-automation.md) — defines the
  update sequence this record extends. §D1 (`:344`) describes it as
  "stop unit → rename with `.prev` retained → start unit → `wait_for_up`"; unit
  refresh was never part of it. Not superseded — extended.
* [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md)
  — the bootout/bootstrap requirement that makes Part A's macOS half a write-only
  change.
* [0072-MADR-phone-reconnect-and-provider-timeout-incident.md](0072-MADR-phone-reconnect-and-provider-timeout-incident.md)
  — D5 introduced `HealStart` (`swap.go:24-27` cites it); Part C makes it
  conditional rather than removing it.
* [0097-MADR-linux-curl-installer.md](0097-MADR-linux-curl-installer.md) and
  [0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md)
  — the installer contract and the F4a template fix that F1 cannot deliver.
