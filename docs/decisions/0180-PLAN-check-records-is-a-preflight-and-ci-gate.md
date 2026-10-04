---
status: in-progress
date: 2026-10-04
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
* P9 (added 2026-10-04, D8): `internal/protocol/doc_coverage_test.go` only, plus this PLAN's execution record. `docs/guides/protocol-v1.md` is **not** touched; it stays as P5 linted it.

User-facing lint set (D6): `README.md`, `docs/README.md`, `docs/architecture.md`, `docs/guides/**/*.md`, `docs/reports/**/*.md`, `apps/mobile/README.md`, and after P4 `apps/mobile/docs/README.md`, `apps/mobile/docs/architecture.md`, `apps/mobile/docs/guides/**/*.md`, `apps/mobile/docs/reports/**/*.md`. Not `AGENTS.md`. Not MADR/PLAN.

### Out of scope

* Rationale rewrites of historical MADRs beyond dated notes and the thin 0055 MADR.
* Whole-tree `npx markdownlint-cli2` (the 3257).
* Windows Go matrix, Flutter CI jobs, `make ci-windows`.
* Push, unless the owner asks in the same turn.
* Re-tagging or releasing `v0.20.2`, or cutting a new tag. A published tag is not moved; a replacement tag needs its own ask.

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

**P9 — Event-type guard reads the paragraph (D8; added 2026-10-04).**

1. ~~Branch off `master` before any commit. Org rule 011 forbids commits on `master`; load `enforcing-git-branch-gates`. Suggested name: `fix/0180-p9-event-type-guard`. First commit: this MADR/PLAN amendment alone (bootstrap exception; no Go in it).~~ Superseded 2026-10-04 (see deviations): no branch, and the agent does not commit. The agent stages the work on `master` and the owner commits and pushes.
2. In `TestEventTypesAreDocumented`, once the line starting with `eventTypeListPrefix` is found, take it and each following line until the first line where `strings.TrimSpace` is empty or the file ends. Check types against that joined text. Keep the existing `Fatalf` for a missing prefix. Reword the error so it names the paragraph rather than the line, and update the comment to match. `TestErrorCodesAreDocumented`, `readProtocolDoc`, and `protocolDoc` stay as they are.
3. Fail-first, in a `git clone --local` under the session scratchpad and never in this tree. Each plant gets its own clean copy of the doc:
   * (a) the doc with `` `codex_unsupported_item` `` deleted from the last line of the enumeration: the test fails, naming that type and only that type;
   * (b) the doc with a blank line inserted before the enumeration's last line: the test fails, naming the three types on that line. This proves the paragraph stops at a blank line and the test does not search the rest of the document;
   * (c) the doc with the `Event \`type\` values:` prefix reworded: `Fatalf` fires with "no line starting".
   Record each command, its exit code, and its output.
4. Gates: `go test -count=1 ./internal/protocol/` exits 0 against the unmodified `protocol-v1.md`. `make pre-add-check FILES=internal/protocol/doc_coverage_test.go` exits 0. `make preflight` exits 0; this is the gate P5 and P8 skipped. `make check-records` exits 0. ~~Commit with `git commit --no-edit`.~~ Stage with `git add`; the owner commits (2026-10-04 deviation).
5. Execution record: P9 entry, A13–A16 results. Set `status: complete` only once A16 holds. Whoever confirms A16 green flips the status. A16 needs a push, and a push needs the owner's ask in the same turn. Until then, `status` stays `in-progress`.

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
| A13 | `go test -count=1 ./internal/protocol/` exit 0 with `docs/guides/protocol-v1.md` as P5 left it (four-line enumeration) |
| A14 | P9 scratch-clone proofs (a), (b), (c) each fail with the expected message |
| A15 | `make preflight` exit 0 on the P9 commit |
| A16 | CI Go jobs (linux/arm64, windows/amd64, "Go (test; build on tag)") green on the pushed P9 commit |

## Rollout and Rollback

**Rollout:** P1→P8. No push unless asked in the same turn. P7 is the workflow edit. ~~P9 lands on a branch. Pushing it, opening a PR, and merging it to `master` each need an explicit ask.~~ P9 is staged on `master`; the owner commits and pushes (2026-10-04 deviation).

