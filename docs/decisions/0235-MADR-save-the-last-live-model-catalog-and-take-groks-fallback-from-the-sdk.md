---
status: accepted
date: 2026-11-06
decision-makers: Project Owner
consulted: "this repository at 92ffc2ad and 0f595dc9; go-llmprovider-sdk v1.27.0 (52ab75f, 2026-11-06): llmprovider/catalog, docs/architecture.md, docs/decisions/0012-MADR-conform-providers-to-reference-clients.md, docs/decisions/0070-MADR-place-grok-4-20-by-date-and-split-model-labels.md; grok-build source at 2bdd1d6a (crates/codegen/xai-grok-models); the grok wire captures under internal/provider/grok/testdata/wire; 0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md F5; six scratch probes, listed under More Information"
informed: anyone who changes a provider's model catalog, internal/picker, the models.list reply, or the go-llmprovider-sdk pin
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Save the last live model catalog, and take grok's fallback from the SDK

## Context and Problem Statement

While recording 0234's single-table probe, the owner saw that Magic's grok fallback list is two
hard-coded ids. They asked: "this is hard-coded and going to constantly be going out of date.
didn't we solve this in go-llmprovider-sdk via models.dev? what can we do to improve fallback
heuristics and make it dynamic?"

Four options were explained:

1. save the last live list;
2. take the fallback from the SDK;
3. use models.dev for facts;
4. rank in Magic.

The owner then said: "other than having to add the go-llmprovider-sdk as a dependency, i am leaning
towards options 1 and 2", and asked for this record: "Write the detailed, complete madr for options
1 and 2."

The SDK's comment calling `grok-4.6` "the Grok CLI's default" was split off as out of scope here.
It is go-llmprovider-sdk issue 11, opened 2026-11-06.

The question: how a provider's model picker stays current when the agent cannot be asked, without a
list that someone in this repository has to keep up to date by hand.

### F1. How Magic builds a model catalog today

* **The live catalog is the agent's own answer, and nothing is mixed into it.**
  * For `acpagent` providers (grok, opencode over ACP), `initialize`'s `_meta.modelState` fills
    `p.catalogCache` (`internal/provider/acpagent/acpagent.go:1473`). A session's model update
    fills it too (`acpagent/host.go:309-317`).
  * Before any session, `ListModels` starts one short-lived agent process to read it
    (`acpagent.go:420-456`, `harvestCatalog`).
  * The comment at `acpagent.go:450-454` reads: "A live catalog is authoritative and must not be
    padded with the static floor".
* **The live catalog is kept in memory only.** `catalogCache` is a field of the `Provider`. A
  daemon restart loses it, and the next picker open must start the agent again.
* **When the agent cannot answer, a provider falls back to its own static list.** That happens
  when the binary is missing, the process cannot start, or `initialize` carries no model state:
  * grok: `staticModels`, two hard-coded options, `grok-4.6` and `grok-4.5`
    (`internal/provider/grok/grok.go:18-31`). The comment says they were "Measured 2026-08-12
    against grok 1.0.3".
  * opencode over ACP: `acpStaticModels`, seven free models from a 2.2.3 capture
    (`internal/provider/opencode/acp.go:40-52`).
  * kilo: `staticModelOptions` (`internal/provider/kilo/catalog.go:16-34`), merged with a live
    list by `httpagent` (`internal/provider/httpagent/provider.go:184-216`).
  * codex: an empty list where the user types a model in (`internal/provider/codex/provider.go:294-303`,
    `:338-383`).
* **The phone shows where a catalog came from.** `picker.Source` is `live`, `static` or `merged`
  (`internal/picker/picker.go:25-34`), and travels as `source` in `models.list_result`
  (`internal/protocol/messages.go:1042-1057`).
  * The phone labels these "Live catalog", "Offline catalog" and "Live + offline", and shows no
    badge for any other value (`apps/mobile/lib/data/protocol/picker.dart:19-50`).

### F2. grok's static list is grok-build's compiled-in last resort, not what users see

