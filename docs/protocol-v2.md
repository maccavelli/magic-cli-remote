# mcremote WebSocket protocol v2

<!-- markdownlint-disable MD013 -->

**Status: negotiation (0068 P0) and the liveness contract (P1) shipped;
replacement, gap signalling and resume land in later 0068 phases and are
marked below.**

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
- On success, `auth_ok` / `pair_ok` carry `protocol: <picked>` (omitted
  for v1 clients, keeping the v1 response byte-identical). `auth_ok`
  additionally carries `caps` when v2 was picked (below).
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
  "history_ring": 800,
  "max_frame_bytes": 1048576,
  "tls_resumed": false
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
- `history_ring` — per-session event ring size (v1: implicit 800).
- `max_frame_bytes` — both directions' frame cap (v1: implicit 1 MiB).
- `tls_resumed` — whether this connection's TLS handshake resumed a prior
  session. Lets clients verify their TLS session cache works (0068 Q3).
- `resume` — absent until 0068 P4; when present:
  `{"window_ms": <granted resume window>}`.

Clients must tolerate unknown keys in `caps` (additive evolution).

## Planned v2 additions (not yet shipped)

Marked per 0068 phase; each updates this document when it lands:

- ~~**P1 — liveness**~~ **Shipped 2026-08-04**: server WS pings with
  pong-extended horizon (above); kernel TCP keepalive (25 s idle, 5 s × 4
  probes ≈ 45 s reap) on the daemon and relay listeners and the
  relay-host's outbound legs; the relay reaps a silent post-upgrade peer
  after 10 s (first-envelope deadline). Config:
  `limits.ws_read_deadline_seconds`, `limits.tcp_keepalive.*`.
- **P2 — connection replacement**: a successful `auth` closes the device's
  older authenticated sockets with close code **4001 `replaced`**. A client
  receiving 4001 must not auto-reconnect (a newer connection of the same
  device exists).
- **P3 — gap signalling**: `session.history_result` and `session.list`
  entries gain `first_seq` / `latest_seq`; `session.list` and `caps` gain
  the daemon **boot epoch** so clients detect seq regression after an
  unclean daemon restart.
- **P4 — resume**: `auth` gains an optional
  `resume: {"token": …, "sessions": {"<id>": last_seq}}`; `auth_ok` gains
  `resumed: {"sessions": {"<id>": {"first_seq": …, "latest_seq": …}}}` or
  `resume_failed: true`. Resume failure is not an auth failure; the
  fallback is the ordinary v1 reconcile. (R1: this rides `auth` rather
  than a separate message to save one round trip; the MADR records the
  alternative.)
- **P6 — hygiene**: `retry_after_ms` on `rate_limited` /
  `too many clients` refusals.

## Compatibility matrix

| Client | Daemon | Result |
| --- | --- | --- |
| v1 (shipped Android) | v2 | Byte-identical v1 behaviour (0068 U1 golden test) |
| v2 | v1 | `protocols` is an unknown payload field, ignored; no `protocol`/`caps` in auth_ok → client stays on v1 semantics |
| v2 | v2 | Negotiated v2; capability block governs |

The relay (`mcrelay`) requires no upgrade: the inner protocol rides the
splice opaquely; join-plane changes arrive only with 0068 P2/P6.
