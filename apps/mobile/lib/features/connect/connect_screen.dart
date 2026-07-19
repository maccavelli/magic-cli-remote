import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../state/app_providers.dart';

/// Android emulator uses 10.0.2.2 for the host machine; Linux desktop uses loopback.
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
  String? _status;

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
    } catch (e) {
      // Secure storage / prefs must never block the connect screen.
      debugPrint('ConnectScreen._load: $e');
    }
  }

  @override
  void dispose() {
    _hostCtrl.dispose();
    _tokenCtrl.dispose();
    super.dispose();
  }

  Future<void> _testHealth() async {
    setState(() {
      _busy = true;
      _status = 'Checking healthz…';
    });
    try {
      final client = ref.read(mcremoteClientProvider);
      final body = await client.healthz(_hostCtrl.text.trim());
      setState(() => _status = 'healthz OK: $body');
    } catch (e) {
      setState(() => _status = 'healthz failed: $e');
    } finally {
      setState(() => _busy = false);
    }
  }

  Future<void> _connect() async {
    final host = _hostCtrl.text.trim();
    final token = _tokenCtrl.text.trim();
    if (host.isEmpty || token.isEmpty) {
      setState(() => _status = 'Host and device token are required');
      return;
    }
    setState(() {
      _busy = true;
      _status = 'Connecting…';
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
      setState(() => _status = 'Connect failed: $e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Magic CLI Remote'),
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            Text(
              'Connect to mcremote',
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 8),
            Text(
              'Paste a device token from `mcremote pair create` on the host. '
              'Emulator: 10.0.2.2:7531 · Physical device: Headscale IP or MagicDNS.',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
            const SizedBox(height: 24),
            TextField(
              controller: _hostCtrl,
              decoration: const InputDecoration(
                labelText: 'Host',
                hintText: '100.64.0.1:7531  (host:port, no http://)',
                border: OutlineInputBorder(),
                helperText: 'Mesh: tailnet IP + :7531 — not Headscale :443',
              ),
              autocorrect: false,
              enableSuggestions: false,
              // Avoid URL keyboards that mangle host:port on some Android builds.
              keyboardType: TextInputType.visiblePassword,
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _tokenCtrl,
              decoration: const InputDecoration(
                labelText: 'Device token',
                hintText: 'mcr_…',
                border: OutlineInputBorder(),
              ),
              autocorrect: false,
              enableSuggestions: false,
              obscureText: true,
            ),
            const SizedBox(height: 24),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: _busy ? null : _testHealth,
                    child: const Text('Test healthz'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: FilledButton(
                    onPressed: _busy ? null : _connect,
                    child: _busy
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
              SelectableText(
                _status!,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ],
        ),
      ),
    );
  }
}
