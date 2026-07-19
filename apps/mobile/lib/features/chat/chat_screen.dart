import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';

class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({
    super.key,
    required this.sessionId,
    this.sessionName,
  });

  final String sessionId;
  final String? sessionName;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  final _composer = TextEditingController();
  final _scroll = ScrollController();
  bool _sending = false;
  bool _userNearBottom = true;
  final _presentedPermissionIds = <String>{};

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
  }

  @override
  void dispose() {
    _composer.dispose();
    _scroll.removeListener(_onScroll);
    _scroll.dispose();
    super.dispose();
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
      _composer.clear();
      _userNearBottom = true;
      _scrollToEnd();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Send failed: $e')),
        );
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
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Cancel failed: $e')),
        );
      }
    }
  }

  Future<void> _endSession() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('End session?'),
        content: const Text(
          'Stops the agent on the host and removes this session from the live list. '
          'You can create a new one afterward.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Keep open'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('End session'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    try {
      try {
        await ref.read(mcremoteClientProvider).cancel(widget.sessionId);
      } catch (_) {}
      await ref.read(mcremoteClientProvider).closeSession(widget.sessionId);
      ref.read(transcriptsProvider.notifier).clearSession(widget.sessionId);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Session ended')),
      );
      Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('End session failed: $e')),
        );
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
                  ...options.map(
                    (o) {
                      final isAllow = (o.kind?.contains('allow') ?? false) ||
                          o.optionId.contains('allow');
                      return Padding(
                        padding: const EdgeInsets.only(bottom: 8),
                        child: isAllow
                            ? FilledButton(
                                onPressed: () =>
                                    Navigator.pop(ctx, o.optionId),
                                child: Text(
                                  o.name.isEmpty ? o.optionId : o.name,
                                ),
                              )
                            : OutlinedButton(
                                onPressed: () =>
                                    Navigator.pop(ctx, o.optionId),
                                child: Text(
                                  o.name.isEmpty ? o.optionId : o.name,
                                ),
                              ),
                      );
                    },
                  ),
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

    if (result == null || ev.permissionId == null) return;
    final client = ref.read(mcremoteClientProvider);
    try {
      if (result == '__cancel__') {
        await client.respondPermission(
          sessionId: widget.sessionId,
          permissionId: ev.permissionId!,
          cancelled: true,
        );
      } else {
        await client.respondPermission(
          sessionId: widget.sessionId,
          permissionId: ev.permissionId!,
          optionId: result,
        );
      }
      ref.read(transcriptsProvider.notifier).clearPending(
            widget.sessionId,
            permissionId: ev.permissionId,
          );
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Permission respond failed: $e')),
        );
      }
    }
  }

  void _maybeShowPermission(SessionTranscript transcript) {
    final pending = transcript.pendingPermission;
    if (pending == null) return;
    final id = pending.permissionId;
    if (id == null || id.isEmpty) return;
    if (_presentedPermissionIds.contains(id)) return;
    _presentedPermissionIds.add(id);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_showPermissionSheet(pending));
    });
  }

  @override
  Widget build(BuildContext context) {
    final transcript = ref.watch(sessionTranscriptProvider(widget.sessionId));
    final items = transcript.items;
    final status = transcript.status;
    final pendingPermission = transcript.pendingPermission;

    ref.listen<SessionTranscript>(
      sessionTranscriptProvider(widget.sessionId),
      (prev, next) {
        final grew = next.items.length > (prev?.items.length ?? 0);
        if (grew && _userNearBottom) {
          _scrollToEnd();
        }
        _maybeShowPermission(next);
      },
    );

    final busy = _sending ||
        status == 'running' ||
        pendingPermission != null;

    final title = (widget.sessionName != null && widget.sessionName!.isNotEmpty)
        ? widget.sessionName!
        : (widget.sessionId.length > 8
            ? 'Session ${widget.sessionId.substring(0, 8)}'
            : widget.sessionId);

    final conn = ref.watch(connectionStateProvider);
    final connState = conn.asData?.value;
    final offline = connState != null &&
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
                    try {
                      await ref.read(mcremoteClientProvider).reconnect();
                    } catch (e) {
                      if (mounted) {
                        ScaffoldMessenger.of(context).showSnackBar(
                          SnackBar(content: Text('$e')),
                        );
                      }
                    }
                  },
                  child: const Text('Retry'),
                ),
              ),
            ),
          if (pendingPermission != null)
            MaterialBanner(
              content: Text(
                'Waiting for permission: ${pendingPermission.toolName ?? 'tool'}',
              ),
              actions: [
                TextButton(
                  onPressed: () => _showPermissionSheet(pendingPermission),
                  child: const Text('Review'),
                ),
              ],
            ),
          Expanded(
            child: items.isEmpty
                ? Center(
                    child: Text(
                      'Send a prompt to start',
                      style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                            color: Theme.of(context)
                                .colorScheme
                                .onSurfaceVariant,
                          ),
                    ),
                  )
                : ListView.builder(
                    controller: _scroll,
                    padding: const EdgeInsets.all(12),
                    itemCount: items.length,
                    itemBuilder: (ctx, i) => _ChatBubble(
                      item: items[i],
                      agentRunning: status == 'running',
                    ),
                  ),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _composer,
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
                                : 'Send a prompt…',
                        border: const OutlineInputBorder(),
                        isDense: true,
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  if (busy)
                    IconButton.filled(
                      style: IconButton.styleFrom(
                        backgroundColor:
                            Theme.of(context).colorScheme.error,
                        foregroundColor:
                            Theme.of(context).colorScheme.onError,
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

class _ChatBubble extends StatelessWidget {
  const _ChatBubble({required this.item, required this.agentRunning});

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
            leading: Icon(
              status == 'completed' || status == 'success'
                  ? Icons.check_circle_outline
                  : status == 'failed' || status == 'error'
                      ? Icons.error_outline
                      : Icons.build_circle_outlined,
            ),
            title: Text(item.toolName ?? 'Tool'),
            subtitle: subtitle.isEmpty
                ? null
                : Text(
                    subtitle,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
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
        return Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Center(
            child: Text(
              item.text ?? '',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ),
        );
    }
  }
}
