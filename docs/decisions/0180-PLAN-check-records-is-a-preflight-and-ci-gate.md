---
status: in-progress
date: 2026-10-03
associated-madr: "0180-MADR-check-records-is-a-preflight-and-ci-gate.md"
---

<!-- markdownlint-disable MD013 MD041 MD024 MD033 MD036 -->

# Implement closing 0175 leftovers and making docs gates fail the merge

Associated MADR: [0180-MADR-check-records-is-a-preflight-and-ci-gate.md](0180-MADR-check-records-is-a-preflight-and-ci-gate.md)

## Goal

Numbering debt gone, spike blobs under package `testdata/`, mobile docs tree standing, user-facing markdownlint green, and both `make check-records` and `make markdownlint-docs` running from `make preflight` and the linux `go` CI job.

## Scope

### In scope (the only files any phase may touch)

* P1: later 0154 pair → `--next` (expected 0181); `0054-PLAN-*` → `0004-PLAN-*`; new `0055-MADR-mcremote-server-remediation.md`; `0177-PLAN-*` → `docs/reports/0177-REPORT-flutter-android-client-assessment.md`; citation repairs; `scripts/check_records.py` warning parentheticals.
* P2: `git mv` the five `docs/reports/*-spike-*` dirs into `internal/provider/{codex,kilo,opencode}/testdata/`; retarget `live_tool_stream_test.go`, `scripts/probe-codex-item-stream.py`, `scripts/quick-probe.py`, and comments in kilo/codex/0075.
* P3: `scripts/check_records.py` placement by `docs/decisions` / `docs/reports` suffix; `--check-all` includes `apps/mobile/docs/**` when present.
* P4: `git mv` Flutter-companion MADR/PLAN pairs into `apps/mobile/docs/decisions/`; `0177-REPORT` and `0178-REPORT` → `apps/mobile/docs/reports/`; guides `ops-android-*`, `ops-ios-*`, `mobile-profiling.md`, `guides/standards/mobile/` → `apps/mobile/docs/guides/`; scaffold `apps/mobile/docs/README.md`, `architecture.md`; links from `apps/mobile/README.md` and root `docs/README.md`; `AGENTS.md` one-line; checker-driven link repair.
* P5: `Makefile` target `markdownlint-docs` (user-facing set below); mechanical lint fixes on that set until the target exits 0.
* P6: `Makefile` `preflight` gains `check-records` and `markdownlint-docs`.
* P7: `.github/workflows/ci.yml` linux `go` job gains both steps (workflow edit; owner-gated).
* P8: this PLAN's execution record and status; MADR amendment only if D1–D7 change.

User-facing lint set (D6): `README.md`, `docs/README.md`, `docs/architecture.md`, `docs/guides/**/*.md`, `docs/reports/**/*.md`, `apps/mobile/README.md`, and after P4 `apps/mobile/docs/README.md`, `apps/mobile/docs/architecture.md`, `apps/mobile/docs/guides/**/*.md`, `apps/mobile/docs/reports/**/*.md`. Not `AGENTS.md`. Not MADR/PLAN.

### Out of scope

* Rationale rewrites of historical MADRs beyond dated notes and the thin 0055 MADR.
* Whole-tree `npx markdownlint-cli2` (the 3257).
* Windows Go matrix, Flutter CI jobs, `make ci-windows`.
* Push, unless the owner asks in the same turn.

## Implementation Steps

Order is fixed; each phase ends with its gates and one commit (`git commit --no-edit`).

**P1 — Numbering debt (D2).**

1. `python3 scripts/check_records.py --next` (expected 0181). `git mv` `0154-MADR-configurable-mcremote-message-size.md` and its PLAN to that number. Dated note: was 0154; 0154 remains mcrelay-paths.
2. `git mv` `0054-PLAN-hardening-implementation.md` → `0004-PLAN-hardening-implementation.md`. Dated note.
3. Write thin `0055-MADR-mcremote-server-remediation.md`. Leave `0055-PLAN-mcremote-server-remediation.md` in place. Point each at the other.
4. `git mv` `0177-PLAN-flutter-android-client-assessment.md` → `docs/reports/0177-REPORT-flutter-android-client-assessment.md`. Dated note: assessment, not a pair. Repair citations (0018, 0026, 0175, 0176, README).
5. Repair remaining citations from checker / `git grep`. Remove 0154/0177 parentheticals from `scripts/check_records.py`.
6. Gates: `make check-records` exit 0 **with no numbering-debt warnings**. Scratch clone: two MADRs sharing a number still warn. Commit.

**P2 — Spikes to testdata (D3).**

