---
status: proposed
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
  `apps/mobile/docs/README.md`, `architecture.md`, `guides/*.md`.
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
   names one, an existing heading anchor. Seen failing on a planted dead link and a planted dead anchor.
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

Not started. The owner answered the MADR's Q1 to Q3 on 2026-11-08 (normalize to `complete`; keep 0200's inferred
acceptance rule; fix a guide to match the code). The plan waits for the owner's approval.
