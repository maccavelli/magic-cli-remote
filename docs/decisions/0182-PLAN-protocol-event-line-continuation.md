---
status: completed
date: 2026-10-04
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0182 — Match wrapped protocol event-type lines

Implements [0182-MADR-protocol-event-line-continuation.md](0182-MADR-protocol-event-line-continuation.md) decisions D1–D2, closing findings F1–F5. D3 and findings F6–F7 are not an executable phase; they are named under Deferred.

## Goal

`go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` passes. `docs/guides/protocol-v1.md` lines 1648-1651 are still the four-line enumeration ending in a period. `event.Types()` is still 34 names. Remote `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df`.

## Scope

### In scope (the only files any phase may touch)

- `internal/protocol/doc_coverage_test.go` — the matcher only.
- `docs/decisions/0182-MADR-protocol-event-line-continuation.md` and `docs/decisions/0182-PLAN-protocol-event-line-continuation.md` — status and an execution record after P1 is approved and run. No other edits to those files during P1.

### Out of scope

- CI workflows, Makefile gates, and any other workflow file.
- `internal/event/event.go` and every Go file except the test file above. No event constant is added or removed.
- `docs/guides/protocol-v1.md`. Do not join lines 1648-1651.
- Doing the test edit in the commit that adds this pair. That commit is docs only.
- Deleting, moving, creating, or pushing any git tag. See Deferred.
- Rebasing local `master` onto `origin/master`, and any `git push`. This commit does not rebase. A push of this `master` would not fast-forward.

## Stability rule

P1 ends with:

```text
gofmt -l internal/protocol/doc_coverage_test.go
go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/
git status -sb
git ls-remote --tags origin refs/tags/v0.20.2
```

`gofmt -l` prints nothing. The test passes. Status shows only the in-scope files. The remote peel is still `3a49f541be1a34c6187d903827da541a74fa37df`.

Commit discipline for P1: one commit, execution-record edits to this pair included only when P1 actually ran. Do not pass `-m`, `-M`, `--message`, or `-F`. Run `git commit --no-edit`. Do not skip hooks. Do not amend. Do not reset.

`git push` is not permitted, and this commit does not rebase. Local `master` and fetched `origin/master` diverge at `95b385652a9201d3c298d2ff25b528b2e7e9021f`, so a push of this `master` would not fast-forward. Tags must not be deleted, moved, or pushed. This docs commit does not retag. P1 does not retag. No phase creates a tag.

## Cross-cutting contracts

C1. No event constant is added or removed. `event.Types()` stays 34 names.

C2. `docs/guides/protocol-v1.md` lines 1648-1651 are not joined, reflowed, or edited. MD013 line length is 200 and line 1648 is already 196 characters.

C3. No CI file is edited.

C4. `git push` is not permitted in any phase.

C5. No tag ref is created, deleted, moved, or pushed. C5 is the contract most likely to break under pressure: a green test makes "just retag v0.20.2" feel like the next step. F6 shows fetched `origin/master` already contains the tagged commit and six commits after it, so the later retag is possible, and D3 still forbids doing it in this plan.

## Dependency and delivery order

P1 is the only executable phase. It does not depend on a tag change. The docs commit that introduces this pair is not P1 and must not contain the test edit.

## Implementation Steps

### P1 — Keep continuation lines through the period (D1, D2; closes F1, F2, F3, F4, F5)

Do this only after an explicit approval to execute P1. This plan's own commit does not do it.

1. In `TestEventTypesAreDocumented`, after the line that starts with `Event `type` values:`, keep that line and each following line until the line that closes the sentence with a period. That period is the one after `` `codex_unsupported_item` `` on `docs/guides/protocol-v1.md` line 1651. Search the combined text, not the first line alone.
2. Do not edit the guide. Do not edit `internal/event/event.go`.
3. Do not fetch, delete, move, or create `v0.20.2`.

**Verification**

```text
gofmt -l internal/protocol/doc_coverage_test.go
go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/
git diff -- internal/event/event.go docs/guides/protocol-v1.md
```

Expect no gofmt output, PASS, and an empty diff. The 25 names that failed at line 60, including `question_resolved` and `codex_unsupported_item`, are no longer reported missing.

## Verification (whole plan)

Run the P1 verification, then:

```text
git ls-remote --tags origin refs/tags/v0.20.2
git status -sb
```

The peel is still `3a49f541be1a34c6187d903827da541a74fa37df`. No tag command that changes a ref has been run.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `TestEventTypesAreDocumented` passes and the 25 continuation names are covered by the matched text | Confirmation `go test` block; closes F1, F2 |
| A2 | Guide lines 1648-1651 are unchanged and line 1651 still ends with a period | D1; F3; F5 |
| A3 | `event.Types()` is unchanged at 34 names | D2; F4 |
| A4 | Remote `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df` | D3; F6; Confirmation `git ls-remote` block |

