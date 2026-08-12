# Implement MADR 0079 — Provider/model drill-down picker

<!-- markdownlint-disable MD004 MD013 MD024 MD029 MD033 MD036 -->

Associated MADR: [0079-MADR-provider-model-drill-down-picker.md](0079-MADR-provider-model-drill-down-picker.md)

## Goal

Replace the new-session dialog's separate **Model provider (optional)** and
**Select model (optional)** fields with one **Model (optional)** field backed
by a single bottom sheet that:

1. opens on the configured/connected model providers for multi-provider agents;
2. keeps every reported provider reachable through a searchable
   **Browse all reported providers (N)…** page;
3. drills into the selected provider's existing scoped model catalog;
4. submits only an explicit model ID and optional thinking level, qualifying
   scoped custom IDs when a multi-provider agent requires that identity;
5. preserves the agent/daemon default when the user clears or makes no model
   selection; and
6. changes no daemon, protocol, credential, or persisted-data surface.

Completion means all decisions D1–D11 in MADR 0079 are implemented and pinned
by tests, `make preflight` passes, and a manual Android smoke test completes the
acceptance scenarios in this plan.

## Scope

### Files to modify

* `apps/mobile/lib/data/protocol/picker.dart`
  * Add typed accessors for provider-row `model_count` and `default_model`
    metadata. Do not change JSON or wire names.
* `apps/mobile/lib/features/widgets/option_picker_sheet.dart`
  * Extract a reusable catalog view from the existing private sheet.
  * Keep `showOptionPicker` and `PickerResult` source-compatible.
* `apps/mobile/lib/features/sessions/sessions_screen.dart`
  * Replace the two-field flow with the model drill-down entry point.
  * Change dialog caches from completed values to in-flight futures.
* `apps/mobile/test/picker_test.dart`
* `apps/mobile/test/model_picker_test.dart`
* `apps/mobile/test/model_provider_step_test.dart`
* `apps/mobile/test/sessions_screen_test.dart`
* `apps/mobile/test/thinking_picker_ui_test.dart`
* `docs/0079-MADR-provider-model-drill-down-picker.md`
  * Link this plan; do not alter D1–D11 during implementation.

### Files to add

* `apps/mobile/lib/features/widgets/model_picker_sheet.dart`
* `apps/mobile/test/model_picker_sheet_test.dart`

### Explicit non-goals

* No change under `internal/`, `cmd/`, or `apps/mobile/lib/data/ws/`.
* No new protocol verb, payload field, capability, or catalog metadata.
* No provider-auth state join and no credential form or setup action in the
  picker. Settings remains the credential-management surface.
* No change to the in-session `/model` flow, ACP config-option picker, or agent
  picker beyond preserving them through the shared-view extraction.
* No change to catalog ordering, daemon cache TTL, 500-option cap, or the
  OpenCode/Kilo default-catalog caps.
* No live CLI probe or live-tagged test. Existing wire behavior is reused, not
  extended.

## Interfaces and State Model

Implement these interfaces before integrating the Sessions screen. The names,
behavior, and fields below are fixed by this plan.

### Model picker result

In `model_picker_sheet.dart`:

```dart
class ModelPickerResult {
  const ModelPickerResult({
    required this.modelProvider,
    required this.model,
    required this.modelLabel,
    this.thinkingLevel,
  });

  final String modelProvider;
  final String model;
  final String modelLabel;
  final String? thinkingLevel;
}
```

An empty `model` is a committed clear action. A `null` result from
`showModelPicker` is cancellation. These outcomes must remain distinct.

### Model picker entry point

```dart
typedef ModelCatalogLoader = Future<PickerCatalog> Function(
  String modelProvider,
);

Future<ModelPickerResult?> showModelPicker(
  BuildContext context, {
  required String provider,
  required PickerCatalog providerCatalog,
  required ModelCatalogLoader loadModels,
  String initialModelProvider = '',
  String initialModel = '',
  String initialModelLabel = '',
  String? thinkingIntent,
});
```

`loadModels('')` means the existing unscoped/default model catalog.
`loadModels('<id>')` means the existing scoped request for that model provider.
The widget does not own RPC or cache policy.

