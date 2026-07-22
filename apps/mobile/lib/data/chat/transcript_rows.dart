import 'chat_models.dart';

/// One renderable row of the chat transcript: either a single [ChatItem] or a
/// collapsed group of consecutive, finished agent actions of the same class.
sealed class TranscriptRow {
  const TranscriptRow();
}

class SingleRow extends TranscriptRow {
  const SingleRow(this.item, this.index);

  final ChatItem item;

  /// Index into the source items list (drives streaming/last-item logic).
  final int index;
}

class GroupRow extends TranscriptRow {
  const GroupRow(this.toolClass, this.items);

  final ToolClass toolClass;

  /// At least two items, in transcript order, all [ChatItemKind.tool] with the
  /// same [ToolClass] and none still running.
  final List<ChatItem> items;

  int get failedCount => items.where((i) => i.toolFailed).length;

  /// Collapsed-row label: "Ran 5 commands", "Edited 3 files", "Used 4 tools".
  String get title {
    final n = items.length;
    return switch (toolClass) {
      ToolClass.command => n == 1 ? 'Ran a command' : 'Ran $n commands',
      ToolClass.fileEdit => n == 1 ? 'Edited a file' : 'Edited $n files',
      ToolClass.other => n == 1 ? 'Used a tool' : 'Used $n tools',
    };
  }
}

/// Fold the transcript into display rows.
///
/// Consecutive *finished* tool items of the same [ToolClass] collapse into one
/// [GroupRow] once there are at least two of them. A tool that is still
/// running (or pending) always renders as its own row so its live spinner
/// stays visible; when it completes it joins the adjacent group on the next
/// rebuild. Everything else passes through as [SingleRow]s.
List<TranscriptRow> buildTranscriptRows(List<ChatItem> items) {
  final rows = <TranscriptRow>[];
  final run = <ChatItem>[];
  var runStart = -1;
  ToolClass? runClass;

  void flushRun() {
    if (run.isEmpty) return;
    if (run.length >= 2) {
      rows.add(GroupRow(runClass!, List<ChatItem>.unmodifiable(run)));
    } else {
      rows.add(SingleRow(run.first, runStart));
    }
    run.clear();
    runClass = null;
    runStart = -1;
  }

  for (var i = 0; i < items.length; i++) {
    final item = items[i];
    final groupable = item.kind == ChatItemKind.tool && !item.toolRunning;
    if (!groupable) {
      flushRun();
      rows.add(SingleRow(item, i));
      continue;
    }
    final cls = item.toolClass;
    if (runClass != null && cls != runClass) flushRun();
    if (run.isEmpty) {
      runClass = cls;
      runStart = i;
    }
    run.add(item);
  }
  flushRun();
  return rows;
}
