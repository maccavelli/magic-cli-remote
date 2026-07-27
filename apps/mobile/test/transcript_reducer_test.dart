import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

SessionEvent _ev(
  String type, {
  String sessionId = 's1',
  String? text,
  String? status,
  String? toolId,
  String? toolName,
  String? toolKind,
  String? stopReason,
  String? error,
  String? permissionId,
  String? questionId,
  List<QuestionItem> questions = const [],
}) {
  return SessionEvent(
    type: type,
    sessionId: sessionId,
    text: text,
    status: status,
    toolId: toolId,
    toolName: toolName,
    toolKind: toolKind,
    stopReason: stopReason,
    error: error,
    permissionId: permissionId,
    questionId: questionId,
    questions: questions,
  );
}

void main() {
  const base = SessionTranscript(sessionId: 's1');

  test('tool_call carries the kind through, and updates never erase it', () {
    var t = applySessionEvent(
      base,
      _ev('tool_call', toolId: 'x', toolName: 'Bash', toolKind: 'execute'),
    );
    expect(t.items.single.toolKind, 'execute');
    expect(t.items.single.toolClass, ToolClass.command);

    // A status-only update without a kind must not clear the stored one.
    t = applySessionEvent(
      t,
      _ev('tool_call_update', toolId: 'x', status: 'completed'),
    );
    expect(t.items.single.toolKind, 'execute');
    expect(t.items.single.toolStatus, 'completed');
  });

  test('error events carry the limit classification through to the item', () {
    final next = applySessionEvent(
      base,
      SessionEvent(
        type: 'error',
        sessionId: 's1',
        error: 'Free usage exceeded, add credits https://opencode.ai/zen',
        errorKind: 'quota',
        retryAt: DateTime.utc(2026, 7, 23, 3),
      ),
    );
    final item = next.items.single;
    expect(item.isError, isTrue);
    expect(item.isLimitError, isTrue);
    expect(item.errorKind, 'quota');
    expect(item.retryAt, DateTime.utc(2026, 7, 23, 3));
    expect(next.status, 'error');
  });

  test('ignores other session ids', () {
    final next = applySessionEvent(
      base,
      _ev('user_message', sessionId: 'other', text: 'hi'),
    );
    expect(identical(next, base), isTrue);
  });

  test('user message appends', () {
    final next = applySessionEvent(base, _ev('user_message', text: 'hello'));
    expect(next.items, hasLength(1));
    expect(next.items.first.kind, ChatItemKind.user);
    expect(next.items.first.text, 'hello');
  });

  test('assistant chunks coalesce', () {
    var t = applySessionEvent(
      base,
      _ev('assistant_message_chunk', text: 'Hel'),
    );
    t = applySessionEvent(t, _ev('assistant_message_chunk', text: 'lo'));
    expect(t.items, hasLength(1));
    expect(t.items.first.text, 'Hello');
  });

  test(
    'long multi-chunk assistant stream coalesces and reuses list when growable',
    () {
      var t = applySessionEvent(
        base,
        _ev('assistant_message_chunk', text: 'x'),
      );
      // After first append the list is growable-owned; subsequent chunks must not
      // allocate a fresh list each time (MADR 0018 B1 / D2).
      expect(t.growableItems, isTrue);
      final listRef = t.items;
      for (var i = 0; i < 200; i++) {
        t = applySessionEvent(t, _ev('assistant_message_chunk', text: 'y'));
      }
      expect(identical(t.items, listRef), isTrue);
      expect(t.items, hasLength(1));
      expect(t.items.single.text, 'x${'y' * 200}');
    },
  );

  test('assistant text is hard-clipped at kMaxItemTextChars', () {
    final big = 'a' * (kMaxItemTextChars + 500);
    var t = applySessionEvent(base, _ev('assistant_message_chunk', text: big));
    expect(t.items.single.text!.length, lessThanOrEqualTo(kMaxItemTextChars));
    expect(t.items.single.text, endsWith(kTextTruncatedMarker));
    // Further growth stays capped.
    t = applySessionEvent(
      t,
      _ev('assistant_message_chunk', text: 'more-more-more'),
    );
    expect(t.items.single.text!.length, lessThanOrEqualTo(kMaxItemTextChars));
  });

  test('tool detail is clipped on store', () {
    final big = 'd' * (kMaxItemTextChars + 100);
    final t = applySessionEvent(
      base,
      _ev(
        'tool_call',
        toolId: 't1',
        toolName: 'Shell',
        status: 'completed',
        text: big,
      ),
    );
    expect(t.items.single.text!.length, lessThanOrEqualTo(kMaxItemTextChars));
    expect(t.items.single.text, endsWith(kTextTruncatedMarker));
  });

  test('thought chunks coalesce', () {
    var t = applySessionEvent(base, _ev('thought_chunk', text: 'hmm'));
    t = applySessionEvent(t, _ev('thought_chunk', text: '…'));
    expect(t.items, hasLength(1));
    expect(t.items.first.kind, ChatItemKind.thought);
    expect(t.items.first.text, 'hmm…');
  });

  test('tool upsert by id', () {
    var t = applySessionEvent(
      base,
      _ev('tool_call', toolId: 't1', toolName: 'Shell', status: 'pending'),
    );
    t = applySessionEvent(
      t,
      _ev(
        'tool_call_update',
        toolId: 't1',
        toolName: 'Shell',
        status: 'completed',
        text: 'done',
      ),
    );
    expect(t.items, hasLength(1));
    expect(t.items.first.toolStatus, 'completed');
    expect(t.items.first.text, 'done');
  });

  test('id-less tool_call_update folds into the last tool card', () {
    // An agent that omits tool_id on updates must not spawn a duplicate card
    // per status change — the update coalesces into the streaming tool.
    var t = applySessionEvent(
      base,
      _ev('tool_call', toolId: 't1', toolName: 'Shell', status: 'running'),
    );
    t = applySessionEvent(
      t,
      _ev(
        'tool_call_update',
        toolName: 'Shell',
        status: 'completed',
        text: 'exit 0',
      ),
    );
    expect(t.items, hasLength(1));
    expect(t.items.first.kind, ChatItemKind.tool);
    expect(t.items.first.toolStatus, 'completed');
    expect(t.items.first.text, 'exit 0');
  });

  test('id-less tool_call still starts its own card', () {
    // Only *updates* coalesce; a fresh tool_call without an id is a new tool.
    var t = applySessionEvent(
      base,
      _ev('tool_call', toolName: 'Read', status: 'running'),
    );
    t = applySessionEvent(
      t,
      _ev('tool_call', toolName: 'Write', status: 'running'),
    );
    expect(t.items.where((i) => i.kind == ChatItemKind.tool), hasLength(2));
  });

  test(
    'id-less update with no prior tool does not crash or coalesce wrongly',
    () {
      // A stray update with nothing to fold into is a no-op-ish append, never a
      // merge into an unrelated (assistant) bubble.
      var t = applySessionEvent(
        base,
        _ev('assistant_message_chunk', text: 'hi'),
      );
      t = applySessionEvent(t, _ev('tool_call_update', status: 'completed'));
      expect(t.items.first.kind, ChatItemKind.assistant);
      expect(t.items.first.text, 'hi');
      // The assistant bubble is untouched; the stray update became its own card.
      expect(t.items.last.kind, ChatItemKind.tool);
    },
  );

  test('notice appends a neutral system message', () {
    final t = applySessionEvent(
      base,
      _ev('notice', text: 'Model is now grok-4.'),
    );
    expect(t.items, hasLength(1));
    expect(t.items.first.kind, ChatItemKind.system);
    expect(t.items.first.text, 'Model is now grok-4.');
    // Not an error line (no 'Error:' prefix → renders neutral, not red).
    expect(t.items.first.text!.startsWith('Error:'), isFalse);
  });

  test('empty notice is a no-op', () {
    final t = applySessionEvent(base, _ev('notice', text: '   '));
    expect(identical(t, base), isTrue);
  });

  test('session_status updates status only', () {
    final next = applySessionEvent(
      base,
      _ev('session_status', status: 'running'),
    );
    expect(next.status, 'running');
    expect(next.items, isEmpty);
  });

  test('turn_complete cancelled announces once via flag path', () {
    var t = applySessionEvent(
      base,
      _ev('turn_complete', stopReason: 'cancelled'),
    );
    expect(t.items.any((i) => i.text == 'Turn cancelled'), isTrue);
    expect(t.status, 'idle');
  });

  test('error adds system line', () {
    final next = applySessionEvent(base, _ev('error', error: 'boom'));
    expect(next.status, 'error');
    expect(next.items.first.kind, ChatItemKind.system);
    expect(next.items.first.text, contains('boom'));
  });

  // MADR 0036 D1: the daemon guarantees an `error` stop is paired with an
  // `error` event, so the stop itself must render nothing — otherwise a
  // contentless "Turn ended (error)" sits above the real message.
  test('turn_complete error appends no stop line', () {
    final next = applySessionEvent(
      base,
      _ev('turn_complete', stopReason: 'error'),
    );
    expect(next.items, isEmpty);
    expect(next.status, 'idle');
  });

  test('turn_complete error then error event yields exactly one line', () {
    var t = applySessionEvent(base, _ev('turn_complete', stopReason: 'error'));
    t = applySessionEvent(t, _ev('error', error: 'model returned 500'));
    expect(t.items.length, 1);
    expect(t.items.single.text, contains('model returned 500'));
    expect(t.items.any((i) => (i.text ?? '').contains('Turn ended')), isFalse);
  });

  test('turn_complete with an unknown reason still explains itself', () {
    final next = applySessionEvent(
      base,
      _ev('turn_complete', stopReason: 'weird_new_reason'),
    );
    expect(next.items.single.text, contains('weird_new_reason'));
  });

  test('permission_request sets pending', () {
    final next = applySessionEvent(
      base,
      _ev('permission_request', permissionId: 'p1', toolName: 'Write'),
    );
    expect(next.pendingPermission?.permissionId, 'p1');
    expect(next.items.first.text, contains('Write'));
  });

  test('question_request sets pending and clears on resolved', () {
    var t = applySessionEvent(
      base,
      _ev(
        'question_request',
        questionId: 'q1',
        text: 'Scope',
        questions: [
          QuestionItem(
            header: 'Scope',
            text: 'Which packages?',
            multiple: true,
            options: [
              PermissionOption(optionId: 'core', name: 'core'),
              PermissionOption(optionId: 'cli', name: 'cli'),
            ],
          ),
        ],
      ),
    );
    expect(t.hasPendingQuestion, isTrue);
    expect(t.pendingQuestion?.questionId, 'q1');
    expect(t.hasBlockingPrompt, isTrue);
    expect(t.items.first.text, contains('Scope'));

    t = applySessionEvent(
      t,
      _ev('question_resolved', questionId: 'q1', status: 'resolved'),
    );
    expect(t.hasPendingQuestion, isFalse);
    expect(t.hasBlockingPrompt, isFalse);
  });

  test('question parse from JSON', () {
    final ev = SessionEvent.fromJson({
      'type': 'question_request',
      'session_id': 's1',
      'question_id': 'q2',
      'text': 'Pick one',
      'questions': [
        {
          'header': 'H',
          'text': 'T',
          'multiple': false,
          'custom': true,
          'options': [
            {'option_id': 'a', 'name': 'a'},
          ],
        },
      ],
    });
    expect(ev.questionId, 'q2');
    expect(ev.questions, hasLength(1));
    expect(ev.questions.first.custom, isTrue);
    expect(ev.questions.first.options.single.optionId, 'a');
  });

  test('soft cap drops oldest items', () {
    var t = base;
    for (var i = 0; i < kMaxTranscriptItems + 50; i++) {
      t = applySessionEvent(t, _ev('user_message', text: 'm$i'));
    }
    expect(t.items.length, kMaxTranscriptItems);
    expect(t.items.first.text, 'm50');
    expect(t.items.last.text, 'm${kMaxTranscriptItems + 49}');
  });

  test('markCancelAnnounced is idempotent-ish', () {
    var t = markCancelAnnounced(base);
    expect(t.cancelAnnounced, isTrue);
    expect(t.items, hasLength(1));
    t = markCancelAnnounced(t);
    expect(t.items, hasLength(1));
  });

  test('available_commands updates catalog without chat noise', () {
    final next = applySessionEvent(
      base,
      SessionEvent(
        type: 'available_commands',
        sessionId: 's1',
        commands: [
          AvailableCommand(name: 'web', description: 'Search', hint: 'q'),
          AvailableCommand(name: 'plan', description: 'Plan'),
        ],
      ),
    );
    expect(next.items, isEmpty);
    expect(next.commands, hasLength(2));
    expect(next.commands.first.name, 'web');
    expect(next.commands.first.hint, 'q');
  });

  group('plan events', () {
    SessionEvent planEv(List<PlanEntry> entries) =>
        SessionEvent(type: 'plan', sessionId: 's1', plan: entries);

    test('a plan event populates transcript.plan without a chat bubble', () {
      final next = applySessionEvent(
        base,
        planEv([
          PlanEntry(
            content: 'Read code',
            status: 'completed',
            priority: 'high',
          ),
          PlanEntry(content: 'Write fix', status: 'in_progress'),
        ]),
      );
      expect(next.items, isEmpty);
      expect(next.plan, hasLength(2));
      expect(next.plan.first.content, 'Read code');
      expect(next.plan.first.status, 'completed');
      expect(next.plan[1].status, 'in_progress');
    });

    test('replace-semantics: a later plan wholly replaces the earlier one', () {
      var t = applySessionEvent(base, planEv([PlanEntry(content: 'step one')]));
      t = applySessionEvent(
        t,
        planEv([
          PlanEntry(content: 'step one', status: 'completed'),
          PlanEntry(content: 'step two'),
        ]),
      );
      expect(t.plan, hasLength(2));
      expect(t.plan.first.status, 'completed');
      expect(t.plan[1].content, 'step two');
    });

    test('an unchanged plan is a no-op (identical instance)', () {
      final t = applySessionEvent(
        base,
        planEv([PlanEntry(content: 'a', status: 'pending', priority: 'low')]),
      );
      final again = applySessionEvent(
        t,
        planEv([PlanEntry(content: 'a', status: 'pending', priority: 'low')]),
      );
      expect(identical(again, t), isTrue);
    });

    test('a clear (empty entries) wipes a populated plan', () {
      var t = applySessionEvent(base, planEv([PlanEntry(content: 'x')]));
      expect(t.plan, hasLength(1));
      // PlanRemoved arrives as a plan event with no entries.
      t = applySessionEvent(t, planEv(const []));
      expect(t.plan, isEmpty);
    });

    test('wire shape: a plan event with entries omitted parses as a clear', () {
      // Go emits the clear as {type:plan, session_id} with `entries` absent
      // (omitempty). fromJson must read that as an empty plan, and the reducer
      // must apply it as a clear against a populated plan.
      var t = applySessionEvent(base, planEv([PlanEntry(content: 'y')]));
      final clear = SessionEvent.fromJson({'type': 'plan', 'session_id': 's1'});
      expect(clear.plan, isEmpty);
      t = applySessionEvent(t, clear);
      expect(t.plan, isEmpty);
    });

    test('plan parses from the `entries` JSON key', () {
      final ev = SessionEvent.fromJson({
        'type': 'plan',
        'session_id': 's1',
        'entries': [
          {'content': 'x', 'status': 'in_progress', 'priority': 'high'},
        ],
      });
      final next = applySessionEvent(base, ev);
      expect(next.plan.single.content, 'x');
      expect(next.plan.single.status, 'in_progress');
      expect(next.plan.single.priority, 'high');
    });
  });

  // ---- Regression tests for the deep-scan findings ----

  group('concurrent permission requests', () {
    test('two requests are both retained (single slot would strand one)', () {
      var t = applySessionEvent(
        base,
        _ev('permission_request', permissionId: 'p1', toolName: 'Read'),
      );
      t = applySessionEvent(
        t,
        _ev('permission_request', permissionId: 'p2', toolName: 'Write'),
      );
      expect(t.pendingPermissions.keys, ['p1', 'p2']);
      // Oldest first, so the UI presents them in arrival order.
      expect(t.pendingPermission!.permissionId, 'p1');
    });

    test('resolving one leaves the other pending', () {
      var t = applySessionEvent(
        base,
        _ev('permission_request', permissionId: 'p1'),
      );
      t = applySessionEvent(t, _ev('permission_request', permissionId: 'p2'));
      t = clearPendingPermission(t, permissionId: 'p1');
      expect(t.pendingPermissions.keys, ['p2']);
      expect(t.hasPendingPermission, isTrue);
    });

    test('replayed request does not append a second system line', () {
      var t = applySessionEvent(
        base,
        _ev('permission_request', permissionId: 'p1', toolName: 'Read'),
      );
      final afterFirst = t.items.length;
      t = applySessionEvent(
        t,
        _ev('permission_request', permissionId: 'p1', toolName: 'Read'),
      );
      expect(t.items.length, afterFirst);
      expect(t.pendingPermissions.length, 1);
    });
  });

  group('permission_resolved unlocks the composer', () {
    test('server-abandoned permission clears pending', () {
      var t = applySessionEvent(
        base,
        _ev('permission_request', permissionId: 'p1'),
      );
      expect(t.hasPendingPermission, isTrue);
      t = applySessionEvent(
        t,
        _ev('permission_resolved', permissionId: 'p1', status: 'cancelled'),
      );
      expect(t.hasPendingPermission, isFalse);
    });

    test('turn_complete defensively clears a stranded permission', () {
      var t = applySessionEvent(
        base,
        _ev('permission_request', permissionId: 'p1'),
      );
      t = applySessionEvent(t, _ev('turn_complete', stopReason: 'end_turn'));
      expect(t.hasPendingPermission, isFalse);
      expect(t.status, 'idle');
    });

    test('error clears pending so the composer is not locked forever', () {
      var t = applySessionEvent(
        base,
        _ev('permission_request', permissionId: 'p1'),
      );
      t = applySessionEvent(t, _ev('error', error: 'boom'));
      expect(t.hasPendingPermission, isFalse);
      expect(t.status, 'error');
    });
  });

  group('cancel announcement dedup', () {
    test('local announce then turn_complete appends only one line', () {
      // This is the real race: _cancelTurn awaits cancel() and the server's
      // turn_complete lands before announceCancel runs, or vice versa.
      var t = markCancelAnnounced(base);
      t = applySessionEvent(t, _ev('turn_complete', stopReason: 'cancelled'));
      final cancels = t.items.where((i) => i.text == 'Turn cancelled').length;
      expect(cancels, 1);
    });

    test('turn_complete then local announce appends only one line', () {
      var t = applySessionEvent(
        base,
        _ev('turn_complete', stopReason: 'cancelled'),
      );
      t = markCancelAnnounced(t);
      final cancels = t.items.where((i) => i.text == 'Turn cancelled').length;
      expect(cancels, 1);
    });

    test('duplicate turn_complete does not re-announce', () {
      var t = applySessionEvent(
        base,
        _ev('turn_complete', stopReason: 'cancelled'),
      );
      t = applySessionEvent(t, _ev('turn_complete', stopReason: 'cancelled'));
      expect(t.items.where((i) => i.text == 'Turn cancelled').length, 1);
    });

    test('a later normal turn re-arms the announcement', () {
      var t = applySessionEvent(
        base,
        _ev('turn_complete', stopReason: 'cancelled'),
      );
      t = applySessionEvent(t, _ev('turn_complete', stopReason: 'end_turn'));
      t = applySessionEvent(t, _ev('turn_complete', stopReason: 'cancelled'));
      expect(t.items.where((i) => i.text == 'Turn cancelled').length, 2);
    });

    test(
      'cancel → new turn → cancel announces both (user_message re-arms)',
      () {
        // Regression: back-to-back cancels used to leave the second one silent,
        // because only a *non-cancelled* completion reset the latch.
        var t = applySessionEvent(
          base,
          _ev('turn_complete', stopReason: 'cancelled'),
        );
        t = applySessionEvent(t, _ev('user_message', text: 'try again'));
        t = applySessionEvent(t, _ev('turn_complete', stopReason: 'cancelled'));
        expect(t.items.where((i) => i.text == 'Turn cancelled').length, 2);
      },
    );
  });

  group('tool card naming', () {
    test('update without a title keeps the established name', () {
      // The daemon substitutes the literal "tool" when ACP omits a title.
      var t = applySessionEvent(
        base,
        _ev('tool_call', toolId: 't1', toolName: 'Read event.go'),
      );
      t = applySessionEvent(
        t,
        _ev(
          'tool_call_update',
          toolId: 't1',
          toolName: 'tool',
          status: 'completed',
        ),
      );
      final card = t.items.firstWhere((i) => i.kind == ChatItemKind.tool);
      expect(card.toolName, 'Read event.go');
      expect(card.toolStatus, 'completed');
    });

    test('update with a real title does replace the name', () {
      var t = applySessionEvent(
        base,
        _ev('tool_call', toolId: 't1', toolName: 'Read'),
      );
      t = applySessionEvent(
        t,
        _ev('tool_call_update', toolId: 't1', toolName: 'Read (2 files)'),
      );
      final card = t.items.firstWhere((i) => i.kind == ChatItemKind.tool);
      expect(card.toolName, 'Read (2 files)');
    });
  });

  group('no-op events return the identical instance', () {
    test('repeated session_status does not allocate', () {
      final t = applySessionEvent(
        base,
        _ev('session_status', status: 'running'),
      );
      final again = applySessionEvent(
        t,
        _ev('session_status', status: 'running'),
      );
      expect(identical(again, t), isTrue);
    });

    test('unchanged available_commands does not allocate', () {
      final ev = SessionEvent(
        type: 'available_commands',
        sessionId: 's1',
        commands: [AvailableCommand(name: 'plan', description: 'Plan it')],
      );
      final t = applySessionEvent(base, ev);
      final again = applySessionEvent(t, ev);
      expect(identical(again, t), isTrue);
    });
  });

  group('stable item ids survive the FIFO trim', () {
    test('seq is unique and monotonic, and tool index stays correct', () {
      var t = base;
      for (var i = 0; i < kMaxTranscriptItems + 50; i++) {
        t = applySessionEvent(t, _ev('user_message', text: 'm$i'));
      }
      t = applySessionEvent(
        t,
        _ev('tool_call', toolId: 'keep', toolName: 'Grep'),
      );
      // Push the tool card toward the front of the window.
      for (var i = 0; i < 100; i++) {
        t = applySessionEvent(t, _ev('user_message', text: 'after$i'));
      }
      final seqs = t.items.map((i) => i.seq).toList();
      expect(seqs.toSet().length, seqs.length, reason: 'seq must be unique');
      expect(seqs, orderedEquals(List.of(seqs)..sort()));

      // The rebuilt index must still point at the right card after trimming.
      t = applySessionEvent(
        t,
        _ev('tool_call_update', toolId: 'keep', status: 'completed'),
      );
      final cards = t.items.where((i) => i.toolId == 'keep').toList();
      expect(cards.length, 1, reason: 'update must not append a duplicate');
      expect(cards.single.toolName, 'Grep');
      expect(cards.single.toolStatus, 'completed');
    });
  });

  group('status resync from session.list', () {
    test('running -> idle is adopted when a turn_complete was lost', () {
      final t = applySessionEvent(
        base,
        _ev('session_status', status: 'running'),
      );
      final synced = applyMetaStatus(t, 'idle');
      expect(synced.status, 'idle');
    });

    test('a dead session is marked disconnected and drops pending', () {
      var t = applySessionEvent(base, _ev('session_status', status: 'running'));
      t = applySessionEvent(t, _ev('permission_request', permissionId: 'p1'));
      final synced = applyMetaStatus(t, 'idle', live: false);
      expect(synced.status, 'disconnected');
      expect(synced.hasPendingPermission, isFalse);
    });

    test('does not clobber a live running turn with a stale poll', () {
      final t = applySessionEvent(base, _ev('session_status', status: 'idle'));
      final synced = applyMetaStatus(t, 'running');
      expect(identical(synced, t), isTrue);
    });
  });

  group('transcript eviction', () {
    test('retainOnly drops sessions the host no longer reports', () {
      const a = SessionTranscript(sessionId: 'a');
      const b = SessionTranscript(sessionId: 'b');
      final state = const TranscriptsState().upsert(a).upsert(b);
      final kept = state.retainOnly({'a'});
      expect(kept.byId.keys, ['a']);
    });

    test('retainOnly is a no-op when everything is still live', () {
      const a = SessionTranscript(sessionId: 'a');
      final state = const TranscriptsState().upsert(a);
      expect(identical(state.retainOnly({'a'}), state), isTrue);
    });

    test('peek does not materialise a transcript', () {
      expect(const TranscriptsState().peek('nope'), isNull);
    });
  });
  group('usage_update (context-window indicator)', () {
    test('applies usage and parses from JSON', () {
      final ev = SessionEvent.fromJson({
        'type': 'usage_update',
        'session_id': 's1',
        'usage': {'used': 1200, 'size': 8000},
      });
      expect(ev.usage, isNotNull);
      final t = applySessionEvent(base, ev);
      expect(t.usage?.used, 1200);
      expect(t.usage?.size, 8000);
      // 1200/8000 = 15%.
      expect((t.usage!.fraction * 100).round(), 15);
      // No chat bubble — usage is header-only.
      expect(t.items, isEmpty);
    });

    test(
      'unchanged usage returns the identical instance (rebuild suppressed)',
      () {
        final ev = SessionEvent(
          type: 'usage_update',
          sessionId: 's1',
          usage: const Usage(used: 10, size: 100),
        );
        final t1 = applySessionEvent(base, ev);
        final t2 = applySessionEvent(t1, ev);
        expect(identical(t1, t2), isTrue);
      },
    );

    test('a new count replaces the old', () {
      var t = applySessionEvent(
        base,
        SessionEvent(
          type: 'usage_update',
          sessionId: 's1',
          usage: const Usage(used: 10, size: 100),
        ),
      );
      t = applySessionEvent(
        t,
        SessionEvent(
          type: 'usage_update',
          sessionId: 's1',
          usage: const Usage(used: 90, size: 100),
        ),
      );
      expect(t.usage?.used, 90);
      expect(t.usage!.fraction, 0.9);
    });

    test('size 0 yields fraction 0 (no divide-by-zero)', () {
      final u = Usage.fromJson({'used': 5, 'size': 0});
      expect(u.fraction, 0.0);
    });
  });

  group('session_capabilities', () {
    test('parses and applies; gates image', () {
      final ev = SessionEvent.fromJson({
        'type': 'session_capabilities',
        'session_id': 's1',
        'capabilities': {'image': true, 'audio': false, 'load_session': true},
      });
      final t = applySessionEvent(base, ev);
      expect(t.capabilities?.image, isTrue);
      expect(t.capabilities?.audio, isFalse);
      expect(t.capabilities?.loadSession, isTrue);
      expect(t.items, isEmpty);
    });
  });

  group('remote_commands', () {
    test('parses the daemon list, availability and reasons', () {
      final ev = SessionEvent.fromJson({
        'type': 'remote_commands',
        'session_id': 's1',
        'remote_commands': [
          {
            'name': 'compact',
            'description': 'Summarise the conversation',
            'available': true,
          },
          {
            'name': 'goal',
            'hint': '<objective>',
            'available': false,
            'reason': 'OpenCode has no goal loop',
          },
        ],
      });
      final t = applySessionEvent(base, ev);
      expect(t.remoteCommands.map((c) => c.name), ['compact', 'goal']);
      expect(t.remoteCommands.first.available, isTrue);
      expect(t.remoteCommands.last.available, isFalse);
      expect(t.remoteCommands.last.reason, 'OpenCode has no goal loop');
      expect(t.remoteCommands.last.hint, '<objective>');
      // Control state, not conversation.
      expect(t.items, isEmpty);
    });

    test('a leading slash in the name is stripped', () {
      final ev = SessionEvent.fromJson({
        'type': 'remote_commands',
        'session_id': 's1',
        'remote_commands': [
          {'name': '/help', 'available': true},
        ],
      });
      expect(applySessionEvent(base, ev).remoteCommands.single.name, 'help');
    });

    test('an unchanged list returns the identical instance', () {
      final ev = SessionEvent(
        type: 'remote_commands',
        sessionId: 's1',
        remoteCommands: [RemoteCommand(name: 'help', available: true)],
      );
      final once = applySessionEvent(base, ev);
      expect(identical(applySessionEvent(once, ev), once), isTrue);
    });

    test('a changed availability replaces the list', () {
      var t = applySessionEvent(
        base,
        SessionEvent(
          type: 'remote_commands',
          sessionId: 's1',
          remoteCommands: [RemoteCommand(name: 'context', available: false)],
        ),
      );
      t = applySessionEvent(
        t,
        SessionEvent(
          type: 'remote_commands',
          sessionId: 's1',
          remoteCommands: [RemoteCommand(name: 'context', available: true)],
        ),
      );
      expect(t.remoteCommands.single.available, isTrue);
    });
  });

  group('session_mode', () {
    test('full list sets modes + current; no chat bubble', () {
      final ev = SessionEvent.fromJson({
        'type': 'session_mode',
        'session_id': 's1',
        'modes': [
          {'id': 'ask', 'name': 'Ask'},
          {'id': 'code', 'name': 'Code'},
        ],
        'current_mode_id': 'code',
      });
      final t = applySessionEvent(base, ev);
      expect(t.modes.map((m) => m.id), ['ask', 'code']);
      expect(t.currentModeId, 'code');
      expect(t.items, isEmpty);
    });

    test('current-only update keeps the existing mode list', () {
      var t = applySessionEvent(
        base,
        SessionEvent(
          type: 'session_mode',
          sessionId: 's1',
          modes: const [
            SessionMode(id: 'ask', name: 'Ask'),
            SessionMode(id: 'code', name: 'Code'),
          ],
          currentModeId: 'ask',
        ),
      );
      t = applySessionEvent(
        t,
        SessionEvent(
          type: 'session_mode',
          sessionId: 's1',
          currentModeId: 'code',
        ),
      );
      expect(
        t.modes.length,
        2,
        reason: 'list must survive a current-only update',
      );
      expect(t.currentModeId, 'code');
    });
  });

  group('no-op replace events (MADR 0042 D8)', () {
    test('a re-sent identical session_mode returns the identical instance', () {
      var t = SessionTranscript(sessionId: 's1');
      SessionEvent modeEvent() => SessionEvent(
        type: 'session_mode',
        sessionId: 's1',
        modes: const [
          SessionMode(id: 'build', name: 'Build'),
          SessionMode(id: 'plan', name: 'Plan'),
        ],
        currentModeId: 'build',
      );

      t = applySessionEvent(t, modeEvent());
      expect(t.modes, hasLength(2));

      final again = applySessionEvent(t, modeEvent());
      expect(
        identical(again, t),
        isTrue,
        reason:
            'a fresh List instance must not count as a change — it '
            'republished the transcript and rebuilt the chat shell',
      );
    });

    test('a changed current mode still applies', () {
      var t = SessionTranscript(sessionId: 's1');
      t = applySessionEvent(
        t,
        SessionEvent(
          type: 'session_mode',
          sessionId: 's1',
          modes: const [SessionMode(id: 'build', name: 'Build')],
          currentModeId: 'build',
        ),
      );
      final next = applySessionEvent(
        t,
        SessionEvent(
          type: 'session_mode',
          sessionId: 's1',
          currentModeId: 'plan',
        ),
      );
      expect(identical(next, t), isFalse);
      expect(next.currentModeId, 'plan');
    });

    test(
      'a re-sent identical session_config returns the identical instance',
      () {
        var t = SessionTranscript(sessionId: 's1');
        SessionEvent configEvent() => SessionEvent(
          type: 'session_config',
          sessionId: 's1',
          configOptions: const [
            ConfigOption(
              id: 'verbosity',
              name: 'Verbosity',
              kind: 'select',
              currentValue: 'high',
              values: [ConfigOptionValue(id: 'high', name: 'High')],
            ),
          ],
        );

        t = applySessionEvent(t, configEvent());
        expect(t.configOptions, hasLength(1));

        final again = applySessionEvent(t, configEvent());
        expect(identical(again, t), isTrue);
      },
    );

    test('a changed option value still applies', () {
      var t = SessionTranscript(sessionId: 's1');
      t = applySessionEvent(
        t,
        SessionEvent(
          type: 'session_config',
          sessionId: 's1',
          configOptions: const [
            ConfigOption(id: 'v', name: 'V', kind: 'select', currentValue: 'a'),
          ],
        ),
      );
      final next = applySessionEvent(
        t,
        SessionEvent(
          type: 'session_config',
          sessionId: 's1',
          configOptions: const [
            ConfigOption(id: 'v', name: 'V', kind: 'select', currentValue: 'b'),
          ],
        ),
      );
      expect(identical(next, t), isFalse);
      expect(next.configOptions.single.currentValue, 'b');
    });
  });

  group('error text clipping (MADR 0042 D8)', () {
    test('a multi-line failure keeps its line structure', () {
      final t = applySessionEvent(
        SessionTranscript(sessionId: 's1'),
        SessionEvent(
          type: 'error',
          sessionId: 's1',
          error: 'build failed\n  at foo.dart:12\n  at bar.dart:3',
        ),
      );
      final text = t.items.single.text ?? '';
      expect(
        text,
        contains('\n'),
        reason:
            'flattening a stack trace makes the one thing the user needs '
            'to read unreadable',
      );
      expect(text, contains('at foo.dart:12'));
    });

    test('runs of spaces still collapse', () {
      final t = applySessionEvent(
        SessionTranscript(sessionId: 's1'),
        SessionEvent(type: 'error', sessionId: 's1', error: 'a     b\t\tc'),
      );
      expect(t.items.single.text, 'a b c');
    });

    test('a clip landing on a surrogate pair stays valid UTF-16', () {
      // 300 chars is the error cap; put an astral emoji straddling it.
      final head = 'x' * 299;
      final t = applySessionEvent(
        SessionTranscript(sessionId: 's1'),
        SessionEvent(
          type: 'error',
          sessionId: 's1',
          error: '$head😀 trailing detail that pushes past the cap',
        ),
      );
      final text = t.items.single.text ?? '';
      for (var i = 0; i < text.length; i++) {
        final unit = text.codeUnitAt(i);
        final isHigh = (unit & 0xFC00) == 0xD800;
        final isLow = (unit & 0xFC00) == 0xDC00;
        if (isHigh) {
          expect(
            i + 1 < text.length && (text.codeUnitAt(i + 1) & 0xFC00) == 0xDC00,
            isTrue,
            reason: 'high surrogate at $i has no pair',
          );
        }
        if (isLow) {
          expect(
            i > 0 && (text.codeUnitAt(i - 1) & 0xFC00) == 0xD800,
            isTrue,
            reason: 'lone low surrogate at $i',
          );
        }
      }
    });
  });

  group('session_config', () {
    test('parses select + boolean options', () {
      final ev = SessionEvent.fromJson({
        'type': 'session_config',
        'session_id': 's1',
        'config_options': [
          {
            'id': 'reasoning',
            'name': 'Reasoning',
            'kind': 'select',
            'current_value': 'smart',
            'values': [
              {'id': 'fast', 'name': 'Fast'},
              {'id': 'smart', 'name': 'Smart'},
            ],
          },
          {'id': 'web', 'name': 'Web', 'kind': 'boolean', 'bool_value': true},
        ],
      });
      final t = applySessionEvent(base, ev);
      expect(t.configOptions.length, 2);
      expect(t.configOptions[0].currentValue, 'smart');
      expect(t.configOptions[0].values.length, 2);
      expect(t.configOptions[1].isBoolean, isTrue);
      expect(t.configOptions[1].boolValue, isTrue);
    });

    test('a single-option update merges, keeping the others', () {
      var t = applySessionEvent(
        base,
        SessionEvent(
          type: 'session_config',
          sessionId: 's1',
          configOptions: const [
            ConfigOption(
              id: 'web',
              name: 'Web',
              kind: 'boolean',
              boolValue: false,
            ),
            ConfigOption(
              id: 'reasoning',
              name: 'Reasoning',
              kind: 'select',
              currentValue: 'fast',
            ),
          ],
        ),
      );
      // Echo just the toggled option.
      t = applySessionEvent(
        t,
        SessionEvent(
          type: 'session_config',
          sessionId: 's1',
          configOptions: const [
            ConfigOption(
              id: 'web',
              name: 'Web',
              kind: 'boolean',
              boolValue: true,
            ),
          ],
        ),
      );
      expect(
        t.configOptions.length,
        2,
        reason: 'merge must not drop reasoning',
      );
      expect(
        t.configOptions.firstWhere((o) => o.id == 'web').boolValue,
        isTrue,
      );
    });
  });

  group('user_message attachments', () {
    test('image-only prompt still appends a bubble carrying the descriptor', () {
      final ev = SessionEvent.fromJson({
        'type': 'user_message',
        'session_id': 's1',
        'text': '',
        'attachments': [
          {'kind': 'image', 'mime_type': 'image/png'},
        ],
      });
      final t = applySessionEvent(base, ev);
      expect(t.items, hasLength(1));
      final item = t.items.single;
      expect(item.kind, ChatItemKind.user);
      expect(item.attachments, hasLength(1));
      expect(item.attachments.single.kind, 'image');
      expect(item.attachments.single.mimeType, 'image/png');
      // No local bytes on the reducer path (added later on the sending device).
      expect(item.attachments.single.bytes, isNull);
    });

    test('text + attachments render both', () {
      final ev = SessionEvent(
        type: 'user_message',
        sessionId: 's1',
        text: 'look',
        attachments: const [
          AttachmentInfo(kind: 'image', mimeType: 'image/jpeg'),
        ],
      );
      final item = applySessionEvent(base, ev).items.single;
      expect(item.text, 'look');
      expect(item.attachments, hasLength(1));
    });

    test(
      'ChatItem attachment descriptors round-trip cache JSON (no bytes)',
      () {
        final item = ChatItem.user(
          'hi',
          attachments: const [
            ChatAttachment(kind: 'image', mimeType: 'image/png'),
          ],
        );
        final restored = ChatItem.fromJson(item.toJson());
        expect(restored.attachments, hasLength(1));
        expect(restored.attachments.single.kind, 'image');
        expect(restored.attachments.single.bytes, isNull);
      },
    );

    test('identical tool update returns identical transcript instance', () {
      final t1 = applySessionEvent(
        base,
        _ev(
          'tool_call',
          toolId: 't1',
          toolName: 'bash',
          toolKind: 'execute',
          text: 'ls',
        ),
      );
      final t2 = applySessionEvent(
        t1,
        _ev(
          'tool_call_update',
          toolId: 't1',
          toolName: 'bash',
          toolKind: 'execute',
          text: 'ls',
        ),
      );
      expect(identical(t2, t1), isTrue);

      // A change in status updates
      final t3 = applySessionEvent(
        t2,
        _ev('tool_call_update', toolId: 't1', status: 'completed'),
      );
      expect(identical(t3, t2), isFalse);
      expect(t3.items.single.toolStatus, 'completed');

      // Empty text update does not clear existing text
      final t4 = applySessionEvent(
        t3,
        _ev('tool_call_update', toolId: 't1', text: ''),
      );
      expect(identical(t4, t3), isTrue);
      expect(t4.items.single.text, 'ls');
    });
  });
}
