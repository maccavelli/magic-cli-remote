<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# MADR 0079 — Prioritize configured model providers in a single drill-down model picker

| field | value |
| --- | --- |
| status | **Accepted 2026-08-12.** Originally proposed 2026-08-11 and implemented through the associated plan; automated acceptance passed, with daemon-backed Android smoke testing retained as a rollout gate. |
| related | MADR 0043 (model catalogs and the current two-step picker), MADR 0052 (thinking levels), MADR 0074 (provider credentials), `docs/standards/mobile/flutter.md` (predictive back) |
| evidence | Current code and tests: `apps/mobile/lib/features/sessions/sessions_screen.dart`, `apps/mobile/lib/features/widgets/option_picker_sheet.dart`, `apps/mobile/lib/data/protocol/picker.dart`, `apps/mobile/lib/data/protocol/models.dart`, `apps/mobile/lib/features/settings/settings_screen.dart`, `internal/ws/server.go`, `internal/provider/opencode/http.go`, `internal/provider/kilo/catalog_live.go`, `internal/provider/acphttp/catalog.go`, `internal/provider/httpagent/provider.go`, `apps/mobile/test/model_provider_step_test.dart`, `apps/mobile/test/model_picker_test.dart`, and `apps/mobile/test/picker_test.dart` |
| plan | [0079-PLAN-provider-model-drill-down-picker.md](0079-PLAN-provider-model-drill-down-picker.md) |

## Context and Problem Statement

The new-session dialog currently exposes model choice as two fields:

1. **Model provider (optional)** loads `models.list {scope: "providers"}`.
2. **Select model (optional)** loads `models.list`, optionally scoped by the
   selected `model_provider`.

Both fields open separate `showOptionPicker` bottom sheets
(`sessions_screen.dart:481-534`, `:799-867`). For OpenCode and Kilo, the first
sheet contains the connected providers first and then every provider reported
by the engine (`opencode/http.go:425-502`; `kilo/catalog_live.go:274-351`).
MADR 0043 recorded one OpenCode probe with 172 reported providers and three
connected providers. That measurement demonstrates the scale problem, but it
is historical evidence, not a fixed catalog size or a current-host invariant.

The two-field UI misrepresents what the wire can do. `model_provider` exists on
`models.list` only to scope a catalog (`mcremote_client.dart:3232-3252` and
`server.go:2233-2303`). `session.create` receives `model`, but it has no
model-provider field; the dialog passes only `model` and `thinkingLevel`
(`sessions_screen.dart:956-969`). Consequently, choosing a model provider and
then creating without a model does **not** select that provider. It merely
changed which catalog the phone last displayed.

The first sheet also presents a long tail by default. OpenCode and Kilo mark
provider rows with `meta.connected`; their daemon implementations order the
connected band first and the remaining providers alphabetically. Goose cannot
enumerate all configured credentials from ACP, so it marks only the engine's
current provider connected and preserves the other reported providers as the
long tail (`acphttp/catalog.go:77-119`). Codex and Grok have one implicit model
provider synthesized by `handleModelsList` and therefore do not need a provider
step (`server.go:2257-2269`).

The problem is therefore twofold:

* a potentially very large provider catalog is the first view even though the
  connected band is normally the useful subset; and
* a catalog-scoping choice is rendered as if it were independently persisted
  session state.

### Verified constraints

