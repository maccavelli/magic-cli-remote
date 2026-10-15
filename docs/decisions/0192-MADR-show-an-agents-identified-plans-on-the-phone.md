---
status: accepted
date: 2026-10-14
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Show an ACP agent's identified plans on the phone, and advertise the unstable `plan` capability

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 item 6 is "`plan_update` with `plan_removed`, then the unstable `plan` capability". The five
items before it were the session title, notices, cost, compaction and elicitation forms (0187–0191).

**What ACP defines** (`acp-go-sdk2@v0.17.1`). There are two plan models.

* **The stable plan**, `plan` (`SessionUpdatePlan`, `types_gen.go:12354-12369`), has no ID. It
  carries `entries`, and "the client replaces the entire plan with each update".
* **Identified plans**, which are **UNSTABLE** ("may be removed or changed at any point"):
  * `plan_update` (`SessionPlanUpdate`, `:12404-12419`) carries one plan's content, addressed by
    `planId`. The content is one of three variants (`PlanUpdateContent`, `:9403-9418`):
    * `items`: entries, replaced whole on each update;
    * `markdown`: raw Markdown text;
    * `file`: "A URI pointing to a file containing the plan".

    An unknown content type arrives as `Unknown`.
  * `plan_removed` (`SessionUpdatePlanRemoved`, `:12451-12466`) removes the plan with that
    `planId`.
  * The client capability `plan` (`PlanCapabilities`, `:8970-8982`; `ClientCapabilities.Plan`,
    `:1359-1367`) says the client "can receive both update types". Without it, "the client does not
    advertise support".
* **The SDK does not gate either update.** Its session-update gates cover notices, compaction and
  boolean config options (`types_gen.go:20122-20150`). An agent that sends `plan_update` or
  `plan_removed` without the advertisement reaches Magic's handler.

**What Magic does today.**

* `plan` maps to `event.TypePlan`, carrying the full entries (`internal/provider/acpagent/session.go`,
  the `u.Plan` case). OpenCode, kilo and codex send the same event. The phone keeps **one** plan
  per session (`SessionTranscript.plan`) and draws it in `WorkItemsPanel` above the composer
  (`features/chat/chat_screen.dart`).
* `plan_update` is declined by name (0184 A4).
* **`plan_removed` clears the stable plan, whatever plan ID it names** (the `u.PlanRemoved` case,
  pinned by `TestSessionUpdatePlanRemovedEmitsEmptyPlan`).
  * The mapping dates from 2026-07-21 (`48d81d9d`), when `plan_removed` was read as "clear the plan"
    ([0025-MADR-goose-provider.md](0025-MADR-goose-provider.md),
    [0038-MADR-grok-acp-parity-assessment.md](0038-MADR-grok-acp-parity-assessment.md)).
  * In schema 1.25 it removes one identified plan. So an agent that keeps a stable plan and also
    removes an identified one would see its stable plan wiped from the phone.
* Magic does not advertise `plan`. The honesty test lists it as a capability Magic must not
  advertise (`advertise_test.go`).

**Who would send one.** No agent Magic runs is known to.

* grok's committed captures contain no `plan`, `plan_update` or `plan_removed` update. grok plans
  through its own `_x.ai/exit_plan_mode` extension.
* gobble-cli contains no plan-update code (checked 2026-10-14).

**The question.** How does Magic show more than one identified plan, in three content shapes,
beside the stable one? How does it stop a removal clearing the wrong plan? And should it advertise
a capability ACP marks unstable?

## Decision Drivers

* Advertise only what Magic implements end to end (0184 D4), and D10's bar.
* Never let one plan's removal hide another.
* Keep the stable plan, which four providers use, exactly as it is.
* A file plan's URI points at the daemon's host. The phone must not be led into fetching agent-named
  files, and the daemon must not read them on the agent's say-so.
* Agent-supplied text is bounded.
* An unstable capability can change under an SDK bump. A change must fail a test, not reach a user
  silently.
* No new kind of UI where an existing idiom fits.

## Considered Options

* Carry identified plans beside the stable one, keyed by plan ID, in all three content shapes
* Fold identified item plans into the single stable plan, and decline Markdown and file plans
* Fix only `plan_removed`, keep declining `plan_update`, and do not advertise
* Leave everything as it is

## Decision Outcome

Chosen option: "Carry identified plans beside the stable one, keyed by plan ID, in all three content
shapes". It is the only option that both fixes the wrong clear and keeps every plan the agent
reports, without merging separate plans into one.

Under the chosen option:

* **D1. acpagent maps `plan_update` onto ~~the plan event~~ a new `identified_plan` event, with an
  identity and a shape** (amended 2026-10-14, see "Amendment 1" below).
  * ~~`event.TypePlan` gains:~~ `event.TypeIdentifiedPlan` (`identified_plan`) carries:
    * `plan_id`, ~~absent for the stable plan~~ always set;
    * `plan_kind` (`items`, `markdown` or `file`);
    * `plan_content`, the Markdown, capped at 32 KiB with `plan_truncated`;
    * `plan_uri`, the file plan's URI as sent.
  * `items` carry `entries` as the stable plan does.
  * An `Unknown` content type is declined as `plan_update.<type>`, and `_meta` as
    `plan_update._meta`.
* **D2. `plan_removed` removes only the plan it names.** It becomes ~~a plan event~~ an
  `identified_plan` event with that `plan_id` and `plan_removed: true`. The stable plan is never
  cleared by it. That corrects the 2026-07-21 mapping, and
  `TestSessionUpdatePlanRemovedEmitsEmptyPlan` is replaced.
