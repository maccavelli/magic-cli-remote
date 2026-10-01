# MADR 0047 — Implementation plan: Codex default mode and auto sandbox

Companion to [MADR 0047](./0047-MADR-codex-default-mode.md). Read that first.

- **Status**: Implemented (2026-07-29)
- **Date**: 2026-07-29
- **Decision**: [MADR 0047](./0047-MADR-codex-default-mode.md)
- **Targets**: codex-cli app-server (0.145.0+ contract), Flutter mobile,
  daemon session helpers

---

## 0. Summary

| layer | change |
|---|---|
| `internal/provider/codex/mode.go` | add `default` mode first; `seedPolicy` helper |
| `internal/provider/codex/session.go` | `newSession` uses `seedPolicy` |
| `internal/session` | codex-shaped `defaultMode` regression case |
| `apps/mobile` `_ModeSelector` | stop `modes.first`; shared resolver |
| tests | list expectations; seed/create/turn wire; mobile honesty |
| docs | 0044 D5 list supersession note; config empty-field meaning |

No protocol message changes. No create-session mode picker.

### Dependency order

```
Phase 1 (codex table + seed + auto invariant)  ──┐
                                                 ├─> Phase 3 (cross-provider pins)
Phase 2 (mobile selector honesty) ──────────────┘
                                                 └─> Phase 4 (docs + verification)
```

Phases 1 and 2 are independent. Recommended: **1 → 2 → 3 → 4**.

---

## Phase 1 — Codex: `default` mode, seed, auto sandbox invariant

### 1.1 Mode table (`internal/provider/codex/mode.go`)

```go
const (
    modeDefault    = "default"
    modeReadOnly   = "read-only"
    modeAuto       = "auto"
    modeFullAccess = "full-access"
)
```

Prepend to `codexModes` (order is load-bearing):

```go
{
    mode: event.SessionMode{
        ID:   modeDefault,
        Name: modeDefault,
        Description: "Edit workspace; ask before shell, network, " +
            "and out-of-tree writes",
    },
    approvalPolicy: "on-request",
    sandbox:        "workspace-write",
},
// existing read-only, auto, full-access unchanged
```

`availableCodexModes` / `advertisedModes` / `findCodexMode` / `modeIDFor`
need no special cases.

### 1.2 `seedPolicy`

```go
// seedPolicy returns the approval policy, sandbox, and mode id a new
// session should run under (MADR 0047 D2).
func seedPolicy(cfg Config) (approval, sandbox, modeID string) {
    if id := modeIDFor(cfg, cfg.ApprovalPolicy, cfg.SandboxMode); id != "" {
        return cfg.ApprovalPolicy, cfg.SandboxMode, id
    }
    // never + empty sandbox → auto pair, not default (live: never alone
    // leaves untrusted projects in readOnly).
    if cfg.ApprovalPolicy == "never" && cfg.SandboxMode == "" {
        m, _ := findCodexMode(cfg, modeAuto)
        return m.approvalPolicy, m.sandbox, m.mode.ID
    }
    m, ok := findCodexMode(cfg, modeDefault)
    if !ok {
        return "on-request", "workspace-write", modeDefault
    }
    return m.approvalPolicy, m.sandbox, m.mode.ID
}
```

### 1.3 Wire seed into `newSession`

```go
approval, sandbox, _ := seedPolicy(cfg)
s := &session{
    // …
    approvalPolicy: approval,
    sandboxMode:    sandbox,
    autoApprove:    approval == "never",
}
```

Keep `s.cfg = cfg`. Do not assign live fields from raw cfg directly.

`emitModes` / `SetMode` already use live policy + table; after seed,
empty config reports `current_mode_id: "default"`.

### 1.4 Auto invariant helper (optional but recommended)

```go
// after SetMode and in seedPolicy for auto:
// if approval == "never" && sandbox == "" { sandbox = "workspace-write" }
```

Or assert in `SetMode` that table-driven assignment never leaves
autoApprove true with empty sandbox for advertised auto modes.

### 1.5 Tests

