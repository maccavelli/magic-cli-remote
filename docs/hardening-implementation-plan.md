# Hardening implementation plan

**Status:** Phases 1, 2, 3 **implemented and verified** (2026-07-20). Phases 4-6
outstanding. See per-item notes.

**Progress:** Phase 1 (front door) ✅ · Phase 2 (both TLS modes safe) ✅ ·
Phase 3 (client identity, ADR 0005) ✅ — cross-stack SPKI fingerprint agreement
confirmed Go ↔ Dart ↔ openssl · Phase 4 (operability) ✅ — 4.1 no-op (D5), 4.2
`/v1/hello` TLS status, 4.3 rotation-vs-attack wording, 4.4 fallback WARN ·
Phase 5 (structural gaps) ✅ — ADRs 0006/0007/0008: `pair prune` + `/v1/hello`
key enforcement built, tailnet-lock and Headscale-certs documented-and-deferred
(both blocked upstream). **Phase 6 (backlog) remains.**
**Date:** 2026-07-20
**Companion:** [0004-certificate-management-decision.md](0004-certificate-management-decision.md)

Consolidates the findings from the mobile deep scan, the protocol cross-check,
the TLS work, and the three-lens certificate review into one ordered,
reviewable plan.

---

## Baseline — already shipped in `cd27bc6`

For reviewer context. Do not redo.

| Area | State |
|---|---|
| Permission lifecycle | `permission_resolved` event; client holds a map and drains a queue. Composer no longer locks. |
| Reducer correctness | Cancel dedup, tool-name clobbering, no-op short-circuits, stable `seq` keys |
| Missing wiring | `session.delete`, session resume params, status resync, transcript eviction |
| UI lifecycle | ~15 `mounted`/`ref` guards, double-tap guards, background suspension, scanner lifecycle |
| Storage | No cleartext token path on mobile; `allowBackup=false`; FLAG_SECURE; release signing |
| Transport | TLS with self-signed + fingerprint pinning; Let's Encrypt DNS-01 |
| Tests | 95 Flutter, all Go packages green; `flutter analyze` clean |

**Verification gate for every phase below:** `flutter analyze` clean,
`flutter test` green, `go build ./... && go vet ./... && go test ./...` green,
**and `flutter build apk --debug` succeeds.** The build step is not optional:
analyze and test do not invoke Gradle's Android resource/dependency packaging,
so a malformed `res/xml` file or a bad native dependency passes both and only
fails at build (an XML-comment defect shipped exactly this way once).

---

## Severity model

| P | Meaning |
|---|---|
| **P0** | Remotely reachable path to code execution, or a failure with no user recovery |
| **P1** | Correctness/robustness defect with a workaround |
| **P2** | Structural gap requiring a decision before implementation |
| **P3** | Backlog |

## Decisions recorded at review

| # | Decision |
|---|---|
| D1 | **1.1** — `tailscale` bind is the default for *all* launch paths including the system unit, for the time being. Revisit if the AWS box needs LAN reachability. |
| D2 | **1.3** — `/v1/hello` is **authenticated**, not stripped. Diagnostics retained behind auth. |
| D3 | **Client identity / mTLS promoted to Phase 3**, ahead of operability, because it may change the transport design. |
| D4 | **Backlog is in scope** (now Phase 6) — executed last, after every other phase is complete and tests pass. |
| D5 | **4.1 MagicDNS** — option 3: IP-dialled hosts stay on `selfsigned`; `override_local_dns` stays `false`. |
| D6 | **Phase 3 client identity** — public-key allowlist, SSH-style. **No CA.** Recorded as [ADR 0005](0005-client-identity-decision.md). |
| D7 | **3.2e enforcement ships default-ON.** The fleet is a single phone owned by the operator, so the staged keyless→keyed migration in ADR 0005 is unnecessary. Re-pairing that one device is the accepted cost. Revisit if a second device is ever enrolled. |

---

# Phase 1 — Close the open front door (P0)

**Rationale.** All three reviews independently ranked this above the
certificate question. Today a bearer token is the only control between any
tailnet node — or any host on the same café wifi — and arbitrary code execution
as the user. This phase is independent of the TLS mode and should land first.

