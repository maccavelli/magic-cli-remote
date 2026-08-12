import 'package:flutter/material.dart';

import '../../data/protocol/picker.dart';
import 'option_picker_sheet.dart';

/// A committed model choice from [showModelPicker].
class ModelPickerResult {
  const ModelPickerResult({
    required this.modelProvider,
    required this.model,
    required this.modelLabel,
    this.thinkingLevel,
  });

  final String modelProvider;
  final String model;
  final String modelLabel;
  final String? thinkingLevel;
}

/// Loads the model catalog for one model provider. Empty means unscoped.
typedef ModelCatalogLoader =
    Future<PickerCatalog> Function(String modelProvider);

/// Opens one model picker whose provider and model pages share one route.
Future<ModelPickerResult?> showModelPicker(
  BuildContext context, {
  required String provider,
  required PickerCatalog providerCatalog,
  required ModelCatalogLoader loadModels,
  String initialModelProvider = '',
  String initialModel = '',
  String initialModelLabel = '',
  String? thinkingIntent,
}) {
  return showModalBottomSheet<ModelPickerResult>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (context) => _ModelPickerSheet(
      provider: provider,
      providerCatalog: providerCatalog,
      loadModels: loadModels,
      initialModelProvider: initialModelProvider,
      initialModel: initialModel,
      initialModelLabel: initialModelLabel,
      thinkingIntent: thinkingIntent,
    ),
  );
}

enum _ModelPickerPage { providerMenu, allProviders, models }

class _ModelPickerSheet extends StatefulWidget {
  const _ModelPickerSheet({
    required this.provider,
    required this.providerCatalog,
    required this.loadModels,
    required this.initialModelProvider,
    required this.initialModel,
    required this.initialModelLabel,
    this.thinkingIntent,
  });

  final String provider;
  final PickerCatalog providerCatalog;
  final ModelCatalogLoader loadModels;
  final String initialModelProvider;
  final String initialModel;
  final String initialModelLabel;
  final String? thinkingIntent;

  @override
  State<_ModelPickerSheet> createState() => _ModelPickerSheetState();
}

class _ModelPickerSheetState extends State<_ModelPickerSheet> {
  late _ModelPickerPage _page;
  PickerOption? _selectedProvider;
  PickerCatalog? _modelCatalog;
  bool _loading = false;
  int _requestGeneration = 0;

  bool get _multiProvider => widget.providerCatalog.options.length > 1;

  @override
  void initState() {
    super.initState();
    final existingProvider = widget.providerCatalog.options
        .where((option) => option.id == widget.initialModelProvider)
        .firstOrNull;
    if (!_multiProvider) {
      _page = _ModelPickerPage.models;
      _startLoad(null);
    } else if (widget.initialModel.isNotEmpty && existingProvider != null) {
      _page = _ModelPickerPage.models;
      _selectedProvider = existingProvider;
      _startLoad(existingProvider);
    } else {
      _page = _ModelPickerPage.providerMenu;
    }
  }

  void _startLoad(PickerOption? provider) {
    final modelProvider = provider?.id ?? '';
    final generation = ++_requestGeneration;
    _selectedProvider = provider;
    _modelCatalog = null;
    _loading = true;
    widget.loadModels(modelProvider).then((catalog) {
      if (!mounted || generation != _requestGeneration) return;
      if ((_selectedProvider?.id ?? '') != modelProvider) return;
      setState(() {
        _modelCatalog = catalog;
        _loading = false;
      });
    });
  }

  void _loadProvider(PickerOption provider) {
    if (!provider.enabled) return;
    setState(() {
      _page = _ModelPickerPage.models;
      _startLoad(provider);
    });
  }

  void _returnToProviderMenu() {
    ++_requestGeneration;
    setState(() {
      _page = _ModelPickerPage.providerMenu;
      _selectedProvider = null;
      _modelCatalog = null;
      _loading = false;
    });
  }

  bool get _canRoutePop {
    if (!_multiProvider) return true;
    return _page == _ModelPickerPage.providerMenu;
  }

  void _handleBack() {
    if (_page == _ModelPickerPage.allProviders ||
        (_page == _ModelPickerPage.models && _multiProvider)) {
      _returnToProviderMenu();
    }
  }

  void _cancel() => Navigator.pop(context);

  List<String> _initialSelection(PickerCatalog catalog) {
    if (!_multiProvider) {
      if (widget.initialModel.isNotEmpty) return [widget.initialModel];
      return catalog.defaultIds;
    }

    final provider = _selectedProvider;
    if (provider == null) return const [];
    final ids = catalog.options.map((option) => option.id).toSet();
    final initial = widget.initialModel;
    if (initial.isNotEmpty &&
        (!initial.contains('/') || initial.startsWith('${provider.id}/')) &&
        (ids.contains(initial) || catalog.allowCustom)) {
      return [initial];
    }
    final providerDefault = provider.defaultModel;
    if (providerDefault.startsWith('${provider.id}/') &&
        ids.contains(providerDefault)) {
      return [providerDefault];
    }
    if (catalog.defaultIds.isNotEmpty) {
      final catalogDefault = catalog.defaultIds.first;
      final allUnqualified = catalog.options
          .where((option) => option.id.isNotEmpty)
          .every((option) => !option.id.contains('/'));
      if (ids.contains(catalogDefault) &&
          (catalogDefault.startsWith('${provider.id}/') || allUnqualified)) {
        return [catalogDefault];
      }
    }
    return const [];
  }

