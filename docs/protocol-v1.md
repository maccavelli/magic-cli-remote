# mcremote WebSocket protocol v1

Transport: WebSocket at `GET /v1/ws` over TLS (`wss://`) by default  
Encoding: JSON text frames  
Version field: `"v": 1` on every message

## Transport security (TLS)

The daemon terminates TLS itself, in one of two certificate modes. **The `mode`
param in the pair URI tells the client which one is in force** — the client
never needs to be configured separately.

**`letsencrypt` mode** (default once a domain and ACME email are configured):
the daemon obtains a publicly trusted certificate from an ACME CA using the
DNS-01 challenge, and the pair URI advertises the certificate's DNS name. The
client validates with the ordinary platform trust store, including hostname
verification.

The pair URI *also* carries an `fp`, and in this mode it is an **alternative**
to chain validation, not a replacement for it — accept a certificate that
validates **or** matches the pin. The pin exists for one specific situation: if
ACME issuance fails (undelegated zone, expired Route 53 credential, rate limit,
no network at boot) the daemon does not refuse to start — it falls back to its
self-signed identity. A client with no pin would then reject that certificate
permanently, turning every recoverable issuance failure into a forced re-pair.

Consequently the `fp` advertised in `letsencrypt` mode is the **self-signed
fallback leaf**, not the ACME leaf. That is deliberate: while ACME is healthy
the chain validates and the pin is never consulted, and when ACME fails the pin
is the only thing that gets an already-paired phone connected. It also means a
client must never treat this pin as exclusive — doing so would reject the
perfectly good ACME certificate. Because the rule is *or* rather than *and*, the
~60-day renewal that once argued against pinning here is a non-issue: a stale
pin is never load-bearing.

**`selfsigned` mode** (fallback, and what bare mesh IPs require): the daemon
generates a **self-signed P-256 ECDSA certificate** into the data dir
(`tls.crt` / `tls.key`, mode `0600`) and reuses it on every subsequent run,
regenerating only within 30 days of expiry. SANs cover the machine hostname,
`localhost`, loopback, and every non-link-local interface address, which is
what makes it valid for LAN and mesh (Tailscale `100.64.0.0/10`) addresses. It
is a plain server-auth leaf — `IsCA: false`, no `keyCertSign` — so installing it
in a system or browser trust store to make `curl` work grants trust for its own
SANs and nothing else.

There is no CA to trust in that mode, so clients authenticate the daemon by
**pinning the certificate's SHA-256 fingerprint**, distributed out-of-band by
the same pair QR that carries the pair code:

```
mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531&code=K7M29X4P&fp=<fingerprint>&mode=selfsigned
```

| param | meaning |
|-------|---------|
| `host` | `host:port`, optionally prefixed `wss://` (TLS) or `ws://` (plaintext). A bare host means "client default", which is TLS. |
| `fp` | SHA-256 of a leaf certificate DER, unpadded **base64url** (43 chars). Present in **both** TLS modes; absent only when TLS is disabled (`ws://` host). |
| `mode` | `selfsigned`, `letsencrypt` or `off`. Selects the certificate acceptance rule below. |

`fp` is also accepted as hex or colon-hex with an optional `sha256:` prefix, so
`openssl x509 -fingerprint -sha256` output can be pasted verbatim; it is
normalised to base64url on the wire.

**Certificate acceptance, by mode**

| `mode` | rule | trust set |
|---|---|---|
| `selfsigned` | fingerprint only — the platform trust store is not consulted | exactly this one certificate |
| `letsencrypt` | valid chain **or** fingerprint match | public CAs for that name ∪ this one certificate |
| `off` | no TLS | — |

`mode` is optional for backwards compatibility. A client that receives a pair
URI with no `mode` must fall back to the pre-`mode` behaviour: treat a present
`fp` as `selfsigned` and an absent one as `off`. An **unrecognised** `mode` must
fail closed — it selects a trust rule, and guessing at one is exactly the wrong
instinct.

**Client obligations**

- Pin `fp` for the identity it was scanned with, and persist it as securely as
  the device token so reconnects and process-death recovery still pin.