### 1.1 Bind to the tailnet interface, not `0.0.0.0` — **S**

`0.0.0.0` is the default in six shipped launch paths, contradicting the
project's own checklist (`docs/0002-…:278`, `:303`).

| File | Line |
|---|---|
| `deploy/systemd/mcremote.service` | 13 (and commented env at 18) |
| `deploy/systemd/mcremote.user.service` | 24 |
| `scripts/start-mcremote-grok.sh` | 137 |
| `internal/cli/setup_service.go` | 68 (flag default) |
| `configs/config.mesh-grok.yaml` | 5 |
| `configs/config.prod.example.yaml` | 4 |

**Change.** Introduce a `listen.host: "tailscale"` sentinel resolved at startup
to the Tailscale IPv4. `internal/config/config.go:343` (`Addr()`) already feeds
`net.Listen("tcp", …)` at `internal/daemon/daemon.go:121`, and
`internal/cli/pair.go:345` already has the detection logic — factor it into a
shared helper rather than duplicating.

**Fail closed:** if the sentinel is set and no Tailscale IPv4 exists, refuse to
start with an actionable error. Do **not** silently widen to `0.0.0.0`.

**D1 — applies to every launch path**, including the system unit
`deploy/systemd/mcremote.service`. `0.0.0.0` remains available as an explicit
opt-in but is no longer any default. Extend the guard at `daemon.go:46` to warn
on `0.0.0.0` regardless of `RequireDeviceToken`.

> Revisit D1 if the AWS box ever needs to serve clients that are not on the
> tailnet. Today it does not, and Phase 1.2 assumes it does not.

**Acceptance:** with the default config, `ss -ltnp` shows 7531 bound to the
`100.x` address only; a request to the daemon's LAN IP is refused.

### 1.2 Ship a deny-by-default Headscale ACL — **M**

`docs/headscale.md:117-127` ships `"src": ["*"], "dst": ["*:*"]` with a "tighten
later" note that was never actioned. `configs/config.prod.example.yaml:4`
justifies its `0.0.0.0` bind with "only with Headscale grants locking TCP 7531"
— grants that do not exist anywhere in the repo.

**Change.** Write a real policy consuming the `tag:mcremote-host` /
`tag:mcremote-client` tags the docs already tell operators to apply
(`headscale.md:215-220`): deny by default, allow `tag:mcremote-client` →
`tag:mcremote-host:7531` only. Replace the allow-all example rather than
leaving it as a "first step". Document the Headscale v0.29 `tagOwners` caveat
already noted at `headscale.md:115`.

Also tighten enrolment: `headscale.md:135,142` uses `--reusable` 48h pre-auth
keys. Recommend single-use keys with short expiry.

**Acceptance:** a tailnet node without `tag:mcremote-client` cannot open a TCP
connection to 7531 on a tagged host.

### 1.3 Authenticate pre-auth disclosure — **S**

`internal/ws/server.go:84-85` exposes `GET /healthz` and `GET /v1/hello` with
no authentication. `handleHello` (`:125-133`) returns the version, the listen
address, **and the Headscale control URL** — reconnaissance that identifies the
service and points at the coordination server.

**D2 — authenticate `/v1/hello`, do not strip it.** The diagnostic value is
worth keeping; it just must not be readable by an unauthenticated caller.

**Change.** Require a valid device token for `/v1/hello`, returning 401
otherwise. Keep `/healthz` unauthenticated but reduce it to a liveness boolean —
it is the natural monitoring probe, and **4.2** adds fields that must not be
public.

**Acceptance:** unauthenticated `GET /v1/hello` returns 401 and discloses
nothing about the mesh; with a valid token it returns the current payload.
`GET /healthz` still answers unauthenticated with liveness only.

---

# Phase 2 — Make both TLS modes safe (P0/P1)

Implements [ADR 0004](0004-certificate-management-decision.md). **The default
stays conditional** — `selfsigned` unless a domain *and* email are configured.
This phase does not change mode selection; it fixes how each mode fails.

