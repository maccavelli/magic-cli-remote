# MADR 0052: Per-model thinking levels, and what the settings panel should hold

<!-- markdownlint-disable MD013 MD060 -->

- **Status**: **Accepted — for implementation.** Open questions resolved
  2026-07-29 (§8). Scope extended twice the same day: first all three §6
  proposals plus pinned working directories, then §6.5 (Enter-key) and §6.6
  (connection & security card). Six settings features in total. Every
  provider claim below was measured against the installed binary; web sources
  are cited only where they add framing the binaries cannot.
- **Date**: 2026-07-29
- **Scope**: model selection (create-session + `/model`), a new app-settings
  default, removal of the hardcoded default provider, and six settings
  features (§6): default session mode, notification granularity, transcript
  storage control, pinned working directories, Enter-key behaviour, and a
  connection & security card. §6.5 and §6.6 were added and accepted
  2026-07-29; all six are covered by the companion plan.
- **Companion plan**:
  [0052-PLAN-thinking-levels-and-settings.md](./0052-PLAN-thinking-levels-and-settings.md)
- **Measured against**: codex **0.145.0**, grok **0.2.114** (`0c78503879`),
  opencode **1.18.7**, goose **1.44.0**, on this host.
- **Related**: [MADR 0044](./0044-MADR-auto-approve-modes.md) (modes as
  provider-advertised state), [MADR 0050](./0050-MADR-grok-cli-surface-drift.md)
  (grok argv contract), [MADR 0051](./0051-MADR-auto-approve-chat-noise.md)
  (`TypeSubagents` — the same advertise-a-set pattern this reuses).

---

## 1. The headline finding: there is no common ladder

The obvious design — a fixed `Low | Medium | High | Max` enum — does not survive
contact with the four providers. Measured:

| Provider | Levels advertised | Default |
|---|---|---|
| codex `gpt-5.6-sol` | low, medium, high, xhigh, max, **ultra** | **low** |
| codex `gpt-5.6-terra` | low, medium, high, xhigh, max, **ultra** | medium |
| codex `gpt-5.6-luna` | low, medium, high, xhigh, max | medium |
| codex `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini` | low, medium, high, xhigh | medium |
| grok `grok-4.5` | low, medium, **high** | **high** |
| opencode (any model) | *none — a boolean only* | n/a |
| goose | none over ACP | n/a |

Three things break a fixed enum at once: **the set differs per model within one
provider** (codex ships 4-, 5- and 6-level models side by side), **the default
differs per model** (sol defaults to `low`, terra to `medium`, grok to `high`),
and **the vocabulary is open** — codex's own schema types `ReasoningEffort` as
`{"type": "string", "minLength": 1}` with the description *"A non-empty
reasoning effort value advertised by the model"*, not an enum.

So the canonical answer is not a ladder we define. It is **the provider
advertises its list; the daemon passes it through; the client renders what it
was given** — precisely the contract already used for session modes
(`event.SessionMode`) and agent config options (`event.ConfigOption`), and most
recently for `TypeSubagents` (MADR 0051 D8). This MADR proposes reusing it a
fourth time rather than inventing a fifth shape.

## 2. What each provider actually exposes

### 2.1 codex — the richest, and settable mid-session

