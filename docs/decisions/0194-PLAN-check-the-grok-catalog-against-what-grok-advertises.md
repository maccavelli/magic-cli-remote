---
status: proposed
date: 2026-10-15
associated-madr: "0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement checking the grok model catalog against what grok advertises

Associated MADR: [0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md](0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option
(D1–D4).

* The owner approves the PLAN by name.
* Each phase ends with `make pre-add-check` on its staged Go files, and `go test -race` on the
  packages it touched.
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

`TestLiveGrokColdCatalog` checks that the cold catalog is grok's own list, by comparing it with what
grok advertises. A unit test guards the no-padding rule in every `go test`. No product code changes.

## Scope

* A new unit test in `internal/provider/acpagent` (D1).
* `internal/provider/grok/live_model_test.go`: the count check replaced by D2's set comparison
  (D2, D3).
* **Out of scope:** the static floor's contents (D4), and any change to `ListModels`.

## Implementation Steps

### G1. The unit test (D1)

1. Add `TestLiveCatalogIsNotPaddedWithTheFloor` to `internal/provider/acpagent`.
   * A `Provider` whose `catalogCache` holds a live catalog (`m-live-1`, `m-shared`), with
     `catalogHas` set.
   * `spec.StaticModels` is `m-shared` and `m-floor-sentinel`.
   * `ListModels` must return exactly `m-live-1` and `m-shared`.
2. It passes on today's code, which is right. So show it failing on a scratch copy where
   `ListModels` merges the floor's options into the live catalog, with the sentinel named in the
   failure.

### G2. The live test (D2, D3)

1. First confirm, with no prompt, that a grok session start yields a `session_config` event
   carrying a `model` option, and record its values. If it does not, stop and record a deviation.
2. In `TestLiveGrokColdCatalog`:
   * Keep the source, non-empty, default, `AllowCustom` and cache checks.
   * Remove the `len(cat.Options) > 3` check.
   * Start a session with no prompt, and read the `model` option's values from its
     `session_config` event, bounded at 30 s.
   * Compare them as sets with `ListModels`' IDs. On a mismatch, fail with both lists.
   * If no `model` option arrives, log why and skip only that comparison.
3. Show it passing against grok `1.0.50`.
4. On a scratch copy, plant a merge in `ListModels` and add a sentinel ID to grok's `staticModels`.
   Show the live test failing, naming the sentinel. That is quota-free: no prompt is sent.

### G3. Records

1. 0184's PLAN already points here, for B1's closing note.
2. Execution record in this PLAN.

## Verification

* G1's test and G2's live test are each seen failing on a planted break on a scratch copy.
* `go test -race ./internal/provider/acpagent/ ./internal/provider/grok/`, `make pre-add-check`,
  `make lint`.
* `go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokColdCatalog` passes. It is
  quota-free.
* `make ci-windows` on the Windows test host.
* `check_records.py --check-all` and `make markdownlint-docs`.

## Rollout and Rollback

* Tests only. Rollback is reverting the commit.
