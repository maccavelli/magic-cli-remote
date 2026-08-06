# Implement MADR 0076 — Kilo debug-pass remediation

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

Associated MADR: [0076-MADR-kilo-debug-pass.md](0076-MADR-kilo-debug-pass.md)

- **Status**: proposed — not yet implemented.
- **Date**: 2026-08-06
- **Scope**: Every finding in MADR 0076 §1 (H1, M1, M2, M3, M4, L1–L3) taken
  from "found and verified" to "fixed and regression-tested." Covers
  `internal/provider/kilo/*`, `internal/daemon/daemon.go`,
  `internal/session/commands.go`, and
  `apps/mobile/lib/features/chat/{chat_screen,chat_helpers}.dart` plus their
  test suites.
- **Non-goals**:
  - `session_tree` flip / child-suppression / subagent-lifecycle test
    porting (`child_suppression_test.go`, `subagent_test.go`,
    `idle_confirm_test.go`, `resync_tree_test.go`, and the tree-scoped half
    of `lifecycle_test.go` — `TestSessionCreatedBindsChildAndCard`,
    `TestSessionIdleParentOnlyEndsTurn`, `TestSessionIdleBlockedByBusyChild`,
    `TestSessionDeletedCompletesCard`, `TestConfirmTreeIdleFiltersGlobalStatus`,
    `TestConfirmTreeIdleReportsBusyChild`). MADR 0075 PD2/Q7 already deferred
    this pending live child-SSE fixtures; MADR 0076 §4 M4 only asks that the
    flip not happen *without* first porting that suite — it does not ask for
    the port now. Re-scope into its own plan when Q7 resolves.
  - The daemon-side "capability bit instead of provider-id allowlist" rework
    MADR 0076 H1 floats as the *better* long-term fix. This plan takes the
    narrower, MADR-explicit fallback (widen the client-side allowlist) to
    keep blast radius to the mobile package and match the codebase's
    existing pattern (`_spawnOnly => provider == 'grok'`,
    `chat_screen.dart:2858`). A capability-bit rework is a separate,
    larger MADR if the owner wants it later (see D1).
  - `L2` (tool-frame instrumentation hook / `live_tool_stream_test.go`
    parity) is optional tooling, not a correctness fix — included as P9,
    explicitly skippable.
  - No protocol/wire changes. No config schema changes. No changes to
    `internal/provider/opencode/*` (source of truth, untouched).
- **Grounding**: Tree as of 2026-08-06 (MADR 0076 authoring commit, no
  intervening changes). Every file:line cited below was re-read directly
  before this plan was written — see §0.

---

## 0. Grounding — re-verified code facts that bound this plan

MADR 0076 assessment: **all cited findings hold**. Every file:line below was
re-read against the current tree immediately before writing this plan; none
had drifted from what MADR 0076 recorded.

