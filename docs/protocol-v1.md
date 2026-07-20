# mcremote WebSocket protocol v1

Transport: WebSocket at `GET /v1/ws` over TLS (`wss://`) by default  
Encoding: JSON text frames  
Version field: `"v": 1` on every message

## Transport security (TLS)

The daemon terminates TLS itself, in one of two certificate modes. **The
presence or absence of `fp` in the pair URI tells the client which one is in
force** — the client never needs to be configured separately.

**`letsencrypt` mode** (default once a domain and ACME email are configured):
the daemon obtains a publicly trusted certificate from an ACME CA using the
DNS-01 challenge, and the pair URI advertises the certificate's DNS name and
carries **no `fp`**. The client validates with the ordinary platform trust
store, including hostname verification. Nothing is pinned — the certificate is
renewed roughly every 60 days and a pin would break at the first renewal.

**`selfsigned` mode** (fallback, and what bare mesh IPs require): the daemon
generates a **self-signed P-256 ECDSA certificate** into the data dir
(`tls.crt` / `tls.key`, mode `0600`) and reuses it on every subsequent run,
regenerating only within 30 days of expiry. SANs cover the machine hostname,
`localhost`, loopback, and every non-link-local interface address, which is
what makes it valid for LAN and mesh (Tailscale `100.64.0.0/10`) addresses.

There is no CA to trust in that mode, so clients authenticate the daemon by
**pinning the certificate's SHA-256 fingerprint**, distributed out-of-band by
the same pair QR that carries the pair code:

```
mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531&code=K7M29X4P&fp=<fingerprint>
```

| param | meaning |
|-------|---------|
| `host` | `host:port`, optionally prefixed `wss://` (TLS) or `ws://` (plaintext). A bare host means "client default", which is TLS. |
| `fp` | SHA-256 of the leaf certificate DER, unpadded **base64url** (43 chars). Absent when the daemon runs in `letsencrypt` mode (validate normally) or with TLS disabled (`ws://` host). |

`fp` is also accepted as hex or colon-hex with an optional `sha256:` prefix, so
`openssl x509 -fingerprint -sha256` output can be pasted verbatim; it is
normalised to base64url on the wire.

**Client obligations**

- Pin `fp` for the host it was scanned with, and persist it as securely as the
  device token so reconnects and process-death recovery still pin.
- Accept the peer certificate **iff** its SHA-256 equals the pinned value.
  Verification must not be delegated to the platform trust store: a pinned
  connection has to reject a publicly-trusted certificate just as firmly as an
  unknown self-signed one.
- On mismatch, fail closed and permanently — error code `cert_mismatch`. Do not
  retry, do not prompt to continue, do not fall back to plaintext.
- With a `wss://` host and **no** `fp`, validate against the platform trust
  store with hostname verification, exactly as for any HTTPS endpoint. This is
  `letsencrypt` mode; the daemon's name is a real DNS name and the certificate
  chains to a public root.
- A certificate that fails platform validation in that case is a permanent
  failure (`cert_unpinned`) — the remedy is fixing the server certificate or
  re-pairing, never disabling verification. Note that a daemon offline for
  more than 90 days returns with an expired certificate and legitimately
  fails this check until it renews.

**Plaintext opt-out.** `tls.mode: off` (or `mcremote serve --tls=false`)
serves plain `http://` / `ws://` for deployments that terminate TLS elsewhere.
The daemon logs a warning, the pair URI is then emitted with an explicit
`ws://` host and no `fp`, and clients must treat the missing `fp` as the only
thing that permits an unpinned connection.

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

Client **must store** `token` for reconnects.

`pair_error` codes: `invalid_code`, `expired`, `rate_limited`, `already_authed` (socket
already authenticated), `unavailable` (pairing not configured on this daemon),
`bad_payload` (undecodable payload), `create_failed` (device store write failed).

Optional: `Authorization: Bearer <token>` on the HTTP Upgrade request.

Unauthenticated clients may only use HTTP `GET /healthz` and `GET /v1/hello`, plus WS `auth` / `pair.claim`.

## Client → server

| type | payload | response |
|------|---------|----------|
| `auth` | `{ "token" }` | `auth_ok` / `auth_error` |
| `pair.claim` | `{ "code", "name?" }` | `pair_ok` / `pair_error` |
| `session.create` | `{ "provider", "name?", "cwd?", "agent_session_id?", "session_id?" }` | `session.created` |
| `session.list` | `{}` | `session.list_result` |
| `session.close` | `{ "session_id" }` | `ok` / `error` |
| `session.delete` | `{ "session_id" }` | `ok` / `error` |
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

