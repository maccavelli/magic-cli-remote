---
status: accepted
date: 2026-08-29
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Move the chat session controls below the composer and give them one card idiom

## Context and Problem Statement

On the codex chat screen the app bar is full enough that the back arrow is
hidden. The controls crowding it — permissions mode, Plan/Default, thinking
level — are also the ones users reach for mid-session, and each opens a
different kind of surface. The owner asked for three things: move these
controls to icons under the composer, give every one of them the same card
when tapped, and make the thinking card say plainly when a provider cannot
change level mid-session.

The work looks like a layout change. Most of it is, but two of the four
findings below are behavioural defects that the redesign would otherwise
carry forward, and one of them is a shipped regression.

### What was measured, not assumed

**The app bar carries six action widgets, three of them variable-width text.**
`chat_screen.dart:2327-2470`, alongside a two-line title (session name, then
`provider cwd`):

| # | Widget | Source | Renders as |
| --- | --- | --- | --- |
| 1 | `_ContextUsageChip` | `:2331` | chip, self-hiding |
| 2 | `_CollaborationSelector` | `:2334` | **text** chip — "Plan" / "Default" |
| 3 | `_ModeSelector` | `:2341` | **text** chip — "read-only", "full access" |
| 4 | `_ThinkingSelector` | `:2352` | icon + **text** + caret — "medium" |
| 5 | `IconButton` (agent settings) | `:2363` | icon |
| 6 | `PopupMenuButton` (session actions) | `:2368` | icon |

`AppBar` lays actions out at their intrinsic width and gives the remainder to
the title and `leading`. Three text chips whose width depends on the *selected
value* — `full access` is eleven characters — is what squeezes the back arrow.

**Codex is the only provider that populates both mode surfaces.**
`collaborationModes` comes from `codex/collaboration.go:250` (Plan / Default);
`modes` comes from `codex/mode.go:43-78` (`default`, `read-only`, `auto`,
`full access`). Both are non-empty only for codex, so only codex renders chips
2 *and* 3 — the "duplicated twice" the owner reports.

**They are not duplicates, and the UI gives the user no way to learn that.**
The two controls are orthogonal: one governs whether the agent edits at all,
the other governs what it may do without asking. `_ModeSelector` receives
`permissionsLabel: _provider == 'codex'` (`:2346`), and that flag changes only
the **`tooltip`** (`:3189`) and the chip's label text. A tooltip requires hover;
on a touch device it is unreachable. So codex presents two adjacent chips of
identical construction whose distinction is, in practice, undiscoverable.

**Four adjacent controls use three interaction idioms.**

| Control | Idiom | Source |
| --- | --- | --- |
| Permissions mode | `PopupMenuButton` + `CheckedPopupMenuItem` | `:3187-3226` |
| Collaboration | `PopupMenuButton` + `CheckedPopupMenuItem` | `:3262-3287` |
| Thinking level | `InkWell` → `showDialog(SimpleDialog)` + `ListTile` | `:3397-3455` |
| Agent settings | bottom sheet (`_showConfigSheet`) | `:2366` |

**The destination the owner asked for already exists, with precedent.**
`chat_screen.dart:2993` is already `Row(key: ValueKey('composer-actions'))`,
holding attach-image, attach-audio, workspace, shell and share `IconButton`s.
Its comment records why it exists:

```text
// Action affordances live on their own row: sharing one Row
// with the field squeezed the prompt to nothing once the
// workspace/shell/share icons shipped (0112 amendment D1).
```

The same failure mode, one row up. This record extends that row rather than
inventing a place.

**A house card system already exists and is in use.**
`lib/features/widgets/option_picker_sheet.dart` (644 lines) exports
`PickerSheetLayout`, `PickerSheetHeader`, `PickerCatalogView` and
`showOptionPicker`; `model_picker_sheet.dart` consumes them. Unification is
therefore mostly *adoption of an existing pattern*, not new design.

**Grok's spawn-only lock is stale, and the app states something false.**
`chat_screen.dart:3391`:

```dart
bool get _spawnOnly => provider.toLowerCase() == 'grok';
```

