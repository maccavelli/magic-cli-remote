# MADR 0043: Model selection — scoped catalogs, a provider step, and an in-session picker

- **Status**: Proposed
- **Date**: 2026-07-28
- **Deciders**: Project Owner
- **Related**: [MADR 0023](./0023-canonical-slash-commands.md) (canonical
  command vocabulary; `/model` resolution and `remote_commands`),
  [MADR 0031](./0031-opencode-catalog-and-metadata-parity.md) (catalog
  truthfulness — this MADR extends its rule to catalog *shape*),
  [MADR 0035](./0035-codex-ui-ux-remediation.md) (`/model` moved to in-place
  `OpSetModel` for codex; D5 here fixes the catalog that decision assumed),
  [MADR 0039](./0039-grok-acp-parity.md) (grok `_meta.modelState` catalog
  cache), [MADR 0036](./0036-protocol-contract-completeness.md) (protocol
  documentation drift guard)
- **Evidence**: live probes run 2026-07-28 against opencode 1.18.7,
  goose 1.44.0, codex-cli 0.145.0 and grok on this host, at `fd35b5c`.
  Raw measurements in §2.
- **Companion plan**:
  [model-selection-implementation-plan.md](./model-selection-implementation-plan.md)

---

## 1. Problem

Choosing a model is the first thing a user does when creating a session and one
of the most common things they do inside one. Both surfaces are bad, and they
are bad in a different way per provider.

**Create session.** One flat, uncached, unscoped picker. For OpenCode it is
literally the entire public model catalog — every model of every provider
models.dev knows about — sorted alphabetically by provider id, so the picker
opens on `302ai`. For goose it is *empty*. For codex it is *empty*. For grok it
is four hardcoded ids, one of which the live agent no longer offers.

**In session.** There is no picker at all. Bare `/model` prints a line of text.
The only way to change model is to already know the id and type it.

The rest of this section is what the code does today; §2 is what the engines
actually offer, measured.

### 1.1 One RPC, no scope

`models.list` takes exactly one field — the *agent* provider id — and returns
one flat `picker.Catalog`:

```go
// internal/protocol/messages.go:213
type ModelsListPayload struct {
    Provider string `json:"provider"`
}
```

There is no way to ask for a subset, no way to ask on behalf of a live session,
and no notion of a **model provider** (anthropic, openai, google) as distinct
from an **agent provider** (grok, opencode, goose, codex). The reply is one
WebSocket frame carrying every option (`internal/ws/server.go:1150-1180`).

### 1.2 OpenCode: the whole internet in a bottom sheet

`ListModelsLive` (`internal/provider/opencode/http.go:240-287`) fetches
`GET /provider` and flattens **every model of every provider** into one option
list, sorted by `(group, id)` — i.e. alphabetically by provider id:

```go
for _, p := range out.All {
    for modelID := range p.Models {
        opts = append(opts, picker.Option{ID: p.ID + "/" + modelID, Label: modelID, Group: p.ID})
    }
}
slices.SortFunc(opts, func(a, b picker.Option) int {
    if a.Group != b.Group { return strings.Compare(a.Group, b.Group) }
    return strings.Compare(a.ID, b.ID)
})
```

Measured on this host (§2.1): **172 providers, 5,788 models, a 4.3 MB engine
fetch taking ~0.9 s, and a 532 KB `models.list_result` frame** — every time the
user taps the model row. Nothing is cached: `AfterBoot` already fetches the same
4.3 MB payload for `contextLimits`, and `ListModels` fetches it again per call.

The engine tells us which providers are actually configured. The decode struct
throws that away — it declares only `Default` and `All`, so the `connected`
array in the same response is discarded.

### 1.3 Goose: no catalog at all

`acphttp.Provider.ListModels` returns `spec.StaticModels`
(`internal/provider/acphttp/provider.go:129-131`), and `goose.newSpec`
(`internal/provider/goose/goose.go:42-51`) never sets that field. So goose's
model catalog is, exactly, the empty list. The picker shows "No catalog
entries. Enter a custom value below."

