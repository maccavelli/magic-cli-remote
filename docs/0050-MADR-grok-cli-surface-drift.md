# MADR 0050: Grok CLI surface drift — seven dead config options

<!-- markdownlint-disable MD013 MD060 -->

- **Status**: **Implemented** (2026-07-29), commits `901923c` (argv), `4ee9c72`
  (live pin), `a868e17` (permission_mode default), `5db2e75` (sandbox),
  `a3d52ff` (tool-policy measurement). Go suite, full `live_grok` suite and
  `make preflight` all green. The §2 discrimination pair now runs as a
  permanent test and passes.
- **Date**: 2026-07-29
- **Scope**: `internal/provider/grok`, `internal/provider/acpagent` (config
  surface), `internal/config`, `docs/config.md`. No protocol change; no mobile
  change.
- **Measured against**: grok **0.2.114** (`0c78503879`, stable), installed at
  `/home/mac/.grok/bin/grok`, on this development host
- **Related**:
  - [MADR 0038](./0038-MADR-grok-acp-parity-assessment.md) /
    [MADR 0039](./0039-MADR-grok-acp-parity.md) — the parity work that added
    these options, measured against grok **0.2.112**
  - [MADR 0049](./0049-MADR-grok-auto-mode.md) — grok's synthetic `auto` mode;
    this MADR explains why it appeared to do nothing and proves it works
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) — auto as a mode
- **Companion plan**: [0050-plan-grok-cli-surface-drift.md](./0050-plan-grok-cli-surface-drift.md)

---

## 1. Problem

**Seven `providers.grok.*` config options do not merely fail to work — they
prevent grok from starting at all.**

`defaultArgs` (`internal/provider/grok/grok.go:97-131`) builds
`grok agent --no-leader <flags> stdio`. But those flags are **global** flags on
the `grok` command, not flags of the `agent` subcommand. grok 0.2.114 rejects
every one of them in that position:

```text
$ grok agent --no-leader --permission-mode default stdio
error: unexpected argument '--permission-mode' found
```

Measured, all six variants, current ordering vs. global-first ordering:

| flag | after `agent` (today) | before `agent` |
|---|---|---|
| `--permission-mode` | **REJECT** | ACCEPT |
| `--tools` | **REJECT** | ACCEPT |
| `--disallowed-tools` | **REJECT** | ACCEPT |
| `--allow` | **REJECT** | ACCEPT |
| `--deny` | **REJECT** | ACCEPT |
| `--no-subagents` | **REJECT** | ACCEPT |
| `--disable-web-search` | **REJECT** | ACCEPT |

Only `--no-leader`, `--always-approve`, `-m/--model` and `--reasoning-effort`
are valid where we put them (confirmed against `grok agent --help`, whose whole
option set is `--reauth -m --reasoning-effort --always-approve --agent-profile
--plugin-dir --leader --no-leader --leader-socket --debug --debug-file
--grok-ws-* --xai-api-base-url --cli-chat-proxy-base-url`).

So setting `providers.grok.permission_mode`, `allowed_tools`,
`disallowed_tools`, `allow_rules`, `deny_rules`, `no_subagents` or
`disable_web_search` makes every grok session fail to launch. The failure is
silent from the phone's point of view: the provider simply never becomes ready.

MADR 0039 shipped these against grok **0.2.112**, where `grok agent` accepted
them. The CLI has since moved them to the top level. Nothing in the repo pins
the ordering, so the drift went unnoticed.

## 2. The consequence that mattered: auto looked like a no-op

MADR 0049 added a synthetic `auto` mode for grok, enforced by intercepting ACP
`session/request_permission`. Probing it live suggested it did nothing: grok
never prompted, so there was nothing to intercept — shell execution,
out-of-workspace writes and deletes all completed without an approval.

That conclusion was **wrong, and the cause is §1**. The permission-mode matrix,
measured with the ordering corrected (one prompt-triggering task per row):

