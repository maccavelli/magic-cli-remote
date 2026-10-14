---
status: proposed
date: 2026-10-14
associated-madr: "0191-MADR-answer-agent-elicitation-forms-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement answering agent elicitation forms on the phone

Associated MADR: [0191-MADR-answer-agent-elicitation-forms-on-the-phone.md](0191-MADR-answer-agent-elicitation-forms-on-the-phone.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"Map form elicitation onto the existing question flow, extended additively with typed, optional,
defaulted fields and a decline" (D1–D7).

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
