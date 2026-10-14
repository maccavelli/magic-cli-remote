---
status: in-progress
date: 2026-10-13
associated-madr: "0188-MADR-show-agent-notices-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing agent notices on the phone

Associated MADR: [0188-MADR-show-agent-notices-on-the-phone.md](0188-MADR-show-agent-notices-on-the-phone.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-13 the owner accepted the
MADR's recommended option and approved N1 and N2 ("accept option 1, do N1 and N2"). This PLAN
implements that option, "A new `agent_notice` event, kept in Magic's history and drawn as a
transcript row with its severity" (D1–D5). N3 and N4 still wait for approval by name.

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

## Execution record

### N1 and N2, 2026-10-13: complete (committed as `fc737d36`)

**Approval:** the owner on 2026-10-13, "committed and pushed, accept option 1, do N1 and N2".

**N1, structure first.** These went in with no behavior, so the tests below could compile against
the code before the mapping.

* `event.TypeAgentNotice` (`agent_notice`) and the fields `NoticeSeverity`, `NoticeTitle` and
  `NoticeDescription` (`notice_severity`, `notice_title`, `notice_description`).
* The type is in `Types()`, in `IsControl`'s must-deliver set, and in `ClassOf` as content, so it
  outlives telemetry and progress when history is trimmed (D2). Its strings are counted in `Bytes`.
* `TestTypeConstants` and `TestControlTypesComplete` list it.
* `protocol-v1.md` names it in the event-type enumeration and in the field list, and has its own
  section, which says the event appears only once the daemon advertises `session.notices` (N3).
* The guards passed after this step: `TestEventTypesAreDocumented`, `TestClassOfIsExhaustive` and
  `TestBytesTracksStructGrowth`.

**N1, the mapping.**

* `TestNoticeBecomesAnAgentNotice` delivers a notice of each severity, with and without a
  description, with `_meta`, and with an unknown severity. On A4's decline it **failed**:
  `Indexed 120 files: agent_notice events = 0, want 1`.
* The `u.Notice` case now emits `agent_notice`. The severity is passed through as sent, and the
  title and description are trimmed. `_meta` is declined as `notice._meta`.
* The A4 table's `Notice` row now expects `agent_notice`.

**N1, the manager.**

* `TestAgentNoticeIsKeptAndDeduplicated` sends a notice, the same notice again, then the same words
  with a new severity, then with new detail, then a daemon `notice` with the same words. Before the
  change it **failed**: the repeat was kept.
* The existing notice dedupe now also covers `agent_notice`. It shares the session's
  `NoticeDeduper`, with kind `agent:<severity>` and text title and description, so an agent notice
  never matches a daemon notice.
* `TestAgentNoticeIsContent` pins D2's history class. It was added when a review of the plants
  found nothing covering it.

**N2, structure first.**

* `SessionEvent` gained `noticeSeverity`, `noticeTitle` and `noticeDescription`, parsed from the
  three keys.
* `ChatItem` gained `noticeSeverity`, `noticeDetail` and `isAgentNotice`, carried through
  `copyWith`. An agent notice is a `system` row whose text is the title.

**N2, the tests.** Before the reducer and the row existed, three **failed**:

* `test/agent_notice_test.dart`, the reducer row: `Expected: an object with length of <1>`,
  `Actual: []`;
* the cache round trip: `Expected: true`, `Actual: <false>`;
* the widget test in `test/chat_render_test.dart`: `Found 0 widgets with text "Navigation is
  limited."`.

The widget test asserts severity on each row's icon, not on pixel widths, since the test font is
fixed-width. The icons are `info_outline` (twice: `info` and the unknown `critical`),
`warning_amber_outlined` and `error_outline`.

**N2, the change.**

* `transcript_reducer.dart` has an `agent_notice` case. It ignores a blank title.
* `ChatItem.toJson` and `fromJson` carry both fields, and a cache entry without them reads back as
  before.
* `chat_bubble.dart` draws `_AgentNotice`: the left accent rail, with an icon in the severity's
  color (`error` the scheme's error, `warning` the theme's `caution`, anything else as info), the
  title in medium weight, and the detail under it, up to four lines.

**Fail first, on scratch copies.** Every plant failed its test.

| Plant | Caught by |
| :--- | :--- |
| G1 description dropped | `TestNoticeBecomesAnAgentNotice` |
| G2 `_meta` decline removed | the same (`declined [], want [notice._meta]`) |
| G3 agent-notice dedupe removed | `TestAgentNoticeIsKeptAndDeduplicated` |
| G4 dedupe key ignoring severity | the same (the error-severity repeat was dropped) |
| G5 classed as telemetry | `TestAgentNoticeIsContent` |
| G6 `Bytes` not counting the fields | `TestBytesTracksStructGrowth` |
| G7 made droppable | `TestControlTypesComplete` |
| F1 reducer dropping `agent_notice` | the reducer tests |
| F2 cache dropping the severity | the cache round-trip test |
| F3 `warning` drawn with the info icon | the widget test |
| F4 an unknown severity drawn as error | the widget test |
| F5 bubble ignoring agent notices | the widget test |
| F6 detail not shown | the widget test |

The first try at G1 broke the build (an unused variable), not an assertion. It was rewritten to
compile, then failed as required. The Flutter plants ran on copies of `apps/mobile`, each with
`flutter test` on the named files.

**Verification,** each exit status recorded before reading its output:

```text
go test -race -count=1 ./...                         exit=0
./scripts/go-precheck.sh <9 changed Go files>        exit=0 (gofmt, golint, govulncheck)
make lint                                            exit=0
make markdownlint-docs                               exit=0, "Summary: 0 issues in 0 files"
make preflight                                       exit=0: check-records, markdownlint, gofmt,
                                                     go vet, staticcheck, go test -race, dart format
                                                     ("Formatted 214 files (0 changed)"), flutter
                                                     analyze ("No issues found!"), flutter test
                                                     ("+1424 ~3: All tests passed!")
```

* **Windows.** `make ci-windows` ran on the Windows test host, in Git Bash, against the N1/N2
  working tree, as a tarball of the 1852 files git lists. Result: exit 0, "ci-windows-local: ALL
  SELECTED CHECKS PASSED", with `internal/event`, `internal/provider/acpagent` and
  `internal/session` all `ok`. That target runs the Go suite. The Flutter suite ran on macOS
  through `make preflight` above.

**Not done:**

* ~~N3 (advertise) and~~ N4 (probe): not approved. N3 is below;
* ~~no commit: N1 and N2 are staged, and a commit needs the owner's word in its turn;~~ the owner
  committed `fc737d36`. It was not yet pushed when N3 started;
* CI on push.

### N3, 2026-10-13: implemented and verified; not committed

**Approval:** the owner on 2026-10-13, "committed, proceed", after N1 and N2 were committed as the
step requires.

**Seen failing first.** `TestANoticeArrivesOverTheWire` (`agentnotice_test.go`) sends a `notice`
from the byte-level fixture agent, over a real connection configured as production configures it.
Before the advertisement it **failed**, with the SDK refusing the notice at its capability gate:

```text
ERROR failed to handle notification method=session/update err="{"code":-32602,"message":"Invalid params",
  "data":{"error":"session/update: capability session.notices not advertised: variant notice"}}"
agentnotice_test.go:38: no agent_notice within 5s: the notice did not get past the SDK's capability gate
```

**The change.**

* `defaultClientCapabilities` advertises `session.notices` (`{}`), with a doc bullet naming the
  behavior that implements it.
* `implementedClientCapabilities` lists it.
* The pinned `initialize` frame is now
  `"session":{"configOptions":{"boolean":{}},"notices":{}}`.
* `TestCapabilityHonesty`'s dishonest example moved from `session.notices` to
  `session.compaction`, a capability Magic still lacks.
* grok's `Spec` uses the default, so grok is offered notices. gobble's set is
  [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md)'s to decide.
* `protocol-v1.md`'s `agent_notice` section now says the daemon advertises `session.notices` to
  every agent that uses the default capabilities.

**Fail first, on scratch copies.** Each plant failed its test:

| Plant | Caught by |
| :--- | :--- |
| H1a notices advertised, not in the implemented set | `TestCapabilityHonesty`: "session.notices is advertised but not implemented" |
| H1b the same, seen through grok's `Spec` | `TestGrokAdvertisesOnlyWhatMagicImplements` |
| H2 the advertisement removed | `TestInitializeFrameIsPinned`, and `TestANoticeArrivesOverTheWire` (the SDK gate again) |

**Verification,** each exit status recorded before reading its output:

```text
go test -race -count=1 ./...                         exit=0
./scripts/go-precheck.sh <3 changed Go files>        exit=0 (gofmt, golint, govulncheck)
make lint                                            exit=0
make markdownlint-docs                               exit=0
```

* **Windows.** `make ci-windows` ran on the Windows test host, in Git Bash, against the N3 working
  tree, as a tarball of the 1852 files git lists. Result: exit 0, "ci-windows-local: ALL SELECTED
  CHECKS PASSED", with `internal/provider/acpagent` and `internal/provider/grok` both `ok`.

**Rollout.** This is the switch. As the Rollout and Rollback section says, a phone without N2
ignores `agent_notice`. A release carrying N3 should go out with, or after, a phone build carrying
N2.

**Not done:**

* N4 (the probe): not approved. Its zero-token half is now possible, since the advertisement
  exists. The half with model turns waits for the owner's grok quota to reset;
* no commit: N3 is staged, and a commit needs the owner's word in its turn;
* CI on push.
