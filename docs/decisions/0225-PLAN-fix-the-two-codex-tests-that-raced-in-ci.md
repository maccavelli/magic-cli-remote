---
status: proposed
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
* every read of a live engine's state in `collaboration_test.go` takes `p.mu` (D2).

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

**Records:** this pair, and `docs/README.md`'s two index rows for it.

**Not in scope:**

* `store_reality.go` and `provider.go`: no production change (D3).
* `live_contract_test.go:86-88`, under the `live` tag (D4).
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
   `reality_lock_test.go:19-32`. A moved line is re-read, not assumed.

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
3. **Gofmt** both files. `git diff --stat` lists only Scope's files.

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
5. **The gates:** `make pre-add-check FILES="…"` on both files; `leftovers/vet_goos.py`;
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
3. **Stage**, under C1: Scope's two test files, this pair, and `docs/README.md` as this pair's two
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

* `live_contract_test.go:86-88` (MADR D4).
* A search of other packages' tests for the same patterns.

## Execution record

None yet.
