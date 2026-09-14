---
status: in-progress
date: 2026-09-06
associated-madr: "0145-MADR-local-windows-ci-style-tests.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement: Local CI-style Windows tests

Associated MADR: [0145-MADR-local-windows-ci-style-tests.md](0145-MADR-local-windows-ci-style-tests.md).

## Goal

On a **Windows host**, `make ci-windows` predicts CI `go-native`
windows/amd64 (hard fail, no retry); `make ci-windows-smoke` covers light
release-shaped `version` stamps; `acceptance-windows.ps1` keeps functional
F5/paths/doctor with hardened JSON asserts.

On **macOS/Linux**, those Make targets **skip with a clear message and exit 0**
(Mac works three OS clones; shared habits must not break). Unix hosts keep
`make preflight`. No `.github/workflows` edits. Never register the Windows dev host as a GHA
self-hosted runner (F20). No cross-OS auto-hook that invokes `ci-windows` off-Windows.

**Done:** Windows host runs locked checklists; non-Windows skip+0; `fb7305e`
fails `ci-windows` on Windows; docs state Windows-host-only; drift checklist
vs `ci.yml` in-repo.

## Scope

**In:** `scripts/ci-windows-local.ps1` (with host guard); Make `ci-windows` /
`ci-windows-smoke` (host-gated); harden `acceptance-windows.ps1`; docs pointer.

**Out:** workflow changes; self-hosted runner; 0144/#23 fix; Flutter/`preflight`;
day-1 package-subset mode; running PowerShell Windows gates on macOS/Linux.

## Prerequisites

* **Windows dev host:** PowerShell 5.1+, Git, Go satisfying `go.mod`; symlink
  privilege for `MC_REQUIRE_SYMLINK=1`.
* **macOS/Linux:** Make only — targets must skip+exit 0 without PowerShell.
* **Mac proceed/execute** on this PLAN before implement.

## Host detection (required)

**Makefile** (prefer existing `HOST_GOOS` from uname / MINGW|MSYS|CYGWIN):

```make
ci-windows:
	@if [ "$(HOST_GOOS)" != "windows" ]; then \
	  echo "Windows-only; skipping on $(HOST_GOOS) (use make preflight on unix)"; \
	  exit 0; \
	fi
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1

ci-windows-smoke:
	@if [ "$(HOST_GOOS)" != "windows" ]; then \
	  echo "Windows-only; skipping on $(HOST_GOOS) (use make preflight on unix)"; \
	  exit 0; \
	fi
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1 -Smoke
```

**Script guard** (defense in depth — even if invoked directly off-Windows):

```powershell
# At top of scripts/ci-windows-local.ps1 after #Requires
# Also accept: [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform(...)
if (-not $IsWindows -and $env:OS -notmatch 'Windows') {
  Write-Host "Windows-only; skipping on $([System.Runtime.InteropServices.RuntimeInformation]::OSDescription)"
  exit 0
}
```

Do **not** add git hooks / agent gates that auto-run `ci-windows` on non-Windows.

## Script `scripts/ci-windows-local.ps1` (Windows path)

Flags: `-UnitOnly` (A only), `-Smoke` (B), `-RetryOnce` (not default Make), `-SkipTests`.

Default (no flags): section A. Exit 0 only if every selected check passed.

Toolchain (A3): read `go` directive from `go.mod`; must satisfy; else FAIL.

Env: `CGO_ENABLED=0`, `MC_REQUIRE_SYMLINK=1`. Symlink probe; failure → FAIL.

## Checklist A — `make ci-windows` (Windows host)

