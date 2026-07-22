import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../data/session_status.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';

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
  bool _endingIdBusy = false;
  bool _creatingBusy = false;
  String? _version;

  @override
  void initState() {
    super.initState();
    _refresh();
    _loadVersion();
  }

  Future<void> _loadVersion() async {
    try {
      final info = await PackageInfo.fromPlatform();
      if (mounted) {
        setState(() {
          _version = '${info.version}+${info.buildNumber}';
        });
      }
    } catch (_) {}
  }

  Future<void> _refresh() async {
    if (!mounted) return;
    final client = ref.read(mcremoteClientProvider);
    // Stay on this screen while paired; never bounce to Connect on a blip.
    if (client.state == McConnectionState.reconnecting ||
        client.state == McConnectionState.connecting ||
        client.state == McConnectionState.authenticating) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = null;
        });
      }
      return;
    }
    if (client.state != McConnectionState.connected) {
      if (mounted) {
        setState(() {
          _loading = false;
          // Keep last known session list visible while offline, but drop the
          // stale error so it does not stay pinned once we come back.
          _error = null;
        });
      }
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
      // Authoritative status: a socket drop can lose the turn_complete that
      // would move a transcript out of 'running', which otherwise leaves the
      // chat composer disabled. Also evicts transcripts for sessions the host
      // no longer reports.
      ref.read(transcriptsProvider.notifier).syncFromMeta(sessions);
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
    // preferredProvider() is a network round-trip before the sheet appears —
    // without this guard a double tap stacks two sheets and two session.create.
    if (_creatingBusy) return;
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Reconnect to the host first')),
      );
      return;
    }
    setState(() => _creatingBusy = true);
    try {
      await _createSessionFlow(client);
    } finally {
      if (mounted) setState(() => _creatingBusy = false);
    }
  }

  Future<void> _createSessionFlow(McremoteClient client) async {
    final nameCtrl = TextEditingController();
    final cwdCtrl = TextEditingController();
    final modelCtrl = TextEditingController();
    // Offer the last-used working directory as the default for the next
    // session; empty means the daemon starts in its user's home directory.
    final settings = ref.read(settingsStoreProvider);
    try {
      cwdCtrl.text = (await settings.getLastCwd()) ?? '';
    } catch (_) {}
    String? provider;
    try {
      provider = await client.preferredProvider();
    } catch (_) {
      provider = _providers.isNotEmpty ? _providers.first.id : 'fake';
    }
    if (!mounted) {
      nameCtrl.dispose();
      cwdCtrl.dispose();
      modelCtrl.dispose();
      return;
    }
    final ids = _providers.map((p) => p.id).toSet();
    if (!ids.contains(provider)) {
      provider = ids.isNotEmpty ? ids.first : null;
    }

    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) {
        return StatefulBuilder(
          builder: (ctx, setModal) {
            return AlertDialog(
              title: const Text('New session'),
              content: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
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
                        helperText: 'empty: host home directory',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: modelCtrl,
                      decoration: const InputDecoration(
                        labelText: 'Model (optional)',
                        helperText:
                            'grok: model name · opencode: provider/model id',
                        border: OutlineInputBorder(),
                      ),
                    ),
                  ],
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(ctx, false),
                  child: const Text('Cancel'),
                ),
                FilledButton(
                  onPressed: () => Navigator.pop(ctx, true),
                  child: const Text('Create'),
                ),
              ],
            );
          },
        );
      },
    );

    final name = nameCtrl.text.trim();
    final cwd = cwdCtrl.text.trim();
    final model = modelCtrl.text.trim();
    nameCtrl.dispose();
    cwdCtrl.dispose();
    modelCtrl.dispose();

    if (ok != true) return;
    try {
      final meta = await client.createSession(
        provider: provider,
        name: name.isEmpty ? null : name,
        cwd: cwd.isEmpty ? null : cwd,
        model: model.isEmpty ? null : model,
      );
      // Remember the directory actually used (the daemon reports the resolved
      // path, e.g. the host home when the field was left empty).
      final usedCwd = meta.cwd ?? cwd;
      if (usedCwd.isNotEmpty) {
        try {
          await settings.setLastCwd(usedCwd);
        } catch (_) {}
      }
      if (!mounted) return;
      final q = meta.name.isNotEmpty
          ? '?name=${Uri.encodeComponent(meta.name)}'
          : '';
      await _openSession('/sessions/${meta.id}$q');
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Create failed: $e')));
    }
  }

  /// Push the chat route and refresh on return. This page can be removed from
  /// the stack while chat is on top (sign-out, invalid_token redirect), so the
  /// mounted check between the two is required — `_refresh` uses `ref`.
  Future<void> _openSession(String location) async {
    await context.push(location);
    if (!mounted) return;
    await _refresh();
  }

  Future<void> _signOut() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Sign out of host?'),
        content: const Text(
          'Stops auto-reconnect on this phone until you connect again. '
          'Agent sessions on the host keep running. '
          'Your device token stays saved so you can reconnect without a new QR '
          '(use Clear credentials on the connect screen to wipe it).',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Stay connected'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Sign out'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final client = ref.read(mcremoteClientProvider);
    await client.disconnect(manual: true);
    ref.read(transcriptsProvider.notifier).clearAll();
    if (mounted) context.go('/');
  }

  Future<void> _reconnect() async {
    try {
      final store = ref.read(settingsStoreProvider);
      await ref.read(mcremoteClientProvider).reconnectFromStore(store);
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Reconnect failed: $e')));
    }
  }

  Future<void> _endSession(SessionMeta s) async {
    if (_endingIdBusy) return;
    // Claim the flag before the confirm dialog: two fast long-presses would
    // otherwise open two dialogs and issue two cancel+close pairs.
    setState(() => _endingIdBusy = true);
    try {
      await _endSessionFlow(s);
    } finally {
      if (mounted) setState(() => _endingIdBusy = false);
    }
  }

  Future<void> _endSessionFlow(SessionMeta s) async {
    final label = s.name.isEmpty
        ? (s.id.length > 8 ? s.id.substring(0, 8) : s.id)
        : s.name;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('End agent session?'),
        content: Text(
          'Stops “$label” on the host and removes it from this list.\n\n'
          'This does not sign the phone out of the host.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(ctx).colorScheme.error,
              foregroundColor: Theme.of(ctx).colorScheme.onError,
            ),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('End session'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;

    // Optimistic remove so the list doesn't look stuck.
    final previous = List<SessionMeta>.from(_sessions);
    setState(() {
      _sessions = _sessions.where((x) => x.id != s.id).toList();
    });
    ref.read(transcriptsProvider.notifier).clearSession(s.id);

    final client = ref.read(mcremoteClientProvider);
    try {
      if (client.state == McConnectionState.connected) {
        if (s.live) {
          try {
            await client.cancel(s.id);
          } catch (_) {}
        }
        // session.delete closes a live session *and* purges the disk record,
        // so it covers both cases. Previously a non-live row skipped the host
        // entirely and simply reappeared on the next refresh.
        await client.deleteSession(s.id);
      }
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Ended $label')));
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      setState(() => _sessions = previous);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('End failed: $e')));
    }
  }

  String _connLabel(McConnectionState? s) {
    switch (s) {
      case McConnectionState.connected:
        return 'Connected to host';
      case McConnectionState.reconnecting:
        return 'Reconnecting to host…';
      case McConnectionState.connecting:
        return 'Connecting…';
      case McConnectionState.authenticating:
        return 'Authenticating…';
      case McConnectionState.error:
        return 'Connection lost — retrying…';
      case McConnectionState.disconnected:
      case null:
        return 'Disconnected';
    }
  }

  @override
  Widget build(BuildContext context) {
    final conn = ref.watch(connectionStateProvider);
    final connState = conn.asData?.value;
    // The stream can fail outright; without this it renders as "Disconnected".
    final connError = conn.hasError ? conn.error.toString() : null;
    final healthy = connState == McConnectionState.connected;
    final scheme = Theme.of(context).colorScheme;

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
            onPressed: healthy && !_loading ? _refresh : null,
            icon: const Icon(Icons.refresh),
          ),
          IconButton(
            tooltip: 'Settings',
            onPressed: () => context.push('/settings'),
            icon: const Icon(Icons.settings_outlined),
          ),
          IconButton(
            tooltip: 'Sign out of host',
            onPressed: _signOut,
            icon: const Icon(Icons.logout),
          ),
        ],
      ),

      body: Stack(
        children: [
          // The MC monogram, faint and full-bleed, dissolved into the surface so
          // it reads as part of the background rather than a foreground image.
          const Positioned.fill(child: _SessionsBackdrop()),
          Column(
        children: [
          if (!healthy)
            Builder(
              builder: (context) {
                // "Linking" (reconnecting/connecting/authenticating) reads as a
                // transient/tertiary state; a hard drop reads as an error.
                final linking =
                    connState == McConnectionState.reconnecting ||
                    connState == McConnectionState.connecting ||
                    connState == McConnectionState.authenticating;
                final bg =
                    linking ? scheme.tertiaryContainer : scheme.errorContainer;
                final fg = linking
                    ? scheme.onTertiaryContainer
                    : scheme.onErrorContainer;
                return Material(
                  color: bg,
                  child: ListTile(
                    dense: true,
                    leading: linking
                        ? SizedBox(
                            width: 22,
                            height: 22,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: fg,
                            ),
                          )
                        : Icon(Icons.wifi_off, color: fg),
                    title: Text(
                      connError != null
                          ? 'Connection error'
                          : _connLabel(connState),
                      style: TextStyle(color: fg),
                    ),
                    subtitle: Text(
                      connError ?? 'Pairing stays active until you sign out.',
                      style: TextStyle(fontSize: 12, color: fg),
                    ),
                    trailing: TextButton(
                      onPressed: _reconnect,
                      child: Text('Retry now', style: TextStyle(color: fg)),
                    ),
                  ),
                );
              },
            )
          else
            Material(
              color: scheme.surfaceContainerHighest,
              child: ListTile(
                dense: true,
                leading: Icon(
                  Icons.check_circle,
                  color: scheme.primary,
                  size: 20,
                ),
                title: Text(
                  _connLabel(connState),
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
            ),
          if (_error != null && healthy)
            Padding(
              padding: const EdgeInsets.all(12),
              child: Text(_error!, style: TextStyle(color: scheme.error)),
            ),
          Expanded(
            child: _loading && healthy
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
                            healthy
                                ? 'No agent sessions yet'
                                : 'Waiting for host connection',
                            style: Theme.of(context).textTheme.titleMedium,
                          ),
                          const SizedBox(height: 8),
                          Text(
                            healthy
                                ? 'Start a Grok (or fake) session from here.'
                                : 'Your pairing is still active. We reconnect automatically when the phone wakes.',
                            textAlign: TextAlign.center,
                            style: TextStyle(color: scheme.onSurfaceVariant),
                          ),
                          const SizedBox(height: 16),
                          if (healthy)
                            FilledButton.icon(
                              onPressed: _creatingBusy ? null : _createSession,
                              icon: const Icon(Icons.add),
                              label: const Text('New session'),
                            )
                          else
                            FilledButton.tonalIcon(
                              onPressed: _reconnect,
                              icon: const Icon(Icons.sync),
                              label: const Text('Retry now'),
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
                          enabled: healthy && s.live,
                          title: Text(title),
                          subtitle: Text(
                            '${s.provider}'
                            '${s.model.isEmpty ? '' : ' · ${s.model}'}'
                            ' · ${humanSessionStatus(s.status)}'
                            '${s.live ? '' : ' · closed'}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                          trailing: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              _StatusChip(status: s.status, live: s.live),
                              PopupMenuButton<String>(
                                tooltip: 'Session actions',
                                onSelected: (v) async {
                                  if (v == 'open' && s.live && healthy) {
                                    final q = s.name.isNotEmpty
                                        ? '?name=${Uri.encodeComponent(s.name)}'
                                        : '';
                                    await _openSession('/sessions/${s.id}$q');
                                  } else if (v == 'end') {
                                    await _endSession(s);
                                  }
                                },
                                itemBuilder: (_) => [
                                  if (s.live && healthy)
                                    const PopupMenuItem(
                                      value: 'open',
                                      child: Text('Open'),
                                    ),
                                  const PopupMenuItem(
                                    value: 'end',
                                    child: Text('End session'),
                                  ),
                                ],
                              ),
                            ],
                          ),
                          onTap: (healthy && s.live)
                              ? () async {
                                  final q = s.name.isNotEmpty
                                      ? '?name=${Uri.encodeComponent(s.name)}'
                                      : '';
                                  await _openSession('/sessions/${s.id}$q');
                                }
                              : null,
                          onLongPress: _endingIdBusy
                              ? null
                              : () => _endSession(s),
                        );
                      },
                    ),
                  ),
          ),
          // Thumb-reachable create action for a populated list. The empty state
          // has its own CTA; without this, a new session was unreachable once
          // any session existed (kept as a bottom button, not a FAB).
          if (healthy && _sessions.isNotEmpty)
            SafeArea(
              top: false,
              bottom: _version == null,
              minimum: const EdgeInsets.fromLTRB(12, 4, 12, 4),
              child: SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  onPressed: _creatingBusy ? null : _createSession,
                  icon: const Icon(Icons.add),
                  label: const Text('New session'),
                ),
              ),
            ),
          if (_version != null)
            SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.all(8.0),
                child: Text(
                  'v$_version',
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant.withValues(alpha: 0.5),
                  ),
                ),
              ),
            ),
        ],
          ),
        ],
      ),
    );
  }
}

/// A faint, full-bleed MC monogram behind the sessions list. Opacity is tuned
/// per brightness so it dissolves into the surface instead of competing with the
/// list rows; [IgnorePointer] keeps it from stealing taps/scroll.
class _SessionsBackdrop extends StatelessWidget {
  const _SessionsBackdrop();

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return IgnorePointer(
      child: Opacity(
        opacity: dark ? 0.10 : 0.05,
        child: Image.asset(
          'assets/MC_icon.png',
          fit: BoxFit.cover,
          alignment: Alignment.center,
        ),
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
      label: Text(
        humanSessionStatus(status),
        style: const TextStyle(fontSize: 12),
      ),
      backgroundColor: color.withValues(alpha: 0.15),
      side: BorderSide(color: color.withValues(alpha: 0.4)),
      visualDensity: VisualDensity.compact,
      padding: EdgeInsets.zero,
    );
  }
}
