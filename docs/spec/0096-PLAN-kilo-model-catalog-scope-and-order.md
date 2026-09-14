<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Implement scoping the model catalog to the session's model provider, and ordering it deterministically

Associated MADR: [0096-MADR-kilo-model-catalog-scope-and-order.md](./0096-MADR-kilo-model-catalog-scope-and-order.md)

## Goal

`kilo-auto/frontier` — and every other Kilo Gateway model — is reachable
from `/model` on a Kilo session, in a stable order, with truncation
reported honestly when it happens.

Acceptance, measured through the daemon's own `models.list` (the numbers
in the MADR's *Context* table are the before-state):

* `models.list{session_id}` on a Kilo session returns the **kilo** model
  provider's catalog (295 options on the 7.4.22 probe host), containing
  `kilo/kilo-auto/frontier`.
* `models.list{}` (provider default) contains at least one
  `kilo/kilo-auto/*` router.
* Two consecutive `ListModelsForLive("kilo")` calls return identical
  option order.
* Any reply that dropped rows carries `truncated: true`.

## Scope

In scope:

* `internal/provider/httpagent` — session-scoped catalog (D1).
* `internal/provider/kilo/catalog_live.go` — default-provider ordering
  (D2), cap alignment (D3), deterministic per-provider order (D4),
  `recommendedIndex` ranking (D5), comment corrections (D7).
* `internal/provider/kilo/live_*_test.go` — the standing pin (D8).

Out of scope: the picker widget, the phone's `/model` intercept,
`/api/model` adoption (D6, deferred), and the `execute_pipeline` MCP
schema (worked around in Kilo config, tracked separately).

## Implementation Steps

1. **D4 first — deterministic order within one model provider.** In
   `modelsOf`, sort the built options by `Option.ID` before handing them
   to `picker.OrderModels`. This is the smallest change and every later
   step's assertions depend on it, so land and test it on its own. Add a
   unit test that builds a `providerEntry` whose models all share one
   `release_date` and asserts a fixed order across repeated calls.

2. **D5 — rank the recommended routers.** Extend `providerModel` with
   `RecommendedIndex *int` (pointer, so index 0 stays distinct from
   absent) and carry it into `Option.Meta` under a new
   `picker.MetaRecommendedIndex` key. Do the ranking **in
   `picker.OrderModels`**, not in `modelsOf`: that function documents
   itself as the only place model ordering is interpreted, and a second
   ordering rule in a provider would be the drift it exists to prevent.
   Insert the comparison between the current-model band and the date
   comparison — an index outranks recency, an absent index loses to every
   present one and then falls through to date, and an unparsable one is
   treated as absent so a change in the engine's field type degrades
   ordering rather than silently ranking first. Unit-test in both
   packages, with the four real 7.4.22 indices (frontier 0, balanced 1,
   efficient 2, free 3) plus an unindexed model.

3. **D3 — report truncation where it happens.** Add `Truncated bool` to
   `picker.Catalog`; set it in `ListModelsLive` when
   `capDefaultCatalogModels` removed rows; carry it through
   `protocol.ModelsResultFromCatalog`; leave `ws.capCatalogOptions` free
   to OR its own truncation on top. Leave `maxDefaultCatalogModels` at
   150 — raising it to 500 fails `TestDefaultCatalogFitsTheFrame` at
   101,136 bytes against its 32 KB budget. Test both directions: a capped
   catalog reports `truncated: true`, and one that fits does not.

4. **D2 — lead with the engine's default model provider.** In
   `ListModelsLive`, before the concatenation loop, reorder
   `conn.Providers` so the provider named by `conn.Default["kilo"]` leads
   (falling back to `d.defaultModelProvider` when the engine reports no
   Gateway default, e.g. logged out). Preserve engine order among the
   rest. Do not sort globally — the picker renders a header per model
   provider (0043 D2). Unit-test with a fixture whose provider order puts
   `kilo` last.

5. **D1 — session-scoped catalog on `httpagent`.** Implement
   `ModelCatalog(ctx, scope)` on `httpagent`'s session:
   * `scope == provider.CatalogScopeProviders` → delegate to the dialect's
     `ModelProviderLister.ListModelProvidersLive`.
   * `scope == provider.CatalogScopeModels` → resolve the session's model
     provider from the provider half of its current model, falling back to
     the dialect's default model provider; delegate to
     `ListModelsForLive(thatProvider)`. With neither available, return the
     dialect's default catalog so the picker is never empty.
   * A dialect implementing neither lister keeps today's behaviour: return
     an error so `handleModelsList` falls through to the provider-wide
     answer.
   Add `var _ provider.ModelCatalogSession = (*session)(nil)`. Confirm the
   OpenCode dialect compiles against it unchanged — D1 is shared, not
   Kilo-only.

