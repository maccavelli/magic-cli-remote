import 'package:flutter/widgets.dart';

/// Broadcasts whether an ancestor scroll view is actively scrolling.
///
/// Decorative animations (shimmer, status pulse) depend on this so they pause
/// during drag/fling and free the frame budget for list layout + markdown.
///
/// Named [ChatScrollActivity] to avoid clashing with Flutter's internal
/// `ScrollActivity` class.
class ChatScrollActivity extends InheritedNotifier<ValueNotifier<bool>> {
  const ChatScrollActivity({
    super.key,
    required ValueNotifier<bool> scrolling,
    required super.child,
  }) : super(notifier: scrolling);

  /// True while the user (or a ballistic fling) is moving a watched list.
  static bool isScrolling(BuildContext context) {
    final n = context
        .dependOnInheritedWidgetOfExactType<ChatScrollActivity>()
        ?.notifier;
    return n?.value ?? false;
  }
}

/// Wraps [child] and drives [scrolling] from scroll start/end notifications.
///
/// Place around a [ListView] / [CustomScrollView]. Does not absorb notifications.
class ChatScrollActivitySensor extends StatelessWidget {
  const ChatScrollActivitySensor({
    super.key,
    required this.scrolling,
    required this.child,
  });

  final ValueNotifier<bool> scrolling;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return NotificationListener<ScrollNotification>(
      onNotification: (n) {
        if (n is ScrollStartNotification) {
          if (!scrolling.value) scrolling.value = true;
        } else if (n is ScrollEndNotification) {
          if (scrolling.value) scrolling.value = false;
        }
        return false;
      },
      child: ChatScrollActivity(scrolling: scrolling, child: child),
    );
  }
}
