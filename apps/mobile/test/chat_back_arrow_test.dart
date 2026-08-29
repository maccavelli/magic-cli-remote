import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// MADR 0123 P8, acceptance A1/A2 — the defect this record was opened for.
///
/// The app bar carried six actions, three of them variable-width *text* chips
/// whose width tracked the selected value. On codex, whose modes include
/// "full access" (eleven characters), the actions run squeezed `leading` and
/// the back arrow was hidden. The controls now live under the composer as
/// fixed-width icons, so nothing in `AppBar.actions` grows with a selection.
///
/// This iterates every codex mode value rather than a representative one:
/// the failure was value-dependent, so a single-value test would have passed
/// throughout the bug's life.

/// The width the defect was reported on, matching composer_layout_test.dart.
const Size _phone = Size(360, 800);

/// Codex's permission catalog (internal/provider/codex/mode.go), including the
/// longest label, which is the one that broke the layout.
const _codexModes = [
  SessionMode(id: 'default', name: 'default'),
  SessionMode(id: 'read-only', name: 'read-only'),
  SessionMode(id: 'auto', name: 'auto', dangerous: true),
  SessionMode(id: 'full-access', name: 'full access', dangerous: true),
];

const _collab = [
  CollaborationMode(id: 'default', name: 'Default'),
  CollaborationMode(id: 'plan', name: 'Plan'),
];

class _CodexClient extends McremoteClient {
  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [
          SessionMeta(
            id: 's1',
            provider: 'codex',
            cwd: '/home/mac/some/deep/working/directory',
            thinkingLevel: 'medium',
          ),
        ],
        complete: true,
      );
}

Widget _host(String currentModeId) {
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: const [],
    modes: _codexModes,
    currentModeId: currentModeId,
    collaborationModes: _collab,
    currentCollaborationModeId: 'plan',
    remoteCommands: [
      RemoteCommand(name: 'thinking', description: 't', available: true),
    ],
  );
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(_CodexClient()),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWithValue(transcript),
    ],
    // A route below something else, so Flutter implies a back button.
    child: MaterialApp(
      home: const Scaffold(body: Text('sessions')),
      routes: {'/chat': (_) => const ChatScreen(sessionId: 's1')},
      builder: (context, child) => child!,
    ),
  );
}

void main() {
  for (final mode in _codexModes) {
    testWidgets('the back arrow stays reachable in codex mode "${mode.name}"', (
      tester,
    ) async {
      tester.view.physicalSize = _phone;
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(_host(mode.id));
      await tester.pumpAndSettle();

      final navigator = tester.state<NavigatorState>(find.byType(Navigator));
      navigator.pushNamed('/chat');
      await tester.pumpAndSettle();

      final back = find.byType(BackButton);
      expect(
        back,
        findsOneWidget,
        reason: 'mode "${mode.name}" must not push the back arrow out',
      );

      // Present is not the same as reachable: assert it has real size and sits
      // inside the surface, which is what the overflow actually destroyed.
      final rect = tester.getRect(back);
      expect(rect.width, greaterThan(0));
      expect(rect.left, greaterThanOrEqualTo(0));
      expect(rect.right, lessThanOrEqualTo(_phone.width));
      expect(
        tester.takeException(),
        isNull,
        reason: 'a RenderFlex overflow throws rather than merely looking wrong',
      );
    });
  }

  testWidgets('no app-bar action grows with the selected value', (
    tester,
  ) async {
    // A2. The regression is structural, not cosmetic: if a widget whose width
    // tracks a selection ever returns to AppBar.actions, the back arrow will
    // be squeezed again by some value nobody tested.
    tester.view.physicalSize = _phone;
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    Future<double> actionsWidthFor(String modeId) async {
      await tester.pumpWidget(_host(modeId));
      await tester.pumpAndSettle();
      final navigator = tester.state<NavigatorState>(find.byType(Navigator));
      navigator.pushNamed('/chat');
      await tester.pumpAndSettle();
      final title = find.byType(AppBar);
      return tester.getSize(title).width;
    }

    final short = await actionsWidthFor('default');
    final long = await actionsWidthFor('full-access');
    expect(
      short,
      long,
      reason: 'the app bar must lay out identically whatever the mode',
    );
  });
}
