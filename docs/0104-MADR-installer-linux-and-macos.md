---
status: proposed
date: 2026-08-19
decision-makers: Project Owner (scope and acceptance)
consulted: none
informed: operators installing via curl|sh on Linux or macOS
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Make the curl bootstrap installer first-class on Linux and macOS

## Context and Problem Statement

The advertised one-liner is the install path:

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

On a Mac that line dies immediately:

```text
error: this installer supports Linux only (found Darwin).
macOS installs need code-signing for durable Full Disk Access grants and are
not covered here; build from source with 'make install' instead.
```

That is not a later accident. [0097](0097-MADR-linux-curl-installer.md)
shipped the installer as **Linux only**, and the tree still enforces it:

| What | Where | What it does |
|---|---|---|
| Hard reject | `scripts/install.sh:73-75` (`detect_arch`) | `uname -s` must be `Linux` or exit 1 |
| Test lock | `scripts/install_test.sh:116-125` | asserts `Darwin rejected (exit 1)` and the "Linux only" string |
| README | `README.md:126-132` | "The installer is Linux-only on purpose" |
| Ops runbook | `docs/ops-linux-install.md:66-67` | "macOS is deliberately out of scope" |
| Release aliases | `.github/workflows/ci.yml:624-626` | Darwin aliases are **not** published, because "darwin aliases would advertise support that does not exist" |

0097 itself said the cut was temporary: "the Linux installer can ship now
and macOS can be added later as one additional branch without redesign."
That later has arrived. The operator sitting at a Mac — the same host the
daemon is developed on ([0060](0060-MADR-local-unsigned-build-and-install.md))
— is told to clone the repo and install a Go toolchain to do what the
one-liner already does on Linux.

This record reverses only the **platform scope** of 0097. It does not
reopen A2 (stable aliases, no GitHub API), B1 (bootstrap, then hand off
to `setup-service`), C2 (`linux/arm64`), the no-root rule, or POSIX `sh`.
Those stay. Windows stays unsupported.

* Implementation Plan: [0104-PLAN-installer-linux-and-macos.md](0104-PLAN-installer-linux-and-macos.md)

Related:
[0058](0058-MADR-macos-launchd-service-hardening.md) (LaunchAgent-only,
no sudo linger),
[0059](0059-MADR-native-paths-and-linux-macos-parity.md) (`~/.local/bin`
on both),
[0060](0060-MADR-local-unsigned-build-and-install.md) (unsigned Darwin
builds already work),
[0069](0069-MADR-macos-permissions-and-sandbox-parity.md) (TCC identity,
unsigned CDHash churn),
[0097](0097-MADR-linux-curl-installer.md) (the installer being extended),
[0099](0099-MADR-installer-service-state-verification.md) (report measured
service state, not requested state),
[0100](0100-MADR-update-unit-refresh-and-daemon-reload.md) (`--refresh`
already rewrites a launchd plist).

### Assessment: the macOS work is already in the binaries

0097's decisive fact was that the installer is a bootstrap, not a package
manager. That is still true, and it is more true on Darwin than 0097
assumed. The Go side already does the Darwin job:

| Capability | Where it lives | Consequence for the script |
|---|---|---|
| Darwin/amd64 and Darwin/arm64 artifacts | `.github/workflows/ci.yml:163-164` | The bytes exist on every tagged release. Only the *unversioned alias* is missing. |
| LaunchAgent write, enable, bootstrap, kickstart | `internal/cli/setup_service.go:149-165`, `internal/cli/service/` | Script delegates, same as systemd. Writes no plist itself. |
| `--refresh` of an existing plist | `internal/cli/service` (`TestRefreshDarwinRewritesPlist`) | Upgrade path already exists. `install.sh:411-418` (`svc_refresh_units`) currently looks only at systemd unit files. |
| Stop / start / is-active / is-installed | `internal/cli/service/control.go:18-148` | `launchctl print` / `bootout` / `bootstrap`+`kickstart`; plist path `~/Library/LaunchAgents/com.magiccliremote.<product>.plist`. |
| ETXTBSY-safe swap on a running Darwin binary | `scripts/install-binary.sh` | Stage, **then** stop, **then** rename. `install.sh` today installs *before* it stops (`install.sh:769-774`), which is fine on Linux (old inode) and wrong on Darwin. |
| launchd teardown race | `scripts/install-binary.sh:104-117` (`wait_for_teardown`) | `bootout` is async. Bootstrapping while the label is still releasing fails with `Bootstrap failed: 5: Input/output error`. The installer has no equivalent wait. |
| Unsigned Darwin execution | [0060](0060-MADR-local-unsigned-build-and-install.md) F1, F4 | `make install` already produces a working unsigned LaunchAgent. A curl-installed unsigned binary is the same class of artifact. |
| TCC / FDA grant | [0069](0069-MADR-macos-permissions-and-sandbox-parity.md) D5/D6, [ops-macos-tcc.md](ops-macos-tcc.md) | Grants attach to code identity. Unsigned identity churns on every upgrade. The installer cannot mint a Developer ID. It can only tell the truth. |

