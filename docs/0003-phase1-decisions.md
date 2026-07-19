# MADR 0003: Phase 1 scaffolding decisions

- **Status**: Accepted
- **Date**: 2026-07-19
- **Deciders**: Project Owner
- **Supersedes (partially)**: Networking primary path in [MADR 0001](./0001-architecture-mcremote.md) for Phase 1 only

## Context

Phase 1 implements the `mcremote` foundation scaffold. Product decisions were locked during planning.

## Decisions

| Topic | Choice |
|-------|--------|
| Networking | **Headscale/Tailscale mesh only** (no outbound relay in Phase 1) |
| Application auth | **Device tokens** (SHA-256 at rest) **and** mesh perimeter |
| Config format | **YAML** default via Viper |
| Paths | **XDG** on Linux and macOS |
| Default listen | `127.0.0.1:**7531**` |
| Providers | Interface + **fake** + empty **Grok** stub |
| Module path | `github.com/maccavelli/magic-cli-remote` |
| Health | Unauthenticated `GET /healthz` allowed |

## Consequences

- Phones must join the Headscale tailnet to reach the daemon remotely.
- MADR 0001’s “relay primary” remains the long-term hybrid vision; relay is deferred past Phase 1.
- Full Grok ACP lands in Phase 2; Phase 1 validates control-plane plumbing with the fake provider.
