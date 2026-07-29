import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../data/local/settings_store.dart' show SecureStorageUnavailable;
import '../../data/protocol/picker.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/top_notification.dart';
import '../widgets/option_picker_sheet.dart';

class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  bool _notifications = true;
  bool _osBlocked = false;
  bool _notifsUnavailable = false;
  String? _host;
  String? _version;
  String? _preferredProvider;
  String? _preferredModel;

  /// null = Provider default; otherwise low|medium|high (MADR 0052).
  String? _defaultThinkingLevel;
  bool _pickingModelBusy = false;

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
    final client = ref.read(mcremoteClientProvider);
    bool notifs = _notifications;
    String? host = _host;
    var blocked = _osBlocked;
    try {
      notifs = await store.getNotificationsEnabled();
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
    String? prefProv;
    String? prefModel;
    String? thinking;
    try {
      thinking = await store.getDefaultThinkingLevel();
    } catch (_) {}
    if (client.state == McConnectionState.connected) {
      try {
        prefProv = await client.preferredProvider();
        prefModel = await store.getPreferredModel(prefProv);
      } catch (_) {}
    }
    if (!mounted) return;
    setState(() {
      _notifications = notifs;
      _osBlocked = blocked;
      _notifsUnavailable = coord.notificationsUnavailable != null;
      _host = host;
      _version = version;
      _preferredProvider = prefProv;
      _preferredModel = prefModel;
      _defaultThinkingLevel = thinking;
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

  Future<void> _pickPreferredModel() async {
    // Two network awaits run before the sheet appears — without this guard a
    // double tap on the tile stacks two picker sheets.
    if (_pickingModelBusy) return;
    setState(() => _pickingModelBusy = true);
    try {
      await _pickPreferredModelFlow();
    } finally {
      if (mounted) setState(() => _pickingModelBusy = false);
    }
  }

  Future<void> _pickPreferredModelFlow() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      if (!mounted) return;
      showTopNotification(context, 'Connect to a host to load models');
      return;
    }
    String provider;
    try {
      provider = await client.preferredProvider();
    } catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        'No provider: $e',
        severity: NoticeSeverity.error,
      );
      return;
    }
    PickerCatalog catalog;
    try {
      catalog = await client.listModels(provider);
    } catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        'Could not load models: $e',
        severity: NoticeSeverity.error,
      );
      catalog = PickerCatalog(allowCustom: true, provider: provider);
    }
    if (!mounted) return;
    final result = await showOptionPicker(
      context,
      catalog: catalog,
      title: 'Default model · $provider',
      initialSelected: (_preferredModel != null && _preferredModel!.isNotEmpty)
          ? [_preferredModel!]
          : null,
    );
    if (result == null || !mounted) return;
    final model = result.single ?? '';
    await ref.read(settingsStoreProvider).setPreferredModel(provider, model);
    if (!mounted) return;
    setState(() {
      _preferredProvider = provider;
      _preferredModel = model.isEmpty ? null : model;
    });
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
            leading: const Icon(Icons.smart_toy_outlined),
            title: const Text('Default model'),
            subtitle: Text(
              _preferredModel == null || _preferredModel!.isEmpty
                  ? (_preferredProvider == null
                        ? 'Provider default (connect to pick)'
                        : 'Provider default · $_preferredProvider')
                  : '$_preferredModel'
                        '${_preferredProvider != null ? ' · $_preferredProvider' : ''}',
            ),
            onTap: _pickPreferredModel,
          ),
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
