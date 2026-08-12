import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/widgets/model_picker_sheet.dart';

class _Loader {
  _Loader(this.catalogs);

  final Map<String, PickerCatalog> catalogs;
  final Map<String, Completer<PickerCatalog>> pending = {};
  final List<String> calls = [];

  Future<PickerCatalog> call(String provider) {
    calls.add(provider);
    return pending[provider]?.future ?? Future.value(catalogs[provider]!);
  }
}

class _ResultBox {
  ModelPickerResult? result;
  bool completed = false;
}

PickerOption _provider(
  String id, {
  bool? connected,
  bool enabled = true,
  String group = 'All providers',
  String label = '',
  String count = '',
  String defaultModel = '',
}) {
  return PickerOption(
    id: id,
    label: label,
    group: group,
    enabled: enabled,
    meta: {
      if (connected != null) 'connected': '$connected',
      if (count.isNotEmpty) 'model_count': count,
      if (defaultModel.isNotEmpty) 'default_model': defaultModel,
    },
  );
}

PickerCatalog _providers([List<PickerOption>? options]) => PickerCatalog(
  provider: 'opencode',
  source: PickerSource.live,
  options:
      options ??
      [
        _provider(
          'zen',
          connected: true,
          group: 'Connected',
          label: 'OpenCode Zen',
          count: '2',
          defaultModel: 'zen/z1',
        ),
        _provider(
          'disabled',
          connected: true,
          enabled: false,
          group: 'Connected',
          label: 'Disabled Provider',
        ),
        _provider('anthropic', connected: false, label: 'Anthropic'),
      ],
);

PickerCatalog _models(
  String provider, {
  List<PickerOption>? options,
  List<String> defaults = const [],
  bool allowCustom = true,
  bool truncated = false,
}) => PickerCatalog(
  provider: 'opencode',
  modelProvider: provider,
  source: PickerSource.live,
  allowCustom: allowCustom,
  defaultIds: defaults,
  truncated: truncated,
  options:
      options ??
      [
        PickerOption(id: '$provider/z1', label: 'Z One'),
        PickerOption(id: '$provider/z2', label: 'Z Two'),
      ],
);

