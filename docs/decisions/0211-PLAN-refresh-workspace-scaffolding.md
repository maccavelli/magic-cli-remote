---
status: complete
date: 2026-10-23
associated-madr: "0211-MADR-refresh-workspace-scaffolding.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Refresh workspace scaffolding

Associated MADR: [0211-MADR-refresh-workspace-scaffolding.md](0211-MADR-refresh-workspace-scaffolding.md)

## Goal

Complete an idempotent workspace-scaffold refresh while preserving the
repository's existing product-specific controls.

## Scope

In scope: the missing Codex MADR/PLAN pointer, documentation-index entry for
this pair, markdownlint exclusions for non-product Markdown, and validation of
the scaffold gates and copied-template inventory.

Out of scope: replacing known local drift in other harness pointers,
`opencode.json`, `.kilo/.gitignore`, `scripts/check_records.py`, product
documentation, source code, CI, dependencies, and application configuration.

## Implementation Steps

1. Copy `templates/pointers/codex.md` to
   `.codex/rules/madr-plan-before-mutating-work.md` and confirm byte identity
   with `cmp`.
2. Regenerate the record index with the repository's existing
   `scripts/check_records.py --write-index`; inspect the resulting
   `docs/README.md` in full and confirm the 0211 MADR and PLAN rows appear.
3. Record the intentionally retained template drift for Claude, Grok,
   OpenCode, Kilo, `opencode.json`, `.kilo/.gitignore`, and the record checker
   in the execution record. Do not modify those files.
4. Exclude agent instructions, generated Flutter dependency/build trees, and
   third-party `testdata` fixtures from the full-tree markdownlint scope. Keep
   all authored product documentation in scope.
5. Exclude Markdown inside Apple `.xcassets` bundles from the full-tree
   markdownlint scope.

## Verification

* `cmp` confirms the new Codex pointer matches its template.
* `python3 scripts/check_records.py --next` prints the next number after 0211.
* `python3 scripts/check_records.py --check-all` exits zero.
* `npx --yes markdownlint-cli2@0.23.2` exits zero after the exclusions are
  added.
* In a scratch copy, a broken relative Markdown link causes
  `python3 scripts/check_records.py --check-all` to exit 1 with
  `broken relative link:`.
* `git ls-files --eol` reports LF attributes for tracked text files.
* Scan added files for the local account name, mail and Git-host domains, and
  user-profile path prefixes; only placeholders may be recorded.

## Rollout and Rollback

The change applies only to repository guidance and documentation metadata.
Rollback removes the new Codex pointer and the 0211 index entries together;
it does not alter product behavior.

## Execution Record

Execution started 2026-10-23 after owner approval.

### Deviation — 2026-10-23

The full-tree markdownlint command failed with 3,127 pre-existing issues in
50 files. The failures were in `AGENTS.md`, `.opencode/rules.md`, generated
Flutter dependency/build trees, and third-party test fixtures; neither the new
Codex pointer nor the 0211 records appeared in the failure output. The owner
selected repository-level exclusions instead of narrowing verification to
`make markdownlint-docs`. Step 4 and its revised verification criterion record
the approved resolution; implementation awaits re-approval.

### Deviation — 2026-10-23 (asset metadata)

After step 4, markdownlint found one pre-existing MD047 violation in an Apple
asset-catalog README. The owner chose the recommended exclusion for Markdown
inside `.xcassets` bundles and explicitly approved proceeding. Step 5 records
that resolution.

### Completed — 2026-10-23

* Added `.codex/rules/madr-plan-before-mutating-work.md`; `cmp` against the
  workspace-scaffolding template exited 0.
* Regenerated `docs/README.md`; it now indexes both 0211 records.
* Retained the intentional local drift in the Claude, Grok, OpenCode, and Kilo
  pointers, `opencode.json`, `.kilo/.gitignore`, and `scripts/check_records.py`.
* Added markdownlint exclusions for agent instructions, generated Flutter
  dependency/build trees, third-party `testdata`, and Apple `.xcassets`
  metadata. The full-tree `markdownlint-cli2 v0.23.2` run linted 55 files with
  0 issues.
* `python3 scripts/check_records.py --next` printed `0212`; `--check-all`
  exited 0. The line-ending scan reported no tracked entries outside the LF or
  binary attributes.
* In a scratch clone, appending `[broken](missing.md)` to `README.md` made
  `python3 scripts/check_records.py --check-all` exit 1 with
  `broken relative link: README.md:1650: missing.md`.
* Identifier scanning of the added content found no local account name, email,
  hostname, or user-profile-path prefix. A Git-host-domain match was confined
  to pre-existing configuration commentary; no changed hunk contains it.
