import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

/// `session.list` answers with a snapshot taken before it is delivered, so any
/// event that lands while it is in flight describes a later moment. Applying
/// the snapshot wholesale put the row back to "Working…" after the agent had
/// already finished — the exact symptom the live listener exists to fix
/// (MADR 0046 L-10).

class _SlowListClient extends McremoteClient {
  _SlowListClient(this._sessions);

  final List<SessionMeta> _sessions;
  final _events = StreamController<SessionEvent>.broadcast();

  void emit(SessionEvent ev) => _events.add(ev);
  Future<void> close() => _events.close();
  Completer<void>? gate;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Stream<SessionEvent> get events => _events.stream;

  @override
  Future<List<SessionMeta>> listSessions() async {
    final g = gate;
    if (g != null) await g.future;
    return _sessions;
  }

  @override
  Future<List<ProviderInfo>> listProviders() async => const [];
}

void main() {
  testWidgets('a turn_complete during a refresh is not undone by it', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1000, 2200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    final client = _SlowListClient([
      SessionMeta(
        id: 'abcdef1234',
        provider: 'codex',
        name: 'build',
        status: 'running',
        live: true,
      ),
    ]);
    addTearDown(client.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          mcremoteClientProvider.overrideWithValue(client),
        ],
        child: const MaterialApp(home: SessionsScreen()),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('WORKING…'), findsOneWidget);

    // Hold the next refresh open, then finish the turn while it is in flight.
    client.gate = Completer<void>();
    await tester.fling(find.byType(ListView), const Offset(0, 600), 1500);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    client.emit(SessionEvent(type: 'turn_complete', sessionId: 'abcdef1234'));
    await tester.pump();

    client.gate!.complete();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));

    expect(find.text('WORKING…'), findsNothing);
    expect(find.text('IDLE'), findsOneWidget);
  });
}
