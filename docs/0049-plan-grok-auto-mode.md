# MADR 0049 — Implementation plan: grok auto mode

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0049](./0049-MADR-grok-auto-mode.md). Read that first: it
carries the problem statement, why neither existing mechanism transfers
unchanged, and the decisions D1–D6. This document is the build order, keyed to
source as of `5831bd9` (2026-07-29).

- **Status**: **Complete** (2026-07-29). Phases A–D landed one commit each.
  Deviation from the plan as written: Phase C's fake agent is a pipe-backed
  ACP responder built in `automode_test.go` (no such harness existed in
  `acpagent`), and the arming path gained an `emitModesOrStatic` call for the
  case where there is no normal mode to switch to — see §D notes below.
- **Date**: 2026-07-29
- **Scope**: `internal/provider/acpagent` (synthetic auto + per-session
  interception), `internal/provider/grok` (opt in), `docs/`. **No** protocol
  change, **no** mobile change, **no** change to goose/opencode/codex.
- **Standards**: `/home/mac/standards/go` — `concurrency.md`, `testing.md`
  (read before touching `session.go`: the mode/permission state is mutex-guarded
  and the tests are table-driven)

---

## 0. Goal, non-goals, ground rules

### Goal

A grok session can be switched to `auto` from the phone's mode menu, gated by
the existing dangerous-mode confirmation, and while armed the daemon answers
every ACP permission request itself so the user is not prompted. Selecting any
other mode disarms it.

### Non-goals

- Changing `providers.grok.permission_mode` semantics (MADR 0049 D6). It stays
  a process-wide launch flag.
- Giving goose, opencode or codex a second auto mode. Goose has a native one;
  opencode and codex already synthesize/implement theirs.
- Making grok's *internal* policy (`acceptEdits`, `dontAsk`,
  `bypassPermissions`) session-switchable. Out of reach: it is CLI-side logic
  fixed at launch (MADR 0038 §5).
- Anything in MADR 0048 (codex sandbox execution). Independent.

### Ground rules

1. **One phase → one commit.** Do not push unless asked.
2. **Go gate before `git add`**: `make pre-add-check FILES="..."` for every
   touched `.go` file (gofmt, golint, govulncheck).
3. **Commit without `-m`** — the prepare-commit-msg hook writes the message.
4. **Line numbers drift.** Prefer the symbol names and quoted comments below.
5. **No phase is done at compile.** Run its named tests, then
   `go test ./internal/provider/...`, then `make preflight`.
6. Live-gated grok tests (`live_grok` build tag) are **not** required to pass
   in CI, but Phase D adds one so the synthetic mode is pinned against a real
   agent the day someone has one.

### File map

| Area | Path |
|---|---|
| ACP-stdio spec | `internal/provider/acpagent/acpagent.go` |
| ACP-stdio session (modes, permissions) | `internal/provider/acpagent/session.go` |
| Grok provider | `internal/provider/grok/grok.go` |
| Grok tests | `internal/provider/grok/grok_test.go` |
| ACP-stdio tests | `internal/provider/acpagent/*_test.go` |
| Config docs | `docs/config.md` |

### Verified baseline facts (do not rediscover)

| Claim | Evidence |
|---|---|
| Grok advertises only `default` + `plan` | `grok/grok.go:39-42` `staticModes` |
| Grok returns no modes from `session/new`, so the static list is what ships | `acpagent/session.go:1185-1188` comment on `emitModesOrStatic` |
| `syntheticModes` is set exactly when the static list was used | `acpagent/session.go:1203-1205` |
| `SetMode` validates against `staticModes` when synthetic, then forwards **every** id to the agent | `acpagent/session.go:486-504` |
| The interception point already exists, keyed only on process-wide config | `acpagent/session.go:1270-1273` — `if s.cfg.AlwaysApprove { return autoAllow(params), nil }` |
| `autoAllow` prefers an allow/approve-kinded option, else the first | `acpagent/session.go:1430-1450` |
| Grok's CLI auto is a launch flag with six values | `grok/grok.go:103-105`; MADR 0039 §5 / MADR 0038 §5 |
| Goose has a native `auto` and it is its default — must not be touched | `goose/goose.go:23-27,48` (and goose is `acphttp`, not `acpagent`) |
| OpenCode's precedent: synthesize + intercept | `opencode/mode.go:127-130` `withAutoMode` |
| Mobile needs no change: it renders whatever is advertised and gates on `dangerous` | MADR 0044 D1; `chat_helpers.dart:16-36` `resolveDisplayedMode` |

