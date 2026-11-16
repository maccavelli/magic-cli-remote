---
status: in-progress
date: 2026-11-15
associated-madr: "0248-MADR-bring-the-docs-current-and-keep-them-so.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement bringing this repository's docs current and keeping them so

Associated MADR: [0248-MADR-bring-the-docs-current-and-keep-them-so.md](0248-MADR-bring-the-docs-current-and-keep-them-so.md)

**Provenance.** Drafted by the strongest model at high effort, from this repository at `0fbed31b`, read-only. The
survey it builds on was made by a mainstream model at medium effort. The rulings are the owner's, of 2026-11-14,
relayed by the cross-repository coordinator.

## Goal

When this plan is complete:

* every defect Phase A finds is fixed by a named commit, or carried with its reason (MADR Confirmation 1);
* `CHANGELOG.md` exists at the root, in Keep a Changelog 1.1.0 form, starting where OQ1's answer says (D3);
* every provider, command, flag, environment variable, settings key, make target and deploy unit in scope (D2, OQ4)
  is described in a README, guide or reference (Confirmation 5);
* every exported name in scope (D2, OQ3) has a doc comment (Confirmation 4);
* `docs/README.md` and the README's documentation tables reach every record and guide (Phase F);
* the standing rules S1 and S2 below are in force for every later phase and tag. They live in this record and in
  briefs only, not in `AGENTS.md` or a rule file (OQ5, as applied 2026-11-14).

This plan is `proposed`. Nothing below starts until the owner accepts the MADR and approves this PLAN, whole or by
phase. Phases that depend on an open question say which one. *(2026-11-14: the owner answered OQ1, OQ3 and OQ6, and
the coordinator applied the owner's "Skill only" ruling to OQ5. See the MADR's dated notes on each question. OQ2, OQ4,
OQ7 and OQ8 remain open.)* *(2026-11-14, later: the owner answered OQ2, OQ4, OQ7 and OQ8 as well. No question is
open, and no phase waits on one. The plan stays `proposed` until the owner approves it and says when it starts.)*
*(2026-11-15: the owner approved the pair, one phase at a time. The plan is `in-progress`, Phase A has run, and the
plan pauses after each phase until the owner says to continue. See the execution record.)*

## Scope

* **Docs:** `README.md`; `docs/README.md` (generated rows by `--write-index`, and the hand-written "I want to…"
  table); `docs/architecture.md`; `docs/guides/**`; `apps/mobile/README.md`; `apps/mobile/docs/**` except records.
* **New:** `CHANGELOG.md` (Phase E); `docs/reports/0248-REPORT-documentation-defect-list.md` (Phase A).
* **Go doc comments only:** `.go` files under `cmd/`, `internal/`, `scripts/`, `test/` (Phase D). No other change to
  any line of code.
* **Process text, factual fixes only (OQ5 (b), as applied 2026-11-14):** `.opencode/rules.md:36` and
  `.kilo/agent/madr-and-plan-skill.md:41` (the record path), and `AGENTS.md:51` (the live-targets list). No docs-step
  paragraph is added to `AGENTS.md`.
