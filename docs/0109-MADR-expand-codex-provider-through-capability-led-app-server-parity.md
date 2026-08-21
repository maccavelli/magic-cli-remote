---
status: accepted
date: 2026-08-20
decision-makers: Project Owner (scope and acceptance)
consulted: OpenAI official documentation; openai/codex source; local Codex 0.148.0 probes
informed: Implementers of the daemon, Codex provider, protocol, and mobile client
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Expand the Codex provider through capability-led app-server parity

## Context and Problem Statement

magic-cli-remote has a substantial Codex integration, not a thin command
wrapper. It launches one shared `codex app-server` process, negotiates the v2
JSON-RPC protocol, creates and resumes threads, streams turns and tool items,
handles approvals and questions, and exposes model, reasoning, collaboration,
goal, review, diff, fork, Fast, personality, and sandbox controls. That design
was established by [MADR 0028](./0028-MADR-codex-provider.md), hardened by the
0032–0048 Codex records, and expanded against Codex 0.147.0 by
[MADR 0080](./0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md).

The installed binary and the upstream source have moved beyond that research
baseline:

| Evidence | Observed 2026-08-20 |
| --- | --- |
| Installed binary | `/opt/homebrew/bin/codex`; `codex-cli 0.148.0` |
| Installed app-server schema | 95 stable client requests, 72 server notifications, and 10 server requests; experimental opt-in expands client requests to 141 and server requests to 11 |
| Live model catalog | Six models: `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, `gpt-5.5`, `gpt-5.4`, and `gpt-5.4-mini` |
| Live permission catalog | `:read-only`, `:workspace`, and `:danger-full-access`, all allowed on this host |
| Live native-thread discovery | `thread/list` returned two matching, unloaded, resumable threads using a state-database-only query scoped to this repository |
| Live MCP diagnostics | Two configured servers; the bounded `toolsAndAuthOnly` response exposed names, auth states, and tool counts without resources or configuration |
| Local upstream checkout | `/Users/saxsmith/gitrepos/codex`, clean `main` at [`6d020311f0f883ddf8c4622d36533527800ee905`](https://github.com/openai/codex/commit/6d020311f0f883ddf8c4622d36533527800ee905), matching `upstream/main`, 632 commits after local tag `rust-v0.147.0`, and 234 commits ahead of the configured fork's `origin/main` |

There is no local stable `rust-v0.148.0` source tag to bind to the installed
binary. The generated schema and live 0.148.0 probes are therefore the
compatibility evidence for features available now. The newer source checkout
is a forward-looking signal and must not be presented as proof that every
source-head method exists in the installed binary.

### Codex version-pin comparison

The repository does not enforce a Codex executable version. Production resolves
the configured `codex` binary and fills its version lazily
(`internal/provider/codex/provider.go:53-54`). The pins are compatibility
evidence in fixtures, tests, scripts, and earlier decisions:

| Repository evidence pin | Representative evidence | Installed comparison |
| --- | --- | --- |
| `0.147.0` | `internal/provider/codex/testdata/0.147.0`; `internal/provider/codex/collaboration_test.go:18`; MADR 0080 | Installed `0.148.0` is newer by one release. |
| `0.146.0` | `internal/provider/codex/device_auth.go:23`; `internal/provider/codex/device_auth_test.go:14` | Installed `0.148.0` is newer by two releases. |
| `0.145.0` | `docs/codex-spike-0.145.0`; `internal/provider/codex/session.go:281,680,1667`; `scripts/bwrap-apparmor-fix.sh:23,48` | Installed `0.148.0` is newer by three releases. |

The installed Codex binary is therefore newer than every Codex compatibility
pin found in the codebase. That is evidence of coverage drift, not a reason to
reject the executable: the provider already follows a runtime capability model
and should strengthen it with the exact-binary manifest in D2.

OpenAI's official [Codex app-server documentation](https://learn.chatgpt.com/docs/app-server)
calls app-server the interface for deep product integrations that need
authentication, conversation history, approvals, and streamed agent events.
The documentation also says generated schemas are specific to the Codex
version that produced them. That makes app-server, plus exact-version schema
and behavior probes, the appropriate provider contract.

This assessment asks:

> Which Codex surfaces should magic-cli-remote adopt now, which protocol gaps
> must be corrected before adding features, which controls belong in the
> provider rather than the phone or host administration plane, and should the
> provider change its API or transport boundary?

This record extends 0080 rather than rewriting it. The collaboration-mode,
goal, review, fork, diff, Fast, and personality decisions in 0080 remain in
force. If accepted, this record supersedes 0080's narrower deferrals wherever
the locked decisions below approve richer native sessions, commands,
administration, transport, extensibility, execution, and realtime surfaces.

### Current provider facts

The current implementation has several strong foundations:

* `internal/provider/codex/provider.go:359-422` owns one app-server process,
  uses stdio JSONL, registers and reaps the process, and starts the read pump
  before the initialization request.
* `internal/provider/codex/provider.go:433-470` opts into the experimental API
  and retries once without it only when initialization rejects the capability.
* `internal/provider/codex/session.go:1218-1541` has explicit handlers for
  turn lifecycle, assistant and reasoning deltas, item lifecycle, command
  output, token usage, plans, goals, diffs, rate limits, and MCP startup
  failures.
* `internal/provider/codex/items.go:10-53` classifies current tool, subagent,
  review, and compaction items through fail-closed allowlists rather than
  rendering unknown item types.
* `internal/provider/codex/mode.go:20-115` deliberately separates the
  permission/autonomy modes `default`, `read-only`, `auto`, and optional
  `full-access` from Codex collaboration modes.
* `go test ./internal/provider/codex` passes against the repository's current
  fixture and unit-test corpus.

Those facts do not establish current protocol parity. The passing tests are
mostly pinned to earlier captured shapes, including 0.147.0 collaboration
fixtures and 0.145.0 approval assumptions.

### Surface assessment

| Surface | Upstream and installed state | Current provider state | Finding |
| --- | --- | --- | --- |
| Product API | App-server v2 is the documented rich-client API. The Codex SDK and `codex exec` are recommended for automation and CI. | Uses app-server v2. | Keep the API choice. SDK/exec would lose the bidirectional approval, question, history, and event lifecycle already implemented. |
| ACP | No ACP client or server integration surface was found in the Codex Rust source or official Codex integration documentation. | Correctly does not negotiate ACP fields. | Do not invent an ACP adapter. The comment claiming Codex has no session-list equivalent is stale, however, because app-server now has `thread/list`. |
| MCP server mode | Installed CLI still exposes `codex mcp-server`. Upstream commit [`5e3a6fe4ee`](https://github.com/openai/codex/commit/5e3a6fe4ee) (“Warn when launching the deprecated MCP server”, an ancestor of the audited `6d020311f0` HEAD) warns that it is deprecated and will be removed. | Not used as the provider transport. | Keep it out of the control plane. It is neither ACP nor a replacement for app-server's client lifecycle. |
| Transport | App-server supports stdio, experimental WebSocket, Unix-socket WebSocket, and `off`. Official docs call WebSocket experimental and unsupported for production. | Shared child process over `--listen stdio://`. | Keep stdio as the default and add daemon-managed Unix-socket and WebSocket launch modes. Offer WSS through daemon-owned TLS/auth termination when Codex exposes only a plain WS listener. The daemon remains responsible for launch, supervision, reconnection, and shutdown; attachment to an externally managed endpoint remains deferred. |
| Schema negotiation | Installed schema has 95 stable requests and 141 with experimental opt-in. The official API says experimental methods and fields require explicit capability opt-in. | A single Boolean enables the whole experimental surface; method-not-found handling is feature-specific only in some areas. | Add a versioned contract manifest and per-capability degradation. `experimentalApi:true` is permission to probe, not evidence that every experimental method is safe to expose. |
| Native sessions | Stable `thread/list`, `thread/read`, resume, fork, metadata, archive, unarchive, delete, section, and loaded-thread APIs expose a complete native thread lifecycle. Experimental search and turn/item pagination improve scale. | Emits `ListSessions:false`; does not implement `AgentSessionLister`. `session.go:2276-2278` incorrectly says Codex has no equivalent. | Add a unified managed/native session browser with replay, search, resume, fork, rename, pinning/sections, archive, unarchive, and confirmed permanent deletion. Use native pagination when available and bounded fallbacks otherwise. |
| Permissions | Beta `permissionProfile/list` exposes built-in and custom profile ids, descriptions, and managed-policy `allowed` state. `approvalsReviewer:"auto_review"` routes eligible approvals to automatic review without widening the sandbox. | Four hard-coded legacy approval/sandbox pairs; network is always false and workspace writable roots are empty on turn overrides. | Add a dynamic permission-profile catalog and a separate automatic-review axis, with legacy fallback. Preserve the current unattended `auto` mode as distinct from safer auto-review. |
| Approval requests | Current v2 has method-specific response types, network approval context, available decisions, per-command permission overlays, and `serverRequest/resolved`. | Treats command, file, and granular permission requests as if all accepted `{decision:...}`. Ignores structured network context and resolved notifications. | Correctness defect. Granular permission approval requires `{permissions, scope}`, not `{decision:"accept"}`. Current auto-approval can log success while Codex interprets the malformed reply as an empty grant. |
| Questions | Current `ToolRequestUserInputQuestion.options` is an array of `{label,description}`; replies are a map keyed by upstream question id. Questions can allow Other and can be secret. | Decodes `options` as `[]string`, loses upstream question ids, and returns an ordered array. The phone supports Other but not option descriptions or secret fields. | Correctness and privacy defect. Current option-bearing questions fail to decode; replies use the wrong shape. Secret questions must fail closed until an obscured, non-persisted end-to-end field exists. |
| MCP elicitation | Stable server request supports standard form, OpenAI form, and URL flows. The 2026-08-18 official changelog notes standard MCP forms and editable message approvals in the iOS client. | Explicitly rejects every MCP elicitation. | Implement every stable form and URL mode with a generic schema renderer, secret handling, browser resume, validation, approval persistence when offered, and resolved-request cleanup. |
| Notifications | Installed schema has 72 notifications, including authoritative errors, warnings, Guardian/config/deprecation notices, readable reasoning summaries, tool progress, and resolved server requests. | Explicitly names only a subset; unhandled messages are debug-only. Provider routing drops every notification without `threadId`, making the existing account rate-limit handler unreachable. | Add thread-scoped and provider-scoped routing, plus a complete classified notification inventory. |
| Tool items | Current `ThreadItem` variants include command, file, MCP, dynamic, collaboration/subagent, web, images, sleep, review, and compaction. | Covers every current item type through tool cards, subagent state, notices, or deliberate silence. | Item-type breadth is good. Improve delta/final-state fidelity rather than adding duplicate cards. |
| Models and features | `model/list` is live and feature flags report apps, browser/computer use, goals, Guardian, hooks, image generation, plugins, multi-agent, tool elicitation, and other capabilities as stable and enabled. | Model, image, subagent, web, MCP tool, goal, review, and image-generation events are substantially covered. | Expose the full feature catalog and its lifecycle/managed state. Permit stable, beta, and development toggles through typed configuration plus an advanced key-path editor; capability-gate every use and preserve independent fallbacks. |
| Commands | Official CLI includes `/status`, `/skills`, `/mcp`, `/permissions`, `/approve`, `/ps`, `/stop`, `/usage`, `/archive`, `/delete`, `/import`, `/apps`, `/plugins`, `/hooks`, `/feedback`, and presentation-only commands in addition to the 0080 scope. | 0080 commands are implemented. Many newer stable operations remain unavailable. `/undo` and `/redo` correctly remain unavailable. | Add provider-backed commands and structured screens for the approved stable surfaces. Defer terminal-presentation emulation as a separate mobile design rather than treating it as provider functionality. |
| History rewrite | Stable `thread/rollback` is deprecated. Experimental `thread/revert` changes conversation history but explicitly does not revert local file changes; there is no redo counterpart. | `/undo` promises file-change reversal through `UndoSession` and is unavailable for Codex. | Keep `/undo` and `/redo` unavailable. Do not map conversation truncation to a command that promises working-tree restoration. |
| Host execution | `thread/shellCommand` is stable and intentionally unsandboxed; `command/exec` is stable and sandboxed. Experimental background-terminal APIs are thread-scoped. | Does not expose shell command or background-terminal controls. | Support both execution paths with unmistakably different labels and confirmation/policy treatment. Add `/ps`, per-terminal stop, and stop-all; terminals survive phone and daemon-session detach but end on explicit termination or app-server shutdown. |