---

## 1. Phase order

```text
A  acpagent: synthetic auto mode + per-session arm/disarm + interception
B  grok: opt in, order the list, docs
C  regression tests (arming, disarming, interception, non-regression for others)
D  live-gated grok test + verification
```

A → B → C → D, sequential. Phase A is inert until B opts a provider in, so A
can land on its own without behaviour change.

---

## Phase A — acpagent: synthetic auto, armed per session

**Files:** `internal/provider/acpagent/acpagent.go`,
`internal/provider/acpagent/session.go`

### A.1 Spec opt-in

In `acpagent.go`, beside `StaticModes`/`DefaultModeID`:

```go
// SynthesizeAutoMode appends a daemon-enforced `auto` mode to the advertised
// list for agents whose own auto-approve is not settable per session (grok:
// --permission-mode is a process launch flag). The id is never forwarded to
// the agent — SetMode intercepts it — so it must not collide with an id the
// agent uses. Off by default: an agent with a native auto (goose) must not
// get a second one (MADR 0049 D2).
SynthesizeAutoMode bool
```

Thread it through the session constructor at `acpagent.go:309-310` alongside
`staticModes`/`defaultModeID`.

### A.2 Session state

In `session.go`, beside `syntheticModes` (~`:92-95`):

```go
// autoApprove is armed by the synthetic `auto` mode and answered in
// RequestPermission. Per session, unlike cfg.AlwaysApprove which is
// process-wide (MADR 0049 D1).
autoApprove bool
// synthesizeAuto mirrors Spec.SynthesizeAutoMode; read-only after construction.
synthesizeAuto bool
```

Add the mode id constant and builder near the top of the mode section:

```go
const autoModeID = "auto"

func syntheticAutoMode() event.SessionMode {
    return event.SessionMode{
        ID:          autoModeID,
        Name:        autoModeID,
        Description: "Auto-approve — no prompts",
        Dangerous:   true,
    }
}
```

`Dangerous: true` is what makes the phone gate it behind the arming dialog
(D3). Do not omit it.

### A.3 Advertise it

In `emitModesOrStatic`, append the synthetic entry to **both** branches — the
agent-supplied list and the static fallback — because an agent that starts
advertising modes must not silently lose auto:

```go
func (s *session) advertisedModes(base []event.SessionMode) []event.SessionMode {
    if !s.synthesizeAuto {
        return base
    }
    // Appended last: the normal mode stays the menu head, so an older client
    // that still falls back to the first entry lands somewhere safe
    // (MADR 0047 D1, MADR 0049 D3).
    if slices.ContainsFunc(base, func(m event.SessionMode) bool {
        return strings.EqualFold(m.ID, autoModeID)
    }) {
        return base // the agent has its own auto; do not shadow it
    }
    return append(slices.Clone(base), syntheticAutoMode())
}
```

Use it for the `Modes:` field in both emit paths. **Clone before appending** —
`s.staticModes` is shared across sessions and `append` on a shared slice with
spare capacity would write into another session's backing array.

Current-mode reporting (D4): when `s.autoApprove` is set, report `autoModeID`;
otherwise report what the branch already computes.

### A.4 Intercept in `SetMode`

Rewrite the head of `SetMode` (`:486`) so `auto` never reaches the agent:

