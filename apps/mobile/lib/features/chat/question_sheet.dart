import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../data/protocol/models.dart';
import '../../theme/celestial.dart';

/// Generic provider question form. State is deliberately widget-local so
/// secret values never enter Riverpod state, transcript history, or reconnect
/// snapshots.
///
/// Fields may be typed, optional and defaulted (MADR 0191 D5). Each rule is
/// checked here before Submit, never more strictly than the daemon checks it,
/// so the phone cannot block an answer the daemon would accept.
class QuestionSheet extends StatefulWidget {
  const QuestionSheet({
    super.key,
    required this.title,
    required this.items,
    required this.onSubmit,
    required this.onCancel,
    this.allowDecline = false,
    this.onDecline,
    this.errorText,
    this.initialAnswers,
  });

  final String title;
  final List<QuestionItem> items;
  final ValueChanged<Map<String, List<String>>> onSubmit;
  final VoidCallback onCancel;

  /// Offer an explicit Decline beside cancel (MADR 0191 D3).
  final bool allowDecline;
  final VoidCallback? onDecline;

  /// The daemon's refusal of the previous answer, shown above Submit.
  final String? errorText;

  /// The previous answer, restored when the form reopens after a refusal.
  final Map<String, List<String>>? initialAnswers;

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
    // Restore the refused answer, or else prefill the field's default.
    for (var i = 0; i < widget.items.length; i++) {
      final item = widget.items[i];
      final initial = widget.initialAnswers?[_fieldId(i)] ?? item.defaults;
      final ids = {for (final o in item.options) _optionKey(o)};
      for (final value in initial) {
        if (ids.contains(value)) {
          _selections[i].add(value);
          if (!item.multiple) break;
        } else if (_usesText(item) && _custom[i].text.isEmpty) {
          _custom[i].text = value;
        }
      }
    }
  }

  String _fieldId(int index) {
    final id = widget.items[index].id;
    return id.isEmpty ? '$index' : id;
  }

  bool _usesText(QuestionItem item) =>
      item.custom || item.secret || item.options.isEmpty;

  /// Why field [i] cannot be submitted: null when it can, an empty string
  /// when it is merely unanswered, otherwise the rule it breaks.
  String? _fieldError(int i) {
    final item = widget.items[i];
    final text = _usesText(item) ? _custom[i].text.trim() : '';
    final selected = _selections[i];
    if (selected.isEmpty && text.isEmpty) return item.optional ? null : '';
    if (item.multiple) {
      final n = selected.length + (text.isEmpty ? 0 : 1);
      if (item.minItems != null && n < item.minItems!) {
        return 'Choose at least ${_choices(item.minItems!)}';
      }
      if (item.maxItems != null && n > item.maxItems!) {
        return 'Choose at most ${_choices(item.maxItems!)}';
      }
      return null;
    }
    if (text.isEmpty) return null;
    return _textError(item, text);
  }

  @override
  void dispose() {
    for (var i = 0; i < _custom.length; i++) {
      if (widget.items[i].secret) _custom[i].clear();
      _custom[i].dispose();
    }
    super.dispose();
  }

  bool get _valid {
    if (_resolved) return false;
    for (var i = 0; i < widget.items.length; i++) {
      if (_fieldError(i) != null) return false;
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
    _clearSecrets();
  }

  void _cancel() {
    if (_resolved) return;
    setState(() => _resolved = true);
    _clearSecrets();
    widget.onCancel();
  }

  void _decline() {
    if (_resolved) return;
    setState(() => _resolved = true);
    _clearSecrets();
    widget.onDecline?.call();
  }

  void _clearSecrets() {
    for (var i = 0; i < _custom.length; i++) {
      if (widget.items[i].secret) _custom[i].clear();
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tokens = celestialOf(context);
    final refusal = (widget.errorText ?? '').trim();
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
                    error: _fieldError(i),
                    onChanged: () => setState(() {}),
                  ),
                  if (i < widget.items.length - 1) const Divider(height: 24),
                ],
                if (refusal.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  Text(
                    refusal,
                    key: const Key('question-error'),
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.error,
                    ),
                  ),
                ],
                const SizedBox(height: 16),
                FilledButton(
                  key: const Key('question-submit'),
                  onPressed: _valid ? _submit : null,
                  child: const Text('Submit'),
                ),
                if (widget.allowDecline)
                  OutlinedButton(
                    key: const Key('question-decline'),
                    onPressed: _resolved ? null : _decline,
                    child: const Text('Decline'),
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

String _optionKey(PermissionOption o) =>
    o.optionId.isEmpty ? o.name : o.optionId;

String _choices(int n) => '$n choice${n == 1 ? '' : 's'}';

/// A bound as people write it: `10`, not `10.0`.
String _num(double v) =>
    v == v.roundToDouble() ? v.toInt().toString() : v.toString();

/// The rule a text answer breaks, or null. Each check is the daemon's, or
/// looser, never stricter (MADR 0191 D5).
String? _textError(QuestionItem item, String v) {
  if (item.isNumeric) {
    final num? n = item.kind == 'integer'
        ? int.tryParse(v)
        : double.tryParse(v);
    if (n == null) {
      return item.kind == 'integer'
          ? 'Must be a whole number'
          : 'Must be a number';
    }
    if (item.min != null && n < item.min!) {
      return 'Must be at least ${_num(item.min!)}';
    }
    if (item.max != null && n > item.max!) {
      return 'Must be at most ${_num(item.max!)}';
    }
    return null;
  }
  final length = v.runes.length;
  if (item.minLength != null && length < item.minLength!) {
    return 'Must be at least ${item.minLength} characters';
  }
  if (item.maxLength != null && length > item.maxLength!) {
    return 'Must be at most ${item.maxLength} characters';
  }
  if (!_matchesFormat(item.format, v)) {
    return 'Must be a valid ${item.format}';
  }
  if (item.pattern.isNotEmpty) {
    try {
      if (!RegExp(item.pattern).hasMatch(v)) {
        return 'Does not match the required pattern';
      }
    } on FormatException {
      // A pattern Dart cannot read is left to the daemon.
    }
  }
  return null;
}

bool _matchesFormat(String format, String v) {
  switch (format) {
    case 'email':
      final at = v.indexOf('@');
      return at > 0 && at < v.length - 1 && !v.contains(' ');
    case 'uri':
      return Uri.tryParse(v)?.hasScheme ?? false;
    case 'date':
      return RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(v) &&
          DateTime.tryParse(v) != null;
    case 'date-time':
      return v.contains('T') && DateTime.tryParse(v) != null;
  }
  return true;
}

/// What the field asks for, shown under it before anything is typed.
String? _hint(QuestionItem item) {
  if (item.isNumeric) {
    final lo = item.min, hi = item.max;
    if (lo != null && hi != null) return '${_num(lo)} to ${_num(hi)}';
    if (lo != null) return 'At least ${_num(lo)}';
    if (hi != null) return 'Up to ${_num(hi)}';
    return item.kind == 'integer' ? 'A whole number' : 'A number';
  }
  final parts = <String>[
    if (item.format.isNotEmpty) item.format,
    if (item.minLength != null && item.maxLength != null)
      '${item.minLength}–${item.maxLength} characters'
    else if (item.maxLength != null)
      'Up to ${item.maxLength} characters'
    else if (item.minLength != null)
      'At least ${item.minLength} characters',
  ];
  return parts.isEmpty ? null : parts.join(' · ');
}

class _QuestionField extends StatelessWidget {
  const _QuestionField({
    required this.index,
    required this.item,
    required this.selections,
    required this.controller,
    required this.error,
    required this.onChanged,
  });

  final int index;
  final QuestionItem item;
  final Set<String> selections;
  final TextEditingController controller;

  /// The rule the current answer breaks; null or empty shows nothing.
  final String? error;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final usesText = item.custom || item.secret || item.options.isEmpty;
    final header = item.optional
        ? (item.header.isEmpty ? '(optional)' : '${item.header} (optional)')
        : item.header;
    final message = (error ?? '').isEmpty ? null : error;
    return Column(
      key: Key('question-field-${item.id.isEmpty ? index : item.id}'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (header.isNotEmpty)
          Text(
            header,
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
            value: selections.contains(_optionKey(option)),
            title: Text(option.name.isEmpty ? option.optionId : option.name),
            subtitle: option.description.isEmpty
                ? null
                : Text(option.description),
            onChanged: (selected) {
              final label = _optionKey(option);
              if (!item.multiple) selections.clear();
              if (selected == true) {
                selections.add(label);
              } else {
                selections.remove(label);
              }
              onChanged();
            },
          ),
        if (!usesText && message != null)
          Text(
            message,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.error,
            ),
          ),
        if (usesText) ...[
          const SizedBox(height: 4),
          TextField(
            key: Key('question-text-$index'),
            controller: controller,
            obscureText: item.secret,
            enableSuggestions: !item.secret,
            autocorrect: !item.secret,
            keyboardType: item.isNumeric
                ? TextInputType.numberWithOptions(
                    decimal: item.kind == 'number',
                    signed: item.min == null || item.min! < 0,
                  )
                : null,
            decoration: InputDecoration(
              labelText: item.secret
                  ? 'Secret answer'
                  : item.custom
                  ? 'Other'
                  : 'Answer',
              helperText: _hint(item),
              errorText: message,
              isDense: true,
            ),
            onChanged: (_) => onChanged(),
          ),
        ],
      ],
    );
  }
}
