# magic-cli-remote (`mcremote`)

Provider-agnostic Go daemon that multiplexes coding-agent CLI sessions and exposes secure remote control over **Headscale/Tailscale** for a Flutter client.

**Current status:** foundation + **Grok Build ACP**, **OpenCode**, **Goose**, **Codex**, **Fake** providers, remote tool permissions over WebSocket, session metadata persistence, mcrelay outbound relay,
XDG path resolution with Linux/macOS parity (`mcremote paths`), and engine lifecycle management (`mcremote engines`).

## Requirements

- Go **1.26.x** (developed on 1.26.5)
- Linux (primary) or macOS
- Optional: Headscale + Tailscale clients for remote access ([docs/headscale.md](docs/headscale.md))
- For `setup-service`: Linux **systemd --user**, or macOS **launchd** user LaunchAgent (no sudo)
- Provider binaries on `PATH` as needed: `grok`, `opencode`, `goose`

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
the binary as `BASE.N` where `BASE` is the latest **release**
tag (`v0.2.1` → `0.2.1`) and `N` is a global build serial (`0.2.1.1`, `0.2.1.2`, …).

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

---

## Service installation

`setup-service` installs a background service definition that manages the daemon for you. It does **not** copy the binary — install first with `make install`.

| Platform | Backend | Survives logout? |
|----------|---------|------------------|
| Linux | systemd `--user` unit | Yes (linger enabled by default) |
| macOS | launchd **user LaunchAgent** (`~/Library/LaunchAgents`) | **No** — session-bound; no sudo system daemon |

Design notes: [docs/0058-MADR-macos-launchd-service-hardening.md](docs/0058-MADR-macos-launchd-service-hardening.md).

### Step-by-step

**1. Build and install the binary**

```bash
make build
make install
# Binary lands at ~/.local/bin/mcremote
```

**2. Install the service**

```bash
# Auto-detects the binary, writes the service definition, enables, and starts.
mcremote setup-service

# Same thing, using the root flag form:
mcremote --setup-service
```

**3. Provide a config file**

```bash
# Copy the mesh + Grok example config:
cp configs/config.mesh-grok.yaml ~/.config/mcremote/config.yaml
chmod 600 ~/.config/mcremote/config.yaml
# Edit it: set TLS domains, provider options, etc.
```

Or let `setup-service` write a default config automatically (it creates `~/.config/mcremote/config.yaml` with built-in defaults when the file is missing).

**4. (Recommended) Bake listen settings and config path into the service**

```bash
mcremote setup-service \
  --listen-host tailscale --listen-port 7531 \
  --config ~/.config/mcremote/config.mesh-grok.yaml \
  --force
```

**5. Verify it's running**

Linux:

```bash
systemctl --user status mcremote
journalctl --user -u mcremote -f
```

macOS:

```bash
launchctl print "gui/$(id -u)/com.magiccliremote.mcremote"
tail -f ~/Library/Logs/mcremote/mcremote.err.log
```

**6. Linux linger** (survives logout — done automatically by `setup-service` on Linux)

```bash
loginctl enable-linger $USER
```

On macOS there is no user-level linger. LaunchAgents stop when you log out. For always-on Macs, keep a login session (auto-login or Screen Sharing).
macOS 13+ may show a **Background Items** notification; System Settings → General → Login Items can disable the agent.

### What `setup-service` actually does

| Step | Linux | macOS |
|------|-------|-------|
| **Config** | Ensures `~/.config/mcremote/config.yaml` (0600; never overwrites) | Same |
| **Definition** | `~/.config/systemd/user/mcremote.service` | `~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` |
| **Enable / start** | `systemctl --user enable` + `restart` | `launchctl enable` + `bootstrap gui/$UID` + `kickstart -k` |
| **Linger** | `loginctl enable-linger` | n/a (session-bound) |
| **Binary swap** | `make install` stops/starts the unit around an atomic rename | Same pattern for the LaunchAgent (no sudo) |

### Preview without installing

```bash
mcremote setup-service --print-only
# Linux: systemd unit text
# macOS: launchd plist XML
```

### Customize

```bash
mcremote setup-service \
  --env MCREMOTE_LOG_LEVEL=debug \
  --env MCREMOTE_LOG_FORMAT=json \
  --force

mcremote setup-service --binary /usr/local/bin/mcremote --force

mcremote setup-service --no-enable --no-start --no-linger --print-only
# --no-linger is Linux-only; no-op on macOS
```

### Uninstall

```bash
mcremote setup-service --remove
```

Stops the daemon, disables the service, and deletes the unit/plist. The binary and config directory are left intact.

Manual plist examples: [deploy/launchd/com.magiccliremote.mcremote.plist](deploy/launchd/com.magiccliremote.mcremote.plist), [deploy/launchd/com.magiccliremote.mcrelay.plist](deploy/launchd/com.magiccliremote.mcrelay.plist).

---

## TLS

The daemon serves HTTPS/WSS by default, in one of three modes selected by
`tls.mode`:

| `tls.mode` | Certificate | How the phone trusts it |
|------------|-------------|--------------------------|
| `letsencrypt` | ACME **DNS-01** via Route 53, renewed automatically | Platform trust store — no pin |
| `selfsigned` | Long-lived P-256 leaf in `<data_dir>/tls.{crt,key}` | SHA-256 fingerprint pinned from the pair QR |
| `off` | none (plaintext `ws://`) | n/a |

Leave `tls.mode` empty and it resolves automatically: **`letsencrypt` once a
domain and an ACME email are configured, `selfsigned` otherwise.**

### Let's Encrypt (default when configured)

