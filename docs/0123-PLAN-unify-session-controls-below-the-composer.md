---
status: in-progress
date: 2026-08-29
associated-madr: "0123-MADR-unify-session-controls-below-the-composer.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0123 — Session controls below the composer, one card idiom

Implements [0123-MADR-unify-session-controls-below-the-composer.md](0123-MADR-unify-session-controls-below-the-composer.md)
decisions D1–D14, closing findings F1–F9.

## Goal

The chat screen's session controls are icons under the composer, each opening
the same kind of card, and the thinking card tells the truth about what the
provider can do.

Finish line:

* on codex, at every selectable mode value, the back arrow is visible and
  tappable;
* no `AppBar.actions` widget has a width that depends on a selected value;
* permissions, collaboration and thinking each have a distinct icon in the
  composer-actions row, each opening a `PickerSheetLayout` card;
* the row holds **ten** icons at 360dp with no overflow and the prompt still
  keeping over half the width, and scrolls rather than truncating past that;
* the thinking card shows the ladder always — selectable when the session
  permits it, disabled under a banner when it does not;
* grok's thinking level is settable mid-session from the app;
* an older daemon that does not send the new field still works, with no banner.

## Scope

### In scope (the only files any phase may touch)

**Daemon (D7, D9, D10):**

* `internal/protocol/messages.go` — the new session-meta field
* `internal/session/manager.go` — populate it
* `internal/session/store.go` — persist it
* `internal/provider/provider.go` — the provider-side accessor, and
  `ErrThinkingLevelFixed`'s fate
* `internal/session/commands.go` — the stale comment at `:1346`; sentinel
  handling at `:1372` only if D10 removes it
* `internal/provider/{acpagent,codex,httpagent,opencode,fake}` — each declares
  its own mutability; **accessor only, no behaviour change**

**App:**

* `apps/mobile/lib/data/protocol/models.dart` — parse the new field
* `apps/mobile/lib/features/chat/chat_screen.dart` — app bar, composer row,
  removal of the four inline selectors
* `apps/mobile/lib/features/chat/session_controls/` — **new**, the extracted
  cards (see P5 and the coverage note) and the density-budgeted composer-actions
  row (D12, D13)
* `apps/mobile/lib/features/widgets/option_picker_sheet.dart` — only if a card
  needs a banner slot the layout does not already offer
* `apps/mobile/test/*` — the named existing tests plus new ones
* `Makefile` — `NEW_DART_FILES`, if P5 adds files

### Out of scope

* **Which options exist, and what they do.** Mode catalogues
  (`codex/mode.go`), collaboration catalogues, and thinking ladders are not
  touched. This record moves and re-presents controls; it does not change what
  they control (D11).
* **The dangerous-mode confirmation's content or threshold** (D6). It moves
  into the card unchanged.
* **The agent-settings sheet's internals.** The icon moves with the group; what
  it opens stays as it is.
* **The context-usage chip and session-actions menu** — they stay on the app
  bar (D2).
* **`chat_screen.dart`'s size in general.** Only the four selectors leave.
  A 3689-line file is a problem; it is not this record's problem.
* **Any provider gaining or losing a thinking capability.** Providers report
  what is already true of them. If a provider's real behaviour is wrong, that
  is its own record.
* **iOS/desktop layout.** Android phone is the target (0121 is the iPhone
  record).

## Stability rule

Every phase ends with:

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

and, for any phase touching Go:

```bash
go build ./...
go vet ./...
go test ./internal/session/ ./internal/protocol/ ./internal/provider/...
```

then **one commit** (`git commit --no-edit`; never `-m`).

**Local `gofmt -l` is unusable on this host** (CRLF working tree, ~500 false
hits). CI's Gofmt step is the authority. No phase may run `make fmt`.

**`git push` is not required by this plan.** Every phase verifies locally. If
the owner wants CI confirmation, that is an explicit instruction in the same
turn.

## Cross-cutting contracts

**C1 — The daemon is the only source of truth for capability.** No phase may
reintroduce a provider-name string in Dart to decide behaviour. If the app
needs to know something about a provider, the daemon says it. This is the
contract F5 was created by breaking.

