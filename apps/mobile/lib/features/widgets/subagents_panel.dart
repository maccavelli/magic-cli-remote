import 'package:flutter/material.dart';

import '../../data/protocol/models.dart';
import '../../theme/celestial.dart';

/// Collapsible panel listing the background sub-agents a session is running.
///
/// Sub-agent *output* never reaches the transcript: a sub-agent reports to the
/// main agent over the engine's own channel, and the parent's own reply carries
/// the conclusion. What the user still needs is the fact that work is happening
/// off-screen, which is what this shows (MADR 0051 Part II).
///
/// Deliberately a panel and not a transient snackbar: sub-agents stay running
/// for minutes, and the above-composer slot is already where this app puts
/// ambient session state (see `WorkItemsPanel`).
class SubagentsPanel extends StatelessWidget {
  const SubagentsPanel({super.key, required this.entries});

  final List<SubagentInfo> entries;

  IconData _iconFor(String status) {
    switch (status) {
      case 'completed':
        return Icons.check_circle;
      case 'failed':
        return Icons.error_outline;
      default:
        return Icons.autorenew;
    }
  }

  Color _colorFor(String status, BuildContext context) {
    final tokens = celestialOf(context);
    switch (status) {
      case 'completed':
        return tokens.success;
      case 'failed':
        return Theme.of(context).colorScheme.error;
      default:
        // Activity blue — teal stays reserved for connectivity states.
        return tokens.running;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final running = entries.where((e) => e.isRunning).length;
    final subtitle = running == 0
        ? '${entries.length} finished'
        : running == 1
        ? '1 running'
        : '$running running';
    return Material(
      color: scheme.surfaceContainerLow,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Divider(height: 1, color: scheme.outlineVariant),
          Theme(
            // ExpansionTile draws divider lines above and below when placed in
            // a Column; suppress them so the panel reads as one block.
            data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
            child: ExpansionTile(
              dense: true,
              tilePadding: const EdgeInsets.symmetric(horizontal: 12),
              leading: const Icon(Icons.groups_outlined, size: 20),
              title: Text(
                'Sub-agents',
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
                        title: Text(e.name),
                        subtitle: e.task.isEmpty
                            ? null
                            : Text(
                                e.task,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                              ),
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
