---
status: proposed
date: 2026-09-21
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0164 — Phone chat: large markdown tables need a scroll container, not column wrap

Implements [0164-MADR-phone-markdown-table-mobile-rendering.md](0164-MADR-phone-markdown-table-mobile-rendering.md) decisions D1–D5, closing findings F1–F6.

## Goal

A markdown table rendered inside a phone-portrait chat bubble:

* **Scrolls horizontally inside its own wrapper** when wider than the bubble, instead of either wrapping into very tall rows or pushing the whole bubble off-screen.
* **Shrinks to its content** when narrower than the bubble, instead of stretching to fill it.
* **Has a visible horizontal scrollbar thumb** on mobile so the affordance is discoverable.
* **Visually matches the existing fenced-code chrome** (same surface tint, same rounded border) so a table does not look like a different document type.
* **Never inflates a single column past the others** because of one long unbroken identifier.
* **Is pinned by a widget test** that fails if any of the above stops being true.

Observable end-state: `flutter test apps/mobile/test/chat_table_render_test.dart` is green and asserts the wrapper, scroll axis, and thumb visibility; `flutter analyze apps/mobile/` is clean; `flutter test apps/mobile/test/` is green.

## Scope

### In scope (the only files any phase may touch)

* `apps/mobile/lib/features/chat/chat_bubble.dart` — extend `_sheetFor` with the four table stylesheet fields (D1, D2, D3, D4).
* `apps/mobile/test/chat_table_render_test.dart` — new file, the widget test that pins D1, D2, D4, D5 and the wrapper behaviour.
* `apps/mobile/test/chat_render_test.dart` — read for fixture parity only; not modified by any phase.

That is the entire surface. No `pubspec.yaml` change (the package version is already pinned in `pubspec.lock`), no new `MarkdownElementBuilder`, no `lib/features/chat/streaming_markdown.dart` change, no Go-side change, no other widget file touched.

### Out of scope

* **Engine swap** to Markwon, `flutter_markdown`, or anything else. `docs/chat-performance.md:75,79` rules this out as a non-goal pending post-Phase-B profiling. Anchored by MADR 0018 D11.
* **Streaming-time table buffering** in `streaming_markdown.dart`. F7 is parked in the MADR; the streaming behaviour already produces a table the moment the delimiter row lands, which is correct.
* **Tap-to-scroll / tap-to-expand affordance** beyond the package's scrollbar thumb. The thumb alone is sufficient; a custom gesture is a future polish.
* **Card-on-mobile layout** (Option D in the MADR). Decision is in the MADR; not re-opened here.
* **Dark-mode-specific tweaks** beyond what `MarkdownStyleSheet.fromTheme(theme)` already provides. Phase 2 picks a tint from `theme.colorScheme` that resolves correctly in both modes.

## Stability rule

Every phase ends with all of the following green, run from the repository root on Windows Git Bash:

```bash
cd apps/mobile && flutter analyze                                   # no new warnings; baseline today: clean for chat_bubble.dart and chat_render_test.dart
flutter test test/chat_render_test.dart                              # existing chat rendering still green
flutter test test/streaming_markdown_test.dart                       # streaming helper still green
flutter test test/chat_table_render_test.dart                        # the new test, green from P2 onward
flutter test                                                        # whole mobile suite, green
dart format --output=none --set-exit-if-changed lib/features/chat/chat_bubble.dart test/chat_table_render_test.dart
```

`dart format` runs on the two files any phase touches. `flutter analyze` is run because `apps/mobile/analysis_options.yaml` enables `flutter_lints`, `unawaited_futures`, `strict-casts`, `strict-raw-types` — the existing bar.

**Pre-add gate.** No Go file is staged in this plan (the changes are all `.dart`), so `scripts/go-precheck.sh` does not run; the file-format and analyze commands above are the full gate. If a future phase needs a Go file, `make pre-add-check FILES="…"` applies.

**Commit discipline.** One commit per phase. The commit message comes from the global `prepare-commit-msg` hook (`git commit --no-edit`); the agent does not pass `-m`/`-F`/`--message`.

**No `git push`** in this plan. The owner pushes explicitly when they choose.

## Cross-cutting contracts