### Picker pages

Use one modal route and one stateful sheet with exactly three logical pages:

```dart
enum _ModelPickerPage { providerMenu, allProviders, models }
```

The transition table is authoritative:

| Current page | Action | Next state or result |
| --- | --- | --- |
| provider menu | tap connected provider | models; load that provider's catalog |
| provider menu | tap browse-all | all providers |
| provider menu | close or system back | return `null` |
| all providers | tap any provider | models; load that provider's catalog |
| all providers | header/system back | provider menu |
| all providers | close or Cancel | return `null` |
| models, multi-provider flow | header/system back | provider menu; do not commit |
| models, direct one-provider flow | system back | return `null` |
| models, either flow | close or Cancel | return `null` |
| models | Select with model | return explicit `ModelPickerResult` |
| models | Clear, then Select | return `ModelPickerResult` with empty model/provider/label and null thinking level |

Do not push a second route or open a nested bottom sheet for any transition.

### Shared catalog view

Extract these package-visible widgets in `option_picker_sheet.dart`:

* `PickerSheetLayout`, which owns the existing 85%-height cap, keyboard inset,
  and safe bottom layout;
* `PickerSheetHeader`, which owns title, optional Back, source chip, and Close;
* `PickerCatalogView`, which composes those primitives with the catalog search,
  rows, empty states, custom input, and footer.

`PickerCatalogView` must support two interaction modes:

```dart
enum PickerCatalogInteraction { select, navigate }
```

Its constructor contract is:

```dart
PickerCatalogView({
  super.key,
  required PickerCatalog catalog,
  required String title,
  required PickerCatalogInteraction interaction,
  required VoidCallback onCancel,
  List<String>? initialSelected,
  bool seedCatalogDefault = true,
  String? thinkingIntent,
  ValueChanged<PickerResult>? onConfirm,
  ValueChanged<PickerOption>? onNavigate,
  VoidCallback? onBack,
});
```

Assert that `select` receives `onConfirm` and `navigate` receives `onNavigate`.
When `seedCatalogDefault` is false, an explicitly empty `initialSelected` must
remain empty; do not repopulate it from `catalog.defaultIds` in `initState`.

* `select` preserves today's radio/checkbox selection, custom-value field,
  thinking chips, Clear/Cancel/Select controls, and `PickerResult` callback.
* `navigate` preserves search, catalog order, group headings, row descriptions,
  disabled state, badges, source chip, empty/match/truncation states, and Cancel,
  but replaces radio controls with chevrons and invokes
  `ValueChanged<PickerOption>` on row tap. It must not expose a Select action or
  treat a provider row as selected state.

`showOptionPicker` remains the route wrapper for `select` mode. Existing callers
must compile unchanged.

## Implementation Steps

### Test-first execution contract

P1–P3 are test-first phases. For each phase, use this sequence exactly:

1. Run the phase's baseline command before editing tests. Fix or record any
   pre-existing failure; do not attribute it to MADR 0079.
2. Write the named new tests and test-fixture changes before changing the
   production behavior they specify.
3. Run the narrowest command containing the new tests and observe at least one
   new test fail for the intended missing API or behavior. A compile failure is
   an acceptable red state when the test references a planned interface that
   does not exist yet. A harness, analyzer, dependency, or environment failure
   is not an acceptable red state.
4. Implement the production change without weakening, skipping, or deleting
   the new expectations.
5. Run the targeted tests to green, then run the phase's regression command.

Record the red command and its intended failure in the implementation notes or
pull-request description. A phase is incomplete if its tests were only listed,
written after the behavior, skipped, or left failing.

## Phase P1 — Extract the reusable picker view without behavior changes

This phase is a pure refactor plus regression tests. Do not add the model picker
until P1 is green.

### Tests to write first

1. Establish the baseline:

   ```bash
   cd apps/mobile
   flutter test test/picker_test.dart test/model_picker_test.dart \
     test/thinking_picker_ui_test.dart
   ```

2. Add these cases before editing production code:

   * `picker_test.dart`: `navigate mode calls onNavigate without selecting`.
   * `picker_test.dart`: `navigate mode keeps search groups badges and truncation`.
   * `thinking_picker_ui_test.dart`: `clear then select returns no model or thinking level`.
   * `model_picker_test.dart`: `parses provider model count and default model metadata`.