| Fact | Where (re-verified) |
| --- | --- |
| Mobile session-actions menu gates Diagnostics/Diff/Fork on `_provider == 'opencode'` | `apps/mobile/lib/features/chat/chat_screen.dart:2075` |
| Kilo backend implements the same ops (`Diagnostics`, `Fork`, `Diff`, `Compact`, `SetModel`, `UndoLast`, `Revert`, `Unrevert`, `Rename`) via the same optional interfaces | `internal/provider/kilo/session_ops.go:21,38,109,130,145,159,175,198,227`; interfaces at `internal/provider/httpagent/session.go:141,152,186` |
| No widget test currently exercises the session-actions popup menu | confirmed absent: `find apps/mobile/test -iname "*chat_screen*" -o -iname "*popup*menu*"` → empty |
| No existing "provider family" helper/constant anywhere in the codebase | confirmed absent: `grep -rn "httpFamily\|providerFamily\|isHttpAgent" apps/mobile/lib internal/` → empty |
| `defaultMode` hardcodes `["default", "build"]`, no `kilo`/`code` case | `internal/session/commands.go:570` |
| `resolveDisplayedMode` hardcodes `['default', 'build']`, no `code` case | `apps/mobile/lib/features/chat/chat_helpers.dart:27` |
| Existing Dart test file to extend for M1 (pattern: one `const` mode list per provider + a `test('empty current prefers X on Y-shaped list')` case) | `apps/mobile/test/resolve_displayed_mode_test.dart` (codex/opencode/goose cases already present, lines 7–21, 28–39) |
| Existing Go test file to extend for M1 (pattern: one table-row per provider) | `internal/session/defaultmode_test.go` (opencode/grok/codex/goose/custom-agent cases present, lines 20–70) |
| Kilo's live agent order has `code` first (why M1 hasn't broken yet) | `docs/kilo-spike-7.4.20/agents-summary.json` (`code` before `ask`/`debug`/...) |
| `mode.go`'s doc comment still says "`build`" next to the correctly-adapted `normalAgentID()` | `internal/provider/kilo/mode.go:114-117` |
| `catalog_live.go`'s security comment cites `TestConnectedCatalogDropsAPIKey`, which only exists for opencode's struct | `internal/provider/kilo/catalog_live.go:118-124`; opencode's test at `internal/provider/opencode/catalog_test.go:180` |
| `daemon.go`'s "no provider ready" warning OR-chain omits `cfg.Providers.Kilo.Enabled` | `internal/daemon/daemon.go:285` |
| Coverage: kilo 14.6% vs opencode 85.4%, no races | `go test -cover ./internal/provider/kilo/... ./internal/provider/opencode/...`; `go test -race` clean on both |
| Kilo's real catalog is bigger than opencode's tested synthetic shape (172 providers/5,788 models) on both axes | `docs/kilo-spike-7.4.20/provider-summary.json` (179 providers/6,006 models spike-day), `docs/0075-PLAN-kilo-cli-provider.md:24-25` (181 providers live P3 run) |
| Function/type names in kilo mirror opencode's 1:1 (`httpDialect`, `httpSession`, `ListAgentsLive`, `ListModelsLive`, `ListModelProvidersLive`, `ListModelsForLive`, `modelsOf`, `capDefaultCatalogModels`, `providerOption`, `AfterBoot`, `emitStatus`, `noteTool`, `noteToolEmit`) — every port below is a mechanical adaptation, not a redesign | confirmed via `grep -n "^func "` diff, `internal/provider/kilo/{catalog_live,dialect,session}.go` vs `internal/provider/opencode/http.go` |
| Kilo's existing tests: `dialect_test.go` (12 tests incl. `TestSplitModelFirstSlash`), `session_test.go` (7 tests), `live_test.go` + `live_permission_test.go` (build-tagged) | `internal/provider/kilo/*_test.go`, function names enumerated by `grep -n "^func Test"` |
| Opencode's full test-name inventory for every file this plan ports from | enumerated below per phase, from `grep -n "^func Test" internal/provider/opencode/*.go` |

## 0.1 Decisions — locked for this plan

| ID | Decision | Rationale |
| --- | --- | --- |
| D1 | H1 fix shape: widen the client-side string check to a small provider-family set, not a daemon capability bit | Matches existing codebase pattern (`_spawnOnly`), zero protocol change, smallest correct fix; capability-bit rework stays a future option (non-goals) |
| D2 | Coverage backfill (M4) is split into one phase per source file/subsystem, each independently landable | MADR 0076 ranked M4 as 5 sub-items by risk; splitting into phases lets each ship as its own PR without blocking on the whole backfill |
| D3 | Ported tests are adapted 1:1 from opencode's test names/table-cases where the underlying logic is byte-identical (confirmed by MADR 0076 §2), with fixture data changed from opencode's wire shapes to kilo's (session id prefixes, event field names already diffed identical, provider/model ids using kilo's real ids from the spike) | Fastest path to real coverage; these aren't new tests being invented, they're regression nets for logic already proven correct once (MADR 0076 §2) but never protected for kilo's copy |
| D4 | `todo.go`'s `TestIsControlPlan` is **not** ported — it tests a shared `internal/event` invariant already covered by opencode's copy of the same test, not kilo-specific code | Avoids a genuinely redundant test (confirmed in MADR 0076 §1 M4 bucket-B note) |
| D5 | Catalog coverage (P6) supersedes the single-test scope of P3 (M2) — P3 ships first as an isolated, fast, security-relevant fix; P6 completes the rest of `catalog_test.go`'s 15 tests against `catalog_live.go`/`catalog.go` | Keeps the security fix (M2) shippable independently and quickly, per MADR 0076 §4 priority order, while still reaching full catalog coverage |

---

## 1. Phase P1 — Correctness quick wins (H1, M3)

Smallest, highest-impact fixes. No new abstractions, no test-porting — just
the missing entries.

### Steps

