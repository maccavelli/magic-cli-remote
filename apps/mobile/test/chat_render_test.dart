import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';
import 'package:magic_cli_remote/theme/widgets.dart';

/// Minimal client: chat only calls sessionHistory() at open, and only when the
/// transcript is empty. We seed a non-empty transcript, so it stays untouched.
class _FakeClient extends McremoteClient {
  final List<String> prompts = [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    prompts.add(text);
  }
}

/// Fake that also answers `session.list`, so the app bar can resolve the
/// session's provider and working directory.
class _MetaClient extends _FakeClient {
  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'grok', cwd: '/home/mac')],
        complete: true,
      );
}

/// Non-live session meta + empty history: triggers the B.1 honesty banner.
class _ClosedMetaClient extends _FakeClient {
  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [
          SessionMeta(
            id: 's1',
            provider: 'grok',
            cwd: '/home/mac',
            live: false,
            status: 'closed',
          ),
        ],
        complete: true,
      );
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

/// Reports the session as OpenCode, which is what gates the provider-specific
/// entries in the session-actions menu.
class _OpencodeMetaClient extends _FakeClient {
  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [
          SessionMeta(id: 's1', provider: 'opencode', cwd: '/home/mac'),
        ],
        complete: true,
      );
}

/// Same host, but with a soft keyboard claiming [keyboardHeight] logical pixels
/// at the bottom of the view — which is all the platform tells us: the keyboard
/// arrives as `MediaQuery.viewInsets.bottom`, and every widget that must stay
/// visible has to be laid out inside what is left.
Widget _hostWithKeyboard(SessionTranscript transcript, double keyboardHeight) {
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
    child: MaterialApp(
      home: Builder(
        builder: (ctx) => MediaQuery(
          data: MediaQuery.of(
            ctx,
          ).copyWith(viewInsets: EdgeInsets.only(bottom: keyboardHeight)),
          child: ChatScreen(sessionId: transcript.sessionId),
        ),
      ),
    ),
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
    // Empty chat notes host-side retention without a false-alarm banner on a
    // brand-new live session (0009 B.1 / D).
    expect(find.textContaining('kept on this host'), findsOneWidget);
    expect(find.byKey(const Key('history-unavailable-banner')), findsNothing);

    await tester.enterText(find.byType(TextField).first, 'hello there');
    await tester.tap(find.byIcon(Icons.send));
    await tester.pumpAndSettle();

    expect(client.prompts, ['hello there']);
  });

  testWidgets('non-live empty history shows dismissible honesty banner (B.1)', (
    tester,
  ) async {
    final client = _ClosedMetaClient();
    await tester.pumpWidget(
      _hostWith(seeded(const [], status: 'idle'), client),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('history-unavailable-banner')), findsOneWidget);
    expect(find.textContaining('No earlier messages'), findsOneWidget);

    await tester.tap(find.text('Got it'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('history-unavailable-banner')), findsNothing);
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

  testWidgets(
    'tool calls are terse and collapsed — detail hidden until tapped',
    (tester) async {
      await tester.pumpWidget(
        _host(
          seeded([
            ChatItem.tool(
              id: 't1',
              name: 'Shell',
              status: 'completed',
              detail: 'secret-command-output',
            ),
          ]),
        ),
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
    },
  );

  testWidgets(
    'auto-approvals collapse into one card; the audit is behind a tap',
    (tester) async {
      // The whole point of MADR 0051 Part I: a turn that auto-approved three
      // things is one transcript line, not three, and the record is still there
      // for anyone who wants to check what ran on their behalf.
      await tester.pumpWidget(
        _host(
          seeded([
            ChatItem.approvals(
              id: 'auto-approvals',
              status: 'completed',
              approvals: const [
                ApprovalItem(toolName: 'bash', detail: 'git status'),
                ApprovalItem(toolName: 'file', detail: 'header.html'),
                ApprovalItem(toolName: 'bash', detail: 'make'),
              ],
            ),
          ]),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.textContaining('bash (git status)'), findsOneWidget);
      expect(find.textContaining('+2 more'), findsOneWidget);
      // Collapsed: the rest are not competing for the reader's attention.
      expect(find.textContaining('header.html'), findsNothing);

      await tester.tap(find.textContaining('+2 more'));
      await tester.pumpAndSettle();
      expect(find.textContaining('header.html'), findsOneWidget);
      expect(find.textContaining('make'), findsOneWidget);
    },
  );

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
        PermissionOption(
          optionId: 'allow_once',
          name: 'Allow once',
          kind: 'allow_once',
        ),
        PermissionOption(
          optionId: 'allow_always',
          name: 'Allow always',
          kind: 'allow_always',
        ),
        PermissionOption(
          optionId: 'reject',
          name: 'Reject',
          kind: 'reject_once',
        ),
      ],
    );
    final transcript = SessionTranscript(
      sessionId: 's1',
      status: 'running',
      pendingPermissions: {'p1': ev},
    );

    // A phone-portrait surface: the default 800x600 test viewport cannot fit
    // the full-height approval sheet, and its options must all be hittable.
    tester.view.physicalSize = const Size(500, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_host(transcript));
    // Bounded pumps, not pumpAndSettle: the RUNNING status chip pulses
    // continuously, so the tree never fully settles. The sheet opens from a
    // post-frame callback, so its route (and entrance animation) needs two
    // extra pump cycles to reach its resting position.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump(const Duration(milliseconds: 500));

    // Context is surfaced: tool name + the actual command.
    expect(find.text('Approve action?'), findsOneWidget);
    expect(find.text('Bash'), findsOneWidget);
    expect(find.text('rm -rf /tmp/scratch'), findsOneWidget);
    expect(find.text('Allow once'), findsOneWidget);
    expect(find.text('Allow always'), findsOneWidget);

    // "Allow always" does not resolve immediately — it asks to confirm first.
    await tester.tap(find.text('Allow always'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('Allow always?'), findsOneWidget);
  });

  // grok's plan approval (MADR 0022 phase 2) arrives as an ordinary
  // permission_request whose detail is a whole plan document, so it renders on
  // the existing sheet with no plan-specific client code. The one thing that
  // does not carry over is the detail box: sized for a shell command it shows
  // four lines of a plan, so a long detail gets a much larger box.
  testWidgets(
    'plan approval renders on the permission sheet with room to read',
    (tester) async {
      final plan = List.generate(
        40,
        (i) => '- step $i: do the thing that step $i does',
      ).join('\n');
      final ev = SessionEvent(
        type: 'permission_request',
        sessionId: 's1',
        permissionId: 'plan1',
        toolName: 'Plan ready for review',
        text: '# Plan\n\n$plan',
        options: [
          PermissionOption(
            optionId: 'plan_approve',
            name: 'Approve plan',
            kind: 'allow_once',
          ),
          PermissionOption(
            optionId: 'plan_changes',
            name: 'Request changes',
            kind: 'reject_once',
          ),
          PermissionOption(
            optionId: 'plan_abandon',
            name: 'Abandon plan',
            kind: 'reject_always',
          ),
        ],
      );
      tester.view.physicalSize = const Size(500, 1200);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(
        _host(
          SessionTranscript(
            sessionId: 's1',
            status: 'running',
            pendingPermissions: {'plan1': ev},
          ),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pump(const Duration(milliseconds: 500));

      expect(find.text('Plan ready for review'), findsOneWidget);
      expect(find.text('Approve plan'), findsOneWidget);

      // The detail box grew past the short-command size (160px). Measured
      // while the sheet is up: answering closes it, and a failed answer no
      // longer re-presents it (that auto-retry was the codex approval loop).
      final box = tester.widget<Container>(
        find
            .ancestor(
              of: find.byKey(const Key('permission-detail')),
              matching: find.byType(Container),
            )
            .first,
      );
      final maxHeight = (box.constraints as BoxConstraints).maxHeight;
      expect(maxHeight, greaterThan(160));

      // "Abandon plan" is a reject option, so it must NOT be gated behind the
      // "always" confirmation dialog that allow_always grants get.
      await tester.tap(find.text('Abandon plan'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Abandon plan?'), findsNothing);
    },
  );

  testWidgets('long-pressing a user message can edit-and-resend it', (
    tester,
  ) async {
    await tester.pumpWidget(_host(seeded([ChatItem.user('deploy the app')])));
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

  testWidgets('consecutive finished commands collapse into one summary row', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        seeded([
          ChatItem.tool(
            id: 'a',
            name: 'npm test',
            status: 'completed',
            toolKind: 'execute',
            detail: 'out-a',
          ),
          ChatItem.tool(
            id: 'b',
            name: 'npm run build',
            status: 'completed',
            toolKind: 'execute',
            detail: 'out-b',
          ),
        ]),
      ),
    );
    await tester.pumpAndSettle();

    // One collapsed summary row; the individual actions are folded away.
    expect(find.text('Ran 2 commands'), findsOneWidget);
    expect(find.text('npm test'), findsNothing);

    await tester.tap(find.text('Ran 2 commands'));
    await tester.pumpAndSettle();
    expect(find.text('npm test'), findsOneWidget);
    expect(find.text('npm run build'), findsOneWidget);
    // Each action's output detail still needs its own tap.
    expect(find.text('out-a'), findsNothing);
  });

  testWidgets('while the agent works, drafts queue instead of sending', (
    tester,
  ) async {
    final client = _FakeClient();
    await tester.pumpWidget(
      _hostWith(seeded([ChatItem.user('go')], status: 'running'), client),
    );
    // Bounded pumps: the RUNNING status chip pulses forever.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    // Busy + empty composer: queue invitation in the hint, stop affordance.
    expect(find.text('Queue a message'), findsOneWidget);
    expect(find.byIcon(Icons.stop), findsOneWidget);

    await tester.enterText(find.byType(TextField).first, 'next task');
    await tester.pump();
    // Busy + drafted text: the action becomes "queue", not send/stop.
    expect(find.byIcon(Icons.schedule_send), findsOneWidget);

    await tester.tap(find.byIcon(Icons.schedule_send));
    await tester.pump();

    // Nothing was sent; the prompt is parked as a removable chip.
    expect(client.prompts, isEmpty);
    expect(find.text('next task'), findsOneWidget);
  });

  testWidgets(
    'submitting a prompt dismisses the keyboard (unfocuses composer)',
    (tester) async {
      final client = _FakeClient();
      await tester.pumpWidget(
        _hostWith(seeded(const [], status: 'idle'), client),
      );
      await tester.pumpAndSettle();

      final field = find.byType(TextField).first;
      await tester.tap(field);
      await tester.pump();
      await tester.enterText(field, 'ship it');
      await tester.pump();

      final focused = tester.widget<TextField>(field).focusNode;
      expect(focused?.hasFocus, isTrue);

      await tester.tap(find.byIcon(Icons.send));
      await tester.pumpAndSettle();

      expect(client.prompts, ['ship it']);
      expect(focused?.hasFocus, isFalse);
    },
  );

  testWidgets('queueing a prompt while busy also dismisses the keyboard', (
    tester,
  ) async {
    final client = _FakeClient();
    await tester.pumpWidget(
      _hostWith(seeded([ChatItem.user('go')], status: 'running'), client),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    final field = find.byType(TextField).first;
    await tester.tap(field);
    await tester.pump();
    await tester.enterText(field, 'queued later');
    await tester.pump();

    final focused = tester.widget<TextField>(field).focusNode;
    expect(focused?.hasFocus, isTrue);

    await tester.tap(find.byIcon(Icons.schedule_send));
    await tester.pump();

    expect(client.prompts, isEmpty);
    expect(focused?.hasFocus, isFalse);
  });

  testWidgets('transcript list is reverse so live end stays pinned', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(seeded([ChatItem.user('hi'), ChatItem.assistant('hello back')])),
    );
    await tester.pumpAndSettle();

    final lists = tester.widgetList<ListView>(find.byType(ListView));
    // Main transcript is reverse; slash-command popup is absent without '/'.
    expect(lists.any((l) => l.reverse), isTrue);
    final transcript = lists.firstWhere((l) => l.reverse);
    expect(
      transcript.keyboardDismissBehavior,
      ScrollViewKeyboardDismissBehavior.onDrag,
    );
  });

  testWidgets('running tools use a static indicator, not a spinner', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        seeded([
          ChatItem.tool(
            id: 't1',
            name: 'Shell',
            status: 'running',
            detail: 'working…',
          ),
        ], status: 'running'),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byIcon(Icons.autorenew), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
  });

  testWidgets('quota errors render the limit card, not a raw red line', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        seeded([
          ChatItem.system(
            'Free usage exceeded, add credits https://opencode.ai/zen',
            error: true,
            errorKind: 'quota',
            retryAt: DateTime.now().add(const Duration(hours: 2)),
          ),
        ]),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Agent quota exceeded'), findsOneWidget);
    expect(find.textContaining('The limit resets at'), findsOneWidget);
    // The raw provider message stays visible as fine print.
    expect(find.textContaining('Free usage exceeded'), findsOneWidget);
  });

  testWidgets('limit reset phrasing labels today / tomorrow / dated resets', (
    tester,
  ) async {
    await tester.pumpWidget(const MaterialApp(home: SizedBox()));
    final context = tester.element(find.byType(SizedBox));
    final loc = MaterialLocalizations.of(context);
    final now = DateTime(2026, 7, 23, 21, 30);

    // Same local calendar day: bare clock time.
    expect(
      limitResetPhrase(context, DateTime(2026, 7, 23, 23, 45), now: now),
      '11:45 PM (in about 2 h 15 min)',
    );
    // Next calendar day is "tomorrow" even though it is under 24 h away.
    expect(
      limitResetPhrase(context, DateTime(2026, 7, 24, 0, 10), now: now),
      'tomorrow 12:10 AM (in about 2 h 40 min)',
    );
    // Still "tomorrow" past the 24 h mark, because it is the next day.
    expect(
      limitResetPhrase(context, DateTime(2026, 7, 24, 23, 0), now: now),
      'tomorrow 11:00 PM (in about 1 d 1 h)',
    );
    // Two calendar days out (only 27 h away): dated, not "tomorrow".
    final in2Days = DateTime(2026, 7, 25, 0, 30);
    expect(
      limitResetPhrase(context, in2Days, now: now),
      '${loc.formatMediumDate(in2Days)} 12:30 AM (in about 1 d 3 h)',
    );
    // Already passed.
    expect(
      limitResetPhrase(context, DateTime(2026, 7, 23, 9, 0), now: now),
      'now — try again',
    );
  });

  testWidgets('rate limits without a reset time show retry guidance', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        seeded([
          ChatItem.system(
            'too many request, please try again',
            error: true,
            errorKind: 'rate_limit',
          ),
        ]),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Agent rate-limited'), findsOneWidget);
    expect(find.textContaining('Give it a moment'), findsOneWidget);
  });

  testWidgets('app bar shows which CLI is being controlled and where', (
    tester,
  ) async {
    await tester.pumpWidget(
      _hostWith(seeded([ChatItem.assistant('hi')]), _MetaClient()),
    );
    await tester.pumpAndSettle();

    // "grok /home/mac" — provider label first, then the working directory.
    expect(
      find.textContaining('grok /home/mac', findRichText: true),
      findsOneWidget,
    );
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

  testWidgets('multi-item history batch does not entrance-animate', (
    tester,
  ) async {
    // Seed a multi-item transcript at open: length jump from empty is not
    // possible via override, so drive the pane through the real notifier —
    // first multi-item commit must leave animate:false on every fade.
    final client = _FakeClient();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(client),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 20));

    final container = ProviderScope.containerOf(
      tester.element(find.byType(ChatScreen)),
    );
    final n = container.read(transcriptsProvider.notifier);
    // Batch of three via history apply (multi-item in one progressive commit
    // of size < batch, or resync). Use replayHistory on empty transcript.
    await n.replayHistory('s1', [
      SessionEvent(
        type: 'user_message',
        sessionId: 's1',
        seq: 1,
        text: 'hello from history',
      ),
      SessionEvent(
        type: 'assistant_message_chunk',
        sessionId: 's1',
        seq: 2,
        text: 'restored reply',
      ),
      SessionEvent(
        type: 'user_message',
        sessionId: 's1',
        seq: 3,
        text: 'another',
      ),
    ]);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 20));
    await tester.pump();

    expect(find.text('hello from history'), findsOneWidget);
    final fades = tester
        .widgetList<EntranceFade>(find.byType(EntranceFade))
        .toList();
    expect(fades, isNotEmpty);
    for (final fade in fades) {
      expect(
        fade.animate,
        isFalse,
        reason: 'restored history must render instantly',
      );
    }
  });

  testWidgets('a live single append may entrance-animate', (tester) async {
    final client = _FakeClient();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(client),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 20));

    final container = ProviderScope.containerOf(
      tester.element(find.byType(ChatScreen)),
    );
    container
        .read(transcriptsProvider.notifier)
        .debugOnEvent(
          SessionEvent(
            type: 'user_message',
            sessionId: 's1',
            seq: 1,
            text: 'live tip',
          ),
        );
    await tester.pump();

    final fades = tester
        .widgetList<EntranceFade>(find.byType(EntranceFade))
        .toList();
    expect(fades, isNotEmpty);
    expect(
      fades.any((f) => f.animate),
      isTrue,
      reason: 'genuinely new live rows should fade in',
    );
  });

  testWidgets('send during an in-flight prompt queues instead of dropping', (
    tester,
  ) async {
    final client = _HangingClient();
    await tester.pumpWidget(
      _hostWith(seeded(const [], status: 'idle'), client),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).first, 'first');
    await tester.pump();
    await tester.tap(find.byIcon(Icons.send));
    await tester.pump(); // RPC in flight → _sending

    // Composer shows queue affordance while the first send is outstanding.
    await tester.enterText(find.byType(TextField).first, 'second');
    await tester.pump();
    expect(find.byTooltip('Queue message'), findsOneWidget);

    await tester.tap(find.byTooltip('Queue message'));
    await tester.pump();

    // First still the only RPC; second is parked as a chip.
    expect(client.prompts, ['first']);
    expect(find.text('second'), findsOneWidget);

    client.gate.complete();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
  });

  testWidgets('identical queued texts delete by identity, not first match', (
    tester,
  ) async {
    final client = _FakeClient();
    await tester.pumpWidget(
      _hostWith(seeded([ChatItem.user('go')], status: 'running'), client),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    // Composer queue button is an IconButton; chip avatars also use the same
    // icon, so target the tooltip rather than the bare icon.
    Finder queueBtn() => find.byTooltip('Queue message');

    final field = find.byType(TextField).first;
    await tester.enterText(field, 'same');
    await tester.pump();
    await tester.tap(queueBtn());
    await tester.pump();

    await tester.enterText(field, 'same');
    await tester.pump();
    await tester.tap(queueBtn());
    await tester.pump();

    expect(find.widgetWithText(InputChip, 'same'), findsNWidgets(2));

    // Delete the second chip only (identity, not first indexOf match).
    final deleteButtons = find.byTooltip('Remove queued message');
    expect(deleteButtons, findsNWidgets(2));
    await tester.tap(deleteButtons.at(1));
    await tester.pump();

    expect(find.widgetWithText(InputChip, 'same'), findsOneWidget);
  });

  testWidgets('the session menu no longer offers "Restore reverts"', (
    tester,
  ) async {
    // MADR 0042 D10: revert is unreachable from this client — `session.revert`
    // requires a message_id and `event.Event` carries none — so its undo was an
    // undo for an action the app cannot perform. Both halves are gone.
    await tester.pumpWidget(
      _hostWith(seeded([ChatItem.assistant('hi')]), _OpencodeMetaClient()),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    await tester.tap(find.byTooltip('Session actions'));
    await tester.pumpAndSettle();

    expect(
      find.text('Fork session'),
      findsOneWidget,
      reason: 'the OpenCode-gated block still renders',
    );
    expect(find.text('Restore reverts'), findsNothing);
  });

  testWidgets('a collapsed group names the tool it is running', (tester) async {
    // MADR 0042 D1: the fold must never be opaque about work in progress.
    await tester.pumpWidget(
      _host(
        seeded([
          ChatItem.tool(
            id: 't1',
            name: 'npm ci',
            status: 'completed',
            toolKind: 'execute',
          ),
          ChatItem.tool(
            id: 't2',
            name: 'npm test',
            status: 'running',
            toolKind: 'execute',
          ),
        ], status: 'running'),
      ),
    );
    // Bounded pumps: the live head shimmers, so pumpAndSettle would never
    // return.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 16));

    expect(find.text('Ran 2 commands'), findsOneWidget);
    expect(
      find.text('· npm test'),
      findsOneWidget,
      reason: 'the running member is named while collapsed',
    );
    // The finished member stays folded away until the row is expanded.
    expect(find.text('npm ci'), findsNothing);
  });

  group('auto-follow does not fight the user (MADR 0042 D5)', () {
    /// Mount a chat with enough content to scroll, driving the real notifier so
    /// appends go through the same path a live event does.
    Future<TranscriptsNotifier> mountScrollable(WidgetTester tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            mcremoteClientProvider.overrideWithValue(_FakeClient()),
            connectionStateProvider.overrideWith(
              (ref) => Stream.value(McConnectionState.connected),
            ),
          ],
          child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
        ),
      );
      await tester.pump();
      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
      );
      final n = container.read(transcriptsProvider.notifier);
      for (var i = 0; i < 30; i++) {
        n.debugOnEvent(
          SessionEvent(
            type: 'user_message',
            sessionId: 's1',
            seq: i + 1,
            text: 'message number $i',
          ),
        );
      }
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));
      return n;
    }

    /// The transcript's own scrollable. `find.byType(Scrollable).last` picks up
    /// the composer's EditableText instead, whose extent is always 0.
    ScrollPosition positionOf(WidgetTester tester) => tester
        .state<ScrollableState>(
          find
              .descendant(
                of: find.byType(ListView),
                matching: find.byType(Scrollable),
              )
              .first,
        )
        .position;

    testWidgets('an append still pins to the live end when idle', (
      tester,
    ) async {
      final n = await mountScrollable(tester);
      final pos = positionOf(tester);
      expect(pos.maxScrollExtent, greaterThan(0), reason: 'must be scrollable');

      // Inside the 120px near-bottom band, so auto-follow stays armed.
      pos.jumpTo(60);
      await tester.pump();

      n.debugOnEvent(
        SessionEvent(
          type: 'user_message',
          sessionId: 's1',
          seq: 100,
          text: 'fresh',
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(pos.pixels, 0, reason: 'idle auto-follow must still work');
    });

    testWidgets('an append mid-drag does not yank the list', (tester) async {
      final n = await mountScrollable(tester);
      final pos = positionOf(tester);

      // Start a real drag so ScrollStartNotification sets the activity flag.
      final gesture = await tester.startGesture(const Offset(200, 300));
      await gesture.moveBy(const Offset(0, 40));
      await tester.pump();
      final duringDrag = pos.pixels;
      expect(duringDrag, greaterThan(0), reason: 'drag moved the list');

      n.debugOnEvent(
        SessionEvent(
          type: 'user_message',
          sessionId: 's1',
          seq: 100,
          text: 'arrives mid-gesture',
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        pos.pixels,
        duringDrag,
        reason: 'jumpTo would goIdle() and cancel the drag',
      );

      await gesture.up();
      await tester.pumpAndSettle();
    });
  });

  testWidgets('the composer stays visible above the soft keyboard', (
    tester,
  ) async {
    // Regression guard. The composer briefly lived in
    // Scaffold.bottomNavigationBar; Scaffold positions that slot at
    // `size.height - barHeight` and applies viewInsets to the body only, so the
    // keyboard covered the field you were typing into. Anything that moves the
    // composer back out of the body reopens that bug.
    const keyboard = 300.0;
    await tester.pumpWidget(
      _hostWithKeyboard(seeded([ChatItem.assistant('hi')]), keyboard),
    );
    await tester.pump();

    final screenHeight = tester.getSize(find.byType(ChatScreen)).height;
    final composer = tester.getRect(find.byType(TextField).first);
    expect(
      composer.bottom,
      lessThanOrEqualTo(screenHeight - keyboard),
      reason: 'composer must lay out above the keyboard, not behind it',
    );
  });

  group('mode-backed slash commands', () {
    SessionTranscript withModes({
      List<SessionMode> modes = const [],
      String? current,
      List<AvailableCommand> commands = const [],
    }) => SessionTranscript(
      sessionId: 's1',
      status: 'idle',
      modes: modes,
      currentModeId: current,
      commands: commands,
    );

    testWidgets('/plan is not offered when the agent has no modes', (
      tester,
    ) async {
      await tester.pumpWidget(_host(withModes()));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/pl');
      await tester.pumpAndSettle();
      // Nothing matches "/pl" without modes; the list hides entirely.
      expect(find.text('/plan'), findsNothing);
      // Sanity: the built-ins are still offered on this session.
      await tester.enterText(find.byType(TextField).first, '/mod');
      await tester.pumpAndSettle();
      expect(find.text('/model'), findsOneWidget);
      expect(find.text('/mode'), findsNothing);
    });

    testWidgets('/plan and /mode are offered once modes are advertised', (
      tester,
    ) async {
      await tester.pumpWidget(
        _host(
          withModes(
            modes: const [
              SessionMode(id: 'default', name: 'Build'),
              SessionMode(id: 'plan', name: 'Plan'),
            ],
            current: 'default',
          ),
        ),
      );
      await tester.pumpAndSettle();
      // Narrowed queries: the suggestion list is height-bounded, so trailing
      // entries are not built when everything matches.
      await tester.enterText(find.byType(TextField).first, '/pl');
      await tester.pumpAndSettle();
      expect(find.text('/plan'), findsOneWidget);

      // "/mo", not "/mode": the composer's own text would otherwise match too.
      await tester.enterText(find.byType(TextField).first, '/mo');
      await tester.pumpAndSettle();
      expect(find.text('/mode'), findsOneWidget);
      expect(find.text('/model'), findsOneWidget);
    });

    testWidgets('an agent-advertised /plan is not shadowed by the built-in', (
      tester,
    ) async {
      await tester.pumpWidget(
        _host(
          withModes(
            modes: const [SessionMode(id: 'plan', name: 'Plan')],
            current: 'plan',
            commands: [
              AvailableCommand(name: 'plan', description: "the agent's own"),
            ],
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/pl');
      await tester.pumpAndSettle();

      expect(find.text('/plan'), findsOneWidget);
      expect(find.textContaining("the agent's own"), findsOneWidget);
    });

    const planModes = [
      SessionMode(id: 'default', name: 'Build'),
      SessionMode(id: 'plan', name: 'Plan'),
    ];

    testWidgets('a normal mode is shown plainly in the app bar', (
      tester,
    ) async {
      await tester.pumpWidget(
        _host(withModes(modes: planModes, current: 'default')),
      );
      await tester.pumpAndSettle();
      expect(find.text('Build'), findsOneWidget);
      expect(find.byIcon(Icons.edit_off), findsNothing);
    });

    testWidgets('plan mode is called out in the app bar', (tester) async {
      await tester.pumpWidget(
        _host(withModes(modes: planModes, current: 'plan')),
      );
      await tester.pumpAndSettle();
      expect(find.text('Plan'), findsOneWidget);
      // "the agent will not touch my files" deserves more than a label.
      expect(find.byIcon(Icons.edit_off), findsOneWidget);
    });
  });

  // The daemon resolves which canonical commands work in a session and sends the
  // answer; the composer renders that rather than guessing (MADR 0023).
  group('daemon-advertised slash commands', () {
    SessionTranscript advertising(
      List<RemoteCommand> remote, {
      List<AvailableCommand> commands = const [],
    }) => SessionTranscript(
      sessionId: 's1',
      status: 'idle',
      remoteCommands: remote,
      commands: commands,
    );

    testWidgets('offers what the daemon says works here', (tester) async {
      await tester.pumpWidget(
        _host(
          advertising([
            RemoteCommand(
              name: 'compact',
              description: 'Summarise the conversation',
              available: true,
            ),
            RemoteCommand(
              name: 'context',
              description: 'Show context usage',
              available: true,
            ),
          ]),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/co');
      await tester.pumpAndSettle();

      expect(find.text('/compact'), findsOneWidget);
      expect(find.text('/context'), findsOneWidget);
      expect(find.textContaining('Summarise the conversation'), findsOneWidget);
    });

    testWidgets('does not offer commands this session cannot run', (
      tester,
    ) async {
      await tester.pumpWidget(
        _host(
          advertising([
            RemoteCommand(
              name: 'compact',
              available: false,
              reason: 'grok compacts only in its own terminal UI',
            ),
            RemoteCommand(name: 'clear', available: true),
          ]),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/c');
      await tester.pumpAndSettle();

      expect(find.text('/compact'), findsNothing);
      expect(find.text('/clear'), findsOneWidget);
    });

    // The daemon's list replaces the hardcoded one entirely: a command it does
    // not advertise is not offered, even if the app used to know about it.
    testWidgets('the hardcoded list yields to the daemon', (tester) async {
      await tester.pumpWidget(
        _host(advertising([RemoteCommand(name: 'help', available: true)])),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/mod');
      await tester.pumpAndSettle();
      expect(find.text('/model'), findsNothing);
    });

    // Against a daemon too old to advertise, the built-in list still drives
    // autocomplete.
    testWidgets('falls back to the built-in list when nothing is advertised', (
      tester,
    ) async {
      await tester.pumpWidget(_host(advertising(const [])));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/mod');
      await tester.pumpAndSettle();
      expect(find.text('/model'), findsOneWidget);
    });

    testWidgets("an agent's own command keeps its own description", (
      tester,
    ) async {
      await tester.pumpWidget(
        _host(
          advertising(
            [
              RemoteCommand(
                name: 'compact',
                description: 'the daemon version',
                available: true,
              ),
            ],
            commands: [
              AvailableCommand(
                name: 'compact',
                description: 'the agent version',
              ),
            ],
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '/comp');
      await tester.pumpAndSettle();

      expect(find.text('/compact'), findsOneWidget);
      expect(find.textContaining('the agent version'), findsOneWidget);
      expect(find.textContaining('the daemon version'), findsNothing);
    });
  });
}

/// prompt() that stalls until [gate] completes — covers the `_sending` window.
class _HangingClient extends _FakeClient {
  final gate = Completer<void>();

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    prompts.add(text);
    await gate.future;
  }
}
