---
status: accepted
date: 2026-11-09
decision-makers: Project Owner
consulted: "this repository at the commit after 0179 P3's follow-up: internal/agenterr, internal/event, internal/provider/acpagent, internal/provider/codex, docs/guides/protocol-v1.md, apps/mobile/lib/features/chat/chat_bubble.dart; gobble-cli at v0.5.0 (errors, retry and acpext unchanged since v0.4.0) and its records; go-llmprovider-sdk at v1.28.0 and its records; the gobble 0.4.0 capture of 0179 PLAN P3 (an OpenCode Zen free-tier limit); a scratch probe of agenterr.Present on that capture"
informed: gobble-cli, go-llmprovider-sdk
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Show exactly which usage limit an agent hit and when it lifts

## Context and Problem Statement

When a model call fails on a limit, the phone shows a card: "Agent quota exceeded" or "Agent rate-limited", "The
limit resets at …" when the daemon found a time, and the daemon's text as fine print. The owner wants that card to say
exactly what happened, as accurately as the agent can tell: a rolling window such as a 5-hour limit that resets in a
few hours, against a weekly or billing-period quota, against a free tier, against plain throttling. And whatever an
agent such as gobble already sends should reach the user, not be lost on the way.

0179 PLAN P3's capture found the first case of the opposite: gobble reported an OpenCode Zen free-tier limit with its
reset time and the provider's own advice, and the phone said "The model provider had a server error. This is usually
temporary".

The question this record answers: what must change, in this repository and in gobble-cli, so a limit is shown as the
limit it is, with when it lifts, from what the agents can send today and what they could send next.

### Findings

#### F1. gobble's quota error is presented as a server error [observed 2026-11-09]

gobble 0.4.0 sent, for an OpenCode Zen free-tier limit:

```text
{"code":-32603,"message":"Internal error","data":{"error":"llm: api status 429: llm: quota exhausted: llmprovider: quota exhausted: opencode-zen/chat_completions HTTP 429 FreeUsageLimitError (retry-after 1h33m21s): Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to OpenCode Go to keep going: https://opencode.ai/go"}}
```

A scratch probe of `agenterr.Present` on that error: `ExtractText` returns "Internal error", the kind is `server`, no
reset, and the message is the fixed server-error copy. `extractJSONMessage` mines a JSON-RPC `data` that is a string,
or an object holding `message` (`internal/agenterr/agenterr.go:474-515`); gobble's `data` is an object holding a
string `error` (acpext's documented `ErrorData`), which falls through.

#### F2. Even its text loses the reset and reads as a dump [observed]

The same probe on the bare `data.error` string classifies it `quota`, correctly (`freeusagelimiterror` is a quota
word, `agenterr.go:93`), but:

* **no reset is found.** The SDK prints its retry-after as a Go duration, `(retry-after 1h33m21s)`, and no pattern in
  `parseReset` reads it: `reAfter` needs a unit and a word boundary after a single number, and `reCompound` and
  `reRelative` need "in" (`agenterr.go:776-779`). The SDK can also print fractions, `2h9m59.87321s`.
* **the message is the raw chain,** with gobble's and the SDK's prefixes (`llm: api status 429: llm: quota
  exhausted: llmprovider: quota exhausted: …`) ahead of the provider's own two sentences.

#### F3. Nothing on the wire says which limit it was [observed]

An `error` event carries `error_kind` (`quota`, `rate_limit`, `auth`, `server`, `permission`) and `retry_at`
(`internal/event/event.go:724-737`; `docs/guides/protocol-v1.md:1432-1450`). Nothing names the window (5 hours, a
day, a week, a month), a free tier, a credit balance, or the upstream. The phone's `_LimitNotice` has three titles and
one sentence pattern (`apps/mobile/lib/features/chat/chat_bubble.dart:838-905`).

#### F4. Codex already sends windows, and this repository drops them [observed]

Codex's `account/rateLimits/updated` carries a primary and a secondary window, each with `usedPercent`,
`windowDurationMins` and `resetsAt` (`internal/provider/codex/session.go:1741-1787`). The daemon reads the primary
alone, turns 100% into `rate_limit` with the reset, and drops the window's length; the secondary (weekly) window is
not read.

#### F5. What gobble and its SDK know, and what they send [observed, gobble-cli v0.5.0, go-llmprovider-sdk v1.28.0]

* **The SDK's `*APIError`** holds `Provider` (`opencode-zen/chat_completions`), `Status`, `Kind` (a sentinel:
  `ErrQuotaExhausted`, `ErrRateLimited`, `ErrNotPermitted`, …), `Code` (`FreeUsageLimitError`), `Message` (redacted,
  at most 512 bytes), `RetryAfter` and `Reason` (`llmprovider/api_error.go:100-124`). It has no absolute reset time,
  window, plan or remaining amount: an absolute `resets_at` or HTTP-date is turned into a relative `RetryAfter`.