3. Rerun the baseline command. The navigate and metadata tests must initially
   fail to compile because their planned interfaces do not exist; the clear
   test must expose stale thinking state if that path is currently reachable.
   Confirm the failures name the missing P1 behavior before proceeding.

### Production steps

1. In `picker.dart`, add read-only accessors:

   ```dart
   String get modelCount => meta['model_count'] ?? '';
   String get defaultModel => meta['default_model'] ?? '';
   ```

   Implement them against the parser/accessor expectations already added to
   `model_picker_test.dart`; do not change the metadata map or wire names.

2. In `option_picker_sheet.dart`, keep these public interfaces unchanged:

   * `PickerResult` constructor, fields, and `single` getter;
   * `showOptionPicker` parameters, defaults, and null-on-cancel contract.

   `showOptionPicker` must construct `PickerCatalogView` with
   `seedCatalogDefault: true`, preserving today's behavior.

3. Extract `PickerSheetLayout` and `PickerSheetHeader`, then move the existing
   catalog body and selection state into `PickerCatalogView`. The route wrapper
   must still apply `isScrollControlled: true` and `useSafeArea: true`; the
   extracted layout must still apply the 85%-height cap and keyboard-inset
   padding.

4. Add `PickerCatalogInteraction.navigate` as specified above. Reuse the same
   flattened `_Row` list, debounced search, `_badge`, source chip, truncation
   footer, and row text. Do not copy those implementations into the later model
   sheet.

5. Fix clear-state consistency while extracting: `_clearSelection` must call
   `_syncThinkingFromSelection` after clearing. A cleared picker must return a
   null thinking level, never the level of the previously selected model.

6. Retain the existing long-press-to-confirm behavior in `select` mode only.
   `navigate` mode has no long-press action.

### Green and regression checkpoint

Run all P1 test files after the production steps:

```bash
cd apps/mobile
flutter test test/picker_test.dart test/model_picker_test.dart test/thinking_picker_ui_test.dart
```

### Phase exit

* The targeted command is green.
* `rg -n "showOptionPicker\\(" apps/mobile/lib` still reports the five current
  production call sites plus its definition; none has changed yet.
* The rendered behavior asserted by the pre-existing picker tests is unchanged.

### P1 execution record

* 2026-08-12 baseline: the three-file Flutter test command passed 16 tests.
* Red: the same command failed because `PickerCatalogView`,
  `PickerCatalogInteraction`, `modelCount`, and `defaultModel` were absent, and
  because Clear returned the stale `high` thinking level.
* Green: the command passed 20 tests; `flutter analyze` passed and the
  `showOptionPicker` audit reported five production callers plus its definition.

## Phase P2 — Implement the model drill-down sheet

Depends on P1. Add only the widget and its isolated tests in this phase.

### Tests to write first

1. Re-run the green P1 command as the P2 baseline.
2. Create `model_picker_sheet_test.dart`, its host widget, and a fake
   `ModelCatalogLoader` that records provider IDs and can hold individual loads
   with `Completer<PickerCatalog>` instances.
3. Write every case in the **Required automated test inventory** below before
   creating `model_picker_sheet.dart`. Prefer visible page content, stable keys,
   loader records, and returned results over private-state assertions.
4. Run:

   ```bash
   cd apps/mobile
   flutter test test/model_picker_sheet_test.dart
   ```

   The suite must be red because the planned model-picker API/widget is absent.
   Confirm that the failure is caused by that missing implementation, then
   implement the production sections below.

### Provider menu construction

1. For a provider catalog with more than one option, derive:

   ```dart
   final connected = providerCatalog.options
       .where((option) => option.connected == true)
       .toList(growable: false);
   ```

   Do not sort this list. The daemon order is authoritative.

2. Render one `ListTile` per connected option:

   * title: `option.displayLabel`;
   * subtitle: join the available fragments with ` · `:
     * `<N> model` when `modelCount == '1'`;
     * `<N> models` for any other non-empty `modelCount`;
     * `default <id-with-selected-provider-prefix-removed>` when
       `defaultModel` is non-empty;
   * no subtitle when both metadata values are absent;
   * trailing chevron;
   * disabled when `option.enabled == false`.

