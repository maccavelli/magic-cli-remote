# Implement adopt grok 1.0.4's pinned session fork and re-pin the 0081 contract

Associated MADR: [0092-MADR-grok-1.0.4-surface-parity.md](0092-MADR-grok-1.0.4-surface-parity.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **Complete** (2026-08-15). Phases A–F landed as commits
  `fa2f7ff` (A, 1.0.4 pin + T-F1), `4813c03` (B, T-F5 load gate),
  `ef4d164` (C, ForkSession + KindOp), `d8f82c9` (D, T-F3 live
  Fork+load), plus this docs commit (F). Phase E was run-only:
  argv/set_model/initialize/models-update/close/catalog/compact/
  subagent green; T-A same 0081 matrix (no write); 0049 auto pair
  still discriminates.
- **Date**: 2026-08-15
- **Keyed to**: repository HEAD at plan-write time; grok **1.0.4
  (`d846eb93d94d`) [stable]** at `/Users/saxsmith/.grok/bin/grok`.
  Re-record `grok --version` at the top of the first live-test commit
  if the implementer's binary differs. If it is not 1.0.4, **stop**
  and update MADR 0092 rather than guessing a new schema.
- **Scope**: `internal/provider/grok` (command table, live pins,
  comments), `internal/provider/acpagent` (ForkSession + request
  builder only), `internal/command` (one resolve test),
  `docs/0092-MADR-*.md` status, this plan. No remote-protocol change.
  No mobile change. No new `providers.grok.*` config keys. No
  `command/specs.go` additions. Do not edit goose/codex/opencode/kilo
  /fake command tables.

## Goal

Make the grok provider tell the truth about grok 1.0.4: the 0081
spawn/catalog contract is re-pinned to this binary, and `/fork`
either becomes a working `KindOp` / `OpFork` that survives
`Manager.Fork` → new process → `session/load`, or it stays
`KindNone` with a live test that names why load failed.

Tests are a deliverable of every phase, not a follow-up. MADR 0092
Confirmation IDs (T-F1 … T-K) are the contract. A phase commit that
changes production code without its named tests is incomplete.

## MADR assessment (grounded)

The 0092 decision is **accepted as written**, with one execution
gate the MADR originally under-specified. That gate is now in the
MADR (T-F5) and is not a new architecture.

### What the MADR got right

| Claim | Code fact |
| --- | --- |
| grok `/fork` is `KindNone` / `ReasonNoFork` | `internal/provider/grok/commandtable.go:59` |
| KindNone is final even if the agent advertises `fork` | `internal/command/command.go:223-226` (`Resolve`: explicit `KindNone` returns before advertisement) |
| `cmdFork` already dispatches `OpFork` | `internal/session/commands.go:235-236` → `cmdFork` at `:986` |
| `Ops[OpFork]` is the live `ForkSession` interface, not the table | `internal/session/commands.go:71` |
| `KindOp` is available only when `state.Ops[OpFork]` is true | `internal/command/command.go:279-280` `available` |
| ACP raw RPC already exists | `acpagent/session.go:2330-2346` `rawRequest` (used by `SetModel` at `:793`) |
| Session already stores `agentID` and `cwd` | `acpagent/session.go:61-63`, accessors `:238-239` |
| acpagent is grok-only today | only `internal/provider/grok/grok.go` constructs `acpagent.New` |
| Static floor, `--no-auto-update`, models-update names | `grok.go:28-31`, `:162`, `:88-89` |
| P4 flags are already forbidden in argv | `grok_test.go:240-252` `TestDefaultArgsDoesNotEmitP4Flags` — **does not yet list `--no-ask-user`** |
| 0081 T-I1 walks four losing shapes and parses `sessionId` | `live_fork_test.go:37-45`, `forkSessionID` at `:77-89` |
| Other providers stay `KindNone` for fork | goose `commandtable.go:32`, fake `fake.go:157`; spec default is `KindNone` (`specs.go:120-124`) |

### What the MADR under-specified (now a plan gate, also added to the MADR)

`Manager.Fork` (`internal/session/manager.go:1842-1895`) does **not**
attach the forked grok session to the existing stdio process. After
`fs.Fork` it always:

1. Checks `res.ForkedFromID` is empty or equals `sess.AgentSessionID()`.
2. Calls `m.Create` with `StartOptions.AgentSessionID = newAgentID`
   and the parent's `CWD` / model / mode.

For grok, `Create` → `acpagent.Provider.Start` with a non-empty
`AgentSessionID` is a **new `grok` process** and ACP `session/load`
(`acpagent.go:834-864`), gated on `agentCaps.LoadSession` (1.0.4
still advertises `loadSession: true`). `loadTimeout` is 120s.

The 0092 probe proved `_x.ai/session/fork` works **in-process**. It
did not prove the new id is loadable from a second process. Shipping
`KindOp` without that proof turns `/fork` into "RPC succeeded, then
Create failed" — worse than today's honest `KindNone`.

**T-F5 is therefore the implement gate.** Production `Fork` and the
table remap happen only after a second `Start` loads the forked id.
If T-F5 is red: leave `/fork` `KindNone`, record the load error in
MADR 0092, do **not** invent a same-process multi-session attach.
That would be a new MADR (it fights `--no-leader` and the
one-session-per-process model).

### What the MADR must not be read as

* "Implement `Fork` on `acpagent.session` and `/fork` works." The
  product path is Fork + load. Both are required.
* "Reuse 0081-PLAN Phase I as-is." 0081 Phase I is Complete and
  forbade guessing `newCwd`. This plan names the winning shape and
  the load gate.
* "Add `--no-ask-user` or web-search domain config." P3/P4 stay out.
* "Replace MADR 0049 auto." P2.3 is run-only.

## Scope

### In scope

MADR 0092 P1.1 (fork, gated on T-F5), P1.2 (re-pin comments and live
headers to 1.0.4), T-K extension (`--no-ask-user` on the existing
P4 argv test), and P2 confirmation runs (no production).

### Out of scope (MADR P3 / P4 — do not implement)

`--no-ask-user` as a spawn flag, `[toolset.web_search]` daemon
config, `GROK_SESSION_ID` plumbing, `StopCancelled` / `updatedInput`
hooks, `GROK_SESSION_SEARCH`, `[ui] follow_up_behavior`, image/video
kill switches, `--worktree`, `--minimal`, `--oauth`, `--json-schema`,
`--max-turns`, `--agents`, memory, `grok agent serve` / `headless` /
`leader`, `grok import`, model id `grok-build`,
`session/set_config_option`, mid-session `xhigh`, `session/list` as
the phone roster, `_x.ai/fs/*` / `_x.ai/git/*`, hook slash commands
in `specs.go`, host skills (`code-review`, `implement`, …) in the
canonical vocabulary, replacing 0049 `auto` with `_meta.autoMode`.

Do not change `cmdFork`, `Manager.Fork`, `ForkOptions`, or
`ForkResult`. If T-F5 or the parent-id check cannot be satisfied
with the existing types, **stop** and update MADR 0092.

## Plan-level decisions (MADR left these to the plan)

These are execution choices, not new architecture:

1. **Winning fork payload is exactly**
   `{sourceSessionId, sourceCwd, newCwd}`. Result id field is
   `newSessionId`. Parent field is `parentSessionId`. Extract a
   package-level builder so the unit test does not need a fake ACP
   connection:

   ```go
   // forkRequest is the pinned 1.0.4 _x.ai/session/fork body
   // (MADR 0092 P1.1). newCwd == sourceCwd == the daemon session cwd.
   func forkRequest(agentID, cwd string) map[string]any {
       return map[string]any{
           "sourceSessionId": agentID,
           "sourceCwd":       cwd,
           "newCwd":          cwd,
       }
   }
   ```

2. **Ignore `ForkOptions.LastTurnID`.** `/fork [turn-id]`
   (`specs.go:120-124`) may pass a turn id through `cmdFork` →
   `Manager.Fork`. Grok's winning payload has no equivalent field.
   A grok `/fork <anything>` forks the whole conversation. Do not
   error on a non-empty LastTurnID (that would make `/fork foo`
   fail). Do not put it on the wire.

3. **Reject `ForkOptions.DeferGoalContinuation`.** Grok is not a
   `GoalSession`, so `Manager.Fork:1863-1867` will not set this
   today. Match `httpagent/session.go:196-198`:
   `fmt.Errorf("defer goal continuation not supported")`. Do not
   silently ignore a true flag.

4. **`ForkedFromID` is the wire `parentSessionId` if non-empty,
   else `s.agentID`.** `Manager.Fork:1875-1877` errors if
   `ForkedFromID` is non-empty and ≠ `sess.AgentSessionID()`. The
   0092 probe's `parentSessionId` equalled the source. If a future
   binary returns a different parent, T-F3 fails and we update the
   MADR — do not drop the field to sneak past the check.

5. **Empty `newSessionId` is an error**, not `ErrForkNothing`.
   `ErrForkNothing` is the Codex "no rollout found" case
   (`provider.go:193-194`); `cmdFork` maps it to "Nothing to fork
   yet". A successful grok RPC with a blank id is a decode bug.

6. **`newCwd` is always `s.cwd`**, the ACP session cwd set at
   `session/new|load`, not `procDir`. `Manager.Create` reuses
   `meta.CWD`, which is that same directory.

7. **Implement `Fork` on `acpagent.session` (grok-only transport).**
   Do not add a `Spec` opt-in. A second acpagent provider would
   inherit it; none exists. Do not implement it on `acphttp`
   (goose).

8. **`--no-ask-user` is added to `TestDefaultArgsDoesNotEmitP4Flags`
   only.** Do not emit the flag. The 0081 test omitted it because
   1.0.3 did not call it out as a P4 leak; 0092 T-K does.

9. **Live tests skip when `grok` is not on `PATH`.** They are not a
   CI gate. Run them on this host before marking a measure-gated
   phase done.

10. **One phase → one commit.** Do not push unless asked.
    `git commit` with no `-m` / `--message` / `-F`. Before `git add`
    of any Go file: `make pre-add-check FILES="…"`.

## Tests

This section is the implementation of MADR 0092 Confirmation. Copy
function names exactly so reviews can grep them.

### Rules

* **Unit first.** Every production change has a unit test that fails
  if the change is reverted. Live tests pin the binary; they do not
  replace the unit test.
* **One ID → one function** unless the MADR row says "extend".
  Extensions add subtests (`t.Run`) rather than silent new
  assertions in an unrelated test.
* **Skip, don't fail, without grok.** `t.Skip("grok not in PATH")`.
* **B then C.** T-F5 (load) is red → no production `Fork`, no
  table remap. A red T-F5 is a documented stop, not a licence to
  implement "the RPC part".
* **Do not weaken T-C.** `/compact` staying silent is an 0081
  invariant. If it starts answering, stop and update MADR 0092;
  do not "fix" compact in this plan.
* **Do not spend tokens in unit tests.** T-F5 and T-F3 start real
  grok processes; run them at acceptance, not in a loop.

### Inventory

| ID | Function | File | When |
| --- | --- | --- | --- |
| T-F1 | extend `TestLiveGrokSessionForkShapes` | `live_fork_test.go` | Phase A |
| T-F5 | `TestLiveGrokForkLoadOnNewProcess` | `live_fork_test.go` | Phase B **before** any production Fork |
| T-F4 | `TestForkRequestOmitsOptionalForkOptions` | `internal/provider/acpagent/fork_test.go` **new** | Phase C |
| T-F4b | `TestForkRequestRequiresIDs` | same file | Phase C |
| T-F2 | `TestGrokForkIsOpFork` | `commandtable_test.go` | Phase C (same commit as remap) |
| T-F2b | `TestResolveGrokForkIsOpWhenSessionCanFork` | `internal/command/command_test.go` | Phase C |
| T-K | extend `TestDefaultArgsDoesNotEmitP4Flags` with `--no-ask-user` | `grok_test.go` | Phase C |
| T-F3 | `TestLiveGrokForkSession` | `live_fork_test.go` | Phase D |
| T-P | existing `TestLiveGrokArgvAcceptsEveryConfiguredFlag` (header → 1.0.4) | `live_argv_test.go` | Phase A header; Phase E run |
| T-A | existing `TestLiveGrokSessionMetaDiscrimination` | `live_sessionmeta_test.go` | Phase E **run only** |
| T-S | existing `TestLiveGrokSubagentSuppressedAndPromoted` | `live_subagent_test.go` | Phase E **run only** |
| T-C | existing `TestLiveGrokCommandTableMatchesReality` `/compact` | `live_command_test.go` | Phase E **run only** |

Existing tests that must stay green and are not rewritten:
`TestStaticModelsFloor`, `TestSpecRegistersBothModelsUpdateNames`,
`TestDefaultArgsPutsGlobalsBeforeSubcommand`,
`TestGrokReviewIsNativeWhenAdvertised`,
`TestProvidersDeclareEveryCanonicalCommand`,
`TestLiveGrokModelsUpdateMethodName`,
`TestLiveGrokSetModelWireContract`,
`TestLiveGrokInitializeMetaWireContract`,
`TestLiveGrokCloseSucceeds`,
`TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow`,
`TestLiveGrokAutoDiscriminationPair`.

## Implementation Steps

### Ground rules (every phase)

1. One phase → one commit. Do not push unless asked.
2. Before `git add` of any Go file:
   `make pre-add-check FILES="…touched.go files…"`.
3. `git commit` with no `-m` / `--message` / `-F`.
4. The binary is the contract. If a step's expected clap/RPC result
   disagrees with the implementer's `grok --version`, stop and update
   MADR 0092 rather than guessing.
5. Keep exactly one `in_progress` todo; mark `completed` only after
   the phase's **named tests** are written and green.
6. Live tests: `//go:build live_grok`, `package grok_test`. Raw-stdio
   probes use `startACP` from `live_helpers_test.go`. Provider-level
   probes use `grok.New` + `p.Ready()` skip.
7. Do not edit P4 flags into `defaultArgs`, mobile, or protocol docs.
8. Do not edit `0081-PLAN` or reopen 0081 todos.

### Verified baseline (do not rediscover)

Measured 2026-08-15 against grok 1.0.4; recorded in MADR 0092.
Frames: `/tmp/grok104-probe/`.

| Claim | Where |
| --- | --- |
| Global policy flags still rejected after `agent` | MADR 0092 context; same 0050/0081 placement |
| `--no-auto-update` accepted before `agent`, rejected after | already in `defaultArgs` |
| `--no-ask-user` accepted before `agent` (exit 0), rejected after (exit 2) | 0092 probe; **not** in `defaultArgs` |
| Live models: `grok-4.6` (default), `grok-4.5`; `grok-build` / `grok-code-fast-1` rejected | initialize + `session/set_model` |
| `_x.ai/models/update` (slash, no id) still emitted | probe `methods_seen` |
| `_x.ai/session/fork` `{sourceSessionId, sourceCwd, newCwd}` → `newSessionId` | probe id=22 |
| `{sourceSessionId}` → `missing field sourceCwd` | probe id=20 |
| `{sourceSessionId, sourceCwd}` → `missing field newCwd` | probe id=21 |
| `session/load` capability still true | initialize `loadSession: true` |
| `session/compact` method-not-found; `/compact` advertised | probe id=24; catalog |
| Production `session/new` sends only `Cwd` + `McpServers` | `acpagent.go:870-873` |
| `SetThinkingLevel` is `ErrThinkingLevelFixed` | `acpagent/session.go` (unchanged) |

---

### Phase A — Re-pin 1.0.4 and teach T-F1 the winning shape

**MADR:** P1.2, T-F1, T-P header
**Files:** `internal/provider/grok/live_helpers_test.go`,
`live_argv_test.go`, `live_setmodel_test.go`,
`live_initializemeta_test.go`, `live_modelsupdate_test.go`,
`live_command_test.go`, `live_commandcatalog_test.go`,
`live_loop_test.go`, `live_sessionmeta_test.go`,
`live_fork_test.go`, `grok.go` comments, `commandtable.go`
comments. **No production behaviour change.**

#### Steps

1. Replace every "grok 1.0.3 (`1a29d5bc12d4`)" pin in those files
   with "grok 1.0.4 (`d846eb93d94d`) [stable] (MADR 0092)". Do not
   rewrite the behavioural assertions.

2. In `live_fork_test.go`:
   * Change the file comment: 0081's four shapes failed with
     `missing field newCwd`; 0092's fifth shape
     `{sourceSessionId, sourceCwd, newCwd}` is the winner.
   * Append that fifth shape to the `shapes` slice as
     `{"source+sourceCwd+newCwd", {sourceSessionId, sourceCwd, newCwd}}`.
     Keep the four losing shapes **in front** so a schema revert
     still logs `missing field newCwd`.
   * Change `forkSessionID` to prefer `newSessionId`, then
     `sessionId`, then nested `result.sessionId`:

     ```go
     if id, _ := res["newSessionId"].(string); id != "" {
         return id
     }
     ```

   * **T-F1 fatal rule (stronger than 0081):** if the fifth shape
     errors or returns an id equal to `sid`, `t.Fatal`. The four
     losing shapes only `t.Log`. After Phase C remaps KindOp, the
     existing "KindOp but no winner → Fatal" rule remains as a
     backstop.

3. Do not implement `Fork`. Do not touch `commandtable.go` mappings
   (comments only).

#### Tests (required deliverable)

* T-F1 extended and green on this host.
* Compiles: `go test -tags live_grok ./internal/provider/grok/ -run SessionForkShapes -count=1`

#### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokSessionForkShapes -count=1
```

#### Acceptance

The fifth shape logs a `newSessionId` ≠ source. The four older
shapes still error. `commandtable.go` `"fork"` is still `KindNone`.

#### Stop

If the fifth shape fails on the implementer's 1.0.4, **stop**. Update
MADR 0092. Do not guess a sixth field.

---

### Phase B — T-F5 load gate (no production)

**MADR:** T-F5
**Files:** `internal/provider/grok/live_fork_test.go` only

This phase answers: *can a second grok process `session/load` the
id `_x.ai/session/fork` just created?* That is what
`Manager.Fork` → `Create` will do.

#### Probe (write this as the test body)

`TestLiveGrokForkLoadOnNewProcess` (`package grok_test`):

1. `dir := t.TempDir()`.
2. `p := grok.New(grok.Config{AlwaysApprove: true})`; skip if
   `!p.Ready()`.
3. `parent, err := p.Start(ctx, provider.StartOptions{Name: "fork-load-src", CWD: dir})`.
4. Read `sid := parent.AgentSessionID()`; `cwd := parent.CWD()`.
   Both must be non-empty.
5. Drive **raw** `_x.ai/session/fork` with `forkRequest` keys
   (do not call a production `Fork` — it does not exist yet).
   Use `startACP` **or** a tiny helper that sends one raw RPC on
   the already-started session.

   Preferred: do **not** start a third grok via `startACP`. Add a
   test-only way to send one raw method on the live session, **or**
   startACP a sibling process solely for the fork RPC, then load
   *that* id.

   Deterministic choice: **use `startACP` for the fork RPC** (same
   helper as T-F1) so this phase does not require `rawRequest`
   exported. Then `p.Start` the load. Two processes, matching
   production.

   Sequence:
   * `ap := startACP(t, nil)` — this creates session id S1 in cwd C1
     (`startACP` uses `t.TempDir()` for `session/new`).
   * Recover C1 from `session/new` `_meta.currentWorkingDirectory`
     (same as T-F1).
   * Send `_x.ai/session/fork`
     `{sourceSessionId: S1, sourceCwd: C1, newCwd: C1}`.
   * Parse `newSessionId` via the updated `forkSessionID`.
   * `child, err := p.Start(ctx, provider.StartOptions{Name: "fork-load-dst", CWD: C1, AgentSessionID: newID})`.
   * `err` must be nil. `child.AgentSessionID()` must equal `newID`.
   * `child.Close`; `ap` cleanup is via `t.Cleanup`.

6. Timeout: parent `Start` 60s; load `Start` 120s (`loadTimeout`).

#### If the load fails

**Stop.** Do not implement Phase C. Commit T-F5 as a failing-to-load
documentation test only if you can make it `t.Log` the error and
`t.Fatal` with a message that names `session/load`. Then update
MADR 0092 Decision Outcome: fork stays `KindNone`; T-F5 is the
evidence. Do not add a same-process attach.

#### Tests (required deliverable)

* T-F5 written and green (or red + MADR update; never green-by-skip
  except `grok not in PATH`).

#### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokForkLoadOnNewProcess -count=1
```

#### Acceptance

A second `grok` process loaded the forked id. Phase C is allowed.

---

### Phase C — Implement `ForkSession` and remap `/fork`

**MADR:** P1.1, T-F2, T-F4, T-K
**Files:**
`internal/provider/acpagent/session.go`,
`internal/provider/acpagent/fork.go` **new** (builder + `Fork`),
`internal/provider/acpagent/fork_test.go` **new**,
`internal/provider/grok/commandtable.go`,
`internal/provider/grok/commandtable_test.go`,
`internal/provider/grok/grok_test.go`,
`internal/command/command_test.go`

Only start this phase if Phase B is green.

#### Production

1. New file `internal/provider/acpagent/fork.go`:

   ```go
   package acpagent

   func forkRequest(agentID, cwd string) map[string]any {
       return map[string]any{
           "sourceSessionId": agentID,
           "sourceCwd":       cwd,
           "newCwd":          cwd,
       }
   }

   var _ provider.ForkSession = (*session)(nil)

   func (s *session) Fork(ctx context.Context, opts provider.ForkOptions) (provider.ForkResult, error) {
       if opts.DeferGoalContinuation {
           return provider.ForkResult{}, fmt.Errorf("defer goal continuation not supported")
       }
       s.mu.Lock()
       agentID := s.agentID
       cwd := s.cwd
       closed := s.closed
       s.mu.Unlock()
       if closed {
           return provider.ForkResult{}, fmt.Errorf("session closed")
       }
       if agentID == "" || cwd == "" {
           return provider.ForkResult{}, fmt.Errorf("fork: missing session id or cwd")
       }
       var resp struct {
           NewSessionID    string `json:"newSessionId"`
           ParentSessionID string `json:"parentSessionId"`
       }
       if err := s.rawRequest(ctx, "_x.ai/session/fork", forkRequest(agentID, cwd), &resp); err != nil {
           return provider.ForkResult{}, err
       }
       if resp.NewSessionID == "" {
           return provider.ForkResult{}, fmt.Errorf("fork: empty newSessionId")
       }
       parent := resp.ParentSessionID
       if parent == "" {
           parent = agentID
       }
       return provider.ForkResult{AgentSessionID: resp.NewSessionID, ForkedFromID: parent}, nil
   }
   ```

   Place `Fork` in `fork.go`, not `session.go`, so the unit tests
   share a file with the builder. Update the `rawRequest` comment
   in `session.go:2331` to name `_x.ai/session/fork` as well as
   `session/set_model`.

   Do **not** log the copied-message counts. Do **not** write
   `LastTurnID` onto the wire. Do **not** call `session/load`
   inside `Fork` — `Manager.Create` does that.

2. `commandtable.go`: replace

   ```go
   "fork":   {Kind: command.KindNone, Note: command.ReasonNoFork},
   ```

   with

   ```go
   // 1.0.4 _x.ai/session/fork {sourceSessionId,sourceCwd,newCwd} returns
   // newSessionId; Manager.Fork then session/load on a new process
   // (MADR 0092 P1.1). available() is false if the live session is not
   // a ForkSession (should not happen on acpagent).
   "fork": {Kind: command.KindOp, Op: command.OpFork},
   ```

   Leave `"compact"` `KindNone`. Leave goose/fake/spec default
   `KindNone`.

3. Do not change `cmdFork`, `Manager.Fork`, `specs.go`, other
   provider tables, or mobile.

#### Tests (required deliverable)

**T-F4** `TestForkRequestOmitsOptionalForkOptions` in
`acpagent/fork_test.go`:

```go
func TestForkRequestOmitsOptionalForkOptions(t *testing.T) {
    got := forkRequest("sid-1", "/tmp/cwd")
    want := map[string]any{
        "sourceSessionId": "sid-1",
        "sourceCwd":       "/tmp/cwd",
        "newCwd":          "/tmp/cwd",
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %#v want %#v", got, want)
    }
    for _, leak := range []string{"lastTurnId", "LastTurnID", "deferGoalContinuation", "sessionId", "cwd"} {
        if _, ok := got[leak]; ok {
            t.Errorf("payload leaked %s", leak)
        }
    }
}
```

**T-F4b** `TestForkRequestRequiresIDs`: `forkRequest("", "/tmp")`
and `forkRequest("sid", "")` still return the map (builder is
dumb); `Fork` is what rejects empties. Cover `Fork` closed/empty
by constructing a `session{closed: true}` or
`session{agentID: "", cwd: "/x"}` and calling `Fork` — no
connection needed for those branches.

Add:

```go
func TestForkRejectsDeferGoalContinuation(t *testing.T) {
    s := &session{agentID: "s", cwd: "/tmp"}
    _, err := s.Fork(context.Background(), provider.ForkOptions{DeferGoalContinuation: true})
    if err == nil || !strings.Contains(err.Error(), "defer goal continuation") {
        t.Fatalf("err=%v", err)
    }
}

func TestForkClosedSession(t *testing.T) {
    s := &session{agentID: "s", cwd: "/tmp", closed: true}
    _, err := s.Fork(context.Background(), provider.ForkOptions{})
    if err == nil || !strings.Contains(err.Error(), "closed") {
        t.Fatalf("err=%v", err)
    }
}
```

**T-F2** `TestGrokForkIsOpFork` in `commandtable_test.go`:

```go
func TestGrokForkIsOpFork(t *testing.T) {
    m := commandTable["fork"]
    if m.Kind != command.KindOp || m.Op != command.OpFork {
        t.Fatalf("fork=%+v, want KindOp OpFork", m)
    }
}
```

**T-F2b** `TestResolveGrokForkIsOpWhenSessionCanFork` in
`command_test.go` (mirror `TestResolveGrokReviewForwardsWhenAdvertised`):

* grok table `{fork: KindOp OpFork}` + `Ops[OpFork]=true` →
  Available, KindOp.
* same table + `Ops` nil → not Available (falls through default
  KindNone).
* goose table `{fork: KindNone}` + `Ops[OpFork]=true` → still
  KindNone / unavailable (`TestExplicitNoneBeatsAdvertisement`
  already covers the KindNone-wins rule; still assert goose).

**T-K:** add `"--no-ask-user"` to the `forbidden` slice in
`TestDefaultArgsDoesNotEmitP4Flags`. Do not add it to `defaultArgs`.

`TestProvidersDeclareEveryCanonicalCommand` stays green because
grok already declares `fork`; only the Kind changes.

#### Verification

```bash
go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/command/ -count=1
make pre-add-check FILES="internal/provider/acpagent/fork.go internal/provider/acpagent/fork_test.go internal/provider/acpagent/session.go internal/provider/grok/commandtable.go internal/provider/grok/commandtable_test.go internal/provider/grok/grok_test.go internal/command/command_test.go"
```

#### Acceptance

`ForkSession` compiles on `*acpagent.session`. grok `/fork` is
`KindOp`. Payload builder has exactly three keys. `--no-ask-user`
is not in argv.

#### Stop

If `rawRequest` cannot decode the 1.0.4 result (wrapper object,
different casing), **stop** and update MADR 0092 with the actual
frame. Do not range over alternate key names in production.

---

### Phase D — Live production `Fork` + load

**MADR:** T-F3
**Files:** `internal/provider/grok/live_fork_test.go` only

#### Test body — `TestLiveGrokForkSession`

1. `p := grok.New(grok.Config{AlwaysApprove: true})`; skip if not ready.
2. `parent, err := p.Start(ctx, {Name: "fork-live", CWD: t.TempDir()})`.
3. `fs, ok := parent.(provider.ForkSession)`; `ok` must be true.
4. `res, err := fs.Fork(ctx, provider.ForkOptions{})`.
5. Require `err == nil`, `res.AgentSessionID != ""`,
   `res.AgentSessionID != parent.AgentSessionID()`,
   `res.ForkedFromID == parent.AgentSessionID()`.
6. `child, err := p.Start(ctx, {Name: "fork-live-child", CWD: parent.CWD(), AgentSessionID: res.AgentSessionID})`.
   This is the production Create path. Require `err == nil` and
   `child.AgentSessionID() == res.AgentSessionID`.
7. Close both.

Also call `fs.Fork(ctx, provider.ForkOptions{LastTurnID: "not-a-turn"})`
and require it **succeeds** (LastTurnID ignored). Do **not** assert
it forked at a turn boundary.

Optional negative: `Fork(..., DeferGoalContinuation: true)` must
error; this is already a unit test, live is optional.

#### Verification

```bash
go test -tags live_grok ./internal/provider/grok/ -run 'TestLiveGrokFork' -count=1
```

#### Acceptance

Production `Fork` + `session/load` both succeed on 1.0.4. T-F1 still
green (fifth shape + KindOp backstop).

#### Stop

If production `Fork` works but the second `Start` fails, Phase B
regressed or `Fork` is returning a different id than the raw RPC.
Do not remap back to KindNone in the same commit as a "fix" —
stop and update the MADR.

---

### Phase E — Confirmation live suite (run only)

**MADR:** T-P, T-A, T-S, T-C, existing 0081 pins
**Files:** none, unless a test header still says 1.0.3 (fix as a
docs-only comment in this commit).

#### Commands (run once, record pass/fail in the commit message
hook by leaving the tree clean — do not paste token-using
transcripts into the MADR)

