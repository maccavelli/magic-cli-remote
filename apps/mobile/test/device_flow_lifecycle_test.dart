import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/settings/device_flow_sheet.dart';

const _flow = DeviceFlowInfo(
  flowId: 'flow-1',
  providerId: 'codex',
  verificationUri: 'https://example.test/device',
  userCode: 'CODE-1',
  // Zero disables the sheet's one-second countdown timer, which would
  // otherwise keep the tree from ever settling. Dismissal semantics are
  // independent of the countdown; the countdown has its own test.
  expiresIn: 0,
);

/// pumpSheet shows the sheet the way the app does, so dismissal paths behave
/// like production rather than like a bare widget.
Future<int Function()> pumpSheet(
  WidgetTester tester, {
  bool transactional = false,
  Stream<String>? updates,
  Future<String?>? result,
}) async {
  var cancels = 0;
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => ElevatedButton(
            onPressed: () => showModalBottomSheet<void>(
              context: context,
              isScrollControlled: true,
              builder: (_) => DeviceFlowSheet(
                flow: _flow,
                transactional: transactional,
                updates: updates,
                result: result,
                onCancel: () async => cancels++,
              ),
            ),
            child: const Text('open'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  // Fixed pumps rather than pumpAndSettle: the sheet renders an indeterminate
  // progress indicator in one state, which never settles.
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 400));
  return () => cancels;
}

void main() {
  // MADR 0074 F5: only the visible Cancel button ever told the daemon to stop.
  // Every other dismissal left a real CLI running against the live credential.
  group('every dismissal cancels exactly once', () {
    testWidgets('cancel button', (tester) async {
      final cancels = await pumpSheet(tester);
      await tester.tap(find.byKey(const Key('device-flow-dismiss')));
      await tester.pump(const Duration(milliseconds: 400));
      expect(cancels(), 1);
    });

    testWidgets('barrier tap', (tester) async {
      final cancels = await pumpSheet(tester);
      // Tap well outside the sheet.
      await tester.tapAt(const Offset(10, 10));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(cancels(), 1, reason: 'a barrier tap must cancel the flow');
    });

    testWidgets('swipe down', (tester) async {
      final cancels = await pumpSheet(tester);
      await tester.drag(
        find.byKey(const Key('device-flow-uri')),
        const Offset(0, 600),
      );
      await tester.pump(const Duration(milliseconds: 400));
      expect(cancels(), 1, reason: 'a swipe dismissal must cancel the flow');
    });

    testWidgets('route disposal', (tester) async {
      final cancels = await pumpSheet(tester);
      // A bare root forces the whole element tree down, including the modal
      // route, without any gesture. Reusing MaterialApp would let Flutter
      // update the existing element instead of disposing it.
      await tester.pumpWidget(const SizedBox());
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(cancels(), 1, reason: 'route disposal must cancel the flow');
    });

    testWidgets('cancel then dispose sends only one', (tester) async {
      final cancels = await pumpSheet(tester);
      await tester.tap(find.byKey(const Key('device-flow-dismiss')));
      await tester.pump(const Duration(milliseconds: 400));
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(milliseconds: 400));
      expect(cancels(), 1, reason: 'cancellation must be idempotent');
    });
  });

  testWidgets('a completed flow is not cancelled on dismissal', (tester) async {
    final cancels = await pumpSheet(tester, result: Future.value(null));
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const Key('device-flow-outcome')), findsOneWidget);
    await tester.tap(find.byKey(const Key('device-flow-dismiss')));
    await tester.pump(const Duration(milliseconds: 400));
    expect(cancels(), 0, reason: 'a finished flow has nothing to cancel');
  });

  testWidgets('transactional copy says the current sign-in survives', (
    tester,
  ) async {
    await pumpSheet(tester, transactional: true);
    expect(find.byKey(const Key('device-flow-safe-notice')), findsOneWidget);
  });

  testWidgets('legacy daemon shows no safety promise it cannot keep', (
    tester,
  ) async {
    await pumpSheet(tester);
    expect(find.byKey(const Key('device-flow-safe-notice')), findsNothing);
  });

  testWidgets('ready_to_activate is neither success nor failure', (
    tester,
  ) async {
    final updates = StreamController<String>.broadcast();
    addTearDown(updates.close);
    await pumpSheet(tester, updates: updates.stream);

    updates.add(deviceFlowReadyToActivate);
    await tester.pump(const Duration(milliseconds: 400));

    expect(
      find.byKey(const Key('device-flow-ready-to-activate')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('device-flow-outcome')),
      findsNothing,
      reason: 'a non-terminal state must not render as an outcome',
    );
    // The user must not be offered a second OAuth exchange.
    expect(find.text('Sign in again'), findsNothing);
  });

  testWidgets('a terminal result replaces the pending state', (tester) async {
    final updates = StreamController<String>.broadcast();
    addTearDown(updates.close);
    final result = Completer<String?>();
    await pumpSheet(tester, updates: updates.stream, result: result.future);

    updates.add(deviceFlowReadyToActivate);
    await tester.pump(const Duration(milliseconds: 400));
    expect(
      find.byKey(const Key('device-flow-ready-to-activate')),
      findsOneWidget,
    );

    result.complete(null);
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const Key('device-flow-outcome')), findsOneWidget);
    expect(
      find.byKey(const Key('device-flow-ready-to-activate')),
      findsNothing,
    );
  });
}
