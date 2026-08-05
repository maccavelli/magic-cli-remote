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
import '../protocol/transport_policy.dart';
import 'client_identity.dart';
import 'link_health.dart';
import 'mc_exception.dart';
import 'relay_transport.dart';
import 'transport_probes.dart';

/// Resources created by one connection attempt. They remain local until the
/// attempt wins its epoch; a stale attempt may therefore only close its own
/// channel/client/relay and can never tear down the newer connection.
typedef _OpenedSocket = ({
  WebSocketChannel channel,
  HttpClient httpClient,
  RelayTransport? relay,
});

/// Relay credentials in force for one dial, and the availability they imply.
typedef _TransportConfig = ({
  TransportAvailability availability,
  String? relayUrl,
  String? relayHostId,
});

/// Mutable state shared by the legs of one DialEpisode (MADR 0062 D4).
class _EpisodeCtx {
  _EpisodeCtx({required this.epoch, this.oneShotCredential = false});

  /// The connect epoch this episode owns. Bumped **once** per episode, never
  /// per leg: an alternate leg that claimed a fresh epoch would make its own
  /// predecessor's cleanup look like a supersession by a stranger.
  final int epoch;

  /// True when the credential being presented can be consumed by a failed
  /// attempt — a pair code (`internal/auth/paircode.go`, `Take`/`Restore`).
  final bool oneShotCredential;

  /// Set the instant that credential goes on the wire.
  ///
  /// After this point no transport hop is permitted, whatever the error code
  /// says. A `pair.claim` that times out client-side may already have been
  /// taken host-side, so re-sending it over the relay would meet a permanent
  /// `invalid_code` and strand a user whose token exists on the host and was
  /// never delivered (MADR 0062 amendment A1).
  bool credentialSpent = false;

