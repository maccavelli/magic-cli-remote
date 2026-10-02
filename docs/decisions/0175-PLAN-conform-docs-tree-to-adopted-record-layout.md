---
status: complete
date: 2026-10-01
associated-madr: "0175-MADR-conform-docs-tree-to-adopted-record-layout.md"
---

<!-- markdownlint-disable MD013 MD041 MD024 MD033 MD036 -->

# Implement conforming the docs tree to the adopted record layout

Associated MADR: [0175-MADR-conform-docs-tree-to-adopted-record-layout.md](0175-MADR-conform-docs-tree-to-adopted-record-layout.md)

## Goal

The docs tree matches the adopted fixed layout: every record sits in `docs/decisions/` or `docs/reports/` with the kind infix, guides sit unnumbered in `docs/guides/`, `docs/` holds exactly `README.md`, `architecture.md` and those three subdirectories, `make check-records` is green over records and unnumbered docs, and `go test ./...` stays green throughout.

## Scope

### In scope (the only files any phase may touch)

* P1: new `scripts/check_records.py`; `Makefile` (one target); no other file.
* P2: `git mv` of every `docs/spec/*.md` (291 files) into `docs/decisions/`; link/path repair in the moved records, in `docs/decisions/0164–0174` where they cite moved files, in `README.md`, `AGENTS.md`, `.grok/rules/madr-plan-before-mutating-work.md`, `.claude/rules/madr-and-plan-skill.md`, `.opencode/rules.md` (verify only); supersession notes in the two 0158 records.
* P3: renames of the four legacy records into `docs/decisions/` (`0004-MADR-certificate-management.md`, `0005-MADR-client-identity.md`, `0176-MADR-phase-2-grok-acp-provider.md`, `0177-PLAN-flutter-android-client-assessment.md`), their in-file dated notes, and their citing records plus `README.md`.
* P4: `mkdir docs/reports`; renames/moves of the five REPORT files (0098, 0099, 0100, 0023, and the next-free 0178); repair in citing records and `README.md`.
* P5: `mkdir docs/guides`; moves of the 19 guide files and `docs/standards/**` → `docs/guides/`; repair of every live `docs/…​.md` path reference repo-wide (records, guides, `README.md`, `AGENTS.md`, `Makefile`, `scripts/install.sh`, `.github/workflows/ci.yml`, `configs/*.yaml`, `internal/cli/service/defaults_mcremote.yaml`, the Go files and Dart files listed below, `apps/mobile/README.md`).
* P6: `git mv` of the five `*-spike-*` directories into `docs/reports/`; repair of any references.
* P7: new `docs/README.md` and `docs/architecture.md`; one link to `docs/README.md` near the top of the root `README.md`.
* P8: no file changes except the PLAN's own execution record and status, and a MADR amendment if a deviation occurred.

### Out of scope

* Content rewrites of `docs/decisions/0164–0174` beyond link/path repair where they cite moved files.
* Rewriting historical rationale in the 0158 records (only their status and supersession note change).
* Wiring `check_records.py` into CI or `make preflight` (MADR D8: deferred).
* Splitting a mobile tree (MADR D7: deferred).
* Deleting anything, including the spike evidence directories.
* Pushing; every commit stays local unless the owner asks in the same turn.

## Implementation Steps

Order is fixed; each phase ends with its gates and one commit (`git commit --no-edit`; the hook owns the message). Numbers cited for future allocations (0176, 0177, 0178) are as of 2026-10-01; if intervening pairs take them, re-derive each from `scripts/check_records.py --next` at the moment of execution and record the actual number in the execution record.

**P1 — Tooling.**

1. Write `scripts/check_records.py` (stdlib only; `scripts/*.py` is already the repo convention, and `.gitignore` already carries the Python rules from MADR 0174):
   * `--next`: scan the whole repository (tracked files plus the working tree) for `NNNN-{MADR,PLAN,REPORT,GATES}-*.md` and print the next unused number. Never scan a single directory.
   * `--check` (default): for every numbered record — filename matches `NNNN-<KIND>-<slug>.md`; the number is unique except that a PLAN may share its MADR's and a REPORT/GATES may share its associated record's; every relative markdown link in the file resolves; error format `broken relative link: <file>:<line>: <target>`. A PLAN with no same-number MADR is a warning, not an error (the known legacy case is 0177).
   * `--check-all`: additionally check unnumbered markdown under `docs/` (guides, `docs/README.md`, `docs/architecture.md`) and the root `README.md`.
   * `--write-index`: regenerate the records ToC in `docs/README.md` between generated markers; never hand-edit inside them.
