# Implement mutating-work MADR/PLAN gate

Associated MADR: [0105-MADR-mutating-work-requires-madr-and-plan.md](0105-MADR-mutating-work-requires-madr-and-plan.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

## Goal

Every mutating action in this repository is preceded by a named,
owner-approved `NNNN-MADR-*` / `NNNN-PLAN-*` pair (new, or an
amendment to an existing pair). The requirement lives in the clone,
in `AGENTS.md` and `.grok/rules/`.

## Scope

**In**

* `AGENTS.md` — operational gate next to the existing MADR naming
  section.
* `.grok/rules/madr-plan-before-mutating-work.md` — Grok project
  rule stating the same gate.
* This numbered pair in `docs/`.

**Out**

* Git hooks.
* Changes to `~/.grok/rules/` or other home-directory agent config.
* CI that parses commits for a `docs/` path.
* Renumbering existing MADRs.

## Implementation Steps

### Phase 1 — Rule text in the repo

1. Add section **MADR and PLAN before mutating work** to `AGENTS.md`
   immediately before **File naming: MADR and plan files**. Keep
   the naming section; do not merge it away. The new section must
   state, as instructions not prose:

   * Read-only investigation is allowed with no pair.
   * Mutating work names the pair it is executing, or stops and
     writes one.
   * Order: MADR (proposed) → owner review → PLAN → owner
     `proceed` / execute approval → then mutate inside that PLAN.
   * Follow-up on the same topic amends that number (new phase /
     Observed / amendment). Do not rewrite historical rationale.
   * Greenfield: next unused `NNNN`, new files, no mutation until
     the PLAN is approved.
   * Bootstrap exception: authoring `docs/NNNN-MADR-*`,
     `docs/NNNN-PLAN-*`, this `AGENTS.md` section, and
     `.grok/rules/` process rules. Not source, tests, CI, or
     product config in the same breath.
   * Completing a phase is not permission to invent the next
     unwritten one. `git push` / tags remain explicit.

2. Create `.grok/rules/madr-plan-before-mutating-work.md` with the
   same gate, short enough to load as a rule, pointing at
   `docs/0105-MADR-mutating-work-requires-madr-and-plan.md` for
   rationale. Do not fork a different workflow there.

3. Verify:

   * Both files name the follow-up vs greenfield split.
   * Both files name the bootstrap exception.
   * `AGENTS.md` still contains the 4-digit naming rules unchanged
     in substance.
   * `ls .grok/rules/*.md` shows the new file.

4. **Commit** this phase (`git commit --no-edit`; no `-m`). No Go
   files; `make pre-add-check` is not required. Do not push unless
   asked.

## Verification

| Check | Pass |
|---|---|
| Pair exists as `0105-MADR-mutating-work-requires-madr-and-plan.md` and `0105-PLAN-mutating-work-requires-madr-and-plan.md` | same slug, same number |
| `AGENTS.md` gate section present | before file-naming |
| `.grok/rules/madr-plan-before-mutating-work.md` present | loaded by Grok project-rules scan |
| No git hook added | `ls .git/hooks` unchanged by this plan |
| No Go/Dart/product source in the commit | docs + rules only |

## Rollout and Rollback

**Rollout.** One commit on `master`. Takes effect for the next
agent session that loads project rules. A session already running
may not see `.grok/rules/` until restart; `AGENTS.md` is in the
always-applied workspace snapshot for Grok once the file is saved
and a new turn starts.

**Rollback.** Revert the commit. Historical MADRs 0001–0104 are
untouched.

## Acceptance criteria

* [x] `AGENTS.md` forbids mutating work without a named approved
      pair, allows read-only investigation, allows follow-up
      amendments, requires a new pair for greenfield.
* [x] `.grok/rules/madr-plan-before-mutating-work.md` states the
      same gate and links the MADR.
* [x] This pair is committed. Push is owner-initiated.
