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

  test('normalizeWsUrl strips http scheme (no ws://http:// bug)', () {
    expect(
      SettingsStore.normalizeWsUrl('http://100.64.0.1:7531'),
      'ws://100.64.0.1:7531/v1/ws',
    );
    expect(
      SettingsStore.normalizeWsUrl('https://100.64.0.1:7531'),
      'wss://100.64.0.1:7531/v1/ws',
    );
  });

  test('normalizeWsUrl strips pasted path', () {
    expect(
      SettingsStore.normalizeWsUrl('100.64.0.1:7531/v1/ws'),
      'ws://100.64.0.1:7531/v1/ws',
    );
    expect(
      SettingsStore.normalizeWsUrl('100.64.0.1:7531/healthz'),
      'ws://100.64.0.1:7531/v1/ws',
    );
  });

  test('httpBaseFromWs', () {
    expect(
      SettingsStore.httpBaseFromWs('ws://10.0.2.2:7531/v1/ws'),
      'http://10.0.2.2:7531',
    );
  });

  test('healthzUrl uses port 7531 not a garbage concatenation', () {
    expect(
      SettingsStore.healthzUrl('100.64.0.1:7531'),
      'http://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('100.64.0.1'),
      'http://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('ws://100.64.0.1:7531/v1/ws'),
      'http://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('http://100.64.0.1:7531'),
      'http://100.64.0.1:7531/healthz',
    );
  });

  test('parseEndpoint rejects invalid ports', () {
    expect(
      () => SettingsStore.parseEndpoint('host:99999'),
      throwsA(isA<ArgumentError>()),
    );
    expect(
      () => SettingsStore.parseEndpoint('host:0'),
      throwsA(isA<ArgumentError>()),
    );
  });

  test('IPv6 with brackets', () {
    expect(
      SettingsStore.healthzUrl('[fd7a:115c:a1e0::1]:7531'),
      'http://[fd7a:115c:a1e0::1]:7531/healthz',
    );
  });
}