The 0097 rationale for excluding macOS was "durable TCC grants need
code-signing, and we have no Developer ID / notarization pipeline." That
is still true, and it is **also true of `make install`** unless the
operator sets `MC_CODESIGN_IDENTITY`. Excluding the one-liner does not
protect FDA durability. It only forces a toolchain onto the Mac that
already runs the daemon.

### Assessment: what actually breaks if we only delete the Darwin `die`

Deleting `install.sh:73-75` is not a macOS installer. Four independent
gaps remain, each of which would ship as a new defect:

1. **Wrong asset name.** `download_all` / `verify_and_resolve`
   (`install.sh:157-194`) hardcode `linux` in the URL and the
   `SHA256SUMS` match. A Darwin host would request
   `mcremote-linux-arm64`, which is the wrong GOOS, and even if the
   aliases were published would install a Linux ELF the kernel cannot
   exec.
2. **No Darwin aliases.** `ci.yml:626-631` copies only
   `mcremote-linux-*-$VER` and `mcrelay-linux-*-$VER`. The versioned
   Darwin assets (`mcremote-darwin-arm64-0.13.9.1`, …) are on the
   release; the constructible names the installer needs
   (`mcremote-darwin-arm64`, …) are not.
3. **`detect_init` never returns launchd.** `install.sh:117-149` probes
   `systemctl`, `runsvdir`/`sv`, `s6-svscan`/`s6-svc`/`s6-svstat`,
   `rc-service`. On a stock Mac none of those exist, so `INIT=none` and
   `setup_service` (`install.sh:557`) falls through to "no supported
   service manager" and prints a `nohup` recipe. Homebrew can install
   runit or s6; those must **not** win over `launchctl` on Darwin.
4. **Stop-after-install and no teardown wait.** Linux rename-over-running
   is an inode trick. Darwin can raise `ETXTBSY`. Combined with
   launchd's async `bootout`, a naive port leaves the first real Mac
   upgrade in the state `install-binary.sh` already spent a MADR-worth
   of bugs getting out of.

### Assessment: POSIX and the two `/bin/sh`s

The script is POSIX `sh` (`set -eu`, no `[[`, no `pipefail`, last line
`main "$@"`). That constraint stays: Alpine `/bin/sh` is busybox ash,
macOS `/bin/sh` is bash 3.2 in POSIX mode. The hasher already prefers
`sha256sum` then `shasum -a 256` then `openssl dgst -sha256`
(`install.sh:59-67`) — `shasum` is the native macOS tool. `curl
--proto '=https' --tlsv1.2` works on Apple's curl. `/proc` reads are
already gated on `[ -r … ]` and will no-op on Darwin.

`uname -s` is `Darwin`. `uname -m` is `arm64` on Apple silicon and
`x86_64` on Intel; both already map (`install.sh:77-85`). No new arch
table is required.

### Assessment: what the installer must never claim on macOS

[0058](0058-MADR-macos-launchd-service-hardening.md) locked
LaunchAgent-only: no LaunchDaemon, no sudo, no user-level linger.
`setup-service --no-linger` is a documented no-op on Darwin
(`setup_service.go:87`). The 0099 honesty contract
([0099](0099-MADR-installer-service-state-verification.md)) still
applies: report what was measured.

