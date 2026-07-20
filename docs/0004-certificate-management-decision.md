# 0004 — Certificate management

* Status: **Accepted**
* Date: 2026-07-20
* Supersedes the ad-hoc TLS defaults introduced alongside `internal/certs`

## Context

`mcremote` executes agent tool calls on the operator's machine and can approve
permission prompts remotely. Compromise means arbitrary code execution as the
user, so transport trust is a first-order concern.

Two certificate strategies are implemented:

* **`selfsigned`** — `internal/certs/certs.go` mints a long-lived P-256 leaf;
  its SHA-256 fingerprint is printed into the pair QR and pinned by the client
  (`apps/mobile/lib/data/ws/mcremote_client.dart`, `CertPinner`).
* **`letsencrypt`** — `internal/certs/acme.go` obtains a publicly trusted
  certificate via certmagic + `libdns/route53`, DNS-01 only.

`internal/config/config.go` `ResolvedMode()` selects between them.

This decision records which is the default, and — more importantly — what had
to change for either to be safe.

## The question we thought we were answering

"Should Let's Encrypt or self-signed be the default?"

Three independent reviews (threat model, operations, adversarial) converged on
the finding that **this was the wrong question**. Two facts reframed it:

1. **The default is already conditional and already correct.**
   `ResolvedMode()` returns `letsencrypt` only when a domain *and* an email are
   configured; otherwise `selfsigned`. Every zero-config, IP-dialled, laptop
   and intermittently-connected host already gets self-signed + pinning with no
   user action. Making that unconditional would buy nothing for those cases and
   would cost the stable-host, browser-client and relay cases.

2. **The Let's Encrypt path has no working failure mode.**
   `Pinned()` is true only for `selfsigned`, so `pairFingerprint()` returns
   empty in LE mode and the QR carries no pin. But `internal/daemon/certs.go`
   falls back to a *self-signed* certificate when ACME fails. The daemon comes
   up, serves a certificate with correct SANs, and the phone — holding no pin,
   performing chain validation — rejects it permanently.

   **The fallback cannot succeed by construction.** It provides availability
   for the daemon process and none for the user. Every LE failure (unresolvable
   zone, expired credential, rate limit, dark host, no network at boot) lands
   in the same unrecoverable state.

The real defect was never the choice of default. It was that one branch of the
choice fails closed with no path back.

## Decision

### 1. Keep the conditional default

`selfsigned` unless a domain *and* email are explicitly configured. Do not make
either mode unconditional. Rationale: it already routes each deployment shape
to the mode that fits it, and both modes have legitimate territory (below).

### 2. Make the Let's Encrypt fallback functional

Emit the certificate fingerprint in the pair QR **in both modes**. The client
accepts a certificate when **either** the chain validates **or** the
fingerprint matches:

| Mode | Client acceptance rule | Trust set |
|---|---|---|
| `selfsigned` | fingerprint **only** (`withTrustedRoots: false`) | exactly 1 certificate |
| `letsencrypt` | valid chain **OR** fingerprint match | public CAs for that name, ∪ this one certificate |

This preserves transparent 60-day ACME renewal (the chain keeps validating, so
a stale pin is never load-bearing) while making the self-signed fallback
actually usable. It is strictly more available than today and strictly more
restrictive than public PKI alone.

The reason the pin was originally omitted in LE mode — that a leaf pin breaks
on renewal — is answered by *or* rather than *and*.

### 3. Treat the transport fixes as higher priority than the cert choice

All three reviews independently ranked these above the certificate question:

* **Bind to the tailnet interface, not `0.0.0.0`.** Currently `0.0.0.0` in six
  shipped launch paths: both systemd units, `scripts/start-mcremote-grok.sh`,
  the `setup_service.go` flag default, and two configs. This contradicts the
  project's own checklist (`docs/0002-…:278`, `:303`).
* **Ship a deny-by-default Headscale ACL** consuming the `tag:mcremote-host` /
  `tag:mcremote-client` tags the docs already instruct operators to apply, and
  delete the allow-all example in `docs/headscale.md`. `config.prod.example.yaml`
  justifies its `0.0.0.0` bind with "only with Headscale grants locking TCP
  7531" — grants that were never written.

With an open ACL and an all-interfaces bind, a bearer token is the only control
standing between any tailnet node — or any host on a café LAN — and code
execution. No certificate strategy addresses that.

### 4. Fix the certificate defects

* **Drop `IsCA: true` and `KeyUsageCertSign` from the serving leaf.** It is
  currently a decade-long signing-capable CA sitting beside its own key. Inert
  while pinned, but the natural act of installing it in a trust store to make
  `curl` or a browser work yields a CA that can sign for *any* name.
* **Stop regenerating identity on any load error.** `Ensure()` falls through to
  `generate()` whenever `load()` returns an error — not only absence or expiry.
  A transient read failure silently mints a new identity and invalidates every
  paired device. Distinguish *absent* (generate) from *unreadable* (fail loudly).
* **Fix the single-slot pin store.** The client keeps one fingerprint under one
  key scoped to one host authority, so alternating between two daemons discards
  the other's pin and forces a rescan every switch. This is the entire
  ongoing cost people attribute to pinning, and it is a client bug, not a
  property of the approach.

### 5. Scope of each mode

| Deployment | Mode | Why |
|---|---|---|
| Laptop / dev machine | `selfsigned` | No AWS credential on a portable device; immune to the 90-day dark-host cliff; recovery is one QR rescan |
| Home server | `selfsigned` | Same, once the multi-host pin bug is fixed |
| Always-on host with a public name (e.g. the AWS box) | `letsencrypt` | EC2 instance role supplies credentials with no rotation burden; always-on so renewal never lapses; already the Route 53 principal |
| Browser / `curl` / third-party clients | `letsencrypt` | Cannot pin a fingerprint from a QR |

## Consequences

**Positive.** Zero-config deployments keep a trust set of exactly one
certificate. The LE path gains a fallback that works. The highest-severity
exposure (open port, open ACL) gets addressed rather than being masked by a
certificate debate. Both modes remain first-class, so the relay path in
`docs/0001` is not foreclosed.

**Negative.** Two client acceptance rules instead of one — more surface to test
(covered by `apps/mobile/test/cert_pinning_test.dart`). LE mode's trust set is
wider than pure pinning by design. Pinning still has no revocation mechanism.

## Explicitly not decided

* **Client certificates / mTLS.** Both strategies authenticate only the
  *server*. The daemon cannot identify which node is connecting; a stolen
  bearer token works from anywhere on the tailnet. This is a larger gap than
  the server-cert choice and deserves its own decision record.
* **Headscale-issued certificates.** `tailscale cert` resolves both the
  credential-distribution and IP-SAN problems on Tailscale SaaS, but Headscale
  has no equivalent node-certificate provisioning. Worth a time-boxed spike
  before any further ACME investment.
* **Tailnet lock.** Absent. Without it, a compromised coordination server can
  inject a node key and MITM WireGuard. Pinning survives this; public PKI does
  not, since whoever controls the name can obtain a valid certificate.

## Rejected alternatives

* **Let's Encrypt only, drop self-signed.** LE never issues for IP addresses
  and `100.64.0.0/10` is CGNAT, so IP-dialled hosts — the documented default
  pairing flow — could not be secured at all.
* **Self-signed only, drop Let's Encrypt.** Forecloses browser clients and the
  relay-primary path in `docs/0001`, and drives operators toward installing a
  CA certificate in system trust stores.
* **Make `selfsigned` the unconditional default.** A no-op for the cases it
  targets, a regression for the rest. See "the question we thought we were
  answering".