* **C1 — The stylesheet change is the only `lib/` change.** Phases 1, 2, 3 add `.copyWith(...)` fields to the existing `_sheetFor` and a single cell-style builder inside the existing `MarkdownBody` construction site. No new file in `lib/`, no new import, no `MarkdownElementBuilder` for `table`. Phases that violate this are out of scope.
* **C2 — The widget test is the only new test file.** Phase 2 creates `chat_table_render_test.dart`; no other test file is added or modified.
* **C3 — Selection must still work.** `MarkdownBody(selectable: !streaming)` (`chat_bubble.dart:804`) is preserved verbatim. The widget test asserts selection by long-press on a cell text and finding the selection toolbar — the test must not relax this if the wrapper interferes.
* **C4 — The streaming markdown helper is untouched.** `streaming_markdown.dart` is not modified. The streaming-then-finalised difference (paragraph → wrapped table) is the existing behaviour and is acceptable.
* **C5 — The package version pin is untouched.** `pubspec.yaml` and `pubspec.lock` are not modified. `flutter_markdown_plus` 1.0.12 + `markdown` 7.3.1 already provides the wrapper; the plan is to *use* it, not bump to it.

**Contract most at risk under time pressure: C3.** Selection across cells under a new horizontal `SingleChildScrollView` is the one thing the test cannot be 100% sure of without a real device. If the widget test fails on C3, the resolution per the MADR's open-question #3 is to verify on a real Android emulator (`flutter run` to a connected device) before declaring the phase green — not to relax the test.

## Dependency and delivery order

P1 must land before P2; P2 must land before P3. No inter-phase dependency with any other plan. P3 is independent of P1/P2 once P2 lands (it tightens cell wrapping, which the wrapper from P1 enables).

## Implementation Steps

### P1 — Enable the package's table scroll wrapper (D1, D2, D3; closes F1, F2, F4)

**Files:** `apps/mobile/lib/features/chat/chat_bubble.dart` only.

**Change (single edit, inside the existing `.copyWith(...)` at `chat_bubble.dart:737-763`):**

Add four fields:

```dart
tableColumnWidth: const IntrinsicColumnWidth(),
tableScrollbarThumbVisibility: true,
tableBorder: TableBorder.all(
  color: theme.colorScheme.outlineVariant,
  borderRadius: BorderRadius.circular(8),
),
tableCellsPadding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
tablePadding: const EdgeInsets.only(bottom: 4),
tableHead: theme.textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
tableHeadAlign: TextAlign.left,
tableBody: theme.textTheme.bodyMedium,
```

`tableColumnWidth: IntrinsicColumnWidth()` is the trigger for the package's wrapper branch (PR #8526). `tableScrollbarThumbVisibility: true` makes the thumb visible. The border, padding, head, and head-align fields set the chrome to match the fenced-code block at `chat_bubble.dart:746-750` (`codeblockDecoration`, `codeblockPadding`) — same outline tint via `outlineVariant`, same `BorderRadius.circular(8)`, same vertical rhythm (`8`-ish px). `tableHeadAlign` is `TextAlign.left` because GFM default centering looks wrong on phone-width prose tables; the test asserts this.

**Required imports (verify before commit):** `flutter_markdown_plus/flutter_markdown_plus.dart` is already imported at `chat_screen.dart:20` and transitively at `chat_bubble.dart:802` (the `MarkdownBody(...)` construction). `IntrinsicColumnWidth` and `MarkdownStyleSheet` come from that import. No new import is added in this phase.

**Verification (P1):**

```bash
cd apps/mobile
flutter analyze                                                  # clean
flutter test test/chat_render_test.dart                           # green (regression guard for assistant markdown rendering)
flutter test test/streaming_markdown_test.dart                    # green
```

Manual check on a phone emulator is not required for P1 — P2's test is the verification. P1's commit message is "Enable flutter_markdown_plus table scroll wrapper in chat stylesheet".

**Commit boundary:** P1 lands on its own. The widget test does not exist yet; the only verification is that existing tests stay green.

### P2 — Pin the wrapper, scrollbar, and chrome with a widget test (D5; closes F6, anchors F1–F4)

**Files:** `apps/mobile/test/chat_table_render_test.dart` (new). No other file modified.

