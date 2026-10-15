import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import '../../data/chat/chat_models.dart';

/// Heading for an identified plan's panel (MADR 0192 D4).
String identifiedPlanTitle(IdentifiedPlan plan) => 'Plan: ${plan.id}';

/// Panel for an identified plan that is not a task list: a Markdown plan,
/// collapsed until opened, or a file plan, shown as its name and path with a
/// copy action (MADR 0192 D4).
///
/// Nothing here fetches anything. A file plan's URI points at the daemon's
/// host and is only displayed, and a Markdown plan's images are drawn as their
/// alt text rather than loaded.
class PlanDocumentPanel extends StatelessWidget {
  const PlanDocumentPanel({super.key, required this.plan});

  final IdentifiedPlan plan;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: scheme.surfaceContainerLow,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Divider(height: 1, color: scheme.outlineVariant),
          if (plan.kind == 'file') _file(context) else _markdown(context),
        ],
      ),
    );
  }

  Widget _title(BuildContext context) => Text(
    identifiedPlanTitle(plan),
    maxLines: 1,
    overflow: TextOverflow.ellipsis,
    style: Theme.of(context).textTheme.titleSmall,
  );

  Widget _markdown(BuildContext context) {
    final theme = Theme.of(context);
    return Theme(
      // ExpansionTile draws divider lines above and below when placed in a
      // Column; suppress them so the panel reads as one block.
      data: theme.copyWith(dividerColor: Colors.transparent),
      child: ExpansionTile(
        dense: true,
        tilePadding: const EdgeInsets.symmetric(horizontal: 12),
        leading: const Icon(Icons.description_outlined, size: 20),
        title: _title(context),
        // A plain label: a line of the plan itself would show raw Markdown.
        subtitle: const Text('Markdown plan'),
        children: [
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 220),
            child: SingleChildScrollView(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  MarkdownBody(
                    data: plan.content,
                    selectable: true,
                    styleSheet: MarkdownStyleSheet.fromTheme(theme),
                    // An agent-written image must not make the phone fetch a
                    // URL or read a local file: show what it is instead.
                    imageBuilder: (uri, title, alt) =>
                        Text((alt ?? '').isNotEmpty ? alt! : (title ?? '')),
                    // As in the chat, links are never followed from here.
                    onTapLink: (text, href, title) {},
                  ),
                  if (plan.truncated)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(
                        'The plan was cut short at 32 KiB.',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _file(BuildContext context) {
    final path = filePlanPath(plan.uri);
    final name = path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? path;
    return ListTile(
      dense: true,
      contentPadding: const EdgeInsets.symmetric(horizontal: 12),
      leading: const Icon(Icons.insert_drive_file_outlined, size: 20),
      title: _title(context),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(name, maxLines: 1, overflow: TextOverflow.ellipsis),
          SelectableText(path, maxLines: 2),
        ],
      ),
      trailing: IconButton(
        tooltip: 'Copy path',
        icon: const Icon(Icons.copy, size: 18),
        onPressed: () => Clipboard.setData(ClipboardData(text: path)),
      ),
    );
  }
}

/// The path a file plan's URI names, for display: a `file:` URI's decoded
/// path, or the URI as sent for any other scheme.
String filePlanPath(String uri) {
  final parsed = Uri.tryParse(uri);
  if (parsed == null || parsed.scheme != 'file') return uri;
  try {
    return Uri.decodeComponent(parsed.path);
  } on ArgumentError {
    return parsed.path;
  } on FormatException {
    return parsed.path;
  }
}
