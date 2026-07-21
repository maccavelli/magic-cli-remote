import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';

class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  bool _notifications = true;
  String? _host;
  String? _version;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final store = ref.read(settingsStoreProvider);
    final notifs = await store.getNotificationsEnabled();
    final host = await store.getHost();
    String? version;
    try {
      final info = await PackageInfo.fromPlatform();
      version = '${info.version}+${info.buildNumber}';
    } catch (_) {}
    if (!mounted) return;
    setState(() {
      _notifications = notifs;
      _host = host;
      _version = version;
    });
  }

  Future<void> _setNotifications(bool value) async {
    setState(() => _notifications = value);
    await ref.read(settingsStoreProvider).setNotificationsEnabled(value);
    await ref.read(notificationCoordinatorProvider).setEnabled(value);
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
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(ctx).colorScheme.error,
              foregroundColor: Theme.of(ctx).colorScheme.onError,
            ),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Clear & sign out'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final store = ref.read(settingsStoreProvider);
    final client = ref.read(mcremoteClientProvider);
    await store.clearAll();
    await client.disconnect(manual: true);
    ref.read(transcriptsProvider.notifier).clearAll();
    if (mounted) context.go('/');
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
}
