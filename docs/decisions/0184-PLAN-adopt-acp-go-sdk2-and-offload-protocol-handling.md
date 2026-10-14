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

### A1, 2026-10-13: complete (committed as `db717ed3`, verified on macOS and Windows)

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
* **The commit:** the owner committed P5 and A1 together, as `db717ed3`. This is recorded as a
  deviation in 0174's PLAN, P5 execution. A1's content is as verified above.

**Windows verification, 2026-10-13.**

* `make ci-windows` ran on the Windows test host, in Git Bash, against `db717ed3`. The commit was
  unpushed, so it was sent as a git bundle, cloned into a fresh directory, and checked by full hash;
  the clone, bundle and log were removed afterwards.
* Result: exit 0, "ci-windows-local: ALL SELECTED CHECKS PASSED", with Go 1.27.2 windows/amd64,
  `CGO_ENABLED=0` and `MC_REQUIRE_SYMLINK=1`. Every check passed:
  * A3, the `go.mod` toolchain;
  * A2, the symlink probe;
  * A4, `go build ./...`;
  * A5, `go vet ./...`;
  * A6, `go test ./...` as CI runs it, with every package ok, `internal/provider/acpagent` and
    `internal/provider/grok` included.
* This confirms steps 4a and 4b on the host whose path rule they concern: no ACP test was refused a
  POSIX path on Windows.
* **Not run:** CI's own `go-native` `windows/amd64` leg, which runs on push. The commit is not
  pushed. `make ci-windows` is 0145's local equivalent of that leg.

**Not done:**

* ~~nothing is staged~~ (committed as `db717ed3`, above);
* ~~the `go-native` `windows/amd64` CI leg and `make ci-windows` on the Windows host have not run,
  and A1 is not complete until both pass~~ (`make ci-windows` passed; see "Windows verification".
  The CI leg passed on the push of `54d15365`: run 38493595777, job `Go (windows/amd64)`, success);
* `make preflight` has not run, since A1 changes no Flutter code.

### A2, 2026-10-13: complete (committed as `01c07620`, verified on macOS, Windows and CI)

**Approval:** the owner on 2026-10-13, "Push to master, then proceed to a2".

**Steps 1 and 2 (D4).**

* `Provider.initializeRequest` builds the one initialize request. It sends `clientInfo`
  (`mcremote`, plus the daemon's version, threaded through as the new `Config.ClientVersion` from
  `daemon.Options.Version`; `dev` when empty). Its client capabilities are the `Spec`'s.
* `defaultClientCapabilities` adds `session.configOptions.boolean` to fs and terminal.
* `TestSessionNewAcceptsABooleanConfigOption`, run before the advertisement through the new
  request builder with the old capabilities, failed: "session/new: capability
  session.configOptions.boolean not advertised: variant boolean". It passes after.
* `TestInitializeFrameIsPinned` pins the serialized frame byte for byte, and
  `TestInitializeSendsClientInfo` covers the empty version.

**Step 3.**

* `CheckClientCapabilities`, exported so each `Spec`'s package can use it, walks the advertised
  block. A capability is advertised when its value is `true` or a present `{}`.
  * `auth`, `fs`, `session` and `session.configOptions` are containers, not capabilities. The first
    run showed `auth` always serializes as `{}`.
  * Anything advertised that is not in `implementedClientCapabilities` is a problem.
