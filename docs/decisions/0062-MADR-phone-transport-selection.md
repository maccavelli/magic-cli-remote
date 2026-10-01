# MADR 0062: Phone transport awareness (mesh / relay selection)

- **Status**: Accepted — decisions locked 2026-08-01; **Socratic dialectic
  amendments applied** the same day; **implemented 2026-08-01** (phases
  P0 → P4, mobile only). Implementation amendments **A1–A5** are folded into
  D4/D7/D8/D10/D11 below and marked inline.
- **Date**: 2026-08-01
- **Deciders**: Project Owner
- **Review**: Socratic Thinker dialectic (stress-test + Chaos black swan);
  synthesis incorporated into D4–D5, D10–D11
- **Implementation plan**:
  [0062-PLAN-phone-transport-selection.md](0062-PLAN-phone-transport-selection.md)
  — codebase-grounded phases, APIs, tests, and acceptance gates (for review)
- **Scope**: Flutter client path selection and connect-screen / settings UX for
  choosing **mesh (direct)** vs **relay (mcrelay join)** when pairing,
  connecting, and reconnecting. Does **not** change mcrelay join-plane
  protocol, mcremote registration, pair URI wire format, or inner TLS /
  client-key rules.
- **Related**:
  [0015-MADR-mcrelay-transport-security.md](0015-MADR-mcrelay-transport-security.md)
  (H7 mesh preference, pair `relay`/`hid`, opaque splice),
  [0016-MADR-mcrelay-audit-hardening.md](0016-MADR-mcrelay-audit-hardening.md)
  (R7 `/healthz` reachability probe),
  [0061-MADR-relay-pair-advertise-and-path-selection.md](0061-MADR-relay-pair-advertise-and-path-selection.md)
  (attempt-scoped force-relay on QR — superseded for interactive path; advertise
  vs register),
  [protocol-v1.md](protocol-v1.md) (inner control plane after transport is up).
- **Supersedes**: Implicit path policy in 0061 **D2** for interactive connect
  (QR/paste/Connect). Replaces “QR with relay always forces relay” with
  probe-aware availability + explicit user selection when both transports are
  live.
- **Non-goals**: Second control-plane API on mcrelay; putting the public relay
  URL into the Host field; pair URI flag for host-side “relay-only advertise”
  (out of scope — see D9); multi-step automatic failover chains beyond one
  DialEpisode fallback (D4).

## Product requirement (locked)

Make the phone app **transport aware** with **smart probes**:

| Phone has… | Behaviour |
|------------|-----------|
| **Only mesh** available (configured + probe pass; relay missing or probe fail) | Use mesh; no Transport menu |
| **Only relay** available (configured + probe pass; mesh missing or probe fail) | Use relay; no Transport menu |
| **Both** configured **and both probes pass** | Show **Transport** control (**Mesh** \| **Relay**); user may initiate either; default **Mesh** unless the user selected Relay |
| First connect | Honour **user transport selection** when the menu is shown; if probes leave **only one** transport available, **auto-select that transport** (no menu) |
| Any dial | **DialEpisode**: primary path once; on **retryable** failure, **one** automatic attempt of the other **configured** transport; **permanent** errors never failover |

Transport control lives on **both** the initial connect screen and Settings
(reconnect / preference without hunting for connect).

---

## Grounding facts (as of 2026-08-01)

### F1 — Two transports, one inner identity

| Transport | What the phone dials first | What authenticates the session |
|-----------|----------------------------|--------------------------------|
| **Mesh / direct** | `wss://<mcremote-host>:7531/v1/ws` (or LAN/LE host) | Device token + client key + TLS pin/mode on **mcremote** |
| **Relay** | Outer WSS to mcrelay `/v1/phone`, `join{host_id}`, then inner WSS to **same** mcremote URL via loopback bridge | Same inner rules; mcrelay never sees protocol-v1 plaintext |

Code: `McremoteClient._openSocketDirect` vs `_openSocketViaRelay` +
`RelayTransport` (`apps/mobile/lib/data/ws/`).

Pair URI always carries **mcremote** as `host=` (and `fp`/`mode`). Relay is
optional add-on params `relay=` + `hid=` ([0015](0015-MADR-mcrelay-transport-security.md),
`PairPayload` / `pairuri`). Host is never the public relay URL.

### F2 — What “configured” means on the phone today

