---
status: approved
date: 2026-08-22
decision: 0112-MADR-opencode-1.18.21-surface-parity.md
---

<!-- markdownlint-disable MD004 MD013 MD024 MD029 MD033 MD036 MD060 -->

# Implement OpenCode 1.18.21 stable surface parity

## Associated decision

This plan implements
[0112-MADR-opencode-1.18.21-surface-parity.md](0112-MADR-opencode-1.18.21-surface-parity.md),
including its 2026-08-22 stable-parity scope amendment. The identifier and
complete slug are intentionally identical; this PLAN is an implementation
artifact, not a second decision record.

The MADR was accepted and this plan approved by the Project Owner on
2026-08-22. Approval fixes the scope, phase order, and acceptance gates below;
it does **not** start the work. No source, test, fixture, configuration, build,
staging, or commit change may be made until the owner gives an explicit
go-ahead to begin executing, in the turn that starts it. Once started, execution
stays inside the approved scope: anything discovered mid-flight that falls
outside it is reported and waits for approval rather than being implemented
opportunistically. Each phase ends in a separate local commit after its
verification gates pass. No phase authorizes a push.

## Goal

Pin and verify OpenCode 1.18.21, then expose the maximum useful documented
stable OpenCode surface that fits mcremote's authenticated single-engine
architecture:

* resume native OpenCode sessions and choose known projects;
* advertise real multimodal capabilities and expose OpenCode model variants
  as reasoning-effort rungs on the existing thinking-level contract;
* send image/audio prompt parts;
* preserve live/replay message and part identity, complete tool states,
  removals, compaction, artifacts, detailed usage, and cost;
* browse and search a session workspace read-only through documented routes;
* report sanitized skill, LSP, formatter, and MCP state;
* author project-local skills through OpenCode's normal agent/write/permission
  flow and explicitly refresh the idle instance afterward;
* optionally control session sharing and direct shell execution behind
  disabled-by-default operator policy;
* regression-pin all stable OpenCode behavior already supported; and
* ship unit/widget tests with every change, keep every touched executable
  target at or above 80%, and prevent uncovered statements or lines from
  accumulating.

The result must not depend on an experimental endpoint, duplicate OpenCode's
configuration/credential control plane, expose arbitrary host paths or
secrets, or create a per-session OpenCode/ACP process.

## Stability rule and scope

### Admitted

An operation is admitted only when all of these are true:

1. It is listed in OpenCode's official server route table, **or** official
   feature documentation documents the capability and the operation is a read.
   Two admitted reads are not in the route table and are recorded as errata
   rather than silently reclassified: `GET /skill` (documented skills feature;
   GET-only in source and OpenAPI; full lifecycle reproduced on 1.18.21) and
   the pre-existing `GET /vcs/status` (grandfathered aggregate Diagnostics use
   only, no new phone operation). Prose absence never admits a **mutation**;
   every mutation this plan admits appears in the official route table.
2. It exists in the 1.18.21 OpenAPI/source boundary.
3. A fixture test proves the exact request/response or event shape.
4. A `live_opencode` test proves required route behavior against exactly
   1.18.21 before the implementation phase is accepted. Event shapes that
   cannot be induced deterministically without a model or destructive state
   use the exact OpenAPI/source schema plus fixtures; naturally occurring
   events are additionally checked live. Share/unshare is the sole route
   exception because it mutates an external service: source/fixture tests are
   mandatory and a live smoke requires separate per-run owner consent.
5. It can be represented without leaking provider configuration, credentials,
   arbitrary absolute paths, or unbounded payloads.

### Excluded

The implementation must not call or expose:

* any `/experimental/*` endpoint, including tool IDs or capabilities;
* any new `/api/*` endpoint; the existing proven
  `/api/session/{id}/model` exception remains;
* ACP, `opencode run`, TUI/web/PTY controls, replay controls, plugin/database/
  GitHub/PR administration, mDNS, CORS, or public server binding;
* undocumented `/sync/*`, message/part mutation, `/vcs/apply`, or similar
  internal routes;
* provider/config reads or writes that could expose API keys;
* project metadata mutation or phone-supplied server log injection;
* a raw phone-side skill/reference/plugin filesystem editor or installer;
* documented but transient `POST /mcp` dynamic add, arbitrary MCP
  remove/configuration, headers, local commands, OAuth, client registration,
  credential reads/writes, callback handling, or remote connect/disconnect.
  Exact 1.18.21 add mutates only instance memory, accepts command/environment
  or URL/header/OAuth-secret configuration, and has no documented matching
  delete; connect/disconnect/OAuth remain absent from official prose;
* `/file/status` and `/find/symbol` as user-visible features while their exact
  1.18.21 handlers unconditionally return empty arrays; or
* video/PDF prompt inputs until the shared client has bounded pickers,
  previews, frame-budget behavior, and protocol acceptance tests.

If a phase discovers that a required route is undocumented, experimental,
absent on 1.18.21, or materially different from the fixture, stop that phase,
record the observation in the MADR/PLAN, and request re-approval. Do not
substitute a nearby internal endpoint.

## Cross-cutting contracts

### Additive provider interfaces

Add optional interfaces in `internal/provider/provider.go`. The session manager
and WebSocket server use type assertions; unsupported providers return the
existing stable unsupported-operation error without changing their behavior.

| Interface | Method contract |
| --- | --- |
| `ProjectCatalog` | `ListProjects(context.Context) ([]ProjectMeta, error)` |
| `WorkspaceSession` | `ListWorkspace`, `ReadWorkspace`, `SearchWorkspace` |
| `SkillRefreshSession` | `RefreshSkills(context.Context) error` after all sessions in the instance are idle |
| `ShareSession` | `CurrentShare(context.Context) (ShareState, error)`, `Share(context.Context) (ShareState, error)`, and `Unshare(context.Context) error` |
| `ShellSession` | `Shell(context.Context, command string) error`; output arrives through normal tool events |

`AgentSessionLister`, `ConfigSession`, `DiagnosticsSession`, `ModelSession`,
`ThinkingSession`, and existing event types are reused and extended rather than
duplicated. In particular no new interface or wire field is introduced for
OpenCode model variants: P3 implements the existing `provider.ThinkingSession`
and populates the existing `picker.Option.ThinkingLevels`.
`httpagent.Provider` and `httpagent.Session` forward only dialect interfaces
that are actually implemented.

P4 adds one transport-internal optional interface in
`internal/provider/httpagent/httpagent.go` rather than changing the public
`provider.Session` contract:

```go
type IdentifiedPromptDialectSession interface {
  NewPromptMessageID() string
  PromptWithMessageID(context.Context, string, []provider.Content) error
}
```

OpenCode returns `"msg_" + uuid.NewString()`, which satisfies the exact 1.18.21
`MessageID` schema (`startsWith("msg")`), and sends that same value through the
documented optional `messageID` prompt field. `httpagent` allocates the ID when
it accepts either an immediate or queued prompt, stores it with the queued
content, stamps the optimistic `user_message`, and calls the identified method
when present. Dialects that do not implement the interface retain the current
ID-less `Prompt` path. This keeps Kilo and other providers source- and
behavior-compatible while giving OpenCode replay a deterministic correlation
key.

Extend `AgentSessionMeta` with `ModelID` (full `provider/model`), `Variant`,
`Agent`, and optional `Aggregate *AgentSessionUsage`. `AgentSessionUsage` has
non-negative `int64` input/output/reasoning/cache-read/cache-write fields and
nullable `*float64 CostUSD`; a nil aggregate means upstream omitted accounting,
while present zero means a known free/empty session.

### Additive phone operations

All requests require the normal authenticated owner connection. Session-bound
handlers also use the manager's ownership check before the provider type
assertion.

| Request | Result | Bounded payload |
| --- | --- | --- |
| existing `agent_sessions.list` | existing `agent_sessions.list_result` | at most 100 root sessions |
| `projects.list` | `projects.list_result` | at most 100 projects |
| `workspace.list` | `workspace.list_result` | at most 200 entries for one directory |
| `workspace.read` | `workspace.read_result` | at most 262,144 UTF-8 bytes |
| `workspace.search` | `workspace.search_result` | at most 100 matches |
| `session.refresh_skills` | `ok` | no phone-supplied path or content |
| `session.share_state` | `session.share_state_result` | optional bounded HTTPS URL; read-only under any policy |
| `session.share` | `session.share_result` | one HTTPS URL |
| `session.unshare` | `ok` | no response data |
| `session.shell` | `ok` after upstream completion | command at most 8,192 UTF-8 bytes |

The corresponding request/result structs live in
`internal/protocol/messages.go`, message constants are documented in
`docs/protocol-v1.md`, dispatch lives in `internal/ws/server.go`, and Dart
models/client methods live in `apps/mobile/lib/data/protocol/models.dart` and
`apps/mobile/lib/data/ws/mcremote_client.dart`.

### Deterministic limits and sanitization

| Data | Limit and normalization |
| --- | --- |
| Native sessions | 100 root sessions; ID 256, title 256, CWD 4,096; newest update first, ID tie-break |
| Projects | 100; ID 256, display name 128, worktree 4,096; reject filesystem root; name then ID |
| Workspace path | normalized slash-separated relative path, at most 4,096; empty means root |
| Workspace list | 200 rows; row path 4,096; directories before files, then lexical path |
| Text/file search | query 256; 100 rows; provider order normalized by relative path then line/column |
| Workspace content | UTF-8 text only, at most 262,144 bytes; no NUL; oversize is `result_too_large`, never partial content |
| Workspace HTTP envelope | at most 2,097,152 raw JSON bytes per response; read 2,097,153 to detect overflow before decode |
| Skills | 64; name 128, description 512; no location or content |
| LSP/formatters/MCP | 32 of each; names 128; states from a closed normalized vocabulary |
| Prompt attachment | kind `image`/`audio`; filename 256 basename characters; MIME 128; decoded non-empty bytes must fit the exact 1 MiB serialized frame |
| Assistant artifact | filename 256, MIME 128, URL 2,048; decoded inline data at most 524,288 bytes |
| Detailed usage | non-negative 64-bit counts; finite non-negative USD cost; invalid values omitted |
| Share URL | HTTPS only, at most 2,048; no fragment/userinfo |
| Shell command | non-empty UTF-8, no NUL, at most 8,192 bytes; 30-minute server operation timeout |

Workspace requests always supply the live session's CWD as the OpenCode
`directory` query and never accept a phone-supplied directory/workspace
override. Before dispatch, clean and join the requested path against the
session CWD and `Lstat` every existing component introduced beneath that
already approved session root; reject absolute input, NUL, `..` escape, any
such symlink component, and any path outside CWD. Repeat validation immediately
before HTTP and validate every returned relative path. OpenCode owns the
eventual file open, so this cannot eliminate a race with a concurrent host
filesystem mutation; the supported threat boundary assumes the local workspace
is not concurrently hostile and tests document that residual risk.
Responses discard upstream absolute paths and URIs and return only normalized
relative paths. The daemon does not fetch artifact URLs; it accepts only HTTPS
or bounded safe data URLs. Logs contain counts, operation names, and sanitized
IDs, never file content, artifact bytes, commands, MCP errors, share URLs,
headers, or credentials.

Add `workspace_read`, `skill_refresh`, `share_state`, `share`, and `shell`
booleans to `event.Capabilities`. They are false by default, emitted through
the existing `session_capabilities` event, and are true only when the live
session implements the relevant optional interface and the corresponding
policy (where required) is enabled. The phone renders no operation whose
capability is false.

### Error contract

Add stable protocol error codes in `internal/protocol/errors.go` and Dart
friendly mappings:

* `feature_disabled` for share or shell when its daemon flag is
  false;
* `unsupported_operation` when the selected provider lacks an optional
  interface;
* `instance_busy` when any session in the scoped OpenCode instance is running
  or a prompt/session start wins the refresh race;
* `invalid_path`, `path_escape`, `path_symlink`, `binary_content`, and
  `result_too_large` for workspace validation;
* `invalid_share_url` and `invalid_command` for bounded
  control validation; and
* existing `provider_error` for a sanitized upstream failure.

No provider response body is copied verbatim into a phone-visible error.

### Unit-test and coverage contract

P0 creates `scripts/coverage-snapshot.sh`, `scripts/coverage-delta.sh`, and
fixture-driven `scripts/coverage-delta_test.sh`. The snapshot command runs Go
tests with `-count=1 -covermode=atomic -coverprofile` under default build tags
and can run Flutter with `--coverage --coverage-path` outside the worktree. The
delta command parses raw Go statement counts and LCOV `LF`/`LH` data; it never
compares rounded console percentages.

The stable CLI grammar is below. Uppercase operands are command grammar, not
unresolved implementation choices: `OUTPUT_DIR`, `BEFORE_DIR`, `AFTER_DIR`,
and `SUMMARY_JSON` are paths; `GO_PACKAGE` is repeatable; and `DART_FILE` is a
repository-relative path repeatable once per touched production file.

