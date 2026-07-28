---
title: "Go Networking Standards"
version: "1.26.5-v2"
last_updated: "2026-07-28"
component: "network"
---

# Go Networking Standards

## HTTP and WebSocket control plane

`internal/ws` exposes `GET /healthz`, `GET /v1/hello`, and `GET /v1/ws` using
`net/http` and `github.com/coder/websocket`. Keep protocol parsing, auth, size
limits, and lifecycle ownership in that boundary rather than distributing them
across providers or handlers.

- Authenticate before performing session work; validate every client-supplied
  identifier, path, model name, and payload length.
- Keep the native/same-origin default. Browser origins are allowed only through
  the explicit `auth.allowed_origins` allowlist—never use `*` as a convenience
  setting.
- Maintain bounded inbound and outbound work. The server already has a 1 MiB
  message cap, per-client bounded output queue, write deadline, async-work
  cap, and admission controls. Preserve or tighten these limits when adding a
  message type.
- A slow peer must not block session event delivery. Enqueue per-client output;
  close peers that cannot keep up.
- Close upgraded sockets explicitly during shutdown. `http.Server.Shutdown`
  alone does not close WebSocket connections.

## TLS and relay

The daemon supports `letsencrypt`, `selfsigned`, and `off` TLS modes. ACME uses
certmagic with Route 53 DNS-01 where configured; self-signed peers are paired
through a SHA-256 certificate fingerprint. Keep plaintext opt-in only. Changes
to TLS, authentication, origin handling, or mTLS must include adversarial tests
for rejection as well as happy-path tests.

Pass bounded contexts to network operations and derive outbound work from the
request or server lifecycle context. Do not detach network goroutines from
cancellation.
