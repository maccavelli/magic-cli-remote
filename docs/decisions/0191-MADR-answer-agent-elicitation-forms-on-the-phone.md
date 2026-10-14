---
status: accepted
date: 2026-10-14
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Answer an ACP agent's elicitation forms on the phone, and advertise `elicitation.form`

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 takes up ACP capabilities one record at a time. Item 5 is "elicitation, form mode first, then
URL mode with a trust policy". The four before it are
[0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md),
[0188-MADR-show-agent-notices-on-the-phone.md](0188-MADR-show-agent-notices-on-the-phone.md),
[0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)
and
[0190-MADR-show-agent-context-compaction-on-the-phone.md](0190-MADR-show-agent-context-compaction-on-the-phone.md).
This record decides form mode. URL mode is left for a record of its own.

**What ACP defines** (`acp-go-sdk2@v0.17.1`):

* `elicitation/create` is an agent-to-client **request**. Its answer is `accept` with `content`,
  `decline`, or `cancel` (`types_gen.go:3266-3474`). A mode or action beginning with `_`, or one not
  yet defined, arrives as `Other`, which a client "MUST NOT render … as a known elicitation mode"
  (`:3080-3095`).
* **Form mode** (`CreateElicitationForm`, `:2777-2884`) carries a `message` and a `requestedSchema`.
  * The schema is an object of primitive properties (`ElicitationSchema`, `:4951-5046`), with an
    optional `title`, `description` and `required` list.
  * Each property is a string, number, integer, boolean or multi-select array (`:4648-4665`):
    * a string may carry `enum`, titled `oneOf` options, a `default`, `minLength`, `maxLength`, a
      `pattern`, and a `format` of `email`, `uri`, `date` or `date-time`;
    * a number or an integer carries `minimum`, `maximum` and a `default`;
    * a boolean carries a `default`;
    * an array carries `items` (`enum`, or titled `anyOf`), `minItems`, `maxItems` and default
      selections.
  * A property type the SDK does not know arrives as `Other`, which a client "MUST NOT render … as
    a known input control" (`:4586-4594`).
* An elicitation is scoped either to a **session**, optionally to one tool call, or to a JSON-RPC
  **request** outside any session, "e.g., during auth/configuration phases" (`:2792-2800`,
  `:4919-4924`, `:5055-5068`).
* **URL mode** (`CreateElicitationUrl`, `:2886-2902`) sends the user to a URL. The agent then sends
  `elicitation/complete` (`:2020-2032`).
* **Gates.** The SDK refuses a form unless the client advertised `elicitation.form`, and a URL
  request or `elicitation/complete` unless it advertised `elicitation.url`
  (`types_gen.go:19854-19860`, `:19938-19954`). `Other` modes are not gated.
* **The SDK loses the field order.** `Properties` is a Go map (`:4971`), so the order the agent
  wrote its fields in does not survive decoding. The patch-semantics gap filed as
  https://github.com/maccavelli/acp-go-sdk2/issues/5 is a different defect; this one is not filed.

**What Magic does today.**

* `CreateElicitation` and `CompleteElicitation` return method-not-found, and no elicitation
  capability is advertised (0184 D9; `internal/provider/acpagent/session.go`). The honesty test lists
  `elicitation` as a capability Magic must not advertise (`advertise_test.go`).
* Magic already has a **question form** flow, used by four providers:
  * the events are `question_request`, `question.respond` and `question_resolved`, with
    `event.QuestionItem` (`internal/event/event.go:323-333`) and
    `provider.QuestionSession.RespondQuestion` (`internal/provider/provider.go:191-202`);
  * the providers are OpenCode's and kilo's questions, codex's `requestUserInput`, and grok's
    `_x.ai/ask_user_question` in acpagent (`internal/provider/acpagent/extensions.go:206-271`);
  * acpagent's version already has a timeout, a single-winner answer latch, and a guaranteed
    `question_resolved` on every path (`awaitAnswers`, `:273-343`);
  * answers are write-only in logs (`QuestionAnswers.LogValue`).
