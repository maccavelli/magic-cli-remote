# mcremote WebSocket protocol v2

<!-- markdownlint-disable MD013 -->

**Status: negotiation (0068 P0), the liveness contract (P1), connection
replacement (P2), gap signalling (P3), and resume (P4) shipped;
operational hygiene lands in P6 and is marked below.**

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
