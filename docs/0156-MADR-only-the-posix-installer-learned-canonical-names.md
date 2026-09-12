---
status: accepted
date: 2026-09-12
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Only the POSIX installer learned canonical asset names

## Context and Problem Statement

The documented Windows install one-liner fails outright against the current
release. It downloads the binary successfully and then refuses to verify it:

```text
install: source https://github.com/maccavelli/magic-cli-remote/releases/latest/download
install: target windows/amd64 -> C:\Users\macsm\AppData\Local\Programs
no checksum entry for mcremote-windows-amd64-* in SHA256SUMS
```

Nothing is installed. The failure is not environmental and not a corrupted
release: the asset and its manifest entry both exist and match. The installer is
looking for a filename shape that this project deliberately stopped publishing.

MADR 0005 moved release manifests to **canonical** asset names — `SHA256SUMS`
lists the exact basename that was downloaded. The previous **legacy** shape
listed version-bearing names (`mcremote-windows-amd64-0.14.10.1.exe`) while the
download URL carried an unversioned alias, so the name never matched and only
the hash value could be compared. `scripts/install.sh` was taught to accept both
shapes. `scripts/install.ps1` was not.

### What was measured, not assumed

* The published manifest for `v0.17.1`, fetched live
  (`curl -sSL .../latest/download/SHA256SUMS`), lists canonical names only:

  ```text
  8ee9588ea4ea47e4769f4c6b57965d18a3719dbc59db2bc665dda95bc29732bf  mcremote-windows-amd64.exe
  49f63af6bf85cfb48cc690c235088df06e692a5954a632d03ad4b0fb706bf323  mcrelay-windows-amd64.exe
  ```

  No entry in the file carries a version segment.
* `gh release list` shows `v0.17.1` (2026-09-08) as the only currently published
  release, and `gh release view` confirms its asset set contains
  `mcremote-windows-amd64.exe` and no versioned Windows asset.
* `scripts/install.ps1:88` builds the lookup prefix as
  `"$Product-windows-$Arch-"` — a hard trailing hyphen — and
  `scripts/install.ps1:162` downloads `"$urlDir/$p-windows-$arch.exe"`, with no
  hyphen. The two lines disagree inside one script.
* `scripts/install.sh:199-208` looks up both shapes, prefers canonical, and
  fails closed when both are present.
