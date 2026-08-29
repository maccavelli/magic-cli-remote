import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';
import 'package:magic_cli_remote/features/chat/session_controls/ui_icons.dart';

/// Records setMode calls so a test can prove the confirmation gate actually
/// blocks the switch rather than merely showing a dialog.
class _ModeClient extends McremoteClient {
  final List<String> modeSwitches = [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [
          SessionMeta(id: 's1', provider: 'opencode', cwd: '/home/mac'),
        ],
        complete: true,
      );

  @override
  Future<void> setMode(String sessionId, String modeId) async {
    modeSwitches.add(modeId);
  }
}

/// OpenCode's advertised list: build/plan plus the daemon-flagged auto mode.
const _opencodeModes = [
  SessionMode(id: 'build', name: 'build'),
  SessionMode(id: 'plan', name: 'plan'),
  SessionMode(
    id: 'auto',
    name: 'auto',
    description:
        'Auto-approve — permissions answered automatically (dangerous)',
    dangerous: true,
  ),
];

/// A *pre-0069* goose daemon's list: `auto` was the default and carried no
/// flag. Kept as the legacy-daemon compat fixture — the phone must keep
/// rendering it plainly rather than inventing danger the daemon never
/// declared.
const _gooseModes = [
  SessionMode(
    id: 'auto',
    name: 'Auto',
    description: 'Automatically approve tool calls',
  ),
  SessionMode(id: 'approve', name: 'Approve'),
  SessionMode(id: 'smart_approve', name: 'Smart Approve'),
  SessionMode(id: 'chat', name: 'Chat'),
];

/// Goose after MADR 0069 D3: `auto` is flagged dangerous and `approve` is
/// the default — the bypass mode takes the 0049 confirmation like every
/// other provider's.
const _gooseModes0069 = [
  SessionMode(
    id: 'auto',
    name: 'Auto',
    description:
        'Automatically approve every tool call — no confirmation, no sandbox',
    dangerous: true,
  ),
  SessionMode(id: 'approve', name: 'Approve'),
  SessionMode(id: 'smart_approve', name: 'Smart Approve'),
  SessionMode(id: 'chat', name: 'Chat'),
];

/// Codex menu after MADR 0047: default first, then read-only, then dangerous auto.
const _codexModes = [
  SessionMode(id: 'default', name: 'default'),
  SessionMode(id: 'read-only', name: 'read-only'),
  SessionMode(
    id: 'auto',
    name: 'auto',
    description: 'Auto-approve — no prompts; edits confined to the workspace',
    dangerous: true,
  ),
];

/// Finds a bundled Lucide glyph by name. The icons are SVG assets now, so
/// `find.byIcon` no longer reaches them (MADR 0123 D15).
Finder findGlyph(String name) => find.byWidgetPredicate(
  (w) => w is UiIcon && w.name == name,
  description: 'UiIcon($name)',
);

Widget _host(
  _ModeClient client, {
  required List<SessionMode> modes,
  required String currentModeId,
}) {
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: const [],
    modes: modes,
    currentModeId: currentModeId,
  );
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWithValue(transcript),
    ],
    child: MaterialApp(home: ChatScreen(sessionId: 's1')),
  );
}

