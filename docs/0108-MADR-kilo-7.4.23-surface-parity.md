---
status: accepted
date: 2026-08-20
decision-makers: Project Owner (scope and acceptance); Implementer (probe)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Pin Kilo known-good to 7.4.23 after wire and behavior compatibility verification

## Context and Problem Statement

[0088](./0088-MADR-kilo-7.4.22-surface-parity.md) pinned the Kilo
provider to **7.4.22** and closed the session-loop gaps found in that
release. The installed Homebrew package is now **7.4.23**:
`/opt/homebrew/bin/kilo` resolves to
`/opt/homebrew/lib/node_modules/@kilocode/cli/bin/kilo`, the installed
package metadata reports `7.4.23`, and an isolated-state
`kilo --version` reports `7.4.23`. The GitHub release was published on
2026-08-20 at 12:26 UTC from tag commit
[`40fa10e`](https://github.com/Kilo-Org/kilocode/commit/40fa10e50a75c4887978d892520d1246515413bf).

The provider still declares `KnownGoodVersion = "7.4.22"` in
`internal/provider/kilo/version.go:9-15`. Its health hook records the
engine version and warns whenever it differs from that constant
(`internal/provider/kilo/version.go:17-45`). A 7.4.23 host therefore
produces a drift warning even if the provider is compatible.

The original 0108 draft correctly found one removed OpenAPI path, but
it treated the entire release as that one path removal. Source review
found additional engine behavior changes, most importantly hardened
Ask/Plan permission precedence. It also found four incorrect delta
claims in the draft: the `session.next.prompt.*` events and `invalid`
tool predate 7.4.23, the Event union has 119 types in both releases,
and only one OpenAPI schema was removed.

This record asks: **is Kilo 7.4.23 compatible with mcremote's HTTP/SSE
session loop, which 7.4.23 behaviors are part of the pin, which surfaces
remain deferred or rejected, and what deterministic evidence is required
before the known-good constant moves?**

Architectural scope is `internal/provider/kilo`, its `httpagent` host,
the command/mode/model catalogs, permission handoffs, live Kilo tests,
and versioned probe fixtures. Kilo Cloud, Agent Manager IDE chrome,
`kilo run` as a control plane, worktrees, PTY, and sandbox adoption
remain outside the provider boundary established by 0075 and 0088.

## Decision Drivers

* The known-good pin must identify a release exercised by deterministic
  compatibility gates, not merely the newest installed version.
* The phone session loop remains the product: prompt, stream, tools,
  permissions, modes, slash commands, resume, cancel, and delete.
* Protocol-schema invariance and engine behavior invariance are different
  claims and require different evidence.
* Wire claims must be reproducible from saved fixtures or live-tagged tests,
  as required by `AGENTS.md`.
* Environment-dependent observations such as authenticated model catalogs,
  project commands, and available skills must not be presented as release
  constants.
* Read-only modes must remain read-only even when daemon auto-approval is
  armed; an engine-side deny must not be represented as a permission the
  phone can override.
* The release tag and package changelog are the release intent; the shipped
  `/doc`, `/global/health`, `/agent`, `/command`, and ACP initialize results
  are the runtime contract.

## Considered Options

* Option 1: Pin 7.4.23 after persisting the wire delta and deterministic
  behavior gates (chosen)
* Option 2: Keep the 7.4.22 pin and treat 7.4.23 as unverified best effort
* Option 3: Wait for 7.5.x before moving the pin

## Decision Outcome

Chosen option: **"Option 1: Pin 7.4.23 after persisting the wire delta
and deterministic behavior gates"**, because the 7.4.23 HTTP Event
union is identical to 7.4.22, the one removed endpoint was never called
by mcremote, and the behavior changes that can affect phone sessions are
compatible with the existing provider boundary. The pin is not just a
constant change: it also requires reproducible fixtures, an exact-version
live gate, and a no-model live check for the new Ask/Plan permission
boundary. No session decoder or event filter changes are justified.

* Implementation Plan:
  [0108-PLAN-kilo-7.4.23-surface-parity.md](./0108-PLAN-kilo-7.4.23-surface-parity.md)

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | Set `KnownGoodVersion = "7.4.23"`; update its health-comment example and add exact-version unit/live assertions. Append errata to 0075 and 0088 instead of rewriting their historical rationale. Keep all older spike directories. |
| **D2** | Treat the OpenAPI delta as exactly one removed path and one removed schema: `/kilocode/agent/requirements` and `AgentRequirementResult`. The release-source and reported live counts are 255 paths and 680 schemas versus 256 and 681 in 7.4.22. `AgentRequirementError` was not a 7.4.22 schema and is not part of this delta. |
| **D3** | Treat the HTTP Event union as unchanged. Resolving each of the 210 refs in `components.schemas.Event.anyOf` and reading only the referenced schema's top-level `properties.type.enum` yields **119 unique event types in both 7.4.22 and 7.4.23**, with an empty set diff. The 119 recorded in 0088 was correct. Recursive descent is not canonical: it falsely adds the nested memory subtypes `recalled`, `saved`, and `skipped`, producing 122; the original 0108 draft's broader scan produced 123. All current decode/filter logic carries forward unchanged. |
| **D4** | The HTTP Event union already contains `session.next.prompted` and `session.next.prompt.admitted` in 7.4.22. Durable `.1` schemas and a legacy `session.next.prompt.promoted` schema also occur elsewhere in both OpenAPI documents; Kilo source says promotion now emits `session.next.prompted`, so `promoted` is not a new emitted Event-union member. PR #13176 changed VS Code queue visibility, not the server event vocabulary. Continue the 0088 rule: ignore/deduplicate `session.next.*` until a live turn proves the primary `message.part.*` stream is absent; do not add a second transcript path for this pin. |
| **D5** | Accept 7.4.23's Ask/Plan permission hardening (#13124) as part of the pin. Under a controlled global `* = allow`, Ask still denies arbitrary bash, write, edit, task, and interactive-terminal actions. Plan still denies arbitrary bash/write and interactive terminal, permits edits only to plan-file patterns, and preserves delegation while denying the `general` task agent. Code remains fully usable. mcremote sends the selected agent on every `prompt_async` (`internal/provider/kilo/session.go:210-245`), while synthetic auto mode selects `code` rather than an `auto` agent (`internal/provider/kilo/mode.go`). Therefore no dialect change is needed, but a controlled `kilo debug agent` live test must pin the boundary. |
| **D6** | Accept the Ask→Code selected-agent/model fixes (#13121/#13112/#13142) and incremental text restoration (#13168) as engine-side fixes. mcremote already selects agents per prompt and consumes `message.part.delta`; the schemas did not change. Existing mode, prompt-stream, and tool-stream tests remain the confirmation. |
| **D7** | The removed requirements route was Agent Manager/experimental configuration surface and was never referenced by `internal/provider/kilo`. Its removal needs no fallback. Do not add a version-gated request or retain the removed `requirements` agent field. |
| **D8** | The `invalid` tool is not new in 7.4.23: `packages/opencode/src/tool/invalid.ts` and registry wiring exist at the 7.4.22 tag, and tool booleans vary with effective permissions. It remains an engine-internal recovery tool; do not add a `kindForTool` mapping or claim an 18-vs-17 release delta. |
| **D9** | Sandbox Git escalation (#13178) remains out of scope with sandbox/PTY/worktree control. Kilo marks such approval requests `metadata.sandboxEscalation=true` and refuses non-interactive approval. mcremote's reply currently omits `interactive` (`internal/provider/kilo/permission.go:373-388`). This is safe while the provider never enables sandbox, but any future sandbox adoption must distinguish a human phone reply (`interactive:true`) from daemon auto-approval (omitted/false) and requires a separate MADR/PLAN. |
| **D10** | ACP remains unused. The 7.4.22→7.4.23 source diff changes ACP permission presentation for sandbox escalation but not `packages/opencode/src/acp/service.ts` initialize capabilities. HTTP `serve` remains the primary transport under 0075 D5/0088 D11. |
| **D11** | Persist a 7.4.23 evidence corpus containing the sorted OpenAPI paths, canonical Event type list, OpenAPI summary, agent identity summary, controlled Ask/Plan permission summary, built-in command snapshot, ACP initialize response, and README. Never commit `/provider`, credentials, raw logs, absolute home-state paths, or the multi-megabyte raw `/doc`. |
| **D12** | Other release items remain engine-internal or outside the control plane: `kilo pr` commands (#13137), Agent Manager/IDE/webview changes, task-aware output-pruner removal (#13214), skill-description cleanup (#13210), state-directory fallback (#13115), blocking-shell wait handling (#13224), model/reasoning catalog fixes, memory/snapshot changes, STT, and Gateway 8.0.0. They may improve engine behavior but require no new mcremote endpoint or decoder for this pin. |

### Required work

1. Move the constant/comment and the two version-specific unit tests to
   7.4.23.
2. Add the complete, sanitized 7.4.23 fixture corpus from D11 and prove its
   path/Event diffs against 7.4.22.
3. Add no-model `live_kilo` gates that start the installed CLI in isolated
   state, assert health/OpenAPI/version invariants, and assert controlled
   Ask/Plan permission boundaries.
4. Append 0075/0088 errata for the moved pin and clarify why 0088's Event
   count was correct; keep the original decisions visibly historical.
5. Run the package race suite and, on a host with a usable model, the existing
   prompt, permission, resume, cancel, tool-stream, and catalog live suite.

Explicitly not required: new event decoding, `session.next.prompt.*`
rendering, a tool mapping for `invalid`, ACP adoption, sandbox escalation,
worktrees, PTY, `kilo pr`, Agent Manager, Cloud, or `kilo run` hosting.

## Consequences

* Good, because the known-good warning matches the installed and verified
  release rather than reporting expected operation as drift.
* Good, because protocol parity is proved by an exact Event-type set diff,
  not inferred from path/schema counts.
* Good, because the release's Ask/Plan security boundary becomes an explicit
  compatibility contract and cannot be accidentally described as IDE-only.
* Good, because no-model live gates are deterministic and do not depend on a
  model choosing to call a tool.
* Good, because environment-specific catalogs, skills, and commands remain
  observations rather than false release invariants.
* Bad, because the fixture and live-gate scope is larger than a two-line
  version bump.
* Bad, because a 7.4.22 host will warn after the pin moves. That is the
  intended skew signal already accepted by 0088.
* Neutral, because the removed requirements endpoint and most release UI
  changes were never reachable from the dialect.

## Pros and Cons of the Options

### Option 1: Pin 7.4.23 after deterministic compatibility gates (Chosen)

* Good, because the pin matches the release actually installed and driven.
* Good, because the plan covers both unchanged wire shapes and changed engine
  semantics.
* Good, because failures can be attributed to exact fixtures and live probes.
* Bad, because it adds a live surface test and a larger evidence corpus.
* Neutral, because the runtime dialect remains unchanged apart from the pin.

### Option 2: Keep the 7.4.22 pin

* Good, because no code or fixture changes are needed.
* Bad, because every healthy 7.4.23 boot reports drift and the support claim
  no longer names the running engine.
* Bad, because it leaves a security-relevant Ask/Plan behavior change
  undocumented.

### Option 3: Wait for 7.5.x

* Good, because it follows 0088's original re-probe cadence literally.
* Bad, because cadence was a heuristic, not a compatibility requirement.
* Bad, because postponement preserves noisy warnings despite an unchanged
  Event contract and a bounded, testable behavior delta.
* Neutral, because the future `session_tree` minimum-version gate remains a
  separate decision under 0075 PD2.

## Confirmation

* `KnownGoodVersion == "7.4.23"`, and the 7.4.23 health body records the
  same version without a drift warning.
* The committed 7.4.23 OpenAPI summary reports 255 paths, 680 schemas,
  and 119 canonical Event types.
* Path diff against 7.4.22 is exactly removed
  `/kilocode/agent/requirements`; schema diff is exactly removed
  `AgentRequirementResult`; Event-type diff is empty.
* A no-model live test asserts the installed `kilo` and `/global/health`
  versions, `/doc` counts, required session-loop events, and removed route.
* A controlled no-model `kilo debug agent` test proves broad top-level allow
  leaves Ask and Plan mutation boundaries denied while Code remains usable.
* `go test -race ./internal/provider/kilo/` passes, including 0087/0088
  chrome, durable-envelope, permission, mode, command, and pin-scope tests.
* `make pre-add-check FILES="internal/provider/kilo/version.go internal/provider/kilo/dialect_test.go internal/provider/kilo/live_surface_test.go"`
  passes before staging the Go files.
* On a host with a usable model, `make live-kilo` completes the token-bearing
  prompt/tool/permission checks. A model declining a requested tool call is
  recorded as a skip and is not misreported as proof of permission behavior.
* The support matrix is re-probed before a later pin moves; version number,
  not major/minor label, determines the need for re-probe.

## More Information

### Evidence ledger

| Evidence | Fact established |
| --- | --- |
| `internal/provider/kilo/version.go:9-45` | Current pin is 7.4.22; health mismatch logs a warning but does not reject boot. |
| `internal/provider/kilo/dialect_test.go:172-188` | The current version-specific tests assert 7.4.22 and must move with D1. |
| `internal/provider/kilo/session.go:210-245` | Every normal prompt carries the selected Kilo agent in `prompt_async`. |
| `internal/provider/kilo/mode.go` | Phone modes are Kilo agents; synthetic `auto` runs the normal `code` agent and daemon-side permission auto-response. |
| `internal/provider/kilo/permission.go:18-90,373-388` | Permission metadata is shown as detail when needed, while engine replies omit Kilo's optional `interactive` flag. |
| `internal/provider/kilo/pin_scope_test.go` | Worktree, sandbox, PTY, Agent Manager, Cloud, and run control are intentionally absent. |
| `/Users/saxsmith/gitrepos/kilocode`, `v7.4.22..0d5d334480` | Local source delta used for 7.4.23 behavior review. GitHub tag commit `40fa10e` has parent `0d5d334480`; its remaining changes are release metadata/version generation. |
| Local Kilo checkout HEAD `67cd629f3c` | The local `main` is not the published tag boundary: after `0d5d334480` it includes the VS Code document-viewer series ending at merge `6f87e9c22e` and a different local `release: v7.4.23` commit. All shipped comparisons therefore use the pinned tag parent, not mutable `HEAD`. |
| Kilo `packages/sdk/openapi.json` at `v7.4.22` and `0d5d334480` | 256/681/119 → 255/680/119; one path removed, one schema removed, no Event types changed. |
| Kilo `packages/opencode/src/kilocode/agent/index.ts` and commit [`d4f3a3a`](https://github.com/Kilo-Org/kilocode/commit/d4f3a3a9e63a3954214887563dc3816ea179858f) | Ask/Plan reapply read-only guards after broad user permission rules. |
| Kilo `packages/opencode/src/permission/index.ts` | `sandboxEscalation` joins `skillShell` as a human-interactive-only approval. |
| Kilo `packages/opencode/src/tool/invalid.ts` and `tool/registry.ts` at `v7.4.22` | The `invalid` recovery tool predates 7.4.23. |
| Kilo `packages/opencode/src/acp/service.ts` at both boundaries | ACP initialize capability declarations are unchanged. |
| [Kilo 7.4.23 release](https://github.com/Kilo-Org/kilocode/releases/tag/v7.4.23) and [CLI changelog](https://github.com/Kilo-Org/kilocode/blob/v7.4.23/packages/opencode/CHANGELOG.md#L3) | Official release intent and package-specific CLI changes. |
| [Kilo CLI permissions](https://kilo.ai/docs/code-with-ai/platforms/cli#permissions) and [agent permissions](https://kilo.ai/docs/customize/agent-permissions) | Permissions are ordered allow/ask/deny rules; built-in read-only agents add stronger restrictions. |

### Corrections made during this assessment

| Original draft claim | Verified finding |
| --- | --- |
| `session.next.prompt.*` is new in 7.4.23 | The two Event-union prompt types and three related durable/legacy schemas are present in both release OpenAPI documents; #13176 changed a VS Code consumer. |
| Event values changed from 119 to 123 | Top-level discriminator extraction yields 119 unique types in both; the set diff is empty. Recursive scanning produces false event values from nested schemas. |
| `invalid:true` is a new eighteenth Code tool | The tool and registry wiring exist at `v7.4.22`; its boolean is permission/config dependent. |
| Two requirement schemas were removed | Only `AgentRequirementResult` is removed between 7.4.22 and 7.4.23. |
| Release delta is one path plus IDE polish | The wire delta is one path/schema, but CLI behavior also changes Ask/Plan permissions, mode transitions, shell waits, state fallback, tool prompting, and model handling. |

### Canonical OpenAPI comparison

The canonical Event count resolves every `$ref` in
`components.schemas.Event.anyOf` and extracts only the referenced schema's
top-level `properties.type.enum` values before sorting and deduplicating.
There are 210 refs and every ref has exactly one top-level discriminator;
deduplication yields 119 event types. Recursive scanning is wrong because
three memory-event schemas contain nested objects whose own `type` enums are
`recalled`, `saved`, and `skipped`. Running the top-level extraction over the
two release-source OpenAPI files produces:

| Item | 7.4.22 | 7.4.23 | Delta |
| --- | ---: | ---: | --- |
| Paths | 256 | 255 | −1 |
| Schemas | 681 | 680 | −1 |
| Unique Event types | 119 | 119 | 0 |

Removed path: `/kilocode/agent/requirements`.

Removed schema: `AgentRequirementResult`.

The Event set in both releases includes every type consumed by the dialect:
`message.part.delta`, `message.part.updated`, `message.part.removed`,
`message.updated`, `session.status`, `session.idle`,
`session.turn.open`, `session.turn.close`, `session.diff`,
`session.error`, permission v1/v2, question v1/v2, and the existing
`session.next.*` family.

### 7.4.23 behavior delta relevant to the provider

| Change | Source fact | Decision |
| --- | --- | --- |
| Ask/Plan broad-permission hardening (#13124) | Catch-all/global allows cannot widen guarded mutating tools; per-agent permission is the explicit opt-in. | Accept and live-pin (D5). |
| Ask→Code and Plan→Code fixes (#13121/#13112/#13142) | Selected agent/model is retained or deliberately changed when leaving a read-only/planning flow. | Accept; current per-prompt agent contract fits (D6). |
| Incremental text restore (#13168) | Restores streaming in Kilo clients; `message.part.delta` remains in the unchanged Event union. | Accept; existing stream test confirms (D6). |
| Queued prompt visibility (#13176) | Changed VS Code session-parts handling; server prompt event types predate this release. | No new decoder (D4). |
| Agent requirements removal (#13225) | Removes route, schema, field, and guards. | No-op for mcremote (D7). |
| Sandbox Git escalation (#13178) | Adds human-only `sandboxEscalation` permission handling. | Deferred with explicit future constraint (D9). |
| Task-aware output pruning removal (#13214) | Removes engine-side tool schema/output transformation. | No wire handler; verify tool stream remains healthy (D12). |
| Blocking shell wait handling (#13224) | Keeps one-shot waits in the blocking shell tool. | No schema change; existing tool-stream live test. |
| State-directory fallback (#13115) | Falls back when the default state directory is unwritable unless an explicit `XDG_STATE_HOME` disables fallback. | Engine startup improvement; no provider change. |
| `kilo pr` command changes (#13137) | Changes direct CLI subcommands, not `GET /command` slash workflows. | Out of control plane (D12). |

### ACP

`packages/opencode/src/acp/service.ts` constructs protocol version 1 with
`loadSession`, HTTP/SSE MCP, embedded-context/image prompts, and
close/fork/list/resume session capabilities. The file has no diff between the
pinned 7.4.22 and 7.4.23 source boundaries. ACP permission presentation does
change elsewhere for sandbox escalation. The implementation plan therefore
repeats initialize over NDJSON, but because mcremote uses `kilo serve`, no ACP
adoption follows from this release.

### Support matrix — mcremote vs Kilo 7.4.23

Legend: **in** = dialect implements it · **pin** = required evidence for
this pin · **later** = real surface deferred to another decision · **out** =
rejected as control plane or IDE-only.

| Surface | 7.4.23 engine | Dialect today | Pin |
| --- | --- | --- | --- |
| `kilo serve` loopback + `?directory=` | yes | in | in |
| Health + version | 7.4.23 | pin 7.4.22 | **D1 bump** |
| `prompt_async` + abort + delete | yes | in | in |
| `message.part.delta/updated` text, reasoning, tools | unchanged | in | in |
| Transient chrome and durable `data` envelopes | unchanged | in (0087) | in |
| `session.status` / `idle` / `turn.*` / `diff` / `error` | unchanged | in | in |
| `session.next.synthetic/text/tool/shell/step/prompt.*` | unchanged | ignored/deduped | later (0088 D3, D4) |
| Permission asked/v2 + once/always/reject | unchanged wire | in | in |
| Ask/Plan read-only guard under global allow | hardened | engine-enforced | **D5 pin** |
| Sandbox escalation human-only reply | new behavior | sandbox never enabled; reply lacks `interactive` | later (D9) |
| Questions | unchanged | in | in |
| Primary agents + synthetic auto | same agent set | in | in |
| Ask→Code / Plan→Code transitions | fixed | per-prompt agent | in |
| Built-in `/init` `/review` `/resume-claude` `/resume-codex` | present | in | in |
| Fork / revert / compact / model / undo / redo / diff | present | in via existing routes | in |
| `invalid` recovery tool | pre-existing | no special kind | in/no-op |
| Skills list/tool and skill-shell approval | present | engine-only; no interactive marker | later |
| Agent requirements endpoint | removed | never used | out (D7) |
| Worktrees / sandbox / PTY | present | no | later |
| ACP `kilo acp` | v1, unchanged capabilities | unused | out (D10) |
| `kilo pr`, Cloud, remote, `kilo run`, Agent Manager, STT | present | no | out |

### Environment-specific observations

On the probe host, Gateway OAuth selected `kilo/kilo-auto/balanced`, the
agent set contained five visible primary agents plus hidden/subagents, and
`GET /command` also included project/MCP/skill-backed commands. Those are
useful live observations, not 7.4.23 release constants. Only the built-in
agent classifications and built-in command subset are acceptance assertions.

The current restricted execution environment could run isolated-state
`kilo --version` and `kilo debug agent`, but `kilo serve` exited with
`ServeError` after failing to start its macOS FSEvents stream. The implementation
plan therefore requires the health/OpenAPI probe and no-model live surface
test in the normal host environment before D1 is committed; this review does
not convert the sandbox failure into a product claim.

A subsequent 2026-08-20 retry in an unrestricted host environment succeeded.
The isolated server selected `127.0.0.1:4096` for `--port 0`, reported health
version 7.4.23, and exposed 255 paths, 680 schemas, 210 Event refs, and 119
unique top-level Event types. Its path, schema, and Event-type sets exactly
matched source boundary `0d5d334480`, and it stopped cleanly. The exact ACP
initialize request in the associated plan also returned protocol 1, the
documented capabilities, agent version 7.4.23, and exit 0. Kilo emitted
one-time migration chatter before the ACP JSON-RPC line; the plan now makes
that mixed-output behavior part of the deterministic parser contract.

### Related

* [0088-MADR-kilo-7.4.22-surface-parity.md](./0088-MADR-kilo-7.4.22-surface-parity.md)
* [0087-MADR-kilo-session-chrome-and-permission-decode.md](./0087-MADR-kilo-session-chrome-and-permission-decode.md)
* [0075-MADR-kilo-cli-provider.md](./0075-MADR-kilo-cli-provider.md)
* [0076-MADR-kilo-debug-pass.md](./0076-MADR-kilo-debug-pass.md)
* [0096-MADR-kilo-model-catalog-scope-and-order.md](./0096-MADR-kilo-model-catalog-scope-and-order.md)
* Spike 7.4.22: [docs/kilo-spike-7.4.22/](./kilo-spike-7.4.22/)
