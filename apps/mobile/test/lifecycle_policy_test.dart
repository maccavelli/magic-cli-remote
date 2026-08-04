import 'package:flutter/foundation.dart' show TargetPlatform;
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/lifecycle_policy.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

void main() {
  // MADR 0067 D2 (U1) — iOS has no foreground service: the process suspends
  // and armed backoff timers never fire, so backgrounding always parks the
  // socket. Android semantics are unchanged byte-for-byte.
  group('shouldParkOnBackground', () {
    test('iOS parks from every state while signed in', () {
      for (final s in McConnectionState.values) {
        for (final alerts in [true, false]) {
          expect(
            shouldParkOnBackground(
              s,
              userLoggedOut: false,
              notificationsEnabled: alerts,
              platform: TargetPlatform.iOS,
            ),
            isTrue,
            reason: '$s alerts=$alerts',
          );
        }
      }
    });

    test('nothing to park after sign-out, on any platform', () {
      for (final p in [TargetPlatform.iOS, TargetPlatform.android]) {
        expect(
          shouldParkOnBackground(
            McConnectionState.error,
            userLoggedOut: true,
            notificationsEnabled: false,
            platform: p,
          ),
          isFalse,
          reason: '$p',
        );
      }
    });

    test('Android leaves a live or in-flight socket alone', () {
      for (final s in [
        McConnectionState.connected,
        McConnectionState.connecting,
        McConnectionState.authenticating,
        McConnectionState.disconnected,
      ]) {
        for (final alerts in [true, false]) {
          expect(
            shouldParkOnBackground(
              s,
              userLoggedOut: false,
              notificationsEnabled: alerts,
              platform: TargetPlatform.android,
            ),
            isFalse,
            reason: '$s alerts=$alerts',
          );
        }
      }
    });

    test('Android with alerts on keeps the retry loop for the service', () {
      for (final s in [
        McConnectionState.reconnecting,
        McConnectionState.error,
      ]) {
        expect(
          shouldParkOnBackground(
            s,
            userLoggedOut: false,
            notificationsEnabled: true,
            platform: TargetPlatform.android,
          ),
          isFalse,
          reason: '$s',
        );
      }
    });

    test('Android with alerts off parks the retry loop', () {
      for (final s in [
        McConnectionState.reconnecting,
        McConnectionState.error,
      ]) {
        expect(
          shouldParkOnBackground(
            s,
            userLoggedOut: false,
            notificationsEnabled: false,
            platform: TargetPlatform.android,
          ),
          isTrue,
          reason: '$s',
        );
      }
    });

    // Coherence: D2's park runs disconnect(manual: false), which lands in
    // `disconnected` — and that must be exactly a state the resume policy
    // reconnects from, or a backgrounded iOS app would never come back.
    test('iOS park lands in a state resume reconnects from', () {
      expect(
        shouldParkOnBackground(
          McConnectionState.connected,
          userLoggedOut: false,
          notificationsEnabled: true,
          platform: TargetPlatform.iOS,
        ),
        isTrue,
      );
      expect(
        shouldReconnectOnResume(
          McConnectionState.disconnected,
          hasCredentials: true,
          userLoggedOut: false,
        ),
        isTrue,
      );
    });
  });

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

  // `reconnecting` stays false here by design: the policy only answers "is a
  // fresh connect needed?". Resuming mid-backoff is handled in
  // ConnectionLifecycleScope._onResume, which collapses the pending retry
  // instead of waiting out the remaining (up to 30s) delay.
  test('skip when already in flight or connected', () {
    for (final s in [
      McConnectionState.connected,
      McConnectionState.connecting,
      McConnectionState.authenticating,
      McConnectionState.reconnecting,
    ]) {
      expect(
        shouldReconnectOnResume(s, hasCredentials: true, userLoggedOut: false),
        isFalse,
        reason: '$s',
      );
    }
  });

  // MADR 0062 D11 — the network generation is what scopes the transport
  // fallback budget. The lifecycle bumps it inside the 350ms `_retryNow`
  // debounce, so a burst of connectivity callbacks shares one generation (and
  // therefore one mesh↔relay hop) while genuinely separate resumes each get
  // their own. The budget arithmetic itself is asserted in
  // dial_episode_test.dart; this covers the counter's own contract.
  group('network generation', () {
    test('each bump is a new generation, and bumps are cheap and safe', () {
      final client = McremoteClient();
      addTearDown(client.dispose);
      // No connection required: the counter must be usable from a lifecycle
      // callback that fires before anything has ever been dialled.
      expect(() {
        for (var i = 0; i < 100; i++) {
          client.bumpNetworkGeneration();
        }
      }, returnsNormally);
    });

    test('activeTransport is null until a socket is actually up', () {
      final client = McremoteClient();
      addTearDown(client.dispose);
      // Guards the settings/status readout: reporting a transport for a dead
      // connection would show a route the user does not have.
      expect(client.activeTransport, isNull);
      expect(client.lastTransportSuccess, isNull);
      expect(client.lastDialSpentCredential, isFalse);
    });
  });
}

// reconnectFromStore / slash commands covered by integration; pure policy above.