| ID | Predicate |
| --- | --- |
| A0 | Host is Windows. Else: one clear skip line (“Windows-only; skipping on <os>”) + exit 0 — **not** a checklist fail, **not** a PASS (message required so nobody thinks the Windows gates ran); do not run go test under a fake Windows env |
| A1 | `CGO_ENABLED=0` for all go invocations |
| A2 | `MC_REQUIRE_SYMLINK=1`; symlink probe succeeds else FAIL |
| A3 | Go satisfies `go.mod` |
| A4 | `go build ./...` exit 0 |
| A5 | `go vet ./...` exit 0 |
| A6 | `go test ./...` full module, no allowlist, exit 0 |
| A7 | No `-race` |
| A8 | No retry unless `-RetryOnce` |
| A9 | No `-tags live_*` |
| A10 | Confirmation: commit `fb7305e` (PR #23 before the 0144 fix) → FAIL on Windows |
| A11 | Commit `11e1560` (the 0144 fix) → PASS on the Windows dev host |

Non-asserts: paths/pair/doctor/Ctrl+C/Flutter/GH download.
A10/A11 confirmation is Windows-only (N/A off-Windows — do not attempt).

A10/A11 name commits, not "`#23` tip". When this plan was written the tip of
PR #23 was `fb7305e` and it failed Windows; the 0144 fix then landed on that
same branch, so "tip" now means the green commit and the predicate inverted
without anything being edited. Pinned SHAs cannot rot that way, and once #23
merges the branch tip stops existing at all. Reference evidence, both on
`Go (windows/amd64)`:

* `fb7305e` — run `34011907466`, `--- FAIL: TestRedactAbsoluteAndRelativeHome`
  on both retry attempts (a hard fail, not a flake).
* `11e1560` — run `34053036163`, green on the first attempt, no retry.

If the commits become unreachable after #23 merges and its branch is deleted,
the run IDs remain the durable record.

## Checklist B — `make ci-windows-smoke` (Windows host)

| ID | Predicate |
| --- | --- |
| B0 | Same as A0 for smoke target |
| B12 | Build `dist/mcremote-windows-amd64.exe` + `dist/mcrelay-windows-amd64.exe` ship ldflags / CGO=0 |
| B13 | `go version -m` each contains `CGO_ENABLED=0` |
| B14 | `version` each exits 0; identity matches build VERSION |

## Checklist C — harden `acceptance-windows.ps1` (Windows only; already PS1)

### C1 `paths --json` …

1. No `-1` path segment (F4)
2. `product` = `mcremote`
3. `config_dir` under `$env:APPDATA\mcremote`; not under LocalAppData
4–6. data/state/cache under LocalAppData\mcremote (+ State/Cache)
7. **runtime_dir:** `$rtBase = Join($env:LOCALAPPDATA,'mcremote','Runtime')`; `instance_key` `^[0-9a-f]{16}$`; `runtime_dir` = `Join($rtBase, instance_key)` (ci, normalized); still C1#1
8. `log_dir` if set = `...\Logs`
9. `admin_socket` = `Join(runtime_dir,'admin.sock')`
10. Absolutes for path fields
11. `engine_registry_dir` non-empty; `instance_key` valid 16-hex

### C2 pair (F5) — temp data-dir create/list/cleanup
### C3 doctor — exit 0 only; no healthy-service assert

## Drift checklist vs `ci.yml`

go-native windows: CGO=0; MC_REQUIRE_SYMLINK; build/vet/test; omit retry/`-race`.
smoke-native: names + version identity; omit GH download.
**Plus:** host-guard behaviour documented; non-Windows skip+0 verified once on macOS and Linux clones.

## Docs

* `ops-windows-install.md` + `AGENTS.md`: **Windows-host-only** — before push on
  Windows dev host → `make ci-windows`; before tag → also `ci-windows-smoke`; functional →
  `acceptance-windows.ps1`. On macOS/Linux → `make preflight` (ci-windows* skips).
* Script/Make headers cite MADR/PLAN 0145, F20, and host-only rule.

## Phases

0. Mac proceed/execute — no code until then
1. Host guards + script + `make ci-windows` (A); verify skip+0 on unix clone
2. `make ci-windows-smoke` (B); skip+0 on unix
3. Harden acceptance-windows (C)
4. Docs + drift header
5. Confirmation closeout; no `.github/workflows` in impl PR

## Testbot

1. ~~C1#7~~ locked
2. ~~Doctor~~ exit 0 only
3. ~~A/B/C~~ approved
4. ~~A0/B0 skip semantics~~ — **ACK** 2026-09-06 (Testbot LGTM); clear for Mac proceed/execute on amended PLAN

## Execution Record

### Deviation — 2026-09-06, the host guard did not guard

*Evidence.* `make ci-windows` on a macOS host printed the skip line and then ran
PowerShell anyway:

```text
Windows-only; skipping on darwin (use make preflight on unix)
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
make: powershell: No such file or directory
make: *** [ci-windows] Error 1      exit=2
```

Both `ci-windows` and `ci-windows-smoke` behaved this way. It is A0 inverted:
the checklist requires "one clear skip line + exit 0 — **not** a checklist
fail", and Scope requires that macOS/Linux "must not fail when invoking
`ci-windows*` by habit". As written, two of the three OS clones got a hard
failure from a target whose whole purpose is to be inert on them. Phase 1's own
step — "verify skip+0 on unix clone" — is the one that would have caught it, and
is the reason this is recorded as a deviation rather than a discovery.

*Cause.* Make runs each recipe line in its own shell. The guard's `exit 0` ended
only the guard's shell; Make then ran the next line regardless. `make -n` shows
the two shells plainly. `HOST_GOOS` was never at fault — it resolves correctly
from `Makefile:59`.

*Resolution taken — one shell per recipe, guard defined once.* `$(WIN_ONLY)`
holds the guard and is prefixed to the real invocation on a single line with
`;`, so `exit 0` ends the recipe. Both candidates were prototyped and measured
before choosing, and were behaviourally identical — skip rc=0 off-Windows, run
on Windows, non-zero propagated when the script fails — so the choice was made
on maintainability, not correctness.

*Resolution rejected — parse-time `ifeq ($(HOST_GOOS),windows)` gating.* Also
correct, and it reads well while each branch is a single line. Rejected because
it defines every target twice, once per branch: a third Windows-only target
would have to be added in both places or silently lose its guard. That is the
enumeration drift `AGENTS.md` records as F2, which this repository has been
caught by twice in the last week. The chosen form adds a target as one line
reusing `$(WIN_ONLY)`, with nothing to keep in sync. Worth revisiting if the two
hosts ever need genuinely different prerequisites rather than one invocation
each.

*Verification.* On this darwin host both targets now print exactly one skip line
and exit 0, and no `powershell` is attempted. `make -n ci-windows
HOST_GOOS=windows` still ends in the PowerShell invocation, so the Windows path
is unchanged. `make -n vet`, `-n test` and `-n build` are unaffected by the
include.

*Unverified, and named rather than left implicit.* The Windows half has not been
run on a Windows host. A0's skip path is proven; A1–A9 still need the real host.

*Adjacent risk found while diagnosing, not fixed here.* The guard keys entirely
on `HOST_GOOS`, which `Makefile:59` derives from `uname -s`, setting `windows`
only on a `MINGW` match. Invoked where `uname` is absent — `mingw32-make` from
`cmd.exe`, and this repository's tooling does use `mingw32-make` — `UNAME_S`
becomes `unknown`, and the guard would skip **on Windows itself**, reporting
"skipping on unknown" on the one host that must run. That is a pre-existing
property of `Makefile`, outside this plan's scope, and is recorded so a later
phase can decide whether A0 needs a positive assertion that the host was
identified, rather than merely found not-Windows.
