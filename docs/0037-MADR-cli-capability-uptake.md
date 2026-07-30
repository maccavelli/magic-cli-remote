# MADR 0037: CLI capability uptake — reasoning effort, plugin isolation, and origin alignment

- **Status**: Accepted
- **Date**: 2026-07-27
- **Deciders**: Project Owner
- **Related**: [MADR 0028](./0028-MADR-codex-provider.md) (codex provider — its
  Milestone 3 owns the multi-agent tree deferred here),
  [MADR 0019](./0019-MADR-opencode-process-management-plan.md) (shared `opencode
  serve` engine), [MADR 0025](./0025-MADR-goose-provider.md) (goose ACP-over-HTTP),
  [MADR 0023](./0023-MADR-canonical-slash-commands.md)
- **Evidence**: flags probed against the installed binaries on 2026-07-27 —
  grok 0.2.112 (9bbd559437), codex-cli 0.145.0, opencode 1.18.7, goose 1.44.0
- **Companion plan**: [0037-PLAN-cli-capability-uptake.md](./0037-PLAN-cli-capability-uptake.md)

---

## 1. Problem

An upstream-release review proposed eleven changes across the four agent CLIs.
Each was checked twice: does the flag exist **on the invocation path mcremote
actually uses**, and is the change already implemented? Four are real gaps, four
are already done, two are not implementable as described, and one is a known
deferral.

That hit rate is the finding. A flag existing in `--help` says nothing about
whether it reaches the daemon's launch path — three of the eleven fail exactly
there.

### 1.1 Verification matrix

| # | Proposed | Flag exists? | Codebase today | Verdict |
|---|---|---|---|---|
| 1 | Grok reasoning effort | **Yes**, on `grok agent` | No typed field; raw `args` override only, and it is lost on model override | **Gap — take it** |
| 2 | Grok worktree session mode | **TUI only** — absent from `grok agent` and `grok agent stdio` | — | **Not implementable as described** |
| 3 | Codex multi-agent UI parity | n/a | Flattened to tool cards | **Known deferral** (MADR 0028 Milestone 3) |
| 4 | Codex process supervision | n/a | `SetProcessGroup` + `SetDeathSignal` + `TerminateProcessGroup` all present | **Already done** |
| 5 | Codex remote `ws://` transport | **Yes** (`--listen ws://IP:PORT`, `--ws-auth <MODE>`) | Hardcoded `stdio://`; no config | **Gap — defer, own MADR** |
| 6 | OpenCode `--pure` | **Yes** | Not implemented anywhere | **Gap — take it** |
| 7 | OpenCode env passthrough | n/a | Already inherited on both paths | **Already done** |
| 8 | Goose `--with-builtin` | **Yes** | Fully implemented, config-wired, validated, tested | **Already done** |
| 9 | Goose `--allowed-origin` | **Yes** | No `Origin` sent; goose accepts today | **Gap — cheap insurance** |

Plus one defect the review did not raise, found while checking #4 (§1.6).

### 1.2 Grok reasoning effort — real, and subtler than "add a flag"

`grok agent --reasoning-effort <EFFORT>` (alias `--effort`) exists on the
invocation path mcremote uses: `grok agent --no-leader [-m MODEL] stdio`
(`internal/provider/grok/grok.go:71-81`).

An operator can already set it — `providers.grok.args` is a raw `[]string`
override (`config.go:356`) consulted when non-empty
(`acpagent/acpagent.go:109-110`). But that route is a trap:

```go
// grok.go:44-51 — per-session model override
ModelArgs: func(cfg Config, model string) []string {
    return defaultArgs(Config{AlwaysApprove: cfg.AlwaysApprove, Model: model})
},
```

`ModelArgs` rebuilds from `defaultArgs`, **discarding `cfg.Args` entirely** —
the comment says so: *"custom Args are intentionally not preserved here"*. So an
operator who adds `--reasoning-effort high` via `args` silently loses it the
first time a session sets a per-session model. The two features do not compose.

A typed field fixes both halves: it survives the `ModelArgs` rebuild, and it
does not require the operator to hand-reproduce `agent --no-leader … stdio`
correctly.

### 1.3 Grok worktree — the flag is on the wrong command

`--worktree [<WORKTREE>]` and `--worktree-ref` are real, and are **TUI-only**.
Probed directly:

```
grok agent --help        | grep -c 'worktree\|--rules'  → 0
grok agent stdio --help  | grep -c 'worktree\|--rules'  → 0
```

mcremote never runs the TUI. The proposal — "expose worktree creation as a
session launch parameter in `grok.go`" — cannot be implemented by passing a
flag, because no flag on that path accepts it. (`--rules` is in the same
position, though the review did not propose it.)

