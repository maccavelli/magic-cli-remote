---
status: accepted
date: 2026-08-20
associated-madr: "0108-MADR-kilo-7.4.23-surface-parity.md"
owner: Implementer
target-milestone: 2026-08-20
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Verify and pin Kilo known-good to 7.4.23

## Associated Decision Record

[0108-MADR-kilo-7.4.23-surface-parity.md](./0108-MADR-kilo-7.4.23-surface-parity.md)

This is the implementation plan for MADR 0108. It is not a second decision
record. Its scope is limited to the decisions locked by 0108 D1-D12. The
owner must explicitly approve execution before any source, test, fixture,
Makefile, or historical-record changes below begin.

## Goal

Make Kilo 7.4.23 the provider's known-good release only after reproducible
checks establish both wire compatibility and the release's changed Ask/Plan
permission behavior. Preserve a sanitized fixture corpus so the pin can be
re-derived without trusting prose or a model-dependent turn.

Success means:

* `KnownGoodVersion == "7.4.23"` and health records 7.4.23 without a drift
  warning.
* Runtime `/doc` has 255 paths, 680 schemas, and 119 canonical Event types.
* The 7.4.22 to 7.4.23 path/schema/Event diffs are exactly those in 0108 D2-D4.
* Deterministic, no-model live tests pin the installed CLI version, health,
  OpenAPI, ACP initialize response, and read-only agent boundaries.
* Existing unit/race tests and the token-bearing `live_kilo` suite pass.
* Historical records carry visible errata instead of silently changing their
  original evidence.

## Scope

### In scope

* The known-good version constant, its comment, and version-specific tests.
* One new `live_kilo` test file for no-model surface and permission checks.
* A sanitized 7.4.23 probe corpus with exact counts and set comparisons.
* Errata in MADRs 0075 and 0088.
* Correction of the inaccurate `live-kilo` Makefile comment.
* Verification and one commit at the end of each mutating phase.

### Out of scope

* Changes to HTTP routes, SSE decode/filter logic, session rendering,
  command dispatch, model catalogs, or permission reply payloads.
* Rendering `session.next.*`, mapping `invalid`, or adopting ACP as the host
  transport.
* Sandbox, worktree, PTY, Agent Manager, Cloud, `kilo run`, `kilo pr`, STT,
  and IDE/webview behavior.
* Fetching or modifying `/Users/saxsmith/gitrepos/kilocode`; that checkout is
  read-only evidence for this plan.
* Publishing commits or pushing a branch.

If a live check contradicts 0108's locked delta or requires any out-of-scope
runtime change, stop. Amend the 0108 pair, present the new evidence, and wait
for renewed approval.

## Evidence Baseline and Preconditions

The assessment established these immutable comparison points:

| Item | Value |
| --- | --- |
| Kilo 7.4.22 source tag | `67cda85c94937a7dfad68993bdddc76cb0353c36` |
| Published tag parent / 7.4.23 source boundary | `0d5d334480bc2093a12b27a34b03cac88cf33422` |
| Published 7.4.23 tag commit | `40fa10e50a75c4887978d892520d1246515413bf` |
| Local checkout HEAD (not the release boundary) | `67cd629f3c5212fa5326c5ea0b86b68db21bc90a` |
| Installed executable | `/opt/homebrew/bin/kilo` |
| Installed package version | `7.4.23` |
| Expected OpenAPI counts | 255 paths; 680 schemas; 119 Event types |
| Expected removed path | `/kilocode/agent/requirements` |
| Expected removed schema | `AgentRequirementResult` |
| Expected Event set delta | empty |
| Successful isolated HTTP retry | health 7.4.23; 255 paths; 680 schemas; 210 Event refs; 119 unique Event types |
| Successful isolated ACP retry | protocol 1; expected capabilities; agent version 7.4.23; exit 0 |
| Successful isolated agent retry | 10 native agents; five visible primary, three hidden primary, two subagents |
| Successful isolated command retry | stable built-ins `init`, `review`, `resume-claude`, `resume-codex`; environment-backed `kilo-config` excluded |
| Successful isolated permission retry | controlled Code/Ask/Plan rules match D5 |

The published tag's parent is the functional source commit above; the tag
commit contains release/version generation. The local checkout continued
through an additional VS Code-only series before its separate local release
commit, so no comparison may use mutable `HEAD`. The runtime probe remains
authoritative for the shipped package.

