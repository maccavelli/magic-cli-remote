---
status: accepted
date: 2026-08-19
decision-makers: Project Owner
consulted: none
informed: every agent working in this repository
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Require an approved MADR and PLAN before any mutating work

## Context and Problem Statement

This repository already numbers architectural decisions and
implementation plans (`docs/NNNN-MADR-*.md`, `docs/NNNN-PLAN-*.md`;
see `AGENTS.md` "File naming"). That convention records *what was
decided* once someone writes a pair. It does not require a pair
*before* an agent edits the tree.

The gap is audit, not naming. A session can change Go, Dart, scripts,
CI, or config, commit it, and leave no MADR/PLAN that a later
maintainer can use to reconstruct why. Follow-up debugging then
lands as untracked patches. Home-directory agent rules (Grok
`~/.grok/rules/global-agent-rules.md` and equivalents) already
describe a MADR-then-plan-then-execute workflow, but those files are
**per machine, not per repo**. A checkout on another host, or an
agent that does not load that home rule, is unconstrained.

The owner required: every **read-write / mutating** action in this
repo is tracked for audit, reference, documentation, and
completeness. Follow-up work on an existing topic amends that
topic's MADR and PLAN. Greenfield work needs a **new** numbered pair
before any mutation.

* Implementation Plan: [0105-PLAN-mutating-work-requires-madr-and-plan.md](0105-PLAN-mutating-work-requires-madr-and-plan.md)

### Assessment: what already exists

| What | Where | What it does |
|---|---|---|
| Numbered `docs/` files | `AGENTS.md` "File naming: MADR and plan files" | How to name a pair. No gate on when one is required. |
| Home MADR workflow | `~/.grok/rules/global-agent-rules.md` (and Claude/Codex copies) | Investigate → write MADR `proposed` → wait → PLAN → wait for execute approval. Not in this git tree. |
| Commit-message hook | `~/.grok/rules/git-prepare-commit-msg.md`; `AGENTS.md` | How to commit, not whether the commit has a MADR. |
| Pre-add Go/Dart checks | `scripts/go-precheck.sh`; `~/.global-agent-hooks/` | Format/lint/vuln. Not documentation completeness. |
| Grok project rules | `AGENTS.md` (loaded as `Agents.md`); `<repo>/.grok/rules/*.md` | In-repo instructions every Grok session in this tree sees. |

There is no git hook that can prove a commit "has a MADR". A hook
that required `docs/` in every commit would reject a later phase of
an already-approved plan that only touches `internal/`. The gate
belongs in agent instructions, the same way `git commit --no-edit`
is a rule and not a hook.

### Assessment: mutating vs read-only

Mutating (requires a current approved PLAN, or an approved amendment
to one):

* Creating, editing, or deleting files in the working tree
* `git add` / `git commit` / rebase / amend (except as listed below)
* Dependency or lockfile changes
* Config, CI, workflow, or hook changes
* Builds or installers that write into the tree, `$HOME`, or a live
  service (`make install`, `scripts/install.sh` against a real
  prefix, `setup-service` on a real label)
* Generating or regenerating committed artifacts

Read-only (no new pair required):

* Reading, searching, listing, `git log` / `git show` / `git diff`
* Running existing tests or diagnostics that do not write the tree
* Fetching documentation
* Drafting a MADR or PLAN in the conversation before it is written
  to disk — writing those files is the bootstrap exception below

### Assessment: bootstrap exception

A process that forbids file writes until a MADR exists cannot create
its first MADR. Authoring or amending `docs/NNNN-MADR-*.md` and
`docs/NNNN-PLAN-*.md` (and the in-repo rule files that point at
them) is allowed without a *prior* pair. Committing those docs is
part of that exception. Source, tests, CI, and config are not.

## Decision Drivers

* Mutating work in this repo must be reconstructible from `docs/`.
* The requirement must live **in the repository**, so every agent
  and every clone sees it — not only hosts with a particular home
  rule set.
* Follow-up debugging on a live topic must not spawn a new number
  for every patch; it amends the existing pair.
* Greenfield work must not start as "a small edit" that never gets a
  record.
* Read-only investigation stays cheap. The gate is on mutation.
* The gate is a rule agents honour, not a git hook that guesses
  intent from the files in an index.

## Considered Options

* **R1 — Status quo: home-directory MADR workflow only**
* **R2 — In-repo `AGENTS.md` section only**
* **R3 — In-repo `AGENTS.md` plus `.grok/rules/` (chosen)**
* **R4 — Git hook that rejects commits with no `docs/` path**
* **R5 — New MADR number for every commit, including plan phases**

## Decision Outcome

Chosen option: **"R3 — put the gate in `AGENTS.md` and
`.grok/rules/`"**, because `AGENTS.md` is already the
always-applied workspace instruction for agents in this tree, and
`.grok/rules/*.md` is the Grok-native project-rules directory. Together
they cover this repo without depending on `~/.grok/rules`. A git
hook cannot tell an approved Phase 3 of 0104 from untracked work.

Concretely:

1. **No mutating work without a current pair.** Before the first
   write, the agent names the `NNNN-MADR-*` / `NNNN-PLAN-*` it is
   executing, or it stops and writes one.
