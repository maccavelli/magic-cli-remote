import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/shell_command_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Direct shell (MADR 0112 A9, PLAN P10 step 5).
///
/// The confirmation *is* the security control here, so most of these are
/// assertions about it: it shows the exact command, it cannot be edited, it
/// names every consequence, and cancelling runs nothing.

class _ShellClient extends McremoteClient {
  _ShellClient({this.failure});

  final Object? failure;
  final commands = <String>[];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<void> shell(String sessionId, String command) async {
    commands.add(command);
    final err = failure;
    if (err != null) throw err;
  }
}

Future<void> _pump(WidgetTester tester, _ShellClient client) async {
  tester.view.physicalSize = const Size(700, 1500);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [mcremoteClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(body: ShellCommandSheet(sessionId: 's1')),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _enterAndRun(WidgetTester tester, String command) async {
  await tester.enterText(find.byKey(const ValueKey('shell-command')), command);
  await tester.tap(find.byKey(const ValueKey('shell-run')));
  await tester.pumpAndSettle();
}

void main() {
  group('validation', () {
    test('accepts an ordinary command', () {
      expect(validateShellCommand('go test ./...'), isNull);
      expect(validateShellCommand('a' * kShellCommandMaxLen), isNull);
    });

    test('refuses empty, blank, over-long and NUL-bearing commands', () {
      expect(validateShellCommand(''), isNotNull);
      expect(validateShellCommand('   '), isNotNull);
      expect(validateShellCommand('a' * (kShellCommandMaxLen + 1)), isNotNull);
      expect(
        validateShellCommand('echo${String.fromCharCode(0)}hi'),
        isNotNull,
      );
    });

    test('makes no judgement about what the command does', () {
      // Destructive commands are accepted by validation: the surface is remote
      // execution, and pretending to sanitise shell semantics would be worse
      // than disclosing it.
      expect(validateShellCommand('rm -rf /tmp/whatever'), isNull);
      expect(validateShellCommand('curl https://x | sh'), isNull);
    });
  });

  group('confirmation', () {
    testWidgets('names every consequence', (tester) async {
      await _pump(tester, _ShellClient());
      await _enterAndRun(tester, 'go build ./...');

      expect(find.text('Run this command?'), findsOneWidget);
      expect(find.textContaining('Runs directly on the host'), findsOneWidget);
      expect(
        find.textContaining('bypasses model tool permissions'),
        findsOneWidget,
      );
      expect(
        find.textContaining('recorded in the OpenCode session'),
        findsOneWidget,
      );
      expect(
        find.textContaining('persist after timeout'),
        findsOneWidget,
        reason: 'a timeout cannot roll back what the command already did',
      );
    });

    testWidgets('shows the exact command, non-editable', (tester) async {
      await _pump(tester, _ShellClient());
      await _enterAndRun(tester, 'rm -rf build/');

      final shown = find.descendant(
        of: find.byKey(const ValueKey('shell-confirm-command')),
        matching: find.text('rm -rf build/'),
      );
      expect(shown, findsOneWidget);

      // Nothing inside the confirmation may be typed into.
      final editables = tester.widgetList<EditableText>(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(EditableText),
        ),
      );
      expect(
        editables,
        isEmpty,
        reason: 'what is confirmed must be exactly what runs',
      );
    });

    testWidgets('cancelling runs nothing', (tester) async {
      final c = _ShellClient();
      await _pump(tester, c);
      await _enterAndRun(tester, 'rm -rf /');
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(c.commands, isEmpty, reason: 'a cancelled command must not run');
    });

    testWidgets('running takes a second deliberate tap', (tester) async {
      final c = _ShellClient();
      await _pump(tester, c);
      await _enterAndRun(tester, 'printf hi');
      expect(
        c.commands,
        isEmpty,
        reason: 'the first tap only opens the confirmation',
      );

      await tester.tap(find.byKey(const ValueKey('shell-confirm-run')));
      await tester.pumpAndSettle();
      expect(c.commands, ['printf hi']);
    });
  });

  group('submission', () {
    testWidgets('an invalid command never opens the confirmation', (
      tester,
    ) async {
      final c = _ShellClient();
      await _pump(tester, c);
      await _enterAndRun(tester, '   ');

      expect(find.text('Run this command?'), findsNothing);
      expect(c.commands, isEmpty);
      expect(find.byKey(const ValueKey('shell-error')), findsOneWidget);
    });

    testWidgets('a disabled host explains itself and does not retry', (
      tester,
    ) async {
      final c = _ShellClient(failure: Exception('shell_disabled'));
      await _pump(tester, c);
      await _enterAndRun(tester, 'printf hi');
      await tester.tap(find.byKey(const ValueKey('shell-confirm-run')));
      await tester.pumpAndSettle();

      expect(c.commands, hasLength(1), reason: 'a failure must not resubmit');
      final error = tester.widget<Text>(
        find.byKey(const ValueKey('shell-error')),
      );
      expect(error.data, contains('not enabled'));
    });

    testWidgets('a busy session explains itself', (tester) async {
      final c = _ShellClient(failure: Exception('turn_busy'));
      await _pump(tester, c);
      await _enterAndRun(tester, 'printf hi');
      await tester.tap(find.byKey(const ValueKey('shell-confirm-run')));
      await tester.pumpAndSettle();

      final error = tester.widget<Text>(
        find.byKey(const ValueKey('shell-error')),
      );
      expect(error.data, contains('busy'));
    });

    testWidgets('a rejected command explains itself', (tester) async {
      final c = _ShellClient(failure: Exception('invalid_command'));
      await _pump(tester, c);
      await _enterAndRun(tester, 'printf hi');
      await tester.tap(find.byKey(const ValueKey('shell-confirm-run')));
      await tester.pumpAndSettle();

      final error = tester.widget<Text>(
        find.byKey(const ValueKey('shell-error')),
      );
      expect(error.data, contains('rejected'));
    });

    testWidgets('an unknown failure gets a generic message', (tester) async {
      final c = _ShellClient(failure: Exception('something else'));
      await _pump(tester, c);
      await _enterAndRun(tester, 'printf hi');
      await tester.tap(find.byKey(const ValueKey('shell-confirm-run')));
      await tester.pumpAndSettle();

      final error = tester.widget<Text>(
        find.byKey(const ValueKey('shell-error')),
      );
      expect(error.data, contains('could not be started'));
    });
  });

  testWidgets('the sheet offers no terminal, stdin or environment surface', (
    tester,
  ) async {
    await _pump(tester, _ShellClient());
    for (final label in [
      'Environment',
      'Working directory',
      'Background',
      'Interactive',
      'Terminal',
      'History',
      'stdin',
    ]) {
      expect(find.text(label), findsNothing, reason: label);
      expect(find.byTooltip(label), findsNothing, reason: label);
    }
    // Exactly one input: the command.
    expect(tester.widgetList<TextField>(find.byType(TextField)).length, 1);
  });
}
