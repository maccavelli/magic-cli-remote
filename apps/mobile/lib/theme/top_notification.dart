import 'dart:async';

import 'package:flutter/material.dart';

import 'celestial.dart';

const _kDefaultDuration = Duration(seconds: 3);

/// Failures get longer on screen than confirmations. Nothing in the app keeps a
/// notification history, so a failure missed in three seconds is a failure the
/// user never learns about.
const _kErrorDuration = Duration(seconds: 5);
const _kActionDuration = Duration(seconds: 6);
const _kSlideDuration = Duration(milliseconds: 250);
const _kVerticalMargin = 8.0;
const _kHorizontalMargin = 12.0;

/// Accent rail width. Matches the transcript's status tiles, which use the same
/// left-rail idiom to carry state.
const _kRailWidth = 3.0;

/// How many messages may wait behind the one on screen. A burst should not
/// become a slideshow, but silently discarding all but the last loses the
/// first error of a cascade — which is usually the informative one.
const _kMaxQueued = 3;

/// What a notification is telling the user, so the card can say it without the
/// reader having to parse the sentence.
///
/// Always passed explicitly, never inferred from the message text. The same
/// rule the transcript follows for [ChatItem.isError]: a message that merely
/// begins "Error:" must not be styled as a failure just because of how it
/// happens to be worded.
enum NoticeSeverity {
  /// Nothing broke — the action simply is not available right now
  /// ("Reconnect to the host first", "No file changes"). The default, and the
  /// most common case, so it stays visually quiet.
  info,

  /// The thing the user asked for happened ("Copied", "Session ended").
  success,

  /// An operation failed ("Resume failed: …", "Could not load models: …").
  error,
}

extension on NoticeSeverity {
  /// Which message survives when the queue overflows; higher wins.
  ///
  /// A confirmation is the most disposable — a "Copied" that never appears
  /// costs nothing, because the copy still happened. A blocked action still
  /// explains why nothing happened, and a failure is the whole reason the
  /// queue exists.
  int get _keepPriority => switch (this) {
    NoticeSeverity.success => 0,
    NoticeSeverity.info => 1,
    NoticeSeverity.error => 2,
  };
}

OverlayEntry? _activeEntry;
OverlayState? _overlay;
final _queue = <_Pending>[];

class _Pending {
  const _Pending(
    this.message,
    this.duration,
    this.actionLabel,
    this.onAction,
    this.severity,
  );
  final String message;
  final Duration duration;
  final String? actionLabel;
  final VoidCallback? onAction;
  final NoticeSeverity severity;
}

void showTopNotification(
  BuildContext context,
  String message, {
  NoticeSeverity severity = NoticeSeverity.info,
  Duration? duration,
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
  final hasAction = actionLabel != null || onAction != null;
  _queue.add(
    _Pending(
      message,
      duration ?? _durationFor(severity, hasAction: hasAction),
      actionLabel,
      onAction,
      severity,
    ),
  );
  _evictToCapacity();
  if (_activeEntry == null) _showNext();
}

Duration _durationFor(NoticeSeverity severity, {required bool hasAction}) {
  // An action must stay reachable long enough to tap, whatever the severity.
  if (hasAction) return _kActionDuration;
  return severity == NoticeSeverity.error ? _kErrorDuration : _kDefaultDuration;
}

/// Trim the queue by importance rather than by age.
///
/// Dropping the oldest — what this did before — discards exactly the message
/// the comment on [_kMaxQueued] says matters most: the first error of a
/// cascade. Severity gives a better rule, and among equals the oldest still
/// goes first, so a run of failures still degrades the way it used to.
void _evictToCapacity() {
  while (_queue.length > _kMaxQueued) {
    var victim = 0;
    for (var i = 1; i < _queue.length; i++) {
      if (_queue[i].severity._keepPriority <
          _queue[victim].severity._keepPriority) {
        victim = i;
      }
    }
    _queue.removeAt(victim);
  }
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
      severity: next.severity,
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
  final NoticeSeverity severity;
  final VoidCallback onRemoved;

  const _TopNotification({
    required this.message,
    required this.duration,
    this.actionLabel,
    this.onAction,
    this.severity = NoticeSeverity.info,
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

    // Severity rides on an accent rail and a leading glyph rather than a
    // tinted card: a red slab for "could not load models" is out of proportion
    // to what happened, and it would undo the point of putting these on the
    // app's own surface. The rail is the same idiom the transcript's status
    // tiles use, so a failure here reads like a failed tool row there.
    //
    // info deliberately gets neither — it is the most common notification in
    // the app ("Reconnect to the host first"), and accenting it would spend the
    // signal on the case where nothing went wrong.
    final tokens = celestialOf(context);
    final (Color? accent, IconData? icon) = switch (widget.severity) {
      NoticeSeverity.error => (scheme.error, Icons.error_outline),
      NoticeSeverity.success => (tokens.success, Icons.check_circle_outline),
      NoticeSeverity.info => (null, null),
    };

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
        child: Dismissible(
          key: ValueKey<Object>(widget),
          direction: DismissDirection.up,
          // Removed outright, not via _dismiss(): the child is already gone and
          // collapsed by the time this fires, so replaying the slide-out left a
          // dismissed Dismissible in the tree for another 250 ms — a debug
          // assertion if anything rebuilt it — and delayed the next queued
          // message by that much again (MADR 0046 L-11).
          onDismissed: (_) {
            _dismissTimer?.cancel();
            if (mounted) widget.onRemoved();
          },
          child: SlideTransition(
            position: _slide,
            child: Material(
              elevation: snack.elevation ?? 6,
              shadowColor: theme.shadowColor.withValues(alpha: 0.3),
              shape:
                  snack.shape ??
                  RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(12),
                  ),
              color: background,
              surfaceTintColor: Colors.transparent,
              // Clip so the rail's square end is trimmed to the card's radius.
              clipBehavior: accent == null ? Clip.none : Clip.antiAlias,
              child: _withRail(
                accent,
                Padding(
                  padding: EdgeInsets.only(
                    left: 16,
                    right: widget.actionLabel != null ? 4 : 16,
                    top: 14,
                    bottom: 14,
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (icon != null) ...[
                        // Decorative: the message already says "failed". A
                        // screen reader announcing a glyph adds nothing.
                        ExcludeSemantics(
                          child: Icon(icon, size: 18, color: accent),
                        ),
                        const SizedBox(width: 10),
                      ],
                      Flexible(
                        child: Text(widget.message, style: contentStyle),
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
                              foregroundColor: actionColor,
                              padding: const EdgeInsets.symmetric(
                                horizontal: 12,
                              ),
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
        ),
      ),
    );
  }

  /// Paint the accent rail down the leading edge.
  ///
  /// [DecoratedBox] rather than a [Container] border or a [Row]: it draws
  /// without taking part in layout, so the rail overlays the leading 3px of the
  /// existing padding and the message text stays exactly where it sits on an
  /// unaccented card.
  Widget _withRail(Color? accent, Widget child) {
    if (accent == null) return child;
    return DecoratedBox(
      decoration: BoxDecoration(
        border: Border(
          left: BorderSide(color: accent, width: _kRailWidth),
        ),
      ),
      position: DecorationPosition.foreground,
      child: child,
    );
  }
}