Error codes: `bad_payload`, `session_create_failed`.

### `session.close` vs `session.delete`

- `session.close` stops the live session and **keeps** the persisted record, with
  `status` set to `disconnected` and `live: false`. The row stays in
  `session.list_result` so the client can resume it later.
- `session.delete` stops the live session (if any) and **hard-deletes** the record
  from disk. Use this to actually remove a row; a `session.close` alone leaves it
  in the list and it will reappear after a refresh.

Both take `{ "session_id" }` and reply `ok`. Error codes: `bad_payload`,
plus `session_close_failed` / `session_delete_failed`.

## Server → client push

| type | payload |
|------|---------|
| `event` | `{ "event": { ... domain event ... } }` |
| `error` | `{ "message", "code?" }` |
| `ok` | none |
| `session.created` | a bare session Meta object (see below) |
| `session.list_result` | `{ "sessions": [ Meta, … ] }` |
| `providers.list_result` | `{ "providers": [ { "id", "name", "ready", … }, … ] }` |

### Session `Meta`

`session.created` carries a Meta object **directly as its payload** (it is not
wrapped in a `session` key). `session.list_result` carries an array of them.

```json
{
  "id": "mcremote session id",
  "provider": "grok",
  "name": "my task",
  "cwd": "/absolute/path",
  "agent_session_id": "provider-native session id",
  "created_at": "2026-07-19T00:00:00Z",
  "status": "idle",
  "live": true
}
```

- `status`: last observed session status — `idle`, `running`, `error`, `cancelled`,
  `disconnected`.
- `live`: whether the daemon currently holds a running provider process for this
  session. **Only `live: true` sessions accept `session.prompt`, `session.cancel`
  or `permission.respond`**; clients gate interaction on it. A session goes
  `live: false` when it is closed, when it is only a persisted record from a
  previous daemon run, or when its provider process died (`session_status`
  with `status: "disconnected"`). To interact with a non-live session, re-create
  it via `session.create` passing the existing `session_id` and `agent_session_id`.

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
  "error": "",
  "agent_session_id": "",
  "stop_reason": ""
}
```

All fields except `type`, `session_id` and `timestamp` are omitted when empty.

- `agent_session_id`: the provider-native session id (e.g. ACP `sessionId`) for the
  session this event belongs to. Included on status / tool / permission / turn
  events so clients can persist it for resume; **deliberately omitted from the
  high-frequency `assistant_message_chunk` and `thought_chunk` events** to cut wire
  noise. Clients should latch the last non-empty value per session rather than
  expecting it on every event.
- `stop_reason`: set on `turn_complete` when known — the provider's reason the turn
  ended (e.g. `end_turn`, `max_tokens`, `refusal`, `cancelled`). On `turn_complete`
  the `status` field carries the same value.

Event `type` values: `session_status`, `user_message`, `assistant_message_chunk`, `thought_chunk`, `tool_call`, `tool_call_update`, `permission_request`, `permission_resolved`, `turn_complete`, `error`, `available_commands`.

### `available_commands` event (slash commands)

Advertised by the agent (ACP `available_commands_update`). Clients show them in the composer; **invoke by sending a normal `session.prompt`** whose text starts with `/name` (optionally followed by args):

```json
{
  "type": "available_commands",
  "session_id": "...",
  "commands": [
    { "name": "web", "description": "Search the web", "hint": "query" },
    { "name": "plan", "description": "Create a plan", "hint": "what to plan" }
  ]
}
```

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

### `permission_resolved` event

Emitted **exactly once** for every `permission_request`, when that request leaves the
pending state for any reason. Clients that block input while a permission is pending
**must** clear that state on this event — otherwise an abandoned request (agent gave
up, turn cancelled, session closed) locks the composer forever.

```json
{
  "type": "permission_resolved",
  "session_id": "...",
  "permission_id": "...",
  "timestamp": "2026-07-19T00:00:00Z",
  "status": "resolved"
}
```

`status` is one of:

- `resolved` — a decision arrived (via `permission.respond`) and was applied to the
  agent. Note this covers an explicit user *reject/cancel* too: the client's answer
  was delivered.
- `cancelled` — the request was **abandoned** and no decision will ever be applied:
  the agent-side context was cancelled, or the session closed while it was pending.
  On session close, one such event is emitted for every still-pending request.

The event carries no `options` and no `tool_id`; correlate with the original
`permission_request` on `permission_id`.

## HTTP (non-WS)

Both endpoints are served over the same listener as `/v1/ws`, so they are
`https://` when TLS is on and must be probed with the same pinned certificate.

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
