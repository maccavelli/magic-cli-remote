---
status: "accepted"
date: 2026-08-24
decision-makers: [Project Owner]
consulted: [Local mise 2026.8.6 help and registry]
informed: [Repository contributors using the managed user environment]
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Manage markdownlint-cli2 with mise in the user environment

## Context and Problem Statement

The repository has a `.markdownlint-cli2.jsonc` configuration, but the
`markdownlint-cli2` executable is not currently resolvable in the active user
environment. The project owner wants the CLI available through mise rather than
through a direct global npm installation or a repository-local Node manifest.
The decision is limited to user-environment tooling and does not change product
code, repository dependencies, or the existing Markdown lint configuration.

## Decision Drivers

* Keep tool ownership consistent with the existing mise-managed Node toolchain.
* Make the executable available through mise shims in fresh shells.
* Record a concrete resolved version rather than leaving environment behavior
  dependent on a moving `latest` selector.
* Avoid adding `package.json`, a lockfile, or `node_modules` to this Go/Flutter
  repository solely for one documentation tool.
* Preserve a simple rollback that removes both the mise configuration entry and
  the unused installed version.

## Considered Options

* Install and pin `markdownlint-cli2` in the user environment with mise.
* Install it directly with `npm install --global`.
* Add it as a repository-local Node development dependency.

## Decision Outcome

Chosen option: "Install and pin `markdownlint-cli2` in the user environment
with mise", because mise already owns the user's Node runtime, its registry maps
`markdownlint-cli2` to `npm:markdownlint-cli2`, and `mise use --global --pin`
provides a reproducible config entry plus a shimmed executable without changing
the repository dependency graph.

* Companion Implementation Plan:
  [0114-PLAN-manage-markdownlint-cli2-with-mise.md](./0114-PLAN-manage-markdownlint-cli2-with-mise.md)

## Consequences

* Good, because mise owns installation, activation, version selection, and
  cleanup through the same user-environment mechanism as Node.
* Good, because the repository gains no Node manifest, lockfile, or vendored
  dependency tree.
* Bad, because the executable is available only in environments that activate
  this user's mise configuration and shims.
* Neutral, because the repository's `.markdownlint-cli2.jsonc` continues to
  exclude MADR and PLAN files; installing the CLI does not change lint scope.

## Pros and Cons of the Options

### Install and pin with mise in the user environment (Chosen)

* Good, because it matches the project owner's requested environment-management
  mechanism.
* Good, because `--pin` records the concrete version resolved from `latest`.
* Bad, because installation requires network access and writes beneath the
  user's mise config and data directories.
* Neutral, because the underlying package still comes from npm through mise's
  npm backend.

### Install directly with npm globally

* Good, because the command is short and familiar.
* Bad, because it bypasses mise and can drift from the mise-selected Node
  runtime.
* Bad, because npm-global ownership is contrary to the project owner's stated
  preference.

### Add a repository-local development dependency

* Good, because the tool version could be committed with a lockfile and invoked
  consistently in CI.
* Bad, because it introduces Node package-management files and installation
  state into a repository that does not currently use them for this tool.
* Bad, because it does not make the executable generally available in the
  user's mise-managed environment.

## Confirmation

The decision is confirmed when all of the following are true:

* `mise ls markdownlint-cli2` reports one active installed version.
* `mise which markdownlint-cli2` resolves an executable under the mise-managed
  data directory.
* `markdownlint-cli2 --version` succeeds from a fresh login shell.
* `~/.config/mise/config.toml` contains a pinned `markdownlint-cli2` tool entry.
* The repository diff contains no package manifest, lockfile, `node_modules`, or
  Markdown lint configuration change from the installation.

## More Information

Local evidence captured on 2026-08-24:

* mise version: `2026.8.6 linux-x64`;
* Node version managed by mise: `22.23.2`;
* registry mapping: `markdownlint-cli2 -> npm:markdownlint-cli2`; and
* global mise configuration: `~/.config/mise/config.toml`.