**C2 — Absent field means "assume mutable".** An older daemon sends nothing.
The app must then behave exactly as it does today for a mutable provider — no
banner, no disabled list. A missing field must never render as "fixed", because
a false banner is the failure this record exists to remove.

**C3 — No behaviour rides along with the layout.** D11. The set of options, the
daemon calls, and the dangerous confirmation are unchanged. The two intended
behaviour changes (D8's banner, D9's grok unblock) are each landed in their own
phase with their own test, so a bisect can find them.

**C4 — Every control keeps its state signalling.** Dangerous and Plan states
are tinted today (`_ModeChip`, `_CollaborationChip`); after the move the icons
carry that tint. A user must still be able to see "this session auto-approves"
without opening anything.

**C5 — `resolveDisplayedMode` survives.** MADR 0047 D4 — never invent a
selection from list order. The cards call the same resolver; a card that
defaults to `modes.first` has broken it.

**C1 is the one at risk.** When a card needs to know whether to show the
banner and the daemon work is not done yet, adding
`provider == 'grok'` for "just now" is a two-line change that would pass every
test in this plan except the one that forbids it. P1 exists so the capability
lands before any card needs it.

## Dependency and delivery order

P1 (wire) → P2 (providers declare) → P3 (app parses) are strictly ordered: the
app cannot honestly render a banner before the daemon can state the fact. P4
(grok unblock) depends on P3 only because it should be verified against the
real signal rather than by deleting a check and hoping. P5–P7 are the UI move
and can only follow P3. P8 is the overflow proof and must be last, because it
is the acceptance test for the whole record.

## Implementation Steps

### P1 — The session-meta capability (D7; closes F6)

Answer MADR open question 1 **first, in writing**, before any code: is a bool
enough? Codex's `/thinking` takes effect on the *next turn*
(`codex/commandtable.go:18-20`); grok applies within the session; a
hypothetical provider locks at spawn. If the card should distinguish "applies
from your next message" from "applies now", a bool cannot carry it.

**Recommendation: a small string enum**, not a bool — `live` / `next_turn` /
`fixed`. It costs the same wire bytes, it lets D8's banner say something
accurate for codex rather than silently implying immediacy, and a bool would
have to be widened later by exactly this work.

Add the field beside `thinking_level` in `protocol/messages.go`,
`session/manager.go`, `session/store.go`, `provider/provider.go`. Additive and
`omitempty`; absent means unknown, which C2 maps to mutable.

**Verification:**

```bash
go build ./... && go vet ./...
go test ./internal/protocol/ ./internal/session/
```

A round-trip test: a payload with the field absent decodes to the "unknown"
value, and does not decode to "fixed".

### P2 — Each provider declares its own mutability (D7, D10; closes F7)

Every `ThinkingSession` implementation reports its value: acpagent (`live` —
MADR 0106), codex (`next_turn` — its own command-table comment), httpagent,
opencode, fake. **No behaviour changes**; each provider states what is already
true of it.

Then D10: fix the stale comment at `session/commands.go:1346`, which names grok
as returning `ErrThinkingLevelFixed` when `acpagent/thinking.go:40` says it does
not. Decide the sentinel's fate and record it here.

**Recommendation: retain it** as a backstop behind the capability, with a
corrected comment. It costs one comment; removing it costs the only defence
against a daemon that mis-declares, and it is already wired at `:1372`.

**Verification:** `go test ./internal/provider/...`, plus a table test asserting
every `ThinkingSession` implementation returns a non-empty value — so a new
provider cannot forget and silently inherit "unknown".

### P3 — The app parses the capability (D7; closes F6 in Dart)

`AgentSessionMeta` gains the field beside `thinkingLevel`
(`models.dart:598`). Absent → the unknown value → treated as mutable (C2).

**Verification:** `flutter test` with a decode test for three payloads: field
present and `fixed`, present and `live`, and **absent**. The absent case is C2
and is the one most likely to be written as `?? fixed` by reflex.

### P4 — Unblock grok (D9; closes F5)

Delete `_spawnOnly` (`chat_screen.dart:3391`) and its refusal branch. Thinking
selection is driven by P3's capability alone (C1).

