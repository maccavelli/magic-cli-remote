# MADR 0004: Phase 2 — Grok ACP provider

- **Status**: Accepted
- **Date**: 2026-07-19
- **Related**: [MADR 0003](./0003-MADR-phase1-decisions.md)

## Decision

Implement a full **Grok Build ACP** adapter in `internal/provider/grok` using `github.com/coder/acp-go-sdk` **v0.13.5**.

## Scope delivered

- Spawn `grok agent --no-leader stdio` (configurable args)
- ACP client: initialize, session/new, session/load, prompt, cancel, close
- Client callbacks: `fs/*`, `terminal/*`, `session/request_permission`, `session/update`
- Map ACP updates → domain events
- Remote permissions via WS `permission.respond`
- Session metadata persistence under `$XDG_DATA_HOME/mcremote/sessions/<id>/meta.json`
- Config: `providers.grok.{enabled,bin,args,always_approve,default_cwd,model}`

## Non-goals (later)

- Image/file prompt attachments
- Full history JSONL replay to clients
- Antigravity provider
- Outbound relay
