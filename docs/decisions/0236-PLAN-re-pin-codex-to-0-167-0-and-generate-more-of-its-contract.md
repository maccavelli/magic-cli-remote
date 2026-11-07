---
status: in-progress
date: 2026-11-07
associated-madr: "0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement re-pinning codex to 0.167.0, and generating more of its contract

Associated MADR: [0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md](0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md)

## Goal

When this plan is done:

* Codex's `KnownGoodVersion` and its embedded contract manifest both name 0.167.0, with a capture
  of that release.
* The manifest, at `schema_version` 2, carries four more generated facts:
  * each client request's response type, read from codex's source;
  * every enum's values;
  * each notification's stability;
  * a check that every name Magic decodes from the app-server has a source in the schema.
* `thread/section/changed` is routed as a declared drop (Q1 A).
* MCP approvals name the tool (Q2 A). Managed checkpoints stay deferred, and no experimental
  surface is adopted (Q3 A).

## Scope

* **P1, the generator (D3):**
  * `internal/provider/codex/contract_generate_test.go`, `contract.go` (the manifest types, the
    validation at `:157`, `CompareSurfaces` at `:364`) and `contract_test.go`;
  * `live_contract_test.go` (the enum comparison);
  * a new `decoded_names_test.go`;
  * `review.go:268-272` (F14);
  * `scripts/codex-contract.ps1`, if it must pass a new input;
  * `testdata/0.166.0/`, regenerated.
* **P2, the re-pin (D1, D2):**
  * `contract.go` (the three embed paths), `version.go` and `contract_test.go`;
  * `routing.go`, plus a routing test for the two F5 shapes;
  * `testdata/0.167.0/` (generated, new) and `testdata/0.166.0/` (deleted);
  * `testdata/wire/0.167.0/` (new), and any test that selects frames by version.
* **P3, adoption (D4):** `callbacks.go` and its tests, and the source-watch manifest's `_meta`
  keys.
* **P4, docs:**
  * [docs/guides/ops-codex-contract.md](../guides/ops-codex-contract.md): the example command, the
    figures, the notification-split section, and the drift-gate table's "CI" use, which nothing
    runs (0109 amendment E10). If 0109's P0 has not fixed that first, this phase does, and
    0109's P0 drops the item;
  * `testdata/0.167.0/README.md`;
  * the comment in `version.go`;
  * `docs/README.md`'s index.
* **Records:** this pair, its index rows, and the cross-references in the 0109 amendment and its
  second PLAN.
* **Not in scope:**
  * the 0109 amendment's phases;
  * a thread-list push (Q1 B, not chosen);
  * `requiredHumanApproval` and every other experimental surface (Q3 A);
  * Go type generation (MADR option C);
  * `sandbox.filesystem_paths` (D6);
  * the source trees under `~/gitrepos`, which are read and never written.

## Constraints

