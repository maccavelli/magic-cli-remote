import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart'
    show ValueNotifier, debugPrint, visibleForTesting;
import 'package:http/io_client.dart';
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../chat/chat_models.dart' show kHistoryFetchLimit;
import '../local/settings_store.dart';
import '../protocol/models.dart';
import '../protocol/frame_budget.dart';
import '../protocol/pair_uri.dart';
import '../protocol/picker.dart';
import 'client_identity.dart';
import 'mc_exception.dart';
import 'relay_transport.dart';

/// Resources created by one connection attempt. They remain local until the
/// attempt wins its epoch; a stale attempt may therefore only close its own
/// channel/client/relay and can never tear down the newer connection.
typedef _OpenedSocket = ({
  WebSocketChannel channel,
  HttpClient httpClient,
  RelayTransport? relay,
});

/// Enforces the certificate acceptance rule for one connection attempt.
///
/// Both rules in ADR 0004 are the same [_accept] callback over a different
/// [SecurityContext], because `badCertificateCallback` fires *only* when
/// platform validation has already failed:
///
/// * [TlsMode.selfsigned] — `withTrustedRoots: false`, so no chain can ever
///   validate and every certificate is routed through [_accept]. Fingerprint
///   only. Relying on the callback alone with the default context would
///   silently skip the pin check for any chain the platform already trusts.
/// * [TlsMode.letsencrypt] — the default context, so a valid ACME chain never
///   reaches [_accept] and is accepted, while the daemon's self-signed
///   *fallback* does reach it and is accepted only on a fingerprint match.
///   Chain **or** pin, and a stale pin is never load-bearing across renewal.
///
/// The `or` never widens into "accept anything": a certificate that neither
/// chains nor matches still fails, permanently, as [McException] `cert_mismatch`.
class CertPinner {
  CertPinner(
    this.pinnedFingerprint, {
    this.mode = TlsMode.fallback,
    this.identity,
    @visibleForTesting this.trustedRootsPem,
  });

  /// Canonical base64url SHA-256, or null for an unpinned connection.
  final String? pinnedFingerprint;

  /// Which acceptance rule to apply. See the class doc.
  final TlsMode mode;

  /// The device's client certificate + key (ADR 0005), presented for TLS
  /// client authentication on this same connection. Null before an identity
  /// exists (e.g. the low-level pinning tests, which do not exercise client
  /// auth) — the connection then presents no client certificate.
  final ClientIdentity? identity;

  /// Test-only extra trust anchors, added to this pinner's own
  /// [SecurityContext] rather than the process-wide
  /// [SecurityContext.defaultContext]. Lets a test simulate a publicly-trusted
  /// chain in isolation, now that the client certificate forces every mode onto
  /// an explicit context.
  final List<int>? trustedRootsPem;

  /// Set when a certificate was rejected specifically because it did not match
  /// the pin, which must be reported differently from a generic TLS failure.
  bool mismatched = false;

  /// The fingerprint actually presented, for diagnostics.
  String? observed;

  bool get isPinned => pinnedFingerprint != null;

  /// SHA-256 of the DER encoding — the same bytes the daemon hashes when it
  /// prints the fingerprint into the pair QR.
  static String fingerprintOf(X509Certificate cert) {
    return base64Url.encode(sha256.convert(cert.der).bytes).replaceAll('=', '');
  }

  bool _accept(X509Certificate cert, String host, int port) {
    final got = fingerprintOf(cert);
    observed = got;
    if (got == pinnedFingerprint) return true;
    mismatched = true;
    return false;
  }

  HttpClient newHttpClient() {
    // The client certificate (ADR 0005) and the server pin ride the *same*
    // SecurityContext — a second HttpClient would not present the certificate
    // on this socket. A client certificate cannot be attached to
    // SecurityContext.defaultContext safely, so *both* modes use an explicit
    // context rather than a bare HttpClient().
    //
    // Trust roots follow the mode: letsencrypt validates a public chain (and
    // consults the pin only when that fails), selfsigned trusts no roots and
    // leans entirely on the pin. This is the only difference between the two
    // rules; the pin check itself is shared.
    final ctx = SecurityContext(withTrustedRoots: mode == TlsMode.letsencrypt);
    final roots = trustedRootsPem;
    if (roots != null) {
      ctx.setTrustedCertificatesBytes(roots);
    }
    final id = identity;
    if (id != null) {
      ctx.useCertificateChainBytes(utf8.encode(id.certPem));
      ctx.usePrivateKeyBytes(utf8.encode(id.keyPem));
    }
    final client = HttpClient(context: ctx);
    if (pinnedFingerprint != null) {
      // badCertificateCallback fires only when platform validation has already
      // failed. With no pin the mode's trust roots are the whole decision, and
      // a validation failure is a fail-closed rejection — a self-signed daemon
      // with no pin fails here, which is correct: the user re-scans a QR that
      // carries the fingerprint.
      client.badCertificateCallback = _accept;
    }
    return client;
  }

  /// Maps a connection failure onto a typed error, distinguishing a pin
  /// mismatch (permanent, do not retry, do not fall back) from a transport
  /// failure (retryable).
  Object translate(Object error, String url) {
    if (mismatched) {
      return McException(
        'The host is presenting a different TLS certificate than the one this '
        'device paired with (expected sha256:$pinnedFingerprint, got '
        'sha256:${observed ?? '?'}).\n\n'
        'This is expected if you rebuilt the host or reset its data directory — '
        're-pair to trust the new certificate. If you did neither, someone may '
        'be impersonating the host: do NOT re-pair until you can confirm the '
        'fingerprint on the host itself (`mcremote pair` prints it).',
        code: 'cert_mismatch',
        permanent: true,
      );
    }
    if (error is HandshakeException || error is CertificateException) {
      return McException(
        isPinned
            ? 'TLS handshake failed for $url: $error'
            // Unpinned means the host was expected to present a publicly
            // trusted certificate. Both causes are plausible and the client
            // cannot tell them apart, so name both rather than assuming the
            // self-signed one — a Let's Encrypt host never puts fp= in its QR,
            // so telling that user to "re-scan for the fingerprint" sends them
            // after something that does not exist.
            : 'TLS handshake failed for $url: $error. Either the host\'s '
                  'certificate is expired or not publicly trusted, or it is a '
                  'self-signed daemon — in that case re-pair from a QR generated '
                  'by `mcremote pair`, which carries the fingerprint.',
        code: isPinned ? 'tls_failed' : 'cert_unpinned',
        permanent: !isPinned,
      );
    }
    return error;
  }
}

enum McConnectionState {
  disconnected,
  connecting,
  authenticating,
  connected,
  reconnecting,
  error,
}

/// WebSocket client for mcremote.v1.
class McremoteClient {
  McremoteClient({
    SettingsStore? settings,
    @visibleForTesting this.afterSocketOpen,
  }) : _settings = settings ?? SettingsStore();

  /// Used only to persist/restore the pinned certificate fingerprint, so a
  /// reconnect after process death still pins. Credentials continue to flow in
  /// from the UI layer.
  final SettingsStore _settings;

  /// Test seam for interleaving attempts after a local socket exists but
  /// before it is adopted into shared connection state.
  @visibleForTesting
  final Future<void> Function()? afterSocketOpen;

  final _uuid = const Uuid();
  WebSocketChannel? _channel;
  HttpClient? _httpClient;

  /// Active mcrelay outer hop + loopback bridge (MADR 0015 E3), if any.
  RelayTransport? _relayTransport;
  String? _relayUrl;
  String? _relayHostId;
  String? _relayAuthority;

  StreamSubscription? _sub;
  Timer? _pingTimer;
  Timer? _reconnectTimer;

  final _events = StreamController<SessionEvent>.broadcast();
  final _connection = StreamController<McConnectionState>.broadcast();
  final _pending = <String, Completer<Envelope>>{};

  McConnectionState _state = McConnectionState.disconnected;
  String? lastError;
  String? lastErrorCode;
  String? deviceId;
  String? deviceName;

  /// The daemon user's home directory, reported on auth — the default working
  /// directory for new sessions. Null until the first successful auth.
  String? hostHomeDir;
  String? wsUrl;

  String? _lastHostInput;

  /// Notifies when [lastHostInput] changes so UI (e.g. sessions banner) can
  /// rebuild without waiting for a connection-state transition.
  final ValueNotifier<String?> hostInputListenable = ValueNotifier<String?>(
    null,
  );

  String? _lastToken;

  /// Canonical base64url SHA-256 of the certificate this device paired with,
  /// and the identity it was pinned for — the daemon's device id once known,
  /// otherwise the host authority (see [_pinIdentity]).
  String? _pinnedFingerprint;
  String? _pinnedFor;

  /// The acceptance rule the current pin was taken under. Resolved and reset
  /// in lockstep with [_pinnedFingerprint] — a pin without its mode would be
  /// applied under the wrong rule.
  TlsMode _tlsMode = TlsMode.fallback;

