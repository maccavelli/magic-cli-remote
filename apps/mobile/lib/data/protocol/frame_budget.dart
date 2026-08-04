import 'dart:convert';

import 'models.dart';

/// The daemon's WebSocket read limit. This is measured on the serialized UTF-8
/// envelope, never on the raw prompt or attachment bytes.
const int kMaxClientFrameBytes = 1 << 20;

/// Encodes the exact request shape written to the WebSocket sink.
///
/// [v] is the negotiated protocol version (MADR 0068 D1); the default keeps
/// every pre-negotiation frame — and every v1 connection — byte-identical.
/// `"v":1` and `"v":2` serialize to the same byte count, so the frame-budget
/// helpers below stay exact without threading the version through.
String encodeRequestEnvelope({
  required String id,
  required String type,
  Map<String, dynamic>? payload,
  String? token,
  int v = kProtocolV1,
}) => jsonEncode(
  Envelope(v: v, type: type, id: id, payload: payload, token: token).toJson(),
);

/// Returns the number of UTF-8 bytes in an exact serialized request envelope.
int encodedRequestBytes({
  required String id,
  required String type,
  Map<String, dynamic>? payload,
  String? token,
}) => utf8
    .encode(
      encodeRequestEnvelope(id: id, type: type, payload: payload, token: token),
    )
    .length;

/// Conservative prompt preflight using a UUID-shaped request id, whose length
/// matches the id generated when the request is sent.
int sessionPromptFrameBytes({
  required String sessionId,
  required String text,
  List<Map<String, dynamic>> attachments = const [],
}) {
  final payload = <String, dynamic>{'session_id': sessionId, 'text': text};
  if (attachments.isNotEmpty) payload['attachments'] = attachments;
  return encodedRequestBytes(
    id: '00000000-0000-0000-0000-000000000000',
    type: 'session.prompt',
    payload: payload,
  );
}
