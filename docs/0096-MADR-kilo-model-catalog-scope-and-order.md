---
status: accepted
date: 2026-08-16
decision-makers: Project Owner (scope and acceptance); Implementer (live probe)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Scope the model catalog to the session's own model provider, and order it deterministically

## Context and Problem Statement

A user tried to start a Kilo session on **`kilo-auto/frontier`** and every
turn failed. Driving the daemon and the engine (kilo 7.4.22,
`/opt/homebrew/bin/kilo`) against the real Kilo Gateway produced three
distinct problems that had been reported as one:

1. **The turn failure is upstream, not ours.** `kilo-auto/frontier` carries
   no `autoRouting` pool — it is `family: claude`, `prompt: anthropic`,
   `ai_sdk_provider: anthropic` — so every turn resolves to
   `anthropic/claude-sonnet-5`. The user's `magictools` MCP server
   advertises `execute_pipeline` with a **top-level `anyOf`** in its
   `inputSchema`, and the Anthropic Messages API rejects
   `oneOf`/`allOf`/`anyOf` at the top level of a tool `input_schema`. The
   engine forwards the schema verbatim, so the turn 400s before a token is
   produced. `kilo-auto/balanced` and `kilo-auto/efficient` route through
   15-model pools containing exactly one Anthropic model, so they fail the
   same way only when the router happens to pick it. This is not a
   frontier bug; it is *"any Anthropic-routed Kilo turn is broken"*, which
   frontier makes deterministic. Handled outside this record (see
   *More Information*).

