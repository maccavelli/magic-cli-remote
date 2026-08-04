import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/foundation.dart' show defaultTargetPlatform;
import 'package:flutter/material.dart';

/// The "Celestial" design system: deep-space dark ("Deep Field") and
/// dawn-sky light themes, plus semantic tokens the Material scheme has no
/// slot for (status colors, the permission "gold", bubble gradients).
///
/// All text/background pairs here were contrast-checked to WCAG AA or better;
/// keep that property when adjusting values.

// ---------------------------------------------------------------------------
// Semantic tokens
// ---------------------------------------------------------------------------

/// Colors with app-specific meaning, resolved via
/// `Theme.of(context).extension<CelestialColors>()!`.
@immutable
class CelestialColors extends ThemeExtension<CelestialColors> {
  const CelestialColors({
    required this.success,
    required this.caution,
    required this.running,
    required this.gold,
    required this.goldContainer,
    required this.onGoldContainer,
    required this.thoughtIcon,
    required this.userBubbleGradA,
    required this.userBubbleGradB,
    required this.onUserBubble,
    required this.starfieldStar,
  });

  /// Idle / live / completed / plan-done.
  final Color success;

  /// Verified-but-ageing: a connection that has not proved itself recently
  /// (MADR 0063 D1). Deliberately distinct from `error` — nothing has failed,
  /// we simply cannot claim health any more.
  final Color caution;

  /// Agent actively working (running chip, plan in-progress).
  final Color running;

  /// Permission & notice accent — the "sun" color for attention moments.
  final Color gold;
  final Color goldContainer;
  final Color onGoldContainer;

  /// Thinking/thought tile accent.
  final Color thoughtIcon;

  /// User chat bubble gradient ends and its text color.
  final Color userBubbleGradA;
  final Color userBubbleGradB;
  final Color onUserBubble;

  /// Star color for the starfield backdrop painter.
  final Color starfieldStar;

  static const dark = CelestialColors(
    success: Color(0xFF8CDCA9),
    caution: Color(0xFFE8C56A),
    running: Color(0xFF8FC6FF),
    gold: Color(0xFFF2C97E),
    goldContainer: Color(0xFF302B2A),
    onGoldContainer: Color(0xFFFFE3B0),
    thoughtIcon: Color(0xFFC5C6E5),
    userBubbleGradA: Color(0xFF3A4090),
    userBubbleGradB: Color(0xFF4A3F9E),
    onUserBubble: Color(0xFFE9E8FF),
    starfieldStar: Color(0xFFE5E7F6),
  );

  static const light = CelestialColors(
    success: Color(0xFF1B6B3F),
    caution: Color(0xFF8A6100),
    running: Color(0xFF1A5CA8),
    gold: Color(0xFF7A5900),
    goldContainer: Color(0xFFFFE3B0),
    onGoldContainer: Color(0xFF5C4200),
    thoughtIcon: Color(0xFF585A7C),
    userBubbleGradA: Color(0xFFDFE0FF),
    userBubbleGradB: Color(0xFFE8DBFF),
    onUserBubble: Color(0xFF0E1360),
    starfieldStar: Color(0xFF4650A8),
  );

  @override
  CelestialColors copyWith({
    Color? success,
    Color? caution,
    Color? running,
    Color? gold,
    Color? goldContainer,
    Color? onGoldContainer,
    Color? thoughtIcon,
    Color? userBubbleGradA,
    Color? userBubbleGradB,
    Color? onUserBubble,
    Color? starfieldStar,
  }) {
    return CelestialColors(
      success: success ?? this.success,
      caution: caution ?? this.caution,
      running: running ?? this.running,
      gold: gold ?? this.gold,
      goldContainer: goldContainer ?? this.goldContainer,
      onGoldContainer: onGoldContainer ?? this.onGoldContainer,
      thoughtIcon: thoughtIcon ?? this.thoughtIcon,
      userBubbleGradA: userBubbleGradA ?? this.userBubbleGradA,
      userBubbleGradB: userBubbleGradB ?? this.userBubbleGradB,
      onUserBubble: onUserBubble ?? this.onUserBubble,
      starfieldStar: starfieldStar ?? this.starfieldStar,
    );
  }

