# magic-cli-remote (`mcremote`)

Provider-agnostic Go daemon that multiplexes coding-agent CLI sessions and exposes secure remote control over **Headscale/Tailscale** for a Flutter client.

**Phase 2 (current):** foundation + **Grok Build ACP** provider (`grok agent stdio`), remote tool permissions over WebSocket, session metadata persistence. Fake provider remains for tests/smoke.

## Requirements

- Go **1.26.x** (developed on 1.26.5)
- Linux or macOS
- Optional: Headscale + Tailscale clients for remote access ([docs/headscale.md](docs/headscale.md))
- For `setup-service`: **systemd** with a user session (`systemctl --user`)

## Quick start

```bash
make build
./bin/mcremote version
./bin/mcremote --version
./bin/mcremote serve
```

Install the host OS/arch binary into the user bin directory (`~/.local/bin` on Linux and macOS):

```bash
make install
# → ~/.local/bin/mcremote

# optional overrides:
make install USER_BIN_DIR=$HOME/bin
make build GOOS=linux GOARCH=arm64   # cross-compile only (no install)
```

**Build versions (robust ledger):** each `make build` / `make install` stamps
the binary as `BASE.N` where `BASE` is the latest **release** tag
(`v0.2.1` → `0.2.1`) and `N` is a global build serial (`0.2.1.1`, `0.2.1.2`, …).

| Piece | Role |
|-------|------|
| Release tags `vX.Y.Z` | Product version base (manual / release process) |
| Build tags `build/X.Y.Z.N` | **Source of truth** for N — claimed by creating+pushing the tag |
| `.build-counter` | Local cache only (gitignored); speeds offline / reduces fetch races |

Allocation (`scripts/next-build-version.sh`): fetch `build/*` tags → max N →
create `build/BASE.N` → **push with retry** on contention. CI always pushes
(so all runners share one sequence). Local builds try to push when `origin` is
reachable; offline builds append a uniqueness suffix (commit / run id).

Override: `make build VERSION=1.2.3`. Disable push: `MCREMOTE_VERSION_PUSH=0 make build`.

All long flags use a **double dash** (`--help`, `--config`, `--listen-host`, `--setup-service`, …). Short `-h` is help only.

### Install as a user systemd service (recommended)

```bash
make install                          # binary → ~/.local/bin/mcremote
mcremote setup-service --force        # unit only (does not copy the binary)
# or:
mcremote --setup-service

systemctl --user status mcremote
journalctl --user -u mcremote -f
```

`setup-service` installs the **systemd user unit only** (never overwrites a running binary):

1. Write `~/.config/systemd/user/mcremote.service` from the **embedded** unit template  
   (`ExecStart` defaults to `~/.local/bin/mcremote` when present)
2. `systemctl --user daemon-reload && enable && start`
3. `loginctl enable-linger $USER` (daemon keeps running after logout)

Update the binary later with `make install` (stops the user unit if active, installs via atomic rename, restarts).

Preview the unit without installing:

```bash
./bin/mcremote setup-service --print-only
```

In another terminal (or after the service is up):

```bash
./bin/mcremote pair code --name phone --qr
# or long-lived token:
./bin/mcremote pair create --name phone --qr

curl -s http://127.0.0.1:7531/healthz
```

Connect a WebSocket client to `ws://127.0.0.1:7531/v1/ws`, send `auth`, then `session.create`. See [docs/protocol-v1.md](docs/protocol-v1.md).

## CLI reference

```text
mcremote [--config PATH] [--log-level LEVEL] [--log-format FORMAT] [--version] [--help]
mcremote serve [--listen-host HOST] [--listen-port PORT] [--data-dir DIR]
mcremote pair code|create|list|revoke ...
mcremote setup-service | mcremote --setup-service
mcremote version
```

| Command | Purpose |
|---------|---------|
| `serve` | Run the daemon in the foreground |
| `pair code` | 8-char short code (5 min, one-shot) — preferred phone pairing |
| `pair create` | Long-lived `mcr_…` device token |
| `pair list` / `pair revoke` | Manage devices |
| `setup-service` / `--setup-service` | Install systemd **user** unit + start |
| `version` / `--version` | Print version |

Full flag and environment tables: **[docs/config.md](docs/config.md)**.

### Global flags

| Flag | Description |
|------|-------------|
| `--config` | Config YAML (`MCREMOTE_CONFIG`) |
| `--log-level` | `debug` \| `info` \| `warn` \| `error` |
| `--log-format` | `text` \| `json` |
| `--help` / `-h` | Help |
| `--version` | Version |
| `--setup-service` | Install + enable + start user systemd unit |

### `serve` flags

| Flag | Description |
|------|-------------|
| `--listen-host` | Bind host (default from config: `127.0.0.1`) |
| `--listen-port` | Bind port (default `7531`) |
| `--data-dir` | State directory (devices, pair codes, sessions) |

### `setup-service` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--unit-name` | `mcremote` | Unit name (no `.service`) |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | Path used in `ExecStart` (not copied) |
| `--service-config` | | Config path embedded in unit |
| `--data-dir` | | Passed through to `serve` |
| `--listen-host` | `0.0.0.0` | Mesh/phone-friendly bind |
| `--listen-port` | `7531` | Port |
| `--working-directory` | `$HOME` | systemd `WorkingDirectory` |
| `--env KEY=VAL` | | Extra environment (repeatable) |
| `--print-only` | | Print unit; do not install |
| `--force` | | Overwrite existing unit |
| `--no-enable` / `--no-start` / `--no-linger` | | Skip enable / start / linger |

