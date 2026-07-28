# MADR 0001: Architecture for Multi-CLI Remote Control Orchestrator (mcremote)

- **Status**: Accepted
- **Date**: 2026-07-19
- **Deciders**: Project Owner
- **Technical Story**: Build a provider-agnostic Go daemon named `mcremote` (part of the `magic-cli-remote` project) that can attach to multiple coding agent CLIs (primarily Grok Build, secondarily Google Antigravity / `agy`), multiplex sessions, and expose secure remote control to a Flutter mobile application.

## Context and Problem Statement

Developers run powerful coding agents (Grok Build, Antigravity CLI, etc.) on local machines or internal network hosts. These machines are frequently behind VPNs or firewalls that mobile devices cannot join. Users want to monitor sessions, inject prompts, approve tool calls, and manage multiple agents from their phone.

We need a reliable, secure, and extensible architecture that:

- Runs as a long-lived daemon (`mcremote`) next to the CLIs
- Supports multiple agent providers concurrently
- Works when the daemon is on an internal network
- Provides a clean foundation for a Flutter remote control UI
- Starts with excellent Grok Build support (optimization target)

## Decision Drivers

- Must work on internal/VPN networks that phones cannot join
- Strong preference for structured protocols over terminal scraping
- Grok Build has mature ACP (`grok agent stdio`) support
- Antigravity CLI (`agy`) currently lacks clear public ACP documentation
- Desire for a provider-agnostic design from day one
- Flutter is the chosen mobile UI technology
- Preference for simple, debuggable protocols in the early phases
- Security and least-privilege are non-negotiable

## Considered Options

### A. Protocol between Daemon and CLIs
1. Agent Client Protocol (ACP) via stdio
2. Headless one-shot mode only (`-p` style)
3. Full PTY / terminal scraping

### B. Communication between Daemon and Phone
1. Outbound Relay (daemon connects out)
2. Tailscale / mesh VPN only
3. Direct inbound connections (port forwarding / UPnP)
4. Hybrid (Relay primary + Tailscale + local)

### C. Mobile UI Technology
1. Flutter / Dart
2. Native (SwiftUI + Kotlin)
3. React Native

### D. Core Language for Daemon
1. Go
2. Rust
3. TypeScript / Node

## Decision Outcome

**Chosen options:**

- **CLI Integration**: ACP (Agent Client Protocol) as the primary protocol. Use `grok agent stdio` for Grok Build. Design a provider adapter interface that can later accommodate Antigravity even if it requires a different (possibly degraded) integration path initially.
- **Go SDK**: `github.com/coder/acp-go-sdk` (target `v0.13.5` or later).
- **Networking**: Hybrid model with **Outbound Relay as the primary path**. Optional Tailscale direct mode and local/LAN mode for better latency when available.
- **Phone Communication Protocol (initial)**: WebSocket + JSON.
- **Mobile UI**: Flutter / Dart.
- **Daemon Language**: Go.
- **Architecture Style**: Provider-agnostic adapter pattern + central Session Manager + Event Bus.
- **Project / Binary Naming**: Repository and project name is `magic-cli-remote`. The daemon binary and command is `mcremote`.

### Positive Consequences

- Works reliably on internal networks without requiring phones to join VPNs.
- Clean, typed integration with Grok Build via an official-style ACP Go SDK.
- Easy to add new providers later.
- Flutter allows rapid dual-platform (iOS + Android) development of the control UI.
- Clear separation of concerns makes testing and iteration straightforward.

### Negative Consequences

- Must build or operate a small relay component (or use an existing tunneling solution).
- Antigravity support may start with reduced capabilities until a clean protocol is available.
- WebSocket + JSON is less strictly typed than gRPC (acceptable for Foundation phase).

## Pros and Cons of the Options

### ACP vs Alternatives for CLI Control
- **ACP**: Structured, supported by Grok Build, good tooling → **Selected**
- Headless only: Too limited for interactive remote control
- PTY scraping: Fragile, high maintenance

### Networking Options
- **Outbound Relay**: Solves the internal network problem cleanly → **Primary**
- Tailscale: Excellent latency and security when available → **Strong secondary**
- Direct inbound: Often impossible on corporate networks → Rejected as primary

## Implementation Notes (Foundation Phase)

**Primary CLI target**: Grok Build via `grok agent stdio`

**Key Go libraries**:
- `github.com/coder/acp-go-sdk v0.13.5`
- `os/exec` + context for process management
- `github.com/gorilla/websocket` or `nhooyr.io/websocket`
- `github.com/spf13/cobra` + `github.com/spf13/viper`
- `log/slog`
- `github.com/golang-jwt/jwt/v5` (for later auth)

**Core abstractions to implement first**:
- `Provider` interface
- `Session` interface
- `Event` types
- Grok Build ACP adapter
- Basic Session Manager
- Local WebSocket server (for initial development)

**Naming**:
- Project / repository: `magic-cli-remote`
- Daemon binary & command: `mcremote`
- Main entrypoint: `cmd/mcremote`

## Links

- Grok Build ACP docs: `grok agent stdio`
- ACP Go SDK: https://github.com/coder/acp-go-sdk
- Agent Client Protocol: https://agentclientprotocol.com
- Related discussion: Remote control architecture for multi-CLI agents (internal)