  @override
  CelestialColors lerp(ThemeExtension<CelestialColors>? other, double t) {
    if (other is! CelestialColors) return this;
    Color l(Color a, Color b) => Color.lerp(a, b, t)!;
    return CelestialColors(
      success: l(success, other.success),
      caution: l(caution, other.caution),
      running: l(running, other.running),
      gold: l(gold, other.gold),
      goldContainer: l(goldContainer, other.goldContainer),
      onGoldContainer: l(onGoldContainer, other.onGoldContainer),
      thoughtIcon: l(thoughtIcon, other.thoughtIcon),
      userBubbleGradA: l(userBubbleGradA, other.userBubbleGradA),
      userBubbleGradB: l(userBubbleGradB, other.userBubbleGradB),
      onUserBubble: l(onUserBubble, other.onUserBubble),
      starfieldStar: l(starfieldStar, other.starfieldStar),
    );
  }
}

/// Resolve the celestial tokens. Both app themes install them; contexts under
/// a foreign theme (widget tests, previews) fall back by brightness.
CelestialColors celestialOf(BuildContext context) {
  final theme = Theme.of(context);
  return theme.extension<CelestialColors>() ??
      (theme.brightness == Brightness.dark
          ? CelestialColors.dark
          : CelestialColors.light);
}

// ---------------------------------------------------------------------------
// Shared styles
// ---------------------------------------------------------------------------

/// One destructive-button style instead of ad-hoc error-colored copies.
ButtonStyle destructiveFilled(ColorScheme scheme) => FilledButton.styleFrom(
  backgroundColor: scheme.error,
  foregroundColor: scheme.onError,
);

/// Monospace body text (tool detail, code-ish content).
const monoDetail = TextStyle(
  fontFamily: 'monospace',
  fontSize: 12.5,
  height: 18 / 12.5,
);

/// Monospace for pair codes / fingerprints.
const monoPairCode = TextStyle(
  fontFamily: 'monospace',
  fontSize: 22,
  fontWeight: FontWeight.w600,
  letterSpacing: 2,
);

// ---------------------------------------------------------------------------
// Color schemes
// ---------------------------------------------------------------------------

const ColorScheme _darkScheme = ColorScheme(
  brightness: Brightness.dark,
  surface: Color(0xFF0B0D1A),
  surfaceContainerLowest: Color(0xFF06070F),
  surfaceContainerLow: Color(0xFF101324),
  surfaceContainer: Color(0xFF151932),
  surfaceContainerHigh: Color(0xFF1C2140),
  surfaceContainerHighest: Color(0xFF242A52),
  onSurface: Color(0xFFE5E7F6),
  onSurfaceVariant: Color(0xFFA9AECE),
  outline: Color(0xFF565C84),
  outlineVariant: Color(0xFF2E3457),
  primary: Color(0xFFADB6FF),
  onPrimary: Color(0xFF161C52),
  primaryContainer: Color(0xFF3A4090),
  onPrimaryContainer: Color(0xFFDFE0FF),
  secondary: Color(0xFFC5C6E5),
  onSecondary: Color(0xFF15182F),
  secondaryContainer: Color(0xFF3F4468),
  onSecondaryContainer: Color(0xFFDFE0F9),
  tertiary: Color(0xFF84D6CE),
  onTertiary: Color(0xFF00201F),
  tertiaryContainer: Color(0xFF1F4E49),
  onTertiaryContainer: Color(0xFFB8F2EA),
  error: Color(0xFFFFB4AB),
  onError: Color(0xFF690005),
  errorContainer: Color(0xFF7F2B24),
  onErrorContainer: Color(0xFFFFDAD6),
  inverseSurface: Color(0xFFE5E7F6),
  onInverseSurface: Color(0xFF101324),
  inversePrimary: Color(0xFF3A4090),
  scrim: Color(0xFF000000),
  shadow: Color(0xFF000000),
);

