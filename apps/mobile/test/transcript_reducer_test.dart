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
}
