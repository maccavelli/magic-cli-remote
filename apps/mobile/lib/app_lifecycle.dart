import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
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
  StreamSubscription<List<ConnectivityResult>>? _connectivitySub;
  Timer? _debounce;

  /// True while the app is actually on screen. Connectivity callbacks fire
  /// while backgrounded (the foreground service keeps the process alive for
  /// exactly that), and they must never be allowed to fake "foregrounded" —
  /// that would suppress the notifications the user backgrounded us to get.
  bool _isForeground = true;

  @override
  void initState() {
    super.initState();
    // Initialise transcripts once so TranscriptsNotifier.build() runs its
    // ref.keepAlive() and the event subscription lives for the app lifetime.
    // (A watch in build() would rebuild this scope on every streamed token.)
    ref.read(transcriptsProvider);
    // Start the notification + foreground-service layer for the app lifetime,
    // honouring the persisted on/off preference.
    final coord = ref.read(notificationCoordinatorProvider);
    ref
        .read(settingsStoreProvider)
        .getNotificationsEnabled()
        .then((v) {
          coord.enabled = v;
          unawaited(coord.start());
          // start() only listens for *future* connection transitions; a fast
          // auto-connect that completed before this pref read resolved would
          // otherwise leave the keep-alive service off until the next blip.
          // setEnabled syncs the service to the connection's current state.
          unawaited(coord.setEnabled(v));
        })
        .catchError((Object e) {
          // A broken prefs read must not silently kill the whole notification
          // layer: fall back to enabled (the shipped default) and start anyway.
          debugPrint('notifications pref read failed: $e');
          coord.enabled = true;
          unawaited(coord.start());
          unawaited(coord.setEnabled(true));
        });
    _listener = AppLifecycleListener(
      onResume: _onResume,
      // Some Android builds only deliver inactive/hidden around lock; treat
      // return to resumed as the reconnect trigger (handled in onResume).
      onShow: _onResume,
      // Mirror image: stop retry work while we are off-screen.
      onPause: _onBackground,
      onHide: _onBackground,
    );

    _connectivitySub = Connectivity().onConnectivityChanged.listen((results) {
      if (results.contains(ConnectivityResult.none)) return;
      final client = ref.read(mcremoteClientProvider);
      if (client.state == McConnectionState.connected) {
        // Interfaces changed while connected. On this app that is routinely
        // Tailscale/VPN churn or a secondary interface appearing — the socket
        // is usually still fine. Probe liveness instead of bouncing a healthy
        // connection; the ping path reconnects if the link is really dead.
        unawaited(client.probeLiveness());
      } else if (client.state == McConnectionState.reconnecting ||
          client.state == McConnectionState.error) {
        // Network is back: collapse the backoff and retry now. This runs in
        // the background too (that is the point of the foreground service) —
        // but must not touch the foregrounded flag.
        _retryNow();
      }
    });
  }

  @override
  void dispose() {
    _connectivitySub?.cancel();
    _debounce?.cancel();
    _listener?.dispose();
    super.dispose();
  }

  /// Backgrounded: cancel any pending resume work and stop the reconnect
  /// backoff loop. Retrying on an exponential timer while suspended burns
  /// radio/battery and produces a burst of state churn on the next resume.
  /// A live socket is left alone — resume then costs nothing.
  void _onBackground() {
    _isForeground = false;
    _debounce?.cancel();
    _debounce = null;
    if (!mounted) return;
    // Off-screen: notifications become worthwhile again for every session.
    final coord = ref.read(notificationCoordinatorProvider);
    coord.setAppForegrounded(false);
    final client = ref.read(mcremoteClientProvider);
    if (client.userLoggedOut) return;
    if (client.state != McConnectionState.reconnecting &&
        client.state != McConnectionState.error) {
      return;
    }
    // With alerts on, the whole point of the foreground service is to keep
    // the link alive off-screen: parking to `disconnected` here would stop
    // the service and nothing would ever reconnect in the background.
    if (coord.enabled) return;
    // Alerts off: stop the backoff loop to save radio/battery. manual: false
    // keeps the pairing intact; _onResume brings the socket back.
    unawaited(
      client.disconnect(manual: false).catchError((Object e) {
        debugPrint('ConnectionLifecycle suspend: $e');
      }),
    );
  }

  void _onResume() {
    _isForeground = true;
    if (mounted) {
      ref.read(notificationCoordinatorProvider).setAppForegrounded(true);
    }
    _retryNow();
  }

  /// Debounced "get the socket back up" — shared by real resumes and
  /// background connectivity restoration. Does not touch the foreground flag.
  void _retryNow() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 350), () {
      if (!mounted) return;
      final client = ref.read(mcremoteClientProvider);
      if (client.userLoggedOut) return;

      final eligible = client.hasCredentials || client.isPaired;
      // Mid-backoff: the policy reports `reconnecting` as "already in
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
        // Freshly foregrounded onto a socket that thinks it is connected: a
        // blackholed link would otherwise take up to a minute of ping misses
        // to notice. One cheap probe settles it now.
        if (_isForeground && client.state == McConnectionState.connected) {
          unawaited(client.probeLiveness());
        }
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