```bash
scripts/coverage-snapshot.sh --output OUTPUT_DIR --go GO_PACKAGE [GO_PACKAGE ...] [--flutter apps/mobile]
scripts/coverage-delta.sh phase --before BEFORE_DIR --after AFTER_DIR --minimum 80.0 --go GO_PACKAGE [GO_PACKAGE ...] [--dart-root apps/mobile --dart-file DART_FILE ...]
scripts/coverage-delta.sh floor --after AFTER_DIR --minimum 80.0 --opencode-floor 85.0 --go GO_PACKAGE [GO_PACKAGE ...] [--dart-root apps/mobile --dart-file DART_FILE ...] [--new-dart-file DART_FILE ...]
scripts/coverage-delta.sh baseline --baseline-json SUMMARY_JSON --after AFTER_DIR --minimum 80.0 --opencode-floor 85.0 [--dart-root apps/mobile --dart-file DART_FILE ...]
```

`coverage-snapshot.sh` fails on any test failure and uses slash-to-underscore
profile names deterministically. `coverage-delta.sh phase` enforces the 80.0%
floor and all per-phase rules and exits nonzero unless at least one executable
target improves. `floor` checks only absolute thresholds and is portable across
CI operating systems; `baseline` checks same-platform cumulative package/
application fractions, uncovered counts, the 80.0% floor, and the OpenCode
floor. For Flutter, the minimum applies both to the application aggregate and
every requested existing production file; each `--new-dart-file` uses the
90.0% floor. All commands print a stable JSON summary suitable for the final
handoff; none writes the repository.

Pre-existing debt is out of scope (0113). For each P1–P10 phase that changes
production code:

1. Before the first production edit, create a unique directory with
   `mktemp -d /tmp/mcremote-0112-pN-coverage.XXXXXX`, record it in the phase
   todo, and snapshot every Go package and Dart production file named in the
   table below.
2. Write unit/widget tests in the same phase and commit as the production code.
   Each new behavior receives success, sanitized upstream failure,
   cancellation/timeout where applicable, exact below/equal/above limits,
   backward-compatible serialization, disabled/unsupported/unauthorized paths,
   and deterministic concurrency/race coverage where shared state is involved.
   Every bug fixed while executing the phase receives a reproducing regression
   test before or with the fix.
3. After implementation, capture the same default-tag profiles and run the
   delta tool with `--minimum 0`. Each touched existing target's exact
   covered/total fraction must not decrease. Every new Dart production file must
   be at least 90.0% line-covered. The absolute uncovered count is reported but
   does not gate the phase (amended 2026-08-23): adding new surface raises it
   even when the new code is better covered than the file it lands in. At least one touched executable package or Dart file must
   strictly improve in each phase. A phase that changes declarations but no
   executable statements still requires round-trip/compatibility tests but is
   neutral in the numeric comparison. Absolute floors are 0113's gate, not this
   plan's: a phase is not blocked because a package it touches was already below
   80% before it started.
4. `internal/provider/opencode` follows the same non-regression rule as every
   other touched package. Raising it to the 85.0% floor is 0113 C6.
5. Run race tests separately. Live-tagged, token-bearing, loopback, integration,
   and end-to-end tests are required where specified but are excluded from the
   unit-coverage profiles; they cannot compensate for a failed unit delta.
6. Keep profiles in the recorded temporary directory until the phase commit and
   coverage result are reviewed, then remove only that exact temporary
   directory. Never commit raw profiles or generate `apps/mobile/coverage/`.
   The phase cannot enter its Commit subsection until `coverage-delta.sh phase`
   exits zero and its JSON result is saved for the P11 handoff.

| Phase | Go coverage targets | Dart production coverage targets |
| --- | --- | --- |
| P1 | `internal/provider/opencode` | none |
| P2 | `internal/provider`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/ws` | protocol models, WebSocket client, sessions screen |
| P3 | `internal/event`, `internal/picker`, `internal/provider`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/session` | protocol models, chat screen |
| P4 | `internal/event`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/session` | chat models, reducer, cache |
| P5 | `internal/event`, `internal/provider/opencode` | chat models, reducer, rows, bubble, screen |
| P6 | `internal/event`, `internal/provider`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/ws` | protocol models, WebSocket client, chat screen, workspace sheet |
| P7 | `internal/event`, `internal/provider`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/ws` | protocol models, WebSocket client, chat screen, diagnostics sheet, skill-authoring sheet |
| P8 | `internal/config`, `internal/cli/service`, `internal/daemon`, `internal/provider/opencode` | none |
| P9 | `internal/event`, `internal/provider`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/ws` | protocol models, WebSocket client, chat screen |
| P10 | `internal/event`, `internal/provider`, `internal/provider/httpagent`, `internal/provider/opencode`, `internal/protocol`, `internal/ws` | protocol models, WebSocket client, chat screen, shell-command sheet |

P0 is coverage-tooling/evidence work and P11 is documentation/full acceptance,
so neither has a production delta. P11 reruns cumulative profiles and confirms
that no target regressed against P0's committed sanitized baseline. Absolute
floors are checked by 0113, not here.

The Dart labels in the table are readability aliases for the exact production
paths in that phase's Files subsection. Pass every such path once with
`--dart-file`; do not pass test paths, generated files, or the whole Dart root
as a substitute for per-file enforcement. The Go package lists are already the
exact arguments to `--go`.

## Dependency and delivery order

`P0 → P1 → P2/P3 → P4 → P5` is the transcript and model path.
`P0 → P6 → P7 → P8` is workspace, diagnostics, and remote policy.
`P7/P8 → P9/P10` provides the operation gate and remote-policy configuration.
`P0, P1–P10 → P11` is full acceptance and documentation.

Execute phases in numeric order even where dependencies permit parallel work.
This keeps every commit independently reviewable and avoids shipping UI before
its daemon contract exists.

For every phase, the phrase “precheck the changed Go files” has one mechanical
meaning: pass the `.go` paths listed in that phase's Files section (including
each named new `*_test.go` file) as the quoted, space-separated `FILES` value
to `make pre-add-check`. Do not include a Go file from another phase or omit a
listed Go file that changed. This is the exact pre-add set for that phase.

## P0 — Freeze the 1.18.21 stable evidence contract

### Outcome

A sanitized, reproducible corpus proves the version, documented primary API
sets, and normal stable route/event shapes. Reusable tested tooling makes unit
coverage deltas enforceable from P1 onward. No acceptance test
depends on experimental output.

### Files

* Amend `docs/0112-MADR-opencode-1.18.21-surface-parity.md` only to append the
  exact sanitized commands/results and observed errata. Do not create an
  unnumbered docs subtree or rewrite historical rationale.
* Add `internal/provider/opencode/testdata/surface-1.18.21/` with
  `manifest.json`, `health.json`, `openapi-paths.txt`,
  `openapi-operations.txt`, `openapi-schemas.txt`, `event-types.txt`,
  `agents-summary.json`, `commands-summary.json`, `stable-routes-summary.json`,
  `unit-coverage-baseline.json`, and the exact fixtures
  later phases consume for session/project lists, model capabilities/variants,
  prompt file parts, message parts/removals, workspace endpoints, diagnostics
  endpoints, native share state, and shell events.
* Add `internal/provider/opencode/surface_contract_test.go`; update
  `internal/provider/opencode/live_http_test.go` with a loopback-bind preflight.
* Create `scripts/coverage-snapshot.sh`, `scripts/coverage-delta.sh`,
  `scripts/coverage-delta_test.sh`, and deterministic Go-cover/LCOV inputs under
  `scripts/testdata/coverage/`.

### Steps

1. Add `TestLiveLoopbackBindPreflight`, which calls
   `net.Listen("tcp", "127.0.0.1:0")`, records only success/failure, and closes
   the listener. Run it before starting the corpus probe. If the execution
   sandbox returns `EPERM`/`Operation not permitted`, stop P0 and rerun it in an
   owner-approved environment that permits loopback bind; do not substitute
   source or fixture evidence for a required runtime gate. Then assert
   `opencode --version` and `GET /global/health` both equal `1.18.21`.
   Record the sanitized command identifier `opencode` (not its user-specific
   absolute path), Darwin/arm64 format, SHA-256, behavior commit
   `57fa34f23599f65dd1027f9caac31e6c576ce644`, package-version commit
   `ad0bb6d9a3e779def694adc093a811e86a529df0`, local assessed HEAD, and the fact
   that the clone has no `v1.18.21` tag in `manifest.json`.
2. Derive sorted path, operation, schema, and top-level Event discriminator
   lists from live `GET /doc` and `packages/sdk/openapi.json` read from exact
   commit `ad0bb6d9a3`, not the newer working tree; compare them with the retained
   1.18.7 boundary.
3. Probe only admitted GET routes with a temporary Git project and isolated
   XDG config/data/cache/state: project, root-session, model/provider, agent,
   command, skill, VCS, file/list/content, text/file find, LSP, formatter, and
   MCP. Isolating XDG and passing `--pure` is **not** sufficient to measure only
   built-ins: OpenCode also loads skills from the absolute home roots
   `~/.claude/skills`, `~/.agents/skills`, and `~/.config/opencode/skills`, and a
   verified `--pure` run on this host still picked up a host skill from
   `~/.claude/skills`. Redirect `HOME` (or otherwise neutralize those roots) for
   any built-ins-only measurement and record in `manifest.json` exactly which
   roots were neutralized. Record in the corpus that `GET /skill` is admitted as
   route-table errata under the stability rule above — documented as a feature,
   GET-only in source and OpenAPI, reproduced live — and not as a route-table
   entry. Separately
   regression-probe the provider's existing aggregate Diagnostics use of
   undocumented `/vcs/status` and label it compatibility-only. Record the empty
   `/file/status` and `/find/symbol` handlers as exclusions, not fixtures for
   planned functionality. Source-record documented `POST /mcp` and why it is
   excluded, but do not send it: even an isolated add can spawn a process or
   initiate an outbound connection.
4. Create one isolated no-model session and execute only
   `printf mcremote-shell-probe` through the documented shell endpoint to pin
   its normal user/message/tool SSE sequence and blocking response. Pin the
   exact observed shape: one synthetic `user` message with a single synthetic
   text part, one `assistant` message with `cost: 0` and every `tokens` bucket
   zero, and one `tool` part whose `tool` field is **`bash`** — 1.18.21 has no
   distinct shell tool ID — in state `completed` with `state.output` equal to
   the command's stdout and `state.metadata.output` repeating it. Because the
   tool name is `bash`, the existing `kindForTool` mapping already yields
   `execute` and P10 needs no new tool-kind entry; a fixture assertion pins
   that. Derive
   model-dependent FilePart, removal, compaction, and usage fixtures from the
   exact 1.18.21 OpenAPI/source schemas; do not spend tokens merely to induce
   an event. Share/unshare is not called because it changes an external
   service.
5. Pin the reproduced skill lifecycle: initialize `GET /skill`, write one
   valid project-local probe skill, prove the cached GET is unchanged, dispose
   only the idle project instance through `POST /instance/dispose`, then prove
   the new skill appears in both `GET /skill` and `GET /command` while the
   existing session and message history remain readable.
6. Hand-redact and mechanically scan the corpus for home paths, tokens, API
   keys, headers, cookies, model output, provider bodies, and absolute paths.
7. Mark any historic tool-ID/capability observation as non-contractual research;
   tests must fail if production code references `/experimental/`.
8. Implement coverage snapshot/delta tooling from the cross-cutting contract.
   Fixture tests must prove exact-rational comparison, uncovered-count
   comparison, exact 79.999% rejection, exact 80.0% acceptance, the 82.0%
   target, equality, strict improvement, zero-statement input, malformed
   profile rejection, existing-Dart-file and aggregate floors, new-Dart-file
   90.0% enforcement, platform-independent `floor` behavior, and failure when
   a requested source file is absent from LCOV.
