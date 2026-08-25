import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:file_selector/file_selector.dart';
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
import '../../data/protocol/frame_budget.dart';
import '../../data/protocol/picker.dart';
import '../../state/app_providers.dart';
import '../../state/session_synchronizer.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/scroll_activity.dart';
import '../../theme/starfield.dart';
import '../../theme/top_notification.dart';
import '../../theme/widgets.dart';
import '../widgets/option_picker_sheet.dart';
import '../widgets/subagents_panel.dart';
import '../widgets/work_items_panel.dart';
import 'chat_helpers.dart';
import 'codex_terminals_screen.dart';
import 'question_sheet.dart';

export 'chat_helpers.dart';

part 'transcript_pane.dart';
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
  _QueuedPrompt({
    required this.text,
    required List<PromptAttachment> attachments,
    required List<Uint8List> imageBytes,
  }) : attachments = List.unmodifiable(attachments),
       imageBytes = List.unmodifiable(
         imageBytes.map((bytes) => Uint8List.fromList(bytes)),
       );

  final String text;
  final List<PromptAttachment> attachments;
  final List<Uint8List> imageBytes;

  String get label {
    if (text.isNotEmpty) return text;
    final count = imageBytes.length;
    return '$count ${count == 1 ? 'image' : 'images'}';
  }
}

/// A non-text block staged for the next prompt (Phase 2; audio added by MADR
/// 0112 A2). Holds the raw bytes for the composer preview plus the base64 wire
/// form. Images and audio share this model so both follow one bounded
/// stage/preview/remove flow.
class _PendingAttachment {
  _PendingAttachment({required this.bytes, required this.attachment});
  final Uint8List bytes;
  final PromptAttachment attachment;

  bool get isImage => attachment.kind == 'image';
  bool get isAudio => attachment.kind == 'audio';
}

/// Stands in for the system photo picker so tests can stage a real attachment
/// without a platform channel. Null in production.
@visibleForTesting
Future<XFile?> Function()? debugPickImage;

/// Stands in for the system file picker used for audio, for the same reason.
/// Null in production.
@visibleForTesting
Future<XFile?> Function()? debugPickAudio;

/// Audio media types the composer will stage.
///
/// An allowlist rather than a family prefix: the daemon accepts any `audio/*`,
/// but offering a container the model cannot decode wastes a whole turn, and
/// these are the types OpenCode's documented audio inputs cover (MADR 0112 A2).
const Set<String> kAcceptedAudioMimeTypes = {
  'audio/aac',
  'audio/flac',
  'audio/mp4',
  'audio/mpeg',
  'audio/ogg',
  'audio/opus',
  'audio/wav',
  'audio/webm',
  'audio/x-m4a',
};

/// Maps a filename extension onto an accepted audio media type.
///
/// Returns an empty string when the extension is unknown, which the caller
/// surfaces as a refusal rather than guessing a type the model would reject.
String audioMimeForPath(String path, [String? given]) {
  if (given != null && kAcceptedAudioMimeTypes.contains(given.toLowerCase())) {
    return given.toLowerCase();
  }
  final lower = path.toLowerCase();
  const byExtension = <String, String>{
    '.aac': 'audio/aac',
    '.flac': 'audio/flac',
    '.m4a': 'audio/mp4',
    '.mp3': 'audio/mpeg',
    '.mp4': 'audio/mp4',
    '.oga': 'audio/ogg',
    '.ogg': 'audio/ogg',
    '.opus': 'audio/opus',
    '.wav': 'audio/wav',
    '.weba': 'audio/webm',
  };
  for (final entry in byExtension.entries) {
    if (lower.endsWith(entry.key)) return entry.value;
  }
  return '';
}

