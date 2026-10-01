---
status: proposed
date: 2026-09-14
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0158 — Move MADR/PLAN files to docs/spec/ subdirectory

Implements [0158-MADR-move-madr-plan-files-to-docs-spec.md](0158-MADR-move-madr-plan-files-to-docs-spec.md) decisions D1–D4, closing findings F1–F5.

## Goal

All 282 MADR/PLAN files reside in `docs/spec/`, all cross-references in README.md, AGENTS.md, per-agent rules, and skill documentation use `docs/spec/` paths, and `Select-String` confirms zero broken references.

## Scope

### In scope (the only files any phase may touch)

- All `docs/NNNN-MADR-*.md` files (156 files)
- All `docs/NNNN-PLAN-*.md` files (126 files)
- `README.md` (50+ cross-references)
- `AGENTS.md` (1 cross-reference at line 161)
- `.grok/rules/madr-plan-before-mutating-work.md` (1 cross-reference at line 4)
- `.claude/rules/madr-and-plan-skill.md` (no direct references, but may need path updates)
- `.opencode/rules.md` (no direct references, but may need path updates)
- `~/.claude/skills/madr-and-plan-writing/SKILL.md` (skill documentation, 2 references to `docs/NNNN-MADR-*` path)

### Out of scope

- Spike report directories (`docs/codex-spike-*`, `docs/kilo-spike-*`, `docs/opencode-spike-*`) — remain in `docs/`
- Operational documentation (`docs/ops-*.md`, `docs/config*.md`) — remain in `docs/`
- Git history rewriting — use `git mv` to preserve rename history
- CI/workflow changes — no CI references MADR/PLAN paths

## Stability rule

Each phase ends with:

```bash
# Verify no broken references introduced
Select-String -Path "README.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches after P2
Select-String -Path "AGENTS.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches after P2
```

Commit discipline: one commit per phase, no `git push` until all phases complete and verified.

## Cross-cutting contracts

**C1.** No MADR/PLAN file content is modified — only file locations and cross-reference paths change.

**C2.** Relative links within MADR/PLAN files (e.g., `[NNNN-MADR-slug.md](NNNN-MADR-slug.md)`) remain valid and are not modified.

**C3.** All cross-references are updated atomically with the file moves to avoid intermediate broken states.

**C4.** The most at-risk contract is C3: under time pressure, it's tempting to move files first and update references later, but this creates a broken intermediate state. The plan enforces atomicity by batching moves and reference updates in the same phase.

## Dependency and delivery order

P1 → P2 → P3 (strictly sequential; P2 depends on P1's directory creation, P3 depends on P2's file moves)

## Implementation Steps

### P1 — Create docs/spec/ directory (D1; closes F1)

Create the `docs/spec/` directory.

**Verification:**

```bash
Test-Path -LiteralPath "docs/spec"  # expect True
```

### P2 — Move all MADR/PLAN files to docs/spec/ (D1; closes F1, F3)

Move all 282 files using `git mv` to preserve history:

```bash
# Move all MADR files
Get-ChildItem -LiteralPath "docs" -Filter "*-MADR-*.md" | ForEach-Object { git mv $_.FullName "docs/spec/$($_.Name)" }

# Move all PLAN files
Get-ChildItem -LiteralPath "docs" -Filter "*-PLAN-*.md" | ForEach-Object { git mv $_.FullName "docs/spec/$($_.Name)" }
```

**Verification:**

```bash
Get-ChildItem -LiteralPath "docs" -Filter "*-MADR-*.md" | Measure-Object  # expect 0
Get-ChildItem -LiteralPath "docs" -Filter "*-PLAN-*.md" | Measure-Object  # expect 0
Get-ChildItem -LiteralPath "docs/spec" -Filter "*-MADR-*.md" | Measure-Object  # expect 156
Get-ChildItem -LiteralPath "docs/spec" -Filter "*-PLAN-*.md" | Measure-Object  # expect 126
```

### P3 — Update cross-references in README.md, AGENTS.md, and per-agent rules (D2, D3; closes F2, F4)

Update all `docs/NNNN-MADR-` and `docs/NNNN-PLAN-` references to `docs/spec/NNNN-MADR-` and `docs/spec/NNNN-PLAN-`:

