# Implement MADR 0077 — Signed receipts for permission decisions

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

Associated MADR: [0077-MADR-signed-receipts-permission-handoffs.md](0077-MADR-signed-receipts-permission-handoffs.md)

- **Status**: proposed — not yet implemented.
- **Date**: 2026-08-08
- **Scope**: Everything required to take `receipts.enabled: true` from config to a
  durable, verifiable, tamper-evident record of a human's permission decision —
  event/wire plumbing, JWS signing (Go + Dart), the in-toto-style Statement
  shape, per-device hash-chained storage, the pattern-matching config gate, the
  daemon↔phone signing round trip, and a CLI verification surface. Implements
  MADR 0077 §5.1 against decisions D1–D10 (all locked; none open).
- **Non-goals**:
  - **Session-handoff receipts** (MADR 0077 §5.2, Reading B). Per D1, a
    device-to-device session-transfer feature does not exist in this codebase
    (`Manager.Authorize` permanently rejects any non-owner device) and needs its
    own MADR before a receipt layer can attach to it. This PLAN implements only
    the `permission-decision` predicate type; the Statement/chain framework
    (D5/D6) is built so a second predicate type is additive later, but no second
    type ships here.
  - **Hardware-attested keys** (MADR 0077 §7.4 Alternative C). Deferred, a
    future hardening MADR.
  - **Dual/daemon-signed primary receipts** (§7.4 Alternative B). Only D8's
    narrow `receipt-unavailable` marker is daemon-signed; every real permission
    decision receipt is device-signed only.
  - **Full in-toto/Sigstore ecosystem adoption** (§7.4 Alternative H — Rekor,
    Fulcio, cosign). Not needed; this PLAN borrows the Statement *shape* only.
  - **Changing behavior for any operator who leaves `receipts.enabled: false`**
    (the default, D4). Every phase below must be a no-op end to end when
    receipts are off — this is the single most important invariant threaded
    through every phase's acceptance criteria.
  - **A JWS library dependency** (D7 — deliberately not adding one; hand-rolled
    ES256 Compact only).

---

## 0. Grounding — code facts that bound this plan

Every fact below was verified directly against the tree at commit `94602c3`
(tag `v0.9.0`) during MADR 0077's three research/grounding passes, and
re-confirmed while writing this plan. File:line citations are load-bearing —
if any of these have moved by the time this plan is picked up, re-verify
before trusting the phase that depends on it.

