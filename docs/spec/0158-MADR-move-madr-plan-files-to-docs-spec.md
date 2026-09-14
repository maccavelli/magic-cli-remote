---
status: proposed
date: 2026-09-14
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Move MADR/PLAN files to docs/spec/ subdirectory

## Context and Problem Statement

The `docs/` directory contains 282 MADR/PLAN files (156 MADR + 126 PLAN) alongside operational documentation, spike reports, and configuration guides. This flat structure makes it difficult to distinguish architectural decision records from operational documentation, and the sheer volume of files obscures the design spine.

### What was measured, not assumed

- `Get-ChildItem -LiteralPath "docs" -Filter "*-MADR-*.md" | Measure-Object` returned 156 files
- `Get-ChildItem -LiteralPath "docs" -Filter "*-PLAN-*.md" | Measure-Object` returned 126 files
- `docs/` contains 312 total entries (including subdirectories)
- Cross-references exist in: AGENTS.md (1+), README.md (50+), .grok/rules/madr-plan-before-mutating-work.md (1), .claude/rules/madr-and-plan-skill.md (implicit via AGENTS.md), .opencode/rules.md (implicit via AGENTS.md)
- MADR/PLAN files cross-reference each other extensively (relative links like `[NNNN-MADR-slug.md](NNNN-MADR-slug.md)`)

### Findings

**F1.** The flat `docs/` structure mixes architectural decision records (MADR/PLAN) with operational documentation (ops-*.md), spike reports (codex-spike-*, kilo-spike-*, opencode-spike-*), and configuration guides (config.md, config-mcrelay.md), making the design spine hard to navigate.

**F2.** Moving MADR/PLAN files to `docs/spec/` will break 50+ cross-references in README.md, AGENTS.md, and per-agent rules files unless those references are updated atomically with the move.

**F3.** MADR/PLAN files cross-reference each other using relative links (e.g., `[0042-PLAN-android-app-remediation.md](0042-PLAN-android-app-remediation.md)`). These links will remain valid after the move since all MADR/PLAN files move together to the same subdirectory.

**F4.** The bootstrap exception in AGENTS.md (line ~161) and .grok/rules/madr-plan-before-mutating-work.md (line 4) reference `docs/0105-MADR-mutating-work-requires-madr-and-plan.md` explicitly. These paths must be updated to `docs/spec/0105-MADR-...`.

**F5.** The skill `madr-and-plan-writing` (loaded from `~/.claude/skills/madr-and-plan-writing/SKILL.md`) documents the naming convention as `docs/NNNN-MADR-short-slug.md`. This documentation must be updated to reflect `docs/spec/NNNN-MADR-short-slug.md`.

## Decision Drivers

- **Navigability**: Separate architectural decisions from operational documentation
- **Discoverability**: Make the design spine (MADR/PLAN files) easier to find and browse
- **Maintainability**: Reduce cognitive load when working in `docs/`
- **Compatibility**: Preserve all cross-references and avoid breaking links

## Considered Options

### A — Move all MADR/PLAN files to docs/spec/ (chosen)

Move all `NNNN-MADR-*.md` and `NNNN-PLAN-*.md` files to `docs/spec/`. Update cross-references in README.md, AGENTS.md, per-agent rules, and the madr-and-plan-writing skill documentation.

### B — Keep flat structure, add filename prefixes

Add a prefix like `spec-` to all MADR/PLAN filenames to distinguish them. No directory reorganization.

### C — Create docs/spec/ but keep recent MADRs in docs/

Move only older MADR/PLAN files (e.g., 0001-0100) to `docs/spec/`, keep recent ones in `docs/` for visibility.

## Decision Outcome

### The decisions

**D1.** Move all 282 MADR/PLAN files (156 MADR + 126 PLAN) from `docs/` to `docs/spec/`.

**D2.** Update all cross-references in README.md, AGENTS.md, .grok/rules/, .claude/rules/, .opencode/rules.md, and the madr-and-plan-writing skill to use `docs/spec/` paths.

**D3.** Preserve relative links within MADR/PLAN files (they remain valid since all files move together).

**D4.** Commit the reorganization as a single atomic commit to preserve git history and avoid intermediate broken states.

### Consequences

- `docs/` will be smaller and easier to browse (312 → ~30 entries)
- The design spine will be isolated in `docs/spec/` for focused review
- All cross-references must be updated atomically to avoid broken links
- The madr-and-plan-writing skill documentation must be updated to reflect the new path
- Git history will show the move as a rename (preserved via `git mv`)

### Confirmation

```bash
# Verify all MADR/PLAN files moved
Get-ChildItem -LiteralPath "docs" -Filter "*-MADR-*.md" | Measure-Object  # expect 0
Get-ChildItem -LiteralPath "docs" -Filter "*-PLAN-*.md" | Measure-Object  # expect 0
Get-ChildItem -LiteralPath "docs/spec" -Filter "*-MADR-*.md" | Measure-Object  # expect 156
Get-ChildItem -LiteralPath "docs/spec" -Filter "*-PLAN-*.md" | Measure-Object  # expect 126

# Verify no broken references
Select-String -Path "README.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches
Select-String -Path "AGENTS.md" -Pattern "docs/[0-9]{4}-(MADR|PLAN)-"  # expect 0 matches
Select-String -Path "README.md" -Pattern "docs/spec/[0-9]{4}-(MADR|PLAN)-"  # expect 50+ matches
```

## Pros and Cons of the Options

### A — Move all MADR/PLAN files to docs/spec/ (chosen)

**Pros:**
- Clear separation of architectural decisions from operational docs
- Reduces `docs/` clutter by 90% (282 → 0 files)
- Design spine is isolated and easy to browse
- Aligns with common conventions (e.g., `docs/architecture/`, `docs/adr/`)

**Cons:**
- Requires updating 50+ cross-references atomically
- Adds one directory level to navigate
- Skill documentation must be updated

### B — Keep flat structure, add filename prefixes

**Pros:**
- No directory reorganization
- Filenames self-identify as specs

**Cons:**
- Does not reduce `docs/` clutter
- Prefixes are redundant with the `NNNN-MADR-` pattern already in filenames
- Does not improve navigability

### C — Create docs/spec/ but keep recent MADRs in docs/

**Pros:**
- Recent decisions remain visible
- Older decisions are archived

**Cons:**
- Splits the design spine across two directories
- Creates ambiguity about where to find a specific MADR
- Requires maintaining two locations

## More Information

### Evidence index

| Claim | Source |
|-------|--------|
| 156 MADR files in docs/ | `Get-ChildItem -LiteralPath "docs" -Filter "*-MADR-*.md" \| Measure-Object` |
| 126 PLAN files in docs/ | `Get-ChildItem -LiteralPath "docs" -Filter "*-PLAN-*.md" \| Measure-Object` |
| 50+ cross-references in README.md | `Select-String -Path "README.md" -Pattern "docs/[0-9]{4}-(MADR\|PLAN)-" \| Measure-Object` |
| AGENTS.md references docs/0105-MADR-... | `AGENTS.md:161` |
| .grok/rules references docs/0105-MADR-... | `.grok/rules/madr-plan-before-mutating-work.md:4` |

### Related records

- MADR 0105: Mutating work requires MADR and PLAN (the process this reorganization follows)

### Open questions for the plan

- Should the `docs/spec/` directory include a README.md explaining its purpose?
- Should the spike report directories (codex-spike-*, kilo-spike-*, opencode-spike-*) also move to `docs/spec/`?
