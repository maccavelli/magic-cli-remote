import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

// PLAN 0188 N2: an ACP agent's `agent_notice` becomes a transcript row that
// keeps its severity, and survives the local transcript cache (MADR 0188 D3).

SessionEvent _notice(String severity, String title, [String? description]) =>
    SessionEvent(
      type: 'agent_notice',
      sessionId: 's1',
      noticeSeverity: severity,
      noticeTitle: title,
      noticeDescription: description,
    );

void main() {
  const base = SessionTranscript(sessionId: 's1');

  test('agent_notice appends an agent-notice row with its severity', () {
    final t = applySessionEvent(
      base,
      _notice('warning', 'gopls unavailable', 'Navigation is limited.'),
    );
    expect(t.items, hasLength(1));
    final item = t.items.single;
    expect(item.kind, ChatItemKind.system);
    expect(item.isAgentNotice, isTrue);
    expect(item.text, 'gopls unavailable');
    expect(item.noticeSeverity, 'warning');
    expect(item.noticeDetail, 'Navigation is limited.');
    // Severity drives the row, never the generic error flag.
    expect(item.isError, isFalse);
  });

  test('an unknown severity is kept as sent; the row draws it as info', () {
    final t = applySessionEvent(base, _notice('critical', 'Future severity'));
    expect(t.items.single.noticeSeverity, 'critical');
    expect(t.items.single.noticeDetail, isNull);
  });

  test('agent_notice without a title is a no-op', () {
    final t = applySessionEvent(base, _notice('info', '   '));
    expect(identical(t, base), isTrue);
  });

  test('the daemon notice stays a plain system line', () {
    final t = applySessionEvent(
      base,
      SessionEvent(type: 'notice', sessionId: 's1', text: 'Model is grok-4.'),
    );
    expect(t.items.single.isAgentNotice, isFalse);
  });

  test('an agent notice survives the transcript cache', () {
    const item = ChatItem(
      kind: ChatItemKind.system,
      text: 'Rate limited',
      noticeSeverity: 'error',
      noticeDetail: 'Retry in a minute.',
    );
    final back = ChatItem.fromJson(item.toJson());
    expect(back.isAgentNotice, isTrue);
    expect(back.noticeSeverity, 'error');
    expect(back.noticeDetail, 'Retry in a minute.');
    expect(back.text, 'Rate limited');
  });
}