```bash
go test -tags live_grok ./internal/provider/grok/ -count=1 \
  -run 'TestLiveGrokArgvAcceptsEveryConfiguredFlag|TestLiveGrokSetModelWireContract|TestLiveGrokInitializeMetaWireContract|TestLiveGrokModelsUpdateMethodName|TestLiveGrokCloseSucceeds|TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow|TestLiveGrokCommandTableMatchesReality|TestLiveGrokSubagentSuppressedAndPromoted'
```

`TestLiveGrokSessionMetaDiscrimination` and
`TestLiveGrokAutoDiscriminationPair` **spend tokens**. Run each
once:

```bash
go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokSessionMetaDiscrimination -count=1
go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokAutoDiscriminationPair -count=1
```

#### Rules

* T-A (`SessionMeta`): **log only**. If yolo/auto now writes a
  file, **do not** wire `_meta.autoMode`. Stop and open a new MADR
  (two autos). Production `NewSession` stays `{Cwd, McpServers}`.
* T-S: if the subagent test fails, fix only if the failure is a
  harness flake against 1.0.4's out-of-order preservation. Do not
  rewrite `HandleXAISessionNotification` "to take advantage" of
  the changelog.
* T-C: if `/compact` returns text, stop and update MADR 0092.
  Do not implement compact in this plan.
