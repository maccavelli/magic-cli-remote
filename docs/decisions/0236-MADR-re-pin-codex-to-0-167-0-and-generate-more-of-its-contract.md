---
status: accepted
date: 2026-11-07
decision-makers: Project Owner
consulted: "the installed codex-cli 0.167.0 on this host, driven under a scratch CODEX_HOME with no login and no turn; codex's source at rust-v0.166.0 and rust-v0.167.0 (a scratch clone of ~/gitrepos/codex with the 0.167.0 tag fetched into it; the owner's checkout was read, not written); make live-codex-contract against 0.167.0; scratch probes probe_codex_0167.py, probe_codex_cli_0167.py, probe_doctor_keys.py, notif_split.py and decoded_keys.py"
informed: anyone working on the codex provider
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Re-pin codex to 0.167.0, and generate more of its contract from codex's own sources

## Context and Problem Statement

codex-cli 0.167.0 is installed on this host, and the owner will pin it. The pin is 0.166.0
(`internal/provider/codex/version.go:20`, and the contract manifest embedded from
`testdata/0.166.0/`, `contract.go:121-127`). By
[0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)
D10, each engine pin is its own record with its own live run, so this record does not reuse
[0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md).

The owner asked for four things: drive the CLI; read the sources for changes that reach Magic's
code; make sure Magic generates as much as it can from codex's published schemas, surfaces and
protocols; and find any new stable features worth adopting. This record holds the findings and
proposes what to do about each.

The short answer: 0.167.0 breaks nothing Magic does. It adds one stable notification Magic does not
route, which the re-pin must deal with. The drift gate saw only three of the release's ten schema
changes. Magic still keeps by hand some facts that codex's sources state outright. Two new features
are worth a decision: one stable, one experimental.

### What changed, 0.166.0 to 0.167.0 [observed 2026-11-07]

* **F1 — The release and its tag.** `codex --version` prints `codex-cli 0.167.0`, published to npm
  on 2026-11-06. `rust-v0.167.0` peels to `086ccc833bc9cf01024ad2dd6af7dbd421b6bf9a`. The owner's
  checkout is at `main` (`57e64682f8`) and does not contain that commit, because the release branch
  was not fetched. It was fetched into a scratch clone, which is where every source citation below
  comes from.
* **F2 — The drift gate passes with three additions.** `make live-codex-contract` (default mode)
  exits 0. It reports: experimental client request `thread/guardianReview`, and server notification
  `thread/section/changed` (listed once per surface, because codex's exporter does not prune
  notifications). Nothing is breaking. The binary identity differs from the fixture, as expected
  off a pin.
* **F3 — The schema, type by type.** This compares a fresh `generate-json-schema` export with the
  0.166.0 export captured earlier the same day.
  * Stable: 106 client requests, 86 notifications, 10 server requests, against 106/85/10.
    Experimental: 180/86/11, against 179/85/11.
  * Added definitions:
    * stable: `ThreadSectionChangedNotification`;
    * experimental: `ThreadGuardianReviewParams`, `ThreadGuardianReviewResponse`,
      `ThreadGuardianAssessment`, `GuardianAssessmentOutcome`.
  * Changed definitions:
    * `CommandExecutionApprovalKind` gains `humanCommand`. Its schema changed shape from a plain
      `enum` to a `oneOf` of two enums.
    * `InitializeCapabilities` gains `requiredHumanApproval`.
    * `OtelRequirements` gains `log_sse_deltas`.
    * `ThreadSampleParams` gains `interruptOnCancel` (experimental).
    * The audio `UserInput` no longer requires `url`, and accepts `fileId` instead
      (`AudioReference`, `app-server-protocol/src/protocol/v2/turn.rs`).
  * Nothing was removed. There are no new item types and no new server requests.
* **F4 — The gate sees methods and required fields, nothing else.** It compares method sets and
  each method's required fields (`live_contract_test.go:46-51`). A new enum value, a new
  capability field, and a field that stops being required (or is renamed while optional) are all
  invisible to it. That is why F2 lists three changes and F3 lists ten.
