# MADR 0044: Auto-approve as a session mode (OpenCode, Codex)

- **Status**: **Implemented** (2026-07-28) on `feat/auto-approve-modes`, all
  eight decisions, across six phases with the build order and per-phase
  verification recorded in the [plan](./0044-PLAN-auto-approve-modes.md).
  Awaiting review before merge.

  Two corrections were forced during implementation and are folded in below:
  D1's "no new protocol" claim became one additive optional field
  (`SessionMode.dangerous`), and D4.2 was rewritten after the original
  bookkeeping design proved to leave the permission-expiry fail-safe armed.
- **Date**: 2026-07-28
- **Deciders**: Project Owner (product surface, risk posture); Implementer
  (daemon/providers/mobile)
- **Related**:
  - [MADR 0022](./0022-MADR-plan-mode-parity.md) — session modes end to end
    (`session_mode` event, `session.set_mode`, `provider.ModeSession`, mobile
    switcher). This MADR extends that machinery rather than adding protocol.
  - [MADR 0020](./0020-MADR-opencode-session-tree.md) — OpenCode permission dual
    shapes (`permission.asked` / `permission.updated`), permission origin
    tracking, session tree
  - [MADR 0028](./0028-MADR-codex-provider.md) — codex app-server transport
  - [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md) — SSE resync, which
    re-emits pending permission sheets
  - [protocol-v1.md](./protocol-v1.md) — `session_mode`, `session.set_mode`,
    `permission_request`, `permission_resolved`

**Verified against** (live, not inferred, on `master` at 2026-07-28):
**OpenCode 1.18.7** (HTTP `serve` transport + decompiled CLI bundle),
**codex-cli 0.145.0** (`app-server --listen stdio://`, driven directly over
JSON-RPC).

**Standards**: reviewed against `/home/mac/standards` `go` v1.26.5-v3 and
`mobile` v3.12.2-v3 (both 2026-07-28). The obligations that change what gets
built are tabulated in the plan's *Standards conformance* section; the two that
changed the **design** rather than the style are recorded in D4.1 (errors must
not be silently ignored) and D4.2 (one bookkeeping path).

---

## Context

The ask: let a session run in **auto-approve mode**, so permission prompts are
answered without the user acting, selected per session rather than globally.

The initial hypothesis was that both CLIs expose auto-approve as a command-line
flag, so the daemon could pass the flag when launching the engine for that
session. **Both halves of that hypothesis are false**, in different ways, and
the failure modes are what drive this decision. Everything below was probed
directly.

### Finding 1 — the flag cannot reach a per-session boundary in either provider

Neither provider gets a process per session. Both run **one shared engine per
provider, with many sessions multiplexed onto it**:

| provider | spawn site | shape |
|---|---|---|
| OpenCode | `internal/provider/opencode/http.go:544` | `opencode serve --hostname 127.0.0.1 --port N` — one HTTP server, sessions are `POST /session` |
| codex | `internal/provider/codex/provider.go:261` | `codex app-server --listen stdio://` — one JSON-RPC engine, sessions are `thread/start` |

A process-level flag is therefore **global by construction**. It would apply to
every session on the engine, which is precisely the opposite of what was asked
for. Restarting or forking a second engine per auto-approve session was
considered and rejected under "Options considered" below.

### Finding 2 — OpenCode's `--auto` is a *client-side* auto-responder, not a mode

`--auto` exists on `opencode` (TUI) and `opencode run`, with hidden aliases
`--yolo` and `--dangerously-skip-permissions`. It does **not** exist on
`opencode serve` or `opencode acp` — the two headless entry points — which is
already fatal for the flag approach.

More decisively, reading the shipped bundle (`/home/mac/.opencode/bin/opencode`
is a non-stripped bun binary; the CLI source is recoverable verbatim) shows what
the flag actually does. In the `run` handler's event loop:

```js
if (Y.type === "permission.asked") {
  const J = Y.properties
  if (J.sessionID !== W) continue
  if (Yj) await N.permission.reply({ requestID: J.id, reply: "once" })   // Yj = --auto
  else { /* print "auto-rejecting" */ await N.permission.reply({ requestID: J.id, reply: "reject" }) }
}
```

…and in the TUI handler it is passed through as `args: { …, auto: D.auto || D.yolo || D["dangerously-skip-permissions"] }`.