  bool _autoReconnect = true;
  bool _manualDisconnect = false;
  bool _userLoggedOut = false;

  /// Monotonic connection-attempt epoch. Every connect/pair attempt claims a
  /// new epoch; after each await it checks it still owns the latest one and
  /// abandons itself otherwise. This is what prevents two interleaved
  /// attempts from stacking listeners / leaking sockets, and what makes
  /// sign-out actually cancel an in-flight attempt.
  int _connectEpoch = 0;

  bool _staleAttempt(int epoch) =>
      epoch != _connectEpoch || _userLoggedOut || _manualDisconnect;

  /// True after a successful pair/auth until the user explicitly signs out.
  /// Survives transient socket drops (screen lock, mesh blip).
  bool _paired = false;
  int _reconnectAttempt = 0;
  bool _reconnectInFlight = false;

  /// Consecutive handshake-level failures (host reachable but auth times out
  /// or a pinned TLS handshake keeps failing). Unlike plain network failures —
  /// which retry until sign-out — these indicate a wedged or misconfigured
  /// host that blind retries will not fix; after [_maxHandshakeFailures] the
  /// loop parks in [McConnectionState.error]. Auto-reconnect stays armed, so
  /// app resume, a connectivity change, or Retry-now re-enters the loop with
  /// a fresh count.
  int _handshakeFailures = 0;
  static const int _maxHandshakeFailures = 6;

  /// When true, socket onDone/onError must not schedule another reconnect
  /// (we are tearing down intentionally to open a new socket).
  bool _suppressReconnect = false;
  int _missedPings = 0;

  Stream<SessionEvent> get events => _events.stream;
  Stream<McConnectionState> get connectionStates => _connection.stream;
  McConnectionState get state => _state;
  String? get lastHostInput => _lastHostInput;

  void _setLastHostInput(String? value) {
    final next = value?.trim();
    final normalized = (next == null || next.isEmpty) ? null : next;
    _lastHostInput = normalized;
    if (hostInputListenable.value != normalized) {
      hostInputListenable.value = normalized;
    }
  }

  bool get userLoggedOut => _userLoggedOut;

  /// Paired to a host with a durable device token (until explicit sign-out).
  bool get isPaired => _paired && !_userLoggedOut;

  /// Host + token present in memory and user has not explicitly logged out.
  bool get hasCredentials =>
      !_userLoggedOut &&
      (_lastHostInput?.isNotEmpty ?? false) &&
      (_lastToken?.isNotEmpty ?? false);

  /// Stay on the sessions UI (paired, even if the socket is briefly down).
  bool get shouldStayInApp => isPaired;

  /// The fast reconnect loop exhausted retryable handshake attempts. A
  /// background coordinator may make a deliberately slow maintenance retry.
  bool get reconnectParked =>
      _state == McConnectionState.error &&
      _handshakeFailures >= _maxHandshakeFailures &&
      hasCredentials &&
      !_manualDisconnect &&
      !_userLoggedOut;

  void _setState(McConnectionState s) {
    _state = s;
    if (!_connection.isClosed) _connection.add(s);
  }

  /// Drop in-memory token (and optionally host). Does not touch secure storage.
  void clearMemoryCredentials({bool host = false}) {
    _lastToken = null;
    if (host) _setLastHostInput(null);
    lastErrorCode = null;
  }

  /// The fingerprint currently pinned in memory, if any (diagnostics/tests).
  String? get pinnedFingerprint => _pinnedFingerprint;

  /// The acceptance rule in force for the current host (diagnostics/tests).
  TlsMode get tlsMode => _tlsMode;

  /// Resolve the certificate pin and acceptance rule for [hostInput].
  ///
  /// Precedence: an [explicit] fingerprint (fresh from a QR) > one carried in
  /// the host input's `#fp=` fragment > the in-memory pin for this identity >
  /// the pin persisted in secure storage. A newly supplied fingerprint is
  /// written through so reconnects and process-death recovery keep pinning.
  /// [explicitMode] follows the same precedence, and is stored with the pin.
  ///
  /// Keyed on [deviceId] where known, so a host that comes back on a new
  /// tailnet IP keeps its pin. A pin that is *present and mismatching* still
  /// fails hard in [CertPinner] under either mode; only an *absent* pin falls
  /// through.
  Future<String?> _resolvePin(
    String hostInput, [
    String? explicit,
    TlsMode? explicitMode,
  ]) async {
    final identity = _pinIdentity(hostInput);
    if (_pinnedFor != null && _pinnedFor != identity) {
      // Different daemon: a pin from the previous host is meaningless here,
      // and so is the rule it was taken under.
      _pinnedFingerprint = null;
      _pinnedFor = null;
      _tlsMode = TlsMode.fallback;
    }

    final supplied = explicit ?? SettingsStore.fingerprintFrom(hostInput);
    final suppliedMode = explicitMode ?? SettingsStore.tlsModeFrom(hostInput);
    if (supplied != null) {
      final canonical = normalizeFingerprint(supplied);
      if (canonical != null) {
        final mode = suppliedMode ?? TlsMode.fallback;
        _pinnedFingerprint = canonical;
        _pinnedFor = identity;
        _tlsMode = mode;
        try {
          await _settings.setFingerprint(
            hostInput,
            canonical,
            deviceId: deviceId,
            mode: mode,
          );
        } catch (e) {
          // Losing the persisted copy costs a re-pair after process death,
          // but must not block the connection that is working right now.
          debugPrint('McremoteClient: could not persist cert pin: $e');
        }
        return canonical;
      }
    }

    // No pin in hand: a mode from the QR still selects the rule, so an
    // LE-mode host with no fingerprint keeps doing plain chain validation.
    if (suppliedMode != null) _tlsMode = suppliedMode;

    if (_pinnedFingerprint != null) return _pinnedFingerprint;
    try {
      final stored = await _settings.getPinnedCert(
        hostInput,
        deviceId: deviceId,
        // With no device id in hand the persisted one may still be the right
        // answer — a daemon that churned onto a new tailnet address keeps both
        // its identity and its certificate. Presenting the *stored* token is
        // what makes that assumption safe: it is issued by, and only accepted
        // by, the daemon the persisted identity names. A hand-entered host with
        // some other token gets no such vouching (MADR 0046 H-B).
        fallbackToPersistedIdentity:
            deviceId == null && await _tokenIsPersisted(),
      );
      if (stored != null) {
        _pinnedFingerprint = stored.fingerprint;
        _pinnedFor = identity;
        if (suppliedMode == null) _tlsMode = stored.mode;
      }
    } catch (e) {
      debugPrint('McremoteClient: could not read cert pin: $e');
    }
    return _pinnedFingerprint;
  }

  /// Whether the credential about to be presented is the one storage holds, so
  /// the daemon that accepts it is necessarily the persisted identity.
  Future<bool> _tokenIsPersisted() async {
    final token = _lastToken;
    if (token == null || token.isEmpty) return false;
    try {
      return await _settings.getToken() == token;
    } catch (_) {
      // No readable token means no proof; fail closed to host-scoped pinning.
      return false;
    }
  }

  /// What the in-memory pin is scoped to: the daemon's device id once it has
  /// issued one, otherwise the dialled authority.
  String _pinIdentity(String hostInput) {
    final id = deviceId;
    return (id != null && id.isNotEmpty) ? 'id:$id' : _authorityOf(hostInput);
  }

  static String _authorityOf(String hostInput) {
    try {
      final ep = SettingsStore.parseEndpoint(hostInput);
      return '${ep.host}:${ep.port}';
    } catch (_) {
      return hostInput.trim();
    }
  }

  /// The client identity, loaded or generated on first use. Generating it here
  /// — before any socket, including the pairing socket — guarantees the daemon
  /// captures a client certificate at `pair.claim`, which is what enrols the
  /// key. Caches the *future* so concurrent first uses (healthz racing
  /// connect) cannot each mint a key and enrol one that storage then loses.
  Future<ClientIdentity>? _identityFuture;
  Future<ClientIdentity> _ensureIdentity() {
    final existing = _identityFuture;
    if (existing != null) return existing;
    final created = ClientIdentity.loadOrCreate(_settings);
    _identityFuture = created;
    return created.catchError((Object error, StackTrace stack) {
      if (identical(_identityFuture, created)) _identityFuture = null;
      throw error;
    });
  }

  /// Open a pinned WebSocket. Throws a typed [McException] on a pin mismatch.
  /// Returns the socket AND its dedicated HttpClient; the caller assigns both
  /// to fields only after confirming it still owns the connect epoch.
  ///
  /// When a relay is configured and the direct host is not reachable, opens
  /// an outer hop to mcrelay, joins, then dials mcremote TLS through a
  /// loopback bridge (inner hop — pin + client key still apply to mcremote).
  Future<_OpenedSocket> _openSocket(
    String url,
    String? pin, {
    String? hostInput,
    String? relayUrl,
    String? relayHostId,
  }) async {
    final useRelay = await _shouldUseRelay(
      hostInput ?? _lastHostInput,
      relayUrl: relayUrl,
      relayHostId: relayHostId,
    );
    if (useRelay) {
      return _openSocketViaRelay(
        url,
        pin,
        relayUrl: relayUrl,
        relayHostId: relayHostId,
      );
    }
    return _openSocketDirect(url, pin);
  }

