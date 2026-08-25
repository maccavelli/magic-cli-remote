import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';

/// Read-only viewer for the active session's working directory (MADR 0112 A5).
///
/// There is deliberately no edit, save, apply, rename, delete or terminal
/// affordance anywhere in this sheet. The daemon exposes no write operation on
/// this surface, so offering one here could only ever fail — and a control that
/// looks like it edits files is worse than no control at all.
class WorkspaceSheet extends ConsumerStatefulWidget {
  const WorkspaceSheet({super.key, required this.sessionId});

  final String sessionId;

  @override
  ConsumerState<WorkspaceSheet> createState() => WorkspaceSheetState();
}

/// Which pane the sheet is showing.
enum WorkspaceView { browse, file, search }

class WorkspaceSheetState extends ConsumerState<WorkspaceSheet> {
  final _search = TextEditingController();

  WorkspaceView _view = WorkspaceView.browse;
  String _dir = '';
  List<WorkspaceEntry> _entries = const [];
  WorkspaceContent? _file;
  WorkspaceSearchResult? _results;
  String _searchKind = 'text';
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    unawaitedLoad(_openDir(''));
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  /// Fire-and-forget for initState, where awaiting is not possible.
  static void unawaitedLoad(Future<void> f) {
    f.ignore();
  }

  McremoteClient get _client => ref.read(mcremoteClientProvider);

  /// Runs one workspace request, holding a single busy latch so a slow listing
  /// cannot be raced by a second tap.
  Future<void> _run(Future<void> Function() body) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await body();
    } catch (e) {
      if (mounted) setState(() => _error = _friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Turns a daemon refusal into something a person can act on. The daemon
  /// deliberately sends a code rather than a host path, so the phone supplies
  /// the wording.
  static String _friendly(Object e) {
    final text = e.toString();
    if (text.contains('path_escape')) {
      return 'That path is outside this session’s directory.';
    }
    if (text.contains('path_symlink')) {
      return 'That path goes through a symbolic link, which is not followed.';
    }
    if (text.contains('binary_content')) {
      return 'That file is not text, so it cannot be shown here.';
    }
    if (text.contains('result_too_large')) {
      return 'That file is too large to show.';
    }
    if (text.contains('invalid_path')) {
      return 'That path is not valid.';
    }
    if (text.contains('invalid_query')) {
      return 'That search is not valid.';
    }
    return 'The workspace request failed.';
  }

  Future<void> _openDir(String path) => _run(() async {
    final entries = await _client.listWorkspace(widget.sessionId, path: path);
    if (!mounted) return;
    setState(() {
      _dir = path;
      _entries = entries;
      _view = WorkspaceView.browse;
      _file = null;
    });
  });

  Future<void> _openFile(String path) => _run(() async {
    final content = await _client.readWorkspace(widget.sessionId, path);
    if (!mounted) return;
    setState(() {
      _file = content;
      _view = WorkspaceView.file;
    });
  });

  Future<void> _runSearch() {
    final q = _search.text.trim();
    if (q.isEmpty) return Future<void>.value();
    return _run(() async {
      final res = await _client.searchWorkspace(
        widget.sessionId,
        kind: _searchKind,
        query: q,
      );
      if (!mounted) return;
      setState(() {
        _results = res;
        _view = WorkspaceView.search;
      });
    });
  }

  /// The parent directory of [path], or null at the root.
  static String? parentOf(String path) {
    if (path.isEmpty) return null;
    final i = path.lastIndexOf('/');
    return i <= 0 ? '' : path.substring(0, i);
  }

  void _back() {
    if (_view != WorkspaceView.browse) {
      setState(() => _view = WorkspaceView.browse);
      return;
    }
    final up = parentOf(_dir);
    if (up != null) unawaitedLoad(_openDir(up));
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                IconButton(
                  key: const ValueKey('workspace-back'),
                  tooltip: 'Back',
                  icon: const Icon(Icons.arrow_back),
                  onPressed: (_dir.isEmpty && _view == WorkspaceView.browse)
                      ? null
                      : _back,
                ),
                Expanded(
                  child: Text(
                    _dir.isEmpty ? 'Workspace' : _dir,
                    key: const ValueKey('workspace-title'),
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
                if (_busy)
                  const Padding(
                    padding: EdgeInsets.only(left: 8),
                    child: SizedBox(
                      key: ValueKey('workspace-busy'),
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    key: const ValueKey('workspace-search-field'),
                    controller: _search,
                    decoration: const InputDecoration(
                      isDense: true,
                      hintText: 'Search',
                      border: OutlineInputBorder(),
                    ),
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) => _runSearch(),
                  ),
                ),
                const SizedBox(width: 8),
                SegmentedButton<String>(
                  key: const ValueKey('workspace-search-kind'),
                  segments: const [
                    ButtonSegment(value: 'text', label: Text('Text')),
                    ButtonSegment(value: 'file', label: Text('Files')),
                  ],
                  selected: {_searchKind},
                  showSelectedIcon: false,
                  onSelectionChanged: (s) =>
                      setState(() => _searchKind = s.first),
                ),
              ],
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _error!,
                  key: const ValueKey('workspace-error'),
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: scheme.error),
                ),
              ),
            const SizedBox(height: 8),
            Flexible(child: _body(context)),
          ],
        ),
      ),
    );
  }

  Widget _body(BuildContext context) {
    switch (_view) {
      case WorkspaceView.file:
        final f = _file;
        if (f == null) return const SizedBox.shrink();
        return SingleChildScrollView(
          key: const ValueKey('workspace-file'),
          child: SelectableText(
            f.text,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
          ),
        );
      case WorkspaceView.search:
        final r = _results;
        if (r == null) return const SizedBox.shrink();
        if (r.matches.isEmpty) {
          return const Text(
            'No matches.',
            key: ValueKey('workspace-no-matches'),
          );
        }
        return ListView.builder(
          key: const ValueKey('workspace-results'),
          shrinkWrap: true,
          itemCount: r.matches.length + (r.truncated ? 1 : 0),
          itemBuilder: (ctx, i) {
            if (i == r.matches.length) {
              // The cap that actually applied, not the row budget: text search
              // is limited upstream and saying otherwise would overstate it.
              return ListTile(
                key: const ValueKey('workspace-truncated'),
                dense: true,
                leading: const Icon(Icons.more_horiz, size: 18),
                title: Text('Showing the first ${r.cap} matches'),
              );
            }
            final m = r.matches[i];
            return ListTile(
              dense: true,
              leading: const Icon(Icons.search, size: 18),
              title: Text(m.line > 0 ? '${m.path}:${m.line}' : m.path),
              subtitle: m.text.isEmpty ? null : Text(m.text, maxLines: 2),
              onTap: () => _openFile(m.path),
            );
          },
        );
      case WorkspaceView.browse:
        if (_entries.isEmpty) {
          return const Text(
            'This directory is empty.',
            key: ValueKey('workspace-empty'),
          );
        }
        return ListView.builder(
          key: const ValueKey('workspace-entries'),
          shrinkWrap: true,
          itemCount: _entries.length,
          itemBuilder: (ctx, i) {
            final e = _entries[i];
            return ListTile(
              dense: true,
              leading: Icon(
                e.dir ? Icons.folder_outlined : Icons.description_outlined,
                size: 18,
              ),
              title: Text(e.name.isEmpty ? e.path : e.name),
              enabled: !e.ignored || e.dir,
              onTap: () => e.dir ? _openDir(e.path) : _openFile(e.path),
            );
          },
        );
    }
  }
}