| Fact | Codebase evidence | Design consequence |
| --- | --- | --- |
| Provider catalogs already carry ordered rows and tri-state `meta.connected` | `picker.dart:149-155`; OpenCode/Kilo `providerOption`; Goose `providerCatalogFromOption` | Level 1 can be derived client-side without a protocol change. |
| `meta.model_count` and `meta.default_model` exist for OpenCode/Kilo provider rows | `opencode/http.go:544-567`; Kilo mirror | These details may be shown when present, but are not universal. |
| Goose provider rows carry neither model count nor default model | `acphttp/catalog.go:84-119` | Level-1 subtitles must degrade gracefully; they cannot require those fields. |
| A scoped model reply is available through the existing `model_provider` request field | `server.go:2279-2297` | Drilling into one provider remains client-only. |
| Model IDs are provider-qualified for OpenCode/Kilo and Goose | `opencode/http.go:288-317`; `acphttp/catalog.go:43-74` | An explicit selected model carries the provider identity that `session.create` needs. |
| A provider-only choice is not sent by `session.create` | `sessions_screen.dart:956-969` | The new picker must return an explicit model or no override; it must not invent a provider-only state. |
| Scoped catalog defaults can be overwritten by the daemon-wide configured model | `httpagent/provider.go:180-199`, `:216-222` | The client must not blindly preselect a scoped catalog default that belongs to a different provider. |
| Provider catalogs are cached for five minutes in the daemon and successful replies are cached for the dialog lifetime on the phone | `picker/cache.go:9-14`; `sessions_screen.dart:408-449` | The drill-down can reuse the existing fetches. The phone cache stores completed catalogs, not in-flight futures. |
| `showOptionPicker` already supplies search, ordered groups, badges, custom values, thinking chips, and a visible truncation footer | `option_picker_sheet.dart:183-218`, `:311-458`, `:461-549` | The new widget should extract and reuse these primitives rather than copy them. |
| A `models.list` reply is capped at 500 options and marked truncated | `server.go:2198-2204`, `:2320-2344` | A scoped provider can still be incomplete; the existing footer must remain. |
| Phone-side credential management already exists | provider auth types in `models.dart:391-599`; credential UI in `settings_screen.dart:1190-1334`; provider-auth handlers in `server.go:1812-2180` | MADR 0074 is not future work for this decision. This picker must not claim that credentials are host-only or duplicate the Settings flow. |

### What must not regress

* Every provider reported by the catalog remains reachable and searchable.
* Leaving model selection unset continues to mean the agent/daemon default.
* Codex, Grok, and any other one-provider catalog open directly on models.
* Catalog ordering, deprecated and unconfigured badges, custom values,
  thinking-level chips, and truncation disclosure remain intact.
* Provider-auth secrets remain outside picker catalogs. The OpenCode/Kilo
  connected response deliberately omits the engine's plaintext `key` field
  (`opencode/http.go:273-284` and the Kilo mirror).

## Decision Drivers

* **Truthful state.** The dialog must not persist or display a model-provider
  selection that `session.create` cannot transmit.
* **Focused first view.** The default provider menu should contain the catalog's
  connected band, not the full reported long tail.
* **One decision, one surface.** Provider is navigation for choosing a model,
  not a second session setting.
* **Complete escape path.** All reported providers, search, custom model entry,
  and explicit truncation remain available.
* **Zero protocol churn.** Existing `models.list` scopes and the existing
  qualified model IDs are sufficient.
* **Honest provider differences.** OpenCode/Kilo metadata must not be assumed
  for Goose or implicit-provider agents.
* **Reuse.** The drill-down keeps the current picker behavior and visual
  language.

## Considered Options

* **Option 1 — Keep the two fields and collapse the long tail.** Preserve the
  provider field as a catalog filter, but initially show only connected rows.
* **Option 2 — Keep two fields and show only connected providers.** Remove the
  reported long tail from the default UI.
* **Option 3 — Use one drill-down Model picker.** Treat provider choice as
  in-sheet navigation and return only an explicit model override or no override.
* **Option 4 — Flatten connected models into one list with provider filters.**
  Render all connected models together and filter them with chips.
* **Option 5 — Use cascading Material menus.** Expand provider rows sideways
  into model submenus.

## Decision Outcome

