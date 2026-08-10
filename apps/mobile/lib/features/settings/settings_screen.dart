import 'package:flutter/foundation.dart'
    show TargetPlatform, defaultTargetPlatform;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import 'dart:async';

import 'package:flutter/services.dart';

import '../../data/chat/transcript_cache.dart';
import '../../data/local/settings_store.dart'
    show SecureStorageUnavailable, SettingsStore;
import '../../data/ws/client_identity.dart' show debugSpkiFingerprint;
import '../../data/notifications/agent_notifications.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/top_notification.dart';
import 'app_update_tile.dart';
import 'provider_auth_sheet.dart';
import 'receipts_screen.dart';

class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  // Checked directly rather than via Theme.of(context).platform: the
  // celestial theme pins the theme platform, which would misreport iOS here.
  bool get _isIOS => defaultTargetPlatform == TargetPlatform.iOS;

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
  String? _relayUrl;
  String? _relayHostId;
  String? _relayAuthority;
  String? _pinFingerprint;
  String? _pinTlsMode;
  bool _clientIdentityPresent = false;

  /// SPKI fingerprint of the enrolled client key (MADR 0066 D9); null when
  /// no identity, '' when the stored key would not parse.
  String? _identityFingerprint;

  /// Last recorded secure-storage failure (MADR 0066 D5), if any.
  ({String op, String error, DateTime at})? _storageFailure;

  /// Transport state for the Route section (MADR 0062 D6).
  TransportAvailability _availability = TransportAvailability.none;
  TransportMode? _sticky;
  TransportMode? _selection;
  bool _probing = false;
  bool _reconnecting = false;

  /// Connect mode (MADR 0064 D6): 'auto' or 'select'.
  String _connectMode = 'auto';

  /// Whether a device token is stored (MADR 0064 D4). Presence only — the
  /// value itself stays in the keystore until the edit dialog asks for it.
  bool _tokenPresent = false;

  @override
  void initState() {
    super.initState();
    _load();
    // Deliberately not chained behind _load(): that one awaits platform
    // channels (package info, OS notification state) which can be slow or
    // simply never answer on some hosts, and the Route section must not sit
    // empty behind them.
    unawaited(_loadConnectionInfo());
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
    } catch (e) {
      // best-effort: version subtitle is decoration.
      debugPrint('settings: package info failed: $e');
    }
    String? thinking;
    try {
      thinking = await store.getDefaultThinkingLevel();
    } catch (e) {
      // best-effort: thinking default chip only.
      debugPrint('settings: default thinking level failed: $e');
    }
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
      } catch (e) {
        // best-effort: provider mode chips still usable offline.
        debugPrint('settings: listProviders/default modes failed: $e');
      }
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

  Future<void> _loadConnectionInfo() async {
    try {
      final store = ref.read(settingsStoreProvider);
      final host = await store.getHost();
      final deviceId = await store.getDeviceId();
      final relayUrl = await store.getRelayUrl();
      final relayHostId = await store.getRelayHostId();
      final relayAuth = await store.getRelayAuthority();
      // Security card: real deviceId, no identity fallback (MADR 0046 H-B).
      final pin = host == null || host.isEmpty
          ? null
          : await store.getPinnedCert(
              host,
              deviceId: deviceId,
              fallbackToPersistedIdentity: false,
            );
      final identity = await store.getClientCertAndKey();
      // MADR 0066 D9: the SPKI fingerprint the daemon enrols, for a visual
      // diff against `pair list`. Computed here, once per load — the EC
      // math is not per-build material. Never throws: an unparseable key
      // yields '' and renders as unreadable.
      final identityFp = identity == null
          ? null
          : debugSpkiFingerprint(identity.key);
      final storageFailure = await store.getLastStorageFailure();
      final authority = _authorityOf(host);
      final sticky = await store.getLastTransportSuccess(authority);
      final connectMode = await store.getConnectMode();
      // Presence check only; a keystore refusing to read counts as absent.
      String? token;
      try {
        token = await store.getToken();
      } catch (e) {
        // best-effort: presence only; keystore refuse counts as absent.
        debugPrint('settings: getToken presence check failed: $e');
      }
      if (!mounted) return;
      setState(() {
        _connectMode = connectMode;
        _tokenPresent = token != null && token.isNotEmpty;
        _host = host;
        _relayUrl = relayUrl;
        _relayHostId = relayHostId;
        _relayAuthority = relayAuth;
        _pinFingerprint = pin?.fingerprint;
        _pinTlsMode = pin?.mode.name;
        _clientIdentityPresent = identity != null;
        _identityFingerprint = identityFp;
        _storageFailure = storageFailure;
        _sticky = sticky;
        _availability = transportAvailabilityFromConfig(
          host: host,
          relayUrl: relayUrl,
          relayHostId: relayHostId,
          relayAuthority: relayAuth,
          hostAuthority: authority,
        );
      });
      unawaited(_refreshTransportProbes());
    } catch (e) {
      // best-effort: settings cards degrade without store snapshot.
      debugPrint('settings: load snapshot failed: $e');
    }
  }

  static String? _authorityOf(String? hostInput) {
    if (hostInput == null || hostInput.trim().isEmpty) return null;
    try {
      final ep = SettingsStore.parseEndpoint(hostInput);
      return '${ep.host}:${ep.port}';
    } catch (_) {
      return hostInput.trim();
    }
  }

  /// Re-run both transport probes for the Route section (MADR 0062 D6).
  Future<void> _refreshTransportProbes() async {
    if (!mounted) return;
    if (!_availability.meshConfigured && !_availability.relayConfigured) return;
    setState(() => _probing = true);
    try {
      final probed = await ref
          .read(transportProbesProvider)
          .probe(configured: _availability, host: _host, relayUrl: _relayUrl);
      if (!mounted) return;
      setState(() {
        _availability = probed;
        _probing = false;
        if (!probed.bothAvailable) _selection = null;
      });
    } catch (e) {
      debugPrint('SettingsScreen transport probes: $e');
      if (mounted) setState(() => _probing = false);
    }
  }

  /// Reconnect on the selected transport, without re-pairing (D6).
  ///
  /// The choice is **forced** as the episode's primary, and the episode is
  /// user-initiated so it keeps its own fallback even if an automatic one
  /// already ran on this network (amendment A5). Switching transports is
  /// therefore never a one-way trip into a dead path.
  Future<void> _reconnectNow() async {
    final client = ref.read(mcremoteClientProvider);
    final store = ref.read(settingsStoreProvider);
    final forced = _selection ?? _availability.soleAvailable ?? _sticky;
    setState(() => _reconnecting = true);
    try {
      await store.setTransportSelection(forced, _authorityOf(_host));
      await client.reconnectFromStore(
        store,
        transport: forced,
        userInitiated: true,
      );
      if (!mounted) return;
      showTopNotification(
        context,
        forced == null
            ? 'Reconnected'
            : 'Reconnected over ${forced.label.toLowerCase()}',
      );
    } catch (e) {
      if (!mounted) return;
      showTopNotification(context, 'Reconnect failed: ${friendlyOpError(e)}');
    } finally {
      if (mounted) setState(() => _reconnecting = false);
      unawaited(_loadConnectionInfo());
    }
  }

  /// Pick what a dual-available pair code does on arrival (MADR 0064 D6).
  Future<void> _pickConnectMode() async {
    final choice = await showDialog<String>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: const Text('Connect mode'),
        children: [
          for (final entry in const [
            (
              'auto',
              'Auto',
              'Scan a pair code and connect immediately over mesh, '
                  'falling back to relay.',
            ),
            (
              'select',
              'Select',
              'Pause after a scan to choose a transport, then Connect.',
            ),
          ])
            ListTile(
              title: Text(entry.$2),
              subtitle: Text(entry.$3),
              trailing: _connectMode == entry.$1
                  ? const Icon(Icons.check)
                  : null,
              onTap: () => Navigator.pop(ctx, entry.$1),
            ),
        ],
      ),
    );
    if (choice == null || !mounted) return;
    await ref.read(settingsStoreProvider).setConnectMode(choice);
    if (!mounted) return;
    setState(() => _connectMode = choice);
  }

  /// Edit the long-lived device token (MADR 0064 D4).
  ///
  /// Cancel returns null; Save returns the trimmed text, where empty means
  /// "clear the stored token".
  Future<void> _editToken() async {
    final store = ref.read(settingsStoreProvider);
    String current = '';
    try {
      current = (await store.getToken()) ?? '';
    } catch (e) {
      // best-effort: empty field if keystore unreadable.
      debugPrint('settings: getToken for edit failed: $e');
    }
    if (!mounted) return;
    final result = await showDialog<String>(
      context: context,
      builder: (_) => _TokenDialog(initial: current),
    );
    if (result == null || !mounted) return;
    try {
      if (result.isEmpty) {
        await store.clearToken();
      } else {
        await store.setToken(result);
      }
    } on SecureStorageUnavailable {
      if (mounted) {
        showTopNotification(
          context,
          'The device keystore is unavailable — the token was not saved.',
          severity: NoticeSeverity.error,
        );
      }
      return;
    }
    if (!mounted) return;
    setState(() => _tokenPresent = result.isNotEmpty);
  }

  /// Route section: what is configured, what just answered a probe, which
  /// transport is live, and a way to move onto the other one (MADR 0062 D6).
  List<Widget> _buildRouteSection(BuildContext context, ColorScheme scheme) {
    final client = ref.watch(mcremoteClientProvider);
    final active = client.activeTransport;
    final relayConfigured = _availability.relayConfigured;

    String subtitle;
    if (!relayConfigured) {
      // Nothing to choose: one route, named plainly rather than dressed up as
      // a decision the user does not have.
      subtitle = 'Mesh only — no relay paired for this host';
    } else if (active != null) {
      subtitle = 'Connected over ${active.label}';
    } else if (_sticky != null) {
      subtitle = 'Last connected over ${_sticky!.label}';
    } else {
      subtitle = 'Mesh and relay both paired';
    }

    return [
      ListTile(
        leading: const Icon(Icons.route),
        title: const Text('Route'),
        subtitle: Text(subtitle),
        trailing: (_availability.meshConfigured || relayConfigured)
            ? TextButton(
                onPressed: _probing
                    ? null
                    : () => unawaited(_refreshTransportProbes()),
                child: Text(_probing ? 'Checking…' : 'Recheck'),
              )
            : null,
      ),
      if (relayConfigured)
        ListTile(
          leading: const Icon(Icons.cloud_outlined),
          title: const Text('Relay'),
          subtitle: Text(
            '${_relayUrl ?? ''}'
            '${(_relayHostId?.isNotEmpty ?? false) ? ' · hid=$_relayHostId' : ''}'
            '${(_relayAuthority?.isNotEmpty ?? false) ? '\nfor $_relayAuthority' : ''}',
          ),
          isThreeLine: (_relayAuthority?.isNotEmpty ?? false),
        ),
      if (relayConfigured) ...[
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
          child: Row(
            children: [
              _probeChip(
                scheme,
                'Mesh',
                configured: _availability.meshConfigured,
                operational: _availability.meshOperational,
              ),
              const SizedBox(width: 8),
              _probeChip(
                scheme,
                'Relay',
                configured: _availability.relayConfigured,
                operational: _availability.relayOperational,
              ),
            ],
          ),
        ),
        if (_availability.bothAvailable)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
            child: SegmentedButton<TransportMode>(
              segments: const [
                ButtonSegment(
                  value: TransportMode.mesh,
                  label: Text('Mesh'),
                  icon: Icon(Icons.lan_outlined),
                ),
                ButtonSegment(
                  value: TransportMode.relay,
                  label: Text('Relay'),
                  icon: Icon(Icons.cloud_outlined),
                ),
              ],
              selected: {_selection ?? active ?? _sticky ?? TransportMode.mesh},
              onSelectionChanged: _reconnecting
                  ? null
                  : (s) => setState(() => _selection = s.first),
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
          child: Align(
            alignment: Alignment.centerLeft,
            child: OutlinedButton.icon(
              onPressed: _reconnecting
                  ? null
                  : () => unawaited(_reconnectNow()),
              icon: _reconnecting
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.sync, size: 18),
              label: const Text('Reconnect now'),
            ),
          ),
        ),
      ],
    ];
  }

  Widget _probeChip(
    ColorScheme scheme,
    String label, {
    required bool configured,
    required bool operational,
  }) {
    if (!configured) {
      return Chip(
        avatar: Icon(Icons.remove, size: 16, color: scheme.onSurfaceVariant),
        label: Text('$label · not paired'),
        visualDensity: VisualDensity.compact,
      );
    }
    // A probe result is soft and session-ephemeral: "no answer" is not a
    // verdict that the transport is broken, only that it did not respond to a
    // ~900ms health check (D2).
    return Chip(
      avatar: Icon(
        operational ? Icons.check_circle_outline : Icons.help_outline,
        size: 16,
        color: operational ? scheme.primary : scheme.onSurfaceVariant,
      ),
      label: Text('$label · ${operational ? 'up' : 'no answer'}'),
      visualDensity: VisualDensity.compact,
    );
  }

  Future<void> _repairHost() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Re-pair this host?'),
        content: const Text(
          'Clears this phone\'s stored credentials — device token, '
          'certificate pin, and client identity. Host address and '
          'preferences are kept. Use this when the host certificate has '
          'changed, or when the host rejects this phone\'s key (for '
          'example after an app update reset its secure storage). '
          'Re-pair afterwards by scanning a new QR — with the pin cleared, '
          'a typed code cannot verify the host\'s certificate.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Re-pair'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    // The scoped secret reset (MADR 0066 D4/F12) plus — only here — the
    // cert pin: this tile is the certificate-rotation recovery, so the old
    // trust record must go. With no pin left, a typed code cannot verify
    // the host; the dialog copy directs to the QR.
    final store = ref.read(settingsStoreProvider);
    final client = ref.read(mcremoteClientProvider);
    await store.clearSecrets();
    await store.clearFingerprint();
    client.clearMemoryCredentials();
    if (!mounted) return;
    context.go('/');
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
    } catch (e) {
      // best-effort: cwd lists are decoration.
      debugPrint('settings: load pinned/recent cwds failed: $e');
    }
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
    } catch (e) {
      // best-effort: transcript usage tile only.
      debugPrint('settings: transcript usage failed: $e');
    }
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
      // Kilo has no mode literally named `default` (MADR 0075 §2.8) — the
      // generic floor entry above would silently never match, so offer its
      // real default agent too (MADR 0076 M1 compounding gap).
      if (providerId == 'kilo') {
        modes.add(const SessionMode(id: 'code', name: 'Code'));
      }
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
          SwitchListTile(
            value: ref.watch(sendWithEnterProvider),
            onChanged: (v) => ref.read(sendWithEnterProvider.notifier).set(v),
            title: const Text('Send with Enter'),
            subtitle: const Text(
              'Off: Enter starts a new line and the send button sends.',
            ),
          ),
          const Divider(),
          _sectionHeader(context, 'Notifications'),
          SwitchListTile(
            value: _notifications,
            onChanged: _setNotifications,
            title: const Text('Agent alerts'),
            subtitle: Text(
              _isIOS
                  // No background connection exists on iOS (MADR 0067 D2) —
                  // claiming one here would be the dishonest liveness 0063
                  // forbids.
                  ? 'Get notified when the agent needs approval or finishes '
                        'a turn.'
                  : 'Get notified when the agent needs approval or finishes '
                        'a turn. Keeps a background connection to your host.',
            ),
          ),
          if (_notifications && _isIOS)
            const ListTile(
              leading: Icon(Icons.info_outline),
              title: Text('Alerts arrive while the app is open'),
              subtitle: Text(
                'iOS pauses the app in the background, so the host '
                'connection and alerts resume when you return. '
                'Background alerts are a planned follow-up.',
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
                'Notifications are blocked by ${_isIOS ? 'iOS' : 'Android'}',
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
          ..._providerCredentialSection(context),
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
          // MADR 0066 D5: the exact platform exception behind a keystore
          // incident, so the next report is a reading, not a paraphrase.
          ListTile(
            leading: const Icon(Icons.security_outlined),
            title: const Text('Secret storage'),
            subtitle: Text(
              _storageFailure == null
                  ? 'No failures recorded'
                  : '${_storageFailure!.op} failed '
                        '${_formatLocalTime(_storageFailure!.at)}: '
                        '${_storageFailure!.error}',
            ),
          ),
          // Shown only when the daemon keeps signed receipts (MADR 0078 D7).
          if (ref.watch(mcremoteClientProvider).serverCaps?.receipts ?? false)
            ListTile(
              leading: const Icon(Icons.receipt_long_outlined),
              title: const Text('Signed receipts'),
              subtitle: const Text('This device\'s chain, verified on device'),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => Navigator.of(context).push(
                MaterialPageRoute<void>(builder: (_) => const ReceiptsScreen()),
              ),
            ),
          const Divider(),
          _sectionHeader(context, 'Connection'),
          ListTile(
            leading: const Icon(Icons.dns_outlined),
            title: const Text('Host'),
            subtitle: Text(_host == null || _host!.isEmpty ? '—' : _host!),
          ),
          ..._buildRouteSection(context, scheme),
          ListTile(
            leading: const Icon(Icons.bolt_outlined),
            title: const Text('Connect mode'),
            subtitle: Text(
              _connectMode == 'select'
                  ? 'Select — choose a transport first'
                  : 'Auto — scan and connect over mesh',
            ),
            onTap: _pickConnectMode,
          ),
          ListTile(
            leading: const Icon(Icons.key_outlined),
            title: const Text('Long-lived token'),
            subtitle: Text(_tokenPresent ? 'present' : 'absent'),
            onTap: _editToken,
          ),
          ListTile(
            leading: Icon(
              Icons.verified_user_outlined,
              color: _pinTlsMode == 'off' ? scheme.error : null,
            ),
            title: const Text('Certificate pin'),
            subtitle: Text(
              _pinFingerprint == null || _pinFingerprint!.isEmpty
                  ? 'not pinned'
                  : '${_pinTlsMode ?? 'unknown'} · ${_formatFingerprint(_pinFingerprint!)}',
            ),
            onLongPress: _pinFingerprint == null || _pinFingerprint!.isEmpty
                ? null
                : () async {
                    await Clipboard.setData(
                      ClipboardData(text: _formatFingerprint(_pinFingerprint!)),
                    );
                    if (context.mounted) {
                      showTopNotification(context, 'Fingerprint copied');
                    }
                  },
          ),
          // MADR 0066 D9: the fingerprint the daemon enrolled, matchable
          // against `pair list`'s KEY column — a mismatch becomes a visual
          // diff. Long-press copies, cloning the pin tile above.
          ListTile(
            leading: const Icon(Icons.badge_outlined),
            title: const Text('Client identity'),
            subtitle: Text(
              !_clientIdentityPresent
                  ? 'absent'
                  : (_identityFingerprint == null ||
                        _identityFingerprint!.isEmpty)
                  ? 'unreadable'
                  : _identityFingerprint!,
            ),
            onLongPress:
                (_identityFingerprint == null || _identityFingerprint!.isEmpty)
                ? null
                : () async {
                    await Clipboard.setData(
                      ClipboardData(text: _identityFingerprint!),
                    );
                    if (context.mounted) {
                      showTopNotification(context, 'Fingerprint copied');
                    }
                  },
          ),
          ListTile(
            leading: Icon(Icons.link_off, color: scheme.error),
            title: Text(
              'Re-pair this host',
              style: TextStyle(color: scheme.error),
            ),
            subtitle: const Text(
              'Clear token, pin + client identity; keep host',
            ),
            onTap: _repairHost,
          ),
          const Divider(),
          _sectionHeader(context, 'Host'),
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
          // MADR 0065: check / download / verify / install APK updates.
          if (!_isIOS) const AppUpdateTile(),
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

  /// Local wall-clock timestamp for the diagnostics row: `2026-08-02 12:30`.
  static String _formatLocalTime(DateTime at) {
    final l = at.toLocal();
    String two(int v) => v.toString().padLeft(2, '0');
    return '${l.year}-${two(l.month)}-${two(l.day)} '
        '${two(l.hour)}:${two(l.minute)}';
  }

  /// Provider credentials (MADR 0074). Renders nothing at all unless the
  /// daemon advertised the `provider_auth` capability *and* some provider
  /// actually reported state — a daemon without the feature looks exactly as
  /// it did before it existed (D6).
  List<Widget> _providerCredentialSection(BuildContext context) {
    final withAuth = _providers.where((p) => p.auth != null).toList();
    if (withAuth.isEmpty) return const [];
    return [
      const Divider(),
      _sectionHeader(context, 'Provider credentials'),
      for (final p in withAuth) ...[
        // Switching is only meaningful with somewhere to switch to, so the
        // control appears only when at least two upstreams are configured
        // (MADR 0074 D14). This is the no-credentials escape from a
        // quota-blocked vendor that MADR 0073 needed.
        if (_configuredUpstreams(p).length > 1)
          ListTile(
            key: Key('provider-active-upstream-${p.id}'),
            leading: const Icon(Icons.swap_horiz),
            title: Text('Active upstream · ${p.id}'),
            subtitle: Text(
              p.auth!.activeUpstream?.isNotEmpty == true
                  ? p.auth!.activeUpstream!
                  : 'Provider default',
            ),
            onTap: () => _pickActiveUpstream(p),
          ),
        for (final up in p.auth!.upstreams)
          ListTile(
            key: Key('provider-auth-tile-${p.id}-${up.id}'),
            leading: Icon(_authStatusIcon(up.status)),
            title: Text('${up.display} · ${p.id}'),
            subtitle: Text(
              _authStatusLabel(
                up.status,
                isActive: p.auth!.activeUpstream == up.id,
              ),
            ),
            trailing: up.isConfigured
                ? IconButton(
                    tooltip: 'Remove credential',
                    icon: const Icon(Icons.link_off),
                    onPressed: () => _clearCredential(p.id, up),
                  )
                : null,
            onTap: () => _openAuthSheet(p.id, up),
          ),
      ],
    ];
  }

  static IconData _authStatusIcon(String status) => switch (status) {
    AuthStatus.configured => Icons.verified_user_outlined,
    AuthStatus.quota => Icons.hourglass_bottom,
    AuthStatus.error => Icons.error_outline,
    _ => Icons.key_off_outlined,
  };

  static String _authStatusLabel(String status, {required bool isActive}) {
    final base = switch (status) {
      AuthStatus.configured => 'Configured',
      AuthStatus.quota => 'Quota reached',
      AuthStatus.error => 'Error',
      _ => 'Needs setup',
    };
    return isActive ? '$base · active' : base;
  }

  static List<UpstreamAuth> _configuredUpstreams(ProviderInfo p) =>
      (p.auth?.upstreams ?? const <UpstreamAuth>[])
          .where((u) => u.isConfigured)
          .toList();

  /// Move an agent to another already-authenticated upstream. No credential
  /// work is involved — that is the whole point (MADR 0074 D14).
  Future<void> _pickActiveUpstream(ProviderInfo p) async {
    final options = _configuredUpstreams(p);
    final chosen = await showModalBottomSheet<String>(
      context: context,
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final up in options)
              ListTile(
                key: Key('active-upstream-option-${up.id}'),
                title: Text(up.display),
                trailing: p.auth!.activeUpstream == up.id
                    ? const Icon(Icons.check)
                    : null,
                onTap: () => Navigator.of(sheetContext).pop(up.id),
              ),
          ],
        ),
      ),
    );
    if (chosen == null || chosen == p.auth!.activeUpstream || !mounted) return;
    final client = ref.read(mcremoteClientProvider);
    try {
      await client.setActiveUpstream(providerId: p.id, upstreamId: chosen);
      if (!mounted) return;
      showTopNotification(context, 'Switched to $chosen');
      await _load();
    } catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        'Could not switch upstream: ${friendlyOpError(e)}',
      );
    }
  }

  Future<void> _openAuthSheet(String providerId, UpstreamAuth up) async {
    final submission = await showModalBottomSheet<ProviderAuthSubmission>(
      context: context,
      isScrollControlled: true,
      builder: (_) => ProviderAuthSheet(providerId: providerId, upstream: up),
    );
    if (submission == null || !mounted) return;
    final client = ref.read(mcremoteClientProvider);
    try {
      if (submission.method.isApiKey) {
        await client.setProviderCredential(
          providerId: providerId,
          upstreamId: up.id,
          secret: submission.secret,
          methodId: submission.method.id,
          inputs: submission.inputs,
        );
      } else {
        // Device OAuth lands in the W2 workstream; until then say so rather
        // than silently doing nothing.
        if (mounted) {
          showTopNotification(context, 'Device sign-in is not available yet');
        }
        return;
      }
      if (!mounted) return;
      showTopNotification(context, 'Credential saved');
      await _load();
    } catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        'Could not save credential: ${friendlyOpError(e)}',
      );
    }
  }

  Future<void> _clearCredential(String providerId, UpstreamAuth up) async {
    final client = ref.read(mcremoteClientProvider);
    try {
      await client.clearProviderCredential(
        providerId: providerId,
        upstreamId: up.id,
      );
      if (!mounted) return;
      showTopNotification(context, 'Credential removed');
      await _load();
    } catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        'Could not remove credential: ${friendlyOpError(e)}',
      );
    }
  }

  /// Uppercase hex, colon-separated — matches mcremote startup log format.
  static String _formatFingerprint(String raw) {
    final hex = raw.replaceAll(RegExp(r'[^0-9a-fA-F]'), '').toUpperCase();
    if (hex.isEmpty) return raw;
    final buf = StringBuffer();
    for (var i = 0; i < hex.length; i += 2) {
      if (i > 0) buf.write(':');
      buf.write(hex.substring(i, i + 2 > hex.length ? hex.length : i + 2));
    }
    return buf.toString();
  }
}