* **D3. ~~The plan event keeps its delivery and retention.~~ The `identified_plan` event takes the
  plan event's delivery and retention.** It is must-deliver and in the progress class, as `plan` is.
  Each event restates one plan whole, so a later one for the same ID supersedes an earlier one.
  `plan` itself is unchanged.
* **D4. The phone keeps plans keyed by ID, the stable plan under the empty ID.**
  * Each plan shows in the panel above the composer, in the order it first appeared:
    * `items` as the existing task list (`WorkItemsPanel`);
    * `markdown` collapsed, rendered with the chat's Markdown renderer when opened;
    * `file` as its file name and full path, with a copy action. It is **never fetched**, neither by
      the phone nor by the daemon.
  * A removal drops only that ID.
  * ~~A phone without D4 ignores the new fields, and so treats an identified items plan as the stable
    plan. That is the behavior it has today, so nothing gets worse.~~ A phone without D4 ignores the
    `identified_plan` event, as it ignores every event type it does not know. Its stable plan is
    untouched, including by a removal. (Amendment 1: the struck sentence was false.)
* **D5. Magic advertises `plan` (`{}`)** in `defaultClientCapabilities`, and adds it to
  `implementedClientCapabilities`, only when D1–D4 ship. The honesty test keeps `elicitation.url` and
  `auth.terminal` as its dishonest examples.
* **D6. The capability's instability is guarded, not hidden.**
  * The A4 table already fails when the SDK adds or renames a session-update variant.
  * A new test pins the three content variants. An SDK bump that changes them fails there first.
  * The advertisement's comment names the capability as unstable.
* **D7. The probe.**
  * A zero-token run against grok records whether grok accepts the advertisement.
  * A run with model turns, which waits for the owner's quota, records whether grok then sends
    identified plans.

  The fixture is built from the schema unless a probe captures real frames.

### Consequences

* Good, because an agent's removal of one plan can no longer wipe another from the phone. That is a
  latent bug fixed whether or not anything is advertised.
* Good, because an agent's several plans, in any of the three shapes, reach the user.
* Good, because the stable plan and the four providers that send it are untouched.
* Good, because a file plan's path is shown without anything fetching it.
* Neutral, because no agent Magic runs sends identified plans yet.
* Bad, because the capability is unstable. An SDK bump may force changes, which the pinned tests
  will surface.
* Bad, because the phone's plan state grows from one list to a keyed set, with two new row shapes.
  Each needs widget tests.

### Confirmation

* An acpagent test sends `plan_update` in each shape, with `_meta`, with an unknown type, and with
  Markdown over the cap. It also sends `plan_removed`. It requires the events in D1–D2, and that a
  removal never clears the stable plan.
* A test pins `PlanUpdateContent`'s three variants (D6).
* Flutter widget tests:
  * the stable plan and two identified plans drawn together, in first-seen order;
  * a removal dropping only its plan;
  * Markdown collapsed until opened;
  * a file plan showing its path, with no network or file access.
* The A2 wire test pins `initialize` with `plan`, and the honesty test fails if `plan` is advertised
  without behavior.
* Each new test is seen failing first, and planted breaks are shown failing on a scratch copy.

## Pros and Cons of the Options

### Carry identified plans beside the stable one, keyed by plan ID, in all three content shapes

* Good, because nothing the agent reports is merged, dropped or misattributed.
* Neutral, because ~~the plan event gains five optional fields~~ the event package gains one type
  and six optional fields (Amendment 1).
* Bad, because it is the largest phone change of the options.

### Fold identified item plans into the single stable plan, and decline Markdown and file plans

* Good, because it needs no phone model change.
* Bad, because two plans overwrite each other in one panel, and a removal must guess which plan is
  showing.
* Bad, because declining two of the three shapes makes advertising `plan` dishonest, since it claims
  the client "can receive both update types".

### Fix only `plan_removed`, keep declining `plan_update`, and do not advertise

* Good, because it fixes the wrong clear at almost no cost, and stays clear of an unstable
  capability.
* Bad, because D10 asks for the capability, and the plans an agent reports still never reach the
  user.

### Leave everything as it is

* Good, because nothing changes.
* Bad, because a removal can still wipe the stable plan.

## More Information

* **Amendment 1, 2026-10-14: identified plans travel as their own event type.** Found at the start
  of P1, before any code was written.
  * `internal/event/event.go` says a `plan` event without `entries` means "clear the plan". The
    phone replaces its one plan with every `plan` event's entries
    (`apps/mobile/lib/data/chat/transcript_reducer.dart`), and reads nothing else from it.
  * So under D1 as first written, a phone without D4 would have wiped its stable plan on every
    Markdown plan, file plan and removal, and replaced it with any identified item plan. D4's "nothing
    gets worse" was false. The daemon and the phone update separately, so a newer daemon with an
    older phone is a normal case.
  * The phone's event switch ignores a type it does not know (`default: break`), and there is no
    client feature negotiation that could gate new fields instead.
  * The owner chose a new event type, `identified_plan`, over gating the new fields by a new
    handshake field. `plan` is unchanged. D1–D4 are amended in place, with the old text struck.
* The facts above were read from the code on 2026-10-14, at `5a5d2eed`, from
  `acp-go-sdk2@v0.17.1`, and from gobble-cli.
* The PLAN is
  [0192-PLAN-show-an-agents-identified-plans-on-the-phone.md](0192-PLAN-show-an-agents-identified-plans-on-the-phone.md).
  ~~Nothing is implemented until the owner accepts an option and approves the PLAN.~~ On 2026-10-14
  the owner accepted the recommended option and approved P1 and P2.
