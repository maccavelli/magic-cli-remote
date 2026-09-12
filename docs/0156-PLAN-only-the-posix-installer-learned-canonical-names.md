---
status: completed
date: 2026-09-12
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0156 — Teach the PowerShell installer both manifest shapes, and test it

Implements [0156-MADR-only-the-posix-installer-learned-canonical-names.md](0156-MADR-only-the-posix-installer-learned-canonical-names.md)
decisions D1–D11, closing findings F1–F8 and F11–F14.

D6 and D9 are prohibitions rather than tasks: no phase claims them. D6 (leave
`Get-File` unchanged) is enforced by **C5**, and D9 by **Out of scope** and
**C1**. F9 and F10 are likewise constraints the
plan respects — they are the reasons the release pipeline and the self-updater
are untouched — not findings a phase closes. F7 is closed jointly: P1 adds the
unit test and P2 adds the fixture test, and neither alone is sufficient.

## Goal

1. `scripts/install.ps1` installs both products from the live `latest` release
   on a Windows host, with no arguments and no elevation.
2. The same script still installs from a legacy versioned manifest when
   `-Version` pins a pre-0.16 release.
3. A manifest carrying both shapes for one product is refused, and nothing is
   written to the install directory.
4. A checksum mismatch is refused, and nothing is written to the install
   directory.
5. `scripts/install_ps1_unit_test.ps1` calls `Select-ManifestEntry` directly,
   loaded from the real `install.ps1` without running it, and covers every
   manifest-decision case offline, with no files and no network — passing
   identically under Windows PowerShell 5.1 and PowerShell 7.
6. Each of three deliberate breaks to `Select-ManifestEntry` turns at least one
   named unit case red.
7. `scripts/install_ps1_test.ps1` exercises the end-to-end cases over loopback
   only, with the installer running under 5.1 and under 7. It exits non-zero if
   any case fails, exits non-zero when pointed at the pre-fix script, and never
   hangs: every run finishes, pass or fail.
8. Both tests run in CI on `windows-latest`, under both shells, on every push.
9. `scripts/install.sh` and `scripts/install_test.sh` are byte-identical to
   their state at `HEAD` before this plan.

## Scope

### In scope (the only files any phase may touch)

* `scripts/install.ps1`
* `scripts/install_ps1_unit_test.ps1` (new, P1)
* `scripts/install_ps1_test.ps1` (new, P2)
* `.github/workflows/ci.yml`
* `docs/0156-MADR-only-the-posix-installer-learned-canonical-names.md`
* `docs/0156-PLAN-only-the-posix-installer-learned-canonical-names.md`

### Out of scope

* `scripts/install.sh` and `scripts/install_test.sh`. The POSIX installer is the
  reference implementation here and is correct (MADR 0156 F3). Editing it to
  "match" the new PowerShell code would destroy the reference.
* The release workflow's build, manifest, and bridge steps
  (`.github/workflows/ci.yml` around lines 896-915). D9 forbids it. The only
  workflow change permitted is adding the four test steps (P3).
* `internal/updateclient/*`. It resolves assets by exact name, not through
  `SHA256SUMS` (F10), so it is not implicated.
* `README.md` and `docs/ops-windows-install.md`. The documented command is
  correct and does not change — only the script behind it does.

## Stability rule

This plan changes no Go source, so the cross-build triad is not the relevant
gate. Every phase ends with:

```bash
./scripts/install_test.sh
git diff --stat HEAD -- scripts/install.sh scripts/install_test.sh
```

and, from this Windows host, from P1 onward:

