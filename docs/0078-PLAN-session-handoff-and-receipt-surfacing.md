<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

# Implement MADR 0078 — Session handoff + handoff receipts + phone receipt surfacing

Associated MADR: [0078-MADR-session-handoff-and-receipt-surfacing.md](0078-MADR-session-handoff-and-receipt-surfacing.md)

- **Status**: proposed — not yet implemented.
- **Date**: 2026-08-10
- **Scope**: Everything to take release+claim handoff (D1–D3), its dual
  receipts (D4–D6), and the phone read surface (D7–D9) from design to shipped.
- **Standing rule (repo)**: every phase that writes or modifies code scopes its
  tests as explicit numbered Steps, never as passive Acceptance prose. Commit
  per phase; do not push until asked.

## 0. Grounding — code facts that bound this plan

| Fact | Where (verified 2026-08-10) |
| --- | --- |
| `Authorize` first-touch claim: `OwnerDeviceID == "" && claim && deviceID != ""` stamps owner (persist-then-stamp) | `internal/session/manager.go:836-862` |
| `visibleTo(owner, dev)` = `owner == "" \|\| owner == dev`; applied in `ListSnapshot` | `internal/session/manager.go:829-831, 1062` |
| `Meta.OwnerDeviceID` + persistence via `persistNow`/`persist` | `internal/session/manager.go:87-89`; `Store` in `internal/session/store.go` |
| Receipt round-trip + transport | `runReceiptRoundTrip` (`manager.go:1365`), `ReceiptTransport.RequestPermissionReceipt` (`:229`), `ws.Server.RequestPermissionReceipt` (`:1368`), waiter device-bound (0077 F2) |
| Statement + predicate types | `internal/receipt/statement.go` |
| Receipt store (append/verify/lines/archive) | `internal/receipt/store.go` |
| WS request dispatch + per-verb handlers | `internal/ws/server.go` `handleMessage` switch (`:658`) |
| Phone WS client + silent receipt signer | `apps/mobile/lib/data/ws/mcremote_client.dart` `_handlePermissionReceiptRequest` |
| Settings screen (ListTile pattern) | `apps/mobile/lib/features/settings/settings_screen.dart` |

### 0.1 Decisions index

D1 release+claim · D2 release clears owner + `PendingHandoffTo` · D3 claim =
first-touch gated by target · D4 dual receipts (two new predicateTypes) · D5
generalize transport `RequestPermissionReceipt`→`RequestReceipt` · D6
`receipts.handoffs` sub-toggle, handoff feature always on · D7 settings screen
+ chat indicator · D8 `receipts.list`/`receipts.get`, own-chain only · D9
verify on device.

---

## 1. Phase P1 — Ownership: release + claim in the Manager (D1–D3)

Foundational; no wire/receipts yet. The whole feature's safety lives here.

### Steps

1. `internal/session/manager.go`: add `PendingHandoffTo string
   \`json:"pending_handoff_to,omitempty"\`` to `Meta` (and the persisted
   record in `internal/session/store.go` — mirror every `OwnerDeviceID`
   round-trip site, the same discipline 0077 P1 used for `ClientKeySPKI`).
2. `visibleTo` → extend to a 3-arg form (or a sibling) so a session with
   `PendingHandoffTo == deviceID` is visible to that device; keep empty-owner
   open visibility. Update `ListSnapshot`'s filter accordingly.
3. New `Manager.Release(ctx, sessionID, ownerDeviceID, toDeviceID string) error`:
   `Authorize` (no claim) → must be current owner; set `OwnerDeviceID = ""`,
   `PendingHandoffTo = toDeviceID`; `persistNow`; return. Reject releasing a
   session not owned by the caller.
4. New `Manager.Claim(ctx, sessionID, deviceID string) (Meta, error)`: reject
   if `OwnerDeviceID != ""` (not released) with a clear sentinel
   (`ErrNotReleased`); reject if `PendingHandoffTo != "" && PendingHandoffTo
   != deviceID` (`ErrForbidden`); else stamp owner, clear `PendingHandoffTo`,
   `persistNow`, return the claimer's `Meta`.
5. Ensure the release/claim mutations take the same lock and persist-then-stamp
   ordering as first-touch claim (0056 H-4), so a disk failure never leaves a
   half-transferred in-memory state.

