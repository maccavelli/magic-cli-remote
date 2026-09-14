# MADR 0049: Grok has no auto mode — per-session auto-approve for ACP-stdio agents

<!-- markdownlint-disable MD013 MD060 -->

- **Status**: **Implemented** (2026-07-29), commits `2dd7c3c` (acpagent),
  `90c4506` (grok opt-in), `b010d4d` (tests), plus the live gate. Grok now
  advertises `default, plan, auto`; no other provider changed. Go suite green,
  `make preflight` green, and the `-tags live_grok` suite green against grok
  **0.2.114**. The live gate caught a D4 violation the fakes missed — see the
  plan's §D notes.
- **Date**: 2026-07-29
- **Scope**: `internal/provider/grok`, `internal/provider/acpagent`, mode
  advertisement and permission interception; no protocol change, no mobile
  change
- **Related**:
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) — auto as a *mode*;
    `SessionMode.dangerous`; opencode's daemon-side interception
  - [MADR 0047](./0047-MADR-codex-default-mode.md) — codex `default` mode,
    create-time seeding, `resolveDisplayedMode` (**both prior findings in this
    report are closed by it** — see §2)
  - [MADR 0048](./0048-MADR-codex-sandbox-namespace.md) — why codex `auto`
    cannot write on this host (sandbox execution, not policy)
  - [MADR 0039](./0039-MADR-grok-acp-parity.md) — `--permission-mode` exposed as
    `providers.grok.permission_mode`
  - [protocol-v1.md](./protocol-v1.md) — `session_mode` contract
- **Companion plan**: [0049-PLAN-grok-auto-mode.md](./0049-PLAN-grok-auto-mode.md)

---

## 1. Problem

Grok is the only agent provider whose mode menu cannot reach auto-approve.

`internal/provider/grok/grok.go:39-42` advertises exactly two modes:

```go
var staticModes = []event.SessionMode{
    {ID: "default", Name: "Build", Description: "Full tool access; edits allowed"},
    {ID: "plan", Name: "Plan", Description: "Research and plan only; no edits"},
}
```

There is no `auto`. A user on a grok session is prompted for every tool call
with no way to stop being asked, while the same phone running codex, opencode
or goose has an auto mode one tap away. Auto-approve is the feature MADR 0044
shipped; grok simply never got it.

Grok's engine *does* have an auto policy — `grok --help` exposes
`--permission-mode {default,acceptEdits,auto,dontAsk,bypassPermissions,plan}`
(MADR 0039 §5) and the daemon forwards it as
`providers.grok.permission_mode` (`grok.go:103-105`). But that is a **launch
flag on the grok process**, so today auto-approve for grok is:

- **operator-only** — it lives in the daemon's config file, not the phone;
- **process-wide** — it applies to every grok session the daemon starts;
- **restart-scoped** — changing it means restarting the engine;
- **invisible** — nothing tells the phone which permission mode the process was
  launched under, and the mode chip keeps saying `Build`.

So the one control the product promises ("auto means you will not be asked") is
unreachable per session, and the one mechanism that exists is unreachable from
the device.

## 2. Status of the two previously-reported findings

Both were reported alongside this one and are **already closed**; recorded here
so the report is not re-opened against stale code.

| Reported | Status |
|---|---|
| Codex menu shows only `auto` and `read-only`; the normal working mode is missing | **Closed** by MADR 0047 (commit `5831bd9`). `codexModes` now leads with `default` = `on-request` + `workspace-write` (`internal/provider/codex/mode.go:43-51`), which is Codex's own TUI "Auto" preset — the normal working mode. |
| New sessions land on the first mode in the list rather than the normal one | **Closed** by MADR 0047 on both sides. Daemon: `seedPolicy` seeds the `default` pair when config is empty, so `CurrentModeID` is no longer `""`. Mobile: `resolveDisplayedMode` (`apps/mobile/lib/features/chat/chat_helpers.dart:16-36`) resolves the exact id, then a non-dangerous `default`/`build`, then any non-plan non-dangerous mode, and only falls back to `modes.first` as a last resort — so list order no longer invents a selection. |
| Codex `auto` cannot write | **Not a mode bug.** MADR 0048 attributes it to bubblewrap failing to create a user namespace on AppArmor-restricted hosts: the daemon puts `never` + `workspace-write` on the wire correctly, but the sandbox backing it cannot execute. Tracked there, not here. |

This MADR therefore covers **only** the grok gap.

### Proven end to end (2026-07-29, after MADR 0050)

When this MADR landed, its mechanism was verified structurally — the mode is
advertised, armed, reported, disarmed, and the synthetic id never reaches the
agent — but **not** that it changed anything, because grok never prompted.
That turned out to be [MADR 0050](./0050-MADR-grok-cli-surface-drift.md): the
`--permission-mode` flag was placed where grok rejects it, so grok always ran
in its host default, which on the test host asks for nothing.

With that fixed and `permission_mode: default`, the discriminating pair passes
(`TestLiveGrokAutoDiscriminationPair`, grok 0.2.114):

