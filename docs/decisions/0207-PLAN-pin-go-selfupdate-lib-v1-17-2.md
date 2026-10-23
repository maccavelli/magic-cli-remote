---
status: in-progress
date: 2026-10-22
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the `go-selfupdate-lib v1.17.2` pin (0207 D1)

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its D1:

> Pin `go-selfupdate-lib v1.17.2`. `go.mod` and both `ci.yml` pins move together, the pins to
> `036289ecc701d84c7749f4950a57a154c1af2cf4` with the comment `# go-selfupdate-lib v1.17.2` (0054
> D1; `TestWorkflowPinsMatchGoMod`). No Go code changes.

Sibling plans of the same MADR:
[0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md](0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md)
(D2, D10 to D13) and
[0207-PLAN-finish-full-semver-pins-and-check-libs.md](0207-PLAN-finish-full-semver-pins-and-check-libs.md)
(D5). This plan is D1 alone: "one library per change, so a regression bisects to one library"
(the MADR's drivers).

## Goal

* `go.mod` requires `github.com/maccavelli/go-selfupdate-lib v1.17.2`, and `go.sum` holds its two
  hashes and no others for it.
* Both reusable-workflow calls in `.github/workflows/ci.yml` name `036289ec…` with the comment
  `# go-selfupdate-lib v1.17.2`. The next tag's publish job then builds the library's release tools
  with Go 1.27.3, closing GO-2026-6809 there (the library's CHANGELOG, "Security").
* `TestWorkflowPinsMatchGoMod` and `TestReleaseJobsReadTheSpec` pass, and `make check-libs` reports
  both libraries at their latest tag with every check ok.

## Scope

* **In scope:** 0207 D1. The files are `go.mod`, `go.sum`, `.github/workflows/ci.yml`, this plan,
  the 0207 MADR (a dated note on D1), and the records index in `docs/README.md`.
* **Not in scope:**
  * 0207 D3, D4, and D8's `binaryWarning` half, which wait for their own plan.
  * Any Go source or test change. D1 says "No Go code changes". The step-by-step checks below stop
    the phase if one turns out to be needed.
  * `ci.yml:660`'s comment, "(go-selfupdate-lib v1.15.2, internal/cmd/selfupdate-release/plan.go)".
    It names the release that introduced the rehearsal stamp rule, and that rule is unchanged at
    `v1.17.2` (`plan.go:95`, `StampVersion: "rehearsal-" + in.SHA[:12]`). It is a citation, not a
    pin.
  * Records that state `v1.17.1` as history. These are 0054's S9 amendment and its PLAN, and
    0207's REPORT, a dated observation ("Magic pins `v1.17.1`", 2026-10-21). None of them claims to
    be current.
  * The `procutil` flake found during this plan's probe (finding F-b below).
  * gobble's pins, which are gobble's.

## Context: the facts this plan rests on [verified 2026-10-22]

**The release.**

* `git ls-remote --tags https://github.com/maccavelli/go-selfupdate-lib.git` lists `v1.17.0`,
  `v1.17.1` and `v1.17.2`, and nothing newer:

  | Tag | Tag object | Peeled commit |
  | :--- | :--- | :--- |
  | `v1.17.1` | `2288228001e7b781284cd53c8d607ed606f95d2f` | `e8d4904a09194d99761042a8996983e8d5b957eb`, today's pin |
  | `v1.17.2` | `3f52f96a3e262861e200a754666056cbeb0c6ff7` | `036289ecc701d84c7749f4950a57a154c1af2cf4` |

* `go mod download -json github.com/maccavelli/go-selfupdate-lib@v1.17.2` gives
  `"Sum": "h1:pe6sZ1U4/efMOc6q5KvFH5tvbxZQLLgK++jY4qIos+c="`,
  `"GoModSum": "h1:boXH5t3XBFF7sNNQL8r/w1TUJjuzjrDh7bKDCrlP0Q8="`, and `"Origin"` hash `036289ec…`
  at `refs/tags/v1.17.2`. So the proxy's module is the tag's commit.

**What changed in the library** (its clone, `git diff v1.17.1 v1.17.2`).

* Every non-test `.go` file: no change. Both reusable workflows, `build-selfupdate-release.yml` and
  `publish-selfupdate-release.yml`: no change. The workflows' inputs, outputs and secrets are
  therefore what Magic calls today.
* Its `go.mod`: `go 1.26.9` → `go 1.26.10`, and `toolchain go1.27.2` → `go1.27.3`.
* The publish workflow sets up Go from the library's own `go.mod`
  (`go-version-file: .core-lib-release-tools/go.mod`, `publish-selfupdate-release.yml:135` and
  `:310`). Moving the pin moves its release tools to Go 1.27.3. The build workflow sets up Go from
  the caller's `go.mod` (`build-selfupdate-release.yml:107`), which for Magic is already
  `go 1.27.3` (`go.mod:3`).
* The library's migration guide, "From v1.17.1 to v1.17.2", asks a caller for exactly two things:
  "Build your program with Go 1.27.3 or 1.26.10", which Magic does, and "Move your workflow pins
  to `v1.17.2`". It adds: "Nothing else is required."

**Where Magic pins it** (`git grep` outside the records):

| Place | Today |
| :--- | :--- |
| `go.mod:12` | `github.com/maccavelli/go-selfupdate-lib v1.17.1` |
| `go.sum:70-71` | the `v1.17.1` `h1:` and `/go.mod h1:` lines |
| `.github/workflows/ci.yml:712` | `build-selfupdate-release.yml@e8d4904a… # go-selfupdate-lib v1.17.1` |
| `.github/workflows/ci.yml:733` | `publish-selfupdate-release.yml@e8d4904a… # go-selfupdate-lib v1.17.1` |

No guide, README or architecture page names the version. The previous move, `e3baf73a` (to
`v1.17.1`), changed the same three files.

**The guards.**

* `internal/updateclient/spec_test.go:138`, `TestWorkflowPinsMatchGoMod`, requires:
  * that `go.mod` require the library with no `replace`;
  * that `ci.yml` make exactly two library-workflow calls;
  * that each be pinned to a 40-hex commit, with the line comment `# go-selfupdate-lib <go.mod's
    version>`.

  Its doc says: "That the commit is that release's tag is checked by hand, with git ls-remote, when
  the pin moves." Step L1.1 is that check.
* `TestReleaseJobsReadTheSpec` (`:218`) requires the build and publish calls to share one pin.

**The change, proven on a scratch clone of `HEAD` `864d9917`** (`d1_probe.py`, the working tree
untouched, `git status --porcelain` clean afterwards):

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.17.1 => v1.17.2
go mod tidy: exit 0
-    github.com/maccavelli/go-selfupdate-lib v1.17.1    (go.mod; a tab in the file)
+    github.com/maccavelli/go-selfupdate-lib v1.17.2
-github.com/maccavelli/go-selfupdate-lib v1.17.1 h1:2SFtFnuxrQXs3A9/nMyAOS3IH6uk8pAQ8J7fgFQFKTw=
-github.com/maccavelli/go-selfupdate-lib v1.17.1/go.mod h1:h/r1sys9pS9HpbvACla1bifv1PsBmKl/a4gqgq06SFY=
+github.com/maccavelli/go-selfupdate-lib v1.17.2 h1:pe6sZ1U4/efMOc6q5KvFH5tvbxZQLLgK++jY4qIos+c=
+github.com/maccavelli/go-selfupdate-lib v1.17.2/go.mod h1:boXH5t3XBFF7sNNQL8r/w1TUJjuzjrDh7bKDCrlP0Q8=
guard, go.mod moved alone: exit 1
   spec_test.go:186: build-selfupdate-release.yml@e8d4904a… is commented "# go-selfupdate-lib v1.17.1"; want "# go-selfupdate-lib v1.17.2", the release go.mod requires
guard, ci.yml moved alone: exit 1
   spec_test.go:186: build-selfupdate-release.yml@036289ec… is commented "# go-selfupdate-lib v1.17.2"; want "# go-selfupdate-lib v1.17.1", the release go.mod requires
all three moved: .github/workflows/ci.yml | 4 ++--, go.mod | 2 +-, go.sum | 4 ++--
guard, all three moved: exit 0
go build ./...: exit 0
go vet darwin/arm64, linux/amd64, windows/amd64: exit 0
go test -race ./...: 42 ok, one FAIL: TestTerminateProcessGroupKillsGrandchildren (internal/procutil), F-b
scripts/vulncheck.sh: exit 0; vulncheck: clear; allowlisted required-module findings: GO-2026-5932
```

`make check-libs` on `HEAD` before this plan (0207 D5's V2) checked `v1.17.2` and failed only
`TestWorkflowPinsMatchGoMod`, because it moves `go.mod` alone. Every other check was ok, apidiff
included.

## Findings

* **F-a — The pin guard is two-sided, so no new test is needed.** The probe saw it fail with
  `go.mod` moved alone and with `ci.yml` moved alone, and pass with both. This is the fail-first
  evidence for this plan's only gate, and L1 repeats it.
* **F-b — `TestTerminateProcessGroupKillsGrandchildren` flakes under load, unrelated to D1.**
  * It failed once in the probe's full race suite, at `procutil_test.go:109`, "expected
    escalation".
  * On the real tree at `HEAD`, `go test -race -count=30 -run
    '^TestTerminateProcessGroupKillsGrandchildren$' ./internal/procutil/` passed all 30 runs.
  * `go list -deps ./internal/procutil/` names no `go-selfupdate-lib` package, so this change
    cannot reach it.
  * The likely cause is the test's fixed 200 ms sleep before the SIGTERM. Under a loaded race run,
    the signal can land before the shell has installed `trap '' TERM`, so the group exits
    gracefully and the test reads that as no escalation. This is inferred from the test, not
    reproduced.
  * It gets its own record (Q3). This plan does not touch it.

## Questions for the owner

* **Q1 — The MADR's status.** 0207 stays `proposed` while D3, D4 and D8's `binaryWarning` half are
  undecided. Approving this plan approves D1, recorded as a dated note on D1. *Recommended.* The
  alternative sets the whole MADR to `accepted` now, which would accept D3 and D4 unplanned.
* **Q2 — The rehearsal.** After the owner pushes L1, dispatch CI once (`gh workflow run ci.yml
  --ref master`). The `build` job then runs the library's build workflow at the new pin, a
  rehearsal that never publishes (0054's S4 amendment). *Recommended: yes, on the owner's word for
  that run, because it is the only check of the new pin on GitHub before a tag.* The alternative
  leaves it to the rehearsal before the next tag.
* **Q3 — F-b.** Record the `procutil` flake for its own fix (a MADR/PLAN pair under the next free
  number), or leave it? *Recommended: record it, after this plan.*

**The owner's answers, 2026-10-22:** "Recommended for any and proceed". So for Q1, D1 is approved
and recorded as a dated note, and the MADR stays `proposed`. For Q2, the rehearsal runs after the
push, on the owner's word for that run. For Q3, F-b gets its own record after this plan. The answer
also approves this plan.

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Each step's expected output is stated.** Any other output stops the phase. It is a deviation,
  with the evidence, the resolutions and the cost of doing nothing.
* **Fail-first plants run on a scratch clone,** never in the working tree.
* **The phase ends with its execution record** in this plan, and is staged for the owner, who
  commits it.
* No push, dispatch or tag without the owner's word in that turn.

### L1 — Move the three pins together (D1)

**Files:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`, this plan, the 0207 MADR, and
`docs/README.md` (the records index, regenerated).

1. **Preconditions.**
   * `git status --porcelain` is empty, and `HEAD` is `master`.
   * `git ls-remote https://github.com/maccavelli/go-selfupdate-lib.git 'refs/tags/v1.17.2^{}'`
     prints `036289ecc701d84c7749f4950a57a154c1af2cf4`. This is the check by hand that
     `TestWorkflowPinsMatchGoMod`'s doc requires.
   * `git ls-remote --tags` lists no tag above `v1.17.2`. A newer tag stops the phase: D1 names
     `v1.17.2`, so a newer one needs an amendment first.
2. **The module.** From the repository root, run
   `go get github.com/maccavelli/go-selfupdate-lib@v1.17.2`, then `go mod tidy`.
   * Expected: `go: upgraded github.com/maccavelli/go-selfupdate-lib v1.17.1 => v1.17.2`, and tidy
     exit 0.
   * `git diff --stat -- go.mod go.sum` shows `go.mod | 2 +-` and `go.sum | 4 ++--`. The added
     lines are exactly the three in the probe output above, with the hashes above.
   * Anything else stops the phase: another module moving, or the `go` or `toolchain` line
     changing. Magic's `go 1.27.3` already satisfies the library's `go 1.26.10`.
3. **The workflow pins.** A Python script in the scratchpad (`d1_pins.py`) replaces
   `@e8d4904a09194d99761042a8996983e8d5b957eb # go-selfupdate-lib v1.17.1` with
   `@036289ecc701d84c7749f4950a57a154c1af2cf4 # go-selfupdate-lib v1.17.2` in `ci.yml`.
   * It asserts exactly two occurrences before the replacement, and none of the old string
     after it.
   * `git diff --stat -- .github/workflows/ci.yml` shows `4 ++--`, on lines 712 and 733 only.
4. **Fail-first, repeated on a scratch clone** of the working tree:
   * `go.mod` and `go.sum` at L1's state and `ci.yml` at `HEAD`'s: `TestWorkflowPinsMatchGoMod`
     fails with the two `spec_test.go:186` lines above;
   * `ci.yml` at L1's state and `go.mod` at `HEAD`'s: it fails with the reversed message.

   The scratch clone is deleted afterwards.
5. **Gates, on the working tree.**
   * `go test -count=1 -run '^(TestWorkflowPinsMatchGoMod|TestReleaseJobsReadTheSpec)$'
     ./internal/updateclient/` passes.
   * `go build ./...`, and `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64
     (`CGO_ENABLED=0`): each exit 0.
   * `make test` and `make race`. A failure in a package whose `go list -deps` holds no
     `go-selfupdate-lib` package is recorded with that evidence and reported to the owner. Any
     other failure stops the phase.
   * `make vulncheck`: `vulncheck: clear`, with GO-2026-5932 allowlisted as today.
   * `go mod tidy -diff` prints nothing.
   * No `.go` file is staged, so `make pre-add-check` has nothing to check.
   * **Hosts.** The `available-dev-hosts` rule exempts this change from a run on other hosts. The
     library's non-test Go code is byte-identical between the tags, so no compiled code changes on
     any system. The three `go vet` runs build every target, and CI's own jobs run on push.
6. **Records.**
   * The 0207 MADR gets a dated note under D1: carried out by this plan, with the `ls-remote`
     evidence and the owner's answer to Q1.
   * `python3 scripts/check_records.py --write-index` adds this plan to the index. `make
     check-records`, and markdownlint on this plan and the MADR, pass.
   * L1's execution record goes here.
7. **Stage** the six files. The owner commits.

### L2 — `make check-libs` on the committed pin

**Files:** this plan, and the 0207 MADR when its D1 note needs the verdict. It is a records-only
commit, under the bootstrap exception.

1. After the owner commits L1, `make check-libs` checks the committed `HEAD`. Expected:
   * `acp-go-sdk2 versions: pinned v1.20.0, checking v1.20.0`, and every check `ok`;
   * `go-selfupdate-lib versions: pinned v1.17.2, checking v1.17.2`, every check `ok`, and `api:
     pinned at v1.17.2; nothing to compare`;
   * `0 failing checks; scratch removed`, exit 0.
2. `git status --porcelain` shows only this plan's own edits after the run.
3. The verdicts go in L2's execution record. The plan becomes `complete` here unless Q2's
   rehearsal is still to run. Stage; the owner commits.

### L3 — The rehearsal (Q2; only on the owner's word)

1. After the owner pushes, run `gh workflow run ci.yml --ref master`, then
   `gh run watch <id> --exit-status` to watch the run once.
2. Expected:
   * `Build self-update release` succeeds at `build-selfupdate-release.yml@036289ec…`;
   * the build stamps `rehearsal-<sha12>`;
   * `Publish self-update release` is skipped, since it runs on a tag only.
3. The run's URL and its job results go in this plan's execution record, and the plan becomes
   `complete`.
4. **The publish workflow's Go 1.27.3 release tools run only on a tag.** They are first exercised
   by the next release, after `make ci-windows-smoke` on the Windows host (`AGENTS.md`). That
   release's record notes it.

## Verification

* `go.mod:12` reads `v1.17.2`. `go.sum` holds only the `v1.17.2` lines for the library, with the
  hashes above.
* `ci.yml:712` and `:733` read `@036289ecc701d84c7749f4950a57a154c1af2cf4 # go-selfupdate-lib
  v1.17.2`.
* `TestWorkflowPinsMatchGoMod` and `TestReleaseJobsReadTheSpec` pass, and the guard was seen failing
  in both one-sided directions.
* Build, vet for three systems, `make test`, `make race`, `make vulncheck` and `go mod tidy -diff`
  are as stated in L1.5.
* `make check-libs` exits 0 on the committed pin (L2).
* With Q2: the rehearsal's build job succeeds at the new pin (L3).

## Rollout and Rollback

* **Rollout.** Nothing an installed `mcremote` does changes: the library's non-test code is
  identical between the tags. The pin reaches users at the next tag, whose publish job builds the
  release tools with Go 1.27.3.
* **Rollback.** Revert L1's commit. All three pins return together, and `TestWorkflowPinsMatchGoMod`
  fails any revert that moves one alone. L2's and L3's records stay as history.

## Execution record

### L1, 2026-10-22: the three pins moved together

Executed as written, with no deviation.

**L1.1, preconditions.**

* `git status --porcelain --branch`: `## master...origin/master`, with only this plan untracked.
* `git ls-remote … 'refs/tags/v1.17.2^{}'`: `036289ecc701d84c7749f4950a57a154c1af2cf4`.
* `git ls-remote --tags --sort=-v:refname`: the first entry is `v1.17.2`, so no newer tag exists.

**L1.2, the module.**

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.17.1 => v1.17.2
go mod tidy: exit 0, no output
go.mod @@ -12 +12 @@   v1.17.1 -> v1.17.2
go.sum @@ -70,2 +70,2 @@
-github.com/maccavelli/go-selfupdate-lib v1.17.1 h1:2SFtFnuxrQXs3A9/nMyAOS3IH6uk8pAQ8J7fgFQFKTw=
-github.com/maccavelli/go-selfupdate-lib v1.17.1/go.mod h1:h/r1sys9pS9HpbvACla1bifv1PsBmKl/a4gqgq06SFY=
+github.com/maccavelli/go-selfupdate-lib v1.17.2 h1:pe6sZ1U4/efMOc6q5KvFH5tvbxZQLLgK++jY4qIos+c=
+github.com/maccavelli/go-selfupdate-lib v1.17.2/go.mod h1:boXH5t3XBFF7sNNQL8r/w1TUJjuzjrDh7bKDCrlP0Q8=
```

The hashes are the proxy's (Context, above), and no other line moved.

**L1.3, the workflow pins.** `d1_pins.py` asserted two occurrences, and printed "moved the pins on
lines [712, 733]". `git diff --stat`: `ci.yml | 4 ++--`, `go.mod | 2 +-`, `go.sum | 4 ++--`, "3
files changed, 5 insertions(+), 5 deletions(-)".

**L1.4, fail-first** (`d1_failfirst.py`, a scratch clone, removed afterwards):

| Case | Result |
| :--- | :--- |
| A: `go.mod` and `go.sum` moved, `ci.yml` at `HEAD` | exit 1: `spec_test.go:186: build-selfupdate-release.yml@e8d4904a… is commented "# go-selfupdate-lib v1.17.1"; want "# go-selfupdate-lib v1.17.2"`, and the same for publish |
| B: `ci.yml` moved, `go.mod` and `go.sum` at `HEAD` | exit 1: `…@036289ec… is commented "# go-selfupdate-lib v1.17.2"; want "# go-selfupdate-lib v1.17.1"`, for both |
| C: all three moved | exit 0, `ok …/internal/updateclient` |

**L1.5, gates on the working tree** (`gates.py`):

| Gate | Result |
| :--- | :--- |
| `TestWorkflowPinsMatchGoMod`, `TestReleaseJobsReadTheSpec` | ok |
| `go build ./...` | exit 0 |
| `go vet ./...`, darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test` | exit 0; 43 packages ok, 0 FAIL lines |
| `make race` | exit 0; 43 packages ok, 0 FAIL lines |
| `make vulncheck` | "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `go mod tidy -diff` | no output |

`make race` took `internal/procutil` from the test cache, as `(cached)`. The cache is keyed on the
package's inputs, and no `go-selfupdate-lib` package is among them (F-b). So the result is that
package's pass on unchanged code, not a new run of F-b's test. No `.go` file changed, so
`make pre-add-check` had nothing to check. The hosts are exempt, as L1.5 states.

**L1.6, records.** The 0207 MADR has its dated note under D1. `check_records.py --write-index`
added one index line, this plan's.

**Not done in L1.** L2 waits for the owner's commit, and L3 for the push and the owner's word for
the dispatch.

L1 was committed as `e715afe7`, on the owner's word ("Proceed, rehearse, commit, push and
proceed"), which also authorized L2's and L3's commits, the push and the dispatch.

### L2, 2026-10-22: `make check-libs` on the committed pin

`make check-libs` on `HEAD` `e715afe7`, with go-shared at `d62e2a2`, toolchain `go1.27.3`:

```text
acp-go-sdk2 versions: pinned v1.20.0, checking v1.20.0
acp-go-sdk2 get and tidy / version / modules / build / vet linux, darwin, windows / test: ok
acp-go-sdk2 api: pinned at v1.20.0; nothing to compare
acp-go-sdk2 govulncheck / advisories: ok
go-selfupdate-lib versions: pinned v1.17.2, checking v1.17.2
go-selfupdate-lib get and tidy / version / modules / build / vet linux, darwin, windows / test: ok
go-selfupdate-lib api: pinned at v1.17.2; nothing to compare
go-selfupdate-lib govulncheck / advisories: ok
0 failing checks; scratch removed
```

It exited 0. This is the expected output of L2.1, line for line. `go-selfupdate-lib test` is ok
where 0207 D5's run failed it, because `TestWorkflowPinsMatchGoMod` now sees the three pins agree.
`git status --porcelain` was empty after the run.
