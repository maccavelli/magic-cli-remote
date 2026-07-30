# MADR 0048 — Implementation plan: Codex sandbox namespace / auto-write recovery

Companion to [MADR 0048](./0048-MADR-codex-sandbox-namespace.md). Read that first.

- **Status**: Proposed
- **Date**: 2026-07-29
- **Decision**: [MADR 0048](./0048-MADR-codex-sandbox-namespace.md)
- **Targets**: codex-cli app-server (0.145.0+), daemon codex provider, config /
  docs; mobile only if notice rendering already insufficient (it should not need
  a new control)

---

## 0. Summary

| layer | change |
|---|---|
| `internal/provider/codex` | sandbox health probe; provider state; session notices; mid-turn marker detection; optional seed under `sandbox_broken_policy` |
| `internal/config` + examples | `providers.codex.sandbox_broken_policy` |
| tests | unit fakes for probe/notices/policy; `live_codex` probe pin |
| docs | MADR status, `config.md`, README/ops blurb, cross-links from 0028/0044/0047 |

No protocol message required for v1 (TypeNotice is enough). No mobile mode-menu
redesign. No redefinition of auto's wire pair.

### Dependency order

```
Phase 0 (repro pins + fixtures) ──► Phase 1 (probe + provider state)
                                         │
                                         ▼
                                   Phase 2 (session notices + SetMode warn)
                                         │
                    ┌────────────────────┼────────────────────┐
                    ▼                    ▼                    ▼
            Phase 3 (config policy)  Phase 4 (mid-turn)   Phase 5 (docs/ops)
                    └────────────────────┼────────────────────┘
                                         ▼
                                   Phase 6 (verification)
```

Phases 3 and 4 can parallelise after Phase 2. Recommended: **0 → 1 → 2 → 3 → 4 → 5 → 6**.

---

## Phase 0 — Reproduce and pin the failure class

### 0.1 Manual repro (implementer machine)

```bash
# Expect fail on AppArmor-restricted hosts:
sysctl kernel.unprivileged_userns_clone kernel.apparmor_restrict_unprivileged_userns
unshare -Ur true          # THE cheap discriminator; fails when restricted
tmpdir=$(mktemp -d)
codex sandbox -c 'sandbox_mode="workspace-write"' -- bash -c "echo x > $tmpdir/w.txt"
codex sandbox -c 'sandbox_mode="danger-full-access"' -- bash -c "echo x > $tmpdir/w2.txt"
sudo dmesg | grep -i 'apparmor.*DENIED.*unprivileged_userns' | tail -3
```

**Do not substitute `bwrap --unshare-user --ro-bind / / true` here.** It passes
on a broken host — bwrap has its own AppArmor profiles and never transitions
into the restrictive one — so it will tell you the bug is absent when it is
not. See MADR §2.1.1; that mistake is why §2.1 originally mis-recorded this row.

Record outcomes in the PR description (same table as MADR §2.1).

**If you just want the host working** (this is an operator action, deliberately
not something the daemon does — see MADR §2.1.2 for the security trade-off):

```bash
sh scripts/bwrap-apparmor-fix.sh
systemctl --user restart mcremote
```

Note that fixing your own host removes your ability to exercise the very code
this plan adds. Phase 1's probe and Phase 2's notices need a *broken* host, so
toggle the restriction back while developing them:

```bash
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=1   # break
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0   # restore
```

### 0.2 Fixture corpus

Add under `internal/provider/codex/testdata/` (or const strings in `*_test.go`):

| name | content |
|---|---|
| `bwrap_userns_denied.txt` | exact bwrap error line from this host |
| `fs_sandbox_helper_failed.txt` | apply_patch verification prefix + bwrap |
| `app_server_stderr_userns.txt` | app-server ERROR line from MADR 0028 §16.1 |

These feed the marker detector and probe classification tests.

### 0.3 Exit criteria

- Repro documented; fixtures committed; no code behaviour change yet.

---

## Phase 1 — Sandbox health probe and provider state

### 1.1 Types (`internal/provider/codex/sandbox_health.go`)

```go
type sandboxHealthReason string

const (
    sandboxOK            sandboxHealthReason = "ok"
    sandboxUsernsDenied  sandboxHealthReason = "userns_denied"
    sandboxBwrapMissing  sandboxHealthReason = "bwrap_missing"
    sandboxProbeFailed   sandboxHealthReason = "probe_failed"
    sandboxUnknown       sandboxHealthReason = "unknown" // before first probe
)

type sandboxHealth struct {
    OK       bool
    Reason   sandboxHealthReason
    Detail   string
    ProbedAt time.Time
}
```

