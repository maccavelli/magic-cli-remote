/// Shared status folding for the provider surfaces (MADR 0082 D2/D3).
library;

import '../../data/protocol/models.dart';

/// The single status that best summarises an agent's credential picture:
/// `error > quota > missing > configured`. An agent with one broken upstream
/// among ten healthy ones needs attention, not a green chip.
String worstAuthStatus(ProviderAuthInfo auth) {
  var worst = AuthStatus.configured;
  var rank = 0;
  const ranks = {
    AuthStatus.configured: 0,
    AuthStatus.missing: 1,
    AuthStatus.quota: 2,
    AuthStatus.error: 3,
  };
  for (final up in auth.upstreams) {
    final r = ranks[up.status] ?? 1;
    if (r > rank) {
      rank = r;
      worst = up.status;
    }
  }
  return worst;
}

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
