# MADR 0076: Kilo CLI provider — debug pass (bugs, gaps, menu alignment)

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: **Findings recorded 2026-08-06; remediation plan authored
  2026-08-06 ([0076-PLAN-kilo-debug-pass-remediation.md](0076-PLAN-kilo-debug-pass-remediation.md)),
  not yet implemented.** Every finding below was independently verified
  against the current tree (read the actual source/comment/test at the cited
  file:line) after being surfaced by a parallel research pass — this is not a
  raw agent dump; the plan's own grounding pass (its §0) re-confirmed every
  citation still held before phasing the fixes.
- **Date**: 2026-08-06
- **Plan**: [0076-PLAN-kilo-debug-pass-remediation.md](0076-PLAN-kilo-debug-pass-remediation.md)
  (9 phases, P1–P9, covering every H/M/L finding in §1)
- **Deciders**: Project Owner
- **Scope**: Debug pass over the Kilo CLI provider shipped in MADR 0075
  (`internal/provider/kilo/`, its config/daemon wiring, and the mobile
  provider/model/agent-mode picker chain in `apps/mobile/lib`). Triggered by
  enabling `providers.kilo.enabled: true` for the first time on this host
  (config change only — no code change was needed; see 0075 §4.8 and the
  session that preceded this one). Goal: find bugs and gaps left by the
  opencode→kilo fork, and confirm the provider/model/mode menus are aligned
  with their daemon-side backends.
- **Method**: line-by-line diff of every `internal/provider/kilo/*.go` file
  against its `internal/provider/opencode/*.go` template; full read of MADR
  0075 and its plan; `go build/vet/test -race -cover` on both packages;
  targeted grep + manual read of the Flutter provider/model/mode picker path
  in `apps/mobile/lib`. Four parallel research passes (session/event loop,
  config/daemon/catalog backend, mobile alignment, test-coverage gap
  analysis), then independent verification of every high/medium finding by
  reading the cited lines directly.
- **Related**: [0075-MADR-kilo-cli-provider.md](0075-MADR-kilo-cli-provider.md)
  (the provider this audits), [0075-PLAN-kilo-cli-provider.md](0075-PLAN-kilo-cli-provider.md),
  0043 (models / scoped catalog, D4 API-key-drop guard), 0044 (dangerous
  auto-approve mode), 0047 (D4 default-mode resolution), 0073 (debug-pass
  format this MADR follows; also the quota/retry hard-limit logic in
  finding M3).

---

## 0. Executive summary

Kilo's session/event loop (`session.go`, `session_ops.go`, `lifecycle.go`,
`permission.go`, `question.go`, `resync.go`, `todo.go`, `usage.go`,
`command.go`, `dialect.go`) is a **faithful, correct fork** of opencode's —
diffed byte-for-byte, the only differences found were the intentional,
documented deltas (default agent `code` not `build`, transient-lifecycle
part filtering, `session.turn.close` EndTurn, Kilo-only event no-ops,
first-slash model split). No logic bug was found inside that core.

