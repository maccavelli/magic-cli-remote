# magic-cli-remote (`mcremote` / `mcrelay`)

Provider-agnostic **Go daemon** that multiplexes coding-agent CLI sessions and
exposes secure remote control over **WebSocket + JSON** to a **Flutter** phone
app. Access paths: **Tailscale/Headscale** (direct), **mcrelay** (public join
plane when the phone is off-mesh), or loopback/LAN for development.

| Binary | Role |
|--------|------|
| **`mcremote`** | Host daemon: providers, sessions, TLS, pairing, protocol v1 |
| **`mcrelay`** | Optional public-edge join router: host register + phone join + opaque WS splice |

---

## Install on Linux and macOS

One command. No Go toolchain, no cloning, and no `sudo` anywhere:

```bash
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

That is the whole installation. On Linux with systemd you get a daemon that
comes back after a reboot. On macOS you get a LaunchAgent that runs for the
login session (it stops on logout — keep a login session for an always-on Mac).

### What it actually does

The script works through five steps and tells you what it did at each one:

1. **Works out what machine it is on.** It reads `uname` and picks the right
   build for your CPU — `amd64` or `arm64`. If you are on something that has
   no published build (32-bit ARM, for example) it stops immediately and says
   so, rather than downloading something that cannot run.
2. **Downloads the two binaries** for the newest release, `mcremote` and
   `mcrelay`.
3. **Checks them before trusting them.** Every release publishes a
   `SHA256SUMS` file, and the script compares what it downloaded against the
   published checksum. If the two disagree it stops and installs nothing —
   and if you already had a working copy, that copy is left exactly as it was.
4. **Installs them to `~/.local/bin`.** Downloads are staged and then renamed
   into place, so a half-finished download can never end up as a live binary.
   If `~/.local/bin` is not on your `PATH`, the script tells you and prints
   the line to add. It will not edit your shell profile behind your back.
5. **Sets up the background service.** On a normal systemd machine it installs
   a user service and enables *lingering*, which is the piece that lets the
   daemon keep running after you log out and start again after a reboot. On
   macOS it installs a user LaunchAgent (`~/Library/LaunchAgents`); there is
   no user-level linger, so the agent is session-bound.

Everything lands under your home directory. The script never asks for root
and never touches anything outside `$HOME`.

### Passing options

Because the command pipes into `sh`, you cannot put flags after it — the
shell would hand them to `sh` instead of to the script. Use environment
variables instead:

```bash
URL=https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh

# install the binaries but do not set up any service
curl -fsSL "$URL" | MCREMOTE_NO_SERVICE=1 sh

# install a specific release instead of the newest one
curl -fsSL "$URL" | MCREMOTE_VERSION=0.13.2 sh

# install somewhere other than ~/.local/bin
curl -fsSL "$URL" | MCREMOTE_INSTALL_DIR=$HOME/bin sh
```

If you would rather read the script before running it — a reasonable habit
with any `curl | sh` — download it first and run it with normal flags:

```bash
curl -fsSLO https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh
less install.sh
sh install.sh --dry-run     # shows every decision, changes nothing
sh install.sh --help        # full flag list
```

### Upgrading later

You only need the installer once. After that the daemon updates itself:

```bash
mcremote update
```

Published binaries are stamped `BASE.N` (e.g. `0.13.9.1`). `update`
follows that string, not the `v0.13.9` tag; `--force` is only for a
local `make` build. Each command recycles only its own unit, if one
exists.

`update` also reconciles the service definition: if the release changed the
systemd unit or the launchd plist, it re-renders yours from the new template —
keeping the options you baked in at setup time — and reloads the service manager
before restarting. A definition you hand-edited is left alone and reported.

Re-running the installer is also safe. It notices the daemons that are
already running, stops them, swaps in the new binaries, and starts them back
up. It refreshes a unit it recognises as its own; one you hand-edited is kept
as you wrote it — pass `--force-service` to overwrite that from defaults.

### If your machine has no systemd

Plenty of Linux environments do not have a per-user systemd: Alpine,
containers, WSL1, and WSL2 unless you have explicitly turned systemd on. The
installer still works on all of them — it verifies and installs the binaries
exactly the same way, then tells you honestly that it could not configure a
background service and shows you how to start the daemon yourself.

Where a rootless supervisor *is* available it will use it. runit and s6 are
both supported, and the script writes the service definition for you.

One honest limitation, worth understanding rather than discovering later:
**only systemd can give you start-on-boot without root.** Something running
as root at boot has to launch your service, and systemd's lingering is the
mechanism that does that for a normal user. runit and s6 will happily
supervise the daemon and restart it if it crashes, but making *them* start at
boot needs a one-time root action. The installer always tells you which of
the two you got, and never claims persistence it did not actually set up.

Full per-environment detail, including WSL2 setup and the AppArmor note for
Ubuntu 24.04 and newer, is in
[docs/ops-linux-install.md](docs/ops-linux-install.md).

### macOS notes and Windows

The same one-liner installs on macOS. Full Disk Access is **not** granted by
the script — add `~/.local/bin/mcremote` in System Settings → Privacy &
Security after install. Unsigned upgrades drop that grant; to keep it, sign
with `MC_CODESIGN_IDENTITY` (see
[docs/ops-macos-tcc.md](docs/ops-macos-tcc.md)). Windows is not a supported
host; use WSL2.

---

**Current product surface (v0.8.x lineage):** Grok Build ACP, OpenCode, Goose,
Codex, Kilo, and Fake providers; remote tool permissions; session modes / model
catalogs / thinking levels; stream coalescing; protocol v2 reconnect/resume;
XDG path layout with Linux/macOS parity (`mcremote paths`); engine lifecycle
(`mcremote engines`); outbound mcrelay registration; Android companion
(shipped release target) with iOS and Linux-desktop dev targets.

Module: `github.com/maccavelli/magic-cli-remote`

---

## Architecture (grounded)

```text
  Flutter app (Android product; Linux desktop for dev)
       │  wss:// (or ws:// offline/dev)
       │  pair QR: mcremote://pair?host=…&code=…&fp=…&mode=…
       ▼
  ┌──────────────── mcremote (host) ────────────────┐
  │  TLS (letsencrypt DNS-01 / selfsigned / off)    │
  │  HTTP: GET /healthz, GET /v1/hello, GET /v1/ws  │
  │  auth (device token + optional client TLS key)  │
  │  session manager + event bus + history ring     │
  │  providers: grok | goose | opencode | codex | kilo | fake │
  │  admin.sock (local Unix, pair-revoke kick)      │
  └───────────┬─────────────┬───────────────────────┘
              │             │ optional outbound
              │             ▼
              │        mcrelay (public edge)
              │        register / join / splice
              ▼
     agent engines (shared processes where applicable)
       grok agent stdio · goose serve · opencode serve · codex app-server · kilo serve