* The phone's question sheet (`apps/mobile/lib/features/chat/question_sheet.dart`) draws option
  lists, single or multiple, and free-text and secret fields.
  * It treats **every** field as required (`_valid`, `:51-60`).
  * It has no numeric keyboard, no defaults, no per-field rule, and no way to say "decline" as
    distinct from "Cancel / skip".
* codex's MCP elicitation (`mcpServer/elicitation/request`) is shown as a permission card with
  accept, decline and cancel. Its form is not drawn (`internal/provider/codex/callbacks.go:155-158`).
  [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md)
  D21 planned generic schema forms for codex. They were not built.

**Who would send one.** No agent Magic runs is known to.

* grok asks its questions through `_x.ai/ask_user_question`, and it has never been offered
  elicitation.
* gobble-cli's own ACP client refuses elicitation (`acpclient/conn.go:454-462`), and its agent sends
  none (checked 2026-10-14).
* An agent that relays an MCP server's elicitation, which is what the tool-call scope is for, would
  send one once it is offered.

**The question.** When an agent asks the user to fill in a form, how does Magic show it, check the
answer, and return a typed `accept`, a `decline` or a `cancel`? And which parts of elicitation does
Magic advertise?

## Decision Drivers

* Advertise only what Magic implements end to end (0184 D4). For form mode that means every
  property type, `required`, defaults and constraints, and all three answers.
* D10's bar: daemon and phone behavior, a wire fixture, and a live probe.
* One form contract on the phone, not one per provider. The same schema subset is what MCP servers
  send, through codex today and through ACP agents once relayed.
* Reuse a flow that already has a timeout, a single-winner answer, guaranteed resolution and
  redacted logging.
* A wrong answer should be caught before it is sent, and never turned into a silent cancel.
* An older phone must not break. It may show less.
* URL mode opens a browser on an agent's say-so. It needs a trust policy of its own, so it is not
  advertised here.

## Considered Options

* Map form elicitation onto the existing question flow, extended additively with typed, optional, defaulted fields and a decline
* A new elicitation event and a dedicated schema-driven form on the phone
* Map form elicitation onto the existing question flow unchanged
* Keep refusing elicitation, and do not advertise it

## Decision Outcome

Chosen option: "Map form elicitation onto the existing question flow, extended additively with
typed, optional, defaulted fields and a decline". It covers the whole form contract while reusing
the flow that already handles timeout, resolution and redaction for four providers. It also leaves
one form contract that codex's MCP elicitation can later use. The owner accepted it on 2026-10-14
("accepted, approved to execute. proceed").

Under the chosen option:

* **D1. Scope: session-scoped form mode only.**
  * Magic advertises `elicitation: {form: {}}`, and not `url`.
  * A **request-scoped** form, which has no session to show it in, is answered `cancel` and declined
    by name as `elicitation.request_scope`.
  * An **`Other` mode** is answered `cancel` and declined as `elicitation.mode.<mode>`.
  * **URL mode** stays unadvertised, so the SDK refuses it. It gets its own record, with a trust
    policy covering schemes, host display, and never opening a link automatically.
* **D2. acpagent maps the schema onto question items.** The `message` becomes the request's text.
  The schema's `title` and `description` head the form. Each property becomes one item, keyed by
  its property name:
  * a string with `enum` or `oneOf` becomes a single choice, whose option ID is the value and whose
    label is the title;
  * any other string becomes a text field, carrying its `format`, `minLength`, `maxLength` and
    `pattern`;
  * a number or an integer becomes a number field with its kind, `minimum` and `maximum`;
  * a boolean becomes a two-option choice, "Yes" (`true`) and "No" (`false`);
  * an array becomes a multiple choice, with `minItems` and `maxItems`;
  * each field carries its `default`, and is marked optional unless `required` names it.

  Further rules:
  * **Order.** The field order is the `required` list's order, then the remaining names sorted.
    The agent's own order is lost in the SDK (see the Context).
  * **Unknown property types.** An `Other` property that is optional is left out, and declined as
    `elicitation.schema.<type>`. One that is required means the form cannot be completed, so the
    elicitation is answered `cancel`, with a daemon notice saying why.
  * **Patterns.** A `pattern` that Go's RE2 cannot compile is not enforced, and is declined as
    `elicitation.schema.pattern`. The agent still validates its own input.
