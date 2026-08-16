# Implement "Ending the only session from inside chat must return to a populated sessions list"

Associated MADR: [0094-MADR-end-session-return-black-screen.md](0094-MADR-end-session-return-black-screen.md)

## Requirement (verbatim)

**A user ending a session from within the session window is returned to
the main session screen without having to hit the back arrow or take any
action; the landing screen shows the post-delete list.** No completion
path of the delete may leave a blank or black frame.

## Grounding

The committed and pushed baseline `81c4fb0` implements MADR decisions
D1–D5 (guarded auto-pop, `sessionsRevisionProvider`, the sessions-screen
listener, and the C1/C2 pinning tests). Per-phase commits `dbc48e2`,
`72adc47`, `de7ea5d`, `0e11ae1` (pushed, CI green, 988 tests) added D6
(the modal-path router-aware exit) and its C6 pinning test. The socratic
assessment (2026-08-16) then surfaced two residual defects, recorded as
MADR 0094 D7 and D8:

* **D7 — lost-ok stranding.** `deleteSession` sends `session.delete`
  with `idempotentRetry: true` (`apps/mobile/lib/data/ws/mcremote_client.dart:3672`);
  the daemon errors on a double delete (`TestWSSessionDelete`,
  `internal/ws/server_session_handlers_test.go:281`); deletes can take
  many seconds (`internal/ws/server.go:689-698`). A purge whose `ok`
  response was lost therefore surfaces as an error, and the catch branch
  strands the user in a dead chat.
* **D8 — ghost transcript.** A `permission_request` frame delivered
  after the delete `ok` re-creates the cleared session's transcript via
  the `forSession` empty fallback (`apps/mobile/lib/data/chat/chat_models.dart:535-542`),
  and while the chat State is still alive its listener can schedule an
  `isDismissible:false` sheet over the landing screen.

Completion-path coverage the plan must deliver:

| Path | State at delete completion | Mechanism | Landing |
| --- | --- | --- | --- |
| P1 | Chat current, no modal | guarded `Navigator.pop(true)` | sessions; `didPopNext` refresh (D1/D3) |
| P2 | User backed out mid-delete | user already on sessions | revision bump refreshes list (D2) |
| P3 | A modal of ours open at completion | revision bump + `context.go('/sessions')` (D6) | sessions, refreshed |
| P4 | Delete failed; confirming list read shows the row **present**, or the read failed | error toast; user stays in chat | n/a — the host still has the session (D7, conservative) |
| P4b | Delete failed; confirming list read shows the row **absent** | treated as ended: `_completeEndSessionFlow()` + success toast | sessions, refreshed (D7) |
| P5 | Notification moved the user to another chat mid-delete | chat State disposed, `mounted == false` | no interference; early return |

## Execution state

