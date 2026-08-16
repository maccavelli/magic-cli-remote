import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

class _DiffForkClient extends McremoteClient {
  @override
  McConnectionState get state => McConnectionState.connected;

  SessionDiffResult diff = const SessionDiffResult(
    summary: 'diff --git a/x b/x\n+hello\n',
    baseSha: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    scope: 'working_tree',
    truncated: true,
  );
  SessionMeta? forked;
  String? lastForkTurnId;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'codex', cwd: '/tmp')],
        complete: true,
      );

  @override
  Future<SessionDiffResult> sessionDiff(
    String sessionId, {
    String? messageId,
  }) async => diff;

  @override
  Future<SessionMeta> forkSession(
    String sessionId, {
    String? messageId,
    String? lastTurnId,
  }) async {
    lastForkTurnId = lastTurnId ?? messageId;
    return forked ??
        SessionMeta(id: 'child-1', provider: 'codex', name: 'work (fork)');
  }
}

class _CaptureClient extends McremoteClient {
  @override
  McConnectionState get state => McConnectionState.connected;

  String? lastType;
  Map<String, dynamic>? lastPayload;
  Envelope reply = Envelope(type: 'ok', payload: const {});

  @override
  Future<Envelope> request(
    String type, {
    Map<String, dynamic>? payload,
    String? token,
    // Nullable to match McremoteClient.request, which resolves an unset
    // timeout through opTimeoutFor (MADR 0095 D7).
    Duration? timeout,
    String? requestId,
    String? expectedType,
    bool idempotentRetry = false,
  }) async {
    lastType = type;
    lastPayload = payload;
    return reply;
  }
}

SessionTranscript _transcript({List<RemoteCommand>? commands}) {
  commands ??= [
    RemoteCommand(name: 'diff', available: true),
    RemoteCommand(name: 'fork', available: true),
  ];
  return SessionTranscript(
    sessionId: 's1',
    items: [ChatItem.assistant('hi')],
    remoteCommands: commands,
  );
}

void main() {
  testWidgets(
    'advertised fork/diff commands appear without provider-name checks',
    (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            mcremoteClientProvider.overrideWithValue(_DiffForkClient()),
            connectionStateProvider.overrideWith(
              (ref) => Stream.value(McConnectionState.connected),
            ),
            sessionTranscriptProvider('s1').overrideWithValue(_transcript()),
          ],
          child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
        ),
      );
      await tester.pump();
      await tester.tap(find.byTooltip('Session actions'));
      await tester.pumpAndSettle();
      expect(find.text('View file diff'), findsOneWidget);
      expect(find.text('Fork session'), findsOneWidget);
    },
  );

  testWidgets('unavailable advertised commands stay hidden', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(_DiffForkClient()),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(
            _transcript(
              commands: [
                RemoteCommand(
                  name: 'diff',
                  available: false,
                  reason: 'integration not wired',
                ),
                RemoteCommand(
                  name: 'fork',
                  available: false,
                  reason: 'integration not wired',
                ),
              ],
            ),
          ),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('Session actions'));
    await tester.pumpAndSettle();
    expect(find.text('View file diff'), findsNothing);
    expect(find.text('Fork session'), findsNothing);
  });

  testWidgets('diff dialog shows fence, base SHA, and truncation', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(_DiffForkClient()),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(_transcript()),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('Session actions'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('View file diff'));
    await tester.pumpAndSettle();
    expect(find.text('Session diff'), findsOneWidget);
    expect(
      find.textContaining('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),
      findsOneWidget,
    );
    expect(find.textContaining('[diff truncated]'), findsOneWidget);
    expect(find.textContaining('```diff'), findsOneWidget);
    expect(find.textContaining('Scope: working_tree'), findsOneWidget);
  });

  testWidgets('fork opens the returned child session', (tester) async {
    final client = _DiffForkClient()
      ..forked = SessionMeta(
        id: 'child-1',
        provider: 'codex',
        name: 'work (fork)',
      );
    final router = GoRouter(
      initialLocation: '/sessions/s1',
      routes: [
        GoRoute(
          path: '/sessions/:id',
          builder: (context, state) {
            final id = state.pathParameters['id']!;
            if (id == 's1') {
              return const ChatScreen(sessionId: 's1');
            }
            return Scaffold(body: Text('opened-$id'));
          },
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(client),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(_transcript()),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('Session actions'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fork session'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Fork'));
    await tester.pumpAndSettle();
    expect(find.text('opened-child-1'), findsOneWidget);
  });

  test('forkSession prefers last_turn_id on the wire', () async {
    final client = _CaptureClient()
      ..reply = Envelope(
        type: 'session.created',
        payload: {'id': 'child-1', 'provider': 'codex'},
      );
    await client.forkSession('s1', lastTurnId: 'turn-9');
    expect(client.lastType, 'session.fork');
    expect(client.lastPayload?['session_id'], 's1');
    expect(client.lastPayload?['last_turn_id'], 'turn-9');
    expect(client.lastPayload?.containsKey('message_id'), isFalse);
  });

  test('sessionDiff decodes additive metadata', () async {
    final client = _CaptureClient()
      ..reply = Envelope(
        type: 'session.diff_result',
        payload: {
          'session_id': 's1',
          'summary': 'patch',
          'base_sha': 'abc',
          'scope': 'latest_codex_turn',
          'truncated': true,
        },
      );
    final got = await client.sessionDiff('s1');
    expect(client.lastType, 'session.diff');
    expect(got.summary, 'patch');
    expect(got.baseSha, 'abc');
    expect(got.scope, 'latest_codex_turn');
    expect(got.truncated, isTrue);
  });
}