```bash
mcremote setup-service --listen-host 0.0.0.0 --listen-port 7531 \
  --config ~/.config/mcremote/config.yaml --force
mcremote setup-service --env 'MCREMOTE_LOG_LEVEL=debug' --force
```

## Environment variables

| Variable | Description |
|----------|-------------|
| `MCREMOTE_CONFIG` | Config file path |
| `MCREMOTE_LISTEN_HOST` | Bind address |
| `MCREMOTE_LISTEN_PORT` | Bind port |
| `MCREMOTE_LOG_LEVEL` | Log level |
| `MCREMOTE_LOG_FORMAT` | `text` or `json` |
| `MCREMOTE_DATA_DIR` | Data directory |
| `MCREMOTE_AUTH_REQUIRE_DEVICE_TOKEN` | Require device tokens (`true`/`false`) |
| `MCREMOTE_PAIR_HOST` | Host shown in pair QR/code (e.g. tailnet IP) |

```bash
export MCREMOTE_LISTEN_HOST=0.0.0.0
export MCREMOTE_LISTEN_PORT=7531
export MCREMOTE_LOG_LEVEL=info
export MCREMOTE_PAIR_HOST=100.64.0.1:7531
```

See [docs/config.md](docs/config.md) for the full table and YAML keys.

## Configuration

Default config path: `~/.config/mcremote/config.yaml` (XDG).  
Default listen (config): **`127.0.0.1:7531`**.  
Examples: [configs/config.example.yaml](configs/config.example.yaml).

```bash
./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531 --log-level debug
```

Precedence: **CLI flags > environment > config file > defaults**.

## Device pairing

```bash
# Preferred: 8-char code (5 min, one-shot). Phone: Enter code or Scan QR
mcremote pair code --name phone --qr
# or:
./scripts/start-mcremote-grok.sh --pair phone

# Long-lived token (advanced / offline):
mcremote pair create --name phone --qr
./scripts/start-mcremote-grok.sh --pair-token phone

mcremote pair list
mcremote pair revoke <id-or-name>
```

Short-code QR encodes `mcremote://pair?host=…&code=…` (no permanent secret in the QR).
Long-token QR still supports `…&token=mcr_…`. Host defaults to Tailscale IPv4 or
`MCREMOTE_PAIR_HOST`. After a successful claim/connect the app stores the durable
`mcr_…` token for reconnect — re-pair only if you revoke.

Tokens are stored as **SHA-256** hashes under `~/.local/share/mcremote/devices.json`.

## Grok sessions

Requires a logged-in Grok Build CLI (`grok` on `PATH`).

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"grok", "name":"task", "cwd":"/path/to/repo" } }
```

When the agent needs tool approval, the server pushes `permission_request` events; answer with `permission.respond` (see [docs/protocol-v1.md](docs/protocol-v1.md)).

Set `providers.grok.always_approve: true` in config to skip remote permission prompts.

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/0001-architecture-mcremote.md](docs/0001-architecture-mcremote.md) | Architecture MADR |
| [docs/0002-community-assessment-and-stack-recommendations.md](docs/0002-community-assessment-and-stack-recommendations.md) | Landscape report |
| [docs/0003-phase1-decisions.md](docs/0003-phase1-decisions.md) | Phase 1 locked decisions |
| [docs/0004-phase2-grok-acp.md](docs/0004-phase2-grok-acp.md) | Phase 2 Grok ACP |
| [docs/protocol-v1.md](docs/protocol-v1.md) | WebSocket JSON schema |
| [docs/config.md](docs/config.md) | Config, flags, and env reference |
| [docs/headscale.md](docs/headscale.md) | Mesh grants & pairing |

## Deploy

| Method | Path |
|--------|------|
| **User service (preferred)** | `mcremote --setup-service` — embedded template |
| User unit example | [deploy/systemd/mcremote.user.service](deploy/systemd/mcremote.user.service) |
| System-wide unit | [deploy/systemd/mcremote.service](deploy/systemd/mcremote.service) |
| launchd | [deploy/launchd/com.magiccliremote.mcremote.plist](deploy/launchd/com.magiccliremote.mcremote.plist) |

Useful after setup:

```bash
systemctl --user status mcremote
systemctl --user restart mcremote
journalctl --user -u mcremote -f
systemctl --user disable --now mcremote
```

## Android companion (Magic CLI Remote)

Flutter app lives in [`apps/mobile`](apps/mobile) (Android-only Phase 3a).

```bash
cd apps/mobile
flutter pub get
flutter run
```

Use host `10.0.2.2:7531` from the Android emulator (daemon must listen on `0.0.0.0`). See [apps/mobile/README.md](apps/mobile/README.md).

## CI / CD (GitHub Actions)

Workflows live under [`.github/workflows/`](.github/workflows/):

| Workflow | Trigger | What it does |
|----------|---------|----------------|
| `ci.yml` | push / PR / manual | Go test+build, Flutter test, **release arm64 APK** on `ubuntu-latest` |
| `release-apk.yml` | tag `v*` / manual | Build APK and attach to a GitHub Release |

**Node.js:** CI pins **Node.js 24 LTS** via `actions/setup-node`, `.node-version`, and `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

Download the APK from Actions **Artifacts**, or from a Release after tagging:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Local APK:

```bash
./scripts/build-apk.sh
```

## Headless protocol smoke (no GUI)

```bash
./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531
./bin/mcremote pair create --name smoke   # copy token
./scripts/smoke-protocol.sh -token 'mcr_…' -provider fake
```

## Development

```bash
make test
make race
make vet
cd apps/mobile && flutter test
```

Module: `github.com/maccavelli/magic-cli-remote`

## License

See repository license when published.