The real problems are at the **edges of the fork** — two call sites outside
`internal/provider/kilo/` that enumerate providers or mode ids explicitly and
were never updated when Kilo shipped — plus **one confirmed mobile UI bug**
that hides three working, tested session features from Kilo users, plus a
**large, concentrated test-coverage gap** (14.6% vs opencode's 85.4%) that
leaves several non-trivial ported subsystems (resync/reconnect recovery,
quota hard-limit handling, auto-approve, catalog-size capping) with zero
regression net.

Nothing here is a live-breaking P0. The two "default mode" omissions
(finding M1) currently work by accident (Kilo's live `code` agent happens to
sort first), and the mobile menu bug (finding H1) degrades Kilo to a strict
subset of opencode's UI rather than breaking it. But H1 is a real, immediate
product gap a user will notice on first use, and the coverage gap (finding
M4) is the kind of thing that turns into a silent regression the next time
someone touches this code without realizing opencode's test suite isn't
protecting Kilo's copy.

**Fix priority**: H1 (mobile menu gating) → M1 (default-mode hardcode, two
call sites) → M2 (stale security-guard comment / missing regression test) →
M4 (coverage backfill, prioritized by the ranking in §4) → low-severity
comment cleanup (L1–L3).

---

## 1. Findings

Severity scale: **High** = user-visible product gap or a latent correctness
risk with a plausible near-term trigger. **Medium** = works today, fragile,
or protects something worth a regression test that doesn't exist. **Low** =
cosmetic / doc-only.

### H1 — Mobile session-actions menu gates Diagnostics/Diff/Fork on an exact `'opencode'` string, not on transport family

- **Where**: `apps/mobile/lib/features/chat/chat_screen.dart:2075`

  ```dart
  final isOpencode = _provider == 'opencode';
  ...
  if (isOpencode) ...[
    // diagnostics, diff, fork menu items
  ]
  ```

- **Backend reality**: Kilo's `internal/provider/kilo/session_ops.go`
  implements `Diagnostics`, `Fork`, `Diff` (and `Compact`, `SetModel`,
  `UndoLast`, `Revert`, `Unrevert`, `Rename`) with the same method set
  opencode's `session_ops.go` exposes, satisfying the same optional
  `dialectFork` / `dialectDiff` / `dialectDiagnostics` interfaces
  (`internal/provider/httpagent/session.go:141,152,186`). The WS RPC
  handlers (`handleSessionFork`, `handleSessionDiff`,
  `handleSessionDiagnostics`) are provider-agnostic — they dispatch on the
  interface, not the provider id.
- **Failure scenario**: A Kilo user opens the session-actions menu and never
  sees "Session diagnostics", "View diff", or "Fork session" — three fully
  working, server-tested features — because the client-side gate checks
  `_provider == 'opencode'` instead of checking a capability bit or the
  provider's transport family (Kilo is architecturally the same
  httpagent/HTTP+SSE shape opencode is; this is exactly the class of bug the
  0075 MADR §4.8 line "No dedicated mobile MADR required if parity is 'same
  as opencode chips'" was betting against, and the bet didn't quite land at
  this one call site).
- **Confidence**: confirmed (both sides read directly).
- **Fix shape**: either broaden the check to a provider-family set
  (`{'opencode', 'kilo'}`) or — better — have the daemon advertise these
  capabilities per-session (already implied by the optional-interface
  pattern server-side) so the client doesn't need a provider-id allowlist at
  all. The narrower fix is a one-line change; the capability-bit fix avoids
  this exact bug recurring for the next opencode-family provider.

### M1 — "Return to normal mode" hardcodes `['default', 'build']` in two independent places, with no `'code'` entry

- **Where** (two call sites, not one):
  - `internal/session/commands.go:570` (`defaultMode`, daemon-side, used by
    `/plan off`)
  - `apps/mobile/lib/features/chat/chat_helpers.dart:27`
    (`resolveDisplayedMode`, mobile-side, used to paint the selected mode
    chip when `currentModeId` is unset/unknown)
- **What both do**: try an exact-id match against `default`/`build` first;
  if neither is present, fall back to "first mode in the list that isn't
  `plan` and isn't `Dangerous`". Kilo's default agent is `code` (MADR 0075
  §2.8) — neither list contains it.
- **Why it hasn't broken**: Kilo's live `GET /agent` happens to return `code`
  first (verified: `docs/kilo-spike-7.4.20/agents-summary.json` lists `code`
  before `ask`/`debug`/`explore`/...), so the "first non-plan non-dangerous"
  fallback silently produces the right answer today.
- **Failure scenario**: This is an unvalidated engine-ordering coincidence,
  not an explicit contract. If Kilo ever reorders its `/agent` response, or
  a future config/live-catalog filter changes iteration order, `/plan off`
  on the daemon and the mode chip on the phone would each independently
  (and possibly *differently*, since they're two separate implementations
  with no shared source of truth) land on whatever agent sorts first instead
  of `code` — with zero test signal, since
  `internal/session/defaultmode_test.go`'s
  `TestDefaultModeNeverResolvesToADangerousMode` table has cases for
  `opencode`, `grok`, `codex`, `goose`, and a custom agent, but **no `kilo`
  case**.
