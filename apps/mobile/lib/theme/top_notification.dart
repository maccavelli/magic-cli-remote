import 'dart:async';

import 'package:flutter/material.dart';

const _kDefaultDuration = Duration(seconds: 3);
const _kSlideDuration = Duration(milliseconds: 250);
const _kVerticalMargin = 8.0;
const _kHorizontalMargin = 12.0;

OverlayEntry? _activeEntry;

void showTopNotification(
  BuildContext context,
  String message, {
  Duration duration = _kDefaultDuration,
  String? actionLabel,
  VoidCallback? onAction,
}) {
  if (!context.mounted) return;

  _activeEntry?.remove();
  _activeEntry = null;

  final overlay = Overlay.of(context, rootOverlay: true);

  late final OverlayEntry entry;
  entry = OverlayEntry(
    builder: (_) => _TopNotification(
      message: message,
      duration: duration,
      actionLabel: actionLabel,
      onAction: onAction,
      onRemoved: () {
        if (_activeEntry == entry) _activeEntry = null;
        entry.remove();
      },
    ),
  );

  _activeEntry = entry;
  overlay.insert(entry);
}

extension TopNotificationX on BuildContext {
  void topNotification(
    String message, {
    Duration duration = _kDefaultDuration,
    String? actionLabel,
    VoidCallback? onAction,
  }) {
    showTopNotification(
      this,
      message,
      duration: duration,
      actionLabel: actionLabel,
      onAction: onAction,
    );
  }
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
    _ctrl = AnimationController(
      duration: _kSlideDuration,
      vsync: this,
    );
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
    final topPadding = MediaQuery.of(context).padding.top + _kVerticalMargin;
    final isLight = theme.brightness == Brightness.light;

    return Positioned(
      top: topPadding,
      left: _kHorizontalMargin,
      right: _kHorizontalMargin,
      child: SlideTransition(
        position: _slide,
        child: Material(
          elevation: 6,
          shadowColor: theme.shadowColor.withValues(alpha: 0.3),
          borderRadius: BorderRadius.circular(12),
          color: isLight
              ? theme.colorScheme.onSurface
              : theme.colorScheme.inverseSurface,
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
                Flexible(
                  child: Text(
                    widget.message,
                    style: TextStyle(
                      color: isLight
                          ? theme.colorScheme.surface
                          : theme.colorScheme.onInverseSurface,
                      fontSize: 14,
                    ),
                  ),
                ),
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
                        foregroundColor: theme.colorScheme.inversePrimary,
                        padding: const EdgeInsets.symmetric(horizontal: 12),
                        minimumSize: Size.zero,
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      child: Text(
                        widget.actionLabel!,
                        style: const TextStyle(
                          fontWeight: FontWeight.w600,
                          fontSize: 13,
                        ),
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