* **F5 — `thread/section/changed` is stable.**
  * In the source, it has no `#[experimental]` attribute (`common.rs`, the `ThreadSectionChanged`
    entry).
  * On the wire, it arrived to a client that initialized with `experimentalApi: false`: once for a
    move into a section, and once for a move out (`sectionId: null`). The params were `threadId`
    and `sectionId`.
  * The app-server broadcasts it after every `thread/section/move` "to its connected clients"
    (`codex-rs/app-server/README.md`).
  * Magic's route table has no entry for it (`routing.go:26-115`), and an unrouted notification is
    logged at Debug and dropped. The routing test fails as soon as the manifest declares it, as it
    did at the last re-pin (0226-PLAN R3 Deviation 2).
* **F6 — Who would send it.** In the default stdio mode Magic starts its own app-server, so Magic
  is the only client that can move a thread. Another client's move reaches Magic only under the
  managed daemon or `proxy` launch modes (`launch.go:16-53`), where the desktop app or the TUI can
  share the daemon. The phone reads sections on demand (`codex.threads.read`), and reads them again
  after each of its own section actions (`codex_threads_screen.dart`). Magic has no push for
  thread-list changes.
* **F7 — Managed human-approval checkpoints are experimental, and refuse non-capable clients.**
  * A managed `requirements.toml` prefix rule with `decision = "prompt-user"` now requires a fresh
    human decision, even under automatic review. The server sends
    `item/commandExecution/requestApproval` with `kind: "humanCommand"`, and only `accept`,
    `decline` and `cancel` are valid answers.
  * A client receives these only if it initialized with both `experimentalApi` and
    `requiredHumanApproval` (`initialize_processor.rs`). "With no capable connection when approval
    is requested, the command is rejected" (app-server README).
  * Magic declares neither, so it never receives one. If it did, its decoder would answer the
    unknown kind as an ordinary command (`callbacks.go:131-140`).
  * On the wire, `requiredHumanApproval: true` was accepted whether or not `experimentalApi` was
    also set. Without `experimentalApi`, codex ignores it without saying so.
* **F8 — MCP tool approvals now name the tool.**
  * The MCP tool-approval elicitation's `_meta` gains `tool_name`, plus `tool_annotations`
    carrying `readOnlyHint`, `destructiveHint`, `idempotentHint` and `openWorldHint` where the
    server set them (commit `4bc746c94f`). `tool_title`, `connector_name` and
    `codex_approval_kind` were already there (`codex-rs/protocol/src/mcp_approval_meta.rs`).
  * The schema types `_meta` as `true` (any JSON), so these keys are published only as Rust
    constants.
  * Magic's sheet for `mcpServer/elicitation/request` shows only the server name
    (`callbacks.go:155-158`). The operator approves a tool call without being told which tool it
    is.
* **F9 — The CLI surface Magic drives is unchanged.**
  * Every subcommand and flag Magic passes still parses:
    * `login --with-api-key`, `login --device-auth`, `login status` and `logout`;
    * `app-server --listen` with `off`, `stdio://`, `unix://` and `ws://`;
    * `app-server proxy`, and the `app-server daemon` operations.
  * The daemon and transport sources did not change; `outgoing_message.rs` gained only the
    tracking for F7's approvals. The `--ws-auth` modes are unchanged.
  * `doctor --json` still has `schemaVersion: 1`, and its keys match `rawDoctorCheck` and
    `DoctorIssue`, which are decoded with `DisallowUnknownFields` (`diagnostics.go:130`).
  * One doctor check id, `sandbox.filesystem_paths`, is not in `knownDoctorChecks`
    (`diagnostics.go:113`). It has been there since before 0.166.0 (commit `b33199b1fb`), and it is
    projected as id, status and summary only.
  * Marked `[experimental]`: `app-server` (with `generate-ts` and `generate-json-schema`),
    `remote-control`, `cloud`, `exec-server` and `migrate-nfs-db`. There is no `acp` subcommand.