  void _confirmModel(PickerResult result) {
    final selected = result.single ?? '';
    if (selected.isEmpty) {
      Navigator.pop(
        context,
        const ModelPickerResult(
          modelProvider: '',
          model: '',
          modelLabel: '',
          thinkingLevel: null,
        ),
      );
      return;
    }

    final catalog = _modelCatalog!;
    final scopedProvider = _multiProvider ? _selectedProvider?.id ?? '' : '';
    final resolved = scopedProvider.isNotEmpty && !selected.contains('/')
        ? '$scopedProvider/$selected'
        : selected;
    PickerOption? option;
    for (final candidate in catalog.options) {
      if (candidate.id == selected || candidate.id == resolved) {
        option = candidate;
        break;
      }
    }
    Navigator.pop(
      context,
      ModelPickerResult(
        modelProvider: _multiProvider ? scopedProvider : catalog.modelProvider,
        model: resolved,
        modelLabel: option?.displayLabel ?? resolved,
        thinkingLevel: result.thinkingLevel,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return PopScope<ModelPickerResult>(
      canPop: _canRoutePop,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _handleBack();
      },
      child: switch (_page) {
        _ModelPickerPage.providerMenu => _providerMenu(context),
        _ModelPickerPage.allProviders => _allProviders(),
        _ModelPickerPage.models => _modelsPage(context),
      },
    );
  }

  Widget _providerMenu(BuildContext context) {
    final connected = widget.providerCatalog.options
        .where((option) => option.connected == true)
        .toList(growable: false);
    final scheme = Theme.of(context).colorScheme;
    return PickerSheetLayout(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PickerSheetHeader(
            title: 'Model · ${widget.provider}',
            source: widget.providerCatalog.source,
            onClose: _cancel,
          ),
          if (connected.isEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 20, 24, 8),
              child: Text(
                'No configured providers were reported. Set one up in '
                'Settings or on the host, or browse all providers.',
                style: TextStyle(color: scheme.onSurfaceVariant),
              ),
            ),
          Expanded(
            child: ListView(
              children: [
                for (final provider in connected) _providerTile(provider),
                ListTile(
                  key: const ValueKey('model-picker-browse-all'),
                  title: Text(
                    'Browse all reported providers '
                    '(${widget.providerCatalog.options.length})…',
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () =>
                      setState(() => _page = _ModelPickerPage.allProviders),
                ),
              ],
            ),
          ),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  TextButton(onPressed: _cancel, child: const Text('Cancel')),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _providerTile(PickerOption provider) {
    final subtitle = <String>[];
    if (provider.modelCount.isNotEmpty) {
      subtitle.add(
        provider.modelCount == '1'
            ? '${provider.modelCount} model'
            : '${provider.modelCount} models',
      );
    }
    if (provider.defaultModel.isNotEmpty) {
      final prefix = '${provider.id}/';
      final display = provider.defaultModel.startsWith(prefix)
          ? provider.defaultModel.substring(prefix.length)
          : provider.defaultModel;
      subtitle.add('default $display');
    }
    return ListTile(
      key: ValueKey('model-picker-provider-${provider.id}'),
      enabled: provider.enabled,
      title: Text(provider.displayLabel),
      subtitle: subtitle.isEmpty ? null : Text(subtitle.join(' · ')),
      trailing: const Icon(Icons.chevron_right),
      onTap: provider.enabled ? () => _loadProvider(provider) : null,
    );
  }

  Widget _allProviders() {
    return PickerCatalogView(
      catalog: widget.providerCatalog,
      title: 'All model providers · ${widget.provider}',
      interaction: PickerCatalogInteraction.navigate,
      onCancel: _cancel,
      onNavigate: _loadProvider,
      onBack: _returnToProviderMenu,
    );
  }

  Widget _modelsPage(BuildContext context) {
    if (_loading || _modelCatalog == null) {
      return PickerSheetLayout(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            PickerSheetHeader(
              title: _modelTitle,
              source: widget.providerCatalog.source,
              onClose: _cancel,
              onBack: _multiProvider ? _returnToProviderMenu : null,
            ),
            const Expanded(child: Center(child: CircularProgressIndicator())),
          ],
        ),
      );
    }
    final catalog = _modelCatalog!;
    return PickerCatalogView(
      key: ValueKey(
        'model-catalog-${_selectedProvider?.id ?? ''}-$_requestGeneration',
      ),
      catalog: catalog,
      title: _modelTitle,
      interaction: PickerCatalogInteraction.select,
      onCancel: _cancel,
      initialSelected: _initialSelection(catalog),
      seedCatalogDefault: false,
      thinkingIntent: widget.thinkingIntent,
      onConfirm: _confirmModel,
      onBack: _multiProvider ? _returnToProviderMenu : null,
    );
  }

  String get _modelTitle {
    final provider = _selectedProvider;
    return provider == null
        ? 'Model · ${widget.provider}'
        : 'Model · ${provider.displayLabel}';
  }
}
