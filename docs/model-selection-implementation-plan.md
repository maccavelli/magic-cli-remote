# Model selection: implementation plan

**Status:** Complete — all phases landed 2026-07-28
(record and deviations: [MADR 0043 §9](./0043-model-selection.md))
**Date:** 2026-07-28
**Decision:** [MADR 0043](./0043-model-selection.md)
**Evidence:** live probes of opencode 1.18.7, goose 1.44.0, codex-cli 0.145.0
and grok, run 2026-07-28 at `fd35b5c` (MADR 0043 §2)

## Goal and non-goal

Deliver two model-selection paths that work on every provider:

1. **create session** → model provider (searchable) → models (scoped, newest first)
2. **in session** → bare `/model` → models for the session's active provider

**Non-goals.** No credential configuration from the phone. No multi-select. No
reasoning-effort picker. No change to how a model is *applied* (`SetModel`,
relaunch fallback, persisted meta) except goose gaining the in-place path. No
phone-side persistent catalog cache.

## Dependency order

Phase 1 defines the wire shape every later phase speaks. Phases 2–5 are the four
provider catalogs and are independent of each other — they can land in any order
or in parallel, and each is separately shippable because phase 1 keeps the old
request shape working. Phase 6 is the client and needs phase 1 plus at least one
catalog to be worth testing; phase 7 needs phase 6.

```
Phase 1 (protocol + daemon scaffolding: scope, cache, cap, ordering)
   ├─> Phase 2 (codex)      ─┐
   ├─> Phase 3 (opencode)   ─┤
   ├─> Phase 4 (goose)      ─┼─> Phase 6 (client: create-session + /model + picker)
   └─> Phase 5 (grok)       ─┘         │
                                       └─> Phase 7 (docs + verification sweep)
```

Recommended landing order: **1 → 2 → 3 → 6 → 4 → 5 → 7**. Codex before opencode
because it is a three-line fix that proves the phase-1 plumbing end to end;
client before goose because goose is the largest provider change and benefits
from a working UI to test against.

Commit per phase, do not push between phases (house convention).

---

## Phase 1 — Protocol and daemon scaffolding

**Implements:** D1, D2, D3, D4
**Files:** `internal/protocol/messages.go`, `internal/ws/server.go`,
`internal/provider/provider.go`, `internal/picker/picker.go` (new ordering
helper), `internal/picker/catalogcache.go` (new)

1. **Request/reply fields.** Add `Scope`, `ModelProvider`, `SessionID` to
   `ModelsListPayload`; add `ModelProvider` and `Truncated` to
   `ModelsResultPayload`. Both reply fields are `omitempty`, so
   `agents.list_result` / `commands.list_result` (type aliases) are unchanged on
   the wire.

2. **Provider interfaces.** Add two optional interfaces next to `ModelCatalog`:

   ```go
   // ModelProviderCatalog is implemented by providers whose models are grouped
   // under distinct model providers (opencode, goose). Absent means one
   // implicit provider and no provider step in the client.
   type ModelProviderCatalog interface {
       ListModelProviders(ctx context.Context) (picker.Catalog, error)
       ListModelsFor(ctx context.Context, modelProvider string) (picker.Catalog, error)
   }

   // SessionModelCatalog is implemented by providers that can scope a catalog
   // to one live session (its active model provider, its current model).
   type SessionModelCatalog interface {
       ListSessionModels(ctx context.Context, sess Session, scope string) (picker.Catalog, error)
   }
   ```

   `ModelCatalog.ListModels` keeps its meaning: the default, connected-set
   catalog. A provider implementing neither new interface behaves exactly as
   today.

3. **`handleModelsList` dispatch.** Resolve in this order, falling through on
   each miss:
   - `SessionID` non-empty and the provider implements `SessionModelCatalog` and
     the session is live and `s.sessions.Authorize(sessionID, deviceID, false)`
     passes → session-scoped. A session the device does not own returns
     `session_forbidden` (the existing mapping for `session.ErrForbidden`,
     `server.go:1081`), not a silent fall-through — the session's model is not
     public information. This is the first use of `deviceID` in
     `handleModelsList`, which currently discards it (`server.go:1153`).
   - `Scope == "providers"` → `ListModelProviders`, or a single synthetic option
     when the provider has no `ModelProviderCatalog`.
   - `ModelProvider` non-empty → `ListModelsFor`.
   - otherwise → `ListModels`.

   An unknown `Scope` value is `bad_payload`, not a silent default — a typo must
   not quietly return the wrong list.

