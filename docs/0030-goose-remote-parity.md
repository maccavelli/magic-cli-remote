# MADR 0030: Evidence-based Goose remote parity

- **Status**: Proposed
- **Date**: 2026-07-26
- **Deciders**: Project Owner
- **Scope**: Goose v1.44.0 through `mcremote`; the ACP-over-HTTP transport,
  Goose provider declaration, v1 session capabilities, command resolution, and
  Flutter session surfaces.
- **Related**: [MADR 0023](./0023-canonical-slash-commands.md),
  [MADR 0025](./0025-goose-provider.md), [MADR 0029](./0029-provider-platform-canonicalization.md), and [protocol-v1](./protocol-v1.md).
- **Implementation plan**: [goose-remote-parity-implementation-plan.md](./goose-remote-parity-implementation-plan.md).

## Context and evidence

`goose --version` reports **1.44.0**. A live ACP initialize probe, repeated
through both `goose serve` and `goose acp`, reported the following negotiated
capabilities:

| Capability | Goose 1.44.0 result | Current mcremote state |
|---|---|---|
| `loadSession` | supported | used for a daemon-known resume |
| `sessionCapabilities.list` | supported | not used; historical Goose sessions are undiscoverable |
| `sessionCapabilities.close` | supported | used by `acphttp.session.Close` |
| prompt image | supported | represented and sent |
| prompt audio | unsupported | correctly hidden |
| embedded context | supported | not represented by `provider.Content` |
| MCP HTTP | supported | forwarded |
| MCP SSE / ACP | unsupported | silently filtered when configured |
| optional auth method | `goose-provider` | only invoked when configured |

The repository's live Goose test passed against the installed binary. It
observed `session_mode`, `session_config`, `session_capabilities`,
`user_message`, `session_status`, `session_title`, streamed assistant text,
`usage_update`, and `turn_complete(end_turn)`. This validates the core chat
path and confirms that session config is a live source of model state.

The terminal's `/help` reports a broader command set: `/mode`, `/model`,
`/plan`, `/endplan`, `/compact`, `/goal`, `/grind`, `/status`, `/skills`,
`/prompts`, `/prompt`, `/extension`, `/builtin`, `/recipe`, `/doctor`,
`/edit`, `/clear`, and presentation commands. The live ACP session did not
emit an `available_commands_update` during session start or the tested turn.
MADR 0023's earlier live probe establishes that only `compact` and `goal` are
advertised by Goose in the relevant command survey; advertisement still does
not by itself prove that forwarding works.

`session/cancel` was found to be an ACP JSON-RPC **notification**. Sending it
as a request caused Goose's `-32601 Method not found` error. The transport now
sends a notification and has a wire-level regression test.

## Socratic assessment

| Decision question | Evidence-based answer | Consequence |
|---|---|---|
| Should remote parity mean every terminal command is forwarded? | No. Many commands operate a local terminal, editor, theme, stored configuration, or extension process; ACP exposes neither their state nor a reliable result stream. | Do not create a generic pass-through for unadvertised terminal commands. |
| Can `/plan` be mapped to mcremote's canonical plan mode? | No. Goose's `/plan` is a terminal workflow; Goose ACP exposes permission modes (`auto`, `approve`, `smart_approve`, `chat`), not a `plan` session mode. | Keep canonical `/plan` unavailable for Goose unless a live probe proves a stable remote operation and event contract. |
| Should the daemon trust `available_commands_update`? | No. MADR 0023 proved that another CLI advertises commands which do not execute over ACP. Goose's own advertisement varies by session/version. | Use a provider declaration plus live tests as the allowlist; an advertisement alone can only enable a previously proven command. |
| Does Goose's model picker belong to a static provider catalog? | No. The binary supports configured providers/models and emits per-session config options. A six-item hard-coded list inevitably becomes stale. | Session config is authoritative after creation; use a small verified fallback and permit an explicit model value before creation. |
| Does `session/list` mean mcremote should import every Goose session? | No. That would expose local agent history without a user choice or durable ownership mapping. | Offer explicit, authenticated discovery/import; preserve the existing daemon-only `/sessions` semantics. |
| Should unsupported MCP transports be silently dropped? | No. Silent omission makes a configured tool look broken. | Validate negotiated MCP transports and report skipped servers clearly. |
| Is full terminal tool output safe to put in transcript history? | No. It conflicts with current bounded-history policy and can contain large or sensitive data. | Keep summaries in history; add an authorized, bounded on-demand detail path only after retention policy work. |

## Decision

Adopt **evidence-based remote parity** for Goose. A capability is exposed only
when its effect, result, lifecycle, and authorization boundary are observable
through ACP or a dedicated mcremote operation. Terminal-only features receive
an explicit explanation; they are never sent blindly as model text.

### 1. Negotiate, preserve, and expose capabilities accurately

