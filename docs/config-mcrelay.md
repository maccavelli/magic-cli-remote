# mcrelay configuration

Aligned with [config.md](config.md) (mcremote): same precedence, XDG layout,
double-dash flags, and systemd user-unit install pattern. Product-specific
prefix is **`MCRELAY_`** and app dir **`mcrelay`**.

| Artifact | Path |
|----------|------|
| Example config | [configs/mcrelay.example.yaml](../configs/mcrelay.example.yaml) |
| setup-service default | [internal/cli/service/defaults_mcrelay.yaml](../internal/cli/service/defaults_mcrelay.yaml) |
| User unit example | [deploy/systemd/mcrelay.user.service](../deploy/systemd/mcrelay.user.service) |
| Ops runbook | [ops-mcrelay.md](ops-mcrelay.md) |

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

## Defaults (all config keys)

| Key | Default | Env | Flag |
|-----|---------|-----|------|
| `listen.host` | `0.0.0.0` | `MCRELAY_LISTEN_HOST` | `--listen-host` |
| `listen.port` | `8443` | `MCRELAY_LISTEN_PORT` | `--listen-port` |
| `tls.mode` | _(empty — auto)_ | `MCRELAY_TLS_MODE` | `--tls-mode` |
| `tls.cert_file` | _(empty)_ | `MCRELAY_TLS_CERT_FILE` | `--tls-cert` |
| `tls.key_file` | _(empty)_ | `MCRELAY_TLS_KEY_FILE` | `--tls-key` |
| `tls.letsencrypt.domains` | `[]` | `MCRELAY_TLS_DOMAINS` | `--tls-domain` (repeatable) |
| `tls.letsencrypt.email` | _(empty)_ | `MCRELAY_TLS_EMAIL` | `--tls-email` |
| `tls.letsencrypt.directory_url` | _(empty)_ | `MCRELAY_TLS_ACME_DIRECTORY_URL` | `--tls-acme-directory` |
| `tls.letsencrypt.staging` | `false` | `MCRELAY_TLS_ACME_STAGING` | `--tls-acme-staging` |
| `tls.letsencrypt.cache_dir` | _(empty → `<data_dir>/acme`)_ | `MCRELAY_TLS_ACME_CACHE_DIR` | _(yaml/env only)_ |
| `tls.letsencrypt.http_port` | `0` (= **80**) | `MCRELAY_TLS_ACME_HTTP_PORT` | `--tls-acme-http-port` |
| `log.level` | `info` | `MCRELAY_LOG_LEVEL` | `--log-level` |
| `log.format` | `text` | `MCRELAY_LOG_FORMAT` | `--log-format` |
| `data_dir` | _(empty — XDG mcrelay data home)_ | `MCRELAY_DATA_DIR` | `--data-dir` |
| `hosts` | _(empty — required)_ | `MCRELAY_HOSTS` | `--allow` (merge) |
| `allow_legacy_tunnel_secret` | `false` | `MCRELAY_ALLOW_LEGACY_TUNNEL_SECRET` | `--allow-legacy-tunnel-secret` |
| `trusted_proxies` | `[]` | `MCRELAY_TRUSTED_PROXIES` | `--trusted-proxy` (repeatable) |
| `limits.max_hosts` | `32` | `MCRELAY_LIMITS_MAX_HOSTS` | _(yaml/env only)_ |
| `limits.max_phones_per_host` | `8` | `MCRELAY_LIMITS_MAX_PHONES_PER_HOST` | _(yaml/env only)_ |
| `limits.max_message_bytes` | `1048576` | `MCRELAY_LIMITS_MAX_MESSAGE_BYTES` | _(yaml/env only)_ |
| `limits.max_concurrent_join` | `64` | `MCRELAY_LIMITS_MAX_CONCURRENT_JOIN` | _(yaml/env only)_ |
| `limits.accept_per_minute` | `120` | `MCRELAY_LIMITS_ACCEPT_PER_MINUTE` | _(yaml/env only)_ |
| `limits.join_per_minute` | `30` | `MCRELAY_LIMITS_JOIN_PER_MINUTE` | _(yaml/env only)_ |
| `limits.register_per_minute` | `20` | `MCRELAY_LIMITS_REGISTER_PER_MINUTE` | _(yaml/env only)_ |
| `limits.join_per_host_per_minute` | `60` | `MCRELAY_LIMITS_JOIN_PER_HOST_PER_MINUTE` | _(yaml/env only)_ |
| `limits.tunnel_wait_seconds` | `15` | `MCRELAY_LIMITS_TUNNEL_WAIT_SECONDS` | _(yaml/env only)_ |
| `limits.register_idle_seconds` | `30` | `MCRELAY_LIMITS_REGISTER_IDLE_SECONDS` | _(yaml/env only)_ |
| `limits.splice_idle_seconds` | `300` (`-1` disables) | `MCRELAY_LIMITS_SPLICE_IDLE_SECONDS` | _(yaml/env only)_ |
| `limits.splice_max_seconds` | `43200` (`-1` disables) | `MCRELAY_LIMITS_SPLICE_MAX_SECONDS` | _(yaml/env only)_ |
| _(path only)_ | — | `MCRELAY_CONFIG` | `--config` |

`tls.mode` empty auto-selects: domains+email → `letsencrypt`; cert files → `files`; else `off`.

### Trusted proxies (MADR 0017 E1)

