import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

void main() {
  test('ProviderInfo.fromJson treats missing prewarm as unknown', () {
    final p = ProviderInfo.fromJson({'id': 'kilo', 'ready': true});
    expect(p.prewarm, isNull);
  });

  test('ProviderInfo.fromJson reads false as false not unknown', () {
    final p = ProviderInfo.fromJson({
      'id': 'kilo',
      'ready': true,
      'prewarm': false,
    });
    expect(p.prewarm, isFalse);
  });

  test('ProviderInfo.fromJson reads true', () {
    final p = ProviderInfo.fromJson({
      'id': 'kilo',
      'ready': true,
      'prewarm': true,
    });
    expect(p.prewarm, isTrue);
  });
}