```bash
mcremote serve \
  --tls-domain devbox.ts.lallygag.net \
  --tls-email ops@lallygag.net \
  --tls-route53-zone-id Z0123456789ABCDEFGHIJ \
  --tls-route53-region us-east-1 \
  --tls-acme-staging          # drop once staging works
```

Only the DNS-01 challenge is supported, because it is the only one that can
work: daemon nodes are mesh-only, so an ACME validator can never reach them
for HTTP-01 or TLS-ALPN-01. DNS-01 needs nothing but a `_acme-challenge` TXT
record in a public Route 53 zone.

Two consequences for pairing: the QR advertises the **DNS name**
(`devbox.ts.lallygag.net:7531`), never the `100.64.0.0/10` mesh IP — Let's
Encrypt does not issue for IPs, so an IP host would fail hostname
verification — and the QR carries **no `fp=`**, because a publicly trusted
cert needs no pin and a pin would break at the first renewal.

If ACME fails for any reason, the daemon logs an error and **falls back to the
self-signed certificate rather than refusing to start**. Certificates last 90
days, so a host left dark for longer returns with an expired cert and the
phone fails closed until it renews. Full setup, the scoped Route 53 IAM
policy, and the staging-first workflow:
**[docs/tls-letsencrypt.md](docs/tls-letsencrypt.md)**.

### Self-signed (fallback, and the right choice for bare mesh IPs)

On first run the daemon writes a self-signed P-256 certificate to
`~/.local/share/mcremote/{tls.crt,tls.key}` (mode `0600`) covering the machine
hostname, `localhost`, and its LAN/mesh IPs, then reuses it on every later run.
There is no CA involved: the phone trusts the daemon by pinning the
certificate's SHA-256 fingerprint, which `mcremote pair` prints and embeds in
the QR as `fp=…`.

```bash
mcremote serve --tls-mode selfsigned
mcremote pair code --name phone --qr   # prints Cert: sha256:… and encodes it in the QR

# Verify what the daemon is actually serving:
echo | openssl s_client -connect 127.0.0.1:7531 2>/dev/null \
  | openssl x509 -noout -fingerprint -sha256
```

If the fingerprint the app reports on a mismatch does not match that output,
stop — something is intercepting the connection.

### Plaintext / external terminator

To terminate TLS elsewhere (or for a loopback-only dev box), opt out
explicitly:

```bash
mcremote serve --tls-mode off       # or --tls=false, tls.enabled: false, MCREMOTE_TLS_MODE=off
```

The pair QR then advertises `ws://` and carries no fingerprint. To serve a
certificate you manage yourself, set `tls.cert_file` and `tls.key_file`
(self-signed mode); the daemon uses them as-is, never regenerates them, and
still advertises their fingerprint for pinning.

---

## CLI reference

```text
mcremote [--config PATH] [--log-level LEVEL] [--log-format FORMAT] [--version] [--help]
mcremote serve [--listen-host HOST] [--listen-port PORT] [--data-dir DIR]
               [--tls|--tls=false] [--tls-mode letsencrypt|selfsigned|off]
               [--tls-domain NAME] [--tls-email ADDR] [--tls-acme-directory URL]
               [--tls-acme-staging] [--tls-route53-zone-id ID]
               [--tls-route53-region REGION] [--tls-route53-profile PROFILE]
               [--relay-url URL] [--relay-host-id ID] [--relay-secret SECRET]
mcremote pair [code] | pair create | pair list | pair revoke | pair prune
mcremote setup-service | mcremote --setup-service
mcremote engines [--reap]
mcremote paths [--json] [--data-dir DIR]
mcremote version
```

| Command | Purpose |
|---------|---------|
| `serve` | Run the daemon in the foreground |
| `pair` / `pair code` | 8-char short code (5 min, one-shot) — preferred phone pairing |
| `pair create` | Long-lived `mcr_…` device token |
| `pair list` / `pair revoke` | Manage devices |
| `pair prune` | Remove stale (`--stale`) or keyless (`--keyless`) devices |
| `setup-service` / `--setup-service` | Install background service + start (Linux systemd --user / macOS launchd agent; `--remove` to uninstall) |
| `engines` | List agent engine processes (`goose`/`opencode` `serve`, `codex app-server`) and whether their owning daemon is alive (`--reap` to stop orphans) |
| `paths` | Print the resolved XDG layout — config, data, state, cache, runtime, admin socket, engine registry, log dir (`--json` for machine-readable). Read-only: creates nothing |
| `version` / `--version` | Print version |

### Global flags

| Flag | Description |
|------|-------------|
| `--config` | Config YAML (`MCREMOTE_CONFIG`) |
| `--log-level` | `debug` \| `info` \| `warn` \| `error` |
| `--log-format` | `text` \| `json` |
| `--help` / `-h` | Help |
| `--version` | Version |
| `--setup-service` | Install + enable + start background service (Linux systemd / macOS launchd) |

### `serve` flags