9. Use the new snapshot tool after the P0 evidence/tooling additions but before
   any production-file edit to write only sanitized covered/total/uncovered
   counts, exact fractions, tool versions, and package/file identifiers for
   every package in the phase target table, the Flutter application, and every
   existing planned Dart file: `lib/data/protocol/models.dart`,
   `lib/data/ws/mcremote_client.dart`,
   `lib/features/sessions/sessions_screen.dart`,
   `lib/features/chat/chat_screen.dart`, `lib/data/chat/chat_models.dart`,
   `lib/data/chat/transcript_reducer.dart`,
   `lib/data/chat/transcript_cache.dart`,
   `lib/data/chat/transcript_rows.dart`, and
   `lib/features/chat/chat_bubble.dart`. Write them to
   `unit-coverage-baseline.json`. P0's new Go test is a fixture/source contract
   validator and must not invoke provider production statements, so the
   OpenCode entry must reproduce the observed 1,383 / 1,659 statements
   (83.36% rounded); Flutter must reproduce 9,061 / 11,789 lines. Keep raw Go
   profiles and LCOV outside the worktree and remove them after verifying the
   summary. Two independent default-tag runs on this host reproduced
   `internal/provider/opencode` 1,383 / 1,659, `internal/provider` 79 / 161,
   `internal/provider/httpagent` 737 / 1,470, `internal/event` 4 / 6,
   `internal/picker` 164 / 191, `internal/protocol` 11 / 25, `internal/config`
   483 / 615, `internal/cli/service` 876 / 1,268, and Flutter 9,061 / 11,789
   exactly, while `internal/daemon` (246 vs 247), `internal/session` (1,401 vs
   1,404), and `internal/ws` (1,152 vs 1,142) each moved by a small number of
   concurrency-dependent statements. Capture the committed baseline from a
   single snapshot run, record for each target whether it was stable across the
   two P0 runs, and treat the three unstable packages as requiring a same-run
   before/after comparison — never a comparison against the prose table in the
   MADR.

### Verification

```bash
go test ./internal/provider/opencode -run 'TestSurfaceContract|TestNoExperimentalRoute'
go test -tags live_opencode ./internal/provider/opencode -run '^TestLiveLoopbackBindPreflight$'
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveVersionAndStableSurface'
shellcheck scripts/coverage-snapshot.sh scripts/coverage-delta.sh scripts/coverage-delta_test.sh
bash scripts/coverage-delta_test.sh
! rg -n 'authorization|api[_-]?key|access[_-]?token|/Users/' internal/provider/opencode/testdata/surface-1.18.21
! rg -n '/experimental/' --glob '*.go' --glob '!surface_contract_test.go' internal/provider/opencode
```

Both negated `rg` commands must find no match.

**Guard split (amended 2026-08-22, owner-approved during P0.)** These were
originally one command that also forbade the literal `/experimental/` inside the
fixture directory. That is unsatisfiable together with step 2: the canonical
path and operation sets are complete sets, and on 1.18.21 they legitimately
contain 21 `/experimental/*` paths and 25 `/experimental/*` operations. Dropping
them would make `openapi-paths.txt` not the path set and would falsify the 162 /
188 counts D3 rests on. The two checks assert the two different things step 6 and
step 7 actually require: the corpus carries no credential or host-path material,
and no OpenCode provider Go file calls an experimental endpoint. The second is
scoped to `internal/provider/opencode` and excludes `surface_contract_test.go`, which is
the file implementing the equivalent Go guard and therefore necessarily contains
the literal it searches for — `TestNoExperimentalRoute` skips itself for the
same reason. It is otherwise scoped exactly as the P11 acceptance `rg` is;
repository-wide the only other match is `internal/provider/kilo/pin_scope_test.go`,
which is Kilo's own guard asserting that it does *not* use
`/experimental/worktree`. `stable-routes-summary.json` names `/experimental/*`
once, as an exclusion, which is a record of the decision rather than a
dependency on it.

### Acceptance

* Version identity is exact and the canonical primary sets remain 162 paths,
  188 operations, 472 schemas, and 89 Events with empty 1.18.7→1.18.21 set
  differences.
* The direct listener control and OpenCode server bind both succeed; an
  environment-level loopback denial blocks P0 explicitly.
* Every route needed by P2–P10 has a stable fixture and source/doc citation;
  event-only fixtures identify their exact source schema and commit.
* The skill fixture proves GET-only discovery, cache behavior, idle disposal,
  session/history preservation, and post-disposal command/skill refresh.
* The shell fixture contains one completed tool part with exact probe output
  and reports zero cost without invoking a model.
* No runtime or test helper calls an experimental endpoint.
* The corpus contains no user-state, secret, absolute-home, or prompt content.
* Coverage tooling rejects every regressing/malformed fixture, accepts exact
  80.0% equality and improvement fixtures, distinguishes the 82.0% headroom
  target from the hard floor, and the sanitized baseline summary matches fresh
  default-tag snapshots without committing raw profiles.

### Commit

Run
`make pre-add-check FILES="internal/provider/opencode/surface_contract_test.go internal/provider/opencode/live_http_test.go"`,
stage only P0 files, and run `git commit --no-edit`.

## P0A — moved out of scope (2026-08-22)

P0A closed the repository's **pre-existing** sub-floor coverage before any parity
work could start. At the owner's direction that scope moved to
[0113-PLAN-preexisting-unit-coverage-debt.md](0113-PLAN-preexisting-unit-coverage-debt.md),
because the debt predates this release, sits mostly in packages this plan barely
edits, and measured roughly a thousand Go statements and six hundred Dart lines —
larger than several parity phases combined.

Nothing about testing *this plan's own work* changed. Every phase below still
ships its unit and widget tests in the same commit, every new Dart production
file still reaches 90.0%, and no phase may degrade a target it touches. What is
gone is the requirement to fix unrelated legacy gaps first, and with it the
absolute-floor `make coverage-check` target and CI job, which 0113 C8 adds in the
change set that first makes the tree satisfy them.

Phase numbering is unchanged so commits, todos and cross-references stay stable.
Read `P0 → P1` and treat this section as a pointer.

## P1 — Pin, bootstrap, and existing-surface corrections

### Outcome

The daemon recognizes 1.18.21 as known-good without rejecting newer compatible
versions, starts from a current engine default, and classifies current tools and
command hints correctly.

### Files

* `internal/provider/opencode/version.go` and `version_test.go`.
* `internal/provider/opencode/http.go`, `http_model_test.go`,
  `catalog_test.go`, and `live_http_test.go`.
* `internal/provider/opencode/command.go` and `command_test.go`.
* `internal/provider/opencode/auth_test.go`, `question_test.go`,
  `permission_test.go`, `todo_test.go`, and `subagent_test.go`; create
  `upstream_test.go`.
* `Makefile`.
* `docs/0021-MADR-opencode-http-api-coverage.md`.

### Steps

1. Add `KnownGoodVersion = "1.18.21"` while retaining
   `MinVersion = "1.18.0"`.
2. Keep below-minimum + session-tree enabled as the only version startup
   failure. Log exact known-good as info and any other parseable supported
   version once per engine boot as known-good drift.
3. Seed `opencode/big-pickle` and use the engine's
   `default["opencode"]` after catalog load; preserve explicit daemon/session
   model precedence.
4. Map `apply_patch` to edit; leave unknown, skill, custom, and MCP tools
   generic.
5. Carry OpenCode command argument hints into `event.AvailableCommand.Hint`
   in `advertiseCommands`. `GET /command` returns `hints` as a **string array**
   of template placeholders (live 1.18.21: `["$ARGUMENTS"]` for built-in `init`
   and `review`, `[]` for skill-backed commands), while `Hint` is a single
   string, so fix the reduction exactly: trim each element, drop empty
   elements, preserve upstream order without sorting or deduplicating, join
   with a single U+0020 space, and clip the result to 128 UTF-8 bytes on a rune
   boundary. An absent, null, or all-empty array yields `""` and the field is
   omitted. Use the same helper in `ListCommandsLive`, replacing its current
   `strings.Join(c.Hints, ", ")` description fallback, so the picker and the
   advertised command list can never disagree about one command's hint.
6. Refresh the living HTTP matrix to 1.18.21 and identify every implemented,
   newly planned, engine-owned, opt-in, and excluded stable route.
7. Before production edits, capture the P1 OpenCode profile. Add or enhance the
   following exact default-tag tests; they are part of P1, not optional gap
   suggestions:
   * `TestCompareVersionsCompleteMatrix`: whitespace, `v`/`V`, missing patch,
     prerelease/build suffix, malformed/malformed, malformed/valid, and every
     major/minor/patch less/equal/greater branch; `TestKnownGoodHealthMatrix`:
     missing/malformed/below-minimum/exact/newer health versions and exactly one
     known-good-drift log per boot.
   * `TestCatalogKnownGoodDefaultMatrix`: connected catalog success,
     engine-reported OpenCode default, missing default, absent OpenCode provider,
     malformed payload, timeout, static fallback, configured/session override,
     and deterministic cache invalidation.
   * `TestCommandHintsAndToolKindsMatrix`: absent/empty/present hints;
     `apply_patch`/edit/write and unknown/skill/custom/MCP mappings; diff event
     malformed/empty/default status, 12/13-file truncation, missing path, and
     command-executed argument clipping.
   * `TestCredentialFileFallbackMatrix` in isolated XDG state: invalid/empty
     secret, empty upstream, foreign method, valid create/update with metadata,
     path error, clear empty/missing/present; assert no secret in errors/logs.
   * `TestSetActiveUpstreamMatrix`: empty/disconnected/read-error, configured
     default model, first model-ID fallback, map-key fallback, no-model error,
     and state unchanged on every failure.
   * `TestResyncPendingQuestionsMatrix`, `TestResyncPendingPermissionsFallbackMatrix`,
     and `TestResyncTodosMatrix`: API error, other-tree filtering, empty parent,
     malformed/legacy/current rows, empty snapshots, and exact emitted IDs.
   * `TestRefreshAndFinishSubagentsMatrix`: absent/completed child refusal,
     name-only/task-only/both updates with clipping, finish one, finish all,
     no-change no-emit, and deterministic ID ordering.
8. Verify with `go tool cover -func` that each function named above actually
   moved. The final default-tag OpenCode profile must not regress against its P1
   before profile — the exact fraction may not fall and the uncovered count may
   not rise — and no live-tagged test contributes to the calculation. Raising the
   package to the 85.0% floor is 0113 C6, not a P1 gate.

### Verification

```bash
go test -race ./internal/provider/opencode
make pre-add-check FILES="internal/provider/opencode/version.go internal/provider/opencode/version_test.go internal/provider/opencode/http.go internal/provider/opencode/http_model_test.go internal/provider/opencode/catalog_test.go internal/provider/opencode/live_http_test.go internal/provider/opencode/command.go internal/provider/opencode/command_test.go internal/provider/opencode/auth_test.go internal/provider/opencode/question_test.go internal/provider/opencode/permission_test.go internal/provider/opencode/todo_test.go internal/provider/opencode/subagent_test.go internal/provider/opencode/upstream_test.go"
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveVersionAndStableSurface|TestLiveCommands'
```

### Acceptance

* 1.18.21 produces no drift warning; 1.18.0 starts; 1.17.9 follows the existing
  session-tree gate; 1.18.22 produces one warning and starts.
* A blank-model session converges on the reported OpenCode default.
* `apply_patch` renders as edit and command picker hints match OpenCode.
* The P1 unit profile strictly improves on its before profile, adds no
  uncovered statements, and every new branch above has a direct assertion
  independent of live-tagged tests.

### Commit

Stage only P1 files and run `git commit --no-edit` after the checks pass.

## P2 — Native session and project discovery

### Outcome

The phone can discover root OpenCode sessions, resume one through the existing
`agent_session_id` path, and select an engine-known project worktree for a new
session.

### Files

* `internal/provider/provider.go` and `provider_test.go`.
* `internal/provider/httpagent/httpagent.go`, `provider.go`, and
  `provider_test.go`.
* Create `internal/provider/opencode/discovery.go` and
  `discovery_test.go`; update `http.go`.
* `internal/protocol/messages.go`, `messages_test.go`, and `op_timeouts.json`.
* `internal/ws/server.go`, `server_session_handlers_test.go`,
  `method_availability_test.go`, and `op_timeout_test.go`.
* `apps/mobile/lib/data/protocol/models.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`, and
  `apps/mobile/lib/features/sessions/sessions_screen.dart`.
* `apps/mobile/test/resume_flow_test.dart`,
  `sessions_screen_test.dart`, and create `project_picker_test.dart`.
* `docs/protocol-v1.md`.

### Steps

1. Extend `provider.AgentSessionMeta` with the exact cross-cutting
   `ModelID`/`Variant`/`Agent`/`Aggregate` contract above; preserve old JSON for
   absent fields. Implement `AgentSessionLister` from
   `GET /session?roots=true&limit=100`. Retain root sessions only, cap fields,
   sort newest first with ID tie-break, and map ID/CWD/title/update time plus
   OpenCode's aggregate session accounting. Reject negative/non-finite values.
2. Add `ProjectMeta{ID, Name, Worktree}` and `ProjectCatalog`. Implement it from
   `GET /project`, discard malformed and non-absolute worktrees, cap, and sort.
   Reject the root sentinel entry on **both** signals: `id` equal to `global`
   and a worktree equal to the filesystem root `/`. A fresh 1.18.21 engine lists
   only real Git projects; opening any **non-Git** directory resolves its
   worktree to `/`, registers it under the project id `global`, and `GET
   /project` returns `{"id":"global","worktree":"/"}` from then on. The filter
   is therefore unconditional — any engine a user has actually driven will have
   opened such a directory — but it is a filter on an on-demand entry, not on a
   standing one. Root must never become a phone-selectable CWD. `Project.name` is optional upstream — fall back to the
   worktree basename, never to the raw path.