### Tests (`internal/session/manager_handoff_test.go`, new)

6. Release by owner → session becomes empty-owner + `PendingHandoffTo` set;
   release by non-owner → `ErrForbidden`.
7. Open release (no target) → visible to a *different* device via
   `ListSnapshot`; targeted release → visible to the target, **not** to a
   third device.
8. Claim of a released session by the target → owner becomes claimer,
   `PendingHandoffTo` cleared; claim of a not-released session →
   `ErrNotReleased`; claim by a non-target when a target is set →
   `ErrForbidden`.
9. Concurrent claim of an open released session by two devices → exactly one
   wins, the other gets `ErrForbidden` (single-winner under the lock).
10. Round-trip through the store (fresh `Manager` over the same dir) preserves
    `PendingHandoffTo` — proving the new field persists.

### Acceptance

- `go test ./internal/session/...` green; steps 6–10 assert real ownership
  transitions, not just compilation.

---

## 2. Phase P2 — Wire verbs: `session.release` / `session.claim` (D1)

Depends on P1. Pure transport plumbing.

### Steps

1. `internal/protocol/messages.go`: `TypeSessionRelease = "session.release"`,
   `TypeSessionClaim = "session.claim"`; payloads
   `SessionReleasePayload{SessionID, ToDeviceID string}` and
   `SessionClaimPayload{SessionID string}`.
2. `internal/ws/server.go`: `handleSessionRelease` (owner device from `c`,
   call `Manager.Release`, emit a `session_status` for the releaser so its UI
   drops the session, reply `ok`) and `handleSessionClaim` (call
   `Manager.Claim`, reply a `session.created`-shaped `Meta`). Register both in
   the dispatch switch. Both async-dispatched like other session ops if they
   can block on persist.
3. `docs/protocol-v1.md`: add both verbs to the message table and a short
   "Session handoff" subsection (release/claim semantics, `to_device_id`
   optional, ownership/visibility rules).

### Tests

4. `internal/ws/*_test.go` (extend the ownership/session test file): a
   two-connection test — device A pairs+creates, releases (targeted at B);
   device B's `session.list` now shows it; B claims; A's list no longer shows
   it and an A prompt now fails forbidden; B can prompt.
5. Open-release variant: released with no target → a third device C can list
   and claim it.
6. Error paths over the wire: claim of a not-released session → typed error;
   release by a non-owner → typed error.

### Acceptance

- `go test ./internal/ws/...` green; step 4 exercises the full A→B transfer
  over real WebSocket frames.

---

## 3. Phase P3 — Generalize the receipt transport (D5)

Depends on nothing new; prepares P4. A rename + signature widen with no
behavior change, so it lands and stays green on its own.

### Steps

1. `internal/session/manager.go`: rename the `ReceiptTransport` method
   `RequestPermissionReceipt(ctx, deviceID, sessionID, permissionID,
   statement)` → `RequestReceipt(ctx, deviceID, sessionID, correlationID,
   statement)`. Permission callers pass the permission id as `correlationID`.
2. `internal/ws/server.go`: rename the implementation + the waiter map's
   conceptual key from "permission id" to "correlation id" (no shape change —
   it was already an opaque string keying a device-bound waiter). Update the
   inbound `permission.receipt` handler to match by correlation id.
3. Phone: no change yet — `permission.receipt_request` still carries a
   `permission_id`; keep that field name on the permission path. (Handoff
   requests in P4 use a `correlation_id` field; the phone learns it there.)

### Tests

4. Existing `manager_receipt_test.go` / `receipt_waiter_test.go` must pass
   unchanged after the rename (they already exercise the device-binding and
   payload-equality — this proves the generalization is behavior-preserving).
5. Add one test asserting the transport carries a **non-permission**
   correlation id end to end (a synthetic id), so the "generic" claim is
   covered, not just implied by the rename.

### Acceptance

- `go test ./internal/session/... ./internal/ws/...` green; the rename is
  behavior-preserving (step 4) and provably generic (step 5).

---

## 4. Phase P4 — Handoff receipts (D4, D6)

Depends on P1, P2, P3. The first new receipt kinds since 0077.