| Flag | Description |
|------|-------------|
| `--listen-host` | Override `listen.host` (config default `127.0.0.1`). `tailscale` resolves to this host's Tailscale IPv4 at startup and refuses to start without one |
| `--listen-port` | Override `listen.port` (config default `7531`) |
| `--data-dir` | State directory (devices, pair codes, sessions, TLS cert) |
| `--tls` | Legacy on/off switch; `--tls=false` == `--tls-mode off` |
| `--tls-mode` | `letsencrypt` \| `selfsigned` \| `off` (default: auto) |
| `--tls-domain` | DNS name to request from the ACME CA (repeatable); first is advertised to phones |
| `--tls-email` | ACME account email (required for `letsencrypt`) |
| `--tls-acme-directory` | ACME directory URL (default: Let's Encrypt production) |
| `--tls-acme-staging` | Use the Let's Encrypt staging CA |
| `--tls-route53-zone-id` | Route 53 hosted zone ID for DNS-01 |
| `--tls-route53-region` / `--tls-route53-profile` | AWS region / shared-config profile |
| `--relay-url` | mcrelay base URL (`wss://…`); env `MCREMOTE_RELAY_URL` |
| `--relay-host-id` | Public host id for mcrelay registration; env `MCREMOTE_RELAY_HOST_ID` |
| `--relay-secret` | mcrelay registration secret (min 16); env `MCREMOTE_RELAY_SECRET` |

### `engines` flags

| Flag | Description |
|------|-------------|
| `--reap` | Stop every engine whose owning daemon is gone |

### `paths` flags

| Flag | Description |
|------|-------------|
| `--json` | Emit the layout as JSON instead of aligned text |
| `--data-dir` | Resolve as if `serve` were given this data directory |

### `pair` flags (by subcommand)

| Subcommand | Flags |
|------------|--------|
| `pair` / `pair code` | `--name`, `--qr`, `--host`, `--ttl` (default `5m`), `--data-dir` |
| `pair create` | `--name`, `--qr`, `--host`, `--data-dir` |
| `pair list` / `pair revoke <id\|name>` | `--data-dir` |
| `pair prune` | `--keyless`, `--stale <duration>`, `--data-dir` (at least one of keyless/stale) |

### `setup-service` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--unit-name` | `mcremote` | Linux unit name; macOS maps to `com.magiccliremote.<name>` Label |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | Path used for serve (not copied) |
| `--service-config` | | Config path embedded in service (else `--config`) |
| `--data-dir` | | Passed through to `serve` |
| `--listen-host` | *(empty — follow config)* | Only baked into the service when set; `tailscale` binds the tailnet IPv4 only |
| `--listen-port` | `0` (follow config) | Only baked into the service when non-zero |
| `--working-directory` | `$HOME` | Working directory |
| `--env KEY=VAL` | | Extra environment (repeatable) |
| `--print-only` | | Print unit/plist; do not install |
| `--force` | | Overwrite existing definition |
| `--no-enable` / `--no-start` / `--no-linger` | | Skip enable / start / linger (`--no-linger` is no-op on macOS) |
| `--remove` | | Stop, disable, and delete the service definition |

```bash
# Listen from config.yaml (recommended):
mcremote setup-service --config ~/.config/mcremote/config.yaml --force
# Or bake listen settings into the unit:
mcremote setup-service --listen-host tailscale --listen-port 7531 --force
mcremote setup-service --env 'MCREMOTE_LOG_LEVEL=debug' --force
mcremote setup-service --binary ~/.local/bin/mcremote --force
```

---

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
| `MCREMOTE_AUTH_REQUIRE_CLIENT_KEY` | Require enrolled TLS client key (`true`/`false`) |
| `MCREMOTE_AUTH_ALLOWED_ORIGINS` | Browser Origin allowlist (comma-separated) |
| `MCREMOTE_TLS_ENABLED` | Serve TLS (`true`/`false`, default `true`); `false` == mode `off` |
| `MCREMOTE_TLS_MODE` | `letsencrypt` \| `selfsigned` \| `off` |
| `MCREMOTE_TLS_DOMAINS` | Comma-separated DNS names to request from the ACME CA |
| `MCREMOTE_TLS_EMAIL` | ACME account email |
| `MCREMOTE_TLS_ACME_DIRECTORY_URL` / `MCREMOTE_TLS_ACME_STAGING` / `MCREMOTE_TLS_ACME_CACHE_DIR` | ACME endpoint / cache |
| `MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID` / `_REGION` / `_PROFILE` / `_MAX_RETRIES` | Route 53 DNS-01 solver |
| `MCREMOTE_TLS_CERT_FILE` / `MCREMOTE_TLS_KEY_FILE` | Use an operator-managed certificate instead of the generated one |
| `MCREMOTE_PAIR_ADVERTISE_HOST` / `MCREMOTE_PAIR_HOST` | Host shown in pair QR/code (e.g. tailnet IP). Ignored in `letsencrypt` mode |
| `MCREMOTE_RELAY_URL` / `MCREMOTE_RELAY_HOST_ID` / `MCREMOTE_RELAY_SECRET` / `MCREMOTE_RELAY_INSECURE_SKIP_VERIFY` | mcrelay outbound relay config |

Viper also maps other keys as `MCREMOTE_` + uppercased path with `_` (e.g. `MCREMOTE_PROVIDERS_GROK_PREWARM`). Full table: [docs/config.md](docs/config.md).

```bash
export MCREMOTE_LISTEN_HOST=tailscale       # tailnet IPv4 only; 0.0.0.0 is an explicit opt-in
export MCREMOTE_LISTEN_PORT=7531
export MCREMOTE_LOG_LEVEL=info
export MCREMOTE_PAIR_HOST=100.64.0.1:7531   # selfsigned mode only

# Let's Encrypt over Route 53 DNS-01
export MCREMOTE_TLS_DOMAINS=devbox.ts.lallygag.net
export MCREMOTE_TLS_EMAIL=ops@lallygag.net
export MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID=Z0123456789ABCDEFGHIJ
```

---

## Configuration

Default config path: `~/.config/mcremote/config.yaml` (XDG on Linux **and** macOS; see `mcremote paths`).  

### Resolved path layout

The daemon resolves every directory through the XDG variables on **both** Linux and macOS — macOS does not get Apple's `~/Library/Application Support` layout, deliberately,
so a single set of paths documents both platforms ([MADR 0059](docs/0059-MADR-native-paths-and-linux-macos-parity.md)).
`mcremote paths` prints exactly what the daemon will use, creating nothing:

```bash
mcremote paths            # aligned text
mcremote paths --json     # machine-readable
```

| Entry | Default | Holds |
|-------|---------|-------|
| `config_dir` | `$XDG_CONFIG_HOME/mcremote` → `~/.config/mcremote` | `config.yaml` |
| `data_dir` | `$XDG_DATA_HOME/mcremote` → `~/.local/share/mcremote` | `devices.json`, pair codes, sessions, `tls.{crt,key}` |
| `state_dir` | `$XDG_STATE_HOME/mcremote` → `~/.local/state/mcremote` | Per-instance state, engine registry |
| `cache_dir` | `$XDG_CACHE_HOME/mcremote` → `~/.cache/mcremote` | Regenerable caches |
| `runtime_dir` | `$XDG_RUNTIME_DIR/mcremote`; else `/run/user/$UID` on Linux; else a per-uid temp leaf | Admin control socket |
| `admin_socket` | `<runtime_dir>/admin.sock` | Local control socket (unix domain, never network-exposed) |
| `engine_registry` | `<state_dir>/instances/<instance_key>/engines` | Engine ownership records read by `mcremote engines` |
| `instance_key` | Derived from the resolved data dir | Keeps two daemons with different `--data-dir` from colliding |
| `log_dir` | macOS: `~/Library/Logs/mcremote` | LaunchAgent stdout/stderr. On Linux the unit logs to journald instead |

Relative `XDG_*` values are ignored with a diagnostic, per the XDG absolute-path rule.
`--data-dir` (or `MCREMOTE_DATA_DIR`) re-bases the data directory and the derived instance key; pass it to `paths` to preview the result.

Default listen (built-in): **`127.0.0.1:7531`** (mesh examples use `tailscale`).  
Precedence: **CLI flags > environment > config file > defaults**.

### Provider matrix

| Provider | Default | Transport | Notes |
|----------|---------|-----------|-------|
| `fake` | `enabled: false` | stdio | Dev/smoke only |
| `grok` | `enabled: true` | stdio (`grok agent --no-leader stdio`) | ACP; remote permissions via WebSocket |
| `goose` | `enabled: true` | ACP over WebSocket (HTTP transport) | Block/Goose; drives through one shared `goose serve` engine |
| `opencode` | `enabled: true` | HTTP + SSE | One shared `opencode serve` engine; multi-agent session tree (MADR 0020 KD11) |
| `codex` | `enabled: true` | app-server JSON-RPC over stdio (`codex app-server --listen stdio://`) | One shared app-server engine; approval policy and sandbox mode are configurable |

### Example config (all keys spelled out)

| File | Use |
|------|-----|
| [configs/config.example.yaml](configs/config.example.yaml) | Dev / localhost defaults |
| [configs/config.mesh-grok.yaml](configs/config.mesh-grok.yaml) | Mesh + Grok + OpenCode + Goose |
| [configs/config.prod.example.yaml](configs/config.prod.example.yaml) | Production-oriented |
| [internal/cli/service/defaults_mcremote.yaml](internal/cli/service/defaults_mcremote.yaml) | Written by `setup-service` when config is missing |

### YAML surface

See [configs/config.example.yaml](configs/config.example.yaml) for every key annotated with defaults and comments. See [docs/config.md](docs/config.md) for the full defaults table.

| Section | Keys |
|---------|------|
| `listen` | `host`, `port` |
| `tls` | `mode`, `enabled`, `cert_file`, `key_file`, `letsencrypt.{domains,email,directory_url,staging,cache_dir,route53.{hosted_zone_id,region,profile,max_retries}}` |
| `log` | `level`, `format` |
| `data_dir` | path (empty = XDG) |
| `auth` | `require_device_token`, `require_client_key`, `allowed_origins` |
| `pair` | `advertise_host` |
| `providers.fake` | `enabled` |
| `providers.grok` | `enabled`, `bin`, `args`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `fs_roots`, `auth_method_id`, `mcp_servers` |
| `providers.goose` | `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm` (always `false`), `turn_stall_notice_seconds`, `stream_coalesce_ms`, `auth_method_id`, `mcp_servers` |
| `providers.opencode` | `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `session_tree` |
| `providers.codex` | `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds` (default `900`), `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `approval_policy`, `sandbox_mode`, `allow_full_access` |
| `headscale` | `control_url` |
| `relay` | `url`, `host_id`, `secret`, `insecure_skip_verify` |
| `limits` | `max_ws_clients`, `max_live_sessions` |

### Stream coalescing

`grok`, `goose`, `opencode`, and `codex` support `stream_coalesce_ms` (default `80`) — hold assistant/thought text this long so it ships as one event instead of one per model token,
capping mid-stream updates at ~12/s. The first chunk of a reply and the tail before any control event are never delayed. `0` disables coalescing; max `1000`.

### `listen.host: tailscale`

`listen.host` accepts the sentinel **`tailscale`**. At startup the daemon
replaces it with this host's Tailscale IPv4 (`tailscale ip -4`), so the listener
binds the mesh interface only and nothing else on the machine's networks can
reach 7531.

It **fails closed**: if no Tailscale IPv4 can be found, `serve` exits with an
error naming the fix. It never falls back to `0.0.0.0`.

`0.0.0.0` remains available as an explicit opt-in for serving clients that are
not on the tailnet; the daemon logs a warning when it is used.

```bash
# Explicit opt-in (dev only — anyone on your network can reach the daemon):
mcremote serve --listen-host 0.0.0.0 --listen-port 7531
```

### `pair.advertise_host`

`pair.advertise_host` (env `MCREMOTE_PAIR_ADVERTISE_HOST` or legacy `MCREMOTE_PAIR_HOST`) pins the host (or host:port) printed into the pair QR/URI, overriding the dynamic detection.
A bare host inherits `listen.port`. Ignored in `letsencrypt` mode (the ACME domain is used). Per-run `mcremote pair --host` overrides this at runtime.

### MCP servers

Providers `grok` and `goose` support an `mcp_servers` list that advertises extra tools/context to the agent.
Each entry is forwarded only if the agent advertises the matching transport (`mcpCapabilities.http` or `mcpCapabilities.sse`):

```yaml
providers:
  grok:
    mcp_servers:
      - name: docs
        transport: http
        url: https://mcp.example.com/sse
        headers:
          Authorization: "Bearer <token>"
```

`mcp_servers` is config-file only (no env / flag — MCP server URLs are sensitive).

---

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
mcremote pair prune --keyless                 # drop devices with no client key
mcremote pair prune --stale 2160h             # unused for ≥ 90 days
```

Short-code QR encodes `mcremote://pair?host=wss://…&code=…&fp=…` (no permanent
secret in the QR; `fp` is the TLS certificate fingerprint the app pins).
Long-token QR still supports `…&token=mcr_…`. Host defaults to Tailscale IPv4 or
`MCREMOTE_PAIR_HOST`. After a successful claim/connect the app stores the durable
`mcr_…` token for reconnect — re-pair only if you revoke.

