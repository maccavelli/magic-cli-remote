import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../data/protocol/models.dart';
import '../../theme/celestial.dart';

/// Generic provider question form. State is deliberately widget-local so
/// secret values never enter Riverpod state, transcript history, or reconnect
/// snapshots.
class QuestionSheet extends StatefulWidget {
  const QuestionSheet({
    super.key,
    required this.title,
    required this.items,
    required this.onSubmit,
    required this.onCancel,
  });

  final String title;
  final List<QuestionItem> items;
  final ValueChanged<Map<String, List<String>>> onSubmit;
  final VoidCallback onCancel;

  @override
  State<QuestionSheet> createState() => _QuestionSheetState();
}

class _QuestionSheetState extends State<QuestionSheet> {
  late final List<Set<String>> _selections;
  late final List<TextEditingController> _custom;
  bool _resolved = false;

  @override
  void initState() {
    super.initState();
    _selections = List.generate(widget.items.length, (_) => <String>{});
    _custom = List.generate(
      widget.items.length,
      (_) => TextEditingController(),
    );
  }

  String _fieldId(int index) {
    final id = widget.items[index].id;
    return id.isEmpty ? '$index' : id;
  }

  bool _usesText(QuestionItem item) =>
      item.custom || item.secret || item.options.isEmpty;

  bool get _valid {
    if (_resolved) return false;
    for (var i = 0; i < widget.items.length; i++) {
      if (_selections[i].isEmpty &&
          (!_usesText(widget.items[i]) || _custom[i].text.trim().isEmpty)) {
        return false;
      }
    }
    return widget.items.isNotEmpty;
  }

  void _submit() {
    if (_resolved || !_valid) return;
    final answers = <String, List<String>>{};
    for (var i = 0; i < widget.items.length; i++) {
      final values = _selections[i].toList();
      final custom = _custom[i].text.trim();
      if (_usesText(widget.items[i]) && custom.isNotEmpty) values.add(custom);
      answers[_fieldId(i)] = values;
    }
    HapticFeedback.selectionClick();
    setState(() => _resolved = true);
    widget.onSubmit(answers);
    for (var i = 0; i < _custom.length; i++) {
      if (widget.items[i].secret) _custom[i].clear();
    }
  }

  void _cancel() {
    if (_resolved) return;
    setState(() => _resolved = true);
    for (var i = 0; i < _custom.length; i++) {
      if (widget.items[i].secret) _custom[i].clear();
    }
    widget.onCancel();
  }

  @override
  void dispose() {
    for (var i = 0; i < _custom.length; i++) {
      if (widget.items[i].secret) _custom[i].clear();
      _custom[i].dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tokens = celestialOf(context);
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.of(context).size.height * 0.9,
        ),
        child: SingleChildScrollView(
          key: const Key('question-sheet-scroll'),
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Center(
                  child: Container(
                    width: 40,
                    height: 4,
                    decoration: BoxDecoration(
                      color: tokens.gold,
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                ),
                const SizedBox(height: 14),
                Row(
                  children: [
                    Icon(Icons.help_outline, color: tokens.gold),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        widget.title,
                        style: theme.textTheme.titleLarge,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 16),
                for (var i = 0; i < widget.items.length; i++) ...[
                  _QuestionField(
                    index: i,
                    item: widget.items[i],
                    selections: _selections[i],
                    controller: _custom[i],
                    onChanged: () => setState(() {}),
                  ),
                  if (i < widget.items.length - 1) const Divider(height: 24),
                ],
                const SizedBox(height: 16),
                FilledButton(
                  key: const Key('question-submit'),
                  onPressed: _valid ? _submit : null,
                  child: const Text('Submit'),
                ),
                TextButton(
                  key: const Key('question-cancel'),
                  onPressed: _resolved ? null : _cancel,
                  child: const Text('Cancel / skip'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _QuestionField extends StatelessWidget {
  const _QuestionField({
    required this.index,
    required this.item,
    required this.selections,
    required this.controller,
    required this.onChanged,
  });

  final int index;
  final QuestionItem item;
  final Set<String> selections;
  final TextEditingController controller;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final usesText = item.custom || item.secret || item.options.isEmpty;
    return Column(
      key: Key('question-field-${item.id.isEmpty ? index : item.id}'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (item.header.isNotEmpty)
          Text(
            item.header,
            style: theme.textTheme.titleSmall?.copyWith(
              fontWeight: FontWeight.w600,
            ),
          ),
        if (item.text.isNotEmpty) ...[
          const SizedBox(height: 4),
          Text(item.text, style: theme.textTheme.bodyMedium),
        ],
        const SizedBox(height: 8),
        for (final option in item.options)
          CheckboxListTile(
            key: Key('question-option-$index-${option.optionId}'),
            dense: true,
            contentPadding: EdgeInsets.zero,
            value: selections.contains(
              option.optionId.isEmpty ? option.name : option.optionId,
            ),
            title: Text(option.name.isEmpty ? option.optionId : option.name),
            subtitle: option.description.isEmpty
                ? null
                : Text(option.description),
            onChanged: (selected) {
              final label = option.optionId.isEmpty
                  ? option.name
                  : option.optionId;
              if (!item.multiple) selections.clear();
              if (selected == true) {
                selections.add(label);
              } else {
                selections.remove(label);
              }
              onChanged();
            },
          ),
        if (usesText) ...[
          const SizedBox(height: 4),
          TextField(
            key: Key('question-text-$index'),
            controller: controller,
            obscureText: item.secret,
            enableSuggestions: !item.secret,
            autocorrect: !item.secret,
            decoration: InputDecoration(
              labelText: item.secret
                  ? 'Secret answer'
                  : item.custom
                  ? 'Other'
                  : 'Answer',
              isDense: true,
            ),
            onChanged: (_) => onChanged(),
          ),
        ],
      ],
    );
  }
}