* **D3. The answer is checked, then typed.**
  * `accept`: the daemon validates every field against the schema, coerces it (integer, number,
    boolean, string, or string list), and sends `content`. Optional fields left empty are omitted.
  * An answer that fails validation is refused at `question.respond`, with an error naming the field
    and the rule. The form stays open. It is never sent as `cancel`.
  * `decline` is a new, explicit choice on `question.respond`, offered only on a form whose
    `question_request` says `allow_decline`.
  * `cancel` covers the existing cancel, the timeout, and the session closing.
* **D4. The question contract grows additively.**
  * `event.QuestionItem` gains `kind` (`text`, `number`, `integer`), `optional`, `default` (a string
    list), `min`, `max`, `min_length`, `max_length`, `min_items`, `max_items`, `format` and
    `pattern`.
  * `question_request` gains `allow_decline`, and `question.respond` gains `declined`.
  * `RespondQuestion` carries the decline through. A provider without a decline treats it as cancel.
    *Note, 2026-10-14:* implemented as an optional `QuestionDeclineSession.DeclineQuestion`, plus
    `Manager.DeclineQuestion`, rather than a new `RespondQuestion` parameter. A session without the
    interface receives a cancel. The behavior is as stated; see the PLAN's deviation entry.
  * Every new field is omitted when unused, so the other three providers' forms are unchanged on the
    wire.
* **D5. The phone honors the new fields.** Optional fields may be left empty. Number fields use a
  numeric keyboard. Defaults are prefilled. Each constraint is shown and checked before Submit. A
  "Decline" button appears when `allow_decline` is set. A daemon refusal reopens the form with its
  message.
  * A phone without D5 still draws the form: every field required, numbers as text, no decline. The
    daemon's validation then catches a bad number. That is less, but never wrong.
* **D6. Magic advertises `elicitation.form`** in `defaultClientCapabilities`, and adds it to
  `implementedClientCapabilities`, only when D1–D5 ship. The honesty test's dishonest example becomes
  `elicitation.url`. 0184 D9 ("Elicitation compiles, but is not offered") is then superseded for form
  mode.
* **D7. The probe.**
  * A zero-token run (`initialize` and `session/new`) against grok records whether grok accepts the
    advertisement.
  * A run with model turns, which waits for the owner's quota, records whether grok then sends
    `elicitation/create` rather than `_x.ai/ask_user_question`.

  The fixture is built from the schema unless a probe captures a real frame.

### Consequences

* Good, because an agent, or an MCP server behind it, can ask the user for typed input and get a
  checked, typed answer.
* Good, because every provider's questions gain optional fields, defaults and numeric input, and
  there is one form contract for codex's MCP elicitation to adopt later.
* Good, because the timeout, single-winner answer, guaranteed resolution and redacted logging are
  reused, not rebuilt.
* Neutral, because no agent Magic runs is known to send elicitation yet.
* Neutral, because the agent's field order is lost. Required fields come first, then the rest
  sorted by name. An SDK change could restore the order.
* Bad, because this is a wider contract change than 0187–0190: the question item, the request, the
  respond message, the provider interface and the phone sheet all change. Each needs tests.
* Bad, because a pattern outside RE2 is not checked by Magic. The agent is the backstop.
* Bad, because URL mode stays unavailable, so an agent that needs a browser flow such as OAuth
  cannot use one until its record lands.

### Confirmation