Tokens are stored as **SHA-256** hashes under `~/.local/share/mcremote/devices.json`.

---

## Provider: Grok

Requires a logged-in Grok Build CLI (`grok` on `PATH`).

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"grok", "name":"task", "cwd":"/path/to/repo" } }
```

When the agent needs tool approval, the server pushes `permission_request` events; answer with `permission.respond` (see [docs/protocol-v1.md](docs/protocol-v1.md)).

Grok supports live mid-session model switching (`/model`) via ACP `session/set_model`, and exposes canonical slash commands `/deep-research` and `/workflow`.

Set `providers.grok.always_approve: true` in config to skip remote permission prompts.

Additional Grok settings:

| Setting | Description |
|---------|-------------|
| `args` | Override the default argv; empty uses `agent --no-leader stdio [+ -m MODEL]` |
| `permission_mode` | Enforces Grok permission policy (`default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`) |
| `allowed_tools` / `disallowed_tools` | Whitelist or blacklist built-in tools (`--tools`, `--disallowed-tools`) |
| `allow_rules` / `deny_rules` | Persistent permission allow / deny rules (`--allow`, `--deny`) |
| `no_subagents` | Disables subagent spawning when true (`--no-subagents`) |
| `disable_web_search` | Disables built-in web search when true (`--disable-web-search`) |
| `fs_roots` | Confine `fs/read_text_file` / `fs/write_text_file` to these roots (+ session cwd). Empty = unrestricted. Defense-in-depth, not a sandbox. |
| `auth_method_id` | ACP auth method to invoke automatically if the agent reports it needs authentication (advertised at initialize) |
| `mcp_servers` | Extra MCP tools/context forwarded only if the agent advertises the matching transport |

---

## Provider: Goose

Goose (block.github.io) is driven through one shared `goose serve` engine using ACP over WebSocket. Pick it per session from the phone's provider menu.
No per-session process — the engine handles all sessions.

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"goose", "name":"task", "cwd":"/path/to/repo" } }
```

