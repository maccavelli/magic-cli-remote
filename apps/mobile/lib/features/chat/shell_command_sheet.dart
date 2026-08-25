import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';

/// Runs one command directly on the host (MADR 0112 A9, PLAN P10 step 5).
///
/// This is remote command execution, not a tool call - OpenCode's shell
/// endpoint bypasses the model's permission checks entirely. The sheet is built
/// around that: the command is shown back **non-editable** in a confirmation
/// that names every consequence, and submitting takes a second deliberate tap.
///
/// There is deliberately no terminal, stdin, PTY, environment editor, command
/// history, or background-job control. Those are not gaps to fill in later.

/// The daemon's own bound, mirrored so an over-long command is refused before a
/// round trip rather than after one.
const int kShellCommandMaxLen = 8192;

/// Why a command cannot be submitted, or null when it can.
///
/// Deliberately no attempt to judge what the command *does*: it can create
/// persistent filesystem and network effects and spawn descendants that outlive
/// it, and none of that is knowable from the string. Pretending otherwise would
/// be worse than saying so plainly.
String? validateShellCommand(String command) {
  final trimmed = command.trim();
  if (trimmed.isEmpty) return 'Enter a command to run.';
  if (command.length > kShellCommandMaxLen) {
    return 'Commands are limited to \$kShellCommandMaxLen characters.';
  }
  if (command.codeUnits.contains(0)) {
    return 'That command contains a null byte.';
  }
  return null;
}

class ShellCommandSheet extends ConsumerStatefulWidget {
  const ShellCommandSheet({super.key, required this.sessionId});

  final String sessionId;

  @override
  ConsumerState<ShellCommandSheet> createState() => ShellCommandSheetState();
}

class ShellCommandSheetState extends ConsumerState<ShellCommandSheet> {
  final _command = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _command.dispose();
    super.dispose();
  }

  static String _friendly(Object e) {
    final text = e.toString();
    if (text.contains('shell_disabled')) {
      return 'Running commands is not enabled on this host.';
    }
    if (text.contains('invalid_command')) {
      return 'That command was rejected.';
    }
    if (text.contains('turn_busy')) {
      return 'This session is already busy. Wait for it to finish.';
    }
    return 'The command could not be started.';
  }

  Future<void> _run() async {
    final command = _command.text;
    final problem = validateShellCommand(command);
    if (problem != null) {
      setState(() => _error = problem);
      return;
    }

    // The confirmation shows the command back verbatim and non-editable, so a
    // second deliberate action is required and what is confirmed is exactly
    // what will run.
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Run this command?'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              key: const ValueKey('shell-confirm-command'),
              width: double.infinity,
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: Theme.of(ctx).colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(6),
              ),
              child: Text(
                command,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
              ),
            ),
            const SizedBox(height: 12),
            const Text(
              'Runs directly on the host in this session’s working '
              'directory and bypasses model tool permissions. The command and '
              'output are recorded in the OpenCode session. Host effects may '
              'persist after timeout.',
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const ValueKey('shell-confirm-run'),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Run'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(mcremoteClientProvider).shell(widget.sessionId, command);
      if (mounted) Navigator.of(context).pop();
    } catch (e) {
      // Never retried: a timed-out command may still be running.
      if (mounted) {
        setState(() {
          _error = _friendly(e);
          _busy = false;
        });
      }
    }
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
              'Run a command',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 4),
            Text(
              'Runs on the host in this session’s working directory. It '
              'does not go through the agent’s permission checks.',
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('shell-command'),
              controller: _command,
              maxLength: kShellCommandMaxLen,
              maxLines: 3,
              enabled: !_busy,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
              decoration: const InputDecoration(
                isDense: true,
                labelText: 'Command',
                border: OutlineInputBorder(),
              ),
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _error!,
                  key: const ValueKey('shell-error'),
                  style: TextStyle(color: scheme.error),
                ),
              ),
            const SizedBox(height: 12),
            FilledButton(
              key: const ValueKey('shell-run'),
              onPressed: _busy ? null : _run,
              child: Text(_busy ? 'Running…' : 'Run…'),
            ),
          ],
        ),
      ),
    );
  }
}