1. `git mv docs/reports/codex-spike-0.145.0 internal/provider/codex/testdata/codex-spike-0.145.0` (create `testdata/` if needed). Same for `kilo-spike-7.4.20`, `7.4.22`, `7.4.23` under `internal/provider/kilo/testdata/`, and `opencode-spike-1.18.5` under `internal/provider/opencode/testdata/`.
2. Retarget writers and comments. 0158 historical prose stays.
3. Gates: no live `docs/reports/*spike*` path outside 0158/0175 mapping prose; `make pre-add-check` on touched Go; `go test` of touched packages under `umask 022`; `make check-records` 0. Commit.

**P3 — Checker multi-tree (D5).**

1. Placement by directory suffix. `--check-all` walks `apps/mobile/docs/` unnumbered files when they exist.
2. Fail-first on a scratch clone: MADR directly under `docs/` warns; planted broken link under `apps/mobile/docs/guides/` fails `--check-all`.
3. Gates: `python3 -m py_compile scripts/check_records.py`; `make check-records` 0. Commit.

**P4 — Mobile tree (D4).**

1. `mkdir` `apps/mobile/docs/{decisions,reports,guides}`. `git mv` each matching MADR/PLAN pair together. Move 0177 and 0178 REPORTs. Move the listed guides and `standards/mobile`.
2. Real `apps/mobile/docs/architecture.md` and `apps/mobile/docs/README.md`. Link from `apps/mobile/README.md` and root `docs/README.md`. One line in `AGENTS.md`.
3. Repair from checker output. `protocolDoc` stays in the root tree.
4. Gates: `ls apps/mobile/docs/` is exactly those five names; `make check-records` 0; `git log --follow` on one moved mobile MADR. Commit.

**P5 — User-facing markdownlint (D6).**

1. Add `make markdownlint-docs` running `npx markdownlint-cli2 --no-globs` on the D6 set (globs that exist; after P4 include the mobile tree). Capture baseline. Mechanical fixes until the target exits 0.
2. Fail-first: scratch copy of `docs/guides/config.md` with a 300-character line; the target exits non-zero.
3. Gates: `make markdownlint-docs` 0. Whole-tree `npx markdownlint-cli2` may still fail; record the count, do not chase it. Commit.

**P6 — Preflight (D1, D6).**

1. `make preflight` runs `check-records` and `markdownlint-docs`.
2. Gates: `git grep` shows both in the `preflight` recipe. Commit.

**P7 — CI (workflow edit; owner-gated).**

1. Linux `go` job: `make check-records` and `make markdownlint-docs` (Node 24 is already set up). Comment 0180 / 0170 D3.
2. Gates: steps are not under `windows-latest`. Commit.

**P8 — Closeout.**

1. `make check-records` 0, no numbering warnings. `make markdownlint-docs` 0. Both call sites in Makefile and `ci.yml`. No `docs/reports/*spike*`. Execution record. `status: complete` only when A1–A12 hold.

## Verification

| # | Criterion |
| --- | --- |
| A1 | `make check-records` exit 0 with **no** numbering-debt warnings |
| A2 | Exactly one 0154 MADR (mcrelay-paths); configurable message-size pair lives at the P1 `--next` number |
| A3 | `0004-PLAN-hardening-implementation.md` exists; `0055-MADR` and `0055-PLAN` exist; no `0054-PLAN-*` filename |
| A4 | `0177-REPORT-flutter-android-client-assessment.md` exists (mobile tree after P4); no `0177-PLAN-*` |
| A5 | No `docs/reports/*spike*`; spike dirs live under the three provider `testdata/` trees |
| A6 | `ls apps/mobile/docs/` is README.md, architecture.md, decisions/, reports/, guides/ |
| A7 | Flutter-companion MADR/PLAN pairs under `apps/mobile/docs/decisions/`; 0177 and 0178 REPORTs under `apps/mobile/docs/reports/` |
| A8 | `make markdownlint-docs` exit 0; whole-tree `npx markdownlint-cli2` is not required to be 0 |
| A9 | `make preflight` invokes `check-records` and `markdownlint-docs` |
| A10 | Linux `go` job runs both; windows/Flutter/`ci-windows` do not |
| A11 | Scratch-clone proofs: broken link, duplicate number, planted MD013 on the user-facing set, record outside `docs/decisions` |
| A12 | `git log --follow` resolves the renumbered 0154-later pair and one moved mobile MADR |

## Rollout and Rollback

**Rollout:** P1→P8. No push unless asked in the same turn. P7 is the workflow edit.

**Rollback:** revert P7 then P6 to drop gates; P2 is a `git mv` so revert restores `docs/reports/*-spike-*`.

## Deviations

**2026-10-02 — leftovers folded in before execution.** Owner: "add the out of pair items to scope."

**2026-10-03 — owner picks.** 1A split; 2B testdata; 3B 0177-REPORT; 4B user-facing lint; 5C 0055-MADR. Not yet executed.
