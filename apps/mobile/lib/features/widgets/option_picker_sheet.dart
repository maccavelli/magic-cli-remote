import 'package:flutter/material.dart';

import '../../data/protocol/picker.dart';

/// Result of [showOptionPicker]. Null means cancelled.
class PickerResult {
  const PickerResult(this.selectedIds);
  final List<String> selectedIds;

  String? get single => selectedIds.isEmpty ? null : selectedIds.first;
}

/// Full-screen modal interactive picker with search, groups, single/multi
/// select, disabled rows, and optional free-text custom value.
Future<PickerResult?> showOptionPicker(
  BuildContext context, {
  required PickerCatalog catalog,
  String title = 'Choose',
  List<String>? initialSelected,
}) {
  return showModalBottomSheet<PickerResult>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (ctx) => _OptionPickerSheet(
      catalog: catalog,
      title: title,
      initialSelected: initialSelected ?? catalog.defaultIds,
    ),
  );
}

class _OptionPickerSheet extends StatefulWidget {
  const _OptionPickerSheet({
    required this.catalog,
    required this.title,
    required this.initialSelected,
  });

  final PickerCatalog catalog;
  final String title;
  final List<String> initialSelected;

  @override
  State<_OptionPickerSheet> createState() => _OptionPickerSheetState();
}

class _OptionPickerSheetState extends State<_OptionPickerSheet> {
  late final Set<String> _selected;
  late final TextEditingController _search;
  late final TextEditingController _custom;
  String _query = '';

  PickerCatalog get c => widget.catalog;

  @override
  void initState() {
    super.initState();
    _selected = {};
    for (final id in widget.initialSelected) {
      if (id.trim().isEmpty) continue;
      _selected.add(id.trim());
      if (!c.isMulti) break;
    }
    // If nothing selected and single-select defaults exist, seed first default.
    if (_selected.isEmpty && c.defaultIds.isNotEmpty) {
      _selected.add(c.defaultIds.first);
    }
    _search = TextEditingController();
    _custom = TextEditingController();
    // If initial is a custom id not in the list, put it in the custom field.
    if (c.allowCustom && _selected.isNotEmpty) {
      final ids = c.options.map((o) => o.id).toSet();
      final custom = _selected.where((id) => !ids.contains(id)).toList();
      if (custom.isNotEmpty) {
        _custom.text = custom.first;
      }
    }
  }

  @override
  void dispose() {
    _search.dispose();
    _custom.dispose();
    super.dispose();
  }

  List<PickerOption> get _filtered {
    final q = _query.trim().toLowerCase();
    if (q.isEmpty) return c.options;
    return c.options.where((o) {
      return o.id.toLowerCase().contains(q) ||
          o.displayLabel.toLowerCase().contains(q) ||
          o.description.toLowerCase().contains(q) ||
          o.group.toLowerCase().contains(q);
    }).toList();
  }

  void _toggle(PickerOption o) {
    if (!o.enabled) return;
    setState(() {
      if (c.isMulti) {
        if (_selected.contains(o.id)) {
          _selected.remove(o.id);
        } else {
          final max = c.maxSelect;
          if (max > 0 && _selected.length >= max) {
            // Drop oldest arbitrary — remove first then add.
            _selected.remove(_selected.first);
          }
          _selected.add(o.id);
        }
      } else {
        _selected
          ..clear()
          ..add(o.id);
        _custom.clear();
      }
    });
  }

  bool get _canConfirm {
    final ids = _resolvedIds();
    if (ids.length < c.minSelect) return false;
    if (!c.isMulti && c.maxSelect == 1 && ids.length > 1) return false;
    if (c.isMulti && c.maxSelect > 0 && ids.length > c.maxSelect) {
      return false;
    }
    return true;
  }

  List<String> _resolvedIds() {
    final out = <String>[];
    final seen = <String>{};
    for (final id in _selected) {
      if (id.isEmpty || seen.contains(id)) continue;
      seen.add(id);
      out.add(id);
    }
    if (c.allowCustom) {
      final custom = _custom.text.trim();
      if (custom.isNotEmpty && !seen.contains(custom)) {
        if (!c.isMulti) {
          return [custom];
        }
        out.add(custom);
      }
    }
    return out;
  }

  void _confirm() {
    if (!_canConfirm) return;
    Navigator.pop(context, PickerResult(_resolvedIds()));
  }

  void _clearSelection() {
    setState(() {
      _selected.clear();
      _custom.clear();
    });
  }

  @override
  Widget build(BuildContext context) {
    // The custom-value field and confirm row sit at the bottom of the sheet;
    // pad by the keyboard inset so they stay visible, and cap the sheet height
    // so padding + sheet never exceed what the modal route allows.
    final targetHeight = MediaQuery.sizeOf(context).height * 0.85;
    final bottomInset = MediaQuery.viewInsetsOf(context).bottom;

    return LayoutBuilder(
      builder: (context, constraints) {
        final height = (constraints.maxHeight - bottomInset).clamp(
          0.0,
          targetHeight,
        );
        return Padding(
          padding: EdgeInsets.only(bottom: bottomInset),
          child: SizedBox(height: height, child: _buildSheetBody(context)),
        );
      },
    );
  }

