import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';
import 'package:magic_cli_remote/theme/celestial.dart';
import 'package:magic_cli_remote/theme/starfield.dart';
import 'package:magic_cli_remote/theme/widgets.dart';

import 'support/fake_path_provider.dart';

class MockMcremoteClient extends McremoteClient {
  MockMcremoteClient({
    this.sessions = const <SessionMeta>[],
    this.providers = const <ProviderInfo>[],
    this.modelOption,
    LinkHealth health = LinkHealth.fresh,
  }) {
    // A fake that reports `connected` must also say the link is answering:
    // since MADR 0063 the two are separate claims, and green requires both.
    // Overriding `state` alone leaves the freshness clock at its initial
    // `lost`, which is correct for a client that has never seen a frame.
    linkHealth.value = health;
  }

  // Not final: the delete spies below mutate it to model host truth
  // (MADR 0095 F4).
  List<SessionMeta> sessions;
  final List<ProviderInfo> providers;
  final PickerOption? modelOption;

  // Handoff spies (MADR 0078).
  List<DeviceInfo> devices = const [];
  final List<({String id, String? to})> releaseCalls = [];
  final List<String> claimCalls = [];

  // Delete spies (MADR 0095 F4 / D5).
  final List<String> deleteCalls = [];
  bool failDelete = false;
  bool failDeleteKeepsRow = false;
  bool listIncomplete = false;

  // Connected so _refresh actually fetches instead of early-returning.
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(sessions: sessions, complete: !listIncomplete);

  @override
  Future<void> cancel(String sessionId) async {}

  @override
  Future<void> deleteSession(String sessionId) async {
    deleteCalls.add(sessionId);
    // Host truth for the lost-ok case: the purge happened, the ok did not
    // arrive (MADR 0094 D7 / 0095 F4).
    if (!failDeleteKeepsRow) {
      sessions = sessions.where((s) => s.id != sessionId).toList();
    }
    if (failDelete) throw McException('timed out', code: 'timeout');
  }

  @override
  Future<List<ProviderInfo>> listProviders() async => providers;

  @override
  Future<PickerCatalog> listModels(
    String provider, {
    String? scope,
    String? modelProvider,
    String? sessionId,
  }) async {
    if (scope == 'providers') {
      return PickerCatalog(
        provider: provider,
        allowCustom: false,
        options: [PickerOption(id: 'models', label: 'Models')],
      );
    }
    return PickerCatalog(
      provider: provider,
      allowCustom: true,
      options: [?modelOption],
    );
  }

  @override
  Future<List<DeviceInfo>> listDevices() async => devices;

  @override
  Future<void> releaseSession(String sessionId, {String? toDeviceId}) async {
    releaseCalls.add((id: sessionId, to: toDeviceId));
  }

  @override
  Future<SessionMeta> claimSession(String sessionId) async {
    claimCalls.add(sessionId);
    return SessionMeta(id: sessionId, provider: 'grok', ownerDeviceId: 'me');
  }
}

Widget _wrapWith(ProviderContainer container) => UncontrolledProviderScope(
  container: container,
  child: const MaterialApp(home: SessionsScreen()),
);

Widget _wrap(MockMcremoteClient client, {ThemeData? theme}) {
  return ProviderScope(
    overrides: [
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      mcremoteClientProvider.overrideWithValue(client),
    ],
    child: MaterialApp(theme: theme, home: const SessionsScreen()),
  );
}