| Signal | Storage / source | Meaning |
|--------|------------------|---------|
| Mesh Host | `SettingsStore` host / connect Host field / pair `host=` | mcremote authority to dial (direct) or to present as inner peer (relay) |
| Relay route | prefs `relay_url`, `relay_host_id`, `relay_authority` (and attempt-scoped fields on ConnectScreen) | Outer join tuple; bound to one mcremote authority (MADR 0046 M-2) |
| Token / pin | secure storage | Orthogonal to transport |

There is **no** dedicated “mesh disabled” flag. Under the current pair
contract, **Host is always present** after a successful pair. Pure “relay
credentials without Host” is invalid (`PairPayload` rejects). “Only relay
available” is therefore **configured + probe**: mesh probe fails while relay
is configured and probe-pass.

### F3 — Path selection is automatic today (no user control)

Decision sits in `McremoteClient._shouldUseRelay` (post-0061):

1. **Attempt-scoped** non-empty `relayUrl` + `relayHostId` (from this QR/paste)
   → **always relay** (0061 D2) — **superseded by this MADR**.
2. Partial attempt args → never invent a path.
3. Else load stored relay for matching authority; if none → **direct**.
4. Else `probeDirectReachable(host)` (~900 ms, `/healthz` then TLS/TCP,
   0016 R7) → **direct if true, else relay**.

Connect screen today **auto-claims/connects immediately** on QR apply
(`_applyPair` → `_claimCode` / `_connect`) — no pause for transport choice.
That conflicts with a dual-available menu and is fixed in D5.

### F4 — Probes are soft signals

- Mesh: `probeDirectReachable` (can false-positive on open ports; false-negative
  on slow mesh).
- Relay: mcrelay `GET /healthz` proves the **edge process** is up, **not** that
  this host is registered or will accept join (`host_offline` still possible).

Probes gate UI availability only. Real dial + DialEpisode covers false
positives.

### F5 — Tension with prior ADRs (resolved)

| Prior | Resolution in 0062 |
|-------|---------------------|
| 0015 **H7** prefer mesh when reachable | Default **Mesh** when both probes pass; sticky may be relay |
| 0061 **D2** QR force-relay | **Withdrawn**; QR only configures relay |

---

## Problem statement

Hybrid operators need explicit transport control when both paths work, automatic
single-path behaviour when only one works (via probe), first-run pairing that
does not strand off-mesh users on a false-positive mesh path, and reconnect
that sticks to what worked without thrashing under mobile multi-trigger storms.

---

## Decision drivers

- Smart probes gate the dual-transport UI (only when both live).
- User selection is authoritative when the menu is shown; probes alone pick
  the path when only one transport is available.
- **One DialEpisode policy** for all dial entry points (not reconnect-only).
- Permanent auth/config errors never trigger transport failover.
- At most one in-flight episode process-wide (mobile multi-trigger safety).
- Host stays mcremote; relay stays outer edge.
- Preference editable on connect **and** Settings.
- No new pair URI flags in this scope.
- Pure policy unit-testable without sockets.

---

## Decision outcome (locked)

### D1 — Transport availability model

```text
enum TransportKind { mesh, relay }

class TransportAvailability {
  bool meshConfigured;    // Host authority non-empty
  bool relayConfigured;   // relay URL + hid for this Host authority
  bool meshOperational;   // probe pass (D2)
  bool relayOperational;  // probe pass (D2)
}

// Derived:
//   meshAvailable  = meshConfigured  && meshOperational
//   relayAvailable = relayConfigured && relayOperational
```

| Available set | UI / auto path |
|---------------|----------------|
| {mesh} only | No menu; use **mesh** |
| {relay} only | No menu; use **relay** (Host still required for inner URL) |
| {mesh, relay} | Show Transport menu; default **Mesh** |
| {} | Error `no_transport` (show probe/config diagnostics) |

### D2 — Smart probes (strict dual-pass for the menu)

**Locked: Option B (strict).**

- Run short probes in **parallel** (target wall clock ≤ ~1 s) when Host/relay
  credentials change, on connect-screen load with dual config, and after
  QR/paste populate.
- **Transport dropdown only when both `meshAvailable` and `relayAvailable`.**
- One probe pass → no menu; auto-use that transport.
- Neither → disable Connect; surface which probes failed.

| Transport | Probe | Notes |
|-----------|-------|-------|
| Mesh | `probeDirectReachable(host)` (0016 R7) | Soft availability |
| Relay | HTTPS `GET /healthz` on mcrelay base (`{"ok":true}`) | Soft: edge up ≠ host registered |