  Future<_OpenedSocket> _openSocketDirect(String url, String? pin) async {
    final identity = await _ensureIdentity();
    final pinner = CertPinner(pin, mode: _tlsMode, identity: identity);
    final httpClient = pinner.newHttpClient();
    WebSocketChannel? channel;
    try {
      final next = IOWebSocketChannel.connect(
        Uri.parse(url),
        customClient: httpClient,
      );
      channel = next;
      await next.ready.timeout(const Duration(seconds: 8));
      return (channel: next, httpClient: httpClient, relay: null);
    } catch (e) {
      _abandonFailedDial(channel, httpClient);
      throw pinner.translate(e, url);
    }
  }

  /// Release a dial that never became a connection.
  ///
  /// Order matters: closing the [HttpClient] aborts the in-flight connect and
  /// is what resolves the channel's futures. Awaiting the sink first would
  /// wait forever — a channel whose `ready` failed never completes
  /// `sink.close()`, because the adapter only wires its controller in the
  /// connect-success branch. That await is what wedged `connect` and killed
  /// the reconnect loop (MADR 0046 H-A), so the close is bounded and
  /// deliberately not awaited.
  void _abandonFailedDial(WebSocketChannel? channel, HttpClient httpClient) {
    httpClient.close(force: true);
    if (channel == null) return;
    unawaited(
      channel.sink
          .close()
          .timeout(const Duration(seconds: 2))
          .catchError((_) {}),
    );
  }

  Future<_OpenedSocket> _openSocketViaRelay(
    String url,
    String? pin, {
    String? relayUrl,
    String? relayHostId,
  }) async {
    final effectiveRelayUrl = relayUrl?.trim() ?? _relayUrl?.trim() ?? '';
    final hostId = relayHostId?.trim() ?? _relayHostId?.trim() ?? '';
    if (effectiveRelayUrl.isEmpty || hostId.isEmpty) {
      throw McException(
        'relay path selected but relay url/host_id missing',
        code: 'relay_misconfigured',
        permanent: true,
      );
    }
    final transport = await RelayTransport.open(
      relayBase: effectiveRelayUrl,
      hostId: hostId,
    );
    final ClientIdentity identity;
    try {
      identity = await _ensureIdentity();
    } catch (_) {
      await transport.close();
      rethrow;
    }
    final pinner = CertPinner(pin, mode: _tlsMode, identity: identity);
    final httpClient = pinner.newHttpClient();
    // Dial loopback for the TCP hop; URL keeps the real host for SNI + pin.
    httpClient.connectionFactory =
        (Uri uri, String? proxyHost, int? proxyPort) {
          final future = Socket.connect(
            InternetAddress.loopbackIPv4,
            transport.localPort,
            timeout: const Duration(seconds: 8),
          );
          return Future.value(
            ConnectionTask.fromSocket(future, () {
              // Cancel is best-effort; Socket.connect has no cancel handle.
            }),
          );
        };

    WebSocketChannel? channel;
    try {
      final next = IOWebSocketChannel.connect(
        Uri.parse(url),
        customClient: httpClient,
      );
      channel = next;
      await next.ready.timeout(const Duration(seconds: 20));
      return (channel: next, httpClient: httpClient, relay: transport);
    } catch (e) {
      _abandonFailedDial(channel, httpClient);
      // The relay bridge owns a loopback ServerSocket, so it is still awaited:
      // its close completes on its own and must finish before the port is
      // considered released.
      try {
        await transport.close();
      } catch (_) {}
      throw pinner.translate(e, url);
    }
  }

  Future<void> _closeOpenedSocket(_OpenedSocket opened) async {
    try {
      await opened.channel.sink.close().timeout(const Duration(seconds: 2));
    } catch (_) {}
    opened.httpClient.close(force: true);
    final relay = opened.relay;
    if (relay != null) {
      try {
        await relay.close();
      } catch (_) {}
    }
  }

  void _adoptOpenedSocket(_OpenedSocket opened) {
    // Swap synchronously. A stale attempt that was awaiting cleanup can no
    // longer observe and close the winner's relay after this point.
    final oldRelay = _relayTransport;
    _channel = opened.channel;
    _httpClient = opened.httpClient;
    _relayTransport = opened.relay;
    if (oldRelay != null && oldRelay != opened.relay) {
      unawaited(oldRelay.close().catchError((_) {}));
    }
  }

  /// Load relay fields from memory or [SettingsStore].
  Future<void> _loadRelayHints(String? hostInput) async {
    if ((_relayUrl?.isNotEmpty ?? false) &&
        (_relayHostId?.isNotEmpty ?? false)) {
      return;
    }
    try {
      _relayUrl ??= await _settings.getRelayUrl();
      _relayHostId ??= await _settings.getRelayHostId();
      _relayAuthority ??= await _settings.getRelayAuthority();
    } catch (_) {}
  }

  /// Prefer direct when reachable; otherwise relay if configured.
  Future<bool> _shouldUseRelay(
    String? hostInput, {
    String? relayUrl,
    String? relayHostId,
  }) async {
    final hasAttemptRoute = relayUrl != null || relayHostId != null;
    if (hasAttemptRoute) {
      final url = relayUrl?.trim() ?? '';
      final id = relayHostId?.trim() ?? '';
      if (url.isEmpty || id.isEmpty) return false;
      if (hostInput == null || hostInput.trim().isEmpty) return true;
      return !await probeDirectReachable(hostInput);
    }
    await _loadRelayHints(hostInput);
    final storedRelayUrl = _relayUrl?.trim() ?? '';
    final hostId = _relayHostId?.trim() ?? '';
    if (storedRelayUrl.isEmpty || hostId.isEmpty) return false;
    if (hostInput != null &&
        hostInput.trim().isNotEmpty &&
        _relayAuthority != _authorityOf(hostInput)) {
      return false;
    }
    if (hostInput == null || hostInput.trim().isEmpty) return true;
    final direct = await probeDirectReachable(hostInput);
    return !direct;
  }

  /// Remember relay routing from a pair QR (or clear when absent).
  void setRelayRoute({String? relayUrl, String? hostId, String? authority}) {
    _relayUrl = relayUrl?.trim().isEmpty ?? true ? null : relayUrl!.trim();
    _relayHostId = hostId?.trim().isEmpty ?? true ? null : hostId!.trim();
    _relayAuthority = _relayUrl == null || _relayHostId == null
        ? null
        : authority;
  }