4. **Ordering helper** in `internal/picker`:

   ```go
   // OrderModels sorts options newest-first using meta["release_date"] where
   // present, keeping the source order otherwise, pinning current first and
   // sinking meta["status"]=="deprecated" last. Stable: options without dates
   // keep their relative engine order.
   func OrderModels(opts []Option, currentID string) []Option
   ```

   Providers put `release_date` / `status` into `Option.Meta`; only this helper
   interprets them, so "newest first" is defined in exactly one place.

5. **Cap + truncation.** `maxCatalogOptions = 500` applied in the ws handler
   after ordering, setting `Truncated`. Log the drop count at debug.

6. **Catalog cache** (`internal/picker/catalogcache.go`): a small
   `Cache[K comparable]` with a 5-minute TTL and an explicit `Invalidate(gen)`
   keyed on an integer generation, so an engine restart drops everything for
   that provider. Concurrency-safe; single-flight so two simultaneous picker
   opens issue one engine fetch.

**Tests (fail before):**
- `handleModelsList` dispatch table: each of the four routes, plus `bad_payload`
  on an unknown scope and `session_forbidden` on a foreign session id.
- `OrderModels`: mixed dates + deprecated + current → expected order; no-date
  input is unchanged.
- Cache: two calls in-window → one fetch; generation bump → refetch; two
  concurrent calls → one fetch.
- Cap: 600-option input → 500 options and `Truncated == true`.

**Done when:** the four routes are reachable, the old single-field request still
returns today's catalog for every provider, and `go test ./internal/...` passes.

---

## Phase 2 — Codex: make the existing call work

**Implements:** D5
**Files:** `internal/provider/codex/provider.go`,
`internal/provider/codex/model_test.go`, new
`internal/provider/codex/live_model_test.go`

1. `fr.sendRequest(ctx, "model/list", map[string]any{})` — an empty object, not
   `nil`. Codex rejects a missing `params` with `-32600` (MADR 0043 §2.2).
2. Decode `{"data": [...], "nextCursor": ...}`. Follow `nextCursor` while
   non-empty, bounded to 10 pages, accumulating into one option list.
3. Skip `hidden: true`. Map `displayName` → `Label`, `description` →
   `Description`, `defaultReasoningEffort` and `inputModalities` → `Meta`.
   Set `default_ids` from `isDefault` when `cfg.Model` is empty.
4. **Stop swallowing the error.** Today every failure returns the same empty
   static catalog, which is why two bugs went unnoticed for a release. Log at
   `Warn` with the RPC error, and set `Source: picker.SourceStatic` only on a
   genuine fallback so the client badges "Offline catalog" instead of showing an
   empty list that looks live.
5. No `ModelProviderCatalog` — codex has one model provider (`openai`), so the
   client shows no provider step.

**Tests (fail before):**
- Unit against a stub framer that rejects `nil` params exactly as codex does →
  the catalog is non-empty only after the fix.
- Unit: a `{"data":[…]}` fixture including one `hidden` entry and a
  `nextCursor` page → both pages present, hidden absent, default from
  `isDefault`.
- Live (`live_codex`, skipped without the binary): `ListModels` with no thread
  open returns ≥ 1 option and `Source != SourceStatic`. This is the regression
  guard for a codex upgrade renaming `data` or reinstating strict params.

**Done when:** `ListModels` on this host returns the 7 models from MADR 0043
§2.2 with `gpt-5.6-sol` as the default, with no session open.

---

## Phase 3 — OpenCode: connected set, provider step, real names

**Implements:** D2, D8, D4 (cheap source + secret hygiene)
**Files:** `internal/provider/opencode/http.go`,
`internal/provider/httpagent/{provider.go,httpagent.go}`,
`internal/provider/opencode/{catalog_test.go,http_model_test.go}`

1. **New dialect hooks.** Extend `httpagent.ModelLister` with
   `ListModelProvidersLive` and `ListModelsForLive(modelProvider string)`;
   `httpagent.Provider` implements the phase-1 interfaces by delegating, keeping
   its existing static-fallback and merge behaviour.