On tap it refuses and shows *"Grok applies thinking level at session start —
start a new session to change it."* But grok is acpagent-backed
(`grok/grok.go:102-106`, `Provider = acpagent.Provider`), and
`acpagent/thinking.go:38-40` says:

> SetThinkingLevel applies grok 1.0.5 session/set_model
> `_meta.reasoningEffort` and confirms via session/resume (MADR 0106).
> **It does not return ErrThinkingLevelFixed.**

Grok gained mid-session thinking changes in 0106. The app still blocks them
and tells the user a falsehood.

**No provider returns `ErrThinkingLevelFixed`, and nothing advertises the
capability.** The sentinel is declared (`provider/provider.go:44`) and handled
(`session/commands.go:1372`, rendering a "new sessions only" notice), but
acpagent explicitly does not return it and `opencode/thinking.go:71` says it
"is never returned by this provider". The comment at `session/commands.go:1346`
still asserts *"Providers that lock the level at spawn (grok) return
ErrThinkingLevelFixed"* — contradicted by the acpagent source above.

**The session-meta wire carries the level but not its mutability.**
`thinking_level` travels through `protocol/messages.go:353`,
`session/manager.go:89`, `session/store.go:29` and `provider/provider.go:778`,
and lands in Dart at `AgentSessionMeta.thinkingLevel`
(`data/protocol/models.dart:598`). There is no companion flag.

**`/thinking` availability is a type assertion, not a capability.**
`session/commands.go:67`:
`_, state.Ops[command.OpSetThinkingLevel] = sess.(provider.ThinkingSession)`.
The chat screen gates the selector on that command's `available` flag
(`chat_screen.dart:2349-2351`). This answers "can this session set a level at
all", never "does setting it take effect now".

**Ten icons do not fit at the default `IconButton` size.** The owner set a
capacity target of ten. `composer_layout_test.dart:19-20` fixes the reference
width — *"A 360dp-wide logical surface: the width the defect was reported on"* —
and the composer's padding is `EdgeInsets.fromLTRB(12, 4, 12, 12)`, so 24dp is
spent before any icon:

| Surface | Available | Budget per icon at n=10 |
| --- | --- | --- |
| 320dp (smallest) | 296dp | 29.6dp |
| **360dp (reference)** | **336dp** | **33.6dp** |
| 392dp (Pixel) | 368dp | 36.8dp |
| 430dp (large) | 406dp | 40.6dp |

Flutter's `IconButton` defaults to `kMinInteractiveDimension` — 48×48, 24dp
glyph, 8dp padding. Ten of those is **480dp against 336dp available: 144dp
over, 43%**. `VisualDensity.compact` only reaches 40dp (400dp total), still
64dp over. So the target is unreachable without setting `iconSize`, `padding`,
`minimumSize` and `tapTargetSize` explicitly; it is not a matter of choosing
tidier glyphs.

**A fixed compact width does not survive the smallest phone.** 32dp × 10 =
320dp fits 360dp comfortably (16dp slack) but exceeds 320dp's 296dp. A single
hardcoded size therefore either wastes the reference width or breaks the
narrow one.

**`chat_screen.dart` is 3689 lines.** All four selectors are private widgets
declared at the bottom of that file (`:3110`, `:3166`, `:3233`, `:3376`).

**Relevant tests already exist**: `mode_selector_dangerous_test.dart`,
`thinking_picker_ui_test.dart`, `collaboration_mode_test.dart`,
`composer_layout_test.dart`, `resolve_displayed_mode_test.dart`,
`default_session_mode_test.dart`, `session_mode_dangerous_test.dart`.

### Findings

**F1 — The app bar overflows because three of its six actions are
value-width text.** The back arrow is the element that loses. Codex is the
worst case because it is the only provider lighting up all six.

**F2 — Codex's two mode chips are visually identical and semantically
different, and the only thing distinguishing them is a tooltip no touch user
can reach.** This is a discoverability defect, not merely clutter; moving the
chips without separating their meaning would preserve it.

**F3 — Three idioms for four adjacent controls.** Nothing about the current
code makes a consistent card cheap by accident; consistency has to be built.

