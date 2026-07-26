# Goose remote parity: implementation plan

**Status:** Accepted — Phases 1, 2, and 4 implemented; Phase 3 evidence pending
**Date:** 2026-07-26
**Decision:** [MADR 0030](./0030-goose-remote-parity.md)
**Verified target:** Goose 1.44.0; rerun live probes before accepting a newer
version.

## Goal

Close the meaningful remote gaps without treating Goose's terminal UI as a
remote API. Preserve the working chat path and the existing daemon ownership,
history, and command-resolution boundaries.

## Implementation record (2026-07-26)

Implemented from Phases 1, 2, and 4:

- `session_capabilities` now reflects the negotiated ACP response rather than
  hard-coded Goose values, including embedded context, native session
  list/close, and each MCP transport.
- The Flutter protocol model carries the same negotiated fields.
- Goose no longer advertises a stale hard-coded model list before a session;
  its pre-session picker allows a configured model identifier and the live
  session config is authoritative.
- A requested model that Goose rejects now fails session creation rather than
  only producing a daemon log warning.
- `agent_sessions.list` is a bounded, authenticated, direct-response discovery
  API. `acphttp` gates `session/list` on negotiated capability, returns only
  metadata, and the mobile new-session picker loads a selected id through the
  existing `session.create` path.
- Configured MCP entries that Goose cannot accept now produce a control notice
  naming only the server and transport; secret header values never enter it.
- `providers.goose.with_builtins` is a typed, validated list that maps only to
  Goose's repeatable `--with-builtin` serve flag. Arbitrary process arguments
  remain unavailable.
- `/compact` and `/goal` are explicitly unavailable pending live ACP execution
  evidence, so Goose terminal commands are never forwarded speculatively.

Phase 0's remaining live probes and Phase 3's terminal-command execution proof
remain acceptance work. Phase 5 remains gated on MADR 0029 retention work.

## Phase 0 — pin the external contract before changing behavior

Add live-tagged Goose tests, each skipped when the binary/provider is not
available and run once at acceptance:

1. Initialize over `goose serve`; assert protocol version 1 and the observed
   capability set: load/list/close, image yes, audio no, embedded context yes,
   MCP HTTP yes, MCP SSE/ACP no.
2. Start a session; assert mode/config/capability events arrive before the
   first prompt, and save the actual model config-option shape as evidence.
3. Start a deliberately slow prompt, send the ACP cancellation notification,
   and assert no `-32601` plus a completed/cancelled lifecycle. Keep the
   existing wire test as the deterministic regression test.
4. Query `session/list`, choose one returned ID, load it, and assert the
   session is usable. Use a uniquely named test session and close it.
5. Capture `available_commands_update`; if `compact`/`goal` appear, send each
   in a disposable session and assert a meaningful ACP result. If not, pin the
   absence and keep them unavailable.
6. Configure one HTTP MCP fixture and one SSE fixture. Assert HTTP reaches
   Goose and SSE is rejected visibly without breaking session creation.

Record the Goose version in test failure output. Do not run these tests in a
unit-test loop: they may consume real provider tokens.

**Gate:** no behavior or table change is made on an inferred terminal feature.

## Phase 1 — negotiated capability and model correctness

### Daemon changes

1. Extend the semantic `event.Capabilities` / v1 schema with fields
   for embedded context, agent-session list, agent-session close, and supported
   MCP transports. Keep image/audio/load fields as part of the same complete
   negotiated snapshot.
2. Change `acphttp.session.emitCapabilities` to receive the stored initialize
   capability result. Remove hard-coded Goose facts. Unit-test a complete and
   an absent capability response.
3. Retain `session/new`/`load` config options as the authoritative model
   catalog. Make an explicit model-set failure visible to the caller; it must
   not be log-only.
4. Replace Goose's static six-model inventory with an allow-custom bootstrap
   catalog containing the configured default when present. Label it as a
   pre-session fallback. The post-create config option drives the actual UI.

### Mobile changes

1. Parse the optional capability fields defensively.
2. Present session config model choices after they arrive; do not overwrite a
   user-selected model until the agent confirms its current value.
