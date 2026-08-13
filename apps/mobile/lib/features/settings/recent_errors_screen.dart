/// The failures the app recorded about itself (MADR 0084 D2).
///
/// The reporting channel this app did not have: with no telemetry and a
/// self-hosted daemon, "it went blank" was previously unanswerable. Each entry
/// is copyable so a report carries a reading rather than a paraphrase — the
/// same rationale as MADR 0066 D5's storage-failure row, applied to the larger
/// class.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/diagnostics/error_recorder.dart';
import '../../state/app_providers.dart';
import '../../theme/celestial.dart';
import '../../theme/top_notification.dart';
import 'section_card.dart';

class RecentErrorsScreen extends ConsumerStatefulWidget {
  const RecentErrorsScreen({super.key});

  @override
  ConsumerState<RecentErrorsScreen> createState() => _RecentErrorsScreenState();
}

class _RecentErrorsScreenState extends ConsumerState<RecentErrorsScreen> {
  List<RecordedError>? _entries;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    final entries = await ref.read(errorRecorderProvider).recent();
    if (!mounted) return;
    setState(() => _entries = entries);
  }

  Future<void> _clear() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        key: const Key('clear-errors-confirm'),
        title: const Text('Clear recorded errors?'),
        content: const Text(
          'Removes this device\'s record of recent failures. Nothing else is '
          'affected.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: destructiveFilled(Theme.of(ctx).colorScheme),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Clear'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    await ref.read(errorRecorderProvider).clear();
    if (!mounted) return;
    setState(() => _entries = const []);
    showTopNotification(context, 'Recorded errors cleared');
  }

  @override
  Widget build(BuildContext context) {
    final entries = _entries;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Recent errors'),
        actions: [
          if (entries != null && entries.isNotEmpty)
            IconButton(
              key: const Key('recent-errors-clear'),
              tooltip: 'Clear',
              icon: const Icon(Icons.delete_outline),
              onPressed: _clear,
            ),
        ],
      ),
      body: switch (entries) {
        null => const Center(child: CircularProgressIndicator()),
        [] => const ListTile(
          key: Key('recent-errors-empty'),
          leading: Icon(Icons.check_circle_outline),
          title: Text('No errors recorded'),
          subtitle: Text(
            'Errors the app catches are listed here so a report can carry '
            'the exact message.',
          ),
        ),
        _ => ListView(
          padding: listBottomPadding(context),
          children: [for (final e in entries) _EntryTile(entry: e)],
        ),
      },
    );
  }
}

class _EntryTile extends StatelessWidget {
  const _EntryTile({required this.entry});

  final RecordedError entry;

  @override
  Widget build(BuildContext context) {
    final local = entry.at.toLocal();
    String two(int v) => v.toString().padLeft(2, '0');
    final when =
        '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
    return ExpansionTile(
      key: Key('recent-error-${entry.at.toIso8601String()}'),
      leading: const Icon(Icons.error_outline),
      title: Text('${entry.kind} · $when'),
      subtitle: Text(
        entry.message,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
      childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
      expandedCrossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SelectableText(
          entry.message,
          style: Theme.of(context).textTheme.bodySmall,
        ),
        if (entry.stack.isNotEmpty) ...[
          const SizedBox(height: 8),
          SelectableText(
            entry.stack,
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(fontFamily: 'monospace'),
          ),
        ],
        const SizedBox(height: 8),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            icon: const Icon(Icons.copy_all_outlined, size: 18),
            label: const Text('Copy'),
            onPressed: () async {
              await Clipboard.setData(
                ClipboardData(text: entry.toClipboardText()),
              );
              if (context.mounted) {
                showTopNotification(context, 'Error details copied');
              }
            },
          ),
        ),
      ],
    );
  }
}