| Setting | Description |
|---------|-------------|
| `bin` | `goose` executable path (default: `goose` on `PATH`) |
| `always_approve` | Skip remote permission prompts |
| `default_cwd` | Default working directory for sessions (empty = daemon user's home) |
| `model` | Model selection (empty = Goose's own default) |
| `permission_timeout_seconds` | How long a remote permission request waits (0 = wait forever) |
| `prewarm` | **Always `false`** — Goose starts its serve engine on first use only |
| `turn_stall_notice_seconds` | Notice when a running turn produces no output (0 = off) |
| `stream_coalesce_ms` | Hold streamed text 80ms (default) so it ships as one event; 0 = one per token; max 1000 |
| `auth_method_id` | ACP auth method (advertised at initialize; session/new works without it) |
| `mcp_servers` | Extra MCP tools/context (config-file only) |

---

## Provider: OpenCode

OpenCode (opencode.ai) is driven through one shared long-lived `opencode serve` engine — every session multiplexes over it, so there is no per-session process to configure.
Pick it per session from the phone's provider menu.

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"opencode", "name":"task", "cwd":"/path/to/repo" } }
```

### Session tree (MADR 0020 KD11)

When `providers.opencode.session_tree` is `true` (default), OpenCode supports multi-agent session trees: child aliases, tree-idle EndTurn, and child fan-in.
When `false`, only the parent session is active (pre-0020 kill switch). Requires OpenCode ≥ 1.18.0 when enabled.

### Model pinning

The OpenCode default model can be unreliable. Pin a model explicitly:

```yaml
providers:
  opencode:
    model: "opencode/deepseek-v4-flash-free"   # free, ~1s short prompts
    # or: "anthropic/claude-haiku-4-5"          # faster chat with paid keys