### 1.2 Probe implementation

```go
// probeSandboxHealth runs a short, isolated check that workspace-write
// sandboxed execution can create a user namespace / write a file.
// Must not touch the live app-server engine or user repos.
func probeSandboxHealth(ctx context.Context, bin string) sandboxHealth
```

Implementation notes:

- Timeout: 5–10 s hard cap.
- Temp dir: `os.MkdirTemp` + always `RemoveAll`.
- Preferred command:

  ```bash
  codex sandbox -c 'sandbox_mode="workspace-write"' -- bash -c 'echo ok > "$1"' _ "$tmpdir/probe.txt"
  ```

  Use `exec.CommandContext` with `bin` from config (not bare PATH search drift).
- Classify stderr/stdout with `classifySandboxError(text string) sandboxHealthReason`.
- If probe write succeeds and file contains `ok` → `OK: true, Reason: ok`.
- If bwrap markers → `userns_denied`.
- Log at Info when ok, Warn when not.

Make `classifySandboxError` pure and unit-tested against Phase 0 fixtures.

### 1.3 Wire into `Provider`

```go
type Provider struct {
    // …
    healthMu sync.RWMutex
    health   sandboxHealth // zero = unknown until probe
}
```

- Call probe at end of successful `startEngine` (after initialize), on a
  background goroutine **or** inline if <1 s typical — prefer **inline once**
  so session create after prewarm already has truth. Cap wait with timeout;
  on timeout store `probe_failed` and continue (engine is still usable for
  chat).
- Methods:

  ```go
  func (p *Provider) sandboxHealth() sandboxHealth
  func (p *Provider) setSandboxHealth(h sandboxHealth)
  func (p *Provider) noteSandboxFailure(detail string) // sticky flip to !ok
  ```

- `Ready()` stays binary-presence (do not make Ready false when sandbox
  broken — sessions can still chat / use full-access). Document that Ready ≠
  "writes work".

### 1.4 Tests

| test | expectation |
|---|---|
| `TestClassifySandboxError` | fixtures → reasons |
| `TestProbeSandboxHealth_FakeBin` | stub script bin that exits 1 with bwrap text → userns_denied |
| `TestProbeSandboxHealth_FakeBinOK` | stub writes probe file → ok |
| `TestProviderHealthAfterStart` | with injectable prober, start path stores health |

### 1.5 Exit criteria

```bash
go test ./internal/provider/codex/ -count=1 -run 'Classify|Probe|Health'
```

---

## Phase 2 — Session notices (create / resume / arm auto)

### 2.1 Notice copy helper

```go
func sandboxBrokenNotice(h sandboxHealth) string
```

Single template (MADR D2). Include `h.Detail` truncated (reuse `truncateRunes`,
cap ~300). Mention `allow_full_access` + full-access mode + ops doc pointer
(`docs/0048-…` or `docs/config.md#codex-sandbox`).

### 2.2 Emit on create/resume

In `startNew` / `resume` after `emitModes` (or immediately after):

```go
if h := s.p.sandboxHealth(); !h.OK && h.Reason != sandboxUnknown {
    s.emitSandboxBrokenNotice(h) // TypeNotice, once (flag on session)
}
```

Session field: `sandboxNoticeSent bool` under `s.mu`.

### 2.3 SetMode(auto) while broken

After successful SetMode to `modeAuto` (and optionally `modeDefault` /
`modeReadOnly` if we want broader honesty — **minimum is auto**):

```go
if mode is sandboxed && !health.OK {
    emit notice again only if !sandboxNoticeSent; set flag
}
```

Do **not** block SetMode(auto) under default `warn` policy — user may still
want no prompts for MCP/questions while reading; but they must see the write
limitation.

### 2.4 Tests

| test | expectation |
|---|---|
| `TestNewSessionEmitsSandboxNoticeWhenUnhealthy` | inject health; create path emits TypeNotice containing "user namespace" or "bubblewrap" |
| `TestNewSessionNoNoticeWhenHealthy` | no such notice |
| `TestSetModeAutoNoticeOnce` | two SetMode(auto) → one notice |
| Existing mode tests | still green; no health assumed |

### 2.5 Exit criteria

Notices fire exactly once per session under `warn`; mode wire tests unchanged.

---

## Phase 3 — Config: `sandbox_broken_policy`

### 3.1 Config surface

`internal/config/config.go` (Codex provider struct):

