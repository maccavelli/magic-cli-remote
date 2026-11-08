---
status: in-progress
date: 2026-11-08
associated-madr: "0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the November 2026 audit of the open records and the guides

Associated MADR: [0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md](0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md)

## Goal

When this plan is done:

* each of the 51 open records either carries a corrected status with a dated note and its evidence, or is listed as
  checked and accurate (D1);
* the 32 `completed` PLANs read `complete` (D2; the owner answered Q1 "Normalize to complete");
* each of the 24 unnumbered docs has been checked against the tree, and its stale facts replaced (D4, D5);
* the records index is regenerated and every relative link in the unnumbered docs resolves (D6);
* `0239-REPORT-record-status-and-docs-accuracy-audit.md` records all of it (D7).

## Scope

* **Records:** the 51 open MADRs and PLANs the MADR lists, in `docs/decisions/` and `apps/mobile/docs/decisions/`;
  the 32 `completed` PLANs (P2 only).
* **Unnumbered docs:** `README.md`; `docs/README.md`, `docs/architecture.md`, `docs/guides/*.md`;
  `apps/mobile/docs/README.md`, `architecture.md`, `guides/*.md`. *Widened by P3 Deviation 2:*
  `docs/guides/standards/go/*.md` (P4), `apps/mobile/docs/guides/standards/mobile/*.md` and `apps/mobile/README.md`
  (P6).
* **New:** `docs/reports/0239-REPORT-record-status-and-docs-accuracy-audit.md`; this pair.
* **Out of scope (D8):** any source, test, config or workflow file; `AGENTS.md`, `.claude/rules/`, `.grok/rules/`,
  `.opencode/rules.md`, whose factual errors are listed for the owner; the 149 records without front matter, except
  where P1 changes one (D3); REPORT and GATES records, unless one is cited as open.

## Constraints

* **C1. Start state.** Each phase starts on a clean `master` equal to `origin/master`, or names what differs.
* **C2. Read-only evidence.** `grep`, `git log`, `git log -S`, `gh run view`, and reading files. No test is run for
  the audit, and no provider quota is spent. A fact that needs a live probe is listed as unverified.
* **C3. Scripts** (the inventory, the reference checker, the link resolver) live in the session scratchpad, are Python,
  and are not committed. Each check script is seen failing on a planted bad input before its results are used.
* **C4. History.** A record's earlier text is never rewritten (0200's rules, MADR D1). A guide's stale fact is
  replaced (MADR D5).
* **C5. Helpers.** At most one read-only helper agent at a time, for evidence gathering only. Its findings are
  spot-checked against the cited lines before any edit, and its notes stay in the scratchpad.
* **C6. Identifiers.** No hostname, account name or real-machine path in any edit. The disclosure guard's redact
  preview runs before each staging.
* **C7. Commits.** The agent stages at the end of each phase; the owner commits with `git commit --no-edit`.
* **C8. Deviations.** A record whose correct status needs an owner decision beyond Q1 to Q3 stops for that decision,
  batched at the end of P1's triage. Anything else unexpected is a dated deviation in this plan.

## Implementation Steps

### P1 — The open records (D1)

1. **Triage.** For each of the 51 records, read its status, its last execution entry and "Not done" list, and check
   each remaining item against the code, git history, citing records and CI (C2). The output is a scratchpad table:
   record, current status, proposed status, evidence, and whether it needs the owner.
2. **The owner's batch.** Records whose status cannot follow from 0200's rules and Q2 go to the owner in one list,
   with the evidence and a recommendation each.
3. **The edits,** in 0200's form: `status` and `date` in the front matter, and a closing "Status note — <date>"
   section that names the evidence and links to the report. No earlier text changes.
4. **Checks:** `make check-records`; a script confirms each edited record still has exactly one front-matter
   `status`, and that every note links to the report; the redact preview.
5. **Stage**, with the report's records section drafted. The owner commits.

### P2 — The `completed` spelling (D2; Q1 answered yes)

1. A script replaces `status: completed` with `status: complete` in the 32 PLANs' front matter, asserting each file
   has exactly one match before it writes, and nothing else changes (`git diff --stat` shows one line per file).
