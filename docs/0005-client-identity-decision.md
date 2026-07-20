# 0005 — Client identity

* Status: **Accepted**
* Date: 2026-07-20
* Relates to: [0004-certificate-management-decision.md](0004-certificate-management-decision.md),
  [hardening-implementation-plan.md](hardening-implementation-plan.md) Phase 3

## Context

Both TLS modes decided in ADR 0004 authenticate only the **server**. The daemon
cannot identify which node is connecting — `internal/ws/server.go` has no
peer-identity plumbing — so authorisation rests entirely on a bearer device
token (`internal/auth/store.go`).

That token:

* never expires and has no scope (`deviceRecord` records `CreatedAt` only)
* is transmitted on every authentication
* grants, on acceptance, the ability to create sessions, send prompts, and
  approve tool-execution permission requests — i.e. arbitrary code execution as
  the user

Anyone holding a copy can use it from anywhere on the tailnet. With the
default-allow ACL and all-interfaces bind documented in the hardening plan
(Phase 1), "anywhere" is broad.

WireGuard authenticates *nodes*, but that identity never reaches the
application layer, so the daemon cannot act on it.

## Decision

**Bind each paired device to a public key, and verify possession of the
corresponding private key at TLS handshake time. Do not build a certificate
authority.**

The model is SSH `authorized_keys`, not PKI.

### Enrolment

1. During pairing the phone generates a keypair (P-256) and a **self-signed**
   client certificate wrapping it.
2. It sends the public key alongside the existing pair-claim.
3. The daemon records the public-key fingerprint on the device record and
   returns the device id as it does today.

`internal/auth/store.go:29-36` (`deviceRecord`) already carries `ID`, `Name`,
`TokenHash`, `CreatedAt`, `LastUsedAt`. A `ClientKeyFingerprint` field is
purely additive and needs no schema migration beyond an optional field.

### Connection

The daemon sets `tls.Config{ClientAuth: tls.RequireAnyClientCert}` with a
`VerifyPeerCertificate` hook that compares the presented key's fingerprint
against the store.

**No `ClientCAs` pool. No CA key. Nothing to protect, rotate, or back up.**

Certificate *validity* is deliberately not checked — the client certificate is
self-signed and its expiry is meaningless. Identity is the key, exactly as with
an SSH authorized key.

### Revocation

Deleting the device record. `Store.Revoke` (`store.go:120`) already does this;
it now revokes transport access rather than merely a bearer secret.

## Why not a certificate authority

Signing client certificates with the **serving** key was considered and
rejected on three grounds, the first decisive:

1. **Incompatible with `letsencrypt` mode.** In LE mode the serving certificate
   is issued by Let's Encrypt: no CA key is held and it carries no `CertSign`.
   That design would work only in `selfsigned` mode, fragmenting client
   identity by TLS mode permanently.
2. **Reverts hardening plan item 2.2.** Restoring `CertSign` to the serving
   leaf reinstates the trust-store hazard 2.2 removes — and mTLS deployments
   raise the odds an operator installs that certificate somewhere.
3. **Couples lifecycles that must move independently.** The serving certificate
   rotates on host rebuild, on regeneration, and roughly every 60 days under
   LE. A client trust anchor must be stable; rotating it invalidates every
   issued client credential. Coupled, each server rotation forces a fleet-wide
   re-enrol.

Blast radius also differs by an order of magnitude: serving-key compromise
allows impersonating *one daemon*; client-CA compromise allows minting
credentials that any trusting daemon accepts. These must not share fate.

A **separate** client CA was also considered. It is defensible but strictly
more machinery than the allowlist for no additional property at this scale: a
CA's advantage is issuing credentials to principals the verifier has never
met, and here the daemon meets every device during pairing.

## Key storage — what this does and does not buy

Dart's `SecurityContext` requires the private key as PEM
(`usePrivateKeyBytes`). **Android Keystore keys are non-exportable by design**,
so the client key cannot be hardware-bound without a native platform channel
implementing TLS client auth outside Dart. That is out of scope.

The key will therefore live in `flutter_secure_storage` — Keystore-*encrypted*
at rest, but decrypted into process memory to use.

Stated honestly, the gain over a bearer token is:

