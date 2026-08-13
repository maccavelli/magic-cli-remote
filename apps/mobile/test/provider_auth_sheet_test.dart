import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/settings/provider_auth_sheet.dart';

/// The github-copilot shape from Kilo's live catalog: a select that gates a
/// conditional text field (MADR 0074 §7.3).
UpstreamAuth copilotUpstream() => const UpstreamAuth(
  id: 'github-copilot',
  label: 'GitHub Copilot',
  status: AuthStatus.missing,
  methods: [
    AuthMethod(
      id: 'github-copilot:0',
      type: AuthMethodType.apiKey,
      label: 'API key',
      inputs: [
        AuthInput(
          key: 'deploymentType',
          type: 'select',
          message: 'Select GitHub deployment type',
          options: [
            AuthInputOption(value: 'github.com', label: 'GitHub.com'),
            AuthInputOption(value: 'enterprise', label: 'Enterprise'),
          ],
        ),
        AuthInput(
          key: 'enterpriseUrl',
          type: 'text',
          message: 'Enterprise URL',
          when: AuthInputCondition(
            key: 'deploymentType',
            op: 'eq',
            value: 'enterprise',
          ),
        ),
      ],
    ),
  ],
);

Future<ProviderAuthSubmission?> pumpSheet(
  WidgetTester tester,
  UpstreamAuth upstream,
) async {
  ProviderAuthSubmission? result;
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => ElevatedButton(
            onPressed: () async {
              result = await showModalBottomSheet<ProviderAuthSubmission>(
                context: context,
                isScrollControlled: true,
                builder: (_) =>
                    ProviderAuthSheet(providerId: 'kilo', upstream: upstream),
              );
            },
            child: const Text('open'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  return result;
}

void main() {
  testWidgets('renders declared inputs and the key field', (tester) async {
    await pumpSheet(tester, copilotUpstream());

    expect(
      find.byKey(const Key('provider-auth-input-deploymentType')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('provider-auth-secret')), findsOneWidget);
  });

  testWidgets('conditional input stays hidden until its condition holds', (
    tester,
  ) async {
    await pumpSheet(tester, copilotUpstream());

    // deploymentType defaults to github.com, so the enterprise URL is not
    // applicable and must not be shown.
    expect(
      find.byKey(const Key('provider-auth-input-enterpriseUrl')),
      findsNothing,
    );

    await tester.tap(
      find.byKey(const Key('provider-auth-input-deploymentType')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Enterprise').last);
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('provider-auth-input-enterpriseUrl')),
      findsOneWidget,
    );
  });

  testWidgets('submits the key and only the visible inputs', (tester) async {
    ProviderAuthSubmission? captured;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () async {
                captured = await showModalBottomSheet<ProviderAuthSubmission>(
                  context: context,
                  isScrollControlled: true,
                  builder: (_) => ProviderAuthSheet(
                    providerId: 'kilo',
                    upstream: copilotUpstream(),
                  ),
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const Key('provider-auth-secret')),
      'sk-test-key',
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('provider-auth-submit')));
    await tester.pumpAndSettle();

    expect(captured, isNotNull);
    expect(captured!.secret, 'sk-test-key');
    expect(captured!.inputs['deploymentType'], 'github.com');
    // The hidden conditional field must not be submitted.
    expect(captured!.inputs.containsKey('enterpriseUrl'), isFalse);
  });

  testWidgets('submit stays disabled until a key is entered', (tester) async {
    await pumpSheet(tester, copilotUpstream());

    final button = tester.widget<FilledButton>(
      find.byKey(const Key('provider-auth-submit')),
    );
    expect(button.onPressed, isNull);

    await tester.enterText(
      find.byKey(const Key('provider-auth-secret')),
      'sk-x',
    );
    await tester.pumpAndSettle();

    final enabled = tester.widget<FilledButton>(
      find.byKey(const Key('provider-auth-submit')),
    );
    expect(enabled.onPressed, isNotNull);
  });

  testWidgets('browser-only OAuth is rendered but not submittable', (
    tester,
  ) async {
    await pumpSheet(
      tester,
      const UpstreamAuth(
        id: 'snowflake-cortex',
        label: 'Snowflake',
        status: AuthStatus.missing,
        methods: [
          AuthMethod(
            id: 'snowflake:0',
            type: AuthMethodType.oauthBrowser,
            label: 'Login with Snowflake (External Browser)',
          ),
        ],
      ),
    );

    expect(
      find.textContaining('cannot be completed from the phone'),
      findsOneWidget,
    );
    final button = tester.widget<FilledButton>(
      find.byKey(const Key('provider-auth-submit')),
    );
    expect(button.onPressed, isNull);
  });

  testWidgets('device OAuth needs no key field', (tester) async {
    await pumpSheet(
      tester,
      const UpstreamAuth(
        id: 'kilo',
        label: 'Kilo Gateway',
        status: AuthStatus.missing,
        methods: [
          AuthMethod(
            id: 'kilo:0',
            type: AuthMethodType.oauthDevice,
            label: 'Kilo Gateway (Device Authorization)',
          ),
        ],
      ),
    );

    expect(find.byKey(const Key('provider-auth-secret')), findsNothing);
    expect(find.text('Start sign-in'), findsOneWidget);
    final button = tester.widget<FilledButton>(
      find.byKey(const Key('provider-auth-submit')),
    );
    expect(button.onPressed, isNotNull);
  });

  // MADR 0074 D11: the credential must not survive the sheet. Submitting
  // clears the controller before the value leaves.
  group('system bar insets', _insetTests);

  testWidgets('secret field is obscured and cleared on submit', (tester) async {
    await pumpSheet(tester, copilotUpstream());

    final field = tester.widget<TextField>(
      find.byKey(const Key('provider-auth-secret')),
    );
    expect(field.obscureText, isTrue);
    expect(field.autocorrect, isFalse);
    expect(field.enableSuggestions, isFalse);

    await tester.enterText(
      find.byKey(const Key('provider-auth-secret')),
      'sk-secret',
    );
    await tester.pumpAndSettle();
    expect(field.controller!.text, 'sk-secret');

    await tester.tap(find.byKey(const Key('provider-auth-submit')));
    await tester.pumpAndSettle();

    expect(field.controller!.text, isEmpty);
  });
}

// Regression: the submit button rendered underneath the Android navigation
// bar, because the sheet padded only for the keyboard (viewInsets) and ignored
// the system bar (viewPadding). Reported from a real device.
void _insetTests() {
  const navBar = 48.0;

  Future<void> pumpWithInsets(
    WidgetTester tester, {
    double viewPaddingBottom = 0,
    double viewInsetsBottom = 0,
  }) async {
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: MediaQueryData(
            viewPadding: EdgeInsets.only(bottom: viewPaddingBottom),
            viewInsets: EdgeInsets.only(bottom: viewInsetsBottom),
          ),
          // Material, not Scaffold: a Scaffold body consumes viewInsets via
          // resizeToAvoidBottomInset, but the real sheet is a route overlay
          // where it does not — so a Scaffold here would test the harness.
          child: Material(
            child: ProviderAuthSheet(
              providerId: 'kilo',
              upstream: copilotUpstream(),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('leaves the navigation bar clear when the keyboard is down', (
    tester,
  ) async {
    await pumpWithInsets(tester, viewPaddingBottom: navBar);

    final padding = tester.widget<Padding>(
      find
          .descendant(
            of: find.byType(ProviderAuthSheet),
            matching: find.byType(Padding),
          )
          .first,
    );
    final bottom = (padding.padding as EdgeInsets).bottom;
    expect(
      bottom,
      greaterThanOrEqualTo(navBar),
      reason: 'the submit row must sit above the system navigation bar',
    );
  });

  testWidgets('does not double-count the bar when the keyboard is up', (
    tester,
  ) async {
    const keyboard = 300.0;
    await pumpWithInsets(
      tester,
      viewPaddingBottom: navBar,
      viewInsetsBottom: keyboard,
    );

    final padding = tester.widget<Padding>(
      find
          .descendant(
            of: find.byType(ProviderAuthSheet),
            matching: find.byType(Padding),
          )
          .first,
    );
    final bottom = (padding.padding as EdgeInsets).bottom;
    // max(), not sum: the keyboard inset already spans the bar's region.
    expect(
      bottom,
      lessThan(keyboard + navBar),
      reason: 'summing the two insets leaves a visible gap above the keyboard',
    );
    expect(bottom, greaterThanOrEqualTo(keyboard));
  });

  testWidgets('content scrolls so the button is always reachable', (
    tester,
  ) async {
    await pumpWithInsets(tester, viewPaddingBottom: navBar);
    expect(
      find.descendant(
        of: find.byType(ProviderAuthSheet),
        matching: find.byType(SingleChildScrollView),
      ),
      findsOneWidget,
    );
  });

  testWidgets('the sheet defaults to the first drivable method', (
    tester,
  ) async {
    // MADR 0083 D4 (fixes A3): engines list oauth first for popular vendors;
    // defaulting there used to send users straight into the broken path.
    const upstream = UpstreamAuth(
      id: 'anthropic',
      label: 'Anthropic',
      status: 'missing',
      methods: [
        AuthMethod(
          id: 'anthropic:0',
          type: 'oauth_device',
          label: 'Claude Pro/Max',
          available: false,
          reason: 'device_unsupported',
        ),
        AuthMethod(id: 'anthropic:1', type: 'api_key', label: 'API key'),
      ],
    );
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ProviderAuthSheet(providerId: 'opencode', upstream: upstream),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // The selected method is the API key: its secret field is shown.
    expect(find.byKey(const Key('provider-auth-secret')), findsOneWidget);
    // The unavailable sibling is rendered in the dropdown but marked.
    await tester.tap(find.byKey(const Key('provider-auth-method')));
    await tester.pumpAndSettle();
    expect(find.textContaining('— host only'), findsWidgets);
  });
}
