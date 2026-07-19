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
    if (client.state != McConnectionState.connected &&
        client.state != McConnectionState.reconnecting) {
      if (mounted) context.go('/');
      return;
    }
    if (client.state == McConnectionState.reconnecting) {
      setState(() {
        _loading = false;
        _error = 'Reconnecting…';
      });
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
      provider = _providers.isNotEmpty ? _providers.first.id : 'fake';
    }
    // Ensure dropdown value is in items.
    final ids = _providers.map((p) => p.id).toSet();
    if (!ids.contains(provider)) {
      provider = ids.isNotEmpty ? ids.first : null;
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
                  if (_providers.isEmpty)
                    const Text('No providers reported by host.')
                  else
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

    final name = nameCtrl.text.trim();
    final cwd = cwdCtrl.text.trim();
    nameCtrl.dispose();
    cwdCtrl.dispose();

    if (ok != true) return;
    try {
      final meta = await client.createSession(
        provider: provider,
        name: name.isEmpty ? null : name,
        cwd: cwd.isEmpty ? null : cwd,
      );
      if (!mounted) return;
      final q = meta.name.isNotEmpty
          ? '?name=${Uri.encodeComponent(meta.name)}'
          : '';
      await context.push('/sessions/${meta.id}$q');
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
    await client.disconnect(manual: true);
    if (mounted) context.go('/');
  }

  Future<void> _reconnect() async {
    try {
      await ref.read(mcremoteClientProvider).reconnect();
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Reconnect failed: $e')),
      );
    }
  }

  Future<void> _endSession(SessionMeta s) async {
    final label = s.name.isEmpty
        ? (s.id.length > 8 ? s.id.substring(0, 8) : s.id)
        : s.name;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('End session?'),
        content: Text('Stop and close “$label”?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('End'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    try {
      final client = ref.read(mcremoteClientProvider);
      if (s.live) {
        try {
          await client.cancel(s.id);
        } catch (_) {}
        await client.closeSession(s.id);
      }
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Ended $label')),
      );
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('End failed: $e')),
      );
    }
  }

  String _connLabel(McConnectionState? s) {
    switch (s) {
      case McConnectionState.connected:
        return 'Connected';
      case McConnectionState.reconnecting:
        return 'Reconnecting…';
      case McConnectionState.connecting:
        return 'Connecting…';
      case McConnectionState.authenticating:
        return 'Authenticating…';
      case McConnectionState.error:
        return 'Connection error';
      case McConnectionState.disconnected:
      case null:
        return 'Disconnected';
    }
  }

  @override
  Widget build(BuildContext context) {
    final conn = ref.watch(connectionStateProvider);
    final connState = conn.asData?.value;
    final healthy = connState == McConnectionState.connected;
    final scheme = Theme.of(context).colorScheme;

    // Refresh when we recover connection.
    ref.listen(connectionStateProvider, (prev, next) {
      final s = next.asData?.value;
      if (s == McConnectionState.connected) {
        _refresh();
      }
    });

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
      floatingActionButton: healthy
          ? FloatingActionButton.extended(
              onPressed: _createSession,
              icon: const Icon(Icons.add),
              label: const Text('New session'),
            )
          : null,
      body: Column(
        children: [
          if (!healthy)
            Material(
              color: scheme.errorContainer,
              child: ListTile(
                dense: true,
                leading: Icon(
                  connState == McConnectionState.reconnecting
                      ? Icons.sync
                      : Icons.wifi_off,
                  color: scheme.onErrorContainer,
                ),
                title: Text(
                  _connLabel(connState),
                  style: TextStyle(color: scheme.onErrorContainer),
                ),
                trailing: TextButton(
                  onPressed: _reconnect,
                  child: Text(
                    'Retry',
                    style: TextStyle(color: scheme.onErrorContainer),
                  ),
                ),
              ),
            )
          else
            Material(
              color: scheme.surfaceContainerHighest,
              child: ListTile(
                dense: true,
                leading: Icon(Icons.check_circle, color: scheme.primary, size: 20),
                title: Text(
                  _connLabel(connState),
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
            ),
          if (_error != null && healthy)
            Padding(
              padding: const EdgeInsets.all(12),
              child: Text(
                _error!,
                style: TextStyle(color: scheme.error),
              ),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : _sessions.isEmpty
                    ? Center(
                        child: Padding(
                          padding: const EdgeInsets.all(32),
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Icon(
                                Icons.chat_bubble_outline,
                                size: 48,
                                color: scheme.onSurfaceVariant,
                              ),
                              const SizedBox(height: 12),
                              Text(
                                'No sessions yet',
                                style: Theme.of(context).textTheme.titleMedium,
                              ),
                              const SizedBox(height: 8),
                              Text(
                                'Start a Grok (or fake) session on the host from here.',
                                textAlign: TextAlign.center,
                                style: TextStyle(color: scheme.onSurfaceVariant),
                              ),
                              const SizedBox(height: 16),
                              if (healthy)
                                FilledButton.icon(
                                  onPressed: _createSession,
                                  icon: const Icon(Icons.add),
                                  label: const Text('New session'),
                                ),
                            ],
                          ),
                        ),
                      )
                    : RefreshIndicator(
                        onRefresh: _refresh,
                        child: ListView.separated(
                          itemCount: _sessions.length,
                          separatorBuilder: (_, _) => const Divider(height: 1),
                          itemBuilder: (ctx, i) {
                            final s = _sessions[i];
                            final title = s.name.isEmpty
                                ? (s.id.length >= 8
                                    ? 'Session ${s.id.substring(0, 8)}'
                                    : s.id)
                                : s.name;
                            return ListTile(
                              enabled: s.live,
                              title: Text(title),
                              subtitle: Text(
                                '${s.provider} · ${s.status}${s.live ? '' : ' · offline'}',
                              ),
                              trailing: Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  _StatusChip(status: s.status, live: s.live),
                                  if (s.live)
                                    PopupMenuButton<String>(
                                      tooltip: 'Session actions',
                                      onSelected: (v) async {
                                        if (v == 'open') {
                                          final q = s.name.isNotEmpty
                                              ? '?name=${Uri.encodeComponent(s.name)}'
                                              : '';
                                          await context
                                              .push('/sessions/${s.id}$q');
                                          await _refresh();
                                        } else if (v == 'end') {
                                          await _endSession(s);
                                        }
                                      },
                                      itemBuilder: (_) => const [
                                        PopupMenuItem(
                                          value: 'open',
                                          child: Text('Open'),
                                        ),
                                        PopupMenuItem(
                                          value: 'end',
                                          child: Text('End session'),
                                        ),
                                      ],
                                    ),
                                ],
                              ),
                              onTap: s.live
                                  ? () async {
                                      final q = s.name.isNotEmpty
                                          ? '?name=${Uri.encodeComponent(s.name)}'
                                          : '';
                                      await context.push('/sessions/${s.id}$q');
                                      await _refresh();
                                    }
                                  : null,
                              onLongPress:
                                  s.live ? () => _endSession(s) : null,
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