```bash
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

and from P2 onward, additionally:

```bash
timeout 300 powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell
timeout 300 pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh
```

`pwsh` on this host is the Microsoft Store package, `7.6.6` when this plan was
written. Its alias lives in `%LOCALAPPDATA%\Microsoft\WindowsApps`, which a
long-lived shell may not have on `PATH`. If `pwsh` is not found, open a fresh
shell rather than skipping the 7 runs.

The second command of the first block must print nothing. That is the mechanical
check on the out-of-scope rule: the POSIX installer is the oracle this plan is
measured against, and a diff there means the oracle moved.

One commit per phase. **`git push` and tags need an explicit instruction in the
same turn** — this plan authorises neither.

## Cross-cutting contracts

**C1 — No change to `install.sh` or `install_test.sh`.** Enforced by the
stability rule's empty diff.

**C2 — The Windows lookup never becomes more permissive than the POSIX one.**
Every manifest that `install.sh` refuses, `install.ps1` must also refuse. The
converse is allowed; divergence in the permissive direction is not.

**C3 — Failure installs nothing.** Every refusal path must leave the install
directory exactly as it was, including the case where the first product verifies
and the second does not.

**C4 — The tests make no network calls beyond loopback.** Fixtures are served
from `http://127.0.0.1:<port>/` by a listener the test owns. A test that
reaches GitHub is not a test of the lookup; it is a test of the release.

**C5 — No production code exists for the tests' sake.** `Get-File` is not
modified (D6): fixtures reach it over loopback HTTP, which `Invoke-WebRequest`
handles on both 5.1 and 7 without a local-path branch that 7 would need. The
unit test loads functions from the AST (D11) and adds no
guard, variable or early return to `install.ps1`. The only production changes
are the lookup (D1–D4), the extraction (D10) and the docstring (D5).

**C6 — Tests exercise the real script, never a copy.** The unit test parses
`scripts/install.ps1` itself at run time, and the fixture test invokes it by
path. Neither embeds, vendors or regenerates the function under test. A test
that holds its own copy of `Select-ManifestEntry` will stay green while the
shipped script drifts, which is F7 again.

**C7 — `Select-ManifestEntry` is pure.** It takes parameters only: no
`$script:` or implicit script-scope reads, no file or network I/O, no
`Write-Host`. Unit tests would run the function with those variables unset,
so any hidden dependency turns into a difference between test and production
that nobody sees.

**C8 — The fixture harness cannot hang.** The listener is created in the test
process and torn down in a `finally` by calling `Close()` from that same
process. Then the test waits a bounded time for the runspace. Never tear down
with `PowerShell.Stop()`: it does not interrupt a blocked `GetContext()`, and
the probe measured it hanging in 4 of 4 shell combinations until killed (MADR
0156 F13). Every local fixture run is wrapped in `timeout`, and every CI
fixture step carries `timeout-minutes`. A hang would otherwise read as a slow
job rather than a failure, and on a negative-control run it would even look
like the non-zero exit that was wanted.

The contract most at risk is **C3**. The existing install loop verifies both
products first and only then moves them into place, which already satisfies C3 —
but the natural way to write the new tests is to check only that an error was
thrown, and the natural way to fix a failing ambiguity test under time pressure
is to throw earlier, inside the per-product loop, which would leave a
half-installed directory on the second product's failure. The assertion that
the install directory is unchanged must be written into every failure case, not
inferred from the throw.

A close second is **C7**. Reading `$Version` from script scope inside the
function is the one-line way to implement D4, it works in production, and it
yields `$null` in the unit test, where a canonical-without-pin case would pass
for the wrong reason.

## Dependency and delivery order

P1 is the user-visible fix together with its unit test, and stands alone — it
could ship by itself and unblock Windows install today. P2 depends on P1 for a
fixed script to pass against, and P3 depends on both test files existing.
Deliver in order.

The unit test lands in the same commit as the fix because `Select-ManifestEntry`
does not exist before P1, so no earlier commit has anything for it to call. That
creates a risk: a test committed together with its fix hides whether it could
ever have failed. P1 answers this with mutation checks — breaking each rule on
purpose must turn a named case red. P2 answers it the other way, by running the
fixture test against the pre-fix script at a pinned revision. Do not fold P2 into
P1: the two negative controls are the separate evidence that each test works.

## Implementation Steps

### P1 — The dual-shape lookup, and its unit test (D1, D2, D3, D4, D5, D10, D11; closes F1, F2, F3, F5, F6, F11, F12; closes F7 with P2)

