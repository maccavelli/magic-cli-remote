---
status: in-progress
date: 2026-10-14
associated-madr: "0192-MADR-show-an-agents-identified-plans-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing an agent's identified plans on the phone

Associated MADR: [0192-MADR-show-an-agents-identified-plans-on-the-phone.md](0192-MADR-show-an-agents-identified-plans-on-the-phone.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-14 the owner accepted the
MADR's recommended option and approved P1 and P2. This PLAN implements that option, "Carry
identified plans beside the stable one, keyed by plan ID, in all three content shapes" (D1–D7). P3
is taken only after P1 and P2 are committed, and P4's second step waits for the owner's quota.

* A phase starts only after the owner approves it by name.
* Each Go phase ends with `make pre-add-check` on its staged Go files, `go test -race ./...` and the
  0145 gates on a Windows host.
* The Flutter phase ends with `make preflight` (`flutter analyze`, `flutter test`, `dart format`).
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

Every plan an ACP agent reports, stable or identified, in any of the three content shapes, reaches
the phone as its own entry. Removing one never hides another. Magic advertises the unstable `plan`
capability only once that is true.

## Scope

* `internal/event`: ~~the plan event's~~ the new `identified_plan` type, its place in the type
  list, must-deliver set and retention class, and its `plan_id`, `plan_kind`, `plan_content`,
  `plan_truncated`, `plan_uri` and `plan_removed` fields, and `Bytes` (D1–D3, amended 2026-10-14).
* `internal/provider/acpagent/session.go`: the `u.PlanUpdate` and `u.PlanRemoved` cases (D1, D2);
  the A4 table; the replacement for `TestSessionUpdatePlanRemovedEmitsEmptyPlan`; and the
  content-variant pin (D6).
* `apps/mobile`: the keyed plan state in the transcript model and reducer, and the panel's Markdown
  and file rows (D4).
* `internal/provider/acpagent/acpagent.go` and `advertise_test.go`: the advertisement (D5).
* `docs/guides/protocol-v1.md`: the plan event's new fields.
* **Out of scope:**
  * reading or opening a file plan's URI;
  * the other providers' plans;
  * grok's `_x.ai/exit_plan_mode` flow.

## Implementation Steps

### P1. The event and the acpagent mapping (D1–D3, D6)

1. ~~Add the plan event's fields~~ Add `TypeIdentifiedPlan` (`identified_plan`) to the type list,
   the must-deliver set and the progress class, and its fields, `omitempty`, and count their strings
   in `Bytes`. Document them in `protocol-v1.md`. A test pins the stable plan's event as encoding
   exactly as before. (Amended 2026-10-14; see the deviation entry.)
2. Write the mapping tests (MADR Confirmation, first item), with frames built from the SDK types.
   They are the schema-built fixture. Write the content-variant pin. Show them failing against the
   decline and the current removal.
3. Map `plan_update` and `plan_removed`. Replace `TestSessionUpdatePlanRemovedEmitsEmptyPlan`, which
   pins the wrong clear, with a test that a removal keeps the stable plan. Update the A4 table's
   rows.

### P2. The phone (D4)

1. Write the widget tests (MADR Confirmation, third item). Show them failing.
2. Key the transcript's plans by ID, with the stable plan under the empty ID. Apply updates and
   removals per ID. Draw `items` with `WorkItemsPanel`, `markdown` collapsed, and `file` as its name
   and path with a copy action.
3. `dart format` on the changed files, and `make preflight`.

### P3. Advertise (D5)

Taken only after P1 and P2 are committed.

1. Remove `plan` from the honesty test's dishonest examples, and add it to
   `implementedClientCapabilities`. Show, on a scratch copy, the test failing with `plan` advertised
   but not implemented.
2. Add `Plan: &acp.PlanCapabilities{}` to `defaultClientCapabilities`, with a comment that it is
   unstable. Update the pinned `initialize` frame.

### P4. Probe (D7)

1. Zero-token: `initialize` and `session/new` against grok, with `plan` advertised and
   `MCREMOTE_WIRE_CAPTURE_DIR` set. Record acceptance, and anything grok sends at session start.
2. With the owner's quota: a turn that leads grok to plan. Record whether it sends `plan`,
   `plan_update` or `plan_removed`, and commit any such frames, redacted, as the fixture.

## Verification

* Every new test is seen failing first, and planted breaks are shown failing on a scratch copy.
* `go test -race ./...`, `make pre-add-check`, `make lint`, `make preflight`.
* `make ci-windows` on the Windows test host.
* `python3 scripts/check_records.py --check-all` and `make markdownlint-docs`.

## Rollout and Rollback

* P1's removal fix changes behavior only for an agent that sends `plan_removed`, which no agent Magic
  runs does.
* P1 and P2 add identified plans for an agent that sends them unadvertised, which the SDK allows.
* P3 is the switch. Reverting it withdraws the advertisement.
* ~~A phone without P2 treats an identified items plan as the stable plan, which is today's behavior.~~
  A phone without P2 ignores `identified_plan` events, so its stable plan is untouched. P2 should
  still ship with or before P3, or identified plans are advertised but not shown.

## Execution record

### Deviation 1, 2026-10-14: identified plans travel as their own event type

* **Found** at the start of P1, before any code was written. D1 put identified plans on the `plan`
  event. A `plan` event without `entries` means "clear" (`internal/event/event.go`), and the phone
  replaces its one plan with every `plan` event's entries
  (`apps/mobile/lib/data/chat/transcript_reducer.dart`). A phone without P2 would have lost or
  replaced its stable plan. The MADR's claim that nothing gets worse was false.
* **Decision.** The owner chose a new event type, `identified_plan`, over gating new fields by a new
  handshake field. MADR Amendment 1 records it, and D1–D4 are amended there.
* **Scope.** P1 step 1 now adds the type to the event package's lists. No file joins the scope.

### P1 and P2, 2026-10-14: complete (not staged)

**Approval:** the owner on 2026-10-14 accepted option 1 and approved P1 and P2, then chose the new
event type (Deviation 1).

**P1 step 1, the event.**

* `internal/event` gained `TypeIdentifiedPlan` (`identified_plan`) in the type list, the
  must-deliver set and the progress class. It also gained the `PlanKind*` constants, and the six
  `Plan*` fields, counted in `Bytes`.
* `protocol-v1.md` has an `identified_plan` section, the fields, and the type in the list. The
  `plan` section no longer says that `plan_removed` clears the plan.
* `TestPlanEventsEncoding` pins the stable plan's event, and its clear. Its expected strings were
  first checked against the unchanged code on a scratch copy.
* **Failing first.** The new tests first failed to build. Once the type existed, the existing guards
  failed:
  * `string fields Bytes does not count: [PlanID PlanKind PlanContent PlanURI]`;
  * `event type "identified_plan" is emitted but missing from the "Event type values:" paragraph`.
* **Two wrong expectations, corrected.**
  * The items-plan string expected `plan_id` before `entries`, but key order follows the struct.
  * The file-plan URI used `<user>`, which `encoding/json` escapes. It became `file:///tmp/plan.md`.

**P1 steps 2 and 3, the mapping.**

* `plans_test.go` builds its frames from the SDK's types. With the event in place and the mapping
  unchanged, it **failed**:
  * every shape: `gave 0 identified plan(s)`;
  * the removal: `gave 0 identified plan(s) and 1 stable plan(s)`;
  * the unknown shape: `declined [plan_update], want [plan_update.diagram]`;
  * `TestPlanRemovedKeepsTheStablePlan`: `plan_removed emitted 1 plan event(s), which a phone reads as
    replacing the stable plan`;
  * the A4 rows: `events = [], want identified_plan` and `events = [plan plan], want identified_plan`.
* `TestPlanUpdateContentVariantsArePinned` passed, as a pin of the current SDK should.
* `plans.go` maps the three shapes and the removal, caps Markdown at 32 KiB on a rune boundary, and
  declines `plan_update._meta`, `plan_removed._meta` and `plan_update.<type>`.
* `session.go` calls it, and the `plan_update` decline is gone.
* `TestSessionUpdatePlanRemovedEmitsEmptyPlan` was removed. `TestPlanRemovedKeepsTheStablePlan`
  replaces it.

**P1, planted breaks, each on a fresh scratch copy.** All eight failed:

| Plant | Failure |
| --- | --- |
| removal emits the old `plan` clear | `plan_removed emitted 1 plan event(s)…`, and the A4 row |
| Markdown cut on a byte | `truncated plan is 32768 bytes, valid UTF-8 false` |
| cap off by one | `TestIdentifiedPlanMarkdownIsCapped` |
| `_meta` not declined | `declined [], want [plan_update._meta]` |
| unknown shape unnamed | `declined [plan_update], want [plan_update.diagram]` |
| `plan_update` declined again | every shape, the cap and the A4 row |
| `identified_plan` not must-deliver | `IsControl("identified_plan") = false, want true` |
| SDK copy renaming the file plan's `uri` (via `replace`) | `acp.PlanUpdateContentFile fields = [… Uri:url]`, and the file subtest |

**P1, verification.**

* `go test -race ./...`: passed.
* `make pre-add-check` on the nine changed Go files: clean. `make lint`: clean.
* `make ci-windows` on the Windows test host: `ci-windows-exit=0`, with `internal/event`,
  `internal/protocol` and `internal/provider/acpagent` all `ok`.

**P2, the phone.**

* `test/identified_plans_test.dart` has 12 tests, and `history_replay_test.dart` gained one:
  * parsing; first-seen order; a removal dropping only its plan; clearing the stable plan;
  * a plan that comes back placed where it reappeared;
  * no-ops for an unchanged plan, an unknown removal, and an event without an ID;
  * in `ChatScreen`, order, removal, Markdown collapsed, the truncation line, no image loaded, and a
    file plan's name, path and copy;
  * hydrating from the cache keeping identified plans and their order.
* The hydrate test was first written against a helper that does not exist. Before it ran, it was
  moved into `history_replay_test.dart`, against the real notifier.
* With only the structure in place (the event fields, `IdentifiedPlan`, and the transcript's
  `identifiedPlans` and `planOrder`), **11 failed**. The parse test passed, because parsing is the
  structure. The no-op test passed trivially; its plants below failed.
* **What was built, within D4.**
  * The reducer keys identified plans by ID. It keeps `planOrder`, in which the stable plan is the
    empty ID, added when it first has entries and dropped when it is cleared.
  * `ChatScreen` draws `planOrder`. A stable plan the order does not list is drawn first.
  * `items` draws `WorkItemsPanel`, titled `Plan: <id>`. `markdown` draws `PlanDocumentPanel`,
    collapsed and labelled "Markdown plan".
  * The Markdown is drawn by `MarkdownBody`, with images drawn as their alt text, and links not
    followed.
  * `file` shows the name, the decoded path of a `file:` URI (any other URI as sent), and a "Copy
    path" button.
  * An unknown shape is kept but not drawn.
  * The notifier batches `identified_plan`, and `hydrateFromCache` keeps the plans and their order.

**P2, planted breaks, on one scratch copy of `apps/mobile`, each file restored from the tree
between plants.** All ten failed:

| Plant | Failing tests |
| --- | --- |
| a removal clears every identified plan | reducer and panel removal |
| an update moves its plan to the end | reducer first-seen order |
| the stable plan not placed in the order | reducer and panel order, removal, hydrate |
| an event without an ID kept | the no-ops test |
| an unchanged plan rebuilt | the no-ops test |
| images loaded (the image builder removed) | "never loads an image" |
| Markdown open from the start | collapsed, image and truncation tests |
| the truncation line never shown | the truncation test |
| the URI copied instead of the path | the file plan test |
| hydrate drops identified plans | the hydrate test |

**P2, verification.** `make preflight` passed:

* markdownlint: 0 issues;
* the Go set;
* `dart format` (`Formatted 219 files (0 changed)`);
* `flutter analyze` (`No issues found!`);
* `flutter test` (`+1473 ~3: All tests passed!`).

**Found, out of scope, for the owner.** The chat's own Markdown (`_AssistantMarkdown` in
`chat_bubble.dart`) passes no image builder. `flutter_markdown_plus`'s default loads an `http` or
`https` image from the network, and reads any other path as a file on the phone. So an agent's
reply containing `![x](https://…)` makes the phone fetch that URL. This predates this PLAN. The
plan panel does not do this.

