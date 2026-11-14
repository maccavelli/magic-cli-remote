---
status: proposed
date: 2026-11-14
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
* the standing rules S1 and S2 below are in force for every later phase and tag.

This plan is `proposed`. Nothing below starts until the owner accepts the MADR and approves this PLAN, whole or by
phase. Phases that depend on an open question say which one.

## Scope

* **Docs:** `README.md`; `docs/README.md` (generated rows by `--write-index`, and the hand-written "I want to…"
  table); `docs/architecture.md`; `docs/guides/**`; `apps/mobile/README.md`; `apps/mobile/docs/**` except records.
* **New:** `CHANGELOG.md` (Phase E); `docs/reports/0248-REPORT-documentation-defect-list.md` (Phase A).
* **Go doc comments only:** `.go` files under `cmd/`, `internal/`, `scripts/`, `test/` (Phase D). No other change to
  any line of code.
* **Process text, only as OQ5 allows:** `AGENTS.md`, `.opencode/rules.md`, `.kilo/agent/madr-and-plan-skill.md`.
* **Records:** this pair (execution record and deviations); the REPORT.
* **Out of scope:** any behaviour change; the `Makefile`, `make/*.mk`, `.github/workflows/*`, `.markdownlint-cli2.jsonc`
  and `scripts/*` (D4: no new check and no changed gate; the stale "MagicTools MCP fleet READMEs" comment in
  `.markdownlint-cli2.jsonc:2` is listed in the REPORT, not changed); front matter of old records (OQ2); `CLAUDE.md`
  layout (OQ8); 0246's and 0247's records (their amendment for D5 is the owner's to approve separately); any other
  repository; tags, pushes and GitHub Release bodies.

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
* **C6. Identifiers.** No hostname, account name, email address or real-machine path in any committed file. Use
  placeholders.
* **C7. Deviations.** Anything that differs from this plan stops the phase. It gets a dated entry here, and an
  amendment in the MADR where a decision or fact changes, before work continues.
* **C8. Stop condition.** An unexpected state, or the same step failing twice, ends the task. The executor returns
  the state and its evidence and does not improvise or try a third time.

## Standing rules (MADR D5, D6)

These are not phases. They bind every later brief in this repository from the day the owner approves this plan.

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
  4. Lint `CHANGELOG.md` by hand with the repository's configuration (OQ7 (a)).
  5. Record steps 1 to 4 in that release's PLAN execution record.

  Older gaps found in step 2 that the release did not cause are added to Phase A's REPORT, not fixed before the tag.

## Order

| Order | Phase | When | Waits on |
| :--- | :--- | :--- | :--- |
| 1 | A — verify and list every defect | now | owner approval |
| 2 | E — `CHANGELOG.md` | after A; before the next phase that changes behaviour in this repository (0246's P1, or whatever lands first) | OQ1 |
| — | 0246 and 0247's phases, each with S1; the next tag with S2 | the owner's order in 0246-PLAN's Scope | those records' own approval |
| 3 | B — wrong statements | after the next tag (D6), or earlier if OQ6 is (b) | OQ5 for the process-text rows |
| 4 | C — missing feature docs | after B | OQ4 |
| 5 | D — doc comments | after 0246's cut-over (it edits Go files 0246 rewrites) | OQ3 |
| 6 | F — index currency | last | none |

Phase letters follow the brief that commissioned this pair. Execution order is the table's.

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

Acceptance:

* every lead in step 2 has a verdict with a `file:line`;
* every file in the read list is named in the REPORT with its result;
* every inventory item has a description location, or a defect row;
* every defect row has a kind, a release class and a destination;
* the baseline table is recorded.

### Phase E — `CHANGELOG.md`

* **Model class and effort:** mainstream, medium for OQ1 (b), where the `v0.26.0` section is written from 30
  commits and their records. Lightweight for OQ1 (a). OQ1 (c) is a judgment pass over 1,806 commits: strongest, high,
  and split into several commits by tag range.
* **Waits on:** OQ1. **Must land before** the next phase that changes behaviour in this repository (D6).
* **Files:** `CHANGELOG.md` (new); `README.md` (one row in the documentation table); `docs/README.md` (one "I want
  to…" row: "see what changed in a release"); `AGENTS.md` (one paragraph stating S1 and D3, only if OQ5 is (a)); this
  PLAN.

Steps:

1. C1.
2. Write the header. It covers what the file covers (D3: `mcremote`, `mcrelay`, installers, units, and the phone app
   built with the tag) and states the form (Keep a Changelog 1.1.0). Say that versions are this repository's `vX.Y.Z`
   tags. Do not claim Semantic Versioning, which no record adopts. Point to the GitHub Releases page (the URL the
   README's install section already uses) and to `docs/README.md` for releases before the first section.
3. `## [Unreleased]`, with an entry for each commit since `v0.26.0` that a user can see. At `0fbed31b` there are none:
   both commits are records only.
4. Per OQ1:
   * (a) nothing more;
   * (b) `## [v0.26.0] - 2026-11-12`, built from `git log v0.25.1..v0.26.0` and the records those commits cite, under
     Added, Changed, Fixed and Removed, each entry linking its record by full filename;
   * (c) a section for every tag.
5. The README row and the "I want to…" row, and the `AGENTS.md` paragraph if OQ5 is (a).
6. Checks. **Stage.**

Verification:

```bash
test -f CHANGELOG.md; echo "exit=$?"                                                     # exit=0
grep -c '^## \[Unreleased\]$' CHANGELOG.md > "$T/e-unrel.out"; echo "exit=$?"            # exit=0, count 1
markdownlint-cli2 --no-globs CHANGELOG.md > "$T/e-lint.out" 2>&1; echo "exit=$?"   # exit=0; same tool, config and --no-globs as the markdownlint-docs target, at its pinned version via npx
python3 "$T/linkcheck.py" CHANGELOG.md > "$T/e-links.out"; echo "exit=$?"               # exit=0; seen failing first on a planted dead link (C2)
python3 -B scripts/check_records.py --check-all > "$T/e-records.out" 2>&1; echo "exit=$?"   # exit=0
make markdownlint-docs > "$T/e-mdl.out" 2>&1; echo "exit=$?"                            # exit=0
```

`check_records.py` does not read `CHANGELOG.md`, which is why the one-off link check exists. It is not committed.

Acceptance:

* the file exists with exactly one `Unreleased` section;
* for OQ1 (b), every non-docs commit in `v0.25.1..v0.26.0` maps to an entry or to a stated "not user-visible" in the
  execution record;
* every link resolves;
* the lint is clean.

### Phase B — wrong statements

* **Model class and effort:** mainstream, medium. Each fix is specified in the REPORT with its evidence and the
  correct fact.
* **Waits on:** the next tag (D6), or OQ6 (b). OQ5 for the process-text rows.
* **Files:** those named in the REPORT's "wrong" rows. Known at drafting:
  * `README.md:773` (A1: give `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` its own row, as `config.md:468` does);
  * the closed provider lists at `README.md:241`, `:264`, `:273`, `:1466`, `:1612`, `docs/architecture.md:37`, `:81`
    and `apps/mobile/README.md:13` (A2: add gobble). The provider matrix row is Phase C's;
  * with OQ5 (a) or (b): `.opencode/rules.md:36`, `.kilo/agent/madr-and-plan-skill.md:41` (`docs/decisions/`), and
    `AGENTS.md:51` (name every `live-*` target, or point to the README's Development table).

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
* **Waits on:** B; OQ4.
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
* **Waits on:** OQ3; 0246's cut-over, since 0246 rewrites `internal/provider/*/auth.go`, `internal/providerauth/` and
  `internal/protocol/`.
* **Files:** the `.go` files the measurement lists, comment lines only. If OQ3 is (ii) or wider, the work is split into
  one commit per top-level package directory (D-1, D-2, …). Each sub-commit is a unit that passes this phase's
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
   * const and var, counted twice: "own comment" (spec doc or trailing comment), and "own or group comment".

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
3. Write the comments for every category OQ3 places in scope. Follow the Go convention: the comment starts with the
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
  `0247-PLAN` and this pair, so the survey's C1 is closed by the commit that carries this pair. Phase F only confirms
  that no record is missing at its `HEAD`.

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

### Close-out (with Phase F's commit)

1. In the REPORT's defect table, name each row's closing commit, or its carry-over reason.
2. MADR Confirmation 1 to 6, each with its evidence, in this PLAN's execution record.
3. This PLAN becomes `complete`, and the MADR `accepted`, once the owner has committed every phase. S1 and S2 stay in
   force after that.

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
  tagged by this plan.
* Rollback is `git revert <commit>`, newest first. Phases A, B, C and F change Markdown only. Phase D changes comments
  only, so a revert cannot change behaviour.
* Revert Phase E only before any later phase has added a `CHANGELOG.md` entry. After that, a revert would drop those
  entries too. Instead, correct the file in a new commit.
* S1 and S2 are rules in this record. Withdrawing them takes an amendment of the MADR that the owner approves.

## What this PLAN does not do

* It adds no gate, no CI job, no check script and no `Makefile` target, and it changes none (D4).
* It changes no behaviour, and it makes no decision a stale doc exposes. Those go to the owner (D8).
* It does not amend 0246's or 0247's records. Their gaining S1 and `CHANGELOG.md` in scope is a separate amendment for
  the owner.
* It does not add front matter to old records (OQ2), backfill changelog sections beyond OQ1's answer, publish
  changelog sections to GitHub Releases (OQ7), or move `AGENTS.md` to `CLAUDE.md` (OQ8).
* It touches no other repository, and it never pushes or tags.

## Execution record

### Drafting — 2026-11-14

* Read-only at `0fbed31b`, clean tree. The only writes: this pair, and `docs/README.md` regenerated with
  `python3 -B scripts/check_records.py --write-index`. The regeneration added the four `0246`/`0247` rows the survey
  found missing (C1), and this pair's two rows. Both staged; nothing committed.
* The doc-comment counts in Phase D's baseline come from the measurement program described in D1. It was seen failing
  first on a planted file, where it reported the five planted gaps, and it was run once over the tree. It and its
  output stay in the drafting session's scratchpad.

Phases: not started. The MADR is `proposed`.
