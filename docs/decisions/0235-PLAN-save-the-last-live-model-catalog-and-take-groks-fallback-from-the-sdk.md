---
status: complete
date: 2026-11-06
associated-madr: "0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement saving the last live model catalog, and taking grok's fallback from the SDK

Associated MADR: [0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md](0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md)

## Goal

Every provider's last non-empty live model catalog is saved on disk, per daemon instance. It is
the first fallback when the agent cannot answer, and it reaches the phone labelled "Saved
catalog". grok's static list, the last fallback, comes from go-llmprovider-sdk v1.27.0:
`SortByRank(grok, Static(grok), nil)`, ids as labels, `grok-4.7` preselected. The SDK is imported
by `internal/provider/grok` alone, which calls only `Static` and `SortByRank`, and tests enforce
both. 0185 carries a dated note that 0235 supersedes it in part.

The MADR is accepted (owner, 2026-11-06), with its correction of the same day. This PLAN follows
the MADR as corrected.

## Scope

**In:**

* P1: the saved-catalog store, `picker.SourceSaved`, and its path (D1's store, D2's Go side);
* P2: the store wired into the daemon and the four providers (D1's providers);
* P3: the phone's `saved` source (D2's phone side);
* P4: the SDK dependency, grok's floor and its default, the boundary tests, and 0185's note (D4,
  D5);
* P5: the live confirmation, the final gates, and closing the pair.

**Out:**

* everything in the MADR's D6 list, including models.dev facts, the SDK's labels, ordering live or
  saved catalogs, and a saved layer for `httpagent`'s model-provider and per-provider lists;
* opencode's and kilo's static lists, which stay hand-kept (D3);
* `mcremote paths` output. The new path joins `appdirs.Paths` but is not printed, because no
  record asks for it;
* an empty live answer from `httpagent` without an error. It is returned as today, unsaved, and the
  saved catalog does not replace it. D1 replaces only the points that return the static list now;
* restoring codex's per-model records (`p.models`, `codex/provider.go:305-310`) from a saved
  catalog. Until codex answers, the features that read them see none, as today;
* `OrderModels` and version ordering: the owner's Q4, a record of its own after this one.

## Constraints

* **C1 — One phase, one commit.** The agent stages, and the owner commits with
  `git commit --no-edit`. `make pre-add-check FILES="…"` passes on every staged Go file first.
  `dart format` passes on every staged Dart file, and `make preflight`'s mobile trio passes in P3.
* **C2 — No push and no tag without an explicit ask in the same turn.** Before asking for a push,
  run the disclosure guard's `pre-push` over the outgoing commits (AGENTS.md).
* **C3 — The owner's word before each host run and each live run.** That covers:
  * the Linux test host's `go test -race ./...` and the Windows test host's `make ci-windows`,
    at the end of P1, P2 and P4;
  * the live grok test in P5;
  * the scratch-daemon and emulator check in P5.

  All are asked for at approval.
* **C4 — Each new test is seen failing first, on a scratch copy of the tree, never by dirtying
  it.** The execution record says what each failure looked like.
* **C5 — `acpagent` names no vendor,** in code, comments or tests. `TestNoProviderIsNamedInAcpagent`
  passes after every phase. `acpagent`'s tests use `vendor` ids.
* **C6 — No provider quota.** The live grok test sends `initialize` only. It runs with grok's home
  isolated and `XAI_API_KEY` unset, so no key is read or used and no prompt is sent.
* **C7 — Records carry no identifier.** Run the disclosure guard's `redact` preview and the
  identifier scan before staging any record.
* **C8 — No file write blocks a caller holding a lock.** `SetModelCatalog` runs with the session's
  mutex held (`acpagent/modelcatalog.go:42-43`), and initialize's cache write is on the agent-spawn
  path. So `Save` records in memory and writes in the background. Tests call `Flush`.

## Implementation Steps

### P1 — The store, the source, and the path (D1's store, D2's Go side)

Files:

* `internal/picker/picker.go`
* `internal/picker/saved/saved.go` (new)
* `internal/picker/saved/saved_test.go` (new)
* `internal/picker/saved/perm_unix_test.go` (new, `//go:build !windows`)
* `internal/appdirs/paths.go`
* `internal/appdirs/paths_test.go`
* `internal/ws/models_cap_test.go`
* `internal/protocol/messages_test.go`, or the file that already tests `ModelsResultFromCatalog`

Steps:

1. **`picker.SourceSaved`.** Add `SourceSaved Source = "saved"` to the `Source` constants
   (`picker.go:27-34`), documented as "the last live catalog this host's agent gave, saved on disk
   (MADR 0235 D1, D2)".
2. **`appdirs.Paths.CatalogDir`.** Add the field after `EngineRegistryDir`. Set it in both `Resolve`
   (`paths.go:101`) and `WithDataDir` (`paths.go:122`) to
   `filepath.Join(p.CacheDir, "instances", key, "catalogs")`.
3. **Package `internal/picker/saved`.** It imports `picker`, `fsutil` and `appdirs`, and nothing
   that names a provider.
   * `const MaxOptions = 500`, the transport's cap.
   * `type Store struct`, safe for concurrent use. Every method is safe on a nil `*Store`: `Load`
     reports nothing, and `Save` and `Flush` do nothing. That is how a provider with no store, or
     doctor, behaves.
   * `func Open(dir string, log *slog.Logger) *Store` returns nil for an empty `dir`. It touches no
     file.
   * `func (s *Store) Save(provider, engineVersion string, c picker.Catalog)`:
     * returns at once, doing nothing, unless `c.Source == picker.SourceLive` and
       `len(c.Options) > 0`;
     * refuses a provider id outside `^[a-z0-9][a-z0-9-]*$`, logging at warn, so no id can name a
       path;
     * normalizes `c`, and cuts it to the first `MaxOptions` options, setting `Truncated`;
     * compares its JSON with the last one saved for that provider, in memory, read from the file
       on first use. An equal catalog is neither recorded nor written;
     * otherwise records it in memory, so the next `Load` returns it, and queues it for writing.
   * The writer is one goroutine per store, started on first need. It writes the latest queued
     catalog for each provider, so a burst of saves writes once.
     * The directory is made with `appdirs.EnsurePrivateDir`.
     * The file is written with `fsutil.WriteFileAtomic`, `Perm: 0o600, SyncFile: true`.
     * A failed write is logged at warn with the provider and the error, and is not retried until
       the next changed `Save`.
   * `func (s *Store) Load(provider string) (picker.Catalog, bool)` returns the in-memory catalog
     when there is one. Otherwise it reads `<dir>/<provider>.json` once. It returns the catalog
     with `Source` set to `picker.SourceSaved`.
     * A missing file, a decode error, an unknown `version`, or a `provider` field that differs
       from the name asked for, each return `false`, logged at debug. A later live `Save` replaces
       the file.
   * `func (s *Store) Flush()` waits until every queued write has finished. It is for tests and
     for daemon shutdown.
   * The file:

     ```json
     {
       "version": 1,
       "provider": "<id>",
       "saved_at": "<RFC 3339, UTC>",
       "engine_version": "<as the provider reports it, may be empty>",
       "catalog": { "<picker.Catalog, with source live>": "" }
     }
     ```

     `engine_version` and `saved_at` go to the debug log when the catalog is loaded, and nowhere
     else (D1).
   * An unexported write hook, defaulting to `fsutil.WriteFileAtomic`, lets tests count writes and
     inject a failure.
