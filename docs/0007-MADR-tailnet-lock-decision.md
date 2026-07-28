# 0007 — Tailnet lock (control-plane MITM)

* Status: **Accepted (document-and-defer)**
* Date: 2026-07-20
* Relates to: hardening plan §5.2, [0004](0004-certificate-management-decision.md),
  [0005](0005-client-identity-decision.md), D5

## Context

The daemon reaches clients over a self-hosted Headscale/Tailscale mesh. A
**compromised or malicious Headscale control plane** can inject a node key and
MITM the WireGuard tunnel — WireGuard authenticates nodes, but the control plane
distributes the node keys. Tailscale SaaS mitigates this with **tailnet lock**
(TKA), which requires node keys to be signed by a trusted signing node before
peers accept them.

## Decision

**Do not implement tailnet lock or any home-grown equivalent. The risk is
already substantially mitigated for the only production client by shipped
controls; record the residual and the compensating controls.**

### Why no code

Self-hosted **Headscale has no network-lock feature**, and no credible near-term
path to one. [juanfont/headscale#1307](https://github.com/juanfont/headscale/issues/1307)
has been open since April 2023, `tailscale-feature-gap`, no milestone, no
assignee; maintainers have explicitly declined contributions ("consider
Tailscale's SaaS if your use-case really requires this") and flagged the crypto
as too high-risk to accept from external contributors.

So there is no idiomatic Headscale primitive to enable. The alternatives are
both non-starters as mcremote code:

* **A home-grown node-key signing/monitoring scheme** would reimplement exactly
  the crypto the Headscale maintainers refuse to attempt — high risk, and it
  would be security theatre or worse.
* **Monitoring node-key changes** is redundant: the app's cert pinning already
  surfaces the attack's *consequence* at connect time, immediately and harder.

### The compensating control (already shipped)

Walking the concrete MITM post-Phase 1/2/3 — attacker compromises Headscale,
injects a node key so the phone's tunnel terminates on an attacker node:

* **Phone → daemon:** the attacker must terminate TLS and present a certificate.
  The phone pins the daemon cert out-of-band via the pair QR, validated
  fingerprint-only (`withTrustedRoots: false`,
  `apps/mobile/lib/data/ws/mcremote_client.dart`). The attacker lacks the
  daemon's private key → permanent `cert_mismatch`. **MITM broken.**
* **Daemon → phone:** Phase 3 requires the phone's enrolled client key
  (`internal/ws/server.go`, enforcement default-on per D7). An attacker
  impersonating the phone lacks it → rejected.

Phases 2 + 3 therefore provide **mutual, out-of-band-bootstrapped
authentication at the TLS layer, independent of the mesh** — functionally what
tailnet lock provides at the WireGuard layer, for the app channel. This is the
answer the plan already intuited ("Pinning survives this; public PKI does
not").

### Residual risk (named, accepted)

1. **Availability.** A hostile control plane can always *deny* coordination.
   Tailnet lock does not fix this either — it guarantees authenticity, not
   availability. Out of scope for any transport control.
2. **LE-mode name-controller caveat.** In `letsencrypt` mode acceptance is
   "valid chain OR pin," so an attacker who controls the DNS name can obtain a
   legitimate cert and pass the chain check. **D5 keeps the app channel on
   `selfsigned`/pin-only (IP-dialled), so this does not bite today** — which
   makes D5 an explicit 5.2 mitigation, not merely an ACME-convenience choice.
   It becomes load-bearing only if LE is ever adopted for the app channel.
3. **Non-pinning consumers.** Only the mobile client and the dev
   `scripts/smoke-protocol` dial the daemon. The smoke tool can pin but falls
   back to `InsecureSkipVerify` without a fingerprint — keep it local-only. Any
   future browser/`curl` client is exposed unless it pins (and a browser
   cannot), which is a reason such a client would force the LE path and reopen
   caveat (2).

### Honest reclassification

Headscale ACLs (Phase 1.2) and pre-auth-key hygiene mitigate a rogue *enrolled
peer*, **not** a compromised *control plane* — a compromised control plane
ignores its own ACLs and mints node keys directly. They remain worthwhile
defense-in-depth against the lesser threat; they are **not** credited against
the §5.2 threat.

## Reopen trigger

Watch [#1307](https://github.com/juanfont/headscale/issues/1307). If Headscale
ships network-lock, adopt it as defense-in-depth. Non-blocking. The only other
way to genuinely close the residual is migrating the control plane to Tailscale
SaaS + `tailscale lock` — not an mcremote code change.