**F4 — The place and the pattern both already exist.** The composer-actions
row (`:2993`) and `option_picker_sheet.dart` mean this work is mostly
relocation and adoption. Estimating it as new design would be wrong.

**F5 — The app blocks grok's mid-session thinking changes and displays a
false explanation.** A shipped regression against MADR 0106, independent of
any redesign. It is in this record because the redesign rewrites exactly this
code path, and shipping the new card over the stale check would re-lay the
same defect in a nicer wrapper.

**F6 — There is no source of truth for "thinking level is fixed for this
session".** The hardcoded provider string is wrong (F5); the sentinel is
reactive and unreturned; no wire capability exists. The owner's banner
requirement cannot be satisfied correctly by any existing signal.

**F7 — `ErrThinkingLevelFixed` is dead machinery with a stale comment
asserting otherwise.** It costs nothing today but it actively misinforms —
`session/commands.go:1346` names grok as a provider that returns it, and grok
does not.

**F8 — The ten-icon target is a sizing decision, not a glyph decision.**
Default `IconButton`s overflow the reference width by 43% at n=10, and no
choice of icon changes that. The row needs an explicit density budget, and a
single fixed width cannot serve both 320dp and 430dp. Capacity must therefore
be *derived* from available width rather than asserted.

**F9 — Meeting the target costs tap-target width, and that trade-off has a
floor.** Holding the full 48dp height while narrowing the box to ~33dp keeps
the row inside WCAG 2.2 SC 2.5.8 (minimum 24×24) with margin, but falls under
Material's own 48×48 guidance on the horizontal axis. This is a real
concession, not a free optimisation, and it is the reason a lower bound on
icon width has to be stated rather than left to arithmetic.

## Decision Drivers

* The back arrow must be reachable on every provider, at every selected value.
* Controls that mean different things must *look* different; controls that
  behave the same must look the same.
* A card must state what a provider cannot do, rather than letting the user
  discover it by a tap that fails or silently does nothing.
* Capability belongs on the wire, where the daemon knows it — not in a Dart
  string constant that goes stale silently, as it already has.
* Adopt the existing picker and composer-row patterns; do not invent a third.
* A layout change must not quietly carry a behavioural defect forward.

## Considered Options

Two axes were decided by the owner on 2026-08-29; both are recorded with the
alternatives so the reasoning survives.

**Axis 1 — where the "fixed level" fact comes from:**

* **A1 — Advertise a wire capability (chosen).**
* **A2 — Use the `ErrThinkingLevelFixed` sentinel reactively.**
* **A3 — Fix the hardcoded provider list only.**

**Axis 2 — how codex's two mode surfaces are presented:**

* **B1 — Two icons with distinct meanings (chosen).**
* **B2 — One combined "Agent mode" card with two sections.**
* **B3 — Defer the choice to a mock-up phase.**

## Decision Outcome

**Chosen: A1 + B1.**

A1 is the only option that can render the banner *before* the user acts. A2
places the explanation after a failed tap and, as F6 records, would show
nothing at all today because no provider returns the sentinel — the banner
would be unreachable code shipped as a feature. A3 is the smallest change and
does fix F5, but it re-creates the staleness that caused F5 in the first place:
a Dart constant asserting a Go behaviour, with nothing to keep them in step.
The cost of A1 is a protocol addition and a Go change, which widens this record
beyond Flutter; that is accepted because the alternative is a UI that lies, and
it has already lied once.

B1 keeps the daemon's two orthogonal controls orthogonal. B2 is genuinely
attractive — one icon, one mental model, and it reads as the "unified" thing
the owner asked for — but it merges controls the daemon treats as independent,
so the card must then explain the independence in prose, which is the F2
problem relocated rather than solved. Distinct icons solve F2 directly: the
reason the chips are confusing today is that they look the same, and two
different icons is the smallest change that stops them looking the same.

### The decisions

**D1 — The session controls move from `AppBar.actions` to the existing
composer-actions row.** Permissions mode, collaboration mode and thinking level
become `IconButton`s in `Row(key: ValueKey('composer-actions'))`
(`chat_screen.dart:2993`), joining the attach/workspace/shell/share icons.

