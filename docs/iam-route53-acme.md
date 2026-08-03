# Route 53 IAM setup for ACME DNS-01

Apply-ready runbook for the credentials `mcremote` needs to obtain Let's
Encrypt certificates via the DNS-01 challenge. This is the source of truth for
the policy; surrounding design and configuration live in the root
[README TLS section](../README.md#tls) and [config.md](config.md).

Everything below assumes the worked example from
[headscale.md](headscale.md): registered domain `lallygag.net`, MagicDNS base
domain `ts.lallygag.net`, daemon name `devbox.ts.lallygag.net`.

---

## Step 0 — Find the zone that actually answers the challenge

**Do this first. Getting it wrong is the most common failure, and the error it
produces (`NXDOMAIN` during validation) does not point at the cause.**

`ts.lallygag.net` is a **MagicDNS base domain**. Headscale serves it *inside
the tailnet*; it is not necessarily delegated in public DNS. Let's Encrypt
validators are on the public internet and resolve
`_acme-challenge.devbox.ts.lallygag.net` through the **public** DNS hierarchy,
so the TXT record must be created in whichever public hosted zone is
authoritative for that name — which is usually **`lallygag.net`, not
`ts.lallygag.net`**.

Check whether `ts.lallygag.net` is publicly delegated:

```bash
dig +short NS ts.lallygag.net @1.1.1.1
```

| Result | Authoritative zone | Use this zone's ID |
|---|---|---|
| **empty** (no delegation — most likely) | `lallygag.net` | `lallygag.net` |
| returns nameservers | `ts.lallygag.net` | `ts.lallygag.net` |

Confirm the zone you picked is the closest public ancestor:

```bash
dig +short SOA lallygag.net @1.1.1.1
```

> If neither zone is public — for example the whole domain is internal-only —
> DNS-01 cannot work as-is. Use the CNAME delegation pattern in
> [Appendix A](#appendix-a--cname-delegation) or run the daemon in
> `selfsigned` mode.

### The phone must also *resolve* the name

DNS-01 only proves you control the name. Separately, the phone has to resolve
that same name to the daemon's tailnet IP, or TLS fails with a hostname
mismatch no matter how good the certificate is.

The intended split is:

| Record | Served by | Purpose |
|---|---|---|
| `_acme-challenge.devbox.ts.lallygag.net` TXT | **public** Route 53 (`lallygag.net`) | Let's Encrypt validation only |
| `devbox.ts.lallygag.net` A | **MagicDNS**, inside the tailnet | what the phone dials |

The public zone never needs an address record for the daemon.

**Check this before deploying:** `docs/headscale.md:91` currently sets

```yaml
override_local_dns: false   # safer on multi-use servers
```

With that setting Headscale does **not** push MagicDNS to clients, so an
Android phone may not resolve `devbox.ts.lallygag.net` at all. The connection
then fails at DNS, or the user falls back to dialling the raw `100.x.y.z`,
which cannot match any Let's Encrypt certificate — LE never issues for IPs, and
`100.64.0.0/10` is CGNAT space.

Pick one before enabling `letsencrypt` mode:

1. **Set `override_local_dns: true`** so MagicDNS reaches clients. Cleanest,
   but it takes over DNS on every device in the tailnet.
2. **Publish a public A record** for `devbox.ts.lallygag.net` → the tailnet IP.
   Works without MagicDNS, but puts your internal addressing in public DNS, and
   some resolvers filter RFC1918/CGNAT answers (DNS rebinding protection).
3. **Stay on `selfsigned`** for hosts the phone reaches by IP. Pinning does not
   care about names, so this is the correct mode for an IP-dialled daemon.

Verify from a phone actually on the tailnet:

```bash
dig +short devbox.ts.lallygag.net       # must return the 100.x tailnet IP
```

### Do not reuse the Headscale name

`headscale.lallygag.net` already runs its own ACME client
(`tls_letsencrypt_hostname`, HTTP-01 on port 80 — `docs/headscale.md:78-80`).
Give the daemon a distinct name. Two ACME clients renewing the same hostname
fight over the certificate and burn the "5 duplicate certificates per week"
limit.

---

## Step 1 — Get the hosted zone ID

```bash
aws route53 list-hosted-zones-by-name \
  --dns-name lallygag.net \
  --query "HostedZones[?Config.PrivateZone==\`false\`].[Name,Id]" \
  --output text
```

Output looks like `lallygag.net. /hostedzone/Z0123456789ABCDEFGHIJ`. You want
the bare ID (`Z0123456789ABCDEFGHIJ`) — strip the `/hostedzone/` prefix.

**Make sure `PrivateZone` is false.** `docs/headscale.md` notes this account
also runs a *private* Route 53 view of `lallygag.net` for the VPC resolver.
Writing challenge records into the private zone is invisible to Let's Encrypt
and fails validation every time. The query above filters it out; verify you got
exactly one row.

```bash
export ACME_ZONE_ID=Z0123456789ABCDEFGHIJ   # substitute yours
```

---

## Step 2 — The policy

Save as `route53-acme-policy.json`, substituting your zone ID.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "WriteAcmeChallengeTxtRecordsOnly",
      "Effect": "Allow",
      "Action": "route53:ChangeResourceRecordSets",
      "Resource": "arn:aws:route53:::hostedzone/Z0123456789ABCDEFGHIJ",
      "Condition": {
        "ForAllValues:StringEquals": {
          "route53:ChangeResourceRecordSetsRecordTypes": ["TXT"],
          "route53:ChangeResourceRecordSetsActions": [
            "CREATE",
            "UPSERT",
            "DELETE"
          ]
        },
        "ForAllValues:StringLike": {
          "route53:ChangeResourceRecordSetsNormalizedRecordNames": [
            "_acme-challenge.*"
          ]
        }
      }
    },
    {
      "Sid": "ReadZoneContents",
      "Effect": "Allow",
      "Action": "route53:ListResourceRecordSets",
      "Resource": "arn:aws:route53:::hostedzone/Z0123456789ABCDEFGHIJ"
    },
    {
      "Sid": "PollChangePropagation",
      "Effect": "Allow",
      "Action": "route53:GetChange",
      "Resource": "arn:aws:route53:::change/*"
    },
    {
      "Sid": "DiscoverZone",
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

### Why each statement exists

| Sid | Why it is needed | Can you drop it? |
|---|---|---|
| `WriteAcmeChallengeTxtRecordsOnly` | Creates and removes the challenge TXT record | No |
| `ReadZoneContents` | The solver reads existing records before an UPSERT | No |
| `PollChangePropagation` | Route 53 is eventually consistent; the solver polls `GetChange` until `INSYNC` before telling Let's Encrypt to validate | No |
| `DiscoverZone` | Maps the domain to a zone ID | **Yes** — drop it if you set `route53.hosted_zone_id` in config (recommended; it is also faster and unambiguous when a private zone shares the name) |

The three condition keys are what make this credential safe: it can write
**only TXT records**, **only** at names beginning `_acme-challenge.`, and
**only** in the one zone. A host compromise cannot repoint your A records, MX
records, or NS delegation.

`ListHostedZones`/`ListHostedZonesByName` and `GetChange` are not
resource-scopable to a single zone — that is an AWS API constraint, not an
oversight. `GetChange` is scoped to the `change/*` ARN here; if your region or
partition rejects that, widen it to `"*"` (it only reveals change status, no
record data).

---

## Step 3 — Attach it

### Option A — EC2 instance role (recommended)

No long-lived keys on disk, and rotation is automatic. Use this when the daemon
runs on EC2.

```bash
aws iam create-policy \
  --policy-name mcremote-acme-dns01 \
  --policy-document file://route53-acme-policy.json

cat > trust.json <<'JSON'
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "Service": "ec2.amazonaws.com" },
    "Action": "sts:AssumeRole"
  }]
}
JSON

aws iam create-role \
  --role-name mcremote-daemon \
  --assume-role-policy-document file://trust.json

aws iam attach-role-policy \
  --role-name mcremote-daemon \
  --policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/mcremote-acme-dns01

aws iam create-instance-profile --instance-profile-name mcremote-daemon
aws iam add-role-to-instance-profile \
  --instance-profile-name mcremote-daemon --role-name mcremote-daemon

aws ec2 associate-iam-instance-profile \
  --instance-id i-0123456789abcdef0 \
  --iam-instance-profile Name=mcremote-daemon
```

Nothing else to configure — the AWS SDK picks the role up from IMDS.

### Option B — IAM user with static keys

For daemon hosts outside AWS (a laptop, a home server).

```bash
aws iam create-user --user-name mcremote-acme
aws iam attach-user-policy \
  --user-name mcremote-acme \
  --policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/mcremote-acme-dns01
aws iam create-access-key --user-name mcremote-acme
```

Put the key on the daemon host as a dedicated profile — **not** in the shell
environment of a shared account:

```ini
# ~/.aws/credentials  (chmod 0600)
[mcremote]
aws_access_key_id = AKIA...
aws_secret_access_key = ...
```

**One IAM user per daemon host.** Sharing one key across hosts means a single
compromise forces you to rotate every host at once.

---

## Step 4 — Point mcremote at it

```yaml
tls:
  mode: letsencrypt
  letsencrypt:
    domains:
      - devbox.ts.lallygag.net
    email: you@example.com
    staging: true          # keep true until a real cert issues cleanly
  route53:
    hosted_zone_id: Z0123456789ABCDEFGHIJ   # the PUBLIC zone from Step 1
    region: us-east-1
    profile: mcremote                       # omit when using an instance role
```

Route 53 is a global service, but the SDK still wants a region; `us-east-1` is
the conventional choice.

---

## Step 5 — Verify before trusting it

Check the credential can do what it needs and nothing more.

```bash
# Should succeed
aws route53 list-resource-record-sets --hosted-zone-id "$ACME_ZONE_ID" \
  --max-items 1 --profile mcremote

# Should be DENIED — proves the record-name condition is working
aws route53 change-resource-record-sets \
  --hosted-zone-id "$ACME_ZONE_ID" --profile mcremote \
  --change-batch '{"Changes":[{"Action":"UPSERT","ResourceRecordSet":{
    "Name":"evil.lallygag.net","Type":"A","TTL":60,
    "ResourceRecords":[{"Value":"1.2.3.4"}]}}]}'
```

If the second command **succeeds**, the conditions are not applied — stop and
recheck the policy before going near production.

Then start the daemon with `staging: true` and confirm issuance, watching the
public TXT record appear and disappear:

```bash
watch -n2 'dig +short TXT _acme-challenge.devbox.ts.lallygag.net @1.1.1.1'
```

Only once that works, set `staging: false`. Let's Encrypt production limits
**5 failed validations per account/hostname/hour**, which is easy to burn
through while debugging IAM.

---

## Appendix A — CNAME delegation

If you would rather not grant write access to your primary zone, delegate just
the challenge name to a throwaway zone:

1. Create a hosted zone `acme.lallygag.net`.
2. In `lallygag.net`, add a static CNAME:
   `_acme-challenge.devbox.ts.lallygag.net` → `devbox.acme.lallygag.net`
3. Scope the policy in Step 2 to the `acme.lallygag.net` zone ID only.

The credential then has no access at all to your real zone. Let's Encrypt
follows the CNAME when validating. The cost is one static CNAME per daemon
name, added by hand.

---

## Troubleshooting

| Symptom | Cause |
|---|---|
| `AccessDenied` on `ChangeResourceRecordSets` | Policy not attached, wrong zone ID, or the name did not start with `_acme-challenge.` |
| `AccessDenied` on `GetChange` | Tighten/widen the `change/*` ARN to `"*"` |
| Validation fails, `NXDOMAIN` | Records went into the **private** zone, or you used `ts.lallygag.net` when it is not publicly delegated — revisit Step 0 |
| `NoSuchHostedZone` | Zone ID belongs to another account or is the private view |
| Validation times out | Propagation slower than the solver's patience; re-run, and confirm the TXT is publicly visible with the `dig` above |
| Worked once, fails ~60 days later | Credential rotated or revoked. Renewal is unattended — an expired key surfaces only as a renewal failure in the daemon log |

Certificates renew at roughly two-thirds of lifetime (~60 days). A host powered
off past expiry returns with a dead cert and the phone refuses it — that is the
intended fail-closed behaviour, not a bug. Bring the host up and renewal
recovers on its own.
