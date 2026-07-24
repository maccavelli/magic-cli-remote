import '../../data/protocol/models.dart';

/// Conservative permission-option classification. Substring matching is
/// dangerous here: `disallow`/`not_allowed` contain "allow" and would have
/// been styled as the prominent approve button. Unknown options render as
/// neutral outlined buttons instead.
bool isAllowOption(PermissionOption o) {
  final kind = (o.kind ?? '').toLowerCase();
  final id = o.optionId.toLowerCase();
  if (kind.startsWith('reject') ||
      kind.startsWith('deny') ||
      id.startsWith('disallow') ||
      id.startsWith('deny') ||
      id.startsWith('reject') ||
      id.startsWith('not_')) {
    return false;
  }
  if (kind == 'allow' ||
      kind == 'allow_once' ||
      kind == 'allow-once' ||
      kind == 'allow_always' ||
      kind == 'allow-always') {
    return true;
  }
  const ids = {
    'allow',
    'allow_once',
    'allow-once',
    'allow_always',
    'allow-always',
    'yes',
    'approve',
  };
  return ids.contains(id);
}

bool isAlwaysOption(PermissionOption o) {
  final kind = (o.kind ?? '').toLowerCase();
  final id = o.optionId.toLowerCase();
  return kind == 'allow_always' ||
      kind == 'allow-always' ||
      id == 'allow_always' ||
      id == 'allow-always' ||
      id == 'always';
}

/// Drop ids from [presented] that are no longer in [stillPending] (resolved),
/// mutating [presented] and returning the dropped ids. Still-pending ids are
/// always kept, so a resolved permission is forgotten — bounding the set for
/// the widget's lifetime — without ever re-presenting one that is still live.
Set<String> prunePresentedPermissionIds(
  Set<String> presented,
  Set<String> stillPending,
) {
  final dropped = presented.where((id) => !stillPending.contains(id)).toSet();
  presented.removeAll(dropped);
  return dropped;
}

/// Same as [prunePresentedPermissionIds] for multi-question forms.
Set<String> prunePresentedQuestionIds(
  Set<String> presented,
  Set<String> stillPending,
) => prunePresentedPermissionIds(presented, stillPending);
