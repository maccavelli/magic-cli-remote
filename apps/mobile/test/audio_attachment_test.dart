import 'dart:async';
import 'dart:typed_data';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/features/chat/diagnostics_sheet.dart';
import 'package:magic_cli_remote/features/chat/workspace_sheet.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Audio composition (MADR 0112 A2).
///
/// The daemon advertises audio only when the *active* model accepts it, and
/// rejects an attachment whose media type, filename or size is out of bounds.
/// The composer must therefore gate on the capability and refuse locally, so a
/// user never spends a turn discovering the model could not read the file.

class _RecordingClient extends McremoteClient {
  final prompts = <List<PromptAttachment>>[];

  @override
  Future<SessionDiagnostics> sessionDiagnostics(String sessionId) async =>
      SessionDiagnostics(
        branch: 'main',
        skills: const [SkillInfo(name: 'customize-opencode')],
      );

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'opencode', cwd: '/w')],
        complete: true,
      );

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    prompts.add(attachments);
  }
}

/// An [XFile] backed by bytes, so no platform channel is involved.
class _FakeXFile extends XFile {
  _FakeXFile(this._bytes, String path, {String? mime})
    : super(path, mimeType: mime);

  final Uint8List _bytes;

  @override
  Future<Uint8List> readAsBytes() async => _bytes;
}

SessionTranscript idleWith({required bool image, required bool audio}) =>
    SessionTranscript(
      sessionId: 's1',
      status: 'idle',
      items: const [],
      nextSeq: 0,
      capabilities: SessionCapabilities(
        image: image,
        audio: audio,
        loadSession: false,
        embeddedContext: false,
        listSessions: false,
        closeSession: false,
        mcpHttp: false,
        mcpSse: false,
        mcpAcp: false,
      ),
    );

Future<ProviderContainer> pumpChat(
  WidgetTester tester,
  McremoteClient client, {
  required bool image,
  required bool audio,
}) async {
  tester.view.physicalSize = const Size(600, 1400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  final container = ProviderContainer(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider(
        's1',
      ).overrideWithValue(idleWith(image: image, audio: audio)),
    ],
  );
  addTearDown(container.dispose);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
    ),
  );
  await tester.pump();
  return container;
}

/// Pumps the chat screen with a session that advertises workspace inspection.
Future<void> pumpChatWithWorkspace(
  WidgetTester tester,
  McremoteClient client,
) async {
  tester.view.physicalSize = const Size(600, 1400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  const transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: [],
    nextSeq: 0,
    capabilities: SessionCapabilities(
      image: false,
      audio: false,
      workspaceRead: true,
      loadSession: false,
      embeddedContext: false,
      listSessions: false,
      closeSession: false,
      mcpHttp: false,
      mcpSse: false,
      mcpAcp: false,
    ),
  );
  final container = ProviderContainer(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWithValue(transcript),
    ],
  );
  addTearDown(container.dispose);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
    ),
  );
  await tester.pump();
}