**D2 — What stays on the app bar.** The context-usage chip and the session
actions `PopupMenuButton` stay; they are status and navigation, not
per-turn session controls. The agent-settings `IconButton` moves with the
group. After this, no `AppBar` action's width depends on a selected value
(F1).

**D3 — Icons, not text, in the composer row.** Each control is an icon whose
*selected value* is conveyed by tint and by the card, never by chip width.
Where a state is dangerous (`SessionMode.dangerous`, driven by the daemon flag
per `_ModeChip.isDangerous`) or is Plan, the icon keeps the existing colour
treatment — that signalling is load-bearing and is not lost in the move.

**D4 — Distinct icons per control (B1).** Three separate affordances,
each opening its own card:

| Control | Icon | Card title |
| --- | --- | --- |
| Permissions mode | shield family (`Icons.shield_outlined`) | Permissions |
| Collaboration | plan/clipboard family (`Icons.assignment_outlined`) | Collaboration |
| Thinking level | `Icons.psychology_outlined` (already in use, `:3477`) | Thinking |

Exact glyphs are the plan's to settle against the Material set; the constraint
is that permissions and collaboration must not read as variants of one icon.
An icon is rendered only when its surface is non-empty, so non-codex providers
show fewer icons rather than disabled ones.

**D5 — One card idiom for all three, built on `option_picker_sheet.dart`.**
Every card uses `PickerSheetLayout` + `PickerSheetHeader`, presents options as
a selection list with the current value checked, and applies on tap. The
`SimpleDialog` in `_ThinkingSelector` and both `PopupMenuButton`s are retired
as presentation (F3). This is adoption of the existing pattern, not a new one
(F4).

**D6 — The dangerous-mode confirmation survives verbatim.**
`_confirmDangerousMode` (`:3337`) still runs before arming a mode with
`dangerous: true`, from inside the card. A redesign is not a licence to make
auto-approve one tap cheaper.

**D7 — Add a session-meta capability for thinking mutability (A1).** A boolean
travelling beside `thinking_level` on the same structures
(`protocol/messages.go`, `session/manager.go`, `session/store.go`,
`provider/provider.go`, and `AgentSessionMeta` in Dart). Its exact name and
whether it is a bool or a richer enum is the plan's first decision; it must be
**additive and absent-tolerant**, since an older daemon will not send it and
the app must degrade to "assume mutable, report failure if the daemon
disagrees" rather than to a false banner.

**D8 — The thinking card renders a banner, not a refusal, when the level is
fixed.** When the capability says immutable, the card still opens and still
shows the ladder — with the options non-selectable and a banner stating the
provider applies the level at session start. The user learns *what the levels
are* and *why they cannot pick one*, in one surface. No toast, and no tap that
appears to work.

**D9 — Remove the hardcoded `_spawnOnly` provider check (F5).** Grok's
mid-session changes are permitted. This is a behaviour fix and must be
verifiable on its own, not folded invisibly into the layout work.

**D10 — Correct the stale sentinel comment and decide the sentinel's fate
(F7).** `session/commands.go:1346` must stop naming grok. Whether
`ErrThinkingLevelFixed` is retained as the reactive backstop behind D7's
capability, or removed as dead machinery, is an explicit choice the plan makes
and records — not something left ambiguous.

**D11 — No behaviour changes ride along.** Which options exist, what they do,
the dangerous confirmation, and every daemon call stay as they are. The
exceptions are exactly D8 and D9, both named, both separately verified.

**D12 — The row holds ten icons at 360dp, by a derived density budget (F8).**
Per-icon width is computed from the available width, not hardcoded:

```text
width = clamp(available / count, 28dp, 40dp)
glyph = 20dp        tap height = 48dp (full)
padding = zero      tapTargetSize = shrinkWrap
```

Measured against that rule:

| Surface | n | Per icon | Total | Fits |
| --- | --- | --- | --- | --- |
| 360dp | 10 | 33.6dp | 336dp | yes (exact) |
| 360dp | 9 | 37.3dp | 336dp | yes |
| 320dp | 10 | 29.6dp | 296dp | yes |
| 320dp | 12 | 28.0dp (floored) | 336dp | **no** → D13 |

