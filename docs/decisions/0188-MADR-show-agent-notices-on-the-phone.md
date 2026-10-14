---
status: proposed
date: 2026-10-13
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Show an ACP agent's `notice` updates on the phone, and advertise `session.notices`

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 takes up ACP capabilities one record at a time. The session title was first
([0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md)).
`notice`, with the client capability `session.notices`, is second.

**What ACP defines.** A `notice` session update is "information for the user that is not part of
session history". It carries:

* `severity`: `info`, `warning` or `error`;
* a required, non-empty `title` "that can stand alone";
* an optional `description`;
* an optional `_meta`.

"Agents MUST only send this update when the Client advertised `ClientSessionCapabilities::notices`"
(`acp-go-sdk2@v0.17.1`, `types_gen.go:8801-8806` and `:12715-12754`).

**What happens today.** Magic does not advertise `session.notices`
(`defaultClientCapabilities`, `internal/provider/acpagent/acpagent.go:1074`). A conforming agent
therefore sends no notices, and the SDK refuses one that arrives anyway, before Magic's handler runs,
with "capability session.notices not advertised: variant notice" (`types_gen.go:20122-20129`).
0184's A4 declines the variant by name (`internal/provider/acpagent/session.go`, the `u.Notice`
case), but no agent can reach that case today. The capability-honesty test even uses
`session.notices` as its example of a capability Magic must *not* advertise
(`internal/provider/acpagent/advertise_test.go:61`).

**Who would send one.** No agent Magic runs is known to. grok has never been offered the
capability, so its behavior when offered it is unmeasured. gobble-cli contains no notice code
(checked 2026-10-13).

**What Magic and the phone have.**

* `event.TypeNotice` (`notice`) is a *daemon*-originated system line, for example the output of
  `/help`. It carries text only, no severity, and it is kept in Magic's history. The manager drops a
  notice identical to the one before it (MADR 0137 F6a).
* The phone appends a `notice` to the transcript as a neutral system line
  (`apps/mobile/lib/data/chat/transcript_reducer.dart:203-207`).
* Agent-originated warnings already have a precedent. Codex's `codex_warning` is a bounded event that
  is kept in history and drawn as a transcript card (`transcript_reducer.dart:184-190`), so a phone
  that was asleep sees it on reconnect.
* The phone also has a transient top banner, `theme/top_notification.dart`, with severities `info`,
  `success` and `error` but no `warning`. Its own comment says "nothing in the app keeps a
  notification history". It is used today for the phone's local feedback only, never for an event.

**The question.** Where does an agent's notice go: into the transcript, where it is kept and
replayed to a phone that reconnects, or into a transient banner, which matches ACP's "not part of
session history" but is lost when no phone is watching? And does Magic advertise `session.notices` at
all, when no agent it runs is known to send one?

## Decision Drivers

* Advertise only what Magic implements end to end. That is 0184's D4 and its honesty test.
* D10's bar for advertising: daemon and phone behavior, a wire fixture, and a live probe.
* A phone is often asleep or backgrounded. A message the user never sees is worth little.
* ACP's own wording: a notice is not part of *session history*. That means the agent's replayable
  conversation, which `session/load` reproduces.
* Keep "the daemon said" separate from "the agent said". Today's `notice` is the daemon's.
* Severity must survive to the screen. A `warning` must not look like an `info`.
* No new kind of UI where an existing idiom fits.

## Considered Options

* A new `agent_notice` event, kept in Magic's history and drawn as a transcript row with its severity
* A new `agent_notice` event, delivered live only and shown as a transient top banner
* Map the notice onto the existing `notice` event (title and description as text, severity dropped)
* Keep declining, and do not advertise `session.notices`

## Decision Outcome

Chosen option: none yet, because this record is `proposed`. Recommended: "A new `agent_notice`
event, kept in Magic's history and drawn as a transcript row with its severity". It is the only
option that both keeps the severity and reaches a phone that was not watching. It follows the
precedent of `codex_warning`, not the phone's banner, which no event uses.

On ACP's wording: Magic's history is Magic's own record for its clients. It is not the agent's
session history, and `session/load` never replays a notice into it, so ACP's rule holds.

Under the recommended option:

* **D1 — acpagent maps `notice` to a new event `agent_notice`.** It carries `severity` (`info`,
  `warning`, `error`), `title`, and `description` when present.
  * An unknown severity is passed through as text. ACP's severities are an open string type, as
    0184's A5 recorded, and the phone treats an unknown one as `info`.
  * A blank title cannot reach Magic: the SDK's `minLength` rule refuses it.
  * `_meta` is not used, and is declined by name once per session, as 0184's D7 does.
