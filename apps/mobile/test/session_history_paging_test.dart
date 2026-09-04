import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart'
    show kHistoryMaxPages;
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

/// Scripted `session.history` responder: returns [pages] in order.
class _PagedClient extends McremoteClient {
  _PagedClient(this.pages);

  final List<Envelope> pages;
  final List<Map<String, dynamic>?> seenPayloads = [];
  var _calls = 0;

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
    expect(type, 'session.history');
    seenPayloads.add(payload);
    final res = pages[_calls++];
    if (expectedType != null &&
        expectedType.isNotEmpty &&
        res.type != 'error' &&
        res.type != expectedType) {
      throw McException(
        'unexpected $type response: ${res.type}',
        code: 'bad_response_type',
        permanent: false,
      );
    }
    return res;
  }
}

Map<String, dynamic> _event(int seq) => {
  'type': 'user_message',
  'session_id': 's1',
  'seq': seq,
  'text': 'm$seq',
};

Envelope _page(
  List<int> seqs, {
  bool truncated = false,
  int nextSinceSeq = 0,
}) => Envelope(
  type: 'session.history_result',
  payload: {
    'events': [for (final s in seqs) _event(s)],
    'truncated': truncated,
    if (nextSinceSeq > 0) 'next_since_seq': nextSinceSeq,
  },
);

void main() {
  backwardTests();
  capsTests();
  test('follows truncated pages and accumulates the full ring', () async {
    final client = _PagedClient([
      _page([1, 2, 3], truncated: true, nextSinceSeq: 3),
      _page([4, 5], truncated: true, nextSinceSeq: 5),
      _page([6]),
    ]);
    final events = await client.sessionHistory('s1');
    expect(events.map((e) => e.seq), [1, 2, 3, 4, 5, 6]);
    // Later pages resume from the previous page's next_since_seq.
    expect(client.seenPayloads[1]?['since_seq'], 3);
    expect(client.seenPayloads[2]?['since_seq'], 5);
  });

  test(
    'mid-page error fails the whole fetch, never a partial prefix',
    () async {
      final client = _PagedClient([
        _page([1, 2, 3], truncated: true, nextSinceSeq: 3),
        Envelope(type: 'error', payload: {'error': 'transient'}),
      ]);
      // An oldest-only prefix would make resyncHistory rebuild away the newest
      // content the user already has. A typed failure keeps local state intact
      // and lets the caller choose retry UI rather than mistaking it for an
      // authoritative empty history.
      await expectLater(
        client.sessionHistory('s1'),
        throwsA(isA<McException>()),
      );
    },
  );

  test('a non-advancing next_since_seq ends the loop', () async {
    final client = _PagedClient([
      _page([1, 2], truncated: true, nextSinceSeq: 2),
      _page([3], truncated: true, nextSinceSeq: 2),
      // Never requested: the loop must stop rather than refetch seq 2 forever.
      _page([99]),
    ]);
    final events = await client.sessionHistory('s1');
    expect(events.map((e) => e.seq), [1, 2, 3]);
    expect(client.seenPayloads, hasLength(2));
  });

  // MADR 0056 M-7: a truncated page followed by an empty events page must fail.
  test(
    'truncated page then empty events fails the whole fetch (M-7)',
    () async {
      final client = _PagedClient([
        _page([1, 2, 3], truncated: true, nextSinceSeq: 3),
        Envelope(
          type: 'session.history_result',
          payload: {'events': <Map<String, dynamic>>[], 'truncated': false},
        ),
      ]);
      await expectLater(
        client.sessionHistory('s1'),
        throwsA(isA<McException>()),
        reason:
            'MADR 0056 M-7: incomplete multi-page history must not return a '
            'partial ring as success',
      );
    },
  );
}

Envelope _backwardPage(
  List<int> seqs, {
  int prevBeforeSeq = 0,
  int firstSeq = 0,
  int latestSeq = 0,
}) => Envelope(
  type: 'session.history_result',
  payload: {
    'events': [for (final s in seqs) _event(s)],
    'truncated': prevBeforeSeq > 0,
    if (prevBeforeSeq > 0) 'prev_before_seq': prevBeforeSeq,
    if (firstSeq > 0) 'first_seq': firstSeq,
    if (latestSeq > 0) 'latest_seq': latestSeq,
  },
);

