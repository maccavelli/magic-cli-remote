---
status: proposed
date: 2026-10-25
associated-madr: "0213-MADR-align-docs-ci-and-disclosure-gates.md"
---
# Implement aligned docs-only CI and disclosure gates

Associated MADR: [0213-MADR-align-docs-ci-and-disclosure-gates.md](0213-MADR-align-docs-ci-and-disclosure-gates.md)

## Goal

A documentation-only push or pull request gets a Docs run with the common Markdown, record/link and portable disclosure gates plus this repository's Markdown-reading checks. Other changes retain full CI. The private pre-push guard remains part of publication.

## Scope

Affected files: `.github/workflows/ci.yml`, `.github/workflows/docs.yml`, `Makefile`, `scripts/check_disclosure.py` (new), a workflow parity test, `AGENTS.md`, `docs/architecture.md`, `docs/README.md`.

Repository-specific work: Keep the existing Docs checks and full-CI filters. Pin `make markdownlint-docs` to markdownlint-cli2 v0.23.2, matching the other repositories. Make the same home-path test an explicit named step in full CI, and add a portable tracked-text disclosure check shared by full and Docs that checks generic home paths without needing the owner's deny list; retain the global pre-push guard. Add a workflow parity test for records, Markdownlint, protocol and disclosure checks, and audit all tracked Markdown read by Go or scripts before retaining `**.md`. Update `AGENTS.md` and `docs/architecture.md`; regenerate `docs/README.md` with the repository tool.

Boundary: Preserve the separate Docs workflow name so `ci-flake-ledger.yml` does not classify a docs run as full CI. A Markdown fixture read by code must remain on full CI if the audit finds one.

## Implementation Steps

### Phase 1: inventory and shared checks

1. Re-read workflow triggers, job commands, Markdownlint globs, tracked Markdown files and every code or script reader of tracked Markdown. Record the docs-safe path set in this PLAN before editing filters. Include hidden agent-rule trees and test fixtures. Review required-status settings; if a filtered job would stay pending, amend this pair with a concrete migration before changing filters.
2. Add missing record/link and Markdownlint targets, reusing the current commands where they exist. Run identical pinned documentation commands in full CI and Docs. Keep Go lint, tests, vulnerability, builds and releases in full CI.
3. Implement a standard-library portable disclosure check over tracked text. It rejects real macOS, Linux and Windows user-home paths, accepts documented placeholders, prints only file, line and category, and fails on unreadable tracked input. Run the same target in full CI and Docs. Retain the private deny list and global pre-push guard locally.
4. Add fixture tests for new gates. Prove each fails on deliberately broken input in a scratch copy and passes on clean input. Record commands, statuses and redacted diagnostics. Commit the approved phase after checks pass.

### Phase 2: workflow routing and parity

1. Keep push and pull-request CI ignore sets identical and limited to audited docs-safe paths. Make Docs paths cover every ignored path and the Docs workflow itself. Mixed changes run full CI. Preserve tags, schedules, dispatch, release jobs and permissions.
2. Add or extend a Python workflow contract check. It validates actual YAML structure, compares event filters, confirms ignored tracked paths are in Docs, refuses known Markdown inputs to code tests unless their reader runs in Docs, and compares common docs-gate commands and pins, and fails when full CI loses a tag, schedule or dispatch trigger it had, or gains a `paths` filter. Integrate it in the workflow/tool test target.
3. Run actionlint and the contract check. In independent scratch copies, plant a mismatched path, a Markdown reader wrongly ignored, a missing Docs gate and a differing pin; require each to fail. Commit the approved phase after checks pass.

### Phase 3: documentation and observed rollout

1. Explain routing, docs-safe paths, common checks, local readers and the private pre-push guard in AGENTS.md and the architecture or CI guide. Verify this pair's exact filenames and current statuses in the records index; use the repository index generator if present.
2. Run `make check-records markdownlint-docs disclosure-check`; `go test -count=1 ./internal/protocol/`; `go test -count=1 -run '^TestTrackedFilesUseOnlyPlaceholderHomes$' ./internal/`; actionlint; scratch plants for MD018, bad record link, real home path and a missing workflow gate. Capture status before filtering output. Check this pair and edited documents for real-machine identifiers. Read each edited file in full and verify links.
3. After explicit authorization to push in that same turn, observe a docs-only push and a non-doc or mixed push. Tag, schedule and dispatch routing is not waited for: GitHub evaluates no path filter for a tag push, `schedule` and `workflow_dispatch` carry none, and the contract check of phase 2, step 2 fails if a trigger is lost or a `paths` filter is added. Record run IDs, conclusions and which workflow started. Mark this PLAN complete only after the criteria below hold; otherwise record the outstanding observation. Commit at the end of each approved phase under repository rules. *Amended 2026-10-25: the wait for the next tag or scheduled run is removed, as acp-go-sdk2's 0037-PLAN-align-docs-ci-and-disclosure-gates.md did in its step 3.*

## Verification

* Both workflows pass pinned Markdownlint, numbered-record/index and relative-link validation, and portable tracked-text disclosure scanning.
* Each Markdown reader runs on changes to its input, either in Docs or full CI.
* A docs-only push starts Docs but not full CI; a non-doc or mixed push starts full CI. Tags, schedules and dispatch retain existing behavior, shown by the contract check and the workflows' structure, not by waiting for a run.
* Scratch plants fail their intended gates; filter or gate drift fails the parity test.
* Before an authorized push, the local pre-push guard checks outgoing commits. Its private deny list is never copied into CI.

## Rollout and Rollback

Land gates before relying on the filters. If routing misclassifies a change, revert the routing commit through a new reviewed commit so full CI runs for every push again. Keep the shared gates while repairing the audit. This PLAN does not authorize a push or tag.

