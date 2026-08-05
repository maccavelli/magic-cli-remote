# mcrelay operations (E4)

Operator runbook for the public-edge join router. Design: [0015](0015-MADR-mcrelay-transport-security.md).
Config reference: [config-mcrelay.md](config-mcrelay.md). Audit hardening: [0016](0016-MADR-mcrelay-audit-hardening.md).

## What mcrelay is (and is not)

| Does | Does not |
|------|----------|
| Accept host registration (`/v1/host`) with allowlist secret | Authenticate phone devices or client keys |
| Accept phone join by `host_id` (`/v1/phone`) | See protocol-v1 plaintext (opaque splice + inner TLS) |
| Splice phone ↔ host tunnel (`/v1/tunnel`) | Store history, tokens, or pair codes |
| Outer TLS (files or ACME HTTP-01) | Replace mesh/Tailscale when direct works |

Pair QR still points at **mcremote** identity (`fp` / `mode`); `relay` + `hid` only select the join path.

---

## 1. Install binary + config

```bash
# From repo
make build-relay
make install-relay   # → ~/.local/bin/mcrelay

mkdir -p ~/.config/mcrelay ~/.local/share/mcrelay
cp configs/mcrelay.example.yaml ~/.config/mcrelay/config.yaml
chmod 600 ~/.config/mcrelay/config.yaml
```

Templates (keep in sync with [config-mcrelay.md](config-mcrelay.md)):

| File | Role |
|------|------|
| [configs/mcrelay.example.yaml](../configs/mcrelay.example.yaml) | Annotated example (all keys + env/flag comments) |
| [internal/cli/service/defaults_mcrelay.yaml](../internal/cli/service/defaults_mcrelay.yaml) | Written by `setup-service` when config missing |
| [deploy/systemd/mcrelay.user.service](../deploy/systemd/mcrelay.user.service) | Manual unit; comments list every `MCRELAY_*` env |

Edit:

1. `hosts[].id` / `hosts[].secret` (or leave secrets only in env — preferred)
2. `tls.letsencrypt.domains` + `email`, or `tls.mode: files` PEMs
3. `listen.port` — `443` for production HTTP-01 + public WSS
4. Optional: `trusted_proxies` if behind a reverse proxy; `allow_legacy_tunnel_secret` only for old hosts

Generate a registration secret:

```bash
openssl rand -base64 32
```

Prefer runtime secrets (unit `Environment=` or drop-in; see commented lines in the unit file):

```bash
export MCRELAY_HOSTS='devbox-1:long-random-registration-secret'
# optional edge knobs:
# export MCRELAY_TRUSTED_PROXIES='127.0.0.1/32'
# export MCRELAY_ALLOW_LEGACY_TUNNEL_SECRET=false
# export MCRELAY_LIMITS_MAX_PHONES_PER_HOST=8
```

---

## 2. Background service (systemd / launchd)

Preferred:

```bash
mcrelay setup-service --force --service-config ~/.config/mcrelay/config.yaml
# or
mcrelay --setup-service
```

**Linux (systemd --user):**

```bash
systemctl --user status mcrelay
journalctl --user -u mcrelay -f
loginctl enable-linger "$USER"   # keep running after logout (default from setup-service)
```

Manual unit: [deploy/systemd/mcrelay.user.service](../deploy/systemd/mcrelay.user.service).

**macOS (launchd user LaunchAgent — no sudo; stops on logout):**

```bash
launchctl print "gui/$(id -u)/com.magiccliremote.mcrelay"
tail -f ~/Library/Logs/mcrelay/mcrelay.err.log
```

Manual plist: [deploy/launchd/com.magiccliremote.mcrelay.plist](../deploy/launchd/com.magiccliremote.mcrelay.plist).
See [0058-MADR](0058-MADR-macos-launchd-service-hardening.md).

### Low ports (80 / 443)

- **HTTP-01** ACME needs **port 80** reachable by the CA.
- **DNS-01** does not need port 80 (Route 53 only).
- Join plane is usually **443** either way.

Options when using HTTP-01:

