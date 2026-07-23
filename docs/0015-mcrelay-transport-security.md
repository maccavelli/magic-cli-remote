# MADR 0015: mcrelay — outbound relay transport & security

- **Status**: Accepted
- **Date**: 2026-07-23
- **Deciders**: Project Owner
- **Implements**: [MADR 0009](0009-post-hardening-action-plan.md) Phase E (design track)
- **Extends**: [MADR 0001](0001-architecture-mcremote.md) hybrid networking (relay path)
- **Preserves**: [MADR 0004](0004-certificate-management-decision.md) server TLS,
  [MADR 0005](0005-client-identity-decision.md) client-key allowlist,
  [protocol-v1.md](protocol-v1.md) full control-plane surface
- **Supersedes**: Mesh-only remote reachability as the *only* path ([MADR 0003](0003-phase1-decisions.md)
  Phase 1 constraint); mesh remains the preferred path when available

## Context and problem statement

`mcremote` is a mature mesh-first control plane: device tokens, SPKI client
keys, TLS pin / Let's Encrypt, owner isolation, dual providers, durable
history, and protocol-v1 (including `models.list` pickers). Phones that are
**not** on the Headscale/Tailscale mesh cannot reach the host.

MADR 0001 named **outbound relay** as the long-term primary networking path.
MADR 0009 Phase E deferred code until an ADR locked:

1. Trust model (end-to-end vs relay-terminated)
2. Auth (device token + client key through the relay)
3. Discovery (pair URI)
4. Ops (self-hosted vs SaaS)
5. Fallback (mesh when available)

**Product requirement (locked):** the relay path must preserve **full**
functionality of the backend `mcremote` daemon — not a reduced API. Priority
for this design is **network transport security and hardening**.

## Decision drivers

- Full protocol-v1 parity (auth, pair, sessions, events, permissions, providers,
  models, history) without a second product surface on the relay
- ADR 0005: client key proven to **mcremote**, not only to a public edge
- ADR 0004: phone trusts **mcremote** identity (pin / LE / fallback), not only
  the public relay certificate
- Relay operator and process are **untrusted for content** (transcripts, tokens,
  tool payloads)
- Host behind NAT/firewall: **outbound-only** from `mcremote` (no inbound ports)
- Self-hosted first; no multi-tenant public SaaS required for v1 (0009 E.2)
- One self-hosted `mcrelay` may serve **multiple** registered hosts

## Decision outcome

### D1 — Role of mcrelay

**`mcrelay` is a join-plane router and opaque byte splice**, not an auth server,
session store, or protocol interpreter.

| mcrelay does | mcrelay does not |
|--------------|------------------|
| Accept host outbound registration | Store device tokens or client keys |
| Accept phone outer connections | Validate `auth` / `pair.claim` for agent power |
| Authorize **join** of phone ↔ host | Run agents or hold `history.json` |
| Splice bytes after join | Decrypt or re-encode protocol-v1 |
| Enforce public DoS limits | Enforce owner isolation (host already does) |

Binary name: **`mcrelay`**. Code lives in this monorepo (`cmd/mcrelay`,
`internal/relay/…`), sharing packages with `mcremote` only where safe (e.g.
logging, pair URI parse extensions) — **not** the session manager or auth
device store.

### D2 — Trust model: opaque join + end-to-end TLS to mcremote

```
Phone                            mcrelay                           mcremote
─────                            ───────                           ────────
Outer TLS (relay identity)  ──►  join plane only
                                 (host_id, reg auth, limits)
Inner TLS / WSS ═══════════════════════════════════════════════►  mcremote
  (fp pin | LE; RequestClientCert;                                leaf +
   full protocol-v1)                                              client key
                                 opaque splice of ciphertext
```

