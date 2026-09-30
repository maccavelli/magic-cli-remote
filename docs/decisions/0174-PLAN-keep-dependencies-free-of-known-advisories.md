---
status: proposed
date: 2026-09-29
associated-madr: "0174-MADR-keep-dependencies-free-of-known-advisories.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0174 — Keep dependencies free of known advisories, with one gated exception

Associated MADR: [0174-MADR-keep-dependencies-free-of-known-advisories.md](0174-MADR-keep-dependencies-free-of-known-advisories.md)
(D1–D5).

## Goal

- `govulncheck -show verbose ./...` reports exactly one finding: GO-2026-5932, at the
  required-module level.
- `make pre-add-check` and CI fail on any other govulncheck finding, at any level. They
  also fail on GO-2026-5932 if it ever reaches the imported or called level.
- `make preflight` fails on any pub package with an OSV advisory.

## Scope

### In scope (the only files any phase may touch)

- P1: `go.mod`, `go.sum`.
- P2: `scripts/go-precheck.sh`; `scripts/vulncheck.sh` (new: the govulncheck gate, split
  out so CI can run it alone); `scripts/vulncheck-allow.txt` (new: the allowlist);
  `scripts/vulncheck_test.sh` (new); `Makefile` (a `vulncheck` target).
- P3: `.github/workflows/ci.yml`, one step added to the `Go (test; build on tag)` job.
- P4: `scripts/check-pub-advisories.py` (new); `.gitignore` (Python ignore rules: the
  repository has three Python scripts under `scripts/` but no Python ignore rules, and
  this adds a fourth); `Makefile` (the `preflight`
  target).
- This pair.

### Out of scope

- Any other dependency version. Only what `go mod tidy` moves with x/crypto.
- certmagic, Let's Encrypt mode, and the `openpgp` package itself (MADR option C).
- Dependency advisories in magic-git or any other repository.

## Implementation Steps

### P1 — x/crypto v0.57.0 (D2)

1. `go get golang.org/x/crypto@v0.57.0`, then `go mod tidy`. Record the exact `go.mod`
   diff. The scratch trial moved x/sys, x/mod, x/sync, x/term and x/text by one minor
   each.
2. `govulncheck -show verbose ./...`: only GO-2026-5932, only under the module results.
3. Gates: `make pre-add-check`, `make race`, `make ci-windows` (Git Bash on the Windows
   host), and CI's "Go mod tidy is clean" check run locally (`go mod tidy && git diff
   --exit-code go.mod go.sum`).
4. Commit.

### P2 — The strict gate (D3, D4)

1. `scripts/vulncheck.sh` runs `govulncheck -show verbose ./...`. It reads the three result
   sections ("Symbol Results" = called, "Package Results" = imported, "Module Results" =
   required), and fails:
   - on any ID in the called or imported sections, allowlisted or not;
   - on any ID in the module section that is not in `scripts/vulncheck-allow.txt`.

   It passes otherwise, naming each allowlisted ID it accepted. An unreachable
   vulnerability database stays a warning, as `go-precheck.sh` does today.
   `GO_VULNCHECK_INPUT=<file>` feeds saved govulncheck output instead of running it. That
   exists for the test only.
2. `scripts/vulncheck-allow.txt`: one tab-separated line per exception: ID, module, review
   date, reason. The first and only entry is GO-2026-5932, `golang.org/x/crypto`,
   2026-09-29, "openpgp is not in the build (go mod why); no fixed version exists;
   x/crypto is required by x/net, certmagic and acmez (MADR 0174 D3)".
3. `scripts/go-precheck.sh` calls `scripts/vulncheck.sh` in place of its inline govulncheck
   block. `GO_PRECHECK_SKIP_VULN=1` still skips it. A `make vulncheck` target runs it
   alone.
4. **Seen to fail.** Every case runs on a scratch clone or a temp file, never the tree:
   - a scratch clone at x/crypto v0.55.0: fails, naming GO-2026-6354 and GO-2026-6355;
   - the real output with GO-2026-5932 removed from a temp copy of the allowlist: fails,
     naming GO-2026-5932;
   - saved output edited so GO-2026-5932 appears under "Package Results": fails, although
     the ID is allowlisted;
   - the same for "Symbol Results".

   `scripts/vulncheck_test.sh` pins the last three with `GO_VULNCHECK_INPUT` fixtures and
   runs the real output to a pass.
5. Gates: `make pre-add-check`, `bash scripts/vulncheck_test.sh`, `make race`. Commit.

### P3 — CI (D4)

1. In `ci.yml`'s `Go (test; build on tag)` job, after the tidy check: install govulncheck at
   the host standard's version (`go install golang.org/x/vuln/cmd/govulncheck@v1.7.0`),
   then `make vulncheck`.
2. Push on ask, and check the new step's log in the CI run: it passes and lists the one
   allowlisted finding.

### P4 — Pub advisories (D5)

1. `scripts/check-pub-advisories.py` uses the stdlib only. It reads
   `apps/mobile/pubspec.lock` (`--lock` overrides) and sends one OSV `querybatch`
   (ecosystem Pub). It exits 1 listing each package, version and advisory ID; exits 0
   when there are none; and exits 2 with a warning when OSV is unreachable.
2. `make preflight` runs it beside the Flutter steps. `.gitignore` gains `__pycache__/`
   and `*.py[cod]`.
3. **Seen to fail:** a temp copy of `pubspec.lock` with one package set to a version OSV
   lists as affected must exit 1 and name it. The real lock exits 0.
4. Gates: `make preflight`. Commit.

## Verification

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `govulncheck -show verbose ./...` lists only GO-2026-5932, only under module results | D2, D3 |
| A2 | `make pre-add-check` and `make vulncheck` pass on the tree | D4 |
| A3 | The gate was seen failing on x/crypto v0.55.0, on a missing allowlist entry, and on an allowlisted ID at the imported and called levels | D3, D4 |
| A4 | CI's Go job runs `make vulncheck`, and it passes | D4 |
| A5 | `make preflight` runs the pub check; it was seen failing on an affected version | D5 |
| A6 | `make race` and `make ci-windows` pass after the bump | D2 |

The criterion most likely to be dropped quietly is **A3's "imported or called" case**.
Nothing in the real tree reaches it. Only the fixture shows that the exception cannot
cover `openpgp` being linked in.

## Rollout and Rollback

Each phase is one commit. Rollback is reverting it. P1's revert restores x/crypto v0.55.0
and its two advisories. P2's revert restores the old, blind gate. P3 and P4 are additive.
`git push` needs an explicit ask.
