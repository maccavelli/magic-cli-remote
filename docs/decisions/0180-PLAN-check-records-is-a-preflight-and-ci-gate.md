---
status: proposed
date: 2026-10-02
associated-madr: "0180-MADR-check-records-is-a-preflight-and-ci-gate.md"
---

<!-- markdownlint-disable MD013 MD041 MD024 MD033 MD036 -->

# Implement closing 0175 leftovers and making docs gates fail the merge

Associated MADR: [0180-MADR-check-records-is-a-preflight-and-ci-gate.md](0180-MADR-check-records-is-a-preflight-and-ci-gate.md)

## Goal

Numbering debt gone, spike dirs gone, mobile docs tree standing, project-config markdownlint green, and both `make check-records` and `npx markdownlint-cli2` running from `make preflight` and the linux `go` CI job.

## Scope

### In scope (the only files any phase may touch)

* P1: the 0154 later pair (rename to `--next`, expected 0181), `0054-PLAN-*` → `0004-PLAN-*`, `0055-PLAN-*` → `0012-PLAN-*`, new `0177-MADR-flutter-android-client-assessment.md`, citation repairs, `scripts/check_records.py` warning parentheticals, this pair's PLAN/MADR if they cite the old names.
* P2: delete `docs/reports/{codex-spike-0.145.0,kilo-spike-7.4.20,kilo-spike-7.4.22,kilo-spike-7.4.23,opencode-spike-1.18.5}/`; retarget `internal/provider/opencode/live_tool_stream_test.go`, `scripts/probe-codex-item-stream.py`, `scripts/quick-probe.py`, and comments in kilo/codex/0075 that name those paths.
* P3: `scripts/check_records.py` placement rule (any `docs/decisions/` / `docs/reports/`); `--check-all` includes `apps/mobile/docs/**` once that tree exists (may land empty-tolerant in P3, used in P4).
* P4: `git mv` of Flutter-companion records (slug contains `mobile`, `android`, `ios`, or `flutter`) and their same-number PLANs into `apps/mobile/docs/decisions/`; `0178-REPORT-mobile-ux-assessment.md` → `apps/mobile/docs/reports/`; guides `ops-android-*`, `ops-ios-*`, `mobile-profiling.md`, `guides/standards/mobile/` → `apps/mobile/docs/guides/`; scaffold `apps/mobile/docs/README.md`, `architecture.md`; link from `apps/mobile/README.md` and root `docs/README.md`; `AGENTS.md` one-line that a second tree exists; checker-driven link repair.
* P5: mechanical markdownlint fixes until `npx markdownlint-cli2` exits 0 (project config; not MADR/PLAN).
* P6: `Makefile` `preflight` gains `check-records` and markdownlint.
* P7: `.github/workflows/ci.yml` linux `go` job gains both steps (workflow edit; owner-gated).
* P8: this PLAN's execution record and status; MADR amendment only if D1–D7 change.

### Out of scope

* Rationale rewrites of historical MADRs beyond the dated renumber notes and 0177's new MADR.
* Windows Go matrix, Flutter CI jobs, `make ci-windows`.
* Push, unless the owner asks in the same turn.

## Implementation Steps

Order is fixed; each phase ends with its gates and one commit (`git commit --no-edit`).

**P1 — Numbering debt (D2).**

1. `python3 scripts/check_records.py --next` at the start of the phase (expected 0181). `git mv` the later 0154 pair (`0154-MADR-configurable-mcremote-message-size.md` and `0154-PLAN-configurable-mcremote-message-size.md`) to that number. Dated 2026-10-02 note: was 0154; 0154 remains the earlier mcrelay-paths pair.
2. `git mv` `0054-PLAN-hardening-implementation.md` → `0004-PLAN-hardening-implementation.md`. Dated note.
3. `git mv` `0055-PLAN-mcremote-server-remediation.md` → `0012-PLAN-mcremote-server-remediation.md`. Dated note.
4. Write `0177-MADR-flutter-android-client-assessment.md` from the PLAN's locked decisions (monorepo; Android-first Flutter companion; grok-if-ready else fake; paste-only pairing; cleartext `ws://` in debug). Point the PLAN at it.
5. Repair citations from `check_records.py --check` / `git grep` of the old filenames. Remove the 0154/0177 parentheticals from `scripts/check_records.py` so a future collision is not labelled "recorded".
6. Gates: `make check-records` exit 0 **with no numbering-debt warnings**. Scratch clone: two MADRs sharing a number still warn. Commit.

**P2 — Delete spikes (D3).**

1. Grep for `*-spike-*` and `docs/reports/kilo-spike` / `codex-spike` / `opencode-spike`. Retarget live writers to `testdata/` or `t.TempDir()` / process temp; comments to the new path or to the embedding test. Do not rewrite 0158 historical spike prose.
2. `git rm -r` the five directories.
3. Gates: grep of live (non-0158, non-0175-mapping) spike paths is empty; `make pre-add-check` on touched Go files; `go test` of the touched packages under `umask 022`; `make check-records` exit 0. Commit.