### Highest-priority defects

#### Structured question requests are wire-incompatible

`internal/provider/codex/session.go:1732-1806` expects each option to be a
string and serializes answers as:

```json
{"answers":[{"answers":["choice"]}]}
```

The installed 0.148.0 schema requires option objects and an answer object keyed
by the upstream question ids:

```json
{"answers":{"question-id":{"answers":["choice"]}}}
```

An option-bearing request currently fails JSON decoding before reaching the
phone. A request without options can reach the phone, but the reply still has
the wrong shape. No focused Codex test covers the current request and response
schema.

#### Granular permission replies use the wrong response type

`internal/provider/codex/session.go:1544-1559` routes
`item/permissions/requestApproval` into the generic decision handler.
`internal/provider/codex/session.go:1009-1045` and `:1619-1651` consequently
reply with `{decision:"accept"}` for both human and automatic acceptance.

The installed response schema instead requires:

```json
{
  "permissions": {
    "network": {"enabled": true},
    "fileSystem": {"entries": []}
  },
  "scope": "turn"
}
```

Only `permissions` is required. `scope` is a `PermissionGrantScope` enum
(`turn` | `session`) defaulting to `turn`, and the response also carries an
optional nullable `strictAutoReview` boolean — “review every subsequent command
in this turn before normal sandboxed execution” — which is the per-grant
expression of the D7 reviewer axis and must be encoded, not dropped.