Therefore a healthy Darwin install reports **session-bound supervision**,
never "running, and enabled at boot". That string is systemd+linger
only (`install.sh:619-624`). Claiming boot persistence for a
LaunchAgent would be the same class of lie 0099 existed to stop.

## Decision Drivers

* The same one-liner must install a verified, running daemon on Linux
  and on macOS.
* 0097's architecture is reused, not replaced: aliases, hash-value
  verify, no API, no root, POSIX `sh`, bootstrap then `setup-service`.
* Service setup is delegated to the binary that already knows launchd.
  The script does not grow a second plist writer.
* Failure modes stay legible. A Mac must never be told to install a
  Linux ELF, and must never be told its LaunchAgent is enabled at boot.
* TCC is told honestly. The installer does not pretend it signed
  anything, and does not refuse to install because it cannot.
* Linux behaviour that 0097/0099/0100 already verified must not change.
* Windows remains unsupported.

## Considered Options

* **D1 — Keep Linux-only; document `make install` for Macs**
* **D2 — Accept Darwin in `detect_arch` only (delete the `die`)**
* **D3 — Extend the existing bootstrap to Darwin: aliases, `OS` in the
  URL, `launchd-agent` init, stop-before-swap, TCC advisory** (chosen)
* **D4 — Split into `install.sh` (Linux) and `install-macos.sh`**
* **D5 — Require Developer ID + notarization before any Darwin one-liner**

## Decision Outcome

Chosen option: **"D3 — extend the existing bootstrap to Darwin"**,
because the product already has Darwin artifacts, a launchd
`setup-service`, an ETXTBSY-safe swap recipe, and a TCC runbook. What
it lacks is the one-liner wiring, and that wiring is one additional
branch of the 0097 script, which is exactly the extension 0097
described.

0097's Linux-only *scope* is superseded by this record. 0097's
*architecture* (A2 + B1 + C2) is kept.

Concretely:

1. **`detect_arch` accepts `Linux` and `Darwin`.** Anything else still
   exits 1. Sets `OS=linux|darwin` and the existing `ARCH=amd64|arm64`.
2. **Asset names are `${product}-${OS}-${ARCH}`.**
   `verify_and_resolve` matches
   `  ${product}-${OS}-${ARCH}-[0-9]` in `SHA256SUMS` and strips that
   prefix for `RESOLVED_VER`. The hardcoded `linux` in
   `install.sh:159-192` goes away.
3. **CI publishes Darwin aliases** the same way it publishes Linux
   ones: byte copies of the versioned assets, after `SHA256SUMS-${VER}`
   is written, not added to the manifest. The comment at
   `ci.yml:624-626` is deleted with the restriction.
4. **`detect_init` on Darwin is launchd-first.** If `launchctl` exists,
   `INIT=launchd-agent`. Homebrew runit/s6/`systemctl` must not win.
   Linux probing is unchanged.
5. **Service setup on `launchd-agent` delegates to
   `mcremote setup-service`**, mirroring `svc_systemd`: first install
   writes the plist; a pre-existing plist is `--refresh`'d (0100) and
   the previously-running agents are restarted. `--force-service`
   still means `setup-service --force`. `--with-relay-service` still
   means also set up `mcrelay`. `--no-linger` remains a no-op.
6. **Stop-before-swap on both platforms**, with a launchd
   `wait_for_teardown` after `bootout`. `svc_note_active` runs while
   the old processes are still observable; `setup_service` must not
   re-probe and conclude nothing was running. This is the
   `install-binary.sh` ordering, lifted into the bootstrap so Darwin
   upgrades do not hit `ETXTBSY` or `Bootstrap failed: 5`.
7. **Quarantine is cleared if present** (`xattr -d com.apple.quarantine`
   on each installed binary, best-effort, missing `xattr` is a no-op).
   `curl` itself does not usually set the xattr; browsers and some
   helper downloaders do. Clearing it is defence in depth, not a
   Gatekeeper bypass that needs a signature.