So `--auto` **never reaches the server**. There is no server-side "auto mode" in
OpenCode. The flag is a client that subscribes to the event stream and replies
`"once"` to each `permission.asked` — which is *exactly the role mcremote's
daemon already plays* for remote sessions.

This is the crux: **the feature is not a flag to forward, it is a client
behaviour to implement**, and mcremote is the client.

Confirming this is viable: the reply path is a plain HTTP call the daemon
already makes — `POST /permission/{id}/reply {"reply":"once"}`, with a
session-scoped fallback (`internal/provider/opencode/permission.go:142`). And a
first version of the behaviour already exists in-tree as the global
`always_approve` config flag (`permission.go:108`). The work is to make it
**per-session, runtime-switchable, and safe**, not to invent it.

### Finding 3 — OpenCode *does* have a server-side per-session permission ruleset, but it is create-time only

`POST /session` accepts `permission: PermissionRuleset` (verified against the
live OpenAPI document at `GET /doc`):

```
PermissionRuleset = PermissionRule[]
PermissionRule    = { permission: string, pattern: string, action: PermissionAction }
PermissionAction  = "allow" | "deny" | "ask"
```

The CLI itself uses this to suppress interactive-only permissions in `run`
(`[{permission:"question",action:"deny",pattern:"*"}, …]`).

This is genuinely server-side and genuinely per-session, and it was the
strongest alternative. It is rejected as the primary mechanism because:

- **Create-time only.** No endpoint mutates a live session's ruleset, so it
  cannot back a runtime mode switch — which is the whole point of putting auto
  in the mode menu.
- **The permission key space is open.** `PermissionConfig` names 15 built-ins
  (`read`, `edit`, `bash`, `glob`, `grep`, `list`, `task`, `external_directory`,
  `todowrite`, `question`, `webfetch`, `websearch`, `lsp`, `doom_loop`, `skill`)
  **plus `additionalProperties`** for MCP tools. "Allow everything" is therefore
  not reliably expressible: an enumerated allow-list silently fails open-ended
  for any MCP tool, and a wildcard key is not specified behaviour we can lean on
  across versions.
- It would give two divergent enforcement points for one user-visible mode.

It remains a useful future refinement (see "Consequences → follow-ups").

### Finding 4 — codex has no `--full-auto` any more, but the protocol carries the policy per thread *and* per turn

`--full-auto` is gone from the interactive CLI in 0.145. What remains is
`-a/--ask-for-approval {untrusted, on-request, never}`,
`-s/--sandbox {read-only, workspace-write, danger-full-access}`, and
`--dangerously-bypass-approvals-and-sandbox`.

But codex does not need a flag, because the app-server protocol already carries
approval policy and sandbox as **per-thread and per-turn parameters**. Probed
live against `codex app-server`:

| request | params | result |
|---|---|---|
| `thread/start` | `sandbox: "workspace-write"`, `approvalPolicy: "never"` | **accepted**, thread created |
| `thread/start` | `sandbox: "danger-full-access"` | **accepted** |
| `thread/start` | `sandbox: {"type":"read-only","networkAccess":false}` | **rejected** — `-32600 invalid value: map, expected map with a single key` |
| `thread/start` | `sandbox: "totally-bogus"` | rejected — `unknown variant, expected one of read-only, workspace-write, danger-full-access` (so the server really does validate) |
| `thread/start` | `approvalPolicy: {granular:{…}}` | rejected — `askForApproval.granular requires experimentalApi capability` |
| `turn/start` | `approvalPolicy:"never"`, `sandboxPolicy:{"type":"workspaceWrite","networkAccess":false,"writableRoots":[]}` | **accepted** |
| `turn/start` | `sandboxPolicy:{"type":"dangerFullAccess"}` | **accepted** |
| `turn/start` | `sandboxPolicy:"workspace-write"` (string) | rejected — `expected internally tagged enum SandboxPolicyDeserialize` |

The generated schema documents `turn/start.approvalPolicy` and
`turn/start.sandboxPolicy` as *"Override … for this turn and subsequent turns"*.
**That is a runtime mode switch, natively.**

Note the shape asymmetry, which is easy to get wrong and which we did get wrong:
`thread/start.sandbox` is a **kebab-case string**, `turn/start.sandboxPolicy` is
an **object with a camelCase type tag**.

### Finding 5 — a latent bug in the existing codex sandbox override *(fixed — see D7)*

`internal/provider/codex/session.go:183` sent:

