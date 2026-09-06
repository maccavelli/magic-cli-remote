---
status: accepted
date: 2026-09-06
decision-makers: maccavelli
consulted: Cappy (CI/CD harness); Testbot (approve + locked pass/fail list 2026-09-06); scripts/acceptance-windows.ps1; CI go-native/smoke-native; MADR 0116 F20/D20/D17; MADR 0143
informed: Anyone developing on the Windows dev host or cutting a windows/amd64 release
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Local CI-style Windows tests catch go-native failures before GitHub

## Context and Problem Statement

Windows/amd64 is a first-class native lane in CI (`go-native` on
`windows-latest`, plus tag-only `smoke-native`), but the day-to-day local loop
on the Windows dev host does not mirror that lane closely enough to catch platform-sensitive
failures before a push.

Concrete recent evidence: PR #23 / MADR 0144 — `TestRedactAbsoluteAndRelativeHome`
green on ubuntu/linux-arm64, red on **Go (windows/amd64)** both retry attempts
(hard fail). Local Windows CI-style gate exists to catch that class before GitHub.

Mac develops this repo on **three OS clones** (Windows, macOS, Linux).
Windows-specific local gates must not break shared Make habits on unix hosts.

### What already exists

| Surface | Role |
| --- | --- |
| `scripts/acceptance-windows.ps1` | Owner-laptop **functional** acceptance (paths/pair/doctor + build/test). Not a GHA self-hosted runner (MADR 0116 F20). |
| `make preflight` | Ubuntu go + flutter — not Windows go-native. Unix-host default. |
| CI `go-native` windows/amd64 | CGO=0, `MC_REQUIRE_SYMLINK=1`, build/vet/`go test ./...`, retry once (0143). |
| CI `smoke-native` windows | Tag-only; download GH artifacts; `version` == tag VER. |

## Decision Drivers

* Catch Windows-only **unit** failures on the Windows dev host before GitHub (0144-class).
* Prefer scripts + Make; **no `.github/workflows` changes** without Mac.
* Never register the Windows dev host as a self-hosted runner (F20).
* Ship contract: `CGO_ENABLED=0`, no `-race` (D20 / C7).
* **Hard local signal:** no 0143 retry in the default target.
* Go toolchain from `go.mod` (not the Windows-preinstalled default).
* Keep functional F5/paths/doctor on `acceptance-windows.ps1` (Testbot).
* Testbot owns pass/fail predicates; Cappy owns harness / Make / drift.
* **Host-gated:** Windows-specific targets only run on a Windows host; macOS/Linux
  keep `make preflight` and must not fail when invoking `ci-windows*` by habit.

## Considered Options

1. Extend acceptance-windows into the CI mirror (blurs functional vs unit).
2. **New sibling** `scripts/ci-windows-local.ps1` + Make — unit/CI mirror only;
   acceptance-windows stays functional.
3. Make-only one-liner — too weak for smoke naming / env contract.
4. Workflow / self-hosted — out of scope without Mac.

## Decision Outcome

**Accepted (Mac 2026-09-06):** Option **2**, per Testbot approve-with-tightenings.

### Amendment (Mac 2026-09-06) — Windows-host-only gates

Mac works three OS clones. `make ci-windows`, `make ci-windows-smoke`, the
PowerShell script, and any related hooks/docs that imply “always run” must
**only fire when the host is Windows**.

* **Detect Windows** via Makefile `HOST_GOOS` (already derived from `uname` /
  MINGW|MSYS|CYGWIN), and/or PowerShell `$IsWindows` / `$env:OS`, and/or
  `go env GOOS` when available.
* **On macOS/Linux:** Make/PS1 detect host at start (`HOST_GOOS` / `$IsWindows` /
  `RuntimeInformation`); print one clear line
  (“Windows-only; skipping on <os>”) and **exit 0**. Skip is **not** a checklist
  fail and **not** a silent PASS (message required). Do **not** invoke PowerShell
  Windows gates or run `go test` under a fake Windows env off-Windows.
  A10/A11 confirmation remains Windows-only (N/A elsewhere).
* **Docs:** state explicitly these targets are Windows-host-only; unix hosts
  keep using `make preflight`.
* **No** cross-OS auto-hook that would invoke `ci-windows` on non-Windows.

### Default `make ci-windows` (day-1, Windows host only)