4. **Store tests, seen failing first (C4).** Each experiment runs on a scratch copy with the named
   behaviour removed:

   | Test | Asserts | Scratch break, and expected failure |
   | :--- | :--- | :--- |
   | `TestSaveThenLoadRoundTrips` | after `Save` and `Flush`, a new `Open` on the same dir loads the same options, defaults and `AllowCustom`, with `Source` `saved` | `Load` returns `false` always: the test reports "no saved catalog" |
   | `TestLoadIsInMemoryBeforeTheWrite` | `Load` right after `Save`, with a write hook that blocks, returns the catalog | `Save` only queues: `Load` misses |
   | `TestOnlyANonEmptyLiveCatalogIsSaved` | `static`, `merged`, `saved` and empty live catalogs write nothing and load nothing | the source check removed: a static catalog is written |
   | `TestAnEqualCatalogIsNotRewritten` | two equal saves, then `Flush`, count one write; a changed one counts two | the comparison removed: two writes for equal catalogs |
   | `TestTheLatestQueuedCatalogWins` | saves A then B with the hook blocked, then `Flush`: the file holds B, after at most two writes | the writer writes each queued save in order: three writes, or A last |
   | `TestABadFileIsNoFile` | invalid JSON, a truncated file, `version: 2`, and a `provider` mismatch each load `false`, and a later save replaces the file | the version check removed: `version: 2` loads |
   | `TestTheCatalogIsCutAtMaxOptions` | 501 options save as 500 with `Truncated` set | the cut removed: 501 load |
   | `TestAProviderIDCannotNameAPath` | `../x`, `a/b`, `""` and `Grok` write nothing outside or inside `dir` | the id check removed: a file appears outside `dir` |
   | `TestAFailedWriteIsLoggedAndKeptInMemory` | an injected write error logs at warn, `Load` still returns the catalog in this process, and no file exists | the error is ignored: nothing at warn |
   | `TestNilStoreIsInert` | every method on a nil `*Store` is a no-op | `Load` dereferences: panic |
   | `TestTheFileIsOwnerOnly` (`perm_unix_test.go`) | the file's mode is `0600`, and the directory's `0700` | `Perm` set to `0o644`: mode `0644` |

5. **Other tests, seen failing first:**
   * `paths_test.go`: `CatalogDir` holds the instance key, sits under `CacheDir`, and differs for
     two data directories, in both `Resolve` and `WithDataDir`. Scratch break: `WithDataDir` does
     not set it. Expected failure: it is empty.
   * `models_cap_test.go`: `saved.MaxOptions == maxCatalogOptions` (`ws/server.go:2866`). Scratch
     break: `MaxOptions` 499.
   * The protocol test: `ModelsResultFromCatalog` of a `saved` catalog carries `"source":"saved"`.
     Scratch break: the constant spelled `"save"`.
6. **Gates.**
   * `make race` on this host.
   * The Linux test host's `go test -race ./...` and the Windows test host's `make ci-windows`
     (C3). The store's directory and file permissions differ by OS (`appdirs/ensure_windows.go`).
7. **Stage.** Run `make pre-add-check` on the staged Go files, then stage P1 and this PLAN's P1
   execution record.

### P2 — The store in the daemon and the providers (D1's providers)

Files:

* `internal/provider/descriptor.go`
* `internal/daemon/daemon.go`
* `internal/daemon/daemon_test.go`
* `internal/provider/acpagent/acpagent.go`
* `internal/provider/acpagent/host.go`
* `internal/provider/acpagent/saved_catalog_test.go` (new)
* `internal/provider/httpagent/httpagent.go` (its `Config`), `internal/provider/httpagent/provider.go`
* `internal/provider/httpagent/saved_catalog_test.go` (new)
* `internal/provider/codex/provider.go`, and the file holding codex's `Config`
* `internal/provider/codex/saved_catalog_test.go` (new)
* `internal/provider/grok/section.go`, `internal/provider/opencode/section.go`,
  `internal/provider/kilo/section.go`, `internal/provider/codex/section.go`

Steps:

1. **`provider.Build.SavedCatalogs *saved.Store`** (`descriptor.go:41-55`), documented as "nil
   when the daemon is only inspecting (mcremote doctor), so no saved catalog is read or written
   (MADR 0235 D1)".
2. **The daemon.**
   * Add `runBase(cfg, version, log) provider.Build` to `daemon.go`. It is `buildBase` plus
     `SavedCatalogs: saved.Open(cfg.Paths.CatalogDir, log)`. `Run` uses it at `daemon.go:244`, after
     `RecomputePaths` (`:213`). `InspectProviders` keeps `buildBase`, so doctor has no store.
   * `Run` defers the store's `Flush`, so a catalog saved just before shutdown reaches the disk.
   * When `CatalogDir` is empty, `Open` returns nil. `Run` logs that at info once, and providers run
     without the saved layer.
3. **Each descriptor passes the store into its provider's config.** Add
   `SavedCatalogs *saved.Store` to `acpagent.Config`, `httpagent.Config` and codex's `Config`. In
   each `New` (`grok/section.go:138`, `opencode/section.go:74`, `kilo/section.go:101`,
   `codex/section.go:317`), take the config `providerConfig` returns and set the field from
   `b.SavedCatalogs` before constructing the provider.
4. **`acpagent`.**
   * **Save,** non-blocking (C8), where the provider receives a live catalog:
     * in `applyInitializeResponse`, after `reportEngineVersion` (`acpagent.go:1478`), so the
       engine version is known: `p.cfg.SavedCatalogs.Save(string(p.spec.ID), p.EngineVersion(), *info.Models)`
       when `info.Models` is set;
     * in `SetModelCatalog` (`host.go:308-316`), after the unlock, with the same arguments and
       `cat`.

     `Save` itself drops a static, merged or empty catalog (P1).
   * **Use.** Add `func (p *Provider) fallbackCatalog(static picker.Catalog) picker.Catalog`. It
     returns `withCatalogDefault(saved.Normalize(), p.cfg.Model)` when the store loads one for
     `p.spec.ID`, and `static` otherwise. Replace the three `return static, nil` in `ListModels`
     (`acpagent.go:437`, `:442`, `:448`) with `return p.fallbackCatalog(static), nil`.
   * `ListModelProviders` and `ListModelsFor` derive from `ListModels` (`modelcatalog.go:54-89`),
     so they gain the layer with no change.
   * The comment at `acpagent.go:450-454` gains a sentence: a saved catalog is this host's agent's
     own last answer, used only where the static list was, never mixed into a live one.
