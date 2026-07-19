import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../data/protocol/pair_uri.dart';
import '../../state/app_providers.dart';
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
      if (host != null &&
          host.isNotEmpty &&
          token != null &&
          token.isNotEmpty) {
        final client = ref.read(mcremoteClientProvider);
        if (client.state == McConnectionState.disconnected ||
            client.state == McConnectionState.error) {
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
            setState(() {
              _status = _friendlyError(e);
              _statusIsError = true;
              _autoConnecting = false;
            });
          }
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

  String _friendlyError(Object e) {
    final s = e.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');
    if (s.contains('TimeoutException') || s.contains('timed out')) {
      return 'Timed out — check host and Tailscale mesh.';
    }
    if (s.contains('invalid_token') || s.contains('invalid device token')) {
      return 'Invalid device token. Create a new pair code on the host.';
    }
    if (s.contains('invalid pair code') || s.contains('invalid_code')) {
      return 'Invalid or already-used pair code.';
    }
    if (s.contains('expired')) {
      return 'Pair code expired (5 min). Generate a new one on the host.';
    }
    return s;
  }

  Future<void> _applyPair(PairPayload payload) async {
    setState(() {
      _hostCtrl.text = payload.host;
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
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final text = data?.text?.trim() ?? '';
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
      final token = await client.claimPairCode(hostInput: host, code: code);
      await store.setHost(host);
      await store.setToken(token);
      _tokenCtrl.text = token;
      if (client.deviceId != null) {
        await store.setDeviceId(client.deviceId!);
      }
      if (!mounted) return;
      context.go('/sessions');
    } catch (e) {
      setState(() {
        _status = _friendlyError(e);
        _statusIsError = true;
      });
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
      final body = await client.healthz(_hostCtrl.text.trim());
      setState(() {
        _status = 'Reachable: $body';
        _statusIsError = false;
      });
    } catch (e) {
      setState(() {
        _status = _friendlyError(e);
        _statusIsError = true;
      });
    } finally {
      setState(() => _busy = false);
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
      await client.connect(hostInput: host, token: token);
      await store.setHost(host);
      await store.setToken(token);
      if (client.deviceId != null) {
        await store.setDeviceId(client.deviceId!);
      }
      if (!mounted) return;
      context.go('/sessions');
    } catch (e) {
      setState(() {
        _status = _friendlyError(e);
        _statusIsError = true;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _clearCredentials() async {
    await ref.read(settingsStoreProvider).clearAll();
    _tokenCtrl.clear();
    setState(() {
      _status = 'Saved credentials cleared';
      _statusIsError = false;
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
                  child: SelectableText(
                    _status!,
                    style: TextStyle(
                      color: _statusIsError
                          ? scheme.onErrorContainer
                          : scheme.onSurface,
                    ),
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

