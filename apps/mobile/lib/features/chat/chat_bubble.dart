part of 'chat_screen.dart';

/// Collapsed summary row for a run of finished agent actions of one class —
/// "Ran 5 commands", "Edited 3 files", "Used 4 tools". Expanding reveals the
/// individual action tiles, each of which can be opened further for its
/// command/output detail. Success stays quiet; failures surface on the
/// collapsed row so they can't hide inside a folded group.
///
/// Expansion is owned by the parent ([expanded] / [onExpansionChanged]) so it
/// survives Element remounts when a finishing tool folds into the group (list
/// key churn). Avoids [PageStorageKey], which collides with nested detail
/// scroll views.
class _ToolGroupTile extends StatelessWidget {
  const _ToolGroupTile({
    required this.group,
    required this.expanded,
    required this.onExpansionChanged,
  });

  final GroupRow group;
  final bool expanded;
  final ValueChanged<bool> onExpansionChanged;

  IconData get _icon => switch (group.leadClass) {
    ToolClass.command => Icons.terminal,
    ToolClass.fileEdit => Icons.edit_note,
    ToolClass.other => Icons.handyman_outlined,
  };

  @override
  Widget build(BuildContext context) {
    // A run of one *is* the tool: render it exactly as a standalone tool card,
    // keeping its status suffix and detail expansion. The threshold is 1 only
    // so the row's key is fixed from the first tool (MADR 0042 D1) — that must
    // not cost the single-tool affordance. The enclosing widget type is
    // unchanged, so growing to two members rebuilds this subtree without
    // remounting the row.
    if (group.items.length == 1) {
      return _ChatBubble(item: group.items.first, agentRunning: false);
    }

    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = celestialOf(context);
    final muted = scheme.onSurfaceVariant;
    final failed = group.failedCount;
    final running = group.runningItem;
    // A run in flight reads as active, not as settled-and-successful.
    final rail = failed > 0
        ? scheme.error
        : running != null
        ? tokens.running
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
              // firstSeq-stable key: when the list remounts this tile after a
              // tool folds in, a new State is created and [initiallyExpanded]
              // re-reads the pane-owned map (avoids snap-closed).
              key: ValueKey('tool-group-${group.items.first.seq}'),
              dense: true,
              initiallyExpanded: expanded,
              onExpansionChanged: onExpansionChanged,
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
                  // Live head: name what is executing so a collapsed group is
                  // never opaque about work in progress (MADR 0042 D1). The
                  // running tool's *output* deliberately stays inside — putting
                  // it here would reflow the row on every streamed delta.
                  if (running != null) ...[
                    const SizedBox(width: 6),
                    Flexible(
                      child: ShimmerText(
                        (running.toolName ?? '').trim().isEmpty
                            ? 'running…'
                            : '· ${running.toolName!.trim()}',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                  ],
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
        final maxW = maxUserWidth ?? MediaQuery.sizeOf(context).width * 0.85;
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
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (item.attachments.isNotEmpty)
                    _UserAttachments(attachments: item.attachments),
                  if (text.isNotEmpty)
                    Text(text, style: TextStyle(color: tokens.onUserBubble)),
                ],
              ),
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
                showTopNotification(
                  context,
                  'Copied reply',
                  severity: NoticeSeverity.success,
                );
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
        );
      case ChatItemKind.approvals:
        // One card for everything auto-approve allowed this turn. Collapsed it
        // is a single line; expanding it is the audit trail — which is the
        // whole point of recording auto-approvals at all (MADR 0051 Part I).
        final approvals = item.approvals;
        final running = item.toolStatus == 'running';
        final first = approvals.isEmpty ? '' : approvals.first.label;
        final more = approvals.length - 1;
        return _CompactStatusTile(
          leading: Icon(
            running ? Icons.autorenew : Icons.verified_user_outlined,
            size: 16,
            color: running ? scheme.tertiary : tokens.success,
          ),
          title: more > 0 ? '$first  +$more more' : first,
          titleSuffix: 'auto-approved',
          railColor: running
              ? scheme.tertiary
              : tokens.success.withValues(alpha: 0.6),
          detail: [
            for (final a in approvals)
              a.time == null
                  ? a.label
                  : '${a.label}  ·  ${_hhmmss(a.time!.toLocal())}',
          ].join('\n'),
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
        );
      case ChatItemKind.system:
        final text = item.text ?? '';
        // Quota / rate-limit hits get a proper card with reset guidance, not
        // a raw red provider dump.
        if (item.isLimitError) {
          return _LimitNotice(item: item);
        }
        // Permission denials likewise (MADR 0069 D4.5): the daemon already
        // composed the remedy (mode switch vs Full Disk Access vs plain)
        // into the message — render it as guidance, not as a failure dump.
        if (item.isPermissionError) {
          return _PermissionNotice(item: item);
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

/// When a usage limit resets, phrased for the limit card: "3:45 PM (in about
/// 2 h)" today, "tomorrow 12:10 AM (in about 40 min)" the next calendar day,
/// "Sat, Jul 25 12:30 AM (in about 1 d 3 h)" further out — or "now — try
/// again" once it has passed. Top-level (not on the private widget) so tests
/// importing chat_screen.dart can call it; [now] is injectable for tests only.
@visibleForTesting
String limitResetPhrase(
  BuildContext context,
  DateTime resetAt, {
  DateTime? now,
}) {
  final nowLocal = (now ?? DateTime.now()).toLocal();
  final local = resetAt.toLocal();
  final d = local.difference(nowLocal);
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
  // Calendar-day distance in local time (date-only), not raw 24 h buckets: a
  // reset 30 h away can still be "tomorrow", while one 27 h away that crosses
  // two midnights is not.
  final dayDiff = DateUtils.dateOnly(
    local,
  ).difference(DateUtils.dateOnly(nowLocal)).inDays;
  final day = switch (dayDiff) {
    0 => '',
    1 => 'tomorrow ',
    _ => '${MaterialLocalizations.of(context).formatMediumDate(local)} ',
  };
  return '$day$clock (in $rel)';
}

/// Card shown when the agent hits a usage quota or rate limit. Leads with
/// what happened and when it resets (when the provider said), keeps the raw
/// provider message as fine print, and points at the practical outs.
/// Card for `errorKind: permission` (MADR 0069 D4.5). Stateless — unlike
/// the limit card there is no countdown; the daemon-composed message *is*
/// the guidance (sandbox-mode switch, Full Disk Access grant, or plain
/// permissions), so it renders verbatim and prominently.
class _PermissionNotice extends StatelessWidget {
  const _PermissionNotice({required this.item});

  final ChatItem item;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final text = (item.text ?? '').trim();
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Center(
        child: Container(
          constraints: const BoxConstraints(maxWidth: 420),
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: scheme.errorContainer.withValues(alpha: 0.25),
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: scheme.error.withValues(alpha: 0.45)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  Icon(Icons.gpp_maybe_outlined, size: 18, color: scheme.error),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Blocked by permissions',
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
              if (text.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(text, style: theme.textTheme.bodyMedium),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _LimitNotice extends StatefulWidget {
  const _LimitNotice({required this.item});

  final ChatItem item;

  @override
  State<_LimitNotice> createState() => _LimitNoticeState();
}

class _LimitNoticeState extends State<_LimitNotice> {
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    // The "(in about 2 h)" phrasing is relative to now, so a card left on
    // screen goes stale without a periodic rebuild. Once the reset time has
    // passed, one final rebuild shows "now — try again" and the timer stops —
    // the phrase never changes again.
    if (widget.item.retryAt != null) {
      _timer = Timer.periodic(const Duration(minutes: 1), (_) {
        if (!DateTime.now().isBefore(widget.item.retryAt!)) {
          _timer?.cancel();
          _timer = null;
        }
        setState(() {});
      });
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.item;
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = celestialOf(context);
    final isQuota = item.errorKind == 'quota';
    final title = isQuota ? 'Agent quota exceeded' : 'Agent rate-limited';
    final retryAt = item.retryAt;

    final String body;
    if (retryAt != null) {
      body = 'The limit resets at ${limitResetPhrase(context, retryAt)}.';
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

/// Counts full [MarkdownBody] builds of assistant content (stream and finalize
/// share one engine — MADR 0056 Phase 7 / 0057). Top-level so tests can assert
/// MADR 0018 guarantees: no re-parse of a finalized reply on unrelated
/// rebuilds, and bounded parses while streaming
/// (test/streaming_markdown_test.dart). Tests reset it in setUp.
@visibleForTesting
int debugMarkdownParseCount = 0;

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
  late String _shown = widget.data;
  late bool _shownStreaming = widget.streaming;
  Widget? _built;

  // Proposal F: frame-aligned throttle — at most one render per frame.
  bool _dirty = false;
  // L-3: stream update was skipped while scrolling; flush when scroll ends.
  bool _pendingAfterScroll = false;
  ValueNotifier<bool>? _scrollNotifier;
  VoidCallback? _scrollListener;

  /// Last time [_render] ran for a streaming update (MADR 0057 H-2 size tiers).
  DateTime? _lastStreamRenderAt;
  Timer? _tierTimer;

  MarkdownStyleSheet? _styleSheet;
  Brightness? _styleBrightness;

  /// Expanded past the "Show more" clamp for huge finalized replies (E3).
  bool _expanded = false;

  /// Minimum wall time between stream re-parses for long replies (on top of
  /// frame alignment). Short replies stay frame-only (Duration.zero).
  static Duration _minStreamInterval(int len) {
    if (len > kStreamMdTier2Chars) return const Duration(milliseconds: 200);
    if (len > kStreamMdTier1Chars) return const Duration(milliseconds: 120);
    return Duration.zero;
  }

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
  // streaming, trailing not-yet-closed markdown is closed so raw markers never
  // flash; the full text renders once the turn completes. Selection is off
  // while streaming (cheaper paint) and on once final — long-press still
  // copies the full reply from the bubble.
  Widget _render(BuildContext context, String text, bool streaming) {
    _shown = text;
    _shownStreaming = streaming;
    // Show-more clamp only on finalized huge replies (E3).
    final needsClamp =
        !streaming && !_expanded && text.length > kAssistantShowMoreChars;
    final String bodyText;
    if (needsClamp) {
      var cut = kAssistantShowMoreChars;
      // Never cut between the halves of a UTF-16 surrogate pair — the split
      // would leave a malformed string.
      if ((text.codeUnitAt(cut) & 0xFC00) == 0xDC00) cut--;
      final clamped = text.substring(0, cut);
      // A cut inside a fence/inline-code/bold gets synthetic closers so the
      // clamped tail does not render as one giant broken code block. The "…"
      // then needs its own paragraph: a closing fence line with trailing text
      // ("```…") would not close the fence.
      final closed = bufferStreamingMarkdown(clamped);
      bodyText = closed == clamped ? '$clamped…' : '$closed\n\n…';
    } else {
      bodyText = text;
    }
    // Single engine for stream and finalize (MADR 0056 M-9…M-12 / Phase 7):
    // always MarkdownBody. While streaming, auto-close open markers so raw
    // syntax does not flash. Frame throttle in didUpdateWidget bounds cost.
    final shown = streaming ? bufferStreamingMarkdown(bodyText) : bodyText;
    debugMarkdownParseCount++;
    if (streaming) _lastStreamRenderAt = DateTime.now();
    final body = MarkdownBody(
      data: shown,
      selectable: !streaming,
      styleSheet: _sheetFor(context),
      builders: <String, MarkdownElementBuilder>{'pre': _CodeBlockBuilder()},
      // MADR 0057 L-5: never auto-navigate from assistant markdown. Allowed
      // schemes are acknowledged only so a future open-in-browser path has a
      // single allowlist; today all taps are no-ops (long-press still copies).
      onTapLink: (text, href, title) {
        if (href == null || href.isEmpty) return;
        final uri = Uri.tryParse(href);
        if (uri == null) return;
        final scheme = uri.scheme.toLowerCase();
        if (scheme != 'http' && scheme != 'https' && scheme != 'mailto') {
          return;
        }
      },
    );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        body,
        if (streaming) const _StreamingCaret(),
        if (needsClamp)
          TextButton(
            onPressed: () {
              setState(() {
                _expanded = true;
                _built = _render(context, widget.data, false);
              });
            },
            child: const Text('Show more'),
          ),
        if (!streaming && _expanded && text.length > kAssistantShowMoreChars)
          TextButton(
            onPressed: () {
              setState(() {
                _expanded = false;
                _built = _render(context, widget.data, false);
              });
            },
            child: const Text('Show less'),
          ),
      ],
    );
  }

  // The current subtree is already current if nothing that affects it changed.
  bool _upToDate(bool streaming) =>
      _shown == widget.data && _shownStreaming == streaming;

  @override
  void didUpdateWidget(covariant _AssistantMarkdown old) {
    super.didUpdateWidget(old);
    if (old.data != widget.data &&
        widget.data.length <= kAssistantShowMoreChars) {
      _expanded = false;
    }

    // ── Finalized path ──
    if (!widget.streaming) {
      _dirty = false;
      _tierTimer?.cancel();
      _tierTimer = null;
      if (!_upToDate(false)) {
        setState(() => _built = _render(context, widget.data, false));
      }
      return;
    }

    // ── Streaming path ──
    if (_upToDate(true)) return;
    // Proposal I: suppress rebuilds while the user is scrolling/flinging.
    // Remember dirty so scroll-end can reschedule (MADR 0056 L-3).
    if (ChatScrollActivity.isScrolling(context)) {
      _pendingAfterScroll = true;
      return;
    }
    // MADR 0057 H-2: size-tier minimum interval on top of frame alignment.
    final minIv = _minStreamInterval(widget.data.length);
    final last = _lastStreamRenderAt;
    if (minIv > Duration.zero && last != null) {
      final elapsed = DateTime.now().difference(last);
      if (elapsed < minIv) {
        _tierTimer?.cancel();
        _tierTimer = Timer(minIv - elapsed, () {
          _tierTimer = null;
          if (!mounted || !widget.streaming) return;
          if (_upToDate(true)) return;
          if (ChatScrollActivity.isScrolling(context)) {
            _pendingAfterScroll = true;
            return;
          }
          unawaited(_updateStreamingRender());
        });
        return;
      }
    }
    // Proposal F: frame-aligned throttle — at most one render per frame.
    if (_dirty) return;
    _dirty = true;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      if (!mounted) {
        _dirty = false;
        return;
      }
      _dirty = false;
      if (_upToDate(true)) return;
      await _updateStreamingRender();
    });
  }

  Future<void> _updateStreamingRender() async {
    final text = widget.data;
    if (!mounted || text != widget.data) return;
    // Single MarkdownBody path: no isolate subset renderer (MADR 0056 Phase 7).
    setState(() {
      _built = _render(context, text, true);
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final n = context
        .dependOnInheritedWidgetOfExactType<ChatScrollActivity>()
        ?.notifier;
    if (identical(n, _scrollNotifier)) return;
    if (_scrollNotifier != null && _scrollListener != null) {
      _scrollNotifier!.removeListener(_scrollListener!);
    }
    _scrollNotifier = n;
    if (n == null) {
      _scrollListener = null;
      return;
    }
    _scrollListener = () {
      if (!n.value && _pendingAfterScroll && mounted && widget.streaming) {
        _pendingAfterScroll = false;
        unawaited(_updateStreamingRender());
      }
    };
    n.addListener(_scrollListener!);
  }

  @override
  void dispose() {
    _dirty = false; // prevent post-frame callback from calling setState
    _tierTimer?.cancel();
    _tierTimer = null;
    if (_scrollNotifier != null && _scrollListener != null) {
      _scrollNotifier!.removeListener(_scrollListener!);
    }
    super.dispose();
  }

  // Returns the cached, already-parsed subtree. Identical across parent
  // rebuilds, so the framework skips re-parsing until [_render] swaps it.
  @override
  Widget build(BuildContext context) {
    // Theme flip (light/dark) must rebuild the stylesheet + markdown body.
    final brightness = Theme.of(context).brightness;
    if (_built == null || _styleBrightness != brightness) {
      _styleSheet = null;
      _styleBrightness = brightness;
      _built = _render(context, widget.data, widget.streaming);
    }
    return _built!;
  }
}

/// Blinking edge pulse so live assistant bubbles still read as "writing" (E2).
class _StreamingCaret extends StatefulWidget {
  const _StreamingCaret();

  @override
  State<_StreamingCaret> createState() => _StreamingCaretState();
}

class _StreamingCaretState extends State<_StreamingCaret>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 700),
  );

  bool get _shouldAnimate {
    final reduce = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    if (reduce) return false;
    if (ChatScrollActivity.isScrolling(context)) return false;
    return true;
  }

  void _syncAnimation() {
    if (!_shouldAnimate) {
      _ctrl.stop();
      _ctrl.value = 1.0;
    } else if (!_ctrl.isAnimating) {
      _ctrl.repeat(reverse: true);
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _syncAnimation();
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    _syncAnimation();
    final color = Theme.of(context).colorScheme.primary;
    final bar = Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Align(
        alignment: Alignment.centerLeft,
        child: Container(
          width: 8,
          height: 14,
          decoration: BoxDecoration(
            color: color,
            borderRadius: BorderRadius.circular(2),
          ),
        ),
      ),
    );
    // Static bar while scrolling / reduce-motion, mirroring [ShimmerText]: a
    // ticking fade fights the list for frames during drag/fling.
    if (!_shouldAnimate) return bar;
    return FadeTransition(
      opacity: Tween(begin: 0.25, end: 1.0).animate(_ctrl),
      child: bar,
    );
  }
}

/// Fenced code blocks (`pre`) with optional language label and Copy (D3).
class _CodeBlockBuilder extends MarkdownElementBuilder {
  @override
  bool isBlockElement() => true;

  @override
  Widget? visitElementAfter(md.Element element, TextStyle? preferredStyle) {
    final text = element.textContent;
    // Language often lives on the nested code element: class="language-foo".
    var lang = '';
    for (final node in element.children ?? const <md.Node>[]) {
      if (node is md.Element) {
        final classes = node.attributes['class'] ?? '';
        if (classes.startsWith('language-')) {
          lang = classes.substring('language-'.length);
          break;
        }
      }
    }
    return _CodeBlockChrome(code: text, language: lang);
  }
}

class _CodeBlockChrome extends StatelessWidget {
  const _CodeBlockChrome({required this.code, this.language = ''});

  final String code;
  final String language;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final codeBg = scheme.surfaceContainerHigh;
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.symmetric(vertical: 4),
      decoration: BoxDecoration(
        color: codeBg,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (language.isNotEmpty || code.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(10, 4, 4, 0),
              child: Row(
                children: [
                  if (language.isNotEmpty)
                    Text(
                      language,
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  const Spacer(),
                  IconButton(
                    tooltip: 'Copy code',
                    visualDensity: VisualDensity.compact,
                    iconSize: 18,
                    onPressed: () async {
                      await Clipboard.setData(ClipboardData(text: code));
                      if (context.mounted) {
                        showTopNotification(
                          context,
                          'Copied code',
                          severity: NoticeSeverity.success,
                        );
                      }
                    },
                    icon: const Icon(Icons.copy_outlined),
                  ),
                ],
              ),
            ),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.fromLTRB(10, 0, 10, 10),
            child: SelectableText(
              code,
              style: theme.textTheme.bodyMedium?.copyWith(
                fontFamily: 'monospace',
                fontSize: 13,
              ),
            ),
          ),
        ],
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
    this.railColor,
    this.shimmerTitle = false,
  });

  final Widget leading;
  final String title;
  final String? titleSuffix;
  final String detail;

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

    // Bound expanded layout so multi-MB tool dumps cannot dominate (C5 / D2).
    final displayDetail = trimmedDetail.length > kMaxExpandedDetailChars
        ? '${trimmedDetail.substring(0, kMaxExpandedDetailChars)}… [truncated]'
        : trimmedDetail;

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
                padding: const EdgeInsets.fromLTRB(10, 4, 4, 10),
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerLow,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Align(
                      alignment: Alignment.centerRight,
                      child: IconButton(
                        tooltip: 'Copy detail',
                        visualDensity: VisualDensity.compact,
                        iconSize: 18,
                        onPressed: () async {
                          await Clipboard.setData(
                            ClipboardData(text: trimmedDetail),
                          );
                          if (context.mounted) {
                            showTopNotification(
                              context,
                              'Copied',
                              severity: NoticeSeverity.success,
                            );
                          }
                        },
                        icon: const Icon(Icons.copy_outlined),
                      ),
                    ),
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 200),
                      child: SingleChildScrollView(
                        child: SelectableText(
                          displayDetail,
                          style: monoDetail.copyWith(color: muted),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Attachment thumbnails on a user bubble: a real image when the sending device
/// still holds the bytes, else a labeled placeholder (other devices / cache
/// reload, which only have the descriptor).
class _UserAttachments extends StatelessWidget {
  const _UserAttachments({required this.attachments});

  final List<ChatAttachment> attachments;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Wrap(
        spacing: 6,
        runSpacing: 6,
        children: [
          for (final a in attachments)
            if (a.bytes != null)
              ClipRRect(
                borderRadius: BorderRadius.circular(10),
                child: Image.memory(
                  a.bytes!,
                  width: 140,
                  height: 140,
                  fit: BoxFit.cover,
                ),
              )
            else
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 8,
                ),
                decoration: BoxDecoration(
                  color: Colors.black.withValues(alpha: 0.22),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      a.kind == 'audio' ? Icons.audiotrack : Icons.image,
                      size: 16,
                      color: Colors.white70,
                    ),
                    const SizedBox(width: 6),
                    Text(
                      a.kind == 'audio' ? 'Audio' : 'Image',
                      style: const TextStyle(
                        color: Colors.white70,
                        fontSize: 12,
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

/// `HH:MM:SS` in the device's local zone, for the auto-approval audit rows.
/// Fixed-width by construction so a column of times stays aligned.
String _hhmmss(DateTime t) {
  String two(int v) => v.toString().padLeft(2, '0');
  return '${two(t.hour)}:${two(t.minute)}:${two(t.second)}';
}
