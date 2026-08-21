import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

AuthMethod method(String id, {bool? configured}) => AuthMethod.fromJson({
  'id': id,
  'type': 'api_key',
  'label': id,
  // Omitted entirely when null: absence is the older-daemon case under test.
  ?configured == null ? null : 'configured': configured,
});

UpstreamAuth upstream(String status, List<AuthMethod> methods) =>
    UpstreamAuth(id: 'xai', status: status, methods: methods);

void main() {
  // MADR 0074 P21 step 3. The failure this guards against is subtle: an older
  // daemon sends no `configured` key, which decodes as false. Treating that as
  // "no method owns the credential" would hide the remove action on every
  // pre-transaction host.
  group('per-method credential state', () {
    test('an older daemon reports no method state', () {
      final up = upstream('configured', [
        method('xai:api'),
        method('xai:device'),
      ]);
      expect(up.hasMethodState, isFalse);
      expect(up.configuredMethods, isEmpty);
      expect(
        up.isExternallyManaged,
        isFalse,
        reason: 'absent state is not evidence of external management',
      );
    });

    test('a transactional daemon names the owning method', () {
      final up = upstream('configured', [
        method('xai:api', configured: true),
        method('xai:device', configured: false),
      ]);
      expect(up.hasMethodState, isTrue);
      expect(up.configuredMethods.map((m) => m.id), ['xai:api']);
      expect(up.isExternallyManaged, isFalse);
    });

    test('grok can hold both credentials at once', () {
      final up = upstream('configured', [
        method('xai:api', configured: true),
        method('xai:device', configured: true),
      ]);
      expect(up.configuredMethods, hasLength(2));
    });

    test('an externally managed credential offers no removal', () {
      // Configured upstream, daemon reported state, nothing it can remove:
      // Grok's XAI_API_KEY environment fallback.
      final up = upstream('configured', [
        method('xai:api', configured: false),
        method('xai:device', configured: false),
      ]);
      expect(up.hasMethodState, isTrue);
      expect(up.isExternallyManaged, isTrue);
    });

    test('an unconfigured upstream is never externally managed', () {
      final up = upstream('missing', [method('xai:api', configured: false)]);
      expect(up.isExternallyManaged, isFalse);
    });
  });

  group('transactional capability', () {
    test('absent reads as false', () {
      final caps = ServerCaps.tryParse(<String, dynamic>{'protocol': 2});
      expect(caps?.providerAuthTransactions, isFalse);
    });

    test('advertised reads as true', () {
      final caps = ServerCaps.tryParse(<String, dynamic>{
        'protocol': 2,
        'provider_auth_transactions': true,
      });
      expect(caps?.providerAuthTransactions, isTrue);
    });

    test('provider auth and transactions are independent', () {
      final caps = ServerCaps.tryParse(<String, dynamic>{
        'protocol': 2,
        'provider_auth': true,
      });
      expect(caps?.providerAuth, isTrue);
      expect(
        caps?.providerAuthTransactions,
        isFalse,
        reason: 'a host may report auth without running transactions',
      );
    });
  });
}