5. **`httpagent`.** Inside the `p.catalogs.Get` closure of `ListModels` (`provider.go:189-215`):
   * **Save:** after `liveCatalog` returns no error and before the `len(static.Options) == 0`
     test, `p.cfg.SavedCatalogs.Save(<provider id>, p.EngineVersion(), live)` when
     `len(live.Options) > 0`. That is before any merge (D1).
   * **Use:** add `fallbackModels(static picker.Catalog) picker.Catalog`. It returns
     `withConfiguredDefault(saved.Normalize(), p.cfg.Model)` when one loads, and `static`
     otherwise. It replaces `return static, nil` at `provider.go:197`.
   * The no-`ModelLister` return (`:187`), `ListModelProviders` and `ListModelsFor` are unchanged
     (the MADR's correction).
6. **codex.**
   * Add `fallbackCatalog() picker.Catalog`. It returns the saved catalog with `p.cfg.Model` as its
     default when one is set and one loads. Otherwise it returns today's
     `picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true)`.
   * In `ListModels` (`codex/provider.go:294-311`), the two engine-unavailable returns (`:298`,
     `:303`) return `p.fallbackCatalog(), nil`.
   * After `collectModels`:
     * a `SourceLive` catalog is saved, with `p.EngineVersion()`;
     * a `SourceStatic` one, which is `collectModels`'s own fallback, is replaced by
       `p.fallbackCatalog()`.

     `listModelsVia` and `collectModels` are unchanged, so their tests are too.
7. **Tests, each seen failing first (C4):**

   | Test | Asserts | Scratch break, and expected failure |
   | :--- | :--- | :--- |
   | `acpagent` `TestAnInitializeCatalogIsSaved` | an agent from `acptest`, whose Spec's `InitializeMeta` returns a live catalog of `vendor-a`, `vendor-b`, leaves that catalog in a temp store after `Flush` | the `applyInitializeResponse` save removed: no file |
   | `acpagent` `TestASessionModelOptionIsSaved` | a session whose `session/new` config options carry a `model` option (as `configoption_test.go` builds), with `ModelsFromOption` set, saves that catalog | the `SetModelCatalog` save removed: no file |
   | `acpagent` `TestAMissingAgentServesTheSavedCatalog` | a provider whose `Bin` does not exist, with a store holding a catalog for its id, lists it with `Source` `saved`, and with the configured model as the default when one is set | `fallbackCatalog` returns `static`: `Source` `static` |
   | `acpagent` `TestNoSavedCatalogServesTheStaticOne` | the same, with an empty store, lists the Spec's static options, and writes nothing | `Save` called on the static path: a file appears |
   | `acpagent` `TestModelsForVendorUsesTheSavedCatalog` | `ListModelsFor("vendor")` over a saved catalog with groups returns that vendor's options, `Source` `saved` | `ListModelsFor` given the static list: the vendor's options are missing |
   | `httpagent` `TestALiveListIsSavedBeforeTheMerge` | a dialect stub, with a static list and no model providers, answering one live option: the saved file holds only the live option, `Source` `live` | the merged catalog saved: two options |
   | `httpagent` `TestAFailedListServesTheSavedCatalog` | the stub answering an error, with a saved catalog, lists it with `Source` `saved` and the configured default | `return static` kept: `Source` `static` |
   | codex `TestAnUnavailableEngineServesTheSavedCatalog` | a provider whose engine cannot start, with a saved catalog, lists it, `Source` `saved` | `fallbackCatalog` returns the empty static: no options |
   | codex `TestALiveModelListIsSaved` | a provider whose live answer, through the same path `ListModels` takes, has two models, saves them | the save removed: no file |
   | daemon `TestRunBaseCarriesTheSavedCatalogs` | `runBase` with `Paths.CatalogDir` set has a non-nil store, and `buildBase` has none | `runBase` drops the store: nil |
   | daemon `TestInspectProvidersWritesNoSavedCatalog` | with `Paths` in a temp dir and a grok section whose `bin` is missing, `InspectProviders`'s grok lists `Source` `static`, and `CatalogDir` does not exist | `InspectProviders` uses `runBase`: the directory appears |

   * The httpagent stub reaches the save point the way kilo's own catalog tests do. If no harness
     reaches `ListModels` without a real engine, the stub sits behind the `liveCatalog` fetch, and
     the step records which.
   * The codex live-path test may need the save moved into a helper that `ListModels` and the test
     share. The step records the shape chosen.
8. **The existing suites pass:** `TestLiveCatalogIsNotPaddedWithTheFloor`,
   `TestModelOptionBecomesTheCatalog`, kilo's catalog tests, codex's `model_list_test.go`, and
   `TestNoProviderIsNamedInAcpagent`.
9. **Gates:** `make race` on this host, the Linux test host's `go test -race ./...`, and
   `make ci-windows` on the Windows test host (C3).
10. **Stage** after `make pre-add-check`, with P2's execution record.

### P3 — The phone's `saved` source (D2's phone side)

Files:

* `apps/mobile/lib/data/protocol/picker.dart`
* `apps/mobile/test/picker_test.dart`

Steps:

1. **`PickerSource.saved`.** Add the value to the enum (`picker.dart:19-50`). `parse` maps
   `'saved'` to it, and `label` returns `'Saved catalog'`. The doc comment cites MADR 0235 D2. No
   other file switches on `PickerSource`: the only `switch` is in `picker.dart`. Uses elsewhere
   name `live`, `staticSource` or `unknown`, and are unchanged.
2. **Tests, seen failing first:**
   * `PickerSource.parse('saved')` is `saved`, and its label is `'Saved catalog'`. Scratch break:
     the `parse` case removed. Expected: `Expected: PickerSource:<PickerSource.saved>`,
     `Actual: PickerSource:<PickerSource.unknown>`.
   * A `PickerCatalog` parsed from JSON with `"source":"saved"` has that source.
   * `parse('unknown-value')` stays `unknown`, with an empty label, as an older daemon's phone
     already handles a newer daemon's unknown value (D2).
3. **Gates:** `make preflight` (`flutter analyze`, `flutter test`, `dart format`), and `dart format`
   on each staged file.
4. **Stage,** with P3's execution record.

### P4 — The SDK, grok's floor, and 0185's note (D4, D5)

Files:

* `go.mod`, `go.sum`
* `internal/provider/grok/grok.go`
* `internal/provider/grok/grok_test.go`
* `internal/provider/grok/sdk_boundary_test.go` (new)
* `internal/provider/acpagent/acpagent.go`
* `internal/provider/acpagent/catalog_test.go`
* `docs/decisions/0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md`

Steps:

1. **The dependency.**
   * `go get github.com/maccavelli/go-llmprovider-sdk@v1.27.0`, then `go mod tidy`.
   * A script on a scratch copy compares `go list -m all` before and after. Expected: exactly one
     line added, `github.com/maccavelli/go-llmprovider-sdk v1.27.0`, and `go.mod` gains only that
     `require`.
   * `govulncheck`, inside `make pre-add-check`, is clean.
2. **`acpagent.Spec.StaticDefaultModel string`** (the MADR's correction). Document it: the static
   catalog's default when no model is configured. In `staticModelCatalog` (`acpagent.go:513-516`),
   `def := p.cfg.Model`, and when that is empty, `def = p.spec.StaticDefaultModel`.
   * `TestTheStaticDefaultModelIsPreselected` in `catalog_test.go`: a Spec with static options
     `vendor-a`, `vendor-b` and `StaticDefaultModel: "vendor-b"` gives `DefaultIDs` `[vendor-b]`,
     and a configured `vendor-a` wins. Scratch break: the fallback line removed. Expected:
     `DefaultIDs []`.
3. **grok's floor.** In `grok.go`, replace the literal `staticModels` (`grok.go:18-31`) with
   `var staticModels = floorModels()`:

   ```go
   func floorModels() []picker.Option {
   	ids := catalog.SortByRank(llmprovider.ProviderGrok, catalog.Static(llmprovider.ProviderGrok), nil)
   	out := make([]picker.Option, 0, len(ids))
   	for _, id := range ids {
   		out = append(out, picker.Option{ID: id, Label: id, Group: "xai"})
   	}
   	return out
   }
   ```

   * The comment above it says:
     * this is the last fallback, after a saved catalog (MADR 0235 D1, D4);
     * it is the SDK's list in the SDK's rank order;
     * labels are ids because grok's live list names models by id;
     * `Static(grok)`'s own first id is not the default.

     It drops "Measured 2026-08-12 against grok 1.0.3", and keeps the AllowCustom sentence.
   * `spec` gains `StaticDefaultModel`, set to the first floor id. `floorModels` must not return
     empty: a test asserts it.
4. **grok's tests,** each seen failing first:

   | Test | Asserts | Scratch break, and expected failure |
   | :--- | :--- | :--- |
   | `TestStaticModelsFloor` (rewritten, `grok_test.go:58-74`) | the ids are `grok-4.7`, `grok-4.6`, `grok-4.5`, `grok-4.3`, `grok-build-0.1`; each `Label` equals its `ID`; each `Group` is `xai`; they equal `SortByRank(grok, Static(grok), nil)`; `spec.StaticDefaultModel` is `grok-4.7`; the retired ids `grok-code-fast-1`, `grok-4` and `grok-build` are absent | `floorModels` uses `Static` unsorted: `got [grok-4.6 grok-4.5 …]` |
   | `TestTheFloorIsInTheKnownGoodCapture` | every floor id is a `modelId` in `testdata/wire/<KnownGoodVersion>/frames.jsonl`'s `availableModels`, read the way `version_test.go:18` names the directory; the capture has at least one such list | a planted `grok-9-planted` appended to the floor: the test names it |
   | `TestNoLabelComesFromTheSDK` | no floor option's `Label` or `Description` equals `catalog.LabelName`, `LabelNote` or `Label` for its id, where those differ from the id | the label taken from `LabelName`: `Grok 4.7` reported |
   | `TestSDKIsImportedOnlyByGrok` (`sdk_boundary_test.go`) | over every `.go` file of the module, outside `testdata` and `vendor`, parsed with `go/parser`: no file outside `internal/provider/grok` imports a `github.com/maccavelli/go-llmprovider-sdk` path. Inside it, the only imports are `…/llmprovider` and `…/llmprovider/catalog`, and the only selectors used are `catalog.Static`, `catalog.SortByRank` and `llmprovider.ProviderGrok`. The test also checks it scanned more than 100 files | two copies: one where `internal/picker` imports `catalog`, one where `grok.go` calls `catalog.Label`. Each is named in the failure |

5. **`TestNoProviderIsNamedInAcpagent` passes.** `StaticDefaultModel` names no vendor.
6. **The size.** Build `./cmd/mcremote` with `-trimpath` from scratch copies of the P3 commit and
   of the P4 tree. Record both sizes. Expected growth: about 329,040 bytes (MADR F5). A growth over
   10% above that is recorded as a deviation before staging.
7. **0185's note (D5).**
   * Under 0185's title, add a dated note: "Superseded in part 2026-11-06 by
     0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md D5. The
     'No dependency' bullet, and the Confirmation's rejection of any go-llmprovider-sdk module, give
     way to one module, two functions (`Static`, `SortByRank`) and one package
     (`internal/provider/grok`). Everything else here stands."
   * Annotate the bullet and the Confirmation sentence in place, pointing at the note. Their text
     stays.
   * 0185 stays `accepted`.
8. **Gates:**
   * `make race` on this host, the Linux test host's `go test -race ./...`, and `make ci-windows`
     on the Windows test host (C3);
   * `python3 scripts/check_records.py --check-all`, the `redact` preview, and the identifier scan
     on 0185 (C7).
9. **Stage** after `make pre-add-check`, with P4's execution record.

### P5 — Confirm live, and close the pair

Files:

* `internal/provider/grok/live_saved_catalog_test.go` (new, `//go:build live_grok`)
* this pair
* `docs/README.md`, regenerated

Steps:

1. **`TestLiveSavedCatalogStandsInForAMissingGrok`** (C3, C6). It uses `isolateGrokHome`
   (`live_auth_test.go:20`) and unsets `XAI_API_KEY`.
   1. A store on `t.TempDir()`. Provider A has the real `grok` and the store. `ListModels` returns
      `Source` `live` and at least one option. For the isolated, signed-out home that is grok's
      compiled-in pair (MADR F2).
   2. After `Flush`, the store's file exists, and is mode `0600` off Windows.
   3. Provider B has a `Bin` that does not exist and a new `Open` on the same directory.
      `ListModels` returns `Source` `saved` and A's ids, in A's order.
   4. With the file removed, provider C, on a new store, returns `Source` `static`, the floor's
      ids, and `DefaultIDs` `[grok-4.7]`.

   Seen failing first: on a scratch copy with the `applyInitializeResponse` save removed, step 3
   gets `Source` `static`.

   Run:
   `go test -tags live_grok -count=1 -v -run '^TestLiveSavedCatalogStandsInForAMissingGrok$' ./internal/provider/grok/`,
   quoted as written, on this host.
2. **The scratch daemon and the phone** (C3; the MADR's Confirmation, "Live").
   1. A scratch directory in the session scratchpad holds a `config.yaml` with grok enabled and its
      real `bin`.
      `mcremote --config <scratch>/config.yaml serve --data-dir <scratch>/data --listen-port <free port> --tls=false`
      starts an instance whose instance key, and so whose `CatalogDir`, differ from the host
      daemon's. Before starting it, `mcremote --config <scratch>/config.yaml paths --data-dir <scratch>/data`
      confirms the data directory, and so the instance key, differ from the host daemon's.
   2. Before pairing, back up the Android emulator's pairing. It keeps one host. Then pair it to the
      scratch instance by pasted URI.
   3. Open the create-session model picker for grok. Expected: "Live catalog". The instance's
      `CatalogDir` holds `grok.json`.
   4. Stop the instance, set grok's `bin` to a path that does not exist, and start it again.
      Expected: the picker shows the same models, labelled "Saved catalog".
   5. Stop it, remove `grok.json`, and start it. Expected: "Offline catalog", the five floor ids,
      `grok-4.7` preselected.
   6. Each state is captured as a host-side screenshot. Then restore the emulator's pairing, and
      remove the scratch directory.
   * The host daemon, the real grok install and its home are not touched. No prompt is sent.
3. **Close the pair.**
   * This PLAN moves to `complete`, with each phase's execution record, including every failure
     seen first and every deviation.
   * The MADR's Confirmation items are checked off against the records, with the evidence.
   * Regenerate the index with `python3 scripts/check_records.py --write-index`. Then run
     `--check-all`, markdownlint on the changed records, the `redact` preview and the identifier
     scan. Stage, with the live test file after `make pre-add-check`.

## Verification

| Check | Phase | Expected |
| :--- | :--- | :--- |
| P1's eleven store tests, each on its scratch break | P1 | each fails as its row says |
| The same, on the tree | P1 | pass |
| `CatalogDir`, `MaxOptions`, and `"source":"saved"` tests, on their breaks and on the tree | P1 | fail, then pass |
| P2's eleven provider and daemon tests, on their breaks and on the tree | P2 | fail, then pass |
| `TestNoProviderIsNamedInAcpagent` | P2, P4 | passes |
| The existing catalog suites (acpagent, kilo, codex) | P2 | pass, unchanged |
| The phone's `saved` tests, on the break and on the tree | P3 | fail, then pass |
| `make preflight` | P3 | passes |
| `go list -m all` gains only the SDK, at v1.27.0 | P4 | one line |
| `TestTheStaticDefaultModelIsPreselected` and grok's four tests, on their breaks and on the tree | P4 | fail, then pass |
| Binary growth | P4 | about 329,040 bytes |
| `make race`, the Linux test host's `go test -race ./...`, `make ci-windows` | P1, P2, P4 | pass |
| `govulncheck` in `make pre-add-check` | every Go phase | clean |
| `TestLiveSavedCatalogStandsInForAMissingGrok`, on its break and on the tree | P5 | fails at step 3, then passes |
| Scratch daemon and emulator: live, then saved, then offline with `grok-4.7` preselected | P5 | as listed |
| `check_records.py --check-all`, markdownlint, `redact` preview, identifier scan | every phase with a record | clean |

## Rollout and Rollback

* **Rollout.**
  * The daemon change ships in the next release. On first start the saved layer is empty, so
    pickers behave as today until each agent answers once. From then on, an agent that cannot
    answer shows its last list.
  * grok's offline list becomes the five SDK models with `grok-4.7` preselected, where it was two,
    with nothing preselected.
  * The phone change ships in its next release, in either order with the daemon. An older phone
    shows no badge for `saved` (D2). A newer phone with an older daemon never receives it.
  * The release notes say that the model picker remembers each agent's last model list per host,
    in the cache directory, and labels it "Saved catalog".
* **Rollback.**
  * Revert P2 to stop saving and using saved catalogs. Files already written stay in the cache
    directory, unread. They hold no credential, and removing
    `<CacheDir>/instances/<InstanceKey>/catalogs/` is safe at any time.
  * Revert P4 to return grok's two-model floor and drop the SDK from `go.mod`. 0185's note then
    needs a dated line saying the dependency was removed.
  * P1 and P3 are inert without P2. Reverting them alone changes nothing a user sees.
  * Every phase reverts on its own, in reverse order. P4 does not depend on P1 to P3.

## Execution record

The owner accepted the MADR on 2026-11-06, and approved this PLAN the same day ("Approved to
proceed. Tests are also approved"). That word covers every host and live run of C3.

### P1 — executed 2026-11-06

**What changed:**

* `picker.SourceSaved` (`internal/picker/picker.go`).
* `appdirs.Paths.CatalogDir`, set in `Resolve` and `WithDataDir` to
  `<CacheDir>/instances/<InstanceKey>/catalogs`.
* The new package `internal/picker/saved`: `MaxOptions`, `Open`, `Save`, `Load` and `Flush`, as
  step 3 describes.
  * `Save` encodes and compares on the caller's goroutine, and does no file I/O there.
  * One writer goroutine writes the newest queued catalog per provider.
  * On a provider's first write in a process, the writer reads the file on disk and leaves an
    equal one alone. So a restarted daemon does not rewrite what it finds.
  * `Load` returns a fresh decode each call, so callers never share option slices with the store.

**Verification:**

* The tests on the tree: `go test -race -count=1 ./internal/picker/... ./internal/appdirs/` passed
  three packages. The two `ws` tests passed with `TestCapCatalog*`.
* Fail-first (C4), `libs/ff_p1.py`. 15 experiments on scratch copies of the working tree, each
  copy removed after its run: "15/15 failed as expected; copies removed: True". The failures:

  | Test | Break | Failure seen |
  | :--- | :--- | :--- |
  | `TestSaveThenLoadRoundTrips` | `Load` always reports nothing | `no saved catalog after Save and Flush` |
  | `TestLoadIsInMemoryBeforeTheWrite` | `Save` only queues | `Load before the write = [], false; want [vendor-a], true` |
  | `TestOnlyANonEmptyLiveCatalogIsSaved` | the source check removed | `writes = 1, want 0` |
  | `TestAnEqualCatalogIsNotRewritten` | the comparison removed | `writes after two equal saves = 2, want 1` |
  | `TestTheLatestQueuedCatalogWins` | a queued catalog not replaced by a newer one | `file holds [b], true; want [c]` |
  | `TestABadFileIsNoFile` | the version check removed | `a bad file loaded: [a]` |
  | `TestTheCatalogIsCutAtMaxOptions` | the cut removed | `options = 501, truncated = false; want 500, true` |
  | `TestAProviderIDCannotNameAPath` | `Save`'s id check removed | `the parent holds 2 entries, want none` |
  | `TestAFailedWriteIsLoggedAndKeptInMemory` | the write error ignored | `the failed write was not logged at warn` |
  | `TestNilStoreIsInert` | `Load` dereferences a nil store | `panic: runtime error: invalid memory address or nil pointer dereference` |
  | `TestTheFileIsOwnerOnly` | the file written `0644` | `file mode = 644, want 600` |
  | `TestSaveDoesNotChangeTheCallersCatalog` | options not copied | `caller's option label = "a", want it untouched` |
  | `TestCatalogDirIsPerInstance` | `WithDataDir` does not set it | `WithDataDir: CatalogDir = "<…>/instances/<key a>/catalogs", want "<…>/instances/<key b>/catalogs"` |
  | `TestSavedCatalogsKeepTheTransportCap` | `MaxOptions` 499 | `saved.MaxOptions = 499, want the transport cap 500` |
  | `TestASavedCatalogIsSentAsSaved` | the constant spelled `"save"` | `reply = {…"source":"save"…}, want "source":"saved"` |

* `make race` on this host: exit 0, 49 packages `ok`, no other line but the command.
* The host gates, `leftovers/pins_hosts.py` over HEAD plus P1's seven files:
  * the Linux test host, `go test -race -count=1 -timeout=40m ./...` on go1.27.3 linux/amd64: exit 0,
    49 `ok` lines;
  * the Windows test host, the dry run then `make ci-windows` on go1.27.3 windows/amd64: exit 0,
    "ci-windows-local: ALL SELECTED CHECKS PASSED", 49 `ok` lines;
  * both scratch trees removed and their absence checked.
* `make pre-add-check` on the seven Go files: "7 file(s) clean (gofmt, golint, govulncheck)".

**Deviations, all 2026-11-06, none changing a decision:**

1. **The protocol test sits in `internal/ws/models_cap_test.go`,** as `TestASavedCatalogIsSentAsSaved`.
   `internal/protocol` has no test of `ModelsResultFromCatalog`; that file is the one that tests it,
   which step 5 allowed.
2. **The permission test's build tag is `unix`, not `!windows`,** matching
   `internal/appdirs/ensure_unix.go`, whose code it checks.
3. **`TestTheLatestQueuedCatalogWins` saves A, B and C,** and its break keeps the first queued
   catalog. The planned break, "writes each queued save in order", needs a queue the store does not
   have, so it could not be planted in one edit. The test still bounds the writes at two.
4. **One test was added,** `TestSaveDoesNotChangeTheCallersCatalog`. It guards the copy that keeps
   `Normalize` from writing into a provider's shared cached catalog.

### P2 — executed 2026-11-06

**What changed:**

* `provider.Build.SavedCatalogs`. The daemon's `runBase` is `buildBase` plus
  `saved.Open(cfg.Paths.CatalogDir, log)`. `Run` uses it, logs at info when there is no directory,
  and defers `Flush` before the spares' shutdown, so it runs after it. `InspectProviders` keeps
  `buildBase`.
* `SavedCatalogs` on `acpagent.Config`, `httpagent.Config` and codex's `Config`. Each of the four
  descriptors sets it from `b.SavedCatalogs`.
* **`acpagent`:**
  * `saveCatalog` runs after `reportEngineVersion` in `applyInitializeResponse`, and after the
    unlock in `SetModelCatalog`.
  * `fallbackCatalog` replaces the three `return static, nil` in `ListModels`.
  * The live-catalog comment says what a saved catalog is.
* **`httpagent`:** `ListModels` saves the live list before the static and merge branches, and
  `fallbackModels` replaces its error-path `return static, nil`.
* **codex:**
  * `fallbackCatalog` replaces the two engine-unavailable returns.
  * `withSavedCatalog`, applied to `collectModels`' result, saves a live list and replaces a static
    one.

**Verification:**

* The eleven tests on the tree pass. So do `TestNoProviderIsNamedInAcpagent` and the existing
  catalog suites, inside `make race`.
* Fail-first (C4), `libs/ff_p2.py`: "11/11 failed as expected; copies removed: True".

  | Test | Break | Failure seen |
  | :--- | :--- | :--- |
  | `TestAnInitializeCatalogIsSaved` | the initialize save removed | `saved after initialize: [], false; want [vendor-a vendor-b]` |
  | `TestASessionModelOptionIsSaved` | the `SetModelCatalog` save removed | `saved after a session's model option: {…Options:[]…}, false` |
  | `TestAMissingAgentServesTheSavedCatalog` | `fallbackCatalog` returns static | `missing agent lists "static" [static/one]; want saved [vendor-a vendor-b]` |
  | `TestNoSavedCatalogServesTheStaticOne` | the static list saved on the fallback path | `the static fallback wrote to the store: <nil>` |
  | `TestModelsForVendorUsesTheSavedCatalog` | `ListModelsFor` given the static list | `models for g1: "static" []; want saved [g1/a g1/c]` |
  | `TestALiveListIsSavedBeforeTheMerge` | the merged catalog saved | `saved = [live-one static-one], true; want only the live [live-one]` |
  | `TestAFailedListServesTheSavedCatalog` | `return static` kept | `ListModels = "static" [static-one]; want saved [kept-one kept-two]` |
  | `TestAnUnavailableEngineServesTheSavedCatalog` | `fallbackCatalog` returns the empty static | `ListModels = "static" []; want saved [gpt-a gpt-b]` |
  | `TestALiveModelListIsSaved` | the save removed | `saved = [], false; want [gpt-5.6-sol gpt-5.6-terra]` |
  | `TestRunBaseCarriesTheSavedCatalogs` | `runBase` drops the store | `runBase has no saved-catalog store` |
  | `TestInspectProvidersUsesNoSavedCatalog` | `InspectProviders` uses `runBase` | `doctor's grok lists a "saved" catalog, want static: it read the saved one` |

* `make race` on this host, over P1 and P2: exit 0, no line but the command and `ok` lines.

**Deviations, all 2026-11-06, none changing a decision:**

1. **`TestInspectProvidersWritesNoSavedCatalog` became `TestInspectProvidersUsesNoSavedCatalog`.**
   With a missing binary no provider has a live answer to save, so "the directory does not appear"
   would pass under the planned break too. The test seeds a saved grok catalog instead, and asserts
   that doctor's grok lists `static`. Doctor and the daemon share one store type, so a provider
   with no store neither reads nor writes.
2. **The `httpagent` stub sits behind `liveCatalog`,** as step 7 allowed. The tests install an engine
   already up (`p.eng`, as `provider_test.go:240` does) and a dialect whose `ListModelsLive` answers
   from memory.
3. **codex's live-path test calls `withSavedCatalog`,** the step `ListModels` applies to
   `collectModels`' result, as step 7 allowed. It feeds `collectModels` the existing `codexLike`
   stub.

### P3 — executed 2026-11-06

**What changed:** `PickerSource.saved` in `apps/mobile/lib/data/protocol/picker.dart`. `parse` maps
`'saved'` to it, `label` is `'Saved catalog'`, and the enum's doc comment cites MADR 0235 D2.

**Verification:**

* `apps/mobile/test/picker_test.dart` gained two tests: "parses a saved catalog and labels it",
  and "an unknown source shows no badge". `flutter test test/picker_test.dart`: "All tests
  passed!", 9 tests.
* Fail-first (C4), `libs/ff_p3.py`, on a scratch copy of `apps/mobile` with build output and the
  signing key file left out, and the `parse` case removed. The test failed with
  `Expected: PickerSource:<PickerSource.saved>` and `Actual: PickerSource:<PickerSource.unknown>`.
  The copy was removed.
* `dart format` on both files: "Formatted 2 files (0 changed)".
* `make preflight` over P1 to P3: "✅ preflight passed". That covered:
  * records, markdownlint, gofmt, `go mod tidy`, `go vet` over every build tag, staticcheck on three
    GOOSes, `go test -race`, and the release build;
  * `dart format` ("Formatted 231 files (0 changed)"), `flutter analyze` ("No issues found!") and
    `flutter test` ("All tests passed!", 1,515).

### P2's host gates — 2026-11-06

* **First run** (`pins_hosts.py`, HEAD plus P1's and P2's 24 files):
  * the Windows test host: `make ci-windows`, "ALL SELECTED CHECKS PASSED", 49 `ok` lines;
  * the Linux test host: exit 1, with one failure in a test P2 does not touch:
    `TestNonBlockingProbeCoalescesConcurrentCallers` (`internal/provider/codex/nonblocking_reality_test.go:107`),
    "no probe ran at all", 48 `ok` lines.
* **Is it P2's?** That test checks codex's background credential probe. Its in-flight flag is
  package-wide (`store_reality.go:309-313`), so an earlier test's probe still in flight makes all
  20 calls return without starting one, which is this failure. `libs/host_codex.py` ran it on the
  Linux test host in two scratch trees, HEAD alone and HEAD plus P2:
  * the test alone, `-race -count=10`: exit 0 in both;
  * the codex package, `-race -count=3`: exit 0 in both.

  It did not reproduce in 13 runs on either tree. It is a timing flake, not P2's. It is left as it
  is: fixing it is outside this PLAN.
* **Second run,** the Linux test host only, the same 24 files: exit 0, 49 `ok` lines. Both scratch
  trees of every run were removed and their absence checked.

### P4 — executed 2026-11-06

**What changed:**

* `go.mod` gains one line, `github.com/maccavelli/go-llmprovider-sdk v1.27.0`. `go.sum` gains its
  two hashes.
* `acpagent.Spec.StaticDefaultModel`, used by `staticModelCatalog` when no model is configured.
* `grok.go`: `staticModels` is `floorModels()`, which is `SortByRank(grok, Static(grok), nil)` as
  options with the id as label and group `xai`. `spec.StaticDefaultModel` is `floorDefault()`, the
  floor's first id. The comment names MADR 0235 D1 and D4, and drops the 2026-08-12 measurement.
* `0185-MADR-…`: a dated note under the title, and in-place annotations on the "No dependency"
  bullet and the Confirmation. 0185 stays `accepted`.

**Verification:**

* The tests on the tree:
  * `TestStaticModelsFloor` (rewritten), `TestTheFloorIsInTheKnownGoodCapture`,
    `TestNoLabelComesFromTheSDK` and `TestSDKIsImportedOnlyByGrok` pass;
  * `TestTheStaticDefaultModelIsPreselected` passes;
  * `TestNoProviderIsNamedInAcpagent` and `TestLiveCatalogIsNotPaddedWithTheFloor` still pass.
* Fail-first (C4), `libs/ff_p4.py`: "6/6 failed as expected; copies removed: True".

  | Test | Break | Failure seen |
  | :--- | :--- | :--- |
  | `TestTheStaticDefaultModelIsPreselected` | the fallback line removed | `static default = [], want [vendor-b]` |
  | `TestStaticModelsFloor` | `Static` unsorted | `floor = [grok-4.6 grok-4.5 grok-4.7 grok-4.3 grok-build-0.1], want [grok-4.7 …]` |
  | `TestTheFloorIsInTheKnownGoodCapture` | a planted id | `floor id "grok-9-planted" is not among the models grok 1.0.70 offered` |
  | `TestNoLabelComesFromTheSDK` | labels from `LabelName` | `grok-4.7 label "Grok 4.7" comes from the SDK's LabelName` |
  | `TestSDKIsImportedOnlyByGrok` | `internal/picker/order.go` imports `catalog` | `internal/picker/order.go imports …/llmprovider/catalog; only internal/provider/grok may` |
  | `TestSDKIsImportedOnlyByGrok` | `grok.go` calls `catalog.Label` | `internal/provider/grok/grok.go uses catalog.Label; grok may use only …` |

* **The module graph** (`libs/size_now.py`, scratch copies before and after P4): `go list -m all`
  gains exactly one line, `github.com/maccavelli/go-llmprovider-sdk v1.27.0`, 95 modules to 96.
* **The size,** `go build -trimpath ./cmd/mcremote`: 39,751,826 bytes before P4 and 40,048,098
  after. That is 296,272 bytes (0.75%), under the 329,040 expected. For the record, P1 to P3 grew
  the binary 68,752 bytes over HEAD's 39,683,074.
* `make race` on this host over P1 to P4: exit 0, 49 packages `ok`, no other line.
* The host gates over HEAD plus P1 to P4: the Linux test host, exit 0, 49 `ok` lines; the Windows
  test host, "ALL SELECTED CHECKS PASSED", 49 `ok` lines; trees removed.
* `make pre-add-check` on P2's and P4's 21 Go files: "21 file(s) clean (gofmt, golint,
  govulncheck)".

### P5 — executed 2026-11-06, after two deviations

**Step 1, done.** `TestLiveSavedCatalogStandsInForAMissingGrok` passed on grok 1.0.70, with grok's
home isolated and no key: "grok's live catalog: [grok-4.6 grok-4.5], default [grok-4.6]". That is
the compiled-in pair of MADR F2, saved and then served as `saved`, then the floor with
`[grok-4.7]` once the file was removed.

* Fail-first, `libs/ff_p5.py`, the initialize save removed: it failed at step 2, not step 3 as the
  PLAN said, because the file check comes first: `no saved catalog after the live answer: stat
  <tmp>/catalogs/grok.json: no such file or directory`.

**Step 2, partly done, then stopped.**

* The app data was backed up: 20 files in `shared_prefs`, `databases`, `files` and `app_flutter`.
  The tree's debug APK was installed.
* A scratch instance (`libs/p5_daemon.py`) had its own XDG directories and data directory, with
  instance key `6c1c43e14a1bc83c`, and grok only. The emulator paired with it by pasted URI.
* **State 1, live:** the create-session model picker for grok showed "Live catalog". Its options
  were `grok-4.7`, `grok-4.7-build-fast`, `grok-4.6` (current) and `grok-4.5`, named "Grok 4.7",
  "Grok 4.7 Fast", "Grok 4.6" and "Grok 4.5": the owner's signed-in account at initialize, with no
  session created. The instance's `grok.json` was mode `0600`, `engine_version` 1.0.70, with
  those four ids and default `grok-4.6`.
* **State 2 could not be reached as planned.** With `providers.grok.bin` set to a missing path, the
  app lists grok as "grok (not installed on host)" and does not let it be chosen
  (`apps/mobile/lib/features/sessions/sessions_screen.dart:812-819`, `enabled: p.ready`). So its
  model picker never asks for a catalog. A first look showed "Live catalog" after the restart, but
  that was the dialog's own cache (`sessions_screen.dart:538`, `cache[key] = pending`), from before
  the restart. It was not the daemon's answer.
* The scratch instance was stopped, and the app's data was restored and checked file by file:
  "20 files wanted; missing 0, changed 0, extra 0". The app reconnected to the host's own daemon.
  The scratch directory is kept for the rerun.

**Deviation 1, 2026-11-06 — the missing-binary state is not reachable from the phone.** The MADR's
Confirmation ("Live") and step 2.4 assume the phone can open a model picker for a provider whose
binary is missing. It cannot.

* **Resolution (owner, 2026-11-06, as recommended):** steps 2.4 and 2.5 point grok's `bin` at a
  scratch stub that exists and exits before `initialize`. The missing-binary case stays confirmed at
  the provider by step 1.
* ~~4. Stop the instance, set grok's `bin` to a path that does not exist, and start it again.~~
  Superseded by this resolution.
* The MADR records it in "Amendment 2026-11-06: the floor takes the SDK's names, and the phone
  check uses a failing grok".

**Deviation 2, 2026-11-06 — a fact behind D4 and Q1 is wrong.** The MADR says "grok's own live list
names each model by its id" (F3, D4, Q1). That holds for the keyed 1.0.70 capture, where `name`
equals `modelId`. A signed-in account's live list carries display names, "Grok 4.7" and so on. So
for a signed-in user, the floor's `grok-4.7` becomes "Grok 4.7" at grok's first answer, which is the
change Q1's answer meant to avoid.

* **Resolution (owner, 2026-11-06, as recommended):** the floor's labels are the SDK's names,
  `catalog.LabelName`, with no note. Recorded in the same MADR amendment, which changes D4, D5, Q1
  and the Confirmation. Files added to P4's scope: none. P4's `grok.go`, `grok_test.go`,
  `sdk_boundary_test.go` and 0185's note change.
* **What changed in P4:**
  * `floorModels` sets `Label: catalog.LabelName(grok, id)`.
  * `TestStaticModelsFloor` expects "Grok 4.7", "Grok 4.6", "Grok 4.5", "Grok 4.3" and
    "Grok Build 0.1".
  * `TestNoLabelComesFromTheSDK` became `TestFloorLabelsAreTheSDKNames`: each label equals
    `LabelName`, and no description is set.
  * The boundary test allows `catalog.LabelName`.
  * 0185's note says three functions.
* **Fail-first,** `libs/ff_p4b.py`: "5/5 failed as expected; copies removed: True".

  | Test | Break | Failure seen |
  | :--- | :--- | :--- |
  | `TestStaticModelsFloor` | labels back to ids | `floor labels = ["grok-4.7" …], want ["Grok 4.7" …]` |
  | `TestFloorLabelsAreTheSDKNames` | labels back to ids | `grok-4.7 label "grok-4.7", want the SDK's name "Grok 4.7"` |
  | `TestFloorLabelsAreTheSDKNames` | the note as description | `grok-4.7 description "frontier model", want none: the SDK's notes are not used` |
  | `TestSDKIsImportedOnlyByGrok` | `grok.go` calls `catalog.LabelNote` | `… uses catalog.LabelNote; grok may use only catalog.Static, catalog.SortByRank, catalog.LabelName and llmprovider.ProviderGrok` |
  | `TestSDKIsImportedOnlyByGrok` | `grok.go` calls `catalog.Label` | `… uses catalog.Label; …` |

* **Gates after the change:**
  * `make race` on this host: exit 0, no line but the command and `ok` lines;
  * `make pre-add-check` on the four grok files: exit 0;
  * the live test, rerun: `ok`;
  * the host gates over HEAD plus every changed file: the Linux test host, exit 0, 49 `ok` lines;
    the Windows test host, "ALL SELECTED CHECKS PASSED", 49 `ok` lines; trees removed.

**Step 2, rerun with the failing grok (deviation 1's resolution):**

* The app data was backed up again (20 files). The scratch instance was rebuilt from the tree,
  with `providers.grok.bin` set to the stub, and the emulator paired with it again.
* **State 2, saved.** grok listed as ready. Its model picker showed **"Saved catalog"**, with the
  four models of state 1 under grok's names, "Grok 4.7", "Grok 4.7 Fast", "Grok 4.6" (preselected)
  and "Grok 4.5". Each kept its reasoning levels. While loading, the sheet showed "Offline catalog"
  for a moment: that is the app's placeholder before any reply, the same as in state 1.
* **State 3, offline.** With the instance stopped, `grok.json` removed and the instance restarted,
  a new dialog's picker showed **"Offline catalog"**, with "Grok 4.7" (preselected), "Grok 4.6",
  "Grok 4.5", "Grok 4.3" and "Grok Build 0.1". The failed start saved nothing: "no grok.json
  saved".
* **Cleanup:**
  * the app's data was restored and checked: "20 files wanted; missing 0, changed 0, extra 0";
  * the app reconnected to the host's own daemon;
  * the scratch instance's directory was removed ("p5d removed: True"), with its device token and
    pair code;
  * the backup archive was deleted, and the emulator was shut down.

  The screenshots stay in the session scratchpad. They are not committed, since they show the
  host's name.

### The MADR's Confirmation, checked off — 2026-11-06

| Item | Evidence |
| :--- | :--- |
| D1, the store | P1's eleven store tests and their failures |
| D1, the providers | P2's eleven tests and their failures |
| D2 | `TestASavedCatalogIsSentAsSaved`; the phone's two `picker_test.dart` tests; `make preflight` |
| D4, as amended | `TestStaticModelsFloor`, `TestFloorLabelsAreTheSDKNames`, `TestTheFloorIsInTheKnownGoodCapture`, `TestSDKIsImportedOnlyByGrok`, `TestTheStaticDefaultModelIsPreselected` |
| D4, a live catalog keeps its order | no code reorders one: `ListModels` returns the agent's list as before, and `TestLiveCatalogIsNotPaddedWithTheFloor` still passes. No new test was written for it |
| The dependency | `go list -m all` gains only the SDK; `govulncheck` clean in every `make pre-add-check` |
| Live | step 1's live test, and step 2's three phone states |
| Gates | `make race`; the Linux test host's `go test -race ./...` and `make ci-windows`, at P1, P2 and P4 |

### Not done, and why

* **No test asserts that a live catalog with the 4.20 ids first keeps that order** (the MADR's D4
  Confirmation item). Nothing in the change sorts a live or saved catalog, so such a test would only
  restate `ListModels`. The item is met by the code as written, and is left without a test of its
  own.
* **The phone's "Offline catalog" placeholder** shows for a moment while a reply loads, in every
  state. It is the app's existing behaviour, and is not changed here.
* **The flaky `TestNonBlockingProbeCoalescesConcurrentCallers`** is not fixed here.
* **The two floor ids a signed-in account does not offer** (`grok-4.3`, `grok-build-0.1`) are left,
  as the MADR's amendment records.

The PLAN is complete. Commits are the owner's: see the handoff.