```go
params["sandbox"] = map[string]any{"type": s.cfg.SandboxMode, "networkAccess": false}
```

Per Finding 4 that shape is **rejected by codex 0.145** with `-32600`, failing
session creation outright. It was latent only because
`providers.codex.sandbox_mode` defaults to `""` (`internal/config/config.go:597`)
and the field is omitted when empty — so any user who set the documented,
validated config option (`internal/config/config.go:844`) got a hard failure.

It survived the test suite because `internal/provider/codex/fixtures_test.go:320`
asserted the *wrong* shape against a fake engine, and even asserted the
kebab-case `type` value that the real server will not accept in object form.
This is a fake-vs-real contract drift of the kind [the fake provider
contract](./0013-MADR-audit-remediation-decisions.md) is meant to prevent.

A **second instance of the same bug** surfaced while fixing it: `resume`
(`session.go:244`) sent *neither* `sandbox` nor `approvalPolicy`, though
`ThreadResumeParams` carries both. A resumed thread therefore fell back to the
engine's own defaults — the same session ran under a different sandbox and
approval policy after a daemon restart. Silent rather than loud, and worse for
this MADR: D5 depends on resume re-asserting the session's policy.

Fixing both was in scope here because this MADR makes that code path
load-bearing.

---

## Decision

**Model auto-approve as a session mode, reusing the existing mode machinery
end to end, and enforce it with the mechanism each provider actually supports —
daemon-side interception for OpenCode, engine-native policy for codex, with
interception as a second layer.**

### D1 — Auto is a *mode*, not a create-session checkbox

The mode surface (`session_mode` event → `session.set_mode` op →
`provider.ModeSession` → `_ModeSelector` in the chat app bar) is already plumbed
end to end from MADR 0022. Auto-approve is exactly a session-scoped operating
mode, and putting it beside Build/Plan means:

- **no new protocol message, no new event type, no new mobile control** — the
  existing `_ModeSelector` renders whatever the daemon advertises.

  *Corrected during review*: this originally claimed **zero** protocol change.
  One **additive, optional** field is needed — `SessionMode.dangerous` — so the
  client can tell an alarming mode from an ordinary one. The alternative was
  matching on the mode id in the UI, which breaks goose: goose has shipped an
  `auto` mode for a while and it is goose's **default**, so id-matching would
  paint an alarm on goose's normal state and gate a one-tap control behind a
  dialog. Danger is a property only the provider knows, so the provider declares
  it. `omitempty` keeps old daemons and old clients compatible in both
  directions;
- it is switchable **mid-session**, which a create-time toggle is not. The most
  likely reason to want auto-approve is that an agent is *already* stuck asking
  for things while the user is away from the phone — a create-time-only control
  cannot serve that case at all;
- it survives session resume as a decision the user re-makes, rather than a
  sticky property baked into a session record.

A create-time default is deliberately **not** part of this decision (see
follow-ups): it is additive later and adds risk now.

### D2 — Modes are mutually exclusive, and auto implies the normal agent

The menu is single-select and must stay honest. Selecting `auto` sets
auto-approve **on** and points prompts at the normal agent (`build` on
OpenCode). Selecting `build` or `plan` sets auto-approve **off**.

Rejected alternative: remembering the underlying agent so `auto → plan → auto`
restores the prior agent. That is hidden state behind a security-relevant
control, and "which agent am I actually running?" stops being answerable from
the chip.

### D3 — OpenCode: enforcement is daemon-side interception

Per Finding 2 this is not a workaround; it is what the vendor's own client does.
A per-session `autoApprove` flag lives on `httpagent.session` (alongside the
existing `Agent`/`SetAgent` pair, `session.go:298`), exposed on the `Host`
interface, and gates `emitPermissionAsk`
(`internal/provider/opencode/permission.go:102`): approve instead of surfacing
the sheet.

Placing the gate **inside `emitPermissionAsk`** rather than at the SSE call
sites is load-bearing: it is the single funnel for `permission.asked`,
`permission.updated`, `permission.v2.asked` **and** the resync path
(`internal/provider/opencode/resync.go:158`), so all four compose for free and
no future permission shape can bypass it.

The flag also naturally covers **subagent/child permissions**, because child
SSE is aliased onto the parent host.

### D4 — Auto-approve must be observable, bounded, and fail-safe

