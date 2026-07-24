import 'dart:math';

import 'package:flutter/material.dart';

import 'celestial.dart';

/// Static starfield + vertical space gradient used behind the connect screen
/// and empty states. Purely decorative: fixed seed so the sky never twinkles
/// into a rebuild, wrapped in RepaintBoundary + IgnorePointer by
/// [CelestialBackdrop].
/// Opacity ceiling for the brightest ordinary star, per mode.
const _darkMaxOpacity = 0.66;
const _lightMaxOpacity = 0.15;

/// Opacity floor for the faintest ordinary star. Below roughly this the dot
/// stops reading as a star against the gradient and just looks like noise.
const _minStarOpacity = 0.12;

class StarfieldPainter extends CustomPainter {
  StarfieldPainter({
    required this.starColor,
    required this.starCount,
    required this.maxOpacity,
    this.brightStars = 6,
  });

  final Color starColor;
  final int starCount;
  final double maxOpacity;
  final int brightStars;

  @override
  void paint(Canvas canvas, Size size) {
    final rnd = Random(42);
    final paint = Paint();
    for (var i = 0; i < starCount; i++) {
      final x = rnd.nextDouble() * size.width;
      final y = rnd.nextDouble() * size.height;
      final r = 0.5 + rnd.nextDouble() * 0.9;
      // Quadratic weighting: most stars faint, a few catch the eye.
      final t = rnd.nextDouble();
      final opacity = (_minStarOpacity + (maxOpacity - _minStarOpacity) * t * t)
          .clamp(0.0, 1.0);
      paint
        ..maskFilter = null
        ..color = starColor.withValues(alpha: opacity);
      canvas.drawCircle(Offset(x, y), r, paint);
    }
    for (var i = 0; i < brightStars; i++) {
      final x = rnd.nextDouble() * size.width;
      final y = rnd.nextDouble() * size.height;
      final r = 1.6 + rnd.nextDouble() * 0.6;
      paint
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 2)
        // Normalised against the dark ceiling so the halo stars track the
        // ordinary ones instead of drifting apart when brightness is tuned.
        ..color = starColor.withValues(
          alpha: 0.35 * (maxOpacity / _darkMaxOpacity),
        );
      canvas.drawCircle(Offset(x, y), r, paint);
    }
  }

  @override
  bool shouldRepaint(StarfieldPainter oldDelegate) =>
      oldDelegate.starColor != starColor ||
      oldDelegate.starCount != starCount ||
      oldDelegate.maxOpacity != maxOpacity;
}

/// Full-bleed celestial background: space gradient + starfield in dark mode,
/// dawn gradient + sparse faint stars in light mode. Place as the bottom
/// layer of a Stack behind screen content.
class CelestialBackdrop extends StatelessWidget {
  const CelestialBackdrop({super.key});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final tokens = celestialOf(context);
    final dark = scheme.brightness == Brightness.dark;

    final gradient = dark
        ? LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [scheme.surface, scheme.surfaceContainerLow],
          )
        : const LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [Color(0xFFFBF8FF), Color(0xFFEDE7FA), Color(0xFFFBF2EC)],
          );

    return IgnorePointer(
      child: RepaintBoundary(
        child: DecoratedBox(
          decoration: BoxDecoration(gradient: gradient),
          child: CustomPaint(
            painter: StarfieldPainter(
              starColor: tokens.starfieldStar,
              starCount: dark ? 90 : 25,
              maxOpacity: dark ? _darkMaxOpacity : _lightMaxOpacity,
              brightStars: dark ? 6 : 0,
            ),
            child: const SizedBox.expand(),
          ),
        ),
      ),
    );
  }
}
