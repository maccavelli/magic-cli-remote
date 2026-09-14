## GitHub content fetching

When reading content from GitHub, use `curl` + `api.github.com` for structured data or `raw.githubusercontent.com` for file content. Do not use the `webfetch` tool on GitHub URLs — the HTML pages are rendered client-side and will not contain the actual content.

## MADR and plan skill

**Normative text lives in `AGENTS.md`**, section "MADR and PLAN before mutating
work". This file carries only the skill name and the gate, and points there for
everything else — a second copy of the workflow is how the skill name went
stale in all per-agent files at once (review 2026-09-01, F2).

**Whenever the user asks for an MADR and a plan, load the
`madr-and-plan-writing` skill first** and follow it for authoring, naming
(`NNNN-MADR-*` / `NNNN-PLAN-*`) and review — for a fresh pair and for amending
an existing one.

The name is exact, and a mistyped one fails quietly rather than loudly: the
call returns `Unknown skill`, and an agent that proceeds without the skill
writes something shaped like a MADR while missing MADR 4.0.0's heading names,
the `Good, because …` argument form, and the mechanical slug rule.

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

**Mutating work needs an approved `docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair
first**; read-only investigation does not. The normative text — what counts as
mutating, the approval order, the bootstrap exception — is in `AGENTS.md`. Read
it there rather than trusting a summary; this file deliberately does not restate
it (review 2026-09-01, F2).