| test | expectation |
|---|---|
| `TestFullAccessModeIsGated/hidden_by_default` | `default,read-only,auto` |
| `…/advertised_when_allowed` | `default,read-only,auto,full-access` |
| `TestDefaultModeIsNotDangerous` | default exists; on-request + workspace-write; not dangerous |
| `TestDefaultDistinctFromAuto` | auto still never + workspace-write |
| `TestSeedPolicyEmpty` | → default pair + id `default` |
| `TestSeedPolicyNeverAlone` | ApprovalPolicy never, Sandbox empty → **auto** pair |
| `TestSeedPolicyMatchingAuto` | never+workspace-write → auto |
| `TestSeedPolicyGatedFullAccess` | never+danger-full-access, gate off → default |
| `TestSeedPolicyPartialSandboxOnly` | only sandbox set → default |
| `TestNewSessionEmptyEmitsDefault` | `emitModes` CurrentModeID == `default` |
| `TestThreadStartEmptySendsDefaultPolicy` | capture create: on-request + workspace-write strings |
| `TestTurnStartAfterAutoCarriesWorkspaceWrite` | SetMode(auto) → beginTurn pipe: never + sandboxPolicy.workspaceWrite |
| `TestSetModeDefaultRoundTrip` | auto → default restores on-request + workspace-write, disarms auto |
| Existing auto/sweep/read-only | update hard-coded mode lists only |

**Turn wire capture sketch** (drive production code, never a hand-built map):

```go
// io.Pipe conn; p.eng = &engine{conn: c, dead: make(chan struct{})}
// s := newSession(p, Config{}, …); s.agentID = "thread-1"
// s.SetMode(ctx, modeAuto)
// go s.beginTurn(…)
// decode turn/start from engineR; assert approvalPolicy + sandboxPolicy.type
```

Run:

```bash
go test ./internal/provider/codex/ -count=1
go test -race ./internal/provider/codex/ -count=1
# optional: go test -tags live_codex ./internal/provider/codex/ -count=1
```

**Exit criteria**: empty create → chip current `default` + wire pair;
`SetMode(auto)` → next turn has both never and workspaceWrite; never-alone
config → auto pair not never+empty.

---

## Phase 2 — Mobile: honest mode selection

### 2.1 Resolver (`chat_helpers.dart` or sibling)

```dart
/// Mirrors daemon session.defaultMode (MADR 0047 D4).
SessionMode? resolveDisplayedMode(
  List<SessionMode> modes,
  String? currentModeId,
) {
  if (modes.isEmpty) return null;
  final id = currentModeId?.trim() ?? '';
  if (id.isNotEmpty) {
    for (final m in modes) {
      if (m.id == id) return m;
    }
  }
  for (final want in const ['default', 'build']) {
    for (final m in modes) {
      if (m.id.toLowerCase() == want && !m.dangerous) return m;
    }
  }
  for (final m in modes) {
    if (m.id.toLowerCase() != 'plan' && !m.dangerous) return m;
  }
  return modes.first;
}
```

### 2.2 `_ModeSelector`

Replace:

```dart
final current = modes.firstWhere(
  (m) => m.id == currentModeId,
  orElse: () => modes.first,
);
```

with `resolveDisplayedMode`. Checked item: `m.id == displayed.id` where
displayed is the resolver result (never prefers a dangerous mode when a
safe one exists).

Do **not** call `setMode` from build.

### 2.3 Widget / unit tests

| case | expectation |
|---|---|
| current=`default`, list default/read-only/auto | chip default |
| current=`''`, same list | chip **default**, not read-only |
| current=`''`, OpenCode-shaped build/plan/auto | chip **build** |
| current=`''`, goose-shaped auto first (not dangerous) | chip **auto** |
| current=`auto` (dangerous) | stays auto + dangerous styling |
| current=unknown, list has default | falls to default |

```bash
cd apps/mobile && dart format lib test
flutter test test/mode_selector_dangerous_test.dart  # + new cases
flutter analyze
```

**Exit criteria**: empty current never checks a non-default first entry
when `default`/`build`/non-dangerous alternatives exist.

---

## Phase 3 — Cross-provider create contract

### 3.1 `defaultmode_test.go`