* **Records:** this pair (execution record and deviations); the REPORT.
* **`Makefile`, one variable** *(2026-11-14, OQ7, the owner's own exception)*: `CHANGELOG.md` joins
  `MARKDOWNLINT_DOCS_ROOTS` (`Makefile:307`), and the comment above it (`Makefile:302-306`) names it. Phase E makes
  this change.
* **Out of scope:** any behaviour change; the `Makefile`, `make/*.mk`, `.github/workflows/*`, `.markdownlint-cli2.jsonc`
  and `scripts/*` (D4: no new check and no changed gate; the stale "MagicTools MCP fleet READMEs" comment in
  `.markdownlint-cli2.jsonc:2` is listed in the REPORT, not changed) *(2026-11-14, OQ7: except the one `Makefile`
  variable above)*; front matter of old records (OQ2; the owner answered "Leave them as history": none of the 168
  changes); `CLAUDE.md` layout (OQ8; the owner answered "A separate record", which the coordinator's ledger carries);
  0246's and 0247's records (their amendment for D5 is the owner's to approve separately); any other
  repository; tags, pushes and GitHub Release bodies. *(2026-11-15: see C5's dated annotation. By the coordinator's
  application of the owner's standing approval of 2026-11-15 ("Yes, all approved plans"), the coordinator commits and
  pushes each owner-approved phase after the disclosure guard exits 0; tags, releases and force pushes still need the
  owner's word each time. The text before this note is kept as written.)*

## Conventions every phase follows

* **C1. Start state.** Read `git status --short`, `HEAD` and `origin/master`. The tree must be clean, or each
  difference must be named and left alone. Record `git log --oneline 0fbed31b..HEAD`. If a file this plan cites has
  changed, re-read it before acting on a line number.
* **C2. One-off scripts** live in the executing session's scratchpad (`$T` below), are Python 3 standard library or
  Go standard library, and are never committed (D4). Each one is first seen failing on a planted bad input in `$T`,
  never in the working tree. The execution record says what the failure looked like.
* **C3. Exit statuses.** Every check runs as `<command> > "$T/<name>.out" 2>&1; echo "exit=$?"`, and the output is
  read after the status is recorded. No pipeline stands between a check and its status.
* **C4. Heavy runs.** No phase needs a suite run: the edits are Markdown and Go comments. If an executor wants one
  anyway, `go test ./...` or `make test` goes through the fleet's `pick-host` and `run-on-host` (or `run-heavy` on the
  session host), never bare. `go test -count=1 ./internal/protocol/` and the home-path guard are single-package runs
  and stay on the session host.
* **C5. Commits.** The agent stages by path. The owner commits with `git commit --no-edit`. The disclosure guard's
  redact preview (`python3 ~/.global-git-hooks/github-disclosure.py redact`, from the repository root) runs before
  each staging and must report no file to edit. No push and no tag without the owner's word in the same turn.
  *(2026-11-15, the coordinator's application of an owner ruling; it is not a new ruling of the owner's on this plan.
  On 2026-11-15 the owner widened a standing approval, answering "Yes, all approved plans": the coordinator commits
  and pushes each owner-approved phase of any plan in the related repositories after the disclosure guard exits 0.
  Tags, releases, force pushes and anything outside an approved plan still need the owner's word each time. The redact
  preview this convention requires still runs before each staging. C5's text above is kept as written.)*
* **C6. Identifiers.** No hostname, account name, email address or real-machine path in any committed file. Use
  placeholders.
* **C7. Deviations.** Anything that differs from this plan stops the phase. It gets a dated entry here, and an
  amendment in the MADR where a decision or fact changes, before work continues.
* **C8. Stop condition.** An unexpected state, or the same step failing twice, ends the task. The executor returns
  the state and its evidence and does not improvise or try a third time.

## Standing rules (MADR D5, D6)

These are not phases. They bind every later brief in this repository from the day the owner approves this plan. They
live in this record and in the briefs that carry them, and nowhere else: not in `AGENTS.md`, and not in a per-agent
rule file (OQ5, as applied 2026-11-14).

* **S1. The docs step in every phase that changes behaviour.** Every later brief for code in this repository carries
  a docs step, per the mandate. The step lists the docs the change makes false or incomplete: README, guide,
  `architecture.md`, config or protocol reference, the doc comments of the names it adds (D2, OQ3), and an entry under
  `## [Unreleased]` in `CHANGELOG.md` for any change a user, operator or the app can see. It updates them in the same
  commit, or the phase's execution record says why none is needed. This applies to 0246's and 0247's phases.
* **S2. The pre-tag step.** The brief for every tag includes these steps, before the tag:
  1. `git log --oneline <previous tag>..HEAD`; for each commit a user can see, confirm a `CHANGELOG.md` entry.
  2. For each such commit, confirm that the docs it makes false were fixed (S1's record), and fix any that were not.
     These are the release-caused fixes (D6).
  3. Move `## [Unreleased]` to `## [vX.Y.Z] - <date>`, and open a new empty `## [Unreleased]`.
  4. Lint `CHANGELOG.md` by hand with the repository's configuration (OQ7 (a)). *(2026-11-14: OQ7 was answered (b).
     From Phase E on, `make markdownlint-docs` lints `CHANGELOG.md`, so this step is that target's exit 0.)*
  5. Record steps 1 to 4 in that release's PLAN execution record.

  Older gaps found in step 2 that the release did not cause are added to Phase A's REPORT, not fixed before the tag.

## Order

*Revised 2026-11-14 for OQ6's answer ("Yes, docs-only phases now").* The Markdown-only phases (A, E, B, C, F) run
now, one at a time. If a release step starts (the S2 brief for a tag), the running phase finishes and is committed,
and the next one waits until the tag is made. Phase D edits Go files, so it waits for 0246's cut-over. These phases
never run at the same time as a 0246 or 0247 phase. Between such phases, they take turns at phase boundaries.

| Order | Phase | When | Waits on |
| :--- | :--- | :--- | :--- |
| 1 | A — verify and list every defect | now | owner approval |
| 2 | E — `CHANGELOG.md` | after A; in any case before the next phase that changes behaviour in this repository (0246's P1, or whatever lands first) | none (OQ1 answered) |
| 3 | B — wrong statements | after E | none (OQ5 applied as (b)) |
| 4 | C — missing feature docs | after B | none (OQ4 answered) |
| 5 | F — index currency | after C | none |
| — | 0246 and 0247's phases, each with S1; the next tag with S2 | the owner's order in 0246-PLAN's Scope, interleaved at phase boundaries | those records' own approval |
| 6 | D — doc comments | after 0246's cut-over (it edits Go files 0246 rewrites) | none (OQ3 answered) |

Phase letters follow the brief that commissioned this pair. Execution order is the table's. The close-out moves to
Phase D's last commit, since D now runs last.

## Implementation Steps

### Phase A — verify every lead and list every defect

* **Model class and effort:** strongest, high. Each lead needs a verdict against the code, and each defect must be
  classified as release-caused or older.
* **Files written:** `docs/reports/0248-REPORT-documentation-defect-list.md` (new); `docs/README.md` (by
  `--write-index` only); this PLAN's execution record.
* **Files read in full:** every doc the survey only sampled or skipped:
  * `docs/guides/config-mcrelay.md`, `protocol-v1.md`, `protocol-v2.md`, `receipts.md`, `headscale.md`,
    `iam-route53-acme.md`, `chat-performance.md`, every `docs/guides/ops-*.md`, and every
    `docs/guides/standards/go/*.md`;
  * `apps/mobile/README.md`, and `apps/mobile/docs/` (README, architecture, guides, standards);
  * `AGENTS.md`, `.claude/rules/*.md`, `.codex/rules/*.md`, `.grok/rules/*.md`, `.opencode/rules.md`,
    `.kilo/agent/*.md`;
  * `internal/provider/codex/testdata/0.167.0/README.md`;
  * every package doc comment (`doc.go`, or the file that carries it);
  * `configs/*.yaml` comments, compared with `docs/guides/config.md`.

  In all, about 9,500 lines of Markdown.

Steps:

1. C1. Note `HEAD`. If it is not `0fbed31b`, list what changed under `docs/`, `README.md`, `internal/`, `cmd/`,
   `deploy/` and the `Makefile`, and re-check every lead those changes touch.
2. **The leads.** Give each a verdict (verified, refuted, or still a lead, with the reason) and cite `file:line`:
   * survey rows A1, A2, B1 to B8, C1 to C8;
   * the MADR's "New, this draft" rows;
   * 0239-REPORT's three "unverified, left as written" rows and its `AsyncNotifier` finding.
3. **The full reads.** Read each file in the list above against the code at `HEAD`. Use 0239's method: a scratch
   reference checker resolves paths, make targets, subcommands, flags, config keys, environment variables, Go
   identifiers and version pins (C2: seen failing first). Then read the file through. For `protocol-v1.md`, record
   which parts `internal/protocol/doc_coverage_test.go` already holds (event types) and check the rest (message
   types and payload fields) against `internal/protocol/messages.go`. For mcrelay, check every flag in
   `internal/relay/cli.go` against `config-mcrelay.md`.
4. **The surface inventory.** From the code, list every:
   * provider (`KnownProviderIDs` and the registry);
   * `mcremote` and `mcrelay` command and flag (the cobra registrations under `internal/cli/` and the flag set in
     `internal/relay/cli.go`);
   * environment variable (string literals in non-test Go code, plus the `MCREMOTE_*` and `MCRELAY_*` names viper
     derives from settings keys);
   * settings key (the `mapstructure` tags of the config and provider-section structs);
   * `.PHONY` target in the `Makefile` and `make/*.mk`;
   * file under `deploy/`;
   * Go package (`go list ./...`).

   For each, name the README, guide or reference that describes it, or record it as missing. Mark each with OQ4's
   reading it falls under (user-set, internal marker, test-only).
5. **The doc-comment baseline.** Build and run Phase D's measurement program (step D1). Record its table. At `0fbed31b`
   it must reproduce Phase D's baseline exactly. A difference at `0fbed31b` is a stop (C8). At a later `HEAD`, each
   difference is explained by a commit.

   **5a. The contract types** *(added 2026-11-14 for OQ3's answer, "Fields of public and contract types")*. Name every
   type a user configures or receives:
   * the wire structs in `internal/protocol`;
   * the config structs whose `mapstructure` tags are settings keys (`internal/config`, the provider sections under
     `internal/provider/*/section.go`, and `internal/relay/fileconfig.go`);
   * any other type the REPORT finds on the wire or in a config file.

   Confirm with `go list -f '{{.ImportPath}} {{.Name}}' ./...` that every package outside `internal/` is a `main`
   package, so "public packages" adds none here (at drafting: `cmd/mcremote`, `cmd/mcrelay`,
   `scripts/capture-wire`, `scripts/smoke-protocol` and `test/supervisors/probeagent`, all `main`). If one is not,
   its exported types join the list. For each contract type, count its exported fields and interface methods, and how
   many lack a comment under the ruling's rule: a field counts as documented when it has its own doc or trailing
   comment, or when its type's doc comment names it. Record the list and the counts in the REPORT. They are Phase D's
   scope.
6. **Classify.** Give every defect:
   * a kind: wrong, missing, or stale;
   * a release class: release-caused for the next tag (made false or undocumented by a commit after `v0.26.0`), or
     older;
   * a destination: Phase B, C, D or F; "S1 of the 0246 or 0247 phase that changes it"; or "owner decision" (D8).

   Write the replacement fact for each wrong statement. Write the exact wording of each "I want to…" row and README
   table row Phase F will add, so that Phase F is mechanical.
7. **The REPORT** in `docs/reports/`, in 0239-REPORT's shape: method, every lead's verdict, every doc read with its
   result, the surface inventory, the defect table, the baseline, and the owner's list.
8. `python3 -B scripts/check_records.py --write-index`. Then the checks below. **Stage** the REPORT, `docs/README.md`
   and this PLAN.

Verification:

```bash
python3 -B scripts/check_records.py --check-all > "$T/a-records.out" 2>&1; echo "exit=$?"   # exit=0, file empty
make markdownlint-docs > "$T/a-lint.out" 2>&1; echo "exit=$?"                               # exit=0, "0 error(s)"
git diff --cached --name-only > "$T/a-staged.out"; echo "exit=$?"                           # exactly the REPORT, docs/README.md, this PLAN
python3 ~/.global-git-hooks/github-disclosure.py redact > "$T/a-redact.out" 2>&1; echo "exit=$?"   # no file to edit
```

*(2026-11-15, on the staged-set line above: at Phase A's run the staged set was four files. The fourth was the MADR,
whose amendments were already staged before the phase and whose status the owner's approval changed. See the execution
record, "Phase A — 2026-11-15", under Deviations.)*

Acceptance:

* every lead in step 2 has a verdict with a `file:line`;
* every file in the read list is named in the REPORT with its result;
* every inventory item has a description location, or a defect row;
* every defect row has a kind, a release class and a destination;
* the baseline table is recorded;
* the contract types are named, with their field and interface-method counts (step 5a).

### Phase E — `CHANGELOG.md`

* **Model class and effort:** mainstream, medium. The `v0.26.0` section is written from 30 commits and their records.
  *(2026-11-14: OQ1 answered (b), "Latest tag forward", so the lightweight and strongest-tier alternatives for (a) and
  (c) no longer apply.)*
* **Waits on:** Phase A. **Must land before** the next phase that changes behaviour in this repository (D6).
* **Files:** `CHANGELOG.md` (new); `README.md` (one row in the documentation table); `docs/README.md` (one "I want
  to…" row: "see what changed in a release"); this PLAN. *(2026-11-14: no `AGENTS.md` paragraph. OQ5 is applied as
  (b).)* *(2026-11-14, OQ7: and `Makefile:302-307`, where `CHANGELOG.md` joins `MARKDOWNLINT_DOCS_ROOTS` and the
  comment names it.)*
* **CI paths.** No workflow changes with the variable. A commit that changes only `CHANGELOG.md` matches `docs.yml`'s
  `paths: ['**.md', …]` (`.github/workflows/docs.yml:12`, `:14`), whose lint step runs `make markdownlint-docs`
  (`:54`). Any commit that touches the `Makefile`, this phase's included, is outside `ci.yml`'s
  `paths-ignore: ['**.md']` (`ci.yml:12`, `:14`), and `ci.yml` also runs `make markdownlint-docs` (`:87`).

Steps:

1. C1.
2. Write the header. It covers what the file covers (D3: `mcremote`, `mcrelay`, installers, units, and the phone app
   built with the tag) and states the form (Keep a Changelog 1.1.0). Say that versions are this repository's `vX.Y.Z`
   tags. Do not claim Semantic Versioning, which no record adopts. Below the sections, give the single pointer the
   owner's OQ1 answer names for older releases: this repository's tags (the GitHub Releases and tags pages, at the URL
   the README's install section already uses) and its records (`docs/README.md`).
3. `## [Unreleased]`, with an entry for each commit since `v0.26.0` that a user can see. At `0fbed31b` there are none:
   both commits are records only.
4. `## [v0.26.0] - 2026-11-12`, built from `git log v0.25.1..v0.26.0` and the records those commits cite, under Added,
   Changed, Fixed and Removed, each entry linking its record by full filename (OQ1 (b), answered 2026-11-14). No
   section for any older tag.
5. The README row and the "I want to…" row.

   **5a.** *(2026-11-14, OQ7.)* Add `CHANGELOG.md` to `MARKDOWNLINT_DOCS_ROOTS` at `Makefile:307`, and to the list in
   the comment above it (`Makefile:302-306`). Change nothing else in the `Makefile`.

   **5b. The widened gate, seen failing (C2).** Make a scratch clone of `HEAD` in `$T`, then copy in this phase's
   `Makefile` and `CHANGELOG.md`. Then:
   * with a deliberately broken `CHANGELOG.md` (a `*` list marker where the configuration pins `-`, MD004, and a
     doubled blank line, MD012), `make markdownlint-docs` exits non-zero and names `CHANGELOG.md`;
   * with that broken file and `HEAD`'s unchanged `Makefile`, it exits 0. This shows that the variable is what makes
     the lint see the file;
   * with the real `CHANGELOG.md`, it exits 0.

   The execution record quotes the failing lines, with temporary paths as `<TMP>`. The clone is removed afterwards.
6. Checks. **Stage.**

Verification:

```bash
test -f CHANGELOG.md; echo "exit=$?"                                                     # exit=0
grep -c '^## \[Unreleased\]$' CHANGELOG.md > "$T/e-unrel.out"; echo "exit=$?"            # exit=0, count 1
markdownlint-cli2 --no-globs CHANGELOG.md > "$T/e-lint.out" 2>&1; echo "exit=$?"   # exit=0; same tool, config and --no-globs as the markdownlint-docs target, at its pinned version via npx
python3 "$T/linkcheck.py" CHANGELOG.md > "$T/e-links.out"; echo "exit=$?"               # exit=0; seen failing first on a planted dead link (C2)
python3 -B scripts/check_records.py --check-all > "$T/e-records.out" 2>&1; echo "exit=$?"   # exit=0
make markdownlint-docs > "$T/e-mdl.out" 2>&1; echo "exit=$?"                            # exit=0, and CHANGELOG.md among the files it lists
(cd "$T/clone" && make markdownlint-docs > "$T/e-broken.out" 2>&1; echo "exit=$?")        # broken CHANGELOG.md, new Makefile: exit non-zero, CHANGELOG.md named
(cd "$T/clone-old" && make markdownlint-docs > "$T/e-old.out" 2>&1; echo "exit=$?")       # broken CHANGELOG.md, HEAD's Makefile: exit=0 (not linted)
git diff --cached -U0 -- Makefile > "$T/e-make.out"; echo "exit=$?"                        # only the MARKDOWNLINT_DOCS_ROOTS line and its comment change
```

`check_records.py` does not read `CHANGELOG.md`, which is why the one-off link check exists. It is not committed.

Acceptance:

* the file exists with exactly one `Unreleased` section and one `v0.26.0` section, and no older section;
* every non-docs commit in `v0.25.1..v0.26.0` maps to an entry or to a stated "not user-visible" in the
  execution record;
* every link resolves;
* the lint is clean;
* *(2026-11-14, OQ7)* `make markdownlint-docs` lints `CHANGELOG.md`, and has been seen failing on a broken copy and
  passing on the real one (step 5b).

### Phase B — wrong statements

* **Model class and effort:** mainstream, medium. Each fix is specified in the REPORT with its evidence and the
  correct fact.
* **Waits on:** Phase E. *(2026-11-14: OQ6 answered (b), so B runs now and not after the tag. OQ5 applied as (b),
  so the process-text rows below are in.)*
* **Files:** those named in the REPORT's "wrong" rows. Known at drafting:
  * `README.md:773` (A1: give `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` its own row, as `config.md:468` does); *(2026-11-15: that citation is `docs/guides/config.md:468`; two tracked files are named `config.md`.)*
  * the closed provider lists at `README.md:241`, `:264`, `:273`, `:1466`, `:1612`, `docs/architecture.md:37`, `:81`
    and `apps/mobile/README.md:13` (A2: add gobble). The provider matrix row is Phase C's;
  * `.opencode/rules.md:36` and `.kilo/agent/madr-and-plan-skill.md:41` (the record path becomes `docs/decisions/`),
    and `AGENTS.md:51` (name every `live-*` target, or point to the README's Development table). These are factual
    fixes only. No rule text changes.

Steps:

1. C1. Re-read each cited line, since 0246's phases may have moved them.
2. Edit each row's file to the REPORT's fact. Where the doc and the code disagree and the fix might be the code, stop
   and list it for the owner (D7, D8).
3. Mark each REPORT row with "fixed in this phase". The commit is named at the close.
4. Checks. **Stage.**

Verification:

```bash
grep -n 'MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES' README.md > "$T/b-a1.out"; echo "exit=$?"   # exit=0, one line
grep -n '_MAX_RETRIES` |' README.md > "$T/b-a1-old.out"; echo "exit=$?"                             # exit=1, the short alias is gone
grep -c -i gobble README.md docs/architecture.md apps/mobile/README.md > "$T/b-gobble.out"; echo "exit=$?"   # exit=0, each count >= 1
python3 -B scripts/check_records.py --check-all > "$T/b-records.out" 2>&1; echo "exit=$?"   # exit=0
make markdownlint-docs > "$T/b-lint.out" 2>&1; echo "exit=$?"                               # exit=0
go test -count=1 ./internal/protocol/ > "$T/b-proto.out" 2>&1; echo "exit=$?"              # exit=0, if a protocol guide changed
go test -count=1 -run '^TestTrackedFilesUseOnlyPlaceholderHomes$' ./internal/ > "$T/b-home.out" 2>&1; echo "exit=$?"   # exit=0
git diff --cached --name-only > "$T/b-staged.out"; echo "exit=$?"                           # only files named by B rows, the REPORT, this PLAN
```

Acceptance: every "wrong" row whose destination is B is fixed, or carried with the owner's reason; and no file outside
those rows is staged.

### Phase C — missing feature docs

* **Model class and effort:** mainstream, medium. The REPORT gives each item and the place it goes. The gobble
  section follows the shape of the README's other "Provider:" sections.
* **Waits on:** B. *(2026-11-14: OQ4 answered (a), "All that non-test code reads". The internal markers go in a short
  "set by the daemon, do not set" table in `docs/guides/config.md`. Test-only variables are described where their live
  or fixture target is described. Every make target gets one row in the README's Development table.)*
* **Files:** the REPORT's "missing" rows. Known at drafting:
  * `README.md`:
    * a gobble row in the provider matrix (`:833-841`) and a "Provider: gobble" section beside the others (`:1005-1210`):
      the binary and transport (`gobble acp`), the known-good version, the `providers.gobble.*` keys (linking
      `config.md`), sign-in as built (device sign-in only for the grok and kilo upstreams, `af8c42fb`), usage limits,
      readiness, and `make live-gobble`;
    * gobble in the YAML surface (`:876-899`);
    * a row for `auth-recovery` in the command table;
    * a row for `deploy/systemd/mcrelay.service` in the Deploy table;
    * every `.PHONY` target in the Development table;
  * `docs/architecture.md`: gobble in "Providers";
  * `docs/guides/config.md`: environment variables per OQ4;
  * `apps/mobile/README.md`: gobble where it lists the picker's providers;
  * whatever else the REPORT lists (the gobble behaviour leads, the 0239 unverified rows).

Steps:

1. C1.
2. Before writing the `mcrelay.service` row, find the code path that installs or cites it. If none exists, stop: an
   orphaned unit is an owner decision (keep, or delete under its own record), not a docs fix.
3. Write each item. Describe behaviour from the code at `HEAD`, never from a record's intent.
4. **One-off surface check (C2).** A scratch script takes each inventory list from Phase A step 4 and asserts that
   every item is named in at least one unnumbered doc or `AGENTS.md`. It is first seen failing on a scratch copy of
   `README.md` with the gobble row removed.
5. Checks. **Stage.**

Verification:

```bash
python3 "$T/surface_check.py" > "$T/c-surface.out" 2>&1; echo "exit=$?"     # exit=0, "0 missing" for every list
grep -n 'deploy/systemd/mcrelay.service' README.md docs/guides/*.md > "$T/c-unit.out"; echo "exit=$?"   # exit=0
python3 -B scripts/check_records.py --check-all > "$T/c-records.out" 2>&1; echo "exit=$?"   # exit=0
make markdownlint-docs > "$T/c-lint.out" 2>&1; echo "exit=$?"                               # exit=0
go test -count=1 -run '^TestTrackedFilesUseOnlyPlaceholderHomes$' ./internal/ > "$T/c-home.out" 2>&1; echo "exit=$?"   # exit=0
```

Acceptance: the surface check reports zero missing in every list OQ4 places in scope, and its result is copied into
the execution record (MADR Confirmation 5); every "missing" row whose destination is C is closed.

### Phase D — doc comments on exported names

* **Model class and effort:** mainstream, medium. A package comment and a field comment need the code read. The five
  `String` and `Error` methods alone would be lightweight work.
* **Waits on:** 0246's cut-over, since 0246 rewrites `internal/provider/*/auth.go`, `internal/providerauth/` and
  `internal/protocol/`.
* **Scope** *(2026-11-14, OQ3 answered "Fields of public and contract types")*. The scope is the top-level names: package
  doc comments, exported funcs, exported types, methods on exported receivers, and consts and vars under their own or
  a group comment. It also covers the exported fields and interface methods of the contract types Phase A step 5a
  names. A field counts as documented when its type's doc comment names it. Out of scope: methods on unexported
  receivers, fields of other types, a separate comment for each const and var, and the Dart API (not ruled on; kept
  out as recommended).
* **Files:** the `.go` files the measurement lists, comment lines only. Split the work into one commit per top-level
  package directory (D-1, D-2, …) when it spans more than one. Each sub-commit is a unit that passes this phase's
  checks on its own.

Steps:

1. **D1, the measurement (C2).** In `$T`, a Go program using only `go/parser`, `go/ast` and `go/token` reads the file
   list `git ls-files '*.go' | grep -v '_test\.go$'` (written to a file first, C3). It skips files where
   `ast.IsGenerated` is true, and counts, per category, exported names with no doc comment:
   * package doc comment (per directory and package name);
   * exported func;
   * exported type, where a single-spec `type` declaration's doc counts;
   * method on an exported receiver;
   * method on an unexported receiver;
   * exported struct field (a doc or trailing comment counts);
   * exported interface method;
   * const and var, counted twice: "own comment" (spec doc or trailing comment), and "own or group comment";
   * *(added 2026-11-14 for OQ3)* contract-type field and contract-type interface method. These are limited to the
     types named in Phase A step 5a, which are passed to the program as a list. A field also counts as documented
     when its type's doc comment contains the field's name as a word.

   The 2026-11-14 additions were not in the program the drafting session ran. Their seen-failing case is an
   undocumented field on a listed type, plus a listed type whose comment names its one field. The program must report
   the first and not the second.

   **Seen failing first:** on a planted two-package tree in `$T` with an undocumented func, an undocumented method on
   a documented type, an undocumented field, an undocumented const and a missing package comment, it reports exactly
   those five.
2. **The baseline.** At `0fbed31b`, over 359 files:

   | Category | Total | Missing |
   | :--- | ---: | ---: |
   | package doc comment | 56 | 4 (`cmd/mcremote`, `cmd/mcrelay`, `internal/provider/codex`, `internal/provider/opencode`) |
   | func | 363 | 0 |
   | type | 515 | 0 |
   | method, exported receiver | 443 | 5 (`procutil/reap.go:68` `Orphan.String`; `provider/cwd.go:36`, `:40` `CWDPermissionError.Error`, `.Unwrap`; `providerauth/classify.go:32` `FlowKind.String`; `updateclient/client.go:328` `ExitError.Error`) |
   | method, unexported receiver | 384 | 176 |
   | struct field | 1,822 | 1,177 |
   | interface method | 216 | 107 |
   | const, own or group comment | 711 | 0 |
   | const, own comment | 711 | 356 |
   | var, own or group comment | 81 | 0 |
   | var, own comment | 81 | 65 |

   At the phase's `HEAD` the numbers will differ, since 0246 adds code. Record the start-of-phase table, and explain
   each difference by a commit.
3. Write the comments for every category in this phase's scope. Follow the Go convention: the comment starts with the
   name. A package comment says what the package is for, not its history. Fields that are settings keys or wire
   members name the key or member and link no record.
4. `make pre-add-check FILES="<the changed .go files>"` (gofmt, golint, govulncheck).
5. Re-run the measurement. It must report 0 missing in every in-scope category.
6. Checks. **Stage**, per sub-commit where split.

Verification:

```bash
"$T/docmeasure" . < "$T/files.txt" > "$T/d-after.out" 2> "$T/d-after.err"; echo "exit=$?"   # exit=0; in-scope categories "missing 0"
make pre-add-check FILES="<changed files>" > "$T/d-pre.out" 2>&1; echo "exit=$?"               # exit=0, no golint output
go build ./... > "$T/d-build.out" 2>&1; echo "exit=$?"                                          # exit=0
go vet ./... > "$T/d-vet.out" 2>&1; echo "exit=$?"                                              # exit=0
git diff --cached -U0 -- '*.go' > "$T/d-diff.out"; echo "exit=$?"                               # every +/- line is a comment line or blank
python3 "$T/comments_only.py" "$T/d-diff.out" > "$T/d-co.out"; echo "exit=$?"                   # exit=0; seen failing first on a planted code line (C2)
```

Acceptance:

* the re-run reports 0 missing in every in-scope category, with the before and after tables in the execution record
  (MADR Confirmation 4);
* the diff changes comment lines only;
* `pre-add-check` is clean.

### Phase F — index currency

* **Model class and effort:** lightweight. Phase A's REPORT gives the exact wording of every row.
* **Files:** `docs/README.md` (regenerated rows; hand-written "I want to…" rows); `README.md` (documentation table
  and "Architecture & key decisions"); `apps/mobile/docs/README.md` if the REPORT lists a row; this PLAN.
* **Already done while drafting:** `--write-index` added the rows for `0246-MADR`, `0246-PLAN`, `0247-MADR`,
  `0247-PLAN` and this pair, so the survey's C1 is closed by the commit that carries this pair (`4e085b86`). Phase F
  only confirms that no record is missing at its `HEAD`.
* **When:** after Phase C, before Phase D *(2026-11-14, OQ6's answer)*. Rows for records added later, Phase D's
  included, are added by those records' own `--write-index`.

Steps:

1. C1.
2. `python3 -B scripts/check_records.py --write-index`, then `git diff --exit-code docs/README.md`. Exit 1 means rows
   were missing. They are now added, and the execution record names them.
3. Add the README documentation-table rows for every guide it omits. At drafting:
   * `ops-codex-contract.md`;
   * `ops-hardware-validation.md`;
   * `ops-android-emulator.md`;
   * `ops-ios-signing.md`;
   * `CHANGELOG.md` (if Phase E did not add it).

   Add `0179-MADR-gobble-native-acp-provider.md` to "Architecture & key decisions", beside the other provider MADRs.
4. Add the "I want to…" rows the REPORT words. At drafting, seven guides have no route: `chat-performance.md`,
   `config.md`, `iam-route53-acme.md`, `ops-codex-contract.md`, `ops-credential-recovery.md`,
   `ops-hardware-validation.md`, `ops-mcrelay.md`. Not every guide needs a row. The REPORT says which do.
5. Checks. **Stage.**

Verification:

```bash
python3 -B scripts/check_records.py --write-index > "$T/f-wi.out" 2>&1; echo "exit=$?"; git diff --exit-code docs/README.md > "$T/f-diff.out"; echo "exit=$?"   # second exit=0 once rows are staged
python3 "$T/reach.py" > "$T/f-reach.out"; echo "exit=$?"     # every guide is in README.md's documentation table; seen failing first (C2)
python3 -B scripts/check_records.py --check-all > "$T/f-records.out" 2>&1; echo "exit=$?"   # exit=0
make markdownlint-docs > "$T/f-lint.out" 2>&1; echo "exit=$?"                               # exit=0
```

Acceptance:

* `--write-index` makes no change;
* every guide is reachable from `README.md`'s documentation table;
* every guide the REPORT marks has an "I want to…" row;
* every REPORT row whose destination is F is closed.

### Close-out (with Phase D's last commit; Phase F's before 2026-11-14's reordering)

1. In the REPORT's defect table, name each row's closing commit, or its carry-over reason.
2. MADR Confirmation 1 to 6, each with its evidence, in this PLAN's execution record.
3. This PLAN becomes `complete`, and the MADR `accepted`, once the owner has committed every phase. S1 and S2 stay in
   force after that. *(2026-11-15: the MADR became `accepted` at the owner's approval of the pair, before any phase
   ran. Only this PLAN's `complete` waits for the close-out.)* *(2026-11-15: see C5's dated annotation. By the
   coordinator's application of the owner's standing approval of 2026-11-15 ("Yes, all approved plans"), the
   coordinator commits and pushes each owner-approved phase after the disclosure guard exits 0; tags, releases and
   force pushes still need the owner's word each time. The text before this note is kept as written.)*

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 every defect closed or carried | A, close-out | the REPORT's defect table |
| 2 a changelog section per tag | E, then S2 at each tag | `CHANGELOG.md`; the release's execution record |
| 3 every later code phase records its docs step | S1 | each later PLAN's execution record |
| 4 zero missing doc comments in scope | D | D1's before and after tables |
| 5 every surface described | C | the surface check's output |
| 6 existing checks pass | every phase | `check-records`, `markdownlint-docs`, `internal/protocol` tests where relevant |

## Rollout and Rollback

* Each phase is one commit (Phase D may be several, each one directory) that the owner makes. Nothing is pushed or
  tagged by this plan. *(2026-11-15: see C5's dated annotation. By the coordinator's application of the owner's
  standing approval of 2026-11-15 ("Yes, all approved plans"), the coordinator commits and pushes each owner-approved
  phase after the disclosure guard exits 0; tags, releases and force pushes still need the owner's word each time. The
  text before this note is kept as written.)*
* Rollback is `git revert <commit>`, newest first. Phases A, B, C and F change Markdown only. Phase D changes comments
  only, so a revert cannot change behaviour.
* Revert Phase E only before any later phase has added a `CHANGELOG.md` entry. After that, a revert would drop those
  entries too. Instead, correct the file in a new commit.
* S1 and S2 are rules in this record. Withdrawing them takes an amendment of the MADR that the owner approves.

## What this PLAN does not do

* It adds no gate, no CI job, no check script and no `Makefile` target, and it changes none (D4). *(2026-11-14, OQ7,
  the owner's own exception: it changes one `Makefile` variable, `MARKDOWNLINT_DOCS_ROOTS`, so the existing
  `markdownlint-docs` gate also lints `CHANGELOG.md`. Still no new gate, CI job, check script or target, and no
  workflow change.)*
* It changes no behaviour, and it makes no decision a stale doc exposes. Those go to the owner (D8).
* It does not amend 0246's or 0247's records. Their gaining S1 and `CHANGELOG.md` in scope is a separate amendment for
  the owner.
* It does not add front matter to old records (OQ2), backfill changelog sections beyond OQ1's answer, publish
  changelog sections to GitHub Releases (OQ7), or move `AGENTS.md` to `CLAUDE.md` (OQ8).
* It does not put S1, S2 or a docs-step paragraph in `AGENTS.md` or any per-agent rule file (OQ5, as applied
  2026-11-14). Those rules live in this record and in briefs.
* It touches no other repository, and it never pushes or tags. *(2026-11-15: see C5's dated annotation. By the
  coordinator's application of the owner's standing approval of 2026-11-15 ("Yes, all approved plans"), the
  coordinator commits and pushes each owner-approved phase after the disclosure guard exits 0; tags, releases and force
  pushes still need the owner's word each time. The text before this note is kept as written.)*

## Execution record

### Drafting — 2026-11-14

* Read-only at `0fbed31b`, clean tree. The only writes: this pair, and `docs/README.md` regenerated with
  `python3 -B scripts/check_records.py --write-index`. The regeneration added the four `0246`/`0247` rows the survey
  found missing (C1), and this pair's two rows. Both staged; nothing committed.
* The doc-comment counts in Phase D's baseline come from the measurement program described in D1. It was seen failing
  first on a planted file, where it reported the five planted gaps, and it was run once over the tree. It and its
  output stay in the drafting session's scratchpad.
* The drafting session staged this pair and the index, and the owner committed them as `4e085b86`.

### Owner rulings — 2026-11-14

The coordinator relayed the owner's answers to three questions put once for all three docs plans. The MADR records
each verbatim on its question.

* OQ1: "Latest tag forward", which is (b). Phase E builds an `Unreleased` section, a `v0.26.0` section and one pointer.
* OQ3: "Fields of public and contract types". Phase A gains step 5a, and Phase D's scope and measurement follow it.
  The Dart API was not ruled on and stays out.
* OQ6: "Yes, docs-only phases now". The order table is revised: A, E, B, C and F now, then D after 0246's cut-over.
* OQ5 was not put to the owner again. The coordinator applied the owner's "Skill only" ruling on the global rules
  file, and told the owner so. Applied here as (b): factual fixes only, and S1 and S2 live in this record and in briefs.

OQ2, OQ4, OQ7 and OQ8 remain open. Both records stay `proposed`.

### Owner answers to OQ2, OQ4, OQ7 and OQ8 — 2026-11-14

The coordinator relayed the answers, each put to the owner as its own question. The MADR records each verbatim on
its question.

* OQ2: "Leave them as history". None of the 168 records changes.
* OQ4: "All that non-test code reads". Phase C follows it.
* OQ7: "Add to the lint target". This is the owner's own exception to "Fix the docs, no new checks": an existing gate
  gains one file. Phase E gains steps 5a and 5b and their verification. No workflow path list needs to change (Phase
  E, "CI paths").
* OQ8: "A separate record". It is out of this pair, and the coordinator's ledger carries it.

No question is open. Both records stay `proposed` until the owner approves the plan and says when it starts.

Phases: not started. The MADR is `proposed`.

### Owner approval — 2026-11-15

The coordinator relayed the owner's approval of the amended pair, given by choosing "Approve, one phase at a time".
The MADR is `accepted`, and Phase A runs. The plan pauses after each phase until the owner says to continue. The same
day the coordinator annotated C5 with its application of a wider owner ruling on commits and pushes (C5's dated note);
that note is the coordinator's, not a ruling of the owner's on this plan.

### Phase A — 2026-11-15

Run by a strongest-tier model at high effort, as the phase prices it, on the coordinator's brief. The result is
[0248-REPORT-documentation-defect-list.md](../reports/0248-REPORT-documentation-defect-list.md).

**Start state (C1).** Branch `master`, `HEAD` `4e085b86`, level with `origin/master`. Staged before the phase: this
pair's amendments, left staged. `git log --oneline 0fbed31b..HEAD` is `4e085b86` alone, the owner's commit of this
pair's first draft; it changed `docs/README.md` and this pair, and nothing under `internal/`, `cmd/`, `deploy/`,
`README.md` or the `Makefile`. So step 1 re-checked one lead, the survey's C1, which that commit closed. The Go files
at `HEAD` are those of `0fbed31b`.

**Steps.**

1. C1, above.
2. The leads. Every survey row (A1, A2, B1 to B8, C1 to C8), every "New, this draft" row and 0239's four open items has
   a verdict with `file:line` in the REPORT. Three MADR counts were corrected there: B3's list (11 `.PHONY` targets
   named nowhere, `run` among them, plus three targets that are not `.PHONY`), the live targets `AGENTS.md:51` omits
   (six `.PHONY`, not five), and the guides the README's documentation table omits (eight, not four). 0239's F6
   "≤60 s" and "164 methods" are refuted, the "Host dedup" row is refuted except for kilo, and the `AsyncNotifier`
   finding still holds. B7's prefix is settled: the release spec's `env_prefix`. `deploy/systemd/mcrelay.service` is
   not an orphan (`Makefile:498`; its own header), which settles Phase C step 2's question in advance.
3. The full reads. The read list is 8,623 lines of Markdown in 44 files, 9,554 with `docs/guides/config.md`; the
   PLAN's "about 9,500" counts the latter. The reference checker ran over the list plus `README.md`,
   `docs/architecture.md`, `docs/guides/config.md` and `docs/README.md`: 408 unresolved references, each read in
   context. Four read-only helper passes read the long guides in full at the same time; the executor read the rule
   files, `AGENTS.md`, `docs/architecture.md`, the example configs and the package doc comments, and re-read the cited
   lines of the helpers' load-bearing findings (the REPORT's "Basis" column says which). `doc_coverage_test.go` holds
   event types and error codes against `protocol-v1.md`, and nothing else; message types and payload fields were checked
   against `internal/protocol/messages.go` by hand. Every mcrelay flag in `internal/relay/cli.go` was checked against
   `config-mcrelay.md`.
4. The surface inventory: 6 providers, 27 commands, 82 flags, 100 variable names in non-test Go plus 99 key-derived,
   164 settings keys, 47 make targets, 6 deploy files, 56 packages. Named nowhere: 17 variables, 14 make targets, one
   deploy file. The REPORT has each list and its gaps.
5. The doc-comment baseline. Rebuilt and run; it reproduces the PLAN's table exactly. Step 5a: 182 contract types
   (150 on the wire, 32 config), no interface type, 885 exported fields of which 548 lack a comment.
6. Classify. 145 defect rows, each with a kind, a release class and a destination: wrong 59, missing 48, stale 38; B
   70, C 44, D 8, F 3, owner 20. Every row is older: the three commits since `v0.26.0` change records only. The
   replacement fact for each wrong statement and the exact Phase F rows are in the REPORT.
7. The REPORT, in 0239-REPORT's shape.
8. `python3 -B scripts/check_records.py --write-index` added the REPORT's row to `docs/README.md` and changed no other
   row. Checks, then staged.

**Measurement reading.** D1's "exported struct field" and "exported interface method" reproduce the baseline only when
they count the direct fields and methods of exported top-level types. Counting every struct and interface literal gives
3,051 / 2,348 fields and 272 / 159 methods. Phase D's D1 keeps the narrower reading.

**Seen failing first (C2)**, each in the scratchpad, never in the tree:

- the reference checker, on a planted doc with one bad and one good reference of each of its eight kinds: it reported
  the eight bad ones (`internal/nope/missing.go`, `make frobnicate`, `mcremote frobulate`, `--no-such-flag`,
  `listen.hostx`, `MCREMOTE_NOPE_VAR`, `FrobnicateWidget`, `9.9.97`) and none of the good ones. Its first run missed the
  planted version (a sentence's full stop defeated the boundary); the boundary was fixed and the plant re-run;
- the D1 measurement, on the PLAN's planted two-package tree: package comment `b`, func `Free`, method `Doc.Undoc`,
  field `Doc.Bad`, const `Loose` (in both const rows), and nothing else; with a contract list, `Wire.Body` reported and
  `Contract.Count` (named by its type's comment) not;
- the key walker, on a planted package: exactly `pre.a.x`, `pre.args`, `pre.list[].x`, `pre.y`, not the `-` field, and
  exit 3 on a missing root; its closure mode followed map values and slices and skipped an unexported field;
- the surface inventory, on a scratch copy of every Markdown file with `--trusted-proxy` removed: "flags: 82 items, 1
  named by no doc", against 0 on the real docs;
- the example-config key check (`listen.hostx` reported, exit 1), the front-matter count (a planted record raised it to
  169), the guide map (a README without `config.md` raised the count to 9), the package-comment lister (the planted
  undocumented package not listed), the defect-row validator (a planted row of kind `broken` rejected, exit 1), and the
  citation checker below.

**Verification**, after the last edit (`$T` is the scratchpad):

```text
python3 -B scripts/check_records.py --check-all > "$T/a-records.out" 2>&1; echo "exit=$?"   exit=0, file empty
make markdownlint-docs > "$T/a-lint.out" 2>&1; echo "exit=$?"                               exit=0, "Linting: 61 files", "Summary: 0 issues in 0 files"
git diff --cached --name-only > "$T/a-staged.out"; echo "exit=$?"                           exit=0, four files (below)
python3 ~/.global-git-hooks/github-disclosure.py redact > "$T/a-redact.out" 2>&1; echo "exit=$?"   exit=0, "would edit 0 file(s), 0 path(s) need a rename"
make check-records > "$T/a-mk-records.out" 2>&1; echo "exit=$?"                             exit=0
python3 "$T/cites.py" . <the REPORT> <this PLAN> <the MADR> > "$T/a-cites.out" 2>&1; echo "exit=$?"   exit=1, 435 checked, 1 bad (below)
```

The one citation the checker cannot place is in this PLAN's own Phase B text from drafting, which cites line 468 of
`config.md` by its basename; two tracked files have that name. Read by hand, line 468 of `docs/guides/config.md` is
the row it means. Every citation this
phase wrote resolves.

The citation checker resolves every `path:line` (and following `:line`) in the three records and fails on a line beyond
the file's end. On a planted file it reported exactly the four planted bad citations: a line beyond the
README's end, a range beyond `load.go`'s end, a bare continuation line beyond the README's end, and a file
that does not exist.

**Deviations.** None that stopped the phase. Two notes. The staged set is four files, where the PLAN's verification
names three: the MADR was staged before the phase with the pair's amendments, and the coordinator's brief kept it
staged and asked for its `accepted` status. And beyond step 8's files, the brief asked for this PLAN's status, the C5
note, and dated notes where the approval made a sentence false (the Goal's status paragraph, the close-out's step 3,
and the MADR's Decision Outcome); those were made. The new REPORT was added to the index before the first redact
preview, because the preview reads tracked files only; the preview ran again before the rest was staged.

**Not verified.** Helper findings marked "helper" in the REPORT were not re-read by the executor. Behaviour that needs a
live run or a device (the hardware rows, upstream agents' behaviour, Gradle against JDK 26, the system unit under
`ProtectSystem=strict`) was not run. Only the `v0.26.0` GitHub Release body was opened. No build, test or suite run was
made.

**Files.** `docs/reports/0248-REPORT-documentation-defect-list.md` (new); `docs/README.md` (one generated row); this
PLAN; the MADR (status, date, one dated note).

Staged, not committed; the coordinator commits and pushes after the disclosure guard (C5's annotation); the plan pauses
until the owner says to continue.

### Phase A, review follow-up — 2026-11-15

On the coordinator's review, the same executor closed the rows Phase A had left open:

- Every sentence of this PLAN that says who commits or whether anything is pushed now carries a dated note pointing to
  C5's annotation: the Scope's out-of-scope list, the close-out's step 3, Rollout and Rollback's first bullet, and the
  last bullet of "What this PLAN does not do". The MADR has no such sentence. The drafting record's past-tense lines
  (the owner's commit of `4e085b86`) stay as history.
- Phase A's verification block gains a dated note: the staged set was four files, the fourth the MADR (see
  "Deviations" above).
- Phase B's drafting citation of line 468 of `config.md` gains a dated note giving `docs/guides/config.md:468`, read by
  hand as the `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` row it means.
- The citation checker now accepts an ambiguous basename only when the same line also cites the full path with the same
  line numbers. On a planted file it still failed a bare ambiguous citation and one whose full path gave a different
  line, and passed the one disambiguated in place.

Re-run after the last edit: `check_records.py --check-all` exit 0, `make check-records` exit 0, `make
markdownlint-docs` exit 0, the redact preview exit 0 with no file to edit, the citation checker exit 0. The earlier run
recorded above (exit 1, one bad citation) stands as the record of that run.
