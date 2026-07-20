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
  String? stopReason,
  String? error,
  String? permissionId,
}) {
  return SessionEvent(
    type: type,
    sessionId: sessionId,
    text: text,
    status: status,
    toolId: toolId,
    toolName: toolName,
    stopReason: stopReason,
    error: error,
    permissionId: permissionId,
  );
}

void main() {
  const base = SessionTranscript(sessionId: 's1');

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
    var t = applySessionEvent(base, _ev('assistant_message_chunk', text: 'Hel'));
    t = applySessionEvent(t, _ev('assistant_message_chunk', text: 'lo'));
    expect(t.items, hasLength(1));
    expect(t.items.first.text, 'Hello');
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

  test('session_status updates status only', () {
    final next = applySessionEvent(base, _ev('session_status', status: 'running'));
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
    final next = applySessionEvent(
      base,
      _ev('error', error: 'boom'),
    );
    expect(next.status, 'error');
    expect(next.items.first.kind, ChatItemKind.system);
    expect(next.items.first.text, contains('boom'));
  });

  test('permission_request sets pending', () {
    final next = applySessionEvent(
      base,
      _ev(
        'permission_request',
        permissionId: 'p1',
        toolName: 'Write',
      ),
    );
    expect(next.pendingPermission?.permissionId, 'p1');
    expect(next.items.first.text, contains('Write'));
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
          PlanEntry(content: 'Read code', status: 'completed', priority: 'high'),
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
      var t = applySessionEvent(
        base,
        planEv([PlanEntry(content: 'step one')]),
      );
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
      final cancels =
          t.items.where((i) => i.text == 'Turn cancelled').length;
      expect(cancels, 1);
    });

    test('turn_complete then local announce appends only one line', () {
      var t = applySessionEvent(
        base,
        _ev('turn_complete', stopReason: 'cancelled'),
      );
      t = markCancelAnnounced(t);
      final cancels =
          t.items.where((i) => i.text == 'Turn cancelled').length;
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
        _ev('tool_call_update',
            toolId: 't1', toolName: 'tool', status: 'completed'),
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
      final t = applySessionEvent(base, _ev('session_status', status: 'running'));
      final again = applySessionEvent(t, _ev('session_status', status: 'running'));
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
      final t = applySessionEvent(base, _ev('session_status', status: 'running'));
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
}
