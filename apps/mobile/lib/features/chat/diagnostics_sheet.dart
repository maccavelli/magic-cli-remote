import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';

/// Sanitized engine diagnostics (MADR 0112 A6, PLAN P7 step 11).
///
/// Everything rendered here is metadata the daemon already stripped: no skill
/// locations or contents, no language-server roots, no formatter executables,
/// and no MCP errors, URLs or OAuth detail. The sheet cannot show what it was
/// never sent, which is the point of sanitizing at the boundary rather than in
/// the UI.
class DiagnosticsSheet extends ConsumerStatefulWidget {
  const DiagnosticsSheet({
    super.key,
    required this.sessionId,
    this.canRefreshSkills = false,
    this.onAuthorSkill,
  });

  final String sessionId;

  /// Whether the session advertises instance recycling.
  final bool canRefreshSkills;

  /// Opens the authoring composer. Null hides the affordance.
  final void Function()? onAuthorSkill;

  @override
  ConsumerState<DiagnosticsSheet> createState() => DiagnosticsSheetState();
}

class DiagnosticsSheetState extends ConsumerState<DiagnosticsSheet> {
  SessionDiagnostics? _data;
  String? _error;
  String? _notice;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load().ignore();
  }

  Future<void> _load() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final d = await ref
          .read(mcremoteClientProvider)
          .sessionDiagnostics(widget.sessionId);
      if (mounted) setState(() => _data = d);
    } catch (e) {
      if (mounted) setState(() => _error = 'Diagnostics are unavailable.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Recycling is never automatic and always confirmed: it restarts the
  /// project's engine instance, and a user who did not ask for that would
  /// experience it as an unexplained interruption (MADR 0112 A10).
  Future<void> _refreshSkills() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Refresh skills?'),
        content: const Text(
          'This restarts the OpenCode instance for this project so it notices '
          'new skills. Your sessions, messages and tool history are kept.\n\n'
          'It only works while that project is idle.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const ValueKey('diagnostics-refresh-confirm'),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Refresh'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;

    setState(() {
      _busy = true;
      _notice = null;
      _error = null;
    });
    try {
      await ref.read(mcremoteClientProvider).refreshSkills(widget.sessionId);
      if (mounted) setState(() => _notice = 'Skills refreshed.');
      await _load();
    } catch (e) {
      if (!mounted) return;
      final busy = e.toString().contains('instance_busy');
      setState(() {
        _error = busy
            ? 'That project is busy. Your skill file is untouched — try again '
                  'once its OpenCode work is idle.'
            : 'The refresh failed.';
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final d = _data;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    'Diagnostics',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                if (_busy)
                  const SizedBox(
                    key: ValueKey('diagnostics-busy'),
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
              ],
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _error!,
                  key: const ValueKey('diagnostics-error'),
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            if (_notice != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _notice!,
                  key: const ValueKey('diagnostics-notice'),
                ),
              ),
            if (d != null)
              Flexible(
                child: ListView(
                  key: const ValueKey('diagnostics-body'),
                  shrinkWrap: true,
                  children: [
                    if (d.branch.isNotEmpty)
                      _row(Icons.call_split, 'Branch', d.branch),
                    _section(
                      context,
                      'Skills',
                      d.skills.length,
                      [
                        for (final s in d.skills)
                          _row(Icons.auto_awesome, s.name, s.description),
                      ],
                      trailing: widget.onAuthorSkill == null
                          ? null
                          : TextButton(
                              key: const ValueKey('diagnostics-author-skill'),
                              onPressed: widget.onAuthorSkill,
                              child: const Text('Create or update with agent'),
                            ),
                      extra: widget.canRefreshSkills
                          ? TextButton(
                              key: const ValueKey('diagnostics-refresh-skills'),
                              onPressed: _busy ? null : _refreshSkills,
                              child: const Text('Refresh skills'),
                            )
                          : null,
                    ),
                    _section(context, 'Language services', d.lsp.length, [
                      for (final l in d.lsp) _row(Icons.code, l.name, l.status),
                    ]),
                    _section(context, 'Formatters', d.formatters.length, [
                      for (final f in d.formatters)
                        _row(
                          Icons.format_align_left,
                          f.name,
                          '${f.enabled ? 'enabled' : 'disabled'} · '
                          '${f.extensions} extensions',
                        ),
                    ]),
                    _section(context, 'MCP servers', d.mcp.length, [
                      for (final m in d.mcp)
                        _row(Icons.hub_outlined, m.name, m.state),
                    ]),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }

  Widget _section(
    BuildContext context,
    String title,
    int count,
    List<Widget> rows, {
    Widget? trailing,
    Widget? extra,
  }) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const Divider(),
      Row(
        children: [
          Expanded(
            child: Text(
              '$title ($count)',
              style: Theme.of(context).textTheme.titleSmall,
            ),
          ),
          ?extra,
          ?trailing,
        ],
      ),
      if (rows.isEmpty)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 4),
          child: Text('None reported.'),
        )
      else
        ...rows,
    ],
  );

  Widget _row(IconData icon, String title, String subtitle) => ListTile(
    dense: true,
    leading: Icon(icon, size: 18),
    title: Text(title),
    subtitle: subtitle.isEmpty ? null : Text(subtitle, maxLines: 2),
  );
}
