# Headscale setup for mcremote (Phase 1)

Phase 1 uses **Headscale/Tailscale only** (no vendor phone-home required for the app path). The phone and the host running `mcremote` must be on the **same tailnet**.

**Important:** Headscale does **not** reverse-proxy or “forward” to mcremote. It only coordinates the mesh (login, keys, IPs, policy). After join, the phone opens **TCP 7531 on the host’s tailnet IP** (WireGuard), not on Headscale’s HTTPS port.

```text
Phone ──HTTPS :443──► Headscale (control plane only)
  │
  └── WireGuard (direct and/or DERP) ──► host 100.x.y.z:7531 (mcremote)
```

Prefer **not** exposing public TCP 7531. Expose Headscale HTTPS (and LE HTTP-01) on the internet; keep mcremote mesh-only.

---

## Production: AWS host + phone over the public internet

This is the path when the control server is an EC2 (or similar) host and the phone is not on the same LAN.

### Target names (this project’s AWS utility box)

| Item | Value |
|------|--------|
| Public IPv4 | `52.2.52.22` (verify with `curl -4 ifconfig.me`) |
| Headscale FQDN | `headscale.lallygag.net` |
| MagicDNS base domain | `ts.lallygag.net` (must **differ** from the Headscale hostname’s zone usage; see config) |
| ACME email | set in `acme_email` (e.g. operator Gmail) |
| mcremote port | **7531** (tailnet only) |

### 1. DNS

Create:

```text
headscale.lallygag.net.  IN  A  52.2.52.22
```

Optional (not required for control plane): records under MagicDNS base are managed by the tailnet, not public DNS.

Wait until the name resolves from the public internet (phone LTE DNS or an external checker) to **exactly** this host’s public IP.

**Split-horizon / VPC DNS:** On this AWS host, the VPC resolver (`10.10.0.2`) serves a private view of `lallygag.net` that may **not** include `headscale` even when public DNS does. That does **not** block Let’s Encrypt (validators use public DNS). For same-host tools (`curl`, `tailscale up`), either:

```bash
# one-line local override (already used on the utility box)
echo '127.0.0.1 headscale.lallygag.net' | sudo tee -a /etc/hosts
```

or add the same A record to the **private** Route 53 hosted zone so VPC DNS matches public.

### 2. AWS security group / network ACL

Inbound from the internet (phone will come from changing carrier IPs):

| Port | Proto | Purpose |
|------|-------|---------|
| **80** | TCP | Let’s Encrypt HTTP-01 |
| **443** | TCP | Headscale HTTPS (clients) |
| **41641** | UDP | Tailscale WireGuard direct path (recommended) |

Do **not** open **7531** publicly if clients are on the mesh.

Outbound: allow normal HTTPS (ACME, DERP map, updates).

Local host firewall is currently inactive on the utility box; if you enable `ufw` later, mirror the same ports.

### 3. Headscale TLS (built-in Let’s Encrypt)

Headscale v0.29+ can obtain and renew certs without certbot. After DNS + SG are ready, set roughly:

```yaml
server_url: https://headscale.lallygag.net
listen_addr: 0.0.0.0:8080

acme_url: https://acme-v02.api.letsencrypt.org/directory
acme_email: you@example.com
tls_letsencrypt_hostname: headscale.lallygag.net
tls_letsencrypt_cache_dir: /var/lib/headscale/cache
tls_letsencrypt_challenge_type: HTTP-01
tls_letsencrypt_listen: ":http"

policy:
  mode: file
  path: /etc/headscale/acl.hujson

dns:
  magic_dns: true
  # Must not collide with how you think about server_url; use a dedicated tailnet zone.
  base_domain: ts.lallygag.net
  override_local_dns: false   # safer on multi-use servers; set true only if you want full MagicDNS push
```

Keep metrics/gRPC on localhost unless you intentionally expose them.

DERP: leaving Tailscale’s public DERP map URLs enabled is fine for phone bring-up when direct WireGuard fails. Embedded DERP can be enabled later (requires working HTTPS).

Restart:

```bash
sudo systemctl restart headscale
sudo systemctl status headscale --no-pager
sudo journalctl -u headscale -n 80 --no-pager
```

Sanity:

```bash
curl -sSI https://headscale.lallygag.net/ | head
# expect HTTP/2 or HTTP/1.1 200/404/etc from headscale, with a valid LE cert
```

### 4. Policy file

Example bring-up policy (`/etc/headscale/acl.hujson`) — Headscale **v0.29** rejected `autogroup:admin` as a `tagOwners` value; use a simple open ACL first, then tighten with real usernames/groups:

```hujson
{
  "acls": [
    {
      "action": "accept",
      "src": ["*"],
      "dst": ["*:*"],
    },
  ],
}
```

Reload after edits (`systemctl reload headscale` or restart).

When creating keys, v0.29 wants **user ID**, not username:

```bash
sudo headscale users list
sudo headscale preauthkeys create -u 1 --reusable --expiration 48h
```

### 5. User + preauth keys

```bash
sudo headscale users create mac
sudo headscale preauthkeys create --user mac --reusable --expiration 48h
# Prefer tagged keys when your Headscale version supports --tags:
#   --tags tag:mcremote-host
#   --tags tag:mcremote-client
```

Use `headscale preauthkeys create --help` for exact flags on v0.29.

### 6. Join the **host** (this machine)

Install Tailscale client if missing, then:

```bash
sudo tailscale up \
  --login-server=https://headscale.lallygag.net \
  --authkey='<host-preauth-key>' \
  --advertise-tags=tag:mcremote-host \
  --accept-dns=false
```

On the same box as Headscale, `http://127.0.0.1:8080` can work for join **only if** Headscale still serves plain HTTP locally; after pure HTTPS `server_url`, use the public `https://…` URL (or the documented local socket/CLI paths).

```bash
tailscale status
tailscale ip -4    # e.g. 100.64.0.1 — phone will use this for mcremote
```

### 7. Join the **phone**

1. Install **Tailscale** from the Play Store / App Store.  
2. Open the app → account menu → **Use custom coordination server** / Headscale.  
3. Server URL: `https://headscale.lallygag.net`  
4. Authenticate with the **phone** preauth key (or interactive login if you enable that).  
5. Confirm the phone appears in `sudo headscale nodes list`.

### 8. Run mcremote (mesh side)

```bash
# Grok-only example — see configs/config.prod.example.yaml
mcremote serve --listen-host 0.0.0.0 --listen-port 7531
# or bind only the tailnet IP:
# mcremote serve --listen-host "$(tailscale ip -4)" --listen-port 7531
```

Pair and connect:

```bash
mcremote pair create --name phone
# paste mcr_… into the Flutter app once
```

App host field:

```text
<host-tailscale-ip>:7531
# or MagicDNS name, e.g. awsutility.ts.lallygag.net:7531
```

WebSocket path: `/v1/ws` (see [protocol-v1.md](./protocol-v1.md)).

---

## Local-only / lab control plane

For same-machine smoke tests only:

```text
http://127.0.0.1:8080
```

Phones on the public internet **cannot** use `localhost` as `server_url`.

## Tags

Recommend advertising:

- Host (daemon machine): `tag:mcremote-host`
- Phone / client: `tag:mcremote-client`

## Bind guidance (mcremote)

| Mode | `listen.host` | Notes |
|------|---------------|--------|
| Local only | `127.0.0.1` | Default; phone cannot connect |
| Mesh | tailnet IP or `0.0.0.0` | Prefer grants so only tailnet peers matter; keep **7531** off public SG |

Always keep `auth.require_device_token: true` in production.

## Pairing checklist

1. On the host: `mcremote pair create --name phone`  
2. Copy the `mcr_…` token into the Flutter client — shown once.  
3. Phone Tailscale logged into the same Headscale tailnet.  
4. Connect to `ws://<host-magicdns-or-tailnet-ip>:7531/v1/ws`  
5. Send the `auth` message with the token (see [protocol-v1.md](./protocol-v1.md)).  

## What Phase 1 does **not** do

- Call the Headscale HTTP API from `mcremote`  
- Install or manage `tailscaled` automatically  
- Expose a public app relay when the phone is off-mesh (join the mesh instead)  
- Reverse-proxy WebSocket traffic through Headscale port 443  

## Troubleshooting

| Symptom | Check |
|---------|--------|
| LE / ACME fails | Public DNS A record, SG TCP 80, nothing else bound to :80, `journalctl -u headscale` |
| Phone cannot add server | `server_url` HTTPS, SG TCP 443, valid cert name matches FQDN |
| Nodes online but app fails | `tailscale ping <host>`, mcremote listening, grant allows tcp:7531, app uses **100.x** not public IP |
| Works on Wi‑Fi not LTE | DERP map reachable; UDP 41641; carrier CGNAT → expect DERP path |
| Fake provider in sessions | Disable `providers.fake`, ensure `grok` on PATH and ready |