The granted value must be a subset of the request. App-server source defaults
an invalid or failed client reply to an empty turn-scoped grant. The current
provider can therefore emit an “Auto-approved” audit card while the engine
received no permission. This is semantic corruption, not a missing feature.

#### Provider-scoped notifications cannot reach existing handlers

`internal/provider/codex/provider.go:627-642` dispatches a notification only
when `params.threadId` identifies a managed session. The installed
`account/rateLimits/updated` payload has no thread id, so the detailed handler
at `internal/provider/codex/session.go:1470-1514` is unreachable. Global
account, app, config, and remote-status changes are dropped by the same route.
Warnings with a null thread id are also dropped.

The shared-engine design therefore needs two explicit destinations:

* thread-scoped messages go only to the matching session; and
* connection/provider-scoped messages update bounded provider state and fan
  out only a sanitized user-relevant projection where appropriate.

Raw global payloads, paths, account identifiers, URLs, configuration, and
credentials must never be broadcast.

### Expansion opportunities

| Priority | Opportunity | Product effect | Stability and boundary |
| --- | --- | --- | --- |
| **P0** | Repair questions, granular permissions, method-specific approvals, authoritative errors, and provider-scoped routing. | Prevents false approvals, broken forms, silent retries, and dropped warnings. | Required before feature expansion; stable 0.148 schema. |
| **P0** | Check in an exact-binary contract manifest and sanitized fixtures, then classify every request, notification, and callback. | Detects protocol drift before it becomes a phone-visible hang. | Stable and experimental inventories remain separate; each experimental capability degrades independently. |
| **P1** | Expand the managed app-server lifecycle and transport layer. | Supports supervised stdio, Unix-socket, WS, and daemon-terminated WSS operation with reconnect and capability renegotiation. | The daemon owns every endpoint; external endpoint attachment and Codex Remote Control remain deferred. |
| **P1** | Add the complete native thread browser, permission/reviewer catalog, account/usage state, diagnostics, and event parity. | Makes existing Codex work discoverable and gives users accurate runtime policy and account state. | Prefer stable RPCs; use bounded fallbacks where pagination/search are experimental. |
| **P2** | Add sandboxed and explicit unsandboxed execution, persistent queued turns, background terminals, filesystem management, and realtime/audio. | Brings remote operation to parity with current Codex execution workflows. | Preserve distinct authority labels, managed roots, reconnect behavior, and capability-specific fallback. |
| **P2** | Add skills, hooks, MCP, apps, plugins, marketplaces, configuration, feature flags, and browser/computer-use presentation. | Exposes Codex's extensibility and agent-operated tools as first-class mobile experiences. | Managed policy stays authoritative; destructive actions are confirmed and secret values never enter telemetry or transcripts. |
| **P2** | Add external-agent import, account login/logout, feedback, thread organization, and stable filesystem operations. | Moves setup and administration into the managed mobile/daemon product instead of requiring a parallel terminal. | Stable API first; preview mutations, report partial failure, and maintain audit state. |
| **Deferred** | `/undo`/`/redo`, runtime environments, Codex Remote Control, dynamic-tool registration, memory administration, Codex Cloud, Windows sandbox setup, TUI presentation parity, and source-head-only project/Bedrock setup APIs. | Preserves explicit future seams without narrowing approved stable functionality. | Experimental-only or platform-missing surfaces wait for a supported contract or daemon prerequisite. |