1. **Outer hop** (phone↔mcrelay, host↔mcrelay): TLS to the **relay** certificate
   (typically public Let's Encrypt). Protects join metadata in transit from
   network observers. The relay operator can see join-plane fields and
   traffic metadata (sizes, timing, who joined whom).
2. **Inner hop** (phone↔mcremote through the splice): **end-to-end TLS**
   terminating on **mcremote**, with the same certificate acceptance rules as
   mesh-direct (`selfsigned` pin, `letsencrypt` chain-or-fallback-pin, client
   key at protocol layer per ADR 0005).
3. After join succeeds, mcrelay copies opaque frames/bytes and **must not**
   parse protocol-v1 envelopes.

**Consequences:**

- Full backend functionality: every protocol-v1 message type works unchanged.
- Compromised mcrelay cannot read prompts/history or forge authed frames
  without breaking inner TLS.
- Pair QR **`fp` / `mode` continue to describe mcremote**, never the relay leaf.
- Outer and inner identities must not be conflated (client bug if they are).

### D3 — Rejected trust models (for this ADR)

| Model | Why rejected as primary |
|-------|-------------------------|
| TLS terminate at mcrelay; clear protocol re-encode to host | Relay sees all envelopes; client key no longer binds host TLS; fails security priority |
| App-layer E2E crypto only (custom AEAD over clear WS) | Reinvents transport crypto; still need client-key proof; higher complexity than nested TLS |
| Relay re-authenticates devices | Split auth store; relay becomes capable of minting agent power if buggy |

A future “inspecting proxy” mode is **out of scope** and would require a new ADR.

### D4 — Full functional parity

Anything that works mesh-direct to `mcremote` must work via relay once the
inner channel is up, including but not limited to:

- `auth`, `pair.claim` (and all typed error codes)
- `session.*` (create/list/close/delete/prompt/cancel/history)
- event push stream (chunks, tools, permissions, status, notices, …)
- `permission.respond`
- `providers.list`, `models.list`
- authenticated `/v1/hello` semantics on the **host** (not reimplemented on relay)
- host-side limits, owner isolation, sticky auth, revoke → live disconnect

**mcrelay is not a subset API.** Feature gaps are bugs unless explicitly
deferred in a later ADR.

### D5 — Host registration (outbound)

1. `mcremote` dials out to mcrelay over TLS (no inbound port on the host).
2. Host authenticates with a **registration secret** (or key) configured only
   on the host and verified by mcrelay (hashed at rest on the relay where
   practical).
3. Host advertises a stable public **`host_id`** (not secret).
4. Connection stays up with keepalive; reconnect with backoff on blip.
5. Optional: mcrelay host allowlist (only configured registration credentials).

**Registration secret never appears on the phone or in the pair QR.**

### D6 — Phone join and discovery

**Pair URI** gains optional relay routing fields. Existing fields keep meaning:

| Param | Meaning (unchanged unless noted) |
|-------|----------------------------------|
| `host` | **mcremote** dial target for mesh/LAN/direct (inner identity peer) |
| `fp` | Pin for **mcremote** leaf (or LE fallback leaf) |
| `mode` | Certificate rule for **mcremote** |
| `code` / `token` | Pair code or device token (consumed on **mcremote** over inner channel) |
| **`relay`** *(new)* | Outer URL for mcrelay, e.g. `wss://relay.example.com:443` |
| **`hid`** *(new)* | Public `host_id` for join routing |

Example (illustrative):

```
mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531&code=K7M29X4P&fp=<mcremote-fp>&mode=selfsigned&relay=wss%3A%2F%2Frelay.example.com&hid=<host_id>
```

**Client connection preference:**

1. If mesh/direct `host` is reachable → use it (no relay).
2. Else if `relay` + `hid` present → outer TLS to relay, join `hid`, then
   **inner** TLS to mcremote using `fp`/`mode` (and mesh `host` as SNI/name
   hint only where applicable; pin remains authoritative in `selfsigned`).
3. Else fail closed with a clear “host unreachable; relay not configured” error.

Join authorization: phone may only attach to a host that is currently
registered; mcrelay must not allow arbitrary host_id probing without rate
limits. v1 may rely on unguessable `host_id` entropy + rate limits; a
short-lived **join ticket** minted by the host remains a reserved extension
if probing becomes practical.

### D7 — Multi-host, self-hosted ops

- One `mcrelay` process may register **N** independent hosts (personal/family
  edge or VPS).
- **No** multi-tenant public SaaS, accounts marketplace, or billing in v1.
- Outer TLS: public name + Let's Encrypt (or operator cert files).
- Single Go binary + systemd unit pattern analogous to `mcremote`.

### D8 — Security requirements (normative)

| ID | Requirement |
|----|-------------|
| **S1** | Inner channel is E2E TLS (or equivalent) to **mcremote** with existing cert rules. |
| **S2** | Device tokens are never sufficient on mcrelay for agent actions. |
| **S3** | Client key possession is proven to **mcremote** (inner handshake / protocol layer). |
| **S4** | mcrelay cannot mint sessions, prompts, or permission approvals without host-side auth. |
| **S5** | Host registration is authenticated; strangers cannot claim a `host_id`. |
| **S6** | Join is constrained (registered host only + rate limits; ticket optional later). |
| **S7** | Fail closed on ambiguous joins (duplicate registration, mid-join drop). |
| **S8** | No durable transcript/token storage on mcrelay; never log tokens, pair codes, or inner plaintext. |
| **S9** | Default public listeners are TLS-only (no plaintext public WS). |
| **S10** | Hard limits: max hosts, phones/host, concurrent joins, frame size, idle timeout, accept rate. |
| **S11** | Host path is outbound-only for relay reachability. |
| **S12** | Host revoke / either-side close tears down the splice promptly. |
| **S13** | Outer (relay) cert and inner (mcremote) identity are distinct; clients must not swap trust rules. |

### D9 — Hardening (should)

| ID | Requirement |
|----|-------------|
| **H1** | Constant-time compare on registration secrets where practical. |
| **H2** | Registration secret rotatable without re-pairing phones (`hid` + `relay` stable). |
| **H3** | Per-IP and per-host join rate limits; pre-auth connection caps. |
| **H4** | Info-level join-plane disconnect reasons only (`host_gone`, `join_denied`, `limit`, …). |
| **H5** | Optional host allowlist on mcrelay. |
| **H6** | If join tickets are added: TTL + single-use. |
| **H7** | Prefer mesh direct when reachable (reduces relay exposure). |

### D10 — Explicit non-goals (v1)

- Protocol inspection / DLP on mcrelay  
- Running agents or caching history on mcrelay  
- Replacing Headscale (hybrid, not either/or)  
- Windows-first ops  
- Required cloud account  
- CLI interactive TUI for relay admin (can be later)  

## Threat model (relay-focused)

| Threat | Mitigation |
|--------|------------|
| Malicious relay operator reads chats | S1 inner TLS |
| Relay injects `session.prompt` | Inner TLS integrity |
| Stolen registration secret hijacks host slot | S5, H2 rotation, H5 allowlist |
| Host_id enumeration / join spam | S6, H3, unguessable ids |
| Public DoS | S10 |
| Downgrade to clear outer | S9 |
| Client confuses relay cert with host | S13; `fp`/`mode` always mcremote |
| Log scrapers | S8 |
| Pivot from relay to host LAN | Host initiates outbound only |

**Accepted residual risk:** relay sees **metadata** (who joined which host_id,
byte volumes, timing). Operators who need metadata privacy should self-host
mcrelay on a machine they trust for traffic analysis, not content.

## Implementation sketch (post-ADR; not a commitment to file layout)

| Component | Responsibility |
|-----------|----------------|
| `cmd/mcrelay` | Public serve, TLS, limits, multi-host join table |
| `internal/relay` | Registration protocol, join protocol, splice |
| `mcremote` | Outbound dialer when `relay.url` + `relay.registration_secret` configured |
| `pairuri` | Encode/parse `relay` + `hid` |
| Flutter client | Prefer direct; else join via relay then inner TLS with existing CertPinner + client identity |

**Join-plane message shapes** (to be detailed at implement time; illustrative):

- Host: `register` → `register_ok` / `register_error`  
- Phone: `join` `{ host_id }` → `join_ok` / `join_error` then opaque splice  
- Errors: `unauthorized`, `unknown_host`, `host_offline`, `rate_limited`, `limit`

Inner channel after `join_ok` is the existing `wss` upgrade to mcremote’s
`/v1/ws` (or a raw TLS stream that then speaks the same HTTP/WS upgrade
semantics — implementation choice must preserve client cert presentation).

## Verification / exit criteria (Phase E)

- [x] ADR accepted (**this document**)
- [x] Automated join-plane smoke + e2e (`internal/relay/e2e_test.go`, CI race on relay)
- [ ] Manual smoke: phone **off-mesh** can auth + create + prompt + permission + history + `models.list` via relay — [ops-mcrelay.md](ops-mcrelay.md) §7
- [ ] Security review: compromised mcrelay credentials alone cannot mint host sessions (automated: unauthorized register; full review manual)
- [ ] Mesh-direct still works with relay disabled
- [ ] Revoke on host drops live relay path
- [ ] Adversarial tests: evil splice injection (must fail inner TLS), join flood, wrong `fp`, stolen outer-only connection without inner auth
- [x] `go test` / Flutter tests for pair URI fields and client path selection

## Phased delivery (after this ADR)

| Phase | Scope | Status |
|-------|--------|--------|
| **E0** | Pair URI `relay`/`hid` + client path selection stubs (no live relay) | **Shipped** — `internal/pairuri`, Flutter `PairPayload` + `ConnectionPath` |
| **E1** | `mcrelay` MVP: register, join, splice, TLS, limits, multi-host | **Shipped** — `cmd/mcrelay`, `internal/relay` |
| **E2** | `mcremote` outbound registration + tunnel→local TCP bridge + pair URI | **Shipped** — `internal/relayhost`, `relay.*` config |
| **E3** | Mobile off-mesh: outer join + **inner TLS** through splice (S1–S13) | **Shipped** — `RelayTransport` loopback bridge + `connectionFactory` |
| **E4** | Ops docs (systemd, LE, rotation of registration secret) | **Shipped** — [ops-mcrelay.md](ops-mcrelay.md), `deploy/systemd/mcrelay.user.service` |

### E1 operator sketch

Full CLI / config / env / `setup-service`: **[config-mcrelay.md](config-mcrelay.md)**.

```bash
make build-relay
install -m 755 bin/mcrelay ~/.local/bin/mcrelay
# config: ~/.config/mcrelay/config.yaml (see configs/mcrelay.example.yaml)
mcrelay serve \
  --listen-host 0.0.0.0 --listen-port 8443 \
  --tls-cert /path/fullchain.pem \
  --tls-key /path/privkey.pem \
  --allow 'devbox-1:your-long-registration-secret'
# or:
mcrelay setup-service --force --service-config ~/.config/mcrelay/config.yaml
```

### E2 mcremote host side

```yaml
# mcremote config
relay:
  url: "wss://relay.example.com:8443"
  host_id: "devbox-1"
  secret: "same-as-mcrelay-allow"   # or MCREMOTE_RELAY_SECRET
```

```bash
mcremote serve --relay-url wss://relay.example.com:8443 \
  --relay-host-id devbox-1 --relay-secret 'same-as-mcrelay-allow'
# pair URI then includes relay= and hid= (secret never on QR)
mcremote pair code --name phone --qr
```

On each phone join, mcrelay sends `dial`; mcremote opens `/v1/tunnel` and
bridges tunnel bytes to the local control-plane TCP listener (loopback rewrite
for `0.0.0.0`).

### E3 mobile path

1. Probe TCP reachability of mcremote host (mesh/LAN); use **direct** if up.
2. Else outer WSS to mcrelay `/v1/phone` → `join` `{host_id}`.
3. After `join_ok`, splice is an opaque byte pipe (WS binary ↔ host TCP).
4. Phone opens a loopback `ServerSocket`, bridges it to the outer WS, then dials
   inner `wss://<mcremote-host>/v1/ws` with `HttpClient.connectionFactory`
   connecting to `127.0.0.1:localPort` so **SNI, pin, and client key** still
   apply to mcremote (not the relay).

Join plane (WebSocket, JSON text until splice):

| Path | First message | Role |
|------|---------------|------|
| `GET /v1/host` | `register` `{host_id,secret}` | Host control; receives `dial` |
| `GET /v1/tunnel` | `tunnel` `{session_id,host_id,secret}` | Host data leg after `dial` |
| `GET /v1/phone` | `join` `{host_id}` | Phone; then opaque splice |
| `GET /healthz` | — | Liveness `{"ok":true,"service":"mcrelay"}` |

Pair URI example with relay routing:

```
mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531&code=…&fp=…&mode=selfsigned&relay=wss%3A%2F%2Frelay.example.com&hid=devbox-1
```

Code must not invent a second auth model on the relay without reopening this ADR.

## Consequences

**Positive**

- Off-mesh reachability without weakening host auth or TLS identity
- Full product surface available remotely
- Self-hosted multi-host relay without SaaS commitment
- Clear trust boundary for security review

**Negative / costs**

- Nested TLS complexity (client must maintain outer + inner)
- Slight latency vs mesh direct
- Relay operator still sees connection metadata
- Implementation care around WebSocket + client certificates through a splice

## Links

- Architecture: [0001](0001-architecture-mcremote.md)
- Phase 1 mesh-only: [0003](0003-phase1-decisions.md)
- Server TLS: [0004](0004-certificate-management-decision.md)
- Client identity: [0005](0005-client-identity-decision.md)
- Product track: [0009](0009-post-hardening-action-plan.md) Phase E
- Post-MVP audit / P1–P6: [0016](0016-mcrelay-audit-hardening.md)
- Community relay patterns: [0002](0002-community-assessment-and-stack-recommendations.md) (Shellular E2E)
- Wire protocol: [protocol-v1.md](protocol-v1.md)