2. `make check-records`. **Stage.**

### P3 — The entry points: `README.md`, `docs/README.md`, both `architecture.md` files (D4, D6)

1. **The reference checker** (C3): extract and resolve each doc's paths, `make` targets, CLI subcommands and flags,
   config keys, environment variables, backticked Go identifiers, record links and versions. Seen failing on a scratch
   copy of one doc with one planted bad reference of each kind.
2. **The link resolver** (C3): every relative link in the unnumbered docs resolves to an existing file and, where it
   names one, an existing heading anchor. Seen failing on a planted dead link and a planted dead anchor. *Narrowed
   by P3 Deviation 1 to anchors: file existence is already `make check-records`' job.*
3. **Corrections,** from the checker's unresolved list, read in context, and a read-through against the records
   changed since each doc's last edit. `architecture.md` keeps no history. Where a doc describes behaviour the code
   does not have, the doc is rewritten to what the code does, and the report's doc-versus-code list gains the place,
   the old text's claim and the code that decided it (Q3, MADR D5).
4. **The index:** `python3 scripts/check_records.py --write-index`; the "I want to…" rows checked against the docs they
   name.
5. Checks: the checker and resolver re-run clean on these files, `make check-records`, markdown lint, the redact
   preview. **Stage.**

### P4 — Install, configuration and operations guides (D4)

`config.md`, `config-mcrelay.md`, `ops-linux-install.md`, `ops-windows-install.md`, `ops-macos-tcc.md`,
`ops-mcrelay.md`, `ops-credential-recovery.md`, `ops-hardware-validation.md`, `headscale.md`,
`iam-route53-acme.md`, `ops-codex-contract.md`.

1. P3 step 3's method, per guide. Config keys are checked against the provider sections and `internal/config`;
   defaults against the code that sets them; install commands and flags against the installers and `--help` text
   in source.
2. Checks as P3 step 5. **Stage.**

### P5 — Protocol and client guides (D4)

`protocol-v1.md`, `protocol-v2.md`, `receipts.md`, `chat-performance.md`.

1. P3 step 3's method. Message types, payload fields and error codes are checked against `internal/protocol` and the
   handlers in `internal/ws`; limits and caps against the code that sets them.
2. Checks as P3 step 5. **Stage.**

### P6 — The mobile tree's guides (D4)

`apps/mobile/docs/guides/*.md`.

1. P3 step 3's method, against `apps/mobile`, its build files and the workflows.
2. Checks as P3 step 5. **Stage.**

### P7 — The report and close-out (D7)

1. **The report:** method, rules (0200's, with Q1 to Q3's answers), every correction with its evidence, the checker's
   per-doc results, what was checked and left, and findings for the owner (D8). It is linked from every P1 note.
2. **Confirmation 1 to 6,** each with its evidence, in this plan's execution record.
3. `check_records.py --write-index`, `make check-records`, markdown lint, the redact preview. This plan becomes
   `complete` and the MADR `accepted` once the owner has committed every phase.
4. **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 every open record in the report | P1, P7 | the triage table; the report's records section |
| 2 no `proposed` MADR with a `complete` PLAN, unless the report says why | P1 | a script over the front matter |
| 3 each unnumbered doc in the report | P3 to P7 | the report's docs section |
| 4 the checker and resolver seen failing | P3 | steps 1 and 2 |
| 5 records check, lint, redact preview | every phase | each phase's checks |
| 6 nothing outside the docs and records changed | every phase | `git diff --cached --stat` at each staging |

## Rollout and Rollback

* One commit per phase, staged for the owner. A phase reverts on its own; P7's report would then describe a
  correction that was reverted, so the report is written last.
* Nothing ships to users except the guides' text, at the next release.

## Execution record

The owner answered the MADR's Q1 to Q3 on 2026-11-08 (normalize to `complete`; keep 0200's inferred acceptance
rule; fix a guide to match the code), and approved the plan: "committed, proceed" (records commit `cb68b744`).

### P1 — 2026-11-08: the open records

