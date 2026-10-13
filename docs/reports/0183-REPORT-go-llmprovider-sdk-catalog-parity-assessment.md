# go-llmprovider-sdk catalog API parity assessment for magic-cli-remote

**Out of scope for Magic (2026-10-13, owner decision).** Magic does not migrate
its model catalogs to `go-llmprovider-sdk`:
[0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](../decisions/0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md).
"What this means for Magic's adapters" below describes no planned Magic work.

Observed 2026-10-11; extended 2026-10-11 with
[Catalog caching is the consumer's responsibility](#catalog-caching-is-the-consumers-responsibility-2026-10-11-owner-decision)
(owner decision). This report evaluates the `go-llmprovider-sdk` provider-catalog
and model-listing API surfaces against the catalog behavior `magic-cli-remote`
provides today, and records what an API extension in the SDK would need to
support. It decides nothing: the API decision and its implementation plan belong
to a future MADR/PLAN pair in the `go-llmprovider-sdk` repository. Repository
names are stated in cross-references; a relative link cannot reach another
repository, so SDK paths are given as repo-relative paths of
`go-llmprovider-sdk` and Magic paths as repo-relative paths of this repository.

## Executive assessment

The requirement is threefold:

1. **A consumer must be able to set the cap** on how many models a provider
   presents. Today that cap is `MaxListed = 6`, a package constant with no
   option surface (`go-llmprovider-sdk` `llmprovider/catalog/models_catalog.go:27`).
2. **A consumer must be able to get full live model catalogs**, the way
   `magic-cli-remote` presents every model an engine reports per provider
   (`internal/provider/opencode/http.go:570`, `ListModelsForLive`), not only a
   curated six.
3. **Calling the API with no arguments keeps the default of 6.**

The SDK is closer than the constant suggests. The six-model cap applies only to
`Catalog.Recommended`; `Catalog.Usable` already returns **every** id the
provider's usability filters admit, uncapped, from the live listing
(`go-llmprovider-sdk` `llmprovider/catalog/discovery.go:59-77`). Full-catalog
reading therefore needs no new fetch path — the network work, pagination,
timeouts, degrade-to-static contract and per-provider normalizers all exist.
What is missing is (a) a configurable cap plumbed through the three cap sites
and the wizard, and (b) per-model metadata exposure so a consumer can build
`magic-cli-remote`-style pickers (label, release date, context window,
thinking levels) without re-deriving it. The SDK already fetches the
models.dev metadata document used for ranking
(`llmprovider/catalog/model_metadata.go`) but discards everything but the
rank.

The recommended direction, recorded here as a proposal and not a decision:

- a scope-neutral **`WithMaxListed(n)`** option on `catalog.List`, default 6
  when absent including for `n <= 0`, applied as the cap of `Recommended` only;
- declared parity between `Catalog.Usable` and Magic's full per-provider live
  catalog, with `Recommended` mapping to `DefaultIDs` and `Live`/`Err` mapping
  to the picker's `Source`/`Truncated` semantics;
- a second, separately-decidable extension that exposes per-model metadata
  alongside `Usable`, which is what lets Magic's `OrderModels`
  (newest-first date ordering) keep working once the SDK is the catalog source.

Item three (the 6 default) is satisfied by definition if the option's zero
value means "default". That is the recommended semantics and is the only
 behavior preserving the existing wizard UX, which depends on the untouched
default (`go-llmprovider-sdk` `wizard/configure.go:481-511`).

## How Magic's catalog/provider works today

Magic's catalogs are engine-dialect adapters feeding one shared picker payload
type, `internal/picker/picker.go`:

- **`picker.Catalog`** (lines 120-143) carries `Kind`, `Source`
  (`live` / `static` / `merged`), the full ordered `Options` list,
  `DefaultIDs`, `AllowCustom`, selection bounds, and `Truncated`.
- **`picker.Option`** (lines 37-72) is a rich row: `ID`, `Label`,
  `Description`, `Group`, `Enabled`, `Meta` (free-string metadata), and typed
  capability fields (`ThinkingLevels`, `Inputs`, `Attachment`, `ToolCall`,
  `Reasoning`).
- **`picker.OrderModels`** (`internal/picker/order.go:58`) is the single sort:
  current selection first, engine-recommended index next
  (`MetaRecommendedIndex`), then `MetaReleaseDate` **descending** (newest
  first), then source order, with `MetaStatus: deprecated` last. This is the
  ordering the user sees in Magic's provider/model pickers.

Provider implementations present **all** models of one engine:

- `internal/provider/opencode/http.go:428` (`ListModelsLive`) lists the
  connected set, capped at `maxDefaultCatalogModels = 200` only for the
  aggregate default reply (guard against the 1 MiB relay frame; MADR 0043).
- `internal/provider/opencode/http.go:571` (`ListModelsForLive`) returns one
  model provider's models **uncapped** — on the measured runtime this is
  thousands of rows across 172 providers; one provider can be hundreds.
- `internal/provider/kilo/catalog.go` does the same against Kilo's
  OpenRouter-shaped catalog, each provider's models in full, newest first.
- The `models.list` RPC (`internal/ws/server.go:2763-2860`) forwards the full
  per-provider catalog over the wire under `maxCatalogOptions = 500` rows per
  reply frame and requires a `Truncated` admission whenever a layer drops
  rows (MADR 0043 D4, 0096 D3).

The essential shape: **one RPC surface, complete per-provider model lists with
display metadata, one shared sort, and an honest truncation flag**. Static
curated lists appear only as fallbacks (`picker.SourceStatic`, merged catalogs)
when the engine is unreachable.

Magic's `go.mod` does not currently depend on `go-llmprovider-sdk`; making the
SDK the catalog source is greenfield on Magic's side, so the SDK's API is free
to change without a Magic-side migration cost beyond the adapters.

## How go-llmprovider-sdk's catalog works today

### The API surface

`catalog.List(ctx, provider, src, opts...)` (`go-llmprovider-sdk`
`llmprovider/catalog/discovery.go:93`) is the entry point for every built-in
provider (Gemini, OpenAI, Claude, Grok, OpenCode Zen/Go, Hugging Face, Kilo,
Together, Ollama). It returns:

```text
Catalog{
    Recommended []string  // curated for menus, at most MaxListed (6)
    Usable      []string  // the provider's usable ids, uncapped
    Live        bool      // false ⇒ Usable/Recommended came from the static fallback
    Err         error     // the listing failure when Live is false
}
```

`discovery.go:79-92` documents the contract: live listing curated against a
static catalog, a failed listing **degrades to the static catalog with a nil
error** (`Catalog.Err` carries the classified failure), 10 s bound, pagination
for Gemini/Anthropic (10 pages × 1000), body size limits, and a one-shot
credential renewal on refusal.

Options (`llmprovider/catalog/config.go:86-110`): `WithProfile`,
`WithKiloOrganization`, plus llmprovider's common `WithHTTPClient`,
`WithBaseURL`, `WithClientInfo`, `WithModelMetadataURL` et al. All cap and
curation figures are constants.

### Where the six-model cap lives

Four enforcement sites, all keyed on one constant
(`models_catalog.go:27`):

| Site | File | Lines |
| :--- | :--- | :--- |
| menu curation + backfill | `llmprovider/catalog/models_catalog.go` (`curateFromCatalog`) | 429-471 |
| metadata-ranked recommendation | `llmprovider/catalog/model_ranking.go` (`rankRecommended`) | 227-247 |
| Ollama installed list | `llmprovider/catalog/discovery.go` (`ollamaCatalog`) | 429 |
| wizard menu building | `wizard/configure.go` reads `catalog.MaxListed` directly | 483, 511 |

Tests pin the invariant in both directions: static catalogs must not exceed
`MaxListed` (`models_catalog_test.go:425`, `kilo_catalog_test.go:68`,
`huggingface_catalog_test.go:25`) and nothing returned must exceed it
(`discovery_test.go:258`, `discovery_catalog_test.go:153`). Any configurable
cap must keep the no-argument default at 6 for these tests to survive
unchanged.

### What full-catalog support already exists

- `Catalog.Usable` is explicitly **uncapped** and de-duplicated
  (`discovery.go:64-69`, `uniqueIDs`): the live listing as the lister admits
  it — for most providers in the listing's own order, cheapest-first for Kilo,
  fastest-first for Hugging Face. This is a full *usable* catalog, not a
  curated slice.
- `catalog.Static(provider)` (`models_catalog.go:244`) returns the curated
  fallback list — the `SourceStatic` arm of Magic's merged catalog.
- `Catalog.Live` / `Catalog.Err` encode which arm won, mapping directly onto
  `picker.Source` (`live` / `static`).
- Ranking (`catalog.Rank`, `models_catalog.go:219`) ranks by *preference*, not
  by recency; the metadata document that feeds it
  (`model_metadata.go`, models.dev) knows release dates, context limits,
  costs, reasoning and family — but the API discards per-model facts and
  returns only the ranked order.

### The gap in one sentence

The SDK answers "give me a short menu and the searchable inventory"; Magic
needs "give me every model with the facts to label and sort it, and let me
choose how long the menu is". The inventory fetch already exists; the cap is a
constant the consumer cannot move; the metadata is fetched and dropped.

## What an API extension must provide

### 1. Configurable per-provider maximum (`WithMaxListed`)

Requirement: consumer sets the cap; default stays 6 with no arguments.

Recommended shape, matching the existing option idiom
(`catalog.WithProfile`'s scope mechanism, `config.go:67-69`):

```go
// catalog.WithMaxListed caps configure-time menus at n recommended ids.
// n <= 0 keeps the default of MaxListed. Scoped to every built-in provider,
// like WithProfile; per-provider overrides can be achieved with a
// scoped option against one provider id.
func WithMaxListed(n int) llmprovider.Option
```

Implementation notes, pointed at the four cap sites:

- `config` gains `MaxListed int` (0 = unset); `configFor` maps the option.
  A package-level helper resolves `cfg.MaxListed` to `MaxListed` when unset —
  the three library cap sites call it instead of the constant.
- `curateFromCatalog` and `rankRecommended` take the resolved cap as a
  parameter (or the call sites pass it from `cfg`); neither changes behavior
  when the cap is 6.
- `ollamaCatalog` uses the same resolved value.
- `wizard/configure.go` keeps reading the constant; the wizard UX is
  deliberately the default and must not change without its own decision.
- The static-catalog-≤-MaxListed tests stay: the fallback inventory is
  curated and small **by design**. A consumer asking for a 20-row menu gets
  the static six only on degrade; with a live listing, `curateFromCatalog`'s
  backfill fills from `Usable` up to the requested cap. That degradation
  asymmetry (live ⇒ full menu, static ⇒ short menu) is what
  `Catalog.Err`/`Live` lets the consumer surface honestly.

`n <= 0` semantics are the decision point. Recommended: `n == 0` means unset
(default 6); a negative means unset too, rather than an error, so a
caller-supplied `if x != ""` plumbing never poisons a call; refusing the
option outright (the `ErrInvalidRequest` idiom) is the alternative and is
arguably more honest, but breaks the "no-args defaults" symmetry for option
lists built conditionally. This is a decision for the SDK's MADR, not this
report.

### 2. Full live catalogs

`Catalog.Usable` is the answer and should be declared as the surface Magic's
adapter binds — no new endpoint. The parity mapping for Magic's adapters:

| Magic concept | SDK source |
| :--- | :--- |
| `picker.Options` (all rows) | `Catalog.Usable` when `Live` |
| `SourceStatic` fallback rows | `catalog.Static(provider)` (+ `Catalog.Recommended` on degrade) |
| `picker.Source` = `live`/`merged`/`static` | `Catalog.Live`, `Catalog.Err` |
| `DefaultIDs` | `Catalog.Recommended` (first hit present in `Usable`) |
| row ordering | consumer-side, from metadata (below) |

One honest caveat a decision must weigh: `Usable` is post-filter. It excludes
what the provider's own usability normalizers reject — embeddings, TTS,
image-gen, dated snapshots, Kilo's training-on-prompts policy, Kilo's
`kilo-auto/*` tiers under the utility profile. Magic's engine dialects
present what the engine reports, similarly post-navigation-of-its-own-shape;
this is comparable, not identical (e.g. Gemini's `Usable` strictness versus
OpenCode showing everything). If a decision requires a raw dump beyond
`Usable`, that is a new API (`ListRaw`-style) with its own deny lists; this
report recommends declaring `Usable` as the full catalog and
documenting the filters, because the filters are what make the result
*generateable* — a picker row that cannot answer a request is the exact
problem the SDK was built to avoid.

### 3. Model metadata for picker rows (second, separable extension)

Magic's pickers sort by release date, label with names/context/cost, gate
attachments on modalities, and offer thinking rungs. To reproduce that from
the SDK alone, the consumer needs per-model facts. The SDK already downloads
the right document (models.dev) per listing and ranks with it
(`model_metadata.go`); the recommended extension exposes what it already has:

```go
// catalog.Model is one usable model's metadata, as the ranking document
// reports it. Fields for an id the document does not cover are zero.
type Model struct {
    ID          string
    Name        string
    Family      string
    ReleaseDate string // "YYYY-MM-DD" or "YYYY-MM"
    Status      string // e.g. "deprecated"
    Context     int
    Input, Output float64 // cost per Mtok, 0 when unknown
    Reasoning   *bool
}

type Catalog struct {
    Recommended []string
    Usable      []string
    // Details is one-to-one with Usable when populated. Only Live listings
    // with an available metadata document populate it.
    Details []Model
    Live    bool
    Err     error
}
```

(Opt-in or always-on, and whether fields map onto Magic's
`picker.Option.Meta` keys — `release_date`, `status`, `context` — are
decision points for the SDK MADR. Magic's `MetaRecommendedIndex` maps to
`Recommended`'s position whenever the id appears in it.)

Without this, Magic keeps its own metadata path for rendering and uses the
SDK only for ids; the ids are correct but the picker fidelity regresses to
bare id strings. With it, the SDK can be the sole catalog source and
`order.go`'s
`MetaReleaseDate` path keeps working unchanged.

### What this means for Magic's adapters

Once the SDK ships the above, Magic's engine-dialect model catalogs (which
today read each engine's HTTP endpoints) can be backed by `catalog.List` per
provider, translating `Catalog` → `picker.Catalog`:

- `Usable` → `Options` (ids), filled out with `Details`;
- `Recommended` → `DefaultIDs` and `MetaRecommendedIndex` by position;
- `Live`/`Err` → `Source`, with `Truncated` set by Magic's own transport caps
  (`maxCatalogOptions`), which remain Magic's responsibility;
- `OrderModels` unchanged.

The providers that move first should be the direct-API ones the SDK already
lists (Gemini, OpenAI, Claude, Grok, Kilo); the engine-dialect providers
OpenCode-HTTP/codex remain engine-backed in the first phase, because their
surfaces are larger than the SDK's lib surface and their catalogs do not
degrade to a curated static fallback.

## Catalog caching is the consumer's responsibility (2026-10-11, owner decision)

The question arose whether the SDK should persist and cache large catalogs
itself (buntdb, TTL, pull-down-only-on-change). Evidence against, from both
trees:

- Magic already has the cache: `internal/picker/cache.go` is a TTL +
  single-flight, in-process cache (`DefaultCatalogTTL = 5 minutes`) whose
  invalidation story is precisely the minutes-scale event that catalog caches
  are exposed to — credentials edits and engine restarts bump a generation
  counter and drop every entry; a failed fetch is deliberately not cached.
- The SDK is an in-process library with no place to own a disk cache, and a
  refetch is cheap and bounded (10 s, page and size limited,
  `discovery.go:56`); its static curated fallback already fills the
  offline-answer role, so a persistent cache would add a third source of
  truth (stale-live) to a clean two-arm design.
- Conditional fetching has almost no leverage: provider model-list endpoints
  are authenticated plain GETs with no uniform ETag support. Only the
  models.dev metadata document is conditionally fetchable, and it is
  metadata, not catalogs.

**Decision (owner, 2026-10-11): caching stays with the consumer.** Magic's
adapter keeps `picker.Cache`; the SDK ships no cache layer, and no persistence
layer is added to the SDK for catalogs. `WithMaxListed` and the `Details`
metadata extension above, if pursued, should state in their MADR that the
SDK's `catalog.List` remains a no-cache, fetch-per-call API.

## Risks and decision points

- **Zero-value semantics for `WithMaxListed`** (unset versus refused —
  recommended: unset), recorded above; must be a MADR decision in the SDK.
- **Filter strictness of `Usable` versus Magic's engine catalogs** — Magic
  shows rows the engine reports even when unusable by the SDK's standards.
  Accepting the SDK's filters is a behavior change for OpenCode users viewing
  Gemini/Kilo entries. The decision should name this explicitly.
- **Wizard invariant**: the wizard keeps the 6 default regardless of the
  option; tests asserting menus of exactly `MaxListed` entries
  (`wizard/lister_test.go:89`) run without the option and stay valid.
- **Metadata document reachability**: `Details` is best-effort
  (`WithModelMetadataURL`, `WithoutModelMetadata` already exist); a picker
  must be renderable with `Details` absent, exactly as Magic's undated
  providers degrade to source order today.
- **The 10 s listing timeout** and 8 MiB page limit remain; a catalog as
  large as OpenCode's (measured 4.3 MB full dump) would be at the edge of the
  decode limit — one more reason the first phase backs only direct-API
  providers with the SDK.

## Verification

This report's claims were checked by reading, not by running the SDK:

- cap sites and option surface: `go-llmprovider-sdk`
  `llmprovider/catalog/{models_catalog.go,model_ranking.go,config.go,discovery.go}`
  and `wizard/configure.go`, as cited in the tables above;
- `Catalog` contract text: `discovery.go:58-92`, `models_catalog.go:412-415`;
- Magic's surface: `internal/picker/{picker.go,order.go}`,
  `internal/provider/opencode/http.go:400-598`,
  `internal/ws/server.go:2763-2860`;
- dependency state: Magic's `go.mod` contains no `go-llmprovider-sdk`
  module path, and both repositories' trees were searched for
  `MaxListed`/`Usable`/`ListModels` usage rather than inferred.
