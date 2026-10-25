---
status: in-progress
date: 2026-10-25
associated-madr: "0215-MADR-test-darwin-arm64-natively-in-ci.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement a native darwin/arm64 CI leg

Associated MADR: [0215-MADR-test-darwin-arm64-natively-in-ci.md](0215-MADR-test-darwin-arm64-natively-in-ci.md)

## Goal

Every push and pull request that runs `ci.yml` runs the Go suite natively on `darwin/arm64`, cgo-free, with one
retry and the flake ledger, then a cgo-free race pass. A darwin-only regression in the update path's signature
checks turns CI red.

## Scope

* **In scope:** the MADR's D1 to D4.
* **Not in scope:**
  * a macOS Flutter or iOS lane;
  * the release build;
  * branch-protection settings (D5).

## Implementation Steps

### Rules

* **Files:**
  * `.github/workflows/ci.yml`;
  * `docs/decisions/0120-MADR-retire-the-darwin-amd64-target.md` (a dated note under D11);
  * this pair;
  * `docs/README.md`'s generated index.

  A file outside this list is a deviation: stop, prompt and record.
* **Every new gate is seen failing first,** on a scratch copy, never in the working tree.
* **Before staging:**
  * `actionlint` on `ci.yml`;
  * `make check-records` and `make markdownlint-docs`;
  * markdownlint on this pair and 0120;
  * the guard's redact preview from the repository root.
* **The owner commits and pushes.** P2 waits for that push.

### P1: the workflow and the records

1. **`ci.yml`, `go-native`'s matrix.** Add `- { runner: macos-15, label: darwin/arm64, shell: bash }` after the
   Windows row, with a comment citing 0215 D1 and D3.
2. **`ci.yml`, a step after `Test` and before `Emit flake ledger row`:**

   ```yaml
   - name: Test (race, darwin, cgo-free)
     if: runner.os == 'macOS'
     run: go test -race ./...
   ```

   Comment it with 0215 D2: the job's `CGO_ENABLED=0` stands, darwin races without cgo (0116 D20), and there is
   no retry.
3. **`ci.yml`, the comment block `# No go-macos job, deliberately (MADR 0120 D11)…`.** Replace it with two lines
   saying `darwin/arm64` runs in `go-native` (0215, superseding 0120 D11).
4. **0120:** a dated note under D11: "Superseded 2026-10-25 by
   0215-MADR-test-darwin-arm64-natively-in-ci.md, which re-measured the gated surface." 0120's text is otherwise
   unchanged.
5. **The index:** `python3 scripts/check_records.py --write-index`, and confirm that its diff is this pair's two
   rows only.

### P1's checks

* **actionlint** (`go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12`, gobble-cli's pin) on `ci.yml`:
  exit 0.
  * Seen failing first on a scratch copy with the new step's `if:` misspelt (`runner.oss`).
* **The leg's commands, seen failing first,** in a scratch clone of HEAD with P1 applied, on this Mac:
  * **The plant:** `updateclient/client.go`'s `runtime.GOOS == "darwin"` codesign condition is made never true.
    * `CGO_ENABLED=0 go test ./internal/updateclient/` must fail `TestNewProbesTheStagedBinary`.
    * `GOOS=linux go vet ./internal/updateclient/` must pass, showing the Linux lanes cannot see it.
  * **Without the plant:**
    * `CGO_ENABLED=0 go build ./...`, `go vet ./...` and `go test ./...` exit 0;
    * `CGO_ENABLED=0 go test -race ./...` exits 0.
* The records gates above.

Stage the four files, and record P1 below.

### P2: the first CI run

After the owner's push:

* Read the run with `gh run list --workflow CI` and `gh run view <id> --json jobs`.
* Record the run ID, `Go (darwin/arm64)`'s conclusion, and the durations of its Test and race steps.
* Record whether the Test step retried.

A red leg is a deviation. Record the failure as quoted, give its cause, and offer resolutions; skipping a test
is not one of them.

When the leg is green, both records become `accepted` / `complete` in one staged change.

## Verification

* P1's checks pass, and both plants fail as described.
* The first CI run after the push has `Go (darwin/arm64)` green, including its race step.

## Rollout and Rollback

* **Rollout:** one commit, then the owner's push. The leg runs from that push on.
* **Rollback:** revert the commit. The other legs are unchanged by it, so a revert restores the previous CI
  exactly. If the leg flakes, 0143's ledger records it. A leg that flakes repeatedly is a new deviation here; it
  is not deleted.

## Execution record

### P1, 2026-10-25: the workflow and the records

**Approval.** The owner answered "proceed to p1".

**The change, by file:**

* `.github/workflows/ci.yml`:
  * `go-native`'s matrix gains `{ runner: macos-15, label: darwin/arm64, shell: bash }`, after the Windows row
    (D1, D3).
  * `Test (race, darwin, cgo-free)`, `if: runner.os == 'macOS'`, runs `go test -race ./...` under the job's
    `CGO_ENABLED=0`, with no retry (D2). It sits after `Test` and before the flake-ledger steps.
  * The 14-line comment explaining the missing job is replaced by two lines citing this pair (D4).
* `0120-MADR-retire-the-darwin-amd64-target.md`: a dated note under D11 points here. Its measurement is unchanged.
* `docs/README.md`: regenerated with `check_records.py --write-index`. The diff is this pair's two rows only.

**Checks**, all on scratch copies (`macci/p1_checks.py`, session scratchpad):

| Check | Result |
| :--- | :--- |
| actionlint v1.7.12 on HEAD's `ci.yml` (baseline) | exit 0 |
| actionlint on the edited `ci.yml` | exit 0 |
| actionlint on a copy with `if: runner.oss == 'macOS'` (the plant) | exit 1: `property "oss" is not defined in object type {arch: string; …; os: string; …} [expression]` |
| The plant: `newProbes`' darwin condition never true; `CGO_ENABLED=0 go test -run TestNewProbesTheStagedBinary ./internal/updateclient/` on this Mac | exit 1: `--- FAIL: TestNewProbesTheStagedBinary`, "with a signing identity: newProbes = 1 probes … want 2 and the version probe" |
| The same plant, `GOOS=linux go vet ./internal/updateclient/` | exit 0: the Linux and Windows lanes cannot see it |
| Without the plant, in a clone of HEAD with P1's files over it, `CGO_ENABLED=0`: `go build ./...`, `go vet ./...` | exit 0, 4 s and 5 s |
| `CGO_ENABLED=0 go test -count=1 ./...` | exit 0, 65 s, 43 packages `ok` |
| `CGO_ENABLED=0 go test -race -count=1 ./...` | exit 0, 69 s, 43 packages `ok` |

The clone was deleted.

**The records gates:**

| Gate | Result |
| :--- | :--- |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |
| markdownlint on this pair | 0 issues |
| markdownlint on 0120 | 35 `MD004/ul-style` errors ("Expected: dash; Actual: asterisk"), all on lines 207–437 |
| The guard's redact preview | "would edit 0 file(s)" |

The 0120 errors predate this change. The dated note was inserted after line 545, so it cannot move or change
lines 207–437, and the note holds no list. 0120 carries no `markdownlint-disable` for MD004, unlike this pair.
`make markdownlint-docs` does not lint decision records, which is why CI has never reported them. Rewriting 0120's
list markers is outside P1's files' purpose; it is left as the owner's call.

**Not done in P1:** the CI run itself, which is P2, after the owner's push.
