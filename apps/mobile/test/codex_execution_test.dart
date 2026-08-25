import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/chat/codex_terminals_screen.dart';

CodexTerminalOutput _chunk(int sequence, String text) => CodexTerminalOutput(
  terminalId: 'term-1',
  sequence: sequence,
  stream: 'stdout',
  dataBase64: base64.encode(utf8.encode(text)),
);

class _FakeExecutionClient with CodexExecutionClient {
  _FakeExecutionClient({this.replay = const [], this.replayGap = false});

  final List<CodexTerminalOutput> replay;
  final bool replayGap;
  final _pushes = StreamController<Map<String, dynamic>>.broadcast();
  final calls = <String>[];
  int lastAfterSequence = -1;

  List<CodexTerminalInfo> terminals = const [
    CodexTerminalInfo(
      id: 'term-1',
      kind: 'exec',
      label: 'SANDBOXED EXECUTION',
      command: 'go test ./...',
      running: true,
    ),
    CodexTerminalInfo(
      id: 'shell-1',
      kind: 'thread_shell',
      label: 'UNSANDBOXED SHELL — FULL HOST ACCESS',
      command: 'sudo systemctl restart nginx',
      running: false,
      exitCode: 0,
    ),
  ];

  @override
  Stream<Map<String, dynamic>> get codexTerminalOutput => _pushes.stream;

  void push(String sessionId, CodexTerminalOutput chunk) => _pushes.add({
    'session_id': sessionId,
    'output': {
      'terminal_id': chunk.terminalId,
      'sequence': chunk.sequence,
      'stream': chunk.stream,
      'data': chunk.dataBase64,
    },
  });

  Future<void> close() => _pushes.close();

  @override
  Future<List<CodexTerminalInfo>> listTerminals(String sessionId) async {
    calls.add('list:$sessionId');
    return terminals;
  }

  @override
  Future<CodexTerminalBuffer> readTerminalOutput(
    String sessionId,
    String terminalId, {
    int afterSequence = 0,
  }) async {
    calls.add('output:$terminalId');
    lastAfterSequence = afterSequence;
    return CodexTerminalBuffer(chunks: replay, sequenceGap: replayGap);
  }

  @override
  Future<CodexExecResult> runSandboxedExec(
    String sessionId,
    List<String> argv, {
    String cwd = '',
    String permissionProfileId = '',
    int timeoutMs = 0,
  }) async {
    calls.add('exec:${argv.join(' ')}');
    return const CodexExecResult(label: 'SANDBOXED EXECUTION');
  }

  @override
  Future<void> runUnsandboxedShell(
    String sessionId,
    String command, {
    required bool confirmed,
  }) async {
    if (!confirmed) throw StateError('unconfirmed');
    calls.add('shell:$command');
  }

  @override
  Future<String> spawnStandaloneProcess(
    String sessionId,
    List<String> argv, {
    required String cwd,
    required bool confirmed,
    Map<String, String> env = const {},
    bool tty = false,
    int rows = 0,
    int cols = 0,
  }) async {
    if (!confirmed) throw StateError('unconfirmed');
    calls.add('spawn:${argv.join(' ')}');
    return 'proc-1';
  }

  @override
  Future<void> writeTerminal(
    String sessionId,
    String terminalId,
    String text, {
    bool closeStdin = false,
  }) async {
    calls.add('write:$terminalId:${text.trim()}');
  }

  @override
  Future<void> resizeTerminal(
    String sessionId,
    String terminalId,
    int rows,
    int cols,
  ) async {
    calls.add('resize:$terminalId:${rows}x$cols');
  }

  @override
  Future<void> stopTerminal(String sessionId, String terminalId) async {
    calls.add('stop:$terminalId');
    terminals = terminals
        .map(
          (t) => t.id == terminalId
              ? CodexTerminalInfo(
                  id: t.id,
                  kind: t.kind,
                  label: t.label,
                  command: t.command,
                  running: false,
                  exitCode: 130,
                )
              : t,
        )
        .toList(growable: false);
  }

  @override
  Future<int> stopAllTerminals(String sessionId) async {
    calls.add('stop_all');
    return terminals.where((t) => t.running).length;
  }
}

