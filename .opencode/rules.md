## GitHub content fetching

When reading content from GitHub, use `curl` + `api.github.com` for structured data or `raw.githubusercontent.com` for file content. Do not use the `webfetch` tool on GitHub URLs — the HTML pages are rendered client-side and will not contain the actual content.

## MADR and plan skill

Whenever the user asks for an MADR and a plan, load the **`madr-and-plan-writing`**
skill first and follow it for authoring, naming (`NNNN-MADR-*` / `NNNN-PLAN-*`),
and review. This applies both to writing a fresh pair and to amending an
existing one.

The name is exact, and a mistyped one fails quietly rather than loudly: the call
does not resolve and an agent may carry on without the skill.

**Corrected 2026-09-06 — second reversal.** The name is
`madr-and-plan-writing`. This file has now asserted each spelling twice
(`0416ac3` and `fd1e75f` said `writing-madr-and-plans`; `e06a0b6` and this
change say `madr-and-plan-writing`), so do not trust the prose here over the
filesystem — including this sentence. Why the wrong spelling keeps being
written is still not established, and this note does not guess. One fact that
does matter: the skill lives outside this repository —
`~/.claude/skills/madr-and-plan-writing` is a symlink into `~/.agents/skills/` —
so no commit here records a change to it, and the filesystem is the only
witness. See `AGENTS.md` for the evidence.

```bash
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md
```

**Mutating work needs an approved `docs/NNNN-MADR-*` / `docs/NNNN-PLAN-*` pair
first**; read-only investigation does not. The normative text — what counts as
mutating, the approval order, the bootstrap exception — is in `AGENTS.md`. Read
it there rather than trusting a summary; this file deliberately does not restate
it (review 2026-09-01, F2).
