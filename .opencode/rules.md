## GitHub content fetching

When reading content from GitHub, use `curl` + `api.github.com` for structured data or `raw.githubusercontent.com` for file content. Do not use the `webfetch` tool on GitHub URLs — the HTML pages are rendered client-side and will not contain the actual content.

## MADR and plan skill

Whenever the user asks for an MADR and a plan, load the **`madr-and-plan-writing`**
skill first and follow it for authoring, naming (`NNNN-MADR-*` / `NNNN-PLAN-*`),
and review. This applies both to writing a fresh pair and to amending an
existing one.

The name is exact, and a mistyped one fails quietly rather than loudly: the call
does not resolve and an agent may carry on without the skill.

**Mutating work needs an approved `docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair
first**; read-only investigation does not. The normative text — what counts as
mutating, the approval order, the bootstrap exception — is in `AGENTS.md`. Read
it there rather than trusting a summary; this file deliberately does not restate
it (review 2026-09-01, F2).
