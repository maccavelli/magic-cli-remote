part of 'chat_screen.dart';

/// Index of the last item of [kind], or -1.
int _lastIndexOfKind(List<ChatItem> items, ChatItemKind kind) {
  for (var i = items.length - 1; i >= 0; i--) {
    if (items[i].kind == kind) return i;
  }
  return -1;
}

/// How a memo result differs from the previous one, in the only terms the
/// caller's key index cares about.
enum _RowsDelta {
  /// The row key set and their order are unchanged, so `_keyIndex` stays valid
  /// exactly as it is.
  keysUnchanged,

  /// Exactly one row was appended at the end. Every earlier row kept its
  /// forward index, so the index only needs the new key added — never a full
  /// rebuild.
  appended,

  /// Rows were rebuilt; `_keyIndex` must be recomputed.
  rebuilt,
}

/// Memo of [buildTranscriptRows]: skip the O(n) fold when only the last item
/// changed (the common streaming path), and tell the caller precisely how the
/// row keys moved.
///
/// The delta is not cosmetic. `findChildIndexCallback` relies on `_keyIndex`
/// to relocate list elements under `reverse: true`; a key missing from it makes
/// Flutter leave that element on an index which now holds a *different* row,
/// so the element is destroyed and re-inflated instead of moved — a full
/// markdown re-parse for an assistant bubble. This function previously reported
/// "keys unchanged" from the append path while adding a key, which is exactly
/// that bug: re-parse cost grew with every append since the last full rebuild
/// (audit 0041 H1, MADR 0042 D3). Reporting [_RowsDelta.appended] instead of
/// lying keeps the fast path *and* the index correct.
(List<TranscriptRow>, _RowsDelta) _memoTranscriptRows(
  List<ChatItem> items,
  List<ChatItem>? prevSource,
  List<TranscriptRow>? prevRows,
) {
  if (identical(items, prevSource) && prevRows != null) {
    return (prevRows, _RowsDelta.keysUnchanged);
  }

  // Append fast-path: exactly one new item, prefix unchanged. A tool is
  // excluded because it always folds into its adjacent run (MADR 0042 D1),
  // which only buildTranscriptRows can resolve.
  if (prevSource != null &&
      prevRows != null &&
      items.length == prevSource.length + 1 &&
      items.isNotEmpty) {
    var prefixSame = true;
    for (var i = 0; i < prevSource.length; i++) {
      if (!identical(items[i], prevSource[i])) {
        prefixSame = false;
        break;
      }
    }
    if (prefixSame && items.last.kind != ChatItemKind.tool) {
      final newRows = List<TranscriptRow>.of(prevRows);
      newRows.add(SingleRow(items.last, items.length - 1));
      return (newRows, _RowsDelta.appended);
    }
  }

  if (prevSource != null &&
      prevRows != null &&
      items.length == prevSource.length &&
      items.isNotEmpty &&
      prevRows.isNotEmpty) {
    final n = items.length;
    var prefixSame = true;
    for (var i = 0; i < n - 1; i++) {
      if (!identical(items[i], prevSource[i])) {
        prefixSame = false;
        break;
      }
    }
    final oldLast = prevSource[n - 1];
    final newLast = items[n - 1];
    if (prefixSame &&
        oldLast.kind == newLast.kind &&
        oldLast.seq == newLast.seq) {
      final lastRow = prevRows.last;
      if (newLast.kind != ChatItemKind.tool && lastRow is SingleRow) {
        final rows = List<TranscriptRow>.of(prevRows);
        rows[rows.length - 1] = SingleRow(newLast, lastRow.index);
        return (rows, _RowsDelta.keysUnchanged);
      }
      // A content change to the newest tool — status, streamed output — cannot
      // restructure the rows: every tool joins its run whatever its class or
      // status, so the trailing group's membership is fixed and only its last
      // member differs. Excluding tools wholesale here was what forced a full
      // fold on every tool event, measured at two per tool (audit 0041 O4).
      if (newLast.kind == ChatItemKind.tool && lastRow is GroupRow) {
        final members = List<ChatItem>.of(lastRow.items);
        members[members.length - 1] = newLast;
        final rows = List<TranscriptRow>.of(prevRows);
        rows[rows.length - 1] = GroupRow(List<ChatItem>.unmodifiable(members));
        return (rows, _RowsDelta.keysUnchanged);
      }
    }
  }
  return (buildTranscriptRows(items), _RowsDelta.rebuilt);
}

/// Transcript list only — watches [items] + [status] so stream chunks do not
/// rebuild the chat shell (app bar, banners, composer).
class _TranscriptPane extends ConsumerStatefulWidget {
  const _TranscriptPane({
    required this.sessionId,
    required this.scrollController,
    required this.openSeqFloor,
    required this.onUserAction,
  });

  final String sessionId;
  final ScrollController scrollController;
  final int openSeqFloor;
  final void Function(String text) onUserAction;

  @override
  ConsumerState<_TranscriptPane> createState() => _TranscriptPaneState();
}

class _TranscriptPaneState extends ConsumerState<_TranscriptPane> {
  List<ChatItem>? _rowSource;
  List<TranscriptRow>? _rowCache;

  /// Row-key value → forward row index. [findChildIndexCallback] is invoked per
  /// retained element per rebuild; a linear scan there was O(rows × visible) on
  /// every 16ms batch.
  ///
  /// Maintained incrementally: an append only adds its own key, since appending
  /// never shifts an earlier row's *forward* index (the callback converts to
  /// the reversed child index using the current `rows.length`). Mutable and
  /// mutated in place — copying it per append would put back the O(rows) cost
  /// the fast path exists to avoid.
  Map<Object, int> _keyIndex = <Object, int>{};

