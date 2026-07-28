import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

/// The `dangerous` flag is how the daemon tells the client that a mode removes
/// a safety control. These tests pin the two properties the UI depends on:
/// it decodes compatibly from daemons that never send it, and it participates
/// in equality so a change to it is not swallowed by the transcript reducer.
void main() {
  group('SessionMode.dangerous', () {
    test('defaults to false when the daemon omits it', () {
      final m = SessionMode.fromJson({'id': 'build', 'name': 'build'});
      expect(m.dangerous, isFalse);
    });

    test('decodes when present', () {
      final m = SessionMode.fromJson({
        'id': 'auto',
        'name': 'auto',
        'description': 'Auto-approve',
        'dangerous': true,
      });
      expect(m.dangerous, isTrue);
      expect(m.description, 'Auto-approve');
    });

    // Note: a wrong-typed `dangerous` throws, matching how `live` and `ready`
    // decode elsewhere in this file. The daemon serializes a Go bool, so the
    // case is unreachable in practice; hardening only this field would be an
    // inconsistency, and hardening all of them is a separate decision.

    test('an explicit false decodes as false', () {
      final m = SessionMode.fromJson({
        'id': 'build',
        'name': 'build',
        'dangerous': false,
      });
      expect(m.dangerous, isFalse);
    });

    // Without this, a mode list differing only in `dangerous` compares equal,
    // the reducer returns the identical transcript, and the chip never updates
    // — silently defeating the flag (MADR 0042 D8 / MADR 0044).
    test('participates in equality and hashCode', () {
      const safe = SessionMode(id: 'auto', name: 'auto');
      const risky = SessionMode(id: 'auto', name: 'auto', dangerous: true);

      expect(safe == risky, isFalse);
      expect(safe.hashCode == risky.hashCode, isFalse);
    });

    test('otherwise-identical modes still compare equal', () {
      const a = SessionMode(id: 'auto', name: 'auto', dangerous: true);
      const b = SessionMode(id: 'auto', name: 'auto', dangerous: true);
      expect(a, equals(b));
      expect(a.hashCode, equals(b.hashCode));
    });

    // Goose has shipped `auto` as its *default* mode for a while and does not
    // send the flag. Nothing about it may change.
    test('a goose mode list is entirely undangerous', () {
      final goose = [
        {'id': 'auto', 'name': 'Auto', 'description': 'Automatically approve tool calls'},
        {'id': 'approve', 'name': 'Approve'},
        {'id': 'smart_approve', 'name': 'Smart Approve'},
        {'id': 'chat', 'name': 'Chat'},
      ].map(SessionMode.fromJson).toList();

      expect(goose.every((m) => !m.dangerous), isTrue,
          reason: 'goose sends no dangerous flag; inferring danger from the id '
              '"auto" would alarm on its default state');
    });
  });
}
