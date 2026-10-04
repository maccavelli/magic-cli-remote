---
status: proposed
date: 2026-10-04
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Match wrapped protocol event-type lines without retagging

## Context and Problem Statement

`TestEventTypesAreDocumented` treats the protocol event-type enumeration as one line. The guide already wraps that sentence across four lines so it stays within the markdownlint line-length limit. The test fails on the wrapped names even though every emitted type is in the guide. Separately, annotated `v0.20.2` was asked to be recreated on current master after the test fix. The measured history does not match that request, so the retag is not an executable phase.

### What was measured, not assumed

- Re-measured this pass. `git rev-parse HEAD` on local `master` is `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`. `git status -sb` showed `## master...origin/master [ahead 2]` and a clean tree. `HEAD^` is `ef65f5d76462defdde53a9035b66be97e9e0e2ce`. `HEAD^^` is `95b385652a9201d3c298d2ff25b528b2e7e9021f`. `git log --oneline 95b385652a9201d3c298d2ff25b528b2e7e9021f..HEAD` lists those two commits and no others. An earlier pass recorded HEAD as `95b38565` with master level with its remote. That was before these two commits. It is not what HEAD is now.
- `git tag -l v0.20*` in this clone lists `v0.20.0` and `v0.20.1` only. `git cat-file -t 3a49f541be1a34c6187d903827da541a74fa37df` could not get object info. The tagged commit is not in the local object database. It was not fetched.
- `git ls-remote --tags origin` reports `refs/tags/v0.20.2` as annotated tag object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8`, peeling to `3a49f541be1a34c6187d903827da541a74fa37df`.
- The commit object for `3a49f541be1a34c6187d903827da541a74fa37df`, read again this pass, has message `chore(ci): append MADR 0143 flake ledger rows` and one parent, `95b385652a9201d3c298d2ff25b528b2e7e9021f`. That parent is `HEAD^^`, not `HEAD`. Neither commit on master after that parent is `3a49f541`. Master is not behind the tag by 1. The histories diverged at `95b38565`. Master does not contain `3a49f541`, so it contains no commit past that sha.
- The annotated tag message is `v0.20.2`. The tag object tagger date is `2026-10-04T04:19:59Z`. Re-read this pass.
- `internal/protocol/doc_coverage_test.go` lines 46-51 keep the first line whose trimmed text starts with `Event `type` values:` and then `break`. Line 60 is the `t.Errorf` for a type missing from that single line.
- `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` was re-run this pass and failed. Line 60 reported 25 types, from `question_resolved` through `codex_unsupported_item`, in `event.Types()` order. The nine names on guide line 1648 were not reported missing.
- `docs/guides/protocol-v1.md` lines 1648-1651 are the enumeration. Measured lengths are 196, 193, 187, and 83 characters. Line 1648 stops at `` `question_request`, ``. Lines 1649-1651 hold the other 25 names. Line 1651 ends with a period after `` `codex_unsupported_item` ``.
- Counting backtick-wrapped event names on those four lines, and not counting the words inside the prefix `Event `type` values:`, gives 34 names. `event.Types()` in `internal/event/event.go` returns 34 constants. The 25 failures are exactly the names that are not on line 1648, so the guide list and `Types()` are the same 34 names.
- `.markdownlint-cli2.jsonc` sets MD013 `line_length` to 200. Line 1648 is already 196 characters. Joining it to line 1649 exceeds 200.

### Findings

F1. The doc guard keeps only the first `Event `type` values:` line (`internal/protocol/doc_coverage_test.go` lines 46-51) and reports every other emitted type at line 60.

F2. A fresh `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` failed on the 25 names that sit on lines 1649-1651. The constants are present. The matcher stops too early.

F3. `docs/guides/protocol-v1.md` lines 1648-1651 already list all 34 event types. Line 1648 stops at `question_request`. The sentence ends with a period on line 1651.

F4. `event.Types()` returns those same 34 names. The failure does not justify adding or removing a constant.

F5. MD013 line length is 200. The wrapped lines are individually under that limit (196, 193, 187, 83). Joining line 1648 with line 1649 does not fit.

F6. Remote annotated `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. That commit's only parent is `95b385652a9201d3c298d2ff25b528b2e7e9021f`, which is `HEAD^^`, not current master. Current master is `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, two commits after that parent (`ef65f5d7`, then `1e1e9b86`). Neither sha is `3a49f541`. Master is not behind the tag by 1. The histories diverged. Master does not contain the tagged commit, so no commit on master is past it. The object is not in the local database.

F7. The retag that was asked for — recreate annotated `v0.20.2` on current master after the test fix, including commits already past `3a49f541` — does not match F6. Those commits are not on master. The test fix is not on master either. Pointing the published name at current master would drop `3a49f541` and still would not include commits past it. The retag as stated is not feasible. It stays deferred.

## Decision Drivers

- The guard must see types that are already documented. The guide should stay wrapped.
- Event constants are not the defect. The fix must not add or remove them.
- A published annotated tag is not a local bookmark. Moving it is destructive, and the measured graph does not contain the commits the retag was meant to include.
- This record is a proposal. Writing it does not accept the decision or edit the test.

## Considered Options

- A — Teach the test to keep the prefix line plus continuation lines through the sentence-ending period. Leave the guide wrapped. Defer any retag.
- B — Join the enumeration onto one line so the current first-line matcher passes.
- C — Recreate annotated `v0.20.2` on current master now, including commits past `3a49f541`, as an executable phase.

## Decision Outcome

Status of this record is proposed, not accepted. Option A is the proposed choice. The commit that adds this record does not change the test, the guide, or any tag.