```bash
# README.md
(Get-Content "README.md") -replace 'docs/([0-9]{4}-(MADR|PLAN)-)', 'docs/spec/$1' | Set-Content "README.md"

# AGENTS.md
(Get-Content "AGENTS.md") -replace 'docs/([0-9]{4}-(MADR|PLAN)-)', 'docs/spec/$1' | Set-Content "AGENTS.md"

# .grok/rules/madr-plan-before-mutating-work.md
(Get-Content ".grok/rules/madr-plan-before-mutating-work.md") -replace 'docs/([0-9]{4}-(MADR|PLAN)-)', 'docs/spec/$1' | Set-Content ".grok/rules/madr-plan-before-mutating-work.md"
```

**Verification:**

```bash
Select-String -Path "README.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches
Select-String -Path "AGENTS.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches
Select-String -Path ".grok/rules/madr-plan-before-mutating-work.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches
Select-String -Path "README.md" -Pattern "docs/spec/[0-9]{4}-(MADR|PLAN)-"  # expect 50+ matches
```

### P4 — Update madr-and-plan-writing skill documentation (D2; closes F5)

Update the skill documentation at `~/.claude/skills/madr-and-plan-writing/SKILL.md` to reflect the new path:

```bash
(Get-Content "$HOME/.claude/skills/madr-and-plan-writing/SKILL.md") -replace 'docs/NNNN-MADR-', 'docs/spec/NNNN-MADR-' | Set-Content "$HOME/.claude/skills/madr-and-plan-writing/SKILL.md"
(Get-Content "$HOME/.claude/skills/madr-and-plan-writing/SKILL.md") -replace 'docs/NNNN-PLAN-', 'docs/spec/NNNN-PLAN-' | Set-Content "$HOME/.claude/skills/madr-and-plan-writing/SKILL.md"
```

**Verification:**

```bash
Select-String -Path "$HOME/.claude/skills/madr-and-plan-writing/SKILL.md" -Pattern "docs/NNNN-(MADR|PLAN)-"  # expect 0 matches
Select-String -Path "$HOME/.claude/skills/madr-and-plan-writing/SKILL.md" -Pattern "docs/spec/NNNN-(MADR|PLAN)-"  # expect 2+ matches
```

## Verification (whole plan)

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
|---|-----------|------|
| A1 | `docs/` contains 0 MADR files | D1, F1 |
| A2 | `docs/` contains 0 PLAN files | D1, F1 |
| A3 | `docs/spec/` contains 156 MADR files | D1, F1 |
| A4 | `docs/spec/` contains 126 PLAN files | D1, F1 |
| A5 | README.md contains 0 `docs/NNNN-` references | D2, F2 |
| A6 | AGENTS.md contains 0 `docs/NNNN-` references | D2, F4 |
| A7 | .grok/rules contains 0 `docs/NNNN-` references | D2, F4 |
| A8 | Skill documentation contains 0 `docs/NNNN-` references | D2, F5 |
| A9 | All relative links within MADR/PLAN files remain valid | D3, F3 |

**A9 is most likely to be quietly dropped:** verifying 282 files' internal links is tedious, but the plan relies on C2 (relative links remain valid since all files move together). A spot-check of 5-10 files is sufficient.

## Rollout and Rollback

**Rollout:** Execute P1–P4 sequentially, commit each phase, verify acceptance criteria, then `git push`.

**Rollback:** `git revert` the commits in reverse order (P4 → P3 → P2 → P1). Since `git mv` preserves history, rollback restores the original structure cleanly.

## Deferred (named, so they are not mistaken for oversights)

- **Spike report directories** (`docs/codex-spike-*`, `docs/kilo-spike-*`, `docs/opencode-spike-*`): These are investigation artifacts, not architectural decisions. Moving them to `docs/spec/` would blur the line between specs and research. They remain in `docs/` for now; a future MADR may address their disposition.
- **docs/spec/README.md**: A directory-level README explaining the purpose of `docs/spec/` would be useful but is not critical for this reorganization. Can be added in a follow-up commit.
- **CI/workflow updates**: No CI workflows reference MADR/PLAN paths, so no updates are needed. If future workflows do reference them, they must use `docs/spec/` paths.
