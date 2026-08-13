import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/diagnostics/error_recorder.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A store whose recent-error ring lives in memory, and which can be made to
/// fail on demand.
class _FakeStore extends SettingsStore {
  List<Map<String, dynamic>> entries = [];
  bool throwOnWrite = false;
  bool throwOnRead = false;

  @override
  Future<List<Map<String, dynamic>>> getRecentErrors() async {
    if (throwOnRead) throw Exception('store unavailable');
    return entries;
  }

  @override
  Future<void> appendRecentError(
    Map<String, dynamic> entry, {
    int maxEntries = 5,
  }) async {
    if (throwOnWrite) throw Exception('store unavailable');
    final next = [...entries, entry];
    entries = next.length > maxEntries
        ? next.sublist(next.length - maxEntries)
        : next;
  }

  @override
  Future<void> clearRecentErrors() async => entries = [];
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('a recorded error round-trips', () async {
    final store = _FakeStore();
    final at = DateTime.utc(2026, 8, 13, 9, 30);
    final recorder = ErrorRecorder(store, clock: () => at);

    await recorder.record(
      StateError('boom'),
      StackTrace.fromString('#0 frame one\n#1 frame two'),
      source: ErrorSource.flutter,
    );

    final got = await recorder.recent();
    expect(got, hasLength(1));
    expect(got.single.kind, 'StateError');
    expect(got.single.message, contains('boom'));
    expect(got.single.stack, contains('frame one'));
    expect(got.single.source, ErrorSource.flutter);
    expect(got.single.at, at);
  });

  test('the ring keeps the newest entries, newest first for display', () async {
    final store = _FakeStore();
    final recorder = ErrorRecorder(store);

    for (var i = 0; i < kRecordedErrorRingSize + 1; i++) {
      await recorder.record(StateError('e$i'), null, source: ErrorSource.app);
    }

    final got = await recorder.recent();
    expect(got, hasLength(kRecordedErrorRingSize));
    // Oldest ('e0') evicted; display order is newest first.
    expect(got.first.message, contains('e$kRecordedErrorRingSize'));
    expect(got.map((e) => e.message).join(), isNot(contains('e0')));
  });

  test('message and stack are bounded to the declared constants', () async {
    final store = _FakeStore();
    final recorder = ErrorRecorder(store);

    await recorder.record(
      StateError('x' * 5000),
      StackTrace.fromString(
        List.generate(200, (i) => '#$i a frame').join('\n'),
      ),
      source: ErrorSource.platform,
    );

    final got = (await recorder.recent()).single;
    expect(got.message.length, kRecordedErrorMaxMessageChars);
    expect(got.stack.split('\n'), hasLength(kRecordedErrorMaxStackLines));
    expect(got.stack.length, lessThanOrEqualTo(kRecordedErrorMaxStackChars));
  });

  test('record never throws when the store fails', () async {
    final store = _FakeStore()..throwOnWrite = true;
    final recorder = ErrorRecorder(store);

    // The diagnostics layer failing must not take down the error path it
    // serves — that would turn one bug into a crash loop.
    await expectLater(
      recorder.record(StateError('boom'), null, source: ErrorSource.flutter),
      completes,
    );
  });

  test('recent returns empty when the store fails or holds junk', () async {
    final failing = ErrorRecorder(_FakeStore()..throwOnRead = true);
    expect(await failing.recent(), isEmpty);

    final junk = _FakeStore()
      ..entries = [
        {'not': 'an error'},
        {'kind': 'X', 'message': 'm', 'at': 'not-a-date', 'source': 'app'},
      ];
    // Malformed entries are dropped, never allowed to break the list.
    expect(await ErrorRecorder(junk).recent(), isEmpty);
  });

  test('clipboard text carries kind, time, message and stack', () async {
    final store = _FakeStore();
    final recorder = ErrorRecorder(store);
    await recorder.record(
      ArgumentError('bad input'),
      StackTrace.fromString('#0 somewhere'),
      source: ErrorSource.app,
    );

    final text = (await recorder.recent()).single.toClipboardText();
    expect(text, contains('ArgumentError'));
    expect(text, contains('bad input'));
    expect(text, contains('somewhere'));
    expect(text, contains(ErrorSource.app));
  });

  test('the real store persists and bounds the ring', () async {
    final store = SettingsStore();
    for (var i = 0; i < 8; i++) {
      await store.appendRecentError({
        'kind': 'StateError',
        'message': 'e$i',
        'at': DateTime.utc(2026, 8, 13, 9, i).toIso8601String(),
        'source': ErrorSource.app,
      });
    }
    final got = await store.getRecentErrors();
    expect(got, hasLength(5));
    expect(got.first['message'], 'e3');
    expect(got.last['message'], 'e7');

    await store.clearRecentErrors();
    expect(await store.getRecentErrors(), isEmpty);
  });
}