A4 is the criterion most likely to be dropped. A passing test looks like permission to delete and recreate `v0.20.2`. It is not. The retag stays deferred (D3, F7). P1 does not change F6.

## Rollout and Rollback

Rollout of P1 is the test commit on `master`. It does not push and it does not retag. Rollback is `git revert` of that commit, not a tag operation and not a reset. The commit that adds this pair rolls back by reverting that docs commit only.

## Deferred (named, so they are not mistaken for oversights)

**Not the next action (2026-10-04).** Do not delete, recreate, or move annotated tag `v0.20.2`. That tag remains and keeps peeling to `3a49f541be1a34c6187d903827da541a74fa37df`. Any later release is a new tag, not a delete or move of `v0.20.2`. The paragraph below is the earlier deferred step. It is superseded by the amendment at the end of this plan.

Recreate annotated tag `v0.20.2` later: land the test fix on current `origin/master` (`82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`), wait for green CI, then delete and recreate the annotated tag. That is the retag already chosen. It is deferred, not an executable phase. This plan does not add a phase that retags. Re-measured this pass after `git fetch origin`: the previous local `origin/master` ref was stale at `95b385652a9201d3c298d2ff25b528b2e7e9021f`, which is why `3a49f541be1a34c6187d903827da541a74fa37df` was missing locally. Fetched `origin/master` contains that commit and the six first-parent commits after it (`a9843f592a74f6070eabb808a18c3c1286645059`, `bc3431dfb87f131e4b624b0960f2b915af5d5af3`, `b5ed5a3062de005b7c3fff6f4a5130a5856c6173`, `ab8122fa79f4b66ad41146af36b99f2e1aefe1d1`, `36772a6f2f1c4e2f9224f5e7715ec89d721b0d6f`, `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2`). `git rev-list --first-parent --count` for that range is 6 (F6, F7). Local `master` is the other side of `95b385652a9201d3c298d2ff25b528b2e7e9021f`: `ef65f5d76462defdde53a9035b66be97e9e0e2ce`, `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e`, `3a59d7c23981ab5006f3ca2adb6efccb0117fa34`, plus this correction commit. Those commits still need a rebase onto fetched `origin/master` before any push. This commit does not rebase. `git push` is not permitted. A push of this `master` would not fast-forward. Recreating an annotated tag on a published name is destructive. This plan does not delete, move, create, or push the existing tag. A later explicit instruction, in the turn that does the work, is required before any tag ref changes and before any rebase.

## Amendment — 2026-10-04: v0.20.2 remains; a later release is a new tag

This supersedes the delete-and-recreate step in the Deferred paragraph above, and the same step as stated in [0182-MADR-protocol-event-line-continuation.md](0182-MADR-protocol-event-line-continuation.md) F7 and D3. Those passages are left as written. They are not the next action.

- Annotated tag `v0.20.2` stays. It keeps peeling to `3a49f541be1a34c6187d903827da541a74fa37df`.
- This plan does not delete, recreate, move, or push that tag. No phase creates a tag.
- Any later release is a new tag. Creating or pushing it needs an explicit instruction in the turn that does the work. This amendment is not that instruction.
- C5 and A4 still forbid retagging inside this plan. Their mention of a later retag of `v0.20.2` is also superseded. There is no deferred delete-and-recreate.

## Execution record (2026-10-04)

This commit is docs only. It does not add a test commit.

P1's matcher is the existing `origin/master` commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`, subject `test(protocol): support reflowed event type documentation`. That commit is already on the rebased base. This commit does not edit `internal/protocol/doc_coverage_test.go`, `internal/event/event.go`, or `docs/guides/protocol-v1.md`.

The matched MADR is `accepted`. This plan is `completed` because that existing commit is the test change and the only executable phase is not being repeated. PLAN status uses `completed`, not `accepted`.

Nothing in this commit was pushed. No tag ref was created, deleted, or moved.

What this plan had wrong, recorded here instead of rewritten into the earlier sections:

- Deferred, and MADR F7 and D3, named a later delete-and-recreate of `v0.20.2` after a new test fix landed on `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` and CI was green. Before this commit the owner superseded that step. `v0.20.2` remains. A later release is a new tag.
- The plan describes P1 as a new edit to `TestEventTypesAreDocumented` in a commit that also carries this execution record. That new test commit was not made. The test change is `27956ea5f23fa00353801153d1b9cf128cc5d58e`.
- P1's written stop was the sentence-ending period. `27956ea5f23fa00353801153d1b9cf128cc5d58e` stops at the next blank line. On the current guide that blank line is the line after the period, so the searched text is the wrapped enumeration. This commit does not change that matcher.
- The stability rule said this work does not rebase. Local `master` was rebased onto fetched `origin/master` before this docs commit, by instruction. After that fetch, `origin/master` was `27956ea5f23fa00353801153d1b9cf128cc5d58e`, which still contains `82fb0356d6b5f3b03894631f3d8c2b57292e9bd2` and `3a49f541be1a34c6187d903827da541a74fa37df`. This commit still does not push.
