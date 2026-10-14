---
status: in-progress
date: 2026-10-14
associated-madr: "0191-MADR-answer-agent-elicitation-forms-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement answering agent elicitation forms on the phone

Associated MADR: [0191-MADR-answer-agent-elicitation-forms-on-the-phone.md](0191-MADR-answer-agent-elicitation-forms-on-the-phone.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-14 the owner accepted the
MADR's recommended option and approved the PLAN ("accepted, approved to execute. proceed"). This
PLAN implements that option, "Map form elicitation onto the existing question flow, extended
additively with typed, optional, defaulted fields and a decline" (D1–D7). The phase gates still
hold: L4 is taken after L1–L3 are committed, and L5's second step waits for the owner's quota.

* A phase starts only after the owner approves it by name.
* Each Go phase ends with `make pre-add-check` on its staged Go files, `go test -race ./...` and the
  0145 gates on a Windows host.
* The Flutter phase ends with `make preflight` (`flutter analyze`, `flutter test`, `dart format`).
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

An ACP agent's session-scoped form reaches the user as a question form with typed, optional and
defaulted fields. The answer is checked and returned as a typed `accept`, a `decline` or a
`cancel`. Magic advertises `elicitation.form` only once that is true.

## Scope

* `internal/event/event.go`: the new `QuestionItem` fields and `AllowDecline` (D4).
* `internal/provider/provider.go`: the decline outcome on `RespondQuestion`, and the four question
  providers' implementations, which treat a decline as cancel unless they support one (D4).
* `internal/session` and `internal/ws`: `question.respond`'s `declined`, and a validation refusal
  returned as an error (D3, D4).
* `internal/provider/acpagent`: a new `elicitation.go` holding the schema mapping, validation and
  coercion, and `CreateElicitation` (D1–D3).
* `apps/mobile`: the question model, the sheet, and the respond call (D5).
* `docs/guides/protocol-v1.md`: the new fields, `allow_decline`, `declined`, and the refusal.
* **Out of scope:**
  * URL mode and `elicitation/complete`;
  * moving codex's MCP elicitation onto the form;
  * restoring the agent's field order, which is an SDK change;
  * gobble's capability set.

## Implementation Steps

### L1. The contract (D4)

1. Add the `QuestionItem` fields, `AllowDecline`, and `question.respond`'s `declined`, all
   `omitempty`. A protocol test round-trips them, and pins an existing provider's form as encoding
   exactly as before. Document them in `protocol-v1.md`.
2. Carry the decline through `RespondQuestion`: the manager, `internal/ws`, and the four providers,
   where a decline means cancel.
3. A refusal path: `RespondQuestion` may return a validation error before it claims the answer. The
   manager and `question.respond` return it to the phone, and the form stays pending. A test shows
   the waiter still pending after a refusal.

### L2. The acpagent mapping (D1–D3)

1. Write the mapping tests and the answer tests (MADR Confirmation, first two items), with frames
   built from the SDK types. They are the schema-built fixture. Show them failing against the
   method-not-found stub.
2. Implement `CreateElicitation` in `elicitation.go`: scope and mode handling, the schema-to-items
   mapping, then `awaitAnswers`, validation, and coercion to `ElicitationContentValue`.
3. Keep answers out of logs and events, as `QuestionAnswers` already is.

### L3. The phone (D5)

1. Write the widget tests (MADR Confirmation, fourth item). Show them failing.
2. Parse the new fields. In the sheet: optional fields, numeric keyboards, prefilled defaults,
   constraint text and checks, a Decline button when `allow_decline` is set, and a refused answer
   reopening the form with the daemon's message.
3. `dart format` on the changed files, and `make preflight`.

### L4. Advertise (D6)

Taken only after L1–L3 are committed.

1. Change the honesty test's dishonest example to `elicitation.url`, and add `elicitation.form` to
   `implementedClientCapabilities`. Show, on a scratch copy, the test failing with form advertised
   but not implemented.
2. Add `Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}}` to
   `defaultClientCapabilities`. Update the pinned `initialize` frame. Add a wire test in which a form
   passes the SDK's gate and a URL request is still refused.

### L5. Probe (D7, owner gate)

1. Zero-token: `initialize` and `session/new` against grok, with `elicitation.form` advertised and
   `MCREMOTE_WIRE_CAPTURE_DIR` set. Record acceptance, and anything grok sends at session start.
2. With the owner's quota: a turn that leads grok to ask the user something. Record whether it uses
   `elicitation/create` or `_x.ai/ask_user_question`, and commit any elicitation frames, redacted,
   as the fixture.

## Verification

* Every new test is seen failing first, and planted breaks are shown failing on a scratch copy.
* `go test -race ./...`, `make pre-add-check`, `make lint`, `make preflight`.
* `make ci-windows` on the Windows test host.
* `python3 scripts/check_records.py --check-all` and `make markdownlint-docs`.

## Rollout and Rollback

* L1–L3 change nothing an ACP agent can reach, because elicitation is not advertised until L4. The
  other providers' forms gain only what they choose to send, which today is nothing new.
* L4 is the switch. Reverting it withdraws the advertisement, and the SDK refuses forms again.
* A phone without L3 still answers forms, with every field required and no decline (D5). L3 should
  ship with or before L4.

## Execution record

### L1–L3, 2026-10-14: complete (staged, not committed)

**Approval:** the owner on 2026-10-14, "accepted, approved to execute. proceed". L4 waits, by its
gate, for L1–L3 to be committed. L5's second step waits for the owner's quota.

**Deviation, 2026-10-14: the decline uses its own method, not `RespondQuestion`'s signature.**

* Step L1.2 said to carry the decline "through `RespondQuestion`" in the four providers. It is
  carried instead by an optional `provider.QuestionDeclineSession.DeclineQuestion` and a new
  `Manager.DeclineQuestion`.
* The manager sends a decline to a session without that interface as `RespondQuestion(…,
  cancelled=true)`.
* OpenCode, kilo and codex are therefore unchanged. The behavior D4 states holds: a provider without
  a decline treats it as cancel.
* Chosen because it is additive and touches no provider that cannot decline. The MADR has a dated
  note on D4.

**L1, the contract.**

* Tests written first:
  * `TestQuestionFormFieldsAreOmittedWhenUnused` pins an existing item's JSON exactly.
  * `TestQuestionFormFieldsRoundTrip` and `TestBytesCountsQuestionFormFields` cover the new fields.
  * `TestQuestionRespondPayloadDeclined` covers the respond payload.
  * In `internal/session/questiondecline_test.go`: `TestDeclineQuestionReachesADecliningSession`,
    `…IsACancelWithoutDeclineSupport`, `…IsOwnerAuthorized`, and
    `TestRefusedAnswerComesBackAsInvalid`.
  * In `internal/ws/question_form_test.go`: `TestWSQuestionDeclineReachesTheSession` and
    `TestWSQuestionRefusalIsQuestionInvalid`.
* The structure came first: the 11 `QuestionItem` fields, `AllowDecline`, `Declined`,
  `ErrQuestionInvalid` with the `question_invalid` code, and a no-op `Manager.DeclineQuestion`.
  Against it the behavioral tests **failed**, for example:
  * `form fields add 0 bytes, want 21`;
  * `declined [], responded []; want q1 declined and nothing else`;
  * `err = <nil>, want ErrForbidden`;
  * `code = "question_failed" (…), want "question_invalid"`.
* Implemented:
  * `Bytes` counts the new strings.
  * `Manager.DeclineQuestion` is owner-authorized, and falls back to cancel.
  * `question.respond` routes `declined: true`; a cancel wins if both are set.
  * `writeSessionErr` maps `provider.ErrQuestionInvalid` to `question_invalid`.
* `protocol-v1.md` documents the new fields table, `allow_decline`, `declined` and
  `question_invalid`. The code is registered in `protocol.ErrorCodes()`.

**L2, the acpagent mapping.** `elicitation.go` replaces 0184 D9's method-not-found stub.

* Tests in `elicitation_test.go`, with requests built from the SDK types (the schema-built fixture):
  * `TestElicitationFormBecomesAQuestion` and `TestElicitationAnswerIsTypedContent`;
  * `TestElicitationOptionalFieldsMayBeEmpty` and `TestElicitationRefusesABadAnswerAndKeepsTheForm`
    (12 broken rules);
  * `TestElicitationDeclineCancelAndTimeout`, including grok's question, which gets a cancel;
  * `TestElicitationFormsMagicCannotShow` (request scope, an `Other` mode, URL mode, and a required
    unknown type) and `TestElicitationDropsWhatItCannotDraw`.
* Against the stub, every case **failed**: forms got `Method not found`, and no `question_request`
  reached the phone.
* One test hung on the first run, because a stub decline left grok's question unanswered and that
  subtest waited without a timeout. It now fails within 3 seconds instead.
* The implementation:
  * The waiter gains an optional `form` check and `allowDecline`.
  * `RespondQuestion` runs the check **before** claiming the question, so a refusal leaves it
    pending, and it builds the typed content there.
  * `DeclineQuestion` sends a decline, or a cancel for a form without one.
  * The mapping, order, bounds, formats and RE2 patterns follow D1–D3.
* **Why the content is built inside `RespondQuestion`.** `question.respond` clears the answer
  strings in place once the provider call returns (`defer p.ClearAnswers()`), and
  `OrderedQuestionAnswers` shares the inner slices. Content built later, on the waiting goroutine,
  could read blanks.

**L1 and L2, planted breaks, each on a scratch copy.** All 21 failed, and none was a build failure:

* L1 (7):
  * `Bytes` ignores the form strings;
  * `kind` always sent;
  * `declined` not parsed;
  * the manager never uses `DeclineQuestion`;
  * the manager's decline skips the owner check;
  * `ws` ignores `declined`;
  * `ws` reports a refusal as `question_failed`.
* L2 (14):
  * answers not checked;
  * a decline sent as a cancel;
  * the form offers no decline;
  * optional fields in map order;
  * maximum, maximum length, format, pattern and maximum items not enforced;
  * every field required;
  * an optional unknown type cancels the form;
  * unknown formats forwarded;
  * integers sent as numbers;
  * the request scope not refused.

**L3, the phone.**

* `test/question_form_test.dart` has 9 tests:
  * parsing, an optional field, a number field's keyboard and bounds, and defaults;
  * length, format and selection checks;
  * Decline shown only when allowed;
  * a refusal shown with the answers kept;
  * in `ChatScreen`, a refused answer reopening the form with the message, and Decline sending
    `declined`.
* Against the structure (the model fields, `allowDecline`, the sheet's new parameters unused, and
  `respondQuestion`'s `declined`), all 9 **failed**.
* The sheet checks each rule the daemon checks, never more strictly:
  * email needs an `@`;
  * a URI needs a scheme;
  * a pattern Dart cannot read is left to the daemon.
* `_showQuestionSheet` loops: a `question_invalid` refusal reopens the form with the daemon's
  message and the non-secret answers kept.
* The existing `question_sheet_test.dart` passes unchanged.
* **Planted breaks, each on a scratch copy of `apps/mobile`.** All 13 failed:
  * `allow_decline` not parsed; `max` not parsed;
  * optional fields required; no number keyboard;
  * maximum not checked; an integer accepting a decimal;
  * defaults ignored; format not checked; maximum items not checked;
  * Decline never shown; the refusal not shown;
  * Decline sent as a cancel;
  * a refusal not reopening the form.

**Verification.**

* `go test -race ./...`: passed.
* `make pre-add-check` on the 16 changed Go files: clean. `make lint`: clean.
* `make ci-windows` on the Windows test host: `ci-windows-exit=0`, with `internal/event`,
  `internal/protocol`, `internal/provider/acpagent`, `internal/session` and `internal/ws` all `ok`.
* `make preflight` passed:
  * markdownlint: 0 issues;
  * `staticcheck`, the release build, and the rest of the Go set;
  * `dart format` (`Formatted 217 files (0 changed)`);
  * `flutter analyze` (`No issues found!`);
  * `flutter test` (`+1460 ~3: All tests passed!`).

**Found, out of scope, for the owner.** grok's existing `_x.ai/ask_user_question` path appears to
have a data race:

* `handleAskUserQuestion` reads `res.answers` on the agent's goroutine, after `RespondQuestion` has
  returned.
* Those slices are the ones `question.respond` blanks with `ClearAnswers()` once the call returns.
* Depending on timing, grok could receive empty answers.

It is not observed and has no test, and it predates this PLAN. The likely fix is one line: copy the
answers in `RespondQuestion`. It waits for the owner's word.

**Not done.**

* **L4** waits until L1–L3 are committed.
* **L5** follows L4. Its second step waits for the quota.
