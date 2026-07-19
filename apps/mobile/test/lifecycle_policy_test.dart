import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/lifecycle_policy.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

void main() {
  test('no reconnect without credentials', () {
    expect(
      shouldReconnectOnResume(
        McConnectionState.disconnected,
        hasCredentials: false,
        userLoggedOut: false,
      ),
      isFalse,
    );
  });

  test('no reconnect when user logged out', () {
    expect(
      shouldReconnectOnResume(
        McConnectionState.error,
        hasCredentials: true,
        userLoggedOut: true,
      ),
      isFalse,
    );
  });

  test('reconnect from disconnected/error when eligible', () {
    expect(
      shouldReconnectOnResume(
        McConnectionState.disconnected,
        hasCredentials: true,
        userLoggedOut: false,
      ),
      isTrue,
    );
    expect(
      shouldReconnectOnResume(
        McConnectionState.error,
        hasCredentials: true,
        userLoggedOut: false,
      ),
      isTrue,
    );
  });

  test('skip when already in flight or connected', () {
    for (final s in [
      McConnectionState.connected,
      McConnectionState.connecting,
      McConnectionState.authenticating,
      McConnectionState.reconnecting,
    ]) {
      expect(
        shouldReconnectOnResume(
          s,
          hasCredentials: true,
          userLoggedOut: false,
        ),
        isFalse,
        reason: '$s',
      );
    }
  });
}
