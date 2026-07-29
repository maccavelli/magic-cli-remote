import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';

/// Measured ladders from MADR 0052 §1 (codex 0.145.0 / grok 0.2.114).
List<ThinkingLevel> codexSixRung() => const [
  ThinkingLevel(id: 'low', description: 'Fast'),
  ThinkingLevel(id: 'medium', description: 'Balanced', isDefault: true),
  ThinkingLevel(id: 'high', description: 'Greater depth'),
  ThinkingLevel(id: 'xhigh', description: 'Extra high'),
  ThinkingLevel(id: 'max', description: 'Maximum'),
  ThinkingLevel(id: 'ultra', description: 'Ultra + delegation'),
];

List<ThinkingLevel> codexFourRung() => const [
  ThinkingLevel(id: 'low'),
  ThinkingLevel(id: 'medium', isDefault: true),
  ThinkingLevel(id: 'high'),
  ThinkingLevel(id: 'xhigh'),
];

List<ThinkingLevel> codexFiveRung() => const [
  ThinkingLevel(id: 'low'),
  ThinkingLevel(id: 'medium', isDefault: true),
  ThinkingLevel(id: 'high'),
  ThinkingLevel(id: 'xhigh'),
  ThinkingLevel(id: 'max'),
];

List<ThinkingLevel> grokThreeRung() => const [
  ThinkingLevel(id: 'low', label: 'Low Effort'),
  ThinkingLevel(id: 'medium', label: 'Medium Effort'),
  ThinkingLevel(id: 'high', label: 'High Effort', isDefault: true),
];

void main() {
  group('ThinkingLevel.fromJson', () {
    test('parses id, label, description, default', () {
      final l = ThinkingLevel.fromJson({
        'id': 'high',
        'label': 'High Effort',
        'description': 'Highest quality',
        'default': true,
      });
      expect(l.id, 'high');
      expect(l.label, 'High Effort');
      expect(l.description, 'Highest quality');
      expect(l.isDefault, isTrue);
      expect(l.displayLabel, 'High Effort');
    });

    test('tolerates missing fields', () {
      final l = ThinkingLevel.fromJson({});
      expect(l.id, '');
      expect(l.isDefault, isFalse);
      expect(l.displayLabel, '');
    });
  });

  group('PickerOption.thinkingLevels', () {
    test('parses thinking_levels array; drops empty ids', () {
      final o = PickerOption.fromJson({
        'id': 'gpt-5.6-terra',
        'thinking_levels': [
          {'id': 'low', 'description': 'Fast'},
          {'id': '', 'description': 'junk'},
          {'id': 'medium', 'default': true},
        ],
      });
      expect(o.thinkingLevels.map((e) => e.id).toList(), ['low', 'medium']);
      expect(o.thinkingLevels[1].isDefault, isTrue);
    });

    test('absent thinking_levels is empty', () {
      final o = PickerOption.fromJson({'id': 'x'});
      expect(o.thinkingLevels, isEmpty);
    });
  });

  group('resolveThinkingLevel', () {
    test('intent high on codex 6-rung → high, never ultra or max', () {
      final got = resolveThinkingLevel(codexSixRung(), 'high');
      expect(got, 'high');
      expect(got, isNot('ultra'));
      expect(got, isNot('max'));
      expect(got, isNot('xhigh'));
    });

    test('intent high on a ladder without high → model default', () {
      final ladder = const [
        ThinkingLevel(id: 'minimal'),
        ThinkingLevel(id: 'xhigh', isDefault: true),
      ];
      expect(resolveThinkingLevel(ladder, 'high'), 'xhigh');
    });

    test('intent null → null (send nothing)', () {
      expect(resolveThinkingLevel(codexSixRung(), null), isNull);
      expect(resolveThinkingLevel(codexSixRung(), ''), isNull);
      expect(resolveThinkingLevel(codexSixRung(), '  '), isNull);
    });

    test('empty ladder → null', () {
      expect(resolveThinkingLevel(const [], 'high'), isNull);
      expect(resolveThinkingLevel(const [], null), isNull);
    });

    test('exact match wins over model default', () {
      // grok defaults to high; intent low must still resolve to low.
      expect(resolveThinkingLevel(grokThreeRung(), 'low'), 'low');
      expect(resolveThinkingLevel(grokThreeRung(), 'high'), 'high');
    });

    test('every intent resolves to a member of the input list or null '
        '(property over measured ladders)', () {
      final ladders = [
        codexSixRung(),
        codexFiveRung(),
        codexFourRung(),
        grokThreeRung(),
      ];
      final intents = <String?>[
        null,
        '',
        'low',
        'medium',
        'high',
        'xhigh',
        'max',
        'ultra',
        'nope',
        'HIGH',
      ];
      for (final levels in ladders) {
        final ids = levels.map((l) => l.id).toSet();
        for (final intent in intents) {
          final got = resolveThinkingLevel(levels, intent);
          if (got != null) {
            expect(
              ids.contains(got),
              isTrue,
              reason:
                  'intent=$intent levels=$ids resolved to $got '
                  'which is not in the ladder',
            );
          }
        }
      }
    });

    test('global high intent never selects ultra on six-rung', () {
      // D3: the stored global intent vocabulary is only low/medium/high, and
      // exact-name resolution must not escalate.
      for (final intent in kThinkingLevelIntents) {
        final got = resolveThinkingLevel(codexSixRung(), intent);
        expect(got, intent);
        expect(got, isNot(anyOf('xhigh', 'max', 'ultra')));
      }
    });
  });

  group('SettingsStore defaultThinkingLevel', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('defaults to null (provider default)', () async {
      final store = SettingsStore(allowPlaintextFallback: true);
      expect(await store.getDefaultThinkingLevel(), isNull);
    });

    test('stores only low medium high; rejects exotic tiers', () async {
      final store = SettingsStore(allowPlaintextFallback: true);
      await store.setDefaultThinkingLevel('high');
      expect(await store.getDefaultThinkingLevel(), 'high');

      await store.setDefaultThinkingLevel('ultra');
      // Rejected: previous value preserved (or still high).
      expect(await store.getDefaultThinkingLevel(), 'high');

      await store.setDefaultThinkingLevel('xhigh');
      expect(await store.getDefaultThinkingLevel(), 'high');

      await store.setDefaultThinkingLevel(null);
      expect(await store.getDefaultThinkingLevel(), isNull);
    });

    test('clears with empty string', () async {
      final store = SettingsStore(allowPlaintextFallback: true);
      await store.setDefaultThinkingLevel('medium');
      await store.setDefaultThinkingLevel('');
      expect(await store.getDefaultThinkingLevel(), isNull);
    });
  });
}
