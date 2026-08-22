---
status: proposed
date: 2026-08-22
decision: 0113-MADR-preexisting-unit-coverage-debt.md
---

<!-- markdownlint-disable MD004 MD013 MD024 MD029 MD033 MD036 MD060 -->

# Close pre-existing unit-coverage debt

## Associated decision

This plan implements
[0113-MADR-preexisting-unit-coverage-debt.md](0113-MADR-preexisting-unit-coverage-debt.md).
It is the work that
[0112-PLAN-opencode-1.18.21-surface-parity.md](0112-PLAN-opencode-1.18.21-surface-parity.md)
carried as its P0A phase before that scope was separated.

Both this plan and its MADR are proposed. Creating them authorizes nothing.
Execution starts only on the owner's explicit go-ahead in the turn that starts
it.

## Goal

Raise every Go package and existing Dart production file in the MADR's debt
table, plus the Flutter application in aggregate, to at least 82.0% using
deterministic default-tag unit and widget tests, then make the 80.0% floor a
required local and CI gate.

This is test-only work. Production behavior, dependencies, runtime
configuration, fixtures and protocol documentation do not change.

## Sequencing against 0112

Do not run this plan concurrently with 0112. The two touch overlapping test
files — `internal/provider/opencode`, `internal/ws`, `internal/session`,
`internal/protocol`, `internal/event` and three of the four Dart files. Run this
plan after 0112 has landed the phases it is going to land, rebase onto that
tree, and re-measure before starting: 0112 adds production statements, so every
denominator in the MADR's table will have moved, and its own tests will have
raised some numerators.

The first step of C0 is therefore a fresh measurement, not a copy of the MADR's
figures.

## Cross-cutting contract

### Tooling

Reuse what 0112 P0 committed. Do not fork it.

```bash
scripts/coverage-snapshot.sh --output OUTPUT_DIR --go GO_PACKAGE [GO_PACKAGE ...] [--flutter apps/mobile]
scripts/coverage-delta.sh floor --after AFTER_DIR --minimum 82.0 --opencode-floor 85.0 --go GO_PACKAGE [...] [--dart-root apps/mobile --dart-file DART_FILE ...]
scripts/coverage-delta.sh phase --before BEFORE_DIR --after AFTER_DIR --minimum 80.0 --go GO_PACKAGE [...] [--dart-root apps/mobile --dart-file DART_FILE ...]
```

All comparisons are exact integer counts; console percentages are never parsed.
`internal/daemon`, `internal/session` and `internal/ws` vary by a few
concurrency-dependent statements between runs and are always compared same-run.

### Rules that apply to every phase

1. Snapshot before, write tests, snapshot after, and require the target group to
   have strictly improved with no other target regressing.
2. Tests assert behavior. After each group, confirm with `go tool cover -func`
   or LCOV inspection that the intended functions moved. A green test whose
   target is still uncovered is not progress.
3. No production file changes. No exported-for-testing symbols, no new seams, no
   refactors, no dead-code deletion, no `//go:build` tricks, no exclusions. If a
   scenario is unreachable without one, stop and record it.
4. No real agent binary, no tokens, no user credential state, no wall-clock
   sleeps to win a race. Use `httptest`, in-memory stores, existing WebSocket
   and widget harnesses, and injected clocks where they already exist.
5. New Go gap files live in the package under test when they need unexported
   helpers.
6. Each phase ends in its own local commit after its checks pass. No phase
   authorizes a push.

## C0 — Re-measure and set the working baseline

### Outcome

A current, committed-to-the-phase-todo measurement of every target on the
post-0112 tree.

### Steps

1. Capture a full snapshot of all eleven Go packages and the Flutter application
   into a `mktemp -d` directory recorded in the phase todo.
2. Record exact covered/total/uncovered and the exact additional statements or
   lines each target needs for 80.0% and for 82.0%.
3. Note which targets 0112 already lifted above the floor; they need only be
   held, not raised.
4. Re-derive the per-phase ordering below from the fresh numbers if the largest
   deficits have moved.

### Verification

```bash
coverage_dir=$(mktemp -d /tmp/mcremote-0113-c0.XXXXXX)
scripts/coverage-snapshot.sh --output "$coverage_dir/base" \
  --go ./internal/provider ./internal/provider/opencode ./internal/provider/httpagent \
       ./internal/event ./internal/picker ./internal/protocol ./internal/session \
       ./internal/ws ./internal/config ./internal/daemon ./internal/cli/service \
  --flutter apps/mobile
```

