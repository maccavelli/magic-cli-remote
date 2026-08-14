import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';

// MADR 0083 D5: each provider-auth wire code renders as one actionable
// sentence, never the daemon's raw error chain.
void main() {
  McException ex(String code, [String msg = 'raw daemon text']) =>
      McException(msg, code: code);

  test('keyring_managed points at the host', () {
    expect(friendlyOpError(ex('keyring_managed')), contains('keyring'));
    expect(friendlyOpError(ex('keyring_managed')), contains('host'));
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

  test('credential_failed falls through to the daemon message', () {
    expect(
      friendlyOpError(ex('credential_failed', 'engine said 500')),
      'engine said 500',
    );
  });
}
