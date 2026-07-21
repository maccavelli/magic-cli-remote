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
  final List<String> prompts = [];

  @override
  Future<List<SessionEvent>> sessionHistory(String sessionId) async => const [];

  @override
  Future<void> prompt(String sessionId, String text) async {
    prompts.add(text);
  }
}

Widget _hostWith(SessionTranscript transcript, McremoteClient client) {
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
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

Widget _host(SessionTranscript transcript) =>
    _hostWith(transcript, _FakeClient());

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

  testWidgets('a freshly-connected idle session can send its first message', (
    tester,
  ) async {
    // Guards against the "send button stuck as stop icon on connect" bug seen
    // in other remote-control clients: an idle, connected session must show a
    // live send button and actually deliver the prompt.
    final client = _FakeClient();
    await tester.pumpWidget(
      _hostWith(seeded(const [], status: 'idle'), client),
    );
    await tester.pumpAndSettle();

    // Send affordance, not a stop/interrupt icon.
    expect(find.byIcon(Icons.send), findsOneWidget);
    expect(find.byIcon(Icons.stop), findsNothing);

    await tester.enterText(find.byType(TextField).first, 'hello there');
    await tester.tap(find.byIcon(Icons.send));
    await tester.pumpAndSettle();

    expect(client.prompts, ['hello there']);
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

  testWidgets('permission sheet shows the command and gates "always" behind a '
      'confirm', (tester) async {
    final ev = SessionEvent(
      type: 'permission_request',
      sessionId: 's1',
      permissionId: 'p1',
      toolName: 'Bash',
      text: 'rm -rf /tmp/scratch',
      options: [
        PermissionOption(optionId: 'allow_once', name: 'Allow once', kind: 'allow_once'),
        PermissionOption(
            optionId: 'allow_always', name: 'Allow always', kind: 'allow_always'),
        PermissionOption(optionId: 'reject', name: 'Reject', kind: 'reject_once'),
      ],
    );
    final transcript = SessionTranscript(
      sessionId: 's1',
      status: 'running',
      pendingPermissions: {'p1': ev},
    );

    await tester.pumpWidget(_host(transcript));
    await tester.pumpAndSettle();

    // Context is surfaced: tool name + the actual command.
    expect(find.text('Approve action?'), findsOneWidget);
    expect(find.text('Bash'), findsOneWidget);
    expect(find.text('rm -rf /tmp/scratch'), findsOneWidget);
    expect(find.text('Allow once'), findsOneWidget);
    expect(find.text('Allow always'), findsOneWidget);

    // "Allow always" does not resolve immediately — it asks to confirm first.
    await tester.tap(find.text('Allow always'));
    await tester.pumpAndSettle();
    expect(find.text('Allow always?'), findsOneWidget);
  });

  testWidgets('long-pressing a user message can edit-and-resend it', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(seeded([ChatItem.user('deploy the app')])),
    );
    await tester.pumpAndSettle();

    await tester.longPress(find.text('deploy the app'));
    await tester.pumpAndSettle();
    expect(find.text('Edit & resend'), findsOneWidget);

    await tester.tap(find.text('Edit & resend'));
    await tester.pumpAndSettle();

    // The composer is prefilled with the message, ready to tweak and resend.
    final field = tester.widget<TextField>(find.byType(TextField).first);
    expect(field.controller?.text, 'deploy the app');
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
