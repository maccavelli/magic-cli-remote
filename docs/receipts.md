# Signed receipts for permission decisions

Design: [MADR 0077](0077-MADR-signed-receipts-permission-handoffs.md) ·
Implementation plan: [0077-PLAN](0077-PLAN-signed-receipts-permission-handoffs.md)

Signed receipts are an **opt-in** feature (`receipts.enabled: false` by
default — see [`docs/config.md`](config.md)): a durable, tamper-evident,
device-signed record of a human's permission decision on a paired phone.
Nothing about this feature changes behavior for an operator who never
touches the `receipts` config section.

## Why this exists

Every other record of a permission decision in this codebase is either
transient (in-memory only) or mutable (rewritable log lines). Signed
receipts exist to answer, durably and cryptographically, "which paired
device approved this specific action, and can I prove it wasn't tampered
with after the fact" — see MADR 0077 §2 for the full requirements this was
built against.

## Enabling it

```yaml
receipts:
  enabled: true
  allow_patterns:
    - "*rm -rf*"
    - "*push --force*"
    - "*kubectl delete*"
  deny_patterns:
    - "*--dry-run*"
```

`allow_patterns`/`deny_patterns` are shell-glob patterns (`*`, `?`,
`[set]`) matched against `"<tool_name> <detail>"` — the same human-readable
summary already carried on the `permission_request` event. A deny match
always wins over an allow match on the same decision. See
[`internal/receipt/match.go`](../internal/receipt/match.go) for exactly why
this is **not** Go stdlib `path.Match` (its `*` cannot cross a `/`, which
broke on the very first realistic example — a receipt-triggering pattern
matching a file path would silently never fire).

## Storage

Each enrolled device with at least one receipt gets its own file:

```text
<data_dir>/receipts/<device_id>.jsonl
```

- **Append-only.** Opened only with `O_APPEND`; there is no code path in
  `internal/receipt.Store` that rewrites an existing line, by construction.
- **One JWS compact string per line.** `header.payload.signature`
  (base64url, RFC 7515 §3.1) — no library on either side (Go or Dart); see
  [`internal/receipt/jws.go`](../internal/receipt/jws.go) and
  [`apps/mobile/lib/data/ws/jws.dart`](../apps/mobile/lib/data/ws/jws.dart).
- **Backward hash-chained per device** (not per session — a device's
  accountability history outlives any one session). Each entry's decoded
  payload carries `chain.prev_sha256`: the SHA-256 of the complete previous
  line (the literal stored JWS string, so it covers that entry's signature
  too), or `null` for a device's first-ever entry.