## Decision Drivers

* Protocol correctness must precede adding controls that exercise the broken
  request paths.
* The phone must never report an approval outcome different from the payload
  Codex accepted.
* The provider should be feature-rich. A capability is not excluded merely
  because it is advanced, mutating, or absent from other providers.
* Runtime-discovered capability and managed-policy state must outrank static
  lists copied from the TUI. Managed authority is expressed through explicit
  daemon configuration, confirmation, and audit—not by silently omitting APIs.
* Stable app-server surfaces are implemented by default. Beta or experimental
  adjuncts to an approved stable feature use explicit negotiation, isolated
  fallback, fixtures, and live probes. Experimental-only product areas may be
  deferred individually.
* The shared app-server must preserve strict per-thread routing while handling
  connection-scoped notifications intentionally.
* Credentials and secret form values must never enter logs, transcript events,
  notification payloads, or unencrypted persistence. Other data is exposed when
  the authenticated user invokes an approved feature, subject to daemon-managed
  roots and policy.
* Existing safe distinctions must survive: collaboration is independent of
  permissions; automatic review is independent of sandbox breadth; and
  unattended `approvalPolicy:"never"` is not the same as Guardian review.
* mcremote's authenticated connection remains the user control plane. Codex
  transport endpoints remain internal, daemon-owned implementation details even
  when Unix socket, WS, or WSS replaces stdio.
* Unknown methods and events must fail closed without crashing, hanging a
  turn, or forwarding raw upstream payloads to the phone.
* The implementation must remain useful on older Codex versions through
  capability fallback rather than a single minimum-version cliff.

## Considered Options

* Option 1: Keep the 0.147-era provider unchanged and rely on best-effort
  compatibility
* Option 2: Blindly mirror every Codex CLI command and app-server method
* Option 3: Replace app-server with ACP, MCP server mode, the SDK, or `codex
  exec`
* Option 4: Expand app-server through a capability-led, feature-complete managed
  parity layer (chosen)

## Decision Outcome