  Widget _buildSheetBody(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final filtered = _filtered;
    // Group filtered options.
    final groups = <String, List<PickerOption>>{};
    for (final o in filtered) {
      final g = o.group.isEmpty ? '' : o.group;
      groups.putIfAbsent(g, () => []).add(o);
    }
    final groupKeys = groups.keys.toList()
      ..sort((a, b) {
        if (a.isEmpty) return 1;
        if (b.isEmpty) return -1;
        return a.compareTo(b);
      });

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 8, 0),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  widget.title,
                  style: Theme.of(context).textTheme.titleLarge,
                ),
              ),
              if (c.source.label.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(right: 4),
                  child: Chip(
                    label: Text(c.source.label),
                    visualDensity: VisualDensity.compact,
                    materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                  ),
                ),
              IconButton(
                tooltip: 'Close',
                onPressed: () => Navigator.pop(context),
                icon: const Icon(Icons.close),
              ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
          child: TextField(
            controller: _search,
            decoration: const InputDecoration(
              prefixIcon: Icon(Icons.search),
              hintText: 'Search…',
              border: OutlineInputBorder(),
              isDense: true,
            ),
            onChanged: (v) => setState(() => _query = v),
          ),
        ),
        if (c.options.isEmpty)
          Expanded(
            child: Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Text(
                  c.allowCustom
                      ? 'No catalog entries. Enter a custom value below.'
                      : 'No options available.',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: scheme.onSurfaceVariant),
                ),
              ),
            ),
          )
        else
          Expanded(
            child: ListView.builder(
              itemCount: _listItemCount(groupKeys, groups),
              itemBuilder: (ctx, i) =>
                  _buildListItem(ctx, i, groupKeys, groups, scheme),
            ),
          ),
        if (c.allowCustom)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: TextField(
              controller: _custom,
              decoration: InputDecoration(
                labelText: c.isMulti
                    ? 'Custom value (added to selection)'
                    : 'Custom value',
                helperText: c.isMulti
                    ? null
                    : 'Overrides the list selection when non-empty',
                border: const OutlineInputBorder(),
                isDense: true,
              ),
              onChanged: (_) => setState(() {}),
            ),
          ),
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
            child: Row(
              children: [
                TextButton(
                  onPressed: _clearSelection,
                  child: const Text('Clear'),
                ),
                const Spacer(),
                TextButton(
                  onPressed: () => Navigator.pop(context),
                  child: const Text('Cancel'),
                ),
                const SizedBox(width: 8),
                FilledButton(
                  onPressed: _canConfirm ? _confirm : null,
                  child: Text(c.isMulti ? 'Done' : 'Select'),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  int _listItemCount(
    List<String> groupKeys,
    Map<String, List<PickerOption>> groups,
  ) {
    var n = 0;
    for (final g in groupKeys) {
      if (g.isNotEmpty) n++; // header
      n += groups[g]!.length;
    }
    return n;
  }

  Widget _buildListItem(
    BuildContext context,
    int index,
    List<String> groupKeys,
    Map<String, List<PickerOption>> groups,
    ColorScheme scheme,
  ) {
    var i = index;
    for (final g in groupKeys) {
      if (g.isNotEmpty) {
        if (i == 0) {
          return Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
            child: Text(
              g,
              style: Theme.of(
                context,
              ).textTheme.labelLarge?.copyWith(color: scheme.primary),
            ),
          );
        }
        i--;
      }
      final list = groups[g]!;
      if (i < list.length) {
        return _optionTile(list[i], scheme);
      }
      i -= list.length;
    }
    return const SizedBox.shrink();
  }

  Widget _optionTile(PickerOption o, ColorScheme scheme) {
    final selected = _selected.contains(o.id) && _custom.text.trim().isEmpty;
    final opacity = o.enabled ? 1.0 : 0.45;
    return Opacity(
      opacity: opacity,
      child: ListTile(
        enabled: o.enabled,
        leading: c.isMulti
            ? Checkbox(
                value: _selected.contains(o.id),
                onChanged: o.enabled ? (_) => _toggle(o) : null,
              )
            : Icon(
                selected ? Icons.radio_button_checked : Icons.radio_button_off,
                color: selected ? scheme.primary : null,
              ),
        title: Text(o.displayLabel),
        subtitle: o.description.isNotEmpty || o.id != o.displayLabel
            ? Text(
                [
                  if (o.id != o.displayLabel) o.id,
                  if (o.description.isNotEmpty) o.description,
                ].join(' · '),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              )
            : null,
        selected: selected,
        onTap: o.enabled ? () => _toggle(o) : null,
        onLongPress: o.enabled && !c.isMulti
            ? () {
                _toggle(o);
                _confirm();
              }
            : null,
      ),
    );
  }
}
