<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

# MADR 0078 — Device-to-device session handoff, with signed handoff receipts and phone-side receipt surfacing

| | |
| --- | --- |
| status | **Proposed 2026-08-10.** Decisions D1–D9 locked with the owner (§4). Two threads, deliberately in one MADR because they share the receipt substrate: (A) device-to-device **session handoff** — the feature MADR 0077 §5.2/D1 scoped out as "not feasible as scoped; own follow-up MADR"; (B) **phone-side receipt surfacing** — receipts (0077) are today CLI-only; the phone signs them silently and can never see them. Handoff produces the first new receipt kinds since 0077 shipped, so surfacing and handoff are designed together. |
| supersedes | nothing |
| depends on | MADR 0077 (signed receipts — the JWS/Statement/chain substrate, reused wholesale), ADR 0005 (client identity — the per-device P-256 key both features sign with), MADR 0056/0068 (session ownership + protocol v2) |
| plan | [0078-PLAN-session-handoff-and-receipt-surfacing.md](0078-PLAN-session-handoff-and-receipt-surfacing.md) |

## 0. Executive summary

mcremote sessions are single-owner: `session.Manager.Authorize` permanently
rejects any device that is not `OwnerDeviceID` (`internal/session/manager.go:860`),
and a device's session list is filtered to what it owns
(`visibleTo`, `:829`). There is no way to hand a running session from one
paired device to another — the exact gap 0077 named as its deferred D1.

This MADR designs that handoff as **release + claim** (D1): the owner
*releases* a session (optionally naming a target device), which returns it to
the same ownerless state a legacy pre-ownership session already occupies; the
receiving device then *claims* it through the first-touch claim path that
already exists. This is deliberately the smallest possible change to the
authorization core — it relaxes the single-owner invariant only by reusing
the one transition (`OwnerDeviceID == "" → claimer`) `Authorize` already
performs, rather than inventing a co-ownership or co-signing protocol.

Each half is attested by a **dual receipt** (D4): the releasing device signs
a `session-handoff-release` Statement, the claiming device signs a
`session-handoff-claim` Statement — two new `predicateType`s inside 0077's
existing envelope, each landing in its own signer's per-device chain. Zero
new cryptography, storage format, or verification code: this is precisely the
"a second receipt kind is purely additive" property 0077 D5 was built to
deliver, now exercised for the first time.

Finally, the phone gets to **see** receipts (D7–D9): a "Signed receipts"
settings screen showing this device's own chain (locally verifiable — the
phone holds its own signing key and pins the daemon's serving cert, so it can
check both device-signed and daemon-signed entries offline), and a small
"receipt recorded" indicator on resolved permission bubbles in chat.

## 1. Context — what exists, verified

| Fact | Where (verified 2026-08-10) |
| --- | --- |
| A session has exactly one `OwnerDeviceID`; empty means legacy/unclaimed | `internal/session/manager.go:87-89` (`Meta.OwnerDeviceID`) |
| `Authorize` rejects any non-owner with `ErrForbidden`; **the one exception** is `OwnerDeviceID == "" && claim` → stamps the caller as owner (first-touch) | `internal/session/manager.go:836-862` |
| A device sees only sessions it owns, **plus** empty-owner ones | `visibleTo(owner, dev) = owner == "" \|\| owner == dev` (`:829-831`), applied in `ListSnapshot` (`:1062`) |
| Empty-owner sessions are broadcast to every authed device (event fan-out) | `internal/ws/server.go` `BroadcastEvent`: empty owner → all devices |
| Wire session-lifecycle verbs today | `session.create/list/close/delete/prompt/cancel/set_mode/…` (`internal/protocol/messages.go:67-104`) — no release/claim |
| Receipt round-trip: daemon builds Statement → device signs → daemon verifies (device key **and** that the signed payload equals the sent one) → append | `internal/session/manager.go` `runReceiptRoundTrip` (`:1365`); `ws.Server.RequestPermissionReceipt` (`:1368`), waiter keyed by an id and bound to the target device (0077 F2 fix) |
| Statement extensibility: a new receipt kind = new `predicateType` + predicate struct, nothing else | `internal/receipt/statement.go:11-33`; 0077 D5 |
| Receipts are CLI-only today (`mcremote receipts list/verify/show`); the phone signs silently and has no read path | `internal/cli/receipts.go`; `apps/mobile/lib/data/ws/mcremote_client.dart` `_handlePermissionReceiptRequest` (signs, never surfaces) |
| The phone already holds its own P-256 key and pins the daemon serving-cert fingerprint | ADR 0005; `apps/mobile/lib/data/ws/client_identity.dart`, cert pinning in `mcremote_client.dart` |

