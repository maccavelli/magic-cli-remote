# Architecture

magic-cli-remote is a provider-agnostic Go daemon that multiplexes coding-agent CLI sessions and exposes remote control over WebSocket plus JSON to a Flutter phone app.

## Deployment units

| Unit | Role |
| :--- | :--- |
| **mcremote** | Host daemon. Owns providers, sessions, TLS, pairing, and the protocol endpoint. |
| **mcrelay** | Optional public-edge join router. Hosts register; phones join; the edge splices opaque WebSockets. It does not authenticate devices, run agents, or see inner-hop plaintext. |
| **Magic CLI Remote** | Flutter companion. Android is the shipped product APK. Linux desktop is the development target. iOS builds and runs on the simulator. |

Binaries install under `$HOME` (`~/.local/bin` on Unix). The installer never uses root.

## Access paths

The phone selects a path; the daemon does not.

- **Mesh** — Tailscale/Headscale. The phone opens TCP on the host's tailnet address (default port 7531). Headscale coordinates login, keys, IPs, and policy; it does not reverse-proxy mcremote.
- **Relay** — mcrelay when the phone is off-mesh. End-to-end TLS to mcremote; the splice is opaque.
- **LAN / loopback** — development and same-network use.

Pairing QR encodes `mcremote://pair?host=…&code=…&fp=…&mode=…`. After claim, the app stores a durable `mcr_…` token.

## Host daemon

```text
  Flutter app (Android product; Linux desktop for dev)
       │  wss:// (or ws:// offline/dev)
       │  pair QR: mcremote://pair?host=…&code=…&fp=…&mode=…
       ▼
  ┌──────────────── mcremote (host) ────────────────┐
  │  TLS (letsencrypt DNS-01 / selfsigned / off)    │
  │  HTTP: GET /healthz, GET /v1/hello, GET /v1/ws  │
  │  auth (device token + optional client TLS key)  │
  │  session manager + event bus + history ring     │
  │  providers: grok, opencode, codex, kilo, fake   │
  │  admin.sock (local Unix, pair-revoke kick)      │
  └───────────┬─────────────┬───────────────────────┘
              │             │ optional outbound
              │             ▼
              │        mcrelay (public edge)
              │        register / join / splice
              ▼
     agent engines (shared processes where applicable)
       grok agent stdio · opencode serve · codex app-server · kilo serve
```

HTTP surface:

| Endpoint | Auth | Purpose |
| :--- | :--- | :--- |
| `GET /healthz` | none | Liveness |
| `GET /v1/hello` | device token (and client key when required) | Capability probe; reports `protocols: [1, 2]` |
| `GET /v1/ws` | device token after upgrade | Protocol v1/v2 control plane |

TLS modes: Let's Encrypt (DNS-01 on mcremote; HTTP-01 or DNS-01 on mcrelay), file certs, self-signed, or off behind an external terminator. Client frames are capped at 1 MiB serialized UTF-8 JSON.

A local Unix admin socket lives under the runtime directory. `pair revoke` uses it to kick live WebSocket clients.

## Protocol

The same `GET /v1/ws` endpoint serves both versions. Every connection starts as
protocol v1. A client may offer `protocols` on `auth` or `pair.claim`; the server
picks the highest mutual version. Absent an offer, the client stays v1.

v2 is a delta over [protocol-v1.md](guides/protocol-v1.md). Envelope format, message types, auth model, and error codes stay the same. v2 adds:

- a capability block on `auth_ok.caps` (read deadline, ping cadence, resume window, history ring, frame cap, epoch, optional Codex surface)
- WebSocket-ping liveness with a pong-extended deadline
- connection replacement (close code `4001` on a newer login)
- gap signalling (`first_seq` / `latest_seq` / `epoch`)
- reconnect resume (`resume_token` + `auth.resume`)

Full contract: [protocol-v2.md](guides/protocol-v2.md).

Sessions are owned by the device that created them. Handoff is `session.release` then `session.claim` (optional `to_device_id`).

## Providers

Registered adapters: **grok**, **opencode**, **codex**, **kilo**, and **fake**
(tests). Each talks to a host-side engine process (ACP stdio, `opencode serve`,
`codex app-server`, `kilo serve`). Missing binaries for enabled providers mark
those engines not ready; the daemon still starts.

The phone drives session lifecycle (`session.create`, prompt, cancel, mode/model,
permission answers) over the WebSocket. Host `~/.codex` and peer CLI configs are
inherited by those engines, not isolated by mcremote.

## Receipts

Signed receipts are opt-in (`receipts.enabled: false` by default). Matching
permission decisions append a JWS compact Statement to
`<data_dir>/receipts/<device_id>.jsonl`, hash-chained per device. The phone
re-verifies its own chain on device. Guide: [receipts.md](guides/receipts.md).

## Configuration and paths

Linux and macOS use XDG (`~/.config/mcremote`, `~/.local/share/mcremote`, …).
Windows uses Known Folders (`%AppData%\mcremote` for config,
`%LocalAppData%\mcremote` for the rest). Inspect with `mcremote paths`.
Precedence: CLI flags, `MCREMOTE_*` environment, `config.yaml`, built-in
defaults. mcrelay uses the same layout under the `mcrelay` leaf and `MCRELAY_*`.

Guides: [config.md](guides/config.md), [config-mcrelay.md](guides/config-mcrelay.md).

## Services

`mcremote setup-service` and `mcrelay setup-service` install a user unit:

- Linux: systemd `--user` (optional lingering so the daemon survives logout)
- macOS: LaunchAgent for the login session
- Windows: Task Scheduler at-logon task, LeastPrivilege

Everything stays under the installing user's home. Privileged ports 80/443 need a proxy, capabilities, or DNS-01.

## Repository map

```text
cmd/mcremote, cmd/mcrelay
internal/   daemon, ws, session, provider, auth, relay, receipt, admin, …
apps/mobile Flutter companion
configs/    example YAML
deploy/     systemd and launchd unit examples
docs/       this tree
```