This is not a goose limitation — see §2.3. Goose publishes a live,
provider-scoped model catalog on the wire, and the daemon already talks to the
mechanism that carries it (`session/set_config_option`,
`acphttp/session.go:181-195`); it just never reads the catalog back.

Goose also routes `/model` to `KindDaemon`
(`internal/provider/goose/commandtable.go:9`), which means relaunch — the model
switch costs the conversation, even though goose can switch in place.

### 1.4 Codex: empty for two stacked reasons, neither of them codex's

`Provider.ListModels` (`internal/provider/codex/provider.go:73-103`) measured
**0 options, `source=static`, 2.13 s**. The premise that codex cannot list
models before a session exists is false — `model/list` is an app-server request
with no thread parameter and it answers fine with no thread open (§2.2). Two
daemon bugs hide that:

1. `fr.sendRequest(ctx, "model/list", nil)` sends no `params`. Codex replies
   `-32600 Invalid request: missing field 'params'`.
2. The decode struct reads `{"models": […]}`; codex returns
   `{"data": […], "nextCursor": …}`. Even with (1) fixed, this decodes to zero
   options.

Every failure mode in that function returns the same
`SingleCatalog(SourceStatic, nil, p.cfg.Model, true)`, so both bugs are
invisible — an empty picker and a working picker are the same code path.

### 1.5 Grok: a static list that disagrees with the agent

Pre-session, grok's catalog is four ids hardcoded in 2026-era source
(`internal/provider/grok/grok.go:20-25`): `grok-code-fast-1`, `grok-4`,
`grok-3`, `grok-3-mini`. After one live session the provider cache fills from
`initialize._meta.modelState` and the catalog becomes 5 merged options with
default `grok-4.5` (§2.4) — a model the static list does not contain, alongside
static ids the agent may no longer accept. Create-session therefore offers a
list that is both incomplete and partly wrong.

### 1.6 In session, `/model` is a print statement

Bare `/model` emits a notice and stops (`internal/session/commands.go:678-685`):

```go
m.emitNotice(id, fmt.Sprintf("Model: %s · usage: /model <name> to switch", label))
```

`/model <name>` works and — for opencode, grok, codex and fake — is in-place
(`KindOp` + `OpSetModel`), so the conversation survives. But the user must
already know the id. On a phone, typing `opencode/deepseek-v4-flash-free`
correctly is the whole problem.

The one place a live catalog does reach the phone is the **Agent settings**
sheet (`chat_screen.dart:574-697`), which renders ACP session config options —
that is goose's `provider` and `model` selects. It is not discoverable from
`/model`, and its select sub-sheet builds

```dart
Column(mainAxisSize: MainAxisSize.min, children: [for (final v in o.values) ListTile(...)])
```

with **no scroll view**. Goose's `provider` option has 71 values (§2.3). The one
working path to goose's real catalog overflows and cannot be scrolled.

### 1.7 The picker widget assumes a small catalog

`option_picker_sheet.dart` re-sorts group headers **alphabetically**
(lines 195-200), discarding whatever order the daemon chose; filters the full
option list inside `setState` on every keystroke (88-97, 242); and resolves each
row by walking the group map from the start (`_buildListItem`, 326-356) — O(groups)
per built row, with 172 groups.

### 1.8 Headroom

`models.list_result` is one frame. The relay caps a message at 1 MiB
(`internal/relay/config.go:52`) and so does the daemon's inbound reader
(`internal/ws/server.go:183`). At 532 KB, OpenCode's catalog is already using
**51% of the frame budget**. A doubling of models.dev breaks model selection
over the relay outright, with no graceful degradation.

---

## 2. Evidence

All figures below were measured on this host on 2026-07-28. Engine versions:
opencode 1.18.7, goose 1.44.0, codex-cli 0.145.0.

### 2.1 OpenCode `GET /provider`

