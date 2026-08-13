/// Grouped section containers for the settings hub (MADR 0082 D1).
///
/// The M3-Expressive-style "containerized" list — a rounded surface card per
/// section under a small primary-coloured header — built from the theme's
/// existing surface-container roles, deliberately not from any framework
/// M3-Expressive widget: Flutter's own component refresh can be adopted later
/// without touching call sites.
library;

import 'package:flutter/material.dart';

/// Bottom padding for a scrollable that must clear the system bar on
/// edge-to-edge Android (MADR 0083 L1): the gesture/nav inset plus breathing
/// room. A fixed constant was how the v0.10.6 provider surfaces ended up with
/// their last rows untappable behind the bar.
EdgeInsets listBottomPadding(BuildContext context, {double extra = 24}) =>
    EdgeInsets.only(bottom: MediaQuery.viewPaddingOf(context).bottom + extra);

class SettingsSection extends StatelessWidget {
  const SettingsSection({
    super.key,
    required this.title,
    this.children = const [],
  });

  final String title;

  /// Rows, rendered inside the rounded container. Spoke rows (a `ListTile`
  /// with a chevron and an `onTap` that pushes a subpage) are ordinary
  /// children — one row shape everywhere beats a special header affordance.
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(28, 20, 28, 6),
          child: Text(
            title,
            style: theme.textTheme.labelLarge?.copyWith(
              color: theme.colorScheme.primary,
            ),
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Material(
            color: theme.colorScheme.surfaceContainerLow,
            borderRadius: BorderRadius.circular(16),
            clipBehavior: Clip.antiAlias,
            child: Column(children: children),
          ),
        ),
      ],
    );
  }
}