The 40dp ceiling stops six icons sprawling on a tablet; the 28dp floor is F9's
stated limit and is what makes the 320dp/12 case overflow rather than shrink
into unusability.

**D13 — Past the floor, the row scrolls; it never wraps and never truncates.**
When `count × 28dp` exceeds the available width the row becomes horizontally
scrollable with the leftmost icons being the session controls. Wrapping to a
second row is rejected: it moves the composer up unpredictably as providers
change, which is the same class of layout surprise as F1. Truncation is
rejected outright — a hidden control is the defect being fixed.

**D14 — Icons are chosen for silhouette separation at 20dp, and the
permission glyph carries its own state (F2).** At 20dp a glyph is read by
outline, not detail, so the vocabulary is picked for distinct shapes:

| Slot | Icon | Silhouette |
| --- | --- | --- |
| Attach image | `add_photo_alternate_outlined` | rectangle |
| Attach audio | `audiotrack_outlined` | note |
| Workspace | `folder_outlined` | folder |
| Shell | `terminal_outlined` | bracket |
| Share | `ios_share` | arrow |
| **Permissions** | shield family, state-varying | angular shield |
| **Collaboration** | `checklist` / `edit_outlined` | horizontal rules |
| **Thinking** | `psychology_outlined` | round head |
| Agent settings | `tune` | sliders |
| *(spare)* | — | — |

The permissions glyph changes with the posture rather than only its tint —
`gpp_good` read-only, `shield_outlined` default, `gpp_maybe` auto,
`bolt` full access. This is what makes the control intuitive at a glance and
is the direct answer to F2: two chips that looked identical become two icons
that cannot be confused, one of which also states its own setting. Tint from
C4 stays, so the signal survives for anyone who reads colour faster than form.

### Consequences

* Good: the back arrow is reachable on every provider (F1), because no app-bar
  action's width tracks a selected value any more.
* Good: codex's two mode surfaces become visually distinct (F2) instead of
  merely being moved.
* Good: one idiom, one card system, one place — and it is the system and place
  the app already has (F3, F4).
* Good: a false statement is removed from the UI and a real capability is
  unblocked (F5).
* Good: the "fixed level" fact becomes a daemon-owned fact, so the next
  provider change updates the app without a Dart edit (F6).
* Bad: this is no longer a Flutter-only change. A protocol field means Go, Dart,
  and an old-daemon compatibility path. Accepted per the A1 reasoning.
* Bad: moving controls off the app bar costs discoverability for users who know
  where they are today. Mitigated by tinting the icons on non-default states so
  a changed mode is visible without opening anything.
* Neutral: `chat_screen.dart` grows or shrinks depending on whether the cards
  are extracted to their own files. The plan should extract them; a 3689-line
  file is not improved by three more widgets.

### Confirmation

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

```text
codex chat screen, every mode value  -> back arrow visible and tappable
AppBar.actions                       -> no widget whose width tracks a value
composer-actions row                 -> permissions, collaboration, thinking icons
each card                            -> PickerSheetLayout + PickerSheetHeader
thinking card, immutable provider    -> ladder shown, disabled, banner present
thinking card, grok                  -> selectable; no "new sessions only" text
dangerous mode from the card         -> still confirms before arming
older daemon without the new field   -> no banner, no crash, level still settable
```

## Pros and Cons of the Options

### A1 — Advertise a wire capability (chosen)

* Good: the only option that can render the banner before the user acts.
* Good: capability lives where it is known and cannot silently rot in Dart.
* Good: fixes F6 for every future provider, not just for grok today.
* Bad: protocol change; spans Go and Dart; needs an absent-field path.

### A2 — Reactive sentinel

* Good: no protocol change; the plumbing exists already.
* Bad: the banner appears only after a tap that failed — the owner asked for a
  card that *states* the limitation.
* Bad: no provider returns the sentinel today (F6), so this ships a banner that
  can never appear.

### A3 — Fix the hardcoded list only

