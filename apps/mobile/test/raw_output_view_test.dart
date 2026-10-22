import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/raw_output_view.dart';

String _fixture(String path) =>
    File('test/fixtures/raw_output/$path').readAsStringSync();

Widget _host(Widget child) => MaterialApp(
  home: Scaffold(body: SingleChildScrollView(child: child)),
);

/// Finds text in plain and rich text alike. Assertions stay on content, never
/// on layout widths: the test font is fixed-width.
Finder _text(String s) => find.textContaining(s, findRichText: true);

void main() {
  tearDown(() => RawOutputView.debugThrowInRenderer = false);

  testWidgets('terminal output keeps its text and drops its escapes', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(RawOutputView(text: _fixture('grok/04-tool.txt'))),
    );
    expect(_text('pkg/beta'), findsOneWidget);
    expect(_text('│ beta     │ failed │'), findsOneWidget);
    expect(_text('\x1b'), findsNothing);
  });

  testWidgets('JSON is pretty-printed', (tester) async {
    await tester.pumpWidget(
      _host(RawOutputView(text: '{"name":"alpha","ports":[80,443]}')),
    );
    expect(_text('"name": "alpha"'), findsOneWidget);
    expect(_text('    80,'), findsOneWidget);
  });

  testWidgets('a diff shows its added and removed lines', (tester) async {
    await tester.pumpWidget(
      _host(RawOutputView(text: _fixture('kilo/02-tool.txt'))),
    );
    expect(_text('+\tfmt.Println("hello, world")'), findsOneWidget);
    expect(_text('-\tprintln("hello")'), findsOneWidget);
  });

  testWidgets('Markdown renders as Markdown', (tester) async {
    await tester.pumpWidget(
      _host(RawOutputView(text: _fixture('codex/06-assistant.txt'))),
    );
    expect(find.byType(MarkdownBody), findsOneWidget);
    expect(_text('Command Summary'), findsOneWidget);
  });

  testWidgets('plain text shows as it is', (tester) async {
    await tester.pumpWidget(_host(const RawOutputView(text: 'done')));
    expect(_text('done'), findsOneWidget);
  });

  testWidgets('long output collapses behind "Show more"', (tester) async {
    final long = List.generate(400, (i) => 'line $i').join('\n');
    await tester.pumpWidget(_host(RawOutputView(text: long, collapseAt: 100)));
    expect(_text('line 399'), findsNothing);
    await tester.tap(find.text('Show more'));
    await tester.pump();
    expect(_text('line 399'), findsOneWidget);
  });

  testWidgets('a renderer that throws falls back to plain text, never hides '
      'it', (tester) async {
    RawOutputView.debugThrowInRenderer = true;
    await tester.pumpWidget(
      _host(RawOutputView(text: _fixture('grok/04-tool.txt'))),
    );
    expect(tester.takeException(), isNull);
    expect(_text('pkg/beta'), findsOneWidget);
  });
}