1. **H1** — `apps/mobile/lib/features/chat/chat_screen.dart:2075`:

   ```dart
   // before
   final isOpencode = _provider == 'opencode';

   // after
   final showsSessionOps = _provider == 'opencode' || _provider == 'kilo';
   ```

   Rename the local (`isOpencode` → `showsSessionOps`) and its one other use
   site (`if (isOpencode) ...` → `if (showsSessionOps) ...`) at
   `chat_screen.dart:2085`. Do not introduce a shared top-level constant yet
   (D1 — keep this a local, minimal-blast-radius fix; revisit only if a
   third httpagent-family provider ships).

2. **M3** — `internal/daemon/daemon.go:285`:

   ```go
   // before
   if anyEnabled := cfg.Providers.Fake.Enabled || cfg.Providers.Grok.Enabled ||
       cfg.Providers.Goose.Enabled || cfg.Providers.Opencode.Enabled ||
       cfg.Providers.Codex.Enabled; anyEnabled {

   // after
   if anyEnabled := cfg.Providers.Fake.Enabled || cfg.Providers.Grok.Enabled ||
       cfg.Providers.Goose.Enabled || cfg.Providers.Opencode.Enabled ||
       cfg.Providers.Codex.Enabled || cfg.Providers.Kilo.Enabled; anyEnabled {
   ```

3. New Go test: add a `kilo`-only case to whatever covers this warning
   today — check first whether `daemon.go`'s readiness-warning block has an
   existing test (`grep -rn "no agent provider is ready" internal/daemon/*_test.go`);
   if one exists, add a `kilo`-only-enabled-with-missing-binary case
   mirroring its existing per-provider cases. If none exists (readiness
   warning is untested for every provider today, not just kilo), do not
   invent a new test-infra pattern here — that's a pre-existing gap outside
   MADR 0076's scope; a one-line comment above the fixed line noting "keep
   this OR-chain in sync with `ProvidersConfig`" is sufficient.

4. New widget-level check for H1 (Flutter has no existing popup-menu test
   to extend — see §0 grounding): add
   `apps/mobile/test/chat_session_actions_menu_test.dart` with a minimal
   widget test that pumps the session-actions `PopupMenuButton` for
   `_provider = 'kilo'` and asserts the `diagnostics`/`diff`/`fork` menu
   items are present (mirrors whatever pump/harness pattern
   `apps/mobile/test/` already uses for other `chat_screen.dart` widgets —
   check `apps/mobile/test/features/chat/` for an existing harness to reuse
   before building a new one from scratch).

### Acceptance

- `go build ./... && go vet ./... && go test ./...` green.
- `cd apps/mobile && flutter analyze && flutter test` green, including the
  new menu test.
- Manual smoke: with `providers.kilo.enabled: true`, open a kilo session on
  the phone, confirm "Session diagnostics" / "View file diff" / "Fork
  session" appear in the session-actions menu and each works (this is the
  literal H1 repro from the conversation that triggered MADR 0076).

---

## 2. Phase P2 — Default-mode alignment (M1) + comment cleanup (L1)

Bundled per MADR 0076 §4 item 6 (L1's clearest instance is the exact
mode.go comment that should have caught M1 during the original fork).

### Steps

1. **M1, daemon side** — `internal/session/commands.go:570`:

   ```go
   for _, want := range []string{"default", "build", "code"} {
   ```

2. **M1, daemon test** — `internal/session/defaultmode_test.go`: add a
   `kilo` table case following the existing `codex` pattern (lines ~60-67):

   ```go
   {
       name:  "kilo",
       modes: []event.SessionMode{{ID: "code"}, {ID: "plan"}, auto},
       want:  "code",
       ok:    true,
   },
   ```

3. **M1, mobile side** — `apps/mobile/lib/features/chat/chat_helpers.dart:27`:

   ```dart
   for (final want in const ['default', 'build', 'code']) {
   ```

4. **M1, mobile test** — `apps/mobile/test/resolve_displayed_mode_test.dart`:
   add a `kilo` const mode list (mirroring the existing `opencode` list at
   lines 12-16) and an `'empty current prefers code on kilo-shaped list'`
   test case (mirroring line 34-36):

   ```dart
   const kilo = [
     SessionMode(id: 'code', name: 'code'),
     SessionMode(id: 'plan', name: 'plan'),
     SessionMode(id: 'auto', name: 'auto', dangerous: true),
   ];
   // ...
   test('empty current prefers code on kilo-shaped list', () {
     expect(resolveDisplayedMode(kilo, '')?.id, 'code');
   });
   ```

