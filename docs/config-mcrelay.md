# mcrelay configuration

Aligned with [config.md](config.md) (mcremote): same precedence, XDG layout,
double-dash flags, and systemd user-unit install pattern. Product-specific
prefix is **`MCRELAY_`** and app dir **`mcrelay`**.

## Locations (XDG)

| Item | Path |
|------|------|
| Config dir | `$XDG_CONFIG_HOME/mcrelay` or `~/.config/mcrelay` |
| Config file | `config.yaml` inside config dir |
| Data dir | `$XDG_DATA_HOME/mcrelay` or `~/.local/share/mcrelay` |
| User unit | `~/.config/systemd/user/mcrelay.service` |

Override config path: `--config /path/to.yaml` or `MCRELAY_CONFIG`.

`mcrelay setup-service` writes a **default** `config.yaml` into the config dir
when missing (0600, never overwrites an existing file) and bakes that path into
the unit’s `ExecStart`. Edit `hosts` / TLS before exposing the public edge.

## Precedence

1. CLI flags (`--listen-host`, `--allow`, …)  
2. Environment (`MCRELAY_*`)  
3. Config file  
4. Built-in defaults  

`--allow host_id:secret` **merges** with file/env hosts (same `id` overwrites).

## Defaults

| Key | Default |
|-----|---------|
| `listen.host` | `0.0.0.0` (public edge; unlike mcremote’s loopback default) |
| `listen.port` | `8443` |
| `tls.mode` | _(empty — `letsencrypt` when domains+email set; else `files` when cert+key set; else `off`)_ |
| `tls.cert_file` / `tls.key_file` | _(empty)_ — PEMs for `files` mode |
| `tls.letsencrypt.domains` | `[]` — public DNS names for HTTP-01 |
| `tls.letsencrypt.email` | _(empty)_ — ACME account contact |
| `tls.letsencrypt.directory_url` | _(empty — production LE)_ |
| `tls.letsencrypt.staging` | `false` |
| `tls.letsencrypt.cache_dir` | _(empty — `<data_dir>/acme`)_ |
| `tls.letsencrypt.http_port` | `0` (= **80**; CA must reach this port) |
| `log.level` | `info` |
| `log.format` | `text` |
| `data_dir` | _(empty — XDG mcrelay data home)_ |
| `hosts` | _(empty — must supply via YAML, `MCRELAY_HOSTS`, or `--allow`)_ |
| `limits.max_hosts` | `32` |
| `limits.max_phones_per_host` | `8` |
| `limits.max_message_bytes` | `1048576` (1 MiB) |
| `limits.max_concurrent_join` | `64` |
| `limits.accept_per_minute` | `120` (pre-auth WS upgrades per IP) |
| `limits.join_per_minute` | `30` (join attempts per IP, R16) |
| `limits.register_per_minute` | `20` (register attempts per IP, R16) |
| `limits.join_per_host_per_minute` | `60` (joins for one host_id, H3/R10) |
| `limits.tunnel_wait_seconds` | `15` |
| `limits.register_idle_seconds` | `30` (host-control ping interval, R5) |
| `limits.splice_idle_seconds` | `300` (end silent splice, R15; `-1` disables) |
| `limits.splice_max_seconds` | `43200` (max splice 12h, R15; `-1` disables) |

## Environment variables

All use the **`MCRELAY_`** prefix. Nested YAML keys use underscores.