Probe results are **session-ephemeral** (re-probe on pull/retry). Probe fail is
not a sticky ban. User cannot pick a probe-failed transport from a menu (menu
hidden); after a **dial** failure they may **Try other** if the other is
**configured** (D5).

### D3 — Interactive selection policy

```text
resolveInteractive(
  meshAvailable, relayAvailable,
  userSelection: mesh | relay | null,
) -> mesh | relay | error
```

| meshAvailable | relayAvailable | userSelection | Result |
|---------------|----------------|---------------|--------|
| true | false | * | **mesh** (auto; no menu) |
| false | true | * | **relay** (auto; no menu) |
| true | true | null | **mesh** (default when menu shown) |
| true | true | mesh | **mesh** |
| true | true | relay | **relay** |
| false | false | * | **error** `no_transport` |

When the menu is hidden (sole available), **ignore** stale
`transport_selection` / sticky for **interactive** path choice. Sticky still
influences the **default highlight** when the menu is shown, if that sticky
path is still available.

### D4 — DialEpisode (unified dial policy)

**Locked (Socratic amendment):** one policy for **claim, Connect, cold
auto-connect, lifecycle reconnect, Settings Reconnect-now**.

```text
DialEpisode(
  primary: TransportMode,       // resolved by UI or sticky rules
  alternate: TransportMode?,  // other if configured
  forcedPrimary: bool,          // Settings Reconnect-now / explicit Try-other
)
```

Algorithm:

1. If another DialEpisode is **in flight**, cancel/supersede via connect epoch
   (extend existing `_connectEpoch` / `_reconnectInFlight`); never run two
   fallback budgets in parallel. **A1 implementation note:** the epoch is
   claimed **once per episode**, not per leg — an alternate leg that took a
   fresh epoch would make its own predecessor's cleanup look like supersession
   by a stranger.
2. Dial `primary` once.
3. On **success** → set `last_transport_success = primary`; done.
4. On **permanent** error (D10) → surface error; **no** transport fallback.
5. On **retryable** error, if `alternate` is configured and this
   **network-generation** has not already spent its fallback budget → dial
   `alternate` once.
6. On alternate success → set `last_transport_success = alternate`.
7. On alternate failure or no alternate → error; stop. No mesh↔relay loop.

**Amendment A1 (implementation, blocking) — a spent credential is never
re-sent.** A pair code is one-shot: the daemon removes it from its store on
`pair.claim` (`internal/auth/paircode.go`, `Take`) and only restores it if the
subsequent device-create fails. So a claim that times out or drops *client
side* may already have been consumed *host side*. Rule 5 is therefore gated on
a `credentialSpent` latch set the instant the claim frame is written:
`claimPairCode` may fall back on **pre-claim** failures only (socket open,
relay join, TLS), and never afterwards — whatever the error code says. Without
this, the natural reading of "`auth_timeout` is retryable" resends the code
over the relay, meets a permanent `invalid_code`, and strands a user whose
token exists on the host and was never delivered. The connect screen mirrors
the rule: after a spent claim it withholds "Try Mesh / Try Relay" entirely and
tells the user to get a fresh code. A token connect is idempotent and keeps the
full retryable set.

**Amendment A5 — user-initiated episodes are exempt from the generation
budget.** The budget (D11) exists to stop a *machine* thrashing under a
connectivity storm. A human tapping **Try Relay** or **Reconnect now** is
self-rate-limiting, and under a strict budget the automatic fallback would
consume it and leave the user's own action a dead button. Background entries
(backoff timer, lifecycle resume, background maintenance) still respect it.

**Episode budget.** ~35s wall clock, enforced by refusing to *start* an
alternate leg past the deadline rather than by racing a timer against a dial —
the latter orphans sockets (0046 H-A). Each leg is already bounded by its own
`ready`/request timeouts (8s mesh, 20s relay). Per-leg progress is published so
a mesh→relay episode reads as two deliberate attempts, not one silent stall.

**Primary resolution:**

| Entry | Primary |
|-------|---------|
| Interactive Connect (menu or sole path) | `resolveInteractive` result |
| Cold auto-connect / lifecycle resume | `last_transport_success` if configured, else interactive defaults (sole available, or Mesh if both available without waiting for menu) |
| Settings **Reconnect now** | **Forced** user-selected mode (`forcedPrimary=true`); fallback still allowed once |
| Explicit **Try Mesh/Relay** after error | Forced that mode as primary of a **new** user-initiated episode |

**Network generation:** incremented on connectivity-change / app-resume that
triggers reconnect. Fallback budget is **per generation**, not per call site
(Chaos amendment: prevents thrash under multi-trigger storms).