```

---

## Provider: Codex

Codex is driven through one shared `codex app-server --listen stdio://` engine — JSON-RPC over stdio, every session multiplexed over the same process.
Requires `codex` on `PATH`; the daemon logs a warning at startup if the provider is enabled and the binary is missing.

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"codex", "name":"task", "cwd":"/path/to/repo" } }
```

| Setting | Default | Description |
|---------|---------|-------------|
| `bin` | `codex` | Executable path |
| `always_approve` | `false` | Skip remote permission prompts |
| `default_cwd` | *(empty)* | Default working directory (empty = daemon user's home) |
| `model` | *(empty)* | Model selection (empty = Codex's own default) |
| `permission_timeout_seconds` | `900` | How long a remote permission request waits. Longer than the other providers on purpose — long enough to unlock a phone and answer, matching the app-server's long-running tool expectation. `0` waits forever |
| `prewarm` | `false` | Boot the shared app-server at daemon start, skipping the ~500ms cold start on first session |
| `turn_stall_notice_seconds` | `0` | Notice when a running turn produces no output (0 = off) |
| `stream_coalesce_ms` | `80` | Hold streamed text so it ships as one event; 0 = one per token; max 1000 |
| `approval_policy` | *(empty)* | Override Codex's approval policy: `untrusted`, `on-request`, `never`. Empty inherits Codex's own `config.toml` and trusted-project behavior |
| `sandbox_mode` | *(empty)* | Override the sandbox: `read-only`, `workspace-write`, `danger-full-access`. Empty inherits `config.toml` |
| `allow_full_access` | `false` | Advertise the `full-access` session mode, which runs with no approval prompts **and** no sandbox. Opt-in by design — auto-approve is one risk, auto-approve with nothing containing it is another ([MADR 0044](docs/0044-MADR-auto-approve-modes.md) D5) |

Some Codex slash commands have no app-server equivalent and report that rather than failing silently — `/deep-research`, `/workflow`, and diff are not exposed by the protocol.

Design: [MADR 0028](docs/0028-MADR-codex-provider.md) (provider), [0035](docs/0035-MADR-codex-ui-ux-remediation.md) (UI/UX remediation), [0047](docs/0047-MADR-codex-default-mode.md) (default mode),
[0048](docs/0048-MADR-codex-sandbox-namespace.md) (sandbox namespace).

---

## `mcremote engines` — engine lifecycle

`goose serve`, `opencode serve`, and `codex app-server` engines are shared processes spawned by the daemon. Use `mcremote engines` to inspect them:

```bash
# List engine processes and whether their owning daemon is alive
mcremote engines

# Stop engines whose daemon is gone (the daemon also does this at startup)
mcremote engines --reap
```

Only processes carrying mcremote's ownership marker are ever listed or stopped — an `opencode serve` you started by hand is never touched.
The daemon also reaps orphaned engines at startup, skipping any engine owned by another live mcremote.

Ownership is tracked through the on-disk **engine registry** (`mcremote paths` → `engine_registry`), which is the cross-platform contract.
Linux additionally carries environment markers and `PR_SET_PDEATHSIG` as defense in depth, neither of which macOS provides ([MADR 0059](docs/0059-MADR-native-paths-and-linux-macos-parity.md) D8).

---

## mcrelay (public join-plane edge)

Outbound join router for phones that cannot reach mcremote on the mesh
([MADR 0015](docs/0015-MADR-mcrelay-transport-security.md)). Opaque WebSocket splice

- end-to-end TLS to mcremote.

```bash
make build-relay
make install-relay                    # → ~/.local/bin/mcrelay
mkdir -p ~/.config/mcrelay
cp configs/mcrelay.example.yaml ~/.config/mcrelay/config.yaml
chmod 600 ~/.config/mcrelay/config.yaml
# edit hosts + TLS (or use MCRELAY_HOSTS for secrets)
mcrelay setup-service --force --service-config ~/.config/mcrelay/config.yaml
systemctl --user status mcrelay
```

### mcrelay CLI reference

```text
mcrelay [--config PATH] [--log-level LEVEL] [--log-format FORMAT] [--setup-service] [--version]
mcrelay serve [--listen-host HOST] [--listen-port PORT] [--data-dir DIR]
               [--tls-mode letsencrypt|files|off]
               [--tls-domain NAME] [--tls-email ADDR] [--tls-acme-challenge http-01|dns-01]
               [--allow HOST_ID:SECRET] [--allow-legacy-tunnel-secret]
               [--trusted-proxy CIDR] ...