**Test structure (single file, three `testWidgets`):**

1. **`renders a wide table wrapped in a horizontal SingleChildScrollView`** — pumps a `MaterialApp` containing a `MarkdownBody` styled with `_FakeChatStyleSheet.build()` (a helper in the test file that returns the same `MarkdownStyleSheet` shape `_sheetFor` produces, derived from `Theme.of` for the test's `MaterialApp`). Markdown body:

   ```text
   | Column A | Column B | Column C |
   | -------- | -------- | -------- |
   | long prose text that should not wrap aggressively on a phone | another long prose cell | third long prose cell |
   | another row | second row | third row |
   ```

   Container width is constrained to `360.dp` (the smallest current Android phone width we target). Assertions:
   * `find.byType(Scrollbar)` is present (the package's wrapper parent).
   * `find.byType(SingleChildScrollView)` is present.
   * The first `SingleChildScrollView` widget's `scrollDirection` equals `Axis.horizontal`.
   * `find.byType(Table)` is present (the table is still rendered, just wrapped).
   * `find.textContaining('long prose text')` finds exactly one widget (the cell text is still rendered, not dropped).

2. **`renders a narrow table without a scrollbar visible (scrollable wrapper still wraps, but no overflow)`** — pumps the same setup with a 2-row × 2-column table whose longest cell fits in 360 dp. Assertions: `find.byType(SingleChildScrollView)` is present (wrapper always wraps in the IntrinsicColumnWidth branch), but the `Scrollbar`'s controller's `position.maxScrollExtent` is `0` (the table fits, no scroll needed).

3. **`preserves text selection across cells`** — pumps a `MarkdownBody(selectable: true, ...)` with a 2-cell table, simulates a long-press on the first cell's text via `tester.longPress(...)`, and asserts that a `SelectionArea`-style selection toolbar is found (`find.byType(AdaptiveTextSelectionToolbar)` or the `TextSelectionControls`-related widget). If the wrapper interferes with selection, this test fails and the phase does not merge.

**Fixture helper:** a top-level `_FakeChatStyleSheet` class in the test file mirrors `_sheetFor(context)` shape using `MarkdownStyleSheet.fromTheme(ThemeData.light())` then `.copyWith(...)` with the exact fields P1 added. The test does *not* import `_sheetFor` from `chat_bubble.dart` (it is private); duplication is acceptable here because the test is the contract.

**Container constraint:** wrap the `MarkdownBody` in a `SizedBox(width: 360, child: MarkdownBody(...))` to simulate phone width. Add `MediaQuery(data: MediaQueryData(size: Size(360, 800)), child: ...)` so any package that reads viewport size gets the phone value.

**Verification (P2):**

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed test/chat_table_render_test.dart
flutter analyze
flutter test test/chat_table_render_test.dart    # green; the three testWidgets pass
flutter test                                     # whole suite, green
```

**Negative-test verification (mandatory before declaring P2 green):** Run the test once with `tableColumnWidth` commented out (or temporarily set to `null`) and confirm all three `testWidgets` fail. The MADR's "A check is not trusted until it has been seen to fail" rule applies. Use a copy of the test file in `apps/mobile/test/_tmp/` (gitignored under the existing `.gitignore` if present, otherwise deleted after the experiment) for the negative run; do *not* dirty the working tree by editing the committed test file. Capture the failing output to a file, then delete the temp file and the experiment log.

If any of the three `testWidgets` does not fail when the stylesheet is missing `tableColumnWidth`, the test is not asserting the right thing; rewrite the assertion before declaring the phase green. The most common silent failure is `find.byType(SingleChildScrollView)` matching the package's other code-block wrappers — the fix is to scope the find with `find.descendant(of: find.byType(MarkdownBody), matching: find.byType(SingleChildScrollView))`.

**Commit boundary:** P2 lands on its own. P3 is independent and may follow.

### P3 — Cap cell text so a single identifier cannot inflate its column (D4; closes F5)

**Files:** `apps/mobile/lib/features/chat/chat_bubble.dart` only.

**Change:** register a cell-level `MarkdownElementBuilder` for `td` and `th` in the existing `builders:` map at `chat_bubble.dart:806`. Today the map is:

```dart
builders: <String, MarkdownElementBuilder>{'pre': _CodeBlockBuilder()},
```

Add a `_TableCellBuilder` that wraps the default rendering in a constrained `Text`:

```dart
builders: <String, MarkdownElementBuilder>{
  'pre': _CodeBlockBuilder(),
  'td': _TableCellBuilder(maxLines: 8),
  'th': _TableCellBuilder(maxLines: 2),
},
```

`_TableCellBuilder` is a new private class near `_CodeBlockBuilder` (at `chat_bubble.dart:1047-1067`). It implements `MarkdownElementBuilder`, returns the default package widget for the cell (`TableRowCell`-equivalent — see "default fallback" below) but wrapped in a `Text` with `maxLines`, `overflow: TextOverflow.ellipsis`. Concretely:

```dart
class _TableCellBuilder extends MarkdownElementBuilder {
  _TableCellBuilder({required this.maxLines});
  final int maxLines;

  @override
  Widget? visitElementAfter(md.Element element, TextStyle? preferredStyle) {
    // The package's default cell text rendering is the union of
    // preferredStyle (tableBody / tableHead) and the element's inline
    // children rendered via WidgetSpan. Easiest deterministic fallback:
    // re-render the element's text content as a single Text inside the
    // cell's existing decoration. We do NOT do that — it would lose inline
    // code spans inside cells. Instead, we return null to let the package
    // render the default, then attach a paint-time constraint via the
    // styleSheet's tableBody/tableHead TextStyle (which already supports
    // overflow + maxLines via TextStyle.height only).
    return null;
  }
}
```

**Important correction to the MADR draft.** D4's "pin with TextStyle.overflow and TextStyle.height" claim turns out not to be reachable from `MarkdownStyleSheet.tableBody` / `tableHead`. `TextStyle` does not carry `maxLines` or `overflow`; those are `Text` widget properties. The cleanest deterministic pin is therefore **a new `MarkdownElementBuilder` for `td` and `th` that returns a `Text` widget directly**:

```dart
class _TableCellBuilder extends MarkdownElementBuilder {
  _TableCellBuilder({required this.maxLines});
  final int maxLines;

  @override
  bool isBlockElement() => false;

  @override
  Widget? visitElementAfter(md.Element element, TextStyle? preferredStyle) {
    // The element is a `td` or `th`; its textContent already includes the
    // inline markdown (bold, code spans, links). Re-render as plain text
    // with the style the package would have applied (preferredStyle) plus
    // a maxLines + overflow cap.
    return Text(
      element.textContent,
      style: preferredStyle,
      maxLines: maxLines,
      overflow: TextOverflow.ellipsis,
      softWrap: true,
    );
  }
}
```

The trade-off: this loses rich inline rendering inside table cells (e.g. inline `code` spans lose their background). The MADR's D4 says *"long identifier cannot inflate its column past the others"* — which is the primary goal — and accepts that *"long cell content is no longer fully visible without horizontal scroll"*. Inline formatting inside cells is rare in the assistant's outputs we render; the cap is the right priority.

If inline formatting inside cells turns out to matter in practice (Phase 2's test or a manual run surfaces it), the next iteration walks `element.children` and uses `MarkdownBody` recursively for inline content. That is P4 territory and is out of scope here.

**Verification (P3):**

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed lib/features/chat/chat_bubble.dart
flutter analyze
flutter test test/chat_table_render_test.dart    # green; the existing three testWidgets still pass
flutter test                                     # green
```

Add a fourth `testWidgets` to `chat_table_render_test.dart` (in P3, not P2):

4. **`caps a cell with a long identifier to maxLines with ellipsis`** — pumps a table whose single cell is `aaaa…` (200 `a`s). Assert `find.textContaining('aaaa')` resolves to a `Text` widget whose `maxLines` equals `8` and `overflow` equals `TextOverflow.ellipsis`. The 200-character input is rendered, but the rendered widget is constrained.

**Negative-test verification:** temporarily remove the `'td'`/`'th'` entries from the `builders` map, run the test, watch the new `testWidgets` fail (assertion is on `Text.maxLines == 8` and the default cell widget is not a `Text`), then restore.

**Commit boundary:** P3 lands on its own after P2 lands.

## Verification (whole plan)

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR | Phase |
| --- | --- | --- | --- |
| A1 | `chat_table_render_test.dart` is green; three baseline assertions (P2) plus the P3 cell-cap assertion | Confirmation, D5 | P2, P3 |
| A2 | `flutter test apps/mobile/test/` is green | Confirmation | P1, P2, P3 |
| A3 | `flutter analyze apps/mobile/` is clean | Confirmation | P1, P2, P3 |
| A4 | `git grep -n 'tableColumnWidth' apps/mobile/lib` returns exactly one hit, inside `_sheetFor` | D1 | P1 |
| A5 | `git grep -n 'tableScrollbarThumbVisibility' apps/mobile/lib` returns exactly one hit | D2 | P1 |
| A6 | Negative-test verification captured for the wrapper assertion (P2) and the cell-cap assertion (P3) | "A check is not trusted until it has been seen to fail" | P2, P3 |
| A7 | `flutter build apk --debug` from `apps/mobile` succeeds (Android phone build still compiles with the new builder) | Confirmation | P3 |
| A8 | Manual: a 4-column prose table in a chat reply on a phone emulator scrolls horizontally inside its bubble; the table height is no greater than the equivalent text-only reply + ~50% | Confirmation | P3 |

**Criterion most likely to be quietly dropped under pressure: A8.** It is the only criterion that requires a real Android emulator, and a reviewer under time pressure will trust the widget test instead. **Do not declare the plan complete without running the chat on an emulator with the fixture.** `flutter run -d <device>` against an Android emulator started via `docs/ops-android-emulator.md`, paste a 4-column table reply, scroll the table horizontally, confirm thumb visibility and vertical height. The script in `docs/ops-android-emulator.md` is the one to follow.

## Rollout and Rollback

**Rollout.** Each phase lands on `main` as one commit; no release branch, no tag, no `git push`. The mobile app is built from `main` for internal builds; the next tagged build picks up all three phases. No migration, no feature flag, no compatibility shim — the stylesheet change is purely additive (new fields in an existing `.copyWith`).

**Rollback.** Revert the commit. Because each phase is additive on `chat_bubble.dart`'s existing `.copyWith(...)`, a `git revert <sha>` of any single phase restores the prior behaviour without affecting any other phase or any other widget. The test file added in P2 is self-contained and revert removes it cleanly. No data migration to undo.

**Risk if the package bumps.** If `flutter_markdown_plus` is upgraded past a version that drops `IntrinsicColumnWidth` from the wrapper branch (per the MADR's Consequences, "Bad" bullet 2), the widget test in P2 must be updated to set `FlexColumnWidth(1.0)` or `FixedColumnWidth(...)` explicitly. The package bump is a separate plan; this plan does not assume one.

## Deferred (named, so they are not mistaken for oversights)

* **Inline formatting inside table cells** (P3 trade-off). If a future review surfaces an assistant reply where inline `code` or **bold** inside a cell matters, the resolution is a `MarkdownElementBuilder` for `td`/`th` that walks `element.children` and uses `MarkdownBody` recursively for inline content. Deferred because it is a polish, not a correctness fix, and the cell-cap is the primary goal.
* **Tap-to-scroll affordance** beyond the scrollbar thumb. A custom "swipe to scroll" hint or a "scroll for more →" overlay is a UX polish, not a correctness fix.
* **Sticky first column** for very wide tables (so the leftmost column — usually a row label — stays visible while the rest scrolls). Common in data-table UIs; not present in any of the comparable projects surveyed; would require a custom builder for `th` and `td` with positioning logic. Out of scope here.
* **Dark-mode-specific tint overrides.** Phase 1 uses `theme.colorScheme.outlineVariant` and `theme.colorScheme.surfaceContainerHigh`, which resolve correctly in both modes. If a manual dark-mode run reveals the chrome is wrong, that is its own plan.
* **Streaming-time pipe buffering** in `streaming_markdown.dart` (F7). The streaming behaviour (table appears whole at delimiter-row arrival) is correct today. Deferred unless a future bug report shows the snap is jarring.
* **Card-on-mobile layout** (Option D in the MADR). Decision is in the MADR; not re-opened.

## Execution record (filled in by the executor)

_To be appended after each phase lands._
