import 'package:flutter/material.dart';

import '../../../data/protocol/models.dart';
import '../chat_helpers.dart';

/// Glyphs for the session-control icons (MADR 0123 D14).
///
/// At 20dp a glyph is read by silhouette, not detail, so permissions
/// (angular shield), collaboration (horizontal rules) and thinking (round
/// head) are deliberately different shapes. The two mode controls previously
/// rendered as adjacent, identical text chips whose only distinguishing
/// feature was a tooltip no touch user can reach (0123 F2) — icons that cannot
/// be confused are the direct fix.
///
/// The permissions glyph also carries its own state, so the icon *is* the
/// setting: an armed auto-approve session is visible without opening anything.
/// Tint is kept as well (C4) — form for those who read shape, colour for those
/// who read colour.

/// Permissions posture, most alarming first.
IconData permissionsIcon(List<SessionMode> modes, String? currentModeId) {
  final current = resolveDisplayedMode(modes, currentModeId);
  if (current == null) return Icons.shield_outlined;
  // Daemon-declared, never guessed from the id (MADR 0049).
  if (current.dangerous) return Icons.bolt;
  final id = current.id.toLowerCase();
  // OpenCode carries "plan" as a *session* mode rather than a collaboration
  // mode, and the old chip gave it edit-off plus a tint because "the agent
  // will not touch my files" is worth noticing at a glance. That signal has
  // to survive the move (MADR 0123 C4), so it is read here too — the shield
  // family alone would have quietly dropped it.
  if (id == 'plan') return Icons.edit_off;
  if (id.contains('read')) return Icons.gpp_good_outlined;
  return Icons.shield_outlined;
}

/// Error tint only for a mode the daemon flagged dangerous; null otherwise so
/// the icon takes the theme's default.
Color? permissionsTint(
  BuildContext context,
  List<SessionMode> modes,
  String? currentModeId,
) {
  final current = resolveDisplayedMode(modes, currentModeId);
  if (current == null) return null;
  final scheme = Theme.of(context).colorScheme;
  if (current.dangerous) return scheme.error;
  if (current.id.toLowerCase() == 'plan') return scheme.tertiary;
  return null;
}

/// Collaboration: planning reads as a checklist, editing as a pencil.
IconData collaborationIcon(
  List<CollaborationMode> modes,
  String? currentModeId,
) {
  return _isPlanning(modes, currentModeId)
      ? Icons.checklist
      : Icons.edit_outlined;
}

/// Plan mode is a state worth noticing at a glance — it was tinted as a chip
/// before the move, and stays tinted as an icon (C4).
Color? collaborationTint(
  BuildContext context,
  List<CollaborationMode> modes,
  String? currentModeId,
) {
  return _isPlanning(modes, currentModeId)
      ? Theme.of(context).colorScheme.tertiary
      : null;
}

bool _isPlanning(List<CollaborationMode> modes, String? currentModeId) {
  final currentId = (currentModeId ?? '').trim();
  if (modes.isEmpty) return false;
  final current = modes.firstWhere(
    (m) => m.id == currentId,
    orElse: () =>
        modes.firstWhere((m) => m.id == 'default', orElse: () => modes.first),
  );
  return current.id.toLowerCase() == 'plan';
}