3. Forward both optional dialect capabilities through `httpagent.Provider`.
4. Add `projects.list`/result request handling with the same provider readiness,
   ownership-independent catalog rules used by models/agents and a 30-second
   operation timeout.
5. Populate the existing native-session picker for OpenCode, showing bounded
   model/agent/aggregate usage and cost when present, and add a project choice
   above free-text CWD in new-session UI. Selection copies the worktree into
   the existing CWD field; it does not bypass pinned-CWD validation.
6. Preserve manual CWD entry and all older daemon/client fallbacks.

### Verification

```bash
go test -race ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol ./internal/ws
(cd apps/mobile && dart format --output=none --set-exit-if-changed lib test)
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/resume_flow_test.dart test/sessions_screen_test.dart test/project_picker_test.dart)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveDiscovery'
```

### Acceptance

* At most 100 root sessions appear in deterministic order and child sessions
  never appear as resumable roots.
* Selecting a native session creates one daemon session that resumes the same
  OpenCode ID. P4's authoritative native-ID semantics make any replay idempotent
  against durable history; P2 does not add another replay path.
* Selecting a project fills only a valid worktree; manual CWD remains available.
* Older providers and clients retain their current behavior.
* P2 unit tests cover malformed/global/child session rows, zero and absent
  aggregate usage, root worktree rejection, caps/sorts, timeout, old JSON, and
  unsupported-provider fallback. All P2 Go/Dart deltas pass, no touched existing target regresses, and at least
  one touched executable target strictly improves.

### Commit

Run pre-add checks for every changed Go file, then stage P2 files and run
`git commit --no-edit`.

## P3 — Model capabilities, reasoning variants, and prompt attachments

### Outcome

OpenCode model metadata truthfully gates image/audio UI, exposes documented
model variants as reasoning-effort rungs on the daemon's existing thinking-level
contract (MADR 0112 A14), and sends non-text prompt content in stable
`FilePartInput` form.

### Files

* `internal/event/event.go` and `event_test.go`.
* `internal/picker/picker.go` and `picker_test.go`.
* `internal/provider/provider.go` and `provider_test.go`.
* `internal/provider/opencode/http.go`,
  `http_model_wire_test.go`, and `http_model_test.go`.
* Create `internal/provider/opencode/model_surface.go` and
  `model_surface_test.go`.
* `internal/provider/httpagent/httpagent.go`, `session.go`, `provider.go`,
  `provider_test.go`, and `session_test.go`.
* `internal/protocol/messages.go` and `messages_test.go`.
* `internal/session/manager.go` and `manager_parity_test.go`.
* `apps/mobile/pubspec.yaml`, `apps/mobile/pubspec.lock`,
  `apps/mobile/lib/features/chat/chat_screen.dart`, and
  `apps/mobile/lib/data/protocol/models.dart`.
* `internal/provider/opencode/commandtable.go` and `command_test.go`
  (flip the canonical `/thinking` entry; no new command and no new spec).
  `internal/session/commands.go` is **not** in this phase: `cmdThinking` already
  type-asserts `provider.ThinkingSession` and is provider-agnostic, so it needs
  no edit.
* `apps/mobile/test/staged_images_test.dart`,
  `thinking_picker_ui_test.dart`, `model_picker_test.dart`, and create
  `audio_attachment_test.dart`.
* `docs/protocol-v1.md`.

### Steps

1. Extend `picker.Option` additively with typed input-capability fields only.
   Decode model `capabilities.input` and the attachment/tool-call/reasoning
   flags; do not hide these in `Meta` or expose cost tables/provider config.
   Decode `Model.variants` — an optional `Record<string, object>` on the
   1.18.21 `Model` schema — into the **existing** `picker.Option.ThinkingLevels`
   field: one `picker.ThinkingLevel` per key, `ID` = the exact upstream key,
   no invented `Label` or `Description`, ordered by `picker.NormalizeThinkingLevels`,
   and no rung marked `Default` because the schema carries no default marker.
   Never synthesize a rung and never emit a level a model did not advertise; a
   model whose `variants` is absent or empty keeps an empty `ThinkingLevels`,
   which remains the honest answer. Do **not** add a new variant field to
   `picker.Option`. Correct the now-false comments at
   `internal/provider/provider.go:388-392` and `internal/picker/picker.go:52-56`
   that name OpenCode as having no per-session effort control. Cache the
   surface by full `provider/model` ID after the live catalog replaces
   bootstrap entries.
2. Emit `session_capabilities` and `session_config` at create/resume and after
   model change. `httpagent.Session` must implement and forward the existing
   `ConfigSession` optional interface when the dialect supports it.
   `image`/`audio` are true only when the active model reports that input. Add
   an optional dialect-session `AfterBootRefined` hook: after asynchronous
   `Dialect.AfterBoot` returns, `httpagent.Provider` snapshots registered
   sessions and asks each open session to re-resolve/re-emit its current model
   surface. A session created before the multi-MB catalog completes therefore
   starts conservatively false and becomes truthful without restart; closed or
   model-switched race cases are locked and idempotent.
3. Implement `provider.ThinkingSession` on the OpenCode session:
   `SetThinkingLevel` accepts only a rung the **active** model advertises and
   the reserved sentinel `default`, rejecting anything else with the existing
   unsupported/invalid error rather than forwarding it; `ThinkingLevel` returns
   the stored value, defaulting to `default`. Include `variant` in
   `prompt_async` and in `POST /session/{id}/command` only when the stored
   value is not `default` — 1.18.21 treats `default` as "no variant override"
   and normalizes it away itself, so sending it is redundant and sending an
   unadvertised key is an upstream error. On model change, reset the stored
   value to `default` and re-emit the model catalog so the phone's existing
   thinking picker re-reads the new rungs. Add **no** `variant` entry to
   `session_config`: the thinking picker and `/thinking` are the single control,
   and a duplicate selector on the same session is a defect the tests must
   reject.
4. On resume, read the authoritative `GET /session/{id}`
   `model.{providerID,id,variant}` fields — `id` and `providerID` are required
   and `variant` is optional in the 1.18.21 `Session` schema. Initialize the
   model from the validated full model ID, falling back to the active model when
   it is absent or no longer in the catalog. The thinking level is **not**
   resolved here: step 6 owns the single resume precedence rule, and this step
   only supplies the validated upstream `variant` to it. Do not fetch or infer
   either setting from message history — exact 1.18.21 session state already
   persists both.
5. Flip the canonical command gate. `internal/provider/opencode/commandtable.go`
   currently pins `"thinking": {Kind: command.KindNone, Note: "OpenCode has no
   per-session thinking level — the model decides"}`, whose comment cites
   MADR 0052 D6. Replace it with
   `"thinking": {Kind: command.KindOp, Op: command.OpSetThinkingLevel}` and
   delete the now-false note and comment, citing MADR 0112 A14. Without this the
   A14 mapping is unreachable: `/thinking` stays advertised as unavailable with
   an incorrect explanation even after `ThinkingSession` exists. Do not add a new
   command spec — `internal/command/specs.go` already defines `thinking`.
   OpenCode applies `variant` per request, so it is settable mid-session and
   `SetThinkingLevel` must never return `provider.ErrThinkingLevelFixed`; that
   sentinel stays Grok-specific.
6. Consume `provider.StartOptions.ThinkingLevel`, which `httpagent` and the
   OpenCode dialect currently ignore entirely, and settle precedence
   deterministically so a relaunch cannot silently re-impose a stale rung:
   * On **create**, apply `StartOptions.ThinkingLevel` when it is non-empty and
     advertised by the resolved model; otherwise `default`.
   * On **resume**, the engine is authoritative. Use `Session.model.variant`
     when present and still advertised. Fall back to
     `StartOptions.ThinkingLevel` only when upstream has no variant at all and
     the stored rung still validates. Otherwise `default`.
   * Never write the daemon's persisted `meta.ThinkingLevel` back to a session
     whose upstream variant disagrees; the manager already persists the user's
     choice on a successful `SetThinkingLevel`, and the OpenCode TUI or another
     client may have changed the engine's value in between.
7. Convert image/audio `provider.Content` into
   `{type:"file", mime, filename, url:"data:<mime>;base64,<data>"}`.
   Add optional bounded filename through `PromptAttachment` and
   `provider.Content`; accept only a basename with no slash, backslash, NUL, or
   control characters. Validate MIME family, canonical base64, decoded
   non-empty bytes, and the exact serialized WebSocket-frame limit before
   request creation, not an attachment-only estimate. Preserve text part order.
8. Never log a data URL or attachment bytes. Reject a non-text block when the
   active model capability is false rather than silently dropping it.
9. Add stable `file_selector: ^1.1.0`, regenerate the lockfile with the
   repository-pinned Flutter 3.44.6/Dart 3.12.2 toolchain, and add an audio-file
   picker restricted to the accepted audio MIME allowlist. Stage audio through
   the same bounded attachment model/preview/removal flow as images.

### Verification

```bash
go test -race ./internal/event ./internal/picker ./internal/provider ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol ./internal/session
(cd apps/mobile && flutter pub get)
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/staged_images_test.dart test/audio_attachment_test.dart test/thinking_picker_ui_test.dart test/model_picker_test.dart)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveModelSurface|TestLivePromptFileParts'
```

### Acceptance

* Image and audio attachment affordances follow the active model and update
  after `/model`; unsupported MIME families never enter the staged list.
* A fixture captures byte-for-byte correct text/image/audio part order and data
  URL form; invalid MIME/base64/oversize input is rejected before HTTP.
* Thinking levels come only from the active model's advertised variant keys,
  survive turns, reset to `default` on model change, are omitted from the
  request when `default`, and appear in no `session_config` option.
* `/thinking` is advertised as **available** on an OpenCode session and reports,
  sets, and rejects rungs through the unchanged manager path; the previous
  "OpenCode has no per-session thinking level" refusal and its note are gone,
  and `ErrThinkingLevelFixed` is never returned by this provider.
* Create and resume precedence is pinned by fixtures for all six combinations of
  present/absent `StartOptions.ThinkingLevel` against present/absent/no-longer-
  advertised `Session.model.variant`; a stale stored rung never overrides a live
  upstream variant.
* Unit tests pin exact full-frame size acceptance/rejection at the 1 MiB
  boundary. The live gate reads the real catalog once at phase acceptance and
  records what is actually advertised; it never fabricates a capability. The
  seeded default `opencode/big-pickle` cannot exercise this phase — on 1.18.21
  it reports `attachment: false`, all-false `capabilities.input` except `text`,
  and an empty `variants` map — so the gate selects, in order: an
  operator-supplied `MCREMOTE_LIVE_MODEL`; otherwise the first zero-cost
  connected model advertising `capabilities.input.image` and a non-empty
  `variants` map (a 2026-08-22 catalog snapshot offered
  `opencode/muse-spark-1.2-contributor-free` with image, audio, and five
  variants, and `opencode/x-preview-f-free` with image and three variants);
  otherwise it skips with a recorded reason. These IDs are mutable catalog
  data, so the gate resolves them at run time and never asserts them as
  constants.
* P3 unit tests cover asynchronous catalog success/failure and close/model
  races, variant-to-rung decoding (absent, empty, single, many, non-object
  values, duplicate-after-trim keys), rung validation and reset, rejection of
  an unadvertised or `default` value on the wire, absence of any
  `session_config` variant option, the flipped command-table entry and its
  advertised availability, `StartOptions.ThinkingLevel` create/resume
  precedence, every MIME/base64/filename boundary, and old clients omitting new
  fields. All P3 Go/Dart deltas pass, no touched existing target regresses, the touched
  mobile files do not gain uncovered lines, and one target strictly improves.

### Commit

Precheck the changed Go files using the mechanical Files rule above;
format/analyze/test Dart; stage P3 only; run `git commit --no-edit`.

## P4 — Transcript identity, replay fidelity, removal, and compaction

### Outcome

Live and replayed OpenCode messages use one mapper, preserve native identity,
show complete tool terminal state, and remove or reconcile stale content.

### Files

* `internal/event/event.go`, `event_test.go`, and `types_test.go`.
* Create `internal/provider/opencode/parts.go` and `parts_test.go`.
* `internal/provider/opencode/http.go`, `resync.go`,
  `http_resync_test.go`, `http_delta_test.go`, and `dedup_test.go`.
* `internal/provider/httpagent/httpagent.go`, `session.go`, `session_test.go`,
  `queue_test.go`, and `coalesce_test.go`.
* `internal/session/manager.go`, `manager_history_test.go`, and
  `manager_durable_test.go`.
* `docs/protocol-v1.md` and `internal/protocol/doc_coverage_test.go`.
* `apps/mobile/lib/data/chat/chat_models.dart`,
  `transcript_reducer.dart`, and `transcript_cache.dart`.
