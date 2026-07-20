import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../data/local/settings_store.dart' show SecureStorageUnavailable;
import '../../data/protocol/pair_uri.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import 'qr_scan_screen.dart';

String _defaultHost() =>
    Platform.isAndroid ? '10.0.2.2:7531' : '127.0.0.1:7531';

class ConnectScreen extends ConsumerStatefulWidget {
  const ConnectScreen({super.key});

  @override
  ConsumerState<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends ConsumerState<ConnectScreen> {
  late final TextEditingController _hostCtrl =
      TextEditingController(text: _defaultHost());
  final _tokenCtrl = TextEditingController();
  bool _busy = false;
  bool _showToken = false;
  bool _advanced = false;
  bool _autoConnecting = false;
  String? _status;
  bool _statusIsError = false;
  bool _invalidToken = false;

  /// Certificate fingerprint from the most recent pair QR, if it carried one.
  ///
  /// Both modes advertise a pin (ADR 0004); what differs is the rule applied to
  /// it, which is why [_pendingTlsMode] travels with it. Reconnects re-read
  /// both from secure storage, so these are only for the first hop.
  String? _pendingFingerprint;

  /// The TLS mode from the same QR, selecting the acceptance rule for the pin.
  TlsMode? _pendingTlsMode;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final store = ref.read(settingsStoreProvider);
      final host = await store.getHost();
      final token = await store.getToken();
      if (!mounted) return;
      if (host != null && host.isNotEmpty) {
        _hostCtrl.text = host;
      }
      if (token != null && token.isNotEmpty) {
        _tokenCtrl.text = token;
      }
      // Cold-start auto-connect when credentials exist.
      // Skip only after an explicit sign-out this process lifetime.
      final client = ref.read(mcremoteClientProvider);
      if (client.userLoggedOut) {
        return;
      }
      if (host != null &&
          host.isNotEmpty &&
          token != null &&
          token.isNotEmpty) {
        if (client.state == McConnectionState.disconnected ||
            client.state == McConnectionState.error ||
            !client.isPaired) {
          setState(() {
            _autoConnecting = true;
            _status = 'Reconnecting with saved credentials…';
            _statusIsError = false;
          });
          try {
            await client.connect(hostInput: host, token: token);
            if (!mounted) return;
            context.go('/sessions');
          } catch (e) {
            if (!mounted) return;
            await _handleConnectFailure(e);
            if (mounted) setState(() => _autoConnecting = false);
          }
        } else if (client.isPaired ||
            client.state == McConnectionState.connected ||
            client.state == McConnectionState.reconnecting) {
          if (mounted) context.go('/sessions');
        }
      }
    } catch (e) {
      debugPrint('ConnectScreen._load: $e');
    }
  }

  @override
  void dispose() {
    _hostCtrl.dispose();
    _tokenCtrl.dispose();
    super.dispose();
  }

  String? _errorCode(Object e) {
    if (e is McException) return e.code;
    return ref.read(mcremoteClientProvider).lastErrorCode;
  }

  String _friendlyError(Object e) {
    // On mobile the store refuses to fall back to cleartext, so a keystore
    // failure surfaces here rather than silently writing an unencrypted token.
    if (e is SecureStorageUnavailable) {
      return 'This device\'s secure keystore is unavailable, so the token '
          'cannot be stored safely. Restart the device or re-enrol its screen '
          'lock, then pair again.';
    }
    final code = _errorCode(e);
    switch (code) {
      case 'invalid_token':
        return 'This device token is no longer valid. Generate a new pair '
            'code on the host and use Enter code or Scan QR.';
      case 'expired':
        return 'Pair code expired (5 min). Generate a new one on the host.';
      case 'invalid_code':
        return 'Invalid or already-used pair code.';
      case 'rate_limited':
        return 'Too many failed attempts. Wait and try a new code.';
      case 'client_key_required':
        return 'This device is not enrolled on the host — it has no client '
            'key on record. Re-pair with a fresh QR or pair code to enrol '
            'this device.';
      case 'client_key_mismatch':
        return 'This device\'s key does not match the one enrolled on the '
            'host. If you reinstalled the app or cleared its data, re-pair to '
            'enrol the new key.';
      case 'cert_mismatch':
        // The McException already carries the full rotation-vs-attack
        // explanation and both fingerprints; surface it verbatim.
        if (e is McException) return e.message;
        return 'The host\'s TLS certificate changed. Re-pair only if you '
            'expected it (host rebuild); otherwise the host may be spoofed.';
      case 'auth_timeout':
        return 'Timed out authenticating — check host and Tailscale mesh.';
      case 'connect_failed':
        return 'Could not reach host — check host and mesh.';
      case 'no_credentials':
        return 'No saved credentials. Enter a pair code or token.';
    }

    final s = e.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');
    if (s.contains('TimeoutException') || s.contains('timed out')) {
      return 'Timed out — check host and Tailscale mesh.';
    }
    if (s.contains('invalid_token') || s.contains('invalid device token')) {
      return 'This device token is no longer valid. Generate a new pair '
          'code on the host and use Enter code or Scan QR.';
    }
    if (s.contains('invalid pair code') || s.contains('invalid_code')) {
      return 'Invalid or already-used pair code.';
    }
    if (s.contains('expired')) {
      return 'Pair code expired (5 min). Generate a new one on the host.';
    }
    if (s.contains('rate_limited') || s.contains('too many failed')) {
      return 'Too many failed attempts. Wait and try a new code.';
    }
    return s;
  }

  bool _isInvalidTokenError(Object e) {
    if (e is McException && e.isInvalidToken) return true;
    final code = _errorCode(e);
    if (code == 'invalid_token') return true;
    final s = e.toString();
    return s.contains('invalid_token') || s.contains('invalid device token');
  }

  /// The recovery for a client-key error is the same as for a bad token —
  /// re-pair — so it drives the same affordance, but the token stays put: the
  /// token is fine, the device's enrolled key is not.
  bool _needsKeyEnrolment(Object e) {
    final code = _errorCode(e);
    return code == 'client_key_required' || code == 'client_key_mismatch';
  }

  Future<void> _handleConnectFailure(Object e) async {
    // Resolve everything that needs `ref` before the first await — this widget
    // may be disposed (invalid_token redirect) while clearToken() is in flight.
    final invalid = _isInvalidTokenError(e);
    final needsRepair = invalid || _needsKeyEnrolment(e);
    final message = _friendlyError(e);
    if (invalid) {
      final store = ref.read(settingsStoreProvider);
      ref.read(mcremoteClientProvider).clearMemoryCredentials();
      await store.clearToken();
      if (!mounted) return;
      _tokenCtrl.clear();
    }
    if (!mounted) return;
    setState(() {
      _status = message;
      _statusIsError = true;
      _invalidToken = needsRepair;
    });
  }

  Future<void> _applyPair(PairPayload payload) async {
    setState(() {
      // Show the bare authority; the fingerprint and its mode travel as real
      // parameters rather than riding inside the host string where the user
      // would see them.
      _hostCtrl.text = payload.hostAuthority;
      _pendingFingerprint = payload.fingerprint;
      _pendingTlsMode = payload.mode;
      if (payload.hasToken) {
        _tokenCtrl.text = payload.token!;
      }
      _status = payload.hasCode
          ? 'Pair code from QR — claiming…'
          : 'Filled from pair QR — connecting…';
      _statusIsError = false;
    });
    if (payload.hasCode) {
      await _claimCode(payload.code!);
    } else if (payload.hasToken) {
      await _connect();
    }
  }

  Future<void> _scanQr() async {
    final payload = await Navigator.of(context).push<PairPayload>(
      MaterialPageRoute(builder: (_) => const QrScanScreen()),
    );
    if (payload == null || !mounted) return;
    await _applyPair(payload);
  }

  Future<void> _pastePairUri() async {
    // Reading the clipboard is a platform round-trip; keep the button disabled
    // so a double tap cannot start two paste/claim flows.
    if (_busy) return;
    setState(() => _busy = true);
    final String text;
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      text = data?.text?.trim() ?? '';
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    if (!mounted) return;
    if (text.isEmpty) {
      setState(() {
        _status = 'Clipboard is empty';
        _statusIsError = true;
      });
      return;
    }
    final payload = PairPayload.tryParse(text);
    if (payload != null) {
      await _applyPair(payload);
      return;
    }
    if (text.startsWith('mcr_')) {
      setState(() {
        _tokenCtrl.text = text;
        _advanced = true;
        _status = 'Pasted token — tap Connect';
        _statusIsError = false;
      });
      return;
    }
    if (PairPayload.looksLikePairCode(text)) {
      await _claimCode(text);
      return;
    }
    setState(() {
      _status = 'Clipboard is not a pair URI, code, or mcr_ token';
      _statusIsError = true;
    });
  }

  Future<void> _enterCode() async {
    final codeCtrl = TextEditingController();
    final code = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) {
        return Padding(
          padding: EdgeInsets.only(
            left: 20,
            right: 20,
            top: 20,
            bottom: MediaQuery.of(ctx).viewInsets.bottom + 20,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('Enter pair code',
                  style: Theme.of(ctx).textTheme.titleLarge),
              const SizedBox(height: 8),
              Text(
                '8 characters from `mcremote pair code` on the host. '
                'Expires in 5 minutes.',
                style: Theme.of(ctx).textTheme.bodyMedium?.copyWith(
                      color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                    ),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: codeCtrl,
                autofocus: true,
                textCapitalization: TextCapitalization.characters,
                autocorrect: false,
                enableSuggestions: false,
                maxLength: 9, // XXXX-XXXX
                style: const TextStyle(
                  letterSpacing: 2,
                  fontSize: 22,
                  fontWeight: FontWeight.w600,
                  fontFamily: 'monospace',
                ),
                textAlign: TextAlign.center,
                decoration: const InputDecoration(
                  hintText: 'XXXX-XXXX',
                  border: OutlineInputBorder(),
                  counterText: '',
                ),
                inputFormatters: [
                  _PairCodeFormatter(),
                ],
                onSubmitted: (v) => Navigator.pop(ctx, v),
              ),
              const SizedBox(height: 16),
              FilledButton(
                onPressed: () => Navigator.pop(ctx, codeCtrl.text),
                child: const Text('Claim & connect'),
              ),
            ],
          ),
        );
      },
    );
    codeCtrl.dispose();
    if (code == null || code.trim().isEmpty || !mounted) return;
    await _claimCode(code);
  }

  Future<void> _claimCode(String code) async {
    final host = _hostCtrl.text.trim();
    if (host.isEmpty) {
      setState(() {
        _status = 'Host is required before claiming a code';
        _statusIsError = true;
      });
      return;
    }
    if (!PairPayload.looksLikePairCode(code)) {
      setState(() {
        _status = 'Code must be 8 characters (e.g. K7M2-9X4P)';
        _statusIsError = true;
      });
      return;
    }
    setState(() {
      _busy = true;
      _status = 'Claiming pair code…';
      _statusIsError = false;
    });
    try {
      final store = ref.read(settingsStoreProvider);
      final client = ref.read(mcremoteClientProvider);
      final token = await client.claimPairCode(
        hostInput: host,
        code: code,
        fingerprint: _pendingFingerprint,
        mode: _pendingTlsMode,
      );
      await store.setHost(host);
      await store.setToken(token);
      if (client.deviceId != null) {
        await store.setDeviceId(client.deviceId!);
      }
      if (!mounted) return;
      _tokenCtrl.text = token;
      setState(() => _invalidToken = false);
      context.go('/sessions');
    } catch (e) {
      await _handleConnectFailure(e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _testHealth() async {
    setState(() {
      _busy = true;
      _status = 'Checking healthz…';
      _statusIsError = false;
    });
    try {
      final client = ref.read(mcremoteClientProvider);
      final body = await client.healthz(
        _hostCtrl.text.trim(),
        fingerprint: _pendingFingerprint,
        mode: _pendingTlsMode,
      );
      if (!mounted) return;
      setState(() {
        _status = 'Reachable: $body';
        _statusIsError = false;
      });
    } catch (e) {
      // healthz can take 8s; the screen may be gone (auto-connect redirect).
      if (!mounted) return;
      setState(() {
        _status = _friendlyError(e);
        _statusIsError = true;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _connect() async {
    final host = _hostCtrl.text.trim();
    final token = _tokenCtrl.text.trim();
    if (host.isEmpty || token.isEmpty) {
      setState(() {
        _status =
            'Host and token required — or use Enter code / Scan QR instead';
        _statusIsError = true;
      });
      return;
    }
    setState(() {
      _busy = true;
      _status = 'Connecting…';
      _statusIsError = false;
    });
    try {
      final store = ref.read(settingsStoreProvider);
      final client = ref.read(mcremoteClientProvider);
      await client.connect(
        hostInput: host,
        token: token,
        fingerprint: _pendingFingerprint,
        mode: _pendingTlsMode,
      );
      await store.setHost(host);
      await store.setToken(token);
      if (client.deviceId != null) {
        await store.setDeviceId(client.deviceId!);
      }
      if (!mounted) return;
      setState(() => _invalidToken = false);
      context.go('/sessions');
    } catch (e) {
      await _handleConnectFailure(e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _clearCredentials() async {
    final store = ref.read(settingsStoreProvider);
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);
    await store.clearAll();
    await client.disconnect(manual: true);
    transcripts.clearAll();
    if (!mounted) return;
    _tokenCtrl.clear();
    setState(() {
      _status = 'Saved credentials cleared';
      _statusIsError = false;
      _invalidToken = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final disabled = _busy || _autoConnecting;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Magic CLI Remote'),
        actions: [
          PopupMenuButton<String>(
            onSelected: (v) {
              if (v == 'clear') unawaited(_clearCredentials());
            },
            itemBuilder: (_) => const [
              PopupMenuItem(
                value: 'clear',
                child: Text('Clear saved credentials'),
              ),
            ],
          ),
        ],
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            Text(
              'Connect to your machine',
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 8),
            Text(
              'On the host run:  mcremote pair code --name phone\n'
              'Then scan the QR or type the 8-character code.',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(
                  child: FilledButton.tonalIcon(
                    onPressed: disabled ? null : _scanQr,
                    icon: const Icon(Icons.qr_code_scanner),
                    label: const Text('Scan QR'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: FilledButton.icon(
                    onPressed: disabled ? null : _enterCode,
                    icon: const Icon(Icons.pin),
                    label: const Text('Enter code'),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            OutlinedButton.icon(
              onPressed: disabled ? null : _pastePairUri,
              icon: const Icon(Icons.content_paste),
              label: const Text('Paste URI / code / token'),
            ),
            const SizedBox(height: 24),
            TextField(
              controller: _hostCtrl,
              decoration: const InputDecoration(
                labelText: 'Host',
                hintText: '100.64.0.1:7531',
                border: OutlineInputBorder(),
                helperText: 'Mesh: tailnet IP + :7531 (not Headscale :443)',
              ),
              autocorrect: false,
              enableSuggestions: false,
              keyboardType: TextInputType.visiblePassword,
            ),
            const SizedBox(height: 12),
            ExpansionTile(
              title: const Text('Advanced: long-lived token'),
              initiallyExpanded: _advanced,
              onExpansionChanged: (v) => setState(() => _advanced = v),
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(0, 0, 0, 12),
                  child: TextField(
                    controller: _tokenCtrl,
                    decoration: InputDecoration(
                      labelText: 'Device token',
                      hintText: 'mcr_…',
                      border: const OutlineInputBorder(),
                      suffixIcon: IconButton(
                        tooltip: _showToken ? 'Hide' : 'Show',
                        onPressed: () =>
                            setState(() => _showToken = !_showToken),
                        icon: Icon(
                          _showToken ? Icons.visibility_off : Icons.visibility,
                        ),
                      ),
                    ),
                    autocorrect: false,
                    enableSuggestions: false,
                    obscureText: !_showToken,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: disabled ? null : _testHealth,
                    child: const Text('Test healthz'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: FilledButton(
                    onPressed: disabled ? null : _connect,
                    child: (_busy || _autoConnecting)
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('Connect'),
                  ),
                ),
              ],
            ),
            if (_status != null) ...[
              const SizedBox(height: 20),
              Card(
                color: _statusIsError
                    ? scheme.errorContainer
                    : scheme.surfaceContainerHighest,
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      SelectableText(
                        _status!,
                        style: TextStyle(
                          color: _statusIsError
                              ? scheme.onErrorContainer
                              : scheme.onSurface,
                        ),
                      ),
                      if (_invalidToken) ...[
                        const SizedBox(height: 12),
                        Text(
                          'Host kept. Use Enter code or Scan QR to re-pair.',
                          style: TextStyle(
                            color: scheme.onErrorContainer,
                            fontSize: 13,
                          ),
                        ),
                        const SizedBox(height: 8),
                        Align(
                          alignment: Alignment.centerLeft,
                          child: TextButton(
                            onPressed: disabled
                                ? null
                                : () => unawaited(_clearCredentials()),
                            child: Text(
                              'Clear host & all data',
                              style: TextStyle(color: scheme.onErrorContainer),
                            ),
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Formats as XXXX-XXXX while typing.
class _PairCodeFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
    TextEditingValue oldValue,
    TextEditingValue newValue,
  ) {
    final raw = newValue.text
        .toUpperCase()
        .replaceAll(RegExp(r'[^2-9A-HJ-NP-Z]'), '');
    final clipped = raw.length > 8 ? raw.substring(0, 8) : raw;
    final buf = StringBuffer();
    for (var i = 0; i < clipped.length; i++) {
      if (i == 4) buf.write('-');
      buf.write(clipped[i]);
    }
    final text = buf.toString();
    return TextEditingValue(
      text: text,
      selection: TextSelection.collapsed(offset: text.length),
    );
  }
}

