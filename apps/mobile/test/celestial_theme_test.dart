import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/foundation.dart' show defaultTargetPlatform;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/theme/celestial.dart';

void main() {
  // MADR 0067 D7 (U7) — the theme must not force Android behaviour on iOS.
  test('page transitions cover both mobile platforms natively', () {
    for (final theme in [celestialDark, celestialLight]) {
      final builders = theme.pageTransitionsTheme.builders;
      expect(
        builders[TargetPlatform.android],
        isA<PredictiveBackPageTransitionsBuilder>(),
      );
      // Without an explicit iOS entry the map falls back to the generic
      // Zoom transition — no edge-swipe back.
      expect(
        builders[TargetPlatform.iOS],
        isA<CupertinoPageTransitionsBuilder>(),
      );
    }
  });

  test('ThemeData.platform follows the ambient platform (scroll physics, '
      'widget behaviour)', () {
    expect(celestialDark.platform, defaultTargetPlatform);
    expect(celestialLight.platform, defaultTargetPlatform);
  });
}