### The decisions

D1. Change `TestEventTypesAreDocumented` so the text it searches is the prefix line plus the following lines through the sentence-ending period. Do not join or reflow `docs/guides/protocol-v1.md` lines 1648-1651.

D2. Do not add or remove event constants in `internal/event/event.go`. The 34 names already match.

D3. Do not delete, move, create, or push `v0.20.2` as an executable phase. F6 shows the requested retag is not feasible as stated, so it stays deferred. This docs commit does not retag.

### Consequences

- The test fix is a small change in `internal/protocol/doc_coverage_test.go`. It is not done in this commit.
- The guide's four-line enumeration stays the canonical list.
- `v0.20.2` keeps peeling to `3a49f541be1a34c6187d903827da541a74fa37df` on the remote. This proposal does not authorize shipping current master under that name.
- The assumption that master is strictly behind the tag by 1 is closed. The parent of the tagged commit is `HEAD^^`, not current master. The histories diverged.

### Confirmation

```text
go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/
# expect: PASS

# the 25 names that failed at line 60 are in the matched text
# guide lines 1648-1651 are still four lines, and line 1651 still ends with a period

git ls-remote --tags origin refs/tags/v0.20.2
# expect the peel to remain 3a49f541be1a34c6187d903827da541a74fa37df
# until a later explicit instruction resolves F6 and authorizes a retag
```

## Pros and Cons of the Options

### A — Match continuation lines through the period, and defer the retag (chosen)

- Good, because the 34 names are already in the guide. The defect is the `break` after the first line.
- Good, because lines 1648-1651 stay under MD013's 200-character limit.
- Good, because it does not pretend commits past `3a49f541` exist on master.
- Bad, because the published tag still points at a commit that is not in the local clone, so the name `v0.20.2` stays easy to misread until the deferred question is answered.

### B — Join the event-type sentence onto one line

- Good, because the current test would pass without a Go change.
- Bad, because line 1648 is 196 characters and MD013 allows 200. Joining line 1649 pushes the sentence past that limit.
- Bad, because the prefix constant is documented as a prefix match so the list can be reflowed, and joining throws away the reflow that already happened.

### C — Retag `v0.20.2` onto current master as an executable phase

- Good, because a tag that does not contain the test fix cannot name the release that fix is for.
- Bad, because master and the tag diverged at `95b38565`. Recreating the annotated tag on current master would move a published name off `3a49f541` onto a history that does not contain that commit, and would not include commits past that sha. There are none on master.
- Bad, because recreating an annotated tag on a published name is destructive. This proposal does not authorize that delete or move.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Local master HEAD is `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, ahead of its remote by 2, clean before this amendment | `git rev-parse HEAD`; `git status -sb` |
| `HEAD^` is `ef65f5d76462defdde53a9035b66be97e9e0e2ce`; `HEAD^^` is `95b385652a9201d3c298d2ff25b528b2e7e9021f`; those are the only two commits after `95b38565` | `git rev-parse HEAD^`; `git rev-parse HEAD^^`; `git log --oneline 95b385652a9201d3c298d2ff25b528b2e7e9021f..HEAD` |
| Local `v0.20*` tags are `v0.20.0` and `v0.20.1` only; `3a49f541be1a34c6187d903827da541a74fa37df` is not a local object | `git tag -l v0.20*`; `git cat-file -t` could not get object info |
| Remote `refs/tags/v0.20.2` is annotated object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` peeling to `3a49f541be1a34c6187d903827da541a74fa37df` | `git ls-remote --tags origin` |
| Tagged commit message is `chore(ci): append MADR 0143 flake ledger rows`; only parent is `95b385652a9201d3c298d2ff25b528b2e7e9021f` | commit API for `3a49f541be1a34c6187d903827da541a74fa37df`, re-read this pass |
| Master is not behind that commit by 1; the histories diverged, and master contains no commit past `3a49f541` | parent sha above plus `git log 95b385652a9201d3c298d2ff25b528b2e7e9021f..HEAD` |
| Tag message `v0.20.2`, tagger date `2026-10-04T04:19:59Z` | tag API for object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8`, re-read this pass |
| The test keeps the first prefix line and stops | `internal/protocol/doc_coverage_test.go` lines 46-51 |
| Failures are reported at line 60 | `internal/protocol/doc_coverage_test.go` line 60; `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` re-run this pass |
| 25 continuation names failed, starting at `question_resolved` and ending at `codex_unsupported_item` | that test run |
| Guide enumeration is lines 1648-1651, lengths 196, 193, 187, 83; line 1648 ends at `question_request`; line 1651 ends with a period | `docs/guides/protocol-v1.md` lines 1648-1651 |
| 34 names on the guide lines and 34 names from `event.Types()` | count of the wrapped list; `internal/event/event.go` `Types()` |
| MD013 line length is 200 | `.markdownlint-cli2.jsonc` key `MD013.line_length` |
| No ref other than `master` was checked for commits after `3a49f541` | **[unverified]** |

### Related records

- [0036-MADR-protocol-contract-completeness.md](0036-MADR-protocol-contract-completeness.md) — the test comment attributes this guard to MADR 0036 D6. This record does not restate that decision.
- [0182-PLAN-protocol-event-line-continuation.md](0182-PLAN-protocol-event-line-continuation.md) — execution plan for D1 and D2. D3 is deferred there.

### Open questions for the plan

- Whether `3a49f541be1a34c6187d903827da541a74fa37df` must be merged before any tag named `v0.20.2` may move. Not decided here. Current master does not contain it.
- Whether any ref other than `master` contains commits after that sha. Not checked. **[unverified]**
