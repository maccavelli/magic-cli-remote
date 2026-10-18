---
status: proposed
date: 2026-10-18
associated-madr: "0198-MADR-prove-grok-auto-mode-by-any-write-not-one-filename.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement proving grok's auto mode by any write in the workspace

Associated MADR: [0198-MADR-prove-grok-auto-mode-by-any-write-not-one-filename.md](0198-MADR-prove-grok-auto-mode-by-any-write-not-one-filename.md)

* The owner chose this resolution on 2026-10-18 ("Retarget the proof").
* The owner approves the PLAN by name.
* Go files pass `make pre-add-check`.
* The owner commits (`git commit --no-edit`). An agent stages only.
* T3's live run spends quota. The owner's choice included it: about 3 minutes, one test.

## Goal

The armed half of `TestLiveGrokAutoDiscriminationPair` proves "writes go through with no prompt"
whatever file the model writes. B2 and 0194 close on the run that shows it.

## Scope

* `internal/provider/grok/workspace_files_test.go` (new, no build tag): the helper and its unit
  test.
* `internal/provider/grok/live_automode_test.go`: the armed half's check, its log line, and the
  doc comment.
* Records:
  * the 0184 PLAN (B2) and the 0194 PLAN, each with a dated entry for the full run and its
    outcome;
  * the 0194 PLAN's status.
* Out of scope: the prompt text, the unarmed half, and the other live tests.

## Implementation Steps

### T1. The helper, test first (A4)

1. Write `TestWorkspaceFilesSkipsHiddenEntries` first. In a `t.TempDir()`:
   * an empty directory gives no files;
   * `probe.txt`, and `docs/decisions/0001-MADR-x.md`, are returned, as sorted slash paths;
   * `.grok/state.json`, a top-level `.hidden`, and an empty directory are not returned.
2. Run it and see it fail to compile, because `workspaceFiles` does not exist yet.
3. Write `workspaceFiles(dir string) ([]string, error)`. It walks `dir`, skips any entry whose name
   starts with `.` (a directory with all its contents), and returns regular files as relative slash
   paths, sorted.

### T2. The live test (A1–A3)

1. The armed half requires `prompts == 0` and `len(files) > 0`. The failure message names the case:
   no prompt and no write means the session was silently blocked.
2. The log line prints `files=` with the list, in place of `wrote=`.
3. The doc comment says why: the model may write something other than `probe.txt`, depending on
   the host's agent rules (the 0198 MADR).

### T3. Prove it

1. On a scratch copy, plant `workspaceFiles` returning hidden entries too, and then returning
   nothing. `TestWorkspaceFilesSkipsHiddenEntries` must fail both times.
2. `go vet -tags live_grok ./internal/provider/grok/` and the package's untagged tests.
3. With the owner's approval as given: `go test -tags live_grok ./internal/provider/grok/ -run
   '^TestLiveGrokAutoDiscriminationPair$' -count=1 -v`. It must pass, and the armed half's log must
   show at least one file and `prompts=0`.

### T4. Gates

* `make pre-add-check` on both Go files, `make lint`, `make staticcheck`, `go test -race ./...`.
* `check_records.py --check-all`.

### T5. Records (A5)

* The 0184 PLAN's B2 and the 0194 PLAN each get a dated entry: the full run, 102 of 103 passing,
  the one failure and its cause by link to this MADR, and T3's run. If T3 passes, B2 is done and
  the 0194 PLAN goes to `complete`.

## Verification

The MADR's Confirmation: T3's planted failures, and the live pass.

## Rollout and Rollback

* Tests and records only. Rollback is reverting the commit, which brings back a check that depends
  on the model's choice of file.

## Execution record

Not started.
