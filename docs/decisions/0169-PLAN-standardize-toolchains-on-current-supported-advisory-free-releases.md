---
status: in-progress
date: 2026-09-29
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

#### P1 execution (2026-09-29)

**Files (magic-git `scripts/tools/devenv/`, left uncommitted for the owner):**
`standard.json` (new), `version_audit.py` (new), `README.md` (two sections added), and
`fork_tools.py`. `fork_tools.py` was recorded as done under 9a, but it had never been
committed. The only copy was an untracked file in the Windows host's magic-git checkout. It
was copied into the canonical checkout unchanged (sha256 `3a25f778…`, identical on both).

**Design points the plan left open:**
- **Where the standard lives (MADR open question):** beside the audit in magic-git. The
  audit reads one file.
- **Host kind.** Each probe document's `host` section gives the host kind (windows, wsl,
  linux, macos), so `standard.json` carries no host identifier. Git for Windows' version is
  a per-kind field.
- **Prose advisory ranges.** git, Git for Windows and Dart publish affected ranges as
  prose. `standard.json` records each advisory's fixed versions under
  `reviewed_advisories`, with a `reviewed_through` date per repository. An advisory
  published after that date is a `REVIEW` finding. The two Git for Windows advisories of
  2026-09-28 (GHSA-v3h4-vrpr-rh9x, GHSA-5j7x-r6vf-jhq2) state no patched version. They were
  split from GHSA-rxqw-wxqg-g7hw (fixed in 2.55.0.windows.3) and their ranges end at
  v2.54.0.windows.1, so both are recorded as fixed in 2.55.0.windows.3.
- **Advisory sources.**
  - OSV for Go stdlib and toolchain, gh and glab.
  - OSV by release-tag commit for CPython. That is how the PSF database is queried.
  - nodejs.org's `security` flag for Node.
  - The newest OpenJDK advisory pages. Each lists the last affected release of each line
    "and earlier".
- **Exit codes.** 1 for any DRIFT, MISE, ADVISORY, REVIEW, STALE or UNSUPPORTED; else 2 for
  ERROR; else 3 for NO-FIX only; else 0.
- **GitHub API.** It reads GitHub with `GITHUB_TOKEN`, `GH_TOKEN` or `gh auth token`.
  Unauthenticated, the 60-request hourly limit ran out during the fixture runs; the audit
  reported each refused request as ERROR rather than passing.

**Seen to fail (A2).** Every fixture was run from the scratchpad, and none is committed.

| Input | Result |
| --- | --- |
| Pre-P0 probe outputs | exit 1. The five Windows exposures by name: Node 24.14.0 (3 later security releases: v24.14.1, v24.17.0, v24.18.1); Temurin 21.0.12 and 25.0.4 ("the OpenJDK advisory of 2026-08-18 affects 21.0.12 / 25.0.4 and earlier"); Git for Windows 2.55.0.windows.3 (GHSA-xrpg-8j9v-v282, fixed in .windows.4); Python 3.14.3 (29 advisories fixed by 3.14.7) |
| Temp standard naming Go 1.26.5 | exit 1. `ADVISORY standard go 1.26.5: 10 advisories fixed by 1.27.1`: the 8 stdlib ones (GO-2026-5026, -5942, -5972, -6088, -6089, -6090, -6091, -6218) and 2 toolchain ones (GO-2026-6179, -6180) |
| Pre-P9 probe outputs (A11) | `MISE` on macOS, the Linux server (binary, 11 PATH entries) and WSL (binary, shims entry) |
| Temp standard with Git for Windows reviewed only through 2026-09-23 | two `REVIEW` lines, for GHSA-v3h4-vrpr-rh9x and GHSA-5j7x-r6vf-jhq2 |
| `--today 2026-10-29` | `STALE standard node 24.21.0 -> 26.10.0 (line 26 is the newest Active LTS)` |
| Negative control: Windows and Linux documents rewritten to every standard value | no DRIFT, ADVISORY or MISE for either host |

Two bugs were fixed before the runs above:
- **Go findings were named by CVE alias.** OSV records are now merged by alias and named
  GO-, then CVE-, then PSF-.
- **NO-FIX used the standard as the fix candidate.** With a vulnerable standard, that
  printed "also affects 1.26.5". The candidate is now the newest release.

The audit also runs, with identical output, under macOS's system Python 3.9.

**Stability rule, 2026-09-29.** The four-host probe, run from the Windows host, gave
`UNCHANGED` for every snapshot. The audit then exited 1 with: 34 DRIFT, 2 ADVISORY, 16
NO-FIX, 3 STALE and 1 UNSUPPORTED.
- **No MISE on any host (A11), and no advisory on Windows.** P0's remediation holds.
- **The DRIFT lines are P2–P7's remaining work.** On WSL they include "no developer
  Python 3.14.7 precedes" the system python3, which is P7's question.
- **NO-FIX:** Python 3.14.7's four advisories, reported on each host and on the standard.
  See the observation below.
- **STALE:**
  - git 2.55.0 → 2.56.0;
  - Git for Windows 2.55.0.windows.5 → 2.56.0.windows.1 (released 2026-09-28);
  - glab 1.119.0 → 1.120.0 (2026-09-29).
- **ADVISORY:**
  - the macOS laptop has an unreferenced `~/.local/go1.26.5`, which carries 10 Go
    advisories;
  - WSL's Ubuntu git 2.43.0 is flagged against upstream's fixes. Ubuntu's backports are
    invisible to this check, and P7 replaces the package.
