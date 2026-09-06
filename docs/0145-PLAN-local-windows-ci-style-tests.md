---
status: proposed
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

**Done:** Windows host runs locked checklists; non-Windows skip+0; `#23` tip
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
| A10 | Confirmation: `#23` tip → FAIL on Windows |
| A11 | After 0144 Windows fix → PASS on the Windows dev host |

Non-asserts: paths/pair/doctor/Ctrl+C/Flutter/GH download.
A10/A11 confirmation is Windows-only (N/A off-Windows — do not attempt).

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