### Acceptance

* Every target has an exact current count and an exact deficit.
* No file in the working tree changed.

## C1 — Small Go targets

### Outcome

`internal/event`, `internal/protocol` and `internal/config` are at or above
82.0%. These are the cheapest targets and prove the harness before the large
ones.

### Files

* `internal/event/event_test.go`.
* `internal/protocol/messages_test.go`.
* `internal/config/config_test.go`; create `internal/config/load_paths_test.go`.

### Steps

| Target and measured gap | Test file and exact test cases |
| --- | --- |
| `internal/event`: 4/6, needs 1 statement | Enhance `event_test.go` with `TestIsInPlaceUpdateMatrix` (`tool_call_update` with ID true; missing ID and wrong type false) and `TestIsTerminalToolStatusMatrix` (completed/failed true; pending/running/empty/unknown false). |
| `internal/protocol`: 11/25, needs 10 | Enhance `messages_test.go` with `TestNegotiateVersionMatrix` (nil, empty, v1, v2, duplicate/reordered, mixed unknown, no mutual); `TestCatalogResultBuildersNormalize` (models/agents/commands preserve provider, source, option order, defaults, custom and truncated while normalizing empty kind/labels and single-select min/max); and `TestDecodePayloadMatrix` (empty no-op, valid decode, malformed error). |
| `internal/config`: 483/615, needs 22 | Add `TestTLSManagedSchemeAndACMECacheMatrix` for managed/external certs, off/self-signed/ACME schemes, explicit/default cache; `TestRecomputePathsFromEmptyAndExistingBase` for both branches and diagnostics retention; and `TestFinalizePathsPrecedenceAndRelativeEnvRejection` for config-relative, flag/CWD-relative, absolute, and invalid relative `MCREMOTE_DATA_DIR`. |

### Verification

```bash
go test -count=1 -race ./internal/event ./internal/protocol ./internal/config
make pre-add-check FILES="internal/event/event_test.go internal/protocol/messages_test.go internal/config/config_test.go internal/config/load_paths_test.go"
```

### Acceptance

* All three packages are at least 82.0%; no other target regressed.
* Every listed input and expected result has a behavioral assertion.

### Commit

Precheck, stage only C1 files, run `git commit --no-edit`.

## C2 — `internal/provider`

### Outcome

`internal/provider` reaches at least 82.0% (needs 54 statements from 79/161).

### Files

* `internal/provider/provider_test.go`, `registry_test.go`, `prewarm_test.go`;
  create `auth_owned_test.go` and `review_test.go`.

### Steps

Add `TestOwnedFlowHandleForwardsLifecycle` for flow metadata, update
forwarding/coalescing, wait success/context cancellation, explicit cancel, and
update-channel close; `TestGoalIsActiveMatrix` for absent/blank/active/paused/
complete/unknown; `TestParseReviewArgMatrix` for bare/uncommitted/base/commit/
custom, whitespace/case, and every missing/unknown rejection; extend the
registry tests with `TestRegistryAllAndListWithAuthMatrix` for plain, auth
success, unsupported, error, cancelled, overwrite and concurrent snapshot
behavior; extend prewarm tests with `TestControllerCurrentAndEngineEdges` for
nil/live current, nil/missing registry, EnsureServer/EnsureWarm preference,
shutdown, and stop-while-live.

### Verification

```bash
go test -count=1 -race ./internal/provider
make pre-add-check FILES="internal/provider/provider_test.go internal/provider/registry_test.go internal/provider/prewarm_test.go internal/provider/auth_owned_test.go internal/provider/review_test.go"
```

### Acceptance

* At least 82.0%; no other target regressed.

### Commit

Precheck, stage only C2 files, run `git commit --no-edit`.

## C3 — `internal/provider/httpagent`

### Outcome

The single largest deficit closes: 737/1,470 to at least 82.0% (469
statements). Split into four commits, one per file, so review stays tractable.

### Files

* Create `authcatalog_test.go`, `deviceauth_test.go`,
  `provider_lifecycle_test.go` and `session_delegation_test.go`; extend
  `provider_test.go` and `session_test.go`.

### Steps

