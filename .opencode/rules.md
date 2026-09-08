## GitHub content fetching

When reading content from GitHub, use `curl` + `api.github.com` for structured data or `raw.githubusercontent.com` for file content. Do not use the `webfetch` tool on GitHub URLs — the HTML pages are rendered client-side and will not contain the actual content.

## MADR and plan skill

**Whenever the user asks for an MADR and a plan, load the
`writing-madr-and-plans` skill first** and follow it for authoring, naming
(`NNNN-MADR-*` / `NNNN-PLAN-*`) and review — for a fresh pair and for amending
an existing one.

The name is exact, and a mistyped one fails quietly rather than loudly: the
call returns `Unknown skill` and an agent may carry on without the skill.

**Settled 2026-09-07 by owner decision, with the cause established (MADR 0152).**
The name reversed four times because it was never a typo: both spellings are
true about different machines. A directory named `madr-and-plan-writing` exists
under a *different agent's* skills root on the POSIX host — evidenced by the
grok wire fixture captured 2026-09-03. On this host, under `~/.claude/skills`,
the name is `writing-madr-and-plans`. Finding the other spelling under some
other root is not a bug here.

The filesystem still outranks this paragraph. Verify:

```bash
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md
```

**Mutating work needs an approved `docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair
first**; read-only investigation does not. The normative text — what counts as
mutating, the approval order, the bootstrap exception — is in `AGENTS.md`. Read
it there rather than trusting a summary; this file deliberately does not restate
it (review 2026-09-01, F2).
