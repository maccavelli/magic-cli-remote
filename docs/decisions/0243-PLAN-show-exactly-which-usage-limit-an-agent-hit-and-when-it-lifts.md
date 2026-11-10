---
status: in-progress
date: 2026-11-09
associated-madr: "0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement showing exactly which usage limit an agent hit and when it lifts

Associated MADR: [0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md](0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md)

## Goal

When this plan is done:

* gobble's quota error reads as `quota`, with its scope, upstream, code, reset and the provider's own words (MADR D1
  to D4, Confirmation 1);
* an `error` event carries `limit_scope`, `limit_window_minutes`, `limit_provider` and `limit_code` when known (D5);
* codex's two rate-limit windows reach the phone by name (D6);
* the phone's limit card titles the limit by scope and says when it lifts (D8);
* a gobble-cli issue holds the requirements for structured data (D7), and, once a gobble release sends it, the gobble
  Spec reads it (P4).

## Scope

* **Go:** `internal/agenterr/` (D1 to D4); `internal/event/event.go` (D5); `internal/provider/acpagent/` (emitting the
  fields, and the Spec hook of D3); `internal/provider/codex/session.go` (D6); `internal/provider/gobble/` (P4 only).
* **Dart:** `apps/mobile/lib/data/protocol/models.dart`, `lib/data/chat/chat_models.dart` and
  `lib/data/chat/transcript_reducer.dart` (the fields), `lib/features/chat/chat_bubble.dart` (`_LimitNotice`), and
  their tests.
* **Docs:** `docs/guides/protocol-v1.md` (the four fields).
* **Records:** this pair; `docs/README.md`; a note on 0179's PLAN pointing here for its P3 finding.
* **Outside this repository:** one gobble-cli issue and one go-llmprovider-sdk issue (P0 step 3; MADR Q1). Nothing
  in either repository's code or records is written by this plan.
* **Not in scope:** MADR D9's list.

## Conventions

* **C1. Start state.** Each phase starts on a clean `master`, or names what differs.
* **C2. Seen failing first.** Each new test runs against today's code and fails for the reason it exists; planted
  breaks run in a scratch clone, never in the tree.
* **C3. Go gates:** `make pre-add-check FILES=…` on every changed Go file, `make test`, `go test -race ./...`,
  `make lint`, `make staticcheck`, `make vet-tags`, output redirected and the exit status read before any filter.
* **C4. Dart gates** (P3): `dart format --output=none --set-exit-if-changed .` over `apps/mobile`, `flutter analyze`,
  `flutter test`.
* **C5. Staging.** The agent stages; the owner commits with `git commit --no-edit`. The redact preview runs before
  each staging. Nothing is pushed by the agent.
* **C6. Quota.** No phase spends provider quota. Test inputs are captured or recorded strings.
* **C7. Deviations** stop the phase, and are recorded with a date, and in the MADR when a decision or fact changes.

## Implementation Steps

### P0 — The records, and the gobble-cli issue

