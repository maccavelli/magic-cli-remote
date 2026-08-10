import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/ws/receipts.dart';
import '../../state/app_providers.dart';

/// The "Signed receipts" screen (MADR 0078 D7): this device's own receipt
/// chain, newest first, each entry's signature re-verified locally on device
/// (D9) — the ✓/✗/⚠ badge is recomputed here, never a daemon-asserted verdict.
class ReceiptsScreen extends ConsumerStatefulWidget {
  const ReceiptsScreen({super.key});

  @override
  ConsumerState<ReceiptsScreen> createState() => _ReceiptsScreenState();
}

class _ReceiptsScreenState extends ConsumerState<ReceiptsScreen> {
  late Future<List<ReceiptEntry>> _future;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  Future<List<ReceiptEntry>> _load() {
    return ref.read(mcremoteClientProvider).listReceipts();
  }

  void _refresh() {
    setState(() {
      _future = _load();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Signed receipts'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            tooltip: 'Reload',
            onPressed: _refresh,
          ),
        ],
      ),
      body: FutureBuilder<List<ReceiptEntry>>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            return _CenteredMessage(
              icon: Icons.error_outline,
              title: 'Could not load receipts',
              detail: '${snapshot.error}',
            );
          }
          final entries = snapshot.data ?? const [];
          if (entries.isEmpty) {
            return const _CenteredMessage(
              icon: Icons.receipt_long_outlined,
              title: 'No receipts yet',
              detail:
                  'Signed receipts are a daemon opt-in. When enabled, each '
                  'permission decision you make on this device — and each '
                  'session handoff — is recorded here, signed by this device '
                  'and verified on device.',
            );
          }
          return ListView.separated(
            itemCount: entries.length,
            separatorBuilder: (_, _) => const Divider(height: 1),
            itemBuilder: (context, i) => _ReceiptTile(entry: entries[i]),
          );
        },
      ),
    );
  }
}

class _ReceiptTile extends StatelessWidget {
  const _ReceiptTile({required this.entry});

  final ReceiptEntry entry;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return ListTile(
      leading: _VerdictBadge(verdict: entry.verdict, scheme: scheme),
      title: Text(_title(entry)),
      subtitle: Text(_subtitle(entry)),
      isThreeLine: true,
    );
  }

  String _title(ReceiptEntry e) {
    switch (e.predicateType) {
      case kPermissionDecisionPredicate:
        final tool = e.predicate['tool_name'] as String? ?? 'tool';
        final option = e.predicate['option_id'] as String? ?? '';
        return option.isEmpty
            ? 'Permission: $tool'
            : 'Permission: $tool → $option';
      case kHandoffReleasePredicate:
        final to = e.predicate['to_device_id'] as String? ?? '';
        return to.isEmpty
            ? 'Session released (open)'
            : 'Session released → $to';
      case kHandoffClaimPredicate:
        return 'Session claimed';
      case kReceiptUnavailablePredicate:
        final reason = e.predicate['reason'] as String? ?? 'unavailable';
        return 'Receipt unavailable ($reason)';
      default:
        return e.predicateType.isEmpty ? 'Receipt' : e.predicateType;
    }
  }

  String _subtitle(ReceiptEntry e) {
    final detail = e.predicate['detail'] as String?;
    final ts =
        e.predicate['decided_at'] ??
        e.predicate['released_at'] ??
        e.predicate['claimed_at'];
    final parts = <String>[];
    if (detail != null && detail.isNotEmpty) parts.add(detail);
    if (ts is String && ts.isNotEmpty) parts.add(ts);
    parts.add(_verdictLabel(e.verdict));
    return parts.join('\n');
  }

  String _verdictLabel(ReceiptVerdict v) {
    switch (v) {
      case ReceiptVerdict.verified:
        return 'Signature verified on this device';
      case ReceiptVerdict.failed:
        return 'Signature did NOT verify';
      case ReceiptVerdict.unverifiable:
        return 'Cannot verify here (daemon-signed or unknown type)';
    }
  }
}

class _VerdictBadge extends StatelessWidget {
  const _VerdictBadge({required this.verdict, required this.scheme});

  final ReceiptVerdict verdict;
  final ColorScheme scheme;

  @override
  Widget build(BuildContext context) {
    switch (verdict) {
      case ReceiptVerdict.verified:
        return Icon(
          Icons.verified,
          color: scheme.primary,
          semanticLabel: 'Verified',
        );
      case ReceiptVerdict.failed:
        return Icon(
          Icons.gpp_bad,
          color: scheme.error,
          semanticLabel: 'Failed',
        );
      case ReceiptVerdict.unverifiable:
        return Icon(
          Icons.help_outline,
          color: scheme.outline,
          semanticLabel: 'Unverifiable',
        );
    }
  }
}

class _CenteredMessage extends StatelessWidget {
  const _CenteredMessage({
    required this.icon,
    required this.title,
    required this.detail,
  });

  final IconData icon;
  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(icon, size: 48, color: scheme.outline),
            const SizedBox(height: 16),
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(
              detail,
              textAlign: TextAlign.center,
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ],
        ),
      ),
    );
  }
}