* T-P: if 1.0.4 rejects a current global, that is a 0050-class
  incident. Stop. New MADR, not a quiet argv shuffle.

#### Acceptance

Named tests green, or a documented MADR update for a real 1.0.4
behaviour change. No production edits in this phase unless a
comment still says 1.0.3.

---

### Phase F — Docs and MADR status

**MADR:** Confirmation
**Files:** `docs/0092-MADR-grok-1.0.4-surface-parity.md`, this plan.
Optional one-line note in `docs/config.md` only if a grok `/fork`
row already exists (it does not — grep is clean). Do not invent a
config key.

#### Steps

1. MADR 0092 YAML: `status: accepted`, keep `date: 2026-08-15`
   or set to the accept-commit day. Add a short
   "Implementation outcomes" paragraph under Decision Outcome
   naming which T-* landed and that T-F5 passed.
2. This plan: **Status: Complete**, list commit SHAs per phase
   (A–F) the way 0081-PLAN does.
3. Do not rewrite 0081. Add one sentence under 0092 More
   Information → Related records if needed: "0081 Phase I stop
   is superseded by 0092 T-F1/T-F5/T-F3."
4. No README table change required (`session.fork` is already
   listed as "where available").

#### Verification

```bash
# no Go in this commit unless a comment typo remains
ls docs/0092-MADR-grok-1.0.4-surface-parity.md docs/0092-PLAN-grok-1.0.4-surface-parity.md
```