**The load-bearing observation:** "release" is not a new authorization state —
it is a *return* to the empty-owner state the system already handles
everywhere (visibility, fan-out, first-touch claim). That is what makes this a
bounded change rather than an ownership-model rewrite.

## 2. What handoff must get right (requirements)

1. **No orphaning.** A released session must remain reachable — visible and
   claimable — not vanish. (Empty-owner visibility already delivers this.)
2. **No silent theft.** A device must not be able to yank a session from an
   active owner without that owner releasing it. (Release is owner-only;
   claim only works on a released session.)
3. **Targeted vs. open release.** In a 3+ device fleet, an owner handing to a
   *specific* teammate must not expose the session to every device. A named
   target must restrict both visibility and who may claim.
4. **Mid-turn safety.** Releasing a session with a running turn or a pending
   permission must not corrupt provider state. The provider process is owned
   by the daemon, not the device (0073) — a handoff changes *who controls*
   the session, never restarts the agent. A pending permission survives as
   pending; the new owner answers it.
5. **Attestation parity.** If receipts are enabled, both the release and the
   claim are recorded with the same non-repudiation guarantee a permission
   decision gets — each device signing only its own action.

## 3. Non-goals

- **Co-ownership / simultaneous multi-device control.** Out of scope; sessions
  stay single-owner-at-a-time. Handoff is a transfer, not a share.
- **Cross-daemon / cross-host handoff.** A session lives on one daemon; this
  is device→device within one daemon, not daemon→daemon.
- **Handoff of a session a device cannot reach.** Both devices must be paired
  to the same daemon (they already are — that is what "fleet" means here).
- **Receipt *editing* UI on the phone.** Surfacing is read-only; receipts are
  append-only by construction (0077 D6).
- **Retrofitting receipts onto historical handoffs.** Only handoffs performed
  after this ships are attested.

## 4. Decisions (locked with owner, 2026-08-10)