- In `selfsigned` mode, accept the peer certificate **iff** its SHA-256 equals
  the pinned value. Verification must not be delegated to the platform trust
  store: a pinned connection has to reject a publicly-trusted certificate just
  as firmly as an unknown self-signed one.
- In `letsencrypt` mode, attempt platform validation (with hostname
  verification) first and consult the pin only if that fails. Do not require
  both: the ACME certificate will not match the pin, and the fallback
  certificate will not chain.
- On failure of every applicable rule, fail closed and permanently — error code
  `cert_mismatch` (`selfsigned`) or `cert_unpinned` (`letsencrypt`). Do not
  retry, do not prompt to continue, do not fall back to plaintext. The remedy is
  fixing the server certificate or re-pairing, never disabling verification.
- Note that a daemon offline for more than 90 days returns with an expired ACME
  certificate; in `letsencrypt` mode the fallback pin covers exactly that gap
  until it renews.

**Plaintext opt-out.** `tls.mode: off` (or `mcremote serve --tls=false`)
serves plain `http://` / `ws://` for deployments that terminate TLS elsewhere.
The daemon logs a warning, the pair URI is then emitted with an explicit
`ws://` host, `mode=off` and no `fp`, and clients must treat that combination as
the only thing that permits an unpinned connection.

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