| Measure | Value |
|---|---|
| Response size | 4,344,010 bytes |
| Fetch time (3 runs) | 0.94 s / 0.90 s / 0.80 s |
| Providers (`all`) | 172 |
| Models | 5,788 |
| `connected` | `["google", "opencode", "opencode-go"]` |
| Serialized `models.list_result` | 531,554 bytes |
| First 6 picker rows (current sort) | `302ai` × 6 |

Per-model metadata present in the same payload:

| Field | Coverage |
|---|---|
| `release_date` | **5,788 / 5,788 (100%)** — 5,616 `YYYY-MM-DD`, 172 `YYYY-MM` |
| `status` | `active` 5,589 · `deprecated` 142 · `beta` 55 · `alpha` 2 |
| `name`, `family`, `limit.context`, `cost`, `capabilities` | present on all |

So "newest first" is not a heuristic for OpenCode — it is a field. And 142
models currently in the picker are marked deprecated by the engine.

**A far cheaper endpoint exists.** `GET /config/providers` returns only the
configured providers, with the same per-model schema:

| Measure | `/provider` | `/config/providers` |
|---|---|---|
| Bytes | 4,344,010 | 99,009 |
| Providers | 172 | 3 |
| Models | 5,788 | 113 |

Its `providers[].id` / `name` give the display names the user asked for:
`opencode` → **"OpenCode Zen"**, `opencode-go` → **"OpenCode Go"**. Its
`default` map gives a per-provider default model (`opencode`→`big-pickle`,
`opencode-go`→`qwen3.7-plus`).

> ⚠️ **`/config/providers` includes a plaintext `key` field per provider — the
> user's API key.** Nothing derived from this endpoint may reach a client. The
> daemon must decode into a struct that has no `key` field (so `encoding/json`
> discards it) and a test must assert no catalog option, label or description
> carries it.

### 2.2 Codex `model/list`

Raw stdio probe against `codex app-server --listen stdio://`, no thread open:

| Request | Result |
|---|---|
| `"params": null` (what the daemon sends) | `-32600 Invalid request: missing field 'params'` |
| `"params": {}` | `{"data": [ …7 models… ], "nextCursor": …}` |

Model entries carry `id`, `model`, `displayName`, `description`, `hidden`,
`isDefault`, `supportedReasoningEfforts`, `inputModalities`, `serviceTiers`.
The ids returned: `gpt-5.6-sol` (default), `gpt-5.6-terra`, `gpt-5.6-luna`,
`gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`. One probe run also returned
`codex-auto-review`, an internal entry whose `displayName` duplicates
`GPT-5.6-Luna`; a later run on the same binary did not. The list is therefore
engine state, not a fixed set — which is the argument for reading `hidden` and
`isDefault` rather than hardcoding anything.

Codex reports one model provider only (`"modelProvider": "openai"` on thread
payloads), so codex needs no provider step — just a working list.

### 2.3 Goose ACP session config options

Live probe: start a goose session through the existing provider, read the
`session_config` event, then switch the `provider` option.

| Option | Kind | Current | Values |
|---|---|---|---|
| `provider` | select | `google` | **71** |
| `model` | select | `gemini-3.6-flash` | **18** (scoped to `google`) |
| `mode` | select | `auto` | 4 |
| `thinking_effort` | select | `medium` | 5 |

Switching the provider re-emits `session_config` with a re-scoped model list:

| Action | Result |
|---|---|
| `provider = openai` | `model` becomes **30** values, `gpt-4o` … `gpt-5.6-luna`, current `gpt-4o` |
| `provider = anthropic` | `-32603 Internal error`, **no** follow-up `session_config` (no credentials configured on this host) |

So goose's catalog is real, live and provider-scoped — and switching to a
provider with no credentials fails at switch time with an opaque error the UI
must translate.

Two limits worth stating plainly:

- Goose's model values carry **`id` and `name` only** — no release date, no
  context window, no cost. "Newest first" is **not derivable** for goose.