**Rollback:** revert P7 then P6 to drop gates; P2 is a `git mv` so revert restores `docs/reports/*-spike-*`. Reverting P9 restores the single-line reader, and CI goes red again on the current doc. If P9 has to be backed out, the fallback is the MADR's MD013-suppression option, which needs its own owner decision.

## Deviations

**2026-10-02 — leftovers folded in before execution.** Owner: "add the out of pair items to scope."

**2026-10-03 — owner picks.** 1A split; 2B testdata; 3B 0177-REPORT; 4B user-facing lint; 5C 0055-MADR. ~~Not yet executed.~~ Executed P1–P8.

**2026-10-04 — P7 replayed onto origin before push.** After P7 landed on P6, origin gained two out-of-pair flake-ledger commits (`3a49f541` / `v0.20.2`, then `a9843f59`, both `ci-flakes.tsv` only). Owner: "fix repo sync." The unpushed P7 `ci.yml` commit was rebased onto that tip and fast-forwarded (`a2fef3f9` → `bc3431df`). No MADR amendment: D1–D7 unchanged.

**2026-10-04 — CI red after push; P9 added.** P5's reflow of the event-type enumeration in `docs/guides/protocol-v1.md` broke `TestEventTypesAreDocumented` (`internal/protocol/doc_coverage_test.go:43`), which reads only the prefix line. Every Go CI job failed from the first push carrying P5 onward (runs 37173869896, 37176700789 on `v0.20.2`, 37177612995), and the failure reproduces locally. Neither P5's gates nor P8 ran `go test` or `make preflight`. Owner: "yes" to having the guard read the paragraph. Files added to scope: `internal/protocol/doc_coverage_test.go`. MADR amended (2026-10-04, D8). Status goes back from `complete` to `in-progress`.

**2026-10-04 — P9 is staged, not branched or committed.** Owner: "we will not be providing a jira key nor creating a feature branch. this project does not require adherence to those mandates. you only add and stage. i will commit and push." P9 step 1's branch and records-only commit are dropped, and step 4's commit becomes `git add`. Records and test are staged together on `master`; the owner chooses how to split commits. No MADR change: D8 is unaffected.

## Execution record (2026-10-04)

Local phases P1–P7, one commit each. P1–P7 are on `origin/master` after the P7 rebase+push. P8 is this closeout.

| Phase | Commit | Subject |
| :--- | :--- | :--- |
| P1 | `4e2ac105` | docs(records): normalize decision record numbering and validation |
| P2 | `7d328aea` | chore(testdata): move provider spike fixtures from docs |
| P3 | `2f6cb5c5` | fix(records): support mobile docs in validation |
| P4 | `a1bee687` | docs(mobile): relocate Flutter documentation tree |
| P5 | `36cb45bd` | chore(docs): add markdown lint target |
| P6 | `95b38565` | ci(preflight): run documentation validation gates |
| P7 | `bc3431df` | ci(linux): run records and docs markdown lint checks |
| P9 | staged, not committed | owner commits (2026-10-04 deviation) |

### P1 — Numbering debt

Later 0154 pair → `0181-MADR-configurable-mcremote-message-size.md` / `0181-PLAN-configurable-mcremote-message-size.md`. `0054-PLAN` → `0004-PLAN-hardening-implementation.md`. Thin `0055-MADR-mcremote-server-remediation.md`. `0177-PLAN` → `0177-REPORT` (then P4 to the mobile reports tree). Checker 0154/0177 parentheticals removed.

### P2 — Spikes to testdata

Five `docs/reports/*-spike-*` trees `git mv`'d to `internal/provider/{codex,kilo,opencode}/testdata/`. Live writers retargeted. 0158/0175 mapping prose left as written.

### P3 — Checker multi-tree

Placement by `docs/decisions` / `docs/reports` suffix. `--check-all` walks `apps/mobile/docs` unnumbered files when present.

### P4 — Mobile tree

`apps/mobile/docs/` is README.md, architecture.md, decisions/, reports/, guides/. Flutter-companion MADR/PLAN pairs under `apps/mobile/docs/decisions/`. 0177 and 0178 REPORTs under `apps/mobile/docs/reports/`. `protocolDoc` stays `docs/guides/protocol-v1.md`.

### P5 — User-facing markdownlint

