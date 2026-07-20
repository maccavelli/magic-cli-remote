# Let's Encrypt (ACME DNS-01 via Route 53)

`tls.mode: letsencrypt` is mcremote's default TLS mode as soon as a domain and
an ACME email are configured. The daemon obtains a publicly trusted
certificate, the phone validates it with the platform trust store, and nothing
is pinned. `tls.mode: selfsigned` remains available and is the automatic
fallback.

## Why DNS-01 is the only supported challenge

mcremote daemons are mesh-only. They bind port 7531 on a Tailscale/Headscale
address in `100.64.0.0/10`, their MagicDNS names (`*.ts.lallygag.net`) are not
published in public DNS, and there is no inbound path from the internet.

* **HTTP-01** requires the CA to fetch `http://<name>/.well-known/acme-challenge/…` on port 80. Unreachable.
* **TLS-ALPN-01** requires the CA to open a TLS connection on port 443. Unreachable.
* **DNS-01** requires only a `_acme-challenge.<name>` TXT record in a zone the
  CA can resolve. The daemon writes it into public Route 53 outbound; the CA
  reads it from public DNS. No inbound reachability at all.

Only DNS-01 is implemented, and HTTP-01/TLS-ALPN-01 are explicitly disabled in
the ACME issuer so no validation attempt is ever wasted on them.

Note the split: the `_acme-challenge` TXT records live in the **public**
`lallygag.net`/`ts.lallygag.net` Route 53 zone, while the A/AAAA records the
phone resolves come from **MagicDNS inside the mesh**. The public zone never
needs an address record for the daemon.

## Prerequisites

1. A public Route 53 hosted zone covering the daemon's name
   (e.g. `ts.lallygag.net`, delegated from `lallygag.net`).
2. AWS credentials on the daemon host with the IAM policy below. They come
   from the standard AWS chain — env vars, `~/.aws/config` profile, or an
   instance/role credential. mcremote never stores them.
3. The name the phone will dial, e.g. `devbox.ts.lallygag.net`.

## IAM policy

> **Applying this for the first time?** Follow
> [iam-route53-acme.md](iam-route53-acme.md) instead — it is the apply-ready
> runbook (zone selection, attach commands, verification) and the source of
> truth for this policy. The copy below is reproduced for reference.
>
> In particular it covers a trap this section glosses over: with MagicDNS,
> `_acme-challenge.<host>.ts.lallygag.net` is normally answered by the
> **`lallygag.net`** public zone, not by `ts.lallygag.net`, because the
> MagicDNS base domain is served inside the tailnet and is often not delegated
> publicly at all.

Scope the credentials to challenge records in the one zone. `GetChange` is
required because the Route 53 API is eventually consistent, and
`ListHostedZonesByName` is required unless you pin `hosted_zone_id` (with it
set, you may drop that statement).

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AcmeChallengeRecords",
      "Effect": "Allow",
      "Action": "route53:ChangeResourceRecordSets",
      "Resource": "arn:aws:route53:::hostedzone/Z0123456789ABCDEFGHIJ",
      "Condition": {
        "ForAllValues:StringEquals": {
          "route53:ChangeResourceRecordSetsRecordTypes": ["TXT"],
          "route53:ChangeResourceRecordSetsActions": ["CREATE", "UPSERT", "DELETE"]
        },
        "ForAllValues:StringLike": {
          "route53:ChangeResourceRecordSetsNormalizedRecordNames": ["_acme-challenge.*"]
        }
      }
    },
    {
      "Sid": "ReadChangeStatus",
      "Effect": "Allow",
      "Action": [
        "route53:GetChange",
        "route53:ListResourceRecordSets"
      ],
      "Resource": "*"
    },
    {
      "Sid": "FindTheZone",
      "Effect": "Allow",
      "Action": [
        "route53:ListHostedZones",
        "route53:ListHostedZonesByName"
      ],
      "Resource": "*"
    }
  ]
}
```

The condition keys restrict the credential to `_acme-challenge` TXT records:
it cannot touch your A records, MX records, or NS delegation even if the host
is compromised.

## Staging first

Let's Encrypt production has hard rate limits (50 certs per registered domain
per week, 5 duplicate certs per week, and — the one that usually bites — 5
failed validations per account/hostname/hour). Get the plumbing right against
staging, which issues untrusted certificates with limits ~100x higher.

```bash
mcremote serve \
  --tls-domain devbox.ts.lallygag.net \
  --tls-email ops@lallygag.net \
  --tls-route53-zone-id Z0123456789ABCDEFGHIJ \
  --tls-route53-region us-east-1 \
  --tls-acme-staging
```

A successful run logs `listening … tls_mode=letsencrypt acme_directory=https://acme-staging-v02…`.
A staging certificate will be rejected by the phone (untrusted issuer) —
that is expected; you are only proving that the Route 53 write, the DNS
propagation check, and the CA validation all work.

Then switch to production by dropping `--tls-acme-staging` (or setting
`tls.letsencrypt.staging: false`). Certificates are cached per directory URL,
so the switch triggers a fresh production issuance.

