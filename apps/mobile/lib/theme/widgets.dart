import 'package:flutter/material.dart';

import '../data/session_status.dart';
import 'celestial.dart';
import 'scroll_activity.dart';

/// Semantic color for a session status string.
Color statusColor(BuildContext context, String status) {
  final scheme = Theme.of(context).colorScheme;
  final tokens = celestialOf(context);
  switch (status) {
    case 'running':
      return tokens.running;
    case 'error':
      return scheme.error;
    case 'disconnected':
    case 'closed':
      return scheme.onSurfaceVariant;
    default: // idle / connected / live
      return tokens.success;
  }
}

/// Status chip with the celestial semantic mapping: color-coded stadium chip,
/// animated color/label transitions, and a pulsing dot while running.
class StatusChip extends StatelessWidget {
  const StatusChip({super.key, required this.status});

  final String status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = statusColor(context, status);
    final dark = theme.brightness == Brightness.dark;
    final fillAlpha = dark ? 0.15 : 0.12;
    return AnimatedContainer(
      duration: const Duration(milliseconds: 250),
      curve: Curves.easeOut,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: fillAlpha),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.4)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (status == 'running') ...[
            _PulsingDot(color: color),
            const SizedBox(width: 5),
          ],
          AnimatedSwitcher(
            duration: const Duration(milliseconds: 200),
            transitionBuilder: (child, anim) => FadeTransition(
              opacity: anim,
              child: SizeTransition(
                sizeFactor: anim,
                axis: Axis.horizontal,
                child: child,
              ),
            ),
            child: Text(
              humanSessionStatus(status).toUpperCase(),
              key: ValueKey(status),
              style: theme.textTheme.labelSmall?.copyWith(
                color: color,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _PulsingDot extends StatefulWidget {
  const _PulsingDot({required this.color});

  final Color color;

  @override
  State<_PulsingDot> createState() => _PulsingDotState();
}

class _PulsingDotState extends State<_PulsingDot>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1200),
  );

  void _syncAnimation() {
    // Honour reduce-motion and scroll activity: a static dot still reads as
    // "running" without burning frames while the transcript is moving.
    final reduce = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    final scrolling = ChatScrollActivity.isScrolling(context);
    if (reduce || scrolling) {
      _c.stop();
      _c.value = 1;
    } else if (!_c.isAnimating) {
      _c.repeat(reverse: true);
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _syncAnimation();
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    // Re-run sync when ScrollActivity flips (InheritedNotifier notify).
    _syncAnimation();
    return FadeTransition(
      opacity: Tween(
        begin: 0.4,
        end: 1.0,
      ).animate(CurvedAnimation(parent: _c, curve: Curves.easeInOut)),
      child: Container(
        width: 6,
        height: 6,
        decoration: BoxDecoration(color: widget.color, shape: BoxShape.circle),
      ),
    );
  }
}

/// The connection banner variants, unified: reconnect/connect strips glide in
/// and out instead of popping.
enum ConnBannerKind { linking, offline }

class ConnBanner extends StatelessWidget {
  const ConnBanner({
    super.key,
    required this.kind,
    required this.message,
    this.trailing,
  });

  final ConnBannerKind kind;
  final String message;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (bg, fg, leading) = switch (kind) {
      ConnBannerKind.linking => (
        scheme.tertiaryContainer,
        scheme.onTertiaryContainer,
        const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
            as Widget,
      ),
      ConnBannerKind.offline => (
        scheme.errorContainer,
        scheme.onErrorContainer,
        const Icon(Icons.cloud_off, size: 18) as Widget,
      ),
    };
    return Material(
      color: bg,
      child: IconTheme(
        data: IconThemeData(color: fg),
        child: ListTile(
          dense: true,
          leading: leading,
          title: Text(message, style: TextStyle(color: fg)),
          trailing: trailing,
        ),
      ),
    );
  }
}

/// Animates banner slot changes (appear/disappear/swap) with a smooth resize.
class BannerSlot extends StatelessWidget {
  const BannerSlot({super.key, required this.child});

