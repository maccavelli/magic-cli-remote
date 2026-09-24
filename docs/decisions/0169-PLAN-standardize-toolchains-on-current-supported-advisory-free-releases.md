---
status: proposed
date: 2026-09-23
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0169 — Standardize every toolchain on its newest supported, advisory-free stable release

Implements [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md)
decisions D1–D12, closing findings F1–F10, and the mise-retirement amendment's D13–D15 (P9).

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
`0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md` (P5); the status line and
an additive amendment of `docs/spec/0114-MADR-manage-markdownlint-cli2-with-mise.md` (P9, D15).

**magic-git** (the owner commits there): `scripts/tools/devenv/standard.json` (new),
`scripts/tools/devenv/version_audit.py` (new), `scripts/tools/devenv/fork_tools.py` (new, P9),
`scripts/tools/devenv/README.md`,
`build_macos.sh` (`FLUTTER_VERSION` only), and golden files **only** if P4's re-run shows
Flutter 3.47.5 moved them. That last case is a deviation to raise, not to absorb.

**Host configuration.** Each change is approved per host (C2), made with a dated backup, and
made through that host's own source:

| Host | Files and installs |
| --- | --- |
| Windows | machine installs: Node, Temurin 21/25, Git for Windows (elevated); `HKCU\Environment` `JAVA_HOME`; `%APPDATA%\.flutter_settings`; `~\sdk\go1.27.1`, `~\sdk\flutter` (git checkout of a tag); `~\toolchains\gh`, `~\toolchains\glab`; `~\go\bin`; `%APPDATA%\go\env` (`GOTOOLCHAIN`) |
| WSL | `~/sdk/go1.27.1`, `~/sdk/jdk-25`, `~/sdk/flutter`, `~/sdk/node-v24.21.0`, `~/sdk/python-3.14.7`; `~/.config/devenv.sh` (the Go path and `JAVA_HOME` lines; P9: the mise shims line and its comment); `~/.config/flutter/settings`; `~/.config/go/env`; `~/.local/bin/{gh,glab}`; ~~mise global config for node and python~~ (P9); apt: the git-core PPA (owner runs `sudo`); P9: mise's binary and directories (removed), the fork checkout's `.tools/`, `mise.local.toml` (deleted) and `.git/info/exclude` |
| Linux server | ~~dotfiles `mise/.config/mise/config.toml` (go, java, node, python, flutter, glab lines; the `[env]` `_.python.venv` line, Deviation 2)~~ (Deviation 4); `~/sdk/*` (P9 and later phases); `~/sdk/python-3.14.7` and `~/default-venv` (recreated, Deviations 2 and 4); `~/.config/go/env`; `~/.local/bin/{gh,glab,just,ninja}`; ~~its `~/.local/bin/git` build~~ (a symlink to Ubuntu's git, see P7); `~/go/bin`; `~/.local/lib/node_modules` (markdownlint-cli2); the Flutter settings file; P9's dotfiles files (listed in P9d step 4); mise's binary and directories and the old `~/.local/go` (removed, P9d step 7); the fork checkout's `.tools/`, `mise.local.toml` (deleted) and `.git/info/exclude` |
| macOS laptop | Homebrew formulae and casks (gh, glab, node@24, python@3.14, flutter, a Temurin 25 cask); `~/.local/go1.27.1` and the `~/.local/bin/go` link; `~/.config/flutter/settings`; `~/Library/Application Support/go/env`; `~/go/bin`; P9: mise's binary and directories (removed), the fork checkout's `.tools/`, `mise.local.toml` (deleted) and `.git/info/exclude` |

### Out of scope

- **Other repositories' `go.mod` files** (the mcp-server-*, mcplib, ocp-*, elastic-toolbox-utility
  repos). A 1.27.1 toolchain builds modules that declare 1.26. Each repository moves its `go`
  directive through its own gates, not this plan.
- **The Android bytecode target**, which stays Java 17 (MADR 0168 D2).
- **Shell-init restructuring** on any host. Only the listed lines change, and only with approval.
- ~~**Rust** in the acp-go-sdk checkout's `mise.toml` (MADR Open questions).~~ Now in scope as a
  fork-local tool (P9, D14). Rust as a host toolchain remains out of scope.
- **Upstream's `mise.toml` and `mise.lock` in the acp-go-sdk fork.** They are upstream's files.
  D14 reads them and never edits them.

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