5. **M1, Settings screen** — re-check whether the "compounding gap" MADR
   0076 M1 flagged (Settings' static default-mode floor `default`/`plan`/
   `auto` never resolving for kilo) is driven by the same
   `resolveDisplayedMode` call or a separate hardcoded list; locate it
   (`grep -rn "default.*plan.*auto\|plan.*auto.*default" apps/mobile/lib/features/settings/`)
   and apply the same `code` addition if it's a distinct literal.

6. **L1** — fix the stale doc comment at `internal/provider/kilo/mode.go:114-117`
   (currently: *"session.defaultMode resolves 'return to normal' as
   `build`"*) to say `code`, matching what `normalAgentID()` a few lines
   below actually does. While in the file, do the same mechanical
   "OpenCode"→"Kilo" pass on doc comments (not string literals, which are
   already correct) in `question.go`, `todo.go`, `usage.go`,
   `session_ops.go` (MADR 0076 L1 full list).

### Acceptance

- `go test ./internal/session/... ./internal/provider/kilo/...` green,
  including the new `kilo` case in `TestDefaultModeNeverResolvesToADangerousMode`.
- `flutter test apps/mobile/test/resolve_displayed_mode_test.dart` green,
  including the new kilo case.
- `git grep -n "OpenCode" internal/provider/kilo/` returns zero hits in doc
  comments (string literals like log messages were already correct per
  MADR 0076 §1 L1 — do not touch those, only comments).
- Manual smoke: on a kilo session, run `/plan` then `/plan off`; confirm it
  lands on `code`, not whatever agent happens to sort first.

---

## 3. Phase P3 — Security regression guard (M2)

Ships independently and fast — this is the one finding with a (latent, not
active) security angle.

### Steps

1. Add `internal/provider/kilo/catalog_test.go` (new file) with
   `TestConnectedCatalogDropsAPIKey`, adapted from
   `internal/provider/opencode/catalog_test.go:180` against kilo's own
   `connectedProvidersResponse` struct (`internal/provider/kilo/catalog_live.go:118-127`)
   and kilo's real field/endpoint names (`GET /config/providers`, per MADR
   0075 §2.4). Read opencode's version first to confirm the exact assertion
   shape (decode a raw JSON blob containing a `key` field per provider,
   assert the decoded Go struct has no way to surface it — field-absence
   check, not a runtime redaction check).
2. Confirm the comment at `catalog_live.go:124` now points at a test that
   actually exists in this package (no comment change needed if the test
   name matches exactly, which it should per D3).

### Acceptance

- `go test ./internal/provider/kilo/... -run TestConnectedCatalogDropsAPIKey -v`
  passes.
- `go test ./internal/provider/kilo/...` still green as a whole.

---

## 4. Phase P4 — Reconnect/resync regression coverage (M4 #1, highest risk)

Highest-ranked M4 item: `resync.go` runs on every SSE-gap reconnect
regardless of `session_tree`, and currently has zero coverage.

### Steps

1. Add `internal/provider/kilo/resync_test.go`, porting from
   `internal/provider/opencode/http_resync_test.go` (all 7 tests):
   - `TestResyncRecoversMissedTurnEnd`
   - `TestResyncLeavesLiveTurnAlone`
   - `TestResyncIgnoresStaleEvidence`
   - `TestResyncIgnoresPendingUserMessage`
   - `TestResyncRecoversAbortedTurn`
   - `TestResyncRecoversErroredTurn`
   - `TestResyncDoesNotDuplicateStreamedText`

   Adapt fixture payloads to kilo's SSE shapes where they differ (event
   type names already confirmed identical per MADR 0075 §2.4 / MADR 0076
   §2 — the fork is byte-identical here, so this is largely a mechanical
   port of the opencode test file with `package opencode` → `package kilo`
   and any opencode-specific session/model id strings swapped for kilo
   equivalents for readability, not because the assertions differ).
2. Explicitly do **not** port `resync_tree_test.go`'s
   `TestResyncKeepsTurnWhenChildBusy` / `TestResyncReemitsTreePermission` —
   those are `session_tree`-gated (non-goals, §0 above).