`Model` in codex's own generated schema (`codex app-server
generate-json-schema --experimental`) has **two required fields** for this:

```text
defaultReasoningEffort : ReasoningEffort
supportedReasoningEfforts : [ ReasoningEffortOption { reasoningEffort, description } ]
```

Live `model/list` returns descriptions written for humans, e.g.:

```text
low     Fast responses with lighter reasoning
medium  Balances speed and reasoning depth for everyday tasks
high    Greater reasoning depth for complex problems
xhigh   Extra high reasoning depth for complex problems
max     Maximum reasoning depth for the hardest problems
ultra   Maximum reasoning with automatic task delegation
```

**Setting it:** `turn/start` accepts `effort` — *"Override the reasoning effort
for this turn and subsequent turns."* `thread/start` does **not**. So codex
supports changing effort at any point in a live session, and it sticks until
changed again. This is the only provider where a mid-session `/thinking` command
is possible.

**Gap:** `codex/provider.go:93` parses `defaultReasoningEffort` (surfaced as
`meta["reasoning_effort"]`, currently unused by the phone) and **ignores
`supportedReasoningEfforts` entirely**.

### 2.2 grok — advertises richly, but only settable at launch

`initialize._meta.modelState.availableModels[]._meta` carries, verbatim from a
live probe:

```json
"supportsReasoningEffort": true,
"reasoningEffort": "high",
"reasoningEfforts": [
  {"id":"high","value":"high","label":"High Effort",
   "description":"Highest implementation quality with extensive reasoning","default":true},
  {"id":"medium","value":"medium","label":"Medium Effort","description":"…","default":false},
  {"id":"low","value":"low","label":"Low Effort","description":"Quick, fast implementations","default":false}
]
```

Note grok ships **labels** as well as descriptions, and marks its own default —
which is `high`, not `medium`.

**Setting it:** the `--reasoning-effort <EFFORT>` launch flag only. I probed the
alternatives and both failed: grok advertises **no** `configOptions` on
`session/new`, and there is no reasoning slash command in its 33-command catalog.
`session/set_model` accepts an extra `reasoningEffort` param and returns
`{"model":{"Ok":"grok-4.5"}}` — but it returns exactly that for a `_meta`-wrapped
variant too, i.e. it silently ignores what it does not understand. That is the
grok trap already recorded in the repo's notes: a wrong reply is
indistinguishable from a right one, so a passing probe proves nothing.

**The saving grace:** `acpagent` spawns **one grok process per session**, so
`--reasoning-effort` *is* a per-session control. It just cannot change
mid-session — that would need a session restart.

**Upstream caveat** ([xAI docs](https://docs.x.ai/developers/model-capabilities/text/reasoning)):
at the API level only some models accept `reasoning_effort`; others return
`INVALID_ARGUMENT`. The CLI's per-model `supportsReasoningEffort` flag is the
authority we should trust, since we drive the CLI, not the API.

**Gap:** `acpagent.GrokAvailableModel.Meta` parses `supportsReasoningEffort` and
`reasoningEffort` but **not** `reasoningEfforts`, and `modelsToCatalog`
(`acpagent.go:779`) discards all three — it maps only id, name and group.

### 2.3 opencode — a boolean, and no API lever at all

The catalog reports `capabilities.reasoning: true|false` per model. That is the
whole of it: **no** level list, no default, and the server's OpenAPI document
contains zero occurrences of `reasoningEffort`, `thinking`, `reasoning_effort`
or `textVerbosity`. The 64 hits for `reasoning` are the *output* part type and
the capability flag.

Levels are configured in opencode's **own config file**, passed through to the
AI SDK ([opencode.ai/docs/models](https://opencode.ai/docs/models/)):

```jsonc
// OpenAI-family
"provider": { "openai": { "models": { "gpt-5": { "options": {
  "reasoningEffort": "high", "textVerbosity": "low", "reasoningSummary": "auto" } } } } }
// Anthropic-family
"provider": { "anthropic": { "models": { "claude-sonnet-4-5-…": { "options": {
  "thinking": { "type": "enabled", "budgetTokens": 16000 } } } } } }