```go
// SandboxBrokenPolicy controls behaviour when the Linux sandbox cannot
// create a user namespace (MADR 0048).
// Valid: "warn" (default), "require_full_access", "refuse".
SandboxBrokenPolicy string `mapstructure:"sandbox_broken_policy"`
```

Validation in existing codex validate switch:

- empty → treat as `warn`
- reject unknown values at load

Defaults in `load.go` / `Default()`: `"warn"` or empty.

Examples:

- `configs/config.example.yaml`
- `configs/config.prod.example.yaml`
- `configs/config.mesh-grok.yaml` (comment only if disabled)

Daemon wiring: `daemon.go` passes field into `codex.Config`.

### 3.2 Provider behaviour matrix

| health | policy | allow_full_access | create behaviour |
|---|---|---|---|
| ok | * | * | unchanged (0047 seed) |
| !ok | `warn` | * | seed per 0047; notice (Phase 2) |
| !ok | `require_full_access` | true | seed **full-access** pair; prefer advertising full-access; still list others? **Decision: advertise full-access + notice that sandboxed modes will not write; seed current = full-access** |
| !ok | `require_full_access` | false | **fail** `Start`/`create` with error mentioning both knobs |
| !ok | `refuse` | * | **fail** create with userns error |

Implement seed override in `newSession` / `seedPolicy` path:

```go
func seedPolicy(cfg Config, health sandboxHealth) (approval, sandbox, modeID string)
```

Signature change: pass health (or call from `newSession` after seed and
re-write). Prefer:

```go
approval, sandbox, id := seedPolicy(cfg)
approval, sandbox, id = applySandboxBrokenPolicy(cfg, health, approval, sandbox, id)
```

Keep pure functions for tests.

### 3.3 Tests

| test | expectation |
|---|---|
| `TestApplySandboxBrokenPolicy_Warn` | policy unchanged |
| `TestApplySandboxBrokenPolicy_Require_GatedOn` | full-access pair |
| `TestApplySandboxBrokenPolicy_Require_GatedOff` | error / sentinel |
| `TestApplySandboxBrokenPolicy_Refuse` | error |
| Config validate unknown value | load error |

### 3.4 Exit criteria

Config load + seed matrix unit-tested; example YAML documents the three values.

---

## Phase 4 — Mid-turn bwrap promotion

### 4.1 Marker scan

In command/file item completion paths (and/or a single helper called from
item completed + tool output text):

```go
func looksLikeSandboxNamespaceFailure(s string) bool
```

Markers (substring, case-sensitive as emitted by bwrap/codex):

- `No permissions to create a new namespace`
- `fs sandbox helper failed`
- `needs access to create user namespaces`

### 4.2 Sticky update

```go
if looksLikeSandboxNamespaceFailure(text) {
    s.p.noteSandboxFailure(text)
    s.emitSandboxBrokenNotice(s.p.sandboxHealth())
}
```

### 4.3 Tests

| test | expectation |
|---|---|
| `TestLooksLikeSandboxNamespaceFailure` | fixtures true; normal stderr false |
| `TestItemCompletedPromotesHealth` | synthetic item completed with bwrap text flips provider health + notice |

### 4.4 Exit criteria

One failed apply_patch style completion in a unit fixture promotes health
without double-notice spam.

---

## Phase 5 — Docs and ops

### 5.1 Doc updates

| file | change |
|---|---|
| `docs/0048-MADR-codex-sandbox-namespace.md` | Status → Accepted/Implemented when done |
| `docs/0048-PLAN-codex-sandbox-namespace.md` | phase checkmarks + notes |
| `docs/config.md` | new keys + "Codex sandbox / user namespaces" section |
| `docs/0028-MADR-codex-provider.md` | link 0048 from userns spike note |
| `docs/0044-MADR-auto-approve-modes.md` | short note under D5: auto requires working workspace-write sandbox; see 0048 |
| `docs/0047-MADR-codex-default-mode.md` | note: wire fix ≠ execution; 0048 |
| `configs/*.yaml` | comments for `sandbox_broken_policy` |
| `README.md` (codex section if present) | one paragraph + link |

### 5.2 Ops remediation checklist (paste into config.md)

1. Confirm: `codex sandbox -c 'sandbox_mode="workspace-write"' -- true`
2. If bwrap userns denied:
   - Check `kernel.apparmor_restrict_unprivileged_userns`
   - Distro docs for bubblewrap / unprivileged userns
   - Container: `--userns=keep-id` / privileged userns flags as appropriate
