import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'data/ws/lifecycle_policy.dart';
import 'state/app_providers.dart';
import 'state/transcripts_notifier.dart';

/// Watches app lifecycle and reconnects the WebSocket when the app resumes
/// (if credentials exist and the user has not logged out).
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
    _listener = AppLifecycleListener(onResume: _onResume);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _listener?.dispose();
    super.dispose();
  }

  void _onResume() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 400), () {
      if (!mounted) return;
      final client = ref.read(mcremoteClientProvider);
      if (!shouldReconnectOnResume(
        client.state,
        hasCredentials: client.hasCredentials,
        userLoggedOut: client.userLoggedOut,
      )) {
        return;
      }
      unawaited(client.reconnect().catchError((_) {}));
    });
  }

  @override
  Widget build(BuildContext context) {
    // Keep transcripts subscribed for the app lifetime.
    ref.watch(transcriptsProvider);
    return widget.child;
  }
}
