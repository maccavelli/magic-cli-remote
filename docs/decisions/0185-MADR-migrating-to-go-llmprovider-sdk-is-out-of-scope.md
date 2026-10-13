---
status: accepted
date: 2026-10-13
decision-makers: Project Owner
consulted: none
informed: maintainers of go-llmprovider-sdk
---

<!-- markdownlint-disable MD013 -->

# Keep Magic's model catalogs engine-backed: migrating to `go-llmprovider-sdk` is out of scope

## Context and Problem Statement

[0183-REPORT-go-llmprovider-sdk-catalog-parity-assessment.md](../reports/0183-REPORT-go-llmprovider-sdk-catalog-parity-assessment.md)
compared the `go-llmprovider-sdk` catalog API (`catalog.List`, `Catalog.Recommended`/`Usable`) with
the catalogs Magic builds today: `internal/picker` (`picker.Catalog`, `OrderModels`, `Cache`) fed by
the engine adapters in `internal/provider/opencode`, `internal/provider/kilo` and the others. The
report decides nothing. Its section "What this means for Magic's adapters" sketches a migration in
which Magic backs its catalogs with `catalog.List`, starting with the direct-API providers, once the
SDK ships two extensions: a `WithMaxListed` option and per-model `Details` metadata.

On the same day, the owner directed Magic to *"offload as much as possible to the libraries, and
provide the fullest adoption possible"*. That direction is applied to self-update in
[0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
and to ACP in
[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md).
Read on its own, it could be taken to cover `go-llmprovider-sdk` as well.

Should Magic plan to migrate its model catalogs to `go-llmprovider-sdk`?

## Considered Options

* Out of scope: Magic keeps its own engine-backed catalogs and does not adopt `go-llmprovider-sdk`
* Migrate the direct-API providers to `catalog.List` once the SDK ships `WithMaxListed` and `Details` (the 0183 report's sketch)

## Decision Outcome

Chosen option: "Out of scope: Magic keeps its own engine-backed catalogs and does not adopt
`go-llmprovider-sdk`", because the owner decided on 2026-10-13 to take migrating to
`go-llmprovider-sdk` out of scope for `magic-cli-remote`.

This means:

* **No dependency.** Magic does not add `go-llmprovider-sdk` to `go.mod`. It has none today: no
  `go.mod` or `go.sum` in this repository names an `llmprovider` module.
* **Catalogs stay as they are.** `internal/picker` (catalog shape, `OrderModels`, `Cache`, the
  `Truncated` admission) and the engine adapters remain the catalog source. Nothing in them is
  planned to move.
* **The owner's adoption direction does not reach this SDK.** The "fullest adoption" direction
  quoted in 0054 and 0184 covers `go-selfupdate-lib` and `acp-go-sdk2`. It does not cover
  `go-llmprovider-sdk`.
* **The 0183 report stays as evidence.** It remains useful to the `go-llmprovider-sdk` repository,
  where any `WithMaxListed` or `Details` decision belongs. Its section "What this means for Magic's
  adapters" no longer describes planned Magic work.
* **No Magic work waits on the SDK.** Magic does not ask for, and does not schedule work around,
  the SDK extensions the report proposes.

These are not affected:

* **Other programs' dependencies.** `gobble-cli` pins `go-llmprovider-sdk`
  ([0179-REPORT-gobble-native-acp-integration-assessment.md](../reports/0179-REPORT-gobble-native-acp-integration-assessment.md)).
  Magic talks to gobble over ACP as a separate process, so that pin is gobble's own.
* **`mcplib`'s `llmprovider` package.** Magic never imported it, and `mcplib` leaves the module
  under 0054.

No PLAN accompanies this record: it decides that no work happens.

### Consequences

* Good, because Magic's picker behavior stays exactly as it is. The report's risks don't arise:
  `Usable`'s filters would hide rows that OpenCode users see today, and the OpenCode catalog
  (measured at 4.3 MB) sits near the SDK's 8 MiB decode limit.
* Good, because Magic takes on no new dependency and depends on no unreleased SDK API.
* Neutral, because the SDK repository can decide its own catalog API without needing to stay
  compatible with Magic.
* Bad, because Magic keeps maintaining its own per-engine catalog code and model metadata. That
  includes the static fallbacks, which the SDK also curates.

### Confirmation

A review rejects any change that adds a `go-llmprovider-sdk` module to `go.mod`, or that calls its
catalog API, unless a later MADR supersedes this one. Bringing the migration back into scope takes
a new MADR, not an amendment to the 0183 report.

## Pros and Cons of the Options

### Out of scope: Magic keeps its own engine-backed catalogs and does not adopt `go-llmprovider-sdk`

* Good, because the shape the report calls essential stays unchanged: complete per-provider lists
  with display metadata, one shared sort, and an honest truncation flag.
* Good, because the catalog cache stays where it already is, in `picker.Cache`. The report's
  2026-10-11 owner decision put caching with the consumer in any case.
* Bad, because Magic never gets the SDK's shared normalizers, pagination and static fallbacks.

### Migrate the direct-API providers to `catalog.List` once the SDK ships `WithMaxListed` and `Details`

* Good, because the list fetching, pagination, timeouts and degrade-to-static behavior already exist in the SDK.
* Bad, because it depends on two SDK extensions that are not decided, let alone released.
* Bad, because `Usable` is filtered, so OpenCode users would see different Gemini and Kilo rows.
* Bad, because the engine-backed providers (OpenCode over HTTP, codex) would stay outside the SDK
  in the first phase. Magic would then run two catalog sources side by side.

## More Information

* Source of the options and risks: [0183-REPORT-go-llmprovider-sdk-catalog-parity-assessment.md](../reports/0183-REPORT-go-llmprovider-sdk-catalog-parity-assessment.md),
  sections "What this means for Magic's adapters" and "Risks and decision points".
* Records that apply the owner's adoption direction, which this record limits: [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)
  and [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md).
