import 'dart:async';

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

/// One built row of the list: a group header or an option. Precomputed per
/// query so [ListView.itemBuilder] is O(1) — it used to walk the group map from
/// the start for every row, which is O(groups) each and 172 groups for an
/// OpenCode provider list.
sealed class _Row {
  const _Row();
}

class _HeaderRow extends _Row {
  const _HeaderRow(this.title);
  final String title;
}

class _OptionRow extends _Row {
  const _OptionRow(this.option);
  final PickerOption option;
}

class _OptionPickerSheetState extends State<_OptionPickerSheet> {
  late final Set<String> _selected;
  late final TextEditingController _search;
  late final TextEditingController _custom;
  String _query = '';
  Timer? _searchDebounce;
  late List<_Row> _rows;

  /// Search runs on every keystroke over a list that can be hundreds of rows,
  /// and each run rebuilds the sheet. Coalesce bursts of typing into one.
  static const _searchDebounceWindow = Duration(milliseconds: 120);

  PickerCatalog get c => widget.catalog;

  @override
  void initState() {
    super.initState();
    _rows = _buildRows('');
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
    _searchDebounce?.cancel();
    _search.dispose();
    _custom.dispose();
    super.dispose();
  }

  void _onSearchChanged(String v) {
    _searchDebounce?.cancel();
    _searchDebounce = Timer(_searchDebounceWindow, () {
      if (!mounted) return;
      setState(() {
        _query = v;
        _rows = _buildRows(v);
      });
    });
  }

  /// Flatten the catalog into headers + options for [query].
  ///
  /// Group order is the catalog's own, taken from where each group first
  /// appears. It is deliberately not sorted: the daemon now orders
  /// meaningfully — connected model providers first, models newest-first —
  /// and re-sorting alphabetically here threw that away and opened an OpenCode
  /// picker on a provider called `302ai` (MADR 0043 D11).
  List<_Row> _buildRows(String query) {
    final q = query.trim().toLowerCase();
    // Two passes: collect each group's members in catalog order, remembering
    // where the group first appeared, then emit. Grouping and order-preserving
    // in one pass would mean inserting into the middle of the list.
    final order = <String>[];
    final byGroup = <String, List<PickerOption>>{};
    final ungrouped = <PickerOption>[];
    for (final o in c.options) {
      if (q.isNotEmpty && !o.searchText.contains(q)) continue;
      if (o.group.isEmpty) {
        ungrouped.add(o);
        continue;
      }
      final members = byGroup.putIfAbsent(o.group, () {
        order.add(o.group);
        return <PickerOption>[];
      });
      members.add(o);
    }
    final rows = <_Row>[for (final o in ungrouped) _OptionRow(o)];
    for (final g in order) {
      rows.add(_HeaderRow(g));
      for (final o in byGroup[g]!) {
        rows.add(_OptionRow(o));
      }
    }
    return rows;
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
            onChanged: _onSearchChanged,
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
        else if (_rows.isEmpty)
          Expanded(
            child: Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Text(
                  'Nothing matches “$_query”.',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: scheme.onSurfaceVariant),
                ),
              ),
            ),
          )
        else
          Expanded(
            child: ListView.builder(
              itemCount: _rows.length,
              itemBuilder: (ctx, i) => switch (_rows[i]) {
                _HeaderRow(:final title) => Padding(
                  padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
                  child: Text(
                    title,
                    style: Theme.of(
                      ctx,
                    ).textTheme.labelLarge?.copyWith(color: scheme.primary),
                  ),
                ),
                _OptionRow(:final option) => _optionTile(option, scheme),
              },
            ),
          ),
        // Truncation is reported, never silent: a catalog quietly missing rows
        // reads as "my model does not exist" (MADR 0043 D4).
        if (c.truncated)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
            child: Text(
              'Showing the first ${c.options.length} — search, or pick a '
              'provider, to narrow the list.',
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
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

  /// Trailing badge for a row whose ranking would otherwise be invisible: a
  /// deprecated model is sorted last and an unconfigured provider is sorted
  /// after the connected ones, and in both cases the user needs to know *why*
  /// rather than just where it landed.
  Widget? _badge(PickerOption o, ColorScheme scheme) {
    final (text, fg) = switch (o) {
      _ when o.isDeprecated => ('deprecated', scheme.error),
      _ when o.connected == false => (
        'not configured',
        scheme.onSurfaceVariant,
      ),
      _ => (null, scheme.onSurfaceVariant),
    };
    if (text == null) return null;
    return Text(
      text,
      style: Theme.of(context).textTheme.labelSmall?.copyWith(color: fg),
    );
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
        trailing: _badge(o, scheme),
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
