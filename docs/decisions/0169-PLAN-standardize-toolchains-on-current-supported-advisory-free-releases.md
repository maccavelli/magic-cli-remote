---
status: proposed
date: 2026-09-23
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0169 — Standardize every toolchain on its newest supported, advisory-free stable release

Implements [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md)
decisions D1–D12, closing findings F1–F10.

## Goal

Observable end states, each checked by the version audit (D12) against the probe:

- **No known advisory** affects any installed or standard version on any of the four hosts or in CI.
- **Go 1.27.1** is the toolchain on every host and in CI. This repository's `go.mod` says
  `go 1.27.1`. The 11 standard Go tools are built by go1.27.1.
- **Flutter 3.47.5 / Dart 3.13.4** on every host, in CI (`FLUTTER_VERSION`) and in magic-git's
  pinned SDK.
- **Temurin 25** (25.0.4.1, or its newer CPU successor) is Flutter's JDK on every host and CI's
  `java-version`. JDK 21 is removed after each host builds the APK on 25.
- **Node: the newest Active LTS** on every host: 24.21.0 until 2026-10-28, then 26.x. CI's
  `NODE_VERSION` follows.
- **Python 3.14.7** is the developer Python on every host.
- **git 2.55.x, gh 2.101.0, glab 1.119.0** on every host.
- **A standard file** records all of the above, and **`version_audit.py`** reports zero drift
  and zero advisories. It was first seen failing on the pre-remediation state.

## Scope

### In scope (the only files any phase may touch)

**This repository:** `go.mod`, `go.sum` (P3, the `go` directive only, plus whatever the 1.27
toolchain's `go mod tidy` rewrites); `.github/workflows/ci.yml` (`FLUTTER_VERSION`, `NODE_VERSION`,
`java-version` lines only); this pair; the additive amendment to
`0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md` (P5).

**magic-git** (the owner commits there): `scripts/tools/devenv/standard.json` (new),
`scripts/tools/devenv/version_audit.py` (new), `scripts/tools/devenv/README.md`,
`build_macos.sh` (`FLUTTER_VERSION` only), and golden files **only** if P4's re-run shows
Flutter 3.47.5 moved them. That last case is a deviation to raise, not to absorb.

**Host configuration.** Each change is approved per host (C2), made with a dated backup, and
made through that host's own source:

| Host | Files and installs |
| --- | --- |
| Windows | machine installs: Node, Temurin 21/25, Git for Windows (elevated); `HKCU\Environment` `JAVA_HOME`; `%APPDATA%\.flutter_settings`; `~\sdk\go1.27.1`, `~\sdk\flutter` (git checkout of a tag); `~\toolchains\gh`, `~\toolchains\glab`; `~\go\bin`; `%APPDATA%\go\env` (`GOTOOLCHAIN`) |
| WSL | `~/sdk/go1.27.1`, `~/sdk/jdk-25`, `~/sdk/flutter`; `~/.config/devenv.sh` (the Go path and `JAVA_HOME` lines); `~/.config/flutter/settings`; `~/.config/go/env`; `~/.local/bin/{gh,glab}`; mise global config for node and python; apt: the git-core PPA (owner runs `sudo`) |
| Linux server | dotfiles `mise/.config/mise/config.toml` (go, java, node, python, flutter, glab lines); `~/.config/go/env`; `~/.local/bin/gh`; its `~/.local/bin/git` build; `~/go/bin` |
| macOS laptop | Homebrew formulae and casks (gh, glab, node@24, python@3.14, flutter, a Temurin 25 cask); `~/.local/go1.27.1` and the `~/.local/bin/go` link; `~/.config/flutter/settings`; `~/Library/Application Support/go/env`; `~/go/bin` |

### Out of scope

- **Other repositories' `go.mod` files** (the mcp-server-*, mcplib, ocp-*, elastic-toolbox-utility
  repos). A 1.27.1 toolchain builds modules that declare 1.26. Each repository moves its `go`
  directive through its own gates, not this plan.
- **The Android bytecode target**, which stays Java 17 (MADR 0168 D2).
- **Shell-init restructuring** on any host. Only the listed lines change, and only with approval.
- **Rust** in the acp-go-sdk checkout's `mise.toml` (MADR Open questions).

## Stability rule

Every phase ends with:

