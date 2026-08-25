import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/workspace_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// The read-only workspace viewer (MADR 0112 A5, PLAN P6 step 5).
///
/// The load-bearing assertion is negative: this sheet must offer no way to
/// change anything. A control that looks like it edits files but cannot is
/// worse than no control, because the daemon has no write operation to back it.

class _WorkspaceClient extends McremoteClient {
  _WorkspaceClient({
    this.entries = const {},
    this.files = const {},
    this.searchResult,
    this.throwOn,
  });

  final Map<String, List<WorkspaceEntry>> entries;
  final Map<String, WorkspaceContent> files;
  final WorkspaceSearchResult? searchResult;

  /// An error to throw instead of answering, keyed by operation name.
  final Map<String, Object>? throwOn;

  final calls = <String>[];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<List<WorkspaceEntry>> listWorkspace(
    String sessionId, {
    String path = '',
  }) async {
    calls.add('list:$path');
    final err = throwOn?['list'];
    if (err != null) throw err;
    return entries[path] ?? const [];
  }

  @override
  Future<WorkspaceContent> readWorkspace(String sessionId, String path) async {
    calls.add('read:$path');
    final err = throwOn?['read'];
    if (err != null) throw err;
    return files[path] ?? const WorkspaceContent(path: '', text: '', bytes: 0);
  }

  @override
  Future<WorkspaceSearchResult> searchWorkspace(
    String sessionId, {
    required String kind,
    required String query,
  }) async {
    calls.add('search:$kind:$query');
    final err = throwOn?['search'];
    if (err != null) throw err;
    return searchResult ?? const WorkspaceSearchResult(kind: 'text');
  }
}

Future<_WorkspaceClient> _pumpSheet(
  WidgetTester tester,
  _WorkspaceClient client,
) async {
  tester.view.physicalSize = const Size(700, 1500);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [mcremoteClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(body: WorkspaceSheet(sessionId: 's1')),
      ),
    ),
  );
  await tester.pumpAndSettle();
  return client;
}