### D5 — Connect screen UX + QR/paste state machine

**Socratic amendment:** stop unconditional auto-claim on QR when dual-available.

| Step | Action |
|------|--------|
| 1 | Populate Host (+ relay tuple if present); pin/mode pending |
| 2 | Parallel probes (“Checking transports…”) |
| 3a | **Both available** → show Transport menu (default Mesh, or sticky if still available); **wait for Connect** (do **not** auto-claim) |
| 3b | **Sole available** → chip “Using Mesh/Relay”; **auto-claim/connect** on that path via DialEpisode |
| 3c | **None available** → disable Connect; probe diagnostics + Retry probes |
| 4 | Connect / claim uses DialEpisode with resolved primary |
| 5 | On retryable failure with other configured → offer **Try Mesh/Relay** (and DialEpisode already attempted one automatic fallback for non-forced interactive? **Yes** for claim/connect primary failure — one automatic alternate, then Try-other for a third user-driven attempt) |

Clarify automatic vs user:

- **Automatic:** one alternate inside DialEpisode after retryable primary fail.
- **User Try-other:** new episode with forced primary = the other transport
  (may fallback again only if that episode’s alternate is still configured and
  budget allows — typically alternate is the first path, one more try).

Host field = mcremote; relay URL/hid informational under Relay option.

### D6 — Settings UX

- Last success transport; probe snapshot + refresh.
- Transport control when both available (same model as connect).
- **Reconnect now** → DialEpisode with **forced** selected mode (D4).
- Users can switch transport without re-pairing.

### D7 — Client API shape

```dart
enum TransportMode { mesh, relay }

Future<void> connect({
  required String hostInput,
  required String token,
  TransportMode? transport,   // A3: nullable — see below
  String? relayUrl,
  String? relayHostId,
  bool allowTransportFallback = true,
  bool userInitiated = true,  // A5: exempts the episode from the D11 budget
  ...
});

Future<String> claimPairCode({ ... same transport + fallback ... });
```

- `mesh` → direct only for that attempt leg.
- `relay` → via relay; require tuple else `relay_misconfigured`.
- QR args configure relay tuple only; no force-relay.
- Fallback orchestration lives in one DialEpisode coordinator (not scattered
  ifs in connect vs reconnect).

**Amendment A3 — `transport` is nullable at the public API, required at the
socket.** Background entries (cold auto-connect, lifecycle reconnect, the
backoff timer, background maintenance) have no UI to resolve a mode, and making
each call site duplicate the sticky→config ladder is how path selection got
scattered in the first place. `null` means "resolve inside the episode"; a
non-null value is authoritative. D7's actual guarantee — *no leg dials without
an explicit mode* — is enforced where it matters, at `_openSocket(…, required
TransportMode mode)`, which no longer infers anything from the presence of
relay arguments.

**Interactive dials probe; background dials do not.** An interactive entry with
*both* transports configured runs the D2 probe pair before choosing, which is
what keeps an off-mesh QR pairing from paying an 8s mesh timeout once the old
force-relay heuristic is gone. With only one transport configured there is
nothing a probe could change, so none runs.

### D8 — Persistence

| Key | Purpose |
|-----|---------|
| Existing `relay_*` | Relay configuration |
| `last_transport_success` | Sticky for cold/lifecycle primary |
| `transport_selection` | Last UI pick when dual-available |
| `transport_authority` | The host both values belong to (A4) |

Clear with `clearAll`. Authority change clears with relay route (0046 M-2).

**Amendment A4 — one value plus its owner, not a per-host map.** Equivalent for
v1's single host, and materially safer: the owner is validated on **read**, so
no call site can leak a stale preference onto a different daemon by forgetting
to clear it. Writing a preference for a new authority drops the other one.
Revisit if multi-host lands.

### D9 — Out of scope

| Item | Status |
|------|--------|
| Pair URI flag (`mesh=0`, `transport=relay`) | **Not in scope** |
| Server / mcrelay protocol changes | Not required |
| Multi-step automatic failover chains | Rejected; DialEpisode one fallback only |

### D10 — Error taxonomy (fallback gate)

| Class | Codes (illustrative; map from `McException.code`) | Transport fallback? |
|-------|-----------------------------------------------------|---------------------|
| **Retryable** | `connect_failed`, `auth_timeout`, `relay_join_failed`, `host_offline`, `relay_connect_failed`, generic timeout / network | **Yes** (once) |
| **Permanent** | `invalid_token`, `expired`, `invalid_code`, `cert_mismatch`, `cert_unpinned`, `client_key_required`, `client_key_mismatch`, `unauthorized`, `unavailable`, `bad_version`, `no_credentials`, `pair_failed`, `unexpected_pair_response`, `rate_limited` | **No** |
| **Config error (A2)** | `relay_misconfigured`, `no_transport` | **Yes** — see below |

