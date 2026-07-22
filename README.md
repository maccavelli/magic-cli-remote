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
               [--tls-mode letsencrypt|selfsigned|off] [--tls-domain NAME] [--tls-email ADDR]
               [--tls-route53-zone-id ID] [--tls-route53-region REGION] [--tls-acme-staging]
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
| `--listen-host` | Bind host (default from config: `127.0.0.1`). `tailscale` resolves to this host's Tailscale IPv4 at startup and refuses to start without one |
| `--listen-port` | Bind port (default `7531`) |
| `--data-dir` | State directory (devices, pair codes, sessions, TLS cert) |
| `--tls` | Legacy on/off switch; `--tls=false` == `--tls-mode off` |
| `--tls-mode` | `letsencrypt` \| `selfsigned` \| `off` (default: auto) |
| `--tls-domain` | DNS name to request from the ACME CA (repeatable); first is advertised to phones |
| `--tls-email` | ACME account email (required for `letsencrypt`) |
| `--tls-acme-directory` | ACME directory URL (default: Let's Encrypt production) |
| `--tls-acme-staging` | Use the Let's Encrypt staging CA |
| `--tls-route53-zone-id` | Route 53 hosted zone ID for DNS-01 |
| `--tls-route53-region` / `--tls-route53-profile` | AWS region / shared-config profile |

### `setup-service` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--unit-name` | `mcremote` | Unit name (no `.service`) |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | Path used in `ExecStart` (not copied) |
| `--service-config` | | Config path embedded in unit |
| `--data-dir` | | Passed through to `serve` |
| `--listen-host` | `tailscale` | Binds the tailnet IPv4 only; fails closed if there is none |
| `--listen-port` | `7531` | Port |
| `--working-directory` | `$HOME` | systemd `WorkingDirectory` |
| `--env KEY=VAL` | | Extra environment (repeatable) |
| `--print-only` | | Print unit; do not install |
| `--force` | | Overwrite existing unit |
| `--no-enable` / `--no-start` / `--no-linger` | | Skip enable / start / linger |

```bash
mcremote setup-service --listen-host tailscale --listen-port 7531 \
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
| `MCREMOTE_TLS_ENABLED` | Serve TLS (`true`/`false`, default `true`); `false` == mode `off` |
| `MCREMOTE_TLS_MODE` | `letsencrypt` \| `selfsigned` \| `off` |
| `MCREMOTE_TLS_DOMAINS` | Comma-separated DNS names to request from the ACME CA |
| `MCREMOTE_TLS_EMAIL` | ACME account email |
| `MCREMOTE_TLS_ACME_DIRECTORY_URL` / `MCREMOTE_TLS_ACME_STAGING` | ACME endpoint selection |
| `MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID` / `_REGION` / `_PROFILE` | Route 53 DNS-01 solver |
| `MCREMOTE_TLS_CERT_FILE` / `MCREMOTE_TLS_KEY_FILE` | Use an operator-managed certificate instead of the generated one |
| `MCREMOTE_PAIR_HOST` | Host shown in pair QR/code (e.g. tailnet IP). Ignored in `letsencrypt` mode |

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
| [docs/protocol-v1.md](docs/protocol-v1.md) | WebSocket JSON schema |
| [docs/config.md](docs/config.md) | Config, flags, and env reference |
| [docs/headscale.md](docs/headscale.md) | Mesh grants & pairing |
| [docs/tls-letsencrypt.md](docs/tls-letsencrypt.md) | Let's Encrypt via ACME DNS-01 (Route 53) |
| [docs/hardening-implementation-plan.md](docs/hardening-implementation-plan.md) | Hardening plan (complete) |
| [docs/mcremote-server-remediation-plan.md](docs/mcremote-server-remediation-plan.md) | Server remediation (phases 0–5 shipped) |

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

Use host `10.0.2.2:7531` from the Android emulator (the daemon must listen on `0.0.0.0` — an explicit dev-only opt-out of the `tailscale` default). See [apps/mobile/README.md](apps/mobile/README.md).

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