Scripts and logs are in the session scratchpad (`tidy_inventory.py`, `p1_triage_gather.py`, `p1_apply.py`,
`p1_apply_0130.py`, `p1_check.py`, `p1_check_failfirst.py`, `p1-triage-evidence.md`).

* **Start state (C1).** Clean tree. `master` was two commits ahead of `origin/master`: the owner's 0179 amendment
  (`c87d22e4`) and this pair (`cb68b744`), both unpushed.
* **Step 1, triage.** 53 open files: the MADR's 51 plus this pair. One read-only helper pass (C5) checked eighteen
  records; its claims were spot-checked against the files (the report lists which). The rest were checked
  directly.
* **Step 2, the owner's batch,** answered 2026-11-08:
  * 0208-MADR: "Accept it now".
  * 0217-PLAN: the dotfiles commit is "Not yet" done.
  * 0089-MADR: "D1 superseded by 0126".
  * 0227: the owner asked for the test procedure. It was given (the PLAN's and MADR's manual check, in a fresh
    session in each tool), and both records wait for the result.
* **Step 3, the edits,** in 0200's form, eleven records:
  * to `accepted`: 0211-MADR, 0090-MADR (inferred), 0208-MADR, 0130-MADR (inferred);
  * to `complete`: 0203-PLAN;
  * status kept, corrected by a note: 0089-MADR (D1 superseded in part), 0090-PLAN, 0143-PLAN, 0163-PLAN, 0169-PLAN,
    0217-PLAN.
* **Step 4, checks.**
  * `p1_check.py` was seen failing first, on a scratch copy of both record trees with three planted breaks (a second
    status line, a note's link removed, 0211-MADR set back to `proposed`). It reported all three, and exited 1. The
    copy was removed.
  * The same run also reported **0130**: its MADR was `proposed` while its PLAN was `complete`. That was a real case
    the triage had filed as parked. 0130's decision was to investigate first, and the investigation ran, so it is
    `accepted` (inferred) with a note that the outcome was to park.
  * On the tree: `p1_check.py` exit 0 (11 edited records, 232 numbers, 0 failures); `make check-records` exit 0; the
    report's markdown lint 0 issues; the redact preview would edit 0 files.
* **Step 5.** The report was created with its P1 section, and the index regenerated. Staged for the owner's commit:
  the eleven records, the report, `docs/README.md` and this plan.

**Not done in P1:** 0227's two statuses, which wait for the owner's manual check. The 0205 tag ruleset and the idle
proposals are findings in the report, not status changes.

P1 was committed by the owner ("committed, proceed").

### P2 Deviation 1, 2026-11-08 — one of the 32 is a goose-only PLAN that 0160 D9 freezes. The owner decided: "Undo 0122, keep 31"

**Found** after step 1's script had run, before anything was staged. One of the 32 `completed` PLANs is
`0122-PLAN-deterministic-goose-file-log-tail-attach.md`. 0160 D9 lists 0122 among the goose-only PLANs that get a
banner line "and change nothing else (PLAN status lines stay as they are: a PLAN's status records whether it ran,
and these did)". The script had changed its status line.

**Decided:** the one-word edit to 0122-PLAN was reverted by the inverse edit, so its file matches the commit again
(`git diff --stat` shows nothing for it). P2 changes 31 PLANs. The MADR's D2 carries the corrected count. The other
goose-only PLANs named by D9 (0025, 0030, 0110) have no front matter and were never matched.

### P2 — 2026-11-08: the `completed` spelling

* **Start state (C1).** Clean tree; `master` three commits ahead of `origin/master` (0179's amendment, this pair, and
  P1), unpushed.
* **Step 1.** `p2_spelling.py` changed `status: completed` to `status: complete` in each front-matter block, asserting
  one match per file. All 32 matches had the same form. After Deviation 1, 31 files are changed.
* **Checks.** `git diff --stat`: 31 files, one line each. `git diff -U0` shows exactly 31 `-status: completed` and 31
  `+status: complete` lines, and nothing else. `make check-records` and the redact preview, as below.
* **Staged:** the 31 PLANs, the MADR's D2 note, this plan, and the report's P2 section.