Extract the selection out of `Resolve-Product` into a new pure function
`Select-ManifestEntry` in `scripts/install.ps1` (D10). Its parameters are
`-Sums` (string array), `-Product`, `-Arch` and `-PinnedVersion` (may be
empty). It returns an object with `Hash`, `Name`, `Shape` (`canonical` or
`legacy`) and `Version`, and resolves against the manifest's filename field:

* Split each manifest line on whitespace; take the last field as the name.
* Canonical match: name equals `<product>-windows-<arch>.exe`.
* Legacy match: name matches `^<product>-windows-<arch>-[0-9]` — the same digit
  predicate `install.sh:200` uses, resolving MADR 0156 open question 2 in favour
  of parity over strictness.
* Both present for one product: throw, naming both entries, with
  `install.sh`'s refusal wording and `Nothing was installed.`
* Neither present: throw the existing "no checksum entry" error, corrected to
  name both shapes it looked for.
* Version: canonical yields `-PinnedVersion` when non-empty and `''`
  otherwise; legacy slices the name as today. Strip a trailing `.exe` in both
  cases. The pinned version arrives as a parameter, never read from
  `$Version` (C7).
* Trim each line and ignore blank lines, so a CRLF manifest or a trailing
  newline cannot produce a phantom entry or a name ending in `\r`.

`Resolve-Product` keeps its current signature plus the pinned version. It calls
`Select-ManifestEntry`, hashes the download, compares by value, throws on
mismatch as today, and returns the entry's `Version`. The main loop passes
`$Version` in explicitly.

Guard the post-install comparison at `install.ps1:187-190` so it runs only when
the resolved version is non-empty.

Do not touch `Get-File` (D6, C5). MADR 0156 open question 1 was closed by
measurement on both shells: 7 refuses local paths, so P2 serves fixtures over
loopback, which both shells fetch through the unmodified `Get-File`.

Rewrite the `.DESCRIPTION` block (D5) to describe both shapes, state that
canonical is preferred and ambiguity is fatal, and drop the "mirrors
scripts/install.sh exactly" claim in favour of naming what is mirrored.

Add `scripts/install_ps1_unit_test.ps1` (D11). It parses
`scripts/install.ps1` with `[System.Management.Automation.Language.Parser]::ParseFile`,
fails immediately if the parse reports any error, and defines
**by name** only the functions it tests — today just `Select-ManifestEntry`.
If a named function is not found, the test fails with that name instead of
skipping. This resolves MADR 0156 open question 3 in favour of loading by name:
C7 means the function calls no helper, so loading everything would only hide a
C7 violation instead of exposing it. No test framework is introduced. The file
is a plain script with a small assertion helper, and it exits non-zero with the
names of the failing cases, matching the `install_test.sh` convention.

Cases, each calling `Select-ManifestEntry` with in-memory lines:

| # | Manifest lines for the product | Pin | Expect |
| --- | --- | --- | --- |
| U1 | canonical only | — | `Shape=canonical`, `Version=''`, correct hash |
| U2 | canonical only | `0.17.1` | `Version='0.17.1'` |
| U3 | legacy `…-0.14.10.1.exe` only | — | `Shape=legacy`, `Version='0.14.10.1'` (`.exe` stripped) |
| U4 | canonical and legacy | — | throws; message names both entries |
| U5 | neither (other products only) | — | throws `no checksum entry` |
| U6 | `mcrelay-windows-amd64.exe` only, asking for `mcremote` | — | throws — no cross-product match |
| U7 | `mcremote-windows-arm64.exe` only, asking for `amd64` | — | throws — no cross-arch match |
| U8 | `mcremote-windows-amd64.exe.sig` or `xmcremote-windows-amd64.exe` | — | throws — anchored, not substring |
| U9 | `mcremote-windows-amd64-beta.exe` | — | throws — legacy requires a digit |
| U10 | canonical line ending in `\r`, plus blank lines | — | `Shape=canonical`, `Name` has no `\r` |
| U11 | the real `v0.17.1` manifest text quoted in MADR 0156 | — | `mcremote` and `mcrelay` both resolve canonical |

**Verification**

```bash
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1; echo "exit=$?"
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1; echo "exit=$?"
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install.ps1 -WhatIf
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install.ps1 -InstallDir "$env:TEMP\mc0156"
"$env:TEMP\mc0156\mcremote\mcremote.exe" version
```

