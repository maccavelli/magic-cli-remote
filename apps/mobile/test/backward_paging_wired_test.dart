import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// MADR 0141 F1: the backward pager was implemented on both sides, unit-tested,
/// and never called from production code. Every reference to
/// `sessionHistoryNewest` lived in its own test file.
///
/// A defect that is an *absence* is invisible to any test that exercises the
/// code — there is nothing to exercise. Only a scan sees it, which is the same
/// reasoning behind `internal/provider/opencode/surface_contract_test.go`'s A11
/// guard, and that one caught a real regression.
void main() {
  test('sessionHistoryNewest has a production caller', () {
    final callers = <String>[];
    var scanned = 0;

    for (final entity in Directory('lib').listSync(recursive: true)) {
      if (entity is! File || !entity.path.endsWith('.dart')) continue;
      scanned++;
      // The declaration itself lives here; it is not a caller.
      if (entity.path.endsWith('data/ws/mcremote_client.dart')) continue;
      if (entity.readAsStringSync().contains('sessionHistoryNewest')) {
        callers.add(entity.path);
      }
    }

    expect(
      scanned,
      greaterThan(50),
      reason: 'the scan found almost no Dart files; it would pass vacuously',
    );
    expect(
      callers,
      isNotEmpty,
      reason:
          'nothing in lib/ calls sessionHistoryNewest. The chat screen '
          'would fall back to the forward walk, which fetches the OLDEST '
          '6,400 events and never the newest (MADR 0141 F1/F2).',
    );
  });

  test('the forward walk is no longer what a chat opens on', () {
    final sync = File('lib/state/session_synchronizer.dart').readAsStringSync();
    expect(
      sync.contains('sessionHistoryNewest'),
      isTrue,
      reason: 'the synchronizer must fetch the newest page',
    );
    expect(
      RegExp(r'client\.sessionHistory\(').hasMatch(sync),
      isFalse,
      reason:
          'a forward walk left in the synchronizer would hand '
          'resyncHistory the oldest events, and its missedOlder branch would '
          'rebuild a backward-paged transcript down to the beginning while '
          'the user was reading the end',
    );
  });
}