/// Reduces a picked path to a bare basename the daemon will accept.
String attachmentBasename(String path) {
  var name = path;
  final slash = name.lastIndexOf('/');
  if (slash >= 0) name = name.substring(slash + 1);
  final back = name.lastIndexOf(r'\');
  if (back >= 0) name = name.substring(back + 1);
  name = name.replaceAll('\u0000', '');
  if (name == '.' || name == '..') return '';
  // The daemon bounds the basename at 256 bytes; keep the extension by
  // trimming the stem rather than the tail.
  if (name.length > 256) {
    final dot = name.lastIndexOf('.');
    final ext = dot > 0 ? name.substring(dot) : '';
    final keep = 256 - ext.length;
    name = keep > 0 ? name.substring(0, keep) + ext : name.substring(0, 256);
  }
  return name;
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
  bool _endingSession = false;
  bool _interceptingModel = false;

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
  final List<_PendingAttachment> _pendingAttachments = [];

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
  ModalRoute<dynamic>? _openPermissionSheetRoute;
  ModalRoute<bool?>? _openAlwaysConfirmRoute;

  final SpeechToText _speech = SpeechToText();
  bool _listening = false;
  String _voiceBase = '';

  /// The session's working directory on the host, shown under the title.
  /// Fetched once from the sessions list; empty until it arrives.
  String _cwd = '';

  /// The CLI provider driving this session ("grok", "opencode"), shown before
  /// the cwd so it's always clear which agent is being controlled.
  String _provider = '';

  /// Session thinking/effort level from meta; empty = provider default.
  String _thinkingLevel = '';

  /// True after we've attempted to apply the settings default session mode
  /// (MADR 0052 B2). One shot per open so we never fight the user.
  bool _appliedDefaultMode = false;

  /// Local seq floor at screen open: items at or above it were appended while
  /// this screen was visible and get the entrance animation; anything below
  /// (history, kept transcript) must render instantly. Captured at open, then
  /// bumped past any multi-item batch (history replay / cache hydrate) by the
  /// transcript listener in [build].
  int _openSeqFloor = 0;

  /// Session is not live on the host (or history was empty for a non-live row).
  /// Used with empty transcript to explain missing messages honestly (0009 B.1).
  bool _sessionLive = true;

  /// Dismissible note: host may have no retained events for this session
  /// (empty/corrupt/never-recorded). Durable history keeps the last ~800
  /// events across close/restart when present (MADR 0056 L-1).
  bool _historyNoteVisible = false;

  /// Driven by [ChatScrollActivitySensor] around the transcript list so shimmer /
  /// pulse can pause while the user is dragging or flinging.
  final ValueNotifier<bool> _listScrolling = ValueNotifier(false);

  /// Armed after a cancel: if the server's `turn_complete` never lands (lost
  /// on a socket blip), pull authoritative status so the composer cannot stay
  /// pinned on "running" until a manual refresh.
  Timer? _cancelResyncTimer;
  StreamSubscription<SessionEvent>? _titleSub;
  late String _title;

  @override
  void initState() {
    super.initState();
    _title = widget.sessionName ?? '';
    _titleSub = ref.read(mcremoteClientProvider).events.listen((event) {
      final title = event.title?.trim() ?? '';
      if (!mounted ||
          event.type != 'session_title' ||
          event.sessionId != widget.sessionId ||
          title.isEmpty) {
        return;
      }
      setState(() => _title = title);
    });
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

  /// After a reconnect, connection-scoped [SessionSynchronizer] owns list +
  /// multi-session history. This only ensures the *mounted* session is warm
  /// for the composer and honesty note (MADR 0056 H-1 Phase 2).
  Future<void> _resyncAfterReconnect() async {
    try {
      await ref
          .read(sessionSynchronizerProvider.notifier)
          .ensureSession(widget.sessionId);
    } catch (e) {
      // best-effort: next open/resync will retry.
      debugPrint('chat: ensureSession after reconnect failed: $e');
    }
    if (!mounted) return;
    final t = ref.read(sessionTranscriptProvider(widget.sessionId));
    if (t.items.isEmpty) {
      setState(() => _historyNoteVisible = true);
    }
  }

  /// Apply [SettingsStore.getDefaultSessionMode] when the mode is still
  /// advertised. A stored mode the provider no longer offers is ignored.
  Future<void> _maybeApplyDefaultMode(
    List<SessionMode> modes,
    String? currentModeId,
  ) async {
    try {
      final want = await ref
          .read(settingsStoreProvider)
          .getDefaultSessionMode(_provider);
      if (want == null || want.isEmpty || !mounted) return;
      final match = modes.where((m) => m.id == want).toList();
      if (match.isEmpty) return; // no longer advertised
      if (currentModeId != null && currentModeId == want) return;
      // Codex Plan is not an autonomy SessionMode (MADR 0080 D10).
      if (_provider == 'codex' && want == 'plan') return;
      await ref.read(mcremoteClientProvider).setMode(widget.sessionId, want);
    } catch (_) {
      // Best-effort: a failed apply leaves the provider's own default.
    }
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
      final thinking = meta?.thinkingLevel ?? '';
      final live = meta?.live ?? true;
      if (!mounted) return;
      setState(() {
        if (cwd.isNotEmpty) _cwd = cwd;
        if (provider.isNotEmpty) _provider = provider;
        _thinkingLevel = thinking;
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

  /// Fetch and replay recorded history on open.
  ///
  /// Empty transcripts always pull history. Populated transcripts skip unless
  /// a reconnect gap is suspected (MADR 0056 H-1). Phone-side cache paints
  /// immediately after process death; host history still reconciles when it
  /// arrives (MADR 0018 E1).
  Future<void> _maybeReplayHistory() async {
    final notifier = ref.read(transcriptsProvider.notifier);
    // Phone-side cache paints immediately after process death.
    await notifier.hydrateFromCache(widget.sessionId);
    if (!mounted) return;
    _raiseOpenSeqFloor();
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    final gap = notifier.isGapSuspected(widget.sessionId);
    if (transcript.items.isNotEmpty && !gap) return;
    try {
      await ref
          .read(sessionSynchronizerProvider.notifier)
          .ensureSession(widget.sessionId);
    } catch (_) {
      return;
    }
    if (!mounted) return;
    _raiseOpenSeqFloor();
    final after = ref.read(sessionTranscriptProvider(widget.sessionId));
    if (after.items.isEmpty && !_sessionLive) {
      setState(() => _historyNoteVisible = true);
    }
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
    _titleSub?.cancel();
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
    // Items are seq-ordered, so counting from the tail and stopping at the
    // first already-read row is O(unread) rather than O(items). This runs per
    // append while the user is scrolled up — precisely when the agent is
    // streaming and the transcript is at its 800-item cap.
    var n = 0;
    for (var i = t.items.length - 1; i >= 0; i--) {
      if (t.items[i].seq < _seqAtLeaveBottom) break;
      n++;
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
  void _scrollToEnd({bool force = false}) {
    if (_scrollQueued) return;
    _scrollQueued = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _scrollQueued = false;
      if (!_scroll.hasClients) return;
      // Already pinned. jumpTo(0) would leave the offset alone but still runs
      // goIdle() + goBallistic(), so it is not free.
      if (_scroll.position.pixels == 0) return;
      // The user is dragging, or a fling is still in flight. `jumpTo` begins
      // with `goIdle()`, which terminates the current ScrollActivity — so an
      // unguarded auto-follow yanks the list out from under the gesture. With
      // "near bottom" being a 120px band and an OpenCode tool burst appending
      // several rows a second, that made the transcript impossible to scroll
      // back through while the agent worked (MADR 0042 D5).
      //
      // Nothing needs re-arming: the next append after the gesture ends jumps
      // normally, and _userNearBottom already decides whether it should.
      if (_listScrolling.value && !force) return;
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
          showTopNotification(context, 'Voice input unavailable');
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
        showTopNotification(
          context,
          'Voice input failed: $e',
          severity: NoticeSeverity.error,
        );
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
          showTopNotification(
            context,
            'Copied',
            severity: NoticeSeverity.success,
          );
        }
    }
  }

  /// Pick an image from the gallery and stage it for the next prompt. Uses the
  /// system photo picker (no runtime permission on modern Android). Only shown
  /// when the agent advertises image support (ACP promptCapabilities.image).
  Future<void> _pickImage() async {
    try {
      final file =
          await (debugPickImage?.call() ??
              ImagePicker().pickImage(
                source: ImageSource.gallery,
                maxWidth: 2048,
                imageQuality: 85,
              ));
      if (file == null) return;
      final bytes = await file.readAsBytes();
      final attachment = PromptAttachment(
        kind: 'image',
        mimeType: _mimeForImage(file.path, file.mimeType),
        data: base64Encode(bytes),
        filename: attachmentBasename(file.path),
      );
      final frameBytes = _promptFrameBytes(_composer.text.trim(), [
        ..._pendingAttachments.map((p) => p.attachment),
        attachment,
      ]);
      if (frameBytes > kMaxClientFrameBytes) {
        if (mounted) {
          showTopNotification(
            context,
            _frameTooLargeMessage(frameBytes),
            severity: NoticeSeverity.error,
          );
        }
        return;
      }
      if (!mounted) return;
      setState(() {
        _pendingAttachments.add(
          _PendingAttachment(bytes: bytes, attachment: attachment),
        );
      });
    } catch (e) {
      if (mounted) {
        showTopNotification(
          context,
          'Could not attach image: $e',
          severity: NoticeSeverity.error,
        );
      }
    }
  }

  /// Pick an audio file and stage it for the next prompt. Only shown when the
  /// active model advertises audio input (MADR 0112 A2). Restricted to
  /// [kAcceptedAudioMimeTypes]: an unrecognised container is refused with a
  /// reason rather than sent for the model to reject a turn later.
  Future<void> _pickAudio() async {
    try {
      final file =
          await (debugPickAudio?.call() ??
              openFile(
                acceptedTypeGroups: const [
                  XTypeGroup(
                    label: 'audio',
                    extensions: [
                      'aac',
                      'flac',
                      'm4a',
                      'mp3',
                      'mp4',
                      'oga',
                      'ogg',
                      'opus',
                      'wav',
                      'weba',
                    ],
                    mimeTypes: [...kAcceptedAudioMimeTypes],
                  ),
                ],
              ));
      if (file == null) return;
      final mime = audioMimeForPath(file.path, file.mimeType);
      if (mime.isEmpty) {
        if (mounted) {
          showTopNotification(
            context,
            'That audio format is not supported.',
            severity: NoticeSeverity.error,
          );
        }
        return;
      }
      final bytes = await file.readAsBytes();
      if (bytes.isEmpty) {
        if (mounted) {
          showTopNotification(
            context,
            'That audio file is empty.',
            severity: NoticeSeverity.error,
          );
        }
        return;
      }
      final attachment = PromptAttachment(
        kind: 'audio',
        mimeType: mime,
        data: base64Encode(bytes),
        filename: attachmentBasename(file.path),
      );
      final frameBytes = _promptFrameBytes(_composer.text.trim(), [
        ..._pendingAttachments.map((p) => p.attachment),
        attachment,
      ]);
      if (frameBytes > kMaxClientFrameBytes) {
        if (mounted) {
          showTopNotification(
            context,
            _frameTooLargeMessage(frameBytes),
            severity: NoticeSeverity.error,
          );
        }
        return;
      }
      if (!mounted) return;
      setState(() {
        _pendingAttachments.add(
          _PendingAttachment(bytes: bytes, attachment: attachment),
        );
      });
    } catch (e) {
      if (mounted) {
        showTopNotification(
          context,
          'Could not attach audio: $e',
          severity: NoticeSeverity.error,
        );
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

  int _promptFrameBytes(String text, List<PromptAttachment> attachments) =>
      sessionPromptFrameBytes(
        sessionId: widget.sessionId,
        text: text,
        attachments: [
          for (final attachment in attachments) attachment.toJson(),
        ],
      );

  String _frameTooLargeMessage(int bytes) =>
      'This prompt is ${(bytes / (1024 * 1024)).toStringAsFixed(2)} MB; '
      'the connection limit is 1 MB. Remove an attachment or shorten the message.';

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
                        if (sheetCtx.mounted) {
                          setSheet(() => opts[i] = prev);
                        }
                        if (sheetCtx.mounted) {
                          showTopNotification(
                            sheetCtx,
                            'Update failed: ${friendlyOpError(e)}',
                            severity: NoticeSeverity.error,
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
                        // Through the shared picker, not a bare Column: goose
                        // reports 71 values for its `provider` option, and the
                        // unscrollable column this replaced simply overflowed
                        // (MADR 0043 D12). Search comes for free.
                        final result = await showOptionPicker(
                          sheetCtx,
                          catalog: PickerCatalog(
                            options: [
                              for (final v in o.values)
                                PickerOption(id: v.id, label: v.name),
                            ],
                            defaultIds: o.currentValue.isEmpty
                                ? const []
                                : [o.currentValue],
                          ),
                          title: o.name,
                          initialSelected: o.currentValue.isEmpty
                              ? const []
                              : [o.currentValue],
                        );
                        final chosen = result?.single;
                        if (chosen == null || chosen.isEmpty) return;
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

  /// Bare `/model` opens the model picker instead of going to the daemon.
  ///
  /// Returns true when it handled the input. The picker is purely a text
  /// composition aid: on confirm this re-enters [_send] with `/model <id>`, so
  /// the daemon executes exactly the command it always has — in-place switch,
  /// relaunch fallback, notice text and transcript echo all unchanged
  /// (MADR 0043 D9). Typing `/model <id>` yourself skips this entirely.
  ///
  /// Gated on `remote_commands`: the daemon decides whether `/model` works in
  /// this session (MADR 0023), and when it says no the text goes through so the
  /// user gets the daemon's explanation rather than a picker for a command that
  /// cannot run.
  Future<bool> _maybeInterceptModelCommand(String text) async {
    if (text.trim().toLowerCase() != '/model') return false;
    // A second tap while the catalog is loading or its picker is up must not
    // fall through and send a bare /model to the daemon.
    if (_interceptingModel) return true;
    final transcript = ref.read(sessionTranscriptProvider(widget.sessionId));
    final available = transcript.remoteCommands.any(
      (c) => c.name.toLowerCase() == 'model' && c.available,
    );
    if (!available) return false;
    if (_provider.isEmpty) return false;

    try {
      setState(() => _interceptingModel = true);
      final client = ref.read(mcremoteClientProvider);
      PickerCatalog catalog;
      try {
        catalog = await client.listModels(
          _provider,
          sessionId: widget.sessionId,
        );
      } catch (e) {
        if (!mounted) return false;
        showTopNotification(
          context,
          'Could not load models: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
        // Fall through to the daemon, which answers with the current model.
        return false;
      }
      if (!mounted) return true;
      if (catalog.options.isEmpty) {
        // Nothing to choose from — let the daemon print the current model.
        return false;
      }
      final scope = catalog.modelProvider.isEmpty
          ? _provider
          : catalog.modelProvider;
      String? thinkingIntent;
      try {
        thinkingIntent = await ref
            .read(settingsStoreProvider)
            .getDefaultThinkingLevel();
      } catch (e) {
        // best-effort: chip preselect only.
        debugPrint('chat: default thinking level failed: $e');
      }
      if (!mounted) return true;
      final chosen = await showOptionPicker(
        context,
        catalog: catalog,
        title: 'Model · $scope',
        initialSelected: catalog.defaultIds,
        thinkingIntent: thinkingIntent,
      );
      if (chosen == null || !mounted) return true;
      final id = chosen.single ?? '';
      if (id.isEmpty) return true;
      _composer.text = '/model $id';
      await _send();
      // Apply a thinking level chosen in the picker (codex next-turn).
      final level = chosen.thinkingLevel;
      if (level != null && level.isNotEmpty && mounted) {
        try {
          await ref
              .read(mcremoteClientProvider)
              .setThinkingLevel(widget.sessionId, level);
          if (mounted) setState(() => _thinkingLevel = level);
        } catch (e) {
          // best-effort: picker still shows selection; next turn may retry.
          debugPrint('chat: setThinkingLevel failed: $e');
        }
      }
      return true;
    } finally {
      if (mounted) setState(() => _interceptingModel = false);
    }
  }

  Future<void> _send() async {
    final text = _composer.text.trim();
    final attachments = [for (final p in _pendingAttachments) p.attachment];
    if (text.isEmpty && attachments.isEmpty) return;
    if (attachments.isEmpty && await _maybeInterceptModelCommand(text)) {
      return;
    }
    if (!mounted) return;
    if (_listening) {
      // Sending is a natural end to dictation.
      unawaited(_speech.stop());
      setState(() => _listening = false);
    }
    final frameBytes = _promptFrameBytes(text, attachments);
    if (frameBytes > kMaxClientFrameBytes) {
      showTopNotification(
        context,
        _frameTooLargeMessage(frameBytes),
        severity: NoticeSeverity.error,
      );
      return;
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
      final queued = _QueuedPrompt(
        text: text,
        attachments: attachments,
        imageBytes: [
          for (final a in _pendingAttachments)
            if (a.isImage) a.bytes,
        ],
      );
      setState(() {
        _queuedPrompts.add(queued);
        _pendingAttachments.clear();
      });
      _composer.clear();
      // Drop the keyboard so the transcript can use the full height while the
      // agent works; user re-taps the field to queue another prompt.
      _focus.unfocus();
      unawaited(HapticFeedback.lightImpact());
      return;
    }
    _composer.clear();
    _focus.unfocus();
    unawaited(HapticFeedback.lightImpact());
    // Attachments ride the direct send only (attach is disabled while busy, so
    // they never queue). Stage the bytes so the user_message echo can fold a
    // real thumbnail into the bubble, then clear the composer strip.
    final pendingImages = List<_PendingAttachment>.from(_pendingAttachments);
    final images = [
      for (final p in pendingImages)
        if (p.isImage) p.bytes,
    ];
    if (images.isNotEmpty) {
      ref
          .read(transcriptsProvider.notifier)
          .stageSentImages(widget.sessionId, images);
      setState(() => _pendingAttachments.clear());
    }
    await _sendText(
      text,
      attachments: attachments,
      stagedImages: images.isNotEmpty,
      restoreComposerOnFailure: true,
      restoreImagesOnFailure: pendingImages,
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
    List<_PendingAttachment> restoreImagesOnFailure = const [],
    _QueuedPrompt? requeuePromptOnFailure,
  }) async {
    setState(() => _sending = true);
    // Read both before the await. `ref` is unusable once this element is
    // unmounted — riverpod throws a StateError, in release builds too — and
    // backing out of the chat while a prompt is in flight is exactly when the
    // failure path below needs the notifier (MADR 0046 H-D).
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);
    try {
      await client.prompt(widget.sessionId, text, attachments: attachments);
      // Guard the async gap: backing out of the chat mid-request disposes
      // the controller and tears down this State.
      if (!mounted) return;
      _userNearBottom.value = true;
      _unreadWhileScrolledUp.value = 0;
      _scrollToEnd();
    } catch (e) {
      // The user_message echo never arrived; drop the staged bytes so they
      // cannot mis-attach to a later turn. Unconditional: the staged batch
      // outlives this screen, so skipping it when unmounted is what let the
      // thumbnails resurface on the next image message.
      if (stagedImages) {
        transcripts.unstageSentImages(widget.sessionId);
      }
      if (mounted) {
        if (restoreComposerOnFailure && _composer.text.trim().isEmpty) {
          _composer.text = text;
          _composer.selection = TextSelection.collapsed(offset: text.length);
        }
        if (restoreImagesOnFailure.isNotEmpty) {
          setState(
            () => _pendingAttachments.insertAll(0, restoreImagesOnFailure),
          );
        }
        if (requeuePromptOnFailure != null) {
          setState(() => _queuedPrompts.insert(0, requeuePromptOnFailure));
        }
        final msg = friendlyOpError(e);
        // MADR 0095 D3: showTopNotification inserts into the ROOT overlay
        // ("survives route changes for the app's lifetime",
        // theme/top_notification.dart), and an action notification stays up
        // for 6 s. `mounted` on this State is therefore not a proxy for "my
        // route is on top": tapped during the exit transition the old
        // Navigator.pop() popped /sessions and emptied the router (the MADR
        // 0094 D1 defect), and tapped afterwards it did nothing at all.
        // GoRouter outlives every route, so capture it here.
        final router = GoRouter.of(context);
        showTopNotification(
          context,
          'Send failed: $msg',
          severity: NoticeSeverity.error,
          actionLabel: 'Sessions',
          onAction: () => router.go('/sessions'),
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
      if (queued.imageBytes.isNotEmpty) {
        ref
            .read(transcriptsProvider.notifier)
            .stageSentImages(widget.sessionId, queued.imageBytes);
      }
      await _sendText(
        queued.text,
        attachments: queued.attachments,
        stagedImages: queued.imageBytes.isNotEmpty,
        requeuePromptOnFailure: queued,
      );
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
        showTopNotification(
          context,
          'Cancel failed: $e',
          severity: NoticeSeverity.error,
        );
      }
    }
  }

  /// OpenCode-only: pull file-change summary (also lands as a notice).
  Future<void> _viewDiagnostics() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      showTopNotification(context, 'Reconnect to the host first');
      return;
    }
    try {
      final diagnostics = await client.sessionDiagnostics(widget.sessionId);
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (ctx) => _DiagnosticsDialog(diagnostics: diagnostics),
      );
    } catch (e) {
      if (mounted) {
        showTopNotification(
          context,
          'Diagnostics failed: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
      }
    }
  }

  /// OpenCode-only: pull file-change summary (also lands as a notice).
  Future<void> _viewDiff() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      showTopNotification(context, 'Reconnect to the host first');
      return;
    }
    try {
      final result = await client.sessionDiff(widget.sessionId);
      if (!mounted) return;
      if (result.summary.isEmpty && !result.truncated) {
        showTopNotification(context, 'No file changes');
        return;
      }
      final body = StringBuffer();
      if (result.scope.isNotEmpty) {
        body.writeln('Scope: ${result.scope}');
      }
      if (result.baseSha.isNotEmpty) {
        body.writeln('Base: ${result.baseSha}');
      }
      if (result.truncated) {
        body.writeln('[diff truncated]');
      }
      if (body.isNotEmpty) body.writeln();
      body.writeln('```diff');
      body.writeln(result.summary);
      body.writeln('```');
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Session diff'),
          content: SingleChildScrollView(
            child: SelectableText(
              body.toString(),
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
        showTopNotification(
          context,
          'Diff failed: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
      }
    }
  }

  /// Opens the Codex execution terminal view for this session.
  Future<void> _viewTerminals() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      showTopNotification(context, 'Reconnect to the host first');
      return;
    }
    final sid = widget.sessionId;
    if (sid.isEmpty || !mounted) return;
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => CodexTerminalsScreen(client: client, sessionId: sid),
      ),
    );
  }

  /// OpenCode-only: fork conversation into a new mcremote session and open it.
  Future<void> _forkSession() async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      showTopNotification(context, 'Reconnect to the host first');
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
        showTopNotification(
          context,
          'Fork failed: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
      }
    }
  }

  Future<void> _endSession() async {
    // Re-opening the menu mid-delete otherwise runs a second cancel/delete
    // pair: the loser fails with "unknown session" and puts a spurious
    // "End session failed" on top of the success toast (MADR 0046 L-9). The
    // sessions list guards its own end action the same way.
    if (_endingSession) return;
    _endingSession = true;
    try {
      await _endSessionFlow();
    } finally {
      if (mounted) _endingSession = false;
    }
  }

  Future<void> _endSessionFlow() async {
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
      showTopNotification(
        context,
        'Reconnect to the host first — the session lives there.',
      );
      return;
    }
    try {
      try {
        await client.cancel(widget.sessionId);
      } catch (e) {
        // best-effort: cancel may fail if the turn already ended; delete is
        // the user-visible primary action.
        debugPrint('chat: cancel before delete failed: $e');
      }
      // session.delete closes the live session and purges the disk record.
      // closeSession alone leaves the record, so the row would reappear on
      // the next session.list — the dialog promises removal.
      await client.deleteSession(widget.sessionId);
      // Its pending asks died with it, and the daemon sends no resolution for
      // them, so the notifications have to be pulled here (MADR 0046 M-4).
      _notifCoord?.dropSessionAsks(widget.sessionId);
      _completeEndSessionFlow();
    } catch (e) {
      if (!mounted) return;
      // D7 (MADR 0094): the host may have purged the session even though
      // the RPC errored — session.delete is idempotentRetry, and the
      // daemon errors on a double delete, so a lost ok surfaces here.
      // Confirm against the host list once; treat a confirmed purge as
      // ended.
      var rowSurvives = true;
      try {
        final snap = await client.listSessionSnapshot();
        // MADR 0095 D2: only a COMPLETE snapshot is evidence of removal.
        // The daemon returns a successful session.list_result with
        // complete=false when its store enumeration skipped rows
        // (internal/session/manager.go ListSnapshot), and pruning local
        // state from a partial list is exactly what MADR 0056 H-6 forbids
        // — the rule TranscriptsNotifier.syncFromMeta already states.
        rowSurvives =
            !snap.complete ||
            snap.sessions.any((s) => s.id == widget.sessionId);
      } catch (_) {
        // Conservative: an unreadable list cannot confirm the purge.
      }
      // The confirming read awaited; the user may have left meanwhile.
      if (!mounted) return;
      if (rowSurvives) {
        showTopNotification(
          context,
          'End session failed: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
      } else {
        _completeEndSessionFlow();
      }
    }
  }

  /// Shared completion tail for the success path and the D7
  /// confirmed-purge path (MADR 0094): clear local state, toast, and
  /// navigate.
  ///
  /// The branch is chosen from the chat route's own state, not from a list
  /// of modal kinds (MADR 0095 D4). Enumerating sheets was unbounded by
  /// construction — every new route this screen can open was another way
  /// to miss the landing (0095 F3), and `clearSession`'s `removeRoute`
  /// does not restore `isCurrent` within the frame anyway (0094 D6 probe).
  ///
  /// Measured at guard time (0095 Step 6 probe, Flutter SDK in use):
  ///
  /// | situation | isActive | isCurrent |
  /// | --- | --- | --- |
  /// | nothing above the chat | true | true |
  /// | user popped the chat mid-delete | false | false |
  /// | untracked dialog / pushed forked chat / permission sheet | true | false |
  void _completeEndSessionFlow() {
    if (!mounted) return;
    // Clear local state only once the host actually deleted it.
    ref.read(transcriptsProvider.notifier).clearSession(widget.sessionId);
    showTopNotification(
      context,
      'Session ended',
      severity: NoticeSeverity.success,
    );
    final route = ModalRoute.of(context);
    if (route != null && route.isCurrent) {
      // Nothing is above us: the ordinary exit, with its pop animation and
      // didPopNext refresh (MADR 0094 D1/D3).
      Navigator.of(context).pop(true);
      return;
    }
    ref.read(sessionsRevisionProvider.notifier).bump();
    if (route != null && route.isActive) {
      // Still in the navigator with something above it — a sheet, a
      // dialog, or a pushed forked chat. A go exit is deterministic and
      // cannot pop the last page; it does not fire didPopNext, so the
      // bump above owns the landing-screen refresh.
      context.go('/sessions');
    }
    // Otherwise the user popped us and is already on the landing screen;
    // the bump is the whole job.
  }

  /// Second confirmation for a broad "always" grant, so it can't be tapped by
  /// mistake as easily as a one-time allow.
  Future<bool> _confirmAlways(BuildContext ctx, String label) async {
    try {
      final ok = await showDialog<bool>(
        context: ctx,
        builder: (dctx) {
          final route = ModalRoute.of(dctx);
          if (route is ModalRoute<bool?>) _openAlwaysConfirmRoute = route;
          return AlertDialog(
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
          );
        },
      );
      return ok ?? false;
    } finally {
      _openAlwaysConfirmRoute = null;
    }
  }

  /// Retire the exact routes owned by an externally resolved permission.
  ///
  /// The confirmation dialog has a bool result while the sheet has a String
  /// result. Popping the navigator top with the sheet sentinel while a dialog
  /// is up is a runtime type error, so remove each captured route explicitly.
  void _dismissPermissionSheetExternally() {
    final confirm = _openAlwaysConfirmRoute;
    _openAlwaysConfirmRoute = null;
    if (confirm != null && confirm.isActive) {
      confirm.navigator?.removeRoute(confirm, false);
    }
    final sheet = _openPermissionSheetRoute;
    _openPermissionSheetRoute = null;
    if (sheet != null && sheet.isActive) {
      sheet.navigator?.removeRoute(sheet, '__external__');
    }
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
        final route = ModalRoute.of(ctx);
        if (route != null) _openPermissionSheetRoute = route;
        // Registered so an externally resolved request (answered on another
        // device, turn cancelled) can retire its own stale sheet.
        _dismissSheet = _dismissPermissionSheetExternally;
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
                        // A command or path fits in 160px; grok's plan approval
                        // puts a whole plan document here, and reviewing one
                        // through a four-line window is not reviewing it. Long
                        // details get a larger share of the sheet (which is
                        // itself capped at 90% of the screen and scrolls).
                        constraints: BoxConstraints(
                          maxHeight: detail.length > longPermissionDetail
                              ? MediaQuery.of(ctx).size.height * 0.42
                              : 160,
                        ),
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
                          child: SelectableText(
                            detail,
                            key: const Key('permission-detail'),
                            style: monoDetail,
                          ),
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
    _openPermissionSheetRoute = null;
    _openAlwaysConfirmRoute = null;

    final permissionId = ev.permissionId;
    if (result == null || permissionId == null) return;
    if (result == '__external__') {
      // Resolved elsewhere (other device / cancelled turn): nothing to send.
      if (mounted) {
        showTopNotification(context, 'Request was resolved elsewhere');
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
      // Do NOT forget that we presented this one. The sheet's dismissal runs
      // `_maybeShowPermission` again, so an id removed here is re-presented
      // immediately — and when the failure is permanent (the daemon already
      // retired the request, so every answer returns "unknown permission")
      // that is an inescapable modal loop: approve, fail, re-present, approve.
      // The request stays outstanding and the "Waiting for permission" banner's
      // Review button clears the presented set for a deliberate retry, so a
      // transient failure is still recoverable — by the user, not by a loop.
      if (mounted) {
        showTopNotification(
          context,
          'Permission respond failed: ${friendlyOpError(e)} — tap Review to retry',
          severity: NoticeSeverity.error,
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

    final result = await showModalBottomSheet<Object?>(
      context: context,
      isDismissible: false,
      enableDrag: false,
      isScrollControlled: true,
      builder: (ctx) {
        _dismissSheet = () {
          if (ctx.mounted) Navigator.pop(ctx, '__external__');
        };
        final title = (ev.text ?? '').trim().isEmpty
            ? 'Agent question'
            : ev.text!.trim();
        return QuestionSheet(
          title: title,
          items: items,
          onSubmit: (answers) => Navigator.pop(ctx, answers),
          onCancel: () => Navigator.pop(ctx, '__cancel__'),
        );
      },
    );
    _dismissSheet = null;
    _openSheetQuestionId = null;

    final questionId = ev.questionId;
    if (result == null || questionId == null) return;
    if (result == '__external__') {
      if (mounted) {
        showTopNotification(context, 'Request was resolved elsewhere');
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
      } else if (result is Map<String, List<String>>) {
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
      if (mounted) {
        showTopNotification(
          context,
          'Question respond failed: ${friendlyOpError(e)}',
          severity: NoticeSeverity.error,
        );
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
    final subagents = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.subagents),
    );
    final goal = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.goal),
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
    // Audio follows the same rule: the affordance appears only when the active
    // model advertises audio input, and the daemon re-emits capabilities after
    // a /model switch so this flips without a restart (MADR 0112 A2).
    final canAttachAudio = ref.watch(
      sessionTranscriptProvider(
        sid,
      ).select((t) => t.capabilities?.audio ?? false),
    );
    final modes = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.modes),
    );
    final currentModeId = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.currentModeId),
    );
    final collaborationModes = ref.watch(
      sessionTranscriptProvider(sid).select((t) => t.collaborationModes),
    );
    final currentCollaborationModeId = ref.watch(
      sessionTranscriptProvider(
        sid,
      ).select((t) => t.currentCollaborationModeId),
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
    final sendWithEnter = ref.watch(sendWithEnterProvider);

    final title = _title.isNotEmpty
        ? _title
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

    // Apply the per-provider default mode once modes arrive (MADR 0052 B2).
    if (!_appliedDefaultMode &&
        modes.isNotEmpty &&
        _provider.isNotEmpty &&
        !offline) {
      _appliedDefaultMode = true;
      unawaited(_maybeApplyDefaultMode(modes, currentModeId));
    }
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
          if (collaborationModes.isNotEmpty)
            _CollaborationSelector(
              sessionId: sid,
              modes: collaborationModes,
              currentModeId: currentCollaborationModeId,
              enabled: !offline && !busy,
            ),
          if (modes.isNotEmpty)
            _ModeSelector(
              sessionId: sid,
              modes: modes,
              currentModeId: currentModeId,
              enabled: !offline,
              permissionsLabel: _provider == 'codex',
            ),
          // Thinking level (MADR 0052) — only when /thinking is available.
          if (remoteCommands.any(
            (c) => c.name.toLowerCase() == 'thinking' && c.available,
          ))
            _ThinkingSelector(
              sessionId: sid,
              provider: _provider,
              currentLevel: _thinkingLevel,
              enabled: !offline,
              onLevelChanged: (level) {
                if (mounted) setState(() => _thinkingLevel = level);
              },
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
              if (v == 'diagnostics') unawaited(_viewDiagnostics());
              if (v == 'fork') unawaited(_forkSession());
              if (v == 'terminals') unawaited(_viewTerminals());
            },
            itemBuilder: (ctx) {
              // Diagnostics/diff/fork are httpagent-family session ops (MADR
              // 0075 §1.2): opencode and kilo share the same backend, so both
              // get the menu items (MADR 0076 H1 — this used to be an
              // opencode-only check that silently hid working kilo features).
              final remotes = ref
                  .read(sessionTranscriptProvider(sid))
                  .remoteCommands;
              final useRemote = remotes.isNotEmpty;
              final showsDiff = useRemote
                  ? remotes.any((c) => c.name == 'diff' && c.available)
                  : (_provider == 'opencode' || _provider == 'kilo');
              final showsFork = useRemote
                  ? remotes.any((c) => c.name == 'fork' && c.available)
                  : (_provider == 'opencode' || _provider == 'kilo');
              final showsDiagnostics =
                  _provider == 'opencode' || _provider == 'kilo';
              // Terminals exist only where the daemon exposes the Codex
              // execution surface; without the negotiated capability the
              // operations would be refused, so the entry point is hidden
              // rather than offered and then failed (MADR 0109 P7).
              final showsTerminals =
                  _provider == 'codex' &&
                  ref.read(mcremoteClientProvider).serverCaps?.codexSurface !=
                      null;
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
                if (showsDiagnostics)
                  const PopupMenuItem(
                    value: 'diagnostics',
                    child: ListTile(
                      leading: Icon(Icons.info_outline),
                      title: Text('Session diagnostics'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                    ),
                  ),
                if (showsDiff)
                  const PopupMenuItem(
                    value: 'diff',
                    child: ListTile(
                      leading: Icon(Icons.difference_outlined),
                      title: Text('View file diff'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                    ),
                  ),
                if (showsFork)
                  const PopupMenuItem(
                    value: 'fork',
                    child: ListTile(
                      leading: Icon(Icons.call_split),
                      title: Text('Fork session'),
                      contentPadding: EdgeInsets.zero,
                      dense: true,
                    ),
                  ),
                if (showsTerminals)
                  const PopupMenuItem(
                    value: 'terminals',
                    child: ListTile(
                      leading: Icon(Icons.terminal),
                      title: Text('Terminals'),
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
              ];
            },
          ),
        ],
      ),
      body: Column(
        children: [
          // Chat is where a dead link is actually noticed: a prompt is sent and
          // nothing comes back. Before this the composer looked perfectly
          // healthy through a blackholed connection, so the silence read as
          // the agent hanging (MADR 0063 D5, review decision 2).
          //
          // Read through the notifier rather than a provider: the same pattern
          // as `hostInputListenable`, and it keeps the rebuild scoped to the
          // banner instead of the whole screen.
          ValueListenableBuilder<LinkHealth>(
            valueListenable: ref.read(mcremoteClientProvider).linkHealth,
            builder: (context, linkHealth, _) {
              final degraded =
                  connState == McConnectionState.connected &&
                  linkHealth != LinkHealth.fresh;
              return BannerSlot(
                child: linking
                    ? const ConnBanner(
                        key: ValueKey('linking'),
                        kind: ConnBannerKind.linking,
                        message: 'Connecting to host…',
                      )
                    : degraded
                    ? ConnBanner(
                        key: const ValueKey('degraded'),
                        kind: ConnBannerKind.degraded,
                        message: linkHealth == LinkHealth.lost
                            ? 'Connection lost — reconnecting…'
                            : 'Checking connection…',
                        subtitle: 'The host has not answered recently.',
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
                            try {
                              final store = ref.read(settingsStoreProvider);
                              // User-tapped retry keeps its own fallback budget
                              // (MADR 0062 A5).
                              await ref
                                  .read(mcremoteClientProvider)
                                  .reconnectFromStore(
                                    store,
                                    userInitiated: true,
                                  );
                            } catch (e) {
                              if (context.mounted) {
                                showTopNotification(
                                  context,
                                  'Reconnect failed: ${friendlyOpError(e)}',
                                  severity: NoticeSeverity.error,
                                );
                              }
                            }
                          },
                          child: const Text('Retry now'),
                        ),
                      )
                    : null,
              );
            },
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
                                // Forced: an explicit tap outranks a fling
                                // still in flight. Unforced, the jump was
                                // swallowed by the gesture guard while the
                                // unread state below had already been reset —
                                // the button vanished and went nowhere
                                // (MADR 0046 M-9).
                                _scrollToEnd(force: true);
                                _userNearBottom.value = true;
                                _unreadWhileScrolledUp.value = 0;
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
          // The composer and everything stacked above it stay inside the body,
          // NOT in Scaffold.bottomNavigationBar. Scaffold lays the bottom bar
          // out at `size.height - barHeight` and only applies viewInsets to the
          // body, so a composer in that slot renders *behind* the soft keyboard
          // and you cannot see what you type. Notifications no longer collide
          // with it — they render as a top overlay (TopNotification), not as
          // bottom snackbars.
          // Compact, collapsible panels above the composer. Both are kept out
          // of the scrolling transcript and hidden entirely when empty.
          // Sub-agents sit above the plan: they are the more transient of the
          // two, and their output is deliberately absent from chat, so this is
          // the only place the user learns work is happening off-screen.
          if (subagents.isNotEmpty) SubagentsPanel(entries: subagents),
          if (plan.isNotEmpty) WorkItemsPanel(entries: plan),
          if (goal != null)
            Material(
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              child: ListTile(
                dense: true,
                leading: const Icon(Icons.flag_outlined, size: 18),
                title: Text(
                  'Goal · ${goal.status.isEmpty ? 'active' : goal.status}',
                  style: Theme.of(context).textTheme.labelMedium,
                ),
                subtitle: Text(goal.displayObjective),
                trailing: goal.tokenBudget > 0
                    ? Text('${goal.tokenUsage}/${goal.tokenBudget}')
                    : null,
              ),
            ),
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
                          queued.label,
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
          if (_pendingAttachments.isNotEmpty)
            SafeArea(
              top: false,
              bottom: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(12, 4, 12, 0),
                child: SizedBox(
                  height: 64,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    itemCount: _pendingAttachments.length,
                    separatorBuilder: (_, _) => const SizedBox(width: 8),
                    itemBuilder: (ctx, i) {
                      final staged = _pendingAttachments[i];
                      void remove() =>
                          setState(() => _pendingAttachments.removeAt(i));
                      if (staged.isAudio) {
                        return _AudioAttachmentChip(
                          filename: staged.attachment.filename,
                          byteCount: staged.bytes.length,
                          onRemove: remove,
                        );
                      }
                      return _AttachmentThumb(
                        bytes: staged.bytes,
                        onRemove: remove,
                      );
                    },
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
                      // New attachments wait for an idle composer, but any
                      // already staged images move atomically with a queued
                      // prompt when the session becomes busy.
                      onPressed: (busy || offline) ? null : _pickImage,
                    ),
                  if (canAttachAudio)
                    IconButton(
                      key: const ValueKey('attach-audio'),
                      tooltip: 'Attach audio',
                      icon: const Icon(Icons.audiotrack_outlined),
                      onPressed: (busy || offline) ? null : _pickAudio,
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
                      textInputAction: sendWithEnter
                          ? TextInputAction.send
                          : TextInputAction.newline,
                      onSubmitted: sendWithEnter ? (_) => _send() : null,
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
                      final hasDraft =
                          value.text.trim().isNotEmpty ||
                          _pendingAttachments.isNotEmpty;
                      final Widget button;
                      if (busy && hasDraft) {
                        button = IconButton.filled(
                          key: const ValueKey('queue'),
                          tooltip: 'Queue message',
                          onPressed: (offline || _interceptingModel)
                              ? null
                              : _send,
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
                          onPressed: (offline || _interceptingModel)
                              ? null
                              : _send,
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

class _DiagnosticsDialog extends StatelessWidget {
  const _DiagnosticsDialog({required this.diagnostics});

  final SessionDiagnostics diagnostics;

  @override
  Widget build(BuildContext context) {
    final vcs = diagnostics.vcs;
    final sections = <Widget>[
      if (diagnostics.branch.isNotEmpty)
        ListTile(
          dense: true,
          leading: const Icon(Icons.account_tree_outlined),
          title: Text(diagnostics.branch),
          subtitle: diagnostics.defaultBranch.isEmpty
              ? null
              : Text('Default: ${diagnostics.defaultBranch}'),
        ),
      if (vcs != null && !vcs.isEmpty)
        ListTile(
          dense: true,
          leading: const Icon(Icons.change_circle_outlined),
          title: Text(
            '${vcs.added} added · ${vcs.modified} modified · ${vcs.deleted} deleted',
          ),
          subtitle: Text('+${vcs.additions} · -${vcs.deletions}'),
        ),
      if (diagnostics.mcp.isNotEmpty) ...[
        const Padding(
          padding: EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Text('MCP servers'),
        ),
        for (final server in diagnostics.mcp)
          ListTile(
            dense: true,
            leading: const Icon(Icons.extension_outlined),
            title: Text(server.name),
            trailing: Text(server.state),
          ),
      ],
    ];
    return AlertDialog(
      title: const Text('Session diagnostics'),
      content: SizedBox(
        width: double.maxFinite,
        child: sections.isEmpty
            ? const Text('No diagnostics are available for this session.')
            : ListView(shrinkWrap: true, children: sections),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Close'),
        ),
      ],
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
    if (usage == null || (usage.size <= 0 && usage.used <= 0)) {
      return const SizedBox.shrink();
    }

    final scheme = Theme.of(context).colorScheme;
    final tokens = celestialOf(context);
    final f = usage.size > 0 ? usage.fraction : 0.0;
    // Escalate as the window fills: neutral < 75% <= gold < 90% <= error.
    final color = f >= 0.9
        ? scheme.error
        : f >= 0.75
        ? tokens.gold
        : scheme.onSurfaceVariant;
    final pct = (f * 100).round();
    final label = usage.size > 0 ? '$pct%' : '${usage.used}';
    final tooltip = usage.size > 0
        ? '$pct% of context used\n${usage.used} / ${usage.size} tokens'
        : '${usage.used} context tokens used';

    return Center(
      child: Tooltip(
        message: tooltip,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 6),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.data_usage, size: 14, color: color),
              const SizedBox(width: 4),
              Text(
                label,
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
    this.permissionsLabel = false,
  });

  final String sessionId;
  final List<SessionMode> modes;
  final String? currentModeId;
  final bool enabled;
  final bool permissionsLabel;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Never invent a selection from list order alone (MADR 0047 D4).
    final current =
        resolveDisplayedMode(modes, currentModeId) ??
        const SessionMode(id: '', name: '');
    return PopupMenuButton<String>(
      enabled: enabled,
      tooltip: permissionsLabel ? 'Permissions' : 'Agent mode',
      onSelected: (id) async {
        // Switching *into* a mode that answers permissions for the user is
        // one tap from the chat screen, so confirm it. Switching away needs
        // no confirmation.
        final target = modes.firstWhere(
          (m) => m.id == id,
          orElse: () => const SessionMode(id: '', name: ''),
        );
        // Read before the confirmation await: the screen can be torn out from
        // under an open dialog (invalid-token redirect, sign-out), and `ref`
        // throws once this element is gone (MADR 0046 L-8).
        final client = ref.read(mcremoteClientProvider);
        if (target.dangerous) {
          final confirmed = await _confirmDangerousMode(context, target);
          if (!confirmed) return;
        }
        try {
          await client.setMode(sessionId, id);
          if (!context.mounted) return;
        } catch (e) {
          if (context.mounted) {
            showTopNotification(
              context,
              'Mode change failed: ${friendlyOpError(e)}',
              severity: NoticeSeverity.error,
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
      child: _ModeChip(mode: current, permissionsLabel: permissionsLabel),
    );
  }
}

/// Independent Plan/Default control. Never uses the dangerous confirmation.
class _CollaborationSelector extends ConsumerWidget {
  const _CollaborationSelector({
    required this.sessionId,
    required this.modes,
    required this.currentModeId,
    required this.enabled,
  });

  final String sessionId;
  final List<CollaborationMode> modes;
  final String? currentModeId;
  final bool enabled;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final currentId = currentModeId?.trim() ?? '';
    CollaborationMode current;
    if (modes.isEmpty) {
      current = const CollaborationMode(id: '', name: 'Default');
    } else {
      current = modes.firstWhere(
        (m) => m.id == currentId,
        orElse: () => modes.firstWhere(
          (m) => m.id == 'default',
          orElse: () => modes.first,
        ),
      );
    }
    final planning = current.id.toLowerCase() == 'plan';
    return PopupMenuButton<String>(
      enabled: enabled,
      tooltip: 'Plan / Default',
      onSelected: (id) async {
        final client = ref.read(mcremoteClientProvider);
        try {
          await client.setCollaborationMode(sessionId, id);
        } catch (e) {
          if (context.mounted) {
            showTopNotification(
              context,
              'Plan change failed: ${friendlyOpError(e)}',
              severity: NoticeSeverity.error,
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
      child: _CollaborationChip(label: planning ? 'Plan' : current.name),
    );
  }
}

class _CollaborationChip extends StatelessWidget {
  const _CollaborationChip({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final planning = label.toLowerCase() == 'plan';
    return Semantics(
      button: true,
      label: 'Collaboration mode $label',
      child: Container(
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
              Icon(Icons.edit_off, size: 16, color: scheme.onTertiaryContainer),
              const SizedBox(width: 4),
            ],
            Text(
              label,
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                color: planning ? scheme.onTertiaryContainer : null,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Confirms arming a mode that answers permission requests without the user.
///
/// Returns false when the dialog is dismissed by any route (cancel, back
/// gesture, barrier tap), so the default is always "do not arm".
Future<bool> _confirmDangerousMode(
  BuildContext context,
  SessionMode mode,
) async {
  final scheme = Theme.of(context).colorScheme;
  final ok = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      icon: Icon(Icons.bolt, color: scheme.error),
      title: const Text('Run without approvals?'),
      content: Text(
        'This session will approve every permission request automatically, '
        'including file edits and shell commands.\n\n'
        '${mode.description.isEmpty ? '' : '${mode.description}\n\n'}'
        'It stays on until you switch modes.',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(
            backgroundColor: scheme.error,
            foregroundColor: scheme.onError,
          ),
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text('Turn on'),
        ),
      ],
    ),
  );
  return ok ?? false;
}

/// Thinking-level chip beside the mode switcher (MADR 0052 A6).
///
/// Grok locks the level at spawn — the chip is read-only with a "new sessions
/// only" hint. Codex (and fake) can change it mid-session via `/thinking`.
class _ThinkingSelector extends ConsumerWidget {
  const _ThinkingSelector({
    required this.sessionId,
    required this.provider,
    required this.currentLevel,
    required this.enabled,
    required this.onLevelChanged,
  });

  final String sessionId;
  final String provider;
  final String currentLevel;
  final bool enabled;
  final ValueChanged<String> onLevelChanged;

  bool get _spawnOnly => provider.toLowerCase() == 'grok';

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final label = currentLevel.isEmpty ? 'thinking' : currentLevel;
    final scheme = Theme.of(context).colorScheme;
    return InkWell(
      onTap: !enabled
          ? null
          : () async {
              if (_spawnOnly) {
                showTopNotification(
                  context,
                  'Grok applies thinking level at session start — '
                  'start a new session to change it.',
                );
                return;
              }
              final client = ref.read(mcremoteClientProvider);
              // Prefer the model's advertised ladder; fall back to the three
              // universal names if the catalog is unavailable.
              var levels = <ThinkingLevel>[
                const ThinkingLevel(id: 'low'),
                const ThinkingLevel(id: 'medium'),
                const ThinkingLevel(id: 'high'),
              ];
              try {
                final cat = await client.listModels(
                  provider,
                  sessionId: sessionId,
                );
                for (final o in cat.options) {
                  if (o.thinkingLevels.isNotEmpty) {
                    // Prefer the current session model when known from defaults.
                    if (cat.defaultIds.contains(o.id) ||
                        o.thinkingLevels.isNotEmpty) {
                      levels = o.thinkingLevels;
                      if (cat.defaultIds.contains(o.id)) break;
                    }
                  }
                }
              } catch (e) {
                // best-effort: fall back to universal low/medium/high.
                debugPrint('chat: listModels for thinking levels failed: $e');
              }
              if (!context.mounted) return;
              final choice = await showDialog<String>(
                context: context,
                builder: (ctx) => SimpleDialog(
                  title: const Text('Thinking level'),
                  children: [
                    for (final l in levels)
                      ListTile(
                        title: Text(l.displayLabel),
                        subtitle: l.description.isEmpty
                            ? null
                            : Text(l.description),
                        trailing: l.id == currentLevel
                            ? const Icon(Icons.check)
                            : null,
                        onTap: () => Navigator.pop(ctx, l.id),
                      ),
                  ],
                ),
              );
              if (choice == null || choice.isEmpty || !context.mounted) return;
              try {
                await client.setThinkingLevel(sessionId, choice);
                onLevelChanged(choice);
              } catch (e) {
                if (context.mounted) {
                  showTopNotification(
                    context,
                    'Thinking level failed: ${friendlyOpError(e)}',
                    severity: NoticeSeverity.error,
                  );
                }
              }
            },
      borderRadius: BorderRadius.circular(12),
      child: Container(
        margin: const EdgeInsets.symmetric(horizontal: 4),
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.psychology_outlined, size: 14, color: scheme.primary),
            const SizedBox(width: 4),
            Text(
              label,
              style: Theme.of(
                context,
              ).textTheme.labelLarge?.copyWith(color: scheme.primary),
            ),
            if (!_spawnOnly)
              Icon(Icons.arrow_drop_down, size: 18, color: scheme.primary)
            else
              Tooltip(
                message: 'New sessions only',
                child: Icon(
                  Icons.lock_outline,
                  size: 14,
                  color: scheme.onSurfaceVariant,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// The mode switcher's label. Plan mode reads as a state, not a menu: it is
/// tinted and carries an edit-off icon, because "the agent will not touch my
/// files" is the one mode difference worth noticing at a glance.
class _ModeChip extends StatelessWidget {
  const _ModeChip({required this.mode, this.permissionsLabel = false});

  final SessionMode mode;
  final bool permissionsLabel;

  static bool isPlan(SessionMode m) => m.id.toLowerCase() == 'plan';

  /// Driven by the daemon's flag, never by the mode id. See [SessionMode.dangerous].
  static bool isDangerous(SessionMode m) => m.dangerous;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final planning = isPlan(mode);
    final dangerous = isDangerous(mode);

    // Plan mode reads as "the agent will not touch my files"; a dangerous mode
    // reads as "the agent will not ask me". Both are worth noticing at a
    // glance, so both get a tint — dangerous gets the loudest one on the bar.
    final (Color? bg, Color? fg) = switch ((dangerous, planning)) {
      (true, _) => (scheme.errorContainer, scheme.onErrorContainer),
      (false, true) => (scheme.tertiaryContainer, scheme.onTertiaryContainer),
      _ => (null, null),
    };
    final icon = dangerous
        ? Icons.bolt
        : planning
        ? Icons.edit_off
        : null;

    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 4),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: bg == null
          ? null
          : BoxDecoration(color: bg, borderRadius: BorderRadius.circular(12)),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 14, color: fg),
            const SizedBox(width: 4),
          ],
          Text(
            mode.name.isEmpty
                ? (permissionsLabel ? 'permissions' : 'mode')
                : mode.name,
            style: Theme.of(
              context,
            ).textTheme.labelLarge?.copyWith(color: fg ?? scheme.primary),
          ),
          Icon(Icons.arrow_drop_down, size: 18, color: fg),
        ],
      ),
    );
  }
}

/// Thumbnail for a staged image attachment with a remove affordance.
/// A staged audio file in the composer strip. Audio has no thumbnail, so it
/// shows its name and size instead, with the same removal affordance as an
/// image (MADR 0112 A2).
class _AudioAttachmentChip extends StatelessWidget {
  const _AudioAttachmentChip({
    required this.filename,
    required this.byteCount,
    required this.onRemove,
  });

  final String filename;
  final int byteCount;
  final VoidCallback onRemove;

  String get _size {
    if (byteCount >= 1024 * 1024) {
      return '${(byteCount / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    if (byteCount >= 1024) return '${(byteCount / 1024).round()} KB';
    return '$byteCount B';
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Stack(
      clipBehavior: Clip.none,
      children: [
        Container(
          key: const ValueKey('staged-audio'),
          height: 64,
          constraints: const BoxConstraints(maxWidth: 200),
          padding: const EdgeInsets.symmetric(horizontal: 12),
          decoration: BoxDecoration(
            color: scheme.surfaceContainerHighest,
            borderRadius: BorderRadius.circular(10),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.audiotrack, size: 18, color: scheme.onSurfaceVariant),
              const SizedBox(width: 8),
              Flexible(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      filename.isEmpty ? 'Audio' : filename,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                    Text(
                      _size,
                      style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
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
