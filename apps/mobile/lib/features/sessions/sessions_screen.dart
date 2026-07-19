import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../state/app_providers.dart';

class SessionsScreen extends ConsumerStatefulWidget {
  const SessionsScreen({super.key});

  @override
  ConsumerState<SessionsScreen> createState() => _SessionsScreenState();
}

class _SessionsScreenState extends ConsumerState<SessionsScreen> {
  List<SessionMeta> _sessions = [];
  List<ProviderInfo> _providers = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      if (mounted) context.go('/');
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final sessions = await client.listSessions();
      final providers = await client.listProviders();
      if (!mounted) return;
      setState(() {
        _sessions = sessions;
        _providers = providers;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _loading = false;
      });
    }
  }

  Future<void> _createSession() async {
    final client = ref.read(mcremoteClientProvider);
    final nameCtrl = TextEditingController();
    final cwdCtrl = TextEditingController();
    String? provider;
    try {
      provider = await client.preferredProvider();
    } catch (_) {
      provider = 'fake';
    }

    if (!mounted) return;
    final ok = await showModalBottomSheet<bool>(
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
          child: StatefulBuilder(
            builder: (ctx, setModal) {
              return Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text('New session',
                      style: Theme.of(ctx).textTheme.titleLarge),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    // ignore: deprecated_member_use
                    value: provider,
                    decoration: const InputDecoration(
                      labelText: 'Provider',
                      border: OutlineInputBorder(),
                    ),
                    items: _providers
                        .map(
                          (p) => DropdownMenuItem(
                            value: p.id,
                            child: Text(
                              '${p.id}${p.ready ? '' : ' (not ready)'}',
                            ),
                          ),
                        )
                        .toList(),
                    onChanged: (v) => setModal(() => provider = v),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: nameCtrl,
                    decoration: const InputDecoration(
                      labelText: 'Name (optional)',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: cwdCtrl,
                    decoration: const InputDecoration(
                      labelText: 'Working directory (optional, absolute)',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 16),
                  FilledButton(
                    onPressed: () => Navigator.pop(ctx, true),
                    child: const Text('Create'),
                  ),
                ],
              );
            },
          ),
        );
      },
    );

    if (ok != true) return;
    try {
      final meta = await client.createSession(
        provider: provider,
        name: nameCtrl.text.trim().isEmpty ? null : nameCtrl.text.trim(),
        cwd: cwdCtrl.text.trim().isEmpty ? null : cwdCtrl.text.trim(),
      );
      if (!mounted) return;
      context.push('/sessions/${meta.id}');
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Create failed: $e')),
      );
    }
  }

  Future<void> _disconnect() async {
    final client = ref.read(mcremoteClientProvider);
    await client.disconnect();
    if (mounted) context.go('/');
  }

  @override
  Widget build(BuildContext context) {
    final conn = ref.watch(connectionStateProvider);
    final connLabel = conn.when(
      data: (s) => s.name,
      loading: () => '…',
      error: (e, _) => 'error',
    );

    return Scaffold(
      appBar: AppBar(
        title: const Text('Sessions'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            onPressed: _loading ? null : _refresh,
            icon: const Icon(Icons.refresh),
          ),
          IconButton(
            tooltip: 'Disconnect',
            onPressed: _disconnect,
            icon: const Icon(Icons.logout),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _createSession,
        icon: const Icon(Icons.add),
        label: const Text('New session'),
      ),
      body: Column(
        children: [
          MaterialBanner(
            content: Text('Connection: $connLabel'),
            actions: const [SizedBox.shrink()],
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.all(12),
              child: Text(_error!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : _sessions.isEmpty
                    ? const Center(child: Text('No sessions yet'))
                    : RefreshIndicator(
                        onRefresh: _refresh,
                        child: ListView.separated(
                          itemCount: _sessions.length,
                          separatorBuilder: (_, _) => const Divider(height: 1),
                          itemBuilder: (ctx, i) {
                            final s = _sessions[i];
                            return ListTile(
                              title: Text(
                                s.name.isEmpty ? s.id.substring(0, 8) : s.name,
                              ),
                              subtitle: Text(
                                '${s.provider} · ${s.status}${s.live ? '' : ' · offline'}',
                              ),
                              trailing: _StatusChip(status: s.status, live: s.live),
                              onTap: s.live
                                  ? () => context.push('/sessions/${s.id}')
                                  : null,
                            );
                          },
                        ),
                      ),
          ),
        ],
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.status, required this.live});

  final String status;
  final bool live;

  @override
  Widget build(BuildContext context) {
    Color color;
    switch (status) {
      case 'running':
        color = Colors.blue;
      case 'error':
        color = Colors.red;
      case 'disconnected':
        color = Colors.grey;
      default:
        color = live ? Colors.green : Colors.grey;
    }
    return Chip(
      label: Text(status, style: const TextStyle(fontSize: 12)),
      backgroundColor: color.withValues(alpha: 0.15),
      side: BorderSide(color: color.withValues(alpha: 0.4)),
      visualDensity: VisualDensity.compact,
      padding: EdgeInsets.zero,
    );
  }
}
