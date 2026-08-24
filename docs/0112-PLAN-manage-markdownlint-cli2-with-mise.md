---
status: "draft"
date: 2026-08-24
associated-madr: "0112-MADR-manage-markdownlint-cli2-with-mise.md"
owner: [Project Owner, Codex]
target-milestone: "Immediate user-environment setup"
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Install and pin markdownlint-cli2 with mise

## Executive Summary & Goal

* **Associated Decision Record:**
  [0112-MADR-manage-markdownlint-cli2-with-mise.md](./0112-MADR-manage-markdownlint-cli2-with-mise.md)
* **Goal:** Install `markdownlint-cli2` through mise's npm backend, pin the
  resolved version in the user mise configuration, and expose the executable
  through mise shims without changing repository dependency files.
* **Success Criteria:**
  * [ ] mise reports one active, pinned `markdownlint-cli2` version.
  * [ ] A fresh login shell resolves `markdownlint-cli2` through mise.
  * [ ] `markdownlint-cli2 --version` exits successfully.
  * [ ] No repository package manifest, lockfile, dependency tree, or lint
    configuration changes as an installation side effect.

## Prerequisites & Dependencies

* **Execution approval:** The project owner must explicitly approve this plan
  after reviewing it, as required by the repository mutation policy.
* **Filesystem access:** The execution environment must be allowed to write
  `~/.config/mise/config.toml` and mise's user data directory beneath
  `~/.local/share/mise`.
* **Network access:** mise's npm backend must be able to resolve and download
  `markdownlint-cli2` and its dependencies from the npm registry.
* **Runtime:** mise-managed Node `22.23.2` is already active and satisfies the
  package installation backend requirement.
* **Repository state:** Preserve the existing uncommitted PLAN 0109 formatting
  edit and the 0112 documentation pair; create no unrelated repository files.

## Architecture & Technical Design Summary

The installation uses mise's registered short name, which resolves to the npm
backend. In mise terminology, `--global` selects the user's global mise config;
it does not perform an npm-global installation.

```text
mise user config (~/.config/mise/config.toml)
                    |
                    v
        npm:markdownlint-cli2@<pinned version>
                    |
                    v
              mise executable shim
                    |
                    v
            markdownlint-cli2 --version
```

No `package.json`, lockfile, or `node_modules` directory is introduced in the
repository. The existing `.markdownlint-cli2.jsonc` file remains unchanged,
including its deliberate exclusion of MADR and PLAN files.

## Phased Implementation Plan

### Phase 1: Pre-flight and version resolution

1. Record `git status --short`, `mise --version`, `mise config ls`, and any
   existing `mise ls markdownlint-cli2` result.
2. Run a mise dry-run against the user configuration:

   ```bash
   mise use --global --pin --dry-run markdownlint-cli2@latest
   ```

3. Confirm the dry-run targets `~/.config/mise/config.toml`, uses the
   `npm:markdownlint-cli2` backend, and does not propose a repository-local
   `mise.toml`.

### Phase 2: Install and pin in the user environment

1. Execute the non-interactive mise installation:

   ```bash
   mise use --global --pin --yes markdownlint-cli2@latest
   ```

2. Capture the concrete version written by mise and the executable path. Do not
   edit the generated configuration by hand.

### Phase 3: Verify activation and repository isolation

1. Run:

   ```bash
   mise ls markdownlint-cli2
   mise which markdownlint-cli2
   markdownlint-cli2 --version
   bash -lc 'command -v markdownlint-cli2 && markdownlint-cli2 --version'
   ```

2. Inspect the relevant mise config entry and confirm it contains the resolved
   concrete version rather than `latest`.
3. Run `git status --short` and `git diff --check`; confirm the installation
   introduced no repository dependency or configuration changes.
4. Update this plan's task checklist and execution log with the installed
   version and verification results. Do not stage, commit, push, or tag unless
   the project owner separately requests it.

## Verification & Testing Strategy

| Check | Command | Passing requirement |
| --- | --- | --- |
| mise registration | `mise ls markdownlint-cli2` | One active installed version is reported. |
| Executable resolution | `mise which markdownlint-cli2` | Path resolves beneath mise-managed user data. |
| CLI execution | `markdownlint-cli2 --version` | Exits zero and prints the installed version. |
| Fresh-shell activation | `bash -lc 'command -v markdownlint-cli2'` | Resolves without manual PATH modification. |
| Version pin | Inspect the mise tool entry | Concrete version is stored, not `latest`. |
| Repository isolation | `git status --short` and `git diff --check` | Only the already-known documentation edits are present and whitespace checks pass. |