| ID | Decision | Rationale |
| --- | --- | --- |
| **D1** | **Handoff mechanism = release + claim.** The owner releases (optionally naming a target); the recipient claims. No co-signing handshake. | Reuses the one authorization transition `Authorize` already performs (empty-owner → first-touch claim). Smallest safe relaxation of the single-owner invariant; naturally yields two independently-signable actions (D4). |
| **D2** | **Release clears `OwnerDeviceID`** back to the empty-owner state, and (when a target is named) sets a new `PendingHandoffTo` field that scopes visibility+claim to that device only. Open release (no target) is visible/claimable by any paired device, exactly like a legacy session. | "Released" is not a new state to design — it is the empty-owner state the whole system already handles. `PendingHandoffTo` is the *only* new field, and it only ever *narrows* the existing empty-owner visibility. |
| **D3** | **Claim = the existing first-touch path**, gated by `PendingHandoffTo` when set. `Authorize(claim=true)` already stamps the claimer as owner; the only new code is "if `PendingHandoffTo != "" && claimer != PendingHandoffTo → reject", and clear `PendingHandoffTo` on successful claim. | Maximum reuse. A targeted release is a claim restricted to one device; an open release is the unrestricted claim that already exists. |
| **D4** | **Dual receipts.** Release → the releasing device signs a `session-handoff-release` Statement; claim → the claiming device signs a `session-handoff-claim` Statement. Each is a new `predicateType`; each lands in its **signer's own** per-device chain. | Direct application of 0077 D5 (additive predicate types) and D2 (a device signs only *its own* action — the releaser attests "I gave S away", the claimer attests "I took S"). An auditor reconstructs the transfer from both halves. No new crypto/storage/verify. |
| **D5** | **Reuse 0077's round-trip transport, generalized from "permission receipt" to "receipt for an action."** The `ws.Server` waiter is keyed by a correlation id and bound to a target device (already true); the handoff correlation id is the session id + a handoff nonce. The `ReceiptTransport` interface gains no new method — the existing `RequestPermissionReceipt` is renamed `RequestReceipt(deviceID, sessionID, correlationID, statement)`; permission receipts pass the permission id as the correlation id, handoff receipts pass the handoff nonce. | The transport is already generic; only the *name* implied "permission". One rename + one caller change makes it carry any Statement. Keeps a single signing path (one place to get the device-binding and payload-equality checks right — 0077 F1/F2). |
| **D6** | **Handoff receipts are gated by the same `receipts.enabled` + a new default-on-when-enabled `receipts.handoffs` sub-toggle**, and the release/claim *features themselves are always available* (handoff is a control feature, not a compliance feature). Receipts are the opt-in layer on top, exactly as for permissions. A handoff with receipts off simply performs the transfer and writes nothing. | Handoff has value for every fleet user (hand your session to your laptop-phone); receipts have value only for the compliance subset (0077 §6). Coupling them would gate a useful feature behind an audit feature nobody else needs. |
| **D7** | **Phone receipt UI = "Signed receipts" settings screen + a per-permission chat indicator.** The settings screen lists this device's chain (newest first), each entry decoded to human text, with a locally-computed verification badge. The chat indicator is a small "receipt recorded" glyph on a resolved permission bubble whose decision was receipted. | Matches the two real read moments: "review my audit trail" (settings) and "was *this* decision recorded" (in-context). The phone can verify locally — it holds its own key (device-signed entries) and pins the daemon serving cert (daemon-signed markers) — so verification needs no round trip. |
| **D8** | **New read wire surface: `receipts.list` (this device's chain, as decoded Statements + their JWS) and `receipts.get` (one entry by correlation id).** Owner/device-scoped: a device can only read **its own** chain, never another device's — enforced daemon-side by device id, mirroring session ownership. | The phone must not become a way to exfiltrate another device's audit trail. A device reading only its own chain is the exact analog of a device listing only its own sessions (§1). |
| **D9** | **Verification is computed on the phone, not trusted from the daemon.** The daemon sends chain entries; the phone recomputes each JWS signature (device key for `permission-decision`/`session-handoff-*`, pinned daemon cert key for `receipt-unavailable`) and the `prev_sha256` links itself. A daemon that tampered with a stored line cannot make the phone show "verified". | The whole point of receipts is not trusting a single party's word (0077 §6). A "verified" badge the daemon could assert would defeat that. The phone already has both keys it needs. |

## 5. Design detail

### 5.1 Release + claim wire protocol

Two new request verbs (additive; a client that never sends them is unaffected):

- **`session.release`** `{ session_id, to_device_id? }` → `ok`/`error`.
  Owner-only (`Authorize` without claim). Sets `OwnerDeviceID = ""` and
  `PendingHandoffTo = to_device_id` (empty for open release). Persists the
  meta immediately (a release must survive restart — Phase-0056 durability).
  Emits a `session_status` so the releasing device's UI drops it from "mine".
  If receipts+handoffs are enabled, fires the release round-trip in the
  background (D5), non-blocking (0077 D8).
- **`session.claim`** `{ session_id }` → `session.created`-shaped result (the
  claimer now owns it and gets the same meta a creator gets). Rejected if the
  session is not released (`OwnerDeviceID != ""`), or if `PendingHandoffTo` is
  set and does not match the claimer. On success: stamps owner, clears
  `PendingHandoffTo`, persists, and (if enabled) fires the claim receipt
  round-trip.

Discovery: a released session already appears in the recipient's
`session.list` via empty-owner visibility (open) — for a **targeted** release,
`ListSnapshot` is extended so a session with `PendingHandoffTo == deviceID` is
visible to that device (and, still, to nobody else). The releasing device
sees it leave its list; the target sees it arrive on next list/broadcast.

### 5.2 Mid-transfer state (requirement 4)

- **Running turn:** unaffected. The turn runs against the daemon-owned
  provider process; ownership is a control-plane label. The new owner sees
  the turn continue via history replay + live events on claim. The releasing
  device stops receiving live events the moment it is no longer owner.
- **Pending permission:** stays pending. It is keyed by permission id in the
  provider, not by owner. After claim, the new owner's `session.pending_asks`
  surfaces it and the new owner answers it — and *that* answer's permission
  receipt is signed by the new owner, correctly attributing the decision.
- **Release with a pending permission the releaser wants answered first:** not
  special-cased — the releaser can answer before releasing, or hand it over
  unanswered. Documented, not enforced.

### 5.3 Handoff Statements (D4)

Both reuse 0077's `Statement` envelope and chain. Subject binds the transfer:

```json
{
  "_type": "https://mcremote.dev/attestations/receipt/v1",
  "subject": [{ "name": "session:<id>/handoff:<nonce>",
                "digest": { "sha256": "<sha256(from_device \x00 to_device \x00 session_id)>" }}],
  "predicateType": "https://mcremote.dev/attestations/session-handoff-release/v1",
  "predicate": { "session_id": "...", "from_device_id": "dev_A",
                 "to_device_id": "dev_B_or_empty", "released_at": "<rfc3339>" },
  "chain": { "scope": "device:dev_A", "prev_sha256": "..." }
}
```

The claim Statement mirrors it with `predicateType:
…/session-handoff-claim/v1`, `predicate: { session_id, claimed_by_device_id,
from_device_id?, claimed_at }`, and `chain.scope: device:dev_B` — landing in
**B's** chain. The two are linked for an auditor by the shared
`session:<id>/handoff:<nonce>` subject name, not by chain adjacency (they live
in different chains by design).

### 5.4 Phone surfacing (D7–D9)

- **Settings → "Signed receipts":** calls `receipts.list`; renders each entry
  as a card (kind, session, decision/tool or from→to, timestamp) with a
  locally-recomputed ✓/✗/⚠ badge (D9). Empty state explains receipts are a
  daemon opt-in.
- **Chat indicator:** the reducer already tracks resolved permissions; when
  `receipts.enabled` is advertised (a new capability bit) and a permission was
  receipted, a resolved permission bubble shows a subtle "recorded" glyph that
  taps through to that entry in the settings screen.
- **Capability advertisement:** the daemon's session/capabilities (or the
  auth_ok capability block) gains a `receipts` flag so the phone shows the UI
  only when the daemon actually keeps receipts — no dead menus otherwise.

