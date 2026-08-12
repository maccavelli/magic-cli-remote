/// Semantic status chips (MADR 0082 D4).
///
/// One chip system for every status the settings surfaces render — provider
/// credential state, transport probes, and the provider cards that follow —
/// so the same state always looks the same everywhere. Each chip carries one
/// content type (a dot plus a short label, per the badge guidance the MADR
/// cites); the `active` variant is a filled pill because "this is the one in
/// use" is a selection, not a health state.
library;

import 'package:flutter/material.dart';

import '../../data/protocol/models.dart' show AuthStatus;
import '../../theme/celestial.dart';

enum StatusKind { ok, caution, error, neutral, active }

class StatusChip extends StatelessWidget {
  const StatusChip({super.key, required this.kind, required this.label});

  /// The chip for an [AuthStatus] wire value (MADR 0074 D3). `quota` is
  /// caution, not error: it needs waiting or switching, not a new key.
  factory StatusChip.auth(String status, {Key? key}) => switch (status) {
    AuthStatus.configured => StatusChip(
      key: key,
      kind: StatusKind.ok,
      label: 'Configured',
    ),
    AuthStatus.quota => StatusChip(
      key: key,
      kind: StatusKind.caution,
      label: 'Quota reached',
    ),
    AuthStatus.error => StatusChip(
      key: key,
      kind: StatusKind.error,
      label: 'Error',
    ),
    _ => StatusChip(key: key, kind: StatusKind.neutral, label: 'Needs setup'),
  };

  final StatusKind kind;
  final String label;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final celestial = celestialOf(context);
    final color = switch (kind) {
      StatusKind.ok => celestial.success,
      StatusKind.caution => celestial.caution,
      StatusKind.error => scheme.error,
      StatusKind.neutral => scheme.onSurfaceVariant,
      StatusKind.active => scheme.onPrimary,
    };
    return Container(
      key: Key('status-chip-${kind.name}'),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: kind == StatusKind.active
            ? scheme.primary
            : scheme.surfaceContainerHigh,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (kind != StatusKind.active) ...[
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(color: color, shape: BoxShape.circle),
            ),
            const SizedBox(width: 5),
          ],
          Text(
            label,
            style: Theme.of(
              context,
            ).textTheme.labelSmall?.copyWith(color: color),
          ),
        ],
      ),
    );
  }
}
