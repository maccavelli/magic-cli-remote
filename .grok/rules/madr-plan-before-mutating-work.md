# MADR and PLAN before mutating work

**Normative text lives in `AGENTS.md`**, section "MADR and PLAN before mutating
work". Rationale: [docs/0105-MADR-mutating-work-requires-madr-and-plan.md](../../docs/0105-MADR-mutating-work-requires-madr-and-plan.md).

This file used to restate that whole section. It no longer does — that fork is
what let the skill name drift out of date in every per-agent copy at once while
each one told the reader not to fork the workflow (review 2026-09-01, F1/F2).
What remains is the two things that must not be missed if `AGENTS.md` is not
loaded.

## The skill

**Whenever the user asks for an MADR and a plan, load the
`madr-and-plan-writing` skill first** and follow it for authoring, naming
(`NNNN-MADR-*` / `NNNN-PLAN-*`) and review — for a fresh pair and for amending
an existing one.

The name is exact, and a mistyped one fails quietly rather than loudly: the
call returns `Unknown skill` and an agent may carry on without the skill.

**Settled 2026-09-08 by owner decision, and by a rename rather than a document
(MADR 0152, second amendment).** The name reversed five times because it was
never a typo: the two spellings were true of two different machines. The Mac's
grok skills root has held `madr-and-plan-writing` since at least 2026-09-03,
while this host's `~/.claude/skills` held `writing-madr-and-plans`. The
directory here has now been renamed to match, so one name covers both machines
and there is no second spelling left to discover and "correct" to.

The filesystem still outranks this paragraph. Verify:

```bash
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md
```

## The gate

**Read-only investigation needs no pair.** Reading, searching, `git log` /
`show` / `diff`, and existing tests or diagnostics that do not write the tree.

**Mutating work does.** Before the first write, name the
`docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair being executed, or stop and write
one.

Everything else — what counts as mutating, the four-step approval order, the
follow-up-vs-greenfield rule, the bootstrap exception, and the rule that
`git push` needs an explicit ask in the same turn — is in `AGENTS.md`. Read it
there rather than trusting a summary here.