The existing global `always_approve` path is fire-and-forget
(`_ = o.RespondPermission(...)`, `permission.go:117`) and skips both origin
tracking and dedup. That is acceptable for a config flag a user opted into once
in a file; it is **not** acceptable for a control on the chat screen. This
decision requires:

1. **Fail-safe, not fail-silent.** If the reply call fails, retry with backoff;
   on final failure **fall back to surfacing the permission sheet** plus a
   notice. A swallowed error today means the agent blocks forever with nothing
   on the user's screen and no way to answer.
2. **Answer through the same path a user answers through.** The auto path must
   call the transport's `RespondPermission` (`httpagent/session.go:637`), not
   the dialect's engine call (`opencode/http.go:989`). The transport method
   claims the id, clears the recorded origin, emits `permission_resolved` and
   drains the prompt queue; the dialect method does none of that.

   This is not a style preference. Tracking a permission for dedup while
   replying through the dialect arms `expirePermission` and never disarms it,
   so `PermissionTimeout` later **cancels a permission the daemon already
   approved** and emits a spurious timeout notice. One shared bookkeeping path
   is the only version of this that is correct; see plan §2.0 for the full
   failure trace.

   `TrackPermissionOrigin` and `TrackPermission` still run *before* the auto
   branch, so the session-scoped reply fallback can route child-session
   permissions and a duplicated `asked`/`updated` pair for one id cannot fire
   two replies.
3. **Audit trail.** Every auto-approval emits a `notice` event carrying the
   permission name and patterns, plus an INFO log line. Auto-approve must never
   mean *invisible*: the user has to be able to scroll back and see what was
   allowed on their behalf.

   `notice` specifically, **not** `permission_resolved`: the mobile reducer's
   `_onPermissionResolved` (`transcript_reducer.dart:133`) only *clears* a
   pending sheet, so a resolved event with no preceding `permission_request`
   renders nothing at all. `notice` is already rendered as a neutral system line
   (`transcript_reducer.dart:141`), so the audit trail needs no mobile change.
   Emitting `permission_request` + `permission_resolved` back to back was
   rejected: it flashes a sheet the user is not meant to answer.
4. **Reply `"once"`, never `"always"`.** `"always"` persists a rule beyond the
   session and beyond the mode being switched off. Auto-approve is a session
   mode; its blast radius ends with the session.
5. **Arming sweeps what is already pending.** Switching to auto answers
   permissions already outstanding. Without this, the primary use case —
   unblocking an agent that is already waiting — silently does nothing until the
   *next* request.

### D5 — codex: engine-native policy, with three modes and a sandbox floor

codex gains session modes for the first time. Three ids, mapping to the
engine-native pair:

| mode id | `approvalPolicy` | sandbox | meaning |
|---|---|---|---|
| `read-only` | `on-request` | `read-only` | agent reads and asks before anything else |
| `auto` | **`never`** | **`workspace-write`** | **no prompts; the sandbox is the guardrail** |
| `full-access` | `never` | `danger-full-access` | no prompts, no sandbox |

> **Mode list extended by [MADR 0047](./0047-MADR-codex-default-mode.md):** a
> non-dangerous `default` mode (`on-request` + `workspace-write`) is advertised
> first and is the create-time seed when config is empty. Auto and full-access
> **semantics are unchanged**. Create-time never-without-sandbox is repaired to
> the auto pair so untrusted projects do not stay `readOnly` while auto-approve
> is armed.

The important choice is `auto`. codex's own "Auto" preset is
`on-request` + `workspace-write`, which still prompts when the agent crosses the
workspace boundary — that is not what was asked for. Setting `never` removes the
prompts; pairing it with `workspace-write` rather than `danger-full-access`
means **a codex session with no human in the loop is still contained**: edits
land in the project, and a network call or a write outside the workspace fails
and is returned to the model instead of silently succeeding.

Auto-approve without a sandbox is a materially different risk, so it gets its
own mode (`full-access`) and is **gated behind an explicit config opt-in**
(`providers.codex.allow_full_access`, default `false`); when off, the mode is
not advertised at all.

Mechanism: mode state is applied at `thread/start` (create-time default) and
re-sent on **every** `turn/start` (`session.go:384`). Re-sending each turn rather
than once is deliberate — it makes the engine's state converge on the daemon's
state after an engine restart or thread resume, instead of drifting.

### D6 — codex keeps daemon-side interception as a second layer

