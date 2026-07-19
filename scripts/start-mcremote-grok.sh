#!/usr/bin/env bash
# Start mcremote in Grok mode for Headscale mesh clients (phone).
# Usage:
#   ./scripts/start-mcremote-grok.sh          # start in background
#   ./scripts/start-mcremote-grok.sh --fg     # foreground (logs to terminal)
#   ./scripts/start-mcremote-grok.sh --stop   # stop the managed instance
#   ./scripts/start-mcremote-grok.sh --status
#   ./scripts/start-mcremote-grok.sh --pair [name]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${MCREMOTE_CONFIG:-$ROOT/configs/config.mesh-grok.yaml}"
BIN="${MCREMOTE_BIN:-$ROOT/bin/mcremote}"
DATA_DIR="${MCREMOTE_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/mcremote}"
RUN_DIR="${XDG_RUNTIME_DIR:-/tmp}/mcremote"
PID_FILE="$RUN_DIR/mcremote-grok.pid"
LOG_FILE="$DATA_DIR/mcremote-grok.log"

# Grok ACP binary must be on PATH for the provider.
export PATH="${HOME}/.grok/bin:${HOME}/.local/bin:${PATH}"

mkdir -p "$DATA_DIR" "$RUN_DIR"

die() { echo "error: $*" >&2; exit 1; }

ensure_bin() {
  if [[ ! -x "$BIN" ]]; then
    echo "building mcremote → $BIN"
    mkdir -p "$(dirname "$BIN")"
    (cd "$ROOT" && go build -o "$BIN" ./cmd/mcremote)
  fi
  [[ -x "$BIN" ]] || die "mcremote binary missing: $BIN"
  command -v grok >/dev/null || die "grok not found on PATH (expected under ~/.grok/bin)"
  [[ -f "$CONFIG" ]] || die "config not found: $CONFIG"
}

is_running() {
  if [[ -f "$PID_FILE" ]]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "${pid:-}" ]] && kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
  fi
  return 1
}

cmd_status() {
  if is_running; then
    local pid
    pid="$(cat "$PID_FILE")"
    echo "mcremote-grok: running (pid $pid)"
    echo "  config: $CONFIG"
    echo "  log:    $LOG_FILE"
    echo "  data:   $DATA_DIR"
    ss -ltnp 2>/dev/null | grep -E ':7531\b' || true
    tailscale ip -4 2>/dev/null | awk '{print "  tailnet:", $0":7531"}' || true
  else
    echo "mcremote-grok: not running"
    return 1
  fi
}

cmd_stop() {
  if ! is_running; then
    echo "mcremote-grok: not running"
    rm -f "$PID_FILE"
    return 0
  fi
  local pid
  pid="$(cat "$PID_FILE")"
  echo "stopping mcremote-grok (pid $pid)..."
  kill "$pid" 2>/dev/null || true
  for _ in $(seq 1 20); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.2
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -9 "$pid" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
  echo "stopped"
}

cmd_pair() {
  local name="${1:-phone}"
  ensure_bin
  "$BIN" --config "$CONFIG" pair create --name "$name"
}

cmd_start() {
  local foreground=0
  if [[ "${1:-}" == "--fg" ]]; then
    foreground=1
  fi

  ensure_bin

  if is_running; then
    echo "already running (pid $(cat "$PID_FILE")); use --stop first or --status"
    cmd_status
    return 0
  fi

  # Refuse to double-bind 7531 if something else owns it.
  if ss -ltn 2>/dev/null | grep -qE ':7531\b'; then
    die "port 7531 already in use by another process:\n$(ss -ltnp | grep -E ':7531\b' || true)"
  fi

  local -a args=(
    serve
    --config "$CONFIG"
    --data-dir "$DATA_DIR"
    --listen-host 0.0.0.0
    --listen-port 7531
  )

  echo "starting mcremote (Grok, mesh)..."
  echo "  bin:    $BIN"
  echo "  config: $CONFIG"
  echo "  data:   $DATA_DIR"
  echo "  log:    $LOG_FILE"
  if command -v tailscale >/dev/null && tailscale ip -4 >/dev/null 2>&1; then
    echo "  phone:  $(tailscale ip -4):7531"
  else
    echo "  phone:  <tailnet-ip>:7531"
  fi

  if [[ "$foreground" -eq 1 ]]; then
    exec "$BIN" "${args[@]}"
  fi

  nohup "$BIN" "${args[@]}" >>"$LOG_FILE" 2>&1 &
  local pid=$!
  echo "$pid" >"$PID_FILE"
  sleep 0.5
  if ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$PID_FILE"
    die "process exited immediately; see $LOG_FILE"
  fi
  echo "started pid $pid"
  echo "  status: $0 --status"
  echo "  pair:   $0 --pair phone"
  echo "  stop:   $0 --stop"
  # Quick readiness hint from log
  sleep 0.5
  if [[ -f "$LOG_FILE" ]]; then
    tail -n 15 "$LOG_FILE" || true
  fi
}

case "${1:-}" in
  --stop|stop)   cmd_stop ;;
  --status|status) cmd_status ;;
  --pair|pair)   shift || true; cmd_pair "${1:-phone}" ;;
  --fg|fg)       cmd_start --fg ;;
  --help|-h|help)
    sed -n '2,10p' "$0"
    ;;
  ""|start|--start)
    cmd_start
    ;;
  *)
    die "unknown arg: $1 (try --help)"
    ;;
esac
