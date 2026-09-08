# MADR and PLAN: load the skill, and gate mutating work

**Normative text lives in `AGENTS.md`**, section "MADR and PLAN before mutating
work". This file carries only the two things that must not be missed if that
file is not loaded, and points at it for everything else. Do not restate the
workflow here — a second copy is how the skill name came to be wrong in three
files at once (review 2026-09-01, F1/F2).

## The skill

Whenever the user asks for an MADR and a plan, load the **`madr-and-plan-writing`**
skill first and follow it for authoring, naming (`NNNN-MADR-*` / `NNNN-PLAN-*`)
and review. This applies to writing a fresh pair and to amending an existing
one.

The name is exact, and a mistyped one fails quietly rather than loudly: the
call returns `Unknown skill`, and an agent that proceeds without the skill
writes something shaped like a MADR while missing MADR 4.0.0's heading names,
the `Good/Neutral/Bad` argument form, and the mechanical slug rule.

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
one. See `AGENTS.md` for what counts as mutating, the approval order, the
follow-up-vs-greenfield rule, and the bootstrap exception.