1. This pair, `proposed`, with the owner's answers to Q1 to Q3 recorded in the MADR when given.
2. `check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview. **Stage.**
3. After the owner accepts the MADR: open the gobble-cli issue with the MADR's "Requirements for gobble-cli" 1 to 5,
   the captured error of F1 as the example, and this record's name; and the go-llmprovider-sdk issue with
   requirement 6 (MADR Q1), each linking the other. Record both numbers here and in the MADR. Each issue's text is
   shown to the owner before it is posted.

### P1 — `agenterr` (MADR D1 to D4)

1. **Seen failing on today's code** (C2): a test with 0179's captured gobble error, asserting Confirmation 1's
   values. It fails: kind `server`, no reset, the fixed copy.
2. **D1:** `jsonDataText` reads an object `data`'s string `error`, `message` or `reason`.
3. **D2:** `parseReset` reads `retry-after <Go duration>` first; table tests for `1h33m21s`, `2h9m59.87321s`, `45s`,
   `250ms`, and the older formats unchanged.
4. **D3:** `Classification.Limit` (`Scope`, `WindowMinutes`, `Provider`, `Code`, `ProviderMessage`); the code table
   and the window phrases; an exported `Limit` type a Spec hook can return. Table tests, including text that states
   no scope and a long retry-after that must not become a window.
5. **D4:** the wrapper removal; tests with gobble's chain, grok's 401 shape and an OpenCode "insufficient balance"
   message, each keeping the provider's own words.
6. Every existing `agenterr` test passes unchanged; planted breaks for D1 to D4, in a scratch clone. C3. **Stage.**

### P2 — The wire, `acpagent` and codex (MADR D5, D6)

1. **`event.Event`** gains `LimitScope`, `LimitWindowMinutes`, `LimitProvider`, `LimitCode` (JSON `limit_scope`,
   `limit_window_minutes`, `limit_provider`, `limit_code`, each `omitempty`).
2. **`acpagent`**: the prompt-error path (`emitClassifiedTurnErrorWith` and the queued-prompt path) copies the
   classification's limit into the event. A Spec hook, `ErrorLimit func(raw string) (agenterr.Limit, bool)`, is
   consulted first, so an agent's structured data wins (D3 source 1). No Spec sets it in this phase.
3. **Codex (D6):** both windows read; the one at 100% sets the scope and window; a weekly window of 10,080 minutes is
   `weekly`; the 90% notice names its window.
4. **`protocol-v1.md`:** the four fields, beside `error_kind` and `retry_at`, with the scope values and the
   degrade-unknown rule.
5. Tests, each seen failing: an event carries the fields; the hook wins over the text; codex's 300-minute and
   10,080-minute windows. C3. **Stage.**

### P3 — The phone's card (MADR D8)

1. The protocol and chat models read the four fields; an event without them decodes as today.
2. `_LimitNotice`: the title by scope; the upstream when known; the reset as a time and how long from now; `credits`'
   "does not reset by itself"; the full provider message.
3. Widget tests for each scope, the reset phrase, `credits`, no scope (today's titles), and an unknown scope value
   (degrades to today's card), each seen failing first. C4. **Stage.**

### P4 — gobble's structured data (MADR D7)

**Gate:** a gobble-cli release whose acpext carries the issue's structured `ErrorData`. Until then this phase waits,
and P5 may close the rest.

1. Bump `github.com/maccavelli/gobble-cli/acpext` to that tag; `go mod tidy`; nothing else enters the module graph.
2. The gobble Spec sets `ErrorLimit`: decode the error's `data` as acpext's `ErrorData` and map `limit`,
   `retryAfterMs`/`resetAt`, `provider`, `code` and `providerMessage` onto `agenterr.Limit`.
3. Tests from acpext's recorded frames for the new fields, seen failing with the hook unset. C3. **Stage.**

### P5 — Close-out

1. The MADR's Confirmation 1 to 7, each with its evidence; CI on the push.
2. A dated note on 0179's PLAN (its P3 finding) pointing here.
3. This plan `complete` (or P4 recorded as waiting on gobble-cli, with the plan left `in-progress`), the MADR
   `accepted`; `check_records.py --write-index`; `make check-records`. **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 gobble's captured error, end to end | P1 | steps 1 to 5 |
| 2 durations, codes, phrases, wrappers, no inference | P1 | steps 3 to 5 |
| 3 codex's windows | P2 | step 3 |
| 4 the protocol fields, and an older event | P2, P3 | P2 step 4; P3 step 1 |
| 5 the card | P3 | step 3 |
| 6 the gobble-cli and go-llmprovider-sdk issues | P0 | step 3 |
| 7 gates and CI | every phase | C3, C4; P5 step 1 |

## Rollout and Rollback

* **Rollout:** additive on the wire; an older phone shows today's card, with the better `error` and `retry_at` from P1.
  Ships with the next daemon and app releases.
* **Rollback:** revert a phase's commit. P1 alone is safe to keep without P2 or P3.
* **Risk:** low to moderate. P1 changes a classifier every agent passes through; its existing tests stay unchanged and
  must pass.

## Execution record

### P0 — 2026-11-09: the records, and the two issues

* This pair, written `proposed`; the owner answered Q1 ("Also open an SDK issue"), Q2 ("Show it") and Q3 ("Include
  it"), recorded in the MADR. `check_records.py --write-index`; `make check-records markdownlint-docs`: exit 0; the
  redact preview: 0 files. Staged, and committed by the owner.
* The owner accepted the MADR and approved this plan: "committed, accept madr and approve plan, and proceed". The
  MADR is `accepted`, this plan `in-progress`.
* **Step 3, the issues.** Both drafts were shown to the owner, who chose "Post both as drafted". Posted on
  2026-11-09, the SDK's first so each could name the other:
  * [maccavelli/go-llmprovider-sdk#13](https://github.com/maccavelli/go-llmprovider-sdk/issues/13), "Keep absolute
    reset times and limit-scope detail on APIError": requirement 6, and the open questions;
  * [maccavelli/gobble-cli#14](https://github.com/maccavelli/gobble-cli/issues/14), "Send structured quota and
    rate-limit errors to ACP clients (provider code, reset time, limit class)": requirements 1 to 5, the policy point,
    and the SDK issue as a dependency. The SDK issue was then edited to link it.

  Neither text carries a hostname, account name or local path.

### P1 — 2026-11-09: `agenterr`

Scripts and logs are in the session scratchpad (`agenterr-probe/`, `p1_0243_planted.py`, `0243-p1-*.log`).

* **Start state (C1).** The owner committed P0; the tree held only this phase's work.
* **Step 1, seen failing on today's code** (C2). `TestGobbleQuotaEndToEnd` (`internal/agenterr/limit_test.go`), with
  0179 P3's captured gobble error, written first against today's fields only. It failed for all three reasons:

  ```text
  limit_test.go:24: kind = "server", want quota (D1: the cause is in data.error)
  limit_test.go:27: reset = 0001-01-01 00:00:00 +0000 UTC, want 2026-07-22 14:33:21 +0000 UTC (D2: a Go-duration retry-after)
  limit_test.go:30: message = "The model provider had a server error. This is usually temporary — try again in a moment, or switch models.", want the provider's own words first (D4)
  ```

* **Steps 2 to 5.**
  * D1: `jsonDataText` reads an object `data`'s string `error`, `message` or `reason`.
  * D2: `parseReset` reads `retry-after <Go duration>` first (`reRetryAfterGo`, `time.ParseDuration`).
  * D3: `internal/agenterr/limit.go`: `Limit` (`Scope`, `WindowMinutes`, `Provider`, `Code`, `ProviderMessage`), the
    scope constants, the code table, the window phrases, and the SDK header parsed for provider, status and code.
    `Classify` fills `Classification.Limit` for quota and rate-limit kinds.
  * D4: the provider's words after the SDK header, or after the agent's wrappers when there is no header; `Present`
    uses them, with the reset suffix when the words give none.
  * Two things D3 implies and did not say, noted in the MADR under D3: a stated window refines `plan`; an explicit
    HTTP 402 is classified `quota`.
* **The tests:** `TestGobbleQuotaEndToEnd` extended with the `Limit` (scope `free_tier`, provider `opencode-zen`, code
  `FreeUsageLimitError`, the provider's two sentences) and the bare data string; `TestRetryAfterGoDuration` (four Go
  durations, two older formats); `TestLimitScopes` (eleven cases: codes, the 5-hour and weekly refinements of
  `plan`, monthly, credits by code and by 402, Claude's 5-hour phrase, Gemini per day, a plain rate limit, and a
  quota with a 5-hour wait and nothing stated, which stays without a scope); `TestProviderWordsWithoutWrappers`
  (after retries, a stream failure, wrappers with no SDK header, no wrapper at all, grok's 401 unchanged). The 26
  existing `agenterr` tests pass unchanged.
* **Seen failing (C2),** `p1_0243_planted.py`, a scratch clone. Unplanted, exit 0. Each break, exit 1:

  | Break | Message |
  | :--- | :--- |
  | D1: `data`'s string members not read | `kind = "server", want quota` |
  | D2: the Go-duration retry-after not read | `parseReset("x HTTP 429 (retry-after 1h33m21s): y") = -2562047h47m16.854775808s from now, want 1h33m21s` |
  | D3: `FreeUsageLimitError` dropped from the table | `want scope "free_tier"` |
  | D3: a stated window does not refine `plan` | `limit = {Scope:plan …}, want scope "window" window 300` |
  | D3: a window inferred for any quota | `limit = {Scope:window …}, want scope "" window 0 code "SomeLimit"` |
  | D3: HTTP 402 not a quota | `limit = {Scope: …} (kind ""), want scope "credits"` |
  | D4: the provider's words not used | `message = "Internal error llm: api status 429: …"` |
  | D4: wrappers not stripped | `wrappers, no SDK header: limit {Scope:weekly … ProviderMessage:}` |

  The first run caught seven: the wrapper break passed, because every case had an SDK header and the stripping runs
  only without one. The "wrappers, no SDK header" case was added, and the second run caught all eight. The clone was
  removed.
* `make pre-add-check` on the three files: clean. The Go gates (C3), each exit 0: `make test` and
  `go test -race -count=1 ./...` (50 `ok` lines, no FAIL, panic or race line), `make lint`, `make staticcheck`,
  `make vet-tags`.
* `make check-records markdownlint-docs` and the redact preview, then **staged** for the owner's commit:
  `internal/agenterr/agenterr.go`, `limit.go`, `limit_test.go`, this pair.

The owner committed P1: "committed, proceed".

### P2 — 2026-11-09: the wire, `acpagent` and codex

Scripts and logs are in the session scratchpad (`p2_0243_planted.py`, `0243-p2-*.log`).

* **Start state (C1).** Clean tree, five commits ahead of `origin/master`, none of them this phase's.
* **Step 1.** `event.Event` gains `LimitScope`, `LimitWindowMinutes`, `LimitProvider`, `LimitCode` (`limit_scope`,
  `limit_window_minutes`, `limit_provider`, `limit_code`, each `omitempty`). The event is serialized to the phone as it
  is, so no other mapping needed them.
* **Step 2.** `acpagent.Spec.ErrorLimit`, bound onto the session by `bindSpec`. **Its signature differs from step 2's:**
  `func(raw string) (limit agenterr.Limit, resetAt time.Time, ok bool)`, a reset time beside the limit, because P4
  must map gobble's `resetAt` and `retryAfterMs` (P4 step 2) and `Limit` has no reset. Both prompt-error paths
  (`emitClassifiedTurnErrorWith` and the queued prompt's) now classify through `session.present`, which applies the
  hook through a new `agenterr.Refine`, and copy the limit onto the event (`withLimit`). `Refine` merges the agent's
  fields over the text's, and rebuilds the message from the provider's words with the reset. A scope makes an
  unclassified or server error a quota (a rate limit for `rate`); an auth error stays auth. No Spec sets the hook yet.
* **Step 3, codex (D6).** `internal/provider/codex/ratelimit.go`: both windows read; the one at 100% (the later reset
  when both are) is the error, with `limit_scope` `window` and its minutes, `weekly` for 10,080 and `daily` for 1,440,
  `limit_provider` `codex`, and the window named in its text ("Codex 5-hour limit reached (100% used)…"); each window
  at 90% or more is a notice naming it. The existing codex rate-limit tests pass unchanged.
* **Step 4.** `docs/guides/protocol-v1.md`: the four fields beside `retry_at`, the scope values, the
  degrade-unknown rule, and that `error` is the provider's words.
* **Step 5, the tests:** `TestErrorEventCarriesTheLimit`, `TestBindSpecCarriesErrorLimit` and `TestErrorLimitHookWins`
  (`internal/provider/acpagent/limit_test.go`); `TestRefineGivesAScopeItsKind` (`internal/agenterr`);
  `TestRateLimitWindowsAreNamed` (`internal/provider/codex/ratelimit_test.go`: the 5-hour window, the weekly window,
  both with the later reset, and the 90% notice).
* **A guard caught the first draft.** `acpagent`'s test first used gobble's captured error, and
  `TestNoProviderIsNamedInAcpagent` failed on it: that package names no vendor, in tests too. It now uses an error of
  the same shape with neutral names (`vendor-a`); gobble's own string stays in `internal/agenterr`'s test.
* **Seen failing (C2),** `p2_0243_planted.py`, a scratch clone. Unplanted, the three packages exit 0. Each break, exit
  1:

  | Break | Message |
  | :--- | :--- |
  | the turn error carries no limit | `event = kind "quota" scope "" provider "" code "" …; want the free-tier limit` |
  | the hook ignored | `event = scope "free_tier" provider "vendor-a" …; want the hook's weekly, vendor, …` |
  | `bindSpec` drops the hook | `scope = "free_tier", want the bound hook's monthly` |
  | `Refine` gives a scope no kind | `Refine("", "weekly") = kind "" scope "weekly", want "quota"` |
  | codex reads the primary window only | `events = [{Type:notice … Approaching codex weekly limit (100% used)…`, not an error |
  | codex: a week is not weekly | `limit = "window"/10080/"codex", want "weekly"/0/codex` |
  | codex: the earlier reset wins | `--- FAIL: TestRateLimitWindowsAreNamed/both,_the_later_reset` |
  | codex: the notice names no window | `… Approaching codex limit (93% used)…`, want one naming the weekly window |

  `TestBindSpecCarriesErrorLimit` was added when planning these breaks: the hook test sets the session's hook
  directly, so a `bindSpec` that dropped it would have passed. The clone was removed.
* `make pre-add-check` on the ten changed Go files: clean.
* **The first gate run failed,** in a guard: `TestBytesTracksStructGrowth` (`internal/event`), `string fields Bytes
  does not count: [LimitScope LimitProvider LimitCode]`. `event.Bytes`, which sizes event retention, must count every
  string field; the three new ones are added to it (`internal/event/retention.go`). The package then passes; pre-add on
  `retention.go`: clean.
* **The Go gates (C3), run again,** each exit 0: `make test` and `go test -race -count=1 ./...` (50 `ok` lines, no
  FAIL, panic or race line), `make lint`, `make staticcheck`, `make vet-tags`.
* `make check-records markdownlint-docs` and the redact preview, then **staged** for the owner's commit: the eleven Go
  files (`internal/agenterr/limit.go`, `limit_test.go`; `internal/event/event.go`, `retention.go`;
  `internal/provider/acpagent/acpagent.go`, `host.go`, `session.go`, `limit_test.go`;
  `internal/provider/codex/session.go`, `ratelimit.go`, `ratelimit_test.go`), `docs/guides/protocol-v1.md`, this
  plan.