2. **`kilo-auto/frontier` is not selectable from the model picker at all.**
   `ListModelsLive` concatenates the connected providers **in engine
   order** and pre-caps the result at `maxDefaultCatalogModels = 150`
   (`internal/provider/kilo/catalog_live.go`). On the probe host the engine
   order is google 38, huggingface 66, openrouter 351, anthropic 15, openai
   48, opencode-go 19, xai 12, opencode 62, **kilo 295** — 866 models, with
   Kilo Gateway *last*. The cap lands 150 rows inside `openrouter`, so every
   Kilo model is cut; only the engine default (`kilo-auto/balanced`)
   survives, force-swapped into the final slot by `capDefaultCatalogModels`.
   Measured through `models.list` on the daemon:

   | request | options | groups | frontier present |
   | --- | --- | --- | --- |
   | `{session_id}` (what the phone's `/model` sends) | 150 | google 38, huggingface 66, openrouter 45, kilo 1 | **no** |
   | `{}` (provider default) | 150 | identical | **no** |
   | `{model_provider: "kilo"}` | 295 | kilo 295 | yes |

   The third row proves the data is present and one scope field away. The
   phone cannot reach it because the session-scoped route falls through:
   `Manager.ModelCatalog` requires `provider.ModelCatalogSession`, which
   only `acphttp` and `fake` implement — **`httpagent` does not**. The ws
   handler catches that error, logs it at `Debug`, and answers the
   provider-wide default instead.

3. **The Kilo list is in a different random order on every call.** Kilo
   stamps **all 295** Gateway models with `release_date` equal to the
   current date (`2026-08-16` on the probe); every other connected provider
   carries real, varied dates. `picker.OrderModels` is a *stable* sort on
   that date, so all 295 tie and fall through to "source order" — which is
   Go **map iteration order** over `providerEntry.Models`. Three consecutive
   `ListModelsForLive("kilo")` calls differed in 293, then 294, of 295
   positions; `kilo-auto/frontier` appeared at index 48 on one run.

A second invariant is violated by (2). `maxDefaultCatalogModels = 150`
drops 716 of 866 rows while `ModelsResultPayload.Truncated` stays `false`,
which is exactly what [0043](./0043-MADR-model-selection.md) D4 forbids
("a catalog that quietly loses rows reads to a user as *my model does not
exist*"). The ws layer's own `capCatalogOptions` does set the flag
honestly — but at `maxCatalogOptions = 500` it never fires behind kilo's
lower pre-cap, so the row that admits the loss is written by a layer that
did not cause it and cleared by one that did.

The 150 itself is **not** the defect and is retained. It is the largest
count that keeps the serialized reply inside the 32 KB default-catalog
budget that `TestDefaultCatalogFitsTheFrame` enforces — a budget carried
by a test rather than a constant, which is why raising the cap to 500
during implementation immediately failed at 101,136 bytes. The bugs are
*which* 150 rows survive and that their survival is reported as
completeness.

Architectural scope: `internal/provider/kilo` catalogs, the `httpagent`
session's optional interfaces, and `internal/ws`'s truncation reporting.
Not the picker widget, not the phone's `/model` intercept, not other
providers' catalogs beyond what the shared `httpagent` change gives them.

## Decision Drivers

* A model the engine offers and the user names must be reachable from the
  picker. Anything else is a broken feature, not a long list.
* Truncation must never be silent (0043 D4). The flag exists; the code path
  that drops rows must be the one that sets it.
* A picker whose order changes between two openings is unusable for
  recognition, which is how people pick models.
* Wire-shape and size claims need live evidence, pinned by a `live_kilo`
  test (AGENTS.md) — the numbers in this record were measured, not assumed.
* Keep one frame under the relay's 1 MiB cap; do not trade a truncation bug
  for a dropped connection.
* Prefer fixing the shared `httpagent` host over a Kilo-only special case:
  OpenCode has the same session-scope gap for the same reason.

## Considered Options

* Option 1: Scope the session's catalog to its own model provider, and
  order deterministically from engine metadata (chosen)
* Option 2: Raise `maxDefaultCatalogModels` until Kilo fits
* Option 3: Sort the merged default catalog globally, newest-first
* Option 4: Move the whole catalog to the v2 `/api/model` endpoint

## Decision Outcome

Chosen option: **"Option 1: Scope the session's catalog to its own model
provider, and order deterministically from engine metadata"**, because it
is the only option that makes the *right* 295 models reachable rather than
making a wrong 866-model list longer, and because the session already
knows which model provider it is billing against — the picker was simply
never asking it.

Option 2 is rejected on measurement, not preference: raising the cap to
cover Kilo's position in engine order means shipping the full 866-model
cross-provider list, and it was tried during implementation — at 500
options the reply is 101,136 bytes against a 32 KB budget, which is the
oversized-frame failure 0043 D2 already rejected. It also still leaves the
user scrolling past 455 unrelated models to reach their own gateway.
Option 3 is
rejected because the picker renders a header per model provider; a global
sort interleaves providers under their own headings, and Kilo's uniform
same-day `release_date` makes "newest first" meaningless across providers
anyway. Option 4 is deferred, not rejected — `/api/model` is the better
long-term source (below) but replacing every catalog call is a larger
change than this defect needs.

* Implementation Plan:
  [0096-PLAN-kilo-model-catalog-scope-and-order.md](./0096-PLAN-kilo-model-catalog-scope-and-order.md)

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | `httpagent`'s session implements `provider.ModelCatalogSession`. Scope `models` to the session's current model provider (the provider half of its resolved model), falling back to the dialect's default model provider when the session has none. Scope `providers` to the dialect's `ModelProviderLister`. This fixes Kilo and OpenCode in one place. |
| **D2** | The Kilo **default** catalog leads with the engine's own default model provider. `ListModelsLive` orders `conn.Providers` so the provider named by `conn.Default["kilo"]` (else the resolved fallback provider) comes first, then the remaining providers in engine order. The engine's order is preserved *within* the remainder. |
| **D3** | Truncation is reported by whichever layer drops rows. `picker.Catalog` gains a `Truncated` field; `ListModelsLive` sets it when `capDefaultCatalogModels` removed anything; `ModelsResultFromCatalog` carries it onto the wire; `ws.capCatalogOptions` ORs its own truncation into it rather than owning the flag. `maxDefaultCatalogModels` **stays at 150** — the 32 KB budget it satisfies is real and test-enforced, and raising it to 500 produces a 101 KB reply. |
| **D4** | Ordering inside one model provider is deterministic. `modelsOf` sorts its options by option ID before `picker.OrderModels`, so a stable tie on `release_date` resolves to model id, never Go map order. This is a correctness fix for every provider whose engine reports uniform dates, not a Kilo special case. |
| **D5** | The Kilo auto-routers are pinned to the top of the `kilo` model provider's list using the engine's own `recommendedIndex` (frontier 0, balanced 1, efficient 2, free 3 on 7.4.22), read from `/config/providers`. A model without `recommendedIndex` ranks after every model that has one. Kilo's same-day `release_date` makes date ordering useless for exactly these rows, and they are the rows a Kilo user picks first. |
| **D6** | The catalog keeps reading `/config/providers`. `/api/model` (v2) is recorded as the better source — 210 distinct real `time.released` epochs across kilo's 359 models versus 1 date across 295, plus an `enabled` flag, at 795 KB for all providers versus 838 KB — but adopting it is deferred to its own record. D4+D5 make the current source deterministic without it. |
| **D7** | The stale size comments in `catalog_live.go` are corrected against the measured 7.4.22 host: `/config/providers` is **838 KB / 866 models** across 9 connected providers, not the 99 KB the 0075 spike recorded. The 32 KB budget reference stays and now names the test that enforces it. |
| **D8** | A `live_kilo` test pins D2 (the default catalog contains at least one `kilo/kilo-auto/*` router), D4 (two consecutive `ListModelsForLive("kilo")` calls return identical option order), and D5 (`kilo/kilo-auto/frontier` ranks above a non-recommended Kilo model). Without it the next Kilo release silently reintroduces all three. |

### Consequences

* Good, because a Kilo user opening `/model` sees their gateway's 295
  models — including every `kilo-auto/*` router — instead of 149 models
  from providers they did not ask about.
* Good, because truncation becomes honest: every code path that drops rows
  now sets `Truncated`, so the phone can say so (0043 D4).
* Good, because D1 fixes OpenCode's identical session-scope gap for free.
* Good, because D4 removes a nondeterminism that no test could have caught
  by construction — a stable sort over uniform keys is only as stable as
  its input, and the input was a Go map.
* Neutral, because the default (no-session) catalog still concatenates
  connected providers; D2 only changes which one leads.
* Bad, because the session-scoped picker no longer shows other providers'
  models inline. Reaching them is one step through the provider list
  (`ListModelProvidersLive`), which is the flow 0043 D2/D8 already
  designed for; this record makes the common case one step shorter and the
  rare case one step longer.
* Bad, because D5 hard-codes a dependency on an engine field
  (`recommendedIndex`) that Kilo may drop. The fallback is defined — no
  index ranks last — so its removal degrades ordering rather than breaking
  the picker, and D8's test names it.

### Confirmation

Live-probed on kilo 7.4.22 against the real Kilo Gateway, through the
daemon's own `models.list` (not a fixture): the three-row table in
*Context* is the measured before-state. D8's `live_kilo` test is the
standing confirmation. `make pre-add-check`, `make test` and
`go test -race ./...` gate the change.

## More Information

* Supersedes the `maxDefaultCatalogModels` sizing rationale in
  [0076](./0076-MADR-kilo-debug-pass.md) M4 #3. 0076's premise — that
  opencode's 200 cap was never validated against Kilo's larger connected
  set — was correct; its remedy (a lower cap) made the default catalog
  drop the user's own gateway, which is worse than the problem it solved.
* [0043](./0043-MADR-model-selection.md) D1/D2/D4/D8 are
  otherwise unchanged: scoped requests, the provider→models drill-down,
  non-silent truncation, and free-text fallback all stand.
* [0088](./0088-MADR-kilo-7.4.22-surface-parity.md) pins the engine at
  7.4.22; this record adds no new version gate.
* The `execute_pipeline` MCP schema failure in *Context* item 1 is not
  fixed here. Per Project Owner direction it is worked around in Kilo
  configuration rather than in the `magictools` server: the offending tool
  is disabled for Kilo so Anthropic-routed models stop 400ing, and the
  underlying schema fix stays with that server's owner. The Anthropic
  constraint itself is documented at
  <https://docs.claude.com/en/docs/agents-and-tools/tool-use/overview>.
* Three unrelated defects surfaced in the same probe pass and were fixed
  alongside this record rather than deferred, none of them architectural:
  `soleSlashCommand` rejected the `:` in Kilo's MCP-namespaced command
  names (`magictools:pipeline-start`, source "mcp" on 7.4.22), so those
  commands were advertised in autocomplete and then sent to the model as
  prompt text instead of `POST /session/{id}/command`; `agenterr.Present`
  had no arm for a tool-schema rejection, so the failure in *Context* item
  1 reached chat as a raw `AI_APICallError` naming an array index; and
  five `//go:build live_kilo` files had no `make` target and no mention in
  AGENTS.md, so nothing documented how to run them.
* Probe evidence: `docs/kilo-spike-7.4.22/`.
