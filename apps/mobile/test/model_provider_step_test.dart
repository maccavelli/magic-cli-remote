import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

typedef _ModelKey = ({String provider, String modelProvider});

class _CatalogRequest {
  const _CatalogRequest({
    required this.provider,
    this.scope,
    this.modelProvider,
  });

  final String provider;
  final String? scope;
  final String? modelProvider;
}

class _CreateCall {
  const _CreateCall({required this.provider, this.model, this.thinkingLevel});

  final String provider;
  final String? model;
  final String? thinkingLevel;
}

/// Catalog fake with independently gateable provider and scoped-model calls.
class _CatalogClient extends McremoteClient {
  _CatalogClient({
    Map<String, List<PickerOption>>? providerOptions,
    this.agentProviders = const ['opencode', 'grok'],
  }) : providerOptions =
           providerOptions ??
           {'opencode': _twoProviders, 'grok': _twoProviders};

  final Map<String, List<PickerOption>> providerOptions;
  final List<String> agentProviders;
  final List<_CatalogRequest> requests = [];
  final List<_CreateCall> creates = [];
  final Map<String, List<Future<PickerCatalog>>> providerResponses = {};
  final Map<_ModelKey, List<Future<PickerCatalog>>> modelResponses = {};

  Iterable<_CatalogRequest> get providerRequests =>
      requests.where((request) => request.scope == 'providers');

  Iterable<_CatalogRequest> get modelRequests =>
      requests.where((request) => request.scope == null);

  void enqueueProvider(String provider, Future<PickerCatalog> response) {
    providerResponses.putIfAbsent(provider, () => []).add(response);
  }

  void enqueueModel(
    String provider,
    String modelProvider,
    Future<PickerCatalog> response,
  ) {
    modelResponses
        .putIfAbsent((
          provider: provider,
          modelProvider: modelProvider,
        ), () => [])
        .add(response);
  }

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      const SessionListSnapshot(sessions: [], complete: true);

  @override
  Future<List<ProviderInfo>> listProviders() async => [
    for (final provider in agentProviders)
      ProviderInfo(id: provider, ready: true),
  ];

  @override
  Future<PickerCatalog> listModels(
    String provider, {
    String? scope,
    String? modelProvider,
    String? sessionId,
  }) {
    requests.add(
      _CatalogRequest(
        provider: provider,
        scope: scope,
        modelProvider: modelProvider,
      ),
    );
    if (scope == 'providers') {
      final queued = providerResponses[provider];
      if (queued != null && queued.isNotEmpty) return queued.removeAt(0);
      return Future.value(
        _providerCatalog(provider, options: providerOptions[provider]),
      );
    }
    final key = (provider: provider, modelProvider: modelProvider ?? '');
    final queued = modelResponses[key];
    if (queued != null && queued.isNotEmpty) return queued.removeAt(0);
    return Future.value(_modelCatalog(provider, modelProvider ?? ''));
  }

  @override
  Future<SessionMeta> createSession({
    String? provider,
    String? name,
    String? cwd,
    String? model,
    String? thinkingLevel,
    String? agent,
    String? agentSessionId,
    String? sessionId,
  }) async {
    creates.add(
      _CreateCall(
        provider: provider ?? '',
        model: model,
        thinkingLevel: thinkingLevel,
      ),
    );
    return SessionMeta(
      id: 'created',
      provider: provider ?? '',
      name: 'Created',
    );
  }
}

final _twoProviders = [
  PickerOption(
    id: 'zen',
    label: 'OpenCode Zen',
    group: 'Connected',
    meta: const {'connected': 'true'},
  ),
  PickerOption(
    id: 'anthropic',
    label: 'Anthropic',
    group: 'Connected',
    meta: const {'connected': 'true'},
  ),
  PickerOption(
    id: 'unconfigured',
    label: 'Unconfigured',
    group: 'All providers',
    meta: const {'connected': 'false'},
  ),
];

PickerCatalog _providerCatalog(
  String provider, {
  List<PickerOption>? options,
}) => PickerCatalog(
  provider: provider,
  options: options ?? _twoProviders,
  allowCustom: false,
);

PickerCatalog _modelCatalog(
  String provider,
  String modelProvider, {
  List<PickerOption>? options,
}) => PickerCatalog(
  provider: provider,
  modelProvider: modelProvider,
  allowCustom: true,
  options:
      options ??
      [
        PickerOption(
          id: modelProvider.isEmpty ? 'model-a' : '$modelProvider/model-a',
          label: 'Model A',
          thinkingLevels: const [
            ThinkingLevel(id: 'low', label: 'Low'),
            ThinkingLevel(id: 'high', label: 'High'),
          ],
        ),
      ],
);

