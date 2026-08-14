/// Device sign-in display for an RFC 8628 flow (MADR 0074 Strategy A, D13).
///
/// The phone's whole job here is to show a URL and a code and wait: the host
/// polls the vendor and stores the credential. Nothing secret passes through
/// this screen.
///
/// D13 chose the system browser plus a displayed code over an in-app listener,
/// because a device flow has no callback to catch. The URL is a one-tap
/// open (url_launcher, MADR 0086 D7) with copy as a secondary action.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../widgets/vendor_icon.dart';
import 'provider_auth_sheet.dart' show bottomInsetFor;

/// Opens a URI in an external application. Tests inject a fake.
typedef DeviceUrlLauncher = Future<bool> Function(Uri uri);

/// What the sheet needs to display a flow, mirroring `oauth.device_flow`.
class DeviceFlowInfo {
  const DeviceFlowInfo({
    required this.flowId,
    required this.providerId,
    required this.verificationUri,
    required this.userCode,
    this.expiresIn = 0,
  });

  final String flowId;
  final String providerId;
  final String verificationUri;
  final String userCode;
  final int expiresIn;

  factory DeviceFlowInfo.fromJson(Map<String, dynamic> j) => DeviceFlowInfo(
    flowId: j['flow_id'] as String? ?? '',
    providerId: j['provider_id'] as String? ?? '',
    verificationUri: j['verification_uri'] as String? ?? '',
    userCode: j['user_code'] as String? ?? '',
    expiresIn: (j['expires_in'] as num?)?.toInt() ?? 0,
  );
}

class DeviceFlowSheet extends StatefulWidget {
  const DeviceFlowSheet({
    super.key,
    required this.flow,
    required this.onCancel,
    this.result,
    this.launchUrlFn,
  });

  final DeviceFlowInfo flow;

  /// Called when the user dismisses the sheet before the flow completes, so
  /// the daemon can stop polling on behalf of nobody.
  final Future<void> Function() onCancel;

  /// Completes when the daemon reports the outcome. Null keeps the sheet in
  /// its waiting state indefinitely (used by tests).
  final Future<String?>? result;

  /// Override for tests. Production uses [launchUrl] in external-application
  /// mode (MADR 0086 D7).
  final DeviceUrlLauncher? launchUrlFn;

  @override
  State<DeviceFlowSheet> createState() => _DeviceFlowSheetState();
}

class _DeviceFlowSheetState extends State<DeviceFlowSheet> {
  Timer? _tick;
  late int _remaining = widget.flow.expiresIn;
  String? _outcome;
  bool _done = false;

  @override
  void initState() {
    super.initState();
    if (_remaining > 0) {
      _tick = Timer.periodic(const Duration(seconds: 1), (_) {
        if (!mounted) return;
        setState(() => _remaining = _remaining > 0 ? _remaining - 1 : 0);
      });
    }
    widget.result?.then((err) {
      if (!mounted) return;
      setState(() {
        _done = true;
        _outcome = err;
        _tick?.cancel();
      });
    });
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  String get _countdown {
    if (_remaining <= 0) return 'expired';
    final m = _remaining ~/ 60;
    final s = _remaining % 60;
    return '${m}m ${s.toString().padLeft(2, '0')}s left';
  }

  Future<void> _openUri() async {
    final raw = widget.flow.verificationUri.trim();
    final uri = Uri.tryParse(raw);
    if (uri == null || !uri.hasScheme) {
      if (!mounted) return;
      ScaffoldMessenger.maybeOf(
        context,
      )?.showSnackBar(const SnackBar(content: Text('Could not open the link')));
      return;
    }
    final launcher =
        widget.launchUrlFn ??
        ((u) => launchUrl(u, mode: LaunchMode.externalApplication));
    final ok = await launcher(uri);
    if (!ok && mounted) {
      ScaffoldMessenger.maybeOf(
        context,
      )?.showSnackBar(const SnackBar(content: Text('Could not open the link')));
    }
  }

  Future<void> _copy(String value, String what) async {
    await Clipboard.setData(ClipboardData(text: value));
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(
      context,
    )?.showSnackBar(SnackBar(content: Text('$what copied')));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      // Same system-bar clearance as the credential sheet: without it the
      // Cancel button sits under the Android navigation bar.
      padding: EdgeInsets.fromLTRB(
        16,
        16,
        16,
        bottomInsetFor(MediaQuery.of(context)) + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              VendorIcon(id: widget.flow.providerId, size: 28),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  'Sign in to ${widget.flow.providerId}',
                  style: theme.textTheme.titleMedium,
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          if (_done) ...[
            Text(
              _outcome == null ? 'Signed in.' : 'Sign-in failed: $_outcome',
              key: const Key('device-flow-outcome'),
              style: theme.textTheme.bodyLarge,
            ),
          ] else ...[
            const Text('1. Open this link on any device:'),
            const SizedBox(height: 4),
            SelectableText(
              widget.flow.verificationUri,
              key: const Key('device-flow-uri'),
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.primary,
              ),
            ),
            TextButton.icon(
              key: const Key('device-flow-open-uri'),
              icon: const Icon(Icons.open_in_browser, size: 16),
              label: const Text('Open link'),
              onPressed: _openUri,
            ),
            TextButton.icon(
              key: const Key('device-flow-copy-uri'),
              icon: const Icon(Icons.copy, size: 16),
              label: const Text('Copy link'),
              onPressed: () => _copy(widget.flow.verificationUri, 'Link'),
            ),
            const SizedBox(height: 12),
            const Text('2. Enter this code:'),
            const SizedBox(height: 4),
            // Monospaced and large: this is read off the screen and typed
            // into another device, often by someone holding both.
            SelectableText(
              widget.flow.userCode,
              key: const Key('device-flow-code'),
              style: theme.textTheme.headlineSmall?.copyWith(
                fontFamily: 'monospace',
                letterSpacing: 2,
              ),
            ),
            TextButton.icon(
              key: const Key('device-flow-copy-code'),
              icon: const Icon(Icons.copy, size: 16),
              label: const Text('Copy code'),
              onPressed: () => _copy(widget.flow.userCode, 'Code'),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                const SizedBox(width: 8),
                Text(
                  'Waiting · $_countdown',
                  key: const Key('device-flow-countdown'),
                ),
              ],
            ),
          ],
          const SizedBox(height: 16),
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(
              key: const Key('device-flow-dismiss'),
              onPressed: () async {
                if (!_done) await widget.onCancel();
                if (context.mounted) Navigator.of(context).pop();
              },
              child: Text(_done ? 'Close' : 'Cancel'),
            ),
          ),
        ],
      ),
    );
  }
}

/// The MADR 0074 D8 confirmation. Codex's device flow deletes the host's
/// existing credential the moment it starts — before the user has entered
/// anything — so the consequence is named in full rather than softened.
Future<bool> confirmDestructiveSignIn(
  BuildContext context,
  String providerId,
) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      key: const Key('destructive-signin-dialog'),
      title: Text('Sign out of $providerId first?'),
      content: const Text(
        'This signs the host out immediately, before you finish signing in. '
        'If you do not complete the sign-in, the host is left signed out.',
      ),
      actions: [
        TextButton(
          key: const Key('destructive-signin-cancel'),
          onPressed: () => Navigator.of(ctx).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('destructive-signin-confirm'),
          onPressed: () => Navigator.of(ctx).pop(true),
          child: const Text('Continue'),
        ),
      ],
    ),
  );
  return ok ?? false;
}