  /// Reachability probe for the mcremote authority (mesh/LAN).
  ///
  /// MADR 0016 R7: prefer `/healthz` over bare TCP so an open port that is
  /// not mcremote does not steal the path from relay. Falls back to a TLS
  /// handshake (any cert) when healthz is unreachable, then TCP.
  @visibleForTesting
  static Future<bool> probeDirectReachable(
    String hostInput, {
    Duration timeout = const Duration(milliseconds: 900),
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

  /// The daemon identity allowed to key a probe of [hostInput].
  ///
  /// A probe goes wherever the user typed, which need not be the host this
  /// client authenticated against. Carrying the connected daemon's id onto a
  /// different authority is what pinned one host to another's certificate and
  /// mode (MADR 0046 M-1). The reset mirrors [_noteHost]'s rule without
  /// claiming the probed host as the connection's.
  String? _probeIdentity(String hostInput) {
    final last = _lastHostInput;
    if (last != null && _authorityOf(last) != _authorityOf(hostInput)) {
      return null;
    }
    return deviceId;
  }

  /// The pin and rule to probe [hostInput] with, scoped to that host alone.
  ///
  /// Deliberately narrower than [_resolvePin]: the connection's in-memory pin
  /// and mode are neither read nor written, and no stored pin is inherited
  /// through the persisted identity.
  Future<({String? fingerprint, TlsMode mode})> _probePin(
    String hostInput,
    String? explicit,
    TlsMode? explicitMode,
  ) async {
    final supplied = explicit ?? SettingsStore.fingerprintFrom(hostInput);
    final suppliedMode = explicitMode ?? SettingsStore.tlsModeFrom(hostInput);
    final identity = _probeIdentity(hostInput);
    final canonical = supplied == null ? null : normalizeFingerprint(supplied);
    if (canonical != null) {
      final mode = suppliedMode ?? TlsMode.fallback;
      try {
        // With no identity in hand this is recorded against the address alone,
        // owned by nobody: a probe has not proven which daemon answers here,
        // and pairing is what claims the record.
        await _settings.setFingerprint(
          hostInput,
          canonical,
          deviceId: identity,
          mode: mode,
        );
      } catch (e) {
        debugPrint('McremoteClient: could not persist probed cert pin: $e');
      }
      return (fingerprint: canonical, mode: mode);
    }
    try {
      final stored = await _settings.getPinnedCert(
        hostInput,
        deviceId: identity,
      );
      if (stored != null) {
        return (
          fingerprint: stored.fingerprint,
          mode: suppliedMode ?? stored.mode,
        );
      }
    } catch (e) {
      debugPrint('McremoteClient: could not read cert pin for probe: $e');
    }
    return (fingerprint: null, mode: suppliedMode ?? TlsMode.fallback);
  }

  Future<String> healthz(
    String hostInput, {
    String? fingerprint,
    TlsMode? mode,
  }) async {
    final String url;
    try {
      url = SettingsStore.healthzUrl(hostInput);
    } catch (e) {
      throw Exception('invalid host for healthz: $e');
    }
    // A reachability probe must not disturb the connection's pin state. It is
    // the one dial that never calls _noteHost, so resolving through
    // _resolvePin evaluated the probe under whichever daemon happens to be
    // paired — pinning host B to host A's certificate and mode, and persisting
    // a scanned fingerprint under A's identity (MADR 0046 M-1).
    final probe = await _probePin(hostInput, fingerprint, mode);
    final identity = await _ensureIdentity();
    final pinner = CertPinner(
      probe.fingerprint,
      mode: probe.mode,
      identity: identity,
    );
    final client = IOClient(pinner.newHttpClient());
    try {
      final res = await client
          .get(Uri.parse(url))
          .timeout(const Duration(seconds: 8));
      if (res.statusCode != 200) {
        throw Exception('healthz HTTP ${res.statusCode} for $url');
      }
      return res.body;
    } catch (e) {
      final translated = pinner.translate(e, url);
      if (translated is McException) throw translated;
      if (e is Exception && e.toString().contains(url)) rethrow;
      throw Exception('healthz $url → $e');
    } finally {
      client.close();
    }
  }

  Future<void> connect({
    required String hostInput,
    required String token,
    String? fingerprint,
    TlsMode? mode,
    String? relayUrl,
    String? relayHostId,
    bool enableAutoReconnect = true,
  }) async {
    _manualDisconnect = false;
    _userLoggedOut = false;
    _autoReconnect = enableAutoReconnect;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _reconnectAttempt = 0;
    _handshakeFailures = 0;
    _reconnectInFlight = false;
    lastErrorCode = null;
    // Remember credentials immediately so a mid-handshake drop can retry.
    // `_paired` is deliberately NOT set here: the router keys on it, so
    // claiming it before auth_ok strands the user on /sessions when the
    // handshake fails. It is set in _connectInternal once auth succeeds.
    _noteHost(hostInput);
    _lastToken = token;
    await _resolvePin(hostInput, fingerprint, mode);

    await _connectInternal(
      hostInput: hostInput,
      token: token,
      relayUrl: relayUrl,
      relayHostId: relayHostId,
    );
  }

  /// Claim an 8-char pair code, receive durable token, and stay connected.
  Future<String> claimPairCode({
    required String hostInput,
    required String code,
    String? name,
    String? fingerprint,
    TlsMode? mode,
    String? relayUrl,
    String? relayHostId,
  }) async {
    final authorityChanged =
        _lastHostInput == null ||
        _authorityOf(_lastHostInput!) != _authorityOf(hostInput);
    _manualDisconnect = false;
    _userLoggedOut = false;
    _autoReconnect = true;
    // `_paired` is set only after pair_ok returns a token (see below).
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _reconnectInFlight = false;
    final epoch = ++_connectEpoch;
    await _teardownSocket(suppressReconnect: true);

    lastError = null;
    lastErrorCode = null;
    if (authorityChanged) {
      // A token authenticates one authority only. Do this before B's socket is
      // opened, while leaving the persisted host untouched until pairing wins.
      _lastToken = null;
      _paired = false;
      await _settings.clearToken();
    }
    _noteHost(hostInput);
    wsUrl = SettingsStore.normalizeWsUrl(hostInput);
    final pin = await _resolvePin(hostInput, fingerprint, mode);
    if (_staleAttempt(epoch)) {
      throw McException('pairing superseded', code: 'pair_failed');
    }
    _setState(McConnectionState.connecting);

    final _OpenedSocket opened;
    try {
      opened = await _openSocket(
        wsUrl!,
        pin,
        hostInput: hostInput,
        relayUrl: relayUrl,
        relayHostId: relayHostId,
      );
      await afterSocketOpen?.call();
    } catch (e) {
      if (_staleAttempt(epoch)) {
        throw McException('pairing superseded', code: 'pair_failed');
      }
      lastError = e.toString();
      lastErrorCode = e is McException
          ? (e.code ?? 'connect_failed')
          : 'connect_failed';
      _setState(McConnectionState.error);
      _suppressReconnect = false;
      await _teardownSocket(suppressReconnect: true);
      _suppressReconnect = false;
      if (e is McException) rethrow;
      throw McException('connection failed: $e', code: 'connect_failed');
    }
    if (_staleAttempt(epoch)) {
      await _closeOpenedSocket(opened);
      throw McException('pairing superseded', code: 'pair_failed');
    }
    _adoptOpenedSocket(opened);

    _suppressReconnect = false;
    _sub = _channel!.stream.listen(
      _onMessage,
      onError: _onSocketError,
      onDone: _onSocketDone,
      cancelOnError: false,
    );

    _setState(McConnectionState.authenticating);
    try {
      final normalized = PairPayload.normalizePairCode(code);
      final res = await request(
        'pair.claim',
        payload: {
          'code': normalized,
          if (name != null && name.isNotEmpty) 'name': name,
        },
        timeout: const Duration(seconds: 20),
      );
      if (_staleAttempt(epoch)) {
        throw McException('pairing superseded', code: 'pair_failed');
      }

      final err = handshakeErrorFrom(
        'pair_ok',
        res.type,
        res.payload,
        isPair: true,
      );
      if (err != null) {
        await _failHandshake(err);
      }

      final token = res.payload?['token'] as String? ?? '';
      if (token.isEmpty) {
        await _failHandshake(
          McException(
            'pair_ok missing token',
            code: 'unexpected_pair_response',
            permanent: true,
          ),
        );
      }

      deviceId = res.payload?['device_id'] as String?;
      deviceName = res.payload?['device_name'] as String?;

      if (_pinnedFingerprint != null) {
        // Best-effort: the daemon has already enrolled this device — a
        // keystore hiccup here must not discard the freshly issued token.
        try {
          await _settings.setFingerprint(
            hostInput,
            _pinnedFingerprint!,
            deviceId: deviceId,
            mode: _tlsMode,
          );
        } catch (e) {
          debugPrint('mcremote: persisting pin after pair failed: $e');
        }
        if (_staleAttempt(epoch)) {
          throw McException('pairing superseded', code: 'pair_failed');
        }
      }

      _lastToken = token;
      setRelayRoute(
        relayUrl: relayUrl,
        hostId: relayHostId,
        authority: _authorityOf(hostInput),
      );
      _paired = true;
      _reconnectAttempt = 0;
      _handshakeFailures = 0;
      lastError = null;
      lastErrorCode = null;
      _setState(McConnectionState.connected);
      _startPing();
      return token;
    } on McException {
      rethrow;
    } on TimeoutException catch (e) {
      if (_staleAttempt(epoch)) {
        throw McException('pairing superseded', code: 'pair_failed');
      }
      await _failHandshake(
        McException(
          e.message ?? 'pair claim timed out',
          code: 'auth_timeout',
          permanent: false,
        ),
      );
    } catch (e) {
      if (_staleAttempt(epoch)) {
        throw McException('pairing superseded', code: 'pair_failed');
      }
      await _failHandshake(
        McException(e.toString(), code: 'pair_failed', permanent: true),
      );
    }
  }

  Future<void> _connectInternal({
    required String hostInput,
    required String token,
    String? relayUrl,
    String? relayHostId,
  }) async {
    final epoch = ++_connectEpoch;
    await _teardownSocket(suppressReconnect: true);
    if (_staleAttempt(epoch)) return;
    lastError = null;
    // Do not clear lastErrorCode until success — callers may inspect prior codes.
    _noteHost(hostInput);
    _lastToken = token;
    wsUrl = SettingsStore.normalizeWsUrl(hostInput);
    final pin = await _resolvePin(hostInput);
    if (_staleAttempt(epoch)) return;

    final isReconnect = _state == McConnectionState.reconnecting;
    if (!isReconnect) {
      _setState(McConnectionState.connecting);
    }

    final _OpenedSocket opened;
    try {
      opened = await _openSocket(
        wsUrl!,
        pin,
        hostInput: hostInput,
        relayUrl: relayUrl,
        relayHostId: relayHostId,
      );
      await afterSocketOpen?.call();
    } catch (e) {
      if (_staleAttempt(epoch)) return;
      lastError = e.toString();
      final permanent = e is McException && e.permanent;
      lastErrorCode = e is McException
          ? (e.code ?? 'connect_failed')
          : 'connect_failed';
      // A retryable pinned-TLS failure is handshake-level too: the host
      // answered but the handshake keeps dying. Plain network failures
      // (host unreachable) stay uncounted and retry until sign-out.
      if (!permanent && lastErrorCode == 'tls_failed') _handshakeFailures++;
      _setState(McConnectionState.error);
      _suppressReconnect = false;
      if (permanent) {
        // A cert mismatch (or an unpinned self-signed host) will not fix
        // itself by retrying, and retrying an unverified peer is exactly what
        // pinning exists to prevent. Fail closed and wait for the user.
        _autoReconnect = false;
      } else {
        _scheduleReconnect();
      }
      rethrow;
    }
    if (_staleAttempt(epoch)) {
      // Superseded (newer attempt or sign-out) while the socket opened: this
      // socket belongs to nobody — close it without touching shared state.
      await _closeOpenedSocket(opened);
      return;
    }
    _adoptOpenedSocket(opened);

    _suppressReconnect = false;
    _sub = _channel!.stream.listen(
      _onMessage,
      onError: _onSocketError,
      onDone: _onSocketDone,
      cancelOnError: false,
    );

    _setState(McConnectionState.authenticating);
    try {
      final auth = await request(
        'auth',
        payload: {'token': token},
        token: token,
      );
      if (_staleAttempt(epoch)) return;

      final err = handshakeErrorFrom(
        'auth_ok',
        auth.type,
        auth.payload,
        isPair: false,
      );
      if (err != null) {
        await _failHandshake(err);
      }

      deviceId = auth.payload?['device_id'] as String?;
      deviceName = auth.payload?['device_name'] as String?;
      final home = auth.payload?['home_dir'] as String?;
      if (home != null && home.isNotEmpty) hostHomeDir = home;

      if (_pinnedFingerprint != null) {
        // Best-effort: a keystore hiccup must not tear down an already
        // authenticated connection (the pin still lives in memory).
        try {
          await _settings.setFingerprint(
            hostInput,
            _pinnedFingerprint!,
            deviceId: deviceId,
            mode: _tlsMode,
          );
        } catch (e) {
          debugPrint('mcremote: persisting pin failed: $e');
        }
        if (_staleAttempt(epoch)) return;
      }

      if (relayUrl != null || relayHostId != null) {
        setRelayRoute(
          relayUrl: relayUrl,
          hostId: relayHostId,
          authority: _authorityOf(hostInput),
        );
      }
      _paired = true;
      _reconnectAttempt = 0;
      _handshakeFailures = 0;
      lastError = null;
      lastErrorCode = null;
      _setState(McConnectionState.connected);
      _startPing();
    } on McException {
      rethrow;
    } on TimeoutException catch (e) {
      if (_staleAttempt(epoch)) return;
      await _failHandshake(
        McException(
          e.message ?? 'auth timed out',
          code: 'auth_timeout',
          permanent: false,
        ),
      );
    } catch (e) {
      if (_staleAttempt(epoch)) return;
      // Network / unexpected during handshake: tear down, allow reconnect.
      lastError = e.toString();
      _setState(McConnectionState.error);
      await _teardownSocket(suppressReconnect: true);
      _suppressReconnect = false;
      _scheduleReconnect();
      rethrow;
    }
  }

  /// Record the dialled host, resetting the daemon identity when the
  /// authority actually changed — a stale [deviceId] would otherwise key the
  /// next host's certificate pin under the previous host's identity,
  /// clobbering its stored pin.
  void _noteHost(String hostInput) {
    final next = hostInput.trim();
    final prev = _lastHostInput;
    if (prev == null || _authorityOf(prev) != _authorityOf(next)) {
      deviceId = null;
      deviceName = null;
      hostHomeDir = null;
    }
    _setLastHostInput(next);
  }

  /// Tear down the socket, set error state, optionally disable auto-reconnect.
  Future<Never> _failHandshake(McException err) async {
    lastError = err.message;
    lastErrorCode = err.code;
    // The socket opened but the handshake failed — that's a host-side
    // problem, counted toward the parked-error cap (see _handshakeFailures).
    if (!err.permanent) _handshakeFailures++;
    _setState(McConnectionState.error);
    if (err.permanent) {
      _autoReconnect = false;
      // Only drop pairing when the server says the secret is bad.
      if (err.isInvalidToken) {
        _lastToken = null;
        _paired = false;
      }
    }
    await _teardownSocket(suppressReconnect: true);
    _suppressReconnect = false;
    throw err;
  }

  void _startPing() {
    _pingTimer?.cancel();
    _missedPings = 0;
    // Faster than typical mobile NAT/idle timeouts so we notice drops sooner.
    _pingTimer = Timer.periodic(const Duration(seconds: 20), (_) {
      if (_state == McConnectionState.connected) {
        // Claimed here, not when the failure arrives. A user-initiated
        // connect() tears down this socket — failing the in-flight ping — and
        // by the time the rejection is delivered the epoch has already moved
        // on, so a guard read there would compare a connection against
        // itself and let a dead ping flap the state of a live handshake
        // (MADR 0046 L-1).
        final pingEpoch = _connectEpoch;
        unawaited(
          request('ping', timeout: const Duration(seconds: 12))
              .then((_) {
                _missedPings = 0; // reset on success
              })
              .catchError((Object e) {
                if (pingEpoch != _connectEpoch) return;
                _missedPings++;
                // Missed pong → force reconnect path if we've missed 2 in a row.
                lastError = e.toString();
                if (_missedPings >= 2 &&
                    !_manualDisconnect &&
                    !_userLoggedOut &&
                    hasCredentials) {
                  _missedPings = 0;
                  unawaited(
                    _teardownSocket(suppressReconnect: true).then((_) {
                      if (pingEpoch != _connectEpoch ||
                          _state != McConnectionState.connected) {
                        return;
                      }
                      _suppressReconnect = false;
                      _setState(McConnectionState.error);
                      _scheduleReconnect();
                    }),
                  );
                }
                // A missed pong is handled entirely by the reconnect side-effects
                // above; this catchError only exists to swallow the rejection, so it
                // yields null (the chain is unawaited and its value is discarded).
              }),
        );
      }
    });
  }

  /// One-shot liveness probe for a socket that claims `connected` (used after
  /// resume / interface churn). On failure it tears down and schedules an
  /// immediate reconnect — much faster than waiting out two periodic ping
  /// misses on a blackholed link, and much cheaper than bouncing a healthy
  /// socket unconditionally.
  Future<void> probeLiveness() async {
    if (_state != McConnectionState.connected) return;
    // Same rule as the periodic ping: the connection this probe is about is
    // the one live when it was sent (MADR 0046 L-1).
    final probeEpoch = _connectEpoch;
    try {
      await request('ping', timeout: const Duration(seconds: 5));
      if (probeEpoch != _connectEpoch) return;
      _missedPings = 0;
    } catch (e) {
      if (probeEpoch != _connectEpoch) return;
      if (_manualDisconnect || _userLoggedOut || !hasCredentials) return;
      if (_state != McConnectionState.connected) return;
      lastError = e.toString();
      await _teardownSocket(suppressReconnect: true);
      _suppressReconnect = false;
      _setState(McConnectionState.error);
      _scheduleReconnect();
    }
  }

  void _onSocketError(Object e) {
    lastError = e.toString();
    if (_suppressReconnect || _manualDisconnect || _userLoggedOut) {
      return;
    }
    if (_state != McConnectionState.reconnecting) {
      _setState(McConnectionState.error);
    }
    _scheduleReconnect();
  }

  void _onSocketDone() {
    _failAllPending('connection closed');
    if (_suppressReconnect || _manualDisconnect || _userLoggedOut) {
      if (_manualDisconnect || _userLoggedOut) {
        _setState(McConnectionState.disconnected);
      }
      return;
    }
    // Any unexpected close while paired should reconnect (incl. screen lock).
    if (hasCredentials || _paired) {
      if (_state != McConnectionState.reconnecting) {
        _setState(McConnectionState.error);
      }
      _scheduleReconnect();
      return;
    }
    _setState(McConnectionState.disconnected);
  }

  void _scheduleReconnect() {
    if (!_autoReconnect || _manualDisconnect || _userLoggedOut) {
      return;
    }
    if (!hasCredentials) {
      return;
    }
    if (_reconnectInFlight || (_reconnectTimer?.isActive ?? false)) {
      return;
    }
    if (_state == McConnectionState.connected) {
      return;
    }
    if (_handshakeFailures >= _maxHandshakeFailures) {
      // Host reachable but the handshake keeps failing (wedged daemon, bad
      // cert): stop the blind loop and park in error so the UI shows a
      // definitive failure instead of "Reconnecting…" forever. Resume /
      // connectivity-change / Retry-now all go through reconnectFromStore,
      // which resets the count and re-arms the loop.
      _setState(McConnectionState.error);
      return;
    }
    final attempt = _reconnectAttempt;
    // 1, 2, 4, 8, 16, 30, 30…
    final delaySec = math.min(30, math.pow(2, math.min(attempt, 5)).toInt());
    final delay = Duration(seconds: attempt == 0 ? 1 : delaySec);
    _setState(McConnectionState.reconnecting);
    _reconnectTimer = Timer(delay, () async {
      if (_manualDisconnect || _userLoggedOut || !hasCredentials) return;
      if (_state == McConnectionState.connected) return;
      _reconnectInFlight = true;
      _reconnectAttempt++;
      try {
        await _connectInternal(hostInput: _lastHostInput!, token: _lastToken!);
      } catch (_) {
        // Retry until explicit sign-out (unless permanent auth failure).
        if (_state != McConnectionState.connected &&
            _autoReconnect &&
            !_manualDisconnect &&
            !_userLoggedOut &&
            hasCredentials) {
          _reconnectInFlight = false;
          _scheduleReconnect();
          return;
        }
      } finally {
        _reconnectInFlight = false;
      }
    });
  }

  /// Manual reconnect from UI or app resume.
  ///
  /// [hostInput]/[token] fill gaps when in-memory credentials were cleared
  /// (e.g. after process death recovery from [SettingsStore]).
  Future<void> reconnect({String? hostInput, String? token}) async {
    if (_userLoggedOut && !_paired) {
      throw McException(
        'signed out — pair or connect again',
        code: 'no_credentials',
        permanent: true,
      );
    }
    final host = (hostInput ?? _lastHostInput)?.trim() ?? '';
    final tok = (token ?? _lastToken)?.trim() ?? '';
    if (host.isEmpty || tok.isEmpty) {
      throw McException(
        'no saved credentials',
        code: 'no_credentials',
        permanent: true,
      );
    }
    _manualDisconnect = false;
    _userLoggedOut = false;
    _autoReconnect = true;
    _paired = true;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _reconnectAttempt = 0;
    _handshakeFailures = 0;
    _reconnectInFlight = false;
    _setState(McConnectionState.reconnecting);
    await _connectInternal(hostInput: host, token: tok);
  }

  /// Load host/token from [store] if memory is empty, then reconnect.
  Future<void> reconnectFromStore(SettingsStore store) async {
    // Explicit sign-out: do not revive from disk until user taps Connect.
    if (_userLoggedOut) {
      throw McException(
        'signed out — pair or connect again',
        code: 'no_credentials',
        permanent: true,
      );
    }
    final host = (_lastHostInput?.isNotEmpty ?? false)
        ? _lastHostInput
        : await store.getHost();
    final tok = (_lastToken?.isNotEmpty ?? false)
        ? _lastToken
        : await store.getToken();
    if ((host == null || host.isEmpty) || (tok == null || tok.isEmpty)) {
      throw McException(
        'no saved credentials',
        code: 'no_credentials',
        permanent: true,
      );
    }
    _paired = true;
    await reconnect(hostInput: host, token: tok);
  }

  /// Explicit sign-out: stop auto-reconnect until the user pairs/connects again.
  /// Does **not** revoke the device on the host; token remains in secure storage
  /// so Connect can re-auth without a new QR (clear credentials to wipe it).
  Future<void> disconnect({bool manual = true}) async {
    // Invalidate any in-flight connect attempt: without this, a socket that
    // finishes opening after sign-out would authenticate and stay up.
    _connectEpoch++;
    _manualDisconnect = manual;
    if (manual) {
      _autoReconnect = false;
      _userLoggedOut = true;
      _paired = false;
      // Drop in-memory token so background resume cannot revive the socket.
      _lastToken = null;
    }
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _reconnectInFlight = false;
    await _teardownSocket(suppressReconnect: true);
    _suppressReconnect = false;
    _setState(McConnectionState.disconnected);
  }

  Future<void> _teardownSocket({bool suppressReconnect = false}) async {
    if (suppressReconnect) {
      _suppressReconnect = true;
    }
    _pingTimer?.cancel();
    _pingTimer = null;
    // Detach every shared resource synchronously before awaiting a close. A
    // newer attempt may adopt a socket while this close handshake is pending;
    // this teardown must only ever close the bundle it observed on entry.
    final sub = _sub;
    _sub = null;
    final channel = _channel;
    _channel = null;
    final httpClient = _httpClient;
    _httpClient = null;
    final relay = _relayTransport;
    _relayTransport = null;
    // The pending map is shared too. Failing it after the awaits let a stale,
    // unawaited teardown error the *newer* attempt's auth completer, leaving
    // it stuck in `authenticating` on a live socket (MADR 0046 L-2).
    final pending = Map.of(_pending);
    _pending.clear();
    await sub?.cancel();
    try {
      // Bounded: on a blackholed link (exactly what the missed-ping path
      // detects) the close handshake can hang on TCP for minutes, and the
      // reconnect only schedules after this returns.
      await channel?.sink.close().timeout(const Duration(seconds: 2));
    } catch (_) {}
    // The pinned HttpClient is per-socket; leaking it would keep the old
    // connection pool (and its pin) alive across reconnects.
    httpClient?.close(force: true);
    if (relay != null) {
      try {
        await relay.close();
      } catch (_) {}
    }
    _failPending(pending, 'disconnected');
  }

  /// Build a typed exception from a session-op error envelope, preserving the
  /// daemon's stable error code (`session_limit`, `session_forbidden`,
  /// `rate_limited`, …) so screens can map it to actionable copy instead of
  /// showing `Exception: …` strings.
  @visibleForTesting
  static McException opException(Envelope res, String fallback) {
    final msg = res.payload?['message'] as String? ?? fallback;
    final code = res.payload?['code'] as String?;
    return McException(msg, code: code);
  }

  void _failAllPending(String reason) {
    final pending = Map.of(_pending);
    _pending.clear();
    _failPending(pending, reason);
  }

  static void _failPending(
    Map<String, Completer<Envelope>> pending,
    String reason,
  ) {
    for (final c in pending.values) {
      if (!c.isCompleted) {
        c.completeError(McException(reason, code: 'connection_lost'));
      }
    }
  }

  void _onMessage(dynamic data) {
    try {
      if (data is! String) {
        debugPrint('mcremote: ignoring non-text frame (${data.runtimeType})');
        return;
      }
      final map = jsonDecode(data) as Map<String, dynamic>;
      final env = Envelope.fromJson(map);
      if (env.v != 1) {
        lastError = 'unsupported protocol version ${env.v}';
        lastErrorCode = 'bad_version';
        debugPrint('mcremote: bad protocol version ${env.v}');
        return;
      }

      if (env.type == 'event') {
        final raw = env.payload?['event'];
        Map<String, dynamic>? evMap;
        if (raw is Map<String, dynamic>) {
          evMap = raw;
        } else if (raw is Map) {
          evMap = Map<String, dynamic>.from(raw);
        } else if (env.payload != null && env.payload!.containsKey('type')) {
          evMap = env.payload;
        }
        if (evMap != null) {
          _events.add(SessionEvent.fromJson(evMap));
        }
        return;
      }

      final id = env.id;
      if (id != null && _pending.containsKey(id)) {
        _pending.remove(id)!.complete(env);
        return;
      }

      if (env.type == 'error') {
        // Unsolicited error frame (no matching request id). Log it, but do
        // NOT overwrite lastErrorCode — the connect screen consults that for
        // handshake classification and a stray frame would mislabel it.
        debugPrint(
          'mcremote: unsolicited error frame: '
          '${env.payload?['code']} ${env.payload?['message']}',
        );
        lastError = env.payload?['message'] as String? ?? 'error';
      }
    } catch (e) {
      debugPrint('mcremote: dropping malformed frame: $e');
      lastError = 'parse error: $e';
    }
  }

  /// Send a control-plane request and wait for the matching response.
  ///
  /// [requestId] reuses a client envelope id (MADR 0056 H-2b). When null a
  /// fresh UUID is minted. Mutating ops that may time out pass the same id on
  /// [idempotentRetry] so the daemon ledger can replay instead of double-exec.
  ///
  /// [expectedType] rejects wrong non-error response types (MADR 0056 M-2)
  /// instead of treating them as empty success.
  Future<Envelope> request(
    String type, {
    Map<String, dynamic>? payload,
    String? token,
    Duration timeout = const Duration(seconds: 30),
    String? requestId,
    String? expectedType,
    bool idempotentRetry = false,
  }) async {
    final ch = _channel;
    if (ch == null) throw StateError('not connected');
    final id = (requestId != null && requestId.isNotEmpty)
        ? requestId
        : _uuid.v4();
    final completer = Completer<Envelope>();
    _pending[id] = completer;
    final encoded = encodeRequestEnvelope(
      id: id,
      type: type,
      payload: payload,
      token: token,
    );
    if (utf8.encode(encoded).length > kMaxClientFrameBytes) {
      _pending.remove(id);
      throw McException(
        'Request is too large for the 1 MB connection limit.',
        code: 'payload_too_large',
        permanent: false,
      );
    }
    try {
      ch.sink.add(encoded);
    } catch (e) {
      // Half-closed socket: drop the completer rather than leaking it, since
      // nothing will ever arrive to complete it.
      _pending.remove(id);
      rethrow;
    }
    try {
      final res = await completer.future.timeout(timeout);
      return _checkExpectedType(res, expectedType, type);
    } on TimeoutException {
      _pending.remove(id);
      // One retry with the same id so the daemon ledger can replay (H-2b).
      if (idempotentRetry) {
        return request(
          type,
          payload: payload,
          token: token,
          timeout: timeout,
          requestId: id,
          expectedType: expectedType,
          idempotentRetry: false,
        );
      }
      throw TimeoutException('request $type timed out');
    }
  }

  Envelope _checkExpectedType(Envelope res, String? expectedType, String op) {
    if (expectedType == null || expectedType.isEmpty) return res;
    if (res.type == 'error') return res;
    if (res.type == expectedType) return res;
    throw McException(
      'unexpected $op response: ${res.type} (want $expectedType)',
      code: 'bad_response_type',
      permanent: false,
    );
  }

  /// Owner-scoped session list. Prefer [listSessionSnapshot] when the caller
  /// may prune local state — only a complete snapshot is destructive-safe.
  Future<List<SessionMeta>> listSessions() async {
    final snap = await listSessionSnapshot();
    return snap.sessions;
  }

  /// `session.list` with completeness metadata (MADR 0056 H-6).
  Future<SessionListSnapshot> listSessionSnapshot() async {
    final res = await request(
      'session.list',
      payload: {},
      expectedType: 'session.list_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'list failed');
    }
    final payload = res.payload;
    final list = payload?['sessions'];
    if (list is! List) {
      throw McException(
        'session.list_result missing sessions array',
        code: 'bad_payload',
        permanent: false,
      );
    }
    final sessions = list.map((e) {
      if (e is Map<String, dynamic>) return SessionMeta.fromJson(e);
      return SessionMeta.fromJson(Map<String, dynamic>.from(e as Map));
    }).toList();
    // Rollout: honor `complete` when present; old daemons omit it — treat a
    // valid sessions list as complete so existing hosts keep working.
    final complete = payload!.containsKey('complete')
        ? payload['complete'] == true
        : true;
    final degraded = payload['degraded'] == true;
    final skipped = payload['skipped'] is num
        ? (payload['skipped'] as num).toInt()
        : 0;
    return SessionListSnapshot(
      sessions: sessions,
      complete: complete,
      degraded: degraded,
      skipped: skipped,
    );
  }

  /// Discover bounded, metadata-only sessions stored by one provider. Listing
  /// never imports a conversation; callers pass a selected id to
  /// [createSession] as `agentSessionId`.
  Future<List<AgentSessionMeta>> listAgentSessions(String provider) async {
    final res = await request(
      'agent_sessions.list',
      payload: {'provider': provider},
      expectedType: 'agent_sessions.list_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'native session list failed');
    }
    final list = res.payload?['sessions'];
    if (list is! List) return [];
    return list
        .whereType<Map>()
        .map((e) => AgentSessionMeta.fromJson(Map<String, dynamic>.from(e)))
        .where((e) => e.id.isNotEmpty)
        .toList(growable: false);
  }

  /// Replay a session's recorded events. The daemon returns each element in the
  /// identical JSON shape as the `event` field of a live `event` envelope, so
  /// each is parsed with [SessionEvent.fromJson] and fed through
  /// `applySessionEvent` exactly like a live event.
  ///
  /// Throws on transport/protocol errors (including an incomplete multi-page
  /// fetch — MADR 0056 M-7). Returns an empty list only when the host reports
  /// no history for the session.
  ///
  /// Auto-pages while `truncated` is true (byte soft-cap or page size) so the
  /// phone gets the full host ring, not only the first ~512 KiB (MADR 0018 E4).
  /// [limit] defaults to [kHistoryFetchLimit] (800).
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async {
    final out = <SessionEvent>[];
    var sinceSeq = 0;
    var prevTruncated = false;
    // Safety bound: ring is ≤800; byte pages may be smaller.
    for (var page = 0; page < 32; page++) {
      final res = await request(
        'session.history',
        payload: {
          'session_id': sessionId,
          if (limit > 0) 'limit': limit,
          if (sinceSeq > 0) 'since_seq': sinceSeq,
        },
        expectedType: 'session.history_result',
      );
      // A mid-page error must not surface the pages fetched so far: they
      // are an oldest-only prefix, and resyncHistory would treat it as the
      // complete ring and rebuild away the newest content. Fail the whole
      // fetch instead, matching the catch path below.
      if (res.type == 'error') {
        throw McremoteClient.opException(res, 'session history failed');
      }
      final list = res.payload?['events'];
      if (list is! List) {
        throw McException(
          'session.history_result missing events array',
          code: 'bad_payload',
          permanent: false,
        );
      }
      if (list.isEmpty) {
        // Previous page said more remained: empty next page is incomplete.
        if (prevTruncated) {
          throw McException(
            'incomplete session.history page after truncated=true',
            code: 'history_incomplete',
            permanent: false,
          );
        }
        return out;
      }
      for (final e in list) {
        if (e is Map<String, dynamic>) {
          out.add(SessionEvent.fromJson(e));
        } else if (e is Map) {
          out.add(SessionEvent.fromJson(Map<String, dynamic>.from(e)));
        }
      }
      final truncated = res.payload?['truncated'] == true;
      final next = res.payload?['next_since_seq'];
      final nextSince = next is num ? next.toInt() : 0;
      if (!truncated || nextSince <= sinceSeq) return out;
      prevTruncated = true;
      sinceSeq = nextSince;
    }
    return out;
  }

  /// Owner-scoped daemon snapshot of unresolved permissions and questions.
  /// Unlike history, failure is not treated as an empty snapshot: callers must
  /// retain their current notifications rather than canceling actionable asks.
  Future<List<SessionEvent>> pendingAsks() async {
    final res = await request(
      'session.pending_asks',
      payload: {},
      expectedType: 'session.pending_asks_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'pending asks failed');
    }
    final raw = res.payload?['events'];
    if (raw is! List) return const [];
    return raw
        .whereType<Map>()
        .map((event) => SessionEvent.fromJson(Map<String, dynamic>.from(event)))
        .where(
          (event) =>
              (event.type == 'permission_request' &&
                  (event.permissionId ?? '').isNotEmpty) ||
              (event.type == 'question_request' &&
                  (event.questionId ?? '').isNotEmpty),
        )
        .toList(growable: false);
  }

  Future<List<ProviderInfo>> listProviders() async {
    final res = await request(
      'providers.list',
      payload: {},
      expectedType: 'providers.list_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'providers failed');
    }
    final list = res.payload?['providers'];
    if (list is! List) return [];
    return list.map((e) {
      if (e is Map<String, dynamic>) return ProviderInfo.fromJson(e);
      return ProviderInfo.fromJson(Map<String, dynamic>.from(e as Map));
    }).toList();
  }

  /// Fetch a model picker catalog for [provider] (`models.list`).
  ///
  /// The three optional scopes are what keep this usable (MADR 0043 D1). An
  /// unscoped call returns the provider's *default* set — for OpenCode, the
  /// model providers the host actually has credentials for, not all 5,788
  /// models models.dev knows about.
  ///
  /// * [scope] `'providers'` enumerates model providers (anthropic, openai, …)
  ///   instead of models — the first step of the two-step picker.
  /// * [modelProvider] narrows a model list to one of them.
  /// * [sessionId] scopes the catalog to a live session: the models of the
  ///   provider that session is using, with its current model pre-selected.
  ///
  /// Returns an empty allow-custom catalog on soft failures so free-text still works.
  Future<PickerCatalog> listModels(
    String provider, {
    String? scope,
    String? modelProvider,
    String? sessionId,
  }) async {
    final res = await request(
      'models.list',
      payload: {
        'provider': provider,
        if (scope != null && scope.isNotEmpty) 'scope': scope,
        if (modelProvider != null && modelProvider.isNotEmpty)
          'model_provider': modelProvider,
        if (sessionId != null && sessionId.isNotEmpty) 'session_id': sessionId,
      },
      expectedType: 'models.list_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'models failed');
    }
    final payload = res.payload;
    if (payload == null) {
      return PickerCatalog(allowCustom: true, provider: provider);
    }
    return PickerCatalog.fromJson(payload);
  }

  /// Fetch the agent-name picker catalog for [provider] (`agents.list`).
  /// OpenCode returns primary/subagent rows from GET /agent; other providers
  /// may return an empty allow-custom catalog.
  Future<PickerCatalog> listAgents(String provider) async {
    final res = await request(
      'agents.list',
      payload: {'provider': provider},
      expectedType: 'agents.list_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'agents failed');
    }
    final payload = res.payload;
    if (payload == null) {
      return PickerCatalog(allowCustom: true, provider: provider);
    }
    return PickerCatalog.fromJson(payload);
  }

  // `commands.list` has no client here: the catalog arrives unprompted as
  // `available_commands` / `remote_commands` events, which the reducer already
  // applies. A polling method alongside them was only ever dead weight.

  /// Fork the OpenCode conversation into a new mcremote session (`session.fork`).
  Future<SessionMeta> forkSession(String sessionId, {String? messageId}) async {
    final res = await request(
      'session.fork',
      payload: {
        'session_id': sessionId,
        if (messageId != null && messageId.isNotEmpty) 'message_id': messageId,
      },
      expectedType: 'session.created',
      idempotentRetry: true,
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'fork failed');
    }
    final p = res.payload;
    if (p == null || (p['id'] as String? ?? '').isEmpty) {
      throw Exception('unexpected session.fork response: ${res.type}');
    }
    return SessionMeta.fromJson(p);
  }

  // `session.revert` / `session.unrevert` have no client here, deliberately
  // (MADR 0042 D10). `session.revert` requires a `message_id`, but `event.Event`
  // carries no message id at all — so no event this client receives contains
  // one, and `ChatItem.seq` is a reducer-assigned local counter unrelated to any
  // provider id. The call could never be constructed. It is a missing protocol
  // capability, not unwired UI, so do not "restore" these methods without first
  // putting a message id on the wire.
  //
  // `unrevert` went with it: with revert unreachable, "Restore reverts" was an
  // undo button for an action this app cannot perform. The daemon handlers stay
  // — other clients may use them.
  //
  // If a message id is ever added, the operations worth building on it are
  // fork-at-message and diff-at-message, both non-destructive; `forkSession`
  // and `sessionDiff` already accept the parameter.

  /// Fetch a file-change summary (`session.diff`). The daemon also emits a
  /// notice with the same text.
  Future<String> sessionDiff(String sessionId, {String? messageId}) async {
    final res = await request(
      'session.diff',
      payload: {
        'session_id': sessionId,
        if (messageId != null && messageId.isNotEmpty) 'message_id': messageId,
      },
      expectedType: 'session.diff_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'diff failed');
    }
    return res.payload?['summary'] as String? ?? '';
  }

  /// Rename a user-visible session title after the host updates its native
  /// provider title too (`session.rename`).
  Future<SessionMeta> renameSession(String sessionId, String name) async {
    final res = await request(
      'session.rename',
      payload: {'session_id': sessionId, 'name': name},
      expectedType: 'session.rename_result',
      idempotentRetry: true,
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'rename failed');
    }
    final raw = res.payload?['session'];
    if (raw is! Map) {
      throw Exception('unexpected session.rename response: ${res.type}');
    }
    return SessionMeta.fromJson(Map<String, dynamic>.from(raw));
  }