* grok-build compiles in `crates/codegen/xai-grok-models/default_models.json`, which holds
  `"default": "grok-4.6"` and models `grok-4.6` and `grok-4.5`. That is the grok-build source at
  `2bdd1d6a`, where the file last changed in `9684fa3c`, 2026-08-27.
* The same crate's `src/lib.rs:4` says: "At runtime each model is resolved from the first of these
  that is set: CLI flag, ENV var, config.toml, remote settings, these defaults."
* What grok 1.0.70 reports at `initialize`:

  | State | `availableModels` | `currentModelId` | Evidence |
  | :--- | :--- | :--- | :--- |
  | signed out, no usable key | `grok-4.6`, `grok-4.5` | `grok-4.6` | 0234 F5, "Single model tables" |
  | a host with an xAI API key | 14 models, `grok-4.7` among them | `grok-4.20-0309-non-reasoning` | `testdata/wire/1.0.70/frames.jsonl` |
  | the owner's signed-in account | not measured | `grok-4.7`, by the owner's report | the owner, 2026-11-06 |

* So Magic's floor is the list a user sees only when grok cannot read its account. Users with an
  account see a different list.
* The committed captures show the live list growing past the floor:
  * 1.0.13 lists `grok-4.6` and `grok-4.5`;
  * 1.0.50 adds `grok-4.7` and `grok-4.7-build-fast`;
  * 1.0.70 lists 14 models.

  The floor has stayed at the first of these.

### F3. What go-llmprovider-sdk does, and does not do

The SDK's `llmprovider/catalog` at v1.27.0, the current release:

* **It layers its listing** (`catalog/discovery.go:186-191`; SDK `docs/architecture.md`):
  1. **live**, from the provider's API with a key;
  2. **stale**, "the last good listing, at most 24 hours old, served after a transient failure",
     handed in by the caller with `catalog.WithLastGood(ids, fetchedAt)` (`catalog/config.go:139-152`);
  3. **static**, `catalog.Static(provider)` (`catalog/models_catalog.go:261`).
* **Its static list is hard-coded too, but kept in one place.** A maintainer tool,
  `go run ./llmprovider/internal/cmd/catalogs`, reads models.dev and each keyed listing, and
  proposes changes (SDK `docs/architecture.md:144`).
