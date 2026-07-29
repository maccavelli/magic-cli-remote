import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

/// The auto-approval card: one collapsing item per turn instead of one system
/// line per approved permission (MADR 0051 Part I).
///
/// The property that matters is the *upsert*. Each `approval_summary` event
/// carries the full list from the start of the turn, so a reducer that appended
/// would reprint every earlier approval on every event — strictly worse than
/// the per-approval notices this replaces.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  SessionEvent summary(
    List<(String, String)> approvals, {
    String group = 'auto-approvals',
    String status = 'running',
  }) => SessionEvent(
    type: 'approval_summary',
    sessionId: 's1',
    status: status,
    approvalGroupId: group,
    text: 'Auto-approved (${approvals.length})',
    approvals: [
      for (final (tool, detail) in approvals)
        ApprovalItem(toolName: tool, detail: detail),
    ],
  );

  const empty = SessionTranscript(sessionId: 's1');

  group('reducer', () {
    test('a growing turn produces exactly one card', () {
      var t = applySessionEvent(empty, summary([('bash', 'git status')]));
      t = applySessionEvent(t, summary([
        ('bash', 'git status'),
        ('file', 'header.html'),
      ]));
      t = applySessionEvent(t, summary([
        ('bash', 'git status'),
        ('file', 'header.html'),
        ('bash', 'make'),
      ]));

      final cards = t.items
          .where((i) => i.kind == ChatItemKind.approvals)
          .toList();
      expect(
        cards,
        hasLength(1),
        reason: 'each event carries the full list; appending would reprint '
            'every earlier approval',
      );
      expect(cards.single.approvals.map((a) => a.detail), [
        'git status',
        'header.html',
        'make',
      ]);
    });

    test('a terminal summary marks the same card completed', () {
      var t = applySessionEvent(empty, summary([('bash', 'git status')]));
      t = applySessionEvent(
        t,
        summary([('bash', 'git status')], status: 'completed'),
      );
      final cards = t.items.where((i) => i.kind == ChatItemKind.approvals);
      expect(cards, hasLength(1));
      expect(cards.single.toolStatus, 'completed');
    });

    test('distinct group ids get distinct cards', () {
      var t = applySessionEvent(empty, summary([('bash', 'a')]));
      t = applySessionEvent(t, summary([('bash', 'b')], group: 'other'));
      expect(t.items.where((i) => i.kind == ChatItemKind.approvals), hasLength(2));
    });

    test('a re-sent identical snapshot does not rebuild the transcript', () {
      final t = applySessionEvent(empty, summary([('bash', 'git status')]));
      final again = applySessionEvent(t, summary([('bash', 'git status')]));
      expect(
        identical(t, again),
        isTrue,
        reason: 'an unchanged snapshot must not churn the item list',
      );
    });

    test('an event with no group id or no approvals renders nothing', () {
      expect(
        applySessionEvent(empty, summary([('bash', 'x')], group: '')).items,
        isEmpty,
      );
      expect(applySessionEvent(empty, summary(const [])).items, isEmpty);
    });

    test('a session that never auto-approves shows no card', () {
      final t = applySessionEvent(
        empty,
        SessionEvent(type: 'assistant_message_chunk', sessionId: 's1', text: 'hi'),
      );
      expect(t.items.any((i) => i.kind == ChatItemKind.approvals), isFalse);
    });
  });

  group('cache round-trip', () {
    test('approvals survive serialisation', () {
      final item = ChatItem.approvals(
        id: 'auto-approvals',
        status: 'completed',
        approvals: [
          ApprovalItem(
            toolName: 'bash',
            detail: 'git status',
            time: DateTime.utc(2026, 7, 29, 14, 2, 3),
          ),
        ],
      );
      final back = ChatItem.fromJson(item.toJson());
      expect(back.kind, ChatItemKind.approvals);
      expect(back.toolStatus, 'completed');
      expect(back.approvals, hasLength(1));
      expect(back.approvals.single.toolName, 'bash');
      expect(back.approvals.single.detail, 'git status');
      expect(back.approvals.single.time, DateTime.utc(2026, 7, 29, 14, 2, 3));
    });
  });

  // Rendering lives in chat_render_test.dart, which owns the ChatScreen host
  // harness these cards have to be pumped through.
}
