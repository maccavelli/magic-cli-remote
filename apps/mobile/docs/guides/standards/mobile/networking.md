---
title: "Mobile Networking and TLS Standards"
version: "3.12.2-v2"
last_updated: "2026-07-28"
component: "networking"
---

# Mobile Networking and TLS Standards

## Transport ownership

`McremoteClient` owns the direct daemon connection. `RelayTransport` owns the
optional outer relay WebSocket and loopback bridge; it preserves the original
daemon host in the URL so TLS SNI and certificate checks still apply over the
relay. Do not open raw `web_socket_channel` connections from screens.

- Bound connection, handshake, and loopback-dial operations with timeouts.
- Close channels, `HttpClient`s, sockets, subscriptions, and controllers on
  every success, failure, reconnect, and application-disposal path.
- Treat network, certificate, and protocol errors as typed user-visible states;
  do not replace a pin mismatch with an automatic retry or trust decision.

## Server identity and client identity

- A self-signed daemon is accepted only when the SHA-256 fingerprint of its DER
  certificate matches the pin obtained during pairing. Pins are normalized and
  persisted with their TLS mode, keyed by device identity where available.
- In Let's Encrypt mode, platform trust accepts a valid public chain. If the
  daemon falls back to a self-signed certificate, the pin is the only allowed
  exception; a certificate that neither chains nor matches fails closed.
- The app creates a P-256 client key and self-signed client certificate, stores
  them securely, and presents them through the same `SecurityContext` used for
  server verification. The daemon authorizes the client's SPKI fingerprint.
- Never set `badCertificateCallback` to accept all certificates in a production
  connection. The health probe that temporarily does so is diagnostic only; do
  not copy that exception into authenticated or WebSocket paths.

Changes to pairing, certificate acceptance, relay bridging, or reconnect policy
require tests for rejection, cleanup, and a successful direct and relay path.