* **models.dev supplies facts, not membership.** Its document sets each model's limits,
  modalities, prices and lifecycle (`catalog/doc.go`, "Model metadata comes from a models.dev-format
  document"). It does not decide which models a listing holds.
* **Its grok list:** `Static(grok)` is `grok-4.6`, `grok-4.5`, `grok-4.7`, `grok-4.3`,
  `grok-build-0.1` (`models_catalog.go:157`; Probe 2). All five appear in the 1.0.70 keyed
  capture's `availableModels`. v1.27.0 keeps that order: SDK 0070 left whether it should lead with
  `grok-4.7` to a record of its own (its Q2). So `Static(grok)`'s first id is not a default to take.
* **Its grok ranking:** `catalog.Rank(grok, id)` (`models_catalog.go:236`, `rankGrokModel` at
  `:682`) scores by generation (Probe 2):

  | id | `Rank` |
  | :--- | ---: |
  | `grok-4.7` | 70 |
  | `grok-4.6` | 60 |
  | `grok-4.5` | 50 |
  | `grok-4.3` | 45 |
  | `grok-build-0.1`, `grok-imagine-*` | 0 |
  | `grok-4.8` (not yet released) | 81 |
  | `grok-5` (not yet released) | 100 |
  | `grok-4.20-0309-non-reasoning`, `-reasoning`, `grok-4.20-multi-agent-0309` | 40 |

  * A generation newer than its anchors ranks above them (`generationScore`, `:740`). So the
    ranking does not go stale for a release it has not seen.
  * **v1.26.1 scored the 4.20 ids 93, above `grok-4.7`.** It read `grok-4.20` as generation 4.20,
    though xAI released it before `grok-4.3`. v1.27.0 reads a family's two-digit minor as an
    integer or a decimal, chosen from the family's dates, else from the versions its siblings
    fill. So `grok-4.20` is 4.2, below `grok-4.3`, and `gemini-3.10` still follows `gemini-3.9`
    (SDK 0070 D1, found while this record was drafted).
* **`catalog.SortByRank(provider, ids, released)` sorts a whole list** by `Rank`'s score, highest
  first, stably, reading each family's versions from the list and from any release dates the
  caller gives (`version_reading.go:220`; new in v1.27.0). It returns a new slice and fetches
  nothing.
  * On `Static(grok)` it gives `grok-4.7`, `grok-4.6`, `grok-4.5`, `grok-4.3`, `grok-build-0.1`.
  * On the 1.0.70 capture's 14 ids it gives 4.7, 4.6, 4.5, 4.3, the three 4.20 ids, then
    `grok-build-0.1` and the six `grok-imagine-*` ids (Probe 5). The capture lists them
    alphabetically, 4.20 first.
  * Its scores are the SDK's preference for its own use, a fast, low-cost model for a git hook
    ("Prioritizes low-latency Flash … for fast Git hook execution", `models_catalog.go:507-508`).
    For claude, gemini and openai it puts the fastest or cheapest model first (Probe 6). grok's
    scores read generation only, so for grok the order is newest first.
* **Its labels come in two parts.** `catalog.Label` still returns a padded terminal-menu string,
  such as `Grok 4.7               [frontier model]`. v1.27.0 adds `catalog.LabelName`, `Grok 4.7`,
  and `catalog.LabelNote`, `frontier model`, both unpadded (`models_catalog.go:954`, `:965`; SDK
  0070 D2). The grok notes no longer call `grok-4.6` the Grok CLI's default. It is now "the Grok
  CLI's built-in default" (`:907`), which closed issue 11.
  * grok's own live list names each model by its id: `name` equals `modelId` for all 14 in the
    1.0.70 capture, and no model carries a note.
* **The SDK cannot list grok's models for Magic.** Its live layer needs Magic to hold the xAI key,
  and 0074 D2 forbids an mcremote-owned vault. grok's own `initialize` stays Magic's live source.

### F4. The SDK's other static lists do not match Magic's other providers

* opencode: `Static(opencode-zen)` is `deepseek-v4.1-flash`, `glm-5.3-flash` and so on, with no
  prefix and no `-free` (`models_catalog.go:87-94`). Magic's opencode ids are
  `opencode/deepseek-v4.1-flash-free` and so on (`opencode/acp.go:44-52`).
* kilo: `Static(kilo)` is gateway ids such as `deepseek/deepseek-v4.1-flash`
  (`models_catalog.go:143-150`). Magic's kilo ids are provider-prefixed engine ids, such as
  `kilo/<default>` and `openrouter/openrouter/free` (`kilo/catalog.go:20-26`). By 0075 Q11 those
  ids "must come from a catalog, never string concatenation".
* So only grok's ids map one to one. A saved live list (D1) covers every provider. The SDK's list
  (D4) covers only grok.

### F5. The cost of the dependency, measured

Probe 1 built `./cmd/mcremote` from two scratch copies of `0f595dc9`. One was unchanged. The other
had `go get github.com/maccavelli/go-llmprovider-sdk@v1.27.0`, and a file in
`internal/provider/grok` calling `catalog.SortByRank` over `catalog.Static`, as D4 does, so that the
linker keeps the package.

| | Value |
| :--- | :--- |
| `go.mod` lines added | `github.com/maccavelli/go-llmprovider-sdk v1.27.0`, and nothing else |
| modules added to `go list -m all` | that one |
| binary, unchanged | 39,683,074 bytes |
| binary, with the SDK | 40,012,114 bytes |
| growth | 329,040 bytes, 0.83% |

At v1.26.1, calling `Static` and `Rank`, the growth was 294,032 bytes, 0.74%. v1.27.0 adds the
version reading.

* The SDK module requires `golang.org/x/term v0.43.0` and `golang.org/x/sys v0.47.0` (SDK
  `go.mod`). Magic already pins newer: `x/sys v0.49.0` and `x/term v0.47.0` (`go.mod:22`, `:65`).
* `catalog` compiles in 16 of the SDK's own packages, `catalog` included, among them
  `llmprovider`, `auth`, `transport` and `wire` (`go list -deps`).
* None of those 16 packages has an `init` function. The SDK's one `init` is in
  `llmprovider/providers/opencode` (`route.go:89`), which `catalog` does not import. `Static`
  and `SortByRank` start no network call.

### F6. 0185 forbids the dependency as it stands

[0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md),
accepted 2026-10-13, decides:

* "No dependency. Magic does not add `go-llmprovider-sdk` to `go.mod`."
* "Catalogs stay as they are. ... the engine adapters remain the catalog source."
* Its Confirmation: "A review rejects any change that adds a `go-llmprovider-sdk` module to
  `go.mod`, or that calls its catalog API, unless a later MADR supersedes this one."

Its own Bad consequence names the cost this record addresses: "Magic keeps maintaining its own
per-engine catalog code and model metadata. That includes the static fallbacks, which the SDK also
curates."

So D4 here needs 0185's first bullet superseded, for one narrow use. Its second bullet still holds:
the engine stays the catalog source, and this record never lets the SDK list or fetch anything.

## Decision Drivers

* **The agent's answer is the truth** (pass-through first; `acpagent.go:450-454`). A fallback
  stands in for it, and never edits it.
* **No list in this repository that a person must keep current by hand,** where a maintained or
  saved one exists.
* **The fallback should be the newest truthful list available:** this host's own last answer
  first, then a maintained list, then nothing.
* **Every provider gets the same fallback layer** (provider parity), even where the SDK has no list
  for it.
* **A fallback says it is one.** The phone already badges `static`. A saved list should be told
  apart from both a live one and a hard-coded one.
* **The smallest dependency that removes the hand-kept list,** with nothing in it that fetches.
* **`acpagent` names no vendor** (`acpagent/neutral_test.go`, `TestNoProviderIsNamedInAcpagent`).

## Considered Options

* **A — Save the last live catalog for every provider, and take grok's static list from the SDK**
  (options 1 and 2).
* **B — Save the last live catalog, and rank Magic's own hard-coded list with a version rule in
  Magic** (options 1 and 4).
* **C — Save the last live catalog only** (option 1).
* **D — Take grok's static list from the SDK only** (option 2).
* **E — Fetch models.dev's `xai` section at run time, and use it as the static list.**
* **F — Do nothing.**

## Decision Outcome

Chosen option: **"A"**, because:

* it removes the hand-kept grok list (D4);
* it gives every provider, with or without an SDK list, the agent's own last answer as its fallback
  (D1);
* it uses only the SDK's two functions that fetch nothing.

What the owner weighed is the dependency's cost, and F5 measures it.

### The decisions

* **D1 — Every provider's last live catalog is saved, and is the first fallback.**
  * **What is saved:** a non-empty catalog whose source is `live`, at the point where the
    provider receives it from its engine:
    * `acpagent`: where `catalogCache` is set, `acpagent.go:1473` and `host.go:313`;
    * `httpagent`: the live list, before the merge with static, `httpagent/provider.go:191-199`;
    * codex: after a `model/list` that returned models, `codex/provider.go:305-308`.
  * **What is not saved:**
    * a `static` catalog;
    * a merged one, since it would store the static rows as if they were the engine's;
    * an empty one;
    * anything from `ListModelsFor` or `ListModelProviders`. Those derive from `ListModels`, so
      they gain the saved layer through it.
  * **When it is used:** at each point where the provider would now return its static catalog:
    * `acpagent.go:437`, `:442`, `:448`;
    * `httpagent/provider.go:197`;
    * codex's fallback returns.

    The saved catalog takes the live one's place in the provider's existing logic. The configured
    model still wins the default (`withCatalogDefault`, `withConfiguredDefault`), and kilo still
    merges with its static list. The result's `Source` is `saved`, from D2.
  * **No age limit.**
    * The SDK limits its last-good listing to 24 hours, because its static list is the next layer.
      Here the next layer is a list from August (F2) or nothing (codex). A list this host's agent
      gave at any time is closer to the truth than either.
    * The saved catalog is replaced the next time the agent answers.
  * **What it holds:**
    * a version;
    * the provider id;
    * the time it was saved;
    * the engine version, when the provider knows it;
    * the catalog, capped at the transport's 500 options (`internal/ws/server.go:2866`), with
      `Truncated` set when rows were cut.

    The engine version is shown only in logs.
  * **Where it lives:** `<CacheDir>/instances/<InstanceKey>/catalogs/<provider-id>.json`. It
    follows the pattern of `EngineRegistryDir` (`internal/appdirs/paths.go:99-102`), so two daemon
    instances with different data directories do not share one.
    * The directory is made with `appdirs.EnsurePrivateDir`, and the file is written with
      `fsutil.WriteFileAtomic` (`internal/fsutil/atomic.go:55`), owner-only.
    * A catalog holds model ids, labels and capability fields, never a credential, but it can name
      an account's models.
  * **Written only on change:** a catalog equal to the saved one is not rewritten. Without this,
    every grok spawn, warm spares included, would write the file.
  * **A bad file is no file.** A missing file, an unknown version or a decode error is treated as
    no saved catalog, logged at debug, and replaced at the next live answer. A failed write is
    logged at warn, and never fails the request.
  * **Who gets a store:** one store type, provider-neutral, in a new package
    `internal/picker/saved`. It reaches providers through a new `provider.Build` field.
    * The daemon's `Run` passes it.
    * `InspectProviders` (`mcremote doctor`, `internal/daemon/daemon.go:55-59`) passes none, so
      doctor neither reads nor writes saved catalogs.
* **D2 — A saved catalog is labelled `saved` on the wire and on the phone.**
  * `picker.SourceSaved Source = "saved"` joins `live`, `static` and `merged`.
  * The phone parses `saved` and labels it "Saved catalog".
  * A phone without that change shows no badge for it (F1). That is the defined behaviour for an
    unknown source, so the change is compatible both ways.
* **D3 — When nothing is saved, each provider keeps its own static list,** with one exception, D4.
  opencode's and kilo's lists stay hand-kept, because the SDK's do not match them (F4). After the
  first live answer, D1 means a user meets them only if the saved file is lost.
* **D4 — grok's static list is the SDK's, ordered by the SDK's rank.**
  * `staticModels` in `internal/provider/grok/grok.go` is
    `catalog.SortByRank(llmprovider.ProviderGrok, catalog.Static(llmprovider.ProviderGrok), nil)`.
    Magic writes no sort of its own. Each option is `{ID: id, Label: id, Group: "xai"}`.
  * At v1.27.0 that gives `grok-4.7`, `grok-4.6`, `grok-4.5`, `grok-4.3`, `grok-build-0.1`. The
    first is the floor's default unless a model is configured. `Static(grok)`'s own first id,
    `grok-4.6`, is not used as the default (F3).
  * **The label is the id,** not `catalog.LabelName`, and no `LabelNote` goes in `Description`.
    grok's live list names each model by its id and carries no note (F3). With the SDK's names,
    the floor's `Grok 4.7` would become `grok-4.7` at grok's first answer.
  * **`SortByRank` orders the SDK's list only.** It never reorders a live or saved catalog. Those
    keep the agent's order and default. Magic orders models in one place, `picker.OrderModels`
    (`internal/picker/order.go:43-58`), which keeps an engine's order when it reports no dates
    ([0043-MADR-model-selection.md](0043-MADR-model-selection.md) D3). Changing that is a change
    for every provider, and `SortByRank`'s scores rank by speed and cost outside grok (F3).
  * **Only `Static` and `SortByRank` are called.** Never `List`, `ListRaw`, `LookupMetadata` or
    any other function that fetches, lists or caches.
  * **The import lives only in `internal/provider/grok`.** `acpagent` stays vendor-neutral, and no
    other package imports the SDK. A source test enforces both, in the same way as
    `TestNoProviderIsNamedInAcpagent`.
  * **The SDK's list must be grok's.** A unit test fails when an id in it is absent from the newest
    committed grok capture's `availableModels`. A new SDK list then means a new capture, or a
    recorded reason, before the pin moves. That is the guard against offering a model grok refuses
    (the `grok.go:21-22` comment).
  * **The SDK pin follows 0217's rule,** current release, and `govulncheck` covers it as it covers
    every module (`scripts/go-precheck.sh`).
* **D5 — 0185 is superseded in part.**
  * Its "No dependency" bullet, and the part of its Confirmation that rejects any
    `go-llmprovider-sdk` module, give way to D4: one module, two functions (`Static`,
    `SortByRank`), one package.
  * Everything else in 0185 stands: catalogs stay engine-backed, and no catalog moves to the SDK's
    listing.
  * 0185 gets a dated note saying so, pointing here.
* **D6 — Scope.** Out of this record:
  * models.dev facts (option 3);
  * the SDK's labels, `Label`, `LabelName` and `LabelNote`;
  * ordering live or saved catalogs with the SDK's ranking, for grok or any provider;
  * a model-generation rule written in Magic (option 4);
  * which grok model a phone key write's config table makes current (0234 F5);
  * a saved layer for `httpagent`'s model-provider and per-provider lists (the correction of
    2026-11-06, below).