`acphttp` must retain the initialize capability result and emit a semantic v1
capability snapshot from it rather than constructing Goose-specific constants.
The snapshot may expose image/audio/embedded-context prompt support,
load/list/close support, and MCP transports. It must not expose raw ACP types
to Flutter.

This is backward compatible: new optional fields are absent to old clients;
old daemons leave them unset. The mobile UI uses them only to gate an available
action, never to assume an action is supported.

### 2. Make models session-authoritative

The `session/new` / `session/load` `configOptions` response is the authoritative
catalog for a running Goose session. `models.list` remains useful before a
session exists, but it must not pretend that a hand-maintained catalog is a
complete Goose catalog. It will offer the configured default and an allow-custom
entry, labelled as a Goose-configured model; the created session then publishes
the authoritative config option.

An explicit requested model is applied through `session/set_config_option`.
Failure is a session-create error or an immediately visible notice, never a
warning hidden in daemon logs. The implementation must not restart a Goose
conversation merely to switch model when its config option supports an
in-place change.

### 3. Discover Goose sessions only on explicit request

Add a narrow optional provider interface for listing resumable native sessions.
It returns neutral metadata (agent session ID, title/name, updated timestamp,
and optional cwd/model), not transcript contents. Goose implements it with
ACP `session/list` only when the negotiated capability permits it.

Add an authenticated `agent_sessions.list` v1 operation scoped to a chosen
provider. Its result is a picker, not an automatic import. The user explicitly
selects an entry; only then does `session.create` call `session/load` and create
the daemon-owned record and ownership mapping. Existing `/sessions` continues
to list only mcremote-owned sessions.

Do not add delete/fork support: Goose did not advertise those capabilities.

### 4. Separate canonical, verified-native, and terminal-only commands

Keep the MADR 0023 canonical vocabulary unchanged. Its table remains the
source of truth for remote commands.

- `/mode`, `/model`, `/context`, `/clear`, `/new`, and `/sessions` retain
  mcremote's existing remote semantics.
- `/compact` and `/goal` may remain `KindNative` only after Goose live tests
  prove that the advertised command generates an observable, correct ACP turn.
- `/plan`, `/endplan`, `/grind`, `/status`, `/skills`, `/prompts`, `/prompt`,
  `/extension`, `/builtin`, `/recipe`, `/doctor`, `/edit`, theme/output toggles,
  and `/exit` are terminal-only by default. Do not forward them merely because
  a terminal accepts them.

If a later Goose release publishes a stable ACP command/update contract for one
of these features, add it through a version-pinned provider manifest with a
live test. `/plan` is especially not a synonym for an ACP mode.

### 5. Make MCP configuration and tool detail honest

At engine initialization, compare configured MCP servers with negotiated
transports. Forward supported HTTP servers. For each unsupported server, emit
a startup/session notice naming the server and required transport; never
silently omit it. Add Goose-only `with_builtins` configuration by constructing
the Goose serve arguments from a typed Goose config, not an arbitrary command
argument escape hatch. Stdio extensions remain terminal-only unless Goose
advertises a safe ACP mechanism.

Keep the transcript's short tool summaries. A later, authenticated
`session.tool_detail` retrieval can return a bounded, redaction-aware detail
buffer for a selected live tool call. It depends on MADR 0029's byte-aware
retention decision and is not part of the initial parity release.

### 6. Pin external behavior with live tests

Every Goose-specific assertion must be live-tagged and version-aware:
capabilities, model configuration, `session/list`/load, cancellation, command
advertisement/execution, HTTP MCP, and the absence of unsupported transports.
Unit tests cover mapping, authorization, cache/replay, and error handling; they
must not infer Goose behavior from fixtures alone.

## Consequences

- The mobile app becomes more trustworthy: it shows actions that can work,
  rather than a terminal command catalog that may silently fail.
- Goose users gain explicit historical-session discovery and accurate model
  configuration without exposing their whole local history automatically.
- The provider transport becomes more reusable because it reports negotiated
  facts instead of embedding Goose constants.
- Some terminal features remain unavailable remotely. This is intentional and
  visible in help/diagnostics, not a claim of feature parity.

## Alternatives rejected

- **Forward every `/command` as a prompt.** This bypasses command policy,
  may send local UI commands to the model, and recreates the silent-failure
  problem MADR 0023 solved.
- **Add every Goose terminal command to canonical `command.Specs`.** Those
  commands are not cross-provider remote capabilities and would burden every
  provider with misleading unavailable entries.
- **Scrape Goose's SQLite database.** It bypasses ACP authorization/version
  contracts and couples the daemon to a private local schema.
- **Preserve unlimited tool output in the transcript.** It defeats existing
  memory, disk, and mobile-cache bounds.
- **Use arbitrary extra serve arguments for built-ins.** It creates a command
  injection/configuration review surface; typed `with_builtins` is sufficient.

