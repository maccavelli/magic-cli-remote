---
status: complete
date: 2026-11-08
associated-madr: "0227-MADR-agent-readonly-command-and-tool-allowlist.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement allow-list of read-only agent commands and tools

Associated MADR: [0227-MADR-agent-readonly-command-and-tool-allowlist.md](0227-MADR-agent-readonly-command-and-tool-allowlist.md)

## Goal

Extend `.claude/settings.json` and `opencode.json`, and add
`.gobble/settings.yaml`, so sane read-only tools and shell commands
auto-allow, without widening mutate paths.

## Scope

In:

* `.claude/settings.json` — `permissions.allow` (keep existing entries;
  add tool names and read-only `Bash(…)` patterns).
* `opencode.json` — `permission.bash` (keep existing; add the same
  command patterns). OpenCode has no separate tool-name list in this
  file; do not invent one.
* `.gobble/settings.yaml` — gobble `permissions.rules` (new).

Out:

* Codex / Grok / Kilo in-repo permission files (none exist).
* Changing `permission.bash` `"*": "ask"` default.
* Revoking existing `go test` / `make test` / `go build` allows.
* AGENTS.md unless a one-line pointer is needed after landing.

## Implementation Steps

### Phase 1 — Claude Code allow-list

1. Keep every current `permissions.allow` entry.
2. Prepend or append these **tool** entries (Claude Code syntax):
   `Read`, `Grep`, `Glob`, `LS`, `Task`, `TodoRead`.
3. Add `Bash(…)` patterns for the read-only git and inspection
   commands named in the MADR "More Information" list. Use the same
   `Bash(cmd *)` / exact-match style as the existing file. Do not add
   `Bash(git *)` or `Bash(*)`.
4. Add `Bash(make -n *)`, `Bash(make help)`,
   `Bash(python3 scripts/check_records.py --next)`,
   `Bash(python3 scripts/check_records.py --help)`.
5. Validate JSON (`python3 -m json.tool .claude/settings.json`).

### Phase 2 — OpenCode allow-list

1. Keep `"*": "ask"` and every current allow.
2. Add the same command patterns as Phase 1 (without the Claude tool
   names). Include Windows `.\` spellings only where the existing file
   already does that for `bin\`; do not invent extra Windows git
   spellings.
3. Validate JSON (`python3 -m json.tool opencode.json`).

### Phase 3 — gobble `.gobble/settings.yaml`

1. Create `.gobble/settings.yaml` with `permissions.allowBypass: false`
   and `permissions.rules` covering gobble tools `read`, `grep`,
   `find`, `tree`, `todo`, plus `bash(<pattern>)` for every command
   prefix already allowed in the Claude/OpenCode files (including the
   existing `go test` / `make test` / `go build` set). Do not add
   `bash(*)` or `bash(git *)`.
2. Do not put host paths, `external_directory`, or `allowBypass: true`
   in the file.

### Phase 4 — records

1. Set both records to `status: accepted` (MADR) and `status: complete`
   (PLAN) only after the owner confirms a read-only turn no longer
   prompts. Until then leave `proposed` / mark `in-progress` when
   executing.
2. `python3 scripts/check_records.py` (or `make check-records`).

## Verification

* JSON parses.
* `make check-records` pairs 0227.
* Manual: Claude Code, OpenCode, and gobble run `git status` and a
  read of `AGENTS.md` with no prompt; `git add` still prompts.

## Rollout and Rollback

Rollout is two JSON files and `.gobble/settings.yaml`. Rollback is
revert those files. No runtime service.

## Execution record

* 2026-11-01 — Phases 1–2 landed (Claude and OpenCode JSON).
* 2026-11-01 — Owner asked for gobble's own file. MADR amended; Phase 3
  added. Phase 3 lands in the same working tree.

## Status note — 2026-11-08

~~`in-progress`~~ `complete`. Phases 1–3 landed in `254279ac`. Phase 4 waited for the owner's confirmation that a read-only turn no longer prompts, which the check below gave.

The owner ran the Confirmation's check on 2026-11-08 at `39b83ba9`, headless, one run per tool in its default
permission mode. The read-only prompt asked for `git status`, `git log -1`, `python3 scripts/check_records.py --next`
and a read of `AGENTS.md`; the control asked for `git add --dry-run AGENTS.md`, which stages nothing even if allowed.

| Tool | Read-only | Control |
| --- | --- | --- |
| Claude Code (`claude -p --permission-mode default`) | all four ran; `permission_denials: []` | denied: `permission_denials` lists the `git add` call |
| OpenCode (`opencode run`, no `--auto`) | all four tool calls `completed` | "permission requested … auto-rejecting"; the call ended `error` |
| gobble (`gobble chat -p --no-session`) | all four ran | "The command was declined, so nothing ran" |

Headless rather than in the interactive sessions the PLAN's Verification names: both modes read the same allow
lists, and a headless run turns an ask into a denial, which is what the control shows.

**Finding:** every gobble run warns `.gobble/settings.yaml:4: permissions.allowBypass: only the global settings
file can set this`. gobble ignores the project file's `allowBypass: false`. It does no harm, since `false` is the
safe value and gobble's default, but the line has no effect. Removing it is a change to that file and needs its own
amendment of this record; the audit does not make it.

Recorded by the November 2026 audit, [0239-REPORT-record-status-and-docs-accuracy-audit.md](../reports/0239-REPORT-record-status-and-docs-accuracy-audit.md).
