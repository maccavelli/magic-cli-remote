import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

/// MADR 0123 D7/C2. The daemon reports when a thinking-level change takes
/// effect; the app must never infer it from a provider name again.
void main() {
  group('ThinkingMutability decoding', () {
    test('an absent field is unknown and settable, never fixed', () {
      final meta = SessionMeta.fromJson({
        'id': 's1',
        'provider': 'grok',
        'thinking_level': 'medium',
      });

      // The whole point of the type. An older daemon omits the field, and
      // reading that as "fixed" would put a false banner in front of the user
      // — the defect MADR 0123 F5 recorded.
      expect(meta.thinkingMutability, ThinkingMutability.unknown);
      expect(meta.thinkingMutability.settable, isTrue);
      expect(meta.thinkingMutability.needsBanner, isFalse);
    });

    test('each wire value decodes to its state', () {
      for (final (wire, want) in [
        ('live', ThinkingMutability.live),
        ('next_turn', ThinkingMutability.nextTurn),
        ('fixed', ThinkingMutability.fixed),
      ]) {
        final meta = SessionMeta.fromJson({
          'id': 's1',
          'provider': 'codex',
          'thinking_mutability': wire,
        });
        expect(meta.thinkingMutability, want, reason: 'wire value "$wire"');
      }
    });

    test('an unrecognised value decodes to unknown, not fixed', () {
      // A newer daemon may name a state this build has never heard of.
      // Guessing "fixed" would withhold a control that probably works.
      final meta = SessionMeta.fromJson({
        'id': 's1',
        'provider': 'codex',
        'thinking_mutability': 'some_future_state',
      });
      expect(meta.thinkingMutability, ThinkingMutability.unknown);
      expect(meta.thinkingMutability.settable, isTrue);
    });

    test('only fixed withholds the control', () {
      expect(ThinkingMutability.unknown.settable, isTrue);
      expect(ThinkingMutability.live.settable, isTrue);
      expect(ThinkingMutability.nextTurn.settable, isTrue);
      expect(ThinkingMutability.fixed.settable, isFalse);
    });

    test('live needs no banner; next_turn and fixed do', () {
      expect(ThinkingMutability.live.needsBanner, isFalse);
      expect(ThinkingMutability.unknown.needsBanner, isFalse);
      expect(ThinkingMutability.nextTurn.needsBanner, isTrue);
      expect(ThinkingMutability.fixed.needsBanner, isTrue);
    });
  });

  test('copyWith carries mutability through a level change', () {
    // copyWith runs when the user picks a level. Dropping the field here
    // would blank the card's banner at the moment the user is reading it.
    final meta = SessionMeta.fromJson({
      'id': 's1',
      'provider': 'codex',
      'thinking_level': 'low',
      'thinking_mutability': 'next_turn',
    });

    final after = meta.copyWith(thinkingLevel: 'high');

    expect(after.thinkingLevel, 'high');
    expect(after.thinkingMutability, ThinkingMutability.nextTurn);
  });
}
