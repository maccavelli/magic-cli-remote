---
status: in-progress
date: 2026-10-13
associated-madr: "0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement adopting `acp-go-sdk2` and moving ACP protocol handling into the SDK

Associated MADR: [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-13 the owner accepted the
MADR, and approved A1 ("Accept 0184 + 0186, do A1"). A phase starts only after the owner approves
that phase by name ("do A1"). Each phase ends with
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

## Amendment — 2026-10-13: steps added from 0186 and the owner's answers

This implements the MADR's amendment of the same date. The steps below join the phases named.
Where they conflict with a step above, they govern. Nothing here is approved for execution: the
MADR is still `proposed`, and each phase still needs approval by name.

* **A1, added steps:**
  * **4a.** Make every ACP test path absolute on the running host. Use `t.TempDir()` or a helper
    that maps a fixed POSIX fixture path to the host. The 2026-10-13 scan found 15 POSIX literals
    in `acpagent`'s tests and a few in grok's.
  * **4b.** Rewrite a replayed wire capture's recorded root to the host's on read. Never edit a
    capture. grok's `1.0.13` capture holds 383 POSIX path values.
  * **4c.** At `SessionUpdate`, add the comment that the handler must not send a request (SDK
    B1).
  * **Verification:** add the `go-native` `windows/amd64` CI leg and `make ci-windows` on the
    Windows host, with no new Windows-specific skip.
* **A2, added steps:**
  * **8.** Close is bounded by a timer, not by a write's context (SDK B16). Write a test with a
    fixture agent that stops reading stdin with its pipe full. It shows close returning within the
    bound, and it is seen failing on the current code first.
  * **9.** Closing a session ends its pending permission requests and closes their phone cards
    (SDK B4), with the daemon test seen failing first.
  * **10.** The turn and close paths do not start a typed call with a context that has already
    ended (SDK B9).
  * **11.** Client capabilities become an `acpagent.Spec` field (owner answer Q3). grok's `Spec`
    keeps `fs.readTextFile`, `fs.writeTextFile` and `terminal`, and the capability-honesty test
    iterates every `Spec`.
* **A3 step 6:** the owner pre-approved this run's quota on 2026-10-13 (owner answer Q7), and
  acceptance's `make live-grok` is pre-approved too. The rest of the step is unchanged.
* **A5 step 3:** gobble is at `v0.17.1`, not `v0.14.0`. The step becomes recording the joint
  bump rule for `v0.18.0`.
* **New phase B1 — move to `acp-go-sdk2 v0.18.0`, once it is tagged.**
  1. Read its release notes against the MADR's D4–D8 and its amendment's defect list.
  2. Pin it. Then remove the B16, B4, B9 and B19 mitigations in favor of the SDK's `Close`, writer
     goroutine, cancellation of pending requests on close, and `errors.Is` on
     `ErrPeerDisconnected`.
  3. Handle the removed identity `MarshalJSON` methods, and the gating of extension messages
     received before `initialize`.
  4. Verify as A1 and A2, with `make live-grok` under the MADR's quota rule.
  5. Coordinate the bump with gobble (D12).

## Execution record

### A1, 2026-10-13: implemented and verified on macOS; not staged

**Approval:** the owner on 2026-10-13, "Accept 0184 + 0186, do A1".

**Step 1.** `git ls-remote https://github.com/maccavelli/acp-go-sdk2 'refs/tags/v0.17.1^{}'` gives
`c77f25a45e3345c40f954dd80add9f09a1fd5ae1`, as recorded. No later tag exists.

**Step 2.** `internal/modguard_test.go` was rewritten first.

* `TestGoModRequiresTaggedACPDirectly` rejects:
  * any `replace` of an ACP module;
  * any requirement of the Coder module;
  * any requirement of `acp-go-sdk2` that is not `vX.Y.Z`;
  * a `go.mod` with no `acp-go-sdk2` requirement;
  * any Go file importing the Coder path.
* `TestGoModProblemsRejectsWhatItMust` has 13 fixtures, and `TestImportProblemsFindsTheCoderImport`
  covers the import scan.
* **Seen failing on the unchanged tree.** Four problems on `go.mod`: the Coder requirement, the
  fork `replace`, no `acp-go-sdk2`, and 28 importing files.
* **Seen failing on scratch copies of the migrated tree:**
  * with `replace github.com/maccavelli/acp-go-sdk2 => … v0.17.0` added: "an ACP SDK must not be
    replaced";
  * with `v0.17.2-0.20261013232958-e91d1b8465a1` required: "must be required at a release tag
    vX.Y.Z".

**Step 3.**

* `go.mod` loses the Coder requirement, the fork `replace` and its comment block, and gains
  `github.com/maccavelli/acp-go-sdk2 v0.17.1`. `go.sum` changes 4 lines. No other module moves.
* 28 files' imports were rewritten by a script that asserted none remained. The PLAN said 29; the
  29th was modguard's string literal, now rewritten by hand.

**Step 4.** The 21 production diagnostics are fixed by D3's rules:

* `convert.go` (new) holds `saturatingInt`, `terminalBufferLimit`, `lineWindow` and `exitCode`.
* `AuthMethodId` and `TerminalId` convert at the boundary.
* `DeleteSession` replaces `UnstableDeleteSession`, and `PlanId` replaces `Id` (in a test).
* `convert_test.go` has boundary tests at each type's maximum. Each was seen failing on a scratch
  copy with a bare cast:
  * `saturatingInt(MaxUint64)` gave `-1`;
  * the terminal limit at `MaxUint64` gave `-1`;
  * a line past the end gave the window `[9, 3)`;
  * `exitCode(-1)` gave `4294967295`.

**Steps 4a and 4b (0186 D5).** A scan of the ACP tests found no fixture that feeds the client a
path through a connection's receive side. The two tests that read grok's `1.0.13` capture decode it
directly, so the host path rule does not reach them. No fixture was changed for it. The Windows leg
is the confirmation, and it has not run (see "Not done").

**Step 4c.** `SessionUpdate` carries the comment that it must not send a request (SDK B1).

**Step 5.** `CreateElicitation` and `CompleteElicitation` answer `NewMethodNotFound` (D9).

**Step 6.**

* `clientConnOptions(log)` passes `WithNotificationOverflowPolicy(OverflowDropNewest)`,
  `WithLogger(log)` and `WithMaxMessageSize(10 << 20)`.
* `spawnAgent` passes the provider logger at construction. The comment explaining why `SetLogger`
  was never called is replaced.
* The `unsafe` cast is gone. The seam the M5 routing test observes is kept, as `standardConnOf`
  over `ClientSideConnection.Connection()`, and the tests' wording is updated with their assertions
  unchanged.

**Step 7.** 0167 D19's survival test fails with the overflow option removed, on a scratch copy at
`GOMAXPROCS=1`, `-count=3`: "only 1085 of 1224 frames were read: the connection stopped reading
(closed=true)". It passes with the option.

**Step 8.** `TestSummarizeToolContentRawValues` covers absent, empty, `null`, string, blank string,
number, boolean, object, array, large string and malformed values.

* **Seen failing** with the raw value handed to `shortAny` undecoded: summaries such as
  `in: null · null` and `in: "ls -la" · null`.
* **The defect it pins compiles silently.** acp-go-sdk2 types `rawInput` and `rawOutput` as
  `json.RawMessage` where the Coder SDK had `any`. `shortRaw` now decodes them.

**Deviation — 2026-10-13, found while running A1: the fixtures never ran `initialize`.**

* acp-go-sdk2 refuses every typed request before `initialize`, and drops every typed notification.
* So, on the migrated tree:
  * six auto-mode tests failed ("acp: initialize has not completed");
  * `TestAStalledPumpIsContainedNotHung` failed, because nothing was parked: its updates never
    reached the handler;
  * `TestACPConnectionSurvivesAStalledPump` **passed vacuously,** for the same reason.
* **Fix:** `initfixture_test.go` (new) adds `initializeForTest` and `answerInitialize`, and the
  auto-mode and stalled-pump fixtures complete `initialize` as `spawnAgent` does.
* The scripted rewind fixture uses only the ungated raw paths, and its tests assert exact method
  lists, so it is unchanged.
* **Scope:** this is within A1. It is test fixtures of the changed package, made necessary by the
  migration.

**Verification,** each exit status recorded before reading its output:

```text
go build ./...                                  exit=0
go vet ./...   (darwin/arm64, linux/amd64, windows/amd64, CGO_ENABLED=0 off darwin)   exit=0 each
go test -count=1 -race ./internal/...           exit=0   43 packages ok
gofmt -l <32 changed Go files>                  4 listed (import order), gofmt -w applied, then clean
Python scan of 821 .go files, go.mod, go.sum for the Coder path   exit=0, 0 hits
  (the same scan on a planted directory: exit=1, 1 hit)
```

**Deviation — 2026-10-13, open: the pre-add gate fails on `master`.**

* `./scripts/go-precheck.sh` on the 32 changed Go files exits 1 in its `govulncheck` step. Five
  advisories are found in `golang.org/x/net v0.58.0`, all fixed in `v0.60.0`, none called or
  imported: GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617.
* This is pre-existing. `HEAD`'s `go.mod` already requires `golang.org/x/net v0.58.0`, and A1 does
  not change it.
* `gofmt` and `golint` reported nothing.
* Until it is resolved, no A1 Go file is staged (AGENTS.md, the pre-add rule).
* **Resolved by the owner, 2026-10-13: "Bump x/net under 0174 first".** The bump is
  [0174-PLAN-keep-dependencies-free-of-known-advisories.md](0174-PLAN-keep-dependencies-free-of-known-advisories.md)
  P5, staged and committed on its own, ahead of A1. A1 is then staged on top, and its `go.mod` and
  `go.sum` carry the bump unchanged.
* **After the bump,** in the working tree with A1 and P5 together:
  * `go test -count=1 -race ./internal/...` exits 0, 43 ok;
  * `./scripts/go-precheck.sh` on the 32 changed Go files exits 0: "32 file(s) clean (gofmt,
    golint, govulncheck)";
  * the tree's `go.mod` and `go.sum` differ from P5's staged copies only by A1's ACP lines (8 and
    4).

**Not done:**

* nothing is staged;
* the `go-native` `windows/amd64` CI leg and `make ci-windows` on the Windows host have not run,
  and A1 is not complete until both pass;
* `make preflight` has not run, since A1 changes no Flutter code.