void main() {
  group('browsing', () {
    testWidgets('the root listing loads on open', (tester) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [
              WorkspaceEntry(name: 'lib', path: 'lib', dir: true),
              WorkspaceEntry(name: 'go.mod', path: 'go.mod'),
            ],
          },
        ),
      );
      expect(c.calls, contains('list:'));
      expect(find.text('lib'), findsOneWidget);
      expect(find.text('go.mod'), findsOneWidget);
      expect(find.text('Workspace'), findsOneWidget);
    });

    testWidgets('tapping a directory descends and shows the path', (
      tester,
    ) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [WorkspaceEntry(name: 'lib', path: 'lib', dir: true)],
            'lib': const [
              WorkspaceEntry(name: 'main.dart', path: 'lib/main.dart'),
            ],
          },
        ),
      );
      await tester.tap(find.text('lib'));
      await tester.pumpAndSettle();
      expect(c.calls, contains('list:lib'));
      expect(find.text('main.dart'), findsOneWidget);
      expect(find.byKey(const ValueKey('workspace-title')), findsOneWidget);
    });

    testWidgets('back climbs to the parent and is disabled at the root', (
      tester,
    ) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [WorkspaceEntry(name: 'a', path: 'a', dir: true)],
            'a': const [WorkspaceEntry(name: 'b', path: 'a/b', dir: true)],
            'a/b': const [],
          },
        ),
      );
      final back = find.byKey(const ValueKey('workspace-back'));
      expect(
        tester.widget<IconButton>(back).onPressed,
        isNull,
        reason: 'there is nowhere above the session root',
      );

      await tester.tap(find.text('a'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('b'));
      await tester.pumpAndSettle();
      await tester.tap(back);
      await tester.pumpAndSettle();
      expect(c.calls, contains('list:a'));
    });

    testWidgets('an empty directory says so', (tester) async {
      await _pumpSheet(tester, _WorkspaceClient(entries: {'': const []}));
      expect(find.byKey(const ValueKey('workspace-empty')), findsOneWidget);
    });

    test('parentOf climbs one level and stops at the root', () {
      expect(WorkspaceSheetState.parentOf(''), isNull);
      expect(WorkspaceSheetState.parentOf('a'), '');
      expect(WorkspaceSheetState.parentOf('a/b'), 'a');
      expect(WorkspaceSheetState.parentOf('a/b/c.txt'), 'a/b');
    });
  });

  group('reading', () {
    testWidgets('tapping a file shows its text', (tester) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [WorkspaceEntry(name: 'go.mod', path: 'go.mod')],
          },
          files: {
            'go.mod': const WorkspaceContent(
              path: 'go.mod',
              text: 'module example',
              bytes: 14,
            ),
          },
        ),
      );
      await tester.tap(find.text('go.mod'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('workspace-file')), findsOneWidget);
      expect(find.text('module example'), findsOneWidget);
    });

    testWidgets('back returns from a file to the listing', (tester) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [WorkspaceEntry(name: 'go.mod', path: 'go.mod')],
          },
          files: {'go.mod': const WorkspaceContent(path: 'go.mod', text: 'x')},
        ),
      );
      await tester.tap(find.text('go.mod'));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('workspace-back')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('workspace-entries')), findsOneWidget);
    });
  });

  group('searching', () {
    testWidgets('a text search lists matches with their line', (tester) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {'': const []},
          searchResult: const WorkspaceSearchResult(
            kind: 'text',
            cap: 10,
            matches: [WorkspaceMatch(path: 'a.go', line: 12, text: 'hit here')],
          ),
        ),
      );
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        'hit',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(c.calls, contains('search:text:hit'));
      expect(find.text('a.go:12'), findsOneWidget);
      expect(find.text('hit here'), findsOneWidget);
    });

    testWidgets('the file kind is sent when selected', (tester) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {'': const []},
          searchResult: const WorkspaceSearchResult(kind: 'file', cap: 100),
        ),
      );
      await tester.tap(find.text('Files'));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        'main',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(c.calls, contains('search:file:main'));
    });

    testWidgets('a truncated result reports the cap that actually applied', (
      tester,
    ) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {'': const []},
          searchResult: const WorkspaceSearchResult(
            kind: 'text',
            cap: 10,
            truncated: true,
            matches: [WorkspaceMatch(path: 'a.go', line: 1)],
          ),
        ),
      );
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        'x',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(find.textContaining('first 10 matches'), findsOneWidget);
    });

    testWidgets('no matches says so', (tester) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {'': const []},
          searchResult: const WorkspaceSearchResult(kind: 'text', cap: 10),
        ),
      );
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        'zzz',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('workspace-no-matches')),
        findsOneWidget,
      );
    });

    testWidgets('an empty query is not sent', (tester) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(entries: {'': const []}),
      );
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        '   ',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(c.calls.where((x) => x.startsWith('search:')), isEmpty);
    });

    testWidgets('tapping a match opens that file', (tester) async {
      final c = await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {'': const []},
          files: {'a.go': const WorkspaceContent(path: 'a.go', text: 'body')},
          searchResult: const WorkspaceSearchResult(
            kind: 'text',
            cap: 10,
            matches: [WorkspaceMatch(path: 'a.go', line: 1)],
          ),
        ),
      );
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        'x',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      await tester.tap(find.text('a.go:1'));
      await tester.pumpAndSettle();
      expect(c.calls, contains('read:a.go'));
      expect(find.text('body'), findsOneWidget);
    });
  });

  group('refusals', () {
    /// Opens a file whose read fails, and asserts the message a person sees.
    Future<void> expectMessage(
      WidgetTester tester,
      Object error,
      String fragment,
    ) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [WorkspaceEntry(name: 'f', path: 'f')],
          },
          throwOn: {'read': error},
        ),
      );
      await tester.tap(find.text('f'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('workspace-error')), findsOneWidget);
      expect(find.textContaining(fragment), findsOneWidget);
    }

    testWidgets('a path escape is explained', (tester) async {
      await expectMessage(tester, Exception('path_escape'), 'outside');
    });

    testWidgets('a symlink refusal is explained', (tester) async {
      await expectMessage(tester, Exception('path_symlink'), 'symbolic link');
    });

    testWidgets('binary content is explained', (tester) async {
      await expectMessage(tester, Exception('binary_content'), 'not text');
    });

    testWidgets('an oversize file is explained', (tester) async {
      await expectMessage(tester, Exception('result_too_large'), 'too large');
    });

    testWidgets('an invalid path is explained', (tester) async {
      await expectMessage(tester, Exception('invalid_path'), 'not valid');
    });

    testWidgets('an unknown failure gets a generic message', (tester) async {
      await expectMessage(tester, Exception('something else'), 'failed');
    });

    testWidgets('an invalid query is explained', (tester) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {'': const []},
          throwOn: {'search': Exception('invalid_query')},
        ),
      );
      await tester.enterText(
        find.byKey(const ValueKey('workspace-search-field')),
        'x',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(find.textContaining('not valid'), findsOneWidget);
    });

    testWidgets('a failed listing surfaces an error', (tester) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(throwOn: {'list': Exception('workspace_failed')}),
      );
      expect(find.byKey(const ValueKey('workspace-error')), findsOneWidget);
    });
  });

  group('read-only guarantee', () {
    testWidgets('the sheet offers no mutation affordance at all', (
      tester,
    ) async {
      await _pumpSheet(
        tester,
        _WorkspaceClient(
          entries: {
            '': const [WorkspaceEntry(name: 'go.mod', path: 'go.mod')],
          },
          files: {
            'go.mod': const WorkspaceContent(path: 'go.mod', text: 'module x'),
          },
        ),
      );
      await tester.tap(find.text('go.mod'));
      await tester.pumpAndSettle();

      // No editable field anywhere except the search box, and no control whose
      // label suggests writing.
      final fields = find.byType(TextField);
      expect(
        tester.widgetList<TextField>(fields).length,
        1,
        reason: 'only the search box may accept input',
      );
      for (final label in [
        'Save',
        'Edit',
        'Apply',
        'Delete',
        'Rename',
        'New file',
        'Run',
        'Terminal',
      ]) {
        expect(
          find.text(label),
          findsNothing,
          reason: '$label implies a write path the daemon does not expose',
        );
        expect(find.byTooltip(label), findsNothing, reason: label);
      }
      // The file view is selectable text, never an editable field. Selectable
      // text builds an EditableText with readOnly set, so the assertion is on
      // that flag rather than on the widget type.
      expect(find.byType(SelectableText), findsOneWidget);
      final editables = tester.widgetList<EditableText>(
        find.byType(EditableText),
      );
      final writable = editables.where((e) => !e.readOnly).length;
      expect(writable, 1, reason: 'only the search box may be writable');
    });
  });
}