const ColorScheme _lightScheme = ColorScheme(
  brightness: Brightness.light,
  surface: Color(0xFFFBF8FF),
  surfaceContainerLowest: Color(0xFFFFFFFF),
  surfaceContainerLow: Color(0xFFF4F1FC),
  surfaceContainer: Color(0xFFEEEBF8),
  surfaceContainerHigh: Color(0xFFE7E3F4),
  surfaceContainerHighest: Color(0xFFE1DCEF),
  onSurface: Color(0xFF1B1C27),
  onSurfaceVariant: Color(0xFF4F5170),
  outline: Color(0xFF787A96),
  outlineVariant: Color(0xFFCBC8DC),
  primary: Color(0xFF4650A8),
  onPrimary: Color(0xFFFFFFFF),
  primaryContainer: Color(0xFFDFE0FF),
  onPrimaryContainer: Color(0xFF0E1360),
  secondary: Color(0xFF585A7C),
  onSecondary: Color(0xFFFFFFFF),
  secondaryContainer: Color(0xFFE1E0F9),
  onSecondaryContainer: Color(0xFF15182F),
  tertiary: Color(0xFF00696B),
  onTertiary: Color(0xFFFFFFFF),
  tertiaryContainer: Color(0xFF9CF1EA),
  onTertiaryContainer: Color(0xFF00201F),
  error: Color(0xFFBA1A1A),
  onError: Color(0xFFFFFFFF),
  errorContainer: Color(0xFFFFDAD6),
  onErrorContainer: Color(0xFF410002),
  inverseSurface: Color(0xFF2F303C),
  onInverseSurface: Color(0xFFF2EFFA),
  inversePrimary: Color(0xFFADB6FF),
  scrim: Color(0xFF000000),
  shadow: Color(0xFF000000),
);

// ---------------------------------------------------------------------------
// Typography
// ---------------------------------------------------------------------------

TextTheme _textTheme(ColorScheme scheme) {
  // Platform-adaptive (MADR 0067 D7): pinning android here asked iOS for
  // Roboto metrics and left text feeling non-native. The explicit sizes in
  // the copyWith below carry the design system on both platforms.
  final base = Typography.material2021(
    platform: defaultTargetPlatform,
    colorScheme: scheme,
  );
  final t = scheme.brightness == Brightness.dark ? base.white : base.black;
  return t.copyWith(
    headlineSmall: t.headlineSmall?.copyWith(
      fontSize: 24,
      height: 32 / 24,
      fontWeight: FontWeight.w600,
      letterSpacing: -0.25,
    ),
    titleLarge: t.titleLarge?.copyWith(
      fontSize: 20,
      height: 28 / 20,
      fontWeight: FontWeight.w600,
    ),
    titleMedium: t.titleMedium?.copyWith(
      fontSize: 16,
      height: 24 / 16,
      fontWeight: FontWeight.w600,
      letterSpacing: 0.1,
    ),
    titleSmall: t.titleSmall?.copyWith(
      fontSize: 14,
      height: 20 / 14,
      fontWeight: FontWeight.w600,
    ),
    bodyLarge: t.bodyLarge?.copyWith(fontSize: 16, height: 24 / 16),
    // Slightly roomier line-height than stock: streamed prose reads calmer.
    bodyMedium: t.bodyMedium?.copyWith(fontSize: 14.5, height: 22 / 14.5),
    bodySmall: t.bodySmall?.copyWith(fontSize: 12, height: 17 / 12),
    labelLarge: t.labelLarge?.copyWith(
      fontSize: 14,
      height: 20 / 14,
      fontWeight: FontWeight.w600,
      letterSpacing: 0.3,
    ),
    labelSmall: t.labelSmall?.copyWith(
      fontSize: 11,
      height: 16 / 11,
      letterSpacing: 0.4,
    ),
  );
}