- **UNSUPPORTED:** the macOS laptop's Homebrew OpenJDK 26.0.2.1, whose line ended
  2026-09-15.

**Observation: Python 3.14.7's advisory count.** Deviation 3 counted 7 advisories by hand.
OSV's git-range analysis reports 4 for the v3.14.7 tag: CVE-2026-15310, -15806, -17084 and
-19672. For the other three (CVE-2026-87910, CVE-2025-15367, CVE-2024-3220), OSV's affected
tag list contains no 3.14 release, so the audit does not report them. The difference is
unresolved: the audit reports what OSV computes, and this record keeps the hand count.

#### Deviation 8 (2026-09-29): git, Git for Windows and glab moved on before P7

**Found** by P1's audit on its first run. git 2.56.0 is final (tag `v2.56.0`), Git for
Windows released 2.56.0.windows.1 on 2026-09-28, and glab released 1.120.0 on 2026-09-29.
All three are newer than the standard the MADR names, so the audit reported them `STALE`.
With `standard.json` moved to them, the audit reports no advisory at any of the three.

**Decision (owner, 2026-09-29): adopt the newest.** P7 installs git 2.56.0 (Git for Windows
2.56.0.windows.1) and glab 1.120.0, so hosts move once. MADR amendment of 2026-09-29 (D7,
D9). The alternative, installing 2.55.x and 1.119.0 as written and rolling forward later,
was declined.

**Scope.** No file added. `standard.json` (in scope) carries the new values, verified
2026-09-29.

#### Deviation 9 (2026-09-29): an old Go 1.26.5 install on the macOS laptop

**Found** by P1's audit on the current state. `~/.local/go1.26.5` (July 2026) is still on
disk. Nothing points at it: `~/.local/bin/go` resolves to `~/.local/go1.26.6`. It carries
10 Go advisories (8 stdlib, 2 toolchain), all fixed in 1.26.6 and later.

**Decision (owner, 2026-09-29): remove it in P2**, when go1.27.1 is installed on the macOS
laptop.

**Scope.** Added to P2: `~/.local/go1.26.5` on the macOS laptop (deleted).

#### Deviation 10 (2026-09-29): Homebrew OpenJDK 26 on the macOS laptop is past end of life

**Found** by P1's audit. Homebrew's `openjdk` formula is 26.0.2.1, and the Java 26 line
ended 2026-09-15 (endoflife.date, eclipse-temurin). It is not Flutter's JDK
(`openjdk@21` is).

**Decision (owner, 2026-09-29): fold it into P5.** After the Temurin 25 cask builds the APK on
the macOS laptop, list what depends on Homebrew `openjdk`. Uninstall it if nothing does;
otherwise report the dependent.

**Scope.** Added to P5: Homebrew `openjdk` on the macOS laptop.

#### Deviation 11 (2026-09-29): `go_tools_standard.py` does not exist

**Found** before P2 step 2. The script is not on any of the four hosts, in magic-git, or in
any repository here. The probe of 2026-09-29 shows the same 11 tools at the same versions on
every host:
- benchstat `v0.0.0-20260825160852-19be9d8e6c70`
- dlv v1.27.1
- gofumpt v0.12.0
- golangci-lint v2.13.2
- golint `v0.0.0-20241112194109-818c5a804067`
- gopls v0.23.0
- gotestsum v1.13.0
- govulncheck v1.7.0
- rsrc v0.10.2
- staticcheck v0.8.1
- treefmt v2.5.0

All were built with go1.26.6.

**Resolution.** Rebuild each with `go install <package>@<same version>` under go1.27.1. That
does what the named script would have done, at unchanged versions. The audit's
Go-bin-directory check confirms the result.

**Scope.** No file added.

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

#### P2 execution (2026-09-29)

The owner approved all four hosts (C2). Each host had dated backups in
`~/backups/0169-p2-2026-09-29/`, outside any repository, and a `devenv_snapshot.py` taken
before and after. Every archive matched go.dev's published sha256 (C3):
- darwin-arm64 `ee215d57…`;
- linux-amd64 `63d339f0…`;
- windows-amd64 `a3911b5e…`.

go1.26.6 stays installed on every host until P3's gates pass (step 5, C4).

| Host | go1.27.1 | PATH / link | `GOTOOLCHAIN` | Snapshot diff |
| --- | --- | --- | --- | --- |
| macOS laptop | `~/.local/go1.27.1` | `~/.local/bin/go` and `gofmt` relinked | Go env file | the Go env file only |
| Linux server | `~/sdk/go1.27.1` | `00-paths.sh` line and the `mcremote` drop-in PATH literal (dotfiles commit `34221b0`, not pushed); `daemon-reload` | `~/.config/go/env` | `00-paths.sh`, `~/.config/go/env` |
| WSL | `~/sdk/go1.27.1` | the `devenv.sh` PATH entry | `~/.config/go/env` (new file) | `devenv.sh`, `~/.config/go/env` |
| Windows | `~\sdk\go1.27.1` | the one User PATH entry, value kind kept (`String`, as before) | registry User environment and `%APPDATA%\go\env` | `HKCU\Environment`, `%APPDATA%\go\env` |

**Resolution.** Every host resolves go1.27.1 from its new location:
- the Linux server in `bash -lic`, in `bash -c`, and in a transient `systemd-run --user`
  unit carrying the drop-in's PATH;
