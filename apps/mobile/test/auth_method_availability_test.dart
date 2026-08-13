import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

// MADR 0083 D4: absent wire fields (an old daemon) read as available, and a
// daemon-annotated method disables the affordance client-side.
void main() {
  test('absent availability fields read as usable', () {
    final m = AuthMethod.fromJson(const {
      'id': 'togetherai:api',
      'type': 'api_key',
      'label': 'API key',
    });
    expect(m.available, isTrue);
    expect(m.isUsable, isTrue);
  });

  test('an annotated method carries its reason and is not usable', () {
    final m = AuthMethod.fromJson(const {
      'id': 'together:api',
      'type': 'api_key',
      'label': 'API key',
      'available': false,
      'reason': 'keyring_managed',
    });
    expect(m.available, isFalse);
    expect(m.reason, 'keyring_managed');
    expect(m.isUsable, isFalse);
  });

  test('browser methods stay unusable even without annotation', () {
    final m = AuthMethod.fromJson(const {
      'id': 'gitlab:0',
      'type': 'oauth_browser',
      'label': 'Sign in',
    });
    expect(m.isUsable, isFalse);
  });

  test('hasUsableMethod: empty methods mean the plain-key long tail', () {
    const bare = UpstreamAuth(id: 'x', status: 'missing');
    expect(bare.hasUsableMethod, isTrue);

    const walled = UpstreamAuth(
      id: 'together',
      status: 'missing',
      methods: [
        AuthMethod(
          id: 'together:api',
          type: 'api_key',
          label: 'API key',
          available: false,
          reason: 'keyring_managed',
        ),
      ],
    );
    expect(walled.hasUsableMethod, isFalse);
  });
}
