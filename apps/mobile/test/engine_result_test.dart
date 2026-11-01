import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

// PLAN 0208 P5: an engine's answer to /usage or /status reaches the transcript
// as the engine sent it, and renders as its JSON (MADR 0208 D12, D13).

/// grok 1.0.70's captured `_x.ai/billing` answer, read from the daemon's wire
/// capture so the phone tests the same bytes the daemon does.
String _billingAnswer() {
  final lines = File(
    '../../internal/provider/grok/testdata/wire/1.0.70/frames.jsonl',
  ).readAsLinesSync();
  final found = <String>[];
  for (final line in lines) {
    final frame = jsonDecode(line) as Map<String, dynamic>;
    final result = frame['result'];
    if (result is Map && result.containsKey('subscription_tier')) {
      found.add(
        line.substring(
          line.indexOf('"result":') + '"result":'.length,
          line.length - 1,
        ),
      );
    }
  }
  expect(found, hasLength(1), reason: 'one billing answer in the capture');
  return found.single;
}

SessionEvent _result(String body, {String method = '_x.ai/billing'}) =>
    SessionEvent(
      type: 'engine_result',
      sessionId: 's1',
      text: body,
      resultMethod: method,
    );

class _NoHistoryClient extends McremoteClient {
  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];
}

Widget _host(SessionTranscript transcript) => ProviderScope(
  overrides: [
    mcremoteClientProvider.overrideWithValue(_NoHistoryClient()),
    connectionStateProvider.overrideWith(
      (ref) => Stream.value(McConnectionState.connected),
    ),
    sessionTranscriptProvider(
      transcript.sessionId,
    ).overrideWithValue(transcript),
  ],
  child: MaterialApp(home: ChatScreen(sessionId: transcript.sessionId)),
);

void main() {
  const base = SessionTranscript(sessionId: 's1');

  test('engine_result keeps the engine body byte for byte', () {
    final body = _billingAnswer();
    expect(body, contains('"creditUsagePercent":28.0'));
    final t = applySessionEvent(base, _result(body));
    final item = t.items.single;
    expect(item.kind, ChatItemKind.system);
    expect(item.isEngineResult, isTrue);
    expect(item.isAgentNotice, isFalse);
    expect(item.isError, isFalse);
    expect(item.text, body);
    expect(item.resultMethod, '_x.ai/billing');
  });

  test('engine_result with no body is a no-op', () {
    final t = applySessionEvent(base, _result('  '));
    expect(identical(t, base), isTrue);
  });

  test('the daemon notice is not an engine result', () {
    final t = applySessionEvent(
      base,
      SessionEvent(type: 'notice', sessionId: 's1', text: 'Usage: 3 turns'),
    );
    expect(t.items.single.isEngineResult, isFalse);
  });

  test('an engine result survives the transcript cache', () {
    final body = _billingAnswer();
    final item = ChatItem(
      kind: ChatItemKind.system,
      text: body,
      resultMethod: '_x.ai/billing',
    );
    final back = ChatItem.fromJson(item.toJson());
    expect(back.isEngineResult, isTrue);
    expect(back.resultMethod, '_x.ai/billing');
    expect(back.text, body);
  });

  testWidgets('an engine result renders as its JSON under its method', (
    tester,
  ) async {
    final body = _billingAnswer();
    final t = applySessionEvent(
      const SessionTranscript(sessionId: 's1', status: 'idle'),
      _result(body),
    );
    await tester.pumpWidget(_host(t));
    await tester.pump();

    expect(find.text('_x.ai/billing'), findsOneWidget);
    expect(
      find.textContaining(
        '"subscription_tier": "SuperGrok"',
        findRichText: true,
      ),
      findsOneWidget,
      reason: 'the body is pretty-printed JSON, not one system line',
    );
    expect(find.text(body), findsNothing);
  });
}
