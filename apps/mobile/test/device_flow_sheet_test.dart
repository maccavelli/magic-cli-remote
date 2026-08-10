import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/settings/device_flow_sheet.dart';

const kiloFlow = DeviceFlowInfo(
  flowId: 'f-1',
  providerId: 'kilo',
  verificationUri: 'https://app.kilo.ai/device-auth',
  userCode: 'RX2Y-4H7X',
  expiresIn: 120,
);

Future<void> pumpFlow(
  WidgetTester tester, {
  DeviceFlowInfo flow = kiloFlow,
  Future<String?>? result,
  Future<void> Function()? onCancel,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: DeviceFlowSheet(
          flow: flow,
          result: result,
          onCancel: onCancel ?? () async {},
        ),
      ),
    ),
  );
  await tester.pump();
}

void main() {
  testWidgets('shows the code and the verification link', (tester) async {
    await pumpFlow(tester);

    expect(find.byKey(const Key('device-flow-code')), findsOneWidget);
    expect(find.text('RX2Y-4H7X'), findsOneWidget);
    expect(find.text('https://app.kilo.ai/device-auth'), findsOneWidget);
  });

  testWidgets('copies the code to the clipboard', (tester) async {
    final copied = <String>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied.add((call.arguments as Map)['text'] as String);
        }
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );

    await pumpFlow(tester);
    await tester.tap(find.byKey(const Key('device-flow-copy-code')));
    await tester.pump();

    expect(copied, contains('RX2Y-4H7X'));
  });

  testWidgets('counts down while waiting', (tester) async {
    await pumpFlow(tester);
    expect(find.textContaining('2m 00s left'), findsOneWidget);

    await tester.pump(const Duration(seconds: 1));
    expect(find.textContaining('1m 59s left'), findsOneWidget);

    // Let the periodic timer stop before the test ends.
    await tester.tap(find.byKey(const Key('device-flow-dismiss')));
    await tester.pumpAndSettle();
  });

  testWidgets('dismissing an unfinished flow cancels it', (tester) async {
    var cancelled = false;
    await pumpFlow(tester, onCancel: () async => cancelled = true);

    await tester.tap(find.byKey(const Key('device-flow-dismiss')));
    await tester.pumpAndSettle();

    expect(
      cancelled,
      isTrue,
      reason: 'an abandoned flow must stop the daemon polling',
    );
  });

  testWidgets('reports success and stops asking to cancel', (tester) async {
    var cancelled = false;
    await pumpFlow(
      tester,
      result: Future<String?>.value(null),
      onCancel: () async => cancelled = true,
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('device-flow-outcome')), findsOneWidget);
    expect(find.text('Signed in.'), findsOneWidget);
    expect(find.text('Close'), findsOneWidget);

    await tester.tap(find.byKey(const Key('device-flow-dismiss')));
    await tester.pumpAndSettle();
    expect(
      cancelled,
      isFalse,
      reason: 'a completed flow has nothing to cancel',
    );
  });

  testWidgets('reports failure with the reason', (tester) async {
    await pumpFlow(tester, result: Future<String?>.value('code expired'));
    await tester.pumpAndSettle();

    expect(find.textContaining('code expired'), findsOneWidget);
  });

  // MADR 0074 D8: codex's device flow signs the host out at the start, so the
  // dialog must name that consequence and default to not doing it.
  group('destructive confirmation', () {
    Future<bool?> showAndTap(WidgetTester tester, Key which) async {
      bool? answer;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => ElevatedButton(
                onPressed: () async {
                  answer = await confirmDestructiveSignIn(context, 'codex');
                },
                child: const Text('go'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('go'));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(which));
      await tester.pumpAndSettle();
      return answer;
    }

    testWidgets('names the consequence', (tester) async {
      await showAndTap(tester, const Key('destructive-signin-cancel'));
      // The dialog is gone now; re-open to inspect its text.
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => ElevatedButton(
                onPressed: () => confirmDestructiveSignIn(context, 'codex'),
                child: const Text('go'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('go'));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('signs the host out immediately'),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('destructive-signin-cancel')));
      await tester.pumpAndSettle();
    });

    testWidgets('cancel means no', (tester) async {
      final answer = await showAndTap(
        tester,
        const Key('destructive-signin-cancel'),
      );
      expect(answer, isFalse);
    });

    testWidgets('continue means yes', (tester) async {
      final answer = await showAndTap(
        tester,
        const Key('destructive-signin-confirm'),
      );
      expect(answer, isTrue);
    });
  });
}