Unknown codes: treat as **retryable** once (prefer connectivity recovery over
stranding), then surface.

**Amendment A2 — config errors hop, they do not strand.** `relay_misconfigured`
and `no_transport` say *"this transport is unusable as configured"*, which is
the strongest possible argument for trying the **other** one, not for giving
up. The realistic trigger is mundane: sticky says relay, `setRelayRoute`
cleared the route on an authority change, and a perfectly reachable mesh is
never dialled. They are checked **before** the permanent sets, because they
arrive flagged `permanent: true` on the exception — and that flag is about
auto-reconnect parking, a different axis. `no_transport` is surfaced to the
user only once both transports are exhausted or unconfigured.

**Two independent sets, deliberately.** The transport denylist above is *not*
`McException.permanent`. That flag governs whether **auto-reconnect parks**
(0046 L-3, which deliberately narrowed it); this set governs whether a
**transport hop** is allowed. `rate_limited` is the clearest case: it must keep
retrying in the background and must never hop (hopping does not placate a
rate-limiting host, and on a claim it spends the code).

**The classifier consults the union of the pair and auth permanent sets**, so a
pair-only verdict such as `expired` also blocks a hop on a token connect where
it cannot normally arise. That fails **closed**, which is the correct
asymmetry: a missed hop costs one surfaced error, a wrong hop can cost a
credential (A1).

### D11 — Concurrency / multi-trigger safety

| Rule | Mechanism |
|------|-----------|
| Single in-flight DialEpisode | Connect epoch supersession (`_connectEpoch`); one bump per **episode**, shared by both legs (A1) |
| One fallback budget per network generation | Generation id bumped on connectivity change / resume-driven reconnect — **background episodes only** (A5) |
| No sticky thrash | Update sticky only on **auth success**, not on probe |

**Amended by MADR 0063 D6 (implemented 2026-08-01):** a reconnect that follows
the *death of the transport currently carrying the session* is exempt from this
budget, on the same reasoning as A5 for user-initiated episodes. The budget
stops a machine thrashing on **blind** retries; a transport-death failover is
not blind — it responds to a specific observed event. Charging it to the budget
produces the worst case: during a connectivity storm the budget is already
spent, so a genuine transport death cannot fail over and the user sits
disconnected with a working alternative one hop away.

**Where the generation is bumped matters.** `ConnectionLifecycleScope` bumps it
*inside* its 350ms `_retryNow` debounce, immediately before
`reconnectFromStore`. A burst of connectivity callbacks collapses into one
debounced body and therefore shares one generation and one hop; bumping per
callback would hand every flap in an airplane-mode toggle its own hop, which is
exactly the thrash this rule forbids.

---

## Implementation sketch (phased) — adjusted post-dialectic

| Phase | Work | Exit |
|-------|------|------|
| **P0** | `TransportAvailability`, parallel probes (mesh + relay healthz), pure `resolveInteractive` | D1–D3 unit green |
| **P0.5** | **DialEpisode** coordinator + D10 taxonomy + epoch/generation guards | Claim/connect/reconnect share one path; concurrency tests |
| **P1** | `TransportMode` on connect/claim/openSocket; remove force-relay-on-attempt | Mode-forced dial tests |
| **P2** | ConnectScreen: probe gate, **QR defer auto-claim when dual-available**, sole-path auto, menu default Mesh | Widget tests for state machine |
| **P3** | sticky + DialEpisode fallback; lifecycle/connectivity generation | Storm test: multi-trigger does not double-fallback thrash |
| **P4** | Settings control + Reconnect now (forced primary); Try-other buttons | Manual hybrid ops smoke |

No mcremote/mcrelay server changes. Pair URI unchanged.

### Primary code touch list

