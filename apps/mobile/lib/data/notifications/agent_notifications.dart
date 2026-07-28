import 'dart:convert';

/// Pure, platform-free core of the notification layer: what to notify about and
/// how to round-trip the tap payload. Kept separate from the plugin wrapper so
/// it can be unit-tested without a device.

/// The kinds of agent event we surface as a notification.
enum NotifKind { permission, question, turnComplete }

/// Actions a notification can carry / return.
enum NotifAction { allow, deny, open }

/// Data attached to a notification so a tap knows what to act on. Serialized
/// into the notification payload string.
class NotifPayload {
  const NotifPayload({
    required this.kind,
    required this.sessionId,
    this.permissionId,
    this.questionId,
    this.allowOptionId,
  });

  final NotifKind kind;
  final String sessionId;

  /// Present only for [NotifKind.permission] — the request to resolve.
  final String? permissionId;
  final String? questionId;

  /// The option id the "Allow" action should select (per-request; "Deny" just
  /// cancels, so it needs no id). Carried so a tap can resolve without the app
  /// re-fetching the request's options.
  final String? allowOptionId;

  String encode() => jsonEncode({
    'k': kind.name,
    's': sessionId,
    if (permissionId != null) 'p': permissionId,
    if (questionId != null) 'q': questionId,
    if (allowOptionId != null) 'a': allowOptionId,
  });

  /// Parse a payload string, or null if it is missing/malformed.
  static NotifPayload? decode(String? raw) {
    if (raw == null || raw.isEmpty) return null;
    try {
      final m = jsonDecode(raw);
      if (m is! Map) return null;
      final kindName = m['k'] as String?;
      final sessionId = m['s'] as String?;
      if (kindName == null || sessionId == null || sessionId.isEmpty) {
        return null;
      }
      final kind = NotifKind.values
          .where((k) => k.name == kindName)
          .firstOrNull;
      if (kind == null) return null;
      return NotifPayload(
        kind: kind,
        sessionId: sessionId,
        permissionId: m['p'] as String?,
        questionId: m['q'] as String?,
        allowOptionId: m['a'] as String?,
      );
    } catch (_) {
      return null;
    }
  }

  @override
  bool operator ==(Object other) =>
      other is NotifPayload &&
      other.kind == kind &&
      other.sessionId == sessionId &&
      other.permissionId == permissionId &&
      other.questionId == questionId &&
      other.allowOptionId == allowOptionId;

  @override
  int get hashCode =>
      Object.hash(kind, sessionId, permissionId, questionId, allowOptionId);
}

/// Decide whether a session event warrants a user-facing notification.
///
/// [watching] means the app is foregrounded AND the event's session is the one
/// currently on screen — there is no point pinging the user about something
/// they are already looking at. Everything else (backgrounded, or on a
/// different session) is worth a notification.
bool shouldNotify({required String eventType, required bool watching}) {
  if (watching) return false;
  return eventType == 'permission_request' ||
      eventType == 'question_request' ||
      eventType == 'turn_complete';
}

/// A stable, per-target notification id so a later update/cancel can address the
/// same notification (e.g. cancel a permission's notification once it resolves).
/// Derived from the session/permission identity rather than a running counter.
int notificationIdFor({
  required NotifKind kind,
  required String sessionId,
  String? requestId,
}) {
  var hash = 0x811c9dc5;
  for (final byte in utf8.encode(
    'v2:${kind.name}:$sessionId:${requestId ?? ''}',
  )) {
    hash ^= byte;
    hash = (hash * 0x01000193) & 0xffffffff;
  }
  return (hash & 0x7fffffff) | 1;
}