* `apps/mobile/test/history_replay_test.dart`,
  `transcript_reducer_test.dart`, `transcript_cache_test.dart`, and
  `transcript_ingest_test.dart`.

### Steps

1. Add optional `native_message_id`, `native_part_id`, and boolean `replace`
   to transcript-bearing events and persisted Dart chat items. `replace=true`
   means an authoritative full native-part snapshot; false means append delta.
2. Add control event `transcript_remove` carrying one message ID and optional
   part ID. Document replace/delete semantics and make it non-droppable.
3. Implement the cross-cutting `IdentifiedPromptDialectSession`. Allocate the
   OpenCode `msg_` ID before the optimistic user event, retain it in a typed
   queue entry with cloned content, and include it for both `prompt_async` and
   slash-command submission. The optimistic event has message ID and no part
   ID. On submission error retain the current visible local-attempt behavior,
   but never claim a part ID or fabricate an upstream response. Unit tests pin
   immediate, queued, drained, overflow, failure, cancel, and non-implementing
   dialect behavior.
4. Move text, reasoning, tool, and user-part mapping into one pure mapper used
   by SSE and replay. The 1.18.21 `Part` union is exactly `TextPart`,
   `SubtaskPart`, `ReasoningPart`, `FilePart`, `ToolPart`, `StepStartPart`,
   `StepFinishPart`, `SnapshotPart`, `PatchPart`, `AgentPart`, `RetryPart`, and
   `CompactionPart`. The mapper handles `TextPart`, `ReasoningPart`, `ToolPart`,
   and `FilePart` (the last via P5); `SubtaskPart` and `AgentPart` continue to
   be consumed by the existing subagent-tree and agent-mode paths and are not
   rendered as new chat rows here; the remaining six are the internal
   bookkeeping parts dropped by step 9. Every member is handled explicitly and
   an unknown future member is dropped with a debug log, never rendered as raw
   text. SSE `message.part.delta` is append; full
   `message.part.updated` and replayed parts are replacement snapshots.
   Preserve provider order and attachment metadata. The first authoritative
   user part for a message deletes/replaces its message-level optimistic row;
   subsequent user parts are keyed inside that row by native part ID. Extend
   `ChatItem` additively with an ordered cached list of native user-part
   components; first sight fixes component order, replacement updates in place,
   and derived text/attachments render as one user bubble. Part removal deletes
   only its component and recomputes the aggregate, making removal exact without
   guessing an ID for server-created parts.
5. Decode replayed tool input, output, metadata title, and error; apply the
   existing redaction and 8,000-character visible-output cap. A terminal failed
   part must never replay as completed.
6. Coalesce append text only when type, message ID, and part ID match. Never
   coalesce a replacement snapshot. Reduce non-user rows by
   `(provider, session, native_message_id, native_part_id)` so a replay/full
   update replaces prior deltas instead of appending duplicate text. Reduce one
   user row by `(provider, session, native_message_id)` and its ordered
   components by `native_part_id`; keep tool identity unchanged.
7. Make daemon durable history apply the same identity rule before sequence
   assignment: when a replacement snapshot arrives, delete every older
   transcript event for that native part and append the authoritative snapshot
   with a new sequence number. The first authoritative user part also removes
   the message-level optimistic event for the same native message. When a
   removal arrives, delete matching native part/message events and retain the
   new tombstone. Sequence gaps are valid; unrelated events and legacy rows
   without native IDs remain untouched.
8. Map `message.removed` and `message.part.removed` to `transcript_remove`.
   Deleting a message removes all matching parts; deleting a part removes only
   that item. Unknown IDs are idempotent no-ops.
9. On `session.compacted`, emit one bounded notice and do not invoke replay.
   A later full update for a pruned/compacted tool part replaces it by native
   ID. Do not render internal step/snapshot/patch/retry/compaction parts as chat
   rows.
10. Migrate cached rows additively; old cached rows with no native IDs remain
   readable and are not guessed at during removal.

### Verification

```bash
go test -race ./internal/event ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/history_replay_test.dart test/transcript_reducer_test.dart test/transcript_cache_test.dart test/transcript_ingest_test.dart)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveReplayIdentity|TestLiveCompactionReconcile'
```

### Acceptance

* The same fixture mapped live and via replay produces the same ordered logical
  transcript and native identities.
* Full tool results/errors survive resume within the existing cap.
* Part/message removal is idempotent and cannot delete another provider's or
  another session's content.
* Resume/replay and compaction leave one reconciled transcript with no
  duplicate rows or append-only replay growth in memory or durable history;
  `session.compacted` itself performs no history fetch.
* Immediate and queued OpenCode prompts send the exact `msg_` ID shown on the
  optimistic row; the first authoritative user part replaces that row, while a
  non-OpenCode dialect continues through the unchanged ID-less path.
* P4 unit tests cover delta/snapshot ordering, sequence gaps, replay over prior
  durable deltas, optimistic-to-authoritative user replacement, immediate and
  queued prompt identity, message/part tombstones, unknown IDs, legacy ID-less
  rows, and concurrent emit/persist paths. All P4 deltas pass, no touched existing target regresses, and one
  touched target strictly improves.

### Commit

Precheck, format, stage P4 only, and run `git commit --no-edit`.

## P5 — Assistant artifacts and detailed usage/cost

### Outcome

Safe assistant file parts render as artifacts, and OpenCode's stable token and
cost accounting is visible without breaking the legacy context indicator.

### Files

* `internal/event/event.go` and `event_test.go`.
* `internal/provider/opencode/parts.go`, `parts_test.go`, `usage.go`,
  `http.go`, and create `usage_test.go`.
* `docs/protocol-v1.md`.
* `apps/mobile/lib/data/chat/chat_models.dart`,
  `transcript_reducer.dart`, `transcript_rows.dart`,
  `apps/mobile/lib/features/chat/chat_bubble.dart`, and
  `apps/mobile/lib/features/chat/chat_screen.dart`.
* Create `apps/mobile/test/artifact_render_test.dart` and
  `usage_detail_test.dart`; update `history_replay_test.dart`.

### Steps

1. Add `artifact` event with native IDs, filename, MIME, byte size,
   `truncated`, and exactly one optional safe URL or bounded inline data value.
   Feed it from assistant `FilePart` values and from
   `ToolStateCompleted.attachments`; use the containing tool part's native
   identity plus attachment index for deterministic identity. On 1.18.21
   `attachments` exists **only** on `ToolStateCompleted` — `ToolStateError`
   (which the dialect surfaces as `failed`) has `status`, `input`, `error`,
   `metadata`, and `time` and no attachment field — so the decoder must not
   look for attachments on a failed tool state, and a fixture pins that a
   failed state produces no artifact. If a future release adds them there, that
   is a new assessment, not an implicit extension.
2. Accept HTTPS URLs with no userinfo and data URLs whose decoded payload is at
   most 524,288 bytes. Return metadata-only/truncated for larger, malformed,
   `file:`, `http:`, or unknown schemes. Never fetch a URL in the daemon.
3. Render bounded images inline; render other artifacts as filename/MIME/size
   cards. Opening uses the phone OS only for validated HTTPS; inline data can
   be previewed in memory, but transcript-cache serialization strips the inline
   bytes and persists only sanitized metadata.
4. Extend `event.Usage` additively with non-negative 64-bit `input`, `output`,
   `reasoning`, `cache_read`, and `cache_write`, plus nullable `cost_usd` so a
   known zero-cost turn differs from missing data. Keep legacy `used`/`size`
   integer fields unchanged.
5. Decode the latest assistant message's token buckets and cost from live and
   replay metadata, reject negative/non-finite values, and label the UI as
   latest-turn accounting rather than cumulative session totals. Show a compact
   context indicator with an on-demand breakdown; P2 owns aggregate native
   session totals.

### Verification

```bash
go test -race ./internal/event ./internal/provider/opencode
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/artifact_render_test.dart test/usage_detail_test.dart test/history_replay_test.dart)
```

### Acceptance

* Safe small image data renders; unsafe schemes and oversized data never become
  tappable and never exceed the frame budget.
* Live and replay artifact rows deduplicate on native IDs and obey removal.
* Legacy clients still receive meaningful `used`/`size`.
* Latest-turn token buckets and USD cost match fixture values exactly, free
  zero is distinguishable from absent cost, and malformed values are omitted.
* P5 unit tests cover every accepted/rejected URL scheme, inline size boundary,
  attachment-index identity, cache stripping, negative/non-finite accounting,
  missing versus free cost, and live/replay equivalence. All P5 deltas pass,
  no touched existing target regresses, and one touched target strictly improves.

### Commit

Precheck, format, stage P5 only, and run `git commit --no-edit`.

## P6 — Read-only workspace list, content, and search

### Outcome

An owner can inspect the active OpenCode workspace from the phone without
granting shell access or introducing a write path.

### Files

* `internal/event/event.go` and `event_test.go`.
* `internal/provider/provider.go` and `provider_test.go`.
* `internal/provider/httpagent/session.go` and `session_test.go`.
* Create `internal/provider/opencode/workspace.go` and `workspace_test.go`.
* `internal/protocol/messages.go`, `messages_test.go`,
  `op_timeouts.json`, and `errors.go`.
* `internal/ws/server.go`, `server_session_handlers_test.go`,
  `op_timeout_test.go`, and create `workspace_handlers_test.go`.
* `docs/protocol-v1.md`.
* Create `apps/mobile/lib/features/chat/workspace_sheet.dart`.
* `apps/mobile/lib/data/protocol/models.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`, and
  `apps/mobile/lib/features/chat/chat_screen.dart`.
* Create `apps/mobile/test/workspace_sheet_test.dart` and
  `workspace_protocol_test.dart`.
* `Makefile`.

### Steps

1. Define typed list entry, content, and search result structs.
   Search kind is the closed enum `text` or `file`; do not advertise symbol
   search until an assessed release implements it.
2. Implement stable `GET /file`, `GET /file/content`, `GET /find`,
   and `GET /find/file` with the live session CWD supplied as the
   URL-encoded `directory` query on every request. Use the exact upstream
   parameter names, which differ per route: `/file` and `/file/content` take
   `path`; `/find` takes `pattern`; `/find/file` takes `query` plus optional
   `type` (`file`/`directory`) and `limit`. Request `limit=100` on
   `/find/file` — the schema accepts an integer in 1..200 and the handler
   defaults to 10 — and accept the text-search cap of ten matches, which is
   hard-coded in the 1.18.21 `findText` handler with no request parameter to
   raise it. Surface that asymmetry in the result rather than implying the
   200-row limit table applies to text search. Strip `/file`'s absolute field. Do not call
   `/vcs/status`, `/file/status`, or `/find/symbol` from this surface. The
   pre-existing aggregate Diagnostics use of `/vcs/status` remains in
   `session_ops.go` and receives a regression test, but no workspace protocol
   message or UI depends on it.
3. Add a route-specific bounded JSON reader that reads at most 2,097,153 bytes
   and returns `result_too_large` when the response exceeds the 2 MiB envelope;
   do not inherit the general `apiAt` helper's 16 MiB ceiling and silently
   truncate. Reject any existing symlink component rather than resolving and
   trusting it; validate immediately before dispatch and validate all returned
   paths. Apply binary, size, count, sorting, and logging rules before and after
   HTTP. Treat upstream base64 binary content as `binary_content`; never return
   raw binary/base64. Document both the residual concurrent-local-filesystem
   race and that upstream text content is `.trim()` output, making this a
   bounded viewer rather than a byte-exact file API.
4. Add owner-checked WS handlers and 30-second read/search timeouts.
5. Add a session workspace sheet with directory/file indicators, directory
   navigation, a monospaced text view, and one search box with text/file
   selector. Do not render VCS badges from `/vcs/status`.
   No edit/save/apply/terminal affordance is present.
6. Add and advertise `session_capabilities.workspace_read` only for a live
   `WorkspaceSession`. Keep it false for other providers and hide the sheet
   when false.
7. Enforce the 90.0% new-file floor on
   `apps/mobile/lib/features/chat/workspace_sheet.dart` in this phase's own
   check, by passing it to `scripts/coverage-delta.sh floor --new-dart-file`.
   Record the path in the phase todo so 0113 C8 can add it to `make
   coverage-check` when that target is created.

### Verification

```bash
go test -race ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol ./internal/ws
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/workspace_sheet_test.dart test/workspace_protocol_test.dart)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveWorkspaceReadOnly'
```

### Acceptance

* Relative in-root text operations succeed and are deterministically capped;
  an upstream response one byte over a route limit fails rather than returning
  partial JSON.
* Absolute, traversal, every existing symlink component, NUL, binary,
  oversized, cross-session, and non-owner requests fail with the specified
  code and leak no content; the documented local-race assumption is not
  misrepresented as race-proof confinement.
