import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

/// The curated Lucide glyph set for the composer-actions row (MADR 0123 D15).
///
/// The first attempt used Material icons and was rejected on a device: the row
/// mixed three idioms — Material outlined, Material filled (`tune`,
/// `checklist`, `bolt`), and one iOS glyph (`ios_share`) — which sit at
/// different optical weights and never read as a set. Lucide is one 24px grid,
/// one 2px stroke, one set of rounded joins, so ten of them look designed
/// together rather than collected.
///
/// Bundled as SVG assets rather than pulled as a package: `flutter_svg` is
/// already a dependency and `assets/vendor_icons/` is an established pipeline
/// (MADR 0082 D5), so this ships thirteen glyphs and no new dependency. ISC
/// licensed; the notice travels with them at `assets/ui_icons/LICENSE`.
class UiIcons {
  const UiIcons._();

  static const attachImage = 'image-plus';
  static const attachAudio = 'audio-lines';
  static const workspace = 'folder';
  static const shell = 'square-terminal';
  static const share = 'share-2';
  static const thinking = 'brain';
  static const agentSettings = 'sliders-horizontal';

  /// Permissions, by posture. The glyph *is* the setting, so an armed
  /// auto-approve is legible without opening anything (D14's surviving
  /// principle, C4).
  static const shieldDefault = 'shield';
  static const shieldReadOnly = 'shield-check';
  static const shieldAuto = 'shield-alert';
  static const shieldFullAccess = 'shield-off';

  /// Collaboration: planning is a checklist, editing is a pencil.
  static const plan = 'list-checks';
  static const edit = 'pencil-line';

  /// The confirmation dialog's alarm. This is the one place colour still
  /// carries meaning: the dialog exists to stop a bypass being armed by
  /// reflex, so it stays red (MADR 0123 D6). Only the glyph family changed.
  static const alert = 'triangle-alert';

  /// Card row selection. Same ink as the label — selection is form, not hue
  /// (MADR 0123 D17).
  static const selected = 'circle-dot';
  static const unselected = 'circle';

  /// Every glyph this app ships. A missing asset renders as a silent blank
  /// rather than an error, so tests assert against this list instead of
  /// trusting the widget tree (P9).
  static const all = <String>[
    attachImage,
    attachAudio,
    workspace,
    shell,
    share,
    thinking,
    agentSettings,
    shieldDefault,
    shieldReadOnly,
    shieldAuto,
    shieldFullAccess,
    plan,
    edit,
    selected,
    unselected,
    alert,
  ];

  static String pathOf(String name) => 'assets/ui_icons/$name.svg';
}

/// Renders a bundled Lucide glyph at [size], tinted [color].
///
/// The SVGs carry `stroke="currentColor"`, so a single `srcIn` filter colours
/// the whole glyph and state can be expressed by ink rather than by swapping
/// assets.
class UiIcon extends StatelessWidget {
  const UiIcon(this.name, {super.key, this.size = 20, this.color});

  final String name;
  final double size;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final tint = color ?? IconTheme.of(context).color ?? Colors.black;
    return SvgPicture.asset(
      UiIcons.pathOf(name),
      width: size,
      height: size,
      colorFilter: ColorFilter.mode(tint, BlendMode.srcIn),
    );
  }
}
