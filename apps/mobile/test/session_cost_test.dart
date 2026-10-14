import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// An agent's cumulative session cost on `usage_update` (MADR 0189 D3,
/// PLAN 0189 C3): a session total in the agent's currency, kept through
/// reports that omit it, and shown in the context chip's tooltip.
class _CostClient extends McremoteClient {
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'grok')],
        complete: true,
      );
}

SessionEvent _usage(Map<String, dynamic> usage) => SessionEvent.fromJson({
  'type': 'usage_update',
  'session_id': 's1',
  'usage': usage,
});

void main() {
  group('parsing', () {
    test('a valid cost is kept with its currency', () {
      final u = Usage.fromJson({
        'used': 10,
        'size': 100,
        'session_cost': {'amount': 0.42, 'currency': 'USD'},
      });
      expect(u.sessionCost, const SessionCost(amount: 0.42, currency: 'USD'));
      expect(u.costUsd, isNull, reason: 'a session total is not a turn cost');
    });

    test('a free session is a real zero', () {
      final u = Usage.fromJson({
        'session_cost': {'amount': 0, 'currency': 'USD'},
      });
      expect(u.sessionCost?.amount, 0);
    });

    test('a lower-case code is upper-cased', () {
      final u = Usage.fromJson({
        'session_cost': {'amount': 1, 'currency': 'eur'},
      });
      expect(u.sessionCost?.currency, 'EUR');
    });

    test('a cost that cannot be shown honestly is dropped', () {
      for (final bad in <Object?>[
        {'amount': -1, 'currency': 'USD'},
        {'amount': 1, 'currency': 'US'},
        {'amount': 1, 'currency': 'U\$D'},
        {'amount': '1', 'currency': 'USD'},
        {'amount': 1},
        'USD 1',
      ]) {
        final u = Usage.fromJson({'session_cost': bad});
        expect(u.sessionCost, isNull, reason: '$bad');
      }
    });

    test('absent means none', () {
      expect(Usage.fromJson({'used': 1, 'size': 2}).sessionCost, isNull);
    });
  });

  group('label', () {
    test('USD is dollars, four decimals under one and two from one up', () {
      expect(
        const SessionCost(amount: 0.0234, currency: 'USD').label,
        '\$0.0234',
      );
      expect(const SessionCost(amount: 1.5, currency: 'USD').label, '\$1.50');
      expect(const SessionCost(amount: 0, currency: 'USD').label, '\$0.0000');
    });

    test('any other currency keeps its code', () {
      expect(
        const SessionCost(amount: 0.42, currency: 'EUR').label,
        '0.4200 EUR',
      );
      expect(const SessionCost(amount: 12, currency: 'JPY').label, '12.00 JPY');
    });
  });

  group('reducer', () {
    const base = SessionTranscript(sessionId: 's1');

    test('a report whose only change is cost reaches the transcript', () {
      var t = applySessionEvent(
        base,
        _usage({
          'used': 10,
          'size': 100,
          'session_cost': {'amount': 0.1, 'currency': 'USD'},
        }),
      );
      t = applySessionEvent(
        t,
        _usage({
          'used': 10,
          'size': 100,
          'session_cost': {'amount': 0.2, 'currency': 'USD'},
        }),
      );
      expect(t.usage?.sessionCost?.amount, 0.2);
    });

    test('a report without a cost keeps the last known one', () {
      var t = applySessionEvent(
        base,
        _usage({
          'used': 10,
          'size': 100,
          'session_cost': {'amount': 0.1, 'currency': 'USD'},
        }),
      );
      t = applySessionEvent(t, _usage({'used': 50, 'size': 100}));
      expect(t.usage?.used, 50, reason: 'the counts still update');
      expect(t.usage?.sessionCost?.amount, 0.1);
    });

    test('an identical report is still a no-op', () {
      final ev = _usage({
        'used': 10,
        'size': 100,
        'session_cost': {'amount': 0.1, 'currency': 'USD'},
      });
      final t1 = applySessionEvent(base, ev);
      expect(identical(applySessionEvent(t1, ev), t1), isTrue);
    });
  });

  group('context chip', () {
    Future<void> pumpWith(WidgetTester tester, Usage usage) async {
      final transcript = SessionTranscript(
        sessionId: 's1',
        items: [ChatItem.assistant('hi')],
        usage: usage,
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            mcremoteClientProvider.overrideWithValue(_CostClient()),
            connectionStateProvider.overrideWith(
              (ref) => Stream.value(McConnectionState.connected),
            ),
            sessionTranscriptProvider('s1').overrideWithValue(transcript),
          ],
          child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
        ),
      );
      await tester.pump();
    }

    testWidgets('the tooltip shows a USD session cost', (tester) async {
      await pumpWith(
        tester,
        const Usage(
          used: 1200,
          size: 8000,
          sessionCost: SessionCost(amount: 0.42, currency: 'USD'),
        ),
      );
      expect(
        find.byTooltip(
          '15% of context used\n1200 / 8000 tokens\nSession cost \$0.4200',
        ),
        findsOneWidget,
      );
    });

    testWidgets('the tooltip shows another currency by its code', (
      tester,
    ) async {
      await pumpWith(
        tester,
        const Usage(
          used: 1200,
          size: 0,
          sessionCost: SessionCost(amount: 3, currency: 'EUR'),
        ),
      );
      expect(
        find.byTooltip('1200 context tokens used\nSession cost 3.00 EUR'),
        findsOneWidget,
      );
    });

    testWidgets('no cost, no cost line', (tester) async {
      await pumpWith(tester, const Usage(used: 1200, size: 8000));
      expect(
        find.byTooltip('15% of context used\n1200 / 8000 tokens'),
        findsOneWidget,
      );
      expect(find.byTooltip(RegExp('Session cost')), findsNothing);
    });
  });
}
