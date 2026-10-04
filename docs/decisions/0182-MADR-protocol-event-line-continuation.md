---
status: accepted
date: 2026-10-04
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Match wrapped protocol event-type lines without retagging

## Context and Problem Statement

`TestEventTypesAreDocumented` treats the protocol event-type enumeration as one line. The guide already wraps that sentence across four lines so it stays within the markdownlint line-length limit. The test fails on the wrapped names even though every emitted type is in the guide. Separately, annotated `v0.20.2` can be recreated later on current `origin/master` after the test fix and green CI. That retag is not an executable phase. An earlier pass called it infeasible because the local `origin/master` ref was stale.

### What was measured, not assumed

- Re-measured this pass after `git fetch origin` only. No pull, rebase, merge, or push. `git rev-parse origin/master` is `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. The fetch moved that ref from `95b385652a9201d3c298d2ff25b528b2e7e9021f`. Before this correction commit, `git rev-parse HEAD` on local `master` is `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`, and `git status -sb` showed `## master...origin/master [ahead 3, behind 7]`. `git merge-base master origin/master` is `95b385652a9201d3c298d2ff25b528b2e7e9021f`.
- The previous local `origin/master` ref was stale at `95b385652a9201d3c298d2ff25b528b2e7e9021f`. That is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally. After the fetch, `git cat-file -t` on that sha reports `commit`. `git for-each-ref` on `refs/tags/v0.20.2` shows annotated tag object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` peeling to that commit. `git log -1` shows message `chore(ci): append MADR 0143 flake ledger rows` and parent `95b385652a9201d3c298d2ff25b528b2e7e9021f`. The tag message is `v0.20.2`. The tagger timestamp is `2026-10-04T04:19:59Z` (`git cat-file -p` on the tag object).
- `git rev-list --first-parent --count 3a49f541be1a34c6187d903827da541a74fa37df..origin/master` is 6. `git log --first-parent --reverse` for that range lists `a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. Fetched `origin/master` contains `3a49f541be1a34c6187d903827da541a74fa37df` and those six commits. The behind-7 count is that tagged commit plus the six.
- Local `master` is the other side of `95b385652a9201d3c298d2ff25b528b2e7e9021f`. `git log --first-parent` lists `ef65f5d76462defdde53a9035b66be97e9e0e2ce`, then `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, then `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`. Those three docs commits, plus this correction commit, still need a rebase onto fetched `origin/master` before any push. This commit does not rebase. A push of this `master` would not fast-forward.
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

F6. The previous local `origin/master` ref was stale at `95b385652a9201d3c298d2ff25b528b2e7e9021f`. That is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally. After `git fetch origin`, `origin/master` is `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. Its first-parent line contains `3a49f541be1a34c6187d903827da541a74fa37df` and the six commits after it: `a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. Annotated `v0.20.2` (`c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8`) peels to `3a49f541be1a34c6187d903827da541a74fa37df`. Local `master` is the other side of the stale sha: `ef65f5d76462defdde53a9035b66be97e9e0e2ce`, `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`.

F7. The retag Mac already chose is still possible later: test fix on current `origin/master`, green CI, then delete and recreate the annotated tag. It is not an executable phase in this plan. The three local docs commits (`ef65f5d76462defdde53a9035b66be97e9e0e2ce`, `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`), plus this correction commit, still need a rebase onto fetched `origin/master` before any push. This commit does not do that rebase. A push of this `master` would not fast-forward.

## Decision Drivers

- The guard must see types that are already documented. The guide should stay wrapped.
- Event constants are not the defect. The fix must not add or remove them.
- A published annotated tag is not a local bookmark. Moving it is destructive. Fetched `origin/master` contains the tagged commit and the six commits after it, and the retag is still not a phase of this plan.
- This record is a proposal. Writing it does not accept the decision or edit the test.

## Considered Options

- A — Teach the test to keep the prefix line plus continuation lines through the sentence-ending period. Leave the guide wrapped. Defer any retag.
- B — Join the enumeration onto one line so the current first-line matcher passes.
- C — Make the later retag an executable phase now: test fix on current `origin/master`, green CI, then delete and recreate the annotated tag.

## Decision Outcome

Status of this record is proposed, not accepted. Option A is the proposed choice. The commit that adds this record does not change the test, the guide, or any tag.

### The decisions

D1. Change `TestEventTypesAreDocumented` so the text it searches is the prefix line plus the following lines through the sentence-ending period. Do not join or reflow `docs/guides/protocol-v1.md` lines 1648-1651.

D2. Do not add or remove event constants in `internal/event/event.go`. The 34 names already match.

D3. Do not delete, move, create, or push `v0.20.2` as an executable phase. The later retag in F7 stays deferred. Do not rebase this `master` onto `origin/master` in this commit. `git push` is not permitted. A push of this `master` would not fast-forward.

### Consequences

- The test fix is a small change in `internal/protocol/doc_coverage_test.go`. It is not done in this commit.
- The guide's four-line enumeration stays the canonical list.
- `v0.20.2` keeps peeling to `3a49f541be1a34c6187d903827da541a74fa37df`. Fetched `origin/master` (`82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`) contains that commit and the six commits after it. This proposal does not delete or recreate the tag.
- Local `master` and fetched `origin/master` split at `95b385652a9201d3c298d2ff25b528b2e7e9021f`. This commit does not rebase, and a push of this `master` would not fast-forward.

### Confirmation

```text
go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/
# expect: PASS

