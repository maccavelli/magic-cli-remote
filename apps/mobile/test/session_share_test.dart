import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/session_share_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Session sharing (MADR 0112 A8, PLAN P9 steps 5 and 6).
///
/// The assertions that matter are about consent and honesty: nothing is
/// published without an explicit confirmation that names the consequence, and
/// an existing public link is never hidden just because this daemon may not
/// change it.

class _ShareClient extends McremoteClient {
  _ShareClient({this.current = const ShareState(), this.error});

  ShareState current;
  final Object? error;

  int shares = 0;
  int unshares = 0;
  int reads = 0;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<ShareState> shareState(String sessionId) async {
    reads++;
    return current;
  }

  @override
  Future<ShareState> share(String sessionId) async {
    shares++;
    final err = error;
    if (err != null) throw err;
    current = const ShareState(shared: true, url: 'https://opencode.ai/s/new');
    return current;
  }

  @override
  Future<void> unshare(String sessionId) async {
    unshares++;
    final err = error;
    if (err != null) throw err;
    current = const ShareState();
  }
}

Future<void> _pump(
  WidgetTester tester,
  _ShareClient client, {
  bool canMutate = false,
}) async {
  tester.view.physicalSize = const Size(700, 1500);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [mcremoteClientProvider.overrideWithValue(client)],
      child: MaterialApp(
        home: Scaffold(
          body: SessionShareSheet(sessionId: 's1', canMutate: canMutate),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  group('state display', () {
    testWidgets('a private session says so and offers to share', (
      tester,
    ) async {
      await _pump(tester, _ShareClient(), canMutate: true);
      expect(find.text('This session is private.'), findsOneWidget);
      expect(find.byKey(const ValueKey('share-start')), findsOneWidget);
      expect(find.byKey(const ValueKey('share-unshare')), findsNothing);
    });

    testWidgets('a shared session shows its link', (tester) async {
      await _pump(
        tester,
        _ShareClient(
          current: const ShareState(
            shared: true,
            url: 'https://opencode.ai/s/abc',
          ),
        ),
        canMutate: true,
      );
      expect(find.text('This session is shared publicly.'), findsOneWidget);
      expect(find.text('https://opencode.ai/s/abc'), findsOneWidget);
      expect(find.byKey(const ValueKey('share-unshare')), findsOneWidget);
    });

    testWidgets('shared without a verifiable link still reads as public', (
      tester,
    ) async {
      await _pump(
        tester,
        _ShareClient(current: const ShareState(shared: true)),
        canMutate: true,
      );
      expect(find.text('This session is shared publicly.'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('share-unverified')),
        findsOneWidget,
        reason: 'the transcript is public whether or not the link validated',
      );
    });
  });

  group('mutation policy', () {
    testWidgets('state and link stay visible when mutation is forbidden', (
      tester,
    ) async {
      await _pump(
        tester,
        _ShareClient(
          current: const ShareState(
            shared: true,
            url: 'https://opencode.ai/s/abc',
          ),
        ),
      );
      expect(find.text('This session is shared publicly.'), findsOneWidget);
      expect(
        find.text('https://opencode.ai/s/abc'),
        findsOneWidget,
        reason: 'hiding an existing public link is the dangerous silence',
      );
      // Only the controls are gone.
      expect(find.byKey(const ValueKey('share-start')), findsNothing);
      expect(find.byKey(const ValueKey('share-unshare')), findsNothing);
    });

    testWidgets('upstream disabled hides the controls even when permitted', (
      tester,
    ) async {
      await _pump(
        tester,
        _ShareClient(current: const ShareState(disabled: true)),
        canMutate: true,
      );
      expect(
        find.byKey(const ValueKey('share-upstream-disabled')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('share-start')),
        findsNothing,
        reason: 'upstream policy is never overridden',
      );
    });
  });

  group('publishing', () {
    testWidgets('confirmation names exactly what becomes public', (
      tester,
    ) async {
      final c = _ShareClient();
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-start')));
      await tester.pumpAndSettle();

      expect(find.text('Share this session?'), findsOneWidget);
      expect(find.textContaining('whole transcript'), findsOneWidget);
      expect(find.textContaining('tool output'), findsOneWidget);
      expect(find.textContaining('Anyone with the link'), findsOneWidget);
      expect(find.textContaining('no password'), findsOneWidget);
    });

    testWidgets('cancelling publishes nothing', (tester) async {
      final c = _ShareClient();
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(
        c.shares,
        0,
        reason: 'a cancelled confirmation must publish nothing',
      );
      expect(find.text('This session is private.'), findsOneWidget);
    });

    testWidgets('confirming publishes exactly once and shows the link', (
      tester,
    ) async {
      final c = _ShareClient();
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('share-confirm')));
      await tester.pumpAndSettle();

      expect(c.shares, 1, reason: 'a retried share can publish twice');
      expect(find.text('https://opencode.ai/s/new'), findsOneWidget);
      expect(find.byKey(const ValueKey('share-unshare')), findsOneWidget);
    });

    testWidgets('a failed share is not retried', (tester) async {
      final c = _ShareClient(error: Exception('upstream refused'));
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('share-confirm')));
      await tester.pumpAndSettle();

      expect(c.shares, 1);
      expect(find.byKey(const ValueKey('share-error')), findsOneWidget);
    });

    testWidgets('a disabled host explains itself', (tester) async {
      final c = _ShareClient(error: Exception('share_disabled'));
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('share-confirm')));
      await tester.pumpAndSettle();

      final error = tester.widget<Text>(
        find.byKey(const ValueKey('share-error')),
      );
      expect(error.data, contains('not enabled'));
    });
  });

  group('revoking', () {
    testWidgets('unsharing clears the state and the link', (tester) async {
      final c = _ShareClient(
        current: const ShareState(
          shared: true,
          url: 'https://opencode.ai/s/abc',
        ),
      );
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-unshare')));
      await tester.pumpAndSettle();

      expect(c.unshares, 1);
      expect(find.text('This session is private.'), findsOneWidget);
      expect(find.text('https://opencode.ai/s/abc'), findsNothing);
    });

    testWidgets('unsharing needs no confirmation — it removes exposure', (
      tester,
    ) async {
      final c = _ShareClient(
        current: const ShareState(
          shared: true,
          url: 'https://opencode.ai/s/abc',
        ),
      );
      await _pump(tester, c, canMutate: true);
      await tester.tap(find.byKey(const ValueKey('share-unshare')));
      await tester.pump();
      expect(find.text('Share this session?'), findsNothing);
    });
  });

  test('ShareState decodes and reports link presence', () {
    final s = ShareState.fromJson(const {
      'shared': true,
      'url': 'https://opencode.ai/s/abc',
    });
    expect(s.shared, isTrue);
    expect(s.hasLink, isTrue);
    expect(s.disabled, isFalse);

    final empty = ShareState.fromJson(const {});
    expect(empty.shared, isFalse);
    expect(empty.hasLink, isFalse);

    final disabled = ShareState.fromJson(const {'disabled': true});
    expect(disabled.disabled, isTrue);
  });
}
