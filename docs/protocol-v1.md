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

Optional: `Authorization: Bearer <token>` on the HTTP Upgrade request.

Unauthenticated clients may only use HTTP `GET /healthz` and `GET /v1/hello`.

## Client → server

| type | payload | response |
|------|---------|----------|
| `auth` | `{ "token" }` | `auth_ok` / `auth_error` |
| `session.create` | `{ "provider", "name?", "cwd?" }` | `session.created` |
| `session.list` | `{}` | `session.list_result` |
| `session.close` | `{ "session_id" }` | `ok` / `error` |
| `session.prompt` | `{ "session_id", "text" }` | `ok` / `error` |
| `session.cancel` | `{ "session_id" }` | `ok` / `error` |
| `providers.list` | `{}` | `providers.list_result` |
| `ping` | `{}` | `pong` |

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
