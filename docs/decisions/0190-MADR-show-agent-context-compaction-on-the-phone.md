---
status: accepted
date: 2026-10-14
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Show an ACP agent's context compaction on the phone, and advertise `session.compaction`

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 takes up ACP capabilities one record at a time. Before this one came the session title
([0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md)),
notices
([0188-MADR-show-agent-notices-on-the-phone.md](0188-MADR-show-agent-notices-on-the-phone.md))
and cost
([0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)).
Compaction, with the client capability `session.compaction`, is fourth.

**What ACP defines** (`acp-go-sdk2@v0.17.1`):

* `compaction_update` is an upsert addressed by ID (`types_gen.go:12773-12789`):
  * `compactionId`: the agent's ID, unique within the session;
  * `status`: `in_progress`, `completed`, `failed` or `cancelled` (`:1908-1916`);
  * an optional `summary`, which is the "complete replacement user-displayable summary content", as
    content blocks;
  * an optional `error` string;
  * an optional `_meta`.

  "The first notification fixes the compaction's timeline position. Later updates with the same ID
  patch that entity in place." `summary`, `error` and `_meta` "have patch semantics: omission
  leaves the stored value unchanged, 'null' clears it, and a concrete value replaces it"
  (`:1961-1967`).
* **The SDK cannot show Magic a `null`.** `orDefault` and `skipInvalid` decode an omitted field and
  a `null` to the same `nil` (`types_gen.go:19722-19758`).
  * So `error: null` and an omitted `error` are indistinguishable in `SessionCompactionUpdate`.
  * `summary: null` is indistinguishable from an omitted `summary`. Only `summary: []`, an empty but
    non-nil slice, can be told apart, and it clears.
  * An agent that clears an error or a summary with `null` cannot be followed through the typed
    API.
* `compaction_summary_chunk` appends one content block to a compaction's summary
  (`:12829-12841`, `:1918-1929`). "A first-seen ID creates an in-progress compaction. Chunks append
  in receive order."
* Both are gated. "Agents MUST only send this update when the Client advertised
  `ClientSessionCapabilities::compaction`", and the SDK refuses either variant without it
  (`types_gen.go:20130-20143`).
* The capability is `{}`, and "'{}' advertises the complete compaction contract" (`:1780-1782`).
* ACP has **no method for a client to request a compaction.** Compaction is the agent's to start,
  whether it does so on its own or when asked through the agent's own `/compact` command. These
  updates only report it.

**What Magic does today.**

* acpagent declines both variants by name (`internal/provider/acpagent/session.go`, the
  `u.CompactionUpdate` and `u.CompactionSummaryChunk` cases). The A4 table pins that.
* Magic does not advertise `session.compaction`. The honesty test uses it as one of four examples
  of a capability Magic must not advertise (`internal/provider/acpagent/advertise_test.go:62`).
  `elicitation`, `plan` and `auth.terminal` are the other three.
* The other providers report a finished compaction as a daemon `notice`, a neutral system line with
  no summary:
  * OpenCode's `session.compacted` becomes "Conversation compacted — earlier turns were summarised
    to free context." (`internal/provider/opencode/http.go:1527-1533`);
  * codex's `contextCompaction` item becomes "Context compacted" (`internal/provider/codex/items.go:106-109`).
* `/compact` on a provider with `CompactSession` emits the daemon notices "Compacting the
  conversation…" and, on error, "Compaction failed: …" (`internal/session/commands.go:456-474`).
* After a compaction, the context-pressure notice re-arms when usage drops (MADR 0138 T1). That
  needs nothing new.

**Who would send it.** No agent Magic runs is known to.

* grok has never been offered the capability.
  * grok's command table maps `/compact` to `KindNone`: "grok compacts only in its own terminal UI"
    (`internal/provider/grok/commandtable.go:37-40`).
  * acpagent nonetheless implements `CompactSession` over `x.ai/compact_conversation`
    (`internal/provider/acpagent/sessioncaps.go:239-254`). Earlier probes found that
    `_x.ai/compact_conversation` "did not return within 12s"
    ([0106-MADR-grok-1.0.5-surface-parity.md](0106-MADR-grok-1.0.5-surface-parity.md)).
  * [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) F9 already
    records the mismatch between the table and 0138's acceptance item 9. This record does not
    settle it.
