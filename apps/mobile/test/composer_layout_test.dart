import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/features/chat/diagnostics_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Composer layout (MADR 0112 amendment 2026-08-25, P12).
///
/// Every action affordance used to share one [Row] with the prompt field. Six
/// 48dp buttons plus the send button overran the content box of a 360dp phone,
/// so the [Expanded] field collapsed to nothing. The actions now sit on their
/// own row underneath, and the engine-diagnostics sheet moved to the session
/// menu rather than being deleted — it is the only route to skills refresh and
/// the skill-authoring composer.

/// A 360dp-wide logical surface: the width the defect was reported on.
const Size _phone = Size(360, 800);

class _StubClient extends McremoteClient {
  @override
  Future<SessionDiagnostics> sessionDiagnostics(String sessionId) async =>
      SessionDiagnostics(
        branch: 'main',
        skills: const [SkillInfo(name: 'customize-opencode')],
      );

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'opencode', cwd: '/w')],
        complete: true,
      );
}

/// Pumps the chat screen at [_phone] with every composer capability set to
/// [on], so one helper covers both the crowded and the empty action row.
Future<void> _pumpComposer(WidgetTester tester, {required bool on}) async {
  tester.view.physicalSize = _phone;
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: const [],
    nextSeq: 0,
    capabilities: SessionCapabilities(
      image: on,
      audio: on,
      workspaceRead: on,
      skillRefresh: on,
      shareState: on,
      share: on,
      shell: on,
      loadSession: false,
      embeddedContext: false,
      listSessions: false,
      closeSession: false,
      mcpHttp: false,
      mcpSse: false,
      mcpAcp: false,
    ),
  );
  final container = ProviderContainer(
    overrides: [
      mcremoteClientProvider.overrideWithValue(_StubClient()),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWithValue(transcript),
    ],
  );
  addTearDown(container.dispose);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
    ),
  );
  await tester.pumpAndSettle();
}

/// The composer's own field, identified by its hint so an unrelated [TextField]
/// elsewhere on the screen can never satisfy the size assertion.
final Finder _promptField = find.byWidgetPredicate(
  (w) =>
      w is TextField &&
      (w.decoration?.hintText == 'Prompt or /command…' ||
          w.decoration?.hintText == 'Queue a message' ||
          w.decoration?.hintText == 'Disconnected'),
);

void main() {
  testWidgets('every action affordance sits on the row below the prompt', (
    tester,
  ) async {
    await _pumpComposer(tester, on: true);

    final actions = find.byKey(const ValueKey('composer-actions'));
    expect(actions, findsOneWidget);

    for (final key in const [
      'attach-audio',
      'open-workspace',
      'open-shell',
      'open-share',
    ]) {
      expect(
        find.descendant(of: actions, matching: find.byKey(ValueKey(key))),
        findsOneWidget,
        reason: '$key belongs on the action row, not beside the field',
      );
    }
    expect(
      find.descendant(of: actions, matching: find.byTooltip('Attach image')),
      findsOneWidget,
    );

    // The send button is the primary action and deliberately stays on the
    // input row (amendment D2), so it must NOT have moved down with the rest.
    expect(
      find.descendant(
        of: actions,
        matching: find.byKey(const ValueKey('send')),
      ),
      findsNothing,
    );
  });

  testWidgets('the composer no longer carries a diagnostics icon', (
    tester,
  ) async {
    await _pumpComposer(tester, on: true);
    expect(find.byKey(const ValueKey('open-diagnostics')), findsNothing);
    expect(find.byTooltip('Diagnostics'), findsNothing);
  });

  testWidgets('the prompt field keeps most of the width with every icon on', (
    tester,
  ) async {
    await _pumpComposer(tester, on: true);

    expect(_promptField, findsOneWidget);
    final width = tester.getSize(_promptField).width;
    expect(
      width,
      greaterThan(_phone.width / 2),
      reason:
          'the field collapsed to ${width.toStringAsFixed(1)}dp — the icons '
          'are crowding it again',
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('the session menu opens the engine diagnostics sheet', (
    tester,
  ) async {
    await _pumpComposer(tester, on: true);

    await tester.tap(find.byTooltip('Session actions'));
    await tester.pumpAndSettle();

    // Named apart from the repository/MCP dialog so the two are tellable.
    expect(find.text('Engine diagnostics & skills'), findsOneWidget);
    expect(find.text('Session diagnostics'), findsNothing);

    await tester.tap(find.text('Engine diagnostics & skills'));
    await tester.pumpAndSettle();

    expect(find.byType(DiagnosticsSheet), findsOneWidget);

    // Both skills buttons render here (skillRefresh on, authoring wired). The
    // section header used to lay them out in a Row and overflowed by 298px at
    // this width; it wraps now, so the sheet is usable on a phone.
    expect(
      find.byKey(const ValueKey('diagnostics-refresh-skills')),
      findsOneWidget,
    );
    expect(
      find.byKey(const ValueKey('diagnostics-author-skill')),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('the action row renders empty without overflowing', (
    tester,
  ) async {
    await _pumpComposer(tester, on: false);

    final actions = find.byKey(const ValueKey('composer-actions'));
    expect(actions, findsOneWidget);
    expect(
      find.descendant(of: actions, matching: find.byType(IconButton)),
      findsNothing,
      reason: 'no capability is advertised, so no action belongs on the row',
    );
    expect(_promptField, findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
