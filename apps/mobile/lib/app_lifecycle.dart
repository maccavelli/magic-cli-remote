import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/foundation.dart' show defaultTargetPlatform;
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'data/notifications/agent_notifications.dart';
import 'data/ws/lifecycle_policy.dart';
import 'state/app_providers.dart';
import 'state/session_synchronizer.dart';
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
    // Connection-scoped resync (MADR 0056 H-1): keep alive for app lifetime.
    ref.read(sessionSynchronizerProvider);
    // Resume claims (MADR 0068 D4): the client includes the transcripts'
    // per-session seqs in a within-window reconnect's auth, so an
    // unchanged daemon can confirm "nothing missed" without a reconcile.
    ref.read(mcremoteClientProvider).resumeSeqSource = () =>
        ref.read(transcriptsProvider.notifier).lastSeqSnapshot();
    // Start the notification + foreground-service layer for the app lifetime,
    // honouring the persisted on/off preference.
    final coord = ref.read(notificationCoordinatorProvider);
    final store = ref.read(settingsStoreProvider);
    Future.wait([
          store.getNotificationsEnabled(),
          store.getNotifyAsks(),
          store.getNotifyTurnComplete(),
          store.getNotifyErrors(),
        ])
        .then((values) {
          final v = values[0];
          coord.enabled = v;
          coord.kinds = NotifyKinds(
            asks: values[1],
            turnComplete: values[2],
            errors: values[3],
          );
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
          coord.kinds = NotifyKinds.all;
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
        //
        // One case is not routine: the VPN interface disappearing while the
        // session is riding the *mesh*. That is the most specific evidence
        // available that this particular transport just died, so it gets a
        // short, urgent probe rather than the lenient one (MADR 0063 D4).
        //
        // Treated as an accelerator, never a precondition: connectivity_plus
        // documents that Apple platforms report `other` rather than `vpn`, so
        // where the signal is absent the ordinary detection path still applies.
        final meshLostItsInterface =
            client.activeTransport == TransportMode.mesh &&
            !results.contains(ConnectivityResult.vpn);
        unawaited(client.probeLiveness(urgent: meshLostItsInterface));
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

  /// Backgrounded: cancel any pending resume work and settle the socket per
  /// [shouldParkOnBackground]. On Android a live socket is left alone (the
  /// foreground service keeps the process running, and with alerts on it
  /// deliberately keeps the retry loop too — parking would stop the service
  /// and nothing would ever reconnect off-screen). On iOS the process
  /// suspends and the OS reclaims sockets, so the link is always parked and
  /// _onResume brings it back (MADR 0067 D2).
  void _onBackground() {
    _isForeground = false;
    _debounce?.cancel();
    _debounce = null;
    if (!mounted) return;
    // Off-screen: notifications become worthwhile again for every session.
    final coord = ref.read(notificationCoordinatorProvider);
    coord.setAppForegrounded(false);
    final client = ref.read(mcremoteClientProvider);
    if (!shouldParkOnBackground(
      client.state,
      userLoggedOut: client.userLoggedOut,
      notificationsEnabled: coord.enabled,
      platform: defaultTargetPlatform,
    )) {
      return;
    }
    // manual: false keeps the pairing intact; _onResume brings the socket
    // back.
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
      // A resume or a restored link means the network may be a different one,
      // so this reconnect gets a fresh transport-fallback budget
      // (MADR 0062 D11). Bumping *inside* the 350ms debounce is what makes the
      // budget survive a storm: a burst of connectivity callbacks collapses
      // into one `_retryNow` body, so it shares one generation and therefore
      // one mesh↔relay hop. Bumping per callback would hand every flap in an
      // airplane-mode toggle its own hop, which is the thrash D11 forbids.
      client.bumpNetworkGeneration();
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