* Good: smallest change, and it does fix F5.
* Good: unblocks the layout work immediately.
* Bad: re-creates the exact staleness that produced F5 — a Dart constant
  asserting a Go behaviour with nothing keeping them in step.
* Bad: leaves the banner as dead code until someone re-introduces a wrong
  constant.

### B1 — Two icons, distinct meanings (chosen)

* Good: attacks F2 at its cause — the chips are confusing because they look
  alike.
* Good: preserves the daemon's orthogonality without prose.
* Bad: two icons on a row that already holds up to five.

### B2 — One combined card

* Good: fewest affordances; reads as the most "unified" answer to the ask.
* Good: one place to look for "how is this agent behaving".
* Bad: merges two independent controls, so the card must explain the
  independence in words — F2 relocated, not solved.
* Bad: a single icon cannot tint for two independent states without ambiguity.

### B3 — Defer to a mock-up phase

* Good: decides with drawings rather than argument.
* Bad: the deciding argument (F2's cause) is already available; deferring buys
  a phase and no new information.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Six app-bar actions | `chat_screen.dart:2327-2470` |
| Three are value-width text chips | `:3227` `_ModeChip`, `:3287` `_CollaborationChip`, `:3479` |
| `permissionsLabel` changes only the tooltip | `:2346`, `:3189` |
| Codex is the only two-surface provider | `codex/collaboration.go:250`, `codex/mode.go:43-78` |
| Three idioms | `:3187`, `:3262`, `:3397`, `:2366` |
| Composer-actions row exists, with rationale | `:2990-3037` (0112 amendment D1) |
| House card system | `lib/features/widgets/option_picker_sheet.dart` |
| Hardcoded grok spawn-only | `:3391` |
| Grok is acpagent-backed | `grok/grok.go:102-106` |
| Grok supports mid-session changes | `acpagent/thinking.go:38-40` (MADR 0106) |
| opencode never returns the sentinel | `opencode/thinking.go:71` |
| Stale sentinel comment naming grok | `session/commands.go:1346` |
| Sentinel declared / handled | `provider/provider.go:44`, `session/commands.go:1372` |
| `thinking_level` wire path | `protocol/messages.go:353`, `session/manager.go:89`, `session/store.go:29`, `provider/provider.go:778` |
| Dart session meta | `data/protocol/models.dart:598` |
| `/thinking` availability is a type assertion | `session/commands.go:67` |
| `chat_screen.dart` is 3689 lines | `wc -l` |

### Related records

* [0112](0112-MADR-opencode-1.18.21-surface-parity.md) — its amendment D1 created the
  composer-actions row for the same overflow reason; D2 extends that row.
* [0106](0106-MADR-grok-1.0.5-surface-parity.md) — gave grok mid-session thinking
  changes. F5 is the app failing to follow.
* [0052](0052-MADR-thinking-levels-and-settings.md) — introduced `/thinking` and the A6 chip
  this record moves.
* [0047](0047-MADR-codex-default-mode.md) — D4, "never invent a selection from list
  order alone"; `resolveDisplayedMode` must survive the rewrite.

### Open questions for the plan

1. What is the new field called, and is a bool sufficient? "Fixed at spawn" and
   "takes effect next turn" (codex, per `codex/commandtable.go:18-20`) are
   different, and a bool flattens them. If the card should distinguish "applies
   from the next turn", the field needs to carry that.
2. Is `ErrThinkingLevelFixed` retained behind the capability as a backstop, or
   removed (D10)? Retaining costs a stale-comment fix; removing costs a
   reactive safety net for daemons the app has misjudged.
3. Should the three cards be extracted from `chat_screen.dart` into
   `lib/features/chat/` files of their own? The Makefile's `NEW_DART_FILES`
   list carries a 90% line-coverage floor for new Dart files (MADR 0112 A13) —
   extraction therefore has a testing cost the plan must budget for, and the
   list must be updated if files are added.
4. ~~Do any icons collide with the five already in the composer row?~~
   **Answered by measurement, and the target was raised to ten.** Default
   `IconButton`s overflow the 360dp reference by 43% at n=10 (F8); D12's
   derived budget fits ten at both 320dp and 360dp; D13 handles the overflow
   case. The remaining sub-question is empirical and belongs to the plan: does
   a 20dp glyph in a 33.6dp box still read as distinct on a real screen, or
   only in the arithmetic? `composer_layout_test.dart` proves the geometry;
   only a device proves the legibility.
5. Does the 28dp floor (F9) need an accessibility escape hatch — for example,
   falling back to D13's scroll at large system display sizes rather than
   holding ten in view? Icons do not scale with `textScaleFactor`, so the
   arithmetic is unaffected, but a user who has enlarged their display has
   asked for bigger targets and D12 would keep giving them 33.6dp.

## Observed — execution results (2026-08-29)

**P8's device pass ran, and the geometry passed while the vocabulary failed.**
The owner ran the build on a phone: the controls work, the back arrow is
reachable, ten fit. The glyphs were rejected — "the small icon choices suck…
it needs to be a showcase, not an aside."

That is the outcome D14 and open question 4 anticipated in as many words: the
arithmetic proves the geometry, only a screen proves the legibility, and if the
glyphs fail the **vocabulary** changes rather than the budget. D12's density
rule is untouched by this record.

### Why the set failed, measured

The row mixed **three icon idioms**, which no amount of per-glyph choice would
have reconciled:

| Idiom | Icons |
| --- | --- |
| Material *outlined* | `folder_outlined`, `terminal_outlined`, `audiotrack_outlined`, `add_photo_alternate_outlined`, `psychology_outlined`, `shield_outlined` |
| Material *filled* | `tune`, `checklist`, `bolt`, `edit_off` |
| *iOS* | `ios_share` |

Outlined and filled glyphs sit at different optical weights, and `ios_share` is
from a different design language entirely. The owner named `folder_outlined` as
the one that reads well — it is from the outlined family, which is the coherent
subset.

## Amendment — 2026-08-29: a curated Lucide set, and permission labels without colour

### D15 — The icon vocabulary is Lucide, bundled as SVG assets

Supersedes D14's Material vocabulary; D14's *principle* — silhouette
separation at 20dp, permissions carrying its own state — stands.

Lucide is 1,500+ icons on a strict 24px grid with a 2px stroke and rounded
joins: one grid, one stroke, one voice. It is the family `folder_outlined`
gestures at, drawn consistently. It is **ISC licensed** (notice bundled at
`assets/ui_icons/LICENSE`) and actively maintained.

**Bundled as assets, not as a package.** `flutter_svg` is already a dependency
and `assets/vendor_icons/` is an established bundled-monochrome-SVG pipeline
(MADR 0082 D5), so this adds **no new dependency** and ships only the ~13
glyphs actually used rather than a 1,500-icon font. Considered and rejected:
`phosphor_flutter` (MIT, six weights, but last published two years ago — a
stale runtime dependency on a project that pins its toolchain deliberately),
and a curated Material Symbols subset (safest, but it is the family the device
pass just rejected).

### D16 — Permission labels are normalised for display

The daemon sends raw ids as names — `default`, `read-only`, `auto`,
`full access`, `build`, `plan` (`codex/mode.go:46-72`) — and the card renders
them verbatim, lowercase and inconsistently punctuated. Normalise to Title Case
at the presentation layer only. **The wire value is never rewritten**: ids
remain what the daemon said, and only the label the user reads changes.

### D17 — No leading colour on any permission row; danger reads monochrome

No row carries a leading tint. A mode the daemon flags `dangerous` is marked by
a trailing monochrome glyph and a plain description of what it does
("Approves every request — no prompts"), in the same ink as every other row.

**The safety control is untouched.** `confirmDangerousMode` still gates arming
(D6), and the composer icon still changes glyph with posture (D14's principle,
now `shield` / `shield-check` / `shield-alert` / `shield-off`). What is removed
is colour-coding *inside the card*, not the confirmation and not the
at-a-glance signal C4 protects — those move to form rather than hue.

Considered and rejected: fully flat rows with nothing distinguishing
auto-approve until the dialog. It is the cleanest look and it is the one option
that weakens C4, because a row that reads identically to "read only" until
tapped is a worse affordance than a quieter one that still says what it does.
