import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

/// A route's text field outlives the future that opened it: the dialog or
/// sheet is still animating out, still focused, when `await showDialog`
/// resolves. Disposing a controller there races the IME's `clearComposing`
/// on it, which throws (MADR 0046 M-7 / M-8). Gesture, CJK and autocorrect
/// keyboards all hold a live composing range at submit time, so this is the
/// ordinary path, not an edge case.

class _RenameClient extends McremoteClient {
  _RenameClient(this._sessions);

  final List<SessionMeta> _sessions;
  String? renamedTo;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(sessions: _sessions, complete: true);

  @override
  Future<List<ProviderInfo>> listProviders() async => const [];

  @override
  Future<SessionMeta> renameSession(String sessionId, String name) async {
    renamedTo = name;
    return SessionMeta(id: sessionId, provider: 'codex', name: name);
  }
}

Widget _wrap(McremoteClient client) => ProviderScope(
  overrides: [
    connectionStateProvider.overrideWith(
      (ref) => Stream.value(McConnectionState.connected),
    ),
    mcremoteClientProvider.overrideWithValue(client),
  ],
  child: const MaterialApp(home: SessionsScreen()),
);

void main() {
  testWidgets('renaming with a live IME composition does not throw', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1000, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    final client = _RenameClient([
      SessionMeta(id: 'abcdef1234', provider: 'codex', name: 'old name'),
    ]);
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Session actions'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Rename'));
    await tester.pumpAndSettle();

    expect(find.text('Rename session'), findsOneWidget);

    // Type, then leave the IME mid-composition — what a gesture keyboard does
    // right up to the moment the user taps Save.
    await tester.enterText(find.byType(TextFormField), 'new name');
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: 'new name',
        selection: TextSelection.collapsed(offset: 8),
        composing: TextRange(start: 4, end: 8),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(client.renamedTo, 'new name');
  });
}
