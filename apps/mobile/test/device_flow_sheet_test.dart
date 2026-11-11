import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/settings/device_flow_sheet.dart';

// DeviceUrlLauncher is defined in device_flow_sheet.dart.

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
  Future<DeviceFlowOutcome>? result,
  Future<void> Function()? onCancel,
  DeviceUrlLauncher? launchUrlFn,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: DeviceFlowSheet(
          flow: flow,
          result: result,
          onCancel: onCancel ?? () async {},
          launchUrlFn: launchUrlFn,
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

  testWidgets('open link invokes the launcher with the verification URI', (
    tester,
  ) async {
    final opened = <Uri>[];
    await pumpFlow(
      tester,
      launchUrlFn: (uri) async {
        opened.add(uri);
        return true;
      },
    );
    await tester.tap(find.byKey(const Key('device-flow-open-uri')));
    await tester.pump();
    expect(opened, [Uri.parse('https://app.kilo.ai/device-auth')]);
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
      result: Future.value(
        const DeviceFlowOutcome(ok: true, state: 'completed'),
      ),
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
    await pumpFlow(
      tester,
      result: Future.value(
        const DeviceFlowOutcome(
          ok: false,
          state: 'failed',
          retryable: true,
          error: 'code expired',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('code expired'), findsOneWidget);
  });

  // MADR 0074 D36: the sheet reads the result's state, not ok alone. Each
  // ok:false state below would read "Sign-in failed" if ok were all it read.
  group('renders each result state', () {
    const cases = <(String, bool, String, bool)>[
      // state, retryable, text the sheet must show, whether a retry hint shows
      ('cancelled', true, 'Sign-in cancelled.', true),
      ('expired', true, 'The sign-in code expired before it was used.', true),
      (
        'conflict',
        true,
        'The host\'s sign-in changed while this was in progress, so nothing '
            'was replaced.',
        true,
      ),
      (
        'recovery_required',
        false,
        'The host needs its sign-in recovered first: run mcremote '
            'auth-recovery there.',
        false,
      ),
      (
        'unsupported_backend',
        false,
        'The host keeps this sign-in where mcremote cannot protect it — sign '
            'in on the host.',
        false,
      ),
    ];
    for (final (state, retryable, text, hint) in cases) {
      testWidgets(state, (tester) async {
        await pumpFlow(
          tester,
          result: Future.value(
            DeviceFlowOutcome(
              ok: false,
              state: state,
              retryable: retryable,
              error: 'raw daemon text',
            ),
          ),
        );
        await tester.pumpAndSettle();

        expect(find.text(text), findsOneWidget);
        expect(find.textContaining('Sign-in failed'), findsNothing);
        expect(find.textContaining('raw daemon text'), findsNothing);
        expect(
          find.byKey(const Key('device-flow-retry-hint')),
          hint ? findsOneWidget : findsNothing,
        );
      });
    }

    testWidgets('completed shows no retry hint', (tester) async {
      await pumpFlow(
        tester,
        result: Future.value(
          const DeviceFlowOutcome(
            ok: true,
            state: 'completed',
            retryable: true,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Signed in.'), findsOneWidget);
      expect(find.byKey(const Key('device-flow-retry-hint')), findsNothing);
    });

    testWidgets('an older daemon, with no state, falls back to ok and error', (
      tester,
    ) async {
      await pumpFlow(
        tester,
        result: Future.value(
          DeviceFlowOutcome.fromJson({
            'flow_id': 'f-1',
            'ok': false,
            'error': 'x',
          }),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Sign-in failed: x'), findsOneWidget);
    });
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