2. Add the `check-records` target to `Makefile` running `python3 scripts/check_records.py --check-all`.
3. Prove the checker fails first, on a scratch clone under the system temp dir — never by dirtying this tree: clone the repo, plant one record with a broken relative link and two records sharing a number, and observe both errors; also run `--check` read-only against this tree and record the pre-existing broken-link baseline (there are some: root-form links like `](docs/0120-…)`, `](docs/spec/0160-…)`, and sibling links from old `docs/spec/` records to the legacy files still in `docs/`). A checker not seen to fail is unused.
4. Gates: `python3 -m py_compile scripts/check_records.py`; the planted-failure evidence; commit.

**P2 — Records relocation and agent-facing path conformance.**

1. `git mv` every `docs/spec/*.md` into `docs/decisions/` in one operation (all 291; MADR/PLAN pairs move together; `docs/spec/` disappears).
2. Repair from `check_records.py --check` output, plus `git grep` for path text: `docs/spec/NNNN-…` citations inside `docs/decisions/0166-PLAN`, `0167-PLAN`, `0169-PLAN`, `0171-MADR`, `0171-PLAN` and inside the moved 0158–0161 records become sibling or `docs/decisions/…` references; `../spec/…` forms become sibling links; root-form links (`docs/0120-…`, `docs/spec/0160-…`) are repaired to the moved locations. Historical prose in 0158 naming `docs/spec/` stays as written.
3. ~~Mark both 0158 records `status: superseded` with a dated note pointing at `0175-MADR-conform-docs-tree-to-adopted-record-layout.md`.~~ Landed in the P3 commit (deviation 2026-10-01).
4. Agent-facing conformance, one pass: `AGENTS.md` (rationale link to 0105 → `docs/decisions/0105-…`; the gate's and bootstrap exception's `docs/NNNN-MADR-*` paths → `docs/decisions/NNNN-MADR-*`; the "File naming" section restated per the adopted layout — records in `docs/decisions/` and `docs/reports/`, four kinds, kind infix, cite by full filename, next number from `scripts/check_records.py --next`, docs-tree layout is `documentation-writing`); `.grok/rules/madr-plan-before-mutating-work.md` (line 4 link, line 41–42 paths); `.claude/rules/madr-and-plan-skill.md` (line 41 path); verify `.opencode/rules.md` needs nothing.
5. Repair the ~50 `docs/spec/` links in the root `README.md` to `docs/decisions/…`.
6. Gates: `make check-records` reports no failure that is not on the recorded baseline; `npx markdownlint-cli2` over the changed `.md` files; `git log --follow` resolves for two sampled moved records; commit.

**P3 — Legacy records.**