| auto armed | permission requests | file written | turn completed |
|---|---|---|---|
| no | **1** | no | no (blocked on approval) |
| yes | **0** | **yes** | **yes** |

Armed auto suppresses a real prompt *and* lets the agent finish the work. The
interception was correct all along; it was unreachable, not broken.

## 3. Why grok cannot reuse either existing mechanism

Two mechanisms already exist for auto, and neither transfers unchanged.

**Codex's mechanism is engine-native policy.** `SetMode` rewrites the session's
`approvalPolicy`/`sandbox` pair and re-sends it on every `turn/start`
(MADR 0044 D5). Grok has no equivalent per-turn policy channel: its policy is
fixed by `--permission-mode` at process launch.

**Grok's own `auto` is not settable per session.** `acpagent.SetMode`
(`internal/provider/acpagent/session.go:486-504`) validates a synthetic mode id
against `staticModes` and then forwards **every** id to the agent as ACP
`session/set_mode`. Grok's ACP mode vocabulary is `default`/`plan` only — the
two ids its TUI toggles between — so forwarding `auto` would be rejected by the
agent. `auto` in `--permission-mode` and `auto` as an ACP mode id are different
namespaces that happen to share a word.

**OpenCode's mechanism does transfer.** OpenCode synthesizes an auto entry onto
the engine's list (`withAutoMode`, `internal/provider/opencode/mode.go:127-130`)
and enforces it daemon-side by short-circuiting the permission request. ACP-stdio
agents already have the identical interception point:
`acpagent.session.RequestPermission` (`session.go:1270-1273`) short-circuits to
`autoAllow(params)` — today only for the process-wide `cfg.AlwaysApprove`.

The gap is that this hook has no *per-session* input.

## 4. Decision

**Give ACP-stdio agents an opt-in synthetic `auto` mode, enforced by the
daemon's existing permission interception, and turn it on for grok.**

### D1 — Synthesize `auto`; do not forward it to the agent

`auto` is added to the advertised list by the daemon and is **intercepted in
`SetMode` before the ACP call**. Arming it does two things:

1. sets a per-session `autoApprove` flag, and
2. forwards the provider's *normal* mode id (`default` for grok) to the agent,
   so a session sitting in `plan` leaves plan mode rather than silently
   auto-approving a mode that refuses edits.

Disarming (selecting any real mode) clears the flag and forwards that mode
normally.

Rejected: mapping `auto` onto `--permission-mode auto`. That flag is
process-wide and launch-scoped; honouring a per-session switch through it would
mean restarting the engine under every other session's feet.

### D2 — Opt-in per provider via the `acpagent.Spec`

A new `Spec.SynthesizeAutoMode bool` gates this. Grok sets it; every other
ACP-stdio agent is unchanged by default. Goose in particular must **not** get a
second auto — it has a native `auto` mode and it is already goose's default
(`internal/provider/goose/goose.go:23-27,48`), and goose is an `acphttp`
provider anyway.

### D3 — `auto` is `Dangerous: true`, and last in the list

The synthetic entry carries `Dangerous: true`, which is what makes the mobile
`_ModeSelector` gate it behind the arming confirmation (MADR 0044 D1). It is
appended **after** the agent's own modes so `default` stays the menu head, per
MADR 0047 D1: the normal mode leads, and any residual first-item fallback in an
older client still lands somewhere safe.

### D4 — Report `auto` as current only while it is armed

`emitModes` reports `auto` when the flag is set, otherwise whatever the agent
reports. A mode chip that says `auto` while the daemon is still forwarding
prompts would be the same class of lie MADR 0047 fixed on the mobile side.

### D5 — Interception is the enforcement, and it is honest about its limits

`RequestPermission` returns `autoAllow(params)` when the session flag is set.
This covers every permission grok routes through ACP. It does **not** cover
policy grok enforces internally on its own tool calls (`acceptEdits`,
`dontAsk`, `bypassPermissions` are CLI-side logic — MADR 0038 §5). That is the
same contract codex has: the daemon guarantees "you will not be asked", not
"the agent's internal policy changed".

### D6 — `providers.grok.permission_mode` is unchanged and takes precedence

The launch flag stays exactly as MADR 0039 shipped it. If an operator launched
grok with `bypassPermissions`, grok will not ask in the first place and the
session mode is advisory. This is documented rather than reconciled: the two
live at different scopes (process vs session) and collapsing them would remove
an operator control.

## 5. Consequences

**Good.** Grok reaches parity with the other three providers; auto-approve
becomes a per-session, per-device decision instead of a config edit and a
restart; the mechanism is the one already proven for opencode; no protocol
change, no mobile change, no new event type — the phone renders whatever the
daemon advertises.

**Cost.** One more synthetic-vs-real mode distinction in `acpagent`, which is
the same complexity opencode already carries. `SetMode` gains a branch that
must be kept in step with the advertised list.

**Risk.** The failure mode to guard is arming `auto` and having the flag not
actually intercept — a mode chip promising no prompts while prompts keep
arriving. The plan's tests assert the interception directly, not just the
advertisement.

**Not addressed.** Grok's internal permission policy is untouched; MADR 0048's
sandbox-execution problem is codex-specific and independent.
