import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';

void main() {
  test('parses mcremote pair URI with token', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=100.64.0.1%3A7531&token=mcr_deadbeef',
    );
    expect(p, isNotNull);
    expect(p!.host, '100.64.0.1:7531');
    expect(p.token, 'mcr_deadbeef');
    expect(p.hasToken, isTrue);
  });

  test('parses pair URI with short code', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=100.64.0.1%3A7531&code=K7M2-9X4P',
    );
    expect(p, isNotNull);
    expect(p!.code, 'K7M2-9X4P');
    expect(p.hasCode, isTrue);
    expect(p.hasToken, isFalse);
  });

  test('rejects non-pair payloads', () {
    expect(PairPayload.tryParse(''), isNull);
    expect(PairPayload.tryParse('mcr_only'), isNull);
    expect(PairPayload.tryParse('https://evil/pair?host=a&token=b'), isNull);
    expect(PairPayload.tryParse('mcremote://pair?host=a'), isNull);
  });

  test('strips schemes from host param', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=ws%3A%2F%2F100.64.0.1%3A7531%2Fv1%2Fws&token=mcr_x',
    );
    expect(p!.host, '100.64.0.1:7531');
  });

  test('looksLikePairCode and format', () {
    expect(PairPayload.looksLikePairCode('K7M2-9X4P'), isTrue);
    expect(PairPayload.looksLikePairCode('k7m29x4p'), isTrue);
    expect(PairPayload.looksLikePairCode('mcr_abc'), isFalse);
    expect(PairPayload.formatPairCode('k7m29x4p'), 'K7M2-9X4P');
  });
}
