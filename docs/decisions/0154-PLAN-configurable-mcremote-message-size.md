---
status: proposed
date: 2026-09-08
associated-madr: 0154-MADR-configurable-mcremote-message-size.md
---
<!-- markdownlint-configure-file {"MD004": {"style": "asterisk"}} -->

# Implement configurable mcremote message size

Associated MADR:
[0154-MADR-configurable-mcremote-message-size.md](0154-MADR-configurable-mcremote-message-size.md).

## Goal

An operator can set `limits.max_message_bytes` in mcremote, and the daemon's
inbound/outbound enforcement, advertised capability, and mobile preflight
all honor that value. Omission or zero preserves the 1 MiB default.

## Scope

One implementation phase delivers the server and mobile contract together.
Only the following files may change; new test files are explicitly identified.

| Files | Purpose |
| --- | --- |
| `internal/config/config.go`, `internal/config/load.go` | Field, constants, defaults, resolution, validation, environment binding |
| `internal/config/config_test.go` | Loading and validation regressions |
| `internal/daemon/daemon.go` | Pass the resolved value into the server |
| `internal/ws/server.go`, `internal/ws/liveness.go` | Read/write enforcement and capability reporting |
| `internal/ws/message_size_test.go` (new) | Real socket boundaries, outbound paths, error handling |
| `internal/ws/negotiation_test.go` | Auth and pairing capability assertions |
| `internal/ws/liveness_test.go` | Update capability helper tests if its signature changes |
| `internal/cli/service/defaults_mcremote.yaml` | Service configuration seed |
| `configs/config.example.yaml`, `configs/config.prod.example.yaml`, `configs/config.mesh-grok.yaml` | All required examples |
| `docs/config.md`, `docs/config-mcrelay.md` | Key, environment variable, framing layers, restart and limits |
| `apps/mobile/lib/data/protocol/frame_budget.dart` | Shared budget validation/default and display helper |
| `apps/mobile/lib/data/protocol/models.dart` | Safe parsing of the advertised budget |
| `apps/mobile/lib/data/ws/mcremote_client.dart` | Current connection budget getter and final send guard |
| `apps/mobile/lib/features/chat/chat_screen.dart` | Image, audio, and prompt preflight; accurate error text |
| `apps/mobile/test/protocol_negotiation_test.dart` | Negotiated values and fallback parsing |
| `apps/mobile/test/mcremote_client_test.dart` | Actual request guard, reconnect and host changes |
| `apps/mobile/test/audio_attachment_test.dart` | Attachment preflight using configured limits |
| `apps/mobile/test/chat_frame_budget_test.dart` (new) | Composer/image/prompt boundary and error feedback |
| This PLAN and its associated MADR | Approval state, execution results, deviations |

Existing reference documents retain their current names. New artifacts use
the required numeric naming convention. The relay implementation, provider
transports, attachment content limits, history paging policy, queue capacity,
dependencies, workflows, live configuration, and deployments are outside this
phase. No external CLI behavior is assumed or changed, so live provider tests
are not required for this feature.

## Implementation Steps

### Phase 1 — Configure and honor the message budget

1. After explicit execution approval, record the accepted MADR and mark this
   PLAN `in-progress`. Recheck `git status` and preserve unrelated work.
   Record the baseline diagnostic results from preparation. Stop and present
   evidence if a required check exposes a pre-existing failure.
2. In `config.go`, add `LimitsConfig.MaxMessageBytes` with the mapstructure
   key `max_message_bytes`. Define the default and validation bounds in one
   Go location usable by the server without duplicating numeric policy.
   Set the default to 1048576, resolve zero to it, and validate zero or
   4096–16777216 inclusive. Register the default and explicit environment
   alias in `load.go`.
3. Extend `config_test.go` for omitted and explicit zero, 4096, 1048576,
   4194304, 16777216, negative, 1, 4095, and 16777217 values. Cover YAML,
   environment-only configuration, environment-over-YAML precedence,
   resolution, and useful validation errors naming the key.
