import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/chat/chat_helpers.dart';

void main() {
  group('resolveDisplayedMode', () {
    const codex = [
      SessionMode(id: 'default', name: 'default'),
      SessionMode(id: 'read-only', name: 'read-only'),
      SessionMode(id: 'auto', name: 'auto', dangerous: true),
    ];
    const opencode = [
      SessionMode(id: 'build', name: 'build'),
      SessionMode(id: 'plan', name: 'plan'),
      SessionMode(id: 'auto', name: 'auto', dangerous: true),
    ];
    const goose = [
      SessionMode(id: 'auto', name: 'Auto'),
      SessionMode(id: 'approve', name: 'Approve'),
      SessionMode(id: 'chat', name: 'Chat'),
    ];
    const kilo = [
      SessionMode(id: 'code', name: 'code'),
      SessionMode(id: 'plan', name: 'plan'),
      SessionMode(id: 'auto', name: 'auto', dangerous: true),
    ];

    test('exact match wins', () {
      expect(resolveDisplayedMode(codex, 'read-only')?.id, 'read-only');
      expect(resolveDisplayedMode(codex, 'auto')?.id, 'auto');
    });

    test('empty current prefers default on codex-shaped list', () {
      // The pre-0047 bug: modes.first was read-only.
      expect(resolveDisplayedMode(codex, '')?.id, 'default');
      expect(resolveDisplayedMode(codex, null)?.id, 'default');
    });

    test('empty current prefers build on opencode-shaped list', () {
      expect(resolveDisplayedMode(opencode, '')?.id, 'build');
    });

    test('empty current keeps goose auto (unflagged, first non-plan)', () {
      expect(resolveDisplayedMode(goose, '')?.id, 'auto');
    });

    test('empty current prefers code on kilo-shaped list', () {
      // MADR 0076 M1: kilo's default agent is `code`, not opencode's
      // `build` — this must not fall back to list order.
      expect(resolveDisplayedMode(kilo, '')?.id, 'code');
    });

    test('unknown id falls back like empty', () {
      expect(resolveDisplayedMode(codex, 'nope')?.id, 'default');
    });

    test('empty list returns null', () {
      expect(resolveDisplayedMode(const [], null), isNull);
    });

    test('skips plan and dangerous when resolving fallback', () {
      const onlyRisky = [
        SessionMode(id: 'plan', name: 'plan'),
        SessionMode(id: 'auto', name: 'auto', dangerous: true),
        SessionMode(id: 'reviewer', name: 'reviewer'),
      ];
      expect(resolveDisplayedMode(onlyRisky, '')?.id, 'reviewer');
    });
  });
}