* An acpagent test sends forms built from the SDK types, covering:
  * each property type; `enum` and `oneOf`; defaults; `required`;
  * a known and an unknown `format`; a pattern RE2 compiles and one it cannot;
  * an optional and a required unknown type;
  * a request-scoped form, and an `Other` mode.

  It requires the question items, the declines by name and the cancels in D1–D2.
* An acpagent test answers those forms with valid, invalid, empty-optional, declined, cancelled and
  timed-out answers. It requires the typed `content`, the refusals at `question.respond`, `decline`
  and `cancel`, and a `question_resolved` on every path.
* A protocol test round-trips the new fields, and shows that a form without them encodes exactly as
  before.
* Flutter widget tests cover an optional field, a numeric field, defaults, a constraint message, the
  Decline button, and a refused answer reopening the form.
* The A2 wire test pins `initialize` with `elicitation.form`. The honesty test fails if `elicitation`
  or `elicitation.url` is advertised without behavior.
* Each new test is seen failing first, and planted breaks are shown failing on a scratch copy.

## Pros and Cons of the Options

### Map form elicitation onto the existing question flow, extended additively with typed, optional, defaulted fields and a decline

* Good, because the flow's timeout, single-winner latch, resolution and redaction are proven across
  four providers.
* Good, because the extension serves every provider's questions, not just ACP's.
* Neutral, because the question item grows eleven optional fields.
* Bad, because changes to the provider interface and the respond message touch every question
  provider, even though they are additive.

### A new elicitation event and a dedicated schema-driven form on the phone

* Good, because the event can mirror ACP's schema exactly, including the decline.
* Bad, because the timeout, latch, resolution and redaction must be built a second time.
* Bad, because the phone then has two form renderers, and the codex MCP path would have to choose
  between them.

### Map form elicitation onto the existing question flow unchanged

* Good, because it needs no protocol or phone change.
* Bad, because optional fields become required, numbers become free text, and there is no decline.
  Advertising `elicitation.form` would claim a contract Magic does not keep.
* Bad, because a mistyped number could only be answered `cancel`, which turns the user's typo into
  a refusal.

### Keep refusing elicitation, and do not advertise it

* Good, because nothing changes, and no agent Magic runs sends elicitation.
* Bad, because an agent that relays an MCP server's question has no way to reach the user.

## More Information

* The facts above were read from the code on 2026-10-14, at `13c04fc6`, from
  `acp-go-sdk2@v0.17.1`, and from gobble-cli.
* **A possible ask for `acp-go-sdk2`, not filed.** Keep `ElicitationSchema.Properties` in the order
  the agent sent, for example as an ordered list beside the map. Filing it is outward-facing and
  waits for the owner's word.
* The PLAN is
  [0191-PLAN-answer-agent-elicitation-forms-on-the-phone.md](0191-PLAN-answer-agent-elicitation-forms-on-the-phone.md).
  ~~Nothing is implemented until the owner accepts an option and approves the PLAN.~~ The owner
  accepted the recommended option and approved the PLAN on 2026-10-14.

## Amendment — 2026-10-14: D7's zero-token probe ran

Magic advertises `elicitation.form` (PLAN L4). D6 rests on grok accepting that advertisement. D7
asked whether grok uses it. The zero-token half of the probe answered both for session start.

* **Evidence.** `TestLiveGrokElicitationAtSessionStart`
  (`internal/provider/grok/live_elicitation_test.go`, `live_grok` tag) ran against
  `grok 1.0.50 (c58f321264ba) [stable]` with wire capture.
  * grok accepted the `initialize` carrying `elicitation: {form: {}}` and opened the session.
  * In the 12 seconds after `session/new`, it sent 29 frames: no `elicitation/create`, and no error.
* **Consequence.** The advertisement is safe for grok `1.0.50` at session start. The fixture remains
  the schema-built requests, as D7 allows.
* **Still open.** Whether grok asks the user through `elicitation/create` rather than its own
  `_x.ai/ask_user_question`, now that both are offered. That needs model turns, so it waits for the
  owner's quota.
