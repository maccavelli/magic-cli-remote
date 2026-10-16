---
status: proposed
date: 2026-10-16
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Defer ACP subagent sessions until an engine Magic runs sends them, and pin that with a live test

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 item 7 is "the unstable subagent variants, mapped onto Magic's own subagent model". It is the
last item on that list. Items 1–6 are 0187–0192. D10's rule is that a capability "is advertised
only when the daemon and phone behavior, the wire fixtures and a live probe exist".

### What ACP defines

The SDK (`acp-go-sdk2 v1.14.0`, `types_gen.go`) and the upstream RFD
(<https://agentclientprotocol.com/rfds/subagents>, read 2026-10-16) define the following. All of it
is marked **UNSTABLE**.

* **The client capability `subagents`** (`ClientCapabilities.Subagents`, `:1365-1375`). `{}` means
  the client "understands child associations, work-state snapshots, session-directed messages, and
  restricted-session semantics". There is no agent-side capability. The RFD: an agent "MUST NOT
  send the updates defined by this RFD unless the Client advertised `subagents`".
* **`subagent_update`** (`SessionSubagentUpdate`, `:12830-12866`). Sent on the parent's session. It
  announces or patches a child: `sessionId` (the child), and the patch fields `title`,
  `description`, `capabilities` (only `cancel` is defined) and `state`.
* **`state`** (`StateUpdate`, `:14492`) is a work-state snapshot, not a lifecycle: `running`,
  `idle` (with an optional `stopReason`, including `cancelled` and `error`), `requires_action`,
  `unknown`, or an open `Other`. The RFD: "Idle does not terminate the child; the parent can
  message it again". Children are reusable across the parent's turns.
* **`session_message` and `session_message_chunk`** (`SessionMessage`, `:11695`): messages between
  sessions, with sender and recipient session IDs. They are "not responses addressed to the human
  user and not tool calls".
* **What advertising obliges a client to do.** The RFD's main client duties are these.
  * Child events arrive automatically, addressed by the child's session ID. Permission,
    elicitation, filesystem and terminal requests can come from a child. "Requests remain
    attributed to their original child session and MUST NOT move to the parent".
  * Restricted sessions: the client must never prompt, load, resume or close a child, and may
    cancel one only if that child's `capabilities.cancel` is set.
  * When the client cancels a session, it must answer that session's pending permission requests
    with `cancelled`. It cleans up a descendant's requests once the descendant reports
    `idle`/`cancelled`.
  * After connection loss or a reload, a last-known `running` state must not be shown as live, and
    cached child capabilities are invalidated.
  * Parent and child costs must never be added together.
* **ACP v2.** "v2 Clients MUST understand `subagent_update` … cannot merely ignore the
  announcement". 0184 D13 does not track v2, so this applies only when Magic follows v2.

### What Magic does today

* **Magic's own subagent model** is `event.TypeSubagents` (`internal/event/event.go:110-114`,
  `:433-456`). It is a replace-whole roster of `SubagentInfo{ID, Name, Task, Status}`. The status is
  `running`, `completed` or `failed`, and a finished entry never reopens. The roster is cleared at
  the parent's turn end (`internal/provider/acpagent/subagents.go`, `clearSubagents`). It is fed by
  grok's vendor channel `_x.ai/session_notification` (`subagent_spawned`, `subagent_progress`,
  `subagent_finished`; MADR 0051), and by codex and OpenCode through their own transports.
* **The ACP variants are declined.** `subagent_update`, `session_message` and
  `session_message_chunk` go to `declineUpdate` (`internal/provider/acpagent/session.go:1981-1986`),
  pinned by `sessionupdate_test.go:90-92`. A `session/update` whose session ID is not the live
  session's is dropped as a child-session update (`session.go:1748-1754`).
* **`subagents` is not advertised.** It is not in `implementedClientCapabilities`
  (`internal/provider/acpagent/acpagent.go:1119-1128`), and `CheckClientCapabilities` reports any
  capability advertised without its behavior.