* **F10 — Feature flags arrive without code.**
  * Promoted: `deferred_tool_world_state` from under-development to stable and on by default;
    `respect_system_proxy` from under-development to experimental.
  * New stable flags, both off by default: `suppress_model_change_announcements`, and
    `gcp_setup_wizard` (TUI sign-in only).
  * The other new flags are under development.
  * Magic reads the catalog from `experimentalFeature/list` (`runtime.go:271`) and names no flag
    in code, so all of these appear on the phone as they are.
* **F11 — Two behaviour changes that touch Magic's paths.**
  * A full `thread/resume` can now fail with "saved history is missing completed assistant items
    for turn …" (commit `fe16bc0a97`, `thread_lifecycle.rs`). This is a new error on a call Magic
    makes, and it surfaces through the existing resume-error path.
  * Network approval prompts are now closed with `serverRequest/resolved` when their request ends
    (commit `4969675f01`). Magic already routes that notification to the session, so a stale
    prompt now clears from the phone with no change on Magic's side.
  * Magic sends no audio (`session.go:431`), so the audio change in F3 does not reach it.

### What Magic generates, and what it still keeps by hand [observed]

* **F12 — Generated today:**
  * the method inventory, and each method's stability, from the two schema bundles;
  * the `implemented` classification, from this package's own code;
  * the required fields of each fixture;
  * the binary's identity;
  * the source-watch delta;
  * the notification stability split, from a scan of `#[experimental]` in `common.rs`.

  ([docs/guides/ops-codex-contract.md](../guides/ops-codex-contract.md),
  [0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md](0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md)
  D3 and D19.)
* **F13 — Kept by hand, though codex's source states it:**
  * **Method to response type.** The generator guesses a response type's name from the method,
    and keeps a hand-written override map for 19 methods whose names it cannot guess
    (`contract_generate_test.go:305-327`). Five of those were added at the last re-pin, after the
    tests failed (0226-PLAN R3 Deviation 2). Yet `client_request_definitions!` in `common.rs`
    declares `params:` and `response:` for every client request.
    * A probe on 2026-11-07 (scratch `probe_response_types.py`) read the `response:` of each
      entry, at both tags. Every client request in the experimental export has a source entry
      (179 at 0.166.0, 180 at 0.167.0), and every response type names a definition in the
      export.
    * All 19 override entries agree with the source. The only source entries missing from the
      export are the three v1 methods `export.rs` strips (`getAuthStatus`,
      `getConversationSummary`, `gitDiffToRemote`).
    * The 16 entries that a naive parser misses are exactly the overridden ones: their `params:`
      carries `#[ts]` and `#[serde]` attributes before the type, so the parser has to allow for
      them.
  * **The notification split is computed but not kept.** The capture prints it, and it is written
    only into the version's README. The manifest does not carry it, because a new field waits for
    `schema_version` 2 (0163).
  * **Enum values.** The values Magic switches on (approval kinds, item types, statuses) appear
    nowhere in the manifest. F3's `humanCommand` passed the gate unseen.
* **F14 — Nothing checks the names Magic decodes.**
  * The non-test files of `internal/provider/codex` carry 274 distinct `json:"…"` names. 75 of
    them appear as a property nowhere in 0.167.0's experimental export.
  * 73 of the 75 belong to Magic's own output types (the runtime snapshot, the manifest, the
    capability snapshot), or to inputs that are not app-server messages (`doctor --json`, the
    daemon's JSON, `auth.json`).
  * Two are app-server decodes with no source in the schema:
    * `capability` in `rpcErrorBody` (`conn.go:98`). This is the field that is never sent
      ([0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md),
      proposed D39).
    * `body` in the review fallback's object branch (`review.go:268-272`), where the schema
      declares `review` a required **string** on both review items.
  * The same branch also reads `text` and `content`. Those names exist elsewhere in the schema,
    so a check by name alone does not catch them. That is the limit of a check by name.
* **F15 — Full code generation is the upstream route, at a cost.**
  * codex's Python SDK generates its models (`sdk/python/src/openai_codex/generated/v2_all.py`,
    14,229 lines) and a notification registry from the same v2 bundle. It uses
    `datamodel-code-generator`, after about 300 lines of schema normalization
    (`sdk/python/scripts/update_sdk_artifacts.py:256-575`).
  * One of those normalizers is `_flatten_string_enum_one_of`, for exactly the shape
    `CommandExecutionApprovalKind` took in 0.167.0 (F3).
  * Separately, four `configRequirements/read` fields are omitted from every export, so generated
    types would silently drop admin-managed hooks and network policy (0163 F35).