By default mcrelay **ignores** `X-Forwarded-For` / `X-Real-IP` so clients cannot
spoof rate-limit identity. If the public edge sits behind a reverse proxy (or
load balancer) that terminates TLS and sets those headers, list the proxy’s
source addresses:

```yaml
trusted_proxies:
  - 127.0.0.1/32
  - 10.0.0.0/8
```

Bare IPs are accepted (`127.0.0.1` → `/32`). Only when `RemoteAddr` falls in a
listed network does mcrelay take the **rightmost non-trusted** hop from
`X-Forwarded-For` (then `X-Real-IP`). Never list the public internet as trusted.

### Limit ceilings (MADR 0017 D9)

Config load **rejects** values above:

| Key | Maximum |
|-----|---------|
| `limits.max_hosts` | 1024 |
| `limits.max_phones_per_host` | 256 |
| `limits.max_message_bytes` | 16777216 (16 MiB) |
| `limits.max_concurrent_join` | 4096 |
| `limits.*_per_minute` | 100000 |
| duration-second fields | 604800 (7 days) |

`ResolvedLimits` also clamps the same ceilings for programmatic `Config` construction.

## Environment variables (complete)

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
| `MCRELAY_ALLOW_LEGACY_TUNNEL_SECRET` | `allow_legacy_tunnel_secret` | Allow registration secret on `/v1/tunnel` (default false) |
| `MCRELAY_TRUSTED_PROXIES` | `trusted_proxies` | Comma-separated CIDRs/IPs of reverse proxies |
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

These are also listed as commented `Environment=` lines in
[deploy/systemd/mcrelay.user.service](../deploy/systemd/mcrelay.user.service).

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
export MCRELAY_ALLOW_LEGACY_TUNNEL_SECRET=false
export MCRELAY_TRUSTED_PROXIES='127.0.0.1/32'
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

### Global / root

| Flag | Env / config | Description |
|------|----------------|-------------|
| `--config` | `MCRELAY_CONFIG` | Config file path |
| `--log-level` | `MCRELAY_LOG_LEVEL` / `log.level` | Log level |
| `--log-format` | `MCRELAY_LOG_FORMAT` / `log.format` | Log format |
| `--help` / `-h` | | Help |
| `--version` | | Version (root) |
| `--setup-service` | | Same as `setup-service` subcommand |

### `mcrelay serve`

| Flag | Config key | Env |
|------|------------|-----|
| `--listen-host` | `listen.host` | `MCRELAY_LISTEN_HOST` |
| `--listen-port` | `listen.port` | `MCRELAY_LISTEN_PORT` |
| `--data-dir` | `data_dir` | `MCRELAY_DATA_DIR` |
| `--tls-mode` | `tls.mode` | `MCRELAY_TLS_MODE` |
| `--tls-cert` | `tls.cert_file` | `MCRELAY_TLS_CERT_FILE` |
| `--tls-key` | `tls.key_file` | `MCRELAY_TLS_KEY_FILE` |
| `--tls-domain` | `tls.letsencrypt.domains` | `MCRELAY_TLS_DOMAINS` |
| `--tls-email` | `tls.letsencrypt.email` | `MCRELAY_TLS_EMAIL` |
| `--tls-acme-directory` | `tls.letsencrypt.directory_url` | `MCRELAY_TLS_ACME_DIRECTORY_URL` |
| `--tls-acme-staging` | `tls.letsencrypt.staging` | `MCRELAY_TLS_ACME_STAGING` |
| `--tls-acme-http-port` | `tls.letsencrypt.http_port` | `MCRELAY_TLS_ACME_HTTP_PORT` |
| `--allow` | merges into `hosts` | (also `MCRELAY_HOSTS`) |
| `--allow-legacy-tunnel-secret` | `allow_legacy_tunnel_secret` | `MCRELAY_ALLOW_LEGACY_TUNNEL_SECRET` |
| `--trusted-proxy` | `trusted_proxies` | `MCRELAY_TRUSTED_PROXIES` |

Limits (`limits.*`) are **yaml / env only** (no CLI flags) — set in config or
`MCRELAY_LIMITS_*`. ACME cache dir is yaml/env only (`MCRELAY_TLS_ACME_CACHE_DIR`).

### `mcrelay setup-service`

| Flag | Description |
|------|-------------|
| `--unit-name` | Unit name without `.service` (default `mcrelay`) |
| `--binary` | `ExecStart` path (default `~/.local/bin/mcrelay` if present) |
| `--service-config` | Config path baked into the unit (else `--config`) |
| `--data-dir` | Baked `--data-dir` |
| `--listen-host` / `--listen-port` | Optional baked listen overrides |
| `--working-directory` | Unit `WorkingDirectory` (default `$HOME`) |
| `--env KEY=VALUE` | Extra `Environment=` (unit mode `0600` if any); use for `MCRELAY_HOSTS=…` |
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
# secrets via unit env:
# mcrelay setup-service --force --env 'MCRELAY_HOSTS=devbox-1:secret…'
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

## Example / default files

| File | Role |
|------|------|
| [configs/mcrelay.example.yaml](../configs/mcrelay.example.yaml) | Annotated production-oriented example |
| [internal/cli/service/defaults_mcrelay.yaml](../internal/cli/service/defaults_mcrelay.yaml) | Written by `setup-service` when config is missing |