* **Permission requests carry no session check** (`session.go:2326`). A child's request would show
  as the parent's, which the RFD forbids once `subagents` is advertised.

### What the engines send

Probed 2026-10-16, without spending quota, by counting the wire strings in each installed engine's
files:

| Engine | `subagent_update` | `session_message_chunk` | Magic's transport |
| :--- | ---: | ---: | :--- |
| grok 1.0.50 (`~/.grok/bin/grok-1.0.50`, 160 MiB) | 0 | 0 | ACP through `acpagent`; has its own `subagent_spawned` (1) |
| goose (260 MiB) | 0 | 0 | not an `acpagent` provider |
| opencode (137 MiB) | 0 | 0 | its own HTTP API |
| kilo (`@kilocode/cli`, all files) | 0 | 0 | its own HTTP API |
| gobble-cli source (`*.go`) | 0 | 0 | not yet registered |

grok is the only provider on `acpagent` (`internal/provider/grok/grok.go:49`, `:140`). The
string `subagent_update` appears in none of them. A string's presence would not prove an engine
sends the update, but its absence is strong evidence that none can.

### The SDK does not gate these variants

`SessionUpdate.gates` (`types_gen.go:20177-20203`) refuses `notice` without `session.notices`, and
compaction without `session.compaction`. It does not refuse `subagent_update` or
`session_message*` without `subagents`, although the RFD says an agent must not send them. Magic
declines them anyway, so nothing breaks. This is an SDK inconsistency, outside this record.

### The question

What should Magic do with ACP's subagent sessions when no engine it runs sends them?

## Decision Drivers

* D10's rule: advertise only with daemon and phone behavior, wire fixtures and a live probe.
* Advertising is a promise: the RFD's client duties (attribution, restricted sessions, cancellation
  clean-up) would be in force from the first `initialize`.
* No code that cannot run. An unadvertised handler is never reached by a conforming agent.
* The adoption trigger must be detected, not remembered.
* The design work the trigger will need (state mapping, reusable children, attribution) should be
  recorded now, while the RFD has been read.

## Considered Options

* Defer: keep declining, record the mapping questions, and pin "no engine sends it" with a live test
* Implement the mapping now, without advertising
* Advertise and implement the client contract now
* Decline permanently, and rely on vendor channels

## Decision Outcome

Chosen option: "Defer: keep declining, record the mapping questions, and pin 'no engine sends it'
with a live test". It is the only option that keeps D10's rule and makes no promise Magic cannot
test, and the live test turns the trigger into a failing check instead of a memory.

* **S1. Not advertised, still declined.** `subagents` stays out of `implementedClientCapabilities`.
  The three variants stay declined, as `sessionupdate_test.go` pins. No product code changes.
  `TestCapabilityHonesty` gains a `subagents` example, so advertising it without behavior fails a
  test by name. Today no example names it, although `CheckClientCapabilities` would catch it.
* **S2. A live test pins the premise.** A new `live_grok` test, `TestLiveGrokShipsNoSubagentSessions`,
  resolves the `grok` on `PATH` to its file and fails if that file contains `subagent_update`. It
  spends no tokens and starts no process. When it fails, grok has shipped support, and this record
  is revisited.
* **S3. The trigger.** This record is reopened when either of these happens:
  * S2 fails, because a grok release contains `subagent_update`;
  * gobble is registered as a provider and implements it.

  Adopting then means a new pair, with the wire capture, the phone design and a live probe that D10
  requires.