### Consequences

* Good, because after an agent's first answer on a host, its picker survives restarts and outages
  with that agent's own list and default, for every provider.
* Good, because the hand-kept grok list goes, and its replacement leads with `grok-4.7`, today's
  flagship by the SDK's ranking and the owner's account.
* Good, because a saved list is labelled, so a user can tell it from a live one.
* Good, because codex's empty fallback gains a list once codex has answered once.
* Neutral, because the floor is now met only before an agent's first answer on a host, or after the
  saved file is lost.
* Bad, because a new grok model reaches the floor only after an SDK release, a Magic pin move and a
  capture. The saved list carries it sooner, from the first live answer.
* Bad, because the binary grows 329,040 bytes (0.83%) for two functions (F5).
* Bad, because a saved list can name a model the agent has since dropped. It is chosen, refused,
  and replaced at the next live answer, as a static list's stale model is today.
* Bad, because when grok cannot read its account, it answers with its compiled-in two models (F2)
  as a live list. That is saved, and replaces a fuller saved list until grok can read the account
  again. Magic saves what the agent says, and does not guess which answers are degraded.

### Confirmation

* **D1, the store:**
  * a save and a load give back the catalog;
  * the file is owner-only;
  * an unknown version, a truncated file and invalid JSON each load as nothing;
  * an equal catalog is not rewritten;
  * more than 500 options are cut, with `Truncated` set.

  Each is seen failing first on a scratch copy with the behaviour removed.
