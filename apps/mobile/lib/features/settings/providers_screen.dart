/// The Providers spoke (MADR 0082 D2): one identity card per agent.
///
/// The settings hub shows a one-line summary; this screen shows the fleet —
/// brand icon, worst-status chip, active upstream and credential count per
/// agent — and each card drills into [ProviderDetailScreen] where the actual
/// management lives. Credential state renders only when the daemon reported
/// it (MADR 0074 D6): against an older daemon the cards simply carry the
/// ready/not-ready chip.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../state/app_providers.dart';
import '../widgets/status_chip.dart';
import '../widgets/vendor_icon.dart';
import 'provider_status.dart';
import 'section_card.dart';

class ProvidersScreen extends ConsumerStatefulWidget {
  const ProvidersScreen({super.key});

  @override
  ConsumerState<ProvidersScreen> createState() => _ProvidersScreenState();
}

class _ProvidersScreenState extends ConsumerState<ProvidersScreen> {
  List<ProviderInfo> _providers = const [];
  bool _connected = true;
  bool _loading = true;
  String? _error;

  /// Live refresh on credential pushes (MADR 0074 D10), mirroring the hub.
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
      if (!mounted) return;
      setState(() {
        _connected = true;
        _providers = providers;
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
      appBar: AppBar(title: const Text('Providers')),
      body: _body(context),
    );
  }

  Widget _body(BuildContext context) {
    if (!_connected) {
      return const ListTile(
        leading: Icon(Icons.cloud_off_outlined),
        title: Text('Not connected'),
        subtitle: Text('Connect to a host to see and manage its agents.'),
      );
    }
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return ListTile(
        leading: const Icon(Icons.error_outline),
        title: const Text('Could not list providers'),
        subtitle: Text(_error!),
      );
    }
    if (_providers.isEmpty) {
      return const ListTile(
        title: Text('No agents'),
        subtitle: Text('The host reported no session providers.'),
      );
    }
    final scheme = Theme.of(context).colorScheme;
    return ListView(
      padding: listBottomPadding(context, extra: 8).copyWith(top: 8),
      children: [
        for (final p in _providers)
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 4),
            child: Material(
              color: scheme.surfaceContainerLow,
              borderRadius: BorderRadius.circular(16),
              clipBehavior: Clip.antiAlias,
              child: ListTile(
                key: Key('provider-card-${p.id}'),
                leading: VendorIcon(id: p.id, size: 32),
                title: Text(p.id),
                // Chips on one line, the summary on its own bounded line —
                // an unbounded Wrap made every card taller than designed on
                // narrow devices (MADR 0083 L6).
                subtitle: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Wrap(
                        spacing: 6,
                        runSpacing: 4,
                        children: [
                          if (p.auth != null)
                            StatusChip.auth(agentAuthStatus(p.auth!))
                          else if (p.ready)
                            const StatusChip(
                              kind: StatusKind.ok,
                              label: 'Ready',
                            )
                          else
                            const StatusChip(
                              kind: StatusKind.neutral,
                              label: 'Not ready',
                            ),
                        ],
                      ),
                    ),
                    if (p.auth != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Text(
                          _credentialSummary(p),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                  ],
                ),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => context.push('/settings/providers/${p.id}'),
              ),
            ),
          ),
      ],
    );
  }

  static String _credentialSummary(ProviderInfo p) {
    final ups = p.auth!.upstreams;
    final configured = ups.where((u) => u.isConfigured).length;
    final active = p.auth!.activeUpstream;
    final creds = configured == 1 ? '1 credential' : '$configured credentials';
    return (active == null || active.isEmpty) ? creds : '$creds · $active';
  }
}