```yaml
tls:
  mode: letsencrypt      # optional; implied by domains + email
  letsencrypt:
    domains: [devbox.ts.lallygag.net]
    email: ops@lallygag.net
    route53:
      hosted_zone_id: Z0123456789ABCDEFGHIJ
      region: us-east-1
```

## Pairing changes in letsencrypt mode

`mcremote pair` behaves differently depending on the mode, because the trust
model differs:

| | `letsencrypt` | `selfsigned` |
|---|---|---|
| Advertised host | primary ACME domain, e.g. `devbox.ts.lallygag.net:7531` | Tailscale IPv4, e.g. `100.64.0.1:7531` |
| `fp=` in the pair QR | present — the **self-signed fallback** leaf | present — the served leaf |
| `mode=` in the pair QR | `letsencrypt` | `selfsigned` |
| Phone validation | platform trust store **or** the pin | fingerprint pin only |

The host must be the DNS name in letsencrypt mode: the certificate has no IP
SAN (Let's Encrypt never issues for an IP, and `100.64.0.0/10` is CGNAT space
that is not even eligible), so dialling `wss://100.64.0.1:7531` would fail
hostname verification every time. `--host` still overrides, and
`MCREMOTE_PAIR_HOST` is ignored in letsencrypt mode.

### Why letsencrypt mode still carries a fingerprint

The pair QR carries `fp=` in letsencrypt mode too, but the phone applies it as
*chain valid **or** pin matches* — never as the sole rule. That distinction is
what answers the original objection to pinning here (certmagic renews roughly
every 60 days, so a leaf pin would break at the first renewal): under *or*, a
stale pin is simply never consulted, because the chain still validates.

The pin exists for the failure path below. And because `mcremote pair` can run
before `serve` has ever obtained an ACME certificate, the fingerprint it
advertises is deliberately the **self-signed fallback leaf** — the very
certificate the daemon serves when issuance fails — rather than the ACME leaf.

That is the useful choice in both branches: while ACME is healthy the pin is
unused, and when ACME breaks the pin is the only thing that keeps an
already-paired phone connecting. Advertising the ACME leaf instead would be
correct exactly when nothing is wrong and useless in the one situation a pin is
for. The limitation to be aware of is the flip side: **in letsencrypt mode the
advertised `fp` is not the certificate a healthy daemon serves**, so a client
must never treat it as exclusive.

Running `mcremote pair` in letsencrypt mode therefore also materialises the
self-signed fallback identity under the data dir, before it is ever needed.

## Renewal and the 90-day cliff

Let's Encrypt certificates are valid for 90 days. certmagic runs a maintenance
goroutine that renews at about two-thirds of the lifetime (~30 days before
expiry), reusing the same DNS-01 solver — so the AWS credentials must stay
valid for the life of the deployment, not just first issuance.

**A host that is powered off or off the network for more than ~90 days comes
back with an expired certificate.** The phone fails closed: TLS validation
rejects the expired leaf and the app cannot connect, and it cannot be repaired
from the phone. Recovery is on the daemon side — bring the host up with
working AWS credentials and let certmagic renew (issuance is attempted
synchronously at startup), or run in `selfsigned` mode for hosts that are
routinely dark for months.

## Failure handling

ACME failure is never fatal. If issuance or renewal fails at startup — bad
credentials, wrong zone, rate limit, no network — the daemon logs an
`ERROR`-level message naming the domains and directory, then falls back to the
self-signed identity and comes up anyway. The startup log line carries
`tls_mode=selfsigned tls_letsencrypt_fallback=true`.

**Phones paired against the public name keep connecting through this.** The
fallback leaf includes the configured ACME domains in its SANs, so the only
thing it fails is issuer trust — and the pin the QR advertised in letsencrypt
mode is precisely this leaf, so the client's *chain or pin* rule accepts it. The
daemon stays reachable while you fix the credentials or the zone.

(Before this was fixed the QR carried no pin in letsencrypt mode, and every ACME
failure — however transient — locked out every paired device permanently. If
your phone was paired with an older build, re-pair once to pick up the `fp`.)

Watch for it:

```bash
journalctl --user -u mcremote | grep -i "Let's Encrypt issuance FAILED"
```

Common causes:

* `AccessDenied` on `ChangeResourceRecordSets` — the IAM policy above is not attached, or the zone ID is wrong.
* Timeout in the propagation check — the name is delegated to a different nameserver than the zone you are writing to. Verify with `dig +short TXT _acme-challenge.devbox.ts.lallygag.net @ns-xxx.awsdns-yy.com`.
* `urn:ietf:params:acme:error:rateLimited` — you skipped staging.

## Reverting to self-signed

```bash
mcremote serve --tls-mode selfsigned
mcremote pair code --name phone --qr    # re-pair: the QR now says mode=selfsigned
```

Phones paired in letsencrypt mode must be re-paired: the mesh IP host differs
from the ACME domain, and the acceptance rule tightens from *chain or pin* to
*pin only*.