* **D1, the providers:**
  * an `acpagent` provider that cannot start its agent returns the saved catalog with source
    `saved`, and without one returns static;
  * the same for `httpagent` and codex;
  * a live answer is saved, and a static or merged one is not;
  * `InspectProviders` writes nothing.

  Each is seen failing first.
* **D2:** a protocol test for `saved`, and a phone test that `saved` parses and is labelled
  "Saved catalog". `flutter analyze`, `flutter test` and `dart format` pass.
* **D4:**
  * the floor equals `SortByRank(grok, Static(grok), nil)`, and its default is `grok-4.7` at
    v1.27.0;
  * no label or description comes from `catalog.Label`, `LabelName` or `LabelNote`;
  * a live catalog with the 4.20 ids first keeps that order (`SortByRank` is not applied to it);
  * every floor id is in the newest committed capture;
  * the source test finds the SDK imported only by `internal/provider/grok`.

  Each is seen failing first.
* **The dependency:** `go list -m all` gains exactly the SDK, and `make pre-add-check`'s
  `govulncheck` is clean.
* **Live:** a scratch daemon instance, with its own data directory, first lists grok's models
  through the real grok, so that it saves them. It is then restarted with its grok `bin` pointing
  at a path that does not exist.
  * Its picker shows the saved grok list, labelled "Saved catalog".
  * With the saved file removed as well, it shows the SDK floor, labelled "Offline catalog".
  * The real grok install and the host's own daemon are not touched, and no prompt is sent.