```

This is the user's "Default vs Thinking" case: for Anthropic models the control
is a *token budget*, not a rung on a ladder. opencode also supports **variants**
— named presets per model — which is the closest thing to a per-model ladder it
has, but they are user-authored, not advertised.

**Consequence:** opencode cannot be given a per-session thinking level through
its API. Honest options are (a) leave it out, (b) write the daemon's choice into
opencode's config file, which mutates state we do not own and is daemon-wide
anyway. §5 recommends (a).

### 2.4 goose — env vars and a preference, no ACP surface

Strings in the 1.44.0 binary yield `GOOSE_THINKING_EFFORT`,
`CLAUDE_THINKING_TYPE`, `CLAUDE_THINKING_ENABLED`,
`CHATGPT_CODEX_REASONING_EFFORT`, `GOOSE_LOCAL_ENABLE_THINKING` and
`GOOSE_CLI_SHOW_THINKING`, plus a `PreferenceKey` variant `gooseThinkingEffort`
and the error string `unknown thinking effort:`. So the concept exists and is
per-provider-family, set by environment or stored preference.

**But**: goose runs through `acphttp` as **one shared server for all sessions**
(`acphttp/provider.go:433` routes by `sessionId` into `p.sessions`). Env vars are
fixed at server launch, so anything we set is **daemon-wide**, not per session.
Community threads confirm a CLI `--thinking` flag is still an open request
([block/goose#7617](https://github.com/block/goose/issues/7617),
[#5717](https://github.com/aaif-goose/goose/issues/5717)).

### 2.5 Summary

| | advertises a list | per-session | mid-session | daemon support (post A1–A4) |
|---|---|---|---|---|
| codex | **yes** (required schema fields) | yes | **yes** (`turn/start.effort`) | list + effort wired; `/thinking` |
| grok | **yes** (`_meta.reasoningEfforts`) | yes (process per session) | no | list + spawn flag; `/thinking` fixed-at-start |
| opencode | no (boolean only) | no | no | no levels; `/thinking` unavailable |
| goose | no | no (shared server) | no | no levels; `/thinking` unavailable |

**Gap statements in §2.1 / §2.2 above describe the pre-implementation state
at investigation time.** After Track A, the daemon no longer discards those
fields.

## 3. Proposed canonical model

### D1 — Advertise, don't enumerate

Add a per-option capability to the picker catalog rather than a new global enum.
`picker.Option.Meta` already crosses the wire and is already on the phone as
`PickerOption.meta`, so the transport exists. Two new well-known keys beside the
ones in `picker/order.go`:

```go
// MetaThinkingLevels is a comma-separated, ordered list of the thinking
// levels this model accepts, cheapest first ("low,medium,high,xhigh").
// Absent means the model has no selectable thinking level.
MetaThinkingLevels = "thinking_levels"
// MetaThinkingDefault is the level the provider itself would pick.
MetaThinkingDefault = "thinking_default"
```

Labels and descriptions are worth carrying too (both grok and codex supply
them), but `Meta` is `map[string]string`. Rather than encode a list of objects
into a string, D1 proposes promoting this to a typed field on the picker option
when the design is accepted — noted here as an open question (§8 Q1).

### D2 — Ordering is the provider's, and it is cheap-first

Never re-sort. Codex returns cheap→expensive; grok returns
`high, medium, low` — expensive first. The daemon normalises to cheapest-first
so the client can render a consistent slider, but never invents a rung.

### D3 — A global default is an *intent*, resolved per model by name

The requested app setting ("Default thinking level: Low / Med / High") cannot be
a literal value, because `high` is not on every ladder and `ultra` is on almost
none. Resolution rule, in order:

1. **Exact name match** against the model's advertised list. All four
   vocabularies contain `low`, `medium` and `high`, so this succeeds nearly
   always.
2. **No match → use the model's own default**, not a guess.
3. **Model advertises nothing → send nothing.** Silently ignored, exactly as the
   request asks.

**A global default must never select `max`, `ultra` or `xhigh`.** Those tiers
cost real money and latency; restricting the global setting to the three names
that exist everywhere means a background preference can never quietly escalate a
session to the most expensive rung. The exotic tiers stay reachable — but only by
deliberate per-session selection. This is the single most important safety
property of the design and the reason to prefer name-matching over
rank-interpolation, which *would* map "High" onto `ultra` for a 6-rung model.

### D4 — Where it is set, per provider

| Provider | create-session | mid-session `/thinking` |
|---|---|---|
| codex | `turn/start.effort` on the first turn | **yes** — same call |
| grok | `--reasoning-effort` in the spawn argv | no; offer "applies to new sessions" |
| opencode / goose | not offered | not offered |

The client learns which of these is available from the advertised metadata, so
the UI disables rather than lies.

## 4. UI surfaces

**Model picker (create-session and `/model`).** The picker sheet already renders
`meta` badges. A model with `thinking_levels` gains a second, inline row of
segmented chips below the model row — the level list, with the resolved default
preselected. Choosing a model without levels shows no row at all, so the sheet
is unchanged for opencode and goose.

**Mid-session.** For codex, a `/thinking` canonical command (MADR 0023 registry)
plus a chip beside the mode chip. For grok the chip renders read-only with the
launch value and a "new sessions only" hint — honest about the constraint rather
than silently doing nothing.

**Settings.** "Default thinking level — Low / Medium / High / Provider default",
defaulting to **Provider default** so nothing changes until deliberately set.

## 5. Removing the hardcoded default provider

The user is right that it is useless, and the reason is worse than cosmetic.
`McremoteClient.preferredProvider()` (`mcremote_client.dart:1885`) hardcodes:

```dart
for (final id in ['grok', 'opencode', 'goose', 'fake']) { … }
```

**codex is not in the list.** On a host where codex and grok are both ready, the
"Default model" setting silently configures *grok* — and on a codex-only host it
falls through to "first ready in whatever order the daemon listed". The setting
then stores a model under a provider key the user never chose, which is why it
appears to do nothing.

**Recommendation: delete `preferredProvider()` and the provider-scoped default
model with it.** Replace with a per-provider default resolved at the point of
use: the create-session sheet already knows which provider is selected, so the
model default belongs there, keyed by the provider actually chosen. That is a
strictly smaller surface *and* it fixes codex.

## 6. Settings-panel proposals

Ranked by friction removed. All three are grounded in machinery that already
exists.

### 6.1 Default session mode (auto-approve posture)

Every new session starts at the provider's default mode, so a user who works in
`auto` re-arms it by hand on every session — open menu, pick, confirm the
`dangerous` dialog. A per-provider "start new sessions in mode X" setting
removes that entirely.

The safety machinery is already built: `event.SessionMode.Dangerous` exists
specifically so clients can style and confirm (MADR 0044 D1), and the phone
already gates dangerous modes behind a confirm dialog. The setting should
**preserve that confirmation at arming time, once, when the default is set** —
not suppress it per session. Highest value of the three because it removes a
repeated multi-tap ritual, and it composes with the thinking-level default:
both are "what a new session should start as".

### 6.2 Notification granularity

Today notifications are one boolean, `Agent alerts`. But the daemon emits three
very differently urgent things: a **permission/question request** (blocking — the
agent is stopped until answered), a **turn complete** (informational), and an
**error** (actionable). One switch forces a choice between being interrupted by
completions or missing a blocking prompt.

Three toggles under the existing Notifications section. `NotificationCoordinator`
already distinguishes pending asks from turn events, so this is routing that is
already there, not new plumbing.

### 6.3 On-device storage: transcript cache usage and a clear action

The transcript cache is the only unbounded on-device growth in the app and it is
completely invisible: `transcript_cache.dart` keeps an LRU with an entry index,
trimmed by `kMaxTranscriptItems` and an LRU cap, and MADR 0046 fixed orphan
reconciliation in it. A user cannot see how much it holds or clear it without
clearing app data — which also destroys the paired host and token, a far more
destructive act than they intended.

A row showing bytes used plus "Clear cached transcripts" (leaving credentials
alone) is small, and it is the difference between a targeted fix and a factory
reset for anyone short on space.

### 6.4 Pinned working directories

Promoted from runner-up into scope on request, and it is the cheapest of the
four because most of it exists. `SettingsStore` already keeps a **recency**
list — `_kRecentCwds`, newest-first, capped at `kMaxRecentCwds = 5`
(`settings_store.dart:59-63, 200-221`) — and the create-session sheet renders it
as dropdown items (`sessions_screen.dart:715`).

Recency is the wrong lifetime for a working directory. Five entries is a small
budget; a week of touching other repos silently evicts the two you actually
live in, and there is no way to say "keep this one". Pinning is the missing
half:

- pinned entries are a separate, ordered list that recency never evicts;
- the dropdown shows pinned first (with an indicator), then recents, deduped;
- a settings screen manages the list — pin, unpin, reorder, remove — which is
  also the first place a stale path can be deleted at all;
- `addRecentCwd` skips anything already pinned, so a pinned path never
  consumes one of the five recency slots.

Deliberately client-side. The daemon has `providers.*.default_cwd`, but that is
one path per provider for the *host*; this is the user's own shortlist and
belongs with the other per-device preferences.

### 6.5 Enter-key behaviour in the composer

**This is a capability the app has and cannot reach.** The composer
(`chat_screen.dart:2389-2399`) is configured `minLines: 1, maxLines: 5` — it is
built to grow to five lines — but also `textInputAction: TextInputAction.send`
with `onSubmitted: (_) => _send()`. Setting the action explicitly overrides the
newline action a multi-line field would otherwise get, so the soft keyboard's
action key is **Send** and there is no newline key beside it. There is no
shift-enter handling, no expand-composer sheet, and no second multi-line input
path anywhere in the app — grepping the chat feature for `textInputAction` and
`maxLines` returns exactly this one field.

The practical effect: a multi-paragraph prompt can only be *pasted*. Typing one
is not possible, and the five-line growth the field advertises is only ever
reached by pasted text. For an app whose entire purpose is sending instructions
to a coding agent, that is a sharp limit.

A "Send with Enter" toggle:

- **on** (today's behaviour, and the right default on a phone) — Enter sends,
  which is what a one-line "yes, continue" wants;
- **off** — Enter inserts a newline and sending moves to the existing send
  button, which the composer already has.

Cheap: one stored boolean switching `TextInputAction.send`/`.newline` and
whether `onSubmitted` is wired. No layout change — the send button is already
there in both states.

### 6.6 Connection & security card: see the pin, re-pair without a wipe

Pinned-certificate state is enforced on every connection and **invisible after
pairing**. `SettingsStore` holds the fingerprint, TLS mode, client cert/key and
relay route; the settings screen reads exactly two of its ~28 accessors
(`getNotificationsEnabled`, `getHost`). The connect screen shows a fingerprint
during pairing and on a mismatch, and after that the user can never see what
they are pinned to.

Worse is the escape hatch. `clearFingerprint()` and `clearClientIdentity()`
exist in the store and are **called from no UI at all** — verified by grep. The
only user-reachable reset is `clearAll()`, which also removes the host, device
id, relay url/host-id/authority, recent working directories and every preferred
model, then clears the token and client identity
(`settings_store.dart:616-647`). So "my host's certificate rotated and I want to
re-pin" and "forget everything about this host" are the same button. That is a
trapped state, and it is precisely the situation where a user is already
anxious.

A read-mostly card:

- **Host** and connection route (direct or relay, with the relay authority when
  one is in use — all three values are already stored and none is displayed);
- **Certificate**: TLS mode and the pinned SHA-256, formatted the way the
  daemon logs it so the two can be compared by eye against
  `mcremote`'s startup line;
- **Client identity**: present / absent, since a missing one is a specific and
  currently unexplainable failure mode;
- **Re-pair this host** — clears the pin and client identity *only*, keeping
  the host and preferences, then routes to the pairing flow.

This is the one proposal that is as much about trust as convenience: a security
control the user cannot inspect is one they cannot verify, and MADR 0046's
pin-identity work made the pin meaningful enough to be worth showing.

**Runner-ups still out of scope.** A reduce-motion setting — investigated and
rejected: the starfield is a static `CustomPainter` with no animation loop, and
the three genuinely animated surfaces already honour the OS
`MediaQuery.disableAnimations` flag (`chat_bubble.dart:928`,
`widgets.dart:97,313`), so an in-app duplicate would add a second source of
truth for no gain. A "keep screen awake while a turn runs" toggle
(`foreground_service.dart:51` sets `allowWakeLock: false` and nothing else takes
a wakelock) — real, but §6.2's notifications already answer "tell me when it is
done" without holding the screen on.

### 6.7 What the settings screen becomes

For review as a whole, since four additions land in one screen:

```text
Appearance      Theme
                Send with Enter         ← 6.5, off = Enter inserts a newline