void main() {
  tearDown(() => debugPickAudio = null);

  group('audioMimeForPath', () {
    test('maps every accepted extension', () {
      const cases = {
        '/a/b.mp3': 'audio/mpeg',
        '/a/b.wav': 'audio/wav',
        '/a/b.flac': 'audio/flac',
        '/a/b.aac': 'audio/aac',
        '/a/b.ogg': 'audio/ogg',
        '/a/b.oga': 'audio/ogg',
        '/a/b.opus': 'audio/opus',
        '/a/b.m4a': 'audio/mp4',
        '/a/b.mp4': 'audio/mp4',
        '/a/b.weba': 'audio/webm',
      };
      cases.forEach((path, want) {
        expect(audioMimeForPath(path), want, reason: path);
      });
    });

    test('is case-insensitive on the extension', () {
      expect(audioMimeForPath('/a/B.MP3'), 'audio/mpeg');
    });

    test('an unknown extension yields no type rather than a guess', () {
      expect(audioMimeForPath('/a/b.txt'), '');
      expect(audioMimeForPath('/a/b'), '');
      expect(audioMimeForPath(''), '');
    });

    test('a supplied accepted media type wins over the extension', () {
      expect(audioMimeForPath('/a/b.bin', 'audio/flac'), 'audio/flac');
    });

    test('a supplied type outside the allowlist is ignored', () {
      // image/png is not audio; the extension decides, and here there is none.
      expect(audioMimeForPath('/a/b.bin', 'image/png'), '');
      // A non-allowlisted audio container still falls back to the extension.
      expect(audioMimeForPath('/a/b.mp3', 'audio/basic'), 'audio/mpeg');
    });

    test('every allowlisted type is reachable from some extension', () {
      final reachable = <String>{
        for (final ext in [
          '.aac',
          '.flac',
          '.m4a',
          '.mp3',
          '.mp4',
          '.oga',
          '.ogg',
          '.opus',
          '.wav',
          '.weba',
        ])
          audioMimeForPath('/a/b$ext'),
      };
      expect(
        reachable.difference(kAcceptedAudioMimeTypes),
        isEmpty,
        reason:
            'the extension table must not produce a type the daemon rejects',
      );
    });
  });

  group('attachmentBasename', () {
    test('strips directories', () {
      expect(attachmentBasename('/tmp/deep/dir/clip.wav'), 'clip.wav');
      expect(attachmentBasename('clip.wav'), 'clip.wav');
    });

    test('strips a traversal attempt to its basename', () {
      expect(attachmentBasename('../../etc/passwd'), 'passwd');
    });

    test('refuses bare dot segments', () {
      expect(attachmentBasename('.'), '');
      expect(attachmentBasename('..'), '');
    });

    test('clamps to the daemon 256-byte bound, keeping the extension', () {
      final long = '${'a' * 400}.wav';
      final got = attachmentBasename(long);
      expect(got.length, lessThanOrEqualTo(256));
      expect(got.endsWith('.wav'), isTrue);
    });

    test('a name already at the bound is untouched', () {
      final exact = 'a' * 256;
      expect(attachmentBasename(exact), exact);
    });
  });

  group('PromptAttachment wire form', () {
    test('omits an empty filename so old daemons see the old shape', () {
      const a = PromptAttachment(
        kind: 'audio',
        mimeType: 'audio/wav',
        data: 'AA==',
      );
      expect(a.toJson().containsKey('filename'), isFalse);
    });

    test('carries a basename when present', () {
      const a = PromptAttachment(
        kind: 'audio',
        mimeType: 'audio/wav',
        data: 'AA==',
        filename: 'clip.wav',
      );
      expect(a.toJson()['filename'], 'clip.wav');
    });
  });

  testWidgets('the audio affordance follows the advertised capability', (
    tester,
  ) async {
    await pumpChat(tester, _RecordingClient(), image: true, audio: false);
    expect(
      find.byKey(const ValueKey('attach-audio')),
      findsNothing,
      reason: 'a model that does not accept audio must not offer the control',
    );

    await pumpChat(tester, _RecordingClient(), image: false, audio: true);
    expect(find.byKey(const ValueKey('attach-audio')), findsOneWidget);
  });

  testWidgets('a picked audio file is staged, previewed and removable', (
    tester,
  ) async {
    debugPickAudio = () async =>
        _FakeXFile(Uint8List.fromList(List.filled(2048, 7)), '/tmp/clip.wav');
    await pumpChat(tester, _RecordingClient(), image: false, audio: true);

    await tester.tap(find.byKey(const ValueKey('attach-audio')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('staged-audio')), findsOneWidget);
    expect(find.text('clip.wav'), findsOneWidget);
    expect(find.text('2 KB'), findsOneWidget);

    await tester.tap(find.byTooltip('Remove'));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('staged-audio')),
      findsNothing,
      reason: 'audio removes through the same flow as an image',
    );
  });

  testWidgets('an unsupported container is refused before staging', (
    tester,
  ) async {
    debugPickAudio = () async =>
        _FakeXFile(Uint8List.fromList([1, 2, 3]), '/tmp/clip.xyz');
    await pumpChat(tester, _RecordingClient(), image: false, audio: true);

    await tester.tap(find.byKey(const ValueKey('attach-audio')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('staged-audio')), findsNothing);
    expect(find.textContaining('not supported'), findsOneWidget);
  });

  testWidgets('an empty audio file is refused', (tester) async {
    debugPickAudio = () async => _FakeXFile(Uint8List(0), '/tmp/clip.wav');
    await pumpChat(tester, _RecordingClient(), image: false, audio: true);

    await tester.tap(find.byKey(const ValueKey('attach-audio')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('staged-audio')), findsNothing);
    expect(find.textContaining('empty'), findsOneWidget);
  });

  testWidgets('a cancelled pick stages nothing and reports nothing', (
    tester,
  ) async {
    debugPickAudio = () async => null;
    await pumpChat(tester, _RecordingClient(), image: false, audio: true);

    await tester.tap(find.byKey(const ValueKey('attach-audio')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('staged-audio')), findsNothing);
    expect(find.textContaining('Could not attach'), findsNothing);
  });

  testWidgets('an oversized audio file is refused against the frame budget', (
    tester,
  ) async {
    // Base64 inflates by 4/3, so ~900 KiB of raw audio already exceeds the
    // 1 MiB frame once encoded and wrapped in the prompt envelope.
    debugPickAudio = () async => _FakeXFile(
      Uint8List.fromList(List.filled(900 * 1024, 3)),
      '/tmp/big.wav',
    );
    await pumpChat(tester, _RecordingClient(), image: false, audio: true);

    await tester.tap(find.byKey(const ValueKey('attach-audio')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('staged-audio')), findsNothing);
    expect(find.textContaining('connection limit'), findsOneWidget);
  });

  testWidgets('a staged audio attachment reaches the prompt with its name', (
    tester,
  ) async {
    final client = _RecordingClient();
    debugPickAudio = () async =>
        _FakeXFile(Uint8List.fromList(List.filled(64, 9)), '/tmp/note.mp3');
    await pumpChat(tester, client, image: false, audio: true);

    await tester.tap(find.byKey(const ValueKey('attach-audio')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, 'transcribe this');
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('send')));
    await tester.pumpAndSettle();

    expect(client.prompts, hasLength(1));
    final sent = client.prompts.single.single;
    expect(sent.kind, 'audio');
    expect(sent.mimeType, 'audio/mpeg');
    expect(sent.filename, 'note.mp3');
    expect(sent.data, isNotEmpty);
  });

  group('chat screen gating', () {
    testWidgets('the workspace button follows the advertised capability', (
      tester,
    ) async {
      await pumpChat(tester, _RecordingClient(), image: false, audio: false);
      expect(
        find.byKey(const ValueKey('open-workspace')),
        findsNothing,
        reason: 'a provider without the surface would only ever refuse',
      );

      await pumpChatWithWorkspace(tester, _RecordingClient());
      expect(find.byKey(const ValueKey('open-workspace')), findsOneWidget);
    });

    testWidgets('tapping it opens the read-only viewer', (tester) async {
      await pumpChatWithWorkspace(tester, _RecordingClient());
      await tester.tap(find.byKey(const ValueKey('open-workspace')));
      await tester.pumpAndSettle();
      expect(find.byType(WorkspaceSheet), findsOneWidget);
    });

    testWidgets('the diagnostics button opens the sanitized report', (
      tester,
    ) async {
      await pumpChat(tester, _RecordingClient(), image: false, audio: false);
      expect(find.byKey(const ValueKey('open-diagnostics')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('open-diagnostics')));
      await tester.pumpAndSettle();
      expect(find.byType(DiagnosticsSheet), findsOneWidget);
    });

    testWidgets('authoring composes into the ordinary composer, not a send', (
      tester,
    ) async {
      final client = _RecordingClient();
      await pumpChat(tester, client, image: false, audio: false);
      await tester.tap(find.byKey(const ValueKey('open-diagnostics')));
      await tester.pumpAndSettle();

      // The affordance lives in the Skills section of the report.
      await tester.tap(find.byKey(const ValueKey('diagnostics-author-skill')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const ValueKey('skill-name')),
        'review-checklist',
      );
      await tester.enterText(
        find.byKey(const ValueKey('skill-description')),
        'Checks a diff',
      );
      await tester.tap(find.byKey(const ValueKey('skill-compose')));
      await tester.pumpAndSettle();

      // The prompt lands in the composer for the user to read and edit; it is
      // never sent on their behalf.
      expect(
        find.textContaining('.opencode/skills/review-checklist/SKILL.md'),
        findsOneWidget,
      );
      expect(client.prompts, isEmpty, reason: 'nothing may be sent yet');
    });
  });
}