- The catalog is **session-scoped**: it arrives on `session/new`. There is no
  pre-session HTTP endpoint — `/models`, `/providers`, `/api/models`, `/config`
  and `/openapi.json` all return 404 on `goose serve`, and ACP itself has no
  model concept (acp-go-sdk v0.13.5 contains no model types).

### 2.4 Grok

| State | `ListModels` result |
|---|---|
| Pre-session | 4 static options, `source=static` |
| After one live session | **5 options, `source=merged`, default `grok-4.5`** |

The live catalog reaches the provider cache only through
`initialize._meta.modelState` / `_x.ai/models_update`
(`acpagent.go:145-176`, `HandleModelsUpdate`).

### 2.5 Summary — what each provider can actually do

| | Model providers | Pre-session catalog | Session catalog | Order metadata | In-place switch |
|---|---|---|---|---|---|
| **opencode** | 172 (3 connected) | yes, `/provider` or `/config/providers` | via session's `providerID/modelID` | `release_date`, `status` | yes |
| **goose** | 71 | only via a probe session | yes, ACP config options | none | yes, once wired |
| **codex** | 1 (openai) | yes, `model/list` | same list | `isDefault` only | yes |
| **grok** | 1 (xai) | only via an agent process | yes, `_meta.modelState` | none | yes |

---

## 3. Decision

Twelve decisions. D1–D4 are the protocol and daemon shape; D5–D8 are per-provider
catalogs; D9–D12 are the two UX paths and the widget.

### D1 — `models.list` gains scope, not siblings

Extend the existing request rather than adding RPCs. Three optional fields, all
back-compatible (an old client sending only `provider` gets today's semantics,
minus the size problem D2 fixes):

```go
type ModelsListPayload struct {
    Provider string `json:"provider"`
    // Scope is "models" (default) or "providers": with "providers" the reply
    // enumerates model providers rather than models.
    Scope string `json:"scope,omitempty"`
    // ModelProvider narrows a "models" request to one model provider id
    // (e.g. "anthropic"). Empty means the connected set (D2).
    ModelProvider string `json:"model_provider,omitempty"`
    // SessionID scopes the catalog to a live session: its active model
    // provider, with its current model as the default id.
    SessionID string `json:"session_id,omitempty"`
}
```

The reply stays `models.list_result` with the shared `picker.Catalog` schema, so
the existing Dart model and picker widget are reused for both steps. Two fields
are added to the reply:

```go
ModelProvider string `json:"model_provider,omitempty"` // echo of the scope
Truncated     bool   `json:"truncated,omitempty"`      // D4
```

For `scope: "providers"` each option is a model provider:

| Field | Value |
|---|---|
| `id` | model-provider id (`opencode`, `anthropic`, …) |
| `label` | display name (`OpenCode Zen`, `Anthropic`) |
| `description` | `"60 models"`, plus `"· not configured"` when applicable |
| `group` | `Connected` or `All providers` |
| `meta.connected` | `"true"` / `"false"` |
| `meta.model_count` | count |
| `meta.default_model` | the engine's default for that provider, when known |

**Why not new RPCs.** `agents.list` and `commands.list` are already type
aliases of the same payload (`messages.go:261,291`); a fourth alias would add a
wire type, a handler, a Dart method and a doc section to express "the same
catalog, narrower". The scope field expresses it in one line and keeps the
client's two-step flow on one code path.

### D2 — The daemon never ships an unscoped catalog

`scope: "models"` with no `model_provider` returns the **connected** model
providers' models only. For OpenCode today that is 113 options / ~9 KB instead
of 5,788 / 532 KB — a 58× reduction on the frame that hurts most.

Everything else stays reachable through the provider step (D1) plus the picker's
existing search, which is exactly the flow the request describes: pick the
provider, then its models. A provider that reports no connectivity information
(codex, grok, goose) is treated as fully connected — they have one provider each,
or in goose's case a `provider` option whose `current_value` is the connected one.

### D3 — Newest first, where "newest" is a fact