Notifications   Agent alerts            (master)
                  Permission requests   ← 6.2, blocking
                  Turn complete         ← 6.2, informational
                  Errors                ← 6.2, NEW capability (never notified today)
Sessions        Default session mode    ← 6.1, per provider
                Default thinking level  ← §4, Provider default | Low | Medium | High
                Working directories     ← 6.4, pin / unpin / reorder / remove
Storage         Cached transcripts      ← 6.3, size + clear
Connection      Host + route            ← 6.6, direct or relay
                Certificate pin         ← 6.6, TLS mode + SHA-256
                Client identity         ← 6.6, present / absent
                Re-pair this host       ← 6.6, clears pin only
Host            Saved host, Version
```

The removed "Default model" row (§5) is not replaced here: it moves to the
create-session sheet where the provider is already known.

## 7. Consequences

**Good.** One pattern for the fourth time instead of a fifth shape; codex gets a
genuinely useful mid-session control; grok gets per-session effort it already
advertises and the daemon currently throws away; the global default cannot
escalate cost; the broken provider default is deleted rather than patched.

**Cost.** Two providers gain a visible capability and two do not, so the UI must
be explicitly capability-gated or it will look broken on opencode and goose.
Grok's "new sessions only" caveat is a real UX wart with no fix short of a
session restart.

**Risk.** The daemon currently discards the very metadata this depends on, so
Phase 1 is parsing work with no visible result — worth sequencing deliberately so
it is not mistaken for a no-op.

## 8. Resolved questions

All four answered 2026-07-29. Recorded as decisions, not options.

### D5 — Typed levels, not packed strings (Q1)

`picker.Option` gains a typed field rather than comma-joined `Meta` values:

```go
// ThinkingLevel is one selectable reasoning/thinking setting for a model, as
// advertised by the provider. Never a daemon-invented ladder: codex ships 4-,
// 5- and 6-rung models side by side and types the value as an open string.
type ThinkingLevel struct {
    ID          string `json:"id"`                    // wire value: "low", "xhigh", …
    Label       string `json:"label,omitempty"`       // grok supplies "High Effort"
    Description string `json:"description,omitempty"` // both supply prose
    Default     bool   `json:"default,omitempty"`     // the provider's own pick
}
```

on `Option` as `ThinkingLevels []ThinkingLevel`, ordered cheapest-first (D2).
The descriptions are what make a six-rung ladder legible and they do not survive
a `map[string]string`.

### D6 — opencode is not wired (Q2)

No config-file writing. It is daemon-wide, it mutates a file the daemon does not
own, and a control that silently applies to every session is worse than no
control. opencode and goose advertise no levels, so their model rows show no
thinking affordance at all.

### D7 — Mid-session changes take effect next turn (Q3)

`turn/start.effort` is per turn, so a change made while a turn is in flight
applies to the following one. The UI says so rather than implying the running
turn changed.

### D8 — The provider-scoped default model is dropped outright (Q4)

No migration, no compatibility shim. The stored `preferred_model_*` /
`preferred_model_provider_*` keys are abandoned in place and the reading code
deleted; whatever they held was keyed by a provider the user never chose (§5),
so preserving it would preserve the bug. Orphaned keys are a few bytes in
`SharedPreferences` and are cleaned up by the §6.3 storage work.

## Sources

- [OpenCode — Models](https://opencode.ai/docs/models/) (per-model `options`,
  `reasoningEffort` / `thinking.budgetTokens`, variants)
- [xAI Docs — Reasoning](https://docs.x.ai/developers/model-capabilities/text/reasoning)
  (`reasoning_effort` values; per-model support)
- [Codex CLI in 2026 — what's new](https://codex.danielvaughan.com/2026/03/27/codex-cli-in-2026-whats-new/)
  and [Model selection](https://codex.danielvaughan.com/2026/03/26/codex-cli-model-selection/)
  (`/model`, `/reasoning`, `model_reasoning_effort` in `config.toml`)
- [block/goose#7617](https://github.com/block/goose/issues/7617),
  [aaif-goose/goose#5717](https://github.com/aaif-goose/goose/issues/5717)
  (thinking toggle / reasoning-effort still open requests)
- Primary: `codex app-server generate-json-schema --experimental` and live
  `model/list`; grok ACP `initialize` / `session/new` probes; opencode
  `/config/providers` and `/doc`; `strings` on the goose 1.44.0 binary.
