# mcremote WebSocket protocol v2

<!-- markdownlint-disable MD013 -->

**Status: complete.** Negotiation (0068 P0), the liveness contract (P1),
connection replacement (P2), gap signalling (P3), resume (P4), client
engine hardening (P5, no wire changes), and operational hygiene (P6) have
all shipped. This document is the finalized v2 contract.

v2 is a **delta over [protocol-v1.md](protocol-v1.md)**: the envelope
format, message types, auth model, and error codes are unchanged. v2 adds a
negotiated capability surface and (in later phases) a formal connection
lifecycle. Design record: [MADR 0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md).

## Version negotiation (shipped)

- The envelope `v` field is the *sender's* protocol version. Every
  connection starts strictly v1: until negotiation completes, inbound
  frames with `v != 1` are rejected `bad_version`, exactly as v1 specifies.
- `auth` and `pair.claim` payloads may carry the client's offer:

  ```json
  {"token": "mcr_…", "protocols": [1, 2]}
  ```

  Absent `protocols` means a v1 client. The server picks the **highest
  mutual** version. A non-empty offer with no mutual version is rejected
  `bad_version` — before an auth strike is counted, and before a pair code
  is consumed — and the client may retry (e.g. offering `[1]`).
- A v2 client that implements the additive Codex application surface also sends
  `"codex_surface_version": 1` on both `auth` and `pair.claim`. Absence means
  the generic v2 protocol only; it never opts an old client into Codex-specific
  operations.
- On success, `auth_ok` / `pair_ok` carry `protocol: <picked>` (omitted
  for v1 clients, keeping the v1 response byte-identical). Both carry `caps`
  when v2 was picked, so a newly paired connection can use negotiated surfaces
  without reconnecting.
- **Accept rules after v2 is negotiated:**
  - The server accepts inbound `v` in `[1, negotiated]`.
  - The client accepts inbound `v` up to the highest version it offered —
    not the negotiated value — because the `auth_ok` concluding the
    negotiation is already stamped with the picked version.
  - Server fan-out frames (`event`) are marshalled once and shared across
    recipients, so they may carry `v: 1` on a v2 connection. `v` is
    per-frame provenance, not per-frame semantics.
- `GET /v1/hello` now also reports `"protocols": [1, 2]` alongside the
  legacy `"protocol": 1`.

## Capability block (shipped)

`auth_ok.caps`, present iff the negotiated version is ≥ 2. Values are
built from the same specification the server enforces with
(`internal/ws/liveness.go`) — **advertised numbers are enforced numbers**:

```json
"caps": {
  "protocol": 2,
  "read_deadline_ms": 60000,
  "ping_interval_ms": 10000,
  "ws_ping_resets_deadline": true,
  "resume": {"window_ms": 120000},
  "history_ring": 16384,
  "history_budget_bytes": 33554432,
  "max_frame_bytes": 1048576,
  "tls_resumed": false,
  "epoch": "a1b2c3d4e5f60718",
  "codex_surface": {
    "version": 1,
    "operations": ["rpc:account/read"],
    "experimental": ["rpc:server/diagnostics"],
    "max_page_size": 100,
    "max_text_bytes": 262144,
    "max_binary_chunk_bytes": 262144
  }
}
```

- `read_deadline_ms` — the rolling inbound-data deadline this connection
  is subject to.
- `ping_interval_ms` — the app-level ping cadence the server expects
  (informative; MADR 0063 locked 10 s).
- `ws_ping_resets_deadline` — whether transport-level pongs also reset the
  deadline. **`true` since 0068 P1**: the daemon pings v2 connections at
  `read_deadline/3` and a completed pong extends the horizon, so a v2
  client that is merely *reading* stays alive. The app-level ping remains
  the primary contract (it proves the event loop, not just the WS stack —
  0063), and is still all that keeps a **v1** connection alive.
- `resume` — present on every v2 `auth_ok` since 0068 P4:
  `{"window_ms": <granted resume window>}` (server ceiling
  `limits.ws_resume_window_seconds`, default 120 s).
- `history_ring` — per-session ring size **in events**. Since MADR 0138 the
  daemon bounds retention by bytes, so this is a conservative estimate derived
  from that budget rather than a hard count; it stays an event count because
  that is what it has always meant and clients size their own buffers from it.
- `history_budget_bytes` — the per-session retention budget the daemon actually
  enforces (MADR 0138 Phase 2). Additive: absent means a daemon that still
  bounds retention by an event count.
