import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';

void main() {
  group('friendlyOpError', () {
    test('maps session_forbidden for multi-device', () {
      final e = McException('nope', code: 'session_forbidden');
      expect(friendlyOpError(e), 'This session belongs to another device.');
    });

    test('maps session_not_live with recovery hint', () {
      final e = McException('gone', code: 'session_not_live');
      expect(friendlyOpError(e), contains('start again'));
    });

    test('maps session_limit and rate_limited', () {
      expect(
        friendlyOpError(McException('x', code: 'session_limit')),
        contains('session limit'),
      );
      expect(
        friendlyOpError(McException('x', code: 'rate_limited')),
        contains('rate-limiting'),
      );
    });

    test('falls back to message for unknown codes', () {
      expect(
        friendlyOpError(McException('custom wire text', code: 'other')),
        'custom wire text',
      );
    });
  });

  group('handshakeErrorFrom auth', () {
    test('auth_ok is success', () {
      expect(
        handshakeErrorFrom('auth_ok', 'auth_ok', null, isPair: false),
        isNull,
      );
    });

    test('auth_error maps code and is permanent', () {
      final e = handshakeErrorFrom('auth_ok', 'auth_error', {
        'message': 'bad',
        'code': 'invalid_token',
      }, isPair: false);
      expect(e, isNotNull);
      expect(e!.code, 'invalid_token');
      expect(e.permanent, isTrue);
      expect(e.isInvalidToken, isTrue);
      expect(e.message, 'bad');
    });

    test('unexpected type is permanent', () {
      final e = handshakeErrorFrom('auth_ok', 'pong', null, isPair: false);
      expect(e!.code, 'unexpected_auth_response');
      expect(e.permanent, isTrue);
    });

    test('a transient auth failure keeps auto-reconnect armed', () {
      // A generic auth_failed is the daemon withholding the detail — often a
      // store hiccup. Parking on it killed background reconnection until the
      // user next opened the app (MADR 0046 L-3).
      final e = handshakeErrorFrom('auth_ok', 'auth_error', {
        'message': 'auth failed',
        'code': 'auth_failed',
      }, isPair: false);
      expect(e!.permanent, isFalse);
    });

    test('an error envelope surfaces the daemon message, not a guess', () {
      // The over-limit auth reply is a plain `error` frame.
      final e = handshakeErrorFrom('auth_ok', 'error', {
        'message': 'too many attempts',
        'code': 'rate_limited',
      }, isPair: false);
      expect(e!.code, 'rate_limited');
      expect(e.message, 'too many attempts');
      expect(e.permanent, isFalse);
    });

    test('an error envelope naming a permanent code stays permanent', () {
      final e = handshakeErrorFrom('auth_ok', 'error', {
        'message': 'nope',
        'code': 'unauthorized',
      }, isPair: false);
      expect(e!.permanent, isTrue);
    });
  });

  group('handshakeErrorFrom pair', () {
    test('pair_ok is success', () {
      expect(
        handshakeErrorFrom('pair_ok', 'pair_ok', {}, isPair: true),
        isNull,
      );
    });

    test('pair_error maps expired', () {
      final e = handshakeErrorFrom('pair_ok', 'pair_error', {
        'message': 'gone',
        'code': 'expired',
      }, isPair: true);
      expect(e!.code, 'expired');
      expect(e.permanent, isTrue);
    });

    test('unexpected pair type', () {
      final e = handshakeErrorFrom('pair_ok', 'auth_ok', null, isPair: true);
      expect(e!.code, 'unexpected_pair_response');
    });

    test('rate_limited is transient — the limiter lifts on its own', () {
      // Another device claiming a code at the same moment trips the
      // process-wide limiter. Reading that as permanent told the user to
      // re-pair over something that clears in minutes (MADR 0046 L-3).
      final e = handshakeErrorFrom('pair_ok', 'pair_error', {
        'message': 'slow down',
        'code': 'rate_limited',
      }, isPair: true);
      expect(e!.permanent, isFalse);
    });

    test('create_failed is transient, client_key_required is not', () {
      expect(
        handshakeErrorFrom('pair_ok', 'pair_error', {
          'code': 'create_failed',
        }, isPair: true)!.permanent,
        isFalse,
      );
      expect(
        handshakeErrorFrom('pair_ok', 'pair_error', {
          'code': 'client_key_required',
        }, isPair: true)!.permanent,
        isTrue,
      );
    });
  });
}
