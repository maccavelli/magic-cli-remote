import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';

void main() {
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
  });
}
