---
status: proposed
date: 2026-11-10
associated-madr: "0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement taking provider auth types from the SDK's auth contract, in one cut-over

Associated MADR: [0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)

## Goal

When this plan is complete:

* the daemon pins `authcontract`, and every method, step, answer and error kind on the wire is the contract's (MADR D1,
  D2, D4);
* every interactive sign-in, for every agent, runs through one owned session with an enforced deadline, survives a
  disconnect to that deadline, and is delivered after a reconnect (D3, D5);
* gobble signs in through ACP elicitation on its own connection, with no 30 s or 120 s bound on the way (D8);
* the phone's contract models are generated from the schema, held by a drift gate, and the sign-in sheet renders
  every step kind (D9);
* a daemon and an app of different `app_contract` refuse each other at connect (D6);
* the old messages and types are gone, in one release of the daemon and the app (D11);
* *(2026-11-11, R1; R1 to R4 are the coordinator's labels for the owner's four rulings of that day, defined in the
  MADR's note on `sessionKind` under "What the upstream drafts give")* the phone shows a stored session's kind and each consent's answer, shows a consent's text and
  link before any allow, changes the answer through `gobble auth consent`, and turns a turn refused for a consent into
  a prompt to allow it (MADR D13);
* *(2026-11-11, R2)* every gobble sign-in the phone starts carries `_meta.gobble.remote: true` (MADR D8);
* the MADR's Confirmation 1 to 10 is met, the owner-run runs of D12 included *(2026-11-11: and 11, D13's)*.