3. While writing these tests, exercise the caveat MADR 0076 §1 M4 #1
   flagged: confirm (via a test, not just re-reading code) that
   `discoverTreeChildren`/`resyncTreeState` run their REST/status-check leg
   even with `session_tree: false`, and that a `TreeIdleConfirmer` failure
   in that leg cannot corrupt `resyncParentMessageTurn`'s stale-evidence
   guard. If this turns up an actual bug (not just an untested-but-correct
   path), stop and add a finding + fix rather than papering over it with a
   test that encodes the bug as "expected."

### Acceptance

- `go test ./internal/provider/kilo/... -run TestResync -v` — all 7 pass.
- `go tool cover -func` shows `resync.go`'s `Resync`,
  `resyncParentMessageTurn` at non-zero, meaningfully-branch-covered
  percentages (not just line-touched).

---

## 5. Phase P5 — Quota/hard-limit regression coverage (M4 #2)

The MADR-0073 "don't hang on quota" logic, scoped narrowly to the two
non-tree-gated tests (§0 non-goals excludes the tree-scoped half of
`lifecycle_test.go`).

### Steps

1. Add `internal/provider/kilo/lifecycle_test.go`, porting from
   `internal/provider/opencode/lifecycle_test.go`:
   - `TestSessionStatusRetryNotice` (line 221)
   - `TestSessionStatusRetryHardLimitEndsTurn` (line 245)
2. Do not port `TestSessionCreatedBindsChildAndCard`,
   `TestSessionIdleParentOnlyEndsTurn`, `TestSessionIdleBlockedByBusyChild`,
   `TestSessionDeletedCompletesCard`, `TestConfirmTreeIdleFiltersGlobalStatus`,
   `TestConfirmTreeIdleReportsBusyChild` in this phase — tree-scoped,
   deferred (§0 non-goals). If any of the first four turn out to be
   tree-independent on closer read (their names suggest some might be, e.g.
   `TestSessionDeletedCompletesCard`), pull them forward into this phase;
   verify against the gate logic at `httpagent/session.go:865-873,881-890`
   before deciding.

### Acceptance

- `go test ./internal/provider/kilo/... -run TestSessionStatusRetry -v` —
  both pass, hard-limit case confirms `EndTurn()` fires and an error card is
  emitted for `agenterr.KindQuota` classification.

---

## 6. Phase P6 — Catalog & model-picker backend coverage (M4 #3, supersedes P3's single test)

`catalog_live.go` is ~0% covered end to end, and its capping logic is
untested even at a scale smaller than kilo's real catalog. This phase
brings kilo's catalog/model-picker backend to opencode-equivalent coverage.

### Steps

1. Extend `internal/provider/kilo/catalog_test.go` (created in P3) with the
   remaining 14 tests from `internal/provider/opencode/catalog_test.go`:
   - `TestAfterBootPrefersFallbackOrderOverEngineDefault`
   - `TestAfterBootFallsBackToEngineDefault`
   - `TestAfterBootKeepsSeedOnFetchError`
   - `TestListModelsLiveIsConnectedOnly`
   - `TestListModelsLiveOrdersNewestFirst`
   - `TestListModelProvidersLive`
   - `TestListModelsForLiveUnknownProviderIsEmpty`
   - `TestListModelsForLiveFallsThroughToFullCatalog`
   - `TestListModelsLiveSkipsTheExpensiveEndpoint`
   - `TestListModelsLiveUsesDialectFallbackDefault`
   - `TestListAgentsLiveSortsPrimaryFirst`
   - `TestListAgentsLiveDefaultsToFirstPrimary`
   - `TestListCommandsLiveSortsAndUsesHints`
   - `TestStaticCatalogs`

   Preserve PD4's constraint while porting: any fallback-model assertions
   must use kilo's real seeds (`kilo/kilo-auto/free`,
   `openrouter/openrouter/free`) — never introduce a hardcoded
   `kilo-auto/balanced` into a test fixture, matching the production-code
   rule MADR 0075 PD4 already locked.

