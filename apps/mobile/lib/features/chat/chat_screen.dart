import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import 'package:flutter/rendering.dart' show ScrollCacheExtent;
import 'package:flutter/services.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:markdown/markdown.dart' as md;
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
import 'chat_helpers.dart';

export 'chat_helpers.dart';

part 'transcript_pane.dart';
part 'plan_panel.dart';
part 'chat_bubble.dart';

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

/// Built-ins that map onto agent operating modes, so they are only offered when
/// the session actually advertises modes (see `_matchingCommands`).
final List<AvailableCommand> _modeCommands = [
  AvailableCommand(
    name: 'plan',
    description: 'Plan without editing; /plan off to leave',
    hint: '[off]',
  ),
  AvailableCommand(
    name: 'mode',
    description: "Show or switch the agent's mode",
    hint: '[id]',
  ),
];

/// Identity-keyed queued composer prompt so chips with identical text delete
/// independently and survive post-frame index shifts from the queue flush.
class _QueuedPrompt {
  _QueuedPrompt(this.text);
  final String text;
}

/// An image staged for the next prompt (Phase 2). Holds the raw bytes for the
/// composer thumbnail plus the base64 wire form.
class _PendingImage {
  _PendingImage({required this.bytes, required this.attachment});
  final Uint8List bytes;
  final PromptAttachment attachment;
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

  /// Near live end of reverse list — FAB / unread without shell setState (B5).
  final ValueNotifier<bool> _userNearBottom = ValueNotifier(true);

  /// Highest item seq when the user left the live end; unread badge (D).
  final ValueNotifier<int> _unreadWhileScrolledUp = ValueNotifier(0);
  int _seqAtLeaveBottom = 0;

  /// Prompts submitted while the agent was mid-turn, in send order. Flushed
  /// one per completed turn by [_maybeFlushQueue]; each shows as a removable
  /// chip above the composer until it goes out. Identity-keyed so two chips
  /// with the same text delete independently.
  final List<_QueuedPrompt> _queuedPrompts = [];

  /// Images attached to the next prompt (Phase 2). Sent with the next direct
  /// send and cleared; the attach button is disabled while the agent is busy,
  /// so these never coexist with the mid-turn queue.
  final List<_PendingImage> _pendingImages = [];

  bool _flushScheduled = false;
  final _presentedPermissionIds = <String>{};
  final _presentedQuestionIds = <String>{};
  bool _permissionSheetOpen = false;
  bool _questionSheetOpen = false;
  NotificationCoordinator? _notifCoord;

  /// Pops the currently open permission/question sheet (set while one is up),
  /// so an externally resolved request can dismiss its own stale sheet.
  VoidCallback? _dismissSheet;
  String? _openSheetPermissionId;
  String? _openSheetQuestionId;

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
  /// (history, kept transcript) must render instantly. Captured at open, then
  /// bumped past any multi-item batch (history replay / cache hydrate) by the
  /// transcript listener in [build].
  int _openSeqFloor = 0;

  /// Session is not live on the host (or history was empty for a non-live row).
  /// Used with empty transcript to explain missing messages honestly (0009 B.1).
  bool _sessionLive = true;

  /// Dismissible note: host keeps history only while the session is live.
  bool _historyNoteVisible = false;

  /// Driven by [ChatScrollActivitySensor] around the transcript list so shimmer /
  /// pulse can pause while the user is dragging or flinging.
  final ValueNotifier<bool> _listScrolling = ValueNotifier(false);

  /// Armed after a cancel: if the server's `turn_complete` never lands (lost
  /// on a socket blip), pull authoritative status so the composer cannot stay
  /// pinned on "running" until a manual refresh.
  Timer? _cancelResyncTimer;

  @override
  void initState() {
    super.initState();
    unawaited(_loadSessionCwd());
    // Tell the notifier we're watching this session, so it won't ping us about
    // events we're already seeing on screen. Captured so dispose() need not
    // touch `ref` after the scope is torn down.
    final coord = ref.read(notificationCoordinatorProvider);
    coord.claimSession(widget.sessionId);
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
      final t = ref.read(sessionTranscriptProvider(widget.sessionId));
      _maybeShowPermission(t);
      _maybeShowQuestion(t);
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
    final notifier = ref.read(transcriptsProvider.notifier);
    // Phone-side cache paints immediately after process death; host history
    // still replaces/reconciles when it arrives (MADR 0018 E1).
    await notifier.hydrateFromCache(widget.sessionId);
    if (!mounted) return;
    _raiseOpenSeqFloor();
    final client = ref.read(mcremoteClientProvider);
    final List<SessionEvent> events;
    try {
      events = await client.sessionHistory(widget.sessionId);
    } catch (_) {
      // Fired unawaited from initState: a flapping socket at chat-open must
      // not surface as an unhandled async error. Live events (or the
      // reconnect resync) will fill the transcript in.
      return;
    }
    if (!mounted) return;
    if (events.isEmpty) {
      final local = ref.read(sessionTranscriptProvider(widget.sessionId));
      if (local.items.isEmpty && !_sessionLive) {
        setState(() => _historyNoteVisible = true);
      }
      return;
    }
    await notifier.replayHistory(widget.sessionId, events);
    if (!mounted) return;
    _raiseOpenSeqFloor();
  }