`auth_error` codes: `invalid_token`, `auth_failed`, and — when the client-key
allowlist is enforced (see [Client identity](#client-identity-client-key-allowlist)) —
`client_key_required` (no client certificate was presented) and
`client_key_mismatch` (the presented key is not the one enrolled for this
device). The last two are **permanent**: the remedy is re-pairing the device,
never a retry.

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
`bad_payload` (undecodable payload), `create_failed` (device store write failed),
`client_key_required` (enforcement is on but the socket presented no client
certificate — see [Client identity](#client-identity-client-key-allowlist)).

Optional: `Authorization: Bearer <token>` on the HTTP Upgrade request.

Unauthenticated clients may only use HTTP `GET /healthz` (liveness only — `{"ok":true}`), plus WS `auth` / `pair.claim`. `GET /v1/hello` requires a device token.

### Client identity (client-key allowlist)

Beyond the bearer device token, each paired device is bound to a **public key it
proves possession of by completing the TLS handshake** (ADR 0005). The model is
SSH `authorized_keys`, not PKI: **there is no CA and no `ClientCAs` pool**, and
the client certificate's validity/expiry is deliberately ignored — the key is
the identity.

**Fingerprint scheme.** A client key's identity is the unpadded **base64url
SHA-256** of the certificate's `RawSubjectPublicKeyInfo` (the SPKI, 43 chars) —
**not** the certificate DER. The certificate may be regenerated around the same
key without changing identity. Both sides must compute this over the same SPKI
bytes.

**Transport.** The daemon serves with `ClientAuth: RequestClientCert` (REQUEST,
not Require, in both `selfsigned` and `letsencrypt` modes). The handshake
therefore completes even when the client presents no certificate or an
unenrolled one, so rejection is a **typed protocol error** (above) rather than
an opaque TLS alert — which the spike found surfaces on the client only as a
generic read failure. The presented certificate is captured from the connection
but never chain-verified; possession is what the handshake proves.

**Enrolment (`pair.claim`).** The daemon reads the client certificate presented
on the pairing connection, computes its SPKI fingerprint, and records it on the
new device record. No extra field in the `pair.claim` payload is needed — the
key rides the TLS layer. With enforcement on, a `pair.claim` on a connection
that presented no client certificate is rejected `client_key_required`.

**Authentication (`auth`).** After resolving the device by token, the daemon
compares the connection's presented SPKI fingerprint to the one stored for that
device:

| enforcement | stored key | presented key | result |
|---|---|---|---|
| on | any | absent | `auth_error` `client_key_required` |
| on | `X` | `Y ≠ X` (incl. a keyless record, whose empty fingerprint nothing matches) | `auth_error` `client_key_mismatch` |
| on | `X` | `X` | `auth_ok` |
| off | — | — | token alone; a presented key is recorded opportunistically on next pair |

**Enforcement flag.** `auth.require_client_key` (env
`MCREMOTE_AUTH_REQUIRE_CLIENT_KEY`) — **default `true`** (decision D7: the fleet
is a single operator-owned phone, so re-pairing a legacy keyless device is the
accepted cost). When `false`, a device whose record has no key authenticates by
token alone, and a key presented at pair time is still recorded so the fleet can
migrate before enforcement is flipped on.

**Revocation** is deleting the device record (`mcremote pair revoke`), which now
denies transport access rather than merely a bearer secret.

## Client → server

| type | payload | response |
|------|---------|----------|
| `auth` | `{ "token" }` | `auth_ok` / `auth_error` |
| `pair.claim` | `{ "code", "name?" }` | `pair_ok` / `pair_error` |
| `session.create` | `{ "provider", "name?", "cwd?", "model?", "agent_session_id?", "session_id?" }` | `session.created` |
| `session.list` | `{}` | `session.list_result` |
| `session.close` | `{ "session_id" }` | `ok` / `error` |
| `session.delete` | `{ "session_id" }` | `ok` / `error` |
| `session.prompt` | `{ "session_id", "text" }` | `ok` / `error` |
| `session.cancel` | `{ "session_id" }` | `ok` / `error` |
| `session.history` | `{ "session_id" }` | `session.history_result` |
| `permission.respond` | `{ "session_id", "permission_id", "option_id"? , "cancelled"? }` | `ok` / `error` |
| `providers.list` | `{}` | `providers.list_result` |
| `ping` | `{}` | `pong` |

### `session.create` (Phase 2)

```json
{
  "provider": "grok",
  "name": "my task",
  "cwd": "/absolute/path",
  "model": "",
  "agent_session_id": "",
  "session_id": ""
}
```

- `provider`: `fake`, `grok`, or `opencode` (see `providers.list` for what the
  host actually offers)
- `model`: optional agent model for this session; grok takes a model name
  (`-m` flag), opencode a `provider/model` id (e.g.
  `anthropic/claude-sonnet-4-5`) applied via its ACP "model" config option.
  Empty uses the provider default.
- `agent_session_id`: when set, the provider uses ACP `session/load` to resume
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

### `session.history` (transcript replay)

Requests the buffered event history for a session so a client that (re)connects
mid-conversation can rebuild the transcript. Payload `{ "session_id" }`; the
reply is `session.history_result`:

```json
{
  "session_id": "...",
  "events": [ { ...domain event... }, … ]
}
```

- **Each element of `events` is the identical JSON shape as the `event` field
  inside a live `event` push** (same `type` vocabulary and fields). Clients feed
  them straight back through the same reducer used for live events — the server
  does no server-side coalescing; raw chunks are replayed as emitted and the
  client's reducer coalesces them.
- Events are ordered oldest-first, exactly as emitted.
- The daemon keeps a **bounded per-session ring buffer (500 events, oldest
  dropped)** for each live session. It buffers every event kind, including the
  high-frequency `assistant_message_chunk` / `thought_chunk` chunks (replaying
  them is the point).
- An **unknown, never-active, or already-closed** session returns
  `{ "session_id", "events": [] }` — an empty list, **not** an error. The buffer
  lives with the live session; once a session is closed its buffer is gone, so
  replay is a best-effort aid for live sessions.

Error codes: `bad_payload` (malformed payload only).

## Server → client push

| type | payload |
|------|---------|
| `event` | `{ "event": { ... domain event ... } }` |
| `error` | `{ "message", "code?" }` |
| `ok` | none |
| `session.created` | a bare session Meta object (see below) |
| `session.list_result` | `{ "sessions": [ Meta, … ] }` |
| `session.history_result` | `{ "session_id", "events": [ domain event, … ] }` |
| `providers.list_result` | `{ "providers": [ { "id", "ready" }, … ] }` |

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
  "owner_device_id": "paired device id",
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
  with `status: "disconnected"` — the daemon then auto-closes the live entry).
  To interact with a non-live session, re-create it via `session.create` passing
  the existing `session_id` and `agent_session_id` (close-and-replace if still live).
- `owner_device_id`: the paired device that created (or first claimed) the session.
  `session.list` only returns sessions owned by the caller (or legacy rows with an
  empty owner). Mutating ops and event pushes are restricted the same way.
  Error code `session_forbidden` means another device owns the session;
  `session_not_live` means it is missing or no longer live.
- **Revocation:** `mcremote pair revoke` updates the device store and, when the
  daemon is running, kicks live WebSocket clients for that device via the local
  admin socket (`$data_dir/admin.sock`).

### Domain event fields

```json
{
  "type": "assistant_message_chunk",
  "session_id": "...",
  "timestamp": "2026-07-19T00:00:00Z",
  "seq": 42,
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

- `seq`: per-session monotonic sequence number, stamped by the daemon as the
  event enters the history ring. The same event carries the same `seq` on the
  live broadcast and in `session.history` replay, so clients can dedupe the
  reconnect overlap and detect ordering. `0`/absent means unstamped (the
  session was not tracked when the event fired).
- `replay`: the agent re-emitted this event while loading an existing
  conversation (session resume via `agent_session_id`). Replay events appear
  in `session.history` (they rebuild the ring for cold clients) but are not
  broadcast live; clients must never append a replay event to a transcript
  that already has content.

- `agent_session_id`: the provider-native session id (e.g. ACP `sessionId`) for the
  session this event belongs to. Included on status / tool / permission / turn
  events so clients can persist it for resume; **deliberately omitted from the
  high-frequency `assistant_message_chunk` and `thought_chunk` events** to cut wire
  noise. Clients should latch the last non-empty value per session rather than
  expecting it on every event.
- `stop_reason`: set on `turn_complete` when known — the provider's reason the turn
  ended (e.g. `end_turn`, `max_tokens`, `refusal`, `cancelled`). On `turn_complete`
  the `status` field carries the same value.

Event `type` values: `session_status`, `user_message`, `assistant_message_chunk`, `thought_chunk`, `tool_call`, `tool_call_update`, `permission_request`, `permission_resolved`, `turn_complete`, `error`, `available_commands`, `plan`.

### `plan` event (agent task list)

Carries the agent's execution plan (ACP `plan` / `plan_removed` updates). **Each
`plan` event is the full current plan (replace-semantics), not a delta** — the
client replaces its stored plan with `entries` on every event.

```json
{
  "type": "plan",
  "session_id": "...",
  "entries": [
    { "content": "Read the config", "status": "completed", "priority": "high" },
    { "content": "Apply the migration", "status": "in_progress", "priority": "medium" }
  ]
}
```

- `status` ∈ `pending`, `in_progress`, `completed`.
- `priority` ∈ `high`, `medium`, `low`. Unknown/absent priorities map to `medium`.
- A **plan clear** (ACP `plan_removed`) is a `plan` event with an empty `entries`
  list; since empty slices are omitted on the wire, a `plan` event with no
  `entries` key means "clear the plan".

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

No auth — liveness only. `version` was removed deliberately: this endpoint is
reachable unauthenticated, so it must not disclose anything about the host.

```json
{ "ok": true }
```

### `GET /v1/hello`

**Requires a device token** — `Authorization: Bearer <mcr_…>`. Without one the
daemon replies `401` with `WWW-Authenticate` and a bare
`{"error":"unauthorized"}`, disclosing none of the fields below.

The payload names the host's listen address and Headscale control URL, which is
reconnaissance for anyone who can reach the port. Since the daemon may be bound
somewhere reachable by a hostile LAN, it is authenticated rather than public.

Example (authorized):

```json
{
  "version": "dev",
  "listen": "127.0.0.1:7531",
  "headscale_control_url": "http://localhost:8080",
  "protocol": 1,
  "tls_mode": "selfsigned",
  "tls_fell_back": false
}
```

`tls_mode` is the certificate mode actually in force (`selfsigned`,
`letsencrypt`, or `off`). `tls_fell_back` is `true` when `letsencrypt` was
configured but ACME issuance failed and the daemon is serving its self-signed
fallback — poll it to catch a broken renewal before the 90-day cliff. Both are
behind the device-token auth above, never on the public `/healthz`.
