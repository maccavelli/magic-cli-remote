---
status: proposed
date: 2026-10-02
informed: repository agents
---

<!-- markdownlint-disable MD013 MD041 -->

# Close 0175 leftovers and make docs gates fail the merge

## Context and Problem Statement

[0175-MADR-conform-docs-tree-to-adopted-record-layout.md](0175-MADR-conform-docs-tree-to-adopted-record-layout.md) finished the layout move. What it deferred, and what P8 still measured, is still sitting in the tree:

* **D8 leftover.** `make check-records` exists and is green, but `make preflight` and GitHub Actions do not run it. A broken relative link can merge. [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md) D3 already requires that every preflight gate also run in CI.
* **Numbering debt** the checker still warns about (exit 0): two MADRs claim **0154**; PLANs **0054**, **0055**, and **0177** have no same-number MADR. The skill says keep the earlier pair and renumber the later one; a MADR may carry several PLANs under its number; a lone PLAN that is really an assessment still needs a MADR or a kind change.
* **Spike directories** under `docs/reports/` (~668K, five trees). 0175 D6 kept them as a recorded divergence from `NNNN-REPORT` filenames. Tests and probes mostly *cite* them; `live_tool_stream_test.go` and the probe scripts still *write* there.
* **Mobile split** deferred by 0175 D7 until a mobile-only tree accreted. About 31 records already have mobile/android/ios/flutter in the slug, plus mobile guides (`ops-android-*`, `ops-ios-*`, `mobile-profiling.md`, `guides/standards/mobile/`). The owner now wants that split in this pair. That amends 0175 D7. The skill's purpose test still says the Flutter app *is* this repository's purpose, not an adjacent stack; this record chooses the split anyway because the owner put it in scope after accretion.
* **markdownlint.** P8 measured 3257 issues under the project config (MADR/PLAN excluded) and 151 on the user-facing files 0175 touched. Content rewrites were out of 0175. There is no lint gate.

`scripts/check_records.py` currently expects MADR/PLAN only in `docs/decisions/` and REPORT/GATES only in `docs/reports/`. A second tree at `apps/mobile/docs/` is a checker change, not only a `git mv`.

Evidence, 2026-10-02:

* `make check-records` on `1ab59450` exits 0 with exactly those four numbering warnings and zero broken-link errors.
* 0154-mcrelay-paths landed in `8f4363ab` (2026-09-08 08:15); 0154-configurable-message-size landed in `8eccfafb` (2026-09-08 21:24). The paths pair is earlier.
* 0054's companion is [0004-MADR-certificate-management.md](0004-MADR-certificate-management.md); 0004 has no PLAN file yet. 0055's companions are 0009/0012; 0012 has no PLAN file yet. 0177 is the Flutter Android assessment (legacy lone PLAN from 0175 P3).
* Spike write paths: `internal/provider/opencode/live_tool_stream_test.go`, `scripts/probe-codex-item-stream.py`, `scripts/quick-probe.py`. Kilo tests embed captures in consts and only mention spike paths in comments.

## Decision Drivers

* 0175's leftover list is now in scope; do not re-defer it.
* 0170 D3: preflight and CI share gates.
* One number, one MADR. Earlier pair keeps 0154; later pair takes `--next` at execution.
* Several PLANs may share a MADR's number.
* Checker unused until seen to fail on planted input (scratch clone).
* Workflow edits need owner approval in the same turn as the `ci.yml` phase.

## Considered Options

* Close every 0175 leftover in this pair: numbering, spikes, mobile tree, markdownlint, then the shared check-records and markdownlint gates.
* Wire the checker only (the 0180 first draft).
* Leave leftovers as warnings and optional targets.

## Decision Outcome

Chosen option: "Close every 0175 leftover in this pair: numbering, spikes, mobile tree, markdownlint, then the shared check-records and markdownlint gates", because the owner put the leftover list in scope and a green checker that still warns about known debt is not a finished sequence.

* **D1 — Same `make check-records` command in preflight and the linux `go` CI job.** `python3 scripts/check_records.py --check-all`. No `--check` vs `--check-all` split. Not on the windows matrix, Flutter jobs, or `make ci-windows`.
* **D2 — Numbering debt is repaired, not allowlisted.**
  * Keep the earlier 0154 pair: `0154-MADR-mcrelay-paths-demands-a-runnable-server.md` and its PLAN. Renumber the later pair (`configurable-mcremote-message-size` MADR+PLAN) to `--next` at that phase (expected **0181** once 0180 exists). Dated note in the moved files. Update every filename citation.
  * `0054-PLAN-hardening-implementation.md` → `0004-PLAN-hardening-implementation.md` (second plan under 0004).
  * `0055-PLAN-mcremote-server-remediation.md` → `0012-PLAN-mcremote-server-remediation.md` (implements 0012; 0012 has no PLAN yet).
  * Add `0177-MADR-flutter-android-client-assessment.md` from the locked decisions already in that PLAN (monorepo, Android-first Flutter companion, paste-only pairing, cleartext `ws://` in debug). Do not convert 0177 to REPORT: the PLAN chose product options.
  * After this, `make check-records` prints **no** numbering-debt warnings. Drop the hard-coded "0154 is the recorded case" / "0177 is the recorded legacy lone plan" parentheticals from `scripts/check_records.py` in the same change, or they lie.