8. **Summary and advisories are platform-honest.**
   * Darwin healthy: session-bound LaunchAgent, `launchctl print
     gui/$(id -u)/com.magiccliremote.mcremote` as the check command.
     Never the systemd "enabled at boot" line.
   * Darwin always prints the Full Disk Access grant path
     (`~/.local/bin/mcremote`) and that unsigned upgrades drop the
     grant unless the operator signs with `MC_CODESIGN_IDENTITY` (see
     [ops-macos-tcc.md](ops-macos-tcc.md)).
   * Uninstall boots out both labels, deletes both plists, then
     deletes both binaries — the Darwin sibling of the 0099 uninstall
     stop-before-delete fix.
9. **The offline test harness grows a Darwin mode** so the suite keeps
   producing identical results on a Mac workstation and a Linux CI
   runner. Today's "Darwin rejected" assertion is inverted. Every
   existing Linux assertion stays.

### Consequences

* Good, because the same one-liner works on the two Unix hosts this
  product actually supports.
* Good, because it reuses `setup-service`, `--refresh`, and the
  launchd control path instead of growing a shell plist writer that
  would drift from `internal/cli/service`.
* Good, because Darwin aliases follow the already-proven 0097 recipe
  (copy after checksum, never listed in `SHA256SUMS`), so the installer
  still makes zero GitHub API calls.
* Good, because stop-before-swap plus `wait_for_teardown` closes the
  two Darwin-specific races that `install-binary.sh` already documented
  (`ETXTBSY`, `Bootstrap failed: 5`) before they can ship in the
  one-liner.
* Neutral, because unsigned curl-installed binaries have the same TCC
  durability as unsigned `make install`. Operators who need a grant
  that survives upgrades still sign, same as today.
* Neutral, because alias count grows by four (`mcremote`/`mcrelay` ×
  `darwin-amd64`/`darwin-arm64`). They are copies of bytes already
  uploaded.
* Bad, because a LaunchAgent stops on logout. A Mac that is a
  headless always-on host still needs a login session (auto-login or
  Screen Sharing), which [0058](0058-MADR-macos-launchd-service-hardening.md)
  already accepted. The installer must say this, every time.
* Bad, because the first Darwin-capable `install.sh` only helps hosts
  that download *that* script. Older published `install.sh` assets
  keep rejecting Darwin until the next release ships. Pinning
  `MCREMOTE_VERSION` to a pre-0104 tag will also 404 the new Darwin
  aliases.

### Confirmation

The decision is satisfied when, with no Go toolchain:

* On Linux, every 0097 / 0099 / 0100 installer behaviour that is green
  today stays green. `sh scripts/install_test.sh` does not lose a
  Linux assertion.
* On Darwin (real host, Apple silicon and — stubbed — Intel):
  `curl … | sh` installs both binaries to `~/.local/bin`, clears
  quarantine if present, writes
  `~/Library/LaunchAgents/com.magiccliremote.mcremote.plist`, and
  `launchctl print gui/$(id -u)/com.magiccliremote.mcremote` shows
  `state = running`. Exit 0. The summary does **not** contain
  "enabled at boot".
* Re-running the script on that Mac is idempotent: one plist, daemon
  recycled onto the new binary, no `ETXTBSY`, no `Bootstrap failed: 5`.
* `--uninstall` on that Mac boots the agent out, deletes the plist,
  deletes the binaries, leaves `~/.config` / `~/.local/share`.
* `--dry-run --verbose` on a stubbed Darwin/arm64 host prints
  `arch: arm64` and a `darwin` OS, and does not print "Linux only".
* A deliberately corrupted Darwin download exits 2 and leaves a
  pre-existing install untouched — same contract as Linux.
* `shellcheck -s sh scripts/install.sh` is clean. The script still
  has no `[[`, no `pipefail`, no `sudo`, and ends with `main "$@"`.
* The next tagged release publishes
  `mcremote-darwin-{amd64,arm64}` and `mcrelay-darwin-{amd64,arm64}`
  whose SHA-256 matches the versioned `SHA256SUMS` lines.

Test coverage lives in `scripts/install_test.sh`. Darwin cases must
run on Linux CI via stubbed `uname` / `launchctl`, the same way today's
Linux cases already run on a Mac.

## Pros and Cons of the Options

### D1 — Keep Linux-only

* Good, because it is zero work and matches the current tests.
* Bad, because the one-liner on a Mac is a hard error, and the
  workaround is a Go toolchain.