| Group | Exact test cases |
| --- | --- |
| Catalog and auth | `TestFetchVendorCatalogMatrix` (HTTP error, blank IDs, name fallback, sort, model counts, connected trimming); `TestFetchAuthMethodsMatrix` (HTTP error, blank vendor/prompt keys, text/select/options/when, label fallback, browser/device/API classification); `TestBuildCatalogMatrix` (catalog-only/method-only/connected/method-duplicate suppression/kilo denial/sort); `TestVerifyAPIKeyMethodMatrix` (empty/synthetic/foreign/non-numeric/negative/out-of-range/OAuth/API/fetch error); `TestAPIKeyAuthBodyMetadata`; `TestEngineCatalogDegradesMissingMethods`; `TestProviderAuthCatalogCacheAndUnsupported`. |
| Device flow | `TestEngineMethodIndexMatrix`; `TestStartEngineDeviceFlowMatrix` for empty upstream, foreign method, authorize error, host-browser refusal, unusable response, valid code/URL/instructions, request path/body/input preservation; `TestSnapshotDeviceMatrix`; `TestAwaitEngineCredentialMatrix` for appearance, fingerprint rotation, unchanged pre-existing credential, engine-unavailable retry, and context cancellation using a reduced restored `DevicePollInterval`. |
| Provider lifecycle | `TestProviderConstructionIdentityAndReadiness`; `TestProviderCatalogFallbackMatrix` covering unsupported dialect, binary absent, ensure failure, live success/error, static/live merge, configured default, provider scope, empty scoped result, cache reuse/invalidation; `TestAuthWriterSelectionMatrix` for API success/failure/verification rollback, cold file fallback, unsupported, clear, cache invalidation, active-turn refusal; `TestDeviceAuthAndUpstreamDelegationMatrix`; `TestAPIAtMatrix` for body encoding, 2xx empty/JSON, non-2xx clipping, malformed JSON, cancellation; `TestStreamOnceMatrix` for bad status, malformed/empty/unknown/stale/known SSE frames, oversized line recovery, EOF and read error; `TestRestartAndShutdownStateMatrix` without spawning a live engine. |
| Session delegation | `TestOptionalSessionOperationsMatrix` covering supported/unsupported/error/success for fork plus deferred-goal rejection, revert/unrevert, diff, mode event emission, compact, model, rename, diagnostics, undo; `TestSessionIdentityAccessorsAndModelCatalogMatrix` for IDs/CWD/config/API/event channel, scoped/default/fallback/empty/error catalogs, session-model default; `TestStartValidationMatrix` for binary/CWD/version/agent validation failures; `TestEmitBackpressureAndFlushMatrix` for closed session, timestamps/session IDs, telemetry drop, text unflush, timer retry, tool-lane terminal delivery, close cancellation. |

### Verification

```bash
go test -count=1 -race ./internal/provider/httpagent
```

### Acceptance

* At least 82.0%; no live engine is spawned by any test.

### Commit

One commit per group, each precheck-clean.

## C4 — `internal/session` and `internal/ws`

### Outcome

`internal/session` and `internal/ws` reach at least 82.0% (116 and 154
statements). Compared same-run: both vary between runs.

### Files

* `internal/session/commands_test.go`, `goal_command_test.go`,
  `manager_history_test.go`, `manager_parity_test.go`, `store_test.go`; create
  `manager_delegation_test.go`.
* `internal/ws/provider_auth_owned_test.go`, `server_session_handlers_test.go`,
  `server_test.go`; create `server_gap_test.go`.

### Steps

| Target | Exact test cases |
| --- | --- |
| `internal/session` | `TestCanonicalAndDaemonCommandNameTables`; no-argument/error/success matrices for compact, diff, undo, redo, sessions, model, thinking, reset/new, personality, goal/review/fork; `TestManagerDelegationAndOwnershipMatrix` for history/history-page, mode/collaboration/config, diagnostics/model catalog, cancel, permission/question, revert/unrevert/diff, close/delete, unknown/non-owner/unsupported/provider-error paths; `TestHistoryForAndPageForOwnerMatrix`; store tests for empty/malformed/list/delete/load-history boundaries. |
| `internal/ws` | Extend `provider_auth_owned_test.go` with start/await success, duplicate/expired/cancelled flow, disconnect/resume window, backup projection; extend `server_session_handlers_test.go` with create validation/capacity/ownership, prompt success/busy/error, collaboration, question, release/claim/history boundaries, agents/native-session failures, sanitized `writeSessionErr`; create `server_gap_test.go` for `CloseClients`, default-provider selection, auth start/cancel/expiry, `writeJSON`/`writeBytes` success and closed/short-write errors, async dispatch timeout/replay. Each request asserts one terminal response and no unauthorized manager or provider call. |