* Route spies prove no write/apply/config endpoint can be reached.
* Route spies also prove `/vcs/status`, `/file/status`, and `/find/symbol` are
  never called by workspace handlers; the existing Diagnostics test separately
  pins its compatibility-only aggregate `/vcs/status` call.
* The phone has no workspace mutation affordance.
* P6 unit tests exercise every path/size/count boundary, malformed/oversize
  JSON, cancellation/timeout, symlink components, ownership, sorting, residual
  race disclosure, existing VCS aggregate compatibility, and excluded-route
  spies. All P6 deltas pass; no touched existing target regresses, the new workspace sheet is at least 90%
  line-covered, and one touched target strictly improves.

### Commit

Precheck, format, stage P6 only, and run `git commit --no-edit`.

## P7 — Diagnostics and agent-mediated skill authoring

### Outcome

Diagnostics explain stable skill, language-service, formatter, and MCP state
without disclosing filesystem locations, server errors, secrets, or
configuration. The phone can ask OpenCode to author a project-local skill
through its normal agent/write/permission path, then explicitly recycle only
an idle project instance so OpenCode discovers the result.

### Files

* `internal/event/event.go` and `event_test.go`.
* `internal/provider/provider.go` and `provider_test.go`.
* `internal/provider/httpagent/httpagent.go`, `provider.go`, `provider_test.go`,
  `session.go`, and `session_test.go`.
* `internal/provider/opencode/session_ops.go`,
  `session_ops_test.go`, `http.go`, and create
  `diagnostics_surface_test.go` and `live_skill_refresh_test.go`.
* `internal/protocol/messages.go`, `messages_test.go`, `errors.go`, and
  `op_timeouts.json`.
* `internal/ws/server.go`, `server_session_handlers_test.go`, and
  `op_timeout_test.go`.
* `apps/mobile/lib/data/protocol/models.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`, and
  `apps/mobile/lib/features/chat/chat_screen.dart`.
* Create `apps/mobile/lib/features/chat/diagnostics_sheet.dart`,
  `skill_authoring_sheet.dart`, `apps/mobile/test/diagnostics_sheet_test.dart`,
  and `skill_authoring_test.dart`.
* `Makefile`.
* `docs/protocol-v1.md`.

### Steps

1. Extend diagnostics with bounded `skills`, `lsp`, and `formatters` while
   preserving VCS and MCP fields.
2. Map `GET /skill` to name/description only; never decode-forward location or
   content. Runtime and fixture tests assert that 1.18.21 exposes only GET for
   `/skill`.
3. Add “Create or update with agent” to the skills section. Collect a validated
   1–64 character name matching `^[a-z0-9]+(-[a-z0-9]+)*$`, a 1–1,024 character
   description, and a bounded intent of at most 4,096 characters. Compose an
   editable normal prompt that instructs OpenCode to use the built-in
   `customize-opencode` skill, write only
   `.opencode/skills/<name>/SKILL.md` in the current worktree, preserve an
   existing skill unless the requested change requires it, and report the
   resulting path and validation. Submit through the existing
   `session.prompt` operation.
4. Do not add a daemon skill-write API, raw Markdown editor, global-home path,
   or special permission bypass. OpenCode's ordinary write/edit/apply-patch and
   external-directory permissions remain authoritative. Advanced global or
   compatibility-directory authoring remains possible only through a user's
   ordinary free-form agent prompt.
5. Add `SkillRefreshSession.RefreshSkills` and
   `session.refresh_skills` with a 30-second timeout and
   `session_capabilities.skill_refresh`. The OpenCode implementation calls
   documented `POST /instance/dispose` exactly once with the URL-encoded value
   of `session.CWD()` in its `directory` query, then reloads `GET /skill` and
   `GET /command`. The phone receives `ok` and immediately re-requests
   diagnostics and the command catalog. The live test may create a skill file
   directly only inside its disposable temporary project to establish cache
   state; production code and the phone never receive that writer.
6. Add one provider-wide `sync.RWMutex` instance-operation gate to
   `httpagent.Provider`. Start holds a read lock across create/resume,
   registration, and initial replay; Prompt holds it across local state claim
   and the short `prompt_async` submission. P10 Shell holds it through its
   atomic local claim, then releases it before the blocking HTTP request; the
   claimed active state makes a racing refresh reject rather than wait for a
   long command. Close holds a read lock through closed-state marking and
   unregister. Refresh holds the write lock, normalizes target CWD exactly as
   OpenCode's directory instance key, and rejects before HTTP when any
   registered session with that CWD is active, prompt-in-flight, queued,
   permission-pending, or question-pending. Idle sessions in another
   CWD do not make the target busy, although the conservative provider-wide
   write lock briefly pauses new starts/prompts in every project. Hold the
   write lock through disposal and catalog reload so a new operation cannot
   win the race.
7. Never recycle automatically. Offer “Refresh skills” only after the
   authoring turn is complete, explain that it recycles the idle OpenCode
   project instance while preserving stored sessions, and require confirmation.
   A busy response leaves the skill file intact and tells the user to retry
   after all OpenCode work in that project is idle.
8. Map `GET /lsp` to name/status and `GET /formatter` to
   name/enabled/extension-count only; discard roots, executable paths, and raw
   errors.
9. Normalize MCP states with an exact, total mapping from the closed 1.18.21
   `MCPStatus` union: `connected` → `connected`, `disabled` → `disabled`,
   `failed` → `failed`, `needs_auth` → `needs_auth`, and
   `needs_client_registration` → `needs_registration`. Any absent, empty, or
   unrecognized status maps to `unknown`; a future upstream member therefore
   degrades instead of leaking a raw string. `failed` and
   `needs_client_registration` carry a required upstream `error` field — drop
   it, along with URLs, headers, and every OAuth detail — and pin the drop with
   a fixture whose `error` contains a URL and a bearer token.
10. Add optional `EngineEventDialect` to `httpagent`: it receives only decoded
    global frames whose session ID is empty and returns whether a bounded
    diagnostics-change marker is required. OpenCode returns true only for
    canonical `mcp.tools.changed` and `lsp.updated`. `httpagent.Provider`
    coalesces bursts into at most one `diagnostics_changed` control event per
    registered session per 500 ms; mobile then re-requests sanitized
    diagnostics. The 1.18.21 Event union has no formatter/config/skill update
    event, so formatter and skill refresh are explicit/on-open. Never forward
    raw global-event payloads or the MCP server name.
11. Render VCS, skills, language services, formatters, and MCP in grouped,
    count-bounded sections.
12. Enforce the 90.0% new-file floor on
    `apps/mobile/lib/features/chat/diagnostics_sheet.dart` and
    `apps/mobile/lib/features/chat/skill_authoring_sheet.dart` by passing both to
    `scripts/coverage-delta.sh floor --new-dart-file`, and record them for 0113
    C8.

### Verification

```bash
go test -race ./internal/event ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol ./internal/ws
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/diagnostics_sheet_test.dart test/skill_authoring_test.dart)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveDiagnosticsSurface|TestLiveSkillDiscoveryRefresh'
```

### Acceptance

* Fixtures containing absolute roots, skill content, OAuth URLs, bearer
  headers, and MCP errors produce none of those strings on the wire.
* Diagnostics counts and normalized states are deterministic.
* The authoring affordance sends only a normal prompt; route/file spies prove
  mcremote itself never writes a skill file or calls an undocumented endpoint.
* Prompt-composer tests pin the project-local path, frontmatter constraints,
  bounded fields, existing-skill preservation instruction, and use of
  `customize-opencode`.
* Refresh refuses active, prompt-in-flight, queued, permission-pending,
  question-pending, and start/refresh race cases without calling OpenCode;
  close/refresh is serialized so disposal never observes a half-registered
  session.
* An idle live refresh calls `/instance/dispose` once, preserves the native
  session and its message/tool history, and makes the new skill visible through
  both `/skill` and `/command`.
* Sessionless MCP/LSP event bursts cause at most one marker and one diagnostics
  refresh per registered session in each debounce window; raw payload fields
  never cross the provider boundary.
* Idle sessions in another CWD do not cause `instance_busy`; concurrent work is
  paused only for the bounded duration of the provider-wide refresh write lock.
* P7 unit tests use fake clocks/servers to cover debounce boundaries, every busy
  state and start/prompt/shell/close refresh race, disposal/reload failures,
  sanitization, and prompt constraints without tokens. All P7 deltas pass;
  no touched existing target regresses, each new Dart production file is at least 90.0% line-covered, and one
  touched target strictly improves.

### Commit

Precheck, format, stage P7 only, and run `git commit --no-edit`.

## P8 — Disabled-by-default remote policy foundation

### Outcome

Operators can independently permit session sharing and direct shell while the
default daemon exposes neither mutation. No MCP lifecycle policy or endpoint is
added.

### Files

* `internal/config/config.go`, `load.go`, and `config_test.go`.
* `configs/config.example.yaml`, `configs/config.prod.example.yaml`,
  `internal/cli/service/defaults_mcremote.yaml`,
  `internal/cli/service/template_parity_test.go`, `docs/config.md`, and
  `README.md`.
* `internal/daemon/daemon.go` and `daemon_test.go`.
* `internal/provider/opencode/http.go`, `http_model_test.go`, and create
  `remote_policy_test.go`.

### Steps

1. Add exactly two `OpencodeProviderConfig` booleans, both default false:
   `allow_remote_share` and `allow_remote_shell`. Add matching
   `MCREMOTE_PROVIDERS_OPENCODE_ALLOW_REMOTE_SHARE` and
   `MCREMOTE_PROVIDERS_OPENCODE_ALLOW_REMOTE_SHELL` bindings, YAML examples,
   service defaults, template-parity assertions, and risk documentation.
2. Add the same two fields to `opencode.Config`; `NewHTTP` and
   `NewHTTPWithLogger` remain source-compatible because omitted Go struct
   fields are false. Copy the daemon config fields into the existing
   `opencode.NewHTTPWithLogger(opencode.Config{...})` construction in
   `internal/daemon/daemon.go`.
3. Store the immutable policy in the OpenCode dialect/session so P9 and P10
   can reject before HTTP and can advertise their capability only when both
   the operation and its policy bit are present. Do not log policy alongside
   commands, URLs, or provider response bodies.
4. Pin all four two-boolean configuration combinations. Enabling either flag
   must leave the other false; do not add an MCP policy key.

### Verification

```bash
go test -race ./internal/config ./internal/cli/service ./internal/daemon ./internal/provider/opencode
make pre-add-check FILES="internal/config/config.go internal/config/load.go internal/config/config_test.go internal/cli/service/template_parity_test.go internal/daemon/daemon.go internal/daemon/daemon_test.go internal/provider/opencode/http.go internal/provider/opencode/http_model_test.go internal/provider/opencode/remote_policy_test.go"
```

### Acceptance

* Fresh/default config has both bits false.
* Environment/YAML/template values agree and template parity passes.
* Each flag is independent and reaches the constructed OpenCode dialect
  without enabling a handler in this phase.
* Production code contains no MCP connect/disconnect path or policy.
* P8 unit tests cover defaults, YAML/environment precedence, all four boolean
  combinations, constructor wiring, and template parity. All P8 Go deltas
  pass, no touched package regresses, and one touched package strictly
  improves.

### Commit

Precheck and stage P8 files only, then run `git commit --no-edit`.

## P9 — Opt-in session share and unshare

### Outcome

An enabled owner can intentionally publish or revoke an OpenCode share, with a
clear external-disclosure warning and validated URL handling.

### Files

* `internal/event/event.go` and `event_test.go`.
* `internal/provider/provider.go` and `provider_test.go`.
* Create `internal/provider/opencode/control.go` and `control_test.go`.
* `internal/provider/httpagent/session.go` and `session_test.go`.
* `internal/protocol/messages.go`, `errors.go`, `messages_test.go`, and
  `op_timeouts.json`.
* `internal/ws/server.go`, `server_session_handlers_test.go`, and
  `op_timeout_test.go`.
* `apps/mobile/lib/data/protocol/models.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`,
  `apps/mobile/lib/features/chat/chat_screen.dart`, and create
  `apps/mobile/test/session_share_test.dart`.
* `docs/protocol-v1.md` and `docs/config.md`.

### Steps

1. Implement `ShareSession` using native session share metadata plus documented
   `POST /session/{id}/share` and `DELETE /session/{id}/share`. Honor an
   upstream `share: disabled` rejection; never modify OpenCode config or retry
   around it.
2. Add owner-checked `session.share_state`/result,
   `session.share`/result, and `session.unshare` handlers. Reject mutations while
   policy is false before HTTP; the read-only state request remains available.
   Use 30 seconds for state and 60 seconds for each mutation. Do not put the
   share URL in the session event/history ring.