- WSL and the macOS laptop in a login shell;
- Windows from the registry. The probe's fresh-logon environment reads it.

The owner then had `mcremote` restarted on the Linux server. It came back active, and its
`/proc` environment's PATH starts with `~/sdk/go1.27.1/bin`.

**Step 2, the tools (Deviation 11).** On every host, 10 of the 11 tools were rebuilt with
`go install <package>@<same version>`. `treefmt` v2.5.0 cannot be `go install`ed: its
module zip is rejected (`malformed file path "test/examples/emoji 🕰️/README.md"`). The
existing binaries had been built from a checkout of the tag. The build info showed
`vcs.revision=7ab41ba4…`, and no ldflags. So on each host it was rebuilt the same way:
- a clone of tag `v2.5.0`;
- HEAD checked equal to `7ab41ba4491e77bfdbdda24d81554618ba1cfff6`, which GitHub's
  annotated tag `0a36c1d…` points to;
- `go build`;
- the result still stamps `mod … v2.5.0`.

`go version` on every tool in each host's Go bin directory reports go1.27.1.

**Step 3, gopls (MADR open question): answered, keep v0.23.0.** It was built with go1.27.1
on the macOS laptop. The scratch module declares `func (b Box) Map[T any](f func(int) T) T`
and runs under `go run`. Results:
- `gopls check` on it reports nothing;
- on a copy with a planted type error, gopls reports it: `cannot use b.v (variable of type
  int) as string value in variable declaration`. So its silence on the first file is
  meaningful.

**Step 4, the acp-go-sdk fork.** `PATH="$PWD/.tools/bin:$PATH" make check test` passes on the
host's go1.27.1 on the macOS laptop, the Linux server and WSL. The fork's `git status` is
empty before and after. The fork has no checkout on Windows, where P9 did not apply.

**Deviation 9 done.** `~/.local/go1.26.5` was removed from the macOS laptop, after checking
that no shell-init file or `~/.local/bin` link referenced it.

**Verification.** A four-host probe gave `UNCHANGED` for every snapshot. The audit then
reported **0** Go findings, against 9 before P2 (the 1.26.5 advisory among them). What it
still reports belongs to later phases.

**Observations, recorded rather than acted on:**
- **dl.google.com answered the Linux server with HTTP 404** for the go1.27.1 archive, and
  the macOS laptop with 200. The archive was fetched on the macOS laptop, checked there, and
  copied over. The server's script checked the same sha256 again before unpacking.
- **Running `~/.local/go1.26.5/bin/go version` downloaded the go1.27.1 toolchain.** This
  happened on the macOS laptop after `GOTOOLCHAIN=go1.27.1` was set. The download went into
  the module cache, checksum-verified by the Go toolchain. The module cache also still
  holds a `go1.26.5` toolchain entry. The audit does not inspect the module cache, and
  nothing puts that entry on PATH.
- **The Windows step was first run under Windows PowerShell 5.1.** Its `Expand-Archive`
  was still unpacking after more than ten minutes, and it was stopped at the owner's
  request. It had changed nothing outside its temp directory. The run was repeated under
  PowerShell 7.6.6.

#### Deviation 12 (2026-09-29): Go 1.27's `json.Decoder` hangs the codex test harness

**Found** at P3 step 2. `make race` on go1.27.1 failed one test:
`internal/provider/codex` `TestDiffUsesCWDOnlyAndValidatesSHA`, at `diff_fork_test.go:88`
("timeout").
- **Not a flake.** With `-race -count=50` it fails 50 of 50 runs on go1.27.1. It passes 50
  of 50 on go1.26.6 in a scratch clone of `HEAD`.
- **Go 1.27 builds `encoding/json` from its v2 implementation by default.** `go list`
  shows `v2_stream.go` where 1.26.6 has `stream.go`.
- **Why the test hangs.** The codex transport sends a frame and its `\n` in one `Write`
  (`transport.go:64`). On an `io.Pipe`, that write returns only when the reader has taken
  every byte. The tests' fake engines read the request with `json.NewDecoder(<pipe>)`. On
  1.27 that decoder stops at the value's closing brace and leaves the `\n` unread.
- **Confirmed by instrumentation.** In a scratch clone, `Diff` stayed blocked in its
  request write until cleanup closed the pipe, then returned `write request: io:
  read/write on closed pipe`.
- **Production is unaffected.** The engine is a separate process on kernel-buffered pipes,
  and replies are read line by line. The production `json.NewDecoder` uses are on a
  socket (`internal/admin`) or on in-memory readers.
- **The latent hazard is wider.** The same pattern appears at 25 sites in 9 codex test
  files; only the longest request failed. And since P2 set `GOTOOLCHAIN=go1.27.1` on every
  host, `make race` fails there even on an unchanged tree.

**Decision (owner, 2026-09-29): fix every fake engine.** One test helper, `readFrame`,
reads a request frame the way the engine does: byte by byte to the newline, then
`json.Unmarshal`. It consumes the whole write, and never reads into a following frame.
Every `json.NewDecoder(<pipe>)` request read in the codex tests uses it. There is no
production change. Declined:
- fixing only the failing test;
- the unoffered workarounds: GOEXPERIMENT/GODEBUG to restore the old decoder, a longer
  timeout, or a skip.

**Scope.** Added to P3, in `internal/provider/codex/`: `conn_test.go` (the helper and 3
sites), `diff_fork_test.go`, `collaboration_state_test.go`, `thinking_test.go`,
`review_test.go`, `fixtures_test.go`, `fast_personality_test.go`, `mode_test.go` and
`permissions_p5_test.go`.

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

