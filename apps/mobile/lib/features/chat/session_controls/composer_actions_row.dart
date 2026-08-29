import 'package:flutter/material.dart';

/// One affordance in the composer-actions row.
class ComposerAction {
  const ComposerAction({
    required this.id,
    required this.icon,
    required this.tooltip,
    this.onPressed,
    this.tint,
  });

  final String id;
  final IconData icon;
  final String tooltip;

  /// Null disables the action. An action whose surface the session does not
  /// have is omitted by the caller rather than disabled here: a row of greyed
  /// icons teaches nothing (MADR 0123 D4).
  final VoidCallback? onPressed;

  /// Non-default state colour — a dangerous permission mode, an armed Plan.
  /// Null takes the theme's default. State signalling must survive the move
  /// off the app bar, where it used to be a tinted chip (MADR 0123 C4).
  final Color? tint;
}

/// The row of icons beneath the prompt, sized to hold ten (MADR 0123 D12/D13).
///
/// Flutter's default `IconButton` is 48x48 (`kMinInteractiveDimension`). Ten of
/// those need 480dp; the 360dp reference surface leaves 336dp after the
/// composer's padding, so the default overflows by 144dp — 43%. Even
/// `VisualDensity.compact` only reaches 40dp. The capacity target is therefore
/// unreachable by choosing tidier glyphs, and this widget exists to make it a
/// layout rule instead:
///
/// ```text
/// width = clamp(available / count, kMinIconWidth, kMaxIconWidth)
/// glyph 20dp - full 48dp tap height - zero padding - shrinkWrap
/// ```
///
/// A single hardcoded width cannot serve both ends of the range: 32dp fits
/// 360dp with room to spare but overflows the 296dp available on a 320dp
/// phone. Deriving from the measured width fits ten at both.
///
/// Past the floor the row scrolls horizontally. It never wraps — a second row
/// would shift the composer up and down as providers change, which is the same
/// class of layout surprise this record was opened to remove — and it never
/// truncates, because a hidden control is the defect being fixed.
class ComposerActionsRow extends StatelessWidget {
  const ComposerActionsRow({super.key, required this.actions});

  final List<ComposerAction> actions;

  /// The narrowest an action may be. Below this the row scrolls instead.
  ///
  /// Holding the full 48dp height while narrowing the box keeps the target
  /// inside WCAG 2.2 SC 2.5.8 (24x24 minimum) with margin, but it is under
  /// Material's own 48x48 guidance on the horizontal axis. That is a real
  /// concession, and this constant is where it stops (MADR 0123 F9).
  static const double kMinIconWidth = 28;

  /// The widest, so a handful of icons do not sprawl on a tablet.
  static const double kMaxIconWidth = 40;

  /// Full-height tap target: only the width is traded away.
  static const double kTapHeight = 48;

  static const double kGlyphSize = 20;

  /// The width one action gets for a given row width and count. Exposed so a
  /// test can assert the capacity target rather than infer it from pixels.
  static double widthFor(double available, int count) {
    if (count <= 0) return kMaxIconWidth;
    final raw = available / count;
    return raw.clamp(kMinIconWidth, kMaxIconWidth);
  }

  /// Whether [count] actions fit in [available] without scrolling.
  static bool fits(double available, int count) =>
      widthFor(available, count) * count <= available;

  @override
  Widget build(BuildContext context) {
    // An empty row still renders, keyed and childless. That is the behaviour
    // the composer had before this widget existed, and 0123 C3 forbids a
    // layout change from quietly altering it — a session advertising no
    // capabilities keeps its (zero-height) row rather than restructuring the
    // column beneath the prompt.
    return LayoutBuilder(
      builder: (context, constraints) {
        final available = constraints.maxWidth;
        final width = widthFor(available, actions.length);
        final scrolls = width * actions.length > available;

        final children = [
          for (final a in actions)
            SizedBox(
              width: width,
              height: kTapHeight,
              child: IconButton(
                // The id IS the key. Existing tests and any future ones
                // address these affordances by their historical names
                // (attach-audio, open-shell, ...); prefixing would have
                // renamed every one of them for no gain.
                key: ValueKey(a.id),
                tooltip: a.tooltip,
                onPressed: a.onPressed,
                iconSize: kGlyphSize,
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(),
                style: const ButtonStyle(
                  tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                ),
                icon: Icon(a.icon, color: a.tint),
              ),
            ),
        ];

        final row = Row(
          key: const ValueKey('composer-actions'),
          mainAxisAlignment: MainAxisAlignment.start,
          mainAxisSize: scrolls ? MainAxisSize.min : MainAxisSize.max,
          children: children,
        );

        if (!scrolls) return row;
        return SingleChildScrollView(
          key: const ValueKey('composer-actions-scroll'),
          scrollDirection: Axis.horizontal,
          child: row,
        );
      },
    );
  }
}
