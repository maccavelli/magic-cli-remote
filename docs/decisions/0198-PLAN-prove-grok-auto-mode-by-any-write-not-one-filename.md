---
status: complete
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

Executed 2026-10-18, after the owner committed the records (`049b2352`). There were no deviations.

### T1, 2026-10-18: the helper

`TestWorkspaceFilesSkipsHiddenEntries` was written first. Its first run failed to build:

```text
internal/provider/grok/workspace_files_test.go:16:17: undefined: workspaceFiles
FAIL	github.com/maccavelli/magic-cli-remote/internal/provider/grok [build failed]
```

With `workspaceFiles` in place it passes. The test also covers a hidden directory below a visible one
(`docs/.cache/y`), which the step did not list.

### T2, 2026-10-18: the live test

The armed half requires `prompts == 0` and at least one file. The log prints `files=`, and the doc
comment says why. The unused `os` and `path/filepath` imports were removed, which `go vet -tags
live_grok` caught.

### T3, 2026-10-18: proof

Each break was planted on a scratch copy of the tree, which was then deleted:

```text
## hidden entries counted: exit=1
   workspace_files_test.go:79: workspaceFiles = [".grok/state.json" ".hidden" "docs/.cache/y" "docs/decisions/0001-MADR-x.md" "probe.txt"], want ["docs/decisions/0001-MADR-x.md" "probe.txt"]
## returns nothing: exit=1
   workspace_files_test.go:79: workspaceFiles = [], want ["docs/decisions/0001-MADR-x.md" "probe.txt"]
```

The live run of the one test, on grok `1.0.50`:

```text
live_automode_test.go:88: arm=false prompts=1 files=[] turnDone=false
live_automode_test.go:88: arm=true  prompts=0 files=["docs/decisions/0001-MADR-create-probe-txt.md" "docs/decisions/0001-PLAN-create-probe-txt.md"] turnDone=true
--- PASS: TestLiveGrokAutoDiscriminationPair (157.46s)
    --- PASS: TestLiveGrokAutoDiscriminationPair/unarmed (91.06s)
    --- PASS: TestLiveGrokAutoDiscriminationPair/armed (66.41s)
```

The armed half again wrote a decision record and no `probe.txt`. The old check would have failed a
second time, so the first failure was not a one-off.

### T4, 2026-10-18: gates

```text
make pre-add-check (workspace_files_test.go, live_automode_test.go) -> 2 file(s) clean (gofmt, golint, govulncheck)
go vet ./internal/provider/grok/, and with -tags live_grok      -> exit 0
make lint, make staticcheck                                   -> exit 0
staticcheck v0.8.2 -tags live_grok ./internal/provider/grok/  -> exit 0
go test -race -count=1 ./...                                  -> exit 0, 42 packages ok
check_records.py --check-all                                  -> exit 0
```

### T5, 2026-10-18: records

The 0184 PLAN's B2 and the 0194 PLAN each have a dated entry. B2 is done, and the 0194 PLAN is
`complete`.