2. **Connected set from `/config/providers`.** Decode into a struct with
   `id`, `name`, `source`, `models`, and **no `key` field** — the endpoint
   returns the user's API key in plaintext and `encoding/json` must drop it
   because the field does not exist, not because we remembered to clear it.
   Add a comment saying exactly that; it is the kind of struct someone
   "completes" later.

3. **`ListModelProvidersLive`.** Connected providers from
   `/config/providers` (group `Connected`, `meta.connected=true`,
   `meta.default_model` from its `default` map), then every other provider from
   `/provider`'s `all` (group `All providers`, `meta.connected=false`).
   `description` is `"N models"`. Provider `label` is the engine's `name`
   ("OpenCode Zen", "OpenCode Go", "Anthropic").

4. **`ListModelsForLive`.** One provider's models, `Label` from the model's
   `name`, `Description` carrying the context window
   (`"200K context"` from `limit.context`), `Meta` carrying `release_date`,
   `status` and `context`. Ordering is left to `picker.OrderModels` (phase 1).

5. **`ListModels` (default)** returns the connected providers' models only —
   113 options on this host — ordered per D3 and grouped by provider id, with
   connected-provider groups in `connected`-array order.

6. **Cache** both catalogs through the phase-1 cache, keyed on the engine
   generation so a respawn invalidates. `/provider` is fetched **only** for the
   full provider list or an unconnected provider's models; the default path
   touches only the 99 KB endpoint.

7. Keep emitting `providerID/modelID` ids — `session.create`, `SetModel`,
   `contextLimits` and persisted meta are unchanged.

**Tests (fail before):**
- Fixture-based (trimmed real `/config/providers` + `/provider` payloads,
  checked into `testdata/`): default catalog has only connected providers;
  `scope:"providers"` lists connected first; `ListModelsFor("anthropic")`
  returns only anthropic models.
- **Secret test:** the `/config/providers` fixture contains
  `"key":"SENTINEL-NOT-A-REAL-KEY"`; the serialized `models.list_result` must
  not contain `SENTINEL`.
- Size test: serialized default reply < 32 KB on the real-shaped fixture (today:
  531,554 bytes).
- Ordering test: the opencode-go fixture comes back `kimi-k3` (2026-07-16) first
  and the oldest last.

**Done when:** `models.list {provider:"opencode"}` returns ≤ 200 options and
< 32 KB, and `scope:"providers"` returns 172 options with the three connected
ones grouped first.

---

## Phase 4 — Goose: read the catalog it already publishes

**Implements:** D6
**Files:** `internal/provider/acphttp/{provider.go,session.go,spec.go}`,
`internal/provider/goose/{goose.go,commandtable.go,goose_test.go}`, new
`internal/provider/goose/live_model_test.go`

1. **Session-held config options.** `acphttp.session` already receives
   `configOptions` at `session/new` and on updates; retain the latest snapshot
   on the session (it currently only emits them) and expose:

   ```go
   func (s *session) configOption(id string) (event.ConfigOption, bool)
   ```

   Implement `SessionModelCatalog` on `acphttp.Provider`: `scope:"providers"` →
   the `provider` option's values; default scope → the `model` option's values
   with `current_value` as `default_ids`.

2. **Provider-level cache.** Every `session_config` snapshot refreshes a
   provider-scoped catalog cache (the grok `catalogCache` pattern,
   `acpagent.go:145-176`). Any live goose session therefore keeps the
   create-session picker warm for free.

3. **Probe session, lazily.** When the cache is cold and a catalog is requested:
   `session/new` in the host home directory, read `configOptions`, `session/close`
   (or `Close`), cache, return. Bounded by:
   - one probe per TTL window, single-flighted through the phase-1 cache;
   - a 20 s context;
   - skipped entirely when a live session already populated the cache.

   Measured cost on this host: ~2 s. Log it at `Info` — a picker that starts an
   agent session must be visible in the log.

4. **Provider-qualified model ids.** `ListModelsFor(mp)` emits `"<mp>/<model>"`.
   `createNew` (`session.go:181-195`) splits an id containing `/` and sets
   `provider` first, then `model`; an id with no `/` keeps today's single
   `set_config_option`. Order matters — goose re-scopes the model list on
   provider change (MADR 0043 §2.3).