```go
func (s *session) SetMode(ctx context.Context, modeID string) error {
    s.mu.Lock()
    agentID, closed, synthetic := s.agentID, s.closed, s.syntheticModes
    canAuto := s.synthesizeAuto
    s.mu.Unlock()
    if closed {
        return fmt.Errorf("session closed")
    }

    if canAuto && strings.EqualFold(modeID, autoModeID) {
        // Arming auto also leaves plan mode: auto-approving a mode that
        // refuses edits would be prompts-off and work-off at once
        // (MADR 0049 D1).
        target := s.normalModeID()
        s.mu.Lock()
        s.autoApprove = true
        s.mu.Unlock()
        if target != "" {
            if _, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
                SessionId: acp.SessionId(agentID),
                ModeId:    acp.SessionModeId(target),
            }); err != nil {
                // Disarm rather than report auto while the agent stayed in plan.
                s.mu.Lock()
                s.autoApprove = false
                s.mu.Unlock()
                return err
            }
        }
        s.emitCurrentMode()
        return nil
    }

    if synthetic && !slices.ContainsFunc(s.staticModes, func(m event.SessionMode) bool {
        return m.ID == modeID
    }) {
        return fmt.Errorf("unknown mode %q", modeID)
    }
    _, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
        SessionId: acp.SessionId(agentID),
        ModeId:    acp.SessionModeId(modeID),
    })
    if err == nil {
        // Any real mode disarms auto.
        s.mu.Lock()
        s.autoApprove = false
        s.mu.Unlock()
    }
    return err
}
```

`normalModeID()` mirrors opencode's `normalAgentID`
(`opencode/mode.go:132-147`): prefer `default`, then `build`, then the first
mode that is neither `plan` nor `auto`, else `""`.

Note the disarm is **after** a successful agent call: a failed switch must not
leave the session claiming a mode the agent is not in.

### A.5 Enforce it

At `session.go:1270`:

```go
func (s *session) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
    s.mu.Lock()
    auto := s.autoApprove
    s.mu.Unlock()
    if s.cfg.AlwaysApprove || auto {
        return autoAllow(params), nil
    }
    ...
```

Leave `cfg.AlwaysApprove` first so operator config keeps working exactly as
before.

### Acceptance

- `go build ./...`; no behaviour change for any provider (nothing sets the flag
  yet). `go test ./internal/provider/...` green.

---

## Phase B — grok: opt in

**Files:** `internal/provider/grok/grok.go`, `docs/config.md`

1. Set `SynthesizeAutoMode: true` in `spec` beside `DefaultModeID: "default"`.
2. Leave `staticModes` as-is — `default` first, `plan` second; `auto` is
   appended by the daemon, so the menu reads **default, plan, auto**.
3. Extend the `staticModes` doc comment to say that auto is synthetic and
   daemon-enforced, and that it is *not* grok's `--permission-mode auto`.
4. `docs/config.md`: under `providers.grok.permission_mode`, add that it is a
   process-wide launch flag and that the per-session `auto` mode is separate
   and daemon-enforced; if the process was launched with `bypassPermissions`,
   grok will not ask and the session mode is advisory (MADR 0049 D6).

### Acceptance

A grok session advertises three modes with `auto` flagged dangerous, and
`CurrentModeID` is `default` on create.

---

## Phase C — regression tests

**Files:** `internal/provider/acpagent/session_test.go` (or a new
`automode_test.go`), `internal/provider/grok/grok_test.go`

Fakes only — no live agent. Drive a session over the existing in-memory `conn`
harness the acpagent tests already use.

1. **Advertised** — a spec with `SynthesizeAutoMode: true` and no agent modes
   emits `[default, plan, auto]`, `auto` last, `Dangerous: true`,
   `CurrentModeID == "default"`.
2. **Not advertised when off** — the same spec with the flag false emits
   exactly the static list. Guards goose/others.
3. **Never shadows a native auto** — a spec with the flag on whose agent
   advertises its own `auto` emits one `auto`, the agent's.
