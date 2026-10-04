---
status: completed
date: 2026-10-04
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0182 — Match wrapped protocol event-type lines

Implements [0182-MADR-protocol-event-line-continuation.md](0182-MADR-protocol-event-line-continuation.md) decisions D1–D3, closing findings F1–F7. The matcher is already on `origin/master`. This plan is completed. It does not create a tag.

## Goal

`27956ea5f23fa00353801153d1b9cf128cc5d58e` is contained in `origin/master` and keeps continuation lines of the event-type sentence. `docs/guides/protocol-v1.md` lines 1648-1651 stay the four-line enumeration ending in a period. `event.Types()` stays 34 names. `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. This plan does not create a tag.

## Scope

### In scope (the only files any phase may touch)

- `internal/protocol/doc_coverage_test.go` — already changed in `27956ea5f23fa00353801153d1b9cf128cc5d58e`. This completion commit does not touch it.
- `docs/decisions/0182-MADR-protocol-event-line-continuation.md` and `docs/decisions/0182-PLAN-protocol-event-line-continuation.md` — this completion commit only.

### Out of scope

- CI workflows, Makefile gates, and any other workflow file.
- `internal/event/event.go` and every Go file except the matcher file already committed in `27956ea5f23fa00353801153d1b9cf128cc5d58e`. No event constant is added or removed. This commit does not add a test edit.
- `docs/guides/protocol-v1.md`. Do not join lines 1648-1651.
- Deleting, moving, or creating any git tag. `v0.20.2` stays.
- Any `git push`. This commit does not push.

## Stability rule

This plan is completed. P1 is the existing origin commit, not a new edit. This docs commit does not re-run `gofmt` or `go test`. A fresh test result in this commit is **[unverified]**.

This docs commit ends with `git status` showing only the two decision files above, and with `v0.20.2` still peeling to `3a49f541be1a34c6187d903827da541a74fa37df`.

Commit discipline for this docs commit: one commit. Do not pass `-m`, `-M`, `--message`, or `-F`. Run `git commit --no-edit`. Do not skip hooks. Do not amend. Do not reset.

`git push` is not permitted. No tag ref is created, deleted, or moved. This commit does not create a tag. P1 did not create a tag. No phase creates a tag.

## Cross-cutting contracts

C1. No event constant is added or removed. `event.Types()` stays 34 names.

C2. `docs/guides/protocol-v1.md` lines 1648-1651 are not joined, reflowed, or edited. MD013 line length is 200 and line 1648 is already 196 characters.

C3. No CI file is edited.

C4. `git push` is not permitted in any phase.

C5. No tag ref is created, deleted, or moved. C5 is the contract most likely to break under pressure: a matcher already on `origin/master` makes a new release name feel like the next step. It is not. `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. Any later release is a new tag, not a phase of this plan, and this commit does not create one.

## Dependency and delivery order

P1 is done. It did not depend on a tag change. This docs commit is not a new test edit and does not create a tag.

## Implementation Steps

### P1 — Continuation-line matcher already on origin (D1, D2; closes F1, F2, F3, F4, F5) — done

Done. The matcher is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`, subject `test(protocol): support reflowed event type documentation`. `git merge-base --is-ancestor` succeeds because that commit is `origin/master`. It keeps continuation lines of the event-type sentence: from the prefix `Event `type` values:` through the following lines until the next blank line. On the current guide that blank line follows the period, so the matched text runs through the period. It does not join the guide and does not add or remove event constants. This plan does not edit that commit and does not repeat the test change.

`v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. This phase does not delete, recreate, or move it, and it does not create a tag.

**Verification**

```text
git show --stat 27956ea5f23fa00353801153d1b9cf128cc5d58e
git merge-base --is-ancestor 27956ea5f23fa00353801153d1b9cf128cc5d58e origin/master
```

Expect a diff confined to `internal/protocol/doc_coverage_test.go` and a successful ancestor check. The diff scans until the next blank line. A fresh `go test` was not run for this docs commit. That result is **[unverified]**.

## Verification (whole plan)

```text
git merge-base --is-ancestor 27956ea5f23fa00353801153d1b9cf128cc5d58e origin/master
git rev-parse "v0.20.2^{}"
git diff -- internal/event/event.go docs/guides/protocol-v1.md
```

The ancestor check succeeds. The peel is `3a49f541be1a34c6187d903827da541a74fa37df`. The diff of this docs commit does not include those two paths. No tag command that changes a ref has been run.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Continuation lines are covered by `27956ea5f23fa00353801153d1b9cf128cc5d58e`, which is `origin/master`; this plan does not add a test edit | Confirmation ancestor check; closes F1, F2 |
| A2 | Guide lines 1648-1651 are unchanged and line 1651 still ends with a period | D1; F3; F5 |
| A3 | `event.Types()` is unchanged at 34 names | D2; F4 |
| A4 | `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df` | D3; F6; F7; Confirmation peel check |

A4 is the criterion most likely to be dropped. A matcher already on `origin/master` looks like permission to create a release tag or to move `v0.20.2`. It is not. The tag stays. Any later release is a new tag, not a phase of this plan, and this commit does not create one.

## Rollout and Rollback

P1 already landed as `27956ea5f23fa00353801153d1b9cf128cc5d58e` on `origin/master`. This docs commit does not push and does not create a tag. This plan does not roll back that matcher commit. Rollback of this docs commit is `git revert` of this docs commit only, not a tag operation and not a reset.

## Deferred (named, so they are not mistaken for oversights)

None. `v0.20.2` stays and still peels to `3a49f541be1a34c6187d903827da541a74fa37df`. Do not delete, recreate, or move it. Any later release is a new tag, not a phase of this plan, and this commit does not create one. There is no leftover tag step.

## Execution record (2026-10-04)

The footnote style was rejected. Live sections were rewritten in place. The earlier delete-and-recreate wording was removed from Deferred, from P1, and from the matched MADR's F7, D3, and option C. That wording is not the decision and is not remaining work.

P1 is existing origin commit `27956ea5f23fa00353801153d1b9cf128cc5d58e`. This docs commit does not edit the test, `internal/event/event.go`, or `docs/guides/protocol-v1.md`. PLAN status is `completed`. MADR status is `accepted`. Nothing in this commit was pushed. No tag ref was created, deleted, or moved. `v0.20.2` still peels to `3a49f541be1a34c6187d903827da541a74fa37df`.
