import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';

// PLAN 0208 P7 step 1: a grouped select reaches the phone with every value and
// its group, and every option with its category (MADR 0208 D3).

/// A session_config option as the daemon sends it (internal/event's
/// ConfigOption): a grouped model select in a categorized agent's shape.
const _groupedModel = '''
{"id":"model","name":"Model","kind":"select","category":"model",
 "current_value":"anthropic/claude-y",
 "values":[
  {"id":"anthropic/claude-x","name":"Claude X","group":"Recent"},
  {"id":"anthropic/claude-x","name":"Claude X","group":"Anthropic"},
  {"id":"anthropic/claude-y","name":"Claude Y","group":"Anthropic"},
  {"id":"plain","name":"Plain"}]}
''';

ConfigOption _parse(String raw) =>
    ConfigOption.fromJson(jsonDecode(raw) as Map<String, dynamic>);

void main() {
  test('an option keeps its category, and each value its group', () {
    final o = _parse(_groupedModel);
    expect(o.category, 'model');
    expect(o.values.map((v) => v.group).toList(), [
      'Recent',
      'Anthropic',
      'Anthropic',
      '',
    ]);
  });

  test('a changed category or group is a change, not a no-op', () {
    final o = _parse(_groupedModel);
    SessionEvent config(ConfigOption opt) => SessionEvent(
      type: 'session_config',
      sessionId: 's1',
      configOptions: [opt],
    );
    final t = applySessionEvent(SessionTranscript(sessionId: 's1'), config(o));

    final recategorized = ConfigOption(
      id: o.id,
      name: o.name,
      kind: o.kind,
      category: 'thought_level',
      currentValue: o.currentValue,
      values: o.values,
    );
    expect(identical(applySessionEvent(t, config(recategorized)), t), isFalse);

    final regrouped = ConfigOption(
      id: o.id,
      name: o.name,
      kind: o.kind,
      category: o.category,
      currentValue: o.currentValue,
      values: [
        for (final v in o.values)
          ConfigOptionValue(id: v.id, name: v.name, group: 'Other'),
      ],
    );
    expect(identical(applySessionEvent(t, config(regrouped)), t), isFalse);
  });

  test('the settings picker lists each value under its group', () {
    final catalog = configValuesCatalog(_parse(_groupedModel));
    expect(catalog.options.map((p) => '${p.group}/${p.id}').toList(), [
      'Recent/anthropic/claude-x',
      'Anthropic/anthropic/claude-x',
      'Anthropic/anthropic/claude-y',
      '/plain',
    ]);
    expect(catalog.defaultIds, ['anthropic/claude-y']);
  });
}
