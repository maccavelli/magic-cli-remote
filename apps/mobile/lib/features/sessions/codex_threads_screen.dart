import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/codex_threads_client.dart';
import '../../data/protocol/models.dart';

export '../../data/codex_threads_client.dart';

class CodexThreadsScreen extends StatefulWidget {
  const CodexThreadsScreen({
    required this.client,
    required this.onResume,
    super.key,
  });

  final CodexThreadsClient client;
  final Future<void> Function(String threadId) onResume;

  @override
  State<CodexThreadsScreen> createState() => _CodexThreadsScreenState();
}

class _CodexThreadsScreenState extends State<CodexThreadsScreen> {
  CodexThreadsSnapshot _snapshot = const CodexThreadsSnapshot();
  bool _loading = true;
  bool _archived = false;
  String _projectFilter = '';
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    if (mounted) setState(() => _loading = true);
    try {
      final snapshot = await widget.client.load(archived: _archived);
      if (!mounted) return;
      setState(() {
        _snapshot = snapshot;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _search(String term) async {
    term = term.trim();
    if (term.isEmpty) return _load();
    setState(() => _loading = true);
    try {
      final page = await widget.client.search(term);
      if (!mounted) return;
      setState(() {
        _snapshot = CodexThreadsSnapshot(
          threads: page.threads,
          sections: _snapshot.sections,
          projects: _snapshot.projects,
          source: page.source,
          hasMore: page.nextCursor.isNotEmpty,
          nextCursor: page.nextCursor,
        );
        _error = null;
      });
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _loadMore() async {
    if (_snapshot.nextCursor.isEmpty) return;
    final page = await widget.client.loadMore(
      _snapshot.nextCursor,
      archived: _archived,
    );
    if (!mounted) return;
    final byId = {for (final thread in _snapshot.threads) thread.id: thread};
    for (final thread in page.threads) {
      byId[thread.id] = thread;
    }
    setState(() {
      _snapshot = CodexThreadsSnapshot(
        threads: byId.values.toList(),
        sections: _snapshot.sections,
        projects: _snapshot.projects,
        source: page.source,
        hasMore: page.nextCursor.isNotEmpty,
        nextCursor: page.nextCursor,
      );
    });
  }

  Future<void> _editSection(CodexThreadSection? section) async {
    var name = section?.name ?? '';
    var icon = section?.icon ?? '';
    var color = section?.color ?? '';
    final saved = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(section == null ? 'Create section' : 'Edit section'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              initialValue: name,
              decoration: const InputDecoration(labelText: 'Name'),
              onChanged: (value) => name = value,
            ),
            TextFormField(
              initialValue: icon,
              decoration: const InputDecoration(labelText: 'Icon'),
              onChanged: (value) => icon = value,
            ),
            TextFormField(
              initialValue: color,
              decoration: const InputDecoration(labelText: 'Color'),
              onChanged: (value) => color = value,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (saved != true || name.trim().isEmpty) return;
    if (section == null) {
      await widget.client.createSection(name.trim(), icon: icon, color: color);
    } else {
      await widget.client.updateSection(
        section.id,
        name.trim(),
        icon: icon,
        color: color,
      );
    }
    await _load();
  }

  Future<void> _createProject() async {
    var name = '';
    var roots = '';
    final saved = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Create project'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              decoration: const InputDecoration(labelText: 'Name'),
              onChanged: (value) => name = value,
            ),
            TextField(
              decoration: const InputDecoration(
                labelText: 'Absolute roots (one per line)',
              ),
              minLines: 2,
              maxLines: 5,
              onChanged: (value) => roots = value,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Create'),
          ),
        ],
      ),
    );
    if (saved != true || name.trim().isEmpty) return;
    await widget.client.createProject(
      name.trim(),
      roots
          .split('\n')
          .map((root) => root.trim())
          .where((root) => root.isNotEmpty)
          .toList(),
    );
    await _load();
  }

  Future<void> _importProject() async {
    var name = '';
    var roots = '';
    var threadIds = '';
    final saved = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Import project'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              decoration: const InputDecoration(labelText: 'Name'),
              onChanged: (value) => name = value,
            ),
            TextField(
              decoration: const InputDecoration(
                labelText: 'Absolute roots (one per line)',
              ),
              onChanged: (value) => roots = value,
            ),
            TextField(
              decoration: const InputDecoration(
                labelText: 'Native thread IDs (one per line)',
              ),
              onChanged: (value) => threadIds = value,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Import'),
          ),
        ],
      ),
    );
    if (saved != true || name.trim().isEmpty) return;
    List<String> lines(String value) => value
        .split('\n')
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty)
        .toList();
    await widget.client.importProject(
      name.trim(),
      lines(roots),
      lines(threadIds),
    );
    await _load();
  }

  Future<void> _editProject(CodexProject project) async {
    var name = project.name;
    var roots = project.roots.join('\n');
    final saved = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Edit project'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              initialValue: name,
              decoration: const InputDecoration(labelText: 'Name'),
              onChanged: (value) => name = value,
            ),
            TextFormField(
              initialValue: roots,
              decoration: const InputDecoration(
                labelText: 'Absolute roots (one per line)',
              ),
              onChanged: (value) => roots = value,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (saved != true || name.trim().isEmpty) return;
    await widget.client.updateProject(
      project.id,
      name.trim(),
      roots
          .split('\n')
          .map((root) => root.trim())
          .where((root) => root.isNotEmpty)
          .toList(),
    );
    await _load();
  }

  Future<void> _projectAction(String action, CodexProject project) async {
    switch (action) {
      case 'edit':
        await _editProject(project);
      case 'move_first':
        final others = _snapshot.projects
            .where((candidate) => candidate.id != project.id)
            .toList();
        await widget.client.moveProject(
          project.id,
          others.isEmpty ? null : others.first.id,
        );
        await _load();
      case 'delete':
        await _deleteProject(project);
    }
  }

  Future<void> _deleteProject(CodexProject project) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Delete ${project.name}?'),
        content: const Text(
          'Member threads become unassigned. Threads, filesystem roots, and '
          'root contents are preserved.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete project'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await widget.client.deleteProject(project.id);
    await _load();
  }

  Future<void> _deleteThread(CodexThreadMeta thread) async {
    final preview = await widget.client.previewDelete(thread.id);
    if (!mounted) return;
    final count = preview.descendantIds.length;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete permanently?'),
        content: Text(
          'This permanently deletes this native Codex thread and affects '
          '$count descendant${count == 1 ? '' : 's'}. '
          '${preview.hasLoadedDescendants ? 'At least one descendant is loaded. ' : ''}'
          'This cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const ValueKey('codex-confirm-delete'),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete permanently'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await widget.client.delete(thread.id);
    await _load();
  }

  Future<void> _deleteSection(CodexThreadSection section) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Delete ${section.name}?'),
        content: const Text(
          'Member threads are preserved and moved out of this section. '
          'No thread or workspace file is deleted.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const ValueKey('codex-confirm-section-delete'),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete section'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await widget.client.deleteSection(section.id);
    await _load();
  }

  Future<void> _renameThread(CodexThreadMeta thread) async {
    var value = thread.title;
    final name = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Rename thread'),
        content: TextFormField(
          initialValue: value,
          autofocus: true,
          onChanged: (next) => value = next,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, value.trim()),
            child: const Text('Rename'),
          ),
        ],
      ),
    );
    if (name == null || name.isEmpty) return;
    await widget.client.rename(thread.id, name);
    await _load();
  }

  Future<void> _threadAction(String action, CodexThreadMeta thread) async {
    if (action.startsWith('section:')) {
      await widget.client.moveThread(thread.id, action.substring(8));
      await _load();
      return;
    }
    if (action.startsWith('project:')) {
      final projectId = action.substring(8);
      await widget.client.assignProject(
        thread.id,
        projectId.isEmpty ? null : projectId,
      );
      await _load();
      return;
    }
    switch (action) {
      case 'rename':
        await _renameThread(thread);
      case 'fork':
        await widget.client.fork(thread.id);
        await _load();
      case 'archive':
        await widget.client.archive(thread.id, !thread.archived);
        await _load();
      case 'unsection':
        await widget.client.moveThread(thread.id, null);
        await _load();
      case 'pin':
        await widget.client.moveThread(
          thread.id,
          thread.pinned ? null : 'pinned',
        );
        await _load();
      case 'delete':
        await _deleteThread(thread);
    }
  }

  List<CodexThreadMeta> _threadsFor(CodexThreadSection section) => _snapshot
      .threads
      .where((thread) => thread.sectionId == section.id)
      .toList();

  Widget _threadTile(CodexThreadMeta thread) => ListTile(
    key: ValueKey('codex-thread-${thread.id}'),
    onTap: () => widget.onResume(thread.id),
    leading: Icon(thread.pinned ? Icons.push_pin : Icons.chat_bubble_outline),
    title: Text(
      thread.displayName,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
    ),
    subtitle: Text(
      [
        if (thread.loaded) 'loaded',
        if (thread.source.isNotEmpty) thread.source,
        if (thread.preview.isNotEmpty) thread.preview,
      ].join(' · '),
      maxLines: 2,
      overflow: TextOverflow.ellipsis,
    ),
    trailing: PopupMenuButton<String>(
      key: ValueKey('codex-thread-menu-${thread.id}'),
      onSelected: (action) => unawaited(_threadAction(action, thread)),
      itemBuilder: (_) => [
        const PopupMenuItem(value: 'rename', child: Text('Rename')),
        const PopupMenuItem(value: 'fork', child: Text('Fork')),
        PopupMenuItem(
          value: 'archive',
          child: Text(thread.archived ? 'Unarchive' : 'Archive'),
        ),
        if (thread.sectionId.isNotEmpty)
          const PopupMenuItem(
            value: 'unsection',
            child: Text('Move out of section'),
          ),
        PopupMenuItem(
          value: 'pin',
          child: Text(thread.pinned ? 'Unpin' : 'Pin'),
        ),
        for (final section in _snapshot.sections)
          if (section.id != thread.sectionId)
            PopupMenuItem(
              value: 'section:${section.id}',
              child: Text('Move to ${section.name}'),
            ),
        for (final project in _snapshot.projects)
          if (project.id != thread.projectId)
            PopupMenuItem(
              value: 'project:${project.id}',
              child: Text('Assign to ${project.name}'),
            ),
        if (thread.projectId.isNotEmpty)
          const PopupMenuItem(
            value: 'project:',
            child: Text('Remove from project'),
          ),
        const PopupMenuDivider(),
        const PopupMenuItem(value: 'delete', child: Text('Delete permanently')),
      ],
    ),
  );

  Widget _orderedSection(
    CodexThreadSection section,
    List<CodexThreadMeta> visibleThreads,
  ) {
    final threads = _threadsFor(
      section,
    ).where(visibleThreads.contains).toList();
    return ReorderableListView(
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      onReorderItem: (oldIndex, newIndex) {
        final moved = threads.removeAt(oldIndex);
        final before = newIndex < threads.length ? threads[newIndex].id : null;
        unawaited(
          widget.client
              .moveThread(moved.id, section.id, beforeThreadId: before)
              .then((_) => _load()),
        );
      },
      children: [for (final thread in threads) _threadTile(thread)],
    );
  }

  @override
  Widget build(BuildContext context) {
    final visibleThreads = _snapshot.threads
        .where(
          (thread) =>
              _projectFilter.isEmpty || thread.projectId == _projectFilter,
        )
        .toList();
    final assigned = visibleThreads
        .where((thread) => thread.sectionId.isNotEmpty)
        .map((thread) => thread.id)
        .toSet();
    final unsectioned = visibleThreads
        .where((thread) => !assigned.contains(thread.id))
        .toList();
    return Scaffold(
      appBar: AppBar(
        title: const Text('Codex threads'),
        actions: [
          IconButton(
            key: const ValueKey('codex-create-section'),
            tooltip: 'Create section',
            onPressed: () => unawaited(_editSection(null)),
            icon: const Icon(Icons.create_new_folder_outlined),
          ),
          IconButton(
            key: const ValueKey('codex-create-project'),
            tooltip: 'Create project',
            onPressed: () => unawaited(_createProject()),
            icon: const Icon(Icons.workspaces_outline),
          ),
          IconButton(
            key: const ValueKey('codex-import-project'),
            tooltip: 'Import project',
            onPressed: () => unawaited(_importProject()),
            icon: const Icon(Icons.drive_folder_upload_outlined),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.all(12),
          children: [
            TextField(
              key: const ValueKey('codex-thread-search'),
              textInputAction: TextInputAction.search,
              onSubmitted: _search,
              decoration: const InputDecoration(
                labelText: 'Search native threads',
                prefixIcon: Icon(Icons.search),
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                FilterChip(
                  key: const ValueKey('codex-archived-filter'),
                  label: const Text('Archived'),
                  selected: _archived,
                  onSelected: (value) {
                    setState(() => _archived = value);
                    unawaited(_load());
                  },
                ),
                const SizedBox(width: 8),
                if (_snapshot.source.isNotEmpty)
                  Chip(label: Text(_snapshot.source.replaceAll('_', ' '))),
              ],
            ),
            if (_snapshot.projects.isNotEmpty) ...[
              const Text(
                'Projects',
                style: TextStyle(fontWeight: FontWeight.bold),
              ),
              Wrap(
                spacing: 8,
                children: [
                  ChoiceChip(
                    label: const Text('All projects'),
                    selected: _projectFilter.isEmpty,
                    onSelected: (_) => setState(() => _projectFilter = ''),
                  ),
                  for (final project in _snapshot.projects) ...[
                    ChoiceChip(
                      label: Text(project.name),
                      selected: _projectFilter == project.id,
                      onSelected: (_) =>
                          setState(() => _projectFilter = project.id),
                    ),
                    PopupMenuButton<String>(
                      tooltip: 'Manage ${project.name}',
                      onSelected: (action) =>
                          unawaited(_projectAction(action, project)),
                      itemBuilder: (_) => const [
                        PopupMenuItem(
                          value: 'edit',
                          child: Text('Edit project'),
                        ),
                        PopupMenuItem(
                          value: 'move_first',
                          child: Text('Move project first'),
                        ),
                        PopupMenuItem(
                          value: 'delete',
                          child: Text('Delete project'),
                        ),
                      ],
                    ),
                  ],
                ],
              ),
            ],
            if (_loading) const LinearProgressIndicator(),
            if (_error != null)
              Text(
                _error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            for (final section in _snapshot.sections) ...[
              ListTile(
                dense: true,
                title: Text(section.name),
                trailing: PopupMenuButton<String>(
                  key: ValueKey('codex-section-menu-${section.id}'),
                  onSelected: (action) {
                    if (action == 'edit') {
                      unawaited(_editSection(section));
                    } else if (action == 'delete') {
                      unawaited(_deleteSection(section));
                    }
                  },
                  itemBuilder: (_) => const [
                    PopupMenuItem(value: 'edit', child: Text('Edit section')),
                    PopupMenuItem(
                      value: 'delete',
                      child: Text('Delete section'),
                    ),
                  ],
                ),
              ),
              _orderedSection(section, visibleThreads),
            ],
            if (unsectioned.isNotEmpty) ...[
              const ListTile(dense: true, title: Text('Unsectioned')),
              for (final thread in unsectioned) _threadTile(thread),
            ],
            if (_snapshot.hasMore)
              TextButton(
                key: const ValueKey('codex-load-more'),
                onPressed: _loadMore,
                child: const Text('Load more'),
              ),
            if (!_loading && _snapshot.threads.isEmpty)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Center(child: Text('No Codex threads')),
              ),
          ],
        ),
      ),
    );
  }
}