4. Wire the resolved field through `daemon.go` into `ws.Options`. Store a
   resolved, immutable limit on `Server`; preserve the default for existing
   zero-valued Options users. Replace the inbound `SetReadLimit` argument
   and the direct response size comparison. Resolve the default centrally
   instead of retaining two independently maintained transport constants.
5. Enforce the limit in `writeBytes` so broadcast, replay, cached responses,
   and receipt paths cannot bypass it. Preserve marshal-once fan-out.
   Return/log an oversize failure and enqueue a bounded typed error; retain
   the request ID for direct responses when it fits. Avoid recursive error
   handling and do not emit an oversized error. Close if a bounded error
   cannot be delivered. Ensure the existing direct response guard runs
   before idempotency capture so oversized original responses are not cached.
6. Have `capsFor` advertise the server's resolved budget for auth and pairing.
   Remove the stale hard-coded capability dependency from the liveness
   helper, adjusting its callers/tests only within the listed files.
7. Add `message_size_test.go` and extend negotiation tests. Use authenticated
   local WebSockets with the test peer read limit explicitly raised when
   receiving large messages. Measure serialized bytes, not character counts.
   Test exact boundary acceptance and one byte over rejection, default
   rejection above 1 MiB, 4 MiB configuration accepting a message above
   1 MiB, a smaller configured budget, and the 16 MiB boundary. Exercise
   inbound, direct responses, broadcast, replay, and repeated oversized
   errors; assert typed failure/closure rather than timeout or silent loss.
8. Populate the key and environment guidance in all four YAML templates.
   Update the two configuration references with a 4 MiB example, accepted
   range, zero semantics, restart requirement, UTF-8/JSON/base64 accounting,
   independent relay framing, and provider/operation limits. Run existing
   template parity tests without changing their assertions.
9. In the mobile protocol helpers/parser, define a validated effective
   budget using the advertised integer when within 4096–16777216 and the
   existing 1 MiB default otherwise. Malformed values, including strings,
   fractional values, zero, and negative values, must not crash parsing or
   disable the guard. Expose the active budget through the client so chat
   and the final request guard consult the same value on every operation.
10. Update the client's send guard and all three chat preflight paths to
    use that getter. Generate limit text from the actual value, with accurate
    MiB units and enough precision to avoid displaying a small limit as zero.
    Preserve attachment state on rejection and the existing request cleanup.
11. Extend/add the listed mobile tests for default fallback, a 4 MiB
    negotiated limit accepting a request above 1 MiB, a smaller limit
    rejecting at one byte over, exact-boundary acceptance, UTF-8 multibyte
    text, base64 expansion, image/audio/prompt preflight, and actual error
    text. Cover disconnect/reconnect and switching hosts with different caps;
    a stale budget must not survive a capability reset. Use valid media
    fixtures so image decoding does not obscure frame-budget failures.
12. Run the negative experiments and gates below. Record full command
    outcomes, observed failures, and final passing results in this PLAN;
    sanitize any environment identifiers when recording evidence. Resolve
    deviations through owner approval and document amendments before edits.
13. Once all acceptance criteria pass, mark the PLAN complete and update its
    execution record. Review the exact diff, run pre-add checks before
    staging Go files, and commit the phase with `git commit --no-edit`.
    Accept the generated message and verify it reflects the scope and any
    deviations; do not supply a message or push.

## Verification

### Prove the regression checks can fail

After the implementation and tests exist, copy the relevant working tree
into a temporary directory outside the repository, excluding `.git` and build
outputs; retain the working tree's actual source and tests. Flutter may create
its normal ignored dependency state in the copy. Run a positive baseline there
first to distinguish copy/setup failures from deliberate mutations.

Use independent copies or restore files from the unmodified copy between
experiments. For each edit, assert the exact original match count, assert the
replacement landed, run the focused test with verbose output, read its entire
output, and require a nonzero result with the expected assertion failure:

* Replace the server's configured inbound limit with 1 MiB: the larger
  configured inbound acceptance test must fail with premature socket closure.
* Replace direct outbound enforcement with the old 1 MiB budget: the
  configured larger-response acceptance test must fail.
* Remove only the common `writeBytes` size guard: broadcast/replay oversize
  tests must fail because an oversized original frame was delivered.