#### Acceptance

MADR status accepted. Plan status Complete. Confirmation IDs
grep-able to green tests.

## Verification

Full gate after Phase D (and again after E):

```bash
go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/command/ ./internal/session/ -count=1
go test -tags live_grok ./internal/provider/grok/ -run 'TestLiveGrokFork|TestLiveGrokSessionForkShapes' -count=1
make pre-add-check FILES="internal/provider/acpagent/fork.go internal/provider/acpagent/fork_test.go internal/provider/acpagent/session.go internal/provider/grok/commandtable.go internal/provider/grok/commandtable_test.go internal/provider/grok/grok_test.go internal/command/command_test.go"
make race   # before any commit that includes the production Fork, per AGENTS.md
```

`make race` is not run by a hook. Run it yourself on the Phase C
commit.

## Rollout and Rollback

### Rollout

* Daemon-only. No protocol version bump. No mobile change. No
  config migration.
* Hosts still on grok &lt; 1.0.4: T-F1/T-F5 will fail locally if
  someone runs `live_grok` there. Production `Fork` will return
  the RPC error (`missing field newCwd` on 1.0.3). `cmdFork`
  already surfaces `Fork failed: …`. That is acceptable: 0092 is
  keyed to 1.0.4. Do not version-switch the payload.
* Behaviour change on 1.0.4: `/fork` becomes available in `/help`
  and autocomplete once the live session implements `ForkSession`
  (it will, on every grok session). Previously `/help` listed it
  as unavailable with `ReasonNoFork`.