| Fact | Where (verified) |
| --- | --- |
| `RespondPermission` receives `deviceID`, uses it once for the `Authorize` gate, never passes it to the provider | `internal/session/manager.go:1242-1255` |
| `provider.PermissionSession` interface — the signature P6 must extend | `internal/provider/provider.go:102-108`: `RespondPermission(ctx, permissionID, optionID string, cancelled bool) error` |
| `event.Event` has no `DeviceID` field anywhere; permission-resolved carries only `PermissionID`+`Status` | `internal/event/event.go:445-447`; zero `DeviceID` hits confirmed via `grep -c` |
| Every provider's resolution-emission call site (P6's exact touch list) | `internal/provider/acpagent/session.go:1291-1299` (grok); `internal/provider/httpagent/session.go:697-701` (shared base, opencode+kilo's primary `RespondPermission` path); `internal/provider/opencode/http.go:1271-1275` and `internal/provider/kilo/session.go:509-513` (dialect-specific `TakePending`/resync emission sites — separate from the httpagent base path); `internal/provider/codex/session.go:486-492` (auto-mode-arm sweep) and `:1001-1008` (normal resolution) |
| `fake` provider does **not** implement `PermissionSession` | confirmed absent via `grep -rln RespondPermission internal/provider/fake/` — out of scope for P6, never touched |
| Phone's durable P-256 identity keypair, generated once at pair time | `apps/mobile/lib/data/ws/client_identity.dart:44-59` (`CryptoUtils.generateEcKeyPair(curve: 'prime256v1')`), persisted via `flutter_secure_storage` |
| Daemon computes only a **fingerprint** of the device key, never persists the key itself | `internal/ws/server.go:500`: `c.clientKeyFP = certs.SPKIFingerprint(r.TLS.PeerCertificates[0])`; `internal/certs/certs.go:549-552`: `sha256.Sum256(cert.RawSubjectPublicKeyInfo)` — one-way hash |
| `Device`/`deviceRecord` structs — P1's exact edit target | `internal/auth/store.go:78-99`; `ClientKeyFP string` only, no key-bytes field |
| `CreateWithClientKey` — P1's exact edit target | `internal/auth/store.go:269`: `func (s *Store) CreateWithClientKey(name, clientKeyFP string) (device Device, plaintextToken string, err error)` |
| `verifyClientKey` — P1's self-healing-backfill insertion point | `internal/ws/server.go:1169-1196`; already confirms `c.clientKeyFP == dev.ClientKeyFP` before proceeding |
| Daemon's own TLS serving keypair is ECDSA P-256, same curve D7's signer handles | `internal/certs/certs.go:239`: `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)` |
| Daemon cert bundle shape — P7's marker-signing key source | `internal/certs/certs.go:69-79` (`Bundle{Certificate tls.Certificate, Leaf *x509.Certificate, ...}`); `Bundle.Certificate.PrivateKey` is the usable `crypto.Signer`, obtained at daemon startup via `internal/daemon/certs.go:28` `EnsureCerts` |
| `Store.SaveHistory`'s pattern P4 must explicitly NOT reuse | `internal/session/store.go:211-232`: whole-file read-modify-rewrite on every save, plus its own independent re-cap to `historyBufferCap` (800) at `store.go:224-226` — correct for a transcript, disqualifying for a tamper-evident log |
| `allow_rules`/`deny_rules` are forwarded as literal CLI flags to the grok binary, not evaluated by mcremote — P5's matcher is new code, not reused code | `internal/provider/grok/grok.go:138-141` |
| `ACPProviderConfig.AllowRules`/`DenyRules` — the config *naming convention* P5 borrows (not the matching code) | `internal/provider/acpagent/config.go:60-63`; `internal/config/config.go:477-480` |
| Existing protocol message payload style — P7's new payloads must match | `internal/protocol/messages.go:528-535` (`PermissionRespondPayload{SessionID, PermissionID, OptionID, Cancelled}`) |
| `PermissionTimeoutSeconds` fail-safe — D8 requires this stays untouched | referenced throughout provider configs, e.g. `internal/provider/codex/session.go` docs; P7 must not add any new blocking wait on this path |
| RFC 7515 §5.1 JWS Signing Input, exact formula (P2/D7's correctness bar) | `ASCII(BASE64URL(UTF8(JWS Protected Header)) \|\| '.' \|\| BASE64URL(JWS Payload))` — [datatracker](https://datatracker.ietf.org/doc/html/rfc7515#section-5.1) |
| RFC 7515 Appendix A.3 — the published ES256 test vector P2 must reproduce/verify | [datatracker](https://datatracker.ietf.org/doc/html/rfc7515#appendix-A.3) |
| RFC 7518 §3.4 — ES256 signature is raw `R\|\|S`, 32+32 bytes big-endian, **not** ASN.1 DER | [rfc-editor](https://www.rfc-editor.org/rfc/rfc7518.html) |
| Go's classic `ecdsa.Sign` returns `(r, s *big.Int)` directly — no DER involved, unlike `ecdsa.SignASN1` | Go stdlib `crypto/ecdsa` |
| Dart's `pointycastle` `ECDSASigner.generateSignature` returns an `ECSignature{r, s}` with raw `BigInt` fields, matching what mcremote's own `client_identity.dart` already manipulates directly (`priv.d`, `params.G * priv.d!`) | `apps/mobile/lib/data/ws/client_identity.dart:44-59,80-95` (confirms the raw-value API is already in use in this codebase, not just in theory) |
| CLI command pattern P8's `mcremote receipts verify` must match | `internal/cli/engines.go:1-40` (cobra `RunE`, `appdirs.SystemPaths`, tabwriter output) |
| Next MADR/PLAN doc-numbering slot | none conflicting — this is `0077-PLAN-*`, sibling to the already-written `0077-MADR-*` |

### 0.1 Decisions — not re-derived here, only indexed

All ten are locked in the MADR; this plan implements them, it does not
re-litigate them. Quick index for cross-reference while reading phases below:

| ID | One-line summary | MADR section |
| --- | --- | --- |
| D1 | Scope: permission receipts now; session-handoff gets its own future MADR | §4.1 |
| D2 | Daemon constructs the canonical payload; phone signs it | §4.1, §7.3 Q1 |
| D3 | JWS (unchanged across all three research passes) | §4.1, §7.2 |
| D4 | Opt-in, config-gated (`receipts` section), not default-on | §4.1 |
| D5 | In-toto-style typed `Statement` (`subject`/`predicateType`/`predicate`) inside the JWS payload | §7.2 |
| D6 | Storage: `<data_dir>/receipts/<device_id>.jsonl`, append-only, `0600`, backward-chained via `chain.prev_sha256` | §7.2 |
| D7 | Hand-rolled ES256 Compact Serialization, no library either side | §7.2 |
| D8 | 10s bounded signing timeout, decoupled from `PermissionTimeoutSeconds`; daemon-signed `receipt-unavailable` marker on failure | §7.2 |
| D9 | Persist `ClientKeySPKI` (not just the fingerprint) at enrollment; self-healing backfill for existing devices | §7.2 |
| D10 | New shell-glob pattern matcher (regexp-backed, not `path.Match` — corrected during P5, see its Steps); `allow_rules`/`deny_rules` naming borrowed, matching code is new | §7.2 |

---

## 1. Phase P1 — Device public-key persistence (D9)

Foundational and lowest-risk: purely additive storage, no behavior change for
anyone, and everything downstream that verifies a receipt depends on it.
Ship and merge this phase alone first if useful — it has value independent of
the rest (it's a real gap in the existing auth store, not just a receipts
prerequisite).

### Steps

1. `internal/auth/store.go`:
   - Add `ClientKeySPKI []byte` (json tag `client_key_spki,omitempty`, base64
     via Go's default `[]byte` JSON marshaling) to both `Device` (`:78-88`)
     and `deviceRecord` (`:90-99`).
   - `CreateWithClientKey(name, clientKeyFP string)` (`:269`) → add a
     `clientKeySPKI []byte` parameter; thread it into the new `deviceRecord`
     literal.
   - Every other `ClientKeyFP: rec.ClientKeyFP` struct-literal site that
     rebuilds a `Device` from a `deviceRecord` (`:283,294,315,346,381,472` —
     six sites) needs the parallel `ClientKeySPKI: rec.ClientKeySPKI` field
     added so the round trip through the store doesn't silently drop it.
   - New method: `func (s *Store) PublicKeyFor(deviceID string) (*ecdsa.PublicKey, error)`
     — look up the device, `x509.ParsePKIXPublicKey(rec.ClientKeySPKI)`, type-assert
     to `*ecdsa.PublicKey`, error clearly (`"device %s has no persisted public key
     yet — it must reconnect once before its receipts can be verified"`) if
     `ClientKeySPKI` is empty. This is P8's exact dependency.
2. `internal/ws/server.go`:
   - `:500`, alongside the existing `c.clientKeyFP = certs.SPKIFingerprint(...)`:
     add `c.clientKeySPKI = r.TLS.PeerCertificates[0].RawSubjectPublicKeyInfo`.
     Add the `clientKeySPKI []byte` field to the `client` struct next to the
     existing `clientKeyFP string` field (`:120`).
   - `:1009`, the `CreateWithClientKey` call site (new-device enrollment):
     pass `c.clientKeySPKI` as the new argument.
   - `verifyClientKey` (`:1169-1196`), after the existing fingerprint-match
     check succeeds (i.e., inside the success path, not the two existing
     rejection branches): if `dev.ClientKeySPKI` is empty, call a new
     `s.store.BackfillClientKeySPKI(dev.ID, c.clientKeySPKI)` — safe precisely
     because the fingerprint match just proved this connection's key *is* the
     enrolled one. Log at `Debug`, not `Info`/`Warn` — this is routine, silent
     self-healing, not an event an operator needs to see.
3. `internal/auth/store.go`: add `BackfillClientKeySPKI(deviceID string, spki []byte) error` —
   load, set the field only if currently empty (defensive: never overwrite a
   populated value), save.
4. **Extend the five existing `store_test.go` tests that already round-trip a
   `Device` through the six struct-literal rebuild sites step 1 touched**,
   asserting `ClientKeySPKI` survives each path rather than silently getting
   dropped by a rebuild site step 1 missed — this is the direct test for
   "every one of the six sites was actually updated," which `go build`
   alone cannot confirm (a forgotten site still compiles, it just returns a
   zeroed field):
   - `TestStoreCreateValidateRevoke` (`internal/auth/store_test.go:13`)
   - `TestRevokeByClientKeyFP` (`:58`)
   - `TestStorePruneKeyless` (`:104`)
   - `TestStorePruneStale` (`:161`)
   - `TestStoreHonorsExternalRevoke` (`:242`)
5. **New tests** for the three behaviors step 1–3 actually added (not just
   extensions of existing ones):
   - New device pairing captures `ClientKeySPKI` immediately;
     `PublicKeyFor` succeeds right after.
   - A fixture device with `ClientKeyFP` set and `ClientKeySPKI` absent
     (simulating a pre-P1 record) gets backfilled on its next successful
     `verifyClientKey` call, and a second call is a no-op (asserted via a
     "value unchanged, no second write" check, not just "still populated").
   - `PublicKeyFor` on a device that has never connected since P1 shipped
     returns the documented clear error, not a panic or a zero-value key.

### Acceptance

- Steps 4 and 5's tests all pass — see those steps for exactly what each one
  proves; not re-listed here.
- `go test ./internal/auth/... ./internal/ws/...` green.

---

## 2. Phase P2 — JWS ES256 Compact Serialization, no library (D7)

Pure, self-contained crypto plumbing — no dependency on P1, can be built and
merged in parallel with it. Both Go and Dart sides ship in this phase since
they share one correctness bar (RFC 7515 Appendix A.3) and should be reviewed
together.

### Steps

1. New package `internal/receipt/jws.go`:
   - `SignES256Compact(priv *ecdsa.PrivateKey, payload []byte) (string, error)`:
     header is the fixed `{"alg":"ES256"}` (no `kid` — device/daemon identity
     is established out-of-band via the existing enrolled-cert lookup, per D9,
     not via a JWS header field, which would be one more place to spoof).
     Base64url-encode header and payload, sign
     `ASCII(BASE64URL(header) || '.' || BASE64URL(payload))` per RFC 7515 §5.1
     (§0's grounding table) using the classic `ecdsa.Sign(rand.Reader, priv, sha256Sum)`
     — **not** `ecdsa.SignASN1`. Left-pad `r` and `s` to 32 bytes each
     (`big.Int.FillBytes` is the clean stdlib way), concatenate to the 64-byte
     raw signature RFC 7518 §3.4 requires, base64url-encode, join with `.`.
   - `VerifyES256Compact(pub *ecdsa.PublicKey, compact string) (payload []byte, err error)`:
     split on `.`, reject if not exactly 3 parts, base64url-decode each,
     reject if header isn't exactly `{"alg":"ES256"}` (no algorithm
     negotiation — a wrong/foreign `alg` value is rejected outright, not
     downgraded to weaker validation), split the 64-byte signature back into
     `r`/`s` (first 32 bytes / last 32 bytes, `big.Int.SetBytes`), verify with
     `ecdsa.Verify(pub, sha256Sum, r, s)`.
2. `internal/receipt/jws_test.go`:
   - **Reproduce RFC 7515 Appendix A.3's published ES256 example exactly**:
     hardcode the RFC's example key (the appendix gives the P-256 key's raw
     `x`/`y`/`d` coordinates) and payload, and assert `VerifyES256Compact`
     accepts the RFC's own published signature. This is the correctness bar
     D7 sets — do this before writing a single real receipt.
   - Round-trip test: generate a fresh key, sign, verify, succeeds.
   - Negative tests: tampered payload byte → verify fails; tampered signature
     byte → verify fails; wrong public key → verify fails; malformed compact
     string (wrong part count, bad base64) → clean error, not a panic.
3. Dart mirror, `apps/mobile/lib/data/ws/jws.dart`:
   - `String signEs256Compact(ECPrivateKey priv, Uint8List payload)` using
     `pointycastle`'s `ECDSASigner` (already imported via `client_identity.dart`'s
     `basic_utils`/`pointycastle` dependency — confirmed present, §0), extracting
     the `ECSignature.r`/`.s` `BigInt` values directly and packing them the
     same 32+32 big-endian way as the Go side. `dart:convert`'s `base64Url`
     (already used in `client_identity.dart`) covers encoding; no new pub.dev
     package.
   - `Uint8List? verifyEs256Compact(ECPublicKey pub, String compact)` — the
     phone needs this too, to sanity-check the daemon's `receipt_request`
     Statement is well-formed before signing it (defense in depth; the phone
     should not blindly sign arbitrary bytes a compromised/buggy daemon
     handed it — though the phone doesn't have the daemon's key to verify a
     *signature* here, so this function is really just structural validation
     of the unsigned Statement JSON, not a JWS verify; see P7 step 3 for what
     the phone actually checks before signing).
   - `apps/mobile/test/jws_test.dart`: the **same RFC 7515 Appendix A.3
     vector**, verified independently on the Dart side — both platforms must
     agree on a format neither is copying from the other's implementation,
     only from the shared published spec.

### Acceptance

- `go test ./internal/receipt/... -run TestES256` passes, including the RFC
  test vector.
- `flutter test test/jws_test.dart` passes, including the same RFC test
  vector, independently implemented.
- A signature produced by the Go signer verifies successfully against the
  Dart verifier and vice versa (cross-platform round-trip test) — this is the
  test that actually proves interoperability, not just "each side agrees with
  itself."

---

## 3. Phase P3 — Statement, chain, and predicate types (D5, D6's shape, D8's marker type)

Depends on P2 (needs the JWS payload bytes to exist before it can be signed,
though Statement *construction* has no crypto dependency itself — this phase
could technically start in parallel with P2, but review it after so the
payload shape and the signer are checked together).

### Steps

1. `internal/receipt/statement.go`:
   ```go
   type Statement struct {
       Type          string             `json:"_type"`
       Subject       []ResourceDescriptor `json:"subject"`
       PredicateType string             `json:"predicateType"`
       Predicate     json.RawMessage    `json:"predicate"`
       Chain         ChainLink          `json:"chain"`
   }
   type ResourceDescriptor struct {
       Name   string            `json:"name"`
       Digest map[string]string `json:"digest"`
   }
   type ChainLink struct {
       Scope      string  `json:"scope"`
       PrevSHA256 *string `json:"prev_sha256"`
   }
   ```
   Constants: `StatementType = "https://mcremote.dev/attestations/receipt/v1"`,
   `PredicateTypePermissionDecision = "https://mcremote.dev/attestations/permission-decision/v1"`,
   `PredicateTypeReceiptUnavailable = "https://mcremote.dev/attestations/receipt-unavailable/v1"`.
2. Predicate payload types (marshaled into `Statement.Predicate`):
   ```go
   type PermissionDecisionPredicate struct {
       DeviceID  string    `json:"device_id"`
       OptionID  string    `json:"option_id"`
       DecidedAt time.Time `json:"decided_at"`
       ToolName  string    `json:"tool_name"`
       Detail    string    `json:"detail"`
   }
   type ReceiptUnavailablePredicate struct {
       Reason       string `json:"reason"` // "timeout" | "invalid_signature"
       PermissionID string `json:"permission_id"`
       DeviceID     string `json:"device_id"`
   }
   ```
3. `BuildPermissionDecisionStatement(sessionID, permissionID, deviceID, optionID, toolName, detail string, decidedAt time.Time, chainScope string, prevSHA256 *string) (*Statement, error)`:
   `subject` is one `ResourceDescriptor{Name: "session:" + sessionID + "/permission:" + permissionID, Digest: {"sha256": hex(sha256(toolName + "\x00" + detail))}}`
   — the exact tool-call content the daemon received from the provider,
   hashed; this is what makes the receipt bind to the *real* action (§2 point
   2 of the MADR) rather than trusting free text to stay in sync.
4. `BuildReceiptUnavailableStatement(...)` — same wrapper shape, `chain.scope`
   still the device's chain (a failed-to-sign entry still belongs in that
   device's sequence, so the backward walk stays intact per D8).
5. Golden-file test: marshal a `BuildPermissionDecisionStatement` result and
   diff it byte-for-byte against a checked-in `testdata/statement_example.json`
   matching MADR 0077 §7.2's example exactly — this is the contract test that
   keeps the wire shape from drifting silently.
6. **A second golden-file test for `BuildReceiptUnavailableStatement`** — the
   original draft of this plan only scoped a test for the permission-decision
   predicate (step 5); the unavailable-marker predicate is equally new code
   (step 4) and needs its own fixture/diff test, not just "the same function
   probably works." Cover both `reason` values (`"timeout"`, `"invalid_signature"`)
   as separate cases, since D8's two failure modes must produce distinguishable
   records, not the same marker with different callers assumed to remember why.
7. JSON round-trip test (marshal → unmarshal → re-marshal, byte-identical)
   for **both** predicate types — the original Acceptance section implied
   this for both but only step 5 (permission-decision) had a concrete test
   named; making it explicit here for the same reason as step 6.

### Acceptance

- `go test ./internal/receipt/... -run TestStatement` passes, including the
  golden-file diff.
- `Statement` JSON round-trips (marshal → unmarshal → re-marshal, byte-identical)
  for both predicate types.

---

## 4. Phase P4 — Storage: append-only, per-device, backward-chained (D6)

Depends on P3 (needs `Statement`/`ChainLink` types) and P2 (stores JWS
compact strings). Independent of P1/P5/P6/P7 otherwise — can be built and
unit-tested with synthetic JWS strings before the rest of the pipeline exists.

### Steps

1. `internal/receipt/store.go`:
   - `type ReceiptStore struct { dir string; mu sync.Mutex; lastHash map[string]string }`
     — `dir` is `<data_dir>/receipts` (mirrors how `appdirs`/`internal/session/store.go`
     resolve `data_dir`; reuse the same resolution helper rather than
     re-deriving the path).
   - `func NewReceiptStore(dataDir string) (*ReceiptStore, error)`: `os.MkdirAll(dir, 0700)`.
   - `func (s *ReceiptStore) LastHash(deviceID string) (string, bool, error)`:
     check the in-memory cache first; on a cache miss (daemon just started),
     open `<dir>/<deviceID>.jsonl` and read only the last line (seek-from-end
     in fixed-size chunks looking backward for `\n`, not a full-file read —
     this file can grow large over a device's lifetime by design, D6/§8
     risk table) — hash it, populate the cache, return. Return `false` for
     "no previous entry" (first-ever receipt for this device) rather than an
     error.
   - `func (s *ReceiptStore) Append(deviceID, jwsCompact string) error`: hold
     `s.mu` (single daemon process, simple mutex is sufficient — no
     cross-process contention to design for), open with
     `os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)`, write
     `jwsCompact + "\n"` in one `Write` call, `Sync()`, close, update
     `s.lastHash[deviceID]` to this line's hash. **Never** open for anything
     but append or read-only — no read-modify-rewrite path exists in this
     type at all, by construction, so `Store.SaveHistory`'s pattern (§0
     grounding) cannot accidentally get copied into this file later.
   - `func (s *ReceiptStore) Verify(deviceID string, pub *ecdsa.PublicKey) (brokenAtLine int, err error)`:
     read the whole file line by line (verification is an infrequent,
     operator/auditor-initiated operation — full-file read here is fine, only
     the hot append/last-hash path needed the seek-from-end optimization),
     for each line: `VerifyES256Compact` (P2) against the appropriate key
     (device key for `permission-decision` lines, daemon key for
     `receipt-unavailable` lines — inspect `predicateType` first to know
     which), then confirm `chain.prev_sha256` equals the SHA-256 of the
     previous line's raw bytes (`nil` only valid on line 1). Return `-1` for
     "chain intact" or the 1-indexed line number of the first break.
2. `internal/receipt/store_test.go`:
   - Append N synthetic entries, confirm `Verify` reports `-1`.
   - Mutate one byte in a middle line directly on disk, confirm `Verify`
     reports exactly that line number.
   - Confirm `LastHash` after a simulated restart (new `ReceiptStore` instance
     over the same directory) matches the last hash before the "restart."
   - Confirm file permissions are `0600` and the directory `0700`.

### Acceptance

- `go test ./internal/receipt/... -run TestStore` green.
- A 10,000-entry synthetic chain appends and verifies in well under a second
  (sanity bound — this is meant to be cheap; not a hard perf gate, just a
  smoke check nothing here is accidentally quadratic).

---

## 5. Phase P5 — Pattern matcher and config (D10, D4)

Independent of P1–P4; can be built in parallel. Gates P7 (the round trip only
fires when this says yes).

### Steps

1. `internal/config/config.go`: new top-level (not per-provider) config
   struct:
   ```go
   type ReceiptsConfig struct {
       Enabled       bool     `mapstructure:"enabled"`       // default false (D4)
       AllowPatterns []string `mapstructure:"allow_patterns"`
       DenyPatterns  []string `mapstructure:"deny_patterns"`
   }
   ```
   Added to the root `Config` struct as `Receipts ReceiptsConfig \`mapstructure:"receipts"\``,
   sibling to `Providers`, not nested under it — receipts are cross-provider
   by design (D4).
2. `internal/config/load.go`: `SetDefault("receipts.enabled", false)`,
   `SetDefault("receipts.allow_patterns", []string{})`,
   `SetDefault("receipts.deny_patterns", []string{})` — mirroring the
   existing per-provider defaults-block pattern in the same file. Env vars
   fall out of the existing `MCREMOTE_` + uppercased-path convention
   automatically (`MCREMOTE_RECEIPTS_ENABLED`, etc.) — no explicit `BindEnv`
   needed, matching how kilo's config wiring worked (no explicit binds were
   needed there either, confirmed in MADR 0076's grounding).
3. `internal/receipt/match.go`:
   ```go
   func ShouldReceipt(cfg config.ReceiptsConfig, toolName, detail string) bool {
       if !cfg.Enabled {
           return false
       }
       target := toolName + " " + detail
       for _, pat := range cfg.DenyPatterns {
           if matchPattern(pat, target) {
               return false // deny wins over allow
           }
       }
       for _, pat := range cfg.AllowPatterns {
           if matchPattern(pat, target) {
               return true
           }
       }
       return false
   }
   ```
   **Corrected while implementing this step** (the original draft called
   `path.Match(pat, target)` directly): `path.Match`'s `*` refuses to cross a
   `/` — path-separator-aware glob semantics, correct for matching one path
   segment, wrong here. `target` is `tool_name + " " + detail`, and `detail`
   is very often a file path; verified directly that
   `path.Match("*rm -rf*", "bash rm -rf ./build")` — this MADR's own §7.2
   worked example — returns `(false, nil)`: no error, just a silent
   non-match. `matchPattern` instead translates the glob (`*`, `?`, `[set]`)
   to an anchored `regexp` (still Go stdlib, no new dependency, results
   cached since patterns are static config evaluated on a hot path) with `*`
   mapped to `.*` so it matches across `/`. An unterminated `[` is this
   scheme's one malformed-pattern case (mirroring `path.Match`'s
   `ErrBadPattern` for the same syntax mistake), treated as non-matching and
   logged at `Warn` with the offending pattern — never a startup-time hard
   failure, since a typo'd pattern degrading to "no receipts for that rule"
   is far preferable to a typo'd pattern crashing the daemon.
4. `configs/config.example.yaml`: add a documented `receipts:` block
   (disabled, with example patterns commented out) in the same annotated
   style as every other section.
5. `docs/config.md`: add the `receipts.*` keys to the config reference table.
6. **New `internal/receipt/match_test.go`** (missing from the original draft
   of this plan, which only described this test's coverage in Acceptance
   prose — added as an explicit step here): table-driven test covering
   disabled → always false regardless of patterns; allow-only match and
   no-match; deny-only match and no-match; both match on the same input →
   deny wins; an unterminated `[` → warns and is treated as non-matching, not
   a panic and not a startup failure; and — the regression guard for this
   step's `path.Match`-to-regexp correction — a pattern like `*rm -rf*`
   matching a detail string with a `/` after the matched text (e.g.
   `"rm -rf ./build"`), which silently failed to match under the original
   `path.Match`-based draft.
7. **Extend `internal/config/config_test.go`** — the actual file name
   (confirmed; the original draft referenced "the existing config-defaults
   test pattern" without naming it) — with `ReceiptsConfig`'s three defaults
   (`enabled: false`, both pattern lists empty), following whichever existing
   test function already asserts other sections' defaults in that file,
   rather than adding a new one-off function for just this section.

### Acceptance

- Steps 6 and 7's tests pass — see those steps for exactly what each covers.

---

## 6. Phase P6 — Event/wire plumbing: `DeviceID` + `OptionID` through the resolution path

The mechanical work item MADR 0077 §1 identified as the concrete gap. No
dependency on P1–P5; can be done any time, but sequenced here because it's
the piece every later phase's "which device answered, with which option"
data comes from. This phase alone (independent of receipts entirely) also
closes a real pre-existing transparency gap — worth landing even if the rest
of this plan stalls.

**Correction from the original draft of this plan, caught by an explicit
test-coverage audit before implementation started:** the first pass listed
five provider touch points and missed a sixth — `internal/provider/acphttp`
(goose's transport, confirmed distinct from `httpagent` despite the similar
name — `internal/provider/goose/goose_test.go:10` imports it directly) also
implements `RespondPermission` and has its own resolution-emission sites.
Caught precisely *because* "list every test file this phase must touch" was
done as its own step rather than left to "existing tests still pass" — the
missing package would have compiled fine (nothing forced its discovery) and
shipped with silently no receipt support for goose.

### Steps

1. `internal/event/event.go`, in the existing "Permission fields" block
   (`:445-447`, right after `Options []PermissionOption`): add
   `DeviceID string \`json:"device_id,omitempty"\`` and
   `OptionID string \`json:"option_id,omitempty"\`` (`OptionID` here is the
   *resolved* choice, distinct from the request's offered `Options` list).
2. `internal/provider/provider.go:102-108`: extend the interface —
   `RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool, deviceID string) error`.
3. `internal/session/manager.go:1242-1255`: pass `deviceID` through to
   `ps.RespondPermission(ctx, permissionID, optionID, cancelled, deviceID)`
   (it's already a parameter to `Manager.RespondPermission` — this removes
   the exact drop-point MADR §1 identified).
4. Update every implementer's signature and its resolution-emission event
   literal to include `DeviceID: deviceID, OptionID: optionID` — **six
   provider packages, eight emission sites**:
   - `internal/provider/acpagent/session.go:1291-1299` (grok — `permissionResolved` helper; add `deviceID, optionID string` params, thread through its one caller)
   - `internal/provider/httpagent/session.go:697-701` (shared HTTP base — opencode's and kilo's primary path)
   - `internal/provider/opencode/http.go:1271-1275` (opencode dialect `TakePending` resync path — separate emission site, same fix)
   - `internal/provider/kilo/session.go:509-513` (kilo dialect, same pattern)
   - `internal/provider/codex/session.go:486-492` (auto-mode-arm sweep — `deviceID`/`optionID` are legitimately empty here: this path answers *previously pending* permissions in bulk when auto mode arms, not a single fresh human tap) and `:1001-1008` (the normal single-decision path, where both are always populated)
   - `internal/provider/acphttp/session.go:760` (goose's `RespondPermission`) — its emission site at `:816` is the normal human-answer path (both fields populated); its second emission site at `:1340` is the `PermissionTimeoutSeconds` auto-cancel fail-safe (`s.log.Warn("permission timed out", ...)`) — `deviceID`/`optionID` are legitimately empty there too, same reasoning as codex's sweep path, and should be documented as such rather than left looking like an oversight.
5. `fake` provider: no change (§0 grounding — doesn't implement
   `PermissionSession`).
6. **Test-double wrapper types that also implement this exact method
   signature and will fail to compile otherwise** — found by grepping for
   the method signature itself, not just the interface name, since these are
   structurally-typed Go interfaces:
   - `internal/provider/opencode/http_delta_test.go:156` — `captureHost.RespondPermission`, a test-only host wrapper that calls `ds.RespondPermission(ctx, permissionID, optionID, cancelled)` (`:164`); add the `deviceID` parameter through both the wrapper's own signature and its inner call.
   - `internal/provider/kilo/session_test.go:152` — the identical wrapper in kilo's test package (`:160`), same fix.
7. **Extend or add one test per provider package asserting the new fields
   land on the resolved event** — six packages, using each package's existing
   test-file convention rather than inventing a new one:
   - `internal/provider/acpagent/` — no dedicated `permission_test.go` exists; add the assertion to whichever existing file already drives a `RespondPermission` call end to end (check `session_test.go` first) rather than create a new file for one assertion.
   - `internal/provider/httpagent/session_test.go` (or the package's equivalent, shared by opencode/kilo's base path) — extend.
   - `internal/provider/opencode/permission_test.go` — extend (this package *does* have a dedicated file).
   - `internal/provider/kilo/` — no dedicated `permission_test.go`; permission-resolution assertions currently live across `automode_test.go`, `autoapprove_test.go`, `session_test.go`; add to whichever most directly exercises the plain (non-auto-approve) `RespondPermission` path.
   - `internal/provider/codex/permission_test.go` — extend; also assert the auto-mode-arm-sweep path (`:486-492`) explicitly leaves `DeviceID`/`OptionID` empty rather than merely not testing it, so a future accidental populate-by-copy-paste is caught.
   - `internal/provider/acphttp/permission_test.go` — extend (goose *does* have a dedicated file); also assert the timeout-cancel path (`:1340`) explicitly leaves both fields empty, same reasoning as codex's.

### Acceptance

- `go build ./...` succeeds (interface signature change compiles cleanly
  across every implementer *and* every test-double wrapper — the compiler
  enforces completeness here, which is exactly why this is safe to do as a
  hard signature change rather than a new optional method).
- All six extended/added tests (step 7) pass and are new lines in a diff, not
  just "existing tests still pass" — a signature change alone cannot prove
  the new fields actually carry the right values end to end.
- `go test ./...` green, no behavior change for any code path that doesn't
  read the two new fields (both `omitempty`, so the wire shape for existing
  clients that ignore them is unaffected).

---

## 7. Phase P7 — Daemon↔phone signing round trip (D2, D8, D9's key lookup, D5's Statement, D10's gate)

The integration phase — depends on P1 (key lookup), P2 (signing), P3
(Statement), P4 (storage), P5 (the gate), and P6 (the data to build a
Statement from). Sequenced last among the "backend" phases deliberately: by
this point every piece it wires together already has its own test coverage,
so this phase's tests can focus purely on orchestration, not re-proving unit
correctness.

**Architecture corrected during implementation.** The original draft below
describes the hook as living inside `session.Manager.RespondPermission`
itself. Building it, two facts surfaced that don't fit that shape:

1. `RespondPermission`'s own parameters (`sessionID, permissionID, optionID,
   cancelled, deviceID`) never include `tool_name`/`detail` — those only ever
   existed on the *original* `permission_request` event, and by the time
   `ps.RespondPermission(...)` returns, that event has already been emitted
   and is not recoverable from `RespondPermission`'s own call frame.
2. `session.Manager` has no notion of a WS connection at all (by design — it
   is transport-agnostic); "send to a specific device and await its reply"
   is a capability only `ws.Server` has, via its `s.clients` connection
   registry.

Both are resolved by hooking the event **pump** instead (the loop each
session's events already flow through before reaching `onEvent`/broadcast),
and by a small interface `ws.Server` implements:

- `session.Manager` already tracks `e.pendingPermissions[permissionID]` —
  the original `TypePermission` event, `tool_name`/`detail` included — and
  deletes it exactly when `TypePermissionResolved` arrives. The receipt hook
  reads that entry *before* the delete (already in the pump's existing
  code), so no provider needed to be touched a second time.
- `session.ReceiptTransport` (new interface, one method,
  `RequestPermissionReceipt(ctx, deviceID, sessionID, permissionID,
  statement) (jws string, err error)`) is implemented by `*ws.Server`
  (verified with a compile-time `var _ session.ReceiptTransport =
  (*Server)(nil)` assertion) and injected into `Manager` after construction
  via `Manager.SetReceiptSupport(ReceiptSupport{...})` — the same
  constructor-order-breaking bridge pattern `internal/daemon`'s existing
  `eventHub` already uses for `onEvent`/`BroadcastEvent`, extended to also
  carry `Config`, `Store`, `AuthStore`, and `DaemonKey`.
- `ws.Server` gained `sendToDevice` (iterate `s.clients`, filter by
  `deviceID` — the same filter `BroadcastEvent` already applies for
  session-owner delivery) and a `permissionID`-keyed waiter map so a reply
  is found even if the device reconnected on a different connection (step 4,
  unchanged from the original draft's intent).
- `handlePermissionReceipt` (the inbound `permission.receipt` handler)
  answers with `TypeOK`, unlike the fire-and-forget the original draft
  implied — this lets the phone use the exact same
  `request()`/await-response path every other outbound message already
  uses, instead of needing a bespoke send-without-reply primitive.
- The daemon's own signing key (D8's marker) is **not** read from
  `identity.SelfSigned` (the live TLS listener's resolved identity):
  grounding found that field is `nil` whenever ACME issuance succeeds or
  `tls.mode: off`, i.e. in two real, non-exotic configurations — exactly the
  cases D8's own framing ("the daemon's TLS serving key") assumed always
  populated. Fixed by calling `EnsureCerts(cfg)` again, independently of
  `identity`: it always resolves a stable, disk-persisted ECDSA key
  regardless of what is actually serving live traffic, since all D8 needs is
  *a* daemon-controlled key, not necessarily the one presented over the wire.

None of this changes the wire protocol, the Statement shape, or D8's
behavior — only where the orchestration code physically lives.

### Steps

1. `internal/protocol/messages.go`, alongside `PermissionRespondPayload`
   (`:528-535`):
   ```go
   const TypePermissionReceiptRequest = "permission.receipt_request" // daemon -> phone
   const TypePermissionReceipt        = "permission.receipt"         // phone -> daemon

   type PermissionReceiptRequestPayload struct {
       SessionID    string          `json:"session_id"`
       PermissionID string          `json:"permission_id"`
       Statement    json.RawMessage `json:"statement"` // the unsigned Statement (P3), including its already-computed chain.prev_sha256
   }
   type PermissionReceiptPayload struct {
       SessionID    string `json:"session_id"`
       PermissionID string `json:"permission_id"`
       JWS          string `json:"jws"` // the signed JWS compact string
   }
   ```
2. Orchestration hook — **actually lands in `Manager.pump`'s
   `TypePermissionResolved` case, not literally inside `RespondPermission`**
   (see the correction above for why). Conceptually still triggered by the
   same resolution, and still spawned from outside any blocking path — after
   the resolved event is observed (the permission grant/deny has already
   fully proceeded — D8's non-blocking requirement), if `ShouldReceipt` (P5)
   matches, spawn a goroutine (`Manager.runReceiptRoundTrip`) that:
   1. `store.PublicKeyFor(deviceID)` isn't needed yet here (that's for
      *verification*, not signing) — skip straight to building the Statement.
   2. `ReceiptStore.LastHash(deviceID)` (P4) → `prevSHA256`.
   3. `BuildPermissionDecisionStatement(...)` (P3) using the just-resolved
      data (session ID, permission ID, deviceID, optionID, tool name, detail,
      now, `"device:"+deviceID`, prevSHA256).
   4. Send `permission.receipt_request` to the connection owning `deviceID`
      (the WS layer already knows how to address a specific authenticated
      device — reuse whatever the existing per-device send path is, the same
      one that delivers `permission_request`/other per-session events).
   5. Wait up to **10 seconds** (D8) for a matching `permission.receipt` on
      the same `permission_id`.
   6. On success: `VerifyES256Compact` (P2) against `store.PublicKeyFor(deviceID)`
      (P1) — reject and fall through to step 7 if verification fails (D8's
      "invalid_signature" reason) rather than trusting the phone's claim
      blindly; on success, `ReceiptStore.Append(deviceID, jws)` (P4).
   7. On timeout or verification failure: `BuildReceiptUnavailableStatement(...)`
      (P3), sign it with the **daemon's own** serving key
      (`certs.Bundle.Certificate.PrivateKey` type-asserted to `*ecdsa.PrivateKey`
      — threaded into the `Manager`/receipt orchestration at daemon startup,
      alongside how `EnsureCerts`'s result already reaches the TLS listener
      setup), `ReceiptStore.Append(deviceID, jws)`.
3. Phone side (`apps/mobile/lib`): handle `permission.receipt_request` —
   parse the unsigned Statement, **sanity-check it structurally** before
   signing (non-empty `subject`/`predicateType`, `predicateType` matches the
   one known type this phase ships, `chain.scope` matches this device's own
   ID — refuse to sign anything that doesn't look like what was actually just
   approved; this is the phone-side half of "never sign blindly," a real
   defense against a compromised/buggy daemon trying to get a device to sign
   an unrelated statement), sign with the device's own persisted
   `ClientIdentity` key (P2's Dart signer), send back `permission.receipt`.
   This is a background operation — no new UI is required for v1 (no
   "signing receipt…" spinner needed; it's a sub-second local operation
   behind the scenes, consistent with D8's framing that it must never be
   perceptible as a delay).
4. Failure/edge handling: connection drops mid-round-trip → the 10s timeout
   (step 6/7) already covers this, no special-case code needed. Device
   answers from a *different* connection than the one that resolved the
   permission (e.g. reconnected mid-flight) → still addressed by `deviceID`,
   not connection identity, so this resolves correctly without extra work.
5. **New `internal/session/manager_receipt_test.go`** (missing from the
   original draft, which described this phase's tests only as Acceptance
   prose — added as an explicit step here, following the existing
   `manager_<topic>_test.go` split used by `manager_durable_test.go` and
   `manager_persist_race_test.go` rather than growing `manager_test.go`
   further): a fake WS client stands in for the phone (send/receive
   `permission.receipt_request`/`permission.receipt` directly, no real
   network), wired against a real `session.Manager` and a temp-dir
   `ReceiptStore`. Covers exactly the five behaviors in Acceptance below —
   each becomes its own test function or subtest, not one monolithic test.
6. **Extend `internal/cli/receipts_test.go`** — created in P8 (§8, step
   below) but its "verify" and "show" subcommands need at least one fixture
   generated by *this* phase's real signing path, not only P8's synthetic
   fixtures, to catch drift between what P7 actually writes and what P8
   assumes the format is. Add one round-trip test here (or in
   `manager_receipt_test.go`, whichever already has the live `ReceiptStore`
   in scope) that appends a real P7-produced entry and asserts
   `ReceiptStore.Verify` accepts it — this is the seam most likely to break
   silently if P3's Statement shape and P8's parser drift apart.

### Acceptance

- End-to-end test (fake WS client standing in for the phone, real
  `session.Manager`): a resolved permission matching an `allow_patterns` rule
  produces exactly one new `.jsonl` line, JWS-valid, chain-linked to the
  previous entry.
- Same test with the fake client never replying (the fake `ReceiptTransport`
  returns an error immediately rather than literally sleeping out the real
  10s window — `receiptRoundTripTimeout` is exercised for real by the actual
  10s constant in production, not re-derived per test run): exactly one new
  `.jsonl` line appears, `predicateType: receipt-unavailable`, signed by the
  daemon's key, verifiable against the daemon's own key.
- Same test with the fake client replying with a garbage/tampered JWS: falls
  through to the `receipt-unavailable`/`invalid_signature` path, not a crash
  and not a false "success."
- **The core invariant, explicitly tested**: with `receipts.enabled: false`
  (the default), none of this phase's code path executes at all — a
  benchmark or a simple call-count assertion confirming `ShouldReceipt` short
  circuits before anything else in this phase runs.
- `RespondPermission`'s own return latency is unaffected whether or not a
  receipt is triggered (assert the goroutine dispatch, not a synchronous
  wait, in a test that would time out if this regressed to blocking).
- All five bullets above are asserted in `internal/session/manager_receipt_test.go`
  (step 5); none live only as prose.

---

## 8. Phase P8 — CLI verification surface + `predicateType` registry doc

Depends on P1 (key lookup) and P4 (the store to read). Independent of P5–P7
otherwise — could be built as soon as P1+P4 land, using synthetic fixtures,
and wired to real data once P7 exists.

### Steps

1. `internal/cli/receipts.go`, following `internal/cli/engines.go`'s exact
   pattern (`§0` grounding — cobra `RunE`, `appdirs.SystemPaths`, tabwriter):
   - `mcremote receipts list [--device ID]`: enumerate `<data_dir>/receipts/*.jsonl`,
     print device ID, entry count, first/last timestamp, chain status
     (intact / broken-at-line-N — calls `ReceiptStore.Verify`).
   - `mcremote receipts verify --device ID`: run `ReceiptStore.Verify`
     against that device's persisted public key (P1's `PublicKeyFor`),
     print pass/fail and the exact broken line if any, non-zero exit code on
     failure (so it's usable in an audit script, not just interactively).
   - `mcremote receipts show --device ID --permission ID`: pretty-print one
     decoded Statement (human-readable, not raw JWS) for a specific decision
     — the "what exactly did this receipt attest to" command.
2. `docs/receipts.md` (new, mirroring `docs/protocol-v1.md`'s role as the
   source of truth for a wire surface): documents the `Statement` shape, the
   two shipped `predicateType`s (`permission-decision`, `receipt-unavailable`),
   the chain format, and — explicitly, per MADR 0077's own risk table — a
   **`predicateType` registry section**: every `predicateType` URI this
   codebase has ever defined, in one place, so a future receipt kind (D1's
   session-handoff follow-up, or anything else) registers itself here before
   shipping, the same discipline `docs/protocol-v1.md` already applies to
   wire messages.
3. `README.md`: add `mcremote receipts` to the CLI reference table (matching
   the existing `engines`/`paths` rows), and a short "Signed receipts"
   subsection cross-linking `docs/receipts.md` and MADR 0077 — following the
   same pattern used for the Kilo provider section.
4. **New `internal/cli/receipts_test.go`** (missing from the original draft,
   which described this phase's tests only as Acceptance prose — added as an
   explicit step here, matching the confirmed existing convention in
   `internal/cli/engines_test.go`): builds synthetic `.jsonl` fixtures under
   a temp `data_dir` (an intact chain, a hand-corrupted chain broken at a
   known line, and a fixture with an unknown/malformed `predicateType` to
   confirm `show` degrades to raw-JSON-with-a-warning rather than crashing),
   then drives `list`/`verify`/`show` via cobra's command-execution test
   harness (matching however `engines_test.go` invokes its `RunE`, not by
   shelling out to a built binary). Covers exactly the three Acceptance
   bullets below.

### Acceptance

- `mcremote receipts verify` against a hand-corrupted `.jsonl` fixture
  reports the exact broken line and exits non-zero; against an intact one,
  exits zero.
- `mcremote receipts show` output matches what a human would need to answer
  "what did this receipt actually attest to" without reading raw JSON.
- `docs/receipts.md` exists and is linked from `README.md`'s docs index,
  matching the existing table's format.
- All three bullets above are asserted in `internal/cli/receipts_test.go`
  (step 4); none are exercised only by manual/ad-hoc invocation.

---

## 9. Phase P9 — Live end-to-end test

Depends on every prior phase. The final acceptance gate before this ships.

### Steps

1. `internal/receipt/live_test.go` (or a top-level integration test,
   whichever this repo's existing live-test convention favors — check
   `docs/config.md`'s live-tag pattern used by `live_opencode`/`live_codex`
   and match it, e.g. `//go:build live_receipts` if a live coding-agent CLI
   is needed, or a plain non-tagged integration test if a `fake` provider
   round trip is sufficient — **the `fake` provider doesn't implement
   `PermissionSession` (§0 grounding), so this test needs either a minimal
   test double implementing it, or one of the real providers with
   `always_approve` armed; prefer the test double to keep this test
   dependency-free and fast, matching this repo's general preference for
   fixtures over live CLIs where the thing under test is receipts plumbing,
   not the provider itself**).
2. Full round trip: enable `receipts.enabled: true` with a matching
   `allow_patterns` rule, drive a real (or double-backed) permission
   decision through the WS protocol layer end to end — `permission.respond`
   in, `permission.receipt_request` out, `permission.receipt` back, verify
   the resulting `.jsonl` entry via `mcremote receipts verify` (P8) as a
   subprocess, not just by calling `ReceiptStore.Verify` directly in-process
   — this is the test that proves the CLI surface and the storage layer
   actually agree with each other end to end.

### Acceptance

- `go test ./...` (whole suite) green.
- The live/integration test in this phase passes and is added to whatever
  this repo's `make test-all`/`make preflight` umbrella already runs, so it's
  not a test that only runs if someone remembers to.

---

## 10. Verification (every phase)

```text
go build ./...
go vet ./...
go test ./...
go test -race ./internal/receipt/... ./internal/auth/... ./internal/session/...
cd apps/mobile && flutter analyze && flutter test
```

Phase-specific additions layer on top of the above as each phase lands (P2's
RFC test vector, P4's tamper-detection test, P7's end-to-end/timeout tests,
P9's full round trip) — listed under each phase's own Acceptance section
above; this table is the baseline every phase must keep green in addition to
its own new tests.

## 11. Rollout and rollback

- **Rollout**: `receipts.enabled` defaults `false` (D4) — every phase ships
  dark by default. P1 (key persistence) and P6 (event fields) are the two
  phases with any effect when receipts are off, and both are purely additive
  (new optional fields, `omitempty`, a new backfill that only ever adds data
  never previously captured) — no existing behavior changes for an operator
  who never touches the `receipts` config section. No protocol version bump
  is needed (MADR 0077 D6 §"Rollout" implicitly — new message types are
  additive, ignored by any client that's never been told to expect them).
- **Rollback**: set `receipts.enabled: false` (or omit the section entirely)
  — P7's orchestration hook short-circuits at `ShouldReceipt`, nothing else
  in the pipeline runs. Full code rollback: revert P1–P9's commits in
  reverse order; P1's `ClientKeySPKI` field can be left in place harmlessly
  even if the rest is reverted (unused data, not a liability) if a partial
  rollback is ever preferable to a full one.
- **Sequencing**: P1, P2, P5, P6 have no dependencies on each other and can
  be built/reviewed/merged in any order or in parallel. P3 depends on P2. P4
  depends on P2+P3. P7 depends on P1+P2+P3+P4+P5+P6 (the integration phase,
  necessarily last among the backend work). P8 depends on P1+P4. P9 depends
  on everything. A reasonable PR sequence: {P1, P2, P5, P6} in any order →
  P3 → P4 → P7 → P8 → P9.
