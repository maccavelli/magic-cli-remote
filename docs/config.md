# mcremote configuration

## Locations (XDG)

| Item | Path |
|------|------|
| Config dir | `$XDG_CONFIG_HOME/mcremote` or `~/.config/mcremote` |
| Config file | `config.yaml` inside config dir |
| Data dir | `$XDG_DATA_HOME/mcremote` or `~/.local/share/mcremote` |
| Devices | `<data_dir>/devices.json` (mode `0600`) |
| Pair codes | `<data_dir>/pair_codes.json` (mode `0600`) |
| User unit | `~/.config/systemd/user/mcremote.service` |

Override config path: `--config /path/to.yaml` or `MCREMOTE_CONFIG`.

## Precedence

1. CLI flags (`--listen-host`, …)  
2. Environment (`MCREMOTE_*`)  
3. Config file  
4. Built-in defaults  

## Defaults

| Key | Default |
|-----|---------|
| `listen.host` | `127.0.0.1` |
| `listen.port` | `7531` |
| `log.level` | `info` |
| `log.format` | `text` |
| `auth.require_device_token` | `true` |
| `providers.fake.enabled` | `true` |
| `providers.grok.enabled` | `true` |
| `providers.grok.bin` | `grok` |
| `providers.grok.always_approve` | `false` |
| `providers.grok.default_cwd` | _(empty)_ |
| `providers.grok.model` | _(empty)_ |
| `headscale.control_url` | `http://localhost:8080` |

> **Note:** `mcremote setup-service` defaults `--listen-host` to **`0.0.0.0`** so phones on the mesh can connect. Interactive `serve` without flags still defaults to localhost via config.

## Environment variables

All use the `MCREMOTE_` prefix. Nested YAML keys use underscores.

| Variable | Maps to | Description |
|----------|---------|-------------|
| `MCREMOTE_CONFIG` | config file path | Explicit config YAML path |
| `MCREMOTE_LISTEN_HOST` | `listen.host` | Bind address |
| `MCREMOTE_LISTEN_PORT` | `listen.port` | Bind port (1–65535) |
| `MCREMOTE_LOG_LEVEL` | `log.level` | `debug` \| `info` \| `warn` \| `error` |
| `MCREMOTE_LOG_FORMAT` | `log.format` | `text` \| `json` |
| `MCREMOTE_DATA_DIR` | `data_dir` | Devices, pair codes, session meta |
| `MCREMOTE_AUTH_REQUIRE_DEVICE_TOKEN` | `auth.require_device_token` | Require device token on WebSocket |
| `MCREMOTE_PAIR_HOST` | _(CLI pair only)_ | Host advertised in pair QR/code (e.g. `100.64.0.1:7531`) |

Viper also accepts automatic env for other keys using `MCREMOTE_` + uppercased path with `_` (e.g. `MCREMOTE_PROVIDERS_GROK_BIN`). Prefer the explicit table above for production.

### Examples

```bash
export MCREMOTE_LISTEN_HOST=0.0.0.0
export MCREMOTE_LISTEN_PORT=7531
export MCREMOTE_LOG_LEVEL=debug
export MCREMOTE_LOG_FORMAT=json
export MCREMOTE_DATA_DIR=/var/lib/mcremote
export MCREMOTE_CONFIG=/etc/mcremote/config.yaml
export MCREMOTE_PAIR_HOST=100.64.0.1:7531
```

## CLI flags

Long options always use **two dashes** (`--flag`). Help is `--help` or `-h`. Version is `--version` or `mcremote version`.

### Global (all commands)

| Flag | Env / config | Description |
|------|----------------|-------------|
| `--config` | `MCREMOTE_CONFIG` | Config file path |
| `--log-level` | `MCREMOTE_LOG_LEVEL` | Log level |
| `--log-format` | `MCREMOTE_LOG_FORMAT` | Log format |
| `--help` / `-h` | | Help |
| `--version` | | Version (root) |

### `mcremote serve`

| Flag | Description |
|------|-------------|
| `--listen-host` | Override `listen.host` |
| `--listen-port` | Override `listen.port` |
| `--data-dir` | Override `data_dir` |

### `mcremote pair` / `pair code` / `pair create`

| Flag | Description |
|------|-------------|
| `--name` | Device label (default `device`) |
| `--qr` | Print terminal QR (default on TTY) |
| `--host` | Advertise host:port in QR/URI |
| `--ttl` | Pair **code** lifetime (default `5m`) |
| `--data-dir` | Data directory for devices / pair codes |

### `mcremote setup-service` / `mcremote --setup-service`

| Flag | Default | Description |
|------|---------|-------------|
| `--setup-service` | false | Root flag alias for this command |
| `--unit-name` | `mcremote` | Unit name without `.service` |
| `--binary` | this executable | Source binary to install |
| `--install-binary` | `true` | Copy binary to `--install-path` |
| `--install-path` | `~/.local/bin/mcremote` | Stable ExecStart path |
| `--service-config` | | Config path embedded in unit (else `--config`) |
| `--data-dir` | | Passed to `serve` |
| `--listen-host` | `0.0.0.0` | Passed to `serve` |
| `--listen-port` | `7531` | Passed to `serve` |
| `--working-directory` | `$HOME` | systemd `WorkingDirectory` |
| `--env` | | Extra `KEY=VALUE` (repeatable) |
| `--print-only` | false | Print unit to stdout only |
| `--force` | false | Overwrite existing unit |
| `--no-enable` | false | Skip `systemctl --user enable` |
| `--no-start` | false | Skip start/restart |
| `--no-linger` | false | Skip `loginctl enable-linger` |

## Examples

- Dev: [configs/config.example.yaml](../configs/config.example.yaml)
- Prod-oriented: [configs/config.prod.example.yaml](../configs/config.prod.example.yaml)
- Mesh + Grok: [configs/config.mesh-grok.yaml](../configs/config.mesh-grok.yaml)
