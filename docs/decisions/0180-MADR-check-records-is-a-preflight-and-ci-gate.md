---
status: accepted
date: 2026-10-03
informed: repository agents
---

<!-- markdownlint-disable MD013 MD041 -->

# Close 0175 leftovers and make docs gates fail the merge

## Context and Problem Statement

[0175-MADR-conform-docs-tree-to-adopted-record-layout.md](0175-MADR-conform-docs-tree-to-adopted-record-layout.md) finished the layout move. What it deferred, and what P8 still measured, is still sitting in the tree:

* **D8 leftover.** `make check-records` exists and is green, but `make preflight` and GitHub Actions do not run it. A broken relative link can merge. [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) D3 already requires that every preflight gate also run in CI.
* **Numbering debt** the checker still warns about (exit 0): two MADRs claim **0154**; PLANs **0054**, **0055**, and **0177** have no same-number MADR.
* **Spike directories** under `docs/reports/` (~668K, five trees). 0175 D6 kept them as a recorded divergence from `NNNN-REPORT` filenames. Tests and probes mostly *cite* them; `live_tool_stream_test.go` and the probe scripts still *write* there.
* **Mobile split** deferred by 0175 D7. About 31 records already have mobile/android/ios/flutter in the slug, plus mobile guides. The skill's purpose test still says the Flutter app *is* this repository's purpose; the owner chose a second tree anyway (2026-10-03).
* **markdownlint.** P8 measured 3257 issues under the project config (MADR/PLAN excluded) and 151 on the user-facing files 0175 touched. There is no lint gate.

`scripts/check_records.py` currently expects MADR/PLAN only in `docs/decisions/` and REPORT/GATES only in `docs/reports/`. A second tree at `apps/mobile/docs/` is a checker change, not only a `git mv`.

Evidence, 2026-10-02:

* `make check-records` on `1ab59450` exits 0 with exactly those four numbering warnings and zero broken-link errors.
* 0154-mcrelay-paths landed in `8f4363ab` (2026-09-08 08:15); 0154-configurable-message-size landed in `8eccfafb` (2026-09-08 21:24). The paths pair is earlier.
* 0054's companion is [0004-MADR-certificate-management.md](0004-MADR-certificate-management.md); 0004 has no PLAN file yet. 0055 has no MADR. 0177 is the Flutter Android assessment (legacy lone PLAN from 0175 P3).
* Spike write paths: `internal/provider/opencode/live_tool_stream_test.go`, `scripts/probe-codex-item-stream.py`, `scripts/quick-probe.py`. Kilo tests embed captures in consts and only mention spike paths in comments.

Owner answers, 2026-10-03: mobile split **A**; spikes **B** (testdata); 0177 **B** (REPORT); markdownlint **B** (user-facing set); 0055 **C** (keep number, write MADR).

## Decision Drivers

* 0175's leftover list is in scope; do not re-defer it.
* 0170 D3: preflight and CI share gates.
* One number, one MADR. Earlier pair keeps 0154; later pair takes `--next` at execution.
* Several PLANs may share a MADR's number.
* An assessment that decides nothing is a REPORT, not a PLAN.
* Checker unused until seen to fail on planted input (scratch clone).
* Workflow edits need owner approval in the same turn as the `ci.yml` phase.

## Considered Options

* Close every 0175 leftover in this pair, with the 2026-10-03 owner picks for how.
* Wire the checker only (the 0180 first draft).
* Leave leftovers as warnings and optional targets.

## Decision Outcome

Chosen option: "Close every 0175 leftover in this pair, with the 2026-10-03 owner picks for how", because the leftover list is in scope and the owner answered the forks.

* **D1 — Same `make check-records` command in preflight and the linux `go` CI job.** `python3 scripts/check_records.py --check-all`. Not on the windows matrix, Flutter jobs, or `make ci-windows`.
* **D2 — Numbering debt is repaired, not allowlisted.**
  * Keep the earlier 0154 pair: `0154-MADR-mcrelay-paths-demands-a-runnable-server.md` and its PLAN. Renumber the later pair (`configurable-mcremote-message-size` MADR+PLAN) to `--next` at that phase (expected **0181** once 0180 exists). Dated note. Update every filename citation.
  * `0054-PLAN-hardening-implementation.md` → `0004-PLAN-hardening-implementation.md` (second plan under 0004).
  * Keep `0055-PLAN-mcremote-server-remediation.md`. Write a thin `0055-MADR-mcremote-server-remediation.md` that records the remediation as the implementation of the 0009/0012 audit findings (no rename).
  * Convert `0177-PLAN-flutter-android-client-assessment.md` → `0177-REPORT-flutter-android-client-assessment.md` in `docs/reports/` (then P4 moves it with the mobile tree). It is an assessment, not a pair. Dated note. Do not write a 0177 MADR.
  * After this, `make check-records` prints **no** numbering-debt warnings. Drop the hard-coded "0154 is the recorded case" / "0177 is the recorded legacy lone plan" parentheticals from `scripts/check_records.py`.
