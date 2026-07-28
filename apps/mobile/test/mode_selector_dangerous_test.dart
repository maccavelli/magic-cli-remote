import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Records setMode calls so a test can prove the confirmation gate actually
/// blocks the switch rather than merely showing a dialog.
class _ModeClient extends McremoteClient {
  final List<String> modeSwitches = [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<List<SessionMeta>> listSessions() async => [
    SessionMeta(id: 's1', provider: 'opencode', cwd: '/home/mac'),
  ];

  @override
  Future<void> setMode(String sessionId, String modeId) async {
    modeSwitches.add(modeId);
  }
}

/// OpenCode's advertised list: build/plan plus the daemon-flagged auto mode.
const _opencodeModes = [
  SessionMode(id: 'build', name: 'build'),
  SessionMode(id: 'plan', name: 'plan'),
  SessionMode(
    id: 'auto',
    name: 'auto',
    description:
        'Auto-approve — permissions answered automatically (dangerous)',
    dangerous: true,
  ),
];

/// Goose's advertised list: `auto` is its *default* and carries no flag.
const _gooseModes = [
  SessionMode(
    id: 'auto',
    name: 'Auto',
    description: 'Automatically approve tool calls',
  ),
  SessionMode(id: 'approve', name: 'Approve'),
  SessionMode(id: 'smart_approve', name: 'Smart Approve'),
  SessionMode(id: 'chat', name: 'Chat'),
];

Widget _host(
  _ModeClient client, {
  required List<SessionMode> modes,
  required String currentModeId,
}) {
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: const [],
    modes: modes,
    currentModeId: currentModeId,
  );
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWithValue(transcript),
    ],
    child: MaterialApp(home: ChatScreen(sessionId: 's1')),
  );
}

void main() {
  group('dangerous mode chip', () {
    testWidgets('an armed dangerous mode is visually alarming', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _opencodeModes, currentModeId: 'auto'),
      );
      await tester.pumpAndSettle();

      expect(
        find.byIcon(Icons.bolt),
        findsWidgets,
        reason: 'an armed auto-approve mode must be unmissable in the app bar',
      );
    });

    testWidgets('a normal mode is not styled as dangerous', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.bolt), findsNothing);
    });

    // The regression guard for goose: it has shipped `auto` as its default for
    // a while and sends no flag, so nothing about its chip may change.
    testWidgets('a goose session in auto renders a plain chip', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _gooseModes, currentModeId: 'auto'),
      );
      await tester.pumpAndSettle();

      expect(
        find.byIcon(Icons.bolt),
        findsNothing,
        reason:
            'goose auto-approve is its normal state and predates this '
            'feature; alarming on it would be a regression',
      );
    });
  });

  group('arming confirmation', () {
    // The app bar holds more than one PopupMenuButton<String> (the mode
    // switcher and the session-actions menu), so target the one wrapping the
    // current mode's chip label.
    Future<void> openMenu(WidgetTester tester, String currentLabel) async {
      await tester.tap(
        find.ancestor(
          of: find.text(currentLabel),
          matching: find.byType(PopupMenuButton<String>),
        ),
      );
      await tester.pumpAndSettle();
    }

    testWidgets('selecting a dangerous mode asks first', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.widgetWithText(CheckedPopupMenuItem<String>, 'auto'),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsOneWidget);
      expect(
        client.modeSwitches,
        isEmpty,
        reason: 'the switch must not happen until the user confirms',
      );
    });

    testWidgets('cancelling does not switch', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.widgetWithText(CheckedPopupMenuItem<String>, 'auto'),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(client.modeSwitches, isEmpty);
    });

    testWidgets('confirming switches', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.widgetWithText(CheckedPopupMenuItem<String>, 'auto'),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Turn on'));
      await tester.pumpAndSettle();

      expect(client.modeSwitches, ['auto']);
    });

    testWidgets('a normal mode switches without a dialog', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.widgetWithText(CheckedPopupMenuItem<String>, 'plan'),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsNothing);
      expect(client.modeSwitches, ['plan']);
    });

    // Goose sends no flag, so switching back to its default must stay one tap.
    testWidgets('a goose auto switch is not gated', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _gooseModes, currentModeId: 'chat'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'Chat');
      await tester.tap(
        find.widgetWithText(CheckedPopupMenuItem<String>, 'Auto'),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsNothing);
      expect(client.modeSwitches, ['auto']);
    });
  });
}