Chosen option: **“Option 4: Expand app-server through a capability-led,
feature-complete managed parity layer”**, because app-server remains the right
API while the current provider both mishandles parts of the stable wire contract
and omits substantial stable product functionality. This option implements the
approved Codex surface rather than enforcing an artificial lowest-common-
denominator provider API. It still distinguishes managed authority, protocol
maturity, platform prerequisites, and product ownership so that broad support
does not become an untyped JSON-RPC pass-through.

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | Keep app-server v2 as the provider API. Keep stdio as the default, and support daemon-launched Unix-socket WebSocket and WS transports when useful. Where secure WebSocket is required and Codex has no direct WSS listener, terminate TLS/authentication in a daemon-owned proxy to its managed WS listener. The daemon owns endpoint creation, supervision, reconnect, and shutdown. Do not attach to externally managed app-server endpoints yet. ACP, deprecated `codex mcp-server`, SDK/exec, and Codex Remote Control do not replace the provider boundary. |
| **D2** | Establish installed `codex-cli 0.148.0` as this record's runtime research baseline, not an unconditional minimum. Persist a compact stable/experimental manifest plus sanitized fixtures for every used request, notification, and callback. A live-tagged no-model test regenerates it from the configured binary. Source HEAD is leading evidence only. |
| **D3** | Correct every server-request encoder before expanding commands. Store the exact upstream method and typed payload. Use method-specific response types for current and legacy approvals; granular permission grants echo only the requested subset and scope. Unknown callbacks receive a bounded typed rejection and can never leave a turn blocked. |
| **D4** | Bring `item/tool/requestUserInput` to the installed schema: preserve upstream ids, labels, descriptions, Other support, blocking state, and keyed replies. Implement secret input end to end: obscure it and exclude its value from logs, transcripts, notifications, persistence, analytics, and errors. Reject secret questions until that path exists. |
| **D5** | Split thread-scoped and provider-scoped routing. Handle the complete classified notification inventory, including authoritative errors/warnings, configuration/deprecation/Guardian notices, account/app/plugin/skill changes, hook events, request resolution, model rerouting/verification, tool progress, and lifecycle changes. Never broadcast an unclassified raw payload. |
| **D6** | Add a unified native/managed thread browser. Implement list, read, lazy replay, resume, fork, rename, metadata, pinning, custom sections and ordering, archive, unarchive, permanent delete with descendant impact, loaded-state reconciliation, unsubscribe, and search. Use native pagination/search when negotiated and bounded full-history/catalog fallbacks otherwise. Link local aliases to the Codex thread id and preserve source badges and filters. |
| **D7** | Discover allowed permission profiles and keep permissions, reviewer, and unattended approval policy as independent axes. Hide policy-disallowed profiles, preserve opaque custom ids/descriptions, label danger-full-access, retain legacy fallback, expose Guardian state, and allow the exact confirmed `/approve` retry without widening the selected profile. |
| **D8** | Expose structured status, account usage, rate limits, workspace messages, model/provider capabilities, context usage, transport/lifecycle state, configuration provenance, and full MCP diagnostics. Apply pagination and size bounds, but do not reduce an explicitly opened authenticated detail view to a name-only projection. Mask credentials and secret values. |
| **D9** | Improve streamed and final event fidelity without duplicate cards. Prefer readable reasoning summaries, final `item/completed` state, bounded progress, terminal interaction, exit/duration, patch state, safety buffering, rerouting, verification, and `serverRequest/resolved` cleanup. Keep the ThreadItem allowlist fail-closed until each new item is classified. |
| **D10** | Support both stable host execution workflows: an explicitly labeled unsandboxed `!`/`thread/shellCommand` path and sandboxed `command/exec` with streaming stdin, PTY resize, and termination. Never present one as the other. |
| **D11** | Implement background terminal `/ps`, `/stop <id>`, and `/stop --all` with output cards. Terminals survive phone disconnect and daemon-session detach; explicit termination or managed app-server shutdown ends them. |
| **D12** | Use Codex's persistent queue APIs when negotiated and retain an in-memory fallback. Normal Send continues to steer the active turn. A distinct Queue action supports list, edit, delete, reorder, and explicit start. |
| **D13** | Support ordinary audio attachments and full realtime text/audio. Deliver attachments first, followed immediately by live realtime. Prefer phone WebRTC signaling relayed through the daemon with a daemon-proxied WebSocket fallback, and negotiate protocol/version independently. |
| **D14** | Defer runtime environments because their complete app-server control plane is experimental-only. |
| **D15** | Defer Codex Remote Control until its app-server management API stabilizes. mcremote remains the control plane; an optional future bridge may coexist with it. |
| **D16** | Provide agent-operated browser/computer-use parity: availability and policy state, execution/progress, screenshots, approvals, MCP elicitation, and results. Do not invent a separate manual remote-desktop protocol. |
| **D17** | Implement the full apps/connectors experience: catalog, metadata, enablement/accessibility, authentication, app-aware tool cards, and sandboxed MCP App widgets with structured fallback. |
| **D18** | Implement complete plugin and marketplace administration through the daemon: browse/search, add/remove/upgrade marketplaces, inspect/install/uninstall/enable/disable plugins, authenticate, show policy/errors, and manage workspace sharing. Confirm destructive changes. Use native search when negotiated and local filtering otherwise. |
| **D19** | Add typed configuration with layered provenance, atomic user/project writes, hot reload, the complete feature catalog and lifecycle labels, and toggles for allowed stable/beta/development features. Also provide an advanced arbitrary key-path editor. Mask credentials and enforce effective managed policy. |
| **D20** | Implement complete native skill discovery and management: enable/disable, forced refresh, live change events, extra roots, source/path/dependency/error metadata, official skill inputs, and preview. Installation comes from plugins/marketplaces or configured extra roots rather than a divergent installer. |
| **D21** | Implement all stable MCP form, OpenAI-form, and URL elicitation modes with generic schema rendering, nested validation, secret handling, browser resume, accept/decline/cancel, offered approval persistence, and resolution cleanup. Render persisted dynamic calls, but defer dynamic-tool registration/callback execution until that API is stable or mcremote defines a concrete tool family. |
| **D22** | Implement full stable direct MCP use: paginated servers/tools/resources, OAuth, reload, resource reads, direct tool calls, widgets, structured fallbacks, progress/cancellation, and exact argument/side-effect review. Confirm destructive or externally mutating direct calls. |
| **D23** | Implement guided Claude Code and Cursor import. Detect home/repository scope; allow granular selection of instructions, config, skills, plugins, MCP, subagents, hooks, commands, memory, and sessions; expose age/count filters; preview destinations/conflicts; confirm; stream progress; report per-item results; and retain import history and connector candidates. |
| **D24** | Implement the full stable account lifecycle: status, plan, rate limits, token/credit usage, workspace messages, browser/device-code/API-key/external-token/Bedrock login where offered, cancellation, switching, and confirmed logout. Secret values use the secret path. Confirm reset-credit consumption and credit-nudge email. Handle token-refresh callbacks in managed and external-auth modes. |
| **D25** | Support stable `feedback/upload` as an explicit workflow with classification, reason, tags, optional thread, reviewed opt-in logs, user-selected staged diagnostics, redaction, size limits, retry state, and temporary-file cleanup. Never upload feedback or logs automatically. |
| **D26** | Add a mobile workspace explorer/editor over all stable filesystem and fuzzy-search APIs: metadata, browse, read, write, create, copy, remove, watch/unwatch, and search. Workspace roots are granted by default; administrators can grant additional roots. Preview overwrites and confirm copy/remove. Use incremental search sessions when negotiated. Defer experimental arbitrary process APIs in favor of managed terminal controls. |
| **D27** | Implement stable hook discovery and management by workspace: event grouping, handler/matcher/source/plugin/trust/managed/error detail, exact-hash review and trust, enable/disable via atomic config, live started/completed cards, bounded output, prompt fragments, and refresh after config/plugin changes. |
| **D28** | Implement complete custom thread sections: paginated list, create, rename, icon/color, drag ordering, move in/out, built-in pinned behavior, and confirmed deletion that preserves and unsections member threads. |
| **D29** | Adopt experimental adjuncts to stable features when individually negotiated: collaboration catalogs, incremental fuzzy search, immediate thread setting updates, paginated thread replay/search, plugin search, and advanced server diagnostics. Each retains its stable fallback. |
| **D30** | Do not advertise attestation until the daemon has a supported token issuer. Type and reject unexpected callbacks promptly. Defer experimental external-clock callbacks. Defer Windows sandbox setup until the daemon supports Windows. |
| **D31** | Keep `/undo` and `/redo` unavailable because history revert does not restore working-tree changes. Defer experimental memory administration, Codex Cloud, TUI/presentation-command parity, and source-head-only experimental project and Bedrock setup APIs. Preserve memory-generated events without adding controls. |