* Bad, because the exclusion no longer matches the product: Darwin
  binaries, launchd `setup-service`, and `~/.local/bin` parity already
  shipped.
* Bad, because the stated reason (TCC durability) also applies to
  `make install`, so the cut does not buy the property it cites.

### D2 — Delete the Darwin `die` only

* Good, because it is a one-line change.
* Bad, because the host then downloads a Linux ELF, or 404s, or
  installs binaries and prints `nohup` next to a working `launchctl`.
* Bad, because it would ship four new defects (wrong GOOS, missing
  aliases, `INIT=none`, stop-after-install) under the appearance of
  macOS support.

### D3 — Extend the existing bootstrap (chosen)

* Good, because it is the extension 0097 already described, against
  code that already exists.
* Good, because one script, one URL, one test file, two Unixes.
* Good, because Linux stays on the path that 0098/0099 already
  verified on real hosts.
* Neutral, because Darwin aliases wait for the next tag to exist on
  GitHub. Until then the new script can be exercised offline against
  the harness and against locally staged assets.
* Bad, because launchd session-binding and unsigned TCC churn have to
  be explained in the installer's own output, every time, or operators
  will file them as installer bugs.

### D4 — Two scripts

* Good, because each script could ignore the other OS entirely.
* Bad, because the download, verify, staging, flags, and summary
  machinery would fork, and 0099-class bugs would have to be fixed
  twice.
* Bad, because the advertised one-liner would have to become two
  one-liners, or a wrapper that still has to detect the OS.

### D5 — Require Developer ID + notarization first

* Good, because FDA grants would survive upgrades and Gatekeeper
  would be quiet for browser-downloaded binaries.
* Bad, because that pipeline does not exist, needs a paid Apple
  account, and blocks the one-liner on a problem `make install` also
  does not solve.
* Bad, because it confuses distribution signing with install-time
  bootstrap. The installer cannot sign with an identity it does not
  have. Notarization is a later record, not a gate on D3.

## More Information

### What this record does not change

* No root. No `sudo`. No LaunchDaemon. No `/usr/local` default.
* No GitHub API in the default path.
* No shell-side unit or plist templating.
* `mcremote update` remains the upgrade tool
  ([0065](0065-MADR-update-automation.md),
  [0103](0103-MADR-update-tracks-release-build-and-active-service.md)).
  Re-running the one-liner stays safe and is not the blessed upgrade.
* Windows, `linux/386`, 32-bit ARM, riscv64 stay rejected.
* AppArmor userns remediation stays a Linux advisory that needs sudo.

### Signing, quarantine, and Gatekeeper — facts, not hopes

* `curl -o` on macOS does **not** normally set
  `com.apple.quarantine`. Safari and some GUI downloaders do. The
  installer still deletes the xattr if it is there.
* An unsigned Mach-O started from a terminal or as a LaunchAgent
  already works; that is the 0060 `make install` path. D3 does not
  make this worse.
* Ad-hoc `codesign -s -` still mints a fresh CDHash per build
  ([0069](0069-MADR-macos-permissions-and-sandbox-parity.md) D6). The
  installer will not ad-hoc sign. It is theatre and it still churns
  the FDA grant.
* A future Developer ID / notarization pipeline can stamp the same
  Darwin artifacts before they are aliased. D3 does not have to be
  redone for that; the installer keeps verifying by hash.

### Linger vs login session

Linux systemd-user + linger is the only rootless "start at boot" this
project has. Darwin LaunchAgents die on logout. The README already
says so (`README.md:327-328`). The installer summary on Darwin must
say the same thing in one line, and must point at keeping a login
session for an always-on Mac. It must not invent a linger equivalent.

### Open questions this record does not need answered to proceed

* Whether hosted `macos-15` CI is turned back on
  (`ci.yml:222-225` currently leaves it off). Darwin installer
  behaviour is stub-tested in `install_test.sh` and acceptance-tested
  on the owner Mac. A hosted runner is nice; it is not a gate.
* Whether Intel Macs are still in the operator set. The `darwin/amd64`
  artifact is already built; D3 installs it when `uname -m` is
  `x86_64`. Live verification on Intel is best-effort.
* Developer ID / notarization. Separate record, if ever.
