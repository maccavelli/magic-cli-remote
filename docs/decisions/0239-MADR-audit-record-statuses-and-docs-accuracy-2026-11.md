---
status: proposed
date: 2026-11-08
decision-makers: Project Owner
consulted: "this repository at 5b3533fc plus the 0179 amendment (owner's commit of 2026-11-08); 0200-REPORT-record-status-audit.md and the commits that applied it (56ea5621); a scripted inventory of every numbered record's front matter and every unnumbered doc (tidy_inventory.py, session scratchpad)"
informed: anyone who reads the records or the guides
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Audit the open records and the guides against the tree, 2026-11

## Context and Problem Statement

On 2026-11-08 the owner asked: "let's do the tidy up pass and ensure all docs reflect accurate up-to-date info".

The last such pass was [0200-REPORT-record-status-audit.md](../reports/0200-REPORT-record-status-audit.md) (2026-10-18,
applied in `56ea5621`). It covered the numbered records only. It set rules for a correction that the owner approved,
and found each open record either corrected or accurate at that date. Three weeks and roughly 40 records later, the
records and the unnumbered docs have drifted again.

### Inventory [observed 2026-11-08, read-only]

**Numbered records.** 449 files in both record trees (`docs/` and `apps/mobile/docs/`). Of the MADRs and PLANs:

| Front-matter status | Count | Notes |
| :--- | ---: | :--- |
| MADR `proposed` | 19 | 0089, 0090, 0095, 0113, 0117, 0121, 0130, 0131, 0135, 0146, 0157, 0181, 0208, 0211, 0213, 0221, 0227, 0229, 0233 |
| PLAN `in-progress` | 20 | 0089, 0090, 0109 (first plan), 0123, 0125, 0132, 0133, 0134, 0136, 0143, 0145, 0159, 0163, 0169, 0203, 0208, 0216, 0217, 0219, 0227 |
| PLAN `proposed` | 12 | 0109 (second plan), 0113, 0117, 0121, 0131, 0146, 0157, 0179, 0181, 0205, 0213, 0228 |
| PLAN `completed` | 32 | a closed status, spelled differently from the `complete` that `madr-and-plan-writing` gives |
| no front-matter status | 149 | 0001 to 0176, the older format; 0200 left these as they are unless it changed them |
| one MADR | 1 | `accepted; superseded in part by MADR 0005 in mcplib`, a closed status with a note |

Some of the 51 open records are open on purpose, and 0200 said so: 0130's investigation is parked, 0113, 0117, 0121,
0131, 0135, 0146, 0157 and 0181 were not implemented, and 0095 holds acceptance until a device check. Others show the
same drift 0200 found. Examples, each to be confirmed in the PLAN:

* **A MADR left `proposed` after its PLAN closed:** 0211's PLAN is `complete`; 0130's PLAN is `complete`.
* **A PLAN whose own record says it finished:** 0208-PLAN reads "P8 is complete with this commit", and its P1 to P8
  are done, but its status must stay `in-progress` while P9 and P10 remain; 0217-PLAN's last phase, P3g, is recorded
  as executed.
* **A PLAN with an execution record and a `proposed` status:** 0205-PLAN, whose decision changes no code. Whether its
  tag ruleset is in place is for the PLAN to read back from the API.

**Unnumbered docs.** 24 files, 10,279 lines:

* the root `README.md` (1,668 lines);
* `docs/README.md`, `docs/architecture.md`, and 15 guides in `docs/guides/`, among them `protocol-v1.md` (2,584
  lines) and `config.md` (888);
* `apps/mobile/docs/README.md`, `architecture.md`, and 4 guides.

