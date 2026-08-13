/// One agent's whole management surface (MADR 0082 D3).
///
/// Before this screen existed, an agent's session-affecting state was split
/// across the settings page: default mode under Sessions, credentials and the
/// active upstream under Provider credentials (MADR 0082 F8). Here the pieces
/// live together: status, active upstream, default mode, the configured
/// credentials, and the entry into the full vendor catalog.
///
/// The credential flows themselves — auth sheet, catalog sheet, device
/// sign-in, removal — moved here verbatim from the settings screen; their
/// recorded guarantees (MADR 0074 D8 destructive confirm, D11 secret hygiene,
/// D14 switch-only-to-configured, D16 paged catalog) are unchanged.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/top_notification.dart';
import '../../data/protocol/picker.dart';
import '../widgets/option_picker_sheet.dart' show showOptionPicker;
import '../widgets/status_chip.dart';
import '../widgets/vendor_icon.dart';
import 'device_flow_sheet.dart';
import 'provider_auth_sheet.dart';
import 'provider_status.dart';
import 'section_card.dart';
import 'upstream_catalog_sheet.dart';

class ProviderDetailScreen extends ConsumerStatefulWidget {
  const ProviderDetailScreen({super.key, required this.providerId});

  final String providerId;

  @override
  ConsumerState<ProviderDetailScreen> createState() =>
      _ProviderDetailScreenState();
}

class _ProviderDetailScreenState extends ConsumerState<ProviderDetailScreen> {
  ProviderInfo? _provider;
  bool _connected = true;
  bool _loading = true;
  String? _error;

  /// Stored default mode for this agent (empty = provider default).
  String _defaultMode = '';