Chosen option: **Option 3 — use one drill-down Model picker**, because it makes
the UI match the protocol: provider selects the catalog to browse, while the
qualified model ID is the value that actually reaches `session.create`. It
also reduces the default provider view without removing the long-tail escape
path or changing the daemon.

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | **One dialog field.** Replace **Model provider (optional)** and **Select model (optional)** with **Model (optional)**. Unset renders `Provider default`; a selection renders the chosen model's display label and qualified ID as space permits. There is no provider-only display state. |
| **D2** | **Level 1 contains connected provider rows.** For a provider catalog with more than one option, show options whose `PickerOption.connected == true`, preserving catalog order. A row always shows its label. It conditionally shows model count and per-provider default only when `meta.model_count` or `meta.default_model` exists; Goose must remain useful without either. |
| **D3** | **The long tail remains reachable in the same sheet.** A final row labelled **Browse all reported providers (N)…** opens the entire catalog returned by `models.list {scope:"providers"}`, with its existing search, ordering, and `not configured` badges. `N` is `catalog.options.length`; the UI must not call this a fixed or universally complete engine catalog because OpenCode/Kilo may return connected-only data when the expensive full endpoint fails. |
| **D4** | **Provider rows navigate; model rows select.** Tapping a provider replaces the sheet content with `models.list {provider, model_provider}` for that row. Selecting a model returns `(modelProvider, model, thinkingLevel)` to the dialog, but `session.create` sends only the explicit qualified `model` and `thinkingLevel`. `modelProvider` is retained only for picker navigation, display, and dialog-lifetime cache lookup. On a multi-provider level, an unqualified custom value is returned as `<selected-provider>/<custom-value>` so its provider is not discarded; the direct, one-provider path keeps its catalog's existing custom-value semantics. |
| **D5** | **No provider-only confirmation.** Clearing the model clears both local `modelProvider` and `model` state and restores the agent/daemon default. Dismissing the sheet preserves the dialog's prior selection. A provider row must not confirm an empty model, because that provider identity would be discarded by `session.create`. |
| **D6** | **Scoped defaults are validated.** On entering level 2, prefer the provider row's `meta.default_model` when it names that provider and exists in the returned catalog. Otherwise use a scoped catalog default only when it belongs to the selected provider (or the catalog uses unqualified IDs). If neither condition holds, open with no selection. This prevents `withConfiguredDefault` from preselecting a daemon-wide model from a different provider. |
| **D7** | **One-provider catalogs skip level 1.** When the reported provider catalog has zero or one option, open the ordinary model list directly. Goose's usual one-connected/many-reported shape still uses level 1 because the all-reported path is meaningful. |
| **D8** | **Provider authentication stays Settings-owned.** The picker may render the existing `meta.connected` distinction, but this MADR adds no credential form, auth-status join, or setup action. The current tree already exposes `configured / missing / error / quota` through `ProviderInfo.auth` and manages credentials in Settings; a future decision may join that state by upstream ID. |
| **D9** | **New model-specific widget, shared picker primitives.** Add a model drill-down entry point under `lib/features/widgets/`. Extract reusable sheet chrome, search/list rows, badges, custom input, thinking chips, and footer behavior from `option_picker_sheet.dart`; do not duplicate them. Preserve the `showOptionPicker` public behavior for its remaining production uses: agent selection, ACP config selects, and the in-session `/model` command. |
| **D10** | **Back and empty states are explicit.** Header back and `PopScope` navigate models → provider menu → dismiss, following `docs/standards/mobile/flutter.md`. Zero connected providers shows **Browse all reported providers (N)…** plus `No configured providers were reported. Set one up in Settings or on the host, or browse all providers.` An empty scoped catalog retains the existing custom-value state. |
| **D11** | **Keep existing cache boundaries, but describe them accurately.** Reuse the dialog's completed-catalog maps and the daemon's TTL/single-flight cache. A plan may cache in-flight futures to prevent a prefetch/tap race, but this decision does not claim the current phone code emits at most one RPC per key. |

### Wireflow

```text
New-session dialog                  Level 1: connected providers
┌──────────────────────────┐        ┌────────────────────────────┐
│ Available provider  [oc] │        │ Model · opencode       ×  │
│ Friendly name            │  tap   │                            │
│ Working directory        │ ─────► │ OpenCode Zen           ›  │
│ Model (optional)         │        │   60 models · default …   │
│   Provider default    ›  │        │ OpenCode Go            ›  │
│ Resume existing          │        │   38 models · default …   │
│ Select agent             │        │ Google                 ›  │
│                          │        │ Browse all reported       │
│       Cancel   Create    │        │ providers (172)…       ›  │
└──────────────────────────┘        └────────────────────────────┘
                                              │
                          tap provider        │ tap browse-all
                                  ▼           ▼
                 ┌──────────────────────┐  ┌──────────────────────┐
                 │ ‹ OpenCode Zen    ×  │  │ ‹ All providers   ×  │
                 │ Search…              │  │ Search…              │
                 │ ● explicit model     │  │ Connected            │
                 │ ○ …                  │  │   OpenCode Zen     ›  │
                 │ [custom value]       │  │ All providers         │
                 │ Clear Cancel Select  │  │   Anthropic        ›  │
                 └──────────────────────┘  └──────────────────────┘
```

The `172` in this illustrative flow is the historical MADR 0043 probe value;
the rendered count is dynamic.

### Consequences

* Good, because the UI now represents the value the create protocol actually
  accepts: an explicit model, not an independently persisted model provider.
* Good, because the first provider view is reduced to the connected band while
  every reported provider remains one labelled action away.
* Good, because the change is mobile-client-only and uses existing catalog
  scopes, qualified IDs, caches, and truncation signaling.
* Good, because the multi-provider dialog drops from seven possible fields to
  six; the provider filter no longer masquerades as session state.