2. **Order.** Investigate (read-only) → write or amend the MADR
   (`status: proposed` unless the owner has already decided) →
   present it → write or amend the PLAN → present it → **mutate only
   after the owner explicitly approves execution**. Stay inside that
   PLAN. Anything discovered mid-execution that is out of scope
   waits: amend the pair, re-approve, then continue.
3. **Follow-up vs greenfield.**
   * Same topic (debug, regression, leftover phase, installer
     footgun found in that plan's live run): **amend** that number.
     Do not silently rewrite historical rationale; add an amendment,
     Observed, or a new PLAN phase.
   * New topic: next unused `NNNN`, new MADR, new PLAN, same slug.
     No mutation until that PLAN is approved.
4. **What "approved" means.** The owner saying to write the MADR, or
   the owner saying "proceed" / "execute the plan" / "do phase N".
   Completing a previous phase is not permission to start an
   unwritten next one. Pushing and tagging remain explicit and
   separate (`AGENTS.md` already forbids unsolicited `git push`).
5. **Bootstrap exception.** Creating or editing `docs/NNNN-MADR-*`,
   `docs/NNNN-PLAN-*`, `AGENTS.md` process text, and
   `.grok/rules/` process rules does not itself require a prior
   pair. Using that exception to sneak source changes into the same
   commit is a violation.
6. **Naming unchanged.** Zero-padded four-digit prefix, shared
   number, kebab-case slug, MADR v4 headings. This record does not
   reopen 0097-style content; it only gates *when* a pair is
   required.

### Consequences

* Good, because the audit trail is in git, next to the code, not in
  a home directory the next clone does not have.
* Good, because follow-up work stays attached to the decision it
  implements (0104 Phase 6 amending 0104, not a new 0106 for a
  `--dir` footgun found in 0104's live run).
* Good, because read-only investigation is unchanged.
* Neutral, because home-directory MADR rules remain; this record
  does not delete them. When they disagree, **this repo's
  `AGENTS.md` wins inside this tree**.
* Bad, because a one-line typo fix still needs a pair or an
  amendment. That is the cost of the audit requirement. A typo in
  an in-flight PLAN's files can be folded into that PLAN's current
  phase.
* Bad, because honour-based rules can be ignored by a human `git
  add` in a raw terminal, same as the pre-add Go gate. There is no
  hook. Completeness is checked by reading `docs/`, not by CI
  blocking merges.

### Confirmation

The decision is satisfied when:

* `AGENTS.md` states the mutating-work gate, the follow-up/greenfield
  split, the bootstrap exception, and the approve-before-execute
  order.
* `.grok/rules/madr-plan-before-mutating-work.md` exists and states
  the same gate so a Grok session that loads project rules sees it
  even if it skims `AGENTS.md`.
* A later agent session in this repo, asked to change source without
  a pair, writes or names a MADR/PLAN instead of editing first.
* Follow-up on an open numbered topic amends that pair rather than
  allocating `NNNN+1` for a bug found in that topic's live run.

## Pros and Cons of the Options

### R1 — Home rules only

* Good, because it is zero repo work; some agents already honour it.
* Bad, because it is invisible in git and absent on a machine
  without those files.
* Bad, because it does not meet the owner's "in this workspace"
  requirement.

### R2 — `AGENTS.md` only

* Good, because Grok already always-applies `Agents.md` in this
  workspace, and other agents that read `AGENTS.md` get it.
* Bad, because Grok also loads `.grok/rules/` independently; a
  rule that exists only in `AGENTS.md` is easier to miss when the
  session is rule-directory oriented.
* Neutral, because one file is simpler than two. Duplication is
  the cost of R3.

### R3 — `AGENTS.md` plus `.grok/rules/` (chosen)

* Good, because it is in the clone, for every agent that reads
  `AGENTS.md` and for Grok's project-rules scan.
* Good, because it matches how this repo already teaches
  `git commit --no-edit` (rule in `AGENTS.md`, not a hook).
* Bad, because the text exists in two places and can drift. The
  PLAN requires the same gate in both; the MADR is the rationale,
  not a third copy of the checklist.

### R4 — Git hook on `docs/` in every commit

* Good, because a raw-terminal `git add` would be forced to include
  something under `docs/`.
* Bad, because a PLAN phase that only changes `internal/` would
  have to touch docs unnaturally, or the hook would have to parse
  intent it cannot see.
* Bad, because this repo deliberately has **no** commit-time hook
  (`AGENTS.md` pre-add rule). Adding one for docs would re-open
  that cut.

### R5 — New number per commit

* Good, because the audit grain would be one commit.
* Bad, because 0104's six phases would have been six MADRs for one
  decision, which is exactly the historical-rationale problem MADR
  v4 tells us not to create.

## More Information

### Relationship to existing process

This record does not replace the pre-add Go/Dart checks, the
prepare-commit-msg hook, or the no-unsolicited-push rule. It adds
a documentation prerequisite in front of mutation.

Home-directory copies of the MADR workflow stay useful for *other*
repos. Inside this repository, `AGENTS.md` is authoritative.

### What this record does not change

* MADR v4 heading names and `Good, because` / `Bad, because` form.
* Sequential zero-padded identifiers. Next unused number after this
  record is **0106**.
* Live-CLI probe evidence still belongs in the MADR and a live-tagged
  test (`AGENTS.md` file-naming paragraph).
* `git push` and tags still require an explicit ask in the same turn.