```go
{
    name: "codex",
    modes: []event.SessionMode{
        {ID: "default"}, {ID: "read-only"},
        {ID: "auto", Dangerous: true},
    },
    want: "default",
    ok:   true,
},
```

Comment on `defaultMode` that codex uses `"default"`.

### 3.2 Provider pins (package-local, already mostly present)

| package | pin |
|---|---|
| codex | phase 1 |
| opencode | empty agent → build |
| acpagent/grok | DefaultModeID default |
| goose | DefaultModeID auto |

**Exit criteria**: `go test ./internal/session/ ./internal/provider/...`
green; codex empty create is non-dangerous `default`.

---

## Phase 4 — Docs and verification sweep

### 4.1 Doc updates

| file | change |
|---|---|
| `docs/0047-MADR-codex-default-mode.md` | Status → Implemented when done |
| `docs/0044-MADR-auto-approve-modes.md` | D5 note: mode **list** extended by 0047; auto/full-access semantics unchanged; never-alone is a bug fixed in 0047 |
| `docs/protocol-v1.md` | if codex modes listed, add `default` |
| `docs/config.md` / example yaml | empty codex policy = mcremote `default` mode for remote sessions; `approval_policy: never` alone is treated as auto pair |

### 4.2 Pre-commit / gates

```bash
make pre-add-check FILES="internal/provider/codex/mode.go internal/provider/codex/session.go …"
# or ./scripts/go-precheck.sh <touched .go>
cd apps/mobile && dart format <touched> && flutter analyze
go test ./internal/...
go test -race ./internal/provider/codex/ ./internal/session/
cd apps/mobile && flutter test
make preflight   # if available
```

### 4.3 Manual Android smoke

1. New codex session → chip **default**; menu default / read-only / auto.
2. Prompt needing a workspace edit → normal permission prompts (not auto).
3. Switch **auto** → confirm → subsequent permissions auto-approved; edits
   confined to workspace (sandbox still on).
4. Switch **default** → prompts return.
5. Switch **read-only** → writes/shell restricted as expected.
6. New OpenCode → **build**; new grok → **default**.

---

## 5. File checklist

| path | action |
|---|---|
| `internal/provider/codex/mode.go` | `modeDefault`, table row, `seedPolicy` |
| `internal/provider/codex/session.go` | `newSession` uses seed |
| `internal/provider/codex/mode_test.go` | list + seed + emit tests |
| `internal/provider/codex/fixtures_test.go` | empty create wire; optional turn capture |
| `internal/session/defaultmode_test.go` | codex case |
| `apps/mobile/lib/features/chat/chat_helpers.dart` | `resolveDisplayedMode` |
| `apps/mobile/lib/features/chat/chat_screen.dart` | `_ModeSelector` |
| `apps/mobile/test/mode_selector_*.dart` | empty/unmatched cases |
| `docs/0044-MADR-auto-approve-modes.md` | D5 note |
| config / protocol docs | vocabulary + empty meaning |

---

## 6. Risks

| risk | mitigation |
|---|---|
| Operators relied on empty = pure engine config.toml | Document; set pair explicitly if needed |
| Config `never` alone now becomes full auto (workspace-write) | Intentional repair; safer than never+readOnly; log Warn |
| Mobile resolver ≠ daemon `defaultMode` | Same preference list; dual tests |
| Old daemon + new app | Resolver falls back to first non-dangerous |
| Test suite still expects `read-only,auto` | Update in same commit as table |

---

## 7. Non-goals

- Create-session mode dropdown
- Phone create-time auto opt-in
- Changing goose default
- Codex plan mode / granular approval / experimentalApi settings update
- Persisting last-used mode across sessions

---

## 8. Implementation notes (fill when done)

| phase | status | notes |
|---|---|---|
| 1 — codex table + seed + auto wire | done | `seedPolicy`, default first, never-alone → auto, turn wire test |
| 2 — mobile honesty | done | `resolveDisplayedMode` + selector + tests |
| 3 — cross-provider pins | done | codex case in `defaultmode_test.go` |
| 4 — docs + verification | done | 0044 D5 note; gates run in implement session |
