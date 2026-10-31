---
status: in-progress
date: 2026-10-31
associated-madr: "0225-MADR-fix-the-two-codex-tests-that-raced-in-ci.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement fixing the two codex tests that raced in CI

Associated MADR: [0225-MADR-fix-the-two-codex-tests-that-raced-in-ci.md](0225-MADR-fix-the-two-codex-tests-that-raced-in-ci.md)

## Goal

Two codex tests stop depending on goroutine scheduling, with no production change:

* the non-blocking probe test asserts that the call returns while a held-open probe runs, and that
  the probe then finishes (MADR D1);
* every read of a live engine's state in `collaboration_test.go` takes `p.mu` (D2);
* `live_contract_test.go` reads the engine through the same helper and fails clearly on a nil
  engine (D4 as amended).

Each is seen failing first, and passes under the perturbations that broke the old tests (D5).

## Scope

**Tests:**

* `internal/provider/codex/nonblocking_reality_test.go`:
  * `TestNonBlockingProbeRunsNoBinaryOnAColdCache` and its comment (`:12-58`), replaced by
    `TestNonBlockingProbeReturnsWhileTheProbeRuns` (D1);
  * the import of `strconv`, which the stub's bound needs.
* `internal/provider/codex/collaboration_test.go` (D2):
  * the helper `liveEngine`, added at the end of the file;
  * `TestEngineGenerationResetsCollaborationProbe`, `:339-340` and `:357-358`;
  * `:161`, `:183` and `:275-276`.
* `internal/provider/codex/live_contract_test.go` (D4 as amended, deviation 1): the locked read at
  `:85-89` becomes `liveEngine`, with a `t.Fatal` on a nil engine. It builds only under the
  `live_codex_contract` tag.
* `internal/provider/codex/reality_lock_test.go` (deviation 2): `waitForFile` takes the reason a
  missing file means, and its two callers pass theirs.

**Records:** this pair, and `docs/README.md`'s two index rows for it.

**Not in scope:**

* `store_reality.go` and `provider.go`: no production change (D3).
* ~~`live_contract_test.go:86-88`, under the `live` tag (D4).~~ In scope since deviation 1.
* A live run of `live_contract_test.go` (C2).
* Other packages' tests.

## Constraints

* **C1 — One commit.** The agent stages; the owner commits with `git commit --no-edit`. Before
  anything is staged, `git diff --cached --name-only` must list nothing but this plan's files.
* **C2 — No live run.** No `make live-*` target runs.
* **C3 — Every check is seen failing first,** on a scratch copy made with `git archive`, never in the
  tree.
* **C4 — The test hosts run only on the owner's word** (Q1 below).
* **C5 — No push without an explicit ask in the same turn.** `leftovers/guard_preview.py` before
  staging; `leftovers/guard_range.py` before asking the owner to push.
* **C6 — `make pre-add-check` passes on every staged Go file.**

## Implementation Steps

### P0 — Before the first write (read only)

1. Record `HEAD` and `git status --short`. Work that is not this plan's is left alone.
2. The line references hold: `nonblocking_reality_test.go:12-58`; `collaboration_test.go:161`,
   `:183`, `:275-276`, `:339-340`, `:357-358`; `store_reality.go:316-327`; `provider.go:1009`;
   `reality_lock_test.go:19-32`; `live_contract_test.go:85-89`. A moved line is re-read, not
   assumed.

### S1 — The old tests, seen failing (D5; C3)

`leftovers/0225_repro.py` (E1, `-count=5`) and `leftovers/0225_repro.py e2 --separate` on copies
of `HEAD`. Want: E1, 5 of 5 fail at `nonblocking_reality_test.go:44`, "the binary ran before the
call returned"; E2, 5 of 5 fail with a data race whose write is `provider.go:1009` and whose read is
`collaboration_test.go:340`. Anything else is a stop. Logs: `leftovers/0225-repro-*.log`.

### S2 — The change (D1, D2)

