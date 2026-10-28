---
status: in-progress
date: 2026-10-27
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

#### P1 execution (2026-09-29)

Approved by the owner, "proceed 0174". `go get golang.org/x/crypto@v0.57.0` reported:
- `golang.org/x/crypto v0.55.0 => v0.57.0`
- `golang.org/x/mod v0.40.0 => v0.41.0`
- `golang.org/x/sync v0.22.0 => v0.23.0`
- `golang.org/x/sys v0.47.0 => v0.48.0`
- `golang.org/x/term v0.45.0 => v0.46.0`
- `golang.org/x/text v0.41.0 => v0.42.0`

That is exactly the scratch trial's set. `go mod tidy` added nothing: `go.mod` 6 lines, `go.sum`
12 lines changed.

| Gate | Result |
| --- | --- |
| `govulncheck -show verbose ./...` | exit 0. Symbol Results and Package Results empty; Module Results: `Vulnerability #1: GO-2026-5932` only (A1) |
| `go mod tidy` again, compared with the committed files | unchanged (CI's "Go mod tidy is clean") |
| `make pre-add-check` | exit 0, "816 file(s) clean (gofmt, golint, govulncheck)" |
| `make race` | exit 0, 42 ok |
| `make ci-windows` (Git Bash on the Windows host, scratch clone of `HEAD` plus this change, go1.27.1) | exit 0, 43 ok, "ci-windows-local: ALL SELECTED CHECKS PASSED" |

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

#### P2 execution (2026-09-29)

**Files.**
- `scripts/vulncheck.sh` (new).
- `scripts/vulncheck-allow.txt` (new): one entry, GO-2026-5932, tab-separated.
- `scripts/vulncheck_test.sh` (new): 7 cases.
- `scripts/go-precheck.sh`: its inline govulncheck block, including the `need
  govulncheck` hint, now calls `scripts/vulncheck.sh`.
- `Makefile`: `vulncheck` already existed as a bare `govulncheck ./...` that nothing
  called, with the same blind spot. It now runs `scripts/vulncheck.sh`.

**The gate.**
- It reads the three `=== … Results ===` sections of `govulncheck -show verbose`. If any
  section is missing it exits 2 ("refusing to pass blind") rather than passing.
- govulncheck's exit 3 (called findings) is parsed as findings. Other non-zero exits with
  network text are an unreachable database, which stays a warning, as before.
- It warns, without failing, when an allowlisted ID is no longer reported.

**Seen to fail (A3).**

| Case | How | Result |
| --- | --- | --- |
| x/crypto v0.55.0 | scratch clone at `HEAD~1` with the new scripts and Makefile: `make pre-add-check` | exit 1 (make 2): "GO-2026-6355 affects a required module and is not in scripts/vulncheck-allow.txt", and the same for GO-2026-6354. The old precheck passed this tree in 0169 P3 |
| allowlist entry missing | the tree's real output, `GO_VULNCHECK_ALLOW` pointing at a temp copy without GO-2026-5932 | exit 1: "GO-2026-5932 affects a required module and is not in …" |
| allowlisted ID imported | `vulncheck_test.sh` fixture | exit 1: "GO-2026-5932 is imported by this module. It is allowlisted only as a required module that is not in the build, so that no longer holds." |
| allowlisted ID called | `vulncheck_test.sh` fixture | exit 1, the same message with "called" |
| the test itself | a scratch clone whose gate was edited to accept allowlisted IDs at every level | `vulncheck_test.sh`: 5 passed, 2 failed (both "want exit 1 got 0"), exit 1 |

**Gates.**
- `make vulncheck`: exit 0, "clear; allowlisted required-module findings: GO-2026-5932"
  (A2).
- `make pre-add-check`: exit 0, 816 files clean.
- `bash scripts/vulncheck_test.sh`: 7 passed, 0 failed.
- `make race`: exit 0, 42 ok.
- On the Windows host, under Git Bash, in the P1 scratch clone: `scripts/vulncheck.sh`
  exit 0 with the same accepted ID, and `vulncheck_test.sh` 7 of 7.

### P3 — CI (D4)

1. In `ci.yml`'s `Go (test; build on tag)` job, after the tidy check: install govulncheck at
   the host standard's version (`go install golang.org/x/vuln/cmd/govulncheck@v1.7.0`),
   then `make vulncheck`.
2. Push on ask, and check the new step's log in the CI run: it passes and lists the one
   allowlisted finding.

#### P3 execution (2026-09-29)

**Step 1 done.** `ci.yml`, `Go (test; build on tag)` job: one step after "Go mod tidy is
clean", named "Vulnerability check (govulncheck, every level)". It runs `go install
golang.org/x/vuln/cmd/govulncheck@v1.7.0`, puts `$(go env GOPATH)/bin` on PATH, and runs
`make vulncheck`.

actionlint 1.7.12 first flagged SC2155 in the new step (`export PATH="$(…)"`). That was
fixed by assigning first. The file then reports the same two shellcheck notes as before
this change: SC2086 and SC2012, both in steps this plan does not touch.

**Step 2 done.** Pushed on the owner's ask (`6ea9f3f1..662650ca`). CI run 36655321841 on
`662650ca` concluded `success`. The Go job's "Vulnerability check (govulncheck, every level)"
step logged `vulncheck: clear; allowlisted required-module findings: GO-2026-5932` (A4).
Every other job passed; the tag-only jobs were skipped.

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

#### P4 execution (2026-09-29)

**Files.**
- `scripts/check-pub-advisories.py` (new, stdlib only).
- `Makefile`: a "pub advisories (OSV)" step in `preflight`, after the Flutter pin. Exit 2
  (OSV unreachable, or the lock unreadable) prints "(pub advisories not checked: see
  above)" and continues. Exit 1 fails preflight.
- `.gitignore`: `__pycache__/` and `*.py[cod]`.

**Coverage.** The script checks only packages whose `source` is `hosted`: 158 of the lock's
163. The other five are SDK packages, which have no pub.dev version for OSV to match.

**Seen to fail (A5).**

| Input | Result |
| --- | --- |
| temp copy of `pubspec.lock` with `http` 1.6.0 → 0.13.2 | exit 1, "pub-advisories: http 0.13.2: GHSA-4rgh-jx4f-qfcq" (CVE-2020-35669) |
| the preflight step's shell logic on that copy | exit 1 |
| a lock path that does not exist | exit 2; the step prints its warning and exits 0 |
| the real lock | exit 0, "158 hosted packages, no advisories." |

**Gates.** `make preflight`: the first run failed at staticcheck, before the new step.
The cached staticcheck had been built by go1.26.6 and could not load go1.27 code. That is
PLAN 0169 Deviation 13, fixed there in `6d3e1c8f`, which keys the cache on the Go version.
The rerun passed every step, exit 0 ("✅ preflight passed"):
- the new step printed "pub-advisories: 158 hosted packages, no advisories.";
- `flutter analyze` "No issues found!";
- `flutter test` "+1418 ~3: All tests passed!".

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

### Acceptance, 2026-09-29

| # | Met | Evidence |
| --- | --- | --- |
| A1 | yes | P1: Module Results list only GO-2026-5932 |
| A2 | yes | P2: `make pre-add-check` and `make vulncheck` pass |
| A3 | yes | P2: fails on x/crypto v0.55.0, on a missing allowlist entry, and on the ID imported or called; the test fails against a broken gate |
| A4 | yes | P3: CI run 36655321841, step log above |
| A5 | yes | P4: preflight runs the step; exit 1 on `http` 0.13.2 |
| A6 | yes | P1: `make race` 42 ok, `make ci-windows` 43 ok |

## Amendment — 2026-10-13: P5, `golang.org/x/net`

**Found by:**
[0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
A1's pre-add gate.

- `govulncheck` reports five advisories in the required module `golang.org/x/net v0.58.0`:
  GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617.
- All are fixed in `v0.60.0`, and none is imported or called.
- The gate is right to fail. The advisories are new since P1, and `HEAD` already carries
  `v0.58.0`, so `make pre-add-check` and CI's `make vulncheck` fail on `master` as it stands.

**Owner's decision (2026-10-13):** bump `x/net` under this record first, in its own change, then
stage the ACP migration on top.

- This is D1 applied as written: a required module with a known advisory that a newer release
  fixes moves to the newest release. The MADR is unchanged.
- The scope rule "only what `go mod tidy` moves with x/crypto" is widened, for this phase only, to
  what `go mod tidy` moves with `x/net`.

### P5 — x/net v0.61.0, the newest release (D1)

1. On a scratch clone of `HEAD`: `go get golang.org/x/net@v0.61.0`, then `go mod tidy`. Record the
   exact `go.mod` diff, and check that a second `tidy` changes nothing.
2. Run `./scripts/vulncheck.sh`. Only GO-2026-5932 may remain, at the required-module level.
3. Run `go build ./...` and `go test -count=1 -race ./internal/...` on that clone.
4. Stage that clone's `go.mod` and `go.sum` in this repository, with this PLAN.
   - The working tree holds the ACP migration as well, so the index is written from the clone's
     files and the working tree is left alone.
   - The same bump is applied in the working tree, so the two agree after the owner commits.
5. The owner commits, then 0184's A1 is staged on top.
6. `make ci-windows` on the Windows host, as P1 did. It cannot run from this macOS host, so it is
   listed as not done until it has run there.

#### P5 execution (2026-10-13)

Approved by the owner: "Bump x/net under 0174 first".

The scratch clone of `HEAD` was at `68e348a1`. `go get golang.org/x/net@v0.61.0` reported:

- `golang.org/x/crypto v0.57.0 => v0.58.0`
- `golang.org/x/net v0.58.0 => v0.61.0`
- `golang.org/x/sync v0.23.0 => v0.24.0`
- `golang.org/x/sys v0.48.0 => v0.49.0`
- `golang.org/x/term v0.46.0 => v0.47.0`
- `golang.org/x/text v0.42.0 => v0.43.0`
- `golang.org/x/tools v0.49.0 => v0.51.0`

`go mod tidy` added nothing: `go.mod` changed 7 lines and `go.sum` 14. A second `tidy` changed
nothing.

| Gate (scratch clone) | Result |
| --- | --- |
| `./scripts/vulncheck.sh` | exit 0, "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `go build ./...` | exit 0 |
| `go test -count=1 -race ./internal/...` | exit 0, 43 ok |

- **The working tree** got the same bump, on top of 0184 A1. Its `go.mod` and `go.sum` differ from
  the clone's only by A1's ACP lines: 8 in `go.mod`, 4 in `go.sum`.
- **What was staged,** for this phase's own commit:
  - the clone's `go.mod` and `go.sum`, written into the index with `git hash-object -w` and
    `git update-index --cacheinfo`, so the ACP lines in the working tree are not staged;
  - this PLAN.
- ~~**Not done:** `make ci-windows` on the Windows host. This PLAN stays `in-progress` until it has
  run.~~ Run on 2026-10-13 against `db717ed3`, which carries P5. Exit 0, "ci-windows-local: ALL
  SELECTED CHECKS PASSED", with Go 1.27.2 windows/amd64: toolchain, symlink probe, `go build`,
  `go vet`, and `go test ./...` with every package ok. The details are in 0184's PLAN, "Windows
  verification". P5 is complete.

**Deviation — 2026-10-13, the commit: P5 shares a commit with 0184 A1.**

- The owner committed P5 and 0184's A1 together, as `db717ed3` ("feat(acpagent): migrate to
  acp-go-sdk2"): 38 files, with the `x/net` bump inside the ACP migration's `go.mod` and `go.sum`.
- The plan was two commits, P5 first, so that a regression could be bisected to one of them.
- **Effect:** reverting `db717ed3` restores `x/net v0.58.0` and its five advisories together with
  the Coder fork.
- **The content is unchanged.** Both phases were verified separately before commit, P5 on a clean
  clone of `68e348a1`, and the commit holds exactly what was verified.
- **History is the owner's to keep or split before push.** At this entry the commit was unpushed
  (`master` 4 ahead of `origin`).

## Amendment — 2026-10-27: P6, `golang.org/x/net` again

**Found by:** the pre-add gate of
[0077-PLAN-signed-receipts-permission-handoffs.md](0077-PLAN-signed-receipts-permission-handoffs.md)
§13 (P10) and
[0212-PLAN-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-PLAN-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
P8, both test-only.

- `make pre-add-check` failed: "vulncheck: GO-2026-6866 affects a required module and is not in
  scripts/vulncheck-allow.txt", and the same for GO-2026-6860.
- Both are in `golang.org/x/net v0.61.0`'s `html` package and fixed in `v0.62.0`, published
  2026-10-27 19:00Z. govulncheck: "Your code is affected by 0 vulnerabilities". Neither is imported
  or called.
- The gate is right to fail, as in P5: the advisories are new, and `HEAD` carries `v0.61.0`, so
  CI's `make vulncheck` fails on `master` too. `x/net` is required through `internal/certs` and
  certmagic (`go mod why -m golang.org/x/net`).
- The allowlist is not the answer: its one entry has no fixed version, and these have one.

**Owner's decision (2026-10-27):** "Bump x/net under 0174 (Recommended)": this phase's own commit,
staged first; 0077's P10 and 0212's P8 are staged on top.

- D1 applied as written, as in P5. The MADR is unchanged.
- The scope rule is widened, for this phase only, to what `go mod tidy` moves with `x/net`.

### P6 — x/net v0.62.0, the newest release (D1)

1. On a scratch clone of `HEAD`: `go get golang.org/x/net@v0.62.0`, then `go mod tidy`. Record the
   modules that move, and check that a second `tidy` changes nothing.
2. Run `./scripts/vulncheck.sh`. Only GO-2026-5932 may remain, at the required-module level.
3. Run `go build ./...`, `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64, and
   `go test -count=1 -race ./...` on that clone.
4. Copy the clone's `go.mod` and `go.sum` into the working tree, whose own are `HEAD`'s, and check
   they are byte-identical to the clone's. Stage them with this PLAN, alone.
5. `make ci-windows` on the Windows host, on the owner's word. Until it has run, this PLAN stays
   `in-progress`.

#### P6 execution (2026-10-27)

Approved by the owner: "Bump x/net under 0174 (Recommended)".

The scratch clone of `HEAD` was at `5d1e7e2c`. `go get golang.org/x/net@v0.62.0` reported one
module: "go: upgraded golang.org/x/net v0.61.0 => v0.62.0". Nothing else moved. `go.mod` changed 1
line and `go.sum` 2; a second `tidy` changed nothing.

| Gate (scratch clone) | Result |
| --- | --- |
| `./scripts/vulncheck.sh` | exit 0, "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `go build ./...` | exit 0 |
| `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `go test -count=1 -race ./...` | exit 0, 44 ok |

**Step 4.** The working tree's `go.mod` and `go.sum` were `HEAD`'s (`git diff --quiet HEAD`, exit
0). The clone's were copied in, and `cmp` found both byte-identical to the clone's.

**The working tree's gates,** with 0077's P10 and 0212's P8 in it too:

| Gate (working tree: 0174 P6, 0077 P10 and 0212 P8) | Result |
| --- | --- |
| `make pre-add-check FILES=…` (the two test files) | exit 0, "go-precheck: 2 file(s) clean (gofmt, golint, govulncheck)" |
| `make staticcheck` | exit 0 |
| `go build ./...` | exit 0 |
| `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `go mod tidy -diff` | exit 0, no diff |
| `make test` | exit 0, 44 ok |
| `make race` | exit 0, 44 ok |
| `go test -race -count=1 ./...` on the Linux test host, in a scratch copy removed afterwards | exit 0, 44 ok |
| `make check-records` | exit 0 |
| `make markdownlint-docs` | exit 0, "0 issues" |
| the guard's redact preview, from the repository root | "would edit 0 file(s)" |

- **Staged** for this phase's own commit: `go.mod`, `go.sum` and this PLAN.
- **Not done:** step 5, `make ci-windows` on the Windows host, which waits for the owner's word.
  This PLAN stays `in-progress` until it has run.

## Rollout and Rollback

Each phase is one commit. Rollback is reverting it. P1's revert restores x/crypto v0.55.0
and its two advisories. P2's revert restores the old, blind gate. P3 and P4 are additive.
`git push` needs an explicit ask.