Model options are ordered:

1. the session's / config's current model, when present, first;
2. then by `release_date` descending, where the source supplies it;
3. then engine order;
4. `status` of `deprecated` sinks to the bottom regardless, tagged
   `meta.status="deprecated"`.

Per provider that resolves to: **opencode** — real dates on 100% of models;
**codex** — `isDefault` first, then engine order (no dates); **goose** and
**grok** — engine order, unreordered.

The daemon must not invent an ordering it cannot justify. Where dates are
absent we keep the engine's order and say nothing about recency — a
name-similarity heuristic (`gpt-5.6` > `gpt-5.5`) would be wrong the first time
a vendor changes its naming, and goose's own lists are inconsistent (its
`google` list is unordered; its `openai` list happens to run oldest→newest).

### D4 — Bound, cache, and admit truncation

**Bound.** Cap any one catalog reply at `maxCatalogOptions = 500` options. At
today's shape nothing hits it (the largest single provider is 60 models); it
exists so a future models.dev cannot silently break the 1 MiB frame. When the
cap bites, `truncated: true` and the client shows "Showing 500 of N — search a
provider to narrow". Silent truncation is not acceptable: a catalog that quietly
drops rows reads as "your model does not exist".

**Cache.** A daemon-side TTL cache per (agent provider, model provider), keyed
also on the engine generation so an engine restart invalidates it. TTL 5 min.
This removes ~0.9 s and 4.3 MB of engine traffic from every picker open.

**Prefer the cheap source.** OpenCode's connected-set catalog comes from
`GET /config/providers` (99 KB); `GET /provider` is fetched only when the user
opens the full provider list or asks for an unconnected provider's models. The
`key` field is never decoded (§2.1 warning).

### D5 — Codex: send `params`, decode `data`

Three-line fix plus the metadata it unlocks:

- send `params: {}` (an empty object, not `nil`);
- decode `{"data": […]}` and follow `nextCursor` while present, bounded to 10
  pages;
- drop entries with `hidden: true`; use `isDefault` for `default_ids` when the
  daemon config names no model;
- carry `description` into `Option.Description` and `defaultReasoningEffort` /
  `inputModalities` into `Option.Meta`.

The pre-session codex picker becomes 7 real models. A live test pins the
`params`/`data` contract so a codex upgrade that renames either fails loudly
instead of silently returning an empty catalog again.

### D6 — Goose: the catalog is its ACP config options

Goose's `provider` and `model` selects **are** the catalog (§2.3). Read them.

**In session.** `models.list {provider:"goose", session_id}` returns the live
session's `model` option values with `current_value` as the default;
`scope:"providers"` returns its `provider` option values. Both come from state
the session already holds — no extra engine calls.

**Pre-session.** No engine endpoint exists, so the daemon harvests the catalog
from a **short-lived probe session**: `session/new` in the host home directory,
read `configOptions`, close. Measured cost on this host: ~2 s, once per TTL
window (D4), and only when a goose catalog is actually requested. Any live goose
session's config options refresh the same cache for free, so in practice the
probe runs at most once.

**Model ids become provider-qualified.** Goose model ids are only meaningful
within a provider (`gpt-4o` exists under `openai`, not `google`), so the id the
picker returns and `session.create` stores is `"<provider>/<model>"`, mirroring
OpenCode. `acphttp.session.createNew` splits it and sets `provider` **then**
`model` (order matters — the model list re-scopes on provider change). An
unqualified id keeps today's behaviour: set `model` only.

**Surface the credential failure.** `provider = anthropic` returns
`-32603 Internal error` with no detail. The daemon translates a config-option
failure on `provider` into: *"goose has no credentials for anthropic — run
`goose configure` on the host."* Guessing is acceptable here because the failure
is otherwise unactionable; the raw error is kept in the debug log.

**`/model` goes in-place.** `internal/provider/goose/commandtable.go` moves
`model` from `KindDaemon` to `KindOp` + `OpSetModel`, backed by a new
`SetModel` on `acphttp.session` that issues `set_config_option`. Goose stops
losing the conversation on a model switch.

