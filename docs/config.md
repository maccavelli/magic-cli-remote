# mcremote configuration

## Locations (XDG)

| Item | Path |
|------|------|
| Config dir | `$XDG_CONFIG_HOME/mcremote` or `~/.config/mcremote` |
| Config file | `config.yaml` inside config dir |
| Data dir | `$XDG_DATA_HOME/mcremote` or `~/.local/share/mcremote` |
| Devices | `<data_dir>/devices.json` (mode `0600`) |
| Pair codes | `<data_dir>/pair_codes.json` (mode `0600`) |
| TLS certificate (selfsigned) | `<data_dir>/tls.crt` (mode `0600`) |
| TLS private key (selfsigned) | `<data_dir>/tls.key` (mode `0600`) |
| ACME storage (letsencrypt) | `<data_dir>/acme/` (certmagic: account key + issued certs) |
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
| `listen.host` | `127.0.0.1` (the `Defaults()` value; every shipped launch path sets `tailscale`) |
| `listen.port` | `7531` |
| `tls.mode` | _(empty — `letsencrypt` when `tls.letsencrypt.domains` + `email` are set, else `selfsigned`)_ |
| `tls.enabled` | `true` (legacy switch; `false` == `tls.mode: off`) |
| `tls.cert_file` / `tls.key_file` | _(empty — generate and manage automatically; `selfsigned` only)_ |
| `tls.letsencrypt.domains` | _(empty)_ |
| `tls.letsencrypt.email` | _(empty)_ |
| `tls.letsencrypt.directory_url` | _(empty — Let's Encrypt production)_ |
| `tls.letsencrypt.staging` | `false` |
| `tls.letsencrypt.cache_dir` | _(empty — `<data_dir>/acme`)_ |
| `tls.letsencrypt.route53.hosted_zone_id` | _(empty — discovered from the zone name)_ |
| `tls.letsencrypt.route53.region` | _(empty — `AWS_REGION`)_ |
| `tls.letsencrypt.route53.profile` | _(empty — `AWS_PROFILE`)_ |
| `log.level` | `info` |
| `log.format` | `text` |
| `auth.require_device_token` | `true` |
| `providers.fake.enabled` | `true` |
| `providers.grok.enabled` | `true` |
| `providers.grok.bin` | `grok` |
| `providers.grok.always_approve` | `false` |
| `providers.grok.default_cwd` | _(empty)_ |
| `providers.grok.model` | _(empty)_ |
| `providers.opencode.enabled` | `true` — pick OpenCode per session from the phone's new-session provider menu; harmless when the binary is absent (listed as not ready) |
| `providers.opencode.bin` | `opencode` |
| `providers.opencode.always_approve` | `false` |
| `providers.opencode.default_cwd` | _(empty)_ |
| `providers.opencode.model` | _(empty — OpenCode's own default; use a `provider/model` id like `anthropic/claude-sonnet-4-5`)_ |
| `headscale.control_url` | `http://localhost:8080` |

### `listen.host: tailscale`

`listen.host` accepts the sentinel **`tailscale`**. At startup the daemon
replaces it with this host's Tailscale IPv4 (`tailscale ip -4`), so the listener
binds the mesh interface only and nothing else on the machine's networks can
reach 7531.

It **fails closed**: if no Tailscale IPv4 can be found, `serve` exits with an
error naming the fix. It never falls back to `0.0.0.0`.

`0.0.0.0` remains available as an explicit opt-in for serving clients that are
not on the tailnet; the daemon logs a warning when it is used. It is no longer
the default anywhere.

> **Note:** `mcremote setup-service`, both systemd units, `scripts/start-mcremote-grok.sh`, and the shipped `configs/*.yaml` all default `--listen-host` / `listen.host` to **`tailscale`**. Interactive `serve` without flags or a config file still defaults to localhost.

## Environment variables

All use the `MCREMOTE_` prefix. Nested YAML keys use underscores.

| Variable | Maps to | Description |
|----------|---------|-------------|
| `MCREMOTE_CONFIG` | config file path | Explicit config YAML path |
| `MCREMOTE_LISTEN_HOST` | `listen.host` | Bind address; `tailscale` binds the tailnet IPv4 only |
| `MCREMOTE_LISTEN_PORT` | `listen.port` | Bind port (1–65535) |
| `MCREMOTE_LOG_LEVEL` | `log.level` | `debug` \| `info` \| `warn` \| `error` |
| `MCREMOTE_LOG_FORMAT` | `log.format` | `text` \| `json` |
| `MCREMOTE_DATA_DIR` | `data_dir` | Devices, pair codes, session meta |
| `MCREMOTE_AUTH_REQUIRE_DEVICE_TOKEN` | `auth.require_device_token` | Require device token on WebSocket |
| `MCREMOTE_TLS_ENABLED` | `tls.enabled` | Serve HTTPS/WSS (`true`/`false`) |
| `MCREMOTE_TLS_CERT_FILE` | `tls.cert_file` | Operator-managed certificate (with key file) |
| `MCREMOTE_TLS_KEY_FILE` | `tls.key_file` | Operator-managed private key (with cert file) |
| `MCREMOTE_TLS_MODE` | `tls.mode` | `letsencrypt` \| `selfsigned` \| `off` |
| `MCREMOTE_TLS_DOMAINS` | `tls.letsencrypt.domains` | Comma-separated DNS names to request |
| `MCREMOTE_TLS_EMAIL` | `tls.letsencrypt.email` | ACME account contact |
| `MCREMOTE_TLS_ACME_DIRECTORY_URL` | `tls.letsencrypt.directory_url` | ACME directory URL |
| `MCREMOTE_TLS_ACME_STAGING` | `tls.letsencrypt.staging` | Use the Let's Encrypt staging CA |
| `MCREMOTE_TLS_ACME_CACHE_DIR` | `tls.letsencrypt.cache_dir` | ACME storage dir |
| `MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID` | `tls.letsencrypt.route53.hosted_zone_id` | Route 53 hosted zone ID |
| `MCREMOTE_TLS_ROUTE53_REGION` | `tls.letsencrypt.route53.region` | AWS region |
| `MCREMOTE_TLS_ROUTE53_PROFILE` | `tls.letsencrypt.route53.profile` | AWS shared-config profile |
| `MCREMOTE_PAIR_HOST` | _(CLI pair only)_ | Host advertised in pair QR/code. Ignored in `letsencrypt` mode, where the primary ACME domain is used |

AWS credentials for the DNS-01 solver are **not** mcremote settings: the
`route53` provider reads the standard chain (`AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`, `AWS_PROFILE`, or an instance
role). See [tls-letsencrypt.md](tls-letsencrypt.md) for the IAM policy.

Viper also accepts automatic env for other keys using `MCREMOTE_` + uppercased path with `_` (e.g. `MCREMOTE_PROVIDERS_GROK_BIN`). Prefer the explicit table above for production.

### Examples

```bash
export MCREMOTE_LISTEN_HOST=tailscale   # or an explicit address; 0.0.0.0 opts out of tailnet-only
export MCREMOTE_LISTEN_PORT=7531
export MCREMOTE_LOG_LEVEL=debug
export MCREMOTE_LOG_FORMAT=json
export MCREMOTE_DATA_DIR=/var/lib/mcremote
export MCREMOTE_CONFIG=/etc/mcremote/config.yaml
export MCREMOTE_PAIR_HOST=100.64.0.1:7531   # selfsigned mode only

# Let's Encrypt (DNS-01 via Route 53)
export MCREMOTE_TLS_DOMAINS=devbox.ts.lallygag.net
export MCREMOTE_TLS_EMAIL=ops@lallygag.net
export MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID=Z0123456789ABCDEFGHIJ
export MCREMOTE_TLS_ROUTE53_REGION=us-east-1
export MCREMOTE_TLS_ACME_STAGING=true      # drop once staging succeeds
```

## TLS modes

| `tls.mode` | Certificate | Phone trust | When |
|------------|-------------|-------------|------|
| `letsencrypt` | ACME DNS-01 via Route 53, auto-renewed by certmagic | Platform trust store — **no** `fp=` in the pair QR | Default once a domain + email are configured |
| `selfsigned` | Long-lived leaf in `<data_dir>/tls.{crt,key}` | SHA-256 fingerprint pinned from the pair QR (`fp=`) | Mesh IPs with no public DNS; also the automatic fallback if ACME fails |
| `off` | none | n/a — plaintext `ws://` | Only behind another TLS terminator |

Only DNS-01 is implemented. Daemon nodes are mesh-only and their MagicDNS
names are not in public DNS, so an ACME validator can never reach them for
HTTP-01 or TLS-ALPN-01. Full setup: [tls-letsencrypt.md](tls-letsencrypt.md).

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
| `--tls` | Legacy on/off switch; `--tls=false` == `--tls-mode off` |
| `--tls-mode` | `letsencrypt` \| `selfsigned` \| `off` |
| `--tls-domain` | DNS name to request (repeatable / comma-separated); first is advertised to phones |
| `--tls-email` | ACME account email |
| `--tls-acme-directory` | ACME directory URL |
| `--tls-acme-staging` | Use the Let's Encrypt staging CA |
| `--tls-route53-zone-id` | Route 53 hosted zone ID |
| `--tls-route53-region` | AWS region |
| `--tls-route53-profile` | AWS shared-config profile |

### `mcremote pair` / `pair code` / `pair create`

| Flag | Description |
|------|-------------|
| `--name` | Device label (default `device`) |
| `--qr` | Print terminal QR (default on TTY) |
| `--host` | Advertise host:port in QR/URI. Default: the primary ACME domain in `letsencrypt` mode, the Tailscale IPv4 in `selfsigned` mode |
| `--ttl` | Pair **code** lifetime (default `5m`) |
| `--data-dir` | Data directory for devices / pair codes |

### `mcremote setup-service` / `mcremote --setup-service`

| Flag | Default | Description |
|------|---------|-------------|
| `--setup-service` | false | Root flag alias for this command |
| `--unit-name` | `mcremote` | Unit name without `.service` |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | `ExecStart` path only (never copies the binary; use `make install`) |
| `--service-config` | | Config path embedded in unit (else `--config`) |
| `--data-dir` | | Passed to `serve` |
| `--listen-host` | `tailscale` | Passed to `serve`; binds the tailnet IPv4 only |
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
