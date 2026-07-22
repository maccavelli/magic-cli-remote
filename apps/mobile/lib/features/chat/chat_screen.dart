import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollCacheExtent;
import 'package:flutter/services.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:speech_to_text/speech_to_text.dart';

import '../../data/chat/streaming_markdown.dart';
import '../../data/chat/transcript_rows.dart';
import '../../data/notifications/notification_coordinator.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/scroll_activity.dart';
import '../../theme/starfield.dart';
import '../../theme/widgets.dart';

/// Slash commands the daemon interprets itself (see
/// `internal/session/commands.go`). Always offered in autocomplete regardless of
/// what the agent advertises, since the server intercepts them.
final List<AvailableCommand> _builtinCommands = [
  AvailableCommand(
    name: 'model',
    description: 'Show or switch the agent model (restarts it)',
    hint: '[name]',
  ),
  AvailableCommand(
    name: 'reset',
    description: 'Restart the agent with a fresh context',
  ),
  AvailableCommand(
    name: 'new',
    description: 'Start a new agent session',
    hint: '[name]',
  ),
  AvailableCommand(name: 'help', description: 'List slash commands'),
];

/// Conservative permission-option classification. Substring matching is
/// dangerous here: `disallow`/`not_allowed` contain "allow" and would have
/// been styled as the prominent approve button. Unknown options render as
/// neutral outlined buttons instead.
@visibleForTesting
bool isAllowOption(PermissionOption o) {
  final kind = (o.kind ?? '').toLowerCase();
  final id = o.optionId.toLowerCase();
  if (kind.startsWith('reject') ||
      kind.startsWith('deny') ||
      id.startsWith('disallow') ||
      id.startsWith('deny') ||
      id.startsWith('reject') ||
      id.startsWith('not_')) {
    return false;
  }
  if (kind == 'allow' ||
      kind == 'allow_once' ||
      kind == 'allow-once' ||
      kind == 'allow_always' ||
      kind == 'allow-always') {
    return true;
  }
  const ids = {
    'allow',
    'allow_once',
    'allow-once',
    'allow_always',
    'allow-always',
    'yes',
    'approve',
  };
  return ids.contains(id);
}