`make markdownlint-docs` (`npx markdownlint-cli2 --no-globs` on the D6 set, 47 files). Mechanical `--fix`, wraps, fenced console in 0098/0099/0100, sibling heading suffixes on 0098. Target 0.

Whole-tree `npx markdownlint-cli2` at P5/P8: **3126 issues in 50 files** (136 files). Not chased (D6).

### P6 — Preflight

`preflight` recipe runs `check-records` then `markdownlint-docs` first.

### P7 — CI

Linux `go` job (`runs-on: ubuntu-latest`) runs both after the Node 24 assert. Comment 0180 / 0170 D3. Not on windows/Flutter/`ci-windows`. Rebase+push: see deviations.

### P8 — Closeout gates

`make check-records` exit 0, empty stderr (no numbering-debt warnings):

```text
==> records and docs links
```

`make markdownlint-docs` exit 0:

```text
Linting: 47 files
Summary: 0 issues in 0 files
```

`python3 scripts/check_records.py --next` prints `0054` (gap after 0054-PLAN moved to 0004).

No `docs/reports/*spike*`. Spike dirs:

```text
internal/provider/codex/testdata/codex-spike-0.145.0
internal/provider/kilo/testdata/kilo-spike-7.4.20
internal/provider/kilo/testdata/kilo-spike-7.4.22
internal/provider/kilo/testdata/kilo-spike-7.4.23
internal/provider/opencode/testdata/opencode-spike-1.18.5
```

`ls apps/mobile/docs/`: `README.md`, `architecture.md`, `decisions/`, `guides/`, `reports/`.

### A11 — scratch worktree proofs

Scratch git worktree of HEAD (system temp; removed after). Planted: `docs/decisions/0180-MADR-planted-duplicate.md`; `docs/9999-MADR-planted-outside.md`; `apps/mobile/docs/guides/0180-planted-broken.md` (`[dead](./no-such-file.md)`); 300-character spaced line at end of `docs/guides/config.md`. Working tree not modified.

`make check-records` (python `--check-all` exit 1; make exit 2):

```text
==> records and docs links
broken relative link: apps/mobile/docs/guides/0180-planted-broken.md:3: ./no-such-file.md
1 broken relative link(s)
```

stderr:

```text
number 0180 is claimed by 2 MADRs: docs/decisions/0180-MADR-check-records-is-a-preflight-and-ci-gate.md, docs/decisions/0180-MADR-planted-duplicate.md (a number is never reused)
record outside its directory: docs/9999-MADR-planted-outside.md (expected a path ending in docs/decisions/)
```

`python3 scripts/check_records.py --check` on the same plant (numbered records only; no unnumbered broken-link walk) exit 0 with the same two stderr warnings.

`make markdownlint-docs` exit 2:

```text
Linting: 48 files
Summary: 1 issue in 1 file
docs/guides/config.md:718:201 error MD013/line-length Line length [Expected: 200; Actual: 300]
```

### A12 — `git log --follow`

- `docs/decisions/0181-MADR-configurable-mcremote-message-size.md` — `a1bee687`, `4e2ac105` (renumber from 0154), `64282e29`, `9ac32229`, `8eccfafb` (original 0154-later add)
- `apps/mobile/docs/decisions/0018-MADR-mobile-chat-performance-action-plan.md` — `a1bee687` (P4 move), then `4e2ac105`, `ed21e326`, `8d5e363f`, `772c041e`, `64282e29`, `9ac32229`, …

### P9 — Event-type guard reads the paragraph (2026-10-04)

`TestEventTypesAreDocumented` now takes the paragraph from the `Event \`type\` values:` line through to the next blank line, joins it, and checks each `event.Types()` entry against that text. The `Fatalf` for a missing prefix is unchanged. The error text says "paragraph" instead of "line", and the `eventTypeListPrefix` comment cites D8. `docs/guides/protocol-v1.md` is untouched (`git diff --quiet` exit 0).

Before the change, on the four-line enumeration, `go test -count=1 -run TestEventTypesAreDocumented ./internal/protocol/` exited 1 with 25 "missing from the ... line" errors, matching CI.

A13, after the change, against the unmodified doc:

```text
$ go test -count=1 ./internal/protocol/      # exit 0
ok  	github.com/maccavelli/magic-cli-remote/internal/protocol	0.738s
```