* **It classifies by provider code:** OpenCode `FreeUsageLimitError`, `GoUsageLimitError`, `BlackUsageLimitError`
  (429) and `CreditsError`, `MonthlyLimitError`, `UserLimitError` (401) as quota; Kilo `PROMOTION_MODEL_LIMIT_REACHED`;
  ChatGPT `usage_limit_reached`, `insufficient_quota`; any 402; Gemini per-day quota; any other 429 as a retryable
  rate limit (`api_error.go:382-471`).
* **gobble sends the client one string.** `promptError` (`acpserver/prompt.go:267-278`) puts `err.Error()` into
  `ErrorData.Error`; the SDK error is still in the chain in-process, with every field above, and none reaches the
  client as a field. `ErrorData` has `command`, `error`, `provider`, `reason`, `sessionId`; `provider` is never filled
  there (`acpext/types.go:327-339`).
* **`_gobble/retry`** carries `cause` (`quota`, `rate-limited`, …), `attempt`, `maxAttempts`, `delayMs` and, at an
  end, `error`. A wait longer than gobble's 60 s cap is not retried and sends no `_gobble/retry` at all; exhausted
  quota is never retried.
* **Nothing distinguishes a 5-hour window from a weekly or monthly quota,** in the SDK or gobble. The only signals are
  the provider's code and the retry-after's length.
* **One suspected fault:** OpenCode's 401 quota codes (`MonthlyLimitError`, `CreditsError`, `UserLimitError`) may be
  worded by gobble as "rejected the API key", since the SDK also unwraps them to its auth sentinel
  (`llm/provider/provider.go:275-285`). Read from the code, not tested.

#### F6. Other agents reach the same classifier as text [observed]

grok, opencode and kilo errors reach `agenterr` as text through `acpagent`'s prompt-error path
(`internal/provider/acpagent/session.go:782-800`); codex and kilo have structured limit paths of their own
(0035 D9; 0138 F9). A fix in `agenterr` helps each.

## Decision Drivers

* Accuracy: say which limit, when it lifts, and the provider's own words, and never guess a window that no source
  states.
* Use what the agents already send before asking for more; ask gobble for structure where text cannot carry it.
* Agent-neutral: `agenterr`, the wire and the phone serve every agent, and vendor knowledge stays in its package.
* Additive on the wire: an older phone still shows today's card.
* No secret on the wire: provider messages arrive redacted by the SDK (F5), and the daemon adds nothing.

## Considered Options

* A. Text fixes only, in `agenterr`
* B. Text fixes, plus a limit description on the wire, from every source there is, and a request to gobble-cli for
  structured data
* C. Wait for gobble-cli to send structured data, and change nothing here until then

## Decision Outcome

Chosen option: "B", because A fixes F1 and F2 but still cannot say which limit it was (F3, F4), and C leaves the phone
wrong for every agent until another repository ships, when most of the gain needs no gobble change.

### D1. Read a JSON-RPC error's data when it is an object of strings

`agenterr` takes the cause from an object `data` whose `error`, `message` or `reason` member is a string, as it already
does for a string `data` and a nested `message`. The JSON-RPC `message` ("Internal error") is the wrapper, not the
cause. This fixes F1 for gobble and for any agent that uses the same shape.

### D2. Read a Go-duration retry-after

