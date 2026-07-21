import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Minimal client: chat only calls sessionHistory() at open, and only when the
/// transcript is empty. We seed a non-empty transcript, so it stays untouched.
class _FakeClient extends McremoteClient {
  @override
  Future<List<SessionEvent>> sessionHistory(String sessionId) async => const [];
}

Widget _host(SessionTranscript transcript) {
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(_FakeClient()),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider(
        transcript.sessionId,
      ).overrideWithValue(transcript),
    ],
    child: MaterialApp(home: ChatScreen(sessionId: transcript.sessionId)),
  );
}

void main() {
  SessionTranscript seeded(List<ChatItem> items, {String status = 'idle'}) {
    // Assign monotonic seqs like the reducer does, so widget keys are stable.
    final withSeq = [
      for (var i = 0; i < items.length; i++) items[i].copyWith(seq: i),
    ];
    return SessionTranscript(
      sessionId: 's1',
      status: status,
      items: withSeq,
      nextSeq: items.length,
    );
  }

  testWidgets('assistant replies render through markdown, not raw text', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(seeded([ChatItem.assistant('# Heading\n\n**bold** body')])),
    );
    await tester.pumpAndSettle();

    // Rendered as markdown (heading/emphasis), so the raw markers are gone.
    expect(find.byType(MarkdownBody), findsOneWidget);
    expect(find.text('# Heading\n\n**bold** body'), findsNothing);
  });

  testWidgets('a still-streaming assistant reply also renders as markdown', (
    tester,
  ) async {
    // status 'running' + last item ⇒ the live/streaming bubble. It must render
    // formatted, not leak raw markdown while the turn is in flight.
    await tester.pumpWidget(
      _host(seeded([ChatItem.assistant('**live**')], status: 'running')),
    );
    await tester.pump();

    expect(find.byType(MarkdownBody), findsOneWidget);
    expect(find.text('**live**'), findsNothing);
  });

  testWidgets('tool calls are terse and collapsed — detail hidden until tapped',
      (tester) async {
    await tester.pumpWidget(
      _host(seeded([
        ChatItem.tool(
          id: 't1',
          name: 'Shell',
          status: 'completed',
          detail: 'secret-command-output',
        ),
      ])),
    );
    await tester.pumpAndSettle();

    // The terse header line is shown…
    expect(find.text('Shell'), findsOneWidget);
    // …but the detail stays minimized by default.
    expect(find.text('secret-command-output'), findsNothing);

    // Expanding reveals it on demand.
    await tester.tap(find.text('Shell'));
    await tester.pumpAndSettle();
    expect(find.text('secret-command-output'), findsOneWidget);
  });

  testWidgets('typing / surfaces built-in slash commands in autocomplete', (
    tester,
  ) async {
    // Seeded transcript advertises no agent commands, so this proves the
    // built-ins (/model, /help, …) are offered on their own.
    await tester.pumpWidget(_host(seeded([ChatItem.assistant('hi')])));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).first, '/');
    await tester.pumpAndSettle();

    expect(find.text('/model'), findsOneWidget);
    expect(find.text('/reset'), findsOneWidget);
    expect(find.text('/help'), findsOneWidget);

    // Narrowing filters the list to matching built-ins.
    await tester.enterText(find.byType(TextField).first, '/mo');
    await tester.pumpAndSettle();
    expect(find.text('/model'), findsOneWidget);
    expect(find.text('/help'), findsNothing);
  });

  testWidgets('thoughts are collapsed by default', (tester) async {
    await tester.pumpWidget(
      _host(seeded([ChatItem.thought('internal reasoning here')])),
    );
    await tester.pumpAndSettle();

    expect(find.text('Thought'), findsOneWidget);
    expect(find.text('internal reasoning here'), findsNothing);

    await tester.tap(find.text('Thought'));
    await tester.pumpAndSettle();
    expect(find.text('internal reasoning here'), findsOneWidget);
  });
}
