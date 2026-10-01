# Implement "Close the residual end-session defects 0094 left reachable, and fix the request-timeout and idempotency gaps behind them"

Associated MADR: [0095-MADR-post-0094-assessment-and-debug-pass.md](0095-MADR-post-0094-assessment-and-debug-pass.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

## Goal

Make MADR 0094's positive requirement true of the **screen**, not only of
the delete's own pop — no reachable path from the chat may empty the
navigator or strand the user above a purged session — and close the
request-lifecycle and hygiene findings behind it (F1–F11).

Requirement (verbatim, inherited from 0094-PLAN):

> **A user ending a session from within the session window is returned to
> the main session screen without having to hit the back arrow or take
> any action; the landing screen shows the post-delete list.** No
> completion path of the delete may leave a blank or black frame.

Extended by this plan to: **no completion path may leave the user above a
chat whose session has been purged**, and **no local state is pruned from
a snapshot the host did not vouch for as complete**.

## Grounding

Baseline `b0e7261` (2026-08-16), verified for this plan:

* `go vet ./...` clean; `staticcheck ./...` clean.
* `go test ./...` — one failure, `TestSetupWritesDefaultMcrelayConfig`
  (F8). This is the *only* failure and the reason 0094-PLAN Steps 5/12
  declared `make preflight` unusable locally.
* `flutter analyze` clean; `flutter test` — 992 passed, 3 skipped.
* MADR 0094 D1–D8 are all present in the tree and match the record.

Round map — three rounds, each independently shippable and revertable:

| Round | Findings | Surface | Steps |
| --- | --- | --- | --- |
| **A** | F1, F2, F3 (all S1) | `apps/mobile` chat | 1–10 |
| **B** | F4 (S2) | `apps/mobile` sessions list | 11–12 |
| **C** | F5–F11 | daemon + client timeouts + service test | 13–24 |

Round A must land first: its three findings are the ones that falsify
0094's stated Consequences. Rounds B and C have no dependency on each
other and may be re-ordered, but not moved ahead of A.

## Codebase anchors (verified 2026-08-16)

Every edit below is anchored to stable code text, not line numbers.
Facts the executor may rely on without re-deriving:

**Phone — chat**

* `_endSessionFlow`, `_completeEndSessionFlow` and `_endSession` are all
  in `apps/mobile/lib/features/chat/chat_screen.dart`.
  `_completeEndSessionFlow` is synchronous and begins `if (!mounted) return;`.
* The delete-failure catch currently sets
  `rowSurvives = snap.sessions.any((s) => s.id == widget.sessionId);`.
* `SessionListSnapshot` exposes `complete`, `degraded`, `skipped`
  (`apps/mobile/lib/data/protocol/models.dart`, class at :162). Both
  existing consumers honour `complete`
  (`sessions_screen.dart` `_refresh`, `session_synchronizer.dart`).
* `chat_screen.dart` already imports `go_router` — it calls both
  `context.go('/sessions')` (in `_completeEndSessionFlow`) and
  `context.push('/sessions/<id>')` (in `_forkSession`). No new import is
  needed for `GoRouter.of`.
* The send-failure toast is in `_sendPrompt`'s `catch`, inside
  `if (mounted) { … }`, and reads:
  `showTopNotification(context, 'Send failed: $msg', severity: NoticeSeverity.error, actionLabel: 'Sessions', onAction: () { if (mounted) Navigator.of(context).pop(); },);`
* `_endingSession` (`chat_screen.dart`) is read in exactly one place —
  the early return in `_endSession`. Nothing else consults it.
* The chat overflow menu is the `PopupMenuButton<String>` with
  `tooltip: 'Session actions'`. For `_provider == 'kilo'` and an empty
  `remoteCommands`, it renders **`Session diagnostics`, `View file diff`,
  `Fork session`** in addition to `Stop current turn` and `End session`.
  The existing nav tests all use `provider: 'kilo'`, so those items are
  already reachable in that harness.
* `_forkSession` awaits `client.forkSession(sessionId)` then
  `await context.push('/sessions/<id>?name=…')`.
* `_viewDiagnostics` awaits `client.sessionDiagnostics(sessionId)` then
  `showDialog<void>(… _DiagnosticsDialog …)`.
  `SessionDiagnostics({this.branch = '', this.defaultBranch = '', this.vcs, this.mcp = const []})`
  — all optional, so `SessionDiagnostics()` constructs.

**Phone — sessions list**

* `sessions_screen.dart` `_endSession(SessionMeta s)` guards on
  `_endingIdBusy`; `_endSessionFlow(SessionMeta s)` holds the confirm
  dialog, the optimistic row removal, the try/catch and
  `friendlyOpError`. The row menu is a `PopupMenuButton` whose
  `End session` item has `value: 'end'`; the confirm dialog's affirmative
  is a `FilledButton` labelled `End session`.

**Phone — transcripts**

* `TranscriptsNotifier` (`apps/mobile/lib/state/transcripts_notifier.dart`):
  `_cleared` is declared immediately after
  `final Map<String, Timer> _cacheTimers = {};`; `clearSession` begins
  `_cleared.add(sessionId);`; `_onEvent` begins
  `final id = ev.sessionId; if (id.isEmpty) return;` followed by the
  `_cleared.contains(id)` drop; `syncFromMeta` begins
  `final liveIds = …; _cleared.removeAll(liveIds);`.
* `transcript_ingest_test.dart` uses a bare `makeContainer()` with
  `addTearDown(c.dispose)` and `useFakePathProvider` in `setUp`; it drives
  `debugOnEvent` directly with no client override.

**Phone — client**

* `McremoteClient.request(String type, {…, Duration timeout = const Duration(seconds: 30), …})`
  in `apps/mobile/lib/data/ws/mcremote_client.dart`.
* Only four call sites pass `timeout:` explicitly:
  `permission.receipt` (10 s), `pair.claim` (20 s), `session.create`
  (120 s), `session.prompt` (60 s). The first two are handled inline on
  the daemon's read loop and are **out of scope** for the ladder.
* `deleteSession` and `closeSession` pass `idempotentRetry: true`;
  `request` retries once with the same id on `TimeoutException`.
* `friendlyOpError` is in `apps/mobile/lib/data/ws/mc_exception.dart` and
  switches on `e.code`.

**Daemon**

* `dispatchAsync`, `asyncOpTimeout`, `isMutatingAsync` and
  `handleSessionList` are in `internal/ws/server.go`.
  `asyncOpTimeout` today: 120 s create; 60 s prompt / `models.list` /
  `agents.list` / `agent_sessions.list`; 30 s history; 30 s default.
* 25 message types reach `dispatchAsync` (full list in Step 21's table).
* `idempotencyLedger` is `internal/ws/idempotency.go`:
  `begin` / `complete` / `capture` / `fail` / `purgeLocked`,
  `maxEntries = 256`, `ttl = 10 * time.Minute`.
* `protocol.ErrorCodes()` (`internal/protocol/errors.go`) is a stable
  ordered registry of every error code; `internal/ws/error_codes_test.go`
  asserts against it. A new code must be added to both.
* `Manager.purged` (`internal/session/manager.go`) is written by
  `markPurged`, read by `writePersist` (twice), cleared by `clearPurged`
  — called only from Create. `persistDebounce = 2 * time.Second`,
  `historyMaxLatency = 5 * time.Second`.
* Prior art for a bounded id set in this repo:
  `internal/provider/acphttp/session.go` `answeredPerms` +
  `answeredOrder` + `const maxAnsweredPerms = 256`, trimmed with
  `for len(s.answeredOrder) > maxAnsweredPerms { … }`.
* `closeMatching` calls `Purge` only via
  `if ps, ok := e.sess.(provider.PurgeSession); ok`.
  `httpagent` and `codex` implement it; `acphttp` does not.
* `acphttp`: `func (p *Provider) caps() acp.AgentCapabilities`;
  `p.framer()` returns `(fr, error)`; `session` has immutable `agentID`;
  `Close` sends `session/close` with `{"sessionId": s.agentID}` then
  deletes `p.sessions[s.agentID]`. `ListAgentSessions` gates on
  `p.caps().SessionCapabilities.List == nil`.
* acp-go-sdk v0.13.5: `AgentMethodSessionDelete = "session/delete"`;
  `SessionCapabilities.Delete *SessionDeleteCapabilities`;
  `UnstableDeleteSessionRequest{ SessionId SessionId }`. **The method is
  marked UNSTABLE in the SDK** ("may be removed or changed at any
  point") — hence the capability gate and the live-tagged pin.
* `internal/cli/service`: `service.OverrideInstallOS(string) (restore func())`
  and `service.OverrideRunSystemctl(func(...string) error) (restore func())`
  and `service.OverrideRunLaunchctl(...)` are exported test seams.
  `TestSetupIdempotentRerun` already uses the first two and carries a
  comment describing exactly the failure F8 hits.
* `make preflight` runs gofmt → tidy → vet → staticcheck → `go test -race`
  → script tests → units → release build → dart format → flutter analyze
  → flutter test.

## Scope

In scope:

* `apps/mobile/lib/features/chat/chat_screen.dart` — D2 (F1), D3 (F2),
  D4/D4b (F3).
* `apps/mobile/lib/features/sessions/sessions_screen.dart` — D5 (F4).
* `apps/mobile/lib/state/transcripts_notifier.dart` — D8 phone half (F7).
* `apps/mobile/lib/data/ws/mcremote_client.dart` — D7 client half (F9).
* `internal/ws/server.go`, `internal/ws/idempotency.go` — D6 (F5), F6,
  D7 daemon half (F9).
* `internal/protocol/errors.go` — one new code for D6.
* `internal/protocol/op_timeouts.json` (new) — the shared timeout table.
* `internal/session/manager.go` — D8 daemon half (F7).
* `internal/ws/resume.go` — F11.
* `internal/provider/acphttp/session.go` — D10 (F10).
* `internal/cli/service/setup_test.go` — D9 (F8).
* Tests: `chat_end_session_navigation_test.dart`,
  `sessions_screen_test.dart`, `transcript_ingest_test.dart`,
  `internal/ws/idempotency_test.go`, `internal/ws/op_timeout_test.go`
  (new), `apps/mobile/test/op_timeout_ladder_test.dart` (new),
  `internal/session/manager_test.go`,
  `internal/provider/acphttp/live_purge_test.go` (new),
  `internal/ws/resume_store_test.go`.
* `Makefile` — a `live-goose` target.
* MADR 0095 status update and the D4 probe record.

Out of scope:

* Any protocol version change. F9 adjusts timeouts; the wire is
  unchanged. The one new **error code** is additive and v1-compatible
  (unknown codes already flow through `friendlyOpError`'s fallback).
* `session.delete` semantics and the absence of a delete broadcast
  (0093 D2, 0094 D4).
* Re-introducing an awaited chat push future (MADR 0046 L-12).
* `internal/procutil` coverage (noted in the MADR as an inventory task,
  not remediation).
* Changing the meaning of `session.close` (`purge=false`) for any
  provider; D10 touches only the `purge=true` path.

## Execution state (2026-08-16)

**Steps 1–21 executed; Steps 22–24 partly outstanding.**

| Step | Status |
| --- | --- |
| 1 baseline | done — one expected failure (F8), vet/staticcheck/analyze clean |
| 2–3 C9 / D2 | done — RED at the named assertion, then green |
| 4–5 C10 / D3 | done — see E1–E3 |
| 6 D4 probe | done — selected **M1**; table in the MADR |
| 7–8 C11 / D4 | done — RED both tests, then green |
| 9–10 Round A gate + docs | done — 997 mobile tests, format/analyze clean |
| 11–12 C12 / D5 | done — see E6 |
| 13–14 C13/C14 + D6/F6 | done — see E7, E8 |
| 15 C15 + D8 bounds | done, both languages |
| 16 D9 | done — `go test ./...` fully clean for the first time; see E9 |
| 17–18 C17 + D7 ladder | done — shared table + both test suites; see E10 |
| 19 C18 + D10 | code done, **capability absent on the installed goose**; see E11 |
| 20 F11 | done |
| 21 full gate | **`make preflight` green end to end**, 1003 mobile tests |
| 22 device confirmation (C5) | **outstanding — needs the `s22+` handset** |
| 23 MADR acceptance | **held** — status stays `proposed` until C5 |
| 24 commit | done; **not pushed** (pending approval) |

## Execution deviations

Recorded as they were found, per the plan's own "a different failure mode
is new evidence" rule. Each was diagnosed before any test was adapted.

* **E1 (Step 4) — `tester.pageBack()` is unusable while a notification is
  on screen.** The first draft of both C10 tests passed on the unfixed
  code. Diagnosis: the notification card is `Positioned` at
  `MediaQuery.padding.top + 8` across the full width, so it covers the
  app bar; `pageBack()` taps the back button's coordinates, the tap lands
  on the toast's `Material`, and the route never pops. `tester.tap` only
  *warns* on a missed hit, so the test read as a pass. The C10 tests
  drive the system back through `tester.binding.handlePopRoute()`
  instead. 0094's C1 is unaffected — no toast is up at its back press, so
  its `pageBack()` reaches the button.
* **E2 (Step 4) — `find.text('Sessions')` became ambiguous.** The toast's
  own action button is labelled `Sessions`, so the existing idiom for
  "the landing screen is showing" now matches the button too. The new
  tests use `find.widgetWithText(AppBar, 'Sessions')`; the pre-existing
  tests are left alone (no toast is up at their assertions).
* **E3 (Step 4) — the second C10 test needs two sessions.** Asserting
  "the action still navigates after the chat is disposed" is vacuous if
  the user is already on `/sessions`. The test now opens a second
  session's chat before tapping the toast, so the assertion has somewhere
  to fail.
* **E4 (Step 6) — the D4 probe selected M1** with no ambiguity; D4b was
  not needed. Full table in MADR 0095, "D4 probe evidence".
* **E5 (Step 7) — C11's landing assertions use the E2 finder** for the
  same reason, and the two tests were given ids `sess-i2` / `sess-j` to
  avoid colliding with C10's `sess-i`.
* **E6 (Step 11) — `agent_message` is not a real event type.** The C12
  helper's transcript seed was invented; unknown types are ignored by the
  reducer, so no transcript was created and all three tests failed in the
  helper rather than the assertion. Uses `user_message` with a `seq`, the
  shape `transcript_ingest_test.dart` already exercises.
* **E7 (Step 13) — the first C14 test could not see the defect.** It used
  a single in-flight entry, where the overshoot is 1 and self-corrects.
  A probe across in-flight fractions showed the real behaviour (200
  in-flight settles at 328, 255 at 518 — about 2x the cap, permanently),
  and C14 was rewritten to that shape: RED 10/10 before the fix. The
  measurement is recorded in MADR 0095 under "F6 — severity re-measured";
  the original F6 text overstated the single-in-flight case.
* **E8 (Step 14) — `retry_no_result` needed a docs entry.**
  `TestErrorCodesAreDocumented` requires every registered code to appear
  in `docs/protocol-v1.md`; the code was added to both tables there.
* **E9 (Step 16) — a pre-existing flaky test surfaced, recorded as F12.**
  `TestCloseAllKeepsSessionsListable` failed 5 times in 30 runs on the
  untouched baseline `b0e7261` (verified in a scratch worktree, so it is
  not attributable to this plan). It blocked Step 21's "preflight green"
  acceptance, so it was fixed here: the ordering assertion now covers the
  tie-break the sort actually implements. Green 60/60.
* **E10 (Step 18) — the nullable `timeout` broke two test fakes.**
  `session_diff_fork_test.dart` and `session_history_paging_test.dart`
  override `request` and had to move to `Duration? timeout` to keep
  matching the base signature. Mechanical, no behaviour change.
* **E11 (Step 19) — the capability probe needed a seam.** Nothing exposed
  `SessionCapabilities.Delete` outside `acphttp`, and surfacing it on the
  wire was out of scope, so `Provider.AdvertisesSessionDelete()` was
  added for the live test to record. It answered **false** on the
  installed goose, so F10 is a documented limitation — see the MADR.

## Implementation Steps

Convention for every step, inherited from 0094-PLAN: each RED step names
the **exact expected failing assertion**. A different failure mode is new
evidence — stop and re-examine rather than proceeding. Phase commits use
the repo's hook: `git add <paths>` then `git commit --no-edit`, never
`-m`. Run `make pre-add-check FILES="…"` before staging Go files if the
agent gate is not active in the shell.

---

## Round A — the three S1 findings

### Step 1 — Verify the baseline (read-only)

```bash
cd apps/mobile
flutter test test/chat_end_session_navigation_test.dart \
  test/sessions_screen_test.dart test/transcript_ingest_test.dart \
  test/chat_render_test.dart test/replaced_close_test.dart \
  test/chat_send_failure_test.dart
dart format --output=none --set-exit-if-changed .
flutter analyze
```

Acceptance: all pass; format reports no changes; analyze reports no
issues. Then, from the repo root:

```bash
go vet ./... && staticcheck ./...
go test ./... 2>&1 | grep -E '^(---)? ?FAIL'
```

Acceptance: vet and staticcheck clean; the **only** `FAIL` line is
`TestSetupWritesDefaultMcrelayConfig`. Any second failure is new
evidence — stop.

### Step 2 — C9: the D7 confirming read must not trust an incomplete snapshot (expect RED)

**2a.** In `apps/mobile/test/chat_end_session_navigation_test.dart`,
extend `_FakeClient` with one field and honour it in the snapshot:

```dart
  bool failDelete = false;
  bool failDeleteKeepsRow = false;
  /// MADR 0095 F1: model a host whose store enumeration skipped rows —
  /// a successful session.list_result carrying a PARTIAL list. Absence
  /// from such a list is not evidence of removal (MADR 0056 H-6).
  bool listIncomplete = false;
```

and in `listSessionSnapshot`:

```dart
  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listCalls++;
    return SessionListSnapshot(sessions: sessions, complete: !listIncomplete);
  }
```

**2b.** Append the test (reuses `pumpApp`; `McException` resolves through
the file's existing `app_providers.dart` import):

```dart
  testWidgets(
    'a delete that fails against an incomplete list keeps the user in the chat',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-f', provider: 'kilo', name: 'Zeta')],
      );
      // The purge did NOT happen; the row is missing from the confirming
      // read only because the host could not enumerate it (complete=false).
      client.failDelete = true;
      client.listIncomplete = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Zeta'));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      // One pump: the fake-client microtask chain has unwound and the
      // toast is up (pumpAndSettle would animate it away).
      await tester.pump();

      // D2 (MADR 0095): a partial list cannot confirm a purge. Conservative
      // branch — error toast, user stays in the chat, transcript intact.
      expect(client.deleteCalls, ['sess-f']);
      expect(find.textContaining('End session failed'), findsOneWidget);
      expect(find.text('Session ended'), findsNothing);

      await tester.pumpAndSettle();
      expect(find.text('Zeta'), findsWidgets);
      expect(find.text('Sessions'), findsNothing);

      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
        listen: false,
      );
      expect(
        container.read(transcriptsProvider.notifier).debugIsCleared('sess-f'),
        isFalse,
        reason: 'an unconfirmed purge must not tombstone the session',
      );
    },
  );
```

**2c.** `debugIsCleared` does not exist yet. Add it to
`TranscriptsNotifier` next to the other `@visibleForTesting` accessors
(`debugLastSeq`, `debugFirstSeq`):

```dart
  /// Whether [sessionId] carries a D8 tombstone (MADR 0094 D8 / 0095 F1).
  @visibleForTesting
  bool debugIsCleared(String sessionId) => _cleared.contains(sessionId);
```

**2d.** Run:

```bash
cd apps/mobile && flutter test test/chat_end_session_navigation_test.dart
```

Acceptance: the five committed tests pass; the new test **fails** on
`expect(find.textContaining('End session failed'), findsOneWidget)` —
today the row's absence alone drives the success path, so the success
toast shows instead.

**2e.** Phase commit:

```bash
git add apps/mobile/test/chat_end_session_navigation_test.dart \
  apps/mobile/lib/state/transcripts_notifier.dart
git commit --no-edit
```

### Step 3 — D2: require `complete` for the absent classification (expect GREEN)

In `chat_screen.dart` `_endSessionFlow`'s catch, replace the single
`rowSurvives` assignment:

```dart
      var rowSurvives = true;
      try {
        final snap = await client.listSessionSnapshot();
        // MADR 0095 D2: only a COMPLETE snapshot is evidence of removal.
        // The daemon returns a successful session.list_result with
        // complete=false when its store enumeration skipped rows
        // (internal/session/manager.go ListSnapshot), and pruning local
        // state from a partial list is exactly what MADR 0056 H-6 forbids
        // — the rule TranscriptsNotifier.syncFromMeta already states.
        rowSurvives =
            !snap.complete ||
            snap.sessions.any((s) => s.id == widget.sessionId);
      } catch (_) {
        // Conservative: an unreadable list cannot confirm the purge.
      }
```

Run:

```bash
cd apps/mobile && flutter test test/chat_end_session_navigation_test.dart
```

Acceptance: all six tests pass. In particular 0094's C7 ("a delete whose
ok was lost still lands on the sessions screen") must still pass — its
fake returns `complete: true`, so D2 does not change it. If C7 breaks,
the fake was mis-edited in Step 2a.

Phase commit:

```bash
git add apps/mobile/lib/features/chat/chat_screen.dart
git commit --no-edit
```

### Step 4 — C10: the send-failure toast action must not pop the last page (expect RED)

**4a.** Extend `_FakeClient` in
`chat_end_session_navigation_test.dart` with a failing prompt:

```dart
  /// MADR 0095 F2: fail the prompt so the chat raises its
  /// "Send failed … [Sessions]" toast, which lives in the ROOT overlay
  /// and outlives this route.
  bool failPrompt = false;

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    if (failPrompt) {
      throw McException('socket closed', code: 'connection_lost');
    }
  }
```

**4b.** Append two tests:

```dart
  testWidgets(
    'the send-failure toast action does not pop the last page after a back press',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-g', provider: 'kilo', name: 'Eta')],
      );
      client.failPrompt = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Eta'));
      await tester.pumpAndSettle();

      await tester.enterText(find.byType(TextField).first, 'hello');
      await tester.pump();
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();
      expect(find.text('Sessions'), findsNothing); // still in the chat
      expect(find.widgetWithText(TextButton, 'Sessions'), findsOneWidget);

      // The user backs out. The toast is in the root overlay, so it is
      // still on screen and still tappable; the chat State stays mounted
      // through the exit transition (the fact MADR 0094 D1 rests on).
      await tester.pageBack();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.tap(find.widgetWithText(TextButton, 'Sessions'));
      await tester.pumpAndSettle();

      // No rogue pop: the sessions route still renders.
      expect(tester.takeException(), isNull);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('No sessions on this device'), findsNothing);
      expect(find.text('Eta'), findsOneWidget);
    },
  );

  testWidgets(
    'the send-failure toast action still navigates after the chat is gone',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-h', provider: 'kilo', name: 'Theta')],
      );
      client.failPrompt = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Theta'));
      await tester.pumpAndSettle();

      await tester.enterText(find.byType(TextField).first, 'hello');
      await tester.pump();
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();

      // Re-enter the chat from the landing screen so the action has
      // somewhere to go, then let the exit transition finish before
      // tapping: `mounted` is false by now, so the pre-fix callback is a
      // silent no-op.
      await tester.pageBack();
      await tester.pumpAndSettle(const Duration(milliseconds: 400));
      await tester.tap(find.text('Theta'));
      await tester.pumpAndSettle();
      expect(find.text('Sessions'), findsNothing);

      await tester.tap(find.widgetWithText(TextButton, 'Sessions'));
      await tester.pumpAndSettle();
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Theta'), findsOneWidget);
    },
  );
```

**4c.** Run:

```bash
cd apps/mobile && flutter test test/chat_end_session_navigation_test.dart
```

Acceptance — **both** new tests fail, with these distinct signatures:

* The first fails with a navigator/router assertion or, in release-shaped
  runs, `expect(find.text('Sessions'), findsOneWidget)` finding nothing
  (the popped-last-page corruption).
* The second fails on the final `expect(find.text('Sessions'), findsOneWidget)`
  — nothing happened, because `mounted` was false.

If the toast is not found at all, the action duration
(`_kActionDuration = 6 s`, `apps/mobile/lib/theme/top_notification.dart`)
or the composer's enablement changed — stop and re-derive rather than
adding pumps.

**4d.** Phase commit.

### Step 5 — D3: router-aware exit from the root-overlay action (expect GREEN)

In `chat_screen.dart` `_sendPrompt`'s catch, capture the router **before**
showing the notification and use it in the callback:

```dart
        final msg = friendlyOpError(e);
        // MADR 0095 D3: showTopNotification inserts into the ROOT overlay
        // ("survives route changes for the app's lifetime",
        // theme/top_notification.dart), and an action notification stays
        // up for 6 s. `mounted` on this State is therefore not a proxy
        // for "my route is on top": tapped during the exit transition the
        // old Navigator.pop() popped /sessions and emptied the router
        // (the MADR 0094 D1 defect), and tapped afterwards it did nothing
        // at all. GoRouter outlives every route, so capture it here.
        final router = GoRouter.of(context);
        showTopNotification(
          context,
          'Send failed: $msg',
          severity: NoticeSeverity.error,
          actionLabel: 'Sessions',
          onAction: () => router.go('/sessions'),
        );
```

Run:

```bash
cd apps/mobile && flutter test test/chat_end_session_navigation_test.dart \
  test/chat_send_failure_test.dart
```

Acceptance: all eight nav tests pass and `chat_send_failure_test.dart` is
unaffected (it never taps the action).

Phase commit.

### Step 6 — D4 probe: measure the chat route's state at delete completion

**This step produces evidence, not code.** MADR 0095 D4 is recorded as a
decision about *what discriminates* the completion branches; its
mechanism is unproven, and 0094's own D6 probe showed that route
lifecycle state does not settle within one frame. Do not implement
Step 8 before this step's result is recorded.

**6a.** Add a temporary instrumentation hook to `chat_screen.dart` —
**this is scratch code and is reverted in 6d**. It must be a
**library-level** variable, not a static on the State: the class is
`_ChatScreenState` (private), so a test file cannot name it. Put it just
above `class ChatScreen`:

```dart
/// TEMPORARY (MADR 0095 D4 probe): (isActive, isCurrent, hadModal) as
/// observed at guard time in _completeEndSessionFlow. Reverted in 6d.
(bool, bool, bool)? debugRouteStateAtCompletion;
```

and, in `_completeEndSessionFlow`, immediately after the `hadModal`
capture and **before** `clearSession`:

```dart
    final probeRoute = ModalRoute.of(context);
    debugRouteStateAtCompletion = (
      probeRoute?.isActive ?? false,
      probeRoute?.isCurrent ?? false,
      hadModal,
    );
```

The probe test reads it via the existing
`import 'package:magic_cli_remote/features/chat/chat_screen.dart';`.

**6b.** Write `apps/mobile/test/probe_0095_route_state_test.dart` as a
copy of `chat_end_session_navigation_test.dart`'s harness with five
scenarios, each gating `deleteSession` on a `Completer` and reading
`debugRouteStateAtCompletion` after `pumpAndSettle`:

| Scenario | Setup before completing the gate | What it models |
| --- | --- | --- |
| **S1** | nothing | chat current, baseline |
| **S2** | `await tester.pageBack(); await tester.pump(const Duration(milliseconds: 100));` | user backed out mid-delete (0094 P2) |
| **S3** | open `Session diagnostics` from the overflow menu (fake returns `SessionDiagnostics()`) | untracked modal on top (F3) |
| **S4** | open `Fork session` and confirm (fake `forkSession` returns `SessionMeta(id: 'sess-fork', provider: 'kilo', name: 'Fork')`) | pushed route on top (F3) |
| **S5** | inject a `permission_request` via `debugOnEvent` so the sheet opens | 0094 D6's known-lagging case |

For S3 and S4 the fake needs:

```dart
  @override
  Future<SessionDiagnostics> sessionDiagnostics(String sessionId) async =>
      SessionDiagnostics();

  @override
  Future<SessionMeta> forkSession(
    String sessionId, {
    String? messageId,
    String? lastTurnId,
  }) async => SessionMeta(id: 'sess-fork', provider: 'kilo', name: 'Fork');
```

**6c.** Record the five `(isActive, isCurrent)` pairs. Then select the
mechanism by this table — the choice is mechanical, not a judgement
call:

| Observation | Mechanism to implement in Step 8 |
| --- | --- |
| S2 reports `isActive == false` **and** S3/S4/S5 report `isActive == true` | **M1** — `isCurrent` ⇒ pop; `isActive && !isCurrent` ⇒ bump + `go('/sessions')`; `!isActive` ⇒ bump only |
| S2 reports `isActive == true` (indistinguishable from S3/S4/S5) | **M2** — `isCurrent` ⇒ pop; otherwise ⇒ bump + `go('/sessions')` unconditionally |
| S1 reports `isCurrent == false` (the guarded pop never fires even in the baseline) | **D4b** — mechanism disproven; go to Step 8b |

M2 is within MADR D4 ("decided by route state, not a flag list") but is
*not* the wording D4 records. If the probe selects M2, amend MADR 0095
D4's mechanism sentence and add the probe table to its "More
Information" before writing code — the correction belongs in the record,
not only in this plan (0094 D6 set this precedent).

**6d.** Revert the instrumentation (`git checkout --
apps/mobile/lib/features/chat/chat_screen.dart` if nothing else is
uncommitted; otherwise remove the two probe blocks by hand) and delete
`probe_0095_route_state_test.dart`. **The probe is not committed**; its
result is committed as MADR prose in Step 10.

### Step 7 — C11: untracked routes above the chat must still land (expect RED)

Append two tests to `chat_end_session_navigation_test.dart`, adding the
`sessionDiagnostics` and `forkSession` overrides from 6b to `_FakeClient`:

```dart
  testWidgets(
    'an untracked dialog open at delete completion still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-i', provider: 'kilo', name: 'Iota')],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Iota'));
      await tester.pumpAndSettle();

      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // Nothing stops the user opening another action while the delete is
      // outstanding: _endingSession guards only a second End tap.
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Session diagnostics'));
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsOneWidget);

      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-i']);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Iota'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );

  testWidgets(
    'a forked chat pushed over the chat still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-j', provider: 'kilo', name: 'Kappa')],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Kappa'));
      await tester.pumpAndSettle();

      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // context.push keeps the old chat State ALIVE underneath — the case
      // 0094-PLAN's P5 row attributed to `mounted == false` (MADR 0095 F3).
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Fork session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Fork'));
      await tester.pumpAndSettle();

      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-j']);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Kappa'), findsNothing);
    },
  );
```

Run. Acceptance: both **fail** on
`expect(find.text('Sessions'), findsOneWidget)` — the completion takes
the bump-only branch and the user stays above the dead chat.

If the second test instead fails because `Fork session` is absent from
the menu, `remoteCommands` is non-empty in this harness; re-check the
`itemBuilder` gate before adapting the test.

Phase commit.

### Step 8 — D4 (or D4b): make the completion branch depend on route state (expect GREEN)

**8a — the D4 path (probe selected M1 or M2).** Replace the `hadModal`
capture and the three branches in `_completeEndSessionFlow`:

```dart
  void _completeEndSessionFlow() {
    if (!mounted) return;
    // Clear local state only once the host actually deleted it.
    ref.read(transcriptsProvider.notifier).clearSession(widget.sessionId);
    showTopNotification(
      context,
      'Session ended',
      severity: NoticeSeverity.success,
    );
    final route = ModalRoute.of(context);
    if (route != null && route.isCurrent) {
      // Nothing is above us: the ordinary exit, with its pop animation
      // and didPopNext refresh (MADR 0094 D1/D3).
      Navigator.of(context).pop(true);
      return;
    }
    // MADR 0095 D4: everything else is decided by route state, not by a
    // list of modal kinds. Enumerating sheets is unbounded by
    // construction — every new route in this screen was another way to
    // miss the landing (F3), and clearSession's removeRoute does not
    // restore isCurrent within the frame anyway (0094 D6 probe).
    ref.read(sessionsRevisionProvider.notifier).bump();
    if (<M1: route != null && route.isActive> /* M2: drop this guard */) {
      // Something is above the chat — a sheet, a dialog, or a pushed
      // forked chat. A go exit is deterministic and cannot pop the last
      // page; it does not fire didPopNext, so the bump above owns the
      // landing-screen refresh.
      context.go('/sessions');
    }
    // M1 only: !isActive means the user popped us and is already on the
    // landing screen; the bump is the whole job.
  }
```

Substitute exactly one of the two forms recorded in Step 6c. Delete the
now-unused `hadModal` local. Leave `_permissionSheetOpen`,
`_questionSheetOpen` and `_openAlwaysConfirmRoute` in place — they are
read elsewhere for sheet retirement; confirm with
`grep -n "_permissionSheetOpen\|_questionSheetOpen\|_openAlwaysConfirmRoute" lib/features/chat/chat_screen.dart`
and only remove a field if this was its sole reader.

**8b — the D4b path (probe disproved both M1 and M2).** Keep
`_completeEndSessionFlow` as committed and instead make the untracked
routes unreachable during a delete:

1. Promote `_endingSession` to a `setState`-driven field so the menu
   rebuilds when it flips.
2. In the overflow menu's `itemBuilder`, wrap `diagnostics`, `diff` and
   `fork` in `if (!_endingSession)`, and in `onSelected` early-return for
   those three when `_endingSession` is true (the menu may already be
   open when the flag flips).
3. In `_userMessageActions` and the config-option sheet, early-return
   when `_endingSession` is true.

D4b makes the C11 fork/diagnostics tests pass by making the routes
unopenable; adjust their assertions to
`expect(find.text('Session diagnostics'), findsNothing)` after the
second menu tap, and note in the test that this is the D4b shape.

**8c.** Run:

```bash
cd apps/mobile
flutter test test/chat_end_session_navigation_test.dart \
  test/sessions_screen_test.dart test/transcript_ingest_test.dart \
  test/chat_render_test.dart test/replaced_close_test.dart \
  test/chat_send_failure_test.dart
```

Acceptance: every test passes, **including 0094's C6** ("a permission
sheet open at delete completion still lands on the sessions screen") —
under M1/M2 that case now reaches the `go` through the generic branch
rather than the `hadModal` flag, and it must not regress.

Phase commit.

### Step 9 — Round A gate

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

Acceptance: format clean, analyze clean, **999 tests pass** (992 baseline
+ C9 + two C10 + two C11 + the two probe-derived fakes add no tests;
adjust the expected count to what Step 1 measured plus five if the
baseline moved).

### Step 10 — Record the probe and commit Round A

**10a.** Add a `### D4 probe evidence (2026-…)` subsection to MADR 0095's
"More Information" with the five measured `(isActive, isCurrent)` pairs,
the mechanism selected, and — if M2 was selected — the amended D4
mechanism sentence. This mirrors 0094's "Modal-path probe evidence (D6)".

**10b.**

```bash
git add docs/0095-MADR-post-0094-assessment-and-debug-pass.md \
  docs/0095-PLAN-post-0094-assessment-and-debug-pass.md
git commit --no-edit
```

Round A is now complete and independently revertable.

---

## Round B — sessions-list parity (F4)

### Step 11 — C12: the sessions-list End must classify a lost `ok` (expect RED)

**11a.** In `apps/mobile/test/sessions_screen_test.dart`, make
`MockMcremoteClient.sessions` mutable and add the delete surface:

```dart
  List<SessionMeta> sessions;              // was: final List<SessionMeta>
  final List<String> deleteCalls = [];
  bool failDelete = false;
  bool failDeleteKeepsRow = false;
  bool listIncomplete = false;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(sessions: sessions, complete: !listIncomplete);

  @override
  Future<void> cancel(String sessionId) async {}

  @override
  Future<void> deleteSession(String sessionId) async {
    deleteCalls.add(sessionId);
    // Host truth for the lost-ok case: the purge happened, the ok did not
    // arrive (MADR 0094 D7 / 0095 F4).
    if (!failDeleteKeepsRow) {
      sessions = sessions.where((s) => s.id != sessionId).toList();
    }
    if (failDelete) throw McException('timed out', code: 'timeout');
  }
```

Removing `final` is safe: no existing test in the file reassigns or
relies on the field's finality (verify with
`grep -n "\.sessions" test/sessions_screen_test.dart`).

**11b.** `_wrap` builds a bare `ProviderScope`; the new tests need the
container to assert transcript state, so add a sibling helper rather than
changing `_wrap`:

```dart
Widget _wrapWith(ProviderContainer container) => UncontrolledProviderScope(
  container: container,
  child: const MaterialApp(home: SessionsScreen()),
);
```

**11c.** Append the two tests:

```dart
  testWidgets('a list-screen delete whose ok was lost is treated as ended', (
    tester,
  ) async {
    final client = MockMcremoteClient(
      sessions: [SessionMeta(id: 's-lost', provider: 'kilo', name: 'Lost')],
    );
    client.failDelete = true;
    final container = ProviderContainer(
      overrides: [
        connectionStateProvider.overrideWith(
          (ref) => Stream.value(McConnectionState.connected),
        ),
        mcremoteClientProvider.overrideWithValue(client),
      ],
    );
    addTearDown(container.dispose);
    // Give the session a transcript so the cleanup is observable.
    // debugOnEvent flushes the batch window synchronously, so byId is
    // populated by the time the next line runs.
    container.read(transcriptsProvider.notifier).debugOnEvent(
      SessionEvent(type: 'agent_message', sessionId: 's-lost', text: 'hi'),
    );
    expect(container.read(transcriptsProvider).byId.containsKey('s-lost'), isTrue);

    await tester.pumpWidget(_wrapWith(container));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_vert).first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('End session'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'End session'));
    await tester.pump();

    expect(client.deleteCalls, ['s-lost']);
    expect(find.textContaining('End failed'), findsNothing);
    await tester.pumpAndSettle();
    expect(
      container.read(transcriptsProvider).byId.containsKey('s-lost'),
      isFalse,
      reason: 'a confirmed purge must clear the local transcript',
    );
  });

  testWidgets('a list-screen delete that genuinely failed keeps the error', (
    tester,
  ) async {
    final client = MockMcremoteClient(
      sessions: [SessionMeta(id: 's-keep', provider: 'kilo', name: 'Keep')],
    );
    client.failDelete = true;
    client.failDeleteKeepsRow = true;
    // … same scaffolding …
    expect(find.textContaining('End failed'), findsOneWidget);
    expect(
      container.read(transcriptsProvider).byId.containsKey('s-keep'),
      isTrue,
    );
  });
```

Acceptance: the first **fails** on
`expect(find.textContaining('End failed'), findsNothing)`; the second
passes on the baseline (it pins existing behaviour).

Phase commit.

### Step 12 — D5: give the sessions-list End the D7 classification (expect GREEN)

**12a.** In `sessions_screen.dart` `_endSessionFlow`, extract the success
tail — everything from `notifications.dropSessionAsks(s.id);` through
`await _refresh();` — into a private method placed immediately after
`_endSessionFlow`:

```dart
  /// Shared completion tail for the success path and the confirmed-purge
  /// path (MADR 0094 D7, extended to this screen by MADR 0095 D5). No
  /// navigation branches here: the user is already on the landing screen.
  Future<void> _completeEndSession(
    SessionMeta s,
    String label,
    NotificationCoordinator notifications,
  ) async {
    notifications.dropSessionAsks(s.id);
    if (!mounted) return;
    ref.read(transcriptsProvider.notifier).clearSession(s.id);
    showTopNotification(
      context,
      'Ended $label',
      severity: NoticeSeverity.success,
    );
    await _refresh();
  }
```

`notifications` is the local already read at the top of
`_endSessionFlow` (`final notifications = ref.read(notificationCoordinatorProvider);`),
passed in rather than re-read so both call sites share one instance.
Confirm its declared type from that line and use it verbatim in the
signature.

**12b.** Replace the catch:

```dart
    } catch (e) {
      if (!mounted) return;
      // MADR 0095 D5: same lost-ok classification the chat screen makes
      // (0094 D7) — session.delete is idempotentRetry and the daemon
      // errors on a double delete, so a purge whose ok was lost arrives
      // here. Only a COMPLETE snapshot is evidence of removal (0095 D2).
      var rowSurvives = true;
      try {
        final snap = await client.listSessionSnapshot();
        rowSurvives =
            !snap.complete || snap.sessions.any((x) => x.id == s.id);
      } catch (_) {
        // Conservative: an unreadable list cannot confirm the purge.
      }
      if (!mounted) return;
      if (!rowSurvives) {
        await _completeEndSession(s, label, notifications);
        return;
      }
      await _refresh();
      if (!mounted) return;
      showTopNotification(
        context,
        'End failed: ${friendlyOpError(e)}',
        severity: NoticeSeverity.error,
      );
    }
```

Note the confirming read replaces the *first* `_refresh()` on the failure
path only when the purge is confirmed; the genuine-failure path keeps its
existing refresh-then-toast order.

**12c.** Run and gate:

```bash
cd apps/mobile
flutter test test/sessions_screen_test.dart test/chat_end_session_navigation_test.dart
dart format --output=none --set-exit-if-changed . && flutter analyze
```

Acceptance: both suites pass; format and analyze clean.

Phase commit.

---

## Round C — daemon, timeouts, hygiene

### Step 13 — C13/C14: the ledger's silent paths and its cap (expect RED)

Append to `internal/ws/idempotency_test.go`:

```go
// A waiter parked on an in-flight entry whose owner fails must not be left
// with nothing: the ledger drops the key so the RETRY may execute, and
// dispatchAsync must re-begin rather than return silently (MADR 0095 F5).
func TestIdempotencyFailReleasesWaiterForReexecute(t *testing.T) {
	l := newIdempotencyLedger()
	_, _, action := l.begin("dev", "req-fail")
	if action != idemExecute {
		t.Fatal("want execute")
	}
	var got []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, wait, a := l.begin("dev", "req-fail")
		if a != idemWait || wait == nil {
			t.Errorf("want wait, got %v", a)
			return
		}
		got = wait(context.Background())
	}()
	time.Sleep(20 * time.Millisecond)
	l.fail("dev", "req-fail")
	wg.Wait()
	if got != nil {
		t.Fatalf("waiter frame = %q, want nil", got)
	}
	// The contract dispatchAsync relies on: after the wait yields nothing,
	// a fresh begin must hand the caller the work rather than replay.
	if _, _, a := l.begin("dev", "req-fail"); a != idemExecute {
		t.Fatalf("after a failed original, want execute, got %v", a)
	}
}

// maxEntries must hold regardless of Go's randomized map iteration order:
// the old loop returned from purgeLocked as soon as iteration happened to
// reach the in-flight entry (MADR 0095 F6).
func TestIdempotencyPurgeEnforcesCapWithInFlightEntry(t *testing.T) {
	l := newIdempotencyLedger()
	l.begin("dev", "inflight") // never completed
	for i := 0; i < l.maxEntries+64; i++ {
		id := fmt.Sprintf("req-%d", i)
		l.begin("dev", id)
		l.complete("dev", id, []byte(`{"type":"ok"}`))
	}
	l.mu.Lock()
	n := len(l.entries)
	_, stillThere := l.entries[idemKey("dev", "inflight")]
	l.mu.Unlock()
	if n > l.maxEntries+1 {
		t.Fatalf("ledger = %d entries, want <= %d", n, l.maxEntries+1)
	}
	if !stillThere {
		t.Fatal("purge must never evict an in-flight entry")
	}
}
```

Add `"fmt"` to the file's imports.

Run:

```bash
go test ./internal/ws/ -run 'TestIdempotency' -count=1 -v
```

Acceptance: `TestIdempotencyFailReleasesWaiterForReexecute` **passes**
today (it pins existing ledger behaviour that the dispatchAsync fix will
rely on); `TestIdempotencyPurgeEnforcesCapWithInFlightEntry` **fails** on
`ledger = N entries, want <= 257`. Because the failure depends on map
order, run with `-count=20` to confirm it is not flaky-green:

```bash
go test ./internal/ws/ -run TestIdempotencyPurgeEnforcesCapWithInFlightEntry -count=20
```

Acceptance: it fails on the large majority of iterations. If it passes
all twenty, the cap is being reached another way — stop and re-derive.

Phase commit.

### Step 14 — D6 + F6: never return silence; enforce the cap (expect GREEN)

**14a.** `internal/protocol/errors.go` — add the code next to
`ErrDeadlineExceeded`:

```go
	// ErrRetryNoResult means a retried request matched a completed ledger
	// entry that captured no response frame, so the daemon cannot replay
	// the original answer. The operation itself is NOT known to have
	// failed — the client must re-read state rather than assume either
	// outcome (MADR 0095 D6).
	ErrRetryNoResult = "retry_no_result"
```

and add `ErrRetryNoResult,` to `ErrorCodes()` immediately after
`ErrDeadlineExceeded,` (the registry order is documented as stable and
`internal/ws/error_codes_test.go` asserts against it).

**14b.** `internal/ws/idempotency.go` — fix `purgeLocked`:

```go
func (l *idempotencyLedger) purgeLocked() {
	now := time.Now()
	for k, e := range l.entries {
		if e.finished && now.Sub(e.at) > l.ttl {
			delete(l.entries, k)
		}
	}
	// Scan for a finished victim rather than giving up at the first
	// in-flight entry: map iteration order is randomized, so the old
	// early return made the cap a coin flip per call (MADR 0095 F6).
	for len(l.entries) > l.maxEntries {
		victim := ""
		for k, e := range l.entries {
			if e.finished {
				victim = k
				break
			}
		}
		if victim == "" {
			return // nothing finished; never evict in-flight work
		}
		delete(l.entries, victim)
	}
}
```

**14c.** `internal/ws/server.go` — replace the ledger block in
`dispatchAsync`:

```go
		// Idempotent replay for mutating ops with a client request id.
		if s.idem != nil && deviceID != "" && env.ID != "" && isMutatingAsync(env.Type) {
			frame, wait, action := s.idem.begin(deviceID, env.ID)
			switch action {
			case idemReplay:
				if len(frame) > 0 {
					_ = s.writeBytes(c, frame)
					return
				}
				// The original succeeded but captured no frame. Guessing
				// `ok` would invent a result; silence would leave the
				// client to time out (MADR 0095 D6/F5).
				_ = s.writeError(opCtx, c, env.ID, protocol.ErrRetryNoResult,
					"the original request completed but its response is unavailable")
				return
			case idemWait:
				if wait != nil {
					if f := wait(opCtx); len(f) > 0 {
						_ = s.writeBytes(c, f)
						return
					}
				}
				// The wait yielded nothing: either the original failed
				// (fail() drops the key so a retry may execute) or opCtx
				// died. Re-begin once — never loop — and run the handler
				// when the ledger hands us the work.
				if _, _, again := s.idem.begin(deviceID, env.ID); again != idemExecute {
					_ = s.writeError(opCtx, c, env.ID, protocol.ErrRetryNoResult,
						"the original request completed but its response is unavailable")
					return
				}
			}
		}
```

Note the control-flow change: the `idemWait` branch now **falls through**
to the handler when the re-begin returns `idemExecute`, instead of always
returning. Verify by reading the whole function that no path reaches the
handler twice.

**14d.** Add the dispatch-level test to `internal/ws/server_test.go` (or
a new `dispatch_idem_test.go`), using the package's existing server
harness: send a mutating request whose handler blocks, send the same id
again from the same device, make the first fail, and assert the second
gets a response frame (either the re-executed op's or
`retry_no_result`) rather than nothing.

**14e.** Run and gate:

```bash
make pre-add-check FILES="internal/ws/idempotency.go internal/ws/server.go internal/protocol/errors.go"
go test ./internal/ws/ ./internal/protocol/ -count=1
go test -race ./internal/ws/ -count=1
```

Acceptance: all pass, including `TestIdempotencyPurgeEnforcesCapWithInFlightEntry`
at `-count=20`.

**14f.** Phone side — map the new code in `friendlyOpError`
(`apps/mobile/lib/data/ws/mc_exception.dart`), before the default return:

```dart
      case 'retry_no_result':
        return 'The host completed that request but could not confirm the '
            'result — pull to refresh.';
```

Phase commit (Go and Dart together, since the code is one contract).

### Step 15 — C15 + D8: bound both tombstone sets (expect RED then GREEN)

**15a — RED, phone.** Append to `apps/mobile/test/transcript_ingest_test.dart`:

```dart
  test('the cleared-session tombstone set is bounded (MADR 0095 F7)', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);
    for (var i = 0; i < kMaxClearedSessions + 64; i++) {
      n.clearSession('sess-$i');
    }
    expect(n.debugClearedCount, lessThanOrEqualTo(kMaxClearedSessions));
    // The most recent tombstones — the only ones whose ghost window is
    // still open — must survive the trim.
    expect(
      n.debugIsCleared('sess-${kMaxClearedSessions + 63}'),
      isTrue,
    );
  });
```

**15b — GREEN, phone.** In `transcripts_notifier.dart`:

```dart
/// How many end-session tombstones to retain (MADR 0095 F7). The window a
/// tombstone protects is the seconds between the delete's `ok` and the
/// next host snapshot, so the oldest entries are long past useful — but
/// every other side table in this class is pruned by `syncFromMeta` and
/// this one, by construction, never matched its prune. Mirrors
/// `maxAnsweredPerms` in internal/provider/acphttp/session.go.
const kMaxClearedSessions = 256;
```

Change the field to an insertion-ordered set and trim in `clearSession`:

```dart
  /// Sessions cleared via [clearSession] … (existing doc comment) …
  /// Bounded by [kMaxClearedSessions]; a LinkedHashSet so the trim drops
  /// the oldest tombstone, whose ghost window closed long ago.
  final Set<String> _cleared = <String>{};   // Dart's default Set is insertion-ordered
```

and in `clearSession`, after `_cleared.add(sessionId);`:

```dart
    while (_cleared.length > kMaxClearedSessions) {
      _cleared.remove(_cleared.first);
    }
```

Add the test accessor next to `debugIsCleared`:

```dart
  @visibleForTesting
  int get debugClearedCount => _cleared.length;
```

(Dart's literal `{}` set is a `LinkedHashSet`, so `first` is the oldest
insertion — state this in the comment so a future refactor to a
`HashSet` is visibly wrong.)

**15c — RED, daemon.** `purged` and `markPurged` are unexported, so this
is an **internal-package** test. Create
`internal/session/purged_bound_test.go` with `package session` (the
pattern `epoch_test.go` and `defaultmode_test.go` already use), and
construct the manager the way `newEpochManager` does —
`NewManager(provider.NewRegistry(), store, nil, func(event.Event) {})`;
a `nil` store is fine here since nothing is persisted:

```go
package session

// The purge tombstone guards a persistDebounce-sized window (2s), not the
// process lifetime; ids are UUIDs so clearPurged-on-Create never fires for
// them (MADR 0095 F7).
func TestPurgedSetIsBounded(t *testing.T) {
	m := NewManager(provider.NewRegistry(), nil, nil, func(event.Event) {})
	for i := 0; i < maxPurgedIDs+64; i++ {
		m.markPurged(fmt.Sprintf("sess-%d", i))
	}
	m.persistMu.Lock()
	n := len(m.purged)
	m.persistMu.Unlock()
	if n > maxPurgedIDs {
		t.Fatalf("purged = %d, want <= %d", n, maxPurgedIDs)
	}
	if !m.isPurged(fmt.Sprintf("sess-%d", maxPurgedIDs+63)) {
		t.Fatal("the newest tombstone must survive the trim")
	}
}
```

**15d — GREEN, daemon.** In `internal/session/manager.go`, add an order
slice beside the map (mirroring `answeredPerms`/`answeredOrder`):

```go
	// purged is ids that session.delete has removed from disk … (existing
	// comment) … Bounded by maxPurgedIDs: the window it guards is one
	// persistDebounce, and ids are UUIDs so clearPurged never fires for a
	// deleted session (MADR 0095 F7).
	purged      map[string]struct{}
	purgedOrder []string
```

```go
// maxPurgedIDs bounds the delete tombstone set (see [Manager.purged]).
const maxPurgedIDs = 256

func (m *Manager) markPurged(id string) {
	m.persistMu.Lock()
	if m.purged == nil {
		m.purged = make(map[string]struct{})
	}
	if _, dup := m.purged[id]; !dup {
		m.purged[id] = struct{}{}
		m.purgedOrder = append(m.purgedOrder, id)
		for len(m.purgedOrder) > maxPurgedIDs {
			delete(m.purged, m.purgedOrder[0])
			m.purgedOrder = m.purgedOrder[1:]
		}
	}
	m.persistMu.Unlock()
}

func (m *Manager) clearPurged(id string) {
	m.persistMu.Lock()
	if _, ok := m.purged[id]; ok {
		delete(m.purged, id)
		for i, v := range m.purgedOrder {
			if v == id {
				m.purgedOrder = append(m.purgedOrder[:i], m.purgedOrder[i+1:]...)
				break
			}
		}
	}
	m.persistMu.Unlock()
}

// isPurged reports whether id carries a delete tombstone. Test seam and
// the single read path for writePersist.
func (m *Manager) isPurged(id string) bool {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()
	_, ok := m.purged[id]
	return ok
}
```

Replace both inline `m.purged[meta.ID]` reads in `writePersist` with
`m.isPurged(meta.ID)`, removing their surrounding `persistMu` lock/unlock
pairs (the accessor takes the lock). Re-read `writePersist` after the
edit to confirm no double-lock remains — `persistMu` is not reentrant.

**15e.** Run:

```bash
make pre-add-check FILES="internal/session/manager.go"
go test ./internal/session/ -count=1 && go test -race ./internal/session/ -count=1
cd apps/mobile && flutter test test/transcript_ingest_test.dart \
  test/chat_end_session_navigation_test.dart test/staged_images_test.dart
```

Acceptance: all pass. `staged_images_test.dart` is included because
`d2ba813` pinned D8 semantics there.

Phase commit.

### Step 16 — D9: make the service setup test platform-honest (F8)

In `internal/cli/service/setup_test.go`, add to
`TestSetupWritesDefaultMcrelayConfig` as the first two statements —
matching `TestSetupIdempotentRerun`'s existing pattern and comment:

```go
func TestSetupWritesDefaultMcrelayConfig(t *testing.T) {
	// Asserts systemd unit directives, so pin the install OS rather than
	// inheriting the host's: on macOS Setup correctly writes a launchd
	// plist and these assertions read XML. Stub the service manager too —
	// this test is about unit-file content, and without the stub it drove
	// real launchctl on the developer's machine (MADR 0095 F8).
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideRunSystemctl(func(...string) error { return nil })()
	dir := t.TempDir()
```

Run:

```bash
go test ./internal/cli/service/ -count=1 -v
go test ./... 2>&1 | grep -E '^(---)? ?FAIL' ; echo "no-failures-exit=$?"
```

Acceptance: the package passes on darwin; the module-wide grep finds no
`FAIL` line (grep exits 1). **Probe already performed 2026-08-16** — this
exact edit turned the failure into a pass on this machine; the edit was
reverted after measuring, so it is expected to reproduce.

Phase commit.

### Step 17 — C17 red: the shared timeout table (F9)

**17a.** Create `internal/protocol/op_timeouts.json` — the single source
of truth for every method that reaches `dispatchAsync`. Values are the
daemon's allowance in milliseconds:

```json
{
  "_comment": "Server-side deadline per async-dispatched method (MADR 0095 D7). internal/ws/asyncOpTimeout must match exactly; the phone's per-method request timeout must strictly exceed each value. Methods handled inline on the read loop are absent by design.",
  "default_ms": 30000,
  "client_margin_ms": 10000,
  "methods": {
    "session.create": 120000,
    "session.prompt": 60000,
    "session.delete": 60000,
    "session.close": 60000,
    "session.fork": 60000,
    "session.history": 30000,
    "session.release": 30000,
    "session.claim": 30000,
    "session.set_collaboration_mode": 30000,
    "session.revert": 30000,
    "session.unrevert": 30000,
    "session.diff": 30000,
    "session.rename": 30000,
    "session.diagnostics": 30000,
    "providers.list": 30000,
    "providers.set_prewarm": 30000,
    "provider.auth_catalog": 30000,
    "provider.set_credential": 30000,
    "provider.clear_credential": 30000,
    "provider.set_active_upstream": 30000,
    "provider.start_auth": 30000,
    "models.list": 60000,
    "agents.list": 60000,
    "agent_sessions.list": 60000,
    "commands.list": 30000
  }
}
```

`session.delete`, `session.close` and `session.fork` move from the 30 s
default to 60 s: each tears down or forks a provider subprocess, and the
opencode/kilo purge alone budgets 15 s for the engine-side delete *after*
local teardown (`internal/provider/httpagent/session.go` `Purge`).

**17b.** `internal/ws/op_timeout_test.go` (new) — the daemon half:

```go
// asyncOpTimeout is the daemon's half of the timeout ladder (MADR 0095
// D7). The table is shared with the phone, which must exceed every value.
func TestAsyncOpTimeoutMatchesSharedTable(t *testing.T) {
	tbl := loadOpTimeouts(t) // reads ../protocol/op_timeouts.json
	for method, wantMS := range tbl.Methods {
		if got := asyncOpTimeout(method); got != time.Duration(wantMS)*time.Millisecond {
			t.Errorf("asyncOpTimeout(%q) = %v, table says %dms", method, got, wantMS)
		}
	}
	if got := asyncOpTimeout("no.such.method"); got != time.Duration(tbl.DefaultMS)*time.Millisecond {
		t.Errorf("default = %v, table says %dms", got, tbl.DefaultMS)
	}
}

// Every method that reaches dispatchAsync must appear in the table, so a
// new async op cannot silently inherit a deadline the phone races.
func TestEveryAsyncDispatchedMethodIsInTheTable(t *testing.T) {
	tbl := loadOpTimeouts(t)
	for _, m := range asyncDispatchedTypes() {
		if _, ok := tbl.Methods[m]; !ok {
			t.Errorf("%q reaches dispatchAsync but is absent from op_timeouts.json", m)
		}
	}
}
```

`asyncDispatchedTypes()` is a new **test-only** helper in
`internal/ws/export_test.go` returning the 25 constants listed in the
anchors section. It is a hand-maintained list; the second test's job is
to make forgetting the JSON entry loud, and a reviewer adding a
`dispatchAsync` case must add it here too. State that in its doc comment.

**17c.** `apps/mobile/test/op_timeout_ladder_test.dart` (new) — the phone
half. Reads the same file (`flutter test` runs from `apps/mobile`, so the
path is `../../internal/protocol/op_timeouts.json`; use
`p.join` / `File(...).existsSync()` and `fail()` with the resolved path if
missing, so a moved file reports clearly rather than as a null error):

```dart
  test('every client request timeout exceeds the daemon deadline', () {
    final tbl = _loadOpTimeouts();
    final margin = Duration(milliseconds: tbl.clientMarginMs);
    for (final entry in tbl.methods.entries) {
      final daemon = Duration(milliseconds: entry.value);
      expect(
        opTimeoutFor(entry.key),
        daemon + margin,
        reason:
            '${entry.key}: the phone must not race the daemon\'s own '
            'deadline — the authoritative failure is its error frame',
      );
    }
    expect(
      opTimeoutFor('no.such.method'),
      Duration(milliseconds: tbl.defaultMs) + margin,
    );
  });
```

**17d.** Run both:

```bash
go test ./internal/ws/ -run 'OpTimeout|AsyncDispatched' -count=1
cd apps/mobile && flutter test test/op_timeout_ladder_test.dart
```

Acceptance — both **fail**, with these signatures:

* Go: `asyncOpTimeout("session.delete") = 30s, table says 60000ms`
  (and the same for `session.close`, `session.fork`).
* Dart: compile error — `opTimeoutFor` does not exist yet. That is the
  expected RED for the phone half; do not stub it to make the test
  compile.

Phase commit (test + table only).

### Step 18 — D7: implement the ladder (expect GREEN)

**18a — daemon.** In `internal/ws/server.go`, extend `asyncOpTimeout`:

```go
func asyncOpTimeout(typ string) time.Duration {
	switch typ {
	case protocol.TypeSessionCreate:
		return 120 * time.Second
	case protocol.TypeSessionPrompt:
		return 60 * time.Second
	case protocol.TypeSessionDelete, protocol.TypeSessionClose,
		protocol.TypeSessionFork:
		// Lifecycle ops tear down or fork a provider subprocess. The
		// opencode/kilo purge alone budgets 15s for the engine-side
		// delete after local teardown; 30s left the phone's own timeout
		// expiring in the same instant (MADR 0095 D7/F9).
		return 60 * time.Second
	case protocol.TypeSessionHistory:
		return 30 * time.Second
	case protocol.TypeModelsList, protocol.TypeAgentsList, protocol.TypeAgentSessionsList:
		return 60 * time.Second
	default:
		return 30 * time.Second
	}
}
```

Add a pointer comment above it: the values are mirrored in
`internal/protocol/op_timeouts.json` and pinned by
`TestAsyncOpTimeoutMatchesSharedTable`.

**18b — phone.** In `apps/mobile/lib/data/ws/mcremote_client.dart`, add
the resolver next to `request`:

```dart
/// Per-method request deadline, mirroring the daemon's `asyncOpTimeout`
/// plus a margin (MADR 0095 D7).
///
/// The daemon's allowance is authoritative for how long an operation may
/// take; the phone's timeout is a backstop for a dead link, not a
/// competing deadline. Where the two were equal the daemon's own
/// `deadline_exceeded` frame was pre-empted by a client timeout and an
/// idempotent retry; where the client was shorter (models.list and
/// friends at 30s against the daemon's 60s) a successful late reply was
/// discarded and the op always surfaced as a failure.
///
/// Values are pinned against `internal/protocol/op_timeouts.json` by
/// `test/op_timeout_ladder_test.dart`.
const Duration kOpTimeoutMargin = Duration(seconds: 10);

Duration opTimeoutFor(String type) {
  switch (type) {
    case 'session.create':
      return const Duration(seconds: 120) + kOpTimeoutMargin;
    case 'session.prompt':
    case 'session.delete':
    case 'session.close':
    case 'session.fork':
    case 'models.list':
    case 'agents.list':
    case 'agent_sessions.list':
      return const Duration(seconds: 60) + kOpTimeoutMargin;
    default:
      return const Duration(seconds: 30) + kOpTimeoutMargin;
  }
}
```

Change `request`'s parameter and resolution:

```dart
    /// Null resolves to [opTimeoutFor] — pass a value only for methods
    /// the daemon handles inline on its read loop (pair.claim,
    /// permission.receipt), which are not on the async ladder.
    Duration? timeout,
    …
  }) async {
    …
    final effectiveTimeout = timeout ?? opTimeoutFor(type);
```

and use `effectiveTimeout` in `completer.future.timeout(...)` **and** in
the recursive idempotent-retry call (`timeout: effectiveTimeout`) — the
retry must not fall back to the default a second time.

**18c.** Remove the now-redundant explicit timeouts at the
`session.create` (120 s) and `session.prompt` (60 s) call sites so both
inherit from `opTimeoutFor`. Leave `pair.claim` (20 s) and
`permission.receipt` (10 s) exactly as they are.

**18d.** Run:

```bash
go test ./internal/ws/ -count=1
cd apps/mobile && flutter test test/op_timeout_ladder_test.dart \
  test/mcremote_client_test.dart test/resume_flow_test.dart \
  test/dial_episode_test.dart test/link_liveness_test.dart
```

Acceptance: all pass. Watch specifically for client tests that assert a
30 s timeout or use `FakeAsync` to elapse exactly 30 s — those need their
elapsed durations updated to 40 s, and each such change must be
accompanied by a comment naming this MADR, so the next reader sees why.

Phase commit.

### Step 19 — C18 + D10: capability-gated goose purge (F10)

**19a.** In `internal/provider/acphttp/session.go`, add next to `Close`:

```go
// Purge removes the agent-native session as well as the local one.
// Implements [provider.PurgeSession] for session.delete (MADR 0095 D10).
//
// Local teardown first, mirroring httpagent: Close stops the engine pump
// routing into this session before the delete round-trip. The delete is
// capability-gated — `session/delete` is UNSTABLE in acp-go-sdk v0.13.5
// ("may be removed or changed at any point"), so an agent that does not
// advertise it degrades to exactly the previous behaviour rather than
// erroring the user's End action.
func (s *session) Purge(ctx context.Context) error {
	_ = s.Close(ctx)
	if s.p.caps().SessionCapabilities.Delete == nil {
		// Agent keeps its own session store; the daemon's record is gone
		// either way. Logged so an operator can see why the session is
		// still in `agent_sessions.list`.
		s.log.Info("agent does not advertise session/delete; "+
			"native session retained",
			slog.String("agent_session_id", s.agentID))
		return nil
	}
	fr, err := s.p.framer()
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_, err = fr.sendRequest(callCtx, "session/delete", acp.UnstableDeleteSessionRequest{
		SessionId: acp.SessionId(s.agentID),
	})
	return err
}

var _ provider.PurgeSession = (*session)(nil)
```

Verify the `acp` and `slog` imports are present in the file (both already
are) and that `s.log` is non-nil on every construction path.

**19b.** Unit test in `internal/provider/acphttp/session_test.go`: with
the provider's `agentCaps` left zero-valued, `Purge` must call `Close`
and send **no** `session/delete`; with
`SessionCapabilities.Delete = &acp.SessionDeleteCapabilities{}`, it must
send exactly one, carrying `s.agentID`. Use the package's existing fake
framer/engine harness (`conn_test.go`, `session_test.go`).

**19c.** Live pin, per `AGENTS.md` ("when a decision rests on how an
external CLI behaves, record the probe evidence in the MADR and pin it
with a live-tagged test"). New file
`internal/provider/acphttp/live_purge_test.go`:

```go
//go:build live_goose
```

It must: start a real goose session, assert it appears in
`ListAgentSessions`, call `Purge`, and assert it is gone. It must also
log goose's advertised `SessionCapabilities.Delete` — if goose does not
advertise it, **F10 degrades to a documented limitation**: keep the code
(it is correct and inert), record the observed capability set in MADR
0095's "More Information", and say so in the Consequences instead of
claiming the behaviour changed.

**19d.** Add the Makefile target next to `live-opencode`, and add it to
the `.PHONY` line:

```make
live-goose:
	go test -tags live_goose ./internal/provider/acphttp/ -count=1 -timeout 600s -v
```

**19e.** Run:

```bash
make pre-add-check FILES="internal/provider/acphttp/session.go"
go test ./internal/provider/acphttp/ -count=1
make live-goose   # requires goose on PATH; spends real tokens — run once, at acceptance
```

Acceptance: unit tests pass; the live test either passes or reports "no
`session/delete` capability", which routes to the documented-limitation
outcome above.

Phase commit.

### Step 20 — F11: constant-time resume-token comparison

In `internal/ws/resume.go` `validate`, replace `e.token == token`:

```go
	// Constant-time, matching every other secret comparison in the tree
	// (auth/store.go, relay/hub.go). Not reachable pre-auth — resume
	// validation runs only after the device token authenticated — but the
	// house rule should not have one exception (MADR 0095 F11).
	return ok && subtle.ConstantTimeCompare([]byte(e.token), []byte(token)) == 1 &&
		r.now().Before(e.expires)
```

Add `"crypto/subtle"` to the imports. Add a case to
`internal/ws/resume_store_test.go` asserting a wrong-but-same-length
token is rejected (the existing suite may only cover a different-length
one, which `ConstantTimeCompare` rejects on length alone).

Run `go test ./internal/ws/ -count=1`. Phase commit.

### Step 21 — Full gate

```bash
make preflight
```

Acceptance: **green end to end, for the first time on this machine** —
gofmt, tidy, vet, staticcheck, `go test -race ./...`, script tests, unit
verification, release build, `dart format`, `flutter analyze`,
`flutter test`. If `staticcheck` flags the new code, fix it here rather
than at review.

If `verify-units` is skipped for want of `systemd-analyze` (expected on
macOS), that is the pre-existing conditional skip, not a failure.

### Step 22 — Device confirmation (C5 extended)

Build and install: `make apk`, install on `s22+`. Repeat 0094's P1–P3,
then the three new scenarios:

1. **P6 (F3 / D4).** Start a session, End it, and while the confirm is
   processing open the overflow menu and tap **Session diagnostics**.
   Expected: when the delete lands, the dialog is retired and the
   sessions landing screen appears with the post-delete list — no back
   press. Under D4b instead: the menu item is absent while the delete is
   in flight.
2. **P7 (F3 / D4).** Same, but tap **Fork session** and confirm.
   Expected: the landing screen, not the forked chat sitting above a dead
   one. (The forked session itself remains — the fork succeeded.)
3. **P8 (F2 / D3).** With the host unreachable, send a prompt so the
   "Send failed — [Sessions]" toast appears; press back, then tap
   **Sessions** on the toast. Expected: the landing screen, populated. Do
   it again and let the exit transition finish first: the action must
   still navigate.

F1, F4, F5–F11 have no device-observable scenario that can be forced
deterministically; their authoritative confirmation is C9, C12, C13/C14,
C15, C16, C17 and C18.

### Step 23 — Accept the MADR

Set MADR 0095 frontmatter `status: accepted` and `date` to the acceptance
date. Fill in the D4 probe subsection if Step 10a left anything open, and
add an "Implementation state" table to "More Information" naming the
phase commits per finding — matching 0094's closing section.

If any finding was **not** remediated (most likely F10 degrading to a
documented limitation), say so explicitly in that table with the reason.
Do not mark the MADR accepted while a finding is silently unaddressed.

### Step 24 — Commit and push

```bash
git status --short           # expect only the docs to remain
git add docs/0095-MADR-post-0094-assessment-and-debug-pass.md \
  docs/0095-PLAN-post-0094-assessment-and-debug-pass.md
git commit --no-edit
```

Push only after the user approves, per the 0094 convention:

```bash
git push origin master
```

Acceptance: CI green on the pushed tip.

## Verification

| ID | What it proves | How | Step |
| --- | --- | --- | --- |
| C9 | An incomplete snapshot never confirms a purge (F1) | nav test, red on baseline | 2 → 3 |
| C10 | The root-overlay toast action cannot empty the navigator, and still works after the chat is gone (F2) | two nav tests, both red | 4 → 5 |
| C11 | An untracked dialog and a pushed forked chat both still land (F3) | two nav tests, red | 7 → 8 |
| C12 | The sessions list classifies a lost `ok` and cleans local state (F4) | two list tests, first red | 11 → 12 |
| C13 | A retry never gets silence from the ledger (F5) | ledger + dispatch tests | 13 → 14 |
| C14 | `maxEntries` holds under randomized map order (F6) | ledger test, red ×20 | 13 → 14 |
| C15 | Both tombstone sets are bounded and still protect their window (F7) | Dart + Go tests, red | 15 |
| C16 | The service test passes on macOS with no `launchctl` (F8) | `go test ./...` clean; probe already performed | 16 |
| C17 | Every client timeout exceeds the daemon's allowance (F9) | shared table + Go and Dart tests, both red | 17 → 18 |
| C18 | An ended goose session leaves `agent_sessions.list` (F10) | unit test + `make live-goose` | 19 |
| C19 | No regressions anywhere | `make preflight` green | 21 |
| C5′ | Device behaviour for P1–P3, P6–P8 | manual, Step 22 | 22 |

Determinism notes:

* Every RED step above names its exact expected failing assertion. A
  different failure mode is new evidence — stop and re-examine, do not
  adapt the test to whatever it does.
* C14 is order-dependent **by nature of the bug**; it is run at
  `-count=20` on the red side so a lucky pass cannot be mistaken for
  correct behaviour. After the fix it must pass at `-count=20` too.
* The D4 mechanism is chosen by a table in Step 6c, not by judgement, and
  the probe is reverted before any implementation lands.
* No step introduces a timer, a post-frame callback, a retry loop or a
  `sleep` that gates correctness. The `pump(Duration(milliseconds: 100))`
  calls only position a probe inside an exit transition, exactly as
  0094's C1 does; the 20 ms sleep in the ledger test is inherited from
  the file's existing `TestIdempotencyWaitForInFlight`.
* The shared `op_timeouts.json` is read by both test suites, so the
  ladder cannot drift in one language without failing in the other. Both
  suites also fail if a new `dispatchAsync` case is added without a table
  entry.
* `session.delete`'s daemon allowance moves 30 s → 60 s; the client's
  moves 30 s → 70 s. No existing timeout shrinks anywhere in this plan.

## Rollout and Rollback

**Rollout.** Three rounds, each a set of phase commits that can land
independently and in order A → B → C. No daemon/phone version coupling is
introduced:

* Round A and B are phone-only; an old daemon is unaffected.
* Round C's `retry_no_result` is a new error **code**, not a new message
  type. A phone that predates Step 14f renders the daemon's raw message
  through `friendlyOpError`'s fallback, which is legible.
* Round C's timeout changes are per-side and independent: a new daemon
  with an old phone is the situation today with a *longer* server
  allowance (the old phone still times out at 30 s and retries
  idempotently — no worse than now); a new phone with an old daemon
  simply waits longer than the old daemon needs.
* D10 changes provider-side state destruction for goose. Call it out in
  the release note: **ending a goose session now deletes the goose-side
  session too**, where before it survived in `agent_sessions.list`.
* Installed daemons need no action; there is no migration. Both tombstone
  bounds are in-memory and die with the process.

**Rollback.** By round, newest first:

* Round C: `git revert` the Step 13–20 commits. Restores the 30 s
  ladder, the silent ledger paths, the unbounded sets, the red macOS
  test and goose's surviving native sessions. `op_timeouts.json` and its
  two tests disappear together, so nothing is left asserting a table that
  no longer exists.
* Round B: revert Step 11–12. The sessions list returns to showing "End
  failed" for a lost `ok`.
* Round A: revert Step 2–10. Returns to `b0e7261` behaviour — 0094 D1–D8
  intact, F1/F2/F3 reachable again.

Reverting Round A alone while keeping B is safe (they touch different
screens); reverting Step 14 while keeping Step 14f is also safe (an
unused `friendlyOpError` case). The only ordering constraint is that
Step 18's client change must not outlive Step 17's table — revert them
together.