### Verification

```bash
go test -count=1 -race ./internal/session ./internal/ws
```

### Acceptance

* Both at least 82.0%, measured in the same run as their before snapshot.

### Commit

One commit per package, each precheck-clean.

## C5 — `internal/daemon` and `internal/cli/service`

### Outcome

`internal/daemon` and `internal/cli/service` reach at least 82.0% (51 and 164
statements).

### Files

* `internal/daemon/certs_test.go`, `credentials_test.go`, `daemon_test.go`;
  create `run_wiring_test.go`.
* `internal/cli/service/control_test.go`, `refresh_test.go`, `setup_test.go`;
  create `result_print_test.go` and `path_helpers_test.go`.

### Steps

| Target | Exact test cases |
| --- | --- |
| `internal/daemon` | Extend cert tests with missing/invalid external pair, self-signed persistence/error, ACME success/fallback/close idempotence; extend credential tests with watcher start, pending activation success/failure, cancellation and close; extend daemon tests with event-hub broadcast nil/one/many/slow clients and all current prewarm-plan combinations; add `run_wiring_test.go` for invalid auth/TLS guards, startup dependency failure, and orderly cancellation on a loopback ephemeral listener. |
| `internal/cli/service` | `TestPrintRefreshAndSetupResultMatrix` for changed/unchanged/warnings/errors/empty output; extend control tests for active/inactive/unknown Linux and Darwin states plus start/stop/probe command errors; extend refresh tests for restore-without-backup, launchd label/program-argument mutation, quoted systemd argv, missing/preserved environment, binary warning, invalid plist and command failures; extend setup tests for Linux/Darwin preflight failures, systemd and launchd removal success/not-found/error, bootstrap hints, UID/runtime environment, default config existing/missing/write failure, atomic unit replacement failure, XDG explicit/default paths. |

### Verification

```bash
go test -count=1 -race ./internal/daemon ./internal/cli/service
```

### Acceptance

* Both at least 82.0%.

### Commit

One commit per package, each precheck-clean.

## C6 — `internal/provider/opencode` to 85.0%

### Outcome

The OpenCode provider reaches at least 85.0% on the current tree. Recompute the
requirement from raw counts: 0112 changes the denominator, so the 28-statement
figure from the original baseline is stale by construction.

### Steps

1. Compute `ceil(total * 85 / 100) - covered` from a fresh profile.
2. Close the gap with direct assertions against functions `go tool cover -func`
   reports below 80%, preferring whatever 0112 has not already covered.
3. Repeat until the recomputed value is non-positive.

### Verification

```bash
go test -count=1 -race ./internal/provider/opencode
```

### Acceptance

* At least 85.0% under default tags, with no live-tagged test contributing.

### Commit

Precheck, stage only C6 files, run `git commit --no-edit`.

## C7 — Dart files and the Flutter aggregate

### Outcome

`models.dart`, `mcremote_client.dart`, `sessions_screen.dart` and
`chat_screen.dart`, plus the Flutter application in aggregate, reach at least
82.0% line coverage.

### Files

* Create `apps/mobile/test/protocol_models_gap_test.dart`,
  `mcremote_client_operations_test.dart` and `chat_screen_gap_test.dart`; update
  `mcremote_client_test.dart` and `sessions_screen_test.dart`.

### Steps