* **D2 — `agent_notice` is a must-deliver event, kept in Magic's history.** A phone that reconnects
  sees it. A notice identical to the one before it is dropped, as the daemon's `notice` is.
* **D3 — the phone draws `agent_notice` as a transcript row with its severity.** The row reuses the
  transcript's status-row idiom, the left accent rail its system rows already use. `warning` and
  `error` are visibly distinct from `info`. The title is the row's text, and the description is
  shown under it, clipped. A phone that does not know `agent_notice` ignores it, as it ignores any
  unknown event.
* **D4 — Magic advertises `session.notices` (`{}`) in `defaultClientCapabilities`,** and adds it to
  `implementedClientCapabilities`, only when D1–D3 ship.
  * The honesty test's dishonest example moves to a capability Magic still lacks,
    `session.compaction`.
  * grok's `Spec` uses the default, so grok is offered notices.
  * gobble's capabilities are
    [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md)'s to set.
* **D5 — the probe.** With notices advertised:
  * a zero-token run (`initialize` and `session/new` only) against grok records whether grok sends
    any notice at session start, for example about an MCP server;
  * a run with model turns waits for the owner's quota.

  If grok sends notices, the frames become the wire fixture. If it sends none, the fixture is a frame
  built from the schema, and the record says so.

### Consequences

* Good, because an agent's warning or error reaches the user with its severity, whether or not the
  phone was watching when it arrived.
* Good, because Magic's advertisement stays honest. The capability is offered only once it is
  implemented end to end.
* Good, because "the agent said" stays distinct from "the daemon said".
* Neutral, because no agent Magic runs is known to send notices yet. The work makes Magic ready
  rather than fixing a visible gap.
* Neutral, because the phone gains one event case and one row variant. It needs a widget test and
  `make preflight`.
* Bad, because advertising a capability changes what grok may send. An agent that starts sending
  notices it previously routed elsewhere, such as grok's `_x.ai/mcp/server_status`, could show the
  user the same news twice. The probe in D5 is there to find that before acceptance.

### Confirmation

* An acpagent test delivers a `notice` of each severity, with and without a description. It
  requires an `agent_notice` event with the same fields, and a declined `_meta`.
* A manager test requires `agent_notice` to be kept in history and deduplicated against the
  previous identical one.
* A Flutter widget test draws each severity, and shows that `warning` and `error` differ from
  `info`.
* The A2 wire test pins the serialized `initialize` frame, now with `session.notices`. The
  honesty test passes with notices implemented, and fails if notices are advertised without being
  implemented.
* Each new test is seen failing first, on a scratch copy.
* D5's probe, with its fixture committed beside the existing grok captures.

## Pros and Cons of the Options

### A new `agent_notice` event, kept in Magic's history and drawn as a transcript row with its severity

* Good, because the notice survives a phone that was asleep, as `codex_warning` does.
* Good, because severity is kept, and the event is distinct from the daemon's own `notice`.
* Neutral, because ACP's "not part of session history" is met only in ACP's sense: the agent's
  replayable conversation. Magic still keeps the notice in its own transcript.
* Bad, because it needs a phone change (one event case, one row variant) and a widget test.

### A new `agent_notice` event, delivered live only and shown as a transient top banner

* Good, because it matches ACP's wording most literally, since nothing is stored.
* Bad, because a notice that arrives while the phone is asleep or backgrounded is lost. The banner's
  own comment says nothing keeps a notification history.
* Bad, because the banner has no `warning` severity, and no event drives it today. Both would be new.

### Map the notice onto the existing `notice` event

* Good, because it needs no phone change at all.
* Bad, because severity is lost, so an agent's `error` looks like `/help` output.
* Bad, because the daemon's messages and the agent's become indistinguishable.

### Keep declining, and do not advertise `session.notices`

* Good, because nothing changes, and no agent Magic runs is known to send notices.
* Bad, because an agent that would tell the user something has no way to, since conforming agents
  send notices only when advertised.

## More Information

* The facts above were read from the code on 2026-10-13, at `145713d7`, and from
  `acp-go-sdk2@v0.17.1`.
* The PLAN is [0188-PLAN-show-agent-notices-on-the-phone.md](0188-PLAN-show-agent-notices-on-the-phone.md).
  Nothing is implemented until the owner accepts an option and approves the PLAN.
* grok's quota was exhausted on 2026-10-13. D5's zero-token run spends no model tokens; anything
  more waits for the reset.