void main() {
  group('terminal buffer sequencing', () {
    test('append drops duplicates and flags a skipped sequence', () {
      var buffer = const CodexTerminalBuffer();
      buffer = buffer.append([_chunk(1, 'a'), _chunk(2, 'b')]);
      expect(buffer.chunks.length, 2);
      expect(buffer.lastSequence, 2);
      expect(buffer.sequenceGap, isFalse);

      // A redelivered chunk must not double-render output.
      buffer = buffer.append([_chunk(2, 'b')]);
      expect(buffer.chunks.length, 2);
      expect(buffer.sequenceGap, isFalse);

      // A skipped sequence is a real hole, and it stays flagged: the bytes
      // are gone from the daemon's bounded buffer, so later contiguous
      // chunks do not make the transcript whole again.
      buffer = buffer.append([_chunk(5, 'e')]);
      expect(buffer.sequenceGap, isTrue);
      buffer = buffer.append([_chunk(6, 'f')]);
      expect(buffer.sequenceGap, isTrue);
    });

    test('decodes bytes lossily rather than throwing', () {
      const binary = CodexTerminalOutput(dataBase64: 'q80=');
      expect(binary.text, isNotEmpty);
      const broken = CodexTerminalOutput(dataBase64: 'not base64!!');
      expect(broken.text, '');
    });
  });

  group('terminal model', () {
    test('classifies which terminals sit outside the sandbox', () {
      const sandboxed = CodexTerminalInfo(kind: 'exec');
      expect(sandboxed.unsandboxed, isFalse);
      for (final kind in ['thread_shell', 'process', 'background']) {
        expect(CodexTerminalInfo(kind: kind).unsandboxed, isTrue, reason: kind);
      }
    });

    test('environment projection carries no endpoint or credential', () {
      final environment = CodexExecutionEnvironment.fromJson({
        'id': 'builder',
        'runtime_workspace_roots': ['/srv/work'],
        'exec_server_url': 'wss://builder.example:8443',
      });
      expect(environment.id, 'builder');
      expect(environment.runtimeWorkspaceRoots, ['/srv/work']);
      // The model has no field for it, so a daemon bug that leaked the URL
      // still could not surface it in the UI.
      expect(
        environment.toString().contains('wss://'),
        isFalse,
        reason: 'exec server URL must not be modelled on the phone',
      );
    });
  });

  group('unsandboxed confirmation', () {
    test('refuses to send an unconfirmed shell or spawn', () async {
      final fake = _FakeExecutionClient();
      addTearDown(fake.close);
      await expectLater(
        fake.runUnsandboxedShell('s1', 'rm -rf /', confirmed: false),
        throwsStateError,
      );
      await expectLater(
        fake.spawnStandaloneProcess(
          's1',
          ['bash'],
          cwd: '/repo',
          confirmed: false,
        ),
        throwsStateError,
      );
      expect(fake.calls, isEmpty);
      await fake.runUnsandboxedShell('s1', 'printf hi', confirmed: true);
      expect(fake.calls, ['shell:printf hi']);
    });
  });

  group('CodexTerminalsScreen', () {
    testWidgets('shows the unsandboxed label verbatim and marks exit state', (
      tester,
    ) async {
      final fake = _FakeExecutionClient();
      addTearDown(fake.close);
      await tester.pumpWidget(
        MaterialApp(
          home: CodexTerminalsScreen(client: fake, sessionId: 's1'),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('SANDBOXED EXECUTION'), findsOneWidget);
      expect(find.text('UNSANDBOXED SHELL — FULL HOST ACCESS'), findsOneWidget);
      expect(find.text('running'), findsOneWidget);
      expect(find.text('exited (0)'), findsOneWidget);
    });

    testWidgets('replays from the last held sequence, not from zero', (
      tester,
    ) async {
      final fake = _FakeExecutionClient(
        replay: [_chunk(1, 'first\n'), _chunk(2, 'second\n')],
      );
      addTearDown(fake.close);
      await tester.pumpWidget(
        MaterialApp(
          home: CodexTerminalsScreen(client: fake, sessionId: 's1'),
        ),
      );
      await tester.pumpAndSettle();
      expect(fake.lastAfterSequence, 0);

      final output = tester.widget<SelectableText>(
        find.byKey(const Key('terminal-output')),
      );
      expect(output.data, 'first\nsecond\n');

      // Re-selecting asks only for what came after what is already held.
      await tester.tap(find.text('go test ./...'));
      await tester.pumpAndSettle();
      expect(fake.lastAfterSequence, 2);
    });

    testWidgets('renders a live push without waiting for a poll', (
      tester,
    ) async {
      final fake = _FakeExecutionClient(replay: [_chunk(1, 'building\n')]);
      addTearDown(fake.close);
      await tester.pumpWidget(
        MaterialApp(
          home: CodexTerminalsScreen(client: fake, sessionId: 's1'),
        ),
      );
      await tester.pumpAndSettle();

      fake.push('s1', _chunk(2, 'done\n'));
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<SelectableText>(find.byKey(const Key('terminal-output')))
            .data,
        'building\ndone\n',
      );

      // A push for a different session must never enter this view.
      fake.push('other-session', _chunk(3, 'leaked\n'));
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<SelectableText>(find.byKey(const Key('terminal-output')))
            .data,
        isNot(contains('leaked')),
      );
    });

    testWidgets('warns instead of silently splicing across a gap', (
      tester,
    ) async {
      final fake = _FakeExecutionClient(
        replay: [_chunk(9, 'tail\n')],
        replayGap: true,
      );
      addTearDown(fake.close);
      await tester.pumpWidget(
        MaterialApp(
          home: CodexTerminalsScreen(client: fake, sessionId: 's1'),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('terminal-sequence-gap')), findsOneWidget);
    });

    testWidgets('sends stdin and stops a running terminal', (tester) async {
      final fake = _FakeExecutionClient(replay: [_chunk(1, 'prompt: ')]);
      addTearDown(fake.close);
      await tester.pumpWidget(
        MaterialApp(
          home: CodexTerminalsScreen(client: fake, sessionId: 's1'),
        ),
      );
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('terminal-stdin')), 'yes');
      await tester.tap(find.byIcon(Icons.send));
      await tester.pumpAndSettle();
      expect(fake.calls, contains('write:term-1:yes'));

      await tester.tap(find.byIcon(Icons.stop).first);
      await tester.pumpAndSettle();
      expect(fake.calls, contains('stop:term-1'));
      expect(find.text('exited (130)'), findsOneWidget);
      // Stdin disappears with the process it was feeding.
      expect(find.byKey(const Key('terminal-stdin')), findsNothing);
    });

    testWidgets('stop-all is offered only while something is running', (
      tester,
    ) async {
      final fake = _FakeExecutionClient();
      addTearDown(fake.close);
      await tester.pumpWidget(
        MaterialApp(
          home: CodexTerminalsScreen(client: fake, sessionId: 's1'),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byIcon(Icons.stop_circle_outlined), findsOneWidget);

      await tester.tap(find.byIcon(Icons.stop_circle_outlined));
      await tester.pumpAndSettle();
      expect(fake.calls, contains('stop_all'));
    });
  });
}