```

Design spine: [docs/0001-MADR-architecture-mcremote.md](docs/0001-MADR-architecture-mcremote.md),
wire contract: [docs/protocol-v1.md](docs/protocol-v1.md).

**Phone → daemon transport choices** (app path selection, not daemon config):
mesh direct, relay join, or LAN — see
[docs/0062-MADR-phone-transport-selection.md](docs/0062-MADR-phone-transport-selection.md)
and [docs/0061-MADR-relay-pair-advertise-and-path-selection.md](docs/0061-MADR-relay-pair-advertise-and-path-selection.md).

---

## Requirements

- **Go 1.26.x** (module pins `go 1.26.5`)
- **Linux** (primary) or **macOS**
- Optional mesh: Headscale + Tailscale clients ([docs/headscale.md](docs/headscale.md))
- For `setup-service`: Linux **systemd --user**, or macOS **launchd** user LaunchAgent (no sudo)
- Provider binaries on `PATH` as needed: `grok`, `opencode`, `goose`, `codex`,
  `kilo` (enabled providers missing a binary are listed as not ready; daemon
  still starts)
- Flutter companion: **Flutter 3.44.x** / **Dart ≥ 3.12.2** (CI pins Flutter **3.44.6**)

---

## Quick start

Building from source, for development or on a platform the installer does not
cover. To install prebuilt binaries on Linux instead, see
[Install on Linux](#install-on-linux) at the top.

```bash
make build
./bin/mcremote version
./bin/mcremote --version
./bin/mcremote serve
```

Install **both** host OS/arch binaries into the user bin directory
(`~/.local/bin` on Linux and macOS). `make install` builds first, then atomic-
swaps each binary and restarts an enabled service if present:

```bash
make install
# → ~/.local/bin/mcremote
# → ~/.local/bin/mcrelay