3. Append exactly one disclosure tile:

   ```text
   Browse all reported providers (<providerCatalog.options.length>)…
   ```

   Show it even when there are zero connected rows. The provider menu is only
   reachable when the catalog contains more than one option, so the tile is
   enabled on every reachable provider-menu state.

4. When there are zero connected rows, render this text above the disclosure
   tile:

   ```text
   No configured providers were reported. Set one up in Settings or on the host, or browse all providers.
   ```

   This is guidance only; do not navigate to Settings from the modal.

### Direct-path and reopen rules

5. At sheet initialization, choose the initial page deterministically:

   * provider catalog length `0` or `1`: enter `models` and call
     `loadModels('')`; this is the direct path and has no provider-menu back
     destination;
   * provider catalog length `>1`, non-empty `initialModel`, and an
     `initialModelProvider` that exists in the provider catalog: enter `models`
     and load that provider so reopening a committed choice returns to it;
   * otherwise: enter `providerMenu` without loading a model catalog.

6. Model loading displays a centered progress indicator inside the same sheet.
   Maintain a monotonically increasing request generation. Apply a completed
   load only when the sheet is mounted, its generation is current, and its
   selected provider still matches. This prevents a late result from reopening
   a page after Back or replacing a newer provider's models.

7. Loader exceptions are handled by the Sessions-screen loader in P3, which
   returns an empty allow-custom catalog and shows a notification. The model
   sheet must render that catalog normally and must not add a second error
   notification.

   Give each loaded `PickerCatalogView` a key containing the selected provider
   and request generation. A provider change must create fresh search, custom,
   selection, and thinking state rather than reuse the previous catalog's
   `State` object.

### Default selection algorithm

8. For the direct path, preserve `showOptionPicker` semantics:

   * use `initialModel` when non-empty;
   * otherwise use the catalog's existing `defaultIds` unchanged.

   Pass the resulting list to `PickerCatalogView` with
   `seedCatalogDefault: false`; the model sheet, not the shared view, owns this
   deterministic default calculation.

9. For a scoped multi-provider path, calculate at most one initial model in this
   exact order:

   1. `initialModel`, when it is either unqualified or begins with
      `<selected-provider>/`, and it exists in the catalog or the catalog allows
      custom values;
   2. the selected provider row's `defaultModel`, when it begins with
      `<selected-provider>/` and exactly matches a catalog option;
   3. the first catalog `defaultIds` value, when it exactly matches a catalog
      option and either begins with `<selected-provider>/` or every non-empty
      catalog option ID is unqualified;
   4. no selection.

   Never preselect a qualified ID belonging to a different provider.
   Pass the calculated zero-or-one-element list with
   `seedCatalogDefault: false`, including when the result is empty.

### Result normalization

10. On Select, resolve the selected/custom ID:

    * scoped multi-provider path and ID contains no `/`: return
      `<selected-provider>/<id>`;
    * otherwise return the ID unchanged.

11. Set `modelLabel` to the selected option's `displayLabel` when the resolved
    ID came from an option. For a custom value, use the resolved ID as the
    label. For a scoped multi-provider path, return the selected provider. For
    a direct path, return `modelCatalog.modelProvider` when present and an empty
    provider otherwise. Return the picker's thinking level unchanged.

12. On an empty picker selection, return the committed-clear result:

    ```dart
    const ModelPickerResult(
      modelProvider: '',
      model: '',
      modelLabel: '',
      thinkingLevel: null,
    );
    ```

### Back behavior

13. Wrap the sheet in `PopScope` using the callback supported by the repository's
    Flutter SDK:

    * `providerMenu`: permit the route pop;
    * `allProviders`: consume the pop and set `providerMenu`;
    * `models` in a multi-provider flow: consume the pop, invalidate any active
      request generation, and set `providerMenu`;
    * `models` in a direct flow: permit the route pop.