Expected: `exit=0` with U1–U11 passing; the third command completes with
`mcremote verified` and `mcrelay verified` and installs both; the fourth prints
a version matching `v0.17.1`. Then remove `$env:TEMP\mc0156`.

**Mutation checks — the unit test must be able to fail.** First save the
finished P1 script outside the tree, e.g. to the session scratchpad. Then, one
at a time, apply a break, run the unit test, record which cases turn red, and
restore by copying the saved script back. Do **not** restore with `git checkout`
or `git stash`: until P1 is committed, `HEAD` holds the pre-fix script, so
either command throws away the phase's own work.

| Break | Must turn red (at least) |
| --- | --- |
| M1 — restore the trailing hyphen, so the canonical name is never matched | U1, U2, U11 |
| M2 — delete the ambiguity throw, so canonical silently wins | U4 |
| M3 — delete the `.exe` strip | U3 |

Record the observed red set for each in the commit message. If any break leaves
every case green, the test is not testing that rule and P1 is not done. None of
these breaks is committed.

### P2 — The end-to-end fixture test, over loopback, on both shells (D7; closes F13; closes F7 with P1)

Add `scripts/install_ps1_test.ps1`, following `install_test.sh`'s fixture
approach: build a release tree in a temp directory (`<root>/latest/download/`
and `<root>/download/v<ver>/`), write real binaries (small stub `.exe` files
are sufficient — the install sequence is under test, not execution), and
compute real SHA-256 values.

**Transport: loopback HTTP, the same on both shells (MADR 0156 D7, F13).** Do
not pass the directory as `-BaseUrl`. That works on 5.1 but fails on 7 with
`The 'file' scheme is not supported`. Instead:

1. Pick a free port by binding a `TcpListener` to `127.0.0.1:0`, reading the
   port, and releasing it.
2. Create the `System.Net.HttpListener` **in the test process**, add the prefix
   `http://127.0.0.1:<port>/`, and `Start()` it. Never use `localhost` or `+`.
   `+` needs elevation, and `127.0.0.1` keeps the traffic off any proxy or IPv6
   resolution path.
3. Pass the started listener object into a background runspace
   (`[powershell]::Create()`, which exists on both 5.1 and 7, so the test needs
   no `ThreadJob` module). The runspace answers requests with the file bytes, or
   `404` for a missing path.
4. Run the installer as a **child process**:
   `& $Shell -NoProfile -ExecutionPolicy Bypass -File $Installer -BaseUrl http://127.0.0.1:<port> -InstallDir <scratch> [-Version …]`.
   Capture its exit code and combined output for the case assertions.
5. Tear down in a `finally`: call `Close()` on the listener **from the test
   process**, wait up to 5 s on the runspace handle, then dispose (C8).

The test takes `-Shell` (`powershell` or `pwsh`) to choose the shell the
installer runs under, and `-Installer`, which defaults to
`scripts/install.ps1`, so the same cases can run against any revision of the
script. This does not break C6: the default is the real file, and the parameter
exists only for the negative control below.

These cases do not repeat the unit table. They cover what U1–U11 cannot see:
that `Resolve-Product` really calls `Select-ManifestEntry` with the pinned
version, that downloads flow through the unmodified `Get-File` and real
`Invoke-WebRequest` on each shell, and the C3 ordering.

Cases, each asserting the install directory's state afterwards:

1. Canonical manifest → both products installed.
2. Legacy versioned manifest with `-Version` → both products installed, and the
   resolved version is reported.
3. Both shapes present for `mcremote` → throws; install directory empty.
4. Canonical manifest with a corrupted hash for `mcrelay` → throws; install
   directory empty, including no `mcremote` (this is the C3 case that the
   verify-then-install ordering exists to guarantee).
5. Manifest missing `mcremote` entirely → throws; install directory empty.

Exit non-zero on any failure, printing which case failed.

**Verification**

