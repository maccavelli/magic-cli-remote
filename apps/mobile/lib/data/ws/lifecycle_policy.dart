import 'package:flutter/foundation.dart' show TargetPlatform;

import 'mcremote_client.dart';

/// True where a platform mechanism actually keeps the Dart process running
/// off-screen — Android, whose foreground service exists for exactly that.
/// On iOS the process is suspended shortly after backgrounding and its
/// timers never fire (MADR 0067 F3): background work gated on this is dead
/// code there at best, and a stale-callback burst on resume at worst.
bool platformKeepsProcessAliveInBackground(TargetPlatform platform) =>
    platform == TargetPlatform.android;

/// Whether backgrounding should park the socket (`disconnect(manual: false)`)
/// instead of leaving it — or its reconnect loop — armed off-screen.
///
/// On iOS this is unconditional while signed in (MADR 0067 D2): the OS
/// reclaims sockets from suspended apps and backoff timers never fire, so
/// the only honest states are "parked" and "foregrounded". On Android the
/// original semantics hold byte-for-byte: a live or in-flight socket is left
/// alone (resume then costs nothing), and with alerts on the foreground
/// service keeps the retry loop running deliberately.
bool shouldParkOnBackground(
  McConnectionState state, {
  required bool userLoggedOut,
  required bool notificationsEnabled,
  required TargetPlatform platform,
}) {
  if (userLoggedOut) return false;
  if (!platformKeepsProcessAliveInBackground(platform)) return true;
  // Android: only a parked-adjacent retry loop is worth stopping, and only
  // when no foreground service is going to hold the link for alerts.
  if (state != McConnectionState.reconnecting &&
      state != McConnectionState.error) {
    return false;
  }
  return !notificationsEnabled;
}

/// Whether a missing `vpn` connectivity signal is evidence that a mesh
/// session's interface just died (MADR 0063 D4, gated by 0068 P5/T2).
///
/// connectivity_plus never reports `vpn` on Apple platforms (it reports
/// `other`), so `!sawVpn` was *always* true there — every connectivity
/// blip on a mesh session fired the urgent 2s probe, which can tear down
/// a healthy session on a slow cellular leg. Absence of a signal the
/// platform never emits is not evidence: Apple platforms always answer
/// false and take the lenient probe path.
bool vpnSignalMeaningful(
  TargetPlatform platform, {
  required bool sawVpn,
  required bool onMesh,
}) {
  if (!onMesh) return false;
  if (platform == TargetPlatform.iOS || platform == TargetPlatform.macOS) {
    return false;
  }
  return !sawVpn;
}

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
