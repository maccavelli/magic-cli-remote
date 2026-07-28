# 0008 — Headscale-issued certificates

* Status: **Accepted (defer)**
* Date: 2026-07-20
* Relates to: hardening plan §5.3, [0004](0004-certificate-management-decision.md),
  D5

## Context

Phase 2 built an ACME/Let's Encrypt TLS mode (certmagic + `libdns/route53`,
DNS-01) alongside self-signed + pinning. The LE path needs a Route 53 IAM
credential **on every daemon host** and only serves hosts with a public DNS
name. The §5.3 hypothesis: `tailscale cert` yields publicly-trusted certs for a
node's MagicDNS name with no per-host DNS credential and no IP-SAN problem,
which could dissolve much of that machinery.

## Decision

**Defer. Keep the current LE + self-signed + pinning design. No repo changes.**

### Why

The enabling capability does not exist in Headscale, and under D5 its value is
small even if it did.

* **`tailscale cert` runs the ACME client on the node** (DNS-01), with the
  control plane writing the `_acme-challenge` TXT for the node's MagicDNS name.
  On Tailscale SaaS this works because Tailscale owns `*.ts.net` and does the
  DNS write. ([tailscale.com/docs/how-to/set-up-https-certificates](https://tailscale.com/docs/how-to/set-up-https-certificates))
* **Self-hosted Headscale does not implement it.**
  [juanfont/headscale#2527](https://github.com/juanfont/headscale/issues/2527)
  ("tailscale cert + serve") is targeted at milestone **v0.34.0** — roughly five
  minor releases out — with no PRs; the required `/machine/set-dns` endpoint,
  `DNSConfig.CertDomains`, and control-plane TXT provisioning are unbuilt.
  Headscale's existing ACME is control-plane-only and supports **HTTP-01 /
  TLS-ALPN-01 only, not DNS-01** ([headscale.net/stable/ref/tls](https://headscale.net/stable/ref/tls/)).
* **D5 shrinks the payoff.** IP-dialled hosts stay on `selfsigned` with no DNS
  name, so Headscale certs could only ever help a *named* host — here just the
  AWS box, which already has a near-free LE path via an EC2 instance role. The
  credential-distribution win (one control-plane credential vs one per daemon)
  is real in principle but nearly moot when only one host is eligible and its
  cost is already near-zero.

### What it would buy if it shipped

Recorded so the reopen decision is informed: it would **replace** the daemon
ACME machinery — drop `internal/certs/acme.go`, certmagic, and `libdns/route53`,
serving instead via the Tailscale `LocalClient.GetCertificate` callback — and
move the sole remaining Route 53 credential from every daemon host onto the
Headscale control plane, off the RCE-exposed daemons. It would also resolve the
IP-SAN limitation.

## Reopen trigger

Reopen when **Headscale ships #2527** (a release noting `/machine/set-dns` +
`DNSConfig.CertDomains` + node-side DNS-01 provisioning) **and** the operator is
willing to make `ts.lallygag.net` an ACME-issuable zone with write credentials
on the control plane. If a browser/`curl` client (which pinning cannot serve) is
also wanted at that time, the two motivations combine and the swap becomes
worthwhile.

Sources:
[headscale#2527](https://github.com/juanfont/headscale/issues/2527) ·
[headscale.net/stable/ref/tls](https://headscale.net/stable/ref/tls/) ·
[tailscale HTTPS certs](https://tailscale.com/docs/how-to/set-up-https-certificates)