Future<_ResultBox> _open(
  WidgetTester tester, {
  required PickerCatalog providers,
  required _Loader loader,
  String initialModelProvider = '',
  String initialModel = '',
  String initialModelLabel = '',
  String? thinkingIntent,
}) async {
  final box = _ResultBox();
  await tester.pumpWidget(
    MaterialApp(
      home: Builder(
        builder: (context) => Scaffold(
          body: TextButton(
            onPressed: () async {
              box.result = await showModelPicker(
                context,
                provider: 'opencode',
                providerCatalog: providers,
                loadModels: loader.call,
                initialModelProvider: initialModelProvider,
                initialModel: initialModel,
                initialModelLabel: initialModelLabel,
                thinkingIntent: thinkingIntent,
              );
              box.completed = true;
            },
            child: const Text('open'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 500));
  return box;
}

Future<void> _chooseProvider(WidgetTester tester, String id) async {
  await tester.tap(find.byKey(ValueKey('model-picker-provider-$id')));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 100));
}

Future<void> _select(WidgetTester tester, String label) async {
  await tester.tap(find.text(label));
  await tester.pump();
  await tester.tap(find.text('Select'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('multi-provider menu shows connected rows in catalog order', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen')});
    await _open(tester, providers: _providers(), loader: loader);
    expect(find.text('OpenCode Zen'), findsOneWidget);
    expect(find.text('Disabled Provider'), findsOneWidget);
    expect(find.text('Anthropic'), findsNothing);
    expect(
      tester.getTopLeft(find.text('OpenCode Zen')).dy,
      lessThan(tester.getTopLeft(find.text('Disabled Provider')).dy),
    );
    expect(loader.calls, isEmpty);
  });

  testWidgets('provider menu omits count and default when metadata is absent', (
    tester,
  ) async {
    final catalog = _providers([
      _provider('goose', connected: true, label: 'Goose'),
      _provider('other', connected: false),
    ]);
    await _open(tester, providers: catalog, loader: _Loader({}));
    final tile = tester.widget<ListTile>(
      find.ancestor(of: find.text('Goose'), matching: find.byType(ListTile)),
    );
    expect(tile.subtitle, isNull);
  });

  testWidgets(
    'provider menu formats singular plural and provider-relative default metadata',
    (tester) async {
      final catalog = _providers([
        _provider(
          'anthropic',
          connected: true,
          label: 'Anthropic',
          count: '1',
          defaultModel: 'anthropic/claude',
        ),
        _provider('openai', connected: true, label: 'OpenAI', count: '2'),
      ]);
      await _open(tester, providers: catalog, loader: _Loader({}));
      expect(find.text('1 model · default claude'), findsOneWidget);
      expect(find.text('2 models'), findsOneWidget);
    },
  );

  testWidgets(
    'browse-all count is dynamic and all groups and badges remain searchable',
    (tester) async {
      await _open(tester, providers: _providers(), loader: _Loader({}));
      expect(find.text('Browse all reported providers (3)…'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('model-picker-browse-all')));
      await tester.pump();
      expect(find.text('Connected'), findsOneWidget);
      expect(find.text('All providers'), findsOneWidget);
      expect(find.text('not configured'), findsOneWidget);
      await tester.enterText(find.byType(TextField).first, 'anthropic');
      await tester.pump(const Duration(milliseconds: 200));
      expect(find.text('Anthropic'), findsOneWidget);
      expect(find.text('OpenCode Zen'), findsNothing);
    },
  );

  testWidgets('zero connected providers keeps browse-all and setup guidance', (
    tester,
  ) async {
    final catalog = _providers([
      _provider('a', connected: false),
      _provider('b', connected: false),
    ]);
    await _open(tester, providers: catalog, loader: _Loader({}));
    expect(
      find.textContaining('No configured providers were reported'),
      findsOneWidget,
    );
    expect(find.text('Browse all reported providers (2)…'), findsOneWidget);
  });

  testWidgets('disabled provider row cannot navigate', (tester) async {
    final loader = _Loader({'disabled': _models('disabled')});
    await _open(tester, providers: _providers(), loader: loader);
    await tester.tap(
      find.byKey(const ValueKey('model-picker-provider-disabled')),
      warnIfMissed: false,
    );
    await tester.pump();
    expect(loader.calls, isEmpty);
  });

  testWidgets('zero reported providers loads the unscoped catalog directly', (
    tester,
  ) async {
    final loader = _Loader({'': _models('', options: [])});
    await _open(tester, providers: _providers([]), loader: loader);
    expect(loader.calls, ['']);
  });

  testWidgets('one reported provider loads the unscoped catalog directly', (
    tester,
  ) async {
    final loader = _Loader({
      '': _models('', options: [PickerOption(id: 'm')]),
    });
    await _open(
      tester,
      providers: _providers([_provider('implicit')]),
      loader: loader,
    );
    expect(loader.calls, ['']);
    expect(find.text('m'), findsOneWidget);
    expect(find.byKey(const ValueKey('model-picker-back')), findsNothing);
  });

  testWidgets('reopening an existing choice loads its provider directly', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen')});
    await _open(
      tester,
      providers: _providers(),
      loader: loader,
      initialModelProvider: 'zen',
      initialModel: 'zen/z2',
    );
    expect(loader.calls, ['zen']);
    expect(find.text('Z Two'), findsOneWidget);
  });

  testWidgets('model load stays in the same sheet and shows progress', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen')});
    loader.pending['zen'] = Completer<PickerCatalog>();
    await _open(tester, providers: _providers(), loader: loader);
    await tester.tap(find.byKey(const ValueKey('model-picker-provider-zen')));
    await tester.pump();
    expect(find.byType(BottomSheet), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    loader.pending['zen']!.complete(_models('zen'));
    await tester.pumpAndSettle();
    expect(find.text('Z One'), findsOneWidget);
  });

  testWidgets(
    'provider tap loads its scoped catalog and returns qualified model',
    (tester) async {
      final loader = _Loader({
        'zen': _models(
          'zen',
          options: [PickerOption(id: 'z1', label: 'Z One')],
        ),
      });
      final box = await _open(tester, providers: _providers(), loader: loader);
      await _chooseProvider(tester, 'zen');
      await _select(tester, 'Z One');
      expect(loader.calls, ['zen']);
      expect(box.result?.modelProvider, 'zen');
      expect(box.result?.model, 'zen/z1');
    },
  );

  testWidgets('scoped custom model is qualified before return', (tester) async {
    final loader = _Loader({'zen': _models('zen', options: [])});
    final box = await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    await tester.enterText(
      find.widgetWithText(TextField, 'Custom value'),
      'custom',
    );
    await tester.tap(find.text('Select'));
    await tester.pumpAndSettle();
    expect(box.result?.model, 'zen/custom');
  });

  testWidgets('direct-path custom model keeps existing unqualified semantics', (
    tester,
  ) async {
    final loader = _Loader({'': _models('', options: [])});
    final box = await _open(tester, providers: _providers([]), loader: loader);
    await tester.enterText(
      find.widgetWithText(TextField, 'Custom value'),
      'custom',
    );
    await tester.tap(find.text('Select'));
    await tester.pumpAndSettle();
    expect(box.result?.model, 'custom');
  });

  testWidgets(
    'selected option returns display label and custom returns resolved ID label',
    (tester) async {
      final selectedLoader = _Loader({'zen': _models('zen')});
      final selected = await _open(
        tester,
        providers: _providers(),
        loader: selectedLoader,
      );
      await _chooseProvider(tester, 'zen');
      await _select(tester, 'Z One');
      expect(selected.result?.modelLabel, 'Z One');

      final customLoader = _Loader({'zen': _models('zen', options: [])});
      final custom = await _open(
        tester,
        providers: _providers(),
        loader: customLoader,
      );
      await _chooseProvider(tester, 'zen');
      await tester.enterText(
        find.widgetWithText(TextField, 'Custom value'),
        'mine',
      );
      await tester.tap(find.text('Select'));
      await tester.pumpAndSettle();
      expect(custom.result?.modelLabel, 'zen/mine');
    },
  );

  testWidgets('provider metadata default wins when valid', (tester) async {
    final loader = _Loader({
      'zen': _models('zen', defaults: const ['zen/z2']),
    });
    await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    final icon = tester.widget<Icon>(
      find.descendant(
        of: find.ancestor(
          of: find.text('Z One'),
          matching: find.byType(ListTile),
        ),
        matching: find.byIcon(Icons.radio_button_checked),
      ),
    );
    expect(icon.icon, Icons.radio_button_checked);
  });

  testWidgets('cross-provider catalog default is not preselected', (
    tester,
  ) async {
    final loader = _Loader({
      'zen': _models('zen', defaults: const ['other/model']),
    });
    await _open(
      tester,
      providers: _providers([
        _provider('zen', connected: true, group: 'Connected'),
        _provider('anthropic', connected: false),
      ]),
      loader: loader,
    );
    await _chooseProvider(tester, 'zen');
    expect(find.byIcon(Icons.radio_button_checked), findsNothing);
  });

  testWidgets('unqualified scoped catalog default is accepted', (tester) async {
    final loader = _Loader({
      'zen': _models(
        'zen',
        options: [PickerOption(id: 'plain', label: 'Plain')],
        defaults: const ['plain'],
      ),
    });
    await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    expect(find.byIcon(Icons.radio_button_checked), findsOneWidget);
  });

  testWidgets(
    'clear returns an empty committed result and null thinking level',
    (tester) async {
      final loader = _Loader({
        'zen': _models(
          'zen',
          options: [
            PickerOption(
              id: 'zen/z1',
              thinkingLevels: const [ThinkingLevel(id: 'high')],
            ),
          ],
        ),
      });
      final box = await _open(
        tester,
        providers: _providers(),
        loader: loader,
        initialModelProvider: 'zen',
        initialModel: 'zen/z1',
        thinkingIntent: 'high',
      );
      await tester.tap(find.text('Clear'));
      await tester.tap(find.text('Select'));
      await tester.pumpAndSettle();
      expect(box.result?.model, isEmpty);
      expect(box.result?.modelProvider, isEmpty);
      expect(box.result?.thinkingLevel, isNull);
    },
  );

  testWidgets(
    'cancel returns null and preserves no transient provider choice',
    (tester) async {
      final loader = _Loader({'zen': _models('zen')});
      final box = await _open(tester, providers: _providers(), loader: loader);
      await _chooseProvider(tester, 'zen');
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(box.completed, isTrue);
      expect(box.result, isNull);
    },
  );

  testWidgets('close returns null and preserves the prior committed choice', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen')});
    final box = await _open(
      tester,
      providers: _providers(),
      loader: loader,
      initialModelProvider: 'zen',
      initialModel: 'zen/z1',
    );
    await tester.tap(find.byKey(const ValueKey('model-picker-close')));
    await tester.pumpAndSettle();
    expect(box.result, isNull);
  });

  testWidgets('system back returns models to provider menu before dismissing', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen')});
    final box = await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Browse all reported providers (3)…'), findsOneWidget);
    expect(box.completed, isFalse);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(box.completed, isTrue);
  });

  testWidgets('header back follows the same transition as system back', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen')});
    await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    await tester.tap(find.byKey(const ValueKey('model-picker-back')));
    await tester.pump();
    expect(find.text('Browse all reported providers (3)…'), findsOneWidget);
  });

  testWidgets('back from all providers returns to provider menu', (
    tester,
  ) async {
    await _open(tester, providers: _providers(), loader: _Loader({}));
    await tester.tap(find.byKey(const ValueKey('model-picker-browse-all')));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('model-picker-back')));
    await tester.pump();
    expect(find.text('Browse all reported providers (3)…'), findsOneWidget);
  });

  testWidgets('late model load is ignored after back', (tester) async {
    final loader = _Loader({'zen': _models('zen')});
    loader.pending['zen'] = Completer<PickerCatalog>();
    await _open(tester, providers: _providers(), loader: loader);
    await tester.tap(find.byKey(const ValueKey('model-picker-provider-zen')));
    await tester.pump();
    await tester.binding.handlePopRoute();
    await tester.pump();
    loader.pending['zen']!.complete(_models('zen'));
    await tester.pumpAndSettle();
    expect(find.text('Browse all reported providers (3)…'), findsOneWidget);
    expect(find.text('Z One'), findsNothing);
  });

  testWidgets('newer provider load wins and receives fresh picker state', (
    tester,
  ) async {
    final catalog = _providers([
      _provider('a', connected: true),
      _provider('b', connected: true),
    ]);
    final loader = _Loader({'a': _models('a'), 'b': _models('b')});
    loader.pending['a'] = Completer<PickerCatalog>();
    await _open(tester, providers: catalog, loader: loader);
    await tester.tap(find.byKey(const ValueKey('model-picker-provider-a')));
    await tester.pump();
    await tester.binding.handlePopRoute();
    await tester.pump();
    await _chooseProvider(tester, 'b');
    expect(find.text('b/z1'), findsOneWidget);
    loader.pending['a']!.complete(_models('a'));
    await tester.pumpAndSettle();
    expect(find.text('b/z1'), findsOneWidget);
    expect(find.text('a/z1'), findsNothing);
  });

  testWidgets('thinking chips survive drill-down and return the chosen level', (
    tester,
  ) async {
    final loader = _Loader({
      'zen': _models(
        'zen',
        options: [
          PickerOption(
            id: 'zen/z1',
            label: 'Z One',
            thinkingLevels: const [
              ThinkingLevel(id: 'low'),
              ThinkingLevel(id: 'high'),
            ],
          ),
        ],
      ),
    });
    final box = await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    await tester.tap(find.text('Z One'));
    await tester.tap(find.byKey(const ValueKey('thinking-zen/z1-high')));
    await tester.tap(find.text('Select'));
    await tester.pumpAndSettle();
    expect(box.result?.thinkingLevel, 'high');
  });

  testWidgets('empty scoped catalog keeps custom-entry empty state', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen', options: [])});
    await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    expect(find.textContaining('No catalog entries'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Custom value'), findsOneWidget);
  });

  testWidgets('truncated scoped catalog keeps the visible truncation footer', (
    tester,
  ) async {
    final loader = _Loader({'zen': _models('zen', truncated: true)});
    await _open(tester, providers: _providers(), loader: loader);
    await _chooseProvider(tester, 'zen');
    expect(find.textContaining('Showing the first 2'), findsOneWidget);
  });
}