### Capability classification rule

Every app-server method, notification, and server request in the captured
manifest must have exactly one classification:

* **implemented** — typed request/response or event mapping exists and is
  tested;
* **intentionally ignored** — the message is redundant or presentation-only,
  with a documented reason;
* **intentionally rejected** — Codex expects a client response but mcremote
  does not claim the authority or UI to provide it;
* **deferred** — valuable but gated by an explicit later decision, stability
  threshold, or platform dependency; or
* **unknown in newer binary** — fail closed, log only method/version/shape
  class, and trigger the live drift gate without logging the raw payload.

The classifier is not a pass-through registry. Adding a generated method name
does not authorize calling it.

### Explicit command boundary

| Command or family | Outcome under this decision |
| --- | --- |
| `/status`, `/usage`, `/debug-config` | Add structured status, usage, and layered-configuration screens with capability/auth fallback. |
| `/skills`, `/apps`, `/plugins`, `/hooks` | Add complete native discovery and approved management workflows. |
| `/mcp` | Add complete paginated status, OAuth, resources, reload, and direct tool use. |
| `/approve`, `/permissions` | Add exact Guardian retry, dynamic profiles, and a distinct auto-review axis. |
| `/ps`, `/stop` | Add per-thread background-terminal list, per-id stop, and stop-all. |
| `/archive`, `/delete`, `/import` | Add native archive/unarchive, confirmed descendant-aware delete, and guided Claude/Cursor import. |
| `/experimental` | Add the complete feature catalog with lifecycle and managed-policy state. |
| `/feedback`, `/logout` | Add explicit stable feedback and managed account lifecycle workflows. |
| `/memories` | Defer while its control API is experimental-only. |
| `/app`, remote-control commands | Defer with Codex Remote Control. |
| `/undo`, `/redo` | Keep unavailable because Codex history rewrite does not restore working-tree changes. |
| copy/raw/theme/title/statusline/pets/vim/keymap, `/ide`, `/side`, `/init`, `/new`, `/clear`, and other presentation-semantic parity | Defer as a separate mobile-product decision; do not misrepresent it as missing provider RPC support. |

## Consequences

* Good, because the provider will stop acknowledging granular permissions and
  questions with payloads Codex cannot interpret.
* Good, because native Codex sessions, organization, transcript replay, search,
  and lifecycle actions become first-class instead of metadata-only discovery.
* Good, because managed transport can evolve beyond stdio without introducing
  an externally owned endpoint or replacing mcremote's authenticated control
  plane.
* Good, because custom permissions, Guardian review, unsandboxed shell,
  sandboxed execution, terminals, queues, filesystem roots, and direct MCP
  calls remain visibly distinct authority choices.
* Good, because apps, plugins, marketplaces, skills, hooks, configuration,
  account management, imports, feedback, MCP, browser/computer use, and
  realtime/audio no longer require a parallel terminal or desktop workflow.
* Good, because a classified exact-version manifest turns silent upstream
  growth into an actionable compatibility signal.
* Good, because app-server source and schema growth can be adopted per feature
  without replacing a working transport or imposing a hard 0.148 minimum.
* Good, because experimental-only environments, memory, cloud, remote control,
  Windows setup, and source-head projects have explicit reconsideration points
  rather than vague permanent exclusions.
* Bad, because the approved scope crosses provider, daemon protocol, mobile UI,
  persistence, reconnection, authentication, browser return, and secret-data
  boundaries and therefore requires a multi-phase plan.
* Bad, because mutating host and account workflows require confirmation,
  provenance, conflict handling, recovery, and audit behavior that read-only
  provider integrations do not need.
* Bad, because beta and experimental adjuncts require lasting fallback paths,
  exact-version fixtures, and per-capability probes.
* Bad, because a complete method classifier and feature-rich UI add maintenance
  whenever Codex grows its app-server surface.
* Neutral, because terminal-specific presentation remains a separate mobile
  product decision even though all corresponding provider data can be exposed.