3. Parse the complete capability snapshot and gate only the matching UI.

**Tests:** Go event/protocol round trips, config-option mapping and rejected
model update; Dart models/reducer/widget tests for bootstrap then confirmed
model state.

## Phase 2 — explicit native-session discovery and import

1. Add a cycle-free optional `provider.AgentSessionLister` interface and a
   neutral `AgentSessionMeta` type. It contains no transcript content.
2. Implement it in `acphttp` using `session/list`, gated by the negotiated
   capability. Ensure context cancellation and response-size limits.
3. Add authenticated `agent_sessions.list` request/result messages to
   `internal/protocol`, `internal/ws`, and the client. Validate provider ID,
   require the selected provider to be enabled/ready, and bound page/result
   size.
4. Add a picker in the mobile session flow. Selecting an entry calls the
   existing `session.create` with its agent session ID; no session is imported
   merely by listing it.
5. Persist the normal daemon record only after `session/load` succeeds. On
   failure, show an actionable error and leave no orphaned ownership record.

**Tests:** authorization (only the requesting device sees discovery results),
empty/large/malformed lists, selection/load success, load failure rollback,
and durable resume.

## Phase 3 — command contract and honest help

1. Update Goose's command table and MADR 0025/0026 evidence using Phase 0
   results. Do not add terminal commands to `command.Specs`.
2. Keep `/plan` unavailable unless a live probe proves a stable ACP-visible
   operation. If proven, introduce a Goose-specific, version-pinned native
   mapping and an explicit `/endplan` exit design; do not map it to ACP mode.
3. Keep `/compact` and `/goal` native only if their live execution tests pass.
   Otherwise change their mapping to `KindNone` with a reason.
4. Improve Goose `/help` caveats/notices to explain that terminal-local
   configuration, extension, display, editor, and diagnostic commands are not
   remotely executable.
5. Correct the stale claims in `docs/0026-mobile-goose-support.md` that
   provider-native commands automatically appear and work remotely.

**Tests:** table conformance, command-resolution snapshots, no forwarding of
unadvertised terminal commands, and live assertions for every `KindNative`
mapping.

## Phase 4 — typed Goose extensions and MCP diagnostics

1. Add `providers.goose.with_builtins: []string` to typed config, defaults,
   validation, example YAML, and config docs. Do not accept arbitrary process
   arguments.
2. Refactor Goose's spec construction so only its `goose serve` arguments add
   `--with-builtin`; retain the shared `acphttp` lifecycle unchanged.
3. Compare configured MCP transports with the post-initialize capabilities.
   Forward HTTP entries and emit one control notice per skipped entry, including
   the server name and unavailable transport.
4. Keep stdio extensions, `/extension`, and `/builtin` session-time mutation
   out of scope unless a future Goose ACP probe exposes a supported mechanism.

**Tests:** exact process arguments, duplicate/empty builtin validation, HTTP
MCP wire mapping, SSE omission notice, and no secret header values in logs.

## Phase 5 — bounded on-demand tool detail (conditional)

Start only after MADR 0029's retention-policy work defines a byte budget.

1. Keep current summary fields in transcript history.
2. Add an in-memory, per-session tool-detail store with a total byte cap, per
   tool cap, TTL, and redaction hooks.
3. Add an owner-authorized `session.tool_detail` RPC returning a selected tool
   call's bounded detail; never broadcast it to all connected devices.
4. Add a mobile expandable detail view with a clear truncation indicator.

**Tests:** cap eviction, authorization, UTF-8-safe clipping, no detail bytes in
history/cache, and a slow-client path that cannot block the stream.

## Completion criteria

1. Every Goose remote affordance is either live-proven and exposed, or visibly
   unavailable with a reason.
2. `session/list` discovery is explicit, authorized, bounded, and cannot
   import a session without user selection.
3. The model UI reflects Goose's session-provided configuration rather than a
   stale global catalog.
4. Unsupported MCP configuration cannot fail silently.
5. Cancellation, end-turn, resume, and provider-native commands have
   version-pinned live evidence.
6. Tool-detail expansion remains bounded and is deferred until retention policy
   makes its storage contract safe.
