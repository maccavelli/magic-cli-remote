import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';

/// Drop ids from [presented] that are no longer in [stillPending] (resolved),
/// mutating [presented] and returning the dropped ids. Still-pending ids are
/// always kept, so a resolved permission is forgotten — bounding the set for
/// the widget's lifetime — without ever re-presenting one that is still live.
@visibleForTesting
Set<String> prunePresentedPermissionIds(
  Set<String> presented,
  Set<String> stillPending,
) {
  final dropped = presented.where((id) => !stillPending.contains(id)).toSet();
  presented.removeAll(dropped);
  return dropped;
}

class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({super.key, required this.sessionId, this.sessionName});

  final String sessionId;
  final String? sessionName;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  final _composer = TextEditingController();
  final _scroll = ScrollController();
  final _focus = FocusNode();
  bool _sending = false;
  bool _userNearBottom = true;
  final _presentedPermissionIds = <String>{};
  bool _permissionSheetOpen = false;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    // Runs once per chat open. If the local transcript is empty (process-death
    // recovery, or a session never seen live), pull recorded history; a
    // populated in-memory transcript survives route disposal and is skipped.
    unawaited(_maybeReplayHistory());
    // A permission that arrived while the user was on the sessions list
    // produces no further transcript change (the agent is blocked), so the
    // ref.listen in build() would never fire for it.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      _maybeShowPermission(
        ref.read(sessionTranscriptProvider(widget.sessionId)),
      );
    });
  }

  /// Fetch and replay recorded history for an empty transcript, once per open.
  ///
  /// Guarded on emptiness at open time so re-entering a populated chat does not
  /// re-fetch. The notifier applies the result ONLY IF the transcript is still
  /// empty when the response lands — live events that raced in meanwhile are
  /// authoritative and win, so history is dropped rather than double-applied.
  Future<void> _maybeReplayHistory() async {
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    if (transcript.items.isNotEmpty) return;
    final client = ref.read(mcremoteClientProvider);
    final events = await client.sessionHistory(widget.sessionId);
    if (!mounted || events.isEmpty) return;
    ref
        .read(transcriptsProvider.notifier)
        .replayHistory(widget.sessionId, events);
  }

  @override
  void dispose() {
    _composer.dispose();
    _focus.dispose();
    _scroll.removeListener(_onScroll);
    _scroll.dispose();
    super.dispose();
  }

  List<AvailableCommand> _matchingCommands(
    List<AvailableCommand> all,
    String text,
  ) {
    if (!text.startsWith('/')) return const [];
    // Only autocomplete the first token (before space) as the command name.
    final space = text.indexOf(' ');
    if (space >= 0) return const [];
    final q = text.substring(1).toLowerCase();
    if (all.isEmpty) return const [];
    return all
        .where((c) => q.isEmpty || c.name.toLowerCase().startsWith(q))
        .take(8)
        .toList();
  }

  void _insertCommand(AvailableCommand cmd) {
    _composer.value = TextEditingValue(
      text: cmd.insertText,
      selection: TextSelection.collapsed(offset: cmd.insertText.length),
    );
    _focus.requestFocus();
  }

  void _onScroll() {
    if (!_scroll.hasClients) return;
    final pos = _scroll.position;
    _userNearBottom = pos.maxScrollExtent - pos.pixels < 120;
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.animateTo(
        _scroll.position.maxScrollExtent + 80,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    });
  }

  Future<void> _send() async {
    final text = _composer.text.trim();
    if (text.isEmpty || _sending) return;
    setState(() => _sending = true);
    try {
      final client = ref.read(mcremoteClientProvider);
      await client.prompt(widget.sessionId, text);
      // Guard the async gap: backing out of the chat mid-request disposes
      // the controller, and clear() on a disposed one throws.
      if (!mounted) return;
      _composer.clear();
      _userNearBottom = true;
      _scrollToEnd();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Send failed: $e')));
      }
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _cancelTurn() async {
    try {
      await ref.read(mcremoteClientProvider).cancel(widget.sessionId);
      ref.read(transcriptsProvider.notifier).announceCancel(widget.sessionId);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Cancel failed: $e')));
      }
    }
  }

  Future<void> _endSession() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('End agent session?'),
        content: const Text(
          'Stops this agent on the host and removes it from the sessions list.\n\n'
          'Your phone stays paired to the host until you sign out.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Keep open'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(ctx).colorScheme.error,
              foregroundColor: Theme.of(ctx).colorScheme.onError,
            ),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('End session'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final client = ref.read(mcremoteClientProvider);
    ref.read(transcriptsProvider.notifier).clearSession(widget.sessionId);
    try {
      if (client.state == McConnectionState.connected) {
        try {
          await client.cancel(widget.sessionId);
        } catch (_) {}
        // session.delete closes the live session and purges the disk record.
        // closeSession alone leaves the record, so the row would reappear on
        // the next session.list — the dialog promises removal.
        await client.deleteSession(widget.sessionId);
      }
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Session ended')));
      Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('End session failed: $e')));
        // Still leave chat — list will refresh on return.
        Navigator.of(context).pop(true);
      }
    }
  }

  Future<void> _showPermissionSheet(SessionEvent ev) async {
    final result = await showModalBottomSheet<String>(
      context: context,
      isDismissible: false,
      enableDrag: false,
      builder: (ctx) {
        final options = ev.options;
        return SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  'Permission required',
                  style: Theme.of(ctx).textTheme.titleLarge,
                ),
                const SizedBox(height: 8),
                Text(ev.toolName ?? ev.text ?? 'Tool needs approval'),
                const SizedBox(height: 16),
                if (options.isEmpty)
                  FilledButton(
                    onPressed: () => Navigator.pop(ctx, '__cancel__'),
                    child: const Text('Dismiss'),
                  )
                else
                  ...options.map((o) {
                    final isAllow =
                        (o.kind?.contains('allow') ?? false) ||
                        o.optionId.contains('allow');
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: isAllow
                          ? FilledButton(
                              onPressed: () => Navigator.pop(ctx, o.optionId),
                              child: Text(o.name.isEmpty ? o.optionId : o.name),
                            )
                          : OutlinedButton(
                              onPressed: () => Navigator.pop(ctx, o.optionId),
                              child: Text(o.name.isEmpty ? o.optionId : o.name),
                            ),
                    );
                  }),
                TextButton(
                  onPressed: () => Navigator.pop(ctx, '__cancel__'),
                  child: const Text('Cancel request'),
                ),
              ],
            ),
          ),
        );
      },
    );

    final permissionId = ev.permissionId;
    if (result == null || permissionId == null) return;
    if (!mounted) return;
    final client = ref.read(mcremoteClientProvider);
    try {
      if (result == '__cancel__') {
        await client.respondPermission(
          sessionId: widget.sessionId,
          permissionId: permissionId,
          cancelled: true,
        );
      } else {
        await client.respondPermission(
          sessionId: widget.sessionId,
          permissionId: permissionId,
          optionId: result,
        );
      }
      if (!mounted) return;
      ref
          .read(transcriptsProvider.notifier)
          .clearPending(widget.sessionId, permissionId: permissionId);
    } catch (e) {
      // The response never landed, so this request is still outstanding —
      // forget that we presented it or it can never be retried.
      _presentedPermissionIds.remove(permissionId);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Permission respond failed: $e')),
        );
      }
    }
  }

  /// Present outstanding permission requests one at a time, oldest first.
  ///
  /// The daemon allows concurrent requests, so this drains a queue rather than
  /// showing a single sheet.
  void _maybeShowPermission(SessionTranscript transcript) {
    // Bound the set for the widget's lifetime: forget ids that have left
    // pendingPermissions (resolved). Pruning only non-pending ids preserves the
    // safety property — a still-pending id is never dropped, so it can never be
    // re-presented while its sheet is up or its request outstanding.
    prunePresentedPermissionIds(
      _presentedPermissionIds,
      transcript.pendingPermissions.keys.toSet(),
    );
    if (_permissionSheetOpen) return;
    for (final pending in transcript.pendingPermissions.values) {
      final id = pending.permissionId;
      if (id == null || id.isEmpty) continue;
      if (_presentedPermissionIds.contains(id)) continue;
      _presentedPermissionIds.add(id);
      _permissionSheetOpen = true;
      WidgetsBinding.instance.addPostFrameCallback((_) async {
        if (!mounted) {
          _permissionSheetOpen = false;
          return;
        }
        try {
          await _showPermissionSheet(pending);
        } finally {
          _permissionSheetOpen = false;
        }
        // Another request may have arrived (or been left) while this sheet
        // was up; drain the rest.
        if (mounted) {
          _maybeShowPermission(
            ref.read(sessionTranscriptProvider(widget.sessionId)),
          );
        }
      });
      return;
    }
  }

  @override
  Widget build(BuildContext context) {
    final transcript = ref.watch(sessionTranscriptProvider(widget.sessionId));
    final items = transcript.items;

    ref.listen(sessionTranscriptProvider(widget.sessionId), (prev, next) {
      if (prev == null) return;
      if (next.items.length > prev.items.length ||
          (next.items.isNotEmpty &&
              prev.items.isNotEmpty &&
              next.items.last.seq == prev.items.last.seq &&
              (next.items.last.text?.length ?? 0) >
                  (prev.items.last.text?.length ?? 0))) {
        if (_userNearBottom) {
          _scrollToEnd();
        }
      }
      _maybeShowPermission(next);
    });

    final status = transcript.status;
    final pendingPermission = transcript.pendingPermission;
    final pendingCount = transcript.pendingPermissions.length;
    final commands = transcript.commands;
    final plan = transcript.plan;

    ref.listen<SessionTranscript>(sessionTranscriptProvider(widget.sessionId), (
      prev,
      next,
    ) {
      final grew = next.items.length > (prev?.items.length ?? 0);
      if (grew && _userNearBottom) {
        _scrollToEnd();
      }
      _maybeShowPermission(next);
    });

    final busy = _sending || status == 'running' || pendingPermission != null;

    final title = (widget.sessionName != null && widget.sessionName!.isNotEmpty)
        ? widget.sessionName!
        : (widget.sessionId.length > 8
              ? 'Session ${widget.sessionId.substring(0, 8)}'
              : widget.sessionId);

    final conn = ref.watch(connectionStateProvider);
    final connState = conn.asData?.value;
    final offline =
        connState != null &&
        connState != McConnectionState.connected &&
        connState != McConnectionState.reconnecting;

    return Scaffold(
      appBar: AppBar(
        title: Text(title),
        actions: [
          if (busy)
            IconButton(
              tooltip: 'Stop turn',
              onPressed: _cancelTurn,
              icon: const Icon(Icons.stop_circle),
              color: Theme.of(context).colorScheme.error,
            ),
          Padding(
            padding: const EdgeInsets.only(right: 4),
            child: Center(
              child: Chip(
                label: Text(status, style: const TextStyle(fontSize: 12)),
                visualDensity: VisualDensity.compact,
              ),
            ),
          ),
          PopupMenuButton<String>(
            tooltip: 'Session actions',
            onSelected: (v) {
              if (v == 'cancel') unawaited(_cancelTurn());
              if (v == 'end') unawaited(_endSession());
            },
            itemBuilder: (ctx) => [
              const PopupMenuItem(
                value: 'cancel',
                child: ListTile(
                  leading: Icon(Icons.stop_circle_outlined),
                  title: Text('Stop current turn'),
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                ),
              ),
              const PopupMenuItem(
                value: 'end',
                child: ListTile(
                  leading: Icon(Icons.delete_outline),
                  title: Text('End session'),
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                ),
              ),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
          if (connState == McConnectionState.reconnecting)
            Material(
              color: Theme.of(context).colorScheme.tertiaryContainer,
              child: const ListTile(
                dense: true,
                leading: SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                title: Text('Reconnecting to host…'),
              ),
            ),
          if (offline)
            Material(
              color: Theme.of(context).colorScheme.errorContainer,
              child: ListTile(
                dense: true,
                leading: const Icon(Icons.wifi_off),
                title: const Text('Disconnected'),
                trailing: TextButton(
                  onPressed: () async {
                    // Resolve the messenger before the await so we never touch
                    // a stale BuildContext afterwards.
                    final messenger = ScaffoldMessenger.of(context);
                    try {
                      final store = ref.read(settingsStoreProvider);
                      await ref
                          .read(mcremoteClientProvider)
                          .reconnectFromStore(store);
                    } catch (e) {
                      messenger.showSnackBar(
                        SnackBar(content: Text('Reconnect failed: $e')),
                      );
                    }
                  },
                  child: const Text('Retry now'),
                ),
              ),
            ),
          if (pendingPermission != null)
            MaterialBanner(
              content: Text(
                pendingCount > 1
                    ? 'Waiting for $pendingCount permissions: '
                          '${pendingPermission.toolName ?? 'tool'} and '
                          '${pendingCount - 1} more'
                    : 'Waiting for permission: '
                          '${pendingPermission.toolName ?? 'tool'}',
              ),
              actions: [
                TextButton(
                  onPressed: () {
                    // Allow re-presenting after a dismissal or failed send.
                    _presentedPermissionIds.clear();
                    _maybeShowPermission(transcript);
                  },
                  child: const Text('Review'),
                ),
              ],
            ),
          Expanded(
            child: items.isEmpty
                ? Center(
                    child: Text(
                      commands.isEmpty
                          ? 'Send a prompt to start'
                          : 'Send a prompt or type / for slash commands',
                      style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  )
                : ListView.builder(
                    controller: _scroll,
                    padding: const EdgeInsets.all(12),
                    itemCount: items.length,
                    itemBuilder: (ctx, i) => _ChatBubble(
                      // Stable across FIFO trims — an index key would hand a
                      // trimmed item's ExpansionTile state to its neighbour.
                      key: ValueKey(items[i].seq),
                      item: items[i],
                      agentRunning:
                          status == 'running' && i == items.length - 1,
                    ),
                  ),
          ),
          // Compact, collapsible plan panel above the command strip. Kept out
          // of the scrolling transcript; hidden entirely when the plan is empty.
          if (plan.isNotEmpty) _PlanPanel(entries: plan),
          // Scoped to the composer's value so typing rebuilds only the
          // command strip, not the whole transcript list.
          ValueListenableBuilder<TextEditingValue>(
            valueListenable: _composer,
            builder: (ctx, value, _) {
              final matches = _matchingCommands(commands, value.text);
              if (matches.isNotEmpty) {
                return Material(
                  elevation: 2,
                  color: Theme.of(ctx).colorScheme.surfaceContainerHigh,
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxHeight: 200),
                    child: ListView.builder(
                      shrinkWrap: true,
                      itemCount: matches.length,
                      itemBuilder: (ctx, i) {
                        final c = matches[i];
                        final subtitle = [
                          if (c.description.isNotEmpty) c.description,
                          if (c.hint.isNotEmpty) 'hint: ${c.hint}',
                        ].join(' · ');
                        return ListTile(
                          dense: true,
                          leading: const Icon(Icons.flash_on, size: 18),
                          title: Text('/${c.name}'),
                          subtitle: subtitle.isEmpty ? null : Text(subtitle),
                          onTap: () => _insertCommand(c),
                        );
                      },
                    ),
                  ),
                );
              }
              if (busy || offline || commands.isEmpty) {
                return const SizedBox.shrink();
              }
              return SizedBox(
                height: 40,
                child: ListView.separated(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  itemCount: commands.length.clamp(0, 12),
                  separatorBuilder: (_, _) => const SizedBox(width: 8),
                  itemBuilder: (ctx, i) {
                    final c = commands[i];
                    return ActionChip(
                      label: Text('/${c.name}'),
                      tooltip: c.description.isEmpty ? null : c.description,
                      onPressed: () => _insertCommand(c),
                    );
                  },
                ),
              );
            },
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _composer,
                      focusNode: _focus,
                      minLines: 1,
                      maxLines: 5,
                      enabled: !busy && !offline,
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _send(),
                      decoration: InputDecoration(
                        hintText: offline
                            ? 'Disconnected'
                            : busy
                            ? 'Agent running…'
                            : commands.isEmpty
                            ? 'Send a prompt…'
                            : 'Prompt or /command…',
                        border: const OutlineInputBorder(),
                        isDense: true,
                        prefixIcon: commands.isEmpty
                            ? null
                            : IconButton(
                                tooltip: 'Slash commands',
                                icon: const Icon(Icons.terminal, size: 20),
                                onPressed: busy || offline
                                    ? null
                                    : () {
                                        _composer.text = '/';
                                        _composer.selection =
                                            const TextSelection.collapsed(
                                              offset: 1,
                                            );
                                        _focus.requestFocus();
                                      },
                              ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  if (busy)
                    IconButton.filled(
                      style: IconButton.styleFrom(
                        backgroundColor: Theme.of(context).colorScheme.error,
                        foregroundColor: Theme.of(context).colorScheme.onError,
                      ),
                      tooltip: 'Stop turn',
                      onPressed: _cancelTurn,
                      icon: const Icon(Icons.stop),
                    )
                  else
                    IconButton.filled(
                      onPressed: offline ? null : _send,
                      icon: const Icon(Icons.send),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Collapsible panel summarising the agent's current plan (ACP `Plan`).
///
/// Replace-semantics: it renders whatever the latest `plan` event left in
/// [SessionTranscript.plan]. Lives above the composer, never in the scrolling
/// transcript, so plan churn does not push chat content around.
class _PlanPanel extends StatelessWidget {
  const _PlanPanel({required this.entries});

  final List<PlanEntry> entries;

  IconData _iconFor(String status) {
    switch (status) {
      case 'completed':
        return Icons.check_circle;
      case 'in_progress':
        return Icons.autorenew;
      default:
        return Icons.radio_button_unchecked;
    }
  }

  Color _colorFor(String status, ColorScheme scheme) {
    switch (status) {
      case 'completed':
        return scheme.primary;
      case 'in_progress':
        return scheme.tertiary;
      default:
        return scheme.onSurfaceVariant;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final done = entries.where((e) => e.status == 'completed').length;
    return Material(
      color: scheme.surfaceContainerHigh,
      child: Theme(
        // ExpansionTile draws divider lines above and below when placed in a
        // Column; suppress them so the panel reads as one block.
        data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          dense: true,
          tilePadding: const EdgeInsets.symmetric(horizontal: 12),
          leading: const Icon(Icons.checklist, size: 20),
          title: Text('Plan', style: Theme.of(context).textTheme.titleSmall),
          subtitle: Text('$done/${entries.length} done'),
          children: [
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 220),
              child: ListView.builder(
                shrinkWrap: true,
                padding: const EdgeInsets.only(bottom: 8),
                itemCount: entries.length,
                itemBuilder: (ctx, i) {
                  final e = entries[i];
                  return ListTile(
                    dense: true,
                    visualDensity: VisualDensity.compact,
                    leading: Icon(
                      _iconFor(e.status),
                      size: 18,
                      color: _colorFor(e.status, scheme),
                    ),
                    title: Text(
                      e.content,
                      style: TextStyle(
                        decoration: e.status == 'completed'
                            ? TextDecoration.lineThrough
                            : null,
                        color: e.status == 'completed'
                            ? scheme.onSurfaceVariant
                            : null,
                      ),
                    ),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChatBubble extends StatelessWidget {
  const _ChatBubble({
    super.key,
    required this.item,
    required this.agentRunning,
  });

  final ChatItem item;
  final bool agentRunning;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    switch (item.kind) {
      case ChatItemKind.user:
        return Align(
          alignment: Alignment.centerRight,
          child: Container(
            margin: const EdgeInsets.symmetric(vertical: 4),
            padding: const EdgeInsets.all(12),
            constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.85,
            ),
            decoration: BoxDecoration(
              color: scheme.primaryContainer,
              borderRadius: BorderRadius.circular(12),
            ),
            child: SelectableText(item.text ?? ''),
          ),
        );
      case ChatItemKind.assistant:
        return Align(
          alignment: Alignment.centerLeft,
          child: Container(
            margin: const EdgeInsets.symmetric(vertical: 4),
            padding: const EdgeInsets.all(12),
            constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.9,
            ),
            decoration: BoxDecoration(
              color: scheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(12),
            ),
            child: SelectableText(item.text ?? ''),
          ),
        );
      case ChatItemKind.thought:
        return ExpansionTile(
          dense: true,
          initiallyExpanded: false,
          leading: agentRunning
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.psychology_outlined, size: 20),
          title: Text(
            agentRunning ? 'Thinking…' : 'Thought',
            style: const TextStyle(fontSize: 13),
          ),
          children: [
            Align(
              alignment: Alignment.centerLeft,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                child: SelectableText(
                  item.text ?? '',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
            ),
          ],
        );
      case ChatItemKind.tool:
        final status = item.toolStatus ?? '';
        final detail = (item.text ?? '').trim();
        final subtitle = [
          if (status.isNotEmpty) status,
          if (detail.isNotEmpty && detail != item.toolName) detail,
        ].join(' · ');
        return Card(
          margin: const EdgeInsets.symmetric(vertical: 4),
          child: ExpansionTile(
            leading: (status == 'running' || status == 'pending')
                ? const SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Icon(
                    status == 'completed' || status == 'success'
                        ? Icons.check_circle_outline
                        : status == 'failed' || status == 'error'
                        ? Icons.error_outline
                        : Icons.build_circle_outlined,
                  ),
            title: Text(item.toolName ?? 'Tool'),
            subtitle: subtitle.isEmpty
                ? null
                : Text(subtitle, maxLines: 2, overflow: TextOverflow.ellipsis),
            children: [
              if (detail.isNotEmpty)
                Align(
                  alignment: Alignment.centerLeft,
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                    child: SelectableText(
                      detail,
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ),
                ),
            ],
          ),
        );
      case ChatItemKind.system:
        final text = item.text ?? '';
        final isError = text.startsWith('Error:');
        return Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Center(
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (isError) ...[
                  Icon(Icons.error_outline, size: 14, color: scheme.error),
                  const SizedBox(width: 4),
                ],
                Flexible(
                  child: Text(
                    text,
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: isError ? scheme.error : scheme.onSurfaceVariant,
                    ),
                  ),
                ),
              ],
            ),
          ),
        );
    }
  }
}
