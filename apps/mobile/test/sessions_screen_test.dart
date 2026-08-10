import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/theme/celestial.dart';
import 'package:magic_cli_remote/theme/starfield.dart';
import 'package:magic_cli_remote/theme/widgets.dart';

class MockMcremoteClient extends McremoteClient {
  MockMcremoteClient({
    this.sessions = const <SessionMeta>[],
    this.providers = const <ProviderInfo>[],
    LinkHealth health = LinkHealth.fresh,
  }) {
    // A fake that reports `connected` must also say the link is answering:
    // since MADR 0063 the two are separate claims, and green requires both.
    // Overriding `state` alone leaves the freshness clock at its initial
    // `lost`, which is correct for a client that has never seen a frame.
    linkHealth.value = health;
  }

  final List<SessionMeta> sessions;
  final List<ProviderInfo> providers;

  // Handoff spies (MADR 0078).
  List<DeviceInfo> devices = const [];
  final List<({String id, String? to})> releaseCalls = [];
  final List<String> claimCalls = [];

  // Connected so _refresh actually fetches instead of early-returning.
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(sessions: sessions, complete: true);

  @override
  Future<List<ProviderInfo>> listProviders() async => providers;

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
      'Select model (optional)',
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
}