void main() {
  group('dangerous mode icon', () {
    testWidgets('an armed dangerous mode is visually alarming', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _opencodeModes, currentModeId: 'auto'),
      );
      await tester.pumpAndSettle();

      expect(
        findGlyph(UiIcons.shieldAuto),
        findsOneWidget,
        reason: 'an armed auto-approve mode must be unmissable',
      );
    });

    testWidgets('a normal mode is not styled as dangerous', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      expect(findGlyph(UiIcons.shieldAuto), findsNothing);
      expect(findGlyph(UiIcons.shieldFullAccess), findsNothing);
    });

    // Empty current_mode_id must not paint modes.first (read-only) when a
    // default mode exists (MADR 0047 D4 / create-time first-item bug).
    testWidgets('empty currentModeId shows codex default not read-only', (
      tester,
    ) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _codexModes, currentModeId: ''),
      );
      await tester.pumpAndSettle();

      // Not dangerous, so the glyph must not alarm.
      expect(findGlyph(UiIcons.shieldAuto), findsNothing);

      await tester.tap(find.byKey(const ValueKey('permissions')));
      await tester.pumpAndSettle();

      // Every mode is offered...
      expect(
        find.byKey(const ValueKey('session-control-option-default')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('session-control-option-read-only')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('session-control-option-auto')),
        findsOneWidget,
      );
      // ...and exactly one is marked current: `default`, not the first-list
      // lie of read-only.
      expect(findGlyph(UiIcons.selected), findsOneWidget);
      final checkedRow = find.ancestor(
        of: findGlyph(UiIcons.selected),
        matching: find.byKey(const ValueKey('session-control-option-default')),
      );
      expect(checkedRow, findsOneWidget);
    });

    // Legacy-daemon compat: a pre-0069 goose sends no flag, and the phone
    // must not invent danger the daemon never declared.
    testWidgets('a goose session in auto renders no alarm', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _gooseModes, currentModeId: 'auto'),
      );
      await tester.pumpAndSettle();

      expect(
        findGlyph(UiIcons.shieldAuto),
        findsNothing,
        reason:
            'a legacy daemon advertised no flag; alarming on it would '
            'invent danger the daemon never declared',
      );
    });

    // MADR 0069 D3 (U3): the current daemon flags goose auto, and the
    // generic machinery must alarm on it with zero goose-specific code.
    testWidgets('a 0069 goose session in auto alarms like any dangerous '
        'mode', (tester) async {
      await tester.pumpWidget(
        _host(_ModeClient(), modes: _gooseModes0069, currentModeId: 'auto'),
      );
      await tester.pumpAndSettle();

      expect(findGlyph(UiIcons.shieldAuto), findsOneWidget);
    });
  });

  group('arming confirmation', () {
    // The permissions control moved off the app bar to an icon under the
    // composer, opening a card (MADR 0123 D1/D5). What it must prove is
    // unchanged: arming a mode that answers permissions for the user takes a
    // confirmation, and dismissing it never arms.
    Future<void> openMenu(WidgetTester tester, String _) async {
      await tester.tap(find.byKey(const ValueKey('permissions')));
      await tester.pumpAndSettle();
    }

    testWidgets('selecting a dangerous mode asks first', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsOneWidget);
      expect(
        client.modeSwitches,
        isEmpty,
        reason: 'the switch must not happen until the user confirms',
      );
    });

    testWidgets('cancelling does not switch', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(client.modeSwitches, isEmpty);
    });

    testWidgets('confirming switches', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Turn on'));
      await tester.pumpAndSettle();

      expect(client.modeSwitches, ['auto']);
    });

    testWidgets('a normal mode switches without a dialog', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _opencodeModes, currentModeId: 'build'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'build');
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-plan')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsNothing);
      expect(client.modeSwitches, ['plan']);
    });

    // Legacy-daemon compat: a pre-0069 goose sends no flag, so switching
    // to its (then-default) auto stays one tap.
    testWidgets('a goose auto switch is not gated', (tester) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _gooseModes, currentModeId: 'chat'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'Chat');
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsNothing);
      expect(client.modeSwitches, ['auto']);
    });

    // MADR 0069 D3 (U3): with the flag advertised, goose auto takes the
    // 0049 confirmation — and confirming still switches.
    testWidgets('a 0069 goose auto switch is gated and confirmable', (
      tester,
    ) async {
      final client = _ModeClient();
      await tester.pumpWidget(
        _host(client, modes: _gooseModes0069, currentModeId: 'approve'),
      );
      await tester.pumpAndSettle();

      await openMenu(tester, 'Approve');
      await tester.tap(
        find.byKey(const ValueKey('session-control-option-auto')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Run without approvals?'), findsOneWidget);
      expect(client.modeSwitches, isEmpty, reason: 'not before consent');

      await tester.tap(find.text('Turn on'));
      await tester.pumpAndSettle();
      expect(client.modeSwitches, ['auto']);
    });
  });
}