*Amended 2026-11-08 (PLAN P3 Deviation 2, the owner's "Add all 17"):* the inventory read one directory level. Seventeen
more user-facing docs are in scope: the coding standards under `docs/guides/standards/go/` (9 files) and
`apps/mobile/docs/guides/standards/mobile/` (7 files), 803 lines together, and `apps/mobile/README.md`, the Flutter
runbook.

Nine of them were last changed on or before 2026-10-03, before several changes they describe: OpenCode v2 over ACP
(0214), strict provider sections and the provider descriptor (0208 P8), the saved model catalog (0235), the codex
re-pin to 0.167.0 (0236), and the pin updates of 0217. ~~`check_records.py` checks links inside numbered records only;
nothing checks a guide's links or its facts.~~ *Corrected 2026-11-08 (PLAN P3 Deviation 1):* `make check-records` runs
`check_records.py --check-all`, which also checks that every relative link in the unnumbered docs resolves to a file.
Nothing checks a link's `#anchor`, or a guide's facts.

## Decision Drivers

* A record's status must say what is true of the tree, so a reader can trust an index built from it.
* A guide must describe the system as it is now. A stale default, path, command or pin in a guide misleads more than a
  missing one.
* History is kept. A correction adds; it never rewrites what a record said at the time (0200's rule).
* Each correction carries its evidence, so it can be checked without redoing the audit.
* No source changes. The code is the truth a doc is checked against (Q3).
* Cost: about 10,000 lines of guides and 51 records. The pass is split into phases, each one commit.

## Considered Options

* Audit the open records and every unnumbered doc, under 0200's rules, in phased commits, with a report
* Audit the open records only, as 0200 did
* Normalise every record's metadata as well
* Leave the drift until each subject is next worked on

## Decision Outcome

Chosen option: "Audit the open records and every unnumbered doc, under 0200's rules, in phased commits, with a
report", because it answers the owner's request as asked, keeps 0200's evidence and history rules, and splits the
guide work small enough to review.

* **D1. Open records, under 0200's rules.** Each of the 51 open records is checked against the code (by `grep` for
  the files and symbols it names), git history (phase-tagged subjects and `git log -S`), later records that cite it,
  and CI runs. Then:
  * MADR `proposed` → `accepted` only when its PLAN ran and the change is on `master`, or the owner's approval is on
    record. Where the acceptance was never written down, 0200's 2026-10-18 owner rule applies: mark it accepted, with
    a note that the acceptance is inferred. The owner confirmed the rule for this audit (Q2).
  * PLAN → `complete` only when the code or the PLAN's own record shows every step landed. PLAN → `in-progress` when
    some did, with the remaining steps named.
  * `superseded` (in whole or in part) when a later record replaced the subject, naming that record.
  * A record that is open on purpose and reads accurately is left as it is and listed as checked.
  * The form is 0200's: front matter's `status` and `date` change, and a closing "Status note — <date>" section is
    added; a record without front matter gains one "Status, corrected <date>" line under its title. Every note links
    to this audit's report.
* **D2. The `completed` spelling** (32 PLANs). The one word changes to `complete`, with no note, as a mechanical
  commit of its own (Q1, the owner's answer). It closes nothing and opens nothing; it lets a query on status be exact.
  *Amended 2026-11-08 (PLAN P2 Deviation 1): 31 PLANs, not 32. 0122-PLAN is a goose-only record, whose status line
  [0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md) D9 freezes ("PLAN status lines stay
  as they are"). The owner chose to leave it as `completed`.*
* **D3. The 149 records without front matter** stay as they are, unless D1 changes one (0200's rule). Normalising
  them would touch 149 closed records for no reader's benefit.
* **D4. Unnumbered docs, checked against the tree.** For each doc:
  * a script extracts every checkable reference: a repository path, a `make` target, an `mcremote` or `mcrelay`
    subcommand or flag, a config key, a Go identifier in backticks, an environment variable, a record link, and a
    version or pin. Each is resolved against the tree (the file exists, the target is in the `Makefile`, the key is in
    a provider section or `config`, the identifier is defined, the pin matches `go.mod`, a `version.go` or a
    workflow). The script is seen failing first on a planted bad reference;
  * every reference that does not resolve is read in context and corrected, or listed as a finding;
  * each doc is then read through for behaviour the records changed since its last edit, using the records changed
    after that date as the list of what to check;
  * `architecture.md` describes the system as it is now, with no history (`documentation-writing`).
* **D5. Corrections to a doc are edits, not notes.** A guide carries no history, so a stale fact is replaced. The
  report lists each one with its evidence. Where a guide describes behaviour the code does not have, the guide is
  rewritten to what the code does (Q3, the owner's answer), and the report lists it separately as a doc-versus-code
  difference, so the owner can see each place the intended behaviour and the code parted.
* **D6. Links.** `docs/README.md`'s records table is regenerated by `check_records.py --write-index`, never by hand.
  Its "I want to…" rows are checked against the docs they name. A throwaway link resolver checks every relative link
  in the unnumbered docs, seen failing first, since `check_records.py` does not. *Amended 2026-11-08 (PLAN P3
  Deviation 1, the owner's "Anchors only"): `check_records.py --check-all` already checks that each link's file
  exists, so the resolver checks only `#anchor` targets.*
* **D7. One report, `0239-REPORT-record-status-and-docs-accuracy-audit.md`,** in `docs/reports/`, in 0200's shape:
  method, rules, every correction with its evidence, what was checked and left, and findings for the owner.
* **D8. Out of scope, recorded as findings:** a source change of any kind, including where D5 finds the code, not
  the guide, may be the one that is wrong; a decision a stale record exposes but does not make; and `AGENTS.md`'s or
  the per-agent rules' process text, which changes only by the owner's word. A factual error in them (a renamed
  target, a wrong path) is listed for the owner.

### Consequences

* Good, because the open records and the guides say what is true of `master`, with the evidence for each change.
* Good, because a guide's references are checked mechanically, once, and the script's results are in the report.
* Good, because history is kept: records gain notes, and nothing they said at the time is rewritten.
* Neutral, because the records without front matter keep the older format.
* Bad, because the pass is long: about 10,000 lines to read, in several commits, and the owner reviews each.
* Bad, because a read-through finds what the reader notices. The reference script narrows the gap; it cannot close it.

### Confirmation

1. Every one of the 51 open records is in the report, as corrected or as checked and left, with its evidence.
2. No `proposed` MADR has a `complete` PLAN unless the report says why.
3. Each unnumbered doc is in the report, with its reference-script result and its corrections.
4. The reference script and the link resolver were each seen failing on a planted bad input.
5. `make check-records` passes; markdown lint passes; the disclosure guard preview finds nothing.
6. No file outside `docs/`, `apps/mobile/docs/`, the root `README.md` and the records changed.

## Pros and Cons of the Options

### Audit the open records and every unnumbered doc, under 0200's rules, in phased commits, with a report

* Good, because it covers what the owner asked for: the records and "all docs".
* Good, because 0200's rules are already approved, so only the new parts need decisions.
* Bad, because it is the largest of the options.

### Audit the open records only, as 0200 did

* Good, because it is a known amount of work.
* Bad, because the guides are what users read, and nothing has checked them.

### Normalise every record's metadata as well

* Good, because every record would carry the same front matter.
* Bad, because it touches 149 closed records to change no fact, and 0200 chose not to.

### Leave the drift until each subject is next worked on

* Good, because it costs nothing now.
* Bad, because a subject that is never worked on again stays wrong, and the index built from statuses stays
  unreliable.

## More Information

### Owner questions, answered 2026-11-08

* **Q1. The `completed` spelling.** Change the 32 PLANs to `complete` in one mechanical commit, or leave both
  spellings, as 0200 did? **Answer: "Normalize to complete".** D2 and PLAN P2.
* **Q2. Inferred acceptance.** Does 0200's 2026-10-18 rule still stand: a MADR whose PLAN ran and shipped, with no
  written acceptance, is marked accepted with a note that the acceptance is inferred? **Answer: "Keep the 0200
  rule".** D1.
* **Q3. A guide that describes behaviour the code does not have.** List it as a finding and leave the guide, or fix
  the guide to match the code? The proposal was to list it. **Answer: "Fix the guide to match code".** D5: the guide
  is rewritten to the code, and the report lists each such place.

### Related

* [0200-REPORT-record-status-audit.md](../reports/0200-REPORT-record-status-audit.md): the last pass and its rules.
* [0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md) D9: goose-only records keep their
  status lines, as amended for 0025 and 0073.
* The PLAN: [0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md](0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md).