* **S4. What that pair must decide.** Recorded now so it starts informed. None of it is decided
  here.
  * **Roster semantics.** Magic's roster is terminal (`completed`/`failed`, never reopened) and
    cleared at the parent's turn end. ACP children are reusable, and `idle` is not an outcome.
    Either the roster gains `idle`, `needs input` and `unknown` and stops clearing at turn end, or
    the mapping folds them, for example `idle`+`end_turn` → completed and `idle`+`error` → failed.
    Folding loses reuse.
  * **`unknown` and connection loss.** The RFD forbids showing a last-known `running` as live, so
    the phone needs a state for "not confirmed".
  * **Attribution.** Permission, elicitation, filesystem and terminal requests need a check on
    their session ID, and the phone must show which child is asking.
  * **Per-child cancel.** Whether the phone offers it when `capabilities.cancel` is set, and the
    clean-up of pending requests that follows.
  * **Child transcripts.** MADR 0051 keeps subagent content out of the parent's transcript. Child
    `session/update` frames and `session_message*` either stay dropped, or get a per-child view.
  * **Cost.** Parent and child costs are shown separately, never summed.
  * **The vendor channel.** Whether grok's `_x.ai/session_notification` handling is retired once
    grok sends the standard update, as 0187–0192 did for their items.
* **S5. ACP v2.** When Magic follows v2 (0184 D13), subagent support stops being optional. That
  record must include S4's work.

### Consequences

* Good, because Magic promises nothing it has not built and tested.
* Good, because the trigger is a failing test in `make live-grok`, not a note someone must re-read.
* Good, because S4 keeps the RFD's reading, so the adopting record does not start from zero.
* Neutral, because D10's list ends with this item deferred, not done.
* Bad, because S2 is a string scan. A grok that sent the update under another encoding would pass
  it. A real probe needs a model turn that spawns a subagent, which spends quota, and is useless
  until Magic advertises the capability.
* Bad, because S2 covers grok only. gobble's adoption is caught by its own records, not by a test
  here.

### Confirmation

* `TestLiveGrokShipsNoSubagentSessions` passes against grok 1.0.50, with the count logged.
* On a scratch copy, it fails when pointed at a file that contains `subagent_update`, naming the
  file.
* `TestCapabilityHonesty`'s new `subagents` example fails on a scratch copy that marks
  `subagents` implemented.
* `TestEverySessionUpdateIsMappedOrDeclined` still declines the three variants, and
  `TestInitializeFrameIsPinned` still holds the advertised set. These are existing tests, so they
  are re-run, not added.

## Pros and Cons of the Options

### Defer: keep declining, record the mapping questions, and pin "no engine sends it" with a live test

* Good, because it follows D10, and it costs one small test.
* Good, because the next record inherits a concrete question list.
* Bad, because the live test is static evidence, not a wire capture.

### Implement the mapping now, without advertising

* Good, because the code would be ready when an engine ships.
* Bad, because a conforming agent never sends the update unadvertised, so the code never runs. Its
  tests would test a fixture of a wire format no engine produces.
* Bad, because S4's roster question changes the phone, and the right answer depends on what the
  first real engine sends.

### Advertise and implement the client contract now

* Good, because Magic would be ready for any engine at once.
* Bad, because no live probe is possible, so it breaks D10's rule.
* Bad, because the RFD's duties (attribution, restricted sessions, cancellation clean-up) would
  be promised from the first `initialize`, with no engine to check them against.

### Decline permanently, and rely on vendor channels

* Good, because it costs nothing.
* Bad, because grok's vendor channel may be replaced by the standard update, as other `_x.ai`
  features were (0187–0192). Magic would then lose its subagent roster.
* Bad, because ACP v2 makes support mandatory.

## More Information

* The RFD is "Subagent Sessions" (authors Vadim Briliantov and Ben Brandt), at
  <https://agentclientprotocol.com/rfds/subagents>. Its sections "Restricted session methods",
  "Cancellation and resource ownership" and "Pending permission and elicitation requests" are the
  client duties S4 summarises.
* MADR 0051 is the roster's origin and the rule that subagent content stays out of the
  transcript. 0187–0192 are D10's items 1–6.
* The SDK gap (no gate on `subagent_update` or `session_message*`) could be filed as an
  `acp-go-sdk2` issue, as #7 was. That is the owner's call.
* The PLAN is
  [0195-PLAN-defer-acp-subagent-sessions-until-an-engine-sends-them.md](0195-PLAN-defer-acp-subagent-sessions-until-an-engine-sends-them.md).
  Nothing is implemented until the owner accepts an option and approves the PLAN.
