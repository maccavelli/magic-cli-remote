import 'package:flutter/material.dart';

import '../../data/protocol/models.dart';
import '../../theme/celestial.dart';

/// Collapsible panel summarising the agent's current work items.
///
/// The daemon publishes plan entries with replace semantics: the panel renders
/// the latest list supplied by a session transcript. Keeping this outside a
/// particular chat implementation makes the same work-item affordance
/// available to every provider-backed chat surface.
class WorkItemsPanel extends StatelessWidget {
  const WorkItemsPanel({super.key, required this.entries});

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
    final remaining = entries.length - done;
    final subtitle = inProgress != null
        ? '$done done, $remaining remaining · ${inProgress.content}'
        : '$done done, $remaining remaining';
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