# the 25 names that failed at line 60 are in the matched text
# guide lines 1648-1651 are still four lines, and line 1651 still ends with a period

git ls-remote --tags origin refs/tags/v0.20.2
# expect the peel to remain 3a49f541be1a34c6187d903827da541a74fa37df
# until a later explicit instruction authorizes the deferred retag in F7
```

## Pros and Cons of the Options

### A — Match continuation lines through the period, and defer the retag (chosen)

- Good, because the 34 names are already in the guide. The defect is the `break` after the first line.
- Good, because lines 1648-1651 stay under MD013's 200-character limit.
- Good, because fetched `origin/master` already contains `3a49f541be1a34c6187d903827da541a74fa37df` and the six commits after it, and this option still does not retag or rebase.
- Bad, because local `master` still does not contain that tagged commit, so the name `v0.20.2` stays easy to misread until the deferred rebase and retag happen in a later turn.

### B — Join the event-type sentence onto one line

- Good, because the current test would pass without a Go change.
- Bad, because line 1648 is 196 characters and MD013 allows 200. Joining line 1649 pushes the sentence past that limit.
- Bad, because the prefix constant is documented as a prefix match so the list can be reflowed, and joining throws away the reflow that already happened.

### C — Retag `v0.20.2` now as an executable phase

- Good, because fetched `origin/master` already contains `3a49f541be1a34c6187d903827da541a74fa37df` and the six commits after it, so a later retag can follow the test fix on that line once CI is green.
- Bad, because that retag is not an executable phase of this plan. D3 keeps it deferred. This proposal does not delete or recreate the tag.
- Bad, because the three local docs commits are not on `origin/master`. This commit does not rebase them, `git push` is not permitted, and a push of this `master` would not fast-forward.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Before this correction, local `master` HEAD is `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`; status showed ahead 3, behind 7 | `git rev-parse HEAD`; `git status -sb` |
| `git fetch origin` moved local `origin/master` from `95b385652a9201d3c298d2ff25b528b2e7e9021f` to `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`; the merge-base is the stale sha | `git fetch origin`; `git rev-parse origin/master`; `git merge-base master origin/master` |
| The stale `origin/master` is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally; after fetch the commit and the tag are local objects | `git fetch origin`; `git cat-file -t`; `git for-each-ref refs/tags/v0.20.2` |
| Annotated tag object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` peels to `3a49f541be1a34c6187d903827da541a74fa37df` | `git rev-parse` of the tag object and of the peeled commit; `git for-each-ref refs/tags/v0.20.2` |
| Tagged commit message is `chore(ci): append MADR 0143 flake ledger rows`; only parent is `95b385652a9201d3c298d2ff25b528b2e7e9021f` | `git log -1` on `3a49f541be1a34c6187d903827da541a74fa37df` |
| Fetched `origin/master` contains that commit and exactly six first-parent commits after it | `git log --first-parent`; `git rev-list --first-parent --count 3a49f541be1a34c6187d903827da541a74fa37df..origin/master` |
| Those six shas are `a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` | `git log --first-parent --reverse 3a49f541be1a34c6187d903827da541a74fa37df..origin/master` |
| Local side after the merge-base is `ef65f5d76462defdde53a9035b66be97e9e0e2ce`, `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, `3a59d7c23981ab5006f3ca2adb6efccb0117fa34` | `git log --first-parent 95b385652a9201d3c298d2ff25b528b2e7e9021f..master` |
| Tag message `v0.20.2`, tagger date `2026-10-04T04:19:59Z` | `git cat-file -p c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` |
| A push of this `master` would not fast-forward; this commit does not rebase | `git merge-base master origin/master` is not `origin/master`; no rebase was run |
| The test keeps the first prefix line and stops | `internal/protocol/doc_coverage_test.go` lines 46-51 |
| Failures are reported at line 60 | `internal/protocol/doc_coverage_test.go` line 60; `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` re-run this pass |
| 25 continuation names failed, starting at `question_resolved` and ending at `codex_unsupported_item` | that test run |
| Guide enumeration is lines 1648-1651, lengths 196, 193, 187, 83; line 1648 ends at `question_request`; line 1651 ends with a period | `docs/guides/protocol-v1.md` lines 1648-1651 |
| 34 names on the guide lines and 34 names from `event.Types()` | count of the wrapped list; `internal/event/event.go` `Types()` |
| MD013 line length is 200 | `.markdownlint-cli2.jsonc` key `MD013.line_length` |

### Related records

- [0036-MADR-protocol-contract-completeness.md](0036-MADR-protocol-contract-completeness.md) — the test comment attributes this guard to MADR 0036 D6. This record does not restate that decision.
- [0182-PLAN-protocol-event-line-continuation.md](0182-PLAN-protocol-event-line-continuation.md) — execution plan for D1 and D2. D3 is deferred there.

### Open questions for the plan

- Whether to rebase the local docs commits onto fetched `origin/master` before any push. Not done here. A push of this `master` would not fast-forward.
- Whether to run the deferred retag after the test fix is on current `origin/master` and CI is green. Not a phase of this plan.

## Amendment — 2026-10-04: v0.20.2 remains; a later release is a new tag

This supersedes the later step in F7 and D3 that said to delete and recreate annotated tag `v0.20.2`. F7 and D3 are left as written so the proposal stays visible. They are not the next action.

- `v0.20.2` remains. It keeps peeling to `3a49f541be1a34c6187d903827da541a74fa37df`.
- Do not delete, recreate, or move that tag.
- Any later release is a new tag, created and pushed only under a later explicit instruction. This amendment does not create a tag and does not authorize a push.
- The open question about running the deferred retag is closed by this amendment. The answer is no. There is no deferred delete-and-recreate.

## Observed — execution results (2026-10-04)

No new test commit was made. The matcher change is the existing `origin/master` commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`, subject `test(protocol): support reflowed event type documentation`. This commit only marks the decision accepted and records that fact. `docs/guides/protocol-v1.md` and `internal/event/event.go` were not edited. Nothing was pushed. `v0.20.2` was not moved.

What the proposal had wrong, recorded rather than rewritten:

- F7's later step was delete-and-recreate of `v0.20.2`. The owner superseded that before this commit. The tag stays. A later release is a new tag. See the amendment above.
- D1 describes a new edit that stops at the sentence-ending period. That edit was not made here. `27956ea5f23fa00353801153d1b9cf128cc5d58e` already changed the guard, and it stops at the next blank line. On the current guide the blank line follows the period.
- F6 measured fetched `origin/master` as `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. A later fetch, before the rebase that preceded this commit, moved `origin/master` to `27956ea5f23fa00353801153d1b9cf128cc5d58e`. That object still contains `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` and `3a49f541be1a34c6187d903827da541a74fa37df`.