5. **Translate the credential failure.** A `set_config_option` failure on
   `provider` becomes
   `"goose has no credentials for <id> — run \`goose configure\` on the host."`
   The raw JSON-RPC error goes to the debug log. Verified failure mode:
   `provider=anthropic` → `-32603 Internal error` with no detail.

6. **`/model` in place.** Add `SetModel` to `acphttp.session` (issue
   `set_config_option{model}`, and `provider` first for a qualified id), and
   change `internal/provider/goose/commandtable.go` from
   `"model": {Kind: command.KindDaemon}` to
   `{Kind: command.KindOp, Op: command.OpSetModel}`.

**Tests (fail before):**
- Unit: goose's command table maps `model` → `KindOp`/`OpSetModel` (mirrors
  `codex/model_test.go:TestCommandTableRoutesModelToInPlaceOp`).
- Unit against a stub framer: a qualified id issues `provider` then `model` in
  that order; an unqualified id issues only `model`.
- Unit: a `-32603` on the `provider` option produces the credential message.
- Live (`live_goose`): pre-session `models.list` returns > 0 options;
  `scope:"providers"` returns > 1; after selecting `openai/gpt-5.6-sol` at
  create, the session's `model` config option reads `gpt-5.6-sol`.

**Done when:** the create-session picker offers goose's 71 providers and each
one's models, and `/model` on goose keeps the conversation.

---

## Phase 5 — Grok: prefer live, refresh the floor

**Implements:** D7
**Files:** `internal/provider/acpagent/acpagent.go`,
`internal/provider/grok/grok.go`, `internal/provider/grok/grok_test.go`

1. Cold-start harvest: when `catalogHas` is false and `Ready()`, claim or spawn
   one agent process, complete `initialize`, read `_meta.modelState`, populate
   the cache, release. Reuse the prewarm spare (`Provider.warm`) when one is
   available; otherwise spawn one, bounded to a single attempt per TTL window
   and a 20 s context. Failure falls back to static, as today.
2. Refresh `staticModels` to the ids the live agent reports (MADR 0043 §2.4
   measured `grok-4.5` as the current default) and note in the comment that this
   list is a floor for the offline case, not a source of truth.
3. No `ModelProviderCatalog` — one model provider (`xai`).

**Tests (fail before):**
- Live (`live_grok`): `ListModels` on a fresh provider, with no session ever
  started, returns `Source != SourceStatic`.
- Unit: with a stubbed spec whose `ListModels` fails, the static floor is
  returned and marked `SourceStatic`.

**Done when:** the create-session grok picker shows the agent's real models
without the user first starting a session.

---

## Phase 6 — Client: two paths, one widget

**Implements:** D9, D10, D11, D12
**Files:** `apps/mobile/lib/data/ws/mcremote_client.dart`,
`apps/mobile/lib/data/protocol/picker.dart`,
`apps/mobile/lib/features/widgets/option_picker_sheet.dart`,
`apps/mobile/lib/features/sessions/sessions_screen.dart`,
`apps/mobile/lib/features/chat/chat_screen.dart`,
`apps/mobile/lib/data/local/settings_store.dart`

**6a — client RPC.** Extend `listModels` with optional `scope`,
`modelProvider`, `sessionId`; parse `model_provider` and `truncated` into
`PickerCatalog`. One method, four call shapes.

**6b — picker widget** (`option_picker_sheet.dart`):
1. Preserve catalog order for groups — replace the alphabetical `groupKeys` sort
   (lines 195-200) with first-appearance order.
2. Precompute a flat `List<_Row>` (headers + options) whenever the query
   changes; `itemBuilder` indexes it directly, replacing the per-row walk over
   the group map (326-356).
3. Debounce the search field 120 ms; match against a precomputed lowercase
   haystack per option.
4. Trailing badges from `meta`: `status` (`deprecated`), `context`,
   `connected: false` → "not configured".
5. Footer line when `truncated`.

**6c — create session** (`sessions_screen.dart`):
1. Add a "Model provider" `InputDecorator` row above the model row, shown only
   when the first `scope:"providers"` fetch returns more than one option (so
   codex and grok show no extra row).