## Decision Drivers

* **D-1 — A pin names what was checked.** It needs a capture of that release, and the live suite
  run against it (0226 D-1).
* **D-2 — Quota is the owner's.** Each live run is named, and waits for the owner's word.
* **D-3 — A fact codex states should be read from codex, not retyped.** Every hand-kept copy has
  already gone stale at least once (F13; 0163 F3 and F12).
* **D-4 — A gate that cannot see a change cannot warn about it** (F4).
* **D-5 — No behaviour change the release does not call for,** unless the owner adopts it on its
  own merits (F7, F8).

## Considered Options

* **A — Re-pin by the documented procedure, and nothing else,** as 0226 D3 did.
* **B — Re-pin, and extend the generated contract:** response types from `common.rs`, enum values
  and the notification split kept in the manifest, and a check that every name Magic decodes has a
  source.
* **C — Re-pin, and generate Go types from the schema bundle,** as codex's Python SDK does.
* **D — Hold the pin at 0.166.0** until a release needs a change.

## Decision Outcome

Chosen option: "B", because it closes the gaps 0.167.0 exposed (F4, F13, F14) using inputs the
capture already reads. A gives the same pin while keeping the override map, which will fail at the
next re-pin as it did at the last, and an enum gate that sees no enums. C trades about 300 lines of
upstream's own normalization work, and 0163 F35's silent field loss, for types Magic mostly does not
read. D leaves the pin behind the binary that runs, against the owner's stated intent.

### The decisions

* **D1 — Re-pin codex to 0.167.0 by the documented procedure.**
  * The contract is regenerated by `scripts/codex-contract.ps1`, from a scratch clone checked out
    at `086ccc833bc9…` (F1). The owner's checkout is not touched.
  * The three `//go:embed` directives point at `testdata/0.167.0/`, and
    `KnownGoodVersion = "0.167.0"`. `testdata/0.166.0/` is deleted, as the procedure's step 5
    says.
  * `contract_test.go` asserts the new manifest's version, digest, counts and commit.
  * `testdata/wire/0.167.0/` gains one live turn's frames, including the initialize answer and
    token usage, read and redacted as the 0.166.0 capture was. That turn is billed (D-2).
  * `make live-codex-contract` must pass in both of its modes, and `make live-codex` must pass.
* **D2 — `thread/section/changed` is routed as a declared drop (Q1 A, owner 2026-11-07).** The two
  shapes seen on the wire in F5 become a fixture, and a test drives both through the route.
* **D3 — The generated contract grows by four facts, under `schema_version` 2.**
  1. **Response types from the source.** The generator reads `params:` and `response:` from
     `client_request_definitions!` in `common.rs` at the tag, which `-SourceTree` already
     provides. The hand-written override map is deleted. The generator fails on any client
     request in the bundle that has no declaration in the source.
  2. **Enum values.** For every string enum in the bundles, the manifest keeps the set of its
     values, with a `oneOf` of enums flattened into one set (F15). In the default gate a removed
     value is breaking and an added one is reported as additive drift, the same rule methods
     follow. Exact mode fails on any difference.
  3. **The notification split.** The manifest keeps each notification's stability, as the capture
     already computes it (F12). The guide's warning that a "stable notification" in the manifest
     only means "declared" is withdrawn. This is the persistence 0163 deferred to
     `schema_version` 2.
  4. **A check of the names Magic decodes.** A unit test, which needs no binary, walks the
     package's non-test Go files with `go/ast`. It collects the JSON names of struct types used
     to decode app-server messages, and requires each name to be a property somewhere in the
     manifest's recorded schema.
     * Types that are not app-server messages are excluded by type, each with a reason: Magic's
       own outputs, `doctor --json`, the daemon's JSON, `auth.json` and the manifest itself.
     * A wire name with no source in the schema is either removed, or listed with the evidence
       for keeping it.
     * F14's two names go first: `review.go`'s object branch is removed, because the schema says
       `review` is a string, and `conn.go`'s `capability` stays listed under 0109's D39 until
       that lands.