4. **Arming does not call the agent with `auto`** — record every
   `session/set_mode` the fake receives; after `SetMode(ctx, "auto")` the fake
   must have seen `default`, never `auto`. This is the test that would have
   caught forwarding a bogus id.
5. **Arming intercepts** — after `SetMode(ctx, "auto")`, `RequestPermission`
   returns the allow-kinded option without emitting a `permission_request`
   event. Assert on the returned outcome *and* that no permission event was
   emitted — advertising auto while still prompting is the failure this MADR
   names as the risk.
6. **Disarming** — `SetMode(ctx, "plan")` clears the flag;
   `RequestPermission` then emits a `permission_request` again.
7. **Failed switch does not arm** — a fake whose `SetSessionMode` errors leaves
   `autoApprove` false and returns the error.
8. **Unknown id still rejected** — `SetMode(ctx, "nonsense")` errors, and does
   not arm.
9. **Current mode reporting** — while armed, the emitted `session_mode` carries
   `CurrentModeID == "auto"`; after disarming, the real id.
10. **Grok wiring** (`grok_test.go`) — the grok spec has
    `SynthesizeAutoMode: true`, and a grok session's advertised ids are exactly
    `[default, plan, auto]`.
11. **Shared-slice safety** — build two sessions from one spec, advertise both,
    and assert the first session's mode slice is unchanged (catches the
    `append`-on-shared-backing-array bug A.3 warns about).

### Acceptance

All eleven green; each of 4, 5, 6 and 11 verified to fail if its production
change is reverted.

---

## Phase D — live gate + verification

1. `internal/provider/grok/live_mode_test.go` (build tag `live_grok`) — extend
   the existing live mode test: arm `auto`, assert the agent received
   `session/set_mode default` and never `auto`, then drive a turn that
   triggers a permission and assert no prompt reached the client.
2. `make pre-add-check FILES="..."` on every touched `.go` file.
3. `go build ./... && go test ./internal/...`.
4. `make preflight`.
5. Manual smoke with a real grok: mode menu shows **default, plan, auto**;
   tapping `auto` raises the arming confirmation (dangerous flag); after
   confirming, a tool call that previously prompted runs unattended; switching
   back to `default` restores prompting.
6. Update MADR 0049 Status to Implemented, and record in §2 of that MADR that
   grok reached parity.

Done means: grok has auto, no other provider changed, suites green.

---

## §D notes — what actually landed

Two things differ from the plan as written; both are recorded here rather than
silently absorbed.

1. **The fake agent had to be built.** `internal/provider/acpagent` had no
   pipe-backed conn harness — every existing `SetMode` test avoids the
   transport by using an invalid id or a closed session. `automode_test.go`
   adds `startFakeAgent`, a newline-delimited JSON-RPC responder on an
   `io.Pipe` pair that records every `session/set_mode` it receives and can be
   told to refuse. That is what makes "the synthetic id never reaches the
   agent" and "a refused switch does not arm" assertable at all, rather than
   inferred.
2. **Arming with no normal mode emits its own event.** When `normalModeID()`
   finds nothing to switch to, there is no agent round-trip and therefore no
   `current_mode_update` to carry the new state — the arm would be silent.
   `armAutoMode` calls `emitModesOrStatic(nil)` in that case so the chip still
   updates. With grok's list this branch is unreachable (`default` always
   resolves); it exists so a future opt-in provider cannot arm invisibly.

Verified to fail without their production change: the shared-slice test
(reverting `slices.Clone`), and the interception test (reverting the
`|| auto`). The remaining nine cover advertisement, ordering, the dangerous
flag, no-shadowing, disarming, unknown ids, current-mode reporting, and the
grok spec wiring.

**Not run here:** the `live_grok` assertions, including the extended
`TestLiveGrokPlanModeSwitch` (now expecting three modes) and the new
`TestLiveGrokAutoModeArmsWithoutSendingAuto`. No grok binary on this host —
they compile and vet clean but need a real agent, which is also the only way
to confirm grok does not reject the `default` switch that arming performs.