* **Gates:** `make race`, the Linux test host's `go test -race ./...`, and `make ci-windows`.

## Pros and Cons of the Options

### A — Saved catalog for every provider, SDK list for grok (chosen)

* Good, because each fallback is the newest truthful list there is: this host's agent's, then a
  maintained one.
* Good, because no grok list is kept by hand in this repository.
* Good, because the SDK's ranking orders a generation it has not seen above the ones it has
  (F3), so the floor's default does not go stale with each release.
* Bad, because it adds a dependency that 0185 ruled out, for a case D1 makes rare.

### B — Saved catalog, and Magic's own version rule over its own list

* Good, because there is no dependency.
* Bad, because grok's list stays hand-kept. On a fresh host the picker still offers `grok-4.6`
  and `grok-4.5`, and misses `grok-4.7`.
* Bad, because a version rule must handle `grok-4.20-0309-*`, `grok-build-0.1` and
  `grok-imagine-*`. The SDK's rule needed a release to read the 4.20 ids (F3). That is a second
  rule to keep right.

### C — Saved catalog only

* Good, because it is the smallest change that makes a fallback current.
* Bad, because the hand-kept list stays for a fresh host, which is what the owner raised.

### D — SDK list only

* Good, because the hand-kept grok list goes.
* Bad, because a restart still loses the live list. The picker then waits for an agent start, or
  shows a list that is not this host's.
