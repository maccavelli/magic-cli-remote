# Implement adopt grok 1.0.3's measured CLI and ACP surface

Associated MADR: [0081-MADR-grok-1.0.3-surface-parity.md](0081-MADR-grok-1.0.3-surface-parity.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **Complete** (2026-08-12). Phases A–J landed as commits
  `1a78d27` (A), `4c1ede7` (B), `1f1225b` (C), `cda2acb` (D),
  `5aabc77` (E), `e2cb2ea` (F), `e2a0c77` (G, `/loop` promoted),
  `c589d3f` (H, `_meta` measured not taken), `ad1689a` (I, fork
  measured not taken), plus this docs commit (J).
- **Date**: 2026-08-12
- **Keyed to**: repository HEAD at plan-write time; grok **1.0.3
  (`1a29d5bc12d4`) [stable]**. Re-record `grok --version` at the top of
  the first live-test commit if the implementer's binary differs.
- **Scope**: `internal/provider/grok`, `internal/provider/acpagent`
  (only where grok's Spec or spawn/session hooks require it),
  `internal/command` (only if Phase G promotes `/loop`), every other
  provider command table (conformance, Phase G only), `docs/config.md`,
  `README.md` grok rows, MADR 0081 status. No remote-protocol change.
  No mobile change. No new `providers.grok.*` config keys unless a
  measure-gated phase below explicitly adds one.

## Goal

Make the grok provider tell the truth about grok 1.0.3: the static
model floor matches what this binary accepts, daemon-spawned grok does
not auto-update itself, the models-update handler is registered under
the method name 1.0.3 actually emits, live tests pin 1.0.3 rather than
0.2.114, `/review` stops lying when grok advertises the review skill,
and later surfaces (`/loop`, session `_meta`, fork) land only after a
discrimination probe.

Tests are a deliverable of every phase, not a follow-up. MADR 0081
Confirmation IDs (T-A1 … T-P4) are the contract. A phase commit that
changes production code without its named tests is incomplete.

## Scope

### In scope

MADR 0081 P1 items 1–5, the `/review` collision in P3 item 12, and the
measure-gated remainder of P2/P3 (session `_meta`, fork, `/loop`).
`session/close` is already called (see baseline). P4 stays out.

### Out of scope (MADR P4 — do not implement)

`--minimal`, `--fullscreen`, `--no-alt-screen`, `--oauth`,
`--include-partial-messages`, `--output-format`, `--restore-code`,
`--verbatim`, `--fork-session`, `-c`, `-r`, `-s`, `--worktree`,
`--worktree-ref`, `--cwd`, `--json-schema`, `--max-turns`,
`--prompt-file`, `--prompt-json`, `--agents`, `--agent`,
`--agent-profile`, `--plugin-dir`, `--experimental-memory`,
`--no-memory`, `grok memory`, `dashboard`, `doctor`, `du`, `export`,
`inspect`, `setup`, `trace`, `wrap`, plugin marketplace, `grok update`,
`grok import` (not in 1.0.3), model id `grok-build`, hidden aliases
(`--yolo`, `--effort`, …), `session/set_config_option`, treating
`xhigh` as an ACP mode, `grok agent serve` / `headless` / `leader`,
`_x.ai/fs/*` / `_x.ai/git/*` as daemon tools, `_x.ai/hooks` as a
policy engine, announcements. Do not add hook command names to
`command/specs.go`. Do not replace the daemon session roster with
`session/list`. Do not replace `session/load` with `session/resume`.
Do not replace MADR 0049's synthetic `auto` with `_meta.autoMode`.

### Plan-level decisions (MADR left these to the plan)

These are execution choices, not new architecture:

1. **Static floor** is exactly `grok-4.6` (first, default) and
   `grok-4.5`. Drop `grok-code-fast-1` and `grok-4`. `AllowCustom`
   stays true so an older install can still type a refused id.
2. **`--no-auto-update` is unconditional** in `defaultArgs`. No new
   config key. Operators who replace argv via `providers.grok.args`
   already bypass `DefaultArgs` (`acpagent.New`).
3. **Register both** `_x.ai/models/update` (live 1.0.3 name) and
   `_x.ai/models_update` (existing key) to the same handler.
4. **`/review` on grok becomes `KindNative` → `review`**, gated by
   advertisement (`available()`). Do not map it to `OpReview` (that
   is Codex inline review). KindNone stays on every other provider.
5. **`session/close` is already wired.** `acpagent/session.go`
   `Close` already calls `s.conn.CloseSession` (`CloseSessionRequest`)
   then kills the process. Do not add a second raw RPC. Phase D only
   pins that this still succeeds on 1.0.3.
6. **Measure-gated phases (G, H, I) implement only if the written
   probe passes.** A failed or inconclusive probe is a documented
   stop, not a best-effort implementation.

## Implementation Steps

### Ground rules (every phase)

1. One phase → one commit. Do not push unless asked.
2. Before `git add` of any Go file:
   `make pre-add-check FILES="…touched.go files…"`.
3. `git commit` with no `-m` / `--message` / `-F`.
4. The binary is the contract. If a step's expected clap/RPC result
   disagrees with the implementer's `grok --version`, stop and update
   MADR 0081 rather than guessing.
5. `go test -tags live_grok ./internal/provider/grok/ …` skips when
   grok is not on `PATH`. Live tests are not a CI gate; they are the
   developer/ops pin. Run them on this host before marking a
   measure-gated phase done.
6. Keep exactly one `in_progress` todo; mark `completed` only after
   the phase's **named tests** are written and green (MADR 0081 T-*
   IDs). Production-only commits are not done.
7. Do not edit P4 flags, mobile, or protocol docs.
8. Live tests: `//go:build live_grok`, `package grok_test`, skip via
   `if !p.Ready() { t.Skip("grok not in PATH") }` (or
   `exec.LookPath("grok")` for raw-stdio probes). Do not spend
   tokens in unit tests. Discrimination tests (T-G1, T-H1, T-I1)
   run once at acceptance, not in a loop.

### Verified baseline (do not rediscover)

Measured 2026-08-12 against grok 1.0.3; recorded in MADR 0081.

| Claim | Where |
| --- | --- |
| Global policy flags still rejected after `agent` | MADR 0081 context; same 0050 placement |
| `--no-auto-update` accepted before `agent`, rejected after | MADR 0081 P1.3 |
| Live models: `grok-4.6` (default), `grok-4.5`; `grok-code-fast-1` and `grok-build` rejected by `session/set_model` | MADR 0081 P1.1 |
| Wire notification is `_x.ai/models/update` (slash, no `id`) | MADR 0081 P1.2 |
| Registered name is `_x.ai/models_update` | `internal/provider/grok/grok.go` `ExtensionNotifications` |
| `grok-4.6` efforts `xhigh, high, medium, low`; both `xhigh` and `high` marked `default: true`; `reasoningEffort` field is `high` | MADR 0081 P1.4 |
| `NormalizeThinkingLevels` keeps `high` as the single default | `internal/picker/thinking.go` |
| `SetThinkingLevel` is `ErrThinkingLevelFixed` | `internal/provider/acpagent/session.go` |
| `session/new` accepts `_meta.yoloMode`, `_meta.autoMode`, `_meta.rules` | MADR 0081 P2.6 |
| `NewSessionRequest` already has `Meta map[string]any` | `acp-go-sdk@v0.13.5` `types_gen.go` `NewSessionRequest` |
| Production `session/new` sends only `Cwd` + `McpServers` | `acpagent.go` `conn.NewSession` |
| `Close` already calls ACP `CloseSession` then kills the process | `acpagent/session.go` `Close` |
| `/review` is advertised as a skill; grok table is `KindNone` | MADR 0081 P3.12; `commandtable.go` |
| `/help` already lists agent-advertised names under "From the agent:" | `internal/session/commands.go` `helpText` |
| Adding a name to `command/specs.go` requires every provider table to declare it | `internal/command/conformance_test.go` |
| `--worktree` optional-value can steal the `agent` token | MADR 0081 P4 |

## Tests

This section is the implementation of MADR 0081 Confirmation. Copy
function names exactly so reviews can grep them.

### Rules

* **Unit first.** Every production change has a unit test that fails
  if the change is reverted. Live tests pin the binary; they do not
  replace the unit test.
* **One ID → one function** unless the MADR row says "extend".
  Extensions add subtests (`t.Run`) rather than silent new
  assertions in an unrelated test.
* **Fixtures from the wire.** JSON in `thinking_test.go` and live
  probes must be copied from the 2026-08-12 initialize / `session/new`
  frames (MADR 0081), not invented.
* **Skip, don't fail, without grok.** `t.Skip("grok not in PATH")`.
* **G/H/I order is test-then-code.** Land T-G1 / T-H1 / T-I1 in the
  same commit as any production change they gate, or in the commit
  immediately before. Never the other way around.
* **Do not weaken T-F4.** `/compact` staying silent is a 0081
  invariant. If it starts answering, stop and update the MADR.

### Inventory (write these)

| ID | Function | File | When |
| --- | --- | --- | --- |
| T-A1 | `TestStaticModelsFloor` | `internal/provider/grok/grok_test.go` | Phase A |
| T-A2 | `TestLiveGrokColdCatalog` (existing; keep) | `live_model_test.go` | Phase A (run, do not rewrite) |
| T-B1 | extend `TestDefaultArgs`, `TestDefaultArgsWithModelAndApprove`, `TestSpecModelArgs`, `TestDefaultArgsWithReasoningEffort`, `TestSpecModelArgsPolicyFlags`, `TestDefaultArgsSandboxProfile`, `TestDefaultArgsPutsGlobalsBeforeSubcommand` | `grok_test.go` | Phase B |
| T-P4 | `TestDefaultArgsDoesNotEmitP4Flags` | `grok_test.go` | Phase B (same commit: argv is the place P4 would leak) |
| T-B2 / T-D1 | extend `TestLiveGrokArgvAcceptsEveryConfiguredFlag` | `live_argv_test.go` | Phase B rows for implicit `--no-auto-update`; Phase D adds `model46`, `reasoningXHigh` |
| T-C1 | `TestSpecRegistersBothModelsUpdateNames` | `internal/provider/grok/extnotif_test.go` **new** | Phase C |
| T-C2 | `TestLiveGrokModelsUpdateMethodName` | `live_modelsupdate_test.go` **new** | Phase D |
| T-D2 | extend `TestLiveGrokSetModelWireContract` | `live_setmodel_test.go` | Phase D |
| T-D3 | extend `TestLiveGrokInitializeMetaWireContract` | `live_initializemeta_test.go` | Phase D |
| T-D4 | `TestLiveGrokCloseSucceeds` | `live_setmodel_test.go` or `live_test.go` | Phase D |
| T-E1 | `TestModelsToCatalogGrok46XHigh` | `acpagent/thinking_test.go` | Phase E |
| T-E2 | `TestSetThinkingLevelFixed` (existing; keep) | `thinking_test.go` | Phase E (run, do not change semantics) |
| T-E3 | `TestNormalizeGrok46DualDefaultKeepsHigh` | `internal/picker/thinking_test.go` | Phase E |
| T-F1 | `TestGrokReviewIsNativeWhenAdvertised` | `internal/provider/grok/commandtable_test.go` **new** | Phase F |
| T-F2 | `TestResolveGrokReviewForwardsWhenAdvertised` | `internal/command/command_test.go` | Phase F |
| T-F3 | extend `TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow` | `live_commandcatalog_test.go` | Phase F |
| T-F4 | `TestLiveGrokCommandTableMatchesReality` (existing `/compact`) | `live_command_test.go` | Phase F (run; must stay KindNone + silent) |
| T-G1 | `TestLiveGrokLoopSchedules` | `live_loop_test.go` **new** | Phase G **first** |
| T-G2 | `TestResolveLoop` | `command_test.go` | Phase G only if T-G1 passes |
| T-H1 | `TestLiveGrokSessionMetaDiscrimination` | `live_sessionmeta_test.go` **new** | Phase H **only test commit** |
| T-I1 | `TestLiveGrokSessionForkShapes` | `live_fork_test.go` **new** | Phase I **first** |
| T-I2 | `TestGrokForkIsOpFork` | `commandtable_test.go` | Phase I only if T-I1 wins a shape |

### Shared live helpers

Reuse `waitForCommands` / `promptText` / `slicesContainsFold` from
`live_command_test.go` (same `package grok_test`). Do not duplicate
them. New raw-stdio probes (T-C2, T-H1, T-I1) may exec `grok`
directly; put a small `startACP(t)` helper in
`live_helpers_test.go` (**new**, `//go:build live_grok`) that:

1. Resolves `grok` via `exec.LookPath`; skips if missing.
2. Starts `grok --no-auto-update --permission-mode default agent --no-leader stdio` with stdin/stdout pipes.
3. Sends `initialize` (protocolVersion 1, fs+terminal caps, `clientInfo.name=mcremote-0081`).
4. Sends `notifications/initialized`.
5. Sends `session/new` with `cwd=t.TempDir()`, `mcpServers=[]`, plus
   optional `_meta`.
6. Returns `*exec.Cmd`, stdin, a `readJSON(id)` helper, and registers
   `t.Cleanup` to close stdin and `Kill` the process.

T-C2 / T-H1 / T-I1 all use this helper. Write the helper in the first
phase that needs it (D for T-C2).

### Commands

```bash
# unit net for 0081 (CI)
go test ./internal/provider/grok/ ./internal/provider/acpagent/ \
  ./internal/command/ ./internal/picker/ -count=1

# live net for 0081 (developer host with grok 1.0.3)
go test -tags live_grok ./internal/provider/grok/ -count=1 \
  -run 'Argv|SetModel|InitializeMeta|ColdCatalog|ModelsUpdate|Close|CommandCatalog|CommandTable|Loop|SessionMeta|Fork'
```

---

### Phase order

```text
A  static model floor = grok-4.6, grok-4.5
B  emit --no-auto-update before agent
C  register _x.ai/models/update (+ keep underscore alias)
D  re-pin live suite to 1.0.3 (argv, set_model, close, comments)
E  xhigh fixture + dual-default pin; thinking stays spawn-only
F  grok /review KindNone → KindNative
G  measure /loop; promote only if it schedules
H  measure session/new _meta autoMode/yoloMode; wire only if new behaviour
I  pin _x.ai/session/fork schema; implement ForkSession only if complete
J  docs + MADR 0081 status
```

A–F are unconditional once the MADR is accepted. G–I each start with a
written probe and have an explicit stop. J is last and records what
G–I actually did.

A may land in parallel with nothing; B depends on A only for a
conflict-free `grok.go` edit (same file). C is independent of A/B
except the same file. D depends on A+B so the new argv rows see
`--no-auto-update` and `grok-4.6`. E is independent of B/C and can
follow A. F is independent. G–I follow D (they need a live 1.0.3
session). J follows everything that landed.

---

## Phase A — Static floor is grok-4.6 and grok-4.5

**MADR:** P1.1
**Files:** `internal/provider/grok/grok.go`, `internal/provider/acpagent/acpagent.go` (comment only), `internal/provider/grok/grok_test.go` (add if no existing static-list test)

### Steps

1. In `internal/provider/grok/grok.go`, replace `staticModels` with:

   ```go
   var staticModels = []picker.Option{
       {ID: "grok-4.6", Label: "Grok 4.6", Group: "xai"},
       {ID: "grok-4.5", Label: "Grok 4.5", Group: "xai"},
   }
   ```

   Rewrite the comment above it. State: floor for no live
   `initialize`; live catalog replaces it outright (MADR 0039 D2 /
   0043 D7); measured 2026-08-12 on grok 1.0.3 as `grok-4.6`
   (default) and `grok-4.5`; `grok-code-fast-1` / `grok-4` removed
   because `session/set_model` rejects them; `AllowCustom` remains
   the type-in escape hatch.

2. In `acpagent.go` `ListModels`, the comment that says "the agent
   offers exactly one model (grok-4.5) while the static list names
   four" is stale. Change it to: live list is authoritative and
   must not be merged with the static floor (MADR 0043 D7 / 0081).

3. Write **T-A1** `TestStaticModelsFloor` in `grok_test.go` (required;
   this phase is not done without it):

   ```go
   func TestStaticModelsFloor(t *testing.T) {
       if len(staticModels) != 2 {
           t.Fatalf("len=%d, want 2", len(staticModels))
       }
       if staticModels[0].ID != "grok-4.6" || staticModels[1].ID != "grok-4.5" {
           t.Fatalf("ids=%v,%v want grok-4.6, grok-4.5", staticModels[0].ID, staticModels[1].ID)
       }
       for _, o := range staticModels {
           switch o.ID {
           case "grok-code-fast-1", "grok-4", "grok-build":
               t.Errorf("stale or docs-only id %q on the floor", o.ID)
           }
           if o.Group != "xai" {
               t.Errorf("%s group=%q, want xai", o.ID, o.Group)
           }
       }
   }
   ```

   Do not parse `grok --help` at test time.

4. Run existing **T-A2** `TestLiveGrokColdCatalog`. Do not change its
   merge/padding assertions.

### Tests (required deliverable)

* T-A1 written and green: `go test ./internal/provider/grok/ -run StaticModelsFloor -count=1`
* T-A2 green on a grok host: `go test -tags live_grok ./internal/provider/grok/ -run ColdCatalog -count=1`

### Verification

```bash
go test ./internal/provider/grok/ -count=1
make pre-add-check FILES="internal/provider/grok/grok.go internal/provider/grok/grok_test.go internal/provider/acpagent/acpagent.go"
```

### Acceptance

`models.list` with no prior session and no harvest returns those two
ids. After harvest (`TestLiveGrokColdCatalog`, existing), the live
list still replaces the floor; that test already forbids
`SourceStatic` and a padded merge (`len(cat.Options) > 3`).

---

## Phase B — Always pass `--no-auto-update` before `agent`

**MADR:** P1.3
**Files:** `internal/provider/grok/grok.go`, `internal/provider/grok/grok_test.go`

### Steps

1. In `defaultArgs`, append `--no-auto-update` as the last global,
   immediately before `agent`:

   ```go
   return append(args, "--no-auto-update", "agent", "--no-leader", "stdio")
   ```

   Update the function comment: verified 1.0.3; global; rejected after
   `agent`; official headless/ACP guidance; MADR 0081 P1.3.

2. `ModelArgs` already calls `defaultArgs(...)`, so the flag rides
   for free. Do not add a Config field.

3. Update every exact-slice assertion in `grok_test.go`:

   | test | expected tail |
   | --- | --- |
   | `TestDefaultArgs` | `"--no-auto-update", "agent", "--no-leader", "stdio"` |
   | `TestDefaultArgsWithModelAndApprove` | same tail after the existing globals |
   | `TestSpecModelArgs` | same |
   | `TestDefaultArgsWithReasoningEffort` | same |
   | `TestSpecModelArgsPolicyFlags` | same |
   | `TestDefaultArgsSandboxProfile` | `"--sandbox", "workspace", "--no-auto-update", "agent", "--no-leader", "stdio"` |

4. In `TestDefaultArgsPutsGlobalsBeforeSubcommand`, add
   `"--no-auto-update"` to the required-before-`agent` flag list.
   The tail assertion stays `[]string{"agent", "--no-leader", "stdio"}`
   (the flag is before `at`, not in the tail).

5. Do **not** change `acpagent/thinking_test.go`
   `TestSpawnArgsThinkingPrecedence`. That test uses a local
   `defaultArgs` closure, not `grok.defaultArgs`.

6. Write **T-P4** `TestDefaultArgsDoesNotEmitP4Flags` in `grok_test.go`
   in this commit (argv is where a P4 flag would leak):

   ```go
   func TestDefaultArgsDoesNotEmitP4Flags(t *testing.T) {
       got := defaultArgs(Config{Model: "grok-4.6", ReasoningEffort: "xhigh", AlwaysApprove: true})
       forbidden := []string{
           "--worktree", "--worktree-ref", "--minimal", "--fullscreen",
           "--cwd", "--oauth", "--json-schema", "--max-turns",
           "--experimental-memory", "--no-memory", "--restore-code",
           "--verbatim", "--include-partial-messages",
       }
       for _, f := range forbidden {
           if slices.Contains(got, f) {
               t.Errorf("P4 flag %s leaked into defaultArgs: %v", f, got)
           }
       }
   }
   ```

### Tests (required deliverable)

* T-B1: every exact-slice test listed in step 3 includes
  `--no-auto-update` before `"agent"`.
* T-P4: `go test ./internal/provider/grok/ -run 'DefaultArgs|SpecModelArgs|P4Flags' -count=1`
* T-B2 (live, may wait for Phase D if the implementer batches live
  runs): `Start` with default config must not fail
  `unexpected argument '--no-auto-update'`.

### Verification

```bash
go test ./internal/provider/grok/ -count=1
# placement pin against the real binary (must ACCEPT):
grok --no-auto-update agent --no-leader stdio --help
# must REJECT:
grok agent --no-auto-update --no-leader stdio --help
make pre-add-check FILES="internal/provider/grok/grok.go internal/provider/grok/grok_test.go"
```

The reject line must contain `unexpected argument '--no-auto-update'`.
If 1.0.3+ moves the flag onto `grok agent`, stop and update MADR 0081
instead of placing it after `agent`.

### Acceptance

Every daemon-spawned grok argv contains `--no-auto-update` before
`agent`. Prewarm still matches: `New` bakes `cfg.Args` from
`DefaultArgs(cfg)`, and `spawnArgs` with no per-session override
reuses that slice.

---

## Phase C — Register the live models-update method name

**MADR:** P1.2
**Files:** `internal/provider/grok/grok.go`, `internal/provider/acpagent/acpagent.go` (comment on `HandleModelsUpdate`), new `internal/provider/grok/extnotif_test.go` (unit), later live pin in Phase D

### Steps

1. In `spec.ExtensionNotifications` register **both** keys to
   `acpagent.HandleModelsUpdate`:

   ```go
   "_x.ai/models/update":  acpagent.HandleModelsUpdate,
   "_x.ai/models_update":  acpagent.HandleModelsUpdate,
   ```

   Comment: 1.0.3 emits the slash form as a notification (no `id`);
   the underscore key is the 0039 name, kept so a revert of grok's
   spelling does not drop the catalog again.

2. Change `HandleModelsUpdate`'s doc comment from
   `_x.ai/models_update` to name both spellings.

3. Unit test in package `grok`: the spec map contains both keys and
   both values are non-nil. Do not invoke the handler against a nil
   session.

4. Delivery still depends on the SDK routing extension
   *notifications* into `HandleExtensionMethod`. This phase does
   **not** invent a second dispatch path. If Phase D's live pin shows
   the notification arrives and the catalog still does not refresh,
   that is a new finding: open a follow-up on the SDK hook, do not
   paper over it in this phase.

5. Write **T-C1** in new file `internal/provider/grok/extnotif_test.go`
   (package `grok`, no build tag):

   ```go
   func TestSpecRegistersBothModelsUpdateNames(t *testing.T) {
       slash := spec.ExtensionNotifications["_x.ai/models/update"]
       underscore := spec.ExtensionNotifications["_x.ai/models_update"]
       if slash == nil || underscore == nil {
           t.Fatalf("models-update handlers slash=%v underscore=%v", slash != nil, underscore != nil)
       }
   }
   ```

   Do not invoke the handlers. Phase D writes T-C2 (live method
   string on the wire).

### Tests (required deliverable)

* T-C1 green: `go test ./internal/provider/grok/ -run RegistersBothModelsUpdate -count=1`

### Verification

```bash
go test ./internal/provider/grok/ ./internal/provider/acpagent/ -count=1
make pre-add-check FILES="internal/provider/grok/grok.go internal/provider/acpagent/acpagent.go internal/provider/grok/extnotif_test.go"
```

### Acceptance

`spec.ExtensionNotifications["_x.ai/models/update"]` is set. The
initialize path is unchanged and remains the first-catalog source.

---

## Phase D — Re-pin the live suite to grok 1.0.3

**MADR:** P1.5, Confirmation, P2.7 (close already wired)
**Files:** `internal/provider/grok/live_argv_test.go`,
`internal/provider/grok/live_setmodel_test.go`,
`internal/provider/grok/live_initializemeta_test.go`,
`internal/provider/grok/live_commandcatalog_test.go`,
new `internal/provider/grok/live_modelsupdate_test.go`,
comments in `commandtable.go`, `grok.go`, `live_command_test.go`

### Steps

1. Record the implementer's `grok --version` in the live-test file
   headers. Expected at plan-write: `grok 1.0.3 (1a29d5bc12d4) [stable]`.

2. `live_argv_test.go`: keep existing rows. Add:

   ```go
   {"model46", grok.Config{Model: "grok-4.6"}},
   {"reasoningXHigh", grok.Config{ReasoningEffort: "xhigh"}},
   ```

   In the `"everything"` row, set `Model: "grok-4.6"` (still a valid
   id) and `ReasoningEffort: "xhigh"`. Failure mode of interest
   remains `unexpected argument` (MADR 0050). Also fail if start
   errors with `unknown model id` or a sandbox/profile refuse for
   these new rows.

3. `live_setmodel_test.go`: `SetModel(ctx, "grok-4.6")` must succeed;
   keep `SetModel(ctx, "grok-4.5")` as a second valid id; keep the
   unknown-id failure. Add `SetModel(ctx, "grok-code-fast-1")` and
   `SetModel(ctx, "grok-build")` as expected failures (MADR 0081:
   both `unknown model id`).

4. `live_initializemeta_test.go`: after `ListModels`, require that
   some option has id `grok-4.6`. If that option has
   `ThinkingLevels`, require it contains `xhigh` and that
   `picker.DefaultThinkingLevel` is not `xhigh` (dual-default
   collapse to `high`). Skip the `xhigh` assertion only if
   `ThinkingLevels` is empty (an install whose grok-4.6 reports
   `supportsReasoningEffort=false` — log and continue; do not invent
   levels).

5. New `TestLiveGrokModelsUpdateMethodName` (`//go:build live_grok`):
   spawn the same argv as the daemon (`permission_mode` default +
   `--no-auto-update` via `grok.New(Config{PermissionMode:"default"})`),
   `Start`, then read events / or drive a tiny raw stdio sibling if
   the session API does not expose raw frames. The pin that must
   fail when grok renames the method: the process's initialize or
   first notifications include the string `_x.ai/models/update`.
   Implementation options, pick the cheaper one that sees the wire:

   * Preferred: a `live_grok` test that execs `grok --no-auto-update
     --permission-mode default agent --no-leader stdio`, writes
     `initialize` + `notifications/initialized` + `session/new`, and
     `strings.Contains` on stdout for `"method":"_x.ai/models/update"`.
     Do not assert the handler ran (SDK routing is a separate claim).
   * Bound the test to 30s. Kill the process in `t.Cleanup`.

6. `Close` pin: in `live_setmodel_test.go` (or a three-line addition
   to `live_test.go`), after `Start`, call `s.Close` and require
   `err == nil`. This is the 1.0.3 confirmation that existing
   `CloseSession` still works. Do not add another close RPC.

7. Refresh comments that still say grok **0.2.112** or **0.2.114** as
   the current pin in:

   * `internal/provider/grok/commandtable.go` package comment
   * `internal/provider/grok/grok.go` `defaultArgs` comment
     ("Verified against grok 0.2.114")
   * `live_command_test.go`, `live_setmodel_test.go`,
     `live_commandcatalog_test.go` headers

   Historical MADRs stay as they are.

8. Write `live_helpers_test.go` (`//go:build live_grok`) with
   `startACP` as specified in Tests / Shared live helpers. T-C2 uses
   it. Also write **T-C2** `TestLiveGrokModelsUpdateMethodName`: after
   `initialize` + `notifications/initialized` + `session/new`, the
   collected stdout must contain `"method":"_x.ai/models/update"`.
   Do not assert the in-process handler ran.

9. Write **T-D4** `TestLiveGrokCloseSucceeds`: `Start`, then
   `if err := s.Close(context.Background()); err != nil { t.Fatal(err) }`.

### Tests (required deliverable)

* T-C2, T-D1 (`model46`, `reasoningXHigh` rows), T-D2, T-D3, T-D4
* `startACP` helper exists and is used by T-C2
* Full live package green on a grok 1.0.3 host

### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -count=1
make pre-add-check FILES="$(git diff --name-only -- '*.go' | tr '\n' ' ')"
```

If grok is missing: unit tests in A–C still pass; live tests skip.
Do not mark D done without a live run on a host that has grok.

### Acceptance

A flag relocation, a rejected `grok-4.6`, a rejected `xhigh`, or a
renamed models-update method fails a `live_grok` test instead of
shipping silently.

---

## Phase E — xhigh is a live rung; `/thinking` stays spawn-only

**MADR:** P1.4
**Files:** `internal/provider/acpagent/thinking_test.go`

### Steps

1. Add `grok46ModelsJSON` captured from the 2026-08-12 initialize
   (MADR 0081): `grok-4.6` with efforts `xhigh, high, medium, low`,
   `xhigh.default=true`, `high.default=true`; `grok-4.5` with
   `high, medium, low`. Do not invent fields that were not on the
   wire.

2. `TestModelsToCatalogGrok46XHigh`:

   * ids after normalize: `low, medium, high, xhigh`
   * `picker.DefaultThinkingLevel` for grok-4.6 is `high` (not `xhigh`)
   * grok-4.5 levels do not include `xhigh`
   * labels are preserved

3. Do **not** change `SetThinkingLevel`. Add a one-line comment on
   that function: 1.0.3 `session/set_model` with `reasoningEffort`
   still returns `Ok` without proving the effort changed (MADR 0052
   trap / 0081 P1.4). Mid-session `/thinking` stays "new sessions
   only".

4. Do **not** send `session/set_mode` with `xhigh`. Do not parse
   `_meta.x.ai/sessionConfig` into session modes (`category: "mode"`
   there is an effort rung, not Build/Plan).

5. Write **T-E3** in `internal/picker/thinking_test.go`:

   ```go
   func TestNormalizeGrok46DualDefaultKeepsHigh(t *testing.T) {
       in := []ThinkingLevel{
           {ID: "xhigh", Default: true},
           {ID: "high", Default: true},
           {ID: "medium"},
           {ID: "low"},
       }
       got := NormalizeThinkingLevels(in)
       if DefaultThinkingLevel(got) != "high" {
           t.Fatalf("default=%q, want high (cheapest-first first-default)", DefaultThinkingLevel(got))
       }
       var defaults int
       for _, l := range got {
           if l.Default {
               defaults++
           }
       }
       if defaults != 1 {
           t.Fatalf("defaults=%d, want 1", defaults)
       }
   }
   ```

### Tests (required deliverable)

* T-E1 `TestModelsToCatalogGrok46XHigh` green
* T-E2 `TestSetThinkingLevelFixed` still green (do not change its
  expected error)
* T-E3 `TestNormalizeGrok46DualDefaultKeepsHigh` green
* `go test ./internal/provider/acpagent/ ./internal/picker/ -run 'Thinking|NormalizeGrok46|SetThinkingLevelFixed' -count=1`

### Verification

```bash
go test ./internal/provider/acpagent/ ./internal/picker/ -run 'Thinking|NormalizeGrok46|SetThinkingLevelFixed' -count=1
make pre-add-check FILES="internal/provider/acpagent/thinking_test.go internal/provider/acpagent/session.go internal/picker/thinking_test.go"
```

### Acceptance

A grok-4.6 live catalog shows `xhigh` in the picker after initialize
(Phase D live test). `/thinking xhigh` on an existing session still
returns the fixed-at-start error. A new session with
`StartOptions.ThinkingLevel: "xhigh"` already rebuilds argv via
`spawnArgs` (`--reasoning-effort xhigh` before `agent`); Phase D's
`reasoningXHigh` argv row is the pin.

---

## Phase F — Grok `/review` forwards the advertised skill

**MADR:** P3.12
**Files:** `internal/provider/grok/commandtable.go`,
`internal/provider/grok/live_commandcatalog_test.go`,
`internal/command/command_test.go` only if an existing grok-table
fixture names `review` as KindNone

### Steps

1. In `commandtable.go` replace

   ```go
   "review": {Kind: command.KindNone, Note: command.ReasonNoReview},
   ```

   with

   ```go
   "review": {Kind: command.KindNative, Native: "review"},
   ```

   Comment: 1.0.3 advertises the bundled review skill under this
   name; KindNone hid it while `/help` also listed it under "From
   the agent". This is not Codex `OpReview`. `available()` is false
   when the agent stops advertising `review`.

2. Extend `TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow`
   (or the wait in `live_command_test.go`) to require `review` in
   the advertised list on 1.0.3. If a future grok drops the skill,
   the live test fails and the table can return to KindNone.

3. Do not add `/review` behaviour tests that spend a review turn.
   Forwarding is `session/prompt` with the slash text, same as
   `/goal`.

4. Leave `fake`, `goose`, `opencode`, `kilo`, `codex` `review`
   mappings unchanged.

5. Write **T-F1** in new `internal/provider/grok/commandtable_test.go`:

   ```go
   func TestGrokReviewIsNativeWhenAdvertised(t *testing.T) {
       m := commandTable["review"]
       if m.Kind != command.KindNative || m.Native != "review" {
           t.Fatalf("review=%+v, want KindNative native=review", m)
       }
   }
   ```

6. Write **T-F2** in `internal/command/command_test.go`:

   * `Resolve("review", grokTable, state{AgentCommands:[]string{"review"}})`
     → `Available: true`, `KindNative`
   * `Resolve("review", grokTable, state{AgentCommands:[]string{}})`
     → `Available: false`
   * `Resolve("review", gooseTable, state{AgentCommands:[]string{"review"}})`
     → still `KindNone` (other providers unchanged)

   Use the real tables via constructing the same mappings, or export
   nothing — inline the grok mapping in the test. Do not make
   `commandTable` accidentally unexported-test-only; it is already
   in package `grok`, so T-F1 lives in package `grok`. T-F2 stays
   in `command` and inlines the mappings.

7. **T-F3:** extend the required names in
   `TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow` to
   `deep-research`, `workflow`, `goal`, `review`.

8. **T-F4:** run `TestLiveGrokCommandTableMatchesReality`. `/compact`
   must remain `KindNone` and silent. Do not "fix" a failure by
   mapping compact to KindNative in this MADR.

### Tests (required deliverable)

* T-F1, T-F2 unit green
* T-F3, T-F4 live green on grok 1.0.3

### Verification

```bash
go test ./internal/command/ ./internal/provider/grok/ -count=1
go test -tags live_grok ./internal/provider/grok/ -run 'CommandCatalog|CommandTable' -count=1
make pre-add-check FILES="internal/provider/grok/commandtable.go internal/provider/grok/commandtable_test.go internal/provider/grok/live_commandcatalog_test.go internal/command/command_test.go"
```

### Acceptance

On a grok session whose `available_commands` includes `review`,
`Resolve("review", grokTable, state)` is `Available: true`,
`KindNative`. `/help` lists `/review` under "You can run:" and does
not also list it under "Not here:". Typing `/review` is forwarded;
it is not rejected as unavailable.

---

## Phase G — Measure `/loop`; promote only if it schedules

**MADR:** P3.10
**Files (measure):** new `internal/provider/grok/live_loop_test.go`
**Files (only if the probe passes):** `internal/command/specs.go`,
`internal/provider/grok/commandtable.go`,
`internal/provider/goose/commandtable.go`,
`internal/provider/opencode/commandtable.go`,
`internal/provider/kilo/commandtable.go`,
`internal/provider/codex/commandtable.go`,
`internal/provider/fake/fake.go`,
`internal/command/command_test.go`,
`internal/command/conformance_test.go` (only if it hard-codes names)

### Probe (must run before any specs.go edit)

`TestLiveGrokLoopSchedules` (`//go:build live_grok`):

1. `Start` a grok session with `AlwaysApprove: true` in a `t.TempDir()`.
2. Wait for `available_commands`. If `loop` is absent: **stop**. Log
   "loop not advertised" and leave specs untouched. Record in MADR
   0081 More Information.
3. `Prompt` exactly `/loop 60s reply with the single word pong`.
4. Pass if **either**:
   * the turn completes with assistant text that names a scheduled
     job / loop id, **or**
   * a subsequent `/loop` or session `_meta` / notice indicates an
     active loop
5. Fail (and **do not promote**) if the turn is silence (the
   `/compact` pattern) or an error that the command is TUI-only.
6. Best-effort cleanup: prompt `/loop` stop/delete if the reply
   named an id. Do not leave a 7-day loop on the operator's grok
   home if a cancel path is obvious from the reply.

Interval is `60s` because grok's documented minimum is 60 seconds.
This test spends tokens. Run it once at acceptance, not in a loop.

### If the probe fails

Commit only the live test (it should `t.Skip` or `t.Log` + return
on "not advertised"; `t.Fatal` only on "advertised but silent" so
we notice a regression). Do not touch `specs.go`. Phase J will
record "measured: `/loop` not promoted".

### If the probe passes

1. Add to `command/specs.go` immediately after `workflow`:

   ```go
   {
       Name:        "loop",
       Args:        "[interval] <prompt>",
       Description: "Run a prompt on a recurring interval",
       Default:     Mapping{Kind: KindNative, Native: "loop"},
   },
   ```

2. Grok table: `"loop": {Kind: command.KindNative, Native: "loop"}`.
3. Every other table: `"loop": {Kind: command.KindNone, Note: "loop is a Grok-specific capability"}`.
4. Conformance test will fail any omitted table — fix all six
   providers in the same commit.
5. Extend `live_commandcatalog_test.go` to require `loop`.

6. If promoting, write **T-G2** `TestResolveLoop` next to
   `TestResolveDeepResearchAndWorkflow`: advertised → available;
   not advertised → unavailable; goose table with KindNone stays
   unavailable even if advertised.

### Tests (required deliverable)

* T-G1 **must exist** in this phase whether or not `/loop` is
  promoted.
* T-G2 and conformance exist **only** if T-G1's pass condition
  (scheduled job or explicit loop id) is met.
* If T-G1 is "advertised but silent": keep T-G1 as `t.Fatal` so a
  later grok that starts executing `/loop` is noticed; do not add
  T-G2.

### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -run Loop -count=1
# only if promoting:
go test ./internal/command/ ./internal/provider/... -count=1
make pre-add-check FILES="internal/command/specs.go internal/provider/grok/commandtable.go …"
```

### Acceptance

Either: `/loop` is canonical, grok-only, and `/help` lists it; or:
the live test exists and specs.go is unchanged. No third state.

---

## Phase H — Measure `session/new` `_meta`; wire only new behaviour

**MADR:** P2.6. Do not replace MADR 0049.

**Files (measure):** new `internal/provider/grok/live_sessionmeta_test.go`
**Files (only if a row below says "wire"):** `internal/provider/acpagent/acpagent.go`
(`NewSession` `Meta`), possibly `internal/provider/grok/grok.go` Spec hook

### Probe

One `live_grok` test, three sessions in a `t.TempDir()`,
`permission_mode: default` (so grok actually asks — MADR 0050 §2).
Prompt the same write: create a file named `probe.txt` containing
`x` inside the temp dir (absolute path in the prompt).

| row | session/new `_meta` | AlwaysApprove | expected if grok-native meta works |
| --- | --- | --- | --- |
| baseline | none | false | permission request **and** no file |
| yolo | `{yoloMode: true}` | false | **no** permission request **and** file exists |
| auto | `{autoMode: true}` | false | record whatever happens; do not assume |

Implementation: do **not** change production `NewSession` to run this
probe. Drive raw JSON-RPC in the test (same pattern as Phase D's
models-update exec), or add a test-only hook behind `//go:build live_grok`
that is not reachable from `grok.New`. Production code stays off until
the table is interpreted.

### Interpretation (deterministic)

* **yolo row matches the table** and is therefore grok-native
  per-session always-approve: **do not wire it as a replacement for
  `AlwaysApprove` or for MADR 0049.** Those already work. Record
  "yoloMode works; not taken — duplicates existing controls".
  Optional later MADR if someone wants spawn-without-`--always-approve`.
* **auto row allows the write with zero permission requests** and
  baseline did not: that is grok classifier auto per session.
  **Still do not replace 0049.** Record the matrix in MADR 0081.
  Wiring `autoMode` as a *fourth* advertised mode is a product
  decision (two autos). Stop and update MADR 0081 rather than
  adding a mode in this plan.
* **Neither row changes behaviour vs baseline:** record "accepted
  on the wire, inert for ACP writes on 1.0.3". No production Meta.
* **yolo or auto fails session/new:** record the error. No production
  Meta.

### Production change allowed in this phase

None, unless a follow-up MADR after this measurement says otherwise.
The commit is **T-H1** `TestLiveGrokSessionMetaDiscrimination` plus a
short note in MADR 0081 More Information stating which of the four
bullets above is true.

T-H1 uses `startACP` (Phase D helper). It must not call production
`grok.New` / `Provider.Start` to inject `_meta` — that would require
the production wiring this phase forbids. Three raw `session/new`
calls, one prompt each, inspect (a) whether a
`session/request_permission` frame arrived and (b) whether
`probe.txt` exists in the temp dir. Timeout per turn: 90s.

### Tests (required deliverable)

* T-H1 written and run once. The test file is the phase.
* Zero production `NewSession` Meta changes in this commit.

### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -run SessionMeta -count=1
```

This spends tokens (three short turns). Run once.

---

## Phase I — Pin `_x.ai/session/fork`; implement only if complete

**MADR:** P2.8
**Files (measure):** new `internal/provider/grok/live_fork_test.go`
**Files (only if probe returns a new session id):**
`internal/provider/acpagent/session.go`,
`internal/provider/grok/commandtable.go`

### Probe

Raw JSON-RPC after `session/new` (`sid`, `cwd` = `t.TempDir()`).
Try **in this order**, stop at the first `result` that contains a
session id different from `sid`:

1. `{sourceSessionId: sid, sourceCwd: cwd}`
2. `{sourceSessionId: sid, sourceCwd: cwd, cwd: cwd}`
3. `{sessionId: sid, sourceSessionId: sid, sourceCwd: cwd}`
4. `{sourceSessionId: sid, sourceCwd: cwd, mcpServers: []}`

MADR 0081 already recorded (1) without `sourceCwd` →
`missing field sourceCwd`. Do not retry shapes that omit `sourceCwd`.

If every shape is an error: **stop**. Leave `/fork` as `KindNone`.
Commit the test that documents the errors. Do not guess a fifth
field.

### If a shape succeeds

1. Implement `provider.ForkSession` on `acpagent.session`:

   ```go
   func (s *session) Fork(ctx context.Context, opts provider.ForkOptions) (provider.ForkResult, error)
   ```

   Send `_x.ai/session/fork` via `rawRequest` with the **exact**
   keys from the winning probe. Ignore `opts.LastTurnID` unless the
   winning payload had an equivalent field (it did not, as of the
   MADR probe). Return `ForkResult{AgentSessionID: <new id>,
   ForkedFromID: s.agentID}`.

2. Compile-time check: `var _ provider.ForkSession = (*session)(nil)`.
   This makes grok grow `/fork` as `KindOp` / `OpFork` because
   `commands.go` sets `state.Ops[OpFork]` from the interface.

3. Remap grok `"fork"` from `KindNone` to
   `{Kind: command.KindOp, Op: command.OpFork}`.
   Do **not** change other providers.

4. Live test: `Start`, `sess.(provider.ForkSession).Fork(...)`,
   require `AgentSessionID != ""` and `!=` the original
   `AgentSessionID()`.

5. `cmdFork` already exists (`session/commands.go`). Do not change
   it unless the manager requires a field grok cannot supply — if
   so, stop and update MADR 0081 rather than weakening `ForkOptions`.

6. **T-I1** `TestLiveGrokSessionForkShapes` walks the four shapes
   above via `startACP` and `t.Log`s every error. Practical rule:
   T-I1 always logs results. It `t.Fatal`s only when
   `commandTable["fork"].Kind == command.KindOp` (T-I2 has landed)
   and the live fork still errors. T-I2 is absent unless T-I1 found
   a winning shape.

7. **T-I2** `TestGrokForkIsOpFork` (same `commandtable_test.go` as
   T-F1): `commandTable["fork"].Kind == command.KindOp` and
   `.Op == command.OpFork`. Write this file-addition **only** in the
   implement commit.

### Tests (required deliverable)

* T-I1 always lands.
* T-I2 and the live `Fork()` success assertion land only with the
  implementation.

### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -run Fork -count=1
# only if implementing:
go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/command/ -count=1
make pre-add-check FILES="…"
```

### Acceptance

Either grok `/fork` is `OpFork` and a live Fork returns a new id, or
`/fork` remains `KindNone` and the live test records the failed
shapes. No half-wired handler.

---

## Phase J — Docs and MADR status

**MADR:** Confirmation
**Files:** `docs/config.md`, `README.md`,
`docs/0081-MADR-grok-1.0.3-surface-parity.md`, this plan

### Steps

1. `docs/config.md` `providers.grok.reasoning_effort` row: change
   the example list from `low, medium, high` to `low, medium, high,
   xhigh` and note that the live model advertises the set (grok-4.6
   includes `xhigh`; grok-4.5 does not). Do not add a new config key
   for `--no-auto-update`; add one sentence under the grok args /
   spawn description: daemon-spawned grok always gets
   `--no-auto-update` (MADR 0081 P1.3).

2. `README.md` grok provider row: if it lists models or
   `reasoning_effort` values, match config.md. If it lists
   `allowed_tools` as working, leave the existing "measured no-op"
   language (0050); this plan did not re-measure that.

3. Set MADR 0081 `status` to `accepted` only after A–F have landed
   and G–I have either landed or recorded a stop. Date the update.
   In More Information, write the Phase G/H/I outcomes as facts
   (promoted / not taken / wired).

4. Set this plan's status to **Complete** and list the commit SHAs
   per phase, same shape as [0050-PLAN](0050-PLAN-grok-cli-surface-drift.md).

5. Do not rewrite MADR 0038/0039/0050/0052 historical measurements.

6. In MADR 0081 Confirmation, tick each T-* row that landed. For
   T-G2 / T-I2, write `not taken` in the MADR if the gate failed.

### Tests (required deliverable)

None new. J is docs-only. Re-run the unit net so a stray edit did
not break T-A1…T-F2.

### Verification

Read the three docs for internal consistency: floor models,
`--no-auto-update`, `xhigh`, `/review`, and whatever G–I actually
did. No `make` step required unless a Go comment in those files
changed (it should not).

## Verification

End-to-end, after all landed phases. Tick only with command output
that names the test function.

```bash
grok --version
go test ./internal/provider/grok/ ./internal/provider/acpagent/ \
  ./internal/command/ ./internal/picker/ -count=1
go test -tags live_grok ./internal/provider/grok/ -count=1
make pre-add-check
make test
```

| ID | Command | Pass condition |
| --- | --- | --- |
| T-A1 | `go test ./internal/provider/grok/ -run StaticModelsFloor` | floor is `grok-4.6`, `grok-4.5` |
| T-A2 | `go test -tags live_grok … -run ColdCatalog` | not `SourceStatic`, `len <= 3` |
| T-B1 | `go test ./internal/provider/grok/ -run 'DefaultArgs\|SpecModelArgs'` | `--no-auto-update` before `agent` |
| T-P4 | `go test ./internal/provider/grok/ -run P4Flags` | no P4 flag in argv |
| T-C1 | `go test ./internal/provider/grok/ -run RegistersBothModelsUpdate` | both method names registered |
| T-E1 | `go test ./internal/provider/acpagent/ -run Grok46XHigh` | levels `low,medium,high,xhigh`; default `high` |
| T-E2 | `go test ./internal/provider/acpagent/ -run SetThinkingLevelFixed` | `ErrThinkingLevelFixed` |
| T-E3 | `go test ./internal/picker/ -run Grok46DualDefault` | single default `high` |
| T-F1 | `go test ./internal/provider/grok/ -run ReviewIsNative` | `KindNative` |
| T-F2 | `go test ./internal/command/ -run GrokReview` | advertised yes / unaadvertised no |
| T-B2/D1 | `go test -tags live_grok … -run Argv` | `model46` + `reasoningXHigh` start |
| T-C2 | `go test -tags live_grok … -run ModelsUpdateMethodName` | stdout has `_x.ai/models/update` |
| T-D2 | `go test -tags live_grok … -run SetModel` | `grok-4.6` ok; `grok-build` / `grok-code-fast-1` fail |
| T-D3 | `go test -tags live_grok … -run InitializeMeta` | live catalog has `grok-4.6` |
| T-D4 | `go test -tags live_grok … -run CloseSucceeds` | `Close` nil |
| T-F3 | `go test -tags live_grok … -run CommandCatalog` | advertised includes `review` |
| T-F4 | `go test -tags live_grok … -run CommandTableMatchesReality` | `/compact` still silent |
| T-G1 | `go test -tags live_grok … -run Loop` | measured; promote only on schedule |
| T-H1 | `go test -tags live_grok … -run SessionMeta` | matrix recorded; no production Meta |
| T-I1 | `go test -tags live_grok … -run SessionForkShapes` | shapes logged; implement only on new id |
| 0049 | `go test -tags live_grok … -run AutoDiscriminationPair` | still discriminates (H must not break it) |

## Rollout and Rollback

### Rollout

* No config migration. No new required keys.
* Behaviour changes on upgrade:
  * `models.list` before harvest offers `grok-4.6` / `grok-4.5`
    instead of `grok-4.5` / `grok-code-fast-1` / `grok-4`.
  * Every grok process is spawned with `--no-auto-update`.
  * `/review` on grok is offered when advertised (was "not here").
* Prewarm: first daemon start after B rebakes `cfg.Args`; old
  prewarmed processes are not reused across a daemon restart.
* Release-note bullets: those three items. Do not claim `/loop`,
  fork, or `_meta.autoMode` unless G–I implemented them.

### Rollback

* Revert the phase commit. Each phase is independent enough to
  revert alone:
  * A: floor goes stale again; live harvest still correct
  * B: grok may auto-update under the daemon again
  * C: mid-session catalog refresh name is wrong again; initialize
    path still works
  * F: `/review` becomes "not here" again
  * G/I: command table / specs return to previous vocabulary
* No on-disk grok session format is rewritten by A–F. Phase I fork,
  if implemented, creates additional grok-local sessions under
  `~/.grok/sessions`; reverting the code does not delete them.
  Operators can `grok sessions delete <id>` if needed.

### Stop / escalate

Stop and update MADR 0081 (do not improvise) if:

* `grok --version` is not 1.0.x, or clap placement for
  `--no-auto-update` / `--permission-mode` has moved
* `session/set_model grok-4.6` fails on the implementer's host
* Phase G/H/I probe results contradict the MADR's "accepted on the
  wire" claims in a way that would require a new product mode
* Any phase needs a remote-protocol or mobile change