14. Header back invokes the same state transition as system back. Close and
    Cancel always dismiss and return null. Add stable widget keys for tests:

    * `model-picker-back`
    * `model-picker-close`
    * `model-picker-browse-all`
    * `model-picker-provider-<provider-id>`

### Required automated test inventory

The new `model_picker_sheet_test.dart` must cover all of these named cases:

1. `multi-provider menu shows connected rows in catalog order`.
2. `provider menu omits count and default when metadata is absent`.
3. `provider menu formats singular plural and provider-relative default metadata`.
4. `browse-all count is dynamic and all groups and badges remain searchable`.
5. `zero connected providers keeps browse-all and setup guidance`.
6. `disabled provider row cannot navigate`.
7. `zero reported providers loads the unscoped catalog directly`.
8. `one reported provider loads the unscoped catalog directly`.
9. `reopening an existing choice loads its provider directly`.
10. `model load stays in the same sheet and shows progress`.
11. `provider tap loads its scoped catalog and returns qualified model`.
12. `scoped custom model is qualified before return`.
13. `direct-path custom model keeps existing unqualified semantics`.
14. `selected option returns display label and custom returns resolved ID label`.
15. `provider metadata default wins when valid`.
16. `cross-provider catalog default is not preselected`.
17. `unqualified scoped catalog default is accepted`.
18. `clear returns an empty committed result and null thinking level`.
19. `cancel returns null and preserves no transient provider choice`.
20. `close returns null and preserves the prior committed choice`.
21. `system back returns models to provider menu before dismissing`.
22. `header back follows the same transition as system back`.
23. `back from all providers returns to provider menu`.
24. `late model load is ignored after back`.
25. `newer provider load wins and receives fresh picker state`.
26. `thinking chips survive drill-down and return the chosen level`.
27. `empty scoped catalog keeps custom-entry empty state`.
28. `truncated scoped catalog keeps the visible truncation footer`.

Use `tester.binding.handlePopRoute()` for the system-back assertion. Do not
assert private state; assert visible page titles/rows and loader/result records.
Use provider fixtures with zero, one, and at least three options; the
multi-provider fixture must contain connected, disconnected, and disabled rows
in distinct groups. In the metadata-formatting case, use counts `1` and `2` and
a default such as `anthropic/claude` so singular/plural rendering and prefix
removal are asserted independently.

### Phase exit

```bash
cd apps/mobile
dart format lib/features/widgets/option_picker_sheet.dart \
  lib/features/widgets/model_picker_sheet.dart \
  lib/data/protocol/picker.dart \
  test/picker_test.dart \
  test/model_picker_test.dart \
  test/model_picker_sheet_test.dart \
  test/thinking_picker_ui_test.dart
flutter analyze
flutter test test/picker_test.dart test/model_picker_test.dart \
  test/model_picker_sheet_test.dart test/thinking_picker_ui_test.dart
```

All commands must pass before P3.

## Phase P3 — Integrate the new-session dialog and deterministic caches

Depends on P2.

### Tests to write first

1. Run the P2 phase-exit command as the P3 baseline.
2. Refactor `_CatalogClient` and write every case in **Required integration-test
   inventory** below before editing `_createSessionFlow`. Update the existing
   layout assertions in `sessions_screen_test.dart` at the same time.
3. Run:

   ```bash
   cd apps/mobile
   flutter test test/model_provider_step_test.dart \
     test/sessions_screen_test.dart
   ```

   The suite must be red against the old two-field dialog. Expected failures
   include the missing unified **Model (optional)** flow, obsolete field labels,
   duplicate in-flight catalog requests, or an unqualified scoped result. Do
   not proceed on an unrelated fixture or environment failure.

### Replace local state and helpers

1. In `_createSessionFlow`, retain:

   * `String model = '';`
   * `String modelProvider = '';`
   * `String? thinkingLevel;`

   Add:

   ```dart
   String modelLabel = '';
   ```

   Delete `bool? hasModelProviders`, `pickModelProvider`, and the old
   `pickModel` implementation.

2. Change the dialog caches to futures:

   ```dart
   final providerCatalogs = <String, Future<PickerCatalog>>{};
   final modelCatalogs =
       <(String provider, String modelProvider), Future<PickerCatalog>>{};
   ```

