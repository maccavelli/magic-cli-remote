/// Brand identity for agents and model vendors (MADR 0082 D5).
///
/// Bundled monochrome SVG when the id has one (see
/// tools/vendor-icons/sync.sh), tinted to the theme; otherwise a
/// deterministic two-letter monogram. The fallback is mandatory, not a
/// degenerate case: goose's pinned table and future engine catalogs will
/// always contain ids no icon set covers.
library;

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

import 'vendor_icon_manifest.g.dart';

class VendorIcon extends StatelessWidget {
  const VendorIcon({super.key, required this.id, this.display, this.size = 24});

  final String id;

  /// Human label the monogram initials are taken from; falls back to [id].
  final String? display;

  final double size;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final asset = kVendorIconAssets[id];
    if (asset != null) {
      return SvgPicture.asset(
        asset,
        key: Key('vendor-icon-$id'),
        width: size,
        height: size,
        colorFilter: ColorFilter.mode(scheme.onSurface, BlendMode.srcIn),
      );
    }
    final source = (display?.trim().isNotEmpty ?? false) ? display!.trim() : id;
    final letters = source.replaceAll(RegExp('[^A-Za-z0-9]'), '');
    final initials = (letters.isEmpty ? '?' : letters)
        .substring(0, letters.length >= 2 ? 2 : 1)
        .toUpperCase();
    // Not String.hashCode: that is not specified to be stable across
    // platforms, and the same vendor should get the same colour on every
    // device.
    var hash = 0;
    for (final unit in id.codeUnits) {
      hash = (hash + unit) % 0x7fffffff;
    }
    final palette = [
      scheme.primaryContainer,
      scheme.secondaryContainer,
      scheme.tertiaryContainer,
      scheme.surfaceContainerHighest,
      scheme.errorContainer,
      scheme.inversePrimary,
    ];
    return SizedBox(
      key: Key('vendor-icon-$id'),
      width: size,
      height: size,
      child: CircleAvatar(
        key: Key('vendor-icon-monogram-$id'),
        backgroundColor: palette[hash % palette.length],
        child: Text(
          initials,
          style: TextStyle(
            fontSize: size * 0.38,
            fontWeight: FontWeight.w600,
            color: scheme.onSurface,
          ),
        ),
      ),
    );
  }
}