```bash
timeout 300 powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell; echo "exit=$?"
timeout 300 pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh;       echo "exit=$?"
git show 71bc2e5:scripts/install.ps1 > "$TEMP/install.pre0156.ps1"
timeout 300 powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell -Installer "$TEMP/install.pre0156.ps1"; echo "exit=$?"
timeout 300 pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh       -Installer "$TEMP/install.pre0156.ps1"; echo "exit=$?"
```

Expected: `exit=0` on the first two. On the last two — the test run against the
**pre-P1** installer at pinned revision `71bc2e5` — a non-zero exit **other
than 124** on both shells, with case 1 failing on the original
`no checksum entry` error. Exit 124 means the harness hung, which is a C8
failure and proves nothing about the lookup. A test that passes against the
unfixed script is not testing this bug and must be rewritten before P3.

Also run one cross-shell pairing, since the probe covered all four: host
`powershell` with `-Shell pwsh`. It must give the same results as the matched
runs.

The revision is pinned by SHA on purpose. An earlier draft of this plan used
`git stash push scripts/install.ps1` here. After P1 is committed there is
nothing to stash, so that command would have run the test against the *fixed*
script and reported a green negative control.

The failure **message** is what makes this control meaningful, not just the
exit code. Case 1 must fail on `no checksum entry for mcremote-windows-amd64-*`.
A fetch error would mean the control proved only that the fixture was
unreachable. Loopback makes this safe on both shells: the pre-fix `Get-File`
performs the same `Invoke-WebRequest` over `http://127.0.0.1`, and the loopback
probe returned 200 and 404 correctly in all four host/client shell combinations
(MADR 0156). If a fetch error appears anyway, stop and record it, because it
contradicts that measurement.

### P3 — CI runs both tests under both shells (D8; closes F8, F14)

Add steps to `.github/workflows/ci.yml` running
`scripts/install_ps1_unit_test.ps1` and then `scripts/install_ps1_test.ps1` on
`windows-latest` under **both** shells: four separate named steps (unit/5.1,
unit/7, fixture/5.1, fixture/7), so a red run names both the layer and the
shell. Use `shell: powershell` for the 5.1 steps and `shell: pwsh` for the 7
steps, with `-Shell` passed to match. 5.1 is the documented minimum
(`#Requires -Version 5.1`), and 7 is what Windows Terminal users get (MADR
0156 F14). Each step first prints `$PSVersionTable.PSVersion`, so the log proves
which shell actually ran rather than leaving it to the `shell:` key. That
matters because the version of `pwsh` on the runner image is outside this
plan's control and was not measured here. Give each fixture step an explicit
`timeout-minutes` (C8). Prefer adding the steps to an
existing Windows job over creating a new one; if no existing job is a sensible
host, add a dedicated job with a timeout consistent with its neighbours.

Touch nothing in the release-staging or bridge steps (D9, scope).

**Verification**

```bash
git diff -- .github/workflows/ci.yml
```

Expected: the diff adds the four test steps and changes no line between
`ci.yml:890` and `ci.yml:915`. CI is green on the pushed branch, with both new
steps visibly executing and passing.

## Verification (whole plan)

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `install.ps1` with no arguments installs both products from the live `latest` release | Confirmation 2 |
| A2 | `install.ps1 -Version <pre-0.16>` installs from a legacy manifest | D1, D4 |
| A3 | An ambiguous manifest throws and installs nothing | D2, Confirmation 4 |
| A4 | A checksum mismatch throws and installs nothing, including the already-verified product | D2, C3 |
| A5 | `install_ps1_unit_test.ps1` exits 0 with U1–U11 passing, under both 5.1 and 7 | D11, Confirmation 3 |
| A6 | Mutations M1, M2 and M3 each turn their named unit cases red, recorded in the P1 commit message | D11, Confirmation 3 |
| A7 | The unit test loads `Select-ManifestEntry` from the real `install.ps1` via the AST, and `install.ps1` contains no test guard | C5, C6, F12 |
| A8 | `Select-ManifestEntry` reads no script-scope variable and performs no I/O | D10, C7 |
| A9 | `install_ps1_test.ps1` exits 0 against the fixed script with `-Shell powershell` and with `-Shell pwsh` | D7, Confirmation 4 |
| A10 | `install_ps1_test.ps1 -Installer <71bc2e5 copy>` exits non-zero and not 124 on both shells, with case 1 failing on `no checksum entry` rather than on a fetch error | D7, Confirmation 5 |
| A11 | Neither test performs network I/O beyond `127.0.0.1` | C4 |
| A12 | CI executes both tests under both shells as four named steps on `windows-latest`, each logging `$PSVersionTable.PSVersion` | D8, F14 |
| A13 | `git diff` of `Get-File` across the plan is empty | D6, C5 |
| A14 | `git diff HEAD -- scripts/install.sh scripts/install_test.sh` is empty | D9, C1 |
| A15 | `install.sh` behaviour is unchanged: `./scripts/install_test.sh` passes | Confirmation 6 |
| A16 | Every fixture run finishes without hitting its timeout, and the listener is closed from the test process in a `finally` | C8, F13 |