* **D4 — MCP approvals show the tool (Q2 A, owner 2026-11-07).**
  * The `mcpServer/elicitation/request` sheet names the connector or server, the tool's title
    and its `tool_name`, and says so plainly when `destructiveHint` is true. This changes only
    the daemon's text, not the protocol.
  * The `_meta` key names Magic reads are scanned from `mcp_approval_meta.rs` into the
    source-watch manifest, so a rename shows up as drift.
* **D5 — Managed human-approval checkpoints are deferred, and no experimental surface is adopted
  (Q3 A, owner 2026-11-07: "defer, no experimental").** Magic does not declare
  `requiredHumanApproval`. Revisit when codex promotes it to stable, or when a managed policy is in
  use. The "no experimental" half also covers D6's experimental items.
* **D6 — Not adopted now, each with its reason:**
  * `thread/guardianReview`: experimental, and it reviews only an injected command on an idle
    thread.
  * Audio file references: Magic has no audio input (F11).
  * `interruptOnCancel`: experimental, and Magic does not call `thread/sample`.
  * `log_sse_deltas`: a managed logging policy, with nothing for Magic to show.
  * The feature flags: they reach the phone through the catalog already (F10).
  * `sandbox.filesystem_paths`: it predates this release (F9), and adding it to
    `knownDoctorChecks` is separate work.
* **D7 — Order.**
  * P1 first: D3's generator changes, at the current 0.166.0 pin. That run should change nothing
    except the new fields, which proves the generator on a known release.
  * P2: the re-pin (D1, D2).
  * P3: D4.
  * P4: the docs.
  * Each phase is its own commit.

### Q1 — `thread/section/changed`

* **A — A declared drop (recommended).** Route it to the provider and log it at Debug, as 0226 did
  for the three notifications new in 0.166.0. In stdio mode the only mover is Magic, and the phone
  already re-reads after its own moves (F6). Revisit when Magic adds any thread-list push.
* **B — Push a thread-list refresh to the phone.** A new protocol message, sent when this
  notification (and later `thread/name/updated`, `thread/archived` and similar) arrives, with the
  phone re-reading on receipt. This touches the protocol and the app, and helps only in the shared
  daemon modes.
* **C — Parse it and patch the daemon's cached section membership.** Magic keeps no such cache
  today, so this means adding one.

### Q2 — Show the tool on MCP approvals (F8)

* **A — Adopt (recommended).** The approval is stable, the keys have been there for several
  releases (`tool_title`, `connector_name`), and an operator should know which tool they are
  approving.
* **B — Defer** until Magic touches the approval sheet for another reason.

### Q3 — Managed human-approval checkpoints (F7)

* **A — Defer, and watch (recommended).** It is experimental. It matters only under a managed
  `requirements.toml`, and none is in use here. It also needs a sheet with no session or
  persistent approvals. Revisit when it is promoted to stable, or when a managed policy is in use.
* **B — Adopt now behind the experimental negotiation** Magic already performs: declare
  `requiredHumanApproval`, render `humanCommand` with only `accept`, `decline` and `cancel`, and
  test it against an engine with a fake managed rule. A phone session would then be able to
  answer checkpoints, where today the command is rejected.
* **C — Never.** A command under a managed `prompt-user` rule stays rejected while Magic is the
  only client.

### Consequences

* Good, because the pin names the binary that runs, with a capture of it.
* Good, because the next re-pin is not stopped by an override map (D3.1), and a new enum value is
  reported rather than missed (D3.2).
* Good, because a name Magic decodes with no source in codex's schema is found by a test, not by a
  reader (D3.4). F14 found two.
* Good, because the operator sees which MCP tool they are approving (Q2 A).
* Bad, because while Magic is the only client, a command under a managed `prompt-user` rule stays
  rejected (Q3 A, F7).