void main() {
  testWidgets(
    'SessionsScreen does not show bottom right FloatingActionButton',
    (tester) async {
      await tester.pumpWidget(_wrap(MockMcremoteClient()));

      await tester.pumpAndSettle();

      // The FloatingActionButton should not be found anywhere on the screen
      expect(find.byType(FloatingActionButton), findsNothing);

      // But the AppBar should be present
      expect(find.text('Sessions'), findsOneWidget);

      // Multi-device empty state (C.2): not "daemon empty", this-device owned.
      expect(find.text('No sessions on this device'), findsOneWidget);
      expect(find.textContaining('another phone'), findsOneWidget);
    },
  );

  testWidgets(
    'sessions list is live-first then newest closed, not host order',
    (tester) async {
      await tester.pumpWidget(
        _wrap(
          MockMcremoteClient(
            sessions: [
              SessionMeta(
                id: 'oldclosed',
                provider: 'kilo',
                name: 'old kilo',
                live: false,
                updatedAt: DateTime.utc(2026, 8, 11),
              ),
              SessionMeta(
                id: 'newclosed',
                provider: 'goose',
                name: 'new goose',
                live: false,
                updatedAt: DateTime.utc(2026, 8, 16, 6),
              ),
              SessionMeta(
                id: 'liveone',
                provider: 'grok',
                name: 'live grok',
                live: true,
                updatedAt: DateTime.utc(2026, 8, 1),
              ),
            ],
          ),
        ),
      );
      await tester.pumpAndSettle();

      final titles = find
          .byType(ListTile)
          .evaluate()
          .map((e) => ((e.widget as ListTile).title as Text).data)
          .whereType<String>()
          .toList();
      expect(titles.skipWhile((t) => !t.startsWith('live')).toList(), [
        'live grok',
        'new goose',
        'old kilo',
      ]);
    },
  );

  testWidgets('long session names truncate to a single ellipsised line', (
    tester,
  ) async {
    const longName =
        'An extremely long session name that would otherwise wrap the tile '
        'title across several lines and break row alignment';
    await tester.pumpWidget(
      _wrap(
        MockMcremoteClient(
          sessions: [
            SessionMeta(id: 'abcdef1234', provider: 'grok', name: longName),
          ],
        ),
      ),
    );

    await tester.pumpAndSettle();

    final title = tester.widget<Text>(find.text(longName));
    expect(title.maxLines, 1);
    expect(title.overflow, TextOverflow.ellipsis);

    // The connected banner derives its hostname in build (no host dialled in
    // this test, so the generic fallback).
    expect(find.text('Connected to host'), findsOneWidget);
  });

  testWidgets('sessions shares the celestial starfield with connect/chat', (
    tester,
  ) async {
    await tester.pumpWidget(_wrap(MockMcremoteClient()));
    await tester.pumpAndSettle();

    expect(find.byType(CelestialBackdrop), findsOneWidget);
  });

  testWidgets('connection banner host label tracks hostInputListenable', (
    tester,
  ) async {
    final client = MockMcremoteClient();
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();
    expect(find.text('Connected to host'), findsOneWidget);

    // Simulates a host note without a connection-state transition.
    client.hostInputListenable.value = '10.0.2.2:7531';
    await tester.pump();

    expect(find.text('Connected to 10.0.2.2'), findsOneWidget);
  });

  testWidgets('unhealthy sessions banner reuses ConnBanner with subtitle', (
    tester,
  ) async {
    final client = MockMcremoteClient();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.reconnecting),
          ),
          mcremoteClientProvider.overrideWithValue(client),
        ],
        child: const MaterialApp(home: SessionsScreen()),
      ),
    );
    // Linking spinner is unbounded; avoid pumpAndSettle.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.byType(ConnBanner), findsOneWidget);
    expect(
      find.text('Pairing stays active until you sign out.'),
      findsOneWidget,
    );
    // TextButton paints label text twice (button + semantics); assert presence.
    expect(find.text('Retry now'), findsWidgets);
  });

  // The new-session dialog mixes three different field widgets
  // (DropdownButtonFormField, TextField, InputDecorator). They only look like
  // one consistent form because each carries the same decoration; a stray
  // padding or a non-floating label silently desymmetrises one of them. These
  // assert the rendered geometry rather than the widget config, under the real
  // app theme, because that is what the eye actually judges.
  group('new-session dialog layout', () {
    const labels = <String>[
      'Available providers',
      'Friendly name',
      'Working directory',
      'Model (optional)',
    ];

    // The dialog reads recent cwds before it opens; without a backing store
    // that read throws and the dialog never appears.
    setUp(() => SharedPreferences.setMockInitialValues({}));

    Future<void> openDialog(WidgetTester tester) async {
      await tester.pumpWidget(
        _wrap(
          MockMcremoteClient(
            providers: [ProviderInfo(id: 'opencode', ready: true)],
          ),
          theme: celestialDark,
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'New session'));
      await tester.pumpAndSettle();
    }

    testWidgets('every field renders at the same height', (tester) async {
      await openDialog(tester);

      final heights = <String, double>{};
      for (final label in labels) {
        final field = find
            .ancestor(
              of: find.text(label),
              matching: find.byType(InputDecorator),
            )
            .first;
        heights[label] = tester.getSize(field).height;
      }

      expect(heights.values.toSet(), hasLength(1), reason: 'heights: $heights');
    });

    testWidgets('every field label starts on the same vertical line', (
      tester,
    ) async {
      await openDialog(tester);

      final lefts = <String, double>{};
      for (final label in labels) {
        lefts[label] = tester.getTopLeft(find.text(label)).dx;
      }

      expect(lefts.values.toSet(), hasLength(1), reason: 'lefts: $lefts');
    });

    testWidgets('the header aligns with the field labels', (tester) async {
      await openDialog(tester);

      final title = tester.getTopLeft(find.text('Begin new session')).dx;
      final firstLabel = tester.getTopLeft(find.text(labels.first)).dx;

      expect(title, firstLabel);
    });

    testWidgets('the gaps between fields are identical', (tester) async {
      await openDialog(tester);

      Rect box(String label) => tester.getRect(
        find
            .ancestor(
              of: find.text(label),
              matching: find.byType(InputDecorator),
            )
            .first,
      );

      final gaps = <double>[
        for (var i = 0; i < labels.length - 1; i++)
          box(labels[i + 1]).top - box(labels[i]).bottom,
      ];

      expect(gaps.toSet(), hasLength(1), reason: 'gaps: $gaps');
      expect(gaps.first, greaterThan(12), reason: 'gaps: $gaps');
    });

    testWidgets('a long model label and qualified id stay one line', (
      tester,
    ) async {
      const modelId =
          'models/a-very-long-qualified-model-identifier-for-layout-testing';
      const modelLabel =
          'A deliberately verbose human-readable model display label';
      await tester.pumpWidget(
        _wrap(
          MockMcremoteClient(
            providers: [ProviderInfo(id: 'opencode', ready: true)],
            modelOption: PickerOption(id: modelId, label: modelLabel),
          ),
          theme: celestialDark,
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'New session'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(DropdownButtonFormField<String>).first);
      await tester.pumpAndSettle();
      await tester.tap(find.text('opencode').last);
      await tester.pumpAndSettle();

      final before = tester.getSize(
        find
            .ancestor(
              of: find.text('Model (optional)'),
              matching: find.byType(InputDecorator),
            )
            .first,
      );
      final modelField = find
          .ancestor(
            of: find.text('Model (optional)'),
            matching: find.byType(InputDecorator),
          )
          .first;
      await tester.tap(
        find.descendant(of: modelField, matching: find.byType(InkWell)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text(modelLabel));
      await tester.pump();
      await tester.tap(find.widgetWithText(FilledButton, 'Select'));
      await tester.pumpAndSettle();

      final value = tester.widget<Text>(find.text('$modelLabel · $modelId'));
      final after = tester.getSize(
        find
            .ancestor(
              of: find.text('Model (optional)'),
              matching: find.byType(InputDecorator),
            )
            .first,
      );
      expect(value.maxLines, 1);
      expect(value.overflow, TextOverflow.ellipsis);
      expect(after.height, before.height);
    });
  });

  // MADR 0063 D1/D5 — the status light is a claim with an expiry, and it says
  // which transport it is talking about.
  testWidgets('a stale link is amber and says so, not green', (tester) async {
    final client = MockMcremoteClient(health: LinkHealth.stale);
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    // The regression: `connected` stayed true across a blackholed path, so the
    // light stayed green and the label kept naming a host the phone could no
    // longer reach.
    expect(find.textContaining('Checking connection'), findsOneWidget);
    expect(find.textContaining('Connected to'), findsNothing);
  });

  testWidgets('a fresh link is green and names the transport', (tester) async {
    final client = MockMcremoteClient();
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    expect(find.textContaining('Connected to'), findsOneWidget);
  });

  testWidgets('a lost link reports loss while the socket is still up', (
    tester,
  ) async {
    // `lost` with the state still `connected` is exactly the blackhole case:
    // nothing closed, but the peer stopped answering long ago.
    final client = MockMcremoteClient(health: LinkHealth.lost);
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    expect(find.textContaining('lost'), findsOneWidget);
    expect(find.textContaining('Connected to'), findsNothing);
  });

  // MADR 0078 handoff: a session I own offers Hand off; an unowned (released
  // or legacy) session is claimable but still fully operable, so the normal
  // actions are never hidden.
  testWidgets('an owned session menu offers Hand off, not Claim', (
    tester,
  ) async {
    final client = MockMcremoteClient(
      sessions: [
        SessionMeta(id: 'owned1234', provider: 'grok', ownerDeviceId: 'me'),
      ],
    )..deviceId = 'me';
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_vert).first);
    await tester.pumpAndSettle();

    expect(find.text('Hand off…'), findsOneWidget);
    expect(find.text('Claim'), findsNothing);
    // Normal actions remain.
    expect(find.text('End session'), findsOneWidget);
    expect(find.text('Rename'), findsOneWidget);
  });

  testWidgets('an unowned session is claimable but keeps normal actions', (
    tester,
  ) async {
    final client = MockMcremoteClient(
      // No owner => released/legacy => claimable, and still operable.
      sessions: [SessionMeta(id: 'freed1234', provider: 'grok')],
    )..deviceId = 'me';
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    // Labelled as claimable (not misleadingly "Released").
    expect(find.textContaining('Claimable'), findsOneWidget);

    await tester.tap(find.byIcon(Icons.more_vert).first);
    await tester.pumpAndSettle();

    expect(find.text('Claim'), findsOneWidget);
    expect(find.text('Hand off…'), findsNothing);
    // An empty-owner session is fully operable — normal actions stay.
    expect(find.text('Open'), findsOneWidget);
    expect(find.text('Rename'), findsOneWidget);
    expect(find.text('End session'), findsOneWidget);
  });

  testWidgets('a session released to this device is labelled for it', (
    tester,
  ) async {
    final client = MockMcremoteClient(
      sessions: [
        SessionMeta(id: 'freed1234', provider: 'grok', pendingHandoffTo: 'me'),
      ],
    )..deviceId = 'me';
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    expect(find.textContaining('Released to you · claimable'), findsOneWidget);
  });

  testWidgets('tapping Claim calls claimSession', (tester) async {
    final client = MockMcremoteClient(
      sessions: [SessionMeta(id: 'freed1234', provider: 'grok')],
    )..deviceId = 'me';
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_vert).first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Claim'));
    await tester.pumpAndSettle();

    expect(client.claimCalls, ['freed1234']);
  });

  testWidgets('Hand off to a device targets that device on release', (
    tester,
  ) async {
    final client =
        MockMcremoteClient(
            sessions: [
              SessionMeta(
                id: 'owned1234',
                provider: 'grok',
                ownerDeviceId: 'me',
              ),
            ],
          )
          ..deviceId = 'me'
          ..devices = const [
            DeviceInfo(deviceId: 'me', name: 'This Phone', isSelf: true),
            DeviceInfo(deviceId: 'dev-laptop', name: 'Laptop'),
          ];
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_vert).first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Hand off…'));
    await tester.pumpAndSettle();

    // The picker excludes this device; pick the laptop.
    expect(find.text('This Phone'), findsNothing);
    await tester.tap(find.text('Laptop'));
    await tester.pumpAndSettle();

    expect(client.releaseCalls.length, 1);
    expect(client.releaseCalls.first.id, 'owned1234');
    expect(client.releaseCalls.first.to, 'dev-laptop');
  });

  group('end-session classification (MADR 0095 D5)', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
      useFakePathProvider(addTearDown);
    });

    Future<ProviderContainer> pumpWith(
      WidgetTester tester,
      MockMcremoteClient client,
      String sessionId,
    ) async {
      final container = ProviderContainer(
        overrides: [
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          mcremoteClientProvider.overrideWithValue(client),
        ],
      );
      addTearDown(container.dispose);
      // Give the session a transcript so the cleanup is observable.
      // debugOnEvent flushes the batch window synchronously, so byId is
      // populated by the time the next line runs.
      container
          .read(transcriptsProvider.notifier)
          .debugOnEvent(
            SessionEvent(
              type: 'user_message',
              sessionId: sessionId,
              seq: 1,
              text: 'hi',
            ),
          );
      expect(
        container.read(transcriptsProvider).byId.containsKey(sessionId),
        isTrue,
      );
      await tester.pumpWidget(_wrapWith(container));
      await tester.pumpAndSettle();
      return container;
    }

    Future<void> endFirstRow(WidgetTester tester) async {
      await tester.tap(find.byIcon(Icons.more_vert).first);
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();
    }

    testWidgets('a delete whose ok was lost is treated as ended', (
      tester,
    ) async {
      final client = MockMcremoteClient(
        sessions: [SessionMeta(id: 's-lost', provider: 'kilo', name: 'Lost')],
      );
      client.failDelete = true;
      final container = await pumpWith(tester, client, 's-lost');

      await endFirstRow(tester);

      expect(client.deleteCalls, ['s-lost']);
      expect(find.textContaining('End failed'), findsNothing);
      await tester.pumpAndSettle();
      expect(
        container.read(transcriptsProvider).byId.containsKey('s-lost'),
        isFalse,
        reason: 'a confirmed purge must clear the local transcript',
      );
    });

    testWidgets('a delete that genuinely failed keeps the error', (
      tester,
    ) async {
      final client = MockMcremoteClient(
        sessions: [SessionMeta(id: 's-keep', provider: 'kilo', name: 'Keep')],
      );
      client.failDelete = true;
      client.failDeleteKeepsRow = true;
      final container = await pumpWith(tester, client, 's-keep');

      await endFirstRow(tester);

      expect(client.deleteCalls, ['s-keep']);
      expect(find.textContaining('End failed'), findsOneWidget);
      await tester.pumpAndSettle();
      expect(
        container.read(transcriptsProvider).byId.containsKey('s-keep'),
        isTrue,
        reason: 'an unconfirmed delete must not wipe local state',
      );
    });

    testWidgets(
      'a delete confirmed only by an incomplete list keeps the error',
      (tester) async {
        final client = MockMcremoteClient(
          sessions: [SessionMeta(id: 's-part', provider: 'kilo', name: 'Part')],
        );
        client.failDelete = true;
        client.listIncomplete = true;
        final container = await pumpWith(tester, client, 's-part');

        await endFirstRow(tester);

        // MADR 0095 D2: a partial list cannot confirm a purge.
        expect(find.textContaining('End failed'), findsOneWidget);
        await tester.pumpAndSettle();
        expect(
          container.read(transcriptsProvider).byId.containsKey('s-part'),
          isTrue,
        );
      },
    );
  });
}