  /// Live refresh on credential pushes (MADR 0074 D10).
  StreamSubscription<Map<String, dynamic>>? _authStatusSub;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
    _authStatusSub = ref.read(mcremoteClientProvider).providerAuthStatus.listen(
      (_) {
        if (mounted) unawaited(_load());
      },
    );
  }

  @override
  void dispose() {
    unawaited(_authStatusSub?.cancel());
    super.dispose();
  }

  Future<void> _load() async {
    final client = ref.read(mcremoteClientProvider);
    final store = ref.read(settingsStoreProvider);
    if (client.state != McConnectionState.connected) {
      if (!mounted) return;
      setState(() {
        _connected = false;
        _loading = false;
      });
      return;
    }
    try {
      final providers = await client.listProviders();
      String mode = '';
      try {
        mode = await store.getDefaultSessionMode(widget.providerId) ?? '';
      } catch (e) {
        // best-effort: the tile falls back to "Provider default".
        debugPrint('provider detail: default mode read failed: $e');
      }
      if (!mounted) return;
      setState(() {
        _connected = true;
        _provider = providers
            .where((p) => p.id == widget.providerId)
            .firstOrNull;
        _defaultMode = mode;
        _loading = false;
        _error = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = friendlyOpError(e);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(widget.providerId)),
      body: _body(context),
    );
  }

  Widget _body(BuildContext context) {
    if (!_connected) {
      return const ListTile(
        leading: Icon(Icons.cloud_off_outlined),
        title: Text('Not connected'),
        subtitle: Text('Connect to a host to manage this agent.'),
      );
    }
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return ListTile(
        leading: const Icon(Icons.error_outline),
        title: const Text('Could not load this agent'),
        subtitle: Text(_error!),
      );
    }
    final p = _provider;
    if (p == null) {
      return ListTile(
        title: Text('No agent named ${widget.providerId}'),
        subtitle: const Text('It may have been disabled on the host.'),
      );
    }
    final auth = p.auth;
    return ListView(
      padding: listBottomPadding(context),
      children: [
        SettingsSection(
          title: 'Status',
          children: [
            ListTile(
              leading: VendorIcon(id: p.id, size: 32),
              title: Text(p.id),
              subtitle: Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Wrap(
                  spacing: 6,
                  runSpacing: 4,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    if (auth != null)
                      StatusChip.auth(worstAuthStatus(auth))
                    else if (p.ready)
                      const StatusChip(kind: StatusKind.ok, label: 'Ready')
                    else
                      const StatusChip(
                        kind: StatusKind.neutral,
                        label: 'Not ready',
                      ),
                    if (auth?.activeUpstream?.isNotEmpty ?? false)
                      Text('active: ${auth!.activeUpstream}'),
                  ],
                ),
              ),
            ),
          ],
        ),
        SettingsSection(
          title: 'Session defaults',
          children: [
            ListTile(
              key: Key('provider-default-mode-${p.id}'),
              leading: const Icon(Icons.tune),
              title: const Text('Default mode'),
              subtitle: Text(
                _defaultMode.isNotEmpty ? _defaultMode : 'Provider default',
              ),
              onTap: _pickDefaultMode,
            ),
          ],
        ),
        if (auth != null)
          SettingsSection(
            title: 'Credentials',
            children: [
              // Switching is only meaningful with somewhere to switch to, so
              // the control appears only when at least two upstreams are
              // configured (MADR 0074 D14). This is the no-credentials escape
              // from a quota-blocked vendor that MADR 0073 needed.
              if (_configuredUpstreams(p).length > 1)
                ListTile(
                  key: Key('provider-active-upstream-${p.id}'),
                  leading: const Icon(Icons.swap_horiz),
                  title: const Text('Active upstream'),
                  subtitle: Text(
                    auth.activeUpstream?.isNotEmpty == true
                        ? auth.activeUpstream!
                        : 'Provider default',
                  ),
                  onTap: () => _pickActiveUpstream(p),
                ),
              for (final up in auth.upstreams)
                ListTile(
                  key: Key('provider-auth-tile-${p.id}-${up.id}'),
                  leading: VendorIcon(id: up.id, display: up.display, size: 28),
                  title: Text(up.display),
                  // One chip system for state (MADR 0082 D4): status is a
                  // coloured dot chip, "active" a filled pill of its own.
                  subtitle: Padding(
                    padding: const EdgeInsets.only(top: 2),
                    child: Wrap(
                      spacing: 6,
                      runSpacing: 4,
                      children: [
                        StatusChip.auth(up.status),
                        if (auth.activeUpstream == up.id)
                          const StatusChip(
                            kind: StatusKind.active,
                            label: 'Active',
                          ),
                      ],
                    ),
                  ),
                  trailing: up.isConfigured
                      ? IconButton(
                          tooltip: 'Remove credential',
                          icon: const Icon(Icons.link_off),
                          onPressed: () => _clearCredential(up),
                        )
                      : null,
                  onTap: () => _openAuthSheet(up),
                ),
              // Everything the agent supports, not just what is configured
              // (MADR 0074 D16). Without this row a vendor with no credential
              // yet — togetherai, deepseek, and ~170 others on the
              // OpenCode-family agents — has no tile to tap and cannot be set
              // up from the phone at all.
              ListTile(
                key: Key('provider-add-credential-${p.id}'),
                leading: const Icon(Icons.add_circle_outline),
                title: const Text('Add credential'),
                subtitle: const Text('Browse every vendor this agent supports'),
                onTap: _browseUpstreamCatalog,
              ),
            ],
          ),
      ],
    );
  }

  static List<UpstreamAuth> _configuredUpstreams(ProviderInfo p) =>
      (p.auth?.upstreams ?? const <UpstreamAuth>[])
          .where((u) => u.isConfigured)
          .toList();

  Future<void> _pickDefaultMode() async {
    // Modes are session-advertised. Collect from any live transcript for this
    // provider; fall back to free-text common ids if none are open.
    final providerId = widget.providerId;
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
    final current = _defaultMode;
    final result = await showOptionPicker(
      context,
      title: 'Default mode · $providerId',
      catalog: PickerCatalog(
        source: PickerSource.staticSource,
        options: [
          PickerOption(id: '', label: 'Provider default'),
          for (final m in modes)
            PickerOption(
              id: m.id,
              label: m.name.isEmpty ? m.id : m.name,
              description: m.dangerous ? 'Runs without approvals' : '',
            ),
        ],
        defaultIds: [if (current.isNotEmpty) current],
      ),
      initialSelected: [if (current.isNotEmpty) current],
    );
    if (result == null || !mounted) return;
    final choice = result.selectedIds.isEmpty ? '' : result.selectedIds.first;
    final picked = modes.where((m) => m.id == choice).firstOrNull;
    if (picked?.dangerous ?? false) {
      // The confirmation survives the picker change (MADR 0052 B2): it now
      // runs after selection, before anything persists.
      final ok = await showDialog<bool>(
        context: context,
        builder: (dctx) => AlertDialog(
          title: const Text('Set dangerous default?'),
          content: Text(
            'New $providerId sessions will start in "${picked!.name.isEmpty ? picked.id : picked.name}" '
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
      if (ok != true || !mounted) return;
    }
    final store = ref.read(settingsStoreProvider);
    await store.setDefaultSessionMode(
      providerId,
      choice.isEmpty ? null : choice,
    );
    if (!mounted) return;
    setState(() => _defaultMode = choice);
  }

  /// Move an agent to another already-authenticated upstream. No credential
  /// work is involved — that is the whole point (MADR 0074 D14). One picker
  /// idiom for the whole app (MADR 0082 D6): the 0079 sheet, not a bespoke
  /// column.
  Future<void> _pickActiveUpstream(ProviderInfo p) async {
    final options = _configuredUpstreams(p);
    final active = p.auth!.activeUpstream ?? '';
    final result = await showOptionPicker(
      context,
      title: 'Active upstream · ${p.id}',
      catalog: PickerCatalog(
        source: PickerSource.live,
        options: [
          for (final up in options) PickerOption(id: up.id, label: up.display),
        ],
        defaultIds: [if (active.isNotEmpty) active],
      ),
      initialSelected: [if (active.isNotEmpty) active],
    );
    if (result == null || !mounted) return;
    final chosen = result.selectedIds.isEmpty ? null : result.selectedIds.first;
    if (chosen == null || chosen == p.auth!.activeUpstream) return;
    final client = ref.read(mcremoteClientProvider);
    try {
      await client.setActiveUpstream(
        providerId: widget.providerId,
        upstreamId: chosen,
      );
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

  /// Browse the agent's full vendor catalog and, on a pick, drop straight
  /// into the same setup sheet a configured row opens (MADR 0074 D16).
  Future<void> _browseUpstreamCatalog() async {
    final client = ref.read(mcremoteClientProvider);
    final chosen = await showModalBottomSheet<UpstreamAuth>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (_) => UpstreamCatalogSheet(
        providerId: widget.providerId,
        configured: _provider == null
            ? const []
            : _configuredUpstreams(_provider!),
        fetch:
            ({
              required String providerId,
              String query = '',
              int offset = 0,
              int limit = 0,
            }) => client.listUpstreamCatalog(
              providerId: providerId,
              query: query,
              offset: offset,
              limit: limit,
            ),
      ),
    );
    if (chosen == null || !mounted) return;
    await _openAuthSheet(chosen);
  }

  Future<void> _openAuthSheet(UpstreamAuth up) async {
    final providerId = widget.providerId;
    final submission = await showModalBottomSheet<ProviderAuthSubmission>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
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
      } else if (submission.method.isDeviceOAuth) {
        await _runDeviceSignIn(
          upstream: up,
          method: submission.method,
          inputs: submission.inputs,
        );
        return;
      } else {
        // Browser OAuth needs a callback to the host's own loopback, which a
        // phone browser cannot reach (MADR 0074 §10, W3).
        if (mounted) {
          showTopNotification(
            context,
            'This vendor must be set up on the host',
          );
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

  /// Run a device sign-in end to end (MADR 0074 Strategy A, D8, D13).
  ///
  /// The daemon does the polling; this waits for the `oauth.device_flow` push
  /// that carries the code, shows it, and closes on the result. Codex is
  /// guarded first: its flow deletes the host's existing ChatGPT session the
  /// moment it starts, whether or not the user finishes (D8).
  Future<void> _runDeviceSignIn({
    required UpstreamAuth upstream,
    required AuthMethod method,
    required Map<String, String> inputs,
  }) async {
    final providerId = widget.providerId;
    final client = ref.read(mcremoteClientProvider);
    final destructive = providerId == 'codex';
    if (destructive) {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          key: const Key('device-auth-destructive-confirm'),
          title: const Text('Sign out of ChatGPT on the host?'),
          content: const Text(
            'This signs the host out of ChatGPT immediately, before you '
            'finish signing in. If you abandon the flow, the host stays '
            'signed out until you complete it.',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(false),
              child: const Text('Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(true),
              child: const Text('Continue'),
            ),
          ],
        ),
      );
      if (confirmed != true || !mounted) return;
    }

    // Subscribe before starting: the push can arrive before the request's own
    // ok frame, and a late listener would miss the code entirely.
    final flowFuture = client.deviceFlows
        .firstWhere((f) => f['provider_id'] == providerId)
        .timeout(const Duration(seconds: 60));
    try {
      await client.startProviderDeviceAuth(
        providerId: providerId,
        upstreamId: upstream.id,
        methodId: method.id,
        inputs: inputs,
        confirmDestructive: destructive,
      );
    } catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        'Could not start sign-in: ${friendlyOpError(e)}',
      );
      return;
    }

    late final DeviceFlowInfo flow;
    try {
      flow = DeviceFlowInfo.fromJson(await flowFuture);
    } catch (_) {
      if (!mounted) return;
      showTopNotification(context, 'The host sent no sign-in code');
      return;
    }
    if (!mounted) return;

    final result = client.deviceFlowResults
        .firstWhere((r) => r['flow_id'] == flow.flowId)
        .then<String?>(
          (r) => r['ok'] == true ? null : (r['error'] as String? ?? 'failed'),
        );
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (_) => DeviceFlowSheet(
        flow: flow,
        result: result,
        onCancel: () => client.cancelDeviceAuth(flow.flowId),
      ),
    );
    if (!mounted) return;
    await _load();
  }

  Future<void> _clearCredential(UpstreamAuth up) async {
    final providerId = widget.providerId;
    // MADR 0082 F5: removal deletes the key on the host and used to fire on a
    // single tap of a trailing icon — the only destructive action on this
    // surface without a confirmation. Now it confirms like the rest.
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        key: const Key('remove-credential-confirm'),
        title: Text('Remove ${up.display} credential?'),
        content: Text(
          '$providerId will lose access to ${up.display} until a new '
          'credential is added. The key is deleted on the host.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: destructiveFilled(Theme.of(ctx).colorScheme),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
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
}
