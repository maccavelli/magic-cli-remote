# MADR and PLAN before mutating work

**Normative text lives in `AGENTS.md`**, section "MADR and PLAN before mutating
work". This file carries only the two things that must not be missed if that
file is not loaded. Do not restate the workflow here.

## The skill

**Whenever the user asks for an MADR and a plan, load the
`madr-and-plan-writing` skill first** and follow it for authoring, naming
(`NNNN-MADR-*` / `NNNN-PLAN-*`) and review, for a fresh pair and for amending
an existing one.

The name is exact. A mistyped one returns `Unknown skill`, and an agent may
carry on without the skill. The filesystem outranks this paragraph:

```bash
ls -d ~/.codex/skills/*madr* && grep '^name:' ~/.codex/skills/*madr*/SKILL.md
```

## The gate

**Read-only investigation needs no pair.** Reading, searching, `git log` /
`show` / `diff`, and existing tests or diagnostics that do not write the tree.

**Mutating work does.** Before the first write, name the
`docs/decisions/NNNN-MADR-*` / `docs/decisions/NNNN-PLAN-*` pair being
executed, or stop and write one. See `AGENTS.md` for what counts as mutating,
the approval order, the bootstrap exception, and the pre-add checks.