### D7 — Grok: prefer the live catalog, keep static as a floor

`acpagent.Provider.ListModels` already prefers its cached live catalog. Add a
bounded cold-start path: when the cache is empty and the binary is present,
spawn one agent process, complete `initialize`, harvest `_meta.modelState`, and
release it — the machinery already exists as the prewarm spare
(`acpagent.Provider.warm`), so this is a claim-and-read, not a new process
model. Bounded to one attempt per TTL window and skipped entirely when a warm
spare is already available.

The static list stays as the last resort but is marked in the reply
(`source: "static"`, which the picker already badges as "Offline catalog"), and
its ids are refreshed to match §2.4.

### D8 — OpenCode: connected first, provider step, real names

`ListModelsLive` is split:

- `ListModelProviders` → `scope:"providers"`. From `/config/providers` for the
  connected set (with display names and per-provider defaults) merged with
  `/provider`'s `all` for the rest, marked `meta.connected=false`. Group
  `Connected` sorts first; within a group, connected-first then by name.
- `ListModelsLive(modelProvider)` → that provider's models, ordered per D3.

The daemon keeps emitting `providerID/modelID` ids, so `session.create`,
`SetModel` and the persisted session meta are unchanged.

### D9 — `/model` opens the picker, client-side, with no new write path

Bare `/model` (no argument) is intercepted **by the client** when — and only
when — `remote_commands` reports `/model` available for this session. The client
then:

1. calls `models.list {provider, session_id}` (D1) — provider-scoped, current
   model pre-selected;
2. shows the existing `showOptionPicker`;
3. on confirm, **submits the text `/model <id>`** through the normal prompt
   path.

Step 3 is the important one: there is no new RPC and no new event. The daemon
executes exactly the command it executes today, so in-place switching, the
relaunch fallback, the notice text, the persisted meta update and the transcript
echo are all unchanged and untouched. The picker is a text-composition aid.

`/model <name>` typed directly still bypasses the picker entirely. When
`remote_commands` says `/model` is unavailable, the client sends the bare text
and the daemon answers as it does now — the "isn't available here" notice.

