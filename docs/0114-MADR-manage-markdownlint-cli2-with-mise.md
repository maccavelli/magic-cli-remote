---
status: "accepted"
date: 2026-08-28
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

## Amendment — 2026-08-28: the decision is scoped to the mise-managed Linux environment

The original record does not say which environment it governs, and read
literally its rejection of `npm install --global` applies everywhere. That
reading does not survive contact with the owner's Windows machine, where the
decision was requested again on 2026-08-28.

**Why the driver does not transfer.** The chosen option rests on "mise already
owns the user's Node runtime". On `windows/amd64` it does not:

* `mise` is not installed and not on `PATH`;
* `~/.config/mise/` does not exist;
* Node is a system installation at `C:\Program Files\nodejs` (v24.14.0, npm
  11.9.0), not a mise-managed runtime;
* the evidence in More Information is `linux-x64`, so the original execution
  was on a different host.

With no mise-managed Node to be consistent *with*, the first decision driver is
vacuous on Windows and the remaining drivers do not by themselves select mise.

**Why mise was not simply installed there.** Windows is mise's weakest target:
tools are reached through shims only, `mise activate` has no PowerShell
support, and `mise.toml` environment variables require `mise x` or `mise run`.
The documented remedy for the VSCode `spawn EINVAL` failure is setting
`windows_shim_mode` to `symlink`, which cannot work on that machine — 0118
established it holds no `SeCreateSymbolicLinkPrivilege`. Adding a version
manager on its weakest platform to obtain one documentation linter fails the
"simple rollback" driver rather than serving it.

**What was done instead.** `npm install --global markdownlint-cli2`, resolving
`markdownlint-cli2@0.23.2` (markdownlint v0.41.1) into
`C:\Users\macsm\AppData\Roaming\npm`, which is inside the user's home directory
and already on `PATH`. Verified: `markdownlint-cli2 --version` succeeds, and a
repository run reports `Linting: 48 files` with the MADR and PLAN globs
excluded as before. The repository diff contains no manifest, lockfile,
`node_modules`, or lint-configuration change, so that consequence of the
original decision is preserved.

**The one driver this does not satisfy.** "Record a concrete resolved version
rather than leaving environment behavior dependent on a moving `latest`
selector." A global npm install pins nothing; `npm update -g` may move it. The
installed version is recorded here (`0.23.2`), which is weaker than a config
entry a tool enforces. Pinning it deliberately is an open item, not a claim
already met.

**Scope of this amendment.** The chosen option stands unchanged for
mise-managed environments. This adds that on a host with no mise-managed Node
runtime, a home-directory global npm install is the sanctioned equivalent, and
the original rejection of option B is read as scoped to hosts where mise owns
Node. Nothing above alters the 2026-08-24 rationale, which was correct for the
environment it was written in.
