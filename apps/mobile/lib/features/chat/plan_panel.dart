part of 'chat_screen.dart';

/// Collapsible panel summarising the agent's current todos (ACP `Plan` events).
///
/// Replace-semantics: it renders whatever the latest `plan` event left in
/// [SessionTranscript.plan]. Lives above the composer, never in the scrolling
/// transcript, so plan churn does not push chat content around.
class _PlanPanel extends StatelessWidget {
  const _PlanPanel({required this.entries});

  final List<PlanEntry> entries;

  IconData _iconFor(String status) {
    switch (status) {
      case 'completed':
        return Icons.check_circle;
      case 'in_progress':
        return Icons.autorenew;
      default:
        return Icons.radio_button_unchecked;
    }
  }

  Color _colorFor(String status, BuildContext context) {
    final tokens = celestialOf(context);
    switch (status) {
      case 'completed':
        return tokens.success;
      case 'in_progress':
        // Activity blue — teal stays reserved for connectivity states.
        return tokens.running;
      default:
        return Theme.of(context).colorScheme.onSurfaceVariant;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final tokens = celestialOf(context);
    final done = entries.where((e) => e.status == 'completed').length;
    final inProgress = entries
        .where((e) => e.status == 'in_progress')
        .firstOrNull;
    // The single most useful line: what the agent is doing right now.
    final subtitle = inProgress != null
        ? '$done/${entries.length} · ${inProgress.content}'
        : '$done/${entries.length} done';
    return Material(
      color: scheme.surfaceContainerLow,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Divider(height: 1, color: scheme.outlineVariant),
          LinearProgressIndicator(
            value: entries.isEmpty ? 0 : done / entries.length,
            minHeight: 2,
            color: tokens.success,
            backgroundColor: scheme.outlineVariant,
          ),
          Theme(
            // ExpansionTile draws divider lines above and below when placed in
            // a Column; suppress them so the panel reads as one block.
            data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
            child: ExpansionTile(
              dense: true,
              tilePadding: const EdgeInsets.symmetric(horizontal: 12),
              leading: const Icon(Icons.checklist, size: 20),
              title: Text(
                'Todos',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              subtitle: Text(
                subtitle,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
              children: [
                ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 220),
                  child: ListView.builder(
                    shrinkWrap: true,
                    padding: const EdgeInsets.only(bottom: 8),
                    itemCount: entries.length,
                    itemBuilder: (ctx, i) {
                      final e = entries[i];
                      return ListTile(
                        dense: true,
                        visualDensity: VisualDensity.compact,
                        leading: Icon(
                          _iconFor(e.status),
                          size: 18,
                          color: _colorFor(e.status, ctx),
                        ),
                        title: Text(
                          e.content,
                          style: TextStyle(
                            decoration: e.status == 'completed'
                                ? TextDecoration.lineThrough
                                : null,
                            color: e.status == 'completed'
                                ? scheme.onSurfaceVariant
                                : null,
                          ),
                        ),
                        trailing: e.priority == 'high'
                            ? Icon(
                                Icons.priority_high,
                                size: 14,
                                color: tokens.gold,
                              )
                            : null,
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