```sh
python3 scripts/tools/devenv/run_probe.py local wsl:<distro> ssh:<host> ssh:<host>   # every snapshot UNCHANGED unless the phase edits that file
python3 scripts/tools/devenv/version_audit.py        # from P1 on: the phase's toolchain shows no drift and no advisory
```

Phases that change this repository also run `make pre-add-check`, `make race`, and
`make ci-windows` (from Git Bash). CI changes are proven locally first, then with a
`workflow_dispatch` of `ci.yml` on `master` so `android-apk` runs (MADR 0168's lesson).
Commit at the end of each phase with `git commit --no-edit`. **`git push`, tags and
workflow dispatches need an explicit ask in the turn they happen.**

## Cross-cutting contracts

- **C1 — Exposures before anything else.** P0 completes before any line change.
- **C2 — Host shell and environment files are the owner's.** Every edit to shell init, the
  registry environment, mise config or Go env is listed in this plan, approved per host, made
  with a dated backup outside any repository, and verified with the probe's before/after
  snapshot showing only the intended files changed.
- **C3 — Every download is verified against its publisher's checksum** before use (nodejs.org
  `SHASUMS256.txt`, Adoptium API, go.dev `sha256`, GitHub release checksums, Git for Windows
  release notes).
- **C4 — Nothing old is removed until its replacement has passed on that host.**
- **C5 — CI moves only after the same change is green locally**, and after a dispatched
  `android-apk` run where the JDK or Flutter changes.
- **C6 — No host identifier in committed content.** Records and magic-git files use
  `<host>`/`<user>`.

The contract most at risk is **C2**. The remaining work is dozens of small edits across four
machines, and "just one more line in the same file" is the easy slip. The probe's snapshot
diff exists to catch exactly that.

## Dependency and delivery order