* Bad, because it helps grok only (F4).

### E — models.dev's `xai` section at run time

* Good, because it changes without a release of anything.
* Bad, because the daemon would fetch from the internet to draw a picker.
* Bad, because models.dev describes the xAI API, not grok's CLI. The CLI lists `grok-build-0.1`
  and image and video models.
* Bad, because the SDK itself uses models.dev for facts, not membership (F3), and 0185 rules out
  calling the SDK's catalog fetches.

### F — Do nothing

* Good, because nothing changes.
* Bad, because grok's floor stays a list a signed-in user never sees (F2), and every restart
  forgets the live list.

## Correction 2026-11-06: what the PLAN's code reading found

Found while writing [0235-PLAN-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md](0235-PLAN-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md),
after acceptance and before any code. The decisions stand; three facts above were wrong, and D4
needs one field it did not name. The text above is left as written.

* **kilo does not merge with its static list.** kilo's dialect lists model providers
  (`httpagent.ModelProviderLister`), so `httpagent`'s `ListModels` returns the live list alone
  (`internal/provider/httpagent/provider.go:205-211`). The merge (`:213-214`) is for a dialect that
  lists no model providers, and none is registered. F1's "merged with a live list by `httpagent`"
  and D1's "kilo still merges with its static list" are wrong. For kilo, as for the others, the
  saved catalog stands in where the static list is returned now.
* **`httpagent`'s `ListModelsFor` and `ListModelProviders` do not derive from `ListModels`.**
  Each fetches live, with no static fallback (`httpagent/provider.go:219-247`). D1's "Those derive
  from `ListModels`" holds for `acpagent` (`internal/provider/acpagent/modelcatalog.go:54-89`)
  only. So kilo's model-provider step and per-provider lists keep today's behaviour when kilo cannot
  answer, and gain no saved layer. That is added to D6's scope list.
* **D4's floor default needs a field.** `acpagent`'s static catalog carries only the configured
  model as its default (`acpagent.go:513-516`), and the phone preselects a default only when
  `default_ids` is not empty (`apps/mobile/lib/features/widgets/option_picker_sheet.dart:217-222`).
  So today's grok floor has no default at all, and "the first is the floor's default" needs code:
  `acpagent.Spec` gains `StaticDefaultModel`, used when no model is configured. grok sets it to the
  floor's first id. opencode leaves it empty, so its floor is unchanged.
* **A further Good consequence.** opencode over ACP never starts an agent to read its catalog
  (`acpNoHarvest`, `internal/provider/opencode/acp_catalog.go:38-43`). Before a session it shows its
  static list. With D1 it shows the list its last session reported instead.

## More Information

### Owner questions

Answered by the owner on 2026-11-06, each as recommended. The record already said each; the
answers confirm it.