### Rollback

* Revert the Phase C commit (table remap + `Fork`). `/fork`
  returns to `KindNone`. Phases A/B (tests and comments) can stay.
* Do not leave KindOp mapped with `Fork` deleted: `available()`
  would be false and `/fork` would fall back to spec default
  `KindNone`, but `TestGrokForkIsOpFork` would fail — that is the
  safety net. Never ship KindOp without `var _ ForkSession`.

## Stop conditions (copy these into the first commit if needed)

| Signal | Action |
| --- | --- |
| Implementer `grok --version` is not 1.0.4 | Update MADR 0092; do not implement from this plan |
| T-F1 fifth shape fails | New MADR / update 0092; no sixth field |
| T-F5 `session/load` fails | Keep KindNone; update 0092; no same-process attach |
| `parentSessionId` ≠ source `agentID` | Update MADR; do not drop `ForkedFromID` to bypass `Manager.Fork` |
| Result id field is not `newSessionId` | Update MADR; do not try a key list |
| T-A yolo/auto now writes a file | New MADR; do not replace 0049 |
| T-C `/compact` returns text | New MADR; do not implement compact here |
| T-P unexpected-argument on a current global | New MADR (0050-class) |

A phase that adds production code without its T-* row is incomplete.
A measure-gated T-* that is red is a documented stop, not a licence
to implement.