* **C1 — One commit per phase.** The agent stages; the owner commits with `git commit --no-edit`.
* **C2 — Live runs only on the owner's word,** each named below:
  * P2's capture turn: **approved**, owner 2026-11-07, "approved to probe/capture";
  * the scratch-home probes, and `make live-codex-contract` (both modes, which spend no quota but
    read the owner's real `CODEX_HOME`): **approved** by the same answer;
  * `make live-codex`: not yet. It is asked for when P2 reaches it.
* **C3 — Every new check is seen failing first,** on a scratch copy, never in the tree.
* **C4 — Captures are read before they are committed.** Identifiers become placeholders. The
  `config/read` answer is left out, as at 0.166.0 (0226-PLAN R3 Deviation 2). The guard's redact
  preview runs on the tree before staging.
* **C5 — No push without an explicit ask in the same turn.**
* **C6 — The installed version is checked at each phase's start.** A binary that moved off
  0.167.0 is a stop.
* **C7 — The source tree is a scratch clone at `086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a`,**
  made by `git clone --no-checkout --shared ~/gitrepos/codex <scratch>`, then fetching
  `refs/tags/rust-v0.167.0` from `https://github.com/openai/codex.git` into the scratch clone
  (MADR F1).

## Implementation Steps

### P1 — The generator, at the 0.166.0 pin (D3)

1. **Response types from the source (D3.1).**
   * The generator parses `client_request_definitions!` in
     `codex-rs/app-server-protocol/src/protocol/common.rs` under `CODEX_SOURCE_TREE`. For each
     entry it takes the method literal, the `params:` type and the `response:` type, with the
     `v2::` or `v1::` prefix removed.
   * Each response type must name a definition in the bundle; a missing one is fatal. Each bundle
     client request must have a source entry; a missing one is fatal.
   * The override map (`contract_generate_test.go:305-327`) and the name-guessing above it are
     deleted.
   * Seen failing on a scratch copy of `common.rs` with one entry removed.
2. **Enum values (D3.2).**
   * The manifest gains `enums`: a map from each definition name to its sorted string values.
     A `oneOf` or `anyOf` whose every member is a string `enum` is flattened into one set.
   * `CompareSurfaces` reports a removed value as breaking, and an added value as additive.
   * Exact mode compares the maps.
   * Seen failing on a scratch manifest with `writeStdin` removed from
     `CommandExecutionApprovalKind`, which must be reported as breaking.
3. **The notification split (D3.3).** ~~Each `server_notifications` entry gains `stability`,~~ *The
   manifest gains a top-level `experimental_notifications` list (P1 Deviation 2),* taken from the
   existing `#[experimental]` scan. The capture's printed totals must equal the counts in the
   manifest.
4. **`schema_version` 2.** The validation at `contract.go:157`, and `contract_test.go:12`, accept
   only 2. Every other reader of the manifest moves in the same commit.
5. **Decoded names (D3.4).** `decoded_names_test.go`:
   * parses the package's non-test `.go` files with `go/parser`;
   * collects the `json` tag name of every field of every struct type, including anonymous
     structs inside functions;
   * drops the types on an exclusion list. Each entry is keyed by file and type name, with a
     one-line reason: an own output, `doctor --json`, the daemon's JSON, `auth.json`, or the
     manifest;
   * requires every remaining name to be in the manifest's recorded set of schema property names.
     The manifest gains that set, generated from both bundles.

   A short `knownOutsideSchema` list holds wire names that codex sends without a schema, each with
   evidence. It starts with `capability` (`conn.go:98`), tied to 0109 D39. Seen failing on a
   scratch copy with a planted field `json:"planted-name-3f9a"` in a decode struct.
6. **The review fallback (F14).** A test feeds `reviewFallbackText` the `exitedReviewMode` item as
   the schema defines it (`review` a string). Today's code passes it, so this test pins the
   behaviour that stays. The object branch, `review.go:268-272`, is removed, and the decoded-name
   check then passes without listing `body`.
7. **Regenerate 0.166.0.** *With codex 0.166.0 installed into a scratch npm prefix and first on
   `PATH` for this command only (P1 Deviation 1):* `pwsh ./scripts/codex-contract.ps1 -Version
   0.166.0 -SourceCommit 6ff1e1ca3b30fc8b914720681fcf5fe62e95f856 -SourceTree <scratch at
   rust-v0.166.0>`. The diff to
   `testdata/0.166.0/` may add only the new fields. Any change to an existing method,
   classification or fixture is a stop.
8. **Checks:**
   * `go test -race ./internal/provider/codex/`, `make pre-add-check` and `make vet-tags`;
   * `make live-codex-contract` (C2). On the 0.167.0 binary it must now also list
     `CommandExecutionApprovalKind +humanCommand` as additive;
   * `CODEX_CONTRACT_EXACT=1 make live-codex-contract` is expected to fail here, because the
     binary is 0.167.0. That failure is recorded, not fixed.

### P2 — Re-pin to 0.167.0 (D1, D2)

1. `codex --version` must still print `codex-cli 0.167.0` (C6).
2. **The contract:** `pwsh ./scripts/codex-contract.ps1 -Version 0.167.0 -SourceCommit
   086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a -SourceTree <scratch, C7> -DryRun`, then without
   `-DryRun`. Expected:
   * stable 106/86/10 and experimental 180/86/11 (MADR F3);
   * an empty `installed_delta`;
   * one notification more than 0.166.0's split, and it is stable.
3. The three embed directives name `testdata/0.167.0/`. `KnownGoodVersion = "0.167.0"`, with its
   comment naming the new capture. `testdata/0.166.0/` is removed with `git rm -r`. Its README's
   notes are carried forward where they still hold, each re-checked against 0.167.0.
   `contract_test.go` asserts 0.167.0's version, digest, counts and commit.
4. **Route `thread/section/changed` (Q1 A).**
   * It joins the provider route table and the declared-drop case in
     `handleProviderNotification`, with a comment naming this record.
   * A test feeds both F5 shapes (a section id, and `null`) and requires the declared drop.
   * The generator is re-run, because the classification is derived from `routing.go`.
5. **The capture (live, C2).**
   * One short turn, run as 0.166.0's was, with the initialize answer and the turn's frames,
     into a scratch directory.
   * The frames are read under C4, then committed as `testdata/wire/0.167.0/` with a `meta.json`
     in the 0.166.0 one's form. That `meta.json` says what was replaced and what was left out.
   * Any test that selects frames by version follows, selecting them by shape.
6. **Checks:**
   * `TestPinMatchesTheFixtureItCites` and the token-usage test, against 0.167.0;
   * `go test -race ./...`, `make pre-add-check` and `make vet-tags`;
   * `make live-codex-contract`, and `CODEX_CONTRACT_EXACT=1 make live-codex-contract`;
   * `make live-codex` (C2).

### P3 — Adoption (D4)

* **MCP approvals name the tool (Q2 A):**
  * `mcpServer/elicitation/request` decodes `_meta.tool_name`, `tool_title`, `connector_name`
    and `tool_annotations.destructiveHint`.
  * The sheet's detail reads "<connector or server>: <tool title> (<tool name>)". It adds "This
    tool can change or delete data" when `destructiveHint` is true.
  * Missing keys fall back to today's text.
  * The key names join the source-watch manifest, scanned from
    `codex-rs/protocol/src/mcp_approval_meta.rs` at the tag.
  * A test with all four keys, and one with none, are seen failing on today's code (the first),
    and passing (the second).
* **Managed checkpoints (Q3 A):** nothing.
* **Checks:** as P2's static checks. `make live-codex` again only if P3 changed the callback path,
  and only on the owner's word.

### P4 — Docs

1. Update `ops-codex-contract.md`:
   * the example command to 0.167.0 and its commit;
   * the figures, giving 0.167.0's beside 0.166.0's;
   * withdraw the "treat 'stable notification' as 'declared'" caveat (D3.3);
   * describe the enum gate and the decoded-name check under "What the generator derives";
   * replace "CI" in the drift-gate table with the hosts that actually run it.
2. The `version.go` comment, and the 0.167.0 README, name this record.
3. `python3 scripts/check_records.py --write-index`; `make check-records markdownlint-docs`.

### P5 — Records

* Each phase's execution record, with output quoted and deviations dated.
* The guard's redact preview, run from the repo root before staging.
* The guard over the outgoing range before the owner pushes.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | The regenerated 0.166.0 manifest differs from the committed one only by the new fields | Confirmation 1 |
| V2 | The override map is gone, and every response type resolves from `common.rs` | D3.1 |
| V3 | The enum, source-declaration and decoded-name checks were each seen failing on a scratch copy | Confirmation 2 |
| V4 | `live-codex-contract` lists `humanCommand` as additive against the 0.166.0 pin | D3.2, F4 |
| V5 | `KnownGoodVersion` and the manifest both say 0.167.0, and `testdata/0.166.0/` is gone | D1 |
| V6 | Both gate modes pass against 0.167.0, and `make live-codex` passes | Confirmation 3 |
| V7 | `thread/section/changed` is routed, and both F5 shapes are tested | D2, Confirmation 4 |
| V8 | The MCP sheet names the tool, and the test failed first | D4, Confirmation 5 |
| V9 | Race, lint, vet-tags, records and markdownlint pass, and the guard previews are clean | Confirmation 6 |

## Rollout and Rollback

* P1 changes no runtime behaviour except F14's dead branch, which the schema says cannot be taken.
  Rolling it back is reverting its commit and regenerating at `schema_version` 1.
* P2 is one commit. A version off its pin warns and never refuses, so reverting it returns the
  0.166.0 pin without stopping a 0.167.0 binary from running.
* P3 changes daemon text only (Q2 A). Reverting it restores the server-name-only sheet.
* Nothing here changes the phone protocol (Q1 A, Q3 A).

## Execution record

### P1 Deviation 1, 2026-11-07 — step 7 cannot capture 0.166.0 with 0.167.0 installed. The owner decided: "Scratch 0.166.0"

- **Found** before any code was written. The generator refuses unless the codex on `PATH` reports
  the version being captured (`contract_inputs_test.go:89-93`), and the installed codex is 0.167.0.
- **Resolution:** codex 0.166.0 is installed with `npm install --prefix <scratch>`, with npm's
  cache also in the scratch directory so nothing is written under `$HOME`. That prefix's `.bin` goes
  first on `PATH` for the capture command only. The installed 0.167.0 is not touched. Step 7's
  criterion is unchanged.
- **Not taken:** folding the proof into P2 by capturing 0.167.0 twice.
- **The MADR is unchanged.**

### P1 Deviation 2, 2026-11-07 — a per-entry notification `stability` would break exact mode. The owner decided: "Top-level list"

- **Found** while designing step 3. Exact mode compares the pinned surfaces with the binary's
  export by `reflect.DeepEqual` (`live_contract_test.go`). The live test has no source tree, so it
  cannot know a notification's stability, and a per-entry field would make exact mode fail on every
  run.
- **Resolution:** the manifest gains a top-level `experimental_notifications` list, outside the
  compared surfaces. D3.3 holds as written: the manifest keeps each notification's stability.
- **Not taken:** holding D3.3 out of P1.
- **The MADR is unchanged.**

### P1 — 2026-11-07: the generator, at the 0.166.0 pin

Installed `codex-cli 0.167.0`, unchanged throughout. Scratch 0.166.0 from `npm install --prefix`
(Deviation 1). Its `bin/codex.js` hashes to the pinned `e14fc837…`, and so does 0.167.0's launcher.
On macOS, then, the digest cannot tell releases apart: version equality is the only evidence.

1. **Response types (D3.1).** `sourceResponseTypes` and `macroEntries` (`contract_inputs_test.go`)
   read `response:` from `client_request_definitions!` and `server_request_definitions!`.
   * The legacy server requests take their lower-camel-cased variant name.
   * `checkSourceResponseTypes` fails the capture on a bundle request with no declaration, or with
     a type the export does not define.
   * The override map and the name-guessing are deleted (`responseSchema`).
   * The live gate takes the pin's response types from its fixtures (`fixtureResponseTypes`).
2. **Enums (D3.2).** `ContractSurface.Enums` is filled by `readSchemaVocabulary`. It covers named
   definitions, `oneOf`/`anyOf` unions of string enums, and titled enums nested anywhere.
   `compareEnums` is wired into `CompareSurfaces`. The titled enums include each method's own
   discriminator (such as `Thread/guardianReviewRequestMethod`), so a new method also reports an
   "added enum" line. That duplicates the method inventory, but it follows D3.2's "every string
   enum" as written.
3. **The split (D3.3).** `ContractManifest.ExperimentalNotifications` (Deviation 2). `Validate`
   requires it sorted and declared.
4. **`schema_version` 2.** `manifestSchemaVersion = 2` in `contract.go`, used by `Validate`, the
   generator and the source-watch manifest. `contract_test.go` asserts 2 and 24 experimental
   notifications.
5. **Decoded names (D3.4).** `decoded_names_test.go`, with 19 `notAppServerTypes` entries, each
   with its reason:
   * `contract.go`;
   * `authDotJSON`;
   * the capability and runtime snapshots;
   * the doctor and daemon JSON;
   * the two `settings.go` projections.

   One `knownOutsideSchema` entry: `capability`, tied to 0109 D39. The first run without
   exclusions flagged the 75 names MADR F14 counted.
6. **The review fallback (F14).** `TestReviewFallbackTextReadsTheSchemaString` was added, and the
   object branch in `review.go` removed. After that, the decoded-name check flags nothing.
7. **Regenerate 0.166.0.** `PATH=<scratch 0.166.0>:$PATH pwsh ./scripts/codex-contract.ps1
   -Version 0.166.0 -SourceCommit 6ff1e1ca… -SourceTree <scratch clone at rust-v0.166.0>`: exit
   0, "notifications: 85 declared; 61 stable, 24 experimental-only".
   * **V1:** `scratch compare_manifests.py`, old against new: "manifest.json: identical apart from
     the new fields", "fixtures.json: identical apart from the new fields",
     "source-watch-manifest.json: identical apart from the new fields". `fixtures.json` is
     byte-identical at 78,490 bytes, so the source-derived response types reproduce exactly what
     the guess plus the 19 overrides gave.
   * New content: 521 stable and 611 experimental enums, 24 experimental notifications, 1,055
     property names.
   * Size: `manifest.json` grows from 116,258 to 239,987 bytes, and `source-watch-manifest.json`
     from 84,358 to 184,190. Both are embedded, so the daemon grows by about 224 KB.
   * The README names this regeneration and the new fields.
8. **Seen failing** (`scratch p1_plants.py`; each plant on its own rsync copy of the tree; 7 of 7
   failed as intended):
   * a planted `json:"planted-name-3f9a"` in `reviewFallbackText`: "JSON name with no source in
     the 0.166.0 schema: review.go:264 review.go:reviewFallbackText json:\"planted-name-3f9a\"";
   * the pre-change object branch restored:
     * the review test fails with `got "guessed", want nothing`;
     * the decoded-name check names `review.go:275 … json:"body"`;
   * `compareEnums` unwired: all four `TestCompareSurfacesClassifiesEnumDrift` subtests fail;
   * `writeStdin` removed from the pinned manifest: "stable CommandExecutionApprovalKind =
     [\"command\"], want [command writeStdin] at 0.166.0";
   * comment stripping removed from the source parser: "entry \"/// Starts a thread; returns\"
     declares no response: type";
   * `thread/unsubscribe` deleted from a copy of `common.rs`: "client_request thread/unsubscribe
     is in the export but has no declaration in common.rs". Nothing was written;
   * the live gate against 0.167.0, with a pinned value codex never had: "BREAKING drift:
     removed stable enum value CommandExecutionApprovalKind.notAKind9f2", exit 1.
9. **Checks:**
   * `go test -race ./internal/provider/codex/`: ok.
   * `make vet-tags`: ok. `go build ./...`: ok.
   * `make pre-add-check` on the nine changed Go files: "9 file(s) clean (gofmt, golint,
     govulncheck)".
   * `make live-codex-contract` on 0.167.0: exit 0, and **9 additive differences** where 3 were
     reported before P1, among them `CommandExecutionApprovalKind.humanCommand` on both surfaces
     and the experimental `GuardianAssessmentOutcome` (V4).
   * `CODEX_CONTRACT_EXACT=1 make live-codex-contract`:
     * on 0.167.0: exit 2, "exact mode: installed stable surface differs from the 0.166.0
       manifest" and a binary identity mismatch, expected and recorded;
     * on the scratch 0.166.0: exit 0, so exact mode passes against the regenerated manifest.

**Not done in P1:** the guide's text (P4), and the re-pin (P2).

### P2 — 2026-11-07: codex to 0.167.0

1. `codex --version`: `codex-cli 0.167.0`. The scratch clone (C7) is at `086ccc833bc9…`.
2. **The contract.**
   * `codex-contract.ps1 -DryRun`: exit 0. It resolved `/opt/homebrew/bin/codex`, which reports
     0.167.0.
   * The capture ran after step 4's route, so the classification includes it. Exit 0,
     "notifications: 86 declared; 62 stable, 24 experimental-only".
   * It wrote `testdata/0.167.0/`, and `scratch check_0167_manifest.py` read it back:
     * Stable 106/86/10 and experimental 180/86/11, as MADR F3 expects.
     * 522 and 614 enums, and `CommandExecutionApprovalKind` = `command`, `humanCommand`,
       `writeStdin`.
     * `thread/section/changed` is not in `experimental_notifications`, and is classified
       `implemented`. `thread/guardianReview` is `typed_deferred`, with response
       `ThreadGuardianReviewResponse` read from the source.
     * No fixture has a generic response, `installed_delta` is empty, and no local path appears in
       the three files.
3. **The pin.**
   * The embeds name `testdata/0.167.0/`, and `KnownGoodVersion = "0.167.0"`, its comment naming
     the new capture.
   * `contract_test.go` asserts 0.167.0, the unchanged launcher digest, 106/86/10 and 180/86/11,
     24 experimental notifications with `thread/section/changed` not among them, the new
     `humanCommand` value, and the `rust-v0.167.0` source commit.
   * `testdata/0.167.0/README.md` carries the 0.166.0 notes with 0.167.0's numbers.
   * *Step 3 said `git rm -r`. `testdata/0.166.0/` was instead removed from the working tree only,
     because `git rm` stages the removal, and P1's commit, which regenerates that directory, was
     still staged. The removal is staged with P2. The resulting commits are the same.*
4. **The route (Q1 A).** `thread/section/changed` joins `notificationRoutes` as a provider route,
   and joins the declared-drop case with a comment naming D2.
   * The F5 shapes are committed as `testdata/wire/0.167.0/thread-section-changed.jsonl`. Their
     ids are from a scratch `CODEX_HOME`. Wire captures are kept from one pin to the next, and
     contract directories are deleted.
   * `TestThreadSectionChangedIsADeclaredDrop` drives both shapes. It requires two declared-drop
     log lines, no id in the log, and no session event.
5. **The capture (live, approved).**
   * `MCREMOTE_WIRE_CAPTURE_DIR=<scratch> go test -tags live_codex_turn -run
     '^TestLiveThreadLifecycle$'`: one turn, "assistant chunk: hello", "turn completed:
     status=end_turn", 30 frames.
   * Read under C4 (`scratch p2_scan_capture.py`). Wirecap had already replaced the home
     directory with `/home/user` and the host name with `<host>`.
     * Frame 4, the `config/read` answer, carried trusted-project paths, per-user temp roots and
       MCP settings. It is left out.
     * Frame 1's `installationId` became `<installation-id>`.
     * `planType` stays, as in the 0.166.0 capture.
   * `scratch p2_redact_capture.py` wrote the 29 frames only after checking that no frame holds
     the home directory, the host or account name, an email address, a temp root, `/private/tmp`
     or a real installation id. `meta.json` says what was replaced and what was left out.
   * The 0.166.0 capture stays as history, and `capture0166` still reads it by name.
6. **Seen failing** (`scratch p2_plants.py`, scratch copies; 2 of 2 failed as intended):
   * the route removed: "declared-drop log lines = 0, want 2", and "1 of 86 declared
     notification(s) have no route";
   * `thread/section/changed` marked experimental in the manifest: "experimental notifications =
     25, want 24", and "thread/section/changed is recorded as experimental; the source marks it
     stable".
7. **Checks:**
   * `go test -race ./...`: ok. The wirecap identifier test, which globs every
     `testdata/wire/*/frames.jsonl`, passes on the new capture.
   * `make pre-add-check` on the five changed Go files: clean. `make vet-tags`: ok.
   * `make live-codex-contract`: exit 0, with no drift.
   * `CODEX_CONTRACT_EXACT=1 make live-codex-contract`: exit 0. The pin, the binary and the
     schema agree.
   * **`make live-codex`: not run.** It spends quota and waits for the owner's word (C2).

### P3 — 2026-11-07: MCP approvals name the tool (D4, Q2 A)

1. **Where the keys come from.** The `_meta` codex builds
   (`core/src/mcp_tool_call.rs`, `build_mcp_tool_approval_elicitation_meta`, at the tag):
   * `tool_name` is always present;
   * `tool_title` and `tool_annotations` appear when the server declared them;
   * `connector_name` appears only for codex's own Apps server.

   The hint inside `tool_annotations` is spelled `destructiveHint` (codex's own test,
   `core/src/mcp_tool_call_tests.rs:803`).
   Codex also puts `Allow <actor> to run tool "<name>"?` into the elicitation's `message`, which
   Magic does not show. Showing it is not in this plan.
2. **The sheet.** `parseCallback` now reads `_meta` as raw JSON and decodes `mcpApprovalMeta` from
   it separately. A `_meta` it cannot read leaves today's text, instead of failing the approval,
   which is what decoding `_meta` inline would have done.
   * `mcpApprovalDetail` builds "<connector or server>: <title> (<name>)".
   * It adds ". This tool can change or delete data." when `destructiveHint` is true.
   * With no keys, the text stays the server name.
3. **Seen by the decoded-name check first.** With the struct added, `TestDecodedNamesHaveASchemaSource`
   failed on all five names (`tool_name`, `tool_title`, `connector_name`, `tool_annotations` and
   `destructiveHint`), because the schema types `_meta` as any JSON. D3.4 working as intended.
4. **The keys' source (D4).**
   * `mcpApprovalMetaKeys` (`contract_inputs_test.go`) reads the 25 string constants of
     `codex-rs/protocol/src/mcp_approval_meta.rs` at the tag into the source-watch manifest's new
     `mcp_approval_meta_keys`. The four codex keys are among them.
   * The decoded-name test accepts those keys only for `callbacks.go:mcpApprovalMeta`.
   * `destructiveHint` is in neither codex's schema nor its source: it is the MCP specification's
     `ToolAnnotations` field, serialized by the rmcp crate codex pins (`=3.5.1`). It is listed in
     `knownOutsideSchema` with that evidence.
   * The 0.167.0 contract was regenerated. `scratch compare_p3.py` against P2's staged files:
     "identical apart from mcp_approval_meta_keys" for all three.
5. **Tests.** `TestMCPApprovalNamesTheTool` (`mcp_approval_test.go`) covers six `_meta` shapes:
   * title, name and a destructive hint;
   * a connector;
   * a name alone, with a hint that is not destructive;
   * empty;
   * null;
   * a string.

   It also checks that the permission event the phone receives carries the text.
6. **Seen failing** (`scratch p3_plants.py`, 2 of 2):
   * against the pre-P3 sheet, the three keyed cases fail (`detail = "docs", want "docs: Search
     pages (search_pages). This tool can change or delete data."`), as does the phone-event check.
     The three no-key cases pass, so the fallback is today's behaviour;
   * with codex's `tool_name` constant renamed in the source-watch manifest: "JSON name with no
     source in the 0.167.0 schema: callbacks.go:186 callbacks.go:mcpApprovalMeta
     json:\"tool_name\"".
7. **Managed checkpoints (Q3 A):** nothing.
8. **Checks:**
   * `go test -race ./internal/provider/codex/`: ok.
   * `make pre-add-check` on the six changed Go files: clean. `make vet-tags`: ok.
     `go build ./...`: ok.
   * `CODEX_CONTRACT_EXACT=1 make live-codex-contract`: exit 0.
   * `make live-codex` was not run. P3 changed the MCP callback path, so the one run asked for at
     P2 covers both phases.
