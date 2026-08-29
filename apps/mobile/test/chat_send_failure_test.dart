import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image_picker/image_picker.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Leaving the chat while a prompt is in flight, and having that prompt then
/// fail, is the reconnect window in miniature. The failure path has to release
/// the staged image bytes without touching `ref` — which throws once the
/// element is gone, in release builds too (MADR 0046 H-D).

class _HangingClient extends McremoteClient {
  final promptCalled = Completer<void>();
  final _result = Completer<void>();
  var prompts = 0;

  void failPrompt(Object error) => _result.completeError(error);

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'codex', cwd: '/home/mac')],
        complete: true,
      );

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) {
    prompts++;
    if (!promptCalled.isCompleted) promptCalled.complete();
    return _result.future;
  }
}

/// A real 1x1 PNG: the composer renders a thumbnail of whatever is staged, so
/// undecodable bytes would fail the test for reasons of their own.
final _pngBytes = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8'
  'z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
);

/// An [XFile] backed by bytes, so no platform channel is involved.
class _FakeXFile extends XFile {
  _FakeXFile(this._bytes) : super('/tmp/pic.png', mimeType: 'image/png');

  final Uint8List _bytes;

  @override
  Future<Uint8List> readAsBytes() async => _bytes;
}

SessionTranscript idleWithImageSupport() => const SessionTranscript(
  sessionId: 's1',
  status: 'idle',
  items: [],
  nextSeq: 0,
  capabilities: SessionCapabilities(
    image: true,
    audio: false,
    loadSession: false,
    embeddedContext: false,
    listSessions: false,
    closeSession: false,
    mcpHttp: false,
    mcpSse: false,
    mcpAcp: false,
  ),
);

void main() {
  tearDown(() => debugPickImage = null);

  testWidgets('a send that fails after leaving the chat unstages its images', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(600, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    final client = _HangingClient();
    debugPickImage = () async => _FakeXFile(_pngBytes);

    final container = ProviderContainer(
      overrides: [
        mcremoteClientProvider.overrideWithValue(client),
        connectionStateProvider.overrideWith(
          (ref) => Stream.value(McConnectionState.connected),
        ),
        sessionTranscriptProvider(
          's1',
        ).overrideWithValue(idleWithImageSupport()),
      ],
    );
    addTearDown(container.dispose);
    final notifier = container.read(transcriptsProvider.notifier);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pumpAndSettle();

    // Attach an image, type, and send.
    await tester.tap(find.byKey(const ValueKey('attach-image')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, 'look at this');
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(Icons.send));
    await tester.pump();

    await client.promptCalled.future;
    expect(client.prompts, 1);
    expect(notifier.debugStagedImageCount('s1'), 1);

    // Back out of the chat while the RPC is still outstanding, then fail it.
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: SizedBox.shrink()),
      ),
    );
    await tester.pumpAndSettle();

    client.failPrompt(McException('socket closed', code: 'connection_lost'));
    await tester.pumpAndSettle();

    // riverpod throws on `ref` use past unmount; whether the element has been
    // deactivated yet depends on timing, so the load-bearing assertion is the
    // one below. This guards the throw if it does surface.
    expect(
      tester.takeException(),
      isNull,
      reason: 'the failure path must not touch ref once unmounted',
    );
    expect(
      notifier.debugStagedImageCount('s1'),
      0,
      reason: 'stale staged bytes mis-attach to the next image message',
    );
  });
}