* **Steps 1–6 (this document's first round) are complete and pushed** —
  baseline verification, C6 (red → green), D6 mechanism, format cleanup;
  commits `dbc48e2`, `72adc47`, `de7ea5d`, `0e11ae1`; CI green (988
  tests). They are retained below as the record of the round.
* **Steps 7–13 (this document) are the remaining work**: C7 (red →
  green), D7, C8 (red → green), D8, green run, gate, commit + C5 +
  MADR acceptance. Nothing in Steps 7–13 is implemented yet.

## Codebase anchors (verified 2026-08-16)

Every edit below is anchored to stable code text, not line numbers.
Verified facts the executor may rely on:

* `_endSessionFlow` lives in `apps/mobile/lib/features/chat/chat_screen.dart`;
  its success tail runs after `_notifCoord?.dropSessionAsks(widget.sessionId);`
  and its catch currently shows `'End session failed: ${friendlyOpError(e)}'`.
  `friendlyOpError` is defined in `apps/mobile/lib/data/ws/mc_exception.dart:31`
  and already imported by `chat_screen.dart`.
* `McException(this.message, {this.code, this.permanent = false, this.retryAfterMs})`
  — `apps/mobile/lib/data/ws/mc_exception.dart:3-8`; exported by
  `apps/mobile/lib/state/app_providers.dart:15`, so the nav test's existing
  imports resolve it with no new import.
* `PermissionOption` and `SessionEvent` live in
  `apps/mobile/lib/data/protocol/models.dart` (`PermissionOption` at :752),
  exported by `app_providers.dart:12` (nav test) and imported directly by
  `transcript_ingest_test.dart` — no new imports needed in either test.
* `TranscriptsNotifier` (`apps/mobile/lib/state/transcripts_notifier.dart`):
  field block ends at `final Map<String, Timer> _cacheTimers = {};`;
  `clearSession(String sessionId)` starts with `_pending.remove(sessionId);`;
  `_onEvent(SessionEvent ev)` starts with `final id = ev.sessionId; if (id.isEmpty) return;`;
  `syncFromMeta(List<SessionMeta> metas, {bool complete = true})` starts
  with `final liveIds = metas.map((m) => m.id).toSet();`; `clearAll()`
  clears `_pending`, `_lastSeq`, `_firstSeq`, `_seqGapSuspected`,
  `_historyGen`, `_hydrating`, `_deferred`, `_sentImages`, `_cacheTimers`.
  `debugOnEvent` routes through `_onEvent`, so tests exercise the drop.
* `transcript_ingest_test.dart` uses a bare `ProviderContainer`
  (`makeContainer()` with `addTearDown(c.dispose)`), `useFakePathProvider`
  in `setUp`, and `debugOnEvent` directly — no client override required
  (its existing tests pass without one).
* `TranscriptsState.byId` is public (used by `syncFromMeta`).
* `make preflight` runs Go stages **before** the mobile trio and fails on
  this macOS machine at the pre-existing
  `TestSetupWritesDefaultMcrelayConfig` (systemd `KillMode` asserted
  against a launchd plist; passes on CI's Linux). The authoritative local
  gate for this plan is the mobile trio run directly; CI's Flutter job is
  the equivalent of the full preflight.

## Scope

In scope (Steps 7–13):

* `apps/mobile/lib/features/chat/chat_screen.dart` — the delete-failure
  catch branch (D7) and the extraction of the completion tail into
  `_completeEndSessionFlow()`.
* `apps/mobile/lib/state/transcripts_notifier.dart` — the cleared-session
  tombstone (D8).
* `apps/mobile/test/chat_end_session_navigation_test.dart` — the C7
  regression test and the `_FakeClient.failDelete` extension.
* `apps/mobile/test/transcript_ingest_test.dart` — the C8 regression test.
* Operator device confirmation (C5, scenarios P1–P3) and the MADR
  `status: proposed → accepted` update.

Out of scope (MADR non-decisions):

* Daemon or protocol changes; `session.delete` keeps replying `ok` with
  no broadcast and keeps erroring on a double delete.
* The sessions list's own End action — the user is already on the main
  screen there.
* Refreshing the list under a *different* chat after a mid-delete
  notification navigation (P5): it refreshes on the next return to the
  sessions screen.
* The residual in-flight-purge window of P4 (row present at the
  confirming read because the daemon's delete is still mid-kill): an
  explicit error toast is shown and the landing screen self-heals on the
  next connection edge (incident log: `peer_closed` ~6 s after the
  last-session purge) — an accepted limitation, not a silent stranding.

## Implementation Steps

### Step 1 — Verify the committed baseline (read-only) — EXECUTED

Run from `apps/mobile`:

```bash
flutter test test/chat_end_session_navigation_test.dart \
  test/sessions_screen_test.dart test/chat_render_test.dart \
  test/replaced_close_test.dart
dart format --output=none --set-exit-if-changed \
  lib/features/chat/chat_screen.dart \
  lib/features/sessions/sessions_screen.dart \
  lib/state/app_providers.dart \
  test/chat_end_session_navigation_test.dart
flutter analyze lib/features/chat/chat_screen.dart \
  lib/features/sessions/sessions_screen.dart \
  lib/state/app_providers.dart \
  test/chat_end_session_navigation_test.dart
```

Acceptance: all tests pass (68 across the four files); format reports
no changes; analyze reports no issues. This confirms C1, C2, C3, C4 on
the baseline before touching anything.

### Step 2 — Add the C6 regression test (expect RED) — EXECUTED

Append this test to `test/chat_end_session_navigation_test.dart`
(reuses the file's existing `_FakeClient` with `deleteGate` and the
`pumpApp` helper):

```dart
  testWidgets(
    'a permission sheet open at delete completion still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [
          SessionMeta(id: 'sess-c', provider: 'kilo', name: 'Gamma'),
        ],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Gamma'));
      await tester.pumpAndSettle();

      // Start ending the session; the fake delete stalls on the gate.
      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // A permission ask arrives over the socket while the delete is in
      // flight and its sheet comes up. The sheet is isDismissible:false —
      // only resolution or external retirement can close it.
      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
        listen: false,
      );
      container.read(transcriptsProvider.notifier).debugOnEvent(
        SessionEvent(
          type: 'permission_request',
          sessionId: 'sess-c',
          permissionId: 'perm-end-race',
          toolName: 'command',
          text: 'rm -rf /tmp/x',
          options: [
            PermissionOption(
              optionId: 'accept',
              name: 'Allow once',
              kind: 'allow_once',
            ),
            PermissionOption(
              optionId: 'decline',
              name: 'Deny',
              kind: 'deny',
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Allow once'), findsOneWidget);

      // The delete completes with the sheet open: clearSession must
      // retire the sheet, and the flow must land on the sessions screen
      // with the post-delete list — no user action.
      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-c']);
      expect(find.text('Allow once'), findsNothing);
      expect(find.text('End agent session?'), findsNothing);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Gamma'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );
```

Run:

```bash
flutter test test/chat_end_session_navigation_test.dart
```

Acceptance: the two committed tests pass; the new test **fails** on
`expect(find.text('Sessions'), findsOneWidget)` (the proven baseline
defect: chat still mounted, success toast shown, no landing). If it
passes on the unmodified baseline, stop — the probe evidence no longer
matches reality and the MADR must be re-examined before continuing.

### Step 3 — Implement the D6 mechanism (expect GREEN) — EXECUTED

In `chat_screen.dart` `_endSessionFlow`, replace the completion tail
(currently: `if (!mounted) return;` → `clearSession` → toast →
`isCurrent` guard → pop-or-bump) with the `hadModal` capture and the
three ordered branches (`isCurrent` first; `hadModal` second; bump-only
third) as shown in the committed code at `de7ea5d`.

### Step 4 — Green run — EXECUTED

```bash
flutter test test/chat_end_session_navigation_test.dart
```

Acceptance: all three tests pass. Then the full related set plus
format/analyze (same commands as Step 1) — acceptance: 69 tests pass,
format clean, analyze clean.

### Step 5 — Gate — EXECUTED

The mobile trio (dart format `--set-exit-if-changed`, `flutter analyze`,
`flutter test`) run from `apps/mobile` — green. `make preflight` is red
on this machine only because of the pre-existing macOS Go failure
(`TestSetupWritesDefaultMcrelayConfig`), which precedes the mobile
stages and is untouched by this plan.

### Step 6 — Code commit — EXECUTED

Per-phase commits `dbc48e2` (format baseline), `72adc47` (C6 test),
`de7ea5d` (D6 mechanism), `0e11ae1` (test formatting); pushed, CI
green. Auto-generated commit messages via the `prepare-commit-msg`
hook; never pass `-m`.

### Step 7 — Add the C7 regression test (expect RED)

**7a.** In `test/chat_end_session_navigation_test.dart`, extend the
committed `_FakeClient`: add the fields next to `deleteGate`, and
replace the committed `deleteSession` override with:

```dart
  Completer<void>? deleteGate;
  bool failDelete = false;
  bool failDeleteKeepsRow = false;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listCalls++;
    return SessionListSnapshot(sessions: sessions, complete: true);
  }

  @override
  Future<List<ProviderInfo>> listProviders() async => const [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<void> cancel(String sessionId) async {}

  @override
  Future<void> deleteSession(String sessionId) async {
    deleteCalls.add(sessionId);
    final gate = deleteGate;
    if (gate != null) await gate.future;
    // Model host truth for the lost-ok case: the purge happened; only
    // the ok response was lost. Removing the row BEFORE throwing is what
    // makes the confirming list read see the row absent (D7/P4b).
    // failDeleteKeepsRow models the conservative case instead: the RPC
    // failed and the host still has the session (D7/P4).
    if (!failDeleteKeepsRow) {
      sessions = sessions.where((s) => s.id != sessionId).toList();
    }
    if (failDelete) {
      throw McException('timed out', code: 'timeout');
    }
  }
```

(`McException` resolves via the file's existing `app_providers.dart`
import; no new import.)

Toast timing fact both tests below rely on: `showTopNotification`
renders via an `AnimationController` (3 s default, 5 s error —
`apps/mobile/lib/theme/top_notification.dart:7,12`), so
`pumpAndSettle` animates the toast **away**. Toast assertions therefore
happen after a single `tester.pump()` — by then the fake-client
microtask chain (cancel → delete → catch → confirming read → toast)
has fully unwound — and landing assertions happen after the subsequent
`pumpAndSettle`.

**7b.** Append this test to the same file (the P4b case — RED on the
baseline):

```dart
  testWidgets(
    'a delete whose ok was lost still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [
          SessionMeta(id: 'sess-d', provider: 'kilo', name: 'Delta'),
        ],
      );
      client.failDelete = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Delta'));
      await tester.pumpAndSettle();

      // End the session: the delete RPC throws (the ok was lost), but the
      // fake models the host truth — the session was purged.
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      // One pump: the whole fake-client microtask chain has unwound and
      // the toast is up (pumpAndSettle would animate it away).
      await tester.pump();

      // D7: the confirming list read sees the row gone, classifies the
      // delete as ended, and runs the completion tail — the success toast
      // is up now and will animate away during settle.
      expect(client.deleteCalls, ['sess-d']);
      expect(find.textContaining('End session failed'), findsNothing);
      expect(find.text('Session ended'), findsOneWidget);

      await tester.pumpAndSettle();
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Delta'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );
```

**7c.** Append the P4 contrast test (MADR C7's contrast case — this one
passes on the baseline AND after Step 8; it pins that D7 does not break
the genuine-failure path):

```dart
  testWidgets(
    'a failed delete whose row survives keeps the user in the chat',
    (tester) async {
      final client = _FakeClient(
        sessions: [
          SessionMeta(id: 'sess-e', provider: 'kilo', name: 'Epsilon'),
        ],
      );
      client.failDelete = true;
      client.failDeleteKeepsRow = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Epsilon'));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // P4 conservative: the confirming read sees the row, the error
      // toast shows, and the user stays in the chat. ('Sessions' is the
      // offstage route below — find.text skips offstage by default.)
      expect(client.deleteCalls, ['sess-e']);
      expect(find.textContaining('End session failed'), findsOneWidget);
      expect(find.text('Epsilon'), findsWidgets);
      expect(find.text('Sessions'), findsNothing);

      await tester.pumpAndSettle();
      expect(find.text('Epsilon'), findsWidgets);
      expect(find.text('Sessions'), findsNothing);
    },
  );
```

**7d.** Run and gate:

```bash
cd apps/mobile
flutter test test/chat_end_session_navigation_test.dart
```

Acceptance: the three committed tests pass; the P4 contrast test also
passes (it pins existing behavior); the P4b test **fails** on
`expect(find.text('Sessions'), findsOneWidget)` — the current catch
branch shows the error toast and keeps the chat. Any other failure mode
is new evidence: stop and re-examine before Step 8.

**7e.** Phase commit:

```bash
git add apps/mobile/test/chat_end_session_navigation_test.dart
git commit --no-edit
```

### Step 8 — Implement D7 (expect GREEN)

In `chat_screen.dart` `_endSessionFlow`:

**8a.** Extract the completion tail into a private method placed
immediately after `_endSessionFlow` closes. Move verbatim the code from
`if (!mounted) return;` (the check after
`_notifCoord?.dropSessionAsks(widget.sessionId);`) through the end of
the three branches, and add the entry guard:

```dart
  /// Shared completion tail for the success path and the D7
  /// confirmed-purge path (MADR 0094): clear local state, toast, and
  /// navigate. The branches are ordered, not mutually exclusive (D6); the
  /// hadModal capture errs toward a correct-but-unnecessary go.
  void _completeEndSessionFlow() {
    if (!mounted) return;
    // A permission/question sheet or always-confirm dialog of ours may
    // be on top. Capture it BEFORE clearSession retires it: the
    // retirement's removeRoute lags one frame before isCurrent
    // reflects it, so the guarded pop below provably cannot fire in
    // that case (MADR 0094 D6 probe).
    final hadModal =
        _permissionSheetOpen ||
        _questionSheetOpen ||
        _openAlwaysConfirmRoute != null;
    // Clear local state only once the host actually deleted it.
    ref.read(transcriptsProvider.notifier).clearSession(widget.sessionId);
    showTopNotification(
      context,
      'Session ended',
      severity: NoticeSeverity.success,
    );
    final route = ModalRoute.of(context);
    if (route != null && route.isCurrent) {
      Navigator.of(context).pop(true);
    } else if (hadModal) {
      // Router-aware exit: deterministic, no frame-timing dependence.
      // A go exit does not fire didPopNext, so the bump owns the
      // landing-screen refresh.
      ref.read(sessionsRevisionProvider.notifier).bump();
      context.go('/sessions');
    } else {
      // The user already left mid-delete; they are on the landing
      // screen. Refresh it past the raced snapshot.
      ref.read(sessionsRevisionProvider.notifier).bump();
    }
  }
```

**8b.** At the success-path call site, replace the inline tail with the
single call:

```dart
      await client.deleteSession(widget.sessionId);
      // Its pending asks died with it, and the daemon sends no resolution for
      // them, so the notifications have to be pulled here (MADR 0046 M-4).
      _notifCoord?.dropSessionAsks(widget.sessionId);
      _completeEndSessionFlow();
```

**8c.** Replace the catch block with the D7 confirming read:

```dart
    } catch (e) {
      if (!mounted) return;
      // D7 (MADR 0094): the host may have purged the session even though
      // the RPC errored — session.delete is idempotentRetry, and the
      // daemon errors on a double delete, so a lost ok surfaces here.
      // Confirm against the host list once; treat a confirmed purge as
      // ended.
      var rowSurvives = true;
      try {
        final snap = await client.listSessionSnapshot();
        rowSurvives = snap.sessions.any((s) => s.id == widget.sessionId);
      } catch (_) {
        // Conservative: an unreadable list cannot confirm the purge.
      }
      // The confirming read awaited; the user may have left meanwhile.
      if (!mounted) return;
      if (rowSurvives) {
        showTopNotification(
          context,
          'End session failed: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
      } else {
        _completeEndSessionFlow();
      }
    }
```

Facts the executor must check while editing (do not assume):

* `client` is in scope in the catch block (declared before the `try`).
* The confirming `listSessionSnapshot()` is the **only** extra RPC on
  the failure path; no retries, no timers.
* The post-await `mounted` re-check in 8c is mandatory: without it an
  unmount during the confirming read would use a dead `context`.
* `_completeEndSessionFlow` is synchronous; nothing after the `await
  client.deleteSession` yield on the success path can unmount between
  the call and the helper's own `mounted` check.

**8d.** Run and gate:

```bash
cd apps/mobile
flutter test test/chat_end_session_navigation_test.dart
```

Acceptance: all five tests pass. Then the related set plus format and
analyze on the touched files:

```bash
flutter test test/chat_end_session_navigation_test.dart \
  test/sessions_screen_test.dart test/chat_render_test.dart \
  test/replaced_close_test.dart
dart format --output=none --set-exit-if-changed \
  lib/features/chat/chat_screen.dart \
  test/chat_end_session_navigation_test.dart
flutter analyze lib/features/chat/chat_screen.dart \
  test/chat_end_session_navigation_test.dart
```

Acceptance: 71 tests pass, format clean, analyze clean.

**8e.** Phase commit:

```bash
git add apps/mobile/lib/features/chat/chat_screen.dart
git commit --no-edit
```

### Step 9 — Add the C8 regression test (expect RED)

Append this test to `apps/mobile/test/transcript_ingest_test.dart`,
inside `main()` (it reuses the file's `makeContainer()`; `SessionEvent`
and `PermissionOption` resolve via the file's existing `models.dart`
import):

```dart
  test('events for a cleared session are dropped (MADR 0094 D8)', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    SessionEvent ask(String permId) => SessionEvent(
      type: 'permission_request',
      sessionId: 'sess-clear',
      permissionId: permId,
      toolName: 'command',
      text: 'ls',
      options: const [
        PermissionOption(
          optionId: 'accept',
          name: 'Allow once',
          kind: 'allow_once',
        ),
      ],
    );

    // Baseline sanity: an ask for a live session creates its transcript.
    n.debugOnEvent(ask('perm-live'));
    expect(
      c.read(transcriptsProvider).byId.containsKey('sess-clear'),
      isTrue,
    );

    n.clearSession('sess-clear');
    expect(
      c.read(transcriptsProvider).byId.containsKey('sess-clear'),
      isFalse,
    );

    // The ghost: an ask that lands after the delete must not resurrect
    // the cleared transcript (no pendingPermissions, no sheet).
    n.debugOnEvent(ask('perm-post-delete'));
    expect(
      c.read(transcriptsProvider).byId.containsKey('sess-clear'),
      isFalse,
      reason: 'a post-delete event must not resurrect a cleared session',
    );
  });
```

Run and gate:

```bash
cd apps/mobile
flutter test test/transcript_ingest_test.dart
```

Acceptance: existing tests pass; C8 **fails** on the final assertion —
the event re-creates the transcript (`byId` contains `sess-clear`).

Phase commit:

```bash
git add apps/mobile/test/transcript_ingest_test.dart
git commit --no-edit
```

### Step 10 — Implement D8 (expect GREEN)

In `apps/mobile/lib/state/transcripts_notifier.dart`, five exact edits:

**10a.** Field — after `final Map<String, Timer> _cacheTimers = {};`:

```dart
  /// Sessions cleared via [clearSession] (end-session purge, MADR 0094
  /// D8). Mirrors the daemon's `purged` set (0093 D3): events for a
  /// cleared id are dropped so a trailing ask cannot resurrect the
  /// transcript; removed when the id reappears in a host snapshot
  /// ([syncFromMeta]) and on [clearAll].
  final Set<String> _cleared = {};
```

**10b.** `clearSession` — insert as the first statement of the body:

```dart
  void clearSession(String sessionId) {
    _cleared.add(sessionId);
    _pending.remove(sessionId);
```

**10c.** `_onEvent` — insert immediately after `if (id.isEmpty)
return;` (before the `_hydrating` check, so a dropped id never reaches
`_noteSeq` — gap tracking and `knownSessionIds()` stay clean):

```dart
    final id = ev.sessionId;
    if (id.isEmpty) return;
    // MADR 0094 D8: a trailing event for a cleared session must not
    // resurrect its transcript (ghost sheet over the landing screen).
    if (_cleared.contains(id)) return;
```

**10d.** `syncFromMeta` — insert immediately after the `liveIds` line:

```dart
    final liveIds = metas.map((m) => m.id).toSet();
    // MADR 0094 D8: the host says these ids are alive again — drop the
    // tombstones (mirrors the daemon clearing `purged` on Create).
    _cleared.removeAll(liveIds);
```

**10e.** `clearAll` — insert after `_sentImages.clear();`:

```dart
    _sentImages.clear();
    _cleared.clear();
```

Run and gate:

```bash
cd apps/mobile
flutter test test/transcript_ingest_test.dart \
  test/chat_end_session_navigation_test.dart
```

Acceptance: C8 green, no regressions in either suite.

Phase commit:

```bash
git add apps/mobile/lib/state/transcripts_notifier.dart
git commit --no-edit
```

### Step 11 — Green run

```bash
cd apps/mobile
flutter test test/chat_end_session_navigation_test.dart \
  test/transcript_ingest_test.dart test/sessions_screen_test.dart \
  test/chat_render_test.dart test/replaced_close_test.dart
dart format --output=none --set-exit-if-changed \
  lib/features/chat/chat_screen.dart lib/state/transcripts_notifier.dart \
  test/chat_end_session_navigation_test.dart test/transcript_ingest_test.dart
flutter analyze lib/features/chat/chat_screen.dart \
  lib/state/transcripts_notifier.dart \
  test/chat_end_session_navigation_test.dart test/transcript_ingest_test.dart
flutter test
```

Acceptance: the related set passes; format reports 0 changed; analyze
reports no issues; the full suite passes (991 tests: the 988 baseline
plus the two C7 tests and C8).

### Step 12 — Gate

`make preflight` is **not** the local gate on this machine: it fails at
the pre-existing macOS-only `TestSetupWritesDefaultMcrelayConfig` before
reaching the mobile stages. The authoritative local gate is the mobile
trio, exactly as CI's Flutter job runs it:

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

Acceptance: all three green. CI (Linux) is the full-preflight
equivalent; its Flutter job enforces this trio and its Go job runs
independently.

### Step 13 — Commit, device confirmation (C5), MADR acceptance

**13a.** Stage and commit the round-2 code changes (the four phase
commits of Steps 7–10 already hold them; verify with
`git log --oneline -5` that the working tree is clean apart from docs):

```bash
git status --short
```

Acceptance: only `docs/0094-MADR-end-session-return-black-screen.md`
(modified) and `docs/0094-PLAN-end-session-return-black-screen.md`
(untracked) appear.

**13b.** Operator performs C5 on `s22+` (build via `make apk`, install).
P4b's authoritative confirmation is the automated C7 test — it cannot
be forced deterministically on-device; C5 covers P1–P3:

1. **P1:** open the only session → menu → End session → confirm → touch
   nothing. Expected: the sessions landing screen appears with the
   empty state ("No sessions on this device"); no back press needed.
2. **P2:** create a session, open it, End session, and press the back
   arrow immediately after confirming. Expected: populated landing
   screen, ended row gone, no black screen.
3. **P3:** with a permission prompt pending in a session, open the menu
   and End session. Expected: landing screen appears; the sheet does
   not survive the exit.

**13c.** After C5 passes: set the MADR frontmatter `status: accepted`
and update `date` to the acceptance date.

**13d.** Commit the docs:

```bash
git add docs/0094-MADR-end-session-return-black-screen.md \
  docs/0094-PLAN-end-session-return-black-screen.md
git commit --no-edit
```

**13e.** Push only after the user approves the push (round-2 commits
stay local until then, per the round-1 convention):

```bash
git push origin master
```

Acceptance: CI green on the pushed tip.

## Verification

| ID | What it proves | How | Status at plan time |
| --- | --- | --- | --- |
| C1 | No rogue pop of the last page (black screen) | race test, fails without D1 | committed in `81c4fb0` |
| C2 | Happy path lands on a populated list | repro test | committed in `81c4fb0` |
| C3 | No regressions in adjacent suites | `sessions_screen_test`, `chat_render_test`, `replaced_close_test` | Step 1 |
| C4 | Format + analyzer clean | `dart format --set-exit-if-changed`, `flutter analyze` | Steps 1/4 |
| C5 | On-device behavior for P1–P3 | manual scenarios, Step 13b | Step 13 |
| C6 | Modal path lands without user action (P3) | Step 2 test: red on baseline, green after Step 3 | committed in `72adc47`/`de7ea5d` |
| C7 | Lost-ok delete still lands (P4b); genuine failure stays in chat (P4) | Step 7 tests: P4b red on baseline → green after Step 8; P4 contrast invariant both sides | Steps 7–8 |
| C8 | Post-delete events are dropped (D8 ghost) | Step 9 test: red on baseline, green after Step 10 | Steps 9–10 |

Determinism notes:

* Every RED step names its exact expected failing assertion
  (`find.text('Sessions')` findsOneWidget for C6/C7; the final `byId`
  assertion for C8) — a different failure mode is new evidence, not a
  pass, and execution stops for re-examination.
* The D6/D7 mechanism contains no timers, post-frame callbacks, or
  retry logic; its inputs are synchronous state reads plus the single
  confirming list read on the failure path (D7).
* The fake clients' `deleteGate`/`failDelete`/`failDeleteKeepsRow` flags
  are the sole timing and failure controls; no `sleep`/`Duration` waits
  gate correctness (the 100 ms pump in C1 only positions the probe
  inside the exit transition).
* Toast assertions use a single `tester.pump()` before
  `pumpAndSettle`: the notification overlay animates away on an
  `AnimationController` (3 s / 5 s), which `pumpAndSettle` would
  fast-forward past.
* The D7 confirming read is one `session.list`, issued once, with a
  conservative fallback (read failure ⇒ row assumed present ⇒ error
  toast).
* The D8 tombstone check sits at the top of `_onEvent`, so dropped ids
  never reach `_noteSeq` — gap tracking, `knownSessionIds()`, and the
  reconnect resync cannot see a ghost.
* The C7 fake removes the session row **before** throwing, modeling
  host truth (purge happened, `ok` lost); the tombstone lifecycle
  (`syncFromMeta` removal, `clearAll`) mirrors the daemon's `purged`
  set, so a re-created id is never suppressed.

## Rollout and Rollback

* Rollout: the round-2 commits land on master via Step 13. CI's Flutter
  job enforces the same trio as Step 12. No daemon or protocol changes
  accompany this change; installed daemons need no action. The D8
  tombstone is phone-side in-memory state and requires no migration.
* Rollback: `git revert` of the round-2 commits (Steps 7–10 and the
  docs commit). This restores the `de7ea5d` behavior (D6 without the
  D7 confirmation or the D8 tombstone): the black-screen and modal-path
  fixes stay; the lost-ok stranding and the ghost-transcript class
  return. There is no data migration and no state to unwind — the
  tombstone set dies with the process.
