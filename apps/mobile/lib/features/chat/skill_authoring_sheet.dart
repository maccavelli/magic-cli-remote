import 'package:flutter/material.dart';

/// Composes a prompt asking OpenCode to author a project-local skill
/// (MADR 0112 A10, PLAN P7 steps 3 and 4).
///
/// This sheet writes nothing. There is no daemon skill-write API, no raw
/// Markdown editor, no global-home path and no permission bypass: it produces
/// ordinary prompt text that the user can read and edit before sending, and
/// OpenCode's usual write/edit and external-directory permission rules decide
/// what actually happens.

/// Bounds on the composed request. They exist so a malformed name cannot become
/// a path and an unbounded intent cannot become an unbounded prompt.
const int kSkillNameMaxLen = 64;
const int kSkillDescriptionMaxLen = 1024;
const int kSkillIntentMaxLen = 4096;

/// Skill names are lowercase kebab-case. The pattern is also what keeps the
/// name usable as a single directory segment: no dots, no separators, nothing
/// that could climb out of `.opencode/skills/`.
final RegExp kSkillNamePattern = RegExp(r'^[a-z0-9]+(-[a-z0-9]+)*$');

/// Why a request is not composable yet, or null when it is.
String? validateSkillRequest({
  required String name,
  required String description,
  required String intent,
}) {
  final n = name.trim();
  if (n.isEmpty) return 'A skill needs a name.';
  if (n.length > kSkillNameMaxLen) {
    return 'The name must be at most $kSkillNameMaxLen characters.';
  }
  if (!kSkillNamePattern.hasMatch(n)) {
    return 'Use lowercase letters, digits and single hyphens, like '
        '“review-checklist”.';
  }
  final d = description.trim();
  if (d.isEmpty) return 'A skill needs a one-line description.';
  if (d.length > kSkillDescriptionMaxLen) {
    return 'The description must be at most $kSkillDescriptionMaxLen '
        'characters.';
  }
  if (intent.trim().length > kSkillIntentMaxLen) {
    return 'The details must be at most $kSkillIntentMaxLen characters.';
  }
  return null;
}

/// Builds the editable prompt.
///
/// It names the built-in `customize-opencode` skill, pins the project-local
/// path, and asks for preservation of an existing skill unless the change
/// requires otherwise — a phone user cannot see what is already there, so
/// "replace it" would be a destructive default.
String composeSkillPrompt({
  required String name,
  required String description,
  required String intent,
}) {
  final n = name.trim();
  final d = description.trim();
  final extra = intent.trim();
  final buffer = StringBuffer()
    ..writeln(
      'Use your built-in `customize-opencode` skill to create or '
      'update a project-local skill.',
    )
    ..writeln()
    ..writeln(
      '- Write only `.opencode/skills/$n/SKILL.md` in the current '
      'worktree. Do not write anywhere else, and do not use a global or '
      'home-directory location.',
    )
    ..writeln('- Frontmatter `name` must be exactly `$n`.')
    ..writeln('- Frontmatter `description` must be exactly: $d')
    ..writeln(
      '- If that skill already exists, preserve its current content '
      'except where this request requires a change.',
    );
  if (extra.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('What the skill should cover:')
      ..writeln(extra);
  }
  buffer
    ..writeln()
    ..writeln(
      'When you are done, report the exact path you wrote and confirm '
      'the frontmatter validates.',
    );
  return buffer.toString().trimRight();
}

/// Collects a bounded skill request and hands back the composed prompt.
///
/// The caller submits it through the ordinary `session.prompt` path; this sheet
/// never talks to the daemon itself.
class SkillAuthoringSheet extends StatefulWidget {
  const SkillAuthoringSheet({super.key, required this.onCompose});

  /// Called with the composed prompt when the user confirms.
  final void Function(String prompt) onCompose;

  @override
  State<SkillAuthoringSheet> createState() => SkillAuthoringSheetState();
}

class SkillAuthoringSheetState extends State<SkillAuthoringSheet> {
  final _name = TextEditingController();
  final _description = TextEditingController();
  final _intent = TextEditingController();
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _description.dispose();
    _intent.dispose();
    super.dispose();
  }

  void _compose() {
    final problem = validateSkillRequest(
      name: _name.text,
      description: _description.text,
      intent: _intent.text,
    );
    if (problem != null) {
      setState(() => _error = problem);
      return;
    }
    widget.onCompose(
      composeSkillPrompt(
        name: _name.text,
        description: _description.text,
        intent: _intent.text,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          16,
          8,
          16,
          16 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'Create or update a skill',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 4),
            Text(
              'The agent writes the file using its normal tools and asks for '
              'permission as usual. You can edit the message before sending.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('skill-name'),
              controller: _name,
              maxLength: kSkillNameMaxLen,
              decoration: const InputDecoration(
                isDense: true,
                labelText: 'Name',
                helperText: 'lowercase-with-hyphens',
                border: OutlineInputBorder(),
              ),
            ),
            TextField(
              key: const ValueKey('skill-description'),
              controller: _description,
              maxLength: kSkillDescriptionMaxLen,
              maxLines: 2,
              decoration: const InputDecoration(
                isDense: true,
                labelText: 'Description',
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 8),
            TextField(
              key: const ValueKey('skill-intent'),
              controller: _intent,
              maxLength: kSkillIntentMaxLen,
              maxLines: 4,
              decoration: const InputDecoration(
                isDense: true,
                labelText: 'What it should cover (optional)',
                border: OutlineInputBorder(),
              ),
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _error!,
                  key: const ValueKey('skill-error'),
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: scheme.error),
                ),
              ),
            const SizedBox(height: 12),
            FilledButton(
              key: const ValueKey('skill-compose'),
              onPressed: _compose,
              child: const Text('Compose message'),
            ),
          ],
        ),
      ),
    );
  }
}