P0 → P1 → **P9 (retire mise; numbered last because it was added last, but it runs before P2–P7,
whose Linux-server steps assume `~/sdk`)** → then P2, P3, P4, P5, P6, P7 in any order, each gated by the audit → P8. P2 (Go on
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
   `~/default-venv` (its `pyvenv.cfg` `home`), ~~then update that interpreter by its own
   installer. The venv is recreated only if its base changed path.~~ The interpreter is
   Ubuntu's system `python3.14` and cannot be moved to 3.14.7 (Deviation 2). ~~Instead:~~
   - ~~Back up the dotfiles `mise/.config/mise/config.toml` (dated, outside the repository).~~
     Backup and freeze **done 2026-09-23**, in `~/backups/0169-p0-2026-09-23/`: the mise config,
     `default-venv.freeze.txt` (85 packages) and `pyvenv.cfg`.
   - ~~Prove the change first with a throwaway mise config: `python3` and `pip` must resolve into
     `~/default-venv` in an interactive shell, in a non-interactive `bash -c`, and through the
     mise shims. Any other result stops the step.~~ Run 2026-09-23: interactive shell passed,
     shims and non-interactive failed. The step stopped (Deviation 4).
   - ~~Save `pip freeze` of the current venv, rename it `~/default-venv.bak-2026-09-23`.~~
   - ~~Add `python = "3.14.7"` and `[env] _.python.venv = { path = "~/default-venv" }` to the
     config; `mise install`; create `~/default-venv` on mise's 3.14.7; reinstall the frozen
     list.~~
   - ~~Verify: `~/default-venv/bin/python --version` = 3.14.7, `pyvenv.cfg` `home` under mise's
     installs, the same package set as the freeze, and the three resolution checks above. The
     `.bak` venv is deleted only after that (C4). `/usr/bin/python3.14` stays Ubuntu's (D6).~~

   **As amended by Deviation 4 (no mise, no PATH change):**
   - Download `cpython-3.14.7+20260901-x86_64-unknown-linux-gnu-install_only.tar.gz` from
     python-build-standalone and check it against its GitHub asset digest (C3). Unpack it to
     `~/sdk/python-3.14.7`.
   - Rename `~/default-venv` to `~/default-venv.bak-2026-09-23`. Create `~/default-venv` with
     `~/sdk/python-3.14.7/bin/python3 -m venv`, then `pip install -r` the saved freeze.
   - Verify:
     - `~/default-venv/bin/python --version` gives 3.14.7, and `pyvenv.cfg` `home` is
       `~/sdk/python-3.14.7/bin`.
     - `pip freeze --all` equals the saved list, apart from pip's own version line.
     - `python3` and `pip` resolve to `~/default-venv/bin` in `bash -lic`, in `bash -c`
       (BASH_ENV), and under the `mcremote` drop-in PATH. That PATH has no venv entry today,
       so there `python3` must stay `/usr/bin/python3`, unchanged.
     - `~/sdk/python-3.14.7/bin` is on no PATH.
   - The `.bak` venv is deleted only after that (C4). `/usr/bin/python3.14` stays Ubuntu's (D6).
   - **Done 2026-09-23.** The archive (119 MiB) matched the published sha256.
     - The venv is Python 3.14.7, with `home = ~/sdk/python-3.14.7/bin`.
     - 85 packages installed, none missing and none extra against the freeze. pip is 26.2.1,
       not the old pin 25.1.1, which is older than the CVE-2025-8869 fix.
     - `bash -lic` and `bash -c` (BASH_ENV) resolve `python3` and `pip` to the venv. The
       drop-in PATH without BASH_ENV resolves `/usr/bin/python3`, as before.
     - `~/sdk/python-3.14.7` is on no PATH.
     - The `.bak` venv was then removed.

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

#### Deviation 2 (2026-09-23): the Linux server's Python is Ubuntu's, and apt has no 3.14.7

**Found.** `~/default-venv/pyvenv.cfg` says `home = /usr/bin`, `version = 3.14.4`. The
interpreter is the Ubuntu 26.04 package `python3.14-minimal` `3.14.4-1ubuntu0.2`, the newest
candidate in `resolute-security`. There is no separately installed 3.14.4 to update, which step
4 assumed. Its apt changelog lists 11 backported CVEs. Checking the PSF advisory database's fix
commits against the `v3.14.4` and `v3.14.7` tags (GitHub compare API) gives 21 advisories that
affect 3.14.4 and are fixed in 3.14.7. Ubuntu has not backported 10 of them: CVE-2025-15366,
CVE-2026-0864, -3087, -3298, -6879, -7210, -11940, -11972, -12003, -18503.

**Also found while resolving it.** Adding `python` to the server's global mise config alone
would move `python3` and `pip` off the venv: `mise activate` puts mise's install directories,
and `00-paths.sh` puts its shims, ahead of `~/default-venv/bin`.

**Decision (owner, 2026-09-23): a mise-managed Python 3.14.7 as the server's developer Python,
with mise activating `~/default-venv` itself** (step 4 as amended). The alternative, keeping
Ubuntu's build and waiting for its backports, was declined. So was installing 3.14.7 through
mise with no config entry: nothing would record it, and `mise prune` would delete the
interpreter under the venv.

**Scope.** Added: `~/default-venv` (recreated) on the Linux server. The dotfiles mise config
was already in scope; its `[env]` line is new.

#### Deviation 3 (2026-09-23): no Python 3.14 release is advisory-free

**Found** by the same check. There is no 3.14.8 (python.org release API, cpython tags). 7
published advisories affect 3.14.7:

| Advisory | State upstream on 2026-09-23 |
| --- | --- |
| CVE-2026-15806 (urllib `HTTPPasswordMgr` scheme), CVE-2026-17084 (stringprep), CVE-2026-15310 (zipfile decompression size) | fix merged on the 3.14 branch, not yet released |
| CVE-2026-87910 (tarfile link fallback), CVE-2025-15367 (poplib) | fixed on main and other branches; no 3.14 fix commit listed |
| CVE-2026-19672 (tarfile filter containment), CVE-2024-3220 (Windows `mimetypes`) | no fix commit |

This contradicts MADR D6's premise, so the MADR carries an amendment. The Goal's "no known
advisory" cannot hold for Python until a fixing release ships.

**Decision (owner, 2026-09-23): stay on 3.14.7 and roll forward to 3.14.8 when it ships.** P1's
audit reports "known advisory, no fixing release" as its own finding state, listing each one,
never as a pass.

**Scope.** No file added. P1's audit gains the finding state.

#### Deviation 4 (2026-09-23): mise's shims bypass `_.python.venv`; the owner retires mise

**Found.** The throwaway trial for step 4 kept everything isolated: `MISE_DATA_DIR`, cache,
state and global config in a temp directory, and a temp venv, all removed afterwards. It
installed 3.14.7 and ran three checks:

- **Interactive `bash -ic`: pass.** `python3`, `pip`, `sys.prefix` and pip's location were all
  the venv.
- **The shim `python3` and `pip`: fail.** They ran mise's bare interpreter
  (`sys.prefix` = mise's `installs/python/3.14.7`), and pip was that interpreter's pip.
- **Non-interactive `bash -c` with the shims first on PATH: fail**, the same way.

The server's `00-paths.sh` and its `mcremote` drop-in both put the shims above
`~/default-venv/bin`. So those contexts would have run the bare interpreter, and `pip install`
there would have gone outside the venv. This is also the first time the resolution check was
seen to fail, so it is known to catch this problem.

**Decision (owner, 2026-09-23): retire mise on every host, folded into this record** (MADR
amendment D13–D15, phase P9). Step 4 installs Python from a python-build-standalone archive
instead, which needs no PATH change. Three alternatives were declined:

- a directory-scoped mise config;
- moving the venv above the shims in `00-paths.sh` and the drop-in;
- `uv python install --no-bin`.

**Scope.** Added: `~/sdk/python-3.14.7` on the Linux server. Removed from step 4: the dotfiles
mise config edit.

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
   - **Known advisory, no fixing release** (added by Deviation 3): an advisory that affects the
     standard version when no newer supported release fixes it. Reported per advisory, with its
     own exit code, so it is never mistaken for a pass. The Python advisories for 3.14.7 are the
     first case, and the PSF advisory database joins the advisory sources.
   - **mise present** (added by D13): a mise binary, a mise data directory, or a shims directory
     on a probed PATH is drift once P9 has run on that host.
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
   - Linux server: ~~mise `go = "1.27.1"`~~ `~/sdk/go1.27.1`, and the `00-paths.sh` line and the
     drop-in PATH entry (D13);
   - macOS: `~/.local/go1.27.1`, then repoint `~/.local/bin/go`.

   Set `GOTOOLCHAIN=go1.27.1` in each host's Go env file, and in the registry environment on
   Windows. Switch PATH entries (Windows User PATH, WSL `devenv.sh`) only on approval (C2).
2. Rebuild the 11 standard tools with go1.27.1 (`go_tools_standard.py --go <1.27.1> --apply`,
   with the built-with check changed to go1.27.1).
3. **gopls (MADR open question):** build gopls v0.23.0 with go1.27.1 and run
   `gopls check` on a scratch file declaring a generic method. If it reports a
   false error, pin the newest gopls that does not, or record the gap in the execution record
   and keep v0.23.0.
4. ~~The acp-go-sdk checkouts' `mise.local.toml`: `go@1.27.1`, then `mise exec -- make check test`.~~
   The acp-go-sdk checkouts use the host's go1.27.1 (D14): `PATH="$PWD/.tools/bin:$PATH" make check test`.
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
   `flutter --version`; Linux server ~~mise `flutter` 3.47.5 (the dotfiles inline table's
   version field)~~ the 3.47.5 archive from the releases JSON (sha256) replacing `~/sdk/flutter`
   (D13); macOS `brew upgrade --cask flutter`, verified to 3.47.5.
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
   - Linux server: ~~mise `java = "temurin-25"`~~ `~/sdk/jdk-25` (Adoptium tarball, sha256);
     `jdk-dir`, the `JAVA_HOME` lines and the PATH entries (D13).
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
  - WSL: ~~mise `node@24.21.0`~~ `~/sdk/node-v24.21.0` (SHASUMS256) and a `devenv.sh` PATH line
    (D13). There is none native today, and Windows' Node leaks in only through the appended
    PATH.
  - Linux server: ~~mise `node = "24"` → `"24.21.0"`~~ `~/sdk/node-v24.21.0` replacing P9's
    `~/sdk/node-v22.23.2`; reinstall markdownlint-cli2 under the new node (D15).
  - macOS: `brew install node@24`, and unlink `node` 26, on approval.
  - CI: `NODE_VERSION: "24"` already resolves the newest 24.x. Keep it.
- **b (2026-10-28 or later).** When nodejs.org marks 26 `lts`, repeat a with the newest
  26.x, and set CI to `"26"`. The audit reports the moment it becomes due.

### P7 — Python 3.14.7, git 2.55.x, gh 2.101.0, glab 1.119.0 (D6–D9; closes F7, F8, F9)

1. **Python:** WSL gets ~~a mise `python@3.14.7`~~ the python-build-standalone 3.14.7 archive
   in `~/sdk/python-3.14.7` for developer use (D13), leaving the system 3.12 alone.
   macOS is already 3.14.7. Windows and the Linux server are handled in P0.
2. **git:**
   - WSL: the owner runs `sudo add-apt-repository ppa:git-core/ppa && sudo apt install git`,
     then `git --version` must be 2.55.x.
   - Linux server: ~~identify how `~/.local/bin/git` 2.53.0 was built, and rebuild or replace
     it with 2.55.0 the same way.~~ `~/.local/bin/git` is a symlink to Ubuntu's `/usr/bin/git`
     2.53.0 (found 2026-09-23). Use the git-core PPA as on WSL; the owner runs `sudo`.
   - macOS: Homebrew 2.55.0 already. Windows: done in P0.
3. **gh 2.101.0 / glab 1.119.0**, from release archives checksummed against the published
   checksums file:
   - Windows `~\toolchains\{gh,glab}`, replacing in place;
   - WSL `~/.local/bin` (new);
   - Linux server: `~/.local/bin/gh`, and ~~mise `glab = "latest"` → `"1.119.0"`~~
     `~/.local/bin/glab` (placed there by P9);
   - macOS: `brew upgrade gh glab`.

   `gh auth status` and `glab auth status` must still pass after each replacement.

### P8 — Close out

A re-run probe and a green audit on all four hosts. Then the execution record, and this plan's
status.

### P9 — Retire mise (D13, D14, D15; added 2026-09-23 by Deviation 4)

Runs after P1 and before P2–P7. Hosts go in the order 9a → 9e. Retirement is like-for-like: no
tool's version changes in this phase. On each host, mise's data is removed only after that
host's verification passes (C4).

**9a — `fork_tools.py` (magic-git `scripts/tools/devenv/`, stdlib only).**

1. It reads the checkout's `mise.toml` `[tools]` table with `tomllib` and installs each entry
   into `<checkout>/.tools`. Unknown entries fail loudly.

   | `mise.toml` entry | Installer (checksum source) |
   | --- | --- |
   | `go` | not installed: the host's Go is used, as its `GOTOOLCHAIN` directs |
   | `go:golang.org/x/tools/gopls`, `gofumpt`, `golangci-lint`, `actionlint` | `GOBIN=.tools/bin go install <module>@v<version>` (sum.golang.org) |
   | `aqua:numtide/treefmt`, `uv` | release asset for the host's OS and architecture (GitHub asset digest) |
   | `pipx:mdformat`, `zizmor` | `uv tool install <pkg>==<version>`, with `UV_TOOL_DIR` and `UV_TOOL_BIN_DIR` under `.tools` (PyPI hashes) |
   | `rust` | `rustup-init` (its published `.sha256`), with `RUSTUP_HOME` and `CARGO_HOME` under `.tools/rust`, `--profile minimal --no-modify-path` |
   | `cargo:mdsh` | `cargo install mdsh --version <version> --locked --root .tools` (crates.io checksums) |

2. It adds `.tools/` to `.git/info/exclude`, never to the tracked `.gitignore`. It is
   idempotent, and it prints each tool's `--version` next to its pin.
3. **Seen to fail, on scratch copies:**
   - a copy of `mise.toml` with a nonexistent version must make it exit non-zero, naming the
     tool;
   - a copy with an unknown entry must do the same;
   - in a scratch clone of the fork, an unformatted Markdown file must make
     `PATH=.tools/bin:$PATH make check` fail.

**9b — WSL.**

1. Back up `~/.config/devenv.sh` (dated).
2. Run `fork_tools.py` in `~/gitrepos/acp-go-sdk`, then delete its untracked `mise.local.toml`.
3. `devenv.sh`: remove the mise shims `export` and its two comment lines.
4. **Verify:**
   - A fresh `bash -lic` resolves `go` to `~/sdk/go1.26.6/bin/go`, and nothing on PATH
     contains `mise`.
   - In the fork, `PATH="$PWD/.tools/bin:$PATH" make check test` passes, and every tool
     version equals its pin.
   - The probe's snapshot diff lists only `devenv.sh`.
5. Remove `~/.local/bin/mise`, `~/.local/share/mise`, `~/.local/state/mise`, `~/.cache/mise` and
   `~/.config/mise` if present.

**9c — macOS.**

1. Run `fork_tools.py` in `~/gitrepos/acp-go-sdk` with Homebrew's `python3`, since
   `/usr/bin/python3` 3.9 has no `tomllib`. Delete `mise.local.toml`.
2. **Verify:** `PATH="$PWD/.tools/bin:$PATH" make check test` passes, with versions equal to the
   pins.
3. Remove `~/.local/bin/mise` and the mise directories. No shell init changes, because mise was
   never activated on this host.

**9d — Linux server.**

1. Take backups, dated, in `~/backups/0169-p9-<date>/`:
   - every dotfiles file in step 4;
   - `~/.config/go/env` and the Flutter settings file;
   - the probe snapshot.
2. Install like-for-like, each archive checked against its publisher's checksum (C3):

   | Tool | Version | Source (checksum) | Location |
   | --- | --- | --- | --- |
   | Go | 1.26.6 | go.dev (`sha256`) | `~/sdk/go1.26.6` |
   | Flutter | 3.47.2 | releases JSON (`sha256`) | `~/sdk/flutter` |
   | Temurin | 21.0.12.1 | Adoptium API (`checksum`) | `~/sdk/jdk-21` |
   | Node | 22.23.2 | nodejs.org (`SHASUMS256.txt`) | `~/sdk/node-v22.23.2` |
   | cmake | 4.4.2 | Kitware (`SHA-256.txt`) | `~/sdk/cmake-4.4.2` |
   | protoc | 35.1 | GitHub asset digest | `~/sdk/protoc-35.1` (`bin`, `include`) |
   | just | 1.58.0 | `SHA256SUMS` | `~/.local/bin/just` |
   | ninja | 1.13.2 | GitHub asset digest | `~/.local/bin/ninja` |
   | glab | 1.113.0 | `checksums.txt` | `~/.local/bin/glab` |
   | Rust | 1.96.1 | the existing standalone rustup | `rustup default 1.96.1` |
   | markdownlint-cli2 | 0.23.2 | npm registry integrity (D15) | `npm install -g --prefix ~/.local` |

   Also `flutter config --jdk-dir ~/sdk/jdk-21`, keeping the Android SDK path.
3. Run `fork_tools.py` in the server's acp-go-sdk checkout, and delete `mise.local.toml`.
4. **Dotfiles edits. These lines only:**
   - `bash/.bashrc.d/00-paths.sh`:
     - drop the shims `path_prepend` and its three comment lines;
     - `~/.local/go/bin` becomes `~/sdk/go1.26.6/bin`;
     - add `~/sdk/flutter/bin`, `~/sdk/jdk-21/bin`, `~/sdk/node-v22.23.2/bin`,
       `~/sdk/cmake-4.4.2/bin`, `~/sdk/protoc-35.1/bin` and `~/.cargo/bin`.
   - `bash/.bashrc.d/05-env.sh`: the two mise comment lines become one toolchain comment; add
     `export JAVA_HOME="$HOME/sdk/jdk-21"`.
   - `bash/.bashrc.d/40-completions.sh`: remove the mise completion line and its comment.
   - `bash/.bashrc.d/50-tools.sh`: remove the `mise activate` line.
   - `config/environment.d/11-tool-env.conf`: the mise comment line; add a `JAVA_HOME` literal.
   - `config/systemd/user/mcremote.service.d/path.conf`:
     - re-derive the PATH literal from the new `00-paths.sh` order, with no shims;
     - drop the two entries that do not exist on this host (`/opt/homebrew/bin`,
       `~/.local/flutter/bin`);
     - update the comments.
   - `agent-hooks/.global-agent-hooks/pre-add-go.sh` lines 61 and 71: change the
     `mise install` hint to the `go install` command.
   - `README.md`: remove the mise row. `bin/apply`: remove `mise` from the stow list. Then
     `stow -D mise`, and `git rm -r mise/`.
5. Run `systemctl --user daemon-reload`. **Restart `mcremote` only when the owner says so**,
   because a restart ends live agent sessions.
6. **Verify:**
   - `bash -lic`, `bash -c` (BASH_ENV), and a transient `systemd-run --user --wait --pipe` with
     the drop-in's PATH and BASH_ENV all resolve every tool in the table to its new location
     and version. No resolved path contains `mise`.
   - After the restart, `systemctl --user show mcremote -p Environment` shows the new PATH, and
     from an agent session `bash -c 'command -v glab node flutter dart java cargo just protoc
     cmake ninja'` resolves all ten.
   - `flutter doctor -v` passes.
   - This repository's `make preflight` passes on the server.
   - The fork passes `PATH="$PWD/.tools/bin:$PATH" make check test`.
   - The probe's snapshot diff lists only step 4's files and the new `~/sdk` entries.
7. After the step 6 checks pass (C4), remove:
   - `~/.local/bin/mise`, and `~/.local/share/mise` (7.2 GB);
   - `~/.local/state/mise` and `~/.cache/mise`;
   - the old `~/.local/go` (go1.26.6, superseded by `~/sdk/go1.26.6`).

   Commit the dotfiles repository with `git commit --no-edit`. Push only on ask.

**9e — This repository.**

1. `docs/spec/0114-MADR-manage-markdownlint-cli2-with-mise.md`: set the status to
   `superseded by 0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md`,
   and add an additive amendment naming D15.
2. Commit the docs alone.

#### Deviation 5 (2026-09-23): mise symlinks in the Linux server's `~/.local/bin`

**Found** during 9d, before any edit. `~/.local/bin/go` and `~/.local/bin/gofmt` are symlinks into
mise's `installs/go/1.26.6`, created 2026-09-23 13:54. `~/.local/bin/glab` points into mise's
`installs/glab/latest`. `00-paths.sh` puts `~/.local/bin` first, so once mise's directories are
removed these would dangle ahead of `~/sdk/go1.26.6/bin`. `glab` is already in scope: 9d step 2
replaced the link with the verified 1.113.0 binary. `go` and `gofmt` were not in the file list.
The other links there (golangci-lint, gopls, govulncheck, staticcheck into `~/go/bin`; git into
`/usr/bin`) resolve and are left alone. Their targets are recorded in
`~/backups/0169-p9-2026-09-23/symlinks.json`.

**Decision (owner, 2026-09-23): remove the dangling symlinks.** `~/.local/bin/go` and
`~/.local/bin/gofmt` are deleted in step 4. `~/sdk/go1.26.6/bin` on PATH replaces them. After 9d
step 7, any remaining symlink in `~/.local/bin` whose target is missing is also removed.

**Scope.** Added: `~/.local/bin/go` and `~/.local/bin/gofmt` (deleted), and any other symlink in
`~/.local/bin` left dangling by step 7.

#### P9 progress (2026-09-23/24)

- **9a done.** `fork_tools.py` was seen failing on four scratch copies: a bad pin, an unknown
  entry, an unsupported backend and a wrong checksum. The unsupported-backend case first failed
  for the wrong reason, because it hit "no host go" before validating the entries. An up-front
  validation pass fixed that.
- **9b (WSL) and 9c (macOS) done.**
  - The fork's `make check test` passes through `.tools`, at upstream's pins.
  - An unformatted README in a scratch clone fails `make check`.
  - mise is removed: 533 MiB on WSL, 480 MiB on macOS.
- **9d steps 1–5 done**, and step 6 in part:
  - All 16 tools resolve at their previous versions from `~/sdk` or `~/.local/bin` in
    `bash -lic`, `bash -c` with BASH_ENV, and a `systemd-run --user` unit carrying the
    drop-in's environment.
  - The same check against the backed-up drop-in fails: glab, just, ninja, markdownlint-cli2
    and golangci-lint resolve through mise's shims.
  - `flutter doctor` reports Android on `~/sdk/jdk-21`.
  - The fork's `make check test` passes.
- **The drop-in, as executed.** It keeps its previous entries and order, and replaces only the
  mise entries. Re-deriving it from the new `00-paths.sh`, as written, would have added
  `~/default-venv/bin` to the daemon's own PATH and changed its `python3`, which P0 step 4
  verified must stay `/usr/bin/python3`.
- **The `mcremote` restart was not a deliberate step.** It happened at 02:03:03 UTC, when
  `install-binary_test.sh` drove the live unit (Deviation 7). The owner had not yet approved a
  restart. The daemon came back healthy under the new drop-in: its `/proc` environment has
  the `~/sdk` PATH, `JAVA_HOME` and `BASH_ENV`.
- **Pending:** `make preflight` green (Deviations 6 and 7, fixed by P10); step 7, removing
  mise's data (C4 holds it until the preflight passes); the dotfiles commit; 9e.

#### Deviation 6 (2026-09-24): `make preflight` fails at staticcheck on every host

**Found.** The server's preflight stopped at `staticcheck ./...`. There are 43 findings on
`master`, and 42 on the server's older checkout both under mise's Go and under `~/sdk`'s Go.
So they predate P9. CI does not run staticcheck. MADR amendment F14, F16–F20 has the detail.

**Decision (owner, 2026-09-24): fix it, under this record.** MADR D18–D21, implemented by P10.
Every other preflight step passed on the server when run individually: `go test -race`,
`verify-units`, the release build, the Flutter pin, `dart format`, `flutter analyze`, and
`flutter test` (1416 tests).

**Scope.** P10's file list.

#### Deviation 7 (2026-09-24): `install-binary_test.sh` drives the live user service

**Found.** Running it on the server restarted the live `mcremote`, and the test still failed.
Driving it, and the script under test, on three hosts showed three more things:

- the test is not hermetic (F11) and its verdict depends on the host (F12);
- `make install` never restarts the service when the user manager is `degraded` (F13, a
  production bug);
- `install_test.sh` fails 3 of its 139 cases on WSL (F15).

**Decision (owner, 2026-09-24): fix it, under this record.** MADR D16–D18, implemented by P10.

**Scope.** P10's file list.

**Verification (whole phase):** the audit reports no mise on any host. `command -v mise` finds
nothing on each host. The fork's `make check test` passes through `.tools` on WSL, macOS and
the Linux server. Every step above that must fail was seen to fail.

### P10 — Make `make preflight` true: hermetic install tests, the degraded-manager fix, the staticcheck findings, and CI parity (D16–D21; closes F11–F22; added 2026-09-24 by Deviations 6 and 7)

Runs before 9d step 7. It adds these files to the in-scope list, and no others:

- `scripts/install-binary.sh`, `scripts/install-binary_test.sh`, `scripts/install_test.sh`
- `Makefile` (the `STATICCHECK_VERSION` variable, the `staticcheck` target, the preflight
  staticcheck line, and the gate-for-gate comment)
- `.github/workflows/ci.yml` (three steps in the `go` job)
- `internal/provider/codex/{execution,managed_daemon,projects,runtime,session,threads,transport,ws_auth,store_reality}.go`
- `internal/ws/codex_handlers.go`
- `internal/providerauth/store.go`
- `internal/provider/launch/{launch,launch_windows}.go`
- `internal/appdirs/security_windows.go`
- `internal/provider/acpagent/rewind_test.go`, `internal/receipt/jws_test.go`
- `internal/cli/doctor.go` and a new or extended doctor test
- tests next to the Go changes, where an existing test asserts a changed error string

It also adds, on the Linux server only, a `git pull --ff-only` of its `magic-cli-remote`
checkout, so preflight runs the fixed tree.

1. **Hermetic install tests (D17), written before the fix.**
   - `install-binary_test.sh` gets a PATH made only of its stub directory plus the few
     coreutils it needs, linked in as `install_test.sh` does. It gets a stub `systemctl`, and
     an entry guard that exits 2 if `command -v systemctl` resolves outside the stub directory.
   - New Linux cases: a running unit is stopped, swapped and started; a `degraded` manager
     still restarts; an enabled-but-stopped unit is healed.
   - `install_test.sh` cases 22c, 24 and 25 set `MC_TEST_OSRELEASE`.
   - **Seen to fail, on scratch copies:**
     - the guard, with the real `systemctl` put first on PATH (it must exit 2 and call
       nothing);
     - the degraded case, against today's `install-binary.sh` (no stop, no start);
     - `install_test.sh` on WSL before the seam fix (3 failures).
2. **D16 in `install-binary.sh`.** `detect_service` uses `install.sh`'s rule: `XDG_RUNTIME_DIR`
   exists, and `is-system-running` or `show-environment` succeeds. The degraded case then
   passes.
   - Drive it again on WSL with the transient-unit probe: the MainPID must change.
3. **Pinned staticcheck (D19).**
   - `STATICCHECK_VERSION = v0.8.1`. The `staticcheck` target runs
     `go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...` once for each of
     GOOS linux, darwin and windows. Preflight calls the target.
   - **Seen to fail** on today's tree: 43 findings.
4. **Fix the findings (D20),** grouped as ST1005 (sentinels plus lower case), dead code, the
   `maxCommandLineBatch` move, and SA1019. Then `make staticcheck` must report 0.
   - `go test -race ./...`, `CGO_ENABLED=0 go test ./...` and `make ci-windows` (Git Bash)
     all pass.
   - Search `apps/mobile` again for any changed text: expected none.
5. **Doctor tells the truth (D21).**
   - A doctor test pins the codex line for each `StoreReality`. **Seen to fail** against
     today's doctor, which prints only the path.
   - Then wire `ObserveCredentialStore` (bounded by the existing timeout) and
     `describeReality` into `internal/cli/doctor.go`.
   - Drive `mcremote doctor` from a fresh build on Windows and on the Linux server.
6. **CI parity (D18).** The `go` job gains three steps: `make staticcheck`,
   `bash scripts/install-binary_test.sh` and `sh scripts/install_test.sh`.
   - The Makefile's gate-for-gate comment then holds as written, so it needs no edit.
   - Push on ask, and a dispatched `ci.yml` run must show all three steps green.
7. **Verification across hosts:**
   - `bash scripts/install-binary_test.sh` passes on macOS, WSL and the Linux server. On the
     server, `systemctl --user show mcremote -p ActiveEnterTimestamp` is unchanged across the
     run.
   - `sh scripts/install_test.sh` gives 139 passed on all three.
   - `make preflight` is green on the Linux server, after the `--ff-only` pull.
   - `make race` and `make ci-windows` pass on Windows.

Commit discipline: one commit for the tests and the D16 fix, one for staticcheck (pin plus
findings), one for doctor, and one for CI. Each passes `make pre-add-check` first. Push and
dispatch only on ask.

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
| A11 | No mise binary, data directory, activation line or shims PATH entry on any host; the audit reports mise as drift, and was seen doing so on a pre-P9 probe output | D13 |
| A12 | On the Linux server, every tool mise supplied resolves at its previous version from `~/sdk` or `~/.local/bin`, in login shells, `bash -c` and the `mcremote` unit, and the agents reach all ten tools the drop-in exists for | D13 |
| A13 | The acp-go-sdk fork passes `make check test` through `.tools` on WSL, macOS and the Linux server, at upstream's pinned versions; `fork_tools.py` was seen failing on a bad pin and an unknown entry | D14 |
| A14 | markdownlint-cli2 0.23.2 resolves from `~/.local/bin` on the Linux server; MADR 0114 marked superseded | D15 |
| A15 | `install-binary_test.sh` and `install_test.sh` pass on macOS, WSL and the Linux server, touch no real service manager (the live unit's start time is unchanged), and cover the Linux branch including a degraded manager; each new case was seen failing | D16, D17 |
| A16 | `make staticcheck` (pinned v0.8.1, three GOOS) reports 0 findings, and was seen reporting 43; no suppression added | D19, D20 |
| A17 | `mcremote doctor` names the codex store reality with its explanation; its test was seen failing first | D21 |
| A18 | CI's `go` job runs staticcheck and both shell suites, green on a dispatched run; `make preflight` green on the Linux server | D18 |

The criterion most likely to be dropped quietly is **A2**. The audit will be written against
the fixed hosts, and it will pass. Only running it against the saved pre-P0 outputs shows that
it can catch what it exists to catch.

## Rollout and Rollback

Hosts go one at a time within each phase. Rollback per host: restore the dated backup (shell
init, registry environment, Flutter settings, Go env), ~~`mise use` the previous version~~
repoint PATH at the previous `~/sdk` directory, or reinstall the previous MSI from the staging
folder. For P9 on a host, until its step "remove mise" runs, restoring the backed-up dotfiles
files and running `systemctl --user daemon-reload` brings mise back unchanged. That is why
mise's data is removed last. This repository: revert the phase's
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
- **The dotfiles repository's own records that describe mise** (its shell-environment and
  cross-host Go-env records): they belong to that repository's record sequence, and the owner
  amends them there. P9 changes only the files it lists.
- **The `mcremote` drop-in as a literal PATH snapshot** (MADR amendment, Consequences): a
  generated or included PATH would remove the re-derive step. That is a change to how
  `mcremote setup-service` writes the unit, so it belongs in its own record.
