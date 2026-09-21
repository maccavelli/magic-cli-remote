---
status: proposed
date: 2026-09-21
decision-makers: Project Owner
consulted: none
informed: none
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Phone chat: large markdown tables need a scroll container, not column wrap

## Context and Problem Statement

The Flutter phone app at `apps/mobile` renders assistant chat through one
engine — `flutter_markdown_plus` 1.0.12 on `markdown` 7.3.1, configured with
`MarkdownStyleSheet.fromTheme(theme).copyWith(...)` for paragraphs, headings,
inline code, fenced code blocks, blockquotes and links. Nothing is configured
for tables: no `tableColumnWidth`, no `tableBorder`, no `tableCellsPadding`,
no `tablePadding`, no `tableScrollbarThumbVisibility`, no custom `table`
builder. (`chat_bubble.dart:737-763`.)

Tables therefore render with the package's defaults: equal-width columns via
`IntrinsicColumnWidth` and no scroll wrapper. On a phone-portrait surface the
result, measured in the live chat, is that a markdown table with a few
columns of prose produces **very tall, very narrow cells** — the column gets
crushed, the cell text wraps aggressively, and a 6-row × 4-column table can
occupy the full height of a chat bubble. Wide tables with code-like content
(id paths, file paths, hashes) instead *expand the whole bubble* off-screen,
because there is no horizontal scroll constraint. Both outcomes are
unreadable on the phone.