**A4 is the criterion most likely to be quietly dropped.** It is the only one
requiring a *partial* failure — one product good, one bad — and the only one
whose assertion is about a directory that should be empty rather than about an
error message. It is tempting to treat A3 as covering it, since both are
"throws and installs nothing", but A3 fails before any download and A4 fails
between two products. Only A4 exercises the verify-then-install ordering, which
is the thing C3 actually depends on.

**A6 is the next most likely to be dropped.** Mutation checks leave no artifact
in the tree. Once U1–U11 are green, they feel like evidence enough. But a unit
test written at the same time as its fix is the easiest kind to write so it
passes for the wrong reason, and A6 is the only criterion that catches that. It
is why the observed red sets must go into the commit message and not just be
asserted.

## Rollout and Rollback

Rollout is by commit; no release is required for P1 to reach users, because
`install.ps1` is fetched from `latest/download` at run time — the copy published
with `v0.17.1` is what users execute. **This means P1 does not reach anyone
until the fixed script is published as a release asset.** Decide explicitly
whether to cut a patch release after P1 or to wait; this plan does not authorise
a tag.

Rollback is `git revert` of the phase commits. There is no migration and no
persisted state: the installer writes only the two product directories, and a
reverted installer fails the same way it does today rather than leaving anything
inconsistent.

Users blocked right now can install manually — download the asset and compare
its SHA-256 against the published `SHA256SUMS` by hand. That path is unaffected
by this plan and needs no fix.

## Deferred (named, so they are not mistaken for oversights)

* **A single cross-platform installer (MADR 0156 option D).** The real cure for
  installer divergence, but an installer runs before any project binary exists,
  so it would remain a script in two languages regardless. Revisit only if a
  third platform installer is ever needed; the test from P2 is the cheaper
  guard until then.
* **Making `-WhatIf` exercise the manifest lookup (MADR 0156 open question 4).**
  `-WhatIf` currently returns before fetching `SHA256SUMS`, so it could not have
  predicted this failure. Fixing that changes what `-WhatIf` means — it would
  begin making network calls — which is a decision, not an implementation
  detail. It belongs in its own record.
* **Verifying the stale `update/github.go` reference in `install.sh:231-233`
  (F10).** The path does not exist and the comment is probably left over from a
  rename. Correcting it requires touching `install.sh`, which C1 forbids in this
  plan. Worth a one-line follow-up.
* **Windows PowerShell or `pwsh` on Windows on Arm.** Both shells are now
  tested (P3), but only on amd64. arm64 is not a published target (MADR 0116
  D19), and the installer refuses it before any code under test runs.
* **A release-time smoke test that installs from the actual published release.**
  The gap that let this reach a user is not only the missing unit-level test but
  that nothing installs from a real release before it is announced. That is a
  release-pipeline change, which D9 puts out of scope here, and it deserves its
  own MADR — the interesting question is whether it gates the release or merely
  reports on it.

## Execution record — 2026-09-12

All three phases ran, in order, one commit each, on this Windows host (Windows
PowerShell `5.1.26100.9444`, PowerShell `7.6.6` Store package). Nothing was
pushed; no tag was made.