| Variable | Maps to | Description |
|----------|---------|-------------|
| `MCRELAY_CONFIG` | config file path | Explicit config YAML path |
| `MCRELAY_LISTEN_HOST` | `listen.host` | Bind address |
| `MCRELAY_LISTEN_PORT` | `listen.port` | Bind port (1–65535) |
| `MCRELAY_LOG_LEVEL` | `log.level` | `debug` \| `info` \| `warn` \| `error` |
| `MCRELAY_LOG_FORMAT` | `log.format` | `text` \| `json` |
| `MCRELAY_DATA_DIR` | `data_dir` | Data directory |
| `MCRELAY_TLS_MODE` | `tls.mode` | `letsencrypt` \| `files` \| `off` |
| `MCRELAY_TLS_CERT_FILE` | `tls.cert_file` | Outer TLS certificate PEM (`files`) |
| `MCRELAY_TLS_KEY_FILE` | `tls.key_file` | Outer TLS private key PEM (`files`) |
| `MCRELAY_TLS_DOMAINS` | `tls.letsencrypt.domains` | Comma-separated DNS names (HTTP-01) |
| `MCRELAY_TLS_EMAIL` | `tls.letsencrypt.email` | ACME account email |
| `MCRELAY_TLS_ACME_DIRECTORY_URL` | `tls.letsencrypt.directory_url` | ACME directory URL |
| `MCRELAY_TLS_ACME_STAGING` | `tls.letsencrypt.staging` | Use LE staging CA |
| `MCRELAY_TLS_ACME_CACHE_DIR` | `tls.letsencrypt.cache_dir` | certmagic storage |
| `MCRELAY_TLS_ACME_HTTP_PORT` | `tls.letsencrypt.http_port` | HTTP-01 port (`0` = 80) |
| `MCRELAY_HOSTS` | host allowlist | Comma-separated `host_id:secret` entries |
| `MCRELAY_LIMITS_MAX_HOSTS` | `limits.max_hosts` | Max simultaneous registered hosts |
| `MCRELAY_LIMITS_MAX_PHONES_PER_HOST` | `limits.max_phones_per_host` | Max concurrent phone splices per host |
| `MCRELAY_LIMITS_MAX_MESSAGE_BYTES` | `limits.max_message_bytes` | Max WebSocket frame size |
| `MCRELAY_LIMITS_MAX_CONCURRENT_JOIN` | `limits.max_concurrent_join` | Pending joins waiting for tunnel |
| `MCRELAY_LIMITS_ACCEPT_PER_MINUTE` | `limits.accept_per_minute` | Pre-auth upgrades per client IP |
| `MCRELAY_LIMITS_JOIN_PER_MINUTE` | `limits.join_per_minute` | Join attempts per client IP (R16) |
| `MCRELAY_LIMITS_REGISTER_PER_MINUTE` | `limits.register_per_minute` | Register attempts per client IP (R16) |
| `MCRELAY_LIMITS_JOIN_PER_HOST_PER_MINUTE` | `limits.join_per_host_per_minute` | Joins per host_id (H3 / R10) |
| `MCRELAY_LIMITS_TUNNEL_WAIT_SECONDS` | `limits.tunnel_wait_seconds` | Host tunnel open deadline |
| `MCRELAY_LIMITS_REGISTER_IDLE_SECONDS` | `limits.register_idle_seconds` | Host control app-level ping interval (R5) |
| `MCRELAY_LIMITS_SPLICE_IDLE_SECONDS` | `limits.splice_idle_seconds` | Silence before ending splice (R15); `-1` disables |
| `MCRELAY_LIMITS_SPLICE_MAX_SECONDS` | `limits.splice_max_seconds` | Max splice lifetime (R15); `-1` disables |

### Examples

```bash
export MCRELAY_CONFIG=~/.config/mcrelay/config.yaml
export MCRELAY_LISTEN_HOST=0.0.0.0
export MCRELAY_LISTEN_PORT=8443
export MCRELAY_LOG_LEVEL=info
export MCRELAY_TLS_CERT_FILE=/etc/ssl/mcrelay/fullchain.pem
export MCRELAY_TLS_KEY_FILE=/etc/ssl/mcrelay/privkey.pem
# Prefer env for secrets over committing them to YAML:
export MCRELAY_HOSTS='devbox-1:your-long-registration-secret'
```

## Host allowlist

Each host mcremote that may register must appear with a **registration secret**
(min 16 characters). The secret never goes on the phone or pair QR (MADR 0015).

YAML:

```yaml
hosts:
  - id: devbox-1
    secret: "long-random-secret-here"
```

CLI:

```bash
mcrelay serve --allow 'devbox-1:long-random-secret-here'
```

Env:

```bash
export MCRELAY_HOSTS='devbox-1:long-random-secret-here,laptop:another-long-secret'
```

## TLS (outer edge)

| `tls.mode` | Behaviour |
|------------|-----------|
| `letsencrypt` | ACME **HTTP-01** via certmagic (public DNS name; CA hits port **80**) |
| `files` | Use `tls.cert_file` + `tls.key_file` |
| `off` | Plaintext HTTP/WS — **warns**; tests / loopback only |
| _(empty)_ | Auto: domains+email → `letsencrypt`; cert files → `files`; else `off` |