3. Add `session_capabilities.share_state` whenever `ShareSession` is
   implemented. Add `session_capabilities.share` only when the interface exists
   and `allow_remote_share` is true. On create/resume, the mobile client requests
   current state when `share_state` is true; mutation capability gates controls,
   not the native shared/private badge.
4. Validate the result as a bounded HTTPS URL without userinfo or fragment.
   Persist no share URL in daemon config or logs.
5. Add a phone confirmation that explicitly says the transcript is synchronized
   to OpenCode's service and anyone with the link can access it. After success,
   offer open/copy/revoke. When mutation policy is false, display existing
   native shared state and the validated link but hide share/unshare controls.
6. Show an existing native share state on resume only after it passes the same
   validation. Never auto-share, auto-unshare, override upstream policy, or
   retry a share mutation.

### Verification

```bash
go test -race ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol ./internal/ws
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/session_share_test.dart)
```

Live share is an optional manual smoke against a disposable, non-sensitive
session because it writes an external service. It requires separate explicit
owner consent for that run; approval to execute this implementation plan is not
consent to publish. Record only pass/fail/status, never the URL or transcript.

### Acceptance

* A share/unshare request under disabled policy makes zero OpenCode mutation
  requests; ordinary create/resume may still read native share state.
* Cancelled phone confirmation makes zero OpenCode requests.
* Enabled share accepts only HTTPS, exposes no URL in logs, and unshare removes
  the UI state.
* Existing valid native share state remains visible when mutation policy is
  false, and upstream `share: disabled` is never overridden.
* Errors do not cause automatic retry or duplicate publishing.
* P9 unit tests cover policy/ownership/timeout/cancellation, URL boundary and
  redaction, upstream-disabled/auto-shared state, old-client compatibility, and
  no-history persistence. All P9 Go/Dart deltas pass, every touched existing
  target regresses, and one touched executable target strictly improves.

### Commit

Precheck, format, stage P9 only, and run `git commit --no-edit`.

## P10 — Opt-in direct shell

### Outcome

An explicitly enabled owner can run one foreground shell command in the
session CWD, with strong acknowledgement that this bypasses model tool
permissions and with output represented by normal bounded tool events.

### Files

* `internal/event/event.go` and `event_test.go`.
* `internal/provider/provider.go` and `provider_test.go`.
* `internal/provider/opencode/control.go`, `control_test.go`,
  `http.go`, and `live_tool_stream_test.go`.
* `internal/provider/httpagent/session.go` and `session_test.go`.
* `internal/protocol/messages.go`, `errors.go`,
  `op_timeouts.json`, and `messages_test.go`.
* `internal/ws/server.go`, `server_session_handlers_test.go`, and
  `op_timeout_test.go`.
* `apps/mobile/lib/data/protocol/models.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`,
  `apps/mobile/lib/features/chat/chat_screen.dart`, and create
  `apps/mobile/lib/features/chat/shell_command_sheet.dart`.
* Create `apps/mobile/test/shell_command_test.dart`.
* `Makefile`.
* `docs/protocol-v1.md` and `docs/config.md`.

### Steps

1. Implement `ShellSession` against documented
   `POST /session/{id}/shell` with `{agent, model, command}` derived from the
   active session; do not accept phone-provided environment, CWD, agent, model,
   or background settings. Resolve the agent through existing
   `sessionModes`/`currentMode`; translate synthetic `auto` to
   `normalAgentID`, require the result to be a visible primary agent, and use
   the existing `resolveModel` provider/model pair. This is the same build/first
   non-plan fallback already advertised by the provider and never sends `auto`
   as an upstream agent ID.
2. Reject unless policy is enabled and command is valid UTF-8, non-empty,
   NUL-free, and at most 8,192 bytes. Do not claim to parse or constrain shell
   semantics: a command can create persistent filesystem/network effects or
   descendants, and timeout cannot roll those effects back.
3. Add `session_capabilities.shell`; emit it true only when `ShellSession` is
   implemented and `allow_remote_shell` is true.
4. Add owner-checked `session.shell` with a 30-minute timeout. Under P7's read
   gate, atomically claim the same per-session turn slot used by Prompt before
   submission; reject active/prompt-in-flight/queued/permission/question cases.
   When the blocking HTTP call returns, end the local turn exactly once (guarded
   against an upstream idle that already ended it); on failure emit sanitized
   error/idle state. Do not retry on timeout or disconnect.
5. Require the phone to show the exact command in a non-editable confirmation:
   “Runs directly on the host in this session's working directory and bypasses
   model tool permissions. The command and output are recorded in the OpenCode
   session. Host effects may persist after timeout.” Submission requires a
   second deliberate tap.
6. Treat global SSE as the canonical transcript/output path. Pass `out=nil` to
   the blocking HTTP call and do not map its response. Consume OpenCode's one
   synthetic user message and shell tool part through the P4 mapper and
   existing 8,000-character visible-output snapshots; do not synthesize a
   second `!<command>` row and never log the command.
7. Do not add an interactive terminal, stdin, PTY, environment editor,
   mcremote-owned command-history feature, automatic retry, or concurrent shell
   requests. OpenCode's own synthetic message remains native session history.
8. Enforce the 90.0% new-file floor on
   `apps/mobile/lib/features/chat/shell_command_sheet.dart` by passing it to
   `scripts/coverage-delta.sh floor --new-dart-file`, and record it for 0113
   C8.

### Verification

```bash
go test -race ./internal/provider/opencode ./internal/provider/httpagent ./internal/protocol ./internal/ws
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test test/shell_command_test.dart)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveDirectShell'
```

The live test runs only `printf mcremote-shell-ok` in a temporary project and
asserts start/running/completed tool state and exact output. It does not invoke
a model or spend tokens.

### Acceptance

* Disabled, non-owner, cancelled, invalid, and concurrently submitted requests
  never spawn a command.
* The command uses the session CWD, resolved visible primary agent, and active
  model; build/plan/auto/custom-primary fixtures pin exact selection and reject
  an empty/subagent/hidden result. It produces bounded normal tool events
  exactly once and completes without a model turn.
* The HTTP response cannot create a second transcript/tool event, and upstream
  submission failure returns the local session to idle without retry.
* Timeout/disconnect never retries. No command or output appears in daemon logs,
  while the documented OpenCode/phone transcript history remains visible.
* There is no mcremote terminal, PTY, stdin, environment, or background-job
  control surface; residual shell effects are explicitly disclosed, not
  represented as contained.
* P10 unit tests cover policy/ownership/validation limits, agent/model
  resolution, turn-claim races, blocking success/error/timeout/disconnect,
  upstream-idle deduplication, and response-body non-mapping. All P10 Go/Dart
  deltas pass; no touched existing target regresses, the new shell sheet is at least 90% line-covered, and one
  touched target strictly improves.

### Commit

Precheck, format, stage P10 only, and run `git commit --no-edit`.

## P11 — Full regression, documentation, and rollout

### Outcome

The complete stable-parity set is tested, documented, reviewable, and safe to
enable incrementally.

### Files

* `docs/0021-MADR-opencode-http-api-coverage.md`,
  `docs/0112-MADR-opencode-1.18.21-surface-parity.md`,
  `docs/protocol-v1.md`, `docs/config.md`, and `README.md`.

Any source or test correction discovered by the commands below is outside P11:
stop, amend the relevant earlier phase and MADR facts, obtain re-approval, then
make the correction in that phase. There is no catch-all implementation scope.

### Steps

1. Update the coverage matrix with exact route, provider interface, phone
   operation, flag, fixture, and live-test ownership for every admitted surface.
2. Document default-off control risk, confirmation, limits, error codes, and
   the fact that direct skill/reference/plugin/MCP configuration remains
   engine-owned while skill authoring is agent-mediated.
3. Run all unit, race, lint, formatting, mobile, and protocol-doc coverage.
   Capture one final default-tag snapshot for every package in the coverage
   table, every touched Dart production file, and the full Flutter application;
   compare it with P0's sanitized baseline. For every target this plan touched it
   must reject a lower exact coverage fraction. This cumulative check is in
   addition to, not a replacement for, each phase-relative delta. Absolute
   floors are 0113's gate.
4. Run no-token OpenCode live gates first, then the existing token-bearing
   OpenCode core/attachment/variant suite once against 1.18.21. The deterministic
   skill-authoring gate is the composed prompt/route/file-spy/idle-refresh
   suite from P7. A model-mediated authoring turn may be run in isolated
   XDG/project state as an informational smoke and reported as such; model
   refusal or failure to choose a write tool does not fail the release contract.
5. Verify default config and each single flag independently; enabling one must
   not enable the other.
6. Append actual observed results and any deviations to the MADR More
   Information section. The MADR is already `accepted` and this plan `approved`
   as of 2026-08-22; P11 does not change either status. Record the completed
   rollout as an implementation note and let the owner decide any further
   status change — do not infer anything from test success.

### Verification

```bash
make test
go test -race ./...
make pre-add-check
coverage_accept_dir=$(mktemp -d /tmp/mcremote-0112-accept-coverage.XXXXXX)
trap 'rm -r "$coverage_accept_dir"' EXIT
scripts/coverage-snapshot.sh --output "$coverage_accept_dir/final" --go ./internal/provider ./internal/provider/opencode ./internal/provider/httpagent ./internal/event ./internal/picker ./internal/protocol ./internal/session ./internal/ws ./internal/config ./internal/daemon ./internal/cli/service --flutter apps/mobile
scripts/coverage-delta.sh baseline --baseline-json internal/provider/opencode/testdata/surface-1.18.21/unit-coverage-baseline.json --after "$coverage_accept_dir/final" --minimum 0 --opencode-floor 0 --go ./internal/provider ./internal/provider/opencode ./internal/provider/httpagent ./internal/event ./internal/picker ./internal/protocol ./internal/session ./internal/ws ./internal/config ./internal/daemon ./internal/cli/service --dart-root apps/mobile
(cd apps/mobile && dart format --output=none --set-exit-if-changed .)
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test)
go test -tags live_opencode ./internal/provider/opencode -run 'TestLiveVersionAndStableSurface|TestLiveDiscovery|TestLiveWorkspaceReadOnly|TestLiveDiagnosticsSurface|TestLiveSkillDiscoveryRefresh|TestLiveDirectShell'
make live-opencode
rg -n '(/experimental/|/api/)' internal/provider/opencode
! rg -n 'ConfigOption\{[^}]*ID:\s*"variant"' internal/provider/opencode
```

For the first `rg`, the only allowed production match is the pre-existing proven
`/api/session/{id}/model` route and explicit rejection/test strings. Any other
production route fails acceptance. The second, negated `rg` must find no match:
per MADR 0112 A14 reasoning effort is carried by `ThinkingLevels`/
`ThinkingSession`, and a `variant` session-config option would be the duplicate
control that decision forbids.

### Acceptance

* All commands pass with OpenCode exactly 1.18.21.
* Default configuration has no share or direct-shell UI or reachable mutation;
  no MCP lifecycle handler exists under any configuration.
* Every admitted route has fixture and live coverage except share/unshare,
  whose live external-service smoke is run only with separate per-run consent;
  every new message/event is documented and doc coverage passes.
* No experimental, undocumented, secret-bearing, daemon-owned arbitrary-write,
  or broad v2 dependency exists.
* Existing OpenCode authentication, session lifecycle, commands/`/init`,
  agents/modes, permissions/questions, todos, diffs, fork, revert/redo,
  compact, model switch, subagent tree, cancel, replay, and generic tool tests
  remain green.
* Deterministic tests prove the phone-composed authoring prompt uses normal
  OpenCode tools/permissions and that guarded refresh discovers a valid
  project-local skill without losing session history. Any model-mediated smoke
  is labeled informational.
* Every target this plan touched meets or exceeds its exact P0 fraction, every
  P1–P10 production phase has a passing delta, and all new Dart production files meet 90.0% line coverage. Absolute
  floors, including the 85.0% OpenCode floor, are confirmed by 0113, not here.
* Every changed behavior has co-committed unit/widget assertions for its normal,
  error, boundary, compatibility, and applicable concurrency paths; no test is
  deferred to P11 or replaced by a live-only assertion.
* Other providers pass their full regression suites without implementing any
  new optional interface.

### Commit and handoff

Run final pre-add checks, stage only P11 documentation changes, and
run `git commit --no-edit`. Present:

* the per-phase commit list;
* exact verification command results;
* exact per-phase and cumulative covered/total/uncovered coverage summaries;
* the required CI coverage-job configuration and final scoped Dart-file list;
* if the owner separately authorized a push and remote CI ran, its result;
  otherwise state explicitly that no remote CI run occurred;
* live tests run and any token/external-service effects;
* the default-off flag states;
* known limitations from the exclusions above; and
* any uncommitted/unrelated user changes left untouched.

Do not push, tag, publish a release, enable a flag in a live config, or change
the MADR status without an explicit owner request in that turn.

## Rollback strategy