* Neutral, because phone credential management remains in Settings. The picker
  may tell the user where setup lives, but it does not duplicate that workflow.
* Bad, because the picker gains internal navigation and predictive-back state.
* Bad, because a connected provider is not guaranteed healthy: OpenCode/Kilo's
  catalog bit means configured, and Goose's means current; quota and runtime
  auth failures remain possible.
* Bad, because a provider without count/default metadata produces a simpler row
  than OpenCode/Kilo. Inventing those values would be worse than the asymmetry.

### Confirmation

| Decision | Check |
| --- | --- |
| D1, D4, D5 | Widget test: the create dialog has one Model field; choosing a provider alone cannot change the submitted session; choosing a model sends its qualified ID; an unqualified scoped custom value gains the selected provider prefix. |
| D2 | Widget test: level 1 contains only `connected == true` rows in input order and tolerates absent count/default metadata. |
| D3 | Widget test: the browse-all row uses the returned option count and opens the full returned catalog with search and unconfigured badges. |
| D6 | Unit/widget tests: a cross-provider `defaultIds` value is not preselected; a matching `meta.default_model` is preselected. |
| D7 | Widget tests: a one-option provider catalog opens models directly; a one-connected/many-reported Goose-shaped catalog shows level 1. |
| D8 | Review: no new credential collection or auth RPC is introduced by the picker. |
| D9 | Existing picker, thinking-picker, ACP config, and `/model` tests continue to pass after primitive extraction. |
| D10 | Widget tests: system back moves level 2 → level 1 before dismissal; zero-connected and empty-model states render the specified guidance. |
| D11 | Widget test: reopening a completed catalog uses the dialog cache; an optional test pins in-flight deduplication if the plan implements it. |

Implementation verification uses `make preflight`, which covers Dart format,
Flutter analysis, and Flutter tests. No new live-tagged test is required because
this decision introduces no new engine behavior; the existing MADR 0043 live
tests already pin catalog availability and scoping.

## Pros and Cons of the Options

### Option 1 — Keep the two fields and collapse the long tail

* Good, because it is the smallest UI change.
* Bad, because it leaves a provider-only state that the create wire discards.
* Bad, because users still move between two sheets to complete one model choice.

### Option 2 — Keep two fields and show only connected providers

* Good, because it removes the largest list from the default path.
* Bad, because it removes the only discoverable path to a reported but
  unconfigured provider unless another escape hatch is added.
* Bad, because it preserves the false implication that provider selection is
  independently submitted.

### Option 3 — Use one drill-down Model picker

* Good, because provider navigation and model submission match the existing
  `models.list` and `session.create` responsibilities.
* Good, because it preserves the long tail, scoped full-model lists, custom
  entry, thinking levels, and truncation signaling.
* Bad, because it requires a model-specific result type and in-sheet navigation.

### Option 4 — Flatten connected models with provider filters

* Good, because models and provider filters are visible together.
* Bad, because OpenCode and Kilo cap their default connected-set catalogs at
  200 and 150 options respectively (`opencode/http.go:348-353`;
  `kilo/catalog_live.go:192-202`). A chip-filtered flat list could therefore be
  incomplete for a provider even though the scoped fetch has more models.
* Bad, because the reported provider long tail still needs a separate discovery
  mechanism.

### Option 5 — Use cascading Material menus

* Good, because it uses a stock desktop menu pattern.
* Bad, because a phone-width cascading submenu cannot accommodate searchable
  model catalogs, custom input, thinking chips, or hundreds of rows.

## More Information

### Auth implementation drift

MADR 0074 still states that implementation had not started at its recorded
revision. Current code is authoritative for this assessment: the tree now has
the `provider_auth` capability, auth state on `ProviderInfo`, credential write
and clear RPCs, upstream switching, provider-auth Settings UI, and device-flow
transport/sheet code. MADR 0079 therefore treats provider auth as an existing,
separate settings surface rather than future work that will land inside this
picker.

### Historical catalog measurements

MADR 0043 records the probes that motivated the current scoped-catalog design,
including OpenCode's 172 reported / three connected provider shape and Goose's
71 reported / one known-connected shape. Those values are useful test fixtures
and scale evidence. They must not be hard-coded into the new UI or described as
current facts without a new live probe.

### Sources

* `docs/0043-MADR-model-selection.md`
* `docs/0052-MADR-thinking-levels-and-settings.md`
* `docs/0074-MADR-remote-provider-auth-from-phone.md`
* `docs/standards/mobile/flutter.md`
