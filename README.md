# magic-cli-remote (`mcremote`)

Provider-agnostic Go daemon that multiplexes coding-agent CLI sessions and exposes secure remote control over **Headscale/Tailscale** for a Flutter client.

**Phase 2 (current):** foundation + **Grok Build ACP** provider (`grok agent stdio`), remote tool permissions over WebSocket, session metadata persistence. Fake provider remains for tests/smoke.

## Requirements

- Go **1.26.x** (developed on 1.26.5)
- Linux or macOS
- Optional: Headscale + Tailscale clients for remote access ([docs/headscale.md](docs/headscale.md))

## Quick start

```bash
make build
./bin/mcremote version
./bin/mcremote serve
```

In another terminal:

```bash
./bin/mcremote pair create --name phone
# copy the mcr_… token (shown once)

curl -s http://127.0.0.1:7531/healthz
```

Connect a WebSocket client to `ws://127.0.0.1:7531/v1/ws`, send `auth`, then `session.create` with `"provider":"fake"`. See [docs/protocol-v1.md](docs/protocol-v1.md).

## Configuration

Default config path: `~/.config/mcremote/config.yaml` (XDG).  
Default listen: **`127.0.0.1:7531`**.  
Examples: [configs/config.example.yaml](configs/config.example.yaml).

```bash
./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531 --log-level debug
```

## Device pairing

```bash
mcremote pair create --name phone
mcremote pair list
mcremote pair revoke <id-or-name>
```

Tokens are stored as **SHA-256** hashes under `~/.local/share/mcremote/devices.json`.

## Grok sessions

Requires a logged-in Grok Build CLI (`grok` on `PATH`).

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"grok", "name":"task", "cwd":"/path/to/repo" } }
```

When the agent needs tool approval, the server pushes `permission_request` events; answer with `permission.respond` (see [docs/protocol-v1.md](docs/protocol-v1.md)).

Set `providers.grok.always_approve: true` (or CLI `--always-approve`) to skip remote permission prompts.

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/0001-architecture-mcremote.md](docs/0001-architecture-mcremote.md) | Architecture MADR |
| [docs/0002-community-assessment-and-stack-recommendations.md](docs/0002-community-assessment-and-stack-recommendations.md) | Landscape report |
| [docs/0003-phase1-decisions.md](docs/0003-phase1-decisions.md) | Phase 1 locked decisions |
| [docs/0004-phase2-grok-acp.md](docs/0004-phase2-grok-acp.md) | Phase 2 Grok ACP |
| [docs/protocol-v1.md](docs/protocol-v1.md) | WebSocket JSON schema |
| [docs/config.md](docs/config.md) | Config reference |
| [docs/headscale.md](docs/headscale.md) | Mesh grants & pairing |

## Deploy

- systemd: [deploy/systemd/mcremote.service](deploy/systemd/mcremote.service)
- launchd: [deploy/launchd/com.magiccliremote.mcremote.plist](deploy/launchd/com.magiccliremote.mcremote.plist)

## Development

```bash
make test
make race
make vet
```

Module: `github.com/maccavelli/magic-cli-remote`

## License

See repository license when published.
