import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

/// The phone half of the timeout ladder (MADR 0095 D7).
///
/// The daemon's `asyncOpTimeout` is authoritative for how long an operation
/// may take; the phone's timeout is a backstop for a dead link, not a
/// competing deadline. Where the two were equal the daemon's own
/// `deadline_exceeded` frame was pre-empted by a client timeout and an
/// idempotent retry; where the client was shorter (models.list and friends
/// at 30s against the daemon's 60s) a successful late reply was discarded
/// and the op always surfaced as a failure.
///
/// Both sides read `internal/protocol/op_timeouts.json`, so the ladder
/// cannot drift in one language without failing in the other.
({int defaultMs, int marginMs, Map<String, int> methods}) _loadOpTimeouts() {
  // flutter test runs with apps/mobile as the working directory.
  const path = '../../internal/protocol/op_timeouts.json';
  final file = File(path);
  if (!file.existsSync()) {
    fail('shared timeout table not found at ${file.absolute.path}');
  }
  final json = jsonDecode(file.readAsStringSync()) as Map<String, dynamic>;
  return (
    defaultMs: json['default_ms'] as int,
    marginMs: json['client_margin_ms'] as int,
    methods: (json['methods'] as Map<String, dynamic>).map(
      (k, v) => MapEntry(k, v as int),
    ),
  );
}

void main() {
  test('every client request timeout exceeds the daemon deadline', () {
    final tbl = _loadOpTimeouts();
    final margin = Duration(milliseconds: tbl.marginMs);
    expect(tbl.methods, isNotEmpty);
    for (final entry in tbl.methods.entries) {
      final daemon = Duration(milliseconds: entry.value);
      expect(
        opTimeoutFor(entry.key),
        daemon + margin,
        reason:
            '${entry.key}: the phone must not race the daemon\'s own '
            'deadline — the authoritative failure is its error frame',
      );
    }
  });

  test('an unlisted method falls back to the shared default', () {
    final tbl = _loadOpTimeouts();
    expect(
      opTimeoutFor('no.such.method'),
      Duration(milliseconds: tbl.defaultMs + tbl.marginMs),
    );
  });
}
