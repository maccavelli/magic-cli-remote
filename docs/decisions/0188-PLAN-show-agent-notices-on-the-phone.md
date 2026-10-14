---
status: proposed
date: 2026-10-13
associated-madr: "0188-MADR-show-agent-notices-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing agent notices on the phone

Associated MADR: [0188-MADR-show-agent-notices-on-the-phone.md](0188-MADR-show-agent-notices-on-the-phone.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"A new `agent_notice` event, kept in Magic's history and drawn as a transcript row with its
severity" (D1–D5). If the owner chooses another option, this PLAN is rewritten first.

* A phase starts only after the owner approves it by name.
* Each Go phase ends with `make pre-add-check` on its staged Go files, `go test -race ./...` and the
  0145 gates on a Windows host.
* The Flutter phase ends with `make preflight` (`flutter analyze`, `flutter test`, `dart format`).
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

An ACP agent's `notice` reaches the user's transcript with its severity, including when the phone
was not watching. Magic advertises `session.notices` only once that is true.

## Scope

* `internal/event/event.go`: `TypeAgentNotice` (`agent_notice`), `NoticeSeverity`, `NoticeTitle`
  and `NoticeDescription` fields, `Types()`, and the must-deliver set (D1, D2).
* `internal/provider/acpagent/session.go`: the `u.Notice` case (D1).
* `internal/session/manager.go`: dedupe of identical consecutive `agent_notice` (D2).
* `internal/provider/acpagent/acpagent.go` and `advertise_test.go`: the advertisement (D4).
* `apps/mobile/lib/data/chat/transcript_reducer.dart`, the chat-item model and the row widget, with
  a widget test (D3).
* `docs/guides/protocol-v1.md`: the event type, in the enumeration that the doc-coverage test
  checks, and its fields.
* **Out of scope:**
  * a transient top banner;
  * any change to the daemon's own `notice`;
  * compaction and the other D10 capabilities;
  * gobble's capability set.

## Implementation Steps

### N1 — The event and the acpagent mapping (D1, D2)

1. Write the acpagent test: a `notice` of each severity, with and without `description`, and with
   `_meta`. It requires `agent_notice` events carrying the same fields, and the `_meta` decline.
   Show it failing on the A4 decline.
2. Add `TypeAgentNotice` and the three fields to `event.Event`. Add the type to `Types()` and to the
   must-deliver set, and document it in `protocol-v1.md`. Without that, the doc-coverage test
   `TestEventTypesAreDocumented` fails.
3. Map the `u.Notice` case. Update the A4 table's `Notice` row from "declined" to "emits
   `agent_notice`".
4. A manager test requires `agent_notice` to be kept in history, and an identical consecutive one
   to be dropped. Show it failing, then extend the notice dedupe at the existing site. The key is
   severity, title and description.

### N2 — The phone (D3)

1. Write the widget test: an `agent_notice` of each severity becomes a transcript row with the
   title. The description is shown under it. `warning` and `error` are visibly distinct from `info`,
   which is asserted on the row's semantic severity, not on pixel widths (the test font is
   fixed-width). An unknown severity draws as `info`. Show it failing.
2. Add the `agent_notice` case to `transcript_reducer.dart` and the row variant, reusing the status
   row's left-rail idiom.
3. `dart format` on the changed files, and `make preflight`.

### N3 — Advertise (D4)

Taken only after N1 and N2 are committed.

1. Move the honesty test's dishonest example from `session.notices` to `session.compaction`, and
   add `session.notices` to `implementedClientCapabilities`. Show the honesty test failing with
   notices advertised but not implemented, on a scratch copy.
2. Add `Notices: &acp.NoticeCapabilities{}` to `defaultClientCapabilities`, and update the pinned
   `initialize` frame in `TestInitializeFrameIsPinned`.

### N4 — Probe (D5, owner gate)

1. Zero-token: `initialize` and `session/new` against grok, with notices advertised and
   `MCREMOTE_WIRE_CAPTURE_DIR` set. Record whether grok sends any `notice`. Commit any such frames,
   redacted, as the fixture. Otherwise build one from the schema and say so.
2. With the owner's quota, after grok's account resets: a short session that triggers the
   conditions grok reports through `_x.ai/mcp/server_status`. This checks for the same news arriving
   twice.

## Verification

* Every new test is seen failing first, on a scratch copy, and passes after.
* `go test -race ./...`, `make pre-add-check`, `make lint`, `make preflight`.
* `make ci-windows` on the Windows test host.
* `python3 scripts/check_records.py` and `make markdownlint-docs`.

## Rollout and Rollback

* N1 and N2 change nothing a user sees, because no agent sends notices until N3 advertises.
* N3 is the switch. Reverting it withdraws the advertisement, and agents stop sending notices.
* A phone build without N2 ignores `agent_notice`, so N3 must not ship to a phone that lacks N2. N2
  ships first.