* Go version from **`go.mod`** (never rely on Windows-preinstalled default).
* `CGO_ENABLED=0`
* `MC_REQUIRE_SYMLINK=1` — missing `SeCreateSymbolicLinkPrivilege` ⇒ **fail**
  (not skip).
* `go build ./...` / `go vet ./...` / `go test ./...` **full module** — **no
  package allowlist**
* **No `-race`**, **no retry** in the default Make target
* Optional `-RetryOnce` only for deliberate CI flake-chase; never default

### Day-1 **exclude** from default `ci-windows`

* `live_*` / token suites; `-race`; Flutter / `make preflight`
* Functional F5 / `paths` / `doctor` — stay on `acceptance-windows.ps1`
* GitHub artifact download (tag `smoke-native` stays remote)
* Package-subset / fast-path as default — **forbidden**; full `./...` only

### Separate day-1 target: `make ci-windows-smoke` (Windows host only)

Light local smoke (not GH download): build release-shaped
`dist\\mcremote-windows-amd64.exe` / `mcrelay-windows-amd64.exe`, assert
`go version -m` ⇒ `CGO_ENABLED=0`, run `version` vs expected VERSION stamp.

### Mirrored vs intentionally omitted CI behaviours

| CI behaviour | Local default |
| --- | --- |
| go-native build/vet/test CGO=0 + MC_REQUIRE_SYMLINK | **mirrored** (Windows host) |
| go-native retry-once (0143) | **omitted** (optional `-RetryOnce` only) |
| `-race` / flutter preflight | **omitted** |
| smoke-native GH artifact download | **omitted**; separate light smoke target |
| acceptance paths/pair/doctor | **omitted** from ci-windows |
| Run on macOS/Linux | **skip + exit 0** |

### MADR acceptance criteria

* Exit ≠0 on any failed check; green ⇒ all selected checks ran
* **Confirmation:** `fb7305e` red-reproduce on Windows; `11e1560` green after the 0144 fix
* F20 documented; Go from `go.mod`; mirrored-vs-omitted table above
* No `ci.yml` change unless Mac expands scope; no 0144 fix in this MADR
* Non-Windows `make ci-windows` / `ci-windows-smoke` print skip and exit 0

## Locked pass/fail criteria (Testbot — fold into PLAN checklist)

### A. Default `ci-windows` — exit 0 only if all pass (Windows host)

1. `CGO_ENABLED=0` for all go invocations
2. `MC_REQUIRE_SYMLINK=1`; cannot create symlinks ⇒ **FAIL** (not skip)
3. Go toolchain satisfies `go.mod`
4. `go build ./...` exit 0
5. `go vet ./...` exit 0
6. `go test ./...` exit 0 — full module, no allowlist
7. No `-race`
8. No retry by default
9. No `-tags live_*`
10. Confirmation: tree at `fb7305e` (PR #23 before the 0144 fix) / broken
    `TestRedactAbsoluteAndRelativeHome` ⇒ **must fail** on Windows
11. At `11e1560` (the 0144 fix) ⇒ **must pass** on the Windows dev host

Non-asserts in default: `paths`/`pair`/`doctor`/Ctrl+C; Flutter; GH artifact download.

### B. `ci-windows-smoke` — when invoked on Windows, all must pass

12. Build `dist/mcremote-windows-amd64.exe` + `dist/mcrelay-windows-amd64.exe`
    with release ldflags / `CGO_ENABLED=0`
13. `go version -m` on each contains `CGO_ENABLED=0` else FAIL
14. `version` on each exits 0 and prints the VERSION used for the build

### C. Beyond stock `go test ./...`?

Default: no extra package-level asserts beyond env + build + vet + full test.
Smoke: yes — B12–14. No day-1 fast path.

## Consequences

* Positive: #23-class unit fails locally; clear unit vs functional split;
  shared Make habits safe on macOS/Linux.
* Negative: still run acceptance-windows for F5 on Windows; PLAN documents both.

## Validation

* Predicates A10–A11 and B12–B14 on the Windows dev host; no workflow files in impl PR.
* On macOS and Linux: `make ci-windows` and `make ci-windows-smoke` print a
  clear skip and exit 0.

## Follow-ups

* PLAN 0145: [0145-PLAN-local-windows-ci-style-tests.md](0145-PLAN-local-windows-ci-style-tests.md)
* Ops pointer for Windows dev host invoke (PLAN phase)
* Implement only after Mac proceed/execute on the PLAN