3. Replace `catalogFor` with a future-caching implementation that inserts the
   future before awaiting it. Its behavior is fixed:

   * concurrent callers for one key receive the same future;
   * success remains cached for the dialog lifetime;
   * failure shows one top notification, removes the failed future from the
     cache, and resolves all current waiters to a new empty allow-custom
     fallback catalog;
   * a later user action retries because the failed entry was removed.

   Pass an explicit fallback builder so provider and model fallbacks carry the
   correct agent-provider/model-provider scope. Do not read the mutable outer
   `provider` variable inside the generic failure path.

   The provider fallback is `PickerCatalog(provider: p, allowCustom: false)`.
   The model fallback is
   `PickerCatalog(provider: p, modelProvider: mp, allowCustom: true)`.

4. `loadModelProviders(p)` becomes a pure cached fetch of
   `client.listModels(p, scope: 'providers')`. It no longer mutates modal state.
   Keep the unawaited prefetch after agent selection.

5. Add `loadModels(p, mp)` using the tuple-keyed future cache and
   `client.listModels(p, modelProvider: mp.isEmpty ? null : mp)`.

### Add the unified picker action

6. Implement `pickModel()` as follows:

   1. snapshot the selected agent provider into a final local `p` and return if
      it is null/empty;
   2. await `loadModelProviders(p)`;
   3. return if the dialog context unmounted or the selected provider changed
      while awaiting;
   4. call `showModelPicker` with current model/provider/label/thinking intent
      and a loader closure bound to `p`;
   5. on null result, change nothing;
   6. on a committed result, set all four local fields in one `setModal` call;
   7. when result.model is empty, force modelProvider/modelLabel empty and
      thinkingLevel null even if a malformed result supplied values.

7. On agent-provider dropdown change, reset `model`, `modelProvider`,
   `modelLabel`, and `thinkingLevel`; keep the existing agent/native-session
   resets. Do not clear caches for other agent providers: their keys isolate
   them and the dialog lifetime is short.

### Replace the two fields

8. Delete the conditional **Model provider (optional)** `InputDecorator` and
   its preceding gap. Rename **Select model (optional)** to **Model (optional)**
   and bind it to the new `pickModel`.

9. Render the value in one line so field height stays aligned:

   * empty model: `Provider default` with `onSurfaceVariant` styling;
   * non-empty model and empty/equal label: `model`;
   * non-empty distinct label: `<modelLabel> · <model>`.

   Keep `maxLines: 1` and `TextOverflow.ellipsis`. Use the existing dropdown
   icon; do not add a second subtitle row.

10. Disable the row until an agent provider is selected by setting its InkWell
    callback to null. The row remains visible; D7 is resolved inside the sheet,
    not by conditionally changing dialog layout.

11. Keep `createSession` unchanged. It must still receive only `model` and
    `thinkingLevel`; do not add or infer a model-provider wire field.

### Required integration-test inventory

Refactor `model_provider_step_test.dart` rather than adding a parallel dialog
test file. Extend `_CatalogClient` to:

* record provider and model catalog requests;
* optionally gate provider/model requests with `Completer` instances;
* override `createSession` and record `provider`, `model`, and `thinkingLevel`;
* return a valid synthetic `SessionMeta` so Create completes.

Replace the old two-field assertions with these cases:

1. `dialog always renders one Model field and no Model provider field`.
2. `Model field is visible but disabled before agent provider selection`.
3. `multi-provider field opens connected provider menu`.
4. `choosing model provider then backing out submits no model override`.
5. `choosing scoped model updates field and submits qualified model`.
6. `chosen thinking level is submitted with the explicit model`.
7. `clearing a prior model submits no model or thinking level`.
8. `single-provider catalog opens model list directly`.
9. `reopening committed model returns to its scoped catalog`.
10. `provider prefetch and immediate tap share one in-flight request`.
11. `reopening completed provider and model catalogs performs no new request`.
12. `switching agent provider clears the committed model display and payload`.
13. `late provider catalog result after agent switch does not open stale picker`.
14. `provider and model caches isolate agent and model-provider keys`.
15. `failed catalog request notifies once returns fallback and retries on next open`.

