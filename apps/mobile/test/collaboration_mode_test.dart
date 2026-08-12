import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

class _CollabClient extends McremoteClient {
  final List<String> collabSwitches = [];
  final List<String> modeSwitches = [];
  final String provider;

  _CollabClient({this.provider = 'codex'});

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: provider, cwd: '/tmp')],
        complete: true,
      );

  @override
  Future<void> setMode(String sessionId, String modeId) async {
    modeSwitches.add(modeId);
  }

  @override
  Future<void> setCollaborationMode(String sessionId, String modeId) async {
    collabSwitches.add(modeId);
  }
}

const _collab = [
  CollaborationMode(id: 'plan', name: 'Plan'),
  CollaborationMode(id: 'default', name: 'Default'),
];

const _codexModes = [
  SessionMode(id: 'default', name: 'default'),
  SessionMode(id: 'auto', name: 'auto', dangerous: true),
];

Widget _host(
  _CollabClient client, {
  List<CollaborationMode> collaborationModes = const [],
  String? currentCollaborationModeId,
  List<SessionMode> modes = _codexModes,
  String? currentModeId = 'default',
  String status = 'idle',
}) {
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: status,
    modes: modes,
    currentModeId: currentModeId,
    collaborationModes: collaborationModes,
    currentCollaborationModeId: currentCollaborationModeId,
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
  test('CollaborationMode JSON and equality', () {
    final m = CollaborationMode.fromJson({
      'id': 'plan',
      'name': 'Plan',
      'description': 'Plan only',
    });
    expect(
      m,
      const CollaborationMode(
        id: 'plan',
        name: 'Plan',
        description: 'Plan only',
      ),
    );
    expect(
      m,
      isNot(
        const SessionMode(id: 'plan', name: 'Plan', description: 'Plan only'),
      ),
    );
  });

  testWidgets('Plan control is hidden without a collaboration catalog', (
    tester,
  ) async {
    await tester.pumpWidget(_host(_CollabClient()));
    await tester.pump();
    expect(find.byTooltip('Plan / Default'), findsNothing);
    expect(find.byTooltip('Permissions'), findsOneWidget);
  });

  testWidgets('Plan and Permissions are independent controls', (tester) async {
    final client = _CollabClient();
    await tester.pumpWidget(
      _host(
        client,
        collaborationModes: _collab,
        currentCollaborationModeId: 'default',
      ),
    );
    await tester.pump();
    expect(find.byTooltip('Plan / Default'), findsOneWidget);
    expect(find.byTooltip('Permissions'), findsOneWidget);

    await tester.tap(find.byTooltip('Plan / Default'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(CheckedPopupMenuItem<String>, 'Plan'));
    await tester.pumpAndSettle();
    expect(client.collabSwitches, ['plan']);
    expect(client.modeSwitches, isEmpty);
  });

  testWidgets('Plan is disabled while the session is running', (tester) async {
    final client = _CollabClient();
    await tester.pumpWidget(
      _host(
        client,
        collaborationModes: _collab,
        currentCollaborationModeId: 'default',
        status: 'running',
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('Plan / Default'));
    await tester.pumpAndSettle();
    expect(find.text('Plan'), findsNothing);
    expect(client.collabSwitches, isEmpty);
  });

  testWidgets('Plan change failure keeps the previous label', (tester) async {
    final client = _FailingCollabClient();
    await tester.pumpWidget(
      _host(
        client,
        collaborationModes: _collab,
        currentCollaborationModeId: 'default',
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('Plan / Default'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(CheckedPopupMenuItem<String>, 'Plan'));
    await tester.pumpAndSettle();
    expect(find.text('Default'), findsWidgets);
    expect(find.textContaining('Plan change failed'), findsOneWidget);
  });

  testWidgets('Grok keeps Agent mode tooltip, not Permissions', (tester) async {
    await tester.pumpWidget(_host(_CollabClient(provider: 'grok')));
    await tester.pump();
    expect(find.byTooltip('Permissions'), findsNothing);
    expect(find.byTooltip('Agent mode'), findsOneWidget);
  });
}

class _FailingCollabClient extends _CollabClient {
  @override
  Future<void> setCollaborationMode(String sessionId, String modeId) async {
    throw Exception('provider failed');
  }
}