* **The private key is never transmitted.** A token is sent on every
  authentication and can be captured from a log, a backup, a screenshot of the
  pair QR, or any endpoint that mishandles it. A private key never leaves the
  device.
* **A stolen token alone becomes useless.** Both the token *and* the device key
  are required, and they are not exfiltrated by the same class of mistake.
* **Revocation becomes meaningful.** Removing the record denies transport
  access, not merely one credential.

It is **not**:

* hardware-bound or attestable — a full device compromise yields the key, as it
  already yields the token
* a defence against a compromised host, which is out of scope for any transport
  control

This is a real reduction in credential-theft exposure, not a hardware root of
trust. Claiming the latter would be wrong.

## Consequences

**Positive.** A lifted token no longer grants access from an unenrolled device.
Client identity is orthogonal to server identity, so it behaves identically in
`selfsigned` and `letsencrypt` modes. No CA to protect, rotate, or lose. Much
of ADR-pending token-lifecycle work (hardening plan 5.1) is subsumed: a
key-bound token is no longer a bearer credential.

**Negative.** Adds a Dart dependency (`basic_utils` or `pointycastle`) for
on-device key and certificate generation. Enrolment gains a step. Already-paired
devices must re-enrol to gain a key. Losing the device key requires re-pairing,
with no remote recovery — consistent with the pinning posture in ADR 0004.

**Neutral.** Client certificate expiry is ignored by design; identity is the
key. This is intentional and mirrors SSH.

## Migration

Client keys must be **optional** during rollout, or every paired device breaks
on daemon upgrade.

1. Ship the daemon accepting devices both with and without a recorded key.
2. Ship the app generating and registering a key on next pair or reconnect.
3. Once the fleet has keys, flip a config flag to require them.

Do **not** default to required in the first release. A device whose record has
no key must continue to authenticate by token until step 3.

## Feasibility spike — completed 2026-07-20

The main unknown has been closed. A standalone spike (Dart VM client ↔ Go TLS
server, outside the repo) verified the whole path:

| Assertion | Result |
|---|---|
| Dart generates a P-256 keypair (`basic_utils` 5.8.2, `CryptoUtils.generateEcKeyPair`) | **pass** |
| Dart builds a self-signed client certificate (`X509Utils.generateEccCsrPem` → `generateSelfSignedCertificate`) | **pass** |
| Dart presents it for TLS client auth (`SecurityContext.useCertificateChainBytes` + `usePrivateKeyBytes`) | **pass** |
| Go accepts via `RequireAnyClientCert` + `VerifyPeerCertificate` over `RawSubjectPublicKeyInfo`, **no `ClientCAs` pool** | **pass** |
| Go-computed SPKI fingerprint matches the `openssl`-derived value byte for byte | **pass** |
| A key **not** on the allowlist is rejected | **pass** |
| A client presenting **no** certificate is rejected | **pass** |

The design is sound and implementable as specified. Fingerprint the
**`RawSubjectPublicKeyInfo`**, not the certificate DER — the certificate can be
regenerated around the same key, and it is the key that is the identity.

**Caveat:** the spike ran on the Dart VM on Linux, not on Android. Dart uses
BoringSSL on both, so confidence is high, but confirm on-device during 3.2a
before relying on it.

### Finding: rejection is correct but illegible

Both negative cases fail as they should, but the client sees only a generic
`HttpException: Read failed`. A TLS alert from client-certificate rejection
carries no usable detail into Dart, so "your device key is not enrolled" is
indistinguishable from "the network dropped".

This must be handled explicitly at the app layer — the failure mode is
otherwise a support burden identical in shape to the `cert_unpinned`
collapse that ADR 0004 exists to avoid. Options: a pre-flight probe on an
endpoint that reports enrolment status, or having the daemon complete the
handshake and reject at the protocol layer with a typed error instead of
failing at TLS. Decide during 3.2b; the second is likely cleaner, since the
protocol already has typed error codes.

## Open items for implementation
2. **Protocol.** Adding the public key to the pair-claim payload, and surfacing
   whether a device has a key registered, are additive changes to
   `docs/protocol-v1.md`.
3. **Interaction with hardening plan 5.1.** Decide whether the token is
   retained alongside the key or eventually replaced by it. Retaining both is
   the conservative first step.
4. **`scripts/smoke-protocol`** will need a client key once enforcement is on.
