## GitHub content fetching

When reading content from GitHub, use `curl` + `api.github.com` for structured data or `raw.githubusercontent.com` for file content. Do not use the `webfetch` tool on GitHub URLs — the HTML pages are rendered client-side and will not contain the actual content.

## MADR and plan skill

Whenever the user asks for an MADR and a plan, load the `writing-madr-and-plans`
skill first and follow it for authoring, naming (`NNNN-MADR-*` / `NNNN-PLAN-*`),
and review. This applies both to writing a fresh pair and to amending an
existing one. The operational copy of the full policy is `AGENTS.md`; honour
both, do not fork a third workflow.
