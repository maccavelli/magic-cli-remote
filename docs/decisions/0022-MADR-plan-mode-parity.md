# MADR 0022: Plan mode parity (`/plan`) across providers

- **Status**: **Accepted** — Phase 1 implemented on `master` (2026-07-25);
  Phase 2 (grok plan-approval hand-off) implemented 2026-07-25.
- **Date**: 2026-07-25
- **Deciders**: Project Owner (command surface, phasing); Implementer
  (daemon/providers/mobile)
- **Related**:
  - [protocol-v1.md](./protocol-v1.md) — `session_mode` event, `session.set_mode`,
    built-in slash commands
  - [MADR 0020](./0020-MADR-opencode-session-tree.md) — OpenCode agents (`agents.list`,
    `StartOptions.Agent`), slash commands (Sprint 5)
  - [MADR 0004](./MADR-phase2-grok-acp.md) — grok ACP transport

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

## Phase 2: grok's plan-approval hand-off

When grok's model finishes planning it calls its `exit_plan_mode` tool, and the
shell sends the **client** an ACP extension request `_x.ai/exit_plan_mode`
expecting a plan-approval answer. Because we implemented no
`acp.ExtensionMethodHandler`, the SDK answered method-not-found: plan mode was
enterable and usable, but the approval step at the end of it always failed. The
same held for `_x.ai/ask_user_question`, the tool grok reaches for when it needs
a clarification mid-turn — including, immediately, "what would you like to change
in the plan?".

### The schema (probed, not documented)

Neither method is in the ACP SDK, in grok's embedded docs, or in any published
reference. Both shapes below were established against **grok 0.2.112** by
driving a real plan turn over ACP stdio and reading what came back. Two findings
made the probing necessary rather than optional:

1. **A wrong reply to `_x.ai/exit_plan_mode` fails silently.** The shell has no
   parse-error path for the response: an unknown outcome, a missing field, or a
   malformed body all land in the same fallback as "the user wants to revise the
   plan". `{"decision":"approved"}` — a perfectly plausible guess — is
   indistinguishable from the user pressing *request changes*.
2. **`_x.ai/ask_user_question`, by contrast, reports its serde errors in the tool
   result.** That is what pinned the vocabulary: handed a bogus outcome it
   answered `unknown variant '__probe__', expected one of 'accepted',
   'chat_about_this', 'skip_interview', 'cancelled'`.

**`_x.ai/exit_plan_mode`** — request:

```json
{"sessionId": "…", "toolCallId": "call-…-4", "planContent": "# Plan\n\n…"}
```

`planContent` is `null` when the model left plan mode without writing (or with an
unreadable) `plan.md`; grok still expects an answer, and the user can still
approve. Response:

```json
{"outcome": "approved" | "abandoned" | <anything else>, "feedback": "…"}
```

| outcome | what the shell does |
|---|---|
| `approved` | plan mode exits; the model is told "Your plan has been approved. You can now start coding." |
| `abandoned` | plan mode is turned **off** and the model is told not to re-enter it unless asked |
| anything else | "The user wants to revise the plan"; plan mode **stays active**; a `feedback` string is quoted to the model as "The user said: …" |

Unknown extra fields are ignored.

**`_x.ai/ask_user_question`** — request:

```json
{"sessionId": "…", "toolCallId": "…", "mode": "plan",
 "questions": [{"question": "…",
                "options": [{"label": "…", "description": "…"}],
                "multiSelect": null}]}
```

Response — `accepted` additionally *requires* `answers`, keyed by the **question
text** (grok mirrors the Claude Code tool shape here), one string per question:

```json
{"outcome": "accepted", "answers": {"Which greeting style do you prefer?": "Hello, X!"}}
```

### Decision

**Implement `HandleExtensionMethod` on `acpagent.session` and map both requests
onto flows the daemon and the phone already have** (`internal/provider/acpagent/extensions.go`):

1. **Plan approval → the permission flow.** The plan arrives as an ordinary
   `permission_request` carrying the plan markdown as its detail and three
   options (`plan_approve` / `plan_changes` / `plan_abandon`, with ACP kinds
   `allow_once` / `reject_once` / `reject_always`). The client answers with the
   existing `permission.respond`, and the handler translates the option id into
   the wire outcome. No new protocol message, no client-side plan concept.
2. **Questions → the question flow.** `question_request` /
   `question.respond` / `question_resolved`, the path built for OpenCode's
   questions, with the option *label* doubling as the option id because the label
   is what grok's wire expects back. `acpagent.session` now implements
   `provider.QuestionSession`.
3. **Everything else keeps answering method-not-found**, which is what the SDK
   did before — so a grok that adds an extension we have not built stays
   diagnosable instead of looking like a hang.
4. **The revise outcome is spelled out (`"changes"`), not left to the fallback.**
   Relying on "anything grok does not recognise means revise" would make a future
   grok that validates outcomes fail in the direction we cannot see.

Both requests share one wait discipline with `session/request_permission`
(`awaitDecision` / `awaitAnswers`): one client answer path, one
`PermissionTimeout` safety valve, one single-winner latch, and release on
`Cancel`/`Close` — so a plan approval cannot outlive its session or strand the
agent.

### Consequences

- `/plan` on grok is now a complete loop: enter plan mode, plan, approve on the
  phone, implement. Before this the last step failed every time.
- **A dismissed approval means "revise", never "approve" and never "abandon".**
  Cancelling the turn, closing the session, or letting the request time out all
  answer `changes` with a feedback line saying so. Approving on the user's behalf
  would let an agent edit files nobody cleared; abandoning would throw away
  planning work. Revise is the only outcome that changes nothing.
- **`AlwaysApprove` does not auto-approve a plan**, deliberately. It is a
  tool-permission setting, and grok itself keeps plan mode armed underneath
  always-approve ("file edits are blocked until you approve exiting plan mode"),
  so auto-approving would silently defeat the mode the user asked for. This
  matches the existing rule that always-approve does not auto-answer questions.
- The approval sheet's detail box was sized for a shell command (160px of
  monospace). A plan is the one case where the detail *is* the content, so a long
  detail now gets ~42% of the screen height; short ones are unchanged.
- Plan markdown is capped at 8000 bytes in the event. The plan file itself stays
  on the agent host; the phone shows an excerpt with a truncation note.

### Known limitations (phase 2)

- **"Request changes" carries no free text.** The permission flow has one option
  id and no message field, so the feedback we send says the user wants changes
  but did not say what. grok's own next move is to ask with
  `ask_user_question` — which we now answer — so the loop closes, at the cost of
  one extra round trip. A free-text reply on the permission path would remove it.
- **Multi-select answers are joined with ", "** into the single string grok
  echoes to the model. grok accepts a list too, but the string is what it renders
  verbatim.
- **`_x.ai/session_notification` → `interaction_resolved` is ignored.** grok
  sends it when an interaction is settled elsewhere (its own TUI attached to the
  same leader). A plan approval answered in grok's terminal will therefore linger
  on the phone until the session ends. Handling it needs the notification path,
  not the request path.
- **Per-option descriptions are dropped.** grok sends a description per question
  option; the question card has no field for it yet, same as the OpenCode
  question path.

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