Widget _wrap(McremoteClient client) {
  final router = GoRouter(
    routes: [
      GoRoute(path: '/', builder: (_, _) => const SessionsScreen()),
      GoRoute(
        path: '/sessions/:id',
        builder: (_, _) => const Scaffold(body: Text('Created session')),
      ),
    ],
  );
  return ProviderScope(
    overrides: [
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      mcremoteClientProvider.overrideWithValue(client),
    ],
    child: MaterialApp.router(routerConfig: router),
  );
}

Future<void> openDialog(WidgetTester tester, McremoteClient client) async {
  await tester.pumpWidget(_wrap(client));
  await tester.pumpAndSettle();
  await tester.tap(find.widgetWithText(FilledButton, 'New session'));
  await tester.pumpAndSettle();
}

Future<void> selectAgentProvider(WidgetTester tester, String provider) async {
  await tester.tap(find.byType(DropdownButtonFormField<String>).first);
  await tester.pumpAndSettle();
  await tester.tap(find.text(provider).last);
  await tester.pump();
}

Future<void> openDialogWithProvider(
  WidgetTester tester,
  McremoteClient client, {
  String provider = 'opencode',
}) async {
  await openDialog(tester, client);
  await selectAgentProvider(tester, provider);
  await tester.pumpAndSettle();
}

Finder _field(String label) => find
    .ancestor(of: find.text(label), matching: find.byType(InputDecorator))
    .first;

Future<void> tapField(WidgetTester tester, String label) async {
  await tester.tap(
    find.descendant(of: _field(label), matching: find.byType(InkWell)),
  );
  await tester.pumpAndSettle();
}

String fieldValue(WidgetTester tester, String label) {
  final text = find.descendant(of: _field(label), matching: find.byType(Text));
  for (final widget in tester.widgetList<Text>(text)) {
    if (widget.data != null && widget.data != label) return widget.data!;
  }
  return '';
}

Future<void> chooseModelProvider(
  WidgetTester tester,
  String displayLabel,
) async {
  await tester.tap(find.text(displayLabel));
  await tester.pumpAndSettle();
}

Future<void> chooseModel(WidgetTester tester) async {
  await tester.tap(find.text('Model A'));
  await tester.pump();
  await tester.tap(find.widgetWithText(FilledButton, 'Select'));
  await tester.pumpAndSettle();
}

Future<void> cancelPicker(WidgetTester tester) async {
  await tester.tap(find.widgetWithText(TextButton, 'Cancel').last);
  await tester.pumpAndSettle();
}