* Hard-code capability reporting to 1 MiB: configured auth/pair assertions
  must report an expected/actual mismatch.
* Hard-code the client getter to 1 MiB: larger-budget send and composer
  tests must fail. Separately bypass the final send guard: one-byte-over
  rejection tests must fail because a request reaches the test server.
* Bypass config range validation: invalid-value tests must fail because
  loading succeeds. Ignore the environment binding/default registration:
  the environment-only/override tests must fail if configuration is no
  longer honored; verify the mutation actually removes environment support.

Never create broken inputs by dirtying or reverting the real working tree.
A mutation that leaves a test passing is not verification; investigate the
test or experiment and report what it actually establishes.

### Commands

Run from the repository root unless indicated otherwise:

```bash
go test ./internal/config ./internal/ws ./internal/cli/service ./internal/daemon -count=1
make test
make race
```

Before staging, format each changed Go file with `gofmt -w` and run the
repository's authoritative gate on this complete planned Go file list:

```bash
./scripts/go-precheck.sh internal/config/config.go internal/config/load.go internal/config/config_test.go internal/daemon/daemon.go internal/ws/server.go internal/ws/liveness.go internal/ws/message_size_test.go internal/ws/negotiation_test.go internal/ws/liveness_test.go
markdownlint-cli2 docs/config.md docs/config-mcrelay.md
git diff --check
```

From `apps/mobile`, format changed Dart files with `dart format` and run:

```bash
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test test/protocol_negotiation_test.dart test/mcremote_client_test.dart test/audio_attachment_test.dart test/chat_frame_budget_test.dart
flutter test
```

Run `make preflight` in a temporary copy of the completed tree with
`MCREMOTE_VERSION_PUSH=0 MCREMOTE_VERSION_TAG=0`; its tidy/build/install
diagnostics write files and must not overwrite unrelated working-tree state.
No actual deployment or live service restart is authorized by this step.

### Acceptance criteria

* YAML and environment selection produce identical effective limits; env
  overrides YAML, zero preserves the default, invalid values fail startup.
* Every application read and outbound enqueue enforces the same byte limit.
* Real messages at the limit pass; one byte above fails explicitly. Raising
  the limit demonstrably permits a message larger than the previous 1 MiB cap.
* Auth and pairing advertise the exact selected budget.
* Updated mobile clients honor that budget in send and composer checks,
  display it accurately, and fall back safely on older or malformed caps.
* Reconnect/host switching uses current capabilities, with no stale budget.
* Existing default behavior and queue protections pass their regressions;
  the formerly unguarded outbound paths now report oversize failure.
* All required examples and documentation agree with the implementation.
* Negative experiments fail for the intended reasons; the unmodified
  implementation passes the targeted, full, race, format, lint, and preflight gates.

## Rollout and Rollback

Keep the default until the updated daemon and phone are installed. To select
4 MiB, set `limits.max_message_bytes: 4194304` in the existing limits section,
or set `MCREMOTE_LIMITS_MAX_MESSAGE_BYTES=4194304`, then restart the daemon and
reconnect the phone. If raising outer relay message sizes too, set the
existing bridge cap to match mcrelay's outer limit. This plan implements the
feature; it does not install binaries or modify any live configuration.

For operational rollback, restore 1048576 (or zero/remove the key), restart,
and reconnect. Requests above that size will again fail explicitly. Reverting
the implementation commit or deploying older binaries is a separate owner
action; an older phone continues to enforce its own 1 MiB request budget.

## Execution Record

2026-09-08: Prepared the proposed pair after read-only inspection at
`199c5c4`. Source implementation and regression test authoring have not begun.
Execution is awaiting owner approval. The baseline command
`go test ./internal/config ./internal/ws ./internal/cli/service ./internal/daemon`
exited 0 with these results (common module prefix omitted):

```text
ok  internal/config       0.430s
ok  internal/ws          16.584s
ok  internal/cli/service  2.208s
ok  internal/daemon       4.518s
```

These are existing-test baseline results, not verification of the proposed
feature. New tests and their negative experiments remain part of execution.