  /// Keep history / cache-restored rows out of the entrance-animation window.
  void _raiseOpenSeqFloor() {
    final next = ref.read(sessionTranscriptProvider(widget.sessionId)).nextSeq;
    if (next > _openSeqFloor) {
      setState(() => _openSeqFloor = next);
    }
  }

  @override
  void dispose() {
    // Release our claim; if another chat is stacked below, its claim wins
    // again automatically.
    _notifCoord?.releaseSession(widget.sessionId);
    _cancelResyncTimer?.cancel();
    if (_listening) unawaited(_speech.stop());
    _composer.dispose();
    _focus.dispose();
    _scroll.removeListener(_onScroll);
    _scroll.dispose();
    _listScrolling.dispose();
    _userNearBottom.dispose();
    _unreadWhileScrolledUp.dispose();
    super.dispose();
  }

  List<AvailableCommand> _matchingCommands(
    List<AvailableCommand> all,
    String text, {
    bool hasModes = false,
    List<RemoteCommand> remote = const [],
  }) {
    if (!text.startsWith('/')) return const [];
    // Only autocomplete the first token (before space) as the command name.
    final space = text.indexOf(' ');
    if (space >= 0) return const [];
    final q = text.substring(1).toLowerCase();
    // The daemon resolves which canonical commands work in this session and
    // sends the answer (`remote_commands`), so offer exactly those — no
    // client-side guessing about modes or provider capabilities (MADR 0023).
    // The hardcoded lists below are the fallback for a daemon too old to send
    // the list; they gate mode commands the way that daemon did.
    final daemon = remote.isNotEmpty
        ? [
            for (final c in remote)
              if (c.available) c.asCommand,
          ]
        : <AvailableCommand>[
            ..._builtinCommands,
            if (hasModes)
              ..._modeCommands.where(
                (c) => !all.any((a) => a.name.toLowerCase() == c.name),
              ),
          ];
    // An agent that advertises a canonical name owns it, and the daemon already
    // forwards it — so its own entry (with its own description) wins.
    final agentNames = {for (final c in all) c.name.toLowerCase()};
    final merged = <AvailableCommand>[
      ...daemon.where((c) => !agentNames.contains(c.name.toLowerCase())),
      ...all,
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
    if (near != _userNearBottom.value) {
      _userNearBottom.value = near;
      if (near) {
        _unreadWhileScrolledUp.value = 0;
      } else {
        final t = ref.read(sessionTranscriptProvider(widget.sessionId));
        _seqAtLeaveBottom = t.nextSeq;
        _unreadWhileScrolledUp.value = 0;
      }
    }
  }

  void _noteUnreadIfScrolledUp(SessionTranscript t) {
    if (_userNearBottom.value) return;
    var n = 0;
    for (final item in t.items) {
      if (item.seq >= _seqAtLeaveBottom) n++;
    }
    if (n != _unreadWhileScrolledUp.value) {
      _unreadWhileScrolledUp.value = n;
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

  /// Pick an image from the gallery and stage it for the next prompt. Uses the
  /// system photo picker (no runtime permission on modern Android). Only shown
  /// when the agent advertises image support (ACP promptCapabilities.image).
  Future<void> _pickImage() async {
    try {
      final file = await ImagePicker().pickImage(
        source: ImageSource.gallery,
        maxWidth: 2048,
        imageQuality: 85,
      );
      if (file == null) return;
      final bytes = await file.readAsBytes();
      // base64 inflates ~33% and the relay caps message bytes; refuse an
      // oversized image rather than send a doomed prompt.
      if (bytes.lengthInBytes > 4 * 1024 * 1024) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('Image too large (max ~4 MB).')),
          );
        }
        return;
      }
      if (!mounted) return;
      setState(() {
        _pendingImages.add(
          _PendingImage(
            bytes: bytes,
            attachment: PromptAttachment(
              kind: 'image',
              mimeType: _mimeForImage(file.path, file.mimeType),
              data: base64Encode(bytes),
            ),
          ),
        );
      });
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Could not attach image: $e')));
      }
    }
  }

  static String _mimeForImage(String path, String? given) {
    if (given != null && given.isNotEmpty) return given;
    final p = path.toLowerCase();
    if (p.endsWith('.png')) return 'image/png';
    if (p.endsWith('.webp')) return 'image/webp';
    if (p.endsWith('.gif')) return 'image/gif';
    return 'image/jpeg';
  }

  /// Agent config options sheet (Phase 3). Optimistic: the switch/selection
  /// moves immediately and reverts if the set RPC fails, since a real agent
  /// confirms via the set response rather than a session_config echo.
  Future<void> _showConfigSheet(List<ConfigOption> initial) async {
    final client = ref.read(mcremoteClientProvider);
    final opts = [...initial];

    ConfigOption withBool(ConfigOption o, bool v) => ConfigOption(
      id: o.id,
      name: o.name,
      kind: o.kind,
      description: o.description,
      boolValue: v,
      currentValue: o.currentValue,
      values: o.values,
    );
    ConfigOption withValue(ConfigOption o, String v) => ConfigOption(
      id: o.id,
      name: o.name,
      kind: o.kind,
      description: o.description,
      boolValue: o.boolValue,
      currentValue: v,
      values: o.values,
    );

    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (sheetCtx) => StatefulBuilder(
        builder: (sheetCtx, setSheet) => SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
                child: Text(
                  'Agent settings',
                  style: Theme.of(sheetCtx).textTheme.titleMedium,
                ),
              ),
              for (var i = 0; i < opts.length; i++)
                Builder(
                  builder: (_) {
                    final o = opts[i];
                    Future<void> apply(
                      ConfigOption optimistic,
                      String value,
                    ) async {
                      final prev = opts[i];
                      setSheet(() => opts[i] = optimistic);
                      try {
                        await client.setConfigOption(
                          widget.sessionId,
                          o.id,
                          o.kind,
                          value,
                        );
                      } catch (e) {
                        setSheet(() => opts[i] = prev);
                        if (sheetCtx.mounted) {
                          ScaffoldMessenger.of(sheetCtx).showSnackBar(
                            SnackBar(
                              content: Text(
                                'Update failed: ${friendlyOpError(e)}',
                              ),
                            ),
                          );
                        }
                      }
                    }

                    if (o.isBoolean) {
                      return SwitchListTile(
                        title: Text(o.name),
                        subtitle: o.description.isEmpty
                            ? null
                            : Text(o.description),
                        value: o.boolValue,
                        onChanged: (v) =>
                            apply(withBool(o, v), v ? 'true' : 'false'),
                      );
                    }
                    final currentName = o.values
                        .firstWhere(
                          (v) => v.id == o.currentValue,
                          orElse: () => ConfigOptionValue(
                            id: o.currentValue,
                            name: o.currentValue,
                          ),
                        )
                        .name;
                    return ListTile(
                      title: Text(o.name),
                      subtitle: Text(
                        currentName.isEmpty ? 'Not set' : currentName,
                      ),
                      trailing: const Icon(Icons.arrow_drop_down),
                      onTap: () async {
                        final chosen = await showModalBottomSheet<String>(
                          context: sheetCtx,
                          showDragHandle: true,
                          builder: (c2) => SafeArea(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                for (final v in o.values)
                                  ListTile(
                                    title: Text(v.name),
                                    trailing: v.id == o.currentValue
                                        ? const Icon(Icons.check)
                                        : null,
                                    onTap: () => Navigator.pop(c2, v.id),
                                  ),
                              ],
                            ),
                          ),
                        );
                        if (chosen == null) return;
                        await apply(withValue(o, chosen), chosen);
                      },
                    );
                  },
                ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _send() async {
    final text = _composer.text.trim();
    final attachments = [for (final p in _pendingImages) p.attachment];
    if (text.isEmpty && attachments.isEmpty) return;
    if (_listening) {
      // Sending is a natural end to dictation.
      unawaited(_speech.stop());
      setState(() => _listening = false);
    }
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    // An in-flight send counts as busy: the composer already shows the queue
    // affordance during the RPC window, so a tap then must queue, not drop.
    final agentBusy =
        _sending ||
        transcript.status == 'running' ||
        transcript.hasBlockingPrompt;
    if (agentBusy) {
      // Mid-turn: queue it. [_maybeFlushQueue] sends it the moment the turn
      // completes and no permission decision is outstanding.
      setState(() => _queuedPrompts.add(_QueuedPrompt(text)));
      _composer.clear();
      // Drop the keyboard so the transcript can use the full height while the
      // agent works; user re-taps the field to queue another prompt.
      _focus.unfocus();
      HapticFeedback.lightImpact();
      return;
    }
    _composer.clear();
    _focus.unfocus();
    HapticFeedback.lightImpact();
    // Attachments ride the direct send only (attach is disabled while busy, so
    // they never queue). Stage the bytes so the user_message echo can fold a
    // real thumbnail into the bubble, then clear the composer strip.
    final images = [for (final p in _pendingImages) p.bytes];
    if (images.isNotEmpty) {
      ref
          .read(transcriptsProvider.notifier)
          .stageSentImages(widget.sessionId, images);
      setState(() => _pendingImages.clear());
    }
    await _sendText(
      text,
      attachments: attachments,
      stagedImages: images.isNotEmpty,
      restoreComposerOnFailure: true,
    );
  }

  /// Deliver one prompt to the daemon. On failure either restores the composer
  /// text (direct sends) or re-queues at the front (queued sends), so a
  /// message is never silently lost.
  Future<void> _sendText(
    String text, {
    List<PromptAttachment> attachments = const [],
    bool stagedImages = false,
    bool restoreComposerOnFailure = false,
    bool requeueOnFailure = false,
  }) async {
    setState(() => _sending = true);
    try {
      final client = ref.read(mcremoteClientProvider);
      await client.prompt(widget.sessionId, text, attachments: attachments);
      // Guard the async gap: backing out of the chat mid-request disposes
      // the controller and tears down this State.
      if (!mounted) return;
      _userNearBottom.value = true;
      _unreadWhileScrolledUp.value = 0;
      _scrollToEnd();
    } catch (e) {
      // The user_message echo never arrived; drop the staged bytes so they
      // cannot mis-attach to a later turn.
      if (stagedImages) {
        ref
            .read(transcriptsProvider.notifier)
            .unstageSentImages(widget.sessionId);
      }
      if (mounted) {
        if (restoreComposerOnFailure && _composer.text.trim().isEmpty) {
          _composer.text = text;
          _composer.selection = TextSelection.collapsed(offset: text.length);
        }
        if (requeueOnFailure) {
          setState(() => _queuedPrompts.insert(0, _QueuedPrompt(text)));
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
    if (t.status == 'running' || t.hasBlockingPrompt) return;
    if (ref.read(mcremoteClientProvider).state != McConnectionState.connected) {
      return;
    }
    _flushScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      _flushScheduled = false;
      if (!mounted || _queuedPrompts.isEmpty || _sending) return;
      final now = ref.read(sessionTranscriptProvider(widget.sessionId));
      if (now.status == 'running' || now.hasBlockingPrompt) return;
      if (ref.read(mcremoteClientProvider).state !=
          McConnectionState.connected) {
        return;
      }
      final queued = _queuedPrompts.first;
      setState(() => _queuedPrompts.removeAt(0));
      await _sendText(queued.text, requeueOnFailure: true);
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
      // Belt-and-braces: syncFromMeta only ever moves status *out* of
      // running, so this is safe even if the turn actually completed.
      _cancelResyncTimer?.cancel();
      _cancelResyncTimer = Timer(const Duration(seconds: 5), () {
        if (!mounted) return;
        final t = ref.read(sessionTranscriptProvider(widget.sessionId));
        if (t.status == 'running') unawaited(_resyncAfterReconnect());
      });
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Cancel failed: $e')));
      }
    }
  }

  /// OpenCode-only: pull file-change summary (also lands as a notice).
  Future<void> _viewDiff() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Reconnect to the host first')),
      );
      return;
    }
    try {
      final summary = await client.sessionDiff(widget.sessionId);
      if (!mounted) return;
      if (summary.isEmpty) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('No file changes')));
        return;
      }
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Session diff'),
          content: SingleChildScrollView(
            child: SelectableText(
              summary,
              style: Theme.of(
                ctx,
              ).textTheme.bodySmall?.copyWith(fontFamily: 'monospace'),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('Close'),
            ),
          ],
        ),
      );
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Diff failed: ${friendlyOpError(e)}')),
        );
      }
    }
  }

  /// OpenCode-only: fork conversation into a new mcremote session and open it.
  Future<void> _forkSession() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Reconnect to the host first')),
      );
      return;
    }
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Fork session?'),
        content: const Text(
          'Creates a new session branched from this conversation on the host. '
          'The current session is left as-is.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Fork'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    try {
      final meta = await client.forkSession(widget.sessionId);
      if (!mounted) return;
      final q = meta.name.isNotEmpty
          ? '?name=${Uri.encodeComponent(meta.name)}'
          : '';
      await context.push('/sessions/${meta.id}$q');
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Fork failed: ${friendlyOpError(e)}')),
        );
      }
    }
  }

  /// OpenCode-only: restore previously reverted messages.
  Future<void> _unrevert() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Reconnect to the host first')),
      );
      return;
    }
    try {
      await client.unrevert(widget.sessionId);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Restored reverted messages')),
      );
      unawaited(_resyncAfterReconnect());
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Restore failed: ${friendlyOpError(e)}')),
        );
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
                                  onPressed: () {
                                    HapticFeedback.selectionClick();
                                    Navigator.pop(ctx, o.optionId);
                                  },
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

  /// Present outstanding multi-question forms one at a time (after permissions).
  void _maybeShowQuestion(SessionTranscript transcript) {
    if (_permissionSheetOpen || _questionSheetOpen) return;
    prunePresentedQuestionIds(
      _presentedQuestionIds,
      transcript.pendingQuestions.keys.toSet(),
    );
    for (final pending in transcript.pendingQuestions.values) {
      final id = pending.questionId;
      if (id == null || id.isEmpty) continue;
      if (_presentedQuestionIds.contains(id)) continue;
      _presentedQuestionIds.add(id);
      _questionSheetOpen = true;
      WidgetsBinding.instance.addPostFrameCallback((_) async {
        if (!mounted) {
          _questionSheetOpen = false;
          return;
        }
        try {
          await _showQuestionSheet(pending);
        } finally {
          _questionSheetOpen = false;
          if (mounted) {
            _maybeShowPermission(
              ref.read(sessionTranscriptProvider(widget.sessionId)),
            );
            _maybeShowQuestion(
              ref.read(sessionTranscriptProvider(widget.sessionId)),
            );
          }
        }
      });
      return;
    }
  }

  Future<void> _showQuestionSheet(SessionEvent ev) async {
    _openSheetQuestionId = ev.questionId;
    final items = ev.questions;
    // Selected labels per question index (OpenCode wire).
    final selections = List<Set<String>>.generate(
      items.length,
      (_) => <String>{},
    );
    final customControllers = List.generate(
      items.length,
      (_) => TextEditingController(),
    );

    final result = await showModalBottomSheet<Object?>(
      context: context,
      isDismissible: false,
      enableDrag: false,
      isScrollControlled: true,
      builder: (ctx) {
        _dismissSheet = () {
          if (ctx.mounted) Navigator.pop(ctx, '__external__');
        };
        final theme = Theme.of(ctx);
        final tokens = celestialOf(ctx);
        final title = (ev.text ?? '').trim().isEmpty
            ? 'Agent question'
            : ev.text!.trim();
        return StatefulBuilder(
          builder: (ctx, setSheetState) {
            return SafeArea(
              child: ConstrainedBox(
                constraints: BoxConstraints(
                  maxHeight: MediaQuery.of(ctx).size.height * 0.9,
                ),
                child: SingleChildScrollView(
                  key: const Key('question-sheet-scroll'),
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
                            Icon(Icons.help_outline, color: tokens.gold),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                title,
                                style: theme.textTheme.titleLarge,
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 16),
                        for (var i = 0; i < items.length; i++) ...[
                          if (items[i].header.isNotEmpty)
                            Text(
                              items[i].header,
                              style: theme.textTheme.titleSmall?.copyWith(
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          if (items[i].text.isNotEmpty) ...[
                            const SizedBox(height: 4),
                            Text(
                              items[i].text,
                              style: theme.textTheme.bodyMedium,
                            ),
                          ],
                          const SizedBox(height: 8),
                          ...items[i].options.map((o) {
                            final label = o.optionId.isEmpty
                                ? o.name
                                : o.optionId;
                            final selected = selections[i].contains(label);
                            if (items[i].multiple) {
                              return CheckboxListTile(
                                dense: true,
                                contentPadding: EdgeInsets.zero,
                                value: selected,
                                title: Text(o.name.isEmpty ? label : o.name),
                                onChanged: (v) {
                                  setSheetState(() {
                                    if (v == true) {
                                      selections[i].add(label);
                                    } else {
                                      selections[i].remove(label);
                                    }
                                  });
                                },
                              );
                            }
                            // Single-select via FilterChip (avoids deprecated RadioListTile).
                            return Padding(
                              padding: const EdgeInsets.only(bottom: 6),
                              child: FilterChip(
                                selected: selected,
                                label: Text(o.name.isEmpty ? label : o.name),
                                onSelected: (v) {
                                  setSheetState(() {
                                    selections[i].clear();
                                    if (v) selections[i].add(label);
                                  });
                                },
                              ),
                            );
                          }),
                          if (items[i].custom) ...[
                            const SizedBox(height: 4),
                            TextField(
                              controller: customControllers[i],
                              decoration: const InputDecoration(
                                labelText: 'Other',
                                isDense: true,
                              ),
                              onChanged: (_) => setSheetState(() {}),
                            ),
                          ],
                          if (i < items.length - 1) const Divider(height: 24),
                        ],
                        const SizedBox(height: 16),
                        FilledButton(
                          onPressed: () {
                            final answers = <List<String>>[];
                            for (var i = 0; i < items.length; i++) {
                              final labels = selections[i].toList();
                              final custom = customControllers[i].text.trim();
                              if (custom.isNotEmpty && items[i].custom) {
                                labels.add(custom);
                              }
                              answers.add(labels);
                            }
                            HapticFeedback.selectionClick();
                            Navigator.pop(ctx, answers);
                          },
                          child: const Text('Submit'),
                        ),
                        TextButton(
                          onPressed: () => Navigator.pop(ctx, '__cancel__'),
                          child: const Text('Cancel / skip'),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            );
          },
        );
      },
    );
    _dismissSheet = null;
    _openSheetQuestionId = null;
    for (final c in customControllers) {
      c.dispose();
    }

    final questionId = ev.questionId;
    if (result == null || questionId == null) return;
    if (result == '__external__') {
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
        await client.respondQuestion(
          sessionId: widget.sessionId,
          questionId: questionId,
          cancelled: true,
        );
      } else if (result is List<List<String>>) {
        await client.respondQuestion(
          sessionId: widget.sessionId,
          questionId: questionId,
          answers: result,
        );
      }
      if (!mounted) return;
      ref
          .read(transcriptsProvider.notifier)
          .clearPending(widget.sessionId, questionId: questionId);
    } catch (e) {
      _presentedQuestionIds.remove(questionId);
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Question respond failed: $e')));
      }
    }
  }

  /// Present outstanding permission requests one at a time, oldest first.
  ///
  /// The daemon allows concurrent requests, so this drains a queue rather than
  /// showing a single sheet.
  void _maybeShowPermission(SessionTranscript transcript) {
    if (_questionSheetOpen) return;
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
        // was up; drain the rest (permissions first, then questions).
        if (mounted) {
          final t = ref.read(sessionTranscriptProvider(widget.sessionId));
          _maybeShowPermission(t);
          _maybeShowQuestion(t);
        }
      });
      return;
    }
    // No permission sheet queued — drain questions if any.
    _maybeShowQuestion(transcript);
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
      sessionTranscriptProvider(sid).select((t) => t.pendingPermissions.length),
    );
    final pendingQuestionCount = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.pendingQuestions.length),
    );
    final pendingToolName = ref.watch(
      sessionTranscriptProvider(
        sid,
      ).select((t) => t.pendingPermission?.toolName ?? ''),
    );
    final pendingQuestionLabel = ref.watch(
      sessionTranscriptProvider(
        sid,
      ).select((t) => t.pendingQuestion?.text ?? ''),
    );
    final hasPending = pendingCount > 0 || pendingQuestionCount > 0;
    final commands = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.commands),
    );
    // What the daemon says works in this session; empty against an older daemon.
    final remoteCommands = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.remoteCommands),
    );
    final plan = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.plan),
    );
    final hasItems = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.items.isNotEmpty),
    );
    // ACP parity UI (Phase 2/3): the image-attach button, mode switcher, and
    // config sheet self-gate on the agent's advertised capabilities/state.
    final canAttachImage = ref.watch(
      sessionTranscriptProvider(
        sid,
      ).select((t) => t.capabilities?.image ?? false),
    );
    final modes = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.modes),
    );
    final currentModeId = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.currentModeId),
    );
    final configOptions = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.configOptions),
    );

    ref.listen(sessionTranscriptProvider(sid), (prev, next) {
      // Multi-item growth is a history/cache batch (live events append one
      // row per update): lift the animation floor above it so restored rows
      // render instantly instead of running their entrance fade.
      final prevLen = prev?.items.length ?? 0;
      if (next.items.length > prevLen + 1 && next.nextSeq > _openSeqFloor) {
        setState(() => _openSeqFloor = next.nextSeq);
      }
      // reverse:true keeps offset 0 pinned while the newest bubble grows, so
      // we only force a jump when a *new* row appears (tool card, user message,
      // fresh assistant bubble) and the user was already following the live end.
      // Chasing maxScrollExtent on every text chunk was the main scroll jitter.
      final appended =
          next.items.isNotEmpty &&
          next.items.last.seq !=
              (prev?.items.isNotEmpty ?? false ? prev!.items.last.seq : -1);
      if (appended && _userNearBottom.value) {
        _scrollToEnd();
      } else if (appended) {
        _noteUnreadIfScrolledUp(next);
      }
      // A sheet whose request was resolved elsewhere must not keep inviting
      // an approval that can no longer be applied.
      final openPerm = _openSheetPermissionId;
      if (openPerm != null && !next.pendingPermissions.containsKey(openPerm)) {
        _dismissSheet?.call();
      }
      final openQ = _openSheetQuestionId;
      if (openQ != null && !next.pendingQuestions.containsKey(openQ)) {
        _dismissSheet?.call();
      }
      _maybeShowPermission(next);
      _maybeShowQuestion(next);
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
    // The composer advertises readiness: its outline goes the same green as
    // the Idle status chip when the agent is idle, and falls back to the
    // stock theme borders whenever the agent is working or unreachable.
    final agentIdle = !busy && !offline && !linking && status == 'idle';

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
          // Context-window indicator (ACP usage_update). Self-hiding until the
          // agent reports usage; working/idle status still lives on the
          // sessions list and the stop control on the composer send slot.
          _ContextUsageChip(widget.sessionId),
          // Agent mode switcher (Phase 3) — only when the agent exposes modes.
          if (modes.isNotEmpty)
            _ModeSelector(
              sessionId: sid,
              modes: modes,
              currentModeId: currentModeId,
              enabled: !offline,
            ),
          // Agent config options (Phase 3) — only when the agent exposes any.
          if (configOptions.isNotEmpty)
            IconButton(
              tooltip: 'Agent settings',
              icon: const Icon(Icons.tune),
              onPressed: offline ? null : () => _showConfigSheet(configOptions),
            ),
          PopupMenuButton<String>(
            tooltip: 'Session actions',
            onSelected: (v) {
              if (v == 'cancel') unawaited(_cancelTurn());
              if (v == 'end') unawaited(_endSession());
              // OpenCode session-tree polish (MADR 0020 Sprint 5 / PR10).
              if (v == 'diff') unawaited(_viewDiff());
              if (v == 'fork') unawaited(_forkSession());
              if (v == 'unrevert') unawaited(_unrevert());
            },
            itemBuilder: (ctx) {
              final isOpencode = _provider == 'opencode';
              return [
                const PopupMenuItem(
                  value: 'cancel',
                  child: ListTile(
                    leading: Icon(Icons.stop_circle_outlined),
                    title: Text('Stop current turn'),
                    contentPadding: EdgeInsets.zero,
                    dense: true,
                  ),
                ),
                if (isOpencode) ...[
                  const PopupMenuItem(
                    value: 'diff',
                    child: ListTile(
                      leading: Icon(Icons.difference_outlined),
                      title: Text('View file diff'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                    ),
                  ),
                  const PopupMenuItem(
                    value: 'fork',
                    child: ListTile(
                      leading: Icon(Icons.call_split),
                      title: Text('Fork session'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                    ),
                  ),
                  const PopupMenuItem(
                    value: 'unrevert',
                    child: ListTile(
                      leading: Icon(Icons.restore),
                      title: Text('Restore reverts'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                    ),
                  ),
                ],
                const PopupMenuItem(
                  value: 'end',
                  child: ListTile(
                    leading: Icon(Icons.delete_outline),
                    title: Text('End session'),
                    contentPadding: EdgeInsets.zero,
                    dense: true,
                  ),
                ),
              ];
            },
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
                pendingCount > 0
                    ? (pendingCount > 1
                          ? 'Waiting for $pendingCount permissions: '
                                '${pendingToolName.isEmpty ? 'tool' : pendingToolName} '
                                'and ${pendingCount - 1} more'
                          : 'Waiting for permission: '
                                '${pendingToolName.isEmpty ? 'tool' : pendingToolName}')
                    : (pendingQuestionCount > 1
                          ? 'Waiting for $pendingQuestionCount questions'
                          : 'Waiting for answer: '
                                '${pendingQuestionLabel.isEmpty ? 'agent question' : pendingQuestionLabel}'),
              ),
              actions: [
                TextButton(
                  onPressed: () {
                    // Allow re-presenting after a dismissal or failed send.
                    _presentedPermissionIds.clear();
                    _presentedQuestionIds.clear();
                    final t = ref.read(sessionTranscriptProvider(sid));
                    _maybeShowPermission(t);
                    _maybeShowQuestion(t);
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
                if (hasItems)
                  Positioned(
                    right: 12,
                    bottom: 12,
                    child: ValueListenableBuilder<bool>(
                      valueListenable: _userNearBottom,
                      builder: (context, nearBottom, _) {
                        if (nearBottom) return const SizedBox.shrink();
                        return ValueListenableBuilder<int>(
                          valueListenable: _unreadWhileScrolledUp,
                          builder: (context, unread, _) {
                            return FloatingActionButton.small(
                              heroTag: 'jump-to-latest',
                              tooltip: unread > 0
                                  ? 'Jump to latest ($unread new)'
                                  : 'Jump to latest',
                              onPressed: () {
                                _focus.unfocus();
                                _userNearBottom.value = true;
                                _unreadWhileScrolledUp.value = 0;
                                _scrollToEnd();
                              },
                              child: unread > 0
                                  ? Badge(
                                      label: Text(
                                        unread > 9 ? '9+' : '$unread',
                                      ),
                                      child: const Icon(Icons.arrow_downward),
                                    )
                                  : const Icon(Icons.arrow_downward),
                            );
                          },
                        );
                      },
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
              final matches = _matchingCommands(
                commands,
                value.text,
                hasModes: modes.isNotEmpty,
                remote: remoteCommands,
              );
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
                  for (final queued in _queuedPrompts)
                    InputChip(
                      key: ObjectKey(queued),
                      avatar: const Icon(Icons.schedule_send, size: 16),
                      label: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 200),
                        child: Text(
                          queued.text,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      visualDensity: VisualDensity.compact,
                      deleteButtonTooltipMessage: 'Remove queued message',
                      // Identity remove: post-frame flush may removeAt(0)
                      // between build and tap, and duplicate texts must not
                      // delete the wrong chip.
                      onDeleted: () => setState(() {
                        _queuedPrompts.remove(queued);
                      }),
                    ),
                ],
              ),
            ),
          // Staged image attachments (Phase 2): thumbnails above the composer,
          // removable, sent with the next prompt.
          if (_pendingImages.isNotEmpty)
            SafeArea(
              top: false,
              bottom: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(12, 4, 12, 0),
                child: SizedBox(
                  height: 64,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    itemCount: _pendingImages.length,
                    separatorBuilder: (_, _) => const SizedBox(width: 8),
                    itemBuilder: (ctx, i) => _AttachmentThumb(
                      bytes: _pendingImages[i].bytes,
                      onRemove: () =>
                          setState(() => _pendingImages.removeAt(i)),
                    ),
                  ),
                ),
              ),
            ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
              child: Row(
                children: [
                  if (canAttachImage)
                    IconButton(
                      tooltip: 'Attach image',
                      icon: const Icon(Icons.add_photo_alternate_outlined),
                      // Disabled while busy: attachments ride a direct send
                      // only, never the mid-turn queue.
                      onPressed: (busy || offline) ? null : _pickImage,
                    ),
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
                        // Idle: green outline matching the status chip. Null
                        // falls back to the stock theme borders (working /
                        // offline keep the current look).
                        enabledBorder: agentIdle
                            ? OutlineInputBorder(
                                borderRadius: BorderRadius.circular(14),
                                borderSide: BorderSide(
                                  color: celestialOf(context).success,
                                ),
                              )
                            : null,
                        focusedBorder: agentIdle
                            ? OutlineInputBorder(
                                borderRadius: BorderRadius.circular(14),
                                borderSide: BorderSide(
                                  color: celestialOf(context).success,
                                  width: 1.5,
                                ),
                              )
                            : null,
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
                  // (stop stays reachable from the session menu in every
                  // state).
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

/// Compact context-window indicator for the chat app bar (ACP usage_update).
/// Watches only the session's [Usage] via `.select`, so a token stream doesn't
/// rebuild it, and renders nothing until the agent reports a window size.
class _ContextUsageChip extends ConsumerWidget {
  const _ContextUsageChip(this.sessionId);

  final String sessionId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final usage = ref.watch(
      sessionTranscriptProvider(sessionId).select((t) => t.usage),
    );
    if (usage == null || usage.size <= 0) return const SizedBox.shrink();

    final scheme = Theme.of(context).colorScheme;
    final tokens = celestialOf(context);
    final f = usage.fraction;
    // Escalate as the window fills: neutral < 75% <= gold < 90% <= error.
    final color = f >= 0.9
        ? scheme.error
        : f >= 0.75
        ? tokens.gold
        : scheme.onSurfaceVariant;
    final pct = (f * 100).round();

    return Center(
      child: Tooltip(
        message: '$pct% of context used\n${usage.used} / ${usage.size} tokens',
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 6),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.data_usage, size: 14, color: color),
              const SizedBox(width: 4),
              Text(
                '$pct%',
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: color,
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Agent operating-mode switcher shown in the chat app bar (ACP session modes).
/// Renders the current mode name with a dropdown; picking one calls set_mode.
class _ModeSelector extends ConsumerWidget {
  const _ModeSelector({
    required this.sessionId,
    required this.modes,
    required this.currentModeId,
    required this.enabled,
  });

  final String sessionId;
  final List<SessionMode> modes;
  final String? currentModeId;
  final bool enabled;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final current = modes.firstWhere(
      (m) => m.id == currentModeId,
      orElse: () => modes.first,
    );
    return PopupMenuButton<String>(
      enabled: enabled,
      tooltip: 'Agent mode',
      onSelected: (id) async {
        try {
          await ref.read(mcremoteClientProvider).setMode(sessionId, id);
        } catch (e) {
          if (context.mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text('Mode change failed: ${friendlyOpError(e)}'),
              ),
            );
          }
        }
      },
      itemBuilder: (_) => [
        for (final m in modes)
          CheckedPopupMenuItem(
            value: m.id,
            checked: m.id == current.id,
            child: Text(m.name),
          ),
      ],
      child: _ModeChip(mode: current),
    );
  }
}

/// The mode switcher's label. Plan mode reads as a state, not a menu: it is
/// tinted and carries an edit-off icon, because "the agent will not touch my
/// files" is the one mode difference worth noticing at a glance.
class _ModeChip extends StatelessWidget {
  const _ModeChip({required this.mode});

  final SessionMode mode;

  static bool isPlan(SessionMode m) => m.id.toLowerCase() == 'plan';

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final planning = isPlan(mode);
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 4),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: planning
          ? BoxDecoration(
              color: scheme.tertiaryContainer,
              borderRadius: BorderRadius.circular(12),
            )
          : null,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (planning) ...[
            Icon(Icons.edit_off, size: 14, color: scheme.onTertiaryContainer),
            const SizedBox(width: 4),
          ],
          Text(
            mode.name,
            style: Theme.of(context).textTheme.labelLarge?.copyWith(
              color: planning ? scheme.onTertiaryContainer : scheme.primary,
            ),
          ),
          Icon(
            Icons.arrow_drop_down,
            size: 18,
            color: planning ? scheme.onTertiaryContainer : null,
          ),
        ],
      ),
    );
  }
}

/// Thumbnail for a staged image attachment with a remove affordance.
class _AttachmentThumb extends StatelessWidget {
  const _AttachmentThumb({required this.bytes, required this.onRemove});

  final Uint8List bytes;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Stack(
      clipBehavior: Clip.none,
      children: [
        ClipRRect(
          borderRadius: BorderRadius.circular(10),
          child: Image.memory(bytes, width: 64, height: 64, fit: BoxFit.cover),
        ),
        Positioned(
          top: -8,
          right: -8,
          child: IconButton(
            iconSize: 14,
            visualDensity: VisualDensity.compact,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(minWidth: 28, minHeight: 28),
            tooltip: 'Remove',
            onPressed: onRemove,
            icon: CircleAvatar(
              radius: 10,
              backgroundColor: scheme.surface,
              child: Icon(Icons.close, size: 12, color: scheme.onSurface),
            ),
          ),
        ),
      ],
    );
  }
}