The 2026-08-20 unrestricted retry started the installed CLI with the isolated
environment below. `kilo serve --port 0` selected `127.0.0.1:4096`, health
returned `{"healthy":true,"version":"7.4.23"}`, and runtime path, schema,
and canonical Event-type sets exactly matched source boundary `0d5d334480`.
The process stopped cleanly. The exact ACP initialize request in Phase 1 also
returned the expected result and exited 0; one-time migration messages
preceded the JSON-RPC response on stdout. These observations close the manual
probe questions but do not replace the committed automated gates.

Execution prerequisites:

* Run in a normal host environment where macOS FSEvents can initialize. The
  unrestricted retry succeeded. The earlier restricted-sandbox `ServeError`
  is environment history, not product evidence; any recurrence during
  execution is a test failure to diagnose, not a permitted skip.
* `kilo` on `PATH` must resolve to the installed 7.4.23 package.
* `git`, `go`, `curl`, and `jq` must be available. A usable configured model
  is required only for the final existing `make live-kilo` suite.
* All no-model subprocesses must use a fresh temporary home, all `XDG_*`
  directories, `KILO_CONFIG_CONTENT`, `KILO_DISABLE_PROJECT_CONFIG=1`,
  `KILO_DISABLE_AUTOUPDATE=1`, `KILO_DISABLE_AUTOCOMPACT=1`,
  `KILO_DISABLE_MODELS_FETCH=1`, `KILO_AUTH_CONTENT={}`, and `KILO_PURE=1`.
  This prevents account, project, plugin, update, and models.dev state from
  affecting the result.

## Affected Files

| File | Planned change |
| --- | --- |
| `docs/0108-MADR-kilo-7.4.23-surface-parity.md` | On approval, change `status` from `proposed` to `accepted`; preserve the completed evidence. |
| `docs/0108-PLAN-kilo-7.4.23-surface-parity.md` | On approval, change `status` from `proposed` to `accepted`; execute this scope without silent expansion. |
| `internal/provider/kilo/version.go` | Move pin and health-comment example to 7.4.23; cite 0108. |
| `internal/provider/kilo/dialect_test.go` | Rename/update the two 7.4.22 version tests. |
| `internal/provider/kilo/live_surface_test.go` | Add no-model version/OpenAPI/ACP/permission live gates. |
| `docs/kilo-spike-7.4.23/README.md` | Record provenance, commands, sanitization, exact deltas, and environment-dependent observations. |
| `docs/kilo-spike-7.4.23/openapi-paths.txt` | Sorted 255-path runtime snapshot. |
| `docs/kilo-spike-7.4.23/event-types.txt` | Sorted 119-type canonical Event snapshot. |
| `docs/kilo-spike-7.4.23/openapi-summary.json` | Counts and exact removed/added sets. |
| `docs/kilo-spike-7.4.23/agents-summary.json` | Stable agent identity/mode/visibility fields only. |
| `docs/kilo-spike-7.4.23/agent-permission-summary.json` | Controlled Code/Ask/Plan boundary results only. |
| `docs/kilo-spike-7.4.23/commands.json` | Stable built-in command subset only. |
| `docs/kilo-spike-7.4.23/acp-initialize.json` | Sanitized ACP initialize result. |
| `docs/0075-MADR-kilo-cli-provider.md` | Append an erratum moving the known-good pin to 0108. |
| `docs/0088-MADR-kilo-7.4.22-surface-parity.md` | Append a pin erratum and Event-extraction clarification; preserve historical text. |
| `Makefile` | Correct the `live-kilo` comment to distinguish required catalog checks from model-dependent skips. |

No other file is authorized by this plan.

## Execution Readiness

No technical or product choice remains open. The installed version, runtime
health/OpenAPI contract, exact source sets, Ask/Plan permission behavior, ACP
request/response shape, ACP mixed stdout, and port-zero behavior have all
been observed or pinned to source. The only remaining gate is owner
authorization to execute. Asking whether the plan is ready does not itself
grant that authorization.

At the start of an approved execution, Phase 0 changes both 0108 artifacts
from `proposed` to `accepted` and commits them alone. No source, test, fixture,
Makefile, or historical-record mutation occurs before that decision-artifact
commit.

## Deterministic Probe Contract

### Canonical OpenAPI extraction

