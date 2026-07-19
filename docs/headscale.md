# Headscale setup for mcremote (Phase 1)

Phase 1 uses **Headscale/Tailscale only** (no outbound relay). The phone and the host running `mcremote` must be on the same tailnet.

## Test control plane

This project’s test Headscale is expected at:

```text
http://localhost:8080
```

Point Tailscale clients at that coordination server when registering nodes.

## Tags

Recommend advertising:

- Host (daemon machine): `tag:mcremote-host`
- Phone / client: `tag:mcremote-client`

## Example grant (huJSON)

Prefer **grants** over legacy ACLs. Conceptual example — adapt to your Headscale policy file:

```hujson
{
  "tagOwners": {
    "tag:mcremote-host": ["autogroup:admin"],
    "tag:mcremote-client": ["autogroup:admin"],
  },
  "grants": [
    {
      "src": ["tag:mcremote-client"],
      "dst": ["tag:mcremote-host"],
      "ip": ["tcp:7531"],
    },
  ],
}
```

Reload Headscale after policy changes (`systemctl reload headscale` or `SIGHUP`).

## Bind guidance

| Mode | `listen.host` | Notes |
|------|---------------|--------|
| Local only | `127.0.0.1` | Default; phone cannot connect |
| Mesh | tailnet IP or `0.0.0.0` | Only if grants/firewall restrict who can open **TCP 7531** |

Always keep `auth.require_device_token: true` in production.

## Pairing checklist

1. On the host: `mcremote pair create --name phone`  
2. Copy the `mcr_…` token into the Flutter (or test) client — shown once.  
3. Ensure the phone’s Tailscale client is logged into the same Headscale tailnet.  
4. Connect to `ws://<host-magicdns-or-ip>:7531/v1/ws`  
5. Send the `auth` message with the token (see [protocol-v1.md](./protocol-v1.md)).  

## What Phase 1 does **not** do

- Call the Headscale HTTP API  
- Install or manage `tailscaled`  
- Provide a public relay when the phone is off-mesh  
