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
}
