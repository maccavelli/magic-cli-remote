import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

/// Serves a model-provider catalog and a per-provider model catalog, recording
/// every request so the scoping can be asserted.
class _CatalogClient extends McremoteClient {
  _CatalogClient({required this.providerOptions});

  final List<PickerOption> providerOptions;
  final List<Map<String, String?>> requests = [];

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      const SessionListSnapshot(sessions: [], complete: true);

  @override
  Future<List<ProviderInfo>> listProviders() async => [
    ProviderInfo(id: 'opencode', ready: true),
  ];

  @override
  Future<PickerCatalog> listModels(
    String provider, {
    String? scope,
    String? modelProvider,
    String? sessionId,
  }) async {
    requests.add({
      'scope': scope,
      'model_provider': modelProvider,
      'session_id': sessionId,
    });
    if (scope == 'providers') {
      return PickerCatalog(
        provider: provider,
        options: providerOptions,
        allowCustom: false,
      );
    }
    return PickerCatalog(
      provider: provider,
      modelProvider: modelProvider ?? '',
      allowCustom: true,
      options: [
        PickerOption(
          id: '${modelProvider ?? 'opencode'}/model-a',
          label: 'Model A',
        ),
      ],
    );
  }
}

Widget _wrap(McremoteClient client) => ProviderScope(
  overrides: [
    connectionStateProvider.overrideWith(
      (ref) => Stream.value(McConnectionState.connected),
    ),
    mcremoteClientProvider.overrideWithValue(client),
  ],
  child: MaterialApp(home: const SessionsScreen()),
);

Future<void> openDialogWithProvider(
  WidgetTester tester,
  McremoteClient client,
) async {
  await tester.pumpWidget(_wrap(client));
  await tester.pumpAndSettle();
  await tester.tap(find.widgetWithText(FilledButton, 'New session'));
  await tester.pumpAndSettle();
  // The hint Text sits inside the dropdown's own layout and does not hit-test;
  // open the menu through the form field itself.
  await tester.tap(find.byType(DropdownButtonFormField<String>).first);
  await tester.pumpAndSettle();
  await tester.tap(find.text('opencode').last);
  await tester.pumpAndSettle();
}

/// Taps the tappable row of the dialog field labelled [label]. Several rows
/// read "Provider default", so targeting by value alone is ambiguous.
Future<void> tapField(WidgetTester tester, String label) async {
  final field = find
      .ancestor(of: find.text(label), matching: find.byType(InputDecorator))
      .first;
  await tester.tap(find.descendant(of: field, matching: find.byType(InkWell)));
  await tester.pumpAndSettle();
}

/// The value shown by the dialog field labelled [label].
String fieldValue(WidgetTester tester, String label) {
  final field = find
      .ancestor(of: find.text(label), matching: find.byType(InputDecorator))
      .first;
  final text = find.descendant(of: field, matching: find.byType(Text));
  for (final w in tester.widgetList<Text>(text)) {
    if (w.data != null && w.data != label) return w.data!;
  }
  return '';
}

/// Dismisses the picker sheet. The create dialog has a Cancel button too, so
/// the sheet's own must be targeted explicitly — it is the last one built.
Future<void> cancelPicker(WidgetTester tester) async {
  await tester.tap(find.widgetWithText(TextButton, 'Cancel').last);
  await tester.pumpAndSettle();
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  final twoProviders = [
    PickerOption(
      id: 'opencode',
      label: 'OpenCode Zen',
      group: 'Connected',
      meta: const {'connected': 'true'},
    ),
    PickerOption(
      id: 'anthropic',
      label: 'Anthropic',
      group: 'All providers',
      meta: const {'connected': 'false'},
    ),
  ];

  testWidgets('the provider row appears for an agent with several providers', (
    tester,
  ) async {
    final client = _CatalogClient(providerOptions: twoProviders);
    await openDialogWithProvider(tester, client);

    expect(find.text('Model provider (optional)'), findsOneWidget);
    // Resolved without the user tapping anything: choosing the agent is enough.
    expect(
      client.requests.where((r) => r['scope'] == 'providers'),
      hasLength(1),
    );
  });

  testWidgets('the provider row stays hidden for a single-provider agent', (
    tester,
  ) async {
    // Codex and grok report one model provider each; a menu of one is a tap
    // that teaches the user nothing.
    final client = _CatalogClient(
      providerOptions: [PickerOption(id: 'openai', label: 'OpenAI')],
    );
    await openDialogWithProvider(tester, client);

    expect(find.text('Model provider (optional)'), findsNothing);
    expect(find.text('Select model (optional)'), findsOneWidget);
  });

  testWidgets(
    'choosing a provider scopes the model request and clears the model',
    (tester) async {
      final client = _CatalogClient(providerOptions: twoProviders);
      await openDialogWithProvider(tester, client);

      // Pick a model under the default (connected-set) scope first.
      await tapField(tester, 'Select model (optional)');
      await tester.tap(find.text('Model A'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Select'));
      await tester.pumpAndSettle();
      expect(fieldValue(tester, 'Select model (optional)'), 'opencode/model-a');

      // Now switch model provider.
      await tapField(tester, 'Model provider (optional)');
      await tester.tap(find.text('Anthropic'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Select'));
      await tester.pumpAndSettle();
      expect(fieldValue(tester, 'Model provider (optional)'), 'anthropic');

      // The model chosen under the old provider is no longer meaningful.
      expect(fieldValue(tester, 'Select model (optional)'), 'Provider default');

      // And the next model request carries the new scope.
      await tapField(tester, 'Select model (optional)');
      final modelRequests = client.requests
          .where((r) => r['scope'] == null)
          .toList();
      expect(modelRequests.last['model_provider'], 'anthropic');
    },
  );

  testWidgets('re-opening a picker does not re-fetch its catalog', (
    tester,
  ) async {
    final client = _CatalogClient(providerOptions: twoProviders);
    await openDialogWithProvider(tester, client);

    await tapField(tester, 'Model provider (optional)');
    await cancelPicker(tester);
    await tapField(tester, 'Model provider (optional)');
    await cancelPicker(tester);

    // One fetch for the whole dialog: for OpenCode the provider list sits
    // behind a multi-MB engine catalog, so re-asking per tap is not free.
    expect(
      client.requests.where((r) => r['scope'] == 'providers'),
      hasLength(1),
    );
  });
}