A14 ran in a `git clone --local` of HEAD under the session scratchpad, with the uncommitted test copied in. Each plant used a fresh copy of the doc, and the clone was removed afterwards. This tree was not modified. The control run on the pristine doc exited 0.

(a) `` `codex_unsupported_item` `` deleted from the enumeration's last line, exit 1:

```text
doc_coverage_test.go:68: event type "codex_unsupported_item" is emitted but missing from the "Event `type` values:" paragraph in ../../docs/guides/protocol-v1.md
```

(b) A blank line inserted before the enumeration's last line, exit 1. Those three types are still in the document, so this shows the reader stops at the blank line:

```text
doc_coverage_test.go:68: event type "codex_model_verification" is emitted but missing from the "Event `type` values:" paragraph in ../../docs/guides/protocol-v1.md
doc_coverage_test.go:68: event type "codex_terminal_interaction" is emitted but missing from the "Event `type` values:" paragraph in ../../docs/guides/protocol-v1.md
doc_coverage_test.go:68: event type "codex_unsupported_item" is emitted but missing from the "Event `type` values:" paragraph in ../../docs/guides/protocol-v1.md
```

(c) Prefix reworded to `Event kinds:`, exit 1:

```text
doc_coverage_test.go:61: no line starting "Event `type` values:" in ../../docs/guides/protocol-v1.md — the enumeration this guard checks is gone or was reworded
```

Gates:

```text
$ make pre-add-check FILES=internal/protocol/doc_coverage_test.go   # exit 0
go-precheck: 1 file(s) clean (gofmt, golint, govulncheck).
$ make preflight                                                     # exit 0 (A15)
==> check-records ... markdownlint-docs ... Summary: 0 issues in 0 files
==> go test -race ... ok  github.com/maccavelli/magic-cli-remote/internal/protocol  3.221s
==> flutter analyze ... No issues found!
==> flutter test ... All tests passed!
✅ preflight passed
```

Zero `FAIL` lines in the preflight log. The three files are staged on `master`. A16 waits on the owner's commit and push.

### What was not done

* Whole-tree `npx markdownlint-cli2` (3126 issues). Out of scope.
* Windows Go matrix, Flutter jobs, `make ci-windows`. Out of scope.
* 0158/0175 historical spike-path prose. Left as written (D3).
* ~~MADR amendment: D1–D7 did not change.~~ Superseded 2026-10-04: D6's "mechanical fixes" assumption failed; D8 added.
* `make preflight` and `go test` were not run at P5 or P8. A9 was checked by reading the recipe. This is the gap D8 closes (added 2026-10-04).

### Acceptance

| # | Result |
| --- | --- |
| A1 | met — `make check-records` exit 0, empty stderr |
| A2 | met — one 0154 MADR (`mcrelay-paths`); configurable message-size pair is 0181 |
| A3 | met — `0004-PLAN-hardening-implementation.md`, `0055-MADR` and `0055-PLAN`; no `0054-PLAN-*` |
| A4 | met — `apps/mobile/docs/reports/0177-REPORT-flutter-android-client-assessment.md`; no `0177-PLAN-*` |
| A5 | met — no `docs/reports/*spike*`; five spike dirs under the three provider `testdata/` trees |
| A6 | met — `ls apps/mobile/docs/` is those five names |
| A7 | met — companion MADR/PLAN under `apps/mobile/docs/decisions/`; 0177 and 0178 under `apps/mobile/docs/reports/` |
| A8 | met — `make markdownlint-docs` 0; whole-tree 3126 issues in 50 files |
| A9 | met — `preflight` recipe invokes both (`Makefile` lines under `preflight:`) |
| A10 | met — linux `go` job runs both; Flutter / `windows-latest` / `make/ci-windows.mk` do not |
| A11 | met — scratch worktree output above |
| A12 | met — `--follow` on 0181 and on moved 0018 mobile MADR |
| A13 | met — `go test -count=1 ./internal/protocol/` exit 0 on the four-line enumeration |
| A14 | met — plants (a), (b), (c) each exit 1 with the expected message; control exit 0 |
| A15 | met — `make preflight` exit 0 on the staged P9 tree |
| A16 | pending — owner commits and pushes; then the CI Go jobs must be green |
