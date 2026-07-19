#!/usr/bin/env bash
# Minimal smoke helper — requires websocat and a running mcremote + device token.
set -euo pipefail

TOKEN="${1:?usage: smoke-ws.sh <device-token> [host:port]}"
ADDR="${2:-127.0.0.1:7531}"

echo "healthz:"
curl -fsS "http://${ADDR}/healthz"
echo

AUTH=$(printf '{"v":1,"type":"auth","id":"1","payload":{"token":"%s"}}' "$TOKEN")
CREATE='{"v":1,"type":"session.create","id":"2","payload":{"provider":"fake","name":"smoke"}}'
PROMPT='{"v":1,"type":"session.prompt","id":"3","payload":{"session_id":"REPLACE","text":"hello from smoke"}}'

if ! command -v websocat >/dev/null 2>&1; then
  echo "install websocat to run interactive WS smoke; healthz OK above"
  exit 0
fi

echo "connect ws://${ADDR}/v1/ws and send auth + session.create manually or extend this script"
echo "auth payload: $AUTH"
echo "create payload: $CREATE"
echo "prompt payload template: $PROMPT"
