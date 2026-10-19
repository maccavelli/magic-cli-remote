import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';

/// MADR 0201 D36 (0073 §7.4). The post-frame callback a streaming update
/// schedules checked `mounted` and `_upToDate(true)`, but not
/// `widget.streaming`. A reply finished between that update and the callback
/// was re-rendered as streaming, caret and all.
void main() {
  testWidgets('a reply finished before the post-frame render stays final', (
    tester,
  ) async {
    final caret = find.byWidgetPredicate(
      (w) => w.runtimeType.toString() == '_StreamingCaret',
    );
    var data = 'chunk';
    var streaming = true;
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              update = setState;
              return debugAssistantMarkdown(data: data, streaming: streaming);
            },
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 16));
    expect(caret, findsOneWidget, reason: 'the reply starts out streaming');

    final owner = tester.binding.buildOwner!;
    final root = tester.binding.rootElement!;
    // A streaming update, built now: it schedules the post-frame render.
    update(() => data = 'chunk and more');
    owner.buildScope(root);
    // The reply finishes before that frame: built now too.
    update(() => streaming = false);
    owner.buildScope(root);
    // The frame, and the post-frame callback with it.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 16));

    expect(caret, findsNothing, reason: 'a finished reply has no caret');
  });
}