  /// Highest item seq this pane has already built. Combined with the
  /// single-live-append rule: multi-item history/resync jumps raise the
  /// ceiling without animating; only a one-row live append may fade. Also
  /// covers tool-fold remounts (group first-seq key change) so already-seen
  /// content does not re-fade.
  int _builtSeqCeiling = -1;

  /// Expansion of tool groups, keyed by the group's first item seq (stable
  /// across membership growth). Lives here so a remounted [_ToolGroupTile]
  /// reopens instead of snapping closed.
  final Map<int, bool> _groupExpanded = {};

  static Object _rowKeyValue(TranscriptRow row) => switch (row) {
    SingleRow(:final item) => item.seq,
    GroupRow(:final items) => 'grp-${items.first.seq}',
  };

  @override
  Widget build(BuildContext context) {
    final items = ref.watch(
      sessionTranscriptProvider(widget.sessionId).select((t) => t.items),
    );
    final status = ref.watch(
      sessionTranscriptProvider(widget.sessionId).select((t) => t.status),
    );
    final (rows, delta) = _memoTranscriptRows(items, _rowSource, _rowCache);
    switch (delta) {
      case _RowsDelta.keysUnchanged:
        break;
      case _RowsDelta.appended:
        _keyIndex[_rowKeyValue(rows.last)] = rows.length - 1;
      case _RowsDelta.rebuilt:
        _keyIndex = {
          for (var ri = 0; ri < rows.length; ri++) _rowKeyValue(rows[ri]): ri,
        };
        // Expansion is keyed by a group's first seq, and a full rebuild is the
        // only point at which groups can disappear (FIFO trim at the 800-item
        // cap, or a resync replacing the transcript). Drop entries for groups
        // that no longer exist so the map cannot grow for the pane's lifetime.
        if (_groupExpanded.isNotEmpty) {
          final live = <int>{
            for (final row in rows)
              if (row is GroupRow) row.items.first.seq,
          };
          _groupExpanded.removeWhere((seq, _) => !live.contains(seq));
        }
    }
    // Live appends grow the list by exactly one item. History / cache hydrate
    // / resync land as multi-item jumps (or full replaces) and must never
    // run EntranceFade — the animation starts in initState, so a later
    // openSeqFloor bump cannot undo a first-frame animate:true.
    final prevLen = _rowSource?.length ?? 0;
    final prevCeiling = _builtSeqCeiling;
    final newMax = items.isEmpty ? prevCeiling : items.last.seq;
    final singleLiveAppend = items.length == prevLen + 1;
    final animateAbove = singleLiveAppend ? prevCeiling : newMax;
    if (newMax > _builtSeqCeiling) {
      _builtSeqCeiling = newMax;
    }
    _rowSource = items;
    _rowCache = rows;

    return LayoutBuilder(
      builder: (ctx, constraints) {
        final maxUserW = constraints.maxWidth * 0.85;
        final maxAssistantW = constraints.maxWidth * 0.9;
        final lastAssistantIdx = _lastIndexOfKind(
          items,
          ChatItemKind.assistant,
        );
        final lastIdx = items.length - 1;
        final running = status == 'running';

        return ListView.builder(
          controller: widget.scrollController,
          reverse: true,
          // ~1.5 screens of pre-built rows: markdown-heavy bubbles are built
          // before a fling reaches them instead of hitching mid-scroll.
          scrollCacheExtent: const ScrollCacheExtent.pixels(900),
          // Rows already carry their own RepaintBoundary; the framework's
          // per-child boundary would just double-wrap them.
          addRepaintBoundaries: false,
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          // reverse:true inverts padding: top becomes the visual bottom inset.
          padding: const EdgeInsets.fromLTRB(12, 28, 12, 12),
          itemCount: rows.length,
          // Preserve Element state when rows reorder under reverse+append.
          findChildIndexCallback: (Key key) {
            if (key is! ValueKey) return null;
            final ri = _keyIndex[key.value];
            return ri == null ? null : rows.length - 1 - ri;
          },
          itemBuilder: (ctx, i) {
            final row = rows[rows.length - 1 - i];
            // Keys MUST sit on the outermost widget: the sliver delegate
            // reads `child.key` off what itemBuilder returns, so a key one
            // level down leaves the delegate keyless — findChildIndexCallback
            // never runs and every append remounts (and re-parses) all rows.
            if (row is GroupRow) {
              final firstSeq = row.items.first.seq;
              return RepaintBoundary(
                key: ValueKey('grp-$firstSeq'),
                child: EntranceFade(
                  animate:
                      firstSeq >= widget.openSeqFloor &&
                      firstSeq > animateAbove,
                  child: _ToolGroupTile(
                    group: row,
                    expanded: _groupExpanded[firstSeq] ?? false,
                    onExpansionChanged: (v) {
                      setState(() => _groupExpanded[firstSeq] = v);
                    },
                  ),
                ),
              );
            }
            final single = row as SingleRow;
            final item = single.item;
            return RepaintBoundary(
              key: ValueKey(item.seq),
              child: EntranceFade(
                animate:
                    item.seq >= widget.openSeqFloor && item.seq > animateAbove,
                child: _ChatBubble(
                  item: item,
                  maxUserWidth: maxUserW,
                  maxAssistantWidth: maxAssistantW,
                  agentRunning: running && single.index == lastIdx,
                  streamingText:
                      running &&
                      item.kind == ChatItemKind.assistant &&
                      single.index == lastAssistantIdx,
                  onUserAction: widget.onUserAction,
                ),
              ),
            );
          },
        );
      },
    );
  }
}
