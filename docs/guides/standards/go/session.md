---
title: "Go Session Standards"
version: "1.26.5-v2"
last_updated: "2026-07-28"
component: "session"
---

# Go Session Standards

`internal/session` owns durable and live coding-agent session lifecycle.
Providers implement agent-specific protocol behavior under `internal/provider`;
the WebSocket layer translates authenticated client requests into session work.

## Ownership and lifecycle

- Preserve the distinction between durable metadata and a live provider
  process. Recovery, history, and shutdown paths must work when either side is
  absent.
- Session-scoped events are delivered to the owning paired device. An unknown
  owner fails closed; do not broaden it into a broadcast while handling races.
- Validate limits before allocating processes, buffers, history, or queues.
  Cleanup must be idempotent because clients can disconnect, providers can
  exit, and shutdown can race.
- Provider adapters must not leak provider-specific transport types across the
  public protocol boundary. Add or revise wire messages in `internal/protocol`
  with conformance and compatibility tests.
- Pair codes, device tokens, client certificates, and relay credentials are
  security boundaries. Generate secret material securely, persist it with the
  established stores, and never include it in logs or telemetry.

Use the existing manager, store, and provider tests as the contract before
changing persistence or event ownership behavior.
