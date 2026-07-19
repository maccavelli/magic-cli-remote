import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';

class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({super.key, required this.sessionId});

  final String sessionId;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  final _composer = TextEditingController();
  final _scroll = ScrollController();
  final _items = <_ChatItem>[];
  StreamSubscription<SessionEvent>? _sub;
  String _status = 'idle';
  bool _sending = false;
  SessionEvent? _pendingPermission;

  @override
  void initState() {
    super.initState();
    final client = ref.read(mcremoteClientProvider);
    _sub = client.events.listen(_onEvent);
  }

  @override
  void dispose() {
    _sub?.cancel();
    _composer.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _onEvent(SessionEvent ev) {
    if (ev.sessionId != widget.sessionId) return;
    setState(() {
      if (ev.type == 'session_status' && (ev.status?.isNotEmpty ?? false)) {
        _status = ev.status!;
      }
      if (ev.type == 'permission_request') {
        _pendingPermission = ev;
        _items.add(_ChatItem.permission(ev));
      } else if (ev.type == 'assistant_message_chunk') {
        _appendAssistant(ev.text ?? '');
      } else if (ev.type == 'thought_chunk') {
        _appendThought(ev.text ?? '');
      } else if (ev.type == 'user_message') {
        _items.add(_ChatItem.user(ev.text ?? ''));
      } else if (ev.type == 'tool_call' || ev.type == 'tool_call_update') {
        _items.add(_ChatItem.tool(ev));
      } else if (ev.type == 'turn_complete') {
        _items.add(_ChatItem.system(
          'Turn complete${ev.stopReason != null ? ' (${ev.stopReason})' : ''}',
        ));
        _status = 'idle';
      } else if (ev.type == 'error') {
        _items.add(_ChatItem.system('Error: ${ev.error ?? 'unknown'}'));
        _status = 'error';
      }
    });
    _scrollToEnd();
    if (ev.type == 'permission_request' && mounted) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _showPermissionSheet(ev);
      });
    }
  }

  void _appendAssistant(String text) {
    if (_items.isNotEmpty && _items.last.kind == _ItemKind.assistant) {
      _items[_items.length - 1] =
          _items.last.copyWith(text: (_items.last.text ?? '') + text);
    } else {
      _items.add(_ChatItem.assistant(text));
    }
  }

  void _appendThought(String text) {
    if (_items.isNotEmpty && _items.last.kind == _ItemKind.thought) {
      _items[_items.length - 1] =
          _items.last.copyWith(text: (_items.last.text ?? '') + text);
    } else {
      _items.add(_ChatItem.thought(text));
    }
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

  Future<void> _cancel() async {
    try {
      await ref.read(mcremoteClientProvider).cancel(widget.sessionId);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Cancel failed: $e')),
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
                if (ev.toolId != null) ...[
                  const SizedBox(height: 4),
                  Text(
                    'Tool: ${ev.toolId}',
                    style: Theme.of(ctx).textTheme.bodySmall,
                  ),
                ],
                const SizedBox(height: 16),
                ...ev.options.map(
                  (o) => Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: FilledButton(
                      onPressed: () => Navigator.pop(ctx, o.optionId),
                      child: Text(o.name.isEmpty ? o.optionId : o.name),
                    ),
                  ),
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
      setState(() {
        if (_pendingPermission?.permissionId == ev.permissionId) {
          _pendingPermission = null;
        }
      });
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Permission respond failed: $e')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final shortId = widget.sessionId.length > 8
        ? widget.sessionId.substring(0, 8)
        : widget.sessionId;

    return Scaffold(
      appBar: AppBar(
        title: Text('Session $shortId'),
        actions: [
          if (_status == 'running')
            IconButton(
              tooltip: 'Cancel turn',
              onPressed: _cancel,
              icon: const Icon(Icons.stop_circle_outlined),
            ),
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Center(
              child: Chip(
                label: Text(_status, style: const TextStyle(fontSize: 12)),
                visualDensity: VisualDensity.compact,
              ),
            ),
          ),
        ],
      ),
      body: Column(
        children: [
          if (_pendingPermission != null)
            MaterialBanner(
              content: Text(
                'Waiting for permission: ${_pendingPermission!.toolName ?? 'tool'}',
              ),
              actions: [
                TextButton(
                  onPressed: () =>
                      _showPermissionSheet(_pendingPermission!),
                  child: const Text('Review'),
                ),
              ],
            ),
          Expanded(
            child: ListView.builder(
              controller: _scroll,
              padding: const EdgeInsets.all(12),
              itemCount: _items.length,
              itemBuilder: (ctx, i) => _ChatBubble(item: _items[i]),
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
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _send(),
                      decoration: const InputDecoration(
                        hintText: 'Send a prompt…',
                        border: OutlineInputBorder(),
                        isDense: true,
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    onPressed: _sending ? null : _send,
                    icon: _sending
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.send),
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

enum _ItemKind { user, assistant, thought, tool, system, permission }

class _ChatItem {
  _ChatItem({
    required this.kind,
    this.text,
    this.event,
  });

  final _ItemKind kind;
  final String? text;
  final SessionEvent? event;

  factory _ChatItem.user(String t) => _ChatItem(kind: _ItemKind.user, text: t);
  factory _ChatItem.assistant(String t) =>
      _ChatItem(kind: _ItemKind.assistant, text: t);
  factory _ChatItem.thought(String t) =>
      _ChatItem(kind: _ItemKind.thought, text: t);
  factory _ChatItem.tool(SessionEvent e) =>
      _ChatItem(kind: _ItemKind.tool, event: e, text: e.toolName ?? e.text);
  factory _ChatItem.system(String t) =>
      _ChatItem(kind: _ItemKind.system, text: t);
  factory _ChatItem.permission(SessionEvent e) =>
      _ChatItem(kind: _ItemKind.permission, event: e, text: e.text);

  _ChatItem copyWith({String? text}) =>
      _ChatItem(kind: kind, text: text ?? this.text, event: event);
}

class _ChatBubble extends StatelessWidget {
  const _ChatBubble({required this.item});

  final _ChatItem item;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    switch (item.kind) {
      case _ItemKind.user:
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
      case _ItemKind.assistant:
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
      case _ItemKind.thought:
        return ExpansionTile(
          dense: true,
          title: const Text('Thinking…', style: TextStyle(fontSize: 13)),
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
      case _ItemKind.tool:
        final ev = item.event;
        return Card(
          margin: const EdgeInsets.symmetric(vertical: 4),
          child: ListTile(
            leading: const Icon(Icons.build_circle_outlined),
            title: Text(ev?.toolName ?? item.text ?? 'Tool'),
            subtitle: Text(
              [
                if (ev?.status != null) ev!.status!,
                if (ev?.toolId != null) ev!.toolId!,
              ].join(' · '),
            ),
          ),
        );
      case _ItemKind.permission:
        return Card(
          color: scheme.tertiaryContainer,
          child: ListTile(
            leading: const Icon(Icons.shield_outlined),
            title: const Text('Permission requested'),
            subtitle: Text(item.text ?? ''),
          ),
        );
      case _ItemKind.system:
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
