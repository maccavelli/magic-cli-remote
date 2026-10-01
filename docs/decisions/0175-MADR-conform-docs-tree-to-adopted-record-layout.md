---
status: accepted
date: 2026-10-01
informed: repository agents
---

<!-- markdownlint-disable MD013 MD041 -->

# Conform the docs tree to the adopted record layout

## Context and Problem Statement

The owner adopted new documentation standards, carried by two skills: **`madr-and-plan-writing`** (record format, naming, numbering) and **`documentation-writing`** (docs-tree layout). Both local skill roots (`~/.claude/skills/` and `~/.agents/skills/`) hold identical copies of both (verified by diff, 2026-10-01), and both specify the same fixed tree:

```
README.md            ← beside the docs/ tree; links to docs/README.md
docs/
  README.md          ← ToC and the "I want to…" matrix
  architecture.md
  decisions/   NNNN-MADR-*.md  NNNN-PLAN-*.md
  reports/     NNNN-REPORT-*.md  NNNN-GATES-*.md
  guides/      user documentation library, unnumbered
```

`docs/` holds only those two files and those three subdirectories, and a record never sits directly in a `docs/`.

The repository does not match the standard. Evidence from a read-only survey, 2026-10-01:

* **291 records (0001–0163) still sit in `docs/spec/`** — the location 0158 chose. The eleven newest pairs (0164–0174, committed 2026-09-30) already sit in `docs/decisions/`, so every record written since the standard was adopted already follows it; only the backlog has not moved.
* **28 files sit directly in `docs/`**, which the layout forbids: 3 numbered findings reports, 4 legacy records behind non-conforming filenames, and 21 guide/reference documents; plus 5 spike evidence directories and a `standards/` subtree, none of which are one of the three sanctioned subdirectories.
* **The record sequence has two pre-existing collisions.** Two records claim 0004: `docs/MADR-certificate-management-decision.md` (titled "# 0004 — Certificate management") and `docs/MADR-phase2-grok-acp.md` (titled "MADR 0004: Phase 2"). The hardening series 0006–0008 cites 0004 and 0005 as certificate management and client identity, so the phase records are the collisions. `docs/PLAN-flutter-android-client-assessment.md` is likewise cited both as "PLAN 0005" (0018, 0026) and as a phase-3 plan.
* **No `docs/README.md` and no `docs/architecture.md`** — both required by the layout.
* **No records tooling.** The standard expects `scripts/check_records.py` (`--next` for numbering, `--write-index` for the ToC, a link check for moved records); the repository has none.
* **Doc paths are load-bearing beyond markdown.** A live test reads one from disk (`internal/protocol/doc_coverage_test.go` → `docs/protocol-v1.md`); help strings print others (`docs/config.md`, `docs/receipts.md`, `docs/ops-macos-tcc.md`); `README.md` (~50 links), `Makefile`, `scripts/install.sh`, `.github/workflows/ci.yml`, `configs/*.yaml`, and Go and Dart comments all point readers at `docs/` paths that move.
* **Agent-facing text still teaches the old layout.** `AGENTS.md` (the rationale link to `docs/spec/0105-MADR-…`, the gate's `docs/NNNN-MADR-*` paths, and "All files in `docs/` use a zero-padded 4-digit prefix"), `.grok/rules/madr-plan-before-mutating-work.md`, and `.claude/rules/madr-and-plan-skill.md` all point records at `docs/spec/` or bare `docs/` paths. `.opencode/rules.md` names no path and stays accurate.

## Decision Drivers

* The two skills are adopted and normative; where they and the tree disagree, the tree is wrong.
* A move's whole risk is link integrity, and repair must be driven by a checker's output, never by pattern-matching (`documentation-writing`).
* Numbering is repository-wide, never reused, and a gap stays a gap; two records cannot both be 0004.
* `go test ./...` must stay green: a protocol guard reads a doc from disk, so a move that forgets it turns a docs change into a broken build.
* Historical rationale is never rewritten: 0158 keeps its story; a later record supersedes it and says so.
* A record never sits directly in `docs/`: any deferred piece of this migration leaves the repo non-compliant by the standard's lead rule.

## Considered Options

* Conform the whole tree in one phased migration: records, legacy records, reports, guides, standards, spikes, scaffold, and tooling.
* Conform records only: move `docs/spec/` to `docs/decisions/` and defer everything that is not a MADR/PLAN.
* Conform records and split the mobile stack into its own tree (`apps/mobile/docs/`) at the same time.

## Decision Outcome

Chosen option: "Conform the whole tree in one phased migration", because every deferred piece leaves files directly in `docs/` — the exact rule the standard leads with — and each later pass would re-walk the same reference-repair surface at higher cost.

The mapping, executed by [0175-PLAN-conform-docs-tree-to-adopted-record-layout.md](0175-PLAN-conform-docs-tree-to-adopted-record-layout.md):

* **D1 — Records.** `git mv` all 291 `docs/spec/` records into `docs/decisions/` in one operation (every MADR/PLAN pair moves together), then supersede 0158: its MADR and PLAN get `status: superseded`, pointing here. Their rationale text stays as written.
* **D2 — Legacy records take real numbers.** The hardening series keeps its numbers: `docs/MADR-certificate-management-decision.md` becomes `0004-MADR-certificate-management.md` and `docs/MADR-client-identity-decision.md` becomes `0005-MADR-client-identity.md` — their titles already claim these numbers and 0006–0008 continue the series. The two colliding phase records take the next unused numbers with dated in-file renumber notes: `docs/MADR-phase2-grok-acp.md` becomes `0176-MADR-phase-2-grok-acp-provider.md`, and `docs/PLAN-flutter-android-client-assessment.md` becomes `0177-PLAN-flutter-android-client-assessment.md` (a lone PLAN that predates the pairing rule; its note says so). Numbers are unique, not chronological.
* **D3 — Findings become REPORTs.** `docs/0098-findings-install-verification-sweep.md`, `docs/0099-findings-reverification.md`, and `docs/0100-findings-update-refresh.md` move to `docs/reports/` keeping their numbers, renamed with the kind infix: `0098-REPORT-install-verification-sweep.md`, `0099-REPORT-reverification.md`, `0100-REPORT-update-refresh.md` — each is the observation record for the decision of the same number.
* **D4 — Assessments become REPORTs.** `docs/mobile-ux-assessment.md` is an observation that decides nothing, associated with no single record → `docs/reports/0178-REPORT-mobile-ux-assessment.md` (next free number at execution, from `--next`). `docs/agent_cli_slash_commands_matrix.md` records probe evidence about the canonical-slash-commands surface → `docs/reports/0023-REPORT-agent-cli-slash-commands-matrix.md` (a report about record 0023 takes its number). Both are judgment calls; if the owner redirects either, that is recorded as a deviation, not silently redone.
* **D5 — Guides.** The remaining 19 reference documents (`ops-*.md`, `protocol-v1.md`, `protocol-v2.md`, `config.md`, `config-mcrelay.md`, `headscale.md`, `iam-route53-acme.md`, `receipts.md`, `chat-performance.md`, `mobile-profiling.md`) move to `docs/guides/` unnumbered, and `docs/standards/{go,mobile}` moves to `docs/guides/standards/{go,mobile}` — a language standard is a document a reader follows, which is the guide definition.
* **D6 — Spike evidence.** The five `*-spike-*` directories are investigation artifacts (raw probe output) → `docs/reports/`, beside the numbered REPORT records. The reports pattern in the standard's sketch covers `NNNN-REPORT/GATES-*.md`; raw evidence directories sitting alongside them is this repository's recorded divergence from that sketch.
* **D7 — One tree; mobile split deferred.** The mobile stack is part of this repository's purpose, its records share the root sequence, and no mobile-only records tree has accreted; the standard says not to split until one does. When it does, splitting costs file moves, never a renumber. `guides/standards/mobile/` is the seed.
* **D8 — Tooling and scaffold.** Add `scripts/check_records.py` (`--next`, `--check`, `--check-all`, `--write-index`) and `make check-records`; scaffold `docs/README.md` (generated ToC plus a hand-written "I want to…" matrix) and a real `docs/architecture.md`; root `README.md` links `docs/README.md`. Wiring the checker into CI or `make preflight` is deliberately deferred to a later pair — this one changes enough gates already.

## Pros and Cons of the Options

* **Conform the whole tree in one phased migration.**
  * Good, because every phase ends with the checker green and one commit, so the tree is never half-conformed between commits.
  * Good, because one checker-driven repair pass covers all moves, instead of several passes over the same files.
  * Bad, because it touches a live Go test, help strings, `configs/*.yaml`, CI wording, and ~300 markdown files in one pair; only the per-phase gates keep that blast radius safe.
* **Conform records only.**
  * Good, because the diff is one `git mv` plus link repair, and nothing outside markdown moves.
  * Bad, because 28 files, five spike directories, and a `standards/` tree still sit directly in `docs/`, which the standard forbids outright — the repo stays non-compliant and the next pass repeats the setup cost.
  * Neutral, because the numbering collisions stay hidden as long as the legacy records keep their non-conforming filenames.
* **Split mobile into its own tree now.**
  * Good, because mobile is a distinct stack with its own standards library, and a tree per adjacent stack is the standard's shape for that.
  * Bad, because mobile records are interleaved in the one root sequence, no mobile-only tree has accreted, and the standard says not to split until one does.
  * Bad, because it roughly doubles the moves and the link-repair surface of this pair for no compliance gain.

## Consequences

* `docs/spec/` disappears; external bookmarks and deep links to it break. `git log --follow` keeps resolving moved-file history because every move is a `git mv`.
* A live protocol guard keeps pinning the wire contract: its doc-path const moves with `protocol-v1.md`, and the test fails loudly if they diverge.
* Every future record is written to `docs/decisions/` and takes its number from `scripts/check_records.py --next`; `AGENTS.md` and the per-agent rules teach that path once this pair executes.
* The ToC in `docs/README.md` is generated, never hand-edited — hand edits are silently reverted by the next `--write-index` run.
* Repair is checker-driven: a link that resolves to the wrong file passes a regex and fails a reader, so the plan repairs from `check_records.py` output, never from a path substitution.

## More Information

* The adopted standards: skills `madr-and-plan-writing` and `documentation-writing` (verified identical across both local skill roots, 2026-10-01).
* Supersedes [0158-MADR-move-madr-plan-files-to-docs-spec.md](0158-MADR-move-madr-plan-files-to-docs-spec.md) — the `docs/spec/` location decision and its plan.
* Executed by [0175-PLAN-conform-docs-tree-to-adopted-record-layout.md](0175-PLAN-conform-docs-tree-to-adopted-record-layout.md).
