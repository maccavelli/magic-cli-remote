import 'package:flutter/material.dart';

import '../../../data/protocol/models.dart';
import '../../../data/protocol/picker.dart';
import '../../../data/ws/mc_exception.dart';
import '../../../theme/top_notification.dart';
import '../chat_helpers.dart';
import 'session_control_card.dart';

/// The three session-control cards (MADR 0123 D5).
///
/// Each is a thin caller over [SessionControlCard]: the shared surface owns
/// the layout, the banner and the row treatment, so the controls cannot drift
/// apart again the way the popup menus and the `SimpleDialog` did.

/// Permissions — what the agent may do without asking.
///
/// Distinct from collaboration mode (below), which is what the agent may
/// *plan*. The two used to render as adjacent, identical text chips whose only
/// distinguishing feature was a tooltip no touch user can reach (MADR 0123 F2).
Future<void> showPermissionsCard(
  BuildContext context, {
  required List<SessionMode> modes,
  required String? currentModeId,
  required Future<void> Function(String modeId) onSelect,
}) {
  // MADR 0047 D4: resolve the current mode, never take modes.first.
  final current = resolveDisplayedMode(modes, currentModeId);
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (sheetContext) => SessionControlCard(
      title: 'Permissions',
      options: [
        for (final m in modes)
          SessionControlOption(
            id: m.id,
            label: m.name,
            description: m.description,
            selected: m.id == current?.id,
            dangerous: m.dangerous,
            onSelected: () async {
              // Arming a mode that answers permissions for the user still
              // confirms (MADR 0123 D6). A nicer surface is not a reason to
              // make auto-approve one tap cheaper than it was.
              if (m.dangerous) {
                final ok = await confirmDangerousMode(context, m);
                if (!ok) return;
              }
              await onSelect(m.id);
            },
          ),
      ],
    ),
  );
}

/// Collaboration — whether the agent plans or edits.
Future<void> showCollaborationCard(
  BuildContext context, {
  required List<CollaborationMode> modes,
  required String? currentModeId,
  required Future<void> Function(String modeId) onSelect,
}) {
  final currentId = (currentModeId ?? '').trim();
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (sheetContext) => SessionControlCard(
      title: 'Collaboration',
      options: [
        for (final m in modes)
          SessionControlOption(
            id: m.id,
            label: m.name,
            selected: m.id == currentId,
            onSelected: () => onSelect(m.id),
          ),
      ],
    ),
  );
}

/// Thinking level, and what the session will actually do with a change.
///
/// The ladder is always shown, even when the level is locked: a user who
/// cannot pick a rung still deserves to see which rungs exist and why the
/// choice is unavailable. That is the banner's job, and it replaces a tap that
/// failed and explained afterwards (MADR 0123 D8).
Future<void> showThinkingCard(
  BuildContext context, {
  required List<ThinkingLevel> levels,
  required String currentLevel,
  required ThinkingMutability mutability,
  required Future<void> Function(String level) onSelect,
}) {
  final banner = switch (mutability) {
    ThinkingMutability.fixed => const SessionControlBanner(
      message:
          'This agent applies the thinking level when the session starts. '
          'Start a new session to change it.',
      severity: SessionControlSeverity.warning,
    ),
    ThinkingMutability.nextTurn => const SessionControlBanner(
      message: 'A new level applies from your next message.',
    ),
    // Live needs no explanation, and unknown must not invent one: an older
    // daemon omits the capability, and a banner there would be a guess
    // presented as a fact (MADR 0123 C2).
    ThinkingMutability.live || ThinkingMutability.unknown => null,
  };

  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (sheetContext) => SessionControlCard(
      title: 'Thinking',
      banner: banner,
      enabled: mutability.settable,
      options: [
        for (final l in levels)
          SessionControlOption(
            id: l.id,
            label: l.displayLabel,
            description: l.description,
            selected: l.id == currentLevel,
            onSelected: () => onSelect(l.id),
          ),
      ],
    ),
  );
}

/// Confirms arming a mode that answers permission requests without the user.
///
/// Returns false when the dialog is dismissed by any route (cancel, back
/// gesture, barrier tap), so the default is always "do not arm". Moved here
/// from chat_screen.dart unchanged (MADR 0123 D6).
Future<bool> confirmDangerousMode(
  BuildContext context,
  SessionMode mode,
) async {
  final scheme = Theme.of(context).colorScheme;
  final ok = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      icon: Icon(Icons.bolt, color: scheme.error),
      title: const Text('Run without approvals?'),
      content: Text(
        'This session will approve every permission request automatically, '
        'including file edits and shell commands.\n\n'
        '${mode.description.isEmpty ? '' : '${mode.description}\n\n'}'
        'It stays on until you switch modes.',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(
            backgroundColor: scheme.error,
            foregroundColor: scheme.onError,
          ),
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text('Turn on'),
        ),
      ],
    ),
  );
  return ok ?? false;
}

/// Reports a failed control change in one place, so all three cards fail the
/// same way rather than three slightly different ways.
void reportControlFailure(BuildContext context, String what, Object error) {
  if (!context.mounted) return;
  showTopNotification(
    context,
    '$what failed: ${friendlyOpError(error)}',
    severity: NoticeSeverity.error,
  );
}