* **Q1. The floor's labels (D4).** Options: the id; `LabelName`; `LabelName` with `LabelNote` in
  `Description`. Answer: the id, so nothing changes name when grok first answers.
* **Q2. The saved catalog's age limit (D1).** Options: none; 30 days; 24 hours, as the SDK's
  `WithLastGood`. Answer: none.
* **Q3. grok's compiled-in pair, answered as a live list (Consequences).** Options: save it as the
  agent said it; a grok `Spec` hook that keeps a fuller saved list; never let a smaller live answer
  replace a larger saved one. Answer: save it as said.
* **Q4. Ordering every provider's models by version.** Options: its own record after this one; its
  own record now; not at all. Answer: its own record, after this one. It would ask the SDK for a
  version sort without `SortByRank`'s speed and cost scores, probe codex's and opencode's live
  order, and revise [0043-MADR-model-selection.md](0043-MADR-model-selection.md) D3. Until then,
  D4 and D6 stand: no live or saved catalog is reordered.

### Evidence index

Every probe ran on scratch copies or in a scratch module, never in the tree. Scripts are in the
session scratchpad.

* **Probe 1** (`libs/sdk_size.py`): F5's table.
  * Two scratch copies of `0f595dc9`, built with `go build -trimpath ./cmd/mcremote`. One gained
    `go get …@v1.27.0` and a file calling `SortByRank` over `Static`, then `go mod tidy`.
  * The first run, on `92ffc2ad` with v1.26.1 and a file calling `Static` and `Rank`, gave the
    0.74%. The unchanged copy measured 39,683,074 bytes both times.
  * Both copies were removed afterwards.
* **Probe 2** (`rankprobe/`, a scratch module, `go run .`): F3's `Static` list and `Rank` table, at
  v1.26.1 and again at v1.27.0, with `LabelName` and `LabelNote` for the five `Static` ids.
* **Probe 3** (`go list -deps` of `llmprovider/catalog`, v1.27.0): F5's count of 16 SDK packages.
  `git grep '^func init('` at the v1.27.0 tag found one, in `providers/opencode`.
* **Probe 4:** the grok `initialize` probes recorded in 0234 F5, the source of F2's first row.
* **Probe 5** (`libs/rank127.py`, the same module on v1.27.0): `Rank` and `SortByRank` over the
  1.0.70 capture's 14 `modelId` values, in capture order.
* **Probe 6** (`libs/sortprobe.py`): `SortByRank` on picker-shaped lists for claude, gemini,
  openai, kilo and opencode-zen. Some ids were made up to show the shape. Claude's came out
  haiku, sonnet, opus; gemini's Pro last; openai's cheapest tier first. Every kilo id scored 0.
* **Captures:** `internal/provider/grok/testdata/wire/`:
  * `1.0.13/frames.jsonl`: `grok-4.6`, `grok-4.5`;
  * `1.0.50/frames.jsonl`: adds `grok-4.7` and `grok-4.7-build-fast`;
  * `1.0.70/frames.jsonl`: 14 models.

  These are the `modelId` values in each file.
* **Cross-repository:** go-llmprovider-sdk issue 11, "catalog: staticGrok and its labels call
  grok-build's compiled-in fallback "the Grok CLI's default"", opened 2026-11-06, closed
  2026-11-06 as completed by v1.27.0.
* **Cross-repository:** go-llmprovider-sdk
  `docs/decisions/0070-MADR-place-grok-4-20-by-date-and-split-model-labels.md`, accepted
  2026-11-06: D1, the version reading and `SortByRank`; D2, `LabelName` and `LabelNote`.

### Related records

* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md):
  superseded in part by D5.
* [0039-MADR-grok-acp-parity.md](0039-MADR-grok-acp-parity.md): D2, the live catalog from
  `_meta.modelState`, which D1 saves.
* [0043-MADR-model-selection.md](0043-MADR-model-selection.md): D7, the live catalog before the
  first session, and D4, truncation, which D1's cap keeps.
* [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md):
  the pin rule D4 follows.
* [0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md](0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md):
  F5, the single-table probe that raised this question.
