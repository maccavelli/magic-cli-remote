import 'mcremote_client.dart';

/// Whether the app should attempt WebSocket reconnect when returning to
/// the foreground (screen unlock / app resume).
///
/// [hasCredentials] may be true when in-memory token exists **or** the client
/// is still paired (token may be reloaded from secure storage).
bool shouldReconnectOnResume(
  McConnectionState state, {
  required bool hasCredentials,
  required bool userLoggedOut,
}) {
  if (userLoggedOut || !hasCredentials) return false;
  switch (state) {
    case McConnectionState.connected:
    case McConnectionState.connecting:
    case McConnectionState.authenticating:
    case McConnectionState.reconnecting:
      return false;
    case McConnectionState.disconnected:
    case McConnectionState.error:
      return true;
  }
}