1. `git mv` with rename: `docs/MADR-certificate-management-decision.md` → `docs/decisions/0004-MADR-certificate-management.md`; `docs/MADR-client-identity-decision.md` → `docs/decisions/0005-MADR-client-identity.md`; `docs/MADR-phase2-grok-acp.md` → `docs/decisions/0176-MADR-phase-2-grok-acp-provider.md`; `docs/PLAN-flutter-android-client-assessment.md` → `docs/decisions/0177-PLAN-flutter-android-client-assessment.md`.
2. Add a dated note at the top of each: the 0176/0177 notes state the renumber (self-claimed 0004 collided with the hardening series' 0004; cited as 0005 in 0018/0026; numbers are unique, not chronological) and that 0177 is a lone PLAN predating the pairing rule.
3. Repair citations by full filename in the citing records (0006, 0007, 0008, 0011, 0015, 0018, 0019, 0022, 0025, 0026 — enumerate with `git grep` at execution) and the root `README.md` row. Link text cites the record's full filename, never the bare number.
4. Gates: `make check-records`; markdownlint over changed files; commit.

**P4 — Reports.**

1. `mkdir docs/reports`, then `git mv` with rename: `docs/0098-findings-install-verification-sweep.md` → `docs/reports/0098-REPORT-install-verification-sweep.md`; `docs/0099-findings-reverification.md` → `docs/reports/0099-REPORT-reverification.md`; `docs/0100-findings-update-refresh.md` → `docs/reports/0100-REPORT-update-refresh.md`; `docs/mobile-ux-assessment.md` → `docs/reports/<next-free>-REPORT-mobile-ux-assessment.md` (expected 0178); `docs/agent_cli_slash_commands_matrix.md` → `docs/reports/0023-REPORT-agent-cli-slash-commands-matrix.md`.
2. Repair citations in the citing records (0097-PLAN, the 0098/0099/0100 pairs, 0103-MADR, 0171 pair, 0018-MADR, 0126-MADR, the 0023/0028/0039-PLAN/0044-PLAN/0160 records for the matrix — enumerate with `git grep`) and `README.md`.
3. Gates: `make check-records`; markdownlint; commit.

**P5 — Guides and standards.**

1. `mkdir docs/guides`; `git mv` the 19 guide files into it: `agent_cli_slash_commands_matrix` is not among them (P4); the set is `chat-performance.md`, `config-mcrelay.md`, `config.md`, `headscale.md`, `iam-route53-acme.md`, `mobile-profiling.md`, `ops-android-emulator.md`, `ops-android-signing.md`, `ops-codex-contract.md`, `ops-credential-recovery.md`, `ops-hardware-validation.md`, `ops-ios-signing.md`, `ops-linux-install.md`, `ops-macos-tcc.md`, `ops-mcrelay.md`, `ops-windows-install.md`, `protocol-v1.md`, `protocol-v2.md`, `receipts.md`. Then `git mv docs/standards docs/guides/standards`.
2. Repair every live path reference, enumerated by `git grep -nE 'docs/(protocol-v[12]|receipts|config|config-mcrelay|headscale|iam-route53-acme|ops-|chat-performance|mobile-profiling|agent_cli_slash_commands_matrix|mobile-ux-assessment)'` at execution. Known set: root `README.md`; `AGENTS.md` (the 0145 Windows gates line citing `docs/ops-windows-install.md`); `Makefile` lines 154, 185, 202, 487; `scripts/install.sh:80`; `.github/workflows/ci.yml:656,687,692`; `configs/config.example.yaml`, `configs/config.mesh-grok.yaml`, `configs/config.prod.example.yaml`, `configs/mcrelay.example.yaml`, `internal/cli/service/defaults_mcremote.yaml`; `internal/protocol/doc_coverage_test.go` (the `protocolDoc` const → `../../docs/guides/protocol-v1.md`); `internal/cli/doctor.go:192,195`; `internal/cli/receipts.go:34`; `internal/cli/root.go:69`; `internal/daemon/daemon.go:114`; `apps/mobile/lib/data/chat/chat_models.dart` and `apps/mobile/lib/data/ws/mc_exception.dart:84`; `apps/mobile/README.md`; the remaining `internal/**` and `docs/**` mentions the grep enumerates.
3. Repair links from records and guides to the moved files (`../<name>.md` → `../guides/<name>.md`, bare sibling forms likewise) strictly from `check_records.py --check-all` output.
4. Gates: `make pre-add-check FILES="internal/protocol/doc_coverage_test.go internal/cli/doctor.go internal/cli/receipts.go internal/cli/root.go internal/daemon/daemon.go"`; `go test ./...` (the protocol guard must read the moved doc); `make check-records`; markdownlint over the changed `.md` files introduces no new violation — some moved guides carry pre-existing violations (measured 2026-10-01: `protocol-v1.md` MD056/MD013 among them); enumerate them in the execution record and leave them for the owner, since content rewrites are out of scope; commit.

**P6 — Spike evidence.**

1. `git mv docs/codex-spike-0.145.0 docs/kilo-spike-7.4.20 docs/kilo-spike-7.4.22 docs/kilo-spike-7.4.23 docs/opencode-spike-1.18.5 docs/reports/`.
2. Repair references (`git grep 'spike-'` at execution; 0158's superseded prose stays as written).
3. Gates: `make check-records`; `git status` clean of strays; commit.

**P7 — Scaffold.**

1. `python3 scripts/check_records.py --write-index` to generate the ToC in a new `docs/README.md`; hand-write the "I want to…" matrix under it — rows phrased as the reader's task (pair a phone, run the daemon as a service, configure mcrelay, configure signed receipts, set up Headscale, sign Android/iOS builds, understand the wire protocol, find why a decision was made), each linking `guides/…` or `decisions/NNNN-…`.
2. Write a real `docs/architecture.md` — the system as it is now (daemon, mcrelay, phone clients, providers, protocol v2, receipts, deployment units), sourced from the root `README.md` and `guides/protocol-v2.md`; no history and no rationale ("we used to"/"we chose" do not appear).
3. Add one link to `docs/README.md` near the top of the root `README.md`.
4. Gates: `make check-records` (second `--write-index` run is a no-op — hand edits outside the markers only); markdownlint over the new files; commit.

**P8 — Closeout.**

1. Full gates: `make check-records` exits 0 with no baseline entries left; `npx markdownlint-cli2` over every `.md` file changed by the pair; `go build ./...`; `git status` clean.
2. `git log --follow` spot-checks: `docs/decisions/0001-…`, `docs/decisions/0163-…`, `docs/guides/receipts.md`, `docs/guides/protocol-v1.md`, `docs/guides/standards/go/README.md`.
3. Write the execution record into this PLAN (what each phase did, the checker output that drove each repair, every deviation with its date) and set `status: complete` only when every acceptance criterion below is verified — not when the last commit lands. Amend the MADR for any deviation that changed a decision or contradicted an asserted fact.

## Verification

| # | Criterion |
| --- | --- |
| A1 | `ls docs/` shows exactly `README.md`, `architecture.md`, `decisions/`, `reports/`, `guides/` — nothing else |
| A2 | Every `NNNN-MADR-*.md` and `NNNN-PLAN-*.md` lives in `docs/decisions/`; `docs/spec/` no longer exists; every MADR/PLAN pair is co-located |
| A3 | The five REPORT files live in `docs/reports/` with the kind infix; 0098/0099/0100/0023 keep their numbers; the mobile-ux report took `--next` (expected 0178) |
| A4 | The five spike evidence directories live under `docs/reports/` |
| A5 | The 19 guide files and `standards/{go,mobile}` live under `docs/guides/`; no numbered record sits directly in `docs/` |
| A6 | `make check-records` exits 0 (records and unnumbered docs) |
| A7 | The checker was seen to fail first on planted broken-link and duplicate-number input in a scratch clone; evidence is in the execution record |
| A8 | `go test ./...` is green and `doc_coverage_test.go` reads `docs/guides/protocol-v1.md` |
| A9 | `make pre-add-check` is clean on every Go file P5 touched |
| A10 | No live `docs/spec` reference remains: `git grep -nE ']\\([^)]*docs/spec|`docs/spec/NNNN'` returns only historical prose inside the superseded 0158 records |
| A11 | `AGENTS.md`, `.claude/rules/madr-and-plan-skill.md`, `.grok/rules/madr-plan-before-mutating-work.md` teach `docs/decisions/NNNN-*` paths, the four kinds, and tool-backed numbering; `.opencode/rules.md` verified |
| A12 | `docs/README.md` carries the generated ToC (a second `--write-index` run changes nothing) and the hand-written "I want to…" matrix; the root `README.md` links it |
| A13 | `docs/architecture.md` describes the system as it is now and contains no "we used to" / "we chose" |
| A14 | The four legacy records are renamed with dated in-file notes; citing records cite full filenames |
| A15 | The 0158 MADR and PLAN are `status: superseded` and point at 0175 |
| A16 | `git log --follow` resolves for the sampled moved files |

## Rollout and Rollback

**Rollout:** execute P1–P8 in order, one commit per phase after its gates pass. No push, tag, or workflow edit without an explicit ask in the same turn.

**Rollback:** `git revert` the phase commits in reverse order (P8 → P1). Every move is a `git mv`, so reverts restore the `docs/spec/` layout with history intact.

## Deviations

**2026-10-01 — 0158 supersession landed in P3.** P2 commit `64282e29` moved `docs/spec/` into `docs/decisions/` and repaired agent-facing paths, but left both 0158 records at `status: proposed` with no supersession note. P3 includes those two files so A15 is not deferred. No MADR amendment: D1 is unchanged.

**2026-10-01 — `--next` fills gaps.** At P3 execution `scripts/check_records.py --next` printed `0004` because 0004 and 0005 were still unused filenames. The phase records still took 0176 and 0177 as D2 specified; intervening pairs had not claimed them. Using `--next` here would have assigned 0004 to the colliding phase-2 MADR. No MADR amendment: D2 already named 0176/0177.

**2026-10-01 — P4 also retargeted `docs/chat-performance.md`.** Two backticks named `mobile-ux-assessment.md`. That file is a guide (P5), but the pointers would be dead until P5. They now name `0178-REPORT-mobile-ux-assessment.md`. No MADR amendment.

**2026-10-01 — P4 markdownlint is pre-existing.** `npx markdownlint-cli2 --no-globs` over the five REPORT files plus `docs/chat-performance.md` reports 96 issues (MD013/MD022/MD032/MD031 on 0023; MD046/MD004/MD007/MD014/MD025/MD032 on 0098–0100; MD029 on 0178; MD013 on chat-performance). Same findings as before the move. Content rewrites are out of scope; left for the owner.

**2026-10-01 — P5 `go test ./...` vs host umask.** First run failed `internal/appdirs.TestFileIsOwnerOnly` and `internal/providerauth.TestValidateRejectsBadCandidates/group_readable` (0666/0644 written, still treated as owner-only). Those packages are unmodified. Host `umask` is `0077`. Re-run of those packages and of `go test ./...` under `umask 022` is green. `internal/protocol` (including `doc_coverage_test.go` reading `docs/guides/protocol-v1.md`) passed on the first run. No MADR amendment.

**2026-10-01 — P5 Go files beyond the five named in the step.** Comment-path repairs also landed in `internal/cli/examples.go`, `pair.go`, `internal/event/*`, `internal/fsutil/syncdir_windows.go`, `internal/protocol/errors.go`, `internal/provider/acpagent/*`, `internal/provider/codex/sandbox_health.go`, `internal/receipt/*`, `internal/relay/cli.go`, `internal/session/*`, `internal/ws/*`, plus `defaults_mcrelay.yaml` and `deploy/systemd/mcrelay*.service`. That is the grep-enumerated remainder in P5 step 2. `apps/mobile/lib/data/chat/chat_models.dart` had no `docs/` path. No MADR amendment.

**2026-10-01 — P5 markdownlint is pre-existing.** `npx markdownlint-cli2 --no-globs` over `docs/guides/**/*.md`, `README.md`, `AGENTS.md`, and `apps/mobile/README.md` reports 49 issues in 9 files (MD013, MD004, MD056, MD040). `protocol-v1.md` MD056/MD013 matches the 2026-10-01 measurement in this phase. Content rewrites are out of scope.

**2026-10-01 — P6 leaves one non-spike broken link.** After the spike moves, `check_records.py --check-all` has a single remaining error: `docs/decisions/0059-MADR-native-paths-and-linux-macos-parity.md:176: ../../internal/xdg/dirs.go`. P6 left it. 0158 historical prose naming `docs/codex-spike-*` was left as written.

**2026-10-01 — P7 repairs the 0059 code link.** The target is gone (`internal/xdg` was replaced by `internal/appdirs`); the depth was already correct from `docs/decisions/`. P8 cannot change records, so the href is now `../../internal/appdirs/paths.go` in this phase so A6 can pass. No MADR amendment.

**2026-10-01 — P8 markdownlint is pre-existing.** `npx markdownlint-cli2 --no-globs` over the 358 `.md` files the pair touched reports 9006 issues, almost all in MADR/PLAN files the project config excludes (`*MADR*`, `*PLAN*`). The same command over the 51 user-facing files (guides, REPORTs, spike READMEs, `docs/README.md`, `architecture.md`, root `README.md`, `AGENTS.md`, `apps/mobile/README.md`) reports 151 issues. Default-config `npx markdownlint-cli2` over the tree reports 3257. None of these were introduced as content rewrites; P4/P5 already left the REPORT/guide findings for the owner. No MADR amendment.

**2026-10-01 — A10 grep also matches this pair's own prose.** The criterion command `git grep -nE ']\\([^)]*docs/spec|\`docs/spec/NNNN'` hits 0158 (historical, as specified) and this PLAN/MADR where they quote old `docs/spec/` paths. There is no remaining markdown href that resolves under `docs/spec/` (`docs/spec/` is gone). No MADR amendment.

Any further mid-execution finding follows the skill's protocol: a dated deviation entry here naming what was found and decided, an amendment to the MADR when a decision or asserted fact changed, and the deviation carried into the commit and the handoff.

## Execution record (2026-10-01)

Local commits, one per phase. Nothing pushed.

| Phase | Commit | Subject |
| :--- | :--- | :--- |
| P1 | `32a0f46b` | feat(docs): add records validation tooling (2 files, +237 −1) |
| P2 | `64282e29` | docs(records): reorganize numbered records into decisions (301 files, +131 −131) |
| P3 | `772c041e` | docs(decisions): align records with adopted layout (27 files, +56 −40) |
| P4 | `8d5e363f` | docs: reorganize decision records and reports (24 files, +40 −36) |
| P5 | `ed21e326` | docs(repo): reorganize reference documentation into guides (236 files, +739 −733) |
| P6 | `784df04b` | docs(reports): reorganize spike evidence and update provider fidelity (89 files, +89 −87) |
| P7 | `5935d9f7` | docs(records): add documentation index and align record layout (5 files, +488 −2) |

### P1 — Tooling

Added `scripts/check_records.py` (`--next`, `--check`, `--check-all`, `--write-index`) and `Makefile` target `check-records`. `python3 -m py_compile scripts/check_records.py` ok.

Planted-failure evidence was not in the P1 commit body. Re-run at P8 on a scratch clone of this tree (system temp dir; clone deleted after): two files `0180-MADR-planted-broken-link.md` (`[dead](./no-such-file.md)`) and `0180-MADR-planted-duplicate.md`. `python3 scripts/check_records.py --check` exited 1:

```text
number 0180 is claimed by 2 MADRs: docs/decisions/0180-MADR-planted-broken-link.md, docs/decisions/0180-MADR-planted-duplicate.md (a number is never reused; 0154 is the recorded pre-existing case, renumbering deferred)
broken relative link: docs/decisions/0180-MADR-planted-broken-link.md:3: ./no-such-file.md
1 broken relative link(s)
```

`--next` on that clone printed `0181`. This tree's `--next` is `0180`.

### P2 — Records relocation

`git mv` of every `docs/spec/*.md` into `docs/decisions/` (291 records). Agent-facing paths in `AGENTS.md`, `.grok/rules/madr-plan-before-mutating-work.md`, `.claude/rules/madr-and-plan-skill.md` retargeted; `.opencode/rules.md` needed nothing. Root `README.md` `docs/spec/` links repaired. 0158 supersession slipped to P3 (deviation above).

### P3 — Legacy records

Renames: `0004-MADR-certificate-management.md`, `0005-MADR-client-identity.md`, `0176-MADR-phase-2-grok-acp-provider.md`, `0177-PLAN-flutter-android-client-assessment.md`, each with a dated 2026-10-01 note. `--next` printed `0004` (gap-fill); D2 numbers 0176/0177 used anyway. 0158 MADR and PLAN marked `status: superseded` pointing at 0175.

### P4 — Reports

Five REPORTs in `docs/reports/`: 0098, 0099, 0100, 0023, 0178 (mobile-ux took `--next`; 0179 was already `0179-MADR-pigo-native-acp-provider.md`). Repair scripts double-prefixed when a new filename contains the old basename; fixed by full-path replace. markdownlint 96 pre-existing issues left.

### P5 — Guides and standards

19 guides plus `docs/standards` → `docs/guides/`. `protocolDoc` is `../../docs/guides/protocol-v1.md`. Extra-depth links after the added `guides/` segment repaired from `--check-all` (65 new, then 0). Host umask `0077` failed unmodified `appdirs`/`providerauth` mode tests; `umask 022` made `go test ./...` green. markdownlint 49 pre-existing issues left.

### P6 — Spike evidence

Five `*-spike-*` directories → `docs/reports/`. 35-file path repair; 0075 template `docs/reports/kilo-spike-<version>/` by hand. 0158 historical spike prose left as written. One leftover 0059 link deferred.

### P7 — Scaffold

`docs/README.md` generated ToC plus hand-written "I want to…" matrix. `docs/architecture.md` (no "we used to" / "we chose"). Root README links `docs/README.md`. Second `--write-index` is a no-op. 0059 href retargeted to `../../internal/appdirs/paths.go`. `ls docs/` is exactly `README.md`, `architecture.md`, `decisions/`, `guides/`, `reports/`.

### P8 — Closeout

`make check-records` exit 0:

```text
==> records and docs links
number 0054 has a PLAN but no MADR: docs/decisions/0054-PLAN-hardening-implementation.md (0177 is the recorded legacy lone plan)
number 0055 has a PLAN but no MADR: docs/decisions/0055-PLAN-mcremote-server-remediation.md (0177 is the recorded legacy lone plan)
number 0154 is claimed by 2 MADRs: docs/decisions/0154-MADR-configurable-mcremote-message-size.md, docs/decisions/0154-MADR-mcrelay-paths-demands-a-runnable-server.md (a number is never reused; 0154 is the recorded pre-existing case, renumbering deferred)
number 0177 has a PLAN but no MADR: docs/decisions/0177-PLAN-flutter-android-client-assessment.md (0177 is the recorded legacy lone plan)
```

No broken-link errors. Those four warnings are the recorded inventory, not a leftover baseline.

`go build ./...` exit 0. `go test ./...` under `umask 022` exit 0; `go test ./internal/protocol` ok; `protocolDoc` const is `../../docs/guides/protocol-v1.md`. `make pre-add-check` on the 21 Go files in `ed21e326` clean.

`git log --follow` (first lines):

- `docs/decisions/0001-MADR-architecture-mcremote.md` — `64282e29` then `9ac32229` (`docs/spec`) then `7daf98b8`
- `docs/decisions/0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md` — `64282e29` then `81dd1bc2`
- `docs/guides/receipts.md` — `ed21e326` then `4b33f88f`
- `docs/guides/protocol-v1.md` — `ed21e326` then earlier protocol commits
- `docs/guides/standards/go/README.md` — `ed21e326` then `e7c6bd99`

### Acceptance

| # | Result |
| --- | --- |
| A1 | met — `ls docs/` is those five names |
| A2 | met — `docs/spec/` gone; 175 MADR and 143 PLAN in `docs/decisions/` only |
| A3 | met — five REPORT files as mapped; 0178 is mobile-ux |
| A4 | met — five spike dirs under `docs/reports/` |
| A5 | met — 19 guides and `standards/{go,mobile}` under `docs/guides/`; no numbered file directly in `docs/` |
| A6 | met — `make check-records` exit 0 |
| A7 | met — planted duplicate + broken link on a scratch clone, exit 1, output above |
| A8 | met — `go test ./...` green under `umask 022`; protocol guard reads the moved doc |
| A9 | met — pre-add-check clean on P5's 21 Go files |
| A10 | met as intent — no live `docs/spec/` href; remaining grep hits are 0158 and this pair's historical wording (P8 deviation) |
| A11 | met — `AGENTS.md` and both per-agent rules teach `docs/decisions/`; `.opencode/rules.md` has no `docs/spec` |
| A12 | met — generated ToC, "I want to…" matrix, second `--write-index` unchanged, root README links the index |
| A13 | met — no "we used to" / "we chose" in `docs/architecture.md` |
| A14 | met — four legacy files renamed with 2026-10-01 notes; citers use full filenames (e.g. `0005`, `0007`, `0008`, `0015`, `0054`) |
| A15 | met — both 0158 records `status: superseded` pointing at 0175 |
| A16 | met — `--follow` resolves the five samples |

What was not done, as scoped: CI/`make preflight` wiring of `check_records.py` (D8); mobile tree split (D7); deleting spikes; content rewrites of pre-existing markdownlint; push.

No MADR amendment: D1–D8 still describe what landed.