* Neutral, because the manifest's `schema_version` moves to 2, and every reader of the manifest
  moves with it in the same phase.
* Bad, because the capture turn and `make live-codex` spend quota (D-2).
* Bad, because D3.4's check works by name, not by path. It misses a wrong guess whose name exists
  somewhere else in the schema (F14's `text` and `content`). A check by path needs each decoder
  tied to its message type, and that is not proposed here.

### Confirmation

1. P1 regenerates the 0.166.0 manifest at `schema_version` 2. Its methods, classifications and
   fixtures are unchanged, and only the new fields are added. With the override map deleted, every
   response type resolves.
2. Each new check is seen failing on a scratch copy:
   * the enum gate on a manifest missing `humanCommand`;
   * the source-declaration check on a bundle method deleted from a copy of `common.rs`;
   * the decoded-name check on a planted struct field `json:"planted-name-3f9a"`.
3. After P2, `make live-codex-contract` in both modes, and `make live-codex`, pass on the owner's
   word. `TestPinMatchesTheFixtureItCites` and the token-usage test read 0.167.0's capture.
4. The routing test passes with `thread/section/changed` routed, and the fixture test drives both
   shapes from F5.
5. A test renders an MCP approval from a `_meta` carrying `tool_name`, `tool_title`
   and `destructiveHint: true`, and fails on today's code.
6. `go test -race ./...`, `make pre-add-check`, `make vet-tags`, and `make check-records
   markdownlint-docs` pass. The guard's redact preview runs on every capture before staging.

## Pros and Cons of the Options

### A — Re-pin only

* Good, because it is the smallest change, and the procedure is proven (0226 R3).
* Bad, because the override map and the gate's blindness to enums both stay, and F13 and F4 recur
  at the next re-pin.

### B — Re-pin, and extend the generated contract

* Good, because every fact it adds is already in an input the capture reads (D-3).
* Good, because it makes the gate see the class of change 0.167.0 actually made (D-4).
* Neutral, because it moves the manifest to `schema_version` 2, which 0163 already foresaw.
* Bad, because it is more work than A, and the decoded-name check is coarse (it works by name).

### C — Generate Go types

* Good, because it is what upstream does, and it would cover every name and path.
* Bad, because the bundle needs normalization before a generator can read it (F15), and that work
  would be ported and maintained here.
* Bad, because generated types would drop the four managed-policy fields (0163 F35) without
  anyone noticing.
* Bad, because Magic reads a small part of each message, and generated types for every message
  would add code nobody reads.

### D — Hold at 0.166.0

* Good, because it costs nothing now.
* Bad, because every check would keep running against a release that no longer runs here (D-1),
  and the owner has said the pin moves.

## More Information

### The owner's answers

*Answered by the owner, 2026-11-07:* "q1. as recommended. q2. adopt it. q3. defer, no experimental.
q4. approved to probe/capture."

* Q1: A, a declared drop (D2).
* Q2: A, adopt (D4).
* Q3: A, defer, with no experimental surface adopted (D5).
* Live runs: the owner approved the probes and P2's capture turn. `make live-codex`, the billed
  live suite in Confirmation 3, is asked for separately when P2 reaches it.
* The record and its PLAN: approved with "committed proceed", 2026-11-07.

### Relationship to other records

* [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)
  D10: why this pin has its own record.
* [0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)
  D3, and its PLAN's R3 and R3 Deviations 1 and 2: the procedure D1 follows, and the failures
  P2 should expect.
* [0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md](0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md)
  D3 and D19: the gate and the generated contract that D3 extends. D3.3 is the `schema_version` 2
  persistence 0163 deferred.
* [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md),
  amendment of 2026-11-07 (proposed):
  * D40's captured error fixtures go under the pin in force when its P1 runs: `testdata/0.167.0/`
    if this record's P2 has landed by then.
  * D39's dual-run ends at "the first re-pin after it lands". If this re-pin lands first, it does
    not count.
  * D42's weekly canary would have reported F2 on its own.
* GitHub issue #31: this is a re-pin of the kind the issue's W1 asked for.