* gobble-cli compacts internally, with its own `compaction` package. It reports a compaction only
  through a following `usage_update`, and it contains no `CompactionUpdate` code (checked
  2026-10-14).

**The question.** When an agent reports a compaction, with a status and possibly a summary of what
it kept, what does Magic show? Does it advertise `session.compaction` at all, when no agent it runs
is known to send one?

## Decision Drivers

* Advertise only what Magic implements end to end (0184 D4). Here that is "the complete compaction
  contract": the four statuses, a summary that is replaced, and a summary that is appended to.
* D10's bar: daemon and phone behavior, a wire fixture, and a live probe.
* A compaction changes what the agent remembers. The user should be able to see that it happened,
  and what was kept.
* Several updates for one compaction must read as one thing, not as a run of separate lines.
* Agent-supplied text is bounded. A summary can be long.
* Replay and a reconnecting phone must arrive at the same state as a phone that watched live.
* Keep "the daemon said" separate from "the agent said", as 0188 did.
* No new kind of UI where an existing idiom fits.

## Considered Options

* A new `compaction` event, folded by ID, drawn as one transcript row with its status and a collapsed summary
* Map each status to a daemon `notice` line, and drop the summary
* Show the summary as an assistant message
* Keep declining, and do not advertise `session.compaction`

## Decision Outcome