P0 → P1 → then P2, P3, P4, P5, P6, P7 in any order, each gated by the audit → P8. P2 (Go on
the hosts) precedes P3 (this repository's `go.mod`). P4 (Flutter) is coupled to magic-git and
lands in both repositories in one working session. The Node line change (P6b) is dated: it
waits for 2026-10-28.

## Implementation Steps

### P0 — Remediate the live exposures (D10; closes F1)

**Owner-directed on 2026-09-23, ahead of this plan's approval** ("fix the windows exposures
now").

1. **Windows Python** 3.14.3 → **3.14.7**: `py install --update 3.14`, the per-user Python install
   manager. **Done 2026-09-23.** The first attempt was refused ("files are still in use"). The
   installer restored the old runtime. No process was found holding the files, and the retry
   succeeded: `python --version` gives `Python 3.14.7`, and site-packages and Scripts were
   restored. The choco `python314` package's target `C:\Python314` holds only an empty `Lib`
   folder, a stale record; the owner decides whether to `choco uninstall` it.
2. **Windows machine installs, one elevated step.**
   - **Staged 2026-09-23** in `%LOCALAPPDATA%\Temp\winfix-2026-09-23\`, each file's SHA-256
     checked against its publisher's (C3): `node-v24.21.0-x64.msi`,
     `OpenJDK21U-jdk_x64_windows_hotspot_21.0.12.1_1.msi`,
     `OpenJDK25U-jdk_x64_windows_hotspot_25.0.4.1_1.msi` and `Git-2.55.0.5-64-bit.exe`.
     The idle Gradle daemon running from the old JDK 21 was stopped.
   - `apply-elevated.ps1` (Windows PowerShell 5.1 syntax, parse-checked) does the rest. It
     re-verifies each hash and installs silently with logs. Temurin gets FeatureMain,
     FeatureEnvironment and FeatureJarFileRunWith, but **not** FeatureJavaHome. An old Temurin
     is removed only if its line's replacement is registered.
   - **The first launch's UAC prompt was declined, and nothing ran.** The step is pending the
     owner.
   - **Done 2026-09-23** on the third launch, with the owner watching for the prompt.
     `results.json`: all four hashes re-verified; msiexec exit 0 for Node, Temurin 21 and
     Temurin 25; `msiexec /x` exit 0 for the old 21.0.12.8 and 25.0.4.7; Git setup exit 0.
     Registered afterwards: Node.js 24.21.0, Temurin 21.0.12.1+1 (DisplayVersion 21.0.12.101),
     Temurin 25.0.4.1+1 (25.0.4.101), Git 2.55.0.5.
3. **Windows post-step, no elevation.**
   - Back up `HKCU\Environment` `JAVA_HOME` and `%APPDATA%\.flutter_settings`.
   - Point both at the new Temurin 21.0.12.1 directory (Temurin 25 becomes Flutter's JDK in P5,
     not here).
   - Verify with a fresh-logon environment: `node --version` = v24.21.0, ~~`java -version` =
     21.0.12.1~~ (see Deviation 1: `java` on PATH is 25.0.4.1; `%JAVA_HOME%\bin\java -version` =
     21.0.12.1), `git --version` = 2.55.0.windows.5, `flutter doctor -v` Java = 21.0.12.1.
   - **Done 2026-09-23.** Both old values (the removed `jdk-21.0.12.8-hotspot`) backed up to
     `JAVA_HOME.bak` and `flutter_settings.bak` in the staging folder; both now name
     `jdk-21.0.12.101-hotspot`. A fresh-logon environment (machine then user PATH, read from
     the registry) gives node v24.21.0, git 2.55.0.windows.5, Python 3.14.7, `JAVA_HOME` java
     21.0.12.1, and `java`/`javac` on PATH 25.0.4.1. `flutter doctor -v` is not yet run.
4. **Linux server Python** 3.14.4 → 3.14.7. First identify the interpreter behind
   `~/default-venv` (its `pyvenv.cfg` `home`), then update that interpreter by its own
   installer. The venv is recreated only if its base changed path.

**Verification:** OSV and the publishers' advisories show none for the new versions (MADR
evidence). The probe's section 3 shows the new versions. The snapshot diff lists only the
files in step 3.

#### Deviation 1 (2026-09-23): Windows `java` on PATH became 25 ahead of P5

**Found.** Before P0, the machine PATH listed `jdk-21.0.12.8-hotspot\bin` ahead of
`jdk-25.0.4.7-hotspot\bin` (the pre-P0 probe's `which -a java` resolves 21 first). The P0
reinstall put the Temurin 25.0.4.1 `bin` first. So `java` and `javac` typed in any new shell are
25.0.4.1, while step 3 expected 21.0.12.1. Nothing the plan approved moves the interactive
JDK to 25 before P5. Build paths are unaffected: Gradle and Flutter read `JAVA_HOME` and
`jdk-dir`, which both name 21.0.12.1, so MADR 0168's build JDK still holds on this host.

**Decision (owner, 2026-09-23): keep Java 25 first on the Windows PATH.** The alternative,
reordering the machine PATH so 21 precedes 25 (one more elevated step), was declined. This
takes the interactive-shell part of P5 early, on Windows only. P5 step 1 still moves
`jdk-dir`, and C4 still holds: the Temurin 21 MSI stays until the APK builds on 25.

**Scope.** No file added. The machine PATH order is a consequence of the P0 install already in
scope, not a new edit.

### P1 — The standard file and the version audit (D11, D12; closes F10)

1. `scripts/tools/devenv/standard.json` (magic-git) holds, per toolchain: `version`, `line`,
   `lts` (Node, Java), `lifecycle_source`, `advisory_source`, `verified` (date). The values are
   the MADR's decisions.
2. `scripts/tools/devenv/version_audit.py` (stdlib only, read-only, no host identifiers):
   - **Drift:** each probe output against `standard.json`, per tool and per host.
   - **Staleness:** `standard.json` against the publisher feeds used for this MADR (go.dev,
     nodejs.org index and schedule, Adoptium, the Flutter release feed, endoflife.date, and the
     GitHub and GitLab release APIs). It reports a newer stable in the same or a newer
     supported line, per D1.
   - **Advisories:** OSV for Go stdlib/toolchain, gh and glab at the standard and installed
     versions; GitHub advisories for git, Git for Windows, gh and Dart; Node's `security`
     flag; the OpenJDK advisory list.
   - Exit non-zero on any drift or advisory. Output is one line per finding.
3. **Seen to fail (C2 of the house rules):** run it against the probe outputs saved **before**
   P0 (copied aside today). It must report the five Windows exposures (Node, both Temurin
   JDKs, Git for Windows, Python) by name. Then run it against a standard file edited in a temp
   copy to name Go 1.26.5: it must report the 8 stdlib vulnerabilities. Neither fixture is
   committed.
4. README section: how to run it, and the calendar (Go point releases; the Oracle Critical
   Patch Update on the third Tuesday of Jan/Apr/Jul/Oct; Node security releases; Flutter
   hotfixes).

### P2 — Go 1.27.1 on every host (D2; closes F2, F3)

1. Per host, install go1.27.1 beside 1.26.6 (go.dev archive, sha256 per C3):
   - Windows: `~\sdk\go1.27.1`;
   - WSL: `~/sdk/go1.27.1`;
   - Linux server: mise `go = "1.27.1"`;
   - macOS: `~/.local/go1.27.1`, then repoint `~/.local/bin/go`.

   Set `GOTOOLCHAIN=go1.27.1` in each host's Go env file, and in the registry environment on
   Windows. Switch PATH entries (Windows User PATH, WSL `devenv.sh`) only on approval (C2).
2. Rebuild the 11 standard tools with go1.27.1 (`go_tools_standard.py --go <1.27.1> --apply`,
   with the built-with check changed to go1.27.1).
3. **gopls (MADR open question):** build gopls v0.23.0 with go1.27.1 and run
   `gopls check` on a scratch file declaring a generic method. If it reports a
   false error, pin the newest gopls that does not, or record the gap in the execution record
   and keep v0.23.0.
4. The acp-go-sdk checkouts' `mise.local.toml`: `go@1.27.1`, then `mise exec -- make check test`.
5. Remove go1.26.6 from a host only after P3's gates pass there (C4).

**Verification:** `go version` = go1.27.1 in a fresh shell on each host;
`go version -m ~/go/bin/*` all go1.27.1; the audit shows no Go drift.

### P3 — This repository on Go 1.27 (D2)

1. `go mod edit -go=1.27.1`, then `go mod tidy`. Record exactly what tidy rewrote (the 1.27
   require-block merge).
2. Gates: `make pre-add-check`, `make race`, `make ci-windows` (Git Bash), `govulncheck ./...`,
   and `go vet ./...`, which now includes `stdversion`.
3. Behaviour changes to check deliberately:
   - `asynctimerchan` removed: search for any code relying on buffered `time` channels.
   - `go test -json` `OutputType`: any consumer of test JSON (CI flake ledger).
4. Push on ask. CI green. If any gate fails for a reason 1.27 introduced, that is a deviation:
   stop and propose a fix. Under D2 the floor for this repository is go1.26.8 until it is fixed.

### P4 — Flutter 3.47.5 / Dart 3.13.4 (D3; closes F6)

1. Hosts: Windows and WSL `git -C ~/sdk/flutter fetch --tags && git checkout 3.47.5`, then
   `flutter --version`; Linux server mise `flutter` 3.47.5 (the dotfiles inline table's
   version field); macOS `brew upgrade --cask flutter`, verified to 3.47.5.
2. This repository: `FLUTTER_VERSION: "3.47.5"`, then `flutter pub get --enforce-lockfile`,
   `flutter analyze`, `flutter test`, `dart format --set-exit-if-changed`, `make apk` locally.
3. magic-git: `build_macos.sh` `FLUTTER_VERSION=3.47.5`. On the macOS laptop, run
   `flutter pub get --enforce-lockfile` and the full `flutter test` including the 48 goldens.
   A golden that moves is a deviation.
4. CI: push on ask, then dispatch `ci.yml` so `android-apk` runs. Both repositories land in the
   same session.

### P5 — Java 25 as the build JDK (D5; closes F5)

1. Hosts:
   - Windows: `jdk-dir` → the Temurin 25.0.4.1 installed in P0. (`java` on PATH is already
     25.0.4.1, per P0 Deviation 1.)
   - WSL: `~/sdk/jdk-25` (Adoptium tarball, sha256); `jdk-dir` and the `devenv.sh`
     `JAVA_HOME` line.
   - Linux server: mise `java = "temurin-25"`.
   - macOS: a Temurin 25 cask; `jdk-dir` to it.

   On each host, `make apk` and the assert script, from a scratch clone.
2. CI: `java-version: "25"`, push on ask, then a dispatched `android-apk` run whose setup-java
   log says 25.x.
3. Class-file check as in 0168 A2: still major 61. The 2026-09-23 trial measured this once.
4. MADR 0168: an additive amendment that D1/D3's 21 is superseded by 0169 D5, and D2 stands.
5. Remove JDK 21 per host after that host's APK passes on 25 (C4). On Windows, that is the
   Temurin 21 MSI.

### P6 — Node: Active LTS now, 26 on 2026-10-28 (D4; closes F4)

- **a (now).** 24.21.0 everywhere:
  - Windows: done in P0.
  - WSL: mise `node@24.21.0`. There is none native today, and Windows' Node leaks in only
    through the appended PATH.
  - Linux server: mise `node = "24"` → `"24.21.0"`.
  - macOS: `brew install node@24`, and unlink `node` 26, on approval.
  - CI: `NODE_VERSION: "24"` already resolves the newest 24.x. Keep it.
- **b (2026-10-28 or later).** When nodejs.org marks 26 `lts`, repeat a with the newest
  26.x, and set CI to `"26"`. The audit reports the moment it becomes due.

### P7 — Python 3.14.7, git 2.55.x, gh 2.101.0, glab 1.119.0 (D6–D9; closes F7, F8, F9)

1. **Python:** WSL gets a mise `python@3.14.7` for developer use, leaving the system 3.12 alone.
   macOS is already 3.14.7. Windows and the Linux server are handled in P0.
2. **git:**
   - WSL: the owner runs `sudo add-apt-repository ppa:git-core/ppa && sudo apt install git`,
     then `git --version` must be 2.55.x.
   - Linux server: identify how `~/.local/bin/git` 2.53.0 was built, and rebuild or replace
     it with 2.55.0 the same way.
   - macOS: Homebrew 2.55.0 already. Windows: done in P0.
3. **gh 2.101.0 / glab 1.119.0**, from release archives checksummed against the published
   checksums file:
   - Windows `~\toolchains\{gh,glab}`, replacing in place;
   - WSL `~/.local/bin` (new);
   - Linux server: `~/.local/bin/gh`, and mise `glab = "latest"` → `"1.119.0"`;
   - macOS: `brew upgrade gh glab`.

   `gh auth status` and `glab auth status` must still pass after each replacement.

### P8 — Close out

A re-run probe and a green audit on all four hosts. Then the execution record, and this plan's
status.

## Verification (whole plan)

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | The five Windows exposures and the server's Python are remediated; the audit shows no advisory | D10, F1 |
| A2 | `version_audit.py` was seen failing on the pre-P0 probe outputs, naming each exposure, and on a Go 1.26.5 standard, naming its 8 vulnerabilities | D12 |
| A3 | `standard.json` exists and the audit reads it; zero drift on all four hosts | D11 |
| A4 | go1.27.1 on all hosts; tools built by it; gopls question answered | D2, F3 |
| A5 | This repository on `go 1.27.1`, all gates green locally and in CI | D2 |
| A6 | Flutter 3.47.5 on hosts, in CI and in magic-git; goldens unmoved (or a deviation raised) | D3 |
| A7 | Temurin 25 builds the APK on every host and in a dispatched CI run; bytecode 61 | D5 |
| A8 | Node 24.21.0 everywhere, then 26.x after 2026-10-28 | D4 |
| A9 | Python 3.14.7, git 2.55.x, gh 2.101.0, glab 1.119.0 everywhere; auth intact | D6–D9 |
| A10 | Every host edit made with a backup and approval; the snapshot diff lists only the intended files | C2 |

The criterion most likely to be dropped quietly is **A2**. The audit will be written against
the fixed hosts, and it will pass. Only running it against the saved pre-P0 outputs shows that
it can catch what it exists to catch.

## Rollout and Rollback

Hosts go one at a time within each phase. Rollback per host: restore the dated backup (shell
init, registry environment, Flutter settings, Go env), `mise use` the previous version, or
reinstall the previous MSI from the staging folder. This repository: revert the phase's
commit. Old toolchains stay installed until C4 is met, so every rollback is a pointer change,
not a reinstall.

## Deferred (named, so they are not mistaken for oversights)

- **Moving other repositories' `go` directives to 1.27**: their own gates and records.
- **Android cmdline-tools 19.0 → current**: the MADR's open question. It needs its own check of
  the Windows `sdkmanager` NDK-path defect.
- **WSL `appendWindowsPath`**: the owner keeps WSL untracked, and the PATH leak is a separate
  decision.
- **The choco `python`/`python3`/`python314` records**: harmless and stale; uninstall at the
  owner's discretion.