2. Add `internal/provider/kilo/catalog_size_test.go`, porting from
   `internal/provider/opencode/catalog_size_test.go`:
   - `TestCapDefaultCatalogModelsKeepsDefault`
   - `TestDefaultCatalogFitsTheFrame`
   - `TestProviderCatalogFitsTheFrame`

   **Do not reuse opencode's measured constants.** Replace
   `realProviderCount = 172` / `realModelCount = 5788` /
   `realConnectedCount = 3` / `realConnectedModels = 113` with kilo's own
   measured shape: `realProviderCount = 181`, and a model count sourced from
   `docs/kilo-spike-7.4.20/provider-summary.json` (confirm the exact number
   there — MADR 0076 M4 #3 cites "179 providers / 6,006 models" spike-day
   and "181 providers" from the later P3 run; use the P3 run's paired
   model count if `provider-summary.json` or the P3 test log records one,
   otherwise scale `syntheticFull()`'s per-provider model density from the
   spike-day 6,006/179 ratio applied to 181 providers — document whichever
   choice is made in a code comment, since this is exactly the kind of
   "measured against a stale spike" drift the MADR is trying to prevent for
   the *next* debug pass). Keep the frame-budget assertions (32 KiB / 64 KiB
   relay caps) identical — those are transport-layer constants, not
   kilo-specific.

### Acceptance

- `go test ./internal/provider/kilo/... -run "TestAfterBoot|TestListModels|TestListModelProviders|TestListAgentsLive|TestListCommandsLive|TestStaticCatalogs|TestCapDefaultCatalogModels|TestDefaultCatalogFitsTheFrame|TestProviderCatalogFitsTheFrame" -v`
  — all pass.
- `go tool cover -func ./internal/provider/kilo/...` shows `catalog_live.go`
  at opencode-comparable coverage (target: no 0% functions remaining in
  that file save genuinely dead/unreachable branches, if any turn up).
- `TestProviderCatalogFitsTheFrame`'s frame-size assertion passes against
  kilo's *larger* real catalog shape — this is the concrete regression net
  MADR 0076 M4 #3 was missing.

---

## 7. Phase P7 — Auto-approve & auto-mode safety-path coverage (M4 #4)

The MADR-0044 "dangerous" feature. Highest product-safety stakes of the
coverage backlog — a regression here is a stuck-or-double-answered
permission, not just a missing UI feature.

### Steps

1. Add `internal/provider/kilo/autoapprove_test.go`, porting all 10 tests
   from `internal/provider/opencode/autoapprove_test.go`:
   - `TestAutoApprovalSummaryConcurrent`
   - `TestAutoApproveAnswersInsteadOfSurfacing`
   - `TestAutoApproveClaimsThePermissionID`
   - `TestAutoApproveDedupesDoubleShape`
   - `TestAutoApproveHandlesV2Shape`
   - `TestAutoApproveOffSurfacesSheet`
   - `TestAutoApproveFallsBackToSheetOnPersistentFailure`
   - `TestAutoApproveRetriesThenSucceeds`
   - `TestAutoApproveStopsWhenClaimedElsewhere`
   - `TestConfigAlwaysApproveStillWorks`
2. Add `internal/provider/kilo/automode_test.go`, porting the
   **non-tree-scoped** 9 of 9 tests from
   `internal/provider/opencode/automode_test.go` (all of them are
   tree-independent per a re-read of the file — none reference
   `TreeIdleConfirmer`/child sessions):
   - `TestAutoModeIsAdvertisedLast`
   - `TestAutoModeIsMarkedDangerous`
   - `TestNormalAgentID`
   - `TestSetModeAutoArmsAndKeepsNormalAgent`
   - `TestSetModeAwayFromAutoDisarms`
   - `TestSetModeUnknownLeavesAutoUntouched`
   - `TestCurrentModeReportsAutoWhenArmed`
   - `TestArmingAutoSweepsPendingPermissions`
   - `TestAutoApproveNotRestoredOnResume`

   For `TestNormalAgentID` specifically: assert it resolves to `code` (not
   `build`) for kilo — this is the test that would have caught M1 if it had
   existed at fork time; note the connection explicitly in the test's doc
   comment so a future reader sees why kilo's expected value differs from
   opencode's.

### Acceptance

- `go test ./internal/provider/kilo/... -run "TestAutoApprove|TestAutoMode|TestNormalAgentID|TestSetModeAuto|TestCurrentModeReportsAuto|TestArmingAuto" -v`
  — all 19 pass.
- `go test -race ./internal/provider/kilo/...` stays clean (auto-approve is
  concurrency-sensitive — `TestAutoApprovalSummaryConcurrent` specifically
  exercises this).

---

## 8. Phase P8 — Remaining session-loop coverage (M4 #5)

Closes out the rest of the ranked backlog: questions, todos, tool-dedup
helpers, session ops, and the Create/Prompt wire-key asymmetry trap.

### Steps

