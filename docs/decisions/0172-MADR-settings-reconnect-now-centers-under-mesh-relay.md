---
status: accepted
date: 2026-09-28
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Settings Reconnect now is centered under the Mesh/Relay control

## Context and Problem Statement

The phone Settings hub's Route section (MADR 0062 D6) stacks two controls when
both transports are available: a `SegmentedButton<TransportMode>` (`Mesh` /
`Relay`) and, below it, an `OutlinedButton.icon` labelled **Reconnect now**.
The reconnect button is the action for the control above it. It was wrapped in
`Align(alignment: Alignment.centerLeft)`, so it sat under the Mesh segment
instead of under the segmented control as a whole.

`SettingsSection` renders its children in a `Column` whose default
`crossAxisAlignment` is `center`. The segmented button shrink-wraps to its
segments and is therefore centered in the card. `Align` without a width factor
expands to the card's max width, then left-aligns its child. The two controls
did not share a horizontal center.

This record is the layout decision for that pair of widgets. It does not reopen
0062's transport policy (forced reconnect, probe chips, when the segmented
control is shown).

### What was measured, not assumed

| Claim | Source |
| --- | --- |
| Reconnect now lives in the Route section under the Mesh/Relay control | `apps/mobile/lib/features/settings/settings_screen.dart` `_buildRouteSection`, the `Align` wrapping `OutlinedButton.icon` |
| Parent column centers shrink-wrapped children | `apps/mobile/lib/features/settings/section_card.dart` `SettingsSection` inner `Column` with no `crossAxisAlignment` override |
| Before the change, `Align` used `Alignment.centerLeft` | the same `Align` widget |
| On the existing settings test viewport (width 1000, dpr 1), the segmented control's center was `500.0` and Reconnect now's center was `152.65` | widget test `Reconnect now is centered under the mesh/relay control` in `apps/mobile/test/settings_screen_test.dart`, run against `centerLeft` |

### Findings

* **F1 — The two controls did not share a horizontal center.** The segmented
  control is centered; Reconnect now was left-aligned inside a full-width
  `Align`.
* **F2 — Presence tests could not catch F1.** The existing Route tests
  (`both transports up: control and Reconnect now are offered`) asserted that
  both widgets exist, not where they sit.

## Decision Drivers

* The owner asked for Reconnect now to sit centered below the Mesh/Relay
  control.
* The Route section's transport policy is already decided (0062 D6); this is
  alignment of an existing action, not a new control.
* A layout assertion must fail on the old alignment, or the grouping can
  regress silently.

## Considered Options

* **A — Center Reconnect now under the Mesh/Relay control** (`Alignment.center`
  on the existing `Align`).
* **B — Stretch both controls to the card's content width.**
* **C — Put Reconnect now on the same row as the segmented control.**
* **D — Leave `Alignment.centerLeft`.**

## Decision Outcome

Chosen option: **"A — Center Reconnect now under the Mesh/Relay control"**,
because that is the grouping the owner asked for, and it is one alignment
constant plus a layout assertion.

### The decisions

* **D1 — Set the existing `Align` to `Alignment.center`.** The button keeps
  its intrinsic width. Closes **F1**.
* **D2 — Pin the grouping with a widget test** that pumps the dual-transport
  Route section and asserts the reconnect button's horizontal center equals
  the segmented control's, within `0.5` px. The assertion must have been seen
  to fail on `Alignment.centerLeft`. Closes **F2**.

### Consequences

* Good, because the two Route actions read as one vertical group.
* Good, because the change does not alter when the button is shown, which
  transport it forces, or 0062 D6.
* Neutral, because when the segmented control is hidden (only one transport
  available) the same `Align` still centers Reconnect now in the card.
* Bad, because a future wrap of the segmented control in a left-aligned
  parent would keep the test green while both controls sit left. The test
  compares the two widgets to each other, which is the grouping that was
  asked for.

### Confirmation

```text
cd apps/mobile
flutter test test/settings_screen_test.dart --name "Reconnect now is centered under the mesh/relay control"
flutter test test/settings_screen_test.dart
```

The centering test must have failed on `centerLeft` (segmented center `500.0`
vs reconnect center `152.65` on the 1000-wide test viewport) and pass on
`Alignment.center`.

## Pros and Cons of the Options

### A — Center Reconnect now under the Mesh/Relay control (chosen)

* Good, because it is the layout the owner asked for.
* Good, because it is one constant on a widget that already exists.
* Neutral, because the button stays intrinsically sized; it does not grow to
  the segmented control's width.

### B — Stretch both controls to the card's content width

* Good, because both edges would line up.
* Bad, because a full-width Reconnect now is a stronger visual weight than
  an outlined action under a two-segment control, and the owner asked for
  centering, not stretching.

### C — Same row as the segmented control

* Good, because the action would sit next to the path it applies to.
* Bad, because a phone-width Route card already holds Mesh, Relay, and the
  reconnect label; crowding that row is a different composition than the
  owner asked for.

### D — Leave `Alignment.centerLeft`

* Good, because it matches leading-edge list rows in the same card.
* Bad, because the segmented control is not a leading-edge row; it is a
  centered shrink-wrapped control, so left-aligning the action under it is
  the misalignment that was reported.

## More Information

* Related: [0062-MADR-phone-transport-selection.md](0062-MADR-phone-transport-selection.md)
  (D6: Route section, transport control, Reconnect now). This record does
  not amend 0062.
* Implementation: [0172-PLAN-settings-reconnect-now-centers-under-mesh-relay.md](0172-PLAN-settings-reconnect-now-centers-under-mesh-relay.md).
