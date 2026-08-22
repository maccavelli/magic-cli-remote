---
status: accepted
date: 2026-08-22
decision-makers: Project Owner
consulted: none
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Pin OpenCode known-good to 1.18.21 after surface and behavior verification

## Context and Problem Statement

[0019](./0019-MADR-opencode-process-management-plan.md) made one
daemon-owned `opencode serve` process and its HTTP/SSE API the only OpenCode
transport. [0020](./0020-MADR-opencode-session-tree.md),
[0021](./0021-MADR-opencode-http-api-coverage.md), and
[0034](./0034-MADR-opencode-tool-stream-fidelity.md) then built the phone
session loop against OpenCode 1.18.4–1.18.7. Later auth evidence reaches
1.18.16, but the provider has never acquired the known-good release pin used
by Kilo.

The installed binary is now **OpenCode 1.18.21** at
`/Users/saxsmith/.opencode/bin/opencode`. `opencode --version` and isolated
`GET /global/health` both report `1.18.21`; the executable is a Darwin
arm64 Mach-O with SHA-256
`8c783005340f8dfc5e7d168478dd0dd2bd1faead531cb34270de2a9689d9f135`.
The official [v1.18.21 release](https://github.com/anomalyco/opencode/releases/tag/v1.18.21)
is marked latest and was published on 2026-08-21.

Matching sources are present at `/Users/saxsmith/gitrepos/opencode`.
The release behavior boundary is upstream commit
[`57fa34f235`](https://github.com/anomalyco/opencode/commit/57fa34f23599f65dd1027f9caac31e6c576ce644);
[`ad0bb6d9a3`](https://github.com/anomalyco/opencode/commit/ad0bb6d9a3e779def694adc093a811e86a529df0)
then synchronizes `packages/opencode/package.json` and the SDK packages to
1.18.21. The local clone has no `v1.18.21` tag, so this record does not claim
that local HEAD or a local tag is the release. `ad0bb6d9a3` is an ancestor of
the local `dev` HEAD `ff3ef6e3e6`; the intervening commits contain later
Bedrock, pricing-documentation, and Nix-hash work. The assessed source boundary
is therefore the package-version commit `ad0bb6d9a3`, with the release's
behavioral commit and the installed binary/live `/doc` used as corroboration,
not mutable local HEAD.

The provider currently has only `MinVersion = "1.18.0"`
(`internal/provider/opencode/version.go:9-13`). When session trees are on,
versions below that floor are rejected; every version at or above the floor is
otherwise treated alike. The existing live “version pin” starts the engine but
then only asserts that `MinVersion` meets itself
(`internal/provider/opencode/live_http_test.go:624-651`). It does not assert
the version that actually answered health.

This record asks: **is OpenCode 1.18.21 compatible with the HTTP/SSE provider,
what behavior and tooling does the release expose, which surfaces should
mcremote adopt, and what evidence must exist before 1.18.21 becomes the
known-good release?**

## Decision Drivers

* The installed binary and its live health/OpenAPI/ACP responses are the
  executable contract; official release notes explain intent but do not
  replace probes.
* “Known-good” and “minimum supported” are different policies. A fast-moving
  newer release should produce a useful drift warning, not an unconditional
  outage, while the 1.18.0 session-tree floor remains enforced.
* The phone session loop is the product boundary: prompt, stream, tools,
  permissions, questions, modes, commands, model selection, resume, cancel,
  delete, fork, revert, diff, compact, and child-session lifecycle.
* Set equality matters more than aggregate counts for HTTP paths, operations,
  schemas, and Event discriminators.
* Engine behavior can change while the OpenAPI sets remain identical; release
  source and bounded live behavior probes are both required.
* Environment-dependent model/provider catalogs, credentials, project skills,
  and project commands must not become release invariants.
* Existing transport and process-ownership decisions remain in force unless
  runtime evidence disproves them. A more capable ACP implementation is not by
  itself a reason to restore one full engine per session.
* External-CLI assumptions used by the decision must become live-tagged tests
  or sanitized, reproducible fixtures as required by `AGENTS.md`.
* Unit-test coverage must grow with the implementation. Live OpenCode probes,
  end-to-end tests, and a green aggregate suite do not excuse uncovered local
  branches, errors, validation boundaries, or concurrency behavior.
* An 80% aggregate can conceal an untested package or a large touched file.
  The floor must therefore apply independently to each touched Go package,
  each touched Dart production file, and the Flutter application as a whole.

## Considered Options

* Option 1: Add a 1.18.21 known-good warning pin, retain the 1.18.0 hard floor,
  refresh the stale bootstrap model and tool classification, and keep the
  HTTP/SSE control plane (chosen)
* Option 2: Require exactly 1.18.21 and refuse every other OpenCode release
* Option 3: Leave only the 1.18.0 minimum and treat 1.18.21 as an unrecorded
  best-effort upgrade

## Decision Outcome

Chosen option: **"Option 1: Add a 1.18.21 known-good warning pin, retain the
1.18.0 hard floor, refresh the stale bootstrap model and tool classification,
and keep the HTTP/SSE control plane"**, because 1.18.21 preserves the exact
1.18.7 HTTP path, operation, schema, and Event-type sets; its provider,
session, retry, compaction, subagent, MCP, and ACP changes are compatible; and
the only provider defects found are bounded local assumptions rather than a
wire migration.

This decision was accepted by the Project Owner on 2026-08-22, together with
the 2026-08-22 stable-parity scope amendment and the A14/A15 additions from the
same day's reverification. Acceptance settles the decision; it is not by itself
authorization to start work. The companion
`0112-PLAN-opencode-1.18.21-surface-parity.md` is approved as the sole execution
plan, and execution begins only on the owner's explicit go-ahead in the turn
that starts it.

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | Add `KnownGoodVersion = "1.18.21"` alongside `MinVersion = "1.18.0"`. Health reports exact known-good as info and any other parseable version as known-good drift. Only the existing below-minimum, session-tree-enabled case remains a startup error. |
| **D2** | Replace the current live version test with a real exact-version assertion against both `opencode --version` and `GET /global/health`. Keep separate unit coverage for the 1.18.0 minimum and for known-good warning behavior. |
| **D3** | Treat the primary OpenAPI shape as unchanged from 1.18.7 to 1.18.21: **162 paths, 188 HTTP operations, 472 schemas, and 89 canonical top-level Event discriminators**, with empty set diffs in all four categories. No REST route, request body, SSE demux, or event decoder change is justified by the release. |
| **D4** | Record the one OpenAPI content change without inventing a set change: `ProviderConfig` and `Model` broaden interleaved-reasoning field configuration to accept `reasoning_text` and custom field names. This is provider/model behavior, not a new phone event. |
| **D5** | Keep the legacy session routes used by the dialect plus the one proven v2 model route. On 1.18.21, `POST /api/session/{id}/wait` still returns 503 “not available yet” and `POST /api/session/{id}/compact` still returns 503 “not available yet”; `GET /api/session/{id}/context` and history work. Continue `/session/{id}/summarize`, `/session/status`, and SSE turn completion. Do not migrate wholesale to `/api`. |
| **D6** | Refresh the bootstrap/default-model policy. The hard-coded preferred IDs `opencode/deepseek-v4-flash-free` and `opencode/north-mini-code-free` are absent from the 1.18.21 OpenCode Zen catalog. Seed `opencode/big-pickle`, which isolated 1.18.21 reports as the OpenCode default with zero cost, and prefer the engine-reported `default["opencode"]` after boot instead of maintaining a latency-ranked list of mutable catalog IDs. Explicit operator/session models continue to win. |
| **D7** | Keep generic tool streaming and add `apply_patch` to the edit-kind mapping. Isolated 1.18.21 exposes 14 tool IDs: `invalid`, `question`, `bash`, `read`, `glob`, `grep`, `edit`, `write`, `task`, `webfetch`, `todowrite`, `websearch`, `skill`, and `apply_patch`. The dialect already streams every tool state, but `kindForTool` currently classifies `apply_patch` as `other` while `edit` and `write` are `edit`. `invalid`, `skill`, and agent-specific/MCP tools remain generic rather than receiving speculative special cases. |
| **D8** | Accept skills and references as engine-owned capability, not a new mcremote control plane. `GET /skill`, the native `skill` tool, `GET /command` skill entries, project/global `SKILL.md` discovery, and configured local/Git references are automatically available to the agent. Existing external-directory and tool permission events continue to flow to the phone. `--pure` continues to mean no external plugins; it does not disable skills, and current documentation already says plugins. |
| **D9** | Accept 1.18.8–1.18.11 MCP compatibility fixes as engine improvements. mcremote continues to expose MCP tool calls through generic tool streaming and aggregate server state through Diagnostics `GET /mcp`. Adding/removing servers, OAuth, connect/disconnect, and credential management from the phone remain a separate product/security decision. |
| **D10** | Keep OpenCode ACP out of the provider transport. Runtime ACP protocol 1 advertises load plus close/fork/list/resume, embedded-context/image prompts, and HTTP/SSE MCP. A no-model session exposed a 991-option model select and build/plan mode select; `session/set_config_option`, `session/list`, and `session/close` succeeded. Source also implements effort, set-mode, and unstable set-model paths. The 1.18.14 turn-drain and cache-write usage fixes improve ACP clients, but `opencode acp` still starts an internal HTTP server per subprocess and does not satisfy 0019's one-engine invariant. |
| **D11** | Take the 1.18.8–1.18.21 behavior fixes that naturally apply under `serve`: provider/network retry coverage, chronological message/fork/revert ordering, compaction-history preservation, bounded retries, subagent failure surfacing, OpenCode Go web search, and continuation after unknown finish reasons. They require compatibility confirmation, not new protocol operations. ACP-only turn-drain/usage fixes and `opencode run` child-permission handling are recorded but do not alter the HTTP dialect. |
| **D12** | Persist a sanitized 1.18.21 evidence corpus and live gates before moving the pin. It must include exact CLI/version/health identity, sorted OpenAPI path/operation/schema/Event sets and comparison summary, ACP initialize plus no-model session summary, built-in agent/command/tool summaries, v2 stub responses, and a README with commands and source boundaries. Do not commit raw `/provider`, credentials, absolute user-state contents, model outputs, or the multi-megabyte raw OpenAPI document. |

### Required work

1. Add the known-good constant, health warning behavior, exact version tests,
   and a no-model live surface gate; preserve the existing 1.18.0 minimum gate.
2. Replace the stale bootstrap seed and latency-ranked fallback logic with the
   1.18.21 engine default policy from D6; update static/offline catalog tests.
3. Classify `apply_patch` as an edit tool and pin the full runtime tool-ID
   observation without requiring every environment-dependent tool to appear.
4. Add the sanitized evidence corpus from D12 and compare its canonical sets
   mechanically with the retained 1.18.7 source boundary.
5. Update the living HTTP coverage inventory and append pin errata to prior
   OpenCode records where needed; do not rewrite their historical rationale.
6. Run package/race checks, the no-model live gates, and once at acceptance
   the existing token-bearing OpenCode prompt, tool, permission, resume,
   cancel, mode, command, subagent, and session-tree suite.

Explicitly not required: restoring ACP transport, exposing the v2 API as a
phone API, adding MCP/skill/reference/plugin configuration screens, adopting
TUI/desktop `mini` or replay controls, running `opencode run`, managing the
OpenCode database, enabling mDNS/CORS, or pinning the mutable provider/model
catalog.

### Stable-parity scope amendment (2026-08-22)

The owner subsequently requested the broadest practical parity with OpenCode's
**stable** feature set. This amendment expands the bounded uptake selected
above without changing the transport decision, the single-engine invariant,
or the exclusion of experimental functionality. It supersedes D8, D9, and D12
only as stated below; the other locked decisions remain in force.

OpenCode's public server documentation, the 1.18.21 OpenAPI document, the
installed 1.18.21 binary, and the reconstructed local release-boundary source
form the stability boundary. A feature is admitted only when official prose
documents it, its public schema and exact release-boundary source agree, and
its required behavior is either reproduced on the installed binary or covered
by a sanitized fixture plus a per-phase loopback gate. Generated OpenAPI alone is
not sufficient. Internal Effect group descriptions saying “Experimental
HttpApi” are also not sufficient to exclude a route: OpenCode uses that label
for legacy groups that the public server documentation explicitly lists. The
actual exclusions are every explicit `/experimental/*` operation, every new
`/api/*` dependency, every mutation absent from the public prose, and a small
set of documented operations whose lifecycle or security model conflicts with
the daemon boundary. The one existing `/api/session/{id}/model` use remains a
proven compatibility exception. “Maximum useful stable parity” therefore means
the largest coherent product surface, not mechanical equality with every
documented HTTP route.

| ID | Amended decision |
| --- | --- |
| **A1** | Add provider-native root-session discovery from stable `GET /session`, plus an engine project/worktree catalog from stable `GET /project`. These make existing OpenCode work resumable and make valid engine worktrees selectable without filesystem guessing. Results are bounded, sorted, and stripped to identifiers, titles, relative or approved working-directory metadata, model, agent, timestamps, and aggregate usage/cost. |
| **A2** | Advertise actual model input capabilities and variants. OpenCode image/audio inputs become normal prompt attachments through `FilePartInput` data URLs under the existing 1 MiB frame limit. The mobile app gains an explicit bounded audio-file picker; today it has image picking plus audio wire/model/display support, not audio composition. Variant selection is governed by **A14**: the exact 1.18.21 `Model.variants` payloads are reasoning-effort configurations, so they are carried by the existing thinking-level contract rather than a second control. Video and PDF remain unadvertised until the shared client has safe pickers, previews, and size handling. |
| **A3** | Make replay and live streaming converge on one part mapper. For OpenCode prompts, preassign an upstream-valid `msg_` ID, attach it to the optimistic user event, and submit the same ID in `prompt_async`; the first authoritative user-part snapshot replaces that message-level optimistic row, and later native parts update ordered components inside the same rendered user bubble. Preserve native message and part IDs and distinguish append deltas from authoritative replacement snapshots so replay and `message.part.updated` cannot duplicate prior deltas. The daemon history ring applies the same replacement/removal identity rule before persistence, preventing resume from consuming the bounded ring with duplicate transcript copies. Replay full tool input/output/error state and tool-state attachments, surface safe assistant `FilePart` artifacts, and process message/part removal. `session.compacted` emits a bounded notice; it must not invoke append-only replay. Updated compacted tool parts reconcile by native identity. Internal snapshot, patch, step, and retry bookkeeping is not rendered as invented chat content. |
| **A4** | Extend usage additively with input, output, reasoning, cache-read, cache-write, and USD cost while preserving the existing used/context fields. Per-turn values come from the latest assistant message and are not mislabeled as cumulative session totals; native discovery may expose OpenCode's aggregate session accounting separately. |
| **A5** | Add a read-only, session-scoped workspace surface using documented, functional `GET /file`, `GET /file/content`, `GET /find`, and `GET /find/file`. Although documented and present in OpenAPI, the 1.18.21 handlers for `/file/status` and `/find/symbol` unconditionally return empty arrays and are therefore not exposed as working features. `GET /vcs/status` works and is already consumed for aggregate Diagnostics, but is absent from the official server route table; retain it only as a grandfathered compatibility dependency and do not add a new phone operation or file-status UI around it. Workspace requests are rooted to the already approved session CWD, accept normalized relative paths only, reject every existing symlink component introduced beneath that root, strip absolute paths/URIs, reject binary content, and enforce response limits before the general 16 MiB HTTP helper. Because OpenCode, rather than mcremote, opens the path, a concurrent local filesystem actor can still race validation; the supported threat boundary assumes the engine's local workspace is not concurrently hostile. No write/apply endpoint is added. |
| **A6** | Expand diagnostics with sanitized skill metadata, LSP status, formatter status, and complete MCP state. `GET /lsp`, `GET /formatter`, and `GET /mcp` are in the official server route table; `GET /skill` is **not**, exactly like the already grandfathered `/vcs/status`. Skills are nevertheless admitted because they are documented as a stable product feature, the route is GET-only in both the exact release-boundary source and the 1.18.21 OpenAPI, and its full discovery/cache/refresh lifecycle was reproduced on the installed binary. The route-table absence is recorded as errata, not concealed, and it does not extend to any mutation: no skill write, delete, or configuration route is admitted. See **A15** for the exact rule this makes explicit. Skill contents/locations, LSP roots, MCP errors, headers, credentials, and OAuth details never cross the phone protocol. The canonical `mcp.tools.changed` and `lsp.updated` events trigger a debounced refresh; formatter and skill refresh are explicit because the stable Event union has no corresponding update event. This amends D8 from engine-only visibility to read-only metadata exposure; direct skill/reference/plugin filesystem administration remains engine-owned, while agent-mediated skill authoring is retained by A10. |
| **A7** | Keep MCP status and generic MCP-tool streaming, but do not expose MCP lifecycle/configuration control. The official server table does document `POST /mcp` dynamic add; exact 1.18.21 source shows that it updates only instance memory and can launch an arbitrary local command with environment variables or connect to an arbitrary remote URL with headers and OAuth client secrets. There is no documented matching delete operation, while connect/disconnect and OAuth routes remain source/OpenAPI-only. A one-way, transient, secret-bearing mutation is not a coherent phone lifecycle and duplicates the host-owned configuration/credential plane. This reaffirms D9 as an intentional architecture/security exclusion rather than incorrectly classifying dynamic add as undocumented. |
| **A8** | Permit stable session share/unshare only when `providers.opencode.allow_remote_share` is explicitly true. Mutation policy never hides an already shared native session: resume must surface its existing share state honestly. The phone must explain that sharing synchronizes the transcript to OpenCode's service and makes it accessible to anyone with the link, require explicit confirmation for each share, accept only bounded HTTPS result URLs, and never override an upstream `share: disabled` policy. |
| **A9** | Permit stable session shell execution only when `providers.opencode.allow_remote_shell` is explicitly true and the authenticated owner explicitly confirms the exact command. OpenCode's shell endpoint bypasses normal model tool-permission evaluation, so it is treated as remote command execution: bounded command length, existing bounded tool-output snapshots, a finite request timeout, no environment-editing or PTY affordance, and disabled-by-default configuration are mandatory. SSE is the canonical event path and the blocking HTTP response is not remapped, preventing duplicate tool cards. A timeout cannot undo filesystem/network effects or guarantee termination of descendants; the confirmation must disclose that the command and output become OpenCode session history. |
| **A10** | Retain and regression-pin stable functionality already implemented: authentication/device flow, core session lifecycle, commands including `/init`, primary agents and modes, permission/question replies, todos, diffs, fork, revert/redo, compact, model switching, subagent trees, cancellation, generic custom/MCP tool streaming, and agent-mediated skill creation/update through the normal prompt, `customize-opencode` skill, write/edit tools, and permission flow. Add a phone affordance that composes a valid project-local authoring request but never writes skill files itself. The deterministic contract is prompt composition, normal tool/permission mediation, and an idle refresh loopback; a model actually choosing to author is an informational token-bearing smoke, not a release gate. After the turn is idle, explicit refresh may recycle only that idle worktree's OpenCode instance through documented `POST /instance/dispose`, then reload skill/command diagnostics. Command argument hints are added to the existing picker rather than creating duplicate controls. |
| **A11** | Replace D12's fixture scope with a stable-only corpus: runtime and acceptance code never calls `/experimental/tool/ids`, `/experimental/capabilities`, ACP, or the excluded v2 wait/compact stubs. Previously observed ACP/experimental/v2-stub output remains only as historical research in this MADR, clearly labeled non-contractual. Machine fixtures and stable gates derive from official routes, exact release-boundary source/OpenAPI, the one grandfathered v2 model compatibility route, and observed normal prompt/tool streams. |
| **A12** | Do not add ACP, a broad v2 migration, OpenCode TUI/web/run/plugin/database/GitHub/PR administration, provider/config credential proxying, project metadata mutation, phone-supplied server log injection, a raw phone-side skill/reference/plugin filesystem editor, transient `POST /mcp` add or other MCP connection/configuration/OAuth control, public server exposure, mDNS/CORS, undocumented sync/message/part/VCS mutations, the nonfunctional 1.18.21 status/symbol stubs, or any `/experimental/*` feature. A10's agent-mediated skill authoring is explicitly included because it stays inside OpenCode's normal tool and permission boundary; direct stable-but-host-local administration remains excluded when it violates daemon ownership, complete-lifecycle, or security boundaries. |
| **A13** | Unit tests are part of every production change, not a cleanup phase. Establish reusable exact-count Go/LCOV tooling in P0, then execute a test-and-gate-only P0A debt-closure phase before product changes. The hard default-tag floor is 80.0% statement coverage for every Go package touched by this plan, 80.0% line coverage for every existing Dart production file touched by it, and 80.0% for the Flutter application in aggregate. P0A targets at least 82.0% on every currently sub-floor target to provide deterministic headroom, adds a local `make coverage-check` target, and adds the same check as a non-allow-failure CI job. `internal/provider/opencode` retains the stronger requirement: raise its observed 83.36% to at least 85.0% in P1. New Dart production files require at least 90.0%. Before and after every later production phase, additionally require no lower exact coverage fraction, no increase in uncovered statements/lines, and a strict improvement in at least one touched executable target. Tests directly exercise success, failure, boundary, compatibility, and applicable race/cancellation paths. Live, token-bearing, loopback, and end-to-end tests supplement but never count toward these unit floors. |
| **A14** | Model variants are the OpenCode reasoning-effort control, not a new session-config concept. Every `Model.variants` value observed on 1.18.21 is a reasoning configuration and nothing else — `{"reasoningEffort":"high"}` for OpenCode Zen and xAI-family models, `{"thinkingConfig":{"includeThoughts":true,"thinkingLevel":"high"}}` for Google models — and OpenCode persists the choice as `Session.model.variant` with the reserved sentinel `default`. mcremote already owns exactly this contract: `picker.ThinkingLevel` is a deliberately open-string, provider-advertised rung list, `provider.ThinkingSession` records a level for subsequent turns, `/thinking` is a canonical command, and the mobile client already renders the picker. Map variant keys onto `picker.Option.ThinkingLevels` and implement `provider.ThinkingSession` on the OpenCode session, sending the selection as the documented optional `variant` field on `prompt_async` and `command`. Do **not** add a parallel `variant` entry to `session_config`: two independent effort controls on one OpenCode session is a product defect, and MADR 0052's "never a daemon-invented ladder" rule is satisfied exactly by passing OpenCode's own variant keys through. Three existing assertions that OpenCode has no per-session effort control are now false and are corrected in the same change: the comments at `internal/provider/provider.go:388-392` and `internal/picker/picker.go:52-56`, and — operationally the important one — the live gate at `internal/provider/opencode/commandtable.go:24-29`, which pins `"thinking"` to `command.KindNone` with the note "OpenCode has no per-session thinking level — the model decides" and must become `{Kind: command.KindOp, Op: command.OpSetThinkingLevel}`. Because OpenCode applies `variant` per request the level is settable mid-session, so `provider.ErrThinkingLevelFixed` is never returned by this provider. |
| **A15** | State the stability rule as it is actually applied, so admission and exclusion use one test. A route is admitted when **either** the official server route table lists it **or** official feature prose documents the capability *and* the exact release-boundary source and 1.18.21 OpenAPI agree *and* required behavior is reproduced on the installed binary *and* the operation is read-only or otherwise completable and reversible within the daemon boundary. Under that rule `GET /skill` is admitted (feature-documented, GET-only, reproduced) and the pre-existing `GET /vcs/status` remains a grandfathered read. MCP connect/disconnect/OAuth stay excluded not merely for prose absence but because they are secret-bearing, host-owned mutations with no complete phone lifecycle; documented `POST /mcp` stays excluded on the same lifecycle/security ground already stated in A7. Prose absence alone never admits a **mutation**. |

#### Stable surface value and disposition

| Stable surface | Platform value | Disposition |
| --- | --- | --- |
| Existing sessions and projects | High: resume work and select valid roots | Implement A1 |
| Model variants and input capabilities | High: reasoning controls and multimodal prompts | Implement A2; variants land on the existing thinking-level contract per A14 |
| Message/part fidelity and artifacts | High: correct replay, edits, compaction, and generated files | Implement A3 |
| Detailed usage and cost | High: transparent spend and context accounting | Implement A4 |
| File/list/content and text/file search | High: useful remote repository inspection without shell access | Implement A5; exclude the two empty stubs and new `/vcs/status` UI |
| Skills, LSP, formatter, and MCP diagnostics | Medium-high: explain what the engine can use and why it is degraded | Implement A6 |
| Agent-mediated skill authoring and idle refresh | High: create reusable project workflows without a raw host editor | Retain and expose A10 |
| Existing MCP status and tool calls | Medium-high: explain and use configured integrations | Retain/expand sanitized A6 diagnostics |
| Dynamic `POST /mcp` add | Potentially useful and documented, but transient, one-way, secret/command-bearing, and host-owned | Exclude A7/A12 deliberately |
| MCP connect/disconnect | Potentially useful, but undocumented as a public remote-control feature | Exclude A7/A12 |
| Share/unshare | Medium: intentional collaboration | Implement A8, opt-in |
| Direct shell | High utility and high risk | Implement A9, opt-in with confirmation |
| Agents, commands, permissions, todos, session operations, auth | High and already present | Regression-pin A10 |
| Unit tests and coverage enforcement | High: keeps provider and cross-layer reliability growing with parity | Implement A13 in every production phase |
| MCP connection/OAuth/config/credential authoring | High risk, secret-bearing, or undocumented | Exclude A7/A12 |
| ACP and host-local CLI/TUI/database administration | Conflicts with one-engine/provider boundary | Exclude A12 |
| Experimental or undocumented routes | No stable compatibility contract | Exclude A11/A12 |

#### Stable-parity alternatives

* **Bounded stable product parity (chosen).** Good, because every admitted
  operation has a complete phone lifecycle, explicit ownership, bounded data,
  and a regression contract. Bad, because documented operations such as
  transient MCP add remain intentionally unavailable when the daemon cannot
  safely complete or reverse their lifecycle.
* **Mechanical parity with every documented server route.** Good, because the
  endpoint checklist would be larger. Bad, because it would proxy host config,
  credentials, public-link publication, arbitrary command execution, and a
  one-way in-memory MCP mutation without a unified security or rollback model.
* **Pin-only compatibility.** Good, because it minimizes implementation risk.
  Bad, because it leaves high-value stable discovery, multimodal, transcript,
  diagnostics, workspace, and agent-mediated authoring capability unused.

The companion `0112-PLAN-opencode-1.18.21-surface-parity.md` is the sole
execution plan for both the original decision and this amendment. This MADR
and this amendment were accepted together on 2026-08-22.

## Consequences

* Good, because operator logs distinguish a verified release from a merely
  above-minimum release without taking newer engines offline.
* Good, because compatibility is grounded in exact wire sets and runtime
  behavior rather than a version-string-only change.
* Good, because the provider keeps its single-engine ownership invariant and
  does not reacquire per-session Bun processes.
* Good, because the first session no longer starts from two model IDs absent
  from the current catalog.
* Good, because `apply_patch` renders consistently with other edits on the
  phone while unknown/custom tools remain forward-compatible.
* Good, because MCP, skills, references, subagents, and provider fixes remain
  usable through the engine without duplicating their configuration surfaces.
* Good, because the documented but transient MCP add route is evaluated on its
  actual command/secret/lifecycle behavior instead of being mislabeled or
  exposed as an incomplete control plane.
* Good, because skill authoring stays behind OpenCode's ordinary agent tool and
  permission decisions while a bounded idle-instance refresh makes the result
  discoverable without restarting the shared engine.
* Good, because existing work, multimodal input, reasoning effort, artifacts,
  usage, and read-only workspace inspection become first-class phone
  capabilities.
* Good, because reasoning effort reuses the one thinking-level contract the
  daemon, protocol, and mobile picker already implement, so an OpenCode session
  cannot end up showing two competing effort controls.
* Good, because the stability rule is stated as the test actually applied, so a
  read-only route absent from the prose table (`/skill`, and the grandfathered
  `/vcs/status`) and a documented but incomplete mutation (`POST /mcp`) are
  decided on their own merits instead of on one ambiguous sentence.
* Good, because the two high-risk stable controls are visible only after
  explicit daemon policy enables them and the phone applies confirmation or
  catalog validation.
* Good, because every change set carries its unit/widget tests and measurable
  coverage delta, so the large parity expansion cannot hide test debt behind
  live probes or a later stabilization phase.
* Good, because the per-package and per-file 80% floors prevent strong
  OpenCode coverage or a large well-tested Dart file from masking weak shared
  provider, protocol, WebSocket, daemon, or screen logic.
* Bad, because the pin requires fixtures and live gates rather than a
  two-line constant change.
* Bad, because transcript identity, artifacts, workspace browsing, and rich
  usage require additive shared-protocol and mobile-client work, not only an
  OpenCode dialect change.
* Bad, because exact before/after coverage capture and targeted gap closure add
  work to every phase and may require testing adjacent legacy branches before a
  feature commit can pass.
* Bad, because the current tree is below the new floor in eight planned Go
  packages, four planned Dart files, and the Flutter aggregate; the
  test-and-gate-only P0A phase is substantial work that must land before parity
  implementation.
* Bad, because every release after 1.18.21 will warn until the compatibility
  assessment is repeated.
* Neutral, because the large v2/experimental API and most direct CLI surface
  remain visible upstream but intentionally outside the phone control plane.

## Pros and Cons of the Options

### Option 1: Known-good warning pin plus bounded compatibility uptake (Chosen)

* Good, because it matches the fast-release policy already used for Kilo.
* Good, because it fixes the two concrete local assumptions discovered by the
  assessment while preserving stable transport and wire behavior.
* Good, because an exact live gate makes the pin falsifiable.
* Bad, because future releases produce drift noise until re-probed.
* Neutral, because most 1.18.21 gains are engine behavior and require no new
  daemon API.

### Option 2: Hard-require exactly 1.18.21

* Good, because every running engine would be byte-for-release aligned with
  the verified contract.
* Bad, because OpenCode releases frequently and an otherwise compatible
  security or provider fix would make the provider unavailable.
* Bad, because an exact version gate is stronger than the evidence requires;
  the 1.18.7→1.18.21 wire sets are identical.

### Option 3: Keep only the 1.18.0 minimum

* Good, because it needs no code or maintenance.
* Bad, because logs and diagnostics cannot distinguish the tested release from
  an unassessed future one.
* Bad, because the current live “pin” remains circular and the stale model/tool
  assumptions remain undocumented.
* Bad, because future regressions cannot be attributed to a persisted release
  contract.

## Confirmation

* Unit tests distinguish `KnownGoodVersion == "1.18.21"` from
  `MinVersion == "1.18.0"`; warning and hard-floor cases are separate.
* An isolated no-model live test asserts exact `opencode --version` and
  `/global/health` values, and proves the health hook recorded the runtime
  version rather than comparing a constant with itself.
* The committed comparison reports 162 paths, 188 operations, 472 schemas,
  and 89 canonical Event types at both 1.18.7 and 1.18.21, with empty set
  differences.
* The required current paths and decoded Event types used by
  `internal/provider/opencode` are asserted explicitly, not inferred from
  counts.
* Historical no-model research records ACP initialize/session configuration
  and the 14 observed built-in tool IDs, but A11 removes experimental tool
  endpoints from the acceptance contract. Stable live gates cover visible
  primary agents and built-in commands/skill exposure; the two v2 503 stubs
  used to justify D5 remain historical evidence and are not acceptance calls.
* Unit tests prove that OpenCode variant keys become `picker.ThinkingLevels`
  rungs, that `SetThinkingLevel` accepts only a rung the active model
  advertises, that the selection reaches `prompt_async`/`command` as `variant`,
  that the reserved `default` sentinel is never sent, and that no `variant`
  entry appears in `session_config`.
* Unit tests prove `apply_patch` maps to edit and the default-model seed is
  `opencode/big-pickle`; live startup proves session creation does not depend
  on asynchronous catalog refinement.
* Existing read-only live tests for health, agents, catalog scoping, auth
  catalog, and `--pure` pass against 1.18.21.
* A stable loopback gate proves `/skill` is GET-only, a post-discovery skill
  write remains cached, guarded idle `/instance/dispose` preserves native
  session/message/tool history, and the refreshed skill appears in both
  `/skill` and `/command`.
* Workspace gates call only the four documented functional read/search routes.
  The existing aggregate Diagnostics call to `/vcs/status` is regression-pinned
  as a compatibility exception and is not used to create a new phone route.
* Deterministic unit and loopback tests prove the authoring affordance composes
  the ordinary OpenCode prompt, tool, and permission path, that the daemon has
  no direct skill writer, and that guarded disposal refreshes discovery without
  losing persisted session/message/tool history. A separately authorized
  token-bearing model smoke is recorded as informational because a model may
  validly decline to invoke a write tool.
* P0 records exact covered, total, and uncovered counts. P0A raises every
  currently sub-floor planned Go package, planned existing Dart file, and the
  Flutter aggregate to at least 82.0% using only deterministic default-tag
  unit/widget tests. Every target is thereafter hard-gated at 80.0%.
  `internal/provider/opencode` reaches at least 85.0% in P1 and never falls
  below it; every new Dart production file reaches at least 90.0%.
* Each production phase passes an exact-count before/after gate for all touched
  targets: the coverage fraction does not fall, uncovered statements/lines do
  not increase, and at least one touched executable target improves. P11 also
  enforces every floor cumulatively. Live/token-bearing tests are excluded from
  these unit metrics.
* `make coverage-check` and a non-allow-failure Ubuntu CI job independently
  enforce the absolute floors from fresh profiles. Same-platform phase/P11
  deltas enforce non-regression; CI does not compare Linux counts to the Darwin
  baseline.
* `go test -race ./internal/provider/opencode/` and the repository's Go
  pre-add checks pass before staging Go files.
* At acceptance, the token-bearing `live_opencode` suite passes once against
  a usable zero-cost or operator-selected model. A model declining a requested
  tool is not misreported as proof of a permission or tool-stream contract.

## More Information

### Probe method and runtime results

All no-model raw probes used temporary XDG config/data/cache/state directories,
`OPENCODE_DISABLE_AUTOUPDATE=1`, `--pure`, and explicit external-skill
disable variables when the intent was to measure only built-ins. Servers bound
only to `127.0.0.1` and were terminated after the probe.

The `Experimental capability`, `v2 session stubs`, and ACP rows below are
historical assessment evidence only. A11 forbids using them as fixtures,
acceptance gates, or production dependencies.

| Probe | Result |
| --- | --- |
| Binary identity | `opencode --version` = 1.18.21; health = `{"healthy":true,"version":"1.18.21"}` |
| CLI top level | `acp`, `serve`, `run`, `mcp`, `agent`, `debug`, `session`, `plugin`, `db`, provider/auth, model, stats, import/export, GitHub, PR, web/TUI |
| HTTP OpenAPI | 162 paths; 188 operations; 472 schemas; 89 canonical Event types |
| HTTP set comparison | Empty path, operation, schema-name, and Event-type diffs against v1.18.7 |
| Isolated agents | Primary `build` and `plan`; subagents `general` and `explore`; hidden `compaction`, `title`, and `summary` |
| Isolated commands/skills | Built-in `init`, `review`; built-in `customize-opencode` skill is also exposed by legacy `GET /command` |
| Isolated tools | 14 IDs listed in D7 |
| Experimental capability | `{"backgroundSubagents":false}` |
| v2 session stubs | Empty-session context/history respond; wait/compact both 503 “not available yet” |
| ACP initialize | Protocol 1; load, close/fork/list/resume, HTTP/SSE MCP, embedded context/image, login auth |
| ACP session | 991 model options and build/plan modes in the probe environment; set mode, list, and close succeeded |
| Direct model/tool smoke | `opencode/x-preview-f-free` completed a read-tool call against line 1 of `go.mod` and returned the line; 24 seconds, exit 0 |
| Existing no-token live tests | Auth catalog, scoped catalog, version-floor start, primary-agent catalog, and pure-flag tests all passed against 1.18.21 |

The model/provider numbers above are observations, not acceptance constants.
During the same isolated probe, the live catalog had 193 upstream providers,
9 connected providers, 62 OpenCode Zen models, 22 OpenCode Go models, and
`default["opencode"] == "big-pickle"`. These values are served by mutable
catalog data and can change independently of the binary.

### Canonical OpenAPI comparison

The source and runtime comparison resolves every `$ref` in
`components.schemas.Event.anyOf` and reads only the referenced schema's
top-level `properties.type.enum`. Recursive descent is not used because
nested objects may contain unrelated `type` values.

| Item | 1.18.7 | 1.18.21 | Set delta |
| --- | ---: | ---: | --- |
| Paths | 162 | 162 | empty |
| HTTP operations | 188 | 188 | empty |
| Schemas | 472 | 472 | empty |
| Event discriminators | 89 | 89 | empty |

The generated OpenAPI file has a 26-line addition / 6-line deletion content
diff, entirely within `ProviderConfig` and `Model` interleaved-reasoning
configuration. Official 1.18.11 notes describe the intended fix as accepting
`reasoning_text` and custom reasoning field names. It does not change the
Event union or any route mcremote calls.

### Release delta relevant to the provider

| Release | Official/core change | mcremote assessment |
| --- | --- | --- |
| [1.18.8](https://github.com/anomalyco/opencode/releases/tag/v1.18.8) | MCP SDK v2, expired-session reconnect, OAuth callback-port handling | Engine-owned MCP reliability; no daemon wire change (D9) |
| [1.18.9](https://github.com/anomalyco/opencode/releases/tag/v1.18.9) | Restored legacy MCP SDK compatibility | Engine-owned MCP reliability (D9) |
| 1.18.10 | Modal model discovery | Mutable catalog only |
| [1.18.11](https://github.com/anomalyco/opencode/releases/tag/v1.18.11) | MCP SSE reconnect-loop fix; broader interleaved reasoning fields | Engine reliability plus OpenAPI content change (D4, D9) |
| 1.18.12 | Azure GPT-5.5+ reasoning fix | Provider/model behavior only |
| 1.18.13 | GitHub PR context and desktop work | Outside serve control plane |
| [1.18.14](https://github.com/anomalyco/opencode/releases/tag/v1.18.14) | Device-only xAI login, retry expansion, ACP cache-write usage and turn drain, workspace proxy fixes | HTTP provider retries apply; ACP changes recorded but transport stays out |
| [1.18.15](https://github.com/anomalyco/opencode/releases/tag/v1.18.15) | Chronological message/fork/revert ordering, compaction-history and truncation cleanup | Directly improves paths already used by mcremote (D11) |
| 1.18.16 | Ignore unknown config fields; project registration | Operator/startup tolerance |
| [1.18.17](https://github.com/anomalyco/opencode/releases/tag/v1.18.17) | Compaction quality, bounded retry with jitter, provider/model fixes | Direct engine behavior, no new wire |
| 1.18.18 | Kimi prompt and xAI xhigh fixes | Provider/model behavior only |
| [1.18.19](https://github.com/anomalyco/opencode/releases/tag/v1.18.19) | Cloudflare passthroughs, provider fixes, OpenCode Go web search, v1 DB compatibility | Engine-owned provider/tool availability |
| [1.18.20](https://github.com/anomalyco/opencode/releases/tag/v1.18.20) | Resumable subagent failures, provider/network retries, `run` child permissions | Subagent and retry behavior apply; direct `run` does not |
| [1.18.21](https://github.com/anomalyco/opencode/releases/tag/v1.18.21) | Continue unknown-finish responses; Vertex multi-region routing | Prevents premature engine completion; no wire change |

### Current mcremote support matrix

Legend: **in** = implemented · **pin** = required evidence/work for this
decision · **plan** = admitted by the scope amendment · **engine** =
automatically available inside OpenCode · **later** = separate product
decision · **out** = intentionally outside the control plane.

| Surface | OpenCode 1.18.21 | mcremote today | Decision |
| --- | --- | --- | --- |
| Loopback `serve`, health, global SSE | yes | in | add known-good pin |
| Create/resume/replay/prompt/abort/delete | yes | in | retain |
| Text/reasoning delta + snapshot reconciliation | unchanged Event set | in | retain |
| Tool start/update/terminal states | 14 built-ins + custom/MCP | in generically | map `apply_patch` as edit |
| Permissions v1/v2, child replies, auto mode | yes | in | retain |
| Questions v1/v2 | yes | in | retain |
| Session tree, status/idle/error, subagent cards | improved behavior | in | re-run live suite |
| Primary agents and build/plan/auto modes | yes | in | retain |
| Commands and skill-backed commands | yes | in through `GET /command` | retain |
| Todos, diff, rename, fork, revert/redo, compact, model | yes | in | retain existing routes |
| Auth catalog, credentials, device flow | yes | in | re-run read-only gates |
| Native root sessions and projects | yes | native-session interface exists; OpenCode does not implement it | plan discovery |
| Model input capabilities and variants | yes | decoder omits both; `ThinkingSession` deliberately unimplemented on the (now false) premise that OpenCode has no effort control | plan truthful capability gating plus variants mapped onto `ThinkingLevels`/`ThinkingSession` (A14) |
| Image/audio prompt file parts | yes | wire/model/display support both; composer picks images only; OpenCode drops non-text | plan bounded data URLs plus audio picker |
| Full replay/removal/artifacts/usage cost | yes | partial mapping and aggregate usage only | plan converged mapper and additive events |
| File/list/content and text/file search | yes | absent | plan session-scoped read-only browser |
| Aggregate VCS file status | works but `/vcs/status` is absent from official route table | existing Diagnostics compatibility call | retain and regression-pin; no new phone operation |
| File status and symbol search handlers | documented/OpenAPI, but return `[]` in 1.18.21 | absent | exclude until functional and reassessed |
| MCP tools and status | local/remote/OAuth | engine + generic stream + aggregate status | plan sanitized diagnostics; retain tool streaming |
| Dynamic MCP add | documented `POST /mcp`; transient instance state, no documented delete | absent | out by A7 lifecycle/security boundary |
| MCP connect/disconnect and OAuth routes | source/OpenAPI only; absent official prose route tables | absent | out under stable-only rule |
| Skills and references | local/global/Git; `GET /skill` is GET-only and absent from the official route table | engine; prompts can already author through normal write permissions | plan sanitized metadata, project-local authoring affordance, and guarded idle refresh; no raw editor |
| LSP and formatter state | yes | absent | plan sanitized diagnostics |
| Share/unshare | yes | absent | plan opt-in with disclosure |
| Direct session shell | yes | absent | plan opt-in remote-execution boundary |
| Plugins and `opencode plugin` | yes | engine config; optional `--pure` | no phone installer |
| v2 `/api` | large surface; wait/compact stubbed | one proven model route | no migration |
| ACP | capable protocol-1 subprocess | removed by 0019 | out |
| TUI/web/mini/replay, direct `run` | yes | no | out |
| Session/database/GitHub/PR CLI management | yes | no | out |
| mDNS/CORS/public server | yes | loopback fixed | out |

### Source and repository evidence

| Evidence | Fact established |
| --- | --- |
| `internal/provider/opencode/version.go:9-88` | Only the 1.18.0 minimum exists; no known-good release is recorded. |
| `internal/provider/opencode/http.go:579-645` | Fixed loopback serve argv, health version capture, minimum warning/gate, and engine version storage. |
| `internal/provider/opencode/http.go:37-69,647-717` | Stale seed/fallback IDs and asynchronous catalog refinement that now lands on big-pickle. |
| `internal/provider/opencode/http.go:1078-1381` | Exact SSE events mapped for messages, tools, permissions, questions, diff, commands, todos, lifecycle, status, idle, and errors. |
| `internal/provider/opencode/http.go:1497-1525` | Generic tool state mapping; `apply_patch` currently falls through to `other`. |
| `internal/provider/opencode/http.go:247-308` | The model decoder currently omits upstream capabilities and variants, so the phone cannot truthfully gate attachments or select a variant. |
| `internal/provider/opencode/http.go:939-1030` | Replay maps a subset of native parts and Prompt currently sends text only; image/audio content already accepted by the shared provider contract is dropped. |
| `internal/provider/opencode/usage.go:1-76` and `internal/event/event.go:338-342` | OpenCode token buckets are decoded, but the shared event exposes only used/context size and omits detailed buckets and cost. |
| `internal/provider/provider.go:342-508` | Diagnostics, semantic session operations, and provider-native discovery already use optional interfaces; OpenCode can add parity without making other providers implement it. |
| `internal/provider/httpagent/session.go` and `internal/provider/httpagent/provider.go` | One shared engine owns a registered session map; per-session turn/prompt/permission/question state exists, but no instance-wide refresh gate or forwarded `ConfigSession` exists. |
| `internal/daemon/daemon.go:216-236` and `internal/config/config.go` | OpenCode provider construction and daemon configuration are the required wiring points for disabled-by-default share/shell policy. |
| `internal/protocol/messages.go:345-357` and `apps/mobile/lib/features/chat/chat_screen.dart:587-620,931-950` | The wire accepts image/audio attachments, while the current composer invokes only image picking; audio composition requires explicit mobile work. |
| [Flutter `file_selector` 1.1.0](https://pub.dev/packages/file_selector) | A stable cross-platform file picker is available for the bounded audio-composition gap; dependency and exact frame-budget tests remain implementation work. |
| `internal/provider/httpagent/session.go:505-516,1418-1439`, `internal/provider/opencode/http.go:937-988`, and `internal/session/manager.go:160-169,803` | Resume invokes dialect replay, replay emits into the manager, and the manager currently appends each event to durable history; invoking replay at compaction would duplicate transcript content unless authoritative native-ID replacement semantics are added. |
| `internal/provider/httpagent/session.go:541-634` and `internal/provider/sessionutil/content.go:24-45` | The transport emits an ID-less optimistic user message before OpenCode submission, queues only content, and the OpenCode SSE mapper ignores user part snapshots; replay later has native user parts, so deterministic message identity must be assigned before enqueue/submission to prevent duplicate resume rows. |
| `internal/provider/opencode/session_ops.go` | Rename/diagnostics/fork/revert/unrevert/summarize/model/undo/diff routes and the historical v2 compact stub finding. |
| `internal/provider/opencode/command.go`, `mode.go`, `permission.go` | Live command catalog, primary-agent modes, synthetic auto, and v1/v2 permission normalization/reply paths. |
| `internal/provider/opencode/live_http_test.go:624-651` | Existing live test starts a real engine but does not assert the reported version. |
| Local OpenCode `ad0bb6d9a3` and ancestry checks | Package metadata is synchronized to 1.18.21; no local `v1.18.21` tag exists, and newer `dev` HEAD is not treated as the release. |
| Local `packages/sdk/openapi.json` plus live `GET /doc` | Exact counts/sets in D3 and interleaved-reasoning content diff in D4. |
| Local `packages/opencode/src/acp/*` plus NDJSON probe | ACP capability and session/config behavior in D10. |
| Local `packages/opencode/src/server/routes/instance/httpapi/handlers/file.ts` at `ad0bb6d9a3` | File list/content and text/file find are implemented; file status and symbol search are hard-coded empty arrays. |
| Local `packages/opencode/src/server/routes/instance/httpapi/groups/mcp.ts`, `packages/opencode/src/mcp/index.ts:641-646`, and official server/MCP docs | `POST /mcp` is documented and adds only transient instance config; local entries can spawn commands with environment and remote entries can carry URLs, headers, and OAuth client secrets. Connect/disconnect/OAuth routes are not in the official server table, and there is no documented dynamic delete. |
| Local `packages/opencode/src/command/index.ts:31,36-44` and live `GET /command` | `Command.hints` is `Schema.Array(Schema.String)` holding argument placeholders extracted from the template (`$1`, `$2`, `$ARGUMENTS`), not a display string. Built-in `init` and `review` both report `["$ARGUMENTS"]`; skill-backed commands report `[]`. `event.AvailableCommand.Hint` is a single string, so the PLAN must fix an exact join and clip. |
| Local `packages/core/src/v1/session.ts` `ToolState` union and 1.18.21 `ToolStateCompleted`/`ToolStateError` schemas | Only `ToolStateCompleted` carries `attachments: FilePart[]`. `ToolStateError` has `status`/`input`/`error`/`metadata`/`time` and **no** attachment field, so a failed tool state cannot produce an artifact on this release. `ToolStateCompleted.time.compacted` exists and is the compaction marker A3 reconciles against. |
| Live `GET /project` on an isolated 1.18.21 server | The list always contains a sentinel entry `{"id":"global","worktree":"/"}` in addition to real projects. It is a permanent engine entry, not an artifact of probing a non-Git directory, so project discovery rejects it by both `id == "global"` and the filesystem-root worktree. |
| Live `POST /session/{id}/shell` on an isolated 1.18.21 server | The completed tool part reports `tool: "bash"` — there is no distinct shell tool ID — with `state.output` equal to the command's stdout, `state.metadata.output` repeating it, `info.cost == 0`, and all `info.tokens` zero. `kindForTool("bash")` already returns `execute`, so no new tool-kind mapping is required. |
| Live `GET /mcp` schema union `MCPStatus{Connected,Disabled,Failed,NeedsAuth,NeedsClientRegistration}` | The exact upstream status vocabulary is `connected`, `disabled`, `failed`, `needs_auth`, and `needs_client_registration`; `failed` and `needs_client_registration` additionally carry a required `error` string that must never cross the phone protocol. |
| `internal/provider/opencode/commandtable.go:24-29` | The canonical `/thinking` command is pinned to `command.KindNone` with the note "OpenCode has no per-session thinking level — the model decides", citing MADR 0052 D6. This is the gate that makes `/thinking` unavailable on OpenCode; `internal/session/commands.go` `cmdThinking` is already provider-agnostic and needs no change once the table entry and `ThinkingSession` land. |
| `internal/provider/provider.go:61-68`, plus a grep of `internal/provider/httpagent` and `internal/provider/opencode` | `StartOptions.ThinkingLevel` is defined and persisted by the manager but consumed by neither the HTTP transport nor the OpenCode dialect, so create/resume precedence against the engine's own `Session.model.variant` is undefined today and must be settled explicitly rather than inherited. |
| `internal/provider/provider.go:388-392` and `internal/picker/picker.go:52-56` | Both assert that OpenCode exposes no per-session reasoning/thinking control. The 1.18.21 catalog disproves this: `Model.variants` advertises provider-authored effort rungs and `PromptInput.variant` accepts one per turn. A14 corrects both comments in the same change that implements the mapping. |
| Local `packages/opencode/src/session/schema.ts` and `session/prompt.ts:635-699,1498-1518` | Prompt accepts an optional caller-supplied message ID; the schema requires only a `msg` prefix, the exact ID is persisted, and optional input part IDs are accepted. This supports preassigned optimistic/native identity without an upstream API extension. |
| Local `packages/opencode/src/share/session.ts` | Upstream `share: disabled` rejects sharing; auto mode can produce existing native share state that mcremote must display without overriding. |
| Local `packages/opencode/src/session/prompt.ts` | Direct shell creates synthetic user and assistant tool parts and streams the completed tool state; its HTTP response must not be mapped a second time. |
| Local `packages/opencode/src/project/instance-store.ts` and `effect/instance-state.ts` | Instance state is keyed by normalized directory and disposal invalidates that cache; persisted sessions/messages survive recycle. |
| [Official server docs](https://opencode.ai/docs/server/) | `serve`, OpenAPI, HTTP/SSE, sessions/messages/files/LSP/formatter/MCP/agents are supported programmatic surfaces; the table includes dynamic `POST /mcp`, but lists `/vcs` rather than `/vcs/status`. |
| [Official CLI docs](https://opencode.ai/docs/cli/) | Current direct CLI commands, ACP NDJSON, plugin, database, serve, and run surfaces. |
| [Official model docs](https://opencode.ai/docs/models/) | Model selection and provider-defined variants are stable user functionality. |
| [Official agent docs](https://opencode.ai/docs/agents/) and [permission docs](https://opencode.ai/docs/permissions/) | Primary/subagent roles, tool permissions, and allow/ask/deny policy are stable and explain why direct shell requires a separate security boundary. |
| [Official command docs](https://opencode.ai/docs/commands/) | Custom commands, argument placeholders, shell injection, and file references justify retaining the native command path and exposing its hints instead of duplicating `/init`. |
| [Official ACP docs](https://opencode.ai/docs/acp/) | ACP uses JSON-RPC over stdio and supports tools, commands, MCP, rules, formatters, agents, and permissions; undo/redo remain exceptions. |
| [Official tools docs](https://opencode.ai/docs/tools/) | Built-ins, custom tools, MCP extension, permissions, `apply_patch`, and skills. |
| [Official MCP docs](https://opencode.ai/docs/mcp-servers/) | Local/remote servers, automatic OAuth, credential storage, auth/debug commands, and tool exposure. |
| [Official skills docs](https://opencode.ai/docs/skills/) and [references docs](https://opencode.ai/docs/references/) | On-demand `SKILL.md` discovery/permission rules and local/Git reference behavior. |
| [Official share docs](https://opencode.ai/docs/share/) | Manual/automatic/disabled sharing, public-link disclosure, synchronized transcript data, and unshare behavior justify the explicit opt-in mutation boundary and honest native-state display. |

### Unit-coverage baseline and exact deficits

On 2026-08-22, before implementation, default-tag atomic Go profiles and a
full Flutter test run with an out-of-tree LCOV path passed. Go counts came from
`go test -count=1 -covermode=atomic -coverprofile` per package; the Flutter run
passed 1,041 tests with 3 skipped. The table records exact profile counts and
the minimum additional covered statements needed to reach 80.0%. P0 repeats
these commands and commits a sanitized machine-readable baseline because a few
concurrent/platform branches can vary slightly between runs.

| Go package | Covered / total | Exact | Add for 80% | Add for P0A 82% |
| --- | ---: | ---: | ---: | ---: |
| `internal/provider` | 79 / 161 | 49.07% | 50 | 54 |
| `internal/provider/opencode` | 1,383 / 1,659 | 83.36% | 0 | 0; P1 requires 85% |
| `internal/provider/httpagent` | 737 / 1,470 | 50.14% | 439 | 469 |
| `internal/event` | 4 / 6 | 66.67% | 1 | 1 |
| `internal/picker` | 164 / 191 | 85.86% | 0 | 0 |
| `internal/protocol` | 11 / 25 | 44.00% | 9 | 10 |
| `internal/session` | 1,401 / 1,852 | 75.65% | 81 | 118 |
| `internal/ws` | 1,152 / 1,593 | 72.32% | 123 | 155 |
| `internal/config` | 483 / 615 | 78.54% | 9 | 22 |
| `internal/daemon` | 246 / 362 | 67.96% | 44 | 51 |
| `internal/cli/service` | 876 / 1,268 | 69.09% | 139 | 164 |

At the unchanged 1,659-statement denominator, the P1 OpenCode-specific 85.0%
floor requires 1,411 covered statements, 28 more than baseline. P1 computes the
ceiling again after production edits because newly added executable statements
change that denominator; 28 is the exact baseline debt, not permission to stop
at a stale count.

Flutter covered 9,061 / 11,789 production lines (76.8598%), leaving 371
additional covered lines to reach 80% in aggregate and 606 to reach P0A's 82%
headroom target. Four existing production files that this plan changes are
independently below the floor:

| Planned Dart file | Covered / total | Exact | Add for 80% | Add for P0A 82% |
| --- | ---: | ---: | ---: | ---: |
| `lib/data/ws/mcremote_client.dart` | 825 / 1,308 | 63.07% | 222 | 248 |
| `lib/features/chat/chat_screen.dart` | 913 / 1,447 | 63.10% | 245 | 274 |
| `lib/features/sessions/sessions_screen.dart` | 529 / 749 | 70.63% | 71 | 86 |
| `lib/data/protocol/models.dart` | 447 / 590 | 75.76% | 25 | 37 |

`go tool cover -func` and LCOV zero-line inspection locate the debt rather than
only quantifying it:

| Target | Concentrated current gaps that the PLAN names tests for |
| --- | --- |
| `internal/provider/opencode` | File credential fallback and active-upstream switching are 0%; question resync is 25%; subagent refresh is 38.5%; permission/todo resync, health-version branches, version comparisons, and diff truncation remain partial. |
| `internal/provider/httpagent` | Vendor/auth catalog reads, device-flow polling, most provider catalog/lifecycle methods, API helpers, and optional session-operation delegates are 0% or partial; this accounts for the largest Go deficit. |
| `internal/provider` | Owned-flow adapter methods, registry `All`/`ListWithAuth`, `GoalIsActive`, `ParseReviewArg`, and prewarm `Current`/edge paths are uncovered or partial. |
| `internal/event` / `internal/protocol` | `IsInPlaceUpdate`, `IsTerminalToolStatus`, `NegotiateVersion`, all three catalog-result builders, and `DecodePayload` boundaries are uncovered or partial. |
| `internal/session` | Command error/empty forms and manager delegation for history, modes/config, diagnostics/catalog, cancel, permission/question, revert/diff, close/delete, plus store error boundaries are the shortest path to its 81-statement deficit. |
| `internal/ws` | Owned device-flow lifecycle, create/prompt/history/release/claim and collaboration handlers, auth start/cancel/expiry, output writers, and async error/timeout paths contain the 123-statement deficit. |
| `internal/config`, `internal/daemon`, `internal/cli/service` | Path recomputation/TLS helpers; certificate/credential/event-hub wiring; and service control/refresh/setup error and platform matrices provide the required 9, 44, and 139 statements respectively. |
| Planned Dart files | Uncovered LCOV lines cluster in client operation wrappers and transport failure recovery; chat reconnect, attachment, queue, dialog and selector paths; sessions loading/mutation/navigation failures; and additive model parse/round-trip branches. |

Other legacy Dart files below 80% are outside this parity plan and are not
silently declared compliant. A13 gates the application aggregate plus every
production file this plan touches; any future plan that touches another
sub-floor file must close that file to 80% in its own change set.

### Environment-specific observations

The existing no-token `live_opencode` subset passed on this host against
1.18.21: auth catalog, scoped model catalog, session-tree minimum start,
primary-agent filtering, and `--pure`. It observed 193 auth upstreams,
17 configured upstreams, 200 default picker options, 9 connected providers,
and a live `big-pickle` default. Those figures are useful scale evidence but
are not release constants.

The isolated ACP session probe returned 991 model options while isolated
`GET /api/model` returned 1,039 models. The difference is directory,
provider, and catalog-state dependent; the pin asserts config-option shape and
successful mode/list/close behavior, not counts.

The direct `x-preview-f-free` read-tool turn proves that the newly listed
zero-cost model and normal tool execution are available. Its 24-second
single-sample elapsed time is not a benchmark and is not evidence to replace
the engine's `big-pickle` default.

The full token-bearing mcremote live suite was not run during MADR authoring.
That is intentionally an acceptance gate for the future PLAN, not an
unsubstantiated claim in this decision; it remains a PLAN acceptance gate.

After the owner corrected loopback permissions, two isolated 1.18.21 reruns
established the stable feature-route behavior used by the amendment:

* `GET /global/health` returned `{"healthy":true,"version":"1.18.21"}` and
  live `GET /doc` again reported 162 paths, 188 HTTP operations, and 472
  schemas. The live `/skill` path had only `GET`; no skill mutation operation
  existed.
* The empty Git project exposed seven agents, three commands, the built-in
  `customize-opencode` skill, ten provider-auth entries, and zero initial
  sessions, LSP servers, formatters, MCP servers, files, or status rows. The
  documented project, path, VCS, session, agent, command, skill, LSP,
  formatter, MCP, file, find, and provider-auth GET routes all returned 2xx.
  Before `git init`, the isolated non-Git directory was reported by
  `GET /project` with worktree `/`; project discovery must therefore reject
  that sentinel rather than offer the filesystem root as a selectable project.
* Source and loopback checks found no useful status/symbol implementation:
  `GET /file/status` and `GET /find/symbol` returned empty arrays, matching
  their hard-coded 1.18.21 handlers. `GET /vcs/status` returns useful data but
  is absent from the official server route table, so it remains only the
  provider's existing aggregate Diagnostics compatibility source rather than a
  newly admitted workspace operation. `GET /file/content` trims returned text
  and base64-encodes binary data, so the phone surface is a bounded viewer
  rather than a byte-for-byte file transport and must reject binary responses.
* The official server table exposes MCP status and dynamic `POST /mcp`; the
  generated group additionally contains connect/disconnect and OAuth routes.
  Exact source shows dynamic add mutates only instance memory and accepts local
  command/environment or remote URL/header/OAuth-secret configuration, with no
  documented dynamic delete. No MCP mutation was exercised or admitted.
* A valid project-local `.opencode/skills/loopback-probe/SKILL.md` written
  after the first `GET /skill` did not appear on a second GET, confirming the
  source-observed per-instance discovery cache. After documented
  `POST /instance/dispose` returned `true`, the skill appeared in both
  `GET /skill` and `GET /command`.
* A second idle-instance recycle preserved an existing session, its two
  messages, its text/tool parts, and the tool output `preserve-me` while making
  a newly written `recycle-probe` skill discoverable. This establishes the
  bounded refresh mechanism in A10 for an idle instance; implementation must
  still prevent disposal while any session in that worktree is running.
* A documented direct-shell probe created one root session and ran
  `printf mcremote-shell-ok`. The response contained one completed tool part
  with exact output `mcremote-shell-ok`, and the session reported cost 0. No
  model turn or external sharing occurred.

Both isolated probe directories were moved to the macOS Trash after their
servers stopped; no probe state remains under `/private/tmp`, and no loopback
probe listener remains running.

During the final document reassessment, a fresh isolated server start could
not bind in the managed execution sandbox: OpenCode returned `ServeError`, and
an independent `nc -l 127.0.0.1 49455` failed with `Operation not permitted`.
That direct control establishes a sandbox socket restriction rather than a
1.18.21 application regression. No new loopback behavior is claimed from that
attempt; the successful isolated probes above remain the runtime evidence, and
P0/P1 still require fresh live gates in an execution environment that permits
loopback binding.

### Independent reverification (2026-08-22, second pass)

The record above was re-audited against the local clone, the installed binary,
and a fresh isolated `opencode serve` on this host. The audit environment
permitted loopback bind (`net.Listen("tcp","127.0.0.1:0")` succeeded), so the
earlier sandbox denial did not recur and every runtime claim below was
re-executed rather than inherited.

| Reverified claim | Result |
| --- | --- |
| Binary identity | `opencode --version` = `1.18.21`; Mach-O 64-bit arm64; SHA-256 `8c78…f135` — unchanged |
| Repository boundary | `57fa34f235` = `fix(opencode): continue unknown finish responses (#43892)`; `ad0bb6d9a3` = `sync release versions for v1.18.21` with `packages/opencode/package.json` at `1.18.21`; ancestor of `dev` HEAD `ff3ef6e3e6`; no `v1.18.21` tag (only `v1.18.2`) |
| OpenAPI sets, source | `packages/sdk/openapi.json` at `ad0bb6d9a3` and at `v1.18.7` both give 162 paths / 188 operations / 472 schemas / 89 Event discriminators, with empty set diffs in all four |
| OpenAPI sets, runtime | Live `GET /doc` from the isolated 1.18.21 server gives the identical four sets — every set difference against the `ad0bb6d9a3` source is empty |
| OpenAPI content diff | `git diff v1.18.7 ad0bb6d9a3 -- packages/sdk/openapi.json` = 26 insertions / 6 deletions, confined to `ProviderConfig` and `Model` `interleaved`, replacing the `reasoning_details` enum member with `reasoning_text` and admitting free-form field names |
| Built-in tool IDs | 14, exactly as D7 lists, including `apply_patch` |
| Bootstrap defect | `deepseek-v4-flash-free` and `north-mini-code-free` are absent from the 62-model OpenCode Zen catalog; `big-pickle` is present, `default["opencode"] == "big-pickle"`, and its cost is zero on every axis |
| Agents | Seven: primary `build`/`plan`, hidden primary `compaction`/`summary`/`title`, subagents `explore`/`general` |
| Commands and skills | Built-in `init` and `review` (source `command`), built-in `customize-opencode` (source `skill`); `/skill` advertises `get` only |
| Empty stubs | `GET /file/status` and `GET /find/symbol` both return `[]` |
| v2 stubs | `POST /api/session/{id}/wait` and `POST /api/session/{id}/compact` both return HTTP 503 |
| Skill lifecycle | Post-discovery `SKILL.md` write invisible to a second `GET /skill`; `POST /instance/dispose` returned `true`; the skill then appeared in both `/skill` and `/command`; the pre-existing session and both of its messages, including the tool part and its exact output, survived |
| Direct shell | One root session, `printf mcremote-shell-ok` → one `completed` part with `tool: "bash"`, exact output, `cost: 0`, all token buckets zero, no model turn |
| mcremote code claims | `version.go`, `http.go` seed/fallback/health/`kindForTool`/`Prompt`/`Replay`, `live_http_test.go:624-651` circularity, `session_ops.go` `/vcs`+`/vcs/status`+`/mcp`+`/api/session/{id}/model`, `httpagent` ID-less optimistic user message and content-only prompt queue, `session/manager.go` append-only ring plus `store.SaveHistory` durability, and `apps/mobile` image-only composition all verified as described |

Probe state (isolated XDG config/data/cache/state plus a temporary Git
project) was deleted and the listener stopped after the run.

#### Corrections and additions from this pass

* **Route-table errata for `GET /skill`.** The official server route table
  documents Global, Project, Path/VCS (`/vcs`, not `/vcs/status`), Instance
  (`POST /instance/dispose`), Config, Provider, Sessions, Messages, Commands,
  Files, experimental Tools, `/lsp` `/formatter` `/mcp` (GET and dynamic POST),
  Agents, Logging, TUI, Auth, Events, and Docs. It has **no skills section**.
  `GET /skill` is therefore in the same documentary position as the
  grandfathered `/vcs/status`. A6 and A15 now state this explicitly instead of
  describing `/skill` as a documented server route.
* **`GET /project` sentinel — corrected during P0.** An intermediate draft of
  this section claimed the `{"id":"global","worktree":"/"}` entry is permanent.
  A controlled P0 probe disproved that and restored the original reading: a
  fresh engine lists only real Git projects, and the entry appears the moment
  any **non-Git** directory is opened. `GET /path` on such a directory resolves
  its worktree to `/`, which registers a project under the id `global`, and
  `GET /project` includes it from then on. The practical consequence is
  unchanged and the filter is still mandatory — any engine a real user has
  driven will have opened a non-Git directory at some point — but discovery
  rejects the entry on both the `global` id and the filesystem-root worktree,
  and the fixture records the exact two-step reproduction rather than asserting
  a standing entry.
* **Model capability gate cannot be demonstrated on the seeded default.**
  `opencode/big-pickle` reports `attachment: false`,
  `input: {text:true, audio:false, image:false, video:false, pdf:false}`, and
  `variants: {}`. A truthful capability/variant gate is therefore invisible on
  the default seed. In the same catalog snapshot
  `opencode/muse-spark-1.2-contributor-free` was zero-cost with image input,
  audio input, and five variants (`minimal`, `low`, `medium`, `high`, `xhigh`),
  and `opencode/mimo-v2.5-free` was zero-cost with image and audio and no
  variants. These are catalog observations, not release constants, and the PLAN
  names them only as preferred live-gate candidates with a documented fallback.
* **Variants are reasoning effort.** Every observed variant body is a reasoning
  configuration: `{"reasoningEffort":"…"}` (optionally with
  `reasoningSummary`/`include`) on OpenCode Zen and xAI-family models, and
  `{"thinkingConfig":{"includeThoughts":true,"thinkingLevel":"…"}}` on Google
  models. 528 models across the connected providers advertised at least one
  variant. This is the basis for A14.
* **`--pure` does not isolate skills.** A `--pure` server with fully isolated
  XDG config/data/cache/state still loaded a skill from `~/.claude/skills`,
  because the Claude- and agent-compatible skill roots are absolute home paths
  rather than XDG paths. Any probe intended to enumerate only built-ins must
  additionally neutralize those roots; a probe that does not is measuring the
  host, not the release.
* **The P0 corpus guard was split (owner-approved during P0 execution).** The
  original single `rg` forbade the literal `/experimental/` anywhere under
  `internal/provider/opencode/testdata/surface-1.18.21`, which cannot hold
  alongside committing the canonical OpenAPI sets: 21 of the 162 paths and 25 of
  the 188 operations on 1.18.21 are `/experimental/*`, and omitting them would
  falsify the very counts D3 asserts. The guard is now two checks — the corpus
  carries no `authorization`, `api_key`, `access_token`, or `/Users/` material,
  and no Go file under `internal/provider/opencode` references
  `/experimental/`. Both pass. A11's intent is unchanged: experimental output is
  non-contractual research and no runtime or test helper calls those endpoints.
* **Two probe-isolation defects were found and corrected before any fixture was
  written.** `--pure` plus fully isolated XDG roots still loaded a skill from
  `~/.claude/skills`, because the Claude- and agent-compatible skill roots are
  absolute home paths; redirecting `HOME` yields the true built-in set of one
  skill, three commands, and seven agents. Separately, `BASH_ENV` set in the
  operator's shell is inherited by the engine and sourced by its `bash` tool, so
  the first shell capture contained host rc errors instead of the command's
  output. The corpus is captured with `HOME` redirected and `BASH_ENV`/`ENV`
  unset, and `manifest.json` records both requirements.
* **Coverage baselines re-measured.** A fresh default-tag run reproduced
  `internal/provider/opencode` at exactly 1,383 / 1,659, `internal/provider` at
  79 / 161, `internal/provider/httpagent` at 737 / 1,470, `internal/event` at
  4 / 6, `internal/picker` at 164 / 191, `internal/protocol` at 11 / 25,
  `internal/config` at 483 / 615, and `internal/cli/service` at 876 / 1,268,
  and the Flutter application at exactly 9,061 / 11,789 lines (76.8598%) over
  1,041 passing tests with 3 skipped, including all four planned per-file
  counts. Three packages moved between runs: `internal/daemon` 246→247,
  `internal/session` 1,401→1,404, and `internal/ws` 1,152→1,142. Every
  "add for 80%" and "add for 82%" figure in the tables above recomputes
  correctly from the recorded counts. The observed drift is why P0 commits a
  freshly captured baseline rather than these prose numbers, and why the
  non-regression gate compares captured profiles rather than this table.

### P0 execution record (2026-08-22)

P0 froze the evidence contract. The corpus lives at
`internal/provider/opencode/testdata/surface-1.18.21/` and is validated on every
default-tag run by `internal/provider/opencode/surface_contract_test.go`.

Exact probe commands, with the isolation P0 established as mandatory:

```bash
env -u BASH_ENV -u ENV \
  HOME="$PROBE/home" \
  XDG_CONFIG_HOME="$PROBE/cfg" XDG_DATA_HOME="$PROBE/data" \
  XDG_CACHE_HOME="$PROBE/cache" XDG_STATE_HOME="$PROBE/state" \
  OPENCODE_DISABLE_AUTOUPDATE=1 \
  opencode serve --hostname 127.0.0.1 --port "$PORT" --pure
curl -s "http://127.0.0.1:$PORT/global/health"
curl -s "http://127.0.0.1:$PORT/doc"
curl -s "http://127.0.0.1:$PORT/<route>?directory=$(urlencode "$PROBE/proj")"
```

| P0 result | Value |
| --- | --- |
| Health | `{"healthy":true,"version":"1.18.21"}` |
| Live `/doc` versus source `ad0bb6d9a3` | 162 / 188 / 472 / 89, all four set differences empty; the corpus generator hard-fails otherwise |
| Built-in agents | 7: primary `build`/`plan`, hidden primary `compaction`/`summary`/`title`, subagents `explore`/`general` |
| Built-in commands | 3: `init` and `review` (source `command`, hints `["$ARGUMENTS"]`), `customize-opencode` (source `skill`, hints `[]`) |
| Built-in skills | 1: `customize-opencode`; `/skill` advertises `get` only |
| Empty stubs | `GET /file/status` → `[]`, `GET /find/symbol` → `[]` |
| Workspace reads | `/file` returns a strippable `absolute` field and marks directories with a trailing `/`; `/file/content` returns trimmed text; `/find` returns `lines.text` including its newline; `/find/file` returns a plain string array |
| Shell | one `completed` part, `tool: "bash"`, output exactly `mcremote-shell-probe`, `cost: 0`, all token buckets 0 |
| Shell SSE order | `session.status(busy)` → `message.updated(user)` → `message.part.updated(text, synthetic)` → `message.updated(assistant)` → `message.part.updated(tool bash, running)` ×4 → `message.updated(assistant)` → `message.part.updated(tool bash, completed)` → `session.status(idle)` → `session.idle`; no `message.part.delta` on this path |
| Skill lifecycle | write → cached `GET /skill` unchanged → `POST /instance/dispose` → `true` → skill visible in `/skill` **and** `/command`; session, both messages, the tool part and its output all preserved |
| Live gates | `TestLiveLoopbackBindPreflight` and `TestLiveVersionAndStableSurface` pass: `cli=1.18.21 health=1.18.21 min=1.18.0 known-good=1.18.21` |

Two corrections were forced by P0 and are recorded rather than quietly applied:

* **The `global` project sentinel is created on demand, not permanent.** A
  controlled probe showed a fresh engine listing only real Git projects, with
  `{"id":"global","worktree":"/"}` appearing only after a non-Git directory was
  opened. This reverses an incorrect intermediate claim in this record and
  restores the original reading; the filter itself is unchanged and still
  mandatory.
* **The P0 corpus guard was split** as described above, because the canonical
  path and operation sets necessarily contain `/experimental/*` entries. The
  runtime half of the guard additionally excludes
  `surface_contract_test.go`, which implements the equivalent Go check and so
  contains the literal it searches for.

The coverage tooling added in P0 (`scripts/coverage-snapshot.sh`,
`scripts/coverage-delta.sh`, `scripts/coverage-delta_test.sh` and
`scripts/testdata/coverage/`) is fixture-driven and proves exact-integer
threshold behavior: 79.999% is rejected and exactly 80.0% accepted at two
scales, the 82.0% P0A target and 85.0% OpenCode floor are distinguished from the
general floor, uncovered-count and exact-fraction regressions are separate
findings that are reported together when both apply, malformed and missing
profiles are rejected rather than read as zero, an LCOV path that is a suffix of
another resolves exactly, a requested Dart file absent from LCOV is an error,
and a gate invoked with no target at all is a usage error rather than a pass.
44 fixture cases pass and all three scripts are shellcheck-clean.

### Related

* [0019-MADR-opencode-process-management-plan.md](./0019-MADR-opencode-process-management-plan.md)
* [0020-MADR-opencode-session-tree.md](./0020-MADR-opencode-session-tree.md)
* [0021-MADR-opencode-http-api-coverage.md](./0021-MADR-opencode-http-api-coverage.md)
* [0022-MADR-plan-mode-parity.md](./0022-MADR-plan-mode-parity.md)
* [0023-MADR-canonical-slash-commands.md](./0023-MADR-canonical-slash-commands.md)
* [0031-MADR-opencode-catalog-and-metadata-parity.md](./0031-MADR-opencode-catalog-and-metadata-parity.md)
* [0034-MADR-opencode-tool-stream-fidelity.md](./0034-MADR-opencode-tool-stream-fidelity.md)
* [0037-MADR-cli-capability-uptake.md](./0037-MADR-cli-capability-uptake.md)
* [0044-MADR-auto-approve-modes.md](./0044-MADR-auto-approve-modes.md)
* [0089-MADR-long-running-session-stability.md](./0089-MADR-long-running-session-stability.md)