Use fresh temporary files and source objects, not the old prose counts:

```bash
probe_root="$(mktemp -d)"
kilo_source=/Users/saxsmith/gitrepos/kilocode

git -C "$kilo_source" show \
  67cda85c94937a7dfad68993bdddc76cb0353c36:packages/sdk/openapi.json \
  > "$probe_root/openapi-7.4.22.json"
git -C "$kilo_source" show \
  0d5d334480bc2093a12b27a34b03cac88cf33422:packages/sdk/openapi.json \
  > "$probe_root/openapi-7.4.23-source.json"

jq -r '.paths | keys[]' "$probe_root/openapi-7.4.22.json" \
  | LC_ALL=C sort -u > "$probe_root/paths-7.4.22.txt"
jq -r '.paths | keys[]' "$probe_root/openapi-7.4.23-source.json" \
  | LC_ALL=C sort -u > "$probe_root/paths-7.4.23-source.txt"
jq -r '.components.schemas | keys[]' "$probe_root/openapi-7.4.22.json" \
  | LC_ALL=C sort -u > "$probe_root/schemas-7.4.22.txt"
jq -r '.components.schemas | keys[]' "$probe_root/openapi-7.4.23-source.json" \
  | LC_ALL=C sort -u > "$probe_root/schemas-7.4.23-source.txt"
```

For each OpenAPI document, canonical Event extraction must resolve the refs
in `components.schemas.Event.anyOf` and read only each referenced schema's
top-level discriminator. A recursive scan is forbidden because it also finds
nested, non-event `type` enums such as `recalled`, `saved`, and `skipped`:

```bash
jq -r '
  . as $doc
  | [.components.schemas.Event.anyOf[]."$ref"
     | split("/")[-1] as $name
     | $doc.components.schemas[$name].properties.type.enum[]]
  | unique[]
' "$probe_root/openapi-7.4.23-source.json" \
  | LC_ALL=C sort -u > "$probe_root/events-7.4.23-source.txt"
```

Run the same command for 7.4.22 and the runtime `/doc`. The assertions are:

```bash
test "$(wc -l < "$probe_root/paths-7.4.23-runtime.txt")" -eq 255
test "$(wc -l < "$probe_root/schemas-7.4.23-runtime.txt")" -eq 680
test "$(wc -l < "$probe_root/events-7.4.23-runtime.txt")" -eq 119
test "$(comm -23 "$probe_root/paths-7.4.22.txt" "$probe_root/paths-7.4.23-runtime.txt")" = "/kilocode/agent/requirements"
test -z "$(comm -13 "$probe_root/paths-7.4.22.txt" "$probe_root/paths-7.4.23-runtime.txt")"
test "$(comm -23 "$probe_root/schemas-7.4.22.txt" "$probe_root/schemas-7.4.23-runtime.txt")" = "AgentRequirementResult"
test -z "$(comm -13 "$probe_root/schemas-7.4.22.txt" "$probe_root/schemas-7.4.23-runtime.txt")"
cmp "$probe_root/events-7.4.22.txt" "$probe_root/events-7.4.23-runtime.txt"
cmp "$probe_root/events-7.4.23-source.txt" "$probe_root/events-7.4.23-runtime.txt"
```

### Runtime isolation and lifecycle

The Go live helper must use `t.TempDir`, set all isolation variables listed
above, run from that temporary directory, start:

```text
kilo serve --hostname 127.0.0.1 --port 0 --pure
```

Kilo's `startWithPortFallback` (`packages/opencode/src/server/server.ts`)
defines port 0 as **prefer 4096, then ask the OS for a free port if 4096 is
unavailable**. Port 0 therefore does not promise an ephemeral port. The helper
must remove ANSI CSI sequences and a trailing carriage return from each
stdout line, match
`^kilo server listening on (http://127\.0\.0\.1:[0-9]+)$`, and use only the
captured URL; it must neither assume 4096 nor require a non-4096 port.

Allow at most 30 seconds for the listening line, 10 seconds for each HTTP
request, and 5 seconds for graceful process shutdown. Capture the last 64 KiB
of stdout and stderr for diagnostics. Cleanup must interrupt/cancel the
process, wait for it, and, if it survives the shutdown deadline, kill it and
fail the test. A normal interrupt exit after successful assertions is not a
test failure. Hard-coded expected ports, `kill %1`, shared
`/tmp/kilo-*.json` files, unbounded polling, and the user's real home/config
are forbidden.