| Path | Role |
|------|------|
| `apps/mobile/lib/data/protocol/transport_policy.dart` (new) | Availability, resolveInteractive, DialEpisode policy, D10 sets |
| `apps/mobile/lib/data/ws/mcremote_client.dart` | Mode dial; episode coordinator; epoch/generation |
| `apps/mobile/lib/data/ws/relay_transport.dart` | Relay healthz helper |
| `apps/mobile/lib/features/connect/connect_screen.dart` | D5 state machine |
| `apps/mobile/lib/features/settings/settings_screen.dart` | Transport + Reconnect now |
| `apps/mobile/lib/data/local/settings_store.dart` | sticky/selection keys |
| `apps/mobile/lib/app_lifecycle.dart` | Network generation on resume/connectivity |
| `apps/mobile/test/*` | Policy, episode, connect, reconnect storms |
| `docs/0061-…` | Note D2 superseded |

---

## Socratic dialectic summary

### Lemma trail (condensed)

1. **Thesis:** Soft probes + dual-pass menu + sticky/fallback is sound if dial
   failure covers false positives.
2. **Antithesis:** Interactive/first connect lacked automatic single fallback
   (worse off-mesh than 0061 force-relay); QR auto-claim blocks user selection;
   permanent vs retryable undefined; Settings forced primary undefined.
3. **Defense:** Unify DialEpisode for all entry points; QR defer when dual;
   D10 taxonomy; Settings forced primary.
4. **Chaos (black swan):** Concurrent lifecycle/Settings/connectivity episodes
   each spend a fallback → thrash under flaky networks.
5. **Defense:** Process-wide single in-flight episode + fallback budget per
   **network generation**.

### Aporia verdict

Product rules stand. **Plan must include** DialEpisode unification, D10, QR
state machine, Settings forced primary, and concurrency guards **before** UI
polish. Residual accepted risk: probe false-positives and relay healthz ≠ host
online — mitigated by DialEpisode + Try-other, not by forcing QR to relay.

---

## Consequences

### Positive

- Dual live paths → user chooses; default Mesh.
- Sole live path → auto, no wrong menu.
- First-run and reconnect share failover semantics (no “reconnect is smarter
  than pair”).
- Multi-trigger storms cannot stack independent fallbacks.
- Permanent auth errors do not hop transports.

### Negative / trade-offs

- Dual-available QR no longer one-scan-and-done: user taps Connect after
  probes (slight friction; correct for selection).
- Sole-available still auto-starts (fast path preserved).
- Relay healthz false sense of join readiness remains residual.

### Neutral

- Inner auth, pin, client key unchanged.
- Host field = mcremote only.

---

## Alternatives considered (rejected)

| Alternative | Why rejected |
|-------------|--------------|
| Menu whenever both configured (no dual probe) | Owner locked strict dual-pass |
| Sticky-only reconnect without fallback | Strands phone when sticky dies |
| Interactive without automatic fallback | Off-mesh first-run worse than 0061 (Socratic) |
| Keep 0061 QR force-relay | Removes hybrid on-mesh choice |
| Pair URI transport flag | Out of scope |
| Per-call-site fallback without generation budget | Chaos thrash under multi-trigger |
| Transport control only on connect | Owner: connect **and** Settings |

---

## Review decisions (closed)

| # | Question | Decision |
|---|----------|----------|
| 1 | Menu when? | **Only both probes pass** (D2) |
| 2 | Reconnect failover? | **Sticky + one fallback** via DialEpisode (D4) |
| 3 | First connect? | **User selection when menu**; sole available auto (D3, D5) |
| 4 | Pair URI flag? | **Not in scope** (D9) |
| 5 | Where is Transport UI? | **Connect + Settings** (D5, D6) |
| 6 *(Socratic)* | Interactive vs reconnect fallback? | **Same DialEpisode** for all dial entry points (D4) |
| 7 *(Socratic)* | QR auto-claim when dual-available? | **Defer**; wait for Connect (D5) |
| 8 *(Socratic)* | Concurrent reconnect storms? | **One episode + generation budget** (D11) |

---

## Verification plan

- Unit: `resolveInteractive` matrix; DialEpisode primary/fallback/permanent;
  generation budget not double-spent.
- Unit: probes → sole/dual/none availability.
- Widget: dual-available QR does **not** auto-claim; menu default Mesh; sole
  available auto-starts.
- Integration: multi-trigger reconnect does not thrash mesh↔relay.
- Manual: on-mesh dual menu; off-mesh sole relay auto; false-positive mesh then
  automatic one relay fallback; invalid_token does not hop.

---

## Relationship to 0061

**0061 D2 is superseded** for interactive phone path selection:

| 0061 D2 (old) | 0062 (new) |
|---------------|------------|
| Attempt-scoped relay args **force** relay | Args **configure** relay; path from D2–D5 + DialEpisode |

0061 D1 (advertise vs register) and D3 (Host vs Relay UI split) remain in force.