1. Add `internal/provider/kilo/question_test.go`, porting from
   `internal/provider/opencode/question_test.go`:
   - `TestQuestionAskedEmitsRequest`
   - `TestQuestionRejectedEmitsResolved`
   - `TestRespondQuestionReplyAndReject`
2. Add `internal/provider/kilo/todo_test.go`, porting from
   `internal/provider/opencode/todo_test.go` **excluding** `TestIsControlPlan`
   (D4 — shared `internal/event` invariant, not kilo-specific, already
   covered by opencode's copy):
   - `TestMapOpenCodeTodos` (rename to `TestMapKiloTodos` if the underlying
     kilo function has its own name — check `todo.go` for whether it's
     literally `mapOpenCodeTodos` still, per MADR 0076 L1's note that
     `todo.go` retains "OpenCode" in identifier-adjacent prose; if the
     function itself is still named `mapOpenCodeTodos`, rename it to
     `mapKiloTodos` as part of the L1 cleanup in P2, then this test targets
     the renamed function — sequence P2 before P8 for this file)
   - `TestMapOpenCodeTodosEmptyClear` (same rename note)
   - `TestTodoUpdatedEmitsPlan`
   - `TestTodoUpdatedEmptyClearsPlan`
   - `TestResyncTodosWhileTreeBusy` — **check first** whether this is
     genuinely tree-gated (name suggests yes) before porting; if it only
     exercises the parent-turn path with a `treeBusy` bool parameter rather
     than requiring `session_tree: true`, it's in scope (parent-side resync
     logic runs regardless of the flag, per P4's finding); if it requires
     live child binding, move it to the deferred non-goals list instead.
3. Add `internal/provider/kilo/dedup_test.go`, porting from
   `internal/provider/opencode/dedup_test.go` (all 6):
   - `TestRepeatedBusyAnnouncesRunningOnce`
   - `TestBusyAfterIdleAnnouncesRunningAgain`
   - `TestErrorStatusReopensTheLatch`
   - `TestUnchangedUsageIsNotResent`
   - `TestUsageIsResentAfterTurnCleanup`
   - `TestToolDedup`
4. Add wire-key-asymmetry coverage — new file
   `internal/provider/kilo/http_model_test.go`, porting from
   `internal/provider/opencode/http_model_test.go` +
   `http_model_wire_test.go`:
   - `TestCreateModelBodyUsesID`
   - `TestPromptModelBodyUsesModelID`
   - `TestCreateAndPromptModelShapesDiffer`
   - `TestCreateAndPromptModelKeysOnWire` (the one that actually drives real
     request builders and asserts JSON keys on the wire — this is the
     specific regression net for the `{providerID, id}` vs
     `{providerID, modelID}` trap MADR 0076 M4 #5 flagged)

   Skip `TestSplitModel` from `http_model_test.go` — kilo already has
   `TestSplitModelFirstSlash` in `dialect_test.go` covering this ground
   (confirm no gap between the two before skipping; if opencode's
   `TestSplitModel` exercises a case kilo's doesn't, port just that case
   into `dialect_test.go` instead of duplicating the whole test).

5. Add `internal/provider/kilo/session_ops_test.go`, porting from
   `internal/provider/opencode/session_ops_test.go` (29 tests — the largest
   single port in this plan). Full list:
   - `TestSessionOpsCarryDirectoryScope`
   - `TestRenameUsesScopedPatchAndRejectsEmptyTitle`
   - `TestDiagnosticsAggregatesWithoutLeakingPaths`
   - `TestForkPostsMessageIDAndReturnsNewSession`
   - `TestForkRejectsEmptyID`
   - `TestRevertRequiresMessageID`
   - `TestRevertOmitsEmptyPartID`
   - `TestDiffAppendsMessageIDWithAmpersand`
   - `TestDiffEmptyIsNotAnError`
   - `TestAbortCascadesToChildren` — **tree-scoped, check before porting**
     (name suggests child-session interaction; if it requires
     `session_tree: true` to exercise meaningfully, defer it and port only
     `TestAbortReturnsParentError` / `TestAbortNoopWithoutAgentSession` from
     this cluster)
   - `TestAbortReturnsParentError`
   - `TestAbortNoopWithoutAgentSession`
   - `TestDeleteNoopWithoutAgentSession`
   - `TestReplayMapsMessageLog`
   - `TestReplaySurvivesFetchError`
   - `TestResumeVerifiesAndAdvertisesCommands`
   - `TestAdvertiseCommandsFallsBackWhenCatalogFails`
   - `TestCommandExecutedNotice`
   - `TestKindForTool`
   - `TestMapToolStatus`
   - `TestCompactSummarizesWithTheSessionModel`
   - `TestCompactWithoutAModelDoesNotCallTheEngine`
   - `TestSetModelUsesModelRefShapeInPlace`
   - `TestSetModelTreatsBareNameAsZen` — **verify this applies to kilo**;
     "Zen" is an opencode-brand-specific model-alias concept (MADR 0075
     Appendix C notes "Zen/Go" as an opencode-only product extra kilo
     explicitly does not have) — if kilo's `SetModel` has no equivalent
     "bare name" special-case, this test doesn't port 1:1; read
     `internal/provider/kilo/session_ops.go:175` (`SetModel`) first and
     either adapt the test to kilo's actual bare-name behavior or note it
     as N/A with a one-line comment explaining why (do not silently skip)
   - `TestUndoLastRevertsTheLatestUserMessage`
   - `TestUndoLastWithNothingToUndo`
   - `TestAssistantTokensBecomeUsage`
   - `TestUsageIsOnlyReportedWhenItMeansSomething`
   - `TestCommandTableOpsAreImplemented`
   - `TestEngineContract`

### Acceptance

- `go test ./internal/provider/kilo/...` — full suite green, including every
  test added across P4–P8.
- `go test -cover ./internal/provider/kilo/...` — target ≥75% (opencode is
  85.4%; kilo will land slightly lower given the explicit tree-scoped
  deferrals in non-goals, which is expected and fine — do not chase 85.4%
  by porting deferred tree tests just to hit a number).
- `go tool cover -func ./internal/provider/kilo/...` — no remaining 0%
  functions in `question.go`, `todo.go` (minus the intentionally-N/A
  tree case if any), `session_ops.go`, or the dedup helpers in `session.go`.

---

## 9. Phase P9 — Optional: tool-frame instrumentation parity (L2)

Explicitly optional. Not required to close MADR 0076 — recorded so it isn't
lost, not because it blocks anything.

### Steps

1. If live tool-stream parity testing is wanted for kilo (matching
   opencode's `live_tool_stream_test.go`), add `onToolPartUpdated` +
   `NewHTTPWithToolFrameHook` to `internal/provider/kilo/kilo.go` /
   `dialect.go`, mirroring opencode's hook exactly.
2. Add `internal/provider/kilo/live_tool_stream_test.go` (build-tagged
   `live_kilo`, requires a real `kilo serve` on the host) once the hook
   exists.

### Acceptance

- Skip this phase entirely if the owner doesn't need live tool-stream
  instrumentation for kilo — nothing else in this plan depends on it.

---

## 10. Verification (every phase)

```text
go build ./...                                        # every phase
go vet ./...                                          # every phase
go test ./...                                         # every phase
go test -race ./internal/provider/kilo/... ./internal/provider/opencode/...  # P4–P9
go test -cover ./internal/provider/kilo/...           # P3–P9, track the delta
cd apps/mobile && flutter analyze && flutter test     # P1, P2
```

Final coverage check: re-run the exact command from MADR 0076's grounding
(`go test -cover ./internal/provider/kilo/... ./internal/provider/opencode/...`)
after P8 and record the new kilo percentage — this is the concrete,
falsifiable "did the coverage gap actually close" signal for whoever reviews
the finished work against this plan.

## 11. Rollout and rollback

- **Rollout**: every phase is a normal code change — no config schema
  change, no protocol change, no migration. P1–P3 are safe to ship
  independently and immediately (small, isolated, no interdependencies
  beyond P2 needing to land before P8's todo-rename note). P4–P8 are
  additive (new test files only, except P2's rename touching `todo.go`) and
  carry no runtime behavior change — a red test in any of them is a real
  finding, not a rollout risk, since production code doesn't move.
- **Rollback**: each phase is independently revertable (delete the new test
  file(s) / revert the one-line diffs); no phase depends on a later phase's
  code (only P8's todo-test naming depends on P2 landing first, noted
  inline).
- **Sequencing**: P1 → P2 → P3 can land in any relative order but are listed
  by MADR 0076 §4 priority. P4–P8 (coverage) have no ordering constraint
  against each other or against P1–P3, except the P2-before-P8 todo-rename
  note above. P9 is fully independent and optional.
