# Record status audit, 2026-10-18

<!-- markdownlint-disable MD013 -->

Observed 2026-10-18, at `master` `67392ed8`. This report checks every decision record that was not
closed against the tree, git history and CI. It records what was found and what was corrected. It
decides nothing: each correction cites its evidence in the record it changed.

## Why

On 2026-10-18 the owner asked for every open record to be corrected, brought up to date, and made
to reflect its unit of work against the current code. Of 358 MADR and PLAN files, 89 record numbers
had at least one record whose status was not closed. Statuses had drifted in four ways:

- MADRs left `proposed` after their PLAN shipped;
- PLANs left `Proposed`, `draft` or without a status after they ran;
- `in-progress` PLANs whose remaining items had since been done, or whose "Not done" text had gone
  stale;
- records whose subject was later replaced, with no mention of it.

## Method

- Every status was read, from YAML front matter or an early `Status:` line, in both record trees
  (`docs/decisions/` and `apps/mobile/docs/decisions/`).
- For each open record, its execution record and last "Not done" list were checked against:
  - the current code, by `grep` for the named symbols and files;
  - git history, through phase-tagged commit subjects such as `fix(0150 P3)`, and `git log -S`;
  - later records that cite it;
  - CI runs (`gh run view`).
- Two read-only helper passes gathered evidence for 0002–0105, and for nineteen of the newer
  records. Their recommendations were spot-checked against the cited lines and runs before any
  edit. Their notes stayed in the session scratch directory.
- No test was run for this audit, and no provider quota was spent.

## Rules for a correction

- **MADR `proposed` → `accepted`** only when its PLAN was executed and the change is on `master`,
  or the owner's approval is on record. Where the acceptance was never written down, the owner
  decided on 2026-10-18 to mark the MADR accepted, with a note that the acceptance is inferred.
- **PLAN → `complete`** only when the code, or the PLAN's own record, shows every step landed.
- **PLAN → `in-progress`** when some steps landed and others did not. The remaining steps are
  named.
- **Additive only.** Nothing was rewritten:
  - a record with YAML front matter had its `status` and `date` changed, and gained a closing
    "Status note — 2026-10-18" section;
  - a record without front matter gained one "Status, corrected 2026-10-18" line under its title,
    the form
    [0160-MADR-remove-goose-cli-support.md](../decisions/0160-MADR-remove-goose-cli-support.md)
    D9 used for its banners.
- Every note links here.

## Corrections

### MADRs now `accepted`

The acceptance is inferred from shipped work unless a source is named.

| Record | Evidence |
| --- | --- |
| 0028 (codex provider) | already "Accepted"; the stale "provider package not started" is corrected |
| 0039 (grok ACP parity) | its own implementation record, phases 0–6; the "not done" rows are stale |
| 0040 (markdownlint) | had no status; the gate shipped as narrowed by 0180 D6 |
| 0045 (mobile hardening) | its PLAN executed |
| 0074 (provider auth) | already accepted; "pending" for D20–D29 is corrected |
| 0078 (session handoff) | D1–D9 locked with the owner; `11a10e6e`…`1cfcf8b6` |
| 0085, 0086 (auth wiring, completion) | executed; accepted 0074 §15 preserves them |
| 0097 (curl installer) | shipped; platform scope later reversed by 0104 |
| 0102 (display name) | owner resolved its questions 2026-08-18; Phases 1–3 shipped |
| 0103 (update tracking) | implemented; superseded in part, below |
| 0136 (codex auth from doctor) | `f451d5bc`, `36b6af84`, `3456f534` |
| 0141, 0147–0151, 0153, 0154 | phase-tagged commits; PLANs `complete`/`completed` |
| 0163 (codex pins) | release 3 acceptance, 2026-09-21 |
| 0169 (toolchains) | P0–P11 executed; `go 1.27.2` |
| 0199 (grok session cost) | owner chose its amendment and approved its PLAN, 2026-10-18 |

### PLANs now `complete`

- 0028, 0034, 0035, 0037, 0039, 0075, 0078, 0080, 0084, 0085, 0094, 0096, 0097, 0099, 0104, 0105,
  0112.
- 0116, 0167 and 0180, each with its last acceptance row met, as their notes cite.

### PLANs now `in-progress`, with what remains

| Record | Remaining |
| --- | --- |
| 0042 | phase 7 step 3, the on-device pass |
| 0045 | reconciling the unticked A-phase boxes with the tree |
| 0074 | P22 steps 5–6, the live acceptance run |
| 0079 | the manual Android acceptance |
| 0089 | Phase 0 and Phases 3–5; only 1–2 landed (the MADR stays `proposed`) |
| 0095 | Step 22 (C5 on a device), then Step 23 |
| 0102 | Phase 4's manual acceptance |
| 0136 | recording the fail-first runs, and Phase 5 on the reporting host |

