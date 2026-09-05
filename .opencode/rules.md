## GitHub content fetching

When reading content from GitHub, use `curl` + `api.github.com` for structured data or `raw.githubusercontent.com` for file content. Do not use the `webfetch` tool on GitHub URLs — the HTML pages are rendered client-side and will not contain the actual content.

## MADR and plan skill

Whenever the user asks for an MADR and a plan, load the **`writing-madr-and-plans`**
skill first and follow it for authoring, naming (`NNNN-MADR-*` / `NNNN-PLAN-*`),
and review. This applies both to writing a fresh pair and to amending an
existing one.

The name is exact, and a mistyped one fails quietly rather than loudly: the call
does not resolve and an agent may carry on without the skill.

**Corrected 2026-09-05:** this file named `madr-and-plan-writing` from
`e06a0b6` until now; that skill does not exist. Do not flip it back from
memory — verify with the command below, and see `AGENTS.md` for the full note.

```bash
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md
```

**Mutating work needs an approved `docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair
first**; read-only investigation does not. The normative text — what counts as
mutating, the approval order, the bootstrap exception — is in `AGENTS.md`. Read
it there rather than trusting a summary; this file deliberately does not restate
it (review 2026-09-01, F2).