```bash
# capability on the binary (re-apply after each install)
sudo setcap 'cap_net_bind_service=+ep' ~/.local/bin/mcrelay

# or reverse-proxy TLS termination (then mcrelay can listen on high ports;
# still present outer TLS or terminate carefully — see trust model in 0015)
# or set tls.letsencrypt.challenge: dns-01
```

Firewall: allow `443/tcp` (and `80/tcp` if using HTTP-01), or `8443` if not using standard ports.

---

## 3. Let's Encrypt (HTTP-01 or DNS-01)

Choose the ACME challenge with `tls.letsencrypt.challenge` (default **`http-01`**).

| Challenge | Use when |
|-----------|----------|
| `http-01` | Port **80** is free and public; simplest for a dedicated public edge |
| `dns-01` | Port 80 unavailable, or you already have Route 53 IAM for mcremote |

### HTTP-01 (default)

Staging first:

```yaml
listen:
  host: "0.0.0.0"
  port: 443
tls:
  mode: letsencrypt
  letsencrypt:
    domains:
      - relay.example.com
    email: ops@example.com
    challenge: http-01
    staging: true
    http_port: 0   # 0 = 80
```

```bash
# DNS A/AAAA for relay.example.com → this host
systemctl --user restart mcrelay
curl -fsS https://relay.example.com/healthz
# expect: {"ok":true}
```

Then set `staging: false` and restart. Certificates live under
`$XDG_DATA_HOME/mcrelay/acme` (or `tls.letsencrypt.cache_dir`).

**Note:** This is the **relay leaf** only. Phone pins / `mode` still target **mcremote** (inner hop).

If something else binds `:80`, ACME fails — free the port, switch to **DNS-01**
(below), or use a non-public ACME directory with a custom `http_port` (not for
production LE).

### DNS-01 (Route 53)

