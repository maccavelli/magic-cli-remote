# MADR and PLAN before mutating work

Rationale: [docs/0105-MADR-mutating-work-requires-madr-and-plan.md](../../docs/0105-MADR-mutating-work-requires-madr-and-plan.md).
The operational copy in this tree is `AGENTS.md`. Honour both; do
not fork a third workflow.

## Gate

**Read-only investigation is allowed with no pair.** Reading,
searching, `git log`/`show`/`diff`, and existing tests or
diagnostics that do not write the tree do not need a MADR.

**Mutating work is not.** Before the first write, name the
`docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair being executed, or
stop and write one.

Mutating means: creating, editing, or deleting files; staging or
committing (except the bootstrap exception); dependency or lockfile
changes; CI/config/hook changes; builds or installers that write
the tree, `$HOME`, or a live service; generating committed
artifacts.

## Order

1. Investigate (read-only).
2. Write or amend the MADR (`status: proposed` unless the owner
   already decided). Present it. Do not implement.
3. Write or amend the PLAN. Present it.
4. Mutate **only after** the owner explicitly approves execution
   (`proceed`, `execute the plan`, `do phase N`). Stay inside that
   PLAN.
5. Out-of-scope discoveries wait: amend the pair, re-approve, then
   continue. Completing a phase is not permission to invent the
   next unwritten one.

## Follow-up vs greenfield

* **Same topic** (debug, leftover phase, bug found in that plan's
  live run): amend that number. Add a PLAN phase or an Observed /
  amendment in the MADR. Do not silently rewrite historical
  rationale.
* **Greenfield**: next unused `NNNN`, new MADR, new PLAN, same
  slug. No mutation until that PLAN is approved.

## Bootstrap exception

Authoring `docs/NNNN-MADR-*`, `docs/NNNN-PLAN-*`, the MADR section
of `AGENTS.md`, and files under `.grok/rules/` does not require a
*prior* pair. Putting source, tests, CI, or product config in that
same commit is a violation.

`git push` and tags still need an explicit ask in the same turn.
