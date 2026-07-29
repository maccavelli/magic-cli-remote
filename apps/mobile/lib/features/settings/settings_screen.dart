import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import 'dart:async';

import '../../data/chat/transcript_cache.dart';
import '../../data/local/settings_store.dart' show SecureStorageUnavailable;
import '../../data/notifications/agent_notifications.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/top_notification.dart';

class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  bool _notifications = true;
  bool _notifyAsks = true;
  bool _notifyTurnComplete = true;
  bool _notifyErrors = true;
  bool _osBlocked = false;
  bool _notifsUnavailable = false;
  String? _host;
  String? _version;

  /// null = Provider default; otherwise low|medium|high (MADR 0052).
  String? _defaultThinkingLevel;

  /// provider id → stored default mode id.
  Map<String, String> _defaultModes = {};
  List<ProviderInfo> _providers = const [];
  int _txSessions = 0;
  int _txBytes = 0;
  List<String> _pinnedCwds = const [];
  List<String> _recentCwds = const [];

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    // Resolve everything that needs `ref` before the first await — the screen
    // can be popped while the storage reads are in flight.
    final store = ref.read(settingsStoreProvider);
    final coord = ref.read(notificationCoordinatorProvider);
    bool notifs = _notifications;
    bool asks = _notifyAsks;
    bool turnDone = _notifyTurnComplete;
    bool errors = _notifyErrors;
    String? host = _host;
    var blocked = _osBlocked;
    try {
      notifs = await store.getNotificationsEnabled();
      asks = await store.getNotifyAsks();
      turnDone = await store.getNotifyTurnComplete();
      errors = await store.getNotifyErrors();
      host = await store.getHost();
      blocked = await coord.osBlocked() ?? false;
    } catch (_) {
      // Settings reads are convenience state. Keep the screen usable with its
      // current defaults if local preferences are temporarily unavailable.
    }
    String? version;
    try {
      final info = await PackageInfo.fromPlatform();
      version = '${info.version}+${info.buildNumber}';
    } catch (_) {}
    String? thinking;
    try {
      thinking = await store.getDefaultThinkingLevel();
    } catch (_) {}
    List<ProviderInfo> providers = const [];
    final modes = <String, String>{};
    final client = ref.read(mcremoteClientProvider);
    if (client.state == McConnectionState.connected) {
      try {
        providers = await client.listProviders();
        for (final p in providers) {
          final m = await store.getDefaultSessionMode(p.id);
          if (m != null && m.isNotEmpty) modes[p.id] = m;
        }
      } catch (_) {}
    }
    if (!mounted) return;
    setState(() {
      _notifications = notifs;
      _notifyAsks = asks;
      _notifyTurnComplete = turnDone;
      _notifyErrors = errors;
      _osBlocked = blocked;
      _notifsUnavailable = coord.notificationsUnavailable != null;
      _host = host;
      _version = version;
      _defaultThinkingLevel = thinking;
      _providers = providers;
      _defaultModes = modes;
    });
    unawaited(_loadTranscriptUsage());
    unawaited(_loadCwds());
  }

  Future<void> _loadCwds() async {
    try {
      final store = ref.read(settingsStoreProvider);
      final pinned = await store.getPinnedCwds();
      final recent = await store.getRecentCwds();
      if (!mounted) return;
      setState(() {
        _pinnedCwds = pinned;
        _recentCwds = recent;
      });
    } catch (_) {}
  }

  Future<void> _loadTranscriptUsage() async {
    try {
      final cache = TranscriptCache();
      final u = await cache.usage();
      if (!mounted) return;
      setState(() {
        _txSessions = u.sessions;
        _txBytes = u.bytes;
      });
    } catch (_) {}
  }

  Future<void> _clearTranscriptCache() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Clear cached transcripts?'),
        content: const Text(
          'Removes on-disk chat snapshots from this phone. Open chats keep '
          'what is already in memory; host credentials and pins are not '
          'touched. Reopen a session to re-fetch history from the host.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Clear'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final store = ref.read(settingsStoreProvider);
    await TranscriptCache().clear();
    await store.clearOrphanedModelPrefs();
    if (!mounted) return;
    setState(() {
      _txSessions = 0;
      _txBytes = 0;
    });
    showTopNotification(context, 'Cached transcripts cleared');
  }

  Future<void> _pickDefaultMode(String providerId) async {
    // Modes are session-advertised. Collect from any live transcript for this
    // provider; fall back to free-text common ids if none are open.
    final transcripts = ref.read(transcriptsProvider);
    final modes = <SessionMode>[];
    final seen = <String>{};
    for (final t in transcripts.byId.values) {
      for (final m in t.modes) {
        if (seen.add(m.id)) modes.add(m);
      }
    }
    if (modes.isEmpty) {
      // Minimal static floor so a user can still set auto/plan/default before
      // opening a session. Unknown ids are ignored at apply time (B2).
      modes.addAll(const [
        SessionMode(id: 'default', name: 'Default'),
        SessionMode(id: 'plan', name: 'Plan'),
        SessionMode(id: 'auto', name: 'Auto', dangerous: true),
      ]);
    }
    final current = _defaultModes[providerId] ?? '';
    final choice = await showDialog<String>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: Text('Default mode · $providerId'),
        children: [
          ListTile(
            title: const Text('Provider default'),
            trailing: current.isEmpty ? const Icon(Icons.check) : null,
            onTap: () => Navigator.pop(ctx, ''),
          ),
          for (final m in modes)
            ListTile(
              title: Text(m.name.isEmpty ? m.id : m.name),
              subtitle: m.dangerous
                  ? const Text('Runs without approvals')
                  : null,
              trailing: current == m.id ? const Icon(Icons.check) : null,
              onTap: () async {
                if (m.dangerous) {
                  final ok = await showDialog<bool>(
                    context: ctx,
                    builder: (dctx) => AlertDialog(
                      title: const Text('Set dangerous default?'),
                      content: Text(
                        'New $providerId sessions will start in "${m.name.isEmpty ? m.id : m.name}" '
                        'and approve permissions automatically. Confirm once here; '
                        'sessions will not re-ask.',
                      ),
                      actions: [
                        TextButton(
                          onPressed: () => Navigator.pop(dctx, false),
                          child: const Text('Cancel'),
                        ),
                        FilledButton(
                          onPressed: () => Navigator.pop(dctx, true),
                          child: const Text('Set as default'),
                        ),
                      ],
                    ),
                  );
                  if (ok != true) return;
                }
                if (ctx.mounted) Navigator.pop(ctx, m.id);
              },
            ),
        ],
      ),
    );
    if (choice == null || !mounted) return;
    final store = ref.read(settingsStoreProvider);
    final next = choice.isEmpty ? null : choice;
    await store.setDefaultSessionMode(providerId, next);
    if (!mounted) return;
    setState(() {
      if (next == null) {
        _defaultModes.remove(providerId);
      } else {
        _defaultModes[providerId] = next;
      }
    });
  }

  Future<void> _pickDefaultThinkingLevel() async {
    final current = _defaultThinkingLevel ?? '';
    final choice = await showDialog<String>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: const Text('Default thinking level'),
        children: [
          for (final entry in [
            ('', 'Provider default'),
            ('low', 'Low'),
            ('medium', 'Medium'),
            ('high', 'High'),
          ])
            ListTile(
              title: Text(entry.$2),
              trailing: current == entry.$1 ? const Icon(Icons.check) : null,
              onTap: () => Navigator.pop(ctx, entry.$1),
            ),
        ],
      ),
    );
    if (choice == null || !mounted) return;
    final store = ref.read(settingsStoreProvider);
    final next = choice.isEmpty ? null : choice;
    await store.setDefaultThinkingLevel(next);
    if (!mounted) return;
    setState(() => _defaultThinkingLevel = next);
  }

  Future<void> _setNotifications(bool value) async {
    // Resolve everything that needs `ref` before the first await — the screen
    // can be popped while the settings write is in flight.
    final store = ref.read(settingsStoreProvider);
    final coord = ref.read(notificationCoordinatorProvider);
    setState(() => _notifications = value);
    await store.setNotificationsEnabled(value);
    await coord.setEnabled(value);
    if (value) {
      // The in-app toggle is meaningless while the OS blocks the app — ask
      // again, and surface the blocked state if the user declined.
      await coord.requestOsPermission();
      final blocked = await coord.osBlocked() ?? false;
      if (mounted) setState(() => _osBlocked = blocked);
    }
  }

  Future<void> _setNotifyKinds({
    bool? asks,
    bool? turnComplete,
    bool? errors,
  }) async {
    final store = ref.read(settingsStoreProvider);
    final coord = ref.read(notificationCoordinatorProvider);
    setState(() {
      if (asks != null) _notifyAsks = asks;
      if (turnComplete != null) _notifyTurnComplete = turnComplete;
      if (errors != null) _notifyErrors = errors;
    });
    if (asks != null) await store.setNotifyAsks(asks);
    if (turnComplete != null) await store.setNotifyTurnComplete(turnComplete);
    if (errors != null) await store.setNotifyErrors(errors);
    coord.kinds = NotifyKinds(
      asks: _notifyAsks,
      turnComplete: _notifyTurnComplete,
      errors: _notifyErrors,
    );
  }

  Future<void> _clearCredentials() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Clear saved credentials?'),
        content: const Text(
          'Removes the saved host and device token from this phone and signs '
          'out. Agent sessions on the host keep running. You will need a new '
          'pair code or QR to reconnect.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: destructiveFilled(Theme.of(ctx).colorScheme),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Clear & sign out'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    // Resolve everything that needs `ref` before the first await — the
    // disconnect triggers the redirect that disposes this screen.
    final store = ref.read(settingsStoreProvider);
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);
    // Outstanding asks can no longer be answered once the token is gone.
    ref.read(notificationCoordinatorProvider).dropAllAsks();
    // A keystore that refused the delete still holds the token; sign out
    // regardless, but say so instead of implying the device is clean.
    Object? clearFailure;
    try {
      await store.clearAll();
    } on SecureStorageUnavailable catch (e) {
      clearFailure = e;
    }
    await client.disconnect(manual: true);
    transcripts.clearAll();
    if (!mounted) return;
    if (clearFailure != null) {
      showTopNotification(
        context,
        'Signed out, but the device keystore refused to erase the saved '
        'credentials — try again.',
        severity: NoticeSeverity.error,
      );
    }
    context.go('/');
  }

  @override
  Widget build(BuildContext context) {
    final theme = ref.watch(themeModeProvider);
    final scheme = Theme.of(context).colorScheme;

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        children: [
          _sectionHeader(context, 'Appearance'),
          RadioGroup<ThemeMode>(
            groupValue: theme,
            onChanged: (m) {
              if (m != null) ref.read(themeModeProvider.notifier).set(m);
            },
            child: const Column(
              children: [
                RadioListTile(
                  value: ThemeMode.system,
                  title: Text('System default'),
                ),
                RadioListTile(value: ThemeMode.light, title: Text('Light')),
                RadioListTile(value: ThemeMode.dark, title: Text('Dark')),
              ],
            ),
          ),
          const Divider(),
          _sectionHeader(context, 'Notifications'),
          SwitchListTile(
            value: _notifications,
            onChanged: _setNotifications,
            title: const Text('Agent alerts'),
            subtitle: const Text(
              'Get notified when the agent needs approval or finishes a turn. '
              'Keeps a background connection to your host.',
            ),
          ),
          SwitchListTile(
            value: _notifyAsks,
            onChanged: _notifications ? (v) => _setNotifyKinds(asks: v) : null,
            title: const Text('Permission requests'),
            subtitle: const Text('Blocking — the agent is waiting on you'),
          ),
          SwitchListTile(
            value: _notifyTurnComplete,
            onChanged: _notifications
                ? (v) => _setNotifyKinds(turnComplete: v)
                : null,
            title: const Text('Turn complete'),
            subtitle: const Text('Informational — a turn finished'),
          ),
          SwitchListTile(
            value: _notifyErrors,
            onChanged: _notifications
                ? (v) => _setNotifyKinds(errors: v)
                : null,
            title: const Text('Errors'),
            subtitle: const Text('A failed turn while you were away'),
          ),
          if (_notifications && _osBlocked)
            ListTile(
              leading: Icon(
                Icons.notifications_off_outlined,
                color: scheme.error,
              ),
              title: Text(
                'Notifications are blocked by Android',
                style: TextStyle(color: scheme.error),
              ),
              subtitle: const Text(
                'Allow them for Magic CLI Remote in system settings, or '
                'alerts will never appear.',
              ),
            ),
          if (_notifications && !_osBlocked && _notifsUnavailable)
            ListTile(
              leading: Icon(Icons.error_outline, color: scheme.error),
              title: Text(
                'Notifications are unavailable on this device',
                style: TextStyle(color: scheme.error),
              ),
              subtitle: const Text(
                'Setting them up failed. Restarting the app usually fixes it; '
                'until then no alerts will appear.',
              ),
            ),
          const Divider(),
          _sectionHeader(context, 'Sessions'),
          ListTile(
            leading: const Icon(Icons.psychology_outlined),
            title: const Text('Default thinking level'),
            subtitle: Text(switch (_defaultThinkingLevel) {
              'low' => 'Low',
              'medium' => 'Medium',
              'high' => 'High',
              _ => 'Provider default',
            }),
            onTap: _pickDefaultThinkingLevel,
          ),
          for (final p in _providers.where((p) => p.ready))
            ListTile(
              leading: const Icon(Icons.tune),
              title: Text('Default mode · ${p.id}'),
              subtitle: Text(
                _defaultModes[p.id]?.isNotEmpty == true
                    ? _defaultModes[p.id]!
                    : 'Provider default',
              ),
              onTap: () => _pickDefaultMode(p.id),
            ),
          const Divider(),
          _sectionHeader(context, 'Working directories'),
          if (_pinnedCwds.isEmpty && _recentCwds.isEmpty)
            const ListTile(
              title: Text('No directories yet'),
              subtitle: Text('Paths used for sessions appear here'),
            ),
          for (final path in _pinnedCwds)
            ListTile(
              leading: const Icon(Icons.push_pin),
              title: Text(path, maxLines: 1, overflow: TextOverflow.ellipsis),
              trailing: IconButton(
                tooltip: 'Unpin',
                icon: const Icon(Icons.close),
                onPressed: () async {
                  await ref.read(settingsStoreProvider).unpinCwd(path);
                  await _loadCwds();
                },
              ),
            ),
          for (final path in _recentCwds)
            if (!_pinnedCwds.contains(path))
              ListTile(
                leading: const Icon(Icons.history),
                title: Text(path, maxLines: 1, overflow: TextOverflow.ellipsis),
                trailing: IconButton(
                  tooltip: 'Pin',
                  icon: const Icon(Icons.push_pin_outlined),
                  onPressed: () async {
                    await ref.read(settingsStoreProvider).pinCwd(path);
                    await _loadCwds();
                  },
                ),
              ),
          const Divider(),
          _sectionHeader(context, 'Storage'),
          ListTile(
            leading: const Icon(Icons.storage_outlined),
            title: const Text('Cached transcripts'),
            subtitle: Text(
              _txSessions == 0
                  ? 'Empty'
                  : '$_txSessions sessions · ${_formatBytes(_txBytes)}',
            ),
            trailing: TextButton(
              onPressed: _txSessions == 0 && _txBytes == 0
                  ? null
                  : _clearTranscriptCache,
              child: const Text('Clear'),
            ),
          ),
          const Divider(),
          _sectionHeader(context, 'Host'),
          ListTile(
            leading: const Icon(Icons.dns_outlined),
            title: const Text('Saved host'),
            subtitle: Text(_host == null || _host!.isEmpty ? '—' : _host!),
          ),
          ListTile(
            leading: Icon(Icons.logout, color: scheme.error),
            title: Text(
              'Clear saved credentials',
              style: TextStyle(color: scheme.error),
            ),
            subtitle: const Text('Removes host + token and signs out'),
            onTap: _clearCredentials,
          ),
          const Divider(),
          _sectionHeader(context, 'About'),
          ListTile(
            leading: const Icon(Icons.info_outline),
            title: const Text('Version'),
            subtitle: Text(_version ?? '—'),
          ),
        ],
      ),
    );
  }

  Widget _sectionHeader(BuildContext context, String text) => Padding(
    padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
    child: Text(
      text,
      style: Theme.of(context).textTheme.labelLarge?.copyWith(
        color: Theme.of(context).colorScheme.primary,
      ),
    ),
  );

  static String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) {
      return '${(bytes / 1024).toStringAsFixed(1)} KB';
    }
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
}