### Stable versus environmental fixtures

The committed summaries may contain only stable fields:

* agents: name, mode, hidden flag, and native flag;
* commands: built-ins `init`, `review`, `resume-claude`, and `resume-codex`;
* permissions: named exact actions derived under
  `KILO_CONFIG_CONTENT={"permission":{"*":"allow"}}`;
* ACP: the matching initialize `result`, with no logs, startup chatter, or
  transport IDs.

Do not commit providers, model catalogs, credentials, auth methods beyond
their stable identifier, raw server logs, timestamps, absolute paths,
project/MCP/skill-backed commands, or the multi-megabyte raw `/doc`.

## Phased Implementation

### Phase 0: Accept and commit the decision artifacts

1. Confirm the owner has explicitly authorized execution in the current
   conversation. Without that authorization, stop here.
2. Change the 0108 MADR and PLAN frontmatter from `status: proposed` to
   `status: accepted`; keep the date `2026-08-20` unless approval occurs on a
   later date, in which case set both dates to the approval date.
3. Verify the pair's cross-links, proposed scope, and local-link targets.
4. Stage only the two 0108 files. Confirm the staged diff contains no source,
   test, fixture, Makefile, historical-record, or unrelated user file.
5. Commit with `git commit --no-edit`. Do not push.

Phase 0 verification:

```bash
rg -n '^status: accepted$' \
  docs/0108-MADR-kilo-7.4.23-surface-parity.md \
  docs/0108-PLAN-kilo-7.4.23-surface-parity.md
git diff --cached --check
git diff --cached --name-only
git commit --no-edit
```

Acceptance: both 0108 artifacts are tracked together in a documentation-only
commit, and the worktree contains no implementation mutation from Phase 1.

### Phase 1: Add reproducible fixtures and no-model live gates

1. Implement `internal/provider/kilo/live_surface_test.go` under the
   `live_kilo` build tag in package `kilo_test`. Define private helpers in that
   file for isolated environment construction, bounded subprocess execution,
   mixed-output scanning, HTTP startup, and cleanup; no existing live-test
   helper provides the full contract, so do not change another test file.
   Build each `exec.Cmd.Env` from `os.Environ()` after removing `HOME`,
   `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`,
   `KILO_TEST_HOME`, `KILO_CONFIG`,
   `KILO_CONFIG_DIR`, `KILO_CONFIG_CONTENT`, `KILO_AUTH_CONTENT`,
   `KILO_SERVER_USERNAME`, `KILO_SERVER_PASSWORD`,
   `KILO_DISABLE_PROJECT_CONFIG`, `KILO_DISABLE_AUTOUPDATE`,
   `KILO_DISABLE_AUTOCOMPACT`, `KILO_DISABLE_MODELS_FETCH`, and `KILO_PURE`.
   Append exactly the isolated values from Preconditions, preserve `PATH`,
   assign the command's `Dir` to the same `t.TempDir()`, and do not call
   `t.Parallel`.
2. Add `TestLiveKilo7423Surface`:
   * assert isolated `kilo --version` is exactly 7.4.23;
   * start `kilo serve --port 0`, parse Kilo's resolved loopback URL, and make
     no assertion about whether the chosen port is 4096 or ephemeral;
   * assert healthy=true and health version 7.4.23;
   * fetch `/doc`, apply the canonical extraction, and assert 255/680/119;
   * assert the removed path/schema are absent; and
   * assert these required session-loop discriminators are present:
     `message.updated`, `message.part.delta`, `message.part.updated`,
     `message.part.removed`, `permission.asked`, `permission.v2.asked`,
     `permission.replied`, `permission.v2.replied`,
     `question.asked`, `question.v2.asked`, `question.replied`,
     `question.v2.replied`, `question.rejected`, `question.v2.rejected`,
     `session.diff`, `session.created`, `session.updated`, `session.deleted`,
     `session.status`, `session.idle`, `session.turn.open`,
     `session.turn.close`, `session.error`, `session.next.prompted`, and
     `session.next.prompt.admitted`.