`parseReset` reads `retry-after <d>` and `(retry-after <d>)` where `<d>` is a Go duration, compound and fractional
(`1h33m21s`, `2h9m59.87321s`, `45s`, `250ms`), with `time.ParseDuration`, before its older patterns.

### D3. A limit description, from the most explicit source available

`agenterr.Classification` gains a `Limit` with:

* `Scope`, an open set: `rate` (throttling), `window` (a rolling window of stated length, such as 5 hours), `daily`,
  `weekly`, `monthly`, `credits` (a balance or billing that does not reset by time), `free_tier`, `plan` (a plan's
  usage limit whose window is not stated);
* `WindowMinutes`, when a source states the window;
* `Provider`, the upstream the agent named (`opencode-zen`, the SDK's route suffix dropped);
* `Code`, the provider's own code (`FreeUsageLimitError`);
* `ProviderMessage`, the provider's own sentences, with agent and SDK prefixes removed (D4).

Its sources, most explicit first, and the first that says something wins:

1. **Structured data from the agent:** codex's rate-limit windows (D6), and gobble's structured `ErrorData` once
   gobble-cli sends it (D7). A Spec hook carries it, so `agenterr` names no vendor.
2. **A provider-code table** in `agenterr`, for codes whose meaning is documented: `FreeUsageLimitError` and
   `PROMOTION_MODEL_LIMIT_REACHED` → `free_tier`; `MonthlyLimitError` → `monthly`; `CreditsError`,
   `insufficient_quota`, `insufficient_balance`, HTTP 402 → `credits`; `GoUsageLimitError`, `BlackUsageLimitError`,
   `usage_limit_reached` → `plan`; Gemini per-day quota → `daily`; a plain rate limit → `rate`.
3. **Phrases in the text** that state a window: "5-hour limit", "weekly limit", "monthly limit", "per day", "daily
   limit".

**A window is never inferred from how long the wait is.** A 4-hour retry-after does not make a 5-hour window (F5). When
no source states the scope, `Scope` is empty and the card says what it knows.

*Note, 2026-11-09, from the PLAN's P1, two things this decision implies and did not say:*

* *A phrase that states the window refines `plan`, whose meaning is "window not stated": `GoUsageLimitError` with
  "5-hour usage limit reached" is `window`, 300 minutes. A code with any other scope is not refined.*
* *An explicit HTTP 402 (`HTTP 402`, `status 402`, "402 Payment Required") is now classified as kind `quota`, which
  `agenterr` did not do before, so that "HTTP 402 → `credits`" has a quota to describe. go-llmprovider-sdk classifies
  any 402 as quota. A bare "402", which could be a port or an id, is not.*

*Note, 2026-11-09, from the PLAN's P4, how source 1 applies to gobble's `ErrorData` (acpext v0.3.0):*

* *The same refinement holds over gobble's data: gobble's `limit` `plan` yields to a window, day, week or month the
  text states, because acpext defines `plan` as "window not stated" too. Any other scope gobble sends wins over the
  text's.*
* *The hook describes a limit only when gobble's data names one: a `limit`, or kind `quota` or `rate-limited`. A
  provider outage, which also carries a provider and a code, keeps the text's reading, as `agenterr` gives no limit for
  kinds other than quota and rate limit.*
* *acpext carries no window length, so `WindowMinutes` stays the text's.*

### D4. Show the provider's own words

The `error` text of a limit is the provider's sentences, with known agent and SDK wrappers removed (`llm: api status
N:`, `llm: <kind>:`, `llmprovider: <kind>:`, `<provider> HTTP N <code> (retry-after <d>):`, `failed after N attempts:`),
so gobble's case reads "Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to
OpenCode Go to keep going: https://opencode.ai/go". The parts removed are not lost: they are the limit's fields (D3)
and `retry_at`. When nothing useful is left, today's fixed copy stands.

### D5. The wire carries it, additively

An `error` event gains, each optional and absent when unknown: `limit_scope`, `limit_window_minutes`,
`limit_provider` and `limit_code`. `error_kind`, `retry_at` and `error` keep their meaning. `docs/guides/protocol-v1.md`
documents them, with "clients must degrade unknown values" as for `error_kind`. An older phone ignores them and shows
today's card, with the better `error` and `retry_at`.

### D6. Codex's windows reach the phone

Codex's handler reads both windows. The one at 100% becomes the error, with `limit_scope` `window` and
`limit_window_minutes` from `windowDurationMins`, or `weekly` for a window of 10,080 minutes, and its `resetsAt`. A
window at 90% or more stays a notice, now naming which window.

### D7. gobble-cli is asked for structured data; this repository reads it when it arrives

A gobble-cli issue, opened after this record is accepted, asks for the requirements in More Information, and a
go-llmprovider-sdk issue asks for the SDK's part (requirement 6; Q1), each linking the other. *Opened 2026-11-09:
[maccavelli/gobble-cli#14](https://github.com/maccavelli/gobble-cli/issues/14) and
[maccavelli/go-llmprovider-sdk#13](https://github.com/maccavelli/go-llmprovider-sdk/issues/13).* Until a gobble release
sends them, D1 to D4 read gobble's text. When one does, the gobble Spec decodes acpext's new fields
through D3's hook, a dependency change under this record's PLAN.

### D8. The phone says it plainly

`_LimitNotice` titles the card by scope ("5-hour limit reached", "Weekly limit reached", "Monthly limit reached",
"Daily limit reached", "Free tier limit reached", "Out of credits", "Plan usage limit reached", "Rate-limited"),
names the upstream when known, and says when it lifts: an absolute time and how long from now, and for `credits`
that it will not lift by itself. The provider's message is shown in full (it is short once D4 has cleaned it), not
cut to three lines. With no scope, today's titles stand.

### D9. Not in this record

* Showing remaining quota before it runs out: no agent reports it (F5).
* gobble's own provider state (`/providers` showing "quota exhausted" with no reset time): gobble-cli's, in the issue.
* kilo's and grok's structured quota paths (0138 F9): unchanged.

### Consequences

* Good, because gobble's limit reads as the limit it is, with its reset and the provider's advice, from what gobble
  already sends.
* Good, because codex's 5-hour and weekly windows reach the phone by name.
* Good, because every agent whose error passes `agenterr` gains D1, D2 and D4.
* Good, because the wire change is additive and an older phone is unaffected.
* Neutral, because the provider-code table is vendor vocabulary in `agenterr`, as its word lists already are.
* Bad, because the window of a plan limit (`GoUsageLimitError`, `usage_limit_reached`) stays unknown until the SDK and
  gobble parse more than they do (F5); the card says "Plan usage limit reached" with the reset, not "5-hour".
* Bad, because full accuracy for gobble needs work in two other repositories, on their schedule.

### Confirmation

1. `agenterr.Present` on 0179's captured gobble error gives kind `quota`, scope `free_tier`, provider `opencode-zen`,
   code `FreeUsageLimitError`, a reset 1h33m21s after the capture's time, and the provider's two sentences as the
   message. Each piece is seen failing first, on today's code.
2. Table tests for D2's durations, D3's code table and phrases, and D4's wrapper removal, including a case where no
   scope is stated and none is inferred.
3. The codex handler maps a 300-minute and a 10,080-minute window at 100% to `window`/300 and `weekly`.
4. `protocol-v1.md` documents the four fields; an `error` event without them still decodes in the phone.
5. The phone's card shows each scope's title, the reset as a time and a duration, `credits`' "does not reset", and the
   full message; widget tests, each seen failing first.
6. The gobble-cli issue and the go-llmprovider-sdk issue are open, with More Information's requirements, linked to
   each other and here.
7. The Go and Dart gates pass, and CI passes on the push.

## Pros and Cons of the Options

### A. Text fixes only

* Good, because it is small: D1, D2 and D4 in one package.
* Bad, because the card still cannot say which limit it was, and codex's windows stay dropped.

### B. Text fixes, a limit description on the wire, and a request to gobble-cli (chosen)

* Good, because it uses every source there is now, and has a place for gobble's structured data when it comes.
* Neutral, because it touches the daemon, the wire and the phone.
* Bad, because a plan limit's window stays unknown until other repositories parse more.

### C. Wait for gobble-cli

* Good, because one structured source is simpler than several.
* Bad, because the phone stays wrong for every agent until gobble-cli ships, and codex gains nothing.

## More Information

### Owner questions

*Answered by the owner on 2026-11-09, asked with options:*

* *Q1: "Also open an SDK issue". Not chosen (had been recommended): list the SDK's part only in the gobble-cli
  issue; leave it out. So two issues are opened: one in gobble-cli, one in go-llmprovider-sdk, each linking the
  other.*
* *Q2: "Show it (Recommended)". Not chosen: this repository's copy only.*
* *Q3: "Include it (Recommended)". Not chosen: leave codex for its own record.*

* **Q1. The SDK's part.** Some precision needs go-llmprovider-sdk: an absolute reset time instead of only a relative
  `RetryAfter`, ChatGPT's `plan_type`, Gemini's quota fields, a `RetryAfter` on stream failures. Options: list it in
  the gobble-cli issue as a dependency, for gobble-cli to carry to the SDK; also open a go-llmprovider-sdk issue now;
  leave it out. Recommended: list it in the gobble-cli issue, since gobble-cli is the SDK's consumer here and decides
  what it needs.
* **Q2. The provider's message on the phone.** gobble already sends the SDK's redacted provider message (F5), and D4
  shows it. gobble-cli's 0025-MADR D4 keeps provider text out of gobble's own state reasons in case it carries key
  fragments. Options: show it (it is the SDK-redacted text gobble already sends); show only this repository's copy.
  Recommended: show it.
* **Q3. Codex.** D6 is outside gobble's case. Options: include it (same field, small change); leave codex for its own
  record. Recommended: include it.

### Requirements for gobble-cli (the issue's body)

1. **Structured `ErrorData`** (a new acpext minor tag; every field optional and `omitzero`; `error` kept as today):
   `kind` (gobble's failure-kind set), `provider` (filled), `httpStatus`, `code` (the provider's code),
   `providerMessage` (the SDK's redacted `Message` alone), `retryAfterMs`, `resetAt` (RFC 3339, now plus
   `RetryAfter` when gobble builds the error), `retryable`, `attempts`, and `limit` (D3's scope, from a gobble-owned
   code table; empty when the code does not say). `promptError` reads them from the SDK `*APIError` still in the
   error chain.
2. **The limit table** in gobble-cli, by provider code, never by the retry-after's length.
3. **`_gobble/retry`:** `retryAfterMs`, `serverDirected`, `code`, `httpStatus` on a start; and a message, or the new
   `ErrorData`, when a wait over the cap is refused, which today is silent.
4. **The suspected 401 wording** (F5): a quota code must not read "rejected the API key"; test it first.
5. **Optional:** a `session.notices` error notice carrying the same object under `_meta.gobble`, sent before
   `promptError` returns; and a `resetAt` on gobble's provider-state mark, cleared when it passes.
6. **Dependencies on go-llmprovider-sdk** (Q1): an absolute `ResetAt` kept from `resets_at` and HTTP-date; ChatGPT
   `plan_type`; Gemini `quotaId`, `quotaMetric`, `quotaValue` and the per-day flag; Kilo's `metadata`; `RetryAfter` on
   stream failures.

### Related

* `0073-MADR-goose-prompt-hang-and-debug-pass.md`: `agenterr`'s JSON-RPC `data` reading, which D1 extends.
* `0035-MADR-codex-ui-ux-remediation.md`: D9, codex's rate-limit card, which D6 extends.
* `0138-PLAN-overhaul-provider-surfaces-and-turn-path.md`: F9, structured quota for kilo and grok.
* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md): P3's capture, where F1 was
  found, and the gobble Spec that D7's hook joins.
* gobble-cli: `0005-MADR` and `0005-PLAN` (retry policy), `0025-MADR` (provider state, D4), `0031-MADR` (D4, retry
  causes), `0042-MADR` (`ErrorData`). go-llmprovider-sdk: `0012-MADR` (classification), `0028-MADR` (reset headers),
  `0043-MADR` (ChatGPT plan limits), `0044-MADR` (retry notices).
