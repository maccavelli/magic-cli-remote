# MADR and PLAN: load the skill, and gate mutating work

**Normative text lives in `AGENTS.md`**, section "MADR and PLAN before mutating
work". This file carries only the two things that must not be missed if that
file is not loaded, and points at it for everything else. Do not restate the
workflow here — a second copy is how the skill name came to be wrong in three
files at once (review 2026-09-01, F1/F2).

## The skill

Whenever the user asks for an MADR and a plan, load the **`madr-and-plan-writing`**
skill first and follow it for authoring, naming (`NNNN-MADR-*` / `NNNN-PLAN-*`)
and review. This applies to writing a fresh pair and to amending an existing one.

The name is exact. It is the `name:` field of
`~/.claude/skills/madr-and-plan-writing/SKILL.md`, and a mistyped skill does not
fail loudly — the call simply does not resolve, and an agent that proceeds
without it writes something that looks like a MADR while missing MADR 4.0.0's
heading names, the `Good/Neutral/Bad` argument form, and the mechanical slug
rule.

## The gate

**Read-only investigation needs no pair.** Reading, searching, `git log` /
`show` / `diff`, and existing tests or diagnostics that do not write the tree.

**Mutating work does.** Before the first write, name the
`docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair being executed, or stop and write
one. See `AGENTS.md` for what counts as mutating, the approval order, the
follow-up-vs-greenfield rule, and the bootstrap exception.
