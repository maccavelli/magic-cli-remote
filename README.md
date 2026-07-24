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

# -k because the cert is self-signed; the phone pins it by fingerprint instead
curl -sk https://127.0.0.1:7531/healthz          # unauthenticated: {"ok":true}

# /v1/hello carries version/listen/control-URL diagnostics and requires a token
curl -sk -H "Authorization: Bearer $MCREMOTE_TOKEN" https://127.0.0.1:7531/v1/hello
```

Connect a WebSocket client to `wss://127.0.0.1:7531/v1/ws`, send `auth`, then `session.create`. See [docs/protocol-v1.md](docs/protocol-v1.md).

### TLS

The daemon serves HTTPS/WSS by default, in one of three modes selected by
`tls.mode`:

| `tls.mode` | Certificate | How the phone trusts it |
|------------|-------------|--------------------------|
| `letsencrypt` | ACME **DNS-01** via Route 53, renewed automatically | Platform trust store — no pin |
| `selfsigned` | Long-lived P-256 leaf in `<data_dir>/tls.{crt,key}` | SHA-256 fingerprint pinned from the pair QR |
| `off` | none (plaintext `ws://`) | n/a |

Leave `tls.mode` empty and it resolves automatically: **`letsencrypt` once a
domain and an ACME email are configured, `selfsigned` otherwise.**

#### Let's Encrypt (default when configured)

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

#### Self-signed (fallback, and the right choice for bare mesh IPs)

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

#### Plaintext / external terminator

To terminate TLS elsewhere (or for a loopback-only dev box), opt out
explicitly:

```bash
mcremote serve --tls-mode off       # or --tls=false, tls.enabled: false, MCREMOTE_TLS_MODE=off
```

The pair QR then advertises `ws://` and carries no fingerprint. To serve a
certificate you manage yourself, set `tls.cert_file` and `tls.key_file`
(self-signed mode); the daemon uses them as-is, never regenerates them, and
still advertises their fingerprint for pinning.

## CLI reference

```text
mcremote [--config PATH] [--log-level LEVEL] [--log-format FORMAT] [--version] [--help]
mcremote serve [--listen-host HOST] [--listen-port PORT] [--data-dir DIR]
               [--tls|--tls=false] [--tls-mode letsencrypt|selfsigned|off]
               [--tls-domain NAME] [--tls-email ADDR] [--tls-acme-directory URL]
               [--tls-acme-staging] [--tls-route53-zone-id ID]
               [--tls-route53-region REGION] [--tls-route53-profile PROFILE]
mcremote pair [code] | pair create | pair list | pair revoke | pair prune
mcremote setup-service | mcremote --setup-service
mcremote version
```

| Command | Purpose |
|---------|---------|
| `serve` | Run the daemon in the foreground |
| `pair` / `pair code` | 8-char short code (5 min, one-shot) — preferred phone pairing |
| `pair create` | Long-lived `mcr_…` device token |
| `pair list` / `pair revoke` | Manage devices |
| `pair prune` | Remove stale (`--stale`) or keyless (`--keyless`) devices |
| `setup-service` / `--setup-service` | Install systemd **user** unit + start (`--remove` to uninstall) |
| `version` / `--version` | Print version |

Full flag, env, YAML key, and unit tables: **[docs/config.md](docs/config.md)**.

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
| `--unit-name` | `mcremote` | Unit name (no `.service`) |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | Path used in `ExecStart` (not copied) |
| `--service-config` | | Config path embedded in unit (else `--config`) |
| `--data-dir` | | Passed through to `serve` |
| `--listen-host` | _(empty — follow config)_ | Only baked into the unit when set; `tailscale` binds the tailnet IPv4 only |
| `--listen-port` | `0` (follow config) | Only baked into the unit when non-zero |
| `--working-directory` | `$HOME` | systemd `WorkingDirectory` |
| `--env KEY=VAL` | | Extra environment (repeatable) |
| `--print-only` | | Print unit; do not install |
| `--force` | | Overwrite existing unit |
| `--no-enable` / `--no-start` / `--no-linger` | | Skip enable / start / linger |
| `--remove` | | Stop, disable, and delete the unit |

