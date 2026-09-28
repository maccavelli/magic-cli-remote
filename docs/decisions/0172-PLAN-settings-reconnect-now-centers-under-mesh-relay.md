---
status: completed
date: 2026-09-28
associated-madr: "0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0172 — Settings Reconnect now is centered under the Mesh/Relay control

Associated MADR: [0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md](0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md)

Implements D1–D2, closing F1–F2.

## Goal

On Settings → Connection & security → Route, when the Mesh/Relay segmented
control is shown, **Reconnect now** shares that control's horizontal center.

Observable end-state: `flutter test test/settings_screen_test.dart --name "Reconnect now is centered under the mesh/relay control"` is green, and that test has been seen to fail on `Alignment.centerLeft`.

## Scope

### In scope

* `apps/mobile/lib/features/settings/settings_screen.dart` — the `Align`
  wrapping **Reconnect now**.
* `apps/mobile/test/settings_screen_test.dart` — one widget test that pins
  the two controls' horizontal centers.

### Out of scope

* 0062 transport policy (when the segmented control appears, forced
  reconnect, probe chips).
* Other Settings actions (Recheck, Re-pair, Connect mode).
* Visual chrome (button style, icons, copy).

## Implementation Steps

### P1 — Center the button and pin it (D1, D2; closes F1, F2)

1. In `_buildRouteSection`, change the Reconnect now `Align` from
   `Alignment.centerLeft` to `Alignment.center`.
2. Add `testWidgets('Reconnect now is centered under the mesh/relay control')`
   next to the existing Route tests in `settings_screen_test.dart`. Pump the
   dual-transport fixture (`relayUrl` / `relayHostId` / `relayAuthority`
   present, both probes up). Compare
   `tester.getRect(SegmentedButton<TransportMode>).center.dx` with the
   `OutlinedButton` ancestor of `find.text('Reconnect now')`, using
   `moreOrLessEquals(..., epsilon: 0.5)`.
3. Fail-first: run that test against `Alignment.centerLeft` and capture the
   failure. Then apply step 1 and re-run.
4. `dart format` the two files. Run the full `settings_screen_test.dart`.

**Verification (P1):**

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed \
  lib/features/settings/settings_screen.dart \
  test/settings_screen_test.dart
flutter test test/settings_screen_test.dart --name "Reconnect now is centered under the mesh/relay control"
flutter test test/settings_screen_test.dart
```

**Commit boundary:** one commit after P1 is green, via `git commit --no-edit`.
No `git push` in this plan.

## Verification

* Centering test fails on `centerLeft` and passes on `Alignment.center`.
* Full `settings_screen_test.dart` stays green.
* Dual-transport Route behaviour (select Relay, tap Reconnect now, forced
  transport) is unchanged.

## Rollout and Rollback

Phone app only. Revert the `Align` constant and the new test.

## Execution record

P1 landed in the working tree on 2026-09-28, before this pair was written
(see Deviations). Uncommitted at the time of this record; the owner did not
ask for a commit in that turn.

### Fail-first (step 3, `Alignment.centerLeft` still in place)

```text
Expected: 500.0 (±0.5)
  Actual: <152.6500015258789>
   Which: 152.6500015258789 is not in the range of 500.0 (±0.5).
The test description was:
  Reconnect now is centered under the mesh/relay control
```

Test viewport: width 1000, dpr 1 (the existing `pumpSettings` size).

### After D1

* `Align` at `_buildRouteSection` uses `Alignment.center`.
* `dart format` on the two files: `Formatted 2 files (0 changed)`.
* `flutter test test/settings_screen_test.dart --name "Reconnect now"`:
  3 tests passed (presence, centering, forced transport).
* `flutter test test/settings_screen_test.dart`: 34 tests passed.

## Deviations

* **2026-09-28 — pair written after the mutation.** AGENTS.md requires the
  MADR then the PLAN, then mutate after approval. The owner asked to center
  the button; the code and test landed; the owner then asked for this pair.
  The files named in P1, the fail-first output, and the passing suite are
  unchanged by writing the record. No further code change is in this plan.
