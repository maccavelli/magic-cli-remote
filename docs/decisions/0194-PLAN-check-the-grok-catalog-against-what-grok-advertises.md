---
status: complete
date: 2026-10-18
associated-madr: "0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement checking the grok model catalog against what grok advertises

Associated MADR: [0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md](0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md)

**Approved 2026-10-15** ("committed proceed", after `ff34eb23`). This PLAN implements the MADR's
chosen option (D1–D4). G1–G3 are executed; see the execution record. `in-progress` until the
MADR's last confirmation item, a full `make live-grok`, has run.

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

## Execution record

Executed 2026-10-15 against `grok 1.0.50 (c58f321264ba) [stable]`. No product code changed, and no
deviation from the steps as written.

### G1. The unit test

* Added `internal/provider/acpagent/catalog_test.go`: `TestLiveCatalogIsNotPaddedWithTheFloor`, as
  step 1 describes. It also checks that the catalog's source is `live`. It passes on the tree.
* Planted breaks, each on a scratch export of `ff34eb23` with the new test copied in:
  * **merge:** `ListModels` returns `picker.MergeLiveStatic(live, static)`.

    ```text
    catalog_test.go:39: source = "merged", want "live"
    catalog_test.go:47: catalog IDs = [m-floor-sentinel m-live-1 m-shared], want [m-live-1 m-shared]: the live catalog was padded with the static floor
    ```

  * **append:** `ListModels` appends the floor's missing options and keeps the source `live`, so
    only the ID check can see it.

    ```text
    catalog_test.go:47: catalog IDs = [m-floor-sentinel m-live-1 m-shared], want [m-live-1 m-shared]: the live catalog was padded with the static floor
    ```

### G2. The live test

* **Step 1.** A probe test on a scratch export started a session through `grok.New(...).Start` with
  no prompt and logged every `session_config` option. grok sent `model` (`grok-4.7`,
  `grok-4.7-build-fast`, `grok-4.6`, `grok-4.5`, current `grok-4.6`) and `reasoning_effort`. The
  MADR's More Information records the output. The premise held, so no deviation.
* **Step 2.** In `internal/provider/grok/live_model_test.go`, the count check is gone. A new helper,
  `sessionModelValues`, starts a session without a prompt and returns the `model` option's values
  from the first `session_config` event that carries one, bounded at 30 s. The test compares them
  with the catalog's IDs as sorted sets. If no such option arrives it logs why and skips only that
  comparison. The other checks are unchanged.
* **Step 3.** On the tree:

  ```text
  live_model_test.go:37: cold catalog: source=live n=4 defaults=[grok-4.6] took=306ms
  live_model_test.go:67: session model option: [grok-4.7 grok-4.7-build-fast grok-4.6 grok-4.5]
  live_model_test.go:82: second open: 3µs (cached)
  --- PASS: TestLiveGrokColdCatalog (1.01s)
  ```

* **Step 4.** On a scratch export with the merge planted and `grok-floor-sentinel` added to grok's
  `staticModels`:

  ```text
  live_model_test.go:37: cold catalog: source=merged n=5 defaults=[grok-4.6] took=307ms
  live_model_test.go:71: cold catalog [grok-4.5 grok-4.6 grok-4.7 grok-4.7-build-fast grok-floor-sentinel] differs from the session's model option [grok-4.5 grok-4.6 grok-4.7 grok-4.7-build-fast]: the catalog is not grok's own list
  --- FAIL: TestLiveGrokColdCatalog (0.91s)
  ```

  No prompt was sent in any run, so no tokens were spent.

### G3. Records

* 0184's PLAN already pointed here.
* The MADR is `accepted`, with the step 1 probe output and what D2 cannot see.

### Verification

* `go test -race -count=1 ./internal/provider/acpagent/ ./internal/provider/grok/`: both `ok`.
* `make pre-add-check` on the two files: `2 file(s) clean (gofmt, golint, govulncheck)`.
* `make lint` and `make staticcheck` (`v0.8.2`, linux, darwin and windows): exit 0.
  `go vet -tags live_grok` and the pinned staticcheck with `-tags live_grok` on both packages: clean.
* `make ci-windows` on the Windows test host, against the working tree: `ci-windows-local: ALL
  SELECTED CHECKS PASSED`, exit 0.

### Not done

* ~~**A full `make live-grok`.** The MADR's Confirmation names it. It sends model turns, which spend
  quota, and the owner has not approved a run for this record. The PLAN's own live check, the
  targeted `TestLiveGrokColdCatalog`, passed without spending any. This PLAN stays `in-progress`
  until a full run is approved and passes.~~ Done on 2026-10-18 (below).

### The full live run, 2026-10-18: complete

The owner approved a full `make live-grok` on 2026-10-18. It ran on grok `1.0.50`: 103 top-level
tests ran, 102 passed and 1 failed. This record's tests passed:

```text
live_model_test.go:37: cold catalog: source=live n=4 defaults=[grok-4.6] took=273ms
live_model_test.go:67: session model option: [grok-4.7 grok-4.7-build-fast grok-4.6 grok-4.5]
--- PASS: TestLiveGrokColdCatalog (0.87s)
```

The one failure was the armed half of `TestLiveGrokAutoDiscriminationPair`. In that run the agent
followed the host's own agent rules and wrote a decision record instead of `probe.txt`, so the
test's check on the filename failed. Auto mode itself worked: there were no prompts for those writes.
[0198-MADR-prove-grok-auto-mode-by-any-write-not-one-filename.md](0198-MADR-prove-grok-auto-mode-by-any-write-not-one-filename.md)
changed the check to accept any file the agent writes. That test, run alone afterwards, passed:

```text
live_automode_test.go:88: arm=false prompts=1 files=[] turnDone=false
live_automode_test.go:88: arm=true  prompts=0 files=["docs/decisions/0001-MADR-create-probe-txt.md" "docs/decisions/0001-PLAN-create-probe-txt.md"] turnDone=true
--- PASS: TestLiveGrokAutoDiscriminationPair (157.46s)
```

The MADR's last confirmation item is met, and this PLAN is `complete`.