```

| Command | Purpose |
|---------|---------|
| `serve` | Run the relay daemon |
| `setup-service` | Install background service (Linux systemd / macOS launchd) |
| `version` | Print version |

Precedence: CLI flags > `MCRELAY_*` env > config.yaml > defaults.

### Key limits (config file / env only — no CLI flags)

| Setting | Default | Max |
|---------|---------|-----|
| `limits.max_hosts` | 32 | 1024 |
| `limits.max_phones_per_host` | 8 | 256 |
| `limits.max_message_bytes` | 1048576 (1 MiB) | 16777216 (16 MiB) |
| `limits.max_concurrent_join` | 64 | 4096 |
| All per-minute rate limits | varies | 100000/min |
| Duration fields | varies | 604800 (7 days) |

Full config / flags / env reference: **[docs/config-mcrelay.md](docs/config-mcrelay.md)**.

| Artifact | Path |
|----------|------|
| Example config (all keys) | [configs/mcrelay.example.yaml](configs/mcrelay.example.yaml) |
| setup-service default | [internal/cli/service/defaults_mcrelay.yaml](internal/cli/service/defaults_mcrelay.yaml) |
| User unit (all env commented) | [deploy/systemd/mcrelay.user.service](deploy/systemd/mcrelay.user.service) |
| Config / flags / env reference | [docs/config-mcrelay.md](docs/config-mcrelay.md) |
| Ops runbook | [docs/ops-mcrelay.md](docs/ops-mcrelay.md) |
| Hardening plan | [docs/0017-MADR-mcrelay-memory-security-action-plan.md](docs/0017-MADR-mcrelay-memory-security-action-plan.md) |

---

## Deploy

| Method | Path |
|--------|------|
| **User service (preferred)** | `mcremote --setup-service` — embedded template (`internal/cli/service/mcremote.user.service.tmpl`) |
| User unit example | [deploy/systemd/mcremote.user.service](deploy/systemd/mcremote.user.service) — mirrors embedded unit (`Restart=always`, hardening, config-driven listen) |
| System-wide unit | [deploy/systemd/mcremote.service](deploy/systemd/mcremote.service) |
| launchd agent (mcremote) | [deploy/launchd/com.magiccliremote.mcremote.plist](deploy/launchd/com.magiccliremote.mcremote.plist) |
| launchd agent (mcrelay) | [deploy/launchd/com.magiccliremote.mcrelay.plist](deploy/launchd/com.magiccliremote.mcrelay.plist) |
| **mcrelay user unit** | `mcrelay setup-service` + [deploy/systemd/mcrelay.user.service](deploy/systemd/mcrelay.user.service) |

Unit options (user template): `Restart=always`, `TimeoutStopSec=45`, `KillMode=control-group`, XDG env,
`NoNewPrivileges` / `PrivateTmp` / `RestrictSUIDSGID` / `ProtectKernelTunables` / `ProtectControlGroups` / `SystemCallArchitectures=native` / `LimitNOFILE=65536`. Full table: [docs/config.md](docs/config.md).

Useful after setup:

```bash
systemctl --user status mcremote
systemctl --user restart mcremote
journalctl --user -u mcremote -f
systemctl --user disable --now mcremote
```

---

## Android companion (Magic CLI Remote)

Flutter app lives in [`apps/mobile`](apps/mobile). **Android is the shipped target** (Phase 3a) — the release APK is the CI artifact.
A Linux desktop target is checked in as well, and running there avoids needing an Android emulator during development.

```bash
cd apps/mobile
flutter pub get
flutter run                 # pick a device
flutter run -d linux        # desktop, no emulator required
```

Use host `10.0.2.2:7531` from the Android emulator (the daemon must listen on `0.0.0.0` — an explicit dev-only opt-out of the `tailscale` default). See [apps/mobile/README.md](apps/mobile/README.md).

---

## CI / CD (GitHub Actions)

Workflows live under [`.github/workflows/`](.github/workflows/):

A single workflow, `ci.yml`, runs four jobs on push to `master`, pull request, tag `v*`, or manual dispatch:

| Job | Runs | What it does |
|-----|------|--------------|
| `go` | always | gofmt, `go mod tidy` cleanliness, vet, race tests across all packages, version-allocator tests, systemd unit validation, build-tag policy check. On tag: builds **mcremote** + **mcrelay** for **linux/amd64, darwin/arm64, darwin/amd64** and uploads them |
| `flutter` | always | Flutter analyze and test |
| `android-apk` | always | arm64 APK; on tag it must be a **release**-mode build, asserted by `scripts/assert-flutter-release-apk.sh` |
| `release` | tag only | Downloads the APK and Go binaries and attaches them to the GitHub Release |

**Build tags are per-OS and CI enforces it:** Linux binaries build with `netgo,osusergo` (pure-Go DNS and passwd resolution, so the static binary behaves the same on any host);
Darwin builds carry no tags, because `CGO_ENABLED=0` already gets there and forcing them would be wrong. `make verify-build-metadata` checks both.

**Node.js:** CI pins **Node.js 24 LTS** via `actions/setup-node`, `.node-version`, and `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

Download binaries/APK from Actions **Artifacts**, or from a Release after tagging:

```bash
git tag v0.1.0
git push origin v0.1.0
```

### Running a downloaded macOS binary

Go ad-hoc signs every `darwin/arm64` binary at link time, including the ones
CI cross-compiles, so a released binary satisfies the Apple Silicon loader
with no Developer ID and no notarization. Gatekeeper only intervenes when the
file carries a quarantine attribute, which a **browser** download sets:

```bash
xattr -d com.apple.quarantine ./mcremote-darwin-arm64-*
chmod +x ./mcremote-darwin-arm64-*
```

`gh release download` does **not** set the attribute, so the step is
unnecessary when fetching through the CLI — and it never applies to
`make install`, which produces a local file that was never quarantined.
Rationale: [docs/0060-MADR-local-unsigned-build-and-install.md](docs/0060-MADR-local-unsigned-build-and-install.md).

Local APK:

```bash
./scripts/build-apk.sh
```

---

## Headless protocol smoke (no GUI)

```bash
./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531
./bin/mcremote pair create --name smoke   # copy token
./scripts/smoke-protocol.sh -token 'mcr_…' -provider fake
```

---

## Development

```bash
make test          # go test ./...
make race          # race detector
make vet
make fmt           # gofmt
make lint          # golangci-lint
make staticcheck
make vulncheck     # govulncheck
make tidy
make test-all      # the full local gate
cd apps/mobile && flutter test

# Pre-push gate: everything CI will check, before you push
make preflight

# Verifications CI also runs
make verify-build-metadata   # per-OS build tags (Linux netgo,osusergo / Darwin none)
make verify-units            # systemd unit directives
make verify-hooks            # a pre-commit hook actually answers
make install-hooks           # install the repo's git hooks (chain-safe)

# Live provider suites (require the real CLI on PATH; not in CI)
make live-opencode
# → go test -tags live_opencode ./internal/provider/opencode/ (MADR 0020 A6)
make live-codex
# → exercises rejection paths a fake provider cannot cover

# Android runtime profiling (physical device / emulator + DevTools)
make profile-devices
make profile                 # flutter run --profile
make profile-apk             # arm64 profile APK
# → docs/mobile-profiling.md
```

