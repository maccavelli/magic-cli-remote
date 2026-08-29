import 'package:flutter/material.dart';

import '../../../data/protocol/picker.dart';
import '../../widgets/option_picker_sheet.dart';

/// The one card idiom for every session control (MADR 0123 D5).
///
/// Before this, four adjacent controls used three different surfaces: the
/// permissions and collaboration switchers were `PopupMenuButton`s, the
/// thinking level was a `SimpleDialog`, and agent settings was a bottom sheet.
/// Nothing made them consistent by accident, so consistency is built here and
/// the controls are thin callers.
///
/// Built on [PickerSheetLayout] and [PickerSheetHeader] rather than a new
/// design: the model picker already uses them, and a fourth bespoke surface is
/// exactly the problem this record set out to remove.
class SessionControlCard extends StatelessWidget {
  const SessionControlCard({
    super.key,
    required this.title,
    required this.options,
    this.banner,
    this.enabled = true,
  });

  /// Card title, e.g. "Permissions".
  final String title;

  /// Selectable rows, in the order the daemon advertised them. Order is never
  /// reinterpreted here — see [SessionControlOption].
  final List<SessionControlOption> options;

  /// Optional explanation shown above the options. This is how a control says
  /// what it *cannot* do, instead of failing a tap and explaining afterwards
  /// (MADR 0123 D8).
  final SessionControlBanner? banner;

  /// When false the rows render but do not accept taps. The list is still
  /// shown: a user who cannot change the value still deserves to see what the
  /// values are.
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    return PickerSheetLayout(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          PickerSheetHeader(
            title: title,
            // No provenance chip: unlike the model catalog, these options come
            // from the live session and there is no second source to name.
            source: PickerSource.unknown,
            onClose: () => Navigator.of(context).maybePop(),
          ),
          if (banner != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
              child: _Banner(banner: banner!),
            ),
          const SizedBox(height: 4),
          Flexible(
            child: ListView.builder(
              shrinkWrap: true,
              itemCount: options.length,
              itemBuilder: (context, i) {
                final o = options[i];
                return _OptionRow(option: o, enabled: enabled);
              },
            ),
          ),
          const SizedBox(height: 8),
        ],
      ),
    );
  }
}

/// One selectable row.
class SessionControlOption {
  const SessionControlOption({
    required this.id,
    required this.label,
    this.description = '',
    this.selected = false,
    this.dangerous = false,
    this.onSelected,
  });

  final String id;
  final String label;
  final String description;

  /// Whether this row is the session's current value.
  ///
  /// Resolved by the caller, never inferred from list position: MADR 0047 D4
  /// forbids inventing a selection from order alone, and a card that defaulted
  /// to `options.first` would silently claim a mode the session is not in.
  final bool selected;

  /// Daemon-declared, never guessed from the id (MADR 0049). Tints the row so
  /// a mode that answers permissions for the user reads as different in kind.
  final bool dangerous;

  final Future<void> Function()? onSelected;
}

/// An explanation shown above the options.
class SessionControlBanner {
  const SessionControlBanner({
    required this.message,
    this.severity = SessionControlSeverity.info,
  });

  final String message;
  final SessionControlSeverity severity;
}

enum SessionControlSeverity { info, warning }

class _Banner extends StatelessWidget {
  const _Banner({required this.banner});

  final SessionControlBanner banner;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final warning = banner.severity == SessionControlSeverity.warning;
    return Container(
      key: const ValueKey('session-control-banner'),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: warning ? scheme.errorContainer : scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            warning ? Icons.lock_outline : Icons.info_outline,
            size: 18,
            color: warning ? scheme.onErrorContainer : scheme.onSurfaceVariant,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              banner.message,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: warning
                    ? scheme.onErrorContainer
                    : scheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _OptionRow extends StatelessWidget {
  const _OptionRow({required this.option, required this.enabled});

  final SessionControlOption option;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return ListTile(
      key: ValueKey('session-control-option-${option.id}'),
      enabled: enabled && option.onSelected != null,
      leading: Icon(
        option.selected ? Icons.radio_button_checked : Icons.radio_button_off,
        color: option.selected ? scheme.primary : scheme.onSurfaceVariant,
      ),
      title: Text(option.label),
      subtitle: option.description.isEmpty ? null : Text(option.description),
      trailing: option.dangerous
          ? Icon(Icons.bolt, size: 18, color: scheme.error)
          : null,
      onTap: !enabled || option.onSelected == null
          ? null
          : () async {
              // Pop first so the card is gone before any confirmation dialog
              // or error notice appears; leaving it up stacks two sheets and
              // the user cannot tell which one owns the message.
              final navigator = Navigator.of(context);
              final callback = option.onSelected!;
              navigator.maybePop();
              await callback();
            },
    );
  }
}