#### P3 execution (2026-09-29)

**Step 1.** `go mod edit -go=1.27.1`, then `go mod tidy` (exit 0). Tidy rewrote nothing:
`go.mod` changed only its `go` line, and `go.sum` is byte-identical. Consequences
anticipated a require-block merge. It did not happen: the two `require` blocks are
unchanged. CI's three `setup-go` steps read `go-version-file: go.mod`, so CI moves to
1.27.1 with this commit.

**Step 3, behaviour changes.**
- **`asynctimerchan`:** no `//go:debug`, no `godebug` line, no GODEBUG setting, and no
  `len`/`cap` of a timer or ticker channel anywhere in the module.
- **`go test -json`:** nothing consumes it. The CI flake ledger (`ci-flake-capture.sh`)
  parses the plain-text `--- FAIL:` lines, and 1.27 does not change them.
- **A fourth change, not in the MADR:** Go 1.27 builds `encoding/json` from its v2
  implementation. It hung a codex test (Deviation 12, fixed there; MADR amendment of
  2026-09-29).

**Step 2, the gates, all on go1.27.1:**

| Gate | Result |
| --- | --- |
| `go vet ./...` (includes `stdversion`) | exit 0 |
| `govulncheck ./...` | exit 0: no called vulnerabilities (see below) |
| `make pre-add-check` | exit 0, "816 file(s) clean (gofmt, golint, govulncheck)" |
| `make race`, before Deviation 12 | exit 2: 41 ok; `--- FAIL: TestDiffUsesCWDOnlyAndValidatesSHA` (`diff_fork_test.go:88: timeout`) |
| that test, `-race -count=50`, before the fix | go1.27.1: 50 of 50 fail; go1.26.6 (scratch clone of `HEAD`): 0 of 50 |
| after the fix (25 sites, 9 files): that test ×50 and the codex package ×5, `-race` | 0 failures on go1.27.1, and on go1.26.6 with the same test files |
| `make race`, after the fix | exit 0, 42 ok |
| `make ci-windows` (Git Bash on the Windows host, scratch clone of `HEAD` plus this change, go1.27.1) | exit 0, 43 ok, "ci-windows-local: ALL SELECTED CHECKS PASSED" |

**Found, not acted on: three uncalled `golang.org/x/crypto` vulnerabilities.**
`govulncheck -show verbose` lists three in `golang.org/x/crypto@v0.55.0` that this code
does not call:
- GO-2026-6354 and GO-2026-6355, denial of service from deadlocked `ssh` channels (one
  undecided, one established), both fixed in v0.56.0;
- GO-2026-5932, the unmaintained `openpgp` package, which has no fix.

They do not depend on the Go version: `go.sum` is unchanged, so `master` has them too.
Moving x/crypto is a dependency change outside this plan's scope.

**Not yet done:** the push and CI (step 4) wait for the owner. go1.26.6 comes off the hosts
only after CI is green on this change (P2 step 5, C4).

#### Deviation 13 (2026-09-29): the cached staticcheck outlives a Go upgrade

