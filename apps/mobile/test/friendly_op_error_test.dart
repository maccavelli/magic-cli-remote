import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';

// MADR 0083 D5: each provider-auth wire code renders as one actionable
// sentence, never the daemon's raw error chain.
void main() {
  McException ex(String code, [String msg = 'raw daemon text']) =>
      McException(msg, code: code);

  // MADR 0160 D13: a session whose agent the host no longer registers answers
  // unknown_provider; the phone must say so rather than show the raw code.
  test('unknown_provider says the agent is gone from the host', () {
    final copy = friendlyOpError(ex('unknown_provider', 'unknown provider'));
    expect(copy, contains('no longer available'));
    expect(copy, isNot('unknown provider'));
  });

  test('method_unsupported suggests the API-key path', () {
    expect(friendlyOpError(ex('method_unsupported')), contains('API key'));
  });

  test('invalid_key keeps the validator detail', () {
    expect(
      friendlyOpError(ex('invalid_key', 'credential is empty')),
      contains('credential is empty'),
    );
  });

  test('engine_unavailable asks about the engine', () {
    expect(friendlyOpError(ex('engine_unavailable')), contains('engine'));
  });

  test('credential_not_accepted says the agent is not using the value', () {
    expect(
      friendlyOpError(ex('credential_not_accepted')),
      contains('not using'),
    );
    expect(friendlyOpError(ex('credential_not_accepted')), contains('sign-in'));
  });

  // MADR 0074 D36: credential_failed has copy of its own now. It keeps the
  // daemon's detail, which is all that says what failed, and no longer shows
  // it bare; this test read "falls through to the daemon message" before.
  test('credential_failed keeps the daemon detail inside its own sentence', () {
    final copy = friendlyOpError(ex('credential_failed', 'engine said 500'));
    expect(copy, contains('engine said 500'));
    expect(copy, isNot('engine said 500'));
    expect(copy, contains('could not complete'));
  });

  // MADR 0074 D36: each coordinator outcome is one actionable sentence.
  group('the credential coordinator\'s outcomes', () {
    const cases = {
      'provider_auth_busy': 'in progress',
      'credential_conflict': 'nothing was replaced',
      'unsupported_backend': 'on the host',
      'recovery_required': 'auth-recovery',
      'too_many_sign_ins': 'finish or cancel',
      'unsupported': 'support',
    };
    cases.forEach((code, phrase) {
      test('$code says what to do', () {
        final copy = friendlyOpError(ex(code));
        expect(copy, contains(phrase));
        expect(copy, isNot('raw daemon text'));
      });
    });
  });
}