  /// Fetch bounded, read-only project/MCP metadata on explicit user request.
  Future<SessionDiagnostics> sessionDiagnostics(String sessionId) async {
    final res = await request(
      'session.diagnostics',
      payload: {'session_id': sessionId},
      expectedType: 'session.diagnostics_result',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'diagnostics failed');
    }
    final raw = res.payload?['diagnostics'];
    if (raw is! Map) {
      throw Exception('unexpected session.diagnostics response: ${res.type}');
    }
    return SessionDiagnostics.fromJson(Map<String, dynamic>.from(raw));
  }

  /// Create a session, or resume an existing agent conversation by passing
  /// [agentSessionId] (and optionally the prior [sessionId]) — the daemon
  /// forwards these to ACP `session/load`.
  Future<SessionMeta> createSession({
    String? provider,
    String? name,
    String? cwd,
    String? model,
    String? thinkingLevel,
    String? agent,
    String? agentSessionId,
    String? sessionId,
  }) async {
    final res = await request(
      'session.create',
      payload: {
        if (provider != null && provider.isNotEmpty) 'provider': provider,
        if (name != null && name.isNotEmpty) 'name': name,
        if (cwd != null && cwd.isNotEmpty) 'cwd': cwd,
        if (model != null && model.isNotEmpty) 'model': model,
        if (thinkingLevel != null && thinkingLevel.isNotEmpty)
          'thinking_level': thinkingLevel,
        if (agent != null && agent.isNotEmpty) 'agent': agent,
        if (agentSessionId != null && agentSessionId.isNotEmpty)
          'agent_session_id': agentSessionId,
        if (sessionId != null && sessionId.isNotEmpty) 'session_id': sessionId,
      },
      expectedType: 'session.created',
      idempotentRetry: true,
      timeout: const Duration(seconds: 120),
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'create failed');
    }
    // The daemon replies with a bare Meta; anything else means we would build
    // a SessionMeta with an empty id and fail confusingly later.
    final p = res.payload;
    if (p == null || (p['id'] as String? ?? '').isEmpty) {
      throw Exception('unexpected session.create response: ${res.type}');
    }
    return SessionMeta.fromJson(p);
  }

  /// Resume a previously closed session from its persisted record.
  Future<SessionMeta> resumeSession(SessionMeta prior) {
    return createSession(
      provider: prior.provider,
      name: prior.name,
      cwd: prior.cwd,
      model: prior.model,
      thinkingLevel: prior.thinkingLevel.isEmpty ? null : prior.thinkingLevel,
      agentSessionId: prior.agentSessionId,
      sessionId: prior.id,
    );
  }

  /// Switch the session's reasoning/thinking level via the daemon `/thinking`
  /// command (MADR 0052). Empty [level] asks the daemon to report the current
  /// level. Grok returns a "new sessions only" notice rather than applying.
  Future<void> setThinkingLevel(String sessionId, String level) {
    final trimmed = level.trim();
    final text = trimmed.isEmpty ? '/thinking' : '/thinking $trimmed';
    return prompt(sessionId, text);
  }

  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    final payload = <String, dynamic>{'session_id': sessionId, 'text': text};
    if (attachments.isNotEmpty) {
      payload['attachments'] = [for (final a in attachments) a.toJson()];
    }
    final res = await request(
      'session.prompt',
      payload: payload,
      expectedType: 'ok',
      idempotentRetry: true,
      timeout: const Duration(seconds: 60),
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'prompt failed');
    }
  }

  /// Switch the session's active operating mode (ACP session modes).
  Future<void> setMode(String sessionId, String modeId) async {
    final res = await request(
      'session.set_mode',
      payload: {'session_id': sessionId, 'mode_id': modeId},
      expectedType: 'ok',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'set mode failed');
    }
  }

  /// Change an agent-defined session config option. [kind] is "select" or
  /// "boolean"; for boolean [value] is "true"/"false", for select it is the
  /// chosen value id.
  Future<void> setConfigOption(
    String sessionId,
    String optionId,
    String kind,
    String value,
  ) async {
    final res = await request(
      'session.set_config_option',
      payload: {
        'session_id': sessionId,
        'option_id': optionId,
        'kind': kind,
        'value': value,
      },
      expectedType: 'ok',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'set config option failed');
    }
  }

  Future<void> cancel(String sessionId) async {
    final res = await request(
      'session.cancel',
      payload: {'session_id': sessionId},
      expectedType: 'ok',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'cancel failed');
    }
  }

  Future<void> closeSession(String sessionId) async {
    final res = await request(
      'session.close',
      payload: {'session_id': sessionId},
      expectedType: 'ok',
      idempotentRetry: true,
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'close failed');
    }
  }

  /// Remove a session's persisted record on the host. Unlike [closeSession],
  /// the row does not reappear on the next `session.list`.
  Future<void> deleteSession(String sessionId) async {
    final res = await request(
      'session.delete',
      payload: {'session_id': sessionId},
      expectedType: 'ok',
      idempotentRetry: true,
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'delete failed');
    }
  }

  Future<void> respondPermission({
    required String sessionId,
    required String permissionId,
    String? optionId,
    bool cancelled = false,
  }) async {
    final res = await request(
      'permission.respond',
      payload: {
        'session_id': sessionId,
        'permission_id': permissionId,
        'option_id': ?optionId,
        if (cancelled) 'cancelled': true,
      },
      expectedType: 'ok',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'permission failed');
    }
  }

  /// Answer or reject an OpenCode multi-question form (`question.respond`).
  Future<void> respondQuestion({
    required String sessionId,
    required String questionId,
    List<List<String>>? answers,
    bool cancelled = false,
  }) async {
    final res = await request(
      'question.respond',
      payload: {
        'session_id': sessionId,
        'question_id': questionId,
        'answers': ?answers,
        if (cancelled) 'cancelled': true,
      },
      expectedType: 'ok',
    );
    if (res.type == 'error') {
      throw McremoteClient.opException(res, 'question failed');
    }
  }

  Future<void> dispose() async {
    _autoReconnect = false;
    _manualDisconnect = true;
    _userLoggedOut = true;
    _reconnectTimer?.cancel();
    await _teardownSocket();
    await _events.close();
    await _connection.close();
    hostInputListenable.dispose();
  }
}
