import 'dart:async';

import 'package:flutter/material.dart';

const _kDefaultDuration = Duration(seconds: 3);
const _kSlideDuration = Duration(milliseconds: 250);
const _kVerticalMargin = 8.0;
const _kHorizontalMargin = 12.0;

/// How many messages may wait behind the one on screen. A burst should not
/// become a slideshow, but silently discarding all but the last loses the
/// first error of a cascade — which is usually the informative one.
const _kMaxQueued = 3;

OverlayEntry? _activeEntry;
OverlayState? _overlay;
final _queue = <_Pending>[];

class _Pending {
  const _Pending(this.message, this.duration, this.actionLabel, this.onAction);
  final String message;
  final Duration duration;
  final String? actionLabel;
  final VoidCallback? onAction;
}

void showTopNotification(
  BuildContext context,
  String message, {
  Duration duration = _kDefaultDuration,
  String? actionLabel,
  VoidCallback? onAction,
}) {
  if (!context.mounted) return;

  // rootOverlay, so this survives route changes for the app's lifetime.
  final overlay = Overlay.of(context, rootOverlay: true);
  if (!identical(overlay, _overlay)) {
    // A different root overlay: the whole tree was replaced. Anything queued
    // belonged to the old one and can never be shown in this one.
    _queue.clear();
    _activeEntry = null;
    _overlay = overlay;
  }
  // Deliberately no `_activeEntry.mounted` liveness check here: an entry is not
  // mounted in the same frame it is inserted, so testing it would let a second
  // call in one frame show two notifications at once. The identity check above
  // is the recovery path that matters — an entry orphaned by its overlay going
  // away is exactly the case where the overlay is no longer the same object.
  _queue.add(_Pending(message, duration, actionLabel, onAction));
  if (_queue.length > _kMaxQueued) {
    _queue.removeRange(0, _queue.length - _kMaxQueued);
  }
  if (_activeEntry == null) _showNext();
}

void _showNext() {
  final overlay = _overlay;
  if (_queue.isEmpty || overlay == null) {
    _activeEntry = null;
    return;
  }
  final next = _queue.removeAt(0);

  late final OverlayEntry entry;
  entry = OverlayEntry(
    builder: (_) => _TopNotification(
      message: next.message,
      duration: next.duration,
      actionLabel: next.actionLabel,
      onAction: next.onAction,
      onRemoved: () {
        if (_activeEntry == entry) _activeEntry = null;
        entry.remove();
        _showNext();
      },
    ),
  );

  _activeEntry = entry;
  overlay.insert(entry);
}

class _TopNotification extends StatefulWidget {
  final String message;
  final Duration duration;
  final String? actionLabel;
  final VoidCallback? onAction;
  final VoidCallback onRemoved;

  const _TopNotification({
    required this.message,
    required this.duration,
    this.actionLabel,
    this.onAction,
    required this.onRemoved,
  });

  @override
  State<_TopNotification> createState() => _TopNotificationState();
}

class _TopNotificationState extends State<_TopNotification>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl;
  late final Animation<Offset> _slide;
  Timer? _dismissTimer;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(duration: _kSlideDuration, vsync: this);
    _slide = Tween<Offset>(
      begin: const Offset(0, -1),
      end: Offset.zero,
    ).animate(CurvedAnimation(parent: _ctrl, curve: Curves.easeOutCubic));

    _ctrl.forward();

    _dismissTimer = Timer(widget.duration, _dismiss);
  }

  @override
  void dispose() {
    _dismissTimer?.cancel();
    _ctrl.dispose();
    super.dispose();
  }

  void _dismiss() {
    if (!mounted) return;
    _dismissTimer?.cancel();
    _ctrl.reverse().then((_) {
      if (!mounted) return;
      widget.onRemoved();
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    // Colour, shape and type come from the app's SnackBarThemeData rather than
    // from literals here: this widget replaced the snackbar, so it should carry
    // the same design tokens instead of a second, drifting copy of them
    // (MADR 0042 D7). Fallbacks keep it renderable under a foreign theme.
    final snack = theme.snackBarTheme;
    final background = snack.backgroundColor ?? scheme.inverseSurface;
    final contentStyle = (snack.contentTextStyle ?? theme.textTheme.bodyMedium)
        ?.copyWith(
          color: snack.contentTextStyle?.color ?? scheme.onInverseSurface,
        );
    final actionColor = snack.actionTextColor ?? scheme.inversePrimary;
    final topPadding = MediaQuery.of(context).padding.top + _kVerticalMargin;

    return Positioned(
      top: topPadding,
      left: _kHorizontalMargin,
      right: _kHorizontalMargin,
      // A transient message the user did not ask for is exactly what a live
      // region is for. SnackBar wraps itself the same way; losing it in the
      // migration made every failure silent to a screen reader.
      child: Semantics(
        container: true,
        liveRegion: true,
        child: SlideTransition(
          position: _slide,
          child: Material(
            elevation: snack.elevation ?? 6,
            shadowColor: theme.shadowColor.withValues(alpha: 0.3),
            shape:
                snack.shape ??
                RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
            color: background,
            surfaceTintColor: Colors.transparent,
            child: Padding(
              padding: EdgeInsets.only(
                left: 16,
                right: widget.actionLabel != null ? 4 : 16,
                top: 14,
                bottom: 14,
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Flexible(child: Text(widget.message, style: contentStyle)),
                  if (widget.actionLabel != null) ...[
                    const SizedBox(width: 8),
                    SizedBox(
                      height: 36,
                      child: TextButton(
                        onPressed: () {
                          _dismiss();
                          widget.onAction?.call();
                        },
                        style: TextButton.styleFrom(
                          foregroundColor: actionColor,
                          padding: const EdgeInsets.symmetric(horizontal: 12),
                          minimumSize: Size.zero,
                          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                        ),
                        child: Text(
                          widget.actionLabel!,
                          style: theme.textTheme.labelLarge,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
