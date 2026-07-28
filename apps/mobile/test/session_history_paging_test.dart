import 'package:flutter_test/flutter_test.dart';
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
    Duration timeout = const Duration(seconds: 30),
  }) async {
    expect(type, 'session.history');
    seenPayloads.add(payload);
    return pages[_calls++];
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
  type: 'result',
  payload: {
    'events': [for (final s in seqs) _event(s)],
    'truncated': truncated,
    if (nextSinceSeq > 0) 'next_since_seq': nextSinceSeq,
  },
);

void main() {
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
}