### 2.1 Make the Let's Encrypt fallback functional — **M** — P0

**The defect.** `TLSConfig.Pinned()` (`config.go:199-201`) is true only for
`selfsigned`, so `pairFingerprint()` (`pair.go:258-261`) returns empty in LE
mode and the QR carries no pin. But `internal/daemon/certs.go:113-118` falls
back to a self-signed certificate when ACME fails. The daemon comes up serving
a certificate with correct SANs, and the phone — holding no pin, doing chain
validation — rejects it permanently.

Every LE failure (undelegated zone, expired Route 53 credential, rate limit,
90-day dark host, no network at boot) funnels into this one unrecoverable
state. The fallback preserves the daemon process and nothing the user needs.

**Change.**

1. Emit the fingerprint in the pair QR in **both** modes. Decouple "which
   fingerprint to advertise" from `Pinned()`.
2. Carry the mode in the pair payload so the client knows which rule to apply.
3. Client acceptance:

   | Mode | Rule | Trust set |
   |---|---|---|
   | `selfsigned` | fingerprint only (`withTrustedRoots: false`) — unchanged | exactly 1 cert |
   | `letsencrypt` | valid chain **OR** fingerprint match | public CAs for that name ∪ this cert |

`CertPinner` (`apps/mobile/lib/data/ws/mcremote_client.dart:28-97`) already has
the right shape; LE mode needs a variant that attempts platform validation
first and consults the pin only on failure.

Renewal stays transparent: the chain keeps validating, so a stale pin is never
load-bearing. The original reason for omitting the pin — a leaf pin breaks on
60-day renewal — is answered by *or* rather than *and*.

**Tests.** Extend `apps/mobile/test/cert_pinning_test.dart`: LE mode accepts a
trusted chain with a stale pin; LE mode accepts an untrusted chain with a
matching pin (the fallback case); LE mode rejects untrusted chain + wrong pin.
Note the existing file's ordering constraint — the trust-store group must stay
last (documented in-file).

**Acceptance:** kill ACME (bad zone), restart the daemon, and a
previously-paired phone still connects.

### 2.2 Drop `IsCA` / `KeyUsageCertSign` from the serving leaf — **S** — P1

`internal/certs/certs.go:162-168` mints the serving certificate with
`IsCA: true` and `KeyUsageCertSign`, valid ~10 years, stored beside its key.

Inert while pinned. But the natural operator action — installing it in a system
or browser trust store so `curl`/a browser works — yields a CA capable of
signing for **any name**, for a decade.

**Change.** `IsCA: false`, drop `KeyUsageCertSign`, keep
`ExtKeyUsageServerAuth`. Existing certs regenerate on next `Ensure()`.

**Migration:** this changes the fingerprint, so paired devices must re-pair.
Bundle with 2.3/2.4/2.4a into a single re-pair event, and call it out in
release notes.

**Tests.** Assert `IsCA == false` and no `CertSign` in `internal/certs/certs_test.go`.

### 2.3 Stop regenerating identity on any load error — **S** — P1

`internal/certs/certs.go:92-107`: `Ensure()` falls through to `generate()`
whenever `load()` errors — not only when the file is absent or expired. A
transient read failure (permissions, partial write, full disk, NFS blip)
silently mints a **new identity** and invalidates every paired device.

**Change.** Distinguish *absent* (generate — correct first-run behaviour) from
*present but unreadable* (fail loudly; do not re-identify). Log distinctly on
deliberate regeneration so `cert_generated=true` (`daemon.go:137`) means
something specific.

**Tests.** Corrupt `tls.crt` → `Ensure()` errors and leaves files untouched.
Absent files → generates. Expired → regenerates.

### 2.4 Fix the single-slot pin store — **M** — P1

`apps/mobile/lib/data/local/settings_store.dart:51-53` keeps **one**
fingerprint under one key, scoped to one host authority (`:95-119`).
Alternating between two daemons silently discards the other's pin and forces a
QR rescan on every switch.

This is the entire ongoing cost people attribute to pinning, and it is a client
bug, not a property of the approach. Fixing it removes the strongest
operational argument against `selfsigned`.

