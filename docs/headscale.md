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

Deny-by-default policy (`/etc/headscale/acl.hujson`). Headscale evaluates only
what the ACL permits: with no matching rule, the connection is dropped. There is
no allow-all rule below and none should be added — the only permitted flow is a
client reaching the daemon port on a daemon host.

**v0.29 caveat:** Headscale v0.29 rejects `autogroup:admin` as a `tagOwners`
value. Name the real user(s) who may advertise each tag (`user:` prefix, the
usernames from `headscale users list`). Substitute `mac` below for yours.

```hujson
{
  // Who is allowed to advertise each tag. Untagged nodes match no rule below
  // and therefore cannot reach 7531 at all.
  "tagOwners": {
    "tag:mcremote-host":   ["user:mac"],
    "tag:mcremote-client": ["user:mac"],
  },

  "acls": [
    // The only allowed flow: a paired phone → the mcremote daemon port.
    {
      "action": "accept",
      "src": ["tag:mcremote-client"],
      "dst": ["tag:mcremote-host:7531"],
    },
  ],
}
```

Everything else — client→client, host→client, any other port on the host, any
untagged node → anything — is denied because no rule accepts it.

If you need SSH or another admin path to the host, add an explicit narrow rule
for it (e.g. `"src": ["user:mac"], "dst": ["tag:mcremote-host:22"]`) rather than
relaxing the rule above.

Reload after edits (`systemctl reload headscale` or restart), then verify: a
tailnet node **without** `tag:mcremote-client` must fail to open TCP 7531 on a
tagged host.

```bash
# From an untagged node — expect a timeout/refusal, not a connection:
nc -vz <host-tailnet-ip> 7531
```

When creating keys, v0.29 wants **user ID**, not username. Prefer **single-use,
short-expiry** keys: a reusable 48h key is a 48-hour window in which anyone
holding it can enrol an arbitrary node into the tailnet. Omit `--reusable` and
give the key only as long as you need to walk to the device.

```bash
sudo headscale users list
sudo headscale preauthkeys create -u 1 --expiration 1h --tags tag:mcremote-host
```

### 5. User + preauth keys

```bash
sudo headscale users create mac
# Single-use (no --reusable), short expiry, and tagged so the ACL above applies.
# Issue one key per node, immediately before enrolling that node.
sudo headscale preauthkeys create --user mac --expiration 1h --tags tag:mcremote-host
sudo headscale preauthkeys create --user mac --expiration 1h --tags tag:mcremote-client
```

A node that joins with an untagged key matches no ACL rule and cannot reach
7531. If your Headscale version lacks `--tags`, set the tag on the node
afterwards (`headscale nodes tag -i <id> -t tag:mcremote-client`) and confirm
with `headscale nodes list`.

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
mcremote pair code --name phone --qr
# or: ./scripts/start-mcremote-grok.sh --pair phone
# phone: Connect → Enter code (or Scan QR)
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
| Local only | `127.0.0.1` | `Defaults()`; phone cannot connect |
| Mesh | `tailscale` | **Recommended, and the default in every shipped launch path.** Resolved at startup to this host's Tailscale IPv4; waits for that address rather than widening to `0.0.0.0` |
| Off-tailnet | `0.0.0.0` | Explicit opt-in only. Reachable from any interface, including café wifi; the ACL above then protects nothing |

Always keep `auth.require_device_token: true` in production.

## Pairing checklist

1. On the host: `mcremote pair code --name phone --qr` (or `./scripts/start-mcremote-grok.sh --pair phone`)  
2. On the phone: Connect → **Enter code** (8 chars) or **Scan QR**.  
3. Durable token is stored on the device after claim — no re-emailing. Codes expire in 5 minutes.  
4. Phone Tailscale logged into the same Headscale tailnet.  
5. Connect to `ws://<host-magicdns-or-tailnet-ip>:7531/v1/ws`  
6. Send the `auth` message with the token (see [protocol-v1.md](./protocol-v1.md)).  

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