**Not done.**

* ~~**P3** waits until P1 and P2 are committed.~~ P1 and P2 were committed as `8c5cf0f0`, and P3
  followed (below).
* **P4**: its first step follows P3. Its second step waits for the owner's quota.
* The PLAN stays `in-progress`.

### P3 and P4 step 1, 2026-10-14: complete (staged, not committed)

**Approval:** the owner on 2026-10-14, "do P3 and P4 step 1", after `8c5cf0f0`.

**P3 step 1.**

* On a scratch copy, `plan` was advertised without being marked implemented. The honesty test
  **failed**: `the default advertisement is dishonest: [plan is advertised but not implemented]`.
* Then, in the tree, `plan` was added to `implementedClientCapabilities`. It left the honesty test's
  dishonest examples, which keep `elicitation.url` and `auth.terminal`.

**P3 step 2.**

* `Plan: &acp.PlanCapabilities{}` is in `defaultClientCapabilities`, with a comment that it is
  unstable. The doc comment names what implements it.
* The pinned frame **failed** first, with `"plan":{}` in the new frame. It now reads
  `…"fs":{"readTextFile":true,"writeTextFile":true},"plan":{},"session":{…`.
* `protocol-v1.md` says the daemon advertises `plan`.

**P3, verification.**

* `go test -race ./...`: the first run **failed**, in two tests this PLAN did not touch,
  `TestSpareIsRearmedAtTurnEndNotTurnStart` and `TestAFailedTurnStillRearmsTheSpare`
  (`prewarm_rearm_test.go`): `wanted 1 "prewarm failed", got 0`.
  * Each waits up to 3 s for a log line.
  * Run alone five times with `-race`, both passed every time, in the tree and on a scratch copy at
    `HEAD`.
  * A second full run passed.
  * It is a timing flake under full-suite load, and it predates this PLAN. It is not fixed here.