3. Interim on trusted personal hosts only:
   ```yaml
   providers:
     codex:
       allow_full_access: true
       sandbox_broken_policy: require_full_access
   ```
4. Prefer host fix over permanent full-access on shared machines.

### 5.3 Exit criteria

Docs cross-linked; no stale claim that empty config "only inherits config.toml"
without mentioning 0047 seed **and** 0048 health.

---

## Phase 6 — Verification sweep

### 6.1 Automated

```bash
# After Go edits — before any git add of .go files:
./scripts/go-precheck.sh \
  internal/provider/codex/sandbox_health.go \
  internal/provider/codex/mode.go \
  internal/provider/codex/session.go \
  internal/provider/codex/provider.go \
  internal/provider/codex/config.go \
  internal/config/config.go \
  # …all touched go files

go test ./internal/provider/codex/ ./internal/config/ ./internal/daemon/ -count=1
go test -race ./internal/provider/codex/ -count=1
go test -tags live_codex ./internal/provider/codex/ -count=1 -run 'Sandbox|Health|Mode' 
# live: on broken host expect probe !ok; on healthy expect ok + write
```

### 6.2 Manual Android / phone smoke (broken host)

1. Start daemon with default codex config (warn).
2. New codex session → notice about user namespace / sandbox.
3. Mode chip `default` or `auto` → ask agent to write a file → fails; notice
   already present; tool card shows failure.
4. Set `allow_full_access: true`, restart (or hot-reload if supported),
   `sandbox_broken_policy: require_full_access`.
5. New session → current mode `full-access` → write succeeds.
6. Gate off + require_full_access → create fails with clear error on phone.

### 6.3 Manual (healthy host, if available)

1. No broken-sandbox notice.
2. Auto mode can write inside workspace.
3. full-access still gated.

---

## 7. File checklist

| path | action |
|---|---|
| `internal/provider/codex/sandbox_health.go` | **new** — types, classify, probe |
| `internal/provider/codex/sandbox_health_test.go` | **new** |
| `internal/provider/codex/provider.go` | store health; probe after start |
| `internal/provider/codex/session.go` | notices; mid-turn scan; policy apply |
| `internal/provider/codex/mode.go` | `applySandboxBrokenPolicy` (or keep in sandbox_health.go) |
| `internal/provider/codex/config.go` | `SandboxBrokenPolicy` field |
| `internal/provider/codex/items.go` or session item complete | call marker scan |
| `internal/config/config.go` + `load.go` + validate | field + default |
| `internal/daemon/daemon.go` | pass field |
| `configs/config*.yaml` | comments / example |
| `docs/config.md` | ops + key |
| `docs/0048-*` | this pair |
| `docs/0028`, `0044`, `0047` | cross-links |

---

## 8. Risks

| risk | mitigation |
|---|---|
| Probe slow / flaky | timeout; classify timeout as probe_failed + warn; don't block forever |
| `codex sandbox` CLI shape changes | live test; fallback raw bwrap; fixture stderr still classifies mid-turn |
| Operators force full-access everywhere | dangerous flag + confirm; docs |
| Double notices | session flag; sticky health without re-notify |
| Prewarm races session create before probe done | probe before publishing engine ready **or** create waits on health with short timeout |
| CI without codex binary | unit tests use stub bins; live tagged |

---

## 9. Non-goals

- Mobile redesign of mode chip beyond existing dangerous confirm.
- Auto = full-access without config.
- Process-wide `--dangerously-bypass-approvals-and-sandbox` on app-server.
- Fixing AppArmor inside the repo.
- OpenCode/goose parity for userns (different sandboxes).

---

## 10. Implementation notes (fill when done)

| phase | status | notes |
|---|---|---|
| 0 — repro + fixtures | pending | |
| 1 — probe + provider state | pending | |
| 2 — session notices | pending | |
| 3 — sandbox_broken_policy | pending | |
| 4 — mid-turn promotion | pending | |
| 5 — docs/ops | pending | |
| 6 — verification | pending | |

---

## 11. Suggested commit series

1. `test(codex): fixtures for bwrap userns sandbox failures`
2. `feat(codex): probe sandbox user-namespace health at engine start`
3. `feat(codex): notice when sandboxed modes cannot write`
4. `feat(codex): sandbox_broken_policy config escape hatch`
5. `feat(codex): promote mid-turn bwrap failures to session diagnosis`
6. `docs: MADR 0048 sandbox namespace recovery`

Each commit: Go precheck before `git add` of `.go` files; `git commit` without
`-m` (prepare-commit-msg hook).
