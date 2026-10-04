---
status: accepted
date: 2026-10-04
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Match wrapped protocol event-type lines and leave v0.20.2 in place

## Context and Problem Statement

`TestEventTypesAreDocumented` used to treat the protocol event-type enumeration as one line. The guide already wraps that sentence across four lines so it stays within the markdownlint line-length limit. The matcher that keeps the continuation lines is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`. Annotated `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. This commit does not delete, recreate, or move that tag. Any later release is a new tag, not a phase of this plan, and this commit does not create one.

### What was measured, not assumed

**Earlier pass.** These bullets are the measurements taken when local `origin/master` had just been fetched to `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. They are not the graph at this rewrite. They stay so that evidence is not rewritten into hindsight.

- Re-measured that pass after `git fetch origin` only. No pull, rebase, merge, or push in that commit. `git rev-parse origin/master` was `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. The fetch moved that ref from `95b385652a9201d3c298d2ff25b528b2e7e9021f`. Before that correction commit, `git rev-parse HEAD` on local `master` was `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`, and `git status -sb` showed `## master...origin/master [ahead 3, behind 7]`. `git merge-base master origin/master` was `95b385652a9201d3c298d2ff25b528b2e7e9021f`.
- The previous local `origin/master` ref was stale at `95b385652a9201d3c298d2ff25b528b2e7e9021f`. That is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally. After that fetch, `git cat-file -t` on that sha reported `commit`. `git for-each-ref` on `refs/tags/v0.20.2` showed annotated tag object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` peeling to that commit. `git log -1` showed message `chore(ci): append MADR 0143 flake ledger rows` and parent `95b385652a9201d3c298d2ff25b528b2e7e9021f`. The tag message is `v0.20.2`. The tagger timestamp is `2026-10-04T04:19:59Z` (`git cat-file -p` on the tag object).
- `git rev-list --first-parent --count 3a49f541be1a34c6187d903827da541a74fa37df..origin/master` was 6 at that fetch. `git log --first-parent --reverse` for that range listed `a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. Fetched `origin/master` contained `3a49f541be1a34c6187d903827da541a74fa37df` and those six commits. The behind-7 count was that tagged commit plus the six.
- At that measurement, local `master` was the other side of `95b385652a9201d3c298d2ff25b528b2e7e9021f`. `git log --first-parent` listed `ef65f5d76462defdde53a9035b66be97e9e0e2ce`, then `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, then `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`. A push at that moment would not have fast-forwarded. A later rebase, already done before this rewrite, put local `master` ahead of `origin/master`. This rewrite does not rebase and does not push.
- Before `27956ea5f23fa00353801153d1b9cf128cc5d58e`, `internal/protocol/doc_coverage_test.go` kept the first line whose trimmed text starts with `Event `type` values:` and then `break`. The `t.Errorf` reported a type missing from that single line.
- `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` was run on that earlier tree and failed. The error reported 25 types, from `question_resolved` through `codex_unsupported_item`, in `event.Types()` order. The nine names on guide line 1648 were not reported missing. This rewrite does not re-run that test. A fresh pass or fail on the current tree is **[unverified]**.
- `docs/guides/protocol-v1.md` lines 1648-1651 are the enumeration, measured on that earlier pass. Measured lengths were 196, 193, 187, and 83 characters. Line 1648 stops at `` `question_request`, ``. Lines 1649-1651 hold the other 25 names. Line 1651 ends with a period after `` `codex_unsupported_item` ``. This rewrite does not edit the guide.
- Counting backtick-wrapped event names on those four lines, and not counting the words inside the prefix `Event `type` values:`, gives 34 names. `event.Types()` in `internal/event/event.go` returns 34 constants. The 25 failures were exactly the names that were not on line 1648, so the guide list and `Types()` are the same 34 names.
- `.markdownlint-cli2.jsonc` sets MD013 `line_length` to 200. Line 1648 is already 196 characters. Joining it to line 1649 exceeds 200.

**This pass, before the rewrite commit.** `HEAD` was `b153fa09f5babcd10f9ee909dc5834094052c89e` on `master`.

- `git rev-parse origin/master` is `27956ea5f23fa00353801153d1b9cf128cc5d58e`. `git merge-base --is-ancestor 27956ea5f23fa00353801153d1b9cf128cc5d58e origin/master` succeeds because that commit is `origin/master`. `git status -sb` showed `## master...origin/master [ahead 5]`.
- `git log -1` on `27956ea5f23fa00353801153d1b9cf128cc5d58e` shows subject `test(protocol): support reflowed event type documentation`, author date `2026-10-04 12:31:13 -0500`. `git show --stat` changes only `internal/protocol/doc_coverage_test.go` (15 insertions, 7 deletions). The diff keeps lines from the prefix `Event `type` values:` until the next blank line, joins those lines, and searches that text. This rewrite does not change that commit.
- `git rev-parse v0.20.2^{}` is `3a49f541be1a34c6187d903827da541a74fa37df`. No tag ref was created, deleted, or moved in this pass.

### Findings