* `TestCapabilityHonesty` covers the default set, an empty set, and four dishonest sets.
  `TestGrokAdvertisesOnlyWhatMagicImplements` (grok's package) covers grok's `Spec`, which keeps fs
  and terminal.

**Step 4.**

* `supportsList` and `supportsDelete` read the SDK's `Negotiated()` record through
  `session.negotiation` and `allows`.
* The MCP server filter at `session/new` and `session/load` reads the same record.
* `s.agentCaps` is kept for the prompt capabilities and `loadSession`, which this step does not
  name. It is the same initialize response.
* `TestSupportsReadsTheAdvertisedCapabilities` now runs over real initialized connections.

**Step 5.**

* `isMethodNotFound` compares `acp.ErrorCodeMethodNotFound`, and also reads a method-level
  `*acp.CapabilityError` (the SDK refusing an unadvertised method) as method-not-found.
* `isBenignPromptErr` compares `acp.ErrorCodeRequestCancelled`.
* No Magic path classifies `*acp.ValidationError` today. A validation refusal surfaces with the
  SDK's own message, which names the property.

**Step 6.** The new wire harness (`wireagent_test.go`) plays the agent at byte level, over a
connection built with `clientConnOptions`. The SDK's answers, pinned by `wireanswers_test.go`:

| Agent sends | Answer |
| :--- | :--- |
| malformed JSON | `-32700` |
| a request whose `method` is not a string | `-32600` |
| an unknown method | `-32601` |
| `terminal/create` to a `Spec` without terminal | `-32601`, "capability terminal not advertised" |
| `elicitation/create` (not advertised) | **`-32602`**, "capability elicitation.form not advertised: variant form" |
| `fs/read_text_file` with no, empty or relative `path`; `fs/write_text_file` with a relative path | `-32602`, naming the property |
| a line past `maxACPMessageSize` | `-32600`, and the next request is served |

* **Finding: the PLAN's `-32601` for an unadvertised method is not the SDK's answer for
  elicitation.** The SDK gates elicitation by its mode variant, so the answer is `-32602`. `-32601`
  is pinned with `terminal/create` instead, which also exercises a per-`Spec` advertisement (step
  11).
* With the SDK shown refusing bad paths before the handler (a handler refusal would be `-32603`),
  the absolute-path checks in `ReadTextFile` and `WriteTextFile` were deleted. The wire tests pass
  after the deletion.

**Steps 7 and 9 (D8): already true; now pinned.**

* `Cancel` and `Close` already resolve every pending permission and question.
* `awaitDecision` already emits `permission_resolved: cancelled` when the handler's context ends.
* `Close` already announces each pending card cancelled.
* `TestCancelClosesAPendingPermissionCard` and `TestCloseClosesAPendingPermissionCard` pin this
  over a real connection: the card closes cancelled, and the agent's request is answered
  `outcome: cancelled`.
* `stopReason: cancelled` already reached `turn_complete` unchanged.
  `TestACancelledTurnEndsCancelledEitherWay` pins both endings, `stopReason: cancelled` and
  `-32800`.
* **Deviation: no Flutter change or widget test.** The phone already clears a pending card on any
  `permission_resolved` (`apps/mobile/lib/data/chat/transcript_reducer.dart:303-306`), and
  `transcript_reducer_test.dart:556` ("permission_resolved unlocks the composer") already pins the
  cancelled case. The step's "close the phone's card" needed no new code.

**Step 8 (SDK B16).**

* `TestCloseIsBoundedWhenTheAgentStopsReading` uses an agent whose reads stop, so every client
  write blocks. On the code before this step, it **failed**: "Close did not return within 7s of an
  agent that stopped reading". The 2-second context never bounded the write.
* `closeAgentSession` now runs `CloseSession` under a timer (`closeTimeout`, 2 s) that does not
  wait on the write, and the test passes.

**Step 10 (SDK B9).** On the code before this step, both tests **failed**:

* `TestCloseSendsNothingWithAnEndedContext`: "Close with an ended context put "session/close" on the
  wire";
* `TestPromptSendsNothingWithAnEndedContext`: "put "session/prompt" on the wire".

`closeAgentSession` and `submitPrompt` now check the context first. `closeAgentSession` also skips
an agent that did not advertise `close`, which the SDK would refuse locally.

**Step 11.** `Spec.ClientCapabilities` (nil means the default). grok's `Spec` leaves it nil.

**Fail first, on scratch copies.** Every plant failed its test:

* `session.notices` advertised: the honesty tests, default and grok.
* `clientInfo` dropped: the frame pin and the clientInfo test.
* `supportsList` forced true: the supports test.
* The method-level `CapabilityError` branch removed: the method-not-found test.
* An abandoned card marked resolved: the cancel-card test.
* `Close`'s two release routes both removed: the close-card test. Removing one route was rightly
  not caught, since the other still closed the card.
* `-32800` no longer read: the cancelled-turn test.
* The stop reason not passed through: the cancelled-turn test.

Two first-attempt plants broke the build rather than an assertion. They were rewritten to compile,
then failed as required.

**Verification,** each exit status recorded before reading its output:

```text
go test -count=1 -race ./internal/...            exit=0, no failures, no races
./scripts/go-precheck.sh <12 changed Go files>   exit=0 (gofmt, golint, govulncheck)
gofmt -l <12 changed Go files>                   nothing listed
```

* **Windows.** `make ci-windows` ran on the Windows test host, in Git Bash, against the A2 working
  tree. It is uncommitted, so it was sent as a tarball of the 1833 files git lists (tracked and new,
  no ignored file) and removed afterwards. Result: exit 0, "ci-windows-local: ALL SELECTED CHECKS
  PASSED". `go build`, `go vet` and `go test ./...` passed, `internal/provider/acpagent`,
  `internal/provider/grok` and `internal/daemon` included. So the wire tests' path answers hold under
  Windows' path rule.
* **Not done:**
  * ~~no commit: A2 is staged, and a commit needs the owner's word in its turn;~~ committed as
    `01c07620` and pushed;
  * ~~CI on push;~~ CI run 38495795003 on `56f95237` (A2 plus 0170's P7) succeeded on every leg,
    `Go (windows/amd64)` and `Flutter (test on windows)` included;
  * `make preflight`, since A2 changes no Flutter code (see the deviation under steps 7 and 9).

### A3, 2026-10-13: committed as `893a40cc`; one CI leg failed outside A3 (see "Not done")

**Approval:** the owner on 2026-10-13, "committed and pushed, proceed", then "proceed". Step 6's
quota was pre-approved (owner answer Q7).

**Step 1.** `rawpath_test.go`'s `TestStandardMethodsAreTypedOutsideGrokFallbacks` parses the
non-test files of `acpagent` and `grok`. It fails on any untyped call (`rawRequest`, `RawRequest`,
`SendRequest`) whose literal method does not begin with `_`, outside `grok.setModelFallback` and
`grok.setThinkingFallback`. It also fails on a computed method name that is not `"_" + …`. On the
code before this phase it **failed** at three sites:

```text
session.go:967:12: acpagent.SetModel sends the standard method "session/set_model" untyped
thinking.go:70:12: acpagent.SetThinkingLevel sends the standard method "session/set_model" untyped
thinking.go:77:12: acpagent.SetThinkingLevel sends the standard method "session/resume" untyped
```

**Step 2.**

* `acpagent.Spec` has three hooks: `SetModelFallback`, `SetThinkingFallback` and
  `HarvestSessionMeta`. Hooks reach the agent through the new `RawCaller` interface, which the
  session implements.
* The session keeps the agent's last full `configOptions`. It is recorded from session/new and
  session/load (`applySessionState`), from each `set_config_option` answer, and from every
  `config_option_update` (`recordConfigOptions`).
* `SetModel` and `SetThinkingLevel` take the agent's select option in category `model` or
  `thought_level` when it has one. They send `session/set_config_option` and read the value back
  from the answer's `configOptions`. The answer is also forwarded to the phone as `session_config`.
* With no such option they call the `Spec` fallback. With neither, they return an error wrapping
  `provider.ErrNotImplemented`.
* At session open, a standard option's current value outranks the `_meta` harvest. The harvest is
  read only for a setting the agent offers no option for.

**Step 3.** grok's raw `session/set_model`, the `_meta.reasoningEffort` form, the `session/resume`
read-back and the `x.ai/sessionConfig` / `x.ai/sessionDetail` harvest moved to
`internal/provider/grok/fallback.go`, wired into grok's `Spec`. `TestAppliedModelAndEffortFromMeta`
and `TestSetThinkingRequestShape` moved with them. `TestNoExtensionMethodIsWrittenWithoutItsUnderscore`
now allows no standard method on the raw path in `acpagent`.

**Step 4.** Nothing to do: after step 3, no untyped `session/resume` remains outside grok's
fallback.

**Step 5.** `configoption_test.go`, over a real connection to the byte-level fixture agent:

* `TestModelAndThinkingUseStandardConfigOptions`: an agent with both options gets
  `session/set_config_option` frames naming the option id and value. The set answer is the
  read-back, so an answer that keeps the old value fails as "not applied". One `session_config`
  event is emitted per answer. No fallback runs; the fallbacks fail the test if called.
* `TestModelAndThinkingFallBackOnlyWithoutStandardOptions`: an agent without the options gets the
  fallbacks only, and acpagent itself puts nothing on the wire.
* `TestModelAndThinkingWithNeitherPathIsNotImplemented` and
  `TestConfigOptionUpdateMovesModelAndThinking` cover the remaining two paths.
* grok's `fallback_test.go` drives the moved fallbacks through a fake `RawCaller`. That covers the
  accepted id, the refusal, and a resume read-back that differs from the request.

**Step 6. Finding: grok `1.0.50` advertises both standard options.** `make live-grok` ran with
`MCREMOTE_WIRE_CAPTURE_DIR` against `grok 1.0.50 (c58f321264ba) [stable]`. The capture held 1161
agent frames.

* Every session/new and session/load answer carried a select option `model` (category `model`) and
  a select option `reasoning_effort` (category `thought_level`).
* The four `set_config_option` answers carried the same two options, each followed by a
  `config_option_update`.
* Setting the model to `grok-4.5` shrank the `thought_level` option's values to `high`, `medium`
  and `low`, so the answer carries per-model state.
* grok's own error answers: unknown model id `-32602 "unknown model id"`, unknown level `-32602
  "unknown reasoning_effort value"`.
* The committed `1.0.13` capture has no `configOptions` at all.

So on `1.0.50` the standard path is the one that runs, and grok's fallbacks serve only releases
without the options, such as the `1.0.13` known-good pin. `TestLiveGrokSetModelWireContract`,
`TestLiveGrokSetModelMetaReasoningEffort` and `TestLiveGrokSetThinkingLevel` passed through it. The
fallbacks were not exercised live, since no installed grok lacks the options. The fixture and fake
tests above cover them.

The finding is pinned twice:

* `TestLiveGrokAdvertisesStandardConfigOptions` (`live_grok` tag) opens a session and requires both
  categories. It spends no tokens, and it passed on `1.0.50`.
* `TestGrok1050FramesDecodeAsStandardConfigOptions` decodes the committed frames with the SDK's own
  response types and requires both categories in every one.

The capture is committed at `internal/provider/grok/testdata/wire/1.0.50/`, `frames.jsonl` with a
`meta.json` beside `1.0.13`'s. See the deviation below.

**The rest of `make live-grok`: four failures, none from A3.**

| Test | Failure | Cause |
| :--- | :--- | :--- |
| `TestLiveGrokAutoDiscriminationPair` | no permission prompt, no write, in either arm | account: every model call answered `-32603`, "API error (status 402 Payment Required): Grok Build usage balance exhausted" |
| `TestLiveGrokLoopSchedules` | `/loop` reply was 0 bytes | the same 402 |
| `TestLiveGrokPlanApprovalHandOff` | grok never asked to approve a plan | the same 402 |
| `TestLiveGrokColdCatalog` | "cold catalog has 4 options; the live list is being merged with the static floor" | grok `1.0.50` offers four models (`grok-4.7`, `grok-4.7-build-fast`, `grok-4.6`, `grok-4.5`). The same test fails identically on `56f95237` (HEAD), exported to a scratch directory, so it predates A3 |

The 402 is the account's balance, and nothing a code change can fix. The catalog drift belongs to a
grok `1.0.50` surface record, not to this PLAN. Both are left for the owner.

**Step 7.** Pointer amendments, dated 2026-10-13, close
[0039-MADR-grok-acp-parity.md](0039-MADR-grok-acp-parity.md) and
[0106-MADR-grok-1.0.5-surface-parity.md](0106-MADR-grok-1.0.5-surface-parity.md). The MADR's
amendment of the same date records the probe.

**Deviation, 2026-10-13: the committed capture is a verbatim subset, not the whole run.** Step 6
says "Commit the new capture, redacted". The run's capture is 1161 frames. 436 are
`_x.ai/session/setup` and most of the rest are vendor notifications that carry account and
environment state, including the host's own installed skill catalog. Only 14 frames are evidence
for this step. Those 14 are committed, each byte-identical to its captured line except one
redaction:

* kept: the first session/new answer, the first session/load answer, the four `set_config_option`
  answers, the four `config_option_update` notifications, and the four `-32602` refusals;
* redacted: the macOS per-user temp root in three `cwd` values, `/var/folders/<id>/<user>/T/`.
  Values changed, not shape, as 0151's C4 allows.

`meta.json` says both. This also departs from A1 step 4b's "Never edit a capture", so it awaits the
owner's confirmation before commit. The alternative is the whole 1161-frame capture with the same
redaction, after a full identifier review. **Resolved 2026-10-13:** the owner committed the subset
as staged (`893a40cc`).

**Fail first, on scratch copies.** Every plant failed its test:

* The `configOptions` lookup emptied, so the session always falls back: the standard-path test (the
  refusing fallback ran, and no `set_config_option` was sent).
* The requested value returned in place of the answer's: the standard-path test ("not applied" was
  not reported).
* The harvest allowed to override a standard option: the standard-path test (model
  `harvested-model`).
* `config_option_update` not recorded: the update test.
* `rawRequest(ctx, "session/set_model", …)` added in `acpagent`: the raw-path test and the
  underscore test.
* A grok function off the allowlist sending `session/resume`: the raw-path test.
* grok's `Spec` without `HarvestSessionMeta`: the wiring test.
* grok's fallbacks ignoring the accepted model id, or the resume read-back: their fake tests.
* One fixture answer's `model` category relabelled: the fixture test.
* A Windows home path injected into the new fixture: `TestCommittedFixturesCarryNoIdentifiers`.
  The existing guard therefore covers the new directory.

The first try at the harvest plant broke the build, not an assertion. It was rewritten to compile,
then failed as required. The first try at the resume read-back test could not catch its plant: its
read-back answered the requested level. The test now also uses a read-back that differs from the
request.

**Verification,** each exit status recorded before reading its output:

```text
go test -race -count=1 ./...                                   exit=0, no failures, no races
go test -race -count=1 ./internal/provider/{acpagent,grok}/ ./internal/wirecap/
                                                               exit=0 (after the last edits)
./scripts/go-precheck.sh <14 changed Go files>                 exit=0 (gofmt, golint, govulncheck)
make lint                                                      exit=0
go vet -tags live_grok ./internal/provider/grok/               exit=0
make live-grok (wire capture)                                  4 failures, none A3's (above)
go test -tags live_grok -run TestLiveGrokAdvertisesStandardConfigOptions   exit=0
```

* **Windows.** `make ci-windows` ran on the Windows test host, in Git Bash, against the A3 working
  tree, as a tarball of the 1837 files git lists. Result: exit 0, "ci-windows-local: ALL SELECTED
  CHECKS PASSED", with `internal/provider/acpagent` and `internal/provider/grok` both `ok`. The run
  predates the fixture, its two tests and four comment edits. Those ran on macOS only.
* **Not done:**
  * ~~no commit: A3 is staged, a commit needs the owner's word in its turn, and the deviation above
    needs the owner's confirmation first;~~ the owner committed `893a40cc` and pushed it;
  * ~~CI on push;~~ CI run 38498213971 on `893a40cc` failed one leg, outside A3. Six legs passed:
    `Go (windows/amd64)`, `Go (linux/arm64)` and both Flutter jobs among them. `Go (test; build on
    tag)` failed in `internal/ws`:

    ```text
    --- FAIL: TestResumeReturnsOwnedSessionBounds (0.01s)
        resume_test.go:153: want auth_ok got event {"event":{"type":"session_status",...
    FAIL	github.com/maccavelli/magic-cli-remote/internal/ws
    ```

    A3 changed nothing in `internal/ws`, and the test drives the fake provider, not ACP. The
    resuming connection received the fake session's `session_status` before its `auth_ok`, which is
    an ordering race between a session broadcast and the auth reply. It did not reproduce
    locally: `go test -race -count=200 -run 'TestResumeReturnsOwnedSessionBounds$' ./internal/ws/`
    exit 0, and both full local race runs passed. It is recorded here and left for its own record.
    A3 is not marked verified in CI until a clean run;
  * `make preflight`, since A3 changes no Flutter code.

### A4, 2026-10-13: implemented and verified; not committed

**Approval:** the owner on 2026-10-13, "committed and pushed, proceed".

**Step 1.** `sessionupdate_test.go`'s `TestEverySessionUpdateIsMappedOrDeclined` has one row per
field of `acp.SessionUpdate`, keyed by the field's name. It walks the struct by reflection, so a
variant a later SDK adds has no row and fails the test. It covers 19 variants plus `Unknown`.

* Each row decodes a real payload with the SDK's own types. The test checks the payload landed in
  that field.
* It delivers the payload twice through `SessionUpdate`, then requires one of three outcomes:
  * the mapped event;
  * a deliberate drop (`user_message_chunk`, which Prompt already emitted);
  * exactly one Debug record `declined session update` naming the variant.
* No record may be the old `unhandled session update`.

Run on a scratch copy of the code before this phase, it **failed**. That copy added only the two
constants the test names, with no behavior change. Every declined variant fell to the silent
default:

```text
sessionupdate_test.go:150: 2 update(s) fell to the silent default
sessionupdate_test.go:153: declined [], want [plan_update] (once each)
sessionupdate_test.go:153: declined [], want [notice] (once each)
sessionupdate_test.go:153: declined [], want [unknown:future_update] (once each)
```

**Step 2.** The `default:` branch is replaced by one case per variant.

* **Mapped, unchanged:** `agent_message_chunk`, `agent_thought_chunk`, `user_message_chunk`
  (dropped as a duplicate of the prompt), `tool_call`, `tool_call_update`, `plan`, `plan_removed`,
  `available_commands_update`, `usage_update` (`used` and `size`), `current_mode_update` and
  `config_option_update`.
* **Declined:**
  * the variants `plan_update`, `session_info_update`, `notice`, `compaction_update`,
    `compaction_summary_chunk`, `subagent_update`, `session_message` and `session_message_chunk`;
  * `usage_update`'s `cost`, which is declined beside the mapped usage event.
* **`Unknown`** is declined as `unknown:<discriminator>`. The agent chooses those names, so a
  session remembers at most 16 (`maxUnknownUpdateNames`). `TestUnknownUpdateNamesAreBounded` pins
  the cap.
* **A final `default:`** names an update with no variant and no `Unknown` as `empty`. The SDK
  produces that only for a `null` update.
* `declineUpdate` logs at Debug, once per session per variant, with `variant` and `session_id`.
  The set is per session, under its own mutex.

The table's `tool_call_update` row has a terminal status. The tool lane
(`chunkbuf.WithToolLane`) holds a running update for its coalesce window, and the test asks only
whether the case maps.

**Fail first, on scratch copies.** Every plant failed its test:

* a declined case removed: the update falls to `empty`, and the row reports it;
* the once-per-session check removed: each variant is logged twice;
* the Unknown cap disabled: 26 names logged, against the cap of 16;
* the `cost` decline removed;
* a table row deleted: "acp.SessionUpdate.SessionMessage has no row";
* the Unknown name dropped: `unknown:` without its discriminator.

**Verification,** each exit status recorded before reading its output:

```text
go test -race -count=1 ./...                                         exit=0
./scripts/go-precheck.sh session.go sessionupdate_test.go            exit=0 (gofmt, golint, govulncheck)
make lint                                                            exit=0
```

* **Windows.** `make ci-windows` ran on the Windows test host, in Git Bash, against the A4 working
  tree, as a tarball of the 1842 files git lists. Result: exit 0, "ci-windows-local: ALL SELECTED
  CHECKS PASSED", with `internal/provider/acpagent` and `internal/provider/grok` both `ok`.
* **Not done:**
  * no commit: A4 is staged, and a commit needs the owner's word in its turn;
  * CI on push;
  * `make preflight`, since A4 changes no Flutter code.