  /// The current banner, or null for none.
  final Widget? child;

  @override
  Widget build(BuildContext context) {
    return AnimatedSize(
      duration: const Duration(milliseconds: 200),
      curve: Curves.easeOut,
      child: AnimatedSwitcher(
        duration: const Duration(milliseconds: 200),
        child: child ?? const SizedBox(width: double.infinity, height: 0),
      ),
    );
  }
}

/// One-shot entrance for freshly appended chat items: fade + rise. Runs in
/// [State.initState], so it fires exactly once per element identity (bubbles
/// carry stable seq-keys) and never on history replay of existing elements.
class EntranceFade extends StatefulWidget {
  const EntranceFade({super.key, required this.animate, required this.child});

  final bool animate;
  final Widget child;

  @override
  State<EntranceFade> createState() => _EntranceFadeState();
}

class _EntranceFadeState extends State<EntranceFade>
    with SingleTickerProviderStateMixin {
  // Nullable, created eagerly and only when animating: a `late final`
  // touched first in dispose() would create a ticker mid-unmount.
  AnimationController? _c;
  CurvedAnimation? _curved;

  @override
  void initState() {
    super.initState();
    if (widget.animate) {
      final c = AnimationController(
        vsync: this,
        duration: const Duration(milliseconds: 180),
      );
      _c = c;
      _curved = CurvedAnimation(parent: c, curve: Curves.easeOut);
      c.forward();
    }
  }

  @override
  void dispose() {
    _curved?.dispose();
    _c?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final curved = _curved;
    if (curved == null) return widget.child;
    return FadeTransition(
      opacity: curved,
      child: AnimatedBuilder(
        animation: curved,
        builder: (context, child) => Transform.translate(
          offset: Offset(0, 6 * (1 - curved.value)),
          child: child,
        ),
        child: widget.child,
      ),
    );
  }
}

/// Animated shimmer across a short label ("Thinking…") — a moving starlight
/// gradient, no packages.
class ShimmerText extends StatefulWidget {
  const ShimmerText(this.text, {super.key, this.style});

  final String text;
  final TextStyle? style;

  @override
  State<ShimmerText> createState() => _ShimmerTextState();
}

class _ShimmerTextState extends State<ShimmerText>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1800),
  );

  bool get _shouldAnimate {
    final reduce = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    if (reduce) return false;
    if (ChatScrollActivity.isScrolling(context)) return false;
    return true;
  }

  void _syncAnimation() {
    if (!_shouldAnimate) {
      _c.stop();
      _c.value = 0.5;
    } else if (!_c.isAnimating) {
      _c.repeat();
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _syncAnimation();
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    _syncAnimation();
    final scheme = Theme.of(context).colorScheme;
    final base = scheme.onSurfaceVariant;
    final glint = scheme.primary;
    // Static label while scrolling / reduce-motion — ShaderMask every frame is
    // expensive next to a reverse list that is already laying out.
    if (!_shouldAnimate) {
      return Text(
        widget.text,
        style: (widget.style ?? const TextStyle()).copyWith(color: base),
      );
    }
    return AnimatedBuilder(
      animation: _c,
      builder: (context, child) {
        final t = _c.value;
        return ShaderMask(
          shaderCallback: (bounds) => LinearGradient(
            colors: [base, glint, base],
            stops: const [0.25, 0.5, 0.75],
            transform: _SlideGradientTransform(t * 2 - 1),
          ).createShader(bounds),
          child: child,
        );
      },
      child: Text(
        widget.text,
        style: (widget.style ?? const TextStyle()).copyWith(
          // The mask needs an opaque base to tint.
          color: Colors.white,
        ),
      ),
    );
  }
}

class _SlideGradientTransform extends GradientTransform {
  const _SlideGradientTransform(this.dx);

  final double dx;

  @override
  Matrix4? transform(Rect bounds, {TextDirection? textDirection}) =>
      Matrix4.translationValues(bounds.width * dx, 0, 0);
}
