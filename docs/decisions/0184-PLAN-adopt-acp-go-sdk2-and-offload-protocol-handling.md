---
status: proposed
date: 2026-10-13
associated-madr: "0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement adopting `acp-go-sdk2` and moving ACP protocol handling into the SDK

Associated MADR: [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)

**Not approved for execution.** The MADR is `proposed`. A phase starts only after the MADR is
accepted **and** the owner approves that phase by name ("do A1"). Each phase ends with
`make pre-add-check` on its staged Go files, `go test -race ./...` and `make preflight`, plus the
0145 gates on a Windows host. The owner commits (`git commit --no-edit`). An agent stages only.

## Goal

Magic depends directly on `github.com/maccavelli/acp-go-sdk2 v0.17.1`. Every standard ACP mechanism
is the SDK's: transport, gates, negotiation, cancellation, validation and typed methods. The only
ACP code Magic owns is grok's vendor surface, behind the grok spec, and the mapping of protocol
events onto Magic's own event model.

## Scope

In scope, across the phases:

* `go.mod`, `go.sum`, `internal/modguard_test.go`;
* `internal/provider/acpagent/` and `internal/provider/acpcommon/`;
* `internal/provider/grok/`, for the vendor fallbacks D6 moves there;
* the phone's permission-card handling, for D8 only, under `apps/mobile/lib/`;
* the record pointer amendments named in each phase.

Out of scope:

* the capability records of MADR D10, each of which gets its own MADR and PLAN;
* gobble's registration (0179);
* `fs/*` confinement to `Roots()` (the MADR's open question);
* any change in `acp-go-sdk2` itself (D11 is a request, made under that repository's records).

## Implementation Steps

### Rules for every phase

* **Fail-first.** A new or changed assertion counts only after it has been seen to fail on a
  deliberately broken scratch copy. The phase's execution record names the break and quotes the
  failure.
* **Read before concluding.** Long output goes to a scratch file with its exit status captured
  before any filter, and the whole file is read before a phase is called green.
* **Quota.** No live provider test runs without the owner's approval in the same turn. `make
  live-grok` spends grok quota.
* **Scope is closed.** Anything found outside a phase's scope is a dated deviation in this PLAN,
  and in the MADR when it changes a decision or a fact, and waits for re-approval.

### A1 — Move to the SDK, with explicit conversions (D1, D2, D3, D9)

This phase lands as one change, because the module does not compile until the adaptations land
with it. Commits inside it may separate the mechanical import change from the adapters, provided
each one builds.

1. Confirm `git ls-remote https://github.com/maccavelli/acp-go-sdk2 'refs/tags/v0.17.1^{}'` gives
   `c77f25a45e3345c40f954dd80add9f09a1fd5ae1`. Read the release notes of any later tag before
   choosing it, and record the choice.
2. Rewrite `internal/modguard_test.go` first (D1). Show it failing on the current `go.mod`, and on a
   scratch copy that re-adds a `replace` or a pseudo-version.
3. Remove the Coder requirement and the `replace` block, `go get
   github.com/maccavelli/acp-go-sdk2@v0.17.1`, rewrite the 29 imports, and run `go mod tidy`.
4. Fix the 25 diagnostics the 2026-10-13 probe listed, by D3's rules. Write each conversion's
   boundary test before its fix: terminal limit at `math.MaxUint64`, usage at `math.MaxUint64`,
   file line and limit past the end of the file, and exit status by signal.
5. Implement `CreateElicitation` and `CompleteElicitation` as method-not-found (D9).
6. Construct with `WithNotificationOverflowPolicy(OverflowDropNewest)`, `WithLogger` and
   `WithMaxMessageSize(10 << 20)` (D2). Replace `rawConnOf` with `Connection()`, and delete the
   `unsafe` import and the comment block at `acpagent.go:496-511`.
7. Re-run 0167 D19's survival test: fail with the overflow option removed, pass with it, for the
   repeat count 0167's PLAN used.
8. Add the raw-value test: empty, `null`, a scalar, an object, an array and a large value. No
   permission summary shows `null`.

Verification: `go build ./...`; `go vet` for darwin, linux and windows; `go test -race
./internal/...`; and a Python scan of every `.go` file and `go.mod` finding no
`github.com/coder/acp-go-sdk`, with the script's exit status recorded.

### A2 — Honest `initialize`, SDK enforcement, cancellation (D4, D5, D8)

1. Write the fixture-agent test that sends a boolean config option in `session/new`. Show it failing
   after A1 and before this phase.
2. Add `clientInfo` and advertise `session.configOptions.boolean`. Pin the serialized initialize
   frame in a wire test.
3. Add the capability-honesty table test (D4). Show it failing on a scratch copy that advertises
   `session.notices` without behavior.
4. Replace `sessioncaps.go`'s copied flags and `buildMcpServers`' transport filter with
   `Negotiated()` and `Negotiation.Allows`.
5. Replace the `-32601` literal with `ErrorCodeMethodNotFound`, and classify errors with
   `*ValidationError` and `*CapabilityError` (D5).
6. Add wire tests for `-32700`, `-32600`, `-32602` (a missing required key; an empty and a relative
   `fs` path) and `-32601`, and for an over-long line. Then delete the duplicate absolute-path checks
   in `ReadTextFile` and `WriteTextFile`. The wire tests must still pass after the deletion.
7. Cancellation (D8): recognise `stopReason: cancelled` as well as `-32800`. When a permission
   handler's context ends with `ErrPromptCancelled`, close the phone's permission card. Add the
   daemon test and the Flutter widget test, each seen failing first.

Verification: as A1, plus `make preflight` for the Flutter change and `dart format` on the changed
Dart files.

### A3 — Typed standard methods; grok's vendor surface behind its spec (D6)

1. Add the test that fails when any method not beginning with `_` reaches `rawRequest` or
   `SendRequest` outside a named grok fallback. Show it failing on the current code.
2. Add `acpagent.Spec` hooks for model selection, thinking selection and config read-back. The
   default implementation uses `SetSessionConfigOption` by category (`model`, `thought_level`) and
   reads back from the response's `configOptions`.
3. Move the raw `session/set_model` paths (`session.go:962`, `thinking.go:70`), the `session/resume`
   read-back (`thinking.go:77`) and the `x.ai/sessionConfig` harvesting (`session_meta.go`) into
   `internal/provider/grok` as that spec's hooks. They run only when the agent advertises no
   standard option.
4. Type every remaining `session/resume` as `ResumeSession`.
5. Fixture tests: an agent with `model` and `thought_level` options produces
   `session/set_config_option` frames, and an agent without them exercises the grok fallback only.
6. **Owner gate:** with explicit approval to spend quota, run `make live-grok` with wire capture,
   and record whether current grok advertises `model` or `thought_level` options. Commit the new
   capture, redacted, beside `testdata/wire/1.0.13/`.
7. Add pointer amendments to `0039-MADR-grok-acp-parity.md` (D1) and
   `0106-MADR-grok-1.0.5-surface-parity.md`, naming this record as where the raw path now lives.

### A4 — Every session update mapped or declined (D7)

1. Write the table test over every schema 1.25 session-update variant plus `Unknown`. Show it
   failing on the current `default:` branch.
2. Replace the branch with one case per variant. The declined variants log once per session per
   variant at Debug, naming the variant.

### A5 — Requests to the SDK, and one pin for both agents (D11, D12)

1. Check whether `v0.17.1`'s typed methods validate outbound params and decoded results beyond
   paths. Record the answer, with the code location, in this PLAN.
2. If they do not, draft the request as an `acp-go-sdk2` record or issue, cited by repository and
   number. Do the same for the second-prompt cancellation question (gobble M10).
3. Record the pin coordination with gobble, which is at `v0.14.0`. The bump itself is gobble's,
   under gobble's records.

### A6 and later — capability records (D10)

One MADR and PLAN per capability, in the MADR's order, each written when its phone behavior is
designed. Nothing in A1–A5 advertises a capability beyond D4's.

## Verification

Whole-plan acceptance is the MADR's Confirmation section. In addition:

* `python3 scripts/check_records.py` and `make markdownlint-docs` pass after each record change;
* the redacted wire captures carry no hostname, account name, or real-machine path. The scan of
  committed fixtures for `/private/var/folders/` and user home paths finds nothing new.

## Rollout and Rollback

* A1 is one revertible change: reverting it restores the fork, the `replace` and the old guard
  together.
* A2's `session.configOptions.boolean` advertisement is a wire change. Rolling it back
  reintroduces the `session/new` failure for any agent that sends boolean options, so it is
  reverted only together with A1.
* A3's fallback keeps grok working whether or not grok advertises the standard options, so it can
  ship before the live probe answers. The probe decides only whether the fallback is still used.
* No phase changes what an operator configures.