**Why not a daemon-driven picker event.** A new control event ("agent requests a
picker") would be the right shape if the daemon needed to drive arbitrary
interactions, but it needs to drive exactly one, and every piece of it —
catalog, picker widget, command text — already exists on the client. The event
would add a wire type, a reducer arm, a resolution RPC and a timeout policy to
express something a `models.list` call already expresses.

### D10 — Create session gets a provider step

The single "Select model (optional)" row becomes two:

| Row | Behaviour |
|---|---|
| **Model provider** | Opens the D1 `scope:"providers"` picker. Defaults to blank = provider default. Choosing one resets the model row. Hidden for providers that report one model provider (codex, grok). |
| **Model** | Opens the models picker, scoped to the chosen model provider, or to the connected set when none is chosen (D2). |

Both rows use `showOptionPicker`, whose search box is the "provider fuzzy
search" requirement — it already matches id, label, description and group
(`option_picker_sheet.dart:88-97`). The catalogs are cached in memory for the
lifetime of the dialog, so re-opening a picker costs nothing.

The stored per-provider preferred model (`settings_store.dart:62`) is extended
with the preferred model provider, so the second session with a given agent
opens on the right list.

### D11 — Preserve the daemon's order; make the widget scale

`option_picker_sheet.dart` changes:

1. **Stop re-sorting groups alphabetically** (lines 195-200). The daemon's order
   is now meaningful (D3: connected first, newest first) and the widget must
   present it. First-appearance order becomes the group order.
2. **Flatten once per query**, not per row: build a `List<_Row>` of headers and
   options when the filter changes, so `itemBuilder` is O(1) instead of walking
   the group map (326-356).
3. **Debounce search** by 120 ms and filter over the precomputed lowercase
   haystack, not four `.toLowerCase()` calls per option per keystroke.
4. Render `meta.status`, `meta.context` and `meta.connected` as trailing badges
   so a deprecated or unconfigured row is visible rather than merely ranked.
5. Show `truncated` (D4) as a footer line.

### D12 — Route the Agent-settings select through the same picker

`chat_screen.dart:669-686` replaces its unscrollable `Column` of `ListTile`s
with `showOptionPicker` over a catalog built from the `ConfigOption`'s values.
Goose's 71-value `provider` select becomes searchable and scrollable, and the
sheet inherits every D11 improvement. This is the one-line-per-call reuse that
makes the existing config-options path a first-class model-selection surface
instead of a broken one.

---

## 4. Requirement → decision map

| Requirement (as stated) | Decisions | Notes |
|---|---|---|
| OpenCode should not populate the entire online catalog | D2, D4, D8 | 5,788 → 113 options by default; 532 KB → ~9 KB |
| List default providers, opencode-go and opencode-zen | D8 | The ids are `opencode-go` and `opencode`; display names "OpenCode Go" / "OpenCode Zen" come from the engine. The *default* set is the engine's own `connected` list, which on this host is exactly `google`, `opencode`, `opencode-go` |
| Pick the provider, then its models | D1, D10 | Two-step, one widget, one RPC |
| Provider fuzzy search for non-default providers | D1 (`scope:"providers"`), D10, D11 | Full 172-provider list is reachable and searchable; only the *default* view is narrowed |
| Models ordered latest → oldest | D3 | Real `release_date` for opencode; honest fallback elsewhere (§2.3, §2.5) |
| Goose needs similar functionality | D6 | Its catalog exists and is already provider-scoped; also fixes `/model` losing the conversation |
| Codex can't populate before a session | **D5 — premise corrected** | `model/list` works with no thread; the empty picker is two daemon bugs (§2.2). Codex gets path 1 too |
| `/model` in session shows available models for the active provider | D9 | Scoped by `session_id`; no new write path |
| Two optimised paths (create + in-session) | D9, D10 | Same catalog RPC, same picker widget |

---

## 5. Alternatives considered

| Alternative | Why not |
|---|---|
| Keep one flat catalog, fix only the client (paginate/virtualise in the picker) | The 532 KB frame and the 4.3 MB engine fetch happen before the client sees anything, and both grow with models.dev. It also leaves goose and codex empty, which is most of the complaint |
| New `model_providers.list` RPC | A fourth alias of the same payload for "the same catalog, narrower" (§D1). More wire surface, two client code paths |
| Daemon-driven picker event for `/model` | Adds an event type, a reducer arm, a resolution RPC and a timeout policy to express something `models.list` + existing text submission already expresses (§D9) |
| Sort every provider's models by name-similarity heuristics to fake "latest first" | Wrong the first time a vendor renames; goose's own lists contradict each other (§D3). Better to order by fact where facts exist and say nothing where they do not |
| Ship goose's catalog by parsing `~/.config/goose/config.yaml` | Gives the *current* provider/model only, not the available set. The ACP config options give the real thing (§2.3) |
| Have the phone fetch the full catalog once and cache it on disk | Moves 532 KB onto the device and makes staleness the client's problem. The daemon is the one with the engine and the cache already |
| Drop deprecated models from OpenCode's catalog | 142 models; some are pinned in users' configs. Rank last and badge (D3) rather than deny |

---

## 6. Consequences

**Better.** Model selection stops being the worst screen in the app. OpenCode's
default picker is 58× smaller and opens on providers the user actually has;
codex and goose get a catalog for the first time; grok stops advertising models
its agent will refuse; `/model` becomes a menu on every provider that supports
switching; goose stops losing the conversation on a model switch. The 1 MiB
frame headroom problem goes away for the foreseeable growth of models.dev.

**Worse / new risk.**

- **A probe session for goose** (D6) creates and closes a real goose session
  before any user session exists. It is bounded, cached and lazy, but it is a
  side effect of opening a picker. Mitigated by preferring any live session's
  config options and by the TTL cache.
- **Two-step selection is one more tap** for the user who wanted the provider
  default. Mitigated: the model row is still directly tappable and defaults to
  the connected set, so the provider step is optional, not mandatory.
- **The client now interprets one command** (`/model`, bare) rather than
  forwarding it blindly (D9). It is gated on `remote_commands` availability and
  falls through to the daemon in every other case, but it is a client-side
  behaviour that must stay in sync with MADR 0023's rule that the daemon decides
  availability.
- **Provider-qualified goose ids** (D6) change what `session.create.model`
  means for goose. Unqualified ids keep working, so persisted sessions are
  unaffected.
- **`/config/providers` carries API keys** (§2.1). The mitigation is structural
  (a struct without the field) plus a test, but this is now a place where a
  careless struct change leaks a secret.

**Unchanged.** `session.create`'s `model` field and its semantics; `SetModel`
and the in-place/relaunch fallback; the persisted session meta; the
`picker.Catalog` schema; every other `*.list` RPC.

---

## 7. Explicit non-goals

- Configuring credentials from the phone. Adding an API key or running
  `goose configure` / `opencode auth` stays a host-side operation; the remote
  only *reports* that a provider is unconfigured.
- Multi-select model catalogs (the schema already supports it; nothing needs it).
- Per-model reasoning-effort selection. Codex and goose both expose it
  (`supportedReasoningEfforts`, `thinking_effort`) and it is carried in
  `Option.Meta` for display, but choosing it is a separate surface.
- A phone-side persistent catalog cache. In-memory for the dialog's lifetime
  only (D10).
- Changing how a model is *applied* — `SetModel` and the relaunch fallback are
  out of scope except for goose gaining the in-place path (D6).
- Model selection for the `fake` provider beyond what it already has.

---

## 8. Verification

Each decision has a check that fails before the change:

| Decision | Check |
|---|---|
| D1/D2 | Unit: `models.list` with no `model_provider` on a stubbed 172-provider dialect returns ≤ the connected set; serialized size asserted < 32 KB |
| D3 | Unit: a catalog with mixed `release_date` and one `deprecated` orders current → newest → oldest → deprecated |
| D4 | Unit: two `ListModels` calls inside the TTL issue one engine fetch; an engine generation bump invalidates. Unit: a 600-option source yields 500 options and `truncated: true` |
| D4 (secret) | Unit: a `/config/providers` fixture containing `"key"` produces a catalog whose serialized form does not contain the key string |
| D5 | Live (`live_codex`): `ListModels` with no thread returns ≥ 1 option and `source != static`; a raw probe asserts `params:{}` and the `data` field name |
| D6 | Live (`live_goose`): pre-session `models.list` returns > 0 options; `scope:"providers"` returns > 1; setting a qualified `provider/model` at create yields a session whose `model` config option matches |
| D6 (`/model`) | Unit: goose's command table maps `model` → `KindOp`/`OpSetModel` (mirrors the codex test in `codex/model_test.go`) |
| D7 | Live (`live_grok`): `ListModels` with no prior session returns `source != static` |
| D9 | Widget: bare `/model` with `remote_commands` available opens the picker and, on confirm, submits `/model <id>`; with it unavailable, submits `/model` unchanged |
| D10 | Widget: choosing a model provider resets the model row and the second picker's catalog request carries `model_provider` |
| D11 | Widget: group order matches the catalog order (not alphabetical); a 500-option catalog builds without a frame budget overrun |
| D12 | Widget: a 71-value select scrolls and filters |

Protocol documentation (`docs/protocol-v1.md`) is updated for the new
`models.list` fields — `internal/protocol/doc_coverage_test.go` guards types,
not fields, so this one is on the checklist rather than the compiler.

---

## 9. Implementation record

To be completed as the companion plan lands.

| Phase | Commit | Outcome |
|---|---|---|
| — | — | Not started |