* **D3 — Delete the five spike directories.** They are not `NNNN-REPORT` records. Retarget live probe writes to package `testdata/` or the process temp dir (not `docs/`). Update comments that name `docs/reports/*-spike-*`. This amends 0175 D6.
* **D4 — Second docs tree at `apps/mobile/docs/`.** Move the Flutter-companion records (slug contains `mobile`, `android`, `ios`, or `flutter`, including 0177) with every PLAN sharing that number, in one operation per number. Move mobile operator guides (`ops-android-*`, `ops-ios-*`, `mobile-profiling.md`) and `guides/standards/mobile/` into `apps/mobile/docs/guides/`. Move `0178-REPORT-mobile-ux-assessment.md` into `apps/mobile/docs/reports/`. Scaffold `apps/mobile/README.md` link, `apps/mobile/docs/README.md` (ToC + "I want to…"), and a real `apps/mobile/docs/architecture.md`. Root `docs/README.md` lists the second tree. Sequence stays repository-wide; no renumber for the move. This amends 0175 D7. Phone-transport / protocol / daemon records stay in the root tree even when the phone is a client of them.
* **D5 — Checker placement is by suffix, not a single root.** A MADR/PLAN is valid under any `docs/decisions/`; a REPORT/GATES under any `docs/reports/`. `--write-index` still regenerates only the **root** `docs/README.md`. `--check-all` also walks `apps/mobile/docs/**` unnumbered files plus that tree's README and architecture. A record in the wrong kind-directory still warns.
* **D6 — Project-config `markdownlint-cli2` is clean, then gated.** Mechanical fixes only (MD013 wrap, MD004 dashes, MD040 language, list/table rules). MADR/PLAN stay excluded by `.markdownlint-cli2.jsonc`. After clean, `npx markdownlint-cli2` exits 0. Then `make preflight` and the linux `go` job run it (0170 D3). Fail first on a scratch clone with a planted MD013.
* **D7 — Fail first on a scratch clone** for each new gate: broken relative link, duplicate number, markdownlint finding, and (after D5) a record sitting outside `docs/decisions` / `docs/reports`. Never dirty this tree for those proofs.

Executed by [0180-PLAN-check-records-is-a-preflight-and-ci-gate.md](0180-PLAN-check-records-is-a-preflight-and-ci-gate.md).

### Consequences

* Good, because the checker, the lint, and the leftover debt all stop being tribal knowledge.
* Good, because 0170 D3 stays true for both new gates.
* Neutral, because 0181 is a gap-fill number for a 2026-09-08 decision; the dated note carries the old 0154 name.
* Bad, because this pair edits `ci.yml`, deletes committed spike blobs, moves ~30 records, and rewrites lint in hundreds of markdown files.
* Bad, because D4 disagrees with the skill's "purpose vs adjacent" test; the owner is choosing a second tree anyway.

### Confirmation

```sh
make check-records                 # exit 0, no numbering-debt warnings
npx markdownlint-cli2              # exit 0 (project config)
make preflight                     # includes check-records and markdownlint
# CI linux go job runs both
ls apps/mobile/docs/               # README.md architecture.md decisions/ reports/ guides/
# no docs/reports/*-spike-*
```

## Pros and Cons of the Options

* **Close every leftover in this pair (chosen).**
  * Good, because 0175's leftover list has an owner and an end state.
  * Bad, because blast radius is Makefile, CI, checker, records, mobile tree, lint, and Go probe paths.
* **Checker wiring only (0180 first draft).**
  * Good, because two files.
  * Bad, because the owner asked to put the leftover list in scope.
* **Leave leftovers.**
  * Good, because zero churn.
  * Bad, because the warnings and optional target never become a merge gate.

## More Information

* Follows 0175 D8; **amends 0175 D6** (spikes) and **D7** (mobile split) when this record is accepted.
* Bound by 0170 D3.
* First draft of 0180 (checker-only) was never executed; this file replaces it before approval.

## Amendment (2026-10-02) — leftovers in scope

Owner: "add the out of pair items to scope." The first draft's D3 (warnings stay warnings) and "out of this decision" list are withdrawn. D2–D7 above replace them. PLAN phases were rewritten to match; the original P1–P3 wiring is now P6–P7 after the debt is gone.