3. Add `TestLiveKilo7423ACPInitialize`. Start `kilo acp --hostname
   127.0.0.1 --port 0` with both `--cwd` and `exec.Cmd.Dir` set to that test's
   same `t.TempDir()` and with the same isolated environment. Send the
   following single-line ACP v1 JSON-RPC request
   over NDJSON, close stdin, and assert the matching response advertises
   protocol 1,
   `loadSession`, HTTP/SSE MCP, embedded-context/image prompts, and
   close/fork/list/resume session capabilities. Bound the whole subprocess
   and sanitize transport IDs before comparing.

   ```json
   {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"mcremote-0108-probe","version":"1"}}}
   ```

   Treat stdout as mixed because a fresh Kilo state can write non-JSON
   one-time migration messages before the response. Read stdout line by line;
   retain the last 64 KiB for diagnostics;
   ignore lines that are not JSON objects; and accept exactly one JSON-RPC
   object whose `jsonrpc` is `"2.0"` and whose numeric `id` is `1`. Fail on a
   matching `error`, a malformed matching response, more than one matching
   response, no matching response within 30 seconds, or a non-zero process
   exit. After receiving the result, close stdin and require exit 0 within 5
   seconds. Commit only the normalized `result` object.
4. Add `TestLiveKilo7423ReadOnlyAgentBoundaries`. Run
   `kilo debug agent code`, `ask`, and `plan` under a global `* = allow`
   configuration, parse JSON, and evaluate ordered permission rules using
   Kilo's last-match semantics. Assert:
   * Ask denies arbitrary bash, write, edit, task, and interactive terminal;
   * Plan denies arbitrary bash, write, edit, and interactive terminal;
   * Plan permits delegated tasks but still denies the general task agent;
   * safe read tools remain enabled; and
   * Code remains usable, including bash/edit/write/task registration.
5. Run those three tests before changing the pin. A skip is acceptable only
   when the installed executable is absent. Wrong version, startup failure,
   malformed JSON, changed counts/capabilities/rules, or an assertion mismatch
   is a failure, not a skip.
6. Generate and inspect all eight files under `docs/kilo-spike-7.4.23/` from
   the same successful runtime and source comparison. `openapi-summary.json`
   must include counts plus sorted `paths_removed`, `paths_added`,
   `schemas_removed`, `schemas_added`, `events_removed`, and `events_added`.
7. Validate fixture JSON with `jq empty`, counts with `wc -l`, sorted/unique
   text with `LC_ALL=C sort -cu`, and exact sets with the commands above.
8. Run the Go pre-add gate and the package race suite, then stage only Phase 1
   files and commit with `git commit --no-edit`.

Phase 1 verification:

```bash
go test -tags live_kilo ./internal/provider/kilo/ \
  -run 'TestLiveKilo7423(Surface|ACPInitialize|ReadOnlyAgentBoundaries)$' \
  -count=1 -timeout 180s -v
make pre-add-check FILES="internal/provider/kilo/live_surface_test.go"
go test -race ./internal/provider/kilo/
jq empty docs/kilo-spike-7.4.23/*.json
test "$(wc -l < docs/kilo-spike-7.4.23/openapi-paths.txt)" -eq 255
test "$(wc -l < docs/kilo-spike-7.4.23/event-types.txt)" -eq 119
LC_ALL=C sort -cu docs/kilo-spike-7.4.23/openapi-paths.txt
LC_ALL=C sort -cu docs/kilo-spike-7.4.23/event-types.txt
git diff --check
```

Acceptance: all checks pass; the fixture contains no secret, absolute-home,
raw-log, model-catalog, or project-dependent data.

### Phase 2: Move the known-good pin

1. In `version.go`, set `KnownGoodVersion = "7.4.23"`, cite MADR 0108 and
   its fixture directory, and update the health-comment example.
2. In `dialect_test.go`, rename the exact-version tests to
   `TestKnownGoodVersionIs7423` and `TestOnHealthyRecords7423`; change inputs,
   expected values, and failure messages to 7.4.23/0108.
3. Run the focused tests, pre-add checks, race suite, and no-model live gates.
4. Stage only the two Phase 2 Go files and commit with
   `git commit --no-edit`.

Phase 2 verification:

```bash
go test ./internal/provider/kilo/ \
  -run 'TestKnownGoodVersionIs7423|TestOnHealthyRecords7423'
make pre-add-check FILES="internal/provider/kilo/version.go internal/provider/kilo/dialect_test.go"
go test -race ./internal/provider/kilo/
go test -tags live_kilo ./internal/provider/kilo/ \
  -run 'TestLiveKilo7423(Surface|ACPInitialize|ReadOnlyAgentBoundaries)$' \
  -count=1 -timeout 180s -v
git diff --check
```

