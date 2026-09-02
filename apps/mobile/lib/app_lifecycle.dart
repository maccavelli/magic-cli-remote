import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/foundation.dart' show defaultTargetPlatform;
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'data/diagnostics/error_recorder.dart';
import 'data/notifications/agent_notifications.dart';
import 'data/notifications/notification_coordinator.dart';
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

  /// When the app was last backgrounded; null while foregrounded. Feeds the
  /// generation-bump gate in [_retryNow] (0068 P5/T3).
  DateTime? _backgroundedAt;

  /// A connectivity event arrived since the last dial — real evidence the
  /// network may have changed, unlike a mere resume (0068 P5/T3).
  bool _connectivityChangedSinceDial = false;

  /// Captured at init so the background park can run even when the widget
  /// is already unmounting (0068 P5 / A1 finding 13): `ref` dies with the
  /// element, and the `!mounted` early-return used to skip the park
  /// entirely — leaving the socket and its timers to suspend live.
  late final McremoteClient _client;
  late final NotificationCoordinator _coord;

  /// Captured with the others: a reconnect can fail while unmounting.
  late final ErrorRecorder _recorder;

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
    _coord = coord;
    _client = ref.read(mcremoteClientProvider);
    _recorder = ref.read(errorRecorderProvider);
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
        .catchError((Object e, StackTrace st) {
          // A broken prefs read must not silently kill the whole notification
          // layer: fall back to enabled (the shipped default) and start anyway.
          // Recorded as well as printed (MADR 0084 A3): the user's alert
          // preferences silently reverting to defaults is user-visible.
          debugPrint('notifications pref read failed: $e');
          // `_recorder`, not `ref` (MADR 0126 F6/D7). This handler exists to
          // survive a preferences failure, and it can run after the scope is
          // disposed — at which point `ref.read` throws, inside the very
          // handler meant to keep that failure from killing the notification
          // layer. The captured references above exist for exactly this.
          unawaited(_recorder.record(e, st, source: ErrorSource.app));
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
      _connectivityChangedSinceDial = true;
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
        // Platform-gated (0068 P5/T2): connectivity_plus reports `other`
        // rather than `vpn` on Apple platforms, so `!contains(vpn)` was
        // *always* true there — every blip on a mesh session fired the 2s
        // urgent probe and could tear down a healthy session on a slow
        // cellular leg. Absence of a signal the platform never emits is
        // not evidence; Apple platforms always use the lenient probe.
        final meshLostItsInterface = vpnSignalMeaningful(
          defaultTargetPlatform,
          sawVpn: results.contains(ConnectivityResult.vpn),
          onMesh: client.activeTransport == TransportMode.mesh,
        );
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
    _backgroundedAt = DateTime.now();
    _debounce?.cancel();
    _debounce = null;
    // Deliberately no `mounted` guard around the park (0068 P5 / A1
    // finding 13): the captured references outlive the element, and a
    // socket left live into suspension resumes as an overdue-keepalive
    // close burst. `ref` is not touched here.
    _coord.setAppForegrounded(false);
    if (!shouldParkOnBackground(
      _client.state,
      userLoggedOut: _client.userLoggedOut,
      notificationsEnabled: _coord.enabled,
      platform: defaultTargetPlatform,
    )) {
      return;
    }
    // manual: false keeps the pairing intact; _onResume brings the socket
    // back.
    unawaited(
      _client.disconnect(manual: false).catchError((Object e) {
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
      // A fresh transport-fallback budget (MADR 0062 D11) is granted only
      // on real evidence the network changed (0068 P5/T3): a connectivity
      // event since the last dial, or a background gap long enough for the
      // network to have moved. On iOS every app switch is a resume —
      // bumping per resume refreshed the budget constantly and repealed
      // D11's anti-thrash property. Bumping *inside* the 350ms debounce
      // still collapses a callback storm into one generation.
      final gap = _backgroundedAt == null
          ? Duration.zero
          : DateTime.now().difference(_backgroundedAt!);
      if (_connectivityChangedSinceDial || gap > const Duration(seconds: 60)) {
        client.bumpNetworkGeneration();
      }
      _connectivityChangedSinceDial = false;
      _backgroundedAt = null;
      unawaited(
        // Take the connection back from the service isolate first (MADR 0129
        // D2/C1). While this UI isolate was dead the service may have been
        // holding the socket; dialling without waiting for its release would
        // put two connections on one device token, and the daemon answers that
        // by closing one with 4001.
        _coord
            .claimForegroundOwnership()
            .then((_) => client.reconnectFromStore(store))
            .catchError((Object e, StackTrace st) {
              // A resume that cannot get the socket back is exactly the "it just
              // stopped working" report this diary exists for (MADR 0084 A3).
              debugPrint('ConnectionLifecycle reconnect: $e');
              unawaited(_recorder.record(e, st, source: ErrorSource.app));
            }),
      );
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