- **Compounding gap**: the mobile Settings screen's default-mode picker also
  offers a static floor of `default`/`plan`/`auto` — for Kilo, picking
  "Default" there never resolves to anything (same root cause), a minor but
  connected UX gap.
- **Confidence**: confirmed (both call sites read directly; live agent order
  confirmed from spike artifact).
- **Fix shape**: add `'code'` to both literal lists (matches the pattern
  already used for `opencode`'s `'build'`), and add a `kilo` case to
  `TestDefaultModeNeverResolvesToADangerousMode`. Longer-term, the daemon
  already knows each provider's "normal" agent id
  (`internal/provider/kilo/mode.go`'s `normalAgentID()` correctly prefers
  `code`) — consider having `session.create`/mode-list responses carry an
  explicit "this is the default" flag instead of two independent
  string-literal lists that must be kept in sync by hand.

### M2 — Stale/misleading security-guard comment: cites a test that doesn't exist for Kilo

- **Where**: `internal/provider/kilo/catalog_live.go:118-124`

  ```go
  // SECURITY: the engine also returns a plaintext `key` field per provider — the
  // user's API key. This struct deliberately has no such field, so encoding/json
  // discards it during decode and it can never reach a catalog option, a log line
  // or the wire. Do not "complete" this struct against the engine's response
  // shape (MADR 0043 D4); TestConnectedCatalogDropsAPIKey guards it.
  ```

  `TestConnectedCatalogDropsAPIKey` exists in
  `internal/provider/opencode/catalog_test.go:180` — it tests **opencode's**
  `connectedProvidersResponse` struct, not Kilo's. Kilo has no test file
  covering its own copy of this struct at all.
- **Why it's not an active vulnerability today**: the structural defense (no
  `key` field on the struct, so `encoding/json` silently drops it on decode)
  is real and correctly copied — verified by reading the struct. The API key
  cannot leak through this path *as the code stands*.
- **Why it's still worth fixing**: the comment's own stated purpose is to
  warn a future editor away from "completing" the struct to match the
  engine's real response shape — and that warning is backed by a promise of
  a guard test that, for Kilo, does not exist. If someone adds the `key`
  field back to Kilo's `connectedProvidersResponse` (e.g. while chasing a
  "why is the API key missing from the debug log" ticket), nothing catches
  it. This is exactly the kind of security-relevant regression a comment
  like this exists to prevent, undermined by copy-paste without copying the
  test.
- **Confidence**: confirmed (comment read, opencode test located, kilo test
  absence confirmed via grep across `internal/provider/kilo/*_test.go`).
