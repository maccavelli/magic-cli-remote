import 'package:flutter/material.dart';
import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Records what the chat screen asked for and what it sent, so the two halves
/// of the `/model` contract — the scoped catalog request and the composed
/// command — can both be asserted.
class _ModelClient extends McremoteClient {
  _ModelClient({this.catalog});

  final PickerCatalog? catalog;
  final List<String> prompts = [];
  final List<Map<String, String?>> modelRequests = [];
  bool throwOnList = false;
  Completer<PickerCatalog>? modelGate;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [
          SessionMeta(id: 's1', provider: 'opencode', cwd: '/home/mac'),
        ],
        complete: true,
      );

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    prompts.add(text);
  }

  @override
  Future<PickerCatalog> listModels(
    String provider, {
    String? scope,
    String? modelProvider,
    String? sessionId,
  }) async {
    modelRequests.add({
      'provider': provider,
      'scope': scope,
      'model_provider': modelProvider,
      'session_id': sessionId,
    });
    if (throwOnList) throw Exception('catalog down');
    final gate = modelGate;
    if (gate != null) return gate.future;
    return catalog ?? PickerCatalog(allowCustom: true, provider: provider);
  }
}

Widget _host(SessionTranscript transcript, McremoteClient client) {
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider(
        transcript.sessionId,
      ).overrideWithValue(transcript),
    ],
    child: MaterialApp(home: ChatScreen(sessionId: transcript.sessionId)),
  );
}

SessionTranscript seeded({required bool modelAvailable}) => SessionTranscript(
  sessionId: 's1',
  status: 'idle',
  items: const [],
  nextSeq: 0,
  remoteCommands: [
    RemoteCommand(
      name: 'model',
      hint: '[name]',
      description: 'Show or switch the agent model',
      available: modelAvailable,
      reason: modelAvailable ? '' : "this agent doesn't offer it",
    ),
  ],
);

PickerCatalog twoModels() => PickerCatalog(
  provider: 'opencode',
  modelProvider: 'opencode',
  source: PickerSource.live,
  allowCustom: true,
  defaultIds: const ['opencode/big-pickle'],
  options: [
    PickerOption(id: 'opencode/big-pickle', label: 'Big Pickle'),
    PickerOption(id: 'opencode/kimi-k3', label: 'Kimi K3'),
  ],
);

Future<void> submit(WidgetTester tester, String text) async {
  await tester.enterText(find.byType(TextField).first, text);
  await tester.tap(find.byIcon(Icons.send));
  await tester.pumpAndSettle();
}

void main() {
  setUp(() {
    // getDefaultThinkingLevel is awaited before the picker opens; without a
    // mock SharedPreferences that call can hang and leave the sheet closed.
    SharedPreferences.setMockInitialValues({});
  });

  testWidgets('bare /model opens the picker and submits the chosen id', (
    tester,
  ) async {
    final client = _ModelClient(catalog: twoModels());
    await tester.pumpWidget(_host(seeded(modelAvailable: true), client));
    await tester.pumpAndSettle();

    await submit(tester, '/model');

    // The catalog request is session-scoped, so the daemon can answer with the
    // models of the provider this session is actually using.
    expect(client.modelRequests, hasLength(1));
    expect(client.modelRequests.single['session_id'], 's1');
    expect(client.modelRequests.single['provider'], 'opencode');

    // Picker is open, and nothing has been sent yet.
    expect(find.text('Kimi K3'), findsOneWidget);
    expect(client.prompts, isEmpty);

    await tester.tap(find.text('Kimi K3'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select'));
    await tester.pumpAndSettle();

    // The picker is a composition aid: what reaches the daemon is the ordinary
    // command, so in-place switching and its notices are untouched.
    expect(client.prompts, ['/model opencode/kimi-k3']);
  });

  testWidgets('cancelling the picker sends nothing', (tester) async {
    final client = _ModelClient(catalog: twoModels());
    await tester.pumpWidget(_host(seeded(modelAvailable: true), client));
    await tester.pumpAndSettle();

    await submit(tester, '/model');
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(client.prompts, isEmpty);
  });

  testWidgets(
    'double-tapping bare /model opens one picker and intercepts once',
    (tester) async {
      final client = _ModelClient()..modelGate = Completer<PickerCatalog>();
      await tester.pumpWidget(_host(seeded(modelAvailable: true), client));
      await tester.pumpAndSettle();

      await tester.enterText(find.byType(TextField).first, '/model');
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();

      expect(client.modelRequests, hasLength(1));
      expect(client.prompts, isEmpty);

      client.modelGate!.complete(twoModels());
      await tester.pumpAndSettle();
      expect(find.text('Kimi K3'), findsOneWidget);
    },
  );

  testWidgets('/model with an argument never opens the picker', (tester) async {
    final client = _ModelClient(catalog: twoModels());
    await tester.pumpWidget(_host(seeded(modelAvailable: true), client));
    await tester.pumpAndSettle();

    await submit(tester, '/model opencode/kimi-k3');

    expect(client.modelRequests, isEmpty);
    expect(client.prompts, ['/model opencode/kimi-k3']);
  });

  testWidgets('an unavailable /model goes to the daemon for its explanation', (
    tester,
  ) async {
    // MADR 0023: the daemon decides whether a canonical command works here.
    // The client must not answer with a picker for a command that cannot run.
    final client = _ModelClient(catalog: twoModels());
    await tester.pumpWidget(_host(seeded(modelAvailable: false), client));
    await tester.pumpAndSettle();

    await submit(tester, '/model');

    expect(client.modelRequests, isEmpty);
    expect(client.prompts, ['/model']);
  });

  testWidgets('a catalog failure falls through to the daemon', (tester) async {
    final client = _ModelClient(catalog: twoModels())..throwOnList = true;
    await tester.pumpWidget(_host(seeded(modelAvailable: true), client));
    await tester.pumpAndSettle();

    await submit(tester, '/model');

    expect(client.modelRequests, hasLength(1));
    // The daemon answers with the current model rather than the user being
    // left with nothing.
    expect(client.prompts, ['/model']);
  });

  testWidgets(
    'an empty catalog falls through instead of showing a blank sheet',
    (tester) async {
      final client = _ModelClient(
        catalog: PickerCatalog(allowCustom: true, provider: 'opencode'),
      );
      await tester.pumpWidget(_host(seeded(modelAvailable: true), client));
      await tester.pumpAndSettle();

      await submit(tester, '/model');

      expect(client.prompts, ['/model']);
    },
  );
}