This is a **behaviour fix and lands alone**, before any layout change, so that
"grok can now set thinking mid-session" is one revertible commit and not a line
inside a redesign.

**Verification:** `thinking_picker_ui_test.dart` gains a grok case asserting the
level is selectable and that the string "new sessions only" appears nowhere.
Existing tests must still pass unchanged.

### P5 — Extract the three cards (D5; closes F3)

New `apps/mobile/lib/features/chat/session_controls/`: one card per control,
each built on `PickerSheetLayout` + `PickerSheetHeader` + a selection list with
the current value checked (`option_picker_sheet.dart`). The permissions card
calls `_confirmDangerousMode` unchanged (D6, C3) and `resolveDisplayedMode`
(C5).

The thinking card is the one with the extra state (D8): the ladder always
renders; when the capability is `fixed` the options are disabled and a banner
states that the provider applies the level at session start. When `next_turn`,
the card says the choice applies from the next message. No toast — the card is
the explanation.

**Coverage cost, budgeted rather than discovered:** the Makefile's
`NEW_DART_FILES` carries a **90% line-coverage floor** for new Dart files
(MADR 0112 A13). Adding three files means three entries and three test files.
Update `NEW_DART_FILES` in the same commit as the extraction, not later.

**Verification:**

```bash
cd apps/mobile && flutter test
scripts/coverage-delta.sh floor --after apps/mobile --minimum 90.0 \
  --dart-root apps/mobile $(NEW_DART_FILES:%=--new-dart-file %)
```

Each card gets a widget test: renders the options, checks the current one,
applies on tap. The thinking card additionally tests all three capability
values, including the banner.

### P6 — Move the icons to the composer row (D1–D4, D12, D13, D14; closes F2, F8, F9)

Remove `_CollaborationSelector`, `_ModeSelector`, `_ThinkingSelector` and the
agent-settings button from `AppBar.actions`. Add icons to
`Row(key: ValueKey('composer-actions'))` (`:2993`), each opening its P5 card.

**Build the density budget first, then add icons to it.** D12 is a layout rule,
not a per-icon style: extract the row into a widget that measures its own
available width (`LayoutBuilder`), computes
`clamp(available / count, 28, 40)`, and applies

```dart
iconSize: 20,
padding: EdgeInsets.zero,
tapTargetSize: MaterialTapTargetSize.shrinkWrap,
minimumSize: Size(width, 48),   // full height, narrowed box (F9)
```

Default `IconButton`s overflow the 360dp reference by 144dp at n=10, so adding
icons before the budget exists means every intermediate commit is broken. Past
the floor the row scrolls horizontally (D13) — never wraps, never truncates.