The upstream rendering engine is not the cause; the cause is that nothing in
the chat stylesheet owns the overflow. `flutter_markdown_plus` 1.0.12 already
ships a built-in solution for one column-width mode (`FixedColumnWidth` →
`SingleChildScrollView` wrapper, since `flutter_markdown` 0.7.3), and a
follow-up PR (`flutter_markdown` #8526, merged 2025-01-28) extended the same
behaviour to `IntrinsicColumnWidth` with a configurable
`tableScrollbarThumbVisibility`. **Neither path is reachable from the
config we currently pass**, because we pass no `tableColumnWidth` at all, so
the table builder never enters the wrapper branch.

### What was measured, not assumed

All claims below are anchored in code already in the tree, in the upstream
package's public docs, or in the linked upstream issues / PRs.

| Claim | Source |
| --- | --- |
| Library, version | `apps/mobile/pubspec.yaml:41-42` (`flutter_markdown_plus: ^1.0.12`, `markdown: ^7.3.0`); `apps/mobile/pubspec.lock:342-348, 648-655` (1.0.12 / 7.3.1) |
| One render path, no overrides | `apps/mobile/lib/features/chat/chat_bubble.dart:802-819` (`MarkdownBody`, only a `pre` builder) |
| Stylesheet has no `table*` fields | `chat_bubble.dart:737-763` — `_sheetFor` configures `p/h1/h2/h3/code/codeblock/blockquote/a` only |
| `flutter_markdown_plus` defaults are GFM | package README (pub.dev): "GitHub Flavored Markdown by default — headings, bold/italic, strikethrough, blockquotes, lists, and horizontal rules … Tables with customisable borders, cell alignment, and per-column widths." |
| Default column sizing is `IntrinsicColumnWidth` with no scroll wrapper | `MarkdownStyleSheet.fromTheme` defaults — `IntrinsicColumnWidth()`, no `Scrollbar` parent for the default branch |
| The package already supports the fix for one column mode | `flutter_markdown` PR #6983 (merged 2024-06-26): wraps the `Table` in `Scrollbar(SingleChildScrollView(scrollDirection: Axis.horizontal))` when `styleSheet.tableColumnWidth is FixedColumnWidth` |
| Extended to the default (Intrinsic) mode | `flutter_markdown` PR #8526 (merged 2025-01-28): same wrapper for `IntrinsicColumnWidth`, plus `tableScrollbarThumbVisibility` |
| Origin of the wrap request | `flutter/flutter#129052` (closed by bot, but the PRs above realise it) |
| No Go-side markdown rendering | `grep` across `*.go` for `goldmark`/`markdown.New`/`markdown.Render`/`MarkdownRenderer` → zero hits; the daemon forwards raw assistant text verbatim |
| Existing tests cover tables | `apps/mobile/test/chat_render_test.dart` and `streaming_markdown_test.dart` — zero hits for `table` |
| MADR 0057 / MADR 0018 context | `docs/chat-performance.md:75,79`: "Replacing `flutter_markdown_plus` unless profiling after Phase B still pins it (MADR 0018 D11)" and "Markdown engine swap (E5 — re-evaluate only if profiles pin parse cost)" — engine swap is explicitly out of scope |

### Findings

**F1 — Tables fall through to package defaults with no overflow owner.**
No `tableColumnWidth` is set (`chat_bubble.dart:737-763`), so the default
branch renders the table unwrapped: equal `IntrinsicColumnWidth` columns,
no horizontal scroll, no scrollbar, no max-width clamp. On a narrow
viewport, prose cells wrap aggressively and produce very tall rows; code-like
content (paths, hashes, IDs) keeps the column at its intrinsic width and
pushes the whole table wider than the bubble.

**F2 — The fix the package already ships is one stylesheet field away.**
`flutter_markdown` 0.7.3 (and therefore `flutter_markdown_plus` 1.0.x,
which is the maintained fork of the same code) wraps the table in a
horizontal `SingleChildScrollView` whenever `styleSheet.tableColumnWidth`
is set to a `FixedColumnWidth` (PR #6983), and from PR #8526 also for
`IntrinsicColumnWidth` — the very default we use. The wrapper gives the
table its own horizontal scroll container with an optional visible thumb.
No engine swap, no new dependency, no new builder.

**F3 — `IntrinsicColumnWidth` (the current default) is the wrong policy
once the wrapper is in place.** With a horizontal scroll container, the
table should size to its **content**, then the container clips and
scrolls. `FlexColumnWidth(1.0)` per column (equal flex) is the policy
GitHub uses for GFM tables; combined with the wrapper it produces a
natural-width table that scrolls when wider than the bubble and shrinks to
fit when narrower. Switching `tableColumnWidth` to
`const FlexColumnWidth(1.0)` (or per-column flex) plus letting the
wrapper own the overflow gives the behaviour we want without a single
line of custom widget code.

**F4 — Tables need visual chrome that matches the existing code blocks.**
`chat_bubble.dart` already gives fenced code blocks a rounded
`BoxDecoration` (`codeblockDecoration`), inner padding (`codeblockPadding`),
and a horizontal scroll affordance via the package's default code builder.
A table that scrolls must not look like a wall of text: rounded border,
subtle surface tint, and an always-on horizontal scrollbar thumb on
mobile so the affordance is visible (most users will not try horizontal
swipe on a markdown table). The wrapper the package provides exposes the
`thumbVisibility` flag for this reason.

**F5 — Cell text needs `overflow-wrap: anywhere`-class behaviour for code.**
Even inside a scrollable table, a long unbroken identifier (a hash, a
Windows path, a JSON pointer) can refuse to wrap inside its cell and
inflate the column past its neighbours, making the table look uneven. The
Markdown package's cell widget is a plain `Text`; wrapping behaviour is
controlled at the cell level via `softWrap` (default true) and at the
`Text` level via `overflow` (default `clip`). Setting a per-cell
`TextStyle` with `overflow: TextOverflow.ellipsis` and a max line count
prevents pathological inflation while keeping the cell readable. We do not
need `overflow-wrap: anywhere` here because the wrapper already owns
horizontal overflow at the table level — the cell is constrained, the
table scrolls.

**F6 — Tests do not exercise table rendering.** `apps/mobile/test/` has
no golden or widget test for a markdown table in the chat bubble. Without
one, this fix is invisible to CI and a regression in the wrapper or
column policy would not be caught until a user hits it on a phone.

**F7 — The streaming markdown helper (`streaming_markdown.dart:21`) does
not buffer pipes (`|`).** A pipe-delimited table in flight renders as a
plain paragraph until the delimiter row (`|---|---|`) lands, then snaps to
a table. This is a streaming artifact, not a layout artifact, and it is
already correct for the chosen option (the table appears whole at the
moment the delimiter closes). Out of scope here, mentioned so a future
plan does not mistake it for the bug being fixed.

## Decision Drivers

* **Phone portrait is the primary surface.** Every chosen option is
  evaluated against what fits in a ~360 dp wide bubble, not against a
  tablet or desktop.
* **No engine swap.** `docs/chat-performance.md:75,79` makes engine
  replacement a non-goal pending post-Phase-B profiling. The chosen
  option must use the package we already have.
* **Match the existing chrome.** A scrollable table should look like a
  chat-bubble block, not like raw HTML — same rounded border and surface
  tint as the fenced code block.
* **Visible affordance.** Users do not horizontally swipe a markdown
  table by reflex. The scrollbar thumb must be visible on mobile so the
  scroll behaviour is discoverable.
* **Tests pin the behaviour.** Without a widget test, the wrapper can
  silently regress on a future package bump.

## Considered Options

* **A — Use the package's built-in `IntrinsicColumnWidth` + scroll wrapper
  (chosen).** Set `tableColumnWidth: const IntrinsicColumnWidth()` on the
  stylesheet so PR #8526's wrapper branch is taken; add cell padding,
  border, and `tableScrollbarThumbVisibility: true`; add a widget test.
* **B — Set `FixedColumnWidth(width)` per column.** Same wrapper path
  (PR #6983), but requires hand-picking a column count and width that
  works across every assistant reply. A 4-column table and a 12-column
  table cannot share one policy.
* **C — Render tables as a custom Flutter widget (`MarkdownElementBuilder`
  for `table`/`thead`/`tbody`/`tr`/`th`/`td`).** Most control, most
  surface area, must re-implement header striping, alignment, GFM
  alignment colons, and selection. Diverges from every other
  chat-renderer project surveyed (Markwon, react-markdown, smalldocs,
  kmesh, LibreChat, Docusaurus MDX) that wraps rather than rebuilds.
* **D — Stack-as-cards on narrow viewports.** Each row becomes a card;
  each cell becomes a label/value pair. This was tried by an internal
  platform team and abandoned (per the `cr0x.net` write-up cited under
  Evidence): it loses the side-by-side comparison that on-call readers
  need, and it broke `scope="row"` semantics for screen readers. Not a
  fit for a chat transcript, where a table is often the comparison.

## Decision Outcome

Chosen: **Option A**, with one stylesheet change, one chrome change, and
one test.

### The decisions

* **D1 — Set `tableColumnWidth: const IntrinsicColumnWidth()` on the chat
  stylesheet.** This is the only stylesheet field required to enter the
  scroll-wrapper branch the package already implements. Combined with
  the wrapper, the table sizes to its content, the wrapper clips it to
  the bubble width, and a horizontal scrollbar appears for anything
  wider. Closes **F1**, **F2**; takes the same wrapper branch **F3**
  describes.
* **D2 — Enable `tableScrollbarThumbVisibility: true`** on the chat
  stylesheet. Mobile users do not reflexively swipe a table horizontally;
  the thumb is the affordance. Closes **F4**.
* **D3 — Style `tableBorder`, `tableCellsPadding`, `tablePadding`, and
  `tableHead` on the stylesheet** to match the existing fenced-code
  chrome: rounded border, subtle surface tint (use
  `theme.colorScheme.surfaceContainerHigh` — the same tint as
  `codeblockDecoration` at `chat_bubble.dart:746-748`), inner padding
  consistent with the rest of the bubble. `tableHead` gets
  `fontWeight: FontWeight.w600` (the package's default) and the same
  surface tint as the body so the header reads as a header without a
  separator line. Closes **F4**.
* **D4 — Pin cell text wrapping with `TextStyle.overflow` and `TextStyle.height`**
  in the cell builder so a long identifier cannot inflate its column past
  the others. Use `overflow: TextOverflow.ellipsis` with a sensible max
  line count (start at 8 — the package's default has no cap, which is
  what produces the "very tall" outcome the bug report describes).
  Closes **F5**.
* **D5 — Add a widget test in `apps/mobile/test/` that renders a wide
  markdown table inside a `MarkdownBody` styled with the chat stylesheet
  and asserts**: a `Scrollbar` is present, a `SingleChildScrollView` with
  `Axis.horizontal` wraps the `Table`, and the table is constrained to
  the available width. The test must run on the existing test target
  (no live-tag). Closes **F6**.

### Consequences

* Good, because the wrapper is the same fix used by every comparable
  project surveyed (Markwon's `HorizontalScrollView` + `TableLayout`,
  LibreChat's overflow wrapper, Markpad's `overflow-x: auto` + sticky
  header, kmesh's `overflowX: auto` on the `table` MDX component,
  smalldocs' `.md-table-scroll`). We are not inventing a layout.
* Good, because the stylesheet change is one field; the wrapper and
  scrollbar are package code; the chrome is a few more stylesheet
  fields. No new dependencies, no new files in `lib/`, no engine swap.
* Good, because the package's existing wrapper means `selectable: true`
  text selection (already enabled for finalized messages at
  `chat_bubble.dart:804`) continues to work — the wrapper does not break
  selection inside cells, and the scroll controller is the package's own.
* Bad, because switching from unwrapped `IntrinsicColumnWidth` to wrapped
  `IntrinsicColumnWidth` changes the visual height of every existing
  table in the chat: narrow tables shrink to their content width (good,
  expected), but a table that previously *filled* the bubble width will
  now be narrower and have empty space to the right. This is the
  intended outcome of the bug fix; it will look like a regression to
  anyone who only saw wide tables. The PLAN must call this out so it is
  not "fixed" by reverting.
* Bad, because the test pins package behaviour. If a future
  `flutter_markdown_plus` release changes the wrapper branch (e.g.
  drops `IntrinsicColumnWidth` support, requires
  `tableScrollbarThumbVisibility` to enable the wrapper), the test
  will catch the regression but the user-visible fix will silently break
  until the test runs in CI. Acceptable: the test runs in CI.
* Neutral, because cell-level ellipsis with a max-line cap means very
  long cell content is no longer fully visible without horizontal scroll.
  This is a deliberate trade — the alternative was "the column inflates
  and the table becomes a wall of vertical text". Long cell content is
  rare in our assistant outputs, and a horizontal scrollbar (D2) makes
  the overflow discoverable.

### Confirmation

```text
flutter test apps/mobile/test/chat_table_render_test.dart     # D5, must be green
flutter test apps/mobile/test/                                # whole suite, green
flutter analyze apps/mobile/                                  # no new warnings
git grep -n 'tableColumnWidth' apps/mobile/lib                # exactly one hit in chat_bubble.dart
flutter build apk --debug                                     # Android phone build still succeeds
phone, manually                                               # a 4-column wide table scrolls inside its bubble; narrow tables shrink to content; vertical height of a 6×4 table is no longer than the equivalent text-only reply + ~50%
```

## Pros and Cons of the Options

### A — Built-in `IntrinsicColumnWidth` + scroll wrapper (chosen)

* Good, because the wrapper is package code we already get.
* Good, because no new dependencies, no `MarkdownElementBuilder` to
  maintain, no risk of breaking selection or alignment colons.
* Good, because it matches the upstream package's own guidance and the
  behaviour of every comparable project surveyed.
* Bad, because it requires `flutter_markdown_plus` ≥ 1.0.x (we have
  1.0.12 — fine) **and** a stylesheet field we have never set. The test
  pin is on the package version, not the stylesheet.

### B — `FixedColumnWidth(width)` per column

* Good, because it was the first path the package shipped (#6983,
  2024-06), so it is the most-tested upstream.
* Bad, because a single fixed width is wrong for both a 3-column table
  (wastes space) and a 12-column table (still wider than the phone).
* Bad, because it forces the chat to assume a column count the assistant
  never declares.

### C — Custom table widget

* Good, because any layout is reachable.
* Bad, because re-implementing GFM tables means handling header striping,
  alignment colons (`---:`, `:---:`, `:---`), cell merging, code spans
  inside cells, and selection — all of which the package does today.
* Bad, because every comparable project surveyed wraps rather than
  rebuilds, so the maintenance burden is also uniquely ours.

### D — Stack-as-cards on narrow viewports

* Good, because a 3-row table becomes 3 readable cards.
* Bad, because the comparison a table exists to provide is destroyed.
  On-call readers, the very users the survey write-up cites as
  abandoned-the-cards, were the reason the team reverted.
* Bad, because `scope="row"` is lost and screen readers announce
  label/value pairs without the column context — a regression for
  accessibility we would have to ship a separate fix for.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Library and version | `apps/mobile/pubspec.yaml:41-42`; `apps/mobile/pubspec.lock:342-348, 648-655` |
| One render engine, no `table*` stylesheet fields | `apps/mobile/lib/features/chat/chat_bubble.dart:737-763, 802-819` |
| `flutter_markdown` 0.7.3 added horizontal scroll for `FixedColumnWidth` tables | upstream PR flutter/packages#6983 (commit 9fc8627, 2024-06-26) |
| `IntrinsicColumnWidth` horizontal scroll, configurable thumb | upstream PR flutter/packages#8526 (merged 2025-01-28); CHANGELOG entries 0.7.3 and later |
| Origin of the wrapper request | flutter/flutter#129052 |
| Comparable project: Markwon | noties.io Markwon docs, `ext-tables` + `recycler-table`; default `HorizontalScrollView + TableLayout` |
| Comparable project: LibreChat | danny-avila/LibreChat#13535 (issue) → #13543 (PR) — wide markdown table contained inside message |
| Comparable project: Markpad | alecdotdev/Markpad#206 — `display: block; overflow-x: auto; max-width: 100%` for `.markdown-body table` |
| Comparable project: smalldocs | espressoplease/smalldocs commit `b33d4b1` — `.md-table-scroll { overflow-x: auto }` wrapper inserted at render time |
| Comparable project: kmesh | kmesh-net/website#307 — MDX `table` component wrapped in `overflowX: auto` container |
| Card-on-mobile failure mode | cr0x.net "Responsive Tables for Technical Docs", 2025-10-14 — internal platform team shipped and reverted |
| No engine-swap decision | `docs/chat-performance.md:75,79` (MADR 0018 D11, MADR 0057 non-goal E5) |
| No Go-side markdown rendering | repo-wide `grep` for `goldmark`/`markdown.New`/`markdown.Render` — zero hits |
| No existing table test | `apps/mobile/test/chat_render_test.dart`, `streaming_markdown_test.dart` — zero hits for `table` |

### Related records

* **0018** — Mobile chat performance assessment; non-goal E5 (engine swap)
  anchors the constraint that we stay on `flutter_markdown_plus`.
* **0057** — Mobile chat performance action plan; H-2 size tiers and the
  cache strategy in `_AssistantMarkdown` are the surface this MADR
  extends, not replaces.

### Open questions for the plan

1. **Should the table chrome use the same tint as the fenced code block
   (`surfaceContainerHigh`)?** They are both "block-level rendered
   content" — visually grouping them is reasonable. The alternative is a
   distinct tint so a table does not look like a code block. Owner
   preference; the PLAN picks and a screenshot pin covers the call.
2. **Max lines per cell.** D4 picks 8 as the starting cap. The right
   value depends on the actual chat bubble width — too low and prose
   tables get clipped mid-paragraph; too high and the "tall rows" bug
   partially returns. The PLAN should run a small fixture sweep
   (3-column prose, 5-column with IDs, 10-column sparse) and pick the
   smallest cap that does not clip the prose fixture.
3. **Selection across cells.** `MarkdownBody(selectable: true)` already
   handles selection across the bubble. The wrapper is a
   `Scrollbar(SingleChildScrollView)` inside the table widget — does
   long-press selection still span cells in the same row, or does the
   horizontal scroll gesture intercept? The PLAN verifies this in the
   widget test, because losing selection is a worse regression than the
   bug being fixed.
4. **Streaming behaviour.** F7 notes the streaming helper does not buffer
   pipes. With the new wrapper, an in-flight pipe-delimited table is a
   plain paragraph (no scroll wrapper, no table widget) until the
   delimiter row lands; at that point the whole table snaps into view
   wrapped. The widget test must cover the finalised case, but a
   streaming assertion would protect against a future change that wraps
   mid-stream and produces a half-wrapped table. Defer unless the test
   is cheap.