## Confirmation

This decision is confirmed only when an approved implementation plan defines
and its implementation passes all of the following evidence. These are
acceptance properties, not authorization to implement before plan approval.

* The saved installed-binary manifest records 95 stable requests, 72
  notifications, 10 stable server requests, 141 experimental requests, and 11
  experimental server requests, or a reviewed replacement count from the
  exact binary named by the implementation.
* Every manifest entry has one classification and unknown server requests are
  always answered; none can leave a turn waiting indefinitely.
* Captured 0.148 fixtures prove structured option decoding, keyed question
  answers, granular permission allow-once/session/deny behavior, legacy and v2
  approval decisions, resolved-request cleanup, error retry classification,
  and global rate-limit routing.
* A secret-question test proves the request is rejected until the end-to-end
  secret path exists; when implemented, its value is absent from history,
  logs, notifications, persistence, and failure output.
* A no-model live test initializes the configured binary across each supported
  managed transport, lists models, permissions, threads/sections, account state,
  skills/hooks, apps/plugins, and MCP state, and records sanitized summaries.
* Native session tests cover bounded listing and replay, resume, fork, rename,
  section ordering, archive/unarchive, descendant-aware delete confirmation,
  search fallback, loaded-thread reconciliation, and unsubscribe lifecycle.
* Profile tests prove `permissions` is never combined with legacy sandbox
  fields, disallowed profiles are hidden, danger-full-access is dangerous, and
  fallback engines retain the current modes.
* Automatic review tests prove the sandbox/profile is unchanged, review state
  is visible, a strict-review warning is not dropped, and `/approve` retries
  only the exact tracked denial once.
* Secret-flow tests prove credentials and secret form answers never cross logs,
  transcripts, notifications, analytics, or unencrypted persistence. Detail
  views prove non-secret MCP resources, tool arguments, configuration, account,
  filesystem, and import data appear only after the authenticated user opens or
  invokes the corresponding approved workflow.
* Transport tests cover daemon-owned endpoint authentication, reconnect,
  capability renegotiation, subscription recovery, and managed shutdown.
* Mutation tests cover atomic config/hook/skill/plugin writes, import previews
  and partial failure, account confirmation, filesystem root grants, destructive
  MCP annotations, feedback consent, and deletion recovery semantics.
* Terminal, queue, realtime/audio, browser/computer, hook, app-widget, MCP-form,
  and filesystem-watch tests cover disconnect/reconnect and request-resolution
  cleanup without duplicating terminal events or leaving callbacks pending.
* Existing Codex package, race, protocol, and mobile tests remain green, and
  the live prompt/approval/question suite passes against the chosen acceptance
  binary.

## Pros and Cons of the Options

### Option 1: Keep the 0.147-era provider unchanged

* Good, because it requires no code or protocol changes.
* Bad, because current structured questions are wire-incompatible.
* Bad, because granular permission acceptance can be logged as approved while
  Codex receives an empty grant.
* Bad, because native session listing and useful read-only diagnostics remain
  falsely advertised as unsupported.
* Bad, because global notifications and authoritative error detail continue to
  be silently dropped.

### Option 2: Blindly mirror every Codex surface

* Good, because it would maximize method-count parity quickly.
* Bad, because it would confuse method presence with a complete user contract
  for credentials, reconnect, mutation, confirmation, and partial failure.
* Bad, because it would expose experimental-only or platform-inapplicable APIs
  without independent capability fallback.
* Bad, because it would make raw JSON-RPC payloads the de facto mobile protocol
  and erase provider-neutral semantics where they are useful.

### Option 3: Replace app-server with ACP, MCP, SDK, or exec

* Good, because ACP would align mechanically with some other providers if
  Codex exposed it.
* Good, because SDK/exec are simpler for noninteractive jobs.
* Bad, because Codex exposes no ACP surface and current source is deprecating
  `codex mcp-server`.
* Bad, because SDK/exec do not replace the rich-client approval, question,
  history, and streaming contract mcremote already uses.
* Bad, because a migration would create broad regression risk with no product
  benefit demonstrated by this audit.

### Option 4: Feature-complete managed app-server parity (Chosen)

* Good, because it preserves the proven API while correcting current protocol
  defects and expanding every owner-approved stable capability.
* Good, because provider-neutral concepts are extended where useful without
  reducing Codex-specific features to a lowest common denominator.
* Good, because each beta or experimental adjunct can degrade independently.
* Good, because the few deferred surfaces have explicit maturity or platform
  prerequisites rather than blanket safety exclusions.
* Bad, because it requires substantially more protocol, UI, lifecycle,
  authentication, and fixture work than a generic JSON-RPC bridge.

## More Information

### Official research sources