Run case 15 for both cache kinds. The provider failure must yield
`PickerCatalog(provider: p, allowCustom: false)`; the model failure must retain
both `provider: p` and `modelProvider: mp` with `allowCustom: true`. For each,
hold two concurrent callers on the same failing future, assert one client
request and one notification, then reopen and assert that the client request
count increments because the failed entry was evicted.

Update `sessions_screen_test.dart`:

* replace `Select model (optional)` with `Model (optional)` in the layout label
  set;
* retain equal-height, label-alignment, and header-alignment assertions;
* add a value-overflow case proving a long `<label> · <qualified-id>` stays one
  line and does not change field height.

### Regression suite for untouched consumers

Run these targeted tests to prove the shared refactor did not change the three
remaining production uses of `showOptionPicker`:

```bash
cd apps/mobile
flutter test test/picker_test.dart \
  test/thinking_picker_ui_test.dart \
  test/model_command_test.dart \
  test/model_provider_step_test.dart \
  test/sessions_screen_test.dart
```

The current tree has no dedicated widget harness for the ACP config-select
sheet. `picker_test.dart` is therefore its shared-component regression gate;
do not create a new chat-screen ACP harness as part of MADR 0079.

Do not start this regression suite until the new P3 integration and layout tests
are green. It is the post-implementation regression gate, not a substitute for
writing the P3 tests above.

### Phase exit

* The targeted tests and `flutter analyze` pass.
* `rg -n "Model provider \\(optional\\)|Select model \\(optional\\)" apps/mobile/lib apps/mobile/test`
  returns no production UI references and no obsolete assertions.
* `rg -n "showOptionPicker\\(" apps/mobile/lib` reports only agent selection,
  ACP config selects, the in-session `/model` command, and the function
  definition; the Sessions screen no longer calls it directly.
* `git diff -- apps/mobile/lib/data/ws internal cmd` shows no protocol, client
  transport, daemon, or Go change.

## Phase P4 — Documentation and final acceptance

P4 adds no new production behavior. Do not add redundant tests in this phase;
run the complete automated suite and preserve the P1–P3 tests as the executable
specification of MADR 0079.

1. Verify MADR 0079's `plan` metadata row still links this file (the link was
   added when this plan was authored):

   ```markdown
   | plan | [0079-PLAN-provider-model-drill-down-picker.md](0079-PLAN-provider-model-drill-down-picker.md) |
   ```

2. Do not change MADR status from Proposed as part of implementation. Acceptance
   is an owner decision. If the owner accepts the decision separately, update
   status/date in a dedicated documentation change.

3. Format all changed Dart files before staging:

   ```bash
   cd apps/mobile
   dart format lib/data/protocol/picker.dart \
     lib/features/widgets/option_picker_sheet.dart \
     lib/features/widgets/model_picker_sheet.dart \
     lib/features/sessions/sessions_screen.dart \
     test/picker_test.dart \
     test/model_picker_test.dart \
     test/model_picker_sheet_test.dart \
     test/thinking_picker_ui_test.dart \
     test/model_provider_step_test.dart \
     test/sessions_screen_test.dart
   ```

4. Run the full repository gate from the repository root:

   ```bash
   make preflight
   ```

5. Review the final diff:

   ```bash
   git diff --check
   git diff --stat
   git diff -- apps/mobile/lib apps/mobile/test docs/0079-MADR-provider-model-drill-down-picker.md docs/0079-PLAN-provider-model-drill-down-picker.md
   ```

   Confirm there are no changes under `internal/`, `cmd/`, protocol docs, or
   provider-auth Settings code. Preserve unrelated working-tree changes.

## Verification

### Decision-to-test matrix

