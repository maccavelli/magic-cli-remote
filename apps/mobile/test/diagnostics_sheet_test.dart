import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/diagnostics_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Sanitized diagnostics rendering and guarded skill refresh
/// (MADR 0112 A6/A10, PLAN P7 steps 7 and 11).

class _DiagClient extends McremoteClient {
  _DiagClient({this.diagnostics, this.diagnosticsError, this.refreshError});

  final SessionDiagnostics? diagnostics;
  final Object? diagnosticsError;
  final Object? refreshError;

  int refreshes = 0;
  int loads = 0;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionDiagnostics> sessionDiagnostics(String sessionId) async {
    loads++;
    final err = diagnosticsError;
    if (err != null) throw err;
    return diagnostics ?? SessionDiagnostics();
  }

  @override
  Future<void> refreshSkills(String sessionId) async {
    refreshes++;
    final err = refreshError;
    if (err != null) throw err;
  }
}

Future<void> _pumpSheet(
  WidgetTester tester,
  _DiagClient client, {
  bool canRefreshSkills = false,
  void Function()? onAuthorSkill,
}) async {
  tester.view.physicalSize = const Size(700, 1600);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [mcremoteClientProvider.overrideWithValue(client)],
      child: MaterialApp(
        home: Scaffold(
          body: DiagnosticsSheet(
            sessionId: 's1',
            canRefreshSkills: canRefreshSkills,
            onAuthorSkill: onAuthorSkill,
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

SessionDiagnostics fullReport() => SessionDiagnostics(
  branch: 'main',
  skills: const [
    SkillInfo(name: 'customize-opencode', description: 'Author skills'),
  ],
  lsp: const [LspStatus(name: 'gopls', status: 'running')],
  formatters: const [
    FormatterInfo(name: 'gofmt', enabled: true, extensions: 2),
  ],
  mcp: [McpServerStatus(name: 'github', state: 'failed')],
);

void main() {
  testWidgets('every section renders with its count', (tester) async {
    await _pumpSheet(tester, _DiagClient(diagnostics: fullReport()));
    expect(find.text('Skills (1)'), findsOneWidget);
    expect(find.text('Language services (1)'), findsOneWidget);
    expect(find.text('Formatters (1)'), findsOneWidget);
    expect(find.text('MCP servers (1)'), findsOneWidget);
    expect(find.text('customize-opencode'), findsOneWidget);
    expect(find.text('gopls'), findsOneWidget);
    expect(find.textContaining('2 extensions'), findsOneWidget);
    expect(find.text('failed'), findsOneWidget);
    expect(find.text('main'), findsOneWidget);
  });

  testWidgets('empty sections say so rather than vanishing', (tester) async {
    await _pumpSheet(tester, _DiagClient(diagnostics: SessionDiagnostics()));
    expect(find.text('Skills (0)'), findsOneWidget);
    expect(find.text('None reported.'), findsWidgets);
  });

  testWidgets('a failed load surfaces an error', (tester) async {
    await _pumpSheet(tester, _DiagClient(diagnosticsError: Exception('nope')));
    expect(find.byKey(const ValueKey('diagnostics-error')), findsOneWidget);
  });

  testWidgets('the authoring affordance appears only when wired', (
    tester,
  ) async {
    await _pumpSheet(tester, _DiagClient(diagnostics: fullReport()));
    expect(
      find.byKey(const ValueKey('diagnostics-author-skill')),
      findsNothing,
    );

    var opened = 0;
    await _pumpSheet(
      tester,
      _DiagClient(diagnostics: fullReport()),
      onAuthorSkill: () => opened++,
    );
    await tester.tap(find.byKey(const ValueKey('diagnostics-author-skill')));
    await tester.pumpAndSettle();
    expect(opened, 1);
  });

  group('skill refresh', () {
    testWidgets('is hidden unless the session advertises it', (tester) async {
      await _pumpSheet(tester, _DiagClient(diagnostics: fullReport()));
      expect(
        find.byKey(const ValueKey('diagnostics-refresh-skills')),
        findsNothing,
      );
    });

    testWidgets('always confirms before recycling', (tester) async {
      final c = _DiagClient(diagnostics: fullReport());
      await _pumpSheet(tester, c, canRefreshSkills: true);

      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-skills')),
      );
      await tester.pumpAndSettle();
      expect(find.text('Refresh skills?'), findsOneWidget);
      expect(
        find.textContaining('restarts the OpenCode instance'),
        findsOneWidget,
      );
      expect(find.textContaining('are kept'), findsOneWidget);

      // Cancelling must not recycle anything.
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(c.refreshes, 0, reason: 'recycling is never automatic');
    });

    testWidgets('confirming refreshes and reloads the report', (tester) async {
      final c = _DiagClient(diagnostics: fullReport());
      await _pumpSheet(tester, c, canRefreshSkills: true);
      final loadsBefore = c.loads;

      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-skills')),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-confirm')),
      );
      await tester.pumpAndSettle();

      expect(c.refreshes, 1);
      expect(c.loads, greaterThan(loadsBefore));
      expect(find.byKey(const ValueKey('diagnostics-notice')), findsOneWidget);
    });

    testWidgets('a busy instance explains the file is intact', (tester) async {
      final c = _DiagClient(
        diagnostics: fullReport(),
        refreshError: Exception('instance_busy'),
      );
      await _pumpSheet(tester, c, canRefreshSkills: true);
      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-skills')),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-confirm')),
      );
      await tester.pumpAndSettle();

      final error = tester.widget<Text>(
        find.byKey(const ValueKey('diagnostics-error')),
      );
      expect(error.data, contains('busy'));
      expect(
        error.data,
        contains('untouched'),
        reason: 'the user must know retrying costs nothing',
      );
    });

    testWidgets('another failure gets a generic message', (tester) async {
      final c = _DiagClient(
        diagnostics: fullReport(),
        refreshError: Exception('something else'),
      );
      await _pumpSheet(tester, c, canRefreshSkills: true);
      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-skills')),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey('diagnostics-refresh-confirm')),
      );
      await tester.pumpAndSettle();
      final error = tester.widget<Text>(
        find.byKey(const ValueKey('diagnostics-error')),
      );
      expect(error.data, contains('failed'));
    });
  });

  testWidgets('the sheet offers no MCP or skill mutation', (tester) async {
    await _pumpSheet(
      tester,
      _DiagClient(diagnostics: fullReport()),
      canRefreshSkills: true,
    );
    for (final label in [
      'Add server',
      'Connect',
      'Disconnect',
      'Authorize',
      'Delete',
      'Edit',
      'Configure',
    ]) {
      expect(find.text(label), findsNothing, reason: label);
      expect(find.byTooltip(label), findsNothing, reason: label);
    }
  });
}