**Found** by PLAN 0174 P4's `make preflight` on the macOS laptop, after P3 had landed.
`make staticcheck` failed on every GOOS with errors like:
- `method must have no type parameters` (in go1.27.1's `math/rand/v2`);
- `package requires newer Go version go1.27 (application built with go1.26)` (this
  module's own packages).

**Cause.** The Makefile caches the pinned tool at
`bin/tools/staticcheck-$(STATICCHECK_VERSION)` and rebuilds it only when that file is
missing.
- The cached binary was built with go1.26.6 before P2, and a staticcheck built by an older
  Go cannot load code for a newer one. The Linux server's checkout has the same stale
  build.
- CI builds the tool fresh each run, and it passed.
- P3's gate list did not include staticcheck, so nothing ran it.
- A fresh scratch clone, which has no cache, built it with go1.27.1, and `make
  staticcheck` passed on linux, darwin and windows.

**Decision (owner, 2026-09-29): key the cache on the Go version too.** The cached binary
becomes `bin/tools/staticcheck-<version>-<go version>` (`go env GOVERSION`), so a toolchain
change rebuilds it on every host, and a rebuild clears older builds. Declined: deleting
the stale binaries by hand, which would recur at the next Go upgrade.

**Scope.** Added to P3: `Makefile` (the staticcheck block).

#### Deviation 14 (2026-09-30): removing go1.26.6 from the hosts is deferred

**Found.** P2 step 5's condition (C4) is met: P3's gates passed on every host, and CI run
36653083292 is green on go1.27.1 (`6ea9f3f1`).

**Decision (owner, 2026-09-30): defer step 5, and proceed to P4–P7.** go1.26.6 stays
installed, unused, beside go1.27.1 on all four hosts:
- `~/.local/go1.26.6` on the macOS laptop;
- `~/sdk/go1.26.6` on the Linux server and WSL;
- `~\sdk\go1.26.6` on Windows.

Nothing points at them: every PATH and `GOTOOLCHAIN` names go1.27.1. They leave with P8, or
at the owner's word.

**Scope.** No file added.

#### Deviation 15 (2026-09-30): WSL has no `unzip`, which Flutter needs

**Found** at P4 step 1 on WSL.
- The `~/sdk/flutter` checkout moved from `stable` (3.47.2) to tag 3.47.5; tag commit
  `6a19cca564`, checked against the stable release.
- `flutter --version` then failed: `Missing "unzip" tool. Unable to extract Dart SDK.`
- `unzip` is not installed (apt candidate `6.0-28ubuntu4.1`). Flutter needs it for every
  artifact it unpacks on Linux, and sudo on WSL needs the owner's password.
- **C4 was broken for a few minutes.** The old version was replaced before the new one
  worked. The checkout was restored to branch `stable` at `d3b14c87` (3.47.2), and
  `flutter --version` works again. The tool rebuilt once. There are no tracked changes.

**Decision (owner, 2026-09-30): the owner installs `unzip` on WSL**
(`sudo apt-get install -y unzip`); WSL's P4 move resumes after that. Declined: skipping
WSL for P4. Using the release archive instead of the checkout was not offered: it would
postpone the same failure to the first artifact download.

**Scope.** Added to WSL: the apt package `unzip`. The owner installs it.

**Process note.** Each host's P4 step now checks `unzip` (Linux) before switching the
checkout, so no host is left half-moved again.

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

#### P4 execution (2026-09-30)

**Step 1, hosts.** Every host is on Flutter 3.47.5 (framework `6a19cca564`, engine
`af7e796e16`) with Dart 3.13.4. The commit was checked against the stable release in
`releases_linux.json`.

| Host | How | Evidence |
| --- | --- | --- |
| macOS laptop | `brew upgrade --cask --greedy flutter`. The cask is `auto_updates`, so a plain upgrade skips it; brew's record said 3.44.6 because the SDK had been updating itself | `flutter --version` 3.47.5; `~/.config/flutter/settings` unchanged |
| Linux server | `flutter_linux_3.47.5-stable.tar.xz`, sha256 `2132e990…` matched. Unpacked beside the old tree, then swapped; 3.47.2 kept as `~/sdk/flutter-3.47.2` until P5 builds an APK here | `flutter doctor -v`: Android toolchain on `~/sdk/jdk-21`; snapshot UNCHANGED |
| Windows | `~\sdk\flutter` fast-forwarded on branch `stable` to the 3.47.5 commit (Deviation 16) | `flutter doctor -v` exit 0, Android toolchain on Temurin 21.0.12.1 |
| WSL | the same fast-forward, after `unzip` (Deviation 15) | `flutter doctor -v`: Android toolchain on `~/sdk/jdk-21`; snapshot UNCHANGED |

**Fast-forward rather than checkout.** The git checkouts on Windows and WSL were
fast-forwarded on branch `stable` rather than detached at tag `3.47.5` as step 1 is
written. The commit is the same. The SDK stays on the stable channel, where a detached
tag reports `[user-branch]`.

The only snapshot changes are history files: PowerShell's `ConsoleHost_history.txt` on
Windows (the owner's own `flutter --version`) and `.bash_history` on the macOS laptop. No
shell-init or environment file changed.

**Step 2, this repository.** `.github/workflows/ci.yml` `FLUTTER_VERSION: "3.47.5"`. On the
macOS laptop:
- `scripts/assert-flutter-pin.sh`: "local and pinned Flutter agree (3.47.5)";
- `flutter pub get --enforce-lockfile`: exit 0, lockfile unchanged;
- `flutter analyze`: "No issues found!";
- `dart format --set-exit-if-changed`: 0 of 213 files changed;
- `flutter test`: "+1418 ~3: All tests passed!";
- `make apk`: exit 0, 41.1 MB, "OK release-mode APK".

**Step 3, magic-git.** `build_macos.sh` `FLUTTER_VERSION="3.47.5"`, left uncommitted for
the owner. On the macOS laptop, `flutter pub get --enforce-lockfile` exited 0. `flutter
test` gave "+4660 ~3: All tests passed!", and afterwards `git status` was empty: no golden
image changed.

**Step 4, CI:** push and a dispatched `ci.yml` run (so `android-apk` runs) wait for the
owner.

#### Deviation 16 (2026-09-30): the Windows Flutter cache, broken over ssh

**Found** at P4 step 1 on Windows, after the fast-forward.
- **The first run used the wrong launcher.** It ran the POSIX `bin/flutter` from Git Bash,
  not `flutter.bat`. It left `bin/cache/dart-sdk` empty, beside an `engine-dart-sdk.stamp`
  claiming 3.47.5's Dart SDK, and a `flutter_tools.stamp` of `:`.
- **`flutter.bat` then failed on every retry** ("The system cannot find the path
  specified"), because it believed the stamp and never downloaded Dart.
- **The ssh session's Git Bash environment broke every repair attempt:**
  - `TEMP=/tmp` crashed PowerShell 7's startup inside `update_engine_version.ps1`;
  - GNU `timeout` shadowed `timeout.exe` in `flutter.bat`'s retry wait;
  - under a PATH rebuilt from the registry, the `7z` extraction hung for 25 minutes.
- **Processes of mine outlived their ssh sessions** and held Flutter's lock; I stopped
  them. None of the owner's processes were touched.
- **Clean-up.** The empty `dart-sdk` and the false stamps were removed each time, so the
  next run would download afresh. None of this touched the git checkout or any shell or
  environment file.

**Resolution (owner, 2026-09-30).** The owner ran `flutter --version` in a normal Windows
terminal, which downloaded and unpacked the Dart SDK. Verified afterwards from a
registry-built environment: `flutter --version` 3.47.5 / Dart 3.13.4, and `flutter doctor
-v` exit 0.

**Process note.** On Windows, run Flutter only through `flutter.bat`, in an environment
built from the registry (PATH, TEMP, TMP), never the POSIX script from Git Bash.

**Scope.** No file added.

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

#### Deviation 17 (2026-09-30): a user Gradle setting pins JDK 21 on the macOS laptop

**Found** at P5 step 1 on the macOS laptop. The build looked like it passed:
- `flutter config --jdk-dir` pointed at the Temurin 25.0.4.1 cask;
- `make apk` from a scratch clone exited 0;
- the assert script passed;
- all three app classes were major 61.

But the only Gradle daemon was started by that build (12:25:25), it served the scratch
clone, and it ran on `/opt/homebrew/opt/openjdk@21/…/bin/java`. The cause is
`~/.gradle/gradle.properties`, which sets `org.gradle.java.home` to Homebrew's
`openjdk@21`, and Gradle obeys it over the `JAVA_HOME` Flutter passes from `jdk-dir`. So
that build was JDK 21's and is not P5 evidence. The Linux server, WSL and Windows have no
such setting.

A second gap was found at the same time. Windows' user `JAVA_HOME` still names Temurin
21. Flutter builds do not read it, but a direct `gradlew` does, and step 5's removal of
the 21 MSI would leave it pointing at nothing. It is in this plan's Windows file list,
but step 1 does not move it.

**Decision (owner, 2026-09-30).**
- `~/.gradle/gradle.properties` on the macOS laptop: `org.gradle.java.home` is repointed
  to `/Library/Java/JavaVirtualMachines/temurin-25.jdk/Contents/Home`, with a dated
  backup. It is kept rather than removed, because the shell's `JAVA_HOME` is Homebrew's
  `openjdk` 26, which Gradle 9.1 does not run on.
- Windows' `HKCU\Environment` `JAVA_HOME` moves to Temurin 25.0.4.1 in step 1, with a
  dated backup.

**Verification change.** A P5 build counts only if the Gradle daemon that served it ran
on the Temurin 25 JDK, checked from the daemon's process. `jdk-dir` and a green APK are
not enough.

**Scope.** Added to the macOS laptop: `~/.gradle/gradle.properties` (the
`org.gradle.java.home` line only). Windows: `JAVA_HOME` joins P5 step 1.

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

#### P5 execution (2026-09-30)

**Step 1, hosts, and step 3, bytecode.**
- Every host has Temurin 25.0.4.1+1:
  - the macOS laptop has the `temurin@25` cask, a `.pkg` whose sha256 `68e28a99…` Homebrew
    checks;
  - the Linux server and WSL have `~/sdk/jdk-25`, from Adoptium's tarball (sha256
    `dbb69839…`);
  - Windows has the P0 MSI.
- Flutter's `jdk-dir` points at it on every host.
- Each host then built the release APK from a scratch clone of `HEAD`, with three checks:
  - the Gradle daemon that served the build ran on that JDK (Deviation 17);
  - `assert-flutter-release-apk.sh` passed;
  - `javap -v` gives major 61 for `MainActivity`, `UpdateInstaller` and
    `UpdateInstallReceiver`.
- **Seen to fail first, on every host:** a class that JDK 25's `javac` compiles by default
  is major 69, and the same check rejected it.

| Host | Other edits (dated backups in `~/backups/0169-p5-2026-09-30/`) | Daemon JVM | APK |
| --- | --- | --- | --- |
| macOS laptop | `~/.gradle/gradle.properties` `org.gradle.java.home` (Deviation 17) | `temurin-25.jdk/…/bin/java` | 39M |
| Linux server | dotfiles: `05-env.sh` `JAVA_HOME`, `00-paths.sh` PATH, `11-tool-env.conf` `JAVA_HOME`, the drop-in PATH; `daemon-reload` | `~/sdk/jdk-25/bin/java` | 40M |
| WSL | `devenv.sh` `JAVA_HOME` | `~/sdk/jdk-25/bin/java` | 40M |
| Windows | `HKCU\Environment` `JAVA_HOME` (Deviation 17; value kind `String`, as before) | `jdk-25.0.4.101-hotspot\bin\java.exe` | 39.2MB |

**Windows ran a different build command.** It ran `flutter.bat build apk --release
--target-platform android-arm64`, the command `make apk` runs, in a registry-built
environment (Deviation 16) rather than through `make` from Git Bash. The repository's
assert script then ran through Git Bash.

**Step 2, CI.** `ci.yml`'s `android-apk` job: `java-version: "25"`. actionlint reports only
the two findings that predate this change. The dispatched run waits for the owner.

**Step 4, MADR 0168.** An additive amendment says its D1/D3 choice of 21 is superseded by
0169 D5, and that D2 stands.

**Step 5, JDK 21 removed where C4 allows.**
- **macOS laptop:** `brew uninstall openjdk@21 openjdk` removed 21.0.12.1 and, per
  Deviation 10, 26.0.2.1. Nothing depended on either. `~/.config/bash/env.sh` exports
  `JAVA_HOME` only if Homebrew's `openjdk` exists, so a fresh login now leaves it unset,
  and `/usr/bin/java` resolves through `java_home` to Temurin 25.0.4.1.
- **WSL:** `~/sdk/jdk-21` removed. Its only reference was an old Gradle daemon log.
  Ubuntu's `/usr/lib/jvm` packages are the distribution's and stay.
- **Windows:** the Temurin 21.0.12.1 MSI (`{285FFC48-…}`) was uninstalled, `msiexec /x`
  exit 0. The machine PATH now lists only `jdk-25.0.4.101-hotspot\bin`. The ssh session
  proved to be elevated (`net session` succeeds), so no UAC prompt was involved.
- **Linux server:** `~/sdk/jdk-21` stays until `mcremote` is restarted. The running
  daemon's environment still names it.

#### P6 execution (2026-09-30), part a

Node 24.21.0 on every host. The Linux archives were checked against nodejs.org's
`SHASUMS256.txt` (`fd8e59d5…`).
- **Windows:** unchanged since P0.
- **Linux server:** `~/sdk/node-v24.21.0`, with `00-paths.sh` and the drop-in PATH moved
  from `node-v22.23.2`, and `daemon-reload`. markdownlint-cli2 0.23.2 was reinstalled under
  the new npm (D15). `bash -lic` and `bash -c` both resolve `node` v24.21.0.
  `~/sdk/node-v22.23.2` stays until `mcremote` restarts.
- **WSL:** `~/sdk/node-v24.21.0` plus a `devenv.sh` PATH entry, where there was no native
  Node before.
- **macOS laptop:** `brew install node@24`, `brew unlink node` (26.8.1, left installed),
  and `brew link --force --overwrite node@24`. A fresh login resolves `node` v24.21.0. The
  agent CLIs were checked before and after:
  - `gemini` runs `/opt/homebrew/opt/node/bin/node` by absolute path;
  - `opencode` is a native binary;
  - `@openai/codex` 0.158.0 needs `node >=16`;
  - `@kilocode/cli` 7.8.1 declares no engine requirement.

  All four still start.
- **CI:** `NODE_VERSION: "24"` is kept.

Part b (Node 26) waits for 2026-10-28.

#### P7 execution (2026-09-30)

**Python (step 1, Deviation 20).** WSL has `~/sdk/python-3.14.7`, from
python-build-standalone `20260901` (GitHub digest `0ab33054…`), and `~/default-venv` on it
is first in `devenv.sh`'s PATH.
- A login shell's `python3` is 3.14.7, with pip 26.2.1.
- `/usr/bin/python3` is still Ubuntu's 3.12.3.
- The archive's own `bin` is on no PATH.

**git (step 2).**
- **macOS laptop:** `brew upgrade git` gave 2.56.0 (with `pcre2` 10.48 → 10.49).
- **Linux server and WSL:** the git-core PPA gave 2.55.0 (`…ubuntu26.04.2` and
  `…ubuntu24.04.2`), per Deviation 19. The only new apt sources are the two PPA files. The
  server's `~/.local/bin/git` still links to `/usr/bin/git`.
- **Windows:** Git for Windows 2.56.0.windows.1 (`Git-2.56.0-64-bit.exe`, digest
  `bfe94e7b…`, re-verified before running). Its installer aborts while any `bash.exe` runs,
  and every ssh command starts Git Bash. So it ran from a one-time scheduled task, in the
  owner's session with highest privileges, which waited until no Git Bash process was
  left. Setup exit 0; `git --version` 2.56.0.windows.1; the task was then deleted.

**gh 2.102.0 and glab 1.120.0 (step 3, Deviation 18).** Each archive was checked against
its publisher's checksums file.
- **macOS laptop:** `brew upgrade gh glab`. `gh auth status` and `glab auth status` pass.
- **Linux server:** `~/.local/bin`, previous binaries backed up. gh is still logged in.
  glab reported "No token found" before and after: this host was never logged in to glab.
- **WSL:** `~/.local/bin`, both new; neither is logged in.
- **Windows:** `~\toolchains\{gh,glab}` replaced in place. The old folders were kept
  until the new binaries answered with their versions. Over ssh neither can read the
  owner's stored credentials: glab reports "failed to read 'token' from the operating
  system keyring … A specified logon session does not exist", and gh calls its token
  invalid. **Confirmed by the owner (2026-09-30):** in a normal Windows terminal, both
  `gh auth status` and `glab auth status` pass.

**Audit after P4–P7.** A four-host probe gave UNCHANGED for every snapshot. The audit
reported 2 DRIFT and 20 NO-FIX:
- DRIFT: Linux git 2.55.0 against 2.56.0 on the server and WSL (Deviation 19);
- NO-FIX: Python 3.14.7's four known advisories on each host and on the standard
  (Deviation 3).

There was no ADVISORY, STALE, UNSUPPORTED or MISE. Go, Flutter/Dart, Node, JDK 25, git,
gh and glab match the standard everywhere else.

#### Deviation 18 (2026-09-30): gh 2.102.0 released before P7

**Found** at P7. cli/cli released v2.102.0 on 2026-09-30, a stable release, not a
prerelease. OSV reports no advisory at 2.102.0 (nor at 2.101.0). Homebrew already offers it.

**Decision (owner, 2026-09-30): adopt 2.102.0**, the same policy as Deviation 8, so hosts
move once. `standard.json` and MADR D8 (amendment of 2026-09-30) name 2.102.0.

**Scope.** No file added.

#### Deviation 19 (2026-09-30): the git-core PPA has only 2.55.0

**Found** at P7 step 2. Launchpad's published sources for `~git-core/ppa` are
`1:2.55.0-0ppa1~ubuntu24.04.2` (noble, WSL) and `1:2.55.0-0ppa1~ubuntu26.04.2` (resolute,
the Linux server). 2.56.0, the standard since Deviation 8, is not published there yet.

**Decision (owner, 2026-09-30): install the PPA's 2.55.0 now**, replacing 2.53.0 on the
server and 2.43.0 on WSL, and roll to 2.56 when the PPA publishes it. The audit reports
Linux git drift until then. Both hosts' steps ran with their passwordless sudo. The WSL
note first said the owner would run them; the owner then pointed out that WSL's sudo is
passwordless too. Declined: building 2.56.0 from source, and waiting.

**Scope.** No file added. The PPA was already in scope for both hosts.

#### Deviation 20 (2026-09-30): WSL's developer Python needs a place on PATH

**Found** at P7 step 1. The step installs the python-build-standalone 3.14.7 archive into
`~/sdk/python-3.14.7`, but nothing puts it on PATH. P1's audit judges the developer Python
by the first `python3` on PATH, and on WSL that stays Ubuntu's 3.12.3.

**Decision (owner, 2026-09-30): the Linux server's arrangement.** `~/default-venv` is
created on the 3.14.7 archive, and `~/default-venv/bin` goes first in `devenv.sh`'s PATH.
`/usr/bin/python3` (Ubuntu's 3.12, D6) stays for apt and system scripts. Declined: the
archive's own `bin` on PATH, and leaving it off PATH.

**Scope.** Added to WSL: `~/default-venv` (new), and a PATH entry in `~/.config/devenv.sh`.

**Also decided (owner, 2026-09-30):** the Windows elevated step runs now: uninstall the
Temurin 21 MSI, and install Git for Windows 2.56.0.windows.1. The `mcremote` restart on the
Linux server is not approved yet. Until it happens, `~/sdk/jdk-21` and
`~/sdk/node-v22.23.2` stay there, because the running daemon's environment still names them
(C4).

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
- ~~**Pending:** `make preflight` green (Deviations 6 and 7, fixed by PLAN 0170, first written here as P10); step 7, removing
  mise's data (C4 holds it until the preflight passes); the dotfiles commit; 9e.~~

#### P9 completion (2026-09-24)

- **9d step 6 finished.** `make preflight` is green on the Linux server (PLAN 0170 P5). Every
  step passed: pinned staticcheck for three GOOS, the hermetic install tests, and 1416 Flutter
  tests. The live `mcremote` start time was unchanged across the run.
- **9d step 7 done.**
  - Removed: `~/.local/bin/mise`, `~/.local/share/mise` (7132 MiB), `~/.local/state/mise`,
    `~/.cache/mise`, and the superseded `~/.local/go` (222 MiB).
  - The two old mise-config backups were moved into `~/backups/0169-p9-2026-09-23/`, not
    deleted. No dangling symlink remained in `~/.local/bin`.
  - The three-context resolution check passes again afterwards.
  - The dotfiles repository is committed on the server (not pushed).
- **9e done.** MADR 0114 is marked superseded by this record, with an additive amendment
  naming D15.
- **Still open under P9:** A11's "the audit reports mise as drift" waits for P1's audit.
  macOS keeps no mise, and WSL no mise.

#### Deviation 6 (2026-09-24): `make preflight` fails at staticcheck on every host

**Found.** The server's preflight stopped at `staticcheck ./...`. There are 43 findings on
`master`, and 42 on the server's older checkout both under mise's Go and under `~/sdk`'s Go.
So they predate P9. CI does not run staticcheck. MADR 0170 F4 and F6–F10 have the detail.

**Decision (owner, 2026-09-24): fix it,** ~~under this record. MADR D18–D21, implemented by P10.~~ Moved the same day, at the owner's request, to [MADR/PLAN 0170](0170-PLAN-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md).
Every other preflight step passed on the server when run individually: `go test -race`,
`verify-units`, the release build, the Flutter pin, `dart format`, `flutter analyze`, and
`flutter test` (1416 tests).

**Scope.** PLAN 0170's file list.

#### Deviation 7 (2026-09-24): `install-binary_test.sh` drives the live user service

**Found.** Running it on the server restarted the live `mcremote`, and the test still failed.
Driving it, and the script under test, on three hosts showed three more things:

- the test is not hermetic (MADR 0170 F1) and its verdict depends on the host (F2);
- `make install` never restarts the service when the user manager is `degraded` (0170 F3, a
  production bug);
- `install_test.sh` fails 3 of its 139 cases on WSL (0170 F5).

**Decision (owner, 2026-09-24): fix it,** ~~under this record. MADR D16–D18, implemented by P10.~~ Moved the same day, at the owner's request, to [MADR/PLAN 0170](0170-PLAN-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md).

**Scope.** PLAN 0170's file list.

**Verification (whole phase):** the audit reports no mise on any host. `command -v mise` finds
nothing on each host. The fork's `make check test` passes through `.tools` on WSL, macOS and
the Linux server. Every step above that must fail was seen to fail.

### ~~P10~~ — moved to PLAN 0170

P10 (hermetic install tests, the degraded-manager fix, pinned staticcheck and the 43
findings, doctor, and CI parity) was written and executed here on 2026-09-24. The same day
the owner moved it to [0170-PLAN-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-PLAN-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) as P1–P5, with its execution record. The text as
first written is in commits `d7f02e5` and `bf7e1e5`. 9d step 7 still waits for PLAN 0170's
P5, a green `make preflight` on the Linux server.

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
| ~~A15–A18~~ | Moved with P10 to PLAN 0170, as its A1–A4 | — |

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
