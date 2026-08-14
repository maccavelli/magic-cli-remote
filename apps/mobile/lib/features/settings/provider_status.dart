/// Shared status folding for the provider surfaces (MADR 0082 D2/D3).
library;

import '../../data/protocol/models.dart';

/// Agent-level chip (MADR 0086 D3): `error > quota > configured`, never
/// `missing` just because some catalog row has no key. Per-row chips stay
/// honest via [StatusChip.auth] on each upstream.
String agentAuthStatus(ProviderAuthInfo auth) {
  if (auth.upstreams.isEmpty) {
    return auth.status.isEmpty ? AuthStatus.missing : auth.status;
  }
  var sawQuota = false;
  var sawConfigured = false;
  for (final up in auth.upstreams) {
    if (up.status == AuthStatus.error) return AuthStatus.error;
    if (up.status == AuthStatus.quota) sawQuota = true;
    if (up.isConfigured) sawConfigured = true;
  }
  if (sawQuota) return AuthStatus.quota;
  if (sawConfigured) return AuthStatus.configured;
  return AuthStatus.missing;
}

/// Kept as a name the older tests/callers use; folds through [agentAuthStatus].
String worstAuthStatus(ProviderAuthInfo auth) => agentAuthStatus(auth);

/// Hub-spoke subtitle anomaly: the first agent whose worst status needs a
/// mention, or null when everything is quiet.
String? firstAuthAnomaly(List<ProviderInfo> providers) {
  for (final p in providers) {
    final auth = p.auth;
    if (auth == null) continue;
    final worst = worstAuthStatus(auth);
    if (worst == AuthStatus.quota) return '${p.id} quota reached';
    if (worst == AuthStatus.error) return '${p.id} credential error';
  }
  return null;
}