| Commit | Phase |
| --- | --- |
| `8aa677e` | MADR accepted, plan approved (docs only) |
| `0f2b43e` | P1 — dual-shape lookup, `Select-ManifestEntry`, unit test |
| `0f303c9` | P2 — loopback fixture test |
| `a5057ef` | P3 — four CI steps on `go-native`'s Windows leg |

**Status stays `in-progress`, deliberately.** Every criterion that can be
checked from this host passes, but A12 needs a real Actions run, and this plan
forbids the push that would produce one (see prediction 1). The plan becomes
`completed` once a run on the pushed commits shows the four `Installer …`
steps executing and green.

### Acceptance criteria, as observed

| # | Result | Evidence |
| --- | --- | --- |
| A1 | **Met** | Live install from `v0.17.1` under both shells: both products verified and installed, SHA-256 matched the published manifest, `mcremote version` → `0.17.1 (199c5c4)`. Temp dirs removed. |
| A2 | **Met, by fixture only** | Fixture case 2 (legacy manifest, `-Version 9.9.9.1`). No pre-0.16 release is still published, so there is no live legacy release to install from. |
| A3 | **Met** | U4/U4b; fixture case 3, install dir empty. |
| A4 | **Met** | Fixture case 4: `mcremote verified` logged, then `checksum mismatch for mcrelay`, install dir empty. |
| A5 | **Met** | 32/32 checks, U1–U11, both shells, identical output apart from the version banner. |
| A6 | **Met** | M1 → U1 U2 U4 U4b U5 U10 U10b U11; M2 → U4 U4b; M3 → U3 U3b; identical on both shells; recorded in `0f2b43e`. Each is a superset of the plan's required set. |
| A7 | **Met** | Function loaded via `Parser::ParseFile`; `install.ps1` contains no `InvocationName`/test-env guard (grep). |
| A8 | **Met** | No script-scope or I/O reference in the function body, checked case-insensitively after prediction 6. |
| A9 | **Met** | 32/32 in all four host/child pairings, 3–4 s each. |
| A10 | **Met** | Against `git show 71bc2e5`: exit 1 (not 124) on both shells; case 1 fails on `no checksum entry for mcremote-windows-amd64-* in SHA256SUMS`; zero fetch errors. |
| A11 | **Met** | Case 1 asserts the three downloads were served by the loopback listener. The test has no other network path. |
| A12 | **Not yet observed** | Step bodies run locally with Actions' powershell/pwsh wrapping: all four exit 0 and log the expected shell; a forced-red test turns the step red on both shells. A real run is still required. |
| A13 | **Met** | `Get-File` byte-identical to `8aa677e`. |
| A14 | **Met** | `git diff HEAD -- scripts/install.sh scripts/install_test.sh` empty after every phase. |
| A15 | **Not met as written; held to baseline instead** | See prediction 2. |
| A16 | **Met** | No run hit a timeout; no teardown warning printed; the listener is closed in `finally` by the test process. |

### What the plan predicted incorrectly

1. **P3's verification contradicts the stability rule.** P3 asks for "CI green
   on the pushed branch", and the stability rule authorises no push. A plan
   cannot require both. Future plans that change CI should either authorise a
   push to a branch for verification or name local step simulation as the
   check, and say which.
2. **`./scripts/install_test.sh` cannot pass on a Windows host, and never
   could.** Before any change it exited 1 with 64 ok / 39 FAIL: its stub `PATH`
   breaks Git Bash's own binaries (`error while loading shared libraries`). Two
   baseline runs gave the identical fail set, so every phase was held to "fail
   set identical to baseline" rather than to a pass. This measures C1, which is
   the rule's purpose, but A15 as written was unachievable here. The plan should
   have run its own stability commands before being approved. Separately,
   nothing in CI or any Makefile runs `install_test.sh` (see Deferred below).
3. **The post-install version guard P1 was to add already existed**
   (`if ($resolvedVersion -and …)`). No change was needed.
4. **"Small stub `.exe` files are sufficient" was wrong.** The installer runs
   `mcremote.exe version` after installing, so a non-PE stub throws there and
   fails every success case for a reason unrelated to the test. Fixtures use
   `HOSTNAME.EXE` with a product-specific tail appended: it runs on both shells,
   and each product gets a distinct hash.