Future<void> create(WidgetTester tester) async {
  await tester.tap(find.widgetWithText(FilledButton, 'Create'));
  await tester.pumpAndSettle();
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets(
    'dialog always renders one Model field and no Model provider field',
    (tester) async {
      final client = _CatalogClient();
      await openDialog(tester, client);

      expect(find.text('Model (optional)'), findsOneWidget);
      expect(
        find.text(['Model', 'provider (optional)'].join(' ')),
        findsNothing,
      );
      await selectAgentProvider(tester, 'opencode');
      await tester.pumpAndSettle();
      expect(find.text('Model (optional)'), findsOneWidget);
      expect(
        find.text(['Model', 'provider (optional)'].join(' ')),
        findsNothing,
      );
    },
  );

  testWidgets(
    'Model field is visible but disabled before agent provider selection',
    (tester) async {
      await openDialog(tester, _CatalogClient());

      final inkWell = tester.widget<InkWell>(
        find.descendant(
          of: _field('Model (optional)'),
          matching: find.byType(InkWell),
        ),
      );
      expect(inkWell.onTap, isNull);
      expect(fieldValue(tester, 'Model (optional)'), 'Provider default');
    },
  );

  testWidgets('multi-provider field opens connected provider menu', (
    tester,
  ) async {
    await openDialogWithProvider(tester, _CatalogClient());
    await tapField(tester, 'Model (optional)');

    expect(find.text('OpenCode Zen'), findsOneWidget);
    expect(find.text('Anthropic'), findsOneWidget);
    expect(find.text('Unconfigured'), findsNothing);
  });

  testWidgets(
    'choosing model provider then backing out submits no model override',
    (tester) async {
      final client = _CatalogClient();
      await openDialogWithProvider(tester, client);
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'OpenCode Zen');
      await tester.tap(find.byKey(const ValueKey('model-picker-back')));
      await tester.pumpAndSettle();
      await cancelPicker(tester);
      await create(tester);

      expect(client.creates.single.model, isNull);
      expect(client.creates.single.thinkingLevel, isNull);
    },
  );

  testWidgets(
    'choosing scoped model updates field and submits qualified model',
    (tester) async {
      final client = _CatalogClient();
      await openDialogWithProvider(tester, client);
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'OpenCode Zen');
      await chooseModel(tester);

      expect(fieldValue(tester, 'Model (optional)'), 'Model A · zen/model-a');
      await create(tester);
      expect(client.creates.single.model, 'zen/model-a');
    },
  );

  testWidgets('chosen thinking level is submitted with the explicit model', (
    tester,
  ) async {
    final client = _CatalogClient();
    await openDialogWithProvider(tester, client);
    await tapField(tester, 'Model (optional)');
    await chooseModelProvider(tester, 'OpenCode Zen');
    await tester.tap(find.text('Model A'));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('thinking-zen/model-a-high')));
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Select'));
    await tester.pumpAndSettle();
    await create(tester);

    expect(client.creates.single.thinkingLevel, 'high');
  });

  testWidgets('clearing a prior model submits no model or thinking level', (
    tester,
  ) async {
    final client = _CatalogClient();
    await openDialogWithProvider(tester, client);
    await tapField(tester, 'Model (optional)');
    await chooseModelProvider(tester, 'OpenCode Zen');
    await tester.tap(find.text('Model A'));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('thinking-zen/model-a-high')));
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Select'));
    await tester.pumpAndSettle();

    await tapField(tester, 'Model (optional)');
    await tester.tap(find.widgetWithText(TextButton, 'Clear'));
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Select'));
    await tester.pumpAndSettle();
    await create(tester);

    expect(client.creates.single.model, isNull);
    expect(client.creates.single.thinkingLevel, isNull);
  });

  testWidgets('single-provider catalog opens model list directly', (
    tester,
  ) async {
    final client = _CatalogClient(
      providerOptions: {
        'opencode': [PickerOption(id: 'openai', label: 'OpenAI')],
      },
      agentProviders: const ['opencode'],
    );
    await openDialogWithProvider(tester, client);
    await tapField(tester, 'Model (optional)');

    expect(find.text('Model A'), findsOneWidget);
    expect(find.text('Browse all reported providers (1)…'), findsNothing);
    expect(client.modelRequests.single.modelProvider, isNull);
  });

  testWidgets('reopening committed model returns to its scoped catalog', (
    tester,
  ) async {
    final client = _CatalogClient();
    await openDialogWithProvider(tester, client);
    await tapField(tester, 'Model (optional)');
    await chooseModelProvider(tester, 'OpenCode Zen');
    await chooseModel(tester);

    await tapField(tester, 'Model (optional)');
    expect(find.text('Model · OpenCode Zen'), findsOneWidget);
    expect(find.byIcon(Icons.radio_button_checked), findsOneWidget);
  });

  testWidgets(
    'provider prefetch and immediate tap share one in-flight request',
    (tester) async {
      final gate = Completer<PickerCatalog>();
      final client = _CatalogClient()..enqueueProvider('opencode', gate.future);
      await openDialog(tester, client);
      await selectAgentProvider(tester, 'opencode');
      await tester.tap(
        find.descendant(
          of: _field('Model (optional)'),
          matching: find.byType(InkWell),
        ),
      );
      await tester.pump();

      expect(client.providerRequests, hasLength(1));
      gate.complete(_providerCatalog('opencode'));
      await tester.pumpAndSettle();
      expect(find.text('OpenCode Zen'), findsOneWidget);
    },
  );

  testWidgets(
    'reopening completed provider and model catalogs performs no new request',
    (tester) async {
      final client = _CatalogClient();
      await openDialogWithProvider(tester, client);
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'OpenCode Zen');
      await chooseModel(tester);
      await tapField(tester, 'Model (optional)');
      await cancelPicker(tester);

      expect(client.providerRequests, hasLength(1));
      expect(client.modelRequests, hasLength(1));
    },
  );

  testWidgets(
    'switching agent provider clears the committed model display and payload',
    (tester) async {
      final client = _CatalogClient();
      await openDialogWithProvider(tester, client);
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'OpenCode Zen');
      await chooseModel(tester);
      expect(fieldValue(tester, 'Model (optional)'), contains('zen/model-a'));

      await selectAgentProvider(tester, 'grok');
      await tester.pumpAndSettle();
      expect(fieldValue(tester, 'Model (optional)'), 'Provider default');
      await create(tester);
      expect(client.creates.single.provider, 'grok');
      expect(client.creates.single.model, isNull);
    },
  );

  testWidgets(
    'late provider catalog result after agent switch does not open stale picker',
    (tester) async {
      final gate = Completer<PickerCatalog>();
      final client = _CatalogClient()..enqueueProvider('opencode', gate.future);
      await openDialog(tester, client);
      await selectAgentProvider(tester, 'opencode');
      await tester.tap(
        find.descendant(
          of: _field('Model (optional)'),
          matching: find.byType(InkWell),
        ),
      );
      await tester.pump();
      await selectAgentProvider(tester, 'grok');
      gate.complete(_providerCatalog('opencode'));
      await tester.pumpAndSettle();

      expect(find.text('Model · opencode'), findsNothing);
      expect(find.text('Begin new session'), findsOneWidget);
      expect(fieldValue(tester, 'Model (optional)'), 'Provider default');
    },
  );

  testWidgets(
    'provider and model caches isolate agent and model-provider keys',
    (tester) async {
      final client = _CatalogClient();
      await openDialogWithProvider(tester, client);
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'OpenCode Zen');
      await cancelPicker(tester);
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'Anthropic');
      await cancelPicker(tester);
      await selectAgentProvider(tester, 'grok');
      await tester.pumpAndSettle();
      await tapField(tester, 'Model (optional)');
      await chooseModelProvider(tester, 'OpenCode Zen');
      await cancelPicker(tester);

      expect(client.providerRequests.map((request) => request.provider), [
        'opencode',
        'grok',
      ]);
      expect(
        client.modelRequests.map(
          (request) => (request.provider, request.modelProvider),
        ),
        [('opencode', 'zen'), ('opencode', 'anthropic'), ('grok', 'zen')],
      );
    },
  );

  testWidgets(
    'failed catalog request notifies once returns fallback and retries on next open',
    (tester) async {
      final providerFailure = Completer<PickerCatalog>();
      final client = _CatalogClient()
        ..enqueueProvider('opencode', providerFailure.future)
        ..enqueueProvider(
          'opencode',
          Future.value(_providerCatalog('opencode')),
        );
      await openDialog(tester, client);
      await selectAgentProvider(tester, 'opencode');
      await tester.tap(
        find.descendant(
          of: _field('Model (optional)'),
          matching: find.byType(InkWell),
        ),
      );
      await tester.pump();
      expect(client.providerRequests, hasLength(1));
      providerFailure.completeError(StateError('provider boom'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Could not load providers'), findsOneWidget);
      expect(client.modelRequests.single.modelProvider, isNull);
      await tester.pump(const Duration(seconds: 6));
      await tester.pumpAndSettle();
      await cancelPicker(tester);
      await tapField(tester, 'Model (optional)');
      await tester.pumpAndSettle();
      expect(client.providerRequests, hasLength(2));

      final modelFailure = Completer<PickerCatalog>();
      client.enqueueModel('opencode', 'anthropic', modelFailure.future);
      client.enqueueModel(
        'opencode',
        'anthropic',
        Future.value(_modelCatalog('opencode', 'anthropic')),
      );
      await tester.tap(find.text('Anthropic'));
      await tester.pump();
      await tester.tap(find.byKey(const ValueKey('model-picker-back')));
      await tester.pump();
      await tester.tap(find.text('Anthropic'));
      await tester.pump();
      expect(
        client.modelRequests.where(
          (request) => request.modelProvider == 'anthropic',
        ),
        hasLength(1),
      );
      modelFailure.completeError(StateError('model boom'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Could not load models'), findsOneWidget);
      expect(
        find.text('No catalog entries. Enter a custom value below.'),
        findsOneWidget,
      );
      await tester.enterText(
        find.widgetWithText(TextField, 'Custom value'),
        'mine',
      );
      await tester.tap(find.widgetWithText(FilledButton, 'Select'));
      await tester.pumpAndSettle();
      expect(fieldValue(tester, 'Model (optional)'), 'anthropic/mine');

      await tapField(tester, 'Model (optional)');
      await tester.pumpAndSettle();
      expect(
        client.modelRequests.where(
          (request) => request.modelProvider == 'anthropic',
        ),
        hasLength(2),
      );
    },
  );
}
