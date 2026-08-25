import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

void main() {
  test('Codex runtime snapshot parses bounded status fields', () {
    final runtime = CodexRuntimeSnapshot.fromJson({
      'codex_version': '0.149.1',
      'transport': 'unix_ws',
      'generation': 7,
      'account': {'kind': 'chatgpt', 'plan': 'pro'},
      'rate_limits': {
        'primary': {'used_percent': 73, 'resets_at': 2000000000},
      },
      'usage': {'tokens': 1234, 'context_window': 128000},
      'model': {
        'id': 'gpt-5.6-sol',
        'reasoning_efforts': ['low', 'high'],
      },
      'mcp_servers': [
        {'name': 'tools', 'status': 'ready'},
      ],
    });
    expect(runtime.codexVersion, '0.149.1');
    expect(runtime.accountPlan, 'pro');
    expect(runtime.primaryRateUsedPercent, 73);
    expect(runtime.mcpServers.single.name, 'tools');
  });

  test('Codex activity cards upsert and resolve by stable key', () {
    var transcript = const SessionTranscript(sessionId: 's1');
    transcript = applySessionEvent(
      transcript,
      SessionEvent(
        type: 'codex_progress',
        sessionId: 's1',
        codex: const CodexEventPayload(
          key: 'review:r1',
          kind: 'guardian_review',
          status: 'running',
          title: 'Guardian review',
          text: 'Reviewing command',
        ),
      ),
    );
    transcript = applySessionEvent(
      transcript,
      SessionEvent(
        type: 'codex_progress',
        sessionId: 's1',
        codex: const CodexEventPayload(
          key: 'review:r1',
          kind: 'guardian_review',
          status: 'completed',
          title: 'Guardian review',
          text: 'approved',
          resolved: true,
        ),
      ),
    );
    expect(transcript.items, hasLength(1));
    expect(transcript.items.single.text, contains('approved'));
    expect(transcript.items.single.dedupeKey, 'review:r1');
  });

  for (final type in const [
    'codex_warning',
    'codex_model_reroute',
    'codex_model_verification',
    'codex_terminal_interaction',
  ]) {
    test('$type renders a readable card', () {
      final transcript = applySessionEvent(
        const SessionTranscript(sessionId: 's1'),
        SessionEvent(
          type: type,
          sessionId: 's1',
          codex: CodexEventPayload(
            key: '$type:1',
            kind: type,
            status: 'completed',
            title: 'Codex activity',
            text: 'Readable detail',
          ),
        ),
      );
      expect(transcript.items.single.text, contains('Readable detail'));
    });
  }

  test('Codex surface capability parses exact version and limits', () {
    final caps = ServerCaps.tryParse({
      'protocol': 2,
      'codex_surface': {
        'version': 1,
        'operations': ['rpc:account/read'],
        'experimental': ['rpc:server/diagnostics'],
        'max_page_size': 100,
        'max_text_bytes': 262144,
        'max_binary_chunk_bytes': 262144,
      },
    });
    expect(caps!.codexSurface!.version, 1);
    expect(caps.codexSurface!.maxPageSize, 100);
  });
}
