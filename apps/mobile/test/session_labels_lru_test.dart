import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/notifications/notification_coordinator.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

/// MADR 0084 D5/C1: the label map used to grow by one entry per session title
/// ever seen, for the life of an app-lifetime object.
void main() {
  test('sessionLabels evicts the oldest past the cap', () {
    final coord = NotificationCoordinator(client: McremoteClient());
    addTearDown(coord.dispose);

    for (var i = 0; i < kTranscriptCacheMaxSessions + 3; i++) {
      coord.sessionLabels['s$i'] = 'Session $i';
    }

    expect(coord.sessionLabels, hasLength(kTranscriptCacheMaxSessions));
    expect(coord.sessionLabels.containsKey('s0'), isFalse);
    expect(coord.sessionLabels.containsKey('s2'), isFalse);
    expect(
      coord.sessionLabels['s${kTranscriptCacheMaxSessions + 2}'],
      'Session ${kTranscriptCacheMaxSessions + 2}',
    );
  });

  test('re-titling an existing session does not grow the map', () {
    final coord = NotificationCoordinator(client: McremoteClient());
    addTearDown(coord.dispose);

    coord.sessionLabels['a'] = 'first';
    coord.sessionLabels['a'] = 'second';

    expect(coord.sessionLabels, hasLength(1));
    expect(coord.sessionLabels['a'], 'second');
  });

  test('a refreshed key survives eviction of genuinely older ones', () {
    final coord = NotificationCoordinator(client: McremoteClient());
    addTearDown(coord.dispose);

    coord.sessionLabels['oldest'] = 'x';
    for (var i = 0; i < kTranscriptCacheMaxSessions - 1; i++) {
      coord.sessionLabels['s$i'] = 'y';
    }
    // Touching it moves it to the newest position.
    coord.sessionLabels['oldest'] = 'x2';
    coord.sessionLabels['one-more'] = 'z';

    expect(coord.sessionLabels['oldest'], 'x2');
    expect(coord.sessionLabels.containsKey('s0'), isFalse);
  });
}