@visibleForTesting
bool isAlwaysOption(PermissionOption o) {
  final kind = (o.kind ?? '').toLowerCase();
  final id = o.optionId.toLowerCase();
  return kind == 'allow_always' ||
      kind == 'allow-always' ||
      id == 'allow_always' ||
      id == 'allow-always' ||
      id == 'always';
}

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

  /// Prompts submitted while the agent was mid-turn, in send order. Flushed
  /// one per completed turn by [_maybeFlushQueue]; each shows as a removable
  /// chip above the composer until it goes out.
  final List<String> _queuedPrompts = [];
  bool _flushScheduled = false;
  final _presentedPermissionIds = <String>{};
  bool _permissionSheetOpen = false;
  NotificationCoordinator? _notifCoord;

  /// Pops the currently open permission sheet (set while one is up), so an
  /// externally resolved request can dismiss its own stale sheet.
  VoidCallback? _dismissSheet;
  String? _openSheetPermissionId;

  final SpeechToText _speech = SpeechToText();
  bool _listening = false;
  String _voiceBase = '';

  /// The session's working directory on the host, shown under the title.
  /// Fetched once from the sessions list; empty until it arrives.
  String _cwd = '';

  /// The CLI provider driving this session ("grok", "opencode"), shown before
  /// the cwd so it's always clear which agent is being controlled.
  String _provider = '';

  /// Local seq floor at screen open: items at or above it were appended while
  /// this screen was visible and get the entrance animation; anything below
  /// (history, kept transcript) must render instantly.
  int _openSeqFloor = 0;

  /// Session is not live on the host (or history was empty for a non-live row).
  /// Used with empty transcript to explain missing messages honestly (0009 B.1).
  bool _sessionLive = true;

  /// Dismissible note: host keeps history only while the session is live.
  bool _historyNoteVisible = false;

  /// Driven by [ChatScrollActivitySensor] around the transcript list so shimmer /
  /// pulse can pause while the user is dragging or flinging.
  final ValueNotifier<bool> _listScrolling = ValueNotifier(false);

  @override
  void initState() {
    super.initState();
    unawaited(_loadSessionCwd());
    // Tell the notifier we're watching this session, so it won't ping us about
    // events we're already seeing on screen. Captured so dispose() need not
    // touch `ref` after the scope is torn down.
    final coord = ref.read(notificationCoordinatorProvider);
    coord.currentSessionId = widget.sessionId;
    _notifCoord = coord;
    _scroll.addListener(_onScroll);
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    _openSeqFloor = transcript.nextSeq;
    // Reopening a populated chat must land at the live end. reverse:true lists
    // put the newest content at offset 0.
    if (transcript.items.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_scroll.hasClients) return;
        _scroll.jumpTo(0);
      });
    }
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

  /// After a reconnect, both the session status and any events missed during
  /// the outage are unknown. Re-sync status from `session.list` (a missed
  /// `turn_complete` would otherwise pin the composer on "running" forever)
  /// and reconcile the transcript against the daemon's history ring.
  Future<void> _resyncAfterReconnect() async {
    final client = ref.read(mcremoteClientProvider);
    try {
      final sessions = await client.listSessions();
      if (!mounted) return;
      ref.read(transcriptsProvider.notifier).syncFromMeta(sessions);
    } catch (_) {}
    try {
      final events = await client.sessionHistory(widget.sessionId);
      if (!mounted) return;
      if (events.isEmpty) {
        // After a reconnect the host ring may be gone (daemon restart). If we
        // also have nothing local, say so — not a silent blank chat.
        final t = ref.read(sessionTranscriptProvider(widget.sessionId));
        if (t.items.isEmpty) {
          setState(() => _historyNoteVisible = true);
        }
        return;
      }
      ref
          .read(transcriptsProvider.notifier)
          .resyncHistory(widget.sessionId, events);
    } catch (_) {}
  }

  /// Look up this session's provider and resolved working directory for the
  /// app bar; also tracks live-ness for the history-loss note.
  Future<void> _loadSessionCwd() async {
    try {
      final client = ref.read(mcremoteClientProvider);
      final sessions = await client.listSessions();
      final meta = sessions.where((s) => s.id == widget.sessionId).firstOrNull;
      final cwd = meta?.cwd ?? '';
      final provider = meta?.provider ?? '';
      final live = meta?.live ?? true;
      if (!mounted) return;
      setState(() {
        if (cwd.isNotEmpty) _cwd = cwd;
        if (provider.isNotEmpty) _provider = provider;
        _sessionLive = live;
        // Non-live + empty transcript: user is looking at a closed row; explain
        // why replay is empty before they blame the phone.
        if (!live) {
          final t = ref.read(sessionTranscriptProvider(widget.sessionId));
          if (t.items.isEmpty) _historyNoteVisible = true;
        }
      });
    } catch (_) {
      // Best-effort decoration; the chat works without it.
    }
  }

  /// Fetch and replay recorded history for an empty transcript, once per open.
  ///
  /// Guarded on emptiness at open time so re-entering a populated chat does not
  /// re-fetch. The notifier applies the result ONLY IF the transcript is still
  /// empty when the response lands — live events that raced in meanwhile are
  /// authoritative and win, so history is dropped rather than double-applied.
  ///
  /// Empty reply on a non-live session surfaces the B.1 honesty note (daemon
  /// ring is live-only; close/restart clears it). Brand-new live sessions stay
  /// quiet so "start typing" is not buried under a false-alarm banner.
  Future<void> _maybeReplayHistory() async {
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    if (transcript.items.isNotEmpty) return;
    final client = ref.read(mcremoteClientProvider);
    final events = await client.sessionHistory(widget.sessionId);
    if (!mounted) return;
    if (events.isEmpty) {
      if (!_sessionLive) {
        setState(() => _historyNoteVisible = true);
      }
      return;
    }
    ref
        .read(transcriptsProvider.notifier)
        .replayHistory(widget.sessionId, events);
  }

  @override
  void dispose() {
    // Only clear if we're still the current session (a fast push to another
    // chat may have already claimed it).
    if (_notifCoord?.currentSessionId == widget.sessionId) {
      _notifCoord?.currentSessionId = null;
    }
    if (_listening) unawaited(_speech.stop());
    _composer.dispose();
    _focus.dispose();
    _scroll.removeListener(_onScroll);
    _scroll.dispose();
    _listScrolling.dispose();
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
    // Daemon-handled built-ins are always offered (they are intercepted server
    // side); agent-advertised commands fill in the rest, minus any name a
    // built-in already owns.
    final builtinNames = _builtinCommands.map((c) => c.name).toSet();
    final merged = <AvailableCommand>[
      ..._builtinCommands,
      ...all.where((c) => !builtinNames.contains(c.name.toLowerCase())),
    ];
    return merged
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
    // reverse:true list — pixels ≈ 0 is the live (newest) end.
    final near = pos.pixels < 120;
    if (near != _userNearBottom) {
      // Crossing the threshold toggles the jump-to-latest pill.
      setState(() => _userNearBottom = near);
    }
  }

  bool _scrollQueued = false;

  /// Pin to the live end of a reverse ListView (offset 0). Coalesced to at most
  /// one jump per frame. With reverse:true, growth of the newest bubble usually
  /// stays pinned without jumping; this is still needed after appends when the
  /// user was near-bottom but not exactly at 0, and on chat-open.
  void _scrollToEnd() {
    if (_scrollQueued) return;
    _scrollQueued = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _scrollQueued = false;
      if (!_scroll.hasClients) return;
      _scroll.jumpTo(0);
    });
  }

  /// Toggle dictation into the composer. Best-effort: a missing recognizer or
  /// denied mic permission surfaces a snackbar rather than crashing.
  Future<void> _toggleVoice() async {
    if (_listening) {
      await _speech.stop();
      if (mounted) setState(() => _listening = false);
      return;
    }
    try {
      final available = await _speech.initialize(
        onStatus: (s) {
          if ((s == 'done' || s == 'notListening') && mounted && _listening) {
            setState(() => _listening = false);
          }
        },
        onError: (_) {
          if (mounted) setState(() => _listening = false);
        },
      );
      if (!available) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('Voice input unavailable')),
          );
        }
        return;
      }
      _voiceBase = _composer.text.trimRight();
      if (mounted) setState(() => _listening = true);
      await _speech.listen(
        onResult: (r) {
          // Plugin callbacks can land after dispose; the controller is gone.
          if (!mounted) return;
          final sep = _voiceBase.isEmpty ? '' : ' ';
          _composer.text = '$_voiceBase$sep${r.recognizedWords}';
          _composer.selection = TextSelection.collapsed(
            offset: _composer.text.length,
          );
        },
      );
    } catch (e) {
      if (mounted) {
        setState(() => _listening = false);
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Voice input failed: $e')));
      }
    }
  }

  /// Actions offered when long-pressing one of your own messages.
  Future<void> _userMessageActions(String text) async {
    final action = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.edit_outlined),
              title: const Text('Edit & resend'),
              onTap: () => Navigator.pop(ctx, 'edit'),
            ),
            ListTile(
              leading: const Icon(Icons.copy_outlined),
              title: const Text('Copy'),
              onTap: () => Navigator.pop(ctx, 'copy'),
            ),
          ],
        ),
      ),
    );
    if (!mounted || action == null) return;
    switch (action) {
      case 'edit':
        // Prefill the composer with the message so it can be tweaked and sent
        // as a fresh turn (no server-side branching required).
        _composer.text = text;
        _composer.selection = TextSelection.collapsed(
          offset: _composer.text.length,
        );
        _focus.requestFocus();
      case 'copy':
        await Clipboard.setData(ClipboardData(text: text));
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('Copied')));
        }
    }
  }

  Future<void> _send() async {
    final text = _composer.text.trim();
    if (text.isEmpty || _sending) return;
    if (_listening) {
      // Sending is a natural end to dictation.
      unawaited(_speech.stop());
      setState(() => _listening = false);
    }
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    final agentBusy =
        transcript.status == 'running' || transcript.hasPendingPermission;
    if (agentBusy) {
      // Mid-turn: queue it. [_maybeFlushQueue] sends it the moment the turn
      // completes and no permission decision is outstanding.
      setState(() => _queuedPrompts.add(text));
      _composer.clear();
      // Drop the keyboard so the transcript can use the full height while the
      // agent works; user re-taps the field to queue another prompt.
      _focus.unfocus();
      return;
    }
    _composer.clear();
    _focus.unfocus();
    await _sendText(text, restoreComposerOnFailure: true);
  }

  /// Deliver one prompt to the daemon. On failure either restores the composer
  /// text (direct sends) or re-queues at the front (queued sends), so a
  /// message is never silently lost.
  Future<void> _sendText(
    String text, {
    bool restoreComposerOnFailure = false,
    bool requeueOnFailure = false,
  }) async {
    setState(() => _sending = true);
    try {
      final client = ref.read(mcremoteClientProvider);
      await client.prompt(widget.sessionId, text);
      // Guard the async gap: backing out of the chat mid-request disposes
      // the controller and tears down this State.
      if (!mounted) return;
      _userNearBottom = true;
      _scrollToEnd();
    } catch (e) {
      if (mounted) {
        if (restoreComposerOnFailure && _composer.text.trim().isEmpty) {
          _composer.text = text;
          _composer.selection = TextSelection.collapsed(offset: text.length);
        }
        if (requeueOnFailure) {
          setState(() => _queuedPrompts.insert(0, text));
        }
        final msg = friendlyOpError(e);
        final code = e is McException ? e.code : null;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Send failed: $msg'),
            // Closed sessions are recovered from the sessions list (resume /
            // create-replace), not by retrying prompt on a dead id.
            action: code == 'session_not_live'
                ? SnackBarAction(
                    label: 'Sessions',
                    onPressed: () {
                      if (mounted) Navigator.of(context).pop();
                    },
                  )
                : null,
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  /// Send the oldest queued prompt once the agent is free. Runs post-frame so
  /// it never mutates state during a provider notification, and re-checks
  /// every condition after the frame in case a permission request or a new
  /// turn raced in.
  void _maybeFlushQueue(SessionTranscript t) {
    if (_queuedPrompts.isEmpty || _sending || _flushScheduled) return;
    if (t.status == 'running' || t.hasPendingPermission) return;
    if (ref.read(mcremoteClientProvider).state != McConnectionState.connected) {
      return;
    }
    _flushScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      _flushScheduled = false;
      if (!mounted || _queuedPrompts.isEmpty || _sending) return;
      final now = ref.read(sessionTranscriptProvider(widget.sessionId));
      if (now.status == 'running' || now.hasPendingPermission) return;
      if (ref.read(mcremoteClientProvider).state !=
          McConnectionState.connected) {
        return;
      }
      final text = _queuedPrompts.first;
      setState(() => _queuedPrompts.removeAt(0));
      await _sendText(text, requeueOnFailure: true);
    });
  }

  Future<void> _cancelTurn() async {
    try {
      // Nothing to stop on an idle session — announcing would latch the
      // cancel flag and print a bogus "Turn cancelled" line.
      final status = ref
          .read(sessionTranscriptProvider(widget.sessionId))
          .status;
      if (status != 'running') return;
      await ref.read(mcremoteClientProvider).cancel(widget.sessionId);
      if (!mounted) return;
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
            style: destructiveFilled(Theme.of(ctx).colorScheme),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('End session'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final client = ref.read(mcremoteClientProvider);
    // Ending a session is a host-side operation: claiming success while
    // offline would wipe the local transcript and change nothing on the host
    // (the row resurrects on the next refresh).
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text(
            'Reconnect to the host first — the session lives there.',
          ),
        ),
      );
      return;
    }
    try {
      try {
        await client.cancel(widget.sessionId);
      } catch (_) {}
      // session.delete closes the live session and purges the disk record.
      // closeSession alone leaves the record, so the row would reappear on
      // the next session.list — the dialog promises removal.
      await client.deleteSession(widget.sessionId);
      if (!mounted) return;
      // Clear local state only once the host actually deleted it.
      ref.read(transcriptsProvider.notifier).clearSession(widget.sessionId);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Session ended')));
      Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('End session failed: ${friendlyOpError(e)}')),
        );
      }
    }
  }

  /// Second confirmation for a broad "always" grant, so it can't be tapped by
  /// mistake as easily as a one-time allow.
  Future<bool> _confirmAlways(BuildContext ctx, String label) async {
    final ok = await showDialog<bool>(
      context: ctx,
      builder: (dctx) => AlertDialog(
        title: const Text('Allow always?'),
        content: Text(
          '“$label” will approve all matching actions in this session '
          'without asking again. Continue?',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dctx, true),
            child: const Text('Allow always'),
          ),
        ],
      ),
    );
    return ok ?? false;
  }

  Future<void> _showPermissionSheet(SessionEvent ev) async {
    _openSheetPermissionId = ev.permissionId;
    final result = await showModalBottomSheet<String>(
      context: context,
      isDismissible: false,
      enableDrag: false,
      // The approval decision must never hide its options below a fold: let
      // the sheet take the height its content needs (it scrolls internally
      // past ~90% of the screen).
      isScrollControlled: true,
      builder: (ctx) {
        // Registered so an externally resolved request (answered on another
        // device, turn cancelled) can retire its own stale sheet.
        _dismissSheet = () {
          if (ctx.mounted) Navigator.pop(ctx, '__external__');
        };
        final options = ev.options;
        final theme = Theme.of(ctx);
        final scheme = theme.colorScheme;
        final tokens = celestialOf(ctx);
        final tool = ev.toolName ?? 'Tool';
        // The daemon now enriches the request with the actual command/path;
        // show it (when it adds something over the title) so the user knows
        // exactly what they are approving.
        final detail = (ev.text ?? '').trim();
        final showDetail = detail.isNotEmpty && detail != ev.toolName;
        final sessionLabel = [
          if ((widget.sessionName ?? '').isNotEmpty) widget.sessionName!,
          if (_provider.isNotEmpty) _provider,
          if (_cwd.isNotEmpty) _cwd,
        ].join(' · ');
        return SafeArea(
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxHeight: MediaQuery.of(ctx).size.height * 0.9,
            ),
            child: SingleChildScrollView(
              key: const Key('permission-sheet-scroll'),
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Center(
                      child: Container(
                        width: 40,
                        height: 4,
                        decoration: BoxDecoration(
                          color: tokens.gold,
                          borderRadius: BorderRadius.circular(2),
                        ),
                      ),
                    ),
                    const SizedBox(height: 14),
                    Row(
                      children: [
                        Icon(Icons.shield_outlined, color: tokens.gold),
                        const SizedBox(width: 8),
                        Text(
                          'Approve action?',
                          style: theme.textTheme.titleLarge,
                        ),
                      ],
                    ),
                    if (sessionLabel.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        sessionLabel,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    const SizedBox(height: 12),
                    Text(
                      tool,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    if (showDetail) ...[
                      const SizedBox(height: 8),
                      Container(
                        width: double.infinity,
                        constraints: const BoxConstraints(maxHeight: 160),
                        padding: const EdgeInsets.all(10),
                        decoration: BoxDecoration(
                          color: scheme.brightness == Brightness.dark
                              ? scheme.surfaceContainerLowest
                              : scheme.surfaceContainerHighest,
                          borderRadius: BorderRadius.circular(10),
                          border: Border.all(
                            color: tokens.gold.withValues(alpha: 0.30),
                          ),
                          boxShadow: [
                            BoxShadow(
                              color: tokens.gold.withValues(alpha: 0.20),
                              blurRadius: 12,
                            ),
                          ],
                        ),
                        child: SingleChildScrollView(
                          child: SelectableText(detail, style: monoDetail),
                        ),
                      ),
                    ],
                    const SizedBox(height: 16),
                    if (options.isEmpty)
                      FilledButton(
                        onPressed: () => Navigator.pop(ctx, '__cancel__'),
                        child: const Text('Dismiss'),
                      )
                    else
                      ...options.map((o) {
                        final isAllow = isAllowOption(o);
                        final isAlways = isAlwaysOption(o);
                        final label = o.name.isEmpty ? o.optionId : o.name;
                        // "Always" grants are broad; make them deliberately harder
                        // (secondary styling + a second confirmation) than "once".
                        if (isAllow && isAlways) {
                          return Padding(
                            padding: const EdgeInsets.only(bottom: 8),
                            child: OutlinedButton.icon(
                              icon: Icon(
                                Icons.warning_amber_rounded,
                                size: 18,
                                color: tokens.gold,
                              ),
                              onPressed: () async {
                                final ok = await _confirmAlways(ctx, label);
                                if (ok && ctx.mounted) {
                                  Navigator.pop(ctx, o.optionId);
                                }
                              },
                              label: Text(label),
                            ),
                          );
                        }
                        return Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: isAllow
                              ? FilledButton(
                                  onPressed: () =>
                                      Navigator.pop(ctx, o.optionId),
                                  child: Text(label),
                                )
                              : OutlinedButton(
                                  onPressed: () =>
                                      Navigator.pop(ctx, o.optionId),
                                  child: Text(label),
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
            ),
          ),
        );
      },
    );
    _dismissSheet = null;
    _openSheetPermissionId = null;

    final permissionId = ev.permissionId;
    if (result == null || permissionId == null) return;
    if (result == '__external__') {
      // Resolved elsewhere (other device / cancelled turn): nothing to send.
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Request was resolved elsewhere')),
        );
      }
      return;
    }
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
    final sid = widget.sessionId;
    // Narrow selects: assistant text growth changes `items` identity but not
    // status/plan/commands/pending — so the shell (app bar, banners, composer)
    // must not rebuild on every stream chunk. The transcript pane watches items.
    final status = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.status),
    );
    final pendingCount = ref.watch(
      sessionTranscriptProvider(
        sid,
      ).select((t) => t.pendingPermissions.length),
    );
    final pendingToolName = ref.watch(
      sessionTranscriptProvider(sid).select(
        (t) => t.pendingPermission?.toolName ?? '',
      ),
    );
    final hasPending = pendingCount > 0;
    final commands = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.commands),
    );
    final plan = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.plan),
    );
    final hasItems = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.items.isNotEmpty),
    );

    ref.listen(sessionTranscriptProvider(sid), (prev, next) {
      // reverse:true keeps offset 0 pinned while the newest bubble grows, so
      // we only force a jump when a *new* row appears (tool card, user message,
      // fresh assistant bubble) and the user was already following the live end.
      // Chasing maxScrollExtent on every text chunk was the main scroll jitter.
      final appended =
          next.items.isNotEmpty &&
          next.items.last.seq !=
              (prev?.items.isNotEmpty ?? false ? prev!.items.last.seq : -1);
      if (appended && _userNearBottom) {
        _scrollToEnd();
      }
      // A sheet whose request was resolved elsewhere must not keep inviting
      // an approval that can no longer be applied.
      final openId = _openSheetPermissionId;
      if (openId != null && !next.pendingPermissions.containsKey(openId)) {
        _dismissSheet?.call();
      }
      _maybeShowPermission(next);
      _maybeFlushQueue(next);
    });

    // A regained connection may have swallowed `turn_complete` (composer
    // stuck on running) and any number of streamed events: resync both.
    ref.listen(connectionStateProvider, (prev, next) {
      final was = prev?.asData?.value;
      final now = next.asData?.value;
      if (now == McConnectionState.connected &&
          was != null &&
          was != McConnectionState.connected) {
        unawaited(_resyncAfterReconnect());
        // Anything queued during the outage can go out once the resync settles.
        _maybeFlushQueue(ref.read(sessionTranscriptProvider(sid)));
      }
    });

    final busy = _sending || status == 'running' || hasPending;

    final title = (widget.sessionName != null && widget.sessionName!.isNotEmpty)
        ? widget.sessionName!
        : (widget.sessionId.length > 8
              ? 'Session ${widget.sessionId.substring(0, 8)}'
              : widget.sessionId);

    final conn = ref.watch(connectionStateProvider);
    final connState = conn.asData?.value;
    // Distinguish "definitely down" from "an attempt is in flight": showing
    // the red Disconnected banner (with a Retry button) during an initial
    // connect both alarms and invites a redundant second attempt.
    final linking =
        connState == McConnectionState.reconnecting ||
        connState == McConnectionState.connecting ||
        connState == McConnectionState.authenticating;
    final offline =
        connState != null &&
        connState != McConnectionState.connected &&
        !linking;

    return Scaffold(
      appBar: AppBar(
        title: (_cwd.isEmpty && _provider.isEmpty)
            ? Text(title)
            : Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(title, overflow: TextOverflow.ellipsis),
                  // "grok /home/mac" — which CLI this session drives, then
                  // where. The provider is tinted so it reads as a label,
                  // not part of the path.
                  Text.rich(
                    TextSpan(
                      children: [
                        if (_provider.isNotEmpty)
                          TextSpan(
                            text: _cwd.isEmpty ? _provider : '$_provider ',
                            style: TextStyle(
                              color: Theme.of(context).colorScheme.primary,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                        if (_cwd.isNotEmpty) TextSpan(text: _cwd),
                      ],
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
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
            child: Center(child: StatusChip(status: status)),
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
          BannerSlot(
            child: linking
                ? const ConnBanner(
                    key: ValueKey('linking'),
                    kind: ConnBannerKind.linking,
                    message: 'Connecting to host…',
                  )
                : offline
                ? ConnBanner(
                    key: const ValueKey('offline'),
                    kind: ConnBannerKind.offline,
                    message: 'Disconnected',
                    trailing: TextButton(
                      onPressed: () async {
                        // Resolve the messenger before the await so we never
                        // touch a stale BuildContext afterwards.
                        final messenger = ScaffoldMessenger.of(context);
                        try {
                          final store = ref.read(settingsStoreProvider);
                          await ref
                              .read(mcremoteClientProvider)
                              .reconnectFromStore(store);
                        } catch (e) {
                          messenger.showSnackBar(
                            SnackBar(
                              content: Text(
                                'Reconnect failed: ${friendlyOpError(e)}',
                              ),
                            ),
                          );
                        }
                      },
                      child: const Text('Retry now'),
                    ),
                  )
                : null,
          ),
          // Live-only ring: empty after close/restart is expected, not a bug.
          if (_historyNoteVisible && !hasItems)
            MaterialBanner(
              key: const Key('history-unavailable-banner'),
              leading: Icon(
                Icons.history_toggle_off,
                color: Theme.of(context).colorScheme.primary,
              ),
              content: const Text(
                'No earlier messages on this host for this session '
                '(new chat, or the host never stored a transcript).',
              ),
              actions: [
                TextButton(
                  onPressed: () => setState(() => _historyNoteVisible = false),
                  child: const Text('Got it'),
                ),
              ],
            ),
          if (hasPending)
            MaterialBanner(
              leading: Icon(
                Icons.shield_outlined,
                color: celestialOf(context).gold,
              ),
              content: Text(
                pendingCount > 1
                    ? 'Waiting for $pendingCount permissions: '
                          '${pendingToolName.isEmpty ? 'tool' : pendingToolName} '
                          'and ${pendingCount - 1} more'
                    : 'Waiting for permission: '
                          '${pendingToolName.isEmpty ? 'tool' : pendingToolName}',
              ),
              actions: [
                TextButton(
                  onPressed: () {
                    // Allow re-presenting after a dismissal or failed send.
                    _presentedPermissionIds.clear();
                    _maybeShowPermission(
                      ref.read(sessionTranscriptProvider(sid)),
                    );
                  },
                  child: const Text('Review'),
                ),
              ],
            ),
          Expanded(
            child: Stack(
              children: [
                if (!hasItems) ...[
                  const Positioned.fill(child: CelestialBackdrop()),
                  Center(
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 32),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(
                            Icons.auto_awesome,
                            size: 40,
                            color: Theme.of(
                              context,
                            ).colorScheme.primary.withValues(alpha: 0.7),
                          ),
                          const SizedBox(height: 12),
                          Text(
                            'Send a prompt or type / for slash commands',
                            textAlign: TextAlign.center,
                            style: Theme.of(context).textTheme.bodyLarge
                                ?.copyWith(
                                  color: Theme.of(
                                    context,
                                  ).colorScheme.onSurfaceVariant,
                                ),
                          ),
                          const SizedBox(height: 8),
                          Text(
                            'Recent chat is kept on this host until you end '
                            'the session permanently.',
                            textAlign: TextAlign.center,
                            style: Theme.of(context).textTheme.bodySmall
                                ?.copyWith(
                                  color: Theme.of(context)
                                      .colorScheme
                                      .onSurfaceVariant
                                      .withValues(alpha: 0.85),
                                ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ] else
                  // Isolated consumer: rebuilds on items/status only, not on
                  // shell-local setState (composer queue chips, listening, …).
                  ChatScrollActivitySensor(
                    scrolling: _listScrolling,
                    child: _TranscriptPane(
                      sessionId: sid,
                      scrollController: _scroll,
                      openSeqFloor: _openSeqFloor,
                      onUserAction: _userMessageActions,
                    ),
                  ),
                if (!_userNearBottom && hasItems)
                  Positioned(
                    right: 12,
                    bottom: 12,
                    child: FloatingActionButton.small(
                      heroTag: 'jump-to-latest',
                      tooltip: 'Jump to latest',
                      onPressed: () {
                        _focus.unfocus();
                        _userNearBottom = true;
                        _scrollToEnd();
                      },
                      child: const Icon(Icons.arrow_downward),
                    ),
                  ),
              ],
            ),
          ),
          // Compact, collapsible plan panel above the composer. Kept out of the
          // scrolling transcript; hidden entirely when the plan is empty.
          if (plan.isNotEmpty) _PlanPanel(entries: plan),
          // Slash-command autocomplete. The persistent chip toolbar was
          // removed; commands stay reachable via the composer's terminal button
          // and by typing '/', which surfaces this list. Scoped to the
          // composer's value so typing rebuilds only this, not the transcript.
          ValueListenableBuilder<TextEditingValue>(
            valueListenable: _composer,
            builder: (ctx, value, _) {
              final matches = _matchingCommands(commands, value.text);
              if (matches.isEmpty) {
                return const SizedBox.shrink();
              }
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
            },
          ),
          if (_queuedPrompts.isNotEmpty)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 0),
              child: Wrap(
                spacing: 6,
                runSpacing: -6,
                children: [
                  for (var i = 0; i < _queuedPrompts.length; i++)
                    InputChip(
                      avatar: const Icon(Icons.schedule_send, size: 16),
                      label: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 200),
                        child: Text(
                          _queuedPrompts[i],
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      visualDensity: VisualDensity.compact,
                      deleteButtonTooltipMessage: 'Remove queued message',
                      onDeleted: () =>
                          setState(() => _queuedPrompts.removeAt(i)),
                    ),
                ],
              ),
            ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _composer,
                      focusNode: _focus,
                      minLines: 1,
                      maxLines: 5,
                      // Stay editable while the agent works so the next prompt
                      // can be queued; only direct sending is gated. Disabling
                      // also stole focus and dismissed the keyboard mid-turn.
                      enabled: !offline,
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _send(),
                      // Tap outside the field (list, app bar, etc.) drops the
                      // soft keyboard so the transcript regains height.
                      onTapOutside: (_) => _focus.unfocus(),
                      decoration: InputDecoration(
                        hintText: offline
                            ? 'Disconnected'
                            : busy
                            ? 'Queue a message'
                            : 'Prompt or /command…',
                        isDense: true,
                        prefixIcon: IconButton(
                          tooltip: 'Slash commands',
                          icon: const Icon(Icons.terminal, size: 20),
                          onPressed: offline
                              ? null
                              : () {
                                  _composer.text = '/';
                                  _composer.selection =
                                      const TextSelection.collapsed(offset: 1);
                                  _focus.requestFocus();
                                },
                        ),
                        suffixIcon: IconButton(
                          tooltip: _listening ? 'Stop dictation' : 'Dictate',
                          icon: Icon(
                            _listening ? Icons.mic : Icons.mic_none,
                            size: 20,
                            color: _listening
                                ? Theme.of(context).colorScheme.error
                                : null,
                          ),
                          // While listening the button must stay live no
                          // matter what — it is the only way to stop
                          // dictation.
                          onPressed: _listening
                              ? _toggleVoice
                              : (busy || offline ? null : _toggleVoice),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  // Three states: idle → send, busy with a drafted message →
                  // queue it, busy with an empty composer → stop the turn
                  // (stop stays reachable from the app bar in every state).
                  ValueListenableBuilder<TextEditingValue>(
                    valueListenable: _composer,
                    builder: (ctx, value, _) {
                      final hasDraft = value.text.trim().isNotEmpty;
                      final Widget button;
                      if (busy && hasDraft) {
                        button = IconButton.filled(
                          key: const ValueKey('queue'),
                          tooltip: 'Queue message',
                          onPressed: offline ? null : _send,
                          icon: const Icon(Icons.schedule_send),
                        );
                      } else if (busy) {
                        button = IconButton.filled(
                          key: const ValueKey('stop'),
                          style: IconButton.styleFrom(
                            backgroundColor: Theme.of(
                              context,
                            ).colorScheme.error,
                            foregroundColor: Theme.of(
                              context,
                            ).colorScheme.onError,
                          ),
                          tooltip: 'Stop turn',
                          onPressed: _cancelTurn,
                          icon: const Icon(Icons.stop),
                        );
                      } else {
                        button = IconButton.filled(
                          key: const ValueKey('send'),
                          onPressed: offline ? null : _send,
                          icon: const Icon(Icons.send),
                        );
                      }
                      return AnimatedSwitcher(
                        duration: const Duration(milliseconds: 150),
                        transitionBuilder: (child, anim) => ScaleTransition(
                          scale: Tween(begin: 0.9, end: 1.0).animate(anim),
                          child: FadeTransition(opacity: anim, child: child),
                        ),
                        child: button,
                      );
                    },
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

/// Index of the last item of [kind], or -1.
int _lastIndexOfKind(List<ChatItem> items, ChatItemKind kind) {
  for (var i = items.length - 1; i >= 0; i--) {
    if (items[i].kind == kind) return i;
  }
  return -1;
}

/// Memo of [buildTranscriptRows]: skip O(n) fold when only the last item's
/// text grew (the common streaming path).
List<TranscriptRow> _memoTranscriptRows(
  List<ChatItem> items,
  List<ChatItem>? prevSource,
  List<TranscriptRow>? prevRows,
) {
  if (identical(items, prevSource) && prevRows != null) return prevRows;
  if (prevSource != null &&
      prevRows != null &&
      items.length == prevSource.length &&
      items.isNotEmpty) {
    final n = items.length;
    var prefixSame = true;
    for (var i = 0; i < n - 1; i++) {
      if (!identical(items[i], prevSource[i])) {
        prefixSame = false;
        break;
      }
    }
    if (prefixSame) {
      final oldLast = prevSource[n - 1];
      final newLast = items[n - 1];
      if (oldLast.kind == newLast.kind &&
          newLast.kind != ChatItemKind.tool &&
          prevRows.isNotEmpty &&
          prevRows.last is SingleRow) {
        final rows = List<TranscriptRow>.of(prevRows);
        final last = rows.last as SingleRow;
        rows[rows.length - 1] = SingleRow(newLast, last.index);
        return rows;
      }
    }
  }
  return buildTranscriptRows(items);
}

/// Transcript list only — watches [items] + [status] so stream chunks do not
/// rebuild the chat shell (app bar, banners, composer).
class _TranscriptPane extends ConsumerStatefulWidget {
  const _TranscriptPane({
    required this.sessionId,
    required this.scrollController,
    required this.openSeqFloor,
    required this.onUserAction,
  });

  final String sessionId;
  final ScrollController scrollController;
  final int openSeqFloor;
  final void Function(String text) onUserAction;

  @override
  ConsumerState<_TranscriptPane> createState() => _TranscriptPaneState();
}

class _TranscriptPaneState extends ConsumerState<_TranscriptPane> {
  List<ChatItem>? _rowSource;
  List<TranscriptRow>? _rowCache;

  @override
  Widget build(BuildContext context) {
    final items = ref.watch(
      sessionTranscriptProvider(widget.sessionId).select((t) => t.items),
    );
    final status = ref.watch(
      sessionTranscriptProvider(widget.sessionId).select((t) => t.status),
    );
    final rows = _memoTranscriptRows(items, _rowSource, _rowCache);
    _rowSource = items;
    _rowCache = rows;

    return LayoutBuilder(
      builder: (ctx, constraints) {
        final maxUserW = constraints.maxWidth * 0.85;
        final maxAssistantW = constraints.maxWidth * 0.9;
        final lastAssistantIdx = _lastIndexOfKind(
          items,
          ChatItemKind.assistant,
        );
        final lastIdx = items.length - 1;
        final running = status == 'running';

        return ListView.builder(
          controller: widget.scrollController,
          reverse: true,
          scrollCacheExtent: const ScrollCacheExtent.pixels(400),
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          // reverse:true inverts padding: top becomes the visual bottom inset.
          padding: const EdgeInsets.fromLTRB(12, 28, 12, 12),
          itemCount: rows.length,
          // Preserve Element state when rows reorder under reverse+append.
          findChildIndexCallback: (Key key) {
            if (key is! ValueKey) return null;
            final v = key.value;
            if (v is int) {
              for (var ri = 0; ri < rows.length; ri++) {
                final row = rows[ri];
                if (row is SingleRow && row.item.seq == v) {
                  return rows.length - 1 - ri;
                }
              }
            } else if (v is String && v.startsWith('grp-')) {
              final seq = int.tryParse(v.substring(4));
              if (seq == null) return null;
              for (var ri = 0; ri < rows.length; ri++) {
                final row = rows[ri];
                if (row is GroupRow && row.items.first.seq == seq) {
                  return rows.length - 1 - ri;
                }
              }
            }
            return null;
          },
          itemBuilder: (ctx, i) {
            final row = rows[rows.length - 1 - i];
            if (row is GroupRow) {
              return RepaintBoundary(
                child: EntranceFade(
                  key: ValueKey('grp-${row.items.first.seq}'),
                  animate: row.items.first.seq >= widget.openSeqFloor,
                  child: _ToolGroupTile(group: row),
                ),
              );
            }
            final single = row as SingleRow;
            final item = single.item;
            return RepaintBoundary(
              child: EntranceFade(
                key: ValueKey(item.seq),
                animate: item.seq >= widget.openSeqFloor,
                child: _ChatBubble(
                  item: item,
                  maxUserWidth: maxUserW,
                  maxAssistantWidth: maxAssistantW,
                  agentRunning: running && single.index == lastIdx,
                  streamingText:
                      running &&
                      item.kind == ChatItemKind.assistant &&
                      single.index == lastAssistantIdx,
                  onUserAction: widget.onUserAction,
                ),
              ),
            );
          },
        );
      },
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

  Color _colorFor(String status, BuildContext context) {
    final tokens = celestialOf(context);
    switch (status) {
      case 'completed':
        return tokens.success;
      case 'in_progress':
        // Activity blue — teal stays reserved for connectivity states.
        return tokens.running;
      default:
        return Theme.of(context).colorScheme.onSurfaceVariant;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final tokens = celestialOf(context);
    final done = entries.where((e) => e.status == 'completed').length;
    final inProgress = entries
        .where((e) => e.status == 'in_progress')
        .firstOrNull;
    // The single most useful line: what the agent is doing right now.
    final subtitle = inProgress != null
        ? '$done/${entries.length} · ${inProgress.content}'
        : '$done/${entries.length} done';
    return Material(
      color: scheme.surfaceContainerLow,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Divider(height: 1, color: scheme.outlineVariant),
          LinearProgressIndicator(
            value: entries.isEmpty ? 0 : done / entries.length,
            minHeight: 2,
            color: tokens.success,
            backgroundColor: scheme.outlineVariant,
          ),
          Theme(
            // ExpansionTile draws divider lines above and below when placed in
            // a Column; suppress them so the panel reads as one block.
            data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
            child: ExpansionTile(
              dense: true,
              tilePadding: const EdgeInsets.symmetric(horizontal: 12),
              leading: const Icon(Icons.checklist, size: 20),
              title: Text(
                'Plan',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              subtitle: Text(
                subtitle,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
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
                          color: _colorFor(e.status, ctx),
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
                        trailing: e.priority == 'high'
                            ? Icon(
                                Icons.priority_high,
                                size: 14,
                                color: tokens.gold,
                              )
                            : null,
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Collapsed summary row for a run of finished agent actions of one class —
/// "Ran 5 commands", "Edited 3 files", "Used 4 tools". Expanding reveals the
/// individual action tiles, each of which can be opened further for its
/// command/output detail. Success stays quiet; failures surface on the
/// collapsed row so they can't hide inside a folded group.
class _ToolGroupTile extends StatelessWidget {
  const _ToolGroupTile({required this.group});

  final GroupRow group;

  IconData get _icon => switch (group.toolClass) {
    ToolClass.command => Icons.terminal,
    ToolClass.fileEdit => Icons.edit_note,
    ToolClass.other => Icons.handyman_outlined,
  };

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = celestialOf(context);
    final muted = scheme.onSurfaceVariant;
    final failed = group.failedCount;
    final rail = failed > 0
        ? scheme.error
        : tokens.success.withValues(alpha: 0.6);
    return Stack(
      children: [
        Positioned(
          left: 0,
          top: 4,
          bottom: 4,
          child: Container(
            width: 2,
            decoration: BoxDecoration(
              color: rail,
              borderRadius: BorderRadius.circular(1),
            ),
          ),
        ),
        Padding(
          padding: const EdgeInsets.only(left: 6),
          child: Theme(
            // Suppress ExpansionTile's divider lines so the row reads as one
            // quiet status, matching _CompactStatusTile.
            data: theme.copyWith(dividerColor: Colors.transparent),
            child: ExpansionTile(
              dense: true,
              initiallyExpanded: false,
              tilePadding: const EdgeInsets.symmetric(horizontal: 4),
              minTileHeight: 0,
              visualDensity: VisualDensity.compact,
              childrenPadding: const EdgeInsets.only(left: 12, bottom: 4),
              expandedCrossAxisAlignment: CrossAxisAlignment.stretch,
              leading: SizedBox(
                width: 20,
                child: Center(child: Icon(_icon, size: 16, color: muted)),
              ),
              title: Row(
                children: [
                  Flexible(
                    child: Text(
                      group.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(color: muted),
                    ),
                  ),
                  if (failed > 0) ...[
                    const SizedBox(width: 6),
                    Text(
                      failed == 1 ? '1 FAILED' : '$failed FAILED',
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: scheme.error,
                      ),
                    ),
                  ],
                ],
              ),
              children: [
                for (final item in group.items)
                  _ChatBubble(item: item, agentRunning: false),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _ChatBubble extends StatelessWidget {
  const _ChatBubble({
    required this.item,
    required this.agentRunning,
    this.streamingText = false,
    this.maxUserWidth,
    this.maxAssistantWidth,
    this.onUserAction,
  });

  final ChatItem item;
  final bool agentRunning;

  /// True when this assistant bubble belongs to the still-open turn and may
  /// still receive chunks — even when a tool card has been appended after it.
  /// Rendering it as "final" mid-turn would flash a half-open code fence as
  /// raw markdown.
  final bool streamingText;

  /// Caps from the list [LayoutBuilder] so bubbles do not each depend on
  /// [MediaQuery] (keyboard open/close would otherwise thrash the list).
  final double? maxUserWidth;
  final double? maxAssistantWidth;

  /// Long-press on a user message → edit-and-resend / copy sheet.
  final void Function(String text)? onUserAction;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final tokens = celestialOf(context);
    switch (item.kind) {
      case ChatItemKind.user:
        final text = item.text ?? '';
        final maxW =
            maxUserWidth ?? MediaQuery.sizeOf(context).width * 0.85;
        return Align(
          alignment: Alignment.centerRight,
          child: GestureDetector(
            onLongPress: onUserAction == null
                ? null
                : () => onUserAction!(text),
            child: Container(
              margin: const EdgeInsets.symmetric(vertical: 4),
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              constraints: BoxConstraints(maxWidth: maxW),
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [tokens.userBubbleGradA, tokens.userBubbleGradB],
                ),
                borderRadius: const BorderRadius.only(
                  topLeft: Radius.circular(18),
                  topRight: Radius.circular(18),
                  bottomLeft: Radius.circular(18),
                  bottomRight: Radius.circular(6),
                ),
              ),
              child: Text(text, style: TextStyle(color: tokens.onUserBubble)),
            ),
          ),
        );
      case ChatItemKind.assistant:
        final maxW =
            maxAssistantWidth ?? MediaQuery.sizeOf(context).width * 0.9;
        return Align(
          alignment: Alignment.centerLeft,
          child: GestureDetector(
            onLongPress: () async {
              await Clipboard.setData(ClipboardData(text: item.text ?? ''));
              if (context.mounted) {
                ScaffoldMessenger.of(
                  context,
                ).showSnackBar(const SnackBar(content: Text('Copied reply')));
              }
            },
            child: Container(
              margin: const EdgeInsets.symmetric(vertical: 4),
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              constraints: BoxConstraints(maxWidth: maxW),
              decoration: BoxDecoration(
                color: scheme.surfaceContainer,
                borderRadius: const BorderRadius.only(
                  topLeft: Radius.circular(18),
                  topRight: Radius.circular(18),
                  bottomLeft: Radius.circular(6),
                  bottomRight: Radius.circular(18),
                ),
                border: scheme.brightness == Brightness.light
                    ? Border.all(color: scheme.outlineVariant)
                    : null,
              ),
              child: _AssistantMarkdown(
                data: item.text ?? '',
                streaming: streamingText,
              ),
            ),
          ),
        );
      case ChatItemKind.thought:
        // Terse, collapsed by default: a quiet one-line status the reader can
        // open only if they care what the agent was reasoning about.
        return _CompactStatusTile(
          leading: Icon(
            Icons.psychology_outlined,
            size: 16,
            color: tokens.thoughtIcon,
          ),
          title: agentRunning ? 'Thinking…' : 'Thought',
          shimmerTitle: agentRunning,
          railColor: tokens.thoughtIcon.withValues(alpha: 0.4),
          detail: item.text ?? '',
          detailAsMarkdown: false,
        );
      case ChatItemKind.tool:
        final status = item.toolStatus ?? '';
        final detail = (item.text ?? '').trim();
        final running = status == 'running' || status == 'pending';
        final failed = status == 'failed' || status == 'error';
        final done = status == 'completed' || status == 'success';
        // One terse line: icon + tool name + a muted status suffix. The detail
        // (command/output summary) is hidden until the row is expanded, so a
        // burst of tool calls no longer floods the transcript.
        // Static icon while running (no CircularProgressIndicator tick) — the
        // app-bar status chip already signals "agent working"; a spinning tool
        // row fought the list for frames during multi-tool turns.
        return _CompactStatusTile(
          leading: Icon(
            running
                ? Icons.autorenew
                : done
                ? Icons.check_circle_outline
                : failed
                ? Icons.error_outline
                : Icons.build_circle_outlined,
            size: 16,
            color: failed
                ? scheme.error
                : running
                ? scheme.tertiary
                : done
                ? tokens.success
                : scheme.onSurfaceVariant,
          ),
          title: item.toolName ?? 'Tool',
          titleSuffix: status.isEmpty ? null : status,
          railColor: failed
              ? scheme.error
              : running
              ? scheme.tertiary
              : done
              ? tokens.success.withValues(alpha: 0.6)
              : scheme.outlineVariant,
          detail: detail == item.toolName ? '' : detail,
          detailAsMarkdown: false,
        );
      case ChatItemKind.system:
        final text = item.text ?? '';
        // Quota / rate-limit hits get a proper card with reset guidance, not
        // a raw red provider dump.
        if (item.isLimitError) {
          return _LimitNotice(item: item);
        }
        // Explicit flag first; legacy items from before the flag existed
        // carried an "Error:" prefix.
        final isError = item.isError || text.startsWith('Error:');
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

/// Card shown when the agent hits a usage quota or rate limit. Leads with
/// what happened and when it resets (when the provider said), keeps the raw
/// provider message as fine print, and points at the practical outs.
class _LimitNotice extends StatelessWidget {
  const _LimitNotice({required this.item});

  final ChatItem item;

  /// "3:45 PM (in about 2 h)" — or "now — try again" once it has passed.
  static String resetPhrase(BuildContext context, DateTime resetAt) {
    final now = DateTime.now();
    final local = resetAt.toLocal();
    final d = local.difference(now);
    if (d.isNegative) return 'now — try again';
    final clock = TimeOfDay.fromDateTime(local).format(context);
    final String rel;
    if (d.inMinutes < 1) {
      rel = 'under a minute';
    } else if (d.inHours < 1) {
      rel = 'about ${d.inMinutes} min';
    } else if (d.inHours < 24) {
      final m = d.inMinutes % 60;
      rel = m == 0 ? 'about ${d.inHours} h' : 'about ${d.inHours} h $m min';
    } else {
      rel = 'about ${d.inDays} d ${d.inHours % 24} h';
    }
    final sameDay =
        local.year == now.year &&
        local.month == now.month &&
        local.day == now.day;
    return sameDay ? '$clock (in $rel)' : 'tomorrow $clock (in $rel)';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = celestialOf(context);
    final isQuota = item.errorKind == 'quota';
    final title = isQuota ? 'Agent quota exceeded' : 'Agent rate-limited';
    final retryAt = item.retryAt;

    final String body;
    if (retryAt != null) {
      body = 'The limit resets at ${resetPhrase(context, retryAt)}.';
    } else if (isQuota) {
      body =
          'The agent’s usage limit has been reached. Wait for it to '
          'reset, or switch models with /model.';
    } else {
      body = 'Too many requests right now. Give it a moment and try again.';
    }

    final detail = (item.text ?? '').trim();
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Center(
        child: Container(
          constraints: const BoxConstraints(maxWidth: 420),
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: tokens.gold.withValues(alpha: 0.08),
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: tokens.gold.withValues(alpha: 0.45)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  Icon(
                    isQuota ? Icons.hourglass_bottom : Icons.speed,
                    size: 18,
                    color: tokens.gold,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      title,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Text(body, style: theme.textTheme.bodyMedium),
              if (detail.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  detail,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: monoDetail.copyWith(
                    fontSize: 11,
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// Assistant text rendered as markdown so headers, emphasis, lists, and fenced
/// code read as intended instead of leaking raw `**`, `#`, and backticks into
/// the chat. Selectable, and sized to the surrounding body text.
/// Assistant reply rendered as markdown, with two optimisations that keep large
/// streaming replies smooth on a phone:
///
///  * The parsed markdown widget is **cached** and only rebuilt when the shown
///    text actually changes. The transcript list rebuilds on every stream
///    chunk; without this, the whole (growing) message re-parses each frame,
///    which is the flicker on large outputs.
///  * While the reply is still streaming, shown-text updates are **throttled**
///    to a few times a second rather than once per chunk.
///
/// The State survives per-chunk rebuilds because the enclosing [_ChatBubble]
/// carries a stable `ValueKey(seq)`.
class _AssistantMarkdown extends StatefulWidget {
  const _AssistantMarkdown({required this.data, required this.streaming});

  final String data;
  final bool streaming;

  @override
  State<_AssistantMarkdown> createState() => _AssistantMarkdownState();
}

class _AssistantMarkdownState extends State<_AssistantMarkdown> {
  static const _throttleDefault = Duration(milliseconds: 120);
  static const _throttleLarge = Duration(milliseconds: 200);
  static const _largeTextChars = 4000;

  late String _shown = widget.data;
  late bool _shownStreaming = widget.streaming;
  Widget? _built;
  Timer? _timer;
  MarkdownStyleSheet? _styleSheet;
  Brightness? _styleBrightness;

  Duration get _throttle => widget.data.length > _largeTextChars
      ? _throttleLarge
      : _throttleDefault;

  MarkdownStyleSheet _sheetFor(BuildContext context) {
    final theme = Theme.of(context);
    final brightness = theme.brightness;
    final cached = _styleSheet;
    if (cached != null && _styleBrightness == brightness) return cached;
    final base = theme.textTheme.bodyMedium;
    final mono = base?.copyWith(fontFamily: 'monospace', fontSize: 13);
    final codeBg = theme.colorScheme.surfaceContainerHigh;
    final sheet = MarkdownStyleSheet.fromTheme(theme).copyWith(
      p: base,
      pPadding: EdgeInsets.zero,
      listBullet: base,
      blockSpacing: 8,
      h1: theme.textTheme.titleLarge,
      h2: theme.textTheme.titleMedium,
      h3: theme.textTheme.titleSmall,
      code: mono?.copyWith(backgroundColor: codeBg),
      codeblockDecoration: BoxDecoration(
        color: codeBg,
        borderRadius: BorderRadius.circular(8),
      ),
      codeblockPadding: const EdgeInsets.all(10),
      a: TextStyle(color: theme.colorScheme.primary),
      blockquotePadding: const EdgeInsets.fromLTRB(12, 4, 12, 4),
      blockquoteDecoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(8),
        border: Border(
          left: BorderSide(
            color: theme.colorScheme.primary.withValues(alpha: 0.5),
            width: 3,
          ),
        ),
      ),
    );
    _styleSheet = sheet;
    _styleBrightness = brightness;
    return sheet;
  }

  // Build the markdown subtree and record what it was built from. While
  // streaming, trailing not-yet-closed markdown is hidden so raw markers never
  // flash; the full text renders once the turn completes. Selection is off
  // while streaming (cheaper paint) and on once final — long-press still
  // copies the full reply from the bubble.
  Widget _render(BuildContext context, String text, bool streaming) {
    _shown = text;
    _shownStreaming = streaming;
    final shown = streaming ? bufferStreamingMarkdown(text) : text;
    return MarkdownBody(
      data: shown,
      selectable: !streaming,
      styleSheet: _sheetFor(context),
    );
  }

  // The current subtree is already current if nothing that affects it changed.
  bool _upToDate(bool streaming) =>
      _shown == widget.data && _shownStreaming == streaming;

  @override
  void didUpdateWidget(covariant _AssistantMarkdown old) {
    super.didUpdateWidget(old);
    if (!widget.streaming) {
      // Finalised (turn ended, or a completed non-live bubble): cancel any
      // pending throttle and show the complete text. The _upToDate guard keeps
      // completed bubbles from re-parsing on every unrelated parent rebuild.
      _timer?.cancel();
      _timer = null;
      if (!_upToDate(false)) {
        setState(() => _built = _render(context, widget.data, false));
      }
      return;
    }
    if (_upToDate(true)) return;
    // Streaming: coalesce re-parses to at most one per throttle window.
    _timer ??= Timer(_throttle, () {
      _timer = null;
      if (mounted && !_upToDate(true)) {
        setState(() => _built = _render(context, widget.data, true));
      }
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  // Returns the cached, already-parsed subtree. Identical across parent
  // rebuilds, so the framework skips re-parsing until [_render] swaps it.
  @override
  Widget build(BuildContext context) {
    // Theme flip (light/dark) must rebuild the stylesheet + markdown body.
    final brightness = Theme.of(context).brightness;
    if (_built == null ||
        (_styleBrightness != null && _styleBrightness != brightness)) {
      _styleSheet = null;
      _built = _render(context, widget.data, widget.streaming);
    }
    return _built!;
  }
}

/// Simple markdown body for expanded tool/thought detail (not the streaming
/// hot path). Assistant bubbles use [_AssistantMarkdown] instead.
class _MarkdownText extends StatelessWidget {
  const _MarkdownText({required this.data});

  final String data;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final base = theme.textTheme.bodyMedium;
    final mono = base?.copyWith(fontFamily: 'monospace', fontSize: 13);
    final codeBg = theme.colorScheme.surfaceContainerHigh;
    return MarkdownBody(
      data: data,
      selectable: true,
      styleSheet: MarkdownStyleSheet.fromTheme(theme).copyWith(
        p: base,
        pPadding: EdgeInsets.zero,
        listBullet: base,
        blockSpacing: 8,
        h1: theme.textTheme.titleLarge,
        h2: theme.textTheme.titleMedium,
        h3: theme.textTheme.titleSmall,
        code: mono?.copyWith(backgroundColor: codeBg),
        codeblockDecoration: BoxDecoration(
          color: codeBg,
          borderRadius: BorderRadius.circular(8),
        ),
        codeblockPadding: const EdgeInsets.all(10),
        a: TextStyle(color: theme.colorScheme.primary),
      ),
    );
  }
}

/// A quiet, single-line status row (agent thinking / tool call) that stays
/// minimized by default. When there is nothing to expand it renders as a plain
/// dense row with no disclosure arrow; when [detail] is present it becomes a
/// collapsed [ExpansionTile] the reader can open on demand.
class _CompactStatusTile extends StatelessWidget {
  const _CompactStatusTile({
    required this.leading,
    required this.title,
    required this.detail,
    this.titleSuffix,
    this.detailAsMarkdown = false,
    this.railColor,
    this.shimmerTitle = false,
  });

  final Widget leading;
  final String title;
  final String? titleSuffix;
  final String detail;
  final bool detailAsMarkdown;

  /// Accent color for the thin left rail (thought/tool state at a glance).
  final Color? railColor;

  /// Render the title with the starlight shimmer (live "Thinking…").
  final bool shimmerTitle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final muted = scheme.onSurfaceVariant;

    final titleStyle = theme.textTheme.bodySmall?.copyWith(color: muted);
    final titleRow = Row(
      children: [
        Flexible(
          child: shimmerTitle
              ? ShimmerText(title, style: titleStyle)
              : Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: titleStyle,
                ),
        ),
        if (titleSuffix != null && titleSuffix!.isNotEmpty) ...[
          const SizedBox(width: 6),
          Text(
            titleSuffix!.toUpperCase(),
            style: theme.textTheme.labelSmall?.copyWith(
              color: muted.withValues(alpha: 0.7),
            ),
          ),
        ],
      ],
    );

    final leadingBox = SizedBox(width: 20, child: Center(child: leading));

    Widget withRail(Widget child) {
      final rail = railColor;
      if (rail == null) return child;
      return Stack(
        children: [
          Positioned(
            left: 0,
            top: 4,
            bottom: 4,
            child: Container(
              width: 2,
              decoration: BoxDecoration(
                color: rail,
                borderRadius: BorderRadius.circular(1),
              ),
            ),
          ),
          Padding(padding: const EdgeInsets.only(left: 6), child: child),
        ],
      );
    }

    final trimmedDetail = detail.trim();
    if (trimmedDetail.isEmpty) {
      // Nothing to expand — a bare, low-chrome line.
      return withRail(
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
          child: Row(
            children: [
              leadingBox,
              const SizedBox(width: 8),
              Expanded(child: titleRow),
            ],
          ),
        ),
      );
    }

    return withRail(
      Theme(
        // Suppress ExpansionTile's divider lines so the row reads as one quiet
        // status, not a boxed section.
        data: theme.copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          dense: true,
          initiallyExpanded: false,
          tilePadding: const EdgeInsets.symmetric(horizontal: 4),
          minTileHeight: 0,
          visualDensity: VisualDensity.compact,
          childrenPadding: const EdgeInsets.fromLTRB(28, 0, 8, 8),
          expandedAlignment: Alignment.centerLeft,
          expandedCrossAxisAlignment: CrossAxisAlignment.start,
          leading: leadingBox,
          title: titleRow,
          children: [
            Align(
              alignment: Alignment.centerLeft,
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerLow,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: detailAsMarkdown
                    ? _MarkdownText(data: trimmedDetail)
                    : SelectableText(
                        trimmedDetail,
                        style: monoDetail.copyWith(color: muted),
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
