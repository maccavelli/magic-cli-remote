# MADR 0022: Plan mode parity (`/plan`) across providers

- **Status**: **Accepted** — Phase 1 implemented on `master` (2026-07-25). Phase 2
  (grok plan-approval hand-off) deferred to a follow-up PR.
- **Date**: 2026-07-25
- **Deciders**: Project Owner (command surface, phasing); Implementer
  (daemon/providers/mobile)
- **Related**:
  - [protocol-v1.md](./protocol-v1.md) — `session_mode` event, `session.set_mode`,
    built-in slash commands
  - [MADR 0020](./0020-opencode-session-tree.md) — OpenCode agents (`agents.list`,
    `StartOptions.Agent`), slash commands (Sprint 5)
  - [MADR 0004](./0004-phase2-grok-acp.md) — grok ACP transport

**Verified against** (live, not inferred): grok **0.2.112** over ACP stdio,
OpenCode **1.18.5** over HTTP, on `master` at 2026-07-25.

---

## Context

`/plan` from the phone answered *"'/plan' isn't available over the remote"*.
`Manager.Prompt` routes a leading `/name` to a built-in, to a command the agent
advertised, or to that notice — and `plan` was none of them. The routing was
correct; what was missing is that **neither provider exposes plan mode in a form
the daemon could see**, while the abstraction that fits it (ACP session modes,
already plumbed end to end: `session_mode` event, `session.set_mode` op,
`provider.ModeSession`, mobile switcher) was never populated.

What the two providers actually do (probed, with the evidence that matters):

| | reality | consequence before this change |
|---|---|---|
| **grok** | `session/set_mode {modeId:"plan"}` **works** — ids `plan` / `default`, each confirmed by a `current_mode_update`. Proof it is not a no-op echo: a following `_x.ai/toggle_plan_mode` notification flipped the session to `default`, i.e. the set had really moved shell state. Grok also accepts *any* id (`bogus-xyz` was echoed back). | `session/new` returns `modes: null`, so `emitModes` no-opped: no list, no switcher, no `/plan`. Grok's 33 advertised commands do not include `plan` (it is a TUI toggle: Shift+Tab). |
| **OpenCode** | plan mode is the built-in **`plan` agent** ("Plan mode. Disallows all edit tools."), selected per message via the `agent` field already sent on `prompt_async`. | the agent was fixed at session create with no setter, no `session_mode` event was emitted, and `session.set_mode` failed with "session does not support modes". `GET /command` has no `plan`. |

## Decision

**Model plan mode as a session mode on every provider, and make `/plan` a thin
daemon built-in on top of `session.set_mode`.**

1. **grok — supply the vocabulary the agent omits.** `acpagent.Spec` gains
   `StaticModes` + `DefaultModeID`; when the agent advertises no modes, the
   session emits that list instead (`emitModesOrStatic`). Because the list is then
   *ours*, `SetMode` validates ids against it — grok would otherwise report a typo
   as a successful switch. An agent-supplied list always wins and is never
   second-guessed.
2. **OpenCode — make the agent switchable.** `httpagent.session` gains a locked
   `Agent`/`SetAgent` pair and implements `provider.ModeSession`, delegating to an
   optional `dialectMode` hook (same shape as the existing fork/revert/diff
   hooks). The OpenCode dialect advertises its **primary, non-hidden** agents as
   modes at create and resume, and `SetMode` repoints subsequent prompts. The
   engine binds the agent per message, so a switch takes effect on the next
   prompt — stated in the command's notice rather than hidden.
3. **Daemon — `/plan` plus a general `/mode`.** The manager tracks the advertised
   modes and current id from `session_mode` events (same place it tracks
   `available_commands`). `/plan` enters plan mode and `/plan off` returns to the
   provider's normal mode, resolved from the advertised list (`default` on grok,
   `build` on OpenCode) rather than hardcoded per provider. `/mode` lists and
   switches anything advertised.