**Change.** Store a map of identity → fingerprint in secure storage. Preserve
the existing scoping semantics (never return another host's pin) and the mobile
no-cleartext-fallback guarantee. Migrate the existing single-value keys on first
read.

**Tests.** Pin host A, pin host B, confirm A still validates; migration from
the old single-slot format.

### 2.4a Survive tailnet IP churn — **S** — P1 *(consequence of D5)*

Pins are keyed on the host authority via `_authorityOf`
(`settings_store.dart:122-129`), i.e. `host:port`. With **D5** committing to
dialling hosts **by IP**, the pin key is now an address that can change: a node
deleted and re-registered in Headscale gets a new `100.x`, the pin lookup
misses, the connection falls through to unpinned, and the user is told to
re-pair — even though the certificate never changed.

Headscale IPs are stable per node in normal operation, so this is infrequent
rather than routine. But it is the one real cost of the IP-dialling decision,
and it lands on the user as a spurious "re-pair" prompt indistinguishable from
a genuine identity change.

**Change.** While rewriting the pin store for 2.4, key pins on a **stable
identity** rather than the dialled address. `device_id` is already issued at
pair time and persisted (`settings_store.dart` `_kDeviceId`,
`connect_screen.dart:323`), which makes it the natural candidate. Keep the host
authority as a secondary check, not the primary key.

Fallback if that proves impractical: on a pin miss where the fingerprint is
otherwise unknown, present a *re-confirm* prompt showing the fingerprint rather
than a hard failure — but **only** when no pin exists for that identity, never
when a pin exists and mismatches, which must stay a hard, loud failure.
Getting that asymmetry backwards silently converts pinning into TOFU on every
reconnect.

**Acceptance:** re-register a node so its tailnet IP changes; a previously
paired phone reconnects without a QR rescan.

**Tests.** Pin at `100.64.0.1:7531`, re-key to `100.64.0.9:7531` with the same
device identity and certificate → still validates. Same address, *different*
fingerprint → still a hard `cert_mismatch`.

---

# Phase 3 — Client identity (P2 — ADR first) *(promoted by D3)*

**Why this moved ahead of operability.** Both certificate strategies
authenticate only the **server**. The daemon cannot identify which node is
connecting — `internal/ws/server.go` has no peer-identity plumbing — so a
stolen bearer token works from anywhere on the tailnet. WireGuard authenticates
nodes, but that identity never reaches the application layer.

This is a larger gap than the server-certificate choice that consumed the last
review, and it can change the transport design. Settling it before Phase 4
avoids building operability tooling against a transport that then changes.

### 3.1 Write the ADR — **M**

Decide between, at minimum:

* **Client-key allowlist (recommended — see guidance below)** — the phone
  generates a keypair at pair time and presents a self-signed client
  certificate; the daemon accepts it only if the public key matches the one
  recorded for that device. No CA anywhere.
* **mTLS with a daemon-issued client CA** — the daemon signs client certs.
  Strongest conventional binding, but introduces a CA key with its own
  lifecycle and blast radius. See "the CA question" below before choosing.
* **Do nothing, rely on Phase 1** — the ACL plus tailnet bind reduce exposure
  to enrolled nodes only. Cheapest; leaves lateral movement inside the tailnet
  unaddressed.

Must address: enrolment flow (the QR already carries a token — can it carry a
client key?), revocation, rotation, and what happens to already-paired devices.

#### The CA question — guidance

**Do not sign client certificates with the serving key.** This is not a style
preference; it is incompatible with the architecture in three ways:

1. **It breaks under `letsencrypt` mode.** In LE mode the serving certificate
   is issued by Let's Encrypt. You do not hold a CA key for it and it carries
   no `CertSign`. So a design that signs client certs with the serving key
   works *only* in `selfsigned` mode, fragmenting client identity by TLS mode.
   This alone is decisive.
2. **It reverts 2.2.** Restoring `CertSign` to the serving leaf reinstates the
   trust-store footgun that 2.2 exists to remove — and mTLS setups increase the
   likelihood an operator installs that certificate somewhere.
3. **It couples two lifecycles that must move independently.** The serving cert
   rotates on host rebuild, on regeneration, and every ~60 days under LE. A
   client trust anchor must be *stable* — rotating it invalidates every issued
   client credential, i.e. every paired device. Coupling them means each server
   cert rotation forces a fleet-wide re-enrol.

Blast radius differs too: serving-key compromise lets an attacker impersonate
*one daemon*. Client-CA compromise lets them mint credentials any daemon
trusting that CA accepts — full RCE. These should not share fate, share
storage, or share a rotation schedule.

**Recommended shape: a public-key allowlist, not a CA.**

Model it on SSH `authorized_keys` rather than on PKI:

* At pair time the phone generates a keypair and sends the public key; the
  daemon stores its fingerprint on the existing device record.
  `internal/auth/store.go:29-36` (`deviceRecord`) already holds `ID`, `Name`,
  `TokenHash` and timestamps — a `ClientKeyFingerprint` field is purely
  additive.
* On connect the client presents a **self-signed** client certificate. The
  daemon uses `tls.Config{ClientAuth: RequireAnyClientCert}` with a
  `VerifyPeerCertificate` hook that checks the presented key against the store.
  **No `ClientCAs` pool, no CA key, nothing to protect or rotate.**
* Revocation is deleting the record — `Store.Revoke` (`store.go:120`) already
  does exactly this, and would now revoke transport access rather than just a
  bearer secret.

This resolves the tension by removing it: there is no CA, so there is no
conflict with 2.2, and it behaves identically in `selfsigned` and
`letsencrypt` modes because client identity becomes fully orthogonal to server
identity. It also composes with 5.1 — a token bound to a key is no longer a
bearer credential, which is most of what 5.1 is asking for.

**If the ADR nonetheless chooses a real CA**, it must be a *separate* key with
its own file, its own permissions, its own long rotation schedule, and no
relationship to `tls.crt`/`tls.key`. Document how it is backed up: losing it
means re-enrolling every device.

**Implementation risk to size during the ADR.** Dart has no built-in X.509
generation. `SecurityContext` can *consume* a client certificate
(`useCertificateChainBytes` / `usePrivateKeyBytes`), but minting a self-signed
client cert on-device needs a package such as `basic_utils` or `pointycastle`.
Verify this works on Android — including where the private key lives, ideally
the Android Keystore — before committing to the approach. This is the main
unknown in the recommended design.

### 3.2 Implement the public-key allowlist — **L**

**ADR accepted:** [0005-client-identity-decision.md](0005-client-identity-decision.md).
Public-key allowlist, SSH-style. No CA.

**3.2a — Spike: DONE (2026-07-20), design confirmed.** A standalone Dart↔Go
spike verified keypair generation, self-signed client certificate construction,
`SecurityContext` client auth, Go-side `RequireAnyClientCert` +
`VerifyPeerCertificate` with **no `ClientCAs` pool**, exact fingerprint
agreement with `openssl`, and rejection of both a non-enrolled key and a
missing certificate. See the spike table in ADR 0005.

Two carry-overs into implementation:
* Fingerprint the **`RawSubjectPublicKeyInfo`**, not the certificate DER — the
  cert may be regenerated around the same key, and the key is the identity.
* **Rejection is illegible on the client** (generic `HttpException: Read
  failed`). Handle enrolment failure at the protocol layer with a typed error
  rather than letting it fail at TLS — see 3.2b.
* Remaining: confirm on a real Android device (Dart uses BoringSSL on both, so
  this is verification, not exploration).

**3.2b — Daemon.** Add `ClientKeyFingerprint` to `deviceRecord`
(`internal/auth/store.go:29-36`, additive, optional). Set
`tls.Config{ClientAuth: tls.RequireAnyClientCert}` with a
`VerifyPeerCertificate` hook comparing the presented key against the store.
No `ClientCAs` pool. Do **not** validate client certificate expiry — identity
is the key, deliberately, as with SSH.

**3.2c — Client.** Generate the keypair at pair time, store it in
`flutter_secure_storage` alongside the token, present it on every connection.

**3.2d — Enrolment protocol.** Carry the public key in the pair-claim payload;
document in `docs/protocol-v1.md`.

**3.2e — Enforcement flag, default ON (D7).** A client key is required to
connect. The keyless path still exists behind the flag for anyone who needs the
staged migration in ADR 0005, but it is not the default here: the fleet is one
operator-owned phone, and re-pairing it once is cheaper than carrying a
migration mode. Revisit if a second device is enrolled.

**Tests.** Handshake succeeds with the enrolled key; fails with a different
key; fails with no client certificate once enforcement is on; still succeeds
for a keyless device while enforcement is off; `Revoke` denies transport access
immediately.

**Acceptance:** a token copied to a device that holds no enrolled key cannot
connect while enforcement is on.

**Dependency:** Phase 4.2 and 4.3 follow this, since both surface transport
state whose shape depends on the outcome here.

---

# Phase 4 — Operability (P1)

### 4.1 MagicDNS gap — **DECIDED (D5): option 3, no code change**

`docs/headscale.md:91` sets `override_local_dns: false`, so Headscale does not
push MagicDNS to clients and the phone may not resolve
`devbox.ts.lallygag.net` at all. The natural workaround — dialling the raw
`100.x.y.z` — can **never** work under Let's Encrypt, which does not issue for
IP addresses (`100.64.0.0/10` is CGNAT).

**Decision (2026-07-20): keep IP-dialled hosts on `selfsigned`.**
`override_local_dns` stays `false`.

Rationale. Under pinning the client validates by fingerprint alone
(`withTrustedRoots: false`), so SANs, `NotAfter`, `NotBefore` and the phone's
clock are all irrelevant — dialling by IP is *correct* here, not a workaround.
This costs nothing, is already the automatic default for hosts with no domain
configured, and avoids perturbing DNS on a host with a documented split-horizon
quirk (`headscale.md:43-50`).

**Consequences:**

* No DNS change, no public zone delegation, no Route 53 IAM, no ACME for
  laptop and home-server daemons.
* Browser / `curl` / third-party clients remain unsupported without a per-host
  trust-store install. Acceptable while the Flutter app is the only client.
* Makes **2.4a** load-bearing: with IP dialling, pins are keyed on an address
  that can change.

**Deferred, not rejected:** `override_local_dns: true` becomes the preferred
path if a browser client is wanted, since browsers cannot pin. At that point
Let's Encrypt on the always-on AWS box is nearly free — an instance role
supplies credentials with no rotation burden. Revisit then; nothing in this
plan forecloses it.

### 4.2 Make the TLS mode legible — **S** — ✅ DONE

`/v1/hello` now returns `tls_mode` and `tls_fell_back` (authenticated, per D2 —
never on public `/healthz`). `Server.SetTLSStatus` is called from `daemon.Run`
once the cert is resolved; `TestHTTPEndpointAuth` asserts the fields are present
when authorized and absent in the 401 body. `mcremote pair` already names the
mode (Phase 2, `printFingerprint`).

### 4.3 Distinguish rotation from attack — **S** — ✅ DONE

The `cert_mismatch` message now frames the security decision explicitly: it is
expected after a host rebuild / data-dir reset (re-pair), and otherwise a
possible impersonation (do NOT re-pair until the fingerprint is confirmed on the
host via `mcremote pair`). `connect_screen` surfaces it verbatim through the
existing re-pair affordance.

Note the honest limit: the client cannot *cryptographically* tell rotation from
attack — the pinned connection is rejected before any server signal — so this is
a UX framing of the user's decision, not a detection mechanism.

### 4.4 Alerting — **S** — ✅ DONE (log side)

The fallback is now a distinct `WARN` in `daemon.Run` (`tls_letsencrypt_fallback=true`
with actionable text), not just an attribute on the `listening` line — so it is
greppable and alertable. Combined with 4.2's pollable `tls_fell_back`, an
operator has both a push (log) and pull (HTTP) signal.

External alert *wiring* (a monitor that emails on the WARN or polls
`/v1/hello`) is deployment config, not code, and remains the operator's to set
up. Low priority under D5 — laptop/home daemons don't use ACME.

---

# Phase 5 — Remaining structural gaps (P2 — ADR each) — ✅ RESOLVED

Each item was analysed against the actual code and current upstream capability,
and recorded as an ADR. Net: one small build (5.1), two document-and-defer.

### 5.1 Token lifecycle — ✅ [ADR 0006](0006-token-lifecycle-decision.md)

**Close it.** Token crypto is already sound (256-bit `crypto/rand`, SHA-256 at
rest — correct for a high-entropy token), and Phase 3's key binding subsumes the
threats expiry/rotation/scope would address. **Built:** `Store.Prune` +
`mcremote pair prune` (`--stale`/`--keyless`) to reap stale/legacy records, and
closed a real residual — `/v1/hello` now enforces the client key, so a stolen
token alone no longer leaks the Headscale control URL. Rejected expiry,
rotation, scopes, bcrypt, and JWT with reasons in the ADR.

### 5.2 Tailnet lock — ✅ [ADR 0007](0007-tailnet-lock-decision.md)

**Document-and-defer.** Headscale has no network-lock
([#1307](https://github.com/juanfont/headscale/issues/1307), open since 2023,
maintainers declined). No idiomatic primitive to enable; a home-grown one would
be theatre or dangerous crypto. The shipped compensating control — out-of-band
server pinning (Phase 2) + client-key allowlist (Phase 3) — already gives
mesh-independent mutual auth for the app channel, surviving control-plane
compromise. D5 is now recorded as an explicit 5.2 mitigation (keeps the app
channel on pin-only). Residual (availability, LE name-controller caveat,
non-pinning consumers) named and accepted.

### 5.3 Headscale-issued certificates — ✅ [ADR 0008](0008-headscale-certs-decision.md)

**Defer.** `tailscale cert` is unimplemented in Headscale
([#2527](https://github.com/juanfont/headscale/issues/2527), ~v0.34; existing
ACME is HTTP-01/TLS-ALPN-01 only, no DNS-01). Under D5 only the named AWS box
could use it, and that already has a near-free instance-role LE path. Concrete
reopen trigger recorded.

---

# Phase 6 — Backlog (P3) *(in scope per D4 — execute last)*

**Execute only after Phases 1-5 are complete and all tests pass.** These are
known, low-severity items from the deep scan; none blocks anything above.

| Item | Location |
|---|---|
| `_presentedPermissionIds` grows unbounded for the widget's lifetime; never pruned against resolved ids | `chat_screen.dart:29` |
| Interleaved thought/assistant chunks produce one bubble per chunk — coalescing only merges when the item is last | `transcript_reducer.dart` |
| ACP `Plan`/`PlanRemoved` updates logged and dropped server-side; agent plan output invisible on mobile | `grok/session.go:337-341` |
| No transcript replay — opening a running session shows empty history | protocol gap; needs a `session.get`/history request |
| `prefer_initializing_formals` info — satisfying it means renaming a public named parameter to `_prefs`; previously judged not worth it, revisit | `settings_store.dart:37` |
| Stale doc drift — port 7910 named against the 7531 decided in `docs/0003:20` | `docs/0002-…:301` |

Transcript replay is the largest of these and is a protocol addition, not a
fix; consider splitting it out if it grows.

---

# Sequencing and dependencies

```
Phase 1  ──────────────►  independent, land first
   1.1 bind ──► 1.2 ACL (bind makes ACL meaningful)
   1.3 auth ──► 4.2 (4.2 publishes to the endpoint 1.3 protects)

Phase 2  ──────────────►  independent of Phase 1
   2.2 + 2.3 + 2.4 + 2.4a ──► single re-pair event, ship together
   2.4 ──► 2.4a (same storage rewrite; do them as one change)
   2.1 ──► independent, highest value in this phase

Phase 3  ──────────────►  ADR 0005 accepted (D6)
   3.2a spike ──► 3.2b-e (spike gates everything; failure reopens the ADR)
   3.2  ──► 5.1 (token lifecycle largely subsumed by key binding)
   3.2  ──► 4.2, 4.3 (transport shape informs what is surfaced)

Phase 4  ──────────────►  after 1.3, 2.x and Phase 3
Phase 5  ──────────────►  ADR each, then re-plan
Phase 6  ──────────────►  last, after 1-5 complete and green
```

**Ship 2.2/2.3/2.4/2.4a as one release.** 2.2 changes the fingerprint and
forces a re-pair; bundling avoids asking users to re-pair twice.

---

# Migration and rollout

| Change | User impact | Mitigation |
|---|---|---|
| 1.1 tailnet bind | Hosts reached by LAN IP stop working — **now applies to the system unit too (D1)** | Explicit `0.0.0.0` opt-in remains; stage on the AWS box first; release note |
| 1.2 ACL | Untagged nodes lose access | Document tagging before enforcing |
| 1.3 `/v1/hello` auth | Any unauthenticated consumer breaks | Grep for callers first; `scripts/smoke-protocol` may probe it |
| 2.1 LE fallback | None — strictly more available | — |
| 2.2 non-CA leaf | Fingerprint changes → **all devices re-pair** | Bundle with 2.3/2.4/2.4a; release note |
| 2.4 pin map | None — migrates old format | Migration test required |
| 2.4a identity-keyed pins | None — removes a spurious re-pair prompt | Mismatch must stay a hard failure; test both cases |
| 3.2 client identity | Devices without a key keep working until enforcement is flipped on | Ship enforcement **default-off**; migrate the fleet; then require. Never required in the first release. |

Already outstanding from `cd27bc6`: phones paired before the TLS change must
re-pair. Route 53 IAM provisioning is **no longer on the critical path** under
D5 — it is needed only if `letsencrypt` is adopted on the AWS box.

---

# Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| 1.1 locks the operator out of a remote host — higher under D1, which applies it to the system unit | Medium | High | Fail-closed error names the fix; `0.0.0.0` opt-in remains; stage on the AWS box with console access |
| 1.2 ACL misconfiguration bricks phone access | Medium | Medium | Test with a second node before enforcing; Headscale reload is non-destructive |
| 1.3 breaks an unauthenticated consumer of `/v1/hello` | Low | Low | Grep for callers before changing |
| 2.1 two client acceptance rules widen test surface | Low | Medium | Explicit tests per mode; trust sets documented in ADR 0004 |
| 2.4 pin-store migration loses pins | Low | Low | Worst case is one rescan; migration test |
| 2.4a re-confirm path degrades pinning to TOFU | Low | **High** | Only ever on *absent* pin, never on mismatch; test both explicitly |
| 3.2a spike fails — Dart cannot do TLS client auth on Android | Low | **High** | Spike **before** any other Phase 3 work; failure reopens ADR 0005 rather than wasting 3.2b-e |
| ~~3.2 enforcement shipped required~~ | — | — | **Accepted under D7** — one operator-owned phone; re-pair is the cost |
| Client key lost with no remote recovery | Medium | Low | Re-pair; consistent with the pinning posture in ADR 0004 |
| Re-pair fatigue across releases | High | Low | Bundle 2.2/2.3/2.4/2.4a; possibly bundle 3.2 |

---

# Open questions

All five review questions are resolved (see **Decisions recorded at review**).
New questions arising:

1. ~~**Phase 3.1** — does client identity make the daemon a CA for client
   certs?~~ **Guidance recorded 2026-07-20:** no — use a public-key allowlist
   on the existing device record, not a CA. Signing client certs with the
   serving key is incompatible with `letsencrypt` mode, reverts 2.2, and
   couples two lifecycles that must move independently. See "The CA question"
   in 3.1. Remaining unknown: on-device keypair generation in Dart.
2. **Phase 6 transcript replay** — a protocol addition rather than a fix.
   Split into its own plan if it grows beyond a backlog item.
3. **Phase 3.1** — can the Android Keystore hold the client private key, and
   can Dart use a Keystore-backed key for TLS client auth? If not, the key
   lives in app storage and the security gain over a bearer token narrows.
   Size this before the ADR concludes.