6. **D7 — correct the stale comments.** In `catalog_live.go`: the
   `/config/providers` size notes become 838 KB / 866 models across 9
   connected providers on kilo 7.4.22 (two places: the response type and
   `ListModelsForLive`'s "cheap endpoint" note); keep the `/provider`
   4.7 MB figure, which still holds; keep the 32 KB budget reference and
   name `TestDefaultCatalogFitsTheFrame` as what enforces it. Rewrite the
   `maxDefaultCatalogModels` comment to say plainly that the cap was never
   the bug — which rows survived it, and the silence about the rest, were.

7. **D8 — pin it live.** Add to `internal/provider/kilo/live_test.go` (or
   a new `live_catalog_test.go`, `//go:build live_kilo`):
   * default catalog contains ≥1 `kilo/kilo-auto/` option;
   * `ListModelsForLive("kilo")` called twice returns identical IDs in
     identical order;
   * `kilo/kilo-auto/frontier` ranks above a Kilo model that carries no
     `recommendedIndex`;
   * the session-scoped catalog for a live Kilo session contains
     `kilo/kilo-auto/frontier`.
   Skip cleanly (`t.Skip`) when `kilo` is not on `PATH`, matching the
   existing live tests.

8. **Wire the live target.** Add a `live-kilo` target to the `Makefile`
   next to `live-opencode` / `live-goose`, and name `-tags live_kilo` in
   AGENTS.md's *Tests* section alongside `live_grok` / `live_opencode`.
   Five `live_kilo` files already exist with no documented way to run
   them.

## Verification

Per step, then once at the end:

```bash
make pre-add-check                 # gofmt + golint + govulncheck (AGENTS.md)
make test                          # go test ./...
go test -race ./...                # or: make race
make live-kilo                     # new target; needs kilo on PATH, spends tokens
```

End-to-end, against a running daemon and engine — this is the acceptance
check, because every defect in this record was invisible to the unit
suite:

1. `kilo serve --hostname 127.0.0.1 --port <p>`; confirm
   `GET /global/health` reports `7.4.22`.
2. Start the daemon, pair a device, create a Kilo session pinned to
   `kilo/kilo-auto/frontier`.
3. Send `models.list` three ways — `{session_id}`, `{}`, and
   `{model_provider:"kilo"}` — and assert against the MADR's table: the
   session-scoped reply must now be the 295-option kilo catalog with
   `frontier_present=true`, not the 150-option cross-provider list.
4. Call the session-scoped route twice; assert identical option order.

Measured after-state on kilo 7.4.22, same host and method as the MADR's
before-table:

| request | options | groups | frontier | truncated |
| --- | --- | --- | --- | --- |
| `{session_id}` | 295 | kilo 295 | yes | false |
| `{}` (provider default) | 150 | kilo 150 | yes | **true** |
| `{model_provider: "kilo"}` | 295 | kilo 295 | yes | false |

The session-scoped reply also now echoes `model_provider: "kilo"` — the
scope actually applied — where it previously echoed the empty string it
was asked with. A turn on `kilo-auto/frontier` completes.

## Rollout and Rollback

Rollout is a plain binary release; there is no migration, no persisted
state and no protocol change — `ModelsResultPayload` already carries every
field used here, so an old phone against a new daemon simply receives a
better-scoped list, and a new phone against an old daemon sees exactly
today's behaviour.

Rollback is per-decision and independent, which is why the steps are
ordered smallest-blast-radius first:

* D1 rolls back by deleting the `ModelCatalog` method — `handleModelsList`
  already handles its absence (that fallback is the current bug's
  mechanism, so the path is proven).
* D2/D4/D5 roll back to the prior ordering with no data-shape change.
* D3 rolls back by dropping `picker.Catalog.Truncated`; the field is
  `omitempty`, so an old client never sees it and a new client treats its
  absence exactly as it treats `false` today.

Risk to watch after release: a Kilo release that drops
`recommendedIndex` degrades D5 to date-then-id ordering rather than
breaking the picker, and D8's test names the field, so the failure is a
red live test rather than a user-visible regression.