F1. Before `27956ea5f23fa00353801153d1b9cf128cc5d58e`, the doc guard kept only the first `Event `type` values:` line and reported every other emitted type. That commit replaces the early stop with a scan through the following lines until the next blank line. This rewrite does not edit the test.

F2. On the earlier tree, `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` failed on the 25 names that sit on lines 1649-1651. The constants were present. The matcher stopped too early. The change that keeps those continuation lines is `27956ea5f23fa00353801153d1b9cf128cc5d58e`, not a new edit in this commit.

F3. `docs/guides/protocol-v1.md` lines 1648-1651 already list all 34 event types. Line 1648 stops at `question_request`. The sentence ends with a period on line 1651.

F4. `event.Types()` returns those same 34 names. The failure does not justify adding or removing a constant.

F5. MD013 line length is 200. The wrapped lines are individually under that limit (196, 193, 187, 83). Joining line 1648 with line 1649 does not fit.

F6. The earlier local `origin/master` ref was stale at `95b385652a9201d3c298d2ff25b528b2e7e9021f`. That is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally. After the earlier `git fetch origin`, `origin/master` was `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` and its first-parent line contained `3a49f541be1a34c6187d903827da541a74fa37df` plus `a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, and `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`. Annotated `v0.20.2` (`c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8`) peels to `3a49f541be1a34c6187d903827da541a74fa37df`. At this rewrite, `origin/master` is `27956ea5f23fa00353801153d1b9cf128cc5d58e`, which still contains `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` and `3a49f541be1a34c6187d903827da541a74fa37df`. Local `master` is ahead of that ref, not diverged from it.

F7. `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. Do not delete, recreate, or move it. Any later release is a new tag, not a phase of this plan, and this commit does not create one.

## Decision Drivers

- The guard must see types that are already documented. The guide should stay wrapped.
- Event constants are not the defect. The fix must not add or remove them.
- The matcher change already exists on `origin/master` as `27956ea5f23fa00353801153d1b9cf128cc5d58e`. This commit does not edit the test.
- A published annotated tag is not a local bookmark. `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. This commit does not delete, recreate, or move it. Any later release is a new tag, not a phase of this plan, and this commit does not create one.

## Considered Options

- A — Keep the existing continuation-line matcher in `27956ea5f23fa00353801153d1b9cf128cc5d58e`. Leave the guide wrapped. Leave `v0.20.2` in place.
- B — Join the enumeration onto one line so a first-line matcher passes.
- C — Make a new release tag a phase of this plan.

## Decision Outcome

Status of this record is accepted. Option A is the choice. The matcher change is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`, not a new test edit. This commit does not create a tag.

### The decisions

D1. Match continuation lines through the period. Do not join the guide. The matcher that does this is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`, not a new test edit in this commit. That commit keeps the prefix line `Event `type` values:` and each following line until the next blank line. On the current guide the blank line follows the period after `` `codex_unsupported_item` `` on `docs/guides/protocol-v1.md` line 1651, so the searched text includes the continuation lines through the period. Do not join or reflow lines 1648-1651. This commit does not change `27956ea5f23fa00353801153d1b9cf128cc5d58e`.

D2. Do not add or remove event constants in `internal/event/event.go`. The 34 names already match. This commit does not edit that file and does not add a test edit.

D3. `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. Do not delete, recreate, or move it. Any later release is a new tag, not a phase of this plan, and this commit does not create one.

### Consequences

- The test change is the existing commit `27956ea5f23fa00353801153d1b9cf128cc5d58e` on `origin/master`. This commit does not edit `internal/protocol/doc_coverage_test.go`.
- The guide's four-line enumeration stays the canonical list. This commit does not edit `docs/guides/protocol-v1.md`.
- `v0.20.2` keeps peeling to `3a49f541be1a34c6187d903827da541a74fa37df`. This commit does not delete, recreate, or move that tag, and it does not create a tag.
- This commit does not push.

### Confirmation

```text
git merge-base --is-ancestor 27956ea5f23fa00353801153d1b9cf128cc5d58e origin/master
# expect success: that commit is origin/master
# its diff keeps continuation lines of the event-type sentence until the next blank line

git rev-parse "v0.20.2^{}"
# expect 3a49f541be1a34c6187d903827da541a74fa37df

# guide lines 1648-1651 stay four lines, and line 1651 still ends with a period
# this commit does not edit the test, the guide, or internal/event/event.go
# this commit does not create a tag
# a fresh go test run in this docs commit was not performed: [unverified]
```

## Pros and Cons of the Options

### A — Keep the existing continuation-line matcher and leave v0.20.2 in place (chosen)

- Good, because the 34 names are already in the guide. `27956ea5f23fa00353801153d1b9cf128cc5d58e` already keeps the continuation lines of that sentence.
- Good, because lines 1648-1651 stay under MD013's 200-character limit.
- Good, because `v0.20.2` stays on `3a49f541be1a34c6187d903827da541a74fa37df` and this commit does not create a tag.
- Bad, because the existing matcher stops at the next blank line, not by searching for the period character. On the current guide those are the same span. A later blank line inside the sentence would stop the scan early. This commit does not change that matcher.

### B — Join the event-type sentence onto one line