Icons per D14: permissions and collaboration **must not read as variants of one
glyph** (F2's cause), and the permissions glyph varies with posture
(`gpp_good` / `shield_outlined` / `gpp_maybe` / `bolt`). Each icon renders only
when its surface is non-empty, so non-codex providers show fewer rather than
disabled ones.

C4 lives here: the dangerous and Plan tints move onto the icons, *in addition
to* D14's glyph change. An icon that renders identically whether or not the
session auto-approves has failed this phase.

**Verification:** `flutter test`, with `composer_layout_test.dart` extended to
assert the capacity target rather than infer it:

| Case | Width | n | Expectation |
| --- | --- | --- | --- |
| Capacity target | 360dp | **10** | all ten laid out, no overflow, prompt keeps > half width |
| Smallest phone | 320dp | 10 | all ten laid out, no overflow |
| Past the floor | 320dp | 12 | row is scrollable; no icon dropped |
| Codex today | 360dp | 9 | all nine, no overflow |

The n=10 case must be a real ten — pad with a test-only spare rather than
asserting nine and hoping. `tester.takeException()` must be null in every case;
a `RenderFlex` overflow throws and would otherwise pass silently in a test that
only checks widget presence.

**Then look at it on a device.** The arithmetic proves the geometry; it does
not prove a 20dp glyph in a 33.6dp box is still legible and distinct
(MADR open question 4). Run the app and confirm the nine glyphs read apart at a
glance. If they do not, D14's vocabulary changes — not D12's budget.

### P7 — Retire the old idioms (D5; closes F3)

Delete the now-unused `_ModeChip`, `_CollaborationChip` and the `SimpleDialog`
path. Confirm no `PopupMenuButton` remains for mode or collaboration.

**Verification:** `flutter analyze` reports no unused declarations;
`grep -n 'SimpleDialog\|CheckedPopupMenuItem' lib/features/chat/chat_screen.dart`
returns nothing for these controls.

### P8 — Prove the back arrow (D1, D2; closes F1)

The acceptance test for the record. A widget test on the codex chat screen at a
narrow width, iterating **every** mode value including `full access` (the
longest, and the one that provoked the report), asserting the back arrow is
present and hit-testable.

Then run the app on the device and look at it (`make profile DEVICE=…` or a
debug run). A widget test proves layout arithmetic; it does not prove the
screen looks right.

**Verification:** the new test, plus a screenshot or a stated observation from a
real device. "The test passes" is not the same claim as "the back arrow is
visible" and this phase needs both.

## Verification (whole plan)

```bash
go build ./... && go vet ./... && go test ./internal/...
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

```text
codex, every mode value       -> back arrow visible and tappable
AppBar.actions                -> no value-width widget
composer row                  -> 3 new icons, distinct glyphs, tinted on state
each card                     -> PickerSheetLayout + PickerSheetHeader
thinking card, fixed          -> ladder shown, disabled, banner
thinking card, grok           -> selectable, no "new sessions only" anywhere
dangerous mode from card      -> still confirms
daemon without the new field  -> no banner, level still settable
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Back arrow visible at every codex mode value, narrow width | D1, F1 |
| A2 | No `AppBar.actions` widget's width tracks a selected value | D2, F1 |
| A3 | Permissions and collaboration have visually distinct icons | D4, F2 |
| A4 | All three cards use `PickerSheetLayout`/`PickerSheetHeader` | D5, F3 |
| A5 | Thinking card shows the ladder + banner when fixed | D8 |
| A6 | Grok's level is settable mid-session; false text gone | D9, F5 |
| A7 | Absent capability field → mutable, no banner, no crash | D7, C2 |
| A8 | No provider-name string decides behaviour in Dart | C1 |
| A9 | Dangerous confirmation unchanged and still fires from the card | D6, C3 |
| A10 | Dangerous/Plan tints visible without opening a card | C4 |
| A11 | `resolveDisplayedMode` still used; no order-based default | C5 |
| A12 | `NEW_DART_FILES` updated; new files meet the 90% floor | 0112 A13 |
| A13 | Stale `commands.go:1346` comment corrected | D10, F7 |
| A14 | **Ten icons lay out at 360dp with no overflow** | D12, F8 |
| A15 | Ten icons lay out at 320dp; past the floor the row scrolls | D12, D13 |
| A16 | Icon boxes keep the full 48dp tap height | D12, F9 |
| A17 | Permissions glyph changes with posture, not tint alone | D14, F2 |
| A18 | Prompt field keeps > half the width with all ten on | 0112 D1 |

**A7 is the one to guard.** It is the only criterion with no visible symptom
when it is wrong — a daemon that predates P1 would show a banner that is a lie,
which is precisely the class of defect (F5) this record was written to remove,
re-created by the fix for it. It is also the easiest to write backwards, since
`?? fixed` reads more "safe" than `?? mutable` and is exactly wrong here.

A8 is second, for the reason named under C1.

**A14 is third, and it is the one most likely to be quietly downgraded.** The
codex row needs nine icons; ten is the owner's stated capacity target, with the
tenth slot deliberately spare. A phase that ships nine working icons has met
every visible requirement and will feel finished, so the n=10 case must be
written with a real tenth icon — a test-only spare — rather than asserted at
nine. Capacity that is never tested at capacity is a guess.

## Rollout and Rollback

The daemon change (P1, P2) is additive and an older app ignores the field. The
app change (P3–P8) degrades safely against an older daemon via C2. There is no
migration and no persisted-state change beyond an optional field in
`session/store.go`.

Each phase reverts independently. P4 and P6 are the two the user would notice:
P4 restores a false refusal, P6 restores the crowded app bar.

## Deferred (named, so they are not mistaken for oversights)

* **Six Windows-only test failures in `transcript_cache` and `history_replay`.**
  `PathAccessException: Access is denied` on a temp path, with mixed
  separators. Pre-existing, reproduced on a pristine `HEAD`, and invisible to
  CI because the Flutter lane is `ubuntu-latest`. Real, and squarely in the
  "never tested on Windows" territory this project is currently working
  through — but unrelated to session controls, so it wants its own record.
* **The stale CRLF working tree.** `.gitattributes` already mandates
  `eol=lf`; the checkout predates it, so `dart format` and `gofmt` both report
  files they should not. `git add --renormalize .` is the likely one-line fix.
  0118 deferred it; it is still deferred, but the cause is now known.

* **Splitting `chat_screen.dart`.** 3689 lines, with four widget classes at the
  bottom that this plan removes. That helps, but the file's size is a separate
  problem with a separate record; doing it here would bury the UI change in a
  move diff.
* **Whether the agent-settings sheet should also become a card.** It is a
  different kind of surface (arbitrary provider config, not a selection list),
  and forcing it into the picker idiom may be wrong. Worth asking once the
  three selection cards exist and can be compared against it.
* **iPhone layout for the new row.** [0121](0121-MADR-achieve-iphone-functional-parity.md)
  owns iPhone parity; eight icons at iPhone SE width is its question, not this
  record's.
* **Whether `next_turn` deserves distinct UI beyond banner wording.** P1 puts
  the fact on the wire; whether codex should, say, show a pending-level marker
  until the next message is a design question this record does not open.

## Execution record — 2026-08-29

**P1–P7 complete. P8 complete in test, incomplete on a device.** The plan stays
`in-progress` for that reason and no other.

| Phase | Commit | Result |
| --- | --- | --- |
| P1 wire capability | `b67d7e4` | done |
| P2 providers declare | `850280b` | done |
| P3 app parses | `2521c74` | done |
| P4 grok unblocked | `f048da0` | done |
| P5 cards | `4a72cb0` | done |
| P6/P7 move + cleanup | `bbf427e` | done |
| P8 back-arrow proof | `bbf427e` | test only — see below |

Final state: `flutter analyze` clean, **1346 passing, 6 failing**. All six
failures are pre-existing and Windows-only, verified by running them on a
pristine `HEAD` with every change of this record removed — they are
`PathAccessException: Access is denied` on temp paths in `transcript_cache` and
`history_replay`, which CI never sees because its Flutter lane is
`ubuntu-latest`. Not caused here, not fixed here (see Deferred).

### What the plan got wrong

**P1 named `internal/protocol/messages.go` and `internal/session/store.go`; it
needed neither.** `session.Meta` is serialised straight into
`SessionListResultPayload` (`ws/server.go:1696`), so the wire came free. And
mutability is deliberately **not** persisted: it is a property of the provider
binary in front of us, not of the record. Grok gained mid-session changes in a
*point release*, so a value written to disk before that upgrade would be stale
in precisely the direction that makes a client lie — the defect this record
exists to remove. The scope was narrower than budgeted, for a reason worth
keeping.

**Open question 1 was answered "enum", and the answer was load-bearing.** A
bool would have flattened codex's `next_turn` into either a lie ("applies now")
or a lock. The three-value enum let D8's banner say something true for codex,
which a bool could not have expressed without being widened later by exactly
this work.

**P6 mis-specified the widget keys.** The first implementation keyed icons
`composer-action-<id>`, which renamed five affordances that existing tests
address by their historical keys (`attach-audio`, `open-shell`, …) and broke 13
tests for no gain. Using the bare id as the key fixed all 13 at once. A plan
that adds a widget should say what its keys are called.

**The plan did not budget for rewriting existing tests, and 18 needed it.**
`mode_selector_dangerous_test` (11), `collaboration_mode_test` (4),
`chat_render_test` (2) and `composer_layout_test` (1) all drove the app-bar
chips. Each was rewritten to drive the card while asserting the same
invariant — the auto-approve confirmation, the resolved (not first-in-list)
selection, the independence of the two mode controls. None was weakened; the
dangerous-mode tests still prove that dismissing by any route does not arm.

**Two tests caught a real omission.** `chat_render_test`'s plan-mode cases
failed because the first `permissionsIcon` dropped a signal the old chip
carried: OpenCode ships `plan` as a *session* mode, and it used to render
`edit_off` plus a tint. C4 requires that signal to survive the move, so the
**code** was fixed rather than the test. Without those two assertions the loss
would have shipped silently.

**One pre-existing behaviour was nearly changed by accident.** The first
`ComposerActionsRow` returned `SizedBox.shrink()` when empty;
`composer_layout_test` asserts the keyed row still exists, holding nothing.
That is C3 working as intended — the row now always renders.

### Not done

* **P8's device pass.** The widget test proves the geometry: the back arrow is
  present, non-zero, and on-screen at 360dp for **every** codex mode including
  `full access`, and the app bar lays out identically whatever the mode. It
  does **not** prove a 20dp glyph in a 33.6dp box is legible, or that the nine
  icons read apart at a glance. That needs a real screen and has not happened.
  MADR open question 4 is therefore still open.
* **Icon vocabulary is provisional** for the same reason. If the glyphs do not
  separate on a device, D14's vocabulary changes — not D12's budget.
* **Capacity is proven at ten**, with a real tenth icon, at both 360dp and
  320dp, scrolling at twelve, each box keeping the full 48dp height (A14–A16).
  Codex uses nine of the ten today.

## Amendment — 2026-08-29: P9 and P10 (MADR D15–D17)

P8's device pass returned "works, glyphs rejected". Two phases follow from the
MADR's 2026-08-29 amendment. Scope additions: `apps/mobile/assets/ui_icons/`
(new, bundled Lucide SVGs + their ISC notice), `apps/mobile/pubspec.yaml`
(assets declaration only — **no new dependency**), and the existing
`session_controls/` files.

### P9 — A curated Lucide set (D15; closes the device half of P8)

Replace every `IconData` in the composer row with a bundled Lucide SVG rendered
through the `flutter_svg` already in the tree. `ComposerAction` carries an asset
name rather than an `IconData`.

The set, chosen for silhouette separation at 20dp (D14's surviving principle):

| Slot | Lucide |
| --- | --- |
| Attach image | `image-plus` |
| Attach audio | `audio-lines` |
| Workspace | `folder` |
| Shell | `square-terminal` |
| Share | `share-2` |
| Permissions | `shield` / `shield-check` / `shield-alert` / `shield-off` |
| Collaboration | `list-checks` (plan) / `pencil-line` (default) |
| Thinking | `brain` |
| Agent settings | `sliders-horizontal` |

`ios_share`, `tune`, `checklist`, `bolt` and `edit_off` all leave — they are the
idiom-breakers the Observed section names.

**Verification:** `flutter analyze`; the composer and capacity tests still pass
at ten; every asset referenced exists (a missing SVG renders as a silent blank,
so assert the manifest, not just the widget tree).

### P10 — Normalised labels, monochrome danger (D16, D17)

Title-case permission labels at the presentation layer, leaving wire ids alone.
Remove leading colour from every card row. Give a `dangerous` mode a trailing
monochrome marker and a description of what it actually does.

**Verification:** the dangerous-mode tests must still pass **unchanged** —
if normalising labels or removing tint breaks them, the safety behaviour moved
and that is a failure, not a fixture update.

### Acceptance additions

| # | Criterion | MADR |
| --- | --- | --- |
| A19 | Every composer icon is Lucide; no Material glyph and no `ios_share` remain | D15 |
| A20 | Bundled under ISC with the notice shipped; no new pubspec dependency | D15 |
| A21 | Permission labels render Title Case; wire ids unchanged | D16 |
| A22 | No card row has a leading colour | D17 |
| A23 | `confirmDangerousMode` still gates arming, tests unchanged | D6, D17 |
| A24 | The permissions composer icon still changes glyph with posture | C4 |

**A23 is the one to guard.** D17 removes a colour that currently signals
danger, and the temptation while making rows uniform is to let the trailing
marker and the description quietly stand in for the confirmation. They do not.
The dialog is the control; the rest is only how the row reads.