Rollback follows phase boundaries in reverse order:

1. Disable both remote-control flags first; this immediately removes share and
   shell reachability without reverting read-only parity.
2. Revert P10, P9, and P8 independently if a control surface is defective.
3. Hide workspace/diagnostics/artifact UI by optional capability absence, then
   revert the matching daemon phase.
4. Reverting P7 removes the authoring affordance and idle-refresh operation but
   never deletes a skill the agent already wrote; that project file remains a
   normal user-controlled repository change.
5. Revert additive protocol/event fields only after its mobile consumer is
   reverted; older clients already ignore additive fields.
6. Revert P2 native/project discovery without touching ordinary session create.
7. Revert P1's KnownGood constant/default/tool corrections only if runtime
   evidence disproves 1.18.21; retain the 1.18.0 minimum and record the reason
   in the MADR.
8. Retain a reverted phase's behavioral regression tests wherever they still
   describe surviving behavior. Never lower a coverage floor as a rollback.

Provider-native shares already published must be explicitly unshared before P9
rollback when the user requests removal. Reverting code alone cannot revoke an
external share. Shell commands and their host effects are not reversible by
software rollback; this is why the feature is default-off and explicitly
confirmed.

## Definition of done

The plan is complete only when every P0 and P1–P11 acceptance item passes,
every phase has its own verified local commit, the final tree contains no
unplanned product change, the coverage matrix and protocol docs match runtime
behavior, the 90.0% new-Dart floor and every per-phase Go/Flutter delta pass,
and the owner receives the handoff above. A production change without its unit
tests, or one that regresses a target it touched, is incomplete even when live
tests pass; it is not deferred, relabeled experimental, or silently omitted.
Closing pre-existing debt to an absolute floor is 0113's definition of done, not
this plan's.

## P12 — Composer action row below the input (amendment, 2026-08-25)

Executes the [0112-MADR amendment of
2026-08-25](./0112-MADR-opencode-1.18.21-surface-parity.md#amendment--2026-08-25-composer-action-row-moves-below-the-input),
decisions D1–D6. Dart-only; no Go, protocol, capability, or config change.

### Outcome

The prompt field spans the full composer width on every supported device. The
composer's action icons sit in one left-aligned row beneath it. The
`open-diagnostics` icon is gone from the composer and `DiagnosticsSheet` —
with its skills refresh and skill-authoring entry point — is reachable from
the session menu, whose two diagnostics-shaped entries are named apart.

### Files

* `apps/mobile/lib/features/chat/chat_screen.dart` (edit).
* `apps/mobile/test/composer_layout_test.dart` (create).
* `apps/mobile/test/audio_attachment_test.dart` — **edited**. Two cases drove
  the sheet through the removed `open-diagnostics` icon; they now open it from
  the session menu. The `attach-audio` cases needed no change, as predicted.
* `apps/mobile/test/chat_render_test.dart` and
  `apps/mobile/test/chat_end_session_navigation_test.dart` — **edited, added
  to this phase mid-execution (deviation, 2026-08-25).** Both assert on the
  menu label `'Session diagnostics'`, which D5 renames to
  `'Repository & MCP status'`. Updating them is the direct consequence of an
  approved decision, not new scope; no assertion's intent changed.
* ~~`Makefile`~~ — not modified; see the deviation on step 9.

No other file is in scope. `diagnostics_sheet.dart`,
`skill_authoring_sheet.dart`, `workspace_sheet.dart`, `question_sheet.dart`,
and every Go package are untouched.

### Steps

1. In `chat_screen.dart`, replace the composer `Row` at line 2833 with a
   `Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment:
   CrossAxisAlignment.stretch)` holding two children, keeping the existing
   `SafeArea` + `Padding(EdgeInsets.fromLTRB(12, 4, 12, 12))` wrapper.
2. First child — the input row: `Row([Expanded(TextField …), SizedBox(width:
   8), ValueListenableBuilder(send/queue/stop)])`. The `TextField` (including
   its `prefixIcon` slash-commands button and `suffixIcon` dictation button),
   the `AnimatedSwitcher`, and the tri-state button logic move across
   verbatim — no behavioural edit (D2, D3).
3. Second child — the action row: `Row(mainAxisAlignment:
   MainAxisAlignment.start)` carrying, in this order and each behind its
   existing gate, `canAttachImage` → `canAttachAudio` (`attach-audio`) →
   `canBrowseWorkspace` (`open-workspace`) → `canRunShell` (`open-shell`) →
   `canReadShare` (`open-share`). Every `ValueKey`, `tooltip`, icon, and
   `onPressed` (including the `busy`/`offline` enablement expressions) is
   carried over character-for-character (D1, D6).
4. Give the action row `key: const ValueKey('composer-actions')` so the new
   test can assert on it directly.
5. Delete the `open-diagnostics` `IconButton` (`chat_screen.dart:2873–2881`).
   Do **not** delete `_showDiagnosticsSheet`, `DiagnosticsSheet`, or
   `_showSkillAuthoringSheet` (D4).
6. Add a session-menu entry that opens the sheet: a `PopupMenuItem` with
   `value: 'engine-diagnostics'`, `leading: Icon(Icons.health_and_safety_outlined)`,
   `title: Text('Engine diagnostics & skills')`, placed immediately after the
   existing `showsDiagnostics` item, and wire
   `if (v == 'engine-diagnostics') unawaited(_showDiagnosticsSheet(canRefreshSkills));`
   into `onSelected` (`chat_screen.dart:2370`). It is offered unconditionally,
   matching the icon's current always-shown behaviour (D6). If
   `canRefreshSkills` is not in scope at the menu's build site, read it there
   with the same `ref.watch` used at `chat_screen.dart:2184` rather than
   threading it through state.
7. Relabel the existing `_viewDiagnostics` menu item from `'Session
   diagnostics'` to `'Repository & MCP status'` (`chat_screen.dart:2419`).
   Its `value: 'diagnostics'`, gate, and handler are unchanged (D5).
8. Create `apps/mobile/test/composer_layout_test.dart` covering: (a) with all
   capabilities on, `composer-actions` exists and contains the five expected
   keys/tooltips; (b) `open-diagnostics` is absent from the widget tree; (c)
   the `TextField`'s laid-out width exceeds half the 360dp test surface —
   the regression assertion, which fails against today's code; (d) tapping
   the menu's `'Engine diagnostics & skills'` opens `DiagnosticsSheet`; (e)
   with every capability off, the action row renders empty and no overflow is
   reported. Follow the harness already used by
   `test/audio_attachment_test.dart` for provider/capability overrides.
9. ~~Add `composer_layout_test.dart` to `NEW_DART_FILES` in the `Makefile`.~~
   **Not done — this step was wrong as written (deviation, 2026-08-25).**
   `NEW_DART_FILES` (`Makefile:489`) lists *production* files that must hold
   at least 90.0% line coverage, per its own header comment; it is consumed by
   `scripts/coverage-delta.sh floor --new-dart-file`. Feeding a test file into
   a production-coverage floor is a category error. No `Makefile` change is
   required: the new test raises coverage of
   `lib/features/chat/diagnostics_sheet.dart`, which is already listed.

### Verification

Run from `apps/mobile` unless noted:

* `dart format --output=none --set-exit-if-changed lib test`
* `flutter analyze` — zero new findings.
* `flutter test test/composer_layout_test.dart`
* `flutter test test/audio_attachment_test.dart` — must pass **unmodified**.
* `flutter test` — full suite, no regression, and no
  `RenderFlex overflowed` exception in the output.
* From the repo root: `make pre-add-check` on the staged files before the
  commit, per the global pre-commit rule.
* Manual check on a device or simulator: the prompt hint text
  `Prompt or /command…` is fully legible with all five action icons present.
  **Done 2026-08-25** on the iPhone 17e simulator (iOS 26.5, 390pt logical
  width — no available iOS device is as narrow as 360pt; the defect bit there
  too, since six icons plus the send button need ~344pt against a 366pt
  content box). Driven by a temporary preview entrypoint that pumped
  `ChatScreen` against stubbed providers, so no daemon pairing was involved;
  the file was deleted afterwards and the tree verified clean. Confirmed by
  screenshot: the field spans the row with its hint fully legible, the send
  button sits beside it, exactly five action icons render on the row beneath,
  and no diagnostics icon is present. A second capture with `DiagnosticsSheet`
  open confirms the step-10 header fix on device — "Skills (1)" and "Refresh
  skills" share the first line, "Create or update with agent" flows to a
  second, and nothing clips.
* **Also verified at exactly 360dp on Android (2026-08-25).** AVD
  `mcremote_test`, 1080x2400; `adb shell wm density 480` makes the logical
  width exactly 1080/3 = 360dp, the width the defect was reported at, which no
  iOS simulator offers. Both captures reproduce the iOS result: the composer
  field spans the row with five action icons beneath and no diagnostics icon,
  and the `DiagnosticsSheet` header wraps to two lines with nothing clipped —
  at the very width where the old `Row` overflowed by 298px. Density was reset
  and the emulator shut down afterwards.

### Coverage result (measured 2026-08-25, commit `d677d68`)

Per [0113](./0113-MADR-preexisting-unit-coverage-debt.md) E1/E2/E5 the
absolute 80.0% application floor belongs to 0113, not here; P12's obligations
are the 90.0% new-Dart floor and non-regression on every target it touched.
Captured with `scripts/coverage-snapshot.sh`, the "before" taken from a
detached worktree at `8f2130f` (P12's parent):

| Target | Before | After | Verdict |
| --- | --- | --- | --- |
| `lib/features/chat/chat_screen.dart` | 1073/1548 (69.3152%) | 1077/1552 (69.3943%) | rose; uncovered unchanged at 475 |
| `lib/features/chat/diagnostics_sheet.dart` | 105/105 (100%) | 104/104 (100%) | held at 100% across the `Row`→`Wrap` change |
| `<application>` | 10748/14145 (75.9844%) | 10751/14148 (75.9895%) | rose; uncovered unchanged at 3397 |

`scripts/coverage-delta.sh phase` reports `"improved": true` with no target
regressed. The 90.0% new-Dart floor passes on all five `NEW_DART_FILES`:
`workspace_sheet` 100%, `diagnostics_sheet` 100%, `skill_authoring_sheet`
100%, `session_share_sheet` 98.8235%, `shell_command_sheet` 100%. The
`below_floor` statuses that remain on `<application>` and `chat_screen.dart`
are the pre-existing debt 0113 owns; P12 neither caused nor worsened them.

### Acceptance criteria

* The prompt `TextField` occupies the full composer content width minus the
  send button and its 8dp gap, at 320dp, 360dp, and 430dp widths.
* Exactly one row of action icons renders beneath the input, left-aligned.
* `open-diagnostics` no longer exists in the composer; `DiagnosticsSheet`,
  "Refresh skills", and the skill-authoring composer are all reachable from
  the session menu in at most two taps.
* The session menu shows "Repository & MCP status" and "Engine diagnostics &
  skills" as distinct entries.
* No capability gate, protocol message, Go file, or config key changed.
* New Dart coverage stays at or above the 90.0% floor.

### Step 10 — added mid-execution 2026-08-25 by owner direction

P12's new widget test was the first to render `DiagnosticsSheet` with both of
its skills-section buttons live, and it exposed a **pre-existing** overflow:
`_section`'s header `Row`
(`apps/mobile/lib/features/chat/diagnostics_sheet.dart:223`) lays out
`Expanded(Text(title))` beside `extra` ("Refresh skills") and `trailing`
("Create or update with agent"). The two `TextButton`s do not fit next to the
title — `RenderFlex overflowed by 298 pixels` at a 360dp phone width and still
by 18 pixels at 900dp, because a modal bottom sheet caps its own width at
640dp. No surface is wide enough, so the header overflows for every user whose
session advertises `skillRefresh`.

This was reported as out of scope. The owner directed: *"do not scope around
it, fix it."* It is therefore in scope for P12.

* Replace that header `Row` with a `Wrap` (`spaceBetween`, centre cross-axis
  alignment, 8dp spacing) so the title and its buttons sit on one line when
  they fit and flow onto a second when they do not. Sections with no buttons
  render identically to today.
* Extend `composer_layout_test.dart` with a case that opens the sheet at
  360dp with `skillRefresh` on and asserts `tester.takeException()` is null.
* Files added to this phase's scope:
  `apps/mobile/lib/features/chat/diagnostics_sheet.dart`.

### Out of scope

Restyling the composer, changing icon glyphs, adding a density override,
touching the staged-attachment strip, any further `DiagnosticsSheet` change
beyond the header-overflow fix in step 10, and the four pre-P3 coverage
regressions owned by 0113. Anything found mid-execution outside this list
stops and waits for an amended, re-approved plan.

### Rollback

Single-commit phase; `git revert` of that commit restores the current
composer. No migration, no persisted state, no protocol surface involved.
