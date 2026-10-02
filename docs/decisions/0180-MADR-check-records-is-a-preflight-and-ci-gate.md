---
status: proposed
date: 2026-10-02
informed: repository agents
---

<!-- markdownlint-disable MD013 MD041 -->

# Make `make check-records` a preflight and CI gate

## Context and Problem Statement

[0175-MADR-conform-docs-tree-to-adopted-record-layout.md](0175-MADR-conform-docs-tree-to-adopted-record-layout.md) D8 added `scripts/check_records.py` and `make check-records`, and deliberately deferred wiring that target into `make preflight` or CI. The tree now matches the adopted layout: `make check-records` exits 0 (warnings only for the recorded numbering debt: lone PLANs 0054/0055/0177, two MADRs on 0154).

The target is still optional. A merge can land a broken relative link in a numbered record or a guide, and neither `make preflight` nor GitHub Actions will see it. [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) D3 already decided that CI runs every gate `make preflight` runs, so adding the checker to only one of those two surfaces would reopen that split.

Evidence, 2026-10-02:

* `Makefile` `check-records` runs `python3 scripts/check_records.py --check-all`. Errors (broken links, pairing/placement failures that `errors()`) fail the target; numbering-debt warnings print to stderr and leave exit 0.
* `make preflight` does not call it. Its comment still claims it mirrors the `go` and `flutter` jobs gate-for-gate.
* `.github/workflows/ci.yml` has no `check-records` / `check_records.py` step. The linux `go` job already runs `python3` for nothing in this area; `preflight` already depends on `python3` for `scripts/check-pub-advisories.py`.
* `make check-records` on current `master` (`1ab59450`) exits 0 with the four recorded warnings and zero broken-link errors.

## Decision Drivers

* 0175 D8 named this as the follow-up; the layout work is complete, so the deferral no longer applies.
* 0170 D3: a gate that joins `make preflight` also joins CI, and the other way around.
* The checker is unused until it has been seen to fail on a deliberately broken input (scratch clone, never this tree).
* Numbering-debt warnings must stay warnings. Renumbering 0154 or pairing 0054/0055/0177 is a different pair.
* Workflow edits need owner approval in the same turn as the phase that touches `.github/workflows/ci.yml`.

## Considered Options

* Wire `make check-records` into both `make preflight` and the linux `go` CI job, same command.
* Wire it into `make preflight` only.
* Wire it into CI only.
* Leave it as an optional target.

## Decision Outcome

Chosen option: "Wire `make check-records` into both `make preflight` and the linux `go` CI job, same command", because 0170 D3 forbids adding a preflight-only or CI-only gate, and an optional target does not stop a broken docs link from merging.

* **D1 — Same command in both places.** `make check-records` (`python3 scripts/check_records.py --check-all`) is the only invocation. Preflight calls that target. The linux `go` job (`Go (test; build on tag)`, `ubuntu-latest`) gains one step that runs `make check-records`. No second wrapper, no `--check` vs `--check-all` split.
* **D2 — One CI job, not the matrix.** Docs links are platform-independent. The step does not join the windows Go matrix, Flutter jobs, or `make ci-windows`. Those surfaces do not run the pub-advisory Python check either.
* **D3 — Warnings stay warnings.** Exit 0 with the recorded 0054/0055/0154/0177 warnings is green. A new broken relative link, or a new unrecorded duplicate number treated as an error, is red.
* **D4 — Fail first on a scratch clone.** Before the wiring commit, a scratch clone plants a broken relative link and shows `make check-records` exit 1. The working tree is not dirtied for that proof.

Executed by [0180-PLAN-check-records-is-a-preflight-and-ci-gate.md](0180-PLAN-check-records-is-a-preflight-and-ci-gate.md).

### Consequences

* Good, because a broken record or guide link fails the same command locally and on push.
* Good, because 0170 D3 stays true after the add.
* Neutral, because the four numbering-debt warnings continue to print and do not block.
* Bad, because a docs-only typo now fails the Go CI job and the full preflight, not only `make check-records`.
* Bad, because `.github/workflows/ci.yml` changes, which this repository treats as owner-gated.

### Confirmation

```sh
make check-records                 # exit 0; only the recorded numbering-debt warnings
make preflight                     # includes the check-records step
# CI linux go job has a step: make check-records
# scratch clone with a planted broken link: make check-records exits 1
```

## Pros and Cons of the Options

* **Wire into both preflight and the linux go CI job.**
  * Good, because 0170 D3 is satisfied and the optional target becomes a merge gate.
  * Good, because `ubuntu-latest` already has `python3`, matching preflight's pub-advisory step.
  * Bad, because it edits `ci.yml`.
* **Preflight only.**
  * Good, because no workflow edit.
  * Bad, because CI can still merge a broken link, and 0170 D3 is violated.
* **CI only.**
  * Good, because pushes are gated even if a developer skips preflight.
  * Bad, because `make preflight` would no longer mean a green CI (0170 F4 / D3).
* **Leave optional.**
  * Good, because zero Makefile/CI churn.
  * Bad, because 0175 D8's deferral becomes a permanent hole: the checker exists and is unused on merge.

## More Information

* Follows [0175-MADR-conform-docs-tree-to-adopted-record-layout.md](0175-MADR-conform-docs-tree-to-adopted-record-layout.md) D8.
* Bound by [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) D3.
* Out of this decision: markdownlint content rewrites; 0154 collision; lone PLANs 0054/0055/0177; mobile docs-tree split (0175 D7); deleting spike directories.