P2 was committed by the owner ("committed, proceed").

### P3 Deviation 1, 2026-11-08 — `make check-records` already checks the guides' links. The owner decided: "Anchors only"

**Found** at P3's start, reading `scripts/check_records.py` before writing the resolver. Its `--check-all` mode checks
that every relative link resolves to a file in `docs/guides/**`, `docs/README.md`, `docs/architecture.md`, the root
`README.md`, and the mobile tree's README, architecture and guides. `make check-records` runs that mode
(`Makefile:287-288`). It does not check a `#anchor`. The MADR's Context said nothing checked a guide's links, and D6
planned a resolver for files and anchors.

**Decided:** the MADR's sentence is corrected in place (struck through, with the dated correction), and D6 is
amended. Step 2's resolver checks only heading anchors.

### P3 Deviation 2, 2026-11-08 — seventeen docs the inventory missed. The owner decided: "Add all 17"

**Found** during P3: the mobile README links `guides/standards/mobile/`, and `make markdownlint-docs` lists
`apps/mobile/README.md`. The inventory had globbed one directory level, so it missed the nested coding standards,
`docs/guides/standards/go/*.md` (9 files) and `apps/mobile/docs/guides/standards/mobile/*.md` (7), 803 lines in all,
and the Flutter runbook, `apps/mobile/README.md`.

**Decided:** all seventeen join the audit. The Go standards join P4; the mobile standards and the runbook join P6.
The MADR's inventory and this plan's Scope carry the change.

### P3 — 2026-11-08: the entry points

Scripts and logs are in the session scratchpad (`docref.py`, `docref_failfirst.py`, `anchors.py`,
`anchors_failfirst.py`, `p3_context.py`, `p3-docref.txt`, `p3-docref-after.txt`).

* **Start state (C1).** Clean tree; `master` four commits ahead of `origin/master`, unpushed.
* **Step 1, the reference checker.** `docref.py` resolves eight kinds: a repository path, a `make` target, a CLI
  subcommand, a `--flag`, a config key, an `MCREMOTE_*`/`MCRELAY_*` variable, a Go identifier, and a tool version
  against its pin. **Seen failing:** on a scratch copy of `docs/architecture.md` with one planted bad reference of each
  kind, it reported all eight (15 findings against 7 unplanted). The copy was removed.
* **Step 2, the anchor resolver.** `anchors.py` checks each relative link's `#anchor` against GitHub's heading slugs
  (Deviation 1). **Seen failing:** on scratch copies of two guides, it reported a planted dead same-file anchor and a
  planted dead cross-file anchor, and passed a live control anchor. The copies were removed. Over every unnumbered doc
  it found one dead anchor, `receipts.md:197` `config.md#locations-xdg`, which P5 fixes.
* **Step 3, corrections.**
  * The checker reported 87 references in the five files. Each was read in context: 23 are protocol message types
    (confirmed against `internal/protocol`), and the rest are English after "mcrelay", record titles in the
    generated index, history, or installer options defined in go-selfupdate-lib's template.
  * The real ones, and a read-through of the README against the code (one read-only helper pass for lines 1–747 and
    1213–1668, C5, spot-checked), gave the corrections the report lists: 32 rows for `README.md`, 1 for
    `apps/mobile/docs/architecture.md`.
  * `docs/architecture.md`, `docs/README.md` and `apps/mobile/docs/README.md` checked out and are unchanged.
  * After the edits the checker reports 85, every one in the classes above.
* **Step 4, the index.** No record title changed, so `--write-index` is a no-op. The "I want to…" rows resolve, and
  their anchors are live.
* **Step 5, checks.**
  * `anchors.py` on the five files: 0 dead.
  * `make check-records`: exit 0.
  * `make markdownlint-docs`: 0 issues.
  * `go test ./internal/protocol/` and `TestTrackedFilesUseOnlyPlaceholderHomes`, the Docs workflow's Go tests: ok.
  * The redact preview: 0 files.
* **Staged:** `README.md`, `apps/mobile/docs/architecture.md`, the MADR and this plan, and the report's P3 section.