- `max_frame_bytes` — both directions' frame cap (v1: implicit 1 MiB).
- `tls_resumed` — whether this connection's TLS handshake resumed a prior
  session. Lets clients verify their TLS session cache works (0068 Q3).
- `epoch` — the daemon's seq-lineage id (0068 P3); omitted when the
  daemon runs without a session store. Clients that see it change drop
  every cached seq.
- `codex_surface` — present only when the authenticated v2 device advertised
  Codex surface version 1. Capability ids are bytewise sorted from the pinned
  installed manifest; experimental ids remain separately labeled. The three
  numeric limits are enforced response/request bounds, not hints.

Clients must tolerate unknown keys in `caps` (additive evolution).

## Codex surface version 1

The following host-global operations require an authenticated v2 connection
that advertised `codex_surface_version: 1`; none requires session ownership:

- `codex.runtime.read` → `codex.runtime.result`: a typed snapshot of account
  plan, usage/rate windows/workspace messages, model/context capabilities,
  transport/generation, sanitized config provenance, feature state, and a
  bounded MCP server list.
- `codex.doctor.run` → `codex.doctor.result`: runs exactly
  `codex doctor --json`, single-flight, with a 30-second timeout and 256 KiB
  output bound. Only schema version 1 is accepted. Known checks are projected
  into typed summaries; unknown checks retain only id/status/summary. Paths,
  URLs, credentials, remediation commands, and raw report data are never sent
  or persisted. The operation never repairs or uploads anything and is never
  run periodically.
- `codex.permissions.write` → `codex.permissions.result`: atomically writes the
  selected `default_permissions` and independent `approvals_reviewer` values
  using the last user-layer version and requests Codex hot reload. The daemon
  accepts only a catalog profile whose effective managed `allowed` state is
  true and reviewer `user` or `auto_review`. The result is the refreshed typed
  runtime snapshot; conflicts or managed denials leave both values unchanged.

The runtime snapshot's `permission_profiles` list contains bounded opaque ids,
optional descriptions, `allowed`, and `dangerous`; managed-disallowed entries
remain visible in the data model but clients omit them from the chooser. Its
config projection reports requested/effective profile and reviewer values plus
sanitized layer classes and an explanatory managed-policy label. It never
contains config values, policy documents, developer instructions, or paths.

Two further Codex-surface operations are **session-scoped** and require
ownership of the named session, not just an authenticated device:

- `codex.execution.read` → `codex.execution.read_result`: bounded terminal
  listing and sequence-numbered output replay for one session, plus the
  host-owned execution-environment catalog and its observational status/info
  reads. The environment arms carry configured ids and allowed roots only; the
  exec-server URL, connect timeout, and any remote credential never leave the
  daemon, and a phone can neither register nor edit an environment.
- `codex.execution.write` → `codex.execution.write_result`: the three distinct
  execution authorities (`exec` sandboxed argv, `shell` full host access,
  `spawn` default-off standalone process), terminal control (`write`,
  `resize`, `stop`, `stop_all`), and `select_environment`. Both unsandboxed
  authorities require `confirm: "run unsandboxed"` on every single call, and
  `select_environment` requires `confirm: "change execution environment"`.
  Confirmation is never cached against the session.

`codex.terminal.output` is pushed to the owning device's Codex-surface
connections as terminal bytes arrive. It is deliberately not an `event` frame:
event types enter session history and the retained ring, while terminal output
lives only in a bounded 1 MiB per-terminal daemon replay buffer. Clients
recover missed chunks through `codex.execution.read` action `output` with
`after_sequence`, and must render `sequence_gap: true` as a discontinuity.

An execution failure reported as `outcome_unknown` may already have run on the
host. Clients must never auto-retry it; they re-read the terminal list and let
the user decide.

The keyed `question.respond` object and additive question `id`, `secret`, and
option `description` fields documented in protocol v1 are base-protocol
additions and therefore apply unchanged on negotiated v2 connections. The
legacy ordered answer array remains decode-only compatibility.

## v2 additions by phase (all shipped)

- ~~**P1 — liveness**~~ **Shipped 2026-08-04**: server WS pings with
  pong-extended horizon (above); kernel TCP keepalive (25 s idle, 5 s × 4
  probes ≈ 45 s reap) on the daemon and relay listeners and the
  relay-host's outbound legs; the relay reaps a silent post-upgrade peer
  after 10 s (first-envelope deadline). Config:
  `limits.ws_read_deadline_seconds`, `limits.tcp_keepalive.*`.