**P3 — Checker multi-tree (D5).**

1. Placement: parent path ends with `docs/decisions` (MADR/PLAN) or `docs/reports` (REPORT/GATES), not a hardcoded `docs/decisions`. `--check-all` also walks `apps/mobile/docs/README.md`, `architecture.md`, and `apps/mobile/docs/guides/**` when those paths exist.
2. Prove fail-first on a scratch clone: a MADR directly under `docs/` still warns; a broken link in a planted `apps/mobile/docs/guides/x.md` fails `--check-all` once the file exists.
3. Gates: `python3 -m py_compile scripts/check_records.py`; `make check-records` still 0 on this tree. Commit.

**P4 — Mobile tree (D4).**

1. `mkdir` `apps/mobile/docs/{decisions,reports,guides}`. `git mv` each matching pair together. Move 0178 REPORT. Move the listed guides and `standards/mobile`.
2. Write real `apps/mobile/docs/architecture.md` (Flutter companion as it is now) and `apps/mobile/docs/README.md` (markers + `--write-index` does **not** have to cover this tree; hand-write the mobile ToC or add a `--index-path` only if P3 already did). Link from `apps/mobile/README.md` and root `docs/README.md`. One line in `AGENTS.md` that mobile records live under `apps/mobile/docs/`.
3. Repair from checker output. `protocolDoc` and daemon guides stay in the root tree.
4. Gates: `ls apps/mobile/docs/` is exactly README, architecture, decisions, reports, guides; `make check-records` exit 0; `git log --follow` on one moved mobile MADR. Commit.

**P5 — markdownlint clean (D6).**

1. Capture `npx markdownlint-cli2` baseline. Fix mechanically until exit 0. Do not edit `*MADR*` / `*PLAN*` (excluded). Do not "fix" by adding file-wide disables except where a table/code block already used them.
2. Prove fail-first: scratch copy of `docs/guides/config.md` with a 300-character line; lint exits non-zero.
3. Gates: `npx markdownlint-cli2` exit 0. Commit.

**P6 — Preflight (D1, D6).**

1. `make preflight` runs `check-records` and `npx markdownlint-cli2` (after pub-advisories is fine; both need nothing from Go).
2. Gates: `git grep` shows both in the `preflight` recipe; `make check-records` 0. Do not require a full preflight run if Flutter is the only remaining cost — run the two new steps plus `python3 -m py_compile scripts/check_records.py`. Commit.

**P7 — CI (workflow edit; owner-gated).**

1. Linux `go` job (`Go (test; build on tag)`, `ubuntu-latest`): steps `make check-records` and `npx markdownlint-cli2` (needs Node; the job already sets up Node 24). Comment 0180 / 0170 D3.
2. Gates: steps are not under `windows-latest`. Commit.

**P8 — Closeout.**

1. `make check-records` 0, no numbering warnings. `npx markdownlint-cli2` 0. Grep confirms both Makefile and `ci.yml` call sites. No `docs/reports/*spike*`. Execution record with planted-failure output. `status: complete` only when A1–A12 hold.

## Verification

| # | Criterion |
| --- | --- |
| A1 | `make check-records` exit 0 with **no** numbering-debt warnings |
| A2 | Exactly one 0154 MADR (mcrelay-paths); configurable message-size pair lives at the number `--next` assigned in P1 |
| A3 | `0004-PLAN-hardening-implementation.md` and `0012-PLAN-mcremote-server-remediation.md` exist; 0054/0055 PLAN filenames do not |
| A4 | `0177-MADR-flutter-android-client-assessment.md` exists beside the 0177 PLAN |
| A5 | No `docs/reports/*spike*` directories; live writers do not write under `docs/` |
| A6 | `ls apps/mobile/docs/` is README.md, architecture.md, decisions/, reports/, guides/ |
| A7 | Flutter-companion records (slug rule) live under `apps/mobile/docs/decisions/`; 0178 REPORT under `apps/mobile/docs/reports/` |
| A8 | `npx markdownlint-cli2` exit 0 |
| A9 | `make preflight` invokes `check-records` and markdownlint |
| A10 | Linux `go` job runs both; windows/Flutter/`ci-windows` do not |
| A11 | Scratch-clone proofs: broken link, duplicate number, planted MD013, record outside `docs/decisions` |
| A12 | `git log --follow` resolves one moved 0154-later file and one moved mobile MADR |

## Rollout and Rollback

**Rollout:** P1→P8. No push unless asked in the same turn. P7 is the workflow edit.

**Rollback:** revert P7 then P6 to drop gates; P2 spike deletion is hard to undo after push — keep the commit until asked to revert.

## Deviations

**2026-10-02 — leftovers folded in before execution.** Owner: "add the out of pair items to scope." Original P1–P3 (preflight, CI, closeout only) are replaced by P1–P8 above. Not yet executed.
