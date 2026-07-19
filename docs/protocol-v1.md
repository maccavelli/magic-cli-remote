# mcremote WebSocket protocol v1

Transport: WebSocket at `GET /v1/ws`  
Encoding: JSON text frames  
Version field: `"v": 1` on every message

## Envelope

```json
{
  "v": 1,
  "type": "<message_type>",
  "id": "<client-request-id>",
  "payload": { }
}
```

- Client requests should set `id`; server echoes it on responses.
- Server pushes (`event`) may omit `id`.

## Authentication

Device token is required when `auth.require_device_token` is true (default).
Unauthenticated sockets have **30s** to send `auth` or `pair.claim`.

### Long-lived device token

**First message after connect:**

```json
{ "v": 1, "type": "auth", "id": "1", "payload": { "token": "mcr_..." } }
```

(`token` may also be a top-level field on the envelope.)

**Success:**

```json
{ "v": 1, "type": "auth_ok", "id": "1", "payload": { "device_id": "...", "device_name": "..." } }
```

**Failure:**

```json
{ "v": 1, "type": "auth_error", "id": "1", "payload": { "message": "...", "code": "invalid_token" } }
```

### Short pair code (preferred onboarding)

Host runs `mcremote pair code --name phone` → 8-char code, **5 minute TTL**, one-shot
(hashed in `data_dir/pair_codes.json`). Phone claims before holding a durable token:

```json
{ "v": 1, "type": "pair.claim", "id": "1", "payload": { "code": "K7M29X4P", "name": "optional" } }
```

**Success** (socket is also marked authenticated):

```json
{
  "v": 1,
  "type": "pair_ok",
  "id": "1",
  "payload": {
    "token": "mcr_…",
    "device_id": "…",
    "device_name": "phone"
  }
}
```

Client **must store** `token` for reconnects. Error codes: `invalid_code`, `expired`, `rate_limited`.

Optional: `Authorization: Bearer <token>` on the HTTP Upgrade request.

Unauthenticated clients may only use HTTP `GET /healthz` and `GET /v1/hello`, plus WS `auth` / `pair.claim`.

## Client → server

| type | payload | response |
|------|---------|----------|
| `auth` | `{ "token" }` | `auth_ok` / `auth_error` |
| `pair.claim` | `{ "code", "name?" }` | `pair_ok` / `pair_error` |
| `session.create` | `{ "provider", "name?", "cwd?" }` | `session.created` |
| `session.list` | `{}` | `session.list_result` |
| `session.close` | `{ "session_id" }` | `ok` / `error` |
| `session.prompt` | `{ "session_id", "text" }` | `ok` / `error` |
| `session.cancel` | `{ "session_id" }` | `ok` / `error` |
| `permission.respond` | `{ "session_id", "permission_id", "option_id"? , "cancelled"? }` | `ok` / `error` |
| `providers.list` | `{}` | `providers.list_result` |
| `ping` | `{}` | `pong` |

### `session.create` (Phase 2)

```json
{
  "provider": "grok",
  "name": "my task",
  "cwd": "/absolute/path",
  "agent_session_id": "",
  "session_id": ""
}
```

- `provider`: `fake` or `grok`
- `agent_session_id`: when set, Grok uses ACP `session/load` to resume
- `session_id`: optional fixed mcremote id when reconnecting a persisted record

## Server → client push

| type | payload |
|------|---------|
| `event` | `{ "event": { ... domain event ... } }` |
| `error` | `{ "message", "code?" }` |

### Domain event fields

```json
{
  "type": "assistant_message_chunk",
  "session_id": "...",
  "timestamp": "2026-07-19T00:00:00Z",
  "status": "",
  "text": "...",
  "tool_id": "",
  "tool_name": "",
  "error": ""
}
```

Event `type` values: `session_status`, `user_message`, `assistant_message_chunk`, `thought_chunk`, `tool_call`, `tool_call_update`, `permission_request`, `turn_complete`, `error`.

### `permission_request` event (Phase 2)

```json
{
  "type": "permission_request",
  "session_id": "...",
  "permission_id": "...",
  "tool_id": "...",
  "tool_name": "Run shell",
  "text": "Run shell",
  "status": "pending",
  "options": [
    { "option_id": "allow_once", "name": "Allow once", "kind": "allow_once" },
    { "option_id": "reject_once", "name": "Reject", "kind": "reject_once" }
  ]
}
```

Client responds with `permission.respond`:

```json
{
  "v": 1,
  "type": "permission.respond",
  "id": "9",
  "payload": {
    "session_id": "...",
    "permission_id": "...",
    "option_id": "allow_once"
  }
}
```

Or `{ "cancelled": true }` to cancel.

## HTTP (non-WS)

### `GET /healthz`

No auth. Example:

```json
{ "ok": true, "version": "dev" }
```

### `GET /v1/hello`

No auth. Example:

```json
{
  "version": "dev",
  "listen": "127.0.0.1:7531",
  "headscale_control_url": "http://localhost:8080",
  "protocol": 1
}
```