1. **D1.** In `nonblocking_reality_test.go`, replace `:12-58` with
   `TestNonBlockingProbeReturnsWhileTheProbeRuns`, as `leftovers/0225_new_nonblocking_test.go.txt`
   holds it and P1 validated it:
   * the stub writes `started`, waits for `release` in 50 ms steps up to 30 s, writes `finished`,
     and exits 1;
   * a cleanup writes `release`;
   * the call runs on a goroutine and must return within 5 s, with `RealityUnknown`, and with
     `finished` absent;
   * then `waitForFile(started)`, write `release`, `waitForFile(finished)`.

   Add `"strconv"` to the imports. The comment keeps 0137 F1's reason and cites MADR 0225 F1, F7 and
   F8.
2. **D2.** In `collaboration_test.go`:
   * add `liveEngine(p *Provider) (*engine, int)`, which returns `p.eng` and `p.generation` under
     `p.mu`, with a comment citing MADR 0225 D2 and `reconnect_p3_test.go`'s inline form;
   * `:339-340`: `first, firstGen := liveEngine(p)`, then `first.cmd.Process.Kill()`;
   * `:357-358`: the generation compared is `liveEngine`'s;
   * `:161`, `:183`, `:275-276`: the engine read is `liveEngine`'s, its result held in a local.
3. **D4, as amended.** In `live_contract_test.go`, replace the locked block at `:85-89` with
   `eng, _ := liveEngine(p)`; `if eng == nil { t.Fatal("no engine after initialize: it exited
   before its state could be read") }`; then `snapshot`, `metadata` and `fr` read from `eng`. The
   values read and their later use do not change.
4. **Gofmt** the three files. `git diff --stat` lists only Scope's files.

### S3 — The checks, green, perturbed and planted (D5; C3)

1. **Green, in the tree:**
   * `go test -race -count=50 -run '^(TestNonBlockingProbeReturnsWhileTheProbeRuns|TestEngineGenerationResetsCollaborationProbe)$' ./internal/provider/codex/`:
     100 PASS, 0 races;
   * `go test -race -count=1 ./internal/provider/codex/`: ok.
