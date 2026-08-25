import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';

/// Session sharing (MADR 0112 A8, PLAN P9 steps 5 and 6).
///
/// Two rules shape this sheet. Sharing is never automatic — every publish is an
/// explicit confirmation that states plainly what becomes public. And state is
/// always shown, even when this daemon may not change it: a transcript shared
/// from the desktop is public regardless, and hiding that would be the more
/// dangerous silence.
class SessionShareSheet extends ConsumerStatefulWidget {
  const SessionShareSheet({
    super.key,
    required this.sessionId,
    this.canMutate = false,
  });

  final String sessionId;

  /// Whether the operator permits changing the share state. Controls only —
  /// it never hides the current state or an existing link.
  final bool canMutate;

  @override
  ConsumerState<SessionShareSheet> createState() => SessionShareSheetState();
}

class SessionShareSheetState extends ConsumerState<SessionShareSheet> {
  ShareState? _state;
  String? _error;
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
      final s = await ref
          .read(mcremoteClientProvider)
          .shareState(widget.sessionId);
      if (mounted) setState(() => _state = s);
    } catch (e) {
      if (mounted) setState(() => _error = _friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  static String _friendly(Object e) {
    final text = e.toString();
    if (text.contains('share_disabled')) {
      return 'Sharing is not enabled on this host.';
    }
    return 'The sharing request failed.';
  }

  /// Publishes after an explicit confirmation that names the consequence.
  ///
  /// There is no retry on failure: a retried share can publish twice, and a
  /// second public link is not something an automatic retry may create.
  Future<void> _share() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Share this session?'),
        content: const Text(
          'This uploads the whole transcript — your messages, the agent’s '
          'replies, and its tool output — to OpenCode’s service.\n\n'
          'Anyone with the link can read it. There is no password.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const ValueKey('share-confirm'),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Share'),
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
      final s = await ref.read(mcremoteClientProvider).share(widget.sessionId);
      if (mounted) setState(() => _state = s);
    } catch (e) {
      if (mounted) setState(() => _error = _friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _unshare() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(mcremoteClientProvider).unshare(widget.sessionId);
      if (mounted) setState(() => _state = const ShareState());
    } catch (e) {
      if (mounted) setState(() => _error = _friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final s = _state;
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
                    'Sharing',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                if (_busy)
                  const SizedBox(
                    key: ValueKey('share-busy'),
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            if (s != null)
              Text(
                s.shared
                    ? 'This session is shared publicly.'
                    : 'This session is private.',
                key: const ValueKey('share-status'),
                style: Theme.of(context).textTheme.bodyMedium,
              ),
            if (s != null && s.shared && !s.hasLink)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  'The link could not be verified, but the transcript is still '
                  'public.',
                  key: const ValueKey('share-unverified'),
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: scheme.error),
                ),
              ),
            if (s != null && s.hasLink)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: SelectableText(
                  s.url,
                  key: const ValueKey('share-url'),
                  style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
                ),
              ),
            if (s != null && s.disabled)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  'The agent’s own configuration disables sharing for this '
                  'session.',
                  key: const ValueKey('share-upstream-disabled'),
                ),
              ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _error!,
                  key: const ValueKey('share-error'),
                  style: TextStyle(color: scheme.error),
                ),
              ),
            const SizedBox(height: 12),
            if (widget.canMutate && s != null && !s.disabled)
              if (s.shared)
                OutlinedButton(
                  key: const ValueKey('share-unshare'),
                  onPressed: _busy ? null : _unshare,
                  child: const Text('Stop sharing'),
                )
              else
                FilledButton(
                  key: const ValueKey('share-start'),
                  onPressed: _busy ? null : _share,
                  child: const Text('Share publicly…'),
                ),
          ],
        ),
      ),
    );
  }
}
