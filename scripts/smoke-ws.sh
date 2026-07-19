#!/usr/bin/env bash
# Deprecated helper — use ./scripts/smoke-protocol.sh instead.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "note: prefer ./scripts/smoke-protocol.sh (full auth→prompt→events smoke)" >&2
if [[ $# -ge 1 ]]; then
  exec "$ROOT/scripts/smoke-protocol.sh" -token "$1" -host "${2:-127.0.0.1:7531}"
fi
echo "usage: $0 <device-token> [host:port]" >&2
echo "   or: ./scripts/smoke-protocol.sh -token mcr_… -host 127.0.0.1:7531" >&2
exit 2