| MADR decision | Automated evidence |
| --- | --- |
| D1 | Dialog integration tests assert one Model field and no provider-only field; layout tests keep one-line equal-height rendering. |
| D2 | Model-sheet tests assert connected-only level 1, catalog order, conditional metadata, and disabled-row behavior. |
| D3 | Browse-all tests assert the dynamic count, entire returned catalog, search, groups, and unconfigured badges. |
| D4 | Scoped-load, qualified-model, custom-qualification, label, and thinking-level tests assert the result contract. |
| D5 | Provider-only/back/cancel/clear integration tests assert that only explicit models reach `createSession`. |
| D6 | Valid provider default, cross-provider default rejection, and unqualified-catalog tests pin the selection algorithm. |
| D7 | Zero/one-provider direct-path and Goose-shaped multi-provider tests pin routing. |
| D8 | Final diff audit proves no provider-auth UI/RPC change. |
| D9 | Existing picker, thinking, `/model`, agent, and ACP-config tests pass after extraction. |
| D10 | System/header-back, late-load, zero-connected, empty-catalog, and truncation tests pin navigation and honest states. |
| D11 | In-flight and completed-cache integration tests assert one request per key while active/successful and retry after failure. |

### Manual Android acceptance

Run against a daemon that exposes at least one OpenCode or Kilo provider catalog
and, if available, Goose:

1. Open **New session**, choose OpenCode or Kilo, and tap **Model**.
   * The first page contains only configured/connected providers.
   * Counts/defaults appear only on rows whose catalog supplies them.
2. Tap **Browse all reported providers (N)…**.
   * Search finds an unconfigured provider.
   * Its `not configured` badge remains visible.
3. Enter a connected provider.
   * The scoped model list loads in the same sheet.
   * Back returns to the short provider menu; a second Back dismisses.
4. Choose a non-default model and create the session.
   * The dialog shows its label and qualified ID.
   * The created session reports/uses that exact qualified model.
5. Reopen **New session**, choose a model with thinking levels, choose a level,
   and create.
   * The selected level reaches the new session.
6. Clear model selection and create.
   * The session uses the configured agent/daemon default.
7. If Goose is available, open its model picker.
   * Its current provider appears without invented count/default text.
   * Browse-all still exposes its reported providers.
8. If Codex or Grok is available, open its model picker.
   * It opens directly on models without a one-row provider page.
9. With a narrow phone viewport and a long model ID, confirm the dialog has no
   overflow and the Model field remains the same height as adjacent fields.

Record the daemon version, phone OS/API level, agent provider, selected upstream,
and selected model for any failed scenario. Do not diagnose a runtime 401/quota
failure as a picker failure unless the picker misreported catalog state; MADR
0079 explicitly distinguishes connected/current catalog state from runtime
health.

## Rollout and Rollback

### Rollout

* Ship as one mobile-client change after `make preflight` and manual Android
  acceptance. No daemon sequencing is required because every RPC and field is
  already used by the current two-step picker.
* No feature flag or data migration is required. The picker stores no state
  outside the open dialog.
* Mixed versions are safe: older phones retain the two-field UI; updated phones
  use the drill-down against the same daemon APIs.
* iOS receives the same Flutter widget when that build is produced. Android is
  the required manual acceptance platform because predictive Back is a locked
  decision and an Android standard in this repository.

### Rollback

* Roll back by shipping the previous mobile build. No daemon, credential,
  session, or local-settings migration must be reversed.
* For a source rollback, restore the two picker helpers and two dialog fields in
  `sessions_screen.dart`, remove `model_picker_sheet.dart` and its tests, and
  revert only the model-specific integration tests. The reusable
  `PickerCatalogView` extraction may remain if its regression suite is green;
  it is behavior-preserving and independently useful.
* Existing sessions created with qualified model IDs require no cleanup. Those
  IDs are already the format used by the current picker and provider adapters.

## Completion Checklist

* [ ] P1–P3 tests were written first and each phase produced an intended red result before production edits.
* [ ] P1 reusable picker extraction is green with unchanged public API.
* [ ] P2 drill-down state machine and all isolated widget tests are green.
* [ ] P3 Sessions integration, in-flight caches, and integration tests are green.
* [ ] No MADR 0079 test is skipped, disabled, weakened, or left failing.
* [ ] Existing agent, ACP-config, thinking, and `/model` picker behavior passes.
* [ ] MADR 0079 links this plan and remains decision-consistent.
* [ ] All changed Dart files are formatted.
* [ ] `make preflight` passes.
* [ ] Manual Android acceptance passes or failures are recorded with the required context.
* [ ] Final diff contains no daemon, protocol, credential, or unrelated user change.