* [Codex App Server](https://learn.chatgpt.com/docs/app-server) — rich-client
  integration boundary, protocol, exact-version schemas, thread APIs,
  permissions, approvals, questions, events, MCP, transports, filesystem,
  accounts, plugins, apps, hooks, and server callbacks.
* [Remote connections](https://learn.chatgpt.com/docs/remote-connections) —
  remote endpoint forms and the distinction between app-server listening and
  its outbound Code Mode host.
* [Codex CLI slash commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli)
  — user-visible permissions, status, usage, skills, MCP, Guardian retry,
  background terminals, imports, hooks, plugins, and presentation behavior.
* [Permissions](https://learn.chatgpt.com/docs/permissions) — beta named
  profiles, built-ins, managed-policy filtering, filesystem/network semantics,
  and incompatibility with legacy sandbox configuration.
* [Hooks](https://learn.chatgpt.com/docs/hooks) — discovery, trust-by-hash,
  managed hooks, lifecycle events, handler behavior, and configuration.
* [Import from another agent](https://learn.chatgpt.com/docs/import) — Claude
  Code and Cursor sources, selectable artifact families, and session bounds.
* [Plugins](https://learn.chatgpt.com/docs/plugins) and
  [Skills and Plugins](https://learn.chatgpt.com/docs/skills-and-plugins) —
  marketplaces, install/enable state, bundled apps/skills/hooks, and policy.
* [ChatGPT and Codex changelog](https://developers.openai.com/codex/changelog) —
  0.147.0 automatic-review/plugin/thread work and the 2026-08-18 iOS MCP form
  and editable-approval update.
* [openai/codex app-server source at the audited commit](https://github.com/openai/codex/blob/6d020311f0f883ddf8c4622d36533527800ee905/codex-rs/app-server/README.md)
  — forward-looking source contract and explicit warning that
  `thread/shellCommand` runs unsandboxed.

### Reproducible read-only probes used for this proposal

The assessment ran these non-mutating or temporary-output commands. Generated
schemas were written only to a temporary directory outside the repository and
were not committed.

```text
codex --version
codex --help
codex app-server --help
codex features list
codex app-server generate-json-schema --out <temporary-directory>
codex app-server generate-json-schema --experimental --out <temporary-directory>
go test ./internal/provider/codex
```

The live handshake used no model turn and issued only initialization and
read-only catalog/diagnostic requests. Output was reduced before display to
method counts, field names, model ids, permission ids/allowed flags, native
thread counts/status/source kinds, MCP names/auth states/tool counts, and
diagnostic top-level keys. It did not print transcript text, paths, resources,
tool arguments, account details, configuration, or credentials.

### Relationship to provider login credentials

[MADR 0074 §15](./0074-MADR-remote-provider-auth-from-phone.md) and its
[approved P17–P22 plan](./0074-PLAN-remote-provider-auth-from-phone.md) are the
controlling records for Codex account/API-key login, `CODEX_HOME`, credential
generations, logout, and phone device-auth ownership. This record's MCP-server
OAuth methods authenticate individual MCP servers inside Codex and are a
separate credential domain; no 0109 phase may bypass or duplicate the 0074
provider-login coordinator.

### Relationship to implementation planning

This record is `accepted`, and its paired
[0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md](./0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md)
was approved by the Project Owner on 2026-08-20. The plan enumerates the exact
phases, files, fixtures, protocol changes, mobile changes, verification
commands, and acceptance criteria for D1–D31.

Acceptance of this record and approval of that plan are not, by themselves, an
instruction to start executing phases. Under the repository's MADR/PLAN
workflow, phase execution begins only on a separate explicit instruction, and
any work a phase exposes outside D1–D31 stops for a fresh amendment and
approval rather than being implemented opportunistically.


## Erratum — 2026-08-21: independent re-verification of the 0.148.0 evidence

Every factual claim in this record was re-checked against the installed binary
and the working tree on 2026-08-21. The record is accurate; the following notes
record what was confirmed and the three corrections applied above.

**Confirmed unchanged.** `codex --version` still reports `codex-cli 0.148.0` at
`/opt/homebrew/bin/codex`. Regenerating both schemas reproduces the counts in
the evidence table exactly: stable `ClientRequest` 95, `ServerNotification` 72,
`ServerRequest` 10; experimental `ClientRequest` 141, `ServerNotification` 72,
`ServerRequest` 11. The single experimental-only server request is
`currentTime/read`, which D30 already defers as the external-clock callback.
The stable server-request inventory is exactly the ten this record names.

**Confirmed defects.** All three highest-priority defects reproduce against the
installed schema:

* `ToolRequestUserInputResponse` requires
  `{"answers":{"<question-id>":{"answers":[…]}}}`, and
  `ToolRequestUserInputOption` requires both `label` and `description`. The
  current `[]string` decode and ordered-array reply at
  `internal/provider/codex/session.go:1732-1806` cannot satisfy either.
* `PermissionsRequestApprovalResponse` requires `permissions`, defaults `scope`
  to `turn`, and has no `decision` member at all, confirming that the generic
  `{decision:"accept"}` reply at `session.go:1009-1045` and `:1619-1651` is
  semantic corruption rather than a missing feature.
* `internal/provider/codex/provider.go:626-642` still routes notifications
  solely by `params.threadId`, leaving the `account/rateLimits/updated` handler
  at `session.go:1470-1514` unreachable.

Every other file, line, and version-pin citation in this record resolves to the
cited content, and `go test ./internal/provider/codex ./internal/protocol
./internal/event ./internal/session ./internal/ws` is green.

**Corrections applied.** The `codex mcp-server` row now identifies
`5e3a6fe4ee` as an ancestor of the audited `6d020311f0` HEAD rather than as
“current source commit”. The granular-permission defect section now records
that `PermissionGrantScope` is the enum `turn | session` and that the response
carries an optional nullable `strictAutoReview` flag, which is the per-grant
expression of the D7 reviewer axis and must be encoded rather than dropped.
“Relationship to implementation planning” no longer describes this record as
`proposed`. No D1–D31 decision, deferral, or boundary changed.
