---
status: proposed
date: 2026-10-14
associated-madr: "0190-MADR-show-agent-context-compaction-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing agent context compaction on the phone

Associated MADR: [0190-MADR-show-agent-context-compaction-on-the-phone.md](0190-MADR-show-agent-context-compaction-on-the-phone.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"A new `compaction` event, folded by ID, drawn as one transcript row with its status and a collapsed
summary" (D1–D6).

* A phase starts only after the owner approves it by name.
* Each Go phase ends with `make pre-add-check` on its staged Go files, `go test -race ./...` and the
  0145 gates on a Windows host.
* The Flutter phase ends with `make preflight` (`flutter analyze`, `flutter test`, `dart format`).
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

An ACP agent's compaction reaches the user's transcript as one row per compaction, showing its
status and what the agent kept, including when the phone was not watching. Magic advertises
`session.compaction` only once that is true.

## Scope

* `internal/event/event.go`: `TypeCompaction` (`compaction`) and its five fields, `Types()`, the
  must-deliver set, `ClassOf` (content), and `Bytes` (D1, D2).
* `internal/provider/acpagent/session.go`, and a new `compaction.go`: the per-ID fold, its bounds,
  and the two cases (D1). The A4 table in `sessionupdate_test.go`.
* `internal/session/manager.go`: dropping an identical consecutive state per ID (D2).
* `internal/provider/acpagent/acpagent.go` and `advertise_test.go`: the advertisement (D4).
* `apps/mobile/lib/data/protocol/models.dart`, `transcript_reducer.dart`, the chat-item model and
  a row widget, with widget tests (D3).
* `docs/guides/protocol-v1.md`: the event, in the enumeration that the doc-coverage test checks,
  and its fields.
* **Out of scope:**
  * the grok `/compact` table mismatch (0179 F9);
  * moving OpenCode's or codex's compaction notices onto the new event;
  * a client-initiated compaction, which ACP does not define;
  * gobble's capability set.

## Implementation Steps

### K1. The event and the acpagent fold (D1, D2)

1. Add `TypeCompaction` and its fields, with no behavior. Add it to `Types()`, the must-deliver set
   and `ClassOf`, and count its strings in `Bytes`. Document it in `protocol-v1.md`. The guards
   `TestEventTypesAreDocumented`, `TestClassOfIsExhaustive` and `TestBytesTracksStructGrowth` must
   pass.
2. Write the acpagent test (MADR Confirmation, first item), with frames built from the SDK types.
   It is the schema-built fixture until D5 replaces it, and its comment says so. Show it failing on
   A4's declines.
3. Implement the fold in `compaction.go` and the two cases. Update the A4 table's two rows.
4. Write the manager test (MADR Confirmation, second item). Show it failing, then drop an identical
   consecutive state per ID at the existing dedupe site.

### K2. The phone (D3)

1. Write the widget tests (MADR Confirmation, third item). Show them failing.
2. Parse `compaction`. Upsert the transcript item by `compaction_id`, so the last state wins and
   the row keeps its position. Add the row variant: the status line, the error, and the collapsed
   summary.
3. `dart format` on the changed files, and `make preflight`.

### K3. Advertise (D4)

Taken only after K1 and K2 are committed.

1. Remove `session.compaction` from the honesty test's dishonest examples, and add it to
   `implementedClientCapabilities`. Show, on a scratch copy, the honesty test failing with
   compaction advertised but not implemented.
2. Add `Compaction: &acp.CompactionCapabilities{}` to `defaultClientCapabilities`, and update the
   pinned `initialize` frame in `TestInitializeFrameIsPinned`.

### K4. Probe (D5, owner gate)

1. Zero-token: `initialize` and `session/new` against grok, with compaction advertised and
   `MCREMOTE_WIRE_CAPTURE_DIR` set. Record whether grok accepts the advertisement, and whether it
   sends anything at session start.
2. With the owner's quota, after grok's account resets: a turn sequence long enough, or grok's own
   `/compact`, to see whether grok sends `compaction_update` or `compaction_summary_chunk`. Commit
   any such frames, redacted, as the fixture. Otherwise the schema-built fixture stands, and the
   record says so.

## Verification

* Every new test is seen failing first, and planted breaks are shown failing on a scratch copy.
* `go test -race ./...`, `make pre-add-check`, `make lint`, `make preflight`.
* `make ci-windows` on the Windows test host.
* `python3 scripts/check_records.py --check-all` and `make markdownlint-docs`.

## Rollout and Rollback

* K1 and K2 change nothing a user sees, because no agent sends compaction updates until K3
  advertises.
* K3 is the switch. Reverting it withdraws the advertisement, and conforming agents stop sending
  compaction updates.
* A phone build without K2 ignores `compaction`, so K3 must not ship to a phone that lacks K2. K2
  ships first.
