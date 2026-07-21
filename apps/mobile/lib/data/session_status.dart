/// Human-friendly label for a raw session/turn status string, so the UI shows
/// "Working…" / "Idle" instead of leaking the wire vocabulary (`running`,
/// `idle`, …). Unknown values are title-cased rather than dropped.
String humanSessionStatus(String raw) {
  switch (raw) {
    case 'running':
      return 'Working…';
    case 'idle':
      return 'Idle';
    case 'error':
      return 'Error';
    case 'disconnected':
      return 'Disconnected';
    case 'connecting':
      return 'Connecting…';
    case 'authenticating':
      return 'Authenticating…';
    case 'reconnecting':
      return 'Reconnecting…';
    case 'cancelled':
    case 'canceled':
      return 'Cancelled';
    case '':
      return '—';
    default:
      return raw[0].toUpperCase() + raw.substring(1);
  }
}