Chosen option: "A new `compaction` event, folded by ID, drawn as one transcript row with its status
and a collapsed summary", because it is the only option that honors the whole contract, including
the summary, while showing one compaction as one row. The owner accepted it on 2026-10-14 ("accept
option 1, proceed to k1-k3").

Under the chosen option:

* **D1. acpagent folds the two variants into a `compaction` event per ID.**
  * The fields are `compaction_id`, `compaction_status`, `compaction_summary` (text),
    `compaction_error`, and `compaction_truncated`.
  * acpagent keeps the current state of each ID it has seen, and applies ACP's patch semantics as
    far as the SDK lets it see them:
    * A `compaction_update` sets the status.
    * A non-nil `summary` replaces the accumulated one, and an empty one clears it. A non-nil
      `error` replaces the stored one.
    * A `nil` field, whether it was omitted or `null`, leaves the stored value unchanged. That is
      ACP's omission rule; ACP's `null`-clears rule is out of reach (see the Context).
    * A chunk appends to the summary. A chunk for an ID not seen before creates an `in_progress`
      compaction.
  * It **emits** the full current state:
    * on every `compaction_update`;
    * on the first chunk for a new ID.

    Further chunks accumulate without an event. So a streamed summary costs one event at its start
    and one at its end, not one per chunk.
  * Bounds:
    * A summary is capped at 32 KiB. Beyond that, `compaction_truncated` is set and text stops
      accumulating.
    * At most 16 IDs are tracked per session; the oldest is forgotten first. A forgotten ID that
      reappears starts over.
  * Text blocks are joined. A non-text block is skipped and declined by name, as
    `compaction.summary.<type>`, once per session.
  * `_meta`, on either variant, is declined by name.
  * A status outside the four is passed through as text. ACP's enums are open (0184 A5).
* **D2. `compaction` is a must-deliver event, kept in Magic's history** as content, like 0188's
  `agent_notice`. History holds every emitted state. A client folds them by ID, and the last state
  wins. An identical consecutive state for the same ID is dropped.
* **D3. The phone draws one row per compaction ID, updated in place.**
  * The row keeps the position of the compaction's first event, as ACP's "fixes the compaction's
    timeline position" asks. Later states change it in place.
  * The row uses the status-row idiom with a left accent rail, as `agent_notice` does.
  * The error is drawn only while the status is `failed`. An error the agent meant to clear with
    `null`, which Magic cannot see, therefore never shows under a later `completed`.
  * Its line depends on the status:
    * `in_progress`: "Compacting the conversation…";
    * `completed`: "Conversation compacted";
    * `failed`: "Compaction failed", with the error under it;
    * `cancelled`: "Compaction cancelled".

    An unknown status reads as in progress.
  * A non-empty summary is shown collapsed, behind "What the agent kept", and expands in place. A
    truncated summary says so.
  * A phone that does not know `compaction` ignores it, as it ignores any unknown event.
* **D4. Magic advertises `session.compaction` (`{}`) in `defaultClientCapabilities`,** and adds it
  to `implementedClientCapabilities`, only when D1–D3 ship.
  * The honesty test keeps its other three dishonest examples.
  * grok's `Spec` uses the default, so grok is offered compaction.
  * gobble's capabilities are 0179's to set.
* **D5. The probe.** With compaction advertised:
  * a zero-token run (`initialize` and `session/new`) against grok records whether grok accepts the
    advertisement;
  * a run with model turns, which waits for the owner's quota, records whether grok sends compaction
    updates. grok compacts on its own when its context fills, and through the agent's own
    `/compact`.

  If grok sends any, the frames become the wire fixture. If it sends none, the fixture is built from
  the schema, and the record says so.
* **D6. Not changed here.**
  * The daemon's own `/compact` notices stay.
  * The grok table's `KindNone` for `/compact` stays; 0179 F9 owns it.
  * OpenCode's and codex's compaction notices stay. Moving them onto `compaction` would be a
    separate decision for each provider.

### Consequences

* Good, because the user sees that the agent's memory changed, and can read what it kept.
* Good, because a streamed summary costs two events, not one per chunk.
* Good, because replay and a reconnecting phone fold to the same row as a phone that watched live.
* Good, because Magic's advertisement stays honest.
* Neutral, because no agent Magic runs is known to send compaction updates yet. As with 0188 and
  0189, this makes Magic ready rather than fixing a visible gap.
* Neutral, because two kinds of compaction message remain. ACP agents send `compaction` rows;
  OpenCode and codex send daemon notices.
* Bad, because this is the largest phone change in the D10 series so far: an upsert by ID, a row
  with four states, and an expanding summary. It needs widget tests and `make preflight`.
* Bad, because an agent that clears a summary with `null` leaves the old summary on screen. The SDK
  decodes the `null` as an omission. Clearing with `[]` works, and a stale error is hidden by D3.
  Exposing the difference is an SDK change (More Information).
* Bad, because once compaction is advertised, a provider that also implements `CompactSession` can
  show both the daemon's "Compacting the conversation…" line and an agent's `in_progress` row for
  the same compaction. Today no provider can: grok's `/compact` resolves to `KindNone`. The probe
  in D5 would show it if that changes.

### Confirmation

* An acpagent test covers:
  * a compaction driven by updates alone;
  * one driven by chunks, then a completing update with and without a `summary`;
  * a first-seen chunk;
  * the 32 KiB cap and the 16-ID cap;
  * a non-text block;
  * `_meta` on either variant;
  * an unknown status;
  * an update that omits `summary` and `error`, which keeps both, and one with `summary: []`, which
    clears it.

  It requires the events and declines named in D1, and that further chunks emit nothing.
* A manager test requires `compaction` to be kept in history, and an identical consecutive state for
  the same ID to be dropped.
* A Flutter widget test draws each status, folds several states for one ID into one row at its first
  position, keeps two IDs as two rows, expands the summary, and hides an error once the status is
  not `failed`. Replaying the same events after a cold start gives the
  same row.
* The A2 wire test pins `initialize` with `session.compaction`. The honesty test passes with
  compaction implemented, and fails if it is advertised without behavior.
* Each new test is seen failing first, and planted breaks are shown failing on a scratch copy.
* D5's probe, with its fixture.

## Pros and Cons of the Options

### A new `compaction` event, folded by ID, drawn as one transcript row with its status and a collapsed summary

* Good, because it carries the whole contract, including the summary, which is the part that tells
  the user what changed.
* Good, because one compaction stays one row, however many updates describe it.
* Neutral, because it is a third way the phone shows compaction, beside two daemon notice texts.
* Bad, because it needs a stateful fold in acpagent, a new event, and a stateful row on the phone.

### Map each status to a daemon `notice` line, and drop the summary

* Good, because it follows the OpenCode and codex precedent and needs no phone change.
* Bad, because the summary, which is the content the contract exists to deliver, is thrown away
  while `{}` claims "the complete compaction contract".
* Bad, because one compaction becomes two or three separate lines, and the daemon's notice cannot
  carry a failure's error distinctly.
* Bad, because the agent's message becomes indistinguishable from the daemon's, which 0188
  avoided.

### Show the summary as an assistant message

* Good, because the summary is shown in full, with no new row type.
* Bad, because the summary is not something the agent said in reply. It would read as a turn the
  user did not prompt.
* Bad, because the status and the error have no place in a message, and an update that replaces
  the summary cannot replace a message already drawn.

### Keep declining, and do not advertise `session.compaction`

* Good, because nothing changes, and no agent Magic runs sends compaction updates.
* Bad, because an agent that would tell the user its context was compacted has no way to, since
  conforming agents send it only when advertised.

## More Information

* The facts above were read from the code on 2026-10-14, at `b1454ab1`, and from
  `acp-go-sdk2@v0.17.1`.
* **An ask for `acp-go-sdk2`, under 0184 D11.** Patch-semantic fields need a three-state value
  (absent, `null`, or set), for example a generic `Optional[T]`. With it, Magic could follow ACP's
  `null`-clears rule. ~~Filing it is outward-facing, so it waits for the owner's word.~~ Filed on
  2026-10-14 at the owner's request ("file the issue and ensure it is on github") as
  https://github.com/maccavelli/acp-go-sdk2/issues/5, "Patch-semantic fields cannot represent null:
  omitted and null decode alike, and null cannot be sent".
  * Before filing, its reproduction was run against `v0.17.1`. Both directions were confirmed: an
    omitted field and `null` decode alike, for `SessionCompactionUpdate` and for `SubagentUpdate`.
    And a `nil` field is dropped on encode, so an agent cannot send `null`.
  * The issue also names the subagent fields (`capabilities`, `description`, `state`, `title`),
    which D10's item 7 will meet.
  * Like https://github.com/maccavelli/acp-go-sdk2/issues/4, it belongs to that repository's
    records, not this one.
* The PLAN is
  [0190-PLAN-show-agent-context-compaction-on-the-phone.md](0190-PLAN-show-agent-context-compaction-on-the-phone.md).
  ~~Nothing is implemented until the owner accepts an option and approves the PLAN.~~ The owner
  accepted the recommended option and approved K1–K3 on 2026-10-14.

## Amendment — 2026-10-14: D5's zero-token probe ran

Magic advertises `session.compaction` since `898f8096` (PLAN K3). D4 rests on grok accepting that
advertisement. D5 asked whether grok uses it. The zero-token half of the probe answered both for
session start.

* **Evidence.** `TestLiveGrokCompactionAtSessionStart`
  (`internal/provider/grok/live_compaction_test.go`, `live_grok` tag) ran against
  `grok 1.0.50 (c58f321264ba) [stable]` with wire capture.
  * grok accepted the `initialize` carrying `session.compaction` and opened the session.
  * In the 12 seconds after `session/new`, it sent 43 frames: no compaction update, and no error.
* **Consequence.** The advertisement is safe for grok `1.0.50`: at session start it changes nothing
  the user sees. The fixture remains the schema-built frames, as D5 allows, because grok sent none
  to capture.
* **Still open.** Two things wait for the half of the probe with model turns, after the owner's grok
  quota resets:
  * whether grok sends compaction updates when it compacts on its own, or through its own
    `/compact`;
  * whether it then also produces a daemon "Compacting the conversation…" line, the double report
    named under Consequences.