/// The long-lived token editor (MADR 0064 D4): the obscured field with the
/// show/hide toggle from the old Advanced tile, plus a clear affordance.
///
/// A widget of its own so the controller is disposed on route unmount — the
/// natural lifecycle — rather than by the caller racing an IME composition
/// the way the enter-code sheet's comment warns about.
class _TokenDialog extends StatefulWidget {
  const _TokenDialog({required this.initial});

  final String initial;

  @override
  State<_TokenDialog> createState() => _TokenDialogState();
}

class _TokenDialogState extends State<_TokenDialog> {
  late final TextEditingController _ctrl = TextEditingController(
    text: widget.initial,
  );
  bool _show = false;

  @override
  void initState() {
    super.initState();
    // The clear icon exists only while there is something to clear.
    _ctrl.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Long-lived token'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'From `mcremote pair create` on the host. Saving with an empty '
            'field removes the stored token.',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _ctrl,
            decoration: InputDecoration(
              labelText: 'Device token',
              hintText: 'mcr_…',
              border: const OutlineInputBorder(),
              suffixIcon: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  // Empties the field only — Save is still the commit point,
                  // and Cancel still backs the whole edit out (MADR 0064 §5).
                  if (_ctrl.text.isNotEmpty)
                    IconButton(
                      tooltip: 'Clear',
                      onPressed: _ctrl.clear,
                      icon: const Icon(Icons.clear),
                    ),
                  IconButton(
                    tooltip: _show ? 'Hide' : 'Show',
                    onPressed: () => setState(() => _show = !_show),
                    icon: Icon(_show ? Icons.visibility_off : Icons.visibility),
                  ),
                ],
              ),
            ),
            autocorrect: false,
            enableSuggestions: false,
            obscureText: !_show,
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, _ctrl.text.trim()),
          child: const Text('Save'),
        ),
      ],
    );
  }
}