* `make pre-add-check` on the two changed Go files: clean. `make lint` and `make markdownlint-docs`:
  clean.
* `make ci-windows` on the Windows test host: `ci-windows-exit=0`, with `internal/provider/acpagent`
  and `internal/provider/grok` both `ok`.

**P4 step 1, the zero-token probe.**

* `TestLiveGrokPlanAtSessionStart` (`internal/provider/grok/live_plan_test.go`, `live_grok` tag)
  ran against `grok 1.0.50 (c58f321264ba) [stable]`, with wire capture. It sent no prompt and spent
  no model tokens.
* grok accepted the `initialize` advertising `plan`, and opened the session. The test **passed** in
  13.01 s: `grok sent 0 plan and 0 identified plan event(s) at session start`.
* The capture holds the frames grok sent:
  * the `initialize` result, and no error frames;
  * 12 `_x.ai/session/setup`, 5 `_x.ai/models/update`, 3 each of `_x.ai/announcements/update` and
    `_x.ai/settings/update`, and 2 `_x.ai/mcp/init_progress`;
  * one each of `servers_updated`, `server_status`, `mcp_initialized` and `sessions/changed`;
  * 3 `session/update`, all `available_commands_update`.
* No `plan`, `plan_update` or `plan_removed` was sent. The schema-built frames in `plans_test.go`
  stand as the fixture, as D7 allows. The capture was not committed.

**Not done.**

* **P4 step 2** waits for the owner's grok quota: a turn that leads grok to plan, to see whether it
  sends `plan`, `plan_update` or `plan_removed` now that `plan` is offered.
* The PLAN stays `in-progress`.