2. `pickModelProvider()` → `showOptionPicker` over the providers catalog;
   choosing one clears `model` and stores `modelProvider`.
3. `pickModel()` passes `modelProvider` when set; otherwise the connected-set
   default.
4. Cache both catalogs in the dialog's closure — re-opening a picker must not
   refetch.
5. `settings_store.dart`: store the preferred **model provider** alongside the
   preferred model, and seed both when the agent provider is chosen.

**6d — in-session `/model`** (`chat_screen.dart`): in `_send()`, before the
normal path, intercept text that trims to exactly `/model` **and** whose
canonical command is reported available in `remote_commands`. Then
`listModels(provider, sessionId: …)` → `showOptionPicker` → on confirm, submit
`/model <id>` through the unchanged send path; on cancel, do nothing and restore
the composer text. Every other case (`/model gpt-5`, `/model` when unavailable,
no `remote_commands`) falls through untouched.

Because step 3 re-enters the normal send path, a mid-turn `/model` queues like
any other prompt — the picker is local and opens immediately, and the daemon
sees the command when the turn ends. That is the existing behaviour for a typed
`/model <id>`, so the two spellings stay identical.

Include a "Change model provider…" row at the top of the in-session picker for
providers that report more than one, which re-opens the picker scoped to the
chosen provider.

**6e — Agent settings select** (`chat_screen.dart:669-686`): replace the
unscrollable `Column` of `ListTile`s with `showOptionPicker` over a
`PickerCatalog` built from the option's values (`allowCustom: false`,
`defaultIds: [currentValue]`).

**Tests (fail before) — `apps/mobile/test/`:**
- Picker: groups render in catalog order, not alphabetical.
- Picker: a 500-option catalog builds; `truncated` footer shows.
- Picker: a 71-value catalog scrolls (drives the scroll extent > 0 — the current
  `Column` yields 0).
- Chat: bare `/model` with `remote_commands` available opens the picker and
  submits `/model <id>`; with it unavailable, submits `/model` verbatim;
  `/model foo` never opens the picker.
- Sessions: choosing a model provider clears the model and the next catalog
  request carries `model_provider`.
- Sessions: the provider row is hidden when the providers catalog has one option.

**Done when:** `make -C apps/mobile test` (or the repo's Flutter test target)
passes and the four flows work against a live daemon.

---

## Phase 7 — Docs and verification sweep

**Files:** `docs/protocol-v1.md`, `docs/0043-model-selection.md` (§9),
`docs/0023-canonical-slash-commands.md`

1. `protocol-v1.md`: update the `models.list` row in the RPC table and its
   section with the three request fields, the two reply fields, the
   `scope:"providers"` option shape, and the truncation contract. Note the
   1 MiB frame cap as the reason the daemon scopes.
2. `0023-canonical-slash-commands.md`: record that goose's `/model` moves from
   `KindDaemon` (relaunch) to `KindOp`/`OpSetModel` (in place), and that bare
   `/model` is now a client-side picker rather than a notice.
3. Fill MADR 0043 §9 with the commit per phase and the measured outcome table:

   | Measure | Before | After |
   |---|---|---|
   | opencode default catalog options | 5,788 | |
   | opencode default reply bytes | 531,554 | |
   | opencode engine bytes per picker open | 4,344,010 | |
   | codex pre-session options | 0 | |
   | goose pre-session options | 0 | |
   | grok pre-session source | `static` | |
   | in-session `/model` (bare) | notice text | |

4. Run the live suites deliberately: `-tags live_codex`, `live_goose`,
   `live_grok`, and the opencode live model tests. Record versions — these
   contracts are engine-version-specific and the whole point of phase 2 is that
   a silent contract change previously produced an empty picker rather than a
   failure.

---

## Definition of done

- The create-session model row opens on the user's connected providers, not on
  `302ai`, for opencode; on 7 real models for codex; on goose's live providers
  and models; and on grok's real models without a prior session.
- Bare `/model` opens a picker scoped to the session's active model provider on
  every provider whose `remote_commands` reports it available.
- No `models.list_result` exceeds 32 KB on today's catalogs, and none can exceed
  the cap without saying `truncated`.
- No API key can reach a client from `/config/providers`, proven by a test.
- Goose `/model` keeps the conversation.
- `make preflight` and `make race` clean; live suites run and recorded.
