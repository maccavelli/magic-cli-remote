part of 'chat_screen.dart';

/// Index of the last item of [kind], or -1.
int _lastIndexOfKind(List<ChatItem> items, ChatItemKind kind) {
  for (var i = items.length - 1; i >= 0; i--) {
    if (items[i].kind == kind) return i;
  }
  return -1;
}

/// Memo of [buildTranscriptRows]: skip O(n) fold when only the last item's
/// text grew (the common streaming path).
List<TranscriptRow> _memoTranscriptRows(
  List<ChatItem> items,
  List<ChatItem>? prevSource,
  List<TranscriptRow>? prevRows,
) {
  if (identical(items, prevSource) && prevRows != null) return prevRows;
  if (prevSource != null &&
      prevRows != null &&
      items.length == prevSource.length &&
      items.isNotEmpty) {
    final n = items.length;
    var prefixSame = true;
    for (var i = 0; i < n - 1; i++) {
      if (!identical(items[i], prevSource[i])) {
        prefixSame = false;
        break;
      }
    }
    if (prefixSame) {
      final oldLast = prevSource[n - 1];
      final newLast = items[n - 1];
      if (oldLast.kind == newLast.kind &&
          newLast.kind != ChatItemKind.tool &&
          prevRows.isNotEmpty &&
          prevRows.last is SingleRow) {
        final rows = List<TranscriptRow>.of(prevRows);
        final last = rows.last as SingleRow;
        rows[rows.length - 1] = SingleRow(newLast, last.index);
        return rows;
      }
    }
  }
  return buildTranscriptRows(items);
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

  /// Row-key value → forward row index, rebuilt only when the row list
  /// changes. [findChildIndexCallback] is invoked per retained element per
  /// rebuild; a linear scan there was O(rows × visible) on every 16ms batch.
  Map<Object, int> _keyIndex = const {};

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
    final rows = _memoTranscriptRows(items, _rowSource, _rowCache);
    if (!identical(rows, _rowCache)) {
      _keyIndex = {
        for (var ri = 0; ri < rows.length; ri++) _rowKeyValue(rows[ri]): ri,
      };
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
            if (row is GroupRow) {
              return RepaintBoundary(
                child: EntranceFade(
                  key: ValueKey('grp-${row.items.first.seq}'),
                  animate: row.items.first.seq >= widget.openSeqFloor,
                  child: _ToolGroupTile(group: row),
                ),
              );
            }
            final single = row as SingleRow;
            final item = single.item;
            return RepaintBoundary(
              child: EntranceFade(
                key: ValueKey(item.seq),
                animate: item.seq >= widget.openSeqFloor,
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

/// Collapsible panel summarising the agent's current plan (ACP `Plan`).
///
/// Replace-semantics: it renders whatever the latest `plan` event left in
/// [SessionTranscript.plan]. Lives above the composer, never in the scrolling
/// transcript, so plan churn does not push chat content around.
