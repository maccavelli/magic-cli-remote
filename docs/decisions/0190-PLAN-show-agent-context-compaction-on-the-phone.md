---
status: in-progress
date: 2026-10-14
associated-madr: "0190-MADR-show-agent-context-compaction-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing agent context compaction on the phone

Associated MADR: [0190-MADR-show-agent-context-compaction-on-the-phone.md](0190-MADR-show-agent-context-compaction-on-the-phone.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-14 the owner accepted the
MADR's recommended option and approved K1–K3 ("accept option 1, proceed to k1-k3"). This PLAN
implements that option, "A new `compaction` event, folded by ID, drawn as one transcript row with
its status and a collapsed summary" (D1–D6). K3 still follows its own gate: it is taken after K1 and
K2 are committed. K4 waits for approval by name, and its second step for the quota.

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

## Execution record

### K1 and K2, 2026-10-14: complete (staged, not committed)

**Approval:** the owner on 2026-10-14, "accept option 1, proceed to k1-k3". K3 waits, by its own
gate, for K1 and K2 to be committed.

**K1 step 1, structure.**

* `event.TypeCompaction` (`compaction`) and five fields: `CompactionID`, `CompactionStatus`,
  `CompactionSummary`, `CompactionError` and `CompactionTruncated` (`compaction_id`, …).
* The type is in `Types()`, in `IsControl`'s must-deliver set, and in `ClassOf` as content. Its
  strings are counted in `Bytes`.
* `TestTypeConstants` and `TestControlTypesComplete` list it.
* `protocol-v1.md` has the event in the type enumeration and the field list, and a section of its
  own. The enumeration line was rewrapped when markdownlint's 200-column rule caught it during
  `make preflight`.
* The guards passed after this step, each seen running: `TestEventTypesAreDocumented`,
  `TestClassOfIsExhaustive`, `TestBytesTracksStructGrowth`, `TestTypeConstants` and
  `TestControlTypesComplete`.

**K1 steps 2–3, the fold.**

* `compaction_test.go` builds its frames from the SDK types; they are the schema-built fixture (D5).
  It covers 11 cases in three tests: `TestCompactionUpdatesFoldByID`, `TestCompactionIsBounded`
  and `TestCompactionDeclinesWhatItCannotShow`.
* The structure (`compaction.go`'s constants and fold type, and the session field) was added first,
  with the A4 declines still in place. Every case **failed**, for example `events = []`,
  `after two chunks: [], want one in_progress event…`, and `no compaction events`.
* `compaction.go` folds by ID:
  * `onCompactionUpdate` sets the status. A non-nil `summary` replaces the accumulated one, and an
    empty one clears it. A non-nil `error` replaces the stored one, and `nil` leaves both.
  * `onCompactionChunk` appends, and emits only when it creates the compaction.
  * The summary is capped at 32 KiB on a rune boundary, at most 16 IDs are tracked with the oldest
    forgotten, non-text blocks are declined as `compaction.summary.<type>`, and `_meta` is declined
    on each variant.
* The A4 table's two rows now expect `compaction`.
* **A test-design finding.** The first event of a chunk-started compaction carries that chunk's
  text, because D1 emits "the full current state". Two expectations written as `Summary: ""` were
  corrected to match D1 before the implementation ran.
* **An SDK finding, beyond issue #5.** `json.Marshal` drops `Summary: []acp.ContentBlock{}`
  (`omitempty`). So a Go agent on `acp-go-sdk2` cannot send the `summary: []` that clears, any more
  than it can send `null`. The test sends that frame as raw JSON, as a non-Go agent would. The
  issue was not amended; that is an outward-facing step for the owner.

**K1 step 4, the manager.**

* `TestCompactionIsKeptAndDeduplicatedPerID` **failed** at first, with all seven states kept,
  repeats included.
* `compactionChangedLocked` (`internal/session/compaction.go`) keeps an FNV-64a hash of the last
  state per ID. A repeat is dropped even with another compaction's state in between. The memory is
  capped at 64 IDs and starts over past the cap, which can only let a repeat through, never drop a
  change. A replayed event is never suppressed.
* `TestCompactionDedupeIsBounded` passes against the bare structure by construction; plant 13
  below shows that it guards the cap.

**K1, planted breaks, each on a scratch copy of the repository.** All 15 failed:

| # | Plant | Failure |
| --- | --- | --- |
| 1 | a first-seen chunk emits nothing | `after two chunks: [], want one in_progress event…` |
| 2 | every chunk emits | `after two chunks: [{…Kept } {…Kept the parser fix.}]…` |
| 3 | an update's summary appends | `Summary:draftfinal … want the summary replaced by "final"` |
| 4 | an omitted summary clears | `completed = [{… Summary: …}], want […Summary:Kept the parser fix.…]` |
| 5 | an omitted error clears | `{… Status:cancelled Summary:partial Error: …}` |
| 6 | no summary cap | `summary 40960 bytes, truncated false; want 32768 and true` |
| 7 | the cut splits a rune | `summary 32768 bytes, truncated true; want 32767, true, and no split rune` |
| 8 | no ID cap | `events = [{ID:id-0 Status:completed Summary:oldnew …}]` |
| 9 | a non-text block not declined | `declined [compaction_summary_chunk._meta compaction_update._meta] … want […compaction.summary.image…]` |
| 10 | update `_meta` not declined | `declined [compaction_summary_chunk._meta compaction.summary.image] …` |
| 11 | the manager keeps repeats | `compactions in history = [c1\|in_progress\| c1\|in_progress\| …]` |
| 12 | the manager ignores the summary | `… = [c1\|in_progress\| c2\|in_progress\| c1\|completed\|Kept the parser fix.]` |
| 13 | the manager's memory unbounded | `remembering 192 compaction IDs, want at most 64` |
| 14 | `Bytes` ignores the strings | `string fields Bytes does not count: [CompactionID CompactionStatus CompactionSummary CompactionError]` |
| 15 | compaction droppable | `IsControl("compaction") = false, want true` |

**K1, verification.**

* `go test -race ./...`: passed.
* `make pre-add-check` on the 10 changed Go files: clean. `make lint`: clean.
* `make ci-windows` on the Windows test host: `ci-windows-exit=0`, with `internal/event`,
  `internal/provider/acpagent` and `internal/session` all `ok`.

**K2, the phone.**

* `apps/mobile/test/compaction_test.dart` has 14 tests.
  * Reducer: one compaction is one row, updated in place at its first position. Two IDs are two
    rows. An identical state is a no-op. An event without an ID is ignored. A replay of the same
    events gives the same row. The row survives the transcript cache.
  * Row: the line for each of the four statuses, and an unknown status reading as in progress. The
    summary is collapsed until opened, behind "What the agent kept". A truncated summary says so.
    The error shows only while the status is `failed`.
* The structure came first: the `SessionEvent` fields and parsing, `ChatItemKind.compaction`,
  `ChatItem.compactionError` and `compactionTruncated` with cache round-trip, and a `SizedBox`
  placeholder in the row switch. Against it, 12 of 14 **failed**, for example
  `Expected: ChatItemKind.compaction`, `Actual: ChatItemKind.assistant`, and
  `Found 0 widgets with text "Conversation compacted"`. The two that passed were the no-op and the
  ignored ID, which should already pass.
* `_upsertCompaction` (`transcript_reducer.dart`) replaces the row in place, keyed by `toolId`, as
  the approvals card is. The row is a `_CompactStatusTile`, the thought, tool and approvals idiom,
  with a status line, an icon, and a rail coloured by status. The collapsed detail holds the error
  (only when `failed`), "What the agent kept:" with the summary, and a truncation line.
* While placing `_upsertCompaction`, I put it between `_upsertApprovals`' doc comment and its
  declaration. The diff showed it, and the comment was moved back before verification.

**K2, planted breaks, each on a scratch copy of `apps/mobile`.** All 11 failed:

| Plant | Failing test |
| --- | --- |
| every state appends a row | `one compaction is one row…`, `an identical state is a no-op`, the replay test, and the error test |
| an identical state rebuilds | `an identical state is a no-op` |
| an update moves the row to the end | `one compaction is one row, updated in place at its first position` |
| an event without an ID makes a row | `an event without an id is ignored` |
| the error shows in every status | `the error shows only while the status is failed` |
| the summary loses its label | `the summary is collapsed until opened` |
| truncation is not said | `a truncated summary says so` |
| an unknown status reads as something else | `status in_progress …` and `status paused …` |
| the cache drops the error | `the row survives the transcript cache` |
| the cache drops truncation | `the row survives the transcript cache` |
| the parser drops truncation | the cache test, and `a truncated summary says so` |

**K2, verification.** `make preflight` passed:

* records and docs links;
* markdownlint, with 0 issues after the rewrap above;
* `gofmt`, `go mod tidy`, `go vet`, `staticcheck` and `go test -race`;
* the install tests, systemd units and release build;
* the Flutter pin and pub advisories;
* `dart format` (`Formatted 216 files (0 changed)`), `flutter analyze` (`No issues found!`), and
  `flutter test` (`+1451 ~3: All tests passed!`).

**Not done.**

* ~~**K3** waits until K1 and K2 are committed, per its own gate.~~ K1 and K2 were committed as
  `575f02e1`, and K3 followed (below).
* **K4** waits for approval by name, and its second step for the quota.
* **Noticed, not changed.** In `chat_bubble.dart`, `_LimitNotice`'s doc comment sits above
  `_AgentNotice`'s, so the two read as one. It was introduced by 0188 N2 (`fc737d36`) and is
  outside this PLAN.

### K3, 2026-10-14: complete (staged, not committed)

**Approval:** the owner on 2026-10-14, "committed, proceed", after K1 and K2 were committed as
`575f02e1`. That met K3's gate.

**The wire test, first.** `TestACompactionArrivesOverTheWire` (`compaction_wire_test.go`) sends a
`compaction_update` over a real connection, through the SDK's capability gate as production
configures it. Before the advertisement it **failed**: the SDK refused the update with
`capability session.compaction not advertised`, and the test reported
`no compaction within 5s: the update did not get past the SDK's capability gate`.

**Step 1.**

* On a scratch copy, `Compaction` was added to `defaultClientCapabilities` without marking it
  implemented. The honesty test **failed**:
  `the default advertisement is dishonest: [session.compaction is advertised but not implemented]`.
* Then, in the tree, `session.compaction` was added to `implementedClientCapabilities`, and removed
  from the honesty test's dishonest examples. `elicitation`, `plan` and `auth.terminal` remain.

**Step 2.**

* `Compaction: &acp.CompactionCapabilities{}` is in `defaultClientCapabilities`, and its doc
  comment says what implements it.
* The pinned `initialize` frame now reads
  `"session":{"compaction":{},"configOptions":{"boolean":{}},"notices":{}}`.
* `protocol-v1.md`'s compaction section says the daemon advertises it to every ACP agent that uses
  the default capabilities, grok included.

**Planted breaks, each on a scratch copy.** Both failed:

| Plant | Failure |
| --- | --- |
| advertisement withdrawn | `initialize frame changed`, and the wire test's `no compaction within 5s` |
| advertised but not marked implemented | `session.compaction is advertised but not implemented` |

**Verification.**

* `go test -race ./...`: passed; the `internal/provider/grok` tests pass with the new frame.
* `make pre-add-check` on the three changed Go files: clean. `make lint`: clean.
* `make markdownlint-docs`: clean. `python3 scripts/check_records.py --check-all`: clean.
* `make ci-windows` on the Windows test host: `ci-windows-exit=0`, with `internal/provider/acpagent`
  and `internal/provider/grok` both `ok`.

**Rollout.** K3 is the switch. A phone build with K2 (`575f02e1`) must ship with or before a daemon
release that contains K3. An older phone ignores `compaction` events: it shows nothing, but does not
break.

**Not done.** K4, the probe, waits for approval by name. Its first step spends no model tokens; its
second waits for the owner's quota.
