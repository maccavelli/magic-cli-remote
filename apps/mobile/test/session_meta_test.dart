import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/protocol/models.dart';

void main() {
  test('compareSessionsRecency is live-first then newest updated', () {
    final liveOld = SessionMeta(
      id: 'live-old',
      provider: 'grok',
      live: true,
      createdAt: DateTime.utc(2026, 8, 1),
      updatedAt: DateTime.utc(2026, 8, 1),
    );
    final closedNew = SessionMeta(
      id: 'goose',
      provider: 'goose',
      live: false,
      createdAt: DateTime.utc(2026, 8, 16, 4, 54),
      updatedAt: DateTime.utc(2026, 8, 16, 6, 23, 19),
    );
    final closedOld = SessionMeta(
      id: 'kilo-ghost',
      provider: 'kilo',
      live: false,
      createdAt: DateTime.utc(2026, 8, 11),
      updatedAt: DateTime.utc(2026, 8, 11),
    );
    final closedMid = SessionMeta(
      id: 'grok-closed',
      provider: 'grok',
      live: false,
      createdAt: DateTime.utc(2026, 8, 15, 22, 12),
      updatedAt: DateTime.utc(2026, 8, 16, 6, 23, 18),
    );
    final shuffled = [closedOld, closedNew, liveOld, closedMid]
      ..sort(compareSessionsRecency);
    expect(shuffled.map((s) => s.id).toList(), [
      'live-old',
      'goose',
      'grok-closed',
      'kilo-ghost',
    ]);
  });

  test('compareSessionsRecency falls back to createdAt then id', () {
    final a = SessionMeta(
      id: 'aaa',
      provider: 'grok',
      live: false,
      createdAt: DateTime.utc(2026, 8, 16),
    );
    final b = SessionMeta(
      id: 'bbb',
      provider: 'goose',
      live: false,
      createdAt: DateTime.utc(2026, 8, 16),
    );
    expect(compareSessionsRecency(a, b), greaterThan(0));
  });

  test('compareAgentSessionsRecency is newest updated first', () {
    final older = AgentSessionMeta(
      id: 'old',
      updatedAt: DateTime.utc(2026, 8, 11),
    );
    final newer = AgentSessionMeta(
      id: 'new',
      updatedAt: DateTime.utc(2026, 8, 16),
    );
    expect(compareAgentSessionsRecency(newer, older), lessThan(0));
    expect(([older, newer]..sort(compareAgentSessionsRecency)).first.id, 'new');
  });

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

  test('SessionDiagnostics parses only bounded aggregate fields', () {
    final d = SessionDiagnostics.fromJson(const {
      'branch': 'feature/parity',
      'default_branch': 'main',
      'vcs': {
        'added': 1,
        'modified': 2,
        'deleted': 0,
        'additions': 12,
        'deletions': 3,
      },
      'mcp': [
        {'name': 'gopls', 'state': 'connected'},
      ],
      'paths': ['must be ignored'],
    });
    expect(d.branch, 'feature/parity');
    expect(d.defaultBranch, 'main');
    expect(d.vcs?.modified, 2);
    expect(d.vcs?.additions, 12);
    expect(d.mcp.single.name, 'gopls');
    expect(d.mcp.single.state, 'connected');
  });
}