Acceptance: the constant and tests say 7.4.23; the live engine and health
agree; no runtime dialect file changes.

### Phase 3: Append traceability errata and correct test documentation

1. Append a dated erratum to 0075 stating that 0108 supersedes only its
   current known-good version, now 7.4.23. Preserve the original 7.4.20/7.4.22
   acceptance narrative.
2. Append a dated erratum to 0088 stating:
   * D1's version value is superseded by 0108 while its pin logic carries;
   * the historical 119 Event count is correct when extraction reads each
     referenced schema's top-level event discriminator; and
   * `session.next.prompt.*` and `invalid` already existed in 7.4.22.
3. Update only the `live-kilo` Makefile comment. State that the aggregate
   catalog test requires a non-empty catalog, dedicated catalog cases may
   skip when prerequisites are absent, and prompt/tool checks are
   model-dependent. Do not change the target command.
4. Inspect all links and changed prose, run `git diff --check`, stage Phase 3
   files, and commit with `git commit --no-edit`.

Phase 3 verification:

```bash
rg -n '0108|7\.4\.23|119|canonical' \
  docs/0075-MADR-kilo-cli-provider.md \
  docs/0088-MADR-kilo-7.4.22-surface-parity.md
rg -n 'Live Kilo HTTP suite|aggregate|model-dependent' Makefile
git diff --check
```

Acceptance: the records are visibly amended, historical claims remain
readable as historical claims, and the Makefile recipe is unchanged.

### Phase 4: Full acceptance

1. Run the entire package race suite again.
2. On a host with a usable explicitly selected model, run `make live-kilo`.
   A model choosing not to invoke a requested tool may produce a documented
   skip in an existing model-dependent test; it is not permission-boundary
   evidence. The no-model permission test from Phase 1 must pass regardless.
3. Confirm `git status --short` contains no unintended file and review all
   implementation commits. Do not push.

Phase 4 verification:

```bash
go test -race ./internal/provider/kilo/
make live-kilo
git diff --check
git status --short
git log --oneline -5
```

Acceptance: required no-model gates, unit/race tests, and token-bearing live
suite have completed; any skip is named and classified rather than reported
as a pass.

## Rollout and Rollback

The rollout is local: after Phase 2, newly started Kilo hosts recognize
7.4.23 as known-good. There is no data migration and no protocol negotiation
change.

Rollback triggers are a runtime `/doc` delta outside the exact sets above, a
permission boundary weaker than D5, an ACP initialize change, or a session
loop regression in the full live suite.

Rollback procedure:

1. Stop before advancing if the trigger appears before a phase commit.
2. If committed, revert the affected phase commit using the repository's
   normal `git revert` workflow and `git commit --no-edit`; do not manually
   erase unrelated work.
3. Restore `KnownGoodVersion` and its two tests to 7.4.22 if Phase 2 is
   reverted. The resulting drift warning is noisy but safe.
4. Keep the 7.4.23 fixture and append the failed observation to 0108; evidence
   is not removed during rollback.
5. Do not alter 0087/0088 session behavior opportunistically. Amend 0108 and
   obtain approval for any compatibility fix.

## Final Acceptance Checklist

- [ ] Phase 0: the accepted 0108 pair is committed alone after explicit owner
      approval.
- [ ] Phase 1: isolated no-model version/OpenAPI/ACP/permission gates pass.
- [ ] Phase 1: complete sanitized fixture corpus is committed.
- [ ] Phase 1: exact path/schema/Event set comparisons pass.
- [ ] Phase 2: known-good constant, comment, and tests are 7.4.23.
- [ ] Phase 2: pre-add checks and package race suite pass before commit.
- [ ] Phase 3: 0075/0088 errata and Makefile comment are accurate.
- [ ] Phase 4: full `make live-kilo` result is recorded with skips classified.
- [ ] Phases 0-3 each have one `git commit --no-edit`; no commit uses `-m`
      or `-F`.
- [ ] No push, tag, release, or external Kilo checkout mutation occurs.
- [ ] Any discovered out-of-scope change is deferred through an amended,
      re-approved MADR/PLAN pair.