No public port 80 required. Uses the same certmagic DNS-01 path as mcremote
([iam-route53-acme.md](iam-route53-acme.md), [config.md](config.md#tls-modes)).

```yaml
tls:
  mode: letsencrypt
  letsencrypt:
    domains:
      - relay.example.com
    email: ops@example.com
    challenge: dns-01
    staging: true
    route53:
      hosted_zone_id: Z0123456789ABCDEFGHIJ
      region: us-east-1
```

Ensure the unit/process has AWS credentials (env, profile, or instance role).
Example drop-in:

```bash
mkdir -p ~/.config/systemd/user/mcrelay.service.d
cat > ~/.config/systemd/user/mcrelay.service.d/aws.conf <<'EOF'
[Service]
Environment=AWS_PROFILE=acme
# or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_REGION
EOF
systemctl --user daemon-reload
systemctl --user restart mcrelay
```

---

## 4. Wire mcremote to the relay

On each agent host (`~/.config/mcremote/config.yaml` or env):

```yaml
relay:
  url: "wss://relay.example.com"
  host_id: "devbox-1"
  registration_secret: "same-as-mcrelay-allowlist"  # never on the phone / QR
```

Restart mcremote. Logs should show registration with mcrelay. Pair URI emission includes `relay` + `hid` when configured.

Rotate registration secret without re-pairing phones:

1. Add new secret on **mcrelay** for the same `host_id` (or update the one entry)
2. Update **mcremote** `registration_secret`
3. Restart mcremote (re-registers)
4. Phones keep using the same `hid` + `relay` URL

Never put the registration secret in the pair QR.

---

## 5. Limits & stability knobs

| Key | Default | Purpose |
|-----|---------|---------|
| `limits.register_idle_seconds` | 30 | Host-control WebSocket ping interval (R5) |
| `limits.splice_idle_seconds` | 300 | End silent opaque splice (R15); `-1` disables |
| `limits.splice_max_seconds` | 43200 | Hard max splice life 12h (R15); `-1` disables |
| `limits.accept_per_minute` | 120 | Pre-auth upgrades per client IP |
| `limits.tunnel_wait_seconds` | 15 | Host must open tunnel after dial |
| `trusted_proxies` | *(empty)* | CIDRs of reverse proxies that may set `X-Forwarded-For` (E1); empty ignores XFF |

If nginx/Caddy terminates TLS in front of mcrelay, set `trusted_proxies` to the
proxy’s source address(es) so accept/join rate limits apply per real client.
Leave empty when phones/hosts dial mcrelay directly.

Shutdown (`SIGTERM` / unit stop) cancels live splices so the process exits cleanly (R17).

---

## 6. Health & logs

```bash
curl -fsS https://relay.example.com/healthz
systemctl --user status mcrelay
journalctl --user -u mcrelay -n 100 --no-pager
```

Useful log lines: `host registered`, `join ok`, `splice ended`, `register denied`, `join denied`, `join timeout`, `phone slot divergence corrected` (0068 P6 sweep — the symptom self-heals in ≤60 s, but a recurring line means a release-pairing bug worth reporting).

### Goroutine-leak triage (debug builds only, 0068 P6)

Release binaries contain no profiling surface. To chase a suspected goroutine
leak in mcrelay **or** mcremote:

```bash
make debug            # builds bin/ with -tags debugpprof + GOEXPERIMENT=goroutineleakprofile
MC_DEBUG_ADDR=127.0.0.1:6060 bin/mcrelay serve …   # same env works for mcremote serve
go tool pprof http://127.0.0.1:6060/debug/pprof/goroutineleak
curl -s http://127.0.0.1:6060/debug/pprof/goroutine?debug=2 | head -100   # classic dump
```

`MC_DEBUG_ADDR` must be loopback — the listener refuses anything else, because
pprof output includes memory contents. Unset means off even in a debug build.
The `goroutineleak` profile (Go 1.26) reports goroutines the runtime has proven
can never be unblocked; the plain `goroutine` profile shows everything.

---

## 7. Phase E smoke checklist (manual)

Use this for **E.3 exit** (device off-mesh). Automated CI covers join-plane e2e (`go test ./internal/relay/...`); full chat needs a live mcremote + phone.

### Automated (CI / local)

```bash
go test ./internal/relay/... ./internal/relayhost/... ./internal/certs/...
go test -race ./internal/relay/... ./internal/relayhost/...
cd apps/mobile && flutter test test/relay_transport_test.dart test/relay_path_test.dart
```

### Manual off-mesh path

| # | Step | Pass? |
|---|------|-------|
| 1 | mcrelay up with TLS; `/healthz` OK | |
| 2 | mcremote registers (`registered with mcrelay` in logs) | |
| 3 | Phone **not** on mesh; pair / connect uses relay path | |
| 4 | Auth (device token + client key) succeeds to mcremote | |
| 5 | Create session, send prompt, receive stream | |
| 6 | Permission request + approve (if tools enabled) | |
| 7 | History / `models.list` picker works | |
| 8 | Disable relay on phone or kill mcrelay → mesh-direct still works when reachable | |
| 9 | Stop mcremote or revoke host → live relay path ends | |
| 10 | Wrong registration secret cannot register; join alone cannot mint sessions | |

### Security review notes

- Compromised **relay** credentials ≠ mcremote session mint (device token + client key still required on inner hop).
- Evil outer splice cannot inject valid protocol-v1 under inner TLS (integrity fails).
- Outer-only connection without inner auth never reaches agent actions.

---

## 8. Troubleshooting

| Symptom | Check |
|---------|--------|
| Host never registers | URL scheme `wss://`, secret match, outbound 443 from host, mcrelay logs `register denied` |
| Join `host_offline` | Host control dropped (NAT idle) — confirm R5 pings; firewall; mcremote still running |
| Join `timeout` | Host cannot open `/v1/tunnel` (TLS, URL, secret) |
| ACME fails | Port 80 free, DNS correct, staging vs prod rate limits |
| Phone stuck after join | Inner TLS pin/`mode` wrong host; mobile outer buffer (0016 R6) |
| Capacity / `limit` forever | Fixed in 0016 P1–P2; restart still clears if on old build |

---

## 9. Related

- [config-mcrelay.md](config-mcrelay.md) — full flags / env
- [iam-route53-acme.md](iam-route53-acme.md) — Route 53 IAM for DNS-01
- [config.md](config.md#tls-modes) — mcremote TLS modes (DNS-01 only; recovery pin)
- [0009-MADR-post-hardening-action-plan.md](0009-MADR-post-hardening-action-plan.md) Phase E
- [0016-MADR-mcrelay-audit-hardening.md](0016-MADR-mcrelay-audit-hardening.md) backlog R10+