* `git merge-base --is-ancestor aed19da 32510a6` exits 0. `aed19da`
  (2026-08-27) is the last commit to touch `install.ps1`; `32510a6`
  (2026-09-02, *"feat(release): adopt MADR 0005 canonical structure and
  publishing"*) is where `install.sh` gained the bridge.
* `.github/workflows/ci.yml:901` copies `install.ps1` into the release staging
  directory. No workflow step executes it. `grep -rln "Pester\|pwsh" .github/workflows/`
  returns nothing.
* `.github/workflows/ci.yml:499` already runs a `windows-latest` job
  (`flutter-windows`).
* `internal/updateclient/client.go:78` selects release assets with
  `selfupdate.NewExactAssetSelector(Platforms)`. `grep -rln "SHA256SUMS" --include=*.go`
  matches only `internal/updateclient/legacy_test.go`.
* `install.ps1` executes its install as soon as it is loaded: the `# main`
  section at `install.ps1:125` is top-level code, so dot-sourcing the file to
  reach its functions would download and install.
* A probe on this host, Windows PowerShell 5.1, parsed `scripts/install.ps1`
  with `[System.Management.Automation.Language.Parser]::ParseFile`: 0 parse
  errors, 7 `FunctionDefinitionAst` nodes found (`Write-Log`, `Write-Warn`,
  `Get-TargetArch`, `Get-UrlDir`, `Get-File`, `Resolve-Product`,
  `Add-ToPathNotice`). Defining `Get-TargetArch` from its extent and calling it
  returned `amd64`, and the main section's `$tmp` variable was never created.
* The same probe showed `$MyInvocation.InvocationName` is `.` when a script is
  dot-sourced and empty when it is run through `iex` — the path the documented
  one-liner uses.
* A second probe on Windows PowerShell `5.1.26100.9444` ran
  `Invoke-WebRequest -Uri <src> -OutFile … -UseBasicParsing` against a local
  file three ways. It returned the file contents for all three: a bare
  backslash path, a `file:///` URI, and the mixed form `C:\…\dir/latest/download/SHA256SUMS`.
  That mixed form is exactly what `Get-UrlDir` produces when `-BaseUrl` is a
  directory.
* The same probe started `System.Net.HttpListener` unelevated. Prefixes
  `http://localhost:48156/` and `http://127.0.0.1:48157/` started. The wildcard
  `http://+:48158/` failed with `Access is denied`.
* PowerShell 7 was then installed on this host: `pwsh` `7.6.6`, edition Core,
  the Microsoft Store package
  (`C:\Program Files\WindowsApps\Microsoft.PowerShell_7.6.6.0_x64__8wekyb3d8bbwe\pwsh.exe`,
  aliased from `%LOCALAPPDATA%\Microsoft\WindowsApps`). The same probes, re-run
  under it, gave these results:
  * **AST loading is identical:** 0 parse errors, the same 7 functions,
    `Get-TargetArch` returned `amd64`, and the main section did not run.
    `InvocationName` is again `.` when dot-sourced and empty under `iex`.
  * **`Invoke-WebRequest` refuses local files.** All three forms (bare path,
    `file:///` URI, mixed-slash) threw `NotSupportedException: The 'file'
    scheme is not supported.` So fixture directories that work on 5.1 do not
    work on 7.
  * **Loopback `HttpListener` behaves as on 5.1:** `localhost` and
    `127.0.0.1` start unelevated, and `+` is `Access is denied.`
* A loopback round-trip probe served a fixture release tree from an
  `HttpListener` on `127.0.0.1` with a free port, in a background runspace. The
  client ran as a **separate process**. It passed in all four host/client
  combinations of 5.1 and 7: a `200` returned the file's exact contents, and a
  missing path returned `404`. The two versions word the 404 error differently
  (`The remote server returned an error: (404) Not Found.` versus
  `Response status code does not indicate success: 404 (Not Found).`).
* In that probe, teardown by `PowerShell.Stop()` on the serving runspace
  **hung**: 4 of 4 combinations were killed by a 45 s timeout (exit 124),
  because `Stop()` does not interrupt a blocked `GetContext()`. Creating the
  listener in the host and calling `Close()` from the host ended the runspace in
  4 ms (5.1) and 6 ms (7), exit 0.
* `scripts/install.ps1 -WhatIf` runs to completion with exit 0 under both 5.1
  and 7.6.6, and prints identical output.
* `Resolve-Product` (`install.ps1:81-113`) does three things in one function:
  selects the manifest line, hashes a file on disk, and slices a version from
  the name.

### Findings

**F1 — `install.ps1` contradicts itself.** It downloads the canonical asset and
then requires a legacy manifest entry for it. Under any single published
manifest shape, one of the two lines is wrong. Today it is the lookup.

**F2 — The current release cannot satisfy that lookup.** `v0.17.1` publishes
canonical names only, so the prefix match at `install.ps1:88` can never
succeed. Windows install via the documented one-liner is broken, not degraded.

**F3 — `install.sh` already holds the correct design.** It resolves canonical
first, falls back to legacy, and refuses to choose when a manifest carries both,
because an appended canonical line would otherwise shadow a real versioned entry
and authorise a substituted binary. `install.ps1` has no equivalent and would
take the first regex hit.

**F4 — This is drift, not a design disagreement.** `install.ps1` predates the
canonical migration by six days and was never revisited. No record decided that
Windows should keep the legacy-only lookup.

**F5 — The script's own documentation asserts the broken behaviour as correct.**
`install.ps1:10-15` states that `SHA256SUMS` lists versioned names and that the
script *"mirrors scripts/install.sh exactly"*. Both claims are now false. A
reader auditing the script against its docstring would conclude it is right.

**F6 — Canonical names destroy the version-resolution mechanism.**
`Resolve-Product` derives the installed version by slicing the version out of
the manifest filename. A canonical entry has no version to slice.
`install.sh:224-231` already answers this: fall back to the pinned tag when one
was requested, otherwise leave it empty rather than inventing a version, and
strip a trailing `.exe` because the extension comes last (Convention C5, MADR
0116 F17).

**F7 — Nothing tests `install.ps1`.** `install_test.sh` covers `install.sh`
only. CI ships `install.ps1` as a release asset without ever running it. That is
why a six-day-old drift survived a release and reached a user as a hard failure.

**F8 — Testing it needs no new CI infrastructure.** A `windows-latest` runner is
already in the workflow.

**F9 — The release side is behaving as designed; do not "fix" it.** The v0.16.0
bridge was scoped by an explicit conditional to that one tag and aimed at the
pre-0.16 client's self-updater, and the workflow comment records that
`SHA256SUMS` *"keeps listing canonical names only"*. Republishing versioned
entries to appease the installer would reintroduce exactly the ambiguous
manifest that F3's fail-closed rule exists to reject.

**F10 — The self-updater is unaffected.** It matches assets by exact name
through `NewExactAssetSelector`, not by parsing `SHA256SUMS`. The comment at
`install.sh:231-233` naming `update/github.go` as a third parser of this format
appears stale — no such path exists and no non-test Go file references
`SHA256SUMS`. **[unverified]** that the updater has no other manifest
dependency; the plan does not touch it either way.

**F11 — The lookup cannot be tested in isolation as written.** `Resolve-Product`
mixes the decision under test (which manifest line, which shape, ambiguous or
not, what version) with file I/O (hashing the download). D4 would also make it
read `$Version` from script scope. A test of the ambiguity rule should not need
a binary on disk, and it should not depend on a variable set elsewhere.

**F12 — The functions can be loaded without a production test hook.** Loading
by dot-source runs an install, but parsing the file and defining functions from
their AST extents does not (probe above). So unit tests need no guard added to
`install.ps1` and no test-only mode. That matters because the obvious guard,
`if ($MyInvocation.InvocationName -eq '.') { return }`, adds a new early-exit
branch to a script that users run through `iex`. That is exactly the kind of
production seam this project avoids (PLAN C5). The AST route was measured on
both 5.1 and 7.6.6.

**F13 — Only a loopback server gives both shells the same fixture transport.**
A fixture directory passed as `-BaseUrl` works on 5.1 and fails on 7 with
`The 'file' scheme is not supported`. Supporting 7 that way would need a
local-path branch in `Get-File`, which is production code for the tests' sake. A
loopback `HttpListener` works identically as server and client on both
versions, needs no elevation on `127.0.0.1`, and exercises the real
`Invoke-WebRequest` path that users hit. It has one proven failure mode:
stopping the serving runspace hangs, and closing the listener from the host
does not.

**F14 — PowerShell 7 is a real user path, and nothing checks it.** The
documented one-liner is `irm … | iex`, and it runs in whichever shell the user
opened. Windows Terminal defaults to `pwsh` once it is installed. The installer
starts cleanly under 7.6.6 (`-WhatIf`, exit 0), but no test runs it under 7.

## Decision Drivers

* A published installer that fails on its first line is the worst class of
  defect: it is the first thing a new user runs.
* Divergence between two installers that claim to mirror each other is a
  standing source of exactly this bug.
* Manifest verification is a security boundary. A fix that makes the Windows
  path *more* permissive than the POSIX path is worse than the current failure,
  which at least fails closed.
* The drift survived a release because nothing executed the script. A fix
  without a test rebuilds the same trap.
* The release pipeline is correct and is depended on by the v0.16.0 bridge.

## Considered Options

* **A — Port the `install.sh` bridge to `install.ps1`, and test it.**
* **B — Make `install.ps1` canonical-only.**
* **C — Republish versioned entries in `SHA256SUMS`.**
* **D — Replace both installers with one implementation.**

## Decision Outcome

Chosen: **A — port the bridge and test it**, because it restores Windows install
by making the two installers agree on the security-relevant behaviour that one
of them already gets right, and it closes the gap that let the divergence ship.

### The decisions

**D1 — Give `install.ps1` the dual-shape lookup.** Resolve the canonical entry
`<product>-windows-<arch>.exe` and the legacy entry
`<product>-windows-<arch>-<digit>…` independently. Prefer canonical.

**D2 — Fail closed on an ambiguous manifest.** If both shapes are present for
one product, install nothing and exit with an error naming both. Match
`install.sh`'s wording and its refusal to pick a winner.

**D3 — Anchor the matches.** Resolve against the manifest's filename field, not
a substring of the whole line, so that a prefix cannot match inside an unrelated
entry. The legacy form requires a digit after the hyphen.

**D4 — Resolve the version the way `install.sh` does.** A canonical entry yields
the pinned version when `-Version` was given and an empty string otherwise;
never invent one. Keep the trailing-`.exe` strip. The post-install comparison
runs only when a version is known.

**D5 — Correct the `.DESCRIPTION` block.** State that both manifest shapes are
accepted, which is preferred, and that an ambiguous manifest is fatal. Remove
the claim that the script mirrors `install.sh` exactly and replace it with the
specific behaviours that are mirrored.

**D6 — Add no fetch seam; leave `Get-File` unchanged.** An earlier draft of this
record proposed a local-path branch in `Get-File`, mirroring `fetch()` in
`install.sh`, on the assumption that `Invoke-WebRequest` could not read a
fixture directory. It can: on Windows PowerShell 5.1 it reads a bare local
path, a `file:///` URI, and the mixed-slash path `Get-UrlDir` builds (measured
above). PowerShell 7 cannot (F13), and that would have brought the branch
back. D7 serves fixtures over loopback instead, so neither shell needs it, and
it stays dropped.

**D7 — Add `scripts/install_ps1_test.ps1`, the end-to-end fixture test.** Build a
fixture release tree and serve it from an `HttpListener` on
`http://127.0.0.1:<free port>/`. The listener runs in a background runspace but
is created and closed by the test process itself (F13). Run the whole installer
against it as a child process under the shell being tested (`-Shell powershell`
or `-Shell pwsh`), and assert: canonical manifest installs; legacy manifest
installs; ambiguous manifest fails with nothing installed; checksum mismatch
fails with nothing installed; a missing entry fails. The test takes the
installer path as a parameter, so it can also be run against a historical copy
of the script.

**D8 — Run both test files in CI on `windows-latest`, under both Windows
PowerShell 5.1 and PowerShell 7 (F14).**

**D9 — Change nothing in the release pipeline or the manifest shape.**

**D10 — Extract the selection into a pure function.** Move the manifest decision
out of `Resolve-Product` into `Select-ManifestEntry`, which takes the manifest
lines, product, arch and pinned version as parameters. It returns the expected
hash, the matched name, the shape (`canonical` or `legacy`) and the resolved
version, or throws. It performs no I/O and reads no script-scope variable.
`Resolve-Product` keeps the hashing and the mismatch error and calls it.

**D11 — Add `scripts/install_ps1_unit_test.ps1`, the unit test.** It loads the
functions it needs by parsing the real `scripts/install.ps1` and defining them
from their AST extents. It never dot-sources the script and never works on a
copy. It calls `Select-ManifestEntry` directly with in-memory manifest lines.
There is no production guard, flag or environment variable for tests (F12).
Cases cover at least: canonical only; legacy only; both shapes (throws);
neither (throws); a canonical name for another product or arch that must not
match; legacy name with `.exe` stripped from the version; canonical with and
without a pinned version; and extra whitespace or a trailing carriage return on
a manifest line. It must pass unchanged under both 5.1 and 7. It uses nothing
that differs between them, so any difference between the two runs is a finding,
not something to explain away.

The two test files check different things, and both are required. The unit test
pins the decision logic exhaustively and cheaply. The fixture test (D7) pins
what the unit test cannot see: that `Resolve-Product` actually calls the pure
function, that downloads are verified before anything is installed, and that a
partial failure leaves the install directory untouched.

### Consequences

* Good: the documented Windows one-liner works against `v0.17.1` and against any
  future canonical release.
* Good: pinning an old pre-0.16 release with `-Version` keeps working, because
  the legacy shape stays supported rather than being dropped.
* Good: the Windows installer gains the fail-closed ambiguity rule it has never
  had, so the fix raises rather than lowers the security floor.
* Good: `install.ps1` becomes executable under test, ending the class of drift
  that produced this.
* Good: the security-relevant decision gets a fast unit test with no files and
  no network, and edge cases such as CRLF manifests or a near-miss product name
  can be added for one line each rather than a whole fixture tree.
* Neutral: `Resolve-Product` is restructured, not only patched. Its outward
  behaviour is pinned by the fixture test, which is why D7 is not optional
  once D10 lands.
* Bad: loading functions from the AST depends on the functions being
  self-contained. If one later reads a script-scope variable, the unit test
  sees `$null` where production sees a value. D10's "reads no script-scope
  variable" rule is what keeps that from being silent, and the fixture test is
  the backstop.
* Neutral: the two installers must now be kept in step deliberately. The test is
  what makes that a detectable obligation rather than a hope.
* Bad: CI gains four Windows steps (two tests × two shells), with the runtime
  and flake surface that implies. Mitigated by fixtures that never leave
  loopback, so no step depends on GitHub or the internet.
* Bad: the fixture test now runs a small HTTP server and a child process, which
  is more moving parts than a directory of files. One of those parts was
  measured to hang CI when torn down the obvious way (F13). That is a known trap
  the plan has to guard with a contract and a timeout, not a risk it can
  ignore.
* Good: PowerShell 7, the shell many Windows users will actually run the
  one-liner from, is tested for the first time (F14).
* Bad: `Resolve-Product` may now return an empty version, so the post-install
  version comparison silently does not run for an unpinned canonical install.
  That is `install.sh`'s existing behaviour and is preferred to inventing a
  version, but it does mean one check quietly weakens on the common path.

### Confirmation

```bash
# 1. The lookup accepts what is actually published.
curl -sSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/SHA256SUMS | grep -E '  mcremote-windows-amd64\.exe$'

# 2. The installer completes against the live release.
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install.ps1 -WhatIf

# 3. The unit test passes, under both shells.
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1

# 4. The fixture test passes, including the fail-closed cases, under both shells.
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh

# 5. The fixture test fails against the script as it was before the fix, under both shells.
git show 71bc2e5:scripts/install.ps1 > "$TEMP/install.pre0156.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell -Installer "$TEMP/install.pre0156.ps1"
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh -Installer "$TEMP/install.pre0156.ps1"

# 6. The POSIX installer is unchanged in behaviour.
./scripts/install_test.sh
```

Expected: (1) prints one line; (2) reports source, target and both products with
no throw; (3) every case passes on both shells; (4) every case passes on both
shells, the ambiguous and mismatch cases report that nothing was installed, and
each run exits within its timeout; (5) exits non-zero on both shells, with the
canonical case failing on `no checksum entry` and not on a fetch error; (6)
passes exactly as before this change.

The unit test also has to show that it can fail. Deliberately breaking the
function one rule at a time must turn at least one named case red for each
break: restoring the trailing hyphen on the canonical name, removing the
ambiguity throw, and dropping the `.exe` strip. A unit test that stays green
through all three is not testing D1–D4.

## Pros and Cons of the Options

### A — Port the `install.sh` bridge to `install.ps1`, and test it (chosen)

* Good: restores Windows install without touching the release pipeline.
* Good: brings the fail-closed ambiguity rule to Windows for the first time.
* Good: keeps `-Version` against pre-0.16 releases working.
* Good: the tests close F7, which is the reason this shipped. There are two
  layers: a unit test for the decision and a fixture test for the install
  sequence around it.
* Neutral: duplicates logic in two languages; the tests are the anti-drift
  device.
* Bad: the largest change of the options considered, made larger by the D10
  extraction and a second test file.

### B — Make `install.ps1` canonical-only

* Good: the smallest possible diff — delete one hyphen and the version slice.
* Good: matches what is actually published today, with no bridge to maintain.
* Good: no ambiguity rule needed, because only one shape is ever accepted.
* The strongest argument for it: the legacy shape is dead. Only `v0.17.1` is
  published, the bridge was fired once at `v0.16.0` and never again, and
  carrying a second code path for filenames nothing will ever emit again is
  speculative generality in a security-sensitive lookup — where the *simpler*
  parser is the more auditable one.
* Bad: silently breaks `-Version` against any pre-0.16 release a user has
  pinned, turning a documented flag into a trap.
* Bad: leaves the two installers structurally different, so the next manifest
  change has the same chance of hitting only one of them.
* Rejected because it trades a known, currently-supported capability for a
  smaller diff, and does nothing about F7 — the actual cause.

### C — Republish versioned entries in `SHA256SUMS`

* Good: fixes every already-installed broken `install.ps1` retroactively, which
  no client-side change can do.
* Bad: produces exactly the both-shapes manifest that `install.sh` is written to
  reject, so it would break the POSIX installer to fix the Windows one.
* Bad: contradicts MADR 0005 and the deliberate v0.16.0-only bridge (F9).
* Rejected: it inverts a settled decision to work around a client bug.

### D — Replace both installers with one implementation

* Good: removes the divergence permanently rather than testing for it.
* Bad: an installer must run before any project binary exists, so the shared
  implementation would have to be a script anyway.
* Bad: far beyond the scope of a broken release, with no user unblocked sooner.
* Rejected as disproportionate; noted in the plan's Deferred section.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Live manifest lists canonical names only | `curl -sSL .../latest/download/SHA256SUMS` |
| `v0.17.1` is the only published release | `gh release list --repo maccavelli/magic-cli-remote` |
| Release assets include `mcremote-windows-amd64.exe` | `gh release view --json assets` |
| Lookup prefix carries a trailing hyphen | `scripts/install.ps1:88` |
| Download URL carries no hyphen | `scripts/install.ps1:162` |
| Docstring asserts the legacy shape and exact mirroring | `scripts/install.ps1:10-15` |
| Dual-shape lookup and fail-closed rule | `scripts/install.sh:178-210` |
| Canonical version fallback, never invented | `scripts/install.sh:224-231` |
| `fetch()` copies for a local absolute path | `scripts/install.sh` `fetch()` |
| Test seam is a plain directory in `MC_TEST_BASE_URL` | `scripts/install_test.sh:280`, `scripts/install.sh:1027` |
| `install.ps1` predates canonical publishing | `git merge-base --is-ancestor aed19da 32510a6` (exit 0) |
| Canonical publishing commit | `32510a6` 2026-09-02 |
| Last `install.ps1` commit | `aed19da` 2026-08-27 |
| CI ships but never runs `install.ps1` | `.github/workflows/ci.yml:901` |
| A `windows-latest` runner already exists | `.github/workflows/ci.yml:499` |
| Bridge is scoped to the `v0.16.0` tag only | `.github/workflows/ci.yml:904-912` |
| Self-updater matches assets by exact name | `internal/updateclient/client.go:78` |
| No non-test Go file parses `SHA256SUMS` | `grep -rln "SHA256SUMS" --include=*.go` |
| Main section runs at load, so dot-sourcing installs | `scripts/install.ps1:125` onward |
| AST parse finds all 7 functions, 0 errors, main not run | PS 5.1 probe, `Parser::ParseFile` on `scripts/install.ps1`, 2026-09-12 |
| `InvocationName` is `.` when dot-sourced, empty under `iex` | PS 5.1 probe, 2026-09-12 |
| `Resolve-Product` mixes selection, hashing and version slicing | `scripts/install.ps1:81-113` |
| PS 5.1 `Invoke-WebRequest` reads bare, `file:///` and mixed-slash local paths | PS `5.1.26100.9444` probe, 2026-09-12 |
| Unelevated `HttpListener` binds `localhost`/`127.0.0.1`, not `+` | PS 5.1 probe, 2026-09-12 |
| `pwsh` was absent at first, then installed as Store package 7.6.6 | `where pwsh`; `$PSVersionTable` and process path, 2026-09-12 |
| AST function loading is identical on 7.6.6 | PS 7.6.6 probe, 2026-09-12 |
| PS 7.6.6 `Invoke-WebRequest` rejects local files: `The 'file' scheme is not supported` | PS 7.6.6 probe, 3 path forms, 2026-09-12 |
| Loopback round-trip (200 and 404) works in all 4 host/client shell combinations | loopback probe, 2026-09-12 |
| `PowerShell.Stop()` teardown hangs (4/4, exit 124 at 45 s); host-side `Close()` completes in 4–6 ms | loopback probe v1 vs v2, 2026-09-12 |
| `install.ps1 -WhatIf` exits 0 with identical output on 5.1 and 7.6.6 | run on this host, 2026-09-12 |
| Pre-fix script revision for the negative control | `71bc2e5` (HEAD when this record was written) |

### Related records

* **MADR 0005** — canonical release structure and publishing; the decision
  `install.ps1` never caught up with.
* **MADR 0116** — Windows and linux/arm64 build targets; origin of
  `install.ps1`, of the `%LOCALAPPDATA%\Programs` location (D13), of the
  unsigned-binary warning (D14), and of Convention C5 / F17, the
  extension-comes-last rule that both installers implement.
* **MADR 0120** — retiring darwin/amd64; touches the same installer docs.

### Open questions for the plan

1. ~~Can `Invoke-WebRequest` be avoided cleanly for fixtures?~~ **Closed by
   measurement, twice.** On 5.1 it reads local fixture paths, but on 7.6.6 it
   refuses the `file` scheme. So the fixture test avoids local paths entirely
   and serves over loopback (D7, F13). Neither shell needs a seam, and the
   pre-fix script, which has none, can be driven the same way for the negative
   control.
2. Should the legacy regex require the digit run to be a plausible version
   (`[0-9]+(\.[0-9]+)*`) rather than a single leading digit? `install.sh` uses
   `-[0-9]`. Matching it exactly is the safer default; diverging to be stricter
   would be a new divergence.
3. Should the unit test load functions by name from the AST, or load every
   function in the file? Loading by name keeps the test explicit about what it
   exercises. Loading all of them avoids a missing-helper failure if
   `Select-ManifestEntry` later calls another function. The plan should choose,
   and a function the test expects but cannot find must fail loudly instead of
   being skipped.
4. Does `-WhatIf` need to exercise the lookup? It currently returns before
   downloading `SHA256SUMS`, so it cannot detect this class of failure at all.
   Worth deciding, not assuming.