### Superseded, in whole or in part

- **0027-PLAN** by 0057: its isolate markdown parse was removed and ruled out.
- **0038-MADR** by 0039.
- **0114-PLAN**, with its MADR, by 0169.
- **0065 MADR/PLAN and 0103 MADR/PLAN**, for the host update path. `45f82032` (2026-09-02) replaced
  `internal/update` with `mcplib/selfupdate`, under MADR 0005 in the mcplib repository
  (`docs/guides/config.md`). 0103's per-product service restart survives.
- **0128-PLAN** stays `complete`, with a note that its `parseBase` code went with `internal/update`.
- **0167-MADR** stays `accepted`, with D16 superseded by 0184 D1. Both of the 2026-10-13
  amendment's conditions are now met.

### Reports marked `complete`

0032 (by 0035), 0033 (by 0034), and 0041 (by 0042).

### Status kept; stale text corrected by a note

| Record | Correction |
| --- | --- |
| 0021 | OpenCode pin is `1.18.26`, not `1.18.21` |
| 0056 | "Plans 0054/0055" means the old hardening plan, now `0004-PLAN` |
| 0070 MADR/PLAN | the phone install UI shipped (`089950fe`) |
| 0073 | remediation started: F1/F2 fixed (`8719682f`, `a62a18ff`) |
| 0075 | kilo known-good is `7.5.6`, not `7.4.22` |
| 0083 | D6, the goose-keyring follow-up, no longer applies |
| 0090, 0109 | `in_progress` respelled `in-progress`; both accurate |
| 0123 | P9/P10 landed (`59e9f690`); only the device pass remains |
| 0133 | `RealityExternal` is gone, replaced by 0136's probe |
| 0145 | `make ci-windows` passes on the host; A10/A11, C1#8 and the closeout remain |
| 0159 | the ACL item was resolved 2026-09-20; P12 (`--env-file`) remains |
| 0169-PLAN | P11 and A6/A7 done; P12 on other hosts, P6b, git, Python remain |
| 0179-PLAN | file rename done; content rewrite and source phases not |
| 0184-PLAN | A4's Flutter check passed (run `38925756161`); the wire-capture confirmation item is open |
| 0186-PLAN | P1 and P2 not started; the code shows both still needed |

## Checked and left as they are

Each of these reads accurately against the tree:

- **Not implemented:**
  - 0002, 0010 (un-actioned assessments);
  - 0054 MADR;
  - 0113, 0117, 0121, 0131, 0135, 0146, 0157, 0181.
- **Open by the record's own rule:** 0090 MADR, and 0095 MADR (acceptance held until C5).
- **Partly decided:** 0089 MADR (two of seven decisions ran).
- **Investigation parked on purpose:** 0130.
- **`in-progress` and accurate:**
  - 0054, 0109;
  - 0125 (P4 on the Mac);
  - 0132 (device acceptance);
  - 0134;
  - 0143 (Phases 3–5);
  - 0199 (G4's live turn).
- **Closed records:** every record already marked accepted, complete or implemented whose subject
  was not later replaced.

## For the owner

- **0025-PLAN** says "Implementation-ready", though it ran and goose was later removed. 0160 D9
  froze the status lines of goose-only PLANs, so it is left as it is. Changing it would need 0160
  amended.
- **0073** gained a correcting note, but deliberately no superseded marking, which 0160 D9 forbids
  for this mixed record.
- **0135 is cited without being implemented.**
  `internal/provider/codex/contract_generate_test.go:32` invokes it for the codex contract
  manifest's `schema_version`. 0135 is about the credential manifest, and it is not implemented.
- **Records with no status at all** were given one only where this audit changed them. Other
  closed old-format records were not normalised.

### Resolved the same day

The owner decided all three on 2026-10-18:

- **0025-PLAN.** [0160-MADR-remove-goose-cli-support.md](../decisions/0160-MADR-remove-goose-cli-support.md)
  was amended, so D9's freeze no longer covers this one PLAN. The PLAN gained a correction line
  saying it ran (`ae280566`).
- **0073.** The same amendment allows a "superseded in part" line, for the Goose root cause and F2,
  and the record now carries one. Its other findings are triaged in
  [0073-REPORT-triage-of-the-non-goose-findings.md](0073-REPORT-triage-of-the-non-goose-findings.md):
  7 fixed, 36 still present, 2 obsolete and 1 unclear.
- **0135's citation.**
  [0163-PLAN-codex-jank-is-stale-pins-not-upstream-churn.md](../decisions/0163-PLAN-codex-jank-is-stale-pins-not-upstream-churn.md)
  P15 rewords the comment. It now cites `ContractManifest.Validate`, the rule the code enforces,
  and 0135 is cited by no Go file.
