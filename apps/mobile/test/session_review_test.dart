import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

void main() {
  test('review notices and assistant text apply once', () {
    var t = const SessionTranscript(sessionId: 's1');
    t = applySessionEvent(
      t,
      SessionEvent(
        type: 'notice',
        sessionId: 's1',
        text: 'Entered review mode',
      ),
    );
    t = applySessionEvent(
      t,
      SessionEvent(
        type: 'assistant_message_chunk',
        sessionId: 's1',
        text: 'Looks good.',
      ),
    );
    t = applySessionEvent(
      t,
      SessionEvent(type: 'notice', sessionId: 's1', text: 'Exited review mode'),
    );
    expect(t.items.where((i) => i.text == 'Entered review mode').length, 1);
    expect(t.items.where((i) => i.text == 'Exited review mode').length, 1);
    expect(t.items.where((i) => i.text == 'Looks good.').length, 1);
  });

  test('exited fallback is a single assistant chunk when no other text', () {
    var t = const SessionTranscript(sessionId: 's1');
    t = applySessionEvent(
      t,
      SessionEvent(
        type: 'assistant_message_chunk',
        sessionId: 's1',
        text: 'fallback review',
      ),
    );
    expect(t.items.where((i) => i.text == 'fallback review').length, 1);
  });
}