```bash
# Listen from config.yaml (recommended):
mcremote setup-service --config ~/.config/mcremote/config.yaml --force
# Or bake listen settings into the unit:
mcremote setup-service --listen-host tailscale --listen-port 7531 --force
mcremote setup-service --env 'MCREMOTE_LOG_LEVEL=debug' --force
mcremote setup-service --remove
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
| `MCREMOTE_AUTH_REQUIRE_CLIENT_KEY` | Require enrolled TLS client key (`true`/`false`) |
| `MCREMOTE_TLS_ENABLED` | Serve TLS (`true`/`false`, default `true`); `false` == mode `off` |
| `MCREMOTE_TLS_MODE` | `letsencrypt` \| `selfsigned` \| `off` |
| `MCREMOTE_TLS_DOMAINS` | Comma-separated DNS names to request from the ACME CA |
| `MCREMOTE_TLS_EMAIL` | ACME account email |
| `MCREMOTE_TLS_ACME_DIRECTORY_URL` / `MCREMOTE_TLS_ACME_STAGING` / `MCREMOTE_TLS_ACME_CACHE_DIR` | ACME endpoint / cache |
| `MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID` / `_REGION` / `_PROFILE` / `_MAX_RETRIES` | Route 53 DNS-01 solver |
| `MCREMOTE_TLS_CERT_FILE` / `MCREMOTE_TLS_KEY_FILE` | Use an operator-managed certificate instead of the generated one |
| `MCREMOTE_PAIR_HOST` | Host shown in pair QR/code (e.g. tailnet IP). Ignored in `letsencrypt` mode |

Viper also maps other keys as `MCREMOTE_` + uppercased path (e.g. `MCREMOTE_PROVIDERS_GROK_PREWARM`). Full table: [docs/config.md](docs/config.md).

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

## Configuration

Default config path: `~/.config/mcremote/config.yaml` (XDG).  
Default listen (built-in): **`127.0.0.1:7531`** (mesh examples use `tailscale`).  
Precedence: **CLI flags > environment > config file > defaults**.

Examples (all keys spelled out):

| File | Use |
|------|-----|
| [configs/config.example.yaml](configs/config.example.yaml) | Dev / localhost defaults |
| [configs/config.mesh-grok.yaml](configs/config.mesh-grok.yaml) | Mesh + Grok + OpenCode |
| [configs/config.prod.example.yaml](configs/config.prod.example.yaml) | Production-oriented |

YAML surface (see [docs/config.md](docs/config.md) for defaults and notes):

| Section | Keys |
|---------|------|
| `listen` | `host`, `port` |
| `tls` | `mode`, `enabled`, `cert_file`, `key_file`, `letsencrypt.{domains,email,directory_url,staging,cache_dir,route53.{hosted_zone_id,region,profile,max_retries}}` |
| `log` | `level`, `format` |
| `data_dir` | path (empty = XDG) |
| `auth` | `require_device_token`, `require_client_key`, `allowed_origins` |
| `providers.fake` | `enabled` |
| `providers.grok` | `enabled`, `bin`, `args`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `fs_roots` |
| `providers.opencode` | `enabled`, `bin`, `transport`, `args`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `fs_roots` |
| `headscale` | `control_url` |
| `limits` | `max_ws_clients`, `max_live_sessions` |

```bash
./bin/mcremote serve --listen-host 127.0.0.1 --listen-port 7531 --log-level debug
```

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
| [docs/0009-post-hardening-action-plan.md](docs/0009-post-hardening-action-plan.md) | Post-hardening action plan (remaining work) |
| [docs/0012-mcremote-daemon-assessment-action-plan.md](docs/0012-mcremote-daemon-assessment-action-plan.md) | Daemon assessment action plan (Phases 0–4 shipped) |
| [docs/0013-audit-remediation-decisions.md](docs/0013-audit-remediation-decisions.md) | Audit remediation decisions & deferral register |
| [docs/0014-sse-reconnect-resync-decision.md](docs/0014-sse-reconnect-resync-decision.md) | SSE reconnect resync (H4) |
| [docs/0015-mcrelay-transport-security.md](docs/0015-mcrelay-transport-security.md) | mcrelay outbound relay (E2E TLS splice; design) |
| [docs/0016-mcrelay-audit-hardening.md](docs/0016-mcrelay-audit-hardening.md) | mcrelay audit findings; capacity/Origin/rate/stability |
| [docs/0017-mcrelay-memory-security-action-plan.md](docs/0017-mcrelay-memory-security-action-plan.md) | mcrelay memory/GC/security hardening (A–D, E1–E3) |
| [docs/ops-mcrelay.md](docs/ops-mcrelay.md) | mcrelay ops: systemd, LE, secret rotation, smoke checklist |
| [docs/protocol-v1.md](docs/protocol-v1.md) | WebSocket JSON schema |
| [docs/config.md](docs/config.md) | mcremote config, flags, and env reference |
| [docs/config-mcrelay.md](docs/config-mcrelay.md) | mcrelay config, flags, env, setup-service (complete matrix) |
| [docs/headscale.md](docs/headscale.md) | Mesh grants & pairing |
| [docs/tls-letsencrypt.md](docs/tls-letsencrypt.md) | Let's Encrypt via ACME DNS-01 (Route 53) |
| [docs/hardening-implementation-plan.md](docs/hardening-implementation-plan.md) | Hardening plan (complete) |
| [docs/mcremote-server-remediation-plan.md](docs/mcremote-server-remediation-plan.md) | Server remediation (phases 0–5 shipped) |

## mcrelay (public join-plane edge)

Outbound join router for phones that cannot reach mcremote on the mesh
([MADR 0015](docs/0015-mcrelay-transport-security.md)). Opaque WebSocket splice
+ end-to-end TLS to mcremote.

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

**Precedence:** CLI flags > `MCRELAY_*` env > config.yaml > defaults.

| Surface | Location |
|---------|----------|
| Example config (all keys) | [configs/mcrelay.example.yaml](configs/mcrelay.example.yaml) |
| setup-service default | [internal/cli/service/defaults_mcrelay.yaml](internal/cli/service/defaults_mcrelay.yaml) |
| User unit (all env commented) | [deploy/systemd/mcrelay.user.service](deploy/systemd/mcrelay.user.service) |
| Config / flags / env reference | [docs/config-mcrelay.md](docs/config-mcrelay.md) |
| Ops runbook | [docs/ops-mcrelay.md](docs/ops-mcrelay.md) |
| Hardening plan | [docs/0017-mcrelay-memory-security-action-plan.md](docs/0017-mcrelay-memory-security-action-plan.md) |

YAML surface (see config-mcrelay.md for defaults, env, and flags):

| Section | Keys |
|---------|------|
| `listen` | `host`, `port` |
| `tls` | `mode`, `cert_file`, `key_file`, `letsencrypt.{domains,email,directory_url,staging,cache_dir,challenge,http_port,route53.{hosted_zone_id,region,profile,max_retries}}` |
| `log` | `level`, `format` |
| `data_dir` | path (empty = XDG) |
| `hosts` | `[{id, secret}, …]` (or `MCRELAY_HOSTS` / `--allow`) |
| `allow_legacy_tunnel_secret` | bool (default `false`) |
| `trusted_proxies` | CIDR/IP list (default empty — ignore XFF) |
| `limits` | `max_hosts`, `max_phones_per_host`, `max_message_bytes`, `max_concurrent_join`, `accept_per_minute`, `join_per_minute`, `register_per_minute`, `join_per_host_per_minute`, `tunnel_wait_seconds`, `register_idle_seconds`, `splice_idle_seconds`, `splice_max_seconds` |

## Deploy

| Method | Path |
|--------|------|
| **User service (preferred)** | `mcremote --setup-service` — embedded template (`internal/cli/service/mcremote.user.service.tmpl`) |
| User unit example | [deploy/systemd/mcremote.user.service](deploy/systemd/mcremote.user.service) — mirrors embedded unit (`Restart=always`, hardening, config-driven listen) |
| System-wide unit | [deploy/systemd/mcremote.service](deploy/systemd/mcremote.service) |
| launchd | [deploy/launchd/com.magiccliremote.mcremote.plist](deploy/launchd/com.magiccliremote.mcremote.plist) |
| **mcrelay user unit** | `mcrelay setup-service` + [deploy/systemd/mcrelay.user.service](deploy/systemd/mcrelay.user.service) |

Unit options (user template): `Restart=always`, `TimeoutStopSec=45`, `KillMode=mixed`, XDG env, `NoNewPrivileges` / `PrivateTmp` / `RestrictSUIDSGID` / `ProtectKernelTunables` / `ProtectControlGroups` / `SystemCallArchitectures=native` / `LimitNOFILE=65536`. Full table: [docs/config.md](docs/config.md#unit-file-options-embedded-user-template).

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

Use host `10.0.2.2:7531` from the Android emulator (the daemon must listen on `0.0.0.0` — an explicit dev-only opt-out of the `tailscale` default). See [apps/mobile/README.md](apps/mobile/README.md).

## CI / CD (GitHub Actions)

Workflows live under [`.github/workflows/`](.github/workflows/):

| Workflow | Trigger | What it does |
|----------|---------|----------------|
| `ci.yml` | push / PR / manual | Go vet+test; on tag `v*`: build **mcremote** + **mcrelay** (linux/amd64), Flutter test, arm64 APK, attach all three to the GitHub Release |

**Node.js:** CI pins **Node.js 24 LTS** via `actions/setup-node`, `.node-version`, and `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

Download binaries/APK from Actions **Artifacts**, or from a Release after tagging:

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
