# OpenCode catalog and metadata parity: implementation plan

**Status:** Implemented
**Date:** 2026-07-26
**Decision:** [MADR 0031](./0031-opencode-catalog-and-metadata-parity.md)
**Verified target:** OpenCode 1.18.5. Re-probe live paths before accepting a
newer minor version.

## Goal and non-goal

Make OpenCode choices truthful before adding any new capability, then add only
session rename and an on-demand, read-only diagnostics view. Do not implement
provider-native session discovery/import: daemon-owned sessions already retain
their native IDs and resume normally.

## Implementation record

Phases 1–5 are implemented. Phase 0's existing local OpenCode 1.18.5 probe
was used for the route/schema evidence; deterministic fixtures cover request
and redaction behavior. Native-session discovery remains intentionally absent.

## Phase 0 — pin the observed contract

1. Add a `live_opencode` catalog test that records the installed version and
   asserts the live `/agent` response contains the expected classification:
   `build`/`plan` primary, `explore`/`general` subagent, and hidden internals.
   Do not assert exact custom-agent names outside the fixture.
2. Add a live model-catalog test using a configured non-OpenCode provider
   default when available, or a deterministic fake HTTP API test otherwise.
   It must prove `providers.opencode.model` is the selected catalog default.
3. Capture and fixture the minimum OpenAPI shapes used by P2: session-title
   patch response, `/vcs`, `/vcs/status`, and `/mcp`. Fixtures must exclude
   all URL, header, credential, and repository-content fields.

**Gate:** only paths confirmed by this phase are implemented. Live tests are
acceptance tests, not part of the normal token-spending loop.

## Phase 1 — P0 agent catalog and validation

1. Centralize OpenCode agent classification next to `agentInfo.visible` in
   `internal/provider/opencode/mode.go` or a dedicated small catalog helper:
   - visible + `primary`/`all` (including empty mode treated as primary) is
     top-level/startable;
   - `subagent` and hidden values are never startable;
   - the same helper feeds session modes and `agents.list`.
2. Change `StaticAgents` and `ListAgentsLive` to return only startable options
   and set `allow_custom=false`. Preserve live plugin-defined primary agents;
   they are discovered through `GET /agent`, not hand-maintained lists.
3. Add an optional, semantic start-agent validation seam to `httpagent` rather
   than putting OpenCode rules in WebSocket code. It validates a non-empty
   requested agent against the project-scoped live `GET /agent` catalog before
   creating or accepting a session. It returns a typed/clear validation error
   for unknown, hidden, and subagent names.
4. Ensure resumed sessions and subsequent prompt dispatch cannot bypass the
   invariant. Empty agent remains legal and selects OpenCode’s normal default.
5. Make `session.create` map the validation failure to a stable `bad_agent`
   protocol error; retain normal ownership and no-orphan guarantees.

**Tests:** helper table tests for every mode/visibility combination; catalog
ordering/default tests; direct WebSocket create tests proving subagent and
unknown rejection; a primary custom agent acceptance test; mobile picker test
proving a subagent never appears.

## Phase 2 — P0 configured model default

1. Add a small generic catalog policy at the `httpagent.Provider.ListModels`
   merge seam: when `Config.Model` is non-empty, replace the merged
   `DefaultIDs` with that value after live/static options merge. Keep
   `allow_custom` semantics so a valid configured model remains selectable even
   when a provider has not yet returned it.
2. Do not alter `httpSession.resolveModel`: it already correctly gives an
   explicit per-session model precedence over `Config.Model` and falls back to
   OpenCode only when both are absent.
3. Verify the mobile new-session dialog uses catalog defaults only when the
   user has no stored preferred model. A saved per-provider user preference
   remains more specific than the daemon default.

**Tests:** generic HTTP-agent catalog merge precedence; OpenCode live-catalog
fixture; session-create body with configured, explicit, and empty model;
Flutter picker default/preferred-model precedence.

## Phase 3 — P2 session rename

1. Add a semantic optional `provider.RenameSession` interface with
   `Rename(ctx, title)`; implement it in the OpenCode HTTP session by issuing
   project-scoped `PATCH /session/{agentID}`. Validate trimmed, non-empty title
   and set a documented length cap before the upstream request.
2. Add `session.rename` and `session.rename_result` protocol messages. The
   request carries `{session_id, name}`; the direct response carries full
   `session.Meta`. Do not add a broadcast event just for a list-label update.
3. Add `session.Manager.Rename`: authorize owner first, call the provider
   operation, mutate `Meta.Name` only after provider success, and persist
   atomically. Do not derive a title from model output or silently accept a
   partial failure.
4. Add server dispatch/error mapping and a Dart client method. Add a simple
   rename action to the session row/detail action sheet, optimistic only after
   success.

**Tests:** owner/non-owner, empty/over-limit title, provider failure atomicity,
persist/reload, OpenCode HTTP request shape and directory query, protocol round
trip, and Flutter success/error state.

## Phase 4 — P2 on-demand diagnostics

1. Add an optional semantic `provider.SessionDiagnostics` interface returning
   a shared struct with fields only for `Branch`, `DefaultBranch`, bounded
   `VCSStatus`, and `[]MCPServerStatus{Name, State}`. Enforce total string and
   item caps in the provider and again at protocol serialization.
2. Implement it for a live OpenCode HTTP session with project-scoped reads:
   `GET /vcs`, `GET /vcs/status`, and `GET /mcp`. Parse only allowlisted fields
   and ignore unknown fields. Failure of one read produces an unavailable
   section/notice, not a failed chat session; context cancellation bounds all
   three calls.
3. Add `session.diagnostics` / `session.diagnostics_result`. Authorize with
   the normal session owner check; invoke only on an explicit client request;
   do not record or broadcast it as transcript history.
4. Add a compact Diagnostics sheet in Flutter. It has no buttons capable of
   modifying VCS/MCP/configuration, hides absent data, and shows a clear
   unavailable state for unsupported providers.

**Tests:** redaction fixtures containing URLs/headers/tokens; result size caps;
owner/not-live/unsupported paths; partial upstream failure; no history/broadcast
side effect; Dart parsing and rendering tests.

## Phase 5 — documentation, verification, and rollout

1. Update [0021-opencode-http-api-coverage.md](./0021-opencode-http-api-coverage.md)
   from its 1.18.4 snapshot to 1.18.5 and mark completed tree/command/compact
   work accurately. Record the intentional exclusion of experimental native
   session discovery.
2. Update [protocol-v1.md](./protocol-v1.md) with rename and diagnostics
   schemas, authorization, payload bounds, and unsupported-provider behavior.
3. Run `go test ./...`, `go test -race ./...`, targeted live OpenCode tests,
   `flutter analyze`, `flutter test`, and Dart formatting. Before staging any
   Go changes run `make pre-add-check` on every changed Go file, per AGENTS.md.
4. Acceptance walkthrough: configured default `opencode-go/deepseek-v4-flash`
   is preselected and used; subagents are absent/rejected; owner can rename;
   diagnostics disclose only branch/status/MCP state; non-owner sees no data.

## Delivery order

Land P0 agent correctness first, then model-default correctness. Land rename
and diagnostics independently after their contracts and redaction tests pass.
No P2 work depends on native-session discovery, and no phase changes the
HTTP+SSE transport decision.