- ~~**P2 — connection replacement**~~ **Shipped 2026-08-04**: a successful
  `auth` closes the device's older authenticated sockets with close code
  **4001 `replaced`**, freeing their slots synchronously — a device's own
  zombies can never exhaust `max_ws_clients` against it. A client
  receiving 4001 must not auto-reconnect (a newer connection of the same
  device exists); the shipped client parks quietly, keeps its pairing,
  and reconnects on the next user-driven action. Applies to v1 and v2
  clients alike (server-initiated close was always legal; the code is the
  new information). Not applied when device tokens are off (dev mode
  shares one identity), and **not** applied at the relay join plane —
  joins carry no device identity by design (0068 Q5).
- ~~**P3 — gap signalling**~~ **Shipped 2026-08-04**:
  `session.history_result` gains `first_seq`/`latest_seq` (the retained
  ring window — a `since_seq` below `first_seq` means truncation, which
  was previously silent); `session.list_result` gains
  `seqs: {"<id>": {first_seq, latest_seq}}` plus `epoch`; `auth_ok.caps`
  gains `epoch`. The epoch is the daemon's seq-lineage id: kept across
  clean restarts, minted fresh after an unclean one (up to 5 s of events
  may be unflushed and seq can regress) — a client seeing it change drops
  every cached seq. Clients whose cached seq equals `latest_seq` skip the
  history walk entirely (the 3-second app-switch resume costs two list
  calls and nothing else); the shipped client also re-arms an interrupted
  resync (bounded retries). All fields additive — v1 clients ignore them.
- ~~**P4 — resume**~~ **Shipped 2026-08-04**: every v2 `auth_ok` issues a
  fresh `resume_token` (rotated per auth — an elder connection's token
  dies with D3's replacement) and grants `caps.resume.window_ms`
  (server ceiling `limits.ws_resume_window_seconds`, default 120 s; the
  client may request narrower via `auth.resume_window_ms`, never wider).
  A within-window reconnect sends
  `resume: {"token": …, "sessions": {"<id>": last_seq}}` on `auth`;
  success answers `resumed: {"sessions": {"<id>": {first_seq,
  latest_seq}}}` (only sessions the daemon knows and the device may
  access — anything absent must be reconciled normally), failure answers
  `resume_failed: true` with auth itself still succeeding. Tokens are
  memory-only: a daemon restart invalidates them and the epoch path (P3)
  covers that case. The shipped client skips its entire reconcile — both
  list calls — when every locally known session is confirmed unchanged.
  (R1: rides `auth` rather than a separate message to save one round
  trip; the MADR records the alternative.)
- ~~**P6 — hygiene**~~ **Shipped 2026-08-05**: refusals tell the client
  when to come back.
  - Daemon genuine-capacity refusal (every slot held by an authenticated
    client) closes with `1013` and reason
    `too many clients; retry_after_ms=<n>` — the soonest deadline horizon
    across current clients, floor 5 s. The hint rides the close reason
    because the refusal precedes any envelope exchange.
  - Relay join-plane `error` payloads gain optional `retry_after_ms`:
    the fixed-window remainder on `rate_limited` (also sent as a standard
    `Retry-After` header on the HTTP 429 upgrade refusal), a 5 s courtesy
    floor on capacity `limit`.
  - Clients treat the hint as a **floor** on their next backoff delay —
    never a promise of success, never a shortening, clamped client-side
    (60 s) so a confused server cannot park reconnection.
  - Ops (server-side only, no protocol surface): a 30 s relay sweep
    self-corrects leaked per-host phone-slot counters after two
    consecutive divergent observations; `make debug` builds expose
    `/debug/pprof/goroutineleak` (Go 1.26) on a loopback-only listener.

## Compatibility matrix

| Client | Daemon | Result |
| --- | --- | --- |
| v1 (shipped Android) | v2 | Byte-identical v1 behaviour (0068 U1 golden test) |
| v2 | v1 | `protocols` is an unknown payload field, ignored; no `protocol`/`caps` in auth_ok → client stays on v1 semantics |
| v2 | v2 | Negotiated v2; capability block governs |

The relay (`mcrelay`) requires no upgrade for v2 itself: the inner
protocol rides the splice opaquely. The only join-plane change 0068
shipped is the additive `retry_after_ms` field (P6), which old clients
ignore.