  /// The transport the credential was spent on, and the (normalized) code —
  /// set together with [credentialSpent], consumed by the burn log line
  /// (MADR 0064 D7). The code is already dead by the time it is logged.
  TransportMode? spentOn;
  String? claimCode;
}

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

  /// The acceptance context for one connection: trust roots per mode, plus
  /// this device's client certificate (ADR 0005).
  ///
  /// Shared by [newHttpClient] and by the relay path, which has to secure its
  /// own socket — see [secureSocket]. Both must present the same client
  /// certificate and apply the same trust rule, or the two transports would
  /// authenticate differently.
  SecurityContext newSecurityContext() {
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
    return ctx;
  }

  /// Wrap [raw] in TLS to [host], applying this pinner's rule.
  ///
  /// The relay path cannot delegate this to [HttpClient]. When a
  /// `connectionFactory` is installed, dart:io uses the socket it returns
  /// **as-is** and never secures it — so an `https`/`wss` URL is written to
  /// the wire in cleartext. The daemon rejects it (`client sent an HTTP
  /// request to an HTTPS server`) and the connection fails closed, but the
  /// inner hop is then never established at all.
  ///
  /// [host] must be the real mcremote authority, not the loopback address the
  /// bridge listens on: it is both the SNI name and the name the certificate
  /// is verified against.
  Future<Socket> secureSocket(Socket raw, String host) {
    return SecureSocket.secure(
      raw,
      host: host,
      context: newSecurityContext(),
      // Same rule as newHttpClient: consulted only after platform validation
      // has failed, and only when a pin exists. With no pin a validation
      // failure stays a failure.
      onBadCertificate: pinnedFingerprint == null
          ? null
          : (cert) => _accept(cert, host, 0),
    );
  }

  HttpClient newHttpClient() {
    final ctx = newSecurityContext();
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
    // Not test-only: the app injects the shared probes through
    // `transportProbesProvider` so the connect screen and the client agree on
    // what is reachable.
    TransportProbes? probes,
    @visibleForTesting Duration? protocolPingInterval,
    @visibleForTesting DateTime Function()? clock,
    @visibleForTesting Duration? appPingPeriod,
  }) : _settings = settings ?? SettingsStore(),
       _probes = probes ?? const TransportProbes(),
       _protocolPingInterval = protocolPingInterval ?? kProtocolPingInterval,
       _now = clock ?? DateTime.now,
       _appPingPeriod = appPingPeriod ?? kAppPingPeriod;

  /// Used only to persist/restore the pinned certificate fingerprint, so a
  /// reconnect after process death still pins. Credentials continue to flow in
  /// from the UI layer.
  final SettingsStore _settings;

  /// Test seam for interleaving attempts after a local socket exists but
  /// before it is adopted into shared connection state.
  @visibleForTesting
  final Future<void> Function()? afterSocketOpen;

  /// Soft transport probes (MADR 0062 D2). Only consulted for *interactive*
  /// dials with a genuine choice to make; background dials never probe.
  final TransportProbes _probes;

  /// WebSocket keepalive interval, overridable so tests can drive a missed
  /// pong in milliseconds instead of waiting out the real 20 s.
  final Duration _protocolPingInterval;

  /// Application heartbeat period, overridable for the same reason.
  final Duration _appPingPeriod;

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

  /// Which network the app believes it is on (MADR 0062 D11). Bumped by
  /// [bumpNetworkGeneration] on connectivity change / resume. The transport
  /// fallback budget is scoped to a generation, so a storm of reconnect
  /// triggers on one network cannot buy an unbounded number of mesh↔relay
  /// hops.
  int _networkGeneration = 0;

  /// The generation whose single automatic fallback has already been spent.
  int? _fallbackSpentGeneration;

  /// Sticky transport, cached in memory ahead of [SettingsStore] (D8).
  TransportMode? _lastTransportSuccess;

  /// The transport carrying the live socket, for status display.
  TransportMode? _activeTransport;

  /// Protocol version negotiated with this connection's daemon
  /// (MADR 0068 D1). Reset to v1 on every fresh socket; raised by
  /// auth_ok/pair_ok. Outbound envelopes are stamped with it.
  int _negotiated = kProtocolV1;

  /// The v2 capability block from auth_ok, null on v1 daemons or before
  /// auth. Consumers must fall back to the shipped constants when null.
  ServerCaps? serverCaps;

  /// The transport that was carrying a session when it died (MADR 0063 D6).
  ///
  /// One-shot: the next dial prefers the *other* path rather than retrying the
  /// one that just failed. Without this the reconnect resolves to the sticky
  /// value, which is by definition the transport that has just proven itself
  /// unusable.
  TransportMode? _diedOnTransport;

  /// True while recovering from a transport death, so the recovery episode is
  /// exempt from the per-generation fallback budget (0062 D11, amended by
  /// MADR 0063 review decision 3).
  ///
  /// The budget exists to stop a machine thrashing on *blind* retries. This is
  /// not blind: it responds to a specific observed event. Charging it to the
  /// budget produces the worst case — during a connectivity storm the budget is
  /// already spent, so a genuine transport death cannot fail over and the user
  /// sits disconnected with a working alternative one hop away.
  bool _recoveringFromTransportDeath = false;

  /// Record that the live transport just died, so the next dial avoids it.
  void _noteTransportDeath() {
    final active = _activeTransport;
    if (active == null) return;
    _diedOnTransport = active;
    _recoveringFromTransportDeath = true;
  }

  /// Set by a dial leg whose failure should re-arm the backoff loop, and
  /// consumed by the episode runner once no fallback remains.
  ///
  /// Legs deliberately do not call [_scheduleReconnect] themselves: a first
  /// leg that scheduled its own retry would race the episode's own alternate
  /// leg, and the two would fight over the socket.
  bool _legWantsReconnect = false;

  /// Per-leg progress for the connect/settings status line, so a mesh→relay
  /// episode reads as two deliberate attempts instead of one silent stall
  /// (MADR 0062 §1.3). Null between episodes.
  final ValueNotifier<String?> dialProgress = ValueNotifier<String?>(null);

  /// When the peer last proved it was there (MADR 0063 D1).
  ///
  /// Stamped by **any** inbound frame — a pong, an event, a response — because
  /// each is equally good evidence. A session streaming a reply is verifying
  /// itself continuously and should not need a ping to look healthy.
  DateTime? _lastVerifiedAt;

  /// How confident we are that the link is alive, for the status UI.
  ///
  /// Orthogonal to [state] on purpose (plan amendment B2): `connected` still
  /// means "socket adopted and authenticated" for the 47 call sites that gate
  /// notifications, session sync and reconnect on it. This answers the second
  /// question — "and is it still answering?" — without moving that goalpost.
  final ValueNotifier<LinkHealth> linkHealth = ValueNotifier<LinkHealth>(
    LinkHealth.lost,
  );

  /// Injectable clock so freshness can be tested without waiting.
  final DateTime Function() _now;

  /// Record proof of life. Cheap enough to call on every frame.
  void _noteInboundFrame() {
    _lastVerifiedAt = _now();
    _evaluateHealth();
  }

  /// Re-derive [linkHealth]. Called on every heartbeat tick and whenever the
  /// connection state changes, so the value is never stale by more than one
  /// heartbeat period — a clock crossing a threshold emits no event of its own
  /// (plan amendment B3), which is why the client owns this rather than the UI.
  void _evaluateHealth() {
    final verified = _lastVerifiedAt;
    final socketUp = _state == McConnectionState.connected && _channel != null;
    final next = classifyLinkHealth(
      sinceVerified: verified == null
          ? kLinkDeadAfter
          : _now().difference(verified),
      socketUp: socketUp,
    );
    if (linkHealth.value != next) linkHealth.value = next;
  }

  /// Wall-clock budget for a whole episode. Not enforced by racing a timer
  /// against the dial — that is how a socket gets orphaned (MADR 0046 H-A).
  /// Each leg is already bounded by its own `ready`/request timeouts (8s mesh,
  /// 20s relay), so the budget is applied where it is safe: the alternate leg
  /// is skipped once the deadline has passed.
  static const kDialEpisodeBudget = Duration(seconds: 35);

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
    // A socket that just went away is `lost` immediately — waiting out the
    // freshness window would keep a green light on a connection we know is
    // gone.
    if (s != McConnectionState.connected) _lastVerifiedAt = null;
    _evaluateHealth();
    if (!_connection.isClosed) _connection.add(s);
  }

  /// Drop in-memory credentials: token, the cached client identity, and
  /// optionally the host. Does not touch secure storage.
  ///
  /// Dropping the identity cache is load-bearing for the reset flows
  /// (MADR 0066 D4): a pairing that follows a `clearSecrets` must present
  /// an identity that exists in *storage*. Keeping the cache here let the
  /// claim re-present a keypair whose persisted copy the reset had just
  /// deleted — the host enrolled a RAM-only key, Settings read the identity
  /// as absent, and the next launch was straight back to `credentialsLost`
  /// (0066 recurrence log, incident #3).
  void clearMemoryCredentials({bool host = false}) {
    _lastToken = null;
    _identityFuture = null;
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

  /// Test seams for the identity cache (see [clearMemoryCredentials]).
  @visibleForTesting
  Future<ClientIdentity>? get debugIdentityFuture => _identityFuture;

  @visibleForTesting
  Future<ClientIdentity> debugEnsureIdentity() => _ensureIdentity();

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

  /// Open a pinned WebSocket over [mode]. Throws a typed [McException] on a
  /// pin mismatch. Returns the socket AND its dedicated HttpClient; the caller
  /// assigns both to fields only after confirming it still owns the epoch.
  ///
  /// [TransportMode.relay] opens an outer hop to mcrelay, joins, then dials
  /// mcremote TLS through a loopback bridge (inner hop — pin + client key
  /// still apply to mcremote end-to-end).
  ///
  /// The mode is **required** and never inferred here. Deciding the path from
  /// "relay arguments happen to be present" is what let an attempt-scoped QR
  /// tuple and a half-up tailnet fight over the route; path choice now belongs
  /// to exactly one place, [_runDialEpisode] (MADR 0062 D7).
  Future<_OpenedSocket> _openSocket(
    String url,
    String? pin, {
    required TransportMode mode,
    String? relayUrl,
    String? relayHostId,
  }) async {
    if (mode == TransportMode.relay) {
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
        // Backstop for a blackholed path: dart:io closes the socket when a
        // pong does not arrive, which is the only signal that does not depend
        // on a Dart timer being scheduled on time (MADR 0063 D2).
        pingInterval: _protocolPingInterval,
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
    // Dial loopback for the TCP hop, then **secure it here**.
    //
    // dart:io does not wrap a `connectionFactory` socket in TLS: whatever this
    // returns is used verbatim, so returning the raw loopback socket sent the
    // inner hop's `GET /v1/ws` in cleartext down the tunnel. mcremote answered
    // `client sent an HTTP request to an HTTPS server` and closed, so the
    // relay path could never complete a handshake — it failed closed rather
    // than downgrading, but it failed every time.
    //
    // The URL still carries the real mcremote authority, and that name (not
    // the loopback address) is what the certificate is checked against.
    final innerHost = Uri.parse(url).host;
    httpClient
        .connectionFactory = (Uri uri, String? proxyHost, int? proxyPort) {
      final future =
          Socket.connect(
            InternetAddress.loopbackIPv4,
            transport.localPort,
            timeout: const Duration(seconds: 8),
          ).then((raw) async {
            try {
              return await pinner.secureSocket(raw, innerHost);
            } catch (e) {
              // A failed upgrade leaves the raw socket owning the loopback
              // port; drop it here rather than waiting for the bridge to time
              // out with a half-open peer.
              raw.destroy();
              rethrow;
            }
          });
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
        // Same backstop on the inner hop. These frames ride the tunnel as
        // ordinary TLS records; mcrelay never sees them as anything else.
        pingInterval: _protocolPingInterval,
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
    // A fresh socket starts at v1 until its auth negotiates otherwise
    // (MADR 0068 D1).
    _negotiated = kProtocolV1;
    serverCaps = null;
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

  /// The relay credentials in force for this dial, and what that makes
  /// available (MADR 0062 D1).
  ///
  /// An **attempt-scoped** tuple (both [relayUrl] and [relayHostId] from this
  /// QR/paste) describes the pairing in hand and wins over anything stored. A
  /// *partial* tuple neither invents a path nor falls through to a stored
  /// route for some other pairing — that fall-through is how a code-only
  /// re-pair used to inherit a stale relay.
  ///
  /// Note this no longer decides the path. It reports what is configured;
  /// [_runDialEpisode] chooses, and an attempt tuple being present is not by
  /// itself a reason to use the relay (0061, superseded by 0062 D7).
  Future<_TransportConfig> _resolveTransportConfig(
    String hostInput, {
    String? relayUrl,
    String? relayHostId,
  }) async {
    final authority = _authorityOf(hostInput);
    final attemptUrl = relayUrl?.trim() ?? '';
    final attemptId = relayHostId?.trim() ?? '';

    String? url;
    String? hostId;
    String? relayAuthority;
    if (attemptUrl.isNotEmpty && attemptId.isNotEmpty) {
      url = attemptUrl;
      hostId = attemptId;
      // It arrived with this pairing, so it belongs to this authority.
      relayAuthority = authority;
    } else if (relayUrl == null && relayHostId == null) {
      await _loadRelayHints(hostInput);
      url = _relayUrl;
      hostId = _relayHostId;
      relayAuthority = _relayAuthority;
    }

    return (
      availability: transportAvailabilityFromConfig(
        host: hostInput,
        relayUrl: url,
        relayHostId: hostId,
        relayAuthority: relayAuthority,
        hostAuthority: authority,
      ),
      relayUrl: url,
      relayHostId: hostId,
    );
  }

  /// Run one dial as a **DialEpisode**: resolve a primary transport, dial it,
  /// and — on a retryable failure only — try the other configured transport
  /// exactly once (MADR 0062 D4).
  ///
  /// Every entry point funnels through here: interactive Connect, pair claim,
  /// cold auto-connect, the backoff timer, lifecycle resume and Settings'
  /// Reconnect-now. That is the point — before this, "should I use the relay"
  /// was answered differently at each call site, so a phone could fail over
  /// on one path and not another.
  ///
  /// Three independent gates gate the fallback, and all must pass:
  ///  * the error code is hop-worthy ([isTransportRetryable], D10);
  ///  * the credential has not already been spent ([_EpisodeCtx.credentialSpent],
  ///    amendment A1);
  ///  * a *background* episode has budget left this network generation (D11) —
  ///    [userInitiated] episodes are exempt, because a human tapping "Try
  ///    Relay" is self-rate-limiting and must not be denied by a budget an
  ///    automatic retry already spent (amendment A5).
  Future<T> _runDialEpisode<T>({
    required String hostInput,
    required _EpisodeCtx ctx,
    required Future<T> Function(
      TransportMode mode,
      String? relayUrl,
      String? relayHostId,
    )
    leg,
    TransportMode? explicitMode,
    String? relayUrl,
    String? relayHostId,
    required bool allowTransportFallback,
    required bool userInitiated,
    required bool interactive,
  }) async {
    final deadline = DateTime.now().add(kDialEpisodeBudget);
    _lastDialSpentCredential = false;
    final config = await _resolveTransportConfig(
      hostInput,
      relayUrl: relayUrl,
      relayHostId: relayHostId,
    );
    var availability = config.availability;

    final primary =
        explicitMode ??
        await _resolvePrimaryTransport(
          hostInput: hostInput,
          config: config,
          interactive: interactive,
          onProbed: (probed) => availability = probed,
        );
    if (primary == null) {
      throw McException(
        'no transport configured for $hostInput — pair again to add a route',
        code: 'no_transport',
        permanent: true,
      );
    }

    final alternate = primary.other;
    final alternateConfigured = alternate == TransportMode.mesh
        ? availability.meshConfigured
        : availability.relayConfigured;

    _dialProgress('Connecting over ${primary.label.toLowerCase()}…');
    try {
      final result = await leg(primary, config.relayUrl, config.relayHostId);
      await _recordTransportSuccess(hostInput, primary);
      return result;
    } catch (e) {
      if (_staleAttempt(ctx.epoch)) rethrow;
      final code = e is McException ? e.code : null;
      final budgetSpent = _fallbackSpentGeneration == _networkGeneration;
      final mayFallback =
          allowTransportFallback &&
          alternateConfigured &&
          !ctx.credentialSpent &&
          isTransportRetryable(code) &&
          (userInitiated || _recoveringFromTransportDeath || !budgetSpent) &&
          DateTime.now().isBefore(deadline);

      if (!mayFallback) {
        _finishFailedEpisode(e, ctx: ctx);
        rethrow;
      }

      _fallbackSpentGeneration = _networkGeneration;
      debugPrint(
        'mcremote: ${primary.label} dial failed (${code ?? 'unknown'}); '
        'trying ${alternate.label}',
      );
      _dialProgress(
        '${primary.label} failed — trying ${alternate.label.toLowerCase()}…',
      );
      try {
        final result = await leg(
          alternate,
          config.relayUrl,
          config.relayHostId,
        );
        await _recordTransportSuccess(hostInput, alternate);
        return result;
      } catch (e2) {
        if (!_staleAttempt(ctx.epoch)) {
          _finishFailedEpisode(e2, ctx: ctx);
        }
        rethrow;
      }
    } finally {
      if (_state == McConnectionState.connected) _dialProgress(null);
    }
  }

  /// Primary transport for an episode with no mode supplied by the caller.
  ///
  /// Interactive dials with a real choice to make pay for probes; background
  /// dials never do (D4). "A real choice" means both transports are
  /// configured — with only one set of credentials there is nothing a probe
  /// could change, and ~1s is a lot to spend confirming it.
  Future<TransportMode?> _resolvePrimaryTransport({
    required String hostInput,
    required _TransportConfig config,
    required bool interactive,
    required void Function(TransportAvailability) onProbed,
  }) async {
    // A transport that just died under a live session is the worst possible
    // primary, and it is exactly what the sticky value would pick. Prefer the
    // other configured path once, then let sticky take over again (D6).
    final died = _diedOnTransport;
    _diedOnTransport = null;
    if (died != null) {
      final alternate = died.other;
      final alternateConfigured = alternate == TransportMode.mesh
          ? config.availability.meshConfigured
          : config.availability.relayConfigured;
      if (alternateConfigured) {
        debugPrint(
          'mcremote: ${died.label} died under a live session; '
          'reconnecting over ${alternate.label}',
        );
        return alternate;
      }
    }

    final sticky = await _stickyTransport(hostInput);
    if (!interactive || !config.availability.bothConfigured) {
      return resolveBackground(
        availability: config.availability,
        sticky: sticky,
      );
    }

    final probed = await _probes.probe(
      configured: config.availability,
      host: hostInput,
      relayUrl: config.relayUrl,
    );
    onProbed(probed);
    final interactiveChoice = resolveInteractive(
      availability: probed,
      userSelection: sticky,
    );
    // Both probes failed. A probe is a soft signal — healthz can be firewalled
    // while the WebSocket is fine — so this refuses to turn a false negative
    // into a refusal to dial. The dial is attempted anyway, and the fallback
    // still covers the wrong guess. Surfacing `no_transport` to the *user* on
    // a dual probe failure is the connect screen's job, where the diagnostics
    // are visible and Connect can be re-armed (D1/D2).
    return interactiveChoice ??
        resolveBackground(availability: config.availability, sticky: sticky);
  }

  /// Sticky transport for [hostInput], memory first, then storage (D8).
  Future<TransportMode?> _stickyTransport(String hostInput) async {
    final cached = _lastTransportSuccess;
    if (cached != null) return cached;
    try {
      return await _settings.getLastTransportSuccess(_authorityOf(hostInput));
    } catch (_) {
      return null;
    }
  }

  /// Persist the transport that just reached auth. Only auth success updates
  /// the sticky value — never a probe, which would let an edge that is merely
  /// up outrank a path that actually carried a session (D11).
  Future<void> _recordTransportSuccess(
    String hostInput,
    TransportMode mode,
  ) async {
    _activeTransport = mode;
    _lastTransportSuccess = mode;
    _recoveringFromTransportDeath = false;
    _diedOnTransport = null;
    _dialProgress(null);
    try {
      await _settings.setLastTransportSuccess(mode, _authorityOf(hostInput));
    } catch (e) {
      // A preference that fails to persist costs one suboptimal primary on the
      // next cold start, and must not fail a connection that is already up.
      debugPrint('mcremote: could not persist sticky transport: $e');
    }
  }

  /// Terminal handling once an episode has no fallback left.
  ///
  /// Legs record *what* failed; the decision to re-arm the backoff loop or to
  /// stop dialling lands here, after the last leg, so a first-leg failure can
  /// never schedule a retry that races its own episode's alternate.
  void _finishFailedEpisode(Object e, {required _EpisodeCtx ctx}) {
    final spentCredential = ctx.credentialSpent;
    final permanent = e is McException && e.permanent;
    _lastDialSpentCredential = spentCredential;
    if (spentCredential) {
      // One stable line per burn (MADR 0064 D7): the host may hold a token
      // this phone never received, visible as an orphan in `mcremote pair
      // list`. The code is dead by definition here, so logging it costs
      // nothing and buys host-side correlation. Emitted exactly once — this
      // method runs once per failed episode (V14).
      debugPrint(
        'mcremote: pair code spent without token '
        '(transport=${ctx.spentOn?.name}, code=${ctx.claimCode})',
      );
      // The pair code may already be consumed host-side. Retrying it on any
      // transport meets `invalid_code`; the user needs a fresh code, so park
      // rather than loop (amendment A1).
      _legWantsReconnect = false;
    }
    if (permanent) {
      _autoReconnect = false;
    } else if (_legWantsReconnect) {
      _scheduleReconnect();
    }
    _legWantsReconnect = false;
  }

  void _dialProgress(String? message) {
    if (dialProgress.value != message) dialProgress.value = message;
  }

  /// Start a new network generation, refreshing the transport fallback budget
  /// (MADR 0062 D11). Call *before* the reconnect it accompanies.
  ///
  /// Coalescing is deliberate: a burst of connectivity callbacks that all land
  /// before the reconnect shares one generation and therefore one fallback,
  /// which is what stops airplane-mode toggling from becoming a mesh↔relay
  /// thrash loop.
  void bumpNetworkGeneration() {
    _networkGeneration++;
  }

  /// The transport carrying the live socket, if any.
  TransportMode? get activeTransport =>
      _state == McConnectionState.connected ? _activeTransport : null;

  /// The transport that last reached auth (memory copy of the sticky value).
  TransportMode? get lastTransportSuccess => _lastTransportSuccess;

  /// True when the most recent failed episode had already committed a one-shot
  /// credential (a pair code) to the wire.
  ///
  /// The UI uses this to *withhold* the "Try Mesh / Try Relay" action: the
  /// host may have consumed the code, so another transport would only earn a
  /// permanent `invalid_code`. What the user needs is a fresh code, and
  /// offering a retry button instead sends them round a loop that cannot
  /// succeed (MADR 0062 amendment A1).
  bool get lastDialSpentCredential => _lastDialSpentCredential;
  bool _lastDialSpentCredential = false;

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
  /// The implementation moved to [probeMeshReachable] so the connect screen and
  /// settings can run it without holding a client; this stays as the name the
  /// rest of the app and its tests already use.
  @visibleForTesting
  static Future<bool> probeDirectReachable(
    String hostInput, {
    Duration timeout = kTransportProbeTimeout,
  }) => probeMeshReachable(hostInput, timeout: timeout);

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

  /// Connect with a durable token.
  ///
  /// [transport] forces the first leg; null lets the episode resolve one from
  /// the sticky value, the configuration and (for a user-initiated dial) a
  /// probe. Relay arguments only *configure* the relay — they no longer imply
  /// "use it" (MADR 0062 D7).
  Future<void> connect({
    required String hostInput,
    required String token,
    String? fingerprint,
    TlsMode? mode,
    String? relayUrl,
    String? relayHostId,
    bool enableAutoReconnect = true,
    TransportMode? transport,
    bool allowTransportFallback = true,
    bool userInitiated = true,
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
      transport: transport,
      allowTransportFallback: allowTransportFallback,
      userInitiated: userInitiated,
    );
  }

  /// Claim an 8-char pair code, receive durable token, and stay connected.
  ///
  /// Runs as a DialEpisode whose credential is **one-shot**: the daemon takes
  /// the code out of its store when the claim arrives and only puts it back if
  /// the device-create then fails. So a hop is allowed while the code is still
  /// in the phone's hand, and forbidden the moment it goes on the wire
  /// (MADR 0062 amendment A1) — see [_EpisodeCtx.credentialSpent].
  Future<String> claimPairCode({
    required String hostInput,
    required String code,
    String? name,
    String? fingerprint,
    TlsMode? mode,
    String? relayUrl,
    String? relayHostId,
    TransportMode? transport,
    bool allowTransportFallback = true,
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

    final ctx = _EpisodeCtx(epoch: epoch, oneShotCredential: true);
    return _runDialEpisode<String>(
      hostInput: hostInput,
      ctx: ctx,
      explicitMode: transport,
      relayUrl: relayUrl,
      relayHostId: relayHostId,
      allowTransportFallback: allowTransportFallback,
      // A claim is always something the user just did.
      userInitiated: true,
      interactive: true,
      leg: (legMode, legRelayUrl, legRelayHostId) => _claimLeg(
        hostInput: hostInput,
        code: code,
        name: name,
        pin: pin,
        ctx: ctx,
        mode: legMode,
        relayUrl: legRelayUrl,
        relayHostId: legRelayHostId,
      ),
    );
  }

  /// One pair-claim attempt over exactly one transport.
  Future<String> _claimLeg({
    required String hostInput,
    required String code,
    required String? name,
    required String? pin,
    required _EpisodeCtx ctx,
    required TransportMode mode,
    String? relayUrl,
    String? relayHostId,
  }) async {
    final epoch = ctx.epoch;
    await _teardownSocket(suppressReconnect: true);
    if (_staleAttempt(epoch)) {
      throw McException('pairing superseded', code: 'pair_failed');
    }
    _setState(McConnectionState.connecting);

    final _OpenedSocket opened;
    try {
      opened = await _openSocket(
        wsUrl!,
        pin,
        mode: mode,
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
      // The point of no return. From here the host may have consumed the code
      // regardless of what this device observes, so no failure below may be
      // retried on another transport (amendment A1).
      ctx.credentialSpent = true;
      ctx.spentOn = mode;
      ctx.claimCode = normalized;
      final res = await request(
        'pair.claim',
        payload: {
          'code': normalized,
          if (name != null && name.isNotEmpty) 'name': name,
          'protocols': kSupportedProtocols,
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
      // pair_ok can negotiate too (MADR 0068 D1) — the claim connection may
      // keep talking after enrolment.
      final pairNegotiated = (res.payload?['protocol'] as num?)?.toInt();
      if (pairNegotiated != null &&
          pairNegotiated >= kProtocolV1 &&
          pairNegotiated <= kProtocolV2) {
        _negotiated = pairNegotiated;
      }

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
      // Only adopt an explicit attempt route. Passing nulls used to wipe a
      // good in-memory relay after a code-only re-pair against the same host.
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
      _noteInboundFrame();
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

  /// Connect with a token, as a DialEpisode (see [_runDialEpisode]).
  ///
  /// The epoch is claimed here, once, and shared by both legs.
  Future<void> _connectInternal({
    required String hostInput,
    required String token,
    String? relayUrl,
    String? relayHostId,
    TransportMode? transport,
    bool allowTransportFallback = true,
    bool userInitiated = false,
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

    final ctx = _EpisodeCtx(epoch: epoch);
    await _runDialEpisode<void>(
      hostInput: hostInput,
      ctx: ctx,
      explicitMode: transport,
      relayUrl: relayUrl,
      relayHostId: relayHostId,
      allowTransportFallback: allowTransportFallback,
      userInitiated: userInitiated,
      // A token connect is idempotent, so an interactive one can afford to
      // probe for the better path before spending an 8s mesh timeout on a
      // link that is plainly down.
      interactive: userInitiated,
      leg: (mode, legRelayUrl, legRelayHostId) => _connectLeg(
        hostInput: hostInput,
        token: token,
        pin: pin,
        ctx: ctx,
        mode: mode,
        relayUrl: legRelayUrl,
        relayHostId: legRelayHostId,
      ),
    );
  }

  /// One connect attempt over exactly one transport. Throws on failure; the
  /// episode runner decides whether anything follows.
  Future<void> _connectLeg({
    required String hostInput,
    required String token,
    required String? pin,
    required _EpisodeCtx ctx,
    required TransportMode mode,
    String? relayUrl,
    String? relayHostId,
  }) async {
    final epoch = ctx.epoch;
    // A second leg inherits the first leg's wreckage; start from a clean slate.
    await _teardownSocket(suppressReconnect: true);
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
        mode: mode,
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
      // A cert mismatch (or an unpinned self-signed host) will not fix itself
      // by retrying, and retrying an unverified peer is exactly what pinning
      // exists to prevent — the runner fails closed on `permanent`. Everything
      // else re-arms the backoff loop, but only once this episode is out of
      // alternates (see [_finishFailedEpisode]).
      if (!permanent) _legWantsReconnect = true;
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
        payload: {'token': token, 'protocols': kSupportedProtocols},
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

      // Negotiated version + capability block (MADR 0068 D1). A v1 daemon
      // sends neither; every consumer of [serverCaps] falls back to the
      // shipped constants on null.
      final negotiated = (auth.payload?['protocol'] as num?)?.toInt();
      if (negotiated != null &&
          negotiated >= kProtocolV1 &&
          negotiated <= kProtocolV2) {
        _negotiated = negotiated;
      }
      serverCaps = ServerCaps.tryParse(auth.payload?['caps']);

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
      _noteInboundFrame();
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
      // Network / unexpected during handshake: tear down, allow reconnect —
      // after the episode has exhausted its alternates.
      lastError = e.toString();
      _setState(McConnectionState.error);
      await _teardownSocket(suppressReconnect: true);
      _suppressReconnect = false;
      _legWantsReconnect = true;
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
    _pingTimer = Timer.periodic(_appPingPeriod, (_) {
      // The freshness clock crosses its thresholds silently, so the value is
      // re-derived on the timer that is already running rather than by a
      // separate ticker in the UI (plan amendment B3).
      _evaluateHealth();
      if (_state == McConnectionState.connected) {
        // **Unconditional — this is a protocol obligation** (MADR 0063 plan
        // amendment B1). The daemon's read loop waits for a *data* message
        // (`internal/ws/server.go:535`, 60 s deadline at `:165`); the
        // WebSocket keepalive of D2 is answered below the application and
        // never satisfies that read. This request is the only thing holding
        // the host's deadline open.
        //
        // Do not make it conditional on freshness to save a wakeup: a session
        // streaming a long reply receives constantly and sends nothing, so
        // skipping the ping would have the daemon drop it mid-answer — which
        // presents as the agent hanging.
        final pingEpoch = _connectEpoch;
        unawaited(
          request('ping', timeout: kAppPingTimeout)
              .then((_) {
                _missedPings = 0; // reset on success
                _noteInboundFrame();
              })
              .catchError((Object e) {
                if (pingEpoch != _connectEpoch) return;
                _missedPings++;
                // First miss is advisory: the UI drops out of green via the
                // freshness clock, but the socket is left alone. Bouncing a
                // live connection on one lost packet is exactly the flap that
                // MADR 0046 L-1 fixed, so the teardown still needs two.
                _evaluateHealth();
                lastError = e.toString();
                if (_missedPings >= 2 &&
                    !_manualDisconnect &&
                    !_userLoggedOut &&
                    hasCredentials) {
                  _missedPings = 0;
                  _noteTransportDeath();
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
                // A missed pong is handled entirely by the reconnect
                // side-effects above; this catchError only exists to swallow
                // the rejection, so it yields null (the chain is unawaited and
                // its value is discarded).
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
  /// [urgent] shortens the probe for the case where the transport's own
  /// interface has just disappeared — there is no point waiting 5 s to confirm
  /// what the OS has already told us (MADR 0063 D4).
  Future<void> probeLiveness({bool urgent = false}) async {
    if (_state != McConnectionState.connected) return;
    if (urgent) {
      // Stop claiming health immediately; the probe below decides the rest.
      _lastVerifiedAt = null;
      _evaluateHealth();
    }
    // Same rule as the periodic ping: the connection this probe is about is
    // the one live when it was sent (MADR 0046 L-1).
    final probeEpoch = _connectEpoch;
    try {
      await request(
        'ping',
        timeout: urgent
            ? const Duration(seconds: 2)
            : const Duration(seconds: 5),
      );
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
    if (_state == McConnectionState.connected) _noteTransportDeath();
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
    // Replaced (MADR 0068 D3, close code 4001): a newer connection of this
    // device authenticated and the daemon closed this one. Reconnecting
    // would fight the newer login, so park quietly instead — not an error,
    // not a transport death. Pairing stays intact; the next *user-driven*
    // action (foreground resume, Reconnect now) dials again and kicks the
    // other side in turn.
    if (_channel?.closeCode == kCloseReplaced) {
      _failAllPending('connection replaced');
      debugPrint('mcremote: connection replaced by a newer login');
      _setState(McConnectionState.disconnected);
      return;
    }
    // A socket that closes under a live session is the transport failing, not
    // the host refusing: remember which path it was so the reconnect does not
    // immediately retry it (MADR 0063 D6).
    if (_state == McConnectionState.connected) _noteTransportDeath();
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
  ///
  /// [transport] forces the first leg — this is what Settings' "Reconnect now"
  /// uses to move a live session onto the other path without re-pairing. A
  /// forced reconnect is [userInitiated], so it keeps its own fallback even if
  /// an automatic one already ran this network generation (amendment A5).
  Future<void> reconnect({
    String? hostInput,
    String? token,
    TransportMode? transport,
    bool allowTransportFallback = true,
    bool userInitiated = true,
  }) async {
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
    await _connectInternal(
      hostInput: host,
      token: tok,
      transport: transport,
      allowTransportFallback: allowTransportFallback,
      userInitiated: userInitiated,
    );
  }

  /// Load host/token from [store] if memory is empty, then reconnect.
  ///
  /// Background by default: no probes, sticky-first path selection, and the
  /// per-generation fallback budget applies. Lifecycle triggers call
  /// [bumpNetworkGeneration] first so a genuinely new network gets a fresh
  /// budget while a burst on one network shares a single one.
  Future<void> reconnectFromStore(
    SettingsStore store, {
    TransportMode? transport,
    bool userInitiated = false,
  }) async {
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
    await reconnect(
      hostInput: host,
      token: tok,
      transport: transport,
      userInitiated: userInitiated,
    );
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
    _activeTransport = null;
    _dialProgress(null);
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
    // The negotiated version and capability block are per-connection
    // (MADR 0068 D1); the next socket re-negotiates from v1.
    _negotiated = kProtocolV1;
    serverCaps = null;
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
    // Before any parsing: a frame we cannot decode is still proof that the
    // peer is there. Stamping after the parse would let a protocol bug read
    // as a dead link.
    _noteInboundFrame();
    try {
      if (data is! String) {
        debugPrint('mcremote: ignoring non-text frame (${data.runtimeType})');
        return;
      }
      final map = jsonDecode(data) as Map<String, dynamic>;
      final env = Envelope.fromJson(map);
      // Accept anything up to the highest version we offer (MADR 0068 D1):
      // the auth_ok that *concludes* negotiation is already stamped with the
      // picked version, so gating on `_negotiated` here would reject it.
      // The server never sends above what it granted this connection.
      if (env.v < kProtocolV1 || env.v > kProtocolV2) {
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
      // Stamped with the negotiated version (MADR 0068 D1); still v1 for
      // the auth/pair.claim frames that perform the negotiation itself.
      v: _negotiated,
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
    // Gap-scaling surface (MADR 0068 P3); absent on pre-P3 daemons.
    final seqs = <String, SeqBounds>{};
    final seqsRaw = payload['seqs'];
    if (seqsRaw is Map) {
      seqsRaw.forEach((key, value) {
        if (key is String && value is Map) {
          final first = (value['first_seq'] as num?)?.toInt() ?? 0;
          final latest = (value['latest_seq'] as num?)?.toInt() ?? 0;
          if (latest > 0) {
            seqs[key] = SeqBounds(first: first, latest: latest);
          }
        }
      });
    }
    return SessionListSnapshot(
      sessions: sessions,
      complete: complete,
      degraded: degraded,
      skipped: skipped,
      epoch: payload['epoch'] as String?,
      seqs: seqs,
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
    dialProgress.dispose();
    linkHealth.dispose();
  }
}
