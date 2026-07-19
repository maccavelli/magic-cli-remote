# mcremote configuration

## Locations (XDG)

| Item | Path |
|------|------|
| Config dir | `$XDG_CONFIG_HOME/mcremote` or `~/.config/mcremote` |
| Config file | `config.yaml` inside config dir |
| Data dir | `$XDG_DATA_HOME/mcremote` or `~/.local/share/mcremote` |
| Devices | `<data_dir>/devices.json` (mode `0600`) |

Override config path: `--config /path/to.yaml` or `MCREMOTE_CONFIG`.

## Precedence

1. CLI flags  
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
| `providers.grok.enabled` | `false` |
| `headscale.control_url` | `http://localhost:8080` |

## Environment examples

```bash
export MCREMOTE_LISTEN_HOST=0.0.0.0
export MCREMOTE_LISTEN_PORT=7531
export MCREMOTE_LOG_LEVEL=debug
export MCREMOTE_LOG_FORMAT=json
export MCREMOTE_DATA_DIR=/var/lib/mcremote
```

## Examples

- Dev: [configs/config.example.yaml](../configs/config.example.yaml)
- Prod-oriented: [configs/config.prod.example.yaml](../configs/config.prod.example.yaml)