| `--permission-mode` | permission requests | file written | turn completed |
|---|---|---|---|
| *(flag absent — today's only reachable state)* | 0 | yes | yes |
| `default` | **1** | no | no (blocked on approval) |
| `acceptEdits` | **1** | no | no |
| `dontAsk` | **1** | no | no |
| `auto` | 0 | yes | yes |
| `bypassPermissions` | 0 | yes | yes |

Grok **does** route approvals through ACP `session/request_permission` — under
`default`, `acceptEdits` and `dontAsk`. It just could never be put into one of
those modes, so it always ran in whatever its own config resolves to, which on
this host is permissive.

With the ordering fixed and `permission_mode: default`, the armed/unarmed pair
discriminates cleanly:

| auto armed | permission requests | file written | turn completed |
|---|---|---|---|
| no | **1** | no | no |
| yes | **0** | **yes** | **yes** |

**MADR 0049's mechanism is correct and effective.** It suppresses a real prompt
*and* lets the agent finish the work. It was unreachable, not broken. The
earlier "auto is a no-op for grok" reading was an artifact of the arg bug.

## 3. Second-order finding: the effective permission mode is not ours to know

With no `--permission-mode` flag, grok resolves the mode from its own
configuration — `~/.grok/config.toml`, project `.grok/`, and (since grok 0.2.102)
**fleet-wide remote config when no local setting exists**. On this host that
resolves to something permissive: nothing prompts.

That is a product problem independent of the bug. The daemon advertises modes
and a `dangerous` flag on the assumption that it knows the session's approval
posture, but with the flag absent it does not, and the phone's mode chip cannot
reflect a policy the daemon never set. Pinning the flag explicitly is what makes
the advertised modes mean anything.

## 4. Third-order finding: unsupported surface

`grok --help` on 0.2.114 exposes capabilities the provider does not model.
Assessed, not all recommended:

| surface | status | assessment |
|---|---|---|
| `--sandbox <PROFILE>` (`off`, `workspace`, `devbox`, `read-only`, `strict`, or custom from `sandbox.toml`; env `GROK_SANDBOX`) | **unsupported** | Verified: the five built-ins are accepted, an unknown name is rejected with a clear error. This is grok's own filesystem/network containment — directly analogous to codex's sandbox (MADR 0048) and currently unavailable to operators. **Recommend adding.** |
| `--tools` / `--disallowed-tools` | broken *and* documented "headless only" | Fix the placement, but verify they affect `agent stdio` at all before advertising them as working. May be inert for ACP sessions. |
| `--agents <JSON>` (inline subagent definitions), `--agent <NAME>` | unsupported | Out of scope; note for a future parity pass. |
| `--rules`, `--system-prompt-override` | unsupported | Out of scope. |
| `--max-turns` | unsupported, headless-only | Not applicable to ACP sessions. |
| `--no-plan`, `--no-memory`, `--experimental-memory` | unsupported | `--no-plan` interacts with the `plan` session mode; worth a look. |
| `/auto` slash command → "classifier permission mode" | not in the command table | grok gained a `/auto` command; our canonical slash-command registry (MADR 0023) should say what it maps to. |
| `staticModels` lists `grok-4.5`, `grok-code-fast-1`, `grok-4` | stale | `grok models` on this host reports **only** `grok-4.5`. MADR 0039 D2 already recorded the catalog as a floor that live data replaces, so this is cosmetic — but two of three entries are models the agent will refuse. |

## 5. Decision

**Treat the CLI argument vector as a versioned contract: fix the placement, pin
it with a test that fails when grok moves a flag again, and pin the permission
mode explicitly so the advertised modes describe reality.**

### D1 — Global flags go before the subcommand

`defaultArgs` emits `grok <global flags> agent --no-leader stdio`. Only
`--no-leader` stays after `agent`. `--always-approve`, `-m` and
`--reasoning-effort` are valid in both positions; put them with the other
globals so there is one rule rather than a per-flag exception list.

### D2 — Pin the contract with a live test that actually executes the binary

The regression is invisible to unit tests: `grok_test.go` asserts the argv
*we build*, which is exactly what was wrong. The pin has to be grok rejecting
or accepting the vector. A `live_grok` test spawns the real binary with each
configured flag and asserts it starts — so the next time grok moves a flag,
this fails instead of silently disabling seven options.

Rejected: parsing `grok --help` at runtime to place flags dynamically. It turns
a startup failure into a fragile heuristic against unstable text, and hides the
drift the test is meant to surface.

### D3 — Default `permission_mode` to `default`, explicitly

Ship `providers.grok.permission_mode: "default"` as the daemon's default rather
than leaving it empty. Empty means "whatever this host's grok config resolves
to" — unknowable to the daemon, host-dependent, and (per §3) possibly fully
permissive with no indication on the phone. An explicit `default` makes grok ask,
which is what makes the mode chip, the `dangerous` flag and MADR 0049's `auto`
mode meaningful.

This is a **behaviour change on upgrade**: hosts that were silently permissive
start prompting. That is the point — but it must be release-noted, and
operators who want the old behaviour set `permission_mode: bypassPermissions`
(or use the `auto` session mode, which is the supported per-session answer).

### D4 — Expose `--sandbox`

Add `providers.grok.sandbox` mapping to `--sandbox <PROFILE>`, validated
against the five built-ins with any other value passed through as a custom
profile name (grok resolves it from `sandbox.toml` and errors clearly if
missing). Empty = omit the flag, preserving grok's own default.

### D5 — Do not chase the rest of the surface in this MADR

`--agents`, `--rules`, `--system-prompt-override`, memory flags and the `/auto`
command are recorded in §4 as a follow-up parity backlog. Bundling them here
would delay a fix for options that currently break startup.

## 6. Consequences

**Good.** Seven config options stop breaking the daemon; `permission_mode`
becomes usable, which in turn makes MADR 0049's `auto` mode reachable and
meaningful; operators get grok's sandbox; and the argv contract gains a test
that notices the next drift.

**Cost.** D3 changes default behaviour: sessions that never prompted will start
prompting. Needs a release note and a documented opt-out.

**Risk.** The live test only runs where grok is installed, so CI will not catch
a future move — it is a developer/ops gate, not a build gate. Mitigated by
making the failure mode loud and by documenting the check in the plan's
verification step.

**Explicitly not addressed.** Whether `--tools`/`--disallowed-tools` have any
effect on `agent stdio` (documented headless-only) — the plan measures this
before the options are described as working.
