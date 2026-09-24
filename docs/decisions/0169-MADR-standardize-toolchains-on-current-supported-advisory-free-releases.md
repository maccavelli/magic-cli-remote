---
status: proposed
date: 2026-09-23
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# MADR 0169: Standardize every toolchain on its newest supported, advisory-free stable release

## Context and Problem Statement

The owner develops on four hosts: a Windows laptop, WSL on that laptop, a Linux server and a
macOS laptop. CI builds the same code. The owner set the Go version by a rule: 1.26.5 had known
vulnerabilities fixed in 1.26.6, so everything moved to 1.26.6. This record applies that rule to
every language and developer CLI the projects depend on: Go, Flutter/Dart, Node.js, Java, Python,
git, gh and glab. Each is held to two tests:

1. **Supported:** the release line is inside its publisher's support window, and it is the
   long-term-support line wherever the publisher designates one.
2. **Advisory-free:** no published security advisory affects the chosen version.

Among versions that pass both, the newest stable patch is preferred, and the richest feature set
breaks a tie between supported lines. It answers what to adopt now, what is exposed today, and
how to keep the answer current without re-researching it by hand.

### What was measured, not assumed

All data were collected on 2026-09-23. Publishers' own machine-readable feeds were used where
they exist, and every raw response is kept (Evidence index).

**Releases and lifecycles.**

| Toolchain | Newest stable | Newest in the line we use | Lifecycle (publisher) |
| --- | --- | --- | --- |
| Go | **1.27.1** (2026-09-01) | 1.26.8 (2026-09-01) | "Each major Go release is supported until there are two newer major releases": 1.26 until 1.28 (~2027-02), 1.27 until 1.29 (~2027-08). 1.25 ended 2026-08-19. |
| Flutter | **3.47.5** (2026-09-18), Dart 3.13.4 | same line | Stable channel; next stable is in beta (3.49.0-0.1.pre, 2026-09-21). |
| Dart | **3.13.4** (2026-09-15) | same | 3.13.5 is in the changelog but not yet released. |
| Node.js | 26.10.0 (**Current**, not LTS) | **24.21.0** (Active LTS "Krypton", 2026-09-07) | 24: Active LTS until 2026-10-20, then maintenance until 2028-04-30. 26: becomes Active LTS on **2026-10-28**, EOL 2029-04-30. 22: maintenance, EOL 2027-04-30. |
| Java (Temurin) | 27 (feature release, non-LTS) | 21.0.12.1+1; newest LTS **25.0.4.1+1** | Adoptium `most_recent_lts: 25`. Temurin 25 EOL 2031-09-30, Temurin 21 EOL 2029-12-31. |
| Python | **3.14.7** (2026-08-05) | same | 3.14: bug fixes until 2027-10, EOL 2030-10. 3.13: bug fixes end 2026-10-01. |
| git | **2.55.0** (2.56.0 is at rc2) | Git for Windows **2.55.0.windows.5** (2026-08-20) | Maintenance releases on the newest line. |
| gh | **2.101.0** (2026-09-15) | — | Rolling releases. |
| glab | **1.119.0** (2026-09-22) | — | Rolling, weekly. |

**Advisories, by exact version.**

- **Go.** OSV (which carries the Go vulnerability database) was queried per version.
  - `stdlib@1.26.5`: 8 vulnerabilities (GO-2026-5026, -5942, -5972, -6088, -6089, -6090, -6091,
    -6218), all fixed in 1.26.6.
  - `stdlib@1.26.6`, `1.26.7`, `1.26.8`, `1.27.0` and `1.27.1`: **0**. `toolchain@1.26.6`,
    `1.26.8` and `1.27.1`: 0.
  - go.dev's release history says 1.26.6 "includes security fixes to the go command, and the
    crypto/tls, encoding/asn1, encoding/xml, html/template, net, net/http, and net/url packages".
    1.26.7 and 1.26.8 are bug-fix only (net/http; cgo, compiler, runtime, debug/elf, os).
  - **1.26.6 is the last security release on the 1.26 line.** The owner's reading was correct.
- **Flutter / Dart.** The flutter/flutter repository has no GitHub security advisories.
  dart-lang/sdk has seven; the newest (GHSA-q739-79rh-vmvp, zip slip in pub) was fixed in Dart
  3.11.0. Flutter 3.47.2's hotfix list includes "Updates `libpng` to fix security
  vulnerability". The hotfix notes for 3.47.3, 3.47.4 and 3.47.5, and the Dart changelog for
  3.13.1–3.13.5, contain bug fixes only.
- **Node.js.** The newest security release, 2026-07-29, patched **11 CVEs (3 high)** in
  22.23.2, 24.18.1 and 26.5.1. The highest, CVE-2026-56846, lets retained HTTP/2 headers
  bypass `maxSessionMemory` and exhaust memory. No release after that is flagged `security` in
  nodejs.org/dist/index.json.
- **Java.** An out-of-band OpenJDK advisory on 2026-08-18 fixed 4 CVEs (CVSS 7.5, 6.8, 5.3,
  3.7) in 2d, java.net, javax.net.ssl and javax.xml.crypto. It lists "26.0.2, 25.0.4, 21.0.12,
  17.0.20, 11.0.32, 8u502, and earlier" as affected. The fixes are Temurin's respins
  **21.0.12.1+1** and **25.0.4.1+1**. The next scheduled advisory is the October Critical
  Patch Update (the third Tuesday, 2026-10-20).
- **Python.** 3.14.7's changelog has ten Security entries, among them a tarfile filter bypass
  that reopened CVE-2025-4330 (gh-151558) and http.client limits against hanging servers
  (gh-150743, GHSA-w4q2-g22w-6fr4). The Security sections of 3.14.4–3.14.6 were not extracted
  (see Open questions).
- **git.** git/git's newest advisories (2025-07-08: CVE-2025-48384/5/6) are fixed in 2.50.1
  and later. Git for Windows has two newer ones. GHSA-rxqw-wxqg-g7hw (wincred heap
  corruption) is fixed in 2.55.0.windows.3. **CVE-2026-62960** (GHSA-xrpg-8j9v-v282, high: a
  server-advertised bundle-uri triggers SMB and leaks the NTLMv2 hash) is fixed in
  **2.55.0.windows.4**. Ubuntu marks CVE-2025-48384 fixed in `1:2.43.0-1ubuntu7.3` for 24.04.
- **gh.** cli/cli has 13 advisories; the newest (GHSA-vfhh-p7hm-pxfh) was fixed in **2.98.0**.
  OSV reports 0 for `github.com/cli/cli/v2` at 2.98.0, 2.99.0 and 2.101.0.
- **glab.** OSV reports 0 for `gitlab.com/gitlab-org/cli` at 1.113.0, 1.116.0, 1.117.0 and 1.119.0.