void backwardTests() {
  test('the first fetch asks for the newest page, not the oldest', () async {
    final client = _PagedClient([
      _backwardPage(
        [98, 99, 100],
        prevBeforeSeq: 98,
        firstSeq: 1,
        latestSeq: 100,
      ),
    ]);

    final page = await client.sessionHistoryNewest('s1', limit: 3);

    // A chat screen opens at the bottom. Asking for the oldest page first is
    // what made a phone download a whole transcript to render its tail
    // (MADR 0138 F17).
    expect(client.seenPayloads.single!['newest'], isTrue);
    expect(client.seenPayloads.single!.containsKey('since_seq'), isFalse);
    expect(client.seenPayloads.single!.containsKey('before_seq'), isFalse);

    expect([for (final e in page.events) e.seq], [98, 99, 100]);
    expect(page.prevBeforeSeq, 98);
    expect(page.hasOlder, isTrue);
    expect(page.firstSeq, 1);
    expect(page.latestSeq, 100);
  });

  test('an older page is requested with before_seq, not newest', () async {
    final client = _PagedClient([
      _backwardPage([95, 96, 97], prevBeforeSeq: 95),
    ]);

    final page = await client.sessionHistoryNewest(
      's1',
      beforeSeq: 98,
      limit: 3,
    );

    expect(client.seenPayloads.single!['before_seq'], 98);
    expect(client.seenPayloads.single!.containsKey('newest'), isFalse);
    expect(page.prevBeforeSeq, 95);
  });

  test('the oldest retained page reports no cursor, so a walk stops', () async {
    final client = _PagedClient([
      _backwardPage([1, 2], firstSeq: 1, latestSeq: 2),
    ]);

    final page = await client.sessionHistoryNewest('s1', beforeSeq: 3);

    expect(page.hasOlder, isFalse);
    expect(page.prevBeforeSeq, isNull);
  });

  test('a truncated page with no cursor is treated as the end', () async {
    // A daemon that says "more remains" but cannot say where would otherwise
    // make a client re-request the same window forever.
    final client = _PagedClient([
      Envelope(
        type: 'session.history_result',
        payload: {
          'events': [_event(7)],
          'truncated': true,
        },
      ),
    ]);

    final page = await client.sessionHistoryNewest('s1');

    expect(page.hasOlder, isFalse);
  });

  test('a backward walk covers the ring without overlap', () async {
    final client = _PagedClient([
      _backwardPage([7, 8, 9], prevBeforeSeq: 7),
      _backwardPage([4, 5, 6], prevBeforeSeq: 4),
      _backwardPage([1, 2, 3], firstSeq: 1),
    ]);

    final seen = <int>[];
    var before = 0;
    for (var i = 0; i < kHistoryMaxPages; i++) {
      final page = await client.sessionHistoryNewest(
        's1',
        beforeSeq: before,
        limit: 3,
      );
      seen.insertAll(0, [for (final e in page.events) e.seq]);
      if (!page.hasOlder) break;
      before = page.prevBeforeSeq!;
    }

    expect(seen, [1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(seen.toSet().length, seen.length, reason: 'pages must not overlap');
  });

  test('an error frame is surfaced, not swallowed as an empty page', () async {
    final client = _PagedClient([
      Envelope(type: 'error', payload: {'code': 'nope', 'message': 'boom'}),
    ]);

    await expectLater(
      client.sessionHistoryNewest('s1'),
      throwsA(isA<McException>()),
    );
  });
}

void capsTests() {
  test('the client reads the host budget instead of assuming 800', () {
    final caps = ServerCaps.tryParse({
      'protocol': 2,
      'read_deadline_ms': 120000,
      'ping_interval_ms': 25000,
      'ws_ping_resets_deadline': true,
      'history_ring': 16384,
      'history_budget_bytes': 33554432,
      'max_frame_bytes': 1 << 20,
    });
    expect(caps, isNotNull);
    // Hardcoding 800 is what made the client walk a ring it assumed was small
    // (MADR 0138 F13/F17).
    expect(caps!.historyRing, 16384);
    expect(caps.historyBudgetBytes, 33554432);
  });

  test('an older daemon without the budget field still parses', () {
    final caps = ServerCaps.tryParse({
      'protocol': 2,
      'read_deadline_ms': 120000,
      'ping_interval_ms': 25000,
      'ws_ping_resets_deadline': true,
      'history_ring': 800,
      'max_frame_bytes': 1 << 20,
    });
    expect(caps, isNotNull);
    expect(caps!.historyRing, 800);
    expect(caps.historyBudgetBytes, 0, reason: 'absent means bounded by count');
  });
}
