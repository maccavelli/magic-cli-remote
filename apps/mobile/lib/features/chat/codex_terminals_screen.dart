import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/codex_execution_client.dart';
import '../../data/protocol/models.dart';

export '../../data/codex_execution_client.dart';

/// The Codex execution terminal view (MADR 0109 D10/D11, plan P7).
///
/// Two things this screen must never do: render an unsandboxed terminal as if
/// it were sandboxed, and splice output across a sequence gap. Both would give
/// the user a false picture of what ran on their host — the first about the
/// authority it ran with, the second about what it printed.
class CodexTerminalsScreen extends StatefulWidget {
  const CodexTerminalsScreen({
    required this.client,
    required this.sessionId,
    super.key,
  });

  final CodexExecutionClient client;
  final String sessionId;

  @override
  State<CodexTerminalsScreen> createState() => _CodexTerminalsScreenState();
}

class _CodexTerminalsScreenState extends State<CodexTerminalsScreen> {
  List<CodexTerminalInfo> _terminals = const [];
  final Map<String, CodexTerminalBuffer> _buffers = {};
  String _selected = '';
  String? _error;
  bool _loading = true;
  StreamSubscription<Map<String, dynamic>>? _pushes;
  final TextEditingController _stdin = TextEditingController();

  @override
  void initState() {
    super.initState();
    // Subscribe before the first read so a chunk arriving during the load is
    // appended rather than lost between the two.
    _pushes = widget.client.codexTerminalOutput.listen(_onPush);
    unawaited(_load());
  }

  @override
  void dispose() {
    unawaited(_pushes?.cancel());
    _stdin.dispose();
    super.dispose();
  }

  void _onPush(Map<String, dynamic> payload) {
    if (payload['session_id'] != widget.sessionId) return;
    final raw = payload['output'];
    if (raw is! Map) return;
    final chunk = CodexTerminalOutput.fromJson(Map<String, dynamic>.from(raw));
    if (chunk.terminalId.isEmpty) return;
    if (!mounted) return;
    setState(() {
      final existing =
          _buffers[chunk.terminalId] ?? const CodexTerminalBuffer();
      _buffers[chunk.terminalId] = existing.append([chunk]);
    });
  }

  Future<void> _load() async {
    try {
      final terminals = await widget.client.listTerminals(widget.sessionId);
      if (!mounted) return;
      setState(() {
        _terminals = terminals;
        _loading = false;
        _error = null;
        if (_selected.isEmpty && terminals.isNotEmpty) {
          _selected = terminals.first.id;
        }
      });
      if (_selected.isNotEmpty) await _refreshOutput(_selected);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = error.toString();
      });
    }
  }

  /// Replays from the last sequence this client already holds, so a
  /// reconnect costs one bounded read rather than the whole buffer.
  Future<void> _refreshOutput(String terminalId) async {
    try {
      final held = _buffers[terminalId];
      final fetched = await widget.client.readTerminalOutput(
        widget.sessionId,
        terminalId,
        afterSequence: held?.lastSequence ?? 0,
      );
      if (!mounted) return;
      setState(() {
        _buffers[terminalId] = (held ?? const CodexTerminalBuffer()).append(
          fetched.chunks,
        );
        if (fetched.sequenceGap) {
          _buffers[terminalId] = CodexTerminalBuffer(
            chunks: _buffers[terminalId]!.chunks,
            sequenceGap: true,
          );
        }
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    }
  }

  Future<void> _send() async {
    final terminalId = _selected;
    final text = _stdin.text;
    if (terminalId.isEmpty || text.isEmpty) return;
    _stdin.clear();
    try {
      await widget.client.writeTerminal(
        widget.sessionId,
        terminalId,
        '$text\n',
      );
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    }
  }

  Future<void> _stop(String terminalId) async {
    try {
      await widget.client.stopTerminal(widget.sessionId, terminalId);
      await _load();
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    }
  }

  Future<void> _stopAll() async {
    try {
      await widget.client.stopAllTerminals(widget.sessionId);
      await _load();
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final selected = _terminals.where((t) => t.id == _selected).firstOrNull;
    final buffer = _buffers[_selected];
    return Scaffold(
      appBar: AppBar(
        title: const Text('Terminals'),
        actions: [
          IconButton(
            onPressed: _loading ? null : _load,
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
          ),
          if (_terminals.any((t) => t.running))
            IconButton(
              onPressed: _stopAll,
              icon: const Icon(Icons.stop_circle_outlined),
              tooltip: 'Stop all',
            ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                if (_error != null)
                  Card(
                    color: theme.colorScheme.errorContainer,
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(
                        _error!,
                        style: TextStyle(
                          color: theme.colorScheme.onErrorContainer,
                        ),
                      ),
                    ),
                  ),
                if (_terminals.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 24),
                    child: Text('No active terminals.'),
                  ),
                for (final terminal in _terminals)
                  _TerminalTile(
                    terminal: terminal,
                    selected: terminal.id == _selected,
                    onSelect: () {
                      setState(() => _selected = terminal.id);
                      unawaited(_refreshOutput(terminal.id));
                    },
                    onStop: terminal.running ? () => _stop(terminal.id) : null,
                  ),
                if (selected != null) ...[
                  const SizedBox(height: 16),
                  if (buffer?.sequenceGap ?? false)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: Text(
                        '⚠ Output before this point was dropped from the '
                        'replay buffer and cannot be recovered.',
                        key: const Key('terminal-sequence-gap'),
                        style: TextStyle(color: theme.colorScheme.error),
                      ),
                    ),
                  Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(12),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: SelectableText(
                      buffer?.chunks.map((c) => c.text).join() ?? '',
                      key: const Key('terminal-output'),
                      style: const TextStyle(fontFamily: 'monospace'),
                    ),
                  ),
                  if (selected.running)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Row(
                        children: [
                          Expanded(
                            child: TextField(
                              controller: _stdin,
                              key: const Key('terminal-stdin'),
                              decoration: const InputDecoration(
                                labelText: 'Send to terminal',
                                isDense: true,
                              ),
                              onSubmitted: (_) => unawaited(_send()),
                            ),
                          ),
                          IconButton(
                            onPressed: () => unawaited(_send()),
                            icon: const Icon(Icons.send),
                          ),
                        ],
                      ),
                    ),
                ],
              ],
            ),
    );
  }
}

class _TerminalTile extends StatelessWidget {
  const _TerminalTile({
    required this.terminal,
    required this.selected,
    required this.onSelect,
    this.onStop,
  });

  final CodexTerminalInfo terminal;
  final bool selected;
  final VoidCallback onSelect;
  final VoidCallback? onStop;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      color: selected ? theme.colorScheme.secondaryContainer : null,
      child: ListTile(
        onTap: onSelect,
        title: Text(terminal.command.isEmpty ? terminal.id : terminal.command),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // The authority label is shown verbatim, in the error colour when
            // the terminal is outside the sandbox. Softening this is how a
            // user ends up not realising what a command could reach.
            Text(
              terminal.label,
              style: TextStyle(
                color: terminal.unsandboxed ? theme.colorScheme.error : null,
                fontWeight: terminal.unsandboxed ? FontWeight.bold : null,
              ),
            ),
            Text(
              terminal.running
                  ? 'running'
                  : 'exited${terminal.exitCode != null ? ' (${terminal.exitCode})' : ''}',
            ),
          ],
        ),
        trailing: onStop == null
            ? null
            : IconButton(
                onPressed: onStop,
                icon: const Icon(Icons.stop),
                tooltip: 'Stop',
              ),
      ),
    );
  }
}