- Good, because a first-line matcher would pass without the continuation-line scan.
- Bad, because line 1648 is 196 characters and MD013 allows 200. Joining line 1649 pushes the sentence past that limit.
- Bad, because the prefix constant is documented as a prefix match so the list can be reflowed, and joining throws away the reflow that already happened.

### C — Make a new release tag a phase of this plan

- Good, because a published name on the matcher commit would tell a client which release contains the continuation-line guard.
- Bad, because `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. This commit does not delete, recreate, or move it.
- Bad, because any later release is a new tag, not a phase of this plan, and this commit does not create one.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Earlier pass: local `master` HEAD was `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`; status showed ahead 3, behind 7 | `git rev-parse HEAD`; `git status -sb` on that pass |
| That `git fetch origin` moved local `origin/master` from `95b385652a9201d3c298d2ff25b528b2e7e9021f` to `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`; the merge-base was the stale sha | `git fetch origin`; `git rev-parse origin/master`; `git merge-base master origin/master` |
| The stale `origin/master` is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally; after that fetch the commit and the tag were local objects | `git fetch origin`; `git cat-file -t`; `git for-each-ref refs/tags/v0.20.2` |
| Annotated tag object `c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` peels to `3a49f541be1a34c6187d903827da541a74fa37df` | `git rev-parse` of the tag object and of the peeled commit; `git for-each-ref refs/tags/v0.20.2`; this pass `git rev-parse v0.20.2^{}` |
| Tagged commit message is `chore(ci): append MADR 0143 flake ledger rows`; only parent is `95b385652a9201d3c298d2ff25b528b2e7e9021f` | `git log -1` on `3a49f541be1a34c6187d903827da541a74fa37df` |
| At the earlier fetch, `origin/master` contained that commit and exactly six first-parent commits after it | `git log --first-parent`; `git rev-list --first-parent --count 3a49f541be1a34c6187d903827da541a74fa37df..origin/master` |
| Those six shas are `a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` | `git log --first-parent --reverse 3a49f541be1a34c6187d903827da541a74fa37df..origin/master` on that pass |
| This pass: `origin/master` is `27956ea5f23fa00353801153d1b9cf128cc5d58e`; local `master` was `b153fa09f5babcd10f9ee909dc5834094052c89e`, ahead 5; that matcher commit is contained in `origin/master` | `git rev-parse origin/master`; `git rev-parse HEAD`; `git status -sb`; `git merge-base --is-ancestor` |
| Matcher commit subject `test(protocol): support reflowed event type documentation`; diff is only `internal/protocol/doc_coverage_test.go`; scan stops at the next blank line | `git show --stat` and the diff of `27956ea5f23fa00353801153d1b9cf128cc5d58e` |
| Tag message `v0.20.2`, tagger date `2026-10-04T04:19:59Z` | `git cat-file -p c2135c90e9746bc02c3b0cadcd9a2138cf6be6a8` |
| This rewrite does not push and does not create a tag | no push was run; no tag-mutating command was run |
| The pre-matcher test kept the first prefix line and stopped | diff of `27956ea5f23fa00353801153d1b9cf128cc5d58e` against its parent |
| Earlier failures were the 25 continuation names, starting at `question_resolved` and ending at `codex_unsupported_item` | `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` on that earlier tree; not re-run in this rewrite |
| Guide enumeration is lines 1648-1651, lengths 196, 193, 187, 83; line 1648 ends at `question_request`; line 1651 ends with a period | `docs/guides/protocol-v1.md` lines 1648-1651, earlier pass |
| 34 names on the guide lines and 34 names from `event.Types()` | count of the wrapped list; `internal/event/event.go` `Types()` |
| MD013 line length is 200 | `.markdownlint-cli2.jsonc` key `MD013.line_length` |

### Related records

- [0036-MADR-protocol-contract-completeness.md](0036-MADR-protocol-contract-completeness.md) — the test comment attributes this guard to MADR 0036 D6. This record does not restate that decision. The matcher comment in `27956ea5f23fa00353801153d1b9cf128cc5d58e` also cites MADR 0180 D8. This record does not restate that decision either.
- [0182-PLAN-protocol-event-line-continuation.md](0182-PLAN-protocol-event-line-continuation.md) — completed plan. D1 and D2 are the existing matcher. D3 leaves `v0.20.2` in place.

### Open questions for the plan

- Whether this commit should edit the test. Closed. The matcher change is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`.
- Whether this plan should create a release tag. Closed. Any later release is a new tag, not a phase of this plan, and this commit does not create one. `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`.

## Observed — execution results (2026-10-04)

The footnote style was rejected. Live sections were rewritten in place. The earlier delete-and-recreate wording was removed from F7, D3, and option C. That wording is not the decision and is not remaining work.

`v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. This commit does not delete, recreate, or move it, and it does not create a tag. The matcher change is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`, subject `test(protocol): support reflowed event type documentation`. That commit keeps continuation lines of the event-type sentence until the next blank line. This commit does not edit the test, `docs/guides/protocol-v1.md`, or `internal/event/event.go`. Nothing was pushed.
