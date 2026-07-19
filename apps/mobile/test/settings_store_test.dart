import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';

void main() {
  test('normalizeWsUrl host:port', () {
    expect(
      SettingsStore.normalizeWsUrl('10.0.2.2:7531'),
      'ws://10.0.2.2:7531/v1/ws',
    );
  });

  test('normalizeWsUrl host only', () {
    expect(
      SettingsStore.normalizeWsUrl('devbox'),
      'ws://devbox:7531/v1/ws',
    );
  });

  test('normalizeWsUrl full path', () {
    expect(
      SettingsStore.normalizeWsUrl('ws://h:7531/v1/ws'),
      'ws://h:7531/v1/ws',
    );
  });

  test('httpBaseFromWs', () {
    expect(
      SettingsStore.httpBaseFromWs('ws://10.0.2.2:7531/v1/ws'),
      'http://10.0.2.2:7531',
    );
  });
}
