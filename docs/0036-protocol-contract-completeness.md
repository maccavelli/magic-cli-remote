# MADR 0036: Protocol contract completeness — specify the vocabularies, pair the failure, guard the drift

- **Status**: Accepted — all phases implemented 2026-07-27
- **Date**: 2026-07-27
- **Deciders**: Project Owner
- **Related**: [MADR 0035](./0035-codex-ui-ux-remediation.md) (codex remediation —
  added the `tool_status` table this MADR generalises; introduced the `error`
  stop reason whose client handling is fixed here),
  [MADR 0023](./0023-canonical-slash-commands.md) (the conformance-test pattern
  reused for drift guards), [MADR 0024](./0024-stream-coalescing.md),
  [MADR 0034](./0034-opencode-tool-stream-fidelity.md)
- **Evidence**: audit of `docs/protocol-v1.md` against the tree at `92372a9`
- **Companion plan**: [protocol-contract-completeness-implementation-plan.md](./protocol-contract-completeness-implementation-plan.md)

---

## 1. Problem

`protocol-v1.md` is the contract for a client the daemon does not ship. An audit
of all 1008 lines against the code found the spec accurate where it is specific
— the client→server table (24 rows), session `Meta` (10 fields), the `plan`
event, `session_capabilities` (9 fields) and both HTTP endpoints all match
exactly — and silently incomplete everywhere it is not.

### 1.1 `stop_reason` is unspecified, and `error` reaches the user as noise

MADR 0035 D5 mapped codex's `failed` turn status to `stop_reason: "error"`,
paired with a `TypeError` carrying the engine's message
(`codex/session.go:1188-1198`). acphttp has emitted the same reason since
earlier (`acphttp/session.go:398-414`).

The mobile reducer has no arm for it:

```dart
// transcript_reducer.dart
if (reason.isNotEmpty && reason != 'end_turn' && reason != 'end-turn') {
  next = _append(next, ChatItem.system(_humanStopReason(reason)));
}

String _humanStopReason(String reason) => switch (reason) {
  'refusal' => 'The agent declined to continue.',
  'max_tokens' => …,
  'max_turn_requests' => …,
  _ => 'Turn ended ($reason)',        // ← "Turn ended (error)"
};
```

So a failed codex turn produces **two** transcript lines: a contentless
"Turn ended (error)" and the real error. That is the exact noise class MADR 0035
set out to remove — `failed` traded "Turn ended (failed)" for "Turn ended
(error)".

Worse, the two providers disagree on whether the pairing exists at all.
`codex.emitTurnComplete` skips the `TypeError` when the engine supplied no
message, and its test records the reasoning:

> `TestEmitTurnCompleteErrorNoMessageSkipsTypeError`: an "error" stop with an
> empty turn-error message does not emit a TypeError (no noise on empty
> errors). **The mobile reducer treats "error" stop as terminal anyway; the user
> sees the canonical "Turn ended (error)" line.**

The reducer does no such thing — there is no `'error'` handling — so that path
produces the uninformative line *and nothing else*. The comment describes client
behaviour that was never implemented, which is precisely what an unspecified
protocol field permits.

The root cause is structural: `stop_reason` is documented as an open example
list (`e.g. end_turn, max_tokens, refusal, cancelled`), omitting `error`. MADR
0035 §2.11 closed exactly this hole for `tool_status` after codex drifted to
`in_progress`. `stop_reason` is the same class of field and was left open.

### 1.2 Two event types are enumerated but never specified

`usage_update` and `session_config` each appear exactly once in the document —
inside the `Event type values` list. There is no section, no payload, no field
table. The structs they carry are entirely undocumented: `Usage{used,size}`,
`ConfigOption{id,name,description,kind,current_value,bool_value,values}` and
`ConfigOptionValue{id,name}`. The string `config_options` appears **zero** times.

This is worse than a plain omission because `session.set_config_option` **is**
documented as a client request (`{session_id, option_id, kind, value}`). A
client is told how to set a config option but never what advertises the
available options, nor which `kind` values are legal.

### 1.3 The canonical event-type list is missing three of twenty-one types

The list names 18 types; `internal/event` defines 21. Absent:

