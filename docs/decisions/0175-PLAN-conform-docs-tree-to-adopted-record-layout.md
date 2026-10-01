---
status: proposed
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
3. Mark both 0158 records `status: superseded` with a dated note pointing at `0175-MADR-conform-docs-tree-to-adopted-record-layout.md`.
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

None yet. Any mid-execution finding follows the skill's protocol: a dated deviation entry here naming what was found and decided, an amendment to the MADR when a decision or asserted fact changed, and the deviation carried into the commit and the handoff.