## 6. Alternatives considered

| Alternative | Verdict |
| --- | --- |
| **Owner-initiated push** (owner picks target, transfer is immediate) | Rejected as the primary model: best for a 2-device fleet but needs a new device-roster wire surface and the recipient gets a session appearing with no action of theirs — worse for attestation (the recipient never *acted*, so a claim receipt would attest a non-action). Release+claim gives both devices a signable act. `PendingHandoffTo` gives push-like directedness without the roster. |
| **Request + approve** (B requests, A approves like a permission) | Rejected: most ceremony, and requires non-owners to discover sessions they cannot see — a bigger visibility change than release+claim. |
| **Single release receipt** (only releaser signs) | Rejected: the claim side is unattested; an auditor cannot prove who received the session. Dual receipts cost nothing extra given D5. |
| **Daemon-signed transfer record** | Kept only as the *fallback* for D4 when a device can't sign (the `receipt-unavailable` analog): if the claim receipt round-trip times out, the daemon writes a daemon-signed `receipt-unavailable` marker into B's chain, exactly as permission receipts already do — so the chain never gaps. |
| **Couple handoff availability to `receipts.enabled`** | Rejected (D6): gates a broadly useful control feature behind a compliance feature. |
| **Trust the daemon's "verified" flag on the phone** | Rejected (D9): defeats the non-repudiation purpose; the phone has the keys to check for itself. |

## 7. Risks

| Risk | Mitigation |
| --- | --- |
| A released session is claimed by the *wrong* device in an open release | Named/targeted release (`PendingHandoffTo`) for anything sensitive; open release is opt-in per release and documented as "any of your paired devices". |
| Release races a concurrent prompt from the old owner | Release takes the same manager lock as every ownership mutation; a prompt after release fails `Authorize` cleanly (the old owner is no longer owner). No new race surface. |
| Two devices claim a released open session simultaneously | First-touch claim is already single-winner under the manager lock (0056 H-4 persist-then-stamp); the loser gets `ErrForbidden`. |
| Handoff receipts double-count or mis-chain | Same round-trip, same per-device chain, same F1/F2 hardening as permission receipts; new predicate types are inert data to the storage/verify layers. |
| Phone shows a stale "verified" after a daemon tampered post-fetch | Verification is recomputed on-device every render from the JWS bytes (D9); there is no cached daemon verdict to go stale. |

## 8. Recommendation

Proceed with D1–D9 as locked. The handoff feature is a genuinely small,
reuse-heavy change to the ownership core; the dual-receipt layer is the first
real exercise of 0077's extensibility claim and validates it; the phone
surfacing closes the "receipts exist but are invisible on the device that
signs them" gap. Sequenced in the linked PLAN so handoff (control feature)
lands independently of its receipt layer (compliance feature), and the phone
read surface lands independently of both.