5. **The first unit-test design could not satisfy A6.** Under M1 the suite
   aborted at U1 (`ErrorActionPreference Stop`) and named no failing cases. The
   mutation check found this, as it exists to. Non-throwing cases now go through
   `pick`, which reports an unexpected throw and continues.
6. **C7's own check was case-sensitive in a case-insensitive language.** The
   function's local `$version` is the same name as the script's `-Version`.
   Safe as written, but invisible to a case-sensitive grep. Renamed to
   `$resolved`, and the purity check is now case-insensitive.
7. **The five fixture cases did not prove pin wiring.** A legacy entry takes its
   version from the filename, so case 2 passes even if `-Version` never reaches
   the selector. Case 1b (canonical, pinned) was added; a canonical entry only
   reports a version if the pin arrives.
8. **Two harness defects the probes had not surfaced:**
   * On 5.1, `Start-Process -PassThru` reports an empty `ExitCode` unless the
     process handle is opened while the child runs. Three success cases
     reported `exit []` over correct installs.
   * From a PowerShell 7 host, `Start-Process` passes pwsh's `PSModulePath` to
     a Windows PowerShell child, which then cannot find `Get-FileHash`. `&`
     does not do this. The harness removes the variable for that child only,
     reproducing what a user typing `powershell` inside pwsh gets. This affects
     the harness only, not users or the installer.
9. **pwsh writes ANSI colour codes into redirected stderr**
   (`[31;1mException:`). Harmless to the substring assertions, none of which
   span a colour boundary, but worth knowing before asserting on exact error
   text.
10. **A tooling note, not a plan error:** in this Git Bash, `grep -c $'\r$'`
    counts every line of a pure-LF file, which briefly reported LF files as CRLF.
    Byte counts (`tr -cd '\r' | wc -c`) and `git ls-files --eol` were
    authoritative.

### Negative control, and F3 observed directly

Against the pre-fix script, fixture case 3 (both shapes present) showed the old
behaviour F3 describes. Under pwsh the old script logged
`mcremote verified, version 9.9.9.1`: it took the versioned line instead of
refusing, and installed nothing only because `mcrelay` happened to have no
versioned entry.

### Deferred, added by execution

* **`scripts/install_test.sh` is not run by CI or any Makefile target.** The
  POSIX installer's test suite exists but executes nowhere automatically. That
  is the same shape as F7, on the other installer. It needs a Linux lane, and
  touching CI for `install.sh` is outside this plan's scope.
* **`install_test.sh` on Windows hosts.** Its stub-`PATH` approach cannot work
  under Git Bash. Either it declares itself POSIX-only with a clear skip, or the
  stub strategy changes. That belongs with the item above, not here.

## Execution record — 2026-09-12 (second): A12 observed in CI

The owner merged and pushed; CI run `34717428526` ran on `5edaec3`. On the
`Go (windows/amd64)` job, all four installer steps executed and succeeded, and
each logged the shell it actually ran under:

| Step | Step shell | Result |
| --- | --- | --- |
| Installer unit test (Windows PowerShell 5.1) | `5.1.26100.33296 (Desktop)` | 32 passed, 0 failed |
| Installer unit test (PowerShell 7) | `7.6.5 (Core)` | 32 passed, 0 failed |
| Installer fixture test (Windows PowerShell 5.1) | `5.1.26100.33296 (Desktop)` | 32 passed, 0 failed |
| Installer fixture test (PowerShell 7) | `7.6.5 (Core)`, `C:\Program Files\PowerShell\pwsh.exe` | 32 passed, 0 failed |

**A12 is met, and with it every criterion that was not a host limitation.** The
runner's builds differ from this laptop's (5.1 `.33296` versus `.9444`; pwsh
7.6.5 MSI versus 7.6.6 Store package), so the result is not an artefact of one
machine.

The run as a whole was red, but not because of this plan. The only failure was
`TestGuardConfigFileIsFatalWithAnInlineSecret` (MADR/PLAN 0155), on the two
Linux lanes, first executed by the same push. It is recorded and amended in
0155 (`c10d01d`). No 0156 file is involved.

Status: `completed`.