**What the hosts run today.** From the read-only environment probe (magic-git
`scripts/tools/devenv/`), 2026-09-23, with every shell-init snapshot unchanged:

| | Windows | WSL | Linux server | macOS laptop | CI |
| --- | --- | --- | --- | --- | --- |
| Go | 1.26.6 | 1.26.6 | 1.26.6 | 1.26.6 | `go.mod` 1.26.6 |
| Flutter / Dart | 3.47.2 / 3.13.2 | same | same | same | `FLUTTER_VERSION` 3.47.2 |
| Node | **24.14.0** | none native (Windows' leaks in via PATH) | 22.23.2 | 26.8.1 (Current) | `NODE_VERSION` "24" |
| JDK (Flutter's) | **Temurin 21.0.12+8** | Temurin 21.0.12.1 | Temurin 21.0.12.1 (mise) | Homebrew OpenJDK 21.0.12.1 | Temurin "21" (resolved 21.0.12+1 on 2026-09-23) |
| Python | **3.14.3** | 3.12.3 (Ubuntu system) | **3.14.4** | 3.14.7 | — |
| git | **2.55.0.windows.3** | 2.43.0 (Ubuntu, patched) | 2.53.0 | 2.55.0 | runner image |
| gh | 2.98.0 | **none** | 2.98.0 | 2.99.0 | runner image |
| glab | 1.117.0 | **none** | 1.113.0 | 1.116.0 | — |

**A measured JDK 25 build.** Before choosing between Java LTS lines, this project's release APK
was built on Temurin **25.0.4.1+1** in WSL. The run was fully isolated: a temporary JDK, a
separate Flutter settings directory and Gradle user home, and a scratch clone, all deleted
afterwards. `flutter doctor -v` reported `Temurin-25.0.4.1+1`. `make apk` exited 0, and
`scripts/assert-flutter-release-apk.sh` printed `OK release-mode APK` (40M). All 4 app class
files are major version **61** (Java 17 bytecode, per MADR 0168 D2). The pinned toolchain
declares support for this: Gradle 9.1.0 is the first release that runs on Java 25 (Gradle
compatibility matrix), and Kotlin 2.3.0 added Java 25 support (the project uses 2.3.20).

**Go 1.27 readiness of the standard Go tools (MADR 0167/0168 era).**

- golangci-lint **v2.13.0** (2026-08-19) added Go 1.27 support; the standard is v2.13.2.
- staticcheck **2026.2** (2026-08-20) "Added support for Go 1.27", including generic methods;
  the standard is 2026.2.1 (v0.8.1).
- gofumpt **v0.12.0** "is based on Go 1.27's gofmt".
- delve publishes a 1.27.x series (1.27.2, 2026-09-09).
- gopls **v0.23.0** (2026-07-07) predates Go 1.27 GA. Its handling of generic methods when
  built with 1.27 is **[unverified]**.

### Findings

- **F1 — four security exposures today, all on the Windows host.**
  - Node 24.14.0 predates three security releases (24.14.1, 24.17.0, 24.18.1), including the
    3 high CVEs of 2026-07-29.
  - Temurin 21.0.12+8 is affected by the 2026-08-18 advisory (fixed in 21.0.12.1).
  - Git for Windows 2.55.0.windows.3 is affected by CVE-2026-62960 (fixed in .windows.4).
  - Python 3.14.3 lacks at least 3.14.7's ten security fixes.

  The Linux server's Python 3.14.4 lacks the same 3.14.7 fixes. These are exposures now, not
  standardization questions, and they do not wait for the rest of this record.
- **F2 — Go 1.26.6 is secure, but it is no longer the newest of anything.** No advisory affects
  1.26.6–1.26.8 or 1.27.x. 1.26.8 carries two later rounds of bug fixes. Go 1.27.1 is the newest
  stable, and its support runs six months longer than 1.26's.
- **F3 — Go 1.27 is the most feature-rich supported Go, and the standard tools are ready for
  it.**
  - Language: generic methods, broader struct-literal keys, generalized function type inference.
  - Libraries: `encoding/json/v2` and `jsontext`, `uuid`, `crypto/mldsa` with ML-DSA in
    `crypto/tls` and `crypto/x509`, and MLKEM1024.
  - HTTP: HTTP/2 client priorities and `Server.MaxHeaderValueCount`.
  - Runtime: small-allocation fast paths ("up to 30%") and the `goroutineleak` profile.
  - Its one platform cost, macOS 13 or later, is met by the macOS laptop (Darwin 25).

  golangci-lint, staticcheck, gofumpt and delve support 1.27. gopls is unverified (above).
- **F4 — Node 26 is the richer line but not yet LTS.** It enables Temporal by default and ships
  V8 14.6 (`Map.getOrInsert`, `Iterator.concat`) and undici 8. It becomes Active LTS on
  2026-10-28. On the same schedule Node 24 leaves Active LTS on 2026-10-20. The publisher's own
  guidance is to run Active or Maintenance LTS in production. The macOS laptop is on 26 already,
  and the Linux server is on the maintenance line 22.
- **F5 — Java 25 is the current LTS, and it builds this project.**
  - The build works (measured above), and the bytecode stays Java 17, so the APK's runtime
    is untouched.
  - Support runs to 2031 against 21's 2029.
  - JDK 25's final features pay off directly for a JVM that runs Gradle: Compact Object
    Headers (JEP 519), Ahead-of-Time command-line ergonomics and method profiling (JEPs
    514/515), Generational Shenandoah (JEP 521) and JFR method timing (JEP 520).
  - MADR 0168 chose 21 on 2026-09-23 before this comparison was made; its D2 (bytecode 17) is
    independent of the build JDK and stands.
- **F6 — Flutter 3.47.5 is the newest stable, and its hotfixes bear on this toolchain.**
  - 3.47.2 (libpng) is the last security-relevant hotfix, so every host is already secure.
  - 3.47.3 fixes `flutter doctor` misreporting licenses with Android cmdline-tools 23+. That
    is one reason the hosts pin cmdline-tools 19.0.
  - 3.47.4 handles Windows Smart App Control blocks gracefully.
  - Moving costs little. There is one cross-repo coupling: magic-git pins `FLUTTER_VERSION`
    in `build_macos.sh`, and its 48 goldens fail on any other Flutter.
- **F7 — Python 3.14 is the line to standardize on.**
  - 3.13's bug-fix window closes on 2026-10-01.
  - 3.14 is the newest, with official free-threading (PEP 779), template strings (PEP 750),
    deferred annotations (PEP 649/749), `compression.zstd` (PEP 784) and multiple interpreters
    (PEP 734).
  - The host tooling scripts need Python 3.9 or later, so every line meets them. WSL's 3.12.3
    is Ubuntu's system Python, patched through the distribution.
- **F8 — git 2.55 is the newest stable git, and 2.43 is secure but feature-old.**
  - 2.55 adds configurable parallel hooks, fsmonitor on Linux, `git url-parse`,
    `git format-rev`, pushing to remote groups, `git history fixup`, and Rust enabled by
    default ahead of Git 3.0.
  - 2.56.0 is still a release candidate.
  - WSL's Ubuntu git 2.43 carries the 2025 CVE fixes as distribution patches, but none of the
    features above.
- **F9 — gh and glab are advisory-free at their newest releases, and those releases add
  features the workflows use.**
  - gh 2.98: `pr checkout --worktree` and semantic issue search.
  - gh 2.99: `--attach` of images and video on issues and PRs.
  - gh 2.100: per-host `api_host`.
  - gh 2.101: rotates the Linux APT/RPM signing key, which a fresh WSL install needs.
  - glab 1.117–1.119: `--attach`, draft MR review comments with `mr note publish`, and a
    dependency-firewall proxy for npm, maven, gradle, pnpm and pypi.
  - WSL has neither tool.
- **F10 — nothing keeps this answer current.** Every version above came from a manual
  research pass. The publishers' calendars differ: Go point releases arrive roughly monthly,
  Java has quarterly Critical Patch Updates plus out-of-band fixes, Node publishes security
  releases, Flutter ships hotfixes, and gh and glab release weekly. Without a check that
  compares the hosts to the standard and the standard to the advisory feeds, the Windows
  exposures in F1 are the steady state, not an accident.

## Decision Drivers

- The owner's two tests: supported (LTS where designated) and no known advisory. Stability is
  judged by the publisher's own "stable" or "GA" designation, never a release candidate or
  beta.
- Feature value: prefer the richest line that passes both tests, where the move is proven.
- One standard for all four hosts and CI, as with MADR 0168 and the Go-tool standard.
- Evidence over assertion: every version choice cites a feed, and every compatibility claim is
  either measured or marked **[unverified]** for the plan to establish.
- Exposure first: known-vulnerable installs are fixed before anything cosmetic.

## Considered Options

- **A — A rule, applied per toolchain.** Newest stable patch of the newest supported line, LTS
  where the publisher designates LTS, and no known advisory. The standard follows the rule on a
  schedule, and exposures are fixed at once.
- **B — Conservative floor.** Newest patch of the *oldest* still-supported line: Go 1.26.8,
  Node 22, Java 21, Python 3.13, the distribution's git.
- **C — Leading edge.** The newest release of anything: Go 1.27.1, Node 26 Current, Java 27,
  Flutter beta 3.49, git 2.56-rc.
- **D — Status quo.** Keep what each host has, and patch only what advisories force.

## Decision Outcome

Chosen option: **A**, because it is the owner's rule stated precisely, it picks a single
answer per toolchain, and it re-derives the answer as releases move. B gives up features and up
to two years of support for no security gain, since the newer lines are equally advisory-free.
C breaks the LTS test for Node and Java and the stability test for git and Flutter. D is how F1
happened.

### The decisions

- **D1 — The rule.** For each toolchain, the standard is the newest stable patch of the newest
  line that is inside its support window, is the LTS line where the publisher designates one
  (Node, Java), and has no published advisory affecting it. A newer version with an advisory is
  skipped for the newest one without. A line is adopted only after this project's gates pass on
  it (D12).
- **D2 — Go: 1.27.1** on every host and in CI, with the `go` directive of Go modules moved to
  1.27 repository by repository after each passes its own gates. Until a repository has
  passed, the floor is **1.26.8**: the same security level as 1.26.6, with two later rounds of
  bug fixes. Go 1.26 remains an acceptable fallback until 1.28 ships (~2027-02). The standard
  Go tools (the 11-tool list) are rebuilt with 1.27.1.
- **D3 — Flutter 3.47.5 with its bundled Dart 3.13.4**, in CI (`FLUTTER_VERSION`) and on every
  host. It moves in lockstep with magic-git's `build_macos.sh` pin and goldens. The next
  Flutter stable is adopted by D1 once it is stable, not while in beta.
- **D4 — Node.js: the newest Active LTS.** Today that is **24.21.0**. From **2026-10-28**, when
  Node 26 becomes Active LTS, the standard moves to the newest 26.x. The Linux server leaves 22
  (maintenance) for the Active LTS line. The macOS laptop's 26.x becomes compliant on that
  date.
- **D5 — Java: Temurin 25 LTS (25.0.4.1+1 today)** as the JDK that runs Gradle and Flutter's
  Android build, on every host and in CI (`java-version: "25"`). This amends MADR 0168 D1 and
  D3 (21 → 25). 0168 D2 is unchanged: the bytecode target stays Java 17. The version rolls
  forward at each Critical Patch Update or out-of-band fix (next: 2026-10-20).
- **D6 — Python: 3.14.7** as the developer Python on every host. Distribution system Pythons
  (WSL's 3.12.3) are left to the distribution's security updates and are not replaced.
- **D7 — git: 2.55.x, newest maintenance build.** On Windows that is Git for Windows
  **2.55.0.windows.5**; elsewhere **2.55.0**. WSL gets it from the Ubuntu git-core PPA
  maintained by git's Ubuntu packagers, replacing 2.43. 2.56 is adopted by D1 once it is final.
- **D8 — gh 2.101.0** everywhere (at least 2.98.0 closes every published advisory), installed
  on WSL from the official APT repository with the rotated signing key.
- **D9 — glab 1.119.0** everywhere, installed on WSL from the official release package.
- **D10 — Exposures are remediated first, ahead of every other change.** F1's four Windows
  installs (Node, Temurin, Git for Windows, Python) and the Linux server's Python are patched
  within the current line before any line change: Node 24.21.0, Temurin 21.0.12.1 (then 25
  under D5), Git for Windows 2.55.0.windows.5, and Python 3.14.7.
- **D11 — The standard is data, not prose.** One machine-readable file records, per toolchain:
  the version, the line, the rule's inputs (the lifecycle source and the advisory source) and
  the date it was last verified. CI pins and host installs are checked against it.
- **D12 — A version audit keeps it current.** A read-only tool beside the environment probe
  (magic-git `scripts/tools/devenv/`) does two things:
  - it compares every host's probe output to the standard file;
  - it compares the standard to the publishers' feeds (go.dev, nodejs.org index and schedule,
    Adoptium, the Flutter release feed, endoflife.date) and to the advisory sources (OSV for
    the Go-built tools and stdlib, GitHub advisories for git, Git for Windows, gh and Dart, and
    Node's security flag).

  It reports drift, a newer stable within the rule, and any advisory affecting a standard or
  installed version. It runs on the security calendar (Go point releases, the Oracle Critical
  Patch Update dates, Node security releases) and on demand. It was first shown to fail against
  today's Windows host, which F1 guarantees.

### Consequences

- Good, because the four live exposures close first (D10), independent of any line change.
- Good, because each toolchain ends on its newest supported line: Go 1.27 (supported to
  ~2027-08), Java 25 (to 2031), Node's Active LTS and Python 3.14. That is the richest
  feature set available without leaving a support window.
- Good, because D11 and D12 turn this research into a repeatable check, so the next advisory
  surfaces as a failing audit rather than as a surprise.
- Good, because JDK 25 was measured building the release APK with unchanged bytecode, before
  it was chosen.
- Neutral, because D4 schedules a known second Node move on 2026-10-28, a date set by the
  publisher.
- Bad, because Go 1.27 changes behaviour a repository may depend on:
  - `asynctimerchan` is removed, so `time` channels are always unbuffered;
  - `go test` runs the `stdversion` vet check;
  - `go mod tidy` rewrites require blocks.

  D2's per-repository gate, with 1.26.8 as the floor, exists for this.
- Bad, because Flutter moves in lockstep across two repositories (magic-cli-remote CI and
  magic-git's pinned SDK and goldens), which lengthens that phase.
- Bad, because WSL gains a third-party package source (the git-core PPA) and GitHub's gh APT
  repository. Both are the projects' official channels, but they are outside Ubuntu's archive.

### Confirmation

```sh
# every host matches the standard, and nothing installed or standard carries an advisory (D11, D12):
python3 scripts/tools/devenv/run_probe.py local wsl:<distro> ssh:<host> ssh:<host>
python3 scripts/tools/devenv/version_audit.py        # expect: 0 drift, 0 advisories
# the audit was seen failing first, on the pre-remediation Windows probe (F1)

# Go (D2): toolchain and each module
go version                                          # expect go1.27.1 (or go1.26.8 while a repo is on the floor)
govulncheck ./...                                   # expect no vulnerabilities
make pre-add-check && make race                     # this repository's gates on the new toolchain

# CI pins (D3, D4, D5):
grep -n 'FLUTTER_VERSION\|NODE_VERSION\|java-version' .github/workflows/ci.yml
#   expect "3.47.5", the Active LTS major, "25"; a dispatched android-apk run is green (0168 A4 precedent)
```

## Pros and Cons of the Options

### A — The rule, applied per toolchain (chosen)

- Good, because it is exactly the owner's two tests, plus a tie-break for features.
- Good, because the answer re-derives itself as releases and advisories move (D12).
- Good, because every move is gated on this project's own checks (D1, D2).
- Bad, because it moves more often than B, including the scheduled Node move and Java patch
  rolls.

### B — Conservative floor

- Good, because it changes least today: Go and Java stay on their current lines.
- Bad, because it gives up Go 1.27's language and library features, Java 25's JVM
  improvements, and two years of Java support, for no security gain.
- Bad, because the floor erodes: 1.26 ends ~2027-02 and Node 22 in 2027-04, so B requires an
  unplanned catch-up.

### C — Leading edge

- Good, because it has the most features the day they ship.
- Bad, because it fails the owner's tests. Node 26 is Current until 2026-10-28, Java 27 is not
  LTS, Flutter 3.49 is beta, and git 2.56 is a release candidate.
- Bad, because the pinned tools lag it. gopls predates Go 1.27, and Gradle does not yet run on
  Java 27 ("JVM 27 and later versions are not yet supported").

### D — Status quo

- Good, because it costs nothing now.
- Bad, because it is how four known-vulnerable installs came to sit on the daily-driver host
  (F1), and nothing would detect the next one.

## More Information

### Evidence index

Raw responses are saved with the session's research scripts (`fetch_sources.py`,
`osv_check.py`, `changelogs.py`, `jdk25_trial.py`). The sources:

| Claim | Source |
| --- | --- |
| Go stable versions; 1.27.1 and 1.26.8 newest | <https://go.dev/dl/?mode=json&include=all> |
| Go support policy; 1.26.6 security scope; 1.26.7/.8 bug-fix only | <https://go.dev/doc/devel/release> |
| Go 1.27 features and the macOS 13 minimum | <https://go.dev/doc/go1.27> |
| Go stdlib/toolchain vulnerabilities per version (8 at 1.26.5, 0 at ≥1.26.6) | OSV `POST /v1/query`, ecosystem Go, `stdlib`/`toolchain` (<https://api.osv.dev>) |
| golangci-lint v2.13.0 adds Go 1.27; staticcheck 2026.2 adds Go 1.27 | <https://github.com/golangci/golangci-lint/releases>, <https://github.com/dominikh/go-tools/releases> |
| gofumpt v0.12.0 is based on Go 1.27's gofmt; delve 1.27.x | <https://github.com/mvdan/gofumpt/releases>, <https://github.com/go-delve/delve/releases> |
| gopls latest v0.23.0 (2026-07-07) | <https://proxy.golang.org/golang.org/x/tools/gopls/@latest> |
| Node versions, LTS flags, security flags | <https://nodejs.org/dist/index.json> |
| Node schedule (24/26 LTS dates); production-LTS guidance | <https://raw.githubusercontent.com/nodejs/Release/main/schedule.json>, <https://github.com/nodejs/Release> |
| Node 2026-07-29 security release: 11 CVEs, patched versions | <https://nodejs.org/en/blog/vulnerability/july-2026-security-releases> |
| Node 26 features and breaking changes | <https://nodejs.org/en/blog/release/v26.0.0> |
| Flutter 3.47.5 / Dart 3.13.4 current stable; beta 3.49 | <https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json>, <https://storage.googleapis.com/dart-archive/channels/stable/release/latest/VERSION> |
| Flutter 3.47.x hotfix contents (libpng in 3.47.2) | <https://raw.githubusercontent.com/flutter/flutter/stable/CHANGELOG.md> |
| Dart 3.13.x patch contents | <https://raw.githubusercontent.com/dart-lang/sdk/stable/CHANGELOG.md> |
| Dart / Flutter advisories | GitHub advisories API, `dart-lang/sdk`, `flutter/flutter` |
| Adoptium LTS set, `most_recent_lts: 25`; Temurin 21.0.12.1 / 25.0.4.1 | <https://api.adoptium.net/v3/info/available_releases>, `/v3/assets/latest/{21,25}/hotspot` |
| Temurin / Oracle JDK lifecycles | <https://endoflife.date/api/eclipse-temurin.json>, <https://endoflife.date/api/oracle-jdk.json> |
| 2026-08-18 OpenJDK advisory: 4 CVEs; 25.0.4 and 21.0.12 affected | <https://openjdk.org/groups/vulnerability/advisories/2026-08-18> |
| OpenJDK advisory calendar (Jan/Apr/Jul/Oct, third Tuesday) | <https://openjdk.org/groups/vulnerability/advisories/> |
| Gradle runs on Java 25 from 9.1.0; not yet on 27 | <https://docs.gradle.org/current/userguide/compatibility.html> |
| Kotlin 2.3.0 supports Java 25 | <https://kotlinlang.org/docs/whatsnew23.html> |
| JDK 25 JEPs (519, 514, 515, 521, 520 …) | <https://openjdk.org/projects/jdk/25/> |
| This project builds on Temurin 25.0.4.1 with bytecode 61 | measured, `jdk25_trial.py`, 2026-09-23 (isolated, deleted afterwards) |
| Python lifecycles; 3.14.7 newest | <https://endoflife.date/api/python.json> |
| Python 3.14.7 security entries | <https://docs.python.org/3.14/whatsnew/changelog.html> |
| Python 3.14 features (PEPs 779, 750, 649/749, 784, 734) | <https://docs.python.org/3.14/whatsnew/3.14.html> |
| git tags (2.55.0 newest final; 2.56.0-rc2) | GitHub API `repos/git/git/tags` |
| git/git advisories (newest fixed in 2.50.1) | GitHub advisories API, `git/git` |
| Git for Windows CVE-2026-62960 fixed in 2.55.0.windows.4; wincred fix in .windows.3 | GitHub advisories API and releases, `git-for-windows/git` |
| Ubuntu 24.04 git 2.43.0-1ubuntu7.3 fixes CVE-2025-48384 | <https://ubuntu.com/security/CVE-2025-48384> |
| git 2.55 features | <https://raw.githubusercontent.com/git/git/master/Documentation/RelNotes/2.55.0.adoc> |
| gh releases and features; advisories fixed through 2.98.0 | GitHub API `repos/cli/cli/releases`, `repos/cli/cli/security-advisories` |
| glab releases and features | <https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases> |
| gh / glab have no OSV entries at the listed versions | OSV, ecosystem Go, `github.com/cli/cli/v2`, `gitlab.com/gitlab-org/cli` |
| Host inventory | magic-git `scripts/tools/devenv/` probe, 2026-09-23 |

### Related records

- [0168-MADR](0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md): JDK 21 for
  CI and the hosts, bytecode 17. D5 here amends its D1/D3 choice of 21 and keeps its D2.
- [0114-MADR-manage-markdownlint-cli2-with-mise.md](../spec/0114-MADR-manage-markdownlint-cli2-with-mise.md):
  markdownlint-cli2 through mise. Superseded by D15 (added by the mise-retirement amendment).

### Open questions for the plan

- **gopls and Go 1.27.** Does gopls v0.23.0, built with 1.27.1, type-check generic methods? If
  not, the plan pins the newest gopls that does, or records the gap.
- **Python 3.14.4–3.14.6 Security sections.** They were not extracted here. They do not change
  D6 (3.14.7 contains them), but D10's statement of the Linux server's exposure should be made
  exact.
- **Where the standard file lives** (D11): beside the audit in magic-git, or in each consuming
  repository. The audit reads one file either way.
- **Android cmdline-tools.** Flutter 3.47.3 fixed `doctor`'s license misreport with 23+. Does
  that lift the hosts' pin at 19.0, or does the separate Windows `sdkmanager` NDK-path defect
  still hold it?
- **Rust**, pinned by the acp-go-sdk fork's `mise.toml` for `mdsh`, is outside this record: it
  is a tool dependency of that checkout, not a supported language here.

## Amendment — 2026-09-23: Windows `java` on PATH is 25 from D10 onwards

D10 reads as if JDK 21 stays each host's JDK until D5's rollout ("Temurin 21.0.12.1 (then 25
under D5)"). On Windows, the D10 reinstall put Temurin 25.0.4.1 ahead of 21.0.12.1 on the
machine PATH, where 21 had been first. The owner chose to keep that order, so a plain `java` or
`javac` there is 25.0.4.1 from D10 onwards. The build JDK is unchanged: `JAVA_HOME` and
Flutter's `jdk-dir` name 21.0.12.1 until D5 moves them, so MADR 0168 D1/D3 still describe how
this host builds. PLAN 0169, P0 Deviation 1, records the evidence.

## Amendment — 2026-09-23: Python 3.14.7 is not advisory-free, and the server's Python is Ubuntu's

Two statements above were wrong when written.

- **D6 and F7 assumed 3.14.7 is advisory-free. It is not.** No 3.14.8 exists. Checking the PSF
  advisory database's fix commits against the cpython release tags shows 7 published advisories
  affecting 3.14.7:
  - CVE-2026-15806, CVE-2026-17084 and CVE-2026-15310 are fixed on the 3.14 branch but not
    yet released.
  - CVE-2026-87910 and CVE-2025-15367 have no 3.14 fix commit.
  - CVE-2026-19672 and CVE-2024-3220 have no fix commit.

  Under D1 no Python release qualifies. The owner's decision is to keep **3.14.7**, the least
  exposed release, and adopt 3.14.8 when it ships. D12's audit reports "known advisory, no fixing
  release" as a distinct finding, never as a pass.
- **F1 and D10 said the Linux server's Python 3.14.4 is "patched within the current line" to
  3.14.7.** That interpreter is Ubuntu 26.04's system `python3.14` (`3.14.4-1ubuntu0.2`, the
  newest in the security pocket). It carries 11 backported CVE fixes and lacks 10 that 3.14.7
  has. Under D6 the system Python stays Ubuntu's. The server's **developer** Python becomes a
  ~~mise-managed 3.14.7, which is owner-decided. mise activates `~/default-venv` on it, so the
  venv stays first on PATH.~~ 3.14.7 from a python-build-standalone archive; see the next
  amendment, which retired mise before this could be applied.

PLAN 0169 P0 Deviations 2 and 3 record the evidence. Deviation 2 lists the 10 CVEs Ubuntu has
not backported, and Deviation 3 lists the 7 affecting 3.14.7.

## Amendment — 2026-09-23: retire mise on every host (D13–D15)

The owner decided to retire mise after the trial in PLAN 0169 P0 Deviation 4. mise's
`_.python.venv` activates the venv on PATH, but its shims run the bare interpreter, and this
setup puts the shims first wherever `mise activate` does not run. The decision is folded into
this record because it changes how every host gets the toolchains D2–D9 name.

### What was measured, not assumed (2026-09-23, read-only survey on all four hosts)

- **Linux server.**
  - mise 2026.8.6 provides 11 tools from the dotfiles global config: go 1.26.6, rust 1.96.1,
    flutter 3.47.2, java `temurin-21` (21.0.12.1), node `22` (22.23.2), just 1.58.0, protoc
    35.1, cmake 4.4.2, ninja 1.13.2, glab 1.113.0 and markdownlint-cli2 0.23.2. Five of them
    are pinned as `latest`. The data directory is 7.2 GB.
  - mise is wired in at four places: activation (`.bashrc.d/50-tools.sh`), completion
    (`40-completions.sh`), shims first on PATH (`00-paths.sh`), and the `mcremote` user unit's
    drop-in `path.conf`. The drop-in restates the whole PATH with the shims first, so the
    agents reach glab, node, dart, flutter, cargo, just, protoc, cmake, ninja and java through
    mise.
  - The drop-in also carries two entries that do not exist on this host: `/opt/homebrew/bin`
    and `~/.local/flutter/bin`.
- **WSL.** mise 2026.9.12, 453 MB. `~/.config/devenv.sh` puts the shims first, so `go`, `gofmt`,
  `rustc`, `cargo` and `uv` all resolve through mise.
- **macOS.** The binary only, never activated. 423 MB, all of it the acp-go-sdk fork's tools.
- **Windows.** No mise.
- **The acp-go-sdk fork** (checked out on all four hosts) is tooled by upstream's `mise.toml`:
  - Pins: go 1.26.3 (overridden to 1.26.6 by an untracked `mise.local.toml` of ours), gopls
    0.22.0, golangci-lint 2.12.2, gofumpt 0.10.0, treefmt 2.5.0, actionlint 1.7.12, zizmor
    1.25.2, uv 0.11.17, mdformat 1.0.0 (pipx backend), rust 1.96.0 and mdsh 0.7.0 (cargo
    backend).
  - Its `make check` is `treefmt --fail-on-change`, which drives gofumpt, mdsh, mdformat,
    actionlint and zizmor. So those exact versions decide whether local formatting matches
    upstream's CI.
  - They differ from the host Go-tool standard: gofumpt 0.12.0, golangci-lint 2.13.2 and gopls
    0.23.0.
- **What mise has cost in this project's sessions:**
  - the server resolved Go 1.26.3 against the 1.26.6 standard;
  - five `latest` pins;
  - a hand-written checksum expression was needed for Flutter;
  - the venv shim failure above;
  - the trial's isolated `mise install` still installed Java and Flutter from the real config.
- **This repository.** MADR 0114 put markdownlint-cli2 under mise, amended to the mise-managed
  Linux environment.
- **Every replacement archive publishes a checksum:**
  - go.dev `sha256`, the Flutter releases JSON, the Adoptium API and nodejs.org `SHASUMS256.txt`;
  - Kitware's `SHA-256.txt` (cmake 4.4.2) and just's `SHA256SUMS` (1.58.0);
  - GitHub release-asset digests for protoc 35.1, ninja 1.13.2, treefmt 2.5.0 and
    python-build-standalone 20260901 (`cpython-3.14.7+20260901-x86_64-unknown-linux-gnu-install_only`);
  - glab's `checksums.txt`, and `rustup-init.sha256`.
- **Also found.** The server's `~/.local/bin/git` is a symlink to Ubuntu's `/usr/bin/git`
  2.53.0 (package `git`), not a local build as PLAN P7 assumed.

### The decisions

- **D13 — Retire mise on every host.**
  - Toolchains are publisher archives verified against the publisher's checksum (PLAN C3). On
    Linux they unpack to `~/sdk/<tool><version>` (the WSL layout, already in use there), and
    single-binary tools go in `~/.local/bin`. macOS keeps Homebrew and `~/.local/go<version>`.
  - PATH is set in one place per host: WSL's `devenv.sh`, and the server's dotfiles
    `.bashrc.d/00-paths.sh`, mirrored literally in the `mcremote` drop-in.
  - The declared toolchain moves from mise's config to `standard.json` (D11), enforced by the
    audit (D12). The audit also reports as drift any mise binary, mise data directory, or shims
    directory on PATH.
  - Retirement is **like-for-like**: it changes where each tool lives, not its version. Version
    moves stay in their own phases (P2–P7).
- **D14 — Checkout-pinned tools live in the checkout.**
  - The acp-go-sdk fork's tools install into a git-excluded `.tools/` inside that checkout.
    magic-git `scripts/tools/devenv/fork_tools.py` reads their versions from upstream's
    `mise.toml` and installs each with its native installer, checksum-verified:
    - `go install` for the Go tools;
    - the release asset and its digest for treefmt and uv;
    - `uv tool install` for mdformat and zizmor;
    - `cargo install --locked` for mdsh, with rustup's homes inside `.tools`.
  - `make` runs with `.tools/bin` first.
  - Upstream's `mise.toml` and `mise.lock` are untouched, and our untracked `mise.local.toml`
    is deleted.
  - Host PATH never sees these pins, so the 11-tool Go standard is unchanged.
- **D15 — MADR 0114 is superseded.** markdownlint-cli2 0.23.2 is installed with
  `npm install --global --prefix ~/.local` (the macOS layout). npm checks the registry's
  integrity hashes.
- **Python on the Linux server** (the previous amendment): a python-build-standalone 3.14.7
  archive in `~/sdk/python-3.14.7`, with `~/default-venv` rebuilt on it. The interpreter itself is
  not put on PATH, so the venv stays the only developer Python on PATH.

### Considered options

- **A — Retire mise everywhere (chosen).**
- **B — Keep mise on the server only**, and remove it from WSL and macOS.
- **C — Keep mise, and move `~/default-venv/bin` above the shims** (Deviation 4, resolution 2).
- **D — Replace mise with another version manager** (asdf, aqua, Nix).

### Pros and cons

- **A.**
  - Good, because the Linux hosts share one mechanism, WSL's, which already works.
  - Good, because tool resolution becomes plain PATH, with nothing intercepting it.
  - Good, because the audit already checks versions on every host, so it replaces mise's
    declared config.
  - Bad, because there is no one-command rebuild of a host. Each version move is a
    checksum-verified archive install, scripted per phase.
  - Bad, because the fork's `.tools` carries its own rustup beside the server's.
  - Bad, because matching upstream's formatting now depends on `fork_tools.py`.
- **B.**
  - Good, because it changes the least on the server.
  - Bad, because the drop-in and shim-ordering fragility, the cause of Deviation 4, stays.
  - Bad, because the hosts keep two mechanisms.
- **C.**
  - Good, because it is a one-line fix for Python.
  - Bad, because it edits shell init and the drop-in, and every other problem measured above
    remains.
- **D.**
  - Neutral, because it is the same class of indirection with a different tool.
  - Bad, because it adds a migration and no evidence says the new tool avoids these failures.

### Consequences

- Good, because a tool's location is visible in PATH, and nothing resolves a version at run time.
- Good, because 8.1 GB of mise data leaves the three hosts once each passes (PLAN C4).
- Bad, because the server's `mcremote` drop-in is still a literal PATH snapshot. It must be
  re-derived whenever `00-paths.sh` changes, as it had to be under mise.
- Neutral, because the dotfiles repository's own records that describe mise are the owner's to
  amend (PLAN Deferred).

### Confirmation

```sh
python3 scripts/tools/devenv/version_audit.py            # reports no mise on any host
command -v mise                                          # nothing, on every host
bash -lic 'echo $PATH'; bash -c 'echo $PATH'             # no "mise" in either (Linux server, WSL)
systemctl --user show mcremote -p Environment            # PATH has no "mise"; ~/sdk entries present
PATH="$PWD/.tools/bin:$PATH" make check test             # acp-go-sdk fork, each mise host
```

## Amendment — 2026-09-24: `make preflight` is red, and its install test drives the live service (F11–F22, D16–D21)

P9's verification on the Linux server ran this repository's `make preflight`. Two failures
surfaced: PLAN 0169 Deviations 6 and 7. The owner decided to fix both under this record. This
amendment records what investigating them established. All of it comes from the code, from
driving the scripts and binaries on WSL, the Linux server and the macOS laptop, and from git
history. Nothing here was changed to obtain it.

### What was measured, not assumed

- **The unapproved restart.** On the Linux server, running `scripts/install-binary_test.sh`
  stopped and restarted the live `mcremote` user service at 2026-09-24 02:03:03 UTC.
  - The service journal shows "Stopping mcremote.service", then providers re-initializing.
  - The test printed `FAIL the service was never stopped`, with the output
    `Stopping mcremote.service for install… Starting mcremote.service…`.
  - The journal shows no session or prompt activity in the 33 minutes before, so no live
    session is known to have been cut.
- **systemd exit codes, measured with no shell in between** (Python `subprocess`; an earlier
  measurement through `wsl -- bash -c` was discarded, because the outer shell expands `$?`):

  | Host | `systemctl --user is-system-running` | `is-active mcremote` | effect of `HOME=<fake>` |
  | --- | --- | --- | --- |
  | WSL, systemd 255 (one failed user unit) | `degraded`, **exit 1** | `inactive`, exit 4 | none |
  | Linux server, systemd 259 | `running`, exit 0 | `active`, exit 0 | none |

- **Driving `install-binary.sh` on a degraded manager.** WSL, with a throwaway transient
  unit (`systemd-run --user --unit=mc-installprobe sleep 600`):
  - the script exited 0 and printed `Installed …`;
  - the unit's MainPID was **240055 before and after**: it was never stopped or restarted;
  - nothing warned.
- **Driving `install-binary_test.sh` on three hosts:**
  - macOS: exit 0.
  - WSL: exit 0, but only because the manager is `degraded`, so the script fell to the
    `launchctl` stub.
  - Linux server: FAIL, after restarting the live service (above).
- **Driving `scripts/install_test.sh`.** Its header claims "identical results on a macOS
  workstation and a Linux CI runner".
  - macOS and the Linux server: 139 passed.
  - WSL: 136 passed, 3 failed (cases 22c, 24 and 25).
  - Those three cases do not set the `MC_TEST_OSRELEASE` seam (`install.sh:106`), so on WSL
    they read the real `/proc/sys/kernel/osrelease` ("microsoft"). They then hit the WSL
    suppression of the advisory they assert (`install.sh:858`), which is MADR 0099 F6 working
    as designed.
- **staticcheck 0.8.1** (the host Go-tool standard) over `master` at `62dd2e6`, for GOOS
  linux, darwin and windows, gives **43 unique findings**, 4 of them in `_test.go` files:

  | Check | Count | Where |
  | --- | --- | --- |
  | ST1005 (error string capitalized) | 33 | `internal/provider/codex/*` (32) and `internal/ws/codex_handlers.go:158`. Every one begins "Codex …" |
  | U1000 (unused) | 5 | `codex/session.go:2128` `serverDied`; `codex/threads.go:675` `nativeThreads`; `codex/store_reality.go:147` `describeReality`; `providerauth/store.go:174` `removeGeneration`; `launch/launch.go:83` `maxCommandLineBatch` (linux and darwin only) |
  | SA1019 (deprecated) | 5 | `appdirs/security_windows.go:37` `windows.OpenCurrentProcessToken`; `acpagent/rewind_test.go:484` `parser.ParseDir`; `receipt/jws_test.go:45,46,59` `ecdsa` `X`, `Y` and `D` |

  On the Linux server's older checkout (`1a7702e`) the count was 42 both under mise's Go and
  under `~/sdk`'s Go. So the findings do not come from the toolchain change.
- **git blame** dates the findings' lines from 2026-07-26 (19fe240) to 2026-09-19 (5500d62).
  34 come from the four Codex commits of 2026-08-25. staticcheck joined `make preflight` in
  627eb4b on 2026-07-27. The two SA1019 deprecations are themselves new ("since Go 1.25",
  "since Go 1.26"), so some older code began failing as the toolchain moved.

### Findings

- **F11 — `install-binary_test.sh` is not hermetic.**
  - It replaces `HOME` and puts a stub `launchctl` first on PATH, but leaves the real
    `systemctl` on PATH.
  - `systemctl --user` talks to the real user manager through `XDG_RUNTIME_DIR` and D-Bus,
    and `HOME` has no effect on it (measured on both Linux hosts).
  - On any host whose user manager is `running`, the script under test takes its Linux branch
    against the live unit. On the Linux server that restarted the running daemon.
- **F12 — That test's verdict depends on the host, not the code.**
  - It passes on macOS and on a degraded Linux manager, and fails on a healthy one.
  - The Linux branch of `install-binary.sh` (`is-active`, `is-enabled`, `stop`, `start` at
    lines 78–172) is exercised by no test at all.
- **F13 — `make install` does not restart the service when the user manager is `degraded`.**
  - `detect_service` (`install-binary.sh:63`) requires `is-system-running` to exit 0, and
    `degraded` exits 1. Any single failed user unit therefore turns off service handling: the
    binary is swapped, the old process keeps running the replaced inode, and the install
    exits 0 without a warning.
  - The repository's two other detectors already get this right. `install.sh:149-152` falls
    back to `show-environment`. The Go `preflightLinux` (`internal/cli/service/setup.go:715-721`)
    fails only when the bus cannot be reached.
- **F14 — Three shell test suites have no reliable runner.**
  - CI's `go` job runs gofmt, tidy, vet, the race and cgo-free tests, `verify-units`,
    `verify-build-metadata` and the build (`.github/workflows/ci.yml:84-159`). It runs
    neither staticcheck nor either shell suite.
  - `make preflight` runs staticcheck and `install-binary_test.sh`. Its comment says it
    "mirrors the `go` and `flutter` jobs gate-for-gate … so a green preflight means a green
    CI" (`Makefile:360-362`). That is false in both directions.
  - `install_test.sh` is referenced by nothing but its own usage line.
  - This is how F11–F13, F15 and F18 went unseen: the only runner of each was a local target
    that had been red since 2026-07-27. That last point is inferred from the dates, not
    measured at each commit.
- **F15 — `install_test.sh` is host-dependent in 3 of its 139 cases** (22c, 24, 25). They omit
  the `MC_TEST_OSRELEASE` seam that its other environment cases set. The installer's behaviour
  is correct.
- **F16 — The ST1005 findings are one convention broken in one package.** All 33 strings begin
  "Codex". No Go test and no Dart code matches their text (searched; the only other occurrences
  are copies of the same literal). "Codex provider unavailable" is one message written out 14
  times, and "Codex engine is not running" 3 times. Every other package passes ST1005.
- **F17 — Four of the five U1000 findings are dead code left by a replacement.**
  - `serverDied` was replaced by `engineLost` plus the reconnect loop (b56b00f,
    `provider.go:1002-1064`). Retained sessions are reconciled on the next `ensureEngine`, so
    nothing needs it. Checked: it is not a missing call.
  - `nativeThreads` is a context-less duplicate of `nativeThreadsFor(ctx)`, which is used at
    all 16 native-thread call sites in `threads.go` (f2b8e48).
  - `removeGeneration` was superseded by `pruneGenerations` (518f1df).
  - `maxCommandLineBatch` is declared in the all-platform `launch.go`, but only
    `launch_windows.go` uses it.
- **F18 — The fifth U1000 finding is an unimplemented promise, not dead code.**
  - MADR 0074's amendment (3066d31) says an `external` credential store "is a reason to tell
    the operator the truth".
  - `describeReality` is that explanation. Its only caller is a `//go:build live_codex` test.
  - Driving `mcremote doctor` (0.20.0) on the Linux server and on Windows prints, for codex,
    only `~/.codex/auth.json`. The operator is never told.
- **F19 — The SA1019 findings have direct replacements.**
  - `OpenCurrentProcessToken` becomes `OpenProcessToken(CurrentProcess(), TOKEN_QUERY)`,
    which is the same access, made explicit.
  - `ParseDir` becomes a `ParseFile` walk; ParseDir also ignored build tags.
  - The raw `ecdsa` field use in the JWS test becomes `ParseRawPrivateKey` /
    `ParseUncompressedPublicKey` or `PublicKey.Bytes`. That code is test-only.
- **F20 — staticcheck's version is whatever the host has.** The Makefile calls `staticcheck`
  from PATH (`Makefile:387,533`). There is no `go.mod` tool directive, no configuration and no
  CI step. What decides preflight is the host's Go-tool standard, not the repository.
- **F21 — The shell-quoting trap applies to this project's own WSL test lane.**
  `wsl -- bash -c '…'` re-parses the command through WSL's default shell, so `$?` and other
  expansions happen outside. Exit codes gathered that way are wrong. The first measurement in
  this amendment made exactly that mistake. `wsl -e` or a script file avoids it.
- **F22 — The degraded state is ordinary.** On WSL one failed desktop-portal unit
  (`xdg-desktop-portal-gtk.service`) is enough. Such units fail routinely on headless hosts, so
  F13 is not a corner case.

### The decisions (proposed)

- **D16 — One systemd detection rule across the repository.** `install-binary.sh` treats the
  user manager as usable when `XDG_RUNTIME_DIR` exists and either `is-system-running` or
  `show-environment` succeeds. That is `install.sh`'s rule, so a `degraded` manager still gets
  its service stopped and restarted.
- **D17 — The install tests are hermetic and cover both branches.**
  - `install-binary_test.sh` stubs `systemctl` as well as `launchctl`, using a PATH made only
    of its stub directory, as `install_test.sh` does.
  - It asserts on entry that `command -v systemctl` resolves inside that directory, and exits
    with an error otherwise.
  - It gains Linux cases: a running unit is stopped, swapped and started; a `degraded` manager
    still restarts (F13); an enabled-but-stopped unit is healed.
  - `install_test.sh` cases 22c, 24 and 25 set `MC_TEST_OSRELEASE` to a non-WSL release.
- **D18 — CI runs every gate `make preflight` runs.** The `go` job gains pinned staticcheck,
  `install-binary_test.sh` and `install_test.sh` on Linux. The Makefile's "gate-for-gate"
  comment then becomes true, rather than being deleted.
- **D19 — staticcheck is pinned by the repository.** One Makefile variable,
  `STATICCHECK_VERSION = v0.8.1`, drives `go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)`
  in both the `staticcheck` target and `preflight`, and CI calls that target. A `go.mod` tool
  directive was considered and not chosen: it would add staticcheck's dependencies to this
  module's graph and to govulncheck's scope.
- **D20 — The 43 findings are fixed, not suppressed.**
  - **ST1005:** one sentinel per repeated message (`errCodexUnavailable`,
    `errCodexEngineNotRunning`), and the remaining strings in lower case. The phone shows the
    lower-case text; no client logic depends on it (F16).
  - **Dead code:** `serverDied`, `nativeThreads` and `removeGeneration` are deleted.
    `maxCommandLineBatch` moves to `launch_windows.go`, with its doc links kept.
  - **SA1019:** replaced as in F19.
  - No `staticcheck.conf`, no `//lint:ignore`, and no check removed from preflight.
- **D21 — `describeReality` is wired, not deleted (F18).** `mcremote doctor`'s codex credential
  line gains the observed store reality and, for a non-protected store, `describeReality`'s
  explanation. It still names no path contents and no account. A unit test pins the doctor
  output for each reality.

### Considered options

- **A — Fix the defects, make the tests hermetic, run the same gates in CI (chosen).**
- **B — Suppress and narrow.** Add `staticcheck.conf` disabling ST1005, drop staticcheck and
  `install-binary_test.sh` from preflight, and correct the comment. Rejected: it hides F13, F17
  and F18 behind a check that no longer runs.
- **C — Fix only Deviation 7** (the test and F13), and leave staticcheck red.
- **D — Status quo.**

### Pros and cons

- **A.**
  - Good, because every defect found gets a fix, and each fix a test or gate that runs in CI.
  - Bad, because it touches the codex provider in about eight files, plus doctor, the two shell
    tests, the Makefile and CI.
  - Neutral, because the phone's error text changes case.
- **B.**
  - Good, because it is the smallest diff.
  - Bad, because F13 (a real `make install` failure) and F18 (a documented promise) stay
    broken, and the tests that could catch them stay switched off.
- **C.**
  - Good, because it removes the risk of a live-service restart quickly.
  - Bad, because preflight stays red, so it keeps failing as a gate, and F18 stays open.
- **D.**
  - Bad, because the next person to run `make preflight` on a Linux host restarts their own
    daemon.

### Confirmation (for D16–D21)

```sh
bash scripts/install-binary_test.sh        # passes on macOS, WSL (degraded) and the Linux server; touches no real unit (journal unchanged)
sh scripts/install_test.sh                 # 139 passed on all three
make staticcheck                           # 0 findings, pinned v0.8.1, GOOS linux/darwin/windows
make preflight                             # green on the Linux server
mcremote doctor                            # codex line names the store reality
# CI: the go job runs staticcheck and both shell suites (a dispatched run shows them)
```

Each new check must first be seen failing:
- the hermetic guard, with the real `systemctl` on PATH;
- the degraded case, against today's `detect_service`;
- the pinned staticcheck, on today's tree (43 findings);
- the doctor test, against today's doctor.