There *is* a viable route: the daemon creates the worktree itself
(`git worktree add`) and passes its path as `StartOptions.CWD`, which already
exists (`provider.go:44`) and is already honoured by every provider. That is a
genuine feature with real lifecycle questions — who removes the worktree, what
happens to uncommitted changes on session close, how orphans are reaped across
daemon restarts — and it is provider-independent, so it does not belong in
`grok.go` at all. Out of scope here; recorded so the flag route is not
re-attempted.

### 1.4 OpenCode `--pure` — a real and cheap security win

`opencode serve --pure` — *"run without external plugins"* — exists on 1.18.7.
mcremote launches `serve --hostname 127.0.0.1 --port N`
(`internal/provider/opencode/http.go:287`) and has no `pure` anywhere in the
tree.

This matters more for a daemon than for interactive use: the daemon boots a
shared engine at start (`prewarm` defaults on) that outlives any one session and
runs whatever third-party plugins the host user happens to have installed, with
the daemon's privileges. An operator running mcremote on a shared or
lightly-trusted host has no way to opt out today.

### 1.5 Goose origin alignment — insurance, not a live bug

`goose serve --allowed-origin <ORIGIN>` exists: *"Allow an exact Origin value
for ACP CORS; may be specified multiple times and replaces the default loopback
origins."*

mcremote's ACP WebSocket dial sends one header and no `Origin`
(`acphttp/conn.go:147-151`). Goose accepts that today — the provider ships and
its live tests pass — so **this is not a current failure**. It is a latent
coupling: mcremote depends on goose's default-loopback-origin policy tolerating
a missing `Origin`, and nothing pins that. A tightening upstream would break
every goose session with a handshake rejection.

Note the daemon already passes `--dangerously-unauthenticated`
(`goose.go:35`), bound to `127.0.0.1`. That is a deliberate loopback-only
posture, and origin handling should stay consistent with it rather than drift.

### 1.6 A stale comment claims a backstop that does not exist

Found while verifying #4. `internal/provider/httpagent/provider.go:348`:

```go
// (see SetDeathSignal); ReapOrphans at the next startup is the backstop.
```

`ReapOrphans` **does not exist** — no declaration anywhere in the module. The
comment asserts a safety net that was never built (or was removed), which is
worse than silence: a reader auditing orphan handling will believe the gap is
covered.

The actual protections are real and adequate for the supervised case
(`SetProcessGroup` + `Pdeathsig=SIGKILL` + `TerminateProcessGroup`'s
SIGTERM-then-SIGKILL), so this is a documentation defect, not a leak. But the
uncovered case the comment gestured at — a daemon killed with SIGKILL, whose
engine's `Pdeathsig` fires but whose grandchildren already escaped their group —
is genuinely uncovered.

### 1.7 Already done, recorded so they are not re-proposed