2. **Perturbed, on scratch copies of the changed tree** (P1 and P2's perturbations):
   * E1's 100 ms pause after the call returns: the probe test passes, 5 of 5;
   * E2's 300 ms pause after `Kill`, five separate processes: the collaboration test passes, 0
     races.
3. **Plants, on scratch copies of the changed tree:**
   * the probe runs on the caller's goroutine (`go func()` made `func()` in `store_reality.go`): the
     probe test must fail, "the call had not returned after 5s";
   * `liveEngine` reads without `p.mu`, with E2's pause, five separate processes: the collaboration
     test must report a data race;
   * the stub never writes `finished`: the probe test must fail in `waitForFile`, naming `finished`.

   Each failure is quoted in the record. A plant that passes is a stop. Copies are removed.
4. **Absence:** no `p.eng` or `p.generation` remains in `collaboration_test.go` outside `liveEngine`,
   apart from writes made before any engine runs (none today).
5. **The live test, built not run (C2):** `go vet -tags live_codex_contract ./internal/provider/codex/`
   and `go test -c -tags live_codex_contract -o <scratch> ./internal/provider/codex/` for darwin,
   linux and windows: each exit 0. Seen failing first: with `liveEngine` renamed in a scratch copy,
   the tagged build must fail on the undefined name, which shows the tag compiles this file.
6. **The gates:** `make pre-add-check FILES="…"` on the three files; `leftovers/vet_goos.py`;
   `make staticcheck`; `make vulncheck`; `make race`; `leftovers/nonascii_scan.py --diff`.

### S4 — The test hosts (C4)

`PINS_PHASE=0225 leftovers/pins_hosts.py <linux> <windows>`. Want: `go test -race -count=1 ./...` on
the Linux test host and `make ci-windows` on the Windows test host, each exit 0. The probe test
skips on Windows (`SkipIfNoPOSIXModes`), as the old one did. Both scratch trees are removed, and
their absence checked.

### S5 — Records and staging

1. This plan's execution record: each step's output, the plants' failures quoted, any deviation,
   dated.
2. `python3 scripts/check_records.py --write-index`; `make check-records`; `make markdownlint-docs`;
   the pair through `markdownlint-cli2 --no-globs`; `leftovers/guard_preview.py`.
3. **Stage**, under C1: Scope's three test files, this pair, and `docs/README.md` as this pair's two
   rows (`leftovers/stage_index_rows.py 0225`). Then run `check_records.py --check-all` on the staged
   tree, exported with `git checkout-index`. The owner commits.

### S6 — After the commit

1. `leftovers/guard_range.py origin/master HEAD` before asking the owner to push.
2. After the push: every Go job and the Docs workflow pass. Record the run ids. The plan is then
   `complete`.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | The old tests failed under E1 and E2 (S1) | D5 |
| V2 | The probe test holds its stub open, bounds the call, and waits for `started` and `finished` | D1 |
| V3 | `collaboration_test.go` reads a live engine only through `liveEngine` | D2 |
| V3a | `live_contract_test.go` reads through `liveEngine`, fails on a nil engine, and builds and vets under its tag on three OSes | D4 |
| V4 | Both tests pass 50 of 50 under `-race`, and under E1's and E2's perturbations (S3) | D1, D2 |
| V5 | The three plants fail as S3 step 3 requires | D5 |
| V6 | No production file changed | D3 |
| V7 | S3's gates, S4's hosts and CI on the push pass | all |

## Rollout and Rollback

* **Rollout:** tests only; nothing ships.
* **Rollback:** revert the commit. The old tests return with their races.

## Owner decisions needed at approval

* **Q1 — The test hosts (C4):** S4 runs on the Linux and Windows test hosts, in scratch trees.
* **Q2 — Scope (MADR options A and B):** lock every live-engine read in `collaboration_test.go`
  (A, recommended), or only the failing lines (B)?

## Not done by this plan

* A live run of `live_contract_test.go` (C2).
* A search of other packages' tests for the same patterns.

## The owner's answers

*Answered by the owner, 2026-10-31:* Q2 "approve scope 1" (option A); Q1 "test hosts 2" (S4 runs);
"but also add the live contract test"; on deviation 1, "Use liveEngine, add nil check".

## Execution record

### Deviation 1 — 2026-10-31: the live contract test already locks

* **Found (before any write, on reading the file the owner added):** `live_contract_test.go:85-89`
  takes `p.mu` around its three reads of `p.eng`. MADR F5 said otherwise. The grep behind it dropped
  every line naming `p.mu`, and the file was not read. What remains is a nil `p.eng` panic if the
  engine exits first.
* **Decision (owner):** "Use liveEngine, add nil check". MADR F5 is corrected and D4 amended.
  Scope gains the file; S2 step 3 and S3 step 5 are added.

### Deviation 2 — 2026-10-31: `waitForFile` names what a missing file means

* **Found (S3 step 3, the `finished` plant):** `waitForFile` (`reality_lock_test.go:20-32`) fails
  with one fixed text, "%s never appeared: the probe did not start" (`:28`). That is true for its two
  original callers, which wait for a `running` marker (`:70`, `:152`). It is false for D1's wait on
  `finished` (`nonblocking_reality_test.go:80`), whose plant failed with "finished never appeared:
  the probe did not start". It is also less precise than it could be for D1's wait on `started`.
* **Decision (owner, "fix this now"):** `waitForFile(t, path, why)` takes the reason as a third
  argument and fails with "%s never appeared within 10 s: %s". Each caller states its own reason:
  * `reality_lock_test.go:70`, `:152`: "the probe did not start", as before;
  * `nonblocking_reality_test.go:76` (`started`): "the background refresh never started the probe";
  * `:80` (`finished`): "the released probe never finished".

  The wait and its bound do not change.
* **Scope:** `internal/provider/codex/reality_lock_test.go` is added. It is seen failing first: the
  `finished` plant again, and a plant in which a `running` stub never writes its marker. Each must
  fail with its caller's reason.
* **Done (after S5's first staging, before the owner's commit):**
  * The helper and its four callers changed as decided. `gofmt -l`: nothing. `go vet`, with and
    without `-tags live_codex_contract`: exit 0.
  * **Before:** the S3 plant that never writes `finished` failed with "finished never appeared: the
    probe did not start", which is the defect.
  * **Green** (`leftovers/0225_s3.py`): `TestNonBlockingProbeDoesNotWaitForARunningProbe` and
    `TestInvalidationDiscardsAProbeStartedBeforeIt`, `-race -count=20`: 40 PASS. The probe test,
    `-race -count=20`: 20 PASS. 0 data races.
  * **Plants**, each in a copy, each failing with its caller's reason:
    * no `finished`: `nonblocking_reality_test.go:79: finished never appeared within 10 s: the
      released probe never finished`;
    * no `started`: `nonblocking_reality_test.go:74: started never appeared within 10 s: the
      background refresh never started the probe; a non-blocking probe that never probes would
      leave the cache cold forever`;
    * no `running`: `reality_lock_test.go:72: running never appeared within 10 s: the probe did not
      start`.
  * **Gates** over the four test files: `make pre-add-check`, "4 file(s) clean"; `vet_goos.py`,
    darwin, windows and linux each exit 0; `make staticcheck`, exit 0, the same 56 ignored names;
    `make vulncheck`, exit 0; `make race`, 45 packages ok, no FAIL, panic or race line;
    `nonascii_scan.py --diff`, 0.
  * **Hosts** (`PINS_PHASE=0225d2`), over `HEAD` plus the six changed files: Linux `go test -race`
    exit 0, 45 ok; Windows `make ci-windows` exit 0, "ALL SELECTED CHECKS PASSED", 45 ok; both
    trees removed, and their absence checked.
  * `reality_lock_test.go` staged with the rest; the S3 step 3 note on this wording is resolved here.

### P0 — 2026-10-31

* `HEAD` `c10d8208`, the owner's commit of this pair and of 0224's close. `git status --short`: this
  pair's amendments only.
* Every line in P0 step 2 read and holding: `nonblocking_reality_test.go:12`, `:58`;
  `collaboration_test.go:161`, `:183`, `:275-276`, `:339-340`, `:357-358`; `store_reality.go:316`;
  `provider.go:1009`; `live_contract_test.go:85-89`.

### S1 — 2026-10-31

On `git archive` copies of `c10d8208` (`leftovers/0225_repro.py`):

* E1, `-race -count=5`: exit 1, 5 of 5 FAIL, `nonblocking_reality_test.go:44: the binary ran before
  the call returned: the probe is still synchronous on the caller's goroutine`.
* E2, five separate `-race -count=1` processes: exit 1, 5 of 5 FAIL, 5 data races. Each names the
  write at `provider.go:1009` from `:995`, and the read at `collaboration_test.go:340`.

### S2 — 2026-10-31

`leftovers/0225_s2_apply.py`, with every anchor asserted once:

1. **D1:** `TestNonBlockingProbeRunsNoBinaryOnAColdCache` replaced by
   `TestNonBlockingProbeReturnsWhileTheProbeRuns`, from `leftovers/0225_new_nonblocking_test.go.txt`.
   It uses the package's `waitForFile` (`reality_lock_test.go:20`) rather than a helper of its own:
   the first validation found a second `waitForFile` would not compile. `strconv` imported.
2. **D2:** `liveEngine` added at the end of `collaboration_test.go`. `:161`, `:183`, `:275-276`,
   `:339-340` and `:357-358` read through it.
3. **D4, as amended:** `live_contract_test.go`'s locked block became `eng, _ := liveEngine(p)`, a
   `t.Fatal` on a nil engine, then the same three reads from `eng`.
4. `gofmt -l internal/provider/codex/`: nothing. `go vet ./internal/provider/codex/` and `go vet
   -tags live_codex_contract ./internal/provider/codex/`: exit 0. `git diff --stat`: the three test
   files and this pair; 148 insertions, 60 deletions.

### S3 — 2026-10-31

`leftovers/0225_s3.py`; perturbations and plants on `tree_copy.py` copies of the changed tree, each
removed.

1. **Green, in the tree:**
   * both tests, `-race -count=50`: exit 0, 100 PASS, 0 FAIL, 0 data races;
   * `go test -race -count=1 ./internal/provider/codex/`: exit 0, 374 top-level PASS, 0 FAIL, 0 data
     races.
2. **Perturbed:**
   * E1's 100 ms pause after the call returns, `-count=5`: the probe test, 5 of 5 PASS;
   * E2's 300 ms pause after `Kill`, five separate processes: the collaboration test, 5 of 5 PASS, 0
     data races.
3. **Plants, each failing as required:**
   * the probe on the caller's goroutine: exit 1, `nonblocking_reality_test.go:63: the call had not
     returned after 5s with the probe still running: it waits for the probe on the caller's
     goroutine`;
   * `liveEngine` without `p.mu`, with E2's pause, five separate processes: exit 1, 5 of 5 FAIL, 5
     data races;
   * the stub never writes `finished`: exit 1, `nonblocking_reality_test.go:80: finished never
     appeared: the probe did not start`. The test fails as required and names the file. The text
     after the colon is `waitForFile`'s fixed wording (`reality_lock_test.go:28`), written for a
     `started` marker; it reads wrongly for `finished`. That helper is outside this plan's scope, so
     it is recorded here and not changed.
4. **Absence:** `p.eng` and `p.generation` appear in `collaboration_test.go` only at `:347`, inside
   the polling loop's `p.mu` section, and at `:454`, inside `liveEngine`. In `live_contract_test.go`,
   nowhere.
5. **The live test, built not run** (`leftovers/0225_live_build.py`): `go vet -tags
   live_codex_contract` and `go test -c -tags live_codex_contract` for darwin/arm64, linux/amd64 and
   windows/amd64, each exit 0. Plant: with the call renamed to an undefined name in a copy, the
   tagged vet fails with `live_contract_test.go:88:12: undefined: liveEngine0225Plant`, and the
   untagged vet passes. So the tag is what compiles the file, and the green builds compiled it.
6. **The gates** (`leftovers/0225_gates.py`):
   * `make pre-add-check` on the three files: exit 0, "3 file(s) clean (gofmt, golint,
     govulncheck)";
   * `vet_goos.py`: darwin/arm64, windows/amd64 and linux/amd64, each exit 0;
   * `make staticcheck`: exit 0, the same 56 ignored acp-go-sdk2 unstable-schema names on each GOOS;
   * `make vulncheck`: exit 0, "clear; allowlisted required-module findings: GO-2026-5932";
   * `make race`: exit 0, 45 packages ok, no FAIL, panic or DATA RACE line;
   * `nonascii_scan.py --diff`: 0 characters outside printable ASCII.

### S4 — 2026-10-31

`PINS_PHASE=0225 leftovers/pins_hosts.py <linux> <windows>`, over `HEAD` plus the five changed files:

* Linux test host, go1.27.3 linux/amd64: `go test -race -count=1 -timeout=40m ./...` exit 0, 45 ok
  lines.
* Windows test host, go1.27.3 windows/amd64: the dry-run check passed; `make ci-windows` exit 0, "ALL
  SELECTED CHECKS PASSED", 45 ok lines.
* Both scratch trees removed, and their absence checked (exit 0).

### S5 — 2026-10-31

1. This record.
2. `check_records.py --write-index`, `make check-records`, `make markdownlint-docs` and
   `guard_preview.py`, each exit 0. The index already held this pair's rows, from `c10d8208`. The
   pair through `markdownlint-cli2 --no-globs` first failed with MD047 at this file's end: the edit
   that wrote this section dropped the trailing newline. Restored, it reports "0 issues in 0
   files".
3. **Staged** under C1: the three test files and this pair. `check_records.py --check-all` ran on the
   staged tree, exported with `git checkout-index`. The owner commits. With deviation 2,
   `reality_lock_test.go` was staged too: six files. `--check-all` on that staged tree: exit 0.
   The owner committed `85e10fb0`.

### S6 — 2026-10-31

1. `leftovers/guard_range.py origin/master HEAD`: exit 0, no output, over `c12e6e37..85e10fb0`. That
   range is the two unpushed commits: `c10d8208` (this pair, proposed, and 0224's close) and
   `85e10fb0` (this change).
