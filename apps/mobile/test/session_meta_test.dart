import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/protocol/models.dart';

void main() {
  test('SessionMeta parses model from wire meta', () {
    final m = SessionMeta.fromJson(const {
      'id': 's1',
      'provider': 'opencode',
      'name': 'demo',
      'model': 'anthropic/claude-sonnet-4-5',
      'status': 'idle',
      'live': true,
    });
    expect(m.provider, 'opencode');
    expect(m.model, 'anthropic/claude-sonnet-4-5');
  });

  test('SessionMeta model defaults empty and survives copyWith', () {
    final m = SessionMeta.fromJson(const {'id': 's1', 'provider': 'grok'});
    expect(m.model, '');
    final withModel = SessionMeta(
      id: 's2',
      provider: 'opencode',
      model: 'opencode/big-pickle',
    );
    // copyWith must not drop the model (resume passes it back to the daemon).
    expect(withModel.copyWith(status: 'running').model, 'opencode/big-pickle');
  });

  test('SessionMeta tolerates owner_device_id and unknown wire fields', () {
    final m = SessionMeta.fromJson(const {
      'id': 's1',
      'provider': 'grok',
      'owner_device_id': 'dev-abc',
      'future_field': 42,
    });
    expect(m.id, 's1');
    expect(m.ownerDeviceId, 'dev-abc');
    expect(m.copyWith(status: 'idle').ownerDeviceId, 'dev-abc');
  });

  test('AgentSessionMeta parses metadata-only discovery entries', () {
    final m = AgentSessionMeta.fromJson(const {
      'id': '20260726_30',
      'cwd': '/work/project',
      'title': 'Resume this task',
      'updated_at': '2026-07-26T20:52:14Z',
      'untrusted_future_field': 'ignored',
    });
    expect(m.id, '20260726_30');
    expect(m.cwd, '/work/project');
    expect(m.displayName, 'Resume this task');
    expect(m.updatedAt, DateTime.parse('2026-07-26T20:52:14Z'));
  });
}
