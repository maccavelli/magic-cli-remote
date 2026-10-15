---
status: proposed
date: 2026-10-14
associated-madr: "0192-MADR-show-an-agents-identified-plans-on-the-phone.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement showing an agent's identified plans on the phone

Associated MADR: [0192-MADR-show-an-agents-identified-plans-on-the-phone.md](0192-MADR-show-an-agents-identified-plans-on-the-phone.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"Carry identified plans beside the stable one, keyed by plan ID, in all three content shapes"
(D1–D7).

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

* `internal/event`: the plan event's `plan_id`, `plan_kind`, `plan_content`, `plan_truncated`,
  `plan_uri` and `plan_removed` fields, and `Bytes` (D1, D2).
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

1. Add the plan event's fields, `omitempty`, and count their strings in `Bytes`. Document them in
   `protocol-v1.md`. A test pins the stable plan's event as encoding exactly as before.
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
* A phone without P2 treats an identified items plan as the stable plan, which is today's behavior.
  P2 should ship with or before P3.