Module: `github.com/maccavelli/magic-cli-remote`

### Code standards

See [AGENTS.md](AGENTS.md) for pre-commit hooks, formatting requirements, and the Go/Dart code standards used in this project.

---

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/0001-MADR-architecture-mcremote.md](docs/0001-MADR-architecture-mcremote.md) | Architecture MADR |
| [docs/0002-MADR-community-assessment-and-stack-recommendations.md](docs/0002-MADR-community-assessment-and-stack-recommendations.md) | Landscape report |
| [docs/0003-MADR-phase1-decisions.md](docs/0003-MADR-phase1-decisions.md) | Phase 1 locked decisions |
| [docs/MADR-phase2-grok-acp.md](docs/MADR-phase2-grok-acp.md) | Phase 2 Grok ACP |
| [docs/0009-MADR-post-hardening-action-plan.md](docs/0009-MADR-post-hardening-action-plan.md) | Post-hardening action plan (remaining work) |
| [docs/0012-MADR-mcremote-daemon-assessment-action-plan.md](docs/0012-MADR-mcremote-daemon-assessment-action-plan.md) | Daemon assessment action plan (Phases 0–4 shipped) |
| [docs/0013-MADR-audit-remediation-decisions.md](docs/0013-MADR-audit-remediation-decisions.md) | Audit remediation decisions & deferral register |
| [docs/0014-MADR-sse-reconnect-resync-decision.md](docs/0014-MADR-sse-reconnect-resync-decision.md) | SSE reconnect resync (H4) |
| [docs/0015-MADR-mcrelay-transport-security.md](docs/0015-MADR-mcrelay-transport-security.md) | mcrelay outbound relay (E2E TLS splice; design) |
| [docs/0016-MADR-mcrelay-audit-hardening.md](docs/0016-MADR-mcrelay-audit-hardening.md) | mcrelay audit findings; capacity/Origin/rate/stability |
| [docs/0017-MADR-mcrelay-memory-security-action-plan.md](docs/0017-MADR-mcrelay-memory-security-action-plan.md) | mcrelay memory/GC/security hardening (A–D, E1–E3) |
| [docs/0018-MADR-mobile-chat-performance-action-plan.md](docs/0018-MADR-mobile-chat-performance-action-plan.md) | Mobile chat performance action plan |
| [docs/0024-MADR-stream-coalescing.md](docs/0024-MADR-stream-coalescing.md) | Coalesce streaming chunk text at the transport emit seam |
| [docs/0025-MADR-goose-provider.md](docs/0025-MADR-goose-provider.md) | Goose ACP-over-HTTP provider (`acphttp` transport over WebSocket; implemented) |
| [docs/0025-PLAN-goose-provider.md](docs/0025-PLAN-goose-provider.md) | Goose provider implementation plan |
| [docs/0026-MADR-mobile-goose-support.md](docs/0026-MADR-mobile-goose-support.md) | Mobile Goose support |
| [docs/0028-MADR-codex-provider.md](docs/0028-MADR-codex-provider.md) | Codex provider (app-server over stdio; implemented) |
| [docs/0035-MADR-codex-ui-ux-remediation.md](docs/0035-MADR-codex-ui-ux-remediation.md) | Codex UI/UX remediation |
| [docs/0047-MADR-codex-default-mode.md](docs/0047-MADR-codex-default-mode.md) | Codex default session mode |
| [docs/0048-MADR-codex-sandbox-namespace.md](docs/0048-MADR-codex-sandbox-namespace.md) | Codex sandbox namespace |
| [docs/ops-mcrelay.md](docs/ops-mcrelay.md) | mcrelay ops: systemd/launchd, LE, secret rotation, smoke checklist |
| [docs/0058-MADR-macos-launchd-service-hardening.md](docs/0058-MADR-macos-launchd-service-hardening.md) | macOS launchd design (agent-only) |
| [docs/0059-MADR-native-paths-and-linux-macos-parity.md](docs/0059-MADR-native-paths-and-linux-macos-parity.md) | XDG paths and Linux/macOS functional parity (`mcremote paths`, engine registry) |
| [docs/mobile-profiling.md](docs/mobile-profiling.md) | Android Flutter profile mode, DevTools, `make profile` |
| [docs/chat-performance.md](docs/chat-performance.md) | Mobile chat scroll/stream performance notes |
| [docs/protocol-v1.md](docs/protocol-v1.md) | WebSocket JSON schema |
| [docs/config.md](docs/config.md) | mcremote config, flags, and env reference (complete matrix) |
| [docs/config-mcrelay.md](docs/config-mcrelay.md) | mcrelay config, flags, env, setup-service (complete matrix) |
| [docs/headscale.md](docs/headscale.md) | Mesh grants & pairing |
| [docs/tls-letsencrypt.md](docs/tls-letsencrypt.md) | Let's Encrypt via ACME DNS-01 (Route 53) |
| [docs/0054-PLAN-hardening-implementation.md](docs/0054-PLAN-hardening-implementation.md) | Hardening plan (complete) |
| [docs/0055-PLAN-mcremote-server-remediation.md](docs/0055-PLAN-mcremote-server-remediation.md) | Server remediation (phases 0–5 shipped) |

## License

See repository license when published.