This record is `proposed`. Nothing below starts before the owner accepts the MADR and approves this PLAN, whole or by
phase. The owner answered O1 to O4 on 2026-11-10: the contract gains `oauth`, `at_start` and `Method.command` before
`authcontract/v0.1.0` (O1, O3), gobble's `authenticate` takes `_meta.gobble.fresh` (O2), and Magic advertises
form-only elicitation (O4). The steps below follow those answers. The MADR's O5 was answered (a) on 2026-11-10:
the three named pieces of Magic-side logic are accepted, P3's codex and grok code lifetimes among them. The defect fixes this plan follows,
[0074-PLAN-remote-provider-auth-from-phone.md](0074-PLAN-remote-provider-auth-from-phone.md) P23 and
[0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P7, carry no open question: the
owner answered P23's Q-D on 2026-11-10.

## Scope

**Base.** `master` at `912b9aa1`, where the owner's 0217 pin commit landed (`go.mod`, `go.sum`,
`.github/workflows/ci.yml` and the pair of `0217-MADR-update-every-pin-to-its-current-release.md`), so the gate it
was waiting for is met. The 0245 REPORT and its `docs/README.md` row followed as the docs-only commit `3e2f784b`,
which changes no file this plan cites, so every line number here is the same at both. Unstaged:
0179 P4's files (`Makefile`, the four configs, `docs/guides/config.md`, `internal/cli/service/defaults_mcremote.yaml`,
`internal/command/resolve_order_test.go`, `internal/config/prewarm_write.go` and its test,
`internal/provider/registered/registered.go`), the amendments to `0074-MADR-remote-provider-auth-from-phone.md`, `0179-MADR-gobble-native-acp-provider.md` and their PLANs, and an edit to `0217-PLAN-update-every-pin-to-its-current-release.md` *(2026-11-11: the 0074 amendments landed in
`bcde74ab` and the 0217 edit in `eefb7c25`; 0074's P23 step 2 landed as `14200b6e`; the 0179 amendments and P4's files remain
unstaged)*. Untracked:
`internal/provider/gobble/live_gobble_test.go`, `internal/provider/gobble/wirecap_host_test.go`, and this pair and
0247's. This plan starts after those land, and after
[0074-PLAN-remote-provider-auth-from-phone.md](0074-PLAN-remote-provider-auth-from-phone.md) P23 and
[0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P7, whose files it rewrites, and
which the owner's DF answer of 2026-11-10 lands first.
*(2026-11-11, the order within this repository, as the owner approved it that day and as
`0179-PLAN-gobble-native-acp-provider.md`'s entry "P4 — 2026-11-11" records it with its reason:
`0074-PLAN-remote-provider-auth-from-phone.md` P23 steps 3 and 4; then 0179's P4, finished and committed, against
gobble `0.4.0`; then 0074's P23 steps 5 to 9; then 0179's P7a, 7b and 7c, once that MADR's A9 is accepted; then a
release carrying those; then this plan's P1, once G1 holds; then P2 onward, with no release from P2 until P6 (C5);
then `0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md` *(later on 2026-11-11: the owner's
ruling R1 adds P4a, the consent on the phone, between P4 and P5, inside the same window with no release)*. The
acp-go-sdk2 move to `v1.36.0`,
`0217-PLAN-update-every-pin-to-its-current-release.md` P3j, is a commit of its own inside that order; that phase
proposes after 0179's P4 and before its P7a. Three files have more than one editor in that order, which is why it
is fixed: `internal/provider/auth_conformance_test.go` (0179's P4 step 5, 0074's P23 step 5, this plan's P3 step
6), `internal/provider/gobble/auth.go` (0074's P23 step 5, 0179's P7, this plan's P4 step 5) and the `Makefile`
(0179's P4 step 6, this plan's P1 step 4).)*

**In scope:**

* Go: `go.mod`, `go.sum`; `internal/protocol/messages.go`, `errors.go`, `op_timeouts.json` and their tests;
  `internal/protocol/testdata/authcontract/` (copied vectors); `internal/provider/auth.go`,
  `auth_conformance_test.go`; `internal/providerauth/` (`registry.go`, `reservation.go` → `session.go`,
  `ownedflow.go`, `classify.go`, `cli.go`, and the new `scrape/`); `internal/ws/server.go`, `provider_auth_owned.go`,
  `liveness.go`, `resume.go` and their tests; `internal/provider/acpagent/` (`acpagent.go`, `elicitation.go`, a new
  `signin.go`); `internal/provider/codex/`, `grok/`, `kilo/`, `httpagent/`, `opencode/` and `gobble/` auth files and
  tests, and gobble's `errorlimit.go`, `notify.go`, `gobble_test.go` and `fixture_test.go` for acpext `v0.5.0`'s
  spellings (P4); `internal/provider/grok/sdk_boundary_test.go`; `internal/ws/op_timeout_test.go`.
* The pairing path: `internal/ws/server.go`'s `handlePairClaim` and `PairClaimPayload` (P2).
* Scripts and gates: `scripts/gen_authcontract_dart.py` (new); the `Makefile` (`check-authcontract`, in
  `preflight`); `.github/workflows/ci.yml` (the gate in the Go job); `.gitignore` for Python caches if the script
  directory lacks them.
* Dart: `apps/mobile/lib/data/protocol/authcontract.g.dart` (generated), `models.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`, `apps/mobile/lib/data/ws/mc_exception.dart`,
  `apps/mobile/lib/features/settings/` (`provider_detail_screen.dart`, a new `sign_in_sheet.dart` replacing
  `device_flow_sheet.dart`, `provider_auth_sheet.dart`, `upstream_catalog_sheet.dart`), `apps/mobile/test/` (the
  matching tests, `fixtures/authcontract/`, `op_timeout_ladder_test.dart`).
* Docs: `docs/guides/protocol-v1.md` ("Remote provider auth"), `docs/guides/protocol-v2.md`, `docs/architecture.md`.
* Records: this pair; P0's notes; `docs/README.md` by `scripts/check_records.py --write-index`.
* *(2026-11-11, added by the owner's ruling R1, for P4a and P5's consent steps.)* Go: `internal/provider/auth.go`
  (`ConsentEntry`, `ConsentSetter`); `internal/provider/gobble/auth.go` (the status members, `SetConsent`), a new
  `internal/provider/gobble/errorconsent.go`, `errorlimit.go` (the shared decode), `gobble.go` (the Spec's
  `ErrorConsent`); `internal/provider/acpagent/acpagent.go` (`Spec.ErrorConsent`), `host.go`, `session.go`;
  `internal/event/event.go`, `retention.go`; `internal/protocol/messages.go`, `errors.go`, `op_timeouts.json`;
  `internal/ws/server.go`, `op_timeout_test.go`; their tests. Dart: `apps/mobile/lib/data/protocol/models.dart`,
  `apps/mobile/lib/data/chat/chat_models.dart`, `transcript_reducer.dart`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`, `mc_exception.dart`,
  `apps/mobile/lib/features/settings/provider_detail_screen.dart`, a new
  `apps/mobile/lib/features/settings/consent_sheet.dart`, `apps/mobile/lib/features/chat/chat_bubble.dart`; their tests
  and `op_timeout_ladder_test.dart`. Docs: `docs/guides/protocol-v1.md` (the auth block's members, the message, the
  event's members).

**Out of scope:** the pasted-callback replay ([0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md) and its PLAN); the defect fixes of `0074-PLAN-remote-provider-auth-from-phone.md` P23 and `0179-PLAN-gobble-native-acp-provider.md` P7; any
go-llmprovider-sdk or gobble-cli file; a release tag or a push without the owner's word.

## Conventions every phase follows

* **C1. Start state.** Each phase re-reads `git status`, `HEAD`, `origin/master` and `go.mod`'s requirements. Files
  not this phase's are named and left alone: never staged, reverted or reformatted. A difference from this plan is a
  deviation (C6).
* **C2. Line numbers** here and in the MADR are `912b9aa1`'s (identical, for every cited file, to `995d442b`'s, where
  the pair was first drafted), re-read at each phase's start.
* **C3. Seen failing.** Every new test, assertion and gate fails first on a planted break in a scratch clone, never in
  the working tree. The record quotes each failure, with temporary paths as `<TMP>`.
* **C4. Gates on the commit.** Each phase's gates run on a scratch clone of `HEAD` with the phase's files copied in:
  `make pre-add-check FILES="<the phase's Go files>"`, `make test`, `go test -race -count=1 ./...`, `make lint`,
  `make staticcheck`, `make vet-tags`, `make check-records`, `make markdownlint-docs`; for Dart, `dart format
  --output=none --set-exit-if-changed .`, `flutter analyze` and `flutter test` in `apps/mobile`. P3, P4 and P6 touch
  process launch, so the Linux test host runs `go test -race -count=1 ./...` and the Windows test host `make
  ci-windows` on a scratch copy.
* **C5. Commits.** The agent stages by path; the owner commits with `git commit --no-edit`. The disclosure guard runs
  over the outgoing commits before any push the owner asks for (`AGENTS.md`, "Host identifiers never appear in
  records"). No release is tagged from P2 until P6 is committed, so no build carries two vocabularies.
* **C6. Deviations** stop the phase, with a dated entry here and an amendment in the MADR where a decision or fact
  changes.
* **C7. Identifiers.** No hostname, account name, email address or real-machine path in any committed file. The
  redact preview (`python3 ~/.global-git-hooks/github-disclosure.py redact`, from the repository root) runs before
  each staging.
* **C8. Quota and sign-ins.** Nothing in P1 to P6 signs in, calls a vendor, or spends quota. Every live run is P7's,
  owner-run, asked first, in a scratch home, with its quota stated.

## Implementation Steps

### P0 — the records (on acceptance)

1. The MADR `accepted`, with the owner's answers to O1 to O3 (2026-11-10) and O4, dated; this PLAN `in-progress`.
2. Dated notes, each quoting the text it changes and pointing to the MADR:
   * `0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md`: a second "Superseded in part" line under the
     title, for `authcontract` (MADR D1), and a parenthetical in its Confirmation as `0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md` added;
   * `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md`: D1's request-scope bullet amended; D6 affirmed (D8);
   * `0074-MADR-remote-provider-auth-from-phone.md`: D5, D6, D16, D27, D28 and W3 as the MADR's More Information says;
   * `0083-MADR-provider-auth-activation-and-layout-gaps.md` D4; `0107-MADR-grok-phone-api-key-and-device-code-auth.md`
     D3 and D9; `0179-MADR-gobble-native-acp-provider.md` D3, D12 items 8 and 10, D13, D14 gate 3, D16;
     `0086-MADR-phone-provider-auth-completion.md` D6; `0095-MADR-post-0094-assessment-and-debug-pass.md` D7.
3. `python3 scripts/check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview.
   **Stage** the records and `docs/README.md` only.

Acceptance: every note quotes what it changes; `make check-records` exits 0.

### Gate G1 — the contract tag

From a scratch directory outside the repository, `go list -m github.com/maccavelli/go-llmprovider-sdk/authcontract@v0.1.0`
succeeds through the module proxy, and its schema holds the owner's O1 and O3: the `Kind` value `oauth`, the
`Completion` value `at_start`, the `Yields` and `ClientIdentity` values `unknown`, and `Method`'s `command` property;
`Step`'s `secret` and `Answer`'s `secret` (go-llmprovider-sdk
`docs/decisions/0073-MADR-publish-an-auth-contract-module.md` D2); `"x-nullable": true` on exactly the four pointer
members, `Step.default`, `Step.error`, `Answer.secret` and `Answer.confirm` (its D4); and the vector
`answer-confirm-false.json`. Each is read from the tagged `authcontract.schema.json` and `testdata/vectors/`, not
assumed, and the schema's actual keyword set is recorded here and compared with D4's list. Until then P1 waits.
*(2026-11-11: re-checked against go-llmprovider-sdk's 0073 as settled that day: `oauth`, `at_start`, `unknown` for
`Yields` and `ClientIdentity`, `Method.command`, `Step.secret`, `Answer.secret`, the four `x-nullable` pointer members,
`x-open-enum`, and the vector `answer-confirm-false.json` are all still in its D2, D4 and PLAN; nothing was added.)*
*(2026-11-11, later: the owner's ruling on session-kind labels, MADR D13, "The contract's types". The tag must also
hold the contract's session-kind type and its labels (go-llmprovider-sdk 0073-MADR, amendment of 2026-11-11): the four
values `chatgpt`, `chatgpt_plan`, `grok` and `kilo_device`, each with its label, read from the tagged
`authcontract.schema.json`, since P1's generator makes the phone's types from the schema alone. A miss is a stop, as
for the members above: the type missing, a value missing or spelled otherwise, or the labels held in Go only. If the
labels need a schema keyword that P1 step 3's list (0073 D4's) does not hold, that is recorded here and is a
deviation (C6) before P1.)*

### P1 — the pin, the boundary, the vectors and the generator (MADR D1, D9)

1. `go get github.com/maccavelli/go-llmprovider-sdk/authcontract@v0.1.0`, `go mod tidy`. Any other requirement that
   moves is a deviation.
2. `internal/provider/grok/sdk_boundary_test.go` (MADR D1, a gate this cut-over amends): `contractModule = sdkModule +
   "/authcontract"`; the `!inGrok` refusal (`:76-77`) admits it in the packages MADR D1 lists; grok's `sdkAllowed`
   (`:23-26`) gains it with the names `grok/signin.go` will use, so the production-name check (`:100`) passes them;
   every other SDK path stays grok's, with today's two packages and four names.
3. `scripts/gen_authcontract_dart.py`:
   * finds the module with `go list -m -f '{{.Dir}}' github.com/maccavelli/go-llmprovider-sdk/authcontract`;
   * reads `authcontract.schema.json`; refuses any keyword outside the list go-llmprovider-sdk
     `0073-MADR-publish-an-auth-contract-module.md` D4 states and its validator accepts
     (`0073-PLAN-publish-an-auth-contract-module.md` Phase 3 step 4): `$schema`, `$id`, `$defs`, `$ref`, `type`,
     `properties`, `required`, `enum`, `items`, `additionalProperties`, `format`, `x-open-enum` and `x-nullable`,
     naming the keyword;
   * writes `apps/mobile/lib/data/protocol/authcontract.g.dart`: a header naming the module version, read from `go
     list -m` (the schema states none), and "generated; do not edit"; per object a `@immutable` class with `fromJson`
     (absent optional members as `null`, lists as empty), `toJson`, `==` and `hashCode`; per open enum an `extension
     type` over `String` with a `static const` per known value, `values`, and `isKnown`;
   * `toJson` writes a member by the schema alone (MADR D9): a required member always; an `x-nullable` member whenever
     it is not null, `false` included; any other optional member only when it is not its zero value; a `date-time`
     member as the `String` it was read as;
   * writes what `dart format` would write, so the committed file passes CI's Flutter job's `dart format
     --set-exit-if-changed` and `flutter analyze` and the Go job's byte comparison at once (MADR D9);
   * copies `testdata/vectors/**` to `internal/protocol/testdata/authcontract/` and
     `apps/mobile/test/fixtures/authcontract/`, byte for byte;
   * `--check DIR` writes into `DIR` and exits 1 naming each file that differs from the committed one;
   * `--self-test` runs its own cases on in-memory schemas.
4. `Makefile`: `check-authcontract` runs `python3 scripts/gen_authcontract_dart.py --self-test` and `--check` into a
   scratch directory, then, when `dart` is on `PATH`, `dart format --output=none --set-exit-if-changed` on the scratch
   `authcontract.g.dart`; `preflight` runs it after `check-records`. `.github/workflows/ci.yml`: the Go job runs `make
   check-authcontract`, where no Dart toolchain is installed, so it compares bytes only; the Flutter job's existing
   `dart format` and `flutter analyze` steps cover the committed file. (The 0217 commit this edit waited for landed
   at `912b9aa1`; C1 re-reads `ci.yml` first.) `analysis_options.yaml` is not changed unless `flutter analyze` finds
   the generated file at fault for a rule it cannot meet, which is a deviation.
5. The generated file and the copies, committed.
6. Tests, each seen failing (C3):

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestAuthContractVectorsRoundTrip` (`internal/protocol/authcontract_vectors_test.go`) | every copied vector decodes into the type its name gives and re-encodes to the same compact bytes | a copy's `seq` written as a string |
   | `TestAuthContractForwardVectors` (same file) | each `forward/` vector decodes, keeps its unknown enum value, and drops its unknown field | the forward vector's unknown kind replaced by `done` |
   | `TestSDKIsImportedOnlyByGrok` (extended) | `authcontract` allowed in D1's packages, grok's included, refused elsewhere; `x/signin` refused everywhere; grok's production files still limited to their names | `internal/ws` importing `.../llmprovider/x/signin`; `internal/session` importing `authcontract`; a grok production file using a `catalog` name outside `sdkAllowed`'s three |
   | `gen_authcontract_dart.py --self-test` | a schema with `oneOf` is refused; an enum without `x-open-enum` is refused; a keyword outside D4's list is refused by name; output is stable across two runs | the keyword check removed |
   | `make check-authcontract` | regenerated output equals the committed files, and is `dart format`-stable | a hand edit to `authcontract.g.dart`; one byte changed in a copied vector; a mis-indented line in the generator's template (the format check fails; recorded) |
   | `authcontract_vectors_test.dart` (`apps/mobile/test/`) | every vector decodes and re-encodes to an equal JSON value, each member present or absent as in the vector: `answer-confirm-false.json` keeps `"confirm":false`, a `paste` step with `secret` false omits it, `expires_at` comes back as the same string; an unknown `StepKind` is kept and `isKnown` is false | in a scratch copy of the generated file: `fromJson` dropping `expires_at`; `toJson` omitting `Answer.confirm` when `false`; `expires_at` re-formatted through `DateTime` |
   | `flutter analyze` (CI's Flutter job, and C4) | clean over `apps/mobile`, the generated file included | an unused import planted in a scratch copy of the generated file |

7. Gates (C4). **Stage.**

Acceptance: the tests pass and each plant failed; no file outside step 1 to 5's names changed.

### P2 — the envelope, one lifecycle, and the version check (MADR D2, D3, D5, D6)

1. `internal/provider/auth.go`: `SignIn` and `SignInStarter` (MADR D5's code), and the host-reason constants of D4
   (`ReasonHostOnly`, `ReasonNotWired`, `ReasonInputUnsupported`, `ReasonUnsupportedBackend`, `ReasonAppUnsupported`).
2. `internal/providerauth/session.go` (from `reservation.go`): `Registry.Admit(deviceID, providerID, upstreamID,
   methodID)` before any start, refusing a second session for one (provider, upstream) with `provider_auth_busy`
   besides the per-device and total limits (MADR D5); `Session.Attach(SignIn)`; one owner goroutine, under a context
   from the server's lifetime, never the start request's, that forwards steps, calls `Wait` once, and records the
   terminal step; the deadline of D5 (`DefaultTTL` from admission until a step carries `expires_at`, then that
   `expires_at`, recomputed, plus 60 s; paused while `coordinator_state` is `ready_to_activate`, bounded there by
   `ActivationGrace`), on a timer of its own; `Detach` without a resume-window timer; `Retain` and `Drain` of
   terminal steps, a `failed` step's message replaced by a fixed one per kind; `CancelAll` and `WaitAll` as today.
3. `internal/protocol/messages.go`: D3's seven payloads; `AuthAnswerPayload`'s `String`, `GoString`, `LogValue`
   delegating to the contract `Answer`; `SetCredentialPayload` gains `GoString`, masking `Secret` as its `String`
   does (MADR D3, "Redaction"; it has none today, `messages.go:866`, `:879`); `AuthPayload.AppContract`,
   `PairClaimPayload.AppContract` and `Caps.AppContract`; `AppContract = 1`.
   `internal/providerauth/session.go` sets each inbound answer's `Secret` from the step the session holds for its
   `seq`, before anything logs or forwards it (MADR D3, "Redaction"), and refuses an answer to a shown step
   (`open_url`, `show_code`, `waiting`) with `invalid_answer`.
   `errors.go`: D3's codes, registered.
   **The op-timeout table, in four files, one commit** (MADR D3): `internal/protocol/op_timeouts.json` gains
   `provider.auth_start` 30000, `provider.auth_answer` 15000 and `provider.auth_sessions` 30000, and its `_comment`'s
   inline list gains `provider.auth_cancel`; `internal/ws/server.go`'s `asyncOpTimeout` (`:1111`) gains the
   `provider.auth_answer` case at 15 s (the other two take the 30 s default); `apps/mobile/lib/data/ws/mcremote_client.dart`'s
   `opTimeoutFor` (`:411`) gains `provider.auth_answer` beside `question.respond`; `apps/mobile/test/op_timeout_ladder_test.dart`
   then passes, as it requires every row's phone timeout to be the daemon's plus the margin. `provider.start_auth`'s
   row stays until P6, since D3's removal applies at the cut-over.
4. `internal/ws/server.go`: handlers for `provider.auth_start` (async, 30 s: admit, start, attach, write `ok`, then
   events through `sendToDevice`), `provider.auth_answer` (async, 15 s), `provider.auth_cancel` (inline),
   `provider.auth_sessions` (async, 30 s); `handleAuth` refuses a V2 client whose `app_contract` differs, before
   `authenticate`, without counting a strike; `handlePairClaim` refuses one the same way before the one-shot code is
   consumed (`:1288-1294`'s place), and `pair_ok`'s caps carry `app_contract` (MADR D6); the disconnect path calls
   `Registry.Detach`; `authErrCode` maps the coordinator sentinels and `ErrTooManyFlows` to D3's codes.
5. `docs/guides/protocol-v1.md`: the new messages, the method entry, the reasons, `app_contract`. The old messages
   stay documented until P6.
6. Tests, each seen failing:

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestSessionAdmitsBeforeStart` (`providerauth`) | a refused admission starts nothing | admission after `Attach` |
   | `TestSessionDeadlineCancels` | a session past its deadline is cancelled and ends `failed`, kind `expired` | the timer not armed |
   | `TestSessionDeadlineFromFirstExpiringStep` | a session whose first step is a `form` with no `expires_at` keeps `DefaultTTL`; when a later `show_code` carries `expires_at`, the deadline becomes it plus 60 s | the deadline read from the first step only |
   | `TestSessionOutlivesItsStartRequest` (`internal/ws`) | after `provider.auth_start` answers and its op context is cancelled, the session's steps still reach the phone and it ends `done` | the session run under the op context |
   | `TestOneSessionPerProviderUpstream` | a second `Admit` for one (provider, upstream) is `provider_auth_busy`; another upstream of the same provider is admitted | the per-(provider, upstream) check removed |
   | `TestAnswerToAShownStepRefused` | an answer whose `seq` is an `open_url` or `show_code` step's is `invalid_answer`, and nothing reaches the `SignIn` | the shown-step check removed |
   | `TestSessionDeadlinePausedWhileActivating` | a `ready_to_activate` session outlives its code's deadline, and `ActivationGrace` ends it | the pause removed |
   | `TestDetachedSessionLivesToItsDeadline` | a detached session is alive after the resume window and ends at its own deadline | `Detach` arming the resume window |
   | `TestTerminalStepsDrainOnce` | `Drain` returns a retained step once; a second call none | `Drain` not deleting |
   | `TestRetainedFailureHoldsNoAgentText` | a retained `failed` step holds the fixed message, not a planted sentinel from the agent's error | the agent's message kept |
   | `TestAuthSignInOverWire` (`internal/ws`) | `auth_start` → `ok{session_id}` → events → `auth_answer` → `done`, against a fake `SignIn`; an answer for another device's session is `no_such_sign_in` | the ownership check removed |
   | `TestAuthCancelIsInlineAndIdempotent` | two cancels both answer `ok`; the fake's `Cancel` runs once | a second cancel erroring |
   | `TestAuthSessionsAfterReconnect` | a session that ended while its device was away is in `finished` once | `PendingResults` not called |
   | `TestAuthAnswerNeverLogged` | a planted paste sentinel is in no log line, no error frame and no retained record, including when the phone sends the answer with `secret: false` for a `paste` step and when it answers a `seq` the session does not hold | `AuthAnswerPayload.LogValue` printing `paste`; the phone's `secret` kept instead of the step's |
   | `TestAppContractMismatchRefused` | a V2 `auth` carrying an invalid token and another `app_contract`, or none, gets `app_outdated`, not `writeAuthError`'s refusal, and `failedAuths` stays 0; a V1 `Bearer` client is unaffected | the check after `s.authenticate` (`server.go:1185`), which then refuses the invalid token and counts a strike |
   | `TestPairClaimAppContractRefused` | a V2 `pair.claim` with another `app_contract` gets `app_outdated` and its pairing code is still claimable; a matching one gets `pair_ok` whose caps carry `app_contract` | the check after the code is consumed |
   | `TestSetCredentialPayloadGoString` (`internal/protocol`) | `%#v`, `%v`, `%+v` and `%s` of a `SetCredentialPayload` and its pointer hold no planted secret | `GoString` removed (today's state, so the test is also seen red at `912b9aa1`) |
   | `TestAsyncOpTimeoutMatchesSharedTable` (existing) | D3's rows match `asyncOpTimeout` | one row's value changed |
   | `op_timeout_ladder_test.dart` (existing) | every row of the table, the new ones included, has a phone timeout of the daemon's plus the margin | `provider.auth_answer` left out of `opTimeoutFor` (it falls to the 30 s default) |

7. Gates (C4). **Stage.**

Acceptance: the tests pass and each plant failed; the existing provider-auth tests pass unchanged (the old path is
untouched until P6); the op-timeout tests on both sides pass with the new rows.

### P3 — the mappers for codex, grok, kilo and opencode, and the quarantined scraper (MADR D4, D7)

0. **Probe P-C, before step 2's `token_paste` (MADR "Not verified").** `codex login --with-access-token` with a real
   access token on stdin, in a scratch `CODEX_HOME`, then `codex login status` there and a read of the scratch
   `auth.json`'s shape (key names and modes only, no value recorded). It records whether the result is a refreshable
   session or access-only, and whether codex's adapter `Validate`s it and `NotOlder` would adopt it
   (`codex/adapter.go:150-159`; `providerauth/adapter.go:71-87`). *Owner-run; asks first; scratch `CODEX_HOME`; a
   dummy value fails, so a real token is needed; quota: none.* Its result sets `token_paste`'s `yields` and
   `refreshable` in step 2. An access-only result that the coordinator cannot adopt amends MADR D7 before step 2
   builds `token_paste`; until the probe is recorded, `token_paste` is not built and the rest of P3 goes on.
1. `internal/providerauth/scrape/`: `StripANSI`, the URL pattern, the code patterns and `Classify`'s URL rule, moved
   from `cli.go:18-27` and `classify.go:52-125` with their tests. `cli.go` and `classify.go` call them.
2. **codex** (`internal/provider/codex/signin.go`): `Methods` (MADR D7's codex row: `api_key`, `token_paste`,
   `device_code` and `browser`, each with `slot` `openai`) and `StartSignIn` for `device_code`, wrapping the owned
   flow: `show_code` with `expires_at` 15 minutes from the code, `waiting`, `coordinator_state` from the flow's
   updates, `done` or `failed`. `token_paste`, after step 0, through `SetCredential`: `codex login
   --with-access-token` with the token on stdin, inside a coordinator transaction as the device flow's candidate is.
3. **grok** (`internal/provider/grok/signin.go`): `Methods` (the API key and the OAuth methods with no shared `slot`)
   and `StartSignIn` for `device_code` (owned when a coordinator exists, the Spec's CLI flow otherwise,
   `section.go:139-145`), `expires_at` 600 s; `browser` listed `host_only`.
4. **kilo** (`internal/provider/kilo/signin.go`, `httpagent/deviceauth.go`): `Methods` from `/provider/auth` with ids
   by label (from `0074-PLAN-remote-provider-auth-from-phone.md` P23), each `oauth` entry `authcontract.KindOAuth` with `CompletionAtStart` (MADR D7, O1);
   a `form` step first for a method with prompts; `authorize`; the URL decides (`show_code` and `waiting`, or `failed`
   `unsupported` for a loopback URL until 0247 adds its paste); polling as today (`deviceauth.go:126-170`).
5. **opencode** (`internal/provider/opencode/signin.go`): `Methods` from the integrations; `oauth` as `KindOAuth`
   with `CompletionAtStart` (O1); `command` and `external` as `KindHostOnly` with `CompletionHostCommand` and the
   command in `Method.Command` (O3); attempts as today, `mode: "code"` `failed` `unsupported` until probe U6.
6. **Which agents are wired** (MADR D5): `provider.SignInWired(p any) bool` in `internal/provider/auth.go`, asking an
   optional `SignInCapable` first and the type assertion otherwise, as `UpstreamSwitchWired` does
   (`internal/provider/auth.go:263`); `acpagent.Provider` implements it from `Spec.SignInMeta != nil ||
   Spec.StartSignIn != nil`, `httpagent` from its dialect. `TestProviderAuthInterfaceConformance` gains a
   `SignInStarter` column and a `wantSignInWired` column for every agent.
6a. **The method entries on the wire** (MADR D4): `provider.UpstreamAuth.Methods` (`internal/provider/auth.go:126`)
   becomes `[]provider.MethodEntry{Method authcontract.Method; Available bool; Reason string; Configured *bool}`;
   each mapper of steps 2 to 5 sets `Available` and `Reason` from its facts (`host_only`, `input_unsupported`,
   `unsupported_backend`); `authStatePayload` and `upstreamAuthPayload` (`internal/ws/server.go:2332`, `:2365`) copy
   the entries with no type switch, adding only `not_wired` where `SignInWired` is false; the `provider.auth_catalog`
   handler (`server.go:847`, `handleAuthCatalog` at `:2447`) serves the same entries; `protocol.ProviderAuthPayload`
   keeps `backup_state` and `recovery_available` (`0074-MADR-remote-provider-auth-from-phone.md` §16 D42). The old `AuthMethodPayload` is still sent
   beside it until P6, from the same entries, so the phone of this commit keeps working.
7. Tests, each seen failing:

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestCodexDeviceSignInSteps` | a stub CLI's code becomes `show_code`, then `waiting`, then `done`; `expires_at` is 15 minutes out | `expires_at` from the reservation |
   | `TestCodexAccessTokenOnStdin` | the token reaches stdin, never argv, the environment or an error | the token in argv |
   | `TestGrokDeviceSignInSteps` | `expires_at` 600 s; a stub that prints no code fails within 30 s | the scan bound removed |
   | `TestKiloOAuthDecidedAtStart` | every `oauth` entry is listed `kind` `oauth`, `completion` `at_start`, with no `paste`; a device URL gives `show_code`; a loopback URL gives `failed` `unsupported`; no label is read | the label rule restored; an entry listed `device_code` |
   | `TestKiloPromptsAreAFormFirst` | GitHub Copilot's prompts become one `form` step whose answer is `authorize`'s `inputs` | the form skipped |
   | `TestOpencodeCommandIsHostOnly` | a `command` and an `external` method are `host_only`, `host_command`, with the command in `Method.Command` and not in `detail`; an `oauth` method is `oauth`, `at_start` | `Command` empty; the command copied into `detail` instead |
   | `TestOnlyScrapeCompilesOutputPatterns` | no file in `internal/providerauth` outside `scrape/`, and no auth file under `internal/provider` (`*auth*.go`, `signin.go`, `device_auth.go`, `owned_device_auth.go`), compiles a regular expression; `stderrtail` and `agenterr` are out of its scope (MADR D7) | a `regexp.MustCompile` in `grok/signin.go` |
   | `TestProviderAuthInterfaceConformance` (extended) | every agent is a `SignInStarter`, and `SignInWired` is true exactly for the agents whose Spec or dialect wires a sign-in | grok's Spec hook set nil in the test's provider, so its `wantSignInWired` true fails (a type-level removal cannot be planted: every `acpagent` agent shares one type) |
   | `TestCodexMethodsShareASlot` | codex's four methods carry `slot` `openai`; grok's API key and OAuth methods share none | `token_paste` without a `slot` |
   | `TestMethodEntriesOnTheWire` (`internal/ws`) | a golden of `providers.list_result`'s auth block for each agent: every entry is `{method, available, reason?, configured?}`; one case per reason (`host_only`, `not_wired`, `input_unsupported`, `unsupported_backend`); a browser method marked available by its mapper is sent available | the old type switch restored in `upstreamAuthPayload`, which marks the browser method `browser_only` |

8. Gates (C4), the Linux and Windows test hosts included. **Stage.**

### Gate G2 — gobble's release

`go list -m github.com/maccavelli/gobble-cli/acpext@v0.5.0` succeeds through the proxy, a gobble release at or above
`v0.6.0` (gobble-cli `0051-PLAN-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md`'s rollout) is installed on the host, and its `gobble acp` advertises
`_meta.gobble.methods` at `initialize` (read with `initialize` alone, no sign-in, no quota). The release's acpext
`AuthenticateMeta` has the member `fresh` (gobble-cli `docs/decisions/0051-PLAN-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md`
P3), read from the tagged source, since the owner's O2 is what P4's `SignInMeta` sends. The release pins the same
acp-go-sdk2 tag as this repository (`0179-MADR-gobble-native-acp-provider.md` D14 gate 3): this repository pins
`v1.35.0` at `912b9aa1` (`go.mod`), and gobble `v0.5.0` pins `v1.34.2`, so the gate is read from the G2 release's
`go.mod`, and a mismatch is a stop.
*(2026-11-11, the owner's rulings of that day, relayed by the cross-repository coordinator: the G2 release is
gobble-cli `v0.6.0`, and the tag both sides must pin is acp-go-sdk2 `v1.36.0`. This repository moves to it under
`0217-PLAN-update-every-pin-to-its-current-release.md` P3j, proposed, and gobble-cli's `main` under its own record
0054; no interim gobble release is cut. Until `v0.6.0`, gate 3 is knowingly unmet
(`0179-MADR-gobble-native-acp-provider.md` A10): this repository has pinned `v1.35.0` since `912b9aa1`, gobble's
releases `v0.4.0` and `v0.5.0` pin `v1.34.2`, and gobble-cli's `main` pins `v1.35.0`. At this gate the release's
`go.mod` and this repository's must both read `v1.36.0`. Anything else is a stop, a release tagged `v0.6.0` on
another tag included. The gate is verified again at P7 step 3.)*
*(2026-11-12, the owner's decision of that day, recorded in gobble-cli
`0055-MADR-release-v0-6-0-now-and-the-sign-in-redesign-as-v0-7-0.md` (accepted) and relayed by the cross-repository
coordinator: gobble-cli releases `v0.6.0` now, from its `main`, and the ACP sign-in redesign this gate waits for
(gobble-cli's 0051 and 0052) is released as `v0.7.0`. Read this gate with `v0.7.0` for the redesign's `v0.6.0`: "a
gobble release at or above `v0.6.0`" is "at or above `v0.7.0`"; "the G2 release is gobble-cli `v0.6.0`" is `v0.7.0`;
"a release tagged `v0.6.0` on another tag" is one tagged `v0.7.0`. The gate-3 check stays, read from `v0.7.0`'s
`go.mod`, which must read `v1.36.0` as this repository's does; anything else is a stop, as above. "Until `v0.6.0`,
gate 3 is knowingly unmet" names the release cut now: both sides pin `v1.36.0` (this repository since `83aac453`, P3j
having landed; gobble-cli's `main` since its `f441c73`), so gate 3 is met when `v0.6.0` is published
(`0179-MADR-gobble-native-acp-provider.md` A10's note of this date). `v0.6.0` does not open G2: it adds no
`_meta.gobble.methods`, and the acpext it is built with has no `fresh` (that record's F5, F6), so an unamended reading
of this gate would stop on it.)*
*(2026-11-11, later the same day, the owner's rulings R1, R2 and R4, relayed by the coordinator. Each is read from
the G2 release's tagged source, and from its `--help` or its tests where a behaviour is not a type, never assumed;
nothing signs in and no quota is spent. A miss is a stop, recorded as a deviation (C6), except where a bullet says
that its finding is recorded only: those are the MADR's "Dependencies D13 found" items 1 and 3, without which D13
shows what it shows today.)*

* *(R2)* acpext's `AuthenticateMeta` has the member `remote`, and gobble's release builds the rule of gobble-cli
  `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D3: with `remote: true` it signs in with no
  loopback listener and offers no method without a paste. The test gobble's PLAN names for it is read, by name.
* *(R1)* `gobble auth status --json` has `sessionKind` and `consents` on its rows (gobble-cli
  `0052-MADR-finish-the-chatgpt-plan-sign-in-on-the-sdk-engine.md` D8), read from its golden file.
  *(2026-11-11, later: aligned with gobble-cli's final 0052 D8. The golden file's plan row also carries
  `consentTerms`, each entry an `authcontract.Consent` with `id`, `required`, a non-empty `text` and a `links`
  entry; `consents` is an object from id to `granted` or `declined`; an unreadable store is 0045-MADR D5's error
  object with exit 2. A release without `consentTerms` is a stop: the phone would have no text to show before an
  allow.)*
* *(R1)* `gobble auth consent <provider> <consent> allow|deny` exists, accepts the consent's id
  (`chatgpt_plan.background_use`) as `<consent>`, asks nothing when both arguments are given, and exits 0, 1 for a
  store it cannot read or lock, and 2 for a usage error (its D3). Whether it accepts the id is not in gobble's records
  (MADR, "Dependencies D13 found", item 2), so this is read from the source; if it takes only gobble's word
  `background`, P4a stops before step 3.
  *(2026-11-11, later: aligned with gobble-cli's final 0052 D3, whose note of that day now states that
  `<consent>` is the SDK's id, `background` its alias, and the exits: 0 stored; 2 for each usage error; 1 for a
  store that cannot be read, parsed or locked; one output line, no `--json`. The gate reads the release's source
  and tests to confirm that the tagged command accepts `chatgpt_plan.background_use`; a release that takes only
  `background` is still a stop before P4a step 3.)*
* *(R1)* acpext's `ErrorData` has `consent` (`json:"consent,omitempty"`), set with kind `not_permitted`, at
  `acpext/v0.5.0`.
  *(2026-11-11, later: aligned with gobble-cli's final 0051 D7: gobble tags it `json:"consent,omitzero"`, as
  every member of `ErrorData` is; for a string the wire is the same as `omitempty`, and either tag passes.)*
* *(R1)* `_meta.gobble.methods` at `initialize` carries `chatgpt_plan` with its `consents`, each with a `text` and a
  `links` entry. Which wording that text is, the SDK's default or gobble's own, is recorded only (MADR, "Dependencies
  D13 found", item 1).
  *(2026-11-11, later: aligned with gobble-cli's final 0052. D13 no longer reads a consent's text from here but
  from `consentTerms` (the first R1 bullet's note). The bullet stays, for the sign-in's catalog; its wording finding
  is now whether the text equals `consentTerms`' and the `confirm` step's, one wording by 0051 D4's note, recorded
  only.)*
* *(R1, recorded only)* Whether the turn gobble ends on an overflow without a grant, and the one it ends when the
  answer cannot be read, carry `ErrorData.kind` `not_permitted` and `consent` (MADR, "Dependencies D13 found",
  item 3), read from gobble's tests of those errors.
  *(2026-11-11, later: aligned with gobble-cli's final 0052 D2, which now states both: the overflow carries
  `consent`, the unreadable answer does not. Still recorded only; a release that differs is a deviation for the
  owner, D13 then showing gobble's message as today.)*
* *(R4)* The release's `initialize` answer advertises `sessionCapabilities.systemPrompt` (gobble-cli
  `0054-MADR-go-1-27-4-and-sibling-pin-refresh.md`, by the owner's ruling of 2026-11-11). This changes gobble's
  `initialize` frame from `0.4.0`'s, whose `sessionCapabilities` are `additionalDirectories`, `close`, `list` and
  `resume` (`internal/provider/gobble/testdata/wire/0.4.0/frames.jsonl`, its first frame). The daemon sends no
  `systemPrompt` (`0217-PLAN-update-every-pin-to-its-current-release.md` P3j step 3 holds it), and the new frame is
  covered by P4 step 6's recapture (P7's L4). No fixture test reads `sessionCapabilities` (`fixture_test.go:156-161`
  reads `_meta` only, at `14200b6e`).

### P4 — gobble through ACP elicitation (MADR D8)

1. `go get github.com/maccavelli/gobble-cli/acpext@v0.5.0`, `go mod tidy`. `v0.5.0` respells acpext's kinds and
   causes to the contract's words and removes `RetryEmpty` (gobble-cli
   `docs/decisions/0051-PLAN-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` P3 step 2), so in the same
   commit:
   * `internal/provider/gobble/errorlimit.go:46` compiles unchanged against the respelled `acpext.KindRateLimited`,
     and now matches gobble's `rate_limited`; its test cases' captured `ErrorData` come from the recaptured fixtures;
   * `internal/provider/gobble/notify.go`'s `retryStartText` prints the cause as sent, unchanged in code, so
     `gobble_test.go:242`'s expected text becomes `"gobble is retrying (attempt 2 of 5) in 4s: rate_limited."`;
   * `internal/provider/gobble/fixture_test.go` reads the G2 release's fixtures (step 6). The `0.4.0` fixtures hold the
     old spellings, so **the owner's recapture, P7's L4, precedes this phase's commit**: the commit carries the new
     fixtures and the pin together, as `0179-MADR-gobble-native-acp-provider.md` D13 requires ("fixtures re-captured
     from the gobble release that carries it"). If the owner defers the recapture, this phase stops, and a dated
     deviation, with the owner's word, records an exemption for the `0.4.0` fixtures instead.
2. `internal/provider/acpagent/signin.go`, agent-neutral:
   * `Provider.StartSignIn(ctx, upstreamID, methodID)` when the Spec sets `SignInMeta`: a process from the Spec's
     argv; a connection with a sending middleware that records `authenticate`'s id through
     `acp.RequestIDFromContext`; `initialize` with the Spec's client capabilities under `startTimeout`; then
     `authenticate` with `methodId` and `_meta` from `Spec.SignInMeta`, under a context from the server's lifetime
     with the session's deadline; the process killed when `authenticate` answers or the session ends;
   * a client whose `CreateElicitation` relays a request-scoped form only under MADR D8's three conditions, decoding
     the step through `Spec.SignInStep` and answering through `Spec.SignInAnswer`; a shown step (`open_url`,
     `show_code`) is acknowledged with an empty `accept` once its `provider.auth_event` is written, and builds no
     `Answer` (MADR D8); every other request is refused as a session client refuses it; no `awaitQuestion`.
3. `elicitation.go`: unchanged for sessions; its comment names this pair's MADR, D8, for the sign-in client.
4. `internal/provider/gobble/signin.go`: `SignInMeta` (`{"gobble": {"method": <contract id>, "fresh": true}}`, through
   `acpext.AuthenticateMeta`; MADR D8, O2; *2026-11-11, R2: `{"gobble": {"method": <contract id>, "fresh": true,
   "remote": true}}`, `remote` always true, MADR D8's "A phone sign-in is always remote"*), `SignInStep`
   (`_meta.gobble.step` through `acpext.MetaStep`, strict), `SignInAnswer` (`callback`, `allow`, fields); the catalog
   from `_meta.gobble.methods` (`acpext.MetaMethods`), kept per gobble version and filled by an `initialize`-only run,
   a `browser_loopback` method listed `host_only` (MADR D7). `KnownGoodVersion` the release of G2.
5. `gobble/auth.go`: `startDeviceAuth`, `firstDeviceCode` and `loginEvent` go; so do the two id tables
   `deviceSignIn` and `noPhoneSignIn` and their partition test `TestDeviceTablesPartitionAcpextsLogins`
   ([0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) A9.2, which removes them at this
   cut-over), since the catalog is `_meta.gobble.methods`; key writes, logout and status stay. `git grep -n
   "deviceSignIn\|noPhoneSignIn" internal/` then finds nothing.
6. Fixtures: `internal/provider/gobble/testdata/wire/<G2 version>/`, recaptured under the D13 rule of
   `0179-MADR-gobble-native-acp-provider.md`, by the owner's word only (C8, P7's L4 runs it), before this phase's
   commit (step 1).
   *(2026-11-11: the fixtures hold gobble's frames, not the requests Magic sends, so `_meta.gobble.remote` (R2)
   appears in none of them, nor in the contract's golden vectors, which carry no `_meta` (MADR D2); step 7's sign-in
   tests assert it on the `authenticate` params the `acptest` fake records. The recaptured `initialize` frame differs
   from `0.4.0`'s by `sessionCapabilities.systemPrompt` (R4, G2's note of this date), and no change to a fixture
   test follows from it.)*
7. Tests, each seen failing:

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestSignInRelaysRequestScopedForms` (`acpagent`, on `acptest`) | each row of MADR D8's table reaches the session as its step, and the answer as its content; an `open_url` with `accepts` is acknowledged with an empty `accept`, and the paste reaches the agent as the `paste` form's `callback` *(2026-11-11, R2: and the `authenticate` params the fake records carry the Spec's `_meta` unchanged, `remote` included)* | `SignInAnswer` skipped; the paste sent as the `open_url` form's content; *(R2)* `_meta` dropped from `authenticate` |
   | `TestSignInAcknowledgesShownSteps` (`acpagent`) | a `show_code` form is answered `accept` with empty content once the step is written to the phone, and no `Answer` reaches the session's `SignIn`; a phone answer naming its `seq` is `invalid_answer` | the form left open until the agent cancels it |
   | `TestSignInRefusesAForeignRequestID` | a request-scoped form naming another request id is answered `cancel` and declined `elicitation.request_scope` | the id check removed |
   | `TestSignInOutlivesTheHandshakeBound` | with `Spec.StartTimeout` 200 ms, a sign-in held 2 s completes | `authenticate` under `initCtx` |
   | `TestSignInHasNoQuestionTimeout` | with `PermissionTimeout` 20 ms, a `paste` form answered after 200 ms completes | the form routed through `awaitQuestion` |
   | `TestSignInCancelSendsCancelRequest` | a phone cancel answers the open form `cancel` and the agent sees `$/cancel_request` for `authenticate` | the context not passed |
   | `TestSignInStepTooNew` | a step whose `schema_version` is above the pin fails the session `unsupported` | the check removed |
   | `TestSessionRequestScopedFormStillCancelled` | outside a sign-in connection, a request-scoped form is cancelled as `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` D1 says | the relay applied to sessions |
   | `TestGobbleCatalogFromInitializeMeta` (`gobble`) | the catalog is `_meta.gobble.methods`, decoded strictly | `Login` read instead |
   | `TestGobbleRetryTextUsesTheContractWord` (`gobble_test.go`, the existing case at `:242`, changed) | the notice reads `…: rate_limited.` | the expectation left `rate-limited`, which fails at `acpext/v0.5.0` |
   | `TestGobbleSignInIsFresh` (`gobble`) | every `authenticate` the sign-in connection sends has `_meta.gobble.fresh: true` and the method's contract id | `fresh` omitted |
   | *(2026-11-11, R2)* `TestGobbleSignInIsRemote` (`gobble`) | every `authenticate` the sign-in connection sends has `_meta.gobble.remote: true` beside `method` and `fresh: true`, for every method of a fake `_meta.gobble.methods`, a device and a `browser_paste` one included | (a) `remote` omitted from `SignInMeta`; (b) `remote` set from the method's `completion` (true for `browser_paste` only), so the device case fails |
   | `TestGobbleLoopbackMethodIsHostOnly` (`gobble`) | a `_meta.gobble.methods` entry whose `completion` is `browser_loopback` (gobble's grok browser) is listed `available: false`, `reason: host_only`; a `browser_paste` one is available | the entry listed as gobble sent it, available |
   | `TestNoProviderIsNamedInAcpagent` (existing, `acpagent/neutral_test.go:22`) | unchanged | the string `_meta.gobble` planted in `signin.go` (its pattern, `:16`, names agents: `callback` would not fail it) |
   | `TestSignInKeysAreTheSpecs` (`acpagent`, new) | no non-test file of `acpagent` holds the string literal `"callback"` or `"allow"`: gobble's content keys live in gobble's Spec hooks (MADR D8) | `"callback"` planted in `signin.go` |

8. Gates (C4), the Linux and Windows test hosts included. **Stage.**

### P4a — the consent on the phone, daemon side (MADR D13)

*Added on 2026-11-11 by the owner's ruling R1 of that day, relayed by the coordinator. It runs after P4, whose
`acpext/v0.5.0` pin and kept catalog it needs, and before P5, inside the window with no release (C5). It reads what
G2's notes of 2026-11-11 checked; a G2 miss stops it there.*
*(2026-11-11, later: aligned with gobble-cli's final 0052. P4a no longer reads the kept catalog for consents: a
consent's text and links come from the status row's `consentTerms` (MADR D13, "Read"). It still needs P4's
`acpext/v0.5.0` pin, for `ErrorData.Consent`.)*

1. `internal/provider/auth.go`: `ConsentEntry{Consent authcontract.Consent; Answer string}`; `UpstreamAuth` gains
   `SessionKind string` and `Consents []ConsentEntry`; the optional `ConsentSetter` (`SetConsent(ctx, upstreamID,
   consentID string, allow bool) error`).
2. `internal/provider/gobble/auth.go`: `statusRow` gains `SessionKind string` (`json:"sessionKind"`) and `Consents
   map[string]string` (`json:"consents"`); `status` fills each upstream's `SessionKind` and `Consents` from its row and
   the kept catalog (MADR D13, "Read"); no consent id and no session kind is named in Go.
   *(2026-11-11, later: aligned with gobble-cli's final 0052 D8: `statusRow` also gains `ConsentTerms
   []authcontract.Consent` (`json:"consentTerms"`), and `status` fills `Consents` from `consentTerms` and
   `consents`, not from the kept catalog.)*
   *(2026-11-11, later: the owner's two rulings on MADR D13. On a status error object (0045-MADR D5's, exit 2),
   `status` lists each upstream of `acpext.AuthProviders()` with `ConsentsUnreadable` true and no consents, the whole
   state still `error`, in place of no upstream entries. `statusRow.SessionKind` and `UpstreamAuth.SessionKind` take
   the contract's session-kind type (go-llmprovider-sdk 0073-MADR, amendment of 2026-11-11), the value kept as sent,
   one the type does not know included.)*
3. `internal/provider/gobble/auth.go`: `SetConsent`, as `clearCredential` is built: MADR D13's checks before it runs;
   `gobble auth consent <upstream> <consent id> allow|deny` under `commandTimeout`, no stdin; exit 0 then `drop` and a
   fresh `report`, the answer compared; exit 1, 2, other and the timeout to D13's four codes; stderr's last line to the
   log only.
   *(2026-11-11, later: aligned with gobble-cli's final 0052 D3; its exits are those MADR D13's note of that
   day lists, and the mapping is unchanged.)*
   *(2026-11-11, later: the owner's ruling on an unreadable consent store. `SetConsent`'s first check: an upstream
   in the unreadable state, from a fresh status read or the cached one, is `consent_store_unreadable` and runs no
   command. Exit 1 keeps its mapping, for a store that becomes unreadable between the cached read and the change.)*
4. `internal/protocol`: the auth block's upstream entry gains `session_kind` and `consents`; `AuthConsentPayload`
   `{provider_id, upstream_id, consent_id, allow}`; `errors.go`: D13's four codes, registered. The op-timeout table in
   its four files in one commit, as P2 step 3 did: `op_timeouts.json` gains `provider.auth_consent` 30000;
   `asyncOpTimeout` takes its 30 s default (nothing to add there, and `TestAsyncOpTimeoutMatchesSharedTable` proves
   it); `apps/mobile/lib/data/ws/mcremote_client.dart`'s `opTimeoutFor` gains `provider.auth_consent` beside
   `provider.set_credential`, so that `op_timeout_ladder_test.dart` passes in this commit (it requires every row of the
   table to have its phone margin, and the 30 s default fallback has none).
   *(2026-11-11, later: the upstream entry also gains `consents_unreadable`, `omitempty` (MADR D13, "Read").)*
5. `internal/ws/server.go`: `provider.auth_consent`, async 30 s: the provider's `ConsentSetter`, else
   `no_such_consent`; on success `ok` with the provider's auth block, read again.
6. **The refused turn.** `internal/provider/gobble/errorlimit.go`: the `ErrorData` decode of `:31-45` becomes
   `errorData(raw string) (acpext.ErrorData, bool)`, and `errorLimitAt` calls it, its behaviour unchanged.
   `internal/provider/gobble/errorconsent.go`: `errorConsent(raw string) (upstream, consent string, ok bool)`, true
   only for `Kind == acpext.KindNotPermitted` with a non-empty `Consent`. `acpagent.Spec.ErrorConsent`, bound in
   `host.go` beside `ErrorLimit` (`:178`), applied in `session.go`'s `present` path (`:826-836`) onto the error event;
   `gobble.go` sets it beside `ErrorLimit` (`:85`). `internal/event/event.go`: `ConsentUpstream` (`consent_upstream`)
   and `Consent` (`consent`), `omitempty`, beside the limit fields (`:748-751`); `retention.go:140` counts them.
7. `docs/guides/protocol-v1.md`: the two auth-block members, `provider.auth_consent`, its codes, and the error event's
   two members.
   *(2026-11-11, later: and `consents_unreadable`, and `session_kind` as the contract's session-kind type.)*
8. Tests, each seen failing on a planted break in a scratch clone (C3):

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestGobbleStatusCarriesSessionKindAndConsents` (`gobble`) | a status fixture with `sessionKind: "chatgpt_plan"` and `consents: {"chatgpt_plan.background_use": "declined"}`, and a catalog fake whose `chatgpt_plan` method lists that consent with `text` and a link: the upstream's `SessionKind` is the string as sent, its one `ConsentEntry` carries the catalog's `Consent` and `Answer` `declined`; a consent not answered has no `Answer`; an answered id no method lists has its `id` alone *(2026-11-11, later, aligned with gobble-cli's final 0052 D8: the fixture's row also carries `consentTerms` with that consent's `text` and link, and the catalog fake lists none; the entry's `Consent` is the one from `consentTerms`; a status error object gives no row and no entry)* *(2026-11-11, later, the owner's two rulings on MADR D13: the error object is `TestGobbleConsentStoreUnreadable`'s, the sentence before superseded; `SessionKind` is the contract's session-kind type holding `chatgpt_plan`, and a row whose `sessionKind` the type does not know keeps it as sent)* | (a) `statusRow` without `Consents`, today's state, so the test is red at `14200b6e`'s code; (b) the `Consent` taken from the status, which has no text *(2026-11-11, later: (b) now reads: the `Consent` taken from the kept catalog, which the fake leaves without it; (c) `consentTerms` not decoded, so the entry has its `id` alone)* *(2026-11-11, later)* (d) a `sessionKind` the type does not know dropped or refused |
   | `TestGobbleConsentRunsTheCLI` (`gobble`) | against a stub `gobble` that records its argv and stdin: `allow` runs `auth consent openai chatgpt_plan.background_use allow` and `deny` the same with `deny`, with empty stdin | (a) `background` passed in place of the id; (b) `allow` and `deny` swapped |
   | `TestGobbleConsentExitStatus` (`gobble`) | stub exit 0 whose next status shows the new answer: `nil`; exit 0 whose status still shows the old one: `consent_failed`; exit 1 with a planted path sentinel on stderr: `consent_store_unreadable`, whose message holds no sentinel while the captured log line does; exit 2: `consent_refused`; exit 3, and a stub that sleeps past a shortened `commandTimeout`: `consent_failed` | (a) exit 1 mapped to `consent_failed`; (b) stderr copied into the returned error's message; (c) the status not read again after exit 0 |
   | `TestGobbleConsentStoreUnreadable` (`gobble`) *(2026-11-11, later, the owner's ruling on an unreadable consent store)* | a stub whose `auth status --json` prints 0045-MADR D5's error object and exits 2: every upstream carries `ConsentsUnreadable` and no consent; `SetConsent` for `chatgpt_plan.background_use` is `consent_store_unreadable` and the stub records no `auth consent` run, on the first read and again on a second call inside `statusTTL`, served from the cache; a stub whose status is readable and whose `auth consent` exits 1 still gives `consent_store_unreadable` | (a) the error object mapped as at `14200b6e`, no upstream entries, so the change is `no_such_consent`; (b) the unreadable check placed after the `no_such_consent` checks; (c) the state set but the command run anyway |
   | `TestConsentAllowNeedsText` (`internal/ws`) | `provider.auth_consent` with `allow: true` for a consent the auth block lists with no `text` is `no_such_consent` and the stub records no run; `allow: false` for it runs | the text check removed |
   | `TestAuthConsentOverWire` (`internal/ws`) | `provider.auth_consent` answers `ok` with the auth block read again; a provider without `ConsentSetter` is `no_such_consent`; an unknown upstream is `no_such_consent` | the `ok` carrying the cached block |
   | `TestErrorConsentReadsNotPermitted` (`gobble`) | an `ErrorData` of kind `not_permitted` with `consent` gives the upstream and the id; `not_permitted` with no `consent`, and a `consent` with kind `quota`, give `ok` false; `errorLimitAt` returns `ok` false for the first and unchanged results for every existing case *(2026-11-11, later, aligned with gobble-cli's final 0052 D2: the no-`consent` case is that D2's second row, `provider` `openai`, `kind` `not_permitted`, no `consent`)* | (a) the kind check removed; (b) `errorLimit` changed to read `not_permitted` as a limit; *(2026-11-11, later)* (c) the non-empty `consent` check removed, so the no-`consent` case gives `ok` true |
   | `TestPromptErrorCarriesConsent` (`acpagent`, on `acptest`) | a prompt that fails with such `ErrorData` emits an error event whose `consent_upstream` and `consent` are set; with `Spec.ErrorConsent` nil, neither | `bindSpec` not copying `ErrorConsent` |
   | `TestEventSizeCountsConsent` (`internal/event`) | an event's retained size grows by the two members' lengths | `retention.go` not counting them |
   | `TestAsyncOpTimeoutMatchesSharedTable` (existing) | `provider.auth_consent`'s row is the 30 s default | the row written 15000 |
   | `op_timeout_ladder_test.dart` (existing) | `provider.auth_consent`'s row has its phone margin | the row left out of `opTimeoutFor` |

9. Gates (C4), the Dart trio over `apps/mobile` for step 4's one Dart line, and the Linux and Windows test hosts, since
   `TestGobbleConsentRunsTheCLI` and `TestGobbleConsentExitStatus` launch a stub process. **Stage.**

Acceptance: the tests pass and each plant failed; no Go file names `chatgpt_plan.background_use` or `chatgpt_plan`
outside tests (`git grep`); the old provider-auth tests pass unchanged.

### P5 — the phone (MADR D6, D9)

1. `mcremote_client.dart`: `startSignIn`, `answerSignIn`, `cancelSignIn`, `signInSessions`; the `provider.auth_event`
   stream; `app_contract` sent in `auth` and in `pair.claim`, and checked in `auth_ok` and in `pair_ok` (MADR D6).
2. `sign_in_sheet.dart`: every step kind of MADR D9, keyed by `session_id`; resumes from `provider.auth_sessions`
   after a reconnect and on open; the paste field cleared on send and never stored; an `open_url` with `accepts` and
   the `paste` step after it on one sheet, the paste sent for the `paste` step's `seq`; no answer sent for a shown
   step; a consent `confirm` with three outcomes: allow, decline background use (`confirm: false`, the sign-in goes
   on), and Cancel (`provider.auth_cancel`); the method's `client_identity` named (MADR D9, the owner's Q10).
3. `provider_detail_screen.dart`: methods from the contract `Method`; `available` and `reason` from the daemon only;
   "a sign-in whose type is learned on start" for an `at_start` method (O1); "Finish on the host" for `host_only`, with
   `Method.command` and a copy button when it is set (O3); removal copy from `Method.slot` (two configured methods
   sharing a slot are one credential), not the provider id; `backup_state` and `recovery_available` as `0074-PLAN-remote-provider-auth-from-phone.md` P23
   left them on the auth block.
4. `models.dart`: the auth block reads the method entries of MADR D4; `AuthMethodType` and `isBrowserOAuth` go.
5. `mc_exception.dart`: copy for D3's codes and for every `ErrorKind`.
5a. *(2026-11-11, R1; MADR D13.)* `mcremote_client.dart`: `setConsent(providerId, upstreamId, consentId, allow)` for
    `provider.auth_consent` (`opTimeoutFor`'s row landed in P4a step 4); `models.dart`: the upstream
    entry's `sessionKind` and `consents`, each consent the generated `Consent` with an optional `answer`;
    `provider_detail_screen.dart`: the "Stored sign-in" line with `session_kind` verbatim, and one row per consent
    with its text, links and answer ("Allowed" for `granted`, "Declined", "Not asked"); `consent_sheet.dart`: the whole
    text and every link, then allow, default no; a consent with no text offers decline only; `chat_models.dart` and
    `transcript_reducer.dart`: the error event's `consent_upstream` and `consent`; `chat_bubble.dart`: "Allow…" on an
    error whose consent the upstream's auth block holds with text, opening the consent sheet, sending nothing for the
    turn; `mc_exception.dart`: copy for D13's four codes.
    *(2026-11-11, later: aligned with gobble-cli's final 0052: each consent's text and links are the auth
    block's, which P4a fills from `consentTerms`; `session_kind` stays verbatim, MADR D13's recommendation, until
    the owner settles it.)*
    *(2026-11-11, later: the owner's two rulings on MADR D13. "`session_kind` stays verbatim … until the owner
    settles it" is settled: `models.dart` reads `session_kind` as the generated session-kind type, which P1's
    generator makes from the tagged schema with its labels (go-llmprovider-sdk 0073-MADR, amendment of 2026-11-11),
    and the "Stored sign-in" line shows its label, or the value verbatim when the generated type does not know it;
    no label is written by hand. An upstream entry with `consents_unreadable` puts the upstream in the unreadable
    state: the line "gobble could not read its credential store on the host, so its consents cannot be shown or
    changed." in place of its consent rows, with no answer and no control; a change answered
    `consent_store_unreadable` leaves the upstream in the same state.)*
6. Tests, each seen failing:

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `sign_in_sheet_test.dart` | each step kind renders; `paste` sends an `Answer` with the paste step's `seq` and the step's `secret`, obscures a secret field and clears it; an `open_url` with `accepts` shows the paste box with the following `paste` step and sends nothing for the `open_url`; `confirm` defaults to no and offers three outcomes, "allow" sending `confirm: true`, "decline background use" sending `confirm: false` and leaving the sheet open, "Cancel" sending `provider.auth_cancel`; an unknown kind shows its `text` and Cancel | the `seq` taken from the session; a secret paste shown in clear; the paste sent for the `open_url`'s `seq`; "decline background use" sending `provider.auth_cancel` |
   | `sign_in_resume_test.dart` | after a reconnect, a `finished` step closes the sheet with its outcome | `auth_sessions` not sent |
   | `app_contract_test.dart` | an `auth_ok`, and a `pair_ok`, with another `app_contract` shows "Update mcremote on the host…" and sends nothing more | the check removed; the check on `auth_ok` only |
   | `provider_detail_screen_test.dart` (`apps/mobile/test/`) | no branch on a provider id: the shared-slot copy follows `slot` (codex's four methods are one credential; grok's key and OAuth are two); a browser method marked available is offered; an `at_start` method shows the learned-on-start label; a `host_only` method's `command` is shown and copied by its button; each `client_identity` value shows its line (`vendor_borrowed`, `app_registered`, `unknown`; none for `none`) | `providerId == 'codex'` restored; the copy button copying `detail`; `client_identity` not read |

   *(2026-11-11, R1; each seen failing, C3.)*

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `provider_consent_test.dart` | an upstream with `session_kind` `chatgpt_plan` shows it verbatim; each answer word shows its copy, an unknown word as "Not asked"; turning a consent on opens the sheet and sends nothing until allow; turning it off sends `allow: false` with no sheet; a consent with no `text` offers decline only *(2026-11-11, later, the owner's two rulings on MADR D13: `chatgpt_plan` shows the generated label, not the value, the sentence's "verbatim" superseded; a kind the generated type does not know shows verbatim; an upstream with `consents_unreadable` shows the unreadable line and no consent row and no control)* | (a) allow sent without the sheet; (b) every answer but `declined` read as allowed, so the unknown word shows "Allowed"; (c) the allow control shown for a consent with no text; *(2026-11-11, later)* (d) the raw value shown for a known kind; (e) an unknown kind shown empty or refused; (f) consent rows and controls shown for an unreadable upstream |
   | `consent_sheet_test.dart` | the text and every link render before the allow button, which is not the default; allow sends `provider.auth_consent` with `allow: true`; each of D13's four codes shows its copy, `consent_store_unreadable` leaving no answer shown as current *(2026-11-11, later: and leaving the upstream in the unreadable state)* | (a) the sheet defaulting to allow; (b) the links not rendered |
   | `chat_consent_prompt_test.dart` | an error event with `consent` that the upstream's auth block holds with text shows "Allow…", which opens the sheet; with an unknown consent, or one with no text, no button; allowing sends no prompt *(2026-11-11, later, aligned with gobble-cli's final 0052 D2: an error event with no `consent`, gobble's unreadable-answer error, shows its text and no button, though the upstream's auth block holds the consent with text)* | (a) the button shown for an unknown consent; (b) a prompt re-sent after allow; *(2026-11-11, later)* (c) the button keyed on the upstream's auth block alone, `consent` not read |

7. `rg -n "providerId == '" apps/mobile/lib` finds nothing. Dart gates (C4). **Stage.**
   *(2026-11-11: and `rg -n "background_use|chatgpt_plan" apps/mobile/lib` finds nothing outside the generated file.)*

### P6 — the cut-over (MADR D11)

1. Removed, daemon and app in one commit: MADR D11's list; `device_flow_sheet.dart` and its tests; the `oauth.*`
   streams; `provider.start_auth`'s table row.
2. `docs/guides/protocol-v1.md` and `protocol-v2.md`: the old messages gone; `docs/architecture.md`: the auth path.
3. Assertions by search: `git grep -n "oauth\.device_flow\|provider\.start_auth\|oauth_browser\|browser_only\|host_oauth\|keyring_managed\|AuthQuota"`
   finds nothing outside `docs/decisions/` and `docs/reports/`.
4. Gates (C4), the Linux and Windows test hosts included. **Stage.**

### P7 — acceptance, and the folded live runs (MADR D12, Confirmation)

1. Confirmation 1 to 9, each with its evidence *(2026-11-11: and 11's tests, its L7 half under step 2)*.
2. **Owner-run runs, each asked first, each in a scratch home, nothing of the owner's real stores read or written
   except where a step says so:**
   * **L1, codex (`0074-PLAN-remote-provider-auth-from-phone.md` P22 steps 5 and 6, codex's half).** In a scratch
     `CODEX_HOME` with a scratch coordinator `DataDir`: LIVE hashed before the code is shown, during polling, and after
     a cancel, a timeout, a socket loss and a forced daemon death, each byte-identical to the hash before (P22 step 5);
     a phone `device_code` sign-in to completion, validated; a second rotation; CURRENT and PREVIOUS retained,
     owner-only modes, nothing printed; one busy activation (a session open, the result `waiting` with
     `ready_to_activate`, then `done` without a second exchange); one logout from the phone, which revokes the grant,
     cannot be undone, and removes LIVE and the generation payloads. **Then the revocation is checked
     (`0074-MADR-remote-provider-auth-from-phone.md` §16 D38, 16.5 item 9):** a refresh with the old refresh token,
     kept by the test harness in memory only before the logout, is refused by OpenAI; a refresh that succeeds is a
     stop, since the phone's logout would then leave a live grant. *Owner-run; asks first; scratch home; quota: no
     model call is made; one refresh request; the logout revokes the test grant at OpenAI.*
   * **L2, grok (the same steps, grok's half).** In a scratch `GROK_HOME`: LIVE hashed at the same five points, each
     byte-identical; a phone `device_code` sign-in to completion, validated; rotation; busy activation; the API key's
     removal keeps the OAuth session, and the OAuth logout keeps the key. *Owner-run; asks first; scratch home; quota:
     none; `grok logout` revokes nothing (`0074-PLAN-remote-provider-auth-from-phone.md` §17.6, its grok finding).*
   * **L3, gobble sign-in by elicitation.** From the phone, a gobble `grok` `device_code` sign-in, then a `kilo` one,
     through D8's connection, in a scratch `GOBBLE_HOME`; then, if the owner has a ChatGPT plan to test, the plan's
     `confirm`, `open_url` and `paste`. *Owner-run; asks first; scratch home; quota: none for the sign-ins; each plan
     sign-in registers an OpenAI client under the account (go-llmprovider-sdk
     `docs/reports/0017-REPORT-openai-sign-in-for-agents.md`, the 0245 report's F23).*
   * **L4, `make live-gobble` (`0179-PLAN-gobble-native-acp-provider.md` P6 step 1),** against the G2 release, with
     the fixture recapture of P4 step 6, which runs before P4's commit (P4 step 1). *Owner-run; asks first; scratch
     home; quota: model turns on the model the owner names, as that PLAN's P3 spent on
     `opencode-zen/deepseek-v4.1-flash`.*
   * **L5, the phone key write (`0179-PLAN-gobble-native-acp-provider.md` P6 step 2):** a dummy key through a scratch
     daemon makes `gobble auth check` report the provider ready; no copy of the key in mcremote's log. *Owner-run; asks
     first; scratch home; quota: none.*
   * **L6, probe P-B:** a `show_code` session started from a scratch daemon (its home, `XDG_*` and the agent's home in
     a temporary directory), the app sent to the background and the browser opened for 3 minutes, on Android and on
     iOS; the record notes whether the socket dropped and whether the session survived. *Owner-run; asks first;
     scratch home; no sign-in completed; quota: none.*
   * **L7, a consent read and changed from the phone (MADR D13; added 2026-11-11, the owner's ruling R1).** In the
     scratch `GOBBLE_HOME` that holds L3's ChatGPT plan session: the phone's provider screen shows `session_kind`
     `chatgpt_plan` and the background-use consent's answer as `gobble auth status --json` in that home reports it;
     declining from the phone makes that status read `declined`; allowing from the phone shows the consent's text and
     link in the sheet before the button, then makes the status read `granted`; with the scratch `auth.json`'s mode
     set to 000 for the run and restored after, a change from the phone shows `consent_store_unreadable` and changes
     nothing. The record notes which wording the sheet showed (MADR, "Dependencies D13 found", item 1). A refused turn
     is not forced live: ending one takes a context overflow, which spends model quota; P4a's and P5's tests cover the
     prompt, and the owner may add a run. *Owner-run; asks first; scratch home; quota: none, no model call; it needs
     L3's plan session, and if the owner has no ChatGPT plan to test, L7 is recorded not run, with that reason.*
     *(2026-11-11, later: aligned with gobble-cli's final 0052. The sheet's text and link are `consentTerms`', which
     0052 D2's note makes the same as the `confirm` step's; L7 records whether they equal what L3's `confirm` step
     showed, in place of "which wording". **Open, for the owner:** with `auth.json` at mode 000, `auth status --json`
     also fails (0052 D8's note: 0045-MADR D5's error object, exit 2), so once the daemon's 15 s status cache
     (`statusTTL`, `internal/provider/gobble/auth.go:31`) expires the upstream lists no consent and a change is
     refused `no_such_consent` before anything runs; `consent_store_unreadable` is reached only inside that window.
     MADR D13 does not say which the run must show, and this note does not decide it.)*
     *(2026-11-11, later: **resolved** by the owner's ruling on an unreadable consent store, MADR D13. With
     `auth.json` at mode 000 the run expects, both inside the 15 s `statusTTL` window and after it, the phone showing
     the upstream in the unreadable state and a change from the phone answered `consent_store_unreadable`, nothing
     changed. Inside the window, while the cached status is still the readable one, the change runs `gobble auth
     consent`, whose exit 1 gives the code and puts the phone in that state; after it, the status read gives the
     error object, the phone shows the state from the auth block, and the daemon refuses the change before running
     anything (that it runs nothing is P4a's `TestGobbleConsentStoreUnreadable`'s to show). `no_such_consent` on
     either is a failure of the run. The record notes, for each, the time since the last status read. The "Stored
     sign-in" line shows the contract's label for `chatgpt_plan` (the owner's ruling on session-kind labels).)*
3. **The rest of `0179-PLAN-gobble-native-acp-provider.md` P6,** carried whole: step 3, `mcremote doctor` lists gobble,
   runs no version check for it, and reports the installed binary; step 4, the other providers' suites pass
   unchanged; step 5, the wider gates (C4); step 6, that record's Confirmation 1 to 8 with their evidence; step 7, a
   comment for issue #33, written into this record for the owner to post. `0179-PLAN-gobble-native-acp-provider.md` P6's heading is then annotated
   done here.
   *(2026-11-11: with that record's Confirmation, its D14 gate 3 is verified again here, by reading both `go.mod`
   files: the G2 release's and this repository's must name one acp-go-sdk2 tag, `v1.36.0`
   (`0179-MADR-gobble-native-acp-provider.md` A10). The gate is marked met in that MADR then, or named unmet here
   with its reason, as the acceptance line below requires.)*
4. The MADR `accepted` if not already; this plan `complete`; `check_records.py --write-index`.

Acceptance: Confirmation 1 to 10 met, or each unmet item named with its reason. *(2026-11-11, R1: 1 to 11.)*

## Verification

Each command's exit status is captured before any filter.

```bash
make pre-add-check FILES="<the phase's Go files>"
make check-authcontract
make test
go test -race -count=1 ./...
make lint staticcheck vet-tags
make preflight
python3 scripts/check_records.py --check-all
cd apps/mobile && dart format --output=none --set-exit-if-changed . && flutter analyze && flutter test
make ci-windows                     # on the Windows test host, from Git Bash, on a scratch copy
go test -race -count=1 ./...        # on the Linux test host, on a scratch copy
python3 ~/.global-git-hooks/github-disclosure.py redact
```

Before any push the owner asks for, the guard over the outgoing commits:

```bash
echo "refs/heads/master $(git rev-parse HEAD) refs/heads/master $(git rev-parse origin/master)" | python3 ~/.global-git-hooks/github-disclosure.py pre-push origin "$(git remote get-url origin)"
```

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 the pin and the boundary | P1 | step 6 |
| 2 vectors in Go and Dart | P1 | step 6 |
| 3 the drift gate | P1 | step 6 |
| 4 each agent's steps | P2, P3 | P2 step 6; P3 step 7 |
| 5 gobble's elicitation sign-in | P4 | step 7 |
| 6 deadline, detach, drain | P2 | step 6 |
| 7 `app_contract` | P2, P5 | P2 step 6; P5 step 6 |
| 8 no paste in logs | P2, P4 | P2 step 6; P4 step 7 |
| 9 the gates | every phase | C4 |
| 10 the owner-run runs | P7 | step 2 |
| 11 the consent on the phone *(2026-11-11, R1)* | P4a, P5, P7 | P4a step 8; P5 step 6's table of 2026-11-11; P7's L7 |

## Rollout and Rollback

* **Order across repositories:**
  0. *(Added 2026-11-11, on the owner's rulings of that day.)* Ahead of everything below, three moves this
     cut-over rests on and does not contain: go-llmprovider-sdk's Go-floor record, its 0077; gobble-cli's record
     0054, which moves gobble's `main` to acp-go-sdk2 `v1.36.0`; and this repository's own move to `v1.36.0`,
     `0217-PLAN-update-every-pin-to-its-current-release.md` P3j with that MADR's F3k. The last two land together
     (`0179-MADR-gobble-native-acp-provider.md` A10). The two records of the other repositories were being drafted
     that day and were in neither tree when this was written, so they are named by number, as relayed.
  1. go-llmprovider-sdk tags `authcontract/v0.1.0` (go-llmprovider-sdk `0073-PLAN-publish-an-auth-contract-module.md` Phase 4). G1 opens.
  2. go-llmprovider-sdk tags the minor carrying its `0074-MADR-one-sign-in-handle-and-method-facts.md` and
     `0075-MADR-chatgpt-plan-consent-host-id-and-client-reuse.md` (`v1.31.0` if its 0076 ships as `v1.30.0`). Magic
     does not require it; gobble does.
  3. gobble-cli tags `acpext/v0.5.0` and releases `v0.6.0` (gobble-cli `0051-PLAN-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` rollout). G2 opens.
  4. This repository: P1 pins the contract tag; P4 pins `acpext/v0.5.0` and moves `KnownGoodVersion`; P6 completes
     the cut-over. The daemon and the app are released together, from one commit, with one `app_contract`.
     *(2026-11-11: what lands in this repository before P1, a release included, is in Scope's note of that day.
     At step 3, `v0.6.0` must pin acp-go-sdk2 `v1.36.0`, G2's note of that day.)*
     *(2026-11-11, later: P4a lands between P4 and P5 in the same release. At step 3, `v0.6.0` must also hold G2's
     R1 and R2 checks of that day.)*
     *(2026-11-12: step 3's release is now gobble `v0.7.0`: gobble-cli tags `acpext/v0.5.0` and releases `v0.7.0`, and
     G2 opens then. By gobble-cli `0055-MADR-release-v0-6-0-now-and-the-sign-in-redesign-as-v0-7-0.md`
     (accepted that day), gobble `v0.6.0` is released earlier, from gobble-cli's `main` and without the redesign; it
     comes after step 0, is not one of these steps, meets 0179's gate 3 when published, and opens nothing here. "At
     step 3, `v0.6.0` must pin acp-go-sdk2 `v1.36.0`" and "`v0.6.0` must also hold G2's R1 and R2 checks" now read
     `v0.7.0`. Whether this repository adopts `v0.6.0` before P4 (`KnownGoodVersion` and the fixture) is the owner's,
     not decided (`0179-MADR-gobble-native-acp-provider.md` A10's note of this date).)*
* **Rollout:** after the release, a phone of the old build is refused at connect with `app_outdated`, and an old
  daemon is refused by the new app. The owner updates both. Stored credentials are untouched: the agents own them
  (`0074-MADR-remote-provider-auth-from-phone.md` D2), and the coordinator's manifests do not change.
* **Rollback before the release:** revert the phase commits newest first; each phase's tests return with it.
* **Rollback after the release:** reinstall the previous daemon and app together. An older gobble is refused by
  nothing, but its `--json` and kind words no longer match; the previous Magic release pins `acpext` before `v0.5.0`
  and drives gobble's CLI, so a rollback pairs it with the gobble release that release was built against.
  *(2026-11-11, R1: a consent answer changed from the phone is gobble's stored state in its own `auth.json`; a
  rollback of Magic leaves it as the phone last set it, and `gobble auth consent` on the host changes it.)*

## Execution record

Not started. The MADR is `proposed`.
