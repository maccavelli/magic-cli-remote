# Android app: deep-dive debug audit

**Status:** For review
**Date:** 2026-07-27
**Scope:** `apps/mobile/` — Dart sources (`lib/`, 16 212 lines across 40 files),
Android manifest/Gradle, and the theme layer. Daemon-side Go was out of scope.
**Tree:** `66d6f1e`. The composer/keyboard fix described in H2 landed as `472ceab`
during this audit; see §8 for what else moved underneath it.

---

## 0. Method

Every finding below was checked against source, and the two that could be
demonstrated were demonstrated rather than argued:

- **Read in full:** the theme layer, `mcremote_client.dart`, `transcripts_notifier.dart`,
  `transcript_reducer.dart`, `chat_models.dart`, `transcript_cache.dart`,
  `transcript_pane.dart`, `transcript_rows.dart`, `scroll_activity.dart`,
  `top_notification.dart`, `starfield.dart`, `notification_coordinator.dart`,
  `settings_screen.dart`, `work_items_panel.dart`, the manifest, the network
  security config and `build.gradle.kts`. Read in part: `chat_screen.dart`,
  `chat_bubble.dart`, `sessions_screen.dart`, `connect_screen.dart`.
- **Executed:** `flutter analyze` (clean), `dart format --set-exit-if-changed`
  (clean), the full suite (**329 pass**), and a throwaway instrumented widget
  test to measure H1 (removed afterwards; the tree carries only H2's fix).
- **Cross-checked against the Flutter SDK** at `~/.local/flutter` for the three
  claims that depend on framework behaviour rather than on this repo
  (`Scaffold` layout, `SliverMultiBoxAdaptorElement.performRebuild`,
  `SnackBar` semantics), and against `~/.pub-cache` for
  `SharedPreferences.getStringList` copy semantics.

Where I could not verify something on a device, I say so rather than asserting it.

**One suspicion I dropped rather than report:** `NotificationCoordinator.sessionLabels`
looked unbounded until I checked its only writer — `sessions_screen.dart:132`
clears before refilling. Not a leak.

---

## 1. Executive summary

The app is in good shape. The plumbing is genuinely careful — epoch-guarded
connects, pin-per-identity TLS, seq-gated history resync, list-identity
discipline through the reducer — and the comment density explains *why*, not
*what*. `flutter analyze` is clean and 329 tests pass. Most of what follows is
polish; three items are real defects.

| # | Severity | Finding | Evidence |
|---|---|---|---|
| H1 | High | Transcript row memo defeats its own key index — markdown re-parse cost grows linearly with appends | **Measured** |
| H2 | High | Composer rendered behind the soft keyboard | **Fixed this session** |
| H3 | High | Staged image bytes never released on session delete / sign-out | Source |
| M1 | Medium | Predictive back declared in the theme, not enabled in the manifest | Source + SDK |
| M2 | Medium | Notifications lost their screen-reader announcement in the snackbar migration | Source + SDK |
| M3 | Medium | Burst notifications replace rather than queue — earlier errors never seen | Source |
| M4 | Medium | `session_mode` / `session_config` lack the no-op guard every sibling event has | Source |
| M5 | Medium | Unread counter rescans the whole transcript per append | Source |
| M6 | Medium | Error text clipper flattens newlines and can split a surrogate pair | Source |
| M7 | Low-Med | `snackBarTheme` is now unreachable configuration | Source |
| M8 | Low-Med | `TopNotification` hardcodes type/shape instead of using celestial tokens | Source |
| L1–L7 | Low | Dead code, unwired API, raw `DateTime` in UI, build config | Source |

---

## 2. High

### H1 — The transcript row memo defeats the key index it exists to feed

**Files:** `apps/mobile/lib/features/chat/transcript_pane.dart:16-80`, `:136-141`

`_memoTranscriptRows` returns `(rows, sameKeys)`. `sameKeys: true` tells
`_TranscriptPaneState` that the row keys are unchanged, so it may keep the
existing `_keyIndex` — the map `findChildIndexCallback` uses to relocate list
elements under `reverse: true` (documented in `docs/chat-performance.md` under
"List element reuse").

The doc comment on the function states the contract precisely:

> reports whether the row keys are provably unchanged from `prevRows` — true
> only on the identity hit and the fast path (which swaps the last row for one
> with the same seq)

The append fast path violates it. It adds a brand-new row — and a brand-new key —
then returns `true` anyway (`:47`). `_keyIndex` therefore never learns about any
row appended through that path, and it is only rebuilt when something forces a
full `buildTranscriptRows`.

The consequence is not cosmetic. For each row missing from `_keyIndex`,
`findChildIndexCallback` returns `null`; Flutter's
`SliverMultiBoxAdaptorElement.performRebuild` treats that as "leave this element
at its current index" (`sliver.dart:1025` — `newChildren.putIfAbsent(index, () => _childElements[index])`).
Under `reverse: true` every append shifts each existing row's child index by
one, so those elements land on an index that now holds a *different* row. The
keys no longer match, `Widget.canUpdate` fails, and the element is destroyed and
re-inflated instead of relocated. For an assistant bubble, re-inflating means a
full `MarkdownBody` re-parse.

**Measured.** A widget test that appends assistant bubbles one at a time and
reads `debugMarkdownParseCount` (the counter the codebase already maintains for
exactly this guarantee):

```
append -> 3 rows: parses=1   (want 1)
append -> 4 rows: parses=2   (want 1)
append -> 5 rows: parses=3   (want 1)
append -> 6 rows: parses=4   (want 1)
append -> 7 rows: parses=5   (want 1)
append -> 8 rows: parses=6   (want 1)
```

Every previously appended bubble re-parses on every new append. Cost per append
grows with the number of appends since the last full row build — quadratic over
a conversation, in the exact hot path the memo/key-index machinery was built to
protect. In practice the blast radius is bounded by materialised elements
(visible rows plus the 900 px `scrollCacheExtent`), but those are precisely the
recent bubbles near the live end.

**Remedy — verified.** Changing `:47` to `return (newRows, false);` collapses it:

```
append -> 3..8 rows: parses=1 each   (want 1)
no-op rebuild:       parses=0        (want 0)
```

That is the one-character fix and it is correct, at the cost of an O(rows) map
rebuild per append. The better fix keeps the fast path and extends the index
incrementally — have `_memoTranscriptRows` report "appended", and in `build`
do `_keyIndex = {..._keyIndex, _rowKeyValue(rows.last): rows.length - 1}`.
Either way this deserves a regression test asserting one parse per append; the
existing `streaming_markdown_test.dart` asserts parse counts during *streaming*
but never across successive appends, which is why this went unnoticed.

I reverted the fix — this document is the deliverable.

### H2 — Composer rendered behind the soft keyboard *(fixed this session)*

**File:** `apps/mobile/lib/features/chat/chat_screen.dart:~2000`

Commit `1ece397` moved the composer into `Scaffold.bottomNavigationBar`.
Scaffold applies `viewInsets` when sizing the **body** only
(`scaffold.dart:1088`); the bottom bar is pinned to `size.height - barHeight`
unconditionally (`:1054`). The keyboard covered the field being typed into.

That commit existed to stop snackbars covering the composer, and the *next*
commit (`5f17953`) solved the same problem properly by moving notifications to a
top overlay — there are now zero `showSnackBar` calls in `lib/`. The workaround
outlived its reason. Fixed by returning the composer to the body, with a comment
recording why the slot is wrong and a regression test
(`chat_render_test.dart`, `_hostWithKeyboard`) that fails at `Actual: <588.0>`
against a 300 px keyboard when the old layout is restored.

### H3 — Staged image bytes survive session deletion and sign-out

**File:** `apps/mobile/lib/state/transcripts_notifier.dart:374`

`_sentImages` holds `Map<String, List<List<Uint8List>>>` — raw image bytes for
prompts sent from this device, awaiting their `user_message` echo to be folded
into the rendered bubble. Entries are removed on a successful echo
(`:403`) or an explicit unstage after a failed send (`:389`).

Nothing else clears it. `clearSession` (`:413`) and `clearAll` (`:425`) prune
`_pending`, `_lastSeq`, `_firstSeq`, `_historyGen`, `_hydrating`, `_deferred`
and `_cacheTimers` — but not `_sentImages`. Neither does `onDispose`.

So bytes are retained for the process lifetime whenever the echo never arrives
and the send did not throw: session ended from the chat menu mid-flight, host
deletes the session, a socket drop between `session.prompt` returning and the
echo landing. `_pickImage` caps a single image at 4 MB
(`chat_screen.dart:518`), so this is megabytes of native heap held past the
point where the session it belonged to no longer exists — and it survives
"Clear saved credentials", which is a privacy expectation as much as a memory one.

**Remedy:** add `_sentImages.remove(sessionId)` to `clearSession` and
`_sentImages.clear()` to `clearAll`, and clear it in `onDispose` alongside
`_cacheTimers`. Consider also dropping a session's queue when `syncFromMeta`
evicts it — that path already prunes six sibling maps by the same rule.

---

## 3. Medium

### M1 — Predictive back is declared in the theme but not enabled in the manifest

**Files:** `apps/mobile/lib/theme/celestial.dart:388-392`,
`apps/mobile/android/app/src/main/AndroidManifest.xml`

The theme opts every Android page transition into
`PredictiveBackPageTransitionsBuilder()`. That builder animates from
`startBackGesture` events delivered by the engine
(`predictive_back_page_transitions_builder.dart:256`,
`widgets/binding.dart:1321`), and Android only delivers them to an app that has
opted into the `OnBackInvokedCallback` API via
`android:enableOnBackInvokedCallback="true"` on `<application>`.

`grep -rn "enableOnBackInvokedCallback" android/` returns nothing. Without the
attribute the builder silently falls back to `FadeForwardsPageTransitionsBuilder`
(documented in its class comment), so the intended Android 14+ transition never
runs. Nothing breaks — the app just doesn't get the polish it asked for.

Adding one manifest attribute is the whole fix. **Verify on a device** before
closing it out: I confirmed the attribute's absence and the framework's
dependency on engine-delivered gestures statically, but did not run on Android 14+.

### M2 — Notifications no longer announce to screen readers

**File:** `apps/mobile/lib/theme/top_notification.dart`

`Flutter`'s `SnackBar` wraps its content in `Semantics(container: true,
liveRegion: true)` (`snack_bar.dart:828-830`), which is what makes TalkBack read
it out. `_TopNotification` is a bare `Positioned`/`Material`/`Row` with no
semantics of any kind — and `grep` finds **zero** `Semantics(`, `semanticLabel:`
or `SemanticsService` anywhere in `lib/`.

The migration in `5f17953` replaced 46 snackbar call sites, so this silently
removed the announcement from every transient message in the app: "Send failed",
"Copied", "Image too large", "Request was resolved elsewhere", "Reconnect to the
host first". A screen-reader user now gets no feedback at all for a failed send.

**Remedy:** wrap the notification body in `Semantics(container: true,
liveRegion: true, child: …)`. One line, restores parity.

### M3 — Rapid notifications replace instead of queueing

**File:** `apps/mobile/lib/theme/top_notification.dart:21-22`

`showTopNotification` removes any active entry before inserting the new one.
`SnackBar` queued; this drops. Two failures in quick succession — plausible when
a reconnect flushes a queued prompt and the send fails, or when a config sheet
applies several options — show only the last, for 3 s, with no history. The
messages are the app's only report channel for these paths.

**Remedy:** a small FIFO in the file-level state, or at minimum do not discard a
notification that has been visible for less than ~1 s.

### M4 — `session_mode` and `session_config` skip the no-op guard their siblings have

**File:** `apps/mobile/lib/data/chat/transcript_reducer.dart:67-95`

Every other replace-semantics event compares content before copying:
`available_commands` → `_sameCommands`, `remote_commands` → `_sameRemoteCommands`,
`plan` → `_samePlan`, `usage_update` and `session_capabilities` → value `==`
(both types override `operator ==`, `models.dart:390`, `:490`).

`session_mode` does not. It builds `List<SessionMode>.from(ev.modes)` and then
tests `identical(modes, current.modes)` — which a freshly constructed list can
never satisfy. `session_config` does not either: past the empty check it always
merges and always copies. Neither `SessionMode` nor `ConfigOption` overrides `==`,
so no cheap content comparison exists today.

Effect: a re-sent, byte-identical `session_mode` or `session_config` produces a
new `SessionTranscript`, a new `TranscriptsState`, and — because the chat shell
selects on `t.modes` and `t.configOptions` by value
(`chat_screen.dart:1599`, `:1605`) and list identity differs — a full shell
rebuild. Neither event is in `_isBatchableEvent`, so each one also forces an
immediate commit. The cost per event is small; the inconsistency is the finding,
and it will bite whichever provider re-sends these on every turn.

**Remedy:** give `SessionMode` and `ConfigOption` value equality (or add
`_sameModes` / `_sameConfigOptions` in the reducer's existing style) and return
`current` unchanged on a no-op.

### M5 — Unread counter rescans the entire transcript on every append

**File:** `apps/mobile/lib/features/chat/chat_screen.dart:391-400`

```dart
for (final item in t.items) {
  if (item.seq >= _seqAtLeaveBottom) n++;
}
```

`items` is capped at `kMaxTranscriptItems` = 800, and this runs per append while
the user is scrolled up — precisely when the agent is streaming and the user is
reading back. Items are seq-ordered ascending, so counting backwards and
breaking on the first `seq < _seqAtLeaveBottom` gives the same answer in O(unread)
instead of O(800). A three-line change.

### M6 — Error clipper flattens newlines and can split a surrogate pair

**File:** `apps/mobile/lib/data/chat/transcript_reducer.dart:559-563`

```dart
String _clip(String s, int max) {
  final trimmed = s.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (trimmed.length <= max) return trimmed;
  return '${trimmed.substring(0, max)}…';
}
```

Used only for the `error` event body (`:144`, cap 300). Two problems:

1. `\s+` → `' '` collapses newlines, so a multi-line daemon error (a stack
   trace, a multi-line CLI failure) renders as one run-on line. This is the
   app's primary display of *why the turn failed*.
2. `substring(0, max)` cuts by UTF-16 code unit. If index 300 falls between the
   halves of a surrogate pair the result carries a lone surrogate and renders as
   a replacement glyph. `_AssistantMarkdown._render` already guards this exact
   case for its show-more clamp (`chat_bubble.dart:572-574`) — the technique
   exists in-tree, it just isn't applied here.

`clipItemText` (`:350`) has the same `substring` issue at the 100 000-char cap;
far less likely to be hit, same one-line fix.

**Remedy:** preserve paragraph breaks (collapse runs of spaces/tabs, keep `\n`),
and back the cut off by one when it lands on a low surrogate.

### M7 — `snackBarTheme` is now unreachable configuration

**File:** `apps/mobile/lib/theme/celestial.dart:368-375`

Zero `showSnackBar` / `SnackBar(` remain in `lib/` after `5f17953`, so this
`SnackBarThemeData` can no longer affect anything rendered. It is the more
carefully specified of the two — floating behaviour, 12 px radius, inverse
surface, `inversePrimary` action colour — while its replacement hardcodes
equivalents (M8). (`bannerTheme` directly below it *is* live —
`MaterialBanner` at `chat_screen.dart:1853`, `:1871`.)

Either delete it, or better, make `TopNotification` read from it so the intent
is expressed once.

### M8 — `TopNotification` bypasses the celestial design system

**File:** `apps/mobile/lib/theme/top_notification.dart:117-184`

The rest of the app is disciplined about this: `grep "Color(0x"` outside
`lib/theme/` returns **nothing**, and semantic colours resolve through
`celestialOf(context)`. The new notification widget is the outlier — it
hardcodes `fontSize: 14` and `fontSize: 13` instead of `textTheme.bodyMedium` /
`labelLarge`, `elevation: 6`, `BorderRadius.circular(12)`, and its own 14 px
vertical padding, and it hand-rolls the light/dark inversion
(`isLight ? onSurface : inverseSurface`) that `snackBarTheme` already expresses.

Nothing looks wrong today; it just won't track the theme. A text-scale user gets
a non-scaling toast, and the next type-ramp change silently skips it.

**Remedy:** pull colours/shape from `Theme.of(context).snackBarTheme` (which
also resolves M7) and the type from `textTheme`.

---

## 4. Low

| # | Finding | Location |
|---|---|---|
| L1 | **Dead code:** `TopNotificationX.topNotification` extension — declared, never called anywhere in `lib/` or `test/` | `theme/top_notification.dart:44-59` |
| L2 | **Dead code:** `McremoteClient.listCommands` — declared, never called; commands arrive via `available_commands` / `remote_commands` events instead | `data/ws/mcremote_client.dart:1438` |
| L3 | **Unwired feature:** `McremoteClient.revertMessage` is implemented and never called, while its counterpart `unrevert` *is* wired into the chat menu. The app offers "Restore reverted messages" with no way to revert one | `data/ws/mcremote_client.dart:1470` |
| L4 | **Raw `DateTime` in UI:** the resume-session picker renders `'Updated ${session.updatedAt!.toLocal()}'` → `2026-07-27 14:03:22.123456`. The only such site in the app; `chat_bubble.dart:319` shows the humanising pattern the codebase otherwise uses | `features/sessions/sessions_screen.dart:377` |
| L5 | **Stale template comment:** `// TODO: Specify your own unique Application ID` sits above an applicationId that *has* been set | `android/app/build.gradle.kts` |
| L6 | `_groupExpanded` (tool-group expansion, keyed by first-item seq) is never pruned when items age out of the 800-item cap. Bounded and tiny — noting for completeness | `features/chat/transcript_pane.dart:121` |
| L7 | Android lint is fully disabled (`checkReleaseBuilds = false`, `abortOnError = false`). The stated reason — memory on small build hosts — is legitimate, but it means manifest and resource regressions (M1 is exactly that class) never surface in CI | `android/app/build.gradle.kts` |

**Release build size (proposal, not a defect).** `buildTypes.release` sets only
`signingConfig`; `isMinifyEnabled` / `isShrinkResources` are unset, so R8 does
not run. Dart is AOT-compiled regardless, but the Java/Kotlin side pulls in
`mobile_scanner` (ML Kit), `image_picker`, `speech_to_text`,
`flutter_local_notifications` and `flutter_foreground_task` — the usual case for
a meaningful APK reduction. It needs ProGuard keep rules and a real
device smoke test of camera/speech/notifications before it can be trusted, so it
belongs in its own change, not a batch.

---

## 5. Look-and-feel consistency

Better than most codebases this size. Concretely verified:

- **Zero hardcoded hex outside `lib/theme/`.** The only `Colors.*` literals are
  `qr_scan_screen.dart` (white/black over a camera preview, correct — the
  backdrop is not themed) and `chat_bubble.dart:1209-1224` (scrim over an image
  thumbnail, same rationale), plus `Colors.transparent` used to suppress
  `ExpansionTile` dividers in three places.
- **Semantic colour always goes through `celestialOf(context)`** —
  `statusColor`, `WorkItemsPanel._colorFor`, permission gold, plan progress.
  `CelestialColors` provides `lerp`, so theme transitions animate.
- **Both themes are complete `ColorScheme`s**, not `fromSeed` derivations, and
  the file states the WCAG-AA constraint it was built under.
- **Decorative animation yields to the user**: `_PulsingDot` and `ShimmerText`
  both honour `MediaQuery.maybeDisableAnimationsOf` *and* pause while the
  transcript is scrolling (`ChatScrollActivity`). That is a level of care most
  apps skip.

Two gaps, both above: M8 (the notification widget sits outside the system) and
M2 (no semantics anywhere — the design system covers colour and type but not
accessibility).

---

## 6. Verified healthy

Worth recording, so the findings above are read in proportion:

- **Connection state machine.** The `_connectEpoch` / `_staleAttempt` discipline
  correctly abandons superseded attempts after every await, closes the orphaned
  socket without touching shared state, and makes sign-out cancel work in
  flight. `_handshakeFailures` distinguishes "host unreachable" (retry forever)
  from "host reachable but the handshake keeps dying" (park in error) — a
  distinction most clients miss.
- **TLS pinning.** The two-mode design in `CertPinner` is right, and the class
  comment explains the subtle part correctly: `badCertificateCallback` fires
  only after platform validation fails, so `selfsigned` must use
  `withTrustedRoots: false` or the pin is silently skipped for any chain the
  platform already trusts.
- **History resync.** The `_lastSeq` / `_firstSeq` gates, the
  deferred-live-event queue during a chunked apply, and the "note seqs only
  after the commit covering them" rule are all implemented as documented, and
  the `progressive` flag correctly distinguishes cold open from rebuild.
- **List-identity discipline** through the reducer (`growableItems`,
  `_publish`, "continue from what `_commit` returned") is consistently applied.
- **`TranscriptCache` serialises its read-modify-write** of the index key
  through `_serial`, and `_clear` sweeps by key prefix rather than trusting the
  index — both defend against exactly the failure the comments describe.
  (`SharedPreferences.getStringList` returns a copy — checked in
  `shared_preferences-2.5.5/lib/src/shared_preferences_legacy.dart:140` — so
  the in-place `index.remove` in `_writeEntry` is safe.)
- **Android security posture is deliberate and documented**: `allowBackup=false`
  plus backup/extraction rules, cleartext off with an explanation of why no
  `<trust-anchors>` entry can exist, `taskAffinity=""`, and a removed deep-link
  intent-filter with a comment explaining the attack it invited and what a
  future re-add must include. `windowSoftInputMode="adjustResize"` is correctly set.
- **Release signing** falls back to the debug key only with a loud warning and a
  comment explaining why the artefact must not be distributed.

---

## 7. Suggested order

1. **H1** — measurable, one-line fix available, add the missing regression test.
2. **H3** — three lines, prevents retained image bytes outliving a deleted session.
3. **M2 + M1** — two attributes/one wrapper; restores accessibility and the
   intended back gesture.
4. **M4, M5, M6** — small correctness/perf items in the reducer and chat screen.
5. **M7 + M8** together — fold `TopNotification` onto `snackBarTheme` and delete
   the duplicate.
6. **L1–L5** — deletions and one-liners; L3 is a product decision (wire revert, or
   drop both halves).

---

## 8. Limits of this audit

- No device or emulator run: M1 is statically verified but its user-visible
  effect is not. No profile-mode trace was taken, so H1's cost is expressed in
  parse counts, not milliseconds.
- `chat_bubble.dart` (1 235 lines), `sessions_screen.dart` (1 319) and
  `connect_screen.dart` (786) were read in part, not end to end. The
  transcript/streaming paths in `chat_bubble.dart` were read closely; its
  permission and limit-card rendering was skimmed.
- No review of `linux/`, the test suite's own coverage gaps beyond the two named
  above, or the Go daemon (except the OpenCode emit path covered in §9).
- Four commits landed in a parallel session during this audit. Their effect on
  `apps/mobile/` is limited to `transcript_reducer.dart` (`_onTurnComplete`'s
  `error` suppression and `_upsertTool`'s identity guard), the H2 fix, and two
  test files — `git diff 75e5a24..HEAD -- apps/mobile/`. All source reads and
  line citations here are against the resulting tree, so no finding is stale.
  The Go-side changes in those commits were not reviewed.

---

# 9. Appendix — OpenCode chat session: rendering and message flow

**Added:** 2026-07-27, in response to a report of janky, bursty rendering during
OpenCode sessions: *"tool calls do not count into the row, the row is expanded
then closed and redrawn with every additional tool call, the screen jitters
badly."*

All four symptoms reproduce, and each maps to a specific defect. Three were
measured with throwaway instrumented tests (removed afterwards; the tree is
unchanged by this appendix).

## 9.1 Symptom → cause

| Reported symptom | Cause |
|---|---|
| "tool calls do not count into the row" | **O1** grouping requires *same-class consecutive* tools; OpenCode alternates classes, so runs are length 1 and never group. **O2** the in-flight tool is excluded from the count. |
| "row is expanded then closed and redrawn" | **O2** a running tool renders as a full standalone card, then is swallowed into a one-line collapsed group on completion — a two-step visual per tool. |
| "bursty" | **O3** `tool_call` bypasses the 32 ms batch window entirely — one commit per tool. **O7** the daemon sends one WS frame per tool part update, blocking and uncoalesced. |
| "screen jitters badly" | **O5** `_scrollToEnd()` cancels the user's in-flight drag/fling. **O2** row-count churn reflows the list on every tool. |
| "un-optimized from caching/buffering" | **O4** every tool event forces a full row fold + key-index rebuild. **O8** redundant tool updates in a window are not folded. |

## 9.2 Evidence

**Measurement 1 — what the rows actually do.** Replaying a realistic OpenCode
tool sequence (`read, grep, bash, read, edit, bash`, each `pending → running →
completed` exactly as `http.go` emits) through the real notifier and
`buildTranscriptRows`:

```
t1 pending  rows=3 :: <user> <assistant> [read:pending]
t1 done     rows=3 :: <user> <assistant> [read:completed]
t2 pending  rows=4 :: <user> <assistant> [read:completed] [grep:pending]
t2 done     rows=3 :: <user> <assistant> {2xother}                      <-- 4→3 collapse
t3 pending  rows=4 :: <user> <assistant> {2xother} [bash:pending]
t3 done     rows=4 :: <user> <assistant> {2xother} [bash:completed]
t4 done     rows=5 :: <user> <assistant> {2xother} [bash] [read]
t5 done     rows=6 :: <user> <assistant> {2xother} [bash] [read] [edit]
t6 done     rows=7 :: <user> <assistant> {2xother} [bash] [read] [edit] [bash]
```

Six tools produced **six rows and exactly one group of two**. The row count
oscillates up-then-down on every tool that does fold (t2: 4→3), and never folds
at all once the classes start alternating.

**Measurement 2 — commits per burst.** Counting `transcriptsProvider`
notifications for events staged into a single 32 ms window:

```
5 parallel tool_call + 5 tool_call_update:  commits=6   (ideal 1)
10 tool_call_update:                        commits=1   (ideal 1)
10 assistant_message_chunk:                 commits=1   (ideal 1)
```

The text path is fully coalesced. The tool-call path is not: each new tool
publishes its own transcript state synchronously.

**Measurement 3 — row-fold cost.** A temporary counter in
`buildTranscriptRows`, with the probe's own calls excluded:

```
6 tools => buildTranscriptRows calls=12, items scanned=66
```

Two full folds per tool, each followed by a full `_keyIndex` map rebuild. On a
realistic 40-tool OpenCode turn over a ~60-item transcript that is ~80 full
folds and ~80 map rebuilds, on top of ~40 uncoalesced commits.

## 9.3 Findings

### O1 (High) — Tool grouping almost never engages for OpenCode

**Files:** `lib/data/chat/transcript_rows.dart:65-81`,
`lib/data/chat/chat_models.dart:68-101`, `internal/provider/opencode/http.go:1144`

`buildTranscriptRows` folds only **consecutive tools of the same `ToolClass`**
(`if (runClass != null && cls != runClass) flushRun();`). The daemon's
`kindForTool` spreads OpenCode's tools across all three classes:

| OpenCode tool | ACP kind | `ToolClass` |
|---|---|---|
| `bash` | `execute` | `command` |
| `edit`, `write`, `patch`, `multiedit` | `edit` | `fileEdit` |
| `read` | `read` | `other` |
| `grep`, `glob`, `list`, `ls` | `search` | `other` |
| `webfetch`, `websearch` | `fetch` | `other` |
| `todowrite`, `todoread` | `think` | `other` |

Real agent work alternates constantly — read, bash, read, edit, bash — so the
same-class run is almost always length 1, below the `run.length >= 2` threshold.
The fold exists but is dead in practice, and the transcript becomes a wall of
individual tool rows. That is the literal "tool calls do not count into the row".

### O2 (High) — Grouping is retroactive, so each tool visibly collapses

**File:** `lib/data/chat/transcript_rows.dart:67`

`final groupable = item.kind == ChatItemKind.tool && !item.toolRunning;` — a
pending or running tool is not groupable, and worse, it calls `flushRun()`,
*breaking* the run in progress.

The user-visible consequence is a two-step animation per tool:

1. The tool starts → a full-height standalone `_CompactStatusTile` card appears.
2. The tool completes → that card **disappears** and the adjacent group row
   redraws with an incremented count.

That is precisely "the row is expanded then closed and redrawn with every
additional tool call". It also means the collapsed group's "Ran N commands"
title is always one behind: the tool currently executing — the one the user most
wants to see — is never counted.

The remount is real, not just repaint: the row's key changes from
`ValueKey(seq)` (SingleRow) to `ValueKey('grp-$seq')` (GroupRow), so the element
is destroyed and re-inflated.

### O3 (High) — `tool_call` bypasses the batch window

**File:** `lib/state/transcripts_notifier.dart:47-60`

`_isBatchableEvent` lists `tool_call_update` but not `tool_call`. The comment
justifies it:

> Status, turn, permission, question, error, notice, user_message and the
> opening `tool_call` all gate an affordance or a state machine

**For `tool_call` that is not true.** In the reducer it does exactly one thing —
append a tool item (`transcript_reducer.dart:120-122`). Nothing else in the app
reads it: `grep -rn "'tool_call'" lib/` outside the reducer returns nothing, the
composer gates on `status`/`hasBlockingPrompt`, and the notification layer only
handles `permission_request` and `turn_complete` (`agent_notifications.dart`
`NotifKind` has two values). No affordance is gated by it.

OpenCode fans out parallel tool calls, so this is the main burst source:
measurement 2 shows 5 parallel tools costing 6 commits instead of 1. Moving
`tool_call` into `_isBatchableEvent` costs at most 32 ms of latency on the first
tool card and is the single highest-value change in this appendix.

### O4 (High) — Every tool event forces a full row fold + key-index rebuild

**File:** `lib/features/chat/transcript_pane.dart:52-78`

The second memo fast path (last item changed in place) excludes tools wholesale:

```dart
if (oldLast.kind == newLast.kind && oldLast.seq == newLast.seq &&
    newLast.kind != ChatItemKind.tool && ...)
```

Every `tool_call_update` to the newest tool therefore falls through to a full
`buildTranscriptRows`, and because that returns `sameKeys: false`, to a full
`_keyIndex` rebuild as well — measured at 2 per tool.

The exclusion is over-broad. A tool update only changes *groupability* when
`toolRunning` flips; a detail/output change on an already-running tool (the
common case, since OpenCode streams `state.Output` as the tool runs) cannot
change the row structure at all. Narrowing the condition to
`oldLast.toolRunning == newLast.toolRunning` would let output streaming take the
cheap path, exactly as text does.

Note this is also why **H1 does not bite during tool bursts** — the constant full
rebuilds keep `_keyIndex` fresh. The two findings trade off: fixing O4 without
fixing H1 would expose H1 on the tool path.

### O5 (High) — The transcript fights the user's finger

**File:** `lib/features/chat/chat_screen.dart:408-416`

```dart
void _scrollToEnd() {
  ...
  WidgetsBinding.instance.addPostFrameCallback((_) {
    if (!_scroll.hasClients) return;
    _scroll.jumpTo(0);
  });
}
```

`ScrollPositionWithSingleContext.jumpTo` begins with `goIdle()`
(`scroll_position_with_single_context.dart:197-207`), which terminates whatever
activity is running — including an in-flight user drag or ballistic fling.

The caller fires this whenever a row is appended and `_userNearBottom.value` is
true, and "near bottom" is a **120 px band** (`:378`). So while the agent works,
a user trying to scroll back through tool output is snapped to the live end on
every new tool — several times per second during a burst. This is the most
likely source of "the screen jitters badly", and it compounds with O2's
row-count churn.

The signal needed to fix it already exists and is already wired: `_listScrolling`
(`:159`, sensor at `:1952`) tracks exactly this, and is currently consumed only
by the shimmer/pulse animations. `_scrollToEnd` should skip the jump while it is
true (and skip entirely when `position.pixels == 0`, where the jump is a no-op
that still cancels activity).

### O6 (Medium) — H1's stale key index fires on every pending-tool append

A pending tool append satisfies `!canFoldIntoGroup` (`toolRunning` is true for
`pending`), so it takes the buggy append fast path from H1 and leaves
`_keyIndex` stale. Today the very next `tool_call_update` forces a full rebuild
and repairs it — that is, **O4's inefficiency is currently masking H1's
correctness bug**. Fix them together or the fix for O4 will surface remounts.

### O7 (Medium, daemon) — Tool events are never coalesced on the wire

**Files:** `internal/event/event.go:116-140`,
`internal/chunkbuf/chunkbuf.go:81-83`, `internal/provider/httpagent/session.go:1147`

`chunkbuf.IsChunk` covers only `TypeAssistantChunk` and `TypeThoughtChunk`, and
both `TypeToolCall` and `TypeToolUpdate` are in `IsControl` — so every tool part
update is a separate, blocking, never-dropped WebSocket frame. MADR 0024's
coalescing buys nothing on the tool path.

OpenCode's own dedup is good and should be kept — `noteToolEmit`
(`http.go:1120-1132`) suppresses byte-identical repeats, so no-op frames are
already filtered. But a `bash` streaming build output produces a genuinely
*different* payload per SSE frame (`state.Output` accumulates), so the phone
receives one frame per output delta, each carrying up to `maxToolOutputChars`
(8 000) characters.

A tool-aware coalescer — hold non-terminal updates per tool id for ~80 ms and
keep only the latest, flush immediately on a terminal status
(`completed`/`failed`) or a control boundary — mirrors what already exists for
text. It changes the control-event delivery contract, so it warrants its own
MADR rather than an inline patch.

### O8 (Medium) — Redundant tool updates within a window are not folded

**File:** `lib/state/transcripts_notifier.dart:299-336`

`_foldChunks` merges adjacent same-type *text* runs (`_isFoldableChunk` covers
assistant/thought only). `tool_call_update` is replace-semantics — only the last
state for a given tool id matters — but a window containing ten updates to one
tool applies all ten, each running `clipItemText` over up to 8 KB and a
`copyWith`. Collapsing consecutive same-`toolId` updates to the last one before
applying is the same optimisation, in the same function, for the same reason.

(The recently added identity guard in `_upsertTool`,
`transcript_reducer.dart:436-443`, already prevents *no-op* updates from
producing a new transcript — this is about redundant *distinct* ones.)

## 9.4 Recommended sequence

Ordered by value-per-risk. 1–3 are small and independently shippable.

1. **O3** — add `tool_call` to `_isBatchableEvent`. One line. Kills the burst.
2. **O5** — guard `_scrollToEnd` on `_listScrolling.value` and on
   `pixels == 0`. Few lines. Kills the jitter and the scroll-fighting.
3. **O8** — extend `_foldChunks` to collapse consecutive same-id
   `tool_call_update`s to the last. Contained, in the existing style.
4. **O1 + O2 together** — the grouping redesign. These must be one change; fixing
   either alone leaves the other's visual artefact.
5. **O4 + H1 together** — narrow the memo's tool exclusion to
   `oldLast.toolRunning == newLast.toolRunning`, *and* fix H1's stale key index,
   since 4 removes the masking.
6. **O7** — daemon-side tool coalescing. Own MADR.

### Sketch for O1 + O2

The goal is a row that **appears once and updates in place** for the whole tool
run, instead of one that forms, breaks, and re-forms.

- **Group all consecutive tool items**, regardless of `ToolClass`. Drop the
  same-class run rule; it is what makes the fold dead for OpenCode.
- **Let a running/pending tool join its group** rather than calling `flushRun()`.
  The group is then continuous from the first tool of the burst.
- **Title from the class histogram**: single dominant class keeps today's copy
  ("Ran 5 commands", "Edited 3 files"); mixed becomes "Used 7 tools".
- **Render the in-flight tool as the group's live head** — the collapsed row
  shows the group summary *plus* the currently running tool's name and spinner,
  so the user still sees what is executing without expanding, and long-running
  output is never hidden behind a collapse.
- **Key on the first item's seq**, which is now stable for the entire run — no
  `SingleRow` → `GroupRow` key transition, so no remount.

This makes the row count monotonic during a turn (one group row per contiguous
tool burst) instead of oscillating, which removes the reflow that O2 causes and
shrinks the list `buildTranscriptRows` walks.

**Open UX question for review:** whether a burst that mixes a 30-second `bash`
with quick `read`s should stay one row, or whether a long-running tool should
break out into its own row while it works. The sketch above keeps it as one row
with a live head; the alternative is closer to today's behaviour but with the
collapse artefact removed. This is a judgment call, not a technical constraint.

## 9.5 Already correct — do not undo

- OpenCode's `noteToolEmit` / `noteTool` dedup (`http.go:1108-1132`) and the
  `session.status busy` latch (`emitStatus`, `:1090`) both exist precisely to
  reduce client commits, and both work. The remaining burst is structural, not a
  dedup failure.
- `_upsertTool`'s identity guard (MADR 0034 D4) correctly returns the same
  transcript instance when nothing observable changed.
- `tool_call_update` batching, the 32 ms window, and the assistant-text fold are
  all working — measurement 2 confirms both the text and tool-update paths
  coalesce to a single commit.
- `clipBlock` (`http.go:1244`) preserves line structure and never cuts mid-rune
  for tool output — the daemon does this correctly, which makes the mobile
  `_clip` gap in M6 the odd one out.
- `ExpansionTile.maintainState` defaults to false, so a collapsed group does not
  retain inflated children. I checked this expecting a leak; there isn't one.

## 9.6 Limits

- All measurements are from widget tests on a synthetic event stream shaped to
  match `http.go`'s emission, not from a captured OpenCode session or a profile
  trace on a device. Frame timings are therefore not quantified — the numbers
  here are commit counts, fold counts and row transitions.
- The tool sequence used (`read, grep, bash, read, edit, bash`) is
  representative, not sampled from real traffic. O1's severity scales directly
  with how often classes alternate; a run of six `read`s would group cleanly.
- O7 is a reading of the emit path, not an observation of frame rates on the
  wire.
