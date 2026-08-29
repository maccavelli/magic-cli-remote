import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/chat/session_controls/session_control_card.dart';
import 'package:magic_cli_remote/features/chat/session_controls/session_control_cards.dart';

/// MADR 0123 D5/D6/D8. One card idiom for every session control, the dangerous
/// confirmation preserved, and the thinking card explaining a limit up front
/// instead of after a tap that failed.

const _codexModes = [
  SessionMode(id: 'default', name: 'default', description: 'Edit workspace'),
  SessionMode(id: 'read-only', name: 'read-only', description: 'Read files'),
  SessionMode(
    id: 'auto',
    name: 'auto',
    description: 'Auto-approve — no prompts',
    dangerous: true,
  ),
];

const _levels = [
  ThinkingLevel(id: 'low'),
  ThinkingLevel(id: 'medium'),
  ThinkingLevel(id: 'high', description: 'Slowest, most thorough'),
];

Widget _host(void Function(BuildContext) open) {
  return MaterialApp(
    home: Scaffold(
      body: Builder(
        builder: (context) => ElevatedButton(
          onPressed: () => open(context),
          child: const Text('open'),
        ),
      ),
    ),
  );
}

Future<void> _open(WidgetTester tester) async {
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

void main() {
  group('permissions card', () {
    testWidgets('shows every mode and marks the current one', (tester) async {
      await tester.pumpWidget(
        _host(
          (c) => showPermissionsCard(
            c,
            modes: _codexModes,
            currentModeId: 'read-only',
            onSelect: (_) async {},
          ),
        ),
      );
      await _open(tester);

      expect(find.text('Permissions'), findsOneWidget);
      for (final m in _codexModes) {
        expect(
          find.byKey(ValueKey('session-control-option-${m.id}')),
          findsOneWidget,
        );
      }
      // The selection is resolved, never taken from list order (MADR 0047 D4).
      expect(find.byIcon(Icons.radio_button_checked), findsOneWidget);
    });

    testWidgets('a dangerous mode still confirms before arming', (
      tester,
    ) async {
      final applied = <String>[];
      await tester.pumpWidget(
        _host(
          (c) => showPermissionsCard(
            c,
            modes: _codexModes,
            currentModeId: 'default',
            onSelect: (id) async => applied.add(id),
          ),
        ),
      );
      await _open(tester);

      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsOneWidget);
      expect(applied, isEmpty, reason: 'nothing applies before confirmation');

      // Dismissing must not arm: the default is always "do not arm".
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(applied, isEmpty);
    });

    testWidgets('confirming a dangerous mode applies it', (tester) async {
      final applied = <String>[];
      await tester.pumpWidget(
        _host(
          (c) => showPermissionsCard(
            c,
            modes: _codexModes,
            currentModeId: 'default',
            onSelect: (id) async => applied.add(id),
          ),
        ),
      );
      await _open(tester);
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Turn on'));
      await tester.pumpAndSettle();

      expect(applied, ['auto']);
    });

    testWidgets('a safe mode applies with no confirmation', (tester) async {
      final applied = <String>[];
      await tester.pumpWidget(
        _host(
          (c) => showPermissionsCard(
            c,
            modes: _codexModes,
            currentModeId: 'default',
            onSelect: (id) async => applied.add(id),
          ),
        ),
      );
      await _open(tester);
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-read-only')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsNothing);
      expect(applied, ['read-only']);
    });
  });

  group('collaboration card', () {
    testWidgets('is a separate card with its own title', (tester) async {
      // MADR 0123 F2: permissions and collaboration are different questions.
      // Two cards that name themselves is the fix for two chips that did not.
      final applied = <String>[];
      await tester.pumpWidget(
        _host(
          (c) => showCollaborationCard(
            c,
            modes: const [
              CollaborationMode(id: 'default', name: 'Default'),
              CollaborationMode(id: 'plan', name: 'Plan'),
            ],
            currentModeId: 'plan',
            onSelect: (id) async => applied.add(id),
          ),
        ),
      );
      await _open(tester);

      expect(find.text('Collaboration'), findsOneWidget);
      expect(find.text('Permissions'), findsNothing);

      await tester.tap(
        find.byKey(const ValueKey('session-control-option-default')),
      );
      await tester.pumpAndSettle();
      expect(applied, ['default']);
    });
  });

  group('thinking card', () {
    testWidgets('live shows the ladder with no banner', (tester) async {
      await tester.pumpWidget(
        _host(
          (c) => showThinkingCard(
            c,
            levels: _levels,
            currentLevel: 'medium',
            mutability: ThinkingMutability.live,
            onSelect: (_) async {},
          ),
        ),
      );
      await _open(tester);

      expect(find.text('Thinking'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('session-control-banner')),
        findsNothing,
      );
      expect(find.byIcon(Icons.radio_button_checked), findsOneWidget);
    });

    testWidgets('unknown shows no banner either', (tester) async {
      // An older daemon omits the capability. Inventing an explanation would
      // be a guess presented as fact (MADR 0123 C2).
      await tester.pumpWidget(
        _host(
          (c) => showThinkingCard(
            c,
            levels: _levels,
            currentLevel: 'low',
            mutability: ThinkingMutability.unknown,
            onSelect: (_) async {},
          ),
        ),
      );
      await _open(tester);
      expect(
        find.byKey(const ValueKey('session-control-banner')),
        findsNothing,
      );
    });

    testWidgets('next_turn explains the delay and still allows selection', (
      tester,
    ) async {
      final applied = <String>[];
      await tester.pumpWidget(
        _host(
          (c) => showThinkingCard(
            c,
            levels: _levels,
            currentLevel: 'low',
            mutability: ThinkingMutability.nextTurn,
            onSelect: (id) async => applied.add(id),
          ),
        ),
      );
      await _open(tester);

      expect(
        find.byKey(const ValueKey('session-control-banner')),
        findsOneWidget,
      );
      expect(find.textContaining('next message'), findsOneWidget);

      await tester.tap(
        find.byKey(const ValueKey('session-control-option-high')),
      );
      await tester.pumpAndSettle();
      expect(applied, ['high'], reason: 'next_turn is a delay, not a lock');
    });

    testWidgets('fixed shows the ladder, disabled, under a banner', (
      tester,
    ) async {
      // D8: the card states the limitation. The user still learns which rungs
      // exist — a locked control is not a reason to hide the information.
      final applied = <String>[];
      await tester.pumpWidget(
        _host(
          (c) => showThinkingCard(
            c,
            levels: _levels,
            currentLevel: 'low',
            mutability: ThinkingMutability.fixed,
            onSelect: (id) async => applied.add(id),
          ),
        ),
      );
      await _open(tester);

      expect(
        find.byKey(const ValueKey('session-control-banner')),
        findsOneWidget,
      );
      expect(find.textContaining('Start a new session'), findsOneWidget);
      for (final l in _levels) {
        expect(
          find.byKey(ValueKey('session-control-option-${l.id}')),
          findsOneWidget,
        );
      }

      await tester.tap(
        find.byKey(const ValueKey('session-control-option-high')),
      );
      await tester.pumpAndSettle();
      expect(applied, isEmpty, reason: 'a locked ladder must not apply');
    });
  });

  group('shared card surface', () {
    testWidgets('renders a banner and options through one layout', (
      tester,
    ) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SessionControlCard(
              title: 'Surface',
              banner: const SessionControlBanner(message: 'hello'),
              options: [
                SessionControlOption(
                  id: 'a',
                  label: 'Alpha',
                  description: 'first',
                  selected: true,
                  onSelected: () async {},
                ),
                const SessionControlOption(id: 'b', label: 'Beta'),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Surface'), findsOneWidget);
      expect(find.text('hello'), findsOneWidget);
      expect(find.text('first'), findsOneWidget);
      // A row with no callback is inert rather than absent.
      expect(
        find.byKey(const ValueKey('session-control-option-b')),
        findsOneWidget,
      );
    });

    testWidgets('a dangerous row is marked on the row itself', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SessionControlCard(
              title: 'Surface',
              options: [
                SessionControlOption(
                  id: 'auto',
                  label: 'auto',
                  dangerous: true,
                  onSelected: () async {},
                ),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byIcon(Icons.bolt), findsOneWidget);
    });

    testWidgets('close dismisses the card', (tester) async {
      await tester.pumpWidget(
        _host(
          (c) => showThinkingCard(
            c,
            levels: _levels,
            currentLevel: 'low',
            mutability: ThinkingMutability.live,
            onSelect: (_) async {},
          ),
        ),
      );
      await _open(tester);
      expect(find.text('Thinking'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('model-picker-close')));
      await tester.pumpAndSettle();
      expect(find.text('Thinking'), findsNothing);
    });
  });

  test('PickerSource.unknown contributes no chip label', () {
    // The cards rely on this to reuse PickerSheetHeader without a provenance
    // chip that would mean nothing here.
    expect(PickerSource.unknown.label, isEmpty);
  });
}