- **Codex process supervision** (#4). `provider.go:170-171` calls
  `procutil.SetProcessGroup` **and** `procutil.SetDeathSignal`; teardown is
  `procutil.TerminateProcessGroup(...)` at `:295`, with `KillProcessGroup` on the
  failure paths at `:219,243`. `procutil_unix.go` implements exactly the proposed
  behaviour: `Setpgid`, `syscall.Kill(-pid, SIGTERM)`, then SIGKILL after a
  timeout. The proposal also named the wrong file — this is `provider.go`, not
  `conn.go`.
- **Goose builtins** (#8). `WithBuiltins []string` on `goose.Config`
  (`goose.go:17`), expanded into repeated `--with-builtin` args (`:37-39`),
  surfaced as `providers.goose.with_builtins` (`config.go:398`), validated for
  empties/whitespace/duplicates (`config.go:769-780`), and covered by
  `TestGooseWithBuiltinsParseAndValidate`.
- **Environment passthrough** (#7). httpagent sets
  `cmd.Env = append(os.Environ(), …)` (`provider.go:354`); acphttp sets no
  `cmd.Env` at all, which means `exec.Cmd` inherits the parent environment. Both
  paths already forward the daemon's environment.
- **Codex multi-agent** (#3). Not a new finding: MADR 0028 records it as a
  product decision deferred to **Milestone 3**, *"pending a live-proven child
  relationship contract"* (§899-900, §408-411). `items.go` deliberately flattens
  `collabAgentToolCall` and `subAgentActivity` into tool cards (`:22-23,86,194,201`)
  in the meantime. Building a tree UI before the probe would be guessing at the
  parent/child wire contract — the same mistake report 0032 rev 1 made with
  `turn/plan/updated`.

---

## 2. Decision

Take the three gaps that are small, safe and grounded. Defer the two that need
their own design. Record the rest as done or impossible.

### 2.1 D1 — Typed `reasoning_effort` for grok, composing with model override

Add `ReasoningEffort string` to `acpagent.Config` and
`providers.grok.reasoning_effort` to config, appended by `defaultArgs` as
`--reasoning-effort <value>`.

Critically, thread it through `ModelArgs` so the two compose:

```go
ModelArgs: func(cfg Config, model string) []string {
    return defaultArgs(Config{
        AlwaysApprove:   cfg.AlwaysApprove,
        Model:           model,
        ReasoningEffort: cfg.ReasoningEffort,   // survives the rebuild
    })
},
```

That single addition is the whole point of the decision — without it the typed
field reproduces the raw-`args` bug it exists to fix.

**Engine-level, not per-session.** `--reasoning-effort` is a process flag, and
grok spawns one process per session (`acpagent.go:449-485`), so per-session
*would* be architecturally possible via `StartOptions`. It is deliberately not
taken here: per-session means a new `StartOptions` field, a new
`session.create` payload field, a protocol change, and mobile UI — a much larger
surface for a setting an operator sets once. Config-level first; revisit if
there is demand.

**No value validation.** grok documents no enum for `<EFFORT>` and mcremote
should not invent one — an unknown value is grok's to reject, and hardcoding a
list here would break the day grok adds a tier. Reject only the empty string
(which would emit a bare flag).

### 2.2 D2 — `pure` for the opencode engine

Add `Pure bool` to the opencode config, surfaced as
`providers.opencode.pure`, appending `--pure` to the serve args.

**Default `false`.** Changing the engine's plugin behaviour by default would
silently break hosts whose workflow depends on a plugin. Opt-in, documented as
the hardening choice.

### 2.3 D3 — Send an explicit `Origin` on the goose ACP dial

Set `Origin: http://127.0.0.1:<port>` on the WebSocket dial in
`acphttp/conn.go`, matching the loopback host the daemon already connects to.

This is the smaller half of the proposal and the one that needs no
configuration: an explicit loopback `Origin` satisfies goose's default policy
explicitly rather than relying on a missing header being tolerated.

**Not taking the `--allowed-origin` passthrough.** Adding a config knob to widen
goose's CORS policy is the wrong direction for a daemon that deliberately binds
loopback and passes `--dangerously-unauthenticated`. If a future deployment
needs a non-loopback origin, that is a security decision deserving its own
review, not a passthrough.

### 2.4 D4 — Delete the false `ReapOrphans` claim

Remove the stale clause and replace it with what the code actually guarantees:
process-group teardown on the supervised path, `Pdeathsig` when the daemon dies,
and an explicit statement that a SIGKILLed daemon can leave grandchildren that
escaped their process group.

Documenting the residual gap honestly is the decision. Building a reaper is not
taken here: it needs an ownership-token scan (`procutil.FindByEnv`,
`OwnerAlive` already exist for exactly this), a policy for what is safe to kill,
and its own testing. Recorded as a follow-on.

### 2.5 Deferred, with reasons

- **Codex remote `ws://` transport.** Both flags are real
  (`--listen ws://IP:PORT`, `--ws-auth <MODE>`), and `provider.go:169` hardcodes
  `stdio://` with no config field, so the gap is real. But this is not a flag —
  it is a second transport with a network listener, a token-auth scheme, a
  reachability model, and a failure surface with no relation to the stdio path.
  It needs its own MADR, including whether a remote codex engine is even in the
  product's threat model. Not half-designed here.
- **Codex multi-agent tree.** MADR 0028 Milestone 3, gated on a live collab
  probe. Unchanged by this MADR.
- **Daemon-managed git worktrees.** Viable via `StartOptions.CWD` (§1.3), but
  provider-independent and lifecycle-heavy. Own design.

---

## 3. Consequences

### 3.1 Positive

- Reasoning effort becomes settable without the raw-`args` foot-gun, and stops
  silently vanishing on model override.
- Operators on shared hosts can stop the daemon's long-lived engine from loading
  third-party plugins.
- The goose handshake stops depending on an unpinned upstream tolerance.
- An auditor reading the orphan-handling comment is no longer misled.

### 3.2 Costs and risks

- **D1 changes grok's default args** when the new field is set. Empty by
  default, so unset behaviour is byte-identical.
- **D2 is opt-in but affects a shared engine**: toggling `pure` requires an
  engine restart to take effect, since the flag is fixed at spawn. Operators must
  know that; the config doc says so.
- **D3 sends a header the daemon did not send before.** Goose accepts a missing
  `Origin` today, so the change is only meaningful if goose tightens — but a
  wrong explicit value would be *worse* than none. It must match a default
  loopback origin exactly, which the plan pins with a live test.

### 3.3 Not changed

No protocol change. No client change. No new event types. Three providers gain
one config field each; the fourth (codex) gains nothing but a corrected comment.

---

## 4. Rejected

| Rejected | Why |
|---|---|
| Grok worktree via a launch flag | `--worktree` does not exist on `grok agent` or `grok agent stdio` — probed, zero matches. No flag can carry it |
| Per-session reasoning effort | Requires `StartOptions` + protocol + mobile UI for a set-once operator setting (§2.1) |
| Validating reasoning-effort values | grok documents no enum; a hardcoded list breaks when grok adds a tier |
| `providers.goose.allowed_origins` passthrough | Widens CORS on a daemon that binds loopback and runs unauthenticated; a security decision, not a knob (§2.3) |
| Codex process-supervision work | Already implemented, and the proposal named the wrong file (§1.7) |
| Goose `with_builtins` | Already implemented, config-wired, validated and tested (§1.7) |
| Env-passthrough work | Already inherited on both spawn paths (§1.7) |
| Building `ReapOrphans` now | Needs an ownership-scan policy and its own tests; the honest comment ships first (§2.4) |
| Codex multi-agent tree UI | MADR 0028 Milestone 3, gated on a live probe. Guessing the child contract repeats report 0032 rev 1's error |
| `--pure` on by default | Silently breaks hosts whose workflow depends on a plugin |

---

## 5. Verification

Every "exists" below was probed against the installed binary on 2026-07-27, not
read from release notes.

| Claim | How verified |
|---|---|
| grok `--reasoning-effort` on the agent path | `grok agent --help` — present, alias `--effort` |
| grok `--worktree`/`--rules` are TUI-only | `grok agent --help` and `grok agent stdio --help` — zero matches |
| mcremote runs `grok agent --no-leader … stdio` | `grok.go:71-81` |
| `ModelArgs` discards `cfg.Args` | `grok.go:44-51` and its own comment |
| codex `--listen ws://`, `--ws-auth` | `codex app-server --help` |
| codex hardcodes stdio | `codex/provider.go:169` |
| codex process groups already handled | `provider.go:170-171,219,243,295`; `procutil_unix.go:15-75` |
| opencode `--pure` exists | `opencode serve --help` — *"run without external plugins"* |
| `pure` unimplemented | Repo-wide search: no match outside the CLI |
| env already inherited | `httpagent/provider.go:354`; acphttp sets no `cmd.Env` |
| goose `--with-builtin`, `--allowed-origin`, `--dangerously-unauthenticated` | `goose serve --help` |
| goose builtins already wired | `goose.go:17,37-39`; `config.go:398,769-780`; `acp_config_test.go:100` |
| no `Origin` on the ACP dial | `acphttp/conn.go:147-151` |
| `ReapOrphans` does not exist | Module-wide search for the declaration — none |
| codex multi-agent deferred | MADR 0028 §408-411, §577, §899-900 |

**Not verified:** `GROK_DISABLE_TOOLS` (the review mentioned it; it appears in no
`--help` output, and `--tools` / `--disallowed-tools` / `--disable-web-search`
are the documented controls). Nothing is decided on it.

---

## 6. Implementation

Phased, in
[0037-PLAN-cli-capability-uptake.md](./0037-PLAN-cli-capability-uptake.md).

Summary: **P1** D1 grok reasoning effort → **P1** D2 opencode `pure` →
**P2** D3 goose origin → **P2** D4 comment correction → docs and verification.

### Implementation Record

Implemented on 2026-07-27 across 5 targeted commits:
- **Phase 0**: Added binary contract probe tests `TestLiveGrokReasoningEffortFlag`, `TestLiveGooseAllowedOriginFlag`, and `TestLiveOpenCodePureFlag`.
- **Phase 1 (D1)**: Added `ReasoningEffort` to `acpagent.Config` and `GrokProviderConfig`, threaded through `grok.go` `defaultArgs` & `ModelArgs` (preserving effort on model override).
- **Phase 2 (D2)**: Added `Pure` to `OpencodeProviderConfig`, `httpagent.Config`, and `httpDialect`, appending `--pure` in `ServeArgs`.
- **Phase 3 (D3)**: Added explicit `Origin` header (`http://127.0.0.1:<port>`) in `acphttp/conn.go` `dialWS`.
- **Phase 4 (D4)**: Updated process supervision comment in `httpagent/provider.go` removing stale `ReapOrphans` reference.
- **Phase 5**: Documented config settings and updated MADR status to Accepted.
