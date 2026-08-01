import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../local/settings_store.dart';
import '../protocol/transport_policy.dart';
import 'relay_transport.dart';

/// Default per-probe budget. Both probes run in parallel, so this is also
/// roughly the wall clock the connect screen spends on "Checking transports…".
const kTransportProbeTimeout = Duration(milliseconds: 900);

/// Soft reachability probes behind an injectable seam (MADR 0062 D2).
///
/// A probe answers "could this transport plausibly carry a dial right now" —
/// nothing stronger. The relay edge can be up while the host is not registered
/// (`host_offline` on join), and a half-up tailnet can pass a TLS handshake
/// while the daemon is unreachable. Both are why a probe pass is not a promise
/// and a probe failure is not a ban: the DialEpisode fallback is what actually
/// recovers, and the "Try Mesh / Try Relay" action keys on *configured*, not on
/// these results.
///
/// Instance methods (not statics) so widget tests can inject deterministic
/// answers instead of dialling the network.
class TransportProbes {
  const TransportProbes();

  /// mcremote authority (mesh/LAN). See [probeMeshReachable].
  Future<bool> mesh(
    String hostInput, {
    Duration timeout = kTransportProbeTimeout,
  }) => probeMeshReachable(hostInput, timeout: timeout);

  /// mcrelay edge. See [probeRelayReachable].
  Future<bool> relay(
    String relayBase, {
    Duration timeout = kTransportProbeTimeout,
  }) => probeRelayReachable(relayBase, timeout: timeout);

  /// Probe both transports at once, skipping either that is not configured.
  ///
  /// Parallel because these are the user's latency: run serially, two 900ms
  /// budgets become 1.8s of staring at a spinner before the menu appears.
  Future<TransportAvailability> probe({
    required TransportAvailability configured,
    required String? host,
    required String? relayUrl,
    Duration timeout = kTransportProbeTimeout,
  }) async {
    final wantMesh =
        configured.meshConfigured && (host?.trim().isNotEmpty ?? false);
    final wantRelay =
        configured.relayConfigured && (relayUrl?.trim().isNotEmpty ?? false);
    final results = await Future.wait([
      wantMesh
          ? mesh(host!.trim(), timeout: timeout)
          : Future<bool>.value(false),
      wantRelay
          ? relay(relayUrl!.trim(), timeout: timeout)
          : Future<bool>.value(false),
    ]);
    return configured.copyWith(
      meshOperational: results[0],
      relayOperational: results[1],
    );
  }
}

/// Reachability probe for the mcremote authority (mesh/LAN).
///
/// MADR 0016 R7: prefer `/healthz` over bare TCP so an open port that is
/// not mcremote does not steal the path from relay. Falls back to a TLS
/// handshake (any cert) when healthz is unreachable, then TCP.
Future<bool> probeMeshReachable(
  String hostInput, {
  Duration timeout = kTransportProbeTimeout,
}) async {
  try {
    final ws = SettingsStore.normalizeWsUrl(hostInput);
    final u = Uri.parse(ws);
    final host = u.host;
    if (host.isEmpty) return false;
    final secure = u.scheme == 'wss' || u.scheme == 'https';

    // 1) Application-level healthz (best signal).
    try {
      final healthz = SettingsStore.healthzUrl(hostInput);
      final client = HttpClient()
        ..connectionTimeout = timeout
        ..badCertificateCallback = (_, _, _) => true;
      try {
        final req = await client.getUrl(Uri.parse(healthz)).timeout(timeout);
        final res = await req.close().timeout(timeout);
        final body = await res.transform(utf8.decoder).join();
        if (res.statusCode == 200 && body.contains('"ok"')) {
          return true;
        }
      } finally {
        client.close(force: true);
      }
    } catch (_) {
      // fall through
    }

    final port = u.hasPort ? u.port : (secure ? 443 : 80);
    final socket = await Socket.connect(host, port, timeout: timeout);
    if (!secure) {
      await socket.close();
      return true;
    }
    // 2) TLS handshake only (pin checked on the real connect).
    try {
      final tls = await SecureSocket.secure(
        socket,
        host: host,
        onBadCertificate: (_) => true,
      ).timeout(timeout);
      await tls.close();
      return true;
    } catch (_) {
      try {
        await socket.close();
      } catch (_) {}
      return false;
    }
  } catch (_) {
    return false;
  }
}

/// Reachability probe for the mcrelay edge: `GET /healthz` → `{"ok":true}`.
///
/// Certificate validation is deliberately bypassed. This probe carries no
/// secret and proves nothing about identity — the security boundary is the
/// *inner* mcremote TLS pin, which is untouched by anything decided here. A
/// pinned probe would instead fail closed on a routine relay certificate
/// rotation and hide a transport that works perfectly (MADR 0062 §1.2).
Future<bool> probeRelayReachable(
  String relayBase, {
  Duration timeout = kTransportProbeTimeout,
}) async {
  if (relayBase.trim().isEmpty) return false;
  final String url;
  try {
    url = RelayTransport.healthzUrl(relayBase);
  } catch (_) {
    return false;
  }
  final client = HttpClient()
    ..connectionTimeout = timeout
    ..badCertificateCallback = (_, _, _) => true;
  try {
    final req = await client.getUrl(Uri.parse(url)).timeout(timeout);
    final res = await req.close().timeout(timeout);
    final body = await res.transform(utf8.decoder).join().timeout(timeout);
    return res.statusCode == 200 && body.contains('"ok"');
  } catch (_) {
    return false;
  } finally {
    client.close(force: true);
  }
}
