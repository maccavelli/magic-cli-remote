import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'data/ws/lifecycle_policy.dart';
import 'state/app_providers.dart';
import 'state/transcripts_notifier.dart';

/// Watches app lifecycle and reconnects the WebSocket when the app resumes
/// after screen lock / background (until the user explicitly signs out).
///
/// Also keeps [transcriptsProvider] alive for the app lifetime.
class ConnectionLifecycleScope extends ConsumerStatefulWidget {
  const ConnectionLifecycleScope({super.key, required this.child});

  final Widget child;

  @override
  ConsumerState<ConnectionLifecycleScope> createState() =>
      _ConnectionLifecycleScopeState();
}

class _ConnectionLifecycleScopeState
    extends ConsumerState<ConnectionLifecycleScope> {
  AppLifecycleListener? _listener;
  Timer? _debounce;

  @override
  void initState() {
    super.initState();
    // Initialise transcripts once so TranscriptsNotifier.build() runs its
    // ref.keepAlive() and the event subscription lives for the app lifetime.
    // (A watch in build() would rebuild this scope on every streamed token.)
    ref.read(transcriptsProvider);
    _listener = AppLifecycleListener(
      onResume: _onResume,
      // Some Android builds only deliver inactive/hidden around lock; treat
      // return to resumed as the reconnect trigger (handled in onResume).
      onShow: _onResume,
      // Mirror image: stop retry work while we are off-screen.
      onPause: _onBackground,
      onHide: _onBackground,
    );
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _listener?.dispose();
    super.dispose();
  }

  /// Backgrounded: cancel any pending resume work and stop the reconnect
  /// backoff loop. Retrying on an exponential timer while suspended burns
  /// radio/battery and produces a burst of state churn on the next resume.
  /// A live socket is left alone — resume then costs nothing.
  void _onBackground() {
    _debounce?.cancel();
    _debounce = null;
    if (!mounted) return;
    final client = ref.read(mcremoteClientProvider);
    if (client.userLoggedOut) return;
    if (client.state != McConnectionState.reconnecting &&
        client.state != McConnectionState.error) {
      return;
    }
    // manual: false keeps the pairing (and auto-reconnect eligibility) intact;
    // _onResume brings the socket back from `disconnected`.
    unawaited(
      client.disconnect(manual: false).catchError((Object e) {
        debugPrint('ConnectionLifecycle suspend: $e');
      }),
    );
  }

  void _onResume() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 350), () {
      if (!mounted) return;
      final client = ref.read(mcremoteClientProvider);
      if (client.userLoggedOut) return;

      final eligible = client.hasCredentials || client.isPaired;
      // Resuming mid-backoff: the policy reports `reconnecting` as "already in
      // flight", but the pending retry can be up to 30s away while the network
      // is already back. Collapse the backoff and retry now —
      // reconnectFromStore() cancels the timer and resets the attempt count.
      final duringBackoff =
          eligible && client.state == McConnectionState.reconnecting;

      // Already up or connecting — nothing to do.
      if (!duringBackoff &&
          !shouldReconnectOnResume(
            client.state,
            hasCredentials: eligible,
            userLoggedOut: client.userLoggedOut,
          )) {
        return;
      }

      final store = ref.read(settingsStoreProvider);
      unawaited(
        client.reconnectFromStore(store).catchError((Object e) {
          debugPrint('ConnectionLifecycle reconnect: $e');
        }),
      );
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