Even with `approvalPolicy: "never"`, codex can still route approvals that are
not command/patch execution (MCP elicitations, and whatever the granular policy
covers once `experimentalApi` is negotiated). So `handleApprovalRequest`
(`internal/provider/codex/session.go:1130`) also honours the per-session auto
flag, not only the global `cfg.AlwaysApprove`.

The result is one user-visible contract — "auto means you will not be asked" —
that holds regardless of which layer the engine happens to use.

### D7 — Fix the codex sandbox wire shape (Finding 5) — **implemented 2026-07-28**

`thread/start.sandbox` becomes the plain kebab-case string;
`turn/start.sandboxPolicy` uses the camelCase-tagged object. The fixture test
asserting the wrong shape is corrected, and a live-gated test asserts both
shapes against a real engine so the fake cannot drift again.

Shipped as described, plus the resume half of Finding 5. What landed:

- `applyPolicyParams` (`internal/provider/codex/session.go`) — one helper used
  by both `startNew` and `resume`, carrying the string-vs-object asymmetry in
  its doc comment so the next reader does not re-derive it.
- `resume` now sends `sandbox` + `approvalPolicy`, so a resumed thread runs
  under the session's policy rather than the engine's defaults.
- `fixtures_test.go` — the hand-built params literals are replaced by
  `captureThreadRequest`, which drives `session.create` through a real `conn`
  over `io.Pipe` and asserts against the frame that actually goes on the wire.
  The old tests could not have caught this: they asserted against a map the
  test itself constructed and never touched production code. **Any future test
  of this surface must drive create/resume, never a local literal.**
- `live_sandbox_test.go` (build tag `live_codex`) — pins both shapes against a
  real engine **in both directions**. Asserting that the *wrong* shape is
  rejected is what stops the fake drifting back; a codex release that starts
  accepting both will fail this test and force a conscious decision.

Verified: reverting `applyPolicyParams` to the object form fails all four
`TestThreadStartSandboxIsEnumString` / `TestThreadResumeCarriesPolicy`
subtests, confirming the guard is real and not merely green. Live tests pass
against codex 0.145.0. `go build ./...`, `go test ./internal/...`,
`go test -race ./internal/provider/codex/`, `gofmt`, `go vet` (with and without
`-tags live_codex`) and `govulncheck` all clean.

### D8 — Auto-approve does not persist

Not written to the session record, not restored on resume, not restored across a
daemon restart. Re-arming is one tap. A security-relevant control that silently
survives a restart the user did not observe is worse than one they have to
re-confirm.

---

## Options considered

| option | why not |
|---|---|
| **Pass `--auto` / `-a never` to the engine process** (the original ask) | Impossible as specified. `--auto` does not exist on `opencode serve`/`acp`; both engines are shared across sessions so a process flag is global; and per Finding 2 `--auto` is a client behaviour that never reaches the OpenCode server at all. |
| **Spawn a second, dedicated engine per auto-approve session** | Makes the flag per-session, at the cost of a second `opencode serve` / `codex app-server` per session — memory, ports, model catalog refetch, and a second SSE/stdio pump — plus it defeats OpenCode's session tree, which assumes one engine per project. Enormous cost to emulate an HTTP call the daemon can already make. |
| **OpenCode `POST /session` permission ruleset** (Finding 3) | Genuinely server-side, but create-time only (cannot back a runtime switch) and cannot express "allow everything" safely over an open-ended, MCP-extensible key space. Kept as a follow-up. |
| **A create-session checkbox instead of a mode** | Cannot serve the main use case (an agent already stuck mid-session), and would need new protocol + new mobile UI, where the mode menu needs neither. |
| **Reuse the existing global `always_approve` config** | Already exists and already works — but it is per-provider and per-daemon, i.e. the exact opposite of per-session, and is not reachable from the phone. |
| **A separate orthogonal "auto-approve" toggle beside the mode chip** | Semantically cleaner (auto really is orthogonal to build/plan) but needs a new protocol op, new event field and a new mobile control, to expose a combination (`plan` + auto-approve) that is close to meaningless — plan mode makes no edits, so it has almost nothing to approve. |
| **codex `auto` = `never` + `danger-full-access`** | Simplest, and matches "YOLO" expectations — but removes the only guardrail at exactly the moment the human leaves the loop. Split into a separate, opt-in-gated `full-access` mode instead. |

---

## Consequences

**Good**

- Zero protocol change and zero mobile plumbing: `_ModeSelector` renders the new
  entries as-is.