- `question_request`, `question_resolved` — described elsewhere in the document
  (the `question.respond` section) but missing from the enumeration a client
  would implement against.
- `session_title` — **zero mentions anywhere in the document.** It is emitted
  live (`acphttp/session.go:959`), is a control event that must not be dropped
  (`event.go:112`), and carries the otherwise-undocumented `title` field.

### 1.4 `timed_out` is undocumented

Set on `permission_resolved` alongside `status: cancelled` by codex
(`session.go:1023`) and acphttp (`session.go:1042`). The section documents both
status values and then states *"The event carries no `options` and no
`tool_id`"* — an exhaustive-sounding sentence that omits the field. A client
cannot distinguish "the agent abandoned this request" from "you took too long
to answer", which warrant different copy.

### 1.5 Twelve error codes are emitted and undocumented

`bad_version`, `permission_failed`, `session_cancel_failed`,
`session_diff_failed`, `session_history_failed`, `session_revert_failed`,
`session_set_config_failed`, `session_set_mode_failed`,
`session_unrevert_failed`, `unknown_type`, `session_limit`, `shutting_down`.
(`unauthorized` appears once, as prose.)

The document specifies codes for some messages (`auth`, `pair.claim`,
`session.create`, `session.prompt`) and not others, with no stated rule for
which. Codes are string literals scattered through `ws/server.go`, so nothing
relates them to the spec.

### 1.6 The provider enum is stale and self-contradicting

Line 281 says `provider` is `fake`, `grok`, or `opencode`. `goose` and `codex`
are registered (`provider.go:31-39`) — and the document's own example at line
356 uses `goose`. The adjacent `model` guidance covers only grok and opencode,
omitting codex's per-turn model semantics.

### 1.7 The `tool_status` table is stricter than the code

The new table says "Allowed values" and "Each provider is responsible for
translating its native vocabulary to these four values". Both reference
implementations pass unknown values through (`return s` —
`codex/items.go:59-71`, `opencode/http.go:1092-1105`), and codex's test states
it is deliberate: *"Unknown inputs intentionally fall through as-is — the
contract is 'do not invent a fake status'"*. That is a reasonable decision; the
document does not record it, so the table reads as a guarantee it is not.

### 1.8 `attachments` is undocumented as an event field

`AttachmentInfo{kind,mime_type}` rides `user_message` from fake, acpagent,
codex and acphttp. The document mentions `attachments` only as a
`session.prompt` request field.

---

## 2. Decision

### 2.1 D1 — `error` is always paired with an error event, and renders once

Two halves of one contract.

**Daemon.** `stop_reason: "error"` **must** be accompanied by a `TypeError` for
the same turn. Where the engine supplied no message, the provider substitutes a
generic one rather than omitting the event. acphttp already satisfies this;
codex is changed to.

**Client.** `_onTurnComplete` treats `error` like `end_turn` — no system bubble.
The paired error event is the user-visible report.

