import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Events shaped like a grok turn: no native message ids, so every assistant
/// chunk takes the id-less folding path in the reducer. That is the shape four
/// of five providers emit (MADR 0141 F5).
SessionEvent _ev(
  String type,
  int seq, {
  String? text,
  String? toolId,
  String? toolName,
  String? status,
}) {
  return SessionEvent.fromJson({
    'type': type,
    'session_id': 's1',
    'seq': seq,
    'text': ?text,
    'tool_id': ?toolId,
    'tool_name': ?toolName,
    'status': ?status,
  });
}

void main() {
  late ProviderContainer c;
  setUp(() => c = ProviderContainer());
  tearDown(() => c.dispose());

  // Acceptance 3. Two separately fetched pages must not merge one turn's text
  // into another's.
  //
  // The join must be assistant-to-assistant to exercise the hazard at all: the
  // reducer only folds an id-less assistant chunk into a preceding id-less
  // assistant bubble. That is the realistic shape, because a 200-event page
  // boundary lands mid-message roughly one time in seven (~3 chunks per
  // message, 22 events per turn).
  //
  // An earlier version of this test put a user_message at the head of the newer
  // page, so the join was never assistant-to-assistant and it passed against a
  // deliberately broken join. It asserted something true about a case that
  // could not fail.
  test(
    'a prepended page does not merge its last bubble into the newer page',
    () async {
      final n = c.read(transcriptsProvider.notifier);

      // The newer page opens mid-message: its first item is an assistant chunk.
      await n.replayHistory('s1', [
        _ev('assistant_message_chunk', 201, text: 'NEWER ANSWER'),
      ]);

      // The older page ends with an assistant message of its own turn.
      await n.prependHistory('s1', [
        _ev('user_message', 1, text: 'older turn'),
        _ev('assistant_message_chunk', 2, text: 'OLDER ANSWER'),
      ]);

      final items = c.read(transcriptsProvider).peek('s1')!.items;
      final assistants = items
          .where((i) => i.kind == ChatItemKind.assistant)
          .toList();

      expect(
        assistants.length,
        2,
        reason:
            "the two pages' assistant messages merged into one bubble: "
            '${assistants.map((a) => a.text).toList()}',
      );
      expect(assistants.first.text, 'OLDER ANSWER');
      expect(assistants.last.text, 'NEWER ANSWER');

      // Order: the older page must sit ahead of the newer.
      final texts = items.map((i) => i.text).join(' | ');
      expect(
        texts.indexOf('OLDER'),
        lessThan(texts.indexOf('NEWER')),
        reason: 'the older page was not placed ahead: $texts',
      );
    },
  );

  // The deviation's other half: toolIndex holds item positions, so prepending
  // must rebase it or the next update lands on the wrong card.
  test('a tool update after a prepend still finds its own card', () async {
    final n = c.read(transcriptsProvider.notifier);

    await n.replayHistory('s1', [
      _ev('user_message', 201, text: 'newer turn'),
      _ev('tool_call', 202, toolId: 'tool-NEW', toolName: 'newer tool'),
    ]);
    await n.prependHistory('s1', [
      _ev('user_message', 1, text: 'older turn'),
      _ev('tool_call', 2, toolId: 'tool-OLD', toolName: 'older tool'),
    ]);

    // Update the newer tool. Its index moved by the size of the older page.
    n.debugOnEvent(
      _ev(
        'tool_call_update',
        203,
        toolId: 'tool-NEW',
        status: 'completed',
        text: 'NEW RESULT',
      ),
    );

    final items = c.read(transcriptsProvider).peek('s1')!.items;
    final tools = items.where((i) => i.kind == ChatItemKind.tool).toList();

    expect(
      tools.length,
      2,
      reason:
          'the update appended a duplicate card instead of finding its own: '
          '${tools.map((t) => '${t.toolName}/${t.text}').toList()}',
    );

    final older = tools.firstWhere((t) => t.toolName == 'older tool');
    final newer = tools.firstWhere((t) => t.toolName == 'newer tool');
    expect(
      newer.text,
      contains('NEW RESULT'),
      reason: 'the newer tool card did not receive its own update',
    );
    expect(
      older.text,
      isNot(contains('NEW RESULT')),
      reason: 'the update landed on the OLDER card — toolIndex was not rebased',
    );
  });

  test('prepending onto an empty transcript is refused', () async {
    final n = c.read(transcriptsProvider.notifier);
    await n.prependHistory('s1', [
      _ev('assistant_message_chunk', 1, text: 'x'),
    ]);
    final t = c.read(transcriptsProvider).peek('s1');
    expect(
      t == null || t.items.isEmpty,
      isTrue,
      reason:
          'prepending with nothing to page back from would leave the '
          'older page owning items.last, and the next live chunk would fold into it',
    );
  });

  test(
    'prepending records the older boundary so a resync sees the gap',
    () async {
      final n = c.read(transcriptsProvider.notifier);
      await n.replayHistory('s1', [
        _ev('assistant_message_chunk', 500, text: 'newer'),
      ]);
      expect(n.debugFirstSeq('s1'), 500);
      await n.prependHistory('s1', [
        _ev('assistant_message_chunk', 300, text: 'older'),
      ]);
      expect(
        n.debugFirstSeq('s1'),
        300,
        reason: 'firstSeq must follow the oldest event now held',
      );
    },
  );
}
