import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/sessions/codex_threads_screen.dart';

class _FakeCodexThreadsClient with CodexThreadsClient {
  final threads = <CodexThreadMeta>[
    CodexThreadMeta(
      id: 'pinned',
      title: 'Pinned task',
      preview: 'important work',
      source: 'mcremote',
      loaded: true,
      pinned: true,
      sectionId: 'pinned',
      projectId: 'project-1',
    ),
    CodexThreadMeta(
      id: 'work',
      title: 'Needle task',
      preview: 'searchable transcript',
      source: 'vscode',
      sectionId: 'section-1',
      projectId: 'project-1',
    ),
    CodexThreadMeta(
      id: 'archived',
      title: 'Archived task',
      archived: true,
      source: 'cli',
    ),
  ];
  final sections = <CodexThreadSection>[
    const CodexThreadSection(id: 'pinned', name: 'Pinned'),
    const CodexThreadSection(id: 'section-1', name: 'Work'),
    const CodexThreadSection(id: 'empty', name: 'Empty'),
  ];
  final projects = <CodexProject>[
    const CodexProject(id: 'project-1', name: 'Project One', roots: ['/repo']),
  ];
  final calls = <String>[];

  @override
  Future<CodexThreadsSnapshot> load({bool archived = false}) async =>
      CodexThreadsSnapshot(
        threads: threads
            .where((thread) => thread.archived == archived)
            .toList(),
        sections: sections,
        projects: projects,
        source: 'native',
        hasMore: !archived,
      );

  @override
  Future<CodexThreadsPage> search(String term) async => CodexThreadsPage(
    threads: threads
        .where(
          (thread) => '${thread.title} ${thread.preview}'
              .toLowerCase()
              .contains(term.toLowerCase()),
        )
        .toList(),
    source: 'native_search',
  );

  @override
  Future<void> archive(String threadId, bool archived) async {
    calls.add('archive:$threadId:$archived');
  }

  @override
  Future<void> rename(String threadId, String name) async {
    calls.add('rename:$threadId:$name');
  }

  @override
  Future<void> fork(String threadId) async {
    calls.add('fork:$threadId');
  }

  @override
  Future<CodexDeletePreview> previewDelete(String threadId) async =>
      const CodexDeletePreview(
        descendantIds: ['child'],
        hasLoadedDescendants: true,
      );

  @override
  Future<void> delete(String threadId) async {
    calls.add('delete:$threadId');
  }

  @override
  Future<void> moveThread(
    String threadId,
    String? sectionId, {
    String? beforeThreadId,
  }) async {
    calls.add('move:$threadId:${sectionId ?? 'none'}');
  }

  @override
  Future<void> deleteSection(String sectionId) async {
    calls.add('delete-section:$sectionId');
  }
}

void main() {
  test('Codex thread models parse additive metadata and pagination', () {
    final page = CodexThreadsPage.fromJson({
      'threads': [
        {
          'id': 't1',
          'title': 'Task',
          'native_status': 'idle',
          'archived': true,
          'pinned': true,
          'section_id': 'pinned',
          'parent_thread_id': 'parent',
          'forked_from_id': 'fork',
          'source': 'vscode',
          'loaded': true,
          'project_id': 'p1',
        },
      ],
      'source': 'stable_fallback',
      'next_cursor': 'next',
      'backwards_cursor': 'back',
      'truncated': true,
    });
    expect(page.threads.single.loaded, isTrue);
    expect(page.threads.single.parentThreadId, 'parent');
    expect(page.nextCursor, 'next');
    expect(page.source, 'stable_fallback');
    expect(page.truncated, isTrue);
  });

  testWidgets(
    'unified browser filters searches paginates and keeps empty sections',
    (tester) async {
      final fake = _FakeCodexThreadsClient();
      await tester.pumpWidget(
        MaterialApp(
          home: CodexThreadsScreen(client: fake, onResume: (_) async {}),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Pinned task'), findsOneWidget);
      expect(find.text('Needle task'), findsOneWidget);
      expect(find.text('Empty'), findsOneWidget);
      expect(find.text('Project One'), findsOneWidget);
      expect(find.text('native'), findsOneWidget);
      expect(find.byKey(const ValueKey('codex-load-more')), findsOneWidget);

      await tester.enterText(
        find.byKey(const ValueKey('codex-thread-search')),
        'needle',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(find.text('Needle task'), findsOneWidget);
      expect(find.text('Pinned task'), findsNothing);
      expect(find.text('native search'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('codex-archived-filter')));
      await tester.pumpAndSettle();
      expect(find.text('Archived task'), findsOneWidget);
    },
  );

  testWidgets(
    'rename fork archive replay section delete and permanent delete confirm',
    (tester) async {
      final fake = _FakeCodexThreadsClient();
      String? resumed;
      await tester.pumpWidget(
        MaterialApp(
          home: CodexThreadsScreen(
            client: fake,
            onResume: (id) async => resumed = id,
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey('codex-thread-pinned')));
      await tester.pump();
      expect(resumed, 'pinned');

      await tester.tap(find.byKey(const ValueKey('codex-thread-menu-pinned')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete permanently'));
      await tester.pumpAndSettle();
      expect(find.textContaining('1 descendant'), findsOneWidget);
      expect(find.textContaining('descendant is loaded'), findsOneWidget);
      expect(find.text('Delete permanently'), findsWidgets);
      await tester.tap(find.byKey(const ValueKey('codex-confirm-delete')));
      await tester.pumpAndSettle();
      expect(fake.calls, contains('delete:pinned'));

      await tester.tap(find.byKey(const ValueKey('codex-section-menu-empty')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete section'));
      await tester.pumpAndSettle();
      expect(find.textContaining('threads are preserved'), findsOneWidget);
      await tester.tap(
        find.byKey(const ValueKey('codex-confirm-section-delete')),
      );
      await tester.pumpAndSettle();
      expect(fake.calls, contains('delete-section:empty'));
    },
  );
}