### Let's Encrypt HTTP-01

mcrelay is the **public** edge, so HTTP-01 is the natural challenge (unlike
mesh-only mcremote, which uses DNS-01). Requirements:

1. `tls.letsencrypt.domains` resolve to this host’s public IP  
2. Port **80** is free for certmagic’s challenge listener (or set `http_port` only for non-public CAs)  
3. Main join-plane listen is usually **443** (`listen.port: 443`)  
4. Start with `staging: true`, then flip to production  

```bash
mcrelay serve \
  --listen-host 0.0.0.0 --listen-port 443 \
  --tls-mode letsencrypt \
  --tls-domain relay.example.com \
  --tls-email ops@example.com \
  --tls-acme-staging \
  --allow 'devbox-1:your-long-registration-secret'
```

Renewal is handled by certmagic’s maintenance goroutine (same as mcremote’s ACME
path). This is the **relay** certificate only; mcremote’s pin/`mode` in the pair
URI remain the **inner** identity (MADR 0015 S13).

## CLI

Long options always use **two dashes** (`--flag`). Help is `--help` or `-h`.

### Global

| Flag | Env / config | Description |
|------|----------------|-------------|
| `--config` | `MCRELAY_CONFIG` | Config file path |
| `--log-level` | `MCRELAY_LOG_LEVEL` | Log level |
| `--log-format` | `MCRELAY_LOG_FORMAT` | Log format |
| `--help` / `-h` | | Help |
| `--version` | | Version (root) |
| `--setup-service` | | Same as `setup-service` subcommand |

### `mcrelay serve`

| Flag | Config key | Description |
|------|------------|-------------|
| `--listen-host` | `listen.host` | Bind host |
| `--listen-port` | `listen.port` | Bind port |
| `--data-dir` | `data_dir` | Data directory |
| `--tls-mode` | `tls.mode` | `files` \| `off` |
| `--tls-cert` | `tls.cert_file` | Certificate PEM |
| `--tls-key` | `tls.key_file` | Private key PEM |
| `--allow` | merges into `hosts` | `host_id:secret` (repeatable) |

### `mcrelay setup-service`

Same shape as `mcremote setup-service`:

| Flag | Description |
|------|-------------|
| `--unit-name` | Unit name without `.service` (default `mcrelay`) |
| `--binary` | `ExecStart` path (default `~/.local/bin/mcrelay` if present) |
| `--service-config` | Config path baked into the unit (else `--config`) |
| `--data-dir` | Baked `--data-dir` |
| `--listen-host` / `--listen-port` | Optional baked listen overrides |
| `--working-directory` | Unit `WorkingDirectory` (default `$HOME`) |
| `--env KEY=VALUE` | Extra `Environment=` (unit mode `0600` if any) |
| `--print-only` | Print unit to stdout |
| `--force` | Overwrite differing unit |
| `--no-enable` / `--no-start` / `--no-linger` | Skip enable / start / linger |
| `--remove` | Stop, disable, delete unit |

```bash
make build-relay
install -m 755 bin/mcrelay ~/.local/bin/mcrelay
mkdir -p ~/.config/mcrelay
cp configs/mcrelay.example.yaml ~/.config/mcrelay/config.yaml
chmod 600 ~/.config/mcrelay/config.yaml
# edit hosts / TLS paths
mcrelay setup-service --force --service-config ~/.config/mcrelay/config.yaml
systemctl --user status mcrelay
journalctl --user -u mcrelay -f
```

### `mcrelay version`

Prints `mcrelay <version> (<commit>) <date>`.

## Join plane (runtime)

Unchanged from MADR 0015 E1: `GET /v1/host`, `/v1/tunnel`, `/v1/phone`, `/healthz`.
See [0015-mcrelay-transport-security.md](0015-mcrelay-transport-security.md).

## Ops runbook

Production install, systemd, ACME, secret rotation, and Phase E smoke checklist:
[ops-mcrelay.md](ops-mcrelay.md). Unit example: [deploy/systemd/mcrelay.user.service](../deploy/systemd/mcrelay.user.service).

## Example file

See [configs/mcrelay.example.yaml](../configs/mcrelay.example.yaml).