This inverts `TestEmitTurnCompleteErrorNoMessageSkipsTypeError`. That test
optimised for "no noise on empty errors" but produced the opposite: an
uninformative "Turn ended (error)" line and no message. One informative line
beats one uninformative line, and a single invariant ("error implies an error
event") is far easier for a client to implement than a conditional one.

### 2.2 D2 — `stop_reason` gets a specified vocabulary

A closed table, in the same shape MADR 0035 gave `tool_status`:

| Value | Meaning | Client rendering |
|---|---|---|
| `end_turn` | normal completion | silent |
| `cancelled` | the user or the daemon interrupted the turn | "Turn cancelled", once |
| `error` | the turn failed; **always paired with an `error` event** | silent — the error event reports it |
| `refusal` | the agent declined to continue | its own line |
| `max_tokens` | the reply hit the model's length limit | its own line |
| `max_turn_requests` | the agent hit its per-turn request limit | its own line |

Providers translate their native vocabulary at the emit boundary. As with
`tool_status`, an unrecognised value passes through rather than being
fabricated, and the client renders it generically — that escape hatch is
documented rather than implied (D5).

### 2.3 D3 — Specify every enumerated event type

`usage_update` and `session_config` get sections with payload shapes and field
tables. `session_title` is added to the enumeration and given a section.
`question_request` and `question_resolved` are added to the enumeration
(their behaviour is already described). `timed_out` and `attachments` are
documented on the events that carry them.

The rule going forward: **if it is in the `Event type values` list, it has a
section or a field entry.**

### 2.4 D4 — Error codes become a registry, enforced by an AST walk

Declare every protocol error code as a named constant in `internal/protocol`,
with an exported `ErrorCodes()` enumeration, and document all of them.

Enforcement is a test that **parses the `internal/ws` package with `go/ast`**
and asserts every string literal used as an error code is registered. It
recognises two shapes, which between them cover all 71 emit sites:

- argument index 3 of `writeError` / `writeSessionErr` / `writePairError`;
- a literal assigned to an identifier named `code` (how `writeAuthError` and
  `writeSessionErr` pick their value).

An AST walk rather than a source-wide rename: it enforces the property that
actually matters (every emitted code is registered and therefore documented)
without a 71-site mechanical edit, and unlike a regex it is not sensitive to
formatting. Call sites may adopt the constants opportunistically; the guard does
not depend on them doing so.

### 2.5 D5 — Document the pass-through escape hatch

Both `tool_status` and `stop_reason` record that a provider emits an
unrecognised native value as-is rather than inventing one, and that clients must
degrade gracefully. This turns an undocumented behaviour into a stated contract
without changing any code.

### 2.6 D6 — Drift guards

Two tests, in the spirit of MADR 0035 D6 (which converted "advertised but dead
commands" into test failures):

1. Every `event.Type` constant appears in `protocol-v1.md`'s event-type list.
2. Every registered error-code constant appears in `protocol-v1.md`.

Both read the document at test time. A new event type or error code that is not
documented fails the build.

**Scope limit, deliberately.** These guards prove *presence*, not correctness —
they cannot tell that a field's description is accurate. That is the honest
boundary of a cheap mechanical check, and it is exactly the failure mode that
occurred: `session_title` shipped with zero mentions, and no test noticed.

### 2.7 Also corrected

The provider enum and the `model` guidance are updated for `goose` and `codex`.

---

## 3. Consequences

### 3.1 Positive

- A failed turn reports once, with content, instead of twice with a contentless
  line first.
- Two event types stop being undocumented, and one stops being invisible.
- A client implementer can enumerate every event type and error code from the
  spec alone.
- Doc rot on the two enumerable surfaces becomes a test failure.

### 3.2 Costs and risks

- **D1 changes emitted events for codex.** A previously-silent failure path now
  emits an error event. That is the intent, but it is a wire-visible change.
- **D1 inverts an existing test** and the decision it recorded (§2.1).
- **D4 touches ~24 call sites** in `ws/server.go`. Mechanical, but it is the
  request-handling path, so it lands on its own commit.
- **D6 couples tests to a documentation file.** Moving or renaming
  `protocol-v1.md` breaks them. Acceptable: the file is the protocol contract
  and its path is stable; the test failure message says what to update.

### 3.3 Explicitly not decided

- **No new event types, no wire-format changes** beyond D1's added error event.
- **No client-facing behaviour change** other than suppressing the `error`
  bubble.
- **No attempt to guard field-level accuracy.** See §2.6.

---

## 4. Rejected

| Rejected | Why |
|---|---|
| Add an `'error'` arm to `_humanStopReason` ("The turn failed.") | One-line client fix, but it double-reports whenever an error event is present — which after D1 is always |
| Suppress the `error` bubble without the daemon pairing | Makes codex's empty-message path a silent failure: no bubble, no error event, nothing |
| Stop emitting `error` as a stop reason | The turn genuinely failed; the status has to carry it for session state |
| Clamp unknown `tool_status`/`stop_reason` to a fallback | Fabricates a status the provider never reported. The existing pass-through is the right call; it just needed documenting |
| Generate `protocol-v1.md` from the Go types | The document carries rationale, invariants and client guidance that no generator can produce. The guards check presence instead |
| Leave error codes as literals with no registry at all | Nothing then relates an emitted code to the spec; `session_title` shipped undocumented exactly this way |
| Mechanically rewrite all 71 emit sites to use the constants | The AST guard already enforces registration, which is the property that matters. A 71-site rename adds churn to the request-handling path for a compile-time nicety, and can be adopted incrementally instead |
| Enforce the registry with a regex over Go source | Breaks on formatting. `go/ast` parses the real syntax tree and is not sensitive to layout |

---

## 5. Verification

| Claim | How verified |
|---|---|
| `error` yields two transcript lines | `transcript_reducer.dart` `_onTurnComplete` + `_humanStopReason` have no `'error'` arm |
| codex skips the error event on an empty message | `codex/session.go` `emitTurnComplete`; `TestEmitTurnCompleteErrorNoMessageSkipsTypeError` |
| acphttp always pairs | `acphttp/session.go:398-414` — `err` is non-nil by construction |
| `usage_update` / `session_config` appear once each | `grep -c` on the document |
| `config_options` appears zero times | `grep -c` |
| 3 of 21 event types missing from the list | Diff of `event.Type` constants against the line-766 enumeration |
| `session_title` has zero mentions | `grep -c`; emitted at `acphttp/session.go:959` |
| `timed_out` undocumented | `grep -c` = 0; set at `codex/session.go:1023`, `acphttp/session.go:1042` |
| 12 error codes undocumented | Diff of `writeError`/`writeSessionErr` codes against the document |
| Provider enum stale | `provider.go:31-39` defines 5 ids; document names 3, and uses `goose` in its own example |
| `tool_status` pass-through | `codex/items.go:59-71`, `opencode/http.go:1092-1105`, `codex/status_test.go` comment |
| Spec is accurate where specific | Field-by-field diff of `Meta`, `Capabilities`, `PlanEntry`, both HTTP endpoints, and the 24-row request table — no drift found |

---

## 6. Implementation

Phased, in
[protocol-contract-completeness-implementation-plan.md](./protocol-contract-completeness-implementation-plan.md).

Summary: **P0** D1 error pairing and single render → **P1** D2/D3/D5/§2.7
documentation → **P1** D4 error-code registry → **P1** D6 drift guards →
verification.

### Implementation record

All phases landed 2026-07-27. `make preflight`, `make race` and `make test-all`
(327 Flutter tests included) are green.

| Decision | Landed as |
|---|---|
| D1 | `codex/session.go` `emitTurnComplete` substitutes `genericTurnError` when the engine gave no message; `transcript_reducer.dart` `_onTurnComplete` skips the stop line for `error`. `TestEmitTurnCompleteErrorNoMessageSkipsTypeError` inverted to `…StillEmitsTypeError`, plus `…NonErrorStopKeepsNoErrorEvent` scoping the pairing to `error`, plus three mobile reducer tests. |
| D2, D3, D5, §2.7 | `protocol-v1.md`: `stop_reason` table with the pairing rule; `usage_update`, `session_config`, `session_title`, `question_request`/`question_resolved` sections; `timed_out`, `attachments`, `title` field entries; pass-through paragraphs on both vocabularies; provider enum and `model` guidance corrected. |
| D4 | `internal/protocol/errors.go` — 41 registered codes with `ErrorCodes()`; `internal/ws/error_codes_test.go` walks the package AST. |
| D6 | `internal/protocol/doc_coverage_test.go` (event types + error codes vs the spec), `internal/event/types_test.go` (`Types()` vs declared constants), plus `event.Types()`. |

**Two things the work surfaced that the audit had not:**

1. **A 41st error code.** The AST guard found `bad_json`
   (`server.go:535`) on its first run. The manual audit missed it because it is
   the one code emitted with an empty request id — the envelope failed to parse,
   so there was no id — and so it did not match the `env.ID, "…"` grep every
   other site does. It was undocumented too. This is the guard justifying itself
   within minutes of existing.
2. **`make preflight` was already red at `92372a9`.** `codex/session.go`
   retained a `resetStallTimer` no-op shim "so older callers compile" after MADR
   0035 D8 replaced it with the ticker; it had zero callers and staticcheck
   flagged it U1000. Pre-existing, not caused by this work, but it blocked the
   gate, so the dead shim was removed here.

**Deliberately not done:** the 71 emit sites in `internal/ws` still use string
literals rather than the new constants. The AST guard enforces registration
regardless (§2.4), so the rename is cosmetic and can be adopted incrementally
without a churn commit through the request path.