4. **Soft built-ins.** `plan` and `mode` yield to an agent that advertises a
   command of the same name, so a future provider shipping a real `/plan` is not
   shadowed by the daemon's interpretation.

### Consequences

- The mode switcher in the mobile app bar now appears for grok and OpenCode
  sessions (it was dead UI before), and plan mode is called out with a tinted
  chip and an edit-off icon: "the agent will not edit my files" is the one mode
  distinction worth noticing at a glance.
- `/plan` and `/mode` are offered in composer autocomplete only where the session
  has modes, mirroring the daemon's own gating.
- Hidden engine-internal OpenCode agents (`compaction`, `summary`, `title`) are
  now dropped from **both** the mode list and the `agents.list` picker, which had
  been offering them as selectable agents.
- Fixed alongside: `hydrateFromCache` preserved live status/commands/plan but not
  `modes`/`currentModeId`/`capabilities`/`configOptions`. Since `session_mode`
  arrives at create — before any chat item — a cache load landing afterwards wiped
  the mode strip (and `/plan`) for the rest of the session.
- `/help` now lists the session's modes and only shows grok's "terminal-only
  commands" caveat on grok sessions, where it is true.

### Known limitations

- **Resumed grok sessions report the default mode.** ACP has no "read current
  mode" call and `session/load` returns no modes, so a session resumed while grok
  was left in plan mode is advertised as `default` until something changes it. The
  next `current_mode_update` (from `/plan`, the switcher, or grok's own
  `enter_plan_mode` tool) corrects it. Reading grok's `_x.ai/session/info`
  extension would fix it but ties the shared ACP machinery to one agent's private
  surface; not worth it for a cosmetic staleness.
- **OpenCode switches apply to the next message.** Inherent: the engine binds the
  agent per prompt, so a mode change cannot alter a turn already in flight.

### Alternatives rejected

- **Per-provider `/plan` handling inside each provider's `Prompt`.** Duplicates
  logic, and for grok would mean fabricating a command it never advertises.
- **Driving grok with `_x.ai/toggle_plan_mode`** (its TUI's own path, an extension
  *notification*). It works, but it is a toggle: the daemon would have to track
  state to avoid flipping the wrong way, and standard `session/set_mode` is both
  idempotent and already implemented.
- **Explicit per-provider commands (`/build`, `/default`).** The command list
  would differ per provider and grow with every mode.

## Phase 2 (deferred): grok's plan-approval hand-off

When grok's model finishes planning it calls its `exit_plan_mode` tool, and the
shell sends the **client** an ACP extension request `_x.ai/exit_plan_mode`
(`ExitPlanModeExtRequest`/`Response`, 3 fields — not in the ACP SDK) expecting a
plan-approval answer. We implement no `acp.ExtensionMethodHandler`, so the SDK
answers method-not-found: plan mode is enterable and usable, but that approval
step fails. Same for `_x.ai/ask_user_question`.

Follow-up PR: probe the live request/response shapes, then implement
`HandleExtensionMethod` on `acpagent.session` (the ACP client), mapping
`_x.ai/exit_plan_mode` onto the existing permission request/respond flow and
`_x.ai/ask_user_question` onto the question flow, returning `NewMethodNotFound`
for anything else.

## Verification

- Unit: `acpagent` static-mode fallback and id validation; OpenCode mode mapping,
  hidden-agent filtering, and "the next prompt really carries `agent: plan`";
  daemon `/plan`, `/plan off`, `/mode`, soft-builtin precedence, and mode tracking
  from events; mobile hydrate + autocomplete gating + plan-mode chip.
- Live (run against the real CLIs at acceptance):
  - `go test -tags live_grok ./internal/provider/grok/ -run Mode -count=1` —
    grok advertises no modes of its own, honors `set_mode plan`, and confirms it
    with `current_mode_update`.
  - `go test -tags live_opencode ./internal/provider/opencode/ -run Mode -count=1` —
    modes are `build`/`plan`, hidden and subagent entries excluded, current
    `build`, and a switch to `plan` is accepted.