### Steps

1. `internal/receipt/statement.go`: two constants
   `PredicateTypeSessionHandoffRelease` / `…Claim`; predicate structs
   (`HandoffReleasePredicate{SessionID, FromDeviceID, ToDeviceID, ReleasedAt}`,
   `HandoffClaimPredicate{SessionID, ClaimedByDeviceID, FromDeviceID,
   ClaimedAt}`); builders `BuildHandoffReleaseStatement` /
   `BuildHandoffClaimStatement`, subject name `session:<id>/handoff:<nonce>`,
   digest over `from \x00 to \x00 session`.
2. `internal/config/config.go` + `load.go`: `ReceiptsConfig.Handoffs bool`
   (default true; only consulted when `Enabled`). `configs/config.example.yaml`
   + `docs/config.md` updated.
3. `internal/session/manager.go`: on successful `Release`/`Claim`, if
   `receipts.Enabled && receipts.Handoffs`, fire a background round-trip (D5's
   `RequestReceipt`) building the matching handoff Statement, verifying, and
   appending to the **signer's** chain (releaser for release, claimer for
   claim). Reuse the exact success/timeout/invalid-signature branches as
   `runReceiptRoundTrip` (extract a shared helper if it reduces duplication).
   Non-blocking; a mid-flight failure writes the daemon-signed
   `receipt-unavailable` marker into the signer's chain.
4. `internal/receipt/store.go` / CLI: no format change — `receipts
   list/verify/show` already walk any predicate type; extend `show`'s decoder
   to pretty-print the two handoff predicates (from→to, timestamps).
