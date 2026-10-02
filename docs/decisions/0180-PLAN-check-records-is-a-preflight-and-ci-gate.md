---
status: proposed
date: 2026-10-02
associated-madr: "0180-MADR-check-records-is-a-preflight-and-ci-gate.md"
---

<!-- markdownlint-disable MD013 MD041 MD024 MD033 MD036 -->

# Implement making `make check-records` a preflight and CI gate

Associated MADR: [0180-MADR-check-records-is-a-preflight-and-ci-gate.md](0180-MADR-check-records-is-a-preflight-and-ci-gate.md)

## Goal

`make preflight` and the linux `go` GitHub Actions job both run `make check-records`. A planted broken relative link fails that target on a scratch clone. Current `master` stays green (recorded numbering-debt warnings only).

## Scope

### In scope (the only files any phase may touch)

* P1: `Makefile` (`preflight` target only, plus a one-line comment if the existing `check-records` comment needs to say it is now a preflight/CI gate).
* P2: `.github/workflows/ci.yml` (one step on the linux `go` job: `make check-records`).
* P3: this PLAN's execution record and status; a MADR amendment only if a deviation changes D1–D4.

### Out of scope

* `scripts/check_records.py` behaviour (exit codes, warning vs error).
* Renumbering 0154; pairing 0054/0055/0177.
* markdownlint, mobile tree split, spike deletion.
* Windows Go matrix, Flutter jobs, `make ci-windows`.
* `AGENTS.md` (it already points at `make check-records` / `scripts/check_records.py --next`).
* Push, unless the owner asks in the same turn.

## Implementation Steps

Order is fixed; each phase ends with its gates and one commit (`git commit --no-edit`).

**P1 — Preflight.**

1. On a scratch clone under the system temp dir, plant one numbered record with a broken relative link and run `make check-records`; observe exit 1 and `broken relative link:`. Do not dirty this tree.
2. Add `$(MAKE) --no-print-directory check-records` to `make preflight` after the pub-advisory step (both already need `python3`).
3. Gates: `make check-records` exit 0 on this tree; the planted-failure evidence; `git grep -n check-records Makefile` shows both the target and the preflight call. Commit.

**P2 — CI (workflow edit; owner-gated).**

1. In `.github/workflows/ci.yml`, on the `go` job (`name: Go (test; build on tag)`, `runs-on: ubuntu-latest`), add a step after checkout/setup that is cheap and independent of Go: `name: Check records and docs links` / `run: make check-records`. Place it next to the other Makefile-shared gates (after `Validate systemd units` is fine). Comment that it is 0180 / 0170 D3, shared with `make preflight`.
2. Gates: `python3 -c 'import yaml'` is not required; visually confirm the step is under `jobs.go` and not under a matrix include that sets `windows-latest`. `make check-records` still exit 0. Commit.

**P3 — Closeout.**

1. `make check-records` exit 0. `git grep -n 'check-records' Makefile .github/workflows/ci.yml` names both call sites.
2. Write the execution record (planted-failure output, Makefile diff intent, CI step location). Set `status: complete` only when A1–A6 are verified. Amend the MADR only if D1–D4 changed.

## Verification

| # | Criterion |
| --- | --- |
| A1 | `make preflight` invokes `make check-records` (or `python3 scripts/check_records.py --check-all`) |
| A2 | The linux `go` job in `.github/workflows/ci.yml` has a step that runs `make check-records` |
| A3 | No windows/Flutter/`ci-windows` surface gained the step |
| A4 | `make check-records` on this tree exits 0; warnings are only 0054, 0055, 0154, 0177 |
| A5 | A scratch clone with a planted broken relative link: `make check-records` exits 1 and prints `broken relative link:` |
| A6 | `scripts/check_records.py` is unchanged |

## Rollout and Rollback

**Rollout:** P1 then P2 then P3. No push unless asked in the same turn. P2 is the workflow edit.

**Rollback:** `git revert` P2 then P1. The checker target remains from 0175.

## Deviations

None yet. Mid-execution findings follow the skill: dated entry here, MADR amendment if a decision or asserted fact changes, then continue.