- **The device's public key is archived alongside** as
  `<data_dir>/receipts/<device_id>.spki` (raw DER `SubjectPublicKeyInfo`,
  write-once) so the chain stays verifiable after the device is revoked —
  see [Revoked devices](#revoked-devices).

Verify the chain any time with:

```console
$ mcremote receipts verify --device <device_id>
OK: device <device_id> chain intact.
```

A tampered or truncated file is detected by walking backward from the last
line: `mcremote receipts verify` reports the exact 1-indexed line the first
break occurs at, and exits non-zero — safe to use in an audit script.

## What the daemon enforces before appending

A line only enters the chain after all three of these hold — each closes a
distinct substitution/spoofing avenue:

1. **The reply came from the device that was asked.** The pending signing
   request is bound to the target device id; a `permission.receipt` from any
   other authed device is ignored (it cannot consume the request, so it also
   cannot downgrade the real device's receipt to a marker by racing it).
2. **The JWS verifies against that device's enrolled public key** (persisted
   at pair time / backfilled on reconnect — MADR 0077 D9).
3. **The signed payload is semantically identical to the Statement the
   daemon constructed.** A valid signature over *different* content — a
   substituted option, tool, timestamp or chain link — is rejected exactly
   like a bad signature (recorded as a `receipt-unavailable` marker with
   `reason: "invalid_signature"`). This is D2's other half: the phone signs,
   it never authors.

Replayed provider events (a `session` load re-emitting the prior
conversation) never mint receipts — a decision is receipted at most once, in
its first life.

## Revoked devices

`Device` records — including the auth store's copy of the public key — are
deleted by `mcremote pair revoke`/`pair prune`, but a device's receipt chain
must outlive its enrollment to be worth anything as an audit trail. So the
daemon **archives the device's public key beside its chain**
(`<data_dir>/receipts/<device_id>.spki`, raw DER `SubjectPublicKeyInfo`,
`0600`, write-once) before every signing round trip — even a chain that only
ever collected `receipt-unavailable` markers carries its key.
`receipts list`/`verify`/`show` resolve the key from the live `Device`
record first (authoritative while enrolled), falling back to the archive
after revocation, so a revoked device's history verifies exactly as before.

The archive is write-once by design: a device id's key never changes
(identity *is* the key, ADR 0005), so a differing rewrite could only be
corruption or an attempt to swap the verification key out from under an
existing chain.

One residual case: a chain written **before archival existed** whose device
was revoked before its next receipt has no `.spki` beside it — `verify`
fails with an error naming both misses. Recover the key from a devices.json
backup (the `client_key_spki` field, standard base64 of DER SPKI) if one
exists; any standard JWS verifier can then check the chain offline.

## The Statement shape

Every receipt's JWS payload is an in-toto-style **Statement** — the
extensibility layer that makes this a framework, not a one-off feature (see
MADR 0077 §7.2 for the full design rationale):

```json
{
  "_type": "https://mcremote.dev/attestations/receipt/v1",
  "subject": [
    {
      "name": "session:<session_id>/permission:<permission_id>",
      "digest": { "sha256": "<hash of the exact tool-call content the daemon received>" }
    }
  ],
  "predicateType": "https://mcremote.dev/attestations/permission-decision/v1",
  "predicate": {
    "device_id": "dev_...",
    "option_id": "once",
    "decided_at": "2026-08-08T21:00:00Z",
    "tool_name": "bash",
    "detail": "rm -rf ./build"
  },
  "chain": {
    "scope": "device:dev_...",
    "prev_sha256": "<SHA-256 of the complete previous stored JWS compact string, or null>"
  }
}
```

The `subject[0].digest.sha256` preimage is deterministic and reproducible by
an external verifier without mcremote: for a `permission-decision` Statement
it is `SHA-256(tool_name + "\x00" + detail)` (lowercase hex), where
`tool_name` and `detail` are exactly the `predicate.tool_name` and
`predicate.detail` values — the NUL separator prevents an ambiguous split
(`"git" + " push"` vs `"git " + "push"`) from producing the same digest.
This is what binds the receipt to the *real* action content rather than
trusting free text to stay in sync.

`chain` sits **outside** `predicate` deliberately — it is the one field
every receipt kind shares regardless of `predicateType`, so it belongs to
the fixed wrapper, not to a per-kind schema that would otherwise have to
remember to redeclare it. Every `predicateType` in this document is a URI
`mcremote` owns (`https://mcremote.dev/attestations/...`, never
`https://in-toto.io/...`) — this borrows in-toto's *shape*, not its
tooling; a receipt does not validate against generic in-toto/SLSA tooling
out of the box, and nothing here claims it does.

## `predicateType` registry

Every `predicateType` this codebase has ever defined, in one place — a
future receipt kind (the session-handoff follow-up MADR 0077 D1 names, or
anything else) registers itself here **before shipping**, the same
discipline [`docs/protocol-v1.md`](protocol-v1.md)/
[`docs/protocol-v2.md`](protocol-v2.md) already apply to wire messages.

| `predicateType` | Signed by | Meaning |
| --- | --- | --- |
| `https://mcremote.dev/attestations/permission-decision/v1` | The paired device's own key (enrolled at pair time, ADR 0005) | A human resolved a permission request: which option, when, for which tool call. |
| `https://mcremote.dev/attestations/receipt-unavailable/v1` | The daemon's own signing key | The device did not sign a receipt in time (`reason: "timeout"`) or its signature failed to verify (`reason: "invalid_signature"`). Keeps the chain's backward walk unbroken — a gap with no explanation is indistinguishable from tampering. |
| `https://mcremote.dev/attestations/session-handoff-release/v1` | The releasing device's own key | A device gave a session away (MADR 0078): which session, from whom, to whom (empty = open release), when. Lands in the releasing device's chain. |
| `https://mcremote.dev/attestations/session-handoff-claim/v1` | The claiming device's own key | A device took over a released session (MADR 0078): which session, by whom, when. Lands in the claiming device's chain, sharing the release's subject (`session:<id>/handoff:<nonce>`) so the two halves link across the two chains. |

## CLI reference

| Command | Purpose |
| --- | --- |
| `mcremote receipts list [--device ID]` | Every device with a receipts chain: entry count, first/last decision timestamp, chain status. |
| `mcremote receipts verify --device ID` | Full chain-integrity check against the device's enrolled key (and the daemon's key for any `receipt-unavailable` markers). Non-zero exit on a break. |
| `mcremote receipts show --device ID --permission ID` | Pretty-prints one decoded Statement — "what exactly did this receipt attest to," not raw JWS. The `--permission` value may also be a handoff nonce, to look up a handoff receipt. |

All three read `data_dir` the same way every other `mcremote` command
does (`--data-dir`, else config, else the XDG default) — see
[`docs/config.md`](config.md#locations-xdg).

## Reading a chain from the phone (MADR 0078)

A daemon that keeps receipts advertises a `receipts` capability bit in
`auth_ok`; the phone then offers Settings → **Signed receipts**, backed by the
`receipts.list` protocol verb. A device can read only **its own** chain — the
exact analog of session ownership — and the phone **re-verifies every entry's
signature locally** against its own enrolled key (and the hash-chain links),
showing a recomputed ✓/✗/⚠ rather than trusting any daemon-asserted verdict
(D9). Daemon-signed `receipt-unavailable` markers show as ⚠ on the phone: it
has no pinned daemon signing key to check them against, so it declines to
claim a pass.

## What this does not cover

- **Hardware-attested keys** — the device key is a software P-256 keypair
  in secure storage (ADR 0005), not a hardware enclave. A deliberate future
  hardening step, not this feature's job.
- **Interop with the wider in-toto/SLSA/Sigstore ecosystem** — the
  Statement *shape* is borrowed; Rekor, Fulcio, and cosign are not adopted,
  and a receipt will not validate against generic in-toto tooling.