No Go, Dart, Flutter, race, or live-provider tests are required because this
plan changes only user-environment tooling and its decision documentation.

## Rollback & Mitigation Procedures

If installation fails before mise updates its config, retain the failure output
and make no manual partial entry. If config was updated but verification fails,
remove the exact resolved version from the user config and allow mise to prune
it when unused:

```bash
mise unuse --global --yes markdownlint-cli2@<resolved-version>
```

Then verify `mise ls markdownlint-cli2` reports no active version and a fresh
shell no longer resolves the command. Do not remove Node or any shared npm
backend files. If another mise configuration references the same installed
version, retain the installation and remove only this user-config reference.

## Task Checklist

- [x] Record the pre-install repository and mise state.
- [x] Dry-run the pinned user-environment installation.
- [x] Confirm the target config and npm backend.
- [x] Install `markdownlint-cli2` with `mise use --global --pin`.
- [x] Record the resolved version and executable path.
- [x] Verify the CLI in the active and fresh login shells.
- [x] Confirm the mise config contains a concrete version pin.
- [x] Confirm no repository dependency or lint configuration changed.
- [x] Record execution evidence below.

## Execution Log

Executed on 2026-08-24 after explicit owner approval.

**Phase 1 — Pre-flight and version resolution**

* `git status --short`: ` M docs/0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md` plus untracked `docs/0112-MADR-manage-markdownlint-cli2-with-mise.md` and `docs/0112-PLAN-manage-markdownlint-cli2-with-mise.md` — only the already-known documentation edits.
* `mise --version`: `2026.8.6 linux-x64 (2026-08-14)`.
* `mise config ls`: `~/.config/mise/config.toml` manages `rust, dart, flutter, java, node, just, protoc, cmake, ninja, glab`; `markdownlint-cli2` not present.
* `mise ls markdownlint-cli2`: no active version (empty).
* Dry-run `mise use --global --pin --dry-run markdownlint-cli2@latest`:
  `mise markdownlint-cli2@0.23.2 ⇢ would install` and
  `mise would update ~/.config/mise/config.toml (add: markdownlint-cli2@0.23.2)`.
  Confirmed user config target, npm backend via the registered short name, and no repository-local `mise.toml` proposal.

**Phase 2 — Install and pin**

* `mise use --global --pin --yes markdownlint-cli2@latest`: installed `markdownlint-cli2@0.23.2` (86 npm packages), wrote `mise ~/.config/mise/config.toml tools: markdownlint-cli2@0.23.2`.
* Config entry (`~/.config/mise/config.toml` line 12): `markdownlint-cli2 = "0.23.2"` — concrete version, not `latest`. Not hand-edited.
* Executable path: `/home/mac/.local/share/mise/installs/markdownlint-cli2/0.23.2/node_modules/.bin/markdownlint-cli2`.

**Phase 3 — Verification**

| Check | Result |
| --- | --- |
| `mise ls markdownlint-cli2` | `markdownlint-cli2  0.23.2  ~/.config/mise/config.toml  0.23.2` — one active installed version. |
| `mise which markdownlint-cli2` | `/home/mac/.local/share/mise/installs/markdownlint-cli2/0.23.2/node_modules/.bin/markdownlint-cli2` — beneath mise-managed user data. |
| `markdownlint-cli2 --version` | Prints `markdownlint-cli2 v0.23.2 (markdownlint v0.41.1)`; exit code `0`. Observed quirk: v0.23.2 prints the banner and then continues as a normal run, treating `--version` as an extra glob — in the repo root this lints 79 files (574 pre-existing issues across 39 files) and still exits `0`; in an empty directory it reports `0 issues in 0 files` and exits `0`. |
| `bash -lc 'command -v markdownlint-cli2'` | `/home/mac/.local/share/mise/shims/markdownlint-cli2` — resolves through a mise shim with no manual PATH modification. |
| Config pin | `~/.config/mise/config.toml` contains `markdownlint-cli2 = "0.23.2"`. |
| Repository isolation | `git status --short` shows only the pre-existing `docs/0109` modification and the untracked 0112 pair; `git diff --check` exits `0`; no `mise.toml`, `package.json`, lockfile, or `node_modules` introduced. `.markdownlint-cli2.jsonc` unchanged; its MADR/PLAN exclusions appeared verbatim in the lint `Finding:` line. |

All success criteria met. No Go/Dart/Flutter tests required per the plan. Nothing staged, committed, pushed, or tagged.
