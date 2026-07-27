import 'chat_models.dart';

/// One renderable row of the chat transcript: either a single [ChatItem] or a
/// collapsed group covering one contiguous run of agent actions.
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
  const GroupRow(this.items);

  /// One or more [ChatItemKind.tool] items, in transcript order. Membership is
  /// contiguity alone — any class, any status, including the tool executing
  /// right now (MADR 0042 D1).
  final List<ChatItem> items;

  int get failedCount => items.where((i) => i.toolFailed).length;

  /// The member currently executing, or null when the run is finished. Drives
  /// the collapsed row's live head so a folded group is never opaque about
  /// work in progress.
  ChatItem? get runningItem => items.where((i) => i.toolRunning).firstOrNull;

  /// The class [title] leads with, so the icon and the label always agree.
  /// Commands come first because they are the actions worth naming.
  ToolClass get leadClass {
    var edits = false;
    for (final i in items) {
      switch (i.toolClass) {
        case ToolClass.command:
          return ToolClass.command;
        case ToolClass.fileEdit:
          edits = true;
        case ToolClass.other:
          break;
      }
    }
    return edits ? ToolClass.fileEdit : ToolClass.other;
  }

  /// Collapsed-row label.
  ///
  /// A single-tool run reads as the tool itself — the run is the tool, and its
  /// own name says more than "Used a tool". Beyond that the label summarises
  /// the histogram, leading with commands when the run contains any.
  String get title {
    final n = items.length;
    if (n == 1) {
      final name = (items.first.toolName ?? '').trim();
      return name.isEmpty ? 'Used a tool' : name;
    }
    var commands = 0;
    var edits = 0;
    for (final i in items) {
      switch (i.toolClass) {
        case ToolClass.command:
          commands++;
        case ToolClass.fileEdit:
          edits++;
        case ToolClass.other:
          break;
      }
    }
    if (commands == n) return 'Ran $n commands';
    if (edits == n) return 'Edited $n files';
    if (commands > 0) {
      final rest = n - commands;
      final ran = commands == 1 ? 'Ran 1 command' : 'Ran $commands commands';
      return '$ran +$rest more';
    }
    return 'Used $n tools';
  }
}

/// Fold the transcript into display rows.
///
/// Every maximal run of **consecutive tool items** becomes one [GroupRow], and
/// everything else passes through as a [SingleRow]. Three properties of that
/// rule are load-bearing (MADR 0042 D1); changing any one reintroduces the
/// churn it was written to remove:
///
///  * **Class is not a boundary.** The previous rule only folded runs of the
///    same [ToolClass], and OpenCode alternates constantly (`read`, `bash`,
///    `read`, `edit`), so runs stayed at length 1 and the fold never engaged.
///  * **A live tool does not break the run.** It joins the group immediately,
///    so it never renders as a standalone card that has to collapse into one
///    when it finishes. The count includes the tool executing right now, and
///    [GroupRow.runningItem] keeps it visible while collapsed.
///  * **The threshold is 1, not 2.** A run's identity is its first item's seq
///    from the very first tool, so the row is never re-keyed from
///    `ValueKey(seq)` to `ValueKey('grp-…')` — which is what eliminates the
///    remount rather than merely reducing it.
///
/// Together these make the row count monotonic within a burst: replaying
/// `read, grep, bash, read, edit, bash` holds at one group row throughout,
/// where the old rule oscillated and re-keyed on every fold.
List<TranscriptRow> buildTranscriptRows(List<ChatItem> items) {
  final rows = <TranscriptRow>[];
  final run = <ChatItem>[];

  void flushRun() {
    if (run.isEmpty) return;
    rows.add(GroupRow(List<ChatItem>.unmodifiable(run)));
    run.clear();
  }

  for (var i = 0; i < items.length; i++) {
    final item = items[i];
    if (item.kind != ChatItemKind.tool) {
      flushRun();
      rows.add(SingleRow(item, i));
      continue;
    }
    run.add(item);
  }
  flushRun();
  return rows;
}