5. Phone: `permission.receipt_request` handler generalized to a
   `receipt_request` that carries a `correlation_id`; the phone's
   refuse-to-sign structural check (`receiptStatementRefusalReason`) extended
   to accept the two handoff `predicateType`s (still scope-checked to this
   device's own chain).

### Tests

6. `internal/receipt/statement_test.go`: golden files for both handoff
   predicates (release + claim), round-trip, and the shared-subject linkage
   (the two Statements name the same `session:<id>/handoff:<nonce>`).
7. `internal/session/manager_handoff_test.go`: with receipts+handoffs enabled,
   a release writes exactly one verifiable `session-handoff-release` entry to
   the releaser's chain; a claim writes one `session-handoff-claim` entry to
   the **claimer's** chain; the timeout path writes a daemon-signed marker;
   `receipts.handoffs=false` writes nothing while the transfer still succeeds.
8. `apps/mobile/test/receipt_statement_test.dart`: `receiptStatementRefusalReason`
   accepts a well-formed handoff-release/claim Statement naming this device,
   and still refuses one naming another device's chain.
9. `internal/cli/receipts_test.go`: `receipts show` on a handoff entry renders
   from→to and the timestamp (not raw JWS).

### Acceptance

- `go test ./...` green; steps 6–9 prove both halves land in the right chains,
  verify, and render.

---

## 5. Phase P5 — Phone read surface: `receipts.list` / `receipts.get` (D8, D9)

Depends on P4 (so there are handoff entries to show) but codeable against
permission entries alone.

### Steps

1. `internal/protocol/messages.go`: `TypeReceiptsList`/`TypeReceiptsListResult`,
   `TypeReceiptsGet`/`TypeReceiptsGetResult`; result payloads carry, per entry,
   the raw JWS compact string **and** the decoded Statement (the phone
   re-verifies the JWS itself — D9 — but decoding server-side saves the phone
   re-implementing the Statement schema for display).
2. `internal/ws/server.go`: `handleReceiptsList` / `handleReceiptsGet` — read
   **only the calling device's** chain (device id from `c`), never another's
   (D8). `receipts.get` by correlation id (subject-name match). Return the
   device's archived/enrolled public key material the phone needs? No — the
   phone already has its own key and pins the daemon cert; it needs no key
   from the wire.
3. `internal/ws/server.go`: advertise a `receipts` capability bit in the
   auth_ok / capability block when `receipts.enabled`, so the phone shows the
   UI only when the daemon keeps receipts.
4. `docs/protocol-v1.md`: document both verbs, the own-chain-only rule, and the
   capability bit.

### Tests

5. `internal/ws/*_test.go`: `receipts.list` returns the calling device's
   entries only; a second device gets *its* entries, never the first's
   (own-chain isolation — the D8 security property).
6. Capability bit present only when receipts enabled.

### Acceptance

- `go test ./internal/ws/...` green; step 5 pins the isolation property.

---

## 6. Phase P6 — Phone UI: settings screen + chat indicator (D7, D9)

Depends on P5. Dart only.

### Steps

1. `apps/mobile/lib/data/ws/mcremote_client.dart`: `listReceipts()` /
   `getReceipt(correlationId)` calling the P5 verbs; parse the `receipts`
   capability bit.
2. `apps/mobile/lib/data/ws/jws.dart` (or a sibling): a `verifyChainEntry`
   that recomputes the JWS signature (device key for permission-decision /
   handoff-*; pinned daemon cert public key for receipt-unavailable) and, for
   a full-chain view, the `prev_sha256` links — returning
   verified/failed/unverifiable (D9). The phone already holds its own key
   (`ClientIdentity`) and the pinned daemon cert.
3. `apps/mobile/lib/features/settings/`: new `receipts_screen.dart` — a
   "Signed receipts" entry in settings (shown only when the capability bit is
   set) listing this device's chain newest-first, each entry decoded to human
   text with a locally-recomputed ✓/✗/⚠ badge, empty state explaining receipts
   are a daemon opt-in.
4. `apps/mobile/lib/features/chat/`: on a resolved permission bubble whose
   decision was receipted (correlate by permission id via a lightweight
   `receipts.get` or a set the reducer already tracks), show a small "recorded"
   glyph tapping through to that entry in the receipts screen.

### Tests

5. `apps/mobile/test/`: a widget test for the receipts screen — renders a
   device-signed permission entry with a ✓ badge, a tampered entry with a ✗,
   and a daemon-marker entry verified against a stand-in pinned key; empty
   state when the capability bit is off.
6. `verifyChainEntry` unit tests: valid device-signed entry → verified;
   byte-flipped → failed; unknown predicateType → unverifiable-not-crash
   (mirrors the Go `Verify` and the existing `jws_test.dart` tamper approach —
   flip a decoded byte, never a trailing base64 char, per the CI flake fix).

### Acceptance

- `flutter analyze` + `flutter test` green; the screen verifies locally (D9),
  never trusting a daemon-asserted verdict.

---

## 7. Phase P7 — Live end-to-end + docs close-out

Depends on everything.

### Steps

1. `internal/ws/*_e2e_test.go` (extend 0077's `receipt_e2e_test.go` pattern):
   two fake WS clients — A creates + releases (targeted at B), B claims — with
   receipts+handoffs enabled; assert both A's release entry and B's claim
   entry land, verify via the real `mcremote receipts verify` subprocess, and
   share the same handoff subject.
2. `README.md`: "Session handoff" subsection (release/claim, targeted vs open)
   and extend the "Signed receipts" section with the handoff predicate types +
   the phone surfacing.
3. `docs/receipts.md`: register the two new `predicateType`s in the registry
   table; document the phone read surface and local verification.
4. MADR 0077's status line: note that D1's follow-up shipped as 0078 (one-line
   cross-reference, no content change).

### Acceptance

- `go test -race ./...`, `flutter test`, `staticcheck ./...` all green.
- The e2e test proves a real A→B handoff produces two independently-verifiable
  receipts in two different chains.

## 8. Verification (every phase)

```text
go build ./... && go vet ./... && staticcheck ./...
go test ./...   (and -race on internal/session, internal/ws, internal/receipt)
cd apps/mobile && flutter analyze && flutter test
```

## 9. Sequencing / rollback

- **Sequencing:** P1 → P2 (handoff usable, no receipts). P3 anytime (prep).
  P4 needs P1–P3. P5 needs P4. P6 needs P5. P7 last. Handoff ships and is
  useful after P2 even if the receipt phases slip.
- **Rollback:** handoff is additive verbs + one new nullable `Meta` field;
  disabling is "clients stop sending release/claim". Receipts stay behind
  `receipts.enabled`/`receipts.handoffs` (default-off feature). The phone UI
  hides itself when the capability bit is absent. No protocol version bump —
  all additive.
