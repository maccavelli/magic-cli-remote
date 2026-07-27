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
| `session.create` | `{ "provider", "name?", "cwd?", "model?", "agent?", "agent_session_id?", "session_id?" }` | `session.created` |
| `session.list` | `{}` | `session.list_result` |
| `session.close` | `{ "session_id" }` | `ok` / `error` |
| `session.delete` | `{ "session_id" }` | `ok` / `error` |
| `session.prompt` | `{ "session_id", "text", "attachments?" }` | `ok` / `error` (`turn_busy` if a turn is already active) |
| `session.set_mode` | `{ "session_id", "mode_id" }` | `ok` / `error` |
| `session.set_config_option` | `{ "session_id", "option_id", "kind", "value" }` | `ok` / `error` |
| `session.cancel` | `{ "session_id" }` | `ok` / `error` |
| `session.history` | `{ "session_id", "since_seq?", "limit?" }` | `session.history_result` |
| `permission.respond` | `{ "session_id", "permission_id", "option_id"? , "cancelled"? }` | `ok` / `error` |
| `question.respond` | `{ "session_id", "question_id", "answers"? , "cancelled"? }` | `ok` / `error` |
| `providers.list` | `{}` | `providers.list_result` |
| `models.list` | `{ "provider" }` | `models.list_result` |
| `agents.list` | `{ "provider" }` | `agents.list_result` |
| `agent_sessions.list` | `{ "provider" }` | `agent_sessions.list_result` |
| `commands.list` | `{ "provider" }` | `commands.list_result` |
| `session.fork` | `{ "session_id", "message_id?" }` | `session.created` |
| `session.revert` | `{ "session_id", "message_id", "part_id?" }` | `ok` |
| `session.unrevert` | `{ "session_id" }` | `ok` |
| `session.diff` | `{ "session_id", "message_id?" }` | `session.diff_result` |
| `session.rename` | `{ "session_id", "name" }` | `session.rename_result` |
| `session.diagnostics` | `{ "session_id" }` | `session.diagnostics_result` |
| `ping` | `{}` | `pong` |

### `session.create` (Phase 2)

```json
{
  "provider": "grok",
  "name": "my task",
  "cwd": "/absolute/path",
  "model": "",
  "agent": "",
  "agent_session_id": "",
  "session_id": ""
}
```

- `provider`: `fake`, `grok`, `opencode`, `goose`, or `codex` (see
  `providers.list` for what the host actually offers — registration does not
  imply the binary is installed)
- `model`: optional agent model for this session. Grok takes a model name
  (`-m` flag); opencode a `provider/model` id (e.g.
  `anthropic/claude-sonnet-4-5`) applied via its ACP "model" config option;
  codex a model name sent on each `turn/start`, so a mid-session change through
  `/model` takes effect from the next turn and keeps the thread; goose uses the
  engine default. Empty uses the provider default. Prefer values from
  `models.list`.
- `agent`: optional OpenCode agent name (e.g. `build`, `plan`) sent on each
  `prompt_async`. Empty uses the engine default. Prefer values from
  `agents.list`. Ignored by non-OpenCode providers.
- `agent_session_id`: when set, the provider uses ACP `session/load` to resume
- `session_id`: optional fixed mcremote id when reconnecting a persisted record

Error codes: `bad_payload`, `session_create_failed`.

### `models.list` (interactive picker catalog)

Returns a **shared picker catalog** for one registered provider so clients can
render a full interactive model picker (search, groups, single-select today,
multi-select schema-ready).

**Request:**

```json
{ "provider": "opencode" }
```

**Reply** `models.list_result`:

```json
{
  "provider": "opencode",
  "kind": "single",
  "source": "merged",
  "allow_custom": true,
  "default_ids": ["opencode/deepseek-v4-flash-free"],
  "min_select": 0,
  "max_select": 1,
  "options": [
    {
      "id": "opencode/deepseek-v4-flash-free",
      "label": "deepseek-v4-flash-free",
      "description": "",
      "group": "opencode",
      "enabled": true
    }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `kind` | `single` (at most one id) or `multi` (bounded by `min_select` / `max_select`; `max_select` 0 = unlimited on multi) |
| `source` | `live` (engine catalog), `static` (built-in fallback), or `merged` |
| `options[]` | Rows: `id` (value returned to server), `label`, `description?`, `group?`, `enabled?` (omit = true), `meta?` |
| `default_ids` | Suggested pre-selection (first used for single-select) |
| `allow_custom` | Client may accept free-text ids not in `options` |

Providers that implement a model catalog (fake, grok static, opencode live+static)
return options; otherwise the result is an empty list with `allow_custom: true`
so free-text still works. Listing may boot a shared engine (OpenCode HTTP) and
is handled off the WS read loop.

Error codes: `bad_payload` (missing `provider`), `unknown_provider`.

### `agent_sessions.list` (provider-native resume discovery)

Returns a bounded, metadata-only page of resumable sessions from a provider
that explicitly supports discovery (currently ACP providers advertising
`sessionCapabilities.list`). It does not create a daemon session, copy a
transcript, or change ownership. A client imports a chosen entry only by
sending its `id` as `session.create.agent_session_id`.

**Request:**

```json
{ "provider": "goose" }
```

**Reply** `agent_sessions.list_result`:

```json
{
  "provider": "goose",
  "sessions": [
    {
      "id": "20260726_30",
      "cwd": "/absolute/path",
      "title": "Optional agent title",
      "updated_at": "2026-07-26T20:52:14Z"
    }
  ]
}
```

The result is sent only to the authenticated requesting socket, never pushed
or broadcast. `id`, `cwd`, `title`, and `updated_at` are provider-controlled
metadata and clients must not treat their presence as a successful load.

Error codes: `bad_payload`, `unknown_provider`, `provider_unavailable`,
`unsupported`, `agent_sessions_list_failed`.

### `agents.list` (OpenCode agent picker catalog)

Same shared picker schema as `models.list`, for OpenCode top-level agent names
(`build`, `plan`, and configured visible `primary`/`all` agents) from the
engine `GET /agent` route. Subagents and hidden engine agents are not exposed:
they cannot receive a user turn.

**Request:**

```json
{ "provider": "opencode" }
```

**Reply** `agents.list_result` — same fields as `models.list_result`. Options
are grouped by startable mode (`primary`, `all`) and set `allow_custom: false`.
A raw `session.create.agent` is also validated on the daemon and returns
`bad_agent` when unknown, hidden, or a subagent.

Providers without an agent catalog return an empty list with
`allow_custom: true`. Non-OpenCode providers typically have no options.

Error codes: `bad_payload` (missing `provider`), `unknown_provider`.

### `commands.list` (slash-command catalog)

```json
{ "provider": "opencode", "session_id": "…" }
```

Same shared picker schema as `models.list`. The canonical commands below come
first (`group: "remote"`), followed by the provider's own catalog — OpenCode maps
engine `GET /command` (`init`, `review`, …). `session_id` is optional: with a live
session the canonical entries are enabled by what that session can actually run;
without it only the ones that work on every session of the provider are enabled,
and the rest carry the reason in their description. Session create also emits
`available_commands` and `remote_commands` so autocomplete works without a
round-trip. Invoking a listed command is done with a normal `session.prompt`
whose text is `/name args…`.

### Canonical slash commands (daemon-interpreted)

A `session.prompt` whose text starts with `/name` is routed by the daemon, not
sent verbatim. The canonical vocabulary (`internal/command`, MADR 0023) is the
same on every provider; how each command is satisfied is not:

| command | effect |
|---|---|
| `/help` | list what this session can run, and why anything else cannot |
| `/plan [off]` | switch to the agent's plan mode; `/plan off` returns to its default mode |
| `/mode [id]` | list the agent's modes, or switch to one |
| `/model [name]` | show or switch the model — in place where the provider can, otherwise by restarting the agent |
| `/context` | context-window usage for this session |
| `/compact` | summarise the conversation to reclaim context |
| `/clear` (`/reset`) | clear the conversation and restart the agent |
| `/new [name]` | start another session with the same provider/cwd/model |
| `/sessions` | list your sessions |
| `/goal <objective>` | set an autonomous goal (agents that have one) |
| `/diff` | file changes made in this session |
| `/undo` / `/redo` | revert the last turn / restore it |

Each **daemon-handled** command emits a `notice` and echoes the command as a
`user_message`, since the agent never sees it. A command mapped to the agent's own
(grok answers `/context` with its `/session-info`) is forwarded instead, and the
agent's output is the answer.

Per-provider mechanisms as of this revision:

| command | grok | OpenCode |
|---|---|---|
| `/help` `/clear` `/new` `/sessions` | daemon | daemon |
| `/plan` `/mode` | session mode (`session/set_mode`) | session mode (primary agent) |
| `/model` | restart with the new model | `POST /api/session/{id}/model`, in place |
| `/context` | forwarded as `/session-info` | daemon, from tracked usage |
| `/compact` | **unavailable** — grok compacts only in its TUI | `POST /session/{id}/summarize` |
| `/goal` | forwarded to grok's `/goal` | **unavailable** |
| `/diff` `/undo` `/redo` | **unavailable** over ACP | engine revert/diff routes |

Availability is per session, and clients do not have to derive it: the daemon
sends the resolved list as a `remote_commands` event (below). A command an agent
advertises via `available_commands` is not automatically trusted — a provider's
declaration wins, because agents advertise commands their shells only execute in
their own terminal UI.

Any non-canonical `/command` is forwarded when the agent advertised it, and
otherwise reported as unavailable rather than sent as confusing literal text.

### `session.set_mode` (agent operating modes)

```json
{ "session_id": "...", "mode_id": "plan" }
```

Valid ids are the ones the session advertised in its `session_mode` event.
Providers without modes return an error (`session does not support modes`).

How each provider implements a mode (MADR 0022):

| provider | mechanism |
|---|---|
| `grok` | ACP `session/set_mode`; ids `default` and `plan`. Grok honors the call but advertises no modes, so the daemon supplies the list and validates ids. |
| `opencode` | the mode id **is** a primary agent name (`build`, `plan`, …); subsequent prompts run as that agent, so a switch takes effect on the next message. |
| `fake` | echoes the switch back (tests). |

### `session.fork` / `session.revert` / `session.unrevert` / `session.diff`

OpenCode session-tree polish (MADR 0020 Sprint 5):

| Type | Payload | Reply |
|------|---------|-------|
| `session.fork` | `{ "session_id", "message_id?" }` | `session.created` for the new mcremote session (engine fork + resume) |
| `session.revert` | `{ "session_id", "message_id", "part_id?" }` | `ok` + notice |
| `session.unrevert` | `{ "session_id" }` | `ok` + notice |
| `session.diff` | `{ "session_id", "message_id?" }` | `session.diff_result` `{ "session_id", "summary" }` (+ notice) |

Unsupported providers return a typed error (`session_fork_failed`, etc.).

Live SSE `session.diff` events are also mapped to a multi-line `notice` (paths
and +/− counts) without a client pull.

### `session.rename` and `session.diagnostics`

Both operations are owner-authorized and require a live session. They are
direct responses only: neither enters transcript history nor becomes a pushed
event.

| Type | Payload | Reply |
|------|---------|-------|
| `session.rename` | `{ "session_id", "name" }` | `session.rename_result` `{ "session": Meta }` |
| `session.diagnostics` | `{ "session_id" }` | `session.diagnostics_result` `{ "session_id", "diagnostics" }` |

`name` is trimmed, required, and capped at 256 bytes. The daemon updates its
durable name only after the provider-native rename succeeds. Unsupported
providers return `session_rename_failed` or `session_diagnostics_failed`.

`diagnostics` is intentionally bounded and redacted:

```json
{
  "branch": "feature/parity",
  "default_branch": "main",
  "vcs": { "added": 1, "modified": 2, "deleted": 0, "additions": 14, "deletions": 3 },
  "mcp": [{ "name": "gopls", "state": "connected" }]
}
```

It never includes repository paths or content, patches, URLs, headers, tokens,
OAuth state, command arguments, or arbitrary provider configuration.

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
mid-conversation can rebuild the transcript.

**Request** (older clients may send only `session_id`):

```json
{
  "session_id": "...",
  "since_seq": 0,
  "limit": 0
}
```

- `since_seq` (optional, exclusive): only events with `seq` **greater than** this
  value are returned. Use `0` or omit for the start of the ring.
- `limit` (optional): max events in this response. `0` / omitted uses the server
  default (200). The server also clamps to a soft byte budget (~512 KiB) so a
  tool-heavy ring cannot force a multi-megabyte frame.

**Reply** `session.history_result`:

```json
{
  "session_id": "...",
  "events": [ { ...domain event... }, … ],
  "truncated": false,
  "next_since_seq": 0
}
```

- **Each element of `events` is the identical JSON shape as the `event` field
  inside a live `event` push** (same `type` vocabulary and fields). Clients feed
  them straight back through the same reducer used for live events — the server
  does no server-side coalescing; raw chunks are replayed as emitted and the
  client's reducer coalesces them.
- Events are ordered oldest-first, exactly as emitted.
- When `truncated` is true, more events remain; re-request with
  `since_seq = next_since_seq` until `truncated` is false.
- The daemon keeps a **bounded per-session ring buffer (800 events, oldest
  dropped)** for each live session (aligned with the mobile client item cap;
  MADR 0018 E4). It buffers every event kind, including the high-frequency
  `assistant_message_chunk` / `thought_chunk` chunks (replaying them is the
  point).
- **Durable transcript (Phase D):** the same 800-event tail is also written under
  `data_dir/sessions/<id>/history.json` (0600, same uid as the daemon). After a
  session closes or the daemon restarts, `session.history` still returns that
  disk slice for listed (non-deleted) sessions. `session.delete` purges the
  transcript with the session directory. Wire paging caps still apply.
- An **unknown or never-active** session returns
  `{ "session_id", "events": [] }` — an empty list, **not** an error.

Error codes: `bad_payload` (malformed payload only); `session_forbidden` if
another device owns the session.

### `session.prompt` error codes

In addition to `bad_payload` / `session_forbidden` / `session_not_live` /
`session_prompt_failed`:

| code | When |
|---|---|
| `turn_busy` | Queue full or session cannot accept another prompt (`provider.ErrTurnBusy`). On both the OpenCode/httpagent and ACP (`acpagent`/grok) paths, a second prompt while busy is **queued** (FIFO, max 4) and returns `ok` with a notice; `turn_busy` only on overflow. Cancel/close clear the queue. Never auto-dequeues while a permission is pending. |

### Error code reference

Every `code` the daemon can put on an `error`, `auth_error` or `pair_error`
frame. The set is closed and enforced: codes are registered in
`internal/protocol` (`ErrorCodes()`), a test asserts no emit site uses an
unregistered one, and another asserts every registered code appears in this
document. Adding a code fails the build until it is listed here.

Codes are a stable contract — branch on them, not on `message`, which is
human-readable and may change.

**Envelope and routing** — possible on any request:

| code | When |
|---|---|
| `bad_json` | The frame is not a decodable JSON envelope. The only code returned with an empty request `id`, because the id could not be parsed either. |
| `bad_version` | Envelope `v` is not the supported protocol version. |
| `unauthorized` | Any request other than `auth` / `pair.claim` before the socket authenticated. |
| `unknown_type` | Envelope `type` the daemon does not handle. |
| `bad_payload` | Undecodable payload, a missing required id, or an oversize field (`cwd`, `model`, `agent`, `agent_session_id`, `name`). |
| `rate_limited` | Transient throttle — too many failed auth/pair attempts, or too many concurrent async requests on this connection. Back off and retry. |

**Provider and catalog lookups** — `models.list`, `agents.list`,
`agent_sessions.list`, `commands.list`:

| code | When |
|---|---|
| `unknown_provider` | Provider id the daemon has not registered. |
| `provider_unavailable` | Registered provider whose engine is not ready (binary missing, failed to boot). |
| `unsupported` | The provider cannot serve this request — e.g. native session discovery on a provider without it. |
| `agent_sessions_list_failed` | The provider-native session listing itself failed. |

**Session ownership and lifecycle** — these **override** the per-operation code
below on any session-scoped request:

| code | When |
|---|---|
| `session_forbidden` | Another device owns the session. |
| `session_not_live` | The session is missing or no longer live; re-create it via `session.create` before interacting. |
| `session_limit` | The daemon's live-session cap is reached. |
| `shutting_down` | The daemon is stopping and accepted no new work. |
| `turn_busy` | A turn is already active — see the table above. Not a generic failure; do not retry blindly. |
| `bad_agent` | The agent name is unknown, hidden, or a subagent that cannot be started top-level. |

**Per-operation failures** — the fallback when none of the above applies. Each
names the request that produced it: `session_create_failed`,
`session_close_failed`, `session_delete_failed`, `session_prompt_failed`,
`session_cancel_failed`, `session_history_failed`, `session_set_mode_failed`,
`session_set_config_failed`, `session_fork_failed`, `session_revert_failed`,
`session_unrevert_failed`, `session_diff_failed`, `session_rename_failed`,
`session_diagnostics_failed`, `permission_failed`, `question_failed`.

**`auth_error` frames:** `auth_failed` (generic — the detail is withheld from
the peer and logged), `invalid_token`, `client_key_required`,
`client_key_mismatch`, `already_authed`. The two `client_key_*` codes are
**permanent**: re-pair, never retry.

**`pair_error` frames:** `invalid_code`, `expired`, `rate_limited`,
`already_authed`, `unavailable` (pairing not configured on this daemon),
`create_failed` (device-store write failed), `client_key_required`.

### `question.respond` (multi-question forms)

Answers a `question_request` event (MADR 0020 Sprint 1b). **Not** a permission.
Raised by OpenCode's questions and by grok's `ask_user_question` tool (MADR 0022
phase 2), which is also how grok asks what to change after a "request changes" on
a plan:

```json
{
  "session_id": "...",
  "question_id": "<engine request id>",
  "answers": [["core", "cli"], ["ship it"]],
  "cancelled": false
}
```

- `answers[i]` is the list of selected **labels** for `questions[i]` on the request.
- `cancelled: true` rejects the form (`POST /question/{id}/reject` on OpenCode;
  `{"outcome":"cancelled"}` on grok). Answering nothing is also a rejection: grok
  refuses an accepted outcome that carries no answers.
- On grok, a multi-select answer is joined with `", "` into the one string its
  shell echoes to the model.
- Error codes: `bad_payload`, `session_forbidden`, `session_not_live`, `question_failed`.

Domain events (inside live `event` push / history):

| type | fields |
|---|---|
| `question_request` | `question_id`, `status: pending`, `text` (summary), `questions[]` with `header`, `text`, `multiple`, `custom`, `options[]` (`option_id` == label) |
| `question_resolved` | `question_id`, `status`: `resolved` \| `cancelled` |

## Server → client push

| type | payload |
|------|---------|
| `event` | `{ "event": { ... domain event ... } }` |
| `error` | `{ "message", "code?" }` |
| `ok` | none |
| `session.created` | a bare session Meta object (see below) |
| `session.list_result` | `{ "sessions": [ Meta, … ] }` |
| `session.history_result` | `{ "session_id", "events": [ domain event, … ], "truncated?", "next_since_seq?" }` |
| `providers.list_result` | `{ "providers": [ { "id", "ready" }, … ] }` |
| `models.list_result` | picker catalog for one provider (see below) |
| `agent_sessions.list_result` | `{ "provider", "sessions": [ { "id", "cwd?", "title?", "updated_at?" }, … ] }` |
| `session.rename_result` | `{ "session": Meta }` |
| `session.diagnostics_result` | `{ "session_id", "diagnostics": { "branch?", "default_branch?", "vcs?", "mcp?" } }` |

### Session `Meta`

`session.created` carries a Meta object **directly as its payload** (it is not
wrapped in a `session` key). `session.list_result` carries an array of them.

```json
{
  "id": "mcremote session id",
  "provider": "grok",
  "name": "my task",
  "model": "optional model id",
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
- `stop_reason`: set on `turn_complete` — why the turn ended. On `turn_complete`
  the `status` field carries the same value. Specified here for the same reason
  as `tool_status` below: an unspecified vocabulary is how codex drifted and how
  `error` reached users as a contentless line (MADR 0036 D2).

  | Value | Meaning | Client rendering |
  |---|---|---|
  | `end_turn` | normal completion | **silent** — no transcript line |
  | `cancelled` | the user or the daemon interrupted the turn | "Turn cancelled", exactly once |
  | `error` | the turn failed | **silent** — see the pairing rule below |
  | `refusal` | the agent declined to continue | its own line |
  | `max_tokens` | the reply hit the model's length limit | its own line |
  | `max_turn_requests` | the agent hit its per-turn request limit | its own line |

  **Pairing rule.** A `stop_reason` of `error` is **always** accompanied by an
  `error` event for the same turn, carrying the failure text (a generic message
  when the engine supplied none). Clients must therefore render nothing for the
  stop itself — the paired `error` event is the report. Emitting a line for both
  produces a contentless "Turn ended (error)" above the real message.

  **Unknown values.** A provider that cannot map its native reason emits it
  as-is rather than inventing one from this table. Clients must degrade
  gracefully — render the raw value in a generic line rather than dropping the
  event or failing.
- `tool_kind`: on `tool_call` / `tool_call_update`, the ACP tool-kind vocabulary
  (`read`, `edit`, `delete`, `move`, `search`, `execute`, `think`, `fetch`,
  `other`) when the agent supplied one. Clients use it to group actions
  ("Ran N commands", "Edited N files"); absent means unclassified.
- `tool_status` (`status` field on `tool_call` / `tool_call_update`): the
  daemon's tool-lifecycle vocabulary. The mobile reducer keys the spinner
  off `toolStatus == 'running' || 'pending'` and the terminal-state
  formatting off the rest. Defined here because MADR 0035 D2 was a direct
  consequence of this field being unspecified (codex 0.145.0 emitted
  `in_progress`, every other provider emitted `running`, no test caught
  the drift). Allowed values:

  | Value | Meaning |
  |---|---|
  | `pending` | queued; not yet started (the daemon reserved the slot from a future stream) |
  | `running` | the tool is currently executing; show a spinner |
  | `completed` | terminal: the tool returned successfully |
  | `failed` | terminal: the tool returned an error, was denied, or was declined |

  Each provider is responsible for translating its native vocabulary to
  these four values at the event-emit boundary. The reference mapping is
  opencode's `mapToolStatus` (`internal/provider/opencode/http.go:1092-1105`);
  codex's `codexToolStatus` (`internal/provider/codex/items.go`) handles
  the codex v2 enum (`inProgress` → `running`, `declined` → `failed`).

  **Snapshot semantics & detail clipping.** `tool_call_update.text` carries a **snapshot** of tool output/detail (clipped daemon-side at `maxToolOutputChars = 8000`), not an incremental delta. Clients replace the card detail with non-empty incoming text snapshots.

  **Delivery vs Ordering guarantees (MADR 0034 §2.3).**
  - `tool_call` carries both a **delivery guarantee** (blocking transport send) and an **ordering guarantee** (creates a transcript item position, acting as a stream boundary for pending assistant text).
  - `tool_call_update` with a non-empty `tool_id` carries a **delivery guarantee** (must not be dropped under back-pressure) but **no ordering constraint** relative to streaming text chunks, because it mutates an item an earlier `tool_call` already positioned. An update with an empty `tool_id` falls back to boundary semantics.

  **Unknown values.** A provider that meets a native status it cannot map emits
  it as-is rather than inventing one from this table — the contract is "do not
  report a status the agent never gave". Clients must degrade gracefully: treat
  anything that is not `running` or `pending` as terminal.
- `error_kind`: on `error` events, the daemon's classification of the failure —
  `quota` (hard usage/credit limit; retrying won't help until it resets) or
  `rate_limit` (transient throttling). Absent for generic errors. Clients
  should render classified errors as actionable limit cards rather than raw
  provider text.
- `retry_at`: on classified `error` events, an RFC 3339 instant for when the
  limit is expected to lift, when the provider's message carried one. Absent
  when unknown.
- `timed_out`: on `permission_resolved` events, `true` when the request was
  auto-cancelled because the client did not answer within
  `permission_timeout_seconds`. Always accompanies `status: "cancelled"`;
  absent otherwise. Lets a client say "the request timed out" rather than the
  generic "the agent withdrew it".
- `attachments`: on `user_message` events, descriptors for the non-text content
  the prompt carried — `[{ "kind": "image", "mime_type": "image/png" }]`. Kind
  and MIME type only; the bytes are never echoed back. Clients render a
  placeholder chip so an image-only prompt still shows as a turn.
- `title`: on `session_title` events, the session's new display title.

Event `type` values: `session_status`, `user_message`, `assistant_message_chunk`, `thought_chunk`, `tool_call`, `tool_call_update`, `permission_request`, `permission_resolved`, `question_request`, `question_resolved`, `turn_complete`, `error`, `notice`, `available_commands`, `remote_commands`, `plan`, `usage_update`, `session_mode`, `session_config`, `session_capabilities`, `session_title`.

Every type in that list has a section or a field entry in this document, and a
test enforces it (`TestEventTypesAreDocumented`): a new event type fails the
build until it is added here.

### `session_capabilities` event (negotiated ACP support)

Emitted at session create/load from the agent's ACP initialize result. These
fields describe the current engine, not a provider's terminal UI or a guessed
feature set:

```json
{
  "type": "session_capabilities",
  "session_id": "...",
  "capabilities": {
    "image": true,
    "audio": false,
    "load_session": true,
    "embedded_context": true,
    "list_sessions": true,
    "close_session": true,
    "mcp_http": true,
    "mcp_sse": false,
    "mcp_acp": false
  }
}
```

Clients should gate only the corresponding UI affordance on a true value. A
false value means the initialized agent did not negotiate that capability;
future protocol fields are optional and must be ignored when unknown.

### `usage_update` event (token / context usage)

Advisory telemetry: the running token count for the session and the model's
context window, as last reported by the agent.

```json
{
  "type": "usage_update",
  "session_id": "...",
  "usage": { "used": 12480, "size": 258400 }
}
```

- `used`: tokens currently in context.
- `size`: the model's context window, or `0` when the agent did not report one
  (render the count alone, not a percentage).

Unlike most events this one is **droppable** under back-pressure — a stale count
self-corrects on the next report, so it is not worth blocking the stream for.
Clients must therefore not treat a missing update as meaningful. The daemon
suppresses repeats with unchanged numbers, and the first report of each turn is
always sent.

This event is also what enables the `/context` canonical command: it is
unavailable until a session has reported usage at least once.

### `session_config` event (agent-defined config options)

The options the agent exposes for this session, and their current values.
Clients render a settings surface from it and write back with
`session.set_config_option`.

```json
{
  "type": "session_config",
  "session_id": "...",
  "config_options": [
    {
      "id": "model",
      "name": "Model",
      "description": "Which model answers this session",
      "kind": "select",
      "current_value": "claude-sonnet-4-5",
      "values": [
        { "id": "claude-sonnet-4-5", "name": "Sonnet 4.5" },
        { "id": "claude-opus-4-1", "name": "Opus 4.1" }
      ]
    },
    { "id": "verbose", "name": "Verbose output", "kind": "boolean", "bool_value": false }
  ]
}
```

- `kind` is `select` or `boolean`. A `select` carries `values[]` and
  `current_value`; a `boolean` carries `bool_value`.
- `description` is optional.
- The event is a **full replacement** of the option list, not a delta.
- Echo `id` back as `option_id` on `session.set_config_option`, with the same
  `kind`, and `value` as the chosen `values[].id` (select) or `"true"`/`"false"`
  (boolean).

### `session_title` event (session title update)

The agent renamed the session (ACP `sessionInfoUpdate`) — for example after
generating a title from the first prompt.

```json
{
  "type": "session_title",
  "session_id": "...",
  "title": "Fix the migration script"
}
```

Clients should update their session-list label. This is distinct from
`session.rename`, which is the *client* setting a title; this event is the agent
doing so, and either may win — the last one applied is the title.

### `question_request` / `question_resolved` events

Emitted for the multi-question form flow. `question_request` carries
`question_id`, `status: "pending"`, a summary `text`, and `questions[]`; the
client answers with `question.respond`. `question_resolved` ends the request the
same way `permission_resolved` ends a permission — see
[`question.respond`](#questionrespond-multi-question-forms) for the payload
shapes and the resolution contract.

### `session_mode` event (agent operating modes)

Advertises the modes a session can switch between and which one is active.
Clients render a switcher and enable the `/plan` and `/mode` built-ins from it.

```json
{
  "type": "session_mode",
  "session_id": "...",
  "modes": [
    { "id": "default", "name": "Build", "description": "Full tool access; edits allowed" },
    { "id": "plan", "name": "Plan", "description": "Research and plan only; no edits" }
  ],
  "current_mode_id": "plan"
}
```

- The **full list** arrives once at session create/load. Later events carry only
  `current_mode_id` (no `modes`) — a mode *change*, whether from
  `session.set_mode`, a `/plan` command, or the agent switching itself. Clients
  merge: keep the stored list, replace the current id.
- A session with no modes emits nothing; treat mode UI and the mode built-ins as
  unavailable there.
- A mode id of `plan` is the read-only planning state on every provider that has
  one, and is worth surfacing distinctly — it is the difference between an agent
  that will edit files and one that will not.

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

### `remote_commands` event (canonical slash commands)

The canonical vocabulary resolved for this session (MADR 0023). Emitted at
session create and again whenever the answer changes — the agent advertises its
commands, modes arrive, the first usage report lands. Replace-semantics: each
event carries the whole list. Unavailable commands are included **with a
reason**, so a client can explain rather than silently omit:

```json
{
  "type": "remote_commands",
  "session_id": "...",
  "remote_commands": [
    { "name": "help", "description": "List the commands available in this session", "available": true },
    { "name": "plan", "hint": "[off]", "description": "Plan without editing; /plan off returns to the default mode", "available": true },
    { "name": "compact", "description": "Summarise the conversation to reclaim context", "available": false,
      "reason": "grok compacts only in its own terminal UI — over the remote /compact returns nothing" }
  ]
}
```

Clients should offer the `available` entries in autocomplete and use `reason` if
they surface the rest. A daemon that sends no `remote_commands` is older than this
revision; clients fall back to their own built-in list.

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

#### Plan approval (grok)

grok asks its *client* to approve a finished plan (MADR 0022 phase 2). The daemon
raises it as an ordinary `permission_request` — no new message type — recognisable
by `tool_name: "Plan ready for review"`, with the plan markdown as `text`
(truncated at 8 KB) and three fixed options:

| `option_id` | `kind` | Meaning to the agent |
|---|---|---|
| `plan_approve` | `allow_once` | leave plan mode and start implementing |
| `plan_changes` | `reject_once` | keep planning; the agent then asks what to change |
| `plan_abandon` | `reject_always` | discard the plan and turn plan mode off |

Clients need no plan-specific code: answer with `permission.respond` as usual.
Cancelling (or letting it expire) means "keep planning" — never approve, never
abandon. `always_approve` does **not** auto-answer this request.

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
  the agent-side context was cancelled, the session closed while it was pending,
  or it timed out. On session close, one such event is emitted for every
  still-pending request.

`timed_out` is `true` when the `cancelled` above was specifically the
`permission_timeout_seconds` expiry rather than an agent-side abandonment. It is
absent otherwise. Clients that distinguish them can say "the request timed out"
instead of "the agent withdrew it"; clients that do not can ignore the field and
treat every `cancelled` alike.

Beyond `permission_id`, `status` and `timed_out`, the event carries no other
request fields — in particular no `options` and no `tool_id`. Correlate with the
original `permission_request` on `permission_id`.

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