# optional overrides:
make install USER_BIN_DIR=$HOME/bin
make build GOOS=linux GOARCH=arm64   # cross-compile only (no install)
# install refuses cross-arch targets (MADR 0060): use build alone for that
```

Relay-only install:

```bash
make install-relay   # → ~/.local/bin/mcrelay
```

**Build versions (ledger):** each `make build` / `make install` stamps binaries
as `BASE.N` where `BASE` is the latest **release** tag (`v0.7.0` → `0.7.0`) and
`N` is a global build serial (`0.7.0.1`, `0.7.0.2`, …). Local builds may append
a uniqueness suffix when offline (e.g. `0.7.0.2.g41548d0`).

| Piece | Role |
|-------|------|
| Release tags `vX.Y.Z` | Product version base (manual / release process) |
| Build tags `build/X.Y.Z.N` | **Source of truth** for N — claimed by creating+pushing the tag |
| `.build-counter` | Local cache only (gitignored); speeds offline / reduces fetch races |

Allocation (`scripts/next-build-version.sh`): fetch `build/*` tags → max N →
create `build/BASE.N` → **push with retry** on contention. CI always pushes on
tag runs. Local builds try to push when `origin` is reachable; offline builds
append a uniqueness suffix.

Override: `make build VERSION=1.2.3`. Disable push: `MCREMOTE_VERSION_PUSH=0 make build`.

All long flags use a **double dash** (`--help`, `--config`, `--listen-host`,
`--setup-service`, …). Short `-h` is help only.

---

## Service installation

`setup-service` installs a background service definition that manages the
daemon. It does **not** copy the binary — install first with `make install`.

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
# Binary lands at ~/.local/bin/mcremote (and mcrelay)
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
# Copy the mesh + agents example config:
cp configs/config.mesh-grok.yaml ~/.config/mcremote/config.yaml
chmod 600 ~/.config/mcremote/config.yaml
# Edit: TLS domains, provider options, listen host, etc.
```

Or let `setup-service` write a default config automatically (it creates
`~/.config/mcremote/config.yaml` with built-in defaults when the file is missing;
never overwrites an existing file).

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

On macOS there is no user-level linger. LaunchAgents stop when you log out. For
always-on Macs, keep a login session (auto-login or Screen Sharing). macOS 13+
may show a **Background Items** notification; System Settings → General → Login
Items can disable the agent.

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

Stops the daemon, disables the service, and deletes the unit/plist. The binary
and config directory are left intact.

Manual plist examples:
[deploy/launchd/com.magiccliremote.mcremote.plist](deploy/launchd/com.magiccliremote.mcremote.plist),
[deploy/launchd/com.magiccliremote.mcrelay.plist](deploy/launchd/com.magiccliremote.mcrelay.plist).

---

## TLS

The daemon serves HTTPS/WSS by default, in one of three modes selected by
`tls.mode`:

| `tls.mode` | Certificate | How the phone trusts it |
|------------|-------------|--------------------------|
| `letsencrypt` | ACME **DNS-01** via Route 53, renewed automatically | Platform chain validation **or** recovery pin (`fp`) |
| `selfsigned` | Long-lived P-256 leaf in `<data_dir>/tls.{crt,key}` | SHA-256 fingerprint **only** (pinned from the pair QR) |
| `off` | none (plaintext `ws://`) | n/a |

Leave `tls.mode` empty and it resolves automatically: **`letsencrypt` once a
domain and an ACME email are configured, `selfsigned` otherwise.**

The pair URI always carries `mode=` (`selfsigned` | `letsencrypt` | `off`) and
carries `fp=` in both TLS-on modes. See
[docs/protocol-v1.md](docs/protocol-v1.md) (transport security).

### Let's Encrypt (default when configured)

```bash
mcremote serve \
  --tls-domain devbox.ts.lallygag.net \
  --tls-email ops@lallygag.net \
  --tls-route53-zone-id Z0123456789ABCDEFGHIJ \
  --tls-route53-region us-east-1 \
  --tls-acme-staging          # drop once staging works
```

Only the **DNS-01** challenge is supported for mcremote: daemon nodes are often
mesh-only, so an ACME validator cannot reach them for HTTP-01 or TLS-ALPN-01.
DNS-01 needs a `_acme-challenge` TXT record in a **public** Route 53 zone.

Consequences for pairing:

- The QR advertises the **DNS name** (`devbox.ts.lallygag.net:7531`), never the
  `100.64.0.0/10` mesh IP — Let's Encrypt does not issue for IPs.
- The QR **does** carry `fp=` and `mode=letsencrypt`. The pin is the
  **self-signed fallback leaf**, not the ACME leaf. While ACME is healthy the
  client validates the public chain and ignores the pin; if issuance fails the
  daemon falls back to that self-signed cert and the pin keeps an already-paired
  phone working. Trust rule: **valid chain OR fingerprint match** — never
  fingerprint-only for this mode.

If ACME fails at start, the daemon logs an error and **falls back to the
self-signed certificate rather than refusing to start**. Certificates last ~90
days; a host left dark longer returns with an expired cert until renewal
succeeds.

IAM / zone setup for DNS-01:
**[docs/iam-route53-acme.md](docs/iam-route53-acme.md)**.
Config matrix: **[docs/config.md](docs/config.md)**.

### Self-signed (fallback, and the right choice for bare mesh IPs)

On first run the daemon writes a self-signed P-256 certificate to
`~/.local/share/mcremote/{tls.crt,tls.key}` (mode `0600`) covering the machine
hostname, `localhost`, and its LAN/mesh IPs, then reuses it on every later run.
The phone trusts the daemon by pinning the certificate's SHA-256 fingerprint,
which `mcremote pair` prints and embeds in the QR as `fp=…` with
`mode=selfsigned`.

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

```bash
mcremote serve --tls-mode off       # or --tls=false, tls.enabled: false, MCREMOTE_TLS_MODE=off
```

The pair QR then advertises `ws://`, `mode=off`, and no fingerprint. To serve a
certificate you manage yourself, set `tls.cert_file` and `tls.key_file`
(self-signed mode); the daemon uses them as-is, never regenerates them, and
still advertises their fingerprint for pinning.

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

Short-code QR encodes
`mcremote://pair?host=wss://…&code=…&fp=…&mode=…` (no permanent secret in the
QR). Long-token QR still supports `…&token=mcr_…`. Host defaults to the ACME
domain in `letsencrypt` mode, else Tailscale IPv4 / `pair.advertise_host` /
loopback. After a successful claim/connect the app stores the durable `mcr_…`
token for reconnect — re-pair only if you revoke.

Tokens are stored as **SHA-256** hashes under
`~/.local/share/mcremote/devices.json` (mode `0600`). Device identity also binds
to an enrolled client TLS key when `auth.require_client_key` is true (default).

`pair revoke` kicks live WebSocket clients via the local **admin socket** when
the daemon is running; if the socket is missing, revoke is disk-only until the
client reconnects and fails auth.

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
mcremote receipts list [--device ID] | verify --device ID | show --device ID --permission ID
mcremote version
mcremote completion bash|zsh|fish|powershell
```

| Command | Purpose |
|---------|---------|
| `serve` | Run the daemon in the foreground |
| `pair` / `pair code` | 8-char short code (5 min, one-shot) — preferred phone pairing |
| `pair create` | Long-lived `mcr_…` device token |
| `pair list` / `pair revoke` | Manage devices |
| `pair prune` | Remove stale (`--stale`) or keyless (`--keyless`) devices |
| `setup-service` / `--setup-service` | Install background service + start (Linux systemd --user / macOS launchd agent; `--remove` to uninstall) |
| `engines` | List agent engine processes (`goose`/`opencode`/`kilo` `serve`, `codex app-server`) and whether their owning daemon is alive (`--reap` to stop orphans) |
| `paths` | Print the resolved XDG layout — config, data, state, cache, runtime, admin socket, engine registry, log dir (`--json` for machine-readable). Read-only: creates nothing |
| `receipts list` / `verify` / `show` | Inspect and verify signed permission-decision receipts (opt-in, see [Signed receipts](#signed-receipts)) |
| `version` / `--version` | Print version |
| `completion` | Shell completion scripts |

### Global flags

| Flag | Description |
|------|-------------|
| `--config` | Config YAML (`MCREMOTE_CONFIG`) |
| `--log-level` | `debug` \| `info` \| `warn` \| `error` |
| `--log-format` | `text` \| `json` |
| `--help` / `-h` | Help |
| `--version` | Version |
| `--setup-service` | Install + enable + start background service (Linux systemd / macOS launchd) |

Root-level flags also include the full `setup-service` option set (`--binary`,
`--force`, `--print-only`, `--remove`, `--env`, …) so `mcremote --setup-service`
works without a subcommand.

### `serve` flags

| Flag | Description |
|------|-------------|
| `--listen-host` | Override `listen.host` (config default `127.0.0.1`). `tailscale` resolves to this host's Tailscale IPv4 at startup; waits for one rather than binding `0.0.0.0` |
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

Viper also maps other keys as `MCREMOTE_` + uppercased path with `_` (e.g.
`MCREMOTE_PROVIDERS_GROK_PREWARM`, `MCREMOTE_PROVIDERS_GROK_SANDBOX`). Full table:
[docs/config.md](docs/config.md).

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

Default config path: `~/.config/mcremote/config.yaml` (XDG on Linux **and**
macOS; see `mcremote paths`).

### Resolved path layout

The daemon resolves every directory through the XDG variables on **both** Linux
and macOS — macOS does not get Apple's `~/Library/Application Support` layout,
deliberately, so a single set of paths documents both platforms
([MADR 0059](docs/0059-MADR-native-paths-and-linux-macos-parity.md)).
`mcremote paths` prints exactly what the daemon will use, creating nothing:

```bash
mcremote paths            # aligned text
mcremote paths --json     # machine-readable
```

| Entry | Default | Holds |
|-------|---------|-------|
| `config_dir` | `$XDG_CONFIG_HOME/mcremote` → `~/.config/mcremote` | `config.yaml` |
| `data_dir` | `$XDG_DATA_HOME/mcremote` → `~/.local/share/mcremote` | `devices.json`, pair codes, sessions, `tls.{crt,key}`, ACME cache |
| `state_dir` | `$XDG_STATE_HOME/mcremote` → `~/.local/state/mcremote` | Per-instance state |
| `cache_dir` | `$XDG_CACHE_HOME/mcremote` → `~/.cache/mcremote` | Regenerable caches |
| `runtime_dir` | `$XDG_RUNTIME_DIR/mcremote/<instance_key>`; else a secure temp leaf | Admin control socket (instance-keyed) |
| `admin_socket` | `<runtime_dir>/admin.sock` | Local control socket (unix domain, mode `0600`, never network-exposed) |
| `engine_registry` | `<state_dir>/instances/<instance_key>/engines` | Engine ownership records read by `mcremote engines` |
| `instance_key` | Derived from the resolved data dir | Keeps two daemons with different `--data-dir` from colliding |
| `log_dir` | macOS: `~/Library/Logs/mcremote` | LaunchAgent stdout/stderr. On Linux the unit logs to journald instead |

Relative `XDG_*` values are ignored with a diagnostic, per the XDG absolute-path
rule. `--data-dir` (or `MCREMOTE_DATA_DIR`) re-bases the data directory and the
derived instance key; pass it to `paths` to preview the result.

Default listen (built-in): **`127.0.0.1:7531`** (mesh examples use `tailscale`).
Precedence: **CLI flags > environment > config file > defaults**.

### Provider matrix

| Provider | Default | Transport | Notes |
|----------|---------|-----------|-------|
| `fake` | `enabled: false` | stdio | Dev/smoke only |
| `grok` | `enabled: true` | stdio (`grok agent --no-leader stdio`) | ACP; remote permissions via WebSocket; optional prewarm |
| `goose` | `enabled: true` | ACP over WebSocket (HTTP transport) | One shared `goose serve` engine; no prewarm |
| `opencode` | `enabled: true` | HTTP + SSE | One shared `opencode serve` engine; multi-agent session tree (MADR 0020 KD11) |
| `codex` | `enabled: true` | app-server JSON-RPC over stdio (`codex app-server --listen stdio://`) | One shared app-server engine; approval policy and sandbox mode are configurable |
| `kilo` | `enabled: true` | HTTP + SSE | One shared `kilo serve` engine, same architecture as OpenCode but a distinct dialect (MADR 0075) |

### Example configs

| File | Use |
|------|-----|
| [configs/config.example.yaml](configs/config.example.yaml) | Dev / localhost defaults (every key annotated) |
| [configs/config.mesh-grok.yaml](configs/config.mesh-grok.yaml) | Mesh + Grok + OpenCode + Goose (+ other agents) |
| [configs/config.prod.example.yaml](configs/config.prod.example.yaml) | Production-oriented |
| [internal/cli/service/defaults_mcremote.yaml](internal/cli/service/defaults_mcremote.yaml) | Written by `setup-service` when config is missing |

### YAML surface

See [configs/config.example.yaml](configs/config.example.yaml) for every key
annotated with defaults and comments. See [docs/config.md](docs/config.md) for
the full defaults table and env map.

| Section | Keys |
|---------|------|
| `listen` | `host`, `port` |
| `tls` | `mode`, `enabled`, `cert_file`, `key_file`, `letsencrypt.{domains,email,directory_url,staging,cache_dir,route53.{hosted_zone_id,region,profile,max_retries}}` |
| `log` | `level`, `format` |
| `data_dir` | path (empty = XDG) |
| `display_name` | friendly host name shown on the phone (empty = dialled address) |
| `auth` | `require_device_token`, `require_client_key`, `allowed_origins` |
| `pair` | `advertise_host` |
| `providers.fake` | `enabled` |
| `providers.grok` | `enabled`, `bin`, `args`, `always_approve`, `default_cwd`, `model`, `reasoning_effort`, `permission_mode`, `allowed_tools`, `disallowed_tools`, `allow_rules`, `deny_rules`, `no_subagents`, `disable_web_search`, `sandbox`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `fs_roots`, `auth_method_id`, `mcp_servers` |
| `providers.goose` | `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm` (default `false`), `turn_stall_notice_seconds`, `stream_coalesce_ms`, `auth_method_id`, `with_builtins`, `mcp_servers` |
| `providers.opencode` | `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `session_tree`, `pure` |
| `providers.codex` | `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds` (default `900`), `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `approval_policy`, `sandbox_mode`, `allow_full_access` |
| `providers.kilo` | `enabled` (default `true`), `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm`, `turn_stall_notice_seconds`, `stream_coalesce_ms`, `session_tree` (default `false`), `pure` |
| `headscale` | `control_url` |
| `relay` | `url`, `host_id`, `secret`, `insecure_skip_verify` |
| `limits` | `max_ws_clients`, `max_live_sessions` |

### Stream coalescing

`grok`, `goose`, `opencode`, `codex`, and `kilo` support `stream_coalesce_ms`
(default `80`) — hold assistant/thought text this long so it ships as one event instead
of one per model token, capping mid-stream updates at ~12/s. The first chunk of
a reply and the tail before any control event are never delayed. `0` disables
coalescing; max `1000`.

### `listen.host: tailscale`

`listen.host` accepts the sentinel **`tailscale`**. At startup the daemon
replaces it with this host's Tailscale IPv4 (`tailscale ip -4`), so the listener
binds the mesh interface only and nothing else on the machine's networks can
reach 7531.

It **fails closed**: it never falls back to `0.0.0.0`. If the tailnet address
is late (common just after reboot), `serve` waits and binds when
`tailscale ip -4` succeeds.

`0.0.0.0` remains available as an explicit opt-in for serving clients that are
not on the tailnet; the daemon logs a warning when it is used.

```bash
# Explicit opt-in (dev only — anyone on your network can reach the daemon):
mcremote serve --listen-host 0.0.0.0 --listen-port 7531
```

### `pair.advertise_host`

`pair.advertise_host` (env `MCREMOTE_PAIR_ADVERTISE_HOST` or legacy
`MCREMOTE_PAIR_HOST`) pins the host (or host:port) printed into the pair QR/URI,
overriding dynamic detection. A bare host inherits `listen.port`. Ignored in
`letsencrypt` mode (the ACME domain is used). Per-run `mcremote pair --host`
overrides this at runtime.

### MCP servers

Providers `grok` and `goose` support an `mcp_servers` list that advertises extra
tools/context to the agent. Each entry is forwarded only if the agent advertises
the matching transport (`mcpCapabilities.http` or `mcpCapabilities.sse`):

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

## Protocol snapshot

Transport: **WebSocket** at `GET /v1/ws` (TLS by default) — the same endpoint
serves both protocol versions. Also:

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `GET /healthz` | none | Liveness only |
| `GET /v1/hello` | device token (+ client key when required) | Authenticated hello / capability probe; reports `protocols: [1, 2]` |
| `GET /v1/ws` | device token after upgrade | Protocol v1/v2 control plane |

Every client→daemon frame is capped at **1 MiB** serialized UTF-8 JSON.

**Protocol v2** ([docs/protocol-v2.md](docs/protocol-v2.md), MADR 0068) is a
fully shipped delta over v1, negotiated per-connection (client offers
`protocols` on `auth`/`pair.claim`; server picks the highest mutual version —
absent an offer, a client stays plain v1). It adds, on top of the unchanged v1
envelope/messages/auth/errors: a capability block (`auth_ok.caps`), WS-ping
liveness with a pong-extended deadline, connection replacement (close code
`4001` on a newer login), gap signalling (`first_seq`/`latest_seq`/`epoch` so
a truncated or stale history read is never silent), and reconnect **resume**
(`resume_token` + `auth.resume`, skipping the reconcile walk when nothing
changed).

Representative client messages (full schema in
[docs/protocol-v1.md](docs/protocol-v1.md), v2 delta in
[docs/protocol-v2.md](docs/protocol-v2.md)):

| Type | Purpose |
|------|---------|
| `auth` / `pair.claim` | Authenticate or claim a short code |
| `session.create` / `.list` / `.close` / `.delete` / `.prompt` / `.cancel` | Session lifecycle |
| `session.set_mode` / `.set_config_option` | Session mode and config options |
| `session.history` / `.pending_asks` | History ring + outstanding permission asks |
| `permission.respond` / `question.respond` | Tool approval and agent questions |
| `providers.list` / `models.list` / `agents.list` / `commands.list` | Catalogs |
| `session.fork` / `.revert` / `.unrevert` / `.diff` / `.rename` / `.diagnostics` | Provider-native ops where available |
| `ping` | Keepalive |

Canonical slash commands (`/help`, `/plan`, `/mode`, `/model`, `/thinking`, …)
are documented in protocol-v1 and
[docs/0023-MADR-canonical-slash-commands.md](docs/0023-MADR-canonical-slash-commands.md).

---

## Provider: Grok

Requires a logged-in Grok Build CLI (`grok` on `PATH`).

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"grok", "name":"task", "cwd":"/path/to/repo" } }
```

When the agent needs tool approval, the server pushes `permission_request`
events; answer with `permission.respond` (see
[docs/protocol-v1.md](docs/protocol-v1.md)).

Grok supports live mid-session model switching (`/model`) via ACP
`session/set_model`, and exposes canonical slash commands `/deep-research`,
`/workflow`, `/loop`, and `/review` (the last two measured on grok 1.0.3).

Set `providers.grok.always_approve: true` in config to skip remote permission
prompts (process-wide). Per-session auto-approve is a **session mode** in the
app (MADR 0044 / 0049), distinct from config `always_approve`.

| Setting | Description |
|---------|-------------|
| `args` | Override the default argv; empty uses `--no-auto-update agent --no-leader stdio [+ -m MODEL]` |
| `model` | Model override (empty = grok's default; live 1.0.3 floor is `grok-4.6`, `grok-4.5`) |
| `reasoning_effort` | `low` \| `medium` \| `high` \| `xhigh` → `--reasoning-effort` when set. The live model advertises the set (`xhigh` is grok-4.6 only) |
| `permission_mode` | Default **`default`**. Valid: `default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`. Process-wide / launch-scoped |
| `sandbox` | OS-level sandbox profile (`--sandbox`): `off`, `workspace`, `devbox`, `read-only`, `strict`, or a custom name from `~/.grok/sandbox.toml` |
| `allowed_tools` / `disallowed_tools` | Whitelist or blacklist built-in tools |
| `allow_rules` / `deny_rules` | Persistent permission allow / deny rules |
| `no_subagents` | Disables subagent spawning when true |
| `disable_web_search` | Disables built-in web search when true |
| `fs_roots` | Confine `fs/read_text_file` / `fs/write_text_file` to these roots (+ session cwd). Empty = unrestricted. Defense-in-depth, not a sandbox |
| `auth_method_id` | ACP auth method to invoke automatically if the agent reports it needs authentication |
| `mcp_servers` | Extra MCP tools/context (config-file only) |
| `prewarm` | Default `false` — keep one spare initialized agent process; off until the phone or config turns it on (MADR 0089 D5) |
| `stream_coalesce_ms` | Default `80` |

Some tool allow/deny flags are **measured no-ops for remote sessions** — see
notes in [docs/config.md](docs/config.md). Prefer `permission_mode`, session
modes, or `sandbox` for real policy.

---

## Provider: Goose

Goose (block.github.io) is driven through one shared `goose serve` engine using
ACP over WebSocket. Pick it per session from the phone's provider menu. No
per-session process — the engine handles all sessions.

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
| `prewarm` | Default `false` — starts `goose serve` on first use; set `true` to boot at daemon start |
| `turn_stall_notice_seconds` | Notice when a running turn produces no output (0 = off) |
| `stream_coalesce_ms` | Hold streamed text (default 80); 0 = one per token; max 1000 |
| `auth_method_id` | ACP auth method (advertised at initialize; session/new works without it) |
| `with_builtins` | Named Goose built-ins to enable on the shared engine (typed list, not free-form argv) |
| `mcp_servers` | Extra MCP tools/context (config-file only) |

Design: [docs/0025-MADR-goose-provider.md](docs/0025-MADR-goose-provider.md).

---

## Provider: OpenCode

OpenCode (opencode.ai) is driven through one shared long-lived `opencode serve`
engine — every session multiplexes over it. Pick it per session from the phone's
provider menu.

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"opencode", "name":"task", "cwd":"/path/to/repo" } }
```

### Session tree (MADR 0020 KD11)

When `providers.opencode.session_tree` is `true` (default), OpenCode supports
multi-agent session trees: child aliases, tree-idle EndTurn, and child fan-in.
When `false`, only the parent session is active (pre-0020 kill switch). Requires
OpenCode ≥ 1.18.0 when enabled.

### Model pinning

The OpenCode default model can be unreliable. Pin a model explicitly:

```yaml
providers:
  opencode:
    model: "opencode/deepseek-v4-flash-free"   # free, ~1s short prompts
    # or: "anthropic/claude-haiku-4-5"          # faster chat with paid keys
    pure: false   # true → opencode serve --pure (no third-party plugins)
```

| Setting | Default | Notes |
|---------|---------|-------|
| `prewarm` | `false` | Boot shared engine at daemon start; off saves ~250MB idle, ~3–5s first session (MADR 0089 D5) |
| `session_tree` | `true` | Multi-agent demux |
| `pure` | `false` | `--pure` on `opencode serve` |
| `stream_coalesce_ms` | `80` | Mid-stream text coalescing |

---

## Provider: Codex

Codex is driven through one shared `codex app-server --listen stdio://` engine —
JSON-RPC over stdio, every session multiplexed over the same process. Requires
`codex` on `PATH`; the daemon logs a warning at startup if the provider is
enabled and the binary is missing.

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
| `permission_timeout_seconds` | `900` | How long a remote permission request waits. Longer than other providers on purpose. `0` waits forever |
| `prewarm` | `false` | Boot the shared app-server at daemon start (~500ms cold start otherwise) |
| `turn_stall_notice_seconds` | `120` | Notice when a running turn produces no output (0 = off) |
| `stream_coalesce_ms` | `80` | Hold streamed text; 0 = one per token; max 1000 |
| `approval_policy` | *(empty)* | Override: `untrusted`, `on-request`, `never`. Empty with empty sandbox seeds mcremote default mode (`on-request` + `workspace-write`, MADR 0047) |
| `sandbox_mode` | *(empty)* | Override: `read-only`, `workspace-write`, `danger-full-access` |
| `allow_full_access` | `false` | Advertise the `full-access` session mode (no approval **and** no sandbox). Opt-in ([MADR 0044](docs/0044-MADR-auto-approve-modes.md) D5) |

Some Codex slash commands have no app-server equivalent and report that rather
than failing silently — `/deep-research`, `/workflow`, and similar.

**Ubuntu 24.04+ note:** Codex sandboxes need unprivileged user namespaces. If
kernel policy blocks them, sandboxed tools fail and only `danger-full-access` /
full-access mode works — see [docs/config.md](docs/config.md) and
[MADR 0048](docs/0048-MADR-codex-sandbox-namespace.md).

**macOS note:** in `auto`/default modes Codex enforces `workspace-write` with
an Apple Seatbelt sandbox — operations outside the session cwd fail with
`operation not permitted`. That error is the sandbox, not a macOS privacy
(TCC) denial; the escape is the full-access mode, which requires
`allow_full_access: true`. Configs provisioned by `setup-service` before
MADR 0069 omitted the key entirely, hiding the mode on exactly the hosts
that needed the explanation — see
[MADR 0069](docs/0069-MADR-macos-permissions-and-sandbox-parity.md).
The *other* macOS "operation not permitted" — privacy protection (TCC)
on Documents/Desktop/Downloads — is a separate layer: diagnose with
`mcremote doctor` and see
[docs/ops-macos-tcc.md](docs/ops-macos-tcc.md), including how to keep a
Full Disk Access grant across upgrades with
`make install MC_CODESIGN_IDENTITY=…`.

Design: [MADR 0028](docs/0028-MADR-codex-provider.md),
[0035](docs/0035-MADR-codex-ui-ux-remediation.md),
[0047](docs/0047-MADR-codex-default-mode.md),
[0048](docs/0048-MADR-codex-sandbox-namespace.md).

---

## Provider: Kilo

Kilo CLI (kilo.ai, an OpenCode fork with its own Gateway, agents, and model
aliases) is driven through one shared `kilo serve` engine — HTTP + SSE, same
architecture as OpenCode but a distinct dialect (MADR 0075). **Disabled by
default** (`providers.kilo.enabled: false`) until acceptance; enable per host.

Install: `npm i -g @kilocode/cli` or `brew install Kilo-Org/tap/kilo`.
Known-good CLI: **kilo 7.4.20** (wire shapes live-tested via `-tags live_kilo`).

```json
{ "v":1, "type":"session.create", "id":"2",
  "payload": { "provider":"kilo", "name":"task", "cwd":"/path/to/repo" } }
```

Agents: `code` (default), `ask`, `debug`, `plan`, `orchestrator` — Kilo has no
`build`. Model ids are `providerID/modelID` split on the **first** slash (Kilo
model ids may contain slashes and `~vendor` aliases): `kilo/kilo-auto/free`,
`openrouter/openrouter/free`, `kilo/~anthropic/claude-sonnet-4-5`. The engine
default is Gateway-auth-state-dependent (`kilo-auto/free` logged out,
`kilo-auto/balanced` with a Gateway session), so leaving `model` empty follows
whatever the host's `kilo auth` state provides.

Credentials are **host-side only** (`kilo auth login`, Gateway session, or env
keys such as `OPENROUTER_API_KEY`) until the MADR 0074 phone-auth phases land.
`session_tree` stays `false` until child-event fixtures prove tree demux on
this fork (MADR 0075 Q7).

Design: [docs/0075-MADR-kilo-cli-provider.md](docs/0075-MADR-kilo-cli-provider.md).

---

## Session modes, models, and thinking

- **Session modes** (`session.set_mode` / `/mode` / `/plan`): provider-specific
  ids (`default`/`plan` on grok; agent names like `build`/`plan` on OpenCode;
  Codex pairs approval+sandbox). Modes marked `dangerous: true` arm auto-approve
  for that session ([MADR 0044](docs/0044-MADR-auto-approve-modes.md)).
- **Model catalogs** (`models.list`): scope `models` or `providers` (model
  *providers* such as anthropic/openai, distinct from agent CLI providers).
  Options may advertise `thinking_levels`.
- **Thinking / reasoning** (`thinking_level` on create, `/thinking`): next-turn
  on Codex; spawn-scoped on Grok; absent for OpenCode/Goose when unsupported
  ([MADR 0052](docs/0052-MADR-thinking-levels-and-settings.md)).

---

## `mcremote engines` — engine lifecycle

`goose serve`, `opencode serve`, `kilo serve`, and `codex app-server` engines
are shared processes spawned by the daemon. Use `mcremote engines` to inspect
them:

```bash
# List engine processes and whether their owning daemon is alive
mcremote engines

# Stop engines whose daemon is gone (the daemon also does this at startup)
mcremote engines --reap
```

Only processes carrying mcremote's ownership marker are ever listed or stopped —
an `opencode serve` you started by hand is never touched. The daemon also reaps
orphaned engines at startup, skipping any engine owned by another live mcremote.

Ownership is tracked through the on-disk **engine registry**
(`mcremote paths` → `engine_registry`), which is the cross-platform contract.
Linux additionally carries environment markers and `PR_SET_PDEATHSIG` as
defense in depth, neither of which macOS provides
([MADR 0059](docs/0059-MADR-native-paths-and-linux-macos-parity.md) D8).

---

## Signed receipts

Opt-in (`receipts.enabled: false` by default): a durable, device-signed,
hash-chained record of a human's permission decision on a paired phone —
"which device approved this, and can I prove it wasn't tampered with after
the fact." Matching decisions (`receipts.allow_patterns`/`deny_patterns`,
shell-glob syntax) get a JWS-signed
[in-toto-style Statement](docs/receipts.md#the-statement-shape) appended to
`<data_dir>/receipts/<device_id>.jsonl`.

```bash
mcremote receipts list                                    # every device's chain
mcremote receipts verify --device dev_abc123               # full integrity check
mcremote receipts show --device dev_abc123 --permission per_a1b2
```

**On the phone:** when a daemon keeps receipts, Settings → **Signed receipts**
lists this device's own chain, each entry's signature re-verified *on device*
(✓/✗/⚠) — never a daemon-asserted verdict (MADR 0078 D9).

### Session handoff

A session is owned by the device that created it; other devices can't see or
drive it. To move one to another device, the owner **releases** it and the
other device **claims** it.

**On the phone:** a session's ⋮ menu has **Hand off…** — pick another paired
device (from the fleet roster) to target it, or release openly for any device
to claim. On the receiving device the session appears as **Released ·
claimable** with a **Claim** action that takes ownership. Under the hood these
are the `session.release` / `session.claim` protocol verbs (targeted release
carries `to_device_id`; an open release omits it), with the phone reading the
`devices.list` roster to offer targets.

A handoff is the minimal, safe relaxation of single-ownership: release returns
the session to the unowned state the system already handles everywhere, and
claim is the existing first-touch ownership path, optionally gated to one
target device. When receipts are enabled (`receipts.handoffs: true`, the
default), each handoff records **two** signed receipts — the releaser signs a
`session-handoff-release` into its chain, the claimer a `session-handoff-claim`
into its — linked by a shared handoff subject so an auditor can tie the two
halves together across the two devices' separate chains.

**Design and complete reference:** [docs/receipts.md](docs/receipts.md) ·
[MADR 0077](docs/0077-MADR-signed-receipts-permission-handoffs.md) ·
[MADR 0078](docs/0078-MADR-session-handoff-and-receipt-surfacing.md).

---

## mcrelay (public join-plane edge)

Outbound join router for phones that cannot reach mcremote on the mesh
([MADR 0015](docs/0015-MADR-mcrelay-transport-security.md)). Opaque WebSocket
splice with end-to-end TLS to mcremote — mcrelay does not authenticate devices,
run agents, or see protocol-v1 plaintext on the inner hop.

**Complete config, flags, env, TLS, and limits:**
**[docs/config-mcrelay.md](docs/config-mcrelay.md)**. Ops runbook:
[docs/ops-mcrelay.md](docs/ops-mcrelay.md).

```bash
make build-relay
make install-relay                    # → ~/.local/bin/mcrelay
# or: make install   (installs both binaries)
mkdir -p ~/.config/mcrelay
cp configs/mcrelay.example.yaml ~/.config/mcrelay/config.yaml
chmod 600 ~/.config/mcrelay/config.yaml
# edit hosts + TLS (or use MCRELAY_HOSTS for secrets)
mcrelay setup-service --force --service-config ~/.config/mcrelay/config.yaml
systemctl --user status mcrelay   # Linux
```

### mcrelay TLS / Let's Encrypt

`tls.mode` auto-select: domains+email → `letsencrypt`; cert+key files → `files`;
else `off`. Explicit: `letsencrypt` | `files` | `off`.

**mcrelay supports both ACME challenges** for `tls.mode: letsencrypt`
(`tls.letsencrypt.challenge` / `--tls-acme-challenge` / `MCRELAY_TLS_ACME_CHALLENGE`):

| Challenge | Default? | When to use | Requirements |
|-----------|----------|-------------|--------------|
| **`http-01`** | **Yes** | Public edge with free port 80 | Domain A/AAAA points at this host; CA can `GET /.well-known/acme-challenge/` (port **80**, or `tls.letsencrypt.http_port`) |
| **`dns-01`** | No | Port 80 blocked, multi-homed, or same Route 53 path as mcremote | Route 53 zone + ambient AWS credentials (`tls.letsencrypt.route53.*`) |

This is **not** the same as mcremote: mcremote is **DNS-01 only** (mesh-only
hosts). mcrelay is a public edge, so HTTP-01 is the default and DNS-01 is fully
supported as well. Full matrix, examples, and flags:
[docs/config-mcrelay.md § TLS (outer edge)](docs/config-mcrelay.md#tls-outer-edge) and
[§ ACME challenge selection](docs/config-mcrelay.md#acme-challenge-selection).

```bash
# HTTP-01 (default) — public VPS, port 80 free
mcrelay serve --tls-mode letsencrypt --tls-acme-challenge http-01 \
  --tls-domain relay.example.com --tls-email ops@example.com \
  --listen-port 443 --allow 'devbox-1:your-long-registration-secret'

# DNS-01 — no port 80; same Route 53 path as mcremote
mcrelay serve --tls-mode letsencrypt --tls-acme-challenge dns-01 \
  --tls-domain relay.example.com --tls-email ops@example.com \
  --tls-route53-zone-id Z0123456789ABCDEFGHIJ \
  --allow 'devbox-1:your-long-registration-secret'
```

### mcrelay CLI reference

```text
mcrelay [--config PATH] [--log-level LEVEL] [--log-format FORMAT] [--setup-service] [--version]
mcrelay serve [--listen-host HOST] [--listen-port PORT] [--data-dir DIR]
               [--tls-mode letsencrypt|files|off]
               [--tls-domain NAME] [--tls-email ADDR]
               [--tls-acme-challenge http-01|dns-01] [--tls-acme-http-port N]
               [--tls-acme-staging] [--tls-route53-zone-id ID] …
               [--tls-cert PATH] [--tls-key PATH]
               [--allow HOST_ID:SECRET] [--allow-legacy-tunnel-secret]
               [--trusted-proxy CIDR] ...
mcrelay paths [--json] [--data-dir DIR]
mcrelay setup-service | mcrelay version | mcrelay completion …
```

| Command | Purpose |
|---------|---------|
| `serve` | Run the relay daemon |
| `paths` | Print resolved XDG layout (read-only) |
| `setup-service` | Install background service (Linux systemd / macOS launchd) |
| `version` | Print version |
| `completion` | Shell completion scripts |

Precedence: CLI flags > `MCRELAY_*` env > config.yaml > defaults.
See **[docs/config-mcrelay.md](docs/config-mcrelay.md)** for every key.

### Key limits (config file / env only — no CLI flags)

| Setting | Default | Max |
|---------|---------|-----|
| `limits.max_hosts` | 32 | 1024 |
| `limits.max_phones_per_host` | 8 | 256 |
| `limits.max_message_bytes` | 1048576 (1 MiB) | 16777216 (16 MiB) |
| `limits.max_concurrent_join` | 64 | 4096 |
| All per-minute rate limits | varies | 100000/min |
| Duration fields | varies | 604800 (7 days) |

| Artifact | Path |
|----------|------|
| **Config / flags / env (source of truth)** | [docs/config-mcrelay.md](docs/config-mcrelay.md) |
| Example config (all keys) | [configs/mcrelay.example.yaml](configs/mcrelay.example.yaml) |
| setup-service default | [internal/cli/service/defaults_mcrelay.yaml](internal/cli/service/defaults_mcrelay.yaml) |
| User unit (all env commented) | [deploy/systemd/mcrelay.user.service](deploy/systemd/mcrelay.user.service) |
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

Unit options (user template): `Restart=always`, `TimeoutStopSec=45`,
`KillMode=control-group`, XDG env, `NoNewPrivileges` / `PrivateTmp` /
`RestrictSUIDSGID` / `ProtectKernelTunables` / `ProtectControlGroups` /
`SystemCallArchitectures=native` / `LimitNOFILE=65536`. Full table:
[docs/config.md](docs/config.md).

Useful after setup:

```bash
systemctl --user status mcremote
systemctl --user restart mcremote
journalctl --user -u mcremote -f
systemctl --user disable --now mcremote
```

---

## Mobile companion (Magic CLI Remote)

Flutter app lives in [`apps/mobile`](apps/mobile).

| Platform | Status |
|----------|--------|
| **Android** | Shipped target — the release APK is the GitHub Release artifact (tag builds only) |
| **iOS** | Software-complete dev target (MADR 0067): builds and runs on the simulator, full test suite green. **Hardware validation is parked** — no physical iPhone to validate on; not part of CI and not a release artifact yet |
| **Linux desktop** | Dev convenience target — same UI against a localhost daemon, no emulator needed |

```bash
cd apps/mobile
flutter pub get
flutter run                 # pick a device
flutter run -d linux        # desktop, no emulator required
open ios/Runner.xcworkspace  # iOS: build/run from Xcode against a simulator
```

Use host `10.0.2.2:7531` from the Android emulator (the daemon must listen on
`0.0.0.0` — an explicit dev-only opt-out of the `tailscale` default). Full app
docs: [apps/mobile/README.md](apps/mobile/README.md).

**Product capabilities (current):**

- Pairing: 8-char code, QR (`mcremote://pair…`), long-lived token
- TLS modes: pin-only (self-signed), chain-or-pin (Let's Encrypt), cleartext
  rejected on Android
- Provider picker: grok / opencode / goose / codex / kilo / fake (as advertised ready)
- Model catalog + model-provider scope + thinking levels
- Session modes (including dangerous/auto-approve where the daemon offers them)
- Live chat: thoughts, tools, assistant text, permissions, questions
- Transcript persistence: daemon history ring + bounded phone cache
- Foreground resume / connection path selection (mesh, relay, LAN)
- Settings, notifications, secure storage (with upgrade-resilience path)

Local APK (debug-signed for sideload):

```bash
./scripts/build-apk.sh
# or: make apk
```

Release signing for CI/canonical tags:
[docs/ops-android-signing.md](docs/ops-android-signing.md).

---

## CI / CD (GitHub Actions)

Workflows live under [`.github/workflows/`](.github/workflows/):

A single workflow, `ci.yml`, runs on push to `master`, pull request, tag `v*`,
or manual dispatch:

| Job | When | What it does |
|-----|------|--------------|
| `go` | always | gofmt, `go mod tidy` cleanliness, vet, race tests, version-allocator tests, systemd unit validation, build-tag policy. **On tag:** builds **mcremote** + **mcrelay** for **linux/amd64, darwin/arm64, darwin/amd64** and uploads them |
| `flutter` | always | `dart format` check, Flutter analyze, Flutter test (Flutter **3.44.6** pinned) |
| `android-apk` | **tag only** | arm64 **release** APK (signing rules in workflow + ops-android-signing); asserted by `scripts/assert-flutter-release-apk.sh`. PRs/branches: build with `make apk` locally |
| `release` | tag only | Downloads the APK and Go binaries and attaches them to the GitHub Release |

**Build tags are per-OS and CI enforces it:** Linux binaries build with
`netgo,osusergo` (pure-Go DNS and passwd resolution). Darwin builds carry **no**
tags (`CGO_ENABLED=0` already gets there). `make verify-build-metadata` checks
both.

**Node.js:** CI pins **Node.js 24 LTS** via `actions/setup-node`, `.node-version`,
and `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

Download binaries/APK from Actions **Artifacts**, or from a Release after tagging:

```bash
git tag v0.7.1
git push origin v0.7.1
```

### Running a downloaded macOS binary

Go ad-hoc signs every `darwin/arm64` binary at link time, including CI
cross-compiles, so a released binary satisfies the Apple Silicon loader with no
Developer ID and no notarization. Gatekeeper only intervenes when the file
carries a quarantine attribute, which a **browser** download sets:

```bash
xattr -d com.apple.quarantine ./mcremote-darwin-arm64-*
chmod +x ./mcremote-darwin-arm64-*
```

`gh release download` does **not** set the attribute, so the step is unnecessary
when fetching through the CLI — and it never applies to `make install`, which
produces a local file that was never quarantined. Rationale:
[docs/0060-MADR-local-unsigned-build-and-install.md](docs/0060-MADR-local-unsigned-build-and-install.md).

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
make fmt           # gofmt (not gofumpt)
make lint          # golangci-lint
make staticcheck
make vulncheck     # govulncheck
make tidy
make test-all      # race suite + Flutter tests
cd apps/mobile && flutter test

# Pre-push gate: mirrors the go + flutter CI jobs before you push
make preflight

# Verifications CI also runs
make verify-build-metadata   # per-OS build tags (Linux netgo,osusergo / Darwin none)
make verify-units            # systemd unit directives
make pre-add-check           # gofmt + golint + govulncheck (the staging rule)

# Live provider suites (require the real CLI on PATH + network; not in CI)
make live-opencode
# → go test -tags live_opencode ./internal/provider/opencode/
make live-codex
# → go test -tags live_codex ./internal/provider/codex/
# Additional live tags (no make shorthand yet):
#   go test -tags live_grok  ./internal/provider/grok/  -count=1 -timeout 600s -v
#   go test -tags live_goose ./internal/provider/goose/ -count=1 -timeout 600s -v
#   go test -tags live_kilo  ./internal/provider/kilo/  -count=1 -timeout 600s -v

# Android runtime profiling (physical device / emulator + DevTools)
make profile-devices
make profile                 # flutter run --profile
make profile-apk             # arm64 profile APK
# → docs/mobile-profiling.md
```

### Repository layout

```text
cmd/mcremote, cmd/mcrelay   # binaries
internal/
  cli/          # cobra commands, setup-service templates
  config/       # YAML + env + defaults
  daemon/       # serve lifecycle, TLS ensure
  ws/           # protocol-v1 WebSocket server
  session/      # session manager
  provider/     # grok, goose, opencode, codex, kilo, fake, ACP helpers
  auth/         # devices, pair codes, tokens
  relay/        # mcrelay server
  relayhost/    # mcremote → mcrelay client
  appdirs/      # XDG paths
  admin/        # local Unix control socket
  …
apps/mobile/                # Flutter companion
configs/                    # example YAML
deploy/                     # systemd + launchd unit examples
docs/                       # MADRs, plans, ops, protocol, config
scripts/                    # build, install, smoke, precheck, hooks
```

### Code standards

See [AGENTS.md](AGENTS.md) for the **pre-add rule** (`gofmt` + `golint` +
`govulncheck` before staging Go), Dart format, and agent conventions.
Language/style guides live under `docs/standards/`.

---

## Documentation

### Operator / product

| Doc | Description |
|-----|-------------|
| [docs/protocol-v1.md](docs/protocol-v1.md) | WebSocket JSON schema (source of truth for the wire) |
| [docs/protocol-v2.md](docs/protocol-v2.md) | v1 delta: negotiation, resume, gap signalling (shipped) |
| [docs/config.md](docs/config.md) | mcremote config, flags, and env reference |
| [docs/receipts.md](docs/receipts.md) | Signed permission-decision receipts: Statement shape, `predicateType` registry, CLI reference |
| [docs/config-mcrelay.md](docs/config-mcrelay.md) | mcrelay config, flags, env, setup-service |
| [docs/ops-mcrelay.md](docs/ops-mcrelay.md) | mcrelay ops: systemd/launchd, LE, secret rotation, smoke |
| [docs/headscale.md](docs/headscale.md) | Mesh grants & pairing |
| [docs/iam-route53-acme.md](docs/iam-route53-acme.md) | Route 53 IAM for ACME DNS-01 |
| [docs/ops-android-signing.md](docs/ops-android-signing.md) | Release APK keystore / CI secrets |
| [apps/mobile/README.md](apps/mobile/README.md) | Flutter companion runbook |
| [docs/mobile-profiling.md](docs/mobile-profiling.md) | Android profile mode / DevTools |
| [docs/chat-performance.md](docs/chat-performance.md) | Mobile chat scroll/stream notes |

### Architecture & key decisions

| Doc | Description |
|-----|-------------|
| [docs/0001-MADR-architecture-mcremote.md](docs/0001-MADR-architecture-mcremote.md) | Architecture MADR |
| [docs/0003-MADR-phase1-decisions.md](docs/0003-MADR-phase1-decisions.md) | Phase 1 locked decisions |
| [docs/MADR-phase2-grok-acp.md](docs/MADR-phase2-grok-acp.md) | Phase 2 Grok ACP |
| [docs/0015-MADR-mcrelay-transport-security.md](docs/0015-MADR-mcrelay-transport-security.md) | mcrelay outbound relay (E2E TLS splice) |
| [docs/0020-MADR-opencode-session-tree.md](docs/0020-MADR-opencode-session-tree.md) | OpenCode multi-agent session tree |
| [docs/0023-MADR-canonical-slash-commands.md](docs/0023-MADR-canonical-slash-commands.md) | Canonical slash commands |
| [docs/0024-MADR-stream-coalescing.md](docs/0024-MADR-stream-coalescing.md) | Stream text coalescing |
| [docs/0025-MADR-goose-provider.md](docs/0025-MADR-goose-provider.md) | Goose provider |
| [docs/0028-MADR-codex-provider.md](docs/0028-MADR-codex-provider.md) | Codex provider |
| [docs/0044-MADR-auto-approve-modes.md](docs/0044-MADR-auto-approve-modes.md) | Auto-approve as session modes |
| [docs/0047-MADR-codex-default-mode.md](docs/0047-MADR-codex-default-mode.md) | Codex default session mode |
| [docs/0048-MADR-codex-sandbox-namespace.md](docs/0048-MADR-codex-sandbox-namespace.md) | Codex sandbox / user namespaces |
| [docs/0052-MADR-thinking-levels-and-settings.md](docs/0052-MADR-thinking-levels-and-settings.md) | Thinking levels |
| [docs/0058-MADR-macos-launchd-service-hardening.md](docs/0058-MADR-macos-launchd-service-hardening.md) | macOS launchd design (agent-only) |
| [docs/0059-MADR-native-paths-and-linux-macos-parity.md](docs/0059-MADR-native-paths-and-linux-macos-parity.md) | XDG paths + functional parity |
| [docs/0060-MADR-local-unsigned-build-and-install.md](docs/0060-MADR-local-unsigned-build-and-install.md) | Local install / ad-hoc sign |
| [docs/0061-MADR-relay-pair-advertise-and-path-selection.md](docs/0061-MADR-relay-pair-advertise-and-path-selection.md) | Relay pair advertise |
| [docs/0062-MADR-phone-transport-selection.md](docs/0062-MADR-phone-transport-selection.md) | Phone transport selection |
| [docs/0063-MADR-connection-liveness-truth.md](docs/0063-MADR-connection-liveness-truth.md) | Connection liveness |
| [docs/0065-MADR-update-automation.md](docs/0065-MADR-update-automation.md) | Update automation |
| [docs/0067-MADR-ios-port.md](docs/0067-MADR-ios-port.md) | iOS port of the mobile companion (software-complete; hardware validation parked) |
| [docs/0068-MADR-protocol-v2-reconnect-resilient-transport.md](docs/0068-MADR-protocol-v2-reconnect-resilient-transport.md) | Protocol v2: negotiation, liveness, resume, gap signalling (shipped) |
| [docs/0069-MADR-macos-permissions-and-sandbox-parity.md](docs/0069-MADR-macos-permissions-and-sandbox-parity.md) | macOS permissions / sandbox parity (TCC, full-access mode) |
| [docs/0074-MADR-remote-provider-auth-from-phone.md](docs/0074-MADR-remote-provider-auth-from-phone.md) | Remote provider auth from phone (proposed; not yet implemented) |
| [docs/0075-MADR-kilo-cli-provider.md](docs/0075-MADR-kilo-cli-provider.md) | Kilo CLI provider |
| [docs/0077-MADR-signed-receipts-permission-handoffs.md](docs/0077-MADR-signed-receipts-permission-handoffs.md) | Signed receipts for permission decisions |

Further numbered MADRs and plans live under [`docs/`](docs/)
(`NNNN-MADR-*.md` / `NNNN-PLAN-*.md`). Standards: [`docs/standards/`](docs/standards/).

---

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