* **D3 — Spike blobs move into package `testdata/`, not `docs/`.** `git mv` the five directories: Codex → `internal/provider/codex/testdata/codex-spike-0.145.0/`, Kilo three trees → `internal/provider/kilo/testdata/kilo-spike-<ver>/`, OpenCode → `internal/provider/opencode/testdata/opencode-spike-1.18.5/`. Retarget live writers and comments. Do not rewrite 0158 historical spike prose. This amends 0175 D6.
* **D4 — Second docs tree at `apps/mobile/docs/`.** Move Flutter-companion **MADR/PLAN** pairs (slug contains `mobile`, `android`, `ios`, or `flutter`) together, one number at a time. Move `0177-REPORT` and `0178-REPORT` into `apps/mobile/docs/reports/`. Move `ops-android-*`, `ops-ios-*`, `mobile-profiling.md`, and `guides/standards/mobile/` into `apps/mobile/docs/guides/`. Scaffold `apps/mobile/docs/README.md`, a real `architecture.md`, and links from `apps/mobile/README.md` and root `docs/README.md`. Sequence stays repository-wide. Amends 0175 D7. Phone-transport / protocol / daemon records stay in the root tree.
* **D5 — Checker placement is by suffix, not a single root.** A MADR/PLAN is valid under any `docs/decisions/`; a REPORT/GATES under any `docs/reports/`. `--write-index` still regenerates only the **root** `docs/README.md`. `--check-all` also walks `apps/mobile/docs/**` unnumbered files plus that tree's README and architecture.
* **D6 — User-facing markdownlint is clean, then that set is gated.** Mechanical fixes only. The set is: root `README.md`, `docs/README.md`, `docs/architecture.md`, `docs/guides/**`, `docs/reports/**/*.md`, `apps/mobile/README.md`, and after P4 `apps/mobile/docs/**` unnumbered markdown (guides, README, architecture, REPORTs). MADR/PLAN stay excluded. `AGENTS.md` is out of the set. Default-config whole-tree `npx markdownlint-cli2` (3257) is **not** this pair's gate. A Makefile target runs `markdownlint-cli2 --no-globs` on that set; preflight and the linux `go` job call it. Fail first on a planted MD013 in a copy of a guide.
* **D7 — Fail first on a scratch clone** for each new gate. Never dirty this tree for those proofs.

Executed by [0180-PLAN-check-records-is-a-preflight-and-ci-gate.md](0180-PLAN-check-records-is-a-preflight-and-ci-gate.md).

### Consequences

* Good, because numbering debt, spike location, the mobile tree, and the two merge gates have an owner and an end state.
* Good, because 0170 D3 stays true for both new gates.
* Neutral, because 0181 is a gap-fill number for a 2026-09-08 decision; the dated note carries the old 0154 name.
* Neutral, because whole-tree markdownlint stays dirty; only the user-facing set is gated.
* Bad, because this pair edits `ci.yml`, moves spike blobs and ~30 records, and rewrites lint in the user-facing set.
* Bad, because D4 disagrees with the skill's "purpose vs adjacent" test; the owner chose the split on 2026-10-03.

### Confirmation

```sh
make check-records                 # exit 0, no numbering-debt warnings
make markdownlint-docs             # user-facing set only, exit 0
make preflight                     # includes check-records and markdownlint-docs
# CI linux go job runs both
ls apps/mobile/docs/               # README.md architecture.md decisions/ reports/ guides/
# no docs/reports/*-spike-*
# spike dirs live under internal/provider/{codex,kilo,opencode}/testdata/
```

## Pros and Cons of the Options

* **Close leftovers with the 2026-10-03 picks (chosen).**
  * Good, because each fork has an owner answer.
  * Bad, because blast radius is still Makefile, CI, checker, records, mobile tree, testdata, and user-facing lint.
* **Checker wiring only (0180 first draft).**
  * Good, because two files.
  * Bad, because the leftover list is in scope.
* **Leave leftovers.**
  * Good, because zero churn.
  * Bad, because the warnings and optional target never become a merge gate.

## More Information

* Follows 0175 D8; **amends 0175 D6** (spikes) and **D7** (mobile split) when this record is accepted.
* Bound by 0170 D3.
* First draft of 0180 (checker-only) was never executed.

## Amendment (2026-10-02) — leftovers in scope

Owner: "add the out of pair items to scope." The first draft's "warnings stay warnings" D3 is withdrawn.

## Amendment (2026-10-03) — owner picks

* D4 split to `apps/mobile/docs/` (A).
* D3 `git mv` spikes into package `testdata/` (B), not `git rm`.
* 0177 becomes `0177-REPORT` (B), not a new MADR.
* D6 user-facing markdownlint set only (B), not whole-tree 3257.
* 0055 keeps its number; write `0055-MADR` (C). Do not rename to 0012-PLAN.
