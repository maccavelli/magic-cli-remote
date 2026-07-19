#!/usr/bin/env bash
# Headless mcremote.v1 smoke test (no Flutter / no GUI).
#
# Prerequisites:
#   - mcremote serve is running (e.g. ./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531)
#   - device token from: ./bin/mcremote pair create --name smoke
#
# Usage:
#   ./scripts/smoke-protocol.sh -token 'mcr_…'
#   MCREMOTE_TOKEN='mcr_…' ./scripts/smoke-protocol.sh
#   ./scripts/smoke-protocol.sh -token 'mcr_…' -provider fake -prompt 'hi'
#   ./scripts/smoke-protocol.sh -token 'mcr_…' -host 127.0.0.1:7531 -provider grok -cwd "$HOME/gitrepos/magic-cli-remote"
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ ! -f go.mod ]]; then
  echo "error: run from repo (go.mod missing)" >&2
  exit 1
fi

exec go run ./scripts/smoke-protocol "$@"