// ---------------------------------------------------------------------------
// ThemeData assembly
// ---------------------------------------------------------------------------

ThemeData _celestialTheme(ColorScheme scheme, CelestialColors tokens) {
  final text = _textTheme(scheme);
  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    textTheme: text,
    scaffoldBackgroundColor: scheme.surface,
    extensions: [tokens],
    appBarTheme: AppBarTheme(
      backgroundColor: scheme.surface,
      foregroundColor: scheme.onSurface,
      elevation: 0,
      scrolledUnderElevation: 0,
      centerTitle: false,
      titleTextStyle: text.titleMedium?.copyWith(color: scheme.onSurface),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        shape: const StadiumBorder(),
        minimumSize: const Size(48, 48),
        textStyle: text.labelLarge,
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        shape: const StadiumBorder(),
        minimumSize: const Size(48, 48),
        side: BorderSide(color: scheme.outline),
        textStyle: text.labelLarge,
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        shape: const StadiumBorder(),
        textStyle: text.labelLarge,
      ),
    ),
    chipTheme: ChipThemeData(
      labelStyle: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
      shape: const StadiumBorder(),
      side: BorderSide(color: scheme.outlineVariant),
      backgroundColor: scheme.surfaceContainerHigh,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: scheme.surfaceContainerLow,
      hintStyle: TextStyle(
        color: scheme.onSurfaceVariant.withValues(alpha: 0.7),
      ),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: scheme.outlineVariant),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: scheme.outlineVariant),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide(color: scheme.primary, width: 1.5),
      ),
    ),
    cardTheme: CardThemeData(
      color: scheme.surfaceContainerLow,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: scheme.brightness == Brightness.light
            ? BorderSide(color: scheme.outlineVariant)
            : BorderSide.none,
      ),
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: scheme.surfaceContainerHigh,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
      titleTextStyle: text.titleMedium?.copyWith(color: scheme.onSurface),
    ),
    // Consumed only by TopNotification — no real SnackBar is left in the app
    // (MADR 0042 D7 replaced them), so these tokens are the notification's
    // palette.
    //
    // Deliberately NOT the Material default of inverseSurface/onInverseSurface.
    // That pair inverts brightness on purpose, which is right for a stock M3
    // snackbar but renders a near-white slab (#E5E7F6) over this theme's
    // #0B0D1A starfield — the one component in the app that ignored the
    // palette. surfaceContainerHigh puts it on the same material as dialogs and
    // bottom sheets; the hairline outline gives a dark card an edge against a
    // dark background, which the white version never needed.
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      backgroundColor: scheme.surfaceContainerHigh,
      contentTextStyle: TextStyle(color: scheme.onSurface),
      actionTextColor: scheme.primary,
      insetPadding: const EdgeInsets.all(12),
    ),
    bannerTheme: MaterialBannerThemeData(
      backgroundColor: tokens.goldContainer,
      contentTextStyle: TextStyle(color: tokens.onGoldContainer),
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: scheme.surfaceContainerHigh,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(28)),
      ),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: scheme.primary),
    dividerTheme: DividerThemeData(color: scheme.outlineVariant),
    pageTransitionsTheme: const PageTransitionsTheme(
      builders: {
        TargetPlatform.android: PredictiveBackPageTransitionsBuilder(),
        // Without an explicit entry iOS fell back to the generic Zoom
        // transition — no edge-swipe back (MADR 0067 D7). Predictive back
        // has no iOS analogue; Cupertino is its native counterpart.
        TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
      },
    ),
  );
}

/// Dark theme — "Deep Field".
final ThemeData celestialDark = _celestialTheme(
  _darkScheme,
  CelestialColors.dark,
);

/// Light theme — "Dawn Sky".
final ThemeData celestialLight = _celestialTheme(
  _lightScheme,
  CelestialColors.light,
);
