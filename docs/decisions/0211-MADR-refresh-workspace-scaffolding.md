---
status: proposed
date: 2026-10-23
decision-makers: Project Owner
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Refresh the workspace scaffold without replacing repository-specific controls

## Context and Problem Statement

The repository has an established workspace scaffold: a root `AGENTS.md`,
documentation trees, record tooling, markdownlint configuration, line-ending
attributes, and harness-specific instruction files. The owner requested that
the `workspace-scaffolding` skill run for this repository.

The skill's inventory found that the Claude, Grok, OpenCode, and Kilo pointers,
`opencode.json`, `.kilo/.gitignore`, and `scripts/check_records.py` differ from
the current skill templates. It also found the required Codex pointer missing.
The differences are intentional repository-specific policy or newer controls:
for example, `opencode.json` contains product command permissions, and the
local record checker has stricter failure behavior than the template.

How should the scaffold be refreshed without weakening established controls?

## Decision Drivers

* Every supported harness needs the mutating-work gate.
* Existing product-specific configuration and stricter documentation checks
  must not be overwritten by a generic scaffold template.
* The refresh must remain idempotent and preserve the repository's current
  documentation layout and line-ending policy.

## Considered Options

* Add the missing Codex pointer and retain all identified template drift.
* Replace every skill-owned file with the current scaffold template.
* Make no changes because the repository is already substantially scaffolded.

## Decision Outcome

Chosen option: "Add the missing Codex pointer and retain all identified template drift", because
the workspace skill requires all five harness pointers but also directs an
idempotent refresh not to overwrite more-specific local rules.

### Amendment — 2026-10-23

The planned full-tree markdownlint command failed with 3,127 pre-existing
issues in 50 files. The failures were in agent instructions, generated Flutter
dependency and build trees, and third-party test fixtures; they did not name
the new Codex pointer or either 0211 record. The owner chose to make the
repository-wide lint scope exclude those non-product inputs.

The configuration will exclude agent instructions, Flutter's generated
dependency/build trees, and `testdata` fixtures, while continuing to lint all
other Markdown. This is broader than the existing `make markdownlint-docs`
target, which would not make the configured full-tree command honest.

### Amendment — 2026-10-23 (asset metadata)

After the first exclusion set, markdownlint checked 56 files and found one
pre-existing issue: a missing final newline in
`apps/mobile/ios/Runner/Assets.xcassets/LaunchImage.imageset/README.md`.
The owner chose to exclude Markdown inside Apple `.xcassets` bundles rather
than modify generated asset metadata. The scope remains reader-facing,
authored documentation.

### Consequences

* Good, because Codex receives the same MADR/PLAN gate as the other harnesses.
* Good, because the product's OpenCode permissions and stricter records tooling remain intact.
* Neutral, because copied-file `cmp` checks will continue to report documented local drift.
* Bad, because future template changes require an explicit review rather than blind copying.
* Good, because a full-tree markdownlint command will no longer fail on
  generated or fixture Markdown that the project does not author.
* Good, because generated Apple asset metadata is also kept outside the
  documentation gate.

## Confirmation

After implementation, verify the new Codex pointer is byte-identical to the
skill template, the existing records and Markdown gates succeed, and a planted
broken link in a scratch copy makes the records gate fail.
