---
status: proposed
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

`git push` is not permitted. Tags must not be deleted, moved, or pushed. This docs commit does not retag. P1 does not retag. No phase creates a tag.

## Cross-cutting contracts

C1. No event constant is added or removed. `event.Types()` stays 34 names.

C2. `docs/guides/protocol-v1.md` lines 1648-1651 are not joined, reflowed, or edited. MD013 line length is 200 and line 1648 is already 196 characters.

C3. No CI file is edited.

C4. `git push` is not permitted in any phase.

C5. No tag ref is created, deleted, moved, or pushed. C5 is the contract most likely to break under pressure: a green test makes "just retag v0.20.2" feel like the next step. F6 says that move is not the graph that was measured, and D3 forbids it.

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

A4 is the criterion most likely to be dropped. A passing test looks like permission to publish the same tag name on the new commit. It is not. F6 is unchanged by P1.

## Rollout and Rollback

Rollout of P1 is the test commit on `master`. It does not push and it does not retag. Rollback is `git revert` of that commit, not a tag operation and not a reset. The commit that adds this pair rolls back by reverting that docs commit only.

## Deferred (named, so they are not mistaken for oversights)

Recreate annotated tag `v0.20.2` on current master after the test fix, including commits already past `3a49f541`. That is what was asked. It is deferred, not scheduled as a phase. Re-measured this pass: local master HEAD is `1e1e9b86ac4dceb2db4a2ef89b65e6a4673ac60e` (`HEAD^` `ef65f5d76462defdde53a9035b66be97e9e0e2ce`, `HEAD^^` `95b385652a9201d3c298d2ff25b528b2e7e9021f`); remote annotated `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df`; that commit's only parent is `95b385652a9201d3c298d2ff25b528b2e7e9021f`, not HEAD; the two commits on master after that parent are not the tagged commit; master is not behind the tag by 1; the histories diverged; there is no commit on master past `3a49f541`; the object is not in the local database and was not fetched (F6, F7). The retag as stated is not feasible. Recreating an annotated tag on a published name is destructive. This plan does not delete, move, create, or push the existing tag, and it does not contain a phase that does so. A later explicit instruction, in the turn that does the work, is required before any tag ref changes.