- **Fix shape**: port `TestConnectedCatalogDropsAPIKey` into
  `internal/provider/kilo/catalog_test.go` (currently doesn't exist — see M4)
  against Kilo's own `connectedProvidersResponse` and real field names.

### M3 — `daemon.go`'s "no provider ready" warning doesn't know Kilo exists

- **Where**: `internal/daemon/daemon.go:285`

  ```go
  if anyEnabled := cfg.Providers.Fake.Enabled || cfg.Providers.Grok.Enabled ||
      cfg.Providers.Goose.Enabled || cfg.Providers.Opencode.Enabled ||
      cfg.Providers.Codex.Enabled; anyEnabled {
      ready := 0
      for _, p := range reg.All() {
          if p.Ready() { ready++ }
      }
      if ready == 0 {
          log.Warn("no agent provider is ready (binaries missing from PATH); " +
              "session.create will fail until grok/opencode/fake is installable")
      }
  }
  ```

  `cfg.Providers.Kilo.Enabled` is missing from the OR-chain (Codex was added
  when it shipped; Kilo wasn't).
- **Failure scenario**: An operator enables only `providers.kilo.enabled:
  true` (every other provider disabled) but the `kilo` binary isn't on PATH.
  `reg.Register(kp)` still runs and `kp.Ready()` is false, but because
  `anyEnabled` evaluates false, this whole diagnostic block is skipped
  entirely — the daemon starts with **zero ready providers and logs no
  warning**, unlike the identical situation with any other single provider.
  This is exactly the "rebuilt the daemon and nothing shows up" class of
  confusion from the conversation that led to this debug pass, just for a
  PATH problem instead of a config-flag problem.
- **Confidence**: confirmed (exact line read).
- **Fix shape**: add `|| cfg.Providers.Kilo.Enabled` to the OR-chain. One
  token.

### M4 — Coverage gap: 14.6% (kilo) vs 85.4% (opencode), concentrated in ported-but-untested subsystems

`go test -cover ./internal/provider/kilo/... ./internal/provider/opencode/...`:

```text
ok  internal/provider/kilo       coverage: 14.6% of statements
ok  internal/provider/opencode   coverage: 85.4% of statements
```

`go test -race` is clean on both — no concurrency bugs found, this is purely
a coverage/regression-net gap. `go tool cover -func` shows 0% concentrated in
files whose *logic* is confirmed present and (per the H/M findings above and
the session-loop audit) currently correct, but has no dedicated kilo test —
only opencode's copy of the same logic is exercised by opencode's suite.
Ranked by risk if a future kilo-specific edit regresses them silently:

1. **`resync.go` (0%)** — reconnect/stall-watchdog recovery
   (`Resync`, `resyncParentMessageTurn`, `resyncTreeState`,
   `discoverTreeChildren`). Runs on **every** SSE-gap reconnect regardless of
   `session_tree` (only child-alias *binding* is gated by that flag, not
   discovery/resync itself — see the caveat in the coverage-gap agent's
   bucket-B note). A regression in the stale-evidence guard
   (`resync.go:233`) could replay old turn text as new, or leave the phone
   stuck on "running" forever, undetected — opencode locks the same logic
   down with 7 dedicated tests (`TestResyncRecoversMissedTurnEnd`, etc.)
   that only run against opencode's copy.
2. **`lifecycle.go`'s quota/hard-limit branch (0% on that branch)** — the
   MADR-0073 "don't hang on quota" split (`hard := cls.Kind ==
   agenterr.KindQuota || ...` at `lifecycle.go:113-149` vs. the soft-retry
   path at `:150-176`). A regression here reproduces the exact symptom MADR
   0073 exists to prevent — a turn that never ends and a phone stuck on
   "running" — specifically for Kilo Gateway rate-limit/quota errors, with
   nothing catching it.
3. **`catalog_live.go`'s capping logic (0%, tested at *no* scale)** —
   `capDefaultCatalogModels`, `maxDefaultCatalogModels = 200`
   (`catalog_live.go:197,244`) is copied verbatim from opencode, whose
   `catalog_size_test.go` guards it against a synthetic 172-provider/
   5,788-model shape and a 32/64 KiB relay-frame budget. Kilo's *real*
   measured catalog is bigger on both axes — 179–181 providers / ~6,006
   models (`docs/kilo-spike-7.4.20/provider-summary.json`,
   `docs/0075-PLAN-kilo-cli-provider.md:24-25`) — and has zero test at any
   scale. A regression could blow the relay frame budget or truncate the
   model picker incorrectly, and CI would stay green.
4. **`permission.go`'s auto-approve path + `mode.go`'s auto-arm (0%)** — the
   MADR-0044 "dangerous" auto-approve feature. Opencode guards the
   dedup-claim invariant and retry/backoff with ~19 tests across
   `autoapprove_test.go`/`automode_test.go`; Kilo's near-verbatim port of
   the same logic has none. A regression here is a *safety* regression (a
   permission double-answered or silently stuck), not just a UX one.
5. **`question.go` (0%)**, **`todo.go` (0%)**, **`session.go`'s tool-dedup
   helpers (0%)**, **`session_ops.go` — all 9 exported ops (0%, no
   `session_ops_test.go` at all)**, **`Create`/`Prompt`'s asymmetric wire
   keys** (`{providerID, id}` vs `{providerID, modelID}` — opencode guards
   this exact trap with `TestCreateAndPromptModelKeysOnWire` specifically so
   a "let's unify these" refactor doesn't ship a silent 400; Kilo has the
   same asymmetry and no guard).

**Not a gap**: session-tree/child-suppression tests
(`child_suppression_test.go`, `subagent_test.go`, `idle_confirm_test.go`,
`resync_tree_test.go`) are legitimately deferred — `session_tree` defaults
`false` (PD2) and the demux gate (`httpagent/session.go:865-873,881-890`) is
real, verified by reading the gating code directly, not just by the config
default existing. Caveat carried forward from the research pass: when
`session_tree` does flip to `true` per the MADR's stated flip criteria, this
entire code path activates with **zero** test net — none of opencode's ~16
tests covering it have a kilo counterpart yet. That's fine to defer as long
as the flip doesn't happen without first porting (a subset of) that suite.

**Confidence**: confirmed (coverage numbers reproduced directly; per-function
breakdown and file-level claims verified by reading the cited source).

### L1 — Stale "OpenCode"/"`build`" wording in doc comments on correctly-adapted Kilo code

- **Where**: `internal/provider/kilo/mode.go:114-117` (doc comment on
  `autoMode()` still says *"session.defaultMode resolves 'return to normal'
  as `build`"* even though the function below it, `normalAgentID()`, was
  correctly changed to prefer `code` — this is the same root fact as M1, and
  is the exact spot where updating the comment should have prompted noticing
  the two hardcoded lists never got the memo). Also: `question.go`,
  `todo.go`, `usage.go`, `session_ops.go` retain "OpenCode" in doc comments
  and identifier-adjacent prose (e.g. *"engineQuestion is the OpenCode
  question.asked…"*, *"openCodeTodo is the OpenCode engine Todo shape"*)
  while the log-line string literals in the same functions were correctly
  changed to say "kilo".
- **Impact**: none functionally — pure documentation drift from a
  copy-then-adapt fork where string literals got updated but doc comments
  above them didn't.
- **Confidence**: confirmed.
- **Fix shape**: mechanical find-replace pass over doc comments in the
  affected files; cheap, low-priority, but do it in the same PR as M1 since
  the mode.go comment directly names the bug.

### L2 — No `NewHTTPWithToolFrameHook` / raw-tool-frame instrumentation hook for Kilo

- **Where**: `internal/provider/kilo/kilo.go`, `dialect.go` — no
  `onToolPartUpdated` field or hook constructor, unlike opencode's
  equivalent (used by `opencode/live_tool_stream_test.go` for live-probe
  instrumentation).
- **Impact**: not a runtime behavior gap — the hook is test/probe tooling,
  not used by the daemon in production. But it means there's no
  `live_tool_stream_test.go`-equivalent possible for Kilo without adding it
  back, which is part of why tool-card dynamics have no live proof beyond
  the MADR's P4 acceptance-run claim (see M4 §5 note on `session_ops.go`).
- **Confidence**: confirmed.

### L3 — No `/review` slash command for either provider (checked, not a Kilo-specific gap)

Checked because it looked like it could be a dropped-during-fork item:
neither `internal/provider/kilo/commandtable.go` nor
`internal/provider/opencode/commandtable.go` has a `review` entry, and
Kilo's `GET /command` doesn't advertise one either per the MADR. This is a
pre-existing product-surface characteristic shared by both providers, not a
Kilo regression. Recorded so it's clear it was checked, not skipped.

---

## 2. What was checked and found correct (not padding — recorded so this audit isn't re-run blind)

- **Session/event core** (`session.go`, `session_ops.go`, `lifecycle.go`,
  `permission.go`, `question.go`, `resync.go`, `todo.go`, `usage.go`,
  `command.go`) — byte-diffed against opencode; zero logic divergence beyond
  intentional, MADR-documented deltas.
- **`splitModel` (PD3)** — first-slash-only split correctly preserves
  `~vendor/model` aliases; matches `dialect_test.go:TestSplitModelFirstSlash`
  exactly, including the bare-name and empty-string edge cases.
- **`session_tree` kill switch** — `treeEnabled()` gate verified by reading
  `httpagent/provider.go:774` and `httpagent/session.go:866/881/929`
  directly; with the default `false`, child-alias binding and
  `ConfirmTreeIdle` are genuinely inert, not just configured-off.
- **Auto-approve retry/dedup mechanics** (logic, not test coverage — see M4
  for the coverage side) — `autoApproveAttempts`/backoff/dedup-claim-release
  read correctly; no double-answer or dropped-response path found.
- **Mode/agent filtering vs MADR §2.8** — `visible()`/`startable()` exactly
  implement the hidden/subagent exclusion table; live-verified by
  `kilo/live_test.go:TestLiveCatalogs` failing loudly if
  `explore/general/compaction/summary/title` leak into the catalog.
- **Config schema, viper wiring, daemon registration, `config.example.yaml`**
  — `KiloProviderConfig` matches MADR §4.2 field-for-field including every
  default value; env-var auto-binding works identically to every other
  provider (no explicit `BindEnv` needed, confirmed via `AutomaticEnv` +
  prefix + replacer); example config has zero drift from the real Go
  defaults; `Shutdown()` coverage is generic across `reg.All()` and covers
  Kilo automatically.
- **PD4/PD5 catalog policy** — no hardcoded `kilo-auto/balanced` or other
  auth-state-dependent id found anywhere in the kilo package (PD4 honored);
  the in-memory per-boot cache is real infra-level behavior
  (`httpagent.Provider.catalogs`, `picker.Cache`) that busts correctly on
  every engine respawn (`provider.go:548`, right after `generation++`) — not
  kilo-specific code, so it just works.
- **`ListModelsForLive` (MADR 0043 scoped catalog)** — implemented for
  Kilo, byte-parity with opencode's version. Initially suspected missing;
  confirmed present on inspection.
- **Mobile picker generality** — `option_picker_sheet.dart` is fully
  generic (`ListView.builder`, daemon-driven grouping/search, 500-option cap
  with an honest truncation banner) and handles Kilo's much larger catalog
  without special-casing. No client-side `provider/model` string parsing
  exists anywhere in `apps/mobile/lib` — Kilo's `~vendor/model` aliases pass
  through as opaque, daemon-labeled strings. No hardcoded provider-id icon
  map, color lookup, or allowlist exists for the provider picker itself.
  `_spawnOnly => provider == 'grok'` (chat_screen.dart:2858) is a
  **legitimate** provider-specific check (grok genuinely locks thinking
  level at spawn) — not the same bug class as H1.
- **No races**: `go test -race ./internal/provider/kilo/...
  ./internal/provider/opencode/...` is clean.

---

## 3. Non-findings explicitly ruled out

- **Session-tree/child-suppression code paths** are not a live risk under
  the current default (`session_tree: false`) — verified by reading the gate,
  not inferred from config. Deferring their test coverage until the Q7
  child-SSE fixtures land (per MADR 0075 PD2) is a reasonable, already-decided
  tradeoff, not a gap this audit is raising fresh. Flagged in M4 only as a
  "don't flip the flag without also porting the test suite" reminder.
- **No security vulnerability is currently exploitable** via M2 — the API
  key cannot leak today; the finding is about the missing regression guard,
  not a live leak.

---

## 4. Suggested remediation order

Not a plan (no phases/decisions locked here — that's a follow-up PLAN doc if
the owner wants one), just a priority read of §1 for whoever picks this up:

1. **H1** — mobile menu gating (one file, `chat_screen.dart:2075`; smallest
   fix, biggest and most immediate user-visible win).
2. **M3** — `anyEnabled` OR-chain (one token, `daemon.go:285`).
3. **M1** — add `'code'` to both hardcoded default-mode lists
   (`commands.go:570`, `chat_helpers.dart:27`) + a `kilo` case in
   `TestDefaultModeNeverResolvesToADangerousMode`.
4. **M2** — port `TestConnectedCatalogDropsAPIKey` for Kilo's own struct.
5. **M4** — backfill tests in the order ranked in §1 M4 (resync → quota
   hard-limit → catalog-size-at-real-scale → auto-approve/auto-mode →
   question/todo/session_ops/wire-key-asymmetry). This is the biggest
   line-count item; consider whether it's worth its own MADR/PLAN given the
   volume (opencode's equivalent suites are ~20 files), or whether it lands
   incrementally as each subsystem gets touched next.
6. **L1–L3** — comment cleanup, bundle into whichever PR touches M1 (mode.go)
   since L1's clearest instance is right next to that fix.
