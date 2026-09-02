import 'dart:async';
import 'dart:io'
    show InternetAddress, InternetAddressType, Platform, SocketException;

import 'package:flutter/foundation.dart'
    show TargetPlatform, defaultTargetPlatform, kDebugMode;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../data/local/settings_store.dart'
    show SecretStoreHealth, SecureStorageUnavailable, SettingsStore;
import '../../data/protocol/pair_uri.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/starfield.dart';
import '../../theme/top_notification.dart';
import 'qr_scan_screen.dart';

/// Emulator-loopback prefill is a developer convenience only: on a real
/// first run it points Test healthz at a dead address, so release builds
/// start the Host field empty (its hint/helper show the expected form).
String _defaultHost() {
  if (!kDebugMode) return '';
  return Platform.isAndroid ? '10.0.2.2:7531' : '127.0.0.1:7531';
}

class ConnectScreen extends ConsumerStatefulWidget {
  const ConnectScreen({super.key});

  @override
  ConsumerState<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends ConsumerState<ConnectScreen> {
  late final TextEditingController _hostCtrl = TextEditingController(
    text: _defaultHost(),
  );

  /// The device token in hand. No longer a visible field (MADR 0064 D4): it
  /// is filled from storage, token QRs, paste, and claim success, and edited
  /// only in Settings — but Connect still dials with it.
  final _tokenCtrl = TextEditingController();
  bool _busy = false;
  bool _autoConnecting = false;
  String? _status;
  bool _statusIsError = false;
  bool _invalidToken = false;

  /// Non-ok verdict from the startup [SettingsStore.probeSecretStore]
  /// (MADR 0066 D2). Rendered as one persistent banner at the top of the
  /// form — never as ambient toasts. Null while ok/unprobed.
  SecretStoreHealth? _storeHealth;

  /// Certificate fingerprint from the most recent pair QR, if it carried one.
  ///
  /// Both modes advertise a pin (ADR 0004); what differs is the rule applied to
  /// it, which is why [_pendingTlsMode] travels with it. Reconnects re-read
  /// both from secure storage, so these are only for the first hop.
  String? _pendingFingerprint;

  /// The TLS mode from the same QR, selecting the acceptance rule for the pin.
  TlsMode? _pendingTlsMode;

  /// The host authority the pending fingerprint was scanned for. If the user
  /// hand-edits the Host field afterwards, the pin must not follow — pinning
  /// host B to host A's certificate guarantees a cert_mismatch (and persists
  /// the wrong pin).
  String? _pendingFor;

  /// The relay route from the same QR. Like the pin, it is credentials for one
  /// daemon: dialling host B through host A's relay sends B's token down A's
  /// tunnel, and persists A's route under B's authority.
  String? _attemptRelayUrl;
  String? _attemptRelayHostId;
  bool _attemptRelaySpecified = false;

  /// What the last probe pass found (MADR 0062 D2). Session-ephemeral: a probe
  /// result is never persisted and never bans a transport, it only decides
  /// whether the user is offered a choice.
  TransportAvailability _availability = TransportAvailability.none;
  bool _probing = false;

  /// The user's pick from the transport menu, honoured only while that menu is
  /// actually on screen (D3).
  TransportMode? _selection;

  /// Coalesces probes while the Host field is being typed into.
  Timer? _probeDebounce;

  /// Set when the failed attempt had already sent a pair code, which is the
  /// one case where offering "try the other transport" is harmful (A1).
  bool _failureSpentCode = false;

  /// The code the last claim used, so Try-other can re-run it. Only ever
  /// re-sent when [_failureSpentCode] is false — see [_buildTryOther].
  String? _lastClaimCode;

  /// A pair code scanned from a QR that has **not** been claimed yet.
  ///
  /// Set only when the transport menu deferred the dial (D5 3a): the code is
  /// one-shot, so it waits in hand until the user picks a path. Without this
  /// the code was simply dropped, and Connect — which needs a *token* — then
  /// answered "Host and token required" for a QR that had just been scanned
  /// successfully.
  String? _pendingPairCode;

  @override
  void initState() {
    super.initState();
    _hostCtrl.addListener(_onHostEdited);
    _load();
  }

  /// True while the Host field is being filled by code rather than typed into.
  ///
  /// Those writes are always followed by an explicit probe pass, so arming the
  /// debounce for them would only leave a timer running for a probe that has
  /// already happened.
  bool _programmaticHostEdit = false;

  void _setHostText(String value) {
    _programmaticHostEdit = true;
    try {
      _hostCtrl.text = value;
    } finally {
      _programmaticHostEdit = false;
    }
  }

  void _onHostEdited() {
    if (_pendingFor != null && _hostCtrl.text.trim() != _pendingFor) {
      _clearPendingPairHints();
    }
    if (_programmaticHostEdit) return;
    // A new authority is a different daemon with different reachability, so
    // the previous probe result says nothing about it.
    _probeDebounce?.cancel();
    _probeDebounce = Timer(
      const Duration(milliseconds: 600),
      () => unawaited(_refreshProbes()),
    );
  }

  /// Credentials in hand, before any probing (D1).
  TransportAvailability _configuredAvailability({
    String? relayUrl,
    String? relayHostId,
  }) {
    final host = _hostCtrl.text.trim();
    String authority;
    try {
      authority = host.isEmpty ? '' : _relayAuthority(host);
    } catch (_) {
      // An unparseable host is not a configured mesh, and cannot own a relay.
      return TransportAvailability.none;
    }
    final url = relayUrl ?? _effectiveRelayUrl;
    final hid = relayHostId ?? _effectiveRelayHostId;
    return transportAvailabilityFromConfig(
      host: host,
      relayUrl: url,
      relayHostId: hid,
      // An attempt tuple arrived with this pairing, so it belongs to this
      // authority by construction; a stored one carries its own owner.
      relayAuthority: _attemptRelaySpecified
          ? authority
          : _storedRelayAuthority,
      hostAuthority: authority,
    );
  }

  String? get _effectiveRelayUrl =>
      _attemptRelaySpecified ? _attemptRelayUrl : _storedRelayUrl;
  String? get _effectiveRelayHostId =>
      _attemptRelaySpecified ? _attemptRelayHostId : _storedRelayHostId;

  String? _storedRelayUrl;
  String? _storedRelayHostId;
  String? _storedRelayAuthority;

  /// Run both probes and fold the result into [_availability].
  ///
  /// Probe failure is not a ban (D2): it hides the menu and picks the other
  /// path, but the user can still force either transport after a *dial*
  /// failure, and a re-probe is one tap away.
  Future<void> _refreshProbes() async {
    if (!mounted) return;
    final configured = _configuredAvailability();
    if (!configured.relayConfigured) {
      // One path, or none: there is nothing a probe could change, and dialling
      // the network to confirm what the user cannot act on is a second of
      // latency spent for nothing.
      setState(() {
        _availability = configured;
        _probing = false;
        _selection = null;
      });
      return;
    }
    setState(() {
      _availability = configured;
      _probing = true;
    });
    try {
      final probes = ref.read(transportProbesProvider);
      final probed = await probes.probe(
        configured: configured,
        host: _hostCtrl.text.trim(),
        relayUrl: _effectiveRelayUrl,
      );
      if (!mounted) return;
      setState(() {
        _availability = probed;
        _probing = false;
        // Drop a selection the menu no longer offers, so a stale pick cannot
        // silently steer the next dial.
        if (!probed.bothAvailable) _selection = null;
      });
    } catch (e) {
      debugPrint('ConnectScreen probes: $e');
      if (mounted) setState(() => _probing = false);
    }
  }

  /// Load the stored relay route so a code-only or token-only flow still knows
  /// whether a relay exists for this host.
  Future<void> _loadStoredRelay() async {
    try {
      final store = ref.read(settingsStoreProvider);
      final url = await store.getRelayUrl();
      final hid = await store.getRelayHostId();
      final authority = await store.getRelayAuthority();
      if (!mounted) return;
      setState(() {
        _storedRelayUrl = url;
        _storedRelayHostId = hid;
        _storedRelayAuthority = authority;
      });
    } catch (e) {
      debugPrint('ConnectScreen stored relay: $e');
    }
  }

  /// The transport to dial with, or null to let the client resolve one.
  ///
  /// Only a visible menu produces an override: when one transport is sole
  /// available the client is told exactly that, and when neither probe passed
  /// the choice is deliberately left to the client, whose dial is a better
  /// test of reachability than the probe that just failed.
  TransportMode? get _effectiveTransport {
    if (_availability.bothAvailable) return _selection ?? TransportMode.mesh;
    final sole = _availability.soleAvailable;
    if (sole != null) return sole;
    // Only one transport has credentials: no probe is needed to know which one
    // the dial must use, and saying so beats making the client re-derive it.
    if (_availability.meshConfigured && !_availability.relayConfigured) {
      return TransportMode.mesh;
    }
    if (_availability.relayConfigured && !_availability.meshConfigured) {
      return TransportMode.relay;
    }
    // Both paired, neither probe answered: leave it to the client, whose dial
    // is a better test of reachability than the probe that just failed.
    return null;
  }

  /// The transport menu — shown only when both paths actually answered (D2).
  ///
  /// Strict on purpose: a menu offering a transport whose probe just failed
  /// invites the user to pick the one path that is known not to work.
  Widget _buildTransportControl(ColorScheme scheme) {
    final selected = _selection ?? TransportMode.mesh;
    final host = _hostCtrl.text.trim();
    final subtitle = selected == TransportMode.mesh
        ? host
        : '${_effectiveRelayUrl ?? ''} · hid=${_effectiveRelayHostId ?? ''}';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 12),
        InputDecorator(
          decoration: InputDecoration(
            labelText: 'Transport',
            border: const OutlineInputBorder(),
            helperText: subtitle.isEmpty ? null : subtitle,
            helperMaxLines: 2,
          ),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 4),
            child: SegmentedButton<TransportMode>(
              segments: const [
                ButtonSegment(
                  value: TransportMode.mesh,
                  label: Text('Mesh'),
                  icon: Icon(Icons.lan_outlined),
                ),
                ButtonSegment(
                  value: TransportMode.relay,
                  label: Text('Relay'),
                  icon: Icon(Icons.cloud_outlined),
                ),
              ],
              selected: {selected},
              onSelectionChanged: _busy
                  ? null
                  : (s) => setState(() => _selection = s.first),
            ),
          ),
        ),
      ],
    );
  }

  /// Status chip for the no-choice cases: one transport up, or neither.
  Widget _buildTransportChip(ColorScheme scheme) {
    if (_probing) {
      return Row(
        children: [
          const SizedBox(
            width: 14,
            height: 14,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
          const SizedBox(width: 10),
          Text(
            'Checking transports…',
            style: TextStyle(color: scheme.onSurfaceVariant, fontSize: 13),
          ),
        ],
      );
    }
    final sole = _availability.soleAvailable;
    if (sole != null) {
      return Row(
        children: [
          Icon(
            sole == TransportMode.mesh
                ? Icons.lan_outlined
                : Icons.cloud_outlined,
            size: 16,
            color: scheme.onSurfaceVariant,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              'Using ${sole.label}',
              style: TextStyle(color: scheme.onSurfaceVariant, fontSize: 13),
            ),
          ),
          TextButton(
            onPressed: _busy ? null : () => unawaited(_refreshProbes()),
            child: const Text('Recheck'),
          ),
        ],
      );
    }
    if (_availability.meshConfigured || _availability.relayConfigured) {
      // Neither answered. Connect stays enabled: a probe is a soft signal (a
      // firewalled healthz, a slow tailnet) and the dial itself is the better
      // test — refusing to try would strand a user whose link actually works.
      return Row(
        children: [
          Icon(Icons.help_outline, size: 16, color: scheme.onSurfaceVariant),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              _availability.bothConfigured
                  ? 'Neither transport answered a probe — Connect will still try both.'
                  : 'No probe answered — Connect will still try.',
              style: TextStyle(color: scheme.onSurfaceVariant, fontSize: 13),
            ),
          ),
          TextButton(
            onPressed: _busy ? null : () => unawaited(_refreshProbes()),
            child: const Text('Recheck'),
          ),
        ],
      );
    }
    return const SizedBox.shrink();
  }

  /// "Try Mesh / Try Relay" after a failed dial.
  ///
  /// Gated on *configured*, not on the probe (D2/D5): the probe that failed is
  /// exactly the thing the user may be overruling. Withheld entirely once a
  /// pair code has gone on the wire (A1) — there the recovery is a new code,
  /// not another transport.
  List<Widget> _buildTryOther(ColorScheme scheme) {
    if (!_availability.bothConfigured) return const [];
    if (_failureSpentCode || _invalidToken) return const [];
    final client = ref.read(mcremoteClientProvider);
    final failed = client.lastTransportSuccess;
    // Offer the transport that is not the one that just succeeded-then-failed;
    // with nothing known, offer both.
    final options = failed == null
        ? TransportMode.values
        : <TransportMode>[failed.other];
    final canClaim = (_lastClaimCode?.isNotEmpty ?? false);
    final canConnect = _tokenCtrl.text.trim().isNotEmpty;
    if (!canClaim && !canConnect) return const [];
    return [
      const SizedBox(height: 12),
      Wrap(
        spacing: 8,
        children: [
          for (final mode in options)
            OutlinedButton.icon(
              onPressed: _busy
                  ? null
                  : () => unawaited(
                      canClaim
                          ? _claimCode(_lastClaimCode!, transport: mode)
                          : _connect(transport: mode),
                    ),
              icon: Icon(
                mode == TransportMode.mesh
                    ? Icons.lan_outlined
                    : Icons.cloud_outlined,
                size: 18,
              ),
              label: Text('Try ${mode.label}'),
            ),
        ],
      ),
    ];
  }

  /// Forget everything the last QR said. Every hint it carries — pin, TLS rule,
  /// relay route — describes one daemon, so once the Host field no longer names
  /// that daemon, or the claim against it failed, none of them apply
  /// (MADR 0046 M-2).
  void _clearPendingPairHints() {
    _pendingFingerprint = null;
    _pendingTlsMode = null;
    _pendingFor = null;
    _attemptRelayUrl = null;
    _attemptRelayHostId = null;
    _attemptRelaySpecified = false;
    _pendingPairCode = null;
  }

  Future<void> _load() async {
    try {
      final store = ref.read(settingsStoreProvider);
      // Probe before the credential reads (MADR 0066 D2): a store the
      // platform silently reset must present as one named banner, not as an
      // inexplicably empty form. The probe self-heals a store that errors
      // while holding nothing readable, so the reads below see its verdict.
      final health = await store.probeSecretStore();
      if (!mounted) return;
      if (health != SecretStoreHealth.ok) {
        setState(() => _storeHealth = health);
      }
      final host = await store.getHost();
      final token = await store.getToken();
      if (!mounted) return;
      if (host != null && host.isNotEmpty) {
        _setHostText(host);
      }
      if (token != null && token.isNotEmpty) {
        _tokenCtrl.text = token;
      }
      // The stored relay route decides whether this host even *has* a second
      // transport, so it has to be in hand before the first probe pass.
      await _loadStoredRelay();
      if (!mounted) return;
      // Surface the transport state for a host that already has both paths
      // paired (D2: probe on connect-screen load with dual config). Unawaited
      // so a cold auto-connect is not held up behind ~1s of probing; the
      // background dial resolves its own path from the sticky value.
      unawaited(_refreshProbes());
      // A store that lost credentials must not auto-dial (MADR 0066 D2/Q1):
      // in the identity-only loss shape the surviving token would dial with
      // a freshly minted key and can only earn `client_key_mismatch` —
      // noise on top of the banner that already carries the recovery.
      if (health == SecretStoreHealth.credentialsLost) return;
      // Cold-start auto-connect when credentials exist.
      // Skip only after an explicit sign-out this process lifetime.
      final client = ref.read(mcremoteClientProvider);
      if (client.userLoggedOut) {
        return;
      }
      if (host != null &&
          host.isNotEmpty &&
          token != null &&
          token.isNotEmpty) {
        if (client.state == McConnectionState.disconnected ||
            client.state == McConnectionState.error ||
            !client.isPaired) {
          setState(() {
            _autoConnecting = true;
            _status = 'Reconnecting with saved credentials…';
            _statusIsError = false;
          });
          try {
            // Take the connection back from the foreground-service isolate
            // before dialling (MADR 0129 D2/C1).
            //
            // This is the path that matters most for it, and it was missed
            // when P4 wired the claim into the *resume* path only: a cold
            // start lands here whenever the Activity is recreated into a
            // process the service kept alive — which is precisely the "swiped
            // away, service took over, user reopens the app" flow. Dialling
            // without the claim put two logins on one device token and the
            // daemon closed one with 4001 (measured 2026-09-02, MADR 0129 P6:
            // `reason=replaced` 5s after an Activity start, with the service
            // isolate logging "connection replaced by a newer login").
            //
            // Never fatal to the dial: a claim that fails or times out means
            // the service did not answer, and refusing to connect then would
            // trade a recoverable 4001 for an app that cannot reach its host
            // at all.
            try {
              await ref
                  .read(notificationCoordinatorProvider)
                  .claimForegroundOwnership();
            } catch (e) {
              debugPrint('cold-start claimForegroundOwnership failed: $e');
            }
            // Cold start is a reconnect, not a menu session (MADR 0062 D5):
            // resolve the path from the sticky value and dial, rather than
            // spending ~1s probing and offering a choice to a user who has not
            // asked for one and may already be past this screen.
            await client.connect(
              hostInput: host,
              token: token,
              userInitiated: false,
            );
            if (!mounted) return;
            _goAfterConnect();
          } catch (e) {
            if (!mounted) return;
            await _handleConnectFailure(e);
            if (mounted) setState(() => _autoConnecting = false);
          }
        } else if (client.isPaired ||
            client.state == McConnectionState.connected ||
            client.state == McConnectionState.reconnecting) {
          if (mounted) _goAfterConnect();
        }
      }
    } catch (e) {
      debugPrint('ConnectScreen._load: $e');
    }
  }

  @override
  void dispose() {
    _probeDebounce?.cancel();
    _hostCtrl.dispose();
    _tokenCtrl.dispose();
    super.dispose();
  }

  String? _errorCode(Object e) {
    if (e is McException) return e.code;
    return ref.read(mcremoteClientProvider).lastErrorCode;
  }

  String _friendlyError(Object e) {
    // On mobile the store refuses to fall back to cleartext, so a keystore
    // failure surfaces here rather than silently writing an unencrypted token.
    if (e is SecureStorageUnavailable) {
      return 'This device\'s secure keystore is unavailable, so the token '
          'cannot be stored safely. Restart the device or re-enrol its screen '
          'lock, then pair again.';
    }
    final code = _errorCode(e);
    switch (code) {
      case 'invalid_token':
        return 'This device token is no longer valid. Generate a new pair '
            'code on the host and use Enter code or Scan QR.';
      case 'expired':
        return 'Pair code expired (5 min). Generate a new one on the host.';
      case 'invalid_code':
        return 'Invalid or already-used pair code.';
      case 'rate_limited':
        return 'Too many failed attempts. Wait and try a new code.';
      case 'client_key_required':
        return 'This device is not enrolled on the host — it has no client '
            'key on record. Re-pair with a fresh QR or pair code to enrol '
            'this device.';
      case 'client_key_mismatch':
        return 'This device\'s key does not match the one enrolled on the '
            'host. If you reinstalled the app or cleared its data, re-pair to '
            'enrol the new key.';
      case 'cert_mismatch':
        // The McException already carries the full rotation-vs-attack
        // explanation and both fingerprints; surface it verbatim.
        if (e is McException) return e.message;
        return 'The host\'s TLS certificate changed. Re-pair only if you '
            'expected it (host rebuild); otherwise the host may be spoofed.';
      case 'cert_unpinned':
        // No trust record for this host (a rotation reset or platform wipe
        // cleared it). A typed code carries no fingerprint, so only the QR
        // can re-establish trust (MADR 0066 incident #3 follow-up) — say
        // that in one line instead of the raw TLS wall of text.
        return 'This host\'s certificate can\'t be verified — no fingerprint '
            'is stored for it. Scan the QR from `mcremote pair code`: a '
            'typed code doesn\'t carry the fingerprint.';
      case 'auth_timeout':
        return 'Timed out authenticating — check host and Tailscale mesh.';
      case 'connect_failed':
        return 'Could not reach host — check host and mesh.';
      case 'no_credentials':
        return 'No saved credentials. Enter a pair code or token.';
    }

    final s = e.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');
    if (s.contains('TimeoutException') || s.contains('timed out')) {
      return 'Timed out — check host and Tailscale mesh.';
    }
    if (s.contains('invalid_token') || s.contains('invalid device token')) {
      return 'This device token is no longer valid. Generate a new pair '
          'code on the host and use Enter code or Scan QR.';
    }
    if (s.contains('invalid pair code') || s.contains('invalid_code')) {
      return 'Invalid or already-used pair code.';
    }
    if (s.contains('expired')) {
      return 'Pair code expired (5 min). Generate a new one on the host.';
    }
    if (s.contains('rate_limited') || s.contains('too many failed')) {
      return 'Too many failed attempts. Wait and try a new code.';
    }
    return s;
  }

  bool _isInvalidTokenError(Object e) {
    if (e is McException && e.isInvalidToken) return true;
    final code = _errorCode(e);
    if (code == 'invalid_token') return true;
    final s = e.toString();
    return s.contains('invalid_token') || s.contains('invalid device token');
  }

  /// The recovery for a client-key error is the same as for a bad token —
  /// re-pair — so it drives the same affordance, but the token stays put: the
  /// token is fine, the device's enrolled key is not.
  bool _needsKeyEnrolment(Object e) {
    final code = _errorCode(e);
    return code == 'client_key_required' || code == 'client_key_mismatch';
  }

  Future<void> _handleConnectFailure(Object e) async {
    // Resolve everything that needs `ref` before the first await — this widget
    // may be disposed (invalid_token redirect) while clearToken() is in flight.
    final invalid = _isInvalidTokenError(e);
    final needsRepair = invalid || _needsKeyEnrolment(e);
    final spentCode = ref.read(mcremoteClientProvider).lastDialSpentCredential;
    var message = spentCode
        // The claim reached the host, so treat the code as consumed even
        // though this device saw a failure. Definite, not hedged (MADR 0064
        // D7): a code that *might* be spent is worthless either way, and
        // saying "retry" — on this or any other transport — sends the user
        // round a loop that can only end in `invalid_code` (0062 A1).
        ? '${_friendlyError(e)}\n\nThat pair code has been used. Ask the '
              'host for a new code (mcremote pair code --name phone), then '
              'scan or enter it.'
        : _friendlyError(e);
    if (_isIOS && _isNetworkShapedError(e)) {
      // Suggestive, never assertive: iOS has no API to distinguish a denied
      // Local Network permission from a dead daemon (TN3179 / MADR 0067 D6).
      message +=
          '\n\nIf iOS asked about devices on your local network and it was '
          'denied, allow Magic CLI Remote under Settings → Privacy & '
          'Security → Local Network, then try again.';
      // Literal-IPv4 mesh hosts cannot be reached from IPv6-only (NAT64)
      // carrier networks — DNS64 cannot synthesize an address for a
      // literal (0068 P5/T8). The relay hop dials a hostname and works;
      // the episode falls back to it automatically when configured.
      final host = _hostCtrl.text.trim();
      final bare = host
          .replaceFirst(RegExp(r'^[a-z]+://'), '')
          .split(':')
          .first;
      if (InternetAddress.tryParse(bare)?.type == InternetAddressType.IPv4) {
        message +=
            '\nOn a cellular/IPv6-only network a numeric host address may '
            'be unreachable — the relay route still works if configured.';
      }
    }
    if (invalid) {
      final store = ref.read(settingsStoreProvider);
      ref.read(mcremoteClientProvider).clearMemoryCredentials();
      await store.clearToken();
      if (!mounted) return;
      _tokenCtrl.clear();
    }
    if (!mounted) return;
    setState(() {
      _status = message;
      _statusIsError = true;
      _invalidToken = needsRepair;
      _failureSpentCode = spentCode;
    });
    if (spentCode) {
      // A failure the user must act on cannot live only at the bottom of a
      // scrollable screen (MADR 0064 D7). The card above keeps the long form
      // with the host command; this carries the fact and the one-tap fix.
      // No Try Mesh / Try Relay here or anywhere: retrying a burnt code can
      // only earn a permanent `invalid_code` (0062 A1).
      showTopNotification(
        context,
        'That pair code has been used. Get a new one and try again.',
        severity: NoticeSeverity.error,
        actionLabel: 'Enter code',
        onAction: () {
          // Fires from the overlay, possibly after this screen is gone.
          if (mounted) unawaited(_enterCode());
        },
      );
    } else if (_needsKeyEnrolment(e)) {
      // The host rejected this phone's key (MADR 0066 D4) — typically the
      // aftermath of a silent secret-store reset regenerating the identity
      // (F1a/F1b). The stale token is half the problem, so the recovery is
      // the full scoped reset, then straight into the pairing flow. The
      // deliberate `invalid_token` clear above stays separate: there the
      // token alone is bad and the enrolled key is fine.
      showTopNotification(
        context,
        'This phone\'s key no longer matches the host. '
        'Reset and re-pair to fix it.',
        severity: NoticeSeverity.error,
        actionLabel: 'Reset & re-pair',
        onAction: () => unawaited(_resetAndRepair()),
      );
    }
  }

  /// One-tap recovery for a key the host no longer accepts (MADR 0066 D4):
  /// scoped credential reset — preferences untouched — then the code sheet.
  Future<void> _resetAndRepair() async {
    if (!mounted) return;
    final store = ref.read(settingsStoreProvider);
    final client = ref.read(mcremoteClientProvider);
    try {
      await store.clearSecrets();
    } on SecureStorageUnavailable catch (e) {
      if (!mounted) return;
      showTopNotification(
        context,
        _friendlyError(e),
        severity: NoticeSeverity.error,
      );
      return;
    }
    client.clearMemoryCredentials();
    if (!mounted) return;
    _tokenCtrl.clear();
    await _enterCode();
  }

  Future<void> _applyPair(PairPayload payload) async {
    // Relay hints belong to this pairing attempt. Do not replace a working
    // route until this QR has authenticated successfully.
    //
    // Host is always the **mcremote** peer (mesh/LAN). Relay is separate
    // (`relay=` + `hid=`) and must not be written into the Host field.
    if (!mounted) return;
    final hostText = SettingsStore.stripFingerprint(payload.host).trim();
    setState(() {
      // Set _pendingFor *before* mutating the controller so _onHostEdited does
      // not treat the QR host fill as a user edit that clears relay/pin hints.
      _pendingFingerprint = payload.fingerprint;
      _pendingTlsMode = payload.mode;
      _pendingFor = hostText;
      _attemptRelayUrl = payload.hasRelay ? payload.relay : null;
      _attemptRelayHostId = payload.hasRelay ? payload.hostId : null;
      // True only when this QR carried a full relay tuple — code-only flows
      // leave this false so a stale route is not forced or wiped incorrectly.
      _attemptRelaySpecified = payload.hasRelay;
      // Preserve the QR's explicit transport signal. A bare host defaults to
      // TLS elsewhere, so dropping ws:// here would silently reinterpret a
      // valid plaintext QR.
      _setHostText(hostText);
      if (payload.hasToken) {
        _tokenCtrl.text = payload.token!;
      }
      // Naming a transport here would be a guess: the QR carries a relay tuple
      // as *configuration*, and which path is dialled is the DialEpisode's
      // answer, reported back through `client.dialProgress` (MADR 0062 D7).
      // Before 0062 this said "via relay …" for every relay QR — including the
      // on-mesh scans that then connected over the mesh.
      _status = 'Checking transports…';
      _statusIsError = false;
      _selection = null;
    });

    // Probe *before* dialling anything (D5). This is the step that turns an
    // off-mesh scan into an immediate relay dial instead of an 8s mesh
    // timeout, and an on-mesh scan into a choice rather than a forced relay.
    await _refreshProbes();
    if (!mounted) return;

    // The pause is only load-bearing for a one-shot *code*, and only when the
    // user chose Select (MADR 0064 D6). A token is idempotent — retrying it is
    // free — so a token QR proceeds in both modes; Auto declines the pause
    // entirely and claims over the mesh default, keeping the DialEpisode's
    // pre-claim relay fallback (0062 D4).
    final mode = await ref.read(settingsStoreProvider).getConnectMode();
    if (!mounted) return;
    if (_availability.bothAvailable && payload.hasCode && mode == 'select') {
      // Both paths work, so the user decides — and nothing is dialled until
      // they do. Auto-claiming here is what made the transport menu
      // unreachable: by the time it rendered, the pairing was already spent on
      // whichever path the old heuristic picked.
      setState(() {
        // Hold the code until the user picks a transport. Connect claims it
        // (see [_onConnectPressed]) — a code-carrying QR has no token, so
        // without this the deferred Connect had nothing to dial with.
        _pendingPairCode = payload.code;
        _status = 'Both transports are up — choose one, then Connect to claim.';
        _statusIsError = false;
      });
      return;
    }

    setState(() {
      _pendingPairCode = null;
      // With both paths up this resolves to the mesh default, so an Auto
      // claim announces the transport it is actually spending the code on.
      final t = _effectiveTransport;
      final via = t == null ? '' : ' over ${t.label.toLowerCase()}';
      _status = payload.hasCode
          ? 'Pair code from QR — claiming$via…'
          : 'Filled from pair QR — connecting$via…';
      _statusIsError = false;
    });

    if (payload.hasCode) {
      await _claimCode(payload.code!);
    } else if (payload.hasToken) {
      await _connect();
    }
  }

  /// A manual pairing supersedes any background auto-connect: the client's
  /// connect epoch abandons the older attempt, so leaving the spinner up would
  /// only misreport what is happening.
  void _supersedeAutoConnect() {
    if (_autoConnecting) setState(() => _autoConnecting = false);
  }

  Future<void> _scanQr() async {
    _supersedeAutoConnect();
    final result = await Navigator.of(
      context,
    ).push<Object>(MaterialPageRoute(builder: (_) => const QrScanScreen()));
    if (!mounted) return;
    if (result == 'enter_code') {
      // The scanner's "Enter code" action: open the sheet directly.
      await _enterCode();
      return;
    }
    if (result is PairPayload) {
      await _applyPair(result);
    }
  }

  Future<void> _pastePairUri() async {
    _supersedeAutoConnect();
    // Reading the clipboard is a platform round-trip; keep the button disabled
    // so a double tap cannot start two paste/claim flows.
    if (_busy) return;
    setState(() => _busy = true);
    final String text;
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      text = data?.text?.trim() ?? '';
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    if (!mounted) return;
    if (text.isEmpty) {
      setState(() {
        _status = 'Clipboard is empty';
        _statusIsError = true;
      });
      return;
    }
    final payload = PairPayload.tryParse(text);
    if (payload != null) {
      await _applyPair(payload);
      return;
    }
    if (text.startsWith('mcr_')) {
      setState(() {
        _tokenCtrl.text = text;
        _status = 'Pasted token — tap Connect';
        _statusIsError = false;
      });
      return;
    }
    if (PairPayload.looksLikePairCode(text)) {
      await _claimCode(text);
      return;
    }
    setState(() {
      _status = 'Clipboard is not a pair URI, code, or mcr_ token';
      _statusIsError = true;
    });
  }

  Future<void> _enterCode() async {
    _supersedeAutoConnect();
    // Tracked via onChanged instead of a controller owned here: the sheet
    // future resolves while the route is still animating out, and disposing a
    // controller then races an active IME composition on the autofocused
    // field. The TextField's own state disposes its internal controller after
    // unmount.
    var entered = '';
    final code = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) {
        return Padding(
          padding: EdgeInsets.only(
            left: 20,
            right: 20,
            top: 20,
            bottom: MediaQuery.of(ctx).viewInsets.bottom + 20,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                'Enter pair code',
                style: Theme.of(ctx).textTheme.titleLarge,
              ),
              const SizedBox(height: 8),
              Text(
                '8 characters from `mcremote pair code` on the host. '
                'Expires in 5 minutes.',
                style: Theme.of(ctx).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(ctx).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 16),
              TextField(
                autofocus: true,
                textCapitalization: TextCapitalization.characters,
                autocorrect: false,
                enableSuggestions: false,
                maxLength: 9, // XXXX-XXXX
                style: monoPairCode,
                textAlign: TextAlign.center,
                decoration: const InputDecoration(
                  hintText: 'XXXX-XXXX',
                  border: OutlineInputBorder(),
                  counterText: '',
                ),
                inputFormatters: [_PairCodeFormatter()],
                onChanged: (v) => entered = v,
                onSubmitted: (v) => Navigator.pop(ctx, v),
              ),
              const SizedBox(height: 16),
              FilledButton(
                onPressed: () => Navigator.pop(ctx, entered),
                child: const Text('Claim & connect'),
              ),
            ],
          ),
        );
      },
    );
    if (code == null || code.trim().isEmpty || !mounted) return;
    await _claimCode(code);
  }

  /// Claim [code]. [transport] forces a path — used by the "Try Mesh / Try
  /// Relay" action, which is only ever offered when the code is still unspent.
  Future<void> _claimCode(String code, {TransportMode? transport}) async {
    final host = _hostCtrl.text.trim();
    if (host.isEmpty) {
      setState(() {
        _status = 'Host is required before claiming a code';
        _statusIsError = true;
      });
      return;
    }
    if (_plaintextBlocked(host)) return;
    if (!PairPayload.looksLikePairCode(code)) {
      setState(() {
        _status = 'Code must be 8 characters (e.g. K7M2-9X4P)';
        _statusIsError = true;
      });
      return;
    }
    setState(() {
      _busy = true;
      _lastClaimCode = code;
      _status = 'Claiming pair code…';
      _statusIsError = false;
    });
    try {
      final store = ref.read(settingsStoreProvider);
      final client = ref.read(mcremoteClientProvider);
      final token = await _withLocalNetRetry(
        () => client.claimPairCode(
          hostInput: host,
          code: code,
          fingerprint: _pendingFingerprint,
          mode: _pendingTlsMode,
          relayUrl: _attemptRelaySpecified ? _attemptRelayUrl : null,
          relayHostId: _attemptRelaySpecified ? _attemptRelayHostId : null,
          transport: transport ?? _effectiveTransport,
        ),
      );
      await store.setHost(host);
      await store.setToken(token);
      if (_attemptRelaySpecified) {
        await store.setRelayRoute(
          url: _attemptRelayUrl,
          hostId: _attemptRelayHostId,
          authority: _relayAuthority(host),
        );
      }
      if (client.deviceId != null) {
        await store.setDeviceId(client.deviceId!);
      }
      if (!mounted) return;
      _tokenCtrl.text = token;
      setState(() => _invalidToken = false);
      _goAfterConnect();
    } catch (e) {
      // This pairing attempt is over, so its hints stop applying: the next
      // Connect may well target a different daemon.
      _clearPendingPairHints();
      // The screen may be gone (auto-connect redirect) before the catch runs,
      // and _handleConnectFailure uses `ref` immediately.
      if (!mounted) return;
      await _handleConnectFailure(e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _testHealth() async {
    if (_plaintextBlocked(_hostCtrl.text.trim())) return;
    setState(() {
      _busy = true;
      _status = 'Checking healthz…';
      _statusIsError = false;
    });
    try {
      final client = ref.read(mcremoteClientProvider);
      final body = await client.healthz(
        _hostCtrl.text.trim(),
        fingerprint: _pendingFingerprint,
        mode: _pendingTlsMode,
      );
      if (!mounted) return;
      setState(() {
        _status = 'Reachable: $body';
        _statusIsError = false;
      });
    } catch (e) {
      // healthz can take 8s; the screen may be gone (auto-connect redirect).
      if (!mounted) return;
      setState(() {
        _status = _friendlyError(e);
        _statusIsError = true;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// What the Connect button does.
  ///
  /// A QR that carried a pair code and was held back for the transport menu
  /// still needs *claiming*, not connecting — it has no token yet. Routing
  /// both through one handler keeps that invisible to the user: they scan,
  /// pick a transport, and press Connect once.
  Future<void> _onConnectPressed() async {
    final pending = _pendingPairCode;
    if (pending != null && pending.isNotEmpty) {
      await _claimCode(pending);
      return;
    }
    await _connect();
  }

  /// Connect with the token in hand. [transport] forces a path (Try-other).
  Future<void> _connect({TransportMode? transport}) async {
    final host = _hostCtrl.text.trim();
    final token = _tokenCtrl.text.trim();
    if (host.isEmpty || token.isEmpty) {
      setState(() {
        _status =
            'Host and token required — or use Enter code / Scan QR instead';
        _statusIsError = true;
      });
      return;
    }
    if (_plaintextBlocked(host)) return;
    setState(() {
      _busy = true;
      _status = 'Connecting…';
      _statusIsError = false;
    });
    try {
      final store = ref.read(settingsStoreProvider);
      final client = ref.read(mcremoteClientProvider);
      await _withLocalNetRetry(
        () => client.connect(
          hostInput: host,
          token: token,
          fingerprint: _pendingFingerprint,
          mode: _pendingTlsMode,
          relayUrl: _attemptRelaySpecified ? _attemptRelayUrl : null,
          relayHostId: _attemptRelaySpecified ? _attemptRelayHostId : null,
          transport: transport ?? _effectiveTransport,
        ),
      );
      await store.setHost(host);
      await store.setToken(token);
      if (_attemptRelaySpecified) {
        await store.setRelayRoute(
          url: _attemptRelayUrl,
          hostId: _attemptRelayHostId,
          authority: _relayAuthority(host),
        );
      }
      if (client.deviceId != null) {
        await store.setDeviceId(client.deviceId!);
      }
      if (!mounted) return;
      setState(() => _invalidToken = false);
      _goAfterConnect();
    } catch (e) {
      // This attempt is over, so its QR hints stop applying (MADR 0046 M-2).
      _clearPendingPairHints();
      // The screen may be gone (auto-connect redirect) before the catch runs,
      // and _handleConnectFailure uses `ref` immediately.
      if (!mounted) return;
      await _handleConnectFailure(e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  static String _relayAuthority(String host) {
    final endpoint = SettingsStore.parseEndpoint(host);
    return '${endpoint.host}:${endpoint.port}';
  }

  /// On phones, plaintext is refused before dialling (MADR 0067 D7 extends
  /// 0046's Android rule to iOS): without this, an iOS user asking for
  /// `ws://` would get an opaque connection failure instead of an
  /// actionable message. Checked via [defaultTargetPlatform] so tests can
  /// exercise both platforms.
  bool _plaintextBlocked(String host) {
    final mobile =
        defaultTargetPlatform == TargetPlatform.android ||
        defaultTargetPlatform == TargetPlatform.iOS;
    if (!mobile) return false;
    try {
      if (SettingsStore.parseEndpoint(host).secure) return false;
    } on ArgumentError {
      // The existing validation path supplies the more specific host error.
      return false;
    }
    final platform = defaultTargetPlatform == TargetPlatform.iOS
        ? 'iOS'
        : 'Android';
    setState(() {
      _status = '$platform requires TLS. Use a wss:// pairing QR.';
      _statusIsError = true;
    });
    return true;
  }

  /// True on iOS only. iOS 14+ local-network privacy (TN3179, MADR 0067 D6):
  /// the dial that first triggers the permission prompt fails, there is no
  /// API to query the permission, and a denied state looks exactly like a
  /// dead daemon.
  bool get _isIOS => defaultTargetPlatform == TargetPlatform.iOS;

  /// One automatic redial has been spent on the local-network prompt.
  bool _localNetRetryUsed = false;

  /// A failure at the socket/route layer — the shape both an untriggered
  /// local-network prompt and a denied permission produce.
  bool _isNetworkShapedError(Object e) {
    if (_errorCode(e) == 'connect_failed') return true;
    if (e is SocketException || e is TimeoutException) return true;
    final s = e.toString();
    return s.contains('SocketException') || s.contains('timed out');
  }

  /// Runs [dial]; on iOS, the first network-shaped failure of this app run
  /// gets one automatic retry after a beat — the prompt-triggering dial
  /// always fails (TN3179), and without this the very first pairing attempt
  /// on a fresh install reads as "host unreachable". Never retries a dial
  /// that spent a pair code: an unsent claim is safe to redial, a claim the
  /// host saw is burnt (0062 A1).
  Future<T> _withLocalNetRetry<T>(Future<T> Function() dial) async {
    try {
      return await dial();
    } catch (e) {
      final spent = ref.read(mcremoteClientProvider).lastDialSpentCredential;
      if (!_isIOS || _localNetRetryUsed || spent || !_isNetworkShapedError(e)) {
        rethrow;
      }
      _localNetRetryUsed = true;
      await Future<void>.delayed(const Duration(milliseconds: 900));
      return await dial();
    }
  }

  Future<void> _clearCredentials() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Clear saved credentials?'),
        content: const Text(
          'Removes the saved host and device token from this phone and signs '
          'out. Agent sessions on the host keep running. You will need a new '
          'pair code or QR to reconnect.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: destructiveFilled(Theme.of(ctx).colorScheme),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Clear & sign out'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final store = ref.read(settingsStoreProvider);
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);
    // Outstanding asks can no longer be answered once the token is gone.
    ref.read(notificationCoordinatorProvider).dropAllAsks();
    // A keystore that refused the delete still holds the token, so the failure
    // has to reach the user rather than being reported as a clean sign-out.
    Object? clearFailure;
    try {
      await store.clearAll();
    } on SecureStorageUnavailable catch (e) {
      clearFailure = e;
    }
    await client.disconnect(manual: true);
    ref.read(pendingNavigationProvider).clear();
    transcripts.clearAll();
    if (!mounted) return;
    _tokenCtrl.clear();
    setState(() {
      _status = clearFailure == null
          ? 'Saved credentials cleared'
          : 'Signed out, but the device keystore refused to erase the saved '
                'credentials — try again.';
      _statusIsError = clearFailure != null;
      _invalidToken = false;
    });
  }

  void _goAfterConnect() {
    final target = ref.read(pendingNavigationProvider).take() ?? '/sessions';
    context.go(target);
  }

  /// Settings is reachable pre-pairing (MADR 0064 D4a) — it owns the
  /// long-lived token and the Connect mode. On return, re-read what it may
  /// have edited: the token always (Settings is its only editor now), the
  /// host only when the field is empty, so a hand-typed authority survives
  /// the round-trip.
  Future<void> _openSettings() async {
    await context.push('/settings');
    if (!mounted) return;
    try {
      final store = ref.read(settingsStoreProvider);
      final token = await store.getToken();
      final host = await store.getHost();
      if (!mounted) return;
      _tokenCtrl.text = token ?? '';
      if (_hostCtrl.text.trim().isEmpty && host != null && host.isNotEmpty) {
        _setHostText(host);
      }
    } catch (e) {
      debugPrint('ConnectScreen settings return: $e');
    }
    unawaited(_refreshProbes());
  }

  /// One persistent error-container card for a secret-store verdict
  /// (MADR 0066 D2). [onAction] null renders without a button.
  Widget _storeHealthBanner(
    ColorScheme scheme, {
    required IconData icon,
    required String title,
    required String body,
    String? actionLabel,
    VoidCallback? onAction,
  }) {
    final text = Theme.of(context).textTheme;
    return Card(
      color: scheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(icon, color: scheme.onErrorContainer),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    title,
                    style: text.titleSmall?.copyWith(
                      color: scheme.onErrorContainer,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              body,
              style: text.bodyMedium?.copyWith(color: scheme.onErrorContainer),
            ),
            if (actionLabel != null) ...[
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton(
                  onPressed: onAction,
                  child: Text(actionLabel),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    // A dial in flight blocks *dialling* — Connect, Test healthz — because a
    // second one would race the first.
    final disabled = _busy || _autoConnecting;
    // …but it must never block the ways back IN. Scan QR, Enter code and Paste
    // are what you reach for precisely when auto-connect is failing, and since
    // MADR 0062 a cold dial is a DialEpisode: mesh `ready` 8 s **plus** relay
    // `ready` 20 s, up to a 35 s budget. Gating these on `_autoConnecting`
    // turned a flicker into half a minute with every pairing route greyed out.
    // A manual pairing supersedes the background attempt by connect epoch,
    // which is what epochs are for (MADR 0046).
    final pairingDisabled = _busy;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Magic CLI Remote'),
        actions: [
          PopupMenuButton<String>(
            onSelected: (v) {
              if (v == 'settings') unawaited(_openSettings());
              if (v == 'clear') unawaited(_clearCredentials());
            },
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'settings', child: Text('Settings')),
              PopupMenuItem(
                value: 'clear',
                child: Text('Clear saved credentials'),
              ),
            ],
          ),
        ],
      ),
      body: Stack(
        children: [
          const Positioned.fill(child: CelestialBackdrop()),
          SafeArea(
            child: ListView(
              padding: const EdgeInsets.all(20),
              children: [
                // No rounded clip: the asset carries an alpha channel, so the
                // monogram sits directly on the backdrop. The clip existed to
                // round off the artwork's baked-in dark square.
                Center(
                  child: Image.asset(
                    'assets/MC_icon.png',
                    width: 96,
                    height: 96,
                    fit: BoxFit.cover,
                  ),
                ),
                const SizedBox(height: 20),
                // MADR 0066 D2: a dead or reset secret store is one persistent,
                // named, action-carrying banner (the Material banner surface) —
                // never ambient error toasts on whatever the user taps next.
                if (_storeHealth == SecretStoreHealth.credentialsLost) ...[
                  _storeHealthBanner(
                    scheme,
                    icon: Icons.key_off,
                    title: 'Stored credentials were reset',
                    body:
                        'This can happen across app updates. Pair again with '
                        'a new code — your hosts and preferences are intact.',
                    actionLabel: 'Enter code',
                    onAction: pairingDisabled
                        ? null
                        : () => unawaited(_enterCode()),
                  ),
                  const SizedBox(height: 12),
                ],
                if (_storeHealth == SecretStoreHealth.broken) ...[
                  _storeHealthBanner(
                    scheme,
                    icon: Icons.lock_outline,
                    title: 'Secure storage unavailable',
                    body:
                        'This device\'s secure keystore is unavailable, so '
                        'the token cannot be stored safely. Restart the '
                        'device or re-enrol its screen lock, then pair again.',
                  ),
                  const SizedBox(height: 12),
                ],
                // The four-step flow, one tap away instead of two lines of
                // permanent prose (MADR 0064 D1). Collapsed always — no state
                // variable, so it can never auto-open on anyone.
                ExpansionTile(
                  title: const Text('Connect to your machine - Steps'),
                  children: [
                    Padding(
                      padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'On the host running mcremote, run:',
                            style: Theme.of(context).textTheme.bodyMedium
                                ?.copyWith(color: scheme.onSurfaceVariant),
                          ),
                          const SizedBox(height: 6),
                          const SelectableText(
                            'mcremote pair code --name <name of device>',
                            style: monoDetail,
                          ),
                          const SizedBox(height: 6),
                          Text(
                            'to generate the QR and short-term code.',
                            style: Theme.of(context).textTheme.bodyMedium
                                ?.copyWith(color: scheme.onSurfaceVariant),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 20),
                Row(
                  children: [
                    Expanded(
                      // Filled, not tonal: these two are peers, and tonal's
                      // secondaryContainer next to a filled primary reads as
                      // greyed-out/unavailable rather than as "secondary".
                      child: FilledButton.icon(
                        onPressed: pairingDisabled ? null : _scanQr,
                        icon: const Icon(Icons.qr_code_scanner),
                        label: const Text('Scan QR'),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: FilledButton.icon(
                        onPressed: pairingDisabled ? null : _enterCode,
                        icon: const Icon(Icons.pin),
                        label: const Text('Enter code'),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 10),
                OutlinedButton.icon(
                  onPressed: pairingDisabled ? null : _pastePairUri,
                  icon: const Icon(Icons.content_paste),
                  label: const Text('Paste URI / code / token'),
                ),
                const SizedBox(height: 24),
                TextField(
                  controller: _hostCtrl,
                  decoration: InputDecoration(
                    labelText: 'Host (mcremote)',
                    hintText: '100.64.0.1:7531',
                    border: const OutlineInputBorder(),
                    helperText: 'Mesh: tailnet IP + :7531 (not Headscale :443)',
                  ),
                  autocorrect: false,
                  enableSuggestions: false,
                  keyboardType: TextInputType.visiblePassword,
                ),
                // Only when the host actually has two paths. A mesh-only
                // pairing has nothing to choose and nothing to diagnose, so
                // "Using Mesh" there would be a permanent row of noise above
                // the Connect button — and on a short screen it pushes Connect
                // itself below the fold.
                if (_availability.relayConfigured) ...[
                  const SizedBox(height: 8),
                  if (_availability.bothAvailable)
                    _buildTransportControl(scheme)
                  else
                    _buildTransportChip(scheme),
                ],
                if (_attemptRelaySpecified &&
                    (_attemptRelayUrl?.isNotEmpty ?? false) &&
                    (_attemptRelayHostId?.isNotEmpty ?? false)) ...[
                  const SizedBox(height: 12),
                  InputDecorator(
                    decoration: const InputDecoration(
                      labelText: 'Relay (mcrelay)',
                      border: OutlineInputBorder(),
                    ),
                    child: SelectableText(
                      '$_attemptRelayUrl  ·  hid=$_attemptRelayHostId',
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                  ),
                ],
                const SizedBox(height: 16),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        onPressed: disabled ? null : _testHealth,
                        child: const Text('Test healthz'),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: FilledButton(
                        onPressed: disabled ? null : _onConnectPressed,
                        child: (_busy || _autoConnecting)
                            ? const SizedBox(
                                width: 18,
                                height: 18,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            // A held pair code makes this button *claim*, not
                            // connect. Saying so is what distinguishes "the
                            // scan worked, now choose a path" from "the scan
                            // did nothing" — the status line alone read as
                            // failure to the first person who used it.
                            : Text(
                                (_pendingPairCode?.isNotEmpty ?? false)
                                    ? 'Claim & connect'
                                    : 'Connect',
                              ),
                      ),
                    ),
                  ],
                ),
                if (_status != null) ...[
                  const SizedBox(height: 20),
                  Card(
                    color: _statusIsError
                        ? scheme.errorContainer
                        : scheme.surfaceContainerHighest,
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          SelectableText(
                            _status!,
                            style: TextStyle(
                              color: _statusIsError
                                  ? scheme.onErrorContainer
                                  : scheme.onSurface,
                            ),
                          ),
                          if (_statusIsError) ..._buildTryOther(scheme),
                          if (_invalidToken) ...[
                            const SizedBox(height: 12),
                            Text(
                              'Host kept. Use Enter code or Scan QR to re-pair.',
                              style: TextStyle(
                                color: scheme.onErrorContainer,
                                fontSize: 13,
                              ),
                            ),
                            const SizedBox(height: 8),
                            Align(
                              alignment: Alignment.centerLeft,
                              child: TextButton(
                                onPressed: disabled
                                    ? null
                                    : () => unawaited(_clearCredentials()),
                                child: Text(
                                  'Clear host & all data',
                                  style: TextStyle(
                                    color: scheme.onErrorContainer,
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Formats as XXXX-XXXX while typing.
class _PairCodeFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
    TextEditingValue oldValue,
    TextEditingValue newValue,
  ) {
    // Count accepted chars left of the caret BEFORE normalising, so the caret
    // can be re-derived instead of jumping to the end (which made fixing a
    // typo mid-code impossible).
    final upper = newValue.text.toUpperCase();
    final keep = RegExp(r'[2-9A-HJ-NP-Z]');
    var acceptedBeforeCaret = 0;
    final caret = newValue.selection.baseOffset.clamp(0, upper.length);
    for (var i = 0; i < caret; i++) {
      if (keep.hasMatch(upper[i])) acceptedBeforeCaret++;
    }

    final raw = upper.replaceAll(RegExp(r'[^2-9A-HJ-NP-Z]'), '');
    final clipped = raw.length > 8 ? raw.substring(0, 8) : raw;
    final buf = StringBuffer();
    for (var i = 0; i < clipped.length; i++) {
      if (i == 4) buf.write('-');
      buf.write(clipped[i]);
    }
    final text = buf.toString();
    var offset = acceptedBeforeCaret.clamp(0, clipped.length);
    if (offset > 4) offset++; // account for the inserted hyphen
    return TextEditingValue(
      text: text,
      selection: TextSelection.collapsed(offset: offset.clamp(0, text.length)),
    );
  }
}