- One user-visible contract across two providers with very different internals.
- Switchable mid-session, including to unblock an already-waiting agent.
- codex gains session modes and a sandbox control it has never exposed remotely,
  as a by-product.
- A real, live-verified bug (Finding 5) is fixed before it reaches a user —
  **already done** (D7), in both its create and resume forms, with a live
  contract test that closes the fake-vs-real gap that hid it.

**Bad / accepted risk**

- Auto-approve is genuinely dangerous, and putting it one tap from the chat
  screen makes it reachable by accident. Mitigated by the confirm-on-arm
  requirement, the distinct chip treatment, the audit trail (D4.3) and
  non-persistence (D8) — not eliminated.
- OpenCode auto-approve is only as good as the daemon's event stream: a
  permission that never reaches the daemon is never auto-approved. The resync
  path (D3) and the existing `PermissionTimeout` fail-safe bound this, but a
  wedged SSE stream means the agent waits, as it does today.
- codex `auto` will let the model take actions that fail at the sandbox boundary
  and get the failure returned to it, rather than escalating to the user. That
  is the documented `never` semantic and is the intended trade, but it can look
  like an agent going in circles.
- Three codex modes plus two OpenCode modes plus `auto` means the mode chip now
  carries provider-specific vocabulary. Acceptable — it already did (`build` is
  not an ACP concept).

**Follow-ups, explicitly out of scope here**

1. Create-time default (`session.create` carrying an initial mode), once the
   runtime path has shipped and been used.
2. OpenCode `POST /session` permission ruleset as a *defence-in-depth* layer
   under the interception, once the wildcard/MCP key semantics are pinned to a
   version.
3. codex granular approval policy (`approvalPolicy.granular`), which needs
   `experimentalApi: true` in the initialize handshake
   (`internal/provider/codex/provider.go:298`) — a broader decision than this one.
4. Applying the same mode to grok, which reaches permissions through the ACP
   transport's own `AlwaysApprove` path.

   **Not goose** — goose already ships this. It advertises `auto` / `approve` /
   `smart_approve` / `chat` (`internal/provider/goose/goose.go:23`), enforced
   engine-side via ACP `session/set_mode`, with `auto` as its **default**. This
   MADR deliberately reuses goose's `auto` id so one word means one thing across
   providers, and deliberately leaves goose's behaviour untouched. Goose is the
   working precedent for D1 and D2, not a target.

   One observation for a separate decision: goose defaulting to auto-approve
   means every goose session on the phone starts with no human in the loop,
   which is a different posture from what this MADR proposes for OpenCode and
   codex (opt in, per session, non-persistent). Worth revisiting on its own
   merits; changing it is out of scope here.

---

## Verification evidence

Reproducible with the tools on this machine:

- OpenCode flag surface: `opencode --help`, `opencode run --help`,
  `opencode serve --help`, `opencode acp --help` (1.18.7).
- OpenCode `--auto` semantics: `strings -n 8 ~/.opencode/bin/opencode` and grep
  for `auto-approve permissions that are not explicitly denied` — the bundled
  CLI source around that string contains both handlers quoted in Finding 2.
- OpenCode session/permission schemas: `opencode serve --hostname 127.0.0.1
  --port N`, then `GET /doc` → `paths./session.post.requestBody` and
  `components.schemas.PermissionRuleset` / `PermissionAction`.
- codex flag surface: `codex --help`, `codex exec --help`,
  `codex app-server --help` (0.145.0).
- codex protocol schema: `codex app-server generate-json-schema --out DIR` →
  `ClientRequest.json`, definitions `ThreadStartParams`, `TurnStartParams`,
  `AskForApproval`, `SandboxMode`, `SandboxPolicy`.
- codex wire-shape probes (Finding 4 table): drive `codex app-server --listen
  stdio://` over stdio JSON-RPC — `initialize`, `initialized`, then
  `thread/start` / `turn/start` with each candidate shape.

Sources for the codex approval/sandbox semantics quoted in D5:

- [Sandbox & approvals · Codex Docs](https://docs.onlinetool.cc/codex/docs/sandbox.html)
- [Codex CLI approval policies and sandbox modes explained](https://vladimirsiedykh.com/blog/codex-cli-approval-modes-2025)
- [Codex CLI Full-Auto Mode](https://www.frr.dev/posts/codex-cli-autonomous-agent-two-flags/)