| Target | Exact widget/unit cases |
| --- | --- |
| `models.dart` | `protocol_models_gap_test.dart` with malformed/null/legacy/full parse and JSON round-trip tables for `AuthInputCondition`, `AuthInput`, `ProviderAuthInfo`, `SessionCapabilities`, `ConfigOption`, `SessionDiagnostics`, `SessionMeta`, `AgentSessionMeta`, `AvailableCommand`, attachments, usage and session events. Assert unknown fields are ignored, missing optional fields retain defaults, invalid nested rows are dropped, and additive fields serialize only when present. |
| `mcremote_client.dart` | `mcremote_client_operations_test.dart` around the existing fake WebSocket server. A response matrix covering success, expected-type mismatch, provider error, malformed payload, timeout and disconnect for the implemented session/provider/model/agent/command/receipt/device/native-session list operations and create/resume/history/pending-asks. A mutation matrix covering release/claim/fork/diff/rename/diagnostics, prewarm, credential/device-auth/upstream, mode/collaboration/config, prompt/cancel/close/delete, permission/question, verifying exact request types and payload omission. Enhance `mcremote_client_test.dart` for retry-after parsing/clamping, direct/relay sticky fallback, stale epoch protection, handshake error cleanup, ping/liveness reconnect, pending-request failure, host-authority credential clearing and idempotent disposal. |
| `sessions_screen.dart` | Extend `sessions_screen_test.dart` with empty/loading/error/retry, version failure, manual CWD validation and fallback, current native-session selection, provider/model/agent dialog states, rename cancel/success/error, create/resume failure, stale refresh suppression, sign-out/reconnect, handoff/claim confirmation and errors, end-session success/lost-ack/failure, connection-label/hostname fallbacks, timestamp buckets, long labels, disposed-widget completion. Each mutation asserts call count and mounted/navigation state. |
| `chat_screen.dart` | `chat_screen_gap_test.dart` with reconnect resync/history success/failure and sequence-floor cases; command filtering/insertion and model-command interception; voice unavailable/permission/error/stop; image cancel/read failure/MIME/size/exact-frame-boundary/remove; queued prompt flush/cancel/error; config sheet optional/multi-select/dangerous confirmation; diagnostics/diff/fork/end success, cancel, unsupported and error; permission/question single/multi/custom/cancel/timeout/replacement flows; mode/collaboration/thinking selector empty/current/dangerous/error states. Assert no duplicate request, stale dialog or transcript row. |

Any operation 0112 added to these files is tested by 0112 in its own phase; this
plan does not re-test it, and must not lower a figure 0112 already raised.

### Verification

```bash
(cd apps/mobile && dart format --output=none --set-exit-if-changed test)
(cd apps/mobile && flutter analyze)
(cd apps/mobile && flutter test)
```

### Acceptance

* Each of the four files and the application aggregate is at least 82.0%.
* LCOV resolves each requested path exactly once.

### Commit

One commit per file group, each `dart format`-clean.

## C8 — Make the floor a required gate

### Outcome

`make coverage-check` and a required CI job enforce the 80.0% absolute floors,
added in the change set that first makes the tree satisfy them.

### Files

* `Makefile` and `.github/workflows/ci.yml`.

### Steps

1. Add `make coverage-check`: run all eleven Go package profiles and the full
   Flutter LCOV run, then `scripts/coverage-delta.sh floor` with
   `COVERAGE_MIN=80.0` and `OPENCODE_COVERAGE_MIN=85.0` defaults and the exact
   Dart paths from the MADR's table.
2. Add a required Ubuntu `coverage` job to `.github/workflows/ci.yml` with
   `permissions: contents: read`, a 30-minute timeout, and the action pins the
   workflow already uses:
   `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1`,
   `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` with
   `go-version-file: go.mod`, and
   `subosito/flutter-action@1a449444c387b1966244ae4d4f8c696479add0b2` with
   `channel: stable`, `flutter-version: ${{ env.FLUTTER_VERSION }}` and
   `cache: true`. Run `(cd apps/mobile && flutter pub get)` then exactly
   `make coverage-check`. Do not set `continue-on-error`.
3. On failure upload only the sanitized JSON summary with
   `actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a`; never
   raw profiles.
4. CI compares against absolute floors only. It never compares Ubuntu counts
   with a Darwin baseline.

### Verification

```bash
make coverage-check
```

### Acceptance

* `make coverage-check` passes on the closed tree and fails a target lowered to
  79.999%.
* The CI job is required and cannot be satisfied by the existing Go or Flutter
  jobs passing independently.

### Commit

Precheck, stage only the Makefile and workflow, run `git commit --no-edit`.

## Out of scope

Legacy Dart files below 80% that are not in the MADR's table are not addressed
and are not declared compliant. They remain future work; a plan that touches one
closes it in its own change set, which is the rule 0112 already follows for the
files it edits.

## Rollback strategy

Every phase is test-only, so reverting any commit restores the previous tree
exactly. Revert C8 first if the gate proves too tight in CI; keep the tests.
Never lower a floor as a rollback.

## Definition of done

Every C0–C8 acceptance item passes, each phase has its own verified local
commit, no production file differs from its pre-plan content, and
`make coverage-check` plus the required CI job enforce the floors from a fresh
capture.
